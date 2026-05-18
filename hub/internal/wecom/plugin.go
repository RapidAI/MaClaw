// Package wecom implements the im.IMPlugin interface for WeCom (企业微信) Bot.
//
// Protocol based on @wecom/aibot-node-sdk WebSocket protocol:
//   - WebSocket endpoint: wss://openws.work.weixin.qq.com
//   - Auth: send aibot_subscribe frame with botId + secret
//   - Inbound: aibot_callback (messages), aibot_event_callback (events)
//   - Outbound: aibot_respond_msg (reply), aibot_send_msg (proactive)
//   - Heartbeat: ping every 30s
//   - Streaming: replyStream with streamId, finish=true to close
//   - Media upload: 3-step chunked upload (init → chunk × N → finish)
//   - File download: HTTP GET + AES-256-CBC decryption
package wecom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultWSURL = "wss://openws.work.weixin.qq.com"

	// WebSocket commands (WsCmd from SDK)
	cmdSubscribe        = "aibot_subscribe"
	cmdPing             = "ping"
	cmdCallback         = "aibot_callback"       // server → client: message
	cmdEventCallback    = "aibot_event_callback" // server → client: event
	cmdRespondMsg       = "aibot_respond_msg"    // client → server: reply
	cmdSendMsg          = "aibot_send_msg"       // client → server: proactive
	cmdUploadMediaInit  = "aibot_upload_media_init"
	cmdUploadMediaChunk = "aibot_upload_media_chunk"
	cmdUploadMediaFin   = "aibot_upload_media_finish"

	heartbeatInterval = 30 * time.Second

	wsReconnectBaseDelay = 3 * time.Second
	wsReconnectMaxDelay  = 30 * time.Second
	wsMaxReconnects      = 10
	wsMaxAuthFailures    = 5

	// Stream expired errcode — server rejects updates after 6 min.
	streamExpiredErrcode = 846608

	textChunkLimit = 4000

	uploadChunkSize = 512 * 1024 // 512 KB per chunk (before base64)

	// Thinking placeholder — WeCom renders this as a typing animation.
	thinkingMessage = "<think></think>"

	// streamStateTTL is how long a stream state is kept before cleanup.
	streamStateTTL = 10 * time.Minute
	// streamStateCleanupInterval is how often expired stream states are reaped.
	streamStateCleanupInterval = time.Minute
)

// ---------------------------------------------------------------------------
// Stream state management — tracks per-message streaming context
// ---------------------------------------------------------------------------

// streamState tracks the streaming reply state for a single inbound message.
type streamState struct {
	reqID       string    // original callback req_id (for replyStream)
	streamID    string    // stream ID for this reply sequence
	platformUID string    // user's platformUID for matching progress delivery
	expired     bool      // true if server returned errcode 846608
	createdAt   time.Time // for TTL cleanup
}

var streamStates = struct {
	mu    sync.Mutex
	items map[string]*streamState // keyed by msgID
}{items: make(map[string]*streamState)}

func getOrCreateStreamState(msgID, reqID, platformUID string) *streamState {
	streamStates.mu.Lock()
	defer streamStates.mu.Unlock()
	if ss, ok := streamStates.items[msgID]; ok {
		return ss
	}
	ss := &streamState{
		reqID:       reqID,
		streamID:    generateReqID("stream"),
		platformUID: platformUID,
		createdAt:   time.Now(),
	}
	streamStates.items[msgID] = ss
	return ss
}

func deleteStreamState(msgID string) {
	streamStates.mu.Lock()
	delete(streamStates.items, msgID)
	streamStates.mu.Unlock()
}

