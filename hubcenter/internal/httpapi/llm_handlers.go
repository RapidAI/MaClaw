package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

var llmProviderProbeHTTPClient = &http.Client{Timeout: 20 * time.Second}

// ---------------------------------------------------------------------------
// LLM Provider admin handlers
// ---------------------------------------------------------------------------

func writeLLMProviderError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, llmservice.ErrProviderNotFound) {
		code = http.StatusNotFound
	} else if errors.Is(err, llmservice.ErrProviderInUse) {
		code = http.StatusConflict
	}
	writeJSONResp(w, code, map[string]string{"error": err.Error()})
}

func queryTimezone(r *http.Request) string {
	if r == nil {
		return ""
	}
	timezone := strings.TrimSpace(r.URL.Query().Get("timezone"))
	if len(timezone) > 64 {
		return ""
	}
	return timezone
}

// sameAuthKind reports whether two auth kinds name the same account family.
// A blank kind on the incoming side means the caller is not touching the
// login, so the stored kind is the one that must match. Empty and "api_key"
// are the same static-key family: the editor sends "api_key" while a saved
// row stores an empty kind, and a blank key field must keep the stored key.
func sameAuthKind(existingKind, incomingKind string) bool {
	existingKind = strings.TrimSpace(existingKind)
	incomingKind = strings.TrimSpace(incomingKind)
	if incomingKind == "" {
		incomingKind = existingKind
	}
	return staticAuthKind(existingKind) == staticAuthKind(incomingKind)
}

func staticAuthKind(kind string) string {
	if strings.TrimSpace(kind) == llmpool.ProviderAuthAPIKey {
		return ""
	}
	return strings.TrimSpace(kind)
}

// forgetVendorLogin drops the in-memory logins a saved provider consumed. A
// session whose credential was never applied stays pending: the admin may
// still be holding its browser tab open.
func forgetVendorLogin(svc *llmservice.Service, sessionIDs ...string) {
	if svc == nil {
		return
	}
	if len(sessionIDs) == 0 {
		return
	}
	workBuddyID := strings.TrimSpace(sessionIDs[0])
	if workBuddyID != "" {
		svc.ForgetWorkBuddyLogin(workBuddyID)
	}
	for _, id := range sessionIDs[1:] {
		if id == "" {
			continue
		}
		svc.ForgetQoderLogin(id)
		svc.ForgetTraeLogin(id)
		svc.ForgetLobsterAILogin(id)
	}
}

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
			HasAPIKey         bool    `json:"has_api_key"`
			APIKey            string  `json:"api_key,omitempty"` // always empty in response
			CurrentMultiplier float64 `json:"current_multiplier"`
			LBGroup           string  `json:"lb_group,omitempty"`
			LBGroupSize       int     `json:"lb_group_size,omitempty"`
			LBEligible        bool    `json:"lb_eligible,omitempty"`
		}
		now := time.Now()
		inputs := make([]llmpool.ProviderLBInput, len(reg.Providers))
		for i, p := range reg.Providers {
			inputs[i] = llmpool.ProviderLBInputFromConfig(p)
		}
		annotations := llmpool.AnnotateProviderLBGroups(inputs, now)
		safe := make([]safeProvider, len(reg.Providers))
		for i, p := range reg.Providers {
			ann := llmpool.ProviderLBAnnotation{}
			if i < len(annotations) {
				ann = annotations[i]
			}
			safe[i] = safeProvider{
				ProviderConfig:    p,
				HasAPIKey:         p.APIKey != "",
				CurrentMultiplier: ann.CurrentMultiplier,
				LBGroup:           ann.LBGroup,
				LBGroupSize:       ann.LBGroupSize,
				LBEligible:        ann.LBEligible,
			}
			safe[i].ProviderConfig.APIKey = "" // redact
			safe[i].WorkBuddyRefreshToken = ""
			safe[i].WorkBuddyUserID = ""
			safe[i].WorkBuddyEnterpriseID = ""
			safe[i].WorkBuddyDomain = ""
			safe[i].WorkBuddySessionID = ""
			safe[i].QoderRefreshToken = ""
			safe[i].QoderUserID = ""
			safe[i].QoderMachineID = ""
			safe[i].QoderSessionID = ""
			safe[i].TraeRefreshToken = ""
			safe[i].TraeUserID = ""
			safe[i].TraeMachineID = ""
			safe[i].TraeDeviceID = ""
			safe[i].TraeSessionID = ""
			safe[i].LobsterAIRefreshToken = ""
			safe[i].LobsterAIUserID = ""
			safe[i].LobsterAISessionUUID = ""
			safe[i].LobsterAIFirstKeyfrom = ""
			safe[i].LobsterAISessionID = ""
		}
		sort.SliceStable(safe, func(i, j int) bool {
			si := llmpool.EffectiveProviderSequence(safe[i].Sequence)
			sj := llmpool.EffectiveProviderSequence(safe[j].Sequence)
			if si != sj {
				return si < sj
			}
			return strings.ToLower(strings.TrimSpace(safe[i].ID)) < strings.ToLower(strings.TrimSpace(safe[j].ID))
		})
		writeJSONResp(w, http.StatusOK, map[string]any{
			"providers":       safe,
			"provider_arrays": reg.ProviderArrays,
		})
	}
}

