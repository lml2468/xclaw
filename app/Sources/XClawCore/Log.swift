import Foundation
import os

/// Unified logging for the app, replacing scattered `print`/silent failures.
/// One subsystem, a category per subsystem area, so logs are filterable in
/// Console.app (`subsystem:app.xclaw`).
public enum Log {
    public static let subsystem = "app.xclaw"

    public static let control = Logger(subsystem: subsystem, category: "control")
    public static let supervisor = Logger(subsystem: subsystem, category: "supervisor")
    public static let app = Logger(subsystem: subsystem, category: "app")
    public static let keychain = Logger(subsystem: subsystem, category: "keychain")
}
