package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const demoMaxDownloadBytes = 200 * 1024 * 1024

type App struct {
	ctx context.Context
}

type GatewayConfig struct {
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	ClientID       string `json:"clientId"`
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
	UserName       string `json:"userName"`
}

type ConnectInput struct {
	GatewayConfig
}

type ConnectResult struct {
	Config    GatewayConfig `json:"config"`
	Handshake any           `json:"handshake"`
	Cursor    string        `json:"cursor"`
}

type SendInput struct {
	GatewayConfig
	Text        string            `json:"text"`
	MessageType string            `json:"messageType,omitempty"`
	Attachments []MediaAttachment `json:"attachments,omitempty"`
}

type PollInput struct {
	GatewayConfig
	Cursor  string `json:"cursor"`
	Timeout int    `json:"timeout"`
	Limit   int    `json:"limit"`
}

type HandshakeRequest = coreim.ThirdPartyHandshakeRequest
type IncomingRequest = coreim.ThirdPartyIncomingRequest
type UserRef = coreim.ThirdPartyUserRef
type MessagePayload = coreim.ThirdPartyMessagePayload
type MediaAttachment = coreim.ThirdPartyMediaReference
type MediaPrepareRequest = coreim.ThirdPartyMediaPrepareRequest
type MediaPrepareResponse = coreim.ThirdPartyMediaPrepareResponse
type ToolDefinition = coreim.ThirdPartyToolDefinition
type ToolCall = coreim.ThirdPartyToolCall
type ToolPlan = coreim.ThirdPartyToolPlan
type ToolPlanStep = coreim.ThirdPartyToolPlanStep
type ToolResultRequest = coreim.ThirdPartyToolResultRequest
type ToolError = coreim.ThirdPartyToolError
type IncomingResponse = coreim.ThirdPartyIncomingAcceptedResponse
type OutgoingResponse = coreim.ThirdPartyOutgoingPollResponse
type OutgoingMessage = coreim.ThirdPartyOutgoingMessage

type DownloadInput struct {
	GatewayConfig
	URL      string `json:"url"`
	FileName string `json:"fileName,omitempty"`
}

type DownloadResult struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType,omitempty"`
}

type ToolExecuteInput struct {
	GatewayConfig
	Message OutgoingMessage `json:"message"`
}

type ToolExecuteResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type AckRequest = coreim.ThirdPartyAckRequest

type gatewayAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type gatewayErrorEnvelope struct {
	OK        bool             `json:"ok"`
	RequestID string           `json:"requestId,omitempty"`
	Error     *gatewayAPIError `json:"error,omitempty"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) Connect(input ConnectInput) (*ConnectResult, error) {
	cfg, err := normalizeConfig(input.GatewayConfig)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), 20*time.Second)
	defer cancel()

	var hs map[string]any
	err = doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/handshake", cfg.APIKey, HandshakeRequest{
		ClientID:        cfg.ClientID,
		ClientName:      "ThirdAPIDemo Wails chat",
		ProtocolVersion: coreim.ThirdPartyProtocolVersion,
		Capabilities:    coreim.ThirdPartyCapabilityMap(),
		Tools:           demoToolDefinitions(),
	}, &hs)
	if err != nil {
		return nil, err
	}
	return &ConnectResult{Config: cfg, Handshake: hs, Cursor: "0"}, nil
}

func (a *App) Send(input SendInput) (*IncomingResponse, error) {
	cfg, err := normalizeConfig(input.GatewayConfig)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(input.Text)
	attachments := normalizeMediaAttachments(input.Attachments)
	messageType := normalizeMessageType(input.MessageType, attachments)
	if text == "" && len(attachments) == 0 {
		return nil, errors.New("message text or attachment is required")
	}
	now := time.Now().UnixMilli()
	messageID := fmt.Sprintf("third_api_demo_%d", now)
	payload := MessagePayload{Type: messageType, Text: text, Attachments: attachments}
	if len(attachments) == 1 {
		payload.ID = attachments[0].ID
		payload.FileName = attachments[0].FileName
		payload.MimeType = attachments[0].MimeType
		payload.Data = attachments[0].Data
		payload.URL = attachments[0].URL
		payload.SizeBytes = attachments[0].SizeBytes
		payload.DurationMs = attachments[0].DurationMs
		payload.Attachments = nil
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), 30*time.Second)
	defer cancel()

	var in IncomingResponse
	err = doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/incoming", cfg.APIKey, IncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        "evt_" + messageID,
		MessageID:      messageID,
		ConversationID: cfg.ConversationID,
		User:           UserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        payload,
		CreatedAt:      now,
	}, &in)
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (a *App) Poll(input PollInput) (*OutgoingResponse, error) {
	cfg, err := normalizeConfig(input.GatewayConfig)
	if err != nil {
		return nil, err
	}
	if input.Cursor == "" {
		input.Cursor = "0"
	}
	if input.Timeout <= 0 || input.Timeout > 60 {
		input.Timeout = 25
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), time.Duration(input.Timeout+10)*time.Second)
	defer cancel()

	var out OutgoingResponse
	outURL := fmt.Sprintf("%s/outgoing?clientId=%s&cursor=%s&timeout=%d&limit=%d",
		cfg.BaseURL, url.QueryEscape(cfg.ClientID), url.QueryEscape(input.Cursor), input.Timeout, input.Limit)
	if err := doGatewayJSON(ctx, http.MethodGet, outURL, cfg.APIKey, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) > 0 {
		ids := make([]string, 0, len(out.Messages))
		for _, msg := range out.Messages {
			if strings.TrimSpace(msg.ID) != "" {
				ids = append(ids, msg.ID)
			}
		}
		if len(ids) > 0 {
			var ackResp map[string]any
			_ = doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/ack", cfg.APIKey, AckRequest{
				ClientID:   cfg.ClientID,
				MessageIDs: ids,
				Status:     "delivered",
			}, &ackResp)
		}
	}
	return &out, nil
}

func (a *App) ExecuteToolMessage(input ToolExecuteInput) (*ToolExecuteResult, error) {
	cfg, err := normalizeConfig(input.GatewayConfig)
	if err != nil {
		return nil, err
	}
	msg := input.Message
	if err := coreim.NormalizeThirdPartyOutgoingMessage(&msg); err != nil {
		return nil, err
	}
	switch msg.Type {
	case "tool_call":
		if msg.ToolCall == nil {
			return nil, errors.New("toolCall is required")
		}
		result := executeDemoToolCall(*msg.ToolCall)
		result.ClientID = cfg.ClientID
		result.ConversationID = firstNonEmptyString(msg.ConversationID, cfg.ConversationID)
		if err := a.submitToolResult(cfg, result); err != nil {
			return nil, err
		}
		return &ToolExecuteResult{OK: true, Message: "tool result submitted"}, nil
	case "tool_plan":
		if msg.ToolPlan == nil {
			return nil, errors.New("toolPlan is required")
		}
		if err := coreim.NormalizeThirdPartyToolPlan(msg.ToolPlan); err != nil {
			return nil, err
		}
		results := executeDemoToolPlan(*msg.ToolPlan)
		for _, result := range results {
			result.ClientID = cfg.ClientID
			result.ConversationID = firstNonEmptyString(msg.ConversationID, cfg.ConversationID)
			if err := a.submitToolResult(cfg, result); err != nil {
				return nil, err
			}
		}
		return &ToolExecuteResult{OK: true, Message: fmt.Sprintf("%d tool plan result(s) submitted", len(results))}, nil
	default:
		return nil, errors.New("message type must be tool_call or tool_plan")
	}
}

func (a *App) SelectUploadFiles(input GatewayConfig) ([]MediaAttachment, error) {
	if a.ctx == nil {
		return nil, errors.New("app is not ready")
	}
	cfg, err := normalizeConfig(input)
	if err != nil {
		return nil, err
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select files to send",
	})
	if err != nil {
		return nil, err
	}
	out := make([]MediaAttachment, 0, len(paths))
	for _, p := range paths {
		upload, err := a.uploadFileToGateway(cfg, p)
		if err != nil {
			return nil, err
		}
		out = append(out, upload)
	}
	return out, nil
}

func (a *App) Download(input DownloadInput) (*DownloadResult, error) {
	cfg, err := normalizeConfig(input.GatewayConfig)
	if err != nil {
		return nil, err
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return nil, errors.New("download url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("download url must be absolute")
	}
	if !isHTTPURLScheme(parsed.Scheme) {
		return nil, errors.New("download url must use http or https")
	}
	if !isGatewayMediaDownloadURL(cfg.BaseURL, rawURL) {
		return nil, errors.New("download url must be a gateway media download URL")
	}
	fileName := safeFileName(defaultString(input.FileName, filepath.Base(parsed.Path)))
	if fileName == "." || fileName == "" {
		fileName = "download.bin"
	}
	savePath, err := wailsruntime.SaveFileDialog(a.requestContext(), wailsruntime.SaveDialogOptions{
		Title:           "Save attachment",
		DefaultFilename: fileName,
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(savePath) == "" {
		return nil, errors.New("download canceled")
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > demoMaxDownloadBytes {
		return nil, fmt.Errorf("download exceeds %d bytes", demoMaxDownloadBytes)
	}
	tmp := savePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, demoMaxDownloadBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return nil, closeErr
	}
	if written > demoMaxDownloadBytes {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("download exceeds %d bytes", demoMaxDownloadBytes)
	}
	if err := os.Rename(tmp, savePath); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return &DownloadResult{Path: savePath, Bytes: written, FileName: filepath.Base(savePath), MimeType: resp.Header.Get("Content-Type")}, nil
}

func (a *App) submitToolResult(cfg GatewayConfig, result ToolResultRequest) error {
	if result.CreatedAt == 0 {
		result.CreatedAt = time.Now().UnixMilli()
	}
	if result.ResultID == "" {
		result.ResultID = demoToolResultID(result)
	}
	if result.IdempotencyKey == "" {
		result.IdempotencyKey = result.ResultID
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), 30*time.Second)
	defer cancel()
	var out map[string]any
	return doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/tool-result", cfg.APIKey, result, &out)
}

func (a *App) requestContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) uploadFileToGateway(cfg GatewayConfig, path string) (MediaAttachment, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return MediaAttachment{}, err
	}
	if info.IsDir() {
		return MediaAttachment{}, fmt.Errorf("%s is a directory", path)
	}
	fileName := safeFileName(info.Name())
	mimeType := detectMimeType(path)
	mediaType := mediaTypeForMime(fileName, mimeType)
	if info.Size() <= int64(coreim.ThirdPartyMaxDirectBytes) {
		data, err := os.ReadFile(path)
		if err != nil {
			return MediaAttachment{}, err
		}
		return MediaAttachment{
			Type:      mediaType,
			FileName:  fileName,
			MimeType:  mimeType,
			Data:      base64.StdEncoding.EncodeToString(data),
			SizeBytes: info.Size(),
		}, nil
	}
	ctx, cancel := context.WithTimeout(a.requestContext(), 30*time.Second)
	defer cancel()

	var prepared MediaPrepareResponse
	if err := doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/media/upload-url", cfg.APIKey, MediaPrepareRequest{
		ClientID:  cfg.ClientID,
		Type:      mediaType,
		FileName:  fileName,
		MimeType:  mimeType,
		SizeBytes: info.Size(),
	}, &prepared); err != nil {
		return MediaAttachment{}, err
	}
	if strings.TrimSpace(prepared.Upload.URL) == "" {
		return MediaAttachment{}, errors.New("gateway did not return upload url")
	}
	if err := uploadFileToURL(ctx, cfg.BaseURL, prepared.Upload.URL, cfg.APIKey, path, mimeType); err != nil {
		return MediaAttachment{}, err
	}
	media := prepared.Media
	if media.Type == "" {
		media.Type = mediaType
	}
	if media.FileName == "" {
		media.FileName = fileName
	}
	if media.MimeType == "" {
		media.MimeType = mimeType
	}
	if media.SizeBytes == 0 {
		media.SizeBytes = info.Size()
	}
	return media, nil
}

func demoToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "demo.echo",
			Description: "Return the supplied arguments.",
			Risk:        "read",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
		},
		{
			Name:        "demo.get_time",
			Description: "Return local demo client time.",
			Risk:        "read",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"timezone": map[string]any{"type": "string"}},
			},
		},
		{
			Name:        "plc.read_register",
			Description: "Simulated read-only industrial register read.",
			Risk:        "read",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"address": map[string]any{"type": "string"}},
				"required":   []string{"address"},
			},
		},
	}
}

func executeDemoToolCall(call ToolCall) ToolResultRequest {
	if err := coreim.NormalizeThirdPartyToolCall(&call); err != nil {
		return rejectedToolResult(call.ID, "", err.Error())
	}
	result := executeDemoTool(call.Name, call.Arguments)
	result.ToolCallID = call.ID
	return result
}

func executeDemoToolPlanStep(planID string, step ToolPlanStep) ToolResultRequest {
	if err := coreim.NormalizeThirdPartyToolPlanStep(&step, 0); err != nil {
		return rejectedToolResult("", planID, err.Error())
	}
	result := executeDemoTool(step.Tool, step.Arguments)
	result.ToolPlanID = planID
	result.StepID = step.ID
	return result
}

func executeDemoToolPlan(plan ToolPlan) []ToolResultRequest {
	if plan.Mode != "sequential" && plan.Mode != "dag" {
		return []ToolResultRequest{{
			ToolPlanID: plan.ID,
			Status:     "rejected",
			Error:      &ToolError{Code: "unsupported_plan_mode", Message: "ThirdAPIDemo only executes sequential or dag tool plans"},
		}}
	}
	if plan.Mode == "dag" {
		return executeDemoToolPlanDAG(plan)
	}
	results := make([]ToolResultRequest, 0, len(plan.Steps))
	statusByStep := map[string]string{}
	for _, step := range plan.Steps {
		blocked := false
		for _, dep := range step.DependsOn {
			if statusByStep[dep] != "success" {
				blocked = true
				break
			}
		}
		if blocked {
			result := ToolResultRequest{
				ToolPlanID: plan.ID,
				StepID:     step.ID,
				Status:     "rejected",
				Error:      &ToolError{Code: "dependency_not_successful", Message: "one or more dependencies did not complete successfully"},
			}
			results = append(results, result)
			statusByStep[step.ID] = result.Status
			continue
		}
		result := executeDemoToolPlanStep(plan.ID, step)
		results = append(results, result)
		statusByStep[step.ID] = result.Status
		if plan.Mode == "sequential" && result.Status != "success" {
			break
		}
	}
	if len(results) == 0 {
		return []ToolResultRequest{{ToolPlanID: plan.ID, Status: "rejected", Error: &ToolError{Code: "empty_plan", Message: "tool plan has no executable steps"}}}
	}
	return results
}

func executeDemoToolPlanDAG(plan ToolPlan) []ToolResultRequest {
	results := make([]ToolResultRequest, 0, len(plan.Steps))
	statusByStep := map[string]string{}
	pending := map[string]bool{}
	for _, step := range plan.Steps {
		pending[step.ID] = true
	}
	for len(pending) > 0 {
		progress := false
		for _, step := range plan.Steps {
			if !pending[step.ID] {
				continue
			}
			blocked := false
			ready := true
			for _, dep := range step.DependsOn {
				status := statusByStep[dep]
				if status == "" {
					ready = false
					break
				}
				if status != "success" {
					blocked = true
					break
				}
			}
			if !ready {
				continue
			}
			var result ToolResultRequest
			if blocked {
				result = ToolResultRequest{
					ToolPlanID: plan.ID,
					StepID:     step.ID,
					Status:     "rejected",
					Error:      &ToolError{Code: "dependency_not_successful", Message: "one or more dependencies did not complete successfully"},
				}
			} else {
				result = executeDemoToolPlanStep(plan.ID, step)
			}
			results = append(results, result)
			statusByStep[step.ID] = result.Status
			delete(pending, step.ID)
			progress = true
		}
		if !progress {
			for _, step := range plan.Steps {
				if !pending[step.ID] {
					continue
				}
				result := ToolResultRequest{
					ToolPlanID: plan.ID,
					StepID:     step.ID,
					Status:     "rejected",
					Error:      &ToolError{Code: "dependency_not_ready", Message: "dependencies could not be satisfied"},
				}
				results = append(results, result)
				statusByStep[step.ID] = result.Status
				delete(pending, step.ID)
			}
		}
	}
	if len(results) == 0 {
		return []ToolResultRequest{{ToolPlanID: plan.ID, Status: "rejected", Error: &ToolError{Code: "empty_plan", Message: "tool plan has no executable steps"}}}
	}
	return results
}

func executeDemoTool(name string, args map[string]any) ToolResultRequest {
	name = coreim.NormalizeThirdPartyToolName(name)
	switch name {
	case "demo.echo":
		return ToolResultRequest{Status: "success", Text: "echo complete", Result: map[string]any{"arguments": args}}
	case "demo.get_time":
		tz := stringArg(args, "timezone")
		now := time.Now()
		if tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				now = now.In(loc)
			}
		}
		return ToolResultRequest{Status: "success", Text: now.Format(time.RFC3339), Result: map[string]any{"time": now.Format(time.RFC3339), "unixMillis": now.UnixMilli()}}
	case "plc.read_register":
		address := stringArg(args, "address")
		if address == "" {
			return ToolResultRequest{Status: "error", Error: &ToolError{Code: "missing_address", Message: "address is required"}}
		}
		return ToolResultRequest{Status: "success", Text: "simulated register read", Result: map[string]any{"address": address, "value": 42, "simulated": true}}
	default:
		return ToolResultRequest{Status: "rejected", Error: &ToolError{Code: "tool_not_allowed", Message: "tool is not in ThirdAPIDemo allowlist"}}
	}
}

func rejectedToolResult(toolCallID, toolPlanID, message string) ToolResultRequest {
	return ToolResultRequest{ToolCallID: toolCallID, ToolPlanID: toolPlanID, Status: "rejected", Error: &ToolError{Code: "invalid_tool_request", Message: message}}
}

func demoToolResultID(result ToolResultRequest) string {
	parts := []string{"demo-result", result.ClientID, result.ConversationID, result.ToolCallID, result.ToolPlanID, result.StepID, coreim.NormalizeThirdPartyToolStatus(result.Status)}
	joined := strings.Join(parts, ":")
	normalized := coreim.NormalizeThirdPartyID(joined)
	if normalized == "" {
		return fmt.Sprintf("demo-result-%d", time.Now().UnixMilli())
	}
	return normalized
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if value, ok := args[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func normalizeConfig(cfg GatewayConfig) (GatewayConfig, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.ClientID = normalizeID(defaultString(cfg.ClientID, "third-api-demo"))
	cfg.ConversationID = normalizeID(defaultString(cfg.ConversationID, "demo"))
	cfg.UserID = normalizeID(defaultString(cfg.UserID, "demo-user"))
	cfg.UserName = strings.TrimSpace(defaultString(cfg.UserName, "Demo User"))
	if cfg.BaseURL == "" {
		return cfg, errors.New("url is required")
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return cfg, errors.New("url must be an absolute URL")
	}
	if !isHTTPURLScheme(parsed.Scheme) {
		return cfg, errors.New("url must use http or https")
	}
	if cfg.APIKey == "" {
		return cfg, errors.New("apikey is required")
	}
	return cfg, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeMessageType(value string, attachments []MediaAttachment) string {
	value = coreim.NormalizeThirdPartyMessageType(value)
	switch value {
	case "image", "file", "voice":
		return value
	}
	if len(attachments) > 0 {
		return attachments[0].Type
	}
	return "text"
}

func normalizeMediaAttachments(items []MediaAttachment) []MediaAttachment {
	out := make([]MediaAttachment, 0, len(items))
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Type = coreim.NormalizeThirdPartyMessageType(item.Type)
		if item.Type == "" {
			item.Type = "file"
		}
		item.FileName = strings.TrimSpace(item.FileName)
		item.MimeType = strings.TrimSpace(item.MimeType)
		item.URL = strings.TrimSpace(item.URL)
		item.Data = strings.TrimSpace(item.Data)
		if item.ID == "" && item.URL == "" && item.Data == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func detectMimeType(path string) string {
	if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
		if typ := mime.TypeByExtension(ext); typ != "" {
			return typ
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n > 0 {
		return http.DetectContentType(buf[:n])
	}
	return "application/octet-stream"
}

func mediaTypeForMime(fileName, mimeType string) string {
	mimeType = strings.ToLower(mimeType)
	ext := strings.ToLower(filepath.Ext(fileName))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "voice"
	case ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp":
		return "image"
	case ext == ".wav" || ext == ".mp3" || ext == ".ogg" || ext == ".opus" || ext == ".m4a" || ext == ".aac" || ext == ".silk":
		return "voice"
	default:
		return "file"
	}
}

func safeFileName(value string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return "file"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(value)
}

func sameGatewayHost(baseURL, targetURL string) bool {
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(base.Scheme, target.Scheme) && strings.EqualFold(base.Host, target.Host)
}

func isGatewayMediaDownloadURL(baseURL, targetURL string) bool {
	return isGatewayMediaURL(baseURL, targetURL, false)
}

func isGatewayMediaUploadURL(baseURL, targetURL string) bool {
	return isGatewayMediaURL(baseURL, targetURL, true)
}

func isGatewayMediaURL(baseURL, targetURL string, upload bool) bool {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	target, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return false
	}
	if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) {
		return false
	}
	basePath := strings.TrimRight(base.Path, "/")
	if basePath == "" {
		basePath = "/api/im-gateway/v1"
	}
	prefix := basePath + "/media/"
	if !strings.HasPrefix(target.Path, prefix) {
		return false
	}
	rest := strings.Trim(strings.TrimPrefix(target.Path, prefix), "/")
	if rest == "" {
		return false
	}
	if upload {
		return strings.Count(rest, "/") == 1 && strings.HasSuffix(rest, "/upload") && strings.TrimSpace(target.Query().Get("mediaToken")) != ""
	}
	return !strings.Contains(rest, "/") && strings.TrimSpace(target.Query().Get("mediaToken")) != ""
}

func isHTTPURLScheme(scheme string) bool {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	return scheme == "http" || scheme == "https"
}

func uploadFileToURL(ctx context.Context, gatewayBaseURL, uploadURL, apiKey, path, mimeType string) error {
	parsed, err := url.Parse(strings.TrimSpace(uploadURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("upload url must be absolute")
	}
	if !isHTTPURLScheme(parsed.Scheme) {
		return errors.New("upload url must use http or https")
	}
	if !isGatewayMediaUploadURL(gatewayBaseURL, uploadURL) {
		return errors.New("upload url must be a gateway media upload URL")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, parsed.String(), f)
	if err != nil {
		return err
	}
	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func doGatewayJSON(ctx context.Context, method, endpoint, apiKey string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope gatewayErrorEnvelope
		if err := json.Unmarshal(data, &envelope); err == nil && envelope.Error != nil {
			code := strings.TrimSpace(envelope.Error.Code)
			message := strings.TrimSpace(envelope.Error.Message)
			if code == "" {
				code = "error"
			}
			if message == "" {
				message = strings.TrimSpace(string(data))
			}
			if envelope.RequestID != "" {
				return fmt.Errorf("gateway HTTP %d [%s] %s (requestId=%s)", resp.StatusCode, code, message, envelope.RequestID)
			}
			return fmt.Errorf("gateway HTTP %d [%s] %s", resp.StatusCode, code, message)
		}
		return fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("gateway returned non-JSON response (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}
