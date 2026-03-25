package freeproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dangbeiAPIBase = "https://ai-api.dangbei.net/ai-search"

// dangbeiAgentID is the "程序猿助理" agent used for agentApi/v1/agentChat.
const dangbeiAgentID = "4479497474"

// DangbeiClient interacts with 当贝 AI's internal API.
type DangbeiClient struct {
	auth   *AuthStore
	client *http.Client
}

// NewDangbeiClient creates a new client backed by the given AuthStore.
func NewDangbeiClient(auth *AuthStore) *DangbeiClient {
	return &DangbeiClient{
		auth: auth,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// v1Sign computes the v1 request signature: MD5(timestamp + body + nonce).toUpperCase()
func v1Sign(timestamp int64, body string, nonce string) string {
	data := fmt.Sprintf("%d%s%s", timestamp, body, nonce)
	hash := md5.Sum([]byte(data))
	return strings.ToUpper(fmt.Sprintf("%x", hash))
}

// generateNonce generates a random string matching the JS nanoid implementation.
func generateNonce(length int) string {
	const chars = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// ── 当贝 API models ──

// DangbeiModel describes a model available on 当贝 AI.
type DangbeiModel struct {
	ID   string // model identifier used in API calls
	Name string // display name
}

// AvailableModels returns the known models on 当贝 AI.
func AvailableModels() []DangbeiModel {
	return []DangbeiModel{
		{ID: "deepseek_r1", Name: "DeepSeek-R1"},
		{ID: "deepseek_v3", Name: "DeepSeek-V3"},
		{ID: "glm_5", Name: "GLM-5"},
		{ID: "tongyi_235b", Name: "通义3-235B"},
		{ID: "kimi_k2_5", Name: "Kimi K2-5"},
		{ID: "doubao_seed", Name: "豆包 Seed"},
		{ID: "hunyuan_t1", Name: "混元 T1"},
		{ID: "qwen3_coder", Name: "Qwen3-Coder"},
		{ID: "deepseek_v3_0324", Name: "DeepSeek-V3-0324"},
		{ID: "step_2_16k", Name: "Step-2-16K"},
		{ID: "ernie_x1", Name: "文心 X1"},
	}
}

// ── Common headers ──

func (c *DangbeiClient) baseHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	h.Set("Accept", "*/*")
	h.Set("Content-Type", "application/json")
	h.Set("Origin", "https://ai.dangbei.com")
	h.Set("Referer", "https://ai.dangbei.com/")
	return h
}

func (c *DangbeiClient) authHeaders() http.Header {
	h := c.baseHeaders()
	cookie := c.auth.GetCookie()
	if cookie != "" {
		h.Set("Cookie", cookie)
		h.Set("token", extractToken(cookie))
	}
	h.Set("deviceid", "")
	return h
}

// sanitizeHeaderValue removes characters that are illegal in HTTP header values.
func sanitizeHeaderValue(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// ── API response envelope ──
// Real format: {"success":true,"errCode":null,"errMessage":null,"requestId":"...","data":{...}}

type apiResponse struct {
	Success    bool            `json:"success"`
	ErrCode    *string         `json:"errCode"`
	ErrMessage *string         `json:"errMessage"`
	RequestID  string          `json:"requestId"`
	Data       json.RawMessage `json:"data"`
}

func (r *apiResponse) ok() bool {
	return r.Success
}

func (r *apiResponse) errMsg() string {
	if r.ErrMessage != nil && *r.ErrMessage != "" {
		return *r.ErrMessage
	}
	if r.ErrCode != nil && *r.ErrCode != "" {
		return *r.ErrCode
	}
	return fmt.Sprintf("success=%v", r.Success)
}

// ── Create conversation ──

// CreateSession creates a new agent conversation and returns the conversation ID.
// Uses agentApi/v1/create with the configured agent ID.
func (c *DangbeiClient) CreateSession(ctx context.Context) (string, error) {
	body := []byte(fmt.Sprintf(`{"agentId":"%s"}`, dangbeiAgentID))
	ar, err := c.apiPost(ctx, "/agentApi/v1/create", body)
	if err != nil {
		return "", fmt.Errorf("create agent conversation: %w", err)
	}
	if !ar.ok() {
		return "", fmt.Errorf("create agent conversation: %s", ar.errMsg())
	}

	var data struct {
		ConversationID string `json:"conversationId"`
	}
	if err := json.Unmarshal(ar.Data, &data); err != nil {
		return "", fmt.Errorf("parse conversation data: %w", err)
	}
	if data.ConversationID == "" {
		return "", fmt.Errorf("empty conversationId returned")
	}
	return data.ConversationID, nil
}

// DeleteSession deletes a conversation. Best-effort; errors are logged but not fatal.
func (c *DangbeiClient) DeleteSession(ctx context.Context, conversationID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		dangbeiAPIBase+"/conversationApi/v1/delete?conversationId="+conversationID, nil)
	if err != nil {
		return err
	}
	req.Header = c.authHeaders()
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain body so the connection can be reused
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// apiPost is a helper that POSTs JSON to a 当贝 API path and decodes the envelope.
// It adds v1 MD5 signing headers automatically.
func (c *DangbeiClient) apiPost(ctx context.Context, path string, body []byte) (*apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dangbeiAPIBase+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = c.authHeaders()

	// Add v1 signing headers
	for k, v := range v1SignHeaders(string(body)) {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var ar apiResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return &ar, nil
}

// ── Chat completion (chunked streaming) ──

// CompletionRequest is the payload for agentApi/v1/agentChat.
type CompletionRequest struct {
	ConversationID string // conversation to chat in
	Prompt         string // user question
	ModelClass     string // model identifier (reserved for future use)
}

// extractToken extracts the "token" value from a raw cookie string.
func extractToken(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == "token" {
			return kv[1]
		}
	}
	return ""
}

// v1SignHeaders returns the v1 MD5 signing headers for the given request body.
func v1SignHeaders(bodyStr string) map[string]string {
	timestamp := time.Now().Unix()
	nonce := generateNonce(21)
	sign := v1Sign(timestamp, bodyStr, nonce)
	return map[string]string{
		"sign":       sign,
		"nonce":      nonce,
		"timestamp":  fmt.Sprintf("%d", timestamp),
		"appType":    "6",
		"lang":       "zh",
		"client-ver": "1.0.2",
		"appVersion": "1.3.9",
		"version":    "v1",
	}
}

// StreamCompletion sends a chat request via agentApi/v1/agentChat and streams
// back tokens via the callback. Uses the "程序猿助理" agent with v1 MD5 signing.
// Returns the full accumulated text and the last message ID.
func (c *DangbeiClient) StreamCompletion(ctx context.Context, cr CompletionRequest, onToken func(token string)) (fullText string, msgID string, err error) {
	// Build JSON body. Include modelCode when a specific model is requested.
	modelPart := ""
	if cr.ModelClass != "" && cr.ModelClass != "free-proxy" {
		modelPart = fmt.Sprintf(`,"modelCode":%s`, mustJSON(cr.ModelClass))
	}
	body := []byte(fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":%s,"question":%s,"agentId":"%s"%s}`,
		mustJSON(cr.ConversationID), mustJSON(cr.Prompt), dangbeiAgentID, modelPart,
	))
	bodyStr := string(body)

	cookie := c.auth.GetCookie()
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"token":        extractToken(cookie),
		"deviceid":     "",
		"connection":   "keep-alive",
	}
	for k, v := range v1SignHeaders(bodyStr) {
		headers[k] = v
	}

	// Determine timeout from context
	timeout := 60 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}

	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/agentApi/v1/agentChat", headers, body, timeout)
	if err != nil {
		return "", "", fmt.Errorf("raw post: %w", err)
	}
	defer raw.Close()

	if raw.StatusCode != http.StatusOK {
		bodyReader := raw.Body()
		errBody, _ := io.ReadAll(io.LimitReader(bodyReader, 4096))
		bodyReader.Close()

		// Dump full diagnostics to temp file for debugging
		homeDir, _ := os.UserHomeDir()
		logDir := filepath.Join(homeDir, ".maclaw", "logs")
		os.MkdirAll(logDir, 0755)
		dumpFile := filepath.Join(logDir, fmt.Sprintf("freeproxy_err_%d.log", time.Now().UnixMilli()))
		var dump strings.Builder
		dump.WriteString(fmt.Sprintf("=== freeproxy error dump ===\nTime: %s\nHTTP Status: %d\n", time.Now().Format(time.RFC3339), raw.StatusCode))
		dump.WriteString(fmt.Sprintf("\n--- Request URL ---\nPOST https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat\n"))
		dump.WriteString(fmt.Sprintf("\n--- Request Headers ---\n"))
		for k, v := range headers {
			if k == "cookie" {
				// mask cookie value, keep first/last 10 chars
				masked := v
				if len(v) > 24 {
					masked = v[:10] + "..." + v[len(v)-10:]
				}
				dump.WriteString(fmt.Sprintf("  %s: %s\n", k, masked))
			} else {
				dump.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
			}
		}
		dump.WriteString(fmt.Sprintf("\n--- Request Body (len=%d) ---\n%s\n", len(body), string(body)))
		dump.WriteString(fmt.Sprintf("\n--- Response Body ---\n%s\n", string(errBody)))
		os.WriteFile(dumpFile, []byte(dump.String()), 0644)
		log.Printf("[freeproxy] chat HTTP %d, diagnostics dumped to %s", raw.StatusCode, dumpFile)

		var ar apiResponse
		if json.Unmarshal(errBody, &ar) == nil && !ar.ok() {
			return "", "", fmt.Errorf("chat HTTP %d: %s (see %s)", raw.StatusCode, ar.errMsg(), dumpFile)
		}
		return "", "", fmt.Errorf("chat HTTP %d: %s (see %s)", raw.StatusCode, string(errBody), dumpFile)
	}

	bodyReader := raw.Body()
	defer bodyReader.Close()
	return c.parseDangbeiStream(bodyReader, onToken)
}

