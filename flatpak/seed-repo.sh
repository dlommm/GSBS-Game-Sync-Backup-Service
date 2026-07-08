#!/usr/bin/env bash
# Seed ./flatpak/repo with the currently published ostree repo so that
# build-flatpak.sh commits on top of the existing history instead of starting
# from scratch. That way update-repo.sh can generate old→new static deltas
# (users download the diff, not the whole app) and the commit chain stays
# continuous across releases.
#
# Usage: flatpak/seed-repo.sh
#
# Env:
#   GSBS_PAGES_GIT  git URL of the published Pages repo
#                   (default: https://github.com/dlommm/gsbs-flatpak.git)
#   GSBS_PAGES_DIR  scratch dir for the clone (default: a temp dir; the clone
#                   is deleted after seeding)
#
# Best-effort by design: if the clone fails or contains no usable repo, the
# build simply proceeds from an empty repo (full downloads, fresh history) —
# exactly the behavior before seeding existed — so publishing is never blocked.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPO_DIR="flatpak/repo"
PAGES_GIT="${GSBS_PAGES_GIT:-https://github.com/dlommm/gsbs-flatpak.git}"
PAGES_DIR="${GSBS_PAGES_DIR:-$(mktemp -d)/gsbs-pages}"

if [ -e "$REPO_DIR" ]; then
  echo "==> $REPO_DIR already exists — leaving it alone (delete it to reseed)"
  exit 0
fi

echo "==> Seeding $REPO_DIR from $PAGES_GIT"
rm -rf "$PAGES_DIR"
if ! git clone --quiet --depth 1 --branch gh-pages "$PAGES_GIT" "$PAGES_DIR"; then
  echo "WARNING: clone failed — building from an empty repo (no delta updates this release)" >&2
  exit 0
fi

if [ -d "$PAGES_DIR/repo/objects" ] && [ -f "$PAGES_DIR/repo/config" ]; then
  cp -R "$PAGES_DIR/repo" "$REPO_DIR"
  # git does not track empty dirs; ostree expects tmp/ when opening for writes.
  mkdir -p "$REPO_DIR/tmp"
  echo "==> Seeded refs:"
  (cd "$REPO_DIR/refs/heads" && find . -type f | sed 's|^\./|    |')
else
  echo "WARNING: published tree has no ostree repo — building from an empty repo" >&2
fi
rm -rf "$PAGES_DIR"
