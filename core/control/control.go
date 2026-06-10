// Package control implements the XClaw control bus: the NDJSON-over-Unix-socket
// protocol (see proto/README.md) between the Go daemon (xclawd) and client
// shells (the Swift macOS app, a web console, a CLI).
//
// This file defines the wire envelope and its codec. Framing decisions (max
// frame size, partial-read tolerance) follow the hardening lessons from Open
// Island's bridge transport.
package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

// ProtocolVersion is the envelope `v` field; clients must match (proto/README).
const ProtocolVersion = 1

// MaxFrameBytes caps a single NDJSON line so a peer that never sends a newline
// can't grow memory without bound.
const MaxFrameBytes = 4 * 1024 * 1024

// Kind discriminates the three envelope categories.
type Kind string

const (
	KindCommand  Kind = "command"  // client → server
	KindResponse Kind = "response" // server → client, correlated by id
	KindEvent    Kind = "event"    // server → client, unsolicited
)

// Envelope is the single wire unit. body is left raw so each side decodes it
// against the concrete command/event type named by `type`.
type Envelope struct {
	V    int             `json:"v"`
	Kind Kind            `json:"kind"`
	ID   string          `json:"id,omitempty"`   // correlates command↔response
	Type string          `json:"type"`           // e.g. "session.send", "session.text"
	TS   int64           `json:"ts,omitempty"`   // unix seconds
	Body json.RawMessage `json:"body,omitempty"` // type-specific payload
}

// ErrFrameTooLarge is returned when a line exceeds MaxFrameBytes.
var ErrFrameTooLarge = errors.New("control: frame exceeds max size")

// Encode marshals an envelope to a single NDJSON line (with trailing newline).
func Encode(e Envelope) ([]byte, error) {
	if e.V == 0 {
		e.V = ProtocolVersion
	}
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// NewScanner returns a bufio.Scanner configured for NDJSON envelopes with the
// frame-size cap applied.
func NewScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxFrameBytes)
	return sc
}

// Decode parses one NDJSON line into an envelope.
func Decode(line []byte) (Envelope, error) {
	if len(line) > MaxFrameBytes {
		return Envelope{}, ErrFrameTooLarge
	}
	var e Envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// --- typed bodies (proto/README.md) ---

// Commands (client → server)

type SessionSendBody struct {
	// BotID selects which bot to route to (multi-bot config mode). Empty = the
	// single/default bot.
	BotID string `json:"botId,omitempty"`
	// UID is the DM uid; the server routes it as a DM inbound for the MVP.
	UID  string `json:"uid"`
	Text string `json:"text"`
}

type SessionHistoryBody struct {
	BotID      string `json:"botId,omitempty"`
	SessionKey string `json:"sessionKey"`
	Limit      int    `json:"limit"`
}

// SecretInjectBody carries a single secret into the core (proto: secret.inject).
// The value is held in memory only — never persisted, never logged.
type SecretInjectBody struct {
	BotID string `json:"botId,omitempty"`
	Kind  string `json:"kind"` // e.g. "octoToken" | "gatewayToken"
	Value string `json:"value"`
}

// Responses / event bodies (server → client)

type OKBody struct {
	OK bool `json:"ok"`
}

type HealthBody struct {
	Uptime      int64  `json:"uptime"`
	Connections int    `json:"connections"`
	Driver      string `json:"driver"`
	Bots        int    `json:"bots"`
}

// BotInfo describes one bot for the bots.list response and bot.status events.
type BotInfo struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
	LastError string `json:"lastError,omitempty"`
}

type SessionTextBody struct {
	BotID      string `json:"botId,omitempty"`
	SessionKey string `json:"sessionKey"`
	Delta      string `json:"delta"`
}

type SessionToolBody struct {
	BotID      string `json:"botId,omitempty"`
	SessionKey string `json:"sessionKey"`
	Name       string `json:"name"`
	Params     string `json:"params"`
}

type SessionUsageBody struct {
	BotID        string `json:"botId,omitempty"`
	SessionKey   string `json:"sessionKey"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
}

type SessionReplyBody struct {
	BotID      string `json:"botId,omitempty"`
	SessionKey string `json:"sessionKey"`
	Text       string `json:"text"`
}

type SessionActivityBody struct {
	BotID      string `json:"botId,omitempty"`
	SessionKey string `json:"sessionKey"`
	Kind       string `json:"kind"`
}

type ErrorBody struct {
	BotID       string `json:"botId,omitempty"`
	Scope       string `json:"scope"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	TS      int64  `json:"ts"`
}
