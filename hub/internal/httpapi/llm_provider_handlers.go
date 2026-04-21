package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type llmProviderRegistryResponse struct {
	Enabled                bool     `json:"enabled"`
	CurrentProviderID      string   `json:"current_provider_id"`
	SmartRouteSingleDevice bool     `json:"smart_route_single_device"`
	Providers              []any    `json:"providers"`
	ExposeAPIBaseURL       string   `json:"expose_api_base_url"`
	ExposeBaseURL          string   `json:"expose_base_url"`
	ExposeModelsURL        string   `json:"expose_models_url"`
	AvailableModels        []string `json:"available_models"`
	AuthMode               string   `json:"auth_mode"`
	AuthHint               string   `json:"auth_hint"`
	Hints                  []string `json:"hints,omitempty"`
	Warnings               []string `json:"warnings,omitempty"`
}

type llmServiceAdminResponse struct {
	ModelServiceGroups          []llmservice.ModelServiceGroup `json:"model_service_groups"`
	GroupBindings               []llmservice.GroupBinding      `json:"group_bindings,omitempty"`
	UserBindings                []llmservice.UserBinding       `json:"user_bindings,omitempty"`
	Cards                       []map[string]any               `json:"cards,omitempty"`
	Grants                      []llmservice.Grant             `json:"grants,omitempty"`
	DefaultNewUserServiceGroups []string                       `json:"default_new_user_service_groups,omitempty"`
	DefaultNewUserDurationDays  int                            `json:"default_new_user_duration_days,omitempty"`
	TokensPerCredit             int                            `json:"tokens_per_credit,omitempty"`
	ExposeAPIBaseURL            string                         `json:"expose_api_base_url,omitempty"`
	ExposeBaseURL               string                         `json:"expose_base_url,omitempty"`
	ExposeModelsURL             string                         `json:"expose_models_url,omitempty"`
	AvailableModels             []string                       `json:"available_models,omitempty"`
	ProviderLinkIssues          []string                       `json:"provider_link_issues,omitempty"`
}

type createLLMServiceCardRequest struct {
	Label           string   `json:"label"`
	ServiceGroupIDs []string `json:"service_group_ids"`
	DurationDays    int      `json:"duration_days"`
	Credits         float64  `json:"credits"`
}

type redeemLLMServiceCardRequest struct {
	Code string `json:"code"`
}

func GetLLMProvidersHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		serviceReg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, registryResponse(r, reg, collectLLMServiceProviderReferenceIssues(serviceReg, reg)))
	}
}

func UpdateLLMProvidersHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req im.LLMProviderRegistry
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		oldReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		for i := range req.Providers {
			p := &req.Providers[i]
			p.ID = strings.TrimSpace(p.ID)
			p.Name = strings.TrimSpace(p.Name)
			p.APIURL = strings.TrimSpace(p.APIURL)
			p.Model = strings.TrimSpace(p.Model)
			p.Protocol = normalizeProviderProtocol(p.Protocol)
			p.WireAPI = normalizeProviderWireAPI(p.WireAPI)
			p.AgentType = strings.TrimSpace(p.AgentType)
			if p.MaxConcurrency < 0 {
				p.MaxConcurrency = 0
			}
			if p.ID == "" {
				writeError(w, http.StatusBadRequest, "LLM_PROVIDER_ID_REQUIRED", "Provider id is required")
				return
			}
			if p.Name == "" {
				p.Name = p.ID
			}
			if p.APIKey == "" && oldReg != nil {
				if old := oldReg.FindProvider(p.ID); old != nil {
					p.APIKey = old.APIKey
				}
			}
		}
		if req.TokenUsage == nil && oldReg != nil {
			req.TokenUsage = oldReg.TokenUsage
		}
		serviceReg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		if issues := collectLLMServiceProviderReferenceIssues(serviceReg, &req); len(issues) > 0 {
			writeError(w, http.StatusBadRequest, "LLM_PROVIDER_IN_USE", strings.Join(issues, "; "))
			return
		}
		if err := im.SaveLLMProviderRegistry(r.Context(), system, &req); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_SAVE_FAILED", err.Error())
			return
		}
		_ = syncLegacyHubLLMConfig(r.Context(), system, &req)
		writeJSON(w, http.StatusOK, registryResponse(r, &req, collectLLMServiceProviderReferenceIssues(serviceReg, &req)))
	}
}

func TestLLMProviderHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req im.LLMProvider
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if strings.TrimSpace(req.APIKey) == "" && strings.TrimSpace(req.ID) != "" {
			if reg, err := im.LoadLLMProviderRegistry(r.Context(), system); err == nil {
				if old := reg.FindProvider(req.ID); old != nil {
					req.APIKey = old.APIKey
				}
			}
		}
		if strings.TrimSpace(req.APIURL) == "" || strings.TrimSpace(req.APIKey) == "" || strings.TrimSpace(req.Model) == "" {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "provider missing api_url/api_key/model"})
			return
		}
		cfg := corelib.MaclawLLMConfig{
			URL:       req.APIURL,
			Key:       req.APIKey,
			Model:     req.Model,
			Protocol:  normalizeProviderProtocol(req.Protocol),
			WireAPI:   normalizeProviderWireAPI(req.WireAPI),
			AgentType: strings.TrimSpace(req.AgentType),
		}
		messages := []interface{}{map[string]string{"role": "user", "content": "Reply with exactly: pong"}}
		start := time.Now()
		resp, err := agent.DoSimpleLLMRequest(cfg, messages, http.DefaultClient, 12*time.Second)
		elapsed := time.Since(start)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error(), "latency_ms": elapsed.Milliseconds()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "reply": resp.Content, "latency_ms": elapsed.Milliseconds()})
	}
}

func GetLLMServicesAdminHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toLLMServiceAdminResponse(r, reg, collectLLMServiceProviderReferenceIssues(reg, providerReg)))
	}
}

func UpdateLLMServicesAdminHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req llmservice.Registry
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		for i := range req.Cards {
			req.Cards[i].CodeHash = ""
		}
		oldReg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		preserveCardHashes(&req, oldReg)
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		if issues := collectLLMServiceProviderReferenceIssues(&req, providerReg); len(issues) > 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_PROVIDER_NOT_FOUND", strings.Join(issues, "; "))
			return
		}
		if err := llmservice.SaveRegistry(r.Context(), system, &req); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toLLMServiceAdminResponse(r, &req, nil))
	}
}

func CreateLLMServiceCardHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createLLMServiceCardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		serviceGroupIDs := normalizeStringSlice(req.ServiceGroupIDs)
		if len(serviceGroupIDs) == 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_REQUIRED", "service_group_ids is required")
			return
		}
		for _, id := range serviceGroupIDs {
			if reg.FindModelServiceGroup(id) == nil {
				writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_NOT_FOUND", "unknown service group: "+id)
				return
			}
		}
		code, err := llmservice.GenerateCardCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
			return
		}
		days := req.DurationDays
		if days <= 0 {
			days = 30
		}
		card := llmservice.RechargeCard{
			ID:              llmservice.NewID("card"),
			CodeHash:        llmserviceHashCode(code),
			Label:           strings.TrimSpace(req.Label),
			ServiceGroupIDs: serviceGroupIDs,
			DurationDays:    days,
			Credits:         req.Credits,
			CreatedAt:       time.Now().UTC(),
		}
		reg.Cards = append(reg.Cards, card)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"card": map[string]any{
				"id":                card.ID,
				"label":             card.Label,
				"service_group_ids": card.ServiceGroupIDs,
				"duration_days":     card.DurationDays,
				"credits":           card.Credits,
				"created_at":        card.CreatedAt,
				"code":              code,
			},
		})
	}
}

func GetLLMServiceStatusHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		status, err := llmservice.ResolveServiceStatus(r.Context(), system, securitySvc, principal.Email, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func GetLLMServiceEntitlementDiagnosticHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeError(w, http.StatusBadRequest, "EMAIL_REQUIRED", "email is required")
			return
		}
		diagnostic, err := llmservice.ExplainEntitlementDiagnostic(r.Context(), system, securitySvc, email, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_DIAGNOSTIC_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, diagnostic)
	}
}

func RedeemLLMServiceCardHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req redeemLLMServiceCardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		status, err := llmservice.RedeemCard(r.Context(), system, securitySvc, principal.Email, req.Code, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_REDEEM_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "service_status": status})
	}
}

func LLMV1ModelsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		status, models, _, err := resolveAuthorizedModels(r.Context(), r, system, securitySvc, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(models))
		for _, m := range models {
			items = append(items, map[string]any{
				"id":                m.Name,
				"object":            "model",
				"owned_by":          "hub",
				"service_mode":      "hub",
				"provider_ids":      m.ProviderIDs,
				"capability_tags":   m.CapabilityTags,
				"priority":          m.Priority,
				"resolution_tier":   m.ResolutionTier,
				"credit_multiplier": m.CreditMultiplier,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items, "service_status": status})
	}
}

