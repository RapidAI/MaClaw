package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var globalLLMEndpointRequestSeq uint64

type llmProviderRegistryResponse struct {
	Enabled                  bool     `json:"enabled"`
	CurrentProviderID        string   `json:"current_provider_id"`
	SmartRouteSingleDevice   bool     `json:"smart_route_single_device"`
	DownstreamMaxConcurrency int      `json:"downstream_max_concurrency"`
	UpstreamTimeoutSec       int      `json:"upstream_timeout_sec"`
	UserRateLimitPerMinute   int      `json:"user_rate_limit_per_minute"`
	UserRateLimitBurst       int      `json:"user_rate_limit_burst"`
	Providers                []any    `json:"providers"`
	ExposeAPIBaseURL         string   `json:"expose_api_base_url"`
	ExposeBaseURL            string   `json:"expose_base_url"`
	ExposeModelsURL          string   `json:"expose_models_url"`
	AvailableModels          []string `json:"available_models"`
	AuthMode                 string   `json:"auth_mode"`
	AuthHint                 string   `json:"auth_hint"`
	Hints                    []string `json:"hints,omitempty"`
	Warnings                 []string `json:"warnings,omitempty"`
	// ServiceAvailable is the single authoritative signal for whether the LLM
	// service has at least one routable path (local providers OR built-in
	// providers via HubCenter). Frontend uses this instead of checking
	// len(providers)==0 to decide whether to show "no provider" warnings.
	ServiceAvailable bool `json:"service_available"`
}

type llmServiceAdminResponse struct {
	ModelServiceGroups          []llmservice.ModelServiceGroup `json:"model_service_groups"`
	GlobalServiceGroupIDs       []string                       `json:"global_service_group_ids,omitempty"`
	GroupBindings               []llmservice.GroupBinding      `json:"group_bindings,omitempty"`
	UserBindings                []llmservice.UserBinding       `json:"user_bindings,omitempty"`
	Cards                       []map[string]any               `json:"cards,omitempty"`
	Grants                      []llmservice.Grant             `json:"grants,omitempty"`
	SystemDefaultServiceGroupID string                         `json:"system_default_service_group_id,omitempty"`
	DefaultNewUserServiceGroups []string                       `json:"default_new_user_service_groups,omitempty"`
	DefaultNewUserDurationDays  int                            `json:"default_new_user_duration_days,omitempty"`
	DefaultNewUserCredits       float64                        `json:"default_new_user_credits,omitempty"`
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
	FiveHourCredits float64  `json:"five_hour_credits"`
	DailyCredits    float64  `json:"daily_credits"`
	WeeklyCredits   float64  `json:"weekly_credits"`
	MonthlyCredits  float64  `json:"monthly_credits"`
	Count           int      `json:"count"`
}

type redeemLLMServiceCardRequest struct {
	Code string `json:"code"`
}

var llmServiceCardDefaultCreditsByDuration = map[int]float64{
	1:   300,
	7:   1200,
	30:  5000,
	91:  17000,
	365: 70000,
}

type llmServiceAccountResponse struct {
	Email    string                    `json:"email"`
	TenantID string                    `json:"tenant_id,omitempty"`
	Status   *llmservice.ServiceStatus `json:"status,omitempty"`
	Usage    llmUsageCounters          `json:"usage"`
}

type llmProviderTestKeyRequest struct {
	Email string `json:"email"`
}

func GetLLMProvidersHandler(system store.SystemSettingsRepository, accessCtrl *llmservice.TenantLLMAccessControl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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
		visibleReg := filterLLMProviderRegistryForRequest(r, currentMaClawAccessControl(accessCtrl), reg)
		writeJSON(w, http.StatusOK, registryResponse(r, visibleReg, serviceReg, collectLLMServiceProviderReferenceIssues(serviceReg, visibleReg)))
	}
}

func UpdateLLMProvidersHandler(system store.SystemSettingsRepository, accessCtrl *llmservice.TenantLLMAccessControl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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
			if p.MaxQueueWaiters < 0 {
				p.MaxQueueWaiters = 0
			}
			if p.QueueTimeoutMS < 0 {
				p.QueueTimeoutMS = 0
			}
			if p.UpstreamTimeoutSec <= 0 {
				p.UpstreamTimeoutSec = im.DefaultLLMProviderUpstreamTimeoutSec
			}
			if p.UpstreamTimeoutSec < 300 {
				p.UpstreamTimeoutSec = 300
			}
			if p.CircuitBreakerThreshold <= 0 {
				p.CircuitBreakerThreshold = im.DefaultLLMProviderCircuitBreakerThreshold
			}
			if p.CircuitBreakerCooldownMS <= 0 {
				p.CircuitBreakerCooldownMS = im.DefaultLLMProviderCircuitBreakerCooldownMS
			}
			if p.FailureBackoffBaseMS <= 0 {
				p.FailureBackoffBaseMS = im.DefaultLLMProviderFailureBackoffBaseMS
			}
			if p.FailureBackoffMaxMS <= 0 {
				p.FailureBackoffMaxMS = im.DefaultLLMProviderFailureBackoffMaxMS
			}
			if p.FailureBackoffMaxMS < p.FailureBackoffBaseMS {
				p.FailureBackoffMaxMS = p.FailureBackoffBaseMS
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
		if llmProviderRegistryAddsProviders(oldReg, &req) {
			tenantID := RequestTenantID(r)
			if tenantID == "" {
				tenantID = store.DefaultTenantID
			}
			currentAccessCtrl := currentMaClawAccessControl(accessCtrl)
			if currentAccessCtrl == nil {
				writeError(w, http.StatusForbidden, "LLM_EXTERNAL_PROVIDER_NOT_GRANTED", "需要获得 MaClaw 官方算力模块授权才能添加自定义 LLM 服务。请联系 MaClaw 官方获取授权。")
				return
			}
			if ok, message := canAddExternalProviderForMutation(r.Context(), currentAccessCtrl, store.NormalizeTenantID(tenantID)); !ok {
				writeError(w, http.StatusForbidden, "LLM_EXTERNAL_PROVIDER_NOT_GRANTED", message)
				return
			}
		}
		if req.TokenUsage == nil && oldReg != nil {
			req.TokenUsage = oldReg.TokenUsage
		}
		req.TokenUsage = filterRemoteCodingToolTokenUsage(req.TokenUsage)
		if req.DownstreamMaxConcurrency <= 0 {
			if oldReg != nil && oldReg.DownstreamMaxConcurrency > 0 {
				req.DownstreamMaxConcurrency = oldReg.DownstreamMaxConcurrency
			} else {
				req.DownstreamMaxConcurrency = im.DefaultLLMProviderDownstreamMaxConcurrency
			}
		}
		if req.UpstreamTimeoutSec <= 0 {
			if oldReg != nil && oldReg.UpstreamTimeoutSec > 0 {
				req.UpstreamTimeoutSec = oldReg.UpstreamTimeoutSec
			} else {
				req.UpstreamTimeoutSec = im.DefaultLLMProviderUpstreamTimeoutSec
			}
		}
		if req.UpstreamTimeoutSec < 300 {
			req.UpstreamTimeoutSec = 300
		}
		if req.UserRateLimitPerMinute <= 0 {
			if oldReg != nil && oldReg.UserRateLimitPerMinute > 0 {
				req.UserRateLimitPerMinute = oldReg.UserRateLimitPerMinute
			} else {
				req.UserRateLimitPerMinute = im.DefaultLLMProviderUserRateLimitPerMinute
			}
		}
		if req.UserRateLimitBurst <= 0 {
			if oldReg != nil && oldReg.UserRateLimitBurst > 0 {
				req.UserRateLimitBurst = oldReg.UserRateLimitBurst
			} else {
				req.UserRateLimitBurst = im.DefaultLLMProviderUserRateLimitBurst
			}
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
		invalidateLLMRuntimeCaches(system)
		if shouldReloadSharedRuntimeForRequest(r) {
			applyLLMEndpointDownstreamConfig(&req)
			applyLLMEndpointUserRateLimitConfig(&req)
			applyMaClawUpstreamTimeout(&req)
		}
		_ = syncLegacyHubLLMConfig(r.Context(), system, &req)
		writeJSON(w, http.StatusOK, registryResponse(r, &req, serviceReg, collectLLMServiceProviderReferenceIssues(serviceReg, &req)))
	}
}

func llmProviderRegistryAddsProviders(oldReg *im.LLMProviderRegistry, req *im.LLMProviderRegistry) bool {
	if req == nil {
		return false
	}
	for _, p := range req.Providers {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		if oldReg == nil || oldReg.FindProvider(id) == nil {
			return true
		}
	}
	return false
}

func canAddExternalProviderForMutation(ctx context.Context, accessCtrl *llmservice.TenantLLMAccessControl, tenantID string) (bool, string) {
	ok, message := accessCtrl.CanAddExternalProvider(ctx, tenantID)
	if ok {
		return true, ""
	}
	if status, err := accessCtrl.RefreshAuthorizationStatus(ctx, tenantID); err == nil && status != nil && status.AllowExternalProviders {
		return true, ""
	}
	return false, message
}

func canViewExternalProvidersForRequest(ctx context.Context, accessCtrl *llmservice.TenantLLMAccessControl, tenantID string) bool {
	ok, _ := accessCtrl.CanAddExternalProvider(ctx, tenantID)
	if ok {
		return true
	}
	status, err := accessCtrl.RefreshAuthorizationStatus(ctx, tenantID)
	return err == nil && status != nil && status.AllowExternalProviders
}

func filterLLMProviderRegistryForRequest(r *http.Request, accessCtrl *llmservice.TenantLLMAccessControl, reg *im.LLMProviderRegistry) *im.LLMProviderRegistry {
	if reg == nil {
		return reg
	}
	tenantID := RequestTenantID(r)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	if accessCtrl != nil {
		if canViewExternalProvidersForRequest(r.Context(), accessCtrl, store.NormalizeTenantID(tenantID)) {
			return reg
		}
	}
	filtered := *reg
	filtered.CurrentProviderID = ""
	filtered.Providers = nil
	return &filtered
}

func TestLLMProviderHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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

func GenerateLLMProviderTestKeyHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeError(w, http.StatusInternalServerError, "IDENTITY_SERVICE_UNAVAILABLE", "identity service unavailable")
			return
		}

		var req llmProviderTestKeyRequest
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			admin := AdminFromContext(r.Context())
			if admin != nil {
				email = strings.TrimSpace(admin.Email)
			}
		}
		if email == "" {
			writeError(w, http.StatusBadRequest, "LLM_PROVIDER_TEST_KEY_EMAIL_REQUIRED", "email is required")
			return
		}

		ctx := auth.WithTenant(r.Context(), RequestTenantID(r))
		user, err := identity.AdminConfirmLoginByEmail(ctx, email)
		if err != nil {
			if errors.Is(err, auth.ErrEmailDomainNotAllowed) {
				writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_TEST_KEY_USER_FAILED", err.Error())
			return
		}
		token, err := identity.IssueViewerTokenForUser(ctx, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_TEST_KEY_ISSUE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"email":           user.Email,
			"access_token":    token,
			"token_type":      "Bearer",
			"auth_header":     "Bearer " + token,
			"expires_in_days": 30,
		})
	}
}
func GetLLMServicesAdminHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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

func UpdateLLMServicesAdminHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		audit := firstAdminAuditRepo(audits...)
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(body, &raw)
		_, cardsProvided := raw["cards"]
		_, grantsProvided := raw["grants"]

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
		if !grantsProvided && oldReg != nil {
			req.Grants = append([]llmservice.Grant(nil), oldReg.Grants...)
		}
		req.Normalize()
		// Cascade-clean orphaned references: when a service group definition is removed,
		// automatically purge all bindings/grants/cards that still reference it.
		// This prevents "ghost references" that silently fail at runtime.
		req.PurgeOrphanedServiceGroupReferences()
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
		knownSecurityGroups, err := collectSecurityGroupIDs(security.WithTenant(r.Context(), RequestTenantID(r)), securitySvc)
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
		writeLLMServiceBindingAudit(r.Context(), audit, adminAuditUserID(r), oldReg, &req)
		invalidateLLMRuntimeCaches(system)
		writeJSON(w, http.StatusOK, toLLMServiceAdminResponse(r, &req, nil))
	}
}

func writeLLMServiceBindingAudit(ctx context.Context, audit store.AdminAuditRepository, adminUserID string, oldReg, nextReg *llmservice.Registry) {
	oldSnapshot := buildLLMServiceBindingAuditSnapshot(oldReg)
	nextSnapshot := buildLLMServiceBindingAuditSnapshot(nextReg)
	if reflect.DeepEqual(oldSnapshot, nextSnapshot) {
		return
	}
	writeAdminAuditLog(ctx, audit, adminUserID, "llm.service_bindings.update", map[string]any{"old": oldSnapshot, "new": nextSnapshot})
}

type llmServiceBindingAuditSnapshot struct {
	GlobalServiceGroupIDs       []string                      `json:"global_service_group_ids"`
	SystemDefaultServiceGroupID string                        `json:"system_default_service_group_id,omitempty"`
	DefaultNewUserServiceGroups []string                      `json:"default_new_user_service_groups"`
	GroupBindings               []llmServiceBindingAuditGroup `json:"group_bindings"`
	UserBindings                []llmServiceBindingAuditUser  `json:"user_bindings"`
}

type llmServiceBindingAuditGroup struct {
	GroupID         string   `json:"group_id"`
	ServiceGroupIDs []string `json:"service_group_ids"`
}

type llmServiceBindingAuditUser struct {
	Email           string   `json:"email"`
	ServiceGroupIDs []string `json:"service_group_ids"`
}

func cloneLLMServiceRegistryForAudit(reg *llmservice.Registry) llmservice.Registry {
	if reg == nil {
		return llmservice.Registry{}
	}
	data, err := json.Marshal(reg)
	if err != nil {
		return *reg
	}
	var clone llmservice.Registry
	if err := json.Unmarshal(data, &clone); err != nil {
		return *reg
	}
	return clone
}

func buildLLMServiceBindingAuditSnapshot(reg *llmservice.Registry) llmServiceBindingAuditSnapshot {
	clone := cloneLLMServiceRegistryForAudit(reg)
	clone.Normalize()
	snapshot := llmServiceBindingAuditSnapshot{
		GlobalServiceGroupIDs:       append([]string(nil), clone.GlobalServiceGroupIDs...),
		SystemDefaultServiceGroupID: clone.SystemDefaultServiceGroupID,
		DefaultNewUserServiceGroups: append([]string(nil), clone.DefaultNewUserServiceGroups...),
		GroupBindings:               make([]llmServiceBindingAuditGroup, 0, len(clone.GroupBindings)),
		UserBindings:                make([]llmServiceBindingAuditUser, 0, len(clone.UserBindings)),
	}
	for _, binding := range clone.GroupBindings {
		if strings.TrimSpace(binding.GroupID) == "" || len(binding.ServiceGroupIDs) == 0 {
			continue
		}
		snapshot.GroupBindings = append(snapshot.GroupBindings, llmServiceBindingAuditGroup{GroupID: binding.GroupID, ServiceGroupIDs: append([]string(nil), binding.ServiceGroupIDs...)})
	}
	for _, binding := range clone.UserBindings {
		if strings.TrimSpace(binding.Email) == "" || len(binding.ServiceGroupIDs) == 0 {
			continue
		}
		snapshot.UserBindings = append(snapshot.UserBindings, llmServiceBindingAuditUser{Email: binding.Email, ServiceGroupIDs: append([]string(nil), binding.ServiceGroupIDs...)})
	}
	sort.Slice(snapshot.GroupBindings, func(i, j int) bool { return snapshot.GroupBindings[i].GroupID < snapshot.GroupBindings[j].GroupID })
	sort.Slice(snapshot.UserBindings, func(i, j int) bool { return snapshot.UserBindings[i].Email < snapshot.UserBindings[j].Email })
	return snapshot
}

func CreateLLMServiceCardHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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
		if !isAllowedLLMServiceCardDuration(days) {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_CARD_DURATION_INVALID", "duration_days must be one of: "+allowedLLMServiceCardDurationsLabel())
			return
		}
		credits := req.Credits
		if credits <= 0 {
			credits = defaultLLMServiceCardCredits(days)
		}
		periodLimits := llmservice.CreditPeriodLimits{
			FiveHour: req.FiveHourCredits,
			Daily:    req.DailyCredits,
			Weekly:   req.WeeklyCredits,
			Monthly:  req.MonthlyCredits,
		}
		periodLimits = sanitizeLLMServiceCardPeriodLimits(days, periodLimits)
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
			enc, err := llmservice.EncryptCardCode(code)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", "encrypt card code: "+err.Error())
				return
			}
			cards = append(cards, llmservice.RechargeCard{
				ID:              llmservice.NewID("card"),
				CodeHash:        hash,
				EncryptedCode:   enc,
				Label:           strings.TrimSpace(req.Label),
				ServiceGroupIDs: serviceGroupIDs,
				DurationDays:    days,
				Credits:         credits,
				PeriodLimits:    periodLimits,
				CreatedAt:       time.Now().UTC(),
			})
			issuedCodes = append(issuedCodes, code)
		}
		reg.Cards = append(reg.Cards, cards...)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_CREATE_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(system)
		writeLLMServiceCardAdminAudit(r.Context(), audit, RequestTenantID(r), "llm.service_card.create", map[string]any{
			"label":             strings.TrimSpace(req.Label),
			"service_group_ids": append([]string(nil), serviceGroupIDs...),
			"duration_days":     days,
			"credits":           credits,
			"period_limits":     periodLimits,
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
				"period_limits":     card.PeriodLimits,
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
		system := scopedSystemSettingsForRequest(r, system)
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
			item := map[string]any{
				"id":                card.ID,
				"label":             card.Label,
				"service_group_ids": card.ServiceGroupIDs,
				"duration_days":     card.DurationDays,
				"credits":           card.Credits,
				"period_limits":     card.PeriodLimits,
				"created_at":        card.CreatedAt,
				"redeemed_by_email": card.RedeemedByEmail,
				"redeemed_at":       card.RedeemedAt,
			}
			// Return decrypted code for unused cards so admin can view/copy.
			// Redeemed cards: code is no longer useful, still return for audit.
			if code := card.PlainCode(); code != "" {
				item["code"] = code
			}
			items = append(items, item)
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
		system := scopedSystemSettingsForRequest(r, system)
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
		system := scopedSystemSettingsForRequest(r, system)
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
		system := scopedSystemSettingsForRequest(r, system)
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
		reg.Cards = append(reg.Cards[:idx], reg.Cards[idx+1:]...)
		removedGrantIDs := removeLLMServiceGrantsByCardIDs(reg, []string{id})
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_DELETE_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(system)
		writeLLMServiceCardAdminAudit(r.Context(), audit, RequestTenantID(r), "llm.service_card.delete", map[string]any{"card_id": id, "deleted_grant_ids": removedGrantIDs})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "deleted_grant_ids": removedGrantIDs})
	}
}

func DeleteLLMServiceCardsBatchHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
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
		deletedGrantIDs := removeLLMServiceGrantsByCardIDs(reg, deleted)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_CARD_DELETE_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(system)
		writeLLMServiceCardAdminAudit(r.Context(), audit, RequestTenantID(r), "llm.service_card.delete_batch", map[string]any{
			"requested_ids":     append([]string(nil), ids...),
			"deleted_ids":       append([]string(nil), deleted...),
			"skipped_ids":       append([]string(nil), skipped...),
			"deleted_grant_ids": append([]string(nil), deletedGrantIDs...),
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted_ids": deleted, "skipped_ids": skipped})
	}
}

func DeleteLLMServiceGrantHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GRANT_ID_REQUIRED", "id is required")
			return
		}
		reg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		idx := findLLMServiceGrantIndex(reg, id)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "LLM_SERVICE_GRANT_NOT_FOUND", "service grant not found")
			return
		}
		reg.Grants = append(reg.Grants[:idx], reg.Grants[idx+1:]...)
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_GRANT_DELETE_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(system)
		writeLLMServiceCardAdminAudit(r.Context(), audit, RequestTenantID(r), "llm.service_grant.delete", map[string]any{"grant_id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

func findLLMServiceGrantIndex(reg *llmservice.Registry, id string) int {
	if reg == nil {
		return -1
	}
	needle := strings.ToLower(strings.TrimSpace(id))
	if needle == "" {
		return -1
	}
	for i := range reg.Grants {
		if strings.ToLower(strings.TrimSpace(reg.Grants[i].ID)) == needle {
			return i
		}
	}
	return -1
}

func removeLLMServiceGrantsByCardIDs(reg *llmservice.Registry, cardIDs []string) []string {
	if reg == nil || len(cardIDs) == 0 {
		return nil
	}
	removeSet := map[string]struct{}{}
	for _, id := range cardIDs {
		clean := strings.ToLower(strings.TrimSpace(id))
		if clean != "" {
			removeSet[clean] = struct{}{}
		}
	}
	if len(removeSet) == 0 {
		return nil
	}
	filtered := reg.Grants[:0]
	deleted := make([]string, 0)
	for _, grant := range reg.Grants {
		if _, ok := removeSet[strings.ToLower(strings.TrimSpace(grant.CardID))]; ok {
			if cleanID := strings.TrimSpace(grant.ID); cleanID != "" {
				deleted = append(deleted, cleanID)
			}
			continue
		}
		filtered = append(filtered, grant)
	}
	reg.Grants = filtered
	return deleted
}
func GetLLMServiceStatusHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := withLLMPromptCacheTenant(store.WithTenant(security.WithTenant(r.Context(), principal.TenantID), principal.TenantID), principal.TenantID)
		r = r.WithContext(ctx)
		// Use cached registry reads to avoid hitting the DB on every poll.
		// The 3s TTL cache is sufficient since registry changes are rare
		// (admin edits, redeem) while status polls are frequent (every few seconds per client).
		serviceReg, err := loadCachedLLMServiceRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		status, _, err := llmservice.ResolveStatusFromRegistryForUser(ctx, serviceReg, securitySvc, principal.UserID, principal.Email, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		providerReg, err := loadCachedLLMProviderRegistry(ctx, system)
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

func GetLLMServiceAccountHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := security.WithTenant(r.Context(), principal.TenantID)
		serviceReg, err := loadCachedLLMServiceRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		status, _, err := llmservice.ResolveStatusFromRegistryForUser(ctx, serviceReg, securitySvc, principal.UserID, principal.Email, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		providerReg, err := loadCachedLLMProviderRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		status, filtered := filterAuthorizedModelsByProviderRegistry(status, status.AuthorizedModels, providerReg)
		if status != nil {
			status.InactiveReasons = explainFilteredServiceStatusIssues(status, filtered, providerReg)
		}
		usage, err := llmUsageTotalsForUser(ctx, system, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_USAGE_REPORT_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, llmServiceAccountResponse{Email: principal.Email, TenantID: principal.TenantID, Status: status, Usage: usage})
	}
}

func GetLLMServiceEntitlementDiagnosticHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := RequestTenantID(r)
		system = scopedSystemSettingsForRequest(r, system)
		ctx := security.WithTenant(r.Context(), tenantID)
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeError(w, http.StatusBadRequest, "EMAIL_REQUIRED", "email is required")
			return
		}
		diagnostic, err := llmservice.ExplainEntitlementDiagnostic(ctx, system, securitySvc, email, externalLLMBaseURL(r))
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
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := security.WithTenant(r.Context(), principal.TenantID)
		status, err := llmservice.RedeemCardForUserID(ctx, system, securitySvc, principal.UserID, principal.Email, req.Code, externalLLMBaseURL(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_REDEEM_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(system)
		providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
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
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := security.WithTenant(r.Context(), principal.TenantID)
		status, models, _, _, err := resolveAuthorizedModels(ctx, r, system, securitySvc, principal.UserID, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(models))
		for _, m := range models {
			items = append(items, llmV1ModelObject(m))
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items, "service_status": status})
	}
}

func LLMV1ModelHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := security.WithTenant(r.Context(), principal.TenantID)
		_, models, _, _, err := resolveAuthorizedModels(ctx, r, system, securitySvc, principal.UserID, principal.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		requested := strings.TrimSpace(r.PathValue("model"))
		for _, m := range models {
			if strings.EqualFold(strings.TrimSpace(m.Name), requested) {
				writeJSON(w, http.StatusOK, llmV1ModelObject(m))
				return
			}
		}
		writeError(w, http.StatusNotFound, "LLM_MODEL_NOT_FOUND", fmt.Sprintf("model %q is not authorized for this account", requested))
	}
}

func llmV1ModelObject(m llmservice.AuthorizedModel) map[string]any {
	return map[string]any{
		"id":                m.Name,
		"object":            "model",
		"created":           int64(0),
		"owned_by":          "hub",
		"service_mode":      "hub",
		"provider_ids":      m.ProviderIDs,
		"capability_tags":   m.CapabilityTags,
		"priority":          m.Priority,
		"resolution_tier":   m.ResolutionTier,
		"credit_multiplier": m.CreditMultiplier,
	}
}

