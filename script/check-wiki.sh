#!/usr/bin/env bash
# check-wiki.sh — Quality gates for wiki source pages in docs/wiki/.
#
# Checks:
#   1. All required wiki pages are present.
#   2. Every page has a level-1 heading (# Title).
#   3. Every page has a "## Related pages" section.
#   4. Code blocks have a language specifier.
#   5. No raw relative docs/ links remain (must be wiki links or full URLs).
#   6. No duplicate upgrade procedure headings outside Upgrading.md.
#   7. Sidebar lists all required pages.
#   8. Wiki Changelog includes the newest CHANGELOG.md release (staleness gate).
#
# Exit codes:
#   0  All checks passed.
#   1  One or more checks failed (details printed to stdout).
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WIKI_SRC="${REPO_ROOT}/docs/wiki"

PASS=true
ERRORS=()

log_err() {
  PASS=false
  ERRORS+=("$*")
  echo "  FAIL: $*"
}
log_ok() { echo "  OK:   $*"; }
log_warn() { echo "  WARN: $*"; }

# ---------------------------------------------------------------------------
# Check 1: Required pages
# ---------------------------------------------------------------------------

echo "[check-wiki] Check 1: Required pages present"

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

for page in "${REQUIRED_PAGES[@]}"; do
  filepath="${WIKI_SRC}/${page}.md"
  if [[ -f "${filepath}" ]]; then
    log_ok "${page}.md exists"
  else
    log_err "Missing required page: ${page}.md"
  fi
done

# ---------------------------------------------------------------------------
# Check 2: Every page has a level-1 heading
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 2: Level-1 heading present"

for file in "${WIKI_SRC}"/*.md; do
  filename="$(basename "${file}")"
  # Skip meta files
  if [[ "${filename}" == "README.md" || "${filename}" == "_Sidebar.md" ]]; then
    continue
  fi
  if grep -qE "^# .+" "${file}"; then
    log_ok "${filename}: has level-1 heading"
  else
    log_err "${filename}: missing level-1 heading (# Title)"
  fi
done

# ---------------------------------------------------------------------------
# Check 3: Every page has a "## Related pages" section
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 3: Related pages section present"

for file in "${WIKI_SRC}"/*.md; do
  filename="$(basename "${file}")"
  if [[ "${filename}" == "README.md" || "${filename}" == "_Sidebar.md" ]]; then
    continue
  fi
  if grep -qiE "^## Related pages" "${file}"; then
    log_ok "${filename}: has Related pages section"
  else
    log_err "${filename}: missing '## Related pages' section"
  fi
done

# ---------------------------------------------------------------------------
# Check 4: Code blocks have a language specifier
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 4: Code blocks have language specifier"

for file in "${WIKI_SRC}"/*.md; do
  filename="$(basename "${file}")"
  # Find lines that are bare ``` (opening fence with no language)
  # Exclude lines that are ``` alone (closing fences) by checking context is odd fence
  bare_fences=$(grep -n "^\`\`\`$" "${file}" 2>/dev/null || true)
  if [[ -n "${bare_fences}" ]]; then
    # Determine which bare fences are opening (every other ``` is a close)
    # Simple heuristic: warn if any bare ``` exists (may include closing fences)
    count=$(echo "${bare_fences}" | wc -l | tr -d ' ')
    log_warn "${filename}: ${count} bare code fence(s) — verify they are closing fences, not missing language tags"
  else
    log_ok "${filename}: all code fences have language specifiers (or no code blocks)"
  fi
done

# ---------------------------------------------------------------------------
# Check 5: No raw relative docs/ links (should be full URLs or wiki links)
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 5: No raw relative docs/ links"

for file in "${WIKI_SRC}"/*.md; do
  filename="$(basename "${file}")"
  if [[ "${filename}" == "README.md" ]]; then
    continue
  fi
  # Match patterns like (docs/FILENAME.md) or (../FILENAME.md) — raw relative links
  bad_links=$(grep -nEo '\((\.\./|docs/)[^)]+\.md\)' "${file}" 2>/dev/null || true)
  if [[ -n "${bad_links}" ]]; then
    log_err "${filename}: raw relative link(s) found — convert to wiki links or full GitHub URLs:"
    echo "${bad_links}" | while read -r line; do echo "    ${line}"; done
  else
    log_ok "${filename}: no raw relative docs links"
  fi
done

# ---------------------------------------------------------------------------
# Check 6: No duplicate upgrade procedure headings outside Upgrading.md
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 6: No duplicate upgrade procedures outside Upgrading.md"

UPGRADE_HEADING_PATTERN="^##+ (Docker Compose.*upgrade|Binary.*upgrade|Upgrade.*step|Before you upgrade|Backup procedure|Dry-run migration|Upgrade.*procedure)"

for file in "${WIKI_SRC}"/*.md; do
  filename="$(basename "${file}")"
  if [[ "${filename}" == "Upgrading.md" || "${filename}" == "README.md" || "${filename}" == "_Sidebar.md" ]]; then
    continue
  fi
  matches=$(grep -niE "${UPGRADE_HEADING_PATTERN}" "${file}" 2>/dev/null || true)
  if [[ -n "${matches}" ]]; then
    log_err "${filename}: duplicate upgrade procedure heading found — move to Upgrading.md:"
    echo "${matches}" | while read -r line; do echo "    ${line}"; done
  else
    log_ok "${filename}: no duplicate upgrade procedure headings"
  fi
done

# ---------------------------------------------------------------------------
# Check 7: Sidebar lists all required pages
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 7: Sidebar references all required pages"

SIDEBAR="${WIKI_SRC}/_Sidebar.md"
if [[ ! -f "${SIDEBAR}" ]]; then
  log_err "_Sidebar.md is missing"
else
  for page in "${REQUIRED_PAGES[@]}"; do
    # Match either [[Page Title|Page-Slug]] or [[PageTitle]] or (Page-Slug) patterns
    if grep -qE "(${page}|\[\[.*${page}.*\]\])" "${SIDEBAR}"; then
      log_ok "_Sidebar.md references ${page}"
    else
      log_err "_Sidebar.md does not reference ${page}"
    fi
  done
fi

# ---------------------------------------------------------------------------
# Check 8: Wiki Changelog is not stale
# ---------------------------------------------------------------------------

echo ""
echo "[check-wiki] Check 8: Wiki Changelog matches latest CHANGELOG.md release"

REPO_CHANGELOG="${REPO_ROOT}/CHANGELOG.md"
WIKI_CHANGELOG="${WIKI_SRC}/Changelog.md"
if [[ -f "${REPO_CHANGELOG}" && -f "${WIKI_CHANGELOG}" ]]; then
  # First "## [x.y.z]" heading in CHANGELOG.md, skipping [Unreleased].
  latest=$(grep -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' "${REPO_CHANGELOG}" | head -1 | tr -d '#[] ')
  if [[ -z "${latest}" ]]; then
    log_warn "could not determine latest release from CHANGELOG.md"
  elif grep -qF "[${latest}]" "${WIKI_CHANGELOG}"; then
    log_ok "Changelog.md mentions latest release ${latest}"
  else
    log_err "Changelog.md is stale: latest release ${latest} (from CHANGELOG.md) is missing — add a summary entry"
  fi
else
  log_warn "CHANGELOG.md or wiki Changelog.md missing; skipping staleness check"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
if [[ "${PASS}" == "true" ]]; then
  echo "[check-wiki] All checks passed."
  exit 0
else
  echo "[check-wiki] ${#ERRORS[@]} check(s) failed:"
  for err in "${ERRORS[@]}"; do
    echo "  - ${err}"
  done
  exit 1
fi
