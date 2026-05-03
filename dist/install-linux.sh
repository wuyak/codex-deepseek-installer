#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH_TAG="amd64" ;;
  arm64|aarch64) ARCH_TAG="arm64" ;;
  *)
    echo "Unsupported Linux architecture: $ARCH" >&2
    exit 1
    ;;
esac

CANDIDATES=(
  "$DIR/codex-deepseek-installer"
  "$DIR/codex-deepseek-installer-linux-$ARCH_TAG"
  "$DIR/dist/codex-deepseek-installer-linux-$ARCH_TAG"
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

cat <<'EOF'
Linux mode currently patches config/catalog only.
Codex App picker Statsig patch is skipped until the Linux Codex App Local Storage path is QA-verified.
EOF

exec "$BIN" install --skip-statsig "$@"
