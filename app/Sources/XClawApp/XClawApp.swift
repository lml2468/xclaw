import SwiftUI

// Scaffold entry point for the XClaw macOS app. The real shell will own an
// AppModel that supervises the xclawd process and renders bot/session state
// from the control bus. For now this is a minimal placeholder so `swift build`
// succeeds and the target structure is in place.
@main
struct XClawApp: App {
    var body: some Scene {
        MenuBarExtra("XClaw", systemImage: "bolt.horizontal.circle") {
            Text("XClaw — control plane scaffold")
            Divider()
            Button("Quit") { NSApplication.shared.terminate(nil) }
        }
    }
}
