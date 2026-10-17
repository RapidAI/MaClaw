package codegenproxy

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// codexIdentityReplacements removes the sentences Codex CLI uses to name
// itself. Third-party hosts match those sentences and refuse the call.
// The rest of the prompt, including a user's own mention of Codex, stays.
var codexIdentityReplacements = []struct{ from, to string }{
	{
		"You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI.",
		"You are a coding agent running in the terminal.",
	},
	{
		"Within this context, Codex refers to the open-source agentic coding interface (not the old Codex language model built by OpenAI).",
		"",
	},
}

var (
	codexGPTIdentitySentence = regexp.MustCompile(`You are Codex, based on GPT-\d+(?:\.\d+)?(?:[-.][A-Za-z0-9.]+)?\. You are running as a coding agent in the Codex CLI on a user's computer\.`)
	codexExtraBlankLines     = regexp.MustCompile(`\n{3,}`)
	codexIdentityNeedles     = [][]byte{
		[]byte("Codex CLI"),
		[]byte("You are Codex"),
		[]byte("open-source agentic coding interface"),
	}
)

// StripCodexClientMarkers removes Codex CLI identity sentences from a chat
// body before it is sent to a third-party host. Official OpenAI and ChatGPT
// hosts keep the text, because that backend expects the Codex client prompt.
// Only system and developer text is rewritten. User and tool text is left
// alone so a project that mentions Codex is not edited.
//
// Bodies that do not contain an identity sentence are returned unchanged,
// including their original bytes.
func StripCodexClientMarkers(rawURL string, body []byte) []byte {
	if len(body) == 0 || upstreamKeepsCodexIdentity(rawURL) || !bodyHasCodexIdentity(body) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload map[string]any
	if dec.Decode(&payload) != nil || payload == nil {
		return body
	}
	changed := false
	if instructions, ok := payload["instructions"].(string); ok {
		if next := rewriteCodexIdentityText(instructions); next != instructions {
			payload["instructions"] = next
			changed = true
		}
	}
	if messages, ok := payload["messages"].([]any); ok {
		for _, item := range messages {
			msg, ok := item.(map[string]any)
			if !ok || !codexIdentityRole(msg["role"]) {
				continue
			}
			if rewriteCodexIdentityContent(msg) {
				changed = true
			}
		}
	}
	if !changed {
		return body
	}
	out, err := marshalCodexPayload(payload)
	if err != nil {
		return body
	}
	return out
}

func bodyHasCodexIdentity(body []byte) bool {
	for _, needle := range codexIdentityNeedles {
		if bytes.Contains(body, needle) {
			return true
		}
	}
	return false
}

func marshalCodexPayload(payload map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func upstreamKeepsCodexIdentity(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	return host == "openai.com" || host == "chatgpt.com" ||
		strings.HasSuffix(host, ".openai.com") || strings.HasSuffix(host, ".chatgpt.com")
}

func codexIdentityRole(role any) bool {
	switch strings.ToLower(strings.TrimSpace(stringField(role))) {
	case "system", "developer":
		return true
	default:
		return false
	}
}

func stringField(value any) string {
	text, _ := value.(string)
	return text
}

func rewriteCodexIdentityContent(msg map[string]any) bool {
	switch content := msg["content"].(type) {
	case string:
		next := rewriteCodexIdentityText(content)
		if next == content {
			return false
		}
		msg["content"] = next
		return true
	case []any:
		changed := false
		for i, part := range content {
			switch item := part.(type) {
			case string:
				next := rewriteCodexIdentityText(item)
				if next == item {
					continue
				}
				content[i] = next
				changed = true
			case map[string]any:
				text, ok := item["text"].(string)
				if !ok {
					continue
				}
				next := rewriteCodexIdentityText(text)
				if next == text {
					continue
				}
				item["text"] = next
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func rewriteCodexIdentityText(s string) string {
	next := s
	for _, item := range codexIdentityReplacements {
		next = strings.ReplaceAll(next, item.from, item.to)
	}
	next = codexGPTIdentitySentence.ReplaceAllString(next, "You are a coding agent running on the user's computer.")
	if next == s {
		return s
	}
	return codexExtraBlankLines.ReplaceAllString(next, "\n\n")
}