func LLMV1ChatCompletionsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		requestID := newLLMEndpointRequestID()
		w.Header().Set("X-MaClaw-Request-ID", requestID)
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := withLLMPromptCacheTenant(store.WithTenant(security.WithTenant(r.Context(), principal.TenantID), principal.TenantID), principal.TenantID)
		r = r.WithContext(ctx)
		providerReg, err := loadCachedLLMProviderRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		downstreamSem, acquired := acquireLLMEndpointDownstreamSlot(r.Context(), principal.TenantID, providerReg)
		if !acquired {
			writeError(w, http.StatusServiceUnavailable, "LLM_ENDPOINT_CONCURRENCY_FULL", "llm endpoint is busy, please retry shortly")
			return
		}
		defer downstreamSem.Release()
		if !globalLLMEndpointUserLimiter.allowForRegistry(principal.TenantID+"\x00"+principal.Email, providerReg) {
			writeError(w, http.StatusTooManyRequests, "LLM_ENDPOINT_USER_RATE_LIMITED", "user request rate exceeded, please retry shortly")
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
			return
		}
		var body map[string]any
		clientIP := llmEndpointClientIP(r)
		requestBody := trimLLMEndpointAccessLogBodyBytes(bodyBytes)
		logStatusCode := http.StatusOK
		logErrorCode := ""
		logRequestedModel := ""
		logAuthorizedModel := ""
		logProviderID := ""
		logUsage := corelib.TokenUsageStat{}
		logFailureStage := ""
		logUpstreamStatus := 0
		defer func() {
			metadata := map[string]any{
				"request_id": requestID,
				"elapsed_ms": time.Since(startedAt).Milliseconds(),
			}
			if logFailureStage != "" {
				metadata["failure_stage"] = logFailureStage
			}
			if logUpstreamStatus > 0 {
				metadata["upstream_status"] = logUpstreamStatus
			}
			if provider := providerReg.FindProvider(logProviderID); provider != nil {
				metadata["upstream_api_url"] = strings.TrimSpace(provider.APIURL)
				metadata["upstream_host"] = llmEndpointUpstreamHost(provider.APIURL)
			} else if officialURL := maclawOfficialHubCenterURL(logProviderID); officialURL != "" {
				metadata["upstream_api_url"] = strings.TrimSpace(officialURL)
				metadata["upstream_host"] = llmEndpointUpstreamHost(officialURL)
			}
			enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{
				Email:             principal.Email,
				ClientIP:          clientIP,
				RequestedModel:    logRequestedModel,
				AuthorizedModel:   logAuthorizedModel,
				ProviderID:        logProviderID,
				StatusCode:        logStatusCode,
				ErrorCode:         logErrorCode,
				InputTokens:       logUsage.InputTokens,
				OutputTokens:      logUsage.OutputTokens,
				TotalTokens:       logUsage.TotalTokens,
				CachedInputTokens: logUsage.CachedInputTokens,
				CacheWriteTokens:  logUsage.CacheWriteTokens,
				InputCostRMB:      logUsage.InputCostRMB,
				OutputCostRMB:     logUsage.OutputCostRMB,
				TotalCostRMB:      logUsage.TotalCostRMB,
				RequestBytes:      len(bodyBytes),
				RequestBody:       requestBody,
				CreatedAt:         time.Now().UTC(),
				Metadata:          metadata,
			})
		}()
		writeLoggedError := func(status int, code, message string) {
			logStatusCode = status
			logErrorCode = code
			writeError(w, status, code, message)
		}
		writeLoggedErrorWithDiag := func(status int, code, message, stage string, upstreamStatus int) {
			logStatusCode = status
			logErrorCode = code
			logFailureStage = strings.TrimSpace(stage)
			if upstreamStatus > 0 {
				logUpstreamStatus = upstreamStatus
			}
			fields := llmEndpointDiagnosticFields(requestID, logFailureStage, logProviderID, logUpstreamStatus, status, startedAt, providerReg)
			writeErrorWithFields(w, status, code, message, fields)
		}
		writeLoggedBillingDenied := func(status int, denial llmBillingDenial) {
			logStatusCode = status
			logErrorCode = denial.Code
			fields := map[string]any{}
			if denial.RetryAfterSeconds > 0 {
				w.Header().Set("Retry-After", strconv.FormatInt(denial.RetryAfterSeconds, 10))
				fields["retry_after_seconds"] = denial.RetryAfterSeconds
			}
			if denial.RetryAfterAt != "" {
				fields["retry_after_at"] = denial.RetryAfterAt
			}
			writeErrorWithFields(w, status, denial.Code, denial.Message, fields)
		}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			writeLoggedError(http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		logRequestedModel = llmEndpointRequestedModel(body)
		var (
			models     []llmservice.AuthorizedModel
			serviceReg *llmservice.Registry
		)
		_, models, serviceReg, err = resolveAuthorizedModelsWithProviderRegistry(ctx, r, system, securitySvc, principal.UserID, principal.Email, providerReg)
		if err != nil {
			writeLoggedError(http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		billableModels, deniedByModel, firstDenial := filterAuthorizedModelsByBillingEligibility(serviceReg, principal.UserID, principal.Email, body, models)
		authorizedModel, requestedModel, err := resolveAuthorizedModel(body, billableModels)
		selectedModelDebug := explainModelSelection(body, billableModels, authorizedModel)
		logRequestedModel = strings.TrimSpace(requestedModel)
		if authorizedModel != nil {
			logAuthorizedModel = strings.TrimSpace(authorizedModel.Name)
		}
		if err != nil {
			if denial, ok := deniedByModel[strings.ToLower(strings.TrimSpace(requestedModel))]; ok && requestedModel != "" && !strings.EqualFold(requestedModel, "auto") && !strings.EqualFold(requestedModel, "default") {
				writeLoggedBillingDenied(llmBillingDenialHTTPStatus(denial), denial)
				return
			}
			if len(models) > 0 && len(billableModels) == 0 && firstDenial.Code != "" {
				writeLoggedBillingDenied(llmBillingDenialHTTPStatus(firstDenial), firstDenial)
				return
			}
			writeLoggedError(http.StatusForbidden, "LLM_MODEL_FORBIDDEN", err.Error())
			return
		}
		cacheCfg := loadCachedHubLLMPromptCacheConfig(ctx, system)
		applyHubLLMPromptCacheRuntimeConfig(firstPromptCacheSource(promptCacheSources), cacheCfg)
		if llmEndpointStreamRequested(body) {
			statusCode, usedProviderID, chargedServiceGroupIDs, usageStat, wroteStream, err := streamAuthorizedModelRequest(w, r, providerReg, authorizedModel, body, requestedModel, selectedModelDebug)
			logStatusCode = statusCode
			logProviderID = strings.TrimSpace(usedProviderID)
			logUpstreamStatus = statusCode
			logUsage = usageStat
			if err != nil {
				if wroteStream {
					logErrorCode = "LLM_STREAM_INTERRUPTED"
					// Still charge credits for tokens already streamed to the client.
					// Without this, users could disconnect mid-stream to avoid payment
					// while having received partial (but useful) LLM output.
					if usedProviderID != "" {
						tokensPerCredit := 0
						if serviceReg != nil {
							tokensPerCredit = serviceReg.TokensPerCredit
						}
						credits := llmservice.EstimateCreditsWithFloor(
							usageStat.TotalTokens,
							llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID),
							tokensPerCredit,
						)
						userGroupIDs := []string(nil)
						if securitySvc != nil {
							if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, principal.Email); resolveErr == nil {
								userGroupIDs = resolved
							}
						}
						enqueueLLMUsageForUserID(system, usedProviderID, usageStat, principal.UserID, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
					}
					return
				}
				if queueErr, ok := err.(*providerConcurrencyError); ok {
					switch queueErr.Kind {
					case providerConcurrencyQueueFull:
						writeLoggedErrorWithDiag(http.StatusTooManyRequests, "LLM_PROVIDER_QUEUE_FULL", queueErr.Error(), "hub_provider_queue", 0)
					default:
						writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_QUEUE_TIMEOUT", queueErr.Error(), "hub_provider_queue", 0)
					}
					return
				}
				if resilienceErr, ok := err.(*providerResilienceError); ok {
					writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_CIRCUIT_OPEN", resilienceErr.Error(), "hub_provider_circuit", 0)
					return
				}
				if status, code, message, ok := llmEndpointUpstreamAuthOrRateError(statusCode, usedProviderID, err); ok {
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
				if statusCode >= 500 {
					status, code, detail := providerUnavailableError(statusCode, usedProviderID, []byte(strings.TrimSpace(fmt.Sprint(err))))
					writeLoggedErrorWithDiag(status, code, detail, "upstream_provider", statusCode)
					return
				}
				writeLoggedErrorWithDiag(http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error(), "hub_to_upstream", statusCode)
				return
			}
			if statusCode < 400 && usedProviderID != "" {
				tokensPerCredit := 0
				if serviceReg != nil {
					tokensPerCredit = serviceReg.TokensPerCredit
				}
				credits := llmservice.EstimateCreditsWithFloor(
					usageStat.TotalTokens,
					llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID),
					tokensPerCredit,
				)
				userGroupIDs := []string(nil)
				if securitySvc != nil {
					if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, principal.Email); resolveErr == nil {
						userGroupIDs = resolved
					}
				}
				enqueueLLMUsageForUserID(system, usedProviderID, usageStat, principal.UserID, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
			}
			return
		}
		respBody, statusCode, usedProviderID, chargedServiceGroupIDs, usageStat, localCacheHit, err := forwardAuthorizedModelRequestWithCache(r, providerReg, authorizedModel, body, requestedModel, firstPromptCacheSource(promptCacheSources), cacheCfg)
		logStatusCode = statusCode
		logProviderID = strings.TrimSpace(usedProviderID)
		logUpstreamStatus = statusCode
		logUsage = usageStat
		if localCacheHit {
			logUsage.InputCostRMB = 0
			logUsage.OutputCostRMB = 0
			logUsage.TotalCostRMB = 0
		}
		if err != nil {
			if queueErr, ok := err.(*providerConcurrencyError); ok {
				switch queueErr.Kind {
				case providerConcurrencyQueueFull:
					writeLoggedErrorWithDiag(http.StatusTooManyRequests, "LLM_PROVIDER_QUEUE_FULL", queueErr.Error(), "hub_provider_queue", 0)
				default:
					writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_QUEUE_TIMEOUT", queueErr.Error(), "hub_provider_queue", 0)
				}
				return
			}
			if resilienceErr, ok := err.(*providerResilienceError); ok {
				writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_CIRCUIT_OPEN", resilienceErr.Error(), "hub_provider_circuit", 0)
				return
			}
			if statusCode >= 400 {
				if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests {
					status, code, message := providerAuthOrRateError(statusCode, usedProviderID, respBody)
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
				if statusCode >= 500 {
					status, code, message := providerUnavailableError(statusCode, usedProviderID, respBody)
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
			}
			writeLoggedErrorWithDiag(http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error(), "hub_to_upstream", statusCode)
			return
		}
		if statusCode < 400 && usedProviderID != "" && !localCacheHit {
			tokensPerCredit := 0
			if serviceReg != nil {
				tokensPerCredit = serviceReg.TokensPerCredit
			}
			credits := llmservice.EstimateCreditsWithFloor(
				usageStat.TotalTokens,
				llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID),
				tokensPerCredit,
			)
			userGroupIDs := []string(nil)
			if securitySvc != nil {
				if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, principal.Email); resolveErr == nil {
					userGroupIDs = resolved
				}
			}
			enqueueLLMUsageForUserID(system, usedProviderID, usageStat, principal.UserID, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
		}
		if statusCode >= 400 {
			bodySnippet := string(respBody)
			if len(bodySnippet) > 500 {
				bodySnippet = bodySnippet[:500] + "..."
			}
			log.Printf("[LLM-V1] upstream error: status=%d provider=%s model=%s requested=%s body=%s", statusCode, usedProviderID, authorizedModel.Name, requestedModel, bodySnippet)
		}
		// Wrap upstream 401/403/429 so the client can distinguish "Hub auth failed"
		// from "upstream provider API key invalid". Without this, the client
		// sees a raw 401 and tells the user their own credentials are wrong.
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests {
			hubStatus, hubCode, detail := providerAuthOrRateError(statusCode, usedProviderID, respBody)
			writeLoggedErrorWithDiag(hubStatus, hubCode, detail, "upstream_provider", statusCode)
			return
		}
		if statusCode >= 500 {
			hubStatus, hubCode, detail := providerUnavailableError(statusCode, usedProviderID, respBody)
			writeLoggedErrorWithDiag(hubStatus, hubCode, detail, "upstream_provider", statusCode)
			return
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

func LLMV1ResponsesHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		requestID := newLLMEndpointRequestID()
		w.Header().Set("X-MaClaw-Request-ID", requestID)
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := withLLMPromptCacheTenant(store.WithTenant(security.WithTenant(r.Context(), principal.TenantID), principal.TenantID), principal.TenantID)
		r = r.WithContext(ctx)
		providerReg, err := loadCachedLLMProviderRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		downstreamSem, acquired := acquireLLMEndpointDownstreamSlot(r.Context(), principal.TenantID, providerReg)
		if !acquired {
			writeError(w, http.StatusServiceUnavailable, "LLM_ENDPOINT_CONCURRENCY_FULL", "llm endpoint is busy, please retry shortly")
			return
		}
		defer downstreamSem.Release()
		if !globalLLMEndpointUserLimiter.allowForRegistry(principal.TenantID+"\x00"+principal.Email, providerReg) {
			writeError(w, http.StatusTooManyRequests, "LLM_ENDPOINT_USER_RATE_LIMITED", "user request rate exceeded, please retry shortly")
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
			return
		}
		clientIP := llmEndpointClientIP(r)
		requestBody := trimLLMEndpointAccessLogBodyBytes(bodyBytes)
		logStatusCode := http.StatusOK
		logErrorCode := ""
		logRequestedModel := ""
		logAuthorizedModel := ""
		logProviderID := ""
		logUsage := corelib.TokenUsageStat{}
		logFailureStage := ""
		logUpstreamStatus := 0
		defer func() {
			metadata := map[string]any{
				"wire_api":   "responses",
				"request_id": requestID,
				"elapsed_ms": time.Since(startedAt).Milliseconds(),
			}
			if logFailureStage != "" {
				metadata["failure_stage"] = logFailureStage
			}
			if logUpstreamStatus > 0 {
				metadata["upstream_status"] = logUpstreamStatus
			}
			if provider := providerReg.FindProvider(logProviderID); provider != nil {
				metadata["upstream_api_url"] = strings.TrimSpace(provider.APIURL)
				metadata["upstream_host"] = llmEndpointUpstreamHost(provider.APIURL)
			} else if officialURL := maclawOfficialHubCenterURL(logProviderID); officialURL != "" {
				metadata["upstream_api_url"] = strings.TrimSpace(officialURL)
				metadata["upstream_host"] = llmEndpointUpstreamHost(officialURL)
			}
			enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{
				Email:             principal.Email,
				ClientIP:          clientIP,
				RequestedModel:    logRequestedModel,
				AuthorizedModel:   logAuthorizedModel,
				ProviderID:        logProviderID,
				StatusCode:        logStatusCode,
				ErrorCode:         logErrorCode,
				InputTokens:       logUsage.InputTokens,
				OutputTokens:      logUsage.OutputTokens,
				TotalTokens:       logUsage.TotalTokens,
				CachedInputTokens: logUsage.CachedInputTokens,
				CacheWriteTokens:  logUsage.CacheWriteTokens,
				InputCostRMB:      logUsage.InputCostRMB,
				OutputCostRMB:     logUsage.OutputCostRMB,
				TotalCostRMB:      logUsage.TotalCostRMB,
				RequestBytes:      len(bodyBytes),
				RequestBody:       requestBody,
				CreatedAt:         time.Now().UTC(),
				Metadata:          metadata,
			})
		}()
		writeLoggedError := func(status int, code, message string) {
			logStatusCode = status
			logErrorCode = code
			writeError(w, status, code, message)
		}
		writeLoggedErrorWithDiag := func(status int, code, message, stage string, upstreamStatus int) {
			logStatusCode = status
			logErrorCode = code
			logFailureStage = strings.TrimSpace(stage)
			if upstreamStatus > 0 {
				logUpstreamStatus = upstreamStatus
			}
			fields := llmEndpointDiagnosticFields(requestID, logFailureStage, logProviderID, logUpstreamStatus, status, startedAt, providerReg)
			fields["wire_api"] = "responses"
			writeErrorWithFields(w, status, code, message, fields)
		}
		var body map[string]any
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			writeLoggedError(http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		logRequestedModel = llmEndpointRequestedModel(body)
		chatBody, responseModel, err := corelib.OpenAICompatResponsesRequestToChat(body)
		if err != nil {
			writeLoggedError(http.StatusBadRequest, "INVALID_RESPONSES_REQUEST", err.Error())
			return
		}
		var (
			models     []llmservice.AuthorizedModel
			serviceReg *llmservice.Registry
		)
		_, models, serviceReg, err = resolveAuthorizedModelsWithProviderRegistry(ctx, r, system, securitySvc, principal.UserID, principal.Email, providerReg)
		if err != nil {
			writeLoggedError(http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		billableModels, deniedByModel, firstDenial := filterAuthorizedModelsByBillingEligibility(serviceReg, principal.UserID, principal.Email, chatBody, models)
		authorizedModel, requestedModel, err := resolveAuthorizedModel(chatBody, billableModels)
		selectedModelDebug := explainModelSelection(chatBody, billableModels, authorizedModel)
		logRequestedModel = strings.TrimSpace(requestedModel)
		if authorizedModel != nil {
			logAuthorizedModel = strings.TrimSpace(authorizedModel.Name)
		}
		if err != nil {
			if denial, ok := deniedByModel[strings.ToLower(strings.TrimSpace(requestedModel))]; ok && requestedModel != "" && !strings.EqualFold(requestedModel, "auto") && !strings.EqualFold(requestedModel, "default") {
				logStatusCode = llmBillingDenialHTTPStatus(denial)
				logErrorCode = denial.Code
				writeLLMBillingDenied(w, denial)
				return
			}
			if len(models) > 0 && len(billableModels) == 0 && firstDenial.Code != "" {
				logStatusCode = llmBillingDenialHTTPStatus(firstDenial)
				logErrorCode = firstDenial.Code
				writeLLMBillingDenied(w, firstDenial)
				return
			}
			writeLoggedError(http.StatusForbidden, "LLM_MODEL_FORBIDDEN", err.Error())
			return
		}
		if llmEndpointStreamRequested(body) {
			statusCode, usedProviderID, chargedServiceGroupIDs, usageStat, wroteStream, err := streamAuthorizedResponsesRequest(w, r, providerReg, authorizedModel, body, chatBody, requestedModel, responseModel, selectedModelDebug)
			logStatusCode = statusCode
			logProviderID = strings.TrimSpace(usedProviderID)
			logUpstreamStatus = statusCode
			logUsage = usageStat
			chargeStreamUsage := func() {
				if usedProviderID == "" {
					return
				}
				tokensPerCredit := 0
				if serviceReg != nil {
					tokensPerCredit = serviceReg.TokensPerCredit
				}
				credits := llmservice.EstimateCreditsWithFloor(
					usageStat.TotalTokens,
					llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID),
					tokensPerCredit,
				)
				userGroupIDs := []string(nil)
				if securitySvc != nil {
					if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, principal.Email); resolveErr == nil {
						userGroupIDs = resolved
					}
				}
				enqueueLLMUsageForUserID(system, usedProviderID, usageStat, principal.UserID, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
			}
			if err != nil {
				if wroteStream {
					logErrorCode = "LLM_STREAM_INTERRUPTED"
					chargeStreamUsage()
					return
				}
				if queueErr, ok := err.(*providerConcurrencyError); ok {
					switch queueErr.Kind {
					case providerConcurrencyQueueFull:
						writeLoggedErrorWithDiag(http.StatusTooManyRequests, "LLM_PROVIDER_QUEUE_FULL", queueErr.Error(), "hub_provider_queue", 0)
					default:
						writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_QUEUE_TIMEOUT", queueErr.Error(), "hub_provider_queue", 0)
					}
					return
				}
				if resilienceErr, ok := err.(*providerResilienceError); ok {
					writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_CIRCUIT_OPEN", resilienceErr.Error(), "hub_provider_circuit", 0)
					return
				}
				if status, code, message, ok := llmEndpointUpstreamAuthOrRateError(statusCode, usedProviderID, err); ok {
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
				if statusCode >= 500 {
					status, code, detail := providerUnavailableError(statusCode, usedProviderID, []byte(strings.TrimSpace(fmt.Sprint(err))))
					writeLoggedErrorWithDiag(status, code, detail, "upstream_provider", statusCode)
					return
				}
				writeLoggedErrorWithDiag(http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error(), "hub_to_upstream", statusCode)
				return
			}
			if statusCode < 400 && usedProviderID != "" {
				chargeStreamUsage()
			}
			return
		}
		cacheCfg := loadCachedHubLLMPromptCacheConfig(ctx, system)
		applyHubLLMPromptCacheRuntimeConfig(firstPromptCacheSource(promptCacheSources), cacheCfg)
		respBody, statusCode, usedProviderID, chargedServiceGroupIDs, usageStat, localCacheHit, rawResponses, err := forwardAuthorizedResponsesRequestWithCache(r, providerReg, authorizedModel, body, chatBody, requestedModel, firstPromptCacheSource(promptCacheSources), cacheCfg)
		logStatusCode = statusCode
		logProviderID = strings.TrimSpace(usedProviderID)
		logUpstreamStatus = statusCode
		logUsage = usageStat
		if localCacheHit {
			logUsage.InputCostRMB = 0
			logUsage.OutputCostRMB = 0
			logUsage.TotalCostRMB = 0
		}
		if err != nil {
			if queueErr, ok := err.(*providerConcurrencyError); ok {
				switch queueErr.Kind {
				case providerConcurrencyQueueFull:
					writeLoggedErrorWithDiag(http.StatusTooManyRequests, "LLM_PROVIDER_QUEUE_FULL", queueErr.Error(), "hub_provider_queue", 0)
				default:
					writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_QUEUE_TIMEOUT", queueErr.Error(), "hub_provider_queue", 0)
				}
				return
			}
			if resilienceErr, ok := err.(*providerResilienceError); ok {
				writeLoggedErrorWithDiag(http.StatusServiceUnavailable, "LLM_PROVIDER_CIRCUIT_OPEN", resilienceErr.Error(), "hub_provider_circuit", 0)
				return
			}
			if statusCode >= 400 {
				if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests {
					status, code, message := providerAuthOrRateError(statusCode, usedProviderID, respBody)
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
				if statusCode >= 500 {
					status, code, message := providerUnavailableError(statusCode, usedProviderID, respBody)
					writeLoggedErrorWithDiag(status, code, message, "upstream_provider", statusCode)
					return
				}
			}
			writeLoggedErrorWithDiag(http.StatusBadGateway, "LLM_UPSTREAM_FAILED", err.Error(), "hub_to_upstream", statusCode)
			return
		}
		if statusCode < 400 && usedProviderID != "" && !localCacheHit {
			tokensPerCredit := 0
			if serviceReg != nil {
				tokensPerCredit = serviceReg.TokensPerCredit
			}
			credits := llmservice.EstimateCreditsWithFloor(
				usageStat.TotalTokens,
				llmservice.CreditMultiplierForProvider(authorizedModel, usedProviderID),
				tokensPerCredit,
			)
			userGroupIDs := []string(nil)
			if securitySvc != nil {
				if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, principal.Email); resolveErr == nil {
					userGroupIDs = resolved
				}
			}
			enqueueLLMUsageForUserID(system, usedProviderID, usageStat, principal.UserID, principal.Email, chargedServiceGroupIDs, userGroupIDs, credits)
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests {
			hubStatus, hubCode, detail := providerAuthOrRateError(statusCode, usedProviderID, respBody)
			writeLoggedErrorWithDiag(hubStatus, hubCode, detail, "upstream_provider", statusCode)
			return
		}
		if statusCode >= 500 {
			hubStatus, hubCode, detail := providerUnavailableError(statusCode, usedProviderID, respBody)
			writeLoggedErrorWithDiag(hubStatus, hubCode, detail, "upstream_provider", statusCode)
			return
		}
		if statusCode < 400 && !rawResponses {
			respBody, err = corelib.OpenAICompatChatResponseToResponses(respBody, responseModel)
			if err != nil {
				writeLoggedError(http.StatusBadGateway, "LLM_RESPONSES_CONVERT_FAILED", err.Error())
				return
			}
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

func writeLLMBillingDenied(w http.ResponseWriter, denial llmBillingDenial) {
	fields := map[string]any{}
	if denial.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(denial.RetryAfterSeconds, 10))
		fields["retry_after_seconds"] = denial.RetryAfterSeconds
	}
	if denial.RetryAfterAt != "" {
		fields["retry_after_at"] = denial.RetryAfterAt
	}
	writeErrorWithFields(w, llmBillingDenialHTTPStatus(denial), denial.Code, denial.Message, fields)
}

func forwardAuthorizedModelRequest(r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string) ([]byte, int, string, []string, error) {
	respBody, statusCode, providerID, serviceGroupIDs, _, _, err := forwardAuthorizedModelRequestWithCache(r, reg, model, body, externalModel, nil, defaultHubLLMPromptCacheConfig())
	return respBody, statusCode, providerID, serviceGroupIDs, err
}

func forwardAuthorizedResponsesRequestWithCache(r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, responsesBody map[string]any, chatBody map[string]any, externalModel string, promptCacheSource any, cacheCfg HubLLMPromptCacheConfig) ([]byte, int, string, []string, corelib.TokenUsageStat, bool, bool, error) {
	if model == nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, false, fmt.Errorf("authorized model is required")
	}
	if reg == nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, false, fmt.Errorf("provider registry is required")
	}
	var lastErr error
	var lastBody []byte
	var lastStatus int
	var lastProviderID string
	request := r.Clone(r.Context())
	for _, providerID := range llmservice.OrderProvidersForRequest(chatBody, model) {
		if IsMaClawProviderRequest(providerID) {
			singleProviderModel := *model
			singleProviderModel.ProviderIDs = []string{providerID}
			respBody, statusCode, usedProviderID, serviceGroupIDs, usageStat, localCacheHit, err := forwardAuthorizedModelRequestWithCache(r, reg, &singleProviderModel, chatBody, externalModel, promptCacheSource, cacheCfg)
			if err != nil {
				lastErr = err
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = firstNonEmptyString(usedProviderID, providerID)
				continue
			}
			if shouldTryNextProviderStatusForProvider(usedProviderID, statusCode) {
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = usedProviderID
				lastErr = fmt.Errorf("provider %q returned http %d", usedProviderID, statusCode)
				continue
			}
			return respBody, statusCode, usedProviderID, serviceGroupIDs, usageStat, localCacheHit, false, nil
		}
		provider := reg.FindProvider(providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %q not configured", providerID)
			continue
		}
		if normalizeProviderProtocol(provider.Protocol) != "openai" || normalizeProviderWireAPI(provider.WireAPI) != "responses" {
			singleProviderModel := *model
			singleProviderModel.ProviderIDs = []string{providerID}
			respBody, statusCode, usedProviderID, serviceGroupIDs, usageStat, localCacheHit, err := forwardAuthorizedModelRequestWithCache(r, reg, &singleProviderModel, chatBody, externalModel, promptCacheSource, cacheCfg)
			if err != nil {
				lastErr = err
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = firstNonEmptyString(usedProviderID, providerID)
				continue
			}
			if shouldTryNextProviderStatusForProvider(usedProviderID, statusCode) {
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = usedProviderID
				lastErr = fmt.Errorf("provider %q returned http %d", usedProviderID, statusCode)
				continue
			}
			return respBody, statusCode, usedProviderID, serviceGroupIDs, usageStat, localCacheHit, false, nil
		}
		if gateErr := globalProviderResilience.beforeAttempt(provider); gateErr != nil {
			lastErr = gateErr
			lastProviderID = provider.ID
			continue
		}
		respBody, statusCode, err := forwardRawResponsesRequest(request, provider, responsesBody)
		if shouldCountProviderFailure(statusCode, err) {
			globalProviderResilience.recordFailure(provider)
		} else {
			globalProviderResilience.recordSuccess(provider.ID)
		}
		if err != nil {
			lastErr = err
			lastBody = respBody
			lastStatus = statusCode
			lastProviderID = provider.ID
			continue
		}
		if statusCode >= 500 || statusCode == http.StatusNotFound || statusCode == http.StatusUnprocessableEntity ||
			statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden ||
			statusCode == http.StatusTooManyRequests {
			lastBody = respBody
			lastStatus = statusCode
			lastProviderID = provider.ID
			lastErr = fmt.Errorf("provider %q returned http %d", provider.ID, statusCode)
			log.Printf("[LLM-V1] provider %q returned %d for responses, trying next provider", provider.ID, statusCode)
			continue
		}
		usageStat := applyProviderUsageCost(parseUsageStats(respBody), provider)
		serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, provider.ID)
		return respBody, statusCode, provider.ID, serviceGroupIDs, usageStat, false, true, nil
	}
	if lastBody != nil && lastStatus > 0 {
		return lastBody, lastStatus, lastProviderID, nil, corelib.TokenUsageStat{}, false, true, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return lastBody, lastStatus, lastProviderID, nil, corelib.TokenUsageStat{}, false, false, lastErr
}

func streamAuthorizedResponsesRequest(w http.ResponseWriter, r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, responsesBody map[string]any, chatBody map[string]any, externalModel string, responseModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (int, string, []string, corelib.TokenUsageStat, bool, error) {
	if model == nil {
		return 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("authorized model is required")
	}
	if reg == nil {
		return 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("provider registry is required")
	}
	request := r.Clone(r.Context())
	var lastErr error
	var lastProviderID string
	var lastStatus int
	for _, providerID := range llmservice.OrderProvidersForRequest(chatBody, model) {
		if IsMaClawProviderRequest(providerID) {
			serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, providerID)
			resp, err := openMaClawOfficialStreamRequest(request, chatBody, serviceGroupIDs)
			if err != nil {
				lastErr = err
				lastProviderID = providerID
				continue
			}
			statusCode := resp.StatusCode
			lastStatus = statusCode
			lastProviderID = providerID
			if shouldTryNextProviderStatusForProvider(providerID, statusCode) {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("provider %q returned http %d: %s", providerID, statusCode, strings.TrimSpace(string(bodyBytes)))
				log.Printf("[LLM-V1] provider %q returned %d for responses stream, trying next provider", providerID, statusCode)
				continue
			}
			if isTerminalProviderStatus(providerID, statusCode) {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				return statusCode, providerID, nil, corelib.TokenUsageStat{}, false, fmt.Errorf("%s", strings.TrimSpace(string(bodyBytes)))
			}
			provider := maclawOfficialStreamProvider()
			streamModel := strings.TrimSpace(responseModel)
			if streamModel == "" {
				streamModel = externalModel
			}
			usageStat, wroteStream, copyErr := writeOpenAIChatAsResponsesStreamResponse(w, resp, provider, model, streamModel, selectedModelDebug)
			_ = resp.Body.Close()
			if copyErr != nil {
				return statusCode, providerID, serviceGroupIDs, usageStat, wroteStream, copyErr
			}
			return statusCode, providerID, serviceGroupIDs, usageStat, wroteStream, nil
		}
		provider := reg.FindProvider(providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %q not configured", providerID)
			continue
		}
		if gateErr := globalProviderResilience.beforeAttempt(provider); gateErr != nil {
			lastErr = gateErr
			lastProviderID = provider.ID
			continue
		}
		var resp *http.Response
		var release func()
		var err error
		rawResponses := normalizeProviderProtocol(provider.Protocol) == "openai" && normalizeProviderWireAPI(provider.WireAPI) == "responses"
		switch {
		case rawResponses:
			resp, release, err = openLLMRawResponsesStreamRequest(request, provider, responsesBody)
		case normalizeProviderWireAPI(provider.WireAPI) == "chat" && normalizeProviderProtocol(provider.Protocol) == "openai":
			resp, release, err = openLLMStreamRequest(request, provider, chatBody)
		default:
			lastErr = fmt.Errorf("provider %q does not support responses streaming passthrough", provider.ID)
			lastProviderID = provider.ID
			continue
		}
		if err != nil {
			globalProviderResilience.recordFailure(provider)
			lastErr = err
			lastProviderID = provider.ID
			continue
		}
		statusCode := resp.StatusCode
		lastStatus = statusCode
		lastProviderID = provider.ID
		if shouldCountProviderFailure(statusCode, nil) {
			globalProviderResilience.recordFailure(provider)
		} else {
			globalProviderResilience.recordSuccess(provider.ID)
		}
		if statusCode >= 500 || statusCode == http.StatusNotFound || statusCode == http.StatusUnprocessableEntity ||
			statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden ||
			statusCode == http.StatusTooManyRequests {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			release()
			lastErr = fmt.Errorf("provider %q returned http %d: %s", provider.ID, statusCode, strings.TrimSpace(string(bodyBytes)))
			log.Printf("[LLM-V1] provider %q returned %d for responses stream, trying next provider", provider.ID, statusCode)
			continue
		}
		var usageStat corelib.TokenUsageStat
		var wroteStream bool
		var copyErr error
		if rawResponses {
			usageStat, wroteStream, copyErr = writeRawResponsesStreamResponse(w, resp, provider, model, selectedModelDebug)
		} else {
			streamModel := strings.TrimSpace(responseModel)
			if streamModel == "" {
				streamModel = externalModel
			}
			usageStat, wroteStream, copyErr = writeOpenAIChatAsResponsesStreamResponse(w, resp, provider, model, streamModel, selectedModelDebug)
		}
		_ = resp.Body.Close()
		release()
		if copyErr != nil {
			return statusCode, provider.ID, llmservice.ServiceGroupIDsForProvider(model, provider.ID), usageStat, wroteStream, copyErr
		}
		return statusCode, provider.ID, llmservice.ServiceGroupIDsForProvider(model, provider.ID), usageStat, wroteStream, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return lastStatus, lastProviderID, nil, corelib.TokenUsageStat{}, false, lastErr
}

func openLLMRawResponsesStreamRequest(r *http.Request, p *im.LLMProvider, body map[string]any) (*http.Response, func(), error) {
	if p == nil {
		return nil, func() {}, fmt.Errorf("provider is required")
	}
	release, err := globalProviderConcurrency.acquire(r.Context(), p.ID, p.MaxConcurrency, p.MaxQueueWaiters, p.QueueTimeoutMS)
	if err != nil {
		return nil, func() {}, err
	}
	cfg := toCoreLLMEndpointProvider(p).MaclawLLMConfig()
	fwd := cloneLLMEndpointBody(body)
	if model := strings.TrimSpace(cfg.UpstreamModel()); model != "" {
		fwd["model"] = model
	}
	jsonBody, err := json.Marshal(fwd)
	if err != nil {
		release()
		return nil, func() {}, fmt.Errorf("marshal responses request: %w", err)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, hubOpenAIResponsesEndpoint(cfg.URL), bytes.NewReader(jsonBody))
	if err != nil {
		release()
		return nil, func() {}, fmt.Errorf("create responses request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	req.Header.Set("User-Agent", cfg.UserAgent())
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	resp, err := llmProviderUpstreamStreamHTTPClient(cfg).Do(req)
	if err != nil {
		release()
		return nil, func() {}, err
	}
	return resp, release, nil
}

func cloneLLMEndpointBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}
	return out
}

func hubOpenAIResponsesEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if hubOpenAIEndpointHasVersionSuffix(trimmed) {
		return trimmed + "/responses"
	}
	return trimmed + "/v1/responses"
}

func hubOpenAIEndpointHasVersionSuffix(endpoint string) bool {
	lastSlash := strings.LastIndex(endpoint, "/")
	if lastSlash < 0 || lastSlash == len(endpoint)-1 {
		return false
	}
	segment := strings.ToLower(endpoint[lastSlash+1:])
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func llmEndpointStreamRequested(body map[string]any) bool {
	stream, _ := body["stream"].(bool)
	return stream
}

func streamAuthorizedModelRequest(w http.ResponseWriter, r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (int, string, []string, corelib.TokenUsageStat, bool, error) {
	if model == nil {
		return 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("authorized model is required")
	}
	if reg == nil {
		return 0, "", nil, corelib.TokenUsageStat{}, false, fmt.Errorf("provider registry is required")
	}
	request := r.Clone(r.Context())
	var lastErr error
	var lastProviderID string
	var lastStatus int
	for _, providerID := range llmservice.OrderProvidersForRequest(body, model) {
		if IsMaClawProviderRequest(providerID) {
			serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, providerID)
			resp, err := openMaClawOfficialStreamRequest(request, body, serviceGroupIDs)
			if err != nil {
				lastErr = err
				lastProviderID = providerID
				continue
			}
			statusCode := resp.StatusCode
			lastStatus = statusCode
			lastProviderID = providerID
			if shouldTryNextProviderStatusForProvider(providerID, statusCode) {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("provider %q returned http %d: %s", providerID, statusCode, strings.TrimSpace(string(bodyBytes)))
				log.Printf("[LLM-V1] provider %q returned %d for stream, trying next provider", providerID, statusCode)
				continue
			}
			if isTerminalProviderStatus(providerID, statusCode) {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				return statusCode, providerID, nil, corelib.TokenUsageStat{}, false, fmt.Errorf("%s", strings.TrimSpace(string(bodyBytes)))
			}
			provider := maclawOfficialStreamProvider()
			usageStat, wroteStream, copyErr := writeOpenAIStreamResponse(w, resp, provider, model, externalModel, selectedModelDebug)
			_ = resp.Body.Close()
			if copyErr != nil {
				return statusCode, providerID, llmservice.ServiceGroupIDsForProvider(model, providerID), usageStat, wroteStream, copyErr
			}
			return statusCode, providerID, serviceGroupIDs, usageStat, wroteStream, nil
		}
		provider := reg.FindProvider(providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %q not configured", providerID)
			continue
		}
		if normalizeProviderWireAPI(provider.WireAPI) != "chat" || normalizeProviderProtocol(provider.Protocol) != "openai" {
			lastErr = fmt.Errorf("provider %q does not support streaming passthrough", provider.ID)
			lastProviderID = provider.ID
			continue
		}
		if gateErr := globalProviderResilience.beforeAttempt(provider); gateErr != nil {
			lastErr = gateErr
			lastProviderID = provider.ID
			continue
		}
		resp, release, err := openLLMStreamRequest(request, provider, body)
		if err != nil {
			globalProviderResilience.recordFailure(provider)
			lastErr = err
			lastProviderID = provider.ID
			continue
		}
		statusCode := resp.StatusCode
		lastStatus = statusCode
		lastProviderID = provider.ID
		if shouldCountProviderFailure(statusCode, nil) {
			globalProviderResilience.recordFailure(provider)
		} else {
			globalProviderResilience.recordSuccess(provider.ID)
		}
		if statusCode >= 500 || statusCode == http.StatusNotFound || statusCode == http.StatusUnprocessableEntity ||
			statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden ||
			statusCode == http.StatusTooManyRequests {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			release()
			lastErr = fmt.Errorf("provider %q returned http %d: %s", provider.ID, statusCode, strings.TrimSpace(string(bodyBytes)))
			log.Printf("[LLM-V1] provider %q returned %d for stream, trying next provider", provider.ID, statusCode)
			continue
		}
		usageStat, wroteStream, copyErr := writeOpenAIStreamResponse(w, resp, provider, model, externalModel, selectedModelDebug)
		_ = resp.Body.Close()
		release()
		if copyErr != nil {
			return statusCode, provider.ID, llmservice.ServiceGroupIDsForProvider(model, provider.ID), usageStat, wroteStream, copyErr
		}
		return statusCode, provider.ID, llmservice.ServiceGroupIDsForProvider(model, provider.ID), usageStat, wroteStream, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return lastStatus, lastProviderID, nil, corelib.TokenUsageStat{}, false, lastErr
}

func openMaClawOfficialStreamRequest(r *http.Request, body map[string]any, serviceGroupIDs []string) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal maclaw official stream request: %w", err)
	}
	resp, err := ForwardStreamViaMaClaw(r.Context(), payload, store.TenantIDFromContext(r.Context()), serviceGroupIDs...)
	if err != nil {
		return nil, err
	}
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	if resp.StatusCode != http.StatusBadRequest {
		return resp, nil
	}
	if retryBody, ok := maclawOfficialSanitizedRetryBody(body); ok {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		retryPayload, marshalErr := json.Marshal(retryBody)
		if marshalErr != nil {
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return resp, nil
		}
		log.Printf("[LLM-V1] maclaw official returned 400 for stream; retrying with sanitized OpenAI-compatible body")
		retryResp, retryErr := ForwardStreamViaMaClaw(r.Context(), retryPayload, store.TenantIDFromContext(r.Context()), serviceGroupIDs...)
		if retryErr != nil {
			return nil, retryErr
		}
		if retryResp.Body == nil {
			retryResp.Body = http.NoBody
		}
		if retryResp.StatusCode != http.StatusBadRequest {
			return retryResp, nil
		}
		resp = retryResp
	}
	if retryBody, ok := maclawOfficialToollessRetryBody(body); ok {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		retryPayload, marshalErr := json.Marshal(retryBody)
		if marshalErr != nil {
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return resp, nil
		}
		log.Printf("[LLM-V1] maclaw official returned 400 for stream; retrying without tool schemas")
		retryResp, retryErr := ForwardStreamViaMaClaw(r.Context(), retryPayload, store.TenantIDFromContext(r.Context()), serviceGroupIDs...)
		if retryErr != nil {
			return nil, retryErr
		}
		if retryResp.Body == nil {
			retryResp.Body = http.NoBody
		}
		if retryResp.StatusCode != http.StatusBadRequest {
			return retryResp, nil
		}
		resp = retryResp
	}
	return resp, nil
}

func maclawOfficialStreamProvider() *im.LLMProvider {
	return &im.LLMProvider{
		ID:       llmservice.MaClawOfficialProviderID,
		Name:     llmservice.MaClawOfficialProviderName,
		Protocol: "openai",
		WireAPI:  "chat",
	}
}

func openLLMStreamRequest(r *http.Request, p *im.LLMProvider, body map[string]any) (*http.Response, func(), error) {
	if p == nil {
		return nil, func() {}, fmt.Errorf("provider is required")
	}
	release, err := globalProviderConcurrency.acquire(r.Context(), p.ID, p.MaxConcurrency, p.MaxQueueWaiters, p.QueueTimeoutMS)
	if err != nil {
		return nil, func() {}, err
	}
	provider := toCoreLLMEndpointProvider(p)
	resp, err := corelib.ForwardOpenAICompatStreamRequest(r.Context(), provider.MaclawLLMConfig(), body, llmProviderUpstreamStreamHTTPClient(provider.MaclawLLMConfig()))
	if err != nil {
		release()
		return nil, func() {}, err
	}
	return resp, release, nil
}

func writeOpenAIStreamResponse(w http.ResponseWriter, resp *http.Response, provider *im.LLMProvider, model *llmservice.AuthorizedModel, externalModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (corelib.TokenUsageStat, bool, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return corelib.TokenUsageStat{}, false, fmt.Errorf("streaming not supported by response writer")
	}
	reader := bufio.NewReaderSize(resp.Body, 4096)
	if !looksLikeOpenAIStream(resp, reader) {
		return writeOpenAINonStreamAsStreamResponse(w, flusher, reader, resp.StatusCode, provider, model, externalModel, selectedModelDebug)
	}
	setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
	w.WriteHeader(resp.StatusCode)
	flusher.Flush()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	usage := corelib.TokenUsageStat{}
	event := make([][]byte, 0, 4)
	flushEvent := func() error {
		if len(event) == 0 {
			return nil
		}
		if !sseEventHasNonEmptyData(event) {
			event = event[:0]
			return nil
		}
		wrote := false
		for _, line := range event {
			if isSSECommentLine(line) {
				continue
			}
			out := rewriteOpenAIStreamLine(line, externalModel, &usage)
			out = append(out, '\n')
			if _, err := w.Write(out); err != nil {
				return err
			}
			wrote = true
		}
		if wrote {
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
			flusher.Flush()
		}
		event = event[:0]
		return nil
	}
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			if err := flushEvent(); err != nil {
				return applyProviderUsageCost(usage, provider), true, err
			}
			continue
		}
		event = append(event, line)
	}
	if err := flushEvent(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	if err := scanner.Err(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	return applyProviderUsageCost(usage, provider), true, nil
}

func writeRawResponsesStreamResponse(w http.ResponseWriter, resp *http.Response, provider *im.LLMProvider, model *llmservice.AuthorizedModel, selectedModelDebug *llmservice.ModelSelectionDebug) (corelib.TokenUsageStat, bool, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return corelib.TokenUsageStat{}, false, fmt.Errorf("streaming not supported by response writer")
	}
	reader := bufio.NewReaderSize(resp.Body, 4096)
	if !looksLikeOpenAIStream(resp, reader) {
		body, err := io.ReadAll(io.LimitReader(reader, 32<<20))
		if err != nil {
			return corelib.TokenUsageStat{}, false, fmt.Errorf("read non-stream responses upstream response: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return corelib.TokenUsageStat{}, false, fmt.Errorf("upstream returned non-SSE, non-JSON responses body: %w", err)
		}
		usage := parseUsageStats(body)
		setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
		w.WriteHeader(resp.StatusCode)
		if err := writeHubResponsesPayloadSSE(w, payload); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		flusher.Flush()
		return applyProviderUsageCost(usage, provider), true, nil
	}
	setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
	w.WriteHeader(resp.StatusCode)
	flusher.Flush()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	usage := corelib.TokenUsageStat{}
	event := make([][]byte, 0, 4)
	flushEvent := func() error {
		if len(event) == 0 {
			return nil
		}
		if !sseEventHasNonEmptyData(event) {
			event = event[:0]
			return nil
		}
		for _, line := range event {
			if isSSECommentLine(line) {
				continue
			}
			trimmed := strings.TrimSpace(string(line))
			if _, err := w.Write(append(append([]byte(nil), line...), '\n')); err != nil {
				return err
			}
			if strings.HasPrefix(trimmed, "data:") {
				if chunkUsage := responsesStreamUsageFromLine(line); chunkUsage.TotalTokens > 0 || chunkUsage.InputTokens > 0 || chunkUsage.OutputTokens > 0 {
					usage = chunkUsage
				}
			}
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
		flusher.Flush()
		event = event[:0]
		return nil
	}
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if err := flushEvent(); err != nil {
				return applyProviderUsageCost(usage, provider), true, err
			}
			continue
		}
		event = append(event, line)
	}
	if err := flushEvent(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	if err := scanner.Err(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	flusher.Flush()
	return applyProviderUsageCost(usage, provider), true, nil
}

func writeOpenAIChatAsResponsesStreamResponse(w http.ResponseWriter, resp *http.Response, provider *im.LLMProvider, model *llmservice.AuthorizedModel, responseModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (corelib.TokenUsageStat, bool, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return corelib.TokenUsageStat{}, false, fmt.Errorf("streaming not supported by response writer")
	}
	reader := bufio.NewReaderSize(resp.Body, 4096)
	if !looksLikeOpenAIStream(resp, reader) {
		return writeOpenAIChatNonStreamAsResponsesStreamResponse(w, flusher, reader, resp.StatusCode, provider, model, responseModel, selectedModelDebug)
	}
	setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
	w.WriteHeader(resp.StatusCode)
	respID := fmt.Sprintf("resp_hub_%d", time.Now().UnixNano())
	msgID := "msg_" + respID
	seq := 1
	nextOutputIndex := 0
	textOutputIndex := -1
	var text strings.Builder
	textStarted := false
	toolCalls := map[int]*hubResponsesStreamToolCallAccum{}
	var toolOrder []int
	usage := corelib.TokenUsageStat{}
	if err := writeHubResponsesSSE(w, "response.created", map[string]any{
		"type":            "response.created",
		"sequence_number": seq,
		"response":        hubResponsesStreamResponseObject(respID, responseModel, "", false, -1, nil, nil, usage),
	}); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	seq++
	flusher.Flush()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var eventLines [][]byte
	flushEvent := func() (bool, error) {
		if len(eventLines) == 0 {
			return true, nil
		}
		lines := eventLines
		eventLines = nil
		payload := openAIStreamEventPayload(lines)
		if strings.TrimSpace(payload) == "" {
			return true, nil
		}
		if strings.TrimSpace(payload) == "[DONE]" {
			return false, nil
		}
		if chunkUsage := corelib.OpenAIStreamUsageFromData([]byte(payload)); chunkUsage.TotalTokens > 0 || chunkUsage.InputTokens > 0 || chunkUsage.OutputTokens > 0 {
			usage = chunkUsage
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return false, err
		}
		for _, choiceRaw := range anySlice(chunk["choices"]) {
			choice := mapFromProviderHandlerAny(choiceRaw)
			delta := mapFromProviderHandlerAny(choice["delta"])
			if content, ok := delta["content"].(string); ok && content != "" {
				if !textStarted {
					textOutputIndex = nextOutputIndex
					nextOutputIndex++
					if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
						"type":            "response.output_item.added",
						"sequence_number": seq,
						"output_index":    textOutputIndex,
						"item": map[string]any{
							"id":      msgID,
							"type":    "message",
							"status":  "in_progress",
							"role":    "assistant",
							"content": []any{},
						},
					}); err != nil {
						return false, err
					}
					seq++
					if err := writeHubResponsesSSE(w, "response.content_part.added", map[string]any{
						"type":            "response.content_part.added",
						"sequence_number": seq,
						"item_id":         msgID,
						"output_index":    textOutputIndex,
						"content_index":   0,
						"part":            map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
					}); err != nil {
						return false, err
					}
					seq++
					textStarted = true
				}
				text.WriteString(content)
				if err := writeHubResponsesSSE(w, "response.output_text.delta", map[string]any{
					"type":            "response.output_text.delta",
					"sequence_number": seq,
					"item_id":         msgID,
					"output_index":    textOutputIndex,
					"content_index":   0,
					"delta":           content,
					"logprobs":        []any{},
				}); err != nil {
					return false, err
				}
				seq++
				flusher.Flush()
			}
			for _, rawTool := range anySlice(delta["tool_calls"]) {
				tool := mapFromProviderHandlerAny(rawTool)
				idx := numberToInt(firstNonNil(tool["index"], 0))
				acc := toolCalls[idx]
				if acc == nil {
					acc = &hubResponsesStreamToolCallAccum{Index: idx, OutputIndex: -1}
					toolCalls[idx] = acc
					toolOrder = append(toolOrder, idx)
				}
				if id := strings.TrimSpace(fmt.Sprint(tool["id"])); id != "" && id != "<nil>" {
					acc.ID = id
				}
				fn := mapFromProviderHandlerAny(tool["function"])
				if name := strings.TrimSpace(fmt.Sprint(fn["name"])); name != "" && name != "<nil>" {
					acc.Name = name
				}
				if args, ok := fn["arguments"].(string); ok && args != "" {
					acc.Arguments += args
					acc.PendingArguments += args
				}
				if acc.Name != "" && acc.ID != "" && !acc.Added {
					if acc.OutputIndex < 0 {
						acc.OutputIndex = nextOutputIndex
						nextOutputIndex++
					}
					acc.ItemID = "fc_" + acc.ID
					if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
						"type":            "response.output_item.added",
						"sequence_number": seq,
						"output_index":    acc.OutputIndex,
						"item": map[string]any{
							"id":        acc.ItemID,
							"type":      "function_call",
							"status":    "in_progress",
							"call_id":   acc.ID,
							"name":      acc.Name,
							"arguments": "",
						},
					}); err != nil {
						return false, err
					}
					seq++
					acc.Added = true
				}
				if acc.Added && acc.PendingArguments != "" {
					if err := writeHubResponsesSSE(w, "response.function_call_arguments.delta", map[string]any{
						"type":            "response.function_call_arguments.delta",
						"sequence_number": seq,
						"item_id":         acc.ItemID,
						"output_index":    acc.OutputIndex,
						"delta":           acc.PendingArguments,
					}); err != nil {
						return false, err
					}
					seq++
					acc.PendingArguments = ""
					flusher.Flush()
				}
			}
			if legacy := mapFromProviderHandlerAny(delta["function_call"]); legacy != nil {
				const idx = 0
				acc := toolCalls[idx]
				if acc == nil {
					acc = &hubResponsesStreamToolCallAccum{Index: idx, OutputIndex: -1, ID: "call_legacy_function"}
					toolCalls[idx] = acc
					toolOrder = append(toolOrder, idx)
				}
				if name := strings.TrimSpace(fmt.Sprint(legacy["name"])); name != "" && name != "<nil>" {
					acc.Name = name
				}
				if args, ok := legacy["arguments"].(string); ok && args != "" {
					acc.Arguments += args
					acc.PendingArguments += args
				}
				if acc.Name != "" && !acc.Added {
					if acc.OutputIndex < 0 {
						acc.OutputIndex = nextOutputIndex
						nextOutputIndex++
					}
					acc.ItemID = "fc_" + acc.ID
					if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
						"type":            "response.output_item.added",
						"sequence_number": seq,
						"output_index":    acc.OutputIndex,
						"item": map[string]any{
							"id":        acc.ItemID,
							"type":      "function_call",
							"status":    "in_progress",
							"call_id":   acc.ID,
							"name":      acc.Name,
							"arguments": "",
						},
					}); err != nil {
						return false, err
					}
					seq++
					acc.Added = true
				}
				if acc.Added && acc.PendingArguments != "" {
					if err := writeHubResponsesSSE(w, "response.function_call_arguments.delta", map[string]any{
						"type":            "response.function_call_arguments.delta",
						"sequence_number": seq,
						"item_id":         acc.ItemID,
						"output_index":    acc.OutputIndex,
						"delta":           acc.PendingArguments,
					}); err != nil {
						return false, err
					}
					seq++
					acc.PendingArguments = ""
					flusher.Flush()
				}
			}
		}
		return true, nil
	}
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			keepGoing, err := flushEvent()
			if err != nil {
				return applyProviderUsageCost(usage, provider), true, err
			}
			if !keepGoing {
				break
			}
			continue
		}
		eventLines = append(eventLines, line)
	}
	if _, err := flushEvent(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	if err := scanner.Err(); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	outputText := text.String()
	if textStarted {
		if err := writeHubResponsesSSE(w, "response.output_text.done", map[string]any{
			"type":            "response.output_text.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"text":            outputText,
			"logprobs":        []any{},
		}); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		seq++
		if err := writeHubResponsesSSE(w, "response.content_part.done", map[string]any{
			"type":            "response.content_part.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"part":            map[string]any{"type": "output_text", "text": outputText, "annotations": []any{}},
		}); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		seq++
		if err := writeHubResponsesSSE(w, "response.output_item.done", map[string]any{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    textOutputIndex,
			"item": map[string]any{
				"id":      msgID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": outputText, "annotations": []any{}}},
			},
		}); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		seq++
	}
	for _, idx := range toolOrder {
		acc := toolCalls[idx]
		if acc == nil || acc.Name == "" {
			continue
		}
		if acc.ID == "" {
			acc.ID = fmt.Sprintf("call_%d", idx)
		}
		if acc.ItemID == "" {
			acc.ItemID = "fc_" + acc.ID
		}
		if acc.OutputIndex < 0 {
			acc.OutputIndex = nextOutputIndex
			nextOutputIndex++
		}
		if !acc.Added {
			if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
				"type":            "response.output_item.added",
				"sequence_number": seq,
				"output_index":    acc.OutputIndex,
				"item": map[string]any{
					"id":        acc.ItemID,
					"type":      "function_call",
					"status":    "in_progress",
					"call_id":   acc.ID,
					"name":      acc.Name,
					"arguments": "",
				},
			}); err != nil {
				return applyProviderUsageCost(usage, provider), true, err
			}
			seq++
		}
		if acc.PendingArguments != "" {
			if err := writeHubResponsesSSE(w, "response.function_call_arguments.delta", map[string]any{
				"type":            "response.function_call_arguments.delta",
				"sequence_number": seq,
				"item_id":         acc.ItemID,
				"output_index":    acc.OutputIndex,
				"delta":           acc.PendingArguments,
			}); err != nil {
				return applyProviderUsageCost(usage, provider), true, err
			}
			seq++
			acc.PendingArguments = ""
		}
		if err := writeHubResponsesSSE(w, "response.function_call_arguments.done", map[string]any{
			"type":            "response.function_call_arguments.done",
			"sequence_number": seq,
			"item_id":         acc.ItemID,
			"output_index":    acc.OutputIndex,
			"arguments":       acc.Arguments,
		}); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		seq++
		if err := writeHubResponsesSSE(w, "response.output_item.done", map[string]any{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    acc.OutputIndex,
			"item": map[string]any{
				"id":        acc.ItemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": acc.Arguments,
			},
		}); err != nil {
			return applyProviderUsageCost(usage, provider), true, err
		}
		seq++
	}
	if err := writeHubResponsesSSE(w, "response.completed", map[string]any{
		"type":            "response.completed",
		"sequence_number": seq,
		"response":        hubResponsesStreamResponseObject(respID, responseModel, outputText, true, textOutputIndex, toolOrder, toolCalls, usage),
	}); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	flusher.Flush()
	return applyProviderUsageCost(usage, provider), true, nil
}

