package messenger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxWebhookBody = 1 << 20

// MessageHandler receives one inbound turn. It may run after the HTTP 200 is sent.
type MessageHandler func(IncomingMessage)

// StatusCallback reports connected, disconnected, or error.
type StatusCallback func(status string)

// Gateway serves the Messenger webhook and posts replies to the Graph Send API.
type Gateway struct {
	handler  MessageHandler
	onStatus StatusCallback
	client   *http.Client
	apiBase  string

	mu      sync.Mutex
	startMu sync.Mutex
	account Account
	server  *http.Server
	running bool
	wg      sync.WaitGroup
	errText string

	// userLocks stripes serialize one PSID without retaining a mutex per sender.
	userLocks [userLockStripes]sync.Mutex
	handlerWg sync.WaitGroup

	seenMu sync.Mutex
	seen   map[string]time.Time

	// quotes remembers text this process already handled, keyed by mid.
	// A reply_to webhook does not include the quoted body.
	quoteMu sync.Mutex
	quotes  map[string]quoteText
}

const (
	maxQuoteEntries = 1024
	maxQuoteRunes   = 500
)

type quoteText struct {
	text string
	at   time.Time
}

// NewGateway builds a gateway. The account is normalized on each use.
func NewGateway(account Account, handler MessageHandler) *Gateway {
	return &Gateway{
		account: account.Normalized(),
		handler: handler,
		client: &http.Client{
			Timeout: 25 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// SetStatusCallback receives connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.onStatus = cb
	g.mu.Unlock()
}

// SetAPIBase points Send at a test server. Empty restores graph.facebook.com.
func (g *Gateway) SetAPIBase(base string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.apiBase = strings.TrimRight(strings.TrimSpace(base), "/")
	g.mu.Unlock()
}

// UpdateAccount swaps credentials and policy without rebinding the listener.
func (g *Gateway) UpdateAccount(account Account) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.account = account.Normalized()
	g.mu.Unlock()
}

func (g *Gateway) accountSnapshot() Account {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.account
}

// Running reports whether the webhook listener is still accepting connections.
func (g *Gateway) Running() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// LastError is the latest listen or serve failure.
func (g *Gateway) LastError() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.errText
}

// Start listens on the account host and port.
func (g *Gateway) Start(ctx context.Context) error {
	if g == nil {
		return errors.New("facebook messenger gateway unavailable")
	}
	account := g.accountSnapshot()
	if !account.CanRun() {
		return errors.New("facebook messenger is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// startMu keeps Stop from returning while Listen is still in progress,
	// without holding mu across the bind. mu stays free for webhook reads.
	g.startMu.Lock()
	defer g.startMu.Unlock()
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()
	addr := net.JoinHostPort(account.ListenHost, strconv.Itoa(account.ListenPort))
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return g.serve(ln)
}

func (g *Gateway) serve(ln net.Listener) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	server := &http.Server{
		Handler:           g,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	g.server = server
	g.running = true
	g.errText = ""
	g.wg.Add(1)
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		err := server.Serve(ln)
		g.mu.Lock()
		// Stop nils g.server first. Only the exit of the live server changes state.
		current := g.server == server
		if current {
			g.server = nil
			g.running = false
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				g.errText = err.Error()
			}
		}
		g.mu.Unlock()
		if current && err != nil && !errors.Is(err, http.ErrServerClosed) {
			g.emit("error")
		}
	}()
	g.mu.Lock()
	running := g.running
	errText := g.errText
	g.mu.Unlock()
	if !running {
		if errText == "" {
			errText = "facebook messenger webhook stopped"
		}
		return errors.New(errText)
	}
	// The connected callback runs before Start returns, still under startMu.
	// Callbacks must not call Start or Stop.
	g.emit("connected")
	return nil
}

// Stop closes the listener. In-flight agent turns are not waited on.
func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
	g.startMu.Lock()
	defer g.startMu.Unlock()
	g.mu.Lock()
	server := g.server
	g.server = nil
	g.running = false
	g.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	g.wg.Wait()
	g.emit("disconnected")
	return nil
}

