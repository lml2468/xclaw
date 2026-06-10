import Foundation

/// NDJSON codec for control-bus envelopes, mirroring Go's `control.Encode` /
/// `control.Decode`. One envelope per line; lines are newline-terminated.
public enum ControlCodec {
    /// Maximum size of a single frame (matches Go's MaxFrameBytes).
    public static let maxFrameBytes = 4 * 1024 * 1024

    public static func encode(_ env: Envelope) throws -> Data {
        var data = try JSONEncoder().encode(env)
        data.append(0x0A) // '\n'
        return data
    }

    public static func decode(_ line: Data) throws -> Envelope {
        try JSONDecoder().decode(Envelope.self, from: line)
    }

    /// Builds a command envelope with a JSON-encoded body.
    public static func command<B: Encodable>(id: String, type: String, body: B) throws -> Envelope {
        let bodyData = try JSONEncoder().encode(body)
        return Envelope(kind: .command, id: id, type: type, body: bodyData)
    }
}

/// Accumulates a byte stream and yields complete NDJSON lines. Enforces the
/// frame-size cap so a peer that never sends a newline can't grow memory without
/// bound (the Open Island hardening lesson).
public final class LineFramer {
    private var buffer = Data()
    private let maxFrame: Int

    public init(maxFrame: Int = ControlCodec.maxFrameBytes) {
        self.maxFrame = maxFrame
    }

    public enum FramerError: Error { case frameTooLarge }

    /// Appends bytes and returns any complete lines (without the trailing '\n').
    public func push(_ chunk: Data) throws -> [Data] {
        buffer.append(chunk)
        var lines: [Data] = []
        while let nl = buffer.firstIndex(of: 0x0A) {
            let line = buffer.subdata(in: buffer.startIndex..<nl)
            buffer.removeSubrange(buffer.startIndex...nl)
            if !line.isEmpty {
                lines.append(line)
            }
        }
        if buffer.count > maxFrame {
            throw FramerError.frameTooLarge
        }
        return lines
    }
}
