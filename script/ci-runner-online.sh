#!/usr/bin/env bash
# Mark this repo's self-hosted runner as online/offline for CI runner resolution.
# Run from the runner host (e.g. systemd ExecStartPre / ExecStopPost):
#   ./script/ci-runner-online.sh true
#   ./script/ci-runner-online.sh false
#
# Requires: gh CLI authenticated with repo admin (classic PAT repo scope, or gh auth login).
set -euo pipefail

repo="${GSBS_GITHUB_REPO:-dlommm/GSBS-Game-Sync-Backup-Service}"
state="${1:-}"

case "$state" in
  true|false) ;;
  *)
    echo "usage: $0 true|false" >&2
    exit 2
    ;;
esac

gh variable set GSBS_RUNNER_ONLINE --repo "$repo" --body "$state"
echo "GSBS_RUNNER_ONLINE=$state (repo $repo)"
