package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PromptMode controls how ClaudeDriver assembles the system prompt and
// tool surface. Minimal aligns with the Agent SDK's reference invocation
// (replace the built-in prompt, silence cwd config, explicit tool
// whitelist). claude_code keeps Claude Code's built-in preamble and
// blanket tool access — escape hatch for SOUL.md authored against the
// preamble.
type PromptMode string

const (
	// PromptModeMinimal is the default: Request.SystemPrompt REPLACES
	// the built-in system prompt; cwd .claude/ is not auto-loaded; the
	// tool surface is the whitelist in Request.AllowedTools (or the
	// driver default if nil). Interactive tools are always disallowed.
	PromptModeMinimal PromptMode = "minimal"
	// PromptModeClaudeCode preserves the previous behavior: prompt is
	// APPENDED to the built-in one, cwd .claude/ auto-loads, every tool
	// runs under bypassPermissions. Use only for bots whose SOUL.md was
	// authored assuming Claude Code's preamble.
	PromptModeClaudeCode PromptMode = "claude_code"
)

// defaultHeadlessAllowedTools is the headless-safe surface ClaudeDriver
// passes to --allowedTools in minimal mode. Bump per claude release when
// the upstream tool set changes. mcp__* covers MCP additions automatically.
var defaultHeadlessAllowedTools = []string{
	"Read", "Edit", "Write", "Bash",
	"Grep", "Glob",
	"WebSearch", "WebFetch",
	"NotebookEdit", "TodoWrite",
	"Agent", "Skill",
	"mcp__*",
}

// disallowedInteractiveTools are blocked unconditionally in minimal mode
// because they expect human input the daemon can't supply (they would
// hang the turn in headless).
var disallowedInteractiveTools = []string{
	"AskUserQuestion",
	"EnterPlanMode", "ExitPlanMode",
	"EnterWorktree", "ExitWorktree",
	"ShareOnboardingGuide",
}

// ClaudeDriver drives Claude Code headlessly via:
//
//	claude -p - --output-format stream-json --verbose [--resume <id>]...
//
// with the prompt fed on stdin, and normalizes its line-delimited JSON
// ("stream-json") into AgentEvents.
//
// Output is plain stream-json (one event per complete content block); the
// driver does NOT request --include-partial-messages, so there are no
// token-level deltas to de-duplicate.
//
// It is the ONE place that knows about the claude binary, its argv
// shape, and its env requirements (ANTHROPIC_*, CLAUDE_CONFIG_DIR).
type ClaudeDriver struct {
	// Bin is the claude executable (default "claude" on PATH).
	Bin string
	// Mode selects between minimal (default) and claude_code prompt modes.
	Mode PromptMode
	// ExtraArgs are appended verbatim.
	ExtraArgs []string
	// Env are extra KEY=VALUE entries layered onto os.Environ for the spawned
	// CLI (e.g. ANTHROPIC_BASE_URL, OCTO_BOT_ID, GH_TOKEN).
	Env []string
	// EnvFn, when set, is evaluated on every Query to build the extra env,
	// overriding the static Env.
	EnvFn func() []string

	// selfcheckOnce gates a one-time diagnostic line emitted on the
	// FIRST Query: claude path resolution, masked ANTHROPIC_AUTH_TOKEN,
	// ANTHROPIC_BASE_URL, CLAUDE_CONFIG_DIR + writability, effective
	// PromptMode + allowed-tools count. Single most useful line for
	// diagnosing "出错了，请稍后重试" from a fresh install.
	selfcheckOnce sync.Once
}

func NewClaudeDriver(bin string) *ClaudeDriver {
	if bin == "" {
		bin = "claude"
	}
	return &ClaudeDriver{Bin: bin, Mode: PromptModeMinimal}
}

func (d *ClaudeDriver) Name() string { return "claude" }

func (d *ClaudeDriver) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Resume: true, ToolEvents: true}
}

// mode returns d.Mode, defaulting to PromptModeMinimal when unset so a
// zero-value ClaudeDriver stays headless-safe.
func (d *ClaudeDriver) mode() PromptMode {
	if d.Mode == PromptModeClaudeCode {
		return PromptModeClaudeCode
	}
	return PromptModeMinimal
}

func (d *ClaudeDriver) buildArgs(req Request) []string {
	// Prompt on stdin (`-p -`), never argv, so a large prompt (group
	// backfill + inlined file content) can't hit ARG_MAX.
	args := []string{"-p", "-", "--output-format", "stream-json", "--verbose"}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if d.mode() == PromptModeMinimal {
		args = d.appendMinimalModeArgs(args, req)
	} else {
		args = d.appendClaudeCodeModeArgs(args, req)
	}
	// Pin auto-memory to the per-session dir. --settings MERGES into the
	// defaults (does not replace), so project-scope skill discovery still
	// works. JSON-encode so a path with special chars can't break the flag.
	//
	// Contract: req.MemoryDir MUST live OUTSIDE req.Cwd. An agent with
	// write access to its cwd must not author the memory injected as
	// trusted context. The gateway derives the two from disjoint bases.
	if req.MemoryDir != "" {
		if b, err := json.Marshal(map[string]string{"autoMemoryDirectory": req.MemoryDir}); err == nil {
			args = append(args, "--settings", string(b))
		}
	}
	args = append(args, d.ExtraArgs...)
	return args
}

