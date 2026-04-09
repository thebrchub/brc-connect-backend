package service

import (
	"context"
	"log"
	"net/url"
	"strings"

	json "github.com/goccy/go-json"

	"sales-scrapper-backend/api/models"
	"sales-scrapper-backend/api/repository"
)

type LeadService struct {
	leadRepo     *repository.LeadRepo
	campaignRepo *repository.CampaignRepo
	dedup        *DedupService
}

func NewLeadService(leadRepo *repository.LeadRepo, campaignRepo *repository.CampaignRepo) *LeadService {
	return &LeadService{
		leadRepo:     leadRepo,
		campaignRepo: campaignRepo,
		dedup:        NewDedupService(leadRepo),
	}
}

// ProcessBatch takes raw leads from Node.js, validates, deduplicates, scores, and inserts them.
func (s *LeadService) ProcessBatch(ctx context.Context, jobID string, rawLeads []models.RawLead) models.LeadBatchResponse {
	toInsert := make([]models.Lead, 0, len(rawLeads))
	inserted, merged, skipped := 0, 0, 0

	for _, raw := range rawLeads {
		// Normalize phone
		phone := ""
		if raw.Phone != nil {
			phone = NormalizePhone(*raw.Phone)
		}

		// Normalize email
		email := ""
		if raw.Email != nil {
			e := strings.TrimSpace(strings.ToLower(*raw.Email))
			if decoded, err := url.QueryUnescape(e); err == nil {
				e = strings.TrimSpace(decoded)
			}
			email = e
		}

		// Extract domain
		websiteURL := ""
		if raw.WebsiteURL != nil {
			websiteURL = *raw.WebsiteURL
		}
		domain := ExtractDomain(websiteURL)

		// Dedup check
		existingID, err := s.dedup.FindDuplicate(ctx, phone, email, websiteURL)
		if err != nil {
			log.Printf("ERROR [lead-service] - dedup check failed error=%s", err)
			skipped++
			continue
		}

		if existingID != "" {
			// Merge sources into existing lead
			srcURLs := map[string]string{}
			if raw.SourceURL != nil && *raw.SourceURL != "" {
				srcURLs[raw.Source] = *raw.SourceURL
			}
			err := s.leadRepo.MergeSources(ctx, existingID, []string{raw.Source}, srcURLs)
			if err != nil {
				log.Printf("ERROR [lead-service] - merge sources failed error=%s", err)
			}
			merged++
			continue
		}

		// Validate phone
		phoneValid := false
		phoneType := "unknown"
		if phone != "" {
			phoneValid = ValidatePhoneFormat(phone)
			phoneType = DetectPhoneType(phone)
		}

		// Validate email (format + disposable only — MX/SMTP checks are async)
		emailValid := false
		emailDisposable := false
		emailConf := 0
		if email != "" {
			formatOK := ValidateEmailFormat(email)
			emailDisposable = IsDisposable(email)
			emailValid = formatOK && !emailDisposable
			// Fast confidence: format(+20) + not-disposable(+10) = 30 max in hot path
			// MX(+30) and SMTP(+30) added later by background validator
			if formatOK {
				emailConf += 20
			}
			if !emailDisposable {
				emailConf += 10
			}
		}

		phoneConf := PhoneConfidence(phoneValid, phoneType, false)
		score := ScoreCalculate(raw, nil)

		lead := models.Lead{
			BusinessName:     raw.BusinessName,
			Category:         raw.Category,
			PhoneValid:       phoneValid,
			PhoneConfidence:  phoneConf,
			EmailValid:       emailValid,
			EmailCatchall:    false,
			EmailDisposable:  emailDisposable,
			EmailConfidence:  emailConf,
			City:             raw.City,
			Country:          detectCountry(raw.Country, phone),
			Source:           []string{raw.Source},
			LeadScore:        score,
			TechStack:        raw.TechStack,
			HasSSL:           raw.HasSSL,
			IsMobileFriendly: raw.IsMobileFriendly,
			Status:           "new",
		}

		// Build source_urls
		if raw.SourceURL != nil && *raw.SourceURL != "" {
			srcURLs := map[string]string{raw.Source: *raw.SourceURL}
			if b, err := json.Marshal(srcURLs); err == nil {
				lead.SourceURLs = b
			}
		}

		if phone != "" {
			lead.PhoneE164 = &phone
			lead.PhoneType = &phoneType
		}
		if email != "" {
			lead.Email = &email
		}
		if websiteURL != "" {
			lead.WebsiteURL = &websiteURL
		}
		if domain != "" {
			lead.WebsiteDomain = &domain
		}
		if raw.Address != nil {
			lead.Address = raw.Address
		}

		toInsert = append(toInsert, lead)
		inserted++
	}

	if len(toInsert) > 0 {
		err := s.leadRepo.InsertBatch(ctx, toInsert)
		if err != nil {
			// Unique constraint violations from concurrent dedup race — fall back to individual inserts
			log.Printf("WARN  [lead-service] - batch insert failed, falling back to individual inserts error=%s", err)
			actualInserted := 0
			for _, lead := range toInsert {
				_, insertErr := s.leadRepo.Insert(ctx, lead)
				if insertErr != nil {
					skipped++
				} else {
					actualInserted++
				}
			}
			inserted = actualInserted
		}
	}

	return models.LeadBatchResponse{
		Inserted: inserted,
		Merged:   merged,
		Skipped:  skipped,
	}
}

// detectCountry returns the country from raw data, falling back to phone prefix detection.
func detectCountry(raw string, phone string) string {
	if raw != "" {
		return raw
	}
	if strings.HasPrefix(phone, "+91") {
		return "IN"
	}
	if strings.HasPrefix(phone, "+1") {
		return "US"
	}
	if strings.HasPrefix(phone, "+44") {
		return "GB"
	}
	if strings.HasPrefix(phone, "+61") {
		return "AU"
	}
	if strings.HasPrefix(phone, "+65") {
		return "SG"
	}
	return ""
}
