// Package config implements XClaw's two-layer, bot-first configuration.
//
// Global ~/.xclaw/config.json holds shared defaults + a bots[] list. Per-bot
// ~/.xclaw/<id>/config.json holds that bot's token + overrides. The per-bot data
// directory (~/.xclaw/<id>/data) is DERIVED from baseDir + id, never configurable
// — so a bot can't escape its own subtree. The bot's persona/behavior prompt
// lives in SOUL.md + AGENTS.md in the same dir, not in config.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AgentConfig is the on-disk "agent" block: the model and the model-gateway
// routing (base URL + token) plus any extra env vars injected into the agent CLI.
type AgentConfig struct {
	Model          string            `json:"model,omitempty"`
	GatewayBaseURL string            `json:"gatewayBaseUrl,omitempty"`
	GatewayToken   string            `json:"gatewayToken,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// RateLimitConfig mirrors the on-disk rateLimit block.
type RateLimitConfig struct {
	MaxPerMinute int `json:"maxPerMinute,omitempty"`
}

// ContextConfig mirrors the on-disk context block.
type ContextConfig struct {
	MaxContextChars int `json:"maxContextChars,omitempty"`
}

// BotEntry is one entry in the global config's bots[] list — just the id; the
// bot's own settings live in ~/.xclaw/<id>/config.json.
type BotEntry struct {
	ID string `json:"id,omitempty"`
}

// File is the on-disk shape of a config.json (global or per-bot). Bots[] is only
// meaningful in the global file.
type File struct {
	APIURL    string           `json:"apiUrl,omitempty"`
	OctoToken string           `json:"octoToken,omitempty"`
	Agent     *AgentConfig     `json:"agent,omitempty"`
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`
	Context   *ContextConfig   `json:"context,omitempty"`
	Bots      []BotEntry       `json:"bots,omitempty"`
}

// Resolved is a single bot's fully-resolved, ready-to-run configuration.
type Resolved struct {
	BotID     string
	APIURL    string
	OctoToken string

	Agent     AgentConfig
	RateLimit RateLimitConfig
	Context   ContextConfig

	// SystemPrompt is the operator-trusted persona/behavior prompt, assembled
	// from SOUL.md + AGENTS.md in the bot dir (not from config).
	SystemPrompt string

	// DataDir is the bot's derived SQLite/data directory (~/.xclaw/<id>/data).
	DataDir string
}

func defaults() Resolved {
	return Resolved{
		RateLimit: RateLimitConfig{MaxPerMinute: 5},
		Context:   ContextConfig{MaxContextChars: 6000},
	}
}

var slugRE = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// validSlug reports whether id is a safe subtree name (no path separators, not
// "." or "..").
func validSlug(id string) bool {
	return slugRE.MatchString(id) && id != "." && id != ".."
}

// DefaultConfigPath is ~/.xclaw/config.json.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".xclaw", "config.json")
}

// Load reads the global config at path (or DefaultConfigPath) and resolves all
// bots. baseDir is the directory containing the config file.
func Load(path string) ([]Resolved, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(abs)

	global, err := readFile(abs)
	if err != nil {
		return nil, err
	}
	return resolveBots(global, baseDir)
}

// readFile parses a config.json, returning a zero File if it doesn't exist.
func readFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

