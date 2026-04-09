package service

import (
	"context"
	"fmt"

	json "github.com/goccy/go-json"

	"github.com/shivanand-burli/go-starter-kit/redis"

	"sales-scrapper-backend/api/config"
	"sales-scrapper-backend/api/models"
	"sales-scrapper-backend/api/repository"
)

type CampaignService struct {
	campaignRepo *repository.CampaignRepo
	jobRepo      *repository.JobRepo
	cfg          config.Config
}

func NewCampaignService(campaignRepo *repository.CampaignRepo, jobRepo *repository.JobRepo, cfg config.Config) *CampaignService {
	return &CampaignService{
		campaignRepo: campaignRepo,
		jobRepo:      jobRepo,
		cfg:          cfg,
	}
}

// ErrDailyLimitReached is returned when the daily campaign creation limit is exceeded.
var ErrDailyLimitReached = fmt.Errorf("daily campaign limit reached")

// Create inserts a campaign, generates one job per source×city×category combination,
// and enqueues all jobs to the scrape queue.
func (s *CampaignService) Create(ctx context.Context, c models.Campaign) (*models.Campaign, error) {
	// Enforce daily campaign limit
	count, err := s.campaignRepo.CountTodayWithLeads(ctx)
	if err != nil {
		return nil, fmt.Errorf("check daily limit: %w", err)
	}
	if count >= s.cfg.DailyCampaignLimit {
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