// appendMinimalModeArgs emits the SDK-aligned flag set: replace the
// system prompt, silence cwd `.claude/` config, explicit tool whitelist
// under default permission mode, headless-unsafe tools disallowed.
func (d *ClaudeDriver) appendMinimalModeArgs(args []string, req Request) []string {
	args = append(args, "--permission-mode", "default")
	// Empty value form silences user/project/local setting sources so a
	// planted CLAUDE.md / skills / agents under the sandbox cwd can't
	// influence the model.
	args = append(args, "--setting-sources=")
	// --system-prompt REPLACES the built-in prompt (doesn't append). Re-
	// sent every turn including resumes: it does NOT persist across
	// --resume, so omitting on a resumed turn would silently lose the
	// non-overridable SecurityPrefix + SOUL identity. Tokens are a
	// prompt-cache hit so re-sending is cheap.
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	allowed := req.AllowedTools
	if allowed == nil {
		allowed = defaultHeadlessAllowedTools
	}
	if len(allowed) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowed, ","))
	}
	if len(disallowedInteractiveTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(disallowedInteractiveTools, ","))
	}
	return args
}

// appendClaudeCodeModeArgs preserves the previous behavior — append on
// top of the built-in prompt, blanket tool access.
func (d *ClaudeDriver) appendClaudeCodeModeArgs(args []string, req Request) []string {
	args = append(args, "--permission-mode", "bypassPermissions")
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	return args
}

func (d *ClaudeDriver) Query(ctx context.Context, req Request) (<-chan AgentEvent, error) {
	cmd := d.buildCommand(ctx, req)
	stdout, stderr, err := commandPipes(cmd)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		// On Start failure cmd.Wait never runs, so Go's normal pipe-close
		// path never triggers and these descriptors leak until the *Cmd is
		// GC'd. Under fd exhaustion or repeated start failures this
		// accumulates quickly — close them explicitly.
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start %s: %w", d.Bin, err)
	}

	out := make(chan AgentEvent, 64)
	var wg sync.WaitGroup
	wg.Add(2)
	var sawTurnDone atomic.Bool
	d.startReaders(ctx, stdout, stderr, req.SessionID, out, &wg, &sawTurnDone)
	waitAndClose(ctx, cmd, out, &wg, &sawTurnDone)
	return out, nil
}

func (d *ClaudeDriver) startReaders(ctx context.Context, stdout, stderr io.Reader, sessionID string, out chan<- AgentEvent, wg *sync.WaitGroup, sawTurnDone *atomic.Bool) {
	go func() {
		defer wg.Done()
		d.drainStderr(ctx, stderr, sessionID, out)
	}()
	go func() {
		defer wg.Done()
		d.drainStdout(ctx, stdout, out, sawTurnDone)
	}()
}

func waitAndClose(ctx context.Context, cmd *exec.Cmd, out chan AgentEvent, wg *sync.WaitGroup, sawTurnDone *atomic.Bool) {
	go func() {
		defer close(out)
		wg.Wait()
		if err := cmd.Wait(); err != nil {
			emitAgentEvent(ctx, out, AgentEvent{
				Kind:        KindError,
				Err:         fmt.Sprintf("claude exited: %v", err),
				Recoverable: sawTurnDone.Load(),
				Raw:         err.Error(),
			})
		}
	}()
}

func (d *ClaudeDriver) drainStderr(ctx context.Context, stderr io.Reader, sessionID string, out chan<- AgentEvent) {
	sc := newClaudeScanner(stderr)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		emitAgentEvent(ctx, out, stderrLineEvent(line, sessionID))
	}
	if err := sc.Err(); err != nil {
		emitAgentEvent(ctx, out, AgentEvent{Kind: KindError, Err: fmt.Sprintf("stderr scan: %v", err), Recoverable: true, Raw: err.Error()})
	}
}

func (d *ClaudeDriver) drainStdout(ctx context.Context, stdout io.Reader, out chan<- AgentEvent, sawTurnDone *atomic.Bool) {
	sc := newClaudeScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		for _, ev := range parseClaudeLine(line) {
			if ev.Kind == KindTurnDone {
				sawTurnDone.Store(true)
			}
			emitAgentEvent(ctx, out, ev)
		}
	}
	if err := sc.Err(); err != nil {
		emitAgentEvent(ctx, out, AgentEvent{Kind: KindError, Err: fmt.Sprintf("stdout scan: %v", err), Raw: err.Error()})
	}
}

func newClaudeScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return sc
}

func stderrLineEvent(line, sessionID string) AgentEvent {
	if sessionID != "" && strings.Contains(line, "No conversation found with session ID") {
		return AgentEvent{Kind: KindError, Err: line, Recoverable: true, ResumeInvalid: true, Raw: line}
	}
	if isTransientUpstream(line) {
		return AgentEvent{Kind: KindError, Err: line, Recoverable: true, Transient: true, RetryHint: retryHint(line), Raw: line}
	}
	return AgentEvent{Kind: KindError, Err: line, Recoverable: true, Raw: line}
}

func emitAgentEvent(ctx context.Context, out chan<- AgentEvent, ev AgentEvent) {
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

func (d *ClaudeDriver) buildCommand(ctx context.Context, req Request) *exec.Cmd {
	cmd := exec.CommandContext(ctx, d.Bin, d.buildArgs(req)...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Env = mergedEnv(d.queryEnv())
	d.selfcheckOnce.Do(func() { d.logSelfcheck(cmd.Env, req.Cwd) })
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

func (d *ClaudeDriver) queryEnv() []string {
	if d.EnvFn != nil {
		return d.EnvFn()
	}
	return d.Env
}

func commandPipes(cmd *exec.Cmd) (stdout, stderr io.ReadCloser, err error) {
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err = cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}
	return stdout, stderr, nil
}
