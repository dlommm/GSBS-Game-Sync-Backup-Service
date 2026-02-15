#!/usr/bin/env bash
# Build Windows exes with version/commit/build date and create a GitHub release.
# Usage: ./script/release.sh [VERSION]
#   VERSION: e.g. v1.0.3 (default: prompt or derive from tag)
# Requires: go, git, gh (GitHub CLI), and a clean working tree for tagging.

set -e
cd "$(dirname "$0")/.."

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

LDFLAGS="-X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"

echo "Building with Version=${VERSION_VALUE} BuildDate=${BUILD_DATE} Commit=${COMMIT}"

# Windows amd64 builds
export GOOS=windows
export GOARCH=amd64

# Server
go build -ldflags "$LDFLAGS" -o gsbs-server-windows-amd64.exe ./server
echo "Built gsbs-server-windows-amd64.exe"

# Client (tray app: -H windowsgui)
go build -ldflags "-H windowsgui $LDFLAGS" -o gsbs-client-windows-amd64.exe ./client
echo "Built gsbs-client-windows-amd64.exe"

# Tag if not already
if ! git rev-parse "$VERSION" >/dev/null 2>&1; then
  git tag -a "$VERSION" -m "Release $VERSION"
  git push origin "$VERSION"
fi

# Create or update release and upload assets
if gh release view "$VERSION" >/dev/null 2>&1; then
  echo "Release $VERSION exists; uploading assets only."
  gh release upload "$VERSION" gsbs-server-windows-amd64.exe gsbs-client-windows-amd64.exe --clobber
else
  gh release create "$VERSION" \
    gsbs-server-windows-amd64.exe \
    gsbs-client-windows-amd64.exe \
    --title "Release $VERSION" \
    --notes "Windows amd64 builds. Run with \`--version\` or \`-v\` to see version, build date, and commit."
fi

echo "Done. Release: $VERSION"
