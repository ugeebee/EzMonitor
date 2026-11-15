-- =====================================================================
-- EzMonitor alert evaluation triggers + notification (PostgreSQL 16+)
-- Owner: Alerting (Aayush)
-- Applied by golang-migrate. Never edit an applied migration.
--
-- Two responsibilities:
--   1. evaluate_alert_rules()  — statement-level trigger on metrics_data
--      that fires/resolves alerts by joining incoming samples against
--      enabled alert_rules for the same group.
--   2. notify_alert_fired()    — row-level trigger on alert_logs that
--      PG-NOTIFYs the Go alert-worker for webhook dispatch.
--
-- Both functions are SECURITY DEFINER so they execute with the
-- privileges of ezm_owner, even though the INSERT/UPDATE on
-- metrics_data is performed by the restricted ezm_ingest role.
-- =====================================================================
BEGIN;

-- ---------------------------------------------------------------------
-- 1. evaluate_alert_rules()
--    Called AFTER INSERT and AFTER UPDATE on metrics_data.
--    Uses transition table `new_rows` (one event per trigger, hence two
--    triggers).
--
--    Logic per (rule, instance) pair, samples taken in time order:
--      • Extract the metric value from the sample using a CASE over
--        metric_kind.
--      • Evaluate the comparison operator using a CASE over cmp_op.
--      • If the condition is TRUE  → fire: INSERT into alert_logs with
--        ON CONFLICT DO NOTHING (idempotent when an active alert exists).
--      • If the condition is FALSE → resolve: UPDATE the active alert
--        row to 'resolved'. br7_freeze_resolved prevents re-opening.
-- ---------------------------------------------------------------------
CREATE FUNCTION evaluate_alert_rules()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    -- A batch can hold several samples per instance (the agent flushes 5 at
    -- a time; backfills and BR4 re-deliveries send more). Evaluating every
    -- sample independently let an EARLIER normal sample "resolve" an alert a
    -- LATER sample fired, writing resolved_at < triggered_at; the CHECK on
    -- alert_logs then rejected the whole ingest batch, so samples were lost
    -- exactly during incidents. Each (rule, instance) pair is now evaluated
    -- in time order:
    --   fire    : at its FIRST breaching sample in the batch
    --   resolve : only if its LATEST sample is within the threshold, at that
    --             sample's time, and never before the alert's own start
    -- The same CTE is repeated in both statements so the UPDATE runs after
    -- the INSERT and can resolve an alert fired earlier in the same batch.

    -- ── Fire ────────────────────────────────────────────────────────
    WITH evaluated AS (
        SELECT ar.rule_id, n.i_id, inst.g_id, n.recorded_at, v.val,
               CASE ar.operator
                   WHEN '>'  THEN v.val >  ar.threshold_value
                   WHEN '>=' THEN v.val >= ar.threshold_value
                   WHEN '<'  THEN v.val <  ar.threshold_value
                   WHEN '<=' THEN v.val <= ar.threshold_value
               END AS breached
        FROM new_rows n
        JOIN instances inst
          ON inst.i_id = n.i_id
         AND inst.enabled                       -- skip disabled instances
        JOIN alert_rules ar
          ON ar.g_id = inst.g_id
         AND ar.enabled                         -- skip disabled rules
        CROSS JOIN LATERAL (
            SELECT CASE ar.metric_name
                       WHEN 'cpu_util'      THEN n.cpu_util::double precision
                       WHEN 'gpu_util'      THEN n.gpu_util::double precision
                       WHEN 'ram_util'      THEN n.ram_util::double precision
                       WHEN 'uptime'        THEN n.uptime::double precision
                       WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                       WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                   END AS val
        ) v
        WHERE v.val IS NOT NULL                 -- no GPU: gpu rules are not evaluated
    )
    INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at)
    SELECT DISTINCT ON (e.rule_id, e.i_id)
           e.rule_id, e.i_id, e.g_id, e.val, e.recorded_at
    FROM evaluated e
    WHERE e.breached
    ORDER BY e.rule_id, e.i_id, e.recorded_at
    ON CONFLICT (rule_id, i_id) WHERE status = 'active'
    DO NOTHING;

    -- ── Resolve ─────────────────────────────────────────────────────
    WITH evaluated AS (
        SELECT ar.rule_id, n.i_id, inst.g_id, n.recorded_at, v.val,
               CASE ar.operator
                   WHEN '>'  THEN v.val >  ar.threshold_value
                   WHEN '>=' THEN v.val >= ar.threshold_value
                   WHEN '<'  THEN v.val <  ar.threshold_value
                   WHEN '<=' THEN v.val <= ar.threshold_value
               END AS breached
        FROM new_rows n
        JOIN instances inst
          ON inst.i_id = n.i_id
         AND inst.enabled                       -- skip disabled instances
        JOIN alert_rules ar
          ON ar.g_id = inst.g_id
         AND ar.enabled                         -- skip disabled rules
        CROSS JOIN LATERAL (
            SELECT CASE ar.metric_name
                       WHEN 'cpu_util'      THEN n.cpu_util::double precision
                       WHEN 'gpu_util'      THEN n.gpu_util::double precision
                       WHEN 'ram_util'      THEN n.ram_util::double precision
                       WHEN 'uptime'        THEN n.uptime::double precision
                       WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                       WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                   END AS val
        ) v
        WHERE v.val IS NOT NULL                 -- no GPU: gpu rules are not evaluated
    ),
    latest AS (
        SELECT DISTINCT ON (e.rule_id, e.i_id)
               e.rule_id, e.i_id, e.recorded_at, e.breached
        FROM evaluated e
        ORDER BY e.rule_id, e.i_id, e.recorded_at DESC
    )
    UPDATE alert_logs al
    SET    status      = 'resolved',
           resolved_at = l.recorded_at
    FROM latest l
    WHERE NOT l.breached
      AND al.rule_id      = l.rule_id
      AND al.i_id         = l.i_id
      AND al.status       = 'active'
      AND al.triggered_at <= l.recorded_at;

    RETURN NULL;   -- statement-level trigger must return NULL
