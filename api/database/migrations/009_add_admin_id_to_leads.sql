-- Add admin_id to leads for multi-tenant isolation
ALTER TABLE leads ADD COLUMN IF NOT EXISTS admin_id UUID REFERENCES users(id);

-- Backfill: assign existing leads to the admin who owns the campaign that scraped them (via city+category match)
UPDATE leads l SET admin_id = (
    SELECT DISTINCT c.admin_id FROM campaigns c
    JOIN scrape_jobs sj ON sj.campaign_id = c.id
    WHERE LOWER(sj.city) = LOWER(l.city) AND LOWER(sj.category) = LOWER(l.category)
    LIMIT 1
) WHERE l.admin_id IS NULL;

-- Index for fast admin-scoped queries
CREATE INDEX IF NOT EXISTS idx_leads_admin_id ON leads(admin_id);
