import Foundation

/// Placeholder for the XClaw control-bus client.
///
/// This will speak the NDJSON-over-Unix-socket protocol defined in `proto/`:
/// connect to `xclawd`, receive `event` envelopes (driving an `AppState`
/// reducer), and send `command` envelopes (start/stop bots, inject secrets from
/// Keychain, request history). Modeled on Open Island's `BridgeTransport` /
/// `BridgeServer`, with roles reversed (here the Go core is the server).
public enum XClawCore {
    /// Protocol version this client speaks (matches `proto/README.md`).
    public static let protocolVersion = 1
}
