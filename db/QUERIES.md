# EzMonitor read path: aggregation queries and optimisation

Owner: Diveshraju (data aggregation and query optimisation).
Code: `db/migrations/000004_query_indexes.*`, `backend/internal/query/` (SQL in `sql/`),
benchmark: `db/bench/run.sh`. API reference for the frontend: `backend/internal/query/README.md`.

## 1. Queries

Every query is tenant-scoped by joining `user_group_roles` on the caller's `u_id` (taken from
the session, never from the request). A user who is not in the group gets zero rows, and the API
answers 404, so other tenants' IDs cannot be probed.

| File | Serves | DBMS techniques |
|---|---|---|
| `series.sql` | instance and group charts | CTEs; `date_bin` time buckets; rollup + raw `UNION ALL` split at a watermark; relational division for label filters (`NOT EXISTS ... NOT EXISTS`); gap-filling with `generate_series` + `LEFT JOIN`; window functions: 3-bucket moving average (`ROWS BETWEEN 2 PRECEDING`), fleet average (`PARTITION BY bucket`), per-bucket `rank()` |
| `fleet_status.sql` | instance table | `LEFT JOIN LATERAL ... ORDER BY recorded_at DESC LIMIT 1` (top-1 per group, served by a backward PK scan); `jsonb_object_agg` for labels |
| `hotspots.sql` | "busiest instances" | `percentile_cont(0.95) WITHIN GROUP`; `dense_rank()`, `cume_dist()`, `avg() OVER ()` computed before `LIMIT`; metric picked by casting a parameter to the `metric_kind` enum |
| `instance_events.sql` | gaps and reboots, availability | `lag()` over consecutive samples: silence longer than `max_gap` = gap, `uptime` going backwards = reboot |
| `alert_summary.sql` | per-rule alert stats | two-level aggregation (rule, instance), then rule; `FILTER (WHERE ...)`; MTTR |
| `alert_history.sql` | alert log | keyset pagination on `(triggered_at, a_id)` row comparison |

`DATE_TRUNC` (named in the proposal) only truncates to whole units (minute, hour). `date_bin`
(PostgreSQL 14+) bins to any width, such as 5 minutes, so it is used instead. All bins use the Unix
epoch as their origin, in SQL and in Go (`window.go`), so both sides agree on bucket boundaries.

## 2. Why a rollup table

The agent scrapes every **1 s**, so one instance writes 86,400 rows a day. Samples from all instances
arrive interleaved, so one instance's rows are spread over **every** heap page of the day's
partition. Reading one instance costs about as many pages as reading all of them (7,917 vs 7,633
buffers below). Indexes cannot fix that, because the limit is heap pages, not finding rows:

- **Covering index** `(i_id, recorded_at) INCLUDE (all metrics)` (measured, rejected): +136 MB
  (82 % of the heap) plus a second write per sample, and 7 days for 1 instance only went from 82 to 61 ms.
- **Rollup** `metrics_rollup_5m` (adopted): one row per (instance, 5 min) storing `n`, `SUM`, `MIN`,
  `MAX`. 6.05 M raw rows (598 MB) become 20,160 rollup rows (4 MB). It stores `SUM` and `n` rather
  than `AVG`, so 5-minute buckets re-aggregate *exactly* into 15 min, 1 h, and so on:
  `avg = Σsum / Σn`. Percentiles cannot be re-aggregated that way, so `hotspots.sql` reads raw rows.

### Correctness: watermark + invalidation

`metrics_rollup_state` holds `watermark` and `dirty_from`. Buckets before
`split = least(watermark, dirty_from)` are exact in the rollup. `series.sql` reads the rollup below
the split and raw rows above it. Late writes cannot make a chart wrong:

- Statement-level `AFTER INSERT / UPDATE / DELETE` triggers on `metrics_data` (transition tables,
  same pattern as T1) take `min(recorded_at)` of the batch. If it is below the watermark, they lower
  `dirty_from` (aligned to a bucket boundary) in the same transaction. This covers BR4
  re-deliveries, delayed agent batches, dev backfills and manual deletes.
- `refresh_metrics_rollup()` rebuilds `[least(watermark, dirty_from), now - 2 min)` and clears
  `dirty_from`. It is called hourly by `run_maintenance()` (the db-maint container; its signature is
  unchanged) and is safe to call more often.
- Race: an ingest transaction could compare against the old watermark while a refresh runs and
  then commit rows the refresh never saw. The trigger holds a **shared** advisory lock and the refresh
  takes it **exclusively**, so the refresh waits for in-flight ingest transactions to commit.
  Ingest transactions that start during a refresh then see the new watermark (READ COMMITTED).
- The rollup is controlled redundancy outside the 3NF design. The application never writes it; it is
  only recomputed from `metrics_data`. `ON DELETE CASCADE` from `instances` keeps BR8.
- Rollups are kept 400 days (`prune_metrics_rollup`), longer than the 30-day raw retention. The
  refresh runs before old partitions are dropped, and `DROP TABLE` fires no triggers.

Evidence:

- `TestSeriesMatchesRawAggregate` compares every bucket of `series.sql` with a direct aggregate of
  `metrics_data`, for steps of 5 m and 1 h (rollup) and 10 s and 7 m (raw).
- `TestRollupInvalidation` repeats that comparison after a BR4 overwrite, a late insert and a delete,
  both before and after the refresh. Writing this test is what caught a double-counting bug
  (`dirty_from` must sit on a bucket boundary; now enforced by a CHECK).
- Stress run: 8 pgbench clients wrote 5-row batches with random timestamps up to 10 min in the past,
  straddling the watermark, for 20 s while another session ran `refresh_metrics_rollup('0 s')`
  in a loop (563 refreshes). Rollup-served results then matched raw in all 69 buckets
  (716,690 rows): **0 mismatches**.

