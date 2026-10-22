package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopBotMetadataKey is set by Hub on an isolated bot's instance
// (desktop_mode=isolated). That bot drives its own display, so its desktop
// user is the owner plus the bot id; every per-desktop lock, hold, and
// handoff here is then that bot's alone.
const desktopBotMetadataKey = "hub_desktop_bot"

func desktopBotUser(owner string, metadata map[string]string) string {
	bot := strings.TrimSpace(metadata[desktopBotMetadataKey])
	if bot == "" || !desktop.ValidBotID(bot) {
		owner, _ = desktop.SplitDesktopUser(owner)
		return owner
	}
	return desktop.BotDesktopUser(owner, bot)
}

func desktopIsolatedBot(scope agentruntime.Scope) bool {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	return bot != ""
}

// requestDesktopTakeover asks the person to do what the bot must not (log
// in, 2FA, captcha). The bot's display is marked as taken over, so its own
// later actions yield, and the reply carries the usual handoff: the GUI shows
// the noVNC view of this bot's display and a hand back button.
func requestDesktopTakeover(ctx context.Context, scope agentruntime.Scope, reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("request_takeover requires reason")
	}
	if len([]rune(reason)) > 300 {
		reason = string([]rune(reason)[:300])
	}
	if desktopIsolatedBot(scope) {
		if _, err := desktopCtlCall(ctx, scope, "", map[string]any{"op": desktop.CtlTakeoverBegin, "reason": reason}); err != nil {
			return "", fmt.Errorf("request_takeover failed: %w", err)
		}
	}
	desktopEmitNote(scope, "takeover_request", "human", reason)
	noteDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID, reason)
	noteDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID)
	return "Takeover requested. The person now sees this desktop with your reason: " + reason +
		". Do not use the desktop any more in this turn. End your turn now with one short message telling them what to do and to press hand back (交还) when finished; you continue after that.", nil
}

// desktopBotYield stops an isolated bot's desktop action while the person
// has its display: a takeover is open, or someone typed or clicked there in
// the last seconds (auto-yield).
func desktopBotYield(ctx context.Context, scope agentruntime.Scope) error {
	if !desktopIsolatedBot(scope) {
		return nil
	}
	out, err := desktopCtlCall(ctx, scope, "", map[string]any{"op": desktop.CtlBotState})
	if err != nil {
		return nil // older supervisor or no display yet: the action itself will tell
	}
	// lease is the supervisor's input lease between the person and the bot
	// ("human" or "bot"); an older supervisor only reports yield.
	lease, _ := out["lease"].(string)
	desktopNoteLease(scope, lease, out)
	if yield, _ := out["yield"].(bool); !yield && lease != "human" {
		return nil
	}
	if takeover, _ := out["takeover"].(bool); takeover {
		return fmt.Errorf("用户正在接管这个桌面（%v），请等用户交还后再操作；现在结束本轮并告诉用户完成后点交还", out["reason"])
	}
	return fmt.Errorf("用户刚刚在这个桌面上输入，正在操作；请暂停桌面动作，结束本轮并告诉用户完成后点交还")
}
