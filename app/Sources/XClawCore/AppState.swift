import Foundation

/// The client-side projection of control-bus events — the single source of
/// truth the GUI renders, updated only via `apply(_:)`. Mirrors Open Island's
/// SessionState.apply reducer discipline.
public struct AppState: Sendable, Equatable {
    public struct SessionView: Sendable, Equatable {
        public var sessionKey: String
        public var lastActivity: String = ""
        public var streamingText: String = ""   // accumulates text deltas this turn
        public var lastReply: String = ""        // assembled reply of the last turn
        public var lastTool: String = ""
        public var inputTokens: Int = 0
        public var outputTokens: Int = 0
    }

    public private(set) var sessions: [String: SessionView] = [:]
    public private(set) var lastError: String?
    public private(set) var connected: Bool = false

    public init() {}

    public mutating func setConnected(_ v: Bool) { connected = v }

    /// Applies one decoded control-bus event envelope.
    public mutating func apply(_ env: Envelope) {
        guard env.kind == .event else { return }
        switch env.type {
        case "session.text":
            if let b = env.decodeBody(SessionTextBody.self) {
                var s = session(b.sessionKey)
                s.streamingText += b.delta
                s.lastActivity = "text"
                sessions[b.sessionKey] = s
            }
        case "session.tool":
            if let b = env.decodeBody(SessionToolBody.self) {
                var s = session(b.sessionKey)
                s.lastTool = b.name
                s.lastActivity = "tool:\(b.name)"
                sessions[b.sessionKey] = s
            }
        case "session.usage":
            if let b = env.decodeBody(SessionUsageBody.self) {
                var s = session(b.sessionKey)
                s.inputTokens = b.inputTokens
                s.outputTokens = b.outputTokens
                sessions[b.sessionKey] = s
            }
        case "session.activity":
            if let b = env.decodeBody(SessionActivityBody.self) {
                var s = session(b.sessionKey)
                s.lastActivity = b.kind
                if b.kind == "turnStart" {
                    s.streamingText = ""   // new turn: reset the streaming buffer
                }
                sessions[b.sessionKey] = s
            }
        case "session.reply":
            if let b = env.decodeBody(SessionReplyBody.self) {
                var s = session(b.sessionKey)
                s.lastReply = b.text
                s.lastActivity = "reply"
                sessions[b.sessionKey] = s
            }
        case "error":
            if let b = env.decodeBody(ErrorBody.self) {
                lastError = b.message
            }
        default:
            break
        }
    }

    private func session(_ key: String) -> SessionView {
        sessions[key] ?? SessionView(sessionKey: key)
    }
}
