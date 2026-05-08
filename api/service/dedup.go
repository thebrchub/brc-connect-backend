package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/shivanand-burli/go-starter-kit/postgress"

	"brc-connect-backend/api/repository"
)

type DedupService struct {
	leadRepo *repository.LeadRepo
}

func NewDedupService(leadRepo *repository.LeadRepo) *DedupService {
	return &DedupService{leadRepo: leadRepo}
}

// FindDuplicate checks phone, email, and domain for an existing lead in a single query.
// Scoped to adminID for multi-tenant isolation.
// Returns the existing lead ID if found, empty string otherwise.
func (d *DedupService) FindDuplicate(ctx context.Context, adminID, phone, email, websiteURL string) (string, error) {
	domain := ExtractDomain(websiteURL)

	// Skip if nothing to check
	if phone == "" && email == "" && domain == "" {
		return "", nil
	}

	// Single query with OR conditions — replaces 3 sequential queries
	conditions := []string{}
	args := []any{}
	argIdx := 1

	// Scope to admin for multi-tenant isolation
	adminFilter := ""
	if adminID != "" {
		adminFilter = fmt.Sprintf("admin_id = $%d AND ", argIdx)
		args = append(args, adminID)
		argIdx++
	}

	if phone != "" {
		conditions = append(conditions, fmt.Sprintf("phone_e164 = $%d", argIdx))
		args = append(args, phone)
		argIdx++
	}
	if email != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(email) = LOWER($%d)", argIdx))
		args = append(args, email)
		argIdx++
	}
	if domain != "" {
		conditions = append(conditions, fmt.Sprintf("website_domain = $%d", argIdx))
		args = append(args, domain)
		argIdx++
	}

	sql := fmt.Sprintf("SELECT id FROM leads WHERE %s(%s) LIMIT 1", adminFilter, strings.Join(conditions, " OR "))
	type idRow struct {
		ID string `db:"id"`
	}
	rows, err := postgress.Query[idRow](ctx, sql, args...)
	if err != nil {
		return "", err
	}
	if len(rows) > 0 {
		return rows[0].ID, nil
	}
	return "", nil
}

// ExtractDomain strips scheme and www from a URL to get the bare domain.
func ExtractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	rawURL = strings.TrimSpace(rawURL)
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return host
}
