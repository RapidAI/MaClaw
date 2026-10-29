package botmgmt

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// While the owner has an isolated bot's desktop view open, the Hub tells
// desktopd so the bot's desktop actions wait (MACLAW_DEMO_SKILL). The bot
// resumes, re-screenshots and replans once the hold is released or lapses.
const (
	desktopViewHoldTTL     = 60 * time.Second
	desktopViewHoldRefresh = 20 * time.Second
	desktopViewHoldTimeout = 5 * time.Second
)

var (
	desktopViewHoldMu   sync.Mutex
	desktopViewHoldSent = map[string]time.Time{}
)

func (s *Service) sendDesktopViewHold(ctx context.Context, tenantID, desktopUser string) {
	if !demoskill.Enabled() || s == nil || s.Desktop == nil {
		return
	}
	ctl, ok := s.Desktop.(DesktopCtl)
	if !ok || ctl == nil {
		return
	}
	key := desktopViewKey(tenantID, desktopUser)
	now := s.now()
	desktopViewHoldMu.Lock()
	if last, ok := desktopViewHoldSent[key]; ok && now.Sub(last) < desktopViewHoldRefresh && !now.Before(last) {
		desktopViewHoldMu.Unlock()
		return
	}
	desktopViewHoldSent[key] = now
	desktopViewHoldMu.Unlock()
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), desktopViewHoldTimeout)
	defer cancel()
	if _, err := ctl.Ctl(callCtx, tenantID, desktopUser, map[string]any{"op": desktop.CtlViewHold, "ttl_seconds": int(desktopViewHoldTTL.Seconds())}); err != nil {
		log.Printf("[desktop-view] hold send failed user=%s err=%v", desktopUser, err)
		desktopViewHoldMu.Lock()
		delete(desktopViewHoldSent, key)
		desktopViewHoldMu.Unlock()
	}
}

func (s *Service) sendDesktopViewRelease(ctx context.Context, tenantID, desktopUser string) {
	if !demoskill.Enabled() || s == nil || s.Desktop == nil {
		return
	}
	ctl, ok := s.Desktop.(DesktopCtl)
	if !ok || ctl == nil {
		return
	}
	desktopViewHoldMu.Lock()
	delete(desktopViewHoldSent, desktopViewKey(tenantID, desktopUser))
	desktopViewHoldMu.Unlock()
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), desktopViewHoldTimeout)
	defer cancel()
	if _, err := ctl.Ctl(callCtx, tenantID, desktopUser, map[string]any{"op": desktop.CtlViewRelease}); err != nil {
		log.Printf("[desktop-view] release send failed user=%s err=%v", desktopUser, err)
	}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
