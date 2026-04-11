// Package dingtalk implements the im.IMPlugin interface for DingTalk (钉钉) Bot.
//
// Protocol: DingTalk Stream Mode (企业内部机器人)
//   - Register endpoint: POST https://api.dingtalk.com/v1.0/gateway/connections/open
//   - AccessToken: POST https://api.dingtalk.com/v1.0/oauth2/accessToken
//   - WebSocket: connect to endpoint returned by connections/open
//   - Inbound: type=CALLBACK, topic=/v1.0/im/bot/messages/get (bot messages)
//   - Outbound: POST to sessionWebhookUrl from callback (markdown reply)
//   - Heartbeat: server sends ping, client replies pong
//   - No public IP required — Stream Mode uses outbound WebSocket
package dingtalk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	tokenEndpoint    = "https://api.dingtalk.com/v1.0/oauth2/accessToken"
	registerEndpoint = "https://api.dingtalk.com/v1.0/gateway/connections/open"

	// Stream callback topics
	topicBotMessages = "/v1.0/im/bot/messages/get"

	wsReconnectBaseDelay = 3 * time.Second
	wsReconnectMaxDelay  = 60 * time.Second
	wsMaxReconnects      = 50

	textChunkLimit = 20000 // DingTalk markdown limit

	// Proactive messaging endpoint (single-chat batch send)
	batchSendEndpoint = "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config holds DingTalk Bot credentials.
type Config struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`     // AppKey
	ClientSecret string `json:"client_secret"` // AppSecret
}

// ConfigProvider returns the current DingTalk config (read from DB).
type ConfigProvider func() Config

// ---------------------------------------------------------------------------
// Stream protocol types
// ---------------------------------------------------------------------------

// streamRegisterResp is the response from connections/open.
type streamRegisterResp struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

// streamFrame is the DingTalk Stream WebSocket frame.
type streamFrame struct {
	SpecVersion string          `json:"specVersion,omitempty"`
	Type        string          `json:"type"`        // SYSTEM, CALLBACK, PING
	Headers     streamHeaders   `json:"headers"`
	Data        json.RawMessage `json:"data,omitempty"`
}

type streamHeaders struct {
	AppID           string `json:"appId,omitempty"`
	ConnectionID    string `json:"connectionId,omitempty"`
	ContentType     string `json:"contentType,omitempty"`
	MessageID       string `json:"messageId,omitempty"`
	Time            string `json:"time,omitempty"`
	Topic           string `json:"topic,omitempty"`
}

// streamAck is the acknowledgement sent back for CALLBACK frames.
type streamAck struct {
	Code    int             `json:"code"`
	Headers streamHeaders   `json:"headers"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// botMessageData is the data payload for bot message callbacks.
type botMessageData struct {
	ConversationID     string `json:"conversationId"`
	AtUsers            []struct {
		DingtalkID string `json:"dingtalkId"`
	} `json:"atUsers,omitempty"`
	ChatbotCorpID      string `json:"chatbotCorpId,omitempty"`
	ChatbotUserID      string `json:"chatbotUserId,omitempty"`
	MsgID              string `json:"msgId"`
	SenderNick         string `json:"senderNick"`
	IsAdmin            bool   `json:"isAdmin,omitempty"`
	SenderStaffID      string `json:"senderStaffId,omitempty"`
	SessionWebhook     string `json:"sessionWebhook"`
	WebhookExpiredTime int64  `json:"sessionWebhookExpiredTime,omitempty"`
	CreateAt           string `json:"createAt,omitempty"`
	SenderCorpID       string `json:"senderCorpId,omitempty"`
	ConversationType   string `json:"conversationType"` // "1" = private, "2" = group
	SenderID           string `json:"senderId"`
	ConversationTitle  string `json:"conversationTitle,omitempty"`
	IsInAtList         bool   `json:"isInAtList,omitempty"`
	Text               *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Msgtype string `json:"msgtype"` // text, richText, picture, audio, video, file
	RichText *struct {
		RichText [][]struct {
			Text    string `json:"text,omitempty"`
			Tag     string `json:"tag,omitempty"`
			PicURL  string `json:"pictureUrl,omitempty"`
			DownURL string `json:"downloadUrl,omitempty"`
		} `json:"richText,omitempty"`
	} `json:"richText,omitempty"`
	// Media message fields (picture, audio, video, file)
	Content *struct {
		DownloadCode string `json:"downloadCode,omitempty"`
		// picture-specific
		PicURL string `json:"pictureDownloadUrl,omitempty"`
		// audio-specific
		Duration    string `json:"duration,omitempty"`
		Recognition string `json:"recognition,omitempty"` // speech-to-text result
		// file-specific
		FileName string `json:"fileName,omitempty"`
		FileSize string `json:"fileSize,omitempty"`
	} `json:"content,omitempty"`
	// robotCode is needed for the download API
	RobotCode string `json:"robotCode,omitempty"`
	// Quote/reply message content
	QuoteContent *struct {
		QuoteMsgID string `json:"quoteMsgId,omitempty"`
		QuoteMsg   string `json:"quoteMsg,omitempty"` // original message text being replied to
	} `json:"quoteContent,omitempty"`
}

