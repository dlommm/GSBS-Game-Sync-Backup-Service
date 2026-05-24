#!/usr/bin/env bash
# Build server and client for Windows amd64 and Linux amd64, create/update a GitHub release,
# and build + push the server Docker image to Docker Hub.
# Usage: ./script/release.sh [VERSION]
#   VERSION: e.g. v1.0.4 (default: prompt or derive from latest tag)
# Requires: go, git, gh (GitHub CLI), docker (buildx for multi-platform image); clean working tree for tagging; docker login for push.
#
# Env (optional): DOCKERHUB_IMAGE (default: dendlomm/gsbs-server) — image to build and push.
#                 BUILD_DARWIN=1 — also build darwin amd64/arm64 (dev/local).
#
# Artifacts:
#   - gsbs-server-windows-amd64.exe, gsbs-client-windows-amd64.exe
#   - gsbs-server-linux-amd64, gsbs-client-linux-amd64
#   - SHA256SUMS, latest-client.json
#   - (optional) BUILD_DARWIN=1: darwin builds
#   - Docker image: $DOCKERHUB_IMAGE:$VERSION and $DOCKERHUB_IMAGE:latest
#
# For CI-driven releases, push a git tag vX.Y.Z instead; see docs/RELEASE.md.

set -euo pipefail
cd "$(dirname "$0")/.."

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

VERSION_VALUE="${VERSION#v}"
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DOCKERHUB_IMAGE="${DOCKERHUB_IMAGE:-dendlomm/gsbs-server}"
export BUILD_DATE COMMIT RELEASED_AT="$BUILD_DATE"

echo "Release ${VERSION} (commit ${COMMIT})"

./script/build.sh "$VERSION" "$(pwd)"
./script/release-assets.sh "$VERSION" "$(pwd)"

# Tag if not already
if ! git rev-parse "$VERSION" >/dev/null 2>&1; then
  git tag -a "$VERSION" -m "Release $VERSION"
  git push origin "$VERSION"
fi

ASSETS=(SHA256SUMS latest-client.json gsbs-server-windows-amd64.exe gsbs-client-windows-amd64.exe gsbs-server-linux-amd64)
[ -f gsbs-client-linux-amd64 ] && ASSETS+=(gsbs-client-linux-amd64)
if [ "${BUILD_DARWIN:-0}" = "1" ]; then
  for arch in amd64 arm64; do
    [ -f "gsbs-server-darwin-${arch}" ] && ASSETS+=("gsbs-server-darwin-${arch}")
    [ -f "gsbs-client-darwin-${arch}" ] && ASSETS+=("gsbs-client-darwin-${arch}")
  done
fi

NOTES="Windows amd64 and Linux amd64 builds. Docker image: \`docker pull ${DOCKERHUB_IMAGE}:${VERSION_VALUE}\`. Client auto-update manifest: \`latest-client.json\`. Run with \`--version\` or \`-v\` to see version, build date, and commit."

if gh release view "$VERSION" >/dev/null 2>&1; then
  echo "Release $VERSION exists; uploading assets only."
  gh release upload "$VERSION" "${ASSETS[@]}" --clobber
else
  gh release create "$VERSION" \
    "${ASSETS[@]}" \
    --title "Release $VERSION" \
    --notes "$NOTES"
fi

echo "Building Docker image ${DOCKERHUB_IMAGE}:${VERSION_VALUE} (and :latest) for linux/amd64,linux/arm64"
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
