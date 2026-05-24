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

APPDIR="${ROOT}/${OUT_DIR}/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/32x32/apps"

cp "$CLIENT_BIN" "$APPDIR/usr/bin/gsbs-client"
chmod +x "$APPDIR/usr/bin/gsbs-client"
cp "$SCRIPT_DIR/gsbs-client.desktop" "$APPDIR/usr/share/applications/"
cp "${ROOT}/client/icon_32.png" "$APPDIR/usr/share/icons/hicolor/32x32/apps/gsbs-client.png"
cp "$SCRIPT_DIR/AppRun" "$APPDIR/AppRun"
chmod +x "$APPDIR/AppRun"

LINUXDEPLOY="${LINUXDEPLOY:-linuxdeploy-x86_64.AppImage}"
PLUGIN="${LINUXDEPLOY_PLUGIN:-linuxdeploy-plugin-appimage-x86_64.AppImage}"

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
cd "$ROOT/${OUT_DIR}"

# AppIndicator libraries are expected on the host; bundle only the binary for a lightweight AppImage.
./"$LINUXDEPLOY" \
  --appdir "$APPDIR" \
  --desktop-file "$APPDIR/usr/share/applications/gsbs-client.desktop" \
  --icon-file "$APPDIR/usr/share/icons/hicolor/32x32/apps/gsbs-client.png" \
  --plugin "appimage" \
  --output appimage

mv gsbs-client-*-x86_64.AppImage "gsbs-client-${VERSION_VALUE}-x86_64.AppImage" 2>/dev/null || true
echo "Built ${OUT_DIR}/gsbs-client-${VERSION_VALUE}-x86_64.AppImage"
