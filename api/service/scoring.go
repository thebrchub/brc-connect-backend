package service

import (
	"encoding/json"
	"sales-scrapper-backend/api/models"
	"strings"
)

// ScoreCalculate returns a lead score (0-100) based on business signals.
func ScoreCalculate(lead models.RawLead, existing *models.Lead) int {
	score := 0
	hasWebsite := lead.WebsiteURL != nil && *lead.WebsiteURL != ""
	hasPhone := lead.Phone != nil && *lead.Phone != ""
	hasEmail := lead.Email != nil && *lead.Email != ""

	// --- Website signals ---

	// No website → high-value prospect (pitch them to build one)
	if !hasWebsite {
		score += 25
	}

	// Has website but no SSL → security upgrade pitch
	if hasWebsite && lead.HasSSL != nil && !*lead.HasSSL {
		score += 15
	}

	// Has website but not mobile-friendly → redesign pitch
	if hasWebsite && lead.IsMobileFriendly != nil && !*lead.IsMobileFriendly {
		score += 15
	}

	// Outdated tech stack (old CMS/server)
	if hasWebsite && len(lead.TechStack) > 0 {
		var ts map[string]string
		if json.Unmarshal(lead.TechStack, &ts) == nil {
			for k, v := range ts {
				lk := strings.ToLower(k)
				lv := strings.ToLower(v)
				if lk == "cms" && (lv == "wordpress" || lv == "joomla" || lv == "drupal" || lv == "wix" || lv == "weebly") {
					score += 10
					break
				}
				if lk == "server" && (lv == "apache" || lv == "iis") {
					score += 5
					break
				}
			}
		}
	}

	// --- Contact signals ---

	// Has phone → directly reachable
	if hasPhone {
		score += 15
	}

	// Has email → outreach channel
	if hasEmail {
		score += 15
	}

	// Has both → best contact lead (bonus)
	if hasPhone && hasEmail {
		score += 5
	}

	// --- Credibility signals ---

	// Found on multiple sources → established business
	if existing != nil && len(existing.Source) >= 2 {
		score += 10
	}

	// Has physical address → verified brick-and-mortar
	if lead.Address != nil && *lead.Address != "" {
		score += 5
	}

	if score > 100 {
		score = 100
	}
	return score
}
