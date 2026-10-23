package guiapp

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

type conversationViewArchiveDocument struct {
	Messages []interface{} `json:"messages"`
}

func (a *App) readAIAssistantUIArchiveLocked() []interface{} {
	if a == nil {
		return []interface{}{}
	}
	data, err := os.ReadFile(a.aiAssistantUIArchivePath())
	if err != nil {
		return []interface{}{}
	}
	var document conversationViewArchiveDocument
	if json.Unmarshal(data, &document) != nil || len(document.Messages) == 0 {
		return []interface{}{}
	}
	return document.Messages
}

func (a *App) readAIAssistantUIMessagesLocked() []map[string]interface{} {
	if a == nil {
		return nil
	}
	data, err := os.ReadFile(a.aiAssistantUIStatePath())
	if err != nil {
		return nil
	}
	var state AIAssistantUIState
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	return state.Messages
}

func (a *App) writeAIAssistantUIArchiveLocked(messages []interface{}) error {
	path := a.aiAssistantUIArchivePath()
	if len(messages) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return configfile.AtomicWriteJSON(path, conversationViewArchiveDocument{Messages: messages})
}

// windowAIAssistantUIStateLocked keeps the hot tail in the UI state and the
// older turns in the archive file. includeDisk merges the previous hot file
// so a client that only saved the tail cannot drop the archive.
// The caller holds aiAssistantUIStateMu. Archive is written here. The hot
// state file is written here only when includeDisk is false and the loaded
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
	path := a.aiAssistantUIStatePath()
	state.StoragePath = ""
	if err := configfile.AtomicWriteJSON(path, state); err != nil {
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
		return conversationViewLocalCount(a.readAIAssistantUIArchiveLocked())
	case strings.HasPrefix(scope, "tab:"):
		tabID := strings.TrimSpace(strings.TrimPrefix(scope, "tab:"))
		if a == nil || tabID == "" {
			return 0
		}
		messages, err := a.ensureProjectTabSessionPersist().LoadConversationViewArchive(tabID)
		if err != nil {
			return 0
		}
		return len(messages)
	default:
		return 0
	}
}
