package botmgmt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

// DemoRecordShot fetches one stored recording screenshot (PNG base64) by
// timeline id and Ref, for captions and the GUI thumbnail strip.
func (s *Service) DemoRecordShot(ctx context.Context, tenantID, userID, botID, demoID, ref string) (string, string, error) {
	out, err := s.demoRecordCtl(ctx, tenantID, userID, botID, map[string]any{
		"op": desktop.CtlDemoShot, "id": strings.TrimSpace(demoID), "ref": strings.TrimSpace(ref),
	})
	if err != nil {
		return "", "", err
	}
	data, _ := out["data"].(string)
	mime, _ := out["mime"].(string)
	if mime == "" {
		mime = "image/png"
	}
	if strings.TrimSpace(data) == "" {
		return "", "", fmt.Errorf("%w: empty demo shot", ErrSrv)
	}
	return mime, data, nil
}

// DemoRecordDistill stops nothing: it takes a timeline (usually from stop),
// loads screenshot bytes from desktopd, and asks MaClawSrv to caption+distill.
func (s *Service) DemoRecordDistill(ctx context.Context, tenantID, userID, botID string, tl demoskill.Timeline, notes string) (map[string]any, error) {
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
	shots := map[string]string{}
	for _, e := range tl.Events {
		if e.Kind != demoskill.KindScreenshot || strings.TrimSpace(e.Ref) == "" || strings.TrimSpace(e.Text) != "" {
			continue
		}
		if len(shots) >= demoskill.CaptionMaxShots {
			break
		}
		_, data, err := s.DemoRecordShot(ctx, tenantID, userID, botID, tl.ID, e.Ref)
		if err != nil {
			continue
		}
		shots[e.Ref] = data
	}
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"timeline": tl, "shots": shots, "notes": strings.TrimSpace(notes)}
	var out map[string]any
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, "/api/v1/skills/demo/distill", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DemoSkillRedistill asks MaClawSrv to re-distill an installed demo skill.
func (s *Service) DemoSkillRedistill(ctx context.Context, tenantID, userID, skillName, notes string) (map[string]any, error) {
	if !demoskill.Enabled() {
		return nil, ErrDisabled
	}
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"skill_name": strings.TrimSpace(skillName), "notes": strings.TrimSpace(notes)}
	var out map[string]any
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, "/api/v1/skills/demo/redistill", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DemoSkillUpdate saves edited guidance for a demo skill.
func (s *Service) DemoSkillUpdate(ctx context.Context, tenantID, userID, skillName string, content, description string, triggers []string) (map[string]any, error) {
	if !demoskill.Enabled() {
		return nil, ErrDisabled
	}
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"content": content, "description": description, "triggers": triggers}
	var out map[string]any
	path := "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(skillName)) + "/demo"
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPut, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
