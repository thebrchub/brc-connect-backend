package handler

import (
	"crypto/subtle"
	"log"
	"net/http"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/jwt"
)

type AuthHandler struct {
	users map[string]authUser // username → user
}

type authUser struct {
	Password string
	Role     string
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func NewAuthHandler(serviceUser, servicePass, adminUser, adminPass string) *AuthHandler {
	users := map[string]authUser{}
	if serviceUser != "" {
		users[serviceUser] = authUser{Password: servicePass, Role: "service"}
	}
	if adminUser != "" {
		users[adminUser] = authUser{Password: adminPass, Role: "admin"}
	}
	return &AuthHandler{users: users}
}

// Login handles POST /auth/login — validates credentials, returns JWT tokens.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Username == "" || req.Password == "" {
		helper.Error(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, exists := h.users[req.Username]
	if !exists || subtle.ConstantTimeCompare([]byte(user.Password), []byte(req.Password)) != 1 {
		helper.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	access, refresh, err := jwt.GenerateToken(req.Username, map[string]any{
		"role": user.Role,
	})
	if err != nil {
		log.Printf("ERROR [auth] - generate token failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{
		"access_token":  access,
		"refresh_token": refresh,
	})
}
