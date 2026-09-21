#!/usr/bin/env bash
# Check release compiler settings without downloading dependencies or compiling.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
mkdir -p "$TEST_DIR/script" "$TEST_DIR/client" "$TEST_DIR/bin"
cp "$ROOT/script/build.sh" "$TEST_DIR/script/build.sh"

cat > "$TEST_DIR/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = env ]; then
  echo linux
elif [ "$1" = build ]; then
  printf '%s %s %s %s\n' "$GOOS" "$GOARCH" "${CGO_ENABLED:-unset}" "${!#}" >> "$BUILD_TEST_LOG"
else
  exit 1
fi
EOF
cat > "$TEST_DIR/bin/goversioninfo" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_DIR/bin/go" "$TEST_DIR/bin/goversioninfo"

export PATH="$TEST_DIR/bin:$PATH" BUILD_WEBUI=0 REGEN_ICONS=0
export COMMIT=test BUILD_DATE=2026-01-01T00:00:00Z
export BUILD_TEST_LOG="$TEST_DIR/build.log"

CGO_ENABLED=0 bash "$TEST_DIR/script/build.sh" v1.2.3 "$TEST_DIR/out" windows-amd64,linux-amd64
diff -u <(printf '%s\n' \
  'windows amd64 1 ./server' \
  'windows amd64 0 ./client' \
  'linux amd64 1 ./server' \
  'linux amd64 1 ./client') "$BUILD_TEST_LOG"

: > "$BUILD_TEST_LOG"
CGO_ENABLED=0 bash "$TEST_DIR/script/build.sh" v1.2.3 "$TEST_DIR/out" linux-amd64
diff -u <(printf '%s\n' \
  'linux amd64 1 ./server' \
  'linux amd64 1 ./client') "$BUILD_TEST_LOG"

echo 'Release build compiler settings passed.'
