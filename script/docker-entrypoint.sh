#!/bin/sh
# Drop privileges after ensuring the data volume is writable by gsbs (UID 1000).
set -e

mkdir -p /app/data
chown -R gsbs:gsbs /app/data

exec su-exec gsbs:gsbs "$@"
