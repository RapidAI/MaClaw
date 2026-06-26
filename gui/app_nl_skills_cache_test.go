package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillExecutorListUsesScanCacheUntilInvalidated(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(tmpHome, "skills")
	if err := os.MkdirAll(filepath.Join(externalDir, "first"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "first", "skill.yaml"), []byte("name: first\ndescription: first skill\ntriggers: [first]\nsteps:\n  - action: bash\n    params:\n      command: echo first\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{externalDir}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	exec := NewSkillExecutor(app, nil, nil)
	if got := exec.List(); len(got) == 0 || got[0].Name != "first" {
		t.Fatalf("initial List() = %+v, want first skill", got)
	}
	if err := os.MkdirAll(filepath.Join(externalDir, "second"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "second", "skill.yaml"), []byte("name: second\ndescription: second skill\ntriggers: [second]\nsteps:\n  - action: bash\n    params:\n      command: echo second\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := exec.List(); len(got) != 1 {
		t.Fatalf("cached List() len = %d, want 1 before invalidation", len(got))
	}
	exec.invalidateSkillCache()
	if got := exec.List(); len(got) != 2 {
		t.Fatalf("List() after invalidation len = %d, want 2", len(got))
	}

	cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: "manual", Description: "manual", Status: "active"})
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := exec.List(); len(got) != 3 {
		t.Fatalf("List() after config key change len = %d, want 3", len(got))
	}
}

func TestSkillExecutorDoesNotForegroundScanWhileAppScannerWarms(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: &CachedSkillScanner{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(tmpHome, "skills")
	if err := os.MkdirAll(filepath.Join(externalDir, "first"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "first", "skill.yaml"), []byte("name: first\ndescription: first skill\ntriggers: [first]\nsteps:\n  - action: bash\n    params:\n      command: echo first\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{externalDir}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	exec := NewSkillExecutor(app, nil, nil)
	if got := exec.List(); len(got) != 0 {
		t.Fatalf("List while scanner warms = %+v, want no foreground scan result", got)
	}

	app.cachedSkillScanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "first", Description: "first skill", Status: "active", SkillDir: filepath.Join(externalDir, "first")}},
		createdAt: time.Now(),
	})
	if got := exec.List(); len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("List after scanner ready = %+v, want first skill", got)
	}
}

func TestSkillExecutorCacheFollowsScannerVersion(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	skillDir := filepath.Join(tmpHome, "skills", "demo")
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills: []corelib.NLSkillEntry{{
			Name:     "demo",
			Status:   "active",
			SkillDir: skillDir,
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "node old.js"}}},
		}},
		createdAt: time.Now(),
	})
	scanner.version.Store(1)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: scanner}
	exec := NewSkillExecutor(app, nil, nil)

	got := exec.List()
	if len(got) != 1 {
		t.Fatalf("initial List() = %+v, want one skill", got)
	}
	cmd, _ := got[0].Steps[0].Params["command"].(string)
	if cmd != "node old.js" {
		t.Fatalf("initial command = %q, want old command", cmd)
	}

	scanner.cache.Store(&skillCacheEntry{
		skills: []corelib.NLSkillEntry{{
			Name:     "demo",
			Status:   "active",
			SkillDir: skillDir,
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "node new.js"}}},
		}},
		createdAt: time.Now(),
	})
	scanner.version.Add(1)

	got = exec.List()
	if len(got) != 1 {
		t.Fatalf("List() after scanner refresh = %+v, want one skill", got)
	}
	cmd, _ = got[0].Steps[0].Params["command"].(string)
	if cmd != "node new.js" {
		t.Fatalf("command after scanner refresh = %q, want new command", cmd)
	}
}

func TestAddSkillInvalidatesCachedSkillScanner(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "old", Description: "old skill", Status: "active"}},
		createdAt: time.Now(),
	})
	scanner.scanning.Store(true)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: scanner}

	if err := app.AddSkill("new", "new skill", "address", "local-new", "claude"); err != nil {
		t.Fatalf("AddSkill() error = %v", err)
	}

	entry := scanner.cache.Load()
	if entry == nil || !entry.stale {
		t.Fatalf("cached scanner entry = %#v, want stale after AddSkill", entry)
	}
}

func TestCloneSkillEntriesDeepCopiesMutableFields(t *testing.T) {
	fallback := corelib.NLSkillStep{Action: "bash", Params: map[string]interface{}{"command": "echo fallback"}}
	original := []corelib.NLSkillEntry{{
		Name:       "deep",
		Triggers:   []string{"deep"},
		Operations: []corelib.NLSkillOperation{{Name: "op", Params: []string{"input"}, Labels: []string{"run"}}},
		Params:     []corelib.NLSkillParam{{Name: "input", Aliases: []string{"q"}}},
		Steps: []corelib.NLSkillStep{{
			Action:       "bash",
			Params:       map[string]interface{}{"args": []interface{}{"one"}, "nested": map[string]interface{}{"k": "v"}},
			Capture:      map[string]string{"id": "(.*)"},
			Poll:         &corelib.StepPollConfig{Interval: 1},
			Loop:         &corelib.StepLoopConfig{MaxIterations: 2},
			FallbackStep: &fallback,
		}},
		SolidificationCandidates: []corelib.SolidificationCandidate{{ParamSlots: []string{"input"}}},
		Pipeline:                 []corelib.SkillPipelineStep{{Skill: "next", Params: map[string]string{"input": "{{input}}"}}},
	}}

	cloned := cloneSkillEntries(original)
	cloned[0].Triggers[0] = "mutated"
	cloned[0].Operations[0].Params[0] = "mutated"
	cloned[0].Params[0].Aliases[0] = "mutated"
	cloned[0].Steps[0].Params["nested"].(map[string]interface{})["k"] = "mutated"
	cloned[0].Steps[0].Capture["id"] = "mutated"
	cloned[0].Steps[0].Poll.Interval = 99
	cloned[0].Steps[0].Loop.MaxIterations = 99
	cloned[0].Steps[0].FallbackStep.Params["command"] = "mutated"
	cloned[0].SolidificationCandidates[0].ParamSlots[0] = "mutated"
	cloned[0].Pipeline[0].Params["input"] = "mutated"

	if original[0].Triggers[0] != "deep" || original[0].Operations[0].Params[0] != "input" || original[0].Params[0].Aliases[0] != "q" {
		t.Fatal("top-level mutable skill fields were not deep-copied")
	}
	if original[0].Steps[0].Params["nested"].(map[string]interface{})["k"] != "v" || original[0].Steps[0].Capture["id"] != "(.*)" {
		t.Fatal("step maps were not deep-copied")
	}
	if original[0].Steps[0].Poll.Interval != 1 || original[0].Steps[0].Loop.MaxIterations != 2 || original[0].Steps[0].FallbackStep.Params["command"] != "echo fallback" {
		t.Fatal("step pointer fields were not deep-copied")
	}
	if original[0].SolidificationCandidates[0].ParamSlots[0] != "input" || original[0].Pipeline[0].Params["input"] != "{{input}}" {
		t.Fatal("pipeline or solidification fields were not deep-copied")
	}
}
