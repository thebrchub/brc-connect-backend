package handler

import (
	"log"
	"net/http"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/jwt"

	"brc-connect-backend/api/service"
)

type AuthHandler struct {
	userSvc *service.UserService
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func NewAuthHandler(userSvc *service.UserService) *AuthHandler {
	return &AuthHandler{userSvc: userSvc}
}

// Login handles POST /auth/login — validates credentials, returns JWT tokens.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Email == "" || req.Password == "" {
		helper.Error(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.userSvc.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		helper.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	claims := map[string]any{
		"role": user.Role,
	}
	if user.Role == "employee" && user.AdminID != nil {
		claims["admin_id"] = *user.AdminID
	}

	access, refresh, err := jwt.GenerateToken(user.ID, claims)
	if err != nil {
		log.Printf("ERROR [auth] - generate token failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"user": map[string]any{
			"id":   user.ID,
			"name": user.Name,
			"role": user.Role,
		},
	})
}
