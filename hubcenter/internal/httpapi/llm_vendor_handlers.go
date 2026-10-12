package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// adminStartQoderLogin opens a Qoder device approval. The edition decides
// which signing gateway the resulting credential is bound to, so it is
// required up front rather than recovered later.
func adminStartQoderLogin(svc *llmservice.Service) http.HandlerFunc {
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
		started, err := svc.StartQoderLogin(req.Edition)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, started)
	}
}

func adminQoderLoginStatus(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		status, err := svc.QoderLoginStatus(r.PathValue("id"))
		if err != nil {
			writeJSONResp(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, status)
	}
}

func adminCancelQoderLogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc != nil {
			svc.CancelQoderLogin(r.PathValue("id"))
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// adminStartTraeLogin opens a Trae SOLO approval. Both editions share the
// login page shape, so the edition only selects the realm behind it.
func adminStartTraeLogin(svc *llmservice.Service) http.HandlerFunc {
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
		started, err := svc.StartTraeLogin(req.Edition)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, started)
	}
}

func adminTraeLoginStatus(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		status, err := svc.TraeLoginStatus(r.PathValue("id"))
		if err != nil {
			writeJSONResp(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, status)
	}
}

func adminCancelTraeLogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc != nil {
			svc.CancelTraeLogin(r.PathValue("id"))
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// adminStartLobsterAILogin opens a LobsterAI portal approval. There is no
// edition: one product, one credential family.
func adminStartLobsterAILogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		started, err := svc.StartLobsterAILogin()
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, started)
	}
}

func adminLobsterAILoginStatus(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider service unavailable"})
			return
		}
		status, err := svc.LobsterAILoginStatus(r.PathValue("id"))
		if err != nil {
			writeJSONResp(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, status)
	}
}

func adminCancelLobsterAILogin(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc != nil {
			svc.CancelLobsterAILogin(r.PathValue("id"))
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