func adminListLLMAccessNodes(proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var nodes []llmservice.AccessNodeView
		if proxyCfg != nil && proxyCfg.ListAccessNodes != nil {
			nodes = proxyCfg.ListAccessNodes()
		}
		if len(nodes) == 0 {
			nodeID := ""
			if proxyCfg != nil {
				nodeID = proxyCfg.NodeID
			}
			nodes = llmservice.DefaultAccessNodes(nodeID)
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

func adminAddLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var provider llmpool.ProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		provider.ID = strings.TrimSpace(provider.ID)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.APIURL = strings.TrimSpace(provider.APIURL)
		sessionID := strings.TrimSpace(provider.WorkBuddySessionID)
		qoderSessionID := strings.TrimSpace(provider.QoderSessionID)
		traeSessionID := strings.TrimSpace(provider.TraeSessionID)
		lobsterAISessionID := strings.TrimSpace(provider.LobsterAISessionID)
		if err := svc.ApplyWorkBuddyLogin(&provider, true); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyQoderLogin(&provider, true); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyTraeLogin(&provider, true); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyLobsterAILogin(&provider, true); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		provider.ID = strings.TrimSpace(provider.ID)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.APIURL = strings.TrimSpace(provider.APIURL)
		if provider.ID == "" || provider.Name == "" || provider.APIURL == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "id, name, and api_url are required"})
			return
		}
		if err := svc.AddProvider(r.Context(), provider); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		forgetVendorLogin(svc, sessionID, qoderSessionID, traeSessionID, lobsterAISessionID)
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
		sessionID := strings.TrimSpace(provider.WorkBuddySessionID)
		qoderSessionID := strings.TrimSpace(provider.QoderSessionID)
		traeSessionID := strings.TrimSpace(provider.TraeSessionID)
		lobsterAISessionID := strings.TrimSpace(provider.LobsterAISessionID)
		if err := svc.ApplyWorkBuddyLogin(&provider, false); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyQoderLogin(&provider, false); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyTraeLogin(&provider, false); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.ApplyLobsterAILogin(&provider, false); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if provider.APIKey == "" {
			existing, err := svc.GetProvider(r.Context(), id)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			// Only the same auth kind may borrow the stored access token. A row
			// switched to another vendor has a token for that vendor's upstream,
			// and reusing it would fail every request against the new one.
			if existing != nil && sameAuthKind(existing.AuthKind, provider.AuthKind) {
				provider.APIKey = existing.APIKey
			}
		}
		if err := svc.UpdateProvider(r.Context(), provider); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		forgetVendorLogin(svc, sessionID, qoderSessionID, traeSessionID, lobsterAISessionID)
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminSetLLMProviderPaused(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		var req struct {
			Paused *bool `json:"paused"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Paused == nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "paused is required"})
			return
		}
		if err := svc.SetProviderPaused(r.Context(), id, *req.Paused); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"status": "ok", "paused": *req.Paused})
	}
}

func adminCopyLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		copied, err := svc.CopyProvider(r.Context(), id)
		if err != nil {
			writeLLMProviderError(w, err)
			return
		}
		if copied == nil || strings.TrimSpace(copied.ID) == "" {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "provider was not copied"})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"id":       copied.ID,
			"name":     copied.Name,
			"array_id": copied.ArrayID,
		})
	}
}

func adminSetLLMProviderSequence(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		var req struct {
			Sequence *int `json:"sequence"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sequence == nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "sequence is required"})
			return
		}
		if err := svc.SetProviderSequence(r.Context(), id, *req.Sequence); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"status": "ok", "sequence": *req.Sequence})
	}
}

func adminSetLLMProviderSequences(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Sequences map[string]int `json:"sequences"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Sequences) == 0 {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "sequences is required"})
			return
		}
		if err := svc.SetProviderSequences(r.Context(), req.Sequences); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"status": "ok", "sequences": req.Sequences})
	}
}

func adminProbeLLMProviderModels(svc *llmservice.Service, proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID string `json:"provider_id"`
			APIURL     string `json:"api_url"`
			APIKey     string `json:"api_key"`
			Protocol   string `json:"protocol"`
			AgentType  string `json:"agent_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		var existing *llmpool.ProviderConfig
		if req.ProviderID != "" && svc != nil {
			got, err := svc.GetProvider(r.Context(), req.ProviderID)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			existing = got
		}
		var (
			models []string
			err    error
		)
		agentType := strings.TrimSpace(req.AgentType)
		if agentType == "" && existing != nil {
			agentType = existing.AgentType
		}
		// A saved provider's key must not be sent to a caller-supplied URL.
		if existing != nil && strings.TrimSpace(req.APIKey) == "" {
			probeProvider := *existing
			if agentType != "" {
				probeProvider.AgentType = agentType
			}
			if proxyCfg != nil && !llmpool.ProviderAllowedOnNode(probeProvider, proxyCfg.NodeID) {
				models, err = llmservice.ProbeProviderModelsWithScope(r.Context(), proxyCfg, &probeProvider, probeProvider.APIURL, probeProvider.APIKey, probeProvider.Protocol)
			} else {
				models, err = probeLLMProviderModels(r.Context(), probeProvider.Name, probeProvider.APIURL, probeProvider.APIKey, probeProvider.Protocol, probeProvider.AgentType)
			}
		} else {
			models, err = probeLLMProviderModels(r.Context(), "", req.APIURL, req.APIKey, req.Protocol, agentType)
		}
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"models": models})
	}
}

func probeLLMProviderModels(ctx context.Context, providerName, apiURL, apiKey, protocol, agentType string) ([]string, error) {
	return llmservice.ProbeProviderModels(ctx, llmProviderProbeHTTPClient, providerName, apiURL, apiKey, protocol, agentType)
}

// openCodeCatalogBase lists Zen models for an OpenCode provider. A Go gateway
// URL does not publish the free catalog, and an empty key is still allowed:
// that is the anonymous free tier. A paid key is sent when the admin saved one.
func openCodeCatalogBase(apiURL, agentType string) string {
	return llmservice.OpenCodeCatalogURL(apiURL, agentType)
}

