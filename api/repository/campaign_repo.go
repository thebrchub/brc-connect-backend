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

type CampaignRepo struct {
	campaignTTL time.Duration
	listTTL     time.Duration
}

func NewCampaignRepo(campaignTTL, listTTL time.Duration) *CampaignRepo {
	return &CampaignRepo{campaignTTL: campaignTTL, listTTL: listTTL}
}

func (r *CampaignRepo) Insert(ctx context.Context, c models.Campaign) (string, error) {
	c.ID = uuid.NewString()
	_, err := postgress.Exec(ctx,
		`INSERT INTO campaigns (id, name, sources, cities, categories, status, auto_rescrape, drop_no_contact, jobs_total, jobs_completed, leads_found, admin_id, assigned_to, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),NOW())`,
		c.ID, c.Name, c.Sources, c.Cities, c.Categories, c.Status, c.AutoRescrape, c.DropNoContact, c.JobsTotal, c.JobsCompleted, c.LeadsFound, c.AdminID, c.AssignedTo)
	if err == nil {
		r.invalidateListCache(ctx)
	}
	return c.ID, err
}

func (r *CampaignRepo) GetByID(ctx context.Context, id string) (*models.Campaign, error) {
	camp, err := redis.Fetch(ctx, "campaign:"+id, r.campaignTTL, func(ctx context.Context) (*models.Campaign, error) {
		var c models.Campaign
		found, err := postgress.Get(ctx, "campaigns", id, &c)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return &c, nil
	})
	if err != nil {
		return nil, err
	}
	return camp, nil
}

func (r *CampaignRepo) GetAll(ctx context.Context, page, pageSize int) ([]models.Campaign, int, error) {
	cacheKey := fmt.Sprintf("campaigns:list:%d:%d", page, pageSize)

	type listResult struct {
		Campaigns []models.Campaign `json:"campaigns"`
		Total     int               `json:"total"`
	}

	result, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*listResult, error) {
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) FROM campaigns")
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		campaigns, err := postgress.Query[models.Campaign](ctx,
			fmt.Sprintf("SELECT * FROM campaigns ORDER BY created_at DESC LIMIT %d OFFSET %d", pageSize, offset))
		if err != nil {
			return nil, err
		}
		return &listResult{Campaigns: campaigns, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	return result.Campaigns, result.Total, nil
}

func (r *CampaignRepo) GetStatus(ctx context.Context, id string) (*models.Campaign, error) {
	return r.GetByID(ctx, id)
}

