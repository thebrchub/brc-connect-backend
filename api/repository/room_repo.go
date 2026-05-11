package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/models"
)

type RoomRepo struct {
	cacheTTL time.Duration
}

func NewRoomRepo(cacheTTL time.Duration) *RoomRepo {
	return &RoomRepo{cacheTTL: cacheTTL}
}

// FindOrCreateDM finds an existing DM room between two users (scoped by adminID)
// or creates a new one. Returns the room and whether it was newly created.
func (r *RoomRepo) FindOrCreateDM(ctx context.Context, adminID, userID1, userID2 string) (*models.Room, bool, error) {
	// Check for existing DM
	var roomID string
	err := postgress.GetPool().QueryRow(ctx, `
		SELECT rm1.room_id FROM room_members rm1
		JOIN room_members rm2 ON rm1.room_id = rm2.room_id
		JOIN rooms r ON r.id = rm1.room_id AND r.type = 'dm' AND r.admin_id = $3
		WHERE rm1.user_id = $1 AND rm2.user_id = $2
		AND rm1.status = 'active' AND rm2.status = 'active'
		LIMIT 1
	`, userID1, userID2, adminID).Scan(&roomID)

	if err == nil {
		room, err := r.GetByID(ctx, roomID)
		return room, false, err
	}

	// Create new room
	var room models.Room
	err = postgress.GetPool().QueryRow(ctx, `
		INSERT INTO rooms (admin_id, type) VALUES ($1, 'dm')
		RETURNING id, admin_id, type, name, created_at, updated_at
	`, adminID).Scan(&room.ID, &room.AdminID, &room.Type, &room.Name, &room.CreatedAt, &room.UpdatedAt)
	if err != nil {
		return nil, false, err
	}

	// Add both members
	batch := &pgx.Batch{}
	batch.Queue(`INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, 'member')`, room.ID, userID1)
	batch.Queue(`INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, 'member')`, room.ID, userID2)
	br := postgress.GetPool().SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < 2; i++ {
		if _, err := br.Exec(); err != nil {
			return nil, false, err
		}
	}

	return &room, true, nil
}

