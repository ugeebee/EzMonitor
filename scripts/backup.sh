#!/usr/bin/env bash
# Custom-format dump of the whole DB into ./backups/. Restore with:
#   docker compose exec -T db pg_restore -U ezm_owner -d ezmonitor --clean < backups/<file>.dump
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p backups
out="backups/ezmonitor_$(date -u +%Y%m%dT%H%M%SZ).dump"
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$out"
echo "wrote $out ($(du -h "$out" | cut -f1))"
