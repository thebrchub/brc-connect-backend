package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"

	"brc-connect-backend/api/service"
)

type UserHandler struct {
	userSvc *service.UserService
}

func NewUserHandler(userSvc *service.UserService) *UserHandler {
	return &UserHandler{userSvc: userSvc}
}

// --- Super Admin: Manage Admins ---

type createAdminRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// CreateAdmin handles POST /users/admins.
func (h *UserHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" || req.Email == "" || req.Password == "" {
		helper.Error(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}

	user, err := h.userSvc.CreateAdmin(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		if err.Error() == "email already in use" {
			helper.Error(w, http.StatusConflict, err.Error())
			return
		}
		log.Printf("ERROR [user] - create admin failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to create admin")
		return
	}

	helper.Created(w, user)
}

// GetAdmins handles GET /users/admins.
func (h *UserHandler) GetAdmins(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	users, total, err := h.userSvc.GetAdmins(r.Context(), page, pageSize)
	if err != nil {
		log.Printf("ERROR [user] - get admins failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch admins")
		return
	}

	helper.Paginated(w, users, page, pageSize, total)
}

// GetAdmin handles GET /users/admins/{id}.
func (h *UserHandler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, err := h.userSvc.GetByID(r.Context(), id)
	if err != nil {
		log.Printf("ERROR [user] - get admin failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch admin")
		return
	}
	if user == nil || user.Role != "admin" {
		helper.Error(w, http.StatusNotFound, "admin not found")
		return
	}
	helper.JSON(w, http.StatusOK, user)
}

// UpdateAdmin handles PATCH /users/admins/{id}.
func (h *UserHandler) UpdateAdmin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var updates map[string]any
	if err := helper.ReadJSON(r, &updates); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Only allow safe fields
	allowed := map[string]bool{"name": true, "email": true, "password": true, "is_active": true}
	for k := range updates {
		if !allowed[k] {
			delete(updates, k)
		}
	}

	if err := h.userSvc.UpdateUser(r.Context(), id, updates); err != nil {
		log.Printf("ERROR [user] - update admin failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update admin")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteAdmin handles DELETE /users/admins/{id} — deactivates admin + cascade employees.
func (h *UserHandler) DeleteAdmin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.userSvc.DeactivateAdmin(r.Context(), id); err != nil {
		log.Printf("ERROR [user] - deactivate admin failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to deactivate admin")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// --- Admin: Manage Employees ---

type createEmployeeRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// CreateEmployee handles POST /users/employees.
func (h *UserHandler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())

	var req createEmployeeRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" || req.Email == "" || req.Password == "" {
		helper.Error(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}

	user, err := h.userSvc.CreateEmployee(r.Context(), adminID, req.Name, req.Email, req.Password)
	if err != nil {
		if err.Error() == "email already in use" {
			helper.Error(w, http.StatusConflict, err.Error())
			return
		}
		log.Printf("ERROR [user] - create employee failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to create employee")
		return
	}

	helper.Created(w, user)
}

// GetEmployees handles GET /users/employees — admin's own employees only.
func (h *UserHandler) GetEmployees(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	users, total, err := h.userSvc.GetEmployeesByAdmin(r.Context(), adminID, page, pageSize)
	if err != nil {
		log.Printf("ERROR [user] - get employees failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch employees")
		return
	}

	helper.Paginated(w, users, page, pageSize, total)
}

// GetEmployee handles GET /users/employees/{id} — admin's own employee only.
func (h *UserHandler) GetEmployee(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	id := r.PathValue("id")

	user, err := h.userSvc.GetByID(r.Context(), id)
	if err != nil {
		log.Printf("ERROR [user] - get employee failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch employee")
		return
	}
	if user == nil || user.Role != "employee" || user.AdminID == nil || *user.AdminID != adminID {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	helper.JSON(w, http.StatusOK, user)
}

// UpdateEmployee handles PATCH /users/employees/{id} — admin's own employee only.
func (h *UserHandler) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.Subject(r.Context())
	id := r.PathValue("id")

	// Verify ownership
	user, err := h.userSvc.GetByID(r.Context(), id)
	if err != nil || user == nil || user.Role != "employee" || user.AdminID == nil || *user.AdminID != adminID {
		helper.Error(w, http.StatusForbidden, "access denied")
		return
	}

	var updates map[string]any
	if err := helper.ReadJSON(r, &updates); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	allowed := map[string]bool{"name": true, "email": true, "password": true, "is_active": true}
	for k := range updates {
		if !allowed[k] {
			delete(updates, k)
		}
	}

	if err := h.userSvc.UpdateUser(r.Context(), id, updates); err != nil {
		log.Printf("ERROR [user] - update employee failed id=%s error=%s", id, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update employee")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
