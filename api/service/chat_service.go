package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/chat"
	"github.com/shivanand-burli/go-starter-kit/chat/chatpb"
	"github.com/shivanand-burli/go-starter-kit/middleware"
	"google.golang.org/protobuf/proto"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
)

var (
	ErrSameUser     = errors.New("cannot create DM with yourself")
	ErrNotSameOrg   = errors.New("users are not in the same organization")
	ErrNotMember    = errors.New("not a member of this room")
	ErrEmptyMessage = errors.New("message content is empty")
)

type ChatService struct {
	roomRepo *repository.RoomRepo
	msgRepo  *repository.MessageRepo
}

func NewChatService(roomRepo *repository.RoomRepo, msgRepo *repository.MessageRepo) *ChatService {
	return &ChatService{
		roomRepo: roomRepo,
		msgRepo:  msgRepo,
	}
}

// CreateDM creates a DM room between two users, enforcing org membership.
func (s *ChatService) CreateDM(ctx context.Context, adminID, currentUserID, targetUserID string) (*models.Room, bool, error) {
	if currentUserID == targetUserID {
		return nil, false, ErrSameUser
	}

	// Check both users are in the same org
	sameOrg, err := s.roomRepo.SameOrg(ctx, currentUserID, targetUserID)
	if err != nil {
		return nil, false, err
	}
	if !sameOrg {
		return nil, false, ErrNotSameOrg
	}

	return s.roomRepo.FindOrCreateDM(ctx, adminID, currentUserID, targetUserID)
}

// GetRooms returns the paginated room list for a user.
func (s *ChatService) GetRooms(ctx context.Context, userID, adminID, cursor string, limit int) ([]models.RoomListItem, string, error) {
	return s.roomRepo.GetUserRooms(ctx, userID, adminID, cursor, limit)
}

// GetMessages returns paginated messages for a room.
func (s *ChatService) GetMessages(ctx context.Context, roomID, userID, cursor string, limit int) ([]models.Message, string, error) {
	isMember, err := s.roomRepo.IsMember(ctx, roomID, userID)
	if err != nil {
		return nil, "", err
	}
	if !isMember {
		return nil, "", ErrNotMember
	}

	return s.msgRepo.GetByRoom(ctx, roomID, cursor, limit)
}

// EditMessage edits a message (sender only).
func (s *ChatService) EditMessage(ctx context.Context, msgID, senderID, content string) error {
	if content == "" {
		return ErrEmptyMessage
	}
	return s.msgRepo.UpdateMessage(ctx, msgID, senderID, content)
}

// DeleteMessage soft-deletes a message (sender only).
func (s *ChatService) DeleteMessage(ctx context.Context, msgID, senderID string) error {
	return s.msgRepo.SoftDeleteMessage(ctx, msgID, senderID)
}

// GetCallHistory returns paginated call history.
func (s *ChatService) GetCallHistory(ctx context.Context, userID, cursor string, limit int) ([]models.CallLog, string, error) {
	return s.msgRepo.GetCallHistory(ctx, userID, cursor, limit)
}

// GetUserRoomIDs returns all active room IDs for a user. Used by chat.Hooks.LoadUserRooms.
func (s *ChatService) GetUserRoomIDs(ctx context.Context, userID, adminID string) ([]string, error) {
	return s.roomRepo.GetUserRoomIDs(ctx, userID, adminID)
}

// GetAllUserRoomIDs returns all active room IDs for a user across all orgs.
func (s *ChatService) GetAllUserRoomIDs(ctx context.Context, userID string) ([]string, error) {
	return s.roomRepo.GetAllUserRoomIDs(ctx, userID)
}

// SameOrg checks if two users belong to the same org. Used by chat.Hooks.CanCall.
func (s *ChatService) SameOrg(ctx context.Context, userID1, userID2 string) (bool, error) {
	return s.roomRepo.SameOrg(ctx, userID1, userID2)
}

// ---------- Group Chat ----------

var (
	ErrGroupNameRequired = errors.New("group name is required")
	ErrNoMembers         = errors.New("at least one member is required")
	ErrNotGroupRoom      = errors.New("room is not a group")
	ErrNotGroupAdmin     = errors.New("only group admins can perform this action")
)

