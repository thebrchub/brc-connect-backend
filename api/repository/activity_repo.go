package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/models"
)

type ActivityRepo struct {
	activityTTL time.Duration
	listTTL     time.Duration
}

func NewActivityRepo(activityTTL, listTTL time.Duration) *ActivityRepo {
	return &ActivityRepo{activityTTL: activityTTL, listTTL: listTTL}
}

// CRMLeadView is a joined view of lead + activity for employee CRM.
type CRMLeadView struct {
	// Lead fields
	LeadID       string   `db:"lead_id" json:"lead_id"`
	BusinessName string   `db:"business_name" json:"business_name"`
	PhoneE164    *string  `db:"phone_e164" json:"phone_e164,omitempty"`
	Email        *string  `db:"email" json:"email,omitempty"`
	City         string   `db:"city" json:"city"`
	Category     string   `db:"category" json:"category"`
	WebsiteURL   *string  `db:"website_url" json:"website_url,omitempty"`
	HasSSL       *bool    `db:"has_ssl" json:"has_ssl,omitempty"`
	IsMobile     *bool    `db:"is_mobile_friendly" json:"is_mobile_friendly,omitempty"`
	Source       []string `db:"source" json:"source"`

	// Activity fields
	ActivityID   string     `db:"activity_id" json:"activity_id"`
	Status       string     `db:"status" json:"status"`
	Notes        *string    `db:"notes" json:"notes"`
	NextAction   *string    `db:"next_action" json:"next_action"`
	NextFollowUp *time.Time `db:"next_follow_up" json:"next_follow_up"`
	LastContact  *time.Time `db:"last_contact" json:"last_contact"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

// GetFreshLeads returns up to 20 pending leads for an employee from assigned campaigns.
func (r *ActivityRepo) GetFreshLeads(ctx context.Context, employeeID string, page int) ([]CRMLeadView, int, error) {
	cacheKey := fmt.Sprintf("crm:leads:%s:%d", employeeID, page)

	type result struct {
		Leads []CRMLeadView `json:"leads"`
		Total int           `json:"total"`
	}

	res, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*result, error) {
		countRows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, `SELECT COUNT(*) AS count FROM lead_activities la
			JOIN campaigns c ON c.id = la.campaign_id
			WHERE c.assigned_to = $1 AND la.status = 'pending'`, employeeID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(countRows) > 0 {
			total = countRows[0].Count
		}

		offset := (page - 1) * 20
		leads, err := postgress.Query[CRMLeadView](ctx,
			`SELECT l.id AS lead_id, l.business_name, l.phone_e164, l.email, l.city, l.category,
				l.website_url, l.has_ssl, l.is_mobile_friendly, l.source,
				la.id AS activity_id, la.status, la.notes, la.next_action, la.next_follow_up, la.last_contact, la.updated_at
			FROM lead_activities la
			JOIN leads l ON l.id = la.lead_id
			JOIN campaigns c ON c.id = la.campaign_id
			WHERE c.assigned_to = $1 AND la.status = 'pending'
			ORDER BY la.created_at ASC
			LIMIT 20 OFFSET $2`, employeeID, offset)
		if err != nil {
			return nil, err
		}
		return &result{Leads: leads, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Leads, res.Total, nil
}

// GetHistory returns past contacted leads (non-pending) for an employee.
func (r *ActivityRepo) GetHistory(ctx context.Context, employeeID string, page, pageSize int) ([]CRMLeadView, int, error) {
	cacheKey := fmt.Sprintf("crm:history:%s:%d:%d", employeeID, page, pageSize)

	type result struct {
		Leads []CRMLeadView `json:"leads"`
		Total int           `json:"total"`
	}

	res, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*result, error) {
		countRows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, `SELECT COUNT(*) AS count FROM lead_activities la
			JOIN campaigns c ON c.id = la.campaign_id
			WHERE c.assigned_to = $1 AND la.status != 'pending'`, employeeID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(countRows) > 0 {
			total = countRows[0].Count
		}

		offset := (page - 1) * pageSize
		leads, err := postgress.Query[CRMLeadView](ctx,
			`SELECT l.id AS lead_id, l.business_name, l.phone_e164, l.email, l.city, l.category,
				l.website_url, l.has_ssl, l.is_mobile_friendly, l.source,
				la.id AS activity_id, la.status, la.notes, la.next_action, la.next_follow_up, la.last_contact, la.updated_at
			FROM lead_activities la
			JOIN leads l ON l.id = la.lead_id
			JOIN campaigns c ON c.id = la.campaign_id
			WHERE c.assigned_to = $1 AND la.status != 'pending'
			ORDER BY la.updated_at DESC
			LIMIT $2 OFFSET $3`, employeeID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		return &result{Leads: leads, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Leads, res.Total, nil
}

// GetByID returns a single lead activity by ID.
func (r *ActivityRepo) GetByID(ctx context.Context, id string) (*models.LeadActivity, error) {
	activity, err := redis.Fetch(ctx, "activity:"+id, r.activityTTL, func(ctx context.Context) (*models.LeadActivity, error) {
		var la models.LeadActivity
		found, err := postgress.Get(ctx, "lead_activities", id, &la)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return &la, nil
	})
	return activity, err
}

// UpdateActivity updates an employee's lead activity (status, notes, etc.).
func (r *ActivityRepo) UpdateActivity(ctx context.Context, id string, updates map[string]any) error {
	updates["updated_at"] = time.Now()

	setClauses := ""
	args := []any{}
	argIdx := 1
	for k, v := range updates {
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += fmt.Sprintf("%s = $%d", k, argIdx)
		args = append(args, v)
		argIdx++
	}
	args = append(args, id)

	_, err := postgress.Exec(ctx, fmt.Sprintf("UPDATE lead_activities SET %s WHERE id = $%d", setClauses, argIdx), args...)
	if err != nil {
		return err
	}
	redis.Remove(ctx, "activity:"+id)
	// Get the activity to know which employee cache to invalidate
	var la models.LeadActivity
	found, _ := postgress.Get(ctx, "lead_activities", id, &la)
	if found {
		r.invalidateEmployeeCache(ctx, la.EmployeeID)
		r.invalidateAdminDashboard(ctx, la.CampaignID)
	}
	return nil
}

// PopulateForCampaign bulk-inserts lead_activities for leads belonging to a campaign's admin, scoped by city+category.
func (r *ActivityRepo) PopulateForCampaign(ctx context.Context, employeeID, campaignID string) error {
	_, err := postgress.Exec(ctx,
		`INSERT INTO lead_activities (id, lead_id, employee_id, campaign_id, status, created_at, updated_at)
		 SELECT gen_random_uuid(), l.id, $1, $2, 'pending', NOW(), NOW()
		 FROM leads l
		 WHERE l.admin_id = (SELECT admin_id FROM campaigns WHERE id = $2)
		 AND EXISTS (
			SELECT 1 FROM scrape_jobs sj
			WHERE sj.campaign_id = $2
			AND LOWER(l.city) = LOWER(sj.city)
			AND LOWER(l.category) = LOWER(sj.category)
		 )
		 AND NOT EXISTS (
			SELECT 1 FROM lead_activities la WHERE la.lead_id = l.id AND la.campaign_id = $2
		 )`, employeeID, campaignID)
	if err != nil {
		return err
	}
	r.invalidateEmployeeCache(ctx, employeeID)
	return nil
}

// InsertBatch creates lead_activity rows for a batch of leads assigned to an employee.
func (r *ActivityRepo) InsertBatch(ctx context.Context, employeeID, campaignID string, leadIDs []string) error {
	for _, leadID := range leadIDs {
		id := uuid.NewString()
		_, err := postgress.Exec(ctx,
			`INSERT INTO lead_activities (id, lead_id, employee_id, campaign_id, status, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,'pending',NOW(),NOW())
			 ON CONFLICT DO NOTHING`,
			id, leadID, employeeID, campaignID)
		if err != nil {
			return err
		}
	}
	r.invalidateEmployeeCache(ctx, employeeID)
	return nil
}

// EmployeeStats holds aggregated performance metrics for an employee.
type EmployeeStats struct {
	EmployeeID       string `db:"employee_id" json:"employee_id"`
	EmployeeName     string `db:"employee_name" json:"employee_name"`
	TotalLeads       int    `db:"total_leads" json:"total_leads"`
	Contacted        int    `db:"contacted" json:"contacted"`
	Conversions      int    `db:"conversions" json:"conversions"`
	OverdueFollowUps int    `db:"overdue_follow_ups" json:"overdue_follow_ups"`
	ActivityThisWeek int    `db:"activity_this_week" json:"activity_this_week"`
}

// GetDashboard returns aggregated stats for all employees under an admin.
func (r *ActivityRepo) GetDashboard(ctx context.Context, adminID string) ([]EmployeeStats, error) {
	cacheKey := fmt.Sprintf("crm:dashboard:%s", adminID)

	type result struct {
		Stats []EmployeeStats `json:"stats"`
	}

	res, err := redis.Fetch(ctx, cacheKey, 60*time.Second, func(ctx context.Context) (*result, error) {
		stats, err := postgress.Query[EmployeeStats](ctx,
			`SELECT
				u.id AS employee_id,
				u.name AS employee_name,
				COUNT(la.id) AS total_leads,
				COUNT(la.id) FILTER (WHERE la.status != 'pending') AS contacted,
				COUNT(la.id) FILTER (WHERE la.status = 'converted') AS conversions,
				COUNT(la.id) FILTER (WHERE la.next_follow_up < CURRENT_DATE
					AND la.status NOT IN ('converted','closed','not_interested')) AS overdue_follow_ups,
				COUNT(la.id) FILTER (WHERE la.updated_at > NOW() - INTERVAL '7 days') AS activity_this_week
			FROM users u
			LEFT JOIN lead_activities la ON la.employee_id = u.id
			WHERE u.admin_id = $1 AND u.role = 'employee' AND u.is_active = true
			GROUP BY u.id, u.name
			ORDER BY conversions DESC`, adminID)
		if err != nil {
			return nil, err
		}
		return &result{Stats: stats}, nil
	})
	if err != nil {
		return nil, err
	}
	return res.Stats, nil
}

// GetEmployeeActivity returns recent activity log for a specific employee.
func (r *ActivityRepo) GetEmployeeActivity(ctx context.Context, employeeID string, page, pageSize int) ([]CRMLeadView, int, error) {
	cacheKey := fmt.Sprintf("crm:activity:%s:%d:%d", employeeID, page, pageSize)

	type result struct {
		Leads []CRMLeadView `json:"leads"`
		Total int           `json:"total"`
	}

	res, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*result, error) {
		countRows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) AS count FROM lead_activities WHERE employee_id = $1", employeeID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(countRows) > 0 {
			total = countRows[0].Count
		}

		offset := (page - 1) * pageSize
		leads, err := postgress.Query[CRMLeadView](ctx,
			`SELECT l.id AS lead_id, l.business_name, l.phone_e164, l.email, l.city, l.category,
				l.website_url, l.has_ssl, l.is_mobile_friendly, l.source,
				la.id AS activity_id, la.status, la.notes, la.next_action, la.next_follow_up, la.last_contact, la.updated_at
			FROM lead_activities la
			JOIN leads l ON l.id = la.lead_id
			WHERE la.employee_id = $1
			ORDER BY la.updated_at DESC
			LIMIT $2 OFFSET $3`, employeeID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		return &result{Leads: leads, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Leads, res.Total, nil
}

// GetEmployeeStats returns performance metrics for a single employee.
func (r *ActivityRepo) GetEmployeeStats(ctx context.Context, employeeID string) (*EmployeeStats, error) {
	cacheKey := fmt.Sprintf("crm:stats:%s", employeeID)

	stats, err := redis.Fetch(ctx, cacheKey, 60*time.Second, func(ctx context.Context) (*EmployeeStats, error) {
		rows, err := postgress.Query[EmployeeStats](ctx,
			`SELECT
				u.id AS employee_id,
				u.name AS employee_name,
				COUNT(la.id) AS total_leads,
				COUNT(la.id) FILTER (WHERE la.status != 'pending') AS contacted,
				COUNT(la.id) FILTER (WHERE la.status = 'converted') AS conversions,
				COUNT(la.id) FILTER (WHERE la.next_follow_up < CURRENT_DATE
					AND la.status NOT IN ('converted','closed','not_interested')) AS overdue_follow_ups,
				COUNT(la.id) FILTER (WHERE la.updated_at > NOW() - INTERVAL '7 days') AS activity_this_week
			FROM users u
			LEFT JOIN lead_activities la ON la.employee_id = u.id
			WHERE u.id = $1
			GROUP BY u.id, u.name`, employeeID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		return &rows[0], nil
	})
	return stats, err
}

func (r *ActivityRepo) invalidateEmployeeCache(ctx context.Context, employeeID string) {
	client := redis.GetRawClient()
	patterns := []string{
		fmt.Sprintf("sales:crm:leads:%s:*", employeeID),
		fmt.Sprintf("sales:crm:history:%s:*", employeeID),
		fmt.Sprintf("sales:crm:activity:%s:*", employeeID),
		fmt.Sprintf("sales:crm:stats:%s", employeeID),
	}
	for _, p := range patterns {
		iter := client.Scan(ctx, 0, p, 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	}
}

func (r *ActivityRepo) invalidateAdminDashboard(ctx context.Context, campaignID string) {
	// Look up admin_id from campaign to invalidate their dashboard
	type campaignAdmin struct {
		AdminID *string `db:"admin_id"`
	}
	rows, err := postgress.Query[campaignAdmin](ctx, "SELECT admin_id FROM campaigns WHERE id = $1", campaignID)
	if err != nil || len(rows) == 0 || rows[0].AdminID == nil {
		return
	}
	redis.Remove(ctx, fmt.Sprintf("crm:dashboard:%s", *rows[0].AdminID))
}