// resolveBots expands the global config into one Resolved per bot, applying
// perBotFile-over-global precedence.
func resolveBots(global File, baseDir string) ([]Resolved, error) {
	entries := global.Bots
	if len(entries) == 0 {
		entries = []BotEntry{{ID: "default"}}
	}

	var out []Resolved
	seenID := map[string]bool{}

	for i, bot := range entries {
		id := bot.ID
		if id == "" {
			id = fmt.Sprintf("bot%d", i)
		}
		if !validSlug(id) {
			return nil, fmt.Errorf("bot %q: invalid id — letters, digits, dot, underscore, hyphen only (no path separators)", id)
		}
		if seenID[id] {
			return nil, fmt.Errorf("duplicate bot id %q", id)
		}
		seenID[id] = true

		botRoot := filepath.Join(baseDir, id)
		perBot, err := readFile(filepath.Join(botRoot, "config.json"))
		if err != nil {
			return nil, err
		}

		r := defaults()
		r.BotID = id
		r.DataDir = filepath.Join(botRoot, "data")

		// precedence: perBotFile ?? global
		r.APIURL = firstNonEmpty(perBot.APIURL, global.APIURL)
		r.OctoToken = firstNonEmpty(perBot.OctoToken, global.OctoToken)

		// shallow-merge agent/rateLimit/context: global → perBotFile keys
		mergeAgent(&r.Agent, global.Agent)
		mergeAgent(&r.Agent, perBot.Agent)
		mergeRate(&r.RateLimit, global.RateLimit)
		mergeRate(&r.RateLimit, perBot.RateLimit)
		mergeCtx(&r.Context, global.Context)
		mergeCtx(&r.Context, perBot.Context)

		// System prompt: SOUL.md (identity) + AGENTS.md (behavior), file-based.
		r.SystemPrompt = soul(botRoot)

		// validation
		if r.OctoToken == "" {
			return nil, fmt.Errorf("bot %q: missing octoToken", id)
		}
		if r.APIURL != "" && !isAllowedURL(r.APIURL) {
			return nil, fmt.Errorf("bot %q: unsafe apiUrl %q (must be https:// or http://localhost; SSRF protection)", id, r.APIURL)
		}
		if r.Agent.GatewayBaseURL != "" && !isAllowedURL(r.Agent.GatewayBaseURL) {
			return nil, fmt.Errorf("bot %q: unsafe gatewayBaseUrl %q (SSRF protection)", id, r.Agent.GatewayBaseURL)
		}

		out = append(out, r)
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func mergeAgent(dst *AgentConfig, src *AgentConfig) {
	if src == nil {
		return
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.GatewayBaseURL != "" {
		dst.GatewayBaseURL = src.GatewayBaseURL
	}
	if src.GatewayToken != "" {
		dst.GatewayToken = src.GatewayToken
	}
	// env merges per-key (global base + per-bot overrides/additions), not whole
	// replacement — so a bot can add its own OCTO_BOT_ID without dropping a
	// globally-shared key.
	if len(src.Env) > 0 {
		if dst.Env == nil {
			dst.Env = map[string]string{}
		}
		for k, v := range src.Env {
			dst.Env[k] = v
		}
	}
}

func mergeRate(dst *RateLimitConfig, src *RateLimitConfig) {
	if src != nil && src.MaxPerMinute > 0 {
		dst.MaxPerMinute = src.MaxPerMinute
	}
}

func mergeCtx(dst *ContextConfig, src *ContextConfig) {
	if src != nil && src.MaxContextChars > 0 {
		dst.MaxContextChars = src.MaxContextChars
	}
}

// soul assembles the bot's operator-trusted system prompt from two files in its
// dir: SOUL.md (identity/persona) followed by AGENTS.md (behavior norms). Each
// is trimmed; missing/empty files are skipped. Returns "" if neither exists.
func soul(botRoot string) string {
	var parts []string
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		data, err := os.ReadFile(filepath.Join(botRoot, name))
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(data)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// DriverEnv builds the KEY=VALUE environment to layer onto the claude CLI's
// process env: the user-declared agent.env plus the model-gateway routing vars
// (mapped to the names claude understands), appended last so they win over any
// same-named agent.env entry.
//
//	ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN
func (r Resolved) DriverEnv() []string {
	var out []string
	for k, v := range r.Agent.Env {
		out = append(out, k+"="+v)
	}
	if r.Agent.GatewayBaseURL != "" {
		out = append(out, "ANTHROPIC_BASE_URL="+r.Agent.GatewayBaseURL)
	}
	if r.Agent.GatewayToken != "" {
		out = append(out, "ANTHROPIC_AUTH_TOKEN="+r.Agent.GatewayToken)
	}
	return out
}
