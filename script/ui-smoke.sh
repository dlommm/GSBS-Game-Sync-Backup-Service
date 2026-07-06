#!/usr/bin/env bash
# WebUI smoke test: boots a throwaway server, completes the setup wizard,
# logs in, and sweeps every WebUI route asserting HTTP 200, no template
# errors, and the strict CSP header. Catches template execution failures on
# real (seeded) data — run before releases and after WebUI changes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PORT=18095
BASE="http://127.0.0.1:${PORT}"
JAR="${TMP}/cookies.txt"
PASS=0
FAIL=0

cleanup() {
  [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

echo "[ui-smoke] building server…"
go build -o "${TMP}/gsbs-server" "${ROOT}/server"

echo "[ui-smoke] booting on ${BASE} (data in ${TMP})…"
GSBS_DB="${TMP}/gsbs.db" GSBS_SAVE_ROOT="${TMP}/saves" GSBS_ADDR="127.0.0.1:${PORT}" \
  "${TMP}/gsbs-server" >"${TMP}/server.log" 2>&1 &
SERVER_PID=$!
for i in $(seq 1 50); do
  if curl -sf -o /dev/null "${BASE}/setup"; then break; fi
  sleep 0.2
done

# Complete the setup wizard (creates the admin) and log in.
CSRF=$(curl -sc "$JAR" "${BASE}/setup" | grep -o 'name="csrf" value="[^"]*"' | head -1 | sed 's/.*value="//;s/"//')
curl -s -b "$JAR" -c "$JAR" -X POST "${BASE}/setup" \
  --data-urlencode "csrf=${CSRF}" \
  --data-urlencode "username=smokeadmin" \
  --data-urlencode "password=smoke-test-passw0rd" \
  --data-urlencode "confirm_password=smoke-test-passw0rd" \
  -o /dev/null
# The wizard auto-logs-in the first user; the jar now carries the session.
if ! curl -s -b "$JAR" -o /dev/null -w '%{http_code}' "${BASE}/dashboard" | grep -q 200; then
  echo "[ui-smoke] setup/auto-login failed"; tail -20 "${TMP}/server.log"; exit 1
fi

check() {
  local path="$1"
  local body status csp
  body="$(curl -s -b "$JAR" -w '\n%{http_code}' "${BASE}${path}")"
  status="${body##*$'\n'}"
  body="${body%$'\n'*}"
  csp="$(curl -s -b "$JAR" -o /dev/null -D - "${BASE}${path}" | grep -i '^content-security-policy:' || true)"
  local ok=1
  [ "$status" = "200" ] || { echo "  FAIL ${path}: HTTP ${status}"; ok=0; }
  if echo "$body" | grep -q "Template error"; then echo "  FAIL ${path}: template error"; ok=0; fi
  if [ -z "$csp" ] || echo "$csp" | grep -qi "unsafe-inline"; then echo "  FAIL ${path}: weak/missing CSP"; ok=0; fi
  if [ "$ok" = "1" ]; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); fi
}

# Auth pages checked without the session (logged-in users are redirected away).
check_anon() {
  local path="$1" status
  status="$(curl -s -o /dev/null -w '%{http_code}' "${BASE}${path}")"
  if [ "$status" = "200" ]; then PASS=$((PASS+1)); else echo "  FAIL ${path} (anon): HTTP ${status}"; FAIL=$((FAIL+1)); fi
}
echo "[ui-smoke] sweeping auth pages (anonymous)…"
check_anon /login
check_anon /register

echo "[ui-smoke] sweeping user routes…"
for p in /dashboard /dashboard/games "/dashboard/games?view=list" \
         /dashboard/clients /dashboard/analytics "/dashboard/analytics?days=7" "/dashboard/analytics?days=90" \
         /dashboard/settings /dashboard/settings/2fa/enable \
         /dashboard/partial/stats /dashboard/partial/clients /dashboard/partial/activity \
         "/dashboard/partial/activity?offset=20" \
         "/dashboard/partial/games?view=grid" /dashboard/partial/clients-list "/dashboard/partial/search?q=a"; do
  check "$p"
done

echo "[ui-smoke] sweeping admin routes…"
for p in /admin /admin/users /admin/manifest /admin/activity /admin/logs /admin/settings \
         "/admin/analytics?tab=overview" "/admin/analytics?tab=fleet" "/admin/analytics?tab=fleet&days=7" \
         "/admin/analytics?tab=pcgw" "/admin/analytics?tab=sync" "/admin/analytics?tab=overview&window=90" \
         /admin/partial/audit "/admin/partial/audit?action=run_job&q=x&page=2&per=10" \
         /admin/partial/fetches "/admin/partial/fetches?page=2" \
         /admin/partial/snapshots "/admin/partial/snapshots?per=50" \
         "/admin/partial/jobs?context=activity" "/admin/partial/jobs?context=activity&job=pcgw_sync&status=failed&page=2" \
         "/admin/partial/logs?offset=200" \
         /admin/pcgw; do
  check "$p"
done

echo "[ui-smoke] checking branded 404…"
body="$(curl -s -b "$JAR" -w '\n%{http_code}' "${BASE}/this-page-does-not-exist")"
if [ "${body##*$'\n'}" = "404" ] && echo "$body" | grep -q "Page not found"; then
  PASS=$((PASS+1))
  echo "  OK   branded 404 page renders"
else
  FAIL=$((FAIL+1))
  echo "  FAIL branded 404 page"
fi

echo "[ui-smoke] ${PASS} passed, ${FAIL} failed"
if [ "$FAIL" -gt 0 ]; then
  echo "[ui-smoke] server log tail:"
  tail -20 "${TMP}/server.log"
  exit 1
fi
echo "[ui-smoke] all good."
