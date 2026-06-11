package octo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lml2468/xclaw/core/agent"
	"github.com/lml2468/xclaw/core/gateway"
	"github.com/lml2468/xclaw/core/router"
)

// Connector wires the Octo IM platform to the gateway: it registers the bot,
// connects the WuKongIM socket, maps inbound BotMessages to
// router.InboundMessage, and (as a gateway.Sink) delivers replies via REST. It
// is the IM-specific edge; everything inside the gateway stays IM-agnostic.
type Connector struct {
	rest    *RESTClient
	gateway *gateway.Gateway

	botUID string

	// runCtx is the context passed to Run; the sink/inbound callbacks (which the
	// gateway.Sink interface does not thread a context through) tie their work to
	// it, so a cancelled Run aborts in-flight turns and outbound REST calls.
	runCtx context.Context

	mu      sync.Mutex
	targets map[string]replyTarget // sessionKey → where to send the reply
	sock    *socketConn
	closed  bool

	// toolProgress mirrors the agent's tool invocations to the channel as it runs
	// (opt-in; see config AgentConfig.ToolProgress). progress holds the per-turn
	// dedup/cap state, keyed by sessionKey; both are guarded by c.mu.
	toolProgress bool
	progress     map[string]*toolProgressState

	// onStatus, if set, is called when the connection state changes
	// (connected=true after a successful register+handshake; false on drop).
	onStatus func(connected bool, lastErr string)

	// reconnect/backoff
	reconnectBase time.Duration
	reconnectMax  time.Duration
}

// maxToolNotices caps how many "🔧 Running …" notices a single turn may emit, so
// a tool-heavy run can't flood the channel. Mirrors cc-channel-octo's
// MAX_TOOL_NOTICES (src/index.ts).
const maxToolNotices = 10

// toolProgressState is the per-turn dedup/cap state for tool-progress notices.
type toolProgressState struct {
	lastNotice string // last label sent, to collapse consecutive duplicates
	count      int    // notices sent this turn
}

// awaitTokenPoll is how often Run rechecks for an injected token before it has
// one (see secret.inject). Short enough that the bot connects promptly once the
// GUI injects, without busy-spinning.
const awaitTokenPoll = 2 * time.Second

// OnStatus registers a connection-state callback (used by the daemon's bot
// registry to surface per-bot status over the control bus).
func (c *Connector) OnStatus(fn func(connected bool, lastErr string)) { c.onStatus = fn }

func (c *Connector) setStatus(connected bool, lastErr string) {
	if c.onStatus != nil {
		c.onStatus(connected, lastErr)
	}
}

type replyTarget struct {
	channelID   string
	channelType ChannelType
}

// NewConnector builds a connector. The gateway must be constructed with this
// connector as its Sink (see AsSink note in package docs).
func NewConnector(rest *RESTClient) *Connector {
	return &Connector{
		rest:          rest,
		targets:       make(map[string]replyTarget),
		progress:      make(map[string]*toolProgressState),
		reconnectBase: 3 * time.Second,
		reconnectMax:  60 * time.Second,
	}
}

// SetToolProgress enables/disables mirroring tool invocations to the channel as
// "🔧 Running <tool>(<params>)…" notices (opt-in; off by default). Wired from
// the bot's resolved AgentConfig.ToolProgress.
func (c *Connector) SetToolProgress(on bool) {
	c.mu.Lock()
	c.toolProgress = on
	c.mu.Unlock()
}

// SetGateway attaches the gateway (done after construction to resolve the
// Connector-is-Sink-of-Gateway cycle).
func (c *Connector) SetGateway(g *gateway.Gateway) { c.gateway = g }

