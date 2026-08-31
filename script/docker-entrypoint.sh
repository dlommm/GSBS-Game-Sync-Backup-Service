#!/bin/sh
# Drop privileges after ensuring the data volume is writable by gsbs (UID 1000).
set -e

mkdir -p /app/data
# Only chown when the top-level dir is not already owned by gsbs (UID 1000).
# Avoids recursively walking potentially GB of save files on every start.
if [ "$(stat -c '%u' /app/data 2>/dev/null)" != "1000" ]; then
    chown 1000:1000 /app/data
    [ -f /app/data/gsbs.db ] && chown 1000:1000 /app/data/gsbs.db
fi

# The save tree needs its own check. The top-level test above says nothing about
# a root-owned gamesaves/ subtree — left by an older root-running image, or by a
# host-side copy — and the server then fails every save write with EACCES, with
# nothing in the container output to explain it.
#
# The scan is depth-limited so a healthy volume still costs a handful of stats
# instead of a recursive walk of potentially GB of saves; -quit stops at the
# first offender when there is one. Ownership below that depth is not detected —
# that is the deliberate cost of not walking the whole tree on every start.
#
# `! -user` (not `! -uid`, which busybox find does not implement) — stderr is
# deliberately NOT discarded so an unsupported predicate fails loudly instead of
# silently reporting a clean volume.
SAVE_ROOT="${GSBS_SAVE_ROOT:-/app/data/gamesaves}"
if [ -d "$SAVE_ROOT" ] && [ -n "$(find "$SAVE_ROOT" -maxdepth 2 ! -user 1000 -print -quit)" ]; then
    echo "gsbs: repairing ownership under $SAVE_ROOT (one-time; may take a moment on large volumes)" >&2
    chown -R 1000:1000 "$SAVE_ROOT"
fi

exec su-exec gsbs:gsbs "$@"
