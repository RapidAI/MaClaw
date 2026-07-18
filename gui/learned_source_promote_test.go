package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldPromoteDiskLearnedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		disk   string
		want   bool
	}{
		{name: "file to learned", config: "file", disk: "learned", want: true},
		{name: "empty to crafted", config: "", disk: "crafted", want: true},
		{name: "manual to learned", config: "manual", disk: "learned", want: true},
		{name: "hub stays hub", config: "hub", disk: "learned", want: false},
		{name: "enterprise_hub stays", config: "enterprise_hub", disk: "learned", want: false},
		{name: "github stays", config: "github", disk: "crafted", want: false},
		{name: "already learned", config: "learned", disk: "crafted", want: false},
		{name: "disk not learned", config: "file", disk: "file", want: false},
		{name: "clawhub stays", config: "clawhub", disk: "learned", want: false},
		{name: "zip_import stays", config: "zip_import", disk: "learned", want: false},
		{name: "auto_hub stays", config: "auto_hub", disk: "crafted", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldPromoteDiskLearnedSource(tt.config, tt.disk)
			if got != tt.want {
				t.Fatalf("shouldPromoteDiskLearnedSource(%q, %q) = %v, want %v",
					tt.config, tt.disk, got, tt.want)
			}
		})
	}
}

func TestSkillPersistsAsDiskOverlay(t *testing.T) {
	t.Parallel()

	if !skillPersistsAsDiskOverlay(corelib.NLSkillEntry{Source: "file", SkillDir: "/x"}) {
		t.Fatal("file skills should use disk overlay")
	}
	if !skillPersistsAsDiskOverlay(corelib.NLSkillEntry{Source: "learned", SkillDir: "/x"}) {
		t.Fatal("learned+SkillDir should use disk overlay")
	}
	if !skillPersistsAsDiskOverlay(corelib.NLSkillEntry{Source: "crafted", SkillDir: "/x"}) {
		t.Fatal("crafted+SkillDir should use disk overlay")
	}
	if skillPersistsAsDiskOverlay(corelib.NLSkillEntry{Source: "learned"}) {
		t.Fatal("config-only learned (no SkillDir) should keep full entry")
	}
	if skillPersistsAsDiskOverlay(corelib.NLSkillEntry{Source: "manual", SkillDir: "/x"}) {
		t.Fatal("manual should not force disk overlay solely from SkillDir")
	}
}

func TestDiskSkillConfigOverlayStripsDefinition(t *testing.T) {
	t.Parallel()

	in := corelib.NLSkillEntry{
		Name:         "craft_task_demo",
		Description:  "should strip",
		Source:       "learned",
		SkillDir:     "/skills/craft-task-demo",
		Status:       "active",
		Steps:        []corelib.NLSkillStep{{Action: "bash"}},
		Triggers:     []string{"demo"},
		UsageCount:   3,
		SuccessCount: 2,
	}
	out := diskSkillConfigOverlay(in)
	if out.Name != "craft_task_demo" || out.Source != "learned" || out.SkillDir != in.SkillDir {
		t.Fatalf("identity/source lost: %+v", out)
	}
	if len(out.Steps) != 0 || len(out.Triggers) != 0 || out.Description != "" {
		t.Fatalf("definition fields should be stripped: %+v", out)
	}
	if out.UsageCount != 3 || out.SuccessCount != 2 {
		t.Fatalf("usage stats should be kept: %+v", out)
	}
}

func TestUpdateLearnedSourceUpsertsThinOverlay(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	// Place under the app's skills root so loadSkills can scan/hydrate it.
	skillsRoot, err := app.primarySkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(skillsRoot, "craft-task-demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := []byte(`name: craft_task_demo
description: demo learned skill
source: learned
status: active
triggers:
  - demo
steps:
  - action: bash
    params:
      command: echo ok
`)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.skillExecutor.UpdateLearnedSource(corelib.NLSkillEntry{
		Name:     "craft_task_demo",
		Source:   "learned",
		SkillDir: skillDir,
		Status:   "active",
	}); err != nil {
		t.Fatalf("UpdateLearnedSource: %v", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("config NLSkills len=%d, want 1: %+v", len(cfg.NLSkills), cfg.NLSkills)
	}
	got := cfg.NLSkills[0]
	if got.Source != "learned" || got.SkillDir != skillDir || got.Name != "craft_task_demo" {
		t.Fatalf("overlay mismatch: %+v", got)
	}
	if len(got.Steps) != 0 {
		t.Fatalf("config overlay must not store steps: %+v", got)
	}

	// Upsert by SkillDir should rename overlay in place without duplicating.
	if err := app.skillExecutor.UpdateLearnedSource(corelib.NLSkillEntry{
		Name:     "craft_task_demo_v2",
		Source:   "learned",
		SkillDir: skillDir,
	}); err != nil {
		t.Fatalf("UpdateLearnedSource rename: %v", err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("after rename want 1 overlay, got %d: %+v", len(cfg.NLSkills), cfg.NLSkills)
	}
	if cfg.NLSkills[0].Name != "craft_task_demo_v2" {
		t.Fatalf("name=%q, want craft_task_demo_v2", cfg.NLSkills[0].Name)
	}

	// Runtime list should still hydrate steps from disk and keep learned source.
	list := app.skillExecutor.List()
	found := false
	for _, s := range list {
		if s.SkillDir == skillDir || s.Name == "craft_task_demo" || s.Name == "craft_task_demo_v2" {
			found = true
			if s.Source != "learned" {
				t.Fatalf("List source=%q, want learned", s.Source)
			}
			if len(s.Steps) == 0 {
				t.Fatalf("List should hydrate steps from disk")
			}
		}
	}
	if !found {
		t.Fatalf("skill not in List: %+v", list)
	}
}
