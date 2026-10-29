package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
)

func demoSkillFixture() demoskill.Timeline {
	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	return demoskill.Timeline{ID: "demo_t1", StartedAt: at, Events: []demoskill.Event{
		{At: at.Add(time.Second), Kind: demoskill.KindNarration, Text: "把除了浏览器以外的窗口都最小化"},
		{At: at.Add(2 * time.Second), Kind: demoskill.KindInput, Action: "click", X: 5, Y: 700},
	}}
}

func TestDistillDemoSkillFlagOff(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "")
	svc := newStatusTestService(t)
	if _, err := svc.DistillDemoSkill(context.Background(), Principal{}, nil, demoSkillFixture()); !errors.Is(err, ErrDemoSkillDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDistillDemoSkillInstallsKnowledgeSkill(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	llm := func(context.Context, string, string) (string, error) {
		return `{"name":"focus-browser","goal":"只保留浏览器窗口","steps":[{"intent":"最小化浏览器以外的所有窗口","check":"屏幕上只有浏览器窗口"}],"deliverable":"桌面上只剩浏览器窗口","evidence":"截图"}`, nil
	}
	got, err := svc.DistillDemoSkill(context.Background(), p, llm, demoSkillFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != demoskill.SourceDemo || got.Type != "knowledge" {
		t.Fatalf("got %+v", got)
	}
	stored, err := svc.GetSkill(context.Background(), p, "focus-browser")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Content, "检查：屏幕上只有浏览器窗口") || !strings.Contains(stored.Content, "持续推进任务") {
		t.Fatalf("stored content:\n%s", stored.Content)
	}
}
