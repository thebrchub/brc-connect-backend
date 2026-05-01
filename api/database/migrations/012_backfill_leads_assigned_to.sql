-- Backfill leads.assigned_to from lead_activities for existing assignments.
UPDATE leads l
SET assigned_to = la.employee_id
FROM (
    SELECT DISTINCT ON (lead_id) lead_id, employee_id
    FROM lead_activities
    ORDER BY lead_id, updated_at DESC
) la
WHERE l.id = la.lead_id AND l.assigned_to IS NULL;
