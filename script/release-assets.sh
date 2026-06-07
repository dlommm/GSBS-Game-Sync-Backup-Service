#!/usr/bin/env bash
# Generate SHA256SUMS and latest-client.json from built release binaries.
# Usage: ./script/release-assets.sh VERSION [DIR]
#   VERSION: e.g. v1.0.14 or 1.0.14
#   DIR: directory containing binaries (default: repo root)

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"
DIR="${2:-$ROOT}"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 VERSION [DIR]" >&2
  exit 1
fi

VERSION_VALUE="${VERSION#v}"
RELEASED_AT="${RELEASED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

cd "$DIR"

CLIENT_ASSETS=(
  "gsbs-client-windows-amd64.exe"
  "gsbs-client-linux-amd64"
)

ALL_ASSETS=(
  "gsbs-server-windows-amd64.exe"
  "gsbs-client-windows-amd64.exe"
  "gsbs-server-linux-amd64"
  "gsbs-client-linux-amd64"
)

# Optional packaged artifacts (included in SHA256SUMS when present)
OPTIONAL_ASSETS=(
  "gsbs-server-setup-${VERSION_VALUE}-windows-amd64.exe"
  "gsbs-client-setup-${VERSION_VALUE}-windows-amd64.exe"
  "gsbs-client_${VERSION_VALUE}_amd64.deb"
  "gsbs-client-${VERSION_VALUE}-x86_64.AppImage"
)

sha256_file() {
  local f="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  else
    shasum -a 256 "$f" | awk '{print $1}'
  fi
}

: > SHA256SUMS.tmp
FOUND=0
for f in "${ALL_ASSETS[@]}" "${OPTIONAL_ASSETS[@]}"; do
  if [ -f "$f" ]; then
    sum="$(sha256_file "$f")"
    printf '%s  %s\n' "$sum" "$f" >> SHA256SUMS.tmp
    FOUND=$((FOUND + 1))
  fi
done

if [ "$FOUND" -eq 0 ]; then
  echo "No release binaries found in ${DIR}" >&2
  exit 1
fi

mv SHA256SUMS.tmp SHA256SUMS
echo "Wrote SHA256SUMS (${FOUND} files)"

# Build latest-client.json
win_sha=""
lin_sha=""
win_name=""
lin_name=""

for f in "${CLIENT_ASSETS[@]}"; do
  if [ ! -f "$f" ]; then
    continue
  fi
  sum="$(sha256_file "$f")"
  case "$f" in
    gsbs-client-windows-amd64.exe)
      win_sha="$sum"
      win_name="$f"
      ;;
    gsbs-client-linux-amd64)
      lin_sha="$sum"
      lin_name="$f"
      ;;
  esac
done

# In per-platform build jobs only one binary is present; the final release job sets
# REQUIRE_COMPLETE_MANIFEST=1 after downloading all artifacts, so it enforces both.
if [ "${REQUIRE_COMPLETE_MANIFEST:-0}" = "1" ]; then
  if [ -z "$win_sha" ] || [ -z "$lin_sha" ]; then
    echo "ERROR: one or more client platform assets are missing; latest-client.json would be incomplete." >&2
    echo "  windows sha: ${win_sha:-(empty)}" >&2
    echo "  linux sha:   ${lin_sha:-(empty)}" >&2
    exit 1
  fi
fi

if [ -z "$win_sha" ] || [ -z "$lin_sha" ]; then
  echo "Skipping latest-client.json (partial build: only one platform present)"
  exit 0
fi

cat > latest-client.json <<EOF
{
  "version": "${VERSION_VALUE}",
  "released_at": "${RELEASED_AT}",
  "assets": {
    "windows-amd64": {
      "name": "${win_name}",
      "sha256": "${win_sha}"
    },
    "linux-amd64": {
      "name": "${lin_name}",
      "sha256": "${lin_sha}"
    }
  }
}
EOF

echo "Wrote latest-client.json"
