package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// GeminiAdapter interacts with gemini.google.com via CDP.
type GeminiAdapter struct{}

func (a *GeminiAdapter) Name() string   { return "gemini" }
func (a *GeminiAdapter) Domain() string { return "gemini.google.com" }

func (a *GeminiAdapter) NewChatJS() string {
	return `(() => {
		const btn = document.querySelector('a[href="/app"]') ||
			document.querySelector('[data-test-id="new-chat-button"]') ||
			document.querySelector('button[aria-label="New chat"]');
		if (btn) btn.click();
	})()`
}

func (a *GeminiAdapter) SendMessage(ctx context.Context, cdp *CDPClient, tabID, message string, onToken func(string)) (string, error) {
	cdp.Evaluate(ctx, a.NewChatJS())
	time.Sleep(1 * time.Second)

	escaped := strings.ReplaceAll(message, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")

	injectJS := fmt.Sprintf(`(() => {
		// Gemini uses a rich text editor (contenteditable or textarea)
		const editor = document.querySelector('.ql-editor') ||
			document.querySelector('[contenteditable="true"]') ||
			document.querySelector('textarea');
		if (!editor) return 'ERR:no_editor';
		if (editor.tagName === 'TEXTAREA') {
			const nativeSet = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
			nativeSet.call(editor, %q);
			editor.dispatchEvent(new Event('input', {bubbles: true}));
		} else {
			editor.innerHTML = '<p>' + %q + '</p>';
			editor.dispatchEvent(new Event('input', {bubbles: true}));
		}
		setTimeout(() => {
			const sendBtn = document.querySelector('[aria-label="Send message"]') ||
				document.querySelector('button.send-button') ||
				document.querySelector('[data-test-id="send-button"]') ||
				document.querySelector('mat-icon[data-mat-icon-name="send"]')?.closest('button');
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
		return "", fmt.Errorf("Gemini: %s", status)
	}

	return pollResponse(ctx, cdp, onToken, `(() => {
		// Gemini renders model responses in message-content containers
		const responses = document.querySelectorAll('model-response message-content');
		if (responses.length === 0) {
			// Fallback: look for .model-response-text
			const alt = document.querySelectorAll('.model-response-text');
			if (alt.length === 0) return {done: false, text: ''};
			const last = alt[alt.length - 1];
			const loading = document.querySelector('.loading-indicator') ||
				document.querySelector('[aria-label="Gemini is thinking"]');
			return {done: !loading, text: last.innerText || ''};
		}
		const last = responses[responses.length - 1];
		const text = last.innerText || '';
		const loading = document.querySelector('.loading-indicator') ||
			document.querySelector('[aria-label="Gemini is thinking"]');
		return {done: !loading, text: text};
	})()`)
}
