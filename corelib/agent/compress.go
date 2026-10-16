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

// CompressionHook keeps a long run's conversation inside the model's context
// window by trimming older rounds before every LLM request.
//
// Without it the conversation only ever grows: RunLoop appends each round and
// nothing prunes it, so a run of a hundred-plus iterations can push the request
// past the context window and burn a provider's period quota on re-sending the
// same history. The hook is the mid-loop counterpart to the per-turn trimming
// hosts already do between turns.
//
// It trims rather than rewrites: TrimConversation works on the loop's native
// []interface{} format and drops whole entry groups, so an assistant message
// carrying tool_calls is never separated from the tool results that answer it.
// The ctxcompress path in AutoCompressConversation cannot be used here — its
// Message type holds only role and content, so round-tripping through it would
// strip tool_calls and tool_call_id and break the pairing.
type CompressionHook struct {
	// GetConfig returns the LLM config in force for the current iteration. It is
	// called every round because hosts may escalate the model mid-loop, and the
	// context window decides when trimming must kick in.
	GetConfig func() corelib.MaclawLLMConfig
	// Client carries the HTTP client used when Summarize is enabled.
	Client *http.Client
	// ToolsTokens estimates what the tool definitions cost, which is subtracted
	// from the message budget. Zero is the safe default: it trims a little
	// earlier than strictly necessary rather than letting the budget go
	// negative.
	ToolsTokens int
	// Summarize replaces dropped rounds with an LLM-written summary instead of
	// the structural handoff. It costs one extra LLM call per compression and
	// can itself fail when the provider is throttling, so it stays off unless a
	// host opts in.
	Summarize bool

	summarizer func(string) string
}

// NewCompressionHook builds a hook that reads its config from getConfig.
func NewCompressionHook(getConfig func() corelib.MaclawLLMConfig, client *http.Client) *CompressionHook {
	return &CompressionHook{GetConfig: getConfig, Client: client}
}

// TransformConversation trims the conversation when it has outgrown the budget.
// Returning nil leaves it untouched, per the LoopHooks contract, so the common
// case — a conversation still comfortably inside the window — allocates nothing.
func (h *CompressionHook) TransformConversation(conversation []interface{}) []interface{} {
	if h == nil || h.GetConfig == nil || len(conversation) == 0 {
		return nil
	}
	cfg := h.GetConfig()
	limit := cfg.EffectiveContextTokens()
	if limit <= 0 {
		return nil
	}
	// TrimConversation re-checks the budget, but estimating first keeps the
	// hot path — every iteration of every run — from paying for a fold and a
	// group scan when there is nothing to drop.
	if EstimateConversationTokens(conversation) <= h.budget(limit) {
		return nil
	}
	before := EstimateConversationTokens(conversation)
	var summarizer func(string) string
	if h.Summarize {
		summarizer = h.summarizerFor(cfg)
	}
	trimmed := TrimConversation(conversation, limit, h.ToolsTokens, summarizer)
	if len(trimmed) == len(conversation) {
		return nil
	}
	log.Printf("[auto-compress] mid-loop trim %d→%d tokens (%d→%d messages)",
		before, EstimateConversationTokens(trimmed), len(conversation), len(trimmed))
	return trimmed
}

// budget mirrors the floor TrimConversation applies, so the pre-check agrees
// with what the trim will actually enforce.
func (h *CompressionHook) budget(limit int) int {
	msgBudget := limit - h.ToolsTokens
	if msgBudget < 4000 {
		msgBudget = 4000
	}
	return msgBudget
}

func (h *CompressionHook) summarizerFor(cfg corelib.MaclawLLMConfig) func(string) string {
	if h.summarizer == nil {
		h.summarizer = MakeSummarizer(cfg, h.Client)
	}
	return h.summarizer
}

// OnToolExecuted is a no-op, implemented rather than embedded because a hook
// type must satisfy the whole interface.
func (h *CompressionHook) OnToolExecuted(string, string, string, bool) {}

// OnEmptyResponse keeps the loop's default: return false, which exits.
func (h *CompressionHook) OnEmptyResponse(int) bool { return false }

// AutoCompressConversation checks if the conversation should be auto-compressed
// before an LLM call. If compression is needed, it compresses the conversation
// in-place and returns the compressed version. Otherwise returns the original.
// If LLM-based compression fails, falls back to truncation-based compression
// to ensure the conversation stays within the context window.
//
// It targets per-turn callers working on plain chat history. Do not use it on a
// tool-calling loop's live conversation: the ctxcompress Message type carries
// only role and content, so the round trip drops tool_calls and tool_call_id
// and leaves the model with tool results it cannot pair. Use CompressionHook,
// which trims through the loop's own format instead.
func AutoCompressConversation(
	conversation []interface{},
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
) []interface{} {
	if len(conversation) <= 5 {
		return conversation
	}
	if conversationHasPendingAsk(conversation) {
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
		if resp == nil || !summaryAccepted(resp.Content) {
			return "", fmt.Errorf("incomplete summarization response")
		}
		return strings.TrimSpace(resp.Content), nil
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
