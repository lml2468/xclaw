// Command octobuddy-daemon is the OctoBuddy gateway daemon.
//
// It wires the full pipeline — store + router + gateway + agent driver — and
// drives it from an inbound source. Two front ends:
//
//	octobuddy-daemon # REPL on stdin (claude driver)
//	octobuddy-daemon -control /tmp/octobuddy.sock # serve the control bus (for the GUI app)
//
// With -control it listens on a Unix socket speaking the proto/ NDJSON protocol
// so the desktop app (or any client) can send commands and receive the live
// event stream. The REPL and the control bus can run together.
//
// Each inbound becomes a DM; the gateway routes it (per-session lock, rate
// limit), drives the agent, streams events to every sink (stdout + control bus),
// and persists the assistant reply + resume id for multi-turn continuity.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lml2468/octobuddy/core/agent"
	"github.com/lml2468/octobuddy/core/control"
	"github.com/lml2468/octobuddy/core/control/wire"
	"github.com/lml2468/octobuddy/core/gateway"
	"github.com/lml2468/octobuddy/core/groupctx"
	"github.com/lml2468/octobuddy/core/im/octo"
	"github.com/lml2468/octobuddy/core/router"
	"github.com/lml2468/octobuddy/core/store"
)

func main() {
	flags := parseDaemonFlags()

	// Config mode: load the single ~/.octobuddy/config.json and run every bot in its
	// own isolated stack. Mutually exclusive with the single-bot flag front ends.
	// `-config` with no value uses the default ~/.octobuddy/config.json. `-control`
	// additionally serves the bus so a GUI can manage all bots.
	if configFlagSet() {
		runConfigMode(flags.configPath, flags.controlSock, flags.exitParent, flags.authStdin)
		return
	}

	runSingleBotMode(flags)
}

type daemonFlags struct {
	claudeBin   string
	fromUID     string
	dbPath      string
	maxPerMin   int
	controlSock string
	noREPL      bool
	octoAPI     string
	octoToken   string
	configPath  string
	exitParent  bool
	authStdin   bool
}

func parseDaemonFlags() daemonFlags {
	claudeBin := flag.String("claude-bin", "", "claude executable (default: 'claude' on PATH)")
	fromUID := flag.String("uid", "repl-user", "synthetic from_uid for REPL inbound (DM session key)")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "octobuddy-daemon.db"), "sqlite path")
	maxPerMin := flag.Int("rate", 30, "max messages per minute per session")
	controlSock := flag.String("control", "", "serve the control bus on this Unix socket path (enables GUI clients)")
	noREPL := flag.Bool("no-repl", false, "disable the stdin REPL (control-bus only)")
	octoAPI := flag.String("octo-api", "", "Octo API base URL (enables the Octo IM connector)")
	octoToken := flag.String("octo-token", "", "Octo bot token (bf_*); or set OCTOBUDDY_OCTO_TOKEN")
	configPath := flag.String("config", "", "load ~/.octobuddy/config.json (or given path) and run all configured bots")
	exitParent := flag.Bool("exit-with-parent", false, "exit when the parent process dies (set by the GUI so the daemon never outlives the app)")
	authStdin := flag.Bool("control-auth-stdin", false, "read the control-bus capability token as the first line of stdin (set by the GUI; out-of-band, never an env/argv). Off = no token: privileged bus commands are denied")
	flag.Parse()
	return daemonFlags{
		claudeBin:   *claudeBin,
		fromUID:     *fromUID,
		dbPath:      *dbPath,
		maxPerMin:   *maxPerMin,
		controlSock: *controlSock,
		noREPL:      *noREPL,
		octoAPI:     *octoAPI,
		octoToken:   *octoToken,
		configPath:  *configPath,
		exitParent:  *exitParent,
		authStdin:   *authStdin,
	}
}

