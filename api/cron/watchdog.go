package cron

import (
	"context"
	"log"

	json "github.com/goccy/go-json"

	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/config"
	"brc-connect-backend/api/repository"
)

type Watchdog struct {
	jobRepo *repository.JobRepo
	cfg     config.Config
}

func NewWatchdog(jobRepo *repository.JobRepo, cfg config.Config) *Watchdog {
	return &Watchdog{jobRepo: jobRepo, cfg: cfg}
}

// Run checks for stalled jobs, requeues or marks as dead.
func (w *Watchdog) Run(ctx context.Context) {
	stalled, err := w.jobRepo.GetStalledJobs(ctx, w.cfg.WatchdogStaleThresholdSec)
	if err != nil {
		log.Printf("ERROR [watchdog] - failed to get stalled jobs error=%s", err)
		return
	}

	for _, job := range stalled {
		// Send kill signal to Node.js scraper via pub/sub
		if err := redis.Publish(ctx, "job_kill", job.ID); err != nil {
			log.Printf("ERROR [watchdog] - publish kill signal failed job_id=%s error=%s", job.ID, err)
		}

		if job.AttemptCount >= w.cfg.WatchdogMaxAttempts {
			if err := w.jobRepo.MarkDead(ctx, job.ID); err != nil {
				log.Printf("ERROR [watchdog] - mark dead failed job_id=%s error=%s", job.ID, err)
			}
			log.Printf("WARN  [watchdog] - job moved to dead letter job_id=%s attempts=%d", job.ID, job.AttemptCount)
		} else {
			if err := w.jobRepo.RequeueJob(ctx, job.ID); err != nil {
				log.Printf("ERROR [watchdog] - requeue failed job_id=%s error=%s", job.ID, err)
				continue
			}
			// Re-enqueue to scrape queue
			b, err := json.Marshal(map[string]string{
				"campaign_id": job.CampaignID,
				"job_id":      job.ID,
				"source":      job.Source,
				"city":        job.City,
				"category":    job.Category,
			})
			if err != nil {
				log.Printf("ERROR [watchdog] - marshal job payload failed job_id=%s error=%s", job.ID, err)
				continue
			}
			if err := redis.Enqueue(ctx, "scrape_queue", string(b), true); err != nil {
				log.Printf("ERROR [watchdog] - enqueue failed job_id=%s error=%s", job.ID, err)
				continue
			}
			log.Printf("INFO  [watchdog] - job requeued job_id=%s attempt=%d", job.ID, job.AttemptCount+1)
		}
	}
}
