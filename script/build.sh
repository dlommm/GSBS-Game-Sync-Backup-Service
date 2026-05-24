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
LDFLAGS="-X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"

mkdir -p "$OUT_DIR"

echo "Building GSBS version=${VERSION_VALUE} commit=${COMMIT} out=${OUT_DIR}"

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

build_windows() {
  export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
  go build -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-windows-amd64.exe" ./server
  echo "Built gsbs-server-windows-amd64.exe"
  go build -ldflags "-H windowsgui ${LDFLAGS}" -o "${OUT_DIR}/gsbs-client-windows-amd64.exe" ./client
  echo "Built gsbs-client-windows-amd64.exe"
}

build_linux() {
  export GOOS=linux GOARCH=amd64
  go build -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-linux-amd64" ./server
  echo "Built gsbs-server-linux-amd64"
  if [ "$HOST_GOOS" = "linux" ]; then
    go build -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-client-linux-amd64" ./client
    echo "Built gsbs-client-linux-amd64"
  else
    echo "Skipping gsbs-client-linux-amd64 (systray requires Linux host; use CI or linux-amd64 runner)"
  fi
}

build_darwin() {
  local arch="$1"
  export GOOS=darwin GOARCH="$arch" CGO_ENABLED=0
  go build -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-server-darwin-${arch}" ./server
  echo "Built gsbs-server-darwin-${arch}"
  go build -ldflags "$LDFLAGS" -o "${OUT_DIR}/gsbs-client-darwin-${arch}" ./client
  echo "Built gsbs-client-darwin-${arch}"
}

if want_platform windows-amd64; then
  build_windows
fi

if want_platform linux-amd64; then
  build_linux
fi

if want_platform darwin-amd64; then
  build_darwin amd64
fi

if want_platform darwin-arm64; then
  build_darwin arm64
fi

echo "Build complete: ${OUT_DIR}"
