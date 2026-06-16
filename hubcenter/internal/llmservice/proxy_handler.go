package llmservice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProxyHandler returns an HTTP handler for the LLM proxy endpoint.
// POST /api/llm/v1/chat/completions
//
// Headers:
//   - Authorization: Bearer <hub_machine_token>  (validated upstream by hub auth middleware)
//   - X-Hub-ID: hub instance ID
//   - X-Tenant-ID: tenant ID on the hub
func ProxyHandler(cfg *ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if hubID == "" || tenantID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID and X-Tenant-ID headers are required")
			return
		}

		// Read body
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		defer r.Body.Close()

		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		proxyReq := &ProxyRequest{
			HubID:    hubID,
			TenantID: tenantID,
			Body:     parsed,
			RawBody:  body,
		}

		resp, err := HandleProxyRequest(r.Context(), cfg, proxyReq)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "authorization denied") {
				writeJSONError(w, http.StatusForbidden, errMsg)
				return
			}
			if strings.Contains(errMsg, "bound to node") {
				// HA: tenant is bound to a different HubCenter node
				writeJSONError(w, http.StatusConflict, errMsg)
				return
			}
			if strings.Contains(errMsg, "not available") || strings.Contains(errMsg, "not specified") {
				writeJSONError(w, http.StatusBadRequest, errMsg)
				return
			}
			if strings.Contains(errMsg, "all providers failed") {
				writeJSONError(w, http.StatusServiceUnavailable, errMsg)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, errMsg)
			return
		}

		// Forward the upstream response as-is
		w.Header().Set("Content-Type", "application/json")
		if resp.CacheHit {
			w.Header().Set("X-Cache", "HIT")
		}
		w.Header().Set("X-Provider-ID", resp.ProviderID)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(resp.Body)
	}
}

type tenantAuthorizationStatus struct {
	HubID                  string                       `json:"hub_id"`
	TenantID               string                       `json:"tenant_id"`
	AllowExternalProviders bool                         `json:"allow_external_providers"`
	Authorizations         []tenantAuthorizationSummary `json:"authorizations"`
}

type tenantAuthorizationSummary struct {
	ID                     string  `json:"id"`
	ServiceGroupID         string  `json:"service_group_id"`
	CreditsTotal           float64 `json:"credits_total"`
	CreditsUsed            float64 `json:"credits_used"`
	CreditsRemaining       float64 `json:"credits_remaining"`
	StartsAt               string  `json:"starts_at"`
	ExpiresAt              string  `json:"expires_at"`
	Status                 string  `json:"status"`
	Active                 bool    `json:"active"`
	AllowExternalProviders bool    `json:"allow_external_providers"`
	Source                 string  `json:"source"`
	CardOrderID            string  `json:"card_order_id,omitempty"`
}

func buildTenantAuthorizationStatus(r *http.Request, checker *AuthorizationChecker, hubID, tenantID string) (*tenantAuthorizationStatus, error) {
	auths, err := checker.repo.ListByHubTenant(r.Context(), hubID, tenantID)
	if err != nil {
		return nil, err
	}

	current := now()
	result := &tenantAuthorizationStatus{
		HubID:    hubID,
		TenantID: tenantID,
	}
	for _, a := range auths {
		active := a.IsActive(current)
		if active && (isExternalProviderGrant(a) || a.ServiceGroupID == ExternalComputePermissionServiceGroupID) {
			result.AllowExternalProviders = true
		}
		result.Authorizations = append(result.Authorizations, tenantAuthorizationSummary{
			ID:                     a.ID,
			ServiceGroupID:         a.ServiceGroupID,
			CreditsTotal:           a.CreditsTotal,
			CreditsUsed:            a.CreditsUsed,
			CreditsRemaining:       a.CreditsRemaining(),
			StartsAt:               a.StartsAt.Format(time.RFC3339),
			ExpiresAt:              a.ExpiresAt.Format(time.RFC3339),
			Status:                 a.Status,
			Active:                 active,
			AllowExternalProviders: a.AllowExternalProviders,
			Source:                 a.Source,
			CardOrderID:            a.CardOrderID,
		})
	}
	return result, nil
}

// AuthorizationQueryHandler returns an HTTP handler for querying tenant authorization status.
// GET /api/llm/v1/authorization?hub_id=X&tenant_id=Y
func AuthorizationQueryHandler(checker *AuthorizationChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if hubID == "" || tenantID == "" {
			writeJSONError(w, http.StatusBadRequest, "hub_id and tenant_id query params are required")
			return
		}

		result, err := buildTenantAuthorizationStatus(r, checker, hubID, tenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// AuthorizationBatchQueryHandler returns authorization status for multiple tenants.
// POST /api/llm/v1/authorization/batch
func AuthorizationBatchQueryHandler(checker *AuthorizationChecker) http.HandlerFunc {
	type batchRequest struct {
		TenantIDs []string `json:"tenant_ids"`
	}
	type batchResponse struct {
		Tenants map[string]*tenantAuthorizationStatus `json:"tenants,omitempty"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		if hubID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID header is required")
			return
		}

		var req batchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		resp := batchResponse{Tenants: map[string]*tenantAuthorizationStatus{}}
		seen := map[string]struct{}{}
		for _, rawTenantID := range req.TenantIDs {
			tenantID := strings.TrimSpace(rawTenantID)
			if tenantID == "" {
				continue
			}
			if _, ok := seen[tenantID]; ok {
				continue
			}
			seen[tenantID] = struct{}{}
			status, err := buildTenantAuthorizationStatus(r, checker, hubID, tenantID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			resp.Tenants[tenantID] = status
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"code":    code,
		},
	})
}

func now() time.Time {
	return time.Now().UTC()
}
