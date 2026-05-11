package handler

import (
	"errors"
	"net/http"

	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"

	"brc-connect-backend/api/service"
)

// GroupCallHandler handles group call HTTP endpoints.
type GroupCallHandler struct {
	svc *service.GroupCallService
}

// NewGroupCallHandler creates a new group call handler.
func NewGroupCallHandler(svc *service.GroupCallService) *GroupCallHandler {
	return &GroupCallHandler{svc: svc}
}

// StartCall initiates a group call in a room.
// POST /chat/groups/{id}/call/start
func (h *GroupCallHandler) StartCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	userID := middleware.Subject(ctx)

	token, err := h.svc.StartCall(ctx, roomID, userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"token": token})
}

// JoinCall joins an active group call.
// POST /chat/groups/{id}/call/join
func (h *GroupCallHandler) JoinCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	userID := middleware.Subject(ctx)

	token, err := h.svc.JoinCall(ctx, roomID, userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"token": token})
}

// LeaveCall leaves an active group call.
// POST /chat/groups/{id}/call/leave
func (h *GroupCallHandler) LeaveCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	userID := middleware.Subject(ctx)

	if err := h.svc.LeaveCall(ctx, roomID, userID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EndCall forcefully ends a group call (admin only).
// POST /chat/groups/{id}/call/end
func (h *GroupCallHandler) EndCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	userID := middleware.Subject(ctx)

	if err := h.svc.EndCall(ctx, roomID, userID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MuteParticipant mutes a participant's track (admin only).
// POST /chat/groups/{id}/call/mute
func (h *GroupCallHandler) MuteParticipant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	adminID := middleware.Subject(ctx)

	var req struct {
		UserID   string `json:"user_id"`
		TrackSID string `json:"track_sid"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.UserID == "" || req.TrackSID == "" {
		helper.Error(w, http.StatusBadRequest, "user_id and track_sid are required")
		return
	}

	if err := h.svc.MuteParticipant(ctx, roomID, adminID, req.UserID, req.TrackSID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// KickParticipant removes a participant from the call (admin only).
// POST /chat/groups/{id}/call/kick
func (h *GroupCallHandler) KickParticipant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomID := r.PathValue("id")
	adminID := middleware.Subject(ctx)

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.UserID == "" {
		helper.Error(w, http.StatusBadRequest, "user_id is required")
		return
	}

	if err := h.svc.KickParticipant(ctx, roomID, adminID, req.UserID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetActiveCalls returns all active group calls across the user's rooms.
// GET /chat/calls/active
func (h *GroupCallHandler) GetActiveCalls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	adminID := getAdminID(ctx)

	calls, err := h.svc.GetActiveCalls(ctx, userID, adminID)
	if err != nil {
		helper.Error(w, http.StatusInternalServerError, "failed to fetch active calls")
		return
	}
	if calls == nil {
		calls = []service.ActiveCallInfo{}
	}
	helper.JSON(w, http.StatusOK, map[string]any{"active_calls": calls})
}

func (h *GroupCallHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrCallAlreadyActive):
		helper.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNoActiveCall):
		helper.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrLiveKitDisabled):
		helper.Error(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrNotMember):
		helper.Error(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNotGroupAdmin):
		helper.Error(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNotGroupRoom):
		helper.Error(w, http.StatusBadRequest, err.Error())
	default:
		helper.Error(w, http.StatusInternalServerError, "internal error")
	}
}
