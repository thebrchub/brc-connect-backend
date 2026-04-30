package models

import "time"

type LeadActivity struct {
	ID           string     `db:"id" json:"id"`
	LeadID       string     `db:"lead_id" json:"lead_id"`
	EmployeeID   string     `db:"employee_id" json:"employee_id"`
	CampaignID   string     `db:"campaign_id" json:"campaign_id"`
	Status       string     `db:"status" json:"status"`
	Notes        *string    `db:"notes" json:"notes"`
	NextAction   *string    `db:"next_action" json:"next_action"`
	NextFollowUp *time.Time `db:"next_follow_up" json:"next_follow_up"`
	LastContact  *time.Time `db:"last_contact" json:"last_contact"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}