// adminTestLLMProviderChat performs an end-to-end chat completion test against
// a configured provider. Unlike probe-models (which only checks /v1/models
// reachability), this sends an actual chat request and verifies a response is
// returned — the same test the client would exercise.
func adminTestLLMProviderChat(svc *llmservice.Service, proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID   string `json:"provider_id"`
			APIURL       string `json:"api_url"`
			APIKey       string `json:"api_key"`
			Model        string `json:"model"`
			RouteModel   string `json:"route_model"`
			LogicalModel string `json:"logical_model"`
			Protocol     string `json:"protocol"`
			WireAPI      string `json:"wire_api"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		// A saved provider ID is authoritative for connection data.  In
		// particular, never combine its stored key with a caller-supplied URL:
		// that would allow an administrator's browser request to send a provider
		// secret to a different endpoint. The model remains selectable so a
		// service-group route can test its configured upstream model. A route
		// id may be an array id, and auto / official-* are billing bands, not
		// upstream model ids. Both are resolved to the member production dials.
		timeoutSec := 0
		var existing *llmpool.ProviderConfig
		if providerID := strings.TrimSpace(req.ProviderID); providerID != "" {
			if svc == nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "provider service unavailable"})
				return
			}
			got, model, err := svc.ProviderChatTestTarget(r.Context(), providerID, req.RouteModel, req.LogicalModel, req.Model)
			if errors.Is(err, llmservice.ErrNoUpstreamTestModel) {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
				return
			}
			if err != nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "load provider: " + err.Error()})
				return
			}
			existing = got
			if existing == nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "provider not found"})
				return
			}
			req.APIKey = existing.APIKey
			req.APIURL = existing.APIURL
			req.Protocol = existing.Protocol
			req.WireAPI = existing.WireAPI
			req.Model = model
			timeoutSec = existing.UpstreamTimeoutSec
			if proxyCfg != nil && !llmpool.ProviderAllowedOnNode(*existing, proxyCfg.NodeID) {
				ctx, cancel := context.WithTimeout(r.Context(), llmProviderTestTimeout(corelib.MaclawLLMConfig{TimeoutSec: timeoutSec}))
				defer cancel()
				reply, errMsg, latencyMs, model := llmservice.TestProviderChatWithScope(ctx, proxyCfg, existing, req.Model)
				if errMsg != "" {
					writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": errMsg, "latency_ms": latencyMs})
					return
				}
				writeJSONResp(w, http.StatusOK, map[string]any{"success": true, "reply": reply, "model": model, "latency_ms": latencyMs})
				return
			}
		}
		if req.APIURL == "" || req.Model == "" {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success": false,
				"error":   "api_url and model are required",
			})
			return
		}
		// Use the provider's configured protocol and wire API, so the test covers
		// the same upstream request shape as production routing.
		cfg := corelib.MaclawLLMConfig{
			URL:        req.APIURL,
			Key:        req.APIKey,
			Model:      req.Model,
			Protocol:   corelib.NormalizeLLMProviderProtocol(req.Protocol),
			WireAPI:    corelib.NormalizeLLMProviderWireAPI(req.WireAPI),
			TimeoutSec: timeoutSec,
		}
		cfg = providerTestConfig(cfg, existing)
		// An availability test must use the provider's production timeout. Slower
		// reasoning providers can legitimately take more than 15 seconds before
		// returning their first completion.
		ctx, cancel := context.WithTimeout(r.Context(), llmProviderTestTimeout(cfg))
		defer cancel()
		reply, errMsg, latencyMs, _ := runLLMProviderChatTest(ctx, cfg)
		if errMsg != "" {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      errMsg,
				"latency_ms": latencyMs,
			})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{
			"success":    true,
			"reply":      reply,
			"model":      req.Model,
			"latency_ms": latencyMs,
		})
	}
}

// testLLMProviderChatStatus runs the same availability test as the admin
// "Test Status" button against a saved provider. It is used by the background
// provider monitor; the model is the provider's first configured model, which
// mirrors the model the admin UI picks for the manual test.
func testLLMProviderChatStatus(ctx context.Context, svc *llmservice.Service, proxyCfg *llmservice.ProxyConfig, providerID string) (success bool, errMsg string, latencyMs int64) {
	if svc == nil {
		return false, "provider service unavailable", 0
	}
	existing, err := svc.GetProvider(ctx, providerID)
	if err != nil {
		return false, "load provider: " + err.Error(), 0
	}
	if existing == nil {
		return false, "provider not found", 0
	}
	model := ""
	if len(existing.Models) > 0 {
		model = existing.Models[0]
	}
	if strings.TrimSpace(existing.APIURL) == "" || model == "" {
		return false, "api_url and model are required", 0
	}
	if proxyCfg != nil && !llmpool.ProviderAllowedOnNode(*existing, proxyCfg.NodeID) {
		_, errMsg, latencyMs, _ := llmservice.TestProviderChatWithScope(ctx, proxyCfg, existing, model)
		return errMsg == "", errMsg, latencyMs
	}
	cfg := corelib.MaclawLLMConfig{
		URL:        existing.APIURL,
		Key:        existing.APIKey,
		Model:      model,
		Protocol:   corelib.NormalizeLLMProviderProtocol(existing.Protocol),
		WireAPI:    corelib.NormalizeLLMProviderWireAPI(existing.WireAPI),
		TimeoutSec: existing.UpstreamTimeoutSec,
	}
	cfg = providerTestConfig(cfg, existing)
	testCtx, cancel := context.WithTimeout(ctx, llmProviderTestTimeout(cfg))
	defer cancel()
	_, errMsg, latencyMs, _ = runLLMProviderChatTest(testCtx, cfg)
	return errMsg == "", errMsg, latencyMs
}

// providerTestConfig is the vendor overlay shared with a remote stream probe.
func providerTestConfig(cfg corelib.MaclawLLMConfig, provider *llmpool.ProviderConfig) corelib.MaclawLLMConfig {
	return llmservice.MemberProbeConfig(cfg, provider)
}

// runLLMProviderChatTest executes one availability probe against cfg and
// returns the model reply. A non-empty errMsg marks the provider unavailable.

// providerTestBudget is the optional output the member probe asked to send.
// Nil pointers keep the short probe default. OutputCap is the saved member
// limit and is applied before the probe leaves this process. Stream uses the
// protocol's streaming wire and still returns one JSON receipt.
type providerTestBudget struct {
	MaxTokens           *int64
	MaxCompletionTokens *int64
	OutputCap           int64
	Stream              bool
}

func (b providerTestBudget) member() llmservice.MemberProbeBudget {
	return llmservice.MemberProbeBudget{
		MaxTokens:           b.MaxTokens,
		MaxCompletionTokens: b.MaxCompletionTokens,
		OutputCap:           b.OutputCap,
		Stream:              b.Stream,
	}
}

// providerProbeResult is one member probe. output is the budgets on the
// request that was built. streamEnd is set only when the caller asked for a stream.
type providerProbeResult struct {
	reply     string
	errMsg    string
	latencyMs int64
	calls     []llmservice.ProviderToolCall
	output    map[string]llmservice.ProbeOutputField
	stream    bool
	streamEnd *bool
}

func runLLMProviderChatTest(ctx context.Context, cfg corelib.MaclawLLMConfig) (reply string, errMsg string, latencyMs int64, calls []llmservice.ProviderToolCall) {
	// The availability check is one real upstream call. The member cap is
	// already on cfg. An empty budget used to hide it from the OpenCode probe,
	// which then sent no output limit at all. The status check stays non-stream.
	got := runLLMProviderChatTestBody(ctx, cfg, nil, nil, providerTestBudget{
		OutputCap: cfg.MemberOutputTokenCap,
	})
	return got.reply, got.errMsg, got.latencyMs, got.calls
}

func runLLMProviderChatTestBody(ctx context.Context, cfg corelib.MaclawLLMConfig, tools, toolChoice json.RawMessage, budget providerTestBudget) providerProbeResult {
	if corelib.IsOpenCodeURL(cfg.URL) {
		return runOpenCodeProviderChatTest(ctx, cfg, tools, toolChoice, budget)
	}
	httpReq, wire, err := llmservice.NewMemberProbeRequest(ctx, cfg, tools, toolChoice, budget.member())
	if err != nil {
		return probeFailed(budget, err.Error(), 0, nil, "", nil)
	}
	output := projectProbeOutput(cfg, budget, tools, wire)
	start := time.Now()
	client := llmservice.WrapMemberProbeClient(corelib.NewLLMEndpointHTTPClient(cfg), cfg)
	resp, err := client.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return probeFailed(budget, llmProviderTestRequestError(err, cfg), latencyMs, output, "", nil)
	}
	defer resp.Body.Close()
	respBody, truncated, err := readLLMProviderTestResponse(resp.Body)
	if err != nil {
		return probeFailed(budget, llmProviderTestResponseReadError(err, cfg), latencyMs, output, "", nil)
	}
	if truncated {
		return probeFailed(budget, "model response exceeds the 64 KiB availability-test limit", latencyMs, output, "", nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeFailed(budget, llmProviderTestHTTPError(resp.StatusCode, respBody), latencyMs, output, "", nil)
	}
	if budget.Stream {
		return finishProbeStream(budget, latencyMs, output, llmservice.ParseProbeStream(respBody, llmservice.ProbeOutputKind(cfg)))
	}
	if errMsg := providerChatTestEnvelopeError(respBody); errMsg != "" {
		return probeFailed(budget, errMsg, latencyMs, output, "", nil)
	}

	// Parse the actual completion. A model is available only after it returns
	// non-empty content to the short test prompt, or a tool call when tools were sent.
	reply, err := llmProviderTestReply(respBody, cfg.Protocol, cfg.WireAPI)
	if err != nil {
		return probeFailed(budget, "invalid model response: "+err.Error(), latencyMs, output, "", nil)
	}
	calls := llmservice.ParseProviderTestToolCalls(respBody)
	if strings.TrimSpace(reply) == "" && len(calls) == 0 {
		return probeFailed(budget, "model returned no completion content", latencyMs, output, reply, calls)
	}
	return probeOK(budget, reply, latencyMs, output, calls, nil)
}

// runOpenCodeProviderChatTest uses the proxy forwarder. Zen free models
// answer with SSE, and one of them only accepts the Responses API; the
// forwarder collects either shape into one chat JSON body.
func runOpenCodeProviderChatTest(ctx context.Context, cfg corelib.MaclawLLMConfig, tools, toolChoice json.RawMessage, budget providerTestBudget) providerProbeResult {
	prompt := "Reply with exactly: pong"
	toolsSet := len(bytes.TrimSpace(tools)) > 0 && string(bytes.TrimSpace(tools)) != "null"
	if toolsSet {
		prompt = "Call one of the available tools now."
	}
	body := map[string]any{
		"model":    cfg.Model,
		"messages": []any{map[string]any{"role": "user", "content": prompt}},
	}
	// The cap is whichever of the probe budget and the request config is set.
	// The availability check puts it only on the config. Treating that as
	// "no cap" sent the OpenCode probe with no output limit.
	outputCap := budget.OutputCap
	if outputCap <= 0 {
		outputCap = cfg.MemberOutputTokenCap
	}
	// An omitted budget with no member cap used to leave max_tokens off the
	// OpenCode body. Inject the short probe budget only when the caller set
	// one, or when a positive cap has to bound that default.
	if budget.MaxTokens != nil || budget.MaxCompletionTokens != nil || outputCap > 0 {
		for key, value := range llmservice.MemberProbeOutputBody(outputCap, budget.MaxTokens, budget.MaxCompletionTokens, toolsSet) {
			body[key] = value
		}
	}
	if parsed, choice := providerTestToolValues(tools, toolChoice); len(parsed) > 0 {
		body["tools"] = parsed
		if choice != nil {
			body["tool_choice"] = choice
		}
	}
	provider := &llmpool.ProviderConfig{
		APIURL:                    cfg.URL,
		APIKey:                    cfg.Key,
		Protocol:                  cfg.Protocol,
		WireAPI:                   cfg.WireAPI,
		AgentType:                 cfg.AgentType,
		UpstreamTimeoutSec:        cfg.TimeoutSec,
		ExtraBody:                 cfg.MemberExtraBody,
		ExtraBodyPriority:         cfg.MemberExtraBodyPriority,
		DropFields:                append([]string(nil), cfg.MemberDropFields...),
		MaxOutputTokensPerRequest: outputCap,
	}
	wire, endpoint, kind, wireErr := openCodeProbeWire(ctx, cfg, provider, body, budget.Stream)
	var output map[string]llmservice.ProbeOutputField
	if wireErr == nil {
		output = projectProbeOutputKind(cfg, kind, budget, tools, wire)
	}
	if budget.Stream {
		if wireErr != nil {
			return probeFailed(budget, wireErr.Error(), 0, output, "", nil)
		}
		return runOpenCodeStreamProbe(ctx, cfg, endpoint, wire, kind, budget, output)
	}
	start := time.Now()
	status, respBody, err := llmservice.ForwardProviderCompletion(ctx, corelib.NewLLMEndpointHTTPClient(cfg), provider, body, cfg.Model)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return probeFailed(budget, llmProviderTestRequestError(err, cfg), latencyMs, output, "", nil)
	}
	if status < 200 || status >= 300 {
		return probeFailed(budget, llmProviderTestHTTPError(status, respBody), latencyMs, output, "", nil)
	}
	if len(respBody) > 64<<10 {
		return probeFailed(budget, "model response exceeds the 64 KiB availability-test limit", latencyMs, output, "", nil)
	}
	if errMsg := providerChatTestEnvelopeError(respBody); errMsg != "" {
		return probeFailed(budget, errMsg, latencyMs, output, "", nil)
	}
	reply, err := llmProviderTestReply(respBody, "openai", "chat")
	if err != nil {
		return probeFailed(budget, "invalid model response: "+err.Error(), latencyMs, output, "", nil)
	}
	calls := llmservice.ParseProviderTestToolCalls(respBody)
	if strings.TrimSpace(reply) == "" && len(calls) == 0 {
		return probeFailed(budget, "model returned no completion content", latencyMs, output, reply, calls)
	}
	return probeOK(budget, reply, latencyMs, output, calls, nil)
}

func newLLMProviderTestRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, tools, toolChoice json.RawMessage, budget providerTestBudget) (*http.Request, error) {
	req, _, err := llmservice.NewMemberProbeRequest(ctx, cfg, tools, toolChoice, budget.member())
	return req, err
}

func projectProbeOutput(cfg corelib.MaclawLLMConfig, budget providerTestBudget, tools json.RawMessage, wire []byte) map[string]llmservice.ProbeOutputField {
	return projectProbeOutputKind(cfg, llmservice.ProbeOutputKind(cfg), budget, tools, wire)
}

func projectProbeOutputKind(cfg corelib.MaclawLLMConfig, kind string, budget providerTestBudget, tools json.RawMessage, wire []byte) map[string]llmservice.ProbeOutputField {
	toolsSet := len(bytes.TrimSpace(tools)) > 0 && string(bytes.TrimSpace(tools)) != "null"
	baseline := llmservice.ProbeOutputBaseline(kind, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields, budget.MaxTokens, budget.MaxCompletionTokens, toolsSet)
	return llmservice.ProjectProbeOutput(wire, baseline)
}

func probeFailed(budget providerTestBudget, errMsg string, latencyMs int64, output map[string]llmservice.ProbeOutputField, reply string, calls []llmservice.ProviderToolCall) providerProbeResult {
	result := providerProbeResult{reply: reply, errMsg: errMsg, latencyMs: latencyMs, calls: calls, output: output}
	if budget.Stream {
		result.stream = true
		end := false
		result.streamEnd = &end
	}
	return result
}

func probeOK(budget providerTestBudget, reply string, latencyMs int64, output map[string]llmservice.ProbeOutputField, calls []llmservice.ProviderToolCall, streamEnd *bool) providerProbeResult {
	result := providerProbeResult{reply: reply, latencyMs: latencyMs, calls: calls, output: output}
	if budget.Stream {
		result.stream = true
		if streamEnd == nil {
			end := false
			streamEnd = &end
		}
		result.streamEnd = streamEnd
	}
	return result
}

func finishProbeStream(budget providerTestBudget, latencyMs int64, output map[string]llmservice.ProbeOutputField, parsed llmservice.ProbeStream) providerProbeResult {
	end := parsed.End
	if errMsg := parsed.Failure(); errMsg != "" {
		result := probeFailed(budget, errMsg, latencyMs, output, parsed.Reply, parsed.Calls)
		result.streamEnd = &end
		return result
	}
	return probeOK(budget, parsed.Reply, latencyMs, output, parsed.Calls, &end)
}

func openCodeProbeWire(ctx context.Context, cfg corelib.MaclawLLMConfig, provider *llmpool.ProviderConfig, body map[string]any, stream bool) (wire []byte, endpoint, kind string, err error) {
	chat := make(map[string]any, len(body)+1)
	for key, value := range body {
		chat[key] = value
	}
	if stream {
		chat["stream"] = true
	}
	plan, planned, err := llmservice.PlanOpenCodeProbe(provider, chat, cfg.Model)
	if err != nil {
		return nil, "", "", err
	}
	if planned && plan.Responses {
		messages, _ := chat["messages"].([]any)
		var tools []map[string]interface{}
		switch raw := chat["tools"].(type) {
		case []map[string]interface{}:
			tools = raw
		case []any:
			for _, item := range raw {
				if tool, ok := item.(map[string]interface{}); ok {
					tools = append(tools, tool)
				}
			}
		}
		var choice interface{}
		if raw, ok := chat["tool_choice"]; ok {
			choice = raw
		}
		req, data, _, buildErr := corellm.NewResponsesAPIRequest(ctx, plan.Config, messages, corellm.ResponsesAPIRequestOptions{
			Stream:     stream,
			Tools:      tools,
			ToolChoice: choice,
			ExtraBody:  corellm.OpenCodeResponsesBudgetExtra(chat),
		})
		if buildErr != nil {
			return nil, "", "", buildErr
		}
		if req != nil && req.Body != nil {
			_ = req.Body.Close()
		}
		if req == nil || req.URL == nil {
			return nil, "", "", errors.New("responses probe request was not built")
		}
		return data, req.URL.String(), "responses", nil
	}
	if planned {
		return plan.Payload, plan.Endpoint, "openai", nil
	}
	raw, err := json.Marshal(chat)
	if err != nil {
		return nil, "", "", err
	}
	return raw, corellm.BuildOpenAIChatCompletionsEndpoint(cfg.URL), "openai", nil
}

func runOpenCodeStreamProbe(ctx context.Context, cfg corelib.MaclawLLMConfig, endpoint string, wire []byte, kind string, budget providerTestBudget, output map[string]llmservice.ProbeOutputField) providerProbeResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
	if err != nil {
		return probeFailed(budget, err.Error(), 0, output, "", nil)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if key := strings.TrimSpace(cfg.Key); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if ua := cfg.UserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	corelib.ApplyOpenCodeSessionHeader(req, cfg)
	start := time.Now()
	resp, err := corelib.NewLLMEndpointHTTPClient(cfg).Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return probeFailed(budget, llmProviderTestRequestError(err, cfg), latencyMs, output, "", nil)
	}
	defer resp.Body.Close()
	respBody, truncated, err := readLLMProviderTestResponse(resp.Body)
	if err != nil {
		return probeFailed(budget, llmProviderTestResponseReadError(err, cfg), latencyMs, output, "", nil)
	}
	if truncated {
		return probeFailed(budget, "model response exceeds the 64 KiB availability-test limit", latencyMs, output, "", nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeFailed(budget, llmProviderTestHTTPError(resp.StatusCode, respBody), latencyMs, output, "", nil)
	}
	return finishProbeStream(budget, latencyMs, output, llmservice.ParseProbeStream(respBody, kind))
}

func providerTestToolValues(tools, toolChoice json.RawMessage) ([]map[string]interface{}, interface{}) {
	tools = bytes.TrimSpace(tools)
	if len(tools) == 0 || string(tools) == "null" {
		return nil, nil
	}
	var parsed []map[string]interface{}
	if err := json.Unmarshal(tools, &parsed); err != nil || len(parsed) == 0 {
		return nil, nil
	}
	toolChoice = bytes.TrimSpace(toolChoice)
	if len(toolChoice) == 0 || string(toolChoice) == "null" {
		return parsed, nil
	}
	var choice interface{}
	if err := json.Unmarshal(toolChoice, &choice); err != nil {
		return parsed, nil
	}
	return parsed, choice
}

func injectProviderTestTools(body []byte, tools []map[string]interface{}, toolChoice interface{}, anthropic bool, drop []string) []byte {
	return llmservice.InjectMemberProbeTools(body, tools, toolChoice, anthropic, drop)
}

func llmProviderTestTimeout(cfg corelib.MaclawLLMConfig) time.Duration {
	return time.Duration(cfg.EffectiveTimeoutSec()) * time.Second
}

func llmProviderTestRequestError(err error, cfg corelib.MaclawLLMConfig) string {
	if errors.Is(err, context.DeadlineExceeded) || isLLMProviderTestTimeoutError(err) {
		return fmt.Sprintf("provider request timed out after %s; check the endpoint, model, and upstream capacity", llmProviderTestTimeout(cfg).Round(time.Second))
	}
	return err.Error()
}

func isLLMProviderTestTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

const llmProviderTestMaxResponseBytes = 64 << 10

func readLLMProviderTestResponse(body io.Reader) ([]byte, bool, error) {
	limited := &io.LimitedReader{R: body, N: llmProviderTestMaxResponseBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > llmProviderTestMaxResponseBytes, nil
}

func llmProviderTestResponseReadError(err error, cfg corelib.MaclawLLMConfig) string {
	if errors.Is(err, context.DeadlineExceeded) || isLLMProviderTestTimeoutError(err) {
		return fmt.Sprintf("provider response timed out after %s; check the endpoint, model, and upstream capacity", llmProviderTestTimeout(cfg).Round(time.Second))
	}
	return "read model response: " + err.Error()
}

func providerChatTestEnvelopeError(body []byte) string {
	var envelope struct {
		Type    string          `json:"type"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "invalid model response: " + err.Error()
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return "model returned an error: " + llmProviderTestErrorMessage(envelope.Error)
	}
	if strings.EqualFold(envelope.Type, "error") {
		errMsg := strings.TrimSpace(envelope.Message)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return "model returned an error: " + errMsg
	}
	return ""
}

