package persona

import (
	"strings"
	"testing"
)

func TestBuildGroupSystemPrompt(t *testing.T) {
	g := Grantor{UID: "u_admin", Name: "Admin"}
	got := g.BuildGroupSystemPrompt()
	want := "你是Admin的AI分身（persona clone）。当群里有人@Admin或@所有人时，就是在叫你，你应当以Admin的身份回复，不要返回 NO_REPLY。"
	if got != want {
		t.Fatalf("BuildGroupSystemPrompt:\n got %q\nwant %q", got, want)
	}
}

func TestBuildGroupSystemPromptFallsBackToUID(t *testing.T) {
	g := Grantor{UID: "u_admin"} // no display name
	got := g.BuildGroupSystemPrompt()
	if got == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !contains(got, "你是u_admin的AI分身") {
		t.Fatalf("expected uid fallback in prompt, got %q", got)
	}
}

func TestBuildGroupSystemPromptNoGrantor(t *testing.T) {
	if got := (Grantor{}).BuildGroupSystemPrompt(); got != "" {
		t.Fatalf("expected empty prompt for non-clone, got %q", got)
	}
}

func TestComposeHint(t *testing.T) {
	g := Grantor{UID: "u_admin", Name: "Admin"}
	got := g.ComposeHint("Reply concisely.")
	want := "你正在以「Admin」的分身身份运作。请以 Admin 的身份回复。\n\nReply concisely."
	if got != want {
		t.Fatalf("ComposeHint:\n got %q\nwant %q", got, want)
	}
}

func TestComposeHintFallsBackToUID(t *testing.T) {
	g := Grantor{UID: "u_admin"}
	got := g.ComposeHint("p")
	if !contains(got, "「u_admin」") || !contains(got, "请以 u_admin 的身份") {
		t.Fatalf("expected uid fallback, got %q", got)
	}
}

func TestComposeHintEmptyPrompt(t *testing.T) {
	g := Grantor{UID: "u_admin", Name: "Admin"}
	if got := g.ComposeHint(""); got != "" {
		t.Fatalf("expected empty for empty prompt, got %q", got)
	}
	if got := g.ComposeHint("   "); got != "" {
		t.Fatalf("expected empty for whitespace prompt, got %q", got)
	}
}

func TestComposeHintNoGrantor(t *testing.T) {
	if got := (Grantor{}).ComposeHint("p"); got != "" {
		t.Fatalf("expected empty for non-clone, got %q", got)
	}
}

// TestRelevanceTruthTable is the relevance-filter truth table from the task spec
// and openclaw inbound.ts ~L2122-2160.
func TestRelevanceTruthTable(t *testing.T) {
	g := Grantor{UID: "u_admin", Name: "Admin"}
	cases := []struct {
		name string
		m    Mention
		want bool
	}{
		{"grantor @-mentioned → relevant", Mention{UIDs: []string{"u_admin"}}, true},
		{"grantor @ alongside @AI → relevant", Mention{UIDs: []string{"u_admin"}, AIs: true}, true},
		{"@所有人 (humans) → relevant", Mention{Humans: true}, true},
		{"@所有人 + ais (server rewrite) → relevant", Mention{Humans: true, AIs: true}, true},
		{"legacy @all → relevant", Mention{All: true}, true},
		{"no mention info → relevant (plain chatter)", Mention{}, true},
		{"pure @AI not addressing grantor → dropped", Mention{AIs: true}, false},
		{"@AI + other human (not grantor) → dropped", Mention{AIs: true, UIDs: []string{"u_other"}}, false},
		{"other human only (not grantor, no broadcast) → dropped", Mention{UIDs: []string{"u_other"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := g.Relevant(c.m); got != c.want {
				t.Fatalf("Relevant(%+v) = %v, want %v", c.m, got, c.want)
			}
		})
	}
}

func TestRelevanceNoGrantorAlwaysRelevant(t *testing.T) {
	g := Grantor{}
	// Even a pure @AI message is "relevant" for a non-clone — the normal mention
	// gate, not the persona filter, governs non-clone bots.
	if !g.Relevant(Mention{AIs: true}) {
		t.Fatal("non-clone should not be filtered by persona relevance")
	}
}

func TestTriggeredAsGrantor(t *testing.T) {
	g := Grantor{UID: "u_admin", Name: "Admin"}
	cases := []struct {
		name        string
		m           Mention
		explicitBot bool
		want        bool
	}{
		{"@所有人 (humans), not direct @bot → grantor voice", Mention{Humans: true}, false, true},
		{"legacy @all, not direct @bot → grantor voice", Mention{All: true}, false, true},
		{"grantor uid mention → grantor voice", Mention{UIDs: []string{"u_admin"}}, false, true},
		{"@所有人 but ALSO direct @bot → reply as self", Mention{Humans: true}, true, false},
		{"pure @AI → reply as self", Mention{AIs: true}, false, false},
		{"direct @bot only → reply as self", Mention{}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := g.TriggeredAsGrantor(c.m, c.explicitBot); got != c.want {
				t.Fatalf("TriggeredAsGrantor(%+v, explicitBot=%v) = %v, want %v", c.m, c.explicitBot, got, c.want)
			}
		})
	}
}

func TestTriggeredAsGrantorNoGrantor(t *testing.T) {
	if (Grantor{}).TriggeredAsGrantor(Mention{Humans: true}, false) {
		t.Fatal("non-clone is never triggered as grantor")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