// Run registers the bot and maintains the socket connection with reconnect
// until ctx is cancelled. The initial registration is retried with backoff so a
// transient API outage at startup doesn't kill the bot.
func (c *Connector) Run(ctx context.Context) error {
	c.runCtx = ctx
	// REST heartbeat loop (30s), separate from the WS ping.
	go c.heartbeatLoop(ctx)

	backoff := c.reconnectBase
	var reg RegisterResponse
	registered := false

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// No token yet (config has none and secret.inject hasn't arrived): wait
		// for one rather than hammering Register with an empty bearer. The GUI
		// injects tokens shortly after the control bus connects.
		if c.rest.Token() == "" {
			c.setStatus(false, "awaiting secret")
			sleep(ctx, awaitTokenPoll)
			continue
		}

		if !registered {
			r, err := c.rest.Register(ctx, false)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				c.setStatus(false, err.Error())
				sleep(ctx, backoff)
				backoff = minDur(backoff*2, c.reconnectMax)
				continue
			}
			reg = r
			c.setUID(reg.RobotID)
			registered = true
			backoff = c.reconnectBase
		}

		c.setStatus(true, "")
		err := c.connectOnce(ctx, reg)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		c.setStatus(false, errStr)

		// Connection dropped: back off, then force a fresh registration (token
		// may have expired) before reconnecting.
		sleep(ctx, backoff)
		backoff = minDur(backoff*2, c.reconnectMax)
		if fresh, rerr := c.rest.Register(ctx, true); rerr == nil {
			reg = fresh
			c.setUID(reg.RobotID)
		} else {
			registered = false // force the retry path above
		}
	}
}

// sleep waits for d or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (c *Connector) connectOnce(ctx context.Context, reg RegisterResponse) error {
	sock := newSocketConn(reg.WSURL, reg.RobotID, reg.IMToken, c.onInbound, func(error) {})
	c.mu.Lock()
	c.sock = sock
	c.mu.Unlock()
	// Always release the socket (fd + ping/watch goroutines) when this attempt
	// ends, so reconnects don't accumulate connections.
	defer sock.close()

	if err := sock.connect(ctx); err != nil {
		return err
	}
	return sock.run(ctx)
}

// ctx returns the Run context, falling back to Background if a callback somehow
// fires before Run set it (defensive — a nil context would panic downstream).
func (c *Connector) ctx() context.Context {
	if c.runCtx != nil {
		return c.runCtx
	}
	return context.Background()
}

// setUID / uid guard botUID with c.mu: Run rewrites it on (re)registration while
// the sink callbacks (OnReply/OnEvent → logf) and a concurrent turn read it.
func (c *Connector) setUID(id string) {
	c.mu.Lock()
	c.botUID = id
	c.mu.Unlock()
}

func (c *Connector) uid() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.botUID
}

// logf reports a recovered/handled error to stderr, tagged with the bot uid.
func (c *Connector) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[octo %s] "+format+"\n", append([]any{c.uid()}, args...)...)
}

func (c *Connector) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.rest.Heartbeat(ctx)
		}
	}
}

// onInbound maps a decoded BotMessage to a router.InboundMessage and feeds the
// gateway. Drops the bot's own messages and non-text payloads.
func (c *Connector) onInbound(m BotMessage) {
	uid := c.uid()
	if m.FromUID == uid {
		return // ignore our own messages
	}
	if m.Payload.Type != MsgText || strings.TrimSpace(m.Payload.Content) == "" {
		return // MVP handles text only
	}

	chType := router.ChannelDM
	if m.ChannelType == ChannelGroup {
		chType = router.ChannelGroup
	}

	inbound := router.InboundMessage{
		FromUID:     m.FromUID,
		FromName:    m.FromName,
		ChannelID:   m.ChannelID,
		ChannelType: chType,
		Text:        m.Payload.Content,
		Mentioned:   m.MentionsBot(uid),
	}
	key, err := inbound.SessionKey()
	if err != nil {
		return // unroutable
	}
	// Remember where to deliver the reply for this session.
	c.mu.Lock()
	c.targets[key] = replyTarget{channelID: m.ChannelID, channelType: m.ChannelType}
	c.mu.Unlock()

	if c.gateway == nil {
		return
	}
	// A group message that doesn't mention the bot is background context, not a
	// turn: observe it so it becomes a later @-mention's delta. (The router
	// would drop it anyway; observing first preserves group context.)
	if inbound.ChannelType == router.ChannelGroup && !inbound.Mentioned {
		c.gateway.Observe(inbound)
		return
	}
	if _, err := c.gateway.Handle(c.ctx(), inbound); err != nil {
		c.logf("handle turn for %s: %v", key, err)
	}
}

// --- gateway.Sink ---

