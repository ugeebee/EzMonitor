-- =====================================================================
-- EzMonitor schema migration: Subgroups hierarchy & Role adjustments
-- Role 2 is updated to 'subadmin' (replacing 'editor')
-- Groups table receives optional parent_id to support nested subgroups
-- =====================================================================
BEGIN;

-- Update role_name for role_id 2 from 'editor' to 'subadmin'
UPDATE roles SET role_name = 'subadmin' WHERE role_id = 2;

-- Support hierarchical subgroups under parent groups
ALTER TABLE groups ADD COLUMN IF NOT EXISTS parent_id BIGINT REFERENCES groups(g_id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS groups_parent_id_idx ON groups (parent_id);

COMMIT;

