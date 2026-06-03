#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="$DIR/dist"
VERSION="${VERSION:-$(git -C "$DIR" describe --tags --always --dirty 2>/dev/null || echo snapshot)}"

mkdir -p "$DIST"
rm -f "$DIST"/codex-deepseek-installer-* "$DIST"/install-*.sh "$DIST"/install-windows.ps1 "$DIST"/README.md "$DIST"/SHA256SUMS

build() {
  local goos="$1"
  local goarch="$2"
  local out="$3"
  echo "Building $out"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-buildid=" -o "$DIST/$out" .
}

package_tar() {
  local platform="$1"
  local binary="$2"
  local script="$3"
  local archive="$DIST/codex-deepseek-installer-$VERSION-$platform.tar.gz"
  echo "Packaging $(basename "$archive")"
  tar -czf "$archive" -C "$DIST" "$binary" "$script" README.md
}

cd "$DIR"

build darwin arm64 codex-deepseek-installer-darwin-arm64
build darwin amd64 codex-deepseek-installer-darwin-amd64

cp "$DIR/install-macos.sh" "$DIST/"
cp "$DIR/README.md" "$DIST/"

chmod +x "$DIST"/codex-deepseek-installer-* "$DIST"/install-macos.sh

package_tar macos-arm64 codex-deepseek-installer-darwin-arm64 install-macos.sh
package_tar macos-amd64 codex-deepseek-installer-darwin-amd64 install-macos.sh

(cd "$DIST" && shasum -a 256 codex-deepseek-installer-"$VERSION"-* > SHA256SUMS)

cat <<EOF
Release files written to:
  $DIST

Version:
  $VERSION

macOS:
  codex-deepseek-installer-$VERSION-macos-arm64.tar.gz
  codex-deepseek-installer-$VERSION-macos-amd64.tar.gz
EOF
