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

exec su-exec gsbs:gsbs "$@"
