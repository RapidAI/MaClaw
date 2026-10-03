package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

const (
	llmAdminAPIDocPath     = "/api/llm/admin-api.md"
	llmAdminAPIOpenAPIPath = "/api/llm/admin-api.json"
)

type llmAdminPrincipal struct {
	session bool
	scopes  []string
}

type llmAdminPrincipalKey struct{}

func RequireLLMAdmin(admins *auth.AdminService, llm *llmservice.Service, next http.HandlerFunc) http.HandlerFunc {
	return RequireLLMAdminScope(admins, llm, "", next)
}

func RequireLLMAdminScope(admins *auth.AdminService, llm *llmservice.Service, scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := llmAdminCredential(r)
		if strings.HasPrefix(token, "hck_") {
			if llm == nil {
				writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Invalid admin API key")
				return
			}
			key, err := llm.AuthorizeAdminAPIKey(r.Context(), token, scope)
			if err != nil {
				status, code, message := llmAdminAuthFailure(err)
				writeError(w, status, code, message)
				return
			}
			principal := llmAdminPrincipal{scopes: append([]string(nil), key.Scopes...)}
			next(w, r.WithContext(context.WithValue(r.Context(), llmAdminPrincipalKey{}, principal)))
			return
		}
		RequireAdmin(admins, func(w http.ResponseWriter, r *http.Request) {
			principal := llmAdminPrincipal{session: true}
			next(w, r.WithContext(context.WithValue(r.Context(), llmAdminPrincipalKey{}, principal)))
		})(w, r)
	}
}

func llmAdminAllows(r *http.Request, scope string) bool {
	if r == nil {
		return true
	}
	principal, ok := r.Context().Value(llmAdminPrincipalKey{}).(llmAdminPrincipal)
	if !ok {
		return true
	}
	if principal.session || len(principal.scopes) == 0 {
		return true
	}
	for _, item := range principal.scopes {
		if item == scope {
			return true
		}
	}
	return false
}

func rejectLLMAdminScope(w http.ResponseWriter, scope string) {
	writeError(w, http.StatusForbidden, "ADMIN_FORBIDDEN", "api key is missing the "+scope+" scope")
}

func llmAdminAuthFailure(err error) (int, string, string) {
	if err == nil {
		return http.StatusOK, "", ""
	}
	if errors.Is(err, llmservice.ErrAdminAPIKeyExpired) {
		return http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin API key expired"
	}
	if errors.Is(err, llmservice.ErrAdminAPIKeyScope) {
		return http.StatusForbidden, "ADMIN_FORBIDDEN", err.Error()
	}
	return http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Invalid admin API key"
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
		writeLLMAdminJSON(w, http.StatusOK, map[string]any{
			"keys":        keys,
			"doc_url":     llmAdminAPIDocPath,
			"openapi_url": llmAdminAPIOpenAPIPath,
		})
	}
}

