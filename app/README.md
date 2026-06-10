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

Scaffold. The control-bus client and AppModel wire up once the core MVP and the
`proto/` contract are finalized.

## Build

```bash
swift build
swift test
swift run XClawApp
```

## Relationship to the core

The app never talks to Claude/Codex directly — it drives `xclawd`, which owns
all agent driving, session state, and IM connectivity. The app is one client of
the control bus; a web console or CLI could be others.
