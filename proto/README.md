# XClaw control-bus protocol

The contract between the Go core (`xclawd`) and any client shell (the Swift
macOS app, a web console, a CLI). It is intentionally language-neutral so each
side can implement it independently.

## Transport

- **v1:** newline-delimited JSON (NDJSON) over a Unix domain socket.
  - `xclawd` listens; clients connect.
  - Each line is one envelope. UTF-8. Max frame size enforced by the server.
  - Rationale: trivial to implement in Swift (`Network`/`FileHandle`) and Go
    (`bufio`), easy to debug (`socat`/`nc`), and battle-tested in Open Island's
    bridge. Upgrade path to gRPC/protobuf-over-UDS if the schema grows.

## Envelope

```jsonc
{ "v": 1, "kind": "event" | "command" | "response", "id": "<uuid>", "ts": 1234567890, "body": { ... } }
```

- `id` correlates a `command` with its `response`.
- `event`s are unsolicited (server → client) and carry no correlation id.

## Commands (client → core)

| command | body | response |
|---|---|---|
| `bots.list` | — | `[{id, connected, lastError}]` |
| `bot.start` | `{id}` | `{ok}` |
| `bot.stop` | `{id}` | `{ok}` |
| `config.reload` | — | `{ok}` |
| `session.history` | `{sessionKey, limit}` | `[{role, content, ts}]` |
| `session.send` | `{botId, channel, text}` | `{ok}` (turn streamed via events) |
| `secret.inject` | `{botId, kind, value}` | `{ok}` (from Keychain; never persisted) |
| `health` | — | `{uptime, bots, connections}` |

## Events (core → client)

Derived from the unified `agent.AgentEvent` plus gateway lifecycle:

| event kind | body |
|---|---|
| `bot.status` | `{id, connected, lastError}` |
| `session.activity` | `{sessionKey, agent, kind}` where kind ∈ `msgIn` \| `turnStart` \| `textDelta` \| `toolUse` \| `toolResult` \| `turnDone` |
| `session.text` | `{sessionKey, delta}` |
| `session.tool` | `{sessionKey, name, params}` |
| `session.usage` | `{sessionKey, inputTokens, outputTokens}` |
| `rateLimit.hit` | `{sessionKey}` |
| `cron.fired` | `{taskId, channel}` |
| `error` | `{scope, message, recoverable}` |

## Notes

- The `session.*` event vocabulary is a 1:1 projection of `core/agent`'s
  `AgentEvent`, so the same normalization that unifies Claude and Codex also
  unifies what every shell renders.
- Secrets travel **into** the core (`secret.inject`) but never appear in any
  event or response; the core holds them in memory only.
