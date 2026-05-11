package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shivanand-burli/go-starter-kit/postgress"

	"brc-connect-backend/api/models"
)

type MessageRepo struct{}

func NewMessageRepo() *MessageRepo {
	return &MessageRepo{}
}

// GetByRoom returns messages for a room with cursor-based pagination.
// Cursor is the created_at of the last message (ISO 8601). Empty = latest.
func (r *MessageRepo) GetByRoom(ctx context.Context, roomID, cursor string, limit int) ([]models.Message, string, error) {
	var rows pgx.Rows
	var err error

	if cursor == "" {
		rows, err = postgress.GetPool().Query(ctx, `
			SELECT m.id, m.room_id, m.sender_id, m.content, m.media_url, m.media_type,
			       m.reply_to, m.edited_at, m.deleted_at, m.created_at, u.name, COALESCE(u.avatar_url, '')
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.room_id = $1 AND m.deleted_at IS NULL
			ORDER BY m.created_at DESC
			LIMIT $2
		`, roomID, limit)
	} else {
		cursorTime, parseErr := time.Parse(time.RFC3339Nano, cursor)
		if parseErr != nil {
			return nil, "", parseErr
		}
		rows, err = postgress.GetPool().Query(ctx, `
			SELECT m.id, m.room_id, m.sender_id, m.content, m.media_url, m.media_type,
			       m.reply_to, m.edited_at, m.deleted_at, m.created_at, u.name, COALESCE(u.avatar_url, '')
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.room_id = $1 AND m.deleted_at IS NULL AND m.created_at < $2
			ORDER BY m.created_at DESC
			LIMIT $3
		`, roomID, cursorTime, limit)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(
			&msg.ID, &msg.RoomID, &msg.SenderID, &msg.Content, &msg.MediaURL, &msg.MediaType,
			&msg.ReplyTo, &msg.EditedAt, &msg.DeletedAt, &msg.CreatedAt, &msg.SenderName, &msg.SenderAvatarURL,
		); err != nil {
			return nil, "", err
		}
		messages = append(messages, msg)
	}

	var nextCursor string
	if len(messages) == limit {
		nextCursor = messages[len(messages)-1].CreatedAt.Format(time.RFC3339Nano)
	}

	return messages, nextCursor, nil
}

