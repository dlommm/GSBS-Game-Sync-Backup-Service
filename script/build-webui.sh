#!/usr/bin/env bash
# Compile Tailwind CSS for the embedded WebUI (server + client share the same compiled output).
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WEBUI="${ROOT}/server/webui"
CLIENT_STATIC="${ROOT}/client/webui/static"
cd "${WEBUI}"
if ! command -v npx >/dev/null 2>&1; then
  echo "npx not found; install Node.js to build WebUI CSS"
  exit 1
fi
npx --yes tailwindcss@3 -c tailwind.config.js -i static/src/input.css -o static/app.css --minify
echo "Built ${WEBUI}/static/app.css"

# Sync compiled CSS + shared static assets to client webui.
mkdir -p "${CLIENT_STATIC}/fonts"
cp "${WEBUI}/static/app.css" "${CLIENT_STATIC}/app.css"
cp "${WEBUI}/static/favicon.png" "${CLIENT_STATIC}/favicon.png"
cp "${ROOT}/docs/images/gsbs-icon.png" "${CLIENT_STATIC}/logo.png"
cp "${WEBUI}/static/fonts/"*.woff2 "${CLIENT_STATIC}/fonts/"
echo "Synced static assets to ${CLIENT_STATIC}"
