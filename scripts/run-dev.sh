#!/bin/zsh
# Dev runner: build the Go core, place it where the Swift app's CorePaths can
# find it, then launch the app. The app's CoreSupervisor spawns the daemon,
# connects the control bus, and the console window streams the agent.
#
#   zsh scripts/run-dev.sh [claude|codex]
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
driver="${1:-claude}"

echo "▸ building xclawd…"
( cd "$repo_root/core" && go build -o "$repo_root/core/.xclawd-dev" ./cmd/xclawd )
echo "  → $repo_root/core/.xclawd-dev"

echo "▸ launching XClawApp (driver=$driver)…"
# Run from repo root so CorePaths' dev-walk finds core/.xclawd-dev. Also export
# an explicit override for robustness.
export XCLAWD_BIN="$repo_root/core/.xclawd-dev"
cd "$repo_root"
swift run --package-path app XClawApp
