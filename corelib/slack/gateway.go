package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// channelListTTL keeps conversations.list off the per-poll path. That
	// method is a low-rate Slack API; calling it every few seconds gets the
	// bot throttled and then every channel goes quiet.
	channelListTTL = 8 * time.Second
	// historyBatch is how many conversations are read each poll. Reading
	// every DM every few seconds exceeds Slack's history limit once a
	// workspace has more than a couple of conversations.
	historyBatch = 3
	// initialLookback is only clock skew. The cursor is the moment polling
	// starts, so a message sent after connect is delivered and older history
	// is not replayed when the app restarts.
	initialLookback = 2 * time.Second
	// slackTextLimit is under Slack's 40k character cap so a long reply is
	// posted in order instead of being rejected whole.
	slackTextLimit = 35000
	// fatalAuthWait is how long to sit idle after Slack rejects the token.
	// Retrying every poll cannot fix a revoked token and only burns the limit.
	fatalAuthWait = 2 * time.Minute
	// threadScanEvery is how often a conversation is checked for replies
	// posted under an older message. Those do not appear in the normal
	// history window.
	threadScanEvery = 20 * time.Second
)

// IncomingMessage is one new Slack DM or group DM addressed to the bot.
type IncomingMessage struct {
	ChannelID string
	UserID    string
	Text      string
	TS        string
	// ThreadTS is the parent message when this arrived as a thread reply.
	// Replies stay in that thread.
	ThreadTS string
}

// MessageHandler receives a message. It may run longer than one poll.
type MessageHandler func(msg IncomingMessage)

// StatusCallback reports connecting, connected, reconnecting, or error.
type StatusCallback func(status string)

// Gateway polls Slack for new direct messages and posts replies.
// Socket Mode needs an extra app token that browser login does not return,
// so a connected workspace is read with the bot token from OAuth.
type Gateway struct {
	token           string
	botUserID       string
	handler         MessageHandler
	onStatus        StatusCallback
	client          *http.Client
	apiBase         string
	pollEvery       time.Duration
	threadScanEvery time.Duration

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool

	userLocks   map[string]*sync.Mutex
	userLocksMu sync.Mutex
	handlerWg   sync.WaitGroup

	// cursors are per conversation. One global cursor drops a message
	// when a newer conversation advances it past a channel whose read failed.
	// startedAt is fixed when polling begins. A channel discovered later
	// still uses it, so the DM that just arrived is not treated as history.
	startedAt     string
	cursors       map[string]string
	replyCounts   map[string]int
	threadScanAt  map[string]time.Time
	channelIDs    []string
	channelsAt    time.Time
	channelCursor int
	lastErrByChan map[string]string
	errText       string
}

// rateLimitError is Slack's 429 or {"error":"ratelimited"}.
type rateLimitError struct {
	wait time.Duration
}

func (e *rateLimitError) Error() string {
	if e == nil {
		return "ratelimited"
	}
	return "ratelimited"
}

// NewGateway creates a gateway. botUserID may be empty; auth.test fills it.
func NewGateway(token, botUserID string, handler MessageHandler) *Gateway {
	return &Gateway{
		token:           strings.TrimSpace(token),
		botUserID:       strings.TrimSpace(botUserID),
		handler:         handler,
		client:          &http.Client{Timeout: 25 * time.Second},
		apiBase:         "https://slack.com/api",
		pollEvery:       3 * time.Second,
		threadScanEvery: threadScanEvery,
		userLocks:       make(map[string]*sync.Mutex),
	}
}

// SetStatusCallback receives connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	if g == nil {
		return
	}
	g.onStatus = cb
}

// Start verifies the bot token and begins polling.
func (g *Gateway) Start(ctx context.Context) error {
	if g == nil || g.token == "" {
		return fmt.Errorf("slack: bot token is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	if g.startedAt == "" {
		g.startedAt = slackTSNowOffset(-initialLookback)
	}
	userID, err := g.authTest(ctx)
	if err != nil {
		g.mu.Unlock()
		return err
	}
	if userID != "" {
		g.botUserID = userID
	}
	pollCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.wg.Add(1)
	go g.pollLoop(pollCtx)
	g.mu.Unlock()
	g.emit("connecting")
	return nil
}

// Stop ends polling and waits for in-flight replies.
func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
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
	g.wg.Wait()
	g.handlerWg.Wait()
	g.emit("disconnected")
	return nil
}

