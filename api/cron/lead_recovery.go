package cron

import (
	"context"
	"fmt"
	"log"
	"time"

	json "github.com/goccy/go-json"

	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
	"brc-connect-backend/api/service"
)

const maxRetryAttempts = 3

// retryEnvelope wraps any queue payload with an attempt counter.
type retryEnvelope struct {
	Attempt int             `json:"_attempt"`
	Data    json.RawMessage `json:"_data"`
}

// LeadRecovery drains lead batches and job status updates from Redis queues.
type LeadRecovery struct {
	leadSvc        *service.LeadService
	jobRepo        *repository.JobRepo
	campaignRepo   *repository.CampaignRepo
	activityRepo   *repository.ActivityRepo
	drainBatchSize int
}

func NewLeadRecovery(leadSvc *service.LeadService, jobRepo *repository.JobRepo, campaignRepo *repository.CampaignRepo, activityRepo *repository.ActivityRepo, drainBatchSize int) *LeadRecovery {
	return &LeadRecovery{
		leadSvc:        leadSvc,
		jobRepo:        jobRepo,
		campaignRepo:   campaignRepo,
		activityRepo:   activityRepo,
		drainBatchSize: drainBatchSize,
	}
}

// requeue pushes a failed payload back to the queue with incremented attempt.
// Returns false if max attempts exceeded (item is dropped).
func requeue(ctx context.Context, queue string, raw json.RawMessage, attempt int) bool {
	if attempt >= maxRetryAttempts {
		return false
	}
	env := retryEnvelope{Attempt: attempt + 1, Data: raw}
	b, err := json.Marshal(env)
	if err != nil {
		return false
	}
	if err := redis.Enqueue(ctx, queue, string(b), true); err != nil {
		log.Printf("ERROR [lead-recovery] - requeue to %s failed error=%s", queue, err)
		return false
	}
	return true
}

// unwrap extracts the raw data and attempt count from a payload.
// Handles both plain payloads (from scraper, attempt=0) and retry envelopes.
func unwrap(payload string) (json.RawMessage, int) {
	var env retryEnvelope
	if err := json.Unmarshal([]byte(payload), &env); err == nil && len(env.Data) > 0 {
		return env.Data, env.Attempt
	}
	// Plain payload from scraper — first attempt
	return json.RawMessage(payload), 0
}

// Run drains lead_batches and job_status queues each tick.
func (lr *LeadRecovery) Run(ctx context.Context) {
	lr.drainLeads(ctx)
	lr.drainJobStatus(ctx)
}

// drainLeads processes up to drainBatchSize lead batches per tick.
func (lr *LeadRecovery) drainLeads(ctx context.Context) {
	for range lr.drainBatchSize {
		payload, ok, err := redis.Dequeue(ctx, "lead_batches")
		if err != nil {
			log.Printf("ERROR [lead-recovery] - dequeue lead_batches failed error=%s", err)
			return
		}
		if !ok {
			return // queue empty
		}

		raw, attempt := unwrap(payload)

		var batch models.LeadBatchRequest
		if err := json.Unmarshal(raw, &batch); err != nil {
			log.Printf("ERROR [lead-recovery] - unmarshal lead batch failed, dropping error=%s", err)
			continue
		}

		if len(batch.Leads) == 0 {
			continue
		}

		result := lr.leadSvc.ProcessBatch(ctx, batch.JobID, batch.Leads)
		if result.Inserted > 0 {
			if err := lr.jobRepo.IncrementLeadsFound(ctx, batch.JobID, result.Inserted); err != nil {
				log.Printf("ERROR [lead-recovery] - increment job leads failed job_id=%s inserted=%d error=%s", batch.JobID, result.Inserted, err)
			}
		}

		// If all leads were skipped due to errors, requeue
		if result.Inserted == 0 && result.Merged == 0 && result.Skipped == len(batch.Leads) {
			if requeue(ctx, "lead_batches", raw, attempt) {
				log.Printf("WARN  [lead-recovery] - batch requeued job_id=%s attempt=%d/%d", batch.JobID, attempt+1, maxRetryAttempts)
			} else {
				log.Printf("ERROR [lead-recovery] - batch dropped after %d attempts job_id=%s leads=%d", maxRetryAttempts, batch.JobID, len(batch.Leads))
			}
			continue
		}

		log.Printf("INFO  [lead-recovery] - processed batch job_id=%s inserted=%d merged=%d skipped=%d",
			batch.JobID, result.Inserted, result.Merged, result.Skipped)
	}
}

