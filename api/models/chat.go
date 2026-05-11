package models

import "time"

// Room member status
const (
	MemberStatusActive = "active"
	MemberStatusLeft   = "left"
)

// Room represents a chat room (DM or group).
type Room struct {
	ID        string    `json:"id"`
	AdminID   string    `json:"admin_id"`
	Type      string    `json:"type"` // "dm" or "group"
	Name      *string   `json:"name,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined fields (from queries)
	Members     []RoomMember `json:"members,omitempty"`
	LastMessage *Message     `json:"last_message,omitempty"`
	UnreadCount int          `json:"unread_count,omitempty"`
}

// RoomMember represents a user's membership in a room.
type RoomMember struct {
	ID              string     `json:"id"`
	RoomID          string     `json:"room_id"`
	UserID          string     `json:"user_id"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	JoinedAt        time.Time  `json:"joined_at"`
	LeftAt          *time.Time `json:"left_at,omitempty"`
	LastReadAt      *time.Time `json:"last_read_at,omitempty"`
	LastDeliveredAt *time.Time `json:"last_delivered_at,omitempty"`

	// Joined fields
	UserName      string `json:"user_name,omitempty"`
	UserEmail     string `json:"user_email,omitempty"`
	UserAvatarURL string `json:"user_avatar_url,omitempty"`
}

// Message represents a chat message.
type Message struct {
	ID        string     `json:"id"`
	RoomID    string     `json:"room_id"`
	SenderID  string     `json:"sender_id"`
	Content   *string    `json:"content,omitempty"`
	MediaURL  *string    `json:"media_url,omitempty"`
	MediaType *string    `json:"media_type,omitempty"`
	ReplyTo   *string    `json:"reply_to,omitempty"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`

	// Joined fields
	SenderName      string `json:"sender_name,omitempty"`
	SenderAvatarURL string `json:"sender_avatar_url,omitempty"`
}

// MessageSearchResult is a message returned from search with room context.
type MessageSearchResult struct {
	ID         string    `json:"id"`
	RoomID     string    `json:"room_id"`
	SenderID   string    `json:"sender_id"`
	Content    *string   `json:"content,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	SenderName string    `json:"sender_name"`
	RoomName   string    `json:"room_name"`
}

// CallLog represents a call history entry.
type CallLog struct {
	ID              string     `json:"id"`
	CallID          string     `json:"call_id"`
	RoomID          *string    `json:"room_id,omitempty"`
	InitiatedBy     string     `json:"initiated_by"`
	PeerID          *string    `json:"peer_id,omitempty"`
	CallType        string     `json:"call_type"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`

	// Joined fields
	PeerName string `json:"peer_name,omitempty"`
}

// CreateDMRequest is the request body for creating a DM room.
type CreateDMRequest struct {
	UserID string `json:"user_id"`
}

// CreateGroupRequest is the request body for creating a group room.
type CreateGroupRequest struct {
	Name      string   `json:"name"`
	MemberIDs []string `json:"member_ids"`
}

// UpdateGroupRequest is the request body for updating a group room.
type UpdateGroupRequest struct {
	Name string `json:"name"`
}

// AddMembersRequest is the request body for adding members to a group.
type AddMembersRequest struct {
	UserIDs []string `json:"user_ids"`
}

// RoomListItem is a room in the sidebar list with last message + unread count.
type RoomListItem struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Name           *string  `json:"name,omitempty"`
	AvatarURL      string   `json:"avatar_url,omitempty"`
	OtherUserID    string   `json:"other_user_id,omitempty"`
	OtherName      string   `json:"other_name,omitempty"`
	OtherAvatarURL string   `json:"other_avatar_url,omitempty"`
	LastMessage    *Message `json:"last_message,omitempty"`
	UnreadCount    int      `json:"unread_count"`
	UpdatedAt      string   `json:"updated_at"`
}
