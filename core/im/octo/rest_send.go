package octo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// SendMessageResult mirrors the server's response. message_id uses
// flexString because the server returns it as a JSON number on some
// deploys and a string on others; a strict-string decode would fail
// the whole response and our caller would treat the error as a
// transient send failure → retry with a fresh client_msg_no → user
// sees two copies of every reply.
type SendMessageResult struct {
	MessageID   flexString `json:"message_id"`
	ClientMsgNo string     `json:"client_msg_no"`
	MessageSeq  int        `json:"message_seq"`
}

// flexString accepts either a JSON string or number and decodes both to
// a Go string. Use it for server fields whose JSON type drifts across
// deploys.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	// Keep the bare-number literal so a uint64 messageID doesn't lose
	// precision through float64. Reject object / array / boolean so a
	// server regression can't propagate {"foo":1} verbatim as a string
	// MessageID (which would later corrupt read-receipt batches).
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("flexString: want JSON string or number, got %q: %w", b, err)
	}
	*f = flexString(string(n))
	return nil
}

// SendText posts a Text message to a channel. The mention object is only
// attached when at least one of mentionUIDs / mentionEntities / mentionAll
// is set.
func (c *RESTClient) SendText(ctx context.Context, channelID string, channelType ChannelType, content string, mentionUIDs []string, mentionEntities []MentionEntity, mentionAll bool) (SendMessageResult, error) {
	return c.SendTextAs(ctx, channelID, channelType, content, mentionUIDs, mentionEntities, mentionAll, "")
}

// SendTextAs is SendText with an optional grantor uid: when non-empty
// the IM presents the message as the grantor speaking (persona OBO).
// Generates a fresh client_msg_no; callers that retry MUST instead use
// SendTextAsWithMsgNo with a stable id (server dedup is keyed on it).
func (c *RESTClient) SendTextAs(ctx context.Context, channelID string, channelType ChannelType, content string, mentionUIDs []string, mentionEntities []MentionEntity, mentionAll bool, onBehalfOf string) (SendMessageResult, error) {
	return c.SendTextAsWithMsgNo(ctx, channelID, channelType, content, mentionUIDs, mentionEntities, mentionAll, onBehalfOf, uuid.NewString())
}

// SendTextAsWithMsgNo takes a caller-supplied client_msg_no for
// idempotent retry. A retry MUST reuse the original id; otherwise a
// post-commit failure (TCP reset, 502, timeout after the server
// committed) produces a successful retry with a new id and the user
// sees the message twice. clientMsgNo MUST be non-empty — empty keys
// collide across every empty-key send and dedup becomes undefined.
func (c *RESTClient) SendTextAsWithMsgNo(ctx context.Context, channelID string, channelType ChannelType, content string, mentionUIDs []string, mentionEntities []MentionEntity, mentionAll bool, onBehalfOf, clientMsgNo string) (SendMessageResult, error) {
	if clientMsgNo == "" {
		return SendMessageResult{}, fmt.Errorf("octo: SendTextAsWithMsgNo requires a non-empty clientMsgNo (server dedup key)")
	}
	payload := map[string]any{
		"type":    int(MsgText),
		"content": content,
	}
	if len(mentionUIDs) > 0 || len(mentionEntities) > 0 || mentionAll {
		mention := map[string]any{}
		if len(mentionUIDs) > 0 {
			mention["uids"] = mentionUIDs
		}
		if len(mentionEntities) > 0 {
			mention["entities"] = mentionEntities
		}
		if mentionAll {
			mention["all"] = 1
		}
		payload["mention"] = mention
	}
	body := map[string]any{
		"channel_id":    channelID,
		"channel_type":  int(channelType),
		"payload":       payload,
		"client_msg_no": clientMsgNo,
	}
	if onBehalfOf != "" {
		body["on_behalf_of"] = onBehalfOf
	}
	var out SendMessageResult
	if err := c.postJSON(ctx, "/v1/bot/sendMessage", body, &out); err != nil {
		return SendMessageResult{}, err
	}
	return out, nil
}

// SendTyping posts a typing indicator (api.ts sendTyping).
func (c *RESTClient) SendTyping(ctx context.Context, channelID string, channelType ChannelType) error {
	return c.SendTypingAs(ctx, channelID, channelType, "")
}

// SendTypingAs is SendTyping with an optional on_behalf_of grantor uid (openclaw
// OBO relay). An empty string is identical to SendTyping.
func (c *RESTClient) SendTypingAs(ctx context.Context, channelID string, channelType ChannelType, onBehalfOf string) error {
	body := map[string]any{
		"channel_id": channelID, "channel_type": int(channelType),
	}
	if onBehalfOf != "" {
		body["on_behalf_of"] = onBehalfOf
	}
	return c.postJSON(ctx, "/v1/bot/typing", body, nil)
}

// Heartbeat posts the REST heartbeat (api.ts sendHeartbeat).
func (c *RESTClient) Heartbeat(ctx context.Context) error {
	return c.postJSON(ctx, "/v1/bot/heartbeat", map[string]any{}, nil)
}

// SendReadReceipt acks one or more messages as read (api.ts sendReadReceipt,
// POST /v1/bot/readReceipt). Called fire-and-forget after an inbound message is
// handled.
func (c *RESTClient) SendReadReceipt(ctx context.Context, channelID string, channelType ChannelType, messageIDs []string) error {
	return c.postJSON(ctx, "/v1/bot/readReceipt", map[string]any{
		"channel_id":   channelID,
		"channel_type": int(channelType),
		"message_ids":  messageIDs,
	}, nil)
}
