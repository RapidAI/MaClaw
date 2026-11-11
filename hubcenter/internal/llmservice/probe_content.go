package llmservice

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"sync"
)

// unknownProbeBlockTypesSeen dedupes the unknown-block-type log line per type
// per process, so a stream that repeats an unrecognized block does not write
// one line per chunk.
var unknownProbeBlockTypesSeen sync.Map

func logUnknownProbeBlockType(typ string) {
	if typ == "" {
		return
	}
	if _, seen := unknownProbeBlockTypesSeen.LoadOrStore(typ, struct{}{}); !seen {
		log.Printf("[llm-probe] ignoring unknown openai content block type %q", typ)
	}
}

// foldOpenAIProbeContent folds one OpenAI `content` value — a message.content
// reply or a delta.content chunk — into visible text and reasoning text. A
// string returns as-is and reports normalized=false. An array folds its
// blocks in order: text blocks (text, input_text, output_text) join the
// reply, thinking/reasoning blocks join the reasoning lane, and an unknown
// block type is skipped with one log line. Parts inside one array join with
// "\n" — the same separator corelib's normalizeOpenAIMessageContent and
// openAIDeltaContent use — so the probe reply matches what a client sees.
// Anything else counts as empty.
func foldOpenAIProbeContent(raw any) (text, reasoning string, normalized bool) {
	switch v := raw.(type) {
	case nil:
		return "", "", false
	case string:
		return v, "", false
	}
	items, ok := raw.([]any)
	if !ok {
		return "", "", false
	}
	var textParts, thinkParts []string
	for _, item := range items {
		block, _ := item.(map[string]any)
		if block == nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(probeContentString(block["type"])))
		switch typ {
		case "text", "input_text", "output_text":
			if s := probeFirstString(block, "text", "content"); s != "" {
				textParts = append(textParts, s)
			}
		case "thinking", "reasoning", "reasoning_content":
			// Mistral nests the fragments:
			// {"type":"thinking","thinking":[{"type":"text","text":"..."}]}
			if inner, ok := block["thinking"].([]any); ok && len(inner) > 0 {
				for _, part := range inner {
					partMap, _ := part.(map[string]any)
					if s := probeFirstString(partMap, "text", "content"); s != "" {
						thinkParts = append(thinkParts, s)
					}
				}
				continue
			}
			if s := probeFirstString(block, "thinking", "reasoning", "reasoning_content", "text", "content"); s != "" {
				thinkParts = append(thinkParts, s)
			}
		default:
			logUnknownProbeBlockType(typ)
		}
	}
	return strings.Join(textParts, "\n"), strings.Join(thinkParts, "\n"), true
}

// ProviderTestContentNormalized reports whether a non-stream test reply put
// its OpenAI message.content as an array that had to be folded into text.
// Anthropic and Google bodies have no choices[].message, so they read false:
// blocks are their native shape, not a normalization.
func ProviderTestContentNormalized(body []byte) bool {
	var envelope struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Choices) == 0 {
		return false
	}
	// Only the first choice feeds the probe reply — matching hopTestReply — so
	// only it decides whether normalization happened.
	raw := bytes.TrimSpace(envelope.Choices[0].Message.Content)
	return len(raw) > 0 && raw[0] == '['
}

func probeContentString(raw any) string {
	s, _ := raw.(string)
	return s
}

func probeFirstString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