// parseDangbeiStream reads the SSE streaming response from agentApi/v1/agentChat.
// Format: lines like "data:{json}" with event types.
// The JSON payload has: {id, type, content, content_type, ...}
// It filters out agent function-call markers (<|FunctionCallBegin|>...<|FunctionCallEnd|>).
func (c *DangbeiClient) parseDangbeiStream(r io.Reader, onToken func(string)) (string, string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var fullText strings.Builder
	var lastMsgID string
	inFuncCall := false // true while inside a FunctionCall block

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		if data == "[DONE]" {
			break
		}

		var evt dangbeiStreamEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		if evt.ID != "" {
			lastMsgID = evt.ID
		}

		// Only collect text content — skip thinking, progress, card, etc.
		if evt.ContentType != "text" && evt.ContentType != "" {
			continue
		}
		if evt.Content == "" {
			continue
		}

		// Filter out ◁FunctionCallBegin▷...◁FunctionCallEnd▷ blocks.
		cleaned := filterFunctionCalls(evt.Content, &inFuncCall)
		if cleaned == "" {
			continue
		}

		fullText.WriteString(cleaned)
		if onToken != nil {
			onToken(cleaned)
		}
	}

	if err := scanner.Err(); err != nil {
		return fullText.String(), lastMsgID, fmt.Errorf("stream read error: %w", err)
	}
	return fullText.String(), lastMsgID, nil
}

