package service

import (
	"net"
	"net/smtp"
	"regexp"
	"strings"
	"sync"
	"time"

	"brc-connect-backend/api/data"
)

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

var junkEmails = map[string]bool{
	"test@test.com":       true,
	"admin@example.com":   true,
	"noreply@example.com": true,
	"info@example.com":    true,
}

// smtpLimiter rate-limits SMTP checks to 1 per second per domain.
var smtpLimiter = &domainLimiter{
	lastCheck: make(map[string]time.Time),
}

type domainLimiter struct {
	mu        sync.Mutex
	lastCheck map[string]time.Time
}

func (dl *domainLimiter) Wait(domain string) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if last, ok := dl.lastCheck[domain]; ok {
		elapsed := time.Since(last)
		if elapsed < time.Second {
			time.Sleep(time.Second - elapsed)
		}
	}
	dl.lastCheck[domain] = time.Now()
}

// ValidateEmailFormat checks RFC-like format and rejects obvious junk.
func ValidateEmailFormat(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return false
	}
	if !emailRe.MatchString(email) {
		return false
	}
	if junkEmails[email] {
		return false
	}
	if strings.HasPrefix(email, "noreply@") || strings.HasPrefix(email, "no-reply@") {
		return false
	}
	return true
}

// CheckMX looks up MX records for the email domain.
func CheckMX(email string) bool {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false
	}
	records, err := net.LookupMX(parts[1])
	return err == nil && len(records) > 0
}

// CheckSMTP does a basic SMTP handshake to verify the mailbox exists.
// Rate-limited to max 1 check per second per domain.
// Returns (mailboxExists, isCatchAll).
func CheckSMTP(email string) (bool, bool) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false, false
	}
	domain := parts[1]

	// Rate limit: max 1 SMTP check per second per domain
	smtpLimiter.Wait(domain)

	records, err := net.LookupMX(domain)
	if err != nil || len(records) == 0 {
		return false, false
	}

	mx := records[0].Host
	conn, err := net.DialTimeout("tcp", mx+":25", 5*time.Second)
	if err != nil {
		return false, false
	}
	defer conn.Close()

	// Set deadline for the entire SMTP conversation
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	client, err := smtp.NewClient(conn, mx)
	if err != nil {
		return false, false
	}
	defer client.Quit()

	if err := client.Hello("verify.local"); err != nil {
		return false, false
	}
	if err := client.Mail("verify@verify.local"); err != nil {
		return false, false
	}

	// Check the real address
	realErr := client.Rcpt(email)
	// Check a fake address to detect catch-all
	fakeErr := client.Rcpt("nonexistent-xyzzy-9999@" + domain)

	mailboxExists := realErr == nil
	catchAll := fakeErr == nil // if fake succeeds, it's catch-all

	return mailboxExists, catchAll
}

// IsDisposable checks if the email domain is a known disposable provider.
func IsDisposable(email string) bool {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false
	}
	return data.DisposableDomains[strings.ToLower(parts[1])]
}

// EmailConfidence calculates a confidence score for the email.
func EmailConfidence(formatValid, mxValid, smtpValid, disposable, catchAll bool) int {
	score := 0
	if formatValid {
		score += 20
	}
	if mxValid {
		score += 30
	}
	if smtpValid {
		score += 30
	}
	if !disposable {
		score += 10
	}
	if !catchAll {
		score += 10
	}
	return score
}
