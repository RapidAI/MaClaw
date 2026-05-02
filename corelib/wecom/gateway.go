// Package wecom implements a WeCom (企业微信) bot gateway for corelib.
// It receives messages via webhook callback and sends replies via REST API.
package wecom

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

// Config holds WeCom bot credentials.
type Config struct {
	CorpID     string `json:"corp_id"`
	CorpSecret string `json:"corp_secret"`
	AgentID    int    `json:"agent_id"`
	Token      string `json:"token"`   // for callback verification
	AESKey     string `json:"aes_key"` // for callback encryption
}

// Gateway implements corelib/im.Plugin for WeCom.
type Gateway struct {
	config  Config
	handler cim.MessageHandler
	client  *http.Client
}

// NewGateway creates a WeCom gateway.
func NewGateway(config Config) *Gateway {
	return &Gateway{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *Gateway) Name() string { return "wecom" }

func (g *Gateway) Start(ctx context.Context) error {
	if g.config.CorpID == "" || g.config.CorpSecret == "" {
		return fmt.Errorf("wecom: corp_id and corp_secret required")
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
		SupportsFile:     true,
		MaxTextLength:    2048,
	}
}

// SendText sends a text message to a WeCom user.
func (g *Gateway) SendText(ctx context.Context, target cim.UserTarget, text string) error {
	token, err := g.getAccessToken()
	if err != nil {
		return err
	}
	body := map[string]any{
		"touser":  target.PlatformUID,
		"msgtype": "text",
		"agentid": g.config.AgentID,
		"text":    map[string]string{"content": text},
	}
	return g.postAPI(token, "https://qyapi.weixin.qq.com/cgi-bin/message/send", body)
}

// SendMarkdown sends a markdown message to a WeCom user.
func (g *Gateway) SendMarkdown(ctx context.Context, target cim.UserTarget, markdown string) error {
	token, err := g.getAccessToken()
	if err != nil {
		return err
	}
	body := map[string]any{
		"touser":   target.PlatformUID,
		"msgtype":  "markdown",
		"agentid":  g.config.AgentID,
		"markdown": map[string]string{"content": markdown},
	}
	return g.postAPI(token, "https://qyapi.weixin.qq.com/cgi-bin/message/send", body)
}

// SendAudio sends a native WeCom voice message. WeCom requires AMR audio.
func (g *Gateway) SendAudio(ctx context.Context, target cim.UserTarget, audioData []byte, durationMs int) error {
	if len(audioData) == 0 {
		return fmt.Errorf("wecom: empty audio data")
	}
	if !isAMR(audioData) {
		return fmt.Errorf("wecom: voice payload must be AMR")
	}
	token, err := g.getAccessToken()
	if err != nil {
		return err
	}
	mediaID, err := g.uploadVoice(ctx, token, audioData)
	if err != nil {
		return fmt.Errorf("wecom: upload voice: %w", err)
	}
	body := map[string]any{
		"touser":  target.PlatformUID,
		"msgtype": "voice",
		"agentid": g.config.AgentID,
		"voice":   map[string]string{"media_id": mediaID},
	}
	return g.postAPI(token, "https://qyapi.weixin.qq.com/cgi-bin/message/send", body)
}

func (g *Gateway) uploadVoice(ctx context.Context, token string, audioData []byte) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("media", "voice.amr")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	url := "https://qyapi.weixin.qq.com/cgi-bin/media/upload?access_token=" + token + "&type=voice"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("upload err=%d: %s", result.ErrCode, result.ErrMsg)
	}
	if result.MediaID == "" {
		return "", fmt.Errorf("upload returned empty media_id")
	}
	return result.MediaID, nil
}

func isAMR(data []byte) bool {
	return bytes.HasPrefix(data, []byte("#!AMR\n")) || bytes.HasPrefix(data, []byte("#!AMR-WB\n"))
}

// WebhookHandler returns an http.HandlerFunc for WeCom callback.
// Handles both GET (URL verification) and POST (message events).
func (g *Gateway) WebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// GET: URL verification
		if r.Method == http.MethodGet {
			g.handleVerify(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
			return
		}

		// Verify signature
		if g.config.Token != "" {
			msgSignature := r.URL.Query().Get("msg_signature")
			timestamp := r.URL.Query().Get("timestamp")
			nonce := r.URL.Query().Get("nonce")
			if !g.verifySignature(msgSignature, timestamp, nonce, "") {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}

		var msg wecomXMLMessage
		if err := xml.Unmarshal(body, &msg); err != nil {
			http.Error(w, "invalid XML", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))

		if msg.MsgType == "text" && msg.Content != "" && g.handler != nil {
			go g.handler(cim.IncomingMessage{
				Platform:    "wecom",
				PlatformUID: msg.FromUserName,
				UserName:    msg.FromUserName,
				MessageID:   msg.MsgID,
				MessageType: "text",
				Text:        strings.TrimSpace(msg.Content),
				RawPayload:  body,
				Timestamp:   time.Now(),
			})
		}
	}
}

type wecomXMLMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgID        string   `xml:"MsgId"`
	AgentID      int      `xml:"AgentID"`
}

func (g *Gateway) handleVerify(w http.ResponseWriter, r *http.Request) {
	echostr := r.URL.Query().Get("echostr")
	if echostr != "" {
		// In production, decrypt echostr with AES key. For simplicity, echo back.
		w.Write([]byte(echostr))
		return
	}
	http.Error(w, "missing echostr", http.StatusBadRequest)
}

func (g *Gateway) getAccessToken() (string, error) {
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		g.config.CorpID, g.config.CorpSecret)
	resp, err := g.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("wecom token: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("wecom token decode: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wecom token err=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return result.AccessToken, nil
}

func (g *Gateway) postAPI(token, baseURL string, body any) error {
	url := baseURL + "?access_token=" + token
	data, _ := json.Marshal(body)
	resp, err := g.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom API err=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func (g *Gateway) verifySignature(msgSignature, timestamp, nonce, encrypt string) bool {
	strs := []string{g.config.Token, timestamp, nonce, encrypt}
	sort.Strings(strs)
	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))
	expected := fmt.Sprintf("%x", h.Sum(nil))
	return msgSignature == expected
}

var _ = log.Println
