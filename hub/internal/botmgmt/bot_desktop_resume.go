package botmgmt

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A bot that asks for a takeover ends its turn and waits for 交还. When the
// supervisor hands the display back by itself (the page shows signed in, or
// nobody touched it for the idle limit) nothing used to restart the run.
// Hub sees every takeover_begin (it relays MaClawSrv's desktop control), so
// it watches that display until the takeover closes: an automatic hand back
// leaves a one-shot resume in the supervisor, and Hub admits the same
// continuation the 交还 button does, once per takeover, no reload.

var (
	desktopResumeEvery   = 3 * time.Second
	desktopResumeMaxWait = 2 * time.Hour
	desktopResumeAdmit   = func(s *Service, ctx context.Context, tenantID, userID, botID, content, phase, key string) error {
		_, err := s.AdmitDesktopMessage(ctx, tenantID, userID, botID, content, phase, key)
		return err
	}
)

type desktopResumeWatch struct {
	mu      sync.Mutex
	running map[string]bool
	resumed map[string]time.Time // tenant/desktop user/since -> admitted
}

var desktopResumeWatches = desktopResumeWatch{running: map[string]bool{}, resumed: map[string]time.Time{}}

// NoteDesktopCtl is told about each desktop control request Hub relayed
// successfully; a takeover_begin on a bot display starts its watcher.
func (s *Service) NoteDesktopCtl(tenantID, desktopUser string, request map[string]any) {
	if s == nil {
		return
	}
	if op, _ := request["op"].(string); strings.TrimSpace(op) != desktop.CtlTakeoverBegin {
		return
	}
	if _, bot := desktop.SplitDesktopUser(desktopUser); bot == "" {
		return
	}
	s.WatchDesktopTakeover(tenantID, desktopUser)
}

// WatchDesktopTakeover polls the bot display's takeover state (read-only
// bot_state watch) until it closes, resuming the run after an automatic
// hand back. One watcher per display.
func (s *Service) WatchDesktopTakeover(tenantID, desktopUser string) {
	ctl, ok := s.Desktop.(DesktopCtl)
	if !ok || ctl == nil {
		return
	}
	key := strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(desktopUser)
	w := &desktopResumeWatches
	w.mu.Lock()
	if w.running[key] {
		w.mu.Unlock()
		return
	}
	w.running[key] = true
	w.mu.Unlock()
	go func() {
		defer func() {
			w.mu.Lock()
			delete(w.running, key)
			w.mu.Unlock()
		}()
		s.watchDesktopTakeover(ctl, tenantID, desktopUser, key)
	}()
}

func (s *Service) watchDesktopTakeover(ctl DesktopCtl, tenantID, desktopUser, key string) {
	deadline := time.Now().Add(desktopResumeMaxWait)
	failures := 0
	for time.Now().Before(deadline) {
		time.Sleep(desktopResumeEvery)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, err := ctl.Ctl(ctx, tenantID, desktopUser, map[string]any{"op": desktop.CtlBotState, "watch": true})
		cancel()
		if err != nil {
			if failures++; failures >= 10 {
				log.Printf("[bot-desktop] takeover watch %s stopped: %v", desktopUser, err)
				return
			}
			continue
		}
		failures = 0
		if resume, _ := out["resume"].(map[string]any); resume != nil {
			s.resumeAfterHandBack(tenantID, desktopUser, key, resume)
			return
		}
		if open, _ := out["takeover"].(bool); !open {
			return // handed back by the person (button or chat): that path resumes
		}
	}
}

func (s *Service) resumeAfterHandBack(tenantID, desktopUser, key string, resume map[string]any) {
	owner, botID := desktop.SplitDesktopUser(desktopUser)
	since := fmt.Sprint(resume["since"])
	if f, ok := resume["since"].(float64); ok {
		since = fmt.Sprintf("%d", int64(f))
	}
	once := key + "\x00" + since
	w := &desktopResumeWatches
	w.mu.Lock()
	for k, at := range w.resumed {
		if time.Since(at) > 24*time.Hour {
			delete(w.resumed, k)
		}
	}
	if _, done := w.resumed[once]; done {
		w.mu.Unlock()
		return
	}
	w.resumed[once] = time.Now()
	w.mu.Unlock()
	trigger, _ := resume["trigger"].(string)
	message := "桌面已自动交还给你（人类接管期间无操作），请先看清当前页面状态，再继续刚才的任务；不要重新打开或刷新页面。"
	if trigger == "published" {
		message = "页面显示已经发布成功了，桌面已自动交还给你。去笔记管理里找到这篇的链接，用你自己的话简短告诉我发好了（带上链接）；不要再点发布，也不要刷新页面。"
	}
	if trigger == "signed_in" {
		message = "页面已显示登录成功，桌面已自动交还给你，请沿用这个登录状态继续刚才的任务；不要重新打开或刷新页面。"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := desktopResumeAdmit(s, ctx, tenantID, owner, botID, message, "execute", "handback-"+botID+"-"+since)
	log.Printf("[bot-desktop] auto hand back (%s) resumed bot %s: err=%v", trigger, botID, err)
}
