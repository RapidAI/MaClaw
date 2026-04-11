// Package dingtalk implements a DingTalk bot gateway for corelib.
// It receives messages via webhook HTTP handler and sends replies via REST API.
package dingtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

// Config holds DingTalk bot credentials.
type Config struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
	RobotCode string `json:"robot_code"` // for outbound messages
}

// Gateway implements corelib/im.Plugin for DingTalk.
type Gateway struct {
	config  Config
	handler cim.MessageHandler
	client  *http.Client
}

// NewGateway creates a DingTalk gateway.
func NewGateway(config Config) *Gateway {
	return &Gateway{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *Gateway) Name() string { return "dingtalk" }

func (g *Gateway) Start(ctx context.Context) error {
	if g.config.AppKey == "" || g.config.AppSecret == "" {
		return fmt.Errorf("dingtalk: app_key and app_secret required")
	}
	return nil
}

func (g *Gateway) Stop(ctx context.Context) error { return nil }

func (g *Gateway) OnMessage(handler cim.MessageHandler) {
	g.handler = handler
}

func (g *Gateway) Capabilities() cim.Capabilities {
	return cim.Capabilities{
		SupportsMarkdown: true,
		SupportsRichCard: false,
		SupportsImage:    true,
		SupportsFile:     false,
		MaxTextLength:    20000,
	}
}

// SendText sends a text message to a DingTalk user.
func (g *Gateway) SendText(ctx context.Context, target cim.UserTarget, text string) error {
	token, err := g.getAccessToken()
	if err != nil {
		return err
	}
	body := map[string]any{
		"robotCode":       g.config.RobotCode,
		"userIds":         []string{target.PlatformUID},
		"msgKey":          "sampleText",
		"msgParam":        fmt.Sprintf(`{"content":"%s"}`, escapeJSON(text)),
	}
	return g.postAPI(token, "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend", body)
}

// SendMarkdown sends a markdown message to a DingTalk user.
func (g *Gateway) SendMarkdown(ctx context.Context, target cim.UserTarget, markdown string) error {
	token, err := g.getAccessToken()
	if err != nil {
		return err
	}
	body := map[string]any{
		"robotCode":       g.config.RobotCode,
		"userIds":         []string{target.PlatformUID},
		"msgKey":          "sampleMarkdown",
		"msgParam":        fmt.Sprintf(`{"title":"消息","text":"%s"}`, escapeJSON(markdown)),
	}
	return g.postAPI(token, "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend", body)
}

// WebhookHandler returns an http.HandlerFunc for DingTalk robot callback.
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

		// Verify signature if secret is configured
		if g.config.AppSecret != "" {
			timestamp := r.Header.Get("timestamp")
			sign := r.Header.Get("sign")
			if !g.verifySignature(timestamp, sign) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		var event dingtalkEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)

		if event.Text.Content != "" && g.handler != nil {
			go g.handler(cim.IncomingMessage{
				Platform:    "dingtalk",
				PlatformUID: event.SenderStaffID,
				UserName:    event.SenderNick,
				MessageID:   event.MsgID,
				MessageType: "text",
				Text:        strings.TrimSpace(event.Text.Content),
				RawPayload:  body,
				Timestamp:   time.Now(),
			})
		}
	}
}

type dingtalkEvent struct {
	MsgID         string `json:"msgId"`
	SenderStaffID string `json:"senderStaffId"`
	SenderNick    string `json:"senderNick"`
	Text          struct {
		Content string `json:"content"`
	} `json:"text"`
	MsgType string `json:"msgtype"`
}

func (g *Gateway) getAccessToken() (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"appKey":    g.config.AppKey,
		"appSecret": g.config.AppSecret,
	})
	resp, err := g.client.Post("https://api.dingtalk.com/v1.0/oauth2/accessToken",
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("dingtalk token: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("dingtalk token decode: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("dingtalk: empty access token")
	}
	return result.AccessToken, nil
}

func (g *Gateway) postAPI(token, url string, body any) error {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("dingtalk API %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (g *Gateway) verifySignature(timestamp, sign string) bool {
	if timestamp == "" || sign == "" {
		return false
	}
	stringToSign := timestamp + "\n" + g.config.AppSecret
	h := hmac.New(sha256.New, []byte(g.config.AppSecret))
	h.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return sign == expected
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// unused but keeps import valid
var _ = strconv.Itoa
var _ = log.Println
