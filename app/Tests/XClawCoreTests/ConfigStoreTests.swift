import Testing
import Foundation
@testable import XClawCore

private func withTempBase(_ body: (URL) throws -> Void) rethrows {
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("xclaw-cfg-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: dir) }
    try body(dir)
}

@Test
func configSaveLoadRoundTrip() throws {
    try withTempBase { base in
        let bots = [
            BotConfig(id: "alpha", apiURL: "https://octo.example", driver: "claude", octoToken: "bf_a"),
            BotConfig(id: "beta", apiURL: "https://octo.example", driver: "codex", octoToken: "bf_b"),
        ]
        try ConfigStore.save(bots, base: base)
        let loaded = try ConfigStore.load(base: base)
        #expect(loaded.count == 2)
        let a = loaded.first { $0.id == "alpha" }
        #expect(a?.octoToken == "bf_a")
        #expect(a?.driver == "claude")
        let b = loaded.first { $0.id == "beta" }
        #expect(b?.driver == "codex")
        #expect(b?.octoToken == "bf_b")
        #expect(b?.apiURL == "https://octo.example")
    }
}

@Test
func configRemoveBotPrunesSubtree() throws {
    try withTempBase { base in
        try ConfigStore.save([
            BotConfig(id: "keep", apiURL: "https://o", octoToken: "t1"),
            BotConfig(id: "drop", apiURL: "https://o", octoToken: "t2"),
        ], base: base)
        #expect(FileManager.default.fileExists(atPath: base.appendingPathComponent("drop/config.json").path))

        // Save without "drop" → its subtree is pruned.
        try ConfigStore.save([BotConfig(id: "keep", apiURL: "https://o", octoToken: "t1")], base: base)
        #expect(!FileManager.default.fileExists(atPath: base.appendingPathComponent("drop").path))
        #expect(FileManager.default.fileExists(atPath: base.appendingPathComponent("keep/config.json").path))
        #expect(try ConfigStore.load(base: base).count == 1)
    }
}

@Test
func configRejectsInvalidSlug() throws {
    try withTempBase { base in
        #expect(throws: ConfigStore.ConfigError.self) {
            try ConfigStore.save([BotConfig(id: "../escape", octoToken: "t")], base: base)
        }
    }
}

@Test
func configRejectsDuplicateID() throws {
    try withTempBase { base in
        #expect(throws: ConfigStore.ConfigError.self) {
            try ConfigStore.save([
                BotConfig(id: "same", octoToken: "t1"),
                BotConfig(id: "same", octoToken: "t2"),
            ], base: base)
        }
    }
}

@Test
func slugValidation() {
    #expect(ConfigStore.validSlug("alpha-1.bot_2"))
    #expect(!ConfigStore.validSlug(""))
    #expect(!ConfigStore.validSlug("."))
    #expect(!ConfigStore.validSlug(".."))
    #expect(!ConfigStore.validSlug("a/b"))
    #expect(!ConfigStore.validSlug("a b"))
}

/// Interop: a config written by the Swift ConfigStore must be parseable by the
/// Go core (config.Load). Runs the dev xclawd with -config pointed at the
/// Swift-written dir and asserts bots.list returns the bots. Skips if the dev
/// binary isn't built.
@Test
func swiftWrittenConfigParsesInGo() async throws {
    guard let bin = devXclawdPath() else { return }
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("xclaw-interop-\(getpid())")
    defer { try? FileManager.default.removeItem(at: dir) }

    try ConfigStore.save([
        BotConfig(id: "alpha", apiURL: "http://127.0.0.1:9", driver: "claude", octoToken: "bf_a"),
        BotConfig(id: "beta", apiURL: "http://127.0.0.1:9", driver: "claude", octoToken: "bf_b"),
    ], base: dir)

    let sock = dir.appendingPathComponent("ctl.sock").path
    let proc = Process()
    proc.executableURL = URL(fileURLWithPath: bin)
    proc.arguments = ["-config", dir.appendingPathComponent("config.json").path, "-control", sock]
    proc.standardOutput = FileHandle.nullDevice
    proc.standardError = FileHandle.nullDevice
    try proc.run()
    defer { proc.terminate() }

    var ready = false
    for _ in 0..<50 {
        if FileManager.default.fileExists(atPath: sock) { ready = true; break }
        try await Task.sleep(for: .milliseconds(100))
    }
    #expect(ready, "Go core didn't start with the Swift-written config")
    guard ready else { return }

    let client = ControlClient(path: sock)
    try client.connect()
    defer { client.disconnect() }

    let got = Box<[String]>([])
    let consumer = Task.detached {
        var state = AppState()
        for await env in client.events where env.kind == .response && env.type == "bots.list" {
            if let infos = env.decodeBody([BotInfo].self) {
                state.setBots(infos)
                got.set(state.sortedBots.map { $0.id })
            }
        }
    }
    defer { consumer.cancel() }

    _ = try client.send(type: "bots.list", body: [String: String]())
    try await Task.sleep(for: .seconds(2))
    #expect(got.get() == ["alpha", "beta"], "Go didn't parse the Swift config; got \(got.get())")
}
