// Package qqbot implements the im.IMPlugin interface for QQ Bot (官方机器人).
//
// Protocol learned from github.com/sliverp/qqbot source code:
//   - WebSocket gateway for receiving events (not webhook)
//   - REST API for sending messages (/v2/users/{openid}/messages)
//   - AccessToken via POST https://bots.qq.com/app/getAppAccessToken
//   - Authorization header: "QQBot {access_token}"
//   - Intents: 1<<25 for GROUP_AND_C2C_EVENT
//   - Events: C2C_MESSAGE_CREATE, GROUP_AT_MESSAGE_CREATE
//   - WebSocket flow: Hello(op=10) → Identify(op=2) → Ready(op=0) → Heartbeat(op=1)
//   - Also supports webhook mode (op=13 validation, op=12 ACK)
package qqbot

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"

	"github.com/gorilla/websocket"
)

const (
	qqAPIBase     = "https://api.sgroup.qq.com"
	tokenEndpoint = "https://bots.qq.com/app/getAppAccessToken"

	// Intents: GROUP_AND_C2C_EVENT (1<<25) covers C2C_MESSAGE_CREATE,
	// GROUP_AT_MESSAGE_CREATE, FRIEND_ADD, etc.
	intentsGroupAndC2C = 1 << 25

	wsReconnectBaseDelay = 3 * time.Second
	wsReconnectMaxDelay  = 60 * time.Second
	wsMaxReconnects      = 50
)

// Config holds QQBot credentials.
type Config struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// ConfigProvider returns the current QQBot config (read from DB).
type ConfigProvider func() Config

// Plugin implements im.IMPlugin for QQ Bot.
type Plugin struct {
	configProvider ConfigProvider
	users          store.UserRepository
	system         store.SystemSettingsRepository
	mailer         Mailer
	broadcaster    NotifyBroadcaster
	client         *http.Client

	mu             sync.Mutex
	messageHandler func(msg im.IncomingMessage)

	// token cache
	tokenMu      sync.Mutex
	accessToken  string
	tokenExpires time.Time

	// openid → email bindings (persisted in system settings)
	bindMu   sync.RWMutex
	bindings map[string]string // openid → email

	// pending verification codes: openid → {email, code, expiry}
	pendingMu sync.Mutex
	pending   map[string]*pendingBind

	// WebSocket gateway
	wsCancel context.CancelFunc
	wsWg     sync.WaitGroup

	// wsMu serialises all writes to the WebSocket connection.
	// gorilla/websocket does not support concurrent writers.
	wsMu sync.Mutex

	// lastSeq is the latest sequence number from the gateway, accessed
	// by both the read loop and the heartbeat goroutine.
	seqMu   sync.Mutex
	lastSeq *int

	// publicBaseURL is the hub's externally reachable URL, used to
	// construct temporary download URLs for large file uploads.
	publicBaseURL string

	// tempFiles stores base64-encoded file data keyed by a random token.
	// Entries expire after tempFileTTL and are cleaned up periodically.
	tempMu    sync.Mutex
	tempFiles map[string]*tempFileEntry
}

// Mailer is the interface for sending emails (satisfied by mail.Service).
type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

// NotifyBroadcaster sends verification codes to all reachable channels.
type NotifyBroadcaster interface {
	BroadcastVerifyCode(ctx context.Context, email, code, excludePlatform string) (sentTo string, err error)
}

type pendingBind struct {
	Email  string
	Code   string
	Expiry time.Time
}

// tempFileEntry holds a base64-encoded file for temporary download.
type tempFileEntry struct {
	Data      []byte // raw file bytes
	MimeType  string
	ExpiresAt time.Time
}

const (
	// tempFileTTL is how long a temp file is kept before cleanup.
	tempFileTTL = 5 * time.Minute
	// urlUploadThreshold: if base64 data exceeds this size (bytes), use URL
	// mode instead of inline file_data to avoid QQ gateway 413 errors.
	// QQ's stgw typically limits request bodies to ~10 MB.
	urlUploadThreshold = 4 * 1024 * 1024 // 4 MB of base64 ≈ 3 MB raw
)

