package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DoubaoAdapter interacts with doubao.com (豆包) via CDP.
type DoubaoAdapter struct{}

func (a *DoubaoAdapter) Name() string   { return "doubao" }
func (a *DoubaoAdapter) Domain() string { return "doubao.com" }

func (a *DoubaoAdapter) NewChatJS() string {
	return `(() => {
		const btn = document.querySelector('[data-testid="new_chat_button"]') ||
			document.querySelector('.new-conversation-btn') ||
			document.querySelector('a[href="/chat/new"]');
		if (btn) btn.click();
	})()`
}

func (a *DoubaoAdapter) SendMessage(ctx context.Context, cdp *CDPClient, tabID, message string, onToken func(string)) (string, error) {
	cdp.Evaluate(ctx, a.NewChatJS())
	time.Sleep(1 * time.Second)

	escaped := strings.ReplaceAll(message, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")

	injectJS := fmt.Sprintf(`(() => {
		const editor = document.querySelector('[contenteditable="true"]') ||
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
			const sendBtn = document.querySelector('[data-testid="send_button"]') ||
				document.querySelector('button[aria-label="发送"]') ||
				document.querySelector('.send-btn');
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
		return "", fmt.Errorf("Doubao: %s", status)
	}

	return pollResponse(ctx, cdp, onToken, `(() => {
		const msgs = document.querySelectorAll('.assistant-message-container') ||
			document.querySelectorAll('[data-testid="message_assistant"]');
		const all = msgs.length > 0 ? msgs : document.querySelectorAll('.bot-message');
		if (all.length === 0) return {done: false, text: ''};
		const last = all[all.length - 1];
		const text = last.innerText || '';
		const loading = document.querySelector('[data-testid="stop_button"]') ||
			document.querySelector('.stop-btn') ||
			document.querySelector('[aria-label="停止生成"]');
		return {done: !loading, text: text};
	})()`)
}
