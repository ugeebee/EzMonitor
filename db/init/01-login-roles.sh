#!/bin/bash
# Runs ONCE, only when the pgdata volume is empty (docker-entrypoint-initdb.d).
# Creates the application login roles with passwords from .env.
# Schema and grants are NOT created here: they come from db/migrations.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
     -v ingest_pw="$EZM_INGEST_PASSWORD" \
     -v api_pw="$EZM_API_PASSWORD" \
     -v ro_pw="$EZM_READONLY_PASSWORD" <<'EOSQL'
CREATE ROLE ezm_ingest   LOGIN PASSWORD :'ingest_pw' CONNECTION LIMIT 50;
CREATE ROLE ezm_api      LOGIN PASSWORD :'api_pw'    CONNECTION LIMIT 50;
CREATE ROLE ezm_readonly LOGIN PASSWORD :'ro_pw'     CONNECTION LIMIT 10;
ALTER DATABASE :"DBNAME" SET timezone TO 'UTC';
EOSQL