func adminCreateLLMAdminAPIKey(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string    `json:"name"`
			Scopes    *[]string `json:"scopes"`
			ExpiresAt string    `json:"expires_at"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		spec := llmservice.AdminAPIKeySpec{Name: req.Name}
		if req.Scopes != nil {
			spec.Scopes = *req.Scopes
		}
		expiresAt, err := parseAdminAPIKeyExpiry(req.ExpiresAt)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		spec.ExpiresAt = expiresAt
		created, err := svc.CreateAdminAPIKeySpec(r.Context(), spec)
		if err != nil {
			writeLLMAdminJSON(w, llmAdminHTTPStatus(err), map[string]string{"error": err.Error()})
			return
		}
		createdPayload := map[string]any{
			"status":      "ok",
			"id":          created.ID,
			"name":        created.Name,
			"prefix":      created.Prefix,
			"api_key":     created.APIKey,
			"created_at":  created.CreatedAt,
			"doc_url":     llmAdminAPIDocPath,
			"openapi_url": llmAdminAPIOpenAPIPath,
		}
		if len(created.Scopes) > 0 {
			createdPayload["scopes"] = created.Scopes
		}
		if !created.ExpiresAt.IsZero() {
			createdPayload["expires_at"] = created.ExpiresAt
		}
		writeLLMAdminJSON(w, http.StatusOK, createdPayload)
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
			writeLLMAdminJSON(w, llmAdminHTTPStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeLLMAdminJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminImportLLMProviderArrays(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DryRun bool                             `json:"dry_run"`
			Arrays []llmservice.ProviderArrayImport `json:"arrays"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		body, err = foldCapabilityTagStrings(body)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if req.DryRun {
			if !llmAdminAllows(r, llmservice.AdminAPIScopeRead) && !llmAdminAllows(r, llmservice.AdminAPIScopeWrite) {
				rejectLLMAdminScope(w, llmservice.AdminAPIScopeRead)
				return
			}
		} else if !llmAdminAllows(r, llmservice.AdminAPIScopeWrite) {
			rejectLLMAdminScope(w, llmservice.AdminAPIScopeWrite)
			return
		}
		var result *llmservice.ProviderArrayImportResult
		if req.DryRun {
			result, err = svc.PreviewProviderArrays(r.Context(), req.Arrays)
		} else {
			result, err = svc.ImportProviderArrays(r.Context(), req.Arrays)
		}
		if err != nil {
			writeLLMAdminJSON(w, llmAdminHTTPStatus(err), map[string]string{"error": err.Error()})
			return
		}
		payload := map[string]any{"status": "ok", "arrays": result.Arrays}
		if req.DryRun {
			payload["dry_run"] = true
		}
		writeLLMAdminJSON(w, http.StatusOK, payload)
	}
}

func adminListLLMProviderArrayInventory(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inventory, err := svc.ListProviderArrayInventory(r.Context())
		if err != nil {
			writeLLMAdminJSON(w, llmAdminHTTPStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeLLMAdminJSON(w, http.StatusOK, inventory)
	}
}

func adminPatchLLMProviderMember(svc *llmservice.Service, bank *SkillMarketHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		var raw map[string]json.RawMessage
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if len(raw) == 0 {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "no changes"})
			return
		}
		if _, hasIDs := raw["allowed_node_ids"]; hasIDs {
			if _, hasNodes := raw["allowed_nodes"]; hasNodes {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "send allowed_nodes or allowed_node_ids, not both"})
				return
			}
		}
		patch := llmservice.ProviderMemberPatch{}
		for key, value := range raw {
			switch key {
			case "enabled":
				var enabled bool
				if err := json.Unmarshal(value, &enabled); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "enabled must be a boolean"})
					return
				}
				patch.Enabled = &enabled
			case "dispatch_weight", "priority", "requests_per_minute", "requests_per_day", "rate_limit_cooldown_sec":
				number, err := jsonWholeInt(value)
				if err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": key + " must be an integer"})
					return
				}
				switch key {
				case "dispatch_weight":
					patch.DispatchWeight = &number
				case "priority":
					patch.Priority = &number
				case "requests_per_minute":
					patch.RequestsPerMinute = &number
				case "requests_per_day":
					patch.RequestsPerDay = &number
				case "rate_limit_cooldown_sec":
					patch.RateLimitCooldownSec = &number
				}
			case "model_map":
				var modelMap map[string]string
				if err := json.Unmarshal(value, &modelMap); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "model_map must be an object"})
					return
				}
				patch.ModelMapSet = true
				patch.ModelMap = modelMap
			case "allowed_node_ids":
				var ids []string
				if err := json.Unmarshal(value, &ids); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "allowed_node_ids must be an array"})
					return
				}
				patch.AllowedNodeIDsSet = true
				patch.AllowedNodeIDs = llmpool.NormalizeAllowedNodeIDs(ids)
			case "allowed_nodes":
				var text string
				if err := json.Unmarshal(value, &text); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "allowed_nodes must be a string"})
					return
				}
				patch.AllowedNodeIDsSet = true
				patch.AllowedNodeIDs = llmpool.ParseAllowedNodeList(text)
			case "capability_tags":
				tags, err := parseCapabilityTagsField(value)
				if err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				patch.CapabilityTagsSet = true
				patch.CapabilityTags = tags
			case "array_id":
				var text string
				if err := json.Unmarshal(value, &text); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "array_id must be a string"})
					return
				}
				patch.ArrayIDSet = true
				patch.ArrayID = text
			case "token_bank_tier":
				var text string
				if err := json.Unmarshal(value, &text); err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "token_bank_tier must be a string"})
					return
				}
				arrayID, err := llmservice.TokenBankArrayID(text)
				if err != nil {
					writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				patch.ArrayIDSet = true
				patch.ArrayID = arrayID
			default:
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "unknown field " + key})
				return
			}
		}
		if _, hasTier := raw["token_bank_tier"]; hasTier {
			if _, hasArray := raw["array_id"]; hasArray {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "send token_bank_tier or array_id, not both"})
				return
			}
			if !llmservice.IsTokenBankMemberID(id) {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "token_bank_tier applies to a token bank member"})
				return
			}
		}
		if tier, arrayID, ok := tokenBankTierPatchTarget(id, patch); ok {
			patch.ArrayID = arrayID
			if err := llmservice.WithTokenBankRouteLock(func() error {
				prevArray, revert, err := storeTokenBankMemberTier(r.Context(), bank, id, tier, arrayID)
				if err != nil {
					return err
				}
				if err := svc.ApplyTokenBankMemberTier(r.Context(), id, tier, prevArray); err != nil {
					if revert != nil {
						if revErr := revert(); revErr != nil {
							return tokenBankTierStoreError{err: fmt.Errorf("%v (tier revert: %v)", err, revErr)}
						}
					}
					return err
				}
				rest := patch
				rest.ArrayIDSet = false
				rest.ArrayID = ""
				if providerMemberPatchEmpty(rest) {
					return nil
				}
				return svc.PatchProviderMember(r.Context(), id, rest)
			}); err != nil {
				writeTokenBankTierPatchError(w, err)
				return
			}
		} else if err := svc.PatchProviderMember(r.Context(), id, patch); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		payload := map[string]any{"status": "ok", "id": id}
		if patch.Enabled != nil {
			payload["enabled"] = *patch.Enabled
		}
		if patch.AllowedNodeIDsSet {
			if patch.AllowedNodeIDs == nil {
				payload["allowed_node_ids"] = []string{}
			} else {
				payload["allowed_node_ids"] = patch.AllowedNodeIDs
			}
		}
		if patch.CapabilityTagsSet {
			if patch.CapabilityTags == nil {
				payload["capability_tags"] = []string{}
			} else {
				payload["capability_tags"] = patch.CapabilityTags
			}
		}
		if patch.ArrayIDSet {
			payload["array_id"] = strings.TrimSpace(patch.ArrayID)
			if tier, ok := llmservice.TokenBankTierOfArray(patch.ArrayID); ok {
				payload["token_bank_tier"] = tier
			}
		}
		writeLLMAdminJSON(w, http.StatusOK, payload)
	}
}