END;
$$;

-- Two triggers because a transition-table trigger allows only one event.
-- BR4 re-deliveries arrive as UPDATEs (ON CONFLICT ... DO UPDATE), so
-- we need both INSERT and UPDATE.
CREATE TRIGGER t2_evaluate_alerts_on_insert
AFTER INSERT ON metrics_data
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION evaluate_alert_rules();

CREATE TRIGGER t2_evaluate_alerts_on_update
AFTER UPDATE ON metrics_data
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION evaluate_alert_rules();

-- ---------------------------------------------------------------------
-- 2. notify_alert_fired()
--    Row-level AFTER INSERT on alert_logs.  Sends a JSON payload over
--    the 'alert_fired' PG NOTIFY channel so the Go alert-worker can
--    dispatch webhooks asynchronously.
-- ---------------------------------------------------------------------
CREATE FUNCTION notify_alert_fired()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    PERFORM pg_notify(
        'alert_fired',
        json_build_object(
            'a_id',         NEW.a_id,
            'rule_id',      NEW.rule_id,
            'i_id',         NEW.i_id,
            'g_id',         NEW.g_id,
            'alert_value',  NEW.alert_value,
            'triggered_at', NEW.triggered_at
        )::text
    );
    RETURN NEW;
END;
$$;

CREATE TRIGGER t3_notify_alert_fired
AFTER INSERT ON alert_logs
FOR EACH ROW
EXECUTE FUNCTION notify_alert_fired();

-- ---------------------------------------------------------------------
-- Privilege adjustments
-- ---------------------------------------------------------------------
-- evaluate_alert_rules() and notify_alert_fired() are SECURITY DEFINER
-- owned by ezm_owner, so they already have full access to alert_rules,
-- alert_logs, instances, etc.
--
-- The triggers fire inside the ezm_ingest session. PostgreSQL requires
-- EXECUTE privilege on the trigger function for the session role. Since
-- both functions are SECURITY DEFINER and owned by ezm_owner, and
-- pg_trigger functions inherit EXECUTE from PUBLIC by default, no
-- explicit GRANT is needed — but we add one for clarity / defense.
GRANT EXECUTE ON FUNCTION evaluate_alert_rules() TO ezm_ingest;
GRANT EXECUTE ON FUNCTION notify_alert_fired()   TO ezm_ingest;

COMMIT;
