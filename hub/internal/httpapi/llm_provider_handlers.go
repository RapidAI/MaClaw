package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
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
	Count           int      `json:"count"`
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

func UpdateLLMServicesAdminHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(body, &raw)
		_, cardsProvided := raw["cards"]

		var req llmservice.Registry
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		oldReg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		if !cardsProvided && oldReg != nil {
			req.Cards = append([]llmservice.RechargeCard(nil), oldReg.Cards...)
		} else {
			for i := range req.Cards {
				req.Cards[i].CodeHash = ""
			}
			preserveCardHashes(&req, oldReg)
		}
		req.Normalize()
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		if issues := collectLLMServiceProviderReferenceIssues(&req, providerReg); len(issues) > 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_PROVIDER_NOT_FOUND", strings.Join(issues, "; "))
			return
		}
		if issues := validateLLMServiceGroupReferences(&req); len(issues) > 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_NOT_FOUND", strings.Join(issues, "; "))
			return
		}
		knownSecurityGroups, err := collectSecurityGroupIDs(r.Context(), securitySvc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SECURITY_GROUP_LOAD_FAILED", err.Error())
			return
		}
		if issues := validateLLMServiceSecurityGroupReferences(&req, knownSecurityGroups); len(issues) > 0 {
			writeError(w, http.StatusBadRequest, "LLM_SECURITY_GROUP_NOT_FOUND", strings.Join(issues, "; "))
			return
		}
		if err := llmservice.SaveRegistry(r.Context(), system, &req); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toLLMServiceAdminResponse(r, &req, nil))
	}
}

func CreateLLMServiceCardHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
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
		count := req.Count
		if count <= 0 {
			count = 1
		}
		if count > 1000 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_COUNT_INVALID", "count must be between 1 and 1000")
			return
		}
		for _, id := range serviceGroupIDs {
			if reg.FindModelServiceGroup(id) == nil {
				writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_NOT_FOUND", "unknown service group: "+id)
				return
			}
		}
		days := req.DurationDays
		if days <= 0 {
			days = 30
		}
		credits := req.Credits
		if credits < 0 {
			credits = 0
		}
		existingHashes := make(map[string]struct{}, len(reg.Cards)+count)
		for _, card := range reg.Cards {
			if hash := strings.TrimSpace(card.CodeHash); hash != "" {
				existingHashes[hash] = struct{}{}
			}
		}
		cards := make([]llmservice.RechargeCard, 0, count)
		issuedCodes := make([]string, 0, count)
		for len(cards) < count {
			code, err := llmservice.GenerateCardCode()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
				return
			}
			if err := llmservice.ValidateCardCode(code); err != nil {
				writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
				return
			}
			hash := llmserviceHashCode(code)
			if _, exists := existingHashes[hash]; exists {
				continue
			}
			existingHashes[hash] = struct{}{}
			cards = append(cards, llmservice.RechargeCard{
				ID:              llmservice.NewID("card"),
				CodeHash:        hash,
				Label:           strings.TrimSpace(req.Label),
				ServiceGroupIDs: serviceGroupIDs,
				DurationDays:    days,
				Credits:         credits,
				CreatedAt:       time.Now().UTC(),
			})
			issuedCodes = append(issuedCodes, code)
		}
		reg.Cards = append(reg.Cards, cards...)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
			return
		}
		writeLLMServiceCardAdminAudit(r.Context(), audit, "llm.service_card.create", map[string]any{
			"label":             strings.TrimSpace(req.Label),
			"service_group_ids": append([]string(nil), serviceGroupIDs...),
			"duration_days":     days,
			"credits":           credits,
			"count":             len(cards),
			"created_ids":       collectRechargeCardIDs(cards),
		})
		cardResponses := make([]map[string]any, 0, len(cards))
		for i, card := range cards {
			cardResponses = append(cardResponses, map[string]any{
				"id":                card.ID,
				"label":             card.Label,
				"service_group_ids": card.ServiceGroupIDs,
				"duration_days":     card.DurationDays,
				"credits":           card.Credits,
				"created_at":        card.CreatedAt,
				"code":              issuedCodes[i],
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"card":  cardResponses[0],
			"cards": cardResponses,
		})
	}
}

func ListLLMServiceCardsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
		if statusFilter == "" {
			statusFilter = "all"
		}
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		page := parsePositiveInt(r.URL.Query().Get("page"), 1)
		pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
		if pageSize > 200 {
			pageSize = 200
		}
		cards := filterLLMServiceCards(reg.Cards, statusFilter, search)
		if cards == nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_STATUS_INVALID", "status must be one of: all, unused, redeemed")
			return
		}
		total := len(cards)
		totalPages := 1
		if total > 0 {
			totalPages = (total + pageSize - 1) / pageSize
		}
		if page > totalPages {
			page = totalPages
		}
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		items := make([]map[string]any, 0, end-start)
		for _, card := range cards[start:end] {
			items = append(items, map[string]any{
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
		writeJSON(w, http.StatusOK, map[string]any{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
	}
}

func ExportSelectedLLMServiceCardsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs    []string `json:"ids"`
			Format string   `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		ids := normalizeStringSlice(req.IDs)
		if len(ids) == 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_IDS_REQUIRED", "ids is required")
			return
		}
		format := strings.ToLower(strings.TrimSpace(req.Format))
		if format == "" {
			format = "txt"
		}
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		lookup := map[string]llmservice.RechargeCard{}
		for _, card := range reg.Cards {
			id := strings.TrimSpace(card.ID)
			if id != "" {
				lookup[strings.ToLower(id)] = card
			}
		}
		cards := make([]llmservice.RechargeCard, 0, len(ids))
		for _, id := range ids {
			if card, ok := lookup[strings.ToLower(strings.TrimSpace(id))]; ok {
				cards = append(cards, card)
			}
		}
		if len(cards) == 0 {
			writeError(w, http.StatusNotFound, "LLM_SERVICE_CARD_NOT_FOUND", "no matching service cards found")
			return
		}
		writeLLMServiceCardsExport(w, cards, format, "llm_service_cards_selected")
	}
}

func ExportLLMServiceCardsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
		if statusFilter == "" {
			statusFilter = "all"
		}
		format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
		if format == "" {
			format = "txt"
		}
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		cards := filterLLMServiceCards(reg.Cards, statusFilter, search)
		if cards == nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_STATUS_INVALID", "status must be one of: all, unused, redeemed")
			return
		}
		writeLLMServiceCardsExport(w, cards, format, "llm_service_cards_"+statusFilter)
	}
}

func DeleteLLMServiceCardHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_ID_REQUIRED", "id is required")
			return
		}
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		card, idx := reg.FindCardByID(id)
		if card == nil || idx < 0 {
			writeError(w, http.StatusNotFound, "LLM_SERVICE_CARD_NOT_FOUND", "service card not found")
			return
		}
		if card.RedeemedAt != nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_REDEEMED", "redeemed service card cannot be deleted")
			return
		}
		reg.Cards = append(reg.Cards[:idx], reg.Cards[idx+1:]...)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_DELETE_FAILED", err.Error())
			return
		}
		writeLLMServiceCardAdminAudit(r.Context(), audit, "llm.service_card.delete", map[string]any{"card_id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

func DeleteLLMServiceCardsBatchHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		ids := normalizeStringSlice(req.IDs)
		if len(ids) == 0 {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_IDS_REQUIRED", "ids is required")
			return
		}
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		removeSet := map[string]struct{}{}
		deleted := make([]string, 0, len(ids))
		skipped := make([]string, 0)
		for _, id := range ids {
			card, _ := reg.FindCardByID(id)
			if card == nil {
				skipped = append(skipped, id)
				continue
			}
			if card.RedeemedAt != nil {
				skipped = append(skipped, id)
				continue
			}
			removeSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
			deleted = append(deleted, id)
		}
		filtered := reg.Cards[:0]
		for _, card := range reg.Cards {
			if _, ok := removeSet[strings.ToLower(strings.TrimSpace(card.ID))]; ok {
				continue
			}
			filtered = append(filtered, card)
		}
		reg.Cards = filtered
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_DELETE_FAILED", err.Error())
			return
		}
		writeLLMServiceCardAdminAudit(r.Context(), audit, "llm.service_card.delete_batch", map[string]any{
			"requested_ids": append([]string(nil), ids...),
			"deleted_ids":   append([]string(nil), deleted...),
			"skipped_ids":   append([]string(nil), skipped...),
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted_ids": deleted, "skipped_ids": skipped})
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
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		status, filtered := filterAuthorizedModelsByProviderRegistry(status, status.AuthorizedModels, providerReg)
		if status != nil {
			status.InactiveReasons = explainFilteredServiceStatusIssues(status, filtered, providerReg)
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
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		status, filtered := filterAuthorizedModelsByProviderRegistry(status, status.AuthorizedModels, providerReg)
		if status != nil {
			status.InactiveReasons = explainFilteredServiceStatusIssues(status, filtered, providerReg)
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
		status, models, _, _, err := resolveAuthorizedModels(r.Context(), r, system, securitySvc, principal.Email)
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

func LLMV1ChatCompletionsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, promptCacheSources ...any) http.HandlerFunc {
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
		status, models, providerReg, serviceReg, err := resolveAuthorizedModels(r.Context(), r, system, securitySvc, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		billableModels, deniedByModel, deniedCode, deniedMessage := filterAuthorizedModelsByBillingEligibility(serviceReg, principal.Email, body, models)
		authorizedModel, requestedModel, err := resolveAuthorizedModel(body, billableModels)
		selectedModelDebug := explainModelSelection(body, billableModels, authorizedModel)
		if err != nil {
			if denial, ok := deniedByModel[strings.ToLower(strings.TrimSpace(requestedModel))]; ok && requestedModel != "" && !strings.EqualFold(requestedModel, "auto") && !strings.EqualFold(requestedModel, "default") {
				writeError(w, http.StatusForbidden, denial.Code, denial.Message)
				return
			}
			if len(models) > 0 && len(billableModels) == 0 && deniedCode != "" {
				writeError(w, http.StatusForbidden, deniedCode, deniedMessage)
				return
			}
			writeError(w, http.StatusForbidden, "LLM_MODEL_FORBIDDEN", err.Error())
			return
		}
		cacheCfg := LoadHubLLMPromptCacheConfig(r.Context(), system)
		applyHubLLMPromptCacheRuntimeConfig(firstPromptCacheSource(promptCacheSources), cacheCfg)
		respBody, statusCode, usedProviderID, chargedServiceGroupIDs, usageStat, localCacheHit, err := forwardAuthorizedModelRequestWithCache(r, providerReg, authorizedModel, body, requestedModel, firstPromptCacheSource(promptCacheSources), cacheCfg)
		if err != nil {
			writeError(w, http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error())
			return
		}
		if statusCode < 400 && usedProviderID != "" && !localCacheHit {
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
		if localCacheHit {
			w.Header().Set("X-MaClaw-Local-Cache", "hit")
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write(respBody)
	}
}

func forwardAuthorizedModelRequest(r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string) ([]byte, int, string, []string, error) {
	respBody, statusCode, providerID, serviceGroupIDs, _, _, err := forwardAuthorizedModelRequestWithCache(r, reg, model, body, externalModel, nil, defaultHubLLMPromptCacheConfig())
	return respBody, statusCode, providerID, serviceGroupIDs, err
}

func forwardAuthorizedModelRequestWithCache(r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, promptCacheSource any, cacheCfg HubLLMPromptCacheConfig) ([]byte, int, string, []string, corelib.TokenUsageStat, bool, error) {
	if model == nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("authorized model is required")
	}
	if reg == nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("provider registry is required")
	}
	promptCache := firstPromptCacheSource([]any{promptCacheSource})
	if respBody, statusCode, providerID, serviceGroupIDs, usageStat, ok, err := getCachedAuthorizedModelResponse(r.Context(), promptCache, model, body, externalModel, cacheCfg); err != nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, err
	} else if ok {
		return respBody, statusCode, providerID, serviceGroupIDs, usageStat, true, nil
	}
	var lastErr error
	var lastBody []byte
	var lastStatus int
	for _, providerID := range llmservice.OrderProvidersForRequest(body, model) {
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
		usageStat := parseUsageStats(respBody)
		serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, provider.ID)
		_ = putCachedAuthorizedModelResponse(r.Context(), promptCache, model, body, externalModel, respBody, statusCode, provider.ID, serviceGroupIDs, usageStat, cacheCfg)
		return respBody, statusCode, provider.ID, serviceGroupIDs, usageStat, false, nil
	}
	if lastBody != nil && lastStatus > 0 {
		return lastBody, lastStatus, "", nil, corelib.TokenUsageStat{}, false, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return nil, 0, "", nil, corelib.TokenUsageStat{}, false, lastErr
}

func firstPromptCacheSource(sources []any) llmPromptCacheStore {
	for _, source := range sources {
		if cache, ok := source.(llmPromptCacheStore); ok && cache != nil {
			return cache
		}
	}
	return nil
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
	includeCards := !strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_cards")), "false")
	cards := make([]map[string]any, 0)
	if includeCards {
		cards = make([]map[string]any, 0, len(reg.Cards))
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

func validateLLMServiceGroupReferences(reg *llmservice.Registry) []string {
	if reg == nil {
		return nil
	}
	issues := []string{}
	check := func(context string, ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if reg.FindModelServiceGroup(id) == nil {
				issues = append(issues, fmt.Sprintf("%s references unknown service group: %s", context, id))
			}
		}
	}
	check("new-user default grants", reg.DefaultNewUserServiceGroups)
	for _, binding := range reg.GroupBindings {
		label := strings.TrimSpace(binding.GroupID)
		if label == "" {
			label = "<empty>"
		}
		check(fmt.Sprintf("security group binding %q", label), binding.ServiceGroupIDs)
	}
	for _, binding := range reg.UserBindings {
		label := strings.TrimSpace(binding.Email)
		if label == "" {
			label = "<empty>"
		}
		check(fmt.Sprintf("user binding %q", label), binding.ServiceGroupIDs)
	}
	for _, card := range reg.Cards {
		label := strings.TrimSpace(card.Label)
		if label == "" {
			label = strings.TrimSpace(card.ID)
		}
		if label == "" {
			label = "<empty>"
		}
		check(fmt.Sprintf("service exchange card %q", label), card.ServiceGroupIDs)
	}
	for _, grant := range reg.Grants {
		id := strings.TrimSpace(grant.ServiceGroupID)
		if id == "" {
			continue
		}
		if reg.FindModelServiceGroup(id) == nil {
			label := strings.TrimSpace(grant.ID)
			if label == "" {
				label = strings.TrimSpace(grant.Email)
			}
			if label == "" {
				label = "<empty>"
			}
			issues = append(issues, fmt.Sprintf("grant %q references unknown service group: %s", label, id))
		}
	}
	return issues
}

func collectSecurityGroupIDs(ctx context.Context, securitySvc *security.SecurityService) (map[string]struct{}, error) {
	if securitySvc == nil {
		return nil, nil
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]struct{}{}
	var walk func(node *security.GroupTreeNode)
	walk = func(node *security.GroupTreeNode) {
		if node == nil {
			return
		}
		id := strings.TrimSpace(node.ID)
		if id != "" {
			known[strings.ToLower(id)] = struct{}{}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(tree)
	return known, nil
}

func validateLLMServiceSecurityGroupReferences(reg *llmservice.Registry, knownGroupIDs map[string]struct{}) []string {
	if reg == nil || len(knownGroupIDs) == 0 {
		return nil
	}
	issues := []string{}
	for _, binding := range reg.GroupBindings {
		id := strings.TrimSpace(binding.GroupID)
		if id == "" {
			continue
		}
		if _, ok := knownGroupIDs[strings.ToLower(id)]; ok {
			continue
		}
		issues = append(issues, fmt.Sprintf("service group binding references unknown security group: %s", id))
	}
	return issues
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

func resolveAuthorizedModels(ctx context.Context, r *http.Request, system store.SystemSettingsRepository, securitySvc *security.SecurityService, email string) (*llmservice.ServiceStatus, []llmservice.AuthorizedModel, *im.LLMProviderRegistry, *llmservice.Registry, error) {
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	status, models, err := llmservice.ResolveStatusFromRegistry(ctx, reg, securitySvc, email, externalLLMBaseURL(r))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	status, models = filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	return status, models, providerReg, reg, nil
}

type llmBillingDenial struct {
	Code    string
	Message string
}

func filterAuthorizedModelsByBillingEligibility(reg *llmservice.Registry, email string, body map[string]any, models []llmservice.AuthorizedModel) ([]llmservice.AuthorizedModel, map[string]llmBillingDenial, string, string) {
	filtered := make([]llmservice.AuthorizedModel, 0, len(models))
	denied := map[string]llmBillingDenial{}
	firstCode := ""
	firstMessage := ""
	for i := range models {
		eligibleModel, code, err := filterAuthorizedModelByBillingEligibility(reg, email, body, &models[i])
		if err != nil {
			denial := llmBillingDenial{Code: code, Message: err.Error()}
			denied[strings.ToLower(strings.TrimSpace(models[i].Name))] = denial
			if firstCode == "" {
				firstCode = code
				firstMessage = err.Error()
			}
			continue
		}
		filtered = append(filtered, *eligibleModel)
	}
	return filtered, denied, firstCode, firstMessage
}

func filterAuthorizedModelByBillingEligibility(reg *llmservice.Registry, email string, body map[string]any, model *llmservice.AuthorizedModel) (*llmservice.AuthorizedModel, string, error) {
	if model == nil || reg == nil {
		return model, "", nil
	}
	orderedProviders := llmservice.OrderProvidersForRequest(body, model)
	if len(orderedProviders) == 0 {
		orderedProviders = append([]string(nil), model.ProviderIDs...)
	}
	eligibleProviderIDs := make([]string, 0, len(orderedProviders))
	firstCode := ""
	firstMessage := ""
	now := time.Now().UTC()
	for _, providerID := range orderedProviders {
		allowed, code, message := billingEligibilityForProvider(reg, email, llmservice.ServiceGroupIDsForProvider(model, providerID), now)
		if allowed {
			eligibleProviderIDs = append(eligibleProviderIDs, providerID)
			continue
		}
		if firstCode == "" {
			firstCode = code
			firstMessage = message
		}
	}
	if len(eligibleProviderIDs) == 0 {
		if firstCode == "" {
			firstCode = "LLM_SERVICE_CREDITS_REQUIRED"
			firstMessage = "selected model requires an active grant with remaining credits"
		}
		return nil, firstCode, errors.New(firstMessage)
	}
	clone := *model
	clone.ProviderIDs = eligibleProviderIDs
	return &clone, "", nil
}

func billingEligibilityForProvider(reg *llmservice.Registry, email string, serviceGroupIDs []string, now time.Time) (bool, string, string) {
	allowed, _, code, message, _, _, _ := llmservice.BillingEligibilityForServiceGroups(reg, email, serviceGroupIDs, now)
	return allowed, code, message
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
func writeLLMServiceCardsExport(w http.ResponseWriter, cards []llmservice.RechargeCard, format, filenamePrefix string) {
	nowName := time.Now().Format("20060102_150405")
	switch format {
	case "txt":
		var sb strings.Builder
		for _, card := range cards {
			sb.WriteString(strings.TrimSpace(card.ID))
			sb.WriteByte(',')
			if card.RedeemedAt != nil {
				sb.WriteString("redeemed")
			} else {
				sb.WriteString("unused")
			}
			sb.WriteByte(',')
			sb.WriteString(strings.TrimSpace(card.Label))
			sb.WriteByte(',')
			sb.WriteString(strings.Join(card.ServiceGroupIDs, "/"))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%.3f", card.Credits))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%d", card.DurationDays))
			sb.WriteByte(',')
			sb.WriteString(card.CreatedAt.Format(time.RFC3339))
			sb.WriteByte(',')
			sb.WriteString(strings.TrimSpace(card.RedeemedByEmail))
			sb.WriteByte('\n')
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_%s.txt", filenamePrefix, nowName))
		w.Header().Set("X-Export-Count", fmt.Sprintf("%d", len(cards)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sb.String()))
	case "csv":
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		_ = writer.Write([]string{"id", "status", "label", "service_group_ids", "credits", "duration_days", "created_at", "redeemed_by_email", "redeemed_at"})
		for _, card := range cards {
			status := "unused"
			redeemedAt := ""
			if card.RedeemedAt != nil {
				status = "redeemed"
				redeemedAt = card.RedeemedAt.Format(time.RFC3339)
			}
			_ = writer.Write([]string{
				strings.TrimSpace(card.ID),
				status,
				strings.TrimSpace(card.Label),
				strings.Join(card.ServiceGroupIDs, ","),
				fmt.Sprintf("%.3f", card.Credits),
				fmt.Sprintf("%d", card.DurationDays),
				card.CreatedAt.Format(time.RFC3339),
				strings.TrimSpace(card.RedeemedByEmail),
				redeemedAt,
			})
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_EXPORT_FAILED", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_%s.csv", filenamePrefix, nowName))
		w.Header().Set("X-Export-Count", fmt.Sprintf("%d", len(cards)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	default:
		writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_FORMAT_INVALID", "format must be one of: txt, csv")
	}
}

func filterLLMServiceCards(cards []llmservice.RechargeCard, statusFilter, search string) []llmservice.RechargeCard {
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	if statusFilter == "" {
		statusFilter = "all"
	}
	if statusFilter != "all" && statusFilter != "unused" && statusFilter != "redeemed" {
		return nil
	}
	filtered := make([]llmservice.RechargeCard, 0, len(cards))
	for _, card := range cards {
		isRedeemed := card.RedeemedAt != nil
		switch statusFilter {
		case "unused":
			if isRedeemed {
				continue
			}
		case "redeemed":
			if !isRedeemed {
				continue
			}
		case "all":
		default:
			return nil
		}
		if !llmServiceCardMatchesSearch(card, search) {
			continue
		}
		filtered = append(filtered, card)
	}
	return filtered
}

func llmServiceCardMatchesSearch(card llmservice.RechargeCard, search string) bool {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return true
	}
	fields := []string{card.Label, card.ID, card.RedeemedByEmail}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), search) {
			return true
		}
	}
	return false
}

func collectRechargeCardIDs(cards []llmservice.RechargeCard) []string {
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		id := strings.TrimSpace(card.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func writeLLMServiceCardAdminAudit(ctx context.Context, audit store.AdminAuditRepository, action string, payload map[string]any) {
	if audit == nil {
		return
	}
	admin := AdminFromContext(ctx)
	if admin == nil || strings.TrimSpace(admin.ID) == "" {
		return
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte(`{}`)
	}
	_ = audit.Create(ctx, &store.AdminAuditLog{
		ID:          fmt.Sprintf("aa_%d", time.Now().UnixNano()),
		AdminUserID: strings.TrimSpace(admin.ID),
		Action:      action,
		PayloadJSON: string(payloadJSON),
		CreatedAt:   time.Now().UTC(),
	})
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
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
	stat := corelib.TokenUsageStat{
		InputTokens:       int64(numberToInt(firstNonNil(usage["prompt_tokens"], usage["input_tokens"]))),
		OutputTokens:      int64(numberToInt(firstNonNil(usage["completion_tokens"], usage["output_tokens"]))),
		TotalTokens:       int64(numberToInt(usage["total_tokens"])),
		CachedInputTokens: int64(numberToInt(cachedUsageValue(usage))),
		CacheWriteTokens:  int64(numberToInt(cacheWriteUsageValue(usage))),
		Requests:          1,
	}
	if stat.TotalTokens <= 0 {
		stat.TotalTokens = stat.InputTokens + stat.OutputTokens
	}
	if stat.CachedInputTokens > 0 || stat.CacheWriteTokens > 0 {
		stat.CachedRequests = 1
	}
	return stat
}

func cachedUsageValue(usage map[string]any) any {
	return firstNonNil(
		lookupMapValue(usage, "prompt_tokens_details", "cached_tokens"),
		lookupMapValue(usage, "input_tokens_details", "cached_tokens"),
		usage["cache_read_input_tokens"],
		usage["cached_input_tokens"],
	)
}

func cacheWriteUsageValue(usage map[string]any) any {
	return firstNonNil(
		usage["cache_creation_input_tokens"],
		usage["cache_write_input_tokens"],
		lookupMapValue(usage, "prompt_tokens_details", "cache_write_tokens"),
		lookupMapValue(usage, "input_tokens_details", "cache_write_tokens"),
	)
}

func lookupMapValue(root map[string]any, key string, nested string) any {
	if root == nil {
		return nil
	}
	child, _ := root[key].(map[string]any)
	if child == nil {
		return nil
	}
	return child[nested]
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
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
