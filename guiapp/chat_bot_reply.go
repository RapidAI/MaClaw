package guiapp

import (
	"log"
	"strings"
)

// chatBotReplyWriter is replaced in tests.
var chatBotReplyWriter func(a *App, req DesktopBotAckRequest) (string, error)

func init() {
	chatBotReplyWriter = func(a *App, req DesktopBotAckRequest) (string, error) {
		return a.WriteDesktopBotAck(req)
	}
}

// chatBotReply writes a stop or follow-up line for the chat-app (IM) path in
// the bot's own voice, through the same attempts as the desktop ack: the
// person's own model, that model again with thinking on, then the Hub. The
// minimal line is used only when all of them fail. Callers reach this only
// after the person asked for the stop or sent the follow-up. source names
// what triggered it and is logged so every stop can be traced.
func (h *IMMessageHandler) chatBotReply(kind, said, lang, source, task, minimal string) string {
	log.Printf("[chat-stop] source=%s kind=%s said_len=%d", source, kind, len([]rune(strings.TrimSpace(said))))
	if h == nil || h.app == nil {
		return minimal
	}
	line, err := chatBotReplyWriter(h.app, DesktopBotAckRequest{
		Kind:        kind,
		Message:     said,
		Lang:        lang,
		RunningTask: task,
	})
	line = strings.TrimSpace(line)
	if err != nil || line == "" {
		log.Printf("[chat-stop] source=%s kind=%s model_failed err=%v; minimal line", source, kind, err)
		return minimal
	}
	return line
}
