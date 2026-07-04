#!/usr/bin/env bash
# Package the macOS GSBS client as a drag-to-Applications .app inside a .dmg.
# Must run on a macOS host (uses hdiutil / iconutil / sips).
#
# Usage: ./script/packaging/macos/build-dmg.sh OUT_DIR VERSION ARCH
#   ARCH: arm64 (Apple Silicon) or amd64 (Intel)
#
# The .app is UNSIGNED and un-notarized (no paid Apple Developer account), so
# Gatekeeper will flag it on first launch. The install docs tell users to run
# `xattr -cr /Applications/GSBS.app` (or right-click → Open) once.
set -euo pipefail

OUT_DIR="${1:-dist}"
VERSION="${2:-}"
ARCH="${3:-arm64}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 OUT_DIR VERSION ARCH" >&2
  exit 1
fi
if [ "$(uname)" != "Darwin" ]; then
  echo "build-dmg.sh must run on macOS (needs hdiutil/iconutil/sips)" >&2
  exit 1
fi
case "$ARCH" in
  arm64|amd64) ;;
  *) echo "Unsupported ARCH: $ARCH (use arm64 or amd64)" >&2; exit 1 ;;
esac

VERSION_VALUE="${VERSION#v}"
CLIENT_BIN="${ROOT}/${OUT_DIR}/gsbs-client-darwin-${ARCH}"
if [ ! -f "$CLIENT_BIN" ]; then
  echo "Missing client binary: $CLIENT_BIN" >&2
  exit 1
fi

BUNDLE_ID="io.github.dlommm.GSBS"
APP_NAME="GSBS"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
APP="${WORK}/${APP_NAME}.app"

# --- .app bundle layout ---
mkdir -p "${APP}/Contents/MacOS" "${APP}/Contents/Resources"
cp "$CLIENT_BIN" "${APP}/Contents/MacOS/gsbs-client"
chmod +x "${APP}/Contents/MacOS/gsbs-client"

# --- Icon: build a .icns from the highest-resolution logo we have ---
SRC_ICON="${ROOT}/assets/images/Logo-Icon-Only.png"
if [ -f "$SRC_ICON" ]; then
  ICONSET="${WORK}/GSBS.iconset"
  mkdir -p "$ICONSET"
  # A square 1024px master; the source is ~561px and nearly square already.
  MASTER="${WORK}/master-1024.png"
  sips -s format png -z 1024 1024 "$SRC_ICON" --out "$MASTER" >/dev/null
  for sz in 16 32 64 128 256 512; do
    sips -z "$sz" "$sz" "$MASTER" --out "${ICONSET}/icon_${sz}x${sz}.png" >/dev/null
    dbl=$((sz * 2))
    sips -z "$dbl" "$dbl" "$MASTER" --out "${ICONSET}/icon_${sz}x${sz}@2x.png" >/dev/null
  done
  iconutil -c icns "$ICONSET" -o "${APP}/Contents/Resources/GSBS.icns"
  ICON_LINE='<key>CFBundleIconFile</key><string>GSBS</string>'
else
  echo "WARNING: icon source not found ($SRC_ICON); building without a custom icon" >&2
  ICON_LINE=''
fi

# --- Info.plist. LSUIElement=true makes this a menubar/agent app (no Dock icon). ---
cat > "${APP}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>${APP_NAME}</string>
	<key>CFBundleDisplayName</key><string>GSBS</string>
	<key>CFBundleIdentifier</key><string>${BUNDLE_ID}</string>
	<key>CFBundleExecutable</key><string>gsbs-client</string>
	<key>CFBundleVersion</key><string>${VERSION_VALUE}</string>
	<key>CFBundleShortVersionString</key><string>${VERSION_VALUE}</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	${ICON_LINE}
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSHumanReadableCopyright</key><string>GSBS Contributors — MIT License</string>
</dict>
</plist>
PLIST

# --- Stage the DMG contents: the .app + a drag target to /Applications ---
STAGE="${WORK}/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "${STAGE}/Applications"

OUT_FILE="${ROOT}/${OUT_DIR}/gsbs-client-${VERSION_VALUE}-darwin-${ARCH}.dmg"
rm -f "$OUT_FILE"
hdiutil create \
  -volname "GSBS ${VERSION_VALUE}" \
  -srcfolder "$STAGE" \
  -fs HFS+ \
  -format UDZO \
  "$OUT_FILE" >/dev/null

echo "Built ${OUT_FILE}"
