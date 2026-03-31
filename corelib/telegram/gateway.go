// Package telegram implements a client-side Telegram Bot gateway using
// long-polling (getUpdates). It receives messages from Telegram users and
// forwards them to the Hub via the existing machine WebSocket connection.
// Outbound replies are sent via the Telegram Bot API (sendMessage, etc.).
//
// This runs entirely on the client machine — the Hub never touches bot tokens.
package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	apiBase            = "https://api.telegram.org/bot"
	pollTimeout        = 30 // seconds, Telegram long-poll timeout
	reconnectBaseDelay = 3 * time.Second
	reconnectMaxDelay  = 60 * time.Second
)

// Config holds the Telegram Bot token.
type Config struct {
	BotToken string
}

// IncomingMessage is a Telegram message received via long-polling.
type IncomingMessage struct {
	ChatID       int64
	Text         string
	Username     string
	LanguageCode string // Telegram user's language_code (e.g. "en", "zh-hans")
	Timestamp    time.Time
	// Media fields (populated when the message contains a document/photo).
	MediaType string // "image", "file", "voice", "video", or ""
	MediaData []byte // raw file bytes (downloaded from Telegram)
	MediaName string // original file name (if available)
	MimeType  string // MIME type from Telegram
}

// OutgoingText is a text message to send to a Telegram user.
type OutgoingText struct {
	ChatID int64
	Text   string
}

// OutgoingMedia is a media message to send to a Telegram user.
type OutgoingMedia struct {
	ChatID   int64
	FileType string // "photo", "document"
	FileData string // base64-encoded
	FileName string
	MimeType string
	Caption  string
}

// MessageHandler is called when a message arrives from Telegram.
type MessageHandler func(msg IncomingMessage)

// StatusCallback is called when the gateway connection status changes.
type StatusCallback func(status string)

// Gateway manages the Telegram Bot long-polling loop on the client side.
type Gateway struct {
	config   Config
	handler  MessageHandler
	onStatus StatusCallback
	client   *http.Client

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool

	// Per-user message processing locks — ensures messages from the same
	// user are handled sequentially while different users run concurrently.
	userLocks   map[string]*sync.Mutex
	userLocksMu sync.Mutex

	// handlerWg tracks in-flight handler goroutines so Stop() can wait
	// for them to finish before returning.
	handlerWg sync.WaitGroup
}

// NewGateway creates a new Telegram Bot gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	return &Gateway{
		config:    config,
		handler:   handler,
		client:    &http.Client{Timeout: time.Duration(pollTimeout+10) * time.Second},
		userLocks: make(map[string]*sync.Mutex),
	}
}

// SetStatusCallback sets a callback for connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.onStatus = cb
}

// Start launches the long-polling loop in the background.
func (g *Gateway) Start(ctx context.Context) error {
	if g.config.BotToken == "" {
		return fmt.Errorf("telegram: BotToken is required")
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
	log.Printf("[telegram/gw] started")
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
	log.Printf("[telegram/gw] stopped")
	g.emitStatus("disconnected")
	return nil
}

// IsRunning returns whether the gateway is currently running.
func (g *Gateway) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

func (g *Gateway) emitStatus(status string) {
	if g.onStatus != nil {
		g.onStatus(status)
	}
}

// userLock returns a per-user mutex, creating one if it doesn't exist yet.
func (g *Gateway) userLock(userKey string) *sync.Mutex {
	g.userLocksMu.Lock()
	defer g.userLocksMu.Unlock()
	ul, ok := g.userLocks[userKey]
	if !ok {
		ul = &sync.Mutex{}
		g.userLocks[userKey] = ul
	}
	return ul
}

// ---------------------------------------------------------------------------
// Long-polling loop
// ---------------------------------------------------------------------------

func (g *Gateway) pollLoop(ctx context.Context) {
	defer g.wg.Done()
	if err := g.verifyToken(ctx); err != nil {
		log.Printf("[telegram/gw] token verification failed: %v", err)
		g.emitStatus("error")
		return
	}
	g.emitStatus("connected")

	var offset int64
	delay := reconnectBaseDelay
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := g.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[telegram/gw] getUpdates error: %v", err)
			g.emitStatus("reconnecting")
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = delay * 2
			if delay > reconnectMaxDelay {
				delay = reconnectMaxDelay
			}
			continue
		}
		delay = reconnectBaseDelay
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil {
				continue
			}
			m := u.Message
			text := m.Text
			if text == "" && m.Caption != "" {
				text = m.Caption
			}

			// Determine media file_id and type
			var fileID, mediaType, fileName, mimeType string
			switch {
			case m.Document != nil:
				fileID = m.Document.FileID
				mediaType = "file"
				fileName = m.Document.FileName
				mimeType = m.Document.MimeType
			case len(m.Photo) > 0:
				// Pick the largest photo
				best := m.Photo[len(m.Photo)-1]
				fileID = best.FileID
				mediaType = "image"
				mimeType = "image/jpeg"
			case m.Voice != nil:
				fileID = m.Voice.FileID
				mediaType = "voice"
				mimeType = m.Voice.MimeType
			case m.Video != nil:
				fileID = m.Video.FileID
				mediaType = "video"
				fileName = m.Video.FileName
				mimeType = m.Video.MimeType
			}

			// Skip messages with no text and no media
			if text == "" && fileID == "" {
				continue
			}

			incoming := IncomingMessage{
				ChatID:       m.Chat.ID,
				Text:         text,
				Username:     m.From.Username,
				LanguageCode: m.From.LanguageCode,
				Timestamp:    time.Unix(int64(m.Date), 0),
			}

			// Download media if present
			if fileID != "" {
				data, err := g.downloadFile(ctx, fileID)
				if err != nil {
					log.Printf("[telegram/gw] download file %s failed: %v", fileID, err)
				} else {
					incoming.MediaType = mediaType
					incoming.MediaData = data
					incoming.MediaName = fileName
					incoming.MimeType = mimeType
				}
			}

			userKey := strconv.FormatInt(m.Chat.ID, 10)
			ul := g.userLock(userKey)
			g.handlerWg.Add(1)
			go func() {
				defer g.handlerWg.Done()
				ul.Lock()
				defer ul.Unlock()
				g.handler(incoming)
			}()
		}
	}
}

