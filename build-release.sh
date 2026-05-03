#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="$DIR/dist"

mkdir -p "$DIST"

build() {
  local goos="$1"
  local goarch="$2"
  local out="$3"
  echo "Building $out"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$DIST/$out" .
}

cd "$DIR"

build darwin arm64 codex-deepseek-installer-darwin-arm64
build darwin amd64 codex-deepseek-installer-darwin-amd64
build linux arm64 codex-deepseek-installer-linux-arm64
build linux amd64 codex-deepseek-installer-linux-amd64
build windows arm64 codex-deepseek-installer-windows-arm64.exe
build windows amd64 codex-deepseek-installer-windows-amd64.exe

cp "$DIR/install-macos.sh" "$DIST/"
cp "$DIR/install-linux.sh" "$DIST/"
cp "$DIR/install-windows.ps1" "$DIST/"
cp "$DIR/README.md" "$DIST/"

chmod +x "$DIST"/codex-deepseek-installer-* "$DIST"/install-macos.sh "$DIST"/install-linux.sh

cat <<EOF
Release files written to:
  $DIST

macOS:
  ./install-macos.sh

Linux:
  ./install-linux.sh

Windows PowerShell:
  powershell -ExecutionPolicy Bypass -File .\install-windows.ps1
EOF
