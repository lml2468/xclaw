package trigger

import "github.com/lml2468/octobuddy/core/persona"

// personaGrantorFixture is a shared test helper for the trigger tests. Kept
// in its own file so default_test.go doesn't have to import core/persona,
// and so future tests can share a single canonical fixture.
func personaGrantorFixture() persona.Grantor {
	return persona.Grantor{UID: "u_admin", Name: "Admin"}
}
