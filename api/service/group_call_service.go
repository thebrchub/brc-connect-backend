package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shivanand-burli/go-starter-kit/chat"
	"github.com/shivanand-burli/go-starter-kit/chat/chatpb"
	"github.com/shivanand-burli/go-starter-kit/redis"
	"github.com/shivanand-burli/go-starter-kit/rtc"
	"google.golang.org/protobuf/proto"

	"brc-connect-backend/api/repository"
)

var (
	ErrCallAlreadyActive = errors.New("group call already active")
	ErrNoActiveCall      = errors.New("no active group call")
	ErrLiveKitDisabled   = errors.New("livekit is not configured")
)

const lkRoomPrefix = "gc_" // LiveKit room name prefix

// GroupCallService handles group call lifecycle.
type GroupCallService struct {
	roomRepo  *repository.RoomRepo
	rtcClient *rtc.Client
}

// NewGroupCallService creates a new group call service.
func NewGroupCallService(roomRepo *repository.RoomRepo, rtcClient *rtc.Client) *GroupCallService {
	return &GroupCallService{roomRepo: roomRepo, rtcClient: rtcClient}
}

// StartCall creates a LiveKit room and marks the group call as active.
func (s *GroupCallService) StartCall(ctx context.Context, roomID, userID string) (string, error) {
	if !s.rtcClient.IsConfigured() {
		return "", ErrLiveKitDisabled
	}

	// Must be a group room
	if err := s.requireGroup(ctx, roomID); err != nil {
		return "", err
	}
	// Must be a member
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return "", err
	}

	// Check no active call
	if chat.GetGroupCallActive(ctx, roomID) != nil {
		return "", ErrCallAlreadyActive
	}

	// Create LiveKit room (name = "gc_{roomID}")
	lkRoomName := lkRoomPrefix + roomID
	if _, err := s.rtcClient.CreateRoom(ctx, lkRoomName, 50); err != nil {
		return "", fmt.Errorf("create livekit room: %w", err)
	}

	// Store active call state in kit-owned Redis keys
	chat.SetGroupCallActive(ctx, roomID, lkRoomName, userID)

	// Broadcast to room via WS (this triggers fireGroupCallEvent in the kit's pub/sub handler)
	s.broadcastGroupCallEvent(roomID, userID, string(chat.MsgGroupCallStarted))

	// Generate token for the starter
	name := s.roomRepo.GetUserName(ctx, userID)
	token, err := s.rtcClient.GenerateToken(lkRoomName, userID, name, true, true)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return token, nil
}

// JoinCall generates a LiveKit token for an existing group call.
func (s *GroupCallService) JoinCall(ctx context.Context, roomID, userID string) (string, error) {
	if !s.rtcClient.IsConfigured() {
		return "", ErrLiveKitDisabled
	}
	if err := s.requireMember(ctx, roomID, userID); err != nil {
		return "", err
	}

	state := chat.GetGroupCallActive(ctx, roomID)
	if state == nil {
		return "", ErrNoActiveCall
	}

	name := s.roomRepo.GetUserName(ctx, userID)
	token, err := s.rtcClient.GenerateToken(state.LkRoomName, userID, name, true, true)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	s.broadcastGroupCallEvent(roomID, userID, string(chat.MsgGroupCallJoined))
	return token, nil
}

// LeaveCall notifies the room that a user left the group call.
func (s *GroupCallService) LeaveCall(ctx context.Context, roomID, userID string) error {
	state := chat.GetGroupCallActive(ctx, roomID)
	if state == nil {
		return ErrNoActiveCall
	}

	// Remove from LiveKit
	s.rtcClient.RemoveParticipant(ctx, state.LkRoomName, userID)

	s.broadcastGroupCallEvent(roomID, userID, string(chat.MsgGroupCallLeft))

	// Auto-end if no participants left
	participants, _ := s.rtcClient.ListParticipants(ctx, state.LkRoomName)
	if len(participants) == 0 {
		s.broadcastGroupCallEvent(roomID, userID, string(chat.MsgGroupCallEnded))
		s.cleanupCall(ctx, roomID, state.LkRoomName)
	}

	return nil
}

// EndCall forcefully ends a group call (group admin only).
func (s *GroupCallService) EndCall(ctx context.Context, roomID, userID string) error {
	if !s.rtcClient.IsConfigured() {
		return ErrLiveKitDisabled
	}

	if err := s.requireAdmin(ctx, roomID, userID); err != nil {
		return err
	}

	state := chat.GetGroupCallActive(ctx, roomID)
	if state == nil {
		return ErrNoActiveCall
	}

	s.broadcastGroupCallEvent(roomID, userID, string(chat.MsgGroupCallEnded))
	s.cleanupCall(ctx, roomID, state.LkRoomName)
	return nil
}