func (g *Gateway) emit(status string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	cb := g.onStatus
	g.mu.Unlock()
	if cb != nil {
		cb(status)
	}
}

// ServeHTTP implements the Meta webhook: GET verifies, POST accepts events.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	account := g.accountSnapshot()
	if !webhookPathMatch(r.URL.Path, account.WebhookPath) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodHead:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		g.handleVerify(w, r, account)
	case http.MethodPost:
		g.handleEvent(w, r, account)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method Not Allowed"})
	}
}

func (g *Gateway) handleVerify(w http.ResponseWriter, r *http.Request, account Account) {
	if !account.CanRun() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Facebook Messenger is not configured"})
		return
	}
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	token := query.Get("hub.verify_token")
	challenge := query.Get("hub.challenge")
	if len(challenge) > 4096 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid verify token"})
		return
	}
	if mode == "subscribe" && verifyTokenMatch(token, account.VerifyToken) && challenge != "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, challenge)
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid verify token"})
}

func (g *Gateway) handleEvent(w http.ResponseWriter, r *http.Request, account Account) {
	if !account.CanRun() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Facebook Messenger is not configured"})
		return
	}
	body, err := readLimitedBody(r.Body, maxWebhookBody)
	if err != nil {
		status := http.StatusBadRequest
		message := "Invalid body"
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
			message = "Payload too large"
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	if !VerifySignature(body, account.AppSecret, headerValue(r.Header, "X-Hub-Signature-256")) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid X-Hub-Signature-256"})
		return
	}
	object, messages, err := ParseIncoming(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid webhook payload"})
		return
	}
	if object != "page" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Unsupported webhook object"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "EVENT_RECEIVED")
	allowed := make([]IncomingMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.PageID != account.PageID || !account.SenderAllowed(msg.PSID) {
			continue
		}
		allowed = append(allowed, msg)
	}
	g.dispatchAll(allowed)
}

func verifyTokenMatch(got, want string) bool {
	if want == "" || len(got) > 512 || len(want) > 512 {
		return false
	}
	digest := func(value string) []byte {
		mac := hmac.New(sha256.New, []byte("maclaw-messenger-verify"))
		_, _ = mac.Write([]byte(value))
		return mac.Sum(nil)
	}
	return hmac.Equal(digest(got), digest(want))
}

// dispatchAll runs one webhook batch after the ACK. Events stay in arrival
// order, and the same PSID is serialized with any other in-flight batch.
func (g *Gateway) dispatchAll(messages []IncomingMessage) {
	if g == nil || g.handler == nil || len(messages) == 0 {
		return
	}
	g.handlerWg.Add(1)
	go func() {
		defer g.handlerWg.Done()
		for _, msg := range messages {
			if !g.rememberMessage(msg) {
				continue
			}
			// Copy the parent before this mid is inserted. A full cache drops
			// the oldest entry, and that entry may be the message being quoted.
			if msg.ReplyToMID != "" {
				msg.ReplyQuote = g.QuotedText(msg.ReplyToMID)
			}
			g.rememberQuote(msg.MessageID, msg.Text)
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						log.Printf("[messenger] inbound handler panic: %v", recovered)
						// The turn did not finish. A Meta retry must be able to run it.
						g.forgetMessage(msg)
					}
				}()
				lock := g.userLock(msg.PSID)
				lock.Lock()
				defer lock.Unlock()
				g.handler(msg)
			}()
		}
	}()
}

// WaitHandlers blocks until inbound turns started so far have returned.
// Stop does not call this: a long agent turn must not hold the listener.
func (g *Gateway) WaitHandlers() {
	if g == nil {
		return
	}
	g.handlerWg.Wait()
}

const seenMessageTTL = 10 * time.Minute

