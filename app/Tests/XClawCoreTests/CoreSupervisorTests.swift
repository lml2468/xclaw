import Testing
import Foundation
@testable import XClawCore

@Test
func supervisorReportsMissingBinary() async {
    let states = StateBox()
    let sup = CoreSupervisor(
        config: .init(binaryPath: "/nonexistent/xclawd", socketPath: "/tmp/none.sock"),
        onState: { states.append($0) }
    )
    #expect(sup.binaryAvailable == false)
    sup.start()
    // It should report a restarting state (missing binary is retryable).
    try? await Task.sleep(for: .milliseconds(300))
    sup.stop()
    #expect(states.contains { if case .restarting = $0 { return true }; return false })
}

@Test
func supervisorLaunchesAndReportsRunning() async throws {
    // A fake "daemon" that sleeps until killed.
    let script = makeFakeDaemon(body: "sleep 30")
    defer { try? FileManager.default.removeItem(atPath: script) }

    let states = StateBox()
    let sup = CoreSupervisor(
        config: .init(binaryPath: script, socketPath: "/tmp/fake.sock"),
        onState: { states.append($0) }
    )
    #expect(sup.binaryAvailable == true)
    sup.start()
    try await Task.sleep(for: .milliseconds(400))

    let running = states.snapshot.contains { if case .running = $0 { return true }; return false }
    #expect(running)
    sup.stop()
    try await Task.sleep(for: .milliseconds(100))
    #expect(states.snapshot.contains(.stopped))
}

@Test
func supervisorRestartsOnExit() async throws {
    // A fake daemon that exits immediately → supervisor should schedule a
    // restart (we observe at least one restarting state with fast backoff).
    let script = makeFakeDaemon(body: "exit 0")
    defer { try? FileManager.default.removeItem(atPath: script) }

    let states = StateBox()
    let sup = CoreSupervisor(
        config: .init(binaryPath: script, socketPath: "/tmp/fake2.sock"),
        onState: { states.append($0) }
    )
    sup.start()
    try await Task.sleep(for: .milliseconds(1200))
    sup.stop()

    let snap = states.snapshot
    let restarts = snap.filter { if case .restarting = $0 { return true }; return false }.count
    #expect(restarts >= 1, "states observed: \(snap)")
}

// MARK: - helpers

/// Thread-safe collector for supervisor state callbacks (called off-main).
final class StateBox: @unchecked Sendable {
    private let lock = NSLock()
    private var states: [CoreSupervisor.State] = []
    func append(_ s: CoreSupervisor.State) { lock.lock(); states.append(s); lock.unlock() }
    var snapshot: [CoreSupervisor.State] { lock.lock(); defer { lock.unlock() }; return states }
    func contains(_ pred: (CoreSupervisor.State) -> Bool) -> Bool { snapshot.contains(where: pred) }
}

/// Writes an executable shell script and returns its path.
func makeFakeDaemon(body: String) -> String {
    let dir = NSTemporaryDirectory()
    let path = (dir as NSString).appendingPathComponent("fake-xclawd-\(UUID().uuidString).sh")
    let content = "#!/bin/sh\n\(body)\n"
    try? content.write(toFile: path, atomically: true, encoding: .utf8)
    try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: path)
    return path
}
