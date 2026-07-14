#!/usr/bin/env bash
# sync-wiki.sh — Transform docs/wiki/ source pages and publish to the GitHub wiki.
#
# Usage:
#   ./script/sync-wiki.sh [--dry-run] [--out-dir <dir>]
#
# Environment variables (for CI):
#   WIKI_REPO    full clone URL of the .wiki.git repo
#                (default: derived from GITHUB_REPOSITORY)
#   GITHUB_TOKEN  token with write access to the wiki repo (CI provides this)
#   GH_ACTOR     committer username (defaults to "github-actions[bot]")
#   GH_REF       tag or branch ref for the commit message (optional)
#
# Behaviour:
#   1. Copies docs/wiki/*.md to a staging directory.
#   2. Rewrites relative wiki-internal links to GitHub wiki link format.
#   3. Rewrites relative repo links (docs/*.md, *.md in root) to full GitHub blob URLs.
#   4. Validates required page files are present.
#   5. Clones the .wiki.git repo (or uses --out-dir in dry-run mode).
#   6. Copies transformed pages over, including _Sidebar.md.
#   7. Commits and pushes only when content has changed.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WIKI_SRC="${REPO_ROOT}/docs/wiki"
STAGING_DIR="$(mktemp -d)"
DRY_RUN=false
OUT_DIR=""

# GitHub repository (e.g. dlommm/GSBS-Game-Sync-Backup-Service)
GITHUB_REPOSITORY="${GITHUB_REPOSITORY:-dlommm/GSBS-Game-Sync-Backup-Service}"
GITHUB_SERVER_URL="${GITHUB_SERVER_URL:-https://github.com}"
REPO_BLOB_BASE="${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/blob/main"
RAW_BASE="https://raw.githubusercontent.com/${GITHUB_REPOSITORY}/main"

GH_ACTOR="${GH_ACTOR:-github-actions[bot]}"
GH_REF="${GH_REF:-}"

# Required wiki pages (filename without .md extension).
REQUIRED_PAGES=(
  "Home"
  "Installation"
  "Client-Setup-and-Usage"
  "Server-Configuration"
  "How-It-Works"
  "Troubleshooting"
  "Upgrading"
  "API-Reference"
  "Changelog"
  "FAQ"
  "Contributing"
)

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    --out-dir) OUT_DIR="$2"; shift 2 ;;
    *) echo "Unknown argument: $1"; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

log()  { echo "[sync-wiki] $*"; }
err()  { echo "[sync-wiki] ERROR: $*" >&2; exit 1; }
warn() { echo "[sync-wiki] WARN: $*" >&2; }

cleanup() { rm -rf "${STAGING_DIR}"; }
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Step 1: Validate required pages exist
# ---------------------------------------------------------------------------

log "Validating required wiki pages in ${WIKI_SRC}..."
MISSING=()
for page in "${REQUIRED_PAGES[@]}"; do
  if [[ ! -f "${WIKI_SRC}/${page}.md" ]]; then
    MISSING+=("${page}.md")
  fi