func runSingleBotMode(flags daemonFlags) {
	st, err := store.Open(flags.dbPath)
	if err != nil {
		fatal("store open: %v", err)
	}
	defer st.Close()

	drv := agent.NewClaudeDriver(flags.claudeBin)

	started := time.Now()

	// Signal cancellation lets control-bus and IM turns finish before defers run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Single-bot secrets are seeded from flags/env and can be updated via control.
	sec := &secretStore{}
	drv.EnvFn = singleBotEnvFn(sec, flags.octoAPI)

	rt := router.New(router.Config{MaxPerMinute: flags.maxPerMin})
	sinks, srv := singleBotSinks(flags.controlSock)
	connector := singleBotConnector(flags.octoAPI, flags.octoToken, sec, &sinks)
	gw := singleBotGateway(drv, st, rt, sinks, connector)

	if srv != nil {
		waitTurns, closeBus := configureSingleBotControl(ctx, srv, gw, st, drv, sec, started, flags)
		defer waitTurns()
		defer closeBus()
	}

	if connector != nil {
		startSingleBotConnector(ctx, connector, gw, flags.octoAPI)
		defer connector.WaitTurns()
	}

	fmt.Printf("octobuddy-daemon — driver=%s caps=%+v\n", drv.Name(), drv.Capabilities())
	fmt.Printf("db=%s  session=dm:%s\n", flags.dbPath, flags.fromUID)

	if flags.noREPL || connector != nil {
		waitSingleBot(ctx, stop, flags.exitParent)
		return
	}

	fmt.Println("type a message and press enter; /reset clears the session; Ctrl-D to exit")
	runREPL(context.Background(), gw, st, flags.fromUID)
}

func singleBotSinks(controlSock string) (multiSink, *control.Server) {
	sinks := multiSink{&stdoutSink{}}
	if controlSock == "" {
		return sinks, nil
	}
	srv := control.NewServer(nil)
	sinks = append(sinks, control.NewEventSink(srv))
	return sinks, srv
}

func singleBotGateway(drv agent.Driver, st *store.Store, rt *router.Router, sinks multiSink, connector *octo.Connector) *gateway.Gateway {
	gw := gateway.New(drv, st, rt, sinks)
	if connector == nil {
		return gw
	}
	return gw.WithGroupContext(groupctx.New(6000)).
		WithMediaAuth(connector.MediaAuth()).
		WithGroupBackfill(connector.BotUID, connector.BackfillFetch)
}

func singleBotEnvFn(sec *secretStore, octoAPI string) func() []string {
	return func() []string {
		var out []string
		if t := sec.GatewayToken(); t != "" {
			out = append(out, "ANTHROPIC_AUTH_TOKEN="+t)
		}
		// octo-cli companion credential: the agent's octo-cli reads these from
		// the env (no on-disk profile). Mirrors DriverEnvForOcto in -config mode.
		if t := sec.OctoToken(); t != "" {
			out = append(out, "OCTO_BOT_TOKEN="+t)
		}
		if octoAPI != "" {
			out = append(out, "OCTO_API_BASE_URL="+octoAPI)
		}
		return out
	}
}

func singleBotConnector(octoAPI, octoToken string, sec *secretStore, sinks *multiSink) *octo.Connector {
	token := octoToken
	if token == "" {
		token = os.Getenv("OCTOBUDDY_OCTO_TOKEN")
	}
	if octoAPI == "" {
		return nil
	}
	if token == "" {
		fatal("-octo-api set but no token (use -octo-token or OCTOBUDDY_OCTO_TOKEN)")
	}
	_ = sec.Set(wire.SecretKindOcto, token)
	connector := octo.NewConnector(octo.NewRESTClient(octoAPI, sec.OctoToken))
	*sinks = append(*sinks, connector)
	return connector
}

func configureSingleBotControl(ctx context.Context, srv *control.Server, gw *gateway.Gateway, st *store.Store, drv *agent.ClaudeDriver, sec *secretStore, started time.Time, flags daemonFlags) (func(), func()) {
	handler, target := makeCommandHandler(ctx, gw, st, drv, sec, srv, started)
	srv.SetHandler(handler)
	configureBusAuth(srv, flags.authStdin)
	return target.turnsWG.Wait, serveControlBus(srv, flags.controlSock)
}

func startSingleBotConnector(ctx context.Context, connector *octo.Connector, gw *gateway.Gateway, octoAPI string) {
	connector.SetGateway(gw)
	go func() {
		if err := connector.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "octo connector: %v\n", err)
		}
	}()
	fmt.Printf("octo connector started (api=%s)\n", octoAPI)
}

func waitSingleBot(ctx context.Context, stop context.CancelFunc, exitParent bool) {
	if exitParent {
		watchParentExit(func() {
			fmt.Fprintln(os.Stderr, "parent exited; shutting down")
			stop()
		})
	}
	fmt.Println("running (control bus / IM connector); press Ctrl-C to exit")
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "shutting down")
}

