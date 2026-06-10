# XClaw macOS app

Native control plane / GUI for the `xclawd` Go core (`../core`). SwiftPM
package, macOS 14+, mirroring [Open Island](https://github.com/lml2468/open-vibe-island)'s
multi-target layout.

## Targets

- **XClawApp** — SwiftUI + AppKit shell. Owns `AppModel`, supervises the
  `xclawd` subprocess, renders bots / sessions from the control bus.
- **XClawCore** — control-bus client (NDJSON over Unix socket, see `../proto`),
  `AppState` reducer, models. Agent/IM-agnostic.

## Status

The app boots the core and renders the live event stream, with multi-bot
management:

- **CoreSupervisor** (`XClawCore`) spawns `xclawd` with a control socket,
  watches it, and restarts with exponential backoff.
- **AppModel** (`XClawApp`) owns the supervisor + `ControlClient`, folds the
  per-bot event stream into `AppState` (bucketed by botId), polls `bots.list`,
  and exposes bots / sessions / `send(botId)` / `reset` to the UI.
- **Console window** — a bot sidebar (per-bot connection status + session count)
  + the selected bot's session view + composer; **MenuBarExtra** shows the bot
  count and bus status.

Verified headlessly: an integration test spawns the REAL `xclawd -config` with a
control socket and a 2-bot config, connects over the bus, and asserts
`bots.list` returns both bots.

## Run (dev)

```bash
# from the repo root — builds the Go core, then launches the app which spawns it
zsh scripts/run-dev.sh            # claude driver
zsh scripts/run-dev.sh codex      # codex driver

# or directly (point the app at a prebuilt daemon)
cd core && go build -o .xclawd-dev ./cmd/xclawd
XCLAWD_BIN=$PWD/.xclawd-dev swift run --package-path ../app XClawApp
```

## Relationship to the core

The app never talks to Claude/Codex directly — it drives `xclawd`, which owns
all agent driving, session state, and IM connectivity. The app is one client of
the control bus; a web console or CLI could be others.
