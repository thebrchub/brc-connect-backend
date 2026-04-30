package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"

	"brc-connect-backend/api/service"
)

type CRMHandler struct {
	activitySvc *service.ActivityService
	sessionSvc  *service.SessionService
	userSvc     *service.UserService
}

func NewCRMHandler(activitySvc *service.ActivityService, sessionSvc *service.SessionService, userSvc *service.UserService) *CRMHandler {
	return &CRMHandler{activitySvc: activitySvc, sessionSvc: sessionSvc, userSvc: userSvc}
}

// --- Employee CRM Endpoints ---

// GetCRMLeads handles GET /crm/leads — max 20 fresh leads for employee.
func (h *CRMHandler) GetCRMLeads(w http.ResponseWriter, r *http.Request) {
	employeeID := middleware.Subject(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	leads, total, err := h.activitySvc.GetFreshLeads(r.Context(), employeeID, page)
	if err != nil {
		log.Printf("ERROR [crm] - get fresh leads failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch leads")
		return
	}

	helper.Paginated(w, leads, page, 20, total)
}

// GetCRMHistory handles GET /crm/leads/history — past contacted leads.
func (h *CRMHandler) GetCRMHistory(w http.ResponseWriter, r *http.Request) {
	employeeID := middleware.Subject(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	leads, total, err := h.activitySvc.GetHistory(r.Context(), employeeID, page, pageSize)
	if err != nil {
		log.Printf("ERROR [crm] - get history failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch history")
		return
	}

	helper.Paginated(w, leads, page, pageSize, total)
}

// UpdateCRMLead handles PATCH /crm/leads/{id} — update activity status/notes.
func (h *CRMHandler) UpdateCRMLead(w http.ResponseWriter, r *http.Request) {
	employeeID := middleware.Subject(r.Context())
	activityID := r.PathValue("id")

	var updates map[string]any
	if err := helper.ReadJSON(r, &updates); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := h.activitySvc.UpdateActivity(r.Context(), activityID, employeeID, updates); err != nil {
		if err.Error() == "access denied" {
			helper.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if err.Error() == "activity not found" {
			helper.Error(w, http.StatusNotFound, "activity not found")
			return
		}
		if err.Error() == "invalid status" {
			helper.Error(w, http.StatusBadRequest, "status must be one of: pending, contacted, follow_up, converted, not_interested, closed")
			return
		}
		log.Printf("ERROR [crm] - update activity failed id=%s error=%s", activityID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update activity")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Heartbeat handles POST /crm/heartbeat — employee session tracking.
func (h *CRMHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	employeeID := middleware.Subject(r.Context())

	var req struct {
		SessionID *string `json:"session_id"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	ip := r.RemoteAddr
	sessionID, err := h.sessionSvc.Heartbeat(r.Context(), employeeID, req.SessionID, ip)
	if err != nil {
		log.Printf("ERROR [crm] - heartbeat failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"session_id": sessionID})
}

// --- Admin CRM Endpoints ---

// GetDashboard handles GET /crm/dashboard — admin's employee stats.
func (h *CRMHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())

	stats, err := h.activitySvc.GetDashboard(r.Context(), adminID)
	if err != nil {
		log.Printf("ERROR [crm] - get dashboard failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch dashboard")
		return
	}

	helper.JSON(w, http.StatusOK, stats)
}

// GetEmployeeActivity handles GET /crm/employees/{id}/activity — admin views employee's activity.
func (h *CRMHandler) GetEmployeeActivity(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	employeeID := r.PathValue("id")

	// Verify ownership
	employee, err := h.userSvc.GetByID(r.Context(), employeeID)
	if err != nil || employee == nil || employee.AdminID == nil || *employee.AdminID != adminID {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	leads, total, err := h.activitySvc.GetEmployeeActivity(r.Context(), employeeID, page, pageSize)
	if err != nil {
		log.Printf("ERROR [crm] - get employee activity failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch activity")
		return
	}

	helper.Paginated(w, leads, page, pageSize, total)
}

// GetEmployeeStats handles GET /crm/employees/{id}/stats — admin views employee performance.
func (h *CRMHandler) GetEmployeeStats(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	employeeID := r.PathValue("id")

	// Verify ownership
	employee, err := h.userSvc.GetByID(r.Context(), employeeID)
	if err != nil || employee == nil || employee.AdminID == nil || *employee.AdminID != adminID {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	stats, err := h.activitySvc.GetEmployeeStats(r.Context(), employeeID)
	if err != nil {
		log.Printf("ERROR [crm] - get employee stats failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch stats")
		return
	}
	if stats == nil {
		helper.Error(w, http.StatusNotFound, "no stats found")
		return
	}

	helper.JSON(w, http.StatusOK, stats)
}

// GetEmployeeEngagement handles GET /crm/employees/{id}/engagement — admin views engagement.
func (h *CRMHandler) GetEmployeeEngagement(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	employeeID := r.PathValue("id")

	// Verify ownership
	employee, err := h.userSvc.GetByID(r.Context(), employeeID)
	if err != nil || employee == nil || employee.AdminID == nil || *employee.AdminID != adminID {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	engagement, err := h.sessionSvc.GetEngagement(r.Context(), employeeID)
	if err != nil {
		log.Printf("ERROR [crm] - get engagement failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch engagement")
		return
	}

	helper.JSON(w, http.StatusOK, engagement)
}

// GetDashboardEngagement handles GET /crm/dashboard/engagement — engagement overview.
func (h *CRMHandler) GetDashboardEngagement(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())

	engagements, err := h.sessionSvc.GetDashboardEngagement(r.Context(), adminID)
	if err != nil {
		log.Printf("ERROR [crm] - get dashboard engagement failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch engagement")
		return
	}

	helper.JSON(w, http.StatusOK, engagements)
}
