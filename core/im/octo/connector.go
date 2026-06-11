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
	"github.com/lml2468/xclaw/core/persona"
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

	// persona, when its grantor uid is set, makes this connector a persona clone
	// (openclaw OBO): extended trigger gate, OBO v2 relevance filter, and
	// on_behalf_of reply routing. Set once at startup via SetPersona; read-only
	// thereafter, so it needs no lock.
	persona persona.Grantor

	// runCtx is the context passed to Run; the sink/inbound callbacks (which the
	// gateway.Sink interface does not thread a context through) tie their work to
	// it, so a cancelled Run aborts in-flight turns and outbound REST calls.
	runCtx context.Context

	mu      sync.Mutex
	targets map[string]replyTarget // sessionKey → where to send the reply
	sock    *socketConn
	closed  bool

	// onStatus, if set, is called when the connection state changes
	// (connected=true after a successful register+handshake; false on drop).
	onStatus func(connected bool, lastErr string)

	// reconnect/backoff
	reconnectBase time.Duration
	reconnectMax  time.Duration
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
	// onBehalfOf, when set, routes the reply (and typing) as the grantor for a
	// persona clone (openclaw OBO). Empty for normal replies.
	onBehalfOf string
}

// SetPersona configures this connector as a persona clone of grantor (openclaw
// OBO). When set, the connector (a) extends the group trigger gate so an
// @grantor / @所有人 mention triggers a turn, (b) applies the OBO v2 relevance
// filter, and (c) routes the reply back to the origin channel with on_behalf_of.
// A zero Grantor (no uid) leaves the connector a regular bot.
func (c *Connector) SetPersona(grantor persona.Grantor) { c.persona = grantor }

// NewConnector builds a connector. The gateway must be constructed with this
// connector as its Sink (see AsSink note in package docs).
func NewConnector(rest *RESTClient) *Connector {
	return &Connector{
		rest:          rest,
		targets:       make(map[string]replyTarget),
		reconnectBase: 3 * time.Second,
		reconnectMax:  60 * time.Second,
	}
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
//
// Persona-clone path (openclaw OBO, inbound.ts): when this connector is a
// persona clone, the group trigger gate is widened (an @grantor / @所有人
// mention triggers a turn), the OBO v2 relevance filter drops irrelevant @AI
// fan-out BEFORE any session state is recorded, and the reply target carries
// on_behalf_of so the server presents the reply as the grantor.
func (c *Connector) onInbound(m BotMessage) {
	uid := c.uid()
	if m.FromUID == uid {
		return // ignore our own messages
	}
	if m.Payload.Type != MsgText || strings.TrimSpace(m.Payload.Content) == "" {
		return // MVP handles text only
	}

	// OBO v2 detection (openclaw inbound.ts ~L2102-2116). The grantor relays a
	// fan-out message carrying an origin-channel hint so the clone replies in the
	// origin group. SECURITY: only trust OBO fields when the message is sent by
	// the configured grantor — otherwise any user could forge a reply-as-someone.
	oboV2 := c.persona.Configured() &&
		m.Payload.OBOOriginChannelID != "" &&
		oboRespondAs(m.Payload) != "" &&
		m.FromUID == c.persona.UID

	// OBO v2 relevance filter (openclaw inbound.ts ~L2122-2160): drop pure @AI
	// fan-out that does not address the grantor BEFORE recording any reply target
	// or session state, so an irrelevant message never leaks into the clone's
	// session.
	if oboV2 && !c.persona.Relevant(m.PersonaMention()) {
		c.logf("OBO v2 skipped — message not relevant to persona")
		return
	}

	chType := router.ChannelDM
	if m.ChannelType == ChannelGroup {
		chType = router.ChannelGroup
	}

	// Trigger gate: persona-aware for clones (an @grantor / @所有人 mention is a
	// call to the clone); a plain @bot / @AI mention otherwise.
	triggered := m.Triggers(uid, c.persona)

	inbound := router.InboundMessage{
		FromUID:     m.FromUID,
		FromName:    m.FromName,
		ChannelID:   m.ChannelID,
		ChannelType: chType,
		Text:        m.Payload.Content,
		Mentioned:   triggered,
	}
	key, err := inbound.SessionKey()
	if err != nil {
		return // unroutable
	}

	// Resolve where (and as whom) the reply goes (openclaw inbound.ts
	// ~L2301-2337). OBO v2: reply to the origin channel as the grantor. Group
	// persona trigger-as-grantor: reply in the same group as the grantor.
	tgt := replyTarget{channelID: m.ChannelID, channelType: m.ChannelType}
	if oboV2 {
		tgt = oboReplyTarget(m.Payload, c.persona.UID)
	} else if chType == router.ChannelGroup &&
		c.persona.TriggeredAsGrantor(m.PersonaMention(), m.ExplicitlyMentionsBot(uid)) {
		tgt.onBehalfOf = c.persona.UID
	}
	c.mu.Lock()
	c.targets[key] = tgt
	c.mu.Unlock()

	if c.gateway == nil {
		return
	}
	// A group message that doesn't trigger the bot is background context, not a
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

// oboRespondAs resolves the grantor uid the payload claims to respond as,
// preferring obo_respond_as over obo_grantor_uid (openclaw inbound.ts L2104).
func oboRespondAs(p MessagePayload) string {
	if p.OBORespondAs != "" {
		return p.OBORespondAs
	}
	return p.OBOGrantorUID
}

// oboReplyTarget derives the OBO v2 reply destination from a (grantor-trusted)
// payload (openclaw inbound.ts ~L2305-2326). DM-relay origin → reply to the
// original sender's uid; group/thread → reply to the origin group. The reply
// always carries on_behalf_of=grantor (the trusted configured grantor uid, NOT
// the payload's respond_as).
func oboReplyTarget(p MessagePayload, grantorUID string) replyTarget {
	chType := ChannelGroup
	if p.OBOOriginChannelType != nil {
		chType = ChannelType(*p.OBOOriginChannelType)
	}
	channelID := p.OBOOriginChannelID
	if chType == ChannelDM && p.OBOOriginFromUID != "" {
		// DM: the bot is only friends with the grantor; reply to the original
		// sender via on_behalf_of=grantor, which bypasses the bot-friend gate.
		channelID = p.OBOOriginFromUID
	}
	return replyTarget{channelID: channelID, channelType: chType, onBehalfOf: grantorUID}
}

// --- gateway.Sink ---

// OnEvent surfaces a typing indicator on the first activity of a turn. (Token /
// tool events are not mirrored to IM in the MVP.)
func (c *Connector) OnEvent(sessionKey string, ev agent.AgentEvent) {
	if ev.Kind == agent.KindSessionStarted {
		if tgt, ok := c.target(sessionKey); ok {
			if err := c.rest.SendTypingAs(c.ctx(), tgt.channelID, tgt.channelType, tgt.onBehalfOf); err != nil {
				c.logf("send typing for %s: %v", sessionKey, err)
			}
		}
	}
}

// OnReply delivers the assembled assistant reply back to the originating
// channel, split into <=3500-char segments (api/stream-relay parity). For a
// persona clone replying as the grantor, the reply carries on_behalf_of so the
// server presents it as the grantor (openclaw OBO).
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
		if _, err := c.rest.SendTextAs(c.ctx(), tgt.channelID, tgt.channelType, seg, nil, false, tgt.onBehalfOf); err != nil {
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
