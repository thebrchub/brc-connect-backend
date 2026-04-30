package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/service"
)

type CampaignHandler struct {
	campaignSvc *service.CampaignService
}

func NewCampaignHandler(campaignSvc *service.CampaignService) *CampaignHandler {
	return &CampaignHandler{campaignSvc: campaignSvc}
}

// CreateCampaign handles POST /campaigns.
func (h *CampaignHandler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())

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

	c.AdminID = &adminID

	campaign, err := h.campaignSvc.Create(r.Context(), c)
	if err != nil {
		if err == service.ErrDailyLimitReached {
			helper.Error(w, http.StatusTooManyRequests, "daily campaign limit reached (5 per day)")
			return
		}
		log.Printf("ERROR [campaign] - create failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, fmt.Sprintf("create: %s", err))
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

// GetCampaigns handles GET /campaigns — role-scoped paginated list.
func (h *CampaignHandler) GetCampaigns(w http.ResponseWriter, r *http.Request) {
	role := middleware.Role(r.Context())
	userID := middleware.Subject(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var campaigns []models.Campaign
	var total int
	var err error

	switch role {
	case "super_admin":
		campaigns, total, err = h.campaignSvc.GetAll(r.Context(), page, pageSize)
	case "admin":
		campaigns, total, err = h.campaignSvc.GetByAdmin(r.Context(), userID, page, pageSize)
	case "employee":
		campaigns, total, err = h.campaignSvc.GetByEmployee(r.Context(), userID, page, pageSize)
	default:
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	if err != nil {
		log.Printf("ERROR [campaign] - list failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch campaigns")
		return
	}

	helper.Paginated(w, campaigns, page, pageSize, total)
}
