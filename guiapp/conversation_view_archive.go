package guiapp

import (
	"log"
	"strings"
)

type conversationViewArchiveDocument struct {
	Messages []interface{} `json:"messages"`
	// Count is the number of messages the archive-scope count endpoint
	// reports for this file (local-session rows for the desktop archive,
	// all rows for tab archives). Writers stamp it atomically with the
	// archive; readers fall back to a full count when it is absent, which
	// covers archives written before the field existed.
	Count *int `json:"count,omitempty"`
}

func (a *App) readAIAssistantUIArchiveLocked() []interface{} {
	if a == nil {
		return []interface{}{}
	}
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return []interface{}{}
	}
	messages, err := loadAIAssistantUIMessageRows(db, aiAssistantUIArchiveLocation)
	if err != nil {
		return []interface{}{}
	}
	return mapsToAny(messages)
}

func (a *App) readAIAssistantUIMessagesLocked() []map[string]interface{} {
	if a == nil {
		return nil
	}
	state, err := a.loadAIAssistantUIStateLocked()
	if err != nil {
		return nil
	}
	return state.Messages
}

func (a *App) writeAIAssistantUIArchiveLocked(messages []interface{}) error {
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAIAssistantUIArchiveTx(tx, messages); err != nil {
		return err
	}
	return tx.Commit()
}

// readAIAssistantUIArchiveCountLocked reports the archived local-message
// count directly from the indexed archive rows without decoding message bodies.
func (a *App) readAIAssistantUIArchiveCountLocked() int {
	if a == nil {
		return 0
	}
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return 0
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_assistant_ui_messages WHERE location=? AND is_local=1`, aiAssistantUIArchiveLocation).Scan(&count); err != nil {
		return 0
	}
	return count
}

// windowAIAssistantUIStateLocked keeps the hot tail in the UI state table and
// older turns in the archive table. includeDisk merges the previous hot rows
// so a client that only saved the tail cannot drop the archive.
// The caller holds aiAssistantUIStateMu. Archive rows are written here. Hot
// rows are written here only when includeDisk is false and the loaded
// transcript was longer than the hot tail.
func (a *App) windowAIAssistantUIStateLocked(state *AIAssistantUIState, includeDisk bool) error {
	if a == nil || state == nil {
		return nil
	}
	archive := anyToMaps(a.readAIAssistantUIArchiveLocked())
	older := archive
	if includeDisk {
		older = mergeConversationViewMaps(archive, a.readAIAssistantUIMessagesLocked())
	}
	merged := mergeConversationViewMaps(older, state.Messages)
	hot, earlier := splitConversationViewMaps(merged)
	earlierAny := capConversationViewArchive(mapsToAny(earlier))
	storedCount := len(state.Messages)
	state.Messages = hot
	state.ArchivedCount = conversationViewLocalCount(earlierAny)
	archiveSame := sameConversationViewIDs(mapsToAny(archive), earlierAny)
	if archiveSame && (includeDisk || storedCount == len(hot)) {
		return nil
	}
	if err := a.writeAIAssistantUIArchiveLocked(earlierAny); err != nil {
		return err
	}
	if includeDisk {
		return nil
	}
	path := a.aiAssistantUIStateDBPath()
	state.StoragePath = ""
	if err := a.saveAIAssistantUIStateLocked(*state); err != nil {
		return err
	}
	log.Printf("[paths] ai_assistant_ui_state windowed path=%q hot=%d archived=%d", path, len(hot), len(earlierAny))
	return nil
}

// LoadAIAssistantUIArchive returns earlier desktop transcript messages.
// Project-scoped rows that share the UI file stay on disk and out of this view.
func (a *App) LoadAIAssistantUIArchive() []map[string]interface{} {
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()

	out := make([]map[string]interface{}, 0)
	for _, message := range a.readAIAssistantUIArchiveLocked() {
		if !conversationViewIsLocal(message) {
			continue
		}
		if mapped := conversationViewMap(message); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

// ConversationViewArchiveCount reports how many archived messages a view can open.
// scope is "desktop" or "tab:<tabID>".
func (a *App) ConversationViewArchiveCount(scope string) int {
	scope = strings.TrimSpace(scope)
	switch {
	case scope == "desktop":
		aiAssistantUIStateMu.Lock()
		defer aiAssistantUIStateMu.Unlock()
		return a.readAIAssistantUIArchiveCountLocked()
	case strings.HasPrefix(scope, "tab:"):
		tabID := strings.TrimSpace(strings.TrimPrefix(scope, "tab:"))
		if a == nil || tabID == "" {
			return 0
		}
		count, err := a.ensureProjectTabSessionPersist().ConversationViewArchiveCount(tabID)
		if err != nil {
			return 0
		}
		return count
	default:
		return 0
	}
}
