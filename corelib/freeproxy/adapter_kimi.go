package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// KimiAdapter interacts with kimi.moonshot.cn via CDP.
type KimiAdapter struct{}

func (a *KimiAdapter) Name() string   { return "kimi" }
func (a *KimiAdapter) Domain() string { return "kimi.moonshot.cn" }

func (a *KimiAdapter) NewChatJS() string {
	return `(() => {
		const btn = document.querySelector('[data-testid="msh-sidebar-new-conversation"]') ||
			document.querySelector('a[href="/chat"]') ||
			document.querySelector('.new-chat-btn');
		if (btn) btn.click();
	})()`
}

func (a *KimiAdapter) SendMessage(ctx context.Context, cdp *CDPClient, tabID, message string, onToken func(string)) (string, error) {
	cdp.Evaluate(ctx, a.NewChatJS())
	time.Sleep(1 * time.Second)

	escaped := strings.ReplaceAll(message, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")

	injectJS := fmt.Sprintf(`(() => {
		const editor = document.querySelector('[data-testid="msh-chatinput-editor"]') ||
			document.querySelector('.chat-input-editor [contenteditable="true"]') ||
			document.querySelector('[contenteditable="true"]');
		if (!editor) return 'ERR:no_editor';
		editor.innerHTML = '<p>' + %q + '</p>';
		editor.dispatchEvent(new Event('input', {bubbles: true}));
		setTimeout(() => {
			const sendBtn = document.querySelector('[data-testid="msh-chatinput-send-button"]') ||
				document.querySelector('.chat-input-send-button') ||
				document.querySelector('button[aria-label="发送"]');
			if (sendBtn && !sendBtn.disabled) sendBtn.click();
		}, 300);
		return 'OK';
	})()`, escaped)

	result, err := cdp.Evaluate(ctx, injectJS)
	if err != nil {
		return "", fmt.Errorf("inject message: %w", err)
	}
	var status string
	json.Unmarshal(result, &status)
	if strings.HasPrefix(status, "ERR:") {
		return "", fmt.Errorf("Kimi: %s", status)
	}

	return pollResponse(ctx, cdp, onToken, `(() => {
		const msgs = document.querySelectorAll('[data-testid="msh-message-assistant-text"]') ||
			document.querySelectorAll('.message-assistant .message-text');
		const all = msgs.length > 0 ? msgs : document.querySelectorAll('.assistant-message');
		if (all.length === 0) return {done: false, text: ''};
		const last = all[all.length - 1];
		const text = last.innerText || '';
		const loading = document.querySelector('[data-testid="msh-chatinput-stop-button"]') ||
			document.querySelector('.stop-generating-btn');
		return {done: !loading, text: text};
	})()`)
}
