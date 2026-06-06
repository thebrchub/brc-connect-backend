-- Composite index for fast COUNT and pagination on employee's pending leads
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_activities_employee_status
    ON lead_activities(employee_id, status);

-- Covering index for the ORDER BY in GetFreshLeads (pending leads sorted by created_at)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lead_activities_employee_pending_created
    ON lead_activities(employee_id, created_at ASC) WHERE status = 'pending';
