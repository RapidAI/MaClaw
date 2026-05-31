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
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	silk "github.com/wdvxdr1123/go-silk"
)

const (
	DefaultBaseURL        = "https://ilinkai.weixin.qq.com"
	DefaultCDNBaseURL     = "https://novac2c.cdn.weixin.qq.com/c2c"
	DefaultBotType        = "3"
	weixinVoiceSampleRate = 16000
	weixinVoiceBitRate    = 8000
	weixinVoiceDebugKeep  = 20

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

var weixinVoiceDebugDirForTest string

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
	Format        string    `json:"format,omitempty"`
	MimeType      string    `json:"mime_type,omitempty"`
	BitsPerSample int       `json:"bits_per_sample,omitempty"`
	SampleRate    int       `json:"sample_rate,omitempty"`
	Playtime      int       `json:"playtime,omitempty"`
	Len           string    `json:"len,omitempty"`
	Size          string    `json:"size,omitempty"`
	VoiceMD5      string    `json:"voice_md5,omitempty"`
	MD5           string    `json:"md5,omitempty"`
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
	Type         int         `json:"type,omitempty"`
	CreateTimeMs int64       `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64       `json:"update_time_ms,omitempty"`
	IsCompleted  bool        `json:"is_completed,omitempty"`
	MsgID        string      `json:"msg_id,omitempty"`
	RefMsg       *refMessage `json:"ref_msg,omitempty"`
	TextItem     *textItem   `json:"text_item,omitempty"`
	ImageItem    *imageItem  `json:"image_item,omitempty"`
	VoiceItem    *voiceItem  `json:"voice_item,omitempty"`
	FileItem     *fileItem   `json:"file_item,omitempty"`
	VideoItem    *videoItem  `json:"video_item,omitempty"`
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
	Seq          int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMs int64         `json:"create_time_ms,omitempty"`
	UpdateTimeMs int64         `json:"update_time_ms,omitempty"`
	DeleteTimeMs int64         `json:"delete_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []messageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type getUpdatesReq struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      baseInfo `json:"base_info"`
}

type getUpdatesResp struct {
	Ret                  int             `json:"ret,omitempty"`
	Errcode              int             `json:"errcode,omitempty"`
	Errmsg               string          `json:"errmsg,omitempty"`
	Msgs                 []weixinMessage `json:"msgs,omitempty"`
	GetUpdatesBuf        string          `json:"get_updates_buf,omitempty"`
	LongpollingTimeoutMs int64           `json:"longpolling_timeout_ms,omitempty"`
}

type sendMessageReq struct {
	Msg      weixinMessage `json:"msg"`
	BaseInfo baseInfo      `json:"base_info"`
}

type getUploadURLReq struct {
	Filekey     string   `json:"filekey,omitempty"`
	MediaType   int      `json:"media_type,omitempty"`
	ToUserID    string   `json:"to_user_id,omitempty"`
	Rawsize     int      `json:"rawsize,omitempty"`
	Rawfilemd5  string   `json:"rawfilemd5,omitempty"`
	Filesize    int      `json:"filesize,omitempty"`
	NoNeedThumb bool     `json:"no_need_thumb,omitempty"`
	AESKey      string   `json:"aeskey,omitempty"`
	BaseInfo    baseInfo `json:"base_info"`
}

type getUploadURLResp struct {
	Ret              int    `json:"ret,omitempty"`
	ErrMsg           string `json:"errmsg,omitempty"`
	UploadParam      string `json:"upload_param,omitempty"`
	UploadFullURL    string `json:"upload_full_url,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
}

type apiStatusResp struct {
	Ret     *int   `json:"ret,omitempty"`
	Errcode *int   `json:"errcode,omitempty"`
	Code    *int   `json:"code,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
	Message string `json:"message,omitempty"`
}

type qrCodeResponse struct {
	Ret              *int   `json:"ret,omitempty"`
	ErrCode          *int   `json:"errcode,omitempty"`
	ErrMsg           string `json:"errmsg,omitempty"`
	Message          string `json:"message,omitempty"`
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type qrStatusResponse struct {
	Status      QRLoginStatus `json:"status"`
	Ret         *int          `json:"ret,omitempty"`
	ErrCode     *int          `json:"errcode,omitempty"`
	BotToken    string        `json:"bot_token,omitempty"`
	ILinkBotID  string        `json:"ilink_bot_id,omitempty"`
	BaseURL     string        `json:"baseurl,omitempty"`
	ILinkUserID string        `json:"ilink_user_id,omitempty"`
	Message     string        `json:"message,omitempty"`
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
	MediaType    string // "image", "video", "voice", "file"
	VoiceVariant string // optional: WeChat native voice wire-format experiment name
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

var ErrQRCodeTokenEmpty = errors.New("qrcode token is empty")

type qrLoginServerError struct {
	Op      string
	Message string
}

func (e *qrLoginServerError) Error() string {
	return fmt.Sprintf("%s failed: %s", e.Op, firstNonEmpty(e.Message, "server returned an error"))
}

func IsQRLoginRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrQRCodeTokenEmpty) {
		return false
	}
	var serverErr *qrLoginServerError
	if errors.As(err, &serverErr) {
		return false
	}
	return true
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
	config    Config
	handler   MessageHandler
	onStatus  StatusCallback
	client    *http.Client
	ctxTokens *contextTokenCache

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

	// interruptHandler is called when a new message arrives while the
	// per-user lock is held (agent loop running). If it returns handled=true,
	// the message is processed immediately (cancel/merge/status) without
	// waiting for the lock. If handled=false, the message is queued normally.
	interruptHandler progress.InterruptHandler

	// correctionStore tracks pending correction options so numbered replies
	// (e.g. "1" for "改为打断") can be resolved to the correct action.
	correctionStore *progress.CorrectionStore

	// lastCorrectionID stores the most recent correction ID per user so
	// short numbered replies can be matched to the right correction set.
	lastCorrectionID sync.Map // map[userID]string
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
		correctionStore:  progress.NewCorrectionStore(),
	}
}

// SetStatusCallback sets a callback for connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.onStatus = cb
}

// SetInterruptHandler sets the handler for interrupt signals from incoming
// messages during active agent loops.
func (g *Gateway) SetInterruptHandler(ih progress.InterruptHandler) {
	g.interruptHandler = ih
}

