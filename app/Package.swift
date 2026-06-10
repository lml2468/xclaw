// swift-tools-version: 6.0
import PackageDescription

// XClaw macOS app — native control plane / GUI for the xclawd Go core.
// Mirrors Open Island's multi-target SwiftPM layout. This is a scaffold: the
// control-bus client (proto/) and AppModel wire up once the core MVP lands.
let package = Package(
    name: "XClaw",
    defaultLocalization: "en",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(name: "XClawApp", targets: ["XClawApp"]),
        .library(name: "XClawCore", targets: ["XClawCore"]),
    ],
    targets: [
        // Models + control-bus client (NDJSON over UDS) + AppState reducer.
        // Mirrors Open Island's OpenIslandCore: the IM/agent-agnostic core.
        .target(
            name: "XClawCore"
        ),
        // SwiftUI + AppKit shell. Owns AppModel, talks to XClawCore.
        // Mirrors Open Island's OpenIslandApp.
        .executableTarget(
            name: "XClawApp",
            dependencies: ["XClawCore"]
        ),
        .testTarget(
            name: "XClawCoreTests",
            dependencies: ["XClawCore"]
        ),
    ]
)
