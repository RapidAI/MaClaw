package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

type llmModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Models []struct {
		ID string `json:"id"`
	} `json:"models"`
}

// ---------------------------------------------------------------------------
// LLM Provider admin handlers
// ---------------------------------------------------------------------------

func adminListLLMProviders(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := svc.LoadRegistry(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Redact API keys — only expose has_api_key flag
		type safeProvider struct {
			llmpool.ProviderConfig
			HasAPIKey bool   `json:"has_api_key"`
			APIKey    string `json:"api_key,omitempty"` // always empty in response
		}
		safe := make([]safeProvider, len(reg.Providers))
		for i, p := range reg.Providers {
			safe[i] = safeProvider{
				ProviderConfig: p,
				HasAPIKey:      p.APIKey != "",
			}
			safe[i].ProviderConfig.APIKey = "" // redact
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"providers": safe})
	}
}

func adminAddLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var provider llmpool.ProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if provider.ID == "" || provider.Name == "" || provider.APIURL == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "id, name, and api_url are required"})
			return
		}
		if err := svc.AddProvider(r.Context(), provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminUpdateLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		var provider llmpool.ProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		provider.ID = id
		if provider.APIKey == "" {
			existing, err := svc.GetProvider(r.Context(), id)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if existing != nil {
				provider.APIKey = existing.APIKey
			}
		}
		if err := svc.UpdateProvider(r.Context(), provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminProbeLLMProviderModels(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID string `json:"provider_id"`
			APIURL     string `json:"api_url"`
			APIKey     string `json:"api_key"`
			Protocol   string `json:"protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if req.APIKey == "" && req.ProviderID != "" && svc != nil {
			existing, err := svc.GetProvider(r.Context(), req.ProviderID)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if existing != nil {
				req.APIKey = existing.APIKey
			}
		}
		models, err := probeLLMProviderModels(r.Context(), req.APIURL, req.APIKey, req.Protocol)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"models": models})
	}
}

func probeLLMProviderModels(ctx context.Context, apiURL, apiKey, protocol string) ([]string, error) {
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		return nil, fmt.Errorf("api_url is required")
	}
	endpoint, err := llmModelsEndpoint(apiURL, protocol)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(protocol, "anthropic") {
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("probe failed: %s", msg)
	}
	var parsed llmModelListResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var models []string
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	for _, item := range parsed.Models {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, nil
}

func llmModelsEndpoint(apiURL, protocol string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/models") {
		return u.String(), nil
	}
	if strings.HasSuffix(path, "/chat/completions") {
		path = strings.TrimSuffix(path, "/chat/completions")
	}
	if strings.EqualFold(protocol, "anthropic") {
		if !strings.HasSuffix(path, "/v1") {
			path += "/v1"
		}
		u.Path = path + "/models"
		return u.String(), nil
	}
	u.Path = path + "/models"
	return u.String(), nil
}

func adminDeleteLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		if err := svc.DeleteProvider(r.Context(), id); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// LLM Compute Agent admin handlers
// ---------------------------------------------------------------------------

func adminListLLMAgents(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agents, err := svc.ListAgents(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

func adminAddLLMAgent(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var agent llmservice.ComputeAgent
		if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := svc.AddAgent(r.Context(), agent); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminUpdateLLMAgent(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "agent id required"})
			return
		}
		var agent llmservice.ComputeAgent
		if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		agent.ID = id
		if err := svc.UpdateAgent(r.Context(), agent); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminDeleteLLMAgent(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "agent id required"})
			return
		}
		if err := svc.DeleteAgent(r.Context(), id); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// LLM Service Group admin handlers
// ---------------------------------------------------------------------------

func adminListLLMServiceGroups(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := svc.LoadRegistry(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"service_groups": reg.ServiceGroups})
	}
}

func adminAddLLMServiceGroup(svc *llmservice.Service, cardStoreSvc *cardstore.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var group llmpool.ServiceGroup
		if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if group.ID == "" || group.Name == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "id and name are required"})
			return
		}
		if err := svc.AddServiceGroup(r.Context(), group); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := ensureDefaultComputeCardTypesForGrantGroup(r.Context(), svc, cardStoreSvc); err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminUpdateLLMServiceGroup(svc *llmservice.Service, cardStoreSvc *cardstore.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service group id required"})
			return
		}
		var group llmpool.ServiceGroup
		if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		group.ID = id
		if err := svc.UpdateServiceGroup(r.Context(), group); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := ensureDefaultComputeCardTypesForGrantGroup(r.Context(), svc, cardStoreSvc); err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func ensureDefaultComputeCardTypesForGrantGroup(ctx context.Context, svc *llmservice.Service, cardStoreSvc *cardstore.Service) error {
	if svc == nil || cardStoreSvc == nil {
		return nil
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	for _, group := range reg.ServiceGroups {
		if group.ID != "" && group.AccessPolicy == llmservice.AccessPolicyGrantRequired {
			return cardStoreSvc.EnsureDefaultComputeCardTypes(ctx, group.ID)
		}
	}
	return nil
}

func adminDeleteLLMServiceGroup(svc *llmservice.Service, checker *llmservice.AuthorizationChecker, cardStoreSvc *cardstore.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service group id required"})
			return
		}
		if checker != nil {
			auths, err := checker.ListByServiceGroup(r.Context(), id)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, auth := range auths {
				if auth != nil && auth.ServiceGroupID == llmservice.ExternalComputePermissionServiceGroupID {
					continue
				}
				if auth != nil && auth.ServiceGroupID == id {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("service group %s is used by tenant %s/%s and cannot be deleted", id, auth.HubID, auth.TenantID)})
					return
				}
			}
		}
		if cardStoreSvc != nil {
			_, total, err := cardStoreSvc.ListOrders(r.Context(), cardstore.OrderFilter{ServiceGroupID: id, Limit: 1, IncludeArchived: true})
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if total > 0 {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("service group %s has purchase orders and cannot be deleted", id)})
				return
			}
		}
		if cardStoreSvc != nil {
			cardTypes, err := cardStoreSvc.ListAllCardTypes(r.Context())
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, ct := range cardTypes {
				if ct != nil && ct.ServiceGroupID == id {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("service group %s is used by card type %s and cannot be deleted", id, ct.ID)})
					return
				}
			}
		}
		if err := svc.DeleteServiceGroup(r.Context(), id); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// LLM Authorization admin handlers
// ---------------------------------------------------------------------------

func adminListLLMAuthorizations(checker *llmservice.AuthorizationChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auths, err := checker.ListAll(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"authorizations": auths})
	}
}

func adminCreateLLMAuthorization(checker *llmservice.AuthorizationChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var auth llmservice.TenantAuthorization
		if err := json.NewDecoder(r.Body).Decode(&auth); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if auth.HubID == "" || auth.TenantID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "hub_id and tenant_id are required"})
			return
		}
		isExternalComputeGrant := auth.AllowExternalProviders || auth.Source == "external_provider_permission" || auth.ServiceGroupID == llmservice.ExternalComputePermissionServiceGroupID
		if auth.ServiceGroupID == "" && isExternalComputeGrant {
			auth.ServiceGroupID = llmservice.ExternalComputePermissionServiceGroupID
		}
		if auth.ServiceGroupID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service_group_id is required"})
			return
		}
		if isExternalComputeGrant {
			auth.Source = "external_provider_permission"
			if auth.Status == "" {
				auth.Status = "active"
			}
			if auth.CreditsTotal <= 0 {
				auth.CreditsTotal = 1000000000000
			}
			now := time.Now().UTC()
			if auth.StartsAt.IsZero() {
				auth.StartsAt = now
			}
			if auth.ExpiresAt.IsZero() {
				auth.ExpiresAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
			}
		}
		if auth.Source == "" {
			auth.Source = "admin_grant"
		}
		if auth.ID == "" {
			auth.ID = "auth_admin_" + auth.HubID + "_" + auth.TenantID + "_" + auth.ServiceGroupID
		}
		var supersededExternal []*llmservice.TenantAuthorization
		if isExternalComputeGrant {
			existing, err := checker.ListByHubTenant(r.Context(), auth.HubID, auth.TenantID)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, old := range existing {
				if old == nil || old.ID == auth.ID {
					continue
				}
				if !old.AllowExternalProviders && old.Source != "external_provider_permission" && old.ServiceGroupID != llmservice.ExternalComputePermissionServiceGroupID {
					continue
				}
				supersededExternal = append(supersededExternal, old)
			}
		}
		now := time.Now().UTC()
		if auth.CreatedAt.IsZero() {
			auth.CreatedAt = now
		}
		auth.UpdatedAt = now
		if err := checker.CreateAuthorization(r.Context(), &auth); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		for _, old := range supersededExternal {
			old.AllowExternalProviders = false
			old.Status = "expired"
			if err := checker.UpdateAuthorization(r.Context(), old); err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok", "id": auth.ID})
	}
}

// ---------------------------------------------------------------------------
// Usage Statistics handler
// ---------------------------------------------------------------------------

func adminLLMUsageHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := llmservice.UsageFilter{
			HubID:     q.Get("hub_id"),
			TenantID:  q.Get("tenant_id"),
			Model:     q.Get("model"),
			Period:    q.Get("period"),
			StartDate: firstNonEmpty(q.Get("start_date"), q.Get("start")),
			EndDate:   firstNonEmpty(q.Get("end_date"), q.Get("end")),
		}
		if limit := q.Get("limit"); limit != "" {
			if n, err := strconv.Atoi(limit); err == nil {
				filter.Limit = n
			}
		}
		if filter.Limit <= 0 {
			filter.Limit = 30
		}
		summary, err := statsSvc.QueryUsageSummary(r.Context(), filter)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Return as "rows" for compat with compute-market-tab.js frontend
		writeJSONResp(w, http.StatusOK, map[string]any{"usage": summary, "rows": summary})
	}
}

// ---------------------------------------------------------------------------
// Payment Config handlers
// ---------------------------------------------------------------------------

func adminGetPaymentConfig(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := llmSvc.GetSystemSetting(r.Context(), "llm_cardstore_payment_config")
		w.Header().Set("Content-Type", "application/json")
		if raw == "" {
			w.Write([]byte("{}"))
		} else {
			w.Write([]byte(raw))
		}
	}
}

func adminSavePaymentConfig(llmSvc *llmservice.Service, cardStoreSvc *cardstore.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
			return
		}
		if err := llmSvc.SetSystemSetting(r.Context(), "llm_cardstore_payment_config", string(body)); err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Reload payment config into card store service
		if cardStoreSvc != nil {
			var cfg struct {
				PaymentMode string                              `json:"payment_mode"`
				Personal    corecardstore.PersonalPaymentConfig `json:"personal_payment"`
				Alipay      corecardstore.AlipayDirectConfig    `json:"alipay_direct"`
			}
			if json.Unmarshal(body, &cfg) == nil {
				personal, alipay := effectiveCardStorePaymentConfig(cfg.PaymentMode, cfg.Personal, cfg.Alipay)
				cardStoreSvc.SetPaymentConfig(personal, alipay)
			}
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------

func effectiveCardStorePaymentConfig(mode string, personal corecardstore.PersonalPaymentConfig, alipay corecardstore.AlipayDirectConfig) (corecardstore.PersonalPaymentConfig, corecardstore.AlipayDirectConfig) {
	switch mode {
	case corecardstore.PaymentModeSemiManual:
		return personal, corecardstore.AlipayDirectConfig{}
	case corecardstore.PaymentModeAlipay:
		return corecardstore.PersonalPaymentConfig{}, alipay
	default:
		return personal, alipay
	}
}

func writeJSONResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
