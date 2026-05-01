package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/models"
)

type LeadRepo struct {
	leadTTL   time.Duration
	filterTTL time.Duration
}

func NewLeadRepo(leadTTL, filterTTL time.Duration) *LeadRepo {
	return &LeadRepo{leadTTL: leadTTL, filterTTL: filterTTL}
}

func (r *LeadRepo) Insert(ctx context.Context, lead models.Lead) (string, error) {
	lead.ID = uuid.NewString()
	_, err := postgress.Exec(ctx, leadInsertSQL,
		lead.ID, lead.AdminID, lead.BusinessName, lead.Category, lead.PhoneE164, lead.PhoneValid, lead.PhoneType, lead.PhoneConfidence,
		lead.Email, lead.EmailValid, lead.EmailCatchall, lead.EmailDisposable, lead.EmailConfidence,
		lead.WebsiteURL, lead.WebsiteDomain, lead.Address, lead.City, lead.Country, lead.Source, lead.SourceURLs,
		lead.LeadScore, lead.TechStack, lead.HasSSL, lead.IsMobileFriendly, lead.Status, lead.AssignedTo)
	if err == nil {
		r.invalidateFilterCache(ctx)
	}
	return lead.ID, err
}

func (r *LeadRepo) InsertBatch(ctx context.Context, leads []models.Lead) error {
	for i := range leads {
		leads[i].ID = uuid.NewString()
		_, err := postgress.Exec(ctx, leadInsertSQL,
			leads[i].ID, leads[i].AdminID, leads[i].BusinessName, leads[i].Category, leads[i].PhoneE164, leads[i].PhoneValid, leads[i].PhoneType, leads[i].PhoneConfidence,
			leads[i].Email, leads[i].EmailValid, leads[i].EmailCatchall, leads[i].EmailDisposable, leads[i].EmailConfidence,
			leads[i].WebsiteURL, leads[i].WebsiteDomain, leads[i].Address, leads[i].City, leads[i].Country, leads[i].Source, leads[i].SourceURLs,
			leads[i].LeadScore, leads[i].TechStack, leads[i].HasSSL, leads[i].IsMobileFriendly, leads[i].Status, leads[i].AssignedTo)
		if err != nil {
			return err
		}
	}
	r.invalidateFilterCache(ctx)
	return nil
}

const leadInsertSQL = `INSERT INTO leads (
	id, admin_id, business_name, category, phone_e164, phone_valid, phone_type, phone_confidence,
	email, email_valid, email_catchall, email_disposable, email_confidence,
	website_url, website_domain, address, city, country, source, source_urls,
	lead_score, tech_stack, has_ssl, is_mobile_friendly, status, assigned_to, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,NOW(),NOW())`

func (r *LeadRepo) GetByID(ctx context.Context, id string) (*models.Lead, error) {
	lead, err := redis.Fetch(ctx, "lead:"+id, r.leadTTL, func(ctx context.Context) (*models.Lead, error) {
		var l models.Lead
		found, err := postgress.Get(ctx, "leads", id, &l)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return &l, nil
	})
	if err != nil {
		return nil, err
	}
	if lead != nil && lead.AssignedTo != nil {
		r.populateAssignedName(ctx, lead)
	}
	return lead, nil
}

type filteredResult struct {
	Leads []models.Lead `json:"leads"`
	Total int           `json:"total"`
}