// runREPL reads stdin lines and feeds each as an inbound DM through the gateway.
func runREPL(ctx context.Context, gw *gateway.Gateway, st *store.Store, fromUID string) {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for {
		fmt.Print("\n> ")
		if !sc.Scan() {
			break
		}
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if text == "/reset" {
			_ = st.ClearResume(fromUID)
			fmt.Println("(session reset)")
			continue
		}

		d, err := gw.Handle(ctx, router.InboundMessage{
			ChannelType: router.ChannelDM,
			FromUID:     fromUID,
			FromName:    fromUID,
			Text:        text,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "handle error: %v\n", err)
			continue
		}
		if d != router.Accepted {
			fmt.Printf("(dropped: %s)\n", d)
		}
	}
}

// stdoutSink renders the live event stream and the final reply to the terminal.
type stdoutSink struct{}

func (s *stdoutSink) OnEvent(sessionKey string, ev agent.AgentEvent) {
	switch ev.Kind {
	case agent.KindSessionStarted:
		fmt.Printf("  [session] %s\n", ev.SessionID)
	case agent.KindTextDelta:
		fmt.Printf("  [text]    %s\n", oneLine(ev.Text))
	case agent.KindThinking:
		fmt.Printf("  [think]   %s\n", oneLine(ev.Text))
	case agent.KindToolUse:
		fmt.Printf("  [tool]    🔧 %s(%s)\n", ev.ToolName, ev.ToolParams)
	case agent.KindToolResult:
		fmt.Printf("  [result]  (tool returned)\n")
	case agent.KindTurnDone:
		fmt.Println(turnDoneLine(ev.Usage))
	case agent.KindError:
		fmt.Printf("  [%s]   %s\n", eventErrorTag(ev), oneLine(ev.Err))
	case agent.KindSystem:
		fmt.Printf("  [sys]     %s\n", oneLine(ev.Text))
	}
}

func turnDoneLine(usage *agent.TokenUsage) string {
	if usage == nil {
		return "  [done]"
	}
	line := fmt.Sprintf("  [done]    in=%d out=%d tokens", usage.InputTokens, usage.OutputTokens)
	if usage.CachedInputTokens > 0 {
		line += fmt.Sprintf(" (cached=%d)", usage.CachedInputTokens)
	}
	if usage.CostUSD > 0 {
		line += fmt.Sprintf(" cost=$%.4f", usage.CostUSD)
	}
	return line
}

func eventErrorTag(ev agent.AgentEvent) string {
	if ev.Recoverable {
		return "retry"
	}
	return "ERR"
}

func (s *stdoutSink) OnReply(sessionKey string, text string) {
	if text != "" {
		fmt.Printf("\n💬 %s\n", text)
	}
}

// OnUserMessage is a no-op for the REPL/stdout sink — the user just typed
// the line into stdin and sees it in their own terminal already.
func (s *stdoutSink) OnUserMessage(string, router.InboundMessage) {}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// multiSink fans gateway events out to several sinks (stdout + control bus).
type multiSink []gateway.Sink

func (m multiSink) OnEvent(key string, ev agent.AgentEvent) {
	for _, s := range m {
		s.OnEvent(key, ev)
	}
}
func (m multiSink) OnReply(key, text string) {
	for _, s := range m {
		s.OnReply(key, text)
	}
}
func (m multiSink) OnUserMessage(key string, msg router.InboundMessage) {
	for _, s := range m {
		s.OnUserMessage(key, msg)
	}
}

// makeCommandHandler builds the single-bot control-bus dispatcher. All command
// logic lives in the shared makeHandler; this only supplies the fixed target,
// the synthetic one-bot roster, and the broadcast hook.
//
// Returns the handler AND the shared target so the caller can block on
// target.turnsWG before closing the store on shutdown (turns in flight under
// session.send would otherwise race the deferred st.Close).
func makeCommandHandler(ctx context.Context, gw *gateway.Gateway, st *store.Store, drv agent.Driver, sec *secretStore, srv *control.Server, started time.Time) (control.CommandHandler, *botTarget) {
	target := &botTarget{gateway: gw, store: st, secrets: sec}
	var broadcast func(string, any)
	if srv != nil {
		broadcast = srv.Broadcast
	}
	return makeHandler(ctx, handlerDeps{
		started:  started,
		driver:   drv.Name(),
		botCount: func() int { return 1 },
		// Single-bot mode has one implicit bot so a GUI client's bots.list
		// bootstrap works the same as in multi-bot mode (proto parity).
		list: func() []control.BotInfo {
			return []control.BotInfo{{ID: "default", Connected: true}}
		},
		resolve:   func(string) (*botTarget, error) { return target, nil },
		broadcast: broadcast,
	}), target
}
