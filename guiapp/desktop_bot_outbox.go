// Package guiapp implements the persistent desktop-bot outbox.
//
// A desktop-bot turn moves through three states: local (composed, not yet
// sent), synced (sent, waiting for Hub to admit), and admitted (Hub named its
// run id). Only an admitted turn is real: a send that was never acknowledged
// must not look like it executed. This file keeps every send that has not yet
// been admitted in a durable file so the page can show it as still-to-deliver
// across a restart, and so the user can retry it explicitly.
package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DesktopBotOutboxItem is one unsent/unadmitted turn. It persists until Hub
// admits it (state=admitted) or the user retries (status moves back to the
// send path). A non-empty Failed means the delivery hit a hard error and is
// surfaced as retryable rather than silently dropped.
type DesktopBotOutboxItem struct {
	Key        string `json:"key"`
	ID         string `json:"id"`          // messageId used by the chat bubble
	BotID      string `json:"bot_id"`
	UserID     string `json:"user_id"`
	Text       string `json:"text"`
	Status     string `json:"status"`      // queued | admitted | failed
	Failed     string `json:"failed"`      // last delivery error, empty while queued
	Retryable  bool   `json:"retryable"`   // failure is a transport/timeout, retry is safe
	CreatedAt  int64  `json:"created_at"`  // unix ms
	Dispatched int64  `json:"dispatched_at"` // unix ms, 0 until Hub admitted
	UpdatedAt  int64  `json:"updated_at"`  // unix ms
}

// desktopBotOutboxKey builds the per-message durable key from the turn's
// request id so a restart rebinds to the same message idempotent id.
func desktopBotOutboxKey(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return "desktop-bot-outbox:" + requestID
}

// DesktopBotOutboxSnapshot is the front-end view of one outbox entry plus the
// live send it still binds to (message_id of the bubble it owns).
type DesktopBotOutboxSnapshot struct {
	Items []DesktopBotOutboxItem `json:"items"`
}

var (
	desktopBotOutboxMu    sync.Mutex
	desktopBotOutboxItems = map[string]*DesktopBotOutboxItem{}
	desktopBotOutboxDir   = ""
	desktopBotOutboxFile  = "desktop_bot_outbox.json"
)

// desktopBotOutboxPath returns the durable file under the app data dir.
func (a *App) desktopBotOutboxPath() string {
	dir := strings.TrimSpace(desktopBotOutboxDir)
	if dir == "" {
		dir = a.GetDataDir()
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "bots", desktopBotOutboxFile)
}

// desktopBotOutboxInit loads the persisted outbox once the data dir is known.
// It marks any in-flight sends that never reached Hub as still-queued so the
// page can offer an explicit retry rather than replaying them.
func (a *App) desktopBotOutboxInit() {
	path := a.desktopBotOutboxPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log_desktop_bot_outbox("read failed", err)
		}
		return
	}
	var items []DesktopBotOutboxItem
	if err := json.Unmarshal(raw, &items); err != nil {
		log_desktop_bot_outbox("decode failed", err)
		return
	}
	desktopBotOutboxMu.Lock()
	defer desktopBotOutboxMu.Unlock()
	for i := range items {
		it := items[i]
		if strings.TrimSpace(it.Key) == "" || strings.TrimSpace(it.BotID) == "" {
			continue
		}
		if it.Status != "admitted" && it.Status != "failed" {
			it.Status = "queued"
		}
		copied := it
		desktopBotOutboxItems[it.Key] = &copied
	}
}

func log_desktop_bot_outbox(msg string, err error) {
	// Keep the log quiet in tests; the store is best-effort but never panics.
	_ = msg
	_ = err
}

// desktopBotOutboxSave writes the current map to disk. Callers hold the lock
// or call desktopBotOutboxPersist which serializes writes.
func (a *App) desktopBotOutboxSave() {
	path := a.desktopBotOutboxPath()
	if path == "" {
		return
	}
	items := make([]DesktopBotOutboxItem, 0, len(desktopBotOutboxItems))
	for _, it := range desktopBotOutboxItems {
		if it == nil {
			continue
		}
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt < items[j].CreatedAt })
	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// desktopBotOutboxPersist saves under a short write lock.
func (a *App) desktopBotOutboxPersist() {
	desktopBotOutboxMu.Lock()
	defer desktopBotOutboxMu.Unlock()
	a.desktopBotOutboxSave()
}