// CreateGroup creates a group room. All members must be in the same org.
func (s *ChatService) CreateGroup(ctx context.Context, adminID, creatorID, name string, memberIDs []string) (*models.Room, error) {
	if name == "" {
		return nil, ErrGroupNameRequired
	}
	if len(memberIDs) == 0 {
		return nil, ErrNoMembers
	}

	// Verify all members are in the same org
	for _, uid := range memberIDs {
		if uid == creatorID {
			continue
		}
		ok, err := s.roomRepo.SameOrg(ctx, creatorID, uid)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotSameOrg
		}
	}

	return s.roomRepo.CreateGroup(ctx, adminID, creatorID, name, memberIDs)
}

// UpdateGroupName updates the name of a group room. Only group admins can do this.
func (s *ChatService) UpdateGroupName(ctx context.Context, roomID, userID, name string) error {
	if name == "" {
		return ErrGroupNameRequired
	}
	if err := s.requireGroupAdmin(ctx, roomID, userID); err != nil {
		return err
	}
	return s.roomRepo.UpdateGroupName(ctx, roomID, name)
}

// UpdateGroupAvatar updates the avatar of a group room. Only group admins can do this.
func (s *ChatService) UpdateGroupAvatar(ctx context.Context, roomID, userID, avatarURL string) error {
	if err := s.requireGroupAdmin(ctx, roomID, userID); err != nil {
		return err
	}
	return s.roomRepo.UpdateGroupAvatar(ctx, roomID, avatarURL)
}

// AddGroupMembers adds members to a group room. Only group admins can do this.
// All new members must be in the same org.
func (s *ChatService) AddGroupMembers(ctx context.Context, roomID, adminID, userID string, newMemberIDs []string) error {
	if len(newMemberIDs) == 0 {
		return ErrNoMembers
	}
	if err := s.requireGroupAdmin(ctx, roomID, userID); err != nil {
		return err
	}

	// Verify all new members are in the same org
	for _, uid := range newMemberIDs {
		ok, err := s.roomRepo.SameOrg(ctx, userID, uid)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotSameOrg
		}
	}

	return s.roomRepo.AddMembers(ctx, roomID, newMemberIDs)
}

// RemoveGroupMember removes a member from a group room. Only group admins can do this.
// Cannot remove yourself — use LeaveGroup instead.
func (s *ChatService) RemoveGroupMember(ctx context.Context, roomID, actorID, targetID string) error {
	if actorID == targetID {
		return errors.New("use leave endpoint to remove yourself")
	}
	if err := s.requireGroupAdmin(ctx, roomID, actorID); err != nil {
		return err
	}
	return s.roomRepo.RemoveMember(ctx, roomID, targetID)
}

// PromoteMember promotes a member to co-admin. Only group admins can do this.
func (s *ChatService) PromoteMember(ctx context.Context, roomID, actorID, targetID string) error {
	if actorID == targetID {
		return errors.New("cannot promote yourself")
	}
	if err := s.requireGroupAdmin(ctx, roomID, actorID); err != nil {
		return err
	}
	role, err := s.roomRepo.GetMemberRole(ctx, roomID, targetID)
	if err != nil {
		return ErrNotMember
	}
	if role == "admin" {
		return errors.New("user is already an admin")
	}
	return s.roomRepo.UpdateMemberRole(ctx, roomID, targetID, "admin")
}

// DemoteMember demotes a co-admin to member. Only group admins can do this.
func (s *ChatService) DemoteMember(ctx context.Context, roomID, actorID, targetID string) error {
	if actorID == targetID {
		return errors.New("cannot demote yourself")
	}
	if err := s.requireGroupAdmin(ctx, roomID, actorID); err != nil {
		return err
	}
	role, err := s.roomRepo.GetMemberRole(ctx, roomID, targetID)
	if err != nil {
		return ErrNotMember
	}
	if role != "admin" {
		return errors.New("user is not an admin")
	}
	return s.roomRepo.UpdateMemberRole(ctx, roomID, targetID, "member")
}

// LeaveGroup removes the caller from a group. If the last admin leaves,
// the oldest remaining member is auto-promoted.
func (s *ChatService) LeaveGroup(ctx context.Context, roomID, userID string) error {
	if err := s.requireGroup(ctx, roomID); err != nil {
		return err
	}
	if err := s.roomRepo.RemoveMember(ctx, roomID, userID); err != nil {
		return err
	}
	// Auto-promote if no admins remain
	s.roomRepo.PromoteOldestMember(ctx, roomID)
	return nil
}

// GetGroupMembers returns the member list of a group room.
func (s *ChatService) GetGroupMembers(ctx context.Context, roomID, userID string) ([]models.RoomMember, error) {
	isMember, err := s.roomRepo.IsMember(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotMember
	}
	return s.roomRepo.GetGroupMembers(ctx, roomID)
}

