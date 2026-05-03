ALTER TABLE lead_activities
    ALTER COLUMN next_follow_up TYPE TIMESTAMPTZ USING next_follow_up::TIMESTAMPTZ;
