-- =====================================================================
-- 000004_query_indexes: read path for dashboards (Diveshraju)
--
-- 1. metrics_rollup_5m: 5-minute pre-aggregates of metrics_data.
--    A dashboard asking for 7 days of one instance reads 2,016 rollup rows
--    instead of 604,800 raw rows (the agent scrapes every 1 s).
--    Stores SUM/COUNT (not AVG) so buckets re-aggregate exactly into any
--    coarser step (15 min, 1 h, ...): avg = sum(sum) / sum(n).
--
-- 2. metrics_rollup_state: buckets in [-inf, least(watermark, dirty_from))
--    are exact. series.sql reads the rollup below that split and raw rows
--    above it, so a dashboard is never wrong, only sometimes slower.
--
-- 3. Invalidation (trigger rollup_invalidate_*): a statement that writes a
--    sample older than the watermark (late agent batch, BR4 re-delivery,
--    dev backfill, manual delete) lowers dirty_from in the same
--    transaction. Readers switch to raw rows from there immediately; the
--    next refresh rebuilds from dirty_from and clears it.
--
-- 4. refresh_metrics_rollup(): rebuilds [least(watermark, dirty_from),
--    now - settle) and advances the watermark. Called from
--    run_maintenance() (db-maint container, hourly); safe to call more often.
--
-- Concurrency: the invalidation trigger holds a shared advisory lock while
-- it compares against the watermark; refresh takes the same lock
-- exclusively. So refresh waits for in-flight ingest transactions to commit
-- (its INSERT ... SELECT then sees their rows), and ingest transactions that
-- start during a refresh compare against the new watermark. Requires READ
-- COMMITTED for the ingest transaction (the default).
--
-- Controlled redundancy: the rollup is derived data outside the 3NF design.
-- The application never writes it; it is only recomputed from metrics_data.
-- =====================================================================
BEGIN;

CREATE TABLE metrics_rollup_5m (
    i_id          BIGINT           NOT NULL REFERENCES instances ON DELETE CASCADE,
    bucket        TIMESTAMPTZ      NOT NULL,      -- start of the 5-minute bucket (epoch aligned)
    n             INTEGER          NOT NULL CHECK (n > 0),
    cpu_sum       DOUBLE PRECISION NOT NULL,
    cpu_min       REAL             NOT NULL,
    cpu_max       REAL             NOT NULL,
    ram_sum       DOUBLE PRECISION NOT NULL,
    ram_min       REAL             NOT NULL,
    ram_max       REAL             NOT NULL,
    gpu_n         INTEGER          NOT NULL CHECK (gpu_n BETWEEN 0 AND n),  -- gpu_util is nullable
    gpu_sum       DOUBLE PRECISION,
    gpu_min       REAL,
    gpu_max       REAL,
    net_up_sum    DOUBLE PRECISION NOT NULL,
    net_up_max    REAL             NOT NULL,
    net_down_sum  DOUBLE PRECISION NOT NULL,
    net_down_max  REAL             NOT NULL,
    uptime_max    BIGINT           NOT NULL,
    PRIMARY KEY (i_id, bucket),                                   -- per-instance range scans
    CHECK (bucket = date_bin('5 minutes', bucket, TIMESTAMPTZ 'epoch')),
    CHECK ((gpu_n = 0) = (gpu_sum IS NULL))
);
-- Refresh deletes/rebuilds a time slice across all instances.
CREATE INDEX metrics_rollup_5m_bucket_idx ON metrics_rollup_5m (bucket);

