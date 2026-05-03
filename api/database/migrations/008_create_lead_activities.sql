CREATE TABLE IF NOT EXISTS lead_activities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id         UUID NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    employee_id     UUID NOT NULL REFERENCES users(id),
    campaign_id     UUID NOT NULL REFERENCES campaigns(id),
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'contacted', 'follow_up', 'converted', 'not_interested', 'closed')),
    notes           TEXT,
    next_action     TEXT,
    next_follow_up  TIMESTAMPTZ,
    last_contact    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lead_activities_employee ON lead_activities(employee_id);
CREATE INDEX IF NOT EXISTS idx_lead_activities_lead ON lead_activities(lead_id);
CREATE INDEX IF NOT EXISTS idx_lead_activities_campaign ON lead_activities(campaign_id);
CREATE INDEX IF NOT EXISTS idx_lead_activities_status ON lead_activities(status);
CREATE INDEX IF NOT EXISTS idx_lead_activities_follow_up ON lead_activities(next_follow_up);
