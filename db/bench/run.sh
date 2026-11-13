#!/usr/bin/env bash
# Reproduces the measurements in db/QUERIES.md.
#
#   docker compose --profile seed run --rm seed          # or: psql -v days=7 -v step=1 -f db/seed/backfill_metrics.sql
#   psql "$URL" -c "SELECT refresh_metrics_rollup()"
#   db/bench/run.sh "postgres://ezm_owner:<pw>@localhost:5432/ezmonitor"
#
# Each case runs EXPLAIN (ANALYZE, BUFFERS) 5 times on a warm cache and
# prints the median execution time and the buffers touched. "raw" cases
# force series.sql onto metrics_data by hiding the rollup watermark inside
# a rolled-back transaction, so nothing is changed permanently. "analytic"
# cases run like Store.analytic(): read-only transaction, work_mem = 32MB.
set -euo pipefail
URL=${1:?usage: run.sh <postgres-url>}
SQL_DIR="$(cd "$(dirname "$0")/../../backend/internal/query/sql" && pwd)"

body() { sed -e '/^[[:space:]]*--/d' "$SQL_DIR/$1" | sed -e 's/;[[:space:]]*$//'; }

# bench <label> <file> <param types> <args> [raw]
bench() {
  local label=$1 file=$2 types=$3 args=$4 mode=${5:-}
  local pre="" post=""
  if [[ $mode == raw ]]; then
    pre="BEGIN; UPDATE metrics_rollup_state SET watermark = '-infinity';"
    post="ROLLBACK;"
  elif [[ $mode == analytic ]]; then   # what Store.analytic() does
    pre="BEGIN READ ONLY; SET LOCAL work_mem = '32MB';"
    post="COMMIT;"
  fi
  local out
  out=$( {
    echo "SET client_min_messages = warning; $pre"
    echo "PREPARE q($types) AS $(body "$file");"
    for _ in 1 2 3 4 5; do
      echo "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, FORMAT JSON) EXECUTE q($args);"
    done
    echo "$post"
  } | psql "$URL" -X -q -A -t -v ON_ERROR_STOP=1 )
  python3 - "$label" <<PY
import json, sys
docs, buf, dec = [], """$out""", json.JSONDecoder()
i = 0
while i < len(buf):
    j = buf.find('[', i)
    if j < 0: break
    obj, i = dec.raw_decode(buf, j)
    docs.append(obj[0])
times = sorted(d["Execution Time"] for d in docs)
plan = docs[-1]["Plan"]
blocks = plan.get("Shared Hit Blocks", 0) + plan.get("Shared Read Blocks", 0)
temp = plan.get("Temp Written Blocks", 0)
print(f"| {sys.argv[1]} | {times[len(times)//2]:.1f} ms | {blocks:,} | {'spill ' + str(temp) + ' blk' if temp else '-'} |")
PY
}

S="bigint,bigint,bigint,text[],text[],timestamptz,timestamptz,interval"
win() { echo "date_bin('$2', now() - interval '$1', 'epoch'), date_bin('$2', now(), 'epoch') + interval '$2', '$2'"; }

echo "| case | median | buffers | temp |"
echo "|---|---|---|---|"
for r in "1 day|5 minutes" "7 days|1 hour"; do
  IFS='|' read -r range step <<<"$r"
  bench "series, 1 instance, $range @ $step, raw"    series.sql "$S" "1, NULL, 3, '{}', '{}', $(win "$range" "$step")" raw
  bench "series, 1 instance, $range @ $step, rollup" series.sql "$S" "1, NULL, 3, '{}', '{}', $(win "$range" "$step")"
  bench "series, group (10), $range @ $step, raw"    series.sql "$S" "1, 1, NULL, '{}', '{}', $(win "$range" "$step")" raw
  bench "series, group (10), $range @ $step, rollup" series.sql "$S" "1, 1, NULL, '{}', '{}', $(win "$range" "$step")"
done
bench "series, group, env=prod, 1 day @ 5 minutes" series.sql "$S" "1, 1, NULL, '{env}', '{prod}', $(win '1 day' '5 minutes')"
bench "series, 1 instance, 1 hour @ 15 s (raw by design)" series.sql "$S" "1, NULL, 3, '{}', '{}', $(win '1 hour' '15 seconds')"
bench "fleet_status, all groups" fleet_status.sql "bigint,bigint,interval" "1, NULL, '5 minutes'"
H="bigint,bigint,timestamptz,timestamptz,metric_kind,int"
E="bigint,bigint,timestamptz,timestamptz,interval"
bench "hotspots p95 cpu, 1 hour"                 hotspots.sql "$H" "1, 1, now() - interval '1 hour', now(), 'cpu_util', 5" analytic
bench "hotspots p95 cpu, 24 hours, work_mem 4MB" hotspots.sql "$H" "1, 1, now() - interval '24 hours', now(), 'cpu_util', 5"
bench "hotspots p95 cpu, 24 hours"               hotspots.sql "$H" "1, 1, now() - interval '24 hours', now(), 'cpu_util', 5" analytic
bench "instance_events, 24 hours, work_mem 4MB"  instance_events.sql "$E" "1, 3, now() - interval '24 hours', now(), '30 seconds'"
bench "instance_events, 24 hours"                instance_events.sql "$E" "1, 3, now() - interval '24 hours', now(), '30 seconds'" analytic
bench "instance_events, 7 days"                  instance_events.sql "$E" "1, 3, now() - interval '7 days', now(), '30 seconds'" analytic
bench "alert_summary, 7 days"     alert_summary.sql "bigint,bigint,timestamptz,timestamptz" "1, 1, now() - interval '7 days', now()"
bench "alert_history, first page" alert_history.sql "bigint,bigint,timestamptz,bigint,alert_status,int" "1, 1, NULL, NULL, NULL, 50"
