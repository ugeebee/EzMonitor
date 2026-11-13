-- name: alert_history
-- Alert log for one group, newest first, keyset-paginated.
-- Pass the (triggered_at, a_id) of the last row of the previous page as the
-- cursor; NULLs fetch the first page. Each page is a range scan on
-- alert_logs_group_time_idx (g_id, triggered_at DESC) plus an incremental
-- sort on a_id for equal timestamps, so page 1,000 costs the same as page 1
-- (OFFSET would read and discard every earlier row).
--
--   $1 u_id         bigint
--   $2 g_id         bigint
--   $3 cursor_at    timestamptz|NULL
--   $4 cursor_id    bigint|NULL
--   $5 status       alert_status|NULL  ('active' | 'resolved')
--   $6 limit        int
SELECT a.a_id, a.rule_id, r.metric_name, r.operator, r.threshold_value,
       a.i_id, i.label AS instance_label,
       a.alert_value, a.triggered_at, a.resolved_at, a.status
FROM alert_logs a
JOIN alert_rules r ON r.rule_id = a.rule_id
JOIN instances   i ON i.i_id    = a.i_id
WHERE a.g_id = $2
  AND EXISTS (SELECT 1 FROM user_group_roles ugr WHERE ugr.u_id = $1 AND ugr.g_id = $2)
  AND (a.triggered_at, a.a_id) < (coalesce($3::timestamptz, 'infinity'),
                                  coalesce($4::bigint, 9223372036854775807))
  AND ($5::alert_status IS NULL OR a.status = $5)
ORDER BY a.triggered_at DESC, a.a_id DESC
LIMIT $6;