## 3. Measurements

PostgreSQL 16.15, `shared_buffers = 256MB`. Dev seed: 10 enabled instances, **7 days at 1 s =
6,048,000 rows**, plus 98k synthetic alerts. Median of 5 warm runs of `EXPLAIN (ANALYZE, BUFFERS)`.
"raw" forces `series.sql` off the rollup (watermark hidden in a rolled-back transaction).
Reproduce with `db/bench/run.sh <url>`.

| case | median | buffers | temp |
|---|---|---|---|
| series, 1 instance, 1 day @ 5 min, raw | 89.3 ms | 7,917 | - |
| series, 1 instance, 1 day @ 5 min, **rollup** | **2.3 ms** | 228 | - |
| series, group (10), 1 day @ 5 min, raw | 780.0 ms | 7,633 | - |
| series, group (10), 1 day @ 5 min, **rollup** | **28.9 ms** | 1,849 | - |
| series, 1 instance, 7 days @ 1 h, raw | 834.9 ms | 55,401 | - |
| series, 1 instance, 7 days @ 1 h, **rollup** | **4.1 ms** | 396 | - |
| series, group (10), 7 days @ 1 h, raw | 6,960.2 ms | 498,563 | - |
| series, group (10), 7 days @ 1 h, **rollup** | **25.0 ms** | 3,554 | - |
| series, group, label env=prod, 1 day @ 5 min | 9.0 ms | 1,046 | - |
| series, 1 instance, 1 h @ 15 s (raw by design) | 4.0 ms | 323 | - |
| fleet_status, all groups | 0.3 ms | 72 | - |
| hotspots p95 cpu, 1 h | 25.7 ms | 415 | - |
| hotspots p95 cpu, 24 h | 609.5 ms | 74,051 | - |
| instance_events, 24 h, work_mem 4 MB | 158.2 ms | 7,903 | spill 719 blk |
| instance_events, 24 h, work_mem 32 MB | 151.2 ms | 7,903 | - |
| instance_events, 7 days, work_mem 32 MB | 1,160.3 ms | 55,381 | spill 5,025 blk |
| alert_summary, 7 days | 17.7 ms | 1,134 | - |
| alert_history, first page | 0.3 ms | 78 | - |

Headline: the group dashboard over 7 days goes from **7.0 s to 25 ms (about 280×)**.

Other measured changes (seed at 5 s / 2.4 M rows, 98k alerts):

| change | before | after |
|---|---|---|
| alert history, page at row 50,000: `OFFSET` vs keyset | 108.8 ms, 150,517 buffers | 0.5 ms, 160 buffers |
| `alert_summary`: `count(DISTINCT i_id)` vs two-level aggregation | 115 ms, 8.6 MB disk sort | 25 ms, no spill |
| first rollup build (6.05 M rows) / hourly incremental refresh | n/a | 1.8 s / ~10 ms |
| ingest batch (5 rows, pgbench, 4 clients) without / with invalidation trigger | 1.33 ms | 1.50 ms |

### Partition pruning

Both bounds of the time range are query parameters, so the planner prunes daily partitions when
the query starts. The raw half of `series.sql` also filters on `recorded_at >= (SELECT ts FROM split)`,
an InitPlan value. Partitions below the rollup split are therefore skipped at run time and appear as
`never executed` in the plan:

```
->  Append (actual rows=0 loops=10)
      ->  Bitmap Heap Scan on metrics_data_20261104 m_1 (never executed)
      ...
      ->  Bitmap Heap Scan on metrics_data_20261111 m_8 (actual rows=0 loops=10)
```

## 4. Index decisions

| index | decision | evidence |
|---|---|---|
| `metrics_data` PK `(i_id, recorded_at)` (000001) | kept, primary access path | per-instance range scans, backward top-1 scan in `fleet_status` |
| `metrics_data` BRIN `(recorded_at)` (000001) | kept, not used by these queries | every query here is scoped to known instances, so it goes through the PK |
| `metrics_rollup_5m` PK `(i_id, bucket)` | **added** | rollup range scans in `series.sql` |
| `metrics_rollup_5m (bucket)` | **added** | refresh deletes a time slice across all instances |
| covering `metrics_data (i_id, recorded_at) INCLUDE (...)` | rejected | +136 MB; 82 → 61 ms vs 4 ms for the rollup |
| partial `alert_logs (i_id) WHERE status = 'active'` | rejected | planner already does an index-only scan of `alert_logs_one_active`; 0.52 ms either way |
| `alert_logs (g_id, triggered_at DESC, a_id DESC)` | rejected | existing `(g_id, triggered_at DESC)` + incremental sort: 0.48 ms either way |

## 5. Known costs and limits

- Percentiles and gap detection read raw rows. The API caps `hotspots` at 24 h (~0.6 s) and
  `events` at 7 days (~1.2 s, still spills at 32 MB). Both run in a read-only transaction with
  `SET LOCAL work_mem = '32MB'`, never a role-wide setting. If `events` needs to be faster: detect
  reboots at bucket resolution from `uptime_max` in the rollup, and gaps from `n` per bucket.
- Series steps that are not a multiple of 5 minutes read raw rows, so they are limited to 24 h windows.
- A refresh blocks new ingest statements for its duration (shared vs exclusive advisory lock):
  ~10 ms hourly, 1–2 s only for a rebuild after a large backfill.
- The ingest transaction must run in READ COMMITTED (the default).
- `000004` redefines `run_maintenance()` (same signature). A later migration that redefines it
  again must keep the `refresh_metrics_rollup()` / `prune_metrics_rollup()` calls.
