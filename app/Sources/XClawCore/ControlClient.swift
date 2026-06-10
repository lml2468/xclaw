import Foundation
#if canImport(Darwin)
import Darwin
#endif

/// Connects to xclawd's control bus over a Unix domain socket, sends command
/// envelopes, and exposes the inbound event stream. Roles are reversed from
/// Open Island's bridge (here the Go core is the server, this is the client).
///
/// Transport detail: a Unix-domain stream socket via POSIX `connect(2)`, with a
/// background read loop on a dispatch queue feeding a `LineFramer`. Decoded
/// event envelopes are published on an AsyncStream.
public final class ControlClient: @unchecked Sendable {
    private let path: String
    private var fd: Int32 = -1
    private let queue = DispatchQueue(label: "app.xclaw.control.read")
    private let framer = LineFramer()
    private var idCounter = 0
    private let idLock = NSLock()

    private var continuation: AsyncStream<Envelope>.Continuation?
    public let events: AsyncStream<Envelope>

    public init(path: String) {
        self.path = path
        var cont: AsyncStream<Envelope>.Continuation!
        self.events = AsyncStream { cont = $0 }
        self.continuation = cont
    }

    public enum ClientError: Error {
        case socketFailed(Int32)
        case connectFailed(Int32)
        case pathTooLong
        case notConnected
        case writeFailed(Int32)
    }

    /// Connects and starts the background read loop. Idempotent-ish: call once.
    public func connect() throws {
        let s = socket(AF_UNIX, SOCK_STREAM, 0)
        guard s >= 0 else { throw ClientError.socketFailed(errno) }

        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let pathBytes = path.utf8CString
        guard pathBytes.count <= MemoryLayout.size(ofValue: addr.sun_path) else {
            close(s)
            throw ClientError.pathTooLong
        }
        withUnsafeMutablePointer(to: &addr.sun_path) { dst in
            dst.withMemoryRebound(to: CChar.self, capacity: pathBytes.count) { p in
                for (i, b) in pathBytes.enumerated() { p[i] = b }
            }
        }
        let connectResult = withUnsafePointer(to: &addr) { ap in
            ap.withMemoryRebound(to: sockaddr.self, capacity: 1) { sp in
                Darwin.connect(s, sp, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard connectResult == 0 else {
            close(s)
            throw ClientError.connectFailed(errno)
        }
        fd = s
        startReadLoop()
    }

    public func disconnect() {
        if fd >= 0 {
            close(fd)
            fd = -1
        }
        continuation?.finish()
    }

    private func nextID() -> String {
        idLock.lock(); defer { idLock.unlock() }
        idCounter += 1
        return "c\(idCounter)"
    }

    /// Sends a command and returns the id used (responses arrive as events with
    /// matching id on the stream).
    @discardableResult
    public func send<B: Encodable>(type: String, body: B) throws -> String {
        guard fd >= 0 else { throw ClientError.notConnected }
        let id = nextID()
        let env = try ControlCodec.command(id: id, type: type, body: body)
        let line = try ControlCodec.encode(env)
        try writeAll(line)
        return id
    }

    private func writeAll(_ data: Data) throws {
        try data.withUnsafeBytes { (raw: UnsafeRawBufferPointer) in
            var off = 0
            let base = raw.bindMemory(to: UInt8.self).baseAddress!
            while off < data.count {
                let n = Darwin.write(fd, base + off, data.count - off)
                if n <= 0 { throw ClientError.writeFailed(errno) }
                off += n
            }
        }
    }

    private func startReadLoop() {
        let localFD = fd
        queue.async { [weak self] in
            guard let self else { return }
            var buf = [UInt8](repeating: 0, count: 64 * 1024)
            while true {
                let n = Darwin.read(localFD, &buf, buf.count)
                if n <= 0 { break }
                let chunk = Data(buf[0..<n])
                do {
                    for line in try self.framer.push(chunk) {
                        if let env = try? ControlCodec.decode(line) {
                            self.continuation?.yield(env)
                        }
                    }
                } catch {
                    break // frame too large or fatal: stop the loop
                }
            }
            self.continuation?.finish()
        }
    }
}