func (r *LeadRepo) GetFiltered(ctx context.Context, city, status, source string, scoreGTE int, hasPhone bool, page, pageSize int) ([]models.Lead, int, error) {
	cacheKey := fmt.Sprintf("leads:filter:%x", sha256.Sum256(
		[]byte(fmt.Sprintf("%s|%s|%s|%d|%v|%d|%d", city, status, source, scoreGTE, hasPhone, page, pageSize)),
	))

	result, err := redis.Fetch(ctx, cacheKey, r.filterTTL, func(ctx context.Context) (*filteredResult, error) {
		where := "1=1"
		args := []any{}
		argIdx := 1

		if city != "" {
			where += fmt.Sprintf(" AND l.city = $%d", argIdx)
			args = append(args, city)
			argIdx++
		}
		if status != "" {
			where += fmt.Sprintf(" AND l.status = $%d", argIdx)
			args = append(args, status)
			argIdx++
		}
		if source != "" {
			where += fmt.Sprintf(" AND $%d = ANY(l.source)", argIdx)
			args = append(args, source)
			argIdx++
		}
		if scoreGTE > 0 {
			where += fmt.Sprintf(" AND l.lead_score >= $%d", argIdx)
			args = append(args, scoreGTE)
			argIdx++
		}
		if hasPhone {
			where += " AND l.phone_valid = true"
		}

		countSQL := "SELECT COUNT(*) FROM leads l WHERE " + where
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, countSQL, args...)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		dataSQL := fmt.Sprintf("SELECT * FROM leads l WHERE %s ORDER BY (l.status = 'new') DESC, l.lead_score DESC, l.created_at DESC LIMIT $%d OFFSET $%d", where, argIdx, argIdx+1)
		args = append(args, pageSize, offset)
		leads, err := postgress.Query[models.Lead](ctx, dataSQL, args...)
		if err != nil {
			return nil, err
		}
		return &filteredResult{Leads: leads, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	r.populateAssignedNames(ctx, result.Leads)
	return result.Leads, result.Total, nil
}

func (r *LeadRepo) Update(ctx context.Context, lead models.Lead) error {
	err := postgress.Update(ctx, "leads", lead)
	if err != nil {
		return err
	}
	redis.Remove(ctx, "lead:"+lead.ID)
	r.invalidateFilterCache(ctx)
	return nil
}

func (r *LeadRepo) FindByPhone(ctx context.Context, phone string) (*models.Lead, error) {
	leads, err := postgress.Query[models.Lead](ctx, "SELECT * FROM leads WHERE phone_e164 = $1 LIMIT 1", phone)
	if err != nil {
		return nil, err
	}
	if len(leads) == 0 {
		return nil, nil
	}
	return &leads[0], nil
}

func (r *LeadRepo) FindByEmail(ctx context.Context, email string) (*models.Lead, error) {
	leads, err := postgress.Query[models.Lead](ctx, "SELECT * FROM leads WHERE LOWER(email) = LOWER($1) LIMIT 1", email)
	if err != nil {
		return nil, err
	}
	if len(leads) == 0 {
		return nil, nil
	}
	return &leads[0], nil
}

func (r *LeadRepo) FindByDomain(ctx context.Context, domain string) (*models.Lead, error) {
	leads, err := postgress.Query[models.Lead](ctx, "SELECT * FROM leads WHERE website_domain = $1 LIMIT 1", domain)
	if err != nil {
		return nil, err
	}
	if len(leads) == 0 {
		return nil, nil
	}
	return &leads[0], nil
}

func (r *LeadRepo) MergeSources(ctx context.Context, id string, newSources []string, sourceURLs map[string]string) error {
	// Merge source_urls JSONB: existing || new (new wins on conflict)
	urlsJSON, _ := json.Marshal(sourceURLs)
	_, err := postgress.Exec(ctx,
		"UPDATE leads SET source = ARRAY(SELECT DISTINCT unnest(source || $1)), source_urls = COALESCE(source_urls, '{}'::jsonb) || $3::jsonb WHERE id = $2",
		newSources, id, string(urlsJSON),
	)
	if err == nil {
		redis.Remove(ctx, "lead:"+id)
		r.invalidateFilterCache(ctx)
	}
	return err
}

// invalidateFilterCache removes all cached filter query results.
func (r *LeadRepo) invalidateFilterCache(ctx context.Context) {
	client := redis.GetRawClient()
	iter := client.Scan(ctx, 0, "sales:leads:filter:*", 100).Iterator()
	for iter.Next(ctx) {
		client.Del(ctx, iter.Val())
	}
}

// BulkAssign sets the assigned_to field on multiple leads.
func (r *LeadRepo) BulkAssign(ctx context.Context, leadIDs []string, employeeID string) (int, error) {
	if len(leadIDs) == 0 {
		return 0, nil
	}
	// Build placeholders: $1 = employeeID, $2..$N = leadIDs
	args := []any{employeeID}
	placeholders := ""
	for i, id := range leadIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}
	sql := fmt.Sprintf("UPDATE leads SET assigned_to = $1, updated_at = NOW() WHERE id IN (%s)", placeholders)
	rowsAffected, err := postgress.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	r.invalidateFilterCache(ctx)
	return int(rowsAffected), nil
}

// UnassignLeads removes the assigned_to on multiple leads.
func (r *LeadRepo) UnassignLeads(ctx context.Context, leadIDs []string) (int, error) {
	if len(leadIDs) == 0 {
		return 0, nil
	}
	args := []any{}
	placeholders := ""
	for i, id := range leadIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	sql := fmt.Sprintf("UPDATE leads SET assigned_to = NULL, updated_at = NOW() WHERE id IN (%s)", placeholders)
	rowsAffected, err := postgress.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	r.invalidateFilterCache(ctx)
	return int(rowsAffected), nil
}

// GetFilteredByAdmin returns leads scoped to an admin via admin_id column.
func (r *LeadRepo) GetFilteredByAdmin(ctx context.Context, adminID, city, status, source string, scoreGTE int, hasPhone bool, page, pageSize int) ([]models.Lead, int, error) {
	cacheKey := fmt.Sprintf("leads:admin:%s:filter:%x", adminID, sha256.Sum256(
		[]byte(fmt.Sprintf("%s|%s|%s|%d|%v|%d|%d", city, status, source, scoreGTE, hasPhone, page, pageSize)),
	))

	result, err := redis.Fetch(ctx, cacheKey, r.filterTTL, func(ctx context.Context) (*filteredResult, error) {
		baseWhere := `FROM leads l WHERE l.admin_id = $1`
		args := []any{adminID}
		argIdx := 2

		where := ""
		if city != "" {
			where += fmt.Sprintf(" AND l.city = $%d", argIdx)
			args = append(args, city)
			argIdx++
		}
		if status != "" {
			where += fmt.Sprintf(" AND l.status = $%d", argIdx)
			args = append(args, status)
			argIdx++
		}
		if source != "" {
			where += fmt.Sprintf(" AND $%d = ANY(l.source)", argIdx)
			args = append(args, source)
			argIdx++
		}
		if scoreGTE > 0 {
			where += fmt.Sprintf(" AND l.lead_score >= $%d", argIdx)
			args = append(args, scoreGTE)
			argIdx++
		}
		if hasPhone {
			where += " AND l.phone_valid = true"
		}

		countSQL := "SELECT COUNT(*) " + baseWhere + where
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, countSQL, args...)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		dataSQL := fmt.Sprintf("SELECT l.* %s%s ORDER BY (l.status = 'new') DESC, l.lead_score DESC, l.created_at DESC LIMIT $%d OFFSET $%d",
			baseWhere, where, argIdx, argIdx+1)
		args = append(args, pageSize, offset)
		leads, err := postgress.Query[models.Lead](ctx, dataSQL, args...)
		if err != nil {
			return nil, err
		}
		return &filteredResult{Leads: leads, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	// Populate assigned employee names
	r.populateAssignedNames(ctx, result.Leads)
	return result.Leads, result.Total, nil
}

// populateAssignedName fills the AssignedToName field for a single lead.
func (r *LeadRepo) populateAssignedName(ctx context.Context, lead *models.Lead) {
	if lead.AssignedTo == nil {
		return
	}
	rows, err := postgress.Query[struct {
		Name string `db:"name"`
	}](ctx, "SELECT name FROM users WHERE id = $1", *lead.AssignedTo)
	if err == nil && len(rows) > 0 {
		lead.AssignedToName = &rows[0].Name
	}
}

// populateAssignedNames fills the AssignedToName field for a list of leads (batch).
func (r *LeadRepo) populateAssignedNames(ctx context.Context, leads []models.Lead) {
	// Collect unique assigned_to IDs
	idSet := map[string]bool{}
	for i := range leads {
		if leads[i].AssignedTo != nil {
			idSet[*leads[i].AssignedTo] = true
		}
	}
	if len(idSet) == 0 {
		return
	}

	// Build IN query
	ids := make([]any, 0, len(idSet))
	placeholders := ""
	idx := 1
	for id := range idSet {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", idx)
		ids = append(ids, id)
		idx++
	}

	type userRow struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	rows, err := postgress.Query[userRow](ctx, fmt.Sprintf("SELECT id, name FROM users WHERE id IN (%s)", placeholders), ids...)
	if err != nil {
		return
	}

	nameMap := map[string]string{}
	for _, row := range rows {
		nameMap[row.ID] = row.Name
	}

	for i := range leads {
		if leads[i].AssignedTo != nil {
			if name, ok := nameMap[*leads[i].AssignedTo]; ok {
				leads[i].AssignedToName = &name
			}
		}
	}
}