done
if [[ ${#MISSING[@]} -gt 0 ]]; then
  err "Missing required wiki pages: ${MISSING[*]}"
fi
log "All ${#REQUIRED_PAGES[@]} required pages found."

# ---------------------------------------------------------------------------
# Step 2: Copy and transform pages to staging
# ---------------------------------------------------------------------------

log "Copying and transforming pages to staging: ${STAGING_DIR}"
cp "${WIKI_SRC}"/*.md "${STAGING_DIR}/"

# Transform each .md file in staging
for file in "${STAGING_DIR}"/*.md; do
  filename="$(basename "${file}")"

  # --- Convert relative docs links to full GitHub blob URLs ---
  # Pattern: links like (../CHANGELOG.md) or (docs/ARCHITECTURE.md) or (SECURITY.md)
  # that do NOT target a known wiki page name.

  # First, build a pipe-separated list of wiki page slugs for exclusion
  wiki_slugs=$(IFS="|"; echo "${REQUIRED_PAGES[*]}")

  # Rewrite: (FILENAME.md) → full blob URL, unless it's a known wiki page slug
  # This sed handles root-level .md files like (SECURITY.md), (CONTRIBUTING.md), (CHANGELOG.md)
  sed -i.bak \
    -E "s|\(([A-Z][A-Za-z_-]+)\.md\)|(${REPO_BLOB_BASE}/\1.md)|g" \
    "${file}"

  # Rewrite: (docs/FILENAME.md) → full blob URL
  sed -i.bak \
    -E "s|\(docs/([A-Za-z_-]+)\.md\)|(${REPO_BLOB_BASE}/docs/\1.md)|g" \
    "${file}"

  # Rewrite: (../docs/FILENAME.md) → full blob URL
  sed -i.bak \
    -E "s|\(\.\./docs/([A-Za-z_-]+)\.md\)|(${REPO_BLOB_BASE}/docs/\1.md)|g" \
    "${file}"

  # Rewrite: (docs/examples/FILENAME.md) → full blob URL
  sed -i.bak \
    -E "s|\(docs/examples/([A-Za-z_-]+)\.md\)|(${REPO_BLOB_BASE}/docs/examples/\1.md)|g" \
    "${file}"

  # Rewrite: (.github/workflows/FILENAME.yml) → full blob URL
  sed -i.bak \
    -E "s|\(\.github/workflows/([A-Za-z_.-]+)\)|(${REPO_BLOB_BASE}/.github/workflows/\1)|g" \
    "${file}"

  # Rewrite: known wiki-internal links (Page-Slug) → keep as-is (GitHub wiki resolves them)
  # These are already in the correct form; no rewrite needed.

  # Remove .bak files
  rm -f "${file}.bak"
done

log "Transformation complete."

# ---------------------------------------------------------------------------
# Step 3: Validate no duplicate upgrade procedure blocks outside Upgrading.md
# ---------------------------------------------------------------------------

log "Checking for duplicate upgrade procedure blocks..."
UPGRADE_VIOLATIONS=()
for file in "${STAGING_DIR}"/*.md; do
  filename="$(basename "${file}")"
  if [[ "${filename}" == "Upgrading.md" ]]; then
    continue
  fi
  # Flag any headings that duplicate upgrade procedure step blocks
  if grep -qiE "^##+ (Docker Compose.*upgrade|Binary.*upgrade|Upgrade.*step|Before you upgrade|Backup procedure|Dry-run migration)" "${file}" 2>/dev/null; then
    UPGRADE_VIOLATIONS+=("${filename}")
  fi
done
if [[ ${#UPGRADE_VIOLATIONS[@]} -gt 0 ]]; then
  warn "Possible duplicate upgrade procedure headings in: ${UPGRADE_VIOLATIONS[*]}"
  warn "All step-by-step upgrade procedures must live in Upgrading.md."
  # Warn only (not a hard error) — reviewers must verify intent
fi

# ---------------------------------------------------------------------------
# Step 4: Dry-run mode — output to --out-dir and exit
# ---------------------------------------------------------------------------

if [[ "${DRY_RUN}" == "true" ]]; then
  if [[ -n "${OUT_DIR}" ]]; then
    mkdir -p "${OUT_DIR}"
    cp "${STAGING_DIR}"/*.md "${OUT_DIR}/"
    log "Dry-run: transformed pages written to ${OUT_DIR}"
  else
    log "Dry-run: pages staged at ${STAGING_DIR} (not pushed)"
  fi
  log "Dry-run complete. No wiki push performed."
  exit 0
fi

# ---------------------------------------------------------------------------
# Step 5: Clone the wiki repo
# ---------------------------------------------------------------------------

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  err "GITHUB_TOKEN is not set. Cannot push to wiki."
fi

WIKI_REPO="${WIKI_REPO:-https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.wiki.git}"
WIKI_CLONE_DIR="$(mktemp -d)"
trap 'rm -rf "${STAGING_DIR}" "${WIKI_CLONE_DIR}"' EXIT

log "Cloning wiki repo..."
git clone --quiet "${WIKI_REPO}" "${WIKI_CLONE_DIR}" || {
  log "Wiki repo does not exist yet or clone failed — treating as empty."
  git init "${WIKI_CLONE_DIR}"
  cd "${WIKI_CLONE_DIR}"
  git checkout -b master
  git remote add origin "${WIKI_REPO}"

  # If the remote is still unreachable (wiki disabled or token lacks access),
  # avoid failing the whole workflow and emit a clear action message instead.
  if ! git ls-remote --exit-code origin >/dev/null 2>&1; then
    warn "Wiki remote is not reachable; skipping publish."
    warn "Ensure GitHub wiki is enabled and workflow credentials can access ${GITHUB_REPOSITORY}.wiki.git."
    exit 0
  fi
  cd -
}

# ---------------------------------------------------------------------------
# Step 6: Copy transformed pages to wiki clone
# ---------------------------------------------------------------------------

log "Copying transformed pages to wiki clone..."
cp "${STAGING_DIR}"/*.md "${WIKI_CLONE_DIR}/"

# ---------------------------------------------------------------------------
# Step 7: Commit and push only when content changed
# ---------------------------------------------------------------------------

cd "${WIKI_CLONE_DIR}"

git config user.email "${GH_ACTOR}@users.noreply.github.com"
git config user.name "${GH_ACTOR}"

git add --all

if git diff --cached --quiet; then
  log "No content changes. Wiki is already up to date."
  exit 0
fi

REF_MSG=""
if [[ -n "${GH_REF}" ]]; then
  REF_MSG=" (${GH_REF})"
fi

COMMIT_MSG="docs: sync wiki from repo${REF_MSG} [skip ci]"
git commit -m "${COMMIT_MSG}"

log "Pushing wiki changes..."
git push origin HEAD

log "Wiki published successfully."
