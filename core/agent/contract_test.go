package agent

import (
	"strings"
	"testing"
)

// contractDriver is one driver configuration under test. Each builds a turn's
// argv from a Request and exposes the system-prompt value it produced. Adding a
// second driver (Codex/Gemini) is a one-line append here — the contract assertion
// below then enforces the same Mandatory-first invariant on it for free.
type contractDriver struct {
	name     string
	newAgent func() *ClaudeDriver
}

func contractDrivers() []contractDriver {
	return []contractDriver{
		{
			name: "claude",
			newAgent: func() *ClaudeDriver {
				return newTestDriver() // probe pre-seeded
			},
		},
	}
}

// TestDriverHonorsMandatoryPrefix is the cross-driver security contract: every
// driver must place SystemPrompt.Mandatory (the SecurityPrefix) into the final
// system prompt AND keep it strictly before the operator-trusted Persona. This
// promotes the "non-overridable security prefix" guarantee from a comment into a
// test every driver must pass — the whole point of structuring SystemPrompt
// (a flattened blob made this unenforceable for a second driver).
func TestDriverHonorsMandatoryPrefix(t *testing.T) {
	const (
		mandatory = "SENTINEL_SECURITY_PREFIX"
		persona   = "SENTINEL_SOUL_PERSONA"
	)
	req := Request{
		Prompt: "hi",
		System: SystemPrompt{
			Mandatory: mandatory,
			Persona:   []string{persona},
		},
	}

	for _, cd := range contractDrivers() {
		t.Run(cd.name, func(t *testing.T) {
			args := cd.newAgent().buildArgs(req)
			sp, ok := systemPromptArg(args)
			if !ok {
				t.Fatalf("%s: no system-prompt flag emitted: %v", cd.name, args)
			}
			secIdx := strings.Index(sp, mandatory)
			soulIdx := strings.Index(sp, persona)
			if secIdx < 0 {
				t.Fatalf("%s: Mandatory (SecurityPrefix) missing from system prompt: %q", cd.name, sp)
			}
			if soulIdx < 0 {
				t.Fatalf("%s: Persona missing from system prompt: %q", cd.name, sp)
			}
			if secIdx >= soulIdx {
				t.Fatalf("%s: Mandatory must precede Persona (got Mandatory@%d, Persona@%d): %q",
					cd.name, secIdx, soulIdx, sp)
			}
		})
	}
}

// TestDriverMandatoryNotDisplacedByEmptyPersona pins that Mandatory survives even
// when there is no Persona/Background — a driver must not drop the SecurityPrefix
// just because it is the only segment.
func TestDriverMandatoryNotDisplacedByEmptyPersona(t *testing.T) {
	const mandatory = "SENTINEL_ONLY_SECURITY"
	req := Request{Prompt: "hi", System: SystemPrompt{Mandatory: mandatory}}

	for _, cd := range contractDrivers() {
		t.Run(cd.name, func(t *testing.T) {
			args := cd.newAgent().buildArgs(req)
			sp, ok := systemPromptArg(args)
			if !ok {
				t.Fatalf("%s: no system-prompt flag emitted: %v", cd.name, args)
			}
			if !strings.Contains(sp, mandatory) {
				t.Fatalf("%s: Mandatory dropped when it was the only segment: %q", cd.name, sp)
			}
		})
	}
}
