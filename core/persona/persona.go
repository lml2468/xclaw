// Package persona implements OBO ("on behalf of") persona-clone semantics,
// ported from openclaw-channel-octo (NOT present in cc-channel-octo). The
// openclaw sources mirrored here are src/persona-prompt.ts and src/inbound.ts
// (buildPersonaGroupSystemPrompt + the OBO v2 relevance filter / trigger gate).
//
// A persona clone is a bot account configured with a grantor (`onBehalfOf`):
// it replies in the grantor's voice. The clone is triggered not only when the
// bot itself is @-mentioned, but also when the grantor is @-mentioned or the
// group is broadcast-addressed (@所有人), because the bot acts on the grantor's
// behalf. When triggered as the grantor's proxy, the reply is routed back to
// the origin channel with an `on_behalf_of` header so the server presents it
// as the grantor speaking.
//
// This package owns the pure, IM-agnostic semantics so the gateway (prompt
// injection) and the Octo connector (trigger gating + reply routing) carry only
// thin hooks:
//
//   - BuildGroupSystemPrompt — mirrors openclaw inbound.ts
//     buildPersonaGroupSystemPrompt (GH octo-adapters#64): the
//     operator-trusted persona instruction injected into the system prompt so
//     the LLM understands it is the grantor's clone and must not return
//     NO_REPLY when it sees `@grantor`.
//   - ComposeHint — mirrors openclaw persona-prompt.ts composePersonaHint:
//     the channel-agnostic variant of octo-server's buildFanoutCopyReq prefix,
//     used when a free-form persona prompt is supplied.
//   - Relevant / Mention — mirrors openclaw inbound.ts OBO v2 relevance filter
//     (~L2122-2160): drops pure @AI fan-out that does not address the grantor
//     so an irrelevant message never leaks into the clone's session.
//   - TriggeredAsGrantor — mirrors openclaw inbound.ts (~L1707-1713): decides
//     whether the reply is sent in the grantor's voice (on_behalf_of) vs. as
//     the bot itself.
//
// All strings are produced for an operator-trusted code path; the grantor uid
// and display name come from operator config (config.OnBehalfOf), never from
// untrusted message payloads. The caller wraps the produced text through
// safety.TrustedText when injecting it into the system prompt.
package persona

import "strings"

// Grantor identifies the human a persona clone speaks for. UID is the
// server-authoritative identity used for the security gate (only the configured
// grantor may sign OBO fields); Name is the display name woven into the persona
// instruction. Both come from operator config, never from untrusted payloads.
type Grantor struct {
	UID  string
	Name string
}

// Configured reports whether a grantor uid is set — i.e. the bot is a persona
// clone. A clone with a uid but no display name falls back to the uid for the
// human-readable parts (mirrors openclaw, which uses `grantorName || grantorUid`).
func (g Grantor) Configured() bool { return strings.TrimSpace(g.UID) != "" }

// displayName returns the name to weave into prompts, falling back to the uid
// when no display name is configured (openclaw: `uidToNameMap.get(uid) || uid`).
func (g Grantor) displayName() string {
	if n := strings.TrimSpace(g.Name); n != "" {
		return n
	}
	return strings.TrimSpace(g.UID)
}

// BuildGroupSystemPrompt builds the group-path persona-clone system instruction.
//
// Mirrors openclaw inbound.ts buildPersonaGroupSystemPrompt (GH
// octo-adapters#64): when the clone and its grantor are both in a group, the
// message arrives as a normal group event and the LLM sees the raw `@grantor`
// text — without this hint it concludes "not addressed to me" → NO_REPLY. The
// hint tells it that an @<grantor> / @所有人 mention is a call to it, to be
// answered in the grantor's voice.
//
// Returns "" when no grantor is configured.
func (g Grantor) BuildGroupSystemPrompt() string {
	if !g.Configured() {
		return ""
	}
	name := g.displayName()
	return "你是" + name + "的AI分身（persona clone）。当群里有人@" + name +
		"或@所有人时，就是在叫你，你应当以" + name + "的身份回复，不要返回 NO_REPLY。"
}

