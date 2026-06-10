import Testing
import Foundation
@testable import XClawCore

/// Keychain round-trip. The macOS Keychain may be unavailable in a headless CI
/// environment (no unlocked login keychain) — in that case `set` throws and the
/// test returns early (treated as skipped) rather than failing.
@Test
func keychainRoundTrip() throws {
    let acct = "xclaw-test/\(UUID().uuidString)"
    do {
        try Keychain.set(account: acct, value: "secret-123")
    } catch {
        return // Keychain not available here; skip.
    }
    defer { try? Keychain.delete(account: acct) }

    #expect(Keychain.get(account: acct) == "secret-123")

    // Update in place.
    try Keychain.set(account: acct, value: "secret-456")
    #expect(Keychain.get(account: acct) == "secret-456")

    // Empty value removes the item.
    try Keychain.set(account: acct, value: "")
    #expect(Keychain.get(account: acct) == nil)
}

@Test
func keychainAccountFormat() {
    #expect(Keychain.account(bot: "alpha", kind: Keychain.kindOcto) == "alpha/octoToken")
    #expect(Keychain.account(bot: "beta", kind: Keychain.kindGateway) == "beta/gatewayToken")
}
