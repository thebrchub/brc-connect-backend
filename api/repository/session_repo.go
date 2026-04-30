package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"
)

type SessionRepo struct{}

func NewSessionRepo() *SessionRepo { return &SessionRepo{} }

// Heartbeat records a heartbeat in Redis (zero DB writes).
// Returns the session ID (creates a new one if sessionID is nil or empty).
func (r *SessionRepo) Heartbeat(ctx context.Context, employeeID string, sessionID *string, ip string) (string, error) {
	client := redis.GetRawClient()
	now := time.Now().UTC()

	var sid string
	if sessionID == nil || *sessionID == "" {
		sid = uuid.NewString()
		// New session — store start time
		sessionKey := fmt.Sprintf("session:%s", sid)
		client.HSet(ctx, sessionKey, map[string]any{
			"employee_id":    employeeID,
			"started_at":     now.Format(time.RFC3339),
			"last_active_at": now.Format(time.RFC3339),
			"actions_count":  0,
			"ip_address":     ip,
		})
		client.Expire(ctx, sessionKey, 10*time.Minute)
	} else {
		sid = *sessionID
		sessionKey := fmt.Sprintf("session:%s", sid)
		client.HSet(ctx, sessionKey, "last_active_at", now.Format(time.RFC3339))
		client.HIncrBy(ctx, sessionKey, "actions_count", 1)
		client.Expire(ctx, sessionKey, 10*time.Minute)
	}

	// Track active session for today
	dateKey := fmt.Sprintf("active_sessions:%s", now.Format("2006-01-02"))
	client.SAdd(ctx, dateKey, sid)
	client.Expire(ctx, dateKey, 48*time.Hour)

	return sid, nil
}

// FlushSessionsToDB scans Redis for active sessions and batch-upserts to Postgres.
// Called by cron every 5 minutes.
func (r *SessionRepo) FlushSessionsToDB(ctx context.Context) error {
	client := redis.GetRawClient()
	iter := client.Scan(ctx, 0, "session:*", 200).Iterator()

	for iter.Next(ctx) {
		key := iter.Val()
		vals, err := client.HGetAll(ctx, key).Result()
		if err != nil || len(vals) == 0 {
			continue
		}

		sid := key[len("session:"):]
		employeeID := vals["employee_id"]
		startedAt := vals["started_at"]
		lastActiveAt := vals["last_active_at"]
		actionsCount, _ := strconv.Atoi(vals["actions_count"])
		ipAddress := vals["ip_address"]

		_, err = postgress.Exec(ctx,
			`INSERT INTO employee_sessions (id, employee_id, started_at, last_active_at, actions_count, ip_address)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (id) DO UPDATE SET
				last_active_at = EXCLUDED.last_active_at,
				actions_count = EXCLUDED.actions_count`,
			sid, employeeID, startedAt, lastActiveAt, actionsCount, ipAddress)
		if err != nil {
			return err
		}
	}

	return nil
}

// EngagementStats holds engagement metrics for an employee.
type EngagementStats struct {
	EmployeeID       string  `json:"employee_id"`
	EmployeeName     string  `json:"employee_name"`
	TimeTodayMinutes float64 `json:"time_today_minutes"`
	TimeWeekMinutes  float64 `json:"time_this_week_minutes"`
	AvgDailyMinutes  float64 `json:"avg_daily_minutes"`
	ActionsPerHour   float64 `json:"actions_per_hour"`
	LastSeen         *string `json:"last_seen"`
	IdleDays         int     `json:"idle_days"`
	DailyStreak      int     `json:"daily_streak"`
	Status           string  `json:"status"` // online | active_today | inactive
}

