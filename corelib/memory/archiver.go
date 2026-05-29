package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ConversationEntry represents a single message in a conversation.
type ConversationEntry struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// LLMSummarizer abstracts the LLM call needed by the archiver and compressor
// so they don't depend on any concrete application struct.
type LLMSummarizer interface {
	// Summarize sends text to the LLM and returns a summary.
	Summarize(prompt string) (string, error)
	// IsConfigured reports whether the LLM backend is ready.
	IsConfigured() bool
}

// Archiver extracts key information from expiring conversations and stores
// them as summary memories via Store. It is a fallback/legacy summarization
// path: OnlineExtractor is the primary fact-write path, and Archiver should
// skip when online extraction has recently succeeded to avoid duplicate
// derived memories.
type Archiver struct {
	store      *Store
	summarizer LLMSummarizer
}

// NewArchiver creates an Archiver.
func NewArchiver(store *Store, summarizer LLMSummarizer) *Archiver {
	return &Archiver{
		store:      store,
		summarizer: summarizer,
	}
}

// Archive analyses the conversation entries and stores a summary memory. It
// skips trivial conversations, unconfigured LLMs, and periods where the
// OnlineExtractor has recently succeeded. Conversation summaries are legacy
// context/fallback records, not first-class evidence for normal recall.
func (a *Archiver) Archive(userID string, entries []ConversationEntry) error {
	if a == nil || a.store == nil {
		return nil
	}
	if len(entries) < 4 {
		return nil
	}

	if a.summarizer == nil || !a.summarizer.IsConfigured() {
		return nil
	}

	if a.store != nil {
		if oe := a.store.OnlineExtractor(); oe != nil && oe.HasRecentActivity(60*time.Minute) {
			return nil
		}
	}

	var convoBuilder strings.Builder
	for _, e := range entries {
		contentStr := formatEntryContent(e.Content)
		if contentStr == "" {
			continue
		}
		convoBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", e.Role, contentStr))
	}
	conversationText := convoBuilder.String()
	if strings.TrimSpace(conversationText) == "" {
		return nil
	}

	prompt := "Extract key information from the following conversation, including user preferences, decisions, important facts, and task progress. Use concise Chinese bullet points. If there is nothing worth remembering, reply with only: NONE.\n\nConversation:\n" + conversationText

	summary, err := a.summarizer.Summarize(prompt)
	if err != nil {
		return fmt.Errorf("conversation_archiver: llm call: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if IsEmptyConversationSummary(summary) {
		return nil
	}

	now := time.Now()

	// Extract meaningful tags from the summary content.
	expanded := ExpandQuery(summary)
	tags := []string{
		"conversation_summary",
		userID,
		now.Format("2006-01-02"),
	}
	for _, entity := range expanded.Entities {
		tags = append(tags, entity)
	}

	_, err = a.store.UpsertConversationSummary(ConversationSummaryUpsertOptions{
		Title:            "Conversation summary",
		Content:          summary,
		Tags:             tags,
		IdentityTagCount: 3,
		OwnerID:          userID,
	})
	return err
}

func formatEntryContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}
