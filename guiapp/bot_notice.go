package guiapp

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

// botNoticeApp is the running App, set at startup, so notice lines built in
// plain helpers can be written by the person's model.
var botNoticeApp atomic.Pointer[App]

// botNoticeWriter is replaced in tests.
var botNoticeWriter func(ctx context.Context, a *App, req DesktopBotAckRequest) (string, error)

func init() {
	botNoticeWriter = func(ctx context.Context, a *App, req DesktopBotAckRequest) (string, error) {
		return a.writeDesktopBotAck(ctx, req)
	}
}

// botNotice writes a user-facing notice in the bot's voice through the same
// attempts as the desktop ack (own model, own model with thinking, Hub). It
// never holds a flow longer than wait: past that, or when every attempt
// fails, it returns minimal. source is logged so every line is traceable.
func botNotice(source, kind, guidance, said, lang, minimal string, wait time.Duration) string {
	a := botNoticeApp.Load()
	if a == nil || botNoticeWriter == nil {
		log.Printf("[bot-notice] source=%s kind=%s no_app; minimal line", source, kind)
		return minimal
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := botNoticeWriter(ctx, a, DesktopBotAckRequest{Kind: kind, Message: said, Lang: lang, Instruction: guidance})
		done <- result{strings.TrimSpace(line), err}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.line == "" {
			log.Printf("[bot-notice] source=%s kind=%s model_failed err=%v; minimal line", source, kind, r.err)
			return minimal
		}
		log.Printf("[bot-notice] source=%s kind=%s ok len=%d", source, kind, len([]rune(r.line)))
		return r.line
	case <-ctx.Done():
		log.Printf("[bot-notice] source=%s kind=%s timeout=%s; minimal line", source, kind, wait)
		return minimal
	}
}

func botNoticeLang(text string) string {
	if sessionRoundTextHasHan(text) {
		return "zh-Hans"
	}
	return "en"
}