// SendText posts a message into a Slack conversation.
func (g *Gateway) SendText(ctx context.Context, channelID, text string) error {
	if g == nil || g.token == "" {
		return fmt.Errorf("slack gateway unavailable")
	}
	channelID = strings.TrimSpace(channelID)
	text = strings.TrimSpace(text)
	if channelID == "" || text == "" {
		return fmt.Errorf("slack message is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return g.SendThreadText(ctx, channelID, "", text)
}

// SendThreadText posts into a thread when threadTS is set, and into the
// conversation itself when it is empty.
func (g *Gateway) SendThreadText(ctx context.Context, channelID, threadTS, text string) error {
	if g == nil || g.token == "" {
		return fmt.Errorf("slack gateway unavailable")
	}
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	text = strings.TrimSpace(text)
	if channelID == "" || text == "" {
		return fmt.Errorf("slack message is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, part := range splitSlackText(text) {
		if err := g.postMessage(ctx, channelID, threadTS, part); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gateway) postMessage(ctx context.Context, channelID, threadTS, text string) error {
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	body := map[string]any{
		"channel":      channelID,
		"text":         text,
		"mrkdwn":       false,
		"unfurl_links": false,
		"unfurl_media": false,
	}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	if err := g.post(ctx, "chat.postMessage", body, &payload); err != nil {
		return err
	}
	if !payload.OK {
		if payload.Error == "" {
			payload.Error = "unknown"
		}
		return fmt.Errorf("slack post failed: %s", payload.Error)
	}
	return nil
}

// LastError is the latest poll or auth failure, in a form safe to show.
func (g *Gateway) LastError() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.errText
}

func (g *Gateway) pollLoop(ctx context.Context) {
	defer g.wg.Done()
	g.setError(nil)
	g.emit("connected")
	var lastLogged string
	for {
		if ctx.Err() != nil {
			return
		}
		err := g.pollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if err.Error() != lastLogged {
				log.Printf("[slack/gw] poll: %v", err)
				lastLogged = err.Error()
			}
			g.setError(err)
			wait := g.pollEvery
			if isTerminalSlackError(err) {
				g.emit("error")
				wait = fatalAuthWait
			} else {
				g.emit("reconnecting")
				if isRateLimit(err) {
					wait = rateLimitWait(err)
				}
			}
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}
		lastLogged = ""
		g.setError(nil)
		g.emit("connected")
		if !sleepCtx(ctx, g.pollEvery) {
			return
		}
	}
}

func (g *Gateway) pollOnce(ctx context.Context) error {
	if err := g.refreshChannels(ctx); err != nil {
		return err
	}
	for _, channelID := range g.nextChannels() {
		wm := g.cursorOf(channelID)
		messages, err := g.history(ctx, channelID, wm)
		if err != nil {
			if isRateLimit(err) || isTerminalSlackError(err) {
				return err
			}
			g.logChannelError(channelID, err)
			continue
		}
		g.clearChannelError(channelID)
		newest := wm
		for _, msg := range messages {
			if slackTSAfter(msg.TS, newest) {
				newest = msg.TS
			}
			if !g.acceptMessage(msg) || !slackTSAfter(msg.TS, wm) {
				continue
			}
			g.dispatch(incomingFrom(channelID, msg))
		}
		if len(messages) == 0 {
			replyNewest, err := g.consumeThreadReplies(ctx, channelID, wm)
			if err != nil {
				if isRateLimit(err) || isTerminalSlackError(err) {
					return err
				}
				g.logChannelError(channelID+":thread", err)
			} else if slackTSAfter(replyNewest, newest) {
				newest = replyNewest
			}
		}
		if slackTSAfter(newest, wm) {
			g.setWatermark(channelID, newest)
		}
	}
	return nil
}

func (g *Gateway) refreshChannels(ctx context.Context) error {
	g.mu.Lock()
	ttl := channelListTTL
	if len(g.channelIDs) == 0 {
		// No conversation yet. Keep looking; an 8s cache hides the first DM.
		ttl = 4 * time.Second
	}
	fresh := !g.channelsAt.IsZero() && time.Since(g.channelsAt) < ttl
	g.mu.Unlock()
	if fresh {
		return nil
	}
	ids, err := g.listIMChannels(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.channelIDs = ids
	g.channelsAt = time.Now()
	g.mu.Unlock()
	return nil
}

func (g *Gateway) nextChannels() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := len(g.channelIDs)
	if n == 0 {
		return nil
	}
	if n <= historyBatch {
		out := make([]string, n)
		copy(out, g.channelIDs)
		return out
	}
	out := make([]string, 0, historyBatch)
	for i := 0; i < historyBatch; i++ {
		out = append(out, g.channelIDs[(g.channelCursor+i)%n])
	}
	g.channelCursor = (g.channelCursor + historyBatch) % n
	return out
}

func (g *Gateway) cursorOf(channelID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cursors == nil {
		g.cursors = map[string]string{}
	}
	if wm, ok := g.cursors[channelID]; ok {
		return wm
	}
	wm := g.startedAt
	if wm == "" {
		wm = slackTSNowOffset(-initialLookback)
	}
	g.cursors[channelID] = wm
	return wm
}

func (g *Gateway) consumeThreadReplies(ctx context.Context, channelID, wm string) (string, error) {
	if !g.dueForThreadScan(channelID) {
		return "", nil
	}
	parents, _, err := g.historyPage(ctx, channelID, "", "", 15)
	if err != nil {
		return "", err
	}
	newest := ""
	for _, parent := range parents {
		parentTS := strings.TrimSpace(parent.TS)
		if parent.ReplyCount <= 0 || parentTS == "" {
			continue
		}
		key := channelID + "\x00" + parentTS
		if parent.ReplyCount <= g.replyCount(key) {
			continue
		}
		replies, err := g.threadReplies(ctx, channelID, parentTS, wm)
		if err != nil {
			return newest, err
		}
		g.setReplyCount(key, parent.ReplyCount)
		for _, msg := range replies {
			if strings.TrimSpace(msg.TS) == parentTS {
				continue
			}
			if slackTSAfter(msg.TS, newest) {
				newest = msg.TS
			}
			if !g.acceptMessage(msg) || !slackTSAfter(msg.TS, wm) {
				continue
			}
			incoming := incomingFrom(channelID, msg)
			if incoming.ThreadTS == "" {
				incoming.ThreadTS = parentTS
			}
			g.dispatch(incoming)
		}
	}
	return newest, nil
}

func incomingFrom(channelID string, msg slackMessage) IncomingMessage {
	return IncomingMessage{
		ChannelID: channelID,
		UserID:    msg.User,
		Text:      normalizeSlackText(msg.Text),
		TS:        msg.TS,
		ThreadTS:  threadParentTS(msg),
	}
}

func threadParentTS(msg slackMessage) string {
	threadTS := strings.TrimSpace(msg.ThreadTS)
	if threadTS == "" || threadTS == strings.TrimSpace(msg.TS) {
		return ""
	}
	return threadTS
}

func (g *Gateway) dueForThreadScan(channelID string) bool {
	every := g.threadScanEvery
	if every <= 0 {
		every = threadScanEvery
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.threadScanAt == nil {
		g.threadScanAt = map[string]time.Time{}
	}
	if !g.threadScanAt[channelID].IsZero() && time.Since(g.threadScanAt[channelID]) < every {
		return false
	}
	g.threadScanAt[channelID] = time.Now()
	return true
}

func (g *Gateway) replyCount(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.replyCounts[key]
}

func (g *Gateway) setReplyCount(key string, n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.replyCounts == nil {
		g.replyCounts = map[string]int{}
	}
	g.replyCounts[key] = n
}

func (g *Gateway) threadReplies(ctx context.Context, channelID, parentTS, oldest string) ([]slackMessage, error) {
	q := url.Values{}
	q.Set("channel", channelID)
	q.Set("ts", parentTS)
	q.Set("limit", "50")
	if oldest != "" {
		q.Set("oldest", oldest)
	}
	var payload struct {
		OK       bool           `json:"ok"`
		Error    string         `json:"error"`
		Messages []slackMessage `json:"messages"`
	}
	if err := g.get(ctx, "conversations.replies", q, &payload); err != nil {
		return nil, err
	}
	if !payload.OK {
		if payload.Error == "ratelimited" {
			return nil, &rateLimitError{wait: 30 * time.Second}
		}
		if payload.Error == "" {
			payload.Error = "unknown"
		}
		return nil, fmt.Errorf("%s", payload.Error)
	}
	for i, j := 0, len(payload.Messages)-1; i < j; i, j = i+1, j-1 {
		payload.Messages[i], payload.Messages[j] = payload.Messages[j], payload.Messages[i]
	}
	return payload.Messages, nil
}

func (g *Gateway) setWatermark(channelID, ts string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cursors == nil {
		g.cursors = map[string]string{}
	}
	g.cursors[channelID] = ts
}

func (g *Gateway) logChannelError(channelID string, err error) {
	msg := err.Error()
	g.mu.Lock()
	if g.lastErrByChan == nil {
		g.lastErrByChan = map[string]string{}
	}
	prev := g.lastErrByChan[channelID]
	g.lastErrByChan[channelID] = msg
	g.mu.Unlock()
	if prev == msg {
		return
	}
	log.Printf("[slack/gw] history %s: %v", channelID, err)
}

func (g *Gateway) clearChannelError(channelID string) {
	g.mu.Lock()
	delete(g.lastErrByChan, channelID)
	g.mu.Unlock()
}

func (g *Gateway) setError(err error) {
	text := explainSlackError(err)
	g.mu.Lock()
	g.errText = text
	g.mu.Unlock()
}

func (g *Gateway) acceptMessage(msg slackMessage) bool {
	if strings.TrimSpace(msg.Text) == "" || strings.TrimSpace(msg.TS) == "" {
		return false
	}
	if msg.BotID != "" || msg.Subtype != "" {
		return false
	}
	if g.botUserID != "" && msg.User == g.botUserID {
		return false
	}
	return true
}

func (g *Gateway) dispatch(msg IncomingMessage) {
	if g.handler == nil {
		return
	}
	ul := g.userLock(msg.ChannelID)
	g.handlerWg.Add(1)
	go func() {
		defer g.handlerWg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[slack/gw] handler panic: %v", rec)
			}
		}()
		ul.Lock()
		defer ul.Unlock()
		g.handler(msg)
	}()
}

func (g *Gateway) userLock(key string) *sync.Mutex {
	g.userLocksMu.Lock()
	defer g.userLocksMu.Unlock()
	ul, ok := g.userLocks[key]
	if !ok {
		ul = &sync.Mutex{}
		g.userLocks[key] = ul
	}
	return ul
}

func (g *Gateway) authTest(ctx context.Context) (string, error) {
	var payload struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
	}
	if err := g.post(ctx, "auth.test", map[string]string{}, &payload); err != nil {
		return "", err
	}
	if !payload.OK {
		if payload.Error == "" {
			payload.Error = "unknown"
		}
		return "", fmt.Errorf("slack auth failed: %s", payload.Error)
	}
	return payload.UserID, nil
}

