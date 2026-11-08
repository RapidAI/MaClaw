package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
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
	// modelCatalogBodyLimit caps an upstream GET /models body. Catalogs that
	// embed per-model pricing (cheaperinference is about 2.6MB for ~570 models)
	// exceed 2MiB. A shorter read truncates the JSON and Unmarshal reports
	// "unexpected end of JSON input". 64MiB leaves room for that catalog to grow.
	modelCatalogBodyLimit = 64 << 20
	// probeErrorBodyLimit caps a non-2xx probe body so an error page cannot use
	// the catalog read budget.
	probeErrorBodyLimit = 8 << 10
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
	Kind                string          `json:"kind"`
	ProviderID          string          `json:"provider_id"`
	UpstreamModel       string          `json:"upstream_model,omitempty"`
	ResponseModel       string          `json:"response_model,omitempty"`
	Model               string          `json:"model,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	MaxTokens           *int64          `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	// ReportTestResult asks the peer to return the probe body even when the
	// upstream answers 429 or 5xx. An explicit node test is about that node;
	// those statuses are the result, not a signal to try a different peer.
	Body map[string]any `json:"body,omitempty"`
	// Provider carries the upstream credential from the node that accepted the
	// request. A copy's egress node may not have that row yet, or may still
	// have an empty key. The hop is HA-authenticated and the node is already
	// in the provider allowlist.
	Provider *llmpool.ProviderConfig `json:"provider,omitempty"`
	// SingleAttempt tells the peer not to repeat a failed upstream call.
	// Array members already fail over to a sibling, so the extra attempts
	// only add latency. Older peers ignore the field and keep their retries.
	SingleAttempt    bool `json:"single_attempt,omitempty"`
	ReportTestResult bool `json:"report_test_result,omitempty"`
}

type upstreamHopForwardResponse struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
}

type upstreamHopProbeResponse struct {
	Models []string `json:"models"`
}

type upstreamHopTestResponse struct {
	Success   bool                        `json:"success"`
	Error     string                      `json:"error,omitempty"`
	Reply     string                      `json:"reply,omitempty"`
	Model     string                      `json:"model,omitempty"`
	LatencyMs int64                       `json:"latency_ms"`
	ToolCalls []ProviderToolCall          `json:"tool_calls,omitempty"`
	Output    map[string]ProbeOutputField `json:"output,omitempty"`
	Stream    bool                        `json:"stream,omitempty"`
	StreamEnd *bool                       `json:"stream_end,omitempty"`
}

func egressProvider(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string) (*providerForwardResponse, error) {
	return egressProviderAttempts(ctx, cfg, provider, body, upstreamModel, responseModel, true)
}

// egressArrayMember tries a provider-array member once. A sibling can take the
// request, so the usual three immediate upstream retries would only add latency.
func egressArrayMember(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string) (*providerForwardResponse, error) {
	return egressProviderAttempts(ctx, cfg, provider, body, upstreamModel, responseModel, false)
}

func egressProviderAttempts(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	provider = RotateTokenBankKey(provider)
	body = tokenBankCappedRequestBody(provider, body)
	if err := enforceTokenBankRequestCaps(ctx, cfg, provider, body); err != nil {
		return nil, err
	}
	if cfg == nil || llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID) {
		return forwardToProviderAttempts(ctx, cfgHTTPClient(cfg), provider, body, upstreamModel, responseModel, retry)
	}
	return hopProviderForwardAttempts(ctx, cfg, provider, body, upstreamModel, responseModel, !retry)
}

