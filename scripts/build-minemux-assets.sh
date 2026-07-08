#!/usr/bin/env sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ASSETS="$ROOT/app/src/main/assets/minemux"
WEBUI_ASSETS="$ASSETS/webui"

mkdir -p "$ASSETS" "$WEBUI_ASSETS"

if command -v go >/dev/null 2>&1; then
  (
    cd "$ROOT/daemon"
    GOOS=android GOARCH=arm64 go build -o "$ASSETS/minemux-daemon" ./cmd/minemux-daemon
  )
else
  echo "go is not installed; keeping existing $ASSETS/minemux-daemon" >&2
fi

cp "$ROOT/webui/index.html" "$WEBUI_ASSETS/index.html"
echo "$ASSETS"
