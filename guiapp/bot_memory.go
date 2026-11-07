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
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	botID = strings.TrimSpace(botID)
	entries, err := a.memoryStore.ListBotMemories(desktopUserID, botID, category, keyword)
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
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	botID = strings.TrimSpace(botID)
	entry, err := a.memoryStore.SaveBotMemory(desktopUserID, botID, content, string(normalizeBotMemoryCategory(category)), tags)
	if err != nil {
		return nil, err
	}
	a.notifyMemoryWarehouseChanged(memoryWarehouseChange{})
	out := projectBotMemory(botID, entry)
	return &out, nil
}

// UpdateBotMemory edits one bot-scoped long-term memory entry.
func (a *App) UpdateBotMemory(botID, id, content, category string, tags []string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "bot_id": botID, "id": id, "category": category, "tags": tags}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	previous := a.memoryEntriesByIDs(id)
	if err := a.memoryStore.UpdateBotMemory(desktopUserID, strings.TrimSpace(botID), id, content, string(normalizeBotMemoryCategory(category)), tags); err != nil {
		return err
	}
	a.notifyMemoryWarehouseChanged(warehouseChangeFromEntries(previous, "updated", content))
	return nil
}

// DeleteBotMemory removes one bot-scoped long-term memory entry.
func (a *App) DeleteBotMemory(botID, id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "delete", "bot_id": botID, "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	previous := a.memoryEntriesByIDs(id)
	if err := a.memoryStore.DeleteBotMemory(desktopUserID, strings.TrimSpace(botID), id); err != nil {
		return err
	}
	a.notifyMemoryWarehouseChanged(warehouseChangeFromEntries(previous, "deleted", ""))
	return nil
}

// DeleteBotMemories bulk-deletes bot-scoped long-term memory entries.
func (a *App) DeleteBotMemories(botID string, ids []string) (map[string]any, error) {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	deleted, skipped, err := a.memoryStore.DeleteBotMemories(desktopUserID, strings.TrimSpace(botID), ids)
	if err != nil {
		return nil, err
	}
	if deleted > 0 {
		a.notifyMemoryWarehouseChanged(memoryWarehouseChange{})
	}
	return map[string]any{"deleted": deleted, "skipped": skipped}, nil
}