func egressProviderStream(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, dst ProxyStreamWriter) (*providerStreamResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	provider = RotateTokenBankKey(provider)
	body = tokenBankCappedRequestBody(provider, body)
	if err := enforceTokenBankRequestCaps(ctx, cfg, provider, body); err != nil {
		return nil, err
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

// providerProbeTimeout is the upstream budget for one member probe. It matches
// a local availability test. The shared proxy client is shorter (180s) and
// would mark a slow overseas member as down when this node is asked to test it.
func providerProbeTimeout(provider *llmpool.ProviderConfig) time.Duration {
	sec := 0
	if provider != nil {
		sec = provider.UpstreamTimeoutSec
	}
	return time.Duration(corelib.MaclawLLMConfig{TimeoutSec: sec}.EffectiveTimeoutSec()) * time.Second
}

// MemberProbeContextTimeout is how long the caller waits for a probe that
// runs on another node: the member's own budget, plus a short hop so the
// peer can return that result.
func MemberProbeContextTimeout(provider *llmpool.ProviderConfig) time.Duration {
	return providerProbeTimeout(provider) + 15*time.Second
}

func providerProbeError(err error, provider *llmpool.ProviderConfig) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || isProbeTimeout(err) {
		return fmt.Sprintf("provider request timed out after %s", providerProbeTimeout(provider).Round(time.Second))
	}
	return err.Error()
}

func isProbeTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func memberProbeHTTPClient(cfg *ProxyConfig, provider *llmpool.ProviderConfig) *http.Client {
	base := cfgHTTPClient(cfg)
	// Follow redirects the same way real egress does. Stopping at the first
	// 3xx made a node-targeted probe fail for an upstream that only redirects.
	return &http.Client{
		Transport: base.Transport,
		Timeout:   providerProbeTimeout(provider),
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
	return hopProviderForwardAttempts(ctx, cfg, provider, body, upstreamModel, responseModel, false)
}

func hopProviderForwardAttempts(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, singleAttempt bool) (*providerForwardResponse, error) {
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:          upstreamHopKindFwd,
		ProviderID:    provider.ID,
		UpstreamModel: upstreamModel,
		ResponseModel: responseModel,
		Body:          body,
		Provider:      hopProviderSnapshot(provider),
		SingleAttempt: singleAttempt,
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
		if shouldRetryProxyProviderFailure(got.StatusCode, got.Body) {
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
		Provider:      hopProviderSnapshot(provider),
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
			// A committed prefix cannot be taken back. An attempt that only
			// buffered business bytes leaves wroteBusinessStream clear and
			// tries the next allowed node.
			if result != nil && result.wroteBusinessStream {
				return result, err
			}
			continue
		}
		if result != nil && shouldRetryProxyProviderFailure(result.statusCode, result.errorBody) {
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
	return hopJSONTimeout(ctx, cfg, nodeID, payload, cfgHTTPClient(cfg).Timeout)
}

func hopJSONTimeout(ctx context.Context, cfg *ProxyConfig, nodeID string, payload []byte, timeout time.Duration) ([]byte, error) {
	resp, err := doUpstreamHop(ctx, cfg, nodeID, payload, timeout)
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
	if !errors.As(err, &hopErr) || hopErr == nil || !shouldRetryProxyProviderFailure(hopErr.Status, hopErr.Body) {
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
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "provider not found")
			return
		}
		if provider == nil {
			supplied, allowed := hopSuppliedProvider(req.Provider, providerID, cfg.NodeID)
			if supplied == nil {
				if req.Provider != nil && !allowed {
					writeJSONError(w, http.StatusForbidden, "this hubcenter node is not in the provider access scope")
					return
				}
				writeJSONError(w, http.StatusNotFound, "provider not found")
				return
			}
			provider = supplied
		} else if !llmpool.ProviderAllowedOnNode(*provider, cfg.NodeID) {
			writeJSONError(w, http.StatusForbidden, "this hubcenter node is not in the provider access scope")
			return
		} else {
			overlayHopCredentials(provider, req.Provider)
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
			resp, err := forwardToProviderAttempts(r.Context(), cfgHTTPClient(cfg), provider, req.Body, req.UpstreamModel, req.ResponseModel, !req.SingleAttempt)
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
			models, err := ProbeProviderModels(r.Context(), probeHTTPClient(cfg), provider.Name, provider.APIURL, provider.APIKey, provider.Protocol, provider.AgentType)
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
			probed := probeProviderChat(r.Context(), memberProbeHTTPClient(cfg, provider), provider, ProviderChatTest{
				Model:               model,
				Tools:               req.Tools,
				ToolChoice:          req.ToolChoice,
				MaxTokens:           req.MaxTokens,
				MaxCompletionTokens: req.MaxCompletionTokens,
				Stream:              req.Stream,
			})
			if !req.ReportTestResult {
				if code := hopRetryableTestStatus(probed.Err); code > 0 {
					writeJSONError(w, code, probed.Err)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(upstreamHopTestResponse{
				Success:   probed.Err == "",
				Error:     probed.Err,
				Reply:     probed.Reply,
				Model:     model,
				LatencyMs: probed.LatencyMs,
				ToolCalls: probed.Calls,
				Output:    probed.Output,
				Stream:    probed.Stream,
				StreamEnd: probed.StreamEnd,
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

// ParseModelCatalogResponse reads a probe response. A non-2xx body is capped
// separately from the catalog limit. A 2xx body is parsed with ParseModelCatalog.
func ParseModelCatalogResponse(statusCode int, status string, body io.Reader) ([]string, error) {
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("probe failed: %s", probeErrorMessage(status, body))
	}
	return ParseModelCatalog(body)
}

// ParseModelCatalog reads an upstream GET /models body and returns model ids.
// The body is capped, then decoded into id fields only. encoding/json skips
// the per-model pricing payload without retaining it. A body past the cap is
// rejected instead of being parsed as truncated JSON.
func ParseModelCatalog(r io.Reader) ([]string, error) {
	return parseModelCatalogIDs(r, modelCatalogBodyLimit)
}

type modelCatalogItem struct {
	ID string `json:"id"`
}

type modelCatalogDocument struct {
	Data   []modelCatalogItem `json:"data"`
	Models []modelCatalogItem `json:"models"`
}

func parseModelCatalogIDs(r io.Reader, limit int64) ([]string, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("model catalog exceeded %d bytes", limit)
	}
	var parsed modelCatalogDocument
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(parsed.Data)+len(parsed.Models))
	models := make([]string, 0, len(parsed.Data)+len(parsed.Models))
	appendModelID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	for _, item := range parsed.Data {
		appendModelID(item.ID)
	}
	for _, item := range parsed.Models {
		appendModelID(item.ID)
	}
	sort.Strings(models)
	return models, nil
}

func probeErrorMessage(status string, r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, probeErrorBodyLimit))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return status
	}
	return msg
}

// ProbeProviderModels lists model ids from an upstream /models endpoint.
// The admin probe and a hopped probe both use this so the request and the
// catalog limit stay the same.
func ProbeProviderModels(ctx context.Context, client *http.Client, providerName, apiURL, apiKey, protocol, agentType string) ([]string, error) {
	apiURL = OpenCodeCatalogURL(apiURL, agentType)
	if corelib.IsOpenCodeURL(apiURL) {
		agent := RequestAgentType(agentType)
		if agent == "" || strings.EqualFold(agent, "openclaw") {
			agentType = "OpenCode"
		}
	}
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
	probeCfg := corelib.MaclawLLMConfig{URL: apiURL, AgentType: RequestAgentType(agentType)}
	if ua := probeCfg.UserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	corelib.ApplyOpenCodeSessionHeader(req, probeCfg)
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	client = DevinHTTPClient(client, providerName, apiURL)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ParseModelCatalogResponse(resp.StatusCode, resp.Status, resp.Body)
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

func testProviderChat(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, spec ProviderChatTest) (reply string, errMsg string, latencyMs int64, calls []ProviderToolCall) {
	got := probeProviderChat(ctx, client, provider, spec)
	return got.Reply, got.Err, got.LatencyMs, got.Calls
}

// providerChatProbe is the peer's member test, including the output budgets
// on the request it built and, when stream was set, whether the end marker arrived.
type providerChatProbe struct {
	Reply     string
	Err       string
	LatencyMs int64
	Calls     []ProviderToolCall
	Output    map[string]ProbeOutputField
	Stream    bool
	StreamEnd *bool
}

func probeProviderChat(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, spec ProviderChatTest) providerChatProbe {
	if provider == nil {
		return providerChatProbe{Err: "provider is required", Stream: spec.Stream, StreamEnd: probeStreamEnd(spec.Stream, false)}
	}
	model := strings.TrimSpace(spec.Model)
	if model == "" {
		return providerChatProbe{Err: "model is required", Stream: spec.Stream, StreamEnd: probeStreamEnd(spec.Stream, false)}
	}
	if spec.Stream {
		return probeProviderChatStream(ctx, client, provider, spec, model)
	}
	prompt := "Reply with exactly: pong"
	toolsSet := len(bytes.TrimSpace(spec.Tools)) > 0 && string(bytes.TrimSpace(spec.Tools)) != "null"
	if toolsSet {
		prompt = "Call one of the available tools now."
	}
	body := map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": prompt}},
	}
	for key, value := range MemberProbeOutputBody(provider.MaxOutputTokensPerRequest, spec.MaxTokens, spec.MaxCompletionTokens, toolsSet) {
		body[key] = value
	}
	if parsed, ok := rawJSONValue(spec.Tools); ok {
		body["tools"] = parsed
	}
	if parsed, ok := rawJSONValue(spec.ToolChoice); ok {
		body["tool_choice"] = parsed
	}
	output := hopProbeOutput(provider, body, spec, toolsSet)
	start := time.Now()
	resp, err := forwardToProviderAttempts(ctx, client, provider, body, model, model, false)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return providerChatProbe{Err: providerProbeError(err, provider), LatencyMs: latencyMs, Output: output}
	}
	if resp == nil {
		return providerChatProbe{Err: "empty response", LatencyMs: latencyMs, Output: output}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerChatProbe{Err: hopTestHTTPError(resp.StatusCode, resp.Body), LatencyMs: latencyMs, Output: output}
	}
	reply, errMsg := hopTestReply(resp.Body)
	calls := ParseProviderTestToolCalls(resp.Body)
	if errMsg == "model returned no completion content" && len(calls) > 0 {
		errMsg = ""
	}
	return providerChatProbe{Reply: reply, Err: errMsg, LatencyMs: latencyMs, Calls: calls, Output: output}
}

func probeStreamEnd(stream bool, end bool) *bool {
	if !stream {
		return nil
	}
	return &end
}

func probeProviderChatStream(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, spec ProviderChatTest, model string) providerChatProbe {
	cfg := MemberProbeConfig(corelib.MaclawLLMConfig{
		URL:        provider.APIURL,
		Key:        strings.TrimSpace(provider.APIKey),
		Model:      model,
		Protocol:   corelib.NormalizeLLMProviderProtocol(provider.Protocol),
		WireAPI:    corelib.NormalizeLLMProviderWireAPI(provider.WireAPI),
		TimeoutSec: provider.UpstreamTimeoutSec,
	}, provider)
	toolsSet := len(bytes.TrimSpace(spec.Tools)) > 0 && string(bytes.TrimSpace(spec.Tools)) != "null"
	var (
		req  *http.Request
		wire []byte
		kind string
		err  error
	)
	if corelib.IsOpenCodeURL(provider.APIURL) {
		req, wire, kind, err = openCodeProbeStreamRequest(ctx, cfg, provider, spec, model)
	} else {
		kind = ProbeOutputKind(cfg)
		req, wire, err = NewMemberProbeRequest(ctx, cfg, spec.Tools, spec.ToolChoice, MemberProbeBudget{
			MaxTokens:           spec.MaxTokens,
			MaxCompletionTokens: spec.MaxCompletionTokens,
			OutputCap:           provider.MaxOutputTokensPerRequest,
			Stream:              true,
		})
	}
	output := ProjectProbeOutput(wire, ProbeOutputBaseline(kind, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields, spec.MaxTokens, spec.MaxCompletionTokens, toolsSet))
	if err != nil {
		return providerChatProbe{Err: err.Error(), Output: output, Stream: true, StreamEnd: probeStreamEnd(true, false)}
	}
	client = WrapMemberProbeClient(client, cfg)
	start := time.Now()
	resp, err := client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return providerChatProbe{Err: providerProbeError(err, provider), LatencyMs: latencyMs, Output: output, Stream: true, StreamEnd: probeStreamEnd(true, false)}
	}
	defer resp.Body.Close()
	respBody, truncated, readErr := ReadProbeBody(resp.Body)
	if readErr != nil {
		return providerChatProbe{Err: providerProbeError(readErr, provider), LatencyMs: latencyMs, Output: output, Stream: true, StreamEnd: probeStreamEnd(true, false)}
	}
	if truncated {
		return providerChatProbe{Err: "model response exceeds the 64 KiB availability-test limit", LatencyMs: latencyMs, Output: output, Stream: true, StreamEnd: probeStreamEnd(true, false)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerChatProbe{Err: hopTestHTTPError(resp.StatusCode, respBody), LatencyMs: latencyMs, Output: output, Stream: true, StreamEnd: probeStreamEnd(true, false)}
	}
	parsed := ParseProbeStream(respBody, kind)
	return providerChatProbe{
		Reply: parsed.Reply, Err: parsed.Failure(), LatencyMs: latencyMs, Calls: parsed.Calls,
		Output: output, Stream: true, StreamEnd: probeStreamEnd(true, parsed.End),
	}
}

func openCodeProbeStreamRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, provider *llmpool.ProviderConfig, spec ProviderChatTest, model string) (*http.Request, []byte, string, error) {
	toolsSet := len(bytes.TrimSpace(spec.Tools)) > 0 && string(bytes.TrimSpace(spec.Tools)) != "null"
	prompt := "Reply with exactly: pong"
	if toolsSet {
		prompt = "Call one of the available tools now."
	}
	body := map[string]any{
		"model":    model,
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": prompt}},
	}
	outputCap := provider.MaxOutputTokensPerRequest
	if spec.MaxTokens != nil || spec.MaxCompletionTokens != nil || outputCap > 0 {
		for key, value := range MemberProbeOutputBody(outputCap, spec.MaxTokens, spec.MaxCompletionTokens, toolsSet) {
			body[key] = value
		}
	}
	if parsed, ok := rawJSONValue(spec.Tools); ok {
		body["tools"] = parsed
	}
	if parsed, ok := rawJSONValue(spec.ToolChoice); ok {
		body["tool_choice"] = parsed
	}
	plan, planned, err := PlanOpenCodeProbe(provider, body, model)
	if err != nil {
		return nil, nil, "", err
	}
	if planned && plan.Responses {
		messages, _ := body["messages"].([]any)
		var tools []map[string]interface{}
		switch raw := body["tools"].(type) {
		case []map[string]interface{}:
			tools = raw
		case []any:
			for _, item := range raw {
				if tool, ok := item.(map[string]interface{}); ok {
					tools = append(tools, tool)
				}
			}
		}
		req, data, _, err := corellm.NewResponsesAPIRequest(ctx, plan.Config, messages, corellm.ResponsesAPIRequestOptions{
			Stream:     true,
			Tools:      tools,
			ToolChoice: body["tool_choice"],
			ExtraBody:  corellm.OpenCodeResponsesBudgetExtra(body),
		})
		req, data, err = finishMemberProbeRequest(req, data, err, true)
		return req, data, "responses", err
	}
	if !planned {
		return nil, nil, "", fmt.Errorf("opencode probe was not planned")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.Endpoint, bytes.NewReader(plan.Payload))
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if ua := cfg.UserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	corelib.ApplyOpenCodeSessionHeader(req, cfg)
	return req, plan.Payload, "openai", nil
}

