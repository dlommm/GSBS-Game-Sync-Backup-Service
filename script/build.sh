#!/usr/bin/env bash
# Build GSBS server and client binaries for release.
# Usage: ./script/build.sh VERSION [OUT_DIR] [PLATFORMS]
#   VERSION: e.g. v1.0.14 or 1.0.14
#   OUT_DIR: output directory (default: repo root)
#   PLATFORMS: comma-separated list (default: all available on host)
#     windows-amd64, linux-amd64, darwin-amd64, darwin-arm64
#
# Env:
#   BUILD_WEBUI=0     skip WebUI CSS build
#   REGEN_ICONS=1     regenerate branding assets from assets/images/ first
#   BUILD_DARWIN=1    include darwin targets when PLATFORMS unset
#   COMMIT            override git commit (default: short HEAD)
#   BUILD_DATE        override build date (default: UTC now)

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-}"
OUT_DIR="${2:-$ROOT}"
PLATFORMS="${3:-}"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 VERSION [OUT_DIR] [PLATFORMS]" >&2
  exit 1
fi

VERSION_VALUE="${VERSION#v}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
HOST_GOOS="$(go env GOOS)"
LDFLAGS="-s -w -X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"

# Numeric major.minor.patch for the Windows exe resource (FixedFileInfo).
SEMVER="$(printf '%s' "$VERSION_VALUE" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true)"
SEMVER="${SEMVER:-0.0.0}"
VER_MAJOR="${SEMVER%%.*}"; VER_REST="${SEMVER#*.}"
VER_MINOR="${VER_REST%%.*}"; VER_PATCH="${VER_REST##*.}"

mkdir -p "$OUT_DIR"

echo "Building GSBS version=${VERSION_VALUE} commit=${COMMIT} out=${OUT_DIR}"

if [ "${REGEN_ICONS:-0}" = "1" ]; then
  echo "Regenerating branding assets from assets/images/ ..."
  go run ./cmd/gen-branding
fi

if [ "${BUILD_WEBUI:-1}" != "0" ] && [ -f script/build-webui.sh ]; then
  ./script/build-webui.sh
fi

want_platform() {
  local name="$1"
  if [ -z "$PLATFORMS" ]; then
    case "$name" in
      darwin-*)
        [ "${BUILD_DARWIN:-0}" = "1" ]
        ;;
      linux-amd64)
        [ "$HOST_GOOS" = "linux" ] || [ -z "$PLATFORMS" ]
        ;;
      *)
        return 0
        ;;
    esac
    return
  fi
  [[ ",${PLATFORMS}," == *",${name},"* ]]
}

# gen_windows_resource embeds the app icon + version info into the client exe
# via a generated client/resource_windows.syso (linked automatically by the
# _windows filename). No-op with a warning if goversioninfo isn't installed.
gen_windows_resource() {
  if ! command -v goversioninfo >/dev/null 2>&1; then
    echo "WARNING: goversioninfo not found — client exe will have no embedded icon/version." >&2
    echo "  install: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest" >&2
    return 0
  fi
  goversioninfo -64 \
    -icon "client/icon.ico" \
    -company "GSBS" \
    -product-name "GSBS Client" \
    -description "GSBS game-save sync client" \
    -file-version "$VERSION_VALUE" \
    -product-version "$VERSION_VALUE" \
    -ver-major "$VER_MAJOR" -ver-minor "$VER_MINOR" -ver-patch "$VER_PATCH" \
    -o "client/resource_windows.syso" \
    "client/versioninfo.json"
  echo "Generated client/resource_windows.syso (v${VERSION_VALUE})"
}

build_windows() {
  export GOOS=windows GOARCH=amd64 CGO_ENABLED=1
  # go-sqlite3 requires CGO for the server binary on Windows.
  go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-windows-amd64.exe" ./server
  echo "Built gsbs-server-windows-amd64.exe"
  export CGO_ENABLED=0
  gen_windows_resource
  go build -trimpath -ldflags "-H windowsgui ${LDFLAGS}" -o "${OUT_DIR}/gsbs-client-windows-amd64.exe" ./client
  echo "Built gsbs-client-windows-amd64.exe"
  rm -f client/resource_windows.syso
}

build_linux() {
  export GOOS=linux GOARCH=amd64
  go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-linux-amd64" ./server
  echo "Built gsbs-server-linux-amd64"
  if [ "$HOST_GOOS" = "linux" ]; then
    go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-client-linux-amd64" ./client
    echo "Built gsbs-client-linux-amd64"
  else
    echo "Skipping gsbs-client-linux-amd64 (systray requires Linux host; use CI or linux-amd64 runner)"
  fi
}

build_darwin() {
  local arch="$1"
  # The macOS systray uses AppKit via cgo, and go-sqlite3 needs cgo too, so the
  # darwin binaries must be built natively on a macOS host (no cross-compile).
  if [ "$HOST_GOOS" != "darwin" ]; then
    echo "Skipping darwin-${arch} (must build on a macOS host; use the macos CI runner)"
    return 0
  fi
  export GOOS=darwin GOARCH="$arch" CGO_ENABLED=1
  go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-darwin-${arch}" ./server
  echo "Built gsbs-server-darwin-${arch}"
  go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-client-darwin-${arch}" ./client
  echo "Built gsbs-client-darwin-${arch}"
}

build_linux_arm64() {
  # The Linux client is pure Go (D-Bus tray), so it cross-compiles cleanly.
  # The server stays Docker-multiarch (cgo go-sqlite3); we ship the client here.
  export GOOS=linux GOARCH=arm64 CGO_ENABLED=0
  go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-client-linux-arm64" ./client
  echo "Built gsbs-client-linux-arm64"
}

if want_platform windows-amd64; then
  build_windows
fi

if want_platform linux-amd64; then
  build_linux
fi

if want_platform linux-arm64; then
  build_linux_arm64
fi

if want_platform darwin-amd64; then
  build_darwin amd64
fi

if want_platform darwin-arm64; then
  build_darwin arm64
fi

echo "Build complete: ${OUT_DIR}"
