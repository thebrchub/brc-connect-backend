package handler

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"brc-connect-backend/api/repository"
)

type ExportHandler struct {
	leadRepo      *repository.LeadRepo
	exportMaxRows int
}

func NewExportHandler(leadRepo *repository.LeadRepo, exportMaxRows int) *ExportHandler {
	return &ExportHandler{leadRepo: leadRepo, exportMaxRows: exportMaxRows}
}

// ExportCSV handles GET /leads/export?format=csv.
// Streams results in chunks to avoid loading all rows into memory.
func (h *ExportHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	city := q.Get("city")
	status := q.Get("status")
	source := q.Get("source")

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=leads.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header row
	writer.Write([]string{
		"Business Name", "Category", "Phone", "Phone Valid",
		"Email", "Email Valid", "Website", "Domain", "City", "Country",
		"Score", "Status", "Sources",
	})

	const chunkSize = 500
	exported := 0
	page := 1
	for exported < h.exportMaxRows {
		remaining := h.exportMaxRows - exported
		size := chunkSize
		if remaining < size {
			size = remaining
		}

		leads, _, err := h.leadRepo.GetFiltered(r.Context(), city, status, source, 0, false, page, size)
		if err != nil {
			log.Printf("ERROR [export] - fetch leads failed page=%d error=%s", page, err)
			// Write error marker row so the client knows the CSV is incomplete
			writer.Write([]string{"ERROR", "Export incomplete — server error during data fetch", "", "", "", "", "", "", "", "", "", "", ""})
			writer.Flush()
			return
		}
		if len(leads) == 0 {
			break
		}

		for _, l := range leads {
			phone := ""
			if l.PhoneE164 != nil {
				phone = *l.PhoneE164
			}
			email := ""
			if l.Email != nil {
				email = *l.Email
			}
			website := ""
			if l.WebsiteURL != nil {
				website = *l.WebsiteURL
			}
			domain := ""
			if l.WebsiteDomain != nil {
				domain = *l.WebsiteDomain
			}
			var sources strings.Builder
			for i, s := range l.Source {
				if i > 0 {
					sources.WriteString(",")
				}
				sources.WriteString(s)
			}

			writer.Write([]string{
				l.BusinessName,
				l.Category,
				phone,
				fmt.Sprintf("%t", l.PhoneValid),
				email,
				fmt.Sprintf("%t", l.EmailValid),
				website,
				domain,
				l.City,
				l.Country,
				strconv.Itoa(l.LeadScore),
				l.Status,
				sources.String(),
			})
		}

		writer.Flush()
		exported += len(leads)
		if len(leads) < size {
			break
		}
		page++
	}
}
