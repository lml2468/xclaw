import Foundation

/// Read/write the daemon's two-layer bot-first config (~/.xclaw) from the GUI,
/// producing JSON the Go `config.Load` parses. Layout:
///   ~/.xclaw/config.json        global: apiUrl + bots:[{id}]
///   ~/.xclaw/<id>/config.json   per-bot: octoToken + overrides
///
/// This is the writable subset of the Go config — enough to add/remove bots and
/// edit id / apiUrl / token from the UI. Token is stored in the per-bot file
/// (plaintext, as today; Keychain is a later step).
public struct BotConfig: Sendable, Equatable, Identifiable {
    public var id: String
    public var apiURL: String
    public var octoToken: String
    /// Model-gateway routing. The Go core maps these to the claude env var
    /// names (ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN).
    public var gatewayBaseURL: String
    public var gatewayToken: String
    /// Arbitrary extra environment variables injected into the agent CLI
    /// (e.g. OCTO_BOT_ID, GH_TOKEN, GLAB_TOKEN).
    public var env: [String: String]

    public init(id: String, apiURL: String = "",
                octoToken: String = "", gatewayBaseURL: String = "",
                gatewayToken: String = "", env: [String: String] = [:]) {
        self.id = id
        self.apiURL = apiURL
        self.octoToken = octoToken
        self.gatewayBaseURL = gatewayBaseURL
        self.gatewayToken = gatewayToken
        self.env = env
    }
}

public enum ConfigStore {
    public enum ConfigError: Error, CustomStringConvertible {
        case invalidSlug(String)
        case duplicateID(String)
        case io(String)
        public var description: String {
            switch self {
            case .invalidSlug(let s): return "invalid bot id \"\(s)\": use letters, digits, dot, underscore, hyphen (no path separators)"
            case .duplicateID(let s): return "duplicate bot id \"\(s)\""
            case .io(let s): return s
            }
        }
    }

    /// Default ~/.xclaw directory.
    public static var baseDir: URL {
        URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent(".xclaw", isDirectory: true)
    }
    static func globalConfigURL(_ base: URL) -> URL { base.appendingPathComponent("config.json") }
    static func botConfigURL(_ base: URL, _ id: String) -> URL {
        base.appendingPathComponent(id, isDirectory: true).appendingPathComponent("config.json")
    }

    // Matches the Go slug rule: ^[A-Za-z0-9._-]+$, not "." or "..".
    public static func validSlug(_ id: String) -> Bool {
        guard !id.isEmpty, id != ".", id != ".." else { return false }
        return id.allSatisfy { c in
            c.isLetter && c.isASCII || c.isNumber && c.isASCII || c == "." || c == "_" || c == "-"
        }
    }

    // MARK: - JSON shapes (minimal, matching Go config tags)

    private struct GlobalFile: Codable {
        var apiUrl: String?
        var bots: [BotEntry]?
        struct BotEntry: Codable { var id: String }
    }
    private struct BotFile: Codable {
        var octoToken: String?
        var apiUrl: String?
        var sdk: SDK?
        struct SDK: Codable {
            var gatewayBaseUrl: String?
            var gatewayToken: String?
            var env: [String: String]?
        }
    }

    // MARK: - load

    /// Loads the configured bots from `base` (default ~/.xclaw), merging the
    /// global apiUrl default with each per-bot file. Returns [] if no global
    /// config exists.
    public static func load(base: URL? = nil) throws -> [BotConfig] {
        let base = base ?? baseDir
        guard let gdata = try? Data(contentsOf: globalConfigURL(base)) else { return [] }
        let global = (try? JSONDecoder().decode(GlobalFile.self, from: gdata)) ?? GlobalFile()
        let entries = global.bots ?? []
        var out: [BotConfig] = []
        for e in entries {
            var bc = BotConfig(id: e.id, apiURL: global.apiUrl ?? "")
            if let bdata = try? Data(contentsOf: botConfigURL(base, e.id)),
               let bf = try? JSONDecoder().decode(BotFile.self, from: bdata) {
                bc.octoToken = bf.octoToken ?? ""
                if let u = bf.apiUrl, !u.isEmpty { bc.apiURL = u }
                bc.gatewayBaseURL = bf.sdk?.gatewayBaseUrl ?? ""
                bc.gatewayToken = bf.sdk?.gatewayToken ?? ""
                bc.env = bf.sdk?.env ?? [:]
            }
            out.append(bc)
        }
        return out
    }

    // MARK: - save

    /// Persists the full bot list to `base` (default ~/.xclaw): rewrites the
    /// global config (apiUrl + bots[]) and each per-bot config (token +
    /// overrides). Validates slugs + uniqueness. Removes per-bot directories for
    /// bots no longer present.
    public static func save(_ bots: [BotConfig], base: URL? = nil) throws {
        let base = base ?? baseDir
        var seen = Set<String>()
        for b in bots {
            guard validSlug(b.id) else { throw ConfigError.invalidSlug(b.id) }
            guard seen.insert(b.id).inserted else { throw ConfigError.duplicateID(b.id) }
        }

        let fm = FileManager.default
        try mkdir(base)

        // Global config: shared apiUrl taken from the first bot (the per-bot
        // files carry the authoritative per-bot values too).
        let shared = bots.first
        let global = GlobalFile(
            apiUrl: shared?.apiURL,
            bots: bots.map { GlobalFile.BotEntry(id: $0.id) }
        )
        try writeJSON(global, to: globalConfigURL(base))

        // Per-bot files.
        var liveIDs = Set<String>()
        for b in bots {
            liveIDs.insert(b.id)
            try mkdir(base.appendingPathComponent(b.id, isDirectory: true))
            let bf = BotFile(octoToken: b.octoToken,
                             apiUrl: b.apiURL,
                             sdk: BotFile.SDK(
                                gatewayBaseUrl: b.gatewayBaseURL.isEmpty ? nil : b.gatewayBaseURL,
                                gatewayToken: b.gatewayToken.isEmpty ? nil : b.gatewayToken,
                                env: b.env.isEmpty ? nil : b.env))
            try writeJSON(bf, to: botConfigURL(base, b.id))
        }

        // Prune per-bot dirs for removed bots (only dirs that look like ours: a
        // child dir of base containing a config.json).
        if let children = try? fm.contentsOfDirectory(at: base, includingPropertiesForKeys: [.isDirectoryKey]) {
            for child in children {
                let name = child.lastPathComponent
                var isDir: ObjCBool = false
                guard fm.fileExists(atPath: child.path, isDirectory: &isDir), isDir.boolValue else { continue }
                guard validSlug(name), !liveIDs.contains(name) else { continue }
                if fm.fileExists(atPath: botConfigURL(base, name).path) {
                    try? fm.removeItem(at: child)
                }
            }
        }
    }

    // MARK: - helpers

    private static func mkdir(_ url: URL) throws {
        do { try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true) }
        catch { throw ConfigError.io("mkdir \(url.path): \(error.localizedDescription)") }
    }

    private static func writeJSON<T: Encodable>(_ v: T, to url: URL) throws {
        let enc = JSONEncoder()
        enc.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        do {
            let data = try enc.encode(v)
            try data.write(to: url, options: .atomic)
        } catch {
            throw ConfigError.io("write \(url.path): \(error.localizedDescription)")
        }
    }
}