// handleCorrectionReply checks if the incoming text is a numbered correction
// reply (e.g. "1" or "2") matching a pending correction set. If so, it
// executes the correction and sends the result. Returns true if handled.
//
// When the correction resolves to Queue or Replace, the original message is
// re-dispatched through the normal handler path so it gets processed as a
// task (Queue waits for the lock; Replace runs after cancellation).
func (g *Gateway) handleCorrectionReply(userID, text, ctxToken string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	// Parse single-digit number.
	if len(text) != 1 || text[0] < '1' || text[0] > '9' {
		return false
	}
	idx := int(text[0]-'0') - 1 // 0-based

	// Look up the last correction ID for this user.
	corrIDVal, ok := g.lastCorrectionID.Load(userID)
	if !ok {
		return false
	}
	corrID, _ := corrIDVal.(string)

	// Atomic consume — no separate Lookup to avoid TOCTOU races.
	cUserID, msgText, originalAction, chosen, found := g.correctionStore.Consume(corrID, idx)
	if !found {
		return false
	}
	g.lastCorrectionID.Delete(userID)
	_ = cUserID // same as userID

	// Parse the chosen action string back to ScheduleAction.
	chosenAction, validAction := progress.ActionFromString(chosen.Action)
	if !validAction {
		log.Printf("[weixin/gw] invalid correction action: %q", chosen.Action)
		return false
	}

	// Execute the correction via the interrupt handler.
	ih, ok := g.interruptHandler.(interface {
		HandleCorrection(userID, messageText string, originalAction, correctionAction progress.ScheduleAction) progress.InterruptResult
	})
	if !ok {
		return false
	}

	result := ih.HandleCorrection(userID, msgText, originalAction, chosenAction)

	if result.Reply != "" {
		if ctxToken == "" {
			ctxToken = g.ctxTokens.Get(userID)
		}
		if ctxToken != "" {
			_ = g.SendText(context.Background(), OutgoingText{
				ToUserID:     userID,
				Text:         result.Reply,
				ContextToken: ctxToken,
			})
		}
	}

	// If the correction is Queue or Replace, the original message needs to
	// be re-dispatched through the normal handler. For Queue, it waits for
	// the current task to finish (lock serialization). For Replace, the
	// current task was just cancelled, so the lock should be available soon.
	if chosenAction == progress.ActionQueue || chosenAction == progress.ActionReplace {
		reIncoming := IncomingMessage{
			FromUserID:   userID,
			Text:         msgText,
			ContextToken: ctxToken,
			Timestamp:    time.Now(),
		}
		g.handlerWg.Add(1)
		go func() {
			defer g.handlerWg.Done()
			ul := g.userLock(userID)
			ul.Lock()
			defer ul.Unlock()
			g.handler(reIncoming)
		}()
	}

	log.Printf("[weixin/gw] correction handled: user=%s original=%s correction=%s reply=%q",
		userID, originalAction, chosenAction, result.Reply)
	return true
}

// scheduleCorrectionFallback starts a timer that re-dispatches the held
// message as a normal queued task if the user doesn't respond to the
// confirmation within the TTL. This prevents message loss when the user
// ignores the correction prompt.
//
// The goroutine checks g.running after the sleep to avoid re-dispatching
// after the gateway has been stopped.
func (g *Gateway) scheduleCorrectionFallback(corrID, userID string, msg IncomingMessage, ttl time.Duration) {
	go func() {
		time.Sleep(ttl)
		// Check if gateway is still running.
		g.mu.Lock()
		running := g.running
		g.mu.Unlock()
		if !running {
			return
		}
		// Try to remove — if still present, user didn't respond.
		if !g.correctionStore.Remove(corrID) {
			return // Already consumed by user or invalidated.
		}
		// Re-dispatch as normal queued message.
		g.lastCorrectionID.Delete(userID)
		log.Printf("[weixin/gw] correction TTL expired for user=%s, re-dispatching as queued message", userID)
		g.handlerWg.Add(1)
		go func() {
			defer g.handlerWg.Done()
			ul := g.userLock(userID)
			ul.Lock()
			defer ul.Unlock()
			g.handler(msg)
		}()
	}()
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
	g.emitStatus("connecting")
	g.wg.Add(1)
	go g.pollLoop(pollCtx)
	log.Printf("[weixin/gw] started (baseURL=%s)", g.config.baseURL())
	return nil
}

