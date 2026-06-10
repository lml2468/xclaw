import Foundation

// Wire types for the XClaw control bus (see ../../proto/README.md and the Go
// `core/control` package). These mirror the Go envelope + typed bodies so both
// sides decode the same JSON.

public enum EnvelopeKind: String, Codable, Sendable {
    case command
    case response
    case event
}

/// One NDJSON line on the control bus. `body` is decoded lazily against the
/// concrete type named by `type`.
public struct Envelope: Codable, Sendable {
    public var v: Int
    public var kind: EnvelopeKind
    public var id: String?
    public var type: String
    public var ts: Int64?
    public var body: Data?

    public init(v: Int = ControlProtocol.version, kind: EnvelopeKind, id: String? = nil,
                type: String, ts: Int64? = nil, body: Data? = nil) {
        self.v = v
        self.kind = kind
        self.id = id
        self.type = type
        self.ts = ts
        self.body = body
    }

    private enum CodingKeys: String, CodingKey { case v, kind, id, type, ts, body }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        v = try c.decodeIfPresent(Int.self, forKey: .v) ?? ControlProtocol.version
        kind = try c.decode(EnvelopeKind.self, forKey: .kind)
        id = try c.decodeIfPresent(String.self, forKey: .id)
        type = try c.decode(String.self, forKey: .type)
        ts = try c.decodeIfPresent(Int64.self, forKey: .ts)
        // `body` is arbitrary JSON; capture it as raw Data for later typed decode.
        if c.contains(.body), let raw = try? c.decode(JSONValue.self, forKey: .body) {
            body = try? JSONEncoder().encode(raw)
        } else {
            body = nil
        }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(v, forKey: .v)
        try c.encode(kind, forKey: .kind)
        try c.encodeIfPresent(id, forKey: .id)
        try c.encode(type, forKey: .type)
        try c.encodeIfPresent(ts, forKey: .ts)
        if let body, let raw = try? JSONDecoder().decode(JSONValue.self, from: body) {
            try c.encode(raw, forKey: .body)
        }
    }

    /// Decodes the `body` payload as a concrete Codable type.
    public func decodeBody<T: Decodable>(_ type: T.Type) -> T? {
        guard let body else { return nil }
        return try? JSONDecoder().decode(T.self, from: body)
    }
}

public enum ControlProtocol {
    public static let version = 1
}

// MARK: - Command bodies (client → server)

public struct SessionSendBody: Codable, Sendable {
    public var botId: String?
    public var uid: String
    public var text: String
    public init(uid: String, text: String, botId: String? = nil) {
        self.uid = uid; self.text = text; self.botId = botId
    }
}

public struct SessionHistoryBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var limit: Int
    public init(sessionKey: String, limit: Int = 40, botId: String? = nil) {
        self.sessionKey = sessionKey; self.limit = limit; self.botId = botId
    }
}

// MARK: - Event / response bodies (server → client)

public struct OKBody: Codable, Sendable { public var ok: Bool }

public struct HealthBody: Codable, Sendable {
    public var uptime: Int64
    public var connections: Int?
    public var driver: String?
    public var bots: Int?
}

public struct BotInfo: Codable, Sendable, Identifiable {
    public var id: String
    public var driver: String?
    public var connected: Bool
    public var lastError: String?
}

public struct SessionTextBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var delta: String
}

public struct SessionToolBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var name: String
    public var params: String
}

public struct SessionUsageBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var inputTokens: Int
    public var outputTokens: Int
}

public struct SessionReplyBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var text: String
}

public struct SessionActivityBody: Codable, Sendable {
    public var botId: String?
    public var sessionKey: String
    public var kind: String
}

public struct ErrorBody: Codable, Sendable {
    public var botId: String?
    public var scope: String
    public var message: String
    public var recoverable: Bool?
}

public struct HistoryMessage: Codable, Sendable {
    public var role: String
    public var content: String
    public var ts: Int64
}
