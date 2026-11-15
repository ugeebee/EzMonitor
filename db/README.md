# EzMonitor database

Owner: Harsha (data infrastructure and schema). PostgreSQL 16, migrations applied by golang-migrate.

## Quick start

```bash
cp .env.example .env                              # set passwords (letters/digits only)
docker compose up -d                              # db + migrate + db-maint
docker compose --profile seed run --rm seed       # dev users, 11 instances, ~403k samples
psql postgres://ezm_owner:<pw>@localhost:5432/ezmonitor
```

`db/init/` runs only when the `pgdata` volume is empty. Migrations run on every `up`.
To start from scratch: `docker compose down -v`.

## Layout

| Path | Purpose |
|---|---|
| `migrations/000001_schema` | 11 tables, enums, partitioning, T1 and BR7 triggers |
| `migrations/000002_privileges` | grants for `ezm_ingest`, `ezm_api`, `ezm_readonly` |
| `migrations/000004_query_indexes` | 5-minute rollup (`metrics_rollup_5m`), invalidation triggers, refresh in `run_maintenance()`; see `QUERIES.md` |
| `QUERIES.md` | dashboard queries, rollup design, `EXPLAIN ANALYZE` measurements, index decisions |
| `bench/run.sh` | reproduces the measurements in `QUERIES.md` |
| `init/01-login-roles.sh` | creates the three login roles with passwords from `.env` |
| `seed/dev_seed.sql` | dev users, groups, instances, rules, webhook |
| `seed/backfill_metrics.sql` | synthetic history (`-v days=7 -v step=15` by default) |

**Migration numbering:** `000003_alerting` belongs to Aayush (alert evaluation and notify triggers),
`000004_query_indexes` to Diveshraju. Never edit a migration that has been merged; add a new one.

## Tables

| Table | Key | Notes |
|---|---|---|
| `roles` | `role_id` | fixed: 1 admin, 2 editor, 3 viewer |
| `users` | `u_id` | `email` is citext, unique, format-checked |
| `groups` | `g_id` | tenant |
| `user_group_roles` | `(u_id, g_id)` | one role per user per group |
| `instances` | `i_id` | `fingerprint` (16 hex), `token_hash` (sha256 hex), `enabled`, `status`, `last_seen_at`; label unique per group |
| `instance_labels` | `(i_id, label_key)` | key/value labels, e.g. `env=prod` |
| `metrics_data` | `(i_id, recorded_at)` | partitioned by UTC day; `cpu_util`, `gpu_util` (NULL = no GPU), `ram_util`, `uptime` (s), `net_up_kbps`, `net_down_kbps` |
| `alert_rules` | `rule_id` | `metric_name` (enum), `operator` (`> >= < <=`), `threshold_value`, `enabled` |
| `alert_logs` | `a_id` | `status` active/resolved; one active per (rule, instance); resolved rows are frozen |
| `webhooks` | `web_id` | https only, `secret` for HMAC signing |
| `alert_rule_webhooks` | `(rule_id, web_id)` | which webhooks a rule notifies; same group enforced by FK |
| `metrics_rollup_5m` | `(i_id, bucket)` | derived: 5-min SUM/MIN/MAX/count of `metrics_data`; never written by the app |
| `metrics_rollup_state` | singleton | rollup `watermark` and `dirty_from` (see `QUERIES.md`) |

## Database roles

| Role | Used by | Can |
|---|---|---|
| `ezm_ingest` | ingest handler | read `instances(i_id, g_id, token_hash, enabled)`, update `last_seen_at/status`, insert/upsert `metrics_data` |
| `ezm_api` | dashboard/config API | CRUD on config tables, read metrics, read/resolve alerts |
| `ezm_readonly` | reports, debugging | read everything except `hash_pass`, `token_hash`, webhook `secret` |

The backend gets `INGEST_DATABASE_URL` and `API_DATABASE_URL` from compose. Two pgx pools.

## For Utkarsh (ingest)

Auth (hash the bearer token in Go with SHA-256, hex-encode):
```sql
SELECT i_id, enabled FROM instances WHERE i_id = $1 AND token_hash = $2;
```
One round trip per batch, BR4 overwrite semantics:
```sql
INSERT INTO metrics_data (i_id, recorded_at, cpu_util, gpu_util, ram_util, uptime, net_up_kbps, net_down_kbps)
SELECT $1::bigint, * FROM unnest($2::timestamptz[], $3::real[], $4::real[], $5::real[],
                                 $6::bigint[], $7::real[], $8::real[])
ON CONFLICT (i_id, recorded_at) DO UPDATE SET
  cpu_util = EXCLUDED.cpu_util, gpu_util = EXCLUDED.gpu_util, ram_util = EXCLUDED.ram_util,
  uptime = EXCLUDED.uptime, net_up_kbps = EXCLUDED.net_up_kbps, net_down_kbps = EXCLUDED.net_down_kbps;
```
Map errors: SQLSTATE `23514` (check_violation: bad value or disabled instance) -> HTTP 422,
`23503` (FK) -> 404, "no partition" (`23514` on a timestamp outside yesterday..+7 days) -> 422.

Agent changes needed: send `gpu_util` as `null` when there is no NVIDIA GPU (use `*float64`),
and map `up_speed`/`down_speed` to `net_up_kbps`/`net_down_kbps`.

Dev credentials: instance `1`, token `dev-token-0001`. Instance 10 is disabled (T1 test).

## For Aayush (alerting, `000003_alerting`)

- Evaluate in an `AFTER ... FOR EACH STATEMENT` trigger with `REFERENCING NEW TABLE`.
  You need two triggers (INSERT and UPDATE), because a transition-table trigger allows one event
  and BR4 re-deliveries arrive as UPDATEs.
- Make the function `SECURITY DEFINER SET search_path = public`: it runs inside the
  `ezm_ingest` session, which has no rights on `alert_rules` / `alert_logs`.
- Insert with `ON CONFLICT (rule_id, i_id) WHERE status = 'active' DO NOTHING`.
- Resolve with `UPDATE ... SET status = 'resolved', resolved_at = ...`. Re-opening is blocked by `br7_freeze_resolved`.
- Notify the Go worker with `pg_notify('alert_fired', ...)` from an AFTER INSERT trigger on `alert_logs`.

## For Diveshraju (queries)

- Always bound time on both sides (`recorded_at >= $from AND recorded_at < $to`) so the planner
  prunes empty future partitions.
- The PK `(i_id, recorded_at)` already covers per-instance range scans; BRIN on `recorded_at`
  covers all-instance scans. Add indexes in `000004`, with `EXPLAIN ANALYZE` evidence.
- Filter tenants through `user_group_roles`; never trust a `g_id` sent by the client.

Done: see [`QUERIES.md`](QUERIES.md) (queries, rollup, measurements) and
[`backend/internal/query/README.md`](../backend/internal/query/README.md) (API for the frontend).
The rollup needs no action from ingest or seeding: writes older than the rollup watermark
(including `backfill_metrics.sql`) mark it dirty, dashboards fall back to raw rows for that range,
and the next hourly `run_maintenance()` rebuilds it. Run `SELECT refresh_metrics_rollup();` to
rebuild immediately after a large backfill.