func (g *Gateway) listIMChannels(ctx context.Context) ([]string, error) {
	var ids []string
	cursor := ""
	for page := 0; page < 5; page++ {
		q := url.Values{}
		q.Set("types", "im,mpim")
		q.Set("limit", "100")
		q.Set("exclude_archived", "true")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var payload struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			Channels []struct {
				ID string `json:"id"`
			} `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := g.get(ctx, "conversations.list", q, &payload); err != nil {
			return nil, err
		}
		if !payload.OK {
			if payload.Error == "ratelimited" {
				return nil, &rateLimitError{wait: 30 * time.Second}
			}
			if payload.Error == "" {
				payload.Error = "unknown"
			}
			return nil, fmt.Errorf("slack list failed: %s", payload.Error)
		}
		for _, ch := range payload.Channels {
			if id := strings.TrimSpace(ch.ID); id != "" {
				ids = append(ids, id)
			}
		}
		cursor = strings.TrimSpace(payload.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
	}
	return ids, nil
}

type slackMessage struct {
	TS         string `json:"ts"`
	ThreadTS   string `json:"thread_ts"`
	User       string `json:"user"`
	Text       string `json:"text"`
	BotID      string `json:"bot_id"`
	Subtype    string `json:"subtype"`
	ReplyCount int    `json:"reply_count"`
}

func (g *Gateway) history(ctx context.Context, channelID, oldest string) ([]slackMessage, error) {
	var collected []slackMessage
	cursor := ""
	for page := 0; page < 4; page++ {
		msgs, next, err := g.historyPage(ctx, channelID, oldest, cursor, 20)
		if err != nil {
			return nil, err
		}
		collected = append(collected, msgs...)
		if next == "" || len(msgs) == 0 {
			break
		}
		cursor = next
	}
	// Pages are newest-first. Deliver older messages first.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, nil
}

func (g *Gateway) historyPage(ctx context.Context, channelID, oldest, cursor string, limit int) ([]slackMessage, string, error) {
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("channel", channelID)
	q.Set("limit", strconv.Itoa(limit))
	if oldest != "" {
		q.Set("oldest", oldest)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var payload struct {
		OK               bool           `json:"ok"`
		Error            string         `json:"error"`
		Messages         []slackMessage `json:"messages"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := g.get(ctx, "conversations.history", q, &payload); err != nil {
		return nil, "", err
	}
	if !payload.OK {
		if payload.Error == "ratelimited" {
			return nil, "", &rateLimitError{wait: 30 * time.Second}
		}
		if payload.Error == "" {
			payload.Error = "unknown"
		}
		return nil, "", fmt.Errorf("%s", payload.Error)
	}
	return payload.Messages, strings.TrimSpace(payload.ResponseMetadata.NextCursor), nil
}

func (g *Gateway) get(ctx context.Context, method string, query url.Values, out any) error {
	endpoint := g.apiBase + "/" + method
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	return g.do(req, out)
}

func (g *Gateway) post(ctx context.Context, method string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiBase+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return g.do(req, out)
}

func (g *Gateway) do(req *http.Request, out any) error {
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := 30 * time.Second
		if sec, convErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); convErr == nil && sec > 0 && sec <= 120 {
			wait = time.Duration(sec) * time.Second
		}
		return &rateLimitError{wait: wait}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack http %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (g *Gateway) emit(status string) {
	if g.onStatus != nil {
		g.onStatus(status)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func slackTSNow() string {
	return slackTSNowOffset(0)
}

func slackTSNowOffset(d time.Duration) string {
	now := time.Now().Add(d)
	return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
}

func normalizeSlackText(text string) string {
	return strings.TrimSpace(html.UnescapeString(text))
}

func splitSlackText(text string) []string {
	runes := []rune(text)
	if len(runes) <= slackTextLimit {
		return []string{text}
	}
	var parts []string
	for len(runes) > 0 {
		n := slackTextLimit
		if n > len(runes) {
			n = len(runes)
		} else {
			for i := n - 1; i > slackTextLimit/2; i-- {
				if runes[i] == '\n' {
					n = i + 1
					break
				}
			}
		}
		part := strings.TrimSpace(string(runes[:n]))
		runes = runes[n:]
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

func explainSlackError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid_auth"), strings.Contains(msg, "token_revoked"), strings.Contains(msg, "token_expired"), strings.Contains(msg, "not_authed"), strings.Contains(msg, "account_inactive"):
		return "Slack 令牌已失效，请重新登录"
	case strings.Contains(msg, "missing_scope"):
		return "Slack 权限不足，请断开后重新登录"
	case strings.Contains(msg, "ratelimited"):
		return "Slack 限流，稍后会自动重试"
	default:
		return msg
	}
}

func isFatalAuth(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"invalid_auth", "token_revoked", "token_expired", "not_authed", "account_inactive"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

func isTerminalSlackError(err error) bool {
	return isFatalAuth(err) || (err != nil && strings.Contains(err.Error(), "missing_scope"))
}

func isRateLimit(err error) bool {
	var limited *rateLimitError
	return errors.As(err, &limited)
}

func rateLimitWait(err error) time.Duration {
	var limited *rateLimitError
	if errors.As(err, &limited) && limited.wait > 0 {
		return limited.wait
	}
	return 30 * time.Second
}

func slackTSAfter(a, b string) bool {
	as, am, aok := parseSlackTS(a)
	bs, bm, bok := parseSlackTS(b)
	if !aok || !bok {
		return a > b
	}
	if as != bs {
		return as > bs
	}
	return am > bm
}

func parseSlackTS(ts string) (sec, micro int64, ok bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return 0, 0, false
	}
	secPart, frac, _ := strings.Cut(ts, ".")
	var err error
	sec, err = strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if frac == "" {
		return sec, 0, true
	}
	if len(frac) > 6 {
		frac = frac[:6]
	}
	for len(frac) < 6 {
		frac += "0"
	}
	micro, err = strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return sec, micro, true
}
