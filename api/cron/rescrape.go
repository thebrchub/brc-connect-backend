package cron

import (
	"context"
	"log"

	json "github.com/goccy/go-json"

	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/config"
	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
)

type Rescrape struct {
	campaignRepo *repository.CampaignRepo
	jobRepo      *repository.JobRepo
	cfg          config.Config
}

func NewRescrape(campaignRepo *repository.CampaignRepo, jobRepo *repository.JobRepo, cfg config.Config) *Rescrape {
	return &Rescrape{campaignRepo: campaignRepo, jobRepo: jobRepo, cfg: cfg}
}

// Run finds campaigns with auto_rescrape=true and re-creates their jobs.
func (rs *Rescrape) Run(ctx context.Context) {
	campaigns, err := rs.campaignRepo.GetAutoRescrape(ctx)
	if err != nil {
		log.Printf("ERROR [rescrape] - failed to get auto-rescrape campaigns error=%s", err)
		return
	}

	for _, c := range campaigns {
		var jobs []models.ScrapeJob
		for _, source := range c.Sources {
			for _, city := range c.Cities {
				for _, category := range c.Categories {
					jobs = append(jobs, models.ScrapeJob{
						CampaignID:     c.ID,
						Source:         source,
						City:           city,
						Category:       category,
						Status:         "pending",
						MaxAttempts:    rs.cfg.WatchdogMaxAttempts,
						TimeoutSeconds: rs.cfg.WatchdogStaleThresholdSec,
					})
				}
			}
		}

		err := rs.jobRepo.InsertBatch(ctx, jobs)
		if err != nil {
			log.Printf("ERROR [rescrape] - insert batch failed campaign_id=%s error=%s", c.ID, err)
			continue
		}

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
				log.Printf("ERROR [rescrape] - marshal job payload failed campaign_id=%s error=%s", c.ID, err)
				continue
			}
			queuePayloads[i] = string(b)
		}
		if err := redis.EnqueueBatch(ctx, "scrape_queue", queuePayloads, true); err != nil {
			log.Printf("ERROR [rescrape] - enqueue batch failed campaign_id=%s error=%s", c.ID, err)
			continue
		}
		log.Printf("INFO  [rescrape] - campaign rescrape queued campaign_id=%s jobs=%d", c.ID, len(jobs))
	}
}