// wsPayload is the QQ Bot WebSocket payload structure.
type wsPayload struct {
	ID string          `json:"id,omitempty"`
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int            `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// New creates a QQBot plugin.
func New(provider ConfigProvider, users store.UserRepository, system store.SystemSettingsRepository, mailer Mailer) *Plugin {
	p := &Plugin{
		configProvider: provider,
		users:          users,
		system:         system,
		mailer:         mailer,
		client:         &http.Client{Timeout: 60 * time.Second},
		bindings:       make(map[string]string),
		pending:        make(map[string]*pendingBind),
		tempFiles:      make(map[string]*tempFileEntry),
	}
	p.loadBindings()
	return p
}

// SetPublicBaseURL sets the hub's externally reachable URL for temp file downloads.
func (p *Plugin) SetPublicBaseURL(url string) {
	p.publicBaseURL = strings.TrimRight(url, "/")
}

// SetBroadcaster wires the cross-IM notification broadcaster.
// Called from bootstrap after the IM Adapter is fully assembled.
func (p *Plugin) SetBroadcaster(b NotifyBroadcaster) {
	p.broadcaster = b
}

// ---------------------------------------------------------------------------
// im.IMPlugin interface
// ---------------------------------------------------------------------------

func (p *Plugin) Name() string { return "qqbot" }

func (p *Plugin) ReceiveMessage(handler func(msg im.IncomingMessage)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageHandler = handler
}

func (p *Plugin) SendText(ctx context.Context, target im.UserTarget, text string) error {
	cfg := p.configProvider()
	if !cfg.Enabled || cfg.AppID == "" {
		return fmt.Errorf("qqbot: not configured")
	}
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("qqbot: PlatformUID (openid) is required")
	}
	return p.sendC2CMessage(ctx, openID, text)
}

func (p *Plugin) SendCard(ctx context.Context, target im.UserTarget, card im.OutgoingMessage) error {
	// QQ Bot C2C 不支持富卡片，降级为文本
	text := card.FallbackText
	if text == "" {
		var sb strings.Builder
		if card.Title != "" {
			sb.WriteString(card.Title)
			sb.WriteString("\n")
		}
		if card.StatusIcon != "" {
			sb.WriteString(card.StatusIcon)
			sb.WriteString(" ")
		}
		if card.Body != "" {
			sb.WriteString(card.Body)
		}
		for _, f := range card.Fields {
			sb.WriteString("\n")
			sb.WriteString(f.Label)
			sb.WriteString(": ")
			sb.WriteString(f.Value)
		}
		text = sb.String()
	}
	return p.SendText(ctx, target, text)
}

func (p *Plugin) SendImage(ctx context.Context, target im.UserTarget, imageKey string, caption string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("qqbot: PlatformUID (openid) is required")
	}

	// imageKey is base64-encoded PNG data from the screenshot pipeline.
	if len(imageKey) > 200 {
		// Compress PNG → JPEG to reduce size; QQ's media upload is prone to
		// 850027 "富媒体文件上传超时" when the payload is large.
		compressed, mime := compressImageForQQ(imageKey, "image/png")
		err := p.sendC2CMedia(ctx, openID, 1, compressed, "", mime, caption)
		if err != nil {
			log.Printf("[qqbot] SendImage failed, trying download-link fallback: %v", err)
			return p.sendImageAsLink(ctx, openID, imageKey, "image/png", caption)
		}
		return nil
	}

	text := caption
	if text == "" {
		text = "[图片]"
	}
	return p.SendText(ctx, target, text)
}

// compressImageForQQ converts base64-encoded PNG data to JPEG at quality 75
// to reduce payload size. If conversion fails, returns the original data.
func compressImageForQQ(base64Data, mimeType string) (string, string) {
	if !strings.HasPrefix(mimeType, "image/png") {
		return base64Data, mimeType
	}
	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return base64Data, mimeType
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return base64Data, mimeType
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75}); err != nil {
		return base64Data, mimeType
	}
	// Only use JPEG if it's actually smaller.
	if buf.Len() >= len(raw) {
		return base64Data, mimeType
	}
	log.Printf("[qqbot] compressed image: PNG %d bytes → JPEG %d bytes (%.0f%% reduction)",
		len(raw), buf.Len(), float64(len(raw)-buf.Len())/float64(len(raw))*100)
	return base64.StdEncoding.EncodeToString(buf.Bytes()), "image/jpeg"
}

// sendImageAsLink stores the image as a temp file and sends a text message
// with the download URL. This is the last-resort fallback when QQ's media
// upload API keeps timing out.
func (p *Plugin) sendImageAsLink(ctx context.Context, openID, base64Data, mimeType, caption string) error {
	if p.publicBaseURL == "" {
		return fmt.Errorf("qqbot: publicBaseURL not set, cannot create download link")
	}
	downloadURL, err := p.storeTempFile(base64Data, mimeType)
	if err != nil {
		return fmt.Errorf("qqbot: store temp file for link fallback: %w", err)
	}
	text := "📷 图片发送失败，请通过链接查看（5分钟内有效）：\n" + downloadURL
	if caption != "" {
		text = caption + "\n" + text
	}
	return p.sendC2CMessage(ctx, openID, text)
}

// SendFile sends a file to the target user via QQ rich media API.
func (p *Plugin) SendFile(ctx context.Context, target im.UserTarget, fileData, fileName, mimeType string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("qqbot: PlatformUID (openid) is required")
	}

	// Determine file type based on mimeType.
	fileType := 4 // default: file
	if strings.HasPrefix(mimeType, "image/") {
		fileType = 1
	} else if strings.HasPrefix(mimeType, "video/") {
		fileType = 2
	} else if strings.HasPrefix(mimeType, "audio/") {
		fileType = 3
	}

	return p.sendC2CMedia(ctx, openID, fileType, fileData, fileName, mimeType, "")
}

func (p *Plugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	p.bindMu.RLock()
	email, ok := p.bindings[platformUID]
	p.bindMu.RUnlock()
	if !ok || email == "" {
		return "", fmt.Errorf("qqbot: user %s not bound, please send your email to bind", platformUID)
	}
	user, err := p.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		return "", fmt.Errorf("qqbot: no hub user found for email %s", email)
	}
	return user.ID, nil
}

func (p *Plugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{
		SupportsRichCard:    false,
		SupportsMarkdown:    false,
		SupportsImage:       true,
		SupportsFile:        true,
		SupportsButton:      false,
		SupportsMessageEdit: false,
		MaxTextLength:       4000,
	}
}

func (p *Plugin) Start(ctx context.Context) error {
	cfg := p.configProvider()
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		log.Printf("[qqbot] not configured, skipping WebSocket gateway")
		return nil
	}
	// Guard against double-start without Stop.
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	wsCtx, cancel := context.WithCancel(context.Background())
	p.wsCancel = cancel
	p.wsWg.Add(1)
	go p.runGateway(wsCtx)
	log.Printf("[qqbot] started (WebSocket gateway launched)")
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	p.seqMu.Lock()
	p.lastSeq = nil
	p.seqMu.Unlock()
	log.Printf("[qqbot] stopped")
	return nil
}

// ---------------------------------------------------------------------------
// AccessToken management
// ---------------------------------------------------------------------------

func (p *Plugin) getAccessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	// Return cached token if still valid (with 60s buffer)
	if p.accessToken != "" && time.Now().Before(p.tokenExpires.Add(-60*time.Second)) {
		return p.accessToken, nil
	}

	cfg := p.configProvider()
	body, _ := json.Marshal(map[string]string{
		"appId":        cfg.AppID,
		"clientSecret": cfg.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("qqbot: token request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("qqbot: token decode failed: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("qqbot: empty access_token returned")
	}

	expSec, _ := result.ExpiresIn.Int64()
	if expSec <= 0 {
		expSec = 7200
	}
	p.accessToken = result.AccessToken
	p.tokenExpires = time.Now().Add(time.Duration(expSec) * time.Second)
	log.Printf("[qqbot] access token refreshed, expires in %ds", expSec)
	return p.accessToken, nil
}

// ---------------------------------------------------------------------------
// Send C2C message via REST API
// ---------------------------------------------------------------------------

func (p *Plugin) sendC2CMessage(ctx context.Context, openID, text string) error {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/v2/users/%s/messages", qqAPIBase, openID)
	body, _ := json.Marshal(map[string]any{
		"content":  text,
		"msg_type": 0, // 文本
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("qqbot: send message failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("qqbot: send message HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// sendC2CMedia uploads base64 data to QQ and sends it as a rich media message.
// fileType: 1=image, 2=video, 3=voice, 4=file
func (p *Plugin) sendC2CMedia(ctx context.Context, openID string, fileType int, base64Data, fileName, mimeType, caption string) error {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return err
	}

	// Step 1: Upload media to get file_info.
	uploadURL := fmt.Sprintf("%s/v2/users/%s/files", qqAPIBase, openID)

	// Try upload, with one retry using URL mode if inline fails.
	fileInfo, err := p.tryUploadMedia(ctx, token, uploadURL, fileType, base64Data, fileName, mimeType)
	if err != nil {
		return err
	}

	// Step 2: Send rich media message.
	msgURL := fmt.Sprintf("%s/v2/users/%s/messages", qqAPIBase, openID)
	msgBody, _ := json.Marshal(map[string]any{
		"msg_type": 7,
		"media": map[string]any{
			"file_info": fileInfo,
		},
	})

	msgReq, err := http.NewRequestWithContext(ctx, http.MethodPost, msgURL, bytes.NewReader(msgBody))
	if err != nil {
		return fmt.Errorf("qqbot: create message request: %w", err)
	}
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Authorization", "QQBot "+token)

	msgResp, err := p.client.Do(msgReq)
	if err != nil {
		return fmt.Errorf("qqbot: send media message failed: %w", err)
	}
	defer msgResp.Body.Close()

	if msgResp.StatusCode >= 300 {
		msgRespBody, _ := io.ReadAll(io.LimitReader(msgResp.Body, 4096))
		return fmt.Errorf("qqbot: send media message HTTP %d: %s", msgResp.StatusCode, string(msgRespBody))
	}

	// Send caption as a separate text message if provided.
	if caption != "" {
		_ = p.sendC2CMessage(ctx, openID, caption)
	}

	return nil
}

// tryUploadMedia attempts to upload media to QQ. It first tries inline mode,
// and if that fails with a timeout/size error, retries with URL mode.
func (p *Plugin) tryUploadMedia(ctx context.Context, token, uploadURL string, fileType int, base64Data, fileName, mimeType string) (string, error) {
	uploadPayload := map[string]any{
		"file_type":    fileType,
		"srv_send_msg": false,
	}
	if fileType == 4 && fileName != "" {
		uploadPayload["file_name"] = fileName
	}

	useURLMode := len(base64Data) > urlUploadThreshold && p.publicBaseURL != ""

	if useURLMode {
		tempURL, storeErr := p.storeTempFile(base64Data, mimeType)
		if storeErr != nil {
			log.Printf("[qqbot] failed to store temp file, falling back to inline: %v", storeErr)
			uploadPayload["file_data"] = base64Data
		} else {
			log.Printf("[qqbot] large file (%d bytes base64), using URL mode: %s", len(base64Data), tempURL)
			uploadPayload["url"] = tempURL
		}
	} else {
		uploadPayload["file_data"] = base64Data
	}

	fileInfo, err := p.doUploadMedia(ctx, token, uploadURL, uploadPayload)
	if err == nil {
		return fileInfo, nil
	}

	// If inline mode failed and we haven't tried URL mode yet, retry with URL.
	errStr := err.Error()
	isTimeoutErr := strings.Contains(errStr, "850027") || strings.Contains(errStr, "40034003") || strings.Contains(errStr, "超时")
	if !useURLMode && isTimeoutErr && p.publicBaseURL != "" {
		log.Printf("[qqbot] inline upload failed with timeout, retrying with URL mode: %v", err)
		tempURL, storeErr := p.storeTempFile(base64Data, mimeType)
		if storeErr != nil {
			return "", fmt.Errorf("qqbot: inline upload timed out and URL fallback failed: %w", err)
		}
		retryPayload := map[string]any{
			"file_type":    fileType,
			"srv_send_msg": false,
			"url":          tempURL,
		}
		if fileType == 4 && fileName != "" {
			retryPayload["file_name"] = fileName
		}
		return p.doUploadMedia(ctx, token, uploadURL, retryPayload)
	}

	return "", err
}

// doUploadMedia performs the actual HTTP POST to QQ's file upload endpoint.
func (p *Plugin) doUploadMedia(ctx context.Context, token, uploadURL string, payload map[string]any) (string, error) {
	uploadBody, _ := json.Marshal(payload)

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(uploadBody))
	if err != nil {
		return "", fmt.Errorf("qqbot: create upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", "application/json")
	uploadReq.Header.Set("Authorization", "QQBot "+token)

	uploadResp, err := p.client.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("qqbot: upload media failed: %w", err)
	}
	defer uploadResp.Body.Close()

	uploadRespBody, _ := io.ReadAll(io.LimitReader(uploadResp.Body, 8192))
	if uploadResp.StatusCode >= 300 {
		return "", fmt.Errorf("qqbot: upload media HTTP %d: %s", uploadResp.StatusCode, string(uploadRespBody))
	}

	var uploadResult struct {
		FileInfo string `json:"file_info"`
	}
	if err := json.Unmarshal(uploadRespBody, &uploadResult); err != nil {
		return "", fmt.Errorf("qqbot: parse upload response: %w", err)
	}
	if uploadResult.FileInfo == "" {
		return "", fmt.Errorf("qqbot: upload returned empty file_info")
	}
	return uploadResult.FileInfo, nil
}
// ---------------------------------------------------------------------------
// WebSocket Gateway — connects to QQ Bot gateway for real-time events
// Protocol: Hello(op=10) → Identify(op=2) → Ready(op=0) → Heartbeat(op=1)
// Learned from github.com/sliverp/qqbot gateway.ts
// ---------------------------------------------------------------------------

func (p *Plugin) runGateway(ctx context.Context) {
	defer p.wsWg.Done()

	var sessionID string
	reconnects := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		gotReady, err := p.connectAndRun(ctx, &sessionID)
		if ctx.Err() != nil {
			return // context cancelled, clean shutdown
		}

		// Reset reconnect counter if we had a successful session.
		if gotReady {
			reconnects = 0
		}

		reconnects++
		if reconnects > wsMaxReconnects {
			log.Printf("[qqbot/ws] max reconnect attempts (%d) reached, giving up", wsMaxReconnects)
			return
		}

		shift := reconnects - 1
		if shift > 4 {
			shift = 4
		}
		delay := wsReconnectBaseDelay * time.Duration(1<<shift)
		if delay > wsReconnectMaxDelay {
			delay = wsReconnectMaxDelay
		}
		if err != nil {
			log.Printf("[qqbot/ws] connection error: %v, reconnecting in %v", err, delay)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (p *Plugin) connectAndRun(ctx context.Context, sessionID *string) (gotReady bool, err error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return false, fmt.Errorf("get token: %w", err)
	}

	// Get gateway URL
	gatewayURL, err := p.getGatewayURL(ctx, token)
	if err != nil {
		return false, fmt.Errorf("get gateway: %w", err)
	}

	log.Printf("[qqbot/ws] connecting to %s", gatewayURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// wsWrite serialises writes to the connection (gorilla/websocket is not
	// safe for concurrent writers).
	wsWrite := func(v any) error {
		p.wsMu.Lock()
		defer p.wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Read Hello (op=10)
	var hello wsPayload
	if err := conn.ReadJSON(&hello); err != nil {
		return false, fmt.Errorf("read hello: %w", err)
	}
	if hello.Op != 10 {
		return false, fmt.Errorf("expected op=10 Hello, got op=%d", hello.Op)
	}

	var helloData struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &helloData)
	heartbeatMs := helloData.HeartbeatInterval
	if heartbeatMs <= 0 {
		heartbeatMs = 45000
	}
	log.Printf("[qqbot/ws] Hello received, heartbeat_interval=%dms", heartbeatMs)

	// Send Identify (op=2) or Resume (op=6)
	p.seqMu.Lock()
	lastSeq := p.lastSeq
	p.seqMu.Unlock()

	if *sessionID != "" && lastSeq != nil {
		log.Printf("[qqbot/ws] resuming session %s seq=%d", *sessionID, *lastSeq)
		resume := map[string]any{
			"op": 6,
			"d": map[string]any{
				"token":      "QQBot " + token,
				"session_id": *sessionID,
				"seq":        *lastSeq,
			},
		}
		if err := wsWrite(resume); err != nil {
			return false, fmt.Errorf("send resume: %w", err)
		}
	} else {
		log.Printf("[qqbot/ws] sending identify with intents=%d", intentsGroupAndC2C)
		identify := map[string]any{
			"op": 2,
			"d": map[string]any{
				"token":   "QQBot " + token,
				"intents": intentsGroupAndC2C,
				"shard":   []int{0, 1},
			},
		}
		if err := wsWrite(identify); err != nil {
			return false, fmt.Errorf("send identify: %w", err)
		}
	}

	// Start heartbeat goroutine
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go func() {
		ticker := time.NewTicker(time.Duration(heartbeatMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				p.seqMu.Lock()
				seq := p.lastSeq
				p.seqMu.Unlock()
				hb := map[string]any{"op": 1, "d": seq}
				if err := wsWrite(hb); err != nil {
					log.Printf("[qqbot/ws] heartbeat send error: %v", err)
					return
				}
			}
		}
	}()

	// Read loop
	for {
		select {
		case <-ctx.Done():
			return gotReady, nil
		default:
		}

		var payload wsPayload
		if err := conn.ReadJSON(&payload); err != nil {
			return gotReady, fmt.Errorf("read: %w", err)
		}

		if payload.S != nil {
			p.seqMu.Lock()
			p.lastSeq = payload.S
			p.seqMu.Unlock()
		}

		switch payload.Op {
		case 0: // Dispatch
			if payload.T == "READY" || payload.T == "RESUMED" {
				gotReady = true
			}
			p.handleDispatch(payload.T, payload.D, sessionID)
		case 1: // Heartbeat request from server
			p.seqMu.Lock()
			seq := p.lastSeq
			p.seqMu.Unlock()
			hb := map[string]any{"op": 1, "d": seq}
			_ = wsWrite(hb)
		case 7: // Reconnect
			log.Printf("[qqbot/ws] server requested reconnect (op=7)")
			return gotReady, nil
		case 9: // Invalid Session
			canResume := false
			_ = json.Unmarshal(payload.D, &canResume)
			if !canResume {
				*sessionID = ""
				p.seqMu.Lock()
				p.lastSeq = nil
				p.seqMu.Unlock()
			}
			log.Printf("[qqbot/ws] invalid session, canResume=%v", canResume)
			return gotReady, nil
		case 11: // Heartbeat ACK
			// ok
		}
	}
}

func (p *Plugin) handleDispatch(eventType string, data json.RawMessage, sessionID *string) {
	switch eventType {
	case "READY":
		var ready struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(data, &ready)
		*sessionID = ready.SessionID
		log.Printf("[qqbot/ws] READY, session=%s", ready.SessionID)

	case "RESUMED":
		log.Printf("[qqbot/ws] session resumed")

	case "C2C_MESSAGE_CREATE":
		cfg := p.configProvider()
		if !cfg.Enabled {
			log.Printf("[qqbot/ws] plugin disabled, ignoring C2C message")
			return
		}
		go p.handleC2CMessage(data)

	case "GROUP_AT_MESSAGE_CREATE":
		log.Printf("[qqbot/ws] group @message received (not yet supported)")

	case "FRIEND_ADD":
		log.Printf("[qqbot/ws] friend added")

	default:
		log.Printf("[qqbot/ws] unhandled event: %s", eventType)
	}
}

func (p *Plugin) getGatewayURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qqAPIBase+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("gateway API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty gateway URL")
	}
	return result.URL, nil
}

// ---------------------------------------------------------------------------
// Webhook handling — alternative to WebSocket for servers with public IP
// Called by HTTP handler when QQ Bot is configured in webhook mode.
// ---------------------------------------------------------------------------

// HandleWebhook processes incoming QQ Bot webhook events.
// Returns (responseBody, httpStatus).
func (p *Plugin) HandleWebhook(r *http.Request) ([]byte, int) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return []byte(`{"error":"read body failed"}`), http.StatusBadRequest
	}

	cfg := p.configProvider()

	var payload struct {
		ID   string          `json:"id"`
		Op   int             `json:"op"`
		S    int             `json:"s"`
		T    string          `json:"t"`
		Data json.RawMessage `json:"d"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []byte(`{"error":"invalid json"}`), http.StatusBadRequest
	}

	log.Printf("[qqbot/webhook] op=%d t=%s", payload.Op, payload.T)

	// Op 13: 回调地址验证 (ed25519 signature) — always respond even if disabled
	if payload.Op == 13 {
		return p.handleValidation(payload.Data, cfg.AppSecret)
	}

	// If disabled, ACK but don't process any messages
	if !cfg.Enabled {
		log.Printf("[qqbot/webhook] plugin disabled, ignoring event %s", payload.T)
		return []byte(`{"op":12}`), http.StatusOK
	}

	// Verify ed25519 signature for non-validation requests
	if cfg.AppSecret != "" {
		sig := r.Header.Get("X-Signature-Ed25519")
		ts := r.Header.Get("X-Signature-Timestamp")
		if sig != "" && ts != "" {
			if !verifyEd25519Signature(cfg.AppSecret, ts, string(body), sig) {
				log.Printf("[qqbot/webhook] signature verification failed")
				return []byte(`{"error":"invalid signature"}`), http.StatusForbidden
			}
		}
	}

	// Op 0: Dispatch
	if payload.Op == 0 {
		switch payload.T {
		case "C2C_MESSAGE_CREATE":
			go p.handleC2CMessage(payload.Data)
		case "GROUP_AT_MESSAGE_CREATE":
			log.Printf("[qqbot/webhook] group message (not yet supported)")
		case "FRIEND_ADD":
			log.Printf("[qqbot/webhook] friend added")
		}
	}

	// Return op=12 HTTP Callback ACK
	ack, _ := json.Marshal(map[string]int{"op": 12})
	return ack, http.StatusOK
}

