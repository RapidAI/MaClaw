package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// CorrectionStore tracks pending correction options keyed by correctionID.
// When the user clicks a correction button, the gateway looks up the stored
// context (original message text, original action) and calls HandleCorrection.
//
// Entries expire automatically after DefaultCorrectionTTL. The store is safe
// for concurrent use.
type CorrectionStore struct {
	mu      sync.Mutex
	entries map[string]*correctionEntry
}

type correctionEntry struct {
	UserID         string
	MessageText    string         // the original user message
	OriginalAction ScheduleAction // what the scheduler decided
	Options        []CorrectionOption
	CreatedAt      time.Time
}

// NewCorrectionStore creates an empty correction store.
func NewCorrectionStore() *CorrectionStore {
	return &CorrectionStore{
		entries: make(map[string]*correctionEntry),
	}
}

// Store saves correction options for a user's message. Returns a unique
// correction ID that can be used to look up the entry later.
func (cs *CorrectionStore) Store(userID, messageText string, originalAction ScheduleAction, options []CorrectionOption) string {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Purge expired entries lazily.
	cs.purgeLocked()

	id := fmt.Sprintf("corr-%s-%d", userID, time.Now().UnixNano())
	cs.entries[id] = &correctionEntry{
		UserID:         userID,
		MessageText:    messageText,
		OriginalAction: originalAction,
		Options:        options,
		CreatedAt:      time.Now(),
	}
	return id
}

// Consume atomically retrieves and removes a correction entry by ID.
// Returns false if not found, expired, or idx is out of range.
// This is the only retrieval method — there is no separate Lookup to
// avoid TOCTOU races between Lookup and Consume.
func (cs *CorrectionStore) Consume(correctionID string, optionIdx int) (userID, messageText string, originalAction ScheduleAction, chosen CorrectionOption, ok bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entry, exists := cs.entries[correctionID]
	if !exists {
		return "", "", 0, CorrectionOption{}, false
	}

	if time.Since(entry.CreatedAt) > time.Duration(DefaultCorrectionTTL)*time.Second {
		delete(cs.entries, correctionID)
		return "", "", 0, CorrectionOption{}, false
	}

	if optionIdx < 0 || optionIdx >= len(entry.Options) {
		return "", "", 0, CorrectionOption{}, false
	}

	chosen = entry.Options[optionIdx]
	userID = entry.UserID
	messageText = entry.MessageText
	originalAction = entry.OriginalAction

	delete(cs.entries, correctionID)
	return userID, messageText, originalAction, chosen, true
}

// InvalidateUser removes all pending corrections for a user. Called when
// the current task ends (corrections are no longer meaningful).
func (cs *CorrectionStore) InvalidateUser(userID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for id, entry := range cs.entries {
		if entry.UserID == userID {
			delete(cs.entries, id)
		}
	}
}

// Remove deletes a correction entry by ID without returning its contents.
// Returns true if the entry existed and was removed (i.e. user didn't
// respond yet). Returns false if already consumed or expired.
func (cs *CorrectionStore) Remove(correctionID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	_, exists := cs.entries[correctionID]
	if !exists {
		return false
	}
	delete(cs.entries, correctionID)
	return true
}

// purgeLocked removes expired entries. Must be called with cs.mu held.
func (cs *CorrectionStore) purgeLocked() {
	now := time.Now()
	ttl := time.Duration(DefaultCorrectionTTL) * time.Second
	for id, entry := range cs.entries {
		if now.Sub(entry.CreatedAt) > ttl {
			delete(cs.entries, id)
		}
	}
}

// FormatCorrectionsText appends correction options to a reply string as
// numbered text links. This is the fallback for IM channels that don't
// support rich buttons (WeChat, QQ). The user can reply with the number
// to trigger the correction.
//
// Example output:
//
//	收到，已纳入当前任务。
//	  回复1: 改为打断 | 回复2: 改为排队
func FormatCorrectionsText(reply string, corrections []CorrectionOption) string {
	if len(corrections) == 0 {
		return reply
	}

	var parts []string
	for i, c := range corrections {
		parts = append(parts, fmt.Sprintf("回复%d: %s", i+1, c.Label))
	}

	return reply + "\n  " + strings.Join(parts, " | ")
}
