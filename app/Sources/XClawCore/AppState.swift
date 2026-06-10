import Foundation

/// The client-side projection of control-bus events — the single source of
/// truth the GUI renders, updated only via `apply(_:)`. Multi-bot aware: events
/// are bucketed by botId (empty botId → the "default" bot in single-bot mode).
public struct AppState: Sendable, Equatable {
    public struct SessionView: Sendable, Equatable, Identifiable {
        public var sessionKey: String
        public var lastActivity: String = ""
        public var streamingText: String = ""   // accumulates text deltas this turn
        public var lastReply: String = ""        // assembled reply of the last turn
        public var lastTool: String = ""
        public var inputTokens: Int = 0
        public var outputTokens: Int = 0
        public var id: String { sessionKey }
    }

    public struct BotView: Sendable, Equatable, Identifiable {
        public var id: String
        public var connected: Bool = false
        public var lastError: String?
        public var sessions: [String: SessionView] = [:]

        public var sortedSessions: [SessionView] {
            sessions.values.sorted { $0.sessionKey < $1.sessionKey }
        }
    }

    public private(set) var bots: [String: BotView] = [:]
    public private(set) var connected: Bool = false

    public init() {}

    public var sortedBots: [BotView] { bots.values.sorted { $0.id < $1.id } }

    public mutating func setConnected(_ v: Bool) { connected = v }

    /// Replaces the bot roster from a bots.list response, preserving any
    /// already-accumulated session views.
    public mutating func setBots(_ infos: [BotInfo]) {
        for info in infos {
            var b = bots[info.id] ?? BotView(id: info.id)
            b.connected = info.connected
            b.lastError = info.lastError
            bots[info.id] = b
        }
    }

    /// Applies one decoded control-bus event envelope.
    public mutating func apply(_ env: Envelope) {
        guard env.kind == .event else { return }
        switch env.type {
        case "bot.status":
            if let info = env.decodeBody(BotInfo.self) {
                var b = bots[info.id] ?? BotView(id: info.id)
                b.connected = info.connected
                b.lastError = info.lastError
                bots[info.id] = b
            }
        case "session.text":
            if let x = env.decodeBody(SessionTextBody.self) {
                mutateSession(x.botId, x.sessionKey) { s in
                    s.streamingText += x.delta
                    s.lastActivity = "text"
                }
            }
        case "session.tool":
            if let x = env.decodeBody(SessionToolBody.self) {
                mutateSession(x.botId, x.sessionKey) { s in
                    s.lastTool = x.name
                    s.lastActivity = "tool:\(x.name)"
                }
            }
        case "session.usage":
            if let x = env.decodeBody(SessionUsageBody.self) {
                mutateSession(x.botId, x.sessionKey) { s in
                    s.inputTokens = x.inputTokens
                    s.outputTokens = x.outputTokens
                }
            }
        case "session.activity":
            if let x = env.decodeBody(SessionActivityBody.self) {
                mutateSession(x.botId, x.sessionKey) { s in
                    s.lastActivity = x.kind
                    if x.kind == "turnStart" { s.streamingText = "" }
                }
            }
        case "session.reply":
            if let x = env.decodeBody(SessionReplyBody.self) {
                mutateSession(x.botId, x.sessionKey) { s in
                    s.lastReply = x.text
                    s.lastActivity = "reply"
                }
            }
        case "error":
            if let x = env.decodeBody(ErrorBody.self) {
                let id = botKey(x.botId)
                var b = bots[id] ?? BotView(id: id)
                b.lastError = x.message
                bots[id] = b
            }
        default:
            break
        }
    }

    // botKey maps an empty botId (single-bot mode) to a stable "default" bucket.
    private func botKey(_ botId: String?) -> String {
        let id = botId ?? ""
        return id.isEmpty ? "default" : id
    }

    private mutating func mutateSession(_ botId: String?, _ sessionKey: String, _ f: (inout SessionView) -> Void) {
        let id = botKey(botId)
        var b = bots[id] ?? BotView(id: id)
        var s = b.sessions[sessionKey] ?? SessionView(sessionKey: sessionKey)
        f(&s)
        b.sessions[sessionKey] = s
        bots[id] = b
    }
}