// handleValidation handles op=13 callback URL verification per QQ Bot docs.
func (p *Plugin) handleValidation(data json.RawMessage, secret string) ([]byte, int) {
	var req struct {
		PlainToken string `json:"plain_token"`
		EventTs    string `json:"event_ts"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return []byte(`{"error":"invalid validation data"}`), http.StatusBadRequest
	}

	_, privateKey := ed25519KeyFromSecret(secret)

	var msg bytes.Buffer
	msg.WriteString(req.EventTs)
	msg.WriteString(req.PlainToken)
	signature := hex.EncodeToString(ed25519.Sign(privateKey, msg.Bytes()))

	resp, _ := json.Marshal(map[string]string{
		"plain_token": req.PlainToken,
		"signature":   signature,
	})
	return resp, http.StatusOK
}

func verifyEd25519Signature(secret, timestamp, body, sigHex string) bool {
	publicKey, _ := ed25519KeyFromSecret(secret)
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.WriteString(body)
	return ed25519.Verify(publicKey, msg.Bytes(), sig)
}

// ed25519KeyFromSecret derives a deterministic ed25519 key pair from the
// app secret, matching the QQ Bot SDK convention.
func ed25519KeyFromSecret(secret string) (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += seed
	}
	seed = seed[:ed25519.SeedSize]
	reader := strings.NewReader(seed)
	pub, priv, _ := ed25519.GenerateKey(reader)
	return pub, priv
}

// ---------------------------------------------------------------------------
// C2C message handling (shared by WebSocket and Webhook paths)
// ---------------------------------------------------------------------------

func (p *Plugin) handleC2CMessage(data json.RawMessage) {
	var event struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			UserOpenID string `json:"user_openid"`
		} `json:"author"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("[qqbot] parse C2C message failed: %v", err)
		return
	}

	openID := event.Author.UserOpenID
	text := strings.TrimSpace(event.Content)
	if openID == "" || text == "" {
		return
	}

	log.Printf("[qqbot] C2C from %s: %s", openID, truncate(text, 80))

	// Handle /unbind command
	if strings.EqualFold(text, "/unbind") {
		p.handleUnbind(openID)
		return
	}

	// Handle binding flow first
	if p.handleBindingFlow(openID, text) {
		return
	}

	// Check if user is bound
	p.bindMu.RLock()
	_, bound := p.bindings[openID]
	p.bindMu.RUnlock()

	if !bound {
		ctx := context.Background()
		_ = p.sendC2CMessage(ctx, openID,
			"👋 欢迎使用 MaClaw QQ Bot！\n\n"+
				"请先绑定您的 Hub 账号，发送您的注册邮箱地址即可开始绑定。")
		return
	}

	// Dispatch to IM Adapter via registered handler
	p.mu.Lock()
	handler := p.messageHandler
	p.mu.Unlock()

	if handler == nil {
		log.Printf("[qqbot] no message handler registered")
		return
	}

	handler(im.IncomingMessage{
		PlatformName: "qqbot",
		PlatformUID:  openID,
		MessageType:  "text",
		Text:         text,
		RawPayload:   data,
		Timestamp:    time.Now(),
	})
}