func LLMV1ChatCompletionsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
			return
		}
		var body map[string]any
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		status, models, providerReg, err := resolveAuthorizedModels(r.Context(), r, system, securitySvc, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		authorizedModel, requestedModel, err := resolveAuthorizedModel(body, models)
		selectedModelDebug := explainModelSelection(body, models, authorizedModel)
		if err != nil {
			writeError(w, http.StatusForbidden, "LLM_MODEL_FORBIDDEN", err.Error())
			return
		}
		respBody, statusCode, usedProviderID, chargedServiceGroupIDs, err := forwardAuthorizedModelRequest(r, providerReg, authorizedModel, body, requestedModel)
		if err != nil {
			writeError(w, http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error())
			return
		}
		if statusCode < 400 && usedProviderID != "" {
			usageStat := parseUsageStats(respBody)
			credits := llmservice.EstimateCredits(usageStat.TotalTokens, llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID), status.TokensPerCredit)
			userGroupIDs := []string(nil)
			if securitySvc != nil {
				if resolved, resolveErr := securitySvc.ResolveUserGroupChain(r.Context(), principal.Email); resolveErr == nil {
					userGroupIDs = resolved
				}
			}
			enqueueLLMUsage(system, usedProviderID, usageStat, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
		}
		w.Header().Set("Content-Type", "application/json")
		if authorizedModel != nil {
			w.Header().Set("X-MaClaw-Authorized-Model", authorizedModel.Name)
		}
		if selectedModelDebug != nil {
			if selectedModelDebug.SelectionReason != "" {
				w.Header().Set("X-MaClaw-Model-Selection", selectedModelDebug.SelectionReason)
			}
			if len(selectedModelDebug.CapabilityNeeds) > 0 {
				w.Header().Set("X-MaClaw-Model-Needs", strings.Join(selectedModelDebug.CapabilityNeeds, ","))
			}
		}
		if usedProviderID != "" {
			w.Header().Set("X-MaClaw-Upstream-Provider", usedProviderID)
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write(respBody)
	}
}

func registryResponse(r *http.Request, reg *im.LLMProviderRegistry, warnings []string) llmProviderRegistryResponse {
	providers := make([]any, 0, len(reg.Providers))
	availableModels := make([]string, 0, len(reg.Providers))
	seenModels := map[string]struct{}{}
	for _, p := range reg.Providers {
		usage := corelib.TokenUsageStat{}
		if stat := reg.TokenUsage[p.ID]; stat != nil {
			usage = *stat
		}
		wireAPI := normalizeProviderWireAPI(p.WireAPI)
		snapshot := globalProviderConcurrency.snapshot(p.ID, p.MaxConcurrency)
		providers = append(providers, map[string]any{
			"id":              p.ID,
			"name":            p.Name,
			"api_url":         p.APIURL,
			"api_key":         "",
			"has_api_key":     p.APIKey != "",
			"model":           p.Model,
			"protocol":        normalizeProviderProtocol(p.Protocol),
			"wire_api":        wireAPI,
			"agent_type":      strings.TrimSpace(p.AgentType),
			"max_concurrency": p.MaxConcurrency,
			"in_flight":       snapshot.InFlight,
			"queue_waiters":   snapshot.QueueWaiters,
			"usage":           usage,
		})
		if _, ok := seenModels[p.ID]; !ok && p.ID != "" {
			availableModels = append(availableModels, p.ID)
			seenModels[p.ID] = struct{}{}
		}
	}
	sort.Strings(availableModels)
	base := externalLLMBaseURL(r)
	mergedHints := []string{
		"Use model=<provider id> to select a configured provider on the unified endpoint.",
		"If model is omitted, the current default provider is used.",
		"Public LLM endpoints require a viewer Bearer token from hub email sign-in; do not distribute upstream provider API keys.",
	}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		mergedHints = append(mergedHints, "Warning: "+warning)
	}
	return llmProviderRegistryResponse{
		Enabled:                reg.Enabled,
		CurrentProviderID:      reg.CurrentProviderID,
		SmartRouteSingleDevice: reg.SmartRouteSingleDevice,
		Providers:              providers,
		ExposeAPIBaseURL:       base,
		ExposeBaseURL:          base + "/chat/completions",
		ExposeModelsURL:        base + "/models",
		AvailableModels:        availableModels,
		AuthMode:               "viewer_bearer_token",
		AuthHint:               "Use Authorization: Bearer <viewer access token>. Reuse the access_token returned by /api/auth/email-confirm or /api/auth/email-poll after email sign-in.",
		Hints:                  mergedHints,
		Warnings:               append([]string(nil), warnings...),
	}
}

