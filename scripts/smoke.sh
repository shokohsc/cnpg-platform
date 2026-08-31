#!/usr/bin/env bash
set -euo pipefail

# Boots a throwaway local Postgres and runs the pg integration tests against it.
PGVER=$(ls /usr/lib/postgresql 2>/dev/null | sort -V | tail -1 || true)
if [ -z "$PGVER" ]; then
  if command -v apt-get >/dev/null && [ "$(id -u)" = 0 ]; then
    apt-get update -qq
    apt-get install -y -qq postgresql >/dev/null 2>&1
    PGVER=$(ls /usr/lib/postgresql | sort -V | tail -1)
    # ensure a non-root path allows initdb
    export PATH="/usr/lib/postgresql/$PGVER/bin:$PATH"
  else
    echo "PostgreSQL not installed and no root to install it — skipping smoke test"
    exit 0
  fi
fi

TMP=$(mktemp -d)
trap 'pg_ctl -D "$TMP/data" stop -m fast >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT

PORT=55432
initdb -D "$TMP/data" -U postgres --auth=trust >/dev/null
pg_ctl -D "$TMP/data" -o "-p $PORT -k $TMP" -l "$TMP/log" start >/dev/null

export CNPG_TEST_DSN="postgres://postgres@localhost:$PORT/postgres?sslmode=disable&application_name=cnpg-manager"
go test ./internal/pg/ -run 'TestIntegration' -v
