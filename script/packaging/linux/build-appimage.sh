#!/usr/bin/env bash
# Build AppImage for gsbs-client using linuxdeploy.
# Usage: ./script/packaging/linux/build-appimage.sh OUT_DIR VERSION
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
if ! command -v file >/dev/null 2>&1; then
  echo "file(1) is required for appimagetool (e.g. apt install file)" >&2
  exit 1
fi

APPDIR="${ROOT}/${OUT_DIR}/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/32x32/apps"

cp "$CLIENT_BIN" "$APPDIR/usr/bin/gsbs-client"
chmod +x "$APPDIR/usr/bin/gsbs-client"
cp "$SCRIPT_DIR/gsbs-client.desktop" "$APPDIR/usr/share/applications/"
cp "${ROOT}/client/icon_32.png" "$APPDIR/usr/share/icons/hicolor/32x32/apps/gsbs-client.png"
cp "$SCRIPT_DIR/AppRun" "$APPDIR/AppRun"
chmod +x "$APPDIR/AppRun"

DEPLOY_DIR="${ROOT}/${OUT_DIR}"
LINUXDEPLOY="${DEPLOY_DIR}/linuxdeploy-x86_64.AppImage"
PLUGIN="${DEPLOY_DIR}/linuxdeploy-plugin-appimage-x86_64.AppImage"

if [ ! -f "$LINUXDEPLOY" ]; then
  curl -fsSL -o "$LINUXDEPLOY" "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage"
  chmod +x "$LINUXDEPLOY"
fi
if [ ! -f "$PLUGIN" ]; then
  curl -fsSL -o "$PLUGIN" "https://github.com/linuxdeploy/linuxdeploy-plugin-appimage/releases/download/continuous/linuxdeploy-plugin-appimage-x86_64.AppImage"
  chmod +x "$PLUGIN"
fi

export ARCH=x86_64
export VERSION="$VERSION_VALUE"
# CI and containerized runners often lack FUSE; extract-and-run avoids fusermount.
export APPIMAGE_EXTRACT_AND_RUN=1
cd "$DEPLOY_DIR"

"$LINUXDEPLOY" \
  --appdir "$APPDIR" \
  --desktop-file "$APPDIR/usr/share/applications/gsbs-client.desktop" \
  --icon-file "$APPDIR/usr/share/icons/hicolor/32x32/apps/gsbs-client.png" \
  --output appimage

OUT_FILE="gsbs-client-${VERSION_VALUE}-x86_64.AppImage"
shopt -s nullglob
for candidate in gsbs-client-*-x86_64.AppImage GSBS_Client-*-x86_64.AppImage *-*-x86_64.AppImage; do
  mv "$candidate" "$OUT_FILE"
  break
done
shopt -u nullglob
if [ ! -f "$OUT_FILE" ]; then
  echo "AppImage output not found in ${DEPLOY_DIR}" >&2
  ls -la ./*.AppImage 2>/dev/null || true
  exit 1
fi
echo "Built ${DEPLOY_DIR}/${OUT_FILE}"