func (g *Gateway) verifyToken(ctx context.Context) error {
	url := apiBase + g.config.BotToken + "/getMe"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("getMe returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message,omitempty"`
}

type tgMessage struct {
	MessageID int    `json:"message_id"`
	From      tgUser `json:"from"`
	Chat      tgChat `json:"chat"`
	Date      int    `json:"date"`
	Text      string `json:"text"`
	Caption   string `json:"caption,omitempty"`
	// Media fields
	Document *tgDocument  `json:"document,omitempty"`
	Photo    []tgPhotoSize `json:"photo,omitempty"`
	Voice    *tgVoice     `json:"voice,omitempty"`
	Video    *tgVideo     `json:"video,omitempty"`
}

type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgPhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int64  `json:"file_size"`
}

type tgVoice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgVideo struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

func (g *Gateway) getUpdates(ctx context.Context, offset int64) ([]tgUpdate, error) {
	url := fmt.Sprintf("%s%s/getUpdates?timeout=%d&allowed_updates=[\"message\"]",
		apiBase, g.config.BotToken, pollTimeout)
	if offset > 0 {
		url += "&offset=" + strconv.FormatInt(offset, 10)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
		return nil, fmt.Errorf("getUpdates returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates: ok=false")
	}
	return result.Result, nil
}

// ---------------------------------------------------------------------------
// Outbound messaging
// ---------------------------------------------------------------------------

// SendText sends a text message to a Telegram chat.
// Telegram has a 4096 character limit per message; longer texts are split.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	const maxLen = 4096
	text := msg.Text
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxLen {
			chunk = text[:maxLen]
		}
		text = text[len(chunk):]
		if err := g.sendTextChunk(ctx, msg.ChatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gateway) sendTextChunk(ctx context.Context, chatID int64, text string) error {
	url := apiBase + g.config.BotToken + "/sendMessage"
	body, _ := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("sendMessage returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// SendMedia sends a photo or document to a Telegram chat via multipart upload.
func (g *Gateway) SendMedia(ctx context.Context, msg OutgoingMedia) error {
	if msg.FileData == "" {
		return fmt.Errorf("telegram: empty file data")
	}
	method := "sendDocument"
	fieldName := "document"
	if msg.FileType == "photo" {
		method = "sendPhoto"
		fieldName = "photo"
	}
	url := apiBase + g.config.BotToken + "/" + method

	decoded, err := base64.StdEncoding.DecodeString(msg.FileData)
	if err != nil {
		// Try RawStdEncoding (no padding)
		decoded, err = base64.RawStdEncoding.DecodeString(msg.FileData)
		if err != nil {
			return fmt.Errorf("telegram: base64 decode failed: %w", err)
		}
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("chat_id", strconv.FormatInt(msg.ChatID, 10))
	if msg.Caption != "" {
		_ = writer.WriteField("caption", msg.Caption)
	}
	fileName := msg.FileName
	if fileName == "" {
		fileName = "file"
	}
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		return err
	}
	if _, err := part.Write(decoded); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s returned %d: %s", method, resp.StatusCode, respBody)
	}
	return nil
}

// downloadFile downloads a file from Telegram servers using the getFile API.
func (g *Gateway) downloadFile(ctx context.Context, fileID string) ([]byte, error) {
	// Step 1: getFile to obtain file_path
	getURL := fmt.Sprintf("%s%s/getFile?file_id=%s", apiBase, g.config.BotToken, url.QueryEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
			FileSize int64  `json:"file_size"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("telegram getFile parse: %w", err)
	}
	if !result.OK || result.Result.FilePath == "" {
		return nil, fmt.Errorf("telegram getFile failed: %s", body)
	}
	if result.Result.FileSize > 20*1024*1024 {
		return nil, fmt.Errorf("telegram file too large: %d bytes", result.Result.FileSize)
	}

	// Step 2: download the file content
	dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", g.config.BotToken, result.Result.FilePath)
	dlReq, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		return nil, err
	}
	dlResp, err := g.client.Do(dlReq)
	if err != nil {
		return nil, err
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != 200 {
		return nil, fmt.Errorf("telegram file download returned %d", dlResp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(dlResp.Body, 20*1024*1024+1))
}
