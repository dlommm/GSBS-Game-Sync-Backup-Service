#!/usr/bin/env bash
# Compile Tailwind CSS for the embedded WebUI.
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WEBUI="${ROOT}/server/webui"
cd "${WEBUI}"
if ! command -v npx >/dev/null 2>&1; then
  echo "npx not found; install Node.js to build WebUI CSS"
  exit 1
fi
npx --yes tailwindcss@3 -c tailwind.config.js -i static/src/input.css -o static/app.css --minify
echo "Built ${WEBUI}/static/app.css"
