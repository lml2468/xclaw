import Foundation

/// Supervises the `xclawd` Go core subprocess: launches it with a control
/// socket, watches for exit, and restarts with exponential backoff. The Swift
/// analogue of Open Island's process supervision — here we own the lifecycle of
/// the daemon the GUI drives.
///
/// Lifecycle is reported via the `onState` callback so an AppModel can reflect
/// it. The supervisor does NOT own the control connection; it only guarantees a
/// running daemon at `socketPath`. A crash-loop (repeated immediate exits) trips
/// a circuit breaker and reports `.failed` rather than restarting forever.
public final class CoreSupervisor: @unchecked Sendable {
    public enum State: Equatable, Sendable {
        case stopped
        case starting
        case running(pid: Int32)
        case restarting(afterError: String)
        case failed(reason: String)
    }

    public struct Config: Sendable {
        /// Absolute path to the `xclawd` binary.
        public var binaryPath: String
        /// Unix socket the daemon will listen on (passed as -control).
        public var socketPath: String
        /// SQLite path (-db). Single-bot mode only; defaults to a temp file.
        public var dbPath: String
        /// When non-empty, run in multi-bot config mode: `-config <configPath>
        /// -control <socketPath>`. Empty path with configMode=true uses the
        /// daemon's default ~/.xclaw/config.json. Takes precedence over the
        /// single-bot flags above.
        public var configMode: Bool
        public var configPath: String
        /// Extra args appended verbatim.
        public var extraArgs: [String]

        public init(binaryPath: String, socketPath: String, dbPath: String = "",
                    configMode: Bool = false,
                    configPath: String = "", extraArgs: [String] = []) {
            self.binaryPath = binaryPath
            self.socketPath = socketPath
            self.dbPath = dbPath
            self.configMode = configMode
            self.configPath = configPath
            self.extraArgs = extraArgs
        }
    }

    private let config: Config
    private let onState: @Sendable (State) -> Void
    private let queue = DispatchQueue(label: "app.xclaw.supervisor")

    private var process: Process?
    private var stopped = false
    private var backoff: TimeInterval = 1.0
    private static let maxBackoff: TimeInterval = 30.0
    /// A run shorter than this counts as a failure (crash-loop detection);
    /// a longer run is "healthy" and resets the failure budget.
    private static let healthyUptime: TimeInterval = 15.0
    private var consecutiveFailures = 0
    private var launchedAt: DispatchTime?

    /// Consecutive immediate failures before the circuit breaker trips and the
    /// supervisor reports `.failed` instead of restarting. Internal so tests can
    /// shrink it; defaults to a production-sane value.
    var maxConsecutiveFailures = 5
    /// Initial restart backoff (doubles up to `maxBackoff`). Internal so tests
    /// can shrink it to keep crash-loop tests fast.
    var initialBackoff: TimeInterval = 1.0

    public init(config: Config, onState: @escaping @Sendable (State) -> Void = { _ in }) {
        self.config = config
        self.onState = onState
    }

    /// Starts the daemon and keeps it alive until `stop()`.
    public func start() {
        queue.async { [weak self] in
            guard let self else { return }
            self.backoff = self.initialBackoff
            self.launchLocked()
        }
    }

    /// Stops the daemon and disables restarts.
    public func stop() {
        queue.async { [weak self] in
            guard let self else { return }
            self.stopped = true
            self.process?.terminationHandler = nil
            self.process?.terminate()
            self.process = nil
            Log.supervisor.info("supervisor stopped")
            self.onState(.stopped)
        }
    }

    /// Whether the supervised binary exists and is executable.
    public var binaryAvailable: Bool {
        FileManager.default.isExecutableFile(atPath: config.binaryPath)
    }

    private func launchLocked() {
        if stopped { return }
        guard binaryAvailable else {
            // Treat a missing binary as a retryable error so a later install/
            // build is picked up.
            handleExit(reason: "xclawd not found at \(config.binaryPath)", healthy: false)
            return
        }

        onState(.starting)

        let p = Process()
        p.executableURL = URL(fileURLWithPath: config.binaryPath)
        var args: [String]
        if config.configMode {
            // Multi-bot: -config [path] -control <sock>. An empty configPath
            // lets the daemon use its default ~/.xclaw/config.json.
            args = ["-config", config.configPath, "-control", config.socketPath]
        } else {
            args = ["-control", config.socketPath, "-no-repl"]
            if !config.dbPath.isEmpty {
                args += ["-db", config.dbPath]
            }
        }
        args += config.extraArgs
        p.arguments = args
        // Inherit stdio so daemon logs surface in the app's console during dev.
        p.standardOutput = FileHandle.standardOutput
        p.standardError = FileHandle.standardError

        p.terminationHandler = { [weak self] proc in
            guard let self else { return }
            self.queue.async {
                if self.stopped { return }
                let uptime = self.launchedAt.map {
                    Double(DispatchTime.now().uptimeNanoseconds - $0.uptimeNanoseconds) / 1e9
                } ?? 0
                let healthy = uptime >= Self.healthyUptime
                self.handleExit(reason: "xclawd exited (status \(proc.terminationStatus))", healthy: healthy)
            }
        }

        do {
            try p.run()
        } catch {
            handleExit(reason: "launch failed: \(error.localizedDescription)", healthy: false)
            return
        }
        process = p
        launchedAt = DispatchTime.now()
        Log.supervisor.info("xclawd launched (pid \(p.processIdentifier))")
        onState(.running(pid: p.processIdentifier))
    }

    /// Handles a process exit / launch failure: resets the failure budget if the
    /// previous run was healthy, then either restarts with backoff or trips the
    /// circuit breaker after too many consecutive immediate failures.
    private func handleExit(reason: String, healthy: Bool) {
        if stopped { return }
        process = nil
        if healthy {
            consecutiveFailures = 0
            backoff = initialBackoff
        }
        consecutiveFailures += 1

        if consecutiveFailures > maxConsecutiveFailures {
            let msg = "xclawd failed \(consecutiveFailures) times in a row; giving up. Last: \(reason)"
            Log.supervisor.fault("\(msg, privacy: .public)")
            onState(.failed(reason: msg))
            return
        }

        let delay = backoff
        backoff = min(backoff * 2, Self.maxBackoff)
        Log.supervisor.error("\(reason, privacy: .public); restarting in \(delay, format: .fixed(precision: 1))s (attempt \(self.consecutiveFailures))")
        onState(.restarting(afterError: reason))
        queue.asyncAfter(deadline: .now() + delay) { [weak self] in
            self?.launchLocked()
        }
    }
}
