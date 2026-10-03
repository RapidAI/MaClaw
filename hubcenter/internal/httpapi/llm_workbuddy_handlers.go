package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func adminStartWorkBuddyLogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Edition string `json:"edition"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		started, err := svc.StartWorkBuddyLogin(req.Edition)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, started)
	}
}

func adminWorkBuddyLoginStatus(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		status, err := svc.WorkBuddyLoginStatus(r.PathValue("id"))
		if err != nil {
			writeJSONResp(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, status)
	}
}

func adminCancelWorkBuddyLogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc != nil {
			svc.CancelWorkBuddyLogin(strings.TrimSpace(r.PathValue("id")))
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
