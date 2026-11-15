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
--    Logic per (sample, rule) pair:
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
    -- ── Fire new alerts ─────────────────────────────────────────────
    -- For every (sample, rule) pair where the threshold condition is
    -- met, attempt to insert an active alert.  The unique partial
    -- index alert_logs_one_active ensures at most one active alert
    -- per (rule_id, i_id), so the ON CONFLICT clause makes this a
    -- no-op when an alert is already active.
    INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at)
    SELECT
        ar.rule_id,
        n.i_id,
        inst.g_id,
        -- Extract the metric value that this rule monitors
        CASE ar.metric_name
            WHEN 'cpu_util'      THEN n.cpu_util::double precision
            WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
            WHEN 'ram_util'      THEN n.ram_util::double precision
            WHEN 'uptime'        THEN n.uptime::double precision
            WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
            WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
        END,
        n.recorded_at
    FROM new_rows n
    JOIN instances inst
        ON inst.i_id = n.i_id
       AND inst.enabled                       -- skip disabled instances
    JOIN alert_rules ar
        ON ar.g_id = inst.g_id
       AND ar.enabled                         -- skip disabled rules
    WHERE
        -- Evaluate the comparison: metric_value <op> threshold
        CASE ar.operator
            WHEN '>'  THEN
                CASE ar.metric_name
                    WHEN 'cpu_util'      THEN n.cpu_util::double precision
                    WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                    WHEN 'ram_util'      THEN n.ram_util::double precision
                    WHEN 'uptime'        THEN n.uptime::double precision
                    WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                    WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                END > ar.threshold_value
            WHEN '>=' THEN
                CASE ar.metric_name
                    WHEN 'cpu_util'      THEN n.cpu_util::double precision
                    WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                    WHEN 'ram_util'      THEN n.ram_util::double precision
                    WHEN 'uptime'        THEN n.uptime::double precision
                    WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                    WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                END >= ar.threshold_value
            WHEN '<'  THEN
                CASE ar.metric_name
                    WHEN 'cpu_util'      THEN n.cpu_util::double precision
                    WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                    WHEN 'ram_util'      THEN n.ram_util::double precision
                    WHEN 'uptime'        THEN n.uptime::double precision
                    WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                    WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                END < ar.threshold_value
            WHEN '<=' THEN
                CASE ar.metric_name
                    WHEN 'cpu_util'      THEN n.cpu_util::double precision
                    WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                    WHEN 'ram_util'      THEN n.ram_util::double precision
                    WHEN 'uptime'        THEN n.uptime::double precision
                    WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                    WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                END <= ar.threshold_value
        END
    ON CONFLICT (rule_id, i_id) WHERE status = 'active'
    DO NOTHING;

    -- ── Resolve stale alerts ────────────────────────────────────────
    -- For every (sample, rule) pair where the threshold condition is
    -- NO LONGER met, mark the currently-active alert as resolved.
    -- br7_freeze_resolved prevents any further modification to the
    -- resolved row, so a re-fire will create a new alert_logs entry.
    UPDATE alert_logs al
    SET    status      = 'resolved',
           resolved_at = sub.recorded_at
    FROM (
        SELECT
            ar.rule_id,
            n.i_id,
            n.recorded_at
        FROM new_rows n
        JOIN instances inst
            ON inst.i_id = n.i_id
           AND inst.enabled
        JOIN alert_rules ar
            ON ar.g_id = inst.g_id
           AND ar.enabled
        WHERE
            -- Condition is FALSE → resolve
            NOT (
                CASE ar.operator
                    WHEN '>'  THEN
                        CASE ar.metric_name
                            WHEN 'cpu_util'      THEN n.cpu_util::double precision
                            WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                            WHEN 'ram_util'      THEN n.ram_util::double precision
                            WHEN 'uptime'        THEN n.uptime::double precision
                            WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                            WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                        END > ar.threshold_value
                    WHEN '>=' THEN
                        CASE ar.metric_name
                            WHEN 'cpu_util'      THEN n.cpu_util::double precision
                            WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                            WHEN 'ram_util'      THEN n.ram_util::double precision
                            WHEN 'uptime'        THEN n.uptime::double precision
                            WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                            WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                        END >= ar.threshold_value
                    WHEN '<'  THEN
                        CASE ar.metric_name
                            WHEN 'cpu_util'      THEN n.cpu_util::double precision
                            WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                            WHEN 'ram_util'      THEN n.ram_util::double precision
                            WHEN 'uptime'        THEN n.uptime::double precision
                            WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                            WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                        END < ar.threshold_value
                    WHEN '<=' THEN
                        CASE ar.metric_name
                            WHEN 'cpu_util'      THEN n.cpu_util::double precision
                            WHEN 'gpu_util'      THEN COALESCE(n.gpu_util, 0)::double precision
                            WHEN 'ram_util'      THEN n.ram_util::double precision
                            WHEN 'uptime'        THEN n.uptime::double precision
                            WHEN 'net_up_kbps'   THEN n.net_up_kbps::double precision
                            WHEN 'net_down_kbps' THEN n.net_down_kbps::double precision
                        END <= ar.threshold_value
                END
            )
    ) sub
    WHERE al.rule_id = sub.rule_id
      AND al.i_id    = sub.i_id
      AND al.status  = 'active';

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