// SearchMessages searches messages across all rooms the user is a member of.
func (s *ChatService) SearchMessages(ctx context.Context, userID, query string, limit int) ([]models.MessageSearchResult, error) {
	return s.msgRepo.SearchMessages(ctx, userID, query, limit)
}

// requireGroupAdmin checks the room is a group and the user is an admin in it.
func (s *ChatService) requireGroupAdmin(ctx context.Context, roomID, userID string) error {
	if err := s.requireGroup(ctx, roomID); err != nil {
		return err
	}
	role, err := s.roomRepo.GetMemberRole(ctx, roomID, userID)
	if err != nil {
		return ErrNotMember
	}
	if role != middleware.RoleAdmin {
		return ErrNotGroupAdmin
	}
	return nil
}

// requireGroup checks the room is a group (not a DM).
func (s *ChatService) requireGroup(ctx context.Context, roomID string) error {
	roomType, err := s.roomRepo.GetRoomType(ctx, roomID)
	if err != nil {
		return err
	}
	if roomType != string(chat.RoomGroup) {
		return ErrNotGroupRoom
	}
	return nil
}

// FlushRoomMessages decodes raw protobuf envelopes for a single room and persists them.
// Handles both chat messages (INSERT) and read/delivered receipts (UPDATE last_read_at/
// last_delivered_at). Called by the chat engine's OnFlush hook.
func (s *ChatService) FlushRoomMessages(ctx context.Context, roomID string, rawMessages [][]byte) error {
	var messages []models.Message

	// Deduplicate receipts: keep only the latest timestamp per user+type
	type receiptKey struct {
		userID  string
		msgType chat.MsgType
	}
	latestReceipts := make(map[receiptKey]time.Time)

	for _, raw := range rawMessages {
		var env chatpb.Envelope
		if err := proto.Unmarshal(raw, &env); err != nil {
			log.Printf("[flusher] unmarshal failed room=%s: %v", roomID, err)
			continue
		}

		switch chat.MsgType(env.Type) {
		case chat.MsgRead:
			key := receiptKey{userID: env.From, msgType: chat.MsgRead}
			ts := time.UnixMilli(env.Ts)
			if existing, ok := latestReceipts[key]; !ok || ts.After(existing) {
				latestReceipts[key] = ts
			}

		case chat.MsgDelivered:
			key := receiptKey{userID: env.From, msgType: chat.MsgDelivered}
			ts := time.UnixMilli(env.Ts)
			if existing, ok := latestReceipts[key]; !ok || ts.After(existing) {
				latestReceipts[key] = ts
			}

		case chat.MsgChatMessage:
			msg := models.Message{
				ID:        env.Id,
				RoomID:    env.RoomId,
				SenderID:  env.From,
				CreatedAt: time.UnixMilli(env.Ts),
			}

			if msg.ID == "" {
				msg.ID = uuid.New().String()
			}

			if len(env.Payload) > 0 {
				var chatMsg chatpb.ChatMessage
				if err := proto.Unmarshal(env.Payload, &chatMsg); err == nil {
					if chatMsg.Text != "" {
						msg.Content = &chatMsg.Text
					}
					if chatMsg.MediaUrl != "" {
						msg.MediaURL = &chatMsg.MediaUrl
					}
					if chatMsg.MediaType != "" {
						msg.MediaType = &chatMsg.MediaType
					}
					if chatMsg.ReplyTo != "" {
						msg.ReplyTo = &chatMsg.ReplyTo
					}
				}
			}

			messages = append(messages, msg)
		}
	}

	if len(messages) > 0 {
		if err := s.msgRepo.InsertBatch(ctx, messages); err != nil {
			log.Printf("[flusher] batch insert failed room=%s count=%d: %v", roomID, len(messages), err)
			return err
		}
	}

	// Batch-update receipts (already deduplicated — one UPDATE per user+type)
	for key, ts := range latestReceipts {
		switch key.msgType {
		case chat.MsgRead:
			if err := s.roomRepo.UpdateLastRead(ctx, roomID, key.userID, ts); err != nil {
				log.Printf("[flusher] update last_read failed room=%s user=%s: %v", roomID, key.userID, err)
			}
		case chat.MsgDelivered:
			if err := s.roomRepo.UpdateLastDelivered(ctx, roomID, key.userID, ts); err != nil {
				log.Printf("[flusher] update last_delivered failed room=%s user=%s: %v", roomID, key.userID, err)
			}
		}
	}

	return nil
}