// ComposeHint composes the channel-agnostic persona hint from a free-form
// persona prompt supplied for the grantor.
//
// Mirrors openclaw persona-prompt.ts composePersonaHint: the buildFanoutCopyReq
// (octo-server obo_fanout.go) prefix without the per-message origin context
// (which is unavailable at prompt-build time). Returns "" when there is no
// usable persona content (empty prompt or no grantor identity).
func (g Grantor) ComposeHint(personaPrompt string) string {
	if !g.Configured() {
		return ""
	}
	prompt := strings.TrimSpace(personaPrompt)
	if prompt == "" {
		return ""
	}
	name := g.displayName()
	if name == "" {
		return ""
	}
	return "你正在以「" + name + "」的分身身份运作。请以 " + name + " 的身份回复。\n\n" + prompt
}

// Mention is the per-message @-mention summary the relevance filter and trigger
// gate operate on. The three broadcast flags are three-state in the wire
// payload (bool|number) — callers normalize to bool before constructing this.
type Mention struct {
	// UIDs are explicitly @-mentioned user uids.
	UIDs []string
	// AIs is @AI / @所有AI (target AI bots directly).
	AIs bool
	// Humans is @所有人 (Plan X) — a human broadcast the grantor is part of.
	Humans bool
	// All is the legacy @所有人 (bool|number 1).
	All bool
}

// mentionsUID reports whether uid is in the explicit mention list.
func (m Mention) mentionsUID(uid string) bool {
	if uid == "" {
		return false
	}
	for _, u := range m.UIDs {
		if u == uid {
			return true
		}
	}
	return false
}

// empty reports whether the mention carries no information at all.
func (m Mention) empty() bool {
	return !m.AIs && !m.Humans && !m.All && len(m.UIDs) == 0
}

// Relevant reports whether an OBO message is relevant to the persona clone and
// should be processed.
//
// Mirrors openclaw inbound.ts OBO v2 relevance filter (~L2122-2160):
//
//   - broadcast (@所有人 humans/all) → relevant (the grantor, a human, is part
//     of the broadcast);
//   - explicit grantor uid mention → relevant (targets the grantor identity);
//   - no mention info at all → relevant (plain chatter the persona should see);
//   - pure @AI fan-out not addressing the grantor → NOT relevant (dropped).
//
// The caller must drop a not-relevant message BEFORE recording any session
// state, so an irrelevant fan-out never leaks into the clone's session (the
// openclaw R10 ordering invariant).
func (g Grantor) Relevant(m Mention) bool {
	if !g.Configured() {
		// No grantor → no persona gating; the normal mention gate already
		// decided this message is worth processing.
		return true
	}
	broadcastRelevant := m.Humans || m.All
	grantorInUids := m.mentionsUID(g.UID)
	noMentionFallback := m.empty()
	return broadcastRelevant || grantorInUids || noMentionFallback
}

// TriggeredAsGrantor reports whether the clone was triggered as the grantor's
// proxy (and so should reply in the grantor's voice, with on_behalf_of), as
// opposed to being addressed directly as itself.
//
// Mirrors openclaw inbound.ts (~L1707-1713): triggered-as-grantor when a human
// broadcast (@所有人 / legacy @all) OR an explicit grantor-uid mention is
// present, the bot is a persona clone, and the bot was NOT explicitly
// @-mentioned by its own uid (a direct @bot mention always replies as itself).
func (g Grantor) TriggeredAsGrantor(m Mention, explicitBotMention bool) bool {
	if !g.Configured() || explicitBotMention {
		return false
	}
	humanBroadcast := m.Humans || m.All
	grantorMentioned := m.mentionsUID(g.UID)
	return humanBroadcast || grantorMentioned
}
