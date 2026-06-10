import Foundation
import Security

/// Thin wrapper over the macOS Keychain (generic-password items) for storing bot
/// secrets, keyed by `service` + `account`. Accounts are `"<botId>/<kind>"`,
/// where kind is `octoToken` / `gatewayToken` (matching the core's secret.inject
/// kinds). Values live only in the Keychain — never in config.json.
public enum Keychain {
    public static let service = "com.xclaw.tokens"

    // Secret kinds — must match the Go core's secret.inject kinds.
    public static let kindOcto = "octoToken"
    public static let kindGateway = "gatewayToken"

    public enum KeychainError: Error, CustomStringConvertible {
        case status(OSStatus)
        public var description: String {
            switch self {
            case .status(let s):
                let msg = SecCopyErrorMessageString(s, nil) as String? ?? "OSStatus \(s)"
                return "keychain error: \(msg) (\(s))"
            }
        }
    }

    public static func account(bot: String, kind: String) -> String { "\(bot)/\(kind)" }

    private static func baseQuery(_ account: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    /// Stores `value` for `account`, creating or updating the item. An empty
    /// value deletes the item (so clearing a field removes the secret).
    public static func set(account: String, value: String) throws {
        if value.isEmpty {
            try delete(account: account)
            return
        }
        let data = Data(value.utf8)
        let update = SecItemUpdate(baseQuery(account) as CFDictionary,
                                   [kSecValueData as String: data] as CFDictionary)
        switch update {
        case errSecSuccess:
            return
        case errSecItemNotFound:
            var add = baseQuery(account)
            add[kSecValueData as String] = data
            let status = SecItemAdd(add as CFDictionary, nil)
            if status != errSecSuccess { throw KeychainError.status(status) }
        default:
            throw KeychainError.status(update)
        }
    }

    /// Returns the stored value for `account`, or nil if absent/unreadable.
    public static func get(account: String) -> String? {
        var query = baseQuery(account)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &out) == errSecSuccess,
              let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// Removes the item for `account`. A missing item is not an error.
    public static func delete(account: String) throws {
        let status = SecItemDelete(baseQuery(account) as CFDictionary)
        if status != errSecSuccess && status != errSecItemNotFound {
            throw KeychainError.status(status)
        }
    }
}
