#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH_TAG="amd64" ;;
  arm64|aarch64) ARCH_TAG="arm64" ;;
  *)
    echo "Unsupported macOS architecture: $ARCH" >&2
    exit 1
    ;;
esac

CANDIDATES=(
  "$DIR/codex-deepseek-installer"
  "$DIR/codex-deepseek-installer-darwin-$ARCH_TAG"
  "$DIR/dist/codex-deepseek-installer-darwin-$ARCH_TAG"
)

BIN=""
for candidate in "${CANDIDATES[@]}"; do
  if [[ -x "$candidate" ]]; then
    BIN="$candidate"
    break
  fi
done

if [[ -z "$BIN" ]]; then
  echo "Missing executable. Expected one of:" >&2
  printf '  %s\n' "${CANDIDATES[@]}" >&2
  exit 1
fi

exec "$BIN" install "$@"