// hopProbeOutput is the capped budget on a non-stream hop. That hop still
// forwards an OpenAI-shaped body, including Google. Anthropic and Responses
// keep the one field their converter sends.
func hopProbeOutput(provider *llmpool.ProviderConfig, chat map[string]any, spec ProviderChatTest, toolsSet bool) map[string]ProbeOutputField {
	cfg := WithMemberUpstreamOverrides(corelib.MaclawLLMConfig{
		URL:      provider.APIURL,
		Protocol: corelib.NormalizeLLMProviderProtocol(provider.Protocol),
		WireAPI:  corelib.NormalizeLLMProviderWireAPI(provider.WireAPI),
	}, provider)
	kind := ProbeOutputKind(cfg)
	if kind == "google" {
		kind = "openai"
	}
	baseline := ProbeOutputBaseline(kind, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields, spec.MaxTokens, spec.MaxCompletionTokens, toolsSet)
	wire := map[string]any{}
	switch kind {
	case "anthropic":
		if value, ok := chat["max_tokens"]; ok {
			wire["max_tokens"] = value
		} else if value, ok := chat["max_completion_tokens"]; ok {
			wire["max_tokens"] = value
		}
	case "responses":
		if value, ok := chat["max_completion_tokens"]; ok {
			wire["max_output_tokens"] = value
		} else if value, ok := chat["max_tokens"]; ok {
			wire["max_output_tokens"] = value
		}
	default:
		for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
			if value, ok := chat[key]; ok {
				wire[key] = value
			}
		}
	}
	corelib.FinishMemberUpstreamBody(cfg, wire)
	raw, err := json.Marshal(wire)
	if err != nil {
		return nil
	}
	return ProjectProbeOutput(raw, baseline)
}

