-- name: instance_events
-- Data gaps (agent silent longer than a threshold) and reboots (uptime went
-- backwards) for one instance, found with LAG() over consecutive samples.
-- The API caps the window at 7 days.
--
--   $1 u_id     bigint
--   $2 i_id     bigint
--   $3 from     timestamptz
--   $4 to       timestamptz   (clamped to now())
--   $5 max_gap  interval      e.g. 30 seconds for a 1 s scrape interval
WITH inst AS (
    SELECT i.i_id
    FROM instances i
    JOIN user_group_roles ugr ON ugr.g_id = i.g_id AND ugr.u_id = $1
    WHERE i.i_id = $2
),
s AS (
    SELECT m.recorded_at,
           m.uptime,
           lag(m.recorded_at, 1, $3::timestamptz) OVER w AS prev_at,   -- window start counts as "seen"
           lag(m.uptime) OVER w                          AS prev_uptime
    FROM metrics_data m
    WHERE m.i_id = $2
      AND m.recorded_at >= $3::timestamptz
      AND m.recorded_at <  $4::timestamptz
      AND EXISTS (SELECT 1 FROM inst)
    WINDOW w AS (ORDER BY m.recorded_at)
),
events AS (
    SELECT 'gap'::text AS kind, prev_at AS started_at, recorded_at AS ended_at
    FROM s
    WHERE recorded_at - prev_at > $5::interval
    UNION ALL
    -- last sample before the reboot .. boot time derived from the new uptime
    SELECT 'reboot', prev_at, recorded_at - make_interval(secs => uptime)
    FROM s
    WHERE uptime < prev_uptime
    UNION ALL
    -- trailing silence: last sample (or window start) .. min(to, now)
    -- An aggregate without GROUP BY yields a row even from zero input rows,
    -- so the RBAC check must sit in HAVING (a WHERE here would not stop it).
    SELECT 'gap', coalesce(max(s.recorded_at), $3::timestamptz), least($4::timestamptz, now())
    FROM s
    HAVING EXISTS (SELECT 1 FROM inst)
       AND least($4::timestamptz, now()) - coalesce(max(s.recorded_at), $3::timestamptz) > $5::interval
)
SELECT kind, started_at, ended_at,
       extract(epoch FROM ended_at - started_at)::float8 AS duration_s
FROM events
ORDER BY started_at, kind;