const (
	funcCallBegin = "<|FunctionCallBegin|>"
	funcCallEnd   = "<|FunctionCallEnd|>"
)

// filterFunctionCalls strips <|FunctionCallBegin|>...<|FunctionCallEnd|> from s.
// *inBlock tracks state across chunks: true means we're inside a block to discard.
func filterFunctionCalls(s string, inBlock *bool) string {
	var out strings.Builder
	for len(s) > 0 {
		if *inBlock {
			// Look for end marker
			if idx := strings.Index(s, funcCallEnd); idx >= 0 {
				s = s[idx+len(funcCallEnd):]
				*inBlock = false
				continue
			}
			// Entire chunk is inside the block — discard all
			return out.String()
		}
		// Look for begin marker
		if idx := strings.Index(s, funcCallBegin); idx >= 0 {
			out.WriteString(s[:idx])
			s = s[idx+len(funcCallBegin):]
			*inBlock = true
			continue
		}
		out.WriteString(s)
		break
	}
	return out.String()
}

// dangbeiStreamEvent represents a single SSE data chunk from the agent chat stream.
type dangbeiStreamEvent struct {
	ID          string `json:"id"`
	Type        string `json:"type"`         // "answer", etc.
	Content     string `json:"content"`      // the actual text chunk
	ContentType string `json:"content_type"` // "text", "thinking", "progress", "card", etc.
}

// IsAuthenticated checks if the current auth cookie is valid by making a
// lightweight API call (getUserInfo).
func (c *DangbeiClient) IsAuthenticated(ctx context.Context) bool {
	ar, err := c.apiPost(ctx, "/userInfoApi/v1/getUserInfo", []byte("{}"))
	if err != nil {
		return false
	}
	return ar.ok()
}

// mustJSON encodes a string as a JSON string value (with quotes and escaping).
func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
