package handler

import (
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"
	"github.com/shivanand-burli/go-starter-kit/storage"

	"brc-connect-backend/api/service"
)

type UserHandler struct {
	userSvc *service.UserService
	store   storage.StorageService
}

func NewUserHandler(userSvc *service.UserService, store storage.StorageService) *UserHandler {
	return &UserHandler{userSvc: userSvc, store: store}
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

// GetProfile handles GET /profile — returns the authenticated user's profile.
func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.Subject(r.Context())
	user, err := h.userSvc.GetByID(r.Context(), userID)
	if err != nil || user == nil {
		helper.Error(w, http.StatusNotFound, "user not found")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]any{
		"id":              user.ID,
		"name":            user.Name,
		"email":           user.Email,
		"role":            user.Role,
		"avatar_url":      user.AvatarURL,
		"presence_hidden": user.PresenceHidden,
		"created_at":      user.CreatedAt,
	})
}

// UpdateProfile handles PATCH /profile — update own name.
func (h *UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.Subject(r.Context())
	var req struct {
		Name           string `json:"name"`
		PresenceHidden *bool  `json:"presence_hidden"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	updates := map[string]any{}
	if strings.TrimSpace(req.Name) != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.PresenceHidden != nil {
		updates["presence_hidden"] = *req.PresenceHidden
	}
	if len(updates) == 0 {
		helper.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if err := h.userSvc.UpdateUser(r.Context(), userID, updates); err != nil {
		log.Printf("ERROR [user] - update profile failed id=%s error=%s", userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UploadAvatar handles POST /profile/avatar — presigned upload for avatar image.
func (h *UserHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		helper.Error(w, http.StatusServiceUnavailable, "file uploads not configured")
		return
	}
	userID := middleware.Subject(r.Context())
	var req struct {
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.FileName == "" || req.ContentType == "" {
		helper.Error(w, http.StatusBadRequest, "file_name and content_type required")
		return
	}
	if !strings.HasPrefix(req.ContentType, "image/") {
		helper.Error(w, http.StatusBadRequest, "only image files allowed")
		return
	}
	ext := filepath.Ext(req.FileName)
	if ext == "" {
		ext = ".png"
	}
	key := "avatars/" + userID + "/" + uuid.NewString() + ext
	uploadURL, err := h.store.PresignPut(r.Context(), &storage.PresignPutInput{
		Key:         key,
		ContentType: req.ContentType,
		Expiry:      5 * 60 * 1e9, // 5 minutes
	})
	if err != nil {
		log.Printf("ERROR [profile] - presign put failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to generate upload URL")
		return
	}
	// Save avatar_url key to user record
	if err := h.userSvc.UpdateUser(r.Context(), userID, map[string]any{"avatar_url": key}); err != nil {
		log.Printf("ERROR [profile] - update avatar failed id=%s error=%s", userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to update avatar")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]any{
		"upload_url": uploadURL,
		"key":        key,
	})
}

// AvatarURL handles GET /profile/avatar?key=avatars/... — returns presigned GET URL.
func (h *UserHandler) AvatarURL(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		helper.Error(w, http.StatusServiceUnavailable, "file uploads not configured")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" || !strings.HasPrefix(key, "avatars/") {
		helper.Error(w, http.StatusBadRequest, "invalid key")
		return
	}
	presigned, err := h.store.PresignGet(r.Context(), &storage.PresignGetInput{
		Key:    key,
		Expiry: 30 * 60 * 1e9, // 30 minutes
	})
	if err != nil {
		helper.Error(w, http.StatusInternalServerError, "failed to generate download URL")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"url": presigned})
}