func writeOpenAIChatNonStreamAsResponsesStreamResponse(w http.ResponseWriter, flusher http.Flusher, reader io.Reader, statusCode int, provider *im.LLMProvider, model *llmservice.AuthorizedModel, responseModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (corelib.TokenUsageStat, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 32<<20))
	if err != nil {
		return corelib.TokenUsageStat{}, false, fmt.Errorf("read non-stream upstream response: %w", err)
	}
	respBody, err := corelib.OpenAICompatChatResponseToResponses(body, responseModel)
	if err != nil {
		return corelib.TokenUsageStat{}, false, err
	}
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return corelib.TokenUsageStat{}, false, err
	}
	usage := parseUsageStats(respBody)
	setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
	w.WriteHeader(statusCode)
	if err := writeHubResponsesPayloadSSE(w, payload); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	flusher.Flush()
	return applyProviderUsageCost(usage, provider), true, nil
}

func writeHubResponsesPayloadSSE(w io.Writer, payload map[string]any) error {
	seq := 1
	if err := writeHubResponsesSSE(w, "response.created", map[string]any{
		"type":            "response.created",
		"sequence_number": seq,
		"response":        payload,
	}); err != nil {
		return err
	}
	seq++
	for outputIndex, rawItem := range anySlice(payload["output"]) {
		item := mapFromProviderHandlerAny(rawItem)
		switch strings.TrimSpace(fmt.Sprint(item["type"])) {
		case "message":
			itemID := firstNonEmptyProviderHandler(strings.TrimSpace(fmt.Sprint(item["id"])), fmt.Sprintf("msg_%d", outputIndex))
			if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
				"type":            "response.output_item.added",
				"sequence_number": seq,
				"output_index":    outputIndex,
				"item":            map[string]any{"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
			}); err != nil {
				return err
			}
			seq++
			contentIndex := 0
			for _, rawPart := range anySlice(item["content"]) {
				part := mapFromProviderHandlerAny(rawPart)
				if strings.TrimSpace(fmt.Sprint(part["type"])) != "output_text" {
					continue
				}
				text := fmt.Sprint(part["text"])
				if part["text"] == nil || text == "<nil>" {
					text = ""
				}
				if err := writeHubResponsesSSE(w, "response.content_part.added", map[string]any{
					"type":            "response.content_part.added",
					"sequence_number": seq,
					"item_id":         itemID,
					"output_index":    outputIndex,
					"content_index":   contentIndex,
					"part":            map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
				}); err != nil {
					return err
				}
				seq++
				if text != "" {
					if err := writeHubResponsesSSE(w, "response.output_text.delta", map[string]any{
						"type":            "response.output_text.delta",
						"sequence_number": seq,
						"item_id":         itemID,
						"output_index":    outputIndex,
						"content_index":   contentIndex,
						"delta":           text,
						"logprobs":        []any{},
					}); err != nil {
						return err
					}
					seq++
				}
				if err := writeHubResponsesSSE(w, "response.output_text.done", map[string]any{
					"type":            "response.output_text.done",
					"sequence_number": seq,
					"item_id":         itemID,
					"output_index":    outputIndex,
					"content_index":   contentIndex,
					"text":            text,
					"logprobs":        []any{},
				}); err != nil {
					return err
				}
				seq++
				if err := writeHubResponsesSSE(w, "response.content_part.done", map[string]any{
					"type":            "response.content_part.done",
					"sequence_number": seq,
					"item_id":         itemID,
					"output_index":    outputIndex,
					"content_index":   contentIndex,
					"part":            map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
				}); err != nil {
					return err
				}
				seq++
				contentIndex++
			}
			if err := writeHubResponsesSSE(w, "response.output_item.done", map[string]any{
				"type":            "response.output_item.done",
				"sequence_number": seq,
				"output_index":    outputIndex,
				"item":            item,
			}); err != nil {
				return err
			}
			seq++
		case "function_call":
			itemID := firstNonEmptyProviderHandler(strings.TrimSpace(fmt.Sprint(item["id"])), fmt.Sprintf("fc_%d", outputIndex))
			callID := firstNonEmptyProviderHandler(strings.TrimSpace(fmt.Sprint(item["call_id"])), itemID)
			name := strings.TrimSpace(fmt.Sprint(item["name"]))
			args := strings.TrimSpace(fmt.Sprint(item["arguments"]))
			if args == "" || args == "<nil>" {
				args = "{}"
			}
			if err := writeHubResponsesSSE(w, "response.output_item.added", map[string]any{
				"type":            "response.output_item.added",
				"sequence_number": seq,
				"output_index":    outputIndex,
				"item":            map[string]any{"id": itemID, "type": "function_call", "status": "in_progress", "call_id": callID, "name": name, "arguments": ""},
			}); err != nil {
				return err
			}
			seq++
			if err := writeHubResponsesSSE(w, "response.function_call_arguments.delta", map[string]any{
				"type":            "response.function_call_arguments.delta",
				"sequence_number": seq,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"delta":           args,
			}); err != nil {
				return err
			}
			seq++
			if err := writeHubResponsesSSE(w, "response.function_call_arguments.done", map[string]any{
				"type":            "response.function_call_arguments.done",
				"sequence_number": seq,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"arguments":       args,
			}); err != nil {
				return err
			}
			seq++
			if err := writeHubResponsesSSE(w, "response.output_item.done", map[string]any{
				"type":            "response.output_item.done",
				"sequence_number": seq,
				"output_index":    outputIndex,
				"item":            item,
			}); err != nil {
				return err
			}
			seq++
		}
	}
	return writeHubResponsesSSE(w, "response.completed", map[string]any{
		"type":            "response.completed",
		"sequence_number": seq,
		"response":        payload,
	})
}

