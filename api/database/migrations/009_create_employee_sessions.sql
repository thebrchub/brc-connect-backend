CREATE TABLE IF NOT EXISTS employee_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id     UUID NOT NULL REFERENCES users(id),
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actions_count   INT NOT NULL DEFAULT 0,
    ip_address      TEXT
);

CREATE INDEX IF NOT EXISTS idx_employee_sessions_employee ON employee_sessions(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_sessions_started ON employee_sessions(started_at);
