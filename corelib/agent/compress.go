package agent

// Conversation compression helpers — standalone functions migrated from
// gui/im_compress.go as part of the agent-unification plan.
//
// These functions convert between conversation formats and provide
// auto-compression integration with the corelib/context.Compressor.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	ctxcompress "github.com/RapidAI/CodeClaw/corelib/context"
)

// AutoCompressConversation checks if the conversation should be auto-compressed
// before an LLM call. If compression is needed, it compresses the conversation
// in-place and returns the compressed version. Otherwise returns the original.
// If LLM-based compression fails, falls back to truncation-based compression
// to ensure the conversation stays within the context window.
func AutoCompressConversation(
	conversation []interface{},
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
) []interface{} {
	if len(conversation) <= 5 {
		return conversation
	}

	messages := InterfaceSliceToContextMessages(conversation)
	if len(messages) == 0 {
		return conversation
	}

	compressor := ctxcompress.NewCompressor(ctxcompress.CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: cfg.EffectiveContextTokens(),
	}, MakeSummarizeCallback(cfg, httpClient))

	if !compressor.ShouldCompress(messages) {
		return conversation
	}

	result, err := compressor.Compress(messages)
	if err != nil {
		log.Printf("[auto-compress] compression failed, falling back to truncation: %v", err)
		// Fallback: use TrimConversation to enforce the token budget.
		// This ensures the conversation doesn't exceed the context window
		// even when LLM-based summarization is unavailable.
		// EffectiveContextTokens() already reserves 20% for output,
		// so pass it directly as the token limit.
		return TrimConversation(conversation, cfg.EffectiveContextTokens(), 0, nil)
	}

	log.Printf("[auto-compress] %s", result.MarkerText)

	return ContextMessagesToInterfaceSlice(result.Messages)
}

// MakeSummarizeCallback creates an LLM summarization callback for the compressor.
func MakeSummarizeCallback(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) (string, error) {
	return func(text string) (string, error) {
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}

		runes := []rune(text)
		if len(runes) > 16000 {
			text = string(runes[:16000]) + "\n...(truncated)"
		}

		messages := []interface{}{
			map[string]string{
				"role":    "system",
				"content": "你是一个对话摘要助手。请将以下对话历史压缩为简洁的摘要，保留关键决策、工具调用结果、文件路径和代码片段。去除冗余的思考过程和重复的失败尝试。",
			},
			map[string]string{
				"role":    "user",
				"content": "请压缩以下对话历史:\n\n" + text,
			},
		}

		resp, err := DoSimpleLLMRequest(cfg, messages, httpClient, 30*time.Second)
		if err != nil {
			return "", err
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("empty summarization response")
		}
		return resp.Content, nil
	}
}

// ---------------------------------------------------------------------------
// Conversion helpers between conversation formats
// ---------------------------------------------------------------------------

// ConversationToContextMessages converts []ConversationEntry to []context.Message.
func ConversationToContextMessages(entries []ConversationEntry) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(entries))
	for _, e := range entries {
		content := EntryContentToString(e.Content)
		if content == "" && e.ToolCalls == nil {
			continue
		}
		messages = append(messages, ctxcompress.Message{
			Role:    e.Role,
			Content: content,
		})
	}
	return messages
}

// InterfaceSliceToContextMessages converts []interface{} (agent loop format)
// to []context.Message for the compressor.
func InterfaceSliceToContextMessages(msgs []interface{}) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(msgs))
	for _, m := range msgs {
		role, content := ExtractRoleContent(m)
		if role == "" {
			continue
		}
		messages = append(messages, ctxcompress.Message{
			Role:    role,
			Content: content,
		})
	}
	return messages
}

// ContextMessagesToConversation converts []context.Message back to []ConversationEntry.
func ContextMessagesToConversation(messages []ctxcompress.Message) []ConversationEntry {
	entries := make([]ConversationEntry, 0, len(messages))
	for _, m := range messages {
		entries = append(entries, ConversationEntry{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return entries
}

// ContextMessagesToInterfaceSlice converts []context.Message to []interface{}
// for the agent loop conversation format.
func ContextMessagesToInterfaceSlice(messages []ctxcompress.Message) []interface{} {
	result := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		result = append(result, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	return result
}

// EntryContentToString extracts a string representation from a ConversationEntry's Content field.
func EntryContentToString(content interface{}) string {
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

// ExtractRoleContent extracts role and content from an interface{} message.
func ExtractRoleContent(m interface{}) (string, string) {
	switch v := m.(type) {
	case map[string]interface{}:
		role, _ := v["role"].(string)
		switch c := v["content"].(type) {
		case string:
			return role, c
		case nil:
			return role, ""
		default:
			data, _ := json.Marshal(c)
			return role, string(data)
		}
	case map[string]string:
		return v["role"], v["content"]
	default:
		return "", ""
	}
}