CREATE TABLE metrics_rollup_state (
    singleton    BOOLEAN     PRIMARY KEY DEFAULT true CHECK (singleton),
    watermark    TIMESTAMPTZ NOT NULL,   -- rollup covers buckets < watermark ...
    dirty_from   TIMESTAMPTZ,            -- ... except from here on (NULL = nothing dirty)
    refreshed_at TIMESTAMPTZ,
    CHECK (dirty_from IS NULL OR dirty_from < watermark),
    -- the split between rollup and raw rows must fall on a bucket boundary,
    -- or the bucket containing it would be counted twice
    CHECK (dirty_from = date_bin('5 minutes', dirty_from, TIMESTAMPTZ 'epoch'))
);
-- -infinity: nothing rolled up yet; every query reads raw rows until the first refresh.
INSERT INTO metrics_rollup_state (watermark) VALUES ('-infinity');

-- Advisory lock key shared by the invalidation trigger and the refresh.
CREATE FUNCTION metrics_rollup_lock_key() RETURNS bigint
LANGUAGE sql IMMUTABLE AS $$ SELECT 4004004004::bigint $$;

-- ---------------------------------------------------------------------
-- Invalidation: statement-level, one min() over the transition table per
-- batch. The common case (fresh samples, above the watermark) only reads
-- one row; the state row is written only when a batch is actually late.
-- SECURITY DEFINER: runs inside the ezm_ingest session, which has no rights
-- on metrics_rollup_state.
-- ---------------------------------------------------------------------
CREATE FUNCTION rollup_invalidate() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE
    batch_min timestamptz;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT min(recorded_at) INTO batch_min FROM old_rows;
    ELSE
        SELECT min(recorded_at) INTO batch_min FROM new_rows;
    END IF;
    IF batch_min IS NULL THEN
        RETURN NULL;
    END IF;

    -- Whole buckets only: the bucket holding batch_min is no longer exact.
    batch_min := date_bin('5 minutes', batch_min, TIMESTAMPTZ 'epoch');

    PERFORM pg_advisory_xact_lock_shared(metrics_rollup_lock_key());
    UPDATE metrics_rollup_state
       SET dirty_from = least(dirty_from, batch_min)        -- least() ignores NULL
     WHERE batch_min < watermark
       AND (dirty_from IS NULL OR batch_min < dirty_from);
    RETURN NULL;
END $$;

-- One trigger per event: a transition-table trigger may have only one.
CREATE TRIGGER rollup_invalidate_on_insert
AFTER INSERT ON metrics_data REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION rollup_invalidate();

CREATE TRIGGER rollup_invalidate_on_update                  -- BR4 upserts
AFTER UPDATE ON metrics_data REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION rollup_invalidate();

CREATE TRIGGER rollup_invalidate_on_delete
AFTER DELETE ON metrics_data REFERENCING OLD TABLE AS old_rows
FOR EACH STATEMENT EXECUTE FUNCTION rollup_invalidate();

-- ---------------------------------------------------------------------
-- Refresh
-- ---------------------------------------------------------------------
CREATE FUNCTION refresh_metrics_rollup(p_settle interval DEFAULT '2 minutes')
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
    origin  CONSTANT timestamptz := 'epoch';
    old_wm  timestamptz;
    dirty   timestamptz;
    new_wm  timestamptz;
    from_ts timestamptz;
    written bigint;
