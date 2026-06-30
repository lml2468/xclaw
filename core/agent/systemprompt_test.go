package agent

import "testing"

// TestSystemPromptFlattenOrder pins the Mandatory → Persona → Background order
// and that empty segments are skipped (no leading/trailing/double blank lines).
func TestSystemPromptFlattenOrder(t *testing.T) {
	sp := SystemPrompt{
		Mandatory:  "SEC",
		Persona:    []string{"SOUL", "", "ROSTER"},
		Background: []string{"HANDBOOK"},
	}
	got := sp.Flatten()
	want := "SEC\n\nSOUL\n\nROSTER\n\nHANDBOOK"
	if got != want {
		t.Fatalf("Flatten order/skip wrong:\n got %q\nwant %q", got, want)
	}
}

// TestSystemPromptFlattenEmpty pins that a zero value flattens to "".
func TestSystemPromptFlattenEmpty(t *testing.T) {
	if got := (SystemPrompt{}).Flatten(); got != "" {
		t.Fatalf("empty Flatten = %q, want \"\"", got)
	}
	if got := (SystemPrompt{Persona: []string{"", ""}}).Flatten(); got != "" {
		t.Fatalf("all-empty Persona Flatten = %q, want \"\"", got)
	}
}

// TestSystemPromptIsZero pins IsZero across the segment combinations.
func TestSystemPromptIsZero(t *testing.T) {
	cases := []struct {
		name string
		sp   SystemPrompt
		zero bool
	}{
		{"empty", SystemPrompt{}, true},
		{"empty persona slice", SystemPrompt{Persona: []string{"", ""}}, true},
		{"mandatory only", SystemPrompt{Mandatory: "X"}, false},
		{"persona only", SystemPrompt{Persona: []string{"X"}}, false},
		{"background only", SystemPrompt{Background: []string{"X"}}, false},
	}
	for _, tc := range cases {
		if got := tc.sp.IsZero(); got != tc.zero {
			t.Errorf("%s: IsZero = %v, want %v", tc.name, got, tc.zero)
		}
	}
}

// TestRequestSysPromptPrecedence pins that structured System wins over the legacy
// flat SystemPrompt, and the flat field is the fallback when System is zero.
func TestRequestSysPromptPrecedence(t *testing.T) {
	// System set → structured wins, flat ignored.
	r := Request{SystemPrompt: "FLAT", System: SystemPrompt{Mandatory: "STRUCT"}}
	if got := r.sysPrompt(); got != "STRUCT" {
		t.Fatalf("System should win: got %q", got)
	}
	// System zero → fall back to flat.
	r = Request{SystemPrompt: "FLAT"}
	if got := r.sysPrompt(); got != "FLAT" {
		t.Fatalf("zero System should fall back to flat: got %q", got)
	}
	// Both empty → "".
	if got := (Request{}).sysPrompt(); got != "" {
		t.Fatalf("both empty should be \"\": got %q", got)
	}
}
