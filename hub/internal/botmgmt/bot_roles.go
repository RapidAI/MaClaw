package botmgmt

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Bot roles (d78). Every bot has three roles:
//
//   Router  - the bot's user-facing interface. It takes the person's
//             commands, sends a new task to a worker and a change to an old
//             task to the worker that owns it (RouteFollowUp), and it is the
//             ONLY layer that speaks to the person.
//   Worker  - the desktop run that does the work. It never talks to the
//             person and never blocks the router; its receipts go to the
//             router.
//   Watcher - patrols this bot's workers (desktop_task_patrol.go) and
//             reports findings to THIS bot's router, never to the person.
//
// So every notice flows worker/watcher -> router -> person, and the router
// alone decides whether and how to say it (short first-person Chinese).

// watchFinding is a watcher's fact about one worker run. It carries no
// person-facing text: wording is the router's job.
type watchFinding struct {
	TenantID, UserID, BotID, RunID string
	Reason                         string // stalled | no_receipt | attention | failed
	Detail                         string // e.g. the worker's own attention line
}

type routerInbox struct {
	mu   sync.Mutex
	seen map[string]time.Time // bot\x00run\x00reason -> handled
}

// routerTell is the single path a router uses to reach the person.
// Replaced in tests.
var routerTell = func(s *Service, tenantID, userID, botID, text, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := s.Say(ctx, tenantID, userID, botID, "", text, key)
	return err
}

// watcherReport hands a finding to the owning bot's router.
func (s *Service) watcherReport(f watchFinding) {
	if s == nil || strings.TrimSpace(f.BotID) == "" || strings.TrimSpace(f.RunID) == "" {
		return
	}
	s.routerReceiveFinding(f)
}

// routerReceiveFinding: the router of f.BotID decides once per run+reason
// what to tell the person.
func (s *Service) routerReceiveFinding(f watchFinding) {
	s.routerOnce.Do(func() { s.router = &routerInbox{seen: map[string]time.Time{}} })
	k := f.BotID + "\x00" + f.RunID + "\x00" + f.Reason
	s.router.mu.Lock()
	if _, done := s.router.seen[k]; done {
		s.router.mu.Unlock()
		return
	}
	now := s.now()
	s.router.seen[k] = now
	for key, at := range s.router.seen {
		if now.Sub(at) > 24*time.Hour {
			delete(s.router.seen, key)
		}
	}
	s.router.mu.Unlock()
	text := routerWordFinding(f)
	if text == "" {
		return // nothing worth the person's attention
	}
	key := "router-" + f.Reason + "-" + f.RunID
	if err := routerTell(s, f.TenantID, f.UserID, f.BotID, text, key); err != nil {
		s.routerFallbackFeed(f, text, key)
	}
}

// routerWordFinding turns a watcher fact into the router's own line.
func routerWordFinding(f watchFinding) string {
	switch f.Reason {
	case "stalled":
		return "这件我停住了：很久没有进展，我先收手，页面保持原样。你说一声我接着做。"
	case "no_receipt":
		return "这件做完了，桌面上就是结果，你看一眼。"
	case "attention":
		return firstNonEmptyString(strings.TrimSpace(f.Detail), "这件需要你看一眼，我在桌面等你。")
	case "failed":
		return "这件我没做成，中途出了问题，我停在当前页面，没有重复提交任何东西。你说一声我再试一次。"
	}
	return ""
}

// routerFallbackFeed: Say is off or failed; the router still leaves one
// line in the feed via the primary's mailbox (not a second person notice).
func (s *Service) routerFallbackFeed(f watchFinding, text, key string) {
	primary := s.primaryBotOf(f.TenantID, f.UserID)
	if primary == "" || primary == f.BotID {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, f.TenantID, f.UserID)
	if err != nil {
		return
	}
	_, _, _ = s.reportLocked(&rec, f.UserID, f.BotID, BotCrossNote{Target: primary, Topic: "question", Text: text, Key: key})
	_ = s.save(ctx, f.TenantID, rec)
}
