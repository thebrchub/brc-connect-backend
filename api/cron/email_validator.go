package cron

import (
	"context"
	"log"

	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/service"
)

// EmailValidator runs as a cron job to perform heavy MX/SMTP validation
// on leads that only have format-level validation.
type EmailValidator struct{}

func NewEmailValidator() *EmailValidator {
	return &EmailValidator{}
}

type pendingEmail struct {
	ID    string  `db:"id"`
	Email *string `db:"email"`
}

func (e *EmailValidator) Run(ctx context.Context) {
	// Fetch leads with emails that haven't been MX-validated yet (confidence <= 30)
	leads, err := postgress.Query[pendingEmail](ctx,
		`SELECT id, email FROM leads 
		 WHERE email IS NOT NULL AND email != '' 
		 AND email_confidence <= 30 AND email_valid = true
		 LIMIT 100`)
	if err != nil {
		log.Printf("ERROR [email-validator] - query pending emails failed error=%s", err)
		return
	}

	for _, lead := range leads {
		if lead.Email == nil || *lead.Email == "" {
			continue
		}

		email := *lead.Email
		mxOK := service.CheckMX(email)
		smtpOK := false
		catchAll := false

		if mxOK {
			smtpOK, catchAll = service.CheckSMTP(email)
		}

		conf := 20 // format already verified
		if !service.IsDisposable(email) {
			conf += 10
		}
		if mxOK {
			conf += 30
		}
		if smtpOK {
			conf += 30
		}
		if !catchAll {
			conf += 10
		}
		if conf > 100 {
			conf = 100
		}

		valid := mxOK

		_, err := postgress.Exec(ctx,
			`UPDATE leads SET email_valid = $1, email_catchall = $2, email_confidence = $3, updated_at = NOW() WHERE id = $4`,
			valid, catchAll, conf, lead.ID)
		if err != nil {
			log.Printf("ERROR [email-validator] - update lead=%s failed error=%s", lead.ID, err)
		} else {
			redis.Remove(ctx, "lead:"+lead.ID)
		}
	}
}