// Stop shuts down the gateway.
func (g *Gateway) Stop() error {
	wl := GetWxLog()
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	wl.Log("gw.stop", "---", "-", "begin")
	if g.cancel != nil {
		g.cancel()
	}
	g.running = false
	g.cancel = nil
	g.mu.Unlock()

	g.wg.Wait()        // wait for pollLoop to exit
	g.handlerWg.Wait() // wait for in-flight handler goroutines
	log.Printf("[weixin/gw] stopped")
	wl.Log("gw.stop", "---", "-", "done")
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

func validateAPIStatus(label string, data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '{' {
		return nil
	}
	var status apiStatusResp
	if err := json.Unmarshal(trimmed, &status); err != nil {
		return nil
	}
	if apiStatusValue(status.Ret) != 0 || apiStatusValue(status.Errcode) != 0 || apiStatusCodeError(status.Code) {
		return fmt.Errorf("weixin: %s API error ret=%d errcode=%d code=%d errmsg=%q resp=%s", label, apiStatusValue(status.Ret), apiStatusValue(status.Errcode), apiStatusValue(status.Code), firstNonEmpty(status.ErrMsg, status.Message), compactAPIResponseLog(trimmed))
	}
	return nil
}

func apiStatusValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func apiStatusCodeError(v *int) bool {
	if v == nil {
		return false
	}
	return *v != 0 && *v != http.StatusOK
}

func compactAPIResponseLog(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "<empty>"
	}
	return string(trimmed[:min(len(trimmed), 512)])
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

	// Extract media (first media item found: image > video > file > voice).
	// Transcribed voice messages may carry voice_item.text without downloadable
	// media; keep the voice modality so reply selection can preserve it.
	mediaType, mediaData, mediaName := g.extractMedia(ctx, fromUserID, msg.ItemList)
	if mediaType == "" && hasVoiceItem(msg.ItemList) {
		mediaType = "voice"
	}
	if summary := voiceItemsDebugSummary(msg.ItemList); summary != "" {
		wl.Log("gw.process", "IN", fromUserID, "voice_items=%s", summary)
	}

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

			// Check if this is a numbered correction reply (e.g. "1", "2").
			if g.correctionStore != nil && incoming.Text != "" {
				if handled := g.handleCorrectionReply(fromUserID, incoming.Text, incoming.ContextToken); handled {
					return
				}
			}

			// Try interrupt handler first — it can cancel/merge/query the
			// running agent loop without waiting for the lock.
			if g.interruptHandler != nil && incoming.Text != "" {
				result := g.interruptHandler.TryInterrupt(fromUserID, incoming.Text)
				if result.PendingConfirm {
					// Scheduler is uncertain — hold the message and ask user.
					// Store in CorrectionStore; if TTL expires without user
					// action, re-dispatch as a normal queued message.
					wl.Log("gw.dispatch", "---", fromUserID, "PENDING_CONFIRM action=%s reply=%q", result.Action, result.Reply)
					replyText := result.Reply
					if len(result.Corrections) > 0 && replyText != "" {
						replyText = progress.FormatCorrectionsText(replyText, result.Corrections)
					}
					if g.correctionStore != nil {
						corrID := g.correctionStore.Store(fromUserID, incoming.Text, result.Action, result.Corrections)
						g.lastCorrectionID.Store(fromUserID, corrID)
						// Schedule fallback: if user doesn't respond within
						// TTL, re-dispatch the message as a normal task.
						g.scheduleCorrectionFallback(corrID, fromUserID, incoming, time.Duration(progress.DefaultCorrectionTTL)*time.Second)
					}
					if replyText != "" {
						ctxToken := incoming.ContextToken
						if ctxToken == "" {
							ctxToken = g.ctxTokens.Get(fromUserID)
						}
						if ctxToken != "" {
							_ = g.SendText(context.Background(), OutgoingText{
								ToUserID:     fromUserID,
								Text:         replyText,
								ContextToken: ctxToken,
							})
						}
					}
					return // Message held — not consumed, not queued.
				}
				if result.Handled || result.Queued {
					wl.Log("gw.dispatch", "---", fromUserID, "INTERRUPT action=%s handled=%v queued=%v reply=%q", result.Action, result.Handled, result.Queued, result.Reply)
					replyText := result.Reply
					// Append correction options as numbered text links for WeChat.
					if len(result.Corrections) > 0 && replyText != "" {
						replyText = progress.FormatCorrectionsText(replyText, result.Corrections)
						// Store corrections so numbered replies can be resolved.
						if g.correctionStore != nil {
							corrID := g.correctionStore.Store(fromUserID, incoming.Text, result.Action, result.Corrections)
							g.lastCorrectionID.Store(fromUserID, corrID)
						}
					}
					if replyText != "" {
						ctxToken := incoming.ContextToken
						if ctxToken == "" {
							ctxToken = g.ctxTokens.Get(fromUserID)
						}
						if ctxToken != "" {
							_ = g.SendText(context.Background(), OutgoingText{
								ToUserID:     fromUserID,
								Text:         replyText,
								ContextToken: ctxToken,
							})
						}
					}
					if result.Handled {
						// Fully handled (Replace/Merge/StatusQuery) — don't queue.
						return
					}
					// Queued — reply was sent as instant feedback, but the
					// message continues to the normal queuing path below.
					// It will be processed after the current loop releases
					// the per-user lock.
				}
			}

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

func hasVoiceItem(items []messageItem) bool {
	for _, item := range items {
		if item.Type == ItemTypeVoice && item.VoiceItem != nil {
			return true
		}
	}
	return false
}

func voiceItemsDebugSummary(items []messageItem) string {
	out := make([]json.RawMessage, 0, 2)
	for _, item := range items {
		if item.Type != ItemTypeVoice || item.VoiceItem == nil {
			continue
		}
		out = append(out, json.RawMessage(voiceItemDebugSummary(item.VoiceItem)))
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "<marshal-error>"
	}
	return string(b)
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

func (g *Gateway) extractMedia(ctx context.Context, fromUserID string, items []messageItem) (mediaType string, data []byte, name string) {
	wl := GetWxLog()
	// Single pass: collect first candidate per type, then try in priority order.
	type candidate struct {
		mtype  string
		param  string // encrypt_query_param
		aesB64 string
		name   string
		voice  *voiceItem
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
			if voice != nil || item.VoiceItem == nil ||
				item.VoiceItem.Media == nil || item.VoiceItem.Media.EncryptQueryParam == "" || item.VoiceItem.Media.AESKey == "" {
				continue
			}
			voice = &candidate{mtype: "voice", param: item.VoiceItem.Media.EncryptQueryParam, aesB64: item.VoiceItem.Media.AESKey, voice: item.VoiceItem}
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
				wl.Log("gw.download", "IN", fromUserID, "ERR media=%s decrypt download_param_len=%d aes_key_len=%d err=%v", c.mtype, len(c.param), len(c.aesB64), err)
				continue
			}
			if c.mtype == "voice" {
				path, saveErr := saveDebugWeixinInboundVoicePayload(buf, c.voice)
				wl.Log("gw.download", "IN", fromUserID, "OK media=voice bytes=%d %s saved=%q save_err=%v", len(buf), voicePayloadDebugSummary(buf), path, saveErr)
			} else {
				wl.Log("gw.download", "IN", fromUserID, "OK media=%s bytes=%d download_param_len=%d", c.mtype, len(buf), len(c.param))
			}
			return c.mtype, buf, c.name
		}
		// Image may have no AES key — try plain download
		if c.mtype == "image" {
			buf, err := g.cdnDownloadPlain(ctx, c.param)
			if err != nil {
				log.Printf("[weixin/gw] image plain download failed: %v", err)
				wl.Log("gw.download", "IN", fromUserID, "ERR media=image plain download_param_len=%d err=%v", len(c.param), err)
				continue
			}
			wl.Log("gw.download", "IN", fromUserID, "OK media=image plain bytes=%d download_param_len=%d", len(buf), len(c.param))
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
	data, err := g.apiPost(ctx, "ilink/bot/sendmessage", body, apiTimeout)
	if err != nil {
		return err
	}
	return validateAPIStatus("sendmessage", data)
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

	uploadData := msg.FileData
	var voiceMeta *voiceMetadata
	if msg.MediaType == "voice" {
		payload, meta, err := voiceUploadPayload(msg.FileName, msg.FileData)
		if err != nil {
			return err
		}
		uploadData = payload
		if meta != nil {
			voiceMeta = meta
		}
		if debugPath, err := saveDebugWeixinVoicePayload(uploadData, voiceMeta); err != nil {
			log.Printf("[weixin/gw] save debug voice failed: %v", err)
		} else if debugPath != "" {
			log.Printf("[weixin/gw] saved debug SILK voice payload: %s", debugPath)
		}
	}

	if msg.MediaType == "voice" && voiceMeta != nil {
		wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "media=%s size=%d upload_size=%d payload_codec=silk_v3 voice_item_encode_type=4 sample_rate=%d playtime=%d name=%s packets=%d packet_bytes=%d packet_min=%d packet_max=%d decoded_pcm=%d decoded_ms=%d decode_err=%q md5=%s", msg.MediaType, len(msg.FileData), len(uploadData), voiceMeta.sampleRate, voiceMeta.playtimeMS, msg.FileName, voiceMeta.packetCount, voiceMeta.packetBytes, voiceMeta.packetSizeMin, voiceMeta.packetSizeMax, voiceMeta.decodedPCM, voiceMeta.decodedMS, voiceMeta.decodeError, voiceMeta.payloadMD5)
	} else {
		wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "media=%s size=%d upload_size=%d name=%s", msg.MediaType, len(msg.FileData), len(uploadData), msg.FileName)
	}

	// Determine upload media type
	uploadType := UploadMediaFile
	switch msg.MediaType {
	case "image":
		uploadType = UploadMediaImage
	case "video":
		uploadType = UploadMediaVideo
	case "voice":
		uploadType = UploadMediaVoice
	}

	// Upload to CDN
	uploaded, err := g.uploadToCDN(ctx, uploadData, msg.ToUserID, uploadType)
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
	case "voice":
		voiceEncryptType := 0
		if msg.VoiceVariant == "integrity_encrypt1" {
			voiceEncryptType = 1
		}
		voiceParam := uploaded.downloadParam
		if msg.VoiceVariant == "upload_param_encrypt0" && uploaded.uploadParam != "" {
			voiceParam = uploaded.uploadParam
		}
		voiceAESKey := aesKeyForMedia
		if msg.VoiceVariant == "raw_aes_encrypt0" || msg.VoiceVariant == "silk_encode6_raw_aes_encrypt0" {
			voiceAESKey = base64.StdEncoding.EncodeToString(uploaded.aesKey)
		}
		item = messageItem{
			Type: ItemTypeVoice,
			VoiceItem: buildVoiceItem(&cdnMedia{
				EncryptQueryParam: voiceParam,
				AESKey:            voiceAESKey,
				EncryptType:       voiceEncryptType,
			}, voiceMeta, msg.VoiceVariant),
		}
		wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "voice_item variant=%s %s", firstNonEmpty(msg.VoiceVariant, "inbound_shape"), voiceItemDebugSummary(item.VoiceItem))
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
	data, err := g.apiPost(ctx, "ilink/bot/sendmessage", body, apiTimeout)
	if err != nil {
		return err
	}
	if err := validateAPIStatus("sendmessage", data); err != nil {
		return err
	}
	wl.Log("gw.SendMedia", "OUT", msg.ToUserID, "OK media=%s sendmessage_resp=%s", msg.MediaType, compactAPIResponseLog(data))
	return nil
}

