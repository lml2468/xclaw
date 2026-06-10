import Foundation

/// Read/write the daemon's single ~/.xclaw/config.json from the GUI, producing
/// JSON the Go `config.Load` parses. Every bot is inlined in the global config's
/// bots[] array (id + octoToken + agent overrides). The bot's persona/behavior
/// prompt is NOT here — it lives in SOUL.md / AGENTS.md under ~/.xclaw/<id>/.
///
/// Token is stored inline (plaintext, as today; Keychain is a later step).
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

    // Matches the Go slug rule: ^[A-Za-z0-9._-]+$, not "." or "..".
    public static func validSlug(_ id: String) -> Bool {
        guard !id.isEmpty, id != ".", id != ".." else { return false }
        return id.allSatisfy { c in
            c.isLetter && c.isASCII || c.isNumber && c.isASCII || c == "." || c == "_" || c == "-"
        }
    }

    // MARK: - JSON shapes (matching the Go config tags)

    private struct ConfigFile: Codable {
        var apiUrl: String?
        var bots: [Bot]?
        struct Bot: Codable {
            var id: String
            var octoToken: String?
            var apiUrl: String?
            var agent: Agent?
        }
        struct Agent: Codable {
            var gatewayBaseUrl: String?
            var gatewayToken: String?
            var env: [String: String]?
        }
    }

    // MARK: - load

    /// Loads the configured bots from `base` (default ~/.xclaw)'s single
    /// config.json. Returns [] if it doesn't exist.
    public static func load(base: URL? = nil) throws -> [BotConfig] {
        let base = base ?? baseDir
        guard let data = try? Data(contentsOf: globalConfigURL(base)) else { return [] }
        let file = (try? JSONDecoder().decode(ConfigFile.self, from: data)) ?? ConfigFile()
        return (file.bots ?? []).map { b in
            BotConfig(
                id: b.id,
                apiURL: b.apiUrl ?? file.apiUrl ?? "",
                octoToken: b.octoToken ?? "",
                gatewayBaseURL: b.agent?.gatewayBaseUrl ?? "",
                gatewayToken: b.agent?.gatewayToken ?? "",
                env: b.agent?.env ?? [:])
        }
    }

    // MARK: - save

    /// Persists the full bot list to `base` (default ~/.xclaw)'s single
    /// config.json: every bot inlined in bots[]. Validates slugs + uniqueness.
    /// Prunes per-bot directories whose bot was removed (keeps SOUL.md/AGENTS.md
    /// dirs of live bots intact).
    public static func save(_ bots: [BotConfig], base: URL? = nil) throws {
        let base = base ?? baseDir
        var seen = Set<String>()
        for b in bots {
            guard validSlug(b.id) else { throw ConfigError.invalidSlug(b.id) }
            guard seen.insert(b.id).inserted else { throw ConfigError.duplicateID(b.id) }
        }

        try mkdir(base)

        let file = ConfigFile(
            apiUrl: bots.first?.apiURL,
            bots: bots.map { b in
                ConfigFile.Bot(
                    id: b.id,
                    octoToken: b.octoToken.isEmpty ? nil : b.octoToken,
                    apiUrl: b.apiURL.isEmpty ? nil : b.apiURL,
                    agent: ConfigFile.Agent(
                        gatewayBaseUrl: b.gatewayBaseURL.isEmpty ? nil : b.gatewayBaseURL,
                        gatewayToken: b.gatewayToken.isEmpty ? nil : b.gatewayToken,
                        env: b.env.isEmpty ? nil : b.env))
            })
        try writeJSON(file, to: globalConfigURL(base))

        // Prune per-bot dirs for removed bots (only dirs that look like ours: a
        // child dir of base containing a data/ subdir or SOUL/AGENTS files).
        let live = Set(bots.map { $0.id })
        let fm = FileManager.default
        if let children = try? fm.contentsOfDirectory(at: base, includingPropertiesForKeys: [.isDirectoryKey]) {
            for child in children {
                let name = child.lastPathComponent
                var isDir: ObjCBool = false
                guard fm.fileExists(atPath: child.path, isDirectory: &isDir), isDir.boolValue else { continue }
                guard validSlug(name), !live.contains(name) else { continue }
                // Only prune dirs that look like a bot subtree (have data/).
                if fm.fileExists(atPath: child.appendingPathComponent("data").path) {
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
