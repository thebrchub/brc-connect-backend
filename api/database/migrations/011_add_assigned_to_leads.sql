ALTER TABLE leads ADD COLUMN IF NOT EXISTS assigned_to UUID REFERENCES users(id);
CREATE INDEX IF NOT EXISTS idx_leads_assigned_to ON leads(assigned_to);