// ---------------------------------------------------------------------------
// Plugin
// ---------------------------------------------------------------------------

// Mailer is the interface for sending emails.
type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

// NotifyBroadcaster sends verification codes to all reachable channels.
type NotifyBroadcaster interface {
	BroadcastVerifyCode(ctx context.Context, email, code, excludePlatform string) (sentTo string, err error)
}

// Plugin implements im.IMPlugin for DingTalk Bot (Stream Mode).
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

	// staffId → email bindings (persisted in system settings)
	bindMu   sync.RWMutex
	bindings map[string]string // senderStaffId → email

	// pending verification codes: staffId → {email, code, expiry}
	pendingMu sync.Mutex
	pending   map[string]*pendingBind

	// WebSocket gateway
	wsCancel context.CancelFunc
	wsWg     sync.WaitGroup
	wsMu     sync.Mutex // serialises WS writes

	// message dedup
	dedupMu        sync.Mutex
	dedupSeen      map[string]time.Time
	dedupLastEvict time.Time

	// sessionWebhook cache (staffId → {url, expiry})
	whMu    sync.RWMutex
	whCache map[string]webhookEntry

	publicBaseURL string
}

type webhookEntry struct {
	URL    string
	Expiry time.Time
}

type pendingBind struct {
	Email  string
	Code   string
	Expiry time.Time
}

// New creates a DingTalk plugin.
func New(provider ConfigProvider, users store.UserRepository, system store.SystemSettingsRepository, mailer Mailer) *Plugin {
	p := &Plugin{
		configProvider: provider,
		users:          users,
		system:         system,
		mailer:         mailer,
		client:         &http.Client{Timeout: 30 * time.Second},
		bindings:       make(map[string]string),
		pending:        make(map[string]*pendingBind),
		dedupSeen:      make(map[string]time.Time),
		whCache:        make(map[string]webhookEntry),
	}
	p.loadBindings()
	return p
}

func (p *Plugin) SetPublicBaseURL(url string) {
	p.publicBaseURL = strings.TrimRight(url, "/")
}

func (p *Plugin) SetBroadcaster(b NotifyBroadcaster) {
	p.broadcaster = b
}

// ---------------------------------------------------------------------------
// im.IMPlugin interface
// ---------------------------------------------------------------------------

func (p *Plugin) Name() string { return "dingtalk" }

func (p *Plugin) ReceiveMessage(handler func(msg im.IncomingMessage)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageHandler = handler
}

func (p *Plugin) SendText(ctx context.Context, target im.UserTarget, text string) error {
	staffID := target.PlatformUID
	if staffID == "" {
		return fmt.Errorf("dingtalk: PlatformUID (staffId) is required")
	}
	// Try sessionWebhook first (fast, no extra API call)
	if err := p.replyViaWebhook(staffID, text); err == nil {
		return nil
	}
	// Fall back to proactive send via OpenAPI
	return p.sendProactive(ctx, staffID, text)
}

func (p *Plugin) SendCard(ctx context.Context, target im.UserTarget, card im.OutgoingMessage) error {
	text := card.FallbackText
	if text == "" {
		var sb strings.Builder
		if card.Title != "" {
			sb.WriteString("### ")
			sb.WriteString(card.Title)
			sb.WriteString("\n\n")
		}
		if card.StatusIcon != "" {
			sb.WriteString(card.StatusIcon)
			sb.WriteString(" ")
		}
		if card.Body != "" {
			sb.WriteString(card.Body)
		}
		for _, f := range card.Fields {
			sb.WriteString("\n\n**")
			sb.WriteString(f.Label)
			sb.WriteString("**: ")
			sb.WriteString(f.Value)
		}
		text = sb.String()
	}
	return p.SendText(ctx, target, text)
}

func (p *Plugin) SendImage(ctx context.Context, target im.UserTarget, imageKey string, caption string) error {
	text := caption
	if text == "" {
		text = "[图片]"
	}
	return p.SendText(ctx, target, text)
}

func (p *Plugin) SendFile(ctx context.Context, target im.UserTarget, fileData, fileName, mimeType string) error {
	return p.SendText(ctx, target, fmt.Sprintf("📎 %s", fileName))
}

