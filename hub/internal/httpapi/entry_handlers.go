package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/entry"
)

type EntryProbeRequest struct {
	Email string `json:"email"`
}

func EntryProbeHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EntryProbeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		resp, err := service.ProbeByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENTRY_PROBE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