func buildVoiceItem(media *cdnMedia, meta *voiceMetadata, variant ...string) *voiceItem {
	item := &voiceItem{
		Media:      media,
		EncodeType: 4,
	}
	if len(variant) > 0 && variant[0] == "silk_encode6_raw_aes_encrypt0" {
		item.EncodeType = 6
	}
	if meta != nil {
		item.SampleRate = meta.sampleRate
		item.Playtime = meta.playtimeMS
	}
	if len(variant) > 0 && variant[0] == "integrity_encrypt1" && meta != nil {
		item.Len = strconv.Itoa(meta.payloadSize)
		item.Size = strconv.Itoa(meta.payloadSize)
		item.VoiceMD5 = meta.payloadMD5
		item.MD5 = meta.payloadMD5
		item.Format = "silk"
		item.MimeType = "audio/silk"
	}
	return item
}

func voiceItemDebugSummary(item *voiceItem) string {
	if item == nil {
		return "null"
	}
	type mediaSummary struct {
		HasEncryptQueryParam bool `json:"has_encrypt_query_param"`
		EncryptQueryParamLen int  `json:"encrypt_query_param_len"`
		AESKeyLen            int  `json:"aes_key_len"`
		EncryptType          int  `json:"encrypt_type"`
	}
	type summary struct {
		EncodeType  int          `json:"encode_type"`
		Format      string       `json:"format,omitempty"`
		MimeType    string       `json:"mime_type,omitempty"`
		SampleRate  int          `json:"sample_rate"`
		Playtime    int          `json:"playtime"`
		Len         string       `json:"len"`
		Size        string       `json:"size"`
		HasVoiceMD5 bool         `json:"has_voice_md5"`
		VoiceMD5Len int          `json:"voice_md5_len"`
		VoiceMD5    string       `json:"voice_md5,omitempty"`
		HasMD5      bool         `json:"has_md5"`
		MD5Len      int          `json:"md5_len"`
		MD5         string       `json:"md5,omitempty"`
		Media       mediaSummary `json:"media"`
	}
	out := summary{
		EncodeType:  item.EncodeType,
		Format:      item.Format,
		MimeType:    item.MimeType,
		SampleRate:  item.SampleRate,
		Playtime:    item.Playtime,
		Len:         item.Len,
		Size:        item.Size,
		HasVoiceMD5: item.VoiceMD5 != "",
		VoiceMD5Len: len(item.VoiceMD5),
		VoiceMD5:    item.VoiceMD5,
		HasMD5:      item.MD5 != "",
		MD5Len:      len(item.MD5),
		MD5:         item.MD5,
	}
	if item.Media != nil {
		out.Media = mediaSummary{
			HasEncryptQueryParam: item.Media.EncryptQueryParam != "",
			EncryptQueryParamLen: len(item.Media.EncryptQueryParam),
			AESKeyLen:            len(item.Media.AESKey),
			EncryptType:          item.Media.EncryptType,
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "<marshal-error>"
	}
	return string(b)
}

type voiceMetadata struct {
	sampleRate    int
	channels      int
	bitsPerSample int
	playtimeMS    int
	packetCount   int
	packetSizeMin int
	packetSizeMax int
	packetBytes   int
	decodedPCM    int
	decodedMS     int
	decodeError   string
	dataStart     int
	dataSize      int
	payloadSize   int
	payloadMD5    string
}

func estimateVoicePlaytimeMS(data []byte) int {
	meta, ok := parseWAVVoiceMetadata(data)
	if !ok {
		return 0
	}
	return meta.playtimeMS
}

func wavVoiceMetadata(data []byte) (sampleRate, bitsPerSample, playtimeMS int, ok bool) {
	meta, ok := parseWAVVoiceMetadata(data)
	if !ok {
		return 0, 0, 0, false
	}
	return meta.sampleRate, meta.bitsPerSample, meta.playtimeMS, true
}

func wavVoicePayload(data []byte) ([]byte, voiceMetadata, bool) {
	meta, ok := parseWAVVoiceMetadata(data)
	if !ok {
		return nil, voiceMetadata{}, false
	}
	return data[meta.dataStart : meta.dataStart+meta.dataSize], meta, true
}

func voiceUploadPayload(fileName string, data []byte) ([]byte, *voiceMetadata, error) {
	if isSilkVoicePayload(data) {
		h := md5.Sum(data)
		meta := &voiceMetadata{sampleRate: weixinVoiceSampleRate, playtimeMS: estimateSilkPlaytimeMS(data), payloadSize: len(data), payloadMD5: hex.EncodeToString(h[:])}
		populateSilkDiagnostics(data, meta)
		return data, meta, nil
	}

	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".wav":
		payload, meta, ok := wavVoicePayload(data)
		if !ok {
			return nil, nil, fmt.Errorf("weixin: invalid PCM WAV voice payload")
		}
		return encodeWAVVoicePayload(payload, meta)
	case ".pcm":
		return nil, nil, fmt.Errorf("weixin: raw PCM voice payload requires WAV metadata before SILK encoding")
	case ".silk", ".slk":
		return nil, nil, fmt.Errorf("weixin: invalid SILK voice payload")
	default:
		if payload, meta, ok := wavVoicePayload(data); ok {
			return encodeWAVVoicePayload(payload, meta)
		}
		return nil, nil, fmt.Errorf("weixin: voice payload must be SILK or PCM WAV, got %s", filepath.Ext(fileName))
	}
}

