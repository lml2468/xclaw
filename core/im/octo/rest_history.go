package octo

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/lml2468/octobuddy/core/clog"
)

// maxHistoricalPayloadBase64Len caps a base64 payload before decode.
// 256 KiB base64 ≈ 192 KiB decoded — well above any legitimate IM
// payload, guards against a hostile / huge sync row.
const maxHistoricalPayloadBase64Len = 256 * 1024

// HistoricalMessage is one row from /v1/bot/messages/sync. The server
// ships content / type / url / name inside a base64-encoded JSON
// payload; GetChannelMessages decodes it and prefers a usable top-level
// field, falling back to the decoded payload.
type HistoricalMessage struct {
	FromUID    string
	FromName   string
	Content    string
	Timestamp  int64
	MessageID  string
	MessageSeq int64
	Type       int
	URL        string
	Name       string
}

type channelMessagesResponse struct {
	Messages []channelMessageWire `json:"messages"`
}

type channelMessageWire struct {
	FromUID   string `json:"from_uid"`
	FromName  string `json:"from_name"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	// message_id is flexString because the server returns it as a number
	// on some deploys and a string on others; strict-string decode would
	// fail the whole response and silently drop the cold-start backfill.
	MessageID  flexString      `json:"message_id"`
	MessageSeq int64           `json:"message_seq"`
	Type       int             `json:"type"`
	URL        string          `json:"url"`
	Name       string          `json:"name"`
	Payload    json.RawMessage `json:"payload"`
}

type historicalPayload struct {
	Content string `json:"content"`
	Type    int    `json:"type"`
	URL     string `json:"url"`
	Name    string `json:"name"`
}

// GetChannelMessages pulls recent messages for a channel via the
// WuKongIM sync endpoint (used by cold-start backfill). limit defaults
// to 20 and caps the returned slice client-side. Returns nil on any
// failure — the agent runs fine without history.
func (c *RESTClient) GetChannelMessages(ctx context.Context, channelID string, channelType ChannelType, limit int) []HistoricalMessage {
	if limit <= 0 {
		limit = 20
	}
	body := map[string]any{
		"channel_id":        channelID,
		"channel_type":      int(channelType),
		"limit":             limit,
		"start_message_seq": 0,
		"end_message_seq":   0,
		"pull_mode":         1, // 1 = pull newer messages
	}
	var raw channelMessagesResponse
	if err := c.postJSON(ctx, "/v1/bot/messages/sync", body, &raw); err != nil {
		clog.For("octo").Warn("getChannelMessages", "err", err)
		return nil
	}
	msgs := capHistoricalMessages(raw.Messages, limit)
	out := make([]HistoricalMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.toHistoricalMessage())
	}
	return out
}

func capHistoricalMessages(msgs []channelMessageWire, limit int) []channelMessageWire {
	if len(msgs) > limit {
		return msgs[:limit] // client-side cap (api.ts D1/S7)
	}
	return msgs
}

func (m channelMessageWire) toHistoricalMessage() HistoricalMessage {
	pl := decodeHistoricalPayload(m.Payload)
	return HistoricalMessage{
		FromUID:    m.FromUID,
		FromName:   m.FromName,
		Content:    firstNonEmpty(m.Content, pl.Content),
		Timestamp:  m.Timestamp,
		MessageID:  string(m.MessageID),
		MessageSeq: m.MessageSeq,
		Type:       firstNonZero(m.Type, pl.Type),
		URL:        firstNonEmpty(m.URL, pl.URL),
		Name:       firstNonEmpty(m.Name, pl.Name),
	}
}

func decodeHistoricalPayload(payload json.RawMessage) historicalPayload {
	var pl historicalPayload
	if len(payload) == 0 {
		return pl
	}
	var s string
	if json.Unmarshal(payload, &s) == nil {
		decodeBase64HistoricalPayload(s, &pl)
		return pl
	}
	if err := json.Unmarshal(payload, &pl); err != nil {
		clog.For("octo").Warn("getChannelMessages object-payload decode", "err", err)
	}
	return pl
}

func decodeBase64HistoricalPayload(s string, pl *historicalPayload) {
	if len(s) > maxHistoricalPayloadBase64Len {
		clog.For("octo").Warn("getChannelMessages dropping oversized payload", "base64_chars", len(s))
		return
	}
	if dec, derr := base64.StdEncoding.DecodeString(s); derr == nil {
		if err := json.Unmarshal(dec, pl); err != nil {
			clog.For("octo").Warn("getChannelMessages base64-payload decode", "err", err)
		}
	}
}
