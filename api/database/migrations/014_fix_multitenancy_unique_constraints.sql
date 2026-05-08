-- Fix multi-tenancy: Replace global UNIQUE constraints with per-admin composite unique constraints.
-- This allows different admins (tenants) to have leads with the same phone/domain.

-- Drop existing global unique constraints
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_phone_e164_key;
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_website_domain_key;

-- Drop any unique indexes that may exist instead of named constraints
DROP INDEX IF EXISTS leads_phone_e164_key;
DROP INDEX IF EXISTS leads_website_domain_key;

-- Create composite unique constraints scoped to admin_id
CREATE UNIQUE INDEX IF NOT EXISTS idx_leads_admin_phone ON leads (admin_id, phone_e164) WHERE phone_e164 IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_leads_admin_domain ON leads (admin_id, website_domain) WHERE website_domain IS NOT NULL;
