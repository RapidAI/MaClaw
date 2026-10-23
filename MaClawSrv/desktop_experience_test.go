package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopExperienceLearnsOnlyValidatedOutcomes(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	// Learned notes only; seeded playbooks are covered in desktop_zhihu_test.go.
	seeds := desktopSeedNotes
	desktopSeedNotes = nil
	t.Cleanup(func() { desktopSeedNotes = seeds })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-exp"}
	script := map[string]any{"action": "script", "script": "el.innerText='secret body 123'"}
	paste := map[string]any{"action": "app_run", "steps": []any{map[string]any{"action": "paste", "text": "secret body 123"}}}
	// Unverified success teaches nothing.
	desktopExperienceRecord(scope, "zhuanlan.zhihu.com", "script", script, "ok", nil)
	if got := desktopExperienceText(scope, "zhuanlan.zhihu.com"); strings.Contains(got, "secret") || strings.Contains(got, "worked:") || strings.Contains(got, "avoid:") {
		t.Fatalf("unverified step stored: %q", got)
	}
	desktopExperienceRecord(scope, "zhuanlan.zhihu.com", "script", script, "ok\n\n"+desktopNotAppliedMarker, nil)
	desktopExperienceRecord(scope, "zhuanlan.zhihu.com", "app_run", paste, "ok\n"+desktopAppliedMarker, nil)
	desktopExperienceRecord(scope, "zhuanlan.zhihu.com", "navigate", map[string]any{"action": "navigate"}, "", errors.New("browser policy blocked cross-origin navigation"))
	got := desktopExperienceText(scope, "")
	for _, want := range []string{"zhuanlan.zhihu.com", "worked: real keyboard/clipboard input (app_run)", "avoid: page script DOM write failed with not_applied", "avoid: navigate failed with cross_origin_block"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	raw, _ := os.ReadFile(desktopExperiencePath(scope))
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("typed text leaked into the store: %s", raw)
	}
	// Another user sees nothing.
	if other := desktopExperienceText(agentruntime.Scope{TenantID: "t", UserID: "u-other"}, "zhuanlan.zhihu.com"); strings.Contains(other, "worked:") || strings.Contains(other, "avoid:") {
		t.Fatalf("leaked across users: %q", other)
	}
	t.Setenv(desktopExperienceEnv, "off")
	if desktopExperienceText(scope, "") != "" {
		t.Fatal("flag off")
	}
}

func TestDesktopExperienceSizeCap(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-cap"}
	for i := 0; i < desktopExperienceMaxSites+10; i++ {
		site := strings.Repeat("a", i%7+1) + "-" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".example"
		desktopExperienceRecord(scope, site, "script", map[string]any{"action": "script"}, "", errors.New("ref e1 is stale"))
	}
	f := desktopExperienceLoad(desktopExperiencePath(scope))
	if len(f.Sites) > desktopExperienceMaxSites {
		t.Fatalf("sites=%d", len(f.Sites))
	}
}

func TestHostOfURL(t *testing.T) {
	for in, want := range map[string]string{"https://www.Zhihu.com/a?b": "zhihu.com", "http://u:p@x.io:8080/": "x.io", "zhihu.com": ""} {
		if got := browser.HostOfURL(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}
