package guiapp

import (
	"errors"
	"testing"
)

func TestChatBotReplyUsesModelLineAndFallsBackOnlyOnFailure(t *testing.T) {
	orig := chatBotReplyWriter
	t.Cleanup(func() { chatBotReplyWriter = orig })
	h := &IMMessageHandler{app: &App{}}
	var got DesktopBotAckRequest
	chatBotReplyWriter = func(_ *App, req DesktopBotAckRequest) (string, error) {
		got = req
		return "  小助手还在收尾，停好告诉你。 ", nil
	}
	if line := h.chatBotReply("stopping", "停下", "zh-Hans", "test", "", "还在停。"); line != "小助手还在收尾，停好告诉你。" {
		t.Fatalf("line=%q", line)
	}
	if got.Kind != "stopping" || got.Message != "停下" {
		t.Fatalf("req=%#v", got)
	}
	chatBotReplyWriter = func(*App, DesktopBotAckRequest) (string, error) { return "", errors.New("all failed") }
	if line := h.chatBotReply("stopping", "停下", "zh-Hans", "test", "", "还在停。"); line != "还在停。" {
		t.Fatalf("fallback=%q", line)
	}
	if line := (*IMMessageHandler)(nil).chatBotReply("stopped", "", "", "test", "", "停了。"); line != "停了。" {
		t.Fatalf("nil handler=%q", line)
	}
}
