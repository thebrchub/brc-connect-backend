package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/postgress"

	"brc-connect-backend/api/models"
)

type JobRepo struct{}

func NewJobRepo() *JobRepo { return &JobRepo{} }

func (r *JobRepo) InsertBatch(ctx context.Context, jobs []models.ScrapeJob) error {
	for i := range jobs {
		jobs[i].ID = uuid.NewString()
		_, err := postgress.Exec(ctx,
			`INSERT INTO scrape_jobs (id, campaign_id, source, city, category, status, attempt_count, max_attempts, timeout_seconds, leads_found, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())`,
			jobs[i].ID, jobs[i].CampaignID, jobs[i].Source, jobs[i].City, jobs[i].Category, jobs[i].Status,
			jobs[i].AttemptCount, jobs[i].MaxAttempts, jobs[i].TimeoutSeconds, jobs[i].LeadsFound)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *JobRepo) GetByID(ctx context.Context, id string) (*models.ScrapeJob, error) {
	var job models.ScrapeJob
	found, err := postgress.Get(ctx, "scrape_jobs", id, &job)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &job, nil
}

func (r *JobRepo) UpdateStatus(ctx context.Context, id, status string, leadsFound int, lastError string) error {
	now := time.Now()
	sql := "UPDATE scrape_jobs SET status = $1, leads_found = $2, updated_at = $3"
	args := []any{status, leadsFound, now}
	argIdx := 4

	if status == "completed" || status == "timeout" {
		sql += fmt.Sprintf(", completed_at = $%d", argIdx)
		args = append(args, now)
		argIdx++
	}
	if status == "dead" {
		sql += fmt.Sprintf(", died_at = $%d", argIdx)
		args = append(args, now)
		argIdx++
	}
	if lastError != "" {
		sql += fmt.Sprintf(", last_error = $%d", argIdx)
		args = append(args, lastError)
		argIdx++
	}
	if status == "in_progress" {
		sql += fmt.Sprintf(", started_at = $%d", argIdx)
		args = append(args, now)
		argIdx++
	}

	sql += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	_, err := postgress.Exec(ctx, sql, args...)
	return err
}

func (r *JobRepo) GetStalledJobs(ctx context.Context, thresholdSec int) ([]models.ScrapeJob, error) {
	return postgress.Query[models.ScrapeJob](ctx,
		`SELECT * FROM scrape_jobs 
		WHERE status = 'in_progress' 
		AND updated_at < NOW() - $1 * INTERVAL '1 second'`, thresholdSec,
	)
}

func (r *JobRepo) RequeueJob(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		"UPDATE scrape_jobs SET status = 'pending', attempt_count = attempt_count + 1, updated_at = NOW() WHERE id = $1", id)
	return err
}

func (r *JobRepo) MarkDead(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		"UPDATE scrape_jobs SET status = 'dead', died_at = NOW(), updated_at = NOW() WHERE id = $1", id)
	return err
}

func (r *JobRepo) RetryDead(ctx context.Context, id string) error {
	_, err := postgress.Exec(ctx,
		"UPDATE scrape_jobs SET status = 'pending', attempt_count = 0, died_at = NULL, updated_at = NOW() WHERE id = $1", id)
	return err
}

func (r *JobRepo) GetByCampaign(ctx context.Context, campaignID string) ([]models.ScrapeJob, error) {
	return postgress.Query[models.ScrapeJob](ctx, "SELECT * FROM scrape_jobs WHERE campaign_id = $1 ORDER BY created_at", campaignID)
}
