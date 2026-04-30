ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS admin_id UUID REFERENCES users(id);
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS assigned_to UUID REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_campaigns_admin_id ON campaigns(admin_id);
CREATE INDEX IF NOT EXISTS idx_campaigns_assigned_to ON campaigns(assigned_to);
