#!/usr/bin/env bash
# Build .deb package for gsbs-client using nfpm.
# Usage: ./script/packaging/linux/build-deb.sh OUT_DIR VERSION
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
CLIENT_BIN="${ROOT}/${OUT_DIR}/gsbs-client-linux-amd64"
if [ ! -f "$CLIENT_BIN" ]; then
  echo "Missing client binary: $CLIENT_BIN" >&2
  exit 1
fi

WORK="${ROOT}/${OUT_DIR}/deb-work"
rm -rf "$WORK"
mkdir -p "$WORK"
cp "$CLIENT_BIN" "$WORK/gsbs-client-linux-amd64"
cp "$SCRIPT_DIR/gsbs-client.desktop" "$WORK/"
cp "${ROOT}/client/icon_32.png" "$WORK/gsbs-client.png"
cp "$SCRIPT_DIR/nfpm.yaml" "$WORK/"

export VERSION="$VERSION_VALUE"
cd "$WORK"
nfpm pkg \
  --config nfpm.yaml \
  --packager deb \
  --target "${ROOT}/${OUT_DIR}"

mv "${ROOT}/${OUT_DIR}/gsbs-client_${VERSION_VALUE}_amd64.deb" "${ROOT}/${OUT_DIR}/gsbs-client_${VERSION_VALUE}_amd64.deb" 2>/dev/null || true
# nfpm names output from config name-version-arch.deb pattern
DEB=$(ls -1 "${ROOT}/${OUT_DIR}/"*.deb 2>/dev/null | head -1 || true)
if [ -n "$DEB" ] && [ "$(basename "$DEB")" != "gsbs-client_${VERSION_VALUE}_amd64.deb" ]; then
  mv "$DEB" "${ROOT}/${OUT_DIR}/gsbs-client_${VERSION_VALUE}_amd64.deb"
fi
echo "Built ${OUT_DIR}/gsbs-client_${VERSION_VALUE}_amd64.deb"
