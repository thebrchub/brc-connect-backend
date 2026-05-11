package repository

import (
	"context"
	"time"

	"github.com/shivanand-burli/go-starter-kit/postgress"
)

// CallRepo handles call log persistence. Methods are called from
// the kit's OnFlushCallEvents hook (cron context, not WS hot path).
type CallRepo struct{}

// InsertCallLog creates a new call log entry.
func (r *CallRepo) InsertCallLog(ctx context.Context, callID, callerID, calleeID, callType, status string) error {
	_, err := postgress.Exec(ctx,
		`INSERT INTO call_logs (call_id, initiated_by, peer_id, call_type, status)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (call_id) DO NOTHING`,
		callID, callerID, calleeID, callType, status,
	)
	return err
}

// MarkAnswered sets the call status to answered and records started_at.
func (r *CallRepo) MarkAnswered(ctx context.Context, callID string, startedAt time.Time) error {
	_, err := postgress.Exec(ctx,
		`UPDATE call_logs SET status = 'answered', started_at = $1 WHERE call_id = $2`,
		startedAt, callID,
	)
	return err
}

// MarkCompleted sets the call status to completed with ended_at and duration.
func (r *CallRepo) MarkCompleted(ctx context.Context, callID string, duration int) error {
	_, err := postgress.Exec(ctx,
		`UPDATE call_logs SET status = 'completed', ended_at = NOW(), duration_seconds = $1
		 WHERE call_id = $2`,
		duration, callID,
	)
	return err
}

// MarkTerminal sets the call to a terminal status (missed, rejected).
func (r *CallRepo) MarkTerminal(ctx context.Context, callID, status string) error {
	_, err := postgress.Exec(ctx,
		`UPDATE call_logs SET status = $1, ended_at = NOW() WHERE call_id = $2`,
		status, callID,
	)
	return err
}

// PurgeOldCallLogs hard-deletes call logs older than olderThan in batches.
func (r *CallRepo) PurgeOldCallLogs(ctx context.Context, olderThan time.Time, batchSize int) (int64, error) {
	var totalDeleted int64
	for {
		n, err := postgress.Exec(ctx, `
			DELETE FROM call_logs
			WHERE id IN (
				SELECT id FROM call_logs
				WHERE created_at < $1
				ORDER BY created_at ASC
				LIMIT $2
			)
		`, olderThan, batchSize)
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += n
		if n < int64(batchSize) {
			break
		}
	}
	return totalDeleted, nil
}

// InsertGroupCallLog creates a group call log entry (peer_id is NULL, room_id is set).
func (r *CallRepo) InsertGroupCallLog(ctx context.Context, callID, roomID, startedBy string) error {
	_, err := postgress.Exec(ctx,
		`INSERT INTO call_logs (call_id, room_id, initiated_by, call_type, status)
		 VALUES ($1, $2, $3, 'group_audio', 'ringing')
		 ON CONFLICT (call_id) DO NOTHING`,
		callID, roomID, startedBy,
	)
	return err
}

// MarkGroupCallCompleted updates a group call log to completed with duration computed from created_at.
func (r *CallRepo) MarkGroupCallCompleted(ctx context.Context, callID string) error {
	_, err := postgress.Exec(ctx,
		`UPDATE call_logs SET status = 'completed', ended_at = NOW(),
		 started_at = COALESCE(started_at, created_at),
		 duration_seconds = EXTRACT(EPOCH FROM (NOW() - COALESCE(started_at, created_at)))::int
		 WHERE call_id = $1`,
		callID,
	)
	return err
}