// ---------------------------------------------------------------------------
// Email binding flow (same pattern as Feishu plugin)
// ---------------------------------------------------------------------------

func (p *Plugin) handleBindingFlow(openID, text string) bool {
	p.pendingMu.Lock()
	pb, hasPending := p.pending[openID]
	// Clean up if expired while we hold the lock.
	if hasPending && pb.Expiry.Before(time.Now()) {
		delete(p.pending, openID)
		hasPending = false
		pb = nil
	}
	p.pendingMu.Unlock()

	if hasPending && pb != nil && isVerifyCode(text) {
		return p.handleVerifyCode(openID, text, pb)
	}

	if looksLikeEmail(text) {
		p.bindMu.RLock()
		_, bound := p.bindings[openID]
		p.bindMu.RUnlock()
		if bound {
			return false // already bound, pass through as normal message
		}
		p.handleEmailSubmit(openID, text)
		return true
	}

	if hasPending && pb != nil {
		ctx := context.Background()
		_ = p.sendC2CMessage(ctx, openID, "请输入您收到的 6 位验证码完成绑定。")
		return true
	}

	return false
}

func (p *Plugin) handleEmailSubmit(openID, email string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := p.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		_ = p.sendC2CMessage(ctx, openID,
			fmt.Sprintf("❌ 未找到邮箱 %s 对应的 Hub 用户，请确认邮箱是否正确。", email))
		return
	}

	code := generateCode()
	p.pendingMu.Lock()
	p.pending[openID] = &pendingBind{
		Email:  email,
		Code:   code,
		Expiry: time.Now().Add(5 * time.Minute),
	}
	p.pendingMu.Unlock()

	// Use cross-IM broadcaster if available, otherwise fall back to email-only.
	if p.broadcaster != nil {
		sentTo, err := p.broadcaster.BroadcastVerifyCode(ctx, email, code, "qqbot")
		if err != nil {
			log.Printf("[qqbot] broadcast verification code for %s failed: %v", email, err)
			_ = p.sendC2CMessage(ctx, openID,
				fmt.Sprintf("❌ 验证码发送失败: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, openID)
			p.pendingMu.Unlock()
			return
		}
		_ = p.sendC2CMessage(ctx, openID,
			fmt.Sprintf("📧 验证码已发送到: %s\n\n请查看验证码，回复给我完成绑定（5 分钟内有效）。", sentTo))
		return
	}

	// Fallback: email-only
	subject := "MaClaw QQ Bot 绑定验证码"
	body := fmt.Sprintf(
		"您好，\r\n\r\n您正在将 QQ 账号绑定到 MaClaw Hub。\r\n\r\n验证码: %s\r\n\r\n请在 5 分钟内将此验证码回复给 QQ Bot 完成绑定。\r\n如非本人操作，请忽略此邮件。\r\n",
		code,
	)
	if p.mailer != nil {
		if err := p.mailer.Send(ctx, []string{email}, subject, body); err != nil {
			log.Printf("[qqbot] send verification email to %s failed: %v", email, err)
			_ = p.sendC2CMessage(ctx, openID,
				fmt.Sprintf("❌ 验证邮件发送失败，请确认 Hub 邮件服务已配置。\n错误: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, openID)
			p.pendingMu.Unlock()
			return
		}
	} else {
		log.Printf("[qqbot] mailer not configured, cannot send verification code")
		_ = p.sendC2CMessage(ctx, openID, "❌ Hub 邮件服务未配置，无法发送验证码。请联系管理员。")
		p.pendingMu.Lock()
		delete(p.pending, openID)
		p.pendingMu.Unlock()
		return
	}

	_ = p.sendC2CMessage(ctx, openID,
		fmt.Sprintf("📧 验证码已发送到邮箱: %s\n\n请查收邮件，将 6 位验证码回复给我完成绑定（5 分钟内有效）。", email))
}

func (p *Plugin) handleVerifyCode(openID, code string, pb *pendingBind) bool {
	ctx := context.Background()

	if pb.Expiry.Before(time.Now()) {
		p.pendingMu.Lock()
		delete(p.pending, openID)
		p.pendingMu.Unlock()
		_ = p.sendC2CMessage(ctx, openID, "⏰ 验证码已过期，请重新发送邮箱地址。")
		return true
	}

	if strings.TrimSpace(code) != pb.Code {
		_ = p.sendC2CMessage(ctx, openID, "❌ 验证码不正确，请重新输入。")
		return true
	}

	p.bindMu.Lock()
	p.bindings[openID] = pb.Email
	p.bindMu.Unlock()
	p.saveBindings()

	p.pendingMu.Lock()
	delete(p.pending, openID)
	p.pendingMu.Unlock()

	_ = p.sendC2CMessage(ctx, openID,
		fmt.Sprintf("✅ 绑定成功！\n\n邮箱: %s\n\n现在您可以直接发送消息与 MaClaw Agent 交互了。", pb.Email))
	return true
}

// ---------------------------------------------------------------------------
// Bindings persistence
// ---------------------------------------------------------------------------

const qqbotBindingsKey = "qqbot_bindings"

func (p *Plugin) loadBindings() {
	raw, err := p.system.Get(context.Background(), qqbotBindingsKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil {
		p.bindMu.Lock()
		p.bindings = m
		p.bindMu.Unlock()
	}
}

func (p *Plugin) saveBindings() {
	p.bindMu.RLock()
	data, _ := json.Marshal(p.bindings)
	p.bindMu.RUnlock()
	_ = p.system.Set(context.Background(), qqbotBindingsKey, string(data))
}

// GetBindings returns the current openid→email bindings (for admin API).
func (p *Plugin) GetBindings() map[string]string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	m := make(map[string]string, len(p.bindings))
	for k, v := range p.bindings {
		m[k] = v
	}
	return m
}

// handleUnbind removes the email binding for a QQ Bot user.
func (p *Plugin) handleUnbind(openID string) {
	p.bindMu.RLock()
	email, bound := p.bindings[openID]
	p.bindMu.RUnlock()
	if !bound || email == "" {
		ctx := context.Background()
		_ = p.sendC2CMessage(ctx, openID, "当前未绑定任何账号。\nNo account is currently bound.")
		return
	}
	p.RemoveBinding(openID)
	log.Printf("[qqbot] unbound email=%s for open_id=%s", email, openID)
	ctx := context.Background()
	_ = p.sendC2CMessage(ctx, openID, fmt.Sprintf("✅ 已解除 %s 的绑定。\n✅ Unbound %s.", email, email))
}

// RemoveBinding removes an openid→email binding.
func (p *Plugin) RemoveBinding(openID string) {
	p.bindMu.Lock()
	delete(p.bindings, openID)
	p.bindMu.Unlock()
	p.saveBindings()
}

// LookupByEmail returns the QQ openid bound to the given email, or "".
// Implements im.BindingLookup for cross-IM verification.
func (p *Plugin) LookupByEmail(email string) string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	for openID, e := range p.bindings {
		if e == email {
			return openID
		}
	}
	return ""
}

// RemoveBindingByEmail removes all openid→email bindings for the given email.
func (p *Plugin) RemoveBindingByEmail(email string) {
	p.bindMu.Lock()
	var removed bool
	for openID, e := range p.bindings {
		if strings.EqualFold(e, email) {
			delete(p.bindings, openID)
			removed = true
		}
	}
	p.bindMu.Unlock()
	if removed {
		p.saveBindings()
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.Index(s, "@")
	dot := strings.LastIndex(s, ".")
	return at > 0 && dot > at && !strings.ContainsAny(s, " \t\n")
}

func isVerifyCode(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func generateCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// ---------------------------------------------------------------------------
// Temporary file store for large file uploads via URL mode
// ---------------------------------------------------------------------------

// storeTempFile decodes base64 data, stores it with a random token, and
// returns a publicly accessible URL. The entry expires after tempFileTTL.
func (p *Plugin) storeTempFile(base64Data, mimeType string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	token := generateTempToken()

	p.tempMu.Lock()
	p.tempFiles[token] = &tempFileEntry{
		Data:      raw,
		MimeType:  mimeType,
		ExpiresAt: time.Now().Add(tempFileTTL),
	}
	// Inline cleanup of any expired entries while we hold the lock.
	now := time.Now()
	for k, v := range p.tempFiles {
		if now.After(v.ExpiresAt) {
			delete(p.tempFiles, k)
		}
	}
	p.tempMu.Unlock()

	return fmt.Sprintf("%s/api/qqbot/tempfile/%s", p.publicBaseURL, token), nil
}

func generateTempToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ServeTempFile is the HTTP handler for GET /api/qqbot/tempfile/{token}.
// QQ's server fetches the file from this URL during the upload flow.
func (p *Plugin) ServeTempFile(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	p.tempMu.Lock()
	entry, ok := p.tempFiles[token]
	p.tempMu.Unlock()

	if !ok {
		http.Error(w, "file not found or expired", http.StatusNotFound)
		return
	}
	if time.Now().After(entry.ExpiresAt) {
		// Lazy cleanup.
		p.tempMu.Lock()
		delete(p.tempFiles, token)
		p.tempMu.Unlock()
		http.Error(w, "file expired", http.StatusGone)
		return
	}

	w.Header().Set("Content-Type", entry.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(entry.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Data)
}