// GetEngagement returns engagement stats for a single employee.
func (r *SessionRepo) GetEngagement(ctx context.Context, employeeID string) (*EngagementStats, error) {
	cacheKey := fmt.Sprintf("crm:engagement:%s", employeeID)

	stats, err := redis.Fetch(ctx, cacheKey, 120*time.Second, func(ctx context.Context) (*EngagementStats, error) {
		type dbRow struct {
			TimeTodayMin float64 `db:"time_today_min"`
			TimeWeekMin  float64 `db:"time_week_min"`
			DaysActive   int     `db:"days_active"`
			TotalActions int     `db:"total_actions"`
			TotalHours   float64 `db:"total_hours"`
			LastSeen     *string `db:"last_seen"`
		}

		rows, err := postgress.Query[dbRow](ctx,
			`SELECT
				COALESCE(SUM(EXTRACT(EPOCH FROM (last_active_at - started_at)) / 60)
					FILTER (WHERE started_at::date = CURRENT_DATE), 0) AS time_today_min,
				COALESCE(SUM(EXTRACT(EPOCH FROM (last_active_at - started_at)) / 60)
					FILTER (WHERE started_at > NOW() - INTERVAL '7 days'), 0) AS time_week_min,
				COUNT(DISTINCT started_at::date)
					FILTER (WHERE started_at > NOW() - INTERVAL '7 days') AS days_active,
				COALESCE(SUM(actions_count)
					FILTER (WHERE started_at > NOW() - INTERVAL '7 days'), 0) AS total_actions,
				COALESCE(SUM(EXTRACT(EPOCH FROM (last_active_at - started_at)) / 3600)
					FILTER (WHERE started_at > NOW() - INTERVAL '7 days'), 0) AS total_hours,
				TO_CHAR(MAX(last_active_at), 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS last_seen
			FROM employee_sessions
			WHERE employee_id = $1`, employeeID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return &EngagementStats{EmployeeID: employeeID, Status: "inactive"}, nil
		}

		row := rows[0]
		avgDaily := 0.0
		if row.DaysActive > 0 {
			avgDaily = row.TimeWeekMin / float64(row.DaysActive)
		}
		actionsPerHour := 0.0
		if row.TotalHours > 0 {
			actionsPerHour = float64(row.TotalActions) / row.TotalHours
		}

		// Determine status from Redis (real-time)
		status := "inactive"
		client := redis.GetRawClient()
		// Check if any active session in Redis
		iter := client.Scan(ctx, 0, "session:*", 200).Iterator()
		for iter.Next(ctx) {
			eid, _ := client.HGet(ctx, iter.Val(), "employee_id").Result()
			if eid == employeeID {
				lastActive, _ := client.HGet(ctx, iter.Val(), "last_active_at").Result()
				if t, err := time.Parse(time.RFC3339, lastActive); err == nil {
					if time.Since(t) < 2*time.Minute {
						status = "online"
					} else {
						status = "active_today"
					}
				}
				break
			}
		}

		return &EngagementStats{
			EmployeeID:       employeeID,
			TimeTodayMinutes: row.TimeTodayMin,
			TimeWeekMinutes:  row.TimeWeekMin,
			AvgDailyMinutes:  avgDaily,
			ActionsPerHour:   actionsPerHour,
			LastSeen:         row.LastSeen,
			Status:           status,
		}, nil
	})
	return stats, err
}

// GetDashboardEngagement returns engagement for all employees under an admin.
func (r *SessionRepo) GetDashboardEngagement(ctx context.Context, adminID string) ([]EngagementStats, error) {
	cacheKey := fmt.Sprintf("crm:engagement:dashboard:%s", adminID)

	type result struct {
		Stats []EngagementStats `json:"stats"`
	}

	res, err := redis.Fetch(ctx, cacheKey, 120*time.Second, func(ctx context.Context) (*result, error) {
		type empRow struct {
			ID   string `db:"id"`
			Name string `db:"name"`
		}
		employees, err := postgress.Query[empRow](ctx,
			"SELECT id, name FROM users WHERE admin_id = $1 AND role = 'employee' AND is_active = true", adminID)
		if err != nil {
			return nil, err
		}

		stats := make([]EngagementStats, 0, len(employees))
		for _, emp := range employees {
			s, err := r.GetEngagement(ctx, emp.ID)
			if err != nil {
				continue
			}
			s.EmployeeName = emp.Name
			stats = append(stats, *s)
		}
		return &result{Stats: stats}, nil
	})
	if err != nil {
		return nil, err
	}
	return res.Stats, nil
}
