package botmgmt

import "testing"

func TestDesktopRunActivityKeepsOnlyAStepPhrase(t *testing.T) {
	if desktopRunActivityText("正在输入") != "正在输入" {
		t.Fatalf("kept = %q", desktopRunActivityText("正在输入"))
	}
	if desktopRunActivityText("正在切换窗口") != "正在切换窗口" {
		t.Fatal("a window change was dropped")
	}
	for _, raw := range []string{"正在输入 secret", "正在处理", "正在一点点推进", "正在询问", "tool"} {
		if got := desktopRunActivityText(raw); got != "" {
			t.Fatalf("%q kept as %q", raw, got)
		}
	}

	svc := &Service{desktopAdmits: map[string]*desktopAdmitState{}}
	state := &desktopAdmitState{}
	svc.desktopAdmits[desktopAdmitKey("t", "u", "b", "r")] = state
	svc.rememberDesktopRunActivity(state, "正在输入")
	if got := svc.DesktopRunActivity("t", "u", "b", "r"); got != "正在输入" {
		t.Fatalf("cached = %q", got)
	}
	svc.rememberDesktopRunActivity(state, "正在输入 secret")
	if got := svc.DesktopRunActivity("t", "u", "b", "r"); got != "" {
		t.Fatalf("invalid phrase left %q", got)
	}
	svc.rememberDesktopRunActivity(state, "正在点击")
	state.mu.Lock()
	state.done = true
	state.mu.Unlock()
	if got := svc.DesktopRunActivity("t", "u", "b", "r"); got != "" {
		t.Fatalf("finished run left %q", got)
	}
	if svc.DesktopRunActivity("t", "u", "b", "missing") != "" {
		t.Fatal("a missing run returned a phrase")
	}
}
