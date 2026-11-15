-- name: fleet_status
-- One row per visible instance: latest sample, labels, open alerts, caller's role.
--
--   $1 u_id      bigint
--   $2 g_id      bigint|NULL   restrict to one group
--   $3 lookback  interval      how far back to look for the latest sample
--
-- The LATERAL subquery is a top-1 per instance: a backward scan of the
-- (i_id, recorded_at) primary key in each partition that survives pruning,
-- so cost is O(instances), not O(samples).
SELECT i.i_id, i.g_id, g.label AS group_label, i.label, ugr.role_id,
       i.enabled, i.status, i.last_seen_at,
       coalesce(lab.labels, '{}'::jsonb) AS labels,
       last.recorded_at, last.cpu_util, last.gpu_util, last.ram_util,
       last.uptime, last.net_up_kbps, last.net_down_kbps,
       act.active_alerts
FROM user_group_roles ugr
JOIN groups    g ON g.g_id = ugr.g_id
JOIN instances i ON i.g_id = ugr.g_id
LEFT JOIN LATERAL (
    SELECT m.recorded_at, m.cpu_util, m.gpu_util, m.ram_util,
           m.uptime, m.net_up_kbps, m.net_down_kbps
    FROM metrics_data m
    WHERE m.i_id = i.i_id
      AND m.recorded_at >= now() - $3::interval
      AND m.recorded_at <  now() + interval '5 minutes'   -- bound both sides: prune future partitions
    ORDER BY m.recorded_at DESC
    LIMIT 1
) last ON true
LEFT JOIN LATERAL (
    SELECT jsonb_object_agg(l.label_key, l.label_value) AS labels
    FROM instance_labels l
    WHERE l.i_id = i.i_id
) lab ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS active_alerts
    FROM alert_logs a
    WHERE a.i_id = i.i_id AND a.status = 'active'        -- index-only scan on alert_logs_one_active
) act ON true
WHERE ugr.u_id = $1
  AND ($2::bigint IS NULL OR ugr.g_id = $2)
ORDER BY g.label, i.label;
