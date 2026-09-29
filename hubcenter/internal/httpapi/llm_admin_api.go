package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

const (
	llmAdminAPIDocPath     = "/api/llm/admin-api.md"
	llmAdminAPIOpenAPIPath = "/api/llm/admin-api.json"
)

func RequireLLMAdmin(admins *auth.AdminService, llm *llmservice.Service, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := llmAdminCredential(r)
		if strings.HasPrefix(token, "hck_") {
			if llm == nil || llm.AuthenticateAdminAPIKey(r.Context(), token) != nil {
				writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Invalid admin API key")
				return
			}
			next(w, r)
			return
		}
		RequireAdmin(admins, next)(w, r)
	}
}

func llmAdminCredential(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > len("Bearer ") && strings.EqualFold(authz[:len("Bearer ")], "bearer ") {
		return strings.TrimSpace(authz[len("Bearer "):])
	}
	return ""
}

func adminListLLMAdminAPIKeys(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := svc.ListAdminAPIKeys(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{
			"keys":       keys,
			"doc_url":    llmAdminAPIDocPath,
			"openapi_url": llmAdminAPIOpenAPIPath,
		})
	}
}

func adminCreateLLMAdminAPIKey(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		created, err := svc.CreateAdminAPIKey(r.Context(), req.Name)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{
			"status":      "ok",
			"id":          created.ID,
			"name":        created.Name,
			"prefix":      created.Prefix,
			"api_key":     created.APIKey,
			"created_at":  created.CreatedAt,
			"doc_url":     llmAdminAPIDocPath,
			"openapi_url": llmAdminAPIOpenAPIPath,
		})
	}
}

func adminRevokeLLMAdminAPIKey(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "api key id required"})
			return
		}
		if err := svc.RevokeAdminAPIKey(r.Context(), id); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminImportLLMProviderArrays(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Arrays []llmservice.ProviderArrayImport `json:"arrays"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		result, err := svc.ImportProviderArrays(r.Context(), req.Arrays)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"status": "ok", "arrays": result.Arrays})
	}
}

func llmAdminAPIMarkdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(llmAdminAPIDocMarkdown))
}

func llmAdminAPIOpenAPIDoc(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, http.StatusOK, llmAdminAPIOpenAPI())
}
