import Foundation

/// XClaw control-bus client namespace. The concrete pieces live in:
/// - `Envelope` / `JSONValue` — wire types (mirror Go `core/control`)
/// - `ControlCodec` / `LineFramer` — NDJSON encode/decode + framing
/// - `ControlClient` — Unix-socket client + inbound event stream
/// - `AppState` — single-source-of-truth reducer for the GUI
public enum XClawCore {
    /// Protocol version this client speaks (matches `proto/README.md`).
    public static let protocolVersion = ControlProtocol.version
}