func toLLMServiceAdminResponse(r *http.Request, reg *llmservice.Registry, providerLinkIssues []string) llmServiceAdminResponse {
	cards := make([]map[string]any, 0, len(reg.Cards))
	for _, card := range reg.Cards {
		cards = append(cards, map[string]any{
			"id":                card.ID,
			"label":             card.Label,
			"service_group_ids": card.ServiceGroupIDs,
			"duration_days":     card.DurationDays,
			"credits":           card.Credits,
			"created_at":        card.CreatedAt,
			"redeemed_by_email": card.RedeemedByEmail,
			"redeemed_at":       card.RedeemedAt,
		})
	}
	availableModels := make([]string, 0)
	seenModels := map[string]struct{}{}
	for _, group := range reg.ModelServiceGroups {
		for _, model := range group.Models {
			name := strings.TrimSpace(model.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seenModels[key]; ok {
				continue
			}
			seenModels[key] = struct{}{}
			availableModels = append(availableModels, name)
		}
	}
	sort.Strings(availableModels)
	baseURL := externalLLMBaseURL(r)
	return llmServiceAdminResponse{
		ModelServiceGroups:          reg.ModelServiceGroups,
		GroupBindings:               reg.GroupBindings,
		UserBindings:                reg.UserBindings,
		Cards:                       cards,
		Grants:                      reg.Grants,
		DefaultNewUserServiceGroups: append([]string(nil), reg.DefaultNewUserServiceGroups...),
		DefaultNewUserDurationDays:  reg.DefaultNewUserDurationDays,
		TokensPerCredit:             reg.TokensPerCredit,
		ExposeAPIBaseURL:            baseURL,
		ExposeBaseURL:               strings.TrimRight(baseURL, "/") + "/chat/completions",
		ExposeModelsURL:             strings.TrimRight(baseURL, "/") + "/models",
		AvailableModels:             availableModels,
		ProviderLinkIssues:          append([]string(nil), providerLinkIssues...),
	}
}

func preserveCardHashes(next *llmservice.Registry, old *llmservice.Registry) {
	if next == nil || old == nil {
		return
	}
	oldByID := map[string]string{}
	for _, card := range old.Cards {
		oldByID[strings.TrimSpace(card.ID)] = strings.TrimSpace(card.CodeHash)
		if strings.TrimSpace(card.ID) == "" && strings.TrimSpace(card.CodeHash) != "" {
			oldByID[strings.TrimSpace(card.CodeHash)] = strings.TrimSpace(card.CodeHash)
		}
	}
	for i := range next.Cards {
		if strings.TrimSpace(next.Cards[i].CodeHash) != "" {
			continue
		}
		if hash := oldByID[strings.TrimSpace(next.Cards[i].ID)]; hash != "" {
			next.Cards[i].CodeHash = hash
		}
	}
}

func resolveAuthorizedModels(ctx context.Context, r *http.Request, system store.SystemSettingsRepository, securitySvc *security.SecurityService, email string) (*llmservice.ServiceStatus, []llmservice.AuthorizedModel, *im.LLMProviderRegistry, error) {
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, err
	}
	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, err
	}
	status, models, err := llmservice.ResolveStatusFromRegistry(ctx, reg, securitySvc, email, externalLLMBaseURL(r))
	if err != nil {
		return nil, nil, nil, err
	}
	status, models = filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	return status, models, providerReg, nil
}

func resolveAuthorizedModel(body map[string]any, models []llmservice.AuthorizedModel) (*llmservice.AuthorizedModel, string, error) {
	requestedModel, _ := body["model"].(string)
	requestedModel = strings.TrimSpace(requestedModel)
	if len(models) == 0 {
		return nil, requestedModel, fmt.Errorf("no active model service entitlement")
	}
	if requestedModel == "" || strings.EqualFold(requestedModel, "auto") || strings.EqualFold(requestedModel, "default") {
		selected := llmservice.SelectBestModelForRequest(body, models)
		if selected == nil {
			selected = &models[0]
		}
		return selected, selected.Name, nil
	}
	for i := range models {
		if strings.EqualFold(strings.TrimSpace(models[i].Name), requestedModel) {
			return &models[i], requestedModel, nil
		}
	}
	return nil, requestedModel, fmt.Errorf("model %q is not authorized for this account", requestedModel)
}