func rawJSONValue(raw json.RawMessage) (any, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false
	}
	return parsed, true
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

func hopProviderSnapshot(provider *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if provider == nil {
		return nil
	}
	clones := cloneProviderConfigs([]llmpool.ProviderConfig{*provider})
	snap := clones[0]
	snap.WorkBuddySessionID = ""
	snap.QoderSessionID = ""
	snap.TraeSessionID = ""
	snap.LobsterAISessionID = ""
	snap.AllowedNodes = ""
	snap.ArrayName = ""
	return &snap
}

// hopSuppliedProvider is the credential from the accepting node when this
// egress node does not have the row yet. allowed is false when the snapshot
// names a real provider that this node must not dial.
func hopSuppliedProvider(supplied *llmpool.ProviderConfig, providerID, nodeID string) (*llmpool.ProviderConfig, bool) {
	if supplied == nil || strings.TrimSpace(supplied.APIURL) == "" {
		return nil, true
	}
	if !strings.EqualFold(strings.TrimSpace(supplied.ID), strings.TrimSpace(providerID)) {
		return nil, true
	}
	if !hopUpstreamURLAllowed(supplied.APIURL) || !hopSnapshotCanDial(supplied) {
		return nil, false
	}
	if len(llmpool.NormalizeAllowedNodeIDs(supplied.AllowedNodeIDs)) == 0 || !llmpool.ProviderAllowedOnNode(*supplied, nodeID) {
		return nil, false
	}
	return hopProviderSnapshot(supplied), true
}

