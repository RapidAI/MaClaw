// Package weixin implements a client-side WeChat gateway using long-polling
// against the iLink backend API. It receives messages from WeChat users and
// forwards them to the Hub. Outbound replies are sent via the iLink API.
//
// This runs entirely on the client machine — the Hub never touches bot tokens.
// Protocol reference: @tencent-weixin/openclaw-weixin plugin.
package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

const (
	DefaultBaseURL    = "https://ilinkai.weixin.qq.com"
	DefaultCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
	DefaultBotType    = "3"

	longPollTimeout        = 35 * time.Second
	apiTimeout             = 15 * time.Second
	maxConsecutiveFailures = 3
	backoffDelay           = 30 * time.Second
	retryDelay             = 2 * time.Second
	sessionExpiredErrcode  = -14
	textChunkLimit         = 4000
	cdnUploadMaxRetries    = 3
	cdnDownloadMaxBytes    = 100 * 1024 * 1024 // 100 MB
	apiResponseMaxBytes    = 10 * 1024 * 1024  // 10 MB
)

// Config holds WeChat gateway configuration.
type Config struct {
	Token     string
	BaseURL   string // defaults to DefaultBaseURL
	CDNURL    string // defaults to DefaultCDNBaseURL
	AccountID string
}

func (c Config) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c Config) cdnURL() string {
	if c.CDNURL != "" {
		return strings.TrimRight(c.CDNURL, "/")
	}
	return DefaultCDNBaseURL
}

// ---------------------------------------------------------------------------
// API types (mirrors the iLink protocol)
// ---------------------------------------------------------------------------

type baseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}

type textItem struct {
	Text string `json:"text,omitempty"`
}

type cdnMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
}

type imageItem struct {
	Media       *cdnMedia `json:"media,omitempty"`
	ThumbMedia  *cdnMedia `json:"thumb_media,omitempty"`
	AESKey      string    `json:"aeskey,omitempty"` // hex-encoded, preferred for inbound
	MidSize     int       `json:"mid_size,omitempty"`
	ThumbSize   int       `json:"thumb_size,omitempty"`
	ThumbHeight int       `json:"thumb_height,omitempty"`
	ThumbWidth  int       `json:"thumb_width,omitempty"`
	HDSize      int       `json:"hd_size,omitempty"`
}

type voiceItem struct {
	Media         *cdnMedia `json:"media,omitempty"`
	EncodeType    int       `json:"encode_type,omitempty"`
	BitsPerSample int      `json:"bits_per_sample,omitempty"`
	SampleRate    int       `json:"sample_rate,omitempty"`
	Playtime      int       `json:"playtime,omitempty"`
	Text          string    `json:"text,omitempty"`
}

type fileItem struct {
	Media    *cdnMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	MD5      string    `json:"md5,omitempty"`
	Len      string    `json:"len,omitempty"`
}

type videoItem struct {
	Media       *cdnMedia `json:"media,omitempty"`
	VideoSize   int       `json:"video_size,omitempty"`
	PlayLength  int       `json:"play_length,omitempty"`
	VideoMD5    string    `json:"video_md5,omitempty"`
	ThumbMedia  *cdnMedia `json:"thumb_media,omitempty"`
	ThumbSize   int       `json:"thumb_size,omitempty"`
	ThumbHeight int       `json:"thumb_height,omitempty"`
	ThumbWidth  int       `json:"thumb_width,omitempty"`
}

type refMessage struct {
	MessageItem *messageItem `json:"message_item,omitempty"`
	Title       string       `json:"title,omitempty"`
}