// tokenBankTierStoreError marks a failure while writing the share model row.
// Registry validation errors stay unwrapped so they keep the member-patch status.
type tokenBankTierStoreError struct{ err error }

func (e tokenBankTierStoreError) Error() string {
	if e.err == nil {
		return "token bank tier store failed"
	}
	return e.err.Error()
}

func (e tokenBankTierStoreError) Unwrap() error { return e.err }

func tokenBankTierPatchTarget(providerID string, patch llmservice.ProviderMemberPatch) (tier, arrayID string, ok bool) {
	if !patch.ArrayIDSet || !llmservice.IsTokenBankMemberID(providerID) {
		return "", "", false
	}
	tier, ok = llmservice.TokenBankTierOfArray(patch.ArrayID)
	if !ok {
		return "", "", false
	}
	arrayID, err := llmservice.TokenBankArrayID(tier)
	if err != nil {
		return "", "", false
	}
	return tier, arrayID, true
}

func providerMemberPatchEmpty(patch llmservice.ProviderMemberPatch) bool {
	return patch.Enabled == nil &&
		patch.DispatchWeight == nil &&
		patch.Priority == nil &&
		patch.RequestsPerMinute == nil &&
		patch.RequestsPerDay == nil &&
		patch.RateLimitCooldownSec == nil &&
		!patch.ModelMapSet &&
		!patch.AllowedNodeIDsSet &&
		!patch.CapabilityTagsSet &&
		!patch.ArrayIDSet
}