// hopSnapshotCanDial reports whether a peer-supplied row has a credential.
// A URL alone must not make this node call an upstream it does not store.
func hopSnapshotCanDial(provider *llmpool.ProviderConfig) bool {
	if provider == nil {
		return false
	}
	if strings.TrimSpace(provider.APIKey) != "" {
		return true
	}
	return strings.TrimSpace(provider.WorkBuddyRefreshToken) != "" ||
		strings.TrimSpace(provider.QoderRefreshToken) != "" ||
		strings.TrimSpace(provider.TraeRefreshToken) != "" ||
		strings.TrimSpace(provider.LobsterAIRefreshToken) != ""
}

func sameHopAuthKind(dstKind, srcKind string) bool {
	normalize := func(kind string) string {
		kind = strings.TrimSpace(kind)
		if kind == llmpool.ProviderAuthAPIKey {
			return ""
		}
		return kind
	}
	return normalize(dstKind) == normalize(srcKind)
}

func hopUpstreamURLAllowed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func overlayHopCredentials(dst, src *llmpool.ProviderConfig) {
	if dst == nil || src == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(src.ID), strings.TrimSpace(dst.ID)) {
		return
	}
	// A stored key wins. The hop key only fills a row that does not have one
	// yet, which is the copy whose egress node has not received the secret.
	// The stored URL and auth kind stay as they are.
	if strings.TrimSpace(dst.APIKey) == "" {
		if key := strings.TrimSpace(src.APIKey); key != "" {
			dst.APIKey = key
		}
	}
	// The User-Agent is not a secret. Fill it when this node has not stored
	// one yet, and leave a stored choice in place.
	if strings.TrimSpace(dst.AgentType) == "" {
		if ua := RequestAgentType(src.AgentType); ua != "" {
			dst.AgentType = ua
		}
	}
	if !sameHopAuthKind(dst.AuthKind, src.AuthKind) {
		return
	}
	if len(dst.TokenBankExtraKeys) == 0 && len(src.TokenBankExtraKeys) > 0 {
		dst.TokenBankExtraKeys = append([]string(nil), src.TokenBankExtraKeys...)
	}
	if token := strings.TrimSpace(src.WorkBuddyRefreshToken); token != "" && strings.TrimSpace(dst.WorkBuddyRefreshToken) == "" {
		dst.WorkBuddyEdition = src.WorkBuddyEdition
		dst.WorkBuddyRefreshToken = token
		dst.WorkBuddyExpiresAt = src.WorkBuddyExpiresAt
		dst.WorkBuddyUserID = src.WorkBuddyUserID
		dst.WorkBuddyEnterpriseID = src.WorkBuddyEnterpriseID
		dst.WorkBuddyDomain = src.WorkBuddyDomain
	}
	if token := strings.TrimSpace(src.QoderRefreshToken); token != "" && strings.TrimSpace(dst.QoderRefreshToken) == "" {
		dst.QoderEdition = src.QoderEdition
		dst.QoderRefreshToken = token
		dst.QoderExpiresAt = src.QoderExpiresAt
		dst.QoderUserID = src.QoderUserID
		dst.QoderMachineID = src.QoderMachineID
	}
	if token := strings.TrimSpace(src.TraeRefreshToken); token != "" && strings.TrimSpace(dst.TraeRefreshToken) == "" {
		dst.TraeEdition = src.TraeEdition
		dst.TraeRefreshToken = token
		dst.TraeExpiresAt = src.TraeExpiresAt
		dst.TraeUserID = src.TraeUserID
		dst.TraeMachineID = src.TraeMachineID
		dst.TraeDeviceID = src.TraeDeviceID
	}
	if token := strings.TrimSpace(src.LobsterAIRefreshToken); token != "" && strings.TrimSpace(dst.LobsterAIRefreshToken) == "" {
		dst.LobsterAIRefreshToken = token
		dst.LobsterAIExpiresAt = src.LobsterAIExpiresAt
		dst.LobsterAIUserID = src.LobsterAIUserID
		dst.LobsterAISessionUUID = src.LobsterAISessionUUID
		dst.LobsterAIFirstKeyfrom = src.LobsterAIFirstKeyfrom
	}
}

