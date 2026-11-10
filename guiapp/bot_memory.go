package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// BotMemoryEntry is the GUI projection of one bot-scoped long-term memory row.
type BotMemoryEntry struct {
	ID          string   `json:"id"`
	BotID       string   `json:"bot_id"`
	Content     string   `json:"content"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	AccessCount int      `json:"access_count"`
}

func normalizeBotMemoryCategory(category string) memory.Category {
	category = strings.TrimSpace(category)
	if category == "" {
		return memory.CategoryUserFact
	}
	return memory.Category(category)
}

func projectBotMemory(botID string, entry memory.Entry) BotMemoryEntry {
	return BotMemoryEntry{
		ID:          entry.ID,
		BotID:       botID,
		Content:     entry.Content,
		Category:    string(memory.MapToCanonical(entry.Category)),
		Tags:        append([]string(nil), entry.Tags...),
		CreatedAt:   entry.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   entry.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		AccessCount: entry.AccessCount,
	}
}

// ListBotMemories returns structured long-term memory for one desktop bot.
func (a *App) ListBotMemories(botID, category, keyword string) ([]BotMemoryEntry, error) {
	a.ensureInteractionInfra()
	runtime, release := a.acquireSharedMemoryRuntime()
	defer release()
	if runtime == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	botID = strings.TrimSpace(botID)
	entries, err := runtime.Store.ListBotMemories(desktopUserID, botID, category, keyword)
	if err != nil {
		return nil, err
	}
	out := make([]BotMemoryEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, projectBotMemory(botID, entry))
	}
	return out, nil
}

// SaveBotMemory creates a structured long-term memory entry for one bot.
func (a *App) SaveBotMemory(botID, content, category string, tags []string) (*BotMemoryEntry, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "bot_id": botID, "category": category, "tags": tags}); err != nil {
		return nil, err
	}
	a.ensureInteractionInfra()
	runtime, release := a.acquireSharedMemoryRuntime()
	defer release()
	if runtime == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	botID = strings.TrimSpace(botID)
	entry, err := runtime.Store.SaveBotMemory(desktopUserID, botID, content, string(normalizeBotMemoryCategory(category)), tags)
	if err != nil {
		return nil, err
	}
	out := projectBotMemory(botID, entry)
	return &out, nil
}

// UpdateBotMemory edits one bot-scoped long-term memory entry.
func (a *App) UpdateBotMemory(botID, id, content, category string, tags []string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "bot_id": botID, "id": id, "category": category, "tags": tags}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	runtime, release := a.acquireSharedMemoryRuntime()
	defer release()
	if runtime == nil {
		return fmt.Errorf("memory store not initialized")
	}
	if err := runtime.Store.UpdateBotMemory(desktopUserID, strings.TrimSpace(botID), id, content, string(normalizeBotMemoryCategory(category)), tags); err != nil {
		return err
	}
	return nil
}

// DeleteBotMemory removes one bot-scoped long-term memory entry.
func (a *App) DeleteBotMemory(botID, id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "delete", "bot_id": botID, "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	runtime, release := a.acquireSharedMemoryRuntime()
	defer release()
	if runtime == nil {
		return fmt.Errorf("memory store not initialized")
	}
	if err := runtime.Store.DeleteBotMemory(desktopUserID, strings.TrimSpace(botID), id); err != nil {
		return err
	}
	return nil
}

// DeleteBotMemories bulk-deletes bot-scoped long-term memory entries.
func (a *App) DeleteBotMemories(botID string, ids []string) (map[string]any, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "delete", "bot_id": botID, "ids": ids}); err != nil {
		return nil, err
	}
	a.ensureInteractionInfra()
	runtime, release := a.acquireSharedMemoryRuntime()
	defer release()
	if runtime == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	deleted, err := runtime.Store.DeleteBotMemoriesAtomic(desktopUserID, strings.TrimSpace(botID), ids)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted, "skipped": []string{}}, nil
}