func llmProviderTestHTTPError(statusCode int, body []byte) string {
	message := strings.TrimSpace(corellm.UserFacingHTTPStatus(statusCode, body))
	if message == "" {
		message = fmt.Sprintf("HTTP %d", statusCode)
	}
	return message
}

func llmProviderTestReply(body []byte, protocol, wireAPI string) (string, error) {
	var (
		response *corellm.Response
		err      error
	)
	if strings.EqualFold(protocol, "google") || strings.EqualFold(protocol, "gemini") {
		response, err = corellm.ParseGoogleGenerateContent(body)
	} else if protocol == "anthropic" {
		var anthropic struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		}
		if err = json.Unmarshal(body, &anthropic); err != nil {
			return "", err
		}
		var text, thinking strings.Builder
		for _, block := range anthropic.Content {
			switch block.Type {
			case "text":
				text.WriteString(block.Text)
			case "thinking":
				thinking.WriteString(block.Thinking)
			}
		}
		if s := strings.TrimSpace(text.String()); s != "" {
			return s, nil
		}
		// Extended-thinking models may spend the whole probe budget on the
		// thinking block; non-empty thinking still proves the model is reachable.
		return strings.TrimSpace(thinking.String()), nil
	} else if wireAPI == "responses" || wireAPI == "responses-ws" {
		response, err = corellm.ParseNonStreamResponsesAPIBody(body)
	} else {
		response, err = corellm.ParseNonStreamOpenAIResponseBody(body)
	}
	if err != nil {
		return "", err
	}
	if response == nil || len(response.Choices) == 0 {
		return "", nil
	}
	msg := response.Choices[0].Message
	if strings.TrimSpace(msg.Content) != "" {
		return msg.Content, nil
	}
	// Reasoning / thinking models (e.g. via OpenCode Zen) may place the short
	// probe reply entirely in reasoning_content when max_tokens is tight. Any
	// non-empty reasoning is still proof the model is reachable.
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		return msg.ReasoningContent, nil
	}
	if len(msg.ToolCalls) > 0 {
		return "tool_calls:" + msg.ToolCalls[0].Function.Name, nil
	}
	// Fallback for gateways that use `reasoning` instead of `reasoning_content`.
	var fallback struct {
		Choices []struct {
			Message struct {
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &fallback); err == nil && len(fallback.Choices) > 0 {
		if s := strings.TrimSpace(fallback.Choices[0].Message.ReasoningContent); s != "" {
			return s, nil
		}
		if s := strings.TrimSpace(fallback.Choices[0].Message.Reasoning); s != "" {
			return s, nil
		}
	}
	return "", nil
}

func llmProviderTestErrorMessage(raw json.RawMessage) string {
	var structured struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &structured); err == nil && strings.TrimSpace(structured.Message) != "" {
		return strings.TrimSpace(structured.Message)
	}
	var message string
	if err := json.Unmarshal(raw, &message); err == nil && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	message = strings.TrimSpace(string(raw))
	if len(message) > 500 {
		message = message[:500]
	}
	if message == "" || message == "null" {
		return "unknown error"
	}
	return message
}

