-- Add 'revisit_later' to the lead_activities status CHECK constraint
ALTER TABLE lead_activities DROP CONSTRAINT IF EXISTS lead_activities_status_check;
ALTER TABLE lead_activities ADD CONSTRAINT lead_activities_status_check
    CHECK (status IN ('pending', 'contacted', 'follow_up', 'revisit_later', 'converted', 'not_interested', 'closed'));
