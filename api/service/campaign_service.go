package service

import (
	"context"
	"fmt"
	"time"

	json "github.com/goccy/go-json"

	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/config"
	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
)

type CampaignService struct {
	campaignRepo *repository.CampaignRepo
	jobRepo      *repository.JobRepo
	activityRepo *repository.ActivityRepo
	cfg          config.Config
}

func NewCampaignService(campaignRepo *repository.CampaignRepo, jobRepo *repository.JobRepo, activityRepo *repository.ActivityRepo, cfg config.Config) *CampaignService {
	return &CampaignService{
		campaignRepo: campaignRepo,
		jobRepo:      jobRepo,
		activityRepo: activityRepo,
		cfg:          cfg,
	}
}

// ErrDailyLimitReached is returned when the daily campaign creation limit is exceeded.
var ErrDailyLimitReached = fmt.Errorf("daily campaign limit reached")

// dailyLimitKey returns a Redis key scoped to today's UTC date.
func dailyLimitKey() string {
	return fmt.Sprintf("campaign_limit:%s", time.Now().UTC().Format("2006-01-02"))
}

// Create inserts a campaign, generates one job per source×city×category combination,
// and enqueues all jobs to the scrape queue.
func (s *CampaignService) Create(ctx context.Context, c models.Campaign) (*models.Campaign, error) {
	// Atomic daily limit check via Redis INCR
	rdb := redis.GetRawClient()
	key := dailyLimitKey()

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("check daily limit: %w", err)
	}
	// Set expiry on first creation (key didn't exist before, INCR returns 1)
	if count == 1 {
		rdb.Expire(ctx, key, 25*time.Hour) // 25h to cover timezone edge cases
	}

	if count > int64(s.cfg.DailyCampaignLimit) {
		// Roll back the increment since we're rejecting this request
		rdb.Decr(ctx, key)
		return nil, ErrDailyLimitReached
	}

	c.Status = "active"
	var jobs []models.ScrapeJob

	for _, source := range c.Sources {
		for _, city := range c.Cities {
			for _, category := range c.Categories {
				jobs = append(jobs, models.ScrapeJob{
					Source:         source,
					City:           city,
					Category:       category,
					Status:         "pending",
					MaxAttempts:    s.cfg.WatchdogMaxAttempts,
					TimeoutSeconds: s.cfg.WatchdogStaleThresholdSec,
				})
			}
		}
	}

	c.JobsTotal = len(jobs)
	id, err := s.campaignRepo.Insert(ctx, c)
	if err != nil {
		return nil, err
	}
	c.ID = id

	for i := range jobs {
		jobs[i].CampaignID = c.ID
	}

	err = s.jobRepo.InsertBatch(ctx, jobs)
	if err != nil {
		return nil, err
	}

	// Enqueue jobs to Redis for Node.js scraper to pick up
	queuePayloads := make([]string, len(jobs))
	for i, j := range jobs {
		b, err := json.Marshal(map[string]any{
			"campaign_id":     c.ID,
			"job_id":          j.ID,
			"source":          j.Source,
			"city":            j.City,
			"category":        j.Category,
			"drop_no_contact": c.DropNoContact,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal job payload: %w", err)
		}
		queuePayloads[i] = string(b)
	}
	err = redis.EnqueueBatch(ctx, "scrape_queue", queuePayloads, true)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// GetStatus returns the campaign with its current job progress.
func (s *CampaignService) GetStatus(ctx context.Context, id string) (*models.Campaign, error) {
	return s.campaignRepo.GetByID(ctx, id)
}

// GetAll returns paginated campaigns.
func (s *CampaignService) GetAll(ctx context.Context, page, pageSize int) ([]models.Campaign, int, error) {
	return s.campaignRepo.GetAll(ctx, page, pageSize)
}

// GetByAdmin returns paginated campaigns scoped to an admin.
func (s *CampaignService) GetByAdmin(ctx context.Context, adminID string, page, pageSize int) ([]models.Campaign, int, error) {
	return s.campaignRepo.GetByAdmin(ctx, adminID, page, pageSize)
}

// GetByEmployee returns paginated campaigns assigned to an employee.
func (s *CampaignService) GetByEmployee(ctx context.Context, employeeID string, page, pageSize int) ([]models.Campaign, int, error) {
	return s.campaignRepo.GetByEmployee(ctx, employeeID, page, pageSize)
}

// AssignEmployee sets the assigned_to field on a campaign and populates lead_activities.
func (s *CampaignService) AssignEmployee(ctx context.Context, campaignID, employeeID string) error {
	if err := s.campaignRepo.AssignEmployee(ctx, campaignID, employeeID); err != nil {
		return err
	}
	return s.activityRepo.PopulateForCampaign(ctx, employeeID, campaignID)
}