// drainJobStatus processes up to drainBatchSize job status updates per tick.
func (lr *LeadRecovery) drainJobStatus(ctx context.Context) {
	for range lr.drainBatchSize {
		payload, ok, err := redis.Dequeue(ctx, "job_status")
		if err != nil {
			log.Printf("ERROR [lead-recovery] - dequeue job_status failed error=%s", err)
			return
		}
		if !ok {
			return
		}

		raw, attempt := unwrap(payload)

		var req struct {
			JobID  string `json:"job_id"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			log.Printf("ERROR [lead-recovery] - unmarshal job status failed, dropping error=%s", err)
			continue
		}

		if err := lr.jobRepo.UpdateStatus(ctx, req.JobID, req.Status, req.Error); err != nil {
			log.Printf("ERROR [lead-recovery] - update job status failed job_id=%s error=%s", req.JobID, err)
			if requeue(ctx, "job_status", raw, attempt) {
				log.Printf("WARN  [lead-recovery] - job status requeued job_id=%s attempt=%d/%d", req.JobID, attempt+1, maxRetryAttempts)
			} else {
				log.Printf("ERROR [lead-recovery] - job status dropped after %d attempts job_id=%s", maxRetryAttempts, req.JobID)
			}
			continue
		}

		// Terminal job statuses should advance campaign progress.
		if req.Status == "completed" || req.Status == "timeout" || req.Status == "failed" {
			job, err := lr.jobRepo.GetByID(ctx, req.JobID)
			if err == nil && job != nil {
				if err := lr.campaignRepo.SyncProgressFromJobs(ctx, job.CampaignID); err != nil {
					log.Printf("ERROR [lead-recovery] - sync campaign counters failed error=%s", err)
				}
				// Mark campaign as failed first (if any job failed), otherwise completed.
				if err := lr.campaignRepo.MarkFailedIfDone(ctx, job.CampaignID); err != nil {
					log.Printf("ERROR [lead-recovery] - mark campaign failed failed error=%s", err)
				}
				if err := lr.campaignRepo.MarkCompletedIfDone(ctx, job.CampaignID); err != nil {
					log.Printf("ERROR [lead-recovery] - mark campaign completed failed error=%s", err)
				} else {
					campaign, cErr := lr.campaignRepo.GetByID(ctx, job.CampaignID)
					if cErr == nil && campaign != nil && (campaign.Status == "completed" || campaign.Status == "failed") {
						// If campaign completed with 0 leads, decrement the daily limit counter
						if campaign.LeadsFound == 0 {
							key := fmt.Sprintf("campaign_limit:%s", time.Now().UTC().Format("2006-01-02"))
							redis.GetRawClient().Decr(ctx, key)
							log.Printf("INFO  [lead-recovery] - zero-lead campaign, daily limit decremented campaign_id=%s", job.CampaignID)
						}
						// Auto-populate lead_activities if campaign was pre-assigned to an employee
						if campaign.AssignedTo != nil && *campaign.AssignedTo != "" {
							if err := lr.activityRepo.PopulateForCampaign(ctx, *campaign.AssignedTo, campaign.ID); err != nil {
								log.Printf("ERROR [lead-recovery] - auto-populate activities failed campaign_id=%s error=%s", campaign.ID, err)
							} else {
								log.Printf("INFO  [lead-recovery] - auto-populated activities for pre-assigned campaign campaign_id=%s employee=%s", campaign.ID, *campaign.AssignedTo)
							}
						}
					}
				}
			}
		}

		log.Printf("INFO  [lead-recovery] - job status updated job_id=%s status=%s", req.JobID, req.Status)
	}
}