func encodeWAVVoicePayload(pcm []byte, meta voiceMetadata) ([]byte, *voiceMetadata, error) {
	if meta.bitsPerSample != 16 {
		return nil, nil, fmt.Errorf("weixin: unsupported WAV bit depth %d for SILK voice", meta.bitsPerSample)
	}
	mono := pcm
	if meta.channels == 2 {
		mono = downmixStereoS16ToMono(pcm)
	} else if meta.channels != 1 {
		return nil, nil, fmt.Errorf("weixin: unsupported WAV channel count %d for SILK voice", meta.channels)
	}
	if meta.sampleRate != weixinVoiceSampleRate {
		mono = resampleS16Mono(mono, meta.sampleRate, weixinVoiceSampleRate)
	}
	mono, err := padPCMForSilk(mono, weixinVoiceSampleRate)
	if err != nil {
		return nil, nil, err
	}
	silkData, err := silk.EncodePcmBuffToSilk(mono, weixinVoiceSampleRate, weixinVoiceBitRate, true)
	if err != nil {
		return nil, nil, fmt.Errorf("weixin: SILK encode failed: %w", err)
	}
	if !isSilkVoicePayload(silkData) {
		return nil, nil, fmt.Errorf("weixin: SILK encode produced invalid payload")
	}
	outMeta := meta
	outMeta.sampleRate = weixinVoiceSampleRate
	outMeta.channels = 1
	outMeta.bitsPerSample = 0
	outMeta.payloadSize = len(silkData)
	h := md5.Sum(silkData)
	outMeta.payloadMD5 = hex.EncodeToString(h[:])
	populateSilkDiagnostics(silkData, &outMeta)
	return silkData, &outMeta, nil
}

func populateSilkDiagnostics(data []byte, meta *voiceMetadata) {
	if meta == nil {
		return
	}
	packetCount, packetBytes, packetMin, packetMax, ok := inspectSilkPackets(data)
	if ok {
		meta.packetCount = packetCount
		meta.packetBytes = packetBytes
		meta.packetSizeMin = packetMin
		meta.packetSizeMax = packetMax
	}
	if meta.sampleRate <= 0 {
		meta.decodeError = "sample_rate_missing"
		return
	}
	pcm, err := silk.DecodeSilkBuffToPcm(data, meta.sampleRate)
	if err != nil {
		meta.decodeError = err.Error()
		return
	}
	meta.decodedPCM = len(pcm)
	if meta.sampleRate > 0 {
		meta.decodedMS = len(pcm) * 1000 / (meta.sampleRate * 2)
	}
}

func inspectSilkPackets(data []byte) (count, packetBytes, minSize, maxSize int, ok bool) {
	if !isSilkVoicePayload(data) {
		return 0, 0, 0, 0, false
	}
	off := 9
	if bytes.HasPrefix(data, []byte("\x02#!SILK_V3")) {
		off = 10
	}
	minSize = int(^uint(0) >> 1)
	for off+2 <= len(data) {
		n := int(int16(binary.LittleEndian.Uint16(data[off : off+2])))
		off += 2
		if n <= 0 || off+n > len(data) {
			break
		}
		packetBytes += n
		if n < minSize {
			minSize = n
		}
		if n > maxSize {
			maxSize = n
		}
		off += n
		count++
	}
	if count == 0 {
		return 0, 0, 0, 0, false
	}
	return count, packetBytes, minSize, maxSize, off == len(data)
}

func saveDebugWeixinVoicePayload(data []byte, meta *voiceMetadata) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	dir, err := weixinVoiceDebugDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now()
	sampleRate := 0
	playtime := 0
	md5sum := ""
	if meta != nil {
		sampleRate = meta.sampleRate
		playtime = meta.playtimeMS
		md5sum = meta.payloadMD5
	}
	if md5sum == "" {
		h := md5.Sum(data)
		md5sum = hex.EncodeToString(h[:])
	}
	name := fmt.Sprintf("voice_%s_%09d_sr%d_ms%d_len%d.silk", now.Format("20060102_150405"), now.Nanosecond(), sampleRate, playtime, len(data))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	info := map[string]any{
		"file":        name,
		"format":      "silk",
		"is_silk":     isSilkVoicePayload(data),
		"sample_rate": sampleRate,
		"playtime_ms": playtime,
		"payload_len": len(data),
		"payload_md5": md5sum,
		"saved_at":    now.Format(time.RFC3339Nano),
	}
	if meta != nil {
		info["packet_count"] = meta.packetCount
		info["packet_bytes"] = meta.packetBytes
		info["packet_size_min"] = meta.packetSizeMin
		info["packet_size_max"] = meta.packetSizeMax
		info["decoded_pcm_bytes"] = meta.decodedPCM
		info["decoded_ms"] = meta.decodedMS
		info["decode_error"] = meta.decodeError
	}
	if b, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = os.WriteFile(path+".json", b, 0o600)
	}
	cleanupDebugWeixinVoicePayloads(dir)
	return path, nil
}

func saveDebugWeixinInboundVoicePayload(data []byte, item *voiceItem) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	dir, err := weixinVoiceDebugDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now()
	format, ext := detectVoicePayloadFormat(data)
	name := fmt.Sprintf("inbound_voice_%s_%09d_%s_len%d%s", now.Format("20060102_150405"), now.Nanosecond(), format, len(data), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	h := md5.Sum(data)
	info := map[string]any{
		"file":        name,
		"direction":   "inbound",
		"format":      format,
		"is_silk":     isSilkVoicePayload(data),
		"payload_len": len(data),
		"payload_md5": hex.EncodeToString(h[:]),
		"first16_hex": firstHex(data, 16),
		"saved_at":    now.Format(time.RFC3339Nano),
	}
	if item != nil {
		info["voice_item"] = json.RawMessage(voiceItemDebugSummary(item))
		info["encode_type"] = item.EncodeType
		info["sample_rate"] = item.SampleRate
		info["playtime_ms"] = item.Playtime
	}
	if b, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = os.WriteFile(path+".json", b, 0o600)
	}
	return path, nil
}

