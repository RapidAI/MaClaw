package main

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Start-of-task desktop tidy. A new task used to start on top of whatever
// the previous one left in front (seen live: an Office task began with last
// task's Xiaohongshu page and a stale app in focus). Before the first
// execute-phase step of a run, app windows from earlier work are minimized.
// Nothing is closed, so unsaved work survives; the browser stays because
// CDP actions and screenshots need its window. MACLAW_DESKTOP_TIDY=off
// turns it off.

const desktopTidyScript = `export DISPLAY="${DISPLAY:-:20}"
command -v xdotool >/dev/null 2>&1 || exit 0
for w in $(xprop -root _NET_CLIENT_LIST 2>/dev/null | grep -o '0x[0-9a-f]*'); do
  c=$(xprop -id "$w" WM_CLASS 2>/dev/null | tr 'A-Z' 'a-z')
  case "$c" in *xfce4-panel*|*xfdesktop*|*chromium*|*chrome*|*fcitx*|*notif*) continue;; esac
  xdotool windowminimize "$w" 2>/dev/null && echo "minimized $w"
done
exit 0`

var (
	desktopTidied   sync.Map // run key -> time.Time
	desktopTidyBash = desktopContainerBash
)

func desktopTidyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_TIDY"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

func desktopTidyKey(scope agentruntime.Scope) string {
	run := strings.TrimSpace(scope.RunID)
	if run == "" {
		run = strings.TrimSpace(scope.SessionID)
	}
	if run == "" {
		return ""
	}
	return scope.TenantID + "/" + scope.UserID + "/" + run
}

// desktopTidyOnce minimizes leftover app windows the first time a run
// touches the desktop in the execute phase. Best effort: a failure is
// logged and never blocks the step.
func desktopTidyOnce(ctx context.Context, scope agentruntime.Scope, action string) {
	if !desktopTidyEnabled() || action == "deliver" {
		return
	}
	if phase, keyed := desktopPhase(ctx); !keyed || phase != "execute" {
		return
	}
	key := desktopTidyKey(scope)
	if key == "" {
		return
	}
	now := time.Now()
	if _, seen := desktopTidied.LoadOrStore(key, now); seen {
		return
	}
	desktopTidied.Range(func(k, v any) bool {
		if t, ok := v.(time.Time); ok && now.Sub(t) > 6*time.Hour {
			desktopTidied.Delete(k)
		}
		return true
	})
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := desktopTidyBash(tctx, scope, desktopTidyScript)
	n := strings.Count(out, "minimized ")
	if err != nil {
		log.Printf("[desktop-tidy] user=%s skipped: %v", scope.UserID, err)
		return
	}
	log.Printf("[desktop-tidy] user=%s minimized=%d", scope.UserID, n)
}
