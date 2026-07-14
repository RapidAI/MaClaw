package moa

import (
	"fmt"
	"strings"
)

const referenceSystemPrompt = `You are an advisor model in a multi-model council.
Analyze the user request and prior dialogue carefully.
Provide concise, concrete advice: key risks, alternatives, and recommended next steps.
Do not call tools. Do not invent file paths or credentials.
Keep the response under a few short paragraphs.`

// BuildReferenceMessages builds a tool-less dialogue view for advisors:
// short system + user/assistant text only (no tools / tool_calls).
func BuildReferenceMessages(conversation []interface{}) []interface{} {
	out := make([]interface{}, 0, len(conversation)+1)
	out = append(out, map[string]string{"role": "system", "content": referenceSystemPrompt})
	for _, m := range conversation {
		role, content, toolCalls, ok := normalizeMessage(m)
		if !ok {
			continue
		}
		switch role {
		case "user", "assistant":
			if role == "assistant" && toolCalls && strings.TrimSpace(content) == "" {
				continue
			}
			if strings.TrimSpace(content) == "" {
				continue
			}
			out = append(out, map[string]string{"role": role, "content": content})
		default:
			// system/tool/function skipped
		}
	}
	return out
}

func normalizeMessage(m interface{}) (role, content string, hasToolCalls bool, ok bool) {
	switch mm := m.(type) {
	case map[string]string:
		return mm["role"], mm["content"], false, true
	case map[string]interface{}:
		role, _ = mm["role"].(string)
		_, hasToolCalls = mm["tool_calls"]
		return role, messageTextContent(mm), hasToolCalls, true
	default:
		return "", "", false, false
	}
}

func messageTextContent(mm map[string]interface{}) string {
	switch c := mm["content"].(type) {
	case string:
		return c
	case []interface{}:
		var b strings.Builder
		for _, part := range c {
			pm, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := pm["type"].(string); t == "text" {
				if s, ok := pm["text"].(string); ok {
					if b.Len() > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(s)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

// RefAdvice is one advisor's labeled output for injection.
type RefAdvice struct {
	Label   string
	Content string
	Error   string
}

// FormatAdviceBlock builds the private tail text for the aggregator.
// Empty when there are no items (caller should skip inject).
func FormatAdviceBlock(items []RefAdvice) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n[Private multi-model council advice — not visible to the user]\n")
	for i, it := range items {
		name := strings.TrimSpace(it.Label)
		if name == "" {
			name = fmt.Sprintf("advisor-%d", i+1)
		}
		b.WriteString("\n### ")
		b.WriteString(name)
		b.WriteByte('\n')
		if it.Error != "" {
			b.WriteString("(advisor error: ")
			b.WriteString(sanitizeError(it.Error))
			b.WriteString(")\n")
			continue
		}
		content := strings.TrimSpace(it.Content)
		if content == "" {
			b.WriteString("(no content)\n")
			continue
		}
		b.WriteString(content)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	return b.String()
}

// InjectAdviceDeepCopy returns a new conversation slice where the last user
// message content has advice appended. Original maps/slices are not mutated.
func InjectAdviceDeepCopy(conversation []interface{}, advice string) []interface{} {
	if strings.TrimSpace(advice) == "" || len(conversation) == 0 {
		return conversation
	}
	out := make([]interface{}, len(conversation))
	copy(out, conversation)
	// Find last user message index
	lastUser := -1
	for i := len(out) - 1; i >= 0; i-- {
		role, _, _, ok := normalizeMessage(out[i])
		if ok && role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return out
	}
	switch src := out[lastUser].(type) {
	case map[string]string:
		clone := make(map[string]string, len(src)+1)
		for k, v := range src {
			clone[k] = v
		}
		clone["content"] = src["content"] + advice
		out[lastUser] = clone
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(src)+1)
		for k, v := range src {
			clone[k] = v
		}
		switch c := clone["content"].(type) {
		case string:
			clone["content"] = c + advice
		case []interface{}:
			parts := make([]interface{}, len(c)+1)
			copy(parts, c)
			parts[len(c)] = map[string]interface{}{"type": "text", "text": advice}
			clone["content"] = parts
		default:
			clone["content"] = strings.TrimSpace(fmt.Sprint(c)) + advice
		}
		out[lastUser] = clone
	}
	return out
}

func sanitizeError(err string) string {
	err = strings.TrimSpace(err)
	if err == "" {
		return "error"
	}
	if len(err) > 200 {
		err = err[:200] + "…"
	}
	// Avoid leaking tokens / credentials in advice and logs.
	lower := strings.ToLower(err)
	if strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "sk-") ||
		strings.Contains(lower, "x-api-key") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "secret") {
		return "auth_or_request_failed"
	}
	return err
}