func detectVoicePayloadFormat(data []byte) (format, ext string) {
	switch {
	case isSilkVoicePayload(data):
		return "silk", ".silk"
	case bytes.HasPrefix(data, []byte("RIFF")) && len(data) >= 12 && string(data[8:12]) == "WAVE":
		return "wav", ".wav"
	case bytes.HasPrefix(data, []byte("#!AMR")):
		return "amr", ".amr"
	case len(data) >= 3 && string(data[:3]) == "ID3":
		return "mp3", ".mp3"
	case len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0:
		return "mp3", ".mp3"
	case bytes.HasPrefix(data, []byte("OggS")):
		return "ogg", ".ogg"
	default:
		return "unknown", ".bin"
	}
}

func firstHex(data []byte, n int) string {
	if n < 0 {
		n = 0
	}
	if len(data) < n {
		n = len(data)
	}
	return hex.EncodeToString(data[:n])
}

func voicePayloadDebugSummary(data []byte) string {
	format, _ := detectVoicePayloadFormat(data)
	h := md5.Sum(data)
	return fmt.Sprintf("format=%s is_silk=%v first16=%s md5=%s", format, isSilkVoicePayload(data), firstHex(data, 16), hex.EncodeToString(h[:]))
}

func weixinVoiceDebugDir() (string, error) {
	if weixinVoiceDebugDirForTest != "" {
		return weixinVoiceDebugDirForTest, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".maclaw", "temp", "weixin_voice_debug"), nil
}

func cleanupDebugWeixinVoicePayloads(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type savedVoice struct {
		name    string
		modTime time.Time
	}
	voices := make([]savedVoice, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".silk") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		voices = append(voices, savedVoice{name: entry.Name(), modTime: info.ModTime()})
	}
	if len(voices) <= weixinVoiceDebugKeep {
		return
	}
	sort.Slice(voices, func(i, j int) bool { return voices[i].modTime.Before(voices[j].modTime) })
	for _, voice := range voices[:len(voices)-weixinVoiceDebugKeep] {
		path := filepath.Join(dir, voice.name)
		_ = os.Remove(path)
		_ = os.Remove(path + ".json")
	}
}

func padPCMForSilk(data []byte, sampleRate int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("weixin: empty WAV voice payload")
	}
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("weixin: malformed 16-bit PCM payload size %d", len(data))
	}
	frameBytes := sampleRate / 1000 * 40
	if frameBytes <= 0 {
		return nil, fmt.Errorf("weixin: invalid SILK sample rate %d", sampleRate)
	}
	rem := len(data) % frameBytes
	if rem == 0 {
		return data, nil
	}
	out := make([]byte, len(data)+frameBytes-rem)
	copy(out, data)
	return out, nil
}

func isSilkVoicePayload(data []byte) bool {
	return bytes.HasPrefix(data, []byte("#!SILK_V3")) || bytes.HasPrefix(data, []byte("\x02#!SILK_V3"))
}

func estimateSilkPlaytimeMS(data []byte) int {
	if !isSilkVoicePayload(data) {
		return 0
	}
	off := 9
	if bytes.HasPrefix(data, []byte("\x02#!SILK_V3")) {
		off = 10
	}
	frames := 0
	for off+2 <= len(data) {
		n := int(int16(binary.LittleEndian.Uint16(data[off : off+2])))
		off += 2
		if n <= 0 || off+n > len(data) {
			break
		}
		off += n
		frames++
	}
	return frames * 20
}

func downmixStereoS16ToMono(data []byte) []byte {
	frames := len(data) / 4
	out := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		l := int(int16(binary.LittleEndian.Uint16(data[i*4:])))
		r := int(int16(binary.LittleEndian.Uint16(data[i*4+2:])))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16((l+r)/2)))
	}
	return out
}

func resampleS16Mono(data []byte, fromRate, toRate int) []byte {
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate || len(data) < 2 {
		return data
	}
	inSamples := len(data) / 2
	outSamples := int((int64(inSamples)*int64(toRate) + int64(fromRate) - 1) / int64(fromRate))
	if outSamples <= 0 {
		return nil
	}
	out := make([]byte, outSamples*2)
	for i := 0; i < outSamples; i++ {
		posNum := int64(i) * int64(fromRate)
		idx := int(posNum / int64(toRate))
		if idx >= inSamples-1 {
			copy(out[i*2:], data[(inSamples-1)*2:inSamples*2])
			continue
		}
		frac := float64(posNum%int64(toRate)) / float64(toRate)
		a := float64(int16(binary.LittleEndian.Uint16(data[idx*2:])))
		b := float64(int16(binary.LittleEndian.Uint16(data[(idx+1)*2:])))
		sample := int16(a + (b-a)*frac)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}

func parseWAVVoiceMetadata(data []byte) (voiceMetadata, bool) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return voiceMetadata{}, false
	}
	var sr, byteRate uint32
	var audioFormat, channels, bits uint16
	meta := voiceMetadata{}
	for off := 12; off+8 <= len(data); {
		chunkID := string(data[off : off+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		chunkStart := off + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd > len(data) {
			break
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				audioFormat = binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2])
				channels = binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4])
				sr = binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8])
				byteRate = binary.LittleEndian.Uint32(data[chunkStart+8 : chunkStart+12])
				bits = binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16])
			}
		case "data":
			meta.dataStart = chunkStart
			meta.dataSize = chunkSize
		}
		if sr > 0 && bits > 0 && meta.dataSize > 0 {
			break
		}
		off = chunkEnd
		if off%2 == 1 {
			off++
		}
	}
	if audioFormat != 1 || sr == 0 || channels == 0 || bits == 0 || meta.dataSize == 0 || meta.dataStart == 0 {
		return voiceMetadata{}, false
	}
	rate := uint64(byteRate)
	if rate == 0 && channels > 0 {
		rate = uint64(sr) * uint64(channels) * uint64(bits) / 8
	}
	if rate == 0 {
		return voiceMetadata{}, false
	}
	meta.sampleRate = int(sr)
	meta.channels = int(channels)
	meta.bitsPerSample = int(bits)
	meta.playtimeMS = int((uint64(meta.dataSize) * 1000) / rate)
	return meta, true
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
	uploadParam    string
	aesKey         []byte
	plaintextSize  int
	ciphertextSize int
}

