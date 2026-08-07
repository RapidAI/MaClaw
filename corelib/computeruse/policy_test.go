package computeruse

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestAllowClickAt_BlockedWindowTitles(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	blocked := []string{
		"User Account Control",
		"用户账户控制",
		"Windows Security",
		"锁屏",
		"凭据管理器 Credentials",
		"登录 - 某应用",
		"隐私设置",
	}
	for _, title := range blocked {
		if err := p.AllowClickAt(100, 100, title); err == nil {
			t.Errorf("title %q must be blocked", title)
		}
	}
	allowed := []string{"", "蓝信", "Notepad", "微信", "文件资源管理器"}
	for _, title := range allowed {
		if err := p.AllowClickAt(100, 100, title); err != nil {
			t.Errorf("title %q must be allowed: %v", title, err)
		}
	}
}

func TestAllowClickAt_TargetAppsAllowlist(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	p.TargetApps = []string{"蓝信", "notepad"}

	if err := p.AllowClickAt(1, 1, "蓝信 - 张三"); err != nil {
		t.Fatalf("allowlisted app blocked: %v", err)
	}
	// "Notepad++" contains "notepad" (case-insensitive substring) → allowed.
	if err := p.AllowClickAt(1, 1, "Notepad++"); err != nil {
		t.Fatalf("substring match should allow Notepad++: %v", err)
	}
	err := p.AllowClickAt(1, 1, "计算器")
	if err == nil || !strings.Contains(err.Error(), "outside approved target apps") {
		t.Fatalf("non-allowlisted app must be denied, got %v", err)
	}
	// Empty title cannot be attributed → allowed (defense-in-depth stays with blocked hints).
	if err := p.AllowClickAt(1, 1, ""); err != nil {
		t.Fatalf("empty title must not trip allowlist: %v", err)
	}
	// Blocked hints still win over the allowlist.
	if err := p.AllowClickAt(1, 1, "蓝信 - 用户账户控制"); err == nil {
		t.Fatal("blocked hint must win over allowlist")
	}
}

func TestAllowClickAt_PauseStop(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	p.Pause()
	if err := p.AllowClickAt(1, 1, "蓝信"); err == nil {
		t.Fatal("paused policy must reject clicks")
	}
	if err := p.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := p.AllowClickAt(1, 1, "蓝信"); err != nil {
		t.Fatalf("resumed policy must allow: %v", err)
	}
	p.Stop()
	if err := p.AllowClickAt(1, 1, "蓝信"); err == nil {
		t.Fatal("stopped policy must reject clicks")
	}
	if err := p.Resume(); err == nil {
		t.Fatal("resume after stop must fail")
	}
}

func TestSessionSetTargetAppsPropagatesToPolicy(t *testing.T) {
	sess := NewSession(DefaultConfig())
	sess.SetTargetApps([]string{"蓝信"})
	if err := sess.CheckClickPolicy(1, 1, "计算器"); err == nil {
		t.Fatal("session policy must enforce target apps")
	}
	if err := sess.CheckClickPolicy(1, 1, "蓝信"); err != nil {
		t.Fatalf("target app must pass: %v", err)
	}
}

func TestCommitObserveAttributesWindowViaResolver(t *testing.T) {
	sess := NewSession(DefaultConfig())
	sess.SetWindowResolver(func(x, y int) string {
		if x > 500 {
			return "用户账户控制"
		}
		return "蓝信"
	})
	els := []taskengine.UIElement{
		{Type: "button", Name: "发送", BBox: [4]int{10, 10, 40, 20}, Source: "accessibility", Interactable: true},
		{Type: "button", Name: "是", BBox: [4]int{600, 10, 40, 20}, Source: "accessibility", Interactable: true},
	}
	res := sess.CommitObserve(ScreenMeta{Width: 1280, Height: 720, ScaleFactor: 1}, nil, els, nil, "")
	if res.Elements[0].Window != "蓝信" || res.Elements[1].Window != "用户账户控制" {
		t.Fatalf("attribution: %+v", res.Elements)
	}
	// The attributed title flows through ResolveClickRef for policy checks.
	_, _, el, err := sess.ResolveClickRef("e1")
	if err != nil || el.Window != "用户账户控制" {
		t.Fatalf("resolve: %v %+v", err, el)
	}
	// No resolver → no attribution (non-Windows hosts).
	sess2 := NewSession(DefaultConfig())
	res2 := sess2.CommitObserve(ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1}, nil, els, nil, "")
	if res2.Elements[0].Window != "" {
		t.Fatalf("unexpected window without resolver: %+v", res2.Elements[0])
	}
}
