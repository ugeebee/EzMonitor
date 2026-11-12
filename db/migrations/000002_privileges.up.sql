-- =====================================================================
-- Least-privilege DB roles. Login roles + passwords are created once by
-- db/init/01-login-roles.sh; this file only creates them (NOLOGIN) if that
-- script did not run, and grants privileges.
--   ezm_ingest   -> ingest path: authenticate agent, write samples, heartbeat
--   ezm_api      -> dashboard/config API: CRUD on config, read metrics/alerts
--   ezm_readonly -> reporting / debugging; no secrets, no password hashes
-- =====================================================================
BEGIN;

DO $$
DECLARE r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['ezm_ingest', 'ezm_api', 'ezm_readonly'] LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('CREATE ROLE %I NOLOGIN', r);
        END IF;
    END LOOP;
END $$;

GRANT USAGE ON SCHEMA public TO ezm_ingest, ezm_api, ezm_readonly;

-- Maintenance functions are for the owner / db-maint only.
REVOKE EXECUTE ON FUNCTION create_metrics_partition(date),
                           drop_old_metrics_partitions(int),
                           run_maintenance(int) FROM PUBLIC;

-- ---- ingest ----
GRANT SELECT (i_id, g_id, token_hash, enabled) ON instances TO ezm_ingest;
GRANT UPDATE (last_seen_at, status)            ON instances TO ezm_ingest;
GRANT SELECT, INSERT, UPDATE                   ON metrics_data TO ezm_ingest;  -- UPDATE for BR4 upsert

-- ---- api ----
GRANT SELECT, INSERT, UPDATE, DELETE
    ON users, groups, user_group_roles, instances, instance_labels,
       alert_rules, webhooks, alert_rule_webhooks
    TO ezm_api;
GRANT SELECT         ON roles, metrics_data TO ezm_api;
GRANT SELECT, UPDATE ON alert_logs          TO ezm_api;   -- manual resolve from UI

-- ---- readonly (column grants hide hash_pass, token_hash, webhook secret) ----
GRANT SELECT ON roles, groups, user_group_roles, instance_labels, metrics_data,
                alert_rules, alert_logs, alert_rule_webhooks TO ezm_readonly;
GRANT SELECT (u_id, name, email, date_created) ON users TO ezm_readonly;
GRANT SELECT (i_id, g_id, label, fingerprint, enabled, status, date_created, last_seen_at)
    ON instances TO ezm_readonly;
GRANT SELECT (web_id, g_id, webhook_url, status, date_created) ON webhooks TO ezm_readonly;

COMMIT;
