package weixin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Default session file: <maclaw data>/im_proactive/weixin_sessions.json
// Survives Maclaw app restarts so 盯人 / scheduled proactive push can keep
// using the last private chat without re-messaging the bot every launch.
const (
	sessionPersistVersion   = 1
	maxPersistedSessions    = 50
	sessionPersistFileName  = "weixin_sessions.json"
	sessionPersistDirName   = "im_proactive"
)

type persistedWeixinSessions struct {
	Version          int                       `json:"version"`
	LastActiveUserID string                    `json:"last_active_user_id,omitempty"`
	Tokens           []persistedContextToken   `json:"tokens,omitempty"`
}

type persistedContextToken struct {
	UserID  string    `json:"user_id"`
	Token   string    `json:"token"`
	Updated time.Time `json:"updated"`
}

// DefaultSessionPersistPath returns the on-disk path for WeChat proactive sessions.
func DefaultSessionPersistPath() string {
	return filepath.Join(maclawpath.DataDir(), sessionPersistDirName, sessionPersistFileName)
}

// SessionPersistPathForKey returns a principal-scoped path (MaClawSrv multi-user).
// Empty key falls back to DefaultSessionPersistPath.
func SessionPersistPathForKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return DefaultSessionPersistPath()
	}
	sum := sha256.Sum256([]byte(key))
	name := "weixin_sessions_" + hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(maclawpath.DataDir(), sessionPersistDirName, name)
}

const sessionPersistDebounce = 400 * time.Millisecond

// setLocked stores a token; caller must hold c.mu.
// updated is optional (zero → now).
func (c *contextTokenCache) setLocked(userID, token string, updated time.Time) {
	if c == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	token = strings.TrimSpace(token)
	if userID == "" || token == "" {
		return
	}
	if updated.IsZero() {
		updated = time.Now()
	}
	c.tokens[userID] = contextTokenEntry{token: token, updated: updated}
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

func (c *contextTokenCache) Set(userID, token string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.setLocked(userID, token, time.Time{})
	c.mu.Unlock()
}

// SetWithTime stores a token with an explicit timestamp (for restore).
func (c *contextTokenCache) SetWithTime(userID, token string, updated time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.setLocked(userID, token, updated)
	c.mu.Unlock()
}

// Delete removes a user's context token (e.g. after API reports it invalid).
func (c *contextTokenCache) Delete(userID string) {
	if c == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	c.mu.Lock()
	delete(c.tokens, userID)
	c.mu.Unlock()
}

// InvalidateContextSession drops a user's cached context token (and last-active
// if it matches), then flushes disk so restarts do not revive a dead session.
func (g *Gateway) InvalidateContextSession(userID string) {
	userID = strings.TrimSpace(userID)
	if g == nil || userID == "" {
		return
	}
	if g.ctxTokens != nil {
		g.ctxTokens.Delete(userID)
	}
	g.mu.Lock()
	if g.lastActiveUID == userID {
		g.lastActiveUID = ""
	}
	g.mu.Unlock()
	g.flushSessionPersist()
	log.Printf("[weixin] invalidated context session for user=%s (re-chat required)", userID)
}

// maybeInvalidateOnSendError clears a stale context token when send API fails.
// Returns a wrapped error with a clear re-chat hint when invalidation applied.
func (g *Gateway) maybeInvalidateOnSendError(userID string, err error) error {
	if err == nil || !IsContextSessionError(err) {
		return err
	}
	g.InvalidateContextSession(userID)
	return fmt.Errorf("weixin: 会话已失效，请用微信私聊机器人一次以刷新: %w", err)
}

func (c *contextTokenCache) entriesByRecencyLocked() []persistedContextToken {
	if c == nil || len(c.tokens) == 0 {
		return nil
	}
	items := make([]persistedContextToken, 0, len(c.tokens))
	for uid, e := range c.tokens {
		tok := strings.TrimSpace(e.token)
		if tok == "" || strings.TrimSpace(uid) == "" {
			continue
		}
		items = append(items, persistedContextToken{
			UserID:  uid,
			Token:   tok,
			Updated: e.updated,
		})
	}
	// Newest first (same ordering as ListByRecency).
	sort.Slice(items, func(i, j int) bool {
		return items[i].Updated.After(items[j].Updated)
	})
	if len(items) > maxPersistedSessions {
		items = items[:maxPersistedSessions]
	}
	return items
}

// loadPersistedSessions restores context tokens + last active user from disk.
// Missing file is not an error.
func (g *Gateway) loadPersistedSessions() {
	if g == nil {
		return
	}
	path := strings.TrimSpace(g.sessionPersistPath)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[weixin] load session persist %s: %v", path, err)
		}
		return
	}
	var doc persistedWeixinSessions
	if err := json.Unmarshal(data, &doc); err != nil {
		log.Printf("[weixin] parse session persist %s: %v", path, err)
		return
	}
	n := 0
	if g.ctxTokens != nil {
		// File is newest-first; keep only the most recent maxPersistedSessions.
		for _, e := range doc.Tokens {
			if n >= maxPersistedSessions {
				break
			}
			uid := strings.TrimSpace(e.UserID)
			tok := strings.TrimSpace(e.Token)
			if uid == "" || tok == "" {
				continue
			}
			g.ctxTokens.SetWithTime(uid, tok, e.Updated)
			n++
		}
	}
	if last := strings.TrimSpace(doc.LastActiveUserID); last != "" {
		g.mu.Lock()
		if strings.TrimSpace(g.lastActiveUID) == "" {
			g.lastActiveUID = last
		}
		g.mu.Unlock()
	}
	if n > 0 {
		log.Printf("[weixin] restored %d proactive session(s) from %s", n, path)
	}
}

