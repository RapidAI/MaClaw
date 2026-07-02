package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
)

type EntryProbeRequest struct {
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

func EntryProbeHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EntryProbeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		identity := entryProbeIdentity(req)
		tenantID, err := tenantIDForEmailRequest(r, service, identity)
		if err != nil {
			writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
			return
		}

		resp, err := service.ProbeByEmail(auth.WithTenant(r.Context(), tenantID), identity)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENTRY_PROBE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func entryProbeIdentity(req EntryProbeRequest) string {
	if email := strings.TrimSpace(req.Email); email != "" {
		return email
	}
	phone := strings.TrimSpace(req.PhoneNumber)
	if strings.HasPrefix(strings.ToLower(phone), "phone:") {
		phone = strings.TrimPrefix(strings.ToLower(phone), "phone:")
	}
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "phone:" + b.String()
}
