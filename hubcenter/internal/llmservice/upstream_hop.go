package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

const (
	upstreamHopHeader     = "X-MaClaw-Upstream-Hop"
	upstreamHopPath       = "/api/internal/ha/llm/upstream"
	upstreamHopKindFwd    = "forward"
	upstreamHopKindStream = "stream"
	upstreamHopKindProbe  = "probe_models"
	upstreamHopKindTest   = "test_chat"
	// Match HA signed-body ceiling so vision payloads and large completions
	// are not truncated mid-hop.
	upstreamHopBodyLimit = 128 << 20
)

// AccessNodeView is one HubCenter node shown in the provider access-scope picker.
type AccessNodeView struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Host      string `json:"host,omitempty"`
	Reachable bool   `json:"reachable"`
	Self      bool   `json:"self,omitempty"`
}

type upstreamHopRequest struct {
	Kind          string         `json:"kind"`
	ProviderID    string         `json:"provider_id"`
	UpstreamModel string         `json:"upstream_model,omitempty"`
	ResponseModel string         `json:"response_model,omitempty"`
	Model         string         `json:"model,omitempty"`
	Body          map[string]any `json:"body,omitempty"`
}

type upstreamHopForwardResponse struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
}

type upstreamHopProbeResponse struct {
	Models []string `json:"models"`
}

type upstreamHopTestResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Reply     string `json:"reply,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

func egressProvider(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string) (*providerForwardResponse, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if cfg == nil || llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID) {
		return forwardToProvider(ctx, cfgHTTPClient(cfg), provider, body, upstreamModel, responseModel)
	}
	return hopProviderForward(ctx, cfg, provider, body, upstreamModel, responseModel)
}

func egressProviderStream(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, dst ProxyStreamWriter) (*providerStreamResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if cfg == nil || llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID) {
		return streamProviderToWriter(ctx, cfgHTTPClient(cfg), provider, body, upstreamModel, responseModel, dst)
	}
	return hopProviderStream(ctx, cfg, provider, body, upstreamModel, responseModel, dst)
}

func cfgHTTPClient(cfg *ProxyConfig) *http.Client {
	if cfg != nil && cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}
	return http.DefaultClient
}

func hopHTTPClient(cfg *ProxyConfig, timeout time.Duration) *http.Client {
	base := cfgHTTPClient(cfg)
	return &http.Client{
		Transport: base.Transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func probeHTTPClient(cfg *ProxyConfig) *http.Client {
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: cfgHTTPClient(cfg).Transport,
	}
}

func hopPeerBaseURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func hopProviderForward(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string) (*providerForwardResponse, error) {
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:          upstreamHopKindFwd,
		ProviderID:    provider.ID,
		UpstreamModel: upstreamModel,
		ResponseModel: responseModel,
		Body:          body,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal upstream hop: %w", err)
	}
	var lastErr error
	var lastResp *providerForwardResponse
	for _, nodeID := range hopCandidateNodeIDs(cfg, provider) {
		resp, err := hopJSON(ctx, cfg, nodeID, payload)
		if err != nil {
			lastErr = err
			if got := hopRetryableForwardResponse(err); got != nil {
				lastResp = got
			}
			continue
		}
		var out upstreamHopForwardResponse
		if err := json.Unmarshal(resp, &out); err != nil {
			lastErr = fmt.Errorf("decode hop from %s: %w", nodeID, err)
			continue
		}
		if out.StatusCode < 100 || out.StatusCode > 599 {
			lastErr = fmt.Errorf("invalid hop status from %s: %d", nodeID, out.StatusCode)
			continue
		}
		got := &providerForwardResponse{StatusCode: out.StatusCode, Body: out.Body}
		if shouldRetryProxyProviderStatus(got.StatusCode) {
			lastResp = got
			lastErr = fmt.Errorf("hubcenter node %s hop HTTP %d", nodeID, got.StatusCode)
			continue
		}
		return got, nil
	}
	if lastResp != nil {
		return lastResp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no allowed hubcenter node reachable for provider %s", provider.ID)
	}
	return nil, lastErr
}

func hopProviderStream(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, dst ProxyStreamWriter) (*providerStreamResult, error) {
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:          upstreamHopKindStream,
		ProviderID:    provider.ID,
		UpstreamModel: upstreamModel,
		ResponseModel: responseModel,
		Body:          body,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal upstream hop: %w", err)
	}
	var lastErr error
	var lastResult *providerStreamResult
	for _, nodeID := range hopCandidateNodeIDs(cfg, provider) {
		result, err := hopStream(ctx, cfg, nodeID, payload, dst, responseModel, body)
		if err != nil {
			lastErr = err
			lastResult = result
			if result != nil && result.wroteBusinessStream {
				return result, err
			}
			continue
		}
		if result != nil && shouldRetryProxyProviderStatus(result.statusCode) {
			lastResult = result
			lastErr = fmt.Errorf("hubcenter node %s hop HTTP %d", nodeID, result.statusCode)
			continue
		}
		return result, nil
	}
	if lastResult != nil && lastResult.statusCode >= http.StatusBadRequest && !lastResult.wroteBusinessStream {
		return lastResult, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no allowed hubcenter node reachable for provider %s", provider.ID)
	}
	return lastResult, lastErr
}

func hopCandidateNodeIDs(cfg *ProxyConfig, provider *llmpool.ProviderConfig) []string {
	if cfg == nil || provider == nil || cfg.LookupAccessPeer == nil {
		return nil
	}
	local := strings.TrimSpace(cfg.NodeID)
	type cand struct {
		id        string
		reachable bool
		rttMs     int64
	}
	var cands []cand
	for _, id := range llmpool.NormalizeAllowedNodeIDs(provider.AllowedNodeIDs) {
		if strings.EqualFold(id, local) {
			continue
		}
		url, reachable, rttMs := cfg.LookupAccessPeer(id)
		if hopPeerBaseURL(url) == "" {
			continue
		}
		cands = append(cands, cand{id: id, reachable: reachable, rttMs: rttMs})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].reachable != cands[j].reachable {
			return cands[i].reachable
		}
		if cands[i].rttMs != cands[j].rttMs {
			if cands[i].rttMs <= 0 {
				return false
			}
			if cands[j].rttMs <= 0 {
				return true
			}
			return cands[i].rttMs < cands[j].rttMs
		}
		return cands[i].id < cands[j].id
	})
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.id)
	}
	return out
}

func hopJSON(ctx context.Context, cfg *ProxyConfig, nodeID string, payload []byte) ([]byte, error) {
	resp, err := doUpstreamHop(ctx, cfg, nodeID, payload, cfgHTTPClient(cfg).Timeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limit := int64(upstreamHopBodyLimit)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limit = 8 << 10
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("read hop from %s: %w", nodeID, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, hopStatusError(nodeID, resp.StatusCode, body)
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); ct != "" && !strings.Contains(ct, "json") {
		return nil, hopStatusError(nodeID, resp.StatusCode, body)
	}
	return body, nil
}

func hopStream(ctx context.Context, cfg *ProxyConfig, nodeID string, payload []byte, dst ProxyStreamWriter, responseModel string, reqBody map[string]any) (*providerStreamResult, error) {
	resp, err := doUpstreamHop(ctx, cfg, nodeID, payload, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		result := &providerStreamResult{statusCode: resp.StatusCode, errorBody: body}
		if resp.StatusCode >= 400 && resp.StatusCode <= 599 {
			return result, nil
		}
		return result, hopStatusError(nodeID, resp.StatusCode, body)
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); !strings.Contains(ct, "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, hopStatusError(nodeID, resp.StatusCode, body)
	}
	result := &providerStreamResult{statusCode: resp.StatusCode}
	if err := proxyProviderSSE(resp.Body, dst, responseModel, result, reqBody); err != nil {
		return result, err
	}
	return result, nil
}

func doUpstreamHop(ctx context.Context, cfg *ProxyConfig, nodeID string, payload []byte, timeout time.Duration) (*http.Response, error) {
	if cfg == nil || cfg.LookupAccessPeer == nil {
		return nil, fmt.Errorf("upstream hop is not configured")
	}
	base, _, _ := cfg.LookupAccessPeer(nodeID)
	base = hopPeerBaseURL(base)
	if base == "" {
		return nil, fmt.Errorf("no internal URL for hubcenter node %s", nodeID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+upstreamHopPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(upstreamHopHeader, "1")
	if cfg.ClusterSecret != nil {
		if secret := strings.TrimSpace(cfg.ClusterSecret()); secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}
	if cfg.SignPeerRequest != nil {
		if err := cfg.SignPeerRequest(req); err != nil {
			return nil, fmt.Errorf("sign hop to %s: %w", nodeID, err)
		}
	}
	return hopHTTPClient(cfg, timeout).Do(req)
}

type hopHTTPError struct {
	NodeID string
	Status int
	Body   []byte
}

func (e *hopHTTPError) Error() string {
	if e == nil {
		return "hop HTTP error"
	}
	msg := strings.TrimSpace(string(e.Body))
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if len(msg) > 512 {
		msg = strings.ToValidUTF8(msg[:512], "") + "..."
	}
	return fmt.Sprintf("hubcenter node %s hop HTTP %d: %s", e.NodeID, e.Status, msg)
}

func hopStatusError(nodeID string, status int, body []byte) error {
	return &hopHTTPError{NodeID: nodeID, Status: status, Body: append([]byte(nil), body...)}
}

func hopRetryableForwardResponse(err error) *providerForwardResponse {
	var hopErr *hopHTTPError
	if !errors.As(err, &hopErr) || hopErr == nil || !shouldRetryProxyProviderStatus(hopErr.Status) {
		return nil
	}
	return &providerForwardResponse{StatusCode: hopErr.Status, Body: hopErr.Body}
}

// UpstreamHopHandler serves HA-authenticated provider egress on a node that
// is in the provider allowlist. It never deducts credits or records usage.
func UpstreamHopHandler(cfg *ProxyConfig, authenticate func(*http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg == nil || cfg.Service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "upstream hop is not configured")
			return
		}
		if authenticate != nil {
			if err := authenticate(r); err != nil {
				writeJSONError(w, http.StatusUnauthorized, err.Error())
				return
			}
		} else {
			writeJSONError(w, http.StatusUnauthorized, "ha peer authentication is not configured")
			return
		}
		if strings.TrimSpace(r.Header.Get(upstreamHopHeader)) == "" {
			writeJSONError(w, http.StatusBadRequest, "missing upstream hop header")
			return
		}
		var req upstreamHopRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, upstreamHopBodyLimit)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid hop body")
			return
		}
		providerID := strings.TrimSpace(req.ProviderID)
		if providerID == "" {
			writeJSONError(w, http.StatusBadRequest, "provider_id is required")
			return
		}
		provider, err := cfg.Service.GetProvider(r.Context(), providerID)
		if err != nil || provider == nil {
			writeJSONError(w, http.StatusNotFound, "provider not found")
			return
		}
		if !llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID) {
			writeJSONError(w, http.StatusForbidden, "this hubcenter node is not in the provider access scope")
			return
		}
		if provider.Paused {
			writeJSONError(w, http.StatusConflict, "provider is paused")
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = upstreamHopKindFwd
		}
		if kind == upstreamHopKindFwd || kind == upstreamHopKindStream {
			release, err := acquireProxyConcurrency(cfg, provider)
			if err != nil {
				writeJSONError(w, http.StatusTooManyRequests, err.Error())
				return
			}
			defer release()
		}
		switch kind {
		case upstreamHopKindFwd:
			resp, err := forwardToProvider(r.Context(), cfgHTTPClient(cfg), provider, req.Body, req.UpstreamModel, req.ResponseModel)
			if err != nil {
				code := hopRetryableTestStatus(err.Error())
				if code <= 0 {
					code = http.StatusBadGateway
				}
				writeJSONError(w, code, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(upstreamHopForwardResponse{StatusCode: resp.StatusCode, Body: resp.Body})
		case upstreamHopKindStream:
			flusher, _ := w.(http.Flusher)
			dst := &hopStreamWriter{ResponseWriter: w, flusher: flusher}
			result, err := streamProviderToWriter(r.Context(), cfgHTTPClient(cfg), provider, req.Body, req.UpstreamModel, req.ResponseModel, dst)
			if dst.started {
				return
			}
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			if result != nil && result.statusCode >= http.StatusBadRequest {
				code := result.statusCode
				if code < 400 || code > 599 {
					code = http.StatusBadGateway
				}
				msg := strings.TrimSpace(string(result.errorBody))
				if msg == "" {
					msg = http.StatusText(code)
				}
				writeJSONError(w, code, msg)
				return
			}
			writeJSONError(w, http.StatusBadGateway, "upstream stream ended before business data")
		case upstreamHopKindProbe:
			models, err := probeProviderModels(r.Context(), probeHTTPClient(cfg), provider.APIURL, provider.APIKey, provider.Protocol)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(upstreamHopProbeResponse{Models: models})
		case upstreamHopKindTest:
			model := strings.TrimSpace(req.Model)
			if model == "" && len(provider.Models) > 0 {
				model = provider.Models[0]
			}
			reply, errMsg, latencyMs := testProviderChat(r.Context(), cfgHTTPClient(cfg), provider, model)
			if code := hopRetryableTestStatus(errMsg); code > 0 {
				writeJSONError(w, code, errMsg)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(upstreamHopTestResponse{
				Success:   errMsg == "",
				Error:     errMsg,
				Reply:     reply,
				Model:     model,
				LatencyMs: latencyMs,
			})
		default:
			writeJSONError(w, http.StatusBadRequest, "unknown hop kind")
		}
	}
}

type hopStreamWriter struct {
	http.ResponseWriter
	flusher http.Flusher
	started bool
}

func (w *hopStreamWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, io.ErrClosedPipe
	}
	if !w.started {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.started = true
	}
	return w.ResponseWriter.Write(p)
}

func (w *hopStreamWriter) Flush() {
	if w != nil && w.flusher != nil {
		w.flusher.Flush()
	}
}

func probeProviderModels(ctx context.Context, client *http.Client, apiURL, apiKey, protocol string) ([]string, error) {
	endpoint, err := hopModelsEndpoint(apiURL, protocol)
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
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
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
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
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

func hopModelsEndpoint(apiURL, protocol string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return "", err
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("api_url is required")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("api_url is required")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/models") {
		return parsed.String(), nil
	}
	if strings.HasSuffix(path, "/chat/completions") {
		path = strings.TrimSuffix(path, "/chat/completions")
	}
	if strings.EqualFold(protocol, "anthropic") && !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	parsed.Path = path + "/models"
	return parsed.String(), nil
}

func testProviderChat(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, model string) (reply string, errMsg string, latencyMs int64) {
	if provider == nil {
		return "", "provider is required", 0
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", "model is required", 0
	}
	body := map[string]any{
		"model":      model,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: pong"}},
		"max_tokens": 16,
	}
	start := time.Now()
	resp, err := forwardToProvider(ctx, client, provider, body, model, model)
	latencyMs = time.Since(start).Milliseconds()
	if err != nil {
		return "", err.Error(), latencyMs
	}
	if resp == nil {
		return "", "empty response", latencyMs
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", hopTestHTTPError(resp.StatusCode, resp.Body), latencyMs
	}
	reply, errMsg = hopTestReply(resp.Body)
	return reply, errMsg, latencyMs
}

func hopTestHTTPError(statusCode int, body []byte) string {
	msg := strings.TrimSpace(hopTestErrorJSONMessage(body))
	if msg == "" {
		snippet := strings.TrimPrefix(proxyProviderErrorSnippet(body), ": ")
		msg = strings.TrimSpace(snippet)
	}
	if msg == "" {
		return fmt.Sprintf("HTTP %d", statusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", statusCode, msg)
}

func hopTestErrorJSONMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var envelope struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	if msg := strings.TrimSpace(envelope.Message); msg != "" && !strings.EqualFold(msg, "error") {
		return msg
	}
	if len(envelope.Error) == 0 || string(envelope.Error) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(envelope.Error, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var nested struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	}
	if err := json.Unmarshal(envelope.Error, &nested); err == nil {
		return strings.TrimSpace(nested.Message)
	}
	return ""
}

func hopTestReply(body []byte) (reply string, errMsg string) {
	var envelope struct {
		Type       string          `json:"type"`
		Message    string          `json:"message"`
		Error      json.RawMessage `json:"error"`
		OutputText string          `json:"output_text"`
		Choices    []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Content string `json:"content"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", "invalid model response: " + err.Error()
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		if msg := hopTestErrorJSONMessage(body); msg != "" {
			return "", "model returned an error: " + msg
		}
		return "", "model returned an error"
	}
	if strings.EqualFold(envelope.Type, "error") {
		errMsg := strings.TrimSpace(envelope.Message)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return "", "model returned an error: " + errMsg
	}
	if len(envelope.Choices) > 0 {
		msg := envelope.Choices[0].Message
		reply = strings.TrimSpace(msg.Content)
		if reply == "" {
			reply = strings.TrimSpace(msg.ReasoningContent)
		}
		if reply == "" {
			reply = strings.TrimSpace(msg.Reasoning)
		}
	}
	if reply == "" {
		var text, thinking strings.Builder
		for _, block := range envelope.Content {
			switch strings.ToLower(strings.TrimSpace(block.Type)) {
			case "text", "":
				text.WriteString(block.Text)
			case "thinking":
				thinking.WriteString(block.Thinking)
			}
		}
		reply = strings.TrimSpace(text.String())
		if reply == "" {
			reply = strings.TrimSpace(thinking.String())
		}
	}
	if reply == "" {
		reply = strings.TrimSpace(envelope.OutputText)
	}
	if reply == "" {
		var text strings.Builder
		for _, item := range envelope.Output {
			for _, part := range item.Content {
				switch strings.ToLower(strings.TrimSpace(part.Type)) {
				case "output_text", "text", "input_text", "":
					if chunk := strings.TrimSpace(part.Text); chunk != "" {
						text.WriteString(chunk)
					} else if chunk := strings.TrimSpace(part.Content); chunk != "" {
						text.WriteString(chunk)
					}
				}
			}
		}
		reply = strings.TrimSpace(text.String())
	}
	if reply == "" {
		return "", "model returned no completion content"
	}
	return reply, ""
}