func (r *CampaignRepo) IncrementLeads(ctx context.Context, id string, count int) error {
	_, err := postgress.Exec(ctx, "UPDATE campaigns SET leads_found = leads_found + $1, updated_at = NOW() WHERE id = $2", count, id)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

func (r *CampaignRepo) IncrementJobsCompleted(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx, "UPDATE campaigns SET jobs_completed = jobs_completed + 1, updated_at = NOW() WHERE id = $1", id)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

// IncrementOnJobComplete atomically increments both leads_found and jobs_completed in one query.
func (r *CampaignRepo) IncrementOnJobComplete(ctx context.Context, id string, leadsFound int) error {
	_, err := postgress.Exec(ctx,
		"UPDATE campaigns SET leads_found = leads_found + $1, jobs_completed = jobs_completed + 1, updated_at = NOW() WHERE id = $2",
		leadsFound, id)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

// SyncProgressFromJobs recomputes campaign counters from scrape_jobs.
// This keeps counters idempotent across retries and repeated status events.
func (r *CampaignRepo) SyncProgressFromJobs(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		`UPDATE campaigns c
		 SET leads_found = COALESCE((
			SELECT SUM(j.leads_found)
			FROM scrape_jobs j
			WHERE j.campaign_id = c.id
		 ), 0),
		 jobs_completed = COALESCE((
			SELECT COUNT(*)
			FROM scrape_jobs j
			WHERE j.campaign_id = c.id
			  AND j.status IN ('completed', 'timeout', 'failed', 'dead')
		 ), 0),
		 updated_at = NOW()
		 WHERE c.id = $1`,
		id,
	)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

func (r *CampaignRepo) GetAutoRescrape(ctx context.Context) ([]models.Campaign, error) {
	return postgress.Query[models.Campaign](ctx, "SELECT * FROM campaigns WHERE auto_rescrape = true AND status = 'active'")
}

// MarkCompletedIfDone sets campaign status to 'completed' if all jobs are finished.
func (r *CampaignRepo) MarkCompletedIfDone(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		`UPDATE campaigns SET status = 'completed', updated_at = NOW()
		 WHERE id = $1 AND status = 'active' AND jobs_completed >= jobs_total AND jobs_total > 0`,
		id)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

// MarkFailedIfDone sets campaign status to 'failed' when all jobs are terminal
// and at least one job failed.
func (r *CampaignRepo) MarkFailedIfDone(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		`UPDATE campaigns c
		 SET status = 'failed', updated_at = NOW()
		 WHERE c.id = $1
		   AND c.status = 'active'
		   AND c.jobs_total > 0
		   AND c.jobs_completed >= c.jobs_total
		   AND EXISTS (
			   SELECT 1 FROM scrape_jobs j
			   WHERE j.campaign_id = c.id AND j.status = 'failed'
		   )`,
		id,
	)
	if err == nil {
		redis.Remove(ctx, "campaign:"+id)
		r.invalidateListCache(ctx)
	}
	return err
}

// CountTodayWithLeads returns the number of campaigns created today that have leads_found > 0
// or are still active (not yet completed/timed out). Used to enforce daily creation limits.
func (r *CampaignRepo) CountTodayWithLeads(ctx context.Context) (int, error) {
	rows, err := postgress.Query[struct {
		Count int `db:"count"`
	}](ctx, `SELECT COUNT(*) FROM campaigns
		WHERE created_at AT TIME ZONE 'UTC' >= (NOW() AT TIME ZONE 'UTC')::date
		AND (leads_found > 0 OR status = 'active')`)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Count, nil
}

func (r *CampaignRepo) GetByAdmin(ctx context.Context, adminID string, page, pageSize int) ([]models.Campaign, int, error) {
	cacheKey := fmt.Sprintf("campaigns:admin:%s:%d:%d", adminID, page, pageSize)

	type listResult struct {
		Campaigns []models.Campaign `json:"campaigns"`
		Total     int               `json:"total"`
	}

	result, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*listResult, error) {
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) AS count FROM campaigns WHERE admin_id = $1", adminID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		campaigns, err := postgress.Query[models.Campaign](ctx,
			"SELECT * FROM campaigns WHERE admin_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
			adminID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		return &listResult{Campaigns: campaigns, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	return result.Campaigns, result.Total, nil
}

func (r *CampaignRepo) GetByEmployee(ctx context.Context, employeeID string, page, pageSize int) ([]models.Campaign, int, error) {
	cacheKey := fmt.Sprintf("campaigns:employee:%s:%d:%d", employeeID, page, pageSize)

	type listResult struct {
		Campaigns []models.Campaign `json:"campaigns"`
		Total     int               `json:"total"`
	}

	result, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*listResult, error) {
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) AS count FROM campaigns WHERE assigned_to = $1", employeeID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		campaigns, err := postgress.Query[models.Campaign](ctx,
			"SELECT * FROM campaigns WHERE assigned_to = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
			employeeID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		return &listResult{Campaigns: campaigns, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	return result.Campaigns, result.Total, nil
}

// invalidateListCache removes all cached campaign list pages.
func (r *CampaignRepo) invalidateListCache(ctx context.Context) {
	client := redis.GetRawClient()
	patterns := []string{"sales:campaigns:list:*", "sales:campaigns:admin:*", "sales:campaigns:employee:*"}
	for _, p := range patterns {
		iter := client.Scan(ctx, 0, p, 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	}
}

// AssignEmployee sets assigned_to on a campaign.
func (r *CampaignRepo) AssignEmployee(ctx context.Context, campaignID, employeeID string) error {
	_, err := postgress.Exec(ctx, "UPDATE campaigns SET assigned_to = $1, updated_at = NOW() WHERE id = $2", employeeID, campaignID)
	if err != nil {
		return err
	}
	r.invalidateListCache(ctx)
	redis.Remove(ctx, "campaign:"+campaignID)
	return nil
}
