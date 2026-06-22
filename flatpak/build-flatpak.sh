#!/usr/bin/env bash
# Build the GSBS client Flatpak into a local ostree repo (./flatpak/repo).
#
# Usage: flatpak/build-flatpak.sh [VERSION]
#   VERSION  e.g. v3.0.4 or 3.0.4 (default: dev build from git)
#
# Requires: flatpak, flatpak-builder, and the Freedesktop SDK + golang extension:
#   flatpak install -y flathub \
#     org.freedesktop.Platform//24.08 org.freedesktop.Sdk//24.08 \
#     org.freedesktop.Sdk.Extension.golang//24.08
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RAW_VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 0.0.0-dev)}"
VERSION="${RAW_VERSION#v}"
BUILD_DATE="$(git show -s --format=%cd --date=short HEAD 2>/dev/null || date -u +%Y-%m-%d)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

MANIFEST="flatpak/io.github.dlommm.GSBS.yaml"
GEN_MANIFEST="flatpak/io.github.dlommm.GSBS.gen.yaml"
BUILD_DIR="flatpak/build-dir"
REPO_DIR="flatpak/repo"

echo "==> Vendoring Go dependencies (offline, reproducible build)"
go mod vendor

echo "==> Generating manifest for version=${VERSION} commit=${COMMIT} date=${BUILD_DATE}"
sed \
  -e "s|^\( *GSBS_VERSION:\).*|\1 '${VERSION}'|" \
  -e "s|^\( *GSBS_BUILD_DATE:\).*|\1 '${BUILD_DATE}'|" \
  -e "s|^\( *GSBS_COMMIT:\).*|\1 '${COMMIT}'|" \
  "$MANIFEST" > "$GEN_MANIFEST"

echo "==> Building Flatpak"
flatpak-builder --force-clean --user \
  --install-deps-from=flathub \
  --repo="$REPO_DIR" \
  --default-branch=stable \
  "$BUILD_DIR" "$GEN_MANIFEST"

rm -f "$GEN_MANIFEST"
echo "==> Done. Local repo: ${REPO_DIR}"
echo "    Test install:  flatpak --user install ${REPO_DIR} io.github.dlommm.GSBS"
echo "    Or build+install directly:"
echo "    flatpak-builder --user --install --force-clean ${BUILD_DIR} ${MANIFEST}"
