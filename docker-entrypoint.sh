#!/bin/sh
set -e

# Detect the owner of the /data mount and re-exec as that user.
# This means the container always writes files as whoever owns the
# bind-mounted directory on the host — no UID env vars needed.
uid=$(stat -c '%u' /data)
gid=$(stat -c '%g' /data)

if [ "$uid" = "0" ]; then
    exec "$@"
fi

exec su-exec "$uid:$gid" "$@"
