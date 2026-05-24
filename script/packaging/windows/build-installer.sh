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

ISCC=""
if command -v iscc >/dev/null 2>&1; then
  ISCC="iscc"
elif [ -x "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" ]; then
  ISCC='"/c/Program Files (x86)/Inno Setup 6/ISCC.exe"'
elif [ -x "/c/Program Files/Inno Setup 6/ISCC.exe" ]; then
  ISCC='"/c/Program Files/Inno Setup 6/ISCC.exe"'
elif [ -n "${PROGRAMFILES(x86):-}" ] && [ -x "${PROGRAMFILES(x86)}/Inno Setup 6/ISCC.exe" ]; then
  ISCC="\"${PROGRAMFILES(x86)}/Inno Setup 6/ISCC.exe\""
else
  echo "Inno Setup compiler (iscc) not found" >&2
  exit 1
fi

WORK="${ROOT}/${OUT_DIR}/inno-work"
rm -rf "$WORK"
mkdir -p "$WORK"
cp "$CLIENT_BIN" "$WORK/gsbs-client-windows-amd64.exe"

# ISCC on Windows needs backslash paths
if [[ "$OSTYPE" == msys* ]] || [[ "$OSTYPE" == cygwin* ]] || [[ -n "${RUNNER_OS:-}" && "${RUNNER_OS}" == "Windows" ]]; then
  WORK_WIN=$(cygpath -w "$WORK" 2>/dev/null || echo "$WORK" | sed 's|/|\\|g')
  OUT_WIN=$(cygpath -w "${ROOT}/${OUT_DIR}" 2>/dev/null || echo "${ROOT}/${OUT_DIR}" | sed 's|/|\\|g')
  ISS_WIN=$(cygpath -w "$SCRIPT_DIR/gsbs-client.iss" 2>/dev/null || echo "$SCRIPT_DIR/gsbs-client.iss" | sed 's|/|\\|g')
  eval "$ISCC" "/DMyAppVersion=${VERSION_VALUE}" "/DSourceDir=${WORK_WIN}" "/O${OUT_WIN}" "\"${ISS_WIN}\""
else
  eval "$ISCC" "/DMyAppVersion=${VERSION_VALUE}" "/DSourceDir=${WORK}" "/O${ROOT}/${OUT_DIR}" "$SCRIPT_DIR/gsbs-client.iss"
fi

echo "Built ${OUT_DIR}/gsbs-client-setup-${VERSION_VALUE}-windows-amd64.exe"
