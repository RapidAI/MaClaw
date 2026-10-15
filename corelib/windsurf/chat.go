package windsurf

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	editorVersion = "2.5.0"
	authService   = "exa.auth_pb.AuthService/GetUserJwt"
	chatService   = "exa.api_server_pb.ApiServerService/GetChatMessage"
	maxToolDesc   = 6998

	cascadeEnableMCPTools = 245
	maxChatNewlines       = 100000
)

var jwtPattern = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{10,}={0,2}\.[A-Za-z0-9_-]+={0,2}\.[A-Za-z0-9_-]+={0,2}$`)

type chatMessage struct {
	role       string
	text       string
	toolCallID string
	toolCalls  []toolCall
	images     []chatImage
}

type chatImage struct {
	mime string
	data string
}

type toolCall struct {
	id, name, args string
}

type toolDef struct {
	name, description, parameters string
}

type chatRequest struct {
	model           string
	messages        []chatMessage
	tools           []toolDef
	maxTokens       int
	temperature     *float64
	disableParallel bool
	forcedTool      string
}

type chatEvent struct {
	kind             string
	text             string
	id               string
	name             string
	reason           string
	promptTokens     int
	completionTokens int
}

func buildMetadata(apiKey, userJWT, sessionID string, requestID uint64, triggerID string) []byte {
	now := time.Now()
	parts := [][]byte{
		encodeString(1, "windsurf"),
		encodeString(2, editorVersion),
		encodeString(3, apiKey),
		encodeString(4, "en"),
		encodeString(5, osName()),
		encodeString(7, editorVersion),
		encodeVarintField(9, requestID),
		encodeString(10, sessionID),
		encodeString(12, "windsurf"),
		encodeBytes(16, encodeTimestamp(now.Unix(), int64(now.Nanosecond()))),
		encodeString(25, triggerID),
		encodeString(26, "Unset"),
		encodeString(28, "windsurf"),
	}
	if userJWT != "" {
		parts = append(parts, encodeString(21, userJWT))
	}
	return bytes.Join(parts, nil)
}

func setDevinHeaders(req *http.Request, apiKey, contentType string) {
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connect-Protocol-Version", "1")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("X-Api-Key", strings.TrimSpace(apiKey))
	}
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	default:
		return "linux"
	}
}

func buildChatRequest(apiKey, userJWT, model, cascadeID, promptID, sessionID string, requestID uint64, triggerID string, req chatRequest) []byte {
	parts := [][]byte{encodeBytes(1, buildMetadata(apiKey, userJWT, sessionID, requestID, triggerID))}
	for _, msg := range collapseSystem(req.messages) {
		source := uint64(1)
		switch msg.role {
		case "assistant":
			source = 2
		case "tool":
			source = 4
		}
		parts = append(parts, encodeBytes(3, encodePrompt(msg, source)))
	}
	parts = append(parts, encodeVarintField(7, 5))
	parts = append(parts, encodeBytes(8, encodeCompletion(req)))
	parts = append(parts, encodeBytes(9, disableMCPExperiment()))
	for _, tool := range req.tools {
		if strings.TrimSpace(tool.name) == "" {
			continue
		}
		parts = append(parts, encodeBytes(10, encodeTool(tool)))
	}
	if req.disableParallel {
		parts = append(parts, encodeVarintField(11, 1))
	}
	if name := strings.TrimSpace(req.forcedTool); name != "" && toolOffered(req.tools, name) {
		parts = append(parts, encodeBytes(12, encodeString(2, name)))
	}
	parts = append(parts,
		encodeString(16, cascadeID),
		encodeString(17, promptID),
		encodeString(21, model),
	)
	return bytes.Join(parts, nil)
}

func collapseSystem(messages []chatMessage) []chatMessage {
	var pending []string
	var out []chatMessage
	flush := func(text string) string {
		if len(pending) == 0 {
			return text
		}
		wrapped := "<system>\n" + strings.Join(pending, "\n\n") + "\n</system>\n" + text
		pending = nil
		return wrapped
	}
	for _, msg := range messages {
		if msg.role == "system" || msg.role == "developer" {
			if strings.TrimSpace(msg.text) != "" {
				pending = append(pending, msg.text)
			}
			continue
		}
		if msg.role == "user" && len(pending) > 0 {
			msg.text = flush(msg.text)
		}
		out = append(out, msg)
	}
	if len(pending) > 0 {
		out = append(out, chatMessage{role: "user", text: flush("")})
	}
	return out
}

func encodePrompt(msg chatMessage, source uint64) []byte {
	text := msg.text
	parts := [][]byte{
		encodeVarintField(2, source),
		encodeString(3, text),
		encodeVarintField(4, uint64(max(1, len(text)/4))),
		encodeVarintField(5, 1),
	}
	if msg.toolCallID != "" {
		parts = append(parts, encodeString(7, msg.toolCallID))
	}
	for _, image := range msg.images {
		if strings.TrimSpace(image.data) == "" {
			continue
		}
		parts = append(parts, encodeBytes(10, bytes.Join([][]byte{
			encodeString(1, image.data),
			encodeString(2, image.mime),
		}, nil)))
	}
	for _, call := range msg.toolCalls {
		parts = append(parts, encodeBytes(6, bytes.Join([][]byte{
			encodeString(1, call.id),
			encodeString(2, call.name),
			encodeString(3, call.args),
		}, nil)))
	}
	return bytes.Join(parts, nil)
}

func toolOffered(tools []toolDef, name string) bool {
	for _, tool := range tools {
		if tool.name == name {
			return true
		}
	}
	return false
}

func encodeTool(tool toolDef) []byte {
	desc := trimToolDesc(tool.description)
	params := tool.parameters
	if strings.TrimSpace(params) == "" {
		params = "{}"
	}
	return bytes.Join([][]byte{
		encodeString(1, tool.name),
		encodeString(2, desc),
		encodeString(3, params),
	}, nil)
}

func trimToolDesc(desc string) string {
	const suffix = "\n…(truncated for cloud)"
	if len(desc) <= maxToolDesc {
		return desc
	}
	limit := maxToolDesc - len(suffix)
	if limit < 0 {
		limit = 0
	}
	if limit > len(desc) {
		limit = len(desc)
	}
	for limit > 0 && !utf8.RuneStart(desc[limit]) {
		limit--
	}
	return desc[:limit] + suffix
}

func disableMCPExperiment() []byte {
	return encodeBytes(2, encodeVarint(cascadeEnableMCPTools))
}

func mcpConfigError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "mcp configuration")
}

func explainDevin(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if mcpConfigError(err) {
		return msg + "。请在 Devin 设置里停用有问题的 MCP 服务器后再试"
	}
	return msg
}

func encodeCompletion(req chatRequest) []byte {
	maxOut := req.maxTokens
	if maxOut <= 0 {
		maxOut = 128000
	}
	temp := 0.4
	if req.temperature != nil {
		temp = *req.temperature
	}
	// Field 2 is max_tokens. Field 3 is max_newlines. Leaving it unset makes the
	// server stop at the first line, which is the autocomplete default.
	return bytes.Join([][]byte{
		encodeVarintField(1, 1),
		encodeVarintField(2, uint64(maxOut)),
		encodeVarintField(3, maxChatNewlines),
		encodeDouble(5, temp),
		encodeDouble(6, 0.95),
		encodeVarintField(7, 50),
		encodeDouble(8, 1),
		encodeDouble(11, 1),
	}, nil)
}

var devinStreamClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		DisableCompression:    true,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          8,
		ExpectContinueTimeout: time.Second,
	},
}

type sessionIDs struct {
	sessionID string
	cascadeID string
}

var (
	sessionMu    sync.Mutex
	sessionCache = map[string]sessionIDs{}
	jwtMu        sync.Mutex
	jwtCache     = map[string]cachedJWT{}
)

type cachedJWT struct {
	token     string
	expiresAt time.Time
}

func resetCaches() {
	sessionMu.Lock()
	sessionCache = map[string]sessionIDs{}
	sessionMu.Unlock()
	jwtMu.Lock()
	jwtCache = map[string]cachedJWT{}
	jwtMu.Unlock()
	resetDevinHosts()
}

func sessionFor(apiKey, host string) sessionIDs {
	key := host + "\n" + apiKey
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if ids, ok := sessionCache[key]; ok {
		return ids
	}
	ids := sessionIDs{sessionID: newID(), cascadeID: newID()}
	sessionCache[key] = ids
	return ids
}

func forEachChatEvent(ctx context.Context, apiServer, apiKey string, req chatRequest, fn func(chatEvent) error) error {
	body, err := openChat(ctx, apiServer, apiKey, req)
	if err != nil {
		return err
	}
	defer body.Close()
	return readChatEvents(body, fn)
}

func openChat(ctx context.Context, apiServer, apiKey string, req chatRequest) (io.ReadCloser, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("请先完成 Devin 登录")
	}
	if strings.TrimSpace(req.model) == "" {
		return nil, fmt.Errorf("请先选择 Devin 模型")
	}
	apiServer, apiKey = resolveAccess(ctx, apiServer, apiKey)
	primary := apiServer
	body, err := openChatOn(ctx, primary, apiKey, req)
	if err == nil {
		noteDevinHost(apiKey, primary)
		return body, nil
	}
	alt, ok := devinHostFallback(primary)
	if devinSessionKey(apiKey) || !ok || alt == primary || !devinAuthRejected(err) {
		return nil, err
	}
	body, err2 := openChatOn(ctx, alt, apiKey, req)
	if err2 != nil {
		return nil, err
	}
	noteDevinHost(apiKey, alt)
	return body, nil
}

func openChatOn(ctx context.Context, apiServer, apiKey string, req chatRequest) (io.ReadCloser, error) {
	if apiServer == "" || apiKey == "" {
		return nil, fmt.Errorf("请先完成 Devin 登录")
	}
	userJWT, err := userJWT(ctx, apiServer, apiKey)
	if err != nil {
		return nil, err
	}
	ids := sessionFor(apiKey, apiServer)
	body := buildChatRequest(apiKey, userJWT, req.model, ids.cascadeID, newID(), ids.sessionID, uint64(time.Now().UnixMilli()), newID(), req)
	framed, err := frameConnect(body, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiServer+"/"+chatService, bytes.NewReader(framed))
	if err != nil {
		return nil, err
	}
	setDevinHeaders(httpReq, apiKey, "application/connect+proto")
	httpReq.Header.Set("Connect-Content-Encoding", "gzip")
	httpReq.Header.Set("Connect-Accept-Encoding", "gzip")
	resp, err := devinStreamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Devin 对话失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("Devin 对话失败: %s", connectError(resp.StatusCode, raw))
	}
	return resp.Body, nil
}

func readChatEvents(body io.Reader, fn func(chatEvent) error) error {
	sawEOS := false
	sawData := false
	for {
		payload, eos, err := readConnectFrame(body)
		if err == io.EOF {
			break
		}
		// A frame that ends mid-header or mid-body is not a finished reply.
		// Treating it as success runs a tool whose arguments never arrived.
		if err == io.ErrUnexpectedEOF {
			return fmt.Errorf("Devin 对话中断: %w", err)
		}
		if err != nil {
			return fmt.Errorf("Devin 对话中断: %w", err)
		}
		if eos || (len(payload) > 0 && payload[0] == '{') {
			if eos {
				sawEOS = true
			}
			if msg := trailerError(payload); msg != "" {
				return fmt.Errorf("Devin 对话失败: %s", msg)
			}
			if eos {
				continue
			}
		}
		if len(payload) > 0 {
			sawData = true
		}
		events := decodeChatFrame(payload)
		for _, event := range events {
			if err := fn(event); err != nil {
				return err
			}
		}
	}
	if !sawEOS && !sawData {
		return fmt.Errorf("Devin 对话未正常结束")
	}
	return nil
}

func trailerError(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if text == "" || !strings.Contains(text, `"error"`) {
		return ""
	}
	return connectError(200, payload)
}

func decodeChatFrame(payload []byte) []chatEvent {
	fields, err := iterFields(payload)
	if err != nil {
		return nil
	}
	var events []chatEvent
	for _, f := range fields {
		switch {
		case f.num == 3 && f.wire == 2:
			if text := string(f.value.([]byte)); text != "" {
				events = append(events, chatEvent{kind: "text", text: text})
			}
		case f.num == 9 && f.wire == 2:
			if text := string(f.value.([]byte)); text != "" {
				events = append(events, chatEvent{kind: "reasoning", text: text})
			}
		case f.num == 6 && f.wire == 2:
			sub, err := iterFields(f.value.([]byte))
			if err != nil {
				continue
			}
			id := fieldString(sub, 1)
			name := fieldString(sub, 2)
			args := fieldString(sub, 3)
			if id != "" && name != "" {
				events = append(events, chatEvent{kind: "tool_start", id: id, name: name})
			}
			if args != "" {
				events = append(events, chatEvent{kind: "tool_args", id: id, text: args})
			}
		case f.num == 5 && f.wire == 0:
			v, _ := f.value.(uint64)
			reason := ""
			switch v {
			case 2, 6, 9, 12:
				reason = "stop"
			case 3, 5:
				reason = "length"
			case 10:
				reason = "tool_calls"
			case 11:
				reason = "content_filter"
			}
			if reason != "" {
				events = append(events, chatEvent{kind: "finish", reason: reason})
			}
		case f.num == 28 && f.wire == 2:
			if usage, ok := decodeUsage(f.value.([]byte)); ok {
				events = append(events, usage)
			}
		}
	}
	return events
}

func decodeUsage(buf []byte) (chatEvent, bool) {
	fields, err := iterFields(buf)
	if err != nil {
		return chatEvent{}, false
	}
	event := chatEvent{kind: "usage"}
	found := false
	for _, f := range fields {
		if f.num != 2 || f.wire != 2 {
			continue
		}
		entry, err := iterFields(f.value.([]byte))
		if err != nil {
			continue
		}
		metric := fieldString(entry, 5)
		value := 0
		if raw, ok := bytesField(entry, 4); ok {
			inner, err := iterFields(raw)
			if err == nil {
				for _, leaf := range inner {
					if leaf.num == 2 && leaf.wire == 5 {
						bits := binary.LittleEndian.Uint32(leaf.value.([]byte))
						value = int(math.Round(float64(math.Float32frombits(bits))))
					}
				}
			}
		}
		if metric == "" {
			continue
		}
		found = true
		switch metric {
		case "input_tokens":
			event.promptTokens = value
		case "output_tokens":
			event.completionTokens = value
		}
	}
	return event, found
}

func bytesField(fields []protoField, num int) ([]byte, bool) {
	for _, f := range fields {
		if f.num == num && f.wire == 2 {
			b, ok := f.value.([]byte)
			return b, ok
		}
	}
	return nil, false
}

func userJWT(ctx context.Context, apiServer, apiKey string) (string, error) {
	key := apiServer + "\n" + apiKey
	jwtMu.Lock()
	if cached, ok := jwtCache[key]; ok && time.Now().Add(time.Minute).Before(cached.expiresAt) {
		jwtMu.Unlock()
		return cached.token, nil
	}
	jwtMu.Unlock()

	body := encodeBytes(1, buildMetadata(apiKey, "", newID(), uint64(time.Now().UnixMilli()), newID()))
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiServer+"/"+authService, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setDevinHeaders(req, apiKey, "application/proto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Devin 会话校验失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Devin 会话校验失败: %s", connectError(resp.StatusCode, raw))
	}
	fields, err := iterFields(raw)
	if err != nil {
		return "", fmt.Errorf("Devin 会话校验失败")
	}
	token := fieldString(fields, 1)
	if !jwtPattern.MatchString(token) {
		return "", fmt.Errorf("Devin 会话校验未返回登录状态")
	}
	expires := time.Now().Add(10 * time.Minute)
	if exp, ok := jwtExpiry(token); ok {
		expires = exp
	}
	jwtMu.Lock()
	jwtCache[key] = cachedJWT{token: token, expiresAt: expires}
	jwtMu.Unlock()
	return token, nil
}

func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var payload struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(payload.Exp, 0), true
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
