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
	ChatID    int64
	Text      string
	Username  string
	Timestamp time.Time
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
}

// NewGateway creates a new Telegram Bot gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	return &Gateway{
		config:  config,
		handler: handler,
		client:  &http.Client{Timeout: time.Duration(pollTimeout+10) * time.Second},
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
	defer g.mu.Unlock()
	if !g.running {
		return nil
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.wg.Wait()
	g.running = false
	g.cancel = nil
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
			if u.Message != nil && u.Message.Text != "" {
				g.handler(IncomingMessage{
					ChatID:    u.Message.Chat.ID,
					Text:      u.Message.Text,
					Username:  u.Message.From.Username,
					Timestamp: time.Unix(int64(u.Message.Date), 0),
				})
			}
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
}

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
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