// InsertBatch inserts a batch of raw message bytes (from the flusher).
// Each message is a protobuf-encoded Envelope that needs to be decoded
// and persisted. The service layer handles the decoding.
func (r *MessageRepo) InsertBatch(ctx context.Context, messages []models.Message) error {
	if len(messages) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, msg := range messages {
		batch.Queue(`
			INSERT INTO messages (id, room_id, sender_id, content, media_url, media_type, reply_to, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO NOTHING
		`, msg.ID, msg.RoomID, msg.SenderID, msg.Content, msg.MediaURL, msg.MediaType, msg.ReplyTo, msg.CreatedAt)
	}

	br := postgress.GetPool().SendBatch(ctx, batch)
	defer br.Close()
	for range messages {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// SearchMessages searches messages across all rooms the user is a member of.
// Uses ILIKE for case-insensitive substring matching. Returns newest first, limited.
func (r *MessageRepo) SearchMessages(ctx context.Context, userID, query string, limit int) ([]models.MessageSearchResult, error) {
	pattern := "%" + query + "%"
	rows, err := postgress.GetPool().Query(ctx, `
		SELECT m.id, m.room_id, m.sender_id, m.content, m.created_at,
		       u.name AS sender_name,
		       CASE WHEN rm.type = 'dm' THEN COALESCE(other_u.name, '') ELSE COALESCE(rm.name, '') END AS room_name
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		JOIN rooms rm ON rm.id = m.room_id
		JOIN room_members rmb ON rmb.room_id = m.room_id AND rmb.user_id = $1 AND rmb.status = 'active'
		LEFT JOIN room_members other_rmb ON other_rmb.room_id = m.room_id AND other_rmb.user_id != $1 AND other_rmb.status = 'active' AND rm.type = 'dm'
		LEFT JOIN users other_u ON other_u.id = other_rmb.user_id
		WHERE m.deleted_at IS NULL AND m.content ILIKE $2
		ORDER BY m.created_at DESC
		LIMIT $3
	`, userID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.MessageSearchResult
	for rows.Next() {
		var r models.MessageSearchResult
		if err := rows.Scan(&r.ID, &r.RoomID, &r.SenderID, &r.Content, &r.CreatedAt, &r.SenderName, &r.RoomName); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// UpdateMessage updates a message's content (for edit).
func (r *MessageRepo) UpdateMessage(ctx context.Context, msgID, senderID, content string) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE messages SET content = $1, edited_at = NOW()
		WHERE id = $2 AND sender_id = $3 AND deleted_at IS NULL
	`, content, msgID, senderID)
	return err
}

// SoftDeleteMessage soft-deletes a message.
func (r *MessageRepo) SoftDeleteMessage(ctx context.Context, msgID, senderID string) error {
	_, err := postgress.GetPool().Exec(ctx, `
		UPDATE messages SET deleted_at = NOW()
		WHERE id = $1 AND sender_id = $2 AND deleted_at IS NULL
	`, msgID, senderID)
	return err
}

// GetCallHistory returns paginated call history for a user.
func (r *MessageRepo) GetCallHistory(ctx context.Context, userID, cursor string, limit int) ([]models.CallLog, string, error) {
	var args []any
	query := `
		SELECT cl.id, cl.call_id, cl.room_id, cl.initiated_by,
		       CASE WHEN cl.peer_id IS NOT NULL
		            THEN CASE WHEN cl.initiated_by = $1 THEN cl.peer_id ELSE cl.initiated_by END
		            ELSE NULL END AS peer_id,
		       cl.call_type, cl.status, cl.started_at, cl.ended_at,
		       cl.duration_seconds, cl.created_at,
		       COALESCE(
		         u.name,
		         r.name,
		         ''
		       ) AS peer_name
		FROM call_logs cl
		LEFT JOIN users u ON u.id = CASE WHEN cl.peer_id IS NOT NULL
		    THEN CASE WHEN cl.initiated_by = $1 THEN cl.peer_id ELSE cl.initiated_by END
		    ELSE NULL END
		LEFT JOIN rooms r ON r.id = cl.room_id
		WHERE (cl.initiated_by = $1 OR cl.peer_id = $1
		       OR cl.room_id IN (SELECT room_id FROM room_members WHERE user_id = $1 AND status = 'active'))
	`
	args = append(args, userID)

	if cursor != "" {
		cursorTime, err := time.Parse(time.RFC3339Nano, cursor)
		if err != nil {
			return nil, "", err
		}
		query += ` AND cl.created_at < $2`
		args = append(args, cursorTime)
	}

	query += ` ORDER BY cl.created_at DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)

	rows, err := postgress.GetPool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var calls []models.CallLog
	for rows.Next() {
		var cl models.CallLog
		if err := rows.Scan(
			&cl.ID, &cl.CallID, &cl.RoomID, &cl.InitiatedBy, &cl.PeerID,
			&cl.CallType, &cl.Status, &cl.StartedAt, &cl.EndedAt,
			&cl.DurationSeconds, &cl.CreatedAt, &cl.PeerName,
		); err != nil {
			return nil, "", err
		}
		calls = append(calls, cl)
	}

	var nextCursor string
	if len(calls) == limit {
		nextCursor = calls[len(calls)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return calls, nextCursor, nil
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// PurgeOldMessages hard-deletes messages older than olderThan in batches.
// Returns the total count deleted and all non-nil media_url values for S3 cleanup.
func (r *MessageRepo) PurgeOldMessages(ctx context.Context, olderThan time.Time, batchSize int) (int64, []string, error) {
	var totalDeleted int64
	var mediaURLs []string

	for {
		// Collect media URLs from this batch before deleting
		rows, err := postgress.GetPool().Query(ctx, `
			SELECT media_url FROM messages
			WHERE created_at < $1 AND media_url IS NOT NULL
			LIMIT $2
		`, olderThan, batchSize)
		if err != nil {
			return totalDeleted, mediaURLs, err
		}
		for rows.Next() {
			var url string
			if err := rows.Scan(&url); err != nil {
				rows.Close()
				return totalDeleted, mediaURLs, err
			}
			mediaURLs = append(mediaURLs, url)
		}
		rows.Close()

		// Hard-delete the batch
		n, err := postgress.Exec(ctx, `
			DELETE FROM messages
			WHERE id IN (
				SELECT id FROM messages
				WHERE created_at < $1
				ORDER BY created_at ASC
				LIMIT $2
			)
		`, olderThan, batchSize)
		if err != nil {
			return totalDeleted, mediaURLs, err
		}

		totalDeleted += n

		// If fewer than batchSize were deleted, we're done
		if n < int64(batchSize) {
			break
		}
	}

	return totalDeleted, mediaURLs, nil
}