// storeTokenBankMemberTier writes the share model's tier, settlement rate, and
// array. A nil bank skips the row and leaves the registry update to the caller.
// The previous array id is returned even when the row already matches, so a
// route left behind by an earlier label can still follow it. The returned
// function restores the previous row when the registry update fails.
func storeTokenBankMemberTier(ctx context.Context, bank *SkillMarketHandlers, providerID, tier, arrayID string) (string, func() error, error) {
	repo := bank.tokenBankAdminRepo()
	if repo == nil {
		return "", func() error { return nil }, nil
	}
	shareID, model, ok := llmservice.ParseTokenBankMemberID(providerID)
	if !ok {
		return "", nil, fmt.Errorf("provider is not a token bank member")
	}
	models, err := repo.ListModels(ctx, shareID)
	if err != nil {
		return "", nil, tokenBankTierStoreError{err: err}
	}
	found := findStoredTokenBankModel(models, model)
	if found == nil {
		return "", nil, sqlite.ErrTokenBankShareNotFound
	}
	multiplier, err := llmservice.TokenBankTierMultiplier(tier)
	if err != nil {
		return "", nil, err
	}
	prev := *found
	prevArray := strings.TrimSpace(prev.ArrayID)
	if strings.EqualFold(strings.TrimSpace(prev.Tier), tier) &&
		prev.TierMultiplier == multiplier &&
		strings.EqualFold(prevArray, arrayID) {
		return prevArray, func() error { return nil }, nil
	}
	now := time.Now().UTC()
	if err := repo.SetModelTier(ctx, shareID, prev.ModelName, tier, multiplier, arrayID, now); err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
			return "", nil, err
		}
		return "", nil, tokenBankTierStoreError{err: err}
	}
	// The request context may already be canceled when the registry write
	// fails. The row still has to go back, or settlement and dispatch diverge.
	revertCtx := context.WithoutCancel(ctx)
	return prevArray, func() error {
		return repo.SetModelTier(revertCtx, shareID, prev.ModelName, prev.Tier, prev.TierMultiplier, prev.ArrayID, time.Now().UTC())
	}, nil
}

func findStoredTokenBankModel(models []sqlite.TokenBankShareModel, name string) *sqlite.TokenBankShareModel {
	name = strings.TrimSpace(name)
	var folded *sqlite.TokenBankShareModel
	for i := range models {
		got := strings.TrimSpace(models[i].ModelName)
		if got == name {
			return &models[i]
		}
		if folded == nil && strings.EqualFold(got, name) {
			folded = &models[i]
		}
	}
	return folded
}

func writeTokenBankTierPatchError(w http.ResponseWriter, err error) {
	if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
		writeJSONResp(w, http.StatusNotFound, map[string]string{"error": "share or model not found"})
		return
	}
	var storeErr tokenBankTierStoreError
	if errors.As(err, &storeErr) {
		writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeLLMProviderError(w, err)
}

