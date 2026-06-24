package trigger

import "slices"

// DefaultClassifier is the production classifier — pure, deterministic.
//
// Rule precedence (top-down):
//  1. Cron — Source==SourceCron bypasses every gate (operator-trusted).
//  2. OBO trust + relevance — strip untrusted OBO; drop grantor-signed
//     fan-out that doesn't address the grantor (R10 leak guard).
//  3. DM — direct messages always trigger.
//  4. ExplicitBot — bot uid in the mention list, beats every group rule below.
//  5. PersonaHumans / PersonaGrantor — persona clone widens for @所有人 / @grantor.
//  6. ReplyToBot — quote-reply to one of the bot's own messages.
//  7. AIBroadcast — pure @AI, gated by Policy.AIBroadcast.
//  8. MentionFreeGroup — channel id on the mention-free allowlist.
//  9. Observation — fall-through for group messages.
type DefaultClassifier struct{}

// Classify applies the precedence-ordered rules above. MatchedRules
// records the firing rule label; Reason is the authoritative signal.
func (DefaultClassifier) Classify(in CanonicalInbound, p Policy) TriggerDecision {
	if in.Source == SourceCron {
		return TriggerDecision{
			Source:       SourceCron,
			Reason:       ReasonCron,
			MatchedRules: []string{"cron_fire"},
			ReplyRouting: cronReplyRouting(p),
		}
	}

	src := in.Source
	if src == "" {
		src = SourceUser
	}

	// OBO trust + relevance — evaluated up front so the reroute / drop
	// applies uniformly to DM and Group. A quote-reply to the bot IS
	// addressing intent (no explicit @ needed); the reply-to-bot rule
	// below handles its routing.
	oboReroute, oboTrusted := evaluateOBOTrust(in, p)
	if oboTrusted && !personaRelevant(in.Mention, p.Grantor) {
		quoteAddressesBot := p.ReplyToBotEnabled && in.ReplyTo != nil && in.ReplyTo.TargetIsBot
		if !quoteAddressesBot {
			return TriggerDecision{
				Source:       src,
				Reason:       ReasonOBOIrrelevant,
				MatchedRules: []string{"obo_v2_relevance_drop"},
			}
		}
	}

	// withRouting layers OBO reroute + grantor stamp onto an accepted
	// decision. Trusted OBO relays always stamp on_behalf_of.
	withRouting := func(r ReplyRouting) ReplyRouting {
		if oboReroute.HasOBOReroute() {
			r.OBORerouteChannelID = oboReroute.OBORerouteChannelID
			r.OBORerouteKind = oboReroute.OBORerouteKind
		}
		if oboTrusted && r.OnBehalfOf == "" {
			r.OnBehalfOf = p.Grantor.UID
		}
		return r
	}

	if in.Channel == ChannelDM {
		return TriggerDecision{
			Source:       src,
			Reason:       ReasonDM,
			MatchedRules: []string{"dm_auto"},
			ReplyRouting: withRouting(ReplyRouting{}),
		}
	}

	if in.Mention != nil && containsUID(in.Mention.UIDs, p.BotUID) {
		return TriggerDecision{
			Source:       src,
			Reason:       ReasonExplicitBot,
			MatchedRules: []string{"explicit_bot_uid"},
			ReplyRouting: withRouting(ReplyRouting{}),
		}
	}

	// Persona widening (clone only).
	if p.Grantor.Configured() && in.Mention != nil {
		if in.Mention.HumansFlag || in.Mention.AllFlag {
			return TriggerDecision{
				Source:       src,
				Reason:       ReasonPersonaHumans,
				MatchedRules: []string{"persona_humans"},
				ReplyRouting: withRouting(ReplyRouting{OnBehalfOf: p.Grantor.UID}),
			}
		}
		if containsUID(in.Mention.UIDs, p.Grantor.UID) {
			return TriggerDecision{
				Source:       src,
				Reason:       ReasonPersonaGrantor,
				MatchedRules: []string{"persona_grantor_uid"},
				ReplyRouting: withRouting(ReplyRouting{OnBehalfOf: p.Grantor.UID}),
			}
		}
	}

	// Reply-to-bot. Persona clones must keep speaking as the grantor in
	// a quote-thread, otherwise the persona identity breaks mid-conversation.
	if p.ReplyToBotEnabled && in.ReplyTo != nil && in.ReplyTo.TargetIsBot {
		routing := ReplyRouting{}
		if p.Grantor.Configured() {
			routing.OnBehalfOf = p.Grantor.UID
		}
		return TriggerDecision{
			Source:       src,
			Reason:       ReasonReplyToBot,
			MatchedRules: []string{"reply_to_bot"},
			ReplyRouting: withRouting(routing),
		}
	}

	// Pure @AI, gated.
	if in.Mention != nil && !in.Mention.AllFlag && !in.Mention.HumansFlag && in.Mention.AIsFlag {
		if reason, rule, ok := evaluateAIBroadcast(in.ChannelID, p); ok {
			return TriggerDecision{
				Source:       src,
				Reason:       reason,
				MatchedRules: []string{rule},
				ReplyRouting: withRouting(ReplyRouting{}),
			}
		}
	}

	if p.MentionFreeGroups[in.ChannelID] {
		return TriggerDecision{
			Source:       src,
			Reason:       ReasonMentionFreeGroup,
			MatchedRules: []string{"mention_free_group"},
			ReplyRouting: withRouting(ReplyRouting{}),
		}
	}

	// Fall-through: observation only.
	return TriggerDecision{
		Source:       src,
		Reason:       ReasonObservation,
		MatchedRules: []string{"no_match"},
		ReplyRouting: ReplyRouting{},
	}
}

func containsUID(uids []string, uid string) bool {
	if uid == "" {
		return false
	}
	return slices.Contains(uids, uid)
}

// cronReplyRouting stamps on_behalf_of on cron fires so a persona-clone
// cron speaks in the grantor's voice.
func cronReplyRouting(p Policy) ReplyRouting {
	if p.Grantor.Configured() {
		return ReplyRouting{OnBehalfOf: p.Grantor.UID}
	}
	return ReplyRouting{}
}

// evaluateAIBroadcast returns (reason, rule, accepted) for the @AI path.
// Unset/invalid policy defaults to Deny.
func evaluateAIBroadcast(channelID string, p Policy) (Reason, string, bool) {
	switch p.effectiveAIBroadcast() {
	case AIBroadcastAllow:
		return ReasonAIBroadcast, "ai_broadcast_allow", true
	case AIBroadcastAllowlist:
		if p.AIBroadcastAllowlist[channelID] {
			return ReasonAIBroadcast, "ai_broadcast_allowlist", true
		}
	}
	return ReasonNone, "", false
}
