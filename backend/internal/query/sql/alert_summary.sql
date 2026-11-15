-- name: alert_summary
-- Per-rule alert statistics for one group over a window.
--
--   $1 u_id  bigint
--   $2 g_id  bigint
--   $3 from  timestamptz
--   $4 to    timestamptz
--
-- Two-level aggregation: (rule, instance) first, then rule. The inner level
-- is a small HashAggregate, and "instances affected" becomes count(*) of
-- inner rows instead of count(DISTINCT i_id), which would sort every alert.
WITH per_instance AS (
    SELECT a.rule_id, a.i_id,
           count(*)                                                     AS fired,
           sum(a.resolved_at - a.triggered_at) FILTER (WHERE a.status = 'resolved') AS resolved_time,
           count(*) FILTER (WHERE a.status = 'resolved')                AS resolved,
           max(a.alert_value)                                           AS worst_value,
           max(a.triggered_at)                                          AS last_fired
    FROM alert_logs a
    WHERE a.g_id = $2
      AND a.triggered_at >= $3::timestamptz
      AND a.triggered_at <  $4::timestamptz
    GROUP BY a.rule_id, a.i_id
)
SELECT r.rule_id, r.metric_name, r.operator, r.threshold_value, r.enabled,
       coalesce(sum(p.fired), 0)::bigint                                 AS fired,
       count(p.i_id)                                                     AS instances_affected,
       extract(epoch FROM sum(p.resolved_time) / nullif(sum(p.resolved), 0))::float8 AS mttr_s,
       max(p.worst_value)                                                AS worst_value,
       max(p.last_fired)                                                 AS last_fired,
       -- open now, regardless of when they fired (index-only scan on alert_logs_one_active)
       (SELECT count(*) FROM alert_logs x
         WHERE x.rule_id = r.rule_id AND x.status = 'active')             AS active_now
FROM user_group_roles ugr
JOIN alert_rules r ON r.g_id = ugr.g_id
LEFT JOIN per_instance p ON p.rule_id = r.rule_id
WHERE ugr.u_id = $1 AND ugr.g_id = $2
GROUP BY r.rule_id
ORDER BY fired DESC, r.rule_id;
