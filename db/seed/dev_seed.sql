-- =====================================================================
-- Development fixtures. NOT a migration: never run against a real deployment.
--   docker compose --profile seed run --rm seed
--
-- Logins (password = the part before @ + "123"):
--   admin@ezmonitor.dev  / admin123   -> admin of "Team 4 Lab", viewer of "Other Tenant"
--   editor@ezmonitor.dev / editor123  -> editor of "Team 4 Lab"
--   viewer@ezmonitor.dev / viewer123  -> viewer of "Team 4 Lab"
--   outsider@ezmonitor.dev / outsider123 -> admin of "Other Tenant" only (tenant-isolation tests)
--
-- Agent credentials: instance N uses token  dev-token-000N  (N = 1..11, zero-padded to 4)
--   e.g. instance_id 1 / token dev-token-0001
--   dev-box-10 is DISABLED (use it to test trigger T1).
--   dev-box-11 belongs to "Other Tenant".
-- =====================================================================
\set ON_ERROR_STOP on
BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users) THEN
        RAISE EXCEPTION 'dev_seed: database already has users; reset with "docker compose down -v" first';
    END IF;
END $$;

INSERT INTO users (name, email, hash_pass)
SELECT initcap(u), u || '@ezmonitor.dev', crypt(u || '123', gen_salt('bf', 10))
FROM unnest(ARRAY['admin', 'editor', 'viewer', 'outsider']) AS u;

INSERT INTO groups (label) VALUES ('Team 4 Lab'), ('Other Tenant');

INSERT INTO user_group_roles (u_id, g_id, role_id)
SELECT u.u_id, g.g_id, m.role_id
FROM (VALUES
        ('admin@ezmonitor.dev',    'Team 4 Lab',   1),
        ('admin@ezmonitor.dev',    'Other Tenant', 3),
        ('editor@ezmonitor.dev',   'Team 4 Lab',   2),
        ('viewer@ezmonitor.dev',   'Team 4 Lab',   3),
        ('outsider@ezmonitor.dev', 'Other Tenant', 1)
     ) AS m(email, grp, role_id)
JOIN users  u ON u.email = m.email
JOIN groups g ON g.label = m.grp;

-- 10 instances in Team 4 Lab + 1 in Other Tenant; inserted in order so i_id = n on a fresh DB.
INSERT INTO instances (g_id, label, fingerprint, token_hash, enabled)
SELECT g.g_id,
       format('dev-box-%s', lpad(n::text, 2, '0')),
       lpad(to_hex(n), 16, '0'),
       encode(sha256(convert_to(format('dev-token-%s', lpad(n::text, 4, '0')), 'UTF8')), 'hex'),
       n <> 10
FROM generate_series(1, 11) AS n
JOIN groups g ON g.label = CASE WHEN n = 11 THEN 'Other Tenant' ELSE 'Team 4 Lab' END
ORDER BY n;

INSERT INTO instance_labels (i_id, label_key, label_value)
SELECT i.i_id, l.k, l.v
FROM instances i
CROSS JOIN LATERAL (VALUES
    ('env',  CASE WHEN i.i_id <= 5 THEN 'prod' ELSE 'dev' END),
    ('rack', 'r' || ((i.i_id - 1) / 4 + 1)),
    ('gpu',  CASE WHEN i.i_id % 2 = 0 THEN 'nvidia' ELSE 'none' END)
) AS l(k, v);

INSERT INTO alert_rules (g_id, metric_name, operator, threshold_value)
SELECT g.g_id, r.metric::metric_kind, r.op::cmp_op, r.thr
FROM groups g
CROSS JOIN (VALUES ('cpu_util', '>', 90.0),
                   ('ram_util', '>', 85.0),
                   ('gpu_util', '>=', 95.0)) AS r(metric, op, thr)
WHERE g.label = 'Team 4 Lab';

INSERT INTO alert_rules (g_id, metric_name, operator, threshold_value)
SELECT g_id, 'cpu_util', '>', 80 FROM groups WHERE label = 'Other Tenant';

-- Replace the URL with your own https://webhook.site/<uuid> to watch deliveries.
INSERT INTO webhooks (g_id, webhook_url, secret)
SELECT g_id, 'https://webhook.site/replace-with-your-uuid', 'dev-webhook-secret'
FROM groups WHERE label = 'Team 4 Lab';

INSERT INTO alert_rule_webhooks (rule_id, web_id, g_id)
SELECT r.rule_id, w.web_id, r.g_id
FROM alert_rules r
JOIN webhooks w ON w.g_id = r.g_id;

COMMIT;

SELECT 'seeded' AS status,
       (SELECT count(*) FROM users)     AS users,
       (SELECT count(*) FROM instances) AS instances,
       (SELECT count(*) FROM alert_rules) AS rules;
