package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/tui/views"
)

func TestFormatCanaryPreviewLine(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	uid := "sticky-user-xyz"
	line := formatCanaryPreviewLine(uid, -1)
	if !strings.Contains(line, "canary-preview:") || !strings.Contains(line, uid) {
		t.Fatalf("line=%q", line)
	}
	p := doctor.PreviewSharedLoopCanary(uid, 50)
	if p.Allows && !strings.Contains(line, "IN canary") {
		t.Fatalf("expected IN: %q", line)
	}
	if !p.Allows && !strings.Contains(line, "OUT canary") {
		t.Fatalf("expected OUT: %q", line)
	}
	if !strings.Contains(line, "bucket=") || !strings.Contains(line, "percent=50") {
		t.Fatalf("line=%q", line)
	}
}

func TestFirstNonFlagArg(t *testing.T) {
	if got := firstNonFlagArg([]string{"--pretty", "alice"}); got != "alice" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonFlagArg([]string{"-x"}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSlashCanaryAndPromptExport(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("MACLAW_DATA_DIR", tempHome)
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "100")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfile(agent.PromptProfileLight)

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{SharedAgentLoopEnabled: true}},
		root: views.NewRootModel("en"),
	}

	m.handleSlashCommand("/canary")
	// usage should set status bar
	if !strings.Contains(m.root.StatusBar.View(200), "/canary") {
		t.Fatalf("usage status=%s", m.root.StatusBar.View(200))
	}

	m.handleSlashCommand("/canary alice")
	if !strings.Contains(m.root.StatusBar.View(200), "canary-preview:") {
		t.Fatalf("canary status=%s", m.root.StatusBar.View(200))
	}
	if !strings.Contains(m.root.StatusBar.View(200), "IN canary") {
		t.Fatalf("100%% canary should allow: %s", m.root.StatusBar.View(200))
	}

	m.handleSlashCommand("/prompt-export")
	if !strings.Contains(m.root.StatusBar.View(200), "exported ") {
		t.Fatalf("export status=%s", m.root.StatusBar.View(200))
	}
	// Default path under data dir exports/
	dir := agent.PromptProfileExportDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "prompt_profile_") && strings.HasSuffix(e.Name(), ".json") {
			found = true
			if _, err := os.Stat(filepath.Join(dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatalf("no export file in %s", dir)
	}

	m.handleSlashCommand("/status alice")
	// status navigates to config; canary line is in chat not status bar necessarily
	if m.root.ActiveTab() != views.TabConfig {
		t.Fatalf("tab=%d", m.root.ActiveTab())
	}
}
