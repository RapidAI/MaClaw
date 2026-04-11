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
// them as long-term memories via Store.
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

// Archive analyses the conversation entries and stores a summary as a
// long-term memory. Skips trivial conversations (< 4 entries) or when
// the LLM is not configured.
func (a *Archiver) Archive(userID string, entries []ConversationEntry) error {
	if len(entries) < 4 {
		return nil
	}

	if a.summarizer == nil || !a.summarizer.IsConfigured() {
		return nil
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

	prompt := "请从以下对话中提取关键信息，包括：用户偏好、决策结论、重要事实、任务进度（做了什么、还差什么）。" +
		"请用简洁的中文列出要点，不要包含无关信息。如果对话中没有值得记录的信息，请回复「无」。\n\n" +
		"对话内容：\n" + conversationText

	summary, err := a.summarizer.Summarize(prompt)
	if err != nil {
		return fmt.Errorf("conversation_archiver: llm call: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
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

	entry := Entry{
		Content:  summary,
		Category: CategoryConversationSummary,
		Tags:     tags,
	}
	return a.store.Save(entry)
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