func hopAdminJSON(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, kind, model string) ([]byte, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:       kind,
		ProviderID: provider.ID,
		Model:      model,
		Provider:   hopProviderSnapshot(provider),
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
	agentType := ""
	providerName := ""
	if provider != nil {
		agentType = provider.AgentType
		providerName = provider.Name
	}
	return ProbeProviderModels(ctx, &http.Client{Timeout: 20 * time.Second}, providerName, apiURL, apiKey, protocol, agentType)
}

// ProviderChatTest is one saved-member probe. Node empty uses this node when
// it is allowed, otherwise the first reachable allowed peer. Tools are sent
// as OpenAI chat tools. MaxTokens and MaxCompletionTokens are optional output
// budgets. Nil keeps the short probe default. A set value is sent through the
// same member output cap as a live forward.
type ProviderChatTest struct {
	Model               string
	Node                string
	Tools               json.RawMessage
	ToolChoice          json.RawMessage
	MaxTokens           *int64
	MaxCompletionTokens *int64
	Stream              bool
}

// ProviderChatTestResult is the probe outcome, including the node that sent it.
type ProviderChatTestResult struct {
	Reply     string
	Err       string
	LatencyMs int64
	Model     string
	Node      string
	ToolCalls []ProviderToolCall
	Output    map[string]ProbeOutputField
	Stream    bool
	StreamEnd *bool
}

