BEGIN;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM ezm_alert;
REVOKE USAGE ON SCHEMA public FROM ezm_alert;
DROP VIEW IF EXISTS effective_group_roles;
ALTER TABLE user_group_roles DROP COLUMN IF EXISTS date_joined;
COMMIT;