func hopRetryableTestStatus(errMsg string) int {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return 0
	}
	if code := hopStatusAfterPrefix(errMsg, "HTTP "); shouldRetryHopTestStatus(code) {
		return code
	}
	// corelib.ForwardLLMEndpointProviderRequest: POST "url": 429 Too Many Requests "body"
	if i := strings.LastIndex(errMsg, `": `); i >= 0 {
		var code int
		if _, err := fmt.Sscanf(errMsg[i+3:], "%d", &code); err == nil && shouldRetryHopTestStatus(code) {
			return code
		}
	}
	return 0
}

func shouldRetryHopTestStatus(statusCode int) bool {
	return statusCode >= 500 || isProxyRateLimitStatus(statusCode)
}

func hopStatusAfterPrefix(errMsg, prefix string) int {
	idx := strings.Index(errMsg, prefix)
	if idx < 0 {
		return 0
	}
	var code int
	if _, err := fmt.Sscanf(errMsg[idx+len(prefix):], "%d", &code); err != nil {
		return 0
	}
	return code
}

func hopAdminJSON(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, kind, model string) ([]byte, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:       kind,
		ProviderID: provider.ID,
		Model:      model,
	})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, nodeID := range hopCandidateNodeIDs(cfg, provider) {
		body, err := hopJSON(ctx, cfg, nodeID, payload)
		if err != nil {
			lastErr = err
			continue
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no allowed hubcenter node reachable for provider %s", provider.ID)
	}
	return nil, lastErr
}