func adminAddLLMProviderArray(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Billing struct {
				Timezone                 string                           `json:"timezone"`
				CreditMultiplier         float64                          `json:"credit_multiplier"`
				CreditMultiplierSchedule []llmpool.CreditMultiplierWindow `json:"credit_multiplier_schedule"`
				TokenPricing             llmpool.TokenPricing             `json:"token_pricing"`
			} `json:"billing"`
		}
		if err := decodeLimitedJSON(w, r, &body, 64<<10); err != nil {
			if errors.Is(err, errRequestBodyTooLarge) {
				writeJSONResp(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := svc.AddProviderArray(r.Context(), body.ID, body.Name, llmservice.ProviderArrayBilling{
			Timezone:                 body.Billing.Timezone,
			CreditMultiplier:         body.Billing.CreditMultiplier,
			CreditMultiplierSchedule: body.Billing.CreditMultiplierSchedule,
			TokenPricing:             body.Billing.TokenPricing,
		}); err != nil {
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminRenameLLMProviderArray(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider array id required"})
			return
		}
		var body struct {
			Name    string `json:"name"`
			Billing *struct {
				Timezone                 string                           `json:"timezone"`
				CreditMultiplier         float64                          `json:"credit_multiplier"`
				CreditMultiplierSchedule []llmpool.CreditMultiplierWindow `json:"credit_multiplier_schedule"`
				TokenPricing             llmpool.TokenPricing             `json:"token_pricing"`
			} `json:"billing"`
		}
		if err := decodeLimitedJSON(w, r, &body, 64<<10); err != nil {
			if errors.Is(err, errRequestBodyTooLarge) {
				writeJSONResp(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		var err error
		if body.Billing == nil {
			err = svc.RenameProviderArray(r.Context(), id, body.Name)
		} else {
			err = svc.UpdateProviderArray(r.Context(), id, body.Name, llmservice.ProviderArrayBilling{
				Timezone:                 body.Billing.Timezone,
				CreditMultiplier:         body.Billing.CreditMultiplier,
				CreditMultiplierSchedule: body.Billing.CreditMultiplierSchedule,
				TokenPricing:             body.Billing.TokenPricing,
			})
		}
		if err != nil {
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func adminListLLMProviderArrayReferences(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider array id required"})
			return
		}
		groups, err := svc.ProviderArrayReferences(r.Context(), id)
		if err != nil {
			writeLLMProviderError(w, err)
			return
		}
		type groupRef struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		refs := make([]groupRef, 0, len(groups))
		for _, g := range groups {
			refs = append(refs, groupRef{ID: g.ID, Name: g.Name})
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"groups": refs})
	}
}

func adminDeleteLLMProviderArray(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider array id required"})
			return
		}
		// prune is ignored. A referenced array is never deleted or stripped
		// from service groups; the admin removes the route first.
		groups, err := svc.DeleteProviderArray(r.Context(), id)
		if err != nil {
			if errors.Is(err, llmservice.ErrProviderInUse) {
				writeJSONResp(w, http.StatusConflict, map[string]any{"error": "provider_in_use", "groups": groups})
				return
			}
			writeLLMProviderError(w, err)
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

func adminDeleteLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		prune := r.URL.Query().Get("prune") == "1" || strings.EqualFold(r.URL.Query().Get("prune"), "true")
		pruned, err := svc.DeleteProvider(r.Context(), id, prune)
		if err != nil {
			if errors.Is(err, llmservice.ErrProviderInUse) {
				writeJSONResp(w, http.StatusConflict, map[string]any{"error": "provider_in_use", "groups": pruned})
				return
			}
			writeLLMProviderError(w, err)
			return
		}
		resp := map[string]any{"status": "ok"}
		if len(pruned) > 0 {
			resp["pruned_groups"] = pruned
		}
		writeJSONResp(w, http.StatusOK, resp)
	}
}

func adminListLLMProviderReferences(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		groups, err := svc.ProviderReferences(r.Context(), id)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		type groupRef struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		refs := make([]groupRef, 0, len(groups))
		for _, g := range groups {
			refs = append(refs, groupRef{ID: g.ID, Name: g.Name})
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"groups": refs})
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
		writeJSONResp(w, http.StatusOK, map[string]any{
			"service_groups":           reg.ServiceGroups,
			"default_service_group_id": catalogDefaultServiceGroupID(reg),
		})
	}
}

func catalogDefaultServiceGroupID(reg *llmservice.Registry) string {
	if reg == nil {
		return ""
	}
	id := strings.TrimSpace(reg.DefaultServiceGroupID)
	if id == "" {
		return ""
	}
	for _, group := range reg.ServiceGroups {
		if strings.EqualFold(strings.TrimSpace(group.ID), id) {
			return strings.TrimSpace(group.ID)
		}
	}
	return ""
}

func adminSetDefaultLLMServiceGroup(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, svc) {
			return
		}
		id := r.PathValue("id")
		if err := svc.SetDefaultServiceGroup(r.Context(), id); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok", "default_service_group_id": id})
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
		if llmpool.IsOfficialConventionGroupID(id) {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "MaClaw official group is system-generated and cannot be deleted"})
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
		views := make([]adminLLMAuthorizationView, 0, len(auths))
		for _, auth := range auths {
			if auth == nil {
				continue
			}
			views = append(views, adminLLMAuthorizationView{
				TenantAuthorization:     auth,
				CreditsUsed:             roundAdminLLMCreditDisplay(auth.CreditsUsed),
				CreditsRemaining:        roundAdminLLMCreditDisplay(auth.CreditsRemaining()),
				IsExternalComputeAccess: isExternalComputeAccessAuthorization(auth),
			})
		}
		writeJSONResp(w, http.StatusOK, map[string]any{"authorizations": views})
	}
}