// desktopBotOutboxSeed records a send that is about to go out. It is keyed by
// the request id so a later admit finds the same row.
func (a *App) desktopBotOutboxSeed(botID, userID, requestID, text string) {
	botID = strings.TrimSpace(botID)
	key := desktopBotOutboxKey(requestID)
	if botID == "" || key == "" {
		return
	}
	now := time.Now().UnixMilli()
	desktopBotOutboxMu.Lock()
	it := desktopBotOutboxItems[key]
	if it == nil {
		it = &DesktopBotOutboxItem{Key: key, ID: requestID, BotID: botID, UserID: strings.TrimSpace(userID), Text: text, CreatedAt: now}
		desktopBotOutboxItems[key] = it
	}
	it.Status = "queued"
	it.Failed = ""
	it.Retryable = false
	it.UpdatedAt = now
	a.desktopBotOutboxSave()
	desktopBotOutboxMu.Unlock()
	a.emitDesktopBotOutbox()
}

// desktopBotOutboxMarkAdmitted flips a queued item to admitted once Hub names
// the run. This is the only path that makes a delivery real.
func (a *App) desktopBotOutboxMarkAdmitted(botID, requestID string) {
	key := desktopBotOutboxKey(requestID)
	if key == "" {
		return
	}
	now := time.Now().UnixMilli()
	desktopBotOutboxMu.Lock()
	if it := desktopBotOutboxItems[key]; it != nil {
		it.Status = "admitted"
		it.Failed = ""
		it.Dispatched = now
		it.UpdatedAt = now
	}
	a.desktopBotOutboxSave()
	desktopBotOutboxMu.Unlock()
	a.emitDesktopBotOutbox()
}

// desktopBotOutboxMarkFailed records a delivery error. retryable marks a
// transport/timeout that is safe to send again; a permanent error stays
// visible but not offered for retry.
func (a *App) desktopBotOutboxMarkFailed(requestID, failure string, retryable bool) {
	key := desktopBotOutboxKey(requestID)
	if key == "" {
		return
	}
	failure = strings.TrimSpace(failure)
	now := time.Now().UnixMilli()
	desktopBotOutboxMu.Lock()
	it := desktopBotOutboxItems[key]
	if it != nil && it.Status == "admitted" {
		// admitted is terminal: Hub already named the run. A late or stray
		// failure report must not downgrade a confirmed delivery back to
		// failed, or the page would show executed work as undelivered.
		desktopBotOutboxMu.Unlock()
		return
	}
	if it != nil {
		it.Status = "failed"
		it.Failed = failure
		it.Retryable = retryable
		it.UpdatedAt = now
	}
	a.desktopBotOutboxSave()
	desktopBotOutboxMu.Unlock()
	a.emitDesktopBotOutbox()
}

// desktopBotOutboxRemove drops a row once the message is settled or the user
// discards it, so a stale entry never reappears.
func (a *App) desktopBotOutboxRemove(requestID string) {
	key := desktopBotOutboxKey(requestID)
	if key == "" {
		return
	}
	desktopBotOutboxMu.Lock()
	delete(desktopBotOutboxItems, key)
	a.desktopBotOutboxSave()
	desktopBotOutboxMu.Unlock()
	a.emitDesktopBotOutbox()
}

// emitDesktopBotOutbox pushes the current outbox to the page.
func (a *App) emitDesktopBotOutbox() {
	a.emitEvent("desktop-bot-outbox", a.DesktopBotOutboxSnapshot())
}