func adminTestLLMProviderMember(svc *llmservice.Service, proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		spec, err := readProviderMemberTestRequest(w, r)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if svc == nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "llm service is required"})
			return
		}
		existing, err := svc.GetProvider(r.Context(), id)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if existing == nil {
			writeLLMProviderError(w, fmt.Errorf("%w: %s", llmservice.ErrProviderNotFound, id))
			return
		}
		node, err := resolveProviderTestNode(proxyCfg, existing, spec.Node)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		status := llmservice.MemberStatusView(*existing, time.Now())
		model := spec.Model
		if model == "" && len(existing.Models) > 0 {
			model = existing.Models[0]
		}
		model = llmservice.UpstreamModelForMember(existing, model)
		localNode := ""
		if proxyCfg != nil {
			localNode = strings.TrimSpace(proxyCfg.NodeID)
		}
		if strings.TrimSpace(existing.APIURL) == "" || model == "" {
			ran := node
			if ran == "" {
				ran = localNode
			}
			writeProviderMemberTest(w, providerMemberTestResult{member: status, errMsg: "api_url and model are required", model: model, node: ran, secret: existing.APIKey, toolsRequested: spec.ToolsSet})
			return
		}
		remote := node != "" && !strings.EqualFold(node, localNode)
		if node == "" && proxyCfg != nil && !llmpool.ProviderAllowedOnNode(*existing, proxyCfg.NodeID) {
			remote = true
		}
		if remote {
			ctx, cancel := context.WithTimeout(r.Context(), llmservice.MemberProbeContextTimeout(existing))
			defer cancel()
			probed := llmservice.TestProviderChat(ctx, proxyCfg, existing, llmservice.ProviderChatTest{
				Model:      model,
				Node:       node,
				Tools:      spec.Tools,
				ToolChoice: spec.ToolChoice,
			})
			writeProviderMemberTest(w, providerMemberTestResult{
				success: probed.Err == "", errMsg: probed.Err, reply: probed.Reply, model: probed.Model,
				node: firstNonEmpty(probed.Node, node), latencyMs: probed.LatencyMs, secret: existing.APIKey,
				toolCalls: probed.ToolCalls, toolsRequested: spec.ToolsSet,
			})
			return
		}
		cfg := corelib.MaclawLLMConfig{
			URL:        existing.APIURL,
			Key:        existing.APIKey,
			Model:      model,
			Protocol:   corelib.NormalizeLLMProviderProtocol(existing.Protocol),
			WireAPI:    corelib.NormalizeLLMProviderWireAPI(existing.WireAPI),
			TimeoutSec: existing.UpstreamTimeoutSec,
		}
		if wb, ok := llmservice.WorkBuddyMaclawConfig(existing, model); ok {
			wb.TimeoutSec = cfg.TimeoutSec
			cfg = wb
		}
		ctx, cancel := context.WithTimeout(r.Context(), llmProviderTestTimeout(cfg))
		defer cancel()
		reply, errMsg, latencyMs, calls := runLLMProviderChatTestBody(ctx, cfg, spec.Tools, spec.ToolChoice)
		ran := node
		if ran == "" {
			ran = localNode
		}
		writeProviderMemberTest(w, providerMemberTestResult{
			success: errMsg == "", errMsg: errMsg, reply: reply, model: model, node: ran,
			latencyMs: latencyMs, secret: existing.APIKey, toolCalls: calls, toolsRequested: spec.ToolsSet,
		})
	}
}

type providerMemberTestBody struct {
	Model      string
	Node       string
	Tools      json.RawMessage
	ToolChoice json.RawMessage
	ToolsSet   bool
}

func readProviderMemberTestRequest(w http.ResponseWriter, r *http.Request) (providerMemberTestBody, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return providerMemberTestBody{}, nil
		}
		return providerMemberTestBody{}, errors.New("invalid request body")
	}
	var spec providerMemberTestBody
	for key, value := range raw {
		switch key {
		case "model":
			var model string
			if err := json.Unmarshal(value, &model); err != nil {
				return providerMemberTestBody{}, errors.New("model must be a string")
			}
			spec.Model = strings.TrimSpace(model)
		case "node":
			var node string
			if err := json.Unmarshal(value, &node); err != nil {
				return providerMemberTestBody{}, errors.New("node must be a string")
			}
			spec.Node = strings.TrimSpace(node)
			if utf8.RuneCountInString(spec.Node) > 64 {
				return providerMemberTestBody{}, errors.New("node is too long")
			}
		case "tools":
			if err := validateProviderTestTools(value); err != nil {
				return providerMemberTestBody{}, err
			}
			spec.Tools = append(json.RawMessage(nil), value...)
			spec.ToolsSet = true
		case "tool_choice":
			if len(bytes.TrimSpace(value)) == 0 || string(bytes.TrimSpace(value)) == "null" {
				return providerMemberTestBody{}, errors.New("tool_choice must be a string or object")
			}
			if len(value) > 1024 {
				return providerMemberTestBody{}, errors.New("tool_choice is too large")
			}
			var asString string
			if err := json.Unmarshal(value, &asString); err != nil {
				var asObject map[string]json.RawMessage
				if err := json.Unmarshal(value, &asObject); err != nil || asObject == nil {
					return providerMemberTestBody{}, errors.New("tool_choice must be a string or object")
				}
			}
			spec.ToolChoice = append(json.RawMessage(nil), value...)
		default:
			return providerMemberTestBody{}, errors.New("unknown field " + key + "; this test uses the saved provider configuration")
		}
	}
	if len(spec.ToolChoice) > 0 && !spec.ToolsSet {
		return providerMemberTestBody{}, errors.New("tool_choice requires tools")
	}
	return spec, nil
}