type adminLLMAuthorizationView struct {
	*llmservice.TenantAuthorization
	CreditsUsed             float64 `json:"credits_used"`
	CreditsRemaining        float64 `json:"credits_remaining"`
	IsExternalComputeAccess bool    `json:"is_external_compute_access"`
}

func roundAdminLLMCreditDisplay(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func isExternalComputeAccessAuthorization(auth *llmservice.TenantAuthorization) bool {
	if auth == nil {
		return false
	}
	return auth.AllowExternalProviders ||
		auth.Source == "external_provider_permission" ||
		auth.ServiceGroupID == llmservice.ExternalComputePermissionServiceGroupID
}

func adminCreateLLMAuthorization(checker *llmservice.AuthorizationChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		var auth llmservice.TenantAuthorization
		if err := json.Unmarshal(body, &auth); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		auth.HubID = strings.TrimSpace(auth.HubID)
		auth.TenantID = strings.TrimSpace(auth.TenantID)
		auth.AdminEmail = strings.TrimSpace(auth.AdminEmail)
		auth.ServiceGroupID = strings.TrimSpace(auth.ServiceGroupID)
		auth.Source = strings.TrimSpace(auth.Source)
		if auth.HubID == "" || auth.TenantID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "hub_id and tenant_id are required"})
			return
		}
		_, hasAllowExternal := raw["allow_external_providers"]
		isExternalComputeGrant := auth.AllowExternalProviders ||
			auth.Source == "external_provider_permission" ||
			auth.ServiceGroupID == llmservice.ExternalComputePermissionServiceGroupID ||
			(hasAllowExternal && auth.ServiceGroupID == "" && auth.CreditsTotal == 0 && auth.Source == "")

		// Revocation path: allow_external_providers explicitly set to false means
		// "revoke external compute access". Expire all existing grants for this
		// hub+tenant and return without creating a new record.
		isRevocation := hasAllowExternal && !auth.AllowExternalProviders && isExternalComputeGrant
		if isRevocation {
			existing, err := checker.ListByHubTenantAliases(r.Context(), auth.HubID, auth.TenantID)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			revokedAt := time.Now().UTC()
			revoked := false
			for _, old := range existing {
				if !isExternalComputeAccessAuthorization(old) {
					continue
				}
				old.AllowExternalProviders = false
				old.Status = "expired"
				old.ExpiresAt = revokedAt
				old.UpdatedAt = revokedAt
				if err := checker.UpdateAuthorization(r.Context(), old); err != nil {
					writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				revoked = true
			}
			if !revoked {
				tombstone := newExternalComputeRevocationAuthorization(auth.HubID, auth.TenantID, auth.AdminEmail)
				existingByID, err := checker.GetByID(r.Context(), tombstone.ID)
				if err != nil {
					writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				if existingByID != nil {
					tombstone.CreatedAt = existingByID.CreatedAt
					if err := checker.UpdateAuthorization(r.Context(), tombstone); err != nil {
						writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
						return
					}
				} else if err := checker.CreateAuthorization(r.Context(), tombstone); err != nil {
					writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
			}
			writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok", "action": "revoked"})
			return
		}

		if auth.ServiceGroupID == "" && isExternalComputeGrant {
			auth.ServiceGroupID = llmservice.ExternalComputePermissionServiceGroupID
		}
		if auth.ServiceGroupID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service_group_id is required"})
			return
		}
		if isExternalComputeGrant {
			auth.Source = "external_provider_permission"
			// External compute grants always allow external providers.
			// To revoke, the admin should expire or delete the authorization,
			// not create a record with AllowExternalProviders=false.
			auth.AllowExternalProviders = true
			if auth.Status == "" {
				auth.Status = "active"
			}
			// Pure permission grant — no credits needed.
			// CreditsTotal stays at 0 (or whatever the admin explicitly set).
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
			existing, err := checker.ListByHubTenantAliases(r.Context(), auth.HubID, auth.TenantID)
			if err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, old := range existing {
				if old == nil || old.ID == auth.ID {
					continue
				}
				if !isExternalComputeAccessAuthorization(old) {
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
		existingByID, err := checker.GetByID(r.Context(), auth.ID)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if existingByID != nil {
			if !existingByID.CreatedAt.IsZero() {
				auth.CreatedAt = existingByID.CreatedAt
			}
			if err := checker.UpdateAuthorization(r.Context(), &auth); err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			if err := checker.CreateAuthorization(r.Context(), &auth); err != nil {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		for _, old := range supersededExternal {
			expiredAt := time.Now().UTC()
			old.AllowExternalProviders = false
			old.Status = "expired"
			old.ExpiresAt = expiredAt
			old.UpdatedAt = expiredAt
			if err := checker.UpdateAuthorization(r.Context(), old); err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok", "id": auth.ID})
	}
}

func newExternalComputeRevocationAuthorization(hubID, tenantID, adminEmail string) *llmservice.TenantAuthorization {
	now := time.Now().UTC()
	startsAt := now.Add(-time.Second)
	return &llmservice.TenantAuthorization{
		ID:                     "auth_admin_" + hubID + "_" + tenantID + "_" + llmservice.ExternalComputePermissionServiceGroupID,
		HubID:                  hubID,
		TenantID:               tenantID,
		AdminEmail:             strings.TrimSpace(adminEmail),
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           0,
		CreditsUsed:            0,
		StartsAt:               startsAt,
		ExpiresAt:              startsAt,
		Status:                 "expired",
		Source:                 "external_provider_permission",
		AllowExternalProviders: false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

// ---------------------------------------------------------------------------
// Usage Statistics handler
// ---------------------------------------------------------------------------

func adminLLMProviderTrafficHandler(statsSvc *llmservice.StatsService, proxyCfg *llmservice.ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes := trafficAccessNodes(proxyCfg)
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, &llmservice.ProviderTrafficReport{
				Timezone:     "Asia/Shanghai",
				Traffic:      map[string]llmservice.ProviderPeriodTraffic{},
				MemberHealth: memberHealthTrafficOrEmpty(r.Context(), "Asia/Shanghai", nodes),
			})
			return
		}
		report, err := statsSvc.QueryProviderTraffic(r.Context(), queryTimezone(r), time.Time{})
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		report.MemberHealth = memberHealthTrafficOrEmpty(r.Context(), report.Timezone, nodes)
		writeJSONResp(w, http.StatusOK, report)
	}
}

func trafficAccessNodes(proxyCfg *llmservice.ProxyConfig) []llmservice.AccessNodeView {
	if proxyCfg == nil || proxyCfg.ListAccessNodes == nil {
		return nil
	}
	return proxyCfg.ListAccessNodes()
}

func memberHealthTrafficOrEmpty(ctx context.Context, timezone string, nodes []llmservice.AccessNodeView) map[string]llmservice.ProviderPeriodTraffic {
	health := llmservice.MemberHealthPeriodTraffic(ctx, timezone, time.Time{}, nodes)
	if health == nil {
		return map[string]llmservice.ProviderPeriodTraffic{}
	}
	return health
}

func adminLLMServiceGroupTrafficHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, &llmservice.ServiceGroupTrafficReport{Timezone: "Asia/Shanghai", Traffic: map[string]llmservice.ProviderPeriodTraffic{}})
			return
		}
		report, err := statsSvc.QueryServiceGroupTraffic(r.Context(), queryTimezone(r), time.Time{})
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, report)
	}
}

func adminLLMUsageHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, map[string]any{"usage": []llmservice.TenantUsageSummary{}, "rows": []llmservice.TenantUsageSummary{}})
			return
		}
		q := r.URL.Query()
		filter := llmservice.UsageFilter{
			HubID:          q.Get("hub_id"),
			TenantID:       q.Get("tenant_id"),
			Model:          q.Get("model"),
			ServiceGroupID: q.Get("service_group_id"),
			WorkloadClass:  q.Get("workload_class"),
			Period:         q.Get("period"),
			StartDate:      firstNonEmpty(q.Get("start_date"), q.Get("start")),
			EndDate:        firstNonEmpty(q.Get("end_date"), q.Get("end")),
			Timezone:       queryTimezone(r),
		}
		if limit := q.Get("limit"); limit != "" {
			if n, err := strconv.Atoi(limit); err == nil {
				filter.Limit = n
			}
		}
		if filter.Limit <= 0 {
			filter.Limit = 30
		}
		if filter.Limit > 366 {
			filter.Limit = 366
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

func adminLLMClassTrafficHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.URL.Query().Get("service_group_id"))
		if groupID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service_group_id is required"})
			return
		}
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"service_group_id": groupID,
				"window":           "day",
				"timezone":         "Asia/Shanghai",
				"rows":             llmservice.NormalizeClassTrafficRows(nil),
				"sources":          map[string]int64{},
				"samples":          []llmservice.ClassTrafficSample{},
			})
			return
		}
		loc := llmservice.TrafficLocation(queryTimezone(r))
		window := strings.TrimSpace(r.URL.Query().Get("window"))
		since, window := llmservice.ClassTrafficSince(window, loc, time.Time{})
		rows, sources, samples, err := statsSvc.QueryClassTraffic(r.Context(), groupID, since)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{
			"service_group_id": groupID,
			"window":           window,
			"timezone":         loc.String(),
			"rows":             rows,
			"sources":          sources,
			"samples":          samples,
		})
	}
}

func adminLLMClassifyPreviewHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.URL.Query().Get("service_group_id"))
		if groupID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service_group_id is required"})
			return
		}
		var req struct {
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reg, err := llmSvc.LoadRegistry(r.Context())
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var group *llmpool.ServiceGroup
		for i := range reg.ServiceGroups {
			if strings.EqualFold(strings.TrimSpace(reg.ServiceGroups[i].ID), groupID) {
				group = &reg.ServiceGroups[i]
				break
			}
		}
		if group == nil {
			writeJSONResp(w, http.StatusNotFound, map[string]string{"error": "service group not found"})
			return
		}
		header := http.Header{}
		for key, value := range req.Headers {
			header.Set(key, value)
		}
		dec := llmpool.ClassifyAndRoute(header, req.Body, group)
		dec.BindRequestedGroup(groupID)
		writeJSONResp(w, http.StatusOK, dec)
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
		if cardstoreHasEnabledPaymentChannel(personal) {
			return personal, corecardstore.AlipayDirectConfig{}
		}
		return corecardstore.PersonalPaymentConfig{}, alipay
	case corecardstore.PaymentModeAlipay:
		if strings.TrimSpace(alipay.AppID) != "" {
			return corecardstore.PersonalPaymentConfig{}, alipay
		}
		return personal, corecardstore.AlipayDirectConfig{}
	default:
		return personal, alipay
	}
}

func cardstoreHasEnabledPaymentChannel(cfg corecardstore.PersonalPaymentConfig) bool {
	for _, ch := range cfg.Channels {
		if ch.Enabled {
			return true
		}
	}
	return false
}

func writeJSONResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
