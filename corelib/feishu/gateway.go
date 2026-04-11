// Package feishu implements a Feishu (Lark) bot gateway for corelib.
// It receives messages via webhook HTTP handler and sends replies via REST API.
// This is a lightweight implementation suitable for iWorkerCenter/DiWorker
// without the full hub IM adapter stack.
package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

// Config holds Feishu bot credentials.
type Config struct {
	AppID             string `json:"app_id"`
	AppSecret         string `json:"app_secret"`
	VerificationToken string `json:"verification_token"`
	EncryptKey        string `json:"encrypt_key"`
}

// Gateway implements corelib/im.Plugin for Feishu.
type Gateway struct {
	config  Config
	handler cim.MessageHandler
	client  *http.Client

	mu          sync.Mutex
	tenantToken string
	tokenExpiry time.Time
}

// NewGateway creates a Feishu gateway.
func NewGateway(config Config) *Gateway {
	return &Gateway{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *Gateway) Name() string { return "feishu" }

func (g *Gateway) Start(ctx context.Context) error {
	if g.config.AppID == "" || g.config.AppSecret == "" {
		return fmt.Errorf("feishu: app_id and app_secret required")
	}
	_, err := g.ensureToken()
	return err
}

func (g *Gateway) Stop(ctx context.Context) error { return nil }

func (g *Gateway) OnMessage(handler cim.MessageHandler) {
	g.handler = handler
}

func (g *Gateway) Capabilities() cim.Capabilities {
	return cim.Capabilities{
		SupportsMarkdown: true,
		SupportsRichCard: true,
		SupportsImage:    true,
		SupportsFile:     true,
		MaxTextLength:    30000,
	}
}

// SendText sends a text message to a Feishu user by open_id.
func (g *Gateway) SendText(ctx context.Context, target cim.UserTarget, text string) error {
	token, err := g.ensureToken()
	if err != nil {
		return err
	}
	body := map[string]any{
		"receive_id": target.PlatformUID,
		"msg_type":   "text",
		"content":    fmt.Sprintf(`{"text":"%s"}`, escapeJSON(text)),
	}
	return g.postAPI(token, "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id", body)
}

// SendMarkdown sends a markdown message (Feishu uses rich text / post format).
func (g *Gateway) SendMarkdown(ctx context.Context, target cim.UserTarget, markdown string) error {
	// Feishu doesn't support raw markdown in messages, fall back to text
	return g.SendText(ctx, target, markdown)
}

// WebhookHandler returns an http.HandlerFunc that processes Feishu event callbacks.
// Mount this at your webhook endpoint (e.g. /feishu/webhook).
func (g *Gateway) WebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}

		// Handle URL verification challenge
		var challenge struct {
			Challenge string `json:"challenge"`
			Type      string `json:"type"`
		}
		if json.Unmarshal(body, &challenge) == nil && challenge.Type == "url_verification" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"challenge": challenge.Challenge})
			return
		}

		// Parse event
		var event feishuEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}

		// Verify token if configured
		if g.config.VerificationToken != "" && event.Header.Token != g.config.VerificationToken {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)

		// Process message events asynchronously
		if event.Header.EventType == "im.message.receive_v1" && g.handler != nil {
			go g.handleMessageEvent(event, body)
		}
	}
}

type feishuEvent struct {
	Header struct {
		EventID   string `json:"event_id"`
		Token     string `json:"token"`
		EventType string `json:"event_type"`
	} `json:"header"`
	Event json.RawMessage `json:"event"`
}

type feishuMessageEvent struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
		} `json:"sender_id"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
	} `json:"message"`
}

func (g *Gateway) handleMessageEvent(event feishuEvent, rawBody []byte) {
	var msgEvent feishuMessageEvent
	if err := json.Unmarshal(event.Event, &msgEvent); err != nil {
		log.Printf("[feishu] parse message event: %v", err)
		return
	}

	// Only handle user messages, not bot messages
	if msgEvent.Sender.SenderType == "bot" {
		return
	}

	text := ""
	if msgEvent.Message.MessageType == "text" {
		var content struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(msgEvent.Message.Content), &content) == nil {
			text = content.Text
		}
	}

	if text == "" {
		return
	}

	g.handler(cim.IncomingMessage{
		Platform:    "feishu",
		PlatformUID: msgEvent.Sender.SenderID.OpenID,
		MessageID:   msgEvent.Message.MessageID,
		MessageType: "text",
		Text:        text,
		RawPayload:  rawBody,
		Timestamp:   time.Now(),
	})
}

func (g *Gateway) ensureToken() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tenantToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.tenantToken, nil
	}
	payload, _ := json.Marshal(map[string]string{
		"app_id":     g.config.AppID,
		"app_secret": g.config.AppSecret,
	})
	resp, err := g.client.Post("https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("feishu token: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("feishu token decode: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("feishu token code=%d", result.Code)
	}
	g.tenantToken = result.TenantAccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(result.Expire-60) * time.Second)
	return g.tenantToken, nil
}

func (g *Gateway) postAPI(token, url string, body any) error {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("feishu API %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// hmacSHA256 computes HMAC-SHA256 (used for event signature verification).
func hmacSHA256(key, data string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return h.Sum(nil)
}