// persistSessions writes current context tokens + last active user to disk.
// Concurrent callers are serialized so temp+rename races cannot clobber the file.
func (g *Gateway) persistSessions() {
	if g == nil {
		return
	}
	path := strings.TrimSpace(g.sessionPersistPath)
	if path == "" {
		return
	}
	g.persistWriteMu.Lock()
	defer g.persistWriteMu.Unlock()

	g.mu.Lock()
	last := strings.TrimSpace(g.lastActiveUID)
	g.mu.Unlock()

	var tokens []persistedContextToken
	if g.ctxTokens != nil {
		g.ctxTokens.mu.RLock()
		tokens = g.ctxTokens.entriesByRecencyLocked()
		g.ctxTokens.mu.RUnlock()
	}
	doc := persistedWeixinSessions{
		Version:          sessionPersistVersion,
		LastActiveUserID: last,
		Tokens:           tokens,
	}
	// Compact JSON: debounced hot path; tokens are sensitive → 0600.
	data, err := json.Marshal(doc)
	if err != nil {
		log.Printf("[weixin] marshal session persist: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[weixin] mkdir session persist: %v", err)
		return
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		log.Printf("[weixin] write session persist %s: %v", path, err)
	}
}

// rememberInboundSession updates last-active (+ optional context token) and
// schedules a debounced disk flush when the session graph changed.
func (g *Gateway) rememberInboundSession(userID, token string) {
	userID = strings.TrimSpace(userID)
	token = strings.TrimSpace(token)
	if g == nil || userID == "" {
		return
	}
	g.mu.Lock()
	uidChanged := g.lastActiveUID != userID
	g.lastActiveUID = userID
	g.mu.Unlock()

	if token != "" && g.ctxTokens != nil {
		// Always Set: refreshes LRU "updated" time even when token string is unchanged.
		g.ctxTokens.Set(userID, token)
	}
	// Skip disk schedule only when neither peer nor token changed (empty-token echo).
	if !uidChanged && token == "" {
		return
	}
	g.scheduleSessionPersist()
}

// scheduleSessionPersist coalesces rapid inbound messages into one disk write.
func (g *Gateway) scheduleSessionPersist() {
	if g == nil || strings.TrimSpace(g.sessionPersistPath) == "" {
		return
	}
	g.persistMu.Lock()
	defer g.persistMu.Unlock()
	if g.persistTimer != nil {
		// Stop may return false if the callback already started; that write is fine.
		_ = g.persistTimer.Stop()
	}
	g.persistTimer = time.AfterFunc(sessionPersistDebounce, func() {
		g.persistMu.Lock()
		g.persistTimer = nil
		g.persistMu.Unlock()
		g.persistSessions()
	})
}

// flushSessionPersist cancels a pending debounce and writes immediately.
// Safe to call when no path is configured.
func (g *Gateway) flushSessionPersist() {
	if g == nil {
		return
	}
	g.persistMu.Lock()
	if g.persistTimer != nil {
		_ = g.persistTimer.Stop()
		g.persistTimer = nil
	}
	g.persistMu.Unlock()
	g.persistSessions()
}