func (p *Plugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	p.bindMu.RLock()
	email, ok := p.bindings[platformUID]
	p.bindMu.RUnlock()
	if !ok || email == "" {
		return "", fmt.Errorf("dingtalk: user %s not bound, please send your email to bind", platformUID)
	}
	user, err := p.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		return "", fmt.Errorf("dingtalk: no hub user found for email %s", email)
	}
	return user.ID, nil
}

func (p *Plugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{
		SupportsRichCard:    false,
		SupportsMarkdown:    true,
		SupportsImage:       false,
		SupportsFile:        false,
		SupportsButton:      false,
		SupportsMessageEdit: false,
		MaxTextLength:       textChunkLimit,
	}
}

func (p *Plugin) Start(ctx context.Context) error {
	cfg := p.configProvider()
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		log.Printf("[dingtalk] not configured, skipping Stream gateway")
		return nil
	}
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	wsCtx, cancel := context.WithCancel(context.Background())
	p.wsCancel = cancel
	p.wsWg.Add(1)
	go p.runGateway(wsCtx)
	p.startCacheCleanup(wsCtx)
	log.Printf("[dingtalk] started (Stream gateway launched)")
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	log.Printf("[dingtalk] stopped")
	return nil
}

// ---------------------------------------------------------------------------
// AccessToken management
// ---------------------------------------------------------------------------

