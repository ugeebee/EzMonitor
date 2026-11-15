-- =====================================================================
-- 000006_effective_roles (Data infrastructure: Harsha)
--
-- 1. effective_group_roles: the ONE definition of "what role does user U
--    have in group G", used by both the API middleware and the dashboard
--    queries. Before this, the API let a parent-group admin into its
--    subgroups but the dashboard queries required direct membership, so the
--    person who created a group saw empty dashboards.
--    Rule (same as middleware.GetEffectiveRoleInGroup):
--      * a direct role in G wins;
--      * otherwise an ADMIN of G's parent is ADMIN of G;
--      * other parent roles are not inherited.
--
-- 2. user_group_roles.date_joined: the members API returns date_joined, but
--    the column did not exist, so it always read 0001-01-01.
--
-- 3. ezm_alert: least-privilege role for the alert worker, which previously
--    connected as the database owner. It only needs to LISTEN (no privilege
--    required) and read what a webhook delivery needs.
-- =====================================================================
BEGIN;

CREATE VIEW effective_group_roles AS
    SELECT ugr.u_id, ugr.g_id, ugr.role_id, false AS inherited
    FROM user_group_roles ugr
    UNION ALL
    SELECT p.u_id, g.g_id, p.role_id, true AS inherited
    FROM groups g
    JOIN user_group_roles p
      ON p.g_id = g.parent_id
     AND p.role_id = 1                                   -- admin only
    WHERE NOT EXISTS (SELECT 1 FROM user_group_roles d
                      WHERE d.u_id = p.u_id AND d.g_id = g.g_id);

COMMENT ON VIEW effective_group_roles IS
    'Direct roles plus admin roles inherited from a parent group. One row per (u_id, g_id).';

ALTER TABLE user_group_roles
    ADD COLUMN date_joined TIMESTAMPTZ NOT NULL DEFAULT now();

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ezm_alert') THEN
        CREATE ROLE ezm_alert NOLOGIN;   -- login + password come from db/init on a fresh volume
    END IF;
END $$;

GRANT USAGE ON SCHEMA public TO ezm_alert;
GRANT SELECT ON alert_rule_webhooks, alert_rules TO ezm_alert;
GRANT SELECT (web_id, g_id, webhook_url, secret, status) ON webhooks TO ezm_alert;
GRANT SELECT (i_id, label) ON instances TO ezm_alert;

GRANT SELECT ON effective_group_roles TO ezm_api, ezm_readonly;

COMMIT;
