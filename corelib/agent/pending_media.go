// corelib/agent/pending_media.go - Pending media buffer for IM message handler.
//
// When a user sends an image/file without accompanying text, the buffer
// holds the attachments for up to 10 seconds, waiting for a follow-up
// text message that describes the user's intent. If no text arrives
// within the timeout, a prompt is sent via the onProgress callback
// asking the user what they want to do with the media.
//
// Multiple attachments arriving within the window are merged; each new
// arrival resets the 10-second timer.
//
// Migrated from gui/im_pending_media.go as part of the agent-unification plan.
package agent

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

const PendingMediaTimeout = 10 * time.Second

// PendingMediaStaleTimeout is the maximum time an entry can remain in the
// buffer after the timer has fired. If the user never sends a follow-up
// text, the entry is garbage-collected to prevent memory leaks.
const PendingMediaStaleTimeout = 5 * time.Minute

// PendingMediaEntry holds buffered attachments for a single user.
type PendingMediaEntry struct {
	Attachments []MessageAttachment
	Timer       *time.Timer
	OnProgress  ProgressCallback // stored so the timeout goroutine can notify the user
	CreatedAt   time.Time        // for stale entry cleanup
}

// PendingMediaBuffer is a per-user buffer for media-only messages.
type PendingMediaBuffer struct {
	mu      sync.Mutex
	entries map[string]*PendingMediaEntry // keyed by userID
}

func NewPendingMediaBuffer() *PendingMediaBuffer {
	b := &PendingMediaBuffer{
		entries: make(map[string]*PendingMediaEntry),
	}
	go b.cleanupLoop()
	return b
}

// Add appends attachments for userID and (re)starts the timeout timer.
// Returns true if the media was buffered (caller should return Deferred).
func (b *PendingMediaBuffer) Add(userID string, attachments []MessageAttachment, onProgress ProgressCallback) bool {
	if len(attachments) == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.entries[userID]
	if exists {
		// Accumulate and reset timer.
		entry.Attachments = append(entry.Attachments, attachments...)
		entry.CreatedAt = time.Now() // reset stale clock on new arrivals
		if entry.OnProgress == nil && onProgress != nil {
			entry.OnProgress = onProgress
		}
		if entry.Timer != nil {
			entry.Timer.Reset(PendingMediaTimeout)
		} else {
			// Timer already fired; restart it for the new batch.
			entry.Timer = time.AfterFunc(PendingMediaTimeout, func() {
				b.onTimeout(userID)
			})
		}
		log.Printf("[PendingMedia] user=%s accumulated %d attachments (total %d)", userID, len(attachments), len(entry.Attachments))
		return true
	}

	// New entry.
	entry = &PendingMediaEntry{
		Attachments: append([]MessageAttachment(nil), attachments...),
		OnProgress:  onProgress,
		CreatedAt:   time.Now(),
	}
	entry.Timer = time.AfterFunc(PendingMediaTimeout, func() {
		b.onTimeout(userID)
	})
	b.entries[userID] = entry
	log.Printf("[PendingMedia] user=%s buffered %d attachments, waiting %v for intent", userID, len(attachments), PendingMediaTimeout)
	return true
}

// Drain removes and returns any pending attachments for userID.
// Returns nil if nothing is pending.
func (b *PendingMediaBuffer) Drain(userID string) []MessageAttachment {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.entries[userID]
	if !exists {
		return nil
	}
	if entry.Timer != nil {
		entry.Timer.Stop()
	}
	atts := entry.Attachments
	delete(b.entries, userID)
	log.Printf("[PendingMedia] user=%s drained %d attachments", userID, len(atts))
	return atts
}

// cleanupLoop periodically removes stale entries that were never drained.
func (b *PendingMediaBuffer) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		b.mu.Lock()
		now := time.Now()
		for uid, entry := range b.entries {
			if now.Sub(entry.CreatedAt) > PendingMediaStaleTimeout {
				if entry.Timer != nil {
					entry.Timer.Stop()
				}
				delete(b.entries, uid)
				log.Printf("[PendingMedia] user=%s stale entry removed (%d attachments)", uid, len(entry.Attachments))
			}
		}
		b.mu.Unlock()
	}
}

// onTimeout fires when the user hasn't sent a follow-up text within the window.
// The attachments are kept in the buffer so a subsequent Drain can still retrieve
// them; only the timer reference is cleared. A background cleanup goroutine
// removes truly stale entries after a generous grace period.
func (b *PendingMediaBuffer) onTimeout(userID string) {
	b.mu.Lock()
	entry, exists := b.entries[userID]
	if !exists {
		b.mu.Unlock()
		return
	}
	// Keep attachments for a follow-up Drain; just record that the timer fired.
	onProgress := entry.OnProgress
	count := len(entry.Attachments)
	entry.OnProgress = nil // avoid double-notify
	entry.Timer = nil      // mark as timed-out
	b.mu.Unlock()

	log.Printf("[PendingMedia] user=%s timeout after %v, %d attachments still buffered", userID, PendingMediaTimeout, count)

	if onProgress == nil {
		return
	}

	// Build a friendly prompt; NOT added to conversation history.
	prompt := BuildMediaPrompt(count)
	onProgress(prompt)
}

// BuildMediaPrompt creates the timeout prompt shown to the user.
func BuildMediaPrompt(count int) string {
	if count == 1 {
		return i18n.T(i18n.MsgMediaSingle, "zh")
	}
	return i18n.Tf(i18n.MsgMediaMultiple, "zh", count)
}

// CanInferIntentFromHistory checks the last few conversation entries through
// the shared semantic task-intent classifier. Media is attached to history only
// when an execution/document route is classified; without the classifier, this
// deliberately returns false and asks the user for intent.
func CanInferIntentFromHistory(entries []ConversationEntry) bool {
	const lookback = 3
	checked := 0
	for i := len(entries) - 1; i >= 0 && checked < lookback; i-- {
		if entries[i].Role != "user" {
			continue
		}
		checked++
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		intent := ClassifyTaskIntent(text)
		switch intent.Intent {
		case IntentCoding, IntentSSH, IntentNonCoding:
			return true
		}
	}
	return false
}

// IsSynthesizedMediaText returns true if the text was auto-generated by the
// webhook layer for a media-only message (e.g. "[用户发送了文件]").
// These messages should be buffered rather than processed immediately.
func IsSynthesizedMediaText(text string) bool {
	return strings.HasPrefix(text, "[用户发送了")
}