// DesktopBotOutboxSnapshot returns the durable outbox ordered oldest-first.
func (a *App) DesktopBotOutboxSnapshot() DesktopBotOutboxSnapshot {
	desktopBotOutboxMu.Lock()
	defer desktopBotOutboxMu.Unlock()
	out := make([]DesktopBotOutboxItem, 0, len(desktopBotOutboxItems))
	for _, it := range desktopBotOutboxItems {
		if it == nil {
			continue
		}
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return DesktopBotOutboxSnapshot{Items: out}
}

// ListDesktopBotOutbox is the Wails binding the page polls.
func (a *App) ListDesktopBotOutbox() []DesktopBotOutboxItem {
	return a.DesktopBotOutboxSnapshot().Items
}

// MarkDesktopBotOutboxFailed lets the page record a delivery failure it
// observed itself (for example a send that returned a transport error before
// the Hub round-trip began), so the bubble and the outbox agree.
func (a *App) MarkDesktopBotOutboxFailed(requestID, failure string) error {
	requestID = strings.TrimSpace(requestID)
	failure = strings.TrimSpace(failure)
	if requestID == "" {
		return fmt.Errorf("request id is required")
	}
	a.desktopBotOutboxMarkFailed(requestID, failure, false)
	return nil
}

// RelaunchDesktopBotOutbox retries one queued or retryable-failed entry. The
// send is user-initiated only: nothing replays an unadmitted turn on its own,
// because the original may still be in flight or partially applied.
func (a *App) RelaunchDesktopBotOutbox(requestID string) error {
	requestID = strings.TrimSpace(requestID)
	key := desktopBotOutboxKey(requestID)
	if key == "" {
		return fmt.Errorf("request id is required")
	}
	desktopBotOutboxMu.Lock()
	it := desktopBotOutboxItems[key]
	var text, botID string
	if it != nil {
		if it.Status == "admitted" {
			desktopBotOutboxMu.Unlock()
			return fmt.Errorf("turn already admitted")
		}
		if it.Status == "failed" && !it.Retryable {
			desktopBotOutboxMu.Unlock()
			return fmt.Errorf("turn is not retryable: %s", it.Failed)
		}
		text, botID = it.Text, it.BotID
		it.Status = "queued"
		it.Failed = ""
		it.UpdatedAt = time.Now().UnixMilli()
		a.desktopBotOutboxSave()
	}
	desktopBotOutboxMu.Unlock()
	if botID == "" || text == "" {
		return fmt.Errorf("nothing to retry")
	}
	a.emitDesktopBotOutbox()
	// Re-run the same send path; the request id stays stable so a duplicate
	// admit binds to the same bubble instead of creating a second line. The
	// item was already re-queued above; finishDesktopBotTask only reports.
	sessionKey := ""
	if userID, _, idErr := desktopBotIdentity(a.desktopBotAccountUserID(), botID); idErr == nil {
		sessionKey = desktopBotLocalSessionKey(userID, botID)
	}
	go func() {
		a.finishDesktopBotTask(requestID, sessionKey, botID, text, "chat")
	}()
	return nil
}

// isDesktopBotRetryableErr reports whether a send failure is a transport or
// timeout that a retry can succeed on, as opposed to a permanent rejection
// (validation, auth) that would only fail again.
func isDesktopBotRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"timeout", "timed out", "deadline", "connection", "reset", "refused",
		"eof", "network", "unavailable", "no such host", "temporary", "broken pipe",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// DesktopBotSetOutboxStatus lets the page flag a bubble's delivery state when
// it knows more than the durable store does (for example a deferred response
// it just received). It never writes admitted itself: only the admit path does.
func (a *App) DesktopBotSetOutboxStatus(requestID, status, failure string) error {
	requestID = strings.TrimSpace(requestID)
	status = strings.TrimSpace(status)
	failure = strings.TrimSpace(failure)
	if requestID == "" {
		return fmt.Errorf("request id is required")
	}
	switch status {
	case "queued":
		desktopBotOutboxMu.Lock()
		if it := desktopBotOutboxItems[desktopBotOutboxKey(requestID)]; it != nil {
			it.Status = "queued"
			it.UpdatedAt = time.Now().UnixMilli()
			a.desktopBotOutboxSave()
		}
		desktopBotOutboxMu.Unlock()
		a.emitDesktopBotOutbox()
		return nil
	case "failed":
		a.desktopBotOutboxMarkFailed(requestID, failure, isDesktopBotRetryableErr(fmt.Errorf("%s", failure)))
		return nil
	default:
		return fmt.Errorf("unsupported outbox status %q", status)
	}
}

// desktopBotOutboxProbe returns the durable state of one request id, used by
// GetActiveDesktopRuns to keep a queued-but-unadmitted turn visible.
func desktopBotOutboxProbe(requestID string) (status, failed string, ok bool) {
	key := desktopBotOutboxKey(requestID)
	if key == "" {
		return "", "", false
	}
	desktopBotOutboxMu.Lock()
	defer desktopBotOutboxMu.Unlock()
	if it := desktopBotOutboxItems[key]; it != nil {
		return it.Status, it.Failed, true
	}
	return "", "", false
}

// desktopBotOutboxForget is the test hook that resets the store.
func desktopBotOutboxForget() {
	desktopBotOutboxMu.Lock()
	defer desktopBotOutboxMu.Unlock()
	desktopBotOutboxItems = map[string]*DesktopBotOutboxItem{}
}
