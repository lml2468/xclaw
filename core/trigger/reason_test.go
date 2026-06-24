package trigger

import "testing"

// TestReasonIsAmbiguousAddressing pins the closed enum behind the bot-loop
// guard's scoping decision. Router/gate consults this method instead of
// pattern-matching the enum value; flipping a reason between ambiguous
// and unambiguous (or adding a new one) must be a one-line change here
// and gate.go remains untouched.
//
// Why this test exists: the bot-loop guard regressed once during the
// #105 refactor (a wider gate dropped legitimate peer-bot @-grantor
// mentions) and the fix bolted on a hard-coded `Reason ==
// ReasonMentionFreeGroup` check. #118 lifted that decision back to the
// trigger pkg — this test is the gate.
func TestReasonIsAmbiguousAddressing(t *testing.T) {
	// Ambiguous: classifier flagged this as a reply candidate but message
	// metadata can't tell us if the sender meant to address the bot.
	ambiguous := []Reason{ReasonMentionFreeGroup}
	for _, r := range ambiguous {
		if !r.IsAmbiguousAddressing() {
			t.Errorf("%q must be ambiguous-addressing (loop guard applies)", r)
		}
	}
	// Unambiguous: addressing intent is clear, peer-bot senders must
	// pass through. Includes ReasonNone (no classification → no scope
	// for the loop guard) and the post-classifier non-reply reasons
	// (observation / obo_irrelevant) which the router never sees but
	// must stay safely on the unambiguous side of any future caller.
	unambiguous := []Reason{
		ReasonNone,
		ReasonDM,
		ReasonExplicitBot,
		ReasonPersonaGrantor,
		ReasonPersonaHumans,
		ReasonReplyToBot,
		ReasonAIBroadcast,
		ReasonCron,
		ReasonObservation,
		ReasonOBOIrrelevant,
	}
	for _, r := range unambiguous {
		if r.IsAmbiguousAddressing() {
			t.Errorf("%q must NOT be ambiguous-addressing", r)
		}
	}
}
