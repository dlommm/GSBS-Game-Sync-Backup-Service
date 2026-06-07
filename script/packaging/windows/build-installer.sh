#!/usr/bin/env bash
# Build Windows Inno Setup installer for gsbs-client.
# Usage: ./script/packaging/windows/build-installer.sh OUT_DIR VERSION
set -euo pipefail

OUT_DIR="${1:-dist}"
VERSION="${2:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 OUT_DIR VERSION" >&2
  exit 1
fi

VERSION_VALUE="${VERSION#v}"
CLIENT_BIN="${ROOT}/${OUT_DIR}/gsbs-client-windows-amd64.exe"
if [ ! -f "$CLIENT_BIN" ]; then
  echo "Missing client binary: $CLIENT_BIN" >&2
  exit 1
fi

ISCC_PATH=""
for candidate in \
  "$(command -v iscc 2>/dev/null || true)" \
  "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" \
  "/c/Program Files/Inno Setup 6/ISCC.exe"; do
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    ISCC_PATH="$candidate"
    break
  fi
done
if [ -z "$ISCC_PATH" ] && [ -n "${PROGRAMFILES(x86):-}" ] && [ -x "${PROGRAMFILES(x86)}/Inno Setup 6/ISCC.exe" ]; then
  ISCC_PATH="${PROGRAMFILES(x86)}/Inno Setup 6/ISCC.exe"
fi
if [ -z "$ISCC_PATH" ]; then
  echo "Inno Setup compiler (iscc) not found" >&2
  exit 1
fi

WORK="${ROOT}/${OUT_DIR}/inno-work"
rm -rf "$WORK"
mkdir -p "$WORK"
cp "$CLIENT_BIN" "$WORK/gsbs-client-windows-amd64.exe"

to_iscc_path() {
  local p="$1"
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -aw "$p"
  else
    printf '%s\n' "$p"
  fi
}

WORK_PATH="$(to_iscc_path "$WORK")"
OUT_PATH="$(to_iscc_path "${ROOT}/${OUT_DIR}")"
ISS_PATH="$(to_iscc_path "${SCRIPT_DIR}/gsbs-client.iss")"

"$ISCC_PATH" \
  "/DMyAppVersion=${VERSION_VALUE}" \
  "/DSourceDir=${WORK_PATH}" \
  "/O${OUT_PATH}" \
  "${ISS_PATH}"

echo "Built ${OUT_DIR}/gsbs-client-setup-${VERSION_VALUE}-windows-amd64.exe"
