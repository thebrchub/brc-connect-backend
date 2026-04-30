-- Backfill existing campaigns: assign to the first super_admin found.
-- This ensures pre-existing campaigns are visible and owned.
UPDATE campaigns
SET admin_id = (SELECT id FROM users WHERE role = 'super_admin' LIMIT 1)
WHERE admin_id IS NULL;
