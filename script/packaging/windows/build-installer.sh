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

# GitHub Actions Windows bash accepts mixed paths; keep args as single quoted tokens.
"$ISCC_PATH" \
  "/DMyAppVersion=${VERSION_VALUE}" \
  "/DSourceDir=${WORK}" \
  "/O${ROOT}/${OUT_DIR}" \
  "${SCRIPT_DIR}/gsbs-client.iss"

echo "Built ${OUT_DIR}/gsbs-client-setup-${VERSION_VALUE}-windows-amd64.exe"
