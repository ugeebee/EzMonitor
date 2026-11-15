-- =====================================================================
-- Synthetic history for dashboard / query work (Diveshraju).
-- Default: 7 days x 15 s x every enabled instance = 40,320 rows per instance
-- (10 enabled instances in the dev seed -> 403,200 rows, ~45 MB).
-- Override:  psql -v days=14 -v step=5 -f backfill_metrics.sql
-- Pattern: daily sine load + noise, occasional CPU spikes (> 90 %) so the
-- alerting work has breaches to find. GPU is NULL on odd i_id (no GPU).
-- =====================================================================
\set ON_ERROR_STOP on
\if :{?days}
\else
    \set days 7
\endif
\if :{?step}
\else
    \set step 15
\endif

SET TIME ZONE 'UTC';

SET client_min_messages = warning;
SELECT count(*) AS partitions_ensured
FROM (SELECT create_metrics_partition(current_date - d) FROM generate_series(0, :days) AS d) x;

-- Disable alert evaluation trigger during bulk historic backfill (prevents transition table resolution conflicts)
ALTER TABLE metrics_data DISABLE TRIGGER t2_evaluate_alerts_on_insert;

INSERT INTO metrics_data (i_id, recorded_at, cpu_util, gpu_util, ram_util,
                          uptime, net_up_kbps, net_down_kbps)
SELECT i.i_id,
       ts,
       least(100, greatest(0,
           CASE WHEN random() < 0.002 THEN 92 + random() * 8          -- rare spike
                ELSE 30 + 20 * sin(phase + i.i_id) + random() * 10 END)),
       CASE WHEN i.i_id % 2 = 0
            THEN least(100, greatest(0, 40 + 35 * sin(phase * 2 + i.i_id) + random() * 15))
       END,
       least(100, greatest(0, 45 + 2 * i.i_id + 6 * sin(phase) + random() * 3)),
       extract(epoch FROM ts - (now() - make_interval(days => :days + 3)))::bigint,
       abs(200 + 150 * sin(phase * 3 + i.i_id) + random() * 80),
       abs(800 + 600 * sin(phase * 3 + i.i_id) + random() * 300)
FROM instances i
CROSS JOIN generate_series(date_trunc('minute', now()) - make_interval(days => :days),
                           date_trunc('minute', now()) - make_interval(secs => :step),
                           make_interval(secs => :step)) AS ts
CROSS JOIN LATERAL (SELECT 2 * pi() * extract(epoch FROM ts) / 86400 AS phase) p
WHERE i.enabled
ON CONFLICT (i_id, recorded_at) DO NOTHING;

ALTER TABLE metrics_data ENABLE TRIGGER t2_evaluate_alerts_on_insert;

ANALYZE metrics_data;

-- Seed test active and resolved alerts for testing dashboards
INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at, status)
SELECT r.rule_id, i.i_id, i.g_id, 94.5, now() - interval '15 minutes', 'active'
FROM alert_rules r
JOIN instances i ON i.g_id = r.g_id
WHERE r.metric_name = 'cpu_util' AND i.label = 'dev-box-01'
ON CONFLICT (rule_id, i_id) WHERE status = 'active' DO NOTHING;

INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at, resolved_at, status)
SELECT r.rule_id, i.i_id, i.g_id, 92.1, now() - interval '2 days', now() - interval '2 days' + interval '20 minutes', 'resolved'
FROM alert_rules r
JOIN instances i ON i.g_id = r.g_id
WHERE r.metric_name = 'cpu_util' AND i.label = 'dev-box-02'
ON CONFLICT DO NOTHING;

-- Update instances last_seen_at and status so dashboard views show live reporting status
UPDATE instances
SET last_seen_at = now() - interval '15 seconds',
    status = 'up'
WHERE enabled;

UPDATE instances
SET last_seen_at = now() - interval '3 days',
    status = 'down'
WHERE NOT enabled;

-- Precompute 5-minute rollups for instant 24h and 7d charts
SELECT refresh_metrics_rollup('0 seconds');

SELECT (SELECT count(*) FROM metrics_data) AS total_rows,
       pg_size_pretty(sum(pg_total_relation_size(inhrelid))) AS on_disk
FROM pg_inherits
WHERE inhparent = 'metrics_data'::regclass;