func explainModelSelection(body map[string]any, models []llmservice.AuthorizedModel, selected *llmservice.AuthorizedModel) *llmservice.ModelSelectionDebug {
	if selected == nil {
		return nil
	}
	_, debug := llmservice.SelectBestModelForRequestWithDebug(body, models)
	if debug != nil && strings.EqualFold(strings.TrimSpace(debug.SelectedModel), strings.TrimSpace(selected.Name)) {
		return debug
	}
	return &llmservice.ModelSelectionDebug{
		SelectedModel:    selected.Name,
		MatchedTags:      append([]string(nil), selected.CapabilityTags...),
		Priority:         selected.Priority,
		ResolutionTier:   selected.ResolutionTier,
		CreditMultiplier: selected.CreditMultiplier,
		SelectionReason:  "manual model selection",
	}
}

func forwardAuthorizedModelRequest(r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string) ([]byte, int, string, []string, error) {
	if model == nil {
		return nil, 0, "", nil, fmt.Errorf("authorized model is required")
	}
	if reg == nil {
		return nil, 0, "", nil, fmt.Errorf("provider registry is required")
	}
	var lastErr error
	var lastBody []byte
	var lastStatus int
	for _, providerID := range model.ProviderIDs {
		provider := reg.FindProvider(providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %q not configured", providerID)
			continue
		}
		respBody, statusCode, err := forwardLLMRequest(r, provider, body, externalModel)
		if err != nil {
			lastErr = err
			continue
		}
		if statusCode >= 500 {
			lastBody = respBody
			lastStatus = statusCode
			lastErr = fmt.Errorf("provider %q returned http %d", provider.ID, statusCode)
			continue
		}
		return respBody, statusCode, provider.ID, llmservice.ServiceGroupIDsForProvider(model, provider.ID), nil
	}
	if lastBody != nil && lastStatus > 0 {
		return lastBody, lastStatus, "", nil, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return nil, 0, "", nil, lastErr
}

func normalizeStringSlice(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
func normalizeProviderProtocol(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

func normalizeProviderWireAPI(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "responses", "responses-ws":
		return v
	default:
		return "chat"
	}
}

func syncLegacyHubLLMConfig(ctx context.Context, system store.SystemSettingsRepository, reg *im.LLMProviderRegistry) error {
	cfg := reg.ToHubLLMConfig()
	if cfg == nil {
		cfg = &im.HubLLMConfig{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return system.Set(ctx, hubLLMConfigKey, string(data))
}

func forwardLLMRequest(r *http.Request, p *im.LLMProvider, body map[string]any, externalModel string) ([]byte, int, error) {
	if p == nil {
		return nil, 0, fmt.Errorf("provider is required")
	}
	release, err := globalProviderConcurrency.acquire(r.Context(), p.ID, p.MaxConcurrency)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	cfg := corelib.MaclawLLMConfig{
		URL:       p.APIURL,
		Key:       p.APIKey,
		Model:     p.Model,
		Protocol:  normalizeProviderProtocol(p.Protocol),
		WireAPI:   normalizeProviderWireAPI(p.WireAPI),
		AgentType: strings.TrimSpace(p.AgentType),
	}
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	return corelib.ForwardOpenAICompatRequest(r.Context(), cfg, fwd, http.DefaultClient, externalModel)
}

func parseUsageStats(respBody []byte) corelib.TokenUsageStat {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return corelib.TokenUsageStat{}
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return corelib.TokenUsageStat{}
	}
	return corelib.TokenUsageStat{
		InputTokens:  int64(numberToInt(usage["prompt_tokens"])),
		OutputTokens: int64(numberToInt(usage["completion_tokens"])),
		TotalTokens:  int64(numberToInt(usage["total_tokens"])),
	}
}

func numberToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func externalLLMBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host + "/api/llm/v1"
}

func llmserviceHashCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	// Keep hashing local to the HTTP layer so admin callers never need to send raw hashes.
	return llmservice.HashCode(code)
}
