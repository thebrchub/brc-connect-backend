package models

import "time"

type Campaign struct {
	ID            string    `db:"id" json:"id"`
	Name          string    `db:"name" json:"name"`
	Sources       []string  `db:"sources" json:"sources"`
	Cities        []string  `db:"cities" json:"cities"`
	Categories    []string  `db:"categories" json:"categories"`
	Status        string    `db:"status" json:"status"`
	AutoRescrape  bool      `db:"auto_rescrape" json:"auto_rescrape"`
	DropNoContact bool      `db:"drop_no_contact" json:"drop_no_contact"`
	JobsTotal     int       `db:"jobs_total" json:"jobs_total"`
	JobsCompleted int       `db:"jobs_completed" json:"jobs_completed"`
	LeadsFound    int       `db:"leads_found" json:"leads_found"`
	AdminID       *string   `db:"admin_id" json:"admin_id"`
	AssignedTo    *string   `db:"assigned_to" json:"assigned_to"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
}