type messageItem struct {
	Type         int        `json:"type,omitempty"`
	CreateTimeMs int64      `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64      `json:"update_time_ms,omitempty"`
	IsCompleted  bool       `json:"is_completed,omitempty"`
	MsgID        string     `json:"msg_id,omitempty"`
	RefMsg       *refMessage `json:"ref_msg,omitempty"`
	TextItem     *textItem  `json:"text_item,omitempty"`
	ImageItem    *imageItem `json:"image_item,omitempty"`
	VoiceItem    *voiceItem `json:"voice_item,omitempty"`
	FileItem     *fileItem  `json:"file_item,omitempty"`
	VideoItem    *videoItem `json:"video_item,omitempty"`
}

// MessageItemType constants
const (
	ItemTypeNone  = 0
	ItemTypeText  = 1
	ItemTypeImage = 2
	ItemTypeVoice = 3
	ItemTypeFile  = 4
	ItemTypeVideo = 5
)

// MessageType constants
const (
	MsgTypeNone = 0
	MsgTypeUser = 1
	MsgTypeBot  = 2
)

// MessageState constants
const (
	MsgStateNew        = 0
	MsgStateGenerating = 1
	MsgStateFinish     = 2
)

// UploadMediaType constants
const (
	UploadMediaImage = 1
	UploadMediaVideo = 2
	UploadMediaFile  = 3
	UploadMediaVoice = 4
)

type weixinMessage struct {
	Seq          int64          `json:"seq,omitempty"`
	MessageID    int64          `json:"message_id,omitempty"`
	FromUserID   string         `json:"from_user_id,omitempty"`
	ToUserID     string         `json:"to_user_id,omitempty"`
	ClientID     string         `json:"client_id,omitempty"`
	CreateTimeMs int64          `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64          `json:"update_time_ms,omitempty"`
	DeleteTimeMs int64          `json:"delete_time_ms,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	GroupID      string         `json:"group_id,omitempty"`
	MessageType  int            `json:"message_type,omitempty"`
	MessageState int            `json:"message_state,omitempty"`
	ItemList     []messageItem  `json:"item_list,omitempty"`
	ContextToken string         `json:"context_token,omitempty"`
}

type getUpdatesReq struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      baseInfo `json:"base_info"`
}

type getUpdatesResp struct {
	Ret                 int              `json:"ret,omitempty"`
	Errcode             int              `json:"errcode,omitempty"`
	Errmsg              string           `json:"errmsg,omitempty"`
	Msgs                []weixinMessage  `json:"msgs,omitempty"`
	GetUpdatesBuf       string           `json:"get_updates_buf,omitempty"`
	LongpollingTimeoutMs int64           `json:"longpolling_timeout_ms,omitempty"`
}

type sendMessageReq struct {
	Msg      weixinMessage `json:"msg"`
	BaseInfo baseInfo      `json:"base_info"`
}

type getUploadURLReq struct {
	Filekey        string   `json:"filekey,omitempty"`
	MediaType      int      `json:"media_type,omitempty"`
	ToUserID       string   `json:"to_user_id,omitempty"`
	Rawsize        int      `json:"rawsize,omitempty"`
	Rawfilemd5     string   `json:"rawfilemd5,omitempty"`
	Filesize       int      `json:"filesize,omitempty"`
	NoNeedThumb    bool     `json:"no_need_thumb,omitempty"`
	AESKey         string   `json:"aeskey,omitempty"`
	BaseInfo       baseInfo `json:"base_info"`
}

type getUploadURLResp struct {
	Ret              int    `json:"ret,omitempty"`
	ErrMsg           string `json:"errmsg,omitempty"`
	UploadParam      string `json:"upload_param,omitempty"`
	UploadFullURL    string `json:"upload_full_url,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
}

type qrCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type qrStatusResponse struct {
	Status      string `json:"status"` // "wait", "scaned", "confirmed", "expired"
	BotToken    string `json:"bot_token,omitempty"`
	ILinkBotID  string `json:"ilink_bot_id,omitempty"`
	BaseURL     string `json:"baseurl,omitempty"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
}

// ---------------------------------------------------------------------------
// IncomingMessage / outgoing types for the gateway consumer
// ---------------------------------------------------------------------------

// IncomingMessage represents a message received from a WeChat user.
type IncomingMessage struct {
	FromUserID   string
	Text         string
	ContextToken string
	Timestamp    time.Time
	// Media fields (populated when inbound message contains media)
	MediaType string // "image", "voice", "file", "video", or ""
	MediaData []byte // decrypted media bytes
	MediaName string // original filename (for file type)
}

// OutgoingText is a text message to send to a WeChat user.
type OutgoingText struct {
	ToUserID     string
	Text         string
	ContextToken string
}

// OutgoingMedia is a media message to send to a WeChat user.
type OutgoingMedia struct {
	ToUserID     string
	Caption      string
	ContextToken string
	FileData     []byte
	FileName     string
	MediaType    string // "image", "video", "file"
}

// MessageHandler is called when a message arrives from WeChat.
type MessageHandler func(msg IncomingMessage)

// StatusCallback is called when the gateway connection status changes.
type StatusCallback func(status string)

// QRLoginResult holds the result of a QR code login attempt.
type QRLoginResult struct {
	Connected bool
	BotToken  string
	AccountID string
	BaseURL   string
	UserID    string
	Message   string
}

// ---------------------------------------------------------------------------
// Context token cache
// ---------------------------------------------------------------------------

const maxContextTokenCacheSize = 1000

type contextTokenEntry struct {
	token   string
	updated time.Time
}

type contextTokenCache struct {
	mu     sync.RWMutex
	tokens map[string]contextTokenEntry
}

func newContextTokenCache() *contextTokenCache {
	return &contextTokenCache{tokens: make(map[string]contextTokenEntry)}
}

func (c *contextTokenCache) Set(userID, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[userID] = contextTokenEntry{token: token, updated: time.Now()}
	// Evict oldest entries if cache exceeds limit
	if len(c.tokens) > maxContextTokenCacheSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.tokens {
			if oldestKey == "" || v.updated.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.updated
			}
		}
		if oldestKey != "" {
			delete(c.tokens, oldestKey)
		}
	}
}

func (c *contextTokenCache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e, ok := c.tokens[userID]; ok {
		return e.token
	}
	return ""
}

// ---------------------------------------------------------------------------
// Gateway
// ---------------------------------------------------------------------------

// Gateway manages the WeChat long-polling loop on the client side.
type Gateway struct {
	config       Config
	handler      MessageHandler
	onStatus     StatusCallback
	client       *http.Client
	ctxTokens    *contextTokenCache

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool

	// Last active user for diagnostic broadcast
	lastActiveUID string

	// Per-user message processing locks — ensures messages from the same
	// user are handled sequentially while different users run concurrently.
	userLocks   map[string]*sync.Mutex
	userLocksMu sync.Mutex

	// Rate-limit queue notifications: at most one per user per 30s.
	queueNoticeMu    sync.Mutex
	queueNoticeTimes map[string]time.Time

	// handlerWg tracks in-flight handler goroutines so Stop() can wait
	// for them to finish before returning.
	handlerWg sync.WaitGroup
}

// NewGateway creates a new WeChat gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	return &Gateway{
		config:           config,
		handler:          handler,
		client:           &http.Client{},
		ctxTokens:        newContextTokenCache(),
		userLocks:        make(map[string]*sync.Mutex),
		queueNoticeTimes: make(map[string]time.Time),
	}
}

// SetStatusCallback sets a callback for connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.onStatus = cb
}

// Start launches the long-polling loop in the background.
func (g *Gateway) Start(ctx context.Context) error {
	if g.config.Token == "" {
		return fmt.Errorf("weixin: Token is required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return nil
	}
	pollCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.wg.Add(1)
	go g.pollLoop(pollCtx)
	log.Printf("[weixin/gw] started (baseURL=%s)", g.config.baseURL())
	g.emitStatus("connecting")
	return nil
}

// Stop shuts down the gateway.
func (g *Gateway) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.running = false
	g.cancel = nil
	g.mu.Unlock()

	g.wg.Wait()        // wait for pollLoop to exit
	g.handlerWg.Wait() // wait for in-flight handler goroutines
	log.Printf("[weixin/gw] stopped")
	g.emitStatus("disconnected")
	return nil
}

// IsRunning returns whether the gateway is currently running.
func (g *Gateway) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// GetContextToken returns the cached context token for a user.
func (g *Gateway) GetContextToken(userID string) string {
	return g.ctxTokens.Get(userID)
}

// LastActiveUserID returns the most recent user who sent a message.
func (g *Gateway) LastActiveUserID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastActiveUID
}

func (g *Gateway) emitStatus(status string) {
	if g.onStatus != nil {
		g.onStatus(status)
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func randomWechatUIN() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	uint32Val := binary.BigEndian.Uint32(b)
	s := strconv.FormatUint(uint64(uint32Val), 10)
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func (g *Gateway) buildHeaders() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("AuthorizationType", "ilink_bot_token")
	h.Set("X-WECHAT-UIN", randomWechatUIN())
	if g.config.Token != "" {
		h.Set("Authorization", "Bearer "+strings.TrimSpace(g.config.Token))
	}
	return h
}

func (g *Gateway) apiPost(ctx context.Context, endpoint string, body []byte, timeout time.Duration) ([]byte, error) {
	base := g.config.baseURL()
	u := base + "/" + endpoint

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = g.buildHeaders()

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, apiResponseMaxBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d: %s", endpoint, resp.StatusCode, string(data[:min(len(data), 512)]))
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Long-polling loop
// ---------------------------------------------------------------------------

func (g *Gateway) pollLoop(ctx context.Context) {
	defer g.wg.Done()
	g.emitStatus("connected")
	wl := GetWxLog()
	wl.Log("gw.pollLoop", "---", "-", "STARTED baseURL=%s", g.config.baseURL())

	var getUpdatesBuf string
	consecutiveFailures := 0
	nextTimeout := longPollTimeout

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		reqBody, _ := json.Marshal(getUpdatesReq{
			GetUpdatesBuf: getUpdatesBuf,
			BaseInfo:      baseInfo{ChannelVersion: "go-maclaw-1.0"},
		})

		data, err := g.apiPost(ctx, "ilink/bot/getupdates", reqBody, nextTimeout+10*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			consecutiveFailures++
			log.Printf("[weixin/gw] getUpdates error (%d/%d): %v", consecutiveFailures, maxConsecutiveFailures, err)
			if consecutiveFailures >= maxConsecutiveFailures {
				g.emitStatus("reconnecting")
				consecutiveFailures = 0
				sleepCtx(ctx, backoffDelay)
			} else {
				sleepCtx(ctx, retryDelay)
			}
			continue
		}

		var resp getUpdatesResp
		if err := json.Unmarshal(data, &resp); err != nil {
			consecutiveFailures++
			log.Printf("[weixin/gw] getUpdates JSON decode error: %v", err)
			sleepCtx(ctx, retryDelay)
			continue
		}

		// Update server-suggested timeout
		if resp.LongpollingTimeoutMs > 0 {
			nextTimeout = time.Duration(resp.LongpollingTimeoutMs) * time.Millisecond
		}

		// Check API errors
		isAPIError := (resp.Ret != 0) || (resp.Errcode != 0)
		if isAPIError {
			isSessionExpired := resp.Errcode == sessionExpiredErrcode || resp.Ret == sessionExpiredErrcode
			if isSessionExpired {
				wl.Log("gw.pollLoop", "---", "-", "SESSION_EXPIRED errcode=%d ret=%d — stopping gateway", resp.Errcode, resp.Ret)
				log.Printf("[weixin/gw] session expired (errcode=%d ret=%d), stopping gateway", resp.Errcode, resp.Ret)
				g.emitStatus("session_expired")
				return // exit pollLoop — caller will clean up
			}
			consecutiveFailures++
			log.Printf("[weixin/gw] getUpdates API error: ret=%d errcode=%d errmsg=%s (%d/%d)",
				resp.Ret, resp.Errcode, resp.Errmsg, consecutiveFailures, maxConsecutiveFailures)
			if consecutiveFailures >= maxConsecutiveFailures {
				g.emitStatus("reconnecting")
				consecutiveFailures = 0
				sleepCtx(ctx, backoffDelay)
			} else {
				sleepCtx(ctx, retryDelay)
			}
			continue
		}

		consecutiveFailures = 0

		// Save sync buf
		if resp.GetUpdatesBuf != "" {
			getUpdatesBuf = resp.GetUpdatesBuf
		}

		// Process messages
		for _, msg := range resp.Msgs {
			g.processIncomingMessage(ctx, msg)
		}
	}
}

// userLock returns a per-user mutex, creating one if it doesn't exist yet.
func (g *Gateway) userLock(userID string) *sync.Mutex {
	g.userLocksMu.Lock()
	defer g.userLocksMu.Unlock()
	ul, ok := g.userLocks[userID]
	if !ok {
		ul = &sync.Mutex{}
		g.userLocks[userID] = ul
	}
	return ul
}

func (g *Gateway) processIncomingMessage(ctx context.Context, msg weixinMessage) {
	wl := GetWxLog()

	// Skip bot's own messages (echoes) and deleted messages
	if msg.MessageType == MsgTypeBot || msg.DeleteTimeMs > 0 {
		wl.Log("gw.process", "IN", msg.FromUserID, "SKIP bot_echo_or_deleted msg_type=%d delete_ts=%d", msg.MessageType, msg.DeleteTimeMs)
		return
	}

	fromUserID := msg.FromUserID
	if fromUserID == "" {
		wl.Log("gw.process", "IN", "?", "SKIP empty fromUserID")
		return
	}

	// Track last active user for diagnostic broadcast
	g.mu.Lock()
	g.lastActiveUID = fromUserID
	g.mu.Unlock()

	// Cache context token
	if msg.ContextToken != "" {
		g.ctxTokens.Set(fromUserID, msg.ContextToken)
	}

	// Extract text body
	text := extractTextBody(msg.ItemList)

	// Extract media (first media item found: image > video > file > voice)
	mediaType, mediaData, mediaName := g.extractMedia(ctx, msg.ItemList)

	var ts time.Time
	if msg.CreateTimeMs > 0 {
		ts = time.UnixMilli(msg.CreateTimeMs)
	} else {
		ts = time.Now()
	}

	incoming := IncomingMessage{
		FromUserID:   fromUserID,
		Text:         text,
		ContextToken: msg.ContextToken,
		Timestamp:    ts,
		MediaType:    mediaType,
		MediaData:    mediaData,
		MediaName:    mediaName,
	}

	wl.Log("gw.process", "IN", fromUserID, "text_len=%d media=%s ctx_token=%v", len(text), mediaType, msg.ContextToken != "")

	// Dispatch handler in a goroutine so the poll loop is never blocked by
	// slow handler processing (e.g. LLM calls). A per-user mutex ensures
	// messages from the same user are still processed sequentially.
	ul := g.userLock(fromUserID)
	g.handlerWg.Add(1)
	go func() {
		defer g.handlerWg.Done()

		// Try to acquire the lock with a short deadline. If the lock is
		// already held (previous message still being processed), send a
		// one-time queued notification so the user knows the message wasn't lost.
		locked := ul.TryLock()
		if !locked {
			wl.Log("gw.dispatch", "---", fromUserID, "QUEUED lock busy, waiting for previous msg")
			// Lock is busy — notify user (rate-limited: only if no recent notification).
			if incoming.Text != "" {
				log.Printf("[weixin/gw] message queued for user=%s (lock busy), text=%s",
					fromUserID, truncateLog(incoming.Text, 50))
				ctxToken := incoming.ContextToken
				if ctxToken == "" {
					ctxToken = g.ctxTokens.Get(fromUserID)
				}
				if ctxToken != "" && g.shouldSendQueueNotice(fromUserID) {
					_ = g.SendText(context.Background(), OutgoingText{
						ToUserID:     fromUserID,
						Text:         i18n.T(i18n.MsgMessageQueued, "zh"),
						ContextToken: ctxToken,
					})
				}
			}
			// Block until the lock is available.
			ul.Lock()
		}
		defer ul.Unlock()
		wl.Log("gw.dispatch", "---", fromUserID, "LOCKED calling handler")
		g.handler(incoming)
		wl.Log("gw.dispatch", "---", fromUserID, "DONE handler returned")
	}()
}

func truncateLog(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// shouldSendQueueNotice returns true if a queue notification should be sent
// for this user. Rate-limited to at most once per 30 seconds per user.
func (g *Gateway) shouldSendQueueNotice(userID string) bool {
	g.queueNoticeMu.Lock()
	defer g.queueNoticeMu.Unlock()
	now := time.Now()
	if last, ok := g.queueNoticeTimes[userID]; ok && now.Sub(last) < 30*time.Second {
		return false
	}
	g.queueNoticeTimes[userID] = now
	return true
}

func extractTextBody(items []messageItem) string {
	for _, item := range items {
		if item.Type == ItemTypeText && item.TextItem != nil && item.TextItem.Text != "" {
			text := item.TextItem.Text
			if item.RefMsg != nil && item.RefMsg.Title != "" {
				text = fmt.Sprintf("[引用: %s]\n%s", item.RefMsg.Title, text)
			}
			return text
		}
		// Voice-to-text
		if item.Type == ItemTypeVoice && item.VoiceItem != nil && item.VoiceItem.Text != "" {
			return item.VoiceItem.Text
		}
	}
	return ""
}

func (g *Gateway) extractMedia(ctx context.Context, items []messageItem) (mediaType string, data []byte, name string) {
	// Single pass: collect first candidate per type, then try in priority order.
	type candidate struct {
		mtype string
		param string // encrypt_query_param
		aesB64 string
		name   string
	}
	var img, vid, file, voice *candidate

	for i := range items {
		item := &items[i]
		switch item.Type {
		case ItemTypeImage:
			if img != nil || item.ImageItem == nil || item.ImageItem.Media == nil || item.ImageItem.Media.EncryptQueryParam == "" {
				continue
			}
			aesKeyB64 := ""
			if item.ImageItem.AESKey != "" {
				raw, err := hex.DecodeString(item.ImageItem.AESKey)
				if err == nil {
					aesKeyB64 = base64.StdEncoding.EncodeToString(raw)
				}
			}
			if aesKeyB64 == "" && item.ImageItem.Media.AESKey != "" {
				aesKeyB64 = item.ImageItem.Media.AESKey
			}
			img = &candidate{mtype: "image", param: item.ImageItem.Media.EncryptQueryParam, aesB64: aesKeyB64}
		case ItemTypeVideo:
			if vid != nil || item.VideoItem == nil || item.VideoItem.Media == nil ||
				item.VideoItem.Media.EncryptQueryParam == "" || item.VideoItem.Media.AESKey == "" {
				continue
			}
			vid = &candidate{mtype: "video", param: item.VideoItem.Media.EncryptQueryParam, aesB64: item.VideoItem.Media.AESKey}
		case ItemTypeFile:
			if file != nil || item.FileItem == nil || item.FileItem.Media == nil ||
				item.FileItem.Media.EncryptQueryParam == "" || item.FileItem.Media.AESKey == "" {
				continue
			}
			file = &candidate{mtype: "file", param: item.FileItem.Media.EncryptQueryParam, aesB64: item.FileItem.Media.AESKey, name: item.FileItem.FileName}
		case ItemTypeVoice:
			if voice != nil || item.VoiceItem == nil || item.VoiceItem.Text != "" ||
				item.VoiceItem.Media == nil || item.VoiceItem.Media.EncryptQueryParam == "" || item.VoiceItem.Media.AESKey == "" {
				continue
			}
			voice = &candidate{mtype: "voice", param: item.VoiceItem.Media.EncryptQueryParam, aesB64: item.VoiceItem.Media.AESKey}
		}
	}

	// Try in priority order: image > video > file > voice
	for _, c := range []*candidate{img, vid, file, voice} {
		if c == nil {
			continue
		}
		if c.aesB64 != "" {
			buf, err := g.cdnDownloadDecrypt(ctx, c.param, c.aesB64)
			if err != nil {
				log.Printf("[weixin/gw] %s download failed: %v", c.mtype, err)
				continue
			}
			return c.mtype, buf, c.name
		}
		// Image may have no AES key — try plain download
		if c.mtype == "image" {
			buf, err := g.cdnDownloadPlain(ctx, c.param)
			if err != nil {
				log.Printf("[weixin/gw] image plain download failed: %v", err)
				continue
			}
			return "image", buf, ""
		}
	}
	return "", nil, ""
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ---------------------------------------------------------------------------
// Outbound messaging
// ---------------------------------------------------------------------------

func generateClientID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "maclaw-wx-" + hex.EncodeToString(b)
}

// SendText sends a text message to a WeChat user.
// Texts longer than 4000 characters are split into chunks.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	wl := GetWxLog()
	if msg.Text == "" {
		wl.Log("gw.SendText", "OUT", msg.ToUserID, "SKIP empty text")
		return nil
	}
	if msg.ContextToken == "" {
		// Try cached token
		msg.ContextToken = g.ctxTokens.Get(msg.ToUserID)
	}
	if msg.ContextToken == "" {
		wl.Log("gw.SendText", "OUT", msg.ToUserID, "ERR no contextToken")
		return fmt.Errorf("weixin: contextToken is required for sending to %s", msg.ToUserID)
	}

	wl.Log("gw.SendText", "OUT", msg.ToUserID, "text_len=%d ctx_token_len=%d", len([]rune(msg.Text)), len(msg.ContextToken))
	runes := []rune(msg.Text)
	chunkIdx := 0
	for len(runes) > 0 {
		chunk := runes
		if len(chunk) > textChunkLimit {
			chunk = runes[:textChunkLimit]
		}
		runes = runes[len(chunk):]
		if err := g.sendTextChunk(ctx, msg.ToUserID, string(chunk), msg.ContextToken); err != nil {
			wl.Log("gw.SendText", "OUT", msg.ToUserID, "ERR chunk=%d: %v", chunkIdx, err)
			return err
		}
		chunkIdx++
	}
	wl.Log("gw.SendText", "OUT", msg.ToUserID, "OK chunks=%d", chunkIdx)
	return nil
}

func (g *Gateway) sendTextChunk(ctx context.Context, to, text, contextToken string) error {
	clientID := generateClientID()
	req := sendMessageReq{
		Msg: weixinMessage{
			ToUserID:     to,
			ClientID:     clientID,
			CreateTimeMs: time.Now().UnixMilli(),
			MessageType:  MsgTypeBot,
			MessageState: MsgStateFinish,
			ContextToken: contextToken,
			ItemList: []messageItem{
				{Type: ItemTypeText, TextItem: &textItem{Text: text}},
			},
		},
		BaseInfo: baseInfo{ChannelVersion: "go-maclaw-1.0"},
	}
	body, _ := json.Marshal(req)
	_, err := g.apiPost(ctx, "ilink/bot/sendmessage", body, apiTimeout)
	return err
}

// SendMedia uploads media to CDN and sends it to a WeChat user.
func (g *Gateway) SendMedia(ctx context.Context, msg OutgoingMedia) error {
	wl := GetWxLog()
	if msg.ContextToken == "" {
		msg.ContextToken = g.ctxTokens.Get(msg.ToUserID)
	}
	if msg.ContextToken == "" {
		wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "ERR no contextToken media=%s", msg.MediaType)
		return fmt.Errorf("weixin: contextToken is required for sending media to %s", msg.ToUserID)
	}
	if len(msg.FileData) == 0 {
		wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "ERR empty file data")
		return fmt.Errorf("weixin: empty file data")
	}

	wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "media=%s size=%d name=%s", msg.MediaType, len(msg.FileData), msg.FileName)

	// Determine upload media type
	uploadType := UploadMediaFile
	switch msg.MediaType {
	case "image":
		uploadType = UploadMediaImage
	case "video":
		uploadType = UploadMediaVideo
	}

	// Upload to CDN
	uploaded, err := g.uploadToCDN(ctx, msg.FileData, msg.ToUserID, uploadType)
	if err != nil {
		return fmt.Errorf("weixin: CDN upload failed: %w", err)
	}

	// Send caption as separate text item if present
	if msg.Caption != "" {
		if err := g.sendTextChunk(ctx, msg.ToUserID, msg.Caption, msg.ContextToken); err != nil {
			log.Printf("[weixin/gw] SendMedia caption error (to=%s): %v", msg.ToUserID, err)
		}
	}

	// Build media message item
	var item messageItem
	// AES key for cdnMedia: base64(hex_string) — the hex-encoded key is 32 chars,
	// then base64-encode that string. Matches the TS reference implementation.
	aesKeyHex := hex.EncodeToString(uploaded.aesKey)
	aesKeyForMedia := base64.StdEncoding.EncodeToString([]byte(aesKeyHex))
	switch msg.MediaType {
	case "image":
		item = messageItem{
			Type: ItemTypeImage,
			ImageItem: &imageItem{
				Media: &cdnMedia{
					EncryptQueryParam: uploaded.downloadParam,
					AESKey:            aesKeyForMedia,
					EncryptType:       1,
				},
				MidSize: uploaded.ciphertextSize,
			},
		}
	case "video":
		item = messageItem{
			Type: ItemTypeVideo,
			VideoItem: &videoItem{
				Media: &cdnMedia{
					EncryptQueryParam: uploaded.downloadParam,
					AESKey:            aesKeyForMedia,
					EncryptType:       1,
				},
				VideoSize: uploaded.ciphertextSize,
			},
		}
	default: // file
		item = messageItem{
			Type: ItemTypeFile,
			FileItem: &fileItem{
				Media: &cdnMedia{
					EncryptQueryParam: uploaded.downloadParam,
					AESKey:            aesKeyForMedia,
					EncryptType:       1,
				},
				FileName: msg.FileName,
				Len:      strconv.Itoa(uploaded.plaintextSize),
			},
		}
	}

	clientID := generateClientID()
	req := sendMessageReq{
		Msg: weixinMessage{
			ToUserID:     msg.ToUserID,
			ClientID:     clientID,
			CreateTimeMs: time.Now().UnixMilli(),
			MessageType:  MsgTypeBot,
			MessageState: MsgStateFinish,
			ContextToken: msg.ContextToken,
			ItemList:     []messageItem{item},
		},
		BaseInfo: baseInfo{ChannelVersion: "go-maclaw-1.0"},
	}
	body, _ := json.Marshal(req)
	_, err = g.apiPost(ctx, "ilink/bot/sendmessage", body, apiTimeout)
	return err
}

// ---------------------------------------------------------------------------
// AES-128-ECB encryption/decryption for CDN
// ---------------------------------------------------------------------------

// aesEcbPaddedSize computes AES-128-ECB ciphertext size with PKCS7 padding.
// Formula: ceil((plaintextSize+1)/16) * 16
// PKCS7 always adds at least 1 byte of padding, so +1 before ceiling.
func aesEcbPaddedSize(plaintextSize int) int {
	return ((plaintextSize + 1 + 15) / 16) * 16
}

// pkcs7Pad pads data to blockSize using PKCS7.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

// pkcs7Unpad removes PKCS7 padding.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding: %d", padding)
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, fmt.Errorf("invalid PKCS7 padding bytes")
		}
	}
	return data[:len(data)-padding], nil
}

// encryptAESECB encrypts plaintext with AES-128-ECB + PKCS7 padding.
func encryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(ciphertext[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return ciphertext, nil
}

// decryptAESECB decrypts AES-128-ECB ciphertext and removes PKCS7 padding.
func decryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext not multiple of block size")
	}
	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], ciphertext[i:i+aes.BlockSize])
	}
	return pkcs7Unpad(plaintext)
}

// parseAESKey parses a base64-encoded AES key. Handles two formats:
// 1. base64(raw 16 bytes) — images
// 2. base64(hex string of 16 bytes) — file/voice/video
func parseAESKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		// Try RawStdEncoding
		decoded, err = base64.RawStdEncoding.DecodeString(aesKeyBase64)
		if err != nil {
			return nil, fmt.Errorf("base64 decode failed: %w", err)
		}
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 {
		// Check if it's hex-encoded
		s := string(decoded)
		raw, err := hex.DecodeString(s)
		if err == nil && len(raw) == 16 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("aes_key must decode to 16 raw bytes or 32-char hex, got %d bytes", len(decoded))
}

// ---------------------------------------------------------------------------
// CDN download
// ---------------------------------------------------------------------------

func (g *Gateway) cdnDownloadDecrypt(ctx context.Context, encryptedQueryParam, aesKeyBase64 string) ([]byte, error) {
	key, err := parseAESKey(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("parse aes key: %w", err)
	}
	cdnBase := g.config.cdnURL()
	dlURL := cdnBase + "/download?encrypted_query_param=" + url.QueryEscape(encryptedQueryParam)

	dlCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, "GET", dlURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("CDN download %d: %s", resp.StatusCode, string(body))
	}
	encrypted, err := io.ReadAll(io.LimitReader(resp.Body, cdnDownloadMaxBytes))
	if err != nil {
		return nil, err
	}
	return decryptAESECB(encrypted, key)
}

func (g *Gateway) cdnDownloadPlain(ctx context.Context, encryptedQueryParam string) ([]byte, error) {
	cdnBase := g.config.cdnURL()
	dlURL := cdnBase + "/download?encrypted_query_param=" + url.QueryEscape(encryptedQueryParam)

	dlCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, "GET", dlURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("CDN download %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(io.LimitReader(resp.Body, cdnDownloadMaxBytes))
}

// ---------------------------------------------------------------------------
// CDN upload
// ---------------------------------------------------------------------------

type uploadResult struct {
	downloadParam  string
	aesKey         []byte
	plaintextSize  int
	ciphertextSize int
}

func (g *Gateway) uploadToCDN(ctx context.Context, plaintext []byte, toUserID string, mediaType int) (*uploadResult, error) {
	rawsize := len(plaintext)
	h := md5.Sum(plaintext)
	rawfilemd5 := hex.EncodeToString(h[:])
	filesize := aesEcbPaddedSize(rawsize)

	filekeyBuf := make([]byte, 16)
	_, _ = rand.Read(filekeyBuf)
	filekey := hex.EncodeToString(filekeyBuf)

	aesKey := make([]byte, 16)
	_, _ = rand.Read(aesKey)

	// Step 1: getUploadUrl
	uploadReq := getUploadURLReq{
		Filekey:     filekey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		Rawsize:     rawsize,
		Rawfilemd5:  rawfilemd5,
		Filesize:    filesize,
		NoNeedThumb: true,
		AESKey:      hex.EncodeToString(aesKey),
		BaseInfo:    baseInfo{ChannelVersion: "go-maclaw-1.0"},
	}
	reqBody, _ := json.Marshal(uploadReq)
	data, err := g.apiPost(ctx, "ilink/bot/getuploadurl", reqBody, apiTimeout)
	if err != nil {
		return nil, fmt.Errorf("getUploadUrl: %w", err)
	}
	var uploadResp getUploadURLResp
	if err := json.Unmarshal(data, &uploadResp); err != nil {
		return nil, fmt.Errorf("getUploadUrl decode: %w", err)
	}
	if uploadResp.UploadParam == "" && uploadResp.UploadFullURL == "" {
		return nil, fmt.Errorf("getUploadUrl returned no upload_param and no upload_full_url, ret=%d errmsg=%q resp=%s",
			uploadResp.Ret, uploadResp.ErrMsg, string(data))
	}

	// Step 2: Encrypt and upload to CDN
	ciphertext, err := encryptAESECB(plaintext, aesKey)
	if err != nil {
		return nil, fmt.Errorf("AES encrypt: %w", err)
	}

	// Prefer upload_full_url (new API format) over legacy upload_param + cdnBase concatenation.
	var cdnURL string
	if uploadResp.UploadFullURL != "" {
		cdnURL = uploadResp.UploadFullURL
	} else {
		cdnBase := g.config.cdnURL()
		cdnURL = cdnBase + "/upload?encrypted_query_param=" + url.QueryEscape(uploadResp.UploadParam) +
			"&filekey=" + url.QueryEscape(filekey)
	}

	var downloadParam string
	var lastErr error
	for attempt := 1; attempt <= cdnUploadMaxRetries; attempt++ {
		uploadCtx, uploadCancel := context.WithTimeout(ctx, 2*time.Minute)
		uploadReqHTTP, err := http.NewRequestWithContext(uploadCtx, "POST", cdnURL, bytes.NewReader(ciphertext))
		if err != nil {
			uploadCancel()
			return nil, err
		}
		uploadReqHTTP.Header.Set("Content-Type", "application/octet-stream")

		uploadResp, err := g.client.Do(uploadReqHTTP)
		uploadCancel()
		if err != nil {
			lastErr = err
			if attempt < cdnUploadMaxRetries {
				log.Printf("[weixin/gw] CDN upload attempt %d failed: %v", attempt, err)
				continue
			}
			break
		}
		// Read X-Encrypted-Param before draining body.
		respDownloadParam := uploadResp.Header.Get("X-Encrypted-Param")
		respStatus := uploadResp.StatusCode
		// Drain body so the underlying TCP connection can be reused.
		_, _ = io.Copy(io.Discard, uploadResp.Body)
		uploadResp.Body.Close()

		if respStatus >= 400 && respStatus < 500 {
			return nil, fmt.Errorf("CDN upload client error %d", respStatus)
		}
		if respStatus != 200 {
			lastErr = fmt.Errorf("CDN upload server error %d", respStatus)
			if attempt < cdnUploadMaxRetries {
				log.Printf("[weixin/gw] CDN upload attempt %d: %v", attempt, lastErr)
				continue
			}
			break
		}

		if respDownloadParam == "" {
			lastErr = fmt.Errorf("CDN response missing X-Encrypted-Param header")
			if attempt < cdnUploadMaxRetries {
				continue
			}
			break
		}
		downloadParam = respDownloadParam
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, fmt.Errorf("CDN upload failed: %w", lastErr)
	}

	return &uploadResult{
		downloadParam:  downloadParam,
		aesKey:         aesKey,
		plaintextSize:  rawsize,
		ciphertextSize: len(ciphertext),
	}, nil
}

// ---------------------------------------------------------------------------
// QR Code Login
// ---------------------------------------------------------------------------

// StartQRLogin fetches a QR code from the iLink API for WeChat login.
// Returns the QR code image URL and a qrcode token for polling status.
// qrHTTPClient is shared across QR login functions to reuse connections.
var qrHTTPClient = &http.Client{Timeout: 40 * time.Second}

func StartQRLogin(ctx context.Context, baseURL, botType string) (qrcodeURL string, qrcodeToken string, err error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if botType == "" {
		botType = DefaultBotType
	}
	base := strings.TrimRight(baseURL, "/")
	u := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", base, url.QueryEscape(botType))

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", u, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := qrHTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("get_bot_qrcode returned %d: %s", resp.StatusCode, string(body))
	}
	var qr qrCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return "", "", fmt.Errorf("decode QR response: %w", err)
	}
	return qr.QRCodeImgContent, qr.QRCode, nil
}

// PollQRStatus polls the QR code login status once. Returns the status response.
// This is a long-poll call (up to 35s). Call in a loop until status is "confirmed" or "expired".
func PollQRStatus(ctx context.Context, baseURL, qrcodeToken string) (*QRLoginResult, string, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	base := strings.TrimRight(baseURL, "/")
	u := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", base, url.QueryEscape(qrcodeToken))

	pollCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")

	resp, err := qrHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &QRLoginResult{Message: "超时"}, "wait", nil
		}
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("get_qrcode_status returned %d: %s", resp.StatusCode, string(data[:min(len(data), 512)]))
	}

	var status qrStatusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, "", fmt.Errorf("decode status: %w", err)
	}

	switch status.Status {
	case "confirmed":
		if status.ILinkBotID == "" {
			return &QRLoginResult{
				Connected: false,
				Message:   "登录失败：服务器未返回 ilink_bot_id",
			}, "confirmed", nil
		}
		return &QRLoginResult{
			Connected: true,
			BotToken:  status.BotToken,
			AccountID: status.ILinkBotID,
			BaseURL:   status.BaseURL,
			UserID:    status.ILinkUserID,
			Message:   "✅ 与微信连接成功！",
		}, "confirmed", nil
	case "scaned":
		return &QRLoginResult{Message: "已扫码，请在微信确认"}, "scaned", nil
	case "expired":
		return &QRLoginResult{Message: "二维码已过期"}, "expired", nil
	default: // "wait"
		return &QRLoginResult{Message: "等待扫码..."}, "wait", nil
	}
}

// WaitForQRLogin polls QR status in a loop until confirmed, expired, or timeout.
func WaitForQRLogin(ctx context.Context, baseURL, qrcodeToken string, timeout time.Duration) (*QRLoginResult, error) {
	if timeout <= 0 {
		timeout = 8 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		result, status, err := PollQRStatus(ctx, baseURL, qrcodeToken)
		if err != nil {
			if ctx.Err() != nil {
				return &QRLoginResult{Message: "登录超时"}, nil
			}
			return nil, err
		}
		switch status {
		case "confirmed":
			return result, nil
		case "expired":
			return result, nil
		}
		// "wait" or "scaned" — continue polling
		select {
		case <-ctx.Done():
			return &QRLoginResult{Message: "登录超时"}, nil
		case <-time.After(1 * time.Second):
		}
	}
}
