BEGIN;

-- Restore the 000001 body of run_maintenance.
CREATE OR REPLACE FUNCTION run_maintenance(keep_days int DEFAULT 30)
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
    today   date := (now() AT TIME ZONE 'UTC')::date;
    dropped int;
BEGIN
    PERFORM create_metrics_partition(today + n) FROM generate_series(-1, 7) AS n;
    dropped := drop_old_metrics_partitions(keep_days);
    RETURN format('partitions ensured %s..%s, dropped %s', today - 1, today + 7, dropped);
END $$;

DROP TRIGGER IF EXISTS rollup_invalidate_on_insert ON metrics_data;
DROP TRIGGER IF EXISTS rollup_invalidate_on_update ON metrics_data;
DROP TRIGGER IF EXISTS rollup_invalidate_on_delete ON metrics_data;
DROP FUNCTION IF EXISTS rollup_invalidate();
DROP FUNCTION IF EXISTS prune_metrics_rollup(int);
DROP FUNCTION IF EXISTS refresh_metrics_rollup(interval);
DROP FUNCTION IF EXISTS metrics_rollup_lock_key();
DROP TABLE IF EXISTS metrics_rollup_state;
DROP TABLE IF EXISTS metrics_rollup_5m;

COMMIT;
