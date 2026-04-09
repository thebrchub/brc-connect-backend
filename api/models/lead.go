package models

import (
	"time"

	json "github.com/goccy/go-json"
)

type Lead struct {
	ID               string          `db:"id" json:"id"`
	BusinessName     string          `db:"business_name" json:"business_name"`
	Category         string          `db:"category" json:"category,omitempty"`
	PhoneE164        *string         `db:"phone_e164" json:"phone_e164,omitempty"`
	PhoneValid       bool            `db:"phone_valid" json:"phone_valid"`
	PhoneType        *string         `db:"phone_type" json:"phone_type,omitempty"`
	PhoneConfidence  int             `db:"phone_confidence" json:"phone_confidence"`
	Email            *string         `db:"email" json:"email,omitempty"`
	EmailValid       bool            `db:"email_valid" json:"email_valid"`
	EmailCatchall    bool            `db:"email_catchall" json:"email_catchall"`
	EmailDisposable  bool            `db:"email_disposable" json:"email_disposable"`
	EmailConfidence  int             `db:"email_confidence" json:"email_confidence"`
	WebsiteURL       *string         `db:"website_url" json:"website_url,omitempty"`
	WebsiteDomain    *string         `db:"website_domain" json:"website_domain,omitempty"`
	Address          *string         `db:"address" json:"address,omitempty"`
	City             string          `db:"city" json:"city"`
	Country          string          `db:"country" json:"country,omitempty"`
	Source           []string        `db:"source" json:"source"`
	SourceURLs       json.RawMessage `db:"source_urls" json:"source_urls,omitempty"`
	LeadScore        int             `db:"lead_score" json:"lead_score"`
	TechStack        json.RawMessage `db:"tech_stack" json:"tech_stack,omitempty"`
	HasSSL           *bool           `db:"has_ssl" json:"has_ssl,omitempty"`
	IsMobileFriendly *bool           `db:"is_mobile_friendly" json:"is_mobile_friendly,omitempty"`
	Status           string          `db:"status" json:"status"`
	CreatedAt        time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at" json:"updated_at"`
}

// RawLead is the shape Node.js POSTs — source-independent contract.
type RawLead struct {
	BusinessName     string          `json:"business_name"`
	Phone            *string         `json:"phone"`
	Email            *string         `json:"email"`
	WebsiteURL       *string         `json:"website_url"`
	Address          *string         `json:"address"`
	City             string          `json:"city"`
	Country          string          `json:"country"`
	Category         string          `json:"category"`
	Source           string          `json:"source"`
	SourceURL        *string         `json:"source_url,omitempty"`
	TechStack        json.RawMessage `json:"tech_stack,omitempty"`
	HasSSL           *bool           `json:"has_ssl"`
	IsMobileFriendly *bool           `json:"is_mobile_friendly"`
}

// LeadBatchRequest is the body of POST /internal/leads/batch.
type LeadBatchRequest struct {
	JobID string    `json:"job_id"`
	Leads []RawLead `json:"leads"`
}

// LeadBatchResponse is the response of POST /internal/leads/batch.
type LeadBatchResponse struct {
	Inserted int `json:"inserted"`
	Merged   int `json:"merged"`
	Skipped  int `json:"skipped"`
}

// JobStatusRequest is the body of POST /internal/jobs/{id}/status.
type JobStatusRequest struct {
	Status     string `json:"status"`
	LeadsFound int    `json:"leads_found"`
	Error      string `json:"error,omitempty"`
}