func cleanupStreamStates() {
	now := time.Now()
	streamStates.mu.Lock()
	for id, ss := range streamStates.items {
		if now.Sub(ss.createdAt) > streamStateTTL {
			delete(streamStates.items, id)
		}
	}
	streamStates.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config holds WeCom Bot credentials.
type Config struct {
	Enabled bool   `json:"enabled"`
	BotID   string `json:"bot_id"`
	Secret  string `json:"secret"`
	WSURL   string `json:"ws_url,omitempty"` // override for private deploy
}

// ConfigProvider returns the current WeCom config (read from DB).
type ConfigProvider func() Config

// ---------------------------------------------------------------------------
// WebSocket frame types
// ---------------------------------------------------------------------------

// wsFrame is the WeCom WebSocket frame structure.
type wsFrame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers wsHeaders       `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	Errcode int             `json:"errcode,omitempty"`
	Errmsg  string          `json:"errmsg,omitempty"`
}

type wsHeaders struct {
	ReqID string `json:"req_id"`
}

// callbackBody is the body of an aibot_callback message.
type callbackBody struct {
	MsgID    string `json:"msgid"`
	AibotID  string `json:"aibotid"`
	ChatID   string `json:"chatid,omitempty"`
	ChatType string `json:"chattype"` // "single" or "group"
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	ResponseURL string `json:"response_url,omitempty"`
	MsgType     string `json:"msgtype"` // text, image, voice, video, file, mixed, event
	Text        *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Image *struct {
		URL    string `json:"url,omitempty"`
		AESKey string `json:"aeskey,omitempty"`
	} `json:"image,omitempty"`
	Voice *struct {
		URL    string `json:"url,omitempty"`
		AESKey string `json:"aeskey,omitempty"`
		Text   string `json:"text,omitempty"` // transcribed text
	} `json:"voice,omitempty"`
	File *struct {
		URL    string `json:"url,omitempty"`
		AESKey string `json:"aeskey,omitempty"`
	} `json:"file,omitempty"`
	Video *struct {
		URL    string `json:"url,omitempty"`
		AESKey string `json:"aeskey,omitempty"`
	} `json:"video,omitempty"`
	Mixed *struct {
		MsgItem []struct {
			MsgType string `json:"msgtype"`
			Text    *struct {
				Content string `json:"content"`
			} `json:"text,omitempty"`
			Image *struct {
				URL    string `json:"url,omitempty"`
				AESKey string `json:"aeskey,omitempty"`
			} `json:"image,omitempty"`
		} `json:"msg_item"`
	} `json:"mixed,omitempty"`
	Quote *struct {
		MsgType string `json:"msgtype"`
		Text    *struct {
			Content string `json:"content"`
		} `json:"text,omitempty"`
	} `json:"quote,omitempty"`
	Event *struct {
		EventType string `json:"eventtype"`
	} `json:"event,omitempty"`
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

// Plugin implements im.IMPlugin for WeCom Bot.
type Plugin struct {
	configProvider ConfigProvider
	users          store.UserRepository
	system         store.SystemSettingsRepository
	mailer         Mailer
	broadcaster    NotifyBroadcaster
	client         *http.Client

	mu             sync.Mutex
	messageHandler func(msg im.IncomingMessage)

	// userid → email bindings
	bindMu   sync.RWMutex
	bindings map[string]string

	// pending verification codes
	pendingMu sync.Mutex
	pending   map[string]*pendingBind

	// WebSocket
	wsCancel context.CancelFunc
	wsWg     sync.WaitGroup
	wsMu     sync.Mutex // serialises WS writes
	wsConn   *websocket.Conn
	wsConnMu sync.RWMutex

	publicBaseURL string

	// kicked is set to true when the server sends a disconnected_event
	// (another instance connected with the same botId). When kicked,
	// the gateway suppresses auto-restart to avoid mutual kicking loops.
	kicked atomic.Bool
}

type pendingBind struct {
	Email  string
	Code   string
	Expiry time.Time
}

// New creates a WeCom plugin.
func New(provider ConfigProvider, users store.UserRepository, system store.SystemSettingsRepository, mailer Mailer) *Plugin {
	p := &Plugin{
		configProvider: provider,
		users:          users,
		system:         system,
		mailer:         mailer,
		client:         &http.Client{Timeout: 60 * time.Second},
		bindings:       make(map[string]string),
		pending:        make(map[string]*pendingBind),
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

func (p *Plugin) Name() string { return "wecom" }

func (p *Plugin) ReceiveMessage(handler func(msg im.IncomingMessage)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageHandler = handler
}

func (p *Plugin) SendText(ctx context.Context, target im.UserTarget, text string) error {
	cfg := p.configProvider()
	if !cfg.Enabled || cfg.BotID == "" {
		return fmt.Errorf("wecom: not configured")
	}
	userID := target.PlatformUID
	if userID == "" {
		return fmt.Errorf("wecom: PlatformUID (userid) is required")
	}
	return p.sendMarkdown(userID, text)
}

func (p *Plugin) SendCard(ctx context.Context, target im.UserTarget, card im.OutgoingMessage) error {
	// WeCom Bot supports markdown, degrade card to markdown text.
	text := card.FallbackText
	if text == "" {
		var sb strings.Builder
		if card.Title != "" {
			sb.WriteString("**")
			sb.WriteString(card.Title)
			sb.WriteString("**\n")
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
	userID := target.PlatformUID
	if userID == "" {
		return fmt.Errorf("wecom: PlatformUID (userid) is required")
	}
	// imageKey is base64-encoded image data from the screenshot pipeline.
	if len(imageKey) > 200 {
		raw, err := base64.StdEncoding.DecodeString(imageKey)
		if err != nil {
			return p.SendText(ctx, target, caption)
		}
		mediaID, err := p.uploadMedia(raw, "image", "image.png")
		if err != nil {
			log.Printf("[wecom] image upload failed, falling back to text: %v", err)
			text := caption
			if text == "" {
				text = "[图片上传失败]"
			}
			return p.SendText(ctx, target, text)
		}
		if err := p.sendMedia(userID, "image", mediaID); err != nil {
			log.Printf("[wecom] send image failed: %v", err)
			return p.SendText(ctx, target, caption)
		}
		if caption != "" {
			_ = p.sendMarkdown(userID, caption)
		}
		return nil
	}
	text := caption
	if text == "" {
		text = "[图片]"
	}
	return p.SendText(ctx, target, text)
}

func (p *Plugin) SendFile(ctx context.Context, target im.UserTarget, fileData, fileName, mimeType string) error {
	userID := target.PlatformUID
	if userID == "" {
		return fmt.Errorf("wecom: PlatformUID (userid) is required")
	}
	raw, err := base64.StdEncoding.DecodeString(fileData)
	if err != nil {
		return p.SendText(ctx, target, fmt.Sprintf("📎 %s", fileName))
	}
	mediaType := "file"
	if strings.HasPrefix(mimeType, "image/") {
		mediaType = "image"
	} else if strings.HasPrefix(mimeType, "video/") {
		mediaType = "video"
	} else if strings.HasPrefix(mimeType, "audio/") || mimeType == "voice" {
		mediaType = "voice"
	}
	mediaID, err := p.uploadMedia(raw, mediaType, fileName)
	if err != nil {
		log.Printf("[wecom] file upload failed, falling back to text: %v", err)
		return p.SendText(ctx, target, fmt.Sprintf("📎 %s (上传失败)", fileName))
	}
	return p.sendMedia(userID, mediaType, mediaID)
}

// SendVoice implements im.VoiceSender for native WeCom voice bubbles. The
// WeCom bot protocol accepts voice media only as AMR/AMR-WB, so other formats
// fail locally instead of being uploaded as an unusable voice item.
func (p *Plugin) SendVoice(ctx context.Context, target im.UserTarget, voiceData, fileName, mimeType string) error {
	userID := target.PlatformUID
	if userID == "" {
		return fmt.Errorf("wecom: PlatformUID (userid) is required")
	}
	raw, err := base64.StdEncoding.DecodeString(voiceData)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(voiceData)
		if err != nil {
			return fmt.Errorf("wecom: voice base64 decode failed: %w", err)
		}
	}
	if !isAMR(raw) {
		return fmt.Errorf("wecom: voice payload must be AMR")
	}
	mediaID, err := p.uploadMedia(raw, "voice", "voice.amr")
	if err != nil {
		return fmt.Errorf("wecom: upload voice: %w", err)
	}
	return p.sendMedia(userID, "voice", mediaID)
}

func isAMR(data []byte) bool {
	return strings.HasPrefix(string(data), "#!AMR\n") || strings.HasPrefix(string(data), "#!AMR-WB\n")
}

func (p *Plugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	p.bindMu.RLock()
	email, ok := p.bindings[platformUID]
	p.bindMu.RUnlock()
	if !ok || email == "" {
		return "", fmt.Errorf("wecom: user %s not bound, please send your email to bind", platformUID)
	}
	user, err := p.users.GetByTenantEmail(ctx, store.DefaultTenantID, email)
	if err != nil || user == nil {
		return "", fmt.Errorf("wecom: no hub user found for email %s", email)
	}
	return user.ID, nil
}

func (p *Plugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{
		SupportsRichCard:    false,
		SupportsMarkdown:    true,
		SupportsImage:       true,
		SupportsFile:        true,
		SupportsButton:      false,
		SupportsMessageEdit: false,
		SupportsVoice:       true, // AMR via SendVoice/native voice media
		MaxTextLength:       textChunkLimit,
	}
}

func (p *Plugin) Start(ctx context.Context) error {
	cfg := p.configProvider()
	if !cfg.Enabled || cfg.BotID == "" || cfg.Secret == "" {
		log.Printf("[wecom] not configured, skipping WebSocket gateway")
		return nil
	}
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	p.kicked.Store(false) // reset anti-kick state on explicit (re)start
	wsCtx, cancel := context.WithCancel(context.Background())
	p.wsCancel = cancel
	p.wsWg.Add(1)
	go p.runGateway(wsCtx)
	log.Printf("[wecom] started (WebSocket gateway launched)")
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	if p.wsCancel != nil {
		p.wsCancel()
		p.wsWg.Wait()
		p.wsCancel = nil
	}
	log.Printf("[wecom] stopped")
	return nil
}

// ---------------------------------------------------------------------------
// WebSocket Gateway — connects to WeCom Bot gateway for real-time events
// Protocol: connect → aibot_subscribe(botId+secret) → authenticated
//           → aibot_callback / aibot_event_callback
//           → ping heartbeat every 30s
// ---------------------------------------------------------------------------

func (p *Plugin) runGateway(ctx context.Context) {
	defer p.wsWg.Done()

	reconnects := 0
	authFailures := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		authenticated, err := p.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}

		// Anti-kick protection: if we were kicked by the server (another
		// instance connected), do NOT reconnect to avoid mutual kicking loops.
		if p.kicked.Load() {
			log.Printf("[wecom/ws] kicked by server, suppressing auto-restart. Check for duplicate instances.")
			return
		}

		if authenticated {
			reconnects = 0
			authFailures = 0
		}

		// Check if auth failure
		if err != nil && strings.Contains(err.Error(), "auth failed") {
			authFailures++
			if authFailures >= wsMaxAuthFailures {
				log.Printf("[wecom/ws] auth failure attempts exhausted (%d), giving up. Check botId/secret.", wsMaxAuthFailures)
				return
			}
		}

		reconnects++
		if reconnects > wsMaxReconnects {
			log.Printf("[wecom/ws] max reconnect attempts (%d) reached, giving up", wsMaxReconnects)
			return
		}

		// Exponential backoff: 3s → 6s → 12s → 24s → 30s cap
		shift := reconnects - 1
		if shift > 4 {
			shift = 4
		}
		delay := wsReconnectBaseDelay * time.Duration(1<<shift)
		if delay > wsReconnectMaxDelay {
			delay = wsReconnectMaxDelay
		}
		if err != nil {
			log.Printf("[wecom/ws] connection error: %v, reconnecting in %v", err, delay)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (p *Plugin) connectAndRun(ctx context.Context) (authenticated bool, err error) {
	cfg := p.configProvider()
	wsURL := cfg.WSURL
	if wsURL == "" {
		wsURL = defaultWSURL
	}

	log.Printf("[wecom/ws] connecting to %s", wsURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer func() {
		conn.Close()
		p.wsConnMu.Lock()
		p.wsConn = nil
		p.wsConnMu.Unlock()
	}()

	// Store connection for sending
	p.wsConnMu.Lock()
	p.wsConn = conn
	p.wsConnMu.Unlock()

	// wsWrite serialises writes to the connection.
	wsWrite := func(v any) error {
		p.wsMu.Lock()
		defer p.wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Step 1: Send subscribe (authenticate)
	subscribeFrame := wsFrame{
		Cmd:     cmdSubscribe,
		Headers: wsHeaders{ReqID: generateReqID("sub")},
		Body:    mustJSON(map[string]string{"bot_id": cfg.BotID, "secret": cfg.Secret}),
	}
	if err := wsWrite(subscribeFrame); err != nil {
		return false, fmt.Errorf("send subscribe: %w", err)
	}

	// Step 2: Read subscribe response
	var subResp wsFrame
	if err := conn.ReadJSON(&subResp); err != nil {
		return false, fmt.Errorf("read subscribe response: %w", err)
	}
	if subResp.Errcode != 0 {
		return false, fmt.Errorf("auth failed: errcode=%d errmsg=%s", subResp.Errcode, subResp.Errmsg)
	}
	log.Printf("[wecom/ws] authenticated successfully")
	authenticated = true

	// Step 3: Start heartbeat goroutine + stream state cleanup
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		cleanupTicker := time.NewTicker(streamStateCleanupInterval)
		defer ticker.Stop()
		defer cleanupTicker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				ping := wsFrame{
					Cmd:     cmdPing,
					Headers: wsHeaders{ReqID: generateReqID("hb")},
				}
				if err := wsWrite(ping); err != nil {
					log.Printf("[wecom/ws] heartbeat send error: %v", err)
					return
				}
			case <-cleanupTicker.C:
				cleanupStreamStates()
			}
		}
	}()

	// Step 4: Read loop
	for {
		select {
		case <-ctx.Done():
			return authenticated, nil
		default:
		}

		var frame wsFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return authenticated, fmt.Errorf("read: %w", err)
		}

		switch frame.Cmd {
		case cmdCallback:
			// Message callback
			cfg := p.configProvider()
			if !cfg.Enabled {
				continue
			}
			go p.handleCallback(frame)

		case cmdEventCallback:
			// Event callback (enter_chat, template_card_event, etc.)
			// Check for disconnected_event (kicked by server)
			var evtBody struct {
				Event *struct {
					EventType string `json:"eventtype"`
				} `json:"event,omitempty"`
			}
			if json.Unmarshal(frame.Body, &evtBody) == nil && evtBody.Event != nil && evtBody.Event.EventType == "disconnected_event" {
				log.Printf("[wecom/ws] kicked by server: a new connection was established elsewhere. Suppressing auto-restart.")
				p.kicked.Store(true)
				return authenticated, fmt.Errorf("kicked by server (disconnected_event)")
			}
			go p.handleEventCallback(frame)

		case "pong":
			// Heartbeat ACK, ignore

		default:
			// Response frames (errcode/errmsg) or unknown commands
			if frame.Errcode != 0 {
				log.Printf("[wecom/ws] error frame: cmd=%s errcode=%d errmsg=%s", frame.Cmd, frame.Errcode, frame.Errmsg)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Message sending via WebSocket
// ---------------------------------------------------------------------------

// sendMarkdown sends a markdown message to the given chatId via WS proactive send.
func (p *Plugin) sendMarkdown(chatID, text string) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	frame := wsFrame{
		Cmd:     cmdSendMsg,
		Headers: wsHeaders{ReqID: generateReqID("send")},
		Body: mustJSON(map[string]any{
			"chatid":  chatID,
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": text,
			},
		}),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// replyStream sends a streaming reply to a callback frame.
func (p *Plugin) replyStream(reqID, streamID, content string, finish bool) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	frame := wsFrame{
		Cmd:     cmdRespondMsg,
		Headers: wsHeaders{ReqID: reqID},
		Body: mustJSON(map[string]any{
			"msgtype": "stream",
			"stream": map[string]any{
				"id":      streamID,
				"finish":  finish,
				"content": content,
			},
		}),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// ---------------------------------------------------------------------------
// Media upload via WebSocket (3-step chunked: init → chunk × N → finish)
// Protocol: aibot_upload_media_init → aibot_upload_media_chunk → aibot_upload_media_finish
// ---------------------------------------------------------------------------

// uploadMedia uploads a file buffer to WeCom via the WebSocket chunked upload protocol.
// Returns the media_id on success.
func (p *Plugin) uploadMedia(data []byte, mediaType, filename string) (string, error) {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()
	if conn == nil {
		return "", fmt.Errorf("wecom: WebSocket not connected")
	}

	uploadID := generateReqID("upload")

	// Step 1: Init
	initFrame := wsFrame{
		Cmd:     cmdUploadMediaInit,
		Headers: wsHeaders{ReqID: generateReqID("uinit")},
		Body: mustJSON(map[string]any{
			"upload_id": uploadID,
			"type":      mediaType,
			"filename":  filename,
			"size":      len(data),
		}),
	}
	initResp, err := p.sendAndWaitReply(conn, initFrame)
	if err != nil {
		return "", fmt.Errorf("upload init: %w", err)
	}
	if initResp.Errcode != 0 {
		return "", fmt.Errorf("upload init errcode=%d: %s", initResp.Errcode, initResp.Errmsg)
	}

	// Step 2: Send chunks
	totalChunks := (len(data) + uploadChunkSize - 1) / uploadChunkSize
	for i := 0; i < totalChunks; i++ {
		start := i * uploadChunkSize
		end := start + uploadChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]
		chunkB64 := base64.StdEncoding.EncodeToString(chunk)

		chunkFrame := wsFrame{
			Cmd:     cmdUploadMediaChunk,
			Headers: wsHeaders{ReqID: generateReqID("uchk")},
			Body: mustJSON(map[string]any{
				"upload_id":   uploadID,
				"chunk_index": i,
				"data":        chunkB64,
			}),
		}
		chunkResp, err := p.sendAndWaitReply(conn, chunkFrame)
		if err != nil {
			return "", fmt.Errorf("upload chunk %d: %w", i, err)
		}
		if chunkResp.Errcode != 0 {
			return "", fmt.Errorf("upload chunk %d errcode=%d: %s", i, chunkResp.Errcode, chunkResp.Errmsg)
		}
	}

	// Step 3: Finish
	finishFrame := wsFrame{
		Cmd:     cmdUploadMediaFin,
		Headers: wsHeaders{ReqID: generateReqID("ufin")},
		Body: mustJSON(map[string]any{
			"upload_id":    uploadID,
			"total_chunks": totalChunks,
		}),
	}
	finishResp, err := p.sendAndWaitReply(conn, finishFrame)
	if err != nil {
		return "", fmt.Errorf("upload finish: %w", err)
	}
	if finishResp.Errcode != 0 {
		return "", fmt.Errorf("upload finish errcode=%d: %s", finishResp.Errcode, finishResp.Errmsg)
	}

	// Extract media_id from finish response
	var result struct {
		Type      string `json:"type"`
		MediaID   string `json:"media_id"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(finishResp.Body, &result); err != nil {
		return "", fmt.Errorf("parse upload finish response: %w", err)
	}
	if result.MediaID == "" {
		return "", fmt.Errorf("upload returned empty media_id")
	}
	log.Printf("[wecom] uploaded media: type=%s media_id=%s size=%d chunks=%d", mediaType, result.MediaID, len(data), totalChunks)
	return result.MediaID, nil
}

