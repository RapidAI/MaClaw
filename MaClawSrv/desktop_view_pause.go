package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// With MACLAW_DEMO_SKILL on, a person on the bot's display (takeover, recent
// input lease, or the owner's open desktop view) pauses the bot's desktop
// action instead of ending the turn. The bot polls with growing intervals,
// and when the person leaves it does NOT run the stale action: it is told to
// screenshot and replan, because the person may have changed the screen.
var (
	desktopPauseIntervals = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
	desktopPauseMax       = 15 * time.Minute
)

const desktopResumeNote = "用户刚才在这个桌面上查看或操作，现已离开，Bot 已自动恢复。上一个动作没有执行：画面可能已被用户改变，先截图观察当前状态，再按任务意图重新规划下一步，继续推进到交付结果。"

const desktopStillHeldNote = "用户仍在这个桌面上查看或操作（已等待 %d 秒）。上一个动作没有执行：先做不需要桌面的部分，稍后再截图查看并继续；不要结束任务。"

// desktopPersonPresent reads the display state once.
func desktopPersonPresent(ctx context.Context, scope agentruntime.Scope) (bool, string) {
	req := map[string]any{"op": desktop.CtlBotState}
	if _, bot := desktop.SplitDesktopUser(scope.UserID); bot != "" {
		req["srv_bot"] = bot
	}
	if scope.RunID != "" {
		req["srv_run"] = scope.RunID
	}
	out, err := desktopCtlCall(ctx, scope, "", req)
	if err != nil {
		return false, ""
	}
	lease, _ := out["lease"].(string)
	yield, _ := out["yield"].(bool)
	takeover, _ := out["takeover"].(bool)
	viewer, _ := out["viewer"].(bool)
	switch {
	case takeover:
		return true, "takeover"
	case yield || lease == "human":
		return true, "input"
	case viewer:
		return true, "viewer"
	}
	return false, ""
}

// desktopWaitForPerson returns "" when the action may run now, or a result
// text for the model when it paused (resumed, or still held after the cap).
// It never returns an error for a person on the display: the run goes on.
func desktopWaitForPerson(ctx context.Context, scope agentruntime.Scope) (string, error) {
	if !desktopIsolatedBot(scope) {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	present, why := desktopPersonPresent(ctx, scope)
	if !present {
		return "", nil
	}
	started := time.Now()
	log.Printf("[desktop] pause for person reason=%s run=%s", why, scope.RunID)
	desktopEmitNote(scope, "takeover_pause", "bot", "用户正在查看或操作桌面，Bot 暂停桌面动作，等待用户离开")
	for i := 0; ; i++ {
		wait := desktopPauseIntervals[len(desktopPauseIntervals)-1]
		if i < len(desktopPauseIntervals) {
			wait = desktopPauseIntervals[i]
		}
		if time.Since(started)+wait > desktopPauseMax {
			return fmt.Sprintf(desktopStillHeldNote, int(time.Since(started).Seconds())), nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
		if present, _ = desktopPersonPresent(ctx, scope); !present {
			log.Printf("[desktop] resume after %s run=%s", time.Since(started).Round(time.Second), scope.RunID)
			desktopEmitNote(scope, "takeover_resume", "bot", "用户已离开桌面，Bot 自动恢复并重新观察画面")
			return desktopResumeNote, nil
		}
	}
}