// GetByID returns a room by ID.
func (r *RoomRepo) GetByID(ctx context.Context, id string) (*models.Room, error) {
	return redis.Fetch(ctx, "room:"+id, r.cacheTTL, func(ctx context.Context) (*models.Room, error) {
		var room models.Room
		err := postgress.GetPool().QueryRow(ctx, `
			SELECT id, admin_id, type, name, created_at, updated_at
			FROM rooms WHERE id = $1
		`, id).Scan(&room.ID, &room.AdminID, &room.Type, &room.Name, &room.CreatedAt, &room.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return &room, nil
	})
}

// GetUserRoomIDs returns all active room IDs for a user (scoped by adminID).
func (r *RoomRepo) GetUserRoomIDs(ctx context.Context, userID, adminID string) ([]string, error) {
	rows, err := postgress.GetPool().Query(ctx, `
		SELECT rm.room_id FROM room_members rm
		JOIN rooms r ON r.id = rm.room_id AND r.admin_id = $2
		WHERE rm.user_id = $1 AND rm.status = 'active'
	`, userID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetAllUserRoomIDs returns all active room IDs for a user across all orgs.
// Used by the chat engine's LoadUserRooms hook (no admin context available).
func (r *RoomRepo) GetAllUserRoomIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := postgress.GetPool().Query(ctx, `
		SELECT room_id FROM room_members
		WHERE user_id = $1 AND status = 'active'
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetUserRooms returns the room list for a user with last message and unread count.
// Cursor is the sort_time (RFC3339Nano) of the last item from the previous page. Empty = latest.
func (r *RoomRepo) GetUserRooms(ctx context.Context, userID, adminID, cursor string, limit int) ([]models.RoomListItem, string, error) {
	baseQuery := `
		SELECT
			r.id, r.type, r.name,
			COALESCE(r.avatar_url, '') AS room_avatar_url,
			other_member.user_id AS other_user_id,
			other_user.name AS other_name,
			COALESCE(other_user.avatar_url, '') AS other_avatar_url,
			last_msg.id AS last_msg_id,
			last_msg.content AS last_msg_content,
			last_msg.sender_id AS last_msg_sender,
			last_msg.created_at AS last_msg_time,
			COALESCE(
				(SELECT COUNT(*) FROM messages m
				 WHERE m.room_id = r.id
				 AND m.created_at > COALESCE(rm.last_read_at, rm.joined_at)
				 AND m.sender_id != $1
				 AND m.deleted_at IS NULL), 0
			) AS unread_count,
			COALESCE(last_msg.created_at, r.updated_at) AS sort_time
		FROM room_members rm
		JOIN rooms r ON r.id = rm.room_id AND r.admin_id = $2
		LEFT JOIN room_members other_member ON other_member.room_id = r.id AND other_member.user_id != $1 AND r.type = 'dm'
		LEFT JOIN users other_user ON other_user.id = other_member.user_id
		LEFT JOIN LATERAL (
			SELECT id, content, sender_id, created_at FROM messages
			WHERE room_id = r.id AND deleted_at IS NULL
			ORDER BY created_at DESC LIMIT 1
		) last_msg ON true
		WHERE rm.user_id = $1 AND rm.status = 'active'`

	var rows pgx.Rows
	var err error

	if cursor == "" {
		rows, err = postgress.GetPool().Query(ctx, baseQuery+`
		ORDER BY sort_time DESC
		LIMIT $3
		`, userID, adminID, limit)
	} else {
		cursorTime, parseErr := time.Parse(time.RFC3339Nano, cursor)
		if parseErr != nil {
			return nil, "", parseErr
		}
		rows, err = postgress.GetPool().Query(ctx, baseQuery+`
		AND COALESCE(last_msg.created_at, r.updated_at) < $3
		ORDER BY sort_time DESC
		LIMIT $4
		`, userID, adminID, cursorTime, limit)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var items []models.RoomListItem
	var lastSortTime time.Time
	for rows.Next() {
		var item models.RoomListItem
		var lastMsgID, lastMsgContent, lastMsgSender *string
		var lastMsgTime *time.Time
		var sortTime time.Time
		var otherUserID, otherName, otherAvatarURL *string

		err := rows.Scan(
			&item.ID, &item.Type, &item.Name, &item.AvatarURL,
			&otherUserID, &otherName, &otherAvatarURL,
			&lastMsgID, &lastMsgContent, &lastMsgSender, &lastMsgTime,
			&item.UnreadCount, &sortTime,
		)
		if err != nil {
			return nil, "", err
		}

		if otherUserID != nil {
			item.OtherUserID = *otherUserID
		}
		if otherName != nil {
			item.OtherName = *otherName
		}
		if otherAvatarURL != nil {
			item.OtherAvatarURL = *otherAvatarURL
		}
		item.UpdatedAt = sortTime.Format(time.RFC3339)
		lastSortTime = sortTime

		if lastMsgID != nil {
			item.LastMessage = &models.Message{
				ID:        *lastMsgID,
				RoomID:    item.ID,
				Content:   lastMsgContent,
				SenderID:  *lastMsgSender,
				CreatedAt: *lastMsgTime,
			}
		}

		items = append(items, item)
	}

	var nextCursor string
	if len(items) == limit {
		nextCursor = lastSortTime.Format(time.RFC3339Nano)
	}

	return items, nextCursor, nil
}

// IsMember checks if a user is an active member of a room.
// Cached via redis.Fetch — safe for hot paths (WS membership checks, handler auth).
// Cache is keyed by room+user and lives for cacheTTL.
func (r *RoomRepo) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	return redis.Fetch(ctx, "room_member:"+roomID+":"+userID, r.cacheTTL, func(ctx context.Context) (bool, error) {
		var exists bool
		err := postgress.GetPool().QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM room_members
				WHERE room_id = $1 AND user_id = $2 AND status = 'active'
			)
		`, roomID, userID).Scan(&exists)
		return exists, err
	})
}

// SameOrg checks if two users belong to the same admin org.
// Cached via redis.Fetch — safe for hot paths (CanCall hook, DM creation).
// Key is canonicalized (smaller ID first) so SameOrg(a,b) == SameOrg(b,a).
func (r *RoomRepo) SameOrg(ctx context.Context, userID1, userID2 string) (bool, error) {
	// Canonical key: smaller ID first to avoid duplicate cache entries
	a, b := userID1, userID2
	if a > b {
		a, b = b, a
	}
	return redis.Fetch(ctx, "same_org:"+a+":"+b, r.cacheTTL, func(ctx context.Context) (bool, error) {
		var exists bool
		err := postgress.GetPool().QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM users u1
				JOIN users u2 ON (
					COALESCE(u1.admin_id, u1.id) = COALESCE(u2.admin_id, u2.id)
				)
				WHERE u1.id = $1 AND u2.id = $2 AND u1.is_active AND u2.is_active
			)
		`, userID1, userID2).Scan(&exists)
		return exists, err
	})
}

// UpdateLastRead sets last_read_at for a user in a room.
// Called from FlushRoomMessages when draining buffered read receipts.
func (r *RoomRepo) UpdateLastRead(ctx context.Context, roomID, userID string, ts time.Time) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE room_members SET last_read_at = $1
		WHERE room_id = $2 AND user_id = $3 AND status = 'active'
	`, ts, roomID, userID)
	if err == nil {
		redis.Remove(ctx, "group_members:"+roomID)
	}
	return err
}

