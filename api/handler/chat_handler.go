package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/chat"
	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/middleware"
	"github.com/shivanand-burli/go-starter-kit/storage"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/service"
)

type ChatHandler struct {
	chatSvc *service.ChatService
	store   storage.StorageService
}

func NewChatHandler(chatSvc *service.ChatService, store storage.StorageService) *ChatHandler {
	return &ChatHandler{chatSvc: chatSvc, store: store}
}

// UploadFile generates a presigned POST URL for direct-to-S3 upload.
// POST /chat/upload {file_name, content_type, file_size}
func (h *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		helper.Error(w, http.StatusServiceUnavailable, "file uploads not configured")
		return
	}

	var req struct {
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
		FileSize    int64  `json:"file_size"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.FileName == "" || req.ContentType == "" || req.FileSize <= 0 {
		helper.Error(w, http.StatusBadRequest, "file_name, content_type, and file_size are required")
		return
	}

	userID := middleware.Subject(r.Context())
	ext := strings.ToLower(filepath.Ext(req.FileName))
	if ext == "" {
		ext = storage.MIMEToExt(req.ContentType)
	}
	key := fmt.Sprintf("chat/%s/%s%s", userID, uuid.New().String(), ext)

	uploadURL, err := h.store.PresignPut(r.Context(), &storage.PresignPutInput{
		Key:         key,
		ContentType: req.ContentType,
	})
	if err != nil {
		if errors.Is(err, storage.ErrMIMENotAllowed) {
			helper.Error(w, http.StatusBadRequest, "unsupported file type")
			return
		}
		helper.Error(w, http.StatusInternalServerError, "failed to generate upload url")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]any{
		"upload_url": uploadURL,
		"file_url":   key,
		"key":        key,
	})
}

// FileURL generates a presigned GET URL to download/view a chat file.
// GET /chat/file?key=chat/userId/uuid.ext
func (h *ChatHandler) FileURL(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		helper.Error(w, http.StatusServiceUnavailable, "file storage not configured")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" || !strings.HasPrefix(key, "chat/") {
		helper.Error(w, http.StatusBadRequest, "invalid key")
		return
	}

	url, err := h.store.PresignGet(r.Context(), &storage.PresignGetInput{Key: key})
	if err != nil {
		helper.Error(w, http.StatusInternalServerError, "failed to generate download url")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]string{"url": url})
}

// HandleWS upgrades the connection to WebSocket and hands off to the chat engine.
// GET /ws
func (h *ChatHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	chat.GetEngine().HandleWS(w, r)
}

// GetRooms returns the paginated room list for the authenticated user.
// GET /chat/rooms?cursor=&limit=
func (h *ChatHandler) GetRooms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	adminID := getAdminID(ctx)

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 30
	}

	rooms, nextCursor, err := h.chatSvc.GetRooms(ctx, userID, adminID, cursor, limit)
	if err != nil {
		log.Printf("ERROR [chat] - get rooms failed user=%s admin=%s error=%v", userID, adminID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch rooms")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]any{
		"rooms":       rooms,
		"next_cursor": nextCursor,
	})
}

// CreateDM creates a DM room with another user.
// POST /chat/rooms/dm
func (h *ChatHandler) CreateDM(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	adminID := getAdminID(ctx)

	var req models.CreateDMRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.UserID == "" {
		helper.Error(w, http.StatusBadRequest, "user_id is required")
		return
	}

	room, created, err := h.chatSvc.CreateDM(ctx, adminID, userID, req.UserID)
	if err != nil {
		switch err {
		case service.ErrSameUser:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotSameOrg:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - create dm failed user=%s target=%s error=%s", userID, req.UserID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to create dm")
		}
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	helper.JSON(w, status, room)
}

// GetMessages returns paginated messages for a room.
// GET /chat/rooms/{id}/messages?cursor=&limit=
func (h *ChatHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}

	msgs, nextCursor, err := h.chatSvc.GetMessages(ctx, roomID, userID, cursor, limit)
	if err != nil {
		if err == service.ErrNotMember {
			helper.Error(w, http.StatusForbidden, "not a member of this room")
			return
		}
		log.Printf("ERROR [chat] - get messages failed room=%s user=%s error=%s", roomID, userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch messages")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]any{
		"messages":    msgs,
		"next_cursor": nextCursor,
	})
}

// EditMessage edits a message (sender only).
// PATCH /chat/messages/{id}
func (h *ChatHandler) EditMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	msgID := r.PathValue("id")

	var req struct {
		Content string `json:"content"`
	}
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := h.chatSvc.EditMessage(ctx, msgID, userID, req.Content); err != nil {
		if err == service.ErrEmptyMessage {
			helper.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("ERROR [chat] - edit message failed msg=%s user=%s error=%s", msgID, userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to edit message")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteMessage soft-deletes a message (sender only).
// DELETE /chat/messages/{id}
func (h *ChatHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	msgID := r.PathValue("id")

	if err := h.chatSvc.DeleteMessage(ctx, msgID, userID); err != nil {
		log.Printf("ERROR [chat] - delete message failed msg=%s user=%s error=%s", msgID, userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetCallHistory returns paginated call history.
// GET /chat/calls?cursor=&limit=
func (h *ChatHandler) GetCallHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	calls, nextCursor, err := h.chatSvc.GetCallHistory(ctx, userID, cursor, limit)
	if err != nil {
		log.Printf("ERROR [chat] - get call history failed user=%s error=%s", userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch call history")
		return
	}

	helper.JSON(w, http.StatusOK, map[string]any{
		"calls":       calls,
		"next_cursor": nextCursor,
	})
}

// getAdminID extracts the org admin_id from JWT claims.
func getAdminID(ctx context.Context) string {
	role := middleware.Role(ctx)
	if role == middleware.RoleAdmin || role == middleware.RoleSuperAdmin {
		return middleware.Subject(ctx)
	}
	return middleware.ClaimString(ctx, "admin_id")
}

// SearchMessages searches messages across all rooms the user is a member of.
// GET /chat/messages/search?q=&limit=
func (h *ChatHandler) SearchMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)

	q := r.URL.Query()
	query := q.Get("q")
	if query == "" || len(query) < 2 {
		helper.Error(w, http.StatusBadRequest, "query must be at least 2 characters")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 20
	}

	results, err := h.chatSvc.SearchMessages(ctx, userID, query, limit)
	if err != nil {
		log.Printf("ERROR [chat] - search messages failed user=%s query=%s error=%v", userID, query, err)
		helper.Error(w, http.StatusInternalServerError, "search failed")
		return
	}
	helper.JSON(w, http.StatusOK, map[string]any{
		"results": results,
	})
}

// ---------- Group Chat ----------

// CreateGroup creates a group room with the given members.
// POST /chat/groups
func (h *ChatHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	adminID := getAdminID(ctx)

	var req models.CreateGroupRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" {
		helper.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.MemberIDs) == 0 {
		helper.Error(w, http.StatusBadRequest, "member_ids is required")
		return
	}

	room, err := h.chatSvc.CreateGroup(ctx, adminID, userID, req.Name, req.MemberIDs)
	if err != nil {
		switch err {
		case service.ErrGroupNameRequired, service.ErrNoMembers:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotSameOrg:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - create group failed user=%s error=%s", userID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to create group")
		}
		return
	}
	helper.JSON(w, http.StatusCreated, room)
}

// UpdateGroup updates a group room's name.
// PUT /chat/groups/{id}
func (h *ChatHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")

	var req models.UpdateGroupRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := h.chatSvc.UpdateGroupName(ctx, roomID, userID, req.Name); err != nil {
		switch err {
		case service.ErrGroupNameRequired:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotGroupRoom:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotGroupAdmin:
			helper.Error(w, http.StatusForbidden, err.Error())
		case service.ErrNotMember:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - update group failed room=%s user=%s error=%s", roomID, userID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to update group")
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UploadGroupAvatar handles POST /chat/groups/{id}/avatar — presigned upload for group avatar.
func (h *ChatHandler) UploadGroupAvatar(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		helper.Error(w, http.StatusServiceUnavailable, "file uploads not configured")
		return
	}
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")

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
	key := "avatars/group/" + roomID + "/" + uuid.NewString() + ext

	uploadURL, err := h.store.PresignPut(ctx, &storage.PresignPutInput{
		Key:         key,
		ContentType: req.ContentType,
		Expiry:      5 * 60 * 1e9, // 5 minutes
	})
	if err != nil {
		log.Printf("ERROR [chat] - presign put group avatar failed error=%s", err)
		helper.Error(w, http.StatusInternalServerError, "failed to generate upload URL")
		return
	}

	if err := h.chatSvc.UpdateGroupAvatar(ctx, roomID, userID, key); err != nil {
		switch err {
		case service.ErrNotGroupRoom:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotGroupAdmin, service.ErrNotMember:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - update group avatar failed room=%s error=%s", roomID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to update group avatar")
		}
		return
	}

	helper.JSON(w, http.StatusOK, map[string]any{
		"upload_url": uploadURL,
		"key":        key,
	})
}

// AddGroupMembers adds members to a group room.
// POST /chat/groups/{id}/members
func (h *ChatHandler) AddGroupMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	adminID := getAdminID(ctx)
	roomID := r.PathValue("id")

	var req models.AddMembersRequest
	if err := helper.ReadJSON(r, &req); err != nil {
		helper.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := h.chatSvc.AddGroupMembers(ctx, roomID, adminID, userID, req.UserIDs); err != nil {
		switch err {
		case service.ErrNoMembers:
			helper.Error(w, http.StatusBadRequest, err.Error())
		case service.ErrNotGroupRoom, service.ErrNotGroupAdmin, service.ErrNotMember:
			helper.Error(w, http.StatusForbidden, err.Error())
		case service.ErrNotSameOrg:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - add members failed room=%s user=%s error=%s", roomID, userID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to add members")
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveGroupMember removes a member from a group room.
// DELETE /chat/groups/{id}/members/{userId}
func (h *ChatHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")
	targetID := r.PathValue("userId")

	if err := h.chatSvc.RemoveGroupMember(ctx, roomID, userID, targetID); err != nil {
		switch err {
		case service.ErrNotGroupRoom, service.ErrNotGroupAdmin, service.ErrNotMember:
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			log.Printf("ERROR [chat] - remove member failed room=%s actor=%s target=%s error=%s", roomID, userID, targetID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to remove member")
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PromoteMember promotes a member to co-admin.
// POST /chat/groups/{id}/members/{userId}/promote
func (h *ChatHandler) PromoteMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")
	targetID := r.PathValue("userId")

	if err := h.chatSvc.PromoteMember(ctx, roomID, userID, targetID); err != nil {
		switch {
		case errors.Is(err, service.ErrNotGroupAdmin), errors.Is(err, service.ErrNotMember):
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			helper.Error(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DemoteMember demotes a co-admin to member.
// POST /chat/groups/{id}/members/{userId}/demote
func (h *ChatHandler) DemoteMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")
	targetID := r.PathValue("userId")

	if err := h.chatSvc.DemoteMember(ctx, roomID, userID, targetID); err != nil {
		switch {
		case errors.Is(err, service.ErrNotGroupAdmin), errors.Is(err, service.ErrNotMember):
			helper.Error(w, http.StatusForbidden, err.Error())
		default:
			helper.Error(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LeaveGroup removes the caller from a group.
// POST /chat/groups/{id}/leave
func (h *ChatHandler) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")

	if err := h.chatSvc.LeaveGroup(ctx, roomID, userID); err != nil {
		switch err {
		case service.ErrNotGroupRoom:
			helper.Error(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("ERROR [chat] - leave group failed room=%s user=%s error=%s", roomID, userID, err)
			helper.Error(w, http.StatusInternalServerError, "failed to leave group")
		}
		return
	}
	helper.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetGroupMembers returns the member list of a group.
// GET /chat/groups/{id}/members
func (h *ChatHandler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.Subject(ctx)
	roomID := r.PathValue("id")

	members, err := h.chatSvc.GetGroupMembers(ctx, roomID, userID)
	if err != nil {
		if err == service.ErrNotMember {
			helper.Error(w, http.StatusForbidden, "not a member of this group")
			return
		}
		log.Printf("ERROR [chat] - get group members failed room=%s user=%s error=%s", roomID, userID, err)
		helper.Error(w, http.StatusInternalServerError, "failed to fetch members")
		return
	}
	helper.JSON(w, http.StatusOK, members)
}