func providerNeedsUpstreamHop(cfg *ProxyConfig, provider *llmpool.ProviderConfig) bool {
	return provider != nil && strings.TrimSpace(provider.ID) != "" && cfg != nil && !llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID)
}

// ProbeProviderModelsWithScope probes locally when this node is in scope,
// otherwise hops to an allowed peer. Unsaved providers (no ID) always probe locally.
func ProbeProviderModelsWithScope(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, apiURL, apiKey, protocol string) ([]string, error) {
	if providerNeedsUpstreamHop(cfg, provider) {
		body, err := hopAdminJSON(ctx, cfg, provider, upstreamHopKindProbe, "")
		if err != nil {
			return nil, err
		}
		var out upstreamHopProbeResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		return out.Models, nil
	}
	if apiURL == "" && provider != nil {
		apiURL = provider.APIURL
		apiKey = provider.APIKey
		protocol = provider.Protocol
	}
	return probeProviderModels(ctx, &http.Client{Timeout: 20 * time.Second}, apiURL, apiKey, protocol)
}

// TestProviderChatWithScope tests locally when this node is in scope,
// otherwise hops to an allowed peer.
func TestProviderChatWithScope(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, model string) (reply string, errMsg string, latencyMs int64, testModel string) {
	if provider == nil {
		return "", "provider is required", 0, model
	}
	model = strings.TrimSpace(model)
	if model == "" && len(provider.Models) > 0 {
		model = provider.Models[0]
	}
	if providerNeedsUpstreamHop(cfg, provider) {
		body, err := hopAdminJSON(ctx, cfg, provider, upstreamHopKindTest, model)
		if err != nil {
			return "", err.Error(), 0, model
		}
		var out upstreamHopTestResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return "", err.Error(), 0, model
		}
		if !out.Success {
			errMsg := strings.TrimSpace(out.Error)
			if errMsg == "" {
				errMsg = "provider test failed"
			}
			return out.Reply, errMsg, out.LatencyMs, firstNonEmpty(out.Model, model)
		}
		return out.Reply, "", out.LatencyMs, firstNonEmpty(out.Model, model)
	}
	reply, errMsg, latencyMs = testProviderChat(ctx, cfgHTTPClient(cfg), provider, model)
	return reply, errMsg, latencyMs, model
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func DefaultAccessNodes(nodeID string) []AccessNodeView {
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "local"
	}
	return []AccessNodeView{{NodeID: id, Name: id, Reachable: true, Self: true}}
}
