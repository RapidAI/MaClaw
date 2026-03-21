package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// DiscussionHistory holds persisted discussion summaries for a user.
type DiscussionHistory struct {
	Entries []DiscussionSummaryEntry `json:"entries"`
}

// DiscussionSummaryEntry is one discussion's summary.
type DiscussionSummaryEntry struct {
	Topic      string   `json:"topic"`
	Devices    []string `json:"devices"`
	Consensus  []string `json:"consensus"`
	Divergence []string `json:"divergence"`
	Pending    []string `json:"pending"`
	Timestamp  int64    `json:"timestamp"`
}

const maxDiscussionHistoryEntries = 20

// SystemSettingsStore is the interface needed for discussion history persistence.
type SystemSettingsStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// SaveDiscussionSummary appends a discussion summary to the user's history.
// Keeps at most 20 entries (FIFO eviction).
func SaveDiscussionSummary(ctx context.Context, store SystemSettingsStore, userID string, entry DiscussionSummaryEntry) {
	key := fmt.Sprintf("discussion_history_%s", userID)
	history := loadDiscussionHistory(ctx, store, key)

	history.Entries = append(history.Entries, entry)
	if len(history.Entries) > maxDiscussionHistoryEntries {
		history.Entries = history.Entries[len(history.Entries)-maxDiscussionHistoryEntries:]
	}

	data, _ := json.Marshal(history)
	if err := store.Set(ctx, key, string(data)); err != nil {
		log.Printf("[DiscussionHistory] save failed for user=%s: %v", userID, err)
	}
}

// LoadRelevantHistory retrieves discussion history entries that might be
// relevant to the given topic. For now, returns the most recent 3 entries
// (simple recency-based retrieval; could be upgraded to semantic search).
func LoadRelevantHistory(ctx context.Context, store SystemSettingsStore, userID string) []DiscussionSummaryEntry {
	key := fmt.Sprintf("discussion_history_%s", userID)
	history := loadDiscussionHistory(ctx, store, key)
	if len(history.Entries) == 0 {
		return nil
	}
	// Return last 3 entries.
	start := len(history.Entries) - 3
	if start < 0 {
		start = 0
	}
	return history.Entries[start:]
}

// FormatHistorySummary converts history entries into a text block for LLM context.
func FormatHistorySummary(entries []DiscussionSummaryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var result string
	for _, e := range entries {
		t := time.Unix(e.Timestamp, 0).Format("2006-01-02 15:04")
		result += fmt.Sprintf("【%s】%s\n", t, e.Topic)
		if len(e.Consensus) > 0 {
			result += "  共识: " + joinTruncated(e.Consensus, 100) + "\n"
		}
		if len(e.Divergence) > 0 {
			result += "  分歧: " + joinTruncated(e.Divergence, 100) + "\n"
		}
	}
	return result
}

func joinTruncated(items []string, maxRunes int) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += "; "
		}
		result += item
		if len([]rune(result)) > maxRunes {
			runes := []rune(result)
			result = string(runes[:maxRunes]) + "…"
			break
		}
	}
	return result
}

func loadDiscussionHistory(ctx context.Context, store SystemSettingsStore, key string) DiscussionHistory {
	raw, err := store.Get(ctx, key)
	if err != nil || raw == "" {
		return DiscussionHistory{}
	}
	var h DiscussionHistory
	if json.Unmarshal([]byte(raw), &h) != nil {
		return DiscussionHistory{}
	}
	return h
}
