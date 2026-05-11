package router

import (
	"net/http"
	"time"

	"github.com/shivanand-burli/go-starter-kit/middleware"
	"github.com/shivanand-burli/go-starter-kit/rtc"

	"brc-connect-backend/api/config"
	"brc-connect-backend/api/handler"
)

func New(cfg config.Config, authH *handler.AuthHandler, leadH *handler.LeadHandler, campaignH *handler.CampaignHandler, exportH *handler.ExportHandler, progressH *handler.ProgressHandler, userH *handler.UserHandler, crmH *handler.CRMHandler, chatH *handler.ChatHandler, gcH *handler.GroupCallHandler, rtcClient *rtc.Client) (http.Handler, *middleware.IPRateLimiter) {
	mux := http.NewServeMux()
	auth := middleware.Auth("")
	superAdmin := middleware.RequireRole(middleware.RoleSuperAdmin)
	admin := middleware.RequireRole(middleware.RoleAdmin)
	employee := middleware.RequireRole(middleware.RoleEmployee)

	// Public — no auth
	mux.HandleFunc("POST /auth/login", authH.Login)
	mux.HandleFunc("POST /auth/refresh", middleware.HandleRefresh(""))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Protected — any authenticated user
	mux.HandleFunc("GET /leads", middleware.Chain(leadH.GetLeads, auth))
	mux.HandleFunc("GET /leads/{id}", middleware.Chain(leadH.GetLead, auth))
	mux.HandleFunc("PATCH /leads/{id}", middleware.Chain(leadH.UpdateLead, auth))
	mux.HandleFunc("POST /leads/assign", middleware.Chain(leadH.BulkAssignLeads, auth, admin))
	mux.HandleFunc("GET /leads/export", middleware.Chain(exportH.ExportCSV, auth))
	mux.HandleFunc("POST /campaigns", middleware.Chain(campaignH.CreateCampaign, auth, admin))
	mux.HandleFunc("GET /campaigns", middleware.Chain(campaignH.GetCampaigns, auth))
	mux.HandleFunc("GET /campaigns/{id}/status", middleware.Chain(campaignH.GetCampaignStatus, auth))
	mux.HandleFunc("PATCH /campaigns/{id}/assign", middleware.Chain(campaignH.AssignCampaign, auth, admin))
	mux.HandleFunc("DELETE /campaigns/{id}", middleware.Chain(campaignH.DeleteCampaign, auth, admin))
	mux.HandleFunc("GET /campaigns/{id}/progress", middleware.Chain(progressH.StreamProgress, auth))

	// Super admin — manage admins
	mux.HandleFunc("POST /users/admins", middleware.Chain(userH.CreateAdmin, auth, superAdmin))
	mux.HandleFunc("GET /users/admins", middleware.Chain(userH.GetAdmins, auth, superAdmin))
	mux.HandleFunc("GET /users/admins/{id}", middleware.Chain(userH.GetAdmin, auth, superAdmin))
	mux.HandleFunc("PATCH /users/admins/{id}", middleware.Chain(userH.UpdateAdmin, auth, superAdmin))
	mux.HandleFunc("DELETE /users/admins/{id}", middleware.Chain(userH.DeleteAdmin, auth, superAdmin))

	// Admin — manage employees
	mux.HandleFunc("POST /users/employees", middleware.Chain(userH.CreateEmployee, auth, admin))
	mux.HandleFunc("GET /users/employees", middleware.Chain(userH.GetEmployees, auth, admin))
	mux.HandleFunc("GET /users/employees/{id}", middleware.Chain(userH.GetEmployee, auth, admin))
	mux.HandleFunc("PATCH /users/employees/{id}", middleware.Chain(userH.UpdateEmployee, auth, admin))

	// Employee CRM
	mux.HandleFunc("GET /crm/leads", middleware.Chain(crmH.GetCRMLeads, auth, employee))
	mux.HandleFunc("GET /crm/leads/history", middleware.Chain(crmH.GetCRMHistory, auth, employee))
	mux.HandleFunc("PATCH /crm/leads/{id}", middleware.Chain(crmH.UpdateCRMLead, auth, employee))
	mux.HandleFunc("GET /crm/stats", middleware.Chain(crmH.GetMyStats, auth, employee))
	mux.HandleFunc("POST /crm/heartbeat", middleware.Chain(crmH.Heartbeat, auth, employee))

	// Admin CRM dashboard
	mux.HandleFunc("GET /crm/dashboard", middleware.Chain(crmH.GetDashboard, auth, admin))
	mux.HandleFunc("GET /crm/employees/{id}/activity", middleware.Chain(crmH.GetEmployeeActivity, auth, admin))
	mux.HandleFunc("GET /crm/employees/{id}/stats", middleware.Chain(crmH.GetEmployeeStats, auth, admin))
	mux.HandleFunc("GET /crm/employees/{id}/engagement", middleware.Chain(crmH.GetEmployeeEngagement, auth, admin))
	mux.HandleFunc("GET /crm/dashboard/engagement", middleware.Chain(crmH.GetDashboardEngagement, auth, admin))

	// Chat — WebSocket + REST
	mux.HandleFunc("GET /ws", middleware.Chain(chatH.HandleWS, auth))

	// Profile — any authenticated user
	mux.HandleFunc("GET /profile", middleware.Chain(userH.GetProfile, auth))
	mux.HandleFunc("PATCH /profile", middleware.Chain(userH.UpdateProfile, auth))
	mux.HandleFunc("POST /profile/avatar", middleware.Chain(userH.UploadAvatar, auth))
	mux.HandleFunc("GET /profile/avatar", middleware.Chain(userH.AvatarURL, auth))

	mux.HandleFunc("GET /chat/rooms", middleware.Chain(chatH.GetRooms, auth))
	mux.HandleFunc("POST /chat/rooms/dm", middleware.Chain(chatH.CreateDM, auth))
	mux.HandleFunc("GET /chat/rooms/{id}/messages", middleware.Chain(chatH.GetMessages, auth))
	mux.HandleFunc("PATCH /chat/messages/{id}", middleware.Chain(chatH.EditMessage, auth))
	mux.HandleFunc("DELETE /chat/messages/{id}", middleware.Chain(chatH.DeleteMessage, auth))
	mux.HandleFunc("GET /chat/calls", middleware.Chain(chatH.GetCallHistory, auth))
	mux.HandleFunc("GET /chat/calls/active", middleware.Chain(gcH.GetActiveCalls, auth))
	mux.HandleFunc("GET /chat/calls/config", middleware.Chain(rtc.HandleCallConfig(rtcClient), auth))
	mux.HandleFunc("POST /chat/upload", middleware.Chain(chatH.UploadFile, auth))
	mux.HandleFunc("GET /chat/file", middleware.Chain(chatH.FileURL, auth))
	mux.HandleFunc("GET /chat/messages/search", middleware.Chain(chatH.SearchMessages, auth))

	// Chat — Group management
	mux.HandleFunc("POST /chat/groups", middleware.Chain(chatH.CreateGroup, auth))
	mux.HandleFunc("PUT /chat/groups/{id}", middleware.Chain(chatH.UpdateGroup, auth))
	mux.HandleFunc("POST /chat/groups/{id}/avatar", middleware.Chain(chatH.UploadGroupAvatar, auth))
	mux.HandleFunc("GET /chat/groups/{id}/members", middleware.Chain(chatH.GetGroupMembers, auth))
	mux.HandleFunc("POST /chat/groups/{id}/members", middleware.Chain(chatH.AddGroupMembers, auth))
	mux.HandleFunc("DELETE /chat/groups/{id}/members/{userId}", middleware.Chain(chatH.RemoveGroupMember, auth))
	mux.HandleFunc("POST /chat/groups/{id}/members/{userId}/promote", middleware.Chain(chatH.PromoteMember, auth))
	mux.HandleFunc("POST /chat/groups/{id}/members/{userId}/demote", middleware.Chain(chatH.DemoteMember, auth))
	mux.HandleFunc("POST /chat/groups/{id}/leave", middleware.Chain(chatH.LeaveGroup, auth))

	// Chat — Group calls (LiveKit)
	mux.HandleFunc("POST /chat/groups/{id}/call/start", middleware.Chain(gcH.StartCall, auth))
	mux.HandleFunc("POST /chat/groups/{id}/call/join", middleware.Chain(gcH.JoinCall, auth))
	mux.HandleFunc("POST /chat/groups/{id}/call/leave", middleware.Chain(gcH.LeaveCall, auth))
	mux.HandleFunc("POST /chat/groups/{id}/call/end", middleware.Chain(gcH.EndCall, auth))
	mux.HandleFunc("POST /chat/groups/{id}/call/mute", middleware.Chain(gcH.MuteParticipant, auth))
	mux.HandleFunc("POST /chat/groups/{id}/call/kick", middleware.Chain(gcH.KickParticipant, auth))

	// Middleware stack
	cors := middleware.NewCORS(middleware.CORSConfig{
		Origin:  cfg.CORSOrigin,
		Headers: "Content-Type, Authorization",
	})

	limiter := middleware.NewIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)

	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		FailureThreshold: cfg.CBFailureThreshold,
		OpenDuration:     time.Duration(cfg.CBOpenDurationSec) * time.Second,
	})

	// Middleware stack (inside → outside): mux → compress → cors → rate limiter → circuit breaker
	var h http.Handler = mux
	h = middleware.Compress(h)
	h = cors(h)
	h = limiter.LimitHandler(h)
	h = cb.Wrap(h)

	return h, limiter
}