// UpdateLastDelivered sets last_delivered_at for a user in a room.
// Called from FlushRoomMessages when draining buffered delivered receipts.
func (r *RoomRepo) UpdateLastDelivered(ctx context.Context, roomID, userID string, ts time.Time) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE room_members SET last_delivered_at = $1
		WHERE room_id = $2 AND user_id = $3 AND status = 'active'
	`, ts, roomID, userID)
	if err == nil {
		redis.Remove(ctx, "group_members:"+roomID)
	}
	return err
}

// ---------- Group Chat ----------

// CreateGroup creates a group room with the given members. The creator is
// automatically added as an admin member. Returns the created room.
func (r *RoomRepo) CreateGroup(ctx context.Context, adminID, creatorID, name string, memberIDs []string) (*models.Room, error) {
	var room models.Room
	err := postgress.GetPool().QueryRow(ctx, `
		INSERT INTO rooms (admin_id, type, name) VALUES ($1, 'group', $2)
		RETURNING id, admin_id, type, name, created_at, updated_at
	`, adminID, name).Scan(&room.ID, &room.AdminID, &room.Type, &room.Name, &room.CreatedAt, &room.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Add creator as admin + all members
	batch := &pgx.Batch{}
	batch.Queue(`INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, 'admin')`, room.ID, creatorID)
	for _, uid := range memberIDs {
		if uid != creatorID {
			batch.Queue(`INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, 'member')`, room.ID, uid)
		}
	}
	br := postgress.GetPool().SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return nil, err
		}
	}

	return &room, nil
}

// UpdateGroupName updates the name of a group room.
func (r *RoomRepo) UpdateGroupName(ctx context.Context, roomID, name string) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE rooms SET name = $1, updated_at = NOW()
		WHERE id = $2 AND type = 'group'
	`, name, roomID)
	return err
}

// UpdateGroupAvatar updates the avatar_url of a group room.
func (r *RoomRepo) UpdateGroupAvatar(ctx context.Context, roomID, avatarURL string) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE rooms SET avatar_url = $1, updated_at = NOW()
		WHERE id = $2 AND type = 'group'
	`, avatarURL, roomID)
	return err
}

// AddMembers adds users to a group room. Skips duplicates via ON CONFLICT.
// Invalidates group_members + room_role caches.
func (r *RoomRepo) AddMembers(ctx context.Context, roomID string, userIDs []string) error {
	batch := &pgx.Batch{}
	for _, uid := range userIDs {
		batch.Queue(`
			INSERT INTO room_members (room_id, user_id, role) VALUES ($1, $2, 'member')
			ON CONFLICT (room_id, user_id) DO UPDATE SET status = 'active', left_at = NULL
		`, roomID, uid)
	}
	br := postgress.GetPool().SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}

	// Invalidate caches
	redis.Remove(ctx, "group_members:"+roomID)
	for _, uid := range userIDs {
		redis.Remove(ctx, "room_role:"+roomID+":"+uid)
		redis.Remove(ctx, "room_member:"+roomID+":"+uid)
	}
	return nil
}

// RemoveMember soft-removes a user from a group room by setting status='left'.
// Invalidates group_members, room_role, and room_member caches.
func (r *RoomRepo) RemoveMember(ctx context.Context, roomID, userID string) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE room_members SET status = 'left', left_at = NOW()
		WHERE room_id = $1 AND user_id = $2 AND status = 'active'
	`, roomID, userID)
	if err != nil {
		return err
	}
	redis.Remove(ctx, "group_members:"+roomID)
	redis.Remove(ctx, "room_role:"+roomID+":"+userID)
	redis.Remove(ctx, "room_member:"+roomID+":"+userID)
	return nil
}

