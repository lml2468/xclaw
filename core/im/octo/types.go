// Package octo implements the Octo IM connector: the WuKongIM binary protocol
// (WebSocket) plus the Octo REST API, ported wire-compatibly from
// cc-channel-octo's src/octo. It produces router.InboundMessage values for the
// gateway and delivers replies via REST sendMessage.
package octo

import "github.com/lml2468/xclaw/core/persona"

// ChannelType mirrors Octo's channel kind (types.ts ChannelType).
type ChannelType int

const (
	ChannelDM             ChannelType = 1
	ChannelGroup          ChannelType = 2
	ChannelCommunityTopic ChannelType = 5
)

// MessageType mirrors Octo's payload type enum (types.ts MessageType).
type MessageType int

const (
	MsgText  MessageType = 1
	MsgImage MessageType = 2
)

// Mention is the @-mention payload (types.ts MentionPayload). Only the fields
// the connector needs are modeled; humans/ais/all are three-state ints.
type Mention struct {
	UIDs   []string `json:"uids,omitempty"`
	All    any      `json:"all,omitempty"`    // legacy @all (bool|number)
	Humans any      `json:"humans,omitempty"` // @all-humans (bool|number)
	AIs    any      `json:"ais,omitempty"`    // @all-AIs (bool|number)
}

// MessagePayload is the decrypted RECV payload JSON (types.ts MessagePayload).
type MessagePayload struct {
	Type    MessageType `json:"type"`
	Content string      `json:"content,omitempty"`
	URL     string      `json:"url,omitempty"`
	Name    string      `json:"name,omitempty"`
	Mention *Mention    `json:"mention,omitempty"`

	// OBO v2 fan-out fields (openclaw inbound.ts ~L2102). Present only on
	// grantor-relayed fan-out messages; the connector trusts them ONLY when the
	// message is sent by the configured grantor (security gate). All optional.
	OBOOriginChannelID   string `json:"obo_origin_channel_id,omitempty"`
	OBOOriginChannelType *int   `json:"obo_origin_channel_type,omitempty"`
	OBOOriginFromUID     string `json:"obo_origin_from_uid,omitempty"`
	OBORespondAs         string `json:"obo_respond_as,omitempty"`
	OBOGrantorUID        string `json:"obo_grantor_uid,omitempty"`
	OBOSystemHint        string `json:"obo_system_hint,omitempty"`
}

// BotMessage is one inbound message decoded from a RECV packet (types.ts
// BotMessage). message_id is a decimal string (int64 precision).
type BotMessage struct {
	MessageID   string
	MessageSeq  uint32
	FromUID     string
	FromName    string
	ChannelID   string
	ChannelType ChannelType
	Timestamp   uint32
	Payload     MessagePayload
	StreamOn    bool
}

// mentionsBot reports whether this message @-mentions the given bot uid, or
// addresses all AIs. Mirrors session-router's group mention gate (uids or ais,
// NOT humans-only @all).
func (m BotMessage) MentionsBot(botUID string) bool {
	if m.Payload.Mention == nil {
		return false
	}
	for _, u := range m.Payload.Mention.UIDs {
		if u == botUID {
			return true
		}
	}
	return truthy(m.Payload.Mention.AIs)
}

// PersonaMention projects this message's @-mention payload onto the IM-agnostic
// persona.Mention vocabulary the persona package operates on. The three-state
// bool|number wire flags are normalized to bool here.
func (m BotMessage) PersonaMention() persona.Mention {
	if m.Payload.Mention == nil {
		return persona.Mention{}
	}
	return persona.Mention{
		UIDs:   m.Payload.Mention.UIDs,
		AIs:    truthy(m.Payload.Mention.AIs),
		Humans: truthy(m.Payload.Mention.Humans),
		All:    truthy(m.Payload.Mention.All),
	}
}

// ExplicitlyMentionsBot reports whether the bot's own uid is in the explicit
// @-mention uid list (NOT @AI / @所有人). Mirrors openclaw inbound.ts
// `isExplicitBotMention`; a direct @bot mention makes a persona clone reply as
// itself rather than as the grantor.
func (m BotMessage) ExplicitlyMentionsBot(botUID string) bool {
	if m.Payload.Mention == nil {
		return false
	}
	for _, u := range m.Payload.Mention.UIDs {
		if u == botUID {
			return true
		}
	}
	return false
}

// Triggers reports whether this message should trigger a turn for the bot,
// accounting for persona-clone semantics. Mirrors openclaw inbound.ts
// `isMentioned` (~L1701-1705):
//
//   - explicit @bot uid → always;
//   - pure @AI (ais=1, no broadcast flags) → yes; @AI suppressed under a
//     broadcast (the server rewrites @所有人 to also set ais=1);
//   - @所有人 (humans=1) → only persona clones (a human is part of the
//     broadcast, the clone acts for them);
//   - grantor @-mentioned → persona clones (targets the grantor identity).
//
// For a non-clone (zero grantor) this collapses to the plain mention gate.
func (m BotMessage) Triggers(botUID string, grantor persona.Grantor) bool {
	if m.Payload.Mention == nil {
		return false
	}
	pm := m.PersonaMention()
	isBroadcast := pm.All || pm.Humans
	if !isBroadcast && pm.AIs {
		return true
	}
	if m.ExplicitlyMentionsBot(botUID) {
		return true
	}
	if grantor.Configured() {
		if pm.Humans {
			return true
		}
		for _, u := range pm.UIDs {
			if u == grantor.UID {
				return true
			}
		}
	}
	return false
}

// truthy interprets Octo's three-state bool|number fields.
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}
