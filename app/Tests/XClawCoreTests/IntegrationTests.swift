import Testing
import Foundation
@testable import XClawCore

/// End-to-end integration of the AppModel flow, headless: CoreSupervisor spawns
/// the REAL xclawd, ControlClient connects over the bus, a command is sent, and
/// events stream back. Skips automatically if the dev binary isn't built (so it
/// never breaks CI machines without Go).
@Test
func supervisorSpawnsRealCoreAndBusDelivers() async throws {
    guard let bin = devXclawdPath() else {
        // Not built here; this path is exercised by scripts/run-dev.sh.
        return
    }

    let sock = (NSTemporaryDirectory() as NSString)
        .appendingPathComponent("xclaw-it-\(getpid()).sock")
    let db = (NSTemporaryDirectory() as NSString)
        .appendingPathComponent("xclaw-it-\(getpid()).db")
    defer {
        try? FileManager.default.removeItem(atPath: sock)
        try? FileManager.default.removeItem(atPath: db)
    }

    let sup = CoreSupervisor(config: .init(
        binaryPath: bin, socketPath: sock, dbPath: db, driver: "codex"))
    sup.start()
    defer { sup.stop() }

    // Wait for the daemon to bind the socket.
    var ready = false
    for _ in 0..<50 {
        if FileManager.default.fileExists(atPath: sock) { ready = true; break }
        try await Task.sleep(for: .milliseconds(100))
    }
    #expect(ready, "xclawd did not bind the control socket")
    guard ready else { return }

    let client = ControlClient(path: sock)
    try client.connect()
    defer { client.disconnect() }

    let seen = SeenEvents()
    let consumer = Task.detached {
        for await env in client.events {
            if env.kind == .response { seen.addResponse(env.type) }
            if env.kind == .event { seen.addEvent(env.type) }
        }
    }
    defer { consumer.cancel() }

    _ = try client.send(type: "health", body: [String: String]())
    _ = try client.send(type: "session.send", body: SessionSendBody(uid: "it-user", text: "ping"))

    // Give the turn time to stream back.
    try await Task.sleep(for: .seconds(4))

    #expect(seen.responses.contains("health"), "responses: \(seen.responses)")
    // The turn should at least produce activity events (turnStart/turnDone),
    // even when the agent isn't authenticated (no assistant text).
    #expect(!seen.events.isEmpty, "no events received; events: \(seen.events)")
}

/// Finds the dev-built xclawd (scripts/run-dev.sh writes core/.xclawd-dev).
private func devXclawdPath() -> String? {
    let fm = FileManager.default
    if let env = ProcessInfo.processInfo.environment["XCLAWD_BIN"], fm.isExecutableFile(atPath: env) {
        return env
    }
    var dir = URL(fileURLWithPath: fm.currentDirectoryPath)
    for _ in 0..<6 {
        let p = dir.appendingPathComponent("core/.xclawd-dev").path
        if fm.isExecutableFile(atPath: p) { return p }
        dir.deleteLastPathComponent()
    }
    return nil
}

final class SeenEvents: @unchecked Sendable {
    private let lock = NSLock()
    private var ev: [String] = []
    private var resp: [String] = []
    func addEvent(_ t: String) { lock.lock(); ev.append(t); lock.unlock() }
    func addResponse(_ t: String) { lock.lock(); resp.append(t); lock.unlock() }
    var events: [String] { lock.lock(); defer { lock.unlock() }; return ev }
    var responses: [String] { lock.lock(); defer { lock.unlock() }; return resp }
}