func uploadMediaTypeName(mediaType int) string {
	switch mediaType {
	case UploadMediaImage:
		return "image"
	case UploadMediaVideo:
		return "video"
	case UploadMediaFile:
		return "file"
	case UploadMediaVoice:
		return "voice"
	default:
		return strconv.Itoa(mediaType)
	}
}

func extractEncryptedQueryParam(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if v := u.Query().Get("encrypted_query_param"); v != "" {
			return v
		}
	}
	return raw
}

func (g *Gateway) uploadToCDN(ctx context.Context, plaintext []byte, toUserID string, mediaType int) (*uploadResult, error) {
	wl := GetWxLog()
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
	wl.Log("gw.upload", "OUT", toUserID, "getuploadurl media_type=%s rawsize=%d filesize=%d raw_md5=%s filekey_len=%d aeskey_len=%d no_need_thumb=%v", uploadMediaTypeName(mediaType), rawsize, filesize, rawfilemd5, len(filekey), len(uploadReq.AESKey), uploadReq.NoNeedThumb)
	reqBody, _ := json.Marshal(uploadReq)
	data, err := g.apiPost(ctx, "ilink/bot/getuploadurl", reqBody, apiTimeout)
	if err != nil {
		wl.Log("gw.upload", "OUT", toUserID, "ERR getuploadurl media_type=%s err=%v", uploadMediaTypeName(mediaType), err)
		return nil, fmt.Errorf("getUploadUrl: %w", err)
	}
	var uploadResp getUploadURLResp
	if err := json.Unmarshal(data, &uploadResp); err != nil {
		wl.Log("gw.upload", "OUT", toUserID, "ERR getuploadurl decode media_type=%s resp=%s err=%v", uploadMediaTypeName(mediaType), compactAPIResponseLog(data), err)
		return nil, fmt.Errorf("getUploadUrl decode: %w", err)
	}
	if uploadResp.Ret != 0 {
		wl.Log("gw.upload", "OUT", toUserID, "ERR getuploadurl API media_type=%s ret=%d errmsg=%q resp=%s", uploadMediaTypeName(mediaType), uploadResp.Ret, uploadResp.ErrMsg, compactAPIResponseLog(data))
		return nil, fmt.Errorf("getUploadUrl API error: ret=%d errmsg=%q resp=%s", uploadResp.Ret, uploadResp.ErrMsg, string(data))
	}
	if uploadResp.UploadParam == "" && uploadResp.UploadFullURL == "" {
		wl.Log("gw.upload", "OUT", toUserID, "ERR getuploadurl empty upload URL media_type=%s resp=%s", uploadMediaTypeName(mediaType), compactAPIResponseLog(data))
		return nil, fmt.Errorf("getUploadUrl returned no upload_param and no upload_full_url, ret=%d errmsg=%q resp=%s",
			uploadResp.Ret, uploadResp.ErrMsg, string(data))
	}
	uploadParamForDebug := uploadResp.UploadParam
	if uploadParamForDebug == "" && uploadResp.UploadFullURL != "" {
		uploadParamForDebug = extractEncryptedQueryParam(uploadResp.UploadFullURL)
	}
	wl.Log("gw.upload", "OUT", toUserID, "OK getuploadurl media_type=%s has_full_url=%v upload_param_len=%d thumb_upload_param_len=%d resp=%s", uploadMediaTypeName(mediaType), uploadResp.UploadFullURL != "", len(uploadParamForDebug), len(uploadResp.ThumbUploadParam), compactAPIResponseLog(data))

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
			wl.Log("gw.upload", "OUT", toUserID, "ERR cdn_upload media_type=%s attempt=%d ciphertext_size=%d err=%v", uploadMediaTypeName(mediaType), attempt, len(ciphertext), err)
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
			wl.Log("gw.upload", "OUT", toUserID, "ERR cdn_upload client media_type=%s attempt=%d status=%d download_param_len=%d", uploadMediaTypeName(mediaType), attempt, respStatus, len(respDownloadParam))
			return nil, fmt.Errorf("CDN upload client error %d", respStatus)
		}
		if respStatus != 200 {
			lastErr = fmt.Errorf("CDN upload server error %d", respStatus)
			wl.Log("gw.upload", "OUT", toUserID, "ERR cdn_upload server media_type=%s attempt=%d status=%d download_param_len=%d", uploadMediaTypeName(mediaType), attempt, respStatus, len(respDownloadParam))
			if attempt < cdnUploadMaxRetries {
				log.Printf("[weixin/gw] CDN upload attempt %d: %v", attempt, lastErr)
				continue
			}
			break
		}

		if respDownloadParam == "" {
			lastErr = fmt.Errorf("CDN response missing X-Encrypted-Param header")
			wl.Log("gw.upload", "OUT", toUserID, "ERR cdn_upload missing download param media_type=%s attempt=%d status=%d", uploadMediaTypeName(mediaType), attempt, respStatus)
			if attempt < cdnUploadMaxRetries {
				continue
			}
			break
		}
		downloadParam = respDownloadParam
		lastErr = nil
		wl.Log("gw.upload", "OUT", toUserID, "OK cdn_upload media_type=%s attempt=%d plaintext_size=%d ciphertext_size=%d download_param_len=%d", uploadMediaTypeName(mediaType), attempt, rawsize, len(ciphertext), len(downloadParam))
		break
	}
	if lastErr != nil {
		return nil, fmt.Errorf("CDN upload failed: %w", lastErr)
	}

	return &uploadResult{
		downloadParam:  downloadParam,
		uploadParam:    uploadParamForDebug,
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
var qrPollTimeout = 35 * time.Second

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
		return "", "", &qrLoginServerError{Op: "get_bot_qrcode", Message: fmt.Sprintf("returned %d: %s", resp.StatusCode, string(body))}
	}
	var qr qrCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return "", "", fmt.Errorf("decode QR response: %w", err)
	}
	if qrCodeResponseHasServerError(qr) {
		return "", "", &qrLoginServerError{Op: "get_bot_qrcode", Message: firstNonEmpty(qr.ErrMsg, qr.Message, "server returned an error")}
	}
	qrcodeURL = strings.TrimSpace(qr.QRCodeImgContent)
	qrcodeToken = strings.TrimSpace(qr.QRCode)
	if qrcodeURL == "" || qrcodeToken == "" {
		return "", "", &qrLoginServerError{Op: "get_bot_qrcode", Message: "response is incomplete"}
	}
	return qrcodeURL, qrcodeToken, nil
}

