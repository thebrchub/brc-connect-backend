package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
)

type LeadHandler struct {
	leadRepo *repository.LeadRepo
}

func NewLeadHandler(leadRepo *repository.LeadRepo) *LeadHandler {
	return &LeadHandler{leadRepo: leadRepo}
}

// GetLeads handles GET /leads with filtering and pagination — scoped by role.
func (h *LeadHandler) GetLeads(w http.ResponseWriter, r *http.Request) {
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

	city := q.Get("city")
	status := q.Get("status")
	source := q.Get("source")
	scoreGTE, _ := strconv.Atoi(q.Get("score_gte"))
	hasPhone := q.Get("has_phone") == "true"

	var leads []models.Lead
	var total int
	var err error

	switch role {
	case "super_admin":
		leads, total, err = h.leadRepo.GetFiltered(r.Context(), city, status, source, scoreGTE, hasPhone, page, pageSize)
	case "admin":
		leads, total, err = h.leadRepo.GetFilteredByAdmin(r.Context(), userID, city, status, source, scoreGTE, hasPhone, page, pageSize)
	default:
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	if err != nil {
		log.Printf("ERROR [lead] - get filtered failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch leads")
		return
	}

	helper.Paginated(w, leads, page, pageSize, total)
}

// GetLead handles GET /leads/{id}.
func (h *LeadHandler) GetLead(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		helper.Error(w, http.StatusBadRequest, "missing lead id")
		return
	}

	lead, err := h.leadRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Printf("ERROR [lead] - get by id failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch lead")
		return
	}
	if lead == nil {
		helper.Error(w, http.StatusNotFound, "lead not found")
		return
	}

	// Admin can only see their own leads
	role := middleware.Role(r.Context())
	userID := middleware.Subject(r.Context())
	if role == "admin" && (lead.AdminID == nil || *lead.AdminID != userID) {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	helper.JSON(w, http.StatusOK, lead)
}

// UpdateLead handles PATCH /leads/{id}.
func (h *LeadHandler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		helper.Error(w, http.StatusBadRequest, "missing lead id")
		return
	}

	existing, err := h.leadRepo.GetByID(r.Context(), id)
	if err != nil {
		log.Printf("ERROR [lead] - get by id for update failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch lead")
		return
	}
	if existing == nil {
		helper.Error(w, http.StatusNotFound, "lead not found")
		return
	}

	// Admin can only update their own leads
	role := middleware.Role(r.Context())
	userID := middleware.Subject(r.Context())
	if role == "admin" && (existing.AdminID == nil || *existing.AdminID != userID) {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	var updates map[string]any
	if err := helper.ReadJSON(r, &updates); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Only allow updating the status field
	validStatuses := map[string]bool{"new": true, "contacted": true, "qualified": true, "converted": true, "closed": true}
	if status, ok := updates["status"].(string); ok {
		if !validStatuses[status] {
			helper.Error(w, http.StatusBadRequest, "status must be one of: new, contacted, qualified, converted, closed")
			return
		}
		existing.Status = status
	} else {
		helper.Error(w, http.StatusBadRequest, "missing or invalid status")
		return
	}

	if err := h.leadRepo.Update(r.Context(), *existing); err != nil {
		log.Printf("ERROR [lead] - update failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update lead")
		return
	}

	helper.JSON(w, http.StatusOK, existing)
}
