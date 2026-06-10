import Foundation
import Observation
import XClawCore

/// Central app state: owns the CoreSupervisor (xclawd lifecycle) and the
/// ControlClient (the bus), folds the inbound event stream into an AppState on
/// the main actor, and exposes everything the SwiftUI views render. The XClaw
/// analogue of Open Island's AppModel.
@MainActor
@Observable
final class AppModel {
    // Surfaced to the UI.
    var coreState: String = "stopped"
    var connected: Bool = false
    var sessions: [AppState.SessionView] = []
    var lastError: String?
    var driver: String = "claude"

    @ObservationIgnored private var supervisor: CoreSupervisor?
    @ObservationIgnored private var client: ControlClient?
    @ObservationIgnored private var consumeTask: Task<Void, Never>?
    @ObservationIgnored private var state = AppState()
    @ObservationIgnored private let socketPath = CorePaths.socketPath
    /// The DM uid used for messages sent from this GUI.
    @ObservationIgnored let localUID = "gui-user"

    /// Boots the core and connects the control bus.
    func start(driver: String = "claude") {
        self.driver = driver
        CorePaths.ensureSupportDir()

        guard let bin = CorePaths.resolveBinary() else {
            coreState = "error"
            lastError = "xclawd binary not found (set XCLAWD_BIN or build core)"
            return
        }

        let cfg = CoreSupervisor.Config(
            binaryPath: bin,
            socketPath: socketPath,
            dbPath: CorePaths.dbPath,
            driver: driver
        )
        let sup = CoreSupervisor(config: cfg) { [weak self] st in
            // Called off the main actor; hop back to update @Observable state.
            Task { @MainActor [weak self] in self?.applyCoreState(st) }
        }
        supervisor = sup
        sup.start()
    }

    /// Stops the bus and the core.
    func stop() {
        consumeTask?.cancel()
        consumeTask = nil
        client?.disconnect()
        client = nil
        connected = false
        supervisor?.stop()
        supervisor = nil
        coreState = "stopped"
    }

    /// Sends a user message to the agent over the bus.
    func send(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, let client else { return }
        do {
            try client.send(type: "session.send", body: SessionSendBody(uid: localUID, text: trimmed))
        } catch {
            lastError = "send failed: \(error)"
        }
    }

    /// Clears the current GUI session's resume mapping.
    func reset() {
        guard let client else { return }
        _ = try? client.send(type: "session.reset", body: SessionSendBody(uid: localUID, text: ""))
        state = AppState()
        sessions = []
    }

    // MARK: - private

    private func applyCoreState(_ st: CoreSupervisor.State) {
        switch st {
        case .stopped:
            coreState = "stopped"
            connected = false
        case .starting:
            coreState = "starting"
        case .running(let pid):
            coreState = "running (pid \(pid))"
            // The daemon needs a moment to bind the socket; connect with retry.
            connectWithRetry()
        case .restarting(let err):
            coreState = "restarting"
            lastError = err
            connected = false
            // Drop the stale connection; reconnect after the new daemon is up.
            consumeTask?.cancel()
            client?.disconnect()
            client = nil
        }
    }

    private func connectWithRetry(attempt: Int = 0) {
        // Already connected.
        if client != nil { return }
        let c = ControlClient(path: socketPath)
        do {
            try c.connect()
        } catch {
            guard attempt < 20 else {
                lastError = "control connect failed: \(error)"
                return
            }
            // Socket may not be bound yet; retry shortly.
            Task { @MainActor [weak self] in
                try? await Task.sleep(for: .milliseconds(150))
                self?.connectWithRetry(attempt: attempt + 1)
            }
            return
        }
        client = c
        connected = true
        consumeEvents(from: c)
        // Probe health so the connection is exercised immediately.
        _ = try? c.send(type: "health", body: [String: String]())
    }

    private func consumeEvents(from c: ControlClient) {
        consumeTask?.cancel()
        consumeTask = Task { @MainActor [weak self] in
            for await env in c.events {
                guard let self else { return }
                if env.kind == .event {
                    self.state.apply(env)
                    self.sessions = self.state.sessions.values.sorted { $0.sessionKey < $1.sessionKey }
                    if let e = self.state.lastError { self.lastError = e }
                }
            }
            // Stream ended (disconnect); reflect it.
            self?.connected = false
        }
    }
}
