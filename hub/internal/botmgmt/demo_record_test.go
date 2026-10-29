package botmgmt

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDemoRecordOffByDefault(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "")
	svc, desk := newViewHoldService(t)
	if _, err := svc.DemoRecordStart(context.Background(), "tenant-v", "alice", "bot_iso", ""); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err=%v want ErrDisabled", err)
	}
	if len(desk.ops) != 0 {
		t.Fatalf("ops=%v", desk.ops)
	}
}

func TestDemoRecordStartNarrateStop(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	svc, desk := newViewHoldService(t)
	ctx := context.Background()
	if _, err := svc.DemoRecordStart(ctx, "tenant-v", "alice", "bot_iso", "发周报"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DemoRecordNarrate(ctx, "tenant-v", "alice", "bot_iso", "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty narration err=%v", err)
	}
	if _, err := svc.DemoRecordNarrate(ctx, "tenant-v", "alice", "bot_iso", "收件人填老板"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DemoRecordStop(ctx, "tenant-v", "alice", "bot_iso"); err != nil {
		t.Fatal(err)
	}
	want := []string{desktop.CtlDemoBegin, desktop.CtlDemoNarrate, desktop.CtlDemoEnd}
	if len(desk.ops) != 3 || desk.ops[0] != want[0] || desk.ops[1] != want[1] || desk.ops[2] != want[2] {
		t.Fatalf("ops=%v", desk.ops)
	}
	if desk.users[0] != desktop.BotDesktopUser("alice", "bot_iso") {
		t.Fatalf("users=%v", desk.users)
	}
	if _, err := svc.DemoRecordStart(ctx, "tenant-v", "bob", "bot_iso", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user err=%v", err)
	}
}