// MuteParticipant mutes a participant's published track (group admin only).
func (s *GroupCallService) MuteParticipant(ctx context.Context, roomID, adminID, targetID, trackSID string) error {
	if err := s.requireAdmin(ctx, roomID, adminID); err != nil {
		return err
	}

	state := chat.GetGroupCallActive(ctx, roomID)
	if state == nil {
		return ErrNoActiveCall
	}

	if _, err := s.rtcClient.MutePublishedTrack(ctx, state.LkRoomName, targetID, trackSID, true); err != nil {
		return fmt.Errorf("mute participant: %w", err)
	}
	return nil
}

// KickParticipant removes a participant from the call (group admin only).
func (s *GroupCallService) KickParticipant(ctx context.Context, roomID, adminID, targetID string) error {
	if err := s.requireAdmin(ctx, roomID, adminID); err != nil {
		return err
	}

	state := chat.GetGroupCallActive(ctx, roomID)
	if state == nil {
		return ErrNoActiveCall
	}

	if err := s.rtcClient.RemoveParticipant(ctx, state.LkRoomName, targetID); err != nil {
		return fmt.Errorf("kick participant: %w", err)
	}
	return nil
}

func (s *GroupCallService) requireAdmin(ctx context.Context, roomID, userID string) error {
	role, err := s.roomRepo.GetMemberRole(ctx, roomID, userID)
	if err != nil {
		return ErrNotMember
	}
	if role != "admin" {
		return ErrNotGroupAdmin
	}
	return nil
}

func (s *GroupCallService) requireGroup(ctx context.Context, roomID string) error {
	room, err := s.roomRepo.GetByID(ctx, roomID)
	if err != nil {
		return err
	}
	if room.Type != string(chat.RoomGroup) {
		return ErrNotGroupRoom
	}
	return nil
}

func (s *GroupCallService) requireMember(ctx context.Context, roomID, userID string) error {
	ok, err := s.roomRepo.IsMember(ctx, roomID, userID)
	if err != nil || !ok {
		return ErrNotMember
	}
	return nil
}

// ActiveCallInfo is a lightweight snapshot of an active group call for a room.
type ActiveCallInfo struct {
	RoomID       string   `json:"room_id"`
	StartedBy    string   `json:"started_by"`
	StartedAt    int64    `json:"started_at"`
	Participants []string `json:"participants"`
}

// GetActiveCalls returns all active group calls in the user's rooms.
func (s *GroupCallService) GetActiveCalls(ctx context.Context, userID, adminID string) ([]ActiveCallInfo, error) {
	roomIDs, err := s.roomRepo.GetUserRoomIDs(ctx, userID, adminID)
	if err != nil {
		return nil, err
	}
	var result []ActiveCallInfo
	for _, rid := range roomIDs {
		state := chat.GetGroupCallActive(ctx, rid)
		if state != nil {
			info := ActiveCallInfo{
				RoomID:       rid,
				StartedBy:    state.StartedBy,
				StartedAt:    state.StartedAt,
				Participants: []string{},
			}
			if parts, err := s.rtcClient.ListParticipants(ctx, state.LkRoomName); err == nil {
				for _, p := range parts {
					info.Participants = append(info.Participants, p.Identity)
				}
			}
			result = append(result, info)
		}
	}
	return result, nil
}

func (s *GroupCallService) cleanupCall(ctx context.Context, roomID, lkRoomName string) {
	s.rtcClient.DeleteRoom(ctx, lkRoomName)
	chat.ClearGroupCall(ctx, roomID)
}

func (s *GroupCallService) broadcastGroupCallEvent(roomID, userID, action string) {
	event := &chatpb.GroupCallEvent{
		RoomId: roomID,
		UserId: userID,
		Action: action,
	}
	payload, _ := proto.Marshal(event)

	// Id = lkRoomName (used as callID for persistence), From = userID
	lkRoomName := lkRoomPrefix + roomID
	env := &chatpb.Envelope{
		Type:    action,
		Id:      lkRoomName,
		RoomId:  roomID,
		From:    userID,
		Payload: payload,
	}
	data, _ := proto.Marshal(env)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	redis.Publish(ctx, chat.GetEngine().Config().PubSubChannel, data)
}