type hubResponsesStreamToolCallAccum struct {
	Index            int
	OutputIndex      int
	ID               string
	ItemID           string
	Name             string
	Arguments        string
	PendingArguments string
	Added            bool
}

func hubResponsesStreamResponseObject(id, model, text string, completed bool, textOutputIndex int, toolOrder []int, toolCalls map[int]*hubResponsesStreamToolCallAccum, usage corelib.TokenUsageStat) map[string]any {
	status := "in_progress"
	output := []any{}
	if completed {
		status = "completed"
		type outputItem struct {
			index int
			item  any
		}
		items := []outputItem{}
		hasTools := len(toolOrder) > 0
		if text != "" || !hasTools {
			if textOutputIndex < 0 {
				textOutputIndex = 0
			}
			items = append(items, outputItem{index: textOutputIndex, item: map[string]any{
				"id":      "msg_" + id,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
			}})
		}
		for _, idx := range toolOrder {
			acc := toolCalls[idx]
			if acc == nil || acc.Name == "" {
				continue
			}
			items = append(items, outputItem{index: acc.OutputIndex, item: map[string]any{
				"id":        firstNonEmptyProviderHandler(acc.ItemID, "fc_"+acc.ID),
				"type":      "function_call",
				"status":    "completed",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": acc.Arguments,
			}})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].index < items[j].index })
		for _, item := range items {
			output = append(output, item.item)
		}
	}
	return map[string]any{
		"id":                  id,
		"object":              "response",
		"created_at":          float64(time.Now().Unix()),
		"status":              status,
		"model":               model,
		"output":              output,
		"parallel_tool_calls": false,
		"tools":               []any{},
		"tool_choice":         "auto",
		"metadata":            map[string]any{},
		"instructions":        nil,
		"incomplete_details":  nil,
		"error":               nil,
		"usage": map[string]any{
			"input_tokens":          usage.InputTokens,
			"output_tokens":         usage.OutputTokens,
			"total_tokens":          usage.TotalTokens,
			"input_tokens_details":  map[string]any{"cached_tokens": 0},
			"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		},
	}
}

