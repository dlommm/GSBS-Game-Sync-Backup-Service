#!/usr/bin/env bash
# Build server and client for Windows amd64 and Linux amd64, create/update a GitHub release,
# and build + push the server Docker image to Docker Hub.
# Usage: ./script/release.sh [VERSION]
#   VERSION: e.g. v1.0.4 (default: prompt or derive from latest tag)
# Requires: go, git, gh (GitHub CLI), docker (buildx for multi-platform image); clean working tree for tagging; docker login for push.
#
# Env (optional): DOCKERHUB_IMAGE (default: dendlomm/gsbs-server) — image to build and push.
#
# Artifacts:
#   - gsbs-server-windows-amd64.exe, gsbs-client-windows-amd64.exe
#   - gsbs-server-linux-amd64, gsbs-client-linux-amd64
#   - (optional) BUILD_DARWIN=1: gsbs-server-darwin-amd64, gsbs-client-darwin-amd64, gsbs-server-darwin-arm64, gsbs-client-darwin-arm64
#   - Docker image: $DOCKERHUB_IMAGE:$VERSION and $DOCKERHUB_IMAGE:latest

set -e
cd "$(dirname "$0")/.."
HOST_GOOS=$(go env GOOS)

# Version: from arg, or latest tag, or prompt
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || true)
  if [ -n "$LATEST_TAG" ]; then
    echo "Latest tag: $LATEST_TAG"
    read -r -p "New version (e.g. v1.0.4): " VERSION
  else
    read -r -p "Version for release (e.g. v1.0.3): " VERSION
  fi
fi
if [ -z "$VERSION" ]; then
  echo "Need a version (e.g. v1.0.3)"
  exit 1
fi
# Strip 'v' for ldflags if present
VERSION_VALUE="${VERSION#v}"
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DOCKERHUB_IMAGE="${DOCKERHUB_IMAGE:-dendlomm/gsbs-server}"

LDFLAGS="-X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"

echo "Building with Version=${VERSION_VALUE} BuildDate=${BUILD_DATE} Commit=${COMMIT}"

# --- Windows amd64 ---
export GOOS=windows
export GOARCH=amd64

go build -ldflags "$LDFLAGS" -o gsbs-server-windows-amd64.exe ./server
echo "Built gsbs-server-windows-amd64.exe"

# Client (tray app: -H windowsgui)
go build -ldflags "-H windowsgui $LDFLAGS" -o gsbs-client-windows-amd64.exe ./client
echo "Built gsbs-client-windows-amd64.exe"

# --- Linux amd64 ---
export GOOS=linux
export GOARCH=amd64

go build -ldflags "$LDFLAGS" -o gsbs-server-linux-amd64 ./server
echo "Built gsbs-server-linux-amd64"

# Client: systray does not cross-compile to Linux from non-Linux; build only on Linux host
if [ "$HOST_GOOS" = "linux" ]; then
  go build -ldflags "$LDFLAGS" -o gsbs-client-linux-amd64 ./client
  echo "Built gsbs-client-linux-amd64"
  HAVE_LINUX_CLIENT=1
else
  echo "Skipping gsbs-client-linux-amd64 (systray does not cross-compile to Linux from $HOST_GOOS); run this script on Linux to build it)"
  HAVE_LINUX_CLIENT=
fi

# --- Optional: macOS (darwin) amd64 + arm64 for local/dev ---
if [ "${BUILD_DARWIN:-0}" = "1" ]; then
  for arch in amd64 arm64; do
    export GOOS=darwin
    export GOARCH=$arch
    go build -ldflags "$LDFLAGS" -o "gsbs-server-darwin-${arch}" ./server
    echo "Built gsbs-server-darwin-${arch}"
    go build -ldflags "$LDFLAGS" -o "gsbs-client-darwin-${arch}" ./client
    echo "Built gsbs-client-darwin-${arch}"
  done
fi

# Tag if not already
if ! git rev-parse "$VERSION" >/dev/null 2>&1; then
  git tag -a "$VERSION" -m "Release $VERSION"
  git push origin "$VERSION"
fi

# Create or update release and upload all assets
ASSETS=(gsbs-server-windows-amd64.exe gsbs-client-windows-amd64.exe gsbs-server-linux-amd64)
[ -n "${HAVE_LINUX_CLIENT:-}" ] && [ -f gsbs-client-linux-amd64 ] && ASSETS+=(gsbs-client-linux-amd64)
if [ "${BUILD_DARWIN:-0}" = "1" ]; then
  ASSETS+=("gsbs-server-darwin-amd64" "gsbs-client-darwin-amd64" "gsbs-server-darwin-arm64" "gsbs-client-darwin-arm64")
fi
if gh release view "$VERSION" >/dev/null 2>&1; then
  echo "Release $VERSION exists; uploading assets only."
  gh release upload "$VERSION" "${ASSETS[@]}" --clobber
else
  gh release create "$VERSION" \
    "${ASSETS[@]}" \
    --title "Release $VERSION" \
    --notes "Windows amd64 and Linux amd64 builds. Docker image: \`docker pull ${DOCKERHUB_IMAGE:-dendlomm/gsbs-server}:${VERSION_VALUE}\`. Run with \`--version\` or \`-v\` to see version, build date, and commit."
fi

# Docker: multi-platform build and push to Docker Hub (linux/amd64 + linux/arm64)
DOCKERHUB_IMAGE="${DOCKERHUB_IMAGE:-dendlomm/gsbs-server}"
echo "Building Docker image ${DOCKERHUB_IMAGE}:${VERSION_VALUE} (and :latest) for linux/amd64,linux/arm64"
# Use a buildx builder that supports multi-platform (default driver often does not)
if ! docker buildx inspect gsbs-multi >/dev/null 2>&1; then
  docker buildx create --name gsbs-multi --use
fi
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="${VERSION_VALUE}" \
  --build-arg BUILD_DATE="${BUILD_DATE}" \
  --build-arg COMMIT="${COMMIT}" \
  -t "${DOCKERHUB_IMAGE}:${VERSION_VALUE}" \
  -t "${DOCKERHUB_IMAGE}:latest" \
  --push \
  .
echo "Pushed ${DOCKERHUB_IMAGE}:${VERSION_VALUE} and ${DOCKERHUB_IMAGE}:latest"

echo "Done. Release: $VERSION"