// TestProviderChatWithScope tests locally when this node is in scope,
// otherwise hops to an allowed peer.
func TestProviderChatWithScope(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, model string) (reply string, errMsg string, latencyMs int64, testModel string) {
	result := TestProviderChat(ctx, cfg, provider, ProviderChatTest{Model: model})
	return result.Reply, result.Err, result.LatencyMs, result.Model
}

// TestProviderChat probes one saved member. A non-empty Node sends the probe
// from that peer instead of the node that received the admin request.
func TestProviderChat(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, spec ProviderChatTest) ProviderChatTestResult {
	result := ProviderChatTestResult{Model: strings.TrimSpace(spec.Model), Node: strings.TrimSpace(spec.Node)}
	if provider == nil {
		result.Err = "provider is required"
		return result
	}
	if result.Model == "" && len(provider.Models) > 0 {
		result.Model = provider.Models[0]
	}
	spec.Model = result.Model
	if result.Node != "" && (cfg == nil || !strings.EqualFold(result.Node, strings.TrimSpace(cfg.NodeID))) {
		body, err := hopMemberTest(ctx, cfg, provider, spec, result.Node)
		if err != nil {
			result.Err = err.Error()
			return result
		}
		return decodeHopMemberTest(body, result)
	}
	if result.Node == "" && providerNeedsUpstreamHop(cfg, provider) {
		var lastErr error
		for _, nodeID := range hopCandidateNodeIDs(cfg, provider) {
			body, err := hopMemberTest(ctx, cfg, provider, spec, nodeID)
			if err != nil {
				lastErr = err
				continue
			}
			result.Node = nodeID
			return decodeHopMemberTest(body, result)
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no allowed hubcenter node reachable for provider %s", provider.ID)
		}
		result.Err = lastErr.Error()
		return result
	}
	if cfg != nil && result.Node == "" {
		result.Node = strings.TrimSpace(cfg.NodeID)
	}
	spec.Model = result.Model
	probed := probeProviderChat(ctx, memberProbeHTTPClient(cfg, provider), provider, spec)
	result.Reply = probed.Reply
	result.Err = probed.Err
	result.LatencyMs = probed.LatencyMs
	result.ToolCalls = probed.Calls
	result.Output = probed.Output
	result.Stream = probed.Stream
	result.StreamEnd = probed.StreamEnd
	return result
}

