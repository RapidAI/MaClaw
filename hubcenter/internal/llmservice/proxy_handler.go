package llmservice

import (
	"context"
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

type TenantAuthorizationStatus struct {
	HubID                  string                       `json:"hub_id"`
	TenantID               string                       `json:"tenant_id"`
	LookupTenantIDs        []string                     `json:"lookup_tenant_ids,omitempty"`
	AllowExternalProviders bool                         `json:"allow_external_providers"`
	Authorizations         []TenantAuthorizationSummary `json:"authorizations,omitempty"`
}

type TenantAuthorizationSummary struct {
	ID                     string  `json:"id"`
	HubID                  string  `json:"hub_id,omitempty"`
	TenantID               string  `json:"tenant_id,omitempty"`
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

func BuildTenantAuthorizationStatus(ctx context.Context, checker *AuthorizationChecker, hubID, tenantID string) (*TenantAuthorizationStatus, error) {
	auths, err := checker.ListByHubTenantAliases(ctx, hubID, tenantID)
	if err != nil {
		return nil, err
	}

	current := now()
	result := &TenantAuthorizationStatus{
		HubID:           hubID,
		TenantID:        tenantID,
		LookupTenantIDs: tenantAuthorizationLookupIDs(tenantID),
	}
	for _, a := range auths {
		active := a.IsActive(current)
		// External compute permission records are pure permission grants,
		// not credit-based quotas. They remain active as long as status is
		// "active" and the time window is valid, regardless of credits.
		if !active && isExternalComputePermissionRecord(a) && isTimeWindowActive(a, current) {
			active = true
		}
		if active && isExternalProviderGrant(a) {
			result.AllowExternalProviders = true
		}
		if active && !isExternalComputePermissionRecord(a) {
			// Only include credit-based authorizations in the list.
			// Pure permission grants (external_provider_permission) are not
			// credit quotas and should not appear as "充值记录" to the user.
			result.Authorizations = append(result.Authorizations, TenantAuthorizationSummary{
				ID:                     a.ID,
				HubID:                  a.HubID,
				TenantID:               a.TenantID,
				ServiceGroupID:         a.ServiceGroupID,
				CreditsTotal:           a.CreditsTotal,
				CreditsUsed:            a.CreditsUsed,
				CreditsRemaining:       a.CreditsRemaining(),
				StartsAt:               a.StartsAt.Format(time.RFC3339),
				ExpiresAt:              a.ExpiresAt.Format(time.RFC3339),
				Status:                 a.Status,
				Active:                 true,
				AllowExternalProviders: a.AllowExternalProviders,
				Source:                 a.Source,
				CardOrderID:            a.CardOrderID,
			})
		}
	}
	return result, nil
}

// isExternalComputePermissionRecord returns true for records that represent
// a permission grant (not a credit-based quota). These records should not be
// gated by CreditsRemaining > 0.
func isExternalComputePermissionRecord(a *TenantAuthorization) bool {
	if a == nil {
		return false
	}
	return a.ServiceGroupID == ExternalComputePermissionServiceGroupID ||
		a.Source == "external_provider_permission"
}

// isTimeWindowActive checks if the record is within its time validity window
// and not explicitly expired/exhausted. Does NOT check credits.
func isTimeWindowActive(a *TenantAuthorization, now time.Time) bool {
	if a == nil {
		return false
	}
	if a.Status == "expired" || a.Status == "exhausted" {
		return false
	}
	// Treat zero StartsAt as "always started".
	if !a.StartsAt.IsZero() && now.Before(a.StartsAt) {
		return false
	}
	// Treat zero ExpiresAt as "never expires".
	if !a.ExpiresAt.IsZero() && now.After(a.ExpiresAt) {
		return false
	}
	return true
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

		result, err := BuildTenantAuthorizationStatus(r.Context(), checker, hubID, tenantID)
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
		Tenants map[string]*TenantAuthorizationStatus `json:"tenants,omitempty"`
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

		resp := batchResponse{Tenants: map[string]*TenantAuthorizationStatus{}}
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
			status, err := BuildTenantAuthorizationStatus(r.Context(), checker, hubID, tenantID)
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
