package botmgmt

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Demonstrate-and-narrate recording (MACLAW_DEMO_SKILL, stage 3). The owner
// shows the task on an isolated bot's desktop while desktopd records
// screenshots, focus and input; narration arrives as text (typed, or
// speech the HTTP layer transcribed). Stop returns the timeline.

// DemoRecordStart opens a recording on the bot's own desktop.
func (s *Service) DemoRecordStart(ctx context.Context, tenantID, userID, botID, title string) (map[string]any, error) {
	return s.demoRecordCtl(ctx, tenantID, userID, botID, map[string]any{"op": desktop.CtlDemoBegin, "title": strings.TrimSpace(title)})
}

// DemoRecordNarrate adds one narration line to the open recording.
func (s *Service) DemoRecordNarrate(ctx context.Context, tenantID, userID, botID, text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("%w: narration is empty", ErrInvalidInput)
	}
	return s.demoRecordCtl(ctx, tenantID, userID, botID, map[string]any{"op": desktop.CtlDemoNarrate, "text": text})
}

// DemoRecordStop closes the recording and returns desktopd's answer, whose
// "timeline" is a demoskill.Timeline.
func (s *Service) DemoRecordStop(ctx context.Context, tenantID, userID, botID string) (map[string]any, error) {
	return s.demoRecordCtl(ctx, tenantID, userID, botID, map[string]any{"op": desktop.CtlDemoEnd})
}

func (s *Service) demoRecordCtl(ctx context.Context, tenantID, userID, botID string, request map[string]any) (map[string]any, error) {
	if !demoskill.Enabled() {
		return nil, ErrDisabled
	}
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != strings.TrimSpace(userID) {
		return nil, ErrNotFound
	}
	bot := rec.Bots[index]
	if !botIsolated(bot) {
		return nil, fmt.Errorf("%w: 演示录制只用于 Bot 自己的独立桌面（desktop_mode=isolated）", ErrInvalidInput)
	}
	ctl, ok := s.Desktop.(DesktopCtl)
	if !ok || ctl == nil {
		return nil, ErrNotConfigured
	}
	out, err := ctl.Ctl(ctx, tenantID, desktopUserForBot(userID, bot), request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	return out, nil
}
