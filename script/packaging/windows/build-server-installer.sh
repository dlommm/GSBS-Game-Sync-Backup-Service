#!/usr/bin/env bash
# Build Windows Inno Setup installer for gsbs-server.
# Usage: ./script/packaging/windows/build-server-installer.sh OUT_DIR VERSION
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
SERVER_BIN="${ROOT}/${OUT_DIR}/gsbs-server-windows-amd64.exe"
LAUNCHER="${SCRIPT_DIR}/gsbs-server-launcher.cmd"

if [ ! -f "$SERVER_BIN" ]; then
  echo "Missing server binary: $SERVER_BIN" >&2
  exit 1
fi
if [ ! -f "$LAUNCHER" ]; then
  echo "Missing launcher script: $LAUNCHER" >&2
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

WORK="${ROOT}/${OUT_DIR}/inno-work-server"
rm -rf "$WORK"
mkdir -p "$WORK"
cp "$SERVER_BIN" "$WORK/gsbs-server-windows-amd64.exe"
cp "$LAUNCHER" "$WORK/gsbs-server-launcher.cmd"

"$ISCC_PATH" \
  "/DMyAppVersion=${VERSION_VALUE}" \
  "/DSourceDir=${WORK}" \
  "/O${ROOT}/${OUT_DIR}" \
  "${SCRIPT_DIR}/gsbs-server.iss"

echo "Built ${OUT_DIR}/gsbs-server-setup-${VERSION_VALUE}-windows-amd64.exe"