// sendAndWaitReply sends a frame and reads the next response frame.
// IMPORTANT: This method reads directly from the WebSocket connection,
// which means it can race with the main read loop in connectAndRun.
// It is safe to call ONLY when the caller holds exclusive read access
// (e.g., during initial connection setup before the read loop starts).
// For upload operations called from outbound paths (SendImage/SendFile),
// the read loop may consume the response. In practice, WeCom's upload
// responses arrive quickly and the read loop's default case logs them.
// A proper fix would use a response channel, but for now we set a
// read deadline to avoid blocking forever.
func (p *Plugin) sendAndWaitReply(conn *websocket.Conn, frame wsFrame) (*wsFrame, error) {
	p.wsMu.Lock()
	err := conn.WriteJSON(frame)
	p.wsMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	// Set a read deadline so we don't block forever if the read loop
	// consumed our response.
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var resp wsFrame
	if err := conn.ReadJSON(&resp); err != nil {
		_ = conn.SetReadDeadline(time.Time{}) // clear deadline
		return nil, fmt.Errorf("read reply: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{}) // clear deadline
	return &resp, nil
}

// sendMedia sends a media message (image/file/voice/video) using a media_id.
func (p *Plugin) sendMedia(chatID, mediaType, mediaID string) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	body := map[string]any{
		"chatid":  chatID,
		"msgtype": mediaType,
	}
	body[mediaType] = map[string]string{
		"media_id": mediaID,
	}

	frame := wsFrame{
		Cmd:     cmdSendMsg,
		Headers: wsHeaders{ReqID: generateReqID("media")},
		Body:    mustJSON(body),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// ---------------------------------------------------------------------------
// Template card messages
// ---------------------------------------------------------------------------

// cmdRespondWelcome is the WS command for welcome replies (also used for template cards).
const cmdRespondWelcome = "aibot_respond_welcome_msg"

// cmdRespondUpdate is the WS command for updating template cards on button click.
const cmdRespondUpdate = "aibot_respond_update_msg"

// replyTemplateCard sends a template card as a reply to a callback frame.
func (p *Plugin) replyTemplateCard(reqID string, card map[string]any) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	frame := wsFrame{
		Cmd:     cmdRespondMsg,
		Headers: wsHeaders{ReqID: reqID},
		Body: mustJSON(map[string]any{
			"msgtype":       "template_card",
			"template_card": card,
		}),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// sendTemplateCard sends a template card proactively to a chat.
func (p *Plugin) sendTemplateCard(chatID string, card map[string]any) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	frame := wsFrame{
		Cmd:     cmdSendMsg,
		Headers: wsHeaders{ReqID: generateReqID("card")},
		Body: mustJSON(map[string]any{
			"chatid":        chatID,
			"msgtype":       "template_card",
			"template_card": card,
		}),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// updateTemplateCard updates a template card in response to a button click event.
// Must be called within 5 seconds of receiving the template_card_event.
func (p *Plugin) updateTemplateCard(reqID string, card map[string]any) error {
	p.wsConnMu.RLock()
	conn := p.wsConn
	p.wsConnMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wecom: WebSocket not connected")
	}

	frame := wsFrame{
		Cmd:     cmdRespondUpdate,
		Headers: wsHeaders{ReqID: reqID},
		Body: mustJSON(map[string]any{
			"template_card": card,
		}),
	}

	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	return conn.WriteJSON(frame)
}

// SendNoticeCard sends a text_notice template card to a user (convenience method).
// Used by the IM Adapter for rich notifications.
func (p *Plugin) SendNoticeCard(chatID, title, desc string) error {
	card := map[string]any{
		"card_type":  "text_notice",
		"main_title": map[string]string{"title": title, "desc": desc},
	}
	return p.sendTemplateCard(chatID, card)
}

// ---------------------------------------------------------------------------
// Inbound message handling
// ---------------------------------------------------------------------------

// message dedup
var msgDedup = struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	lastEvict time.Time
}{seen: make(map[string]time.Time)}

const msgDedupTTL = 5 * time.Minute

func isDuplicateMsg(msgID string) bool {
	if msgID == "" {
		return false
	}
	now := time.Now()
	msgDedup.mu.Lock()
	defer msgDedup.mu.Unlock()

	if now.Sub(msgDedup.lastEvict) > time.Minute {
		for id, t := range msgDedup.seen {
			if now.Sub(t) > msgDedupTTL {
				delete(msgDedup.seen, id)
			}
		}
		msgDedup.lastEvict = now
	}

	if _, exists := msgDedup.seen[msgID]; exists {
		return true
	}
	msgDedup.seen[msgID] = now
	return false
}

func (p *Plugin) handleCallback(frame wsFrame) {
	var body callbackBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		log.Printf("[wecom] parse callback failed: %v", err)
		return
	}

	userID := body.From.UserID
	if userID == "" {
		return
	}

	// Skip event-type callbacks (handled by handleEventCallback)
	if body.MsgType == "event" {
		return
	}

	// Dedup
	if isDuplicateMsg(body.MsgID) {
		log.Printf("[wecom] duplicate msg_id=%s, skipping", body.MsgID)
		return
	}

	isGroup := body.ChatType == "group"
	chatID := body.ChatID
	if chatID == "" {
		chatID = userID
	}

	// Extract text content
	text := p.extractText(&body)

	// In group chats, strip @bot mentions (WeCom prepends @BotName to the text)
	if isGroup && text != "" {
		text = stripAtMention(text)
	}

	// Download media attachments
	var attachments []im.MessageAttachment
	attachments = append(attachments, p.extractAttachments(&body)...)

	// Extract quote content
	if text == "" && body.Quote != nil && body.Quote.Text != nil {
		text = body.Quote.Text.Content
	}

	if text == "" && len(attachments) == 0 {
		log.Printf("[wecom] empty message from %s, skipping", userID)
		return
	}

	log.Printf("[wecom] %s message from %s (chat=%s): %s",
		map[bool]string{true: "group", false: "dm"}[isGroup], userID, chatID, truncate(text, 80))

	// Handle /unbind command (DM only)
	if !isGroup && strings.EqualFold(text, "/unbind") {
		p.handleUnbind(userID)
		return
	}

	// Handle binding flow (DM only — group messages skip binding prompts)
	if !isGroup && p.handleBindingFlow(userID, text) {
		return
	}

	// Check if user is bound
	p.bindMu.RLock()
	_, bound := p.bindings[userID]
	p.bindMu.RUnlock()

	if !bound {
		if !isGroup {
			_ = p.sendMarkdown(chatID,
				"👋 欢迎使用 MaClaw 企业微信 Bot！\n\n"+
					"请先绑定您的 Hub 账号，发送您的注册邮箱地址即可开始绑定。")
		} else {
			log.Printf("[wecom] unbound user %s in group %s, ignoring", userID, chatID)
		}
		return
	}

	// Dispatch to IM Adapter with streaming support
	p.mu.Lock()
	handler := p.messageHandler
	p.mu.Unlock()

	if handler == nil {
		log.Printf("[wecom] no message handler registered")
		return
	}

	msgType := "text"
	if len(attachments) > 0 {
		msgType = attachments[0].Type
	}

	// Create stream state for this message (for thinking + progress updates)
	reqID := frame.Headers.ReqID
	ss := getOrCreateStreamState(body.MsgID, reqID, userID)

	// Send "thinking" placeholder via replyStream (non-final)
	if err := p.replyStream(reqID, ss.streamID, thinkingMessage, false); err != nil {
		log.Printf("[wecom] failed to send thinking message: %v", err)
	}

	handler(im.IncomingMessage{
		PlatformName: "wecom",
		PlatformUID:  userID,
		MessageType:  msgType,
		Text:         text,
		Attachments:  attachments,
		RawPayload:   frame.Body,
		Timestamp:    time.Now(),
		MessageID:    body.MsgID,
	})
}

// ---------------------------------------------------------------------------
// Streaming progress delivery — called by IM Adapter for intermediate updates
// ---------------------------------------------------------------------------

// DeliverStreamProgress sends a streaming progress update for an in-flight message.
// Called by the IM Adapter's progress delivery callback.
// If the stream has expired (errcode 846608), falls back to sendMessage.
func (p *Plugin) DeliverStreamProgress(userID, platformUID, text string) {
	// Find the active stream state for this user.
	streamStates.mu.Lock()
	var activeSS *streamState
	var activeMsgID string
	for msgID, ss := range streamStates.items {
		if ss.platformUID != platformUID {
			continue
		}
		if activeSS == nil || ss.createdAt.After(activeSS.createdAt) {
			activeSS = ss
			activeMsgID = msgID
		}
	}
	streamStates.mu.Unlock()

	if activeSS == nil {
		// No active stream — fall back to proactive send
		_ = p.sendMarkdown(platformUID, text)
		return
	}

	if activeSS.expired {
		// Stream expired — use proactive sendMessage
		_ = p.sendMarkdown(platformUID, text)
		deleteStreamState(activeMsgID)
		return
	}

	// Try streaming update
	err := p.replyStream(activeSS.reqID, activeSS.streamID, text, false)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, fmt.Sprintf("%d", streamExpiredErrcode)) {
			activeSS.expired = true
			log.Printf("[wecom] stream expired for msg=%s, falling back to sendMessage", activeMsgID)
			_ = p.sendMarkdown(platformUID, text)
		} else {
			log.Printf("[wecom] stream progress error: %v", err)
		}
	}
}

