package octo

import "github.com/lml2468/octobuddy/core/trigger"

// TriggerMention projects this message's @-mention payload onto the
// IM-agnostic trigger.MentionPayload vocabulary the classifier operates
// on. The three-state bool|number wire flags are normalized to bool here.
// Returns nil when the wire payload has no mention struct at all (vs an
// empty payload — the classifier distinguishes the two).
func (m BotMessage) TriggerMention() *trigger.MentionPayload {
	if m.Payload.Mention == nil {
		return nil
	}
	return &trigger.MentionPayload{
		UIDs:       m.Payload.Mention.UIDs,
		AIsFlag:    truthy(m.Payload.Mention.AIs),
		HumansFlag: truthy(m.Payload.Mention.Humans),
		AllFlag:    truthy(m.Payload.Mention.All),
	}
}

// TriggerOBO projects this message's OBO v2 fields onto the IM-agnostic
// trigger.OBOSignal. Returns nil when no OBO origin is set (the common
// case — only persona relays carry OBO fields). The trust gate inside the
// classifier validates that FromUID matches the configured grantor; an
// untrusted signal is silently stripped.
func (m BotMessage) TriggerOBO() *trigger.OBOSignal {
	if m.Payload.OBOOriginChannelID == "" && oboRespondAs(m.Payload) == "" {
		return nil
	}
	channelTypeCode := 0
	if m.Payload.OBOOriginChannelType != nil {
		channelTypeCode = *m.Payload.OBOOriginChannelType
	}
	return &trigger.OBOSignal{
		OriginChannelID:   m.Payload.OBOOriginChannelID,
		OriginChannelType: channelTypeCode,
		OriginFromUID:     m.Payload.OBOOriginFromUID,
		RespondAs:         oboRespondAs(m.Payload),
	}
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
