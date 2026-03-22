package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ChatGPTAdapter interacts with chatgpt.com via CDP.
type ChatGPTAdapter struct{}

func (a *ChatGPTAdapter) Name() string   { return "chatgpt" }
func (a *ChatGPTAdapter) Domain() string { return "chatgpt.com" }

func (a *ChatGPTAdapter) NewChatJS() string {
	return `(() => {
		const btn = document.querySelector('a[href="/"]');
		if (btn) btn.click();
	})()`
}

func (a *ChatGPTAdapter) SendMessage(ctx context.Context, cdp *CDPClient, tabID, message string, onToken func(string)) (string, error) {
	// Start a new chat to avoid context pollution
	cdp.Evaluate(ctx, a.NewChatJS())
	time.Sleep(1 * time.Second)

	// Inject the message into the textarea and submit
	escaped := strings.ReplaceAll(message, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")

	injectJS := fmt.Sprintf(`(() => {
		const textarea = document.querySelector('#prompt-textarea');
		if (!textarea) return 'ERR:no_textarea';
		// Use the ProseMirror or contenteditable approach
		if (textarea.tagName === 'TEXTAREA') {
			const nativeSet = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
			nativeSet.call(textarea, %q);
			textarea.dispatchEvent(new Event('input', {bubbles: true}));
		} else {
			// contenteditable div (newer ChatGPT UI)
			textarea.innerHTML = '<p>' + %q + '</p>';
			textarea.dispatchEvent(new Event('input', {bubbles: true}));
		}
		// Click send button after a short delay
		setTimeout(() => {
			const sendBtn = document.querySelector('[data-testid="send-button"]') ||
				document.querySelector('button[aria-label="Send prompt"]') ||
				document.querySelector('form button[type="submit"]');
			if (sendBtn && !sendBtn.disabled) sendBtn.click();
		}, 300);
		return 'OK';
	})()`, escaped, escaped)

	result, err := cdp.Evaluate(ctx, injectJS)
	if err != nil {
		return "", fmt.Errorf("inject message: %w", err)
	}
	var status string
	json.Unmarshal(result, &status)
	if strings.HasPrefix(status, "ERR:") {
		return "", fmt.Errorf("ChatGPT: %s", status)
	}

	// Poll for the response
	return pollResponse(ctx, cdp, onToken, `(() => {
		const turns = document.querySelectorAll('[data-message-author-role="assistant"]');
		if (turns.length === 0) return {done: false, text: ''};
		const last = turns[turns.length - 1];
		const text = last.innerText || '';
		// Check if still generating
		const stopBtn = document.querySelector('[data-testid="stop-button"]') ||
			document.querySelector('[aria-label="Stop generating"]');
		return {done: !stopBtn, text: text};
	})()`)
}
