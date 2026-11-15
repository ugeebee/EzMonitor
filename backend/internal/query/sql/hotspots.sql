-- name: hotspots
-- Instances ranked by the 95th percentile of one metric over a window.
-- Percentiles cannot be rebuilt from SUM/COUNT rollups, so this reads raw
-- rows; the API caps the window at 24 h.
--
--   $1 u_id    bigint
--   $2 g_id    bigint|NULL
--   $3 from    timestamptz
--   $4 to      timestamptz
--   $5 metric  metric_kind   ('cpu_util', 'gpu_util', ... ; the enum validates it)
--   $6 limit   int
WITH scope AS (
    SELECT i.i_id, i.label
    FROM instances i
    JOIN user_group_roles ugr ON ugr.g_id = i.g_id AND ugr.u_id = $1
    WHERE $2::bigint IS NULL OR i.g_id = $2
),
stats AS (
    SELECT m.i_id,
           count(x.v)                                         AS samples,
           avg(x.v)                                           AS avg,
           max(x.v)                                           AS max,
           percentile_cont(0.95) WITHIN GROUP (ORDER BY x.v)  AS p95
    FROM metrics_data m
    CROSS JOIN LATERAL (
        SELECT CASE $5::metric_kind
                   WHEN 'cpu_util'      THEN m.cpu_util::float8
                   WHEN 'gpu_util'      THEN m.gpu_util::float8
                   WHEN 'ram_util'      THEN m.ram_util::float8
                   WHEN 'uptime'        THEN m.uptime::float8
                   WHEN 'net_up_kbps'   THEN m.net_up_kbps::float8
                   WHEN 'net_down_kbps' THEN m.net_down_kbps::float8
               END AS v
    ) x
    WHERE m.i_id IN (SELECT i_id FROM scope)
      AND m.recorded_at >= $3::timestamptz
      AND m.recorded_at <  $4::timestamptz
      AND x.v IS NOT NULL
    GROUP BY m.i_id
)
SELECT s.i_id, sc.label, s.samples, s.avg, s.max, s.p95,
       dense_rank() OVER (ORDER BY s.p95 DESC)  AS rank,
       s.p95 - avg(s.p95) OVER ()               AS p95_vs_fleet_avg,
       cume_dist() OVER (ORDER BY s.p95)        AS fleet_percentile
FROM stats s
JOIN scope sc USING (i_id)
ORDER BY rank, s.i_id
LIMIT $6;