func validateProviderTestTools(raw json.RawMessage) error {
	if len(raw) > 32<<10 {
		return errors.New("tools is too large")
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return errors.New("tools must be an array")
	}
	if len(tools) == 0 {
		return errors.New("tools must include at least one tool")
	}
	if len(tools) > 8 {
		return errors.New("tools is limited to 8 entries")
	}
	for _, tool := range tools {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(tool, &obj); err != nil || obj == nil {
			return errors.New("each tool must be an object")
		}
	}
	return nil
}

func resolveProviderTestNode(proxyCfg *llmservice.ProxyConfig, provider *llmpool.ProviderConfig, node string) (string, error) {
	node = strings.TrimSpace(node)
	if node == "" || provider == nil {
		return "", nil
	}
	canonical := ""
	if proxyCfg != nil && proxyCfg.ListAccessNodes != nil {
		for _, item := range proxyCfg.ListAccessNodes() {
			if strings.EqualFold(strings.TrimSpace(item.NodeID), node) {
				canonical = strings.TrimSpace(item.NodeID)
				break
			}
		}
	}
	if canonical == "" && proxyCfg != nil && strings.EqualFold(strings.TrimSpace(proxyCfg.NodeID), node) {
		canonical = strings.TrimSpace(proxyCfg.NodeID)
	}
	if canonical == "" {
		return "", errors.New("unknown node " + node)
	}
	if !llmpool.ProviderAllowedOnNode(*provider, canonical) {
		return "", errors.New("node is outside the member access scope")
	}
	return canonical, nil
}

type providerMemberTestResult struct {
	member         llmservice.ProviderArrayMemberView
	success        bool
	errMsg         string
	reply          string
	model          string
	node           string
	latencyMs      int64
	secret         string
	toolCalls      []llmservice.ProviderToolCall
	toolsRequested bool
}

func writeProviderMemberTest(w http.ResponseWriter, result providerMemberTestResult) {
	payload := map[string]any{
		"success":    result.success,
		"latency_ms": result.latencyMs,
		"model":      result.model,
		"member":     result.member,
	}
	if strings.TrimSpace(result.node) != "" {
		payload["node"] = result.node
	}
	if result.errMsg != "" {
		payload["error"] = sanitizeProviderTestError(result.errMsg, result.secret)
	}
	if result.reply != "" {
		payload["reply"] = sanitizeProviderTestReply(result.reply, result.secret)
	}
	if result.toolsRequested {
		calls := result.toolCalls
		if calls == nil {
			calls = []llmservice.ProviderToolCall{}
		}
		for i := range calls {
			calls[i].Name = sanitizeProviderTestReply(calls[i].Name, result.secret)
			calls[i].Arguments = sanitizeProviderTestReply(calls[i].Arguments, result.secret)
		}
		payload["tool_calls"] = calls
	}
	writeLLMAdminJSON(w, http.StatusOK, payload)
}

func sanitizeProviderTestError(msg, secret string) string {
	return llmservice.RedactStatusError(llmservice.RedactConfiguredSecret(msg, secret))
}

func sanitizeProviderTestReply(reply, secret string) string {
	return llmservice.RedactStatusText(llmservice.RedactConfiguredSecret(reply, secret))
}

func foldCapabilityTagStrings(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload any
	if err := dec.Decode(&payload); err != nil {
		return nil, errors.New("invalid request body")
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid request body")
	}
	if err := foldCapabilityTagValue(payload); err != nil {
		return nil, err
	}
	if err := rejectBatchPauseFields(payload); err != nil {
		return nil, err
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("invalid request body")
	}
	return out, nil
}