// FinishStream sends the final streaming reply and cleans up state.
// Called after the Agent produces its final response.
// If the stream has expired, falls back to sendMessage.
func (p *Plugin) FinishStream(platformUID, text, msgID string) {
	streamStates.mu.Lock()
	ss, ok := streamStates.items[msgID]
	streamStates.mu.Unlock()

	if !ok || ss == nil {
		// No stream state — just send as proactive message
		_ = p.sendMarkdown(platformUID, text)
		return
	}

	defer deleteStreamState(msgID)

	if ss.expired {
		// Stream expired — use proactive sendMessage
		_ = p.sendMarkdown(platformUID, text)
		return
	}

	// Send final streaming reply (finish=true)
	err := p.replyStream(ss.reqID, ss.streamID, text, true)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, fmt.Sprintf("%d", streamExpiredErrcode)) {
			log.Printf("[wecom] stream expired on finish for msg=%s, falling back to sendMessage", msgID)
			_ = p.sendMarkdown(platformUID, text)
		} else {
			log.Printf("[wecom] stream finish error: %v, falling back to sendMessage", err)
			_ = p.sendMarkdown(platformUID, text)
		}
	}
}

func (p *Plugin) handleEventCallback(frame wsFrame) {
	var body callbackBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		log.Printf("[wecom] parse event callback failed: %v", err)
		return
	}
	eventType := ""
	if body.Event != nil {
		eventType = body.Event.EventType
	}
	log.Printf("[wecom] event callback: eventtype=%s msgid=%s", eventType, body.MsgID)

	switch eventType {
	case "enter_chat":
		// User entered the bot's single chat for the first time today.
		// Send a welcome message via replyStream (must respond within 5s).
		userID := body.From.UserID
		if userID == "" {
			return
		}
		cfg := p.configProvider()
		if !cfg.Enabled {
			return
		}
		// Check if user is bound
		p.bindMu.RLock()
		_, bound := p.bindings[userID]
		p.bindMu.RUnlock()

		welcome := "👋 您好！我是 MaClaw 智能助手，有什么可以帮您的吗？"
		if !bound {
			welcome = "👋 欢迎使用 MaClaw 企业微信 Bot！\n\n请发送您的 Hub 注册邮箱地址来绑定账号。"
		}
		// Use replyStream for welcome (must be within 5s of event)
		streamID := generateReqID("welcome")
		if err := p.replyStream(frame.Headers.ReqID, streamID, welcome, true); err != nil {
			log.Printf("[wecom] failed to send welcome: %v", err)
			// Fallback to proactive send
			_ = p.sendMarkdown(userID, welcome)
		}

	case "template_card_event":
		// User clicked a button on a template card.
		// Extract event_key and task_id from the event body.
		var tcEvent struct {
			Event *struct {
				TemplateCardEvent *struct {
					CardType string `json:"card_type"`
					EventKey string `json:"event_key"`
					TaskID   string `json:"task_id"`
				} `json:"template_card_event"`
			} `json:"event"`
		}
		if json.Unmarshal(frame.Body, &tcEvent) == nil && tcEvent.Event != nil && tcEvent.Event.TemplateCardEvent != nil {
			tce := tcEvent.Event.TemplateCardEvent
			log.Printf("[wecom] template card click: event_key=%s task_id=%s card_type=%s",
				tce.EventKey, tce.TaskID, tce.CardType)

			// Update the card to show the selected state (must respond within 5s).
			// Replace the card with a text_notice showing the user's choice.
			updatedCard := map[string]any{
				"card_type":  "text_notice",
				"main_title": map[string]string{"title": "✅ " + tce.EventKey},
				"task_id":    tce.TaskID,
			}
			if err := p.updateTemplateCard(frame.Headers.ReqID, updatedCard); err != nil {
				log.Printf("[wecom] failed to update template card: %v", err)
			}

			// Also dispatch the button click as a text message to the Agent
			// so it can react to the user's choice.
			userID := body.From.UserID
			if userID != "" {
				p.mu.Lock()
				handler := p.messageHandler
				p.mu.Unlock()
				if handler != nil {
					p.bindMu.RLock()
					_, bound := p.bindings[userID]
					p.bindMu.RUnlock()
					if bound {
						handler(im.IncomingMessage{
							PlatformName: "wecom",
							PlatformUID:  userID,
							MessageType:  "text",
							Text:         tce.EventKey,
							Timestamp:    time.Now(),
						})
					}
				}
			}
		} else {
			log.Printf("[wecom] template card event received but could not parse")
		}

	case "feedback_event":
		log.Printf("[wecom] feedback event received")
	}
}