// GetMemberRole returns the role of a user in a room, or "" if not a member.
// Cached via redis.Fetch.
func (r *RoomRepo) GetMemberRole(ctx context.Context, roomID, userID string) (string, error) {
	return redis.Fetch(ctx, "room_role:"+roomID+":"+userID, r.cacheTTL, func(ctx context.Context) (string, error) {
		var role string
		err := postgress.GetPool().QueryRow(ctx, `
			SELECT role FROM room_members
			WHERE room_id = $1 AND user_id = $2 AND status = 'active'
		`, roomID, userID).Scan(&role)
		if err != nil {
			return "", err
		}
		return role, nil
	})
}

// GetRoomType returns the type of a room ("dm" or "group").
func (r *RoomRepo) GetRoomType(ctx context.Context, roomID string) (string, error) {
	room, err := r.GetByID(ctx, roomID)
	if err != nil {
		return "", err
	}
	return room.Type, nil
}

// PromoteOldestMember promotes the oldest active member to admin if no admins remain.
// Returns the promoted userID, or "" if no promotion was needed/possible.
// Invalidates room_role cache for the promoted user.
func (r *RoomRepo) PromoteOldestMember(ctx context.Context, roomID string) (string, error) {
	// Check if any admin still exists
	var hasAdmin bool
	err := postgress.GetPool().QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM room_members
			WHERE room_id = $1 AND role = 'admin' AND status = 'active'
		)
	`, roomID).Scan(&hasAdmin)
	if err != nil {
		return "", err
	}
	if hasAdmin {
		return "", nil
	}

	// Promote oldest member
	var promoted string
	err = postgress.GetPool().QueryRow(ctx, `
		UPDATE room_members SET role = 'admin'
		WHERE id = (
			SELECT id FROM room_members
			WHERE room_id = $1 AND status = 'active'
			ORDER BY joined_at ASC LIMIT 1
		)
		RETURNING user_id
	`, roomID).Scan(&promoted)
	if err != nil {
		return "", err
	}
	redis.Remove(ctx, "room_role:"+roomID+":"+promoted)
	redis.Remove(ctx, "group_members:"+roomID)
	return promoted, nil
}

// UpdateMemberRole sets the role of a member in a room.
// Invalidates room_role and group_members caches.
func (r *RoomRepo) UpdateMemberRole(ctx context.Context, roomID, userID, role string) error {
	tag, err := postgress.GetPool().Exec(ctx, `
		UPDATE room_members SET role = $3
		WHERE room_id = $1 AND user_id = $2 AND status = 'active'
	`, roomID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("member not found")
	}
	redis.Remove(ctx, "room_role:"+roomID+":"+userID)
	redis.Remove(ctx, "group_members:"+roomID)
	return nil
}

// GetGroupMembers returns active members of a group room with user names.
// Cached via redis.Fetch — safe for repeated member list renders.
func (r *RoomRepo) GetGroupMembers(ctx context.Context, roomID string) ([]models.RoomMember, error) {
	return redis.Fetch(ctx, "group_members:"+roomID, r.cacheTTL, func(ctx context.Context) ([]models.RoomMember, error) {
		rows, err := postgress.GetPool().Query(ctx, `
			SELECT rm.id, rm.room_id, rm.user_id, rm.role, rm.status, rm.joined_at,
			       rm.left_at, rm.last_read_at, rm.last_delivered_at,
			       COALESCE(u.name, '') AS user_name, COALESCE(u.email, '') AS user_email,
			       COALESCE(u.avatar_url, '') AS user_avatar_url
			FROM room_members rm
			JOIN users u ON u.id = rm.user_id
			WHERE rm.room_id = $1 AND rm.status = 'active'
			ORDER BY rm.joined_at ASC
		`, roomID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var members []models.RoomMember
		for rows.Next() {
			var m models.RoomMember
			if err := rows.Scan(&m.ID, &m.RoomID, &m.UserID, &m.Role, &m.Status,
				&m.JoinedAt, &m.LeftAt, &m.LastReadAt, &m.LastDeliveredAt,
				&m.UserName, &m.UserEmail, &m.UserAvatarURL); err != nil {
				return nil, err
			}
			members = append(members, m)
		}
		return members, nil
	})
}

// GetUserName returns the display name of a user by ID.
func (r *RoomRepo) GetUserName(ctx context.Context, userID string) string {
	var name string
	_ = postgress.GetPool().QueryRow(ctx, `SELECT COALESCE(name,'') FROM users WHERE id = $1`, userID).Scan(&name)
	return name
}