func foldCapabilityTagValue(value any) error {
	switch node := value.(type) {
	case map[string]any:
		if raw, ok := node["capability_tags"]; ok {
			switch tags := raw.(type) {
			case string:
				parsed, err := llmservice.ParseCapabilityTagList(tags)
				if err != nil {
					return err
				}
				if parsed == nil {
					node["capability_tags"] = []any{}
				} else {
					items := make([]any, len(parsed))
					for i, tag := range parsed {
						items[i] = tag
					}
					node["capability_tags"] = items
				}
			case nil:
			case []any:
				for _, item := range tags {
					if _, ok := item.(string); !ok {
						return errors.New("capability_tags must be an array or a comma-separated string")
					}
				}
			default:
				return errors.New("capability_tags must be an array or a comma-separated string")
			}
		}
		for _, child := range node {
			if err := foldCapabilityTagValue(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := foldCapabilityTagValue(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectBatchPauseFields(payload any) error {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if pauseField(root) {
		return errors.New("pause and resume use PATCH enabled")
	}
	for _, arrays := range matchingSlices(root, "arrays") {
		for i, item := range arrays {
			arr, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if pauseField(arr) {
				return fmt.Errorf("arrays[%d] pause and resume use PATCH enabled", i)
			}
			for _, providers := range matchingSlices(arr, "providers") {
				for _, raw := range providers {
					provider, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if pauseField(provider) {
						return fmt.Errorf("arrays[%d] pause and resume use PATCH enabled", i)
					}
				}
			}
		}
	}
	return nil
}

func pauseField(node map[string]any) bool {
	for key := range node {
		if strings.EqualFold(key, "paused") || strings.EqualFold(key, "enabled") {
			return true
		}
	}
	return false
}

func matchingSlices(node map[string]any, key string) [][]any {
	var out [][]any
	for name, value := range node {
		if !strings.EqualFold(name, key) {
			continue
		}
		if items, ok := value.([]any); ok {
			out = append(out, items)
		}
	}
	return out
}

func parseCapabilityTagsField(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("capability_tags must be an array or a comma-separated string")
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, errors.New("capability_tags must be an array or a comma-separated string")
		}
		return llmservice.ParseCapabilityTagList(text)
	}
	var tags []string
	if err := json.Unmarshal(raw, &tags); err != nil {
		return nil, errors.New("capability_tags must be an array or a comma-separated string")
	}
	return llmservice.NormalizeCapabilityTags(tags)
}

func jsonWholeInt(raw json.RawMessage) (int, error) {
	var num json.Number
	if err := json.Unmarshal(raw, &num); err != nil {
		return 0, err
	}
	i64, err := num.Int64()
	if err != nil {
		f, ferr := num.Float64()
		if ferr != nil || f != math.Trunc(f) || f > float64(math.MaxInt) || f < float64(math.MinInt) {
			return 0, errors.New("must be an integer")
		}
		i64 = int64(f)
	}
	if int64(int(i64)) != i64 {
		return 0, errors.New("must be an integer")
	}
	return int(i64), nil
}

func parseAdminAPIKeyExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, errors.New("expires_at must be RFC3339 or YYYY-MM-DD")
	}
	loc, locErr := time.LoadLocation("Asia/Shanghai")
	if locErr != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, loc).UTC(), nil
}

func adminListMemberHealth(proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		day := strings.TrimSpace(r.URL.Query().Get("day"))
		if day != "" {
			if _, err := time.Parse("2006-01-02", day); err != nil {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "day must be YYYY-MM-DD"})
				return
			}
		}
		providerID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider_id")))
		var nodes []llmservice.AccessNodeView
		if proxyCfg != nil && proxyCfg.ListAccessNodes != nil {
			nodes = proxyCfg.ListAccessNodes()
		}
		report, err := llmservice.ListMemberHealth(r.Context(), day, nodes)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if providerID != "" {
			for i := range report.Nodes {
				filtered := make([]llmservice.MemberHealthMember, 0, len(report.Nodes[i].Members))
				for _, member := range report.Nodes[i].Members {
					if member.ID == providerID {
						filtered = append(filtered, member)
					}
				}
				report.Nodes[i].Members = filtered
			}
		}
		writeLLMAdminJSON(w, http.StatusOK, report)
	}
}

func writeLLMAdminJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSONResp(w, status, payload)
}

func llmAdminHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	if strings.Contains(msg, "llm registry") || strings.Contains(msg, "admin api keys") || strings.Contains(msg, "llm service is required") {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func llmAdminAPIMarkdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(llmAdminAPIDocMarkdown))
}

func llmAdminAPIOpenAPIDoc(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, http.StatusOK, llmAdminAPIOpenAPI())
}