func (p *Plugin) getAccessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	if p.accessToken != "" && time.Now().Before(p.tokenExpires) {
		return p.accessToken, nil
	}

	cfg := p.configProvider()
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return "", fmt.Errorf("dingtalk: credentials not configured")
	}

	body, _ := json.Marshal(map[string]string{
		"appKey":    cfg.ClientID,
		"appSecret": cfg.ClientSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("dingtalk: token request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("dingtalk: token decode failed: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("dingtalk: empty access token returned")
	}

	p.accessToken = result.AccessToken
	// Refresh 5 minutes before expiry
	p.tokenExpires = time.Now().Add(time.Duration(result.ExpireIn)*time.Second - 5*time.Minute)
	log.Printf("[dingtalk] access token refreshed, expires in %ds", result.ExpireIn)
	return p.accessToken, nil
}

// ---------------------------------------------------------------------------
// Stream Gateway — register + WebSocket connect
// ---------------------------------------------------------------------------

// registerStream calls the DingTalk gateway registration endpoint to get
// a WebSocket URL and ticket for the Stream connection.
func (p *Plugin) registerStream(ctx context.Context) (*streamRegisterResp, error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	cfg := p.configProvider()
	body, _ := json.Marshal(map[string]any{
		"clientId": cfg.ClientID,
		"subscriptions": []map[string]string{
			{"type": "CALLBACK", "topic": topicBotMessages},
		},
		"ua": "maclaw-hub",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: register stream failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("dingtalk: register stream HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result streamRegisterResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("dingtalk: register stream decode failed: %w", err)
	}
	if result.Endpoint == "" {
		return nil, fmt.Errorf("dingtalk: empty endpoint from register")
	}
	return &result, nil
}

func (p *Plugin) runGateway(ctx context.Context) {
	defer p.wsWg.Done()

	reconnects := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		connected, err := p.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}

		// Reset counter if we had a successful connection that later dropped.
		if connected {
			reconnects = 0
		}

		reconnects++
		if reconnects > wsMaxReconnects {
			log.Printf("[dingtalk/ws] max reconnect attempts (%d) reached, giving up", wsMaxReconnects)
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
			log.Printf("[dingtalk/ws] connection error: %v, reconnecting in %v (attempt %d/%d)", err, delay, reconnects, wsMaxReconnects)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (p *Plugin) connectAndRun(ctx context.Context) (connected bool, err error) {
	reg, err := p.registerStream(ctx)
	if err != nil {
		return false, fmt.Errorf("register: %w", err)
	}

	// Build WebSocket URL with ticket (URL-encode the ticket value)
	wsURL := reg.Endpoint + "?ticket=" + url.QueryEscape(reg.Ticket)
	log.Printf("[dingtalk/ws] connecting to stream endpoint")

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	connected = true

	wsWrite := func(v any) error {
		p.wsMu.Lock()
		defer p.wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Read loop
	for {
		select {
		case <-ctx.Done():
			return connected, nil
		default:
		}

		var frame streamFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return connected, fmt.Errorf("read: %w", err)
		}

		switch frame.Type {
		case "SYSTEM":
			// Connection established confirmation
			log.Printf("[dingtalk/ws] SYSTEM: connectionId=%s", frame.Headers.ConnectionID)

		case "PING":
			// Reply with PONG
			pong := map[string]any{
				"code":    200,
				"headers": frame.Headers,
				"message": "OK",
			}
			if err := wsWrite(pong); err != nil {
				log.Printf("[dingtalk/ws] pong send error: %v", err)
			}

		case "CALLBACK":
			// Message callback — process and ACK
			cfg := p.configProvider()
			if !cfg.Enabled {
				// ACK but don't process
				ack := streamAck{Code: 200, Headers: frame.Headers, Message: "OK"}
				_ = wsWrite(ack)
				continue
			}
			// ACK immediately (DingTalk requires fast ACK)
			ack := streamAck{Code: 200, Headers: frame.Headers, Message: "OK"}
			_ = wsWrite(ack)

			if frame.Headers.Topic == topicBotMessages {
				go p.handleBotMessage(frame.Data)
			} else {
				log.Printf("[dingtalk/ws] unknown callback topic: %s", frame.Headers.Topic)
			}

		default:
			log.Printf("[dingtalk/ws] unknown frame type: %s", frame.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// Inbound message handling
// ---------------------------------------------------------------------------

const msgDedupTTL = 5 * time.Minute

func (p *Plugin) isDuplicateMsg(msgID string) bool {
	if msgID == "" {
		return false
	}
	now := time.Now()
	p.dedupMu.Lock()
	defer p.dedupMu.Unlock()

	if now.Sub(p.dedupLastEvict) > time.Minute {
		for id, t := range p.dedupSeen {
			if now.Sub(t) > msgDedupTTL {
				delete(p.dedupSeen, id)
			}
		}
		p.dedupLastEvict = now
	}

	if _, exists := p.dedupSeen[msgID]; exists {
		return true
	}
	p.dedupSeen[msgID] = now
	return false
}

// webhookTTL: DingTalk sessionWebhook expires after ~2 hours; we use 90 min.
const webhookTTL = 90 * time.Minute

func (p *Plugin) cacheWebhook(staffID, webhook string) {
	p.cacheWebhookWithTTL(staffID, webhook, webhookTTL)
}

func (p *Plugin) cacheWebhookWithTTL(staffID, webhook string, ttl time.Duration) {
	if staffID == "" || webhook == "" {
		return
	}
	p.whMu.Lock()
	p.whCache[staffID] = webhookEntry{URL: webhook, Expiry: time.Now().Add(ttl)}
	p.whMu.Unlock()
}

func (p *Plugin) getWebhook(staffID string) string {
	p.whMu.RLock()
	entry, ok := p.whCache[staffID]
	p.whMu.RUnlock()
	if !ok || time.Now().After(entry.Expiry) {
		return ""
	}
	return entry.URL
}

func (p *Plugin) handleBotMessage(data json.RawMessage) {
	var msg botMessageData
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[dingtalk] parse bot message failed: %v", err)
		return
	}

	// Use senderStaffId as the platform UID (stable within the org)
	staffID := msg.SenderStaffID
	if staffID == "" {
		staffID = msg.SenderID // fallback
	}
	if staffID == "" {
		log.Printf("[dingtalk] message with no sender ID, skipping")
		return
	}

	// Group chat: only process if bot was @mentioned
	if msg.ConversationType == "2" && !msg.IsInAtList {
		return
	}

	// Dedup
	if p.isDuplicateMsg(msg.MsgID) {
		log.Printf("[dingtalk] duplicate msgId=%s, skipping", msg.MsgID)
		return
	}

	// Cache the sessionWebhook for replies (use server-provided expiry if available)
	whExpiry := webhookTTL
	if msg.WebhookExpiredTime > 0 {
		serverExpiry := time.Until(time.UnixMilli(msg.WebhookExpiredTime))
		if serverExpiry > time.Minute {
			whExpiry = serverExpiry - time.Minute // 1 min safety margin
		}
	}
	p.cacheWebhookWithTTL(staffID, msg.SessionWebhook, whExpiry)

	// Extract text
	text := p.extractText(&msg)

	// Strip @bot mention prefix (DingTalk prepends "@BotName " in group chats)
	if msg.ConversationType == "2" {
		text = stripAtMention(text)
	}

	// Prepend quoted message content for context (reply/引用 messages)
	if msg.QuoteContent != nil && msg.QuoteContent.QuoteMsg != "" {
		quoted := strings.TrimSpace(msg.QuoteContent.QuoteMsg)
		if quoted != "" {
			text = "> " + strings.ReplaceAll(quoted, "\n", "\n> ") + "\n\n" + text
		}
	}

	// For pure-text messages, skip if empty. Media messages may have no text
	// but will be handled via attachments later.
	isMediaMsg := msg.Msgtype == "picture" || msg.Msgtype == "audio" || msg.Msgtype == "video" || msg.Msgtype == "file"
	if text == "" && !isMediaMsg {
		log.Printf("[dingtalk] empty message from %s (%s), skipping", staffID, msg.SenderNick)
		return
	}

	log.Printf("[dingtalk] message from %s (%s) conv=%s: %s",
		staffID, msg.SenderNick, msg.ConversationType, truncate(text, 80))

	// Handle stop/interrupt commands
	trimmed := strings.TrimSpace(text)
	if isStopCommand(trimmed) {
		// Route as /stop command to the IM adapter
		text = "/stop"
	}

	// Handle /unbind command
	if strings.EqualFold(trimmed, "/unbind") {
		p.handleUnbind(staffID)
		return
	}

	// Handle binding flow
	if p.handleBindingFlow(staffID, text) {
		return
	}

	// Check if user is bound
	p.bindMu.RLock()
	_, bound := p.bindings[staffID]
	p.bindMu.RUnlock()

	if !bound {
		_ = p.replyViaWebhook(staffID,
			"👋 欢迎使用 MaClaw 钉钉 Bot！\n\n"+
				"请先绑定您的 Hub 账号，发送您的注册邮箱地址即可开始绑定。")
		return
	}

	// Dispatch to IM Adapter
	p.mu.Lock()
	handler := p.messageHandler
	p.mu.Unlock()

	if handler == nil {
		log.Printf("[dingtalk] no message handler registered")
		return
	}

	// Extract media attachments (picture, audio, video, file)
	attachments := p.extractAttachments(&msg)

	// For audio messages with speech recognition, use the recognized text
	if text == "" && msg.Msgtype == "audio" && msg.Content != nil && msg.Content.Recognition != "" {
		text = msg.Content.Recognition
		log.Printf("[dingtalk] using audio recognition text: %s", truncate(text, 80))
	}

	// Determine message type
	msgType := "text"
	if len(attachments) > 0 {
		msgType = attachments[0].Type // "image", "audio", "file", "video"
	}

	// If still no text and no attachments, nothing to dispatch
	if text == "" && len(attachments) == 0 {
		log.Printf("[dingtalk] no text or attachments from %s, skipping", staffID)
		return
	}

	handler(im.IncomingMessage{
		PlatformName: "dingtalk",
		PlatformUID:  staffID,
		MessageID:    msg.MsgID,
		MessageType:  msgType,
		Text:         text,
		Attachments:  attachments,
		RawPayload:   data,
		Timestamp:    time.Now(),
	})
}

func (p *Plugin) extractText(msg *botMessageData) string {
	switch msg.Msgtype {
	case "text":
		if msg.Text != nil {
			return strings.TrimSpace(msg.Text.Content)
		}
	case "richText":
		if msg.RichText != nil {
			var parts []string
			for _, section := range msg.RichText.RichText {
				for _, item := range section {
					if item.Text != "" {
						parts = append(parts, item.Text)
					}
				}
			}
			return strings.TrimSpace(strings.Join(parts, "\n"))
		}
	}
	return ""
}

// stripAtMention removes the leading "@BotName " prefix that DingTalk
// prepends in group chat messages when the bot is @mentioned.
func stripAtMention(text string) string {
	// DingTalk format: "@BotName rest of message" or just "@BotName"
	// The @mention is always at the start for isInAtList messages.
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "@") {
		return text
	}
	// Find the end of the @mention (first space or end of string)
	idx := strings.IndexByte(text, ' ')
	if idx < 0 {
		return "" // entire message is just "@BotName"
	}
	return strings.TrimSpace(text[idx+1:])
}

// isStopCommand checks if the message is a stop/interrupt command.
// Matches: 停止, stop, /stop, esc, 取消, cancel, /cancel
func isStopCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch lower {
	case "停止", "stop", "/stop", "esc", "取消", "cancel", "/cancel":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Media attachment download via DingTalk OpenAPI
// ---------------------------------------------------------------------------

const (
	downloadEndpoint = "https://api.dingtalk.com/v1.0/robot/messageFiles/download"
	maxDownloadSize  = im.MaxAttachmentSize // 10 MB
)

// extractAttachments downloads media files referenced by downloadCode in the
// message and returns them as im.MessageAttachment slices.
func (p *Plugin) extractAttachments(msg *botMessageData) []im.MessageAttachment {
	if msg.Content == nil || msg.Content.DownloadCode == "" {
		return nil
	}

	attType, mimeType, fileName := classifyMediaType(msg)
	if attType == "" {
		return nil
	}

	// For audio with speech recognition, we can use the text directly
	// and skip the download — the IM adapter handles text just fine.
	// But we still download the audio for the agent to have the raw file.

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fileData, err := p.downloadMediaFile(ctx, msg.Content.DownloadCode, msg.RobotCode)
	if err != nil {
		log.Printf("[dingtalk] download %s failed (code=%s): %v", attType, msg.Content.DownloadCode[:min(16, len(msg.Content.DownloadCode))], err)
		return nil
	}

	if int64(len(fileData)) > maxDownloadSize {
		log.Printf("[dingtalk] %s too large (%d bytes), skipping", attType, len(fileData))
		return nil
	}

	att := im.MessageAttachment{
		Type:     attType,
		FileName: fileName,
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(fileData),
		Size:     int64(len(fileData)),
	}
	log.Printf("[dingtalk] downloaded %s: %s (%s, %d bytes)", attType, fileName, mimeType, len(fileData))
	return []im.MessageAttachment{att}
}

// classifyMediaType returns (type, mimeType, fileName) based on msgtype.
func classifyMediaType(msg *botMessageData) (string, string, string) {
	switch msg.Msgtype {
	case "picture":
		return "image", "image/jpeg", "image.jpg"
	case "audio":
		return "audio", "audio/amr", "voice.amr"
	case "video":
		return "video", "video/mp4", "video.mp4"
	case "file":
		name := "file"
		if msg.Content != nil && msg.Content.FileName != "" {
			name = msg.Content.FileName
		}
		return "file", "application/octet-stream", name
	}
	return "", "", ""
}

// downloadMediaFile calls the DingTalk robot/messageFiles/download API
// to exchange a downloadCode for the actual file bytes.
func (p *Plugin) downloadMediaFile(ctx context.Context, downloadCode, robotCode string) ([]byte, error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	// If robotCode is empty, use the clientId as fallback (they're the same for internal apps)
	if robotCode == "" {
		robotCode = p.configProvider().ClientID
	}

	body, _ := json.Marshal(map[string]string{
		"downloadCode": downloadCode,
		"robotCode":    robotCode,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, downloadEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("download API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// The API returns JSON with a downloadUrl field
	var result struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode download response: %w", err)
	}
	if result.DownloadURL == "" {
		return nil, fmt.Errorf("empty downloadUrl in response")
	}

	// Now fetch the actual file
	return p.fetchFileBytes(ctx, result.DownloadURL)
}

// fetchFileBytes downloads raw bytes from a URL with size limit.
func (p *Plugin) fetchFileBytes(ctx context.Context, fileURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch file HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
}

// ---------------------------------------------------------------------------
// Reply via sessionWebhook (with chunked sending for long texts)
// ---------------------------------------------------------------------------

// sendChunkLimit is the safe per-message limit for sessionWebhook replies.
// DingTalk's actual limit is ~20000 chars, but we use a smaller chunk size
// to leave room for markdown overhead and ensure reliable delivery.
const sendChunkLimit = 8000

func (p *Plugin) replyViaWebhook(staffID, text string) error {
	webhook := p.getWebhook(staffID)
	if webhook == "" {
		log.Printf("[dingtalk] no valid sessionWebhook for %s, reply dropped", staffID)
		return fmt.Errorf("dingtalk: no sessionWebhook cached for %s", staffID)
	}

	runes := []rune(text)
	if len(runes) <= sendChunkLimit {
		return p.sendOneMarkdown(webhook, text)
	}

	// Split into chunks at line boundaries when possible
	chunks := splitTextChunks(text, sendChunkLimit)
	for i, chunk := range chunks {
		if i > 0 {
			time.Sleep(300 * time.Millisecond) // rate limit between chunks
		}
		if err := p.sendOneMarkdown(webhook, chunk); err != nil {
			log.Printf("[dingtalk] chunk %d/%d send failed: %v", i+1, len(chunks), err)
			return err
		}
	}
	return nil
}

func (p *Plugin) sendOneMarkdown(webhook, text string) error {
	body, _ := json.Marshal(map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": "MaClaw",
			"text":  text,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: webhook reply failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("dingtalk: webhook reply HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// splitTextChunks splits text into chunks of at most maxRunes runes,
// preferring to break at newline boundaries.
func splitTextChunks(text string, maxRunes int) []string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}

	var chunks []string
	for len(runes) > 0 {
		end := maxRunes
		if end > len(runes) {
			end = len(runes)
		}

		// Try to find a newline near the end to break cleanly
		if end < len(runes) {
			bestBreak := -1
			// Search backwards from end for a newline within the last 20% of the chunk
			searchStart := end - end/5
			if searchStart < 0 {
				searchStart = 0
			}
			for i := end - 1; i >= searchStart; i-- {
				if runes[i] == '\n' {
					bestBreak = i + 1 // include the newline in this chunk
					break
				}
			}
			if bestBreak > 0 {
				end = bestBreak
			}
		}

		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

// ---------------------------------------------------------------------------
// Proactive messaging via OpenAPI (when sessionWebhook is expired/unavailable)
// ---------------------------------------------------------------------------

// sendProactive sends a markdown message to a user via the DingTalk
// oToMessages/batchSend API. This works even when no sessionWebhook is
// cached, enabling verification code delivery and device notifications.
func (p *Plugin) sendProactive(ctx context.Context, staffID, text string) error {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("dingtalk proactive: get token: %w", err)
	}

	cfg := p.configProvider()
	robotCode := cfg.ClientID // robotCode = AppKey for internal apps

	// Truncate if needed
	runes := []rune(text)
	if len(runes) > textChunkLimit {
		text = string(runes[:textChunkLimit-3]) + "..."
	}

	// Build msgParam as proper JSON to avoid injection issues
	msgParamObj := map[string]string{"title": "MaClaw", "text": text}
	msgParamBytes, _ := json.Marshal(msgParamObj)

	body, _ := json.Marshal(map[string]any{
		"robotCode": robotCode,
		"userIds":   []string{staffID},
		"msgKey":    "sampleMarkdown",
		"msgParam":  string(msgParamBytes),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, batchSendEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk proactive: send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("dingtalk proactive: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Webhook cache cleanup goroutine
// ---------------------------------------------------------------------------

// startCacheCleanup launches a background goroutine that periodically evicts
// expired entries from the webhook cache and dedup map.
func (p *Plugin) startCacheCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.evictExpiredCaches()
			}
		}
	}()
}

func (p *Plugin) evictExpiredCaches() {
	now := time.Now()

	// Evict expired webhook entries
	p.whMu.Lock()
	for k, entry := range p.whCache {
		if now.After(entry.Expiry) {
			delete(p.whCache, k)
		}
	}
	whSize := len(p.whCache)
	p.whMu.Unlock()

	// Evict expired dedup entries
	p.dedupMu.Lock()
	for k, t := range p.dedupSeen {
		if now.Sub(t) > msgDedupTTL {
			delete(p.dedupSeen, k)
		}
	}
	dedupSize := len(p.dedupSeen)
	p.dedupMu.Unlock()

	if whSize > 0 || dedupSize > 0 {
		log.Printf("[dingtalk] cache cleanup: webhooks=%d, dedup=%d", whSize, dedupSize)
	}
}

// ---------------------------------------------------------------------------
// Email binding flow (same pattern as WeCom / QQBot)
// ---------------------------------------------------------------------------

func (p *Plugin) handleBindingFlow(staffID, text string) bool {
	p.pendingMu.Lock()
	pb, hasPending := p.pending[staffID]
	if hasPending && pb.Expiry.Before(time.Now()) {
		delete(p.pending, staffID)
		hasPending = false
		pb = nil
	}
	p.pendingMu.Unlock()

	if hasPending && pb != nil && isVerifyCode(text) {
		return p.handleVerifyCode(staffID, text, pb)
	}

	if looksLikeEmail(text) {
		p.bindMu.RLock()
		_, bound := p.bindings[staffID]
		p.bindMu.RUnlock()
		if bound {
			return false
		}
		p.handleEmailSubmit(staffID, text)
		return true
	}

	if hasPending && pb != nil {
		_ = p.replyViaWebhook(staffID, "请输入您收到的 6 位验证码完成绑定。")
		return true
	}

	return false
}

func (p *Plugin) handleEmailSubmit(staffID, email string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := p.users.GetByEmail(ctx, email)
	if err != nil || user == nil {
		_ = p.replyViaWebhook(staffID,
			fmt.Sprintf("❌ 未找到邮箱 %s 对应的 Hub 用户，请确认邮箱是否正确。", email))
		return
	}

	code := generateCode()
	p.pendingMu.Lock()
	p.pending[staffID] = &pendingBind{
		Email:  email,
		Code:   code,
		Expiry: time.Now().Add(5 * time.Minute),
	}
	p.pendingMu.Unlock()

	if p.broadcaster != nil {
		sentTo, err := p.broadcaster.BroadcastVerifyCode(ctx, email, code, "dingtalk")
		if err != nil {
			log.Printf("[dingtalk] broadcast verification code for %s failed: %v", email, err)
			_ = p.replyViaWebhook(staffID, fmt.Sprintf("❌ 验证码发送失败: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, staffID)
			p.pendingMu.Unlock()
			return
		}
		_ = p.replyViaWebhook(staffID,
			fmt.Sprintf("📧 验证码已发送到: %s\n\n请查看验证码，回复给我完成绑定（5 分钟内有效）。", sentTo))
		return
	}

	// Fallback: email-only
	subject := "MaClaw 钉钉 Bot 绑定验证码"
	body := fmt.Sprintf(
		"您好，\r\n\r\n您正在将钉钉账号绑定到 MaClaw Hub。\r\n\r\n验证码: %s\r\n\r\n请在 5 分钟内将此验证码回复给钉钉 Bot 完成绑定。\r\n如非本人操作，请忽略此邮件。\r\n",
		code,
	)
	if p.mailer != nil {
		if err := p.mailer.Send(ctx, []string{email}, subject, body); err != nil {
			log.Printf("[dingtalk] send verification email to %s failed: %v", email, err)
			_ = p.replyViaWebhook(staffID, fmt.Sprintf("❌ 验证邮件发送失败: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, staffID)
			p.pendingMu.Unlock()
			return
		}
	} else {
		_ = p.replyViaWebhook(staffID, "❌ Hub 邮件服务未配置，无法发送验证码。请联系管理员。")
		p.pendingMu.Lock()
		delete(p.pending, staffID)
		p.pendingMu.Unlock()
		return
	}

	_ = p.replyViaWebhook(staffID,
		fmt.Sprintf("📧 验证码已发送到邮箱: %s\n\n请查收邮件，将 6 位验证码回复给我完成绑定（5 分钟内有效）。", email))
}

func (p *Plugin) handleVerifyCode(staffID, code string, pb *pendingBind) bool {
	if pb.Expiry.Before(time.Now()) {
		p.pendingMu.Lock()
		delete(p.pending, staffID)
		p.pendingMu.Unlock()
		_ = p.replyViaWebhook(staffID, "⏰ 验证码已过期，请重新发送邮箱地址。")
		return true
	}

	if strings.TrimSpace(code) != pb.Code {
		_ = p.replyViaWebhook(staffID, "❌ 验证码不正确，请重新输入。")
		return true
	}

	p.bindMu.Lock()
	p.bindings[staffID] = pb.Email
	p.bindMu.Unlock()
	p.saveBindings()

	p.pendingMu.Lock()
	delete(p.pending, staffID)
	p.pendingMu.Unlock()

	_ = p.replyViaWebhook(staffID,
		fmt.Sprintf("✅ 绑定成功！\n\n邮箱: %s\n\n现在您可以直接发送消息与 MaClaw Agent 交互了。", pb.Email))
	return true
}

func (p *Plugin) handleUnbind(staffID string) {
	p.bindMu.RLock()
	email, bound := p.bindings[staffID]
	p.bindMu.RUnlock()
	if !bound || email == "" {
		_ = p.replyViaWebhook(staffID, "当前未绑定任何账号。")
		return
	}
	p.RemoveBinding(staffID)
	log.Printf("[dingtalk] unbound email=%s for staffId=%s", email, staffID)
	_ = p.replyViaWebhook(staffID, fmt.Sprintf("✅ 已解除 %s 的绑定。", email))
}

// RemoveBinding removes a staffId→email binding.
func (p *Plugin) RemoveBinding(staffID string) {
	p.bindMu.Lock()
	delete(p.bindings, staffID)
	p.bindMu.Unlock()
	p.saveBindings()
}

// LookupByEmail returns the DingTalk staffId bound to the given email, or "".
func (p *Plugin) LookupByEmail(email string) string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	for uid, e := range p.bindings {
		if strings.EqualFold(e, email) {
			return uid
		}
	}
	return ""
}

// GetBindings returns the current staffId→email bindings (for admin API).
func (p *Plugin) GetBindings() map[string]string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	m := make(map[string]string, len(p.bindings))
	for k, v := range p.bindings {
		m[k] = v
	}
	return m
}

// RemoveBindingByEmail removes all staffId→email bindings for the given email.
func (p *Plugin) RemoveBindingByEmail(email string) {
	p.bindMu.Lock()
	var removed bool
	for uid, e := range p.bindings {
		if strings.EqualFold(e, email) {
			delete(p.bindings, uid)
			removed = true
		}
	}
	p.bindMu.Unlock()
	if removed {
		p.saveBindings()
	}
}

// ---------------------------------------------------------------------------
// Bindings persistence
// ---------------------------------------------------------------------------

const dingtalkBindingsKey = "dingtalk_bindings"

func (p *Plugin) loadBindings() {
	raw, err := p.system.Get(context.Background(), dingtalkBindingsKey)
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
	_ = p.system.Set(context.Background(), dingtalkBindingsKey, string(data))
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
