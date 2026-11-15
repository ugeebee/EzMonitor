BEGIN;

ALTER TABLE groups DROP COLUMN IF EXISTS parent_id;

UPDATE roles SET role_name = 'editor' WHERE role_id = 2;

COMMIT;