func writeHubResponsesSSE(w io.Writer, event string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(data))
	return err
}

func openAIStreamEventPayload(lines [][]byte) string {
	parts := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		parts = append(parts, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
	}
	return strings.Join(parts, "\n")
}

func responsesStreamUsageFromLine(line []byte) corelib.TokenUsageStat {
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "data:") {
		return corelib.TokenUsageStat{}
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return corelib.TokenUsageStat{}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		return corelib.TokenUsageStat{}
	}
	if response := mapFromProviderHandlerAny(obj["response"]); response != nil {
		if usage := mapFromProviderHandlerAny(response["usage"]); usage != nil {
			data, _ := json.Marshal(map[string]any{"usage": usage})
			return parseUsageStats(data)
		}
	}
	if usage := mapFromProviderHandlerAny(obj["usage"]); usage != nil {
		data, _ := json.Marshal(map[string]any{"usage": usage})
		return parseUsageStats(data)
	}
	return corelib.TokenUsageStat{}
}

func mapFromProviderHandlerAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func firstNonEmptyProviderHandler(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func looksLikeOpenAIStream(resp *http.Response, reader *bufio.Reader) bool {
	if resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return true
	}
	peeked, err := reader.Peek(64)
	if err != nil && len(peeked) == 0 {
		return false
	}
	trimmed := strings.TrimLeft(string(peeked), " \t\r\n")
	return strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, ":")
}

func isSSECommentLine(line []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(line)), ":")
}

func sseEventHasNonEmptyData(event [][]byte) bool {
	for _, line := range event {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "data:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")) != "" {
			return true
		}
	}
	return false
}

func writeOpenAINonStreamAsStreamResponse(w http.ResponseWriter, flusher http.Flusher, reader io.Reader, statusCode int, provider *im.LLMProvider, model *llmservice.AuthorizedModel, externalModel string, selectedModelDebug *llmservice.ModelSelectionDebug) (corelib.TokenUsageStat, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 32<<20))
	if err != nil {
		return corelib.TokenUsageStat{}, false, fmt.Errorf("read non-stream upstream response: %w", err)
	}
	chunk, usage, err := synthesizeOpenAIStreamChunk(body, externalModel)
	if err != nil {
		return corelib.TokenUsageStat{}, false, err
	}
	setOpenAIStreamResponseHeaders(w, provider, model, selectedModelDebug)
	w.WriteHeader(statusCode)
	if _, err := w.Write([]byte("data: ")); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	if _, err := w.Write(chunk); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	if _, err := w.Write([]byte("\n\ndata: [DONE]\n\n")); err != nil {
		return applyProviderUsageCost(usage, provider), true, err
	}
	flusher.Flush()
	return applyProviderUsageCost(usage, provider), true, nil
}

func setOpenAIStreamResponseHeaders(w http.ResponseWriter, provider *im.LLMProvider, model *llmservice.AuthorizedModel, selectedModelDebug *llmservice.ModelSelectionDebug) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if model != nil {
		w.Header().Set("X-MaClaw-Authorized-Model", model.Name)
	}
	if selectedModelDebug != nil {
		if selectedModelDebug.SelectionReason != "" {
			w.Header().Set("X-MaClaw-Model-Selection", selectedModelDebug.SelectionReason)
		}
		if len(selectedModelDebug.CapabilityNeeds) > 0 {
			w.Header().Set("X-MaClaw-Model-Needs", strings.Join(selectedModelDebug.CapabilityNeeds, ","))
		}
	}
	if provider != nil && provider.ID != "" {
		w.Header().Set("X-MaClaw-Upstream-Provider", provider.ID)
	}
}

func synthesizeOpenAIStreamChunk(body []byte, externalModel string) ([]byte, corelib.TokenUsageStat, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, corelib.TokenUsageStat{}, fmt.Errorf("upstream returned non-SSE, non-JSON response: %w", err)
	}
	model := strings.TrimSpace(externalModel)
	if model == "" {
		model, _ = payload["model"].(string)
	}
	id, _ := payload["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}
	choice := firstOpenAIChoice(payload)
	message, _ := choice["message"].(map[string]any)
	delta := map[string]any{"role": "assistant"}
	if reasoning := firstStringField(message, "reasoning_content", "ReasoningContent"); reasoning != "" {
		delta["reasoning_content"] = reasoning
	}
	if content, ok := message["content"].(string); ok {
		delta["content"] = content
	}
	if toolCalls, ok := message["tool_calls"]; ok {
		delta["tool_calls"] = synthesizeOpenAIStreamToolCalls(toolCalls)
	}
	finishReason, _ := choice["finish_reason"].(string)
	if finishReason == "" {
		finishReason = "stop"
	}
	chunk := map[string]any{
		"id":     id,
		"object": "chat.completion.chunk",
		"model":  model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		chunk["usage"] = usage
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, corelib.TokenUsageStat{}, fmt.Errorf("marshal synthesized stream chunk: %w", err)
	}
	return data, parseUsageStats(body), nil
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func synthesizeOpenAIStreamToolCalls(raw any) any {
	calls, ok := raw.([]any)
	if !ok {
		return raw
	}
	out := make([]any, 0, len(calls))
	for i, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		clone := make(map[string]any, len(call)+1)
		for k, v := range call {
			clone[k] = v
		}
		if _, ok := clone["index"]; !ok {
			clone["index"] = i
		}
		out = append(out, clone)
	}
	return out
}

func firstOpenAIChoice(payload map[string]any) map[string]any {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return map[string]any{}
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return map[string]any{}
	}
	return choice
}

func rewriteOpenAIStreamLine(line []byte, externalModel string, usage *corelib.TokenUsageStat) []byte {
	out := append([]byte(nil), line...)
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "data:") {
		return out
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	chunkUsage := corelib.OpenAIStreamUsageFromData([]byte(payload))
	if usage != nil && (chunkUsage.TotalTokens > 0 || chunkUsage.InputTokens > 0 || chunkUsage.OutputTokens > 0) {
		*usage = chunkUsage
	}
	rewritten := corelib.RewriteOpenAIStreamDataModel([]byte(payload), externalModel)
	out = []byte("data: ")
	out = append(out, rewritten...)
	return out
}

func upstreamGatewayStatus(upstreamStatus int) int {
	switch upstreamStatus {
	case http.StatusGatewayTimeout:
		return http.StatusGatewayTimeout
	case http.StatusServiceUnavailable:
		return http.StatusServiceUnavailable
	default:
		if upstreamStatus >= 500 {
			return http.StatusBadGateway
		}
		return upstreamStatus
	}
}

func newLLMEndpointRequestID() string {
	seq := atomic.AddUint64(&globalLLMEndpointRequestSeq, 1)
	return fmt.Sprintf("llm_%x_%d", time.Now().UnixNano(), seq)
}

func llmEndpointDiagnosticFields(requestID, failureStage, providerID string, upstreamStatus, hubStatus int, startedAt time.Time, reg *im.LLMProviderRegistry) map[string]any {
	fields := map[string]any{
		"request_id": requestID,
		"hub_status": hubStatus,
		"elapsed_ms": time.Since(startedAt).Milliseconds(),
	}
	if failureStage != "" {
		fields["failure_stage"] = failureStage
	}
	if providerID != "" {
		fields["provider_id"] = providerID
	}
	if upstreamStatus > 0 {
		fields["upstream_status"] = upstreamStatus
	}
	if reg != nil {
		if provider := reg.FindProvider(providerID); provider != nil {
			host := llmEndpointUpstreamHost(provider.APIURL)
			if host != "" {
				fields["upstream_host"] = host
			}
			return fields
		}
	}
	if officialURL := maclawOfficialHubCenterURL(providerID); officialURL != "" {
		if host := llmEndpointUpstreamHost(officialURL); host != "" {
			fields["upstream_host"] = host
		}
	}
	return fields
}

func maclawOfficialHubCenterURL(providerID string) string {
	if !IsMaClawProviderRequest(providerID) {
		return ""
	}
	module := GetMaClawModule()
	if module == nil || module.Client == nil {
		return ""
	}
	return strings.TrimSpace(module.Client.CurrentHubCenterURL())
}

func llmEndpointUpstreamFailureMessage(providerID string, statusCode int, err error) string {
	providerName := strings.TrimSpace(providerID)
	if providerName == "" {
		providerName = "unknown"
	}
	msg := fmt.Sprintf("upstream LLM provider %q returned HTTP %d", providerName, statusCode)
	if errText := strings.TrimSpace(fmt.Sprint(err)); errText != "" && errText != "<nil>" {
		msg += ": " + errText
	}
	return msg
}

func providerUnavailableError(statusCode int, providerID string, respBody []byte) (int, string, string) {
	providerName := strings.TrimSpace(providerID)
	if providerName == "" {
		providerName = "unknown"
	}
	upstreamMsg := extractUpstreamErrorMessage(respBody)
	if upstreamMsg == "" {
		upstreamMsg = strings.TrimSpace(string(respBody))
		if upstreamMsg == "<nil>" {
			upstreamMsg = ""
		}
		if len(upstreamMsg) > 200 {
			upstreamMsg = upstreamMsg[:200] + "..."
		}
	}
	appendDetail := func(detail string) string {
		if upstreamMsg != "" {
			return detail + " (" + upstreamMsg + ")"
		}
		return detail
	}
	if IsMaClawProviderRequest(providerName) {
		return upstreamGatewayStatus(statusCode), "LLM_OFFICIAL_UNAVAILABLE", appendDetail("MaClaw official service is temporarily unavailable")
	}
	return upstreamGatewayStatus(statusCode), "LLM_UPSTREAM_FAILED", appendDetail(fmt.Sprintf("upstream LLM provider %q is temporarily unavailable", providerName))
}

func llmEndpointUpstreamAuthOrRateError(upstreamStatus int, providerID string, err error) (int, string, string, bool) {
	switch upstreamStatus {
	case http.StatusUnauthorized:
	case http.StatusForbidden:
	case http.StatusTooManyRequests:
	default:
		return 0, "", "", false
	}
	status, code, detail := providerAuthOrRateError(upstreamStatus, providerID, []byte(strings.TrimSpace(fmt.Sprint(err))))
	return status, code, detail, true
}

