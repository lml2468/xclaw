#!/bin/zsh
# Dev runner: build the Go core dev binary, then launch the Wails desktop app
# with hot reload (`wails3 dev`). The app's bridge spawns the daemon in multi-bot
# config mode (~/.octobuddy/config.json), connects the control bus, and the console
# lists bots + streams sessions. The bridge resolves the daemon via $OCTOBUDDY_DAEMON_BIN.
#
#   zsh scripts/run-dev.sh                 # launch (needs ~/.octobuddy/config.json)
#   zsh scripts/run-dev.sh --seed-config   # write a starter ~/.octobuddy config, then launch
#   zsh scripts/run-dev.sh --preview       # UI preview (mock data, no daemon)
#
# Without a config the app shows a "needs-config" state instead of running an
# empty daemon — seed one (or edit it by hand) to add bots.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$(go env GOPATH)/bin:$PATH"
seed=false; preview=false
case "${1:-}" in
  --seed-config) seed=true ;;
  --preview) preview=true ;;
esac

if $seed; then
  cfg_dir="$HOME/.octobuddy"
  mkdir -p "$cfg_dir/default"
  if [[ ! -f "$cfg_dir/config.json" ]]; then
    cat > "$cfg_dir/config.json" <<'JSON'
{
  "apiUrl": "https://your-octo-server.example",
  "agent": { "model": "claude-opus-4-8" },
  "bots": [ { "id": "default" } ]
}
JSON
    echo "▸ wrote $cfg_dir/config.json (edit apiUrl; set the token via the in-app editor)"
  fi
fi

if $preview; then
  echo "▸ UI preview (mock data, no daemon)…"
  export OCTOBUDDY_PREVIEW=1
  cd "$repo_root/desktop" && exec wails3 dev
fi

echo "▸ building octobuddy-daemon dev binary…"
( cd "$repo_root/core" && go build -o "$repo_root/core/.octobuddy-daemon-dev" ./cmd/octobuddy-daemon )
echo "  → $repo_root/core/.octobuddy-daemon-dev"

echo "▸ launching OctoBuddy (wails3 dev)…"
# The bridge resolves the daemon from $OCTOBUDDY_DAEMON_BIN (else a cwd-walk finds
# core/.octobuddy-daemon-dev).
export OCTOBUDDY_DAEMON_BIN="$repo_root/core/.octobuddy-daemon-dev"
cd "$repo_root/desktop"
exec wails3 dev