func hopMemberTest(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, spec ProviderChatTest, nodeID string) ([]byte, error) {
	payload, err := json.Marshal(upstreamHopRequest{
		Kind:                upstreamHopKindTest,
		ProviderID:          provider.ID,
		Model:               spec.Model,
		Tools:               spec.Tools,
		ToolChoice:          spec.ToolChoice,
		MaxTokens:           spec.MaxTokens,
		MaxCompletionTokens: spec.MaxCompletionTokens,
		Stream:              spec.Stream,
		Provider:            hopProviderSnapshot(provider),
		ReportTestResult:    strings.TrimSpace(spec.Node) != "",
	})
	if err != nil {
		return nil, err
	}
	return hopJSONTimeout(ctx, cfg, nodeID, payload, MemberProbeContextTimeout(provider))
}

func decodeHopMemberTest(body []byte, result ProviderChatTestResult) ProviderChatTestResult {
	var out upstreamHopTestResponse
	if err := json.Unmarshal(body, &out); err != nil {
		result.Err = err.Error()
		return result
	}
	result.Reply = out.Reply
	result.LatencyMs = out.LatencyMs
	result.Model = firstNonEmpty(out.Model, result.Model)
	result.ToolCalls = out.ToolCalls
	result.Output = out.Output
	result.Stream = out.Stream
	result.StreamEnd = out.StreamEnd
	if !out.Success {
		result.Err = strings.TrimSpace(out.Error)
		if result.Err == "" {
			result.Err = "provider test failed"
		}
	}
	return result
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