type authorizedModelForwardResult struct {
	respBody        []byte
	statusCode      int
	providerID      string
	serviceGroupIDs []string
	usageStat       corelib.TokenUsageStat
	localCacheHit   bool
}

type authorizedModelRequestFlight struct {
	done   chan struct{}
	result authorizedModelForwardResult
	err    error
}

type authorizedModelRequestFlightGroup struct {
	mu    sync.Mutex
	calls map[string]*authorizedModelRequestFlight
}

var globalAuthorizedModelRequestFlights authorizedModelRequestFlightGroup

func (g *authorizedModelRequestFlightGroup) do(ctx context.Context, key string, waitTimeout time.Duration, fn func(context.Context) (authorizedModelForwardResult, error)) (authorizedModelForwardResult, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*authorizedModelRequestFlight{}
	}
	if call := g.calls[key]; call != nil {
		recordLLMPromptCacheSingleflightShared(ctx)
		g.mu.Unlock()
		waitCtx := ctx
		var cancel context.CancelFunc
		if waitTimeout > 0 {
			waitCtx, cancel = context.WithTimeout(ctx, waitTimeout)
			defer cancel()
		}
		select {
		case <-waitCtx.Done():
			return authorizedModelForwardResult{}, waitCtx.Err()
		case <-call.done:
			return call.result, call.err
		}
	}
	call := &authorizedModelRequestFlight{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	result, err := fn(ctx)
	call.result = result
	call.err = err
	close(call.done)

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return result, err
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
	cacheKey := ""
	if promptCache != nil && llmPromptCacheableForTenant(r.Context(), body, cacheCfg) {
		var err error
		cacheKey, _, err = llmPromptCacheKey(model, body, externalModel, cacheCfg)
		if err != nil {
			return nil, 0, "", nil, corelib.TokenUsageStat{}, false, err
		}
		cacheKey = tenantScopedLLMPromptCacheKey(r.Context(), cacheKey)
	}
	var result authorizedModelForwardResult
	var err error
	if cacheKey != "" {
		result, err = globalAuthorizedModelRequestFlights.do(r.Context(), cacheKey, time.Duration(cacheCfg.SingleflightWaitTimeoutMS)*time.Millisecond, func(ctx context.Context) (authorizedModelForwardResult, error) {
			return executeAuthorizedModelRequestWithCache(ctx, r, reg, model, body, externalModel, promptCache, cacheCfg)
		})
	} else {
		result, err = executeAuthorizedModelRequestWithCache(r.Context(), r, reg, model, body, externalModel, promptCache, cacheCfg)
	}
	if err != nil {
		return result.respBody, result.statusCode, result.providerID, result.serviceGroupIDs, result.usageStat, result.localCacheHit, err
	}
	return result.respBody, result.statusCode, result.providerID, result.serviceGroupIDs, result.usageStat, result.localCacheHit, nil
}

func executeAuthorizedModelRequestWithCache(ctx context.Context, r *http.Request, reg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, promptCache llmPromptCacheStore, cacheCfg HubLLMPromptCacheConfig) (authorizedModelForwardResult, error) {
	if respBody, statusCode, providerID, serviceGroupIDs, usageStat, ok, err := getCachedAuthorizedModelResponse(ctx, promptCache, model, body, externalModel, cacheCfg); err != nil {
		return authorizedModelForwardResult{}, err
	} else if ok {
		return authorizedModelForwardResult{respBody: respBody, statusCode: statusCode, providerID: providerID, serviceGroupIDs: serviceGroupIDs, usageStat: usageStat, localCacheHit: true}, nil
	}
	var lastErr error
	var lastBody []byte
	var lastStatus int
	var lastProviderID string
	request := r.Clone(ctx)
	for _, providerID := range llmservice.OrderProvidersForRequest(body, model) {
		if IsMaClawProviderRequest(providerID) {
			serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, providerID)
			respBody, statusCode, fwdErr := forwardMaClawOfficialRequestWithCompatRetry(ctx, body, store.TenantIDFromContext(ctx), serviceGroupIDs)
			if fwdErr != nil {
				lastErr = fwdErr
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = providerID
				continue
			}
			if shouldTryNextProviderStatusForProvider(providerID, statusCode) {
				lastBody = respBody
				lastStatus = statusCode
				lastProviderID = providerID
				lastErr = fmt.Errorf("provider %q returned http %d", providerID, statusCode)
				log.Printf("[LLM-V1] provider %q returned %d, trying next provider", providerID, statusCode)
				continue
			}
			usageStat := parseUsageStats(respBody)
			if isTerminalProviderStatus(providerID, statusCode) {
				return authorizedModelForwardResult{
					respBody:        respBody,
					statusCode:      statusCode,
					providerID:      providerID,
					serviceGroupIDs: serviceGroupIDs,
					usageStat:       usageStat,
					localCacheHit:   false,
				}, nil
			}
			_ = putCachedAuthorizedModelResponse(ctx, promptCache, model, body, externalModel, respBody, statusCode, providerID, serviceGroupIDs, usageStat, cacheCfg)
			return authorizedModelForwardResult{
				respBody:        respBody,
				statusCode:      statusCode,
				providerID:      providerID,
				serviceGroupIDs: serviceGroupIDs,
				usageStat:       usageStat,
				localCacheHit:   false,
			}, nil
		}
		provider := reg.FindProvider(providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %q not configured", providerID)
			continue
		}
		if gateErr := globalProviderResilience.beforeAttempt(provider); gateErr != nil {
			lastErr = gateErr
			lastProviderID = provider.ID
			continue
		}
		respBody, statusCode, err := forwardLLMRequest(request, provider, body, externalModel)
		if shouldCountProviderFailure(statusCode, err) {
			globalProviderResilience.recordFailure(provider)
		} else {
			globalProviderResilience.recordSuccess(provider.ID)
		}
		if err != nil {
			lastErr = err
			lastBody = respBody
			lastStatus = statusCode
			lastProviderID = provider.ID
			continue
		}
		if statusCode >= 500 || statusCode == http.StatusNotFound || statusCode == http.StatusUnprocessableEntity ||
			statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden ||
			statusCode == http.StatusTooManyRequests {
			lastBody = respBody
			lastStatus = statusCode
			lastProviderID = provider.ID
			lastErr = fmt.Errorf("provider %q returned http %d", provider.ID, statusCode)
			log.Printf("[LLM-V1] provider %q returned %d, trying next provider", provider.ID, statusCode)
			continue
		}
		usageStat := applyProviderUsageCost(parseUsageStats(respBody), provider)
		serviceGroupIDs := llmservice.ServiceGroupIDsForProvider(model, provider.ID)
		_ = putCachedAuthorizedModelResponse(ctx, promptCache, model, body, externalModel, respBody, statusCode, provider.ID, serviceGroupIDs, usageStat, cacheCfg)
		return authorizedModelForwardResult{
			respBody:        respBody,
			statusCode:      statusCode,
			providerID:      provider.ID,
			serviceGroupIDs: serviceGroupIDs,
			usageStat:       usageStat,
			localCacheHit:   false,
		}, nil
	}
	if lastBody != nil && lastStatus > 0 {
		return authorizedModelForwardResult{respBody: lastBody, statusCode: lastStatus, providerID: lastProviderID}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorized providers available for model %q", model.Name)
	}
	return authorizedModelForwardResult{respBody: lastBody, statusCode: lastStatus, providerID: lastProviderID}, lastErr
}

func forwardMaClawOfficialRequestWithCompatRetry(ctx context.Context, body map[string]any, tenantID string, serviceGroupIDs []string) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal maclaw official request: %w", err)
	}
	respBody, statusCode, err := ForwardViaMaClaw(ctx, payload, tenantID, serviceGroupIDs...)
	if err != nil || statusCode != http.StatusBadRequest {
		return respBody, statusCode, err
	}
	if retryBody, ok := maclawOfficialSanitizedRetryBody(body); ok {
		retryPayload, marshalErr := json.Marshal(retryBody)
		if marshalErr != nil {
			return respBody, statusCode, nil
		}
		log.Printf("[LLM-V1] maclaw official returned 400; retrying with sanitized OpenAI-compatible body")
		retryRespBody, retryStatusCode, retryErr := ForwardViaMaClaw(ctx, retryPayload, tenantID, serviceGroupIDs...)
		if retryErr == nil && retryStatusCode != http.StatusBadRequest {
			return retryRespBody, retryStatusCode, nil
		}
		if retryErr != nil {
			return retryRespBody, retryStatusCode, retryErr
		}
		respBody, statusCode = retryRespBody, retryStatusCode
	}
	if retryBody, ok := maclawOfficialToollessRetryBody(body); ok {
		retryPayload, marshalErr := json.Marshal(retryBody)
		if marshalErr != nil {
			return respBody, statusCode, nil
		}
		log.Printf("[LLM-V1] maclaw official returned 400; retrying without tool schemas")
		retryRespBody, retryStatusCode, retryErr := ForwardViaMaClaw(ctx, retryPayload, tenantID, serviceGroupIDs...)
		if retryErr == nil && retryStatusCode != http.StatusBadRequest {
			return retryRespBody, retryStatusCode, nil
		}
		if retryErr != nil {
			return retryRespBody, retryStatusCode, retryErr
		}
		respBody, statusCode = retryRespBody, retryStatusCode
	}
	return respBody, statusCode, nil
}

func maclawOfficialSanitizedRetryBody(body map[string]any) (map[string]any, bool) {
	if !maclawOfficialBodyHasCompatRisk(body) {
		return nil, false
	}
	retry := cloneLLMEndpointBody(body)
	corelib.SanitizeCodeGenOpenAICompatBody(retry)
	return retry, true
}

func maclawOfficialToollessRetryBody(body map[string]any) (map[string]any, bool) {
	if !maclawOfficialBodyHasCompatRisk(body) || maclawOfficialBodyHasToolHistory(body) {
		return nil, false
	}
	retry := cloneLLMEndpointBody(body)
	for _, key := range []string{
		"tools",
		"tool_choice",
		"functions",
		"function_call",
		"parallel_tool_calls",
		"response_format",
		"stream_options",
	} {
		delete(retry, key)
	}
	return retry, true
}

func maclawOfficialBodyHasCompatRisk(body map[string]any) bool {
	for _, key := range []string{
		"tools",
		"tool_choice",
		"functions",
		"function_call",
		"parallel_tool_calls",
		"response_format",
		"stream_options",
	} {
		if _, ok := body[key]; ok {
			return true
		}
	}
	return false
}

func maclawOfficialBodyHasToolHistory(body map[string]any) bool {
	messages, ok := body["messages"].([]any)
	if !ok {
		if items, ok := body["messages"].([]interface{}); ok {
			messages = items
		}
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			if m, ok := item.(map[string]interface{}); ok {
				msg = m
			} else {
				continue
			}
		}
		role := strings.TrimSpace(fmt.Sprint(msg["role"]))
		if role == "tool" || role == "function" {
			return true
		}
		if calls, ok := msg["tool_calls"]; ok && maclawOfficialToolFieldPresent(calls) {
			return true
		}
		if call, ok := msg["function_call"]; ok && maclawOfficialToolFieldPresent(call) {
			return true
		}
	}
	return false
}

func maclawOfficialToolFieldPresent(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		rv := reflect.ValueOf(value)
		return rv.IsValid() && !rv.IsZero()
	}
}

func shouldTryNextProviderStatus(statusCode int) bool {
	return statusCode >= 500 ||
		statusCode == http.StatusNotFound ||
		statusCode == http.StatusUnprocessableEntity ||
		statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests
}

func shouldTryNextProviderStatusForProvider(providerID string, statusCode int) bool {
	if isTerminalProviderStatus(providerID, statusCode) {
		return false
	}
	return shouldTryNextProviderStatus(statusCode)
}

func isTerminalProviderStatus(providerID string, statusCode int) bool {
	if !IsMaClawProviderRequest(providerID) {
		return false
	}
	return statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests
}

func providerAuthOrRateError(statusCode int, providerID string, respBody []byte) (int, string, string) {
	providerName := strings.TrimSpace(providerID)
	if providerName == "" {
		providerName = "unknown"
	}
	upstreamMsg := extractUpstreamErrorMessage(respBody)
	if upstreamMsg == "" {
		upstreamMsg = strings.TrimSpace(string(respBody))
		if upstreamMsg == "<nil>" {
			upstreamMsg = ""
		}
		if len(upstreamMsg) > 200 {
			upstreamMsg = upstreamMsg[:200] + "..."
		}
	}
	appendDetail := func(detail string) string {
		if upstreamMsg != "" {
			return detail + " (" + upstreamMsg + ")"
		}
		return detail
	}
	if IsMaClawProviderRequest(providerName) {
		switch statusCode {
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests, "LLM_OFFICIAL_RATE_LIMITED", appendDetail("MaClaw official service is rate limited")
		case http.StatusUnauthorized:
			return http.StatusBadGateway, "LLM_OFFICIAL_AUTH_FAILED", appendDetail("MaClaw official service rejected Hub credentials")
		default:
			return http.StatusForbidden, "LLM_OFFICIAL_AUTHORIZATION_DENIED", appendDetail("MaClaw official service denied this tenant authorization")
		}
	}
	switch statusCode {
	case http.StatusTooManyRequests:
		return http.StatusTooManyRequests, "LLM_UPSTREAM_RATE_LIMITED", appendDetail(fmt.Sprintf("upstream LLM provider %q is rate limited", providerName))
	case http.StatusUnauthorized:
		return http.StatusBadGateway, "LLM_UPSTREAM_AUTH_FAILED", appendDetail(fmt.Sprintf("upstream LLM provider %q rejected the configured API key", providerName))
	default:
		return http.StatusBadGateway, "LLM_UPSTREAM_AUTH_FAILED", appendDetail(fmt.Sprintf("upstream LLM provider %q denied access for the configured API key or model", providerName))
	}
}

func firstPromptCacheSource(sources []any) llmPromptCacheStore {
	for _, source := range sources {
		if cache, ok := source.(llmPromptCacheStore); ok && cache != nil {
			return cache
		}
	}
	return nil
}

func applyProviderUsageCost(usage corelib.TokenUsageStat, provider *im.LLMProvider) corelib.TokenUsageStat {
	inputPrice := corelib.DefaultLLMInputPricePerMTokensRMB
	outputPrice := corelib.DefaultLLMOutputPricePerMTokensRMB
	if provider != nil {
		inputPrice = corelib.NormalizeLLMTokenPricePerMTokensRMB(provider.InputPricePerMTokensRMB, inputPrice)
		outputPrice = corelib.NormalizeLLMTokenPricePerMTokensRMB(provider.OutputPricePerMTokensRMB, outputPrice)
	}
	usage.InputPricePerMTokensRMB = inputPrice
	usage.OutputPricePerMTokensRMB = outputPrice
	usage.InputCostRMB, usage.OutputCostRMB, usage.TotalCostRMB = corelib.CalculateLLMCostRMB(usage.InputTokens, usage.OutputTokens, inputPrice, outputPrice)
	return usage
}

