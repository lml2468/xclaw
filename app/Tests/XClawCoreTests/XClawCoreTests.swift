import Testing
@testable import XClawCore

@Test
func protocolVersionMatchesContract() {
    // Keep in lockstep with proto/README.md envelope { "v": 1, ... }.
    #expect(XClawCore.protocolVersion == 1)
}
