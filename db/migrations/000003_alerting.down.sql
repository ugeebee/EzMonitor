-- =====================================================================
-- Rollback 000003_alerting: drop triggers and functions in reverse order.
-- =====================================================================
BEGIN;

-- (No REVOKE needed: DROP FUNCTION removes its grants. The previous
-- "REVOKE ... ON FUNCTION IF EXISTS" was not valid PostgreSQL syntax.)

-- Drop triggers (must be dropped before the functions they reference)
DROP TRIGGER IF EXISTS t3_notify_alert_fired         ON alert_logs;
DROP TRIGGER IF EXISTS t2_evaluate_alerts_on_update  ON metrics_data;
DROP TRIGGER IF EXISTS t2_evaluate_alerts_on_insert  ON metrics_data;

-- Drop functions
DROP FUNCTION IF EXISTS notify_alert_fired();
DROP FUNCTION IF EXISTS evaluate_alert_rules();

COMMIT;
