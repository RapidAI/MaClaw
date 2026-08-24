package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
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

func writeLLMProviderError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, llmservice.ErrProviderNotFound) {
		code = http.StatusNotFound
	}
	writeJSONResp(w, code, map[string]string{"error": err.Error()})
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
		}
		sort.SliceStable(safe, func(i, j int) bool {
			si := llmpool.EffectiveProviderSequence(safe[i].Sequence)
			sj := llmpool.EffectiveProviderSequence(safe[j].Sequence)
			if si != sj {
				return si < sj
			}
			return strings.ToLower(strings.TrimSpace(safe[i].ID)) < strings.ToLower(strings.TrimSpace(safe[j].ID))
		})
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
			writeLLMProviderError(w, err)
			return
		}
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

// adminTestLLMProviderChat performs an end-to-end chat completion test against
// a configured provider. Unlike probe-models (which only checks /v1/models
// reachability), this sends an actual chat request and verifies a response is
// returned — the same test the client would exercise.
func adminTestLLMProviderChat(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID string `json:"provider_id"`
			APIURL     string `json:"api_url"`
			APIKey     string `json:"api_key"`
			Model      string `json:"model"`
			Protocol   string `json:"protocol"`
			WireAPI    string `json:"wire_api"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		// A saved provider ID is authoritative for connection data.  In
		// particular, never combine its stored key with a caller-supplied URL:
		// that would allow an administrator's browser request to send a provider
		// secret to a different endpoint. The model remains selectable so a
		// service-group route can test its configured upstream model.
		timeoutSec := 0
		if providerID := strings.TrimSpace(req.ProviderID); providerID != "" {
			if svc == nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "provider service unavailable"})
				return
			}
			existing, err := svc.GetProvider(r.Context(), providerID)
			if err != nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "load provider: " + err.Error()})
				return
			}
			if existing == nil {
				writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": "provider not found"})
				return
			}
			req.APIKey = existing.APIKey
			req.APIURL = existing.APIURL
			req.Protocol = existing.Protocol
			req.WireAPI = existing.WireAPI
			timeoutSec = existing.UpstreamTimeoutSec
			if req.Model == "" && len(existing.Models) > 0 {
				req.Model = existing.Models[0]
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
		protocol := corelib.NormalizeLLMProviderProtocol(req.Protocol)
		wireAPI := corelib.NormalizeLLMProviderWireAPI(req.WireAPI)
		cfg := corelib.MaclawLLMConfig{
			URL:        req.APIURL,
			Key:        req.APIKey,
			Model:      req.Model,
			Protocol:   protocol,
			WireAPI:    wireAPI,
			TimeoutSec: timeoutSec,
		}
		// An availability test must use the provider's production timeout. Slower
		// reasoning providers can legitimately take more than 15 seconds before
		// returning their first completion.
		ctx, cancel := context.WithTimeout(r.Context(), llmProviderTestTimeout(cfg))
		defer cancel()
		httpReq, err := newLLMProviderTestRequest(ctx, cfg)
		if err != nil {
			writeJSONResp(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		start := time.Now()
		client := corelib.NewLLMEndpointHTTPClient(cfg)
		resp, err := client.Do(httpReq)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      llmProviderTestRequestError(err, cfg),
				"latency_ms": latencyMs,
			})
			return
		}
		defer resp.Body.Close()
		respBody, truncated, err := readLLMProviderTestResponse(resp.Body)
		if err != nil {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      llmProviderTestResponseReadError(err, cfg),
				"latency_ms": latencyMs,
			})
			return
		}
		if truncated {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "model response exceeds the 64 KiB availability-test limit",
				"latency_ms": latencyMs,
			})
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      llmProviderTestHTTPError(resp.StatusCode, respBody),
				"latency_ms": latencyMs,
			})
			return
		}
		// A successful HTTP response is not itself proof that the model is usable:
		// some compatible gateways return an error envelope with HTTP 200. Treat
		// those responses, malformed bodies, and empty completions as failures.
		var envelope struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(respBody, &envelope); err != nil {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "invalid model response: " + err.Error(),
				"latency_ms": latencyMs,
			})
			return
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "model returned an error: " + llmProviderTestErrorMessage(envelope.Error),
				"latency_ms": latencyMs,
			})
			return
		}
		if strings.EqualFold(envelope.Type, "error") {
			errMsg := strings.TrimSpace(envelope.Message)
			if errMsg == "" {
				errMsg = "unknown error"
			}
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "model returned an error: " + errMsg,
				"latency_ms": latencyMs,
			})
			return
		}

		// Parse the actual completion. A model is available only after it returns
		// non-empty content to the short test prompt.
		reply, err := llmProviderTestReply(respBody, protocol, wireAPI)
		if err != nil {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "invalid model response: " + err.Error(),
				"latency_ms": latencyMs,
			})
			return
		}
		if strings.TrimSpace(reply) == "" {
			writeJSONResp(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      "model returned no completion content",
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

func newLLMProviderTestRequest(ctx context.Context, cfg corelib.MaclawLLMConfig) (*http.Request, error) {
	messages := []interface{}{map[string]string{"role": "user", "content": "Reply with exactly: pong"}}
	if cfg.Protocol == "anthropic" {
		endpoint, body, err := corellm.BuildAnthropicMessagesRequestData(cfg, messages, corellm.AnthropicMessagesRequestOptions{MaxTokens: 64})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.Key != "" {
			req.Header.Set("x-api-key", cfg.Key)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		return req, nil
	}
	if cfg.IsResponsesAPI() {
		req, _, _, err := corellm.NewResponsesAPIRequest(ctx, cfg, messages, corellm.ResponsesAPIRequestOptions{
			ExtraBody: map[string]interface{}{"max_output_tokens": 64},
		})
		return req, err
	}
	req, _, _, err := corellm.NewOpenAIChatRequest(ctx, cfg, messages, corellm.OpenAIChatRequestOptions{
		ExtraBody: map[string]interface{}{"max_tokens": 64, "temperature": 0},
	})
	return req, err
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
	if protocol == "anthropic" {
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

func adminDeleteLLMProvider(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "provider id required"})
			return
		}
		if err := svc.DeleteProvider(r.Context(), id); err != nil {
			writeLLMProviderError(w, err)
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

func adminLLMProviderTrafficHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, &llmservice.ProviderTrafficReport{Timezone: "Asia/Shanghai", Traffic: map[string]llmservice.ProviderPeriodTraffic{}})
			return
		}
		timezone := strings.TrimSpace(r.URL.Query().Get("timezone"))
		if len(timezone) > 64 {
			timezone = ""
		}
		report, err := statsSvc.QueryProviderTraffic(r.Context(), timezone, time.Time{})
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, report)
	}
}

func adminLLMServiceGroupTrafficHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if statsSvc == nil {
			writeJSONResp(w, http.StatusOK, &llmservice.ServiceGroupTrafficReport{Timezone: "Asia/Shanghai", Traffic: map[string]llmservice.ProviderPeriodTraffic{}})
			return
		}
		timezone := strings.TrimSpace(r.URL.Query().Get("timezone"))
		if len(timezone) > 64 {
			timezone = ""
		}
		report, err := statsSvc.QueryServiceGroupTraffic(r.Context(), timezone, time.Time{})
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, report)
	}
}

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

func adminLLMClassTrafficHandler(statsSvc *llmservice.StatsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.URL.Query().Get("service_group_id"))
		if groupID == "" {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "service_group_id is required"})
			return
		}
		window := strings.TrimSpace(r.URL.Query().Get("window"))
		days := 1
		switch window {
		case "7d":
			days = 7
		case "30d":
			days = 30
		default:
			window = "24h"
		}
		since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		rows, sources, samples, err := statsSvc.QueryClassTraffic(r.Context(), groupID, since)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]any{
			"service_group_id": groupID,
			"window":           window,
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
