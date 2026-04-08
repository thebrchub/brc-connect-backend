package handler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/shivanand-burli/go-starter-kit/helper"

	"sales-scrapper-backend/api/models"
	"sales-scrapper-backend/api/repository"
	"sales-scrapper-backend/api/service"
)

type InternalHandler struct {
	leadSvc      *service.LeadService
	jobRepo      *repository.JobRepo
	campaignRepo *repository.CampaignRepo
}

func NewInternalHandler(leadSvc *service.LeadService, jobRepo *repository.JobRepo, campaignRepo *repository.CampaignRepo) *InternalHandler {
	return &InternalHandler{
		leadSvc:      leadSvc,
		jobRepo:      jobRepo,
		campaignRepo: campaignRepo,
	}
}

// LeadBatch handles POST /internal/leads/batch.
func (h *InternalHandler) LeadBatch(w http.ResponseWriter, r *http.Request) {
	var req models.LeadBatchRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.JobID == "" {
		helper.Error(w, http.StatusBadRequest, "job_id is required")
		return
	}
	if len(req.Leads) == 0 {
		helper.Error(w, http.StatusBadRequest, "leads array is empty")
		return
	}
	const maxBatchSize = 500
	if len(req.Leads) > maxBatchSize {
		helper.Error(w, http.StatusBadRequest, "leads batch cannot exceed 500")
		return
	}

	job, err := h.jobRepo.GetByID(r.Context(), req.JobID)
	if err != nil {
		log.Printf("ERROR [internal] - verify job failed job_id=%s error=%s", req.JobID, err)
		helper.Error(w, http.StatusInternalServerError, fmt.Sprintf("verify job: %s", err))
		return
	}
	if job == nil {
		helper.Error(w, http.StatusNotFound, "job not found")
		return
	}

	result := h.leadSvc.ProcessBatch(r.Context(), req.JobID, req.Leads)
	helper.JSON(w, http.StatusOK, result)
}

// JobStatus handles POST /internal/jobs/{id}/status.
func (h *InternalHandler) JobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		helper.Error(w, http.StatusBadRequest, "missing job id")
		return
	}

	var req models.JobStatusRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	err := h.jobRepo.UpdateStatus(r.Context(), id, req.Status, req.LeadsFound, req.Error)
	if err != nil {
		log.Printf("ERROR [internal] - update job status failed job_id=%s status=%s error=%s", id, req.Status, err)
		helper.Error(w, http.StatusInternalServerError, fmt.Sprintf("update status: %s", err))
		return
	}

	// If job completed, atomically increment campaign counters
	if req.Status == "completed" {
		job, err := h.jobRepo.GetByID(r.Context(), id)
		if err == nil && job != nil {
			if err := h.campaignRepo.IncrementOnJobComplete(r.Context(), job.CampaignID, req.LeadsFound); err != nil {
				log.Printf("ERROR [internal-handler] - increment campaign counters failed error=%s", err)
			}
		}
	}

	helper.NoContent(w)
}
