#!/usr/bin/env bash
# Finalize the local ostree repo for static hosting (GitHub Pages, Cloudflare,
# any web server) and write the .flatpakrepo descriptor users add once.
#
# Usage: flatpak/update-repo.sh
#
# Env:
#   GSBS_REPO_URL   public URL the repo will be served from
#                   (default: https://dlommm.github.io/gsbs-flatpak/repo)
#   GSBS_GPG_KEY    GPG key id used to sign the repo (optional but recommended)
#   GSBS_GPG_HOME   GnuPG home dir holding the private key (default: ~/.gnupg)
#
# After running, publish the ./flatpak/repo directory to your static host
# (e.g. push it to the gh-pages branch of the dlommm/gsbs-flatpak repository).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPO_DIR="flatpak/repo"
REPO_URL="${GSBS_REPO_URL:-https://dlommm.github.io/gsbs-flatpak/repo}"

if [ ! -d "$REPO_DIR" ]; then
  echo "error: $REPO_DIR not found — run build-flatpak.sh first" >&2
  exit 1
fi

SIGN_ARGS=()
GPGKEY_LINE=""
if [ -n "${GSBS_GPG_KEY:-}" ]; then
  GPG_HOME="${GSBS_GPG_HOME:-$HOME/.gnupg}"
  SIGN_ARGS=(--gpg-sign="$GSBS_GPG_KEY" --gpg-homedir="$GPG_HOME")
  echo "==> Exporting public key"
  gpg --homedir "$GPG_HOME" --export "$GSBS_GPG_KEY" > "$REPO_DIR/gsbs.gpg"
  GPGKEY_LINE="GPGKey=$(base64 -w0 < "$REPO_DIR/gsbs.gpg" 2>/dev/null || base64 < "$REPO_DIR/gsbs.gpg" | tr -d '\n')"
else
  echo "WARNING: no GSBS_GPG_KEY set — building an UNSIGNED repo (not recommended for public use)" >&2
fi

if [ ${#SIGN_ARGS[@]} -gt 0 ]; then
  # build-update-repo only signs the summary, NOT the individual commits, so a
  # client with GPGKey= set would fail to pull with "no signatures found".
  # Sign every ref's commit here (writes the .commitmeta detached signatures).
  echo "==> Signing commits"
  while IFS= read -r ref; do
    [ -n "$ref" ] || continue
    IFS=/ read -r _kind id arch branch <<<"$ref"
    flatpak build-sign "$REPO_DIR" "$id" "$branch" --arch="$arch" "${SIGN_ARGS[@]}"
  done < <(cd "$REPO_DIR/refs/heads" && find . -type f | sed 's|^\./||')
fi

echo "==> Generating static deltas + summary"
flatpak build-update-repo --generate-static-deltas "${SIGN_ARGS[@]}" "$REPO_DIR"

echo "==> Writing $REPO_DIR/gsbs.flatpakrepo"
{
  echo "[Flatpak Repo]"
  echo "Title=GSBS"
  echo "Url=${REPO_URL}"
  echo "Homepage=https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-"
  echo "Comment=Game Sync & Backup Service — sync, backup, protect your saves"
  echo "Description=Official GSBS client Flatpak repository"
  echo "Icon=${REPO_URL%/repo}/io.github.dlommm.GSBS.png"
  [ -n "$GPGKEY_LINE" ] && echo "$GPGKEY_LINE"
} > "$REPO_DIR/gsbs.flatpakrepo"

echo "==> Done. Publish the '$REPO_DIR' directory to: ${REPO_URL}"
echo "    Users then run:"
echo "      flatpak remote-add --if-not-exists gsbs ${REPO_URL}/gsbs.flatpakrepo"
echo "      flatpak install gsbs io.github.dlommm.GSBS"