// rememberMessage reports whether this event should start a turn.
// Meta retries a delivery when the ACK is late, and the same mid must not
// run the agent twice. Events without an id are keyed by sender, time, and text.
func messageDedupeKey(msg IncomingMessage) string {
	key := strings.TrimSpace(msg.MessageID)
	if key != "" {
		// message_edit reuses the original mid. The edit count is part of the
		// delivery identity, or a correction never reaches the agent.
		if msg.EditNum > 0 {
			return key + "#edit:" + strconv.Itoa(msg.EditNum)
		}
		return key
	}
	if msg.Text == "" {
		return ""
	}
	return msg.PSID + "\n" + strconv.FormatInt(msg.Timestamp, 10) + "\n" + msg.Text
}

func (g *Gateway) rememberMessage(msg IncomingMessage) bool {
	if g == nil {
		return false
	}
	key := messageDedupeKey(msg)
	if key == "" {
		return true
	}
	now := time.Now()
	g.seenMu.Lock()
	defer g.seenMu.Unlock()
	if g.seen == nil {
		g.seen = make(map[string]time.Time)
	}
	for id, at := range g.seen {
		if now.Sub(at) > seenMessageTTL {
			delete(g.seen, id)
		}
	}
	if _, ok := g.seen[key]; ok {
		return false
	}
	// Drop the oldest id so a full map cannot forget every recent delivery.
	if len(g.seen) >= 1024 {
		oldestID := ""
		var oldest time.Time
		for id, at := range g.seen {
			if oldestID == "" || at.Before(oldest) {
				oldestID = id
				oldest = at
			}
		}
		delete(g.seen, oldestID)
	}
	g.seen[key] = now
	return true
}

// QuotedText returns text previously stored for mid. A miss returns empty.
func (g *Gateway) QuotedText(mid string) string {
	if g == nil {
		return ""
	}
	mid = strings.TrimSpace(mid)
	if mid == "" {
		return ""
	}
	g.quoteMu.Lock()
	defer g.quoteMu.Unlock()
	rec, ok := g.quotes[mid]
	if !ok {
		return ""
	}
	return rec.text
}

func (g *Gateway) rememberQuote(mid, text string) {
	if g == nil {
		return
	}
	mid = strings.TrimSpace(mid)
	text = truncateQuote(strings.TrimSpace(text), maxQuoteRunes)
	if mid == "" || text == "" {
		return
	}
	g.quoteMu.Lock()
	defer g.quoteMu.Unlock()
	if g.quotes == nil {
		g.quotes = make(map[string]quoteText, maxQuoteEntries)
	}
	if _, exists := g.quotes[mid]; !exists && len(g.quotes) >= maxQuoteEntries {
		oldestID := ""
		var oldest time.Time
		for id, rec := range g.quotes {
			if oldestID == "" || rec.at.Before(oldest) {
				oldestID = id
				oldest = rec.at
			}
		}
		delete(g.quotes, oldestID)
	}
	g.quotes[mid] = quoteText{text: text, at: time.Now()}
}

func truncateQuote(text string, limit int) string {
	if limit <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func (g *Gateway) forgetMessage(msg IncomingMessage) {
	if g == nil {
		return
	}
	key := messageDedupeKey(msg)
	if key == "" {
		return
	}
	g.seenMu.Lock()
	delete(g.seen, key)
	g.seenMu.Unlock()
}

const userLockStripes = 64

func (g *Gateway) userLock(psid string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(psid))
	return &g.userLocks[h.Sum32()%userLockStripes]
}

func webhookPathMatch(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if want == "" {
		want = DefaultWebhookPath
	}
	got = strings.TrimRight(got, "/")
	want = strings.TrimRight(want, "/")
	if got == "" {
		got = "/"
	}
	if want == "" {
		want = "/"
	}
	return got == want
}

var errBodyTooLarge = errors.New("payload too large")

func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, errors.New("empty body")
	}
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errBodyTooLarge
	}
	return raw, nil
}

func headerValue(header http.Header, name string) string {
	if header == nil {
		return ""
	}
	return header.Get(name)
}

func writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