BEGIN
    -- Wait for ingest transactions that already compared against the old
    -- watermark; block new ones until this transaction commits.
    PERFORM pg_advisory_xact_lock(metrics_rollup_lock_key());
    SELECT watermark, dirty_from INTO old_wm, dirty FROM metrics_rollup_state FOR UPDATE;

    new_wm := greatest(old_wm, date_bin('5 minutes', now() - p_settle, origin));

    IF old_wm = '-infinity' THEN
        -- First run: start at the oldest partition's lower bound (catalog lookup, no scan).
        SELECT min(substring(c.relname FROM '\d{8}$')::date)::timestamp AT TIME ZONE 'UTC'
          INTO from_ts
          FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
         WHERE i.inhparent = 'metrics_data'::regclass
           AND c.relname ~ '^metrics_data_\d{8}$';
        from_ts := coalesce(from_ts, new_wm);
    ELSE
        from_ts := least(old_wm, dirty);
    END IF;
    from_ts := date_bin('5 minutes', from_ts, origin);

    DELETE FROM metrics_rollup_5m WHERE bucket >= from_ts AND bucket < new_wm;

    INSERT INTO metrics_rollup_5m
    SELECT m.i_id,
           date_bin('5 minutes', m.recorded_at, origin),
           count(*),
           -- sum(real) returns real; accumulate in float8 to keep precision
           sum(m.cpu_util::float8), min(m.cpu_util), max(m.cpu_util),
           sum(m.ram_util::float8), min(m.ram_util), max(m.ram_util),
           count(m.gpu_util), sum(m.gpu_util::float8), min(m.gpu_util), max(m.gpu_util),
           sum(m.net_up_kbps::float8),   max(m.net_up_kbps),
           sum(m.net_down_kbps::float8), max(m.net_down_kbps),
           max(m.uptime)
    FROM metrics_data m
    WHERE m.recorded_at >= from_ts AND m.recorded_at < new_wm
    GROUP BY 1, 2;
    GET DIAGNOSTICS written = ROW_COUNT;

    UPDATE metrics_rollup_state
       SET watermark = new_wm, dirty_from = NULL, refreshed_at = now();

    RETURN format('rollup %s..%s, %s buckets', from_ts, new_wm, written);
END $$;

-- Rollups outlive raw partitions on purpose (cheap long-range history).
CREATE FUNCTION prune_metrics_rollup(keep_days int DEFAULT 400)
RETURNS bigint LANGUAGE sql AS $$
    WITH d AS (DELETE FROM metrics_rollup_5m
               WHERE bucket < now() - make_interval(days => keep_days) RETURNING 1)
    SELECT count(*) FROM d;
$$;

-- Same signature as 000001, so the db-maint container needs no change.
-- Body = original partition maintenance + rollup refresh/prune.
CREATE OR REPLACE FUNCTION run_maintenance(keep_days int DEFAULT 30)
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
    today   date := (now() AT TIME ZONE 'UTC')::date;
    dropped int;
    rollup  text;
BEGIN
    PERFORM create_metrics_partition(today + n) FROM generate_series(-1, 7) AS n;
    -- Roll up before dropping, so data leaving the raw retention window is kept as 5-min buckets.
    -- (DROP TABLE on a partition fires no triggers, so it does not mark the rollup dirty.)
    rollup  := refresh_metrics_rollup();
    PERFORM prune_metrics_rollup();
    dropped := drop_old_metrics_partitions(keep_days);
    RETURN format('partitions ensured %s..%s, dropped %s; %s', today - 1, today + 7, dropped, rollup);
END $$;

REVOKE EXECUTE ON FUNCTION refresh_metrics_rollup(interval),
                           prune_metrics_rollup(int) FROM PUBLIC;
-- CREATE OR REPLACE keeps the 000002 REVOKE on run_maintenance.

-- ---------------------------------------------------------------------
-- Index decisions (measurements in db/QUERIES.md)
-- ---------------------------------------------------------------------
-- Added: metrics_rollup_5m PK (i_id, bucket) and (bucket), above.
-- Considered and rejected:
--   * covering (i_id, recorded_at) INCLUDE (metrics) on metrics_data:
--     +82 % of heap size, 7-day query only 82 -> 61 ms (rollup: 4 ms).
--   * partial alert_logs (i_id) WHERE status = 'active': the planner already
--     uses alert_logs_one_active for the fleet query at the same cost.
--   * (g_id, triggered_at DESC, a_id DESC) for keyset pagination: the
--     existing (g_id, triggered_at DESC) + incremental sort is equally fast.

-- ---------------------------------------------------------------------
-- Grants
-- ---------------------------------------------------------------------
GRANT SELECT ON metrics_rollup_5m, metrics_rollup_state TO ezm_api, ezm_readonly;

COMMIT;
