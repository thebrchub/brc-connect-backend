package models

import "time"

type EmployeeSession struct {
	ID           string    `db:"id" json:"id"`
	EmployeeID   string    `db:"employee_id" json:"employee_id"`
	StartedAt    time.Time `db:"started_at" json:"started_at"`
	LastActiveAt time.Time `db:"last_active_at" json:"last_active_at"`
	ActionsCount int       `db:"actions_count" json:"actions_count"`
	IPAddress    *string   `db:"ip_address" json:"ip_address,omitempty"`
}
