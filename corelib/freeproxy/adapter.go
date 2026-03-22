// Package freeproxy implements a local OpenAI-compatible proxy that forwards
// LLM requests to free AI web interfaces (ChatGPT, Gemini, Kimi, Doubao)
// via Chrome DevTools Protocol (CDP).
package freeproxy

import "context"

// Adapter defines the interface for a web AI provider adapter.
// Each adapter knows how to interact with a specific AI website
// through CDP (injecting JS, reading responses, etc.).
type Adapter interface {
	// Name returns the adapter identifier (e.g. "chatgpt", "gemini").
	Name() string

	// Domain returns the website domain used to find/create the tab.
	Domain() string

	// SendMessage sends a user message and streams back the response.
	// The onToken callback is called for each chunk of text received.
	// It blocks until the full response is collected or ctx is cancelled.
	SendMessage(ctx context.Context, cdp *CDPClient, tabID string, message string, onToken func(string)) (string, error)

	// NewChatJS returns JavaScript that navigates to a new chat session.
	NewChatJS() string
}

// Registry holds all registered adapters keyed by name.
var Registry = map[string]Adapter{}

func register(a Adapter) { Registry[a.Name()] = a }

func init() {
	register(&ChatGPTAdapter{})
	register(&GeminiAdapter{})
	register(&KimiAdapter{})
	register(&DoubaoAdapter{})
}

// DefaultAdapter is the adapter used when model name doesn't match any known adapter.
const DefaultAdapter = "chatgpt"

// ResolveAdapter returns the adapter for the given model name.
// Model name mapping: "chatgpt"→ChatGPT, "gemini"→Gemini, "kimi"→Kimi, "doubao"→Doubao.
// Unknown models fall back to DefaultAdapter.
func ResolveAdapter(model string) Adapter {
	if a, ok := Registry[model]; ok {
		return a
	}
	return Registry[DefaultAdapter]
}