func registryResponse(r *http.Request, reg *im.LLMProviderRegistry, serviceReg *llmservice.Registry, warnings []string) llmProviderRegistryResponse {
	usageByProvider := filterRemoteCodingToolTokenUsage(reg.TokenUsage)
	providers := make([]any, 0, len(reg.Providers))
	availableModels := make([]string, 0, len(reg.Providers))
	seenModels := map[string]struct{}{}
	for _, p := range reg.Providers {
		usage := corelib.TokenUsageStat{}
		if stat := usageByProvider[p.ID]; stat != nil {
			usage = *stat
		}
		wireAPI := normalizeProviderWireAPI(p.WireAPI)
		snapshot := globalProviderConcurrency.snapshot(p.ID, p.MaxConcurrency, p.MaxQueueWaiters, p.QueueTimeoutMS)
		resilience := globalProviderResilience.snapshot(&p)
		providers = append(providers, map[string]any{
			"id":                            p.ID,
			"name":                          p.Name,
			"api_url":                       p.APIURL,
			"api_key":                       "",
			"has_api_key":                   p.APIKey != "",
			"model":                         p.Model,
			"protocol":                      normalizeProviderProtocol(p.Protocol),
			"wire_api":                      wireAPI,
			"agent_type":                    strings.TrimSpace(p.AgentType),
			"input_price_per_m_tokens_rmb":  p.InputPricePerMTokensRMB,
			"output_price_per_m_tokens_rmb": p.OutputPricePerMTokensRMB,
			"max_concurrency":               p.MaxConcurrency,
			"max_queue_waiters":             p.MaxQueueWaiters,
			"queue_timeout_ms":              p.QueueTimeoutMS,
			"circuit_breaker_threshold":     p.CircuitBreakerThreshold,
			"circuit_breaker_cooldown_ms":   p.CircuitBreakerCooldownMS,
			"failure_backoff_base_ms":       p.FailureBackoffBaseMS,
			"failure_backoff_max_ms":        p.FailureBackoffMaxMS,
			"in_flight":                     snapshot.InFlight,
			"queue_waiters":                 snapshot.QueueWaiters,
			"consecutive_failures":          resilience.ConsecutiveFailures,
			"circuit_open":                  resilience.CircuitOpen,
			"circuit_open_until":            resilience.CircuitOpenUntil,
			"backoff_until":                 resilience.BackoffUntil,
			"usage":                         usage,
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
	// ServiceAvailable: the LLM service has at least one routable path.
	// True when either local providers are configured, OR the service registry
	// contains a model service group with at least one built-in provider route.
	serviceAvailable := len(reg.Providers) > 0
	if !serviceAvailable {
		serviceAvailable = llmservice.HasBuiltinProviderRoute(serviceReg)
	}
	return llmProviderRegistryResponse{
		Enabled:                  reg.Enabled,
		CurrentProviderID:        reg.CurrentProviderID,
		SmartRouteSingleDevice:   reg.SmartRouteSingleDevice,
		DownstreamMaxConcurrency: reg.DownstreamMaxConcurrency,
		UpstreamTimeoutSec:       reg.UpstreamTimeoutSec,
		UserRateLimitPerMinute:   reg.UserRateLimitPerMinute,
		UserRateLimitBurst:       reg.UserRateLimitBurst,
		Providers:                providers,
		ExposeAPIBaseURL:         base,
		ExposeBaseURL:            base + "/chat/completions",
		ExposeModelsURL:          base + "/models",
		AvailableModels:          availableModels,
		AuthMode:                 "viewer_bearer_token",
		AuthHint:                 "Use Authorization: Bearer <viewer access token>. Reuse the access_token returned by /api/auth/email-confirm or /api/auth/email-poll after email sign-in.",
		Hints:                    mergedHints,
		Warnings:                 append([]string(nil), warnings...),
		ServiceAvailable:         serviceAvailable,
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
				"period_limits":     card.PeriodLimits,
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
		GlobalServiceGroupIDs:       append([]string(nil), reg.GlobalServiceGroupIDs...),
		GroupBindings:               reg.GroupBindings,
		UserBindings:                reg.UserBindings,
		Cards:                       cards,
		Grants:                      reg.Grants,
		SystemDefaultServiceGroupID: reg.SystemDefaultServiceGroupID,
		DefaultNewUserServiceGroups: append([]string(nil), reg.DefaultNewUserServiceGroups...),
		DefaultNewUserDurationDays:  reg.DefaultNewUserDurationDays,
		DefaultNewUserCredits:       reg.DefaultNewUserCredits,
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
	seenServiceGroups := map[string]string{}
	for _, group := range reg.ModelServiceGroups {
		id := strings.TrimSpace(group.ID)
		if id == "" {
			issues = append(issues, "model service group id is required")
			continue
		}
		key := strings.ToLower(id)
		if prev, ok := seenServiceGroups[key]; ok {
			issues = append(issues, fmt.Sprintf("duplicate model service group id: %s conflicts with %s", id, prev))
			continue
		}
		seenServiceGroups[key] = id
	}
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
	check("global service groups", reg.GlobalServiceGroupIDs)
	check("system default service group", []string{reg.SystemDefaultServiceGroupID})
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

func resolveAuthorizedModels(ctx context.Context, r *http.Request, system store.SystemSettingsRepository, securitySvc *security.SecurityService, userID string, email string) (*llmservice.ServiceStatus, []llmservice.AuthorizedModel, *im.LLMProviderRegistry, *llmservice.Registry, error) {
	providerReg, err := loadCachedLLMProviderRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	status, models, serviceReg, err := resolveAuthorizedModelsWithProviderRegistry(ctx, r, system, securitySvc, userID, email, providerReg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return status, models, providerReg, serviceReg, nil
}

func resolveAuthorizedModelsWithProviderRegistry(ctx context.Context, r *http.Request, system store.SystemSettingsRepository, securitySvc *security.SecurityService, userID string, email string, providerReg *im.LLMProviderRegistry) (*llmservice.ServiceStatus, []llmservice.AuthorizedModel, *llmservice.Registry, error) {
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return nil, nil, nil, err
	}
	if providerReg == nil {
		providerReg, err = loadCachedLLMProviderRegistry(ctx, system)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	status, models, err := llmservice.ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, userID, email, externalLLMBaseURL(r))
	if err != nil {
		return nil, nil, nil, err
	}
	status, models = filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	return status, models, reg, nil
}

type llmBillingDenial struct {
	Code              string
	Message           string
	RetryAfterSeconds int64
	RetryAfterAt      string
}

func filterAuthorizedModelsByBillingEligibility(reg *llmservice.Registry, userID string, email string, body map[string]any, models []llmservice.AuthorizedModel) ([]llmservice.AuthorizedModel, map[string]llmBillingDenial, llmBillingDenial) {
	filtered := make([]llmservice.AuthorizedModel, 0, len(models))
	denied := map[string]llmBillingDenial{}
	firstDenial := llmBillingDenial{}
	for i := range models {
		eligibleModel, denial, err := filterAuthorizedModelByBillingEligibility(reg, userID, email, body, &models[i])
		if err != nil {
			if denial.Message == "" {
				denial.Message = err.Error()
			}
			denied[strings.ToLower(strings.TrimSpace(models[i].Name))] = denial
			if firstDenial.Code == "" || llmBillingDenialRank(denial.Code) < llmBillingDenialRank(firstDenial.Code) {
				firstDenial = denial
			}
			continue
		}
		filtered = append(filtered, *eligibleModel)
	}
	return filtered, denied, firstDenial
}

func filterAuthorizedModelByBillingEligibility(reg *llmservice.Registry, userID string, email string, body map[string]any, model *llmservice.AuthorizedModel) (*llmservice.AuthorizedModel, llmBillingDenial, error) {
	if model == nil || reg == nil {
		return model, llmBillingDenial{}, nil
	}
	orderedProviders := llmservice.OrderProvidersForRequest(body, model)
	if len(orderedProviders) == 0 {
		orderedProviders = append([]string(nil), model.ProviderIDs...)
	}
	eligibleProviderIDs := make([]string, 0, len(orderedProviders))
	firstDenial := llmBillingDenial{}
	now := time.Now().UTC()
	for _, providerID := range orderedProviders {
		allowed, denial := billingEligibilityForProvider(reg, userID, email, llmservice.ServiceGroupIDsForProvider(model, providerID), now)
		if allowed {
			eligibleProviderIDs = append(eligibleProviderIDs, providerID)
			continue
		}
		if firstDenial.Code == "" || llmBillingDenialRank(denial.Code) < llmBillingDenialRank(firstDenial.Code) {
			firstDenial = denial
		}
	}
	if len(eligibleProviderIDs) == 0 {
		if firstDenial.Code == "" {
			firstDenial = llmBillingDenial{Code: "LLM_SERVICE_CREDITS_REQUIRED", Message: "selected model requires an active grant with remaining credits"}
		}
		return nil, firstDenial, errors.New(firstDenial.Message)
	}
	clone := *model
	clone.ProviderIDs = eligibleProviderIDs
	return &clone, llmBillingDenial{}, nil
}

func llmBillingDenialRank(code string) int {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "LLM_SERVICE_PERIOD_LIMITED":
		return 0
	case "LLM_SERVICE_GRANT_QUEUED":
		return 1
	case "LLM_SERVICE_CREDITS_EXHAUSTED":
		return 2
	case "LLM_SERVICE_GRANT_EXPIRED":
		return 3
	case "LLM_SERVICE_CREDITS_REQUIRED":
		return 4
	default:
		return 5
	}
}

func llmBillingDenialHTTPStatus(denial llmBillingDenial) int {
	if strings.EqualFold(strings.TrimSpace(denial.Code), "LLM_SERVICE_PERIOD_LIMITED") {
		return http.StatusTooManyRequests
	}
	return http.StatusForbidden
}

func billingEligibilityForProvider(reg *llmservice.Registry, userID string, email string, serviceGroupIDs []string, now time.Time) (bool, llmBillingDenial) {
	allowed, _, code, message, _, _, _ := llmservice.BillingEligibilityForServiceGroupsForUserID(reg, userID, email, serviceGroupIDs, now)
	denial := llmBillingDenial{Code: code, Message: message}
	if !allowed && code == "LLM_SERVICE_PERIOD_LIMITED" {
		if retryAt := llmservice.PeriodLimitRetryAtForServiceGroupsForUserID(reg, userID, email, serviceGroupIDs, now); retryAt != nil && retryAt.After(now) {
			denial.RetryAfterAt = retryAt.Format(time.RFC3339)
			denial.RetryAfterSeconds = int64((retryAt.Sub(now) + time.Second - 1) / time.Second)
		}
	} else if !allowed && code == "LLM_SERVICE_GRANT_QUEUED" {
		if startsAt := llmservice.GrantStartAtForServiceGroupsForUserID(reg, userID, email, serviceGroupIDs, now); startsAt != nil && startsAt.After(now) {
			denial.RetryAfterAt = startsAt.Format(time.RFC3339)
			denial.RetryAfterSeconds = int64((startsAt.Sub(now) + time.Second - 1) / time.Second)
		}
	}
	return allowed, denial
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
			sb.WriteString(fmt.Sprintf("%.3f", card.PeriodLimits.FiveHour))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%.3f", card.PeriodLimits.Daily))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%.3f", card.PeriodLimits.Weekly))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%.3f", card.PeriodLimits.Monthly))
			sb.WriteByte(',')
			sb.WriteString(fmt.Sprintf("%d", card.DurationDays))
			sb.WriteByte(',')
			sb.WriteString(card.CreatedAt.Format(time.RFC3339))
			sb.WriteByte(',')
			sb.WriteString(strings.TrimSpace(card.RedeemedByEmail))
			sb.WriteByte(',')
			sb.WriteString(card.PlainCode())
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
		_ = writer.Write([]string{"id", "code", "status", "label", "service_group_ids", "credits", "five_hour_credits", "daily_credits", "weekly_credits", "monthly_credits", "duration_days", "created_at", "redeemed_by_email", "redeemed_at"})
		for _, card := range cards {
			status := "unused"
			redeemedAt := ""
			if card.RedeemedAt != nil {
				status = "redeemed"
				redeemedAt = card.RedeemedAt.Format(time.RFC3339)
			}
			_ = writer.Write([]string{
				strings.TrimSpace(card.ID),
				card.PlainCode(),
				status,
				strings.TrimSpace(card.Label),
				strings.Join(card.ServiceGroupIDs, ","),
				fmt.Sprintf("%.3f", card.Credits),
				fmt.Sprintf("%.3f", card.PeriodLimits.FiveHour),
				fmt.Sprintf("%.3f", card.PeriodLimits.Daily),
				fmt.Sprintf("%.3f", card.PeriodLimits.Weekly),
				fmt.Sprintf("%.3f", card.PeriodLimits.Monthly),
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

func isAllowedLLMServiceCardDuration(days int) bool {
	_, ok := llmServiceCardDefaultCreditsByDuration[days]
	return ok
}

func defaultLLMServiceCardCredits(days int) float64 {
	if credits, ok := llmServiceCardDefaultCreditsByDuration[days]; ok {
		return credits
	}
	return llmServiceCardDefaultCreditsByDuration[30]
}

func allowedLLMServiceCardDurationsLabel() string {
	durations := make([]int, 0, len(llmServiceCardDefaultCreditsByDuration))
	for days := range llmServiceCardDefaultCreditsByDuration {
		durations = append(durations, days)
	}
	sort.Ints(durations)
	parts := make([]string, 0, len(durations))
	for _, days := range durations {
		parts = append(parts, strconv.Itoa(days))
	}
	return strings.Join(parts, ", ")
}

func sanitizeLLMServiceCardPeriodLimits(days int, limits llmservice.CreditPeriodLimits) llmservice.CreditPeriodLimits {
	limits = sanitizeLLMServicePeriodLimits(limits)
	if days < 7 {
		limits.Daily = 0
	}
	if days < 30 {
		limits.Weekly = 0
	}
	if days < 91 {
		limits.Monthly = 0
	}
	return limits
}

func sanitizeLLMServicePeriodLimits(limits llmservice.CreditPeriodLimits) llmservice.CreditPeriodLimits {
	if limits.FiveHour < 0 {
		limits.FiveHour = 0
	}
	if limits.Daily < 0 {
		limits.Daily = 0
	}
	if limits.Weekly < 0 {
		limits.Weekly = 0
	}
	if limits.Monthly < 0 {
		limits.Monthly = 0
	}
	return limits
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
	// Decrypt and check card code only if no cheaper field matched.
	if code := card.PlainCode(); code != "" {
		if strings.Contains(strings.ToLower(code), search) {
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

func writeLLMServiceCardAdminAudit(ctx context.Context, audit store.AdminAuditRepository, tenantID, action string, payload map[string]any) {
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
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(admin.TenantID)
	}
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	_ = audit.Create(ctx, &store.AdminAuditLog{
		ID:          fmt.Sprintf("aa_%d", time.Now().UnixNano()),
		TenantID:    tenantID,
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
	return corelib.NormalizeLLMProviderProtocol(v)
}

func normalizeProviderWireAPI(v string) string {
	return corelib.NormalizeLLMProviderWireAPI(v)
}

const legacyHubLLMConfigKey = "hub_llm_config"

func syncLegacyHubLLMConfig(ctx context.Context, system store.SystemSettingsRepository, reg *im.LLMProviderRegistry) error {
	cfg := reg.ToHubLLMConfig()
	if cfg == nil {
		cfg = &im.HubLLMConfig{}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return system.Set(ctx, legacyHubLLMConfigKey, string(data))
}

func forwardLLMRequest(r *http.Request, p *im.LLMProvider, body map[string]any, externalModel string) ([]byte, int, error) {
	if p == nil {
		return nil, 0, fmt.Errorf("provider is required")
	}
	release, err := globalProviderConcurrency.acquire(r.Context(), p.ID, p.MaxConcurrency, p.MaxQueueWaiters, p.QueueTimeoutMS)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	provider := toCoreLLMEndpointProvider(p)
	return corelib.ForwardLLMEndpointProviderRequest(r.Context(), provider, body, llmProviderUpstreamHTTPClient(provider.MaclawLLMConfig()), externalModel)
}

func forwardRawResponsesRequest(r *http.Request, p *im.LLMProvider, body map[string]any) ([]byte, int, error) {
	if p == nil {
		return nil, 0, fmt.Errorf("provider is required")
	}
	release, err := globalProviderConcurrency.acquire(r.Context(), p.ID, p.MaxConcurrency, p.MaxQueueWaiters, p.QueueTimeoutMS)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	provider := toCoreLLMEndpointProvider(p)
	return corelib.ForwardOpenAIResponsesRawRequest(r.Context(), provider.MaclawLLMConfig(), body, llmProviderUpstreamHTTPClient(provider.MaclawLLMConfig()))
}

func llmProviderUpstreamHTTPClient(cfg corelib.MaclawLLMConfig) *http.Client {
	return corelib.NewLLMEndpointHTTPClient(cfg)
}

func llmProviderUpstreamStreamHTTPClient(cfg corelib.MaclawLLMConfig) *http.Client {
	client := corelib.NewLLMEndpointHTTPClient(cfg)
	client.Timeout = 0
	return client
}

func toCoreLLMEndpointProvider(p *im.LLMProvider) corelib.LLMEndpointProvider {
	if p == nil {
		return corelib.LLMEndpointProvider{}
	}
	return corelib.LLMEndpointProvider{
		ID:                       p.ID,
		Name:                     p.Name,
		APIURL:                   p.APIURL,
		APIKey:                   p.APIKey,
		Model:                    p.Model,
		Protocol:                 p.Protocol,
		WireAPI:                  p.WireAPI,
		AgentType:                p.AgentType,
		UpstreamTimeoutSec:       p.UpstreamTimeoutSec,
		MaxConcurrency:           p.MaxConcurrency,
		MaxQueueWaiters:          p.MaxQueueWaiters,
		QueueTimeoutMS:           p.QueueTimeoutMS,
		CircuitBreakerThreshold:  p.CircuitBreakerThreshold,
		CircuitBreakerCooldownMS: p.CircuitBreakerCooldownMS,
		FailureBackoffBaseMS:     p.FailureBackoffBaseMS,
		FailureBackoffMaxMS:      p.FailureBackoffMaxMS,
	}
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

// extractUpstreamErrorMessage tries to extract a human-readable error message
// from an upstream LLM provider's error response body. Returns empty string
// if the body cannot be parsed. The result is truncated to 200 characters.
func extractUpstreamErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var errBody struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errBody); err != nil {
		return ""
	}
	msg := strings.TrimSpace(errBody.Error.Message)
	if msg == "" {
		msg = strings.TrimSpace(errBody.Message)
	}
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}
