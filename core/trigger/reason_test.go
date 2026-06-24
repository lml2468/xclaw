package trigger

import "testing"

// TestReasonIsAmbiguousAddressing pins the closed-enum partition behind
// the router's bot-loop-guard scope. Adding a new Reason without
// classifying it here fails loudly instead of silently defaulting
// through IsAmbiguousAddressing's `default: return false` branch.
func TestReasonIsAmbiguousAddressing(t *testing.T) {
	ambiguous := map[Reason]bool{ReasonMentionFreeGroup: true}
	for r := range ambiguous {
		if !r.IsAmbiguousAddressing() {
			t.Errorf("%q must be ambiguous-addressing (loop guard applies)", r)
		}
	}
	// Unambiguous covers the reply-warranting reasons + the post-
	// classifier non-reply reasons that never reach the router but must
	// stay safely classified.
	unambiguous := map[Reason]bool{
		ReasonNone:           true,
		ReasonDM:             true,
		ReasonExplicitBot:    true,
		ReasonPersonaGrantor: true,
		ReasonPersonaHumans:  true,
		ReasonReplyToBot:     true,
		ReasonAIBroadcast:    true,
		ReasonCron:           true,
		ReasonObservation:    true,
		ReasonOBOIrrelevant:  true,
	}
	for r := range unambiguous {
		if r.IsAmbiguousAddressing() {
			t.Errorf("%q must NOT be ambiguous-addressing", r)
		}
	}

	// Coverage closure: every Reason in AllReasons must appear in
	// exactly one partition.
	for _, r := range AllReasons {
		inA := ambiguous[r]
		inU := unambiguous[r]
		if inA == inU {
			t.Errorf("Reason %q must be in exactly one partition (ambiguous=%v unambiguous=%v); add it to one and to IsAmbiguousAddressing's switch", r, inA, inU)
		}
	}
	if len(ambiguous)+len(unambiguous) != len(AllReasons) {
		t.Errorf("partition coverage drift: ambiguous=%d unambiguous=%d AllReasons=%d", len(ambiguous), len(unambiguous), len(AllReasons))
	}
}