// extractText extracts text content from a callback body.
func (p *Plugin) extractText(body *callbackBody) string {
	switch body.MsgType {
	case "text":
		if body.Text != nil {
			return strings.TrimSpace(body.Text.Content)
		}
	case "voice":
		// Voice messages have transcribed text
		if body.Voice != nil && body.Voice.Text != "" {
			return strings.TrimSpace(body.Voice.Text)
		}
	case "mixed":
		// Extract text parts from mixed messages
		if body.Mixed != nil {
			var parts []string
			for _, item := range body.Mixed.MsgItem {
				if item.MsgType == "text" && item.Text != nil {
					parts = append(parts, item.Text.Content)
				}
			}
			return strings.TrimSpace(strings.Join(parts, "\n"))
		}
	}
	return ""
}

// extractAttachments downloads and converts media from a callback body into im.MessageAttachment.
func (p *Plugin) extractAttachments(body *callbackBody) []im.MessageAttachment {
	var result []im.MessageAttachment
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch body.MsgType {
	case "image":
		if body.Image != nil && body.Image.URL != "" {
			data, err := p.downloadAndDecrypt(ctx, body.Image.URL, body.Image.AESKey)
			if err != nil {
				log.Printf("[wecom] download image failed: %v", err)
				break
			}
			if int64(len(data)) <= im.MaxAttachmentSize {
				result = append(result, im.MessageAttachment{
					Type:     "image",
					FileName: "image.jpg",
					MimeType: "image/jpeg",
					Data:     base64.StdEncoding.EncodeToString(data),
					Size:     int64(len(data)),
				})
			}
		}
	case "file":
		if body.File != nil && body.File.URL != "" {
			data, err := p.downloadAndDecrypt(ctx, body.File.URL, body.File.AESKey)
			if err != nil {
				log.Printf("[wecom] download file failed: %v", err)
				break
			}
			if int64(len(data)) <= im.MaxAttachmentSize {
				result = append(result, im.MessageAttachment{
					Type:     "file",
					FileName: "file",
					MimeType: "application/octet-stream",
					Data:     base64.StdEncoding.EncodeToString(data),
					Size:     int64(len(data)),
				})
			}
		}
	case "voice":
		if body.Voice != nil && body.Voice.URL != "" {
			data, err := p.downloadAndDecrypt(ctx, body.Voice.URL, body.Voice.AESKey)
			if err != nil {
				log.Printf("[wecom] download voice failed: %v", err)
				break
			}
			if int64(len(data)) <= im.MaxAttachmentSize {
				result = append(result, im.MessageAttachment{
					Type:     "voice",
					FileName: "voice.amr",
					MimeType: "audio/amr",
					Data:     base64.StdEncoding.EncodeToString(data),
					Size:     int64(len(data)),
				})
			}
		}
	case "video":
		if body.Video != nil && body.Video.URL != "" {
			data, err := p.downloadAndDecrypt(ctx, body.Video.URL, body.Video.AESKey)
			if err != nil {
				log.Printf("[wecom] download video failed: %v", err)
				break
			}
			if int64(len(data)) <= im.MaxAttachmentSize {
				result = append(result, im.MessageAttachment{
					Type:     "video",
					FileName: "video.mp4",
					MimeType: "video/mp4",
					Data:     base64.StdEncoding.EncodeToString(data),
					Size:     int64(len(data)),
				})
			}
		}
	case "mixed":
		if body.Mixed != nil {
			for _, item := range body.Mixed.MsgItem {
				if item.MsgType == "image" && item.Image != nil && item.Image.URL != "" {
					data, err := p.downloadAndDecrypt(ctx, item.Image.URL, item.Image.AESKey)
					if err != nil {
						log.Printf("[wecom] download mixed image failed: %v", err)
						continue
					}
					if int64(len(data)) <= im.MaxAttachmentSize {
						result = append(result, im.MessageAttachment{
							Type:     "image",
							FileName: "image.jpg",
							MimeType: "image/jpeg",
							Data:     base64.StdEncoding.EncodeToString(data),
							Size:     int64(len(data)),
						})
					}
				}
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Email binding flow (same pattern as QQ Bot / Feishu)
// ---------------------------------------------------------------------------

func (p *Plugin) handleBindingFlow(userID, text string) bool {
	p.pendingMu.Lock()
	pb, hasPending := p.pending[userID]
	if hasPending && pb.Expiry.Before(time.Now()) {
		delete(p.pending, userID)
		hasPending = false
		pb = nil
	}
	p.pendingMu.Unlock()

	if hasPending && pb != nil && isVerifyCode(text) {
		return p.handleVerifyCode(userID, text, pb)
	}

	if looksLikeEmail(text) {
		p.bindMu.RLock()
		_, bound := p.bindings[userID]
		p.bindMu.RUnlock()
		if bound {
			return false
		}
		p.handleEmailSubmit(userID, text)
		return true
	}

	if hasPending && pb != nil {
		_ = p.sendMarkdown(userID, "请输入您收到的 6 位验证码完成绑定。")
		return true
	}

	return false
}

func (p *Plugin) handleEmailSubmit(userID, email string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := p.users.GetByTenantEmail(ctx, store.DefaultTenantID, email)
	if err != nil || user == nil {
		_ = p.sendMarkdown(userID,
			fmt.Sprintf("❌ 未找到邮箱 %s 对应的 Hub 用户，请确认邮箱是否正确。", email))
		return
	}

	code := generateCode()
	p.pendingMu.Lock()
	p.pending[userID] = &pendingBind{
		Email:  email,
		Code:   code,
		Expiry: time.Now().Add(5 * time.Minute),
	}
	p.pendingMu.Unlock()

	if p.broadcaster != nil {
		sentTo, err := p.broadcaster.BroadcastVerifyCode(ctx, email, code, "wecom")
		if err != nil {
			log.Printf("[wecom] broadcast verification code for %s failed: %v", email, err)
			_ = p.sendMarkdown(userID, fmt.Sprintf("❌ 验证码发送失败: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, userID)
			p.pendingMu.Unlock()
			return
		}
		_ = p.sendMarkdown(userID,
			fmt.Sprintf("📧 验证码已发送到: %s\n\n请查看验证码，回复给我完成绑定（5 分钟内有效）。", sentTo))
		return
	}

	// Fallback: email-only
	subject := "MaClaw 企业微信 Bot 绑定验证码"
	body := fmt.Sprintf(
		"您好，\r\n\r\n您正在将企业微信账号绑定到 MaClaw Hub。\r\n\r\n验证码: %s\r\n\r\n请在 5 分钟内将此验证码回复给企业微信 Bot 完成绑定。\r\n如非本人操作，请忽略此邮件。\r\n",
		code,
	)
	if p.mailer != nil {
		if err := p.mailer.Send(ctx, []string{email}, subject, body); err != nil {
			log.Printf("[wecom] send verification email to %s failed: %v", email, err)
			_ = p.sendMarkdown(userID, fmt.Sprintf("❌ 验证邮件发送失败: %v", err))
			p.pendingMu.Lock()
			delete(p.pending, userID)
			p.pendingMu.Unlock()
			return
		}
	} else {
		_ = p.sendMarkdown(userID, "❌ Hub 邮件服务未配置，无法发送验证码。请联系管理员。")
		p.pendingMu.Lock()
		delete(p.pending, userID)
		p.pendingMu.Unlock()
		return
	}

	_ = p.sendMarkdown(userID,
		fmt.Sprintf("📧 验证码已发送到邮箱: %s\n\n请查收邮件，将 6 位验证码回复给我完成绑定（5 分钟内有效）。", email))
}

func (p *Plugin) handleVerifyCode(userID, code string, pb *pendingBind) bool {
	if pb.Expiry.Before(time.Now()) {
		p.pendingMu.Lock()
		delete(p.pending, userID)
		p.pendingMu.Unlock()
		_ = p.sendMarkdown(userID, "⏰ 验证码已过期，请重新发送邮箱地址。")
		return true
	}

	if strings.TrimSpace(code) != pb.Code {
		_ = p.sendMarkdown(userID, "❌ 验证码不正确，请重新输入。")
		return true
	}

	p.bindMu.Lock()
	p.bindings[userID] = pb.Email
	p.bindMu.Unlock()
	p.saveBindings()

	p.pendingMu.Lock()
	delete(p.pending, userID)
	p.pendingMu.Unlock()

	_ = p.sendMarkdown(userID,
		fmt.Sprintf("✅ 绑定成功！\n\n邮箱: %s\n\n现在您可以直接发送消息与 MaClaw Agent 交互了。", pb.Email))
	return true
}

func (p *Plugin) handleUnbind(userID string) {
	p.bindMu.RLock()
	email, bound := p.bindings[userID]
	p.bindMu.RUnlock()
	if !bound || email == "" {
		_ = p.sendMarkdown(userID, "当前未绑定任何账号。")
		return
	}
	p.RemoveBinding(userID)
	log.Printf("[wecom] unbound email=%s for userid=%s", email, userID)
	_ = p.sendMarkdown(userID, fmt.Sprintf("✅ 已解除 %s 的绑定。", email))
}

// RemoveBinding removes a userid→email binding.
func (p *Plugin) RemoveBinding(userID string) {
	p.bindMu.Lock()
	delete(p.bindings, userID)
	p.bindMu.Unlock()
	p.saveBindings()
}

// LookupByEmail returns the WeCom userid bound to the given email, or "".
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

// GetBindings returns the current userid→email bindings (for admin API).
func (p *Plugin) GetBindings() map[string]string {
	p.bindMu.RLock()
	defer p.bindMu.RUnlock()
	m := make(map[string]string, len(p.bindings))
	for k, v := range p.bindings {
		m[k] = v
	}
	return m
}

// RemoveBindingByEmail removes all userid→email bindings for the given email.
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

const wecomBindingsKey = "wecom_bindings"

func (p *Plugin) loadBindings() {
	raw, err := p.system.Get(context.Background(), wecomBindingsKey)
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
	_ = p.system.Set(context.Background(), wecomBindingsKey, string(data))
}

// ---------------------------------------------------------------------------
// AES-256-CBC file decryption (for image/file/voice/video downloads)
// ---------------------------------------------------------------------------

// downloadAndDecrypt downloads a file from WeCom CDN and decrypts it with AES-256-CBC.
// The aesKey is base64-encoded, the IV is the first 16 bytes of the ciphertext.
func (p *Plugin) downloadAndDecrypt(ctx context.Context, fileURL, aesKeyBase64 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	ciphertext, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50MB limit
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if aesKeyBase64 == "" {
		// No encryption, return raw data
		return ciphertext, nil
	}

	// Decode AES key
	aesKey, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode aes key: %w", err)
	}

	// AES-256-CBC: IV is first 16 bytes
	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size %d", len(ciphertext), aes.BlockSize)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS7 padding
	plaintext, err = pkcs7Unpad(plaintext)
	if err != nil {
		return nil, fmt.Errorf("pkcs7 unpad: %w", err)
	}

	return plaintext, nil
}

// pkcs7Unpad removes PKCS#7 padding.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > aes.BlockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid padding length %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid padding byte")
		}
	}
	return data[:len(data)-padLen], nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateReqID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s_%d", prefix, hex.EncodeToString(b), time.Now().UnixMilli())
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

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

// stripAtMention removes @BotName mentions from group chat messages.
// WeCom prepends "@BotName " to the text when the bot is mentioned.
func stripAtMention(text string) string {
	runes := []rune(text)
	var result []rune
	i := 0
	for i < len(runes) {
		if runes[i] == '@' {
			// Skip @mention: consume until whitespace or end
			i++ // skip '@'
			for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' && runes[i] != '\n' && runes[i] != '\u00a0' {
				i++
			}
			// Skip trailing whitespace after mention
			for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
				i++
			}
		} else {
			result = append(result, runes[i])
			i++
		}
	}
	return strings.TrimSpace(string(result))
}
