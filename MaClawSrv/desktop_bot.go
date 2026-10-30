package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
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

// A takeover only so the person clicks 发布 is not needed once publishing is
// approved for this task: the bot finishes with action=publish itself.
var desktopPublishOnlyTakeoverRe = regexp.MustCompile(`(?i)(亲自|手动|帮忙|自行)?\s*[点按](击|下)?[^。，,.]{0,16}?[「“"']?(发布|发表|发送)|click (the )?(publish|post|submit)`)

// desktopElementMissingRe: a takeover asked because the bot could not find a
// control. 10-31 05:55 Xiaohongshu: 「页面里我拿不到"发布"按钮的元素」 then a
// takeover the person saw as a login prompt, while a site overlay covered the
// button. Missing elements are not a login wall.
var desktopElementMissingRe = regexp.MustCompile(`(?i)找不到|拿不到|定位不到|没找到|未找到|看不到|被(遮挡|挡住|覆盖)|弹窗|浮层|not found|can(no|')t find|could not find|no element|covered|overlay|popup`)

const desktopElementMissingRefusal = "refused: takeover_for_missing_element — a control you cannot find is not a login wall. A site popup or overlay (tips, 效率工具/标签 guide, cookie banner, dialog) often covers it: press Escape, click its 关闭/×/我知道了/跳过 (a #topic or @mention dropdown opened by typing closes with Escape or a click on a blank area), scroll, then probe the CURRENT page again without a filter. Do not navigate or reload; the filled content stays. Ask for a takeover only with positive evidence on the page of a login form, QR code, captcha or verification."

var desktopPublishLoginRe = regexp.MustCompile(`(?i)登录|验证码|扫码|验证|captcha|log ?in|sign ?in|2fa`)

const desktopPublishTakeoverRefusal = "refused: takeover_for_publish — publishing is approved for this task, so do not ask the person to click 发布. The editor is open: finish with ONE call action=publish (title, body, and upload/verify params as needed); it fills, clicks publish, confirms and reads the post back. Only ask for a takeover for a login, captcha or verification."

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
	if desktopPublishOnlyTakeoverRe.MatchString(reason) && !desktopPublishLoginRe.MatchString(reason) && desktopPublishAllowed(ctx) {
		return "", fmt.Errorf("%s", desktopPublishTakeoverRefusal)
	}
	if desktopElementMissingRe.MatchString(reason) && !desktopPublishLoginRe.MatchString(reason) {
		return "", fmt.Errorf("%s", desktopElementMissingRefusal)
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
	req := map[string]any{"op": desktop.CtlBotState}
	// The supervisor tags its events with these so they join the run file.
	if _, bot := desktop.SplitDesktopUser(scope.UserID); bot != "" {
		req["srv_bot"] = bot
	}
	if scope.RunID != "" {
		req["srv_run"] = scope.RunID
	}
	out, err := desktopCtlCall(ctx, scope, "", req)
	if err != nil {
		return nil // older supervisor or no display yet: the action itself will tell
	}
	// lease is the supervisor's input lease between the person and the bot
	// ("human" or "bot"); an older supervisor only reports yield.
	lease, _ := out["lease"].(string)
	desktopNoteAutoHandback(scope, out)
	desktopNoteLease(scope, lease, out)
	if yield, _ := out["yield"].(bool); !yield && lease != "human" {
		return nil
	}
	if takeover, _ := out["takeover"].(bool); takeover {
		return fmt.Errorf("用户正在接管这个桌面（%v），请等用户交还后再操作；现在结束本轮并告诉用户完成后点交还", out["reason"])
	}
	return fmt.Errorf("用户刚刚在这个桌面上输入，正在操作；请暂停桌面动作，结束本轮并告诉用户完成后点交还")
}

// desktopNoteAutoHandback shows in the feed that a human takeover ended by
// itself after the supervisor saw no person input for its idle limit.
func desktopNoteAutoHandback(scope agentruntime.Scope, out map[string]any) {
	if handed, _ := out["auto_handback"].(bool); !handed {
		return
	}
	idle := 0
	if v, ok := out["auto_handback_idle"].(float64); ok {
		idle = int(v)
	}
	log.Printf("[desktop] takeover auto hand back after %ds without human input", idle)
	desktopEmitNote(scope, "takeover_auto_handback", "bot", fmt.Sprintf("人类接管 %d 秒无操作，已自动交还给 Bot", idle))
}