// OnEvent surfaces a typing indicator on the first activity of a turn and, when
// tool-progress is enabled, mirrors each tool invocation to the channel as a
// brief "🔧 Running <tool>(<params>)…" notice. Ported from cc-channel-octo's
// opt-in `sdk.toolProgress` hook (src/index.ts onToolUse): collapse consecutive
// duplicates, cap at maxToolNotices per turn, reset on turn start/end.
func (c *Connector) OnEvent(sessionKey string, ev agent.AgentEvent) {
	switch ev.Kind {
	case agent.KindSessionStarted:
		// Turn start: reset the per-turn progress state and show a typing hint.
		c.mu.Lock()
		if c.toolProgress {
			c.progress[sessionKey] = &toolProgressState{}
		}
		c.mu.Unlock()
		if tgt, ok := c.target(sessionKey); ok {
			if err := c.rest.SendTyping(c.ctx(), tgt.channelID, tgt.channelType); err != nil {
				c.logf("send typing for %s: %v", sessionKey, err)
			}
		}
	case agent.KindToolUse:
		c.maybeSendToolNotice(sessionKey, ev)
	case agent.KindTurnDone:
		// Turn end: drop the per-turn progress state so the next turn starts clean.
		c.mu.Lock()
		delete(c.progress, sessionKey)
		c.mu.Unlock()
	}
}

// maybeSendToolNotice emits a "🔧 Running <tool>(<params>)…" notice for a
// KindToolUse event when tool-progress is on, collapsing consecutive identical
// notices and capping the count per turn. The dedup/cap decision is made under
// c.mu; the REST send happens after unlocking so a slow send never holds the
// connector lock (and never blocks the agent stream's other sessions).
func (c *Connector) maybeSendToolNotice(sessionKey string, ev agent.AgentEvent) {
	label := ev.ToolName
	if label == "" {
		return
	}
	if ev.ToolParams != "" {
		// ToolParams is already a whitespace-collapsed one-liner truncated to 120
		// chars by claude.go's truncateParams — mirrors MAX_TOOL_PARAM_CHARS.
		label += "(" + ev.ToolParams + ")"
	}

	c.mu.Lock()
	if !c.toolProgress {
		c.mu.Unlock()
		return
	}
	st := c.progress[sessionKey]
	if st == nil {
		// No KindSessionStarted seen for this session this turn — start fresh.
		st = &toolProgressState{}
		c.progress[sessionKey] = st
	}
	if label == st.lastNotice {
		c.mu.Unlock()
		return // collapse exact consecutive repeats
	}
	st.lastNotice = label
	if st.count >= maxToolNotices {
		c.mu.Unlock()
		return // capped — stay quiet for the rest of the turn
	}
	st.count++
	c.mu.Unlock()

	tgt, ok := c.target(sessionKey)
	if !ok {
		return
	}
	if _, err := c.rest.SendText(c.ctx(), tgt.channelID, tgt.channelType, "🔧 Running "+label+"…", nil, false); err != nil {
		c.logf("send tool-progress for %s: %v", sessionKey, err)
	}
}

// OnReply delivers the assembled assistant reply back to the originating
// channel, split into <=3500-char segments (api/stream-relay parity).
func (c *Connector) OnReply(sessionKey string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	tgt, ok := c.target(sessionKey)
	if !ok {
		return
	}
	for _, seg := range splitMessage(text, 3500) {
		if _, err := c.rest.SendText(c.ctx(), tgt.channelID, tgt.channelType, seg, nil, false); err != nil {
			c.logf("send reply to %s: %v", sessionKey, err)
		}
	}
}

func (c *Connector) target(sessionKey string) (replyTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.targets[sessionKey]
	return t, ok
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// splitMessage breaks text into <=max-rune segments, preferring paragraph,
// newline, then space boundaries before a hard cut (stream-relay parity).
func splitMessage(text string, max int) []string {
	runes := []rune(text)
	if len(runes) <= max {
		return []string{text}
	}
	var out []string
	for len(runes) > max {
		cut := max
		// prefer a boundary within the window
		window := string(runes[:max])
		if i := strings.LastIndex(window, "\n\n"); i > 0 {
			cut = len([]rune(window[:i]))
		} else if i := strings.LastIndex(window, "\n"); i > 0 {
			cut = len([]rune(window[:i]))
		} else if i := strings.LastIndex(window, " "); i > 0 {
			cut = len([]rune(window[:i]))
		}
		if cut <= 0 {
			cut = max
		}
		out = append(out, strings.TrimRight(string(runes[:cut]), " \n"))
		runes = runes[cut:]
		// skip leading whitespace of the next segment
		for len(runes) > 0 && (runes[0] == '\n' || runes[0] == ' ') {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}
