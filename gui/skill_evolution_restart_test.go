package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestSkillExecutorRestartPreservesVerificationMetadata exercises the durable
// boundary used by staged admission: metadata written to skill.yaml must be
// visible to a newly constructed executor and to an explicit YAML rescan.
func TestSkillExecutorRestartPreservesVerificationMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))

	root := filepath.Join(home, "external-skills")
	dir := filepath.Join(root, "verified-candidate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "name: verified-candidate\nsource: auto_discovered\nstatus: active\nverified_at: 2026-08-29T10:00:00Z\nverification_run_id: verify-1234\nverification_digest: sha256:deadbeef\nverification_gate_status: passed\nsteps:\n  - action: bash\n    params:\n      command: echo verified\n"
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: home}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{root}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	first := NewSkillExecutor(app, nil, nil)
	got := first.loadSkills()
	assertVerifiedCandidate(t, got)
	rescanned := first.scanSkillYAMLFiles()
	assertVerifiedCandidate(t, rescanned)

	// A fresh executor must not depend on the previous executor's skill cache.
	second := NewSkillExecutor(app, nil, nil)
	assertVerifiedCandidate(t, second.loadSkills())
}

// TestSkillExecutorOverlayCannotPromoteStagedCandidate verifies the fail
// closed direction of the overlay merge: an active config row cannot promote
// an on-disk auto-discovered staged candidate without constrained verification.
func TestSkillExecutorOverlayCannotPromoteStagedCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))

	root := filepath.Join(home, "external-skills")
	dir := filepath.Join(root, "staged-candidate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: staged-candidate\nsource: auto_discovered\nstatus: staged\nsteps:\n  - action: bash\n    params:\n      command: echo staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: home}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{root}
	// Simulate a stale/hostile overlay from an older process. It must not be
	// able to turn the authoritative staged YAML into an active skill.
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: "staged-candidate", Source: "auto_discovered", SkillDir: dir, Status: "active",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded := NewSkillExecutor(app, nil, nil).loadSkills()
	if len(loaded) != 1 {
		t.Fatalf("loaded skills = %#v, want one candidate", loaded)
	}
	if loaded[0].Status != "staged" {
		t.Fatalf("overlay promoted staged candidate to %q", loaded[0].Status)
	}
}

func assertVerifiedCandidate(t *testing.T, skills []corelib.NLSkillEntry) {
	t.Helper()
	if len(skills) != 1 {
		t.Fatalf("skills = %#v, want one candidate", skills)
	}
	s := skills[0]
	if s.Name != "verified-candidate" || s.Status != "active" || s.Source != "auto_discovered" {
		t.Fatalf("candidate identity/status = %#v", s)
	}
	if s.VerifiedAt != "2026-08-29T10:00:00Z" || s.VerificationRunID != "verify-1234" ||
		s.VerificationDigest != "sha256:deadbeef" || s.VerificationGateStatus != "passed" {
		t.Fatalf("verification metadata lost after load/rescan: %#v", s)
	}
}
