-- name: series
-- Time-bucketed metrics for one instance or a whole group (dashboard charts).
--
--   $1 u_id    bigint       caller (RBAC: only groups in user_group_roles)
--   $2 g_id    bigint|NULL  group filter
--   $3 i_id    bigint|NULL  instance filter (at least one of $2/$3 is set by the API)
--   $4 keys    text[]       label filter keys   (instance must match ALL pairs)
--   $5 values  text[]       label filter values (same length as $4)
--   $6 from    timestamptz  inclusive, aligned to $8 from the Unix epoch
--   $7 to      timestamptz  exclusive, aligned to $8 from the Unix epoch
--   $8 step    interval     bucket width; a multiple of 5 min uses the rollup
--
-- One row per (instance, bucket), gap-filled: a bucket without samples has
-- samples = 0 and NULL metrics, so charts show a gap instead of interpolating.
-- Zero rows means "no visible instance": the API answers 404 (no existence leak).
WITH scope AS (
    SELECT i.i_id, i.label
    FROM instances i
    JOIN user_group_roles ugr ON ugr.g_id = i.g_id AND ugr.u_id = $1
    WHERE ($2::bigint IS NULL OR i.g_id = $2)
      AND ($3::bigint IS NULL OR i.i_id = $3)
      -- relational division: no requested (key, value) pair is missing on i
      AND NOT EXISTS (
            SELECT 1
            FROM unnest($4::text[], $5::text[]) AS f(k, v)
            WHERE NOT EXISTS (SELECT 1 FROM instance_labels l
                              WHERE l.i_id = i.i_id AND l.label_key = f.k AND l.label_value = f.v))
),
-- Rollups cover [from, split), raw rows cover [split, to). The rollup is
-- exact below least(watermark, dirty_from); least() ignores a NULL dirty_from.
split AS (
    SELECT CASE WHEN extract(epoch FROM $8::interval)::bigint % 300 = 0
                THEN greatest($6::timestamptz, least($7::timestamptz, s.watermark, s.dirty_from))
                ELSE $6::timestamptz
           END AS ts
    FROM metrics_rollup_state s
),
src AS (
    SELECT r.i_id,
           date_bin($8::interval, r.bucket, TIMESTAMPTZ 'epoch') AS bucket,
           r.n, r.cpu_sum, r.cpu_max, r.ram_sum, r.ram_max,
           r.gpu_n, r.gpu_sum, r.gpu_max,
           r.net_up_sum, r.net_up_max, r.net_down_sum, r.net_down_max, r.uptime_max
    FROM metrics_rollup_5m r
    WHERE r.i_id IN (SELECT i_id FROM scope)
      AND r.bucket >= $6::timestamptz
      AND r.bucket <  (SELECT ts FROM split)
    UNION ALL
    SELECT m.i_id,
           date_bin($8::interval, m.recorded_at, TIMESTAMPTZ 'epoch'),
           1, m.cpu_util, m.cpu_util, m.ram_util, m.ram_util,
           (m.gpu_util IS NOT NULL)::int, m.gpu_util, m.gpu_util,
           m.net_up_kbps, m.net_up_kbps, m.net_down_kbps, m.net_down_kbps, m.uptime
    FROM metrics_data m
    WHERE m.i_id IN (SELECT i_id FROM scope)
      AND m.recorded_at >= $6::timestamptz          -- static bounds: plan-time partition pruning
      AND m.recorded_at <  $7::timestamptz
      AND m.recorded_at >= (SELECT ts FROM split)   -- skip what the rollup already covers
),
agg AS (
    SELECT i_id, bucket,
           sum(n)                              AS samples,
           sum(cpu_sum) / sum(n)               AS cpu_avg,
           max(cpu_max)                        AS cpu_max,
           sum(ram_sum) / sum(n)               AS ram_avg,
           max(ram_max)                        AS ram_max,
           sum(gpu_sum) / nullif(sum(gpu_n), 0) AS gpu_avg,
           max(gpu_max)                        AS gpu_max,
           sum(net_up_sum) / sum(n)            AS net_up_avg,
           max(net_up_max)                     AS net_up_max,
           sum(net_down_sum) / sum(n)          AS net_down_avg,
           max(net_down_max)                   AS net_down_max,
           max(uptime_max)                     AS uptime
    FROM src
    GROUP BY i_id, bucket
),
grid AS (   -- every (instance, bucket) pair, so empty buckets survive the join
    SELECT s.i_id, s.label, b.bucket
    FROM scope s
    CROSS JOIN generate_series($6::timestamptz, $7::timestamptz - $8::interval, $8::interval) AS b(bucket)
)
SELECT g.i_id, g.label, g.bucket,
       coalesce(a.samples, 0) AS samples,
       a.cpu_avg, a.cpu_max, a.ram_avg, a.ram_max, a.gpu_avg, a.gpu_max,
       a.net_up_avg, a.net_up_max, a.net_down_avg, a.net_down_max, a.uptime,
       -- 3-bucket moving average per instance (smooths single-scrape spikes)
       avg(a.cpu_avg) OVER (PARTITION BY g.i_id ORDER BY g.bucket
                            ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS cpu_avg_smooth,
       -- fleet context for the same bucket
       avg(a.cpu_avg) OVER (PARTITION BY g.bucket)                    AS fleet_cpu_avg,
       CASE WHEN a.cpu_avg IS NOT NULL
            THEN rank() OVER (PARTITION BY g.bucket ORDER BY a.cpu_avg DESC NULLS LAST)
       END                                                            AS cpu_rank
FROM grid g
LEFT JOIN agg a USING (i_id, bucket)
ORDER BY g.i_id, g.bucket;
