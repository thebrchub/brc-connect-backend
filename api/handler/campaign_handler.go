package handler

import (
	"log"
	"net/http"

	"github.com/shivanand-burli/go-starter-kit/helper"

	"sales-scrapper-backend/api/models"
	"sales-scrapper-backend/api/service"
)

type CampaignHandler struct {
	campaignSvc *service.CampaignService
}

func NewCampaignHandler(campaignSvc *service.CampaignService) *CampaignHandler {
	return &CampaignHandler{campaignSvc: campaignSvc}
}

// CreateCampaign handles POST /campaigns.
func (h *CampaignHandler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	var c models.Campaign
	if err := helper.ReadJSON(r, &c); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if c.Name == "" {
		helper.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(c.Sources) == 0 {
		helper.Error(w, http.StatusBadRequest, "at least one source is required")
		return
	}
	if len(c.Cities) == 0 {
		helper.Error(w, http.StatusBadRequest, "at least one city is required")
		return
	}
	if len(c.Categories) == 0 {
		helper.Error(w, http.StatusBadRequest, "at least one category is required")
		return
	}
	const maxArrayLen = 50
	if len(c.Sources) > maxArrayLen || len(c.Cities) > maxArrayLen || len(c.Categories) > maxArrayLen {
		helper.Error(w, http.StatusBadRequest, "sources, cities, and categories cannot exceed 50 each")
		return
	}
	const maxJobs = 10000
	if len(c.Sources)*len(c.Cities)*len(c.Categories) > maxJobs {
		helper.Error(w, http.StatusBadRequest, "total job combinations (sources × cities × categories) cannot exceed 10000")
		return
	}

	campaign, err := h.campaignSvc.Create(r.Context(), c)
	if err != nil {
		log.Printf("ERROR [campaign] - create failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to create campaign")
		return
	}

	helper.Created(w, campaign)
}

// GetCampaignStatus handles GET /campaigns/{id}/status.
func (h *CampaignHandler) GetCampaignStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		helper.Error(w, http.StatusBadRequest, "missing campaign id")
		return
	}

	campaign, err := h.campaignSvc.GetStatus(r.Context(), id)
	if err != nil {
		log.Printf("ERROR [campaign] - get status failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch campaign")
		return
	}
	if campaign == nil {
		helper.Error(w, http.StatusNotFound, "campaign not found")
		return
	}

	helper.JSON(w, http.StatusOK, campaign)
}