func qrCodeResponseHasServerError(qr qrCodeResponse) bool {
	if qr.Ret != nil && *qr.Ret != 0 {
		return true
	}
	if qr.ErrCode != nil && *qr.ErrCode != 0 {
		return true
	}
	return false
}

// PollQRStatus polls the QR code login status once. Returns the status response.
func PollQRStatus(ctx context.Context, baseURL, qrcodeToken string) (*QRLoginResult, QRLoginStatus, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	qrcodeToken = strings.TrimSpace(qrcodeToken)
	if qrcodeToken == "" {
		return nil, QRLoginStatusUnknown, ErrQRCodeTokenEmpty
	}
	base := strings.TrimRight(baseURL, "/")
	u := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", base, url.QueryEscape(qrcodeToken))

	pollCtx, cancel := context.WithTimeout(ctx, qrPollTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, "GET", u, nil)
	if err != nil {
		return nil, QRLoginStatusUnknown, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")

	resp, err := qrHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil || pollCtx.Err() != nil {
			return &QRLoginResult{Message: "timeout"}, QRLoginStatusWait, nil
		}
		return nil, QRLoginStatusUnknown, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, QRLoginStatusUnknown, err
	}
	if resp.StatusCode != 200 {
		return nil, QRLoginStatusUnknown, &qrLoginServerError{Op: "get_qrcode_status", Message: fmt.Sprintf("returned %d: %s", resp.StatusCode, string(data[:min(len(data), 512)]))}
	}

	status, err := decodeQRStatusResponse(data)
	if err != nil {
		return nil, QRLoginStatusUnknown, fmt.Errorf("decode status: %w", err)
	}
	if qrStatusHasServerError(status) {
		return nil, QRLoginStatusUnknown, &qrLoginServerError{Op: "get_qrcode_status", Message: firstNonEmpty(status.Message, "server returned an error")}
	}
	normalizedStatus := NormalizeQRLoginStatus(status.Status)
	if strings.TrimSpace(status.Message) == "" {
		status.Message = qrStatusDefaultMessage(normalizedStatus)
	}
	return qrLoginResultFromStatus(status, normalizedStatus)
}

func decodeQRStatusResponse(data []byte) (qrStatusResponse, error) {
	var status qrStatusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		return status, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return status, nil
	}
	mergeQRStatusFields(&status, raw)
	mergeNestedQRStatusFields(&status, raw, 0)
	if status.Status == "" && status.ILinkBotID != "" {
		status.Status = QRLoginStatusConfirmed
	}
	return status, nil
}

func qrStatusHasServerError(status qrStatusResponse) bool {
	if status.Ret != nil && *status.Ret != 0 {
		return true
	}
	if status.ErrCode != nil && *status.ErrCode != 0 {
		return true
	}
	return false
}

func mergeNestedQRStatusFields(status *qrStatusResponse, raw map[string]json.RawMessage, depth int) {
	if depth >= 4 {
		return
	}
	for _, value := range raw {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(value, &nested); err == nil && len(nested) > 0 {
			mergeQRStatusFields(status, nested)
			mergeNestedQRStatusFields(status, nested, depth+1)
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			continue
		}
		for _, item := range items {
			var itemMap map[string]json.RawMessage
			if err := json.Unmarshal(item, &itemMap); err == nil && len(itemMap) > 0 {
				mergeQRStatusFields(status, itemMap)
				mergeNestedQRStatusFields(status, itemMap, depth+1)
			}
		}
	}
}

func qrLoginResultFromStatus(status qrStatusResponse, normalizedStatus QRLoginStatus) (*QRLoginResult, QRLoginStatus, error) {
	switch normalizedStatus {
	case QRLoginStatusConfirmed:
		if status.ILinkBotID == "" {
			return &QRLoginResult{
				Connected: false,
				Message:   firstNonEmpty(status.Message, "login failed: server did not return ilink_bot_id"),
			}, QRLoginStatusConfirmed, nil
		}
		if status.BotToken == "" {
			return &QRLoginResult{
				Connected: false,
				AccountID: status.ILinkBotID,
				BaseURL:   status.BaseURL,
				UserID:    status.ILinkUserID,
				Message:   firstNonEmpty(status.Message, "login failed: server did not return bot_token"),
			}, QRLoginStatusConfirmed, nil
		}
		return &QRLoginResult{
			Connected: true,
			BotToken:  status.BotToken,
			AccountID: status.ILinkBotID,
			BaseURL:   status.BaseURL,
			UserID:    status.ILinkUserID,
			Message:   firstNonEmpty(status.Message, "WeChat connected"),
		}, QRLoginStatusConfirmed, nil
	case QRLoginStatusScanned:
		return &QRLoginResult{Message: status.Message}, QRLoginStatusScanned, nil
	case QRLoginStatusExpired:
		return &QRLoginResult{Message: status.Message}, QRLoginStatusExpired, nil
	default:
		return &QRLoginResult{Message: status.Message}, QRLoginStatusWait, nil
	}
}

func mergeQRStatusFields(status *qrStatusResponse, raw map[string]json.RawMessage) {
	if status.Status == "" {
		status.Status = QRLoginStatus(rawJSONFirstString(raw, "status", "state", "qr_status", "qrStatus"))
	}
	if status.BotToken == "" {
		status.BotToken = rawJSONFirstString(raw, "bot_token", "botToken", "ilink_bot_token", "ilinkBotToken", "token", "access_token", "accessToken")
	}
	if status.ILinkBotID == "" {
		status.ILinkBotID = rawJSONFirstString(raw, "ilink_bot_id", "ilinkBotId", "bot_id", "botId", "account_id", "accountId")
	}
	if status.BaseURL == "" {
		status.BaseURL = rawJSONFirstString(raw, "baseurl", "base_url", "baseUrl")
	}
	if status.ILinkUserID == "" {
		status.ILinkUserID = rawJSONFirstString(raw, "ilink_user_id", "ilinkUserId", "user_id", "userId")
	}
	if status.Message == "" {
		status.Message = rawJSONFirstString(raw, "message", "msg", "errmsg", "error")
	}
}

func rawJSONFirstString(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		value := raw[key]
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal(value, &s); err == nil && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		var n json.Number
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&n); err == nil && strings.TrimSpace(n.String()) != "" {
			return strings.TrimSpace(n.String())
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func qrStatusDefaultMessage(status QRLoginStatus) string {
	switch status {
	case QRLoginStatusScanned:
		return "scanned, waiting for phone confirmation"
	case QRLoginStatusConfirmed:
		return "WeChat connected"
	case QRLoginStatusExpired:
		return "QR code expired"
	default:
		return "waiting for scan"
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
		case QRLoginStatusConfirmed:
			return result, nil
		case QRLoginStatusExpired:
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
