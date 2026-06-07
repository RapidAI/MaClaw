package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
	Text string `json:"text"`
}

type PollInput struct {
	GatewayConfig
	Cursor  string `json:"cursor"`
	Timeout int    `json:"timeout"`
	Limit   int    `json:"limit"`
}

type HandshakeRequest struct {
	ClientID        string         `json:"clientId"`
	ClientName      string         `json:"clientName"`
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
}

type IncomingRequest struct {
	ClientID       string         `json:"clientId"`
	EventID        string         `json:"eventId"`
	MessageID      string         `json:"messageId"`
	ConversationID string         `json:"conversationId"`
	User           UserRef        `json:"user"`
	Message        MessagePayload `json:"message"`
	CreatedAt      int64          `json:"createdAt"`
}

type UserRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type MessagePayload struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type IncomingResponse struct {
	OK              bool      `json:"ok"`
	Code            string    `json:"code,omitempty"`
	Message         string    `json:"message,omitempty"`
	RequestID       string    `json:"requestId,omitempty"`
	Accepted        bool      `json:"accepted"`
	Duplicate       bool      `json:"duplicate"`
	MaclawMessageID string    `json:"maclawMessageId"`
	Error           *APIError `json:"error,omitempty"`
}

type OutgoingResponse struct {
	OK         bool              `json:"ok"`
	Code       string            `json:"code,omitempty"`
	Message    string            `json:"message,omitempty"`
	RequestID  string            `json:"requestId,omitempty"`
	Messages   []OutgoingMessage `json:"messages"`
	NextCursor string            `json:"nextCursor"`
	HasMore    bool              `json:"hasMore"`
	Error      *APIError         `json:"error,omitempty"`
}

type OutgoingMessage struct {
	ID               string         `json:"id"`
	Seq              int64          `json:"seq"`
	ConversationID   string         `json:"conversationId"`
	ReplyToMessageID string         `json:"replyToMessageId,omitempty"`
	Type             string         `json:"type"`
	Text             string         `json:"text,omitempty"`
	Caption          string         `json:"caption,omitempty"`
	FileName         string         `json:"fileName,omitempty"`
	ContentType      string         `json:"contentType,omitempty"`
	Data             string         `json:"data,omitempty"`
	Progress         bool           `json:"progress,omitempty"`
	Error            string         `json:"error,omitempty"`
	CreatedAt        int64          `json:"createdAt"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type AckRequest struct {
	ClientID   string   `json:"clientId"`
	MessageIDs []string `json:"messageIds"`
	Status     string   `json:"status"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
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
		ProtocolVersion: "1.0",
		Capabilities: map[string]any{
			"text":        true,
			"longPolling": true,
			"ack":         true,
		},
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
	if text == "" {
		return nil, errors.New("message text is required")
	}
	now := time.Now().UnixMilli()
	messageID := fmt.Sprintf("third_api_demo_%d", now)
	ctx, cancel := context.WithTimeout(a.requestContext(), 30*time.Second)
	defer cancel()

	var in IncomingResponse
	err = doGatewayJSON(ctx, http.MethodPost, cfg.BaseURL+"/incoming", cfg.APIKey, IncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        "evt_" + messageID,
		MessageID:      messageID,
		ConversationID: cfg.ConversationID,
		User:           UserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        MessagePayload{Type: "text", Text: text},
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

func (a *App) requestContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
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
	if cfg.APIKey == "" {
		return cfg, errors.New("apikey is required")
	}
	return cfg, nil
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
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("gateway returned non-JSON response (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}
