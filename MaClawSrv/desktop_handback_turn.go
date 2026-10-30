package main

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A turn whose message is the person's "done" (the chat's 登录完成，继续, or
// Hub's hand-back text) hands the bot's display back by itself. Before, the
// chat button only sent the message: the takeover stayed open in the
// supervisor, every desktop action yielded with 用户正在接管这个桌面, and
// the person also had to press 交还 on the desktop (papers 2026-10-31 05:5x).
// One click is the hand-back: the first desktop action of such a turn ends
// the takeover, then the bot continues on the same page.

var desktopDoneMessageRe = regexp.MustCompile(`(?i)登录或验证已在当前桌面浏览器完成|沿用这个登录状态继续|登录完成，继续|登入完成，繼續|login done, continue|桌面交还给你|请继续刚才的任务|hand(ed)? (the desktop )?back`)

var (
	desktopHandBackTurnMu sync.Mutex
	desktopHandBackTurns  = map[string]time.Time{}
)

// desktopHandBackOnDoneTurn ends an open takeover once per run when the
// turn's message says the person is done. It reports whether it did.
func desktopHandBackOnDoneTurn(ctx context.Context, request agentruntime.TurnRequest, scope agentruntime.Scope) bool {
	if !desktopIsolatedBot(scope) {
		return false
	}
	content, _ := desktopTurnMessage(request)
	if !desktopDoneMessageRe.MatchString(strings.TrimSpace(content)) {
		return false
	}
	key := scope.TenantID + "\x00" + scope.UserID + "\x00" + strings.TrimSpace(scope.RunID)
	if strings.TrimSpace(scope.RunID) == "" {
		key += "\x00" + strings.TrimSpace(scope.SessionID)
	}
	now := time.Now()
	desktopHandBackTurnMu.Lock()
	for k, at := range desktopHandBackTurns {
		if now.Sub(at) > time.Hour {
			delete(desktopHandBackTurns, k)
		}
	}
	if _, done := desktopHandBackTurns[key]; done {
		desktopHandBackTurnMu.Unlock()
		return false
	}
	desktopHandBackTurns[key] = now
	desktopHandBackTurnMu.Unlock()
	if _, err := desktopCtlCall(ctx, scope, "", map[string]any{"op": desktop.CtlTakeoverEnd}); err != nil {
		desktopHandBackTurnMu.Lock()
		delete(desktopHandBackTurns, key) // try again on the next action
		desktopHandBackTurnMu.Unlock()
		return false
	}
	return true
}
