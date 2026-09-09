package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func TestRecoverTUIEvolutionCompensationsRestoresEmptyConfigAndRemovesCreatedDir(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Setenv("MACLAW_DATA_DIR", base)

	store := commands.NewFileConfigStore(base)
	if err := store.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "half-installed"}}}); err != nil {
		t.Fatal(err)
	}
	finalDir := filepath.Join(base, "skills", "half-installed")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := skill.NewEvolutionCompensationRecord("tui-recovery-empty", "half-installed", "tui_install", "", nil, false, []corelib.NLSkillEntry{}, "test")
	record.SetRecoveryScope(base)
	record.SetCreatedDirectories([]string{finalDir})
	record.SetSkipIndexRefresh(true)
	if err := skill.PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}

	recovered, pending, err := recoverTUIEvolutionCompensations(base)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Fatalf("created directory remains after rollback: %v", err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NLSkills) != 0 {
		t.Fatalf("config was not restored to empty pre-image: %#v", cfg.NLSkills)
	}
}

func TestTUIValidationTransactionRecoversAfterRestart(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Setenv("MACLAW_DATA_DIR", base)

	skillDir := filepath.Join(base, "skills", "portable")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: portable\ndescription: original\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	tx, err := beginTUIValidationTransaction("portable", skillDir)
	if err != nil {
		t.Fatalf("begin validation transaction: %v", err)
	}
	// Simulate an in-place fixer changing the working copy before the process
	// exits. Startup recovery must restore the moved pre-image, not trust the
	// partially modified directory.
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: portable\ndescription: partial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recovered, pending, err := recoverTUIEvolutionCompensations(base)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("recovered YAML = %q, want original %q", data, original)
	}
	if _, err := os.Stat(tx.record.DirBackupPath); !os.IsNotExist(err) {
		t.Fatalf("validation backup remains after recovery: %v", err)
	}
}

func TestTUIValidationRecoveryHonorsFinalAuditAndOnlyCleansBackup(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Setenv("MACLAW_DATA_DIR", base)

	skillDir := filepath.Join(base, "skills", "audited")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: audited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tx, err := beginTUIValidationTransaction("audited", skillDir)
	if err != nil {
		t.Fatalf("begin validation transaction: %v", err)
	}
	updated := []byte("name: audited\ndescription: committed\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), updated, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := skill.RecordEvolutionEventStrict("skill:tui_skill_validated", map[string]string{
		"skill": "audited", "action": "validate_auto_fix", "decision": "applied",
		"request_id": tx.record.RequestID, "schema_version": "2", "evidence_mode": "none",
	}, "tui"); err != nil {
		t.Fatalf("record final audit: %v", err)
	}
	recovered, pending, err := recoverTUIEvolutionCompensations(base)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(updated) {
		t.Fatalf("recovery rolled back audited validation: %q", data)
	}
	if _, err := os.Stat(tx.record.DirBackupPath); !os.IsNotExist(err) {
		t.Fatalf("committed validation backup remains: %v", err)
	}
}

func TestTUIUploadTransactionRecoversBeforeRemoteAcceptance(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Setenv("MACLAW_DATA_DIR", base)

	skillDir := filepath.Join(base, "skills", "upload-pre-remote")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: upload-pre-remote\ndescription: original\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := beginTUIDirectoryTransaction("upload-pre-remote", skillDir, "tui_upload", skill.KindFromEventName("skill:tui_skill_uploaded"))
	if err != nil {
		t.Fatalf("begin upload transaction: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: upload-pre-remote\ndescription: partial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recovered, pending, err := recoverTUIEvolutionCompensations(base)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("pre-remote upload recovery = %q, want original %q", data, original)
	}
}

func TestTUIUploadTransactionKeepsLocalTreeAfterRemoteAcceptance(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Setenv("MACLAW_DATA_DIR", base)

	skillDir := filepath.Join(base, "skills", "upload-accepted")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: upload-accepted\ndescription: original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tx, err := beginTUIDirectoryTransaction("upload-accepted", skillDir, "tui_upload", skill.KindFromEventName("skill:tui_skill_uploaded"))
	if err != nil {
		t.Fatalf("begin upload transaction: %v", err)
	}
	accepted := []byte("name: upload-accepted\ndescription: accepted\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), accepted, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.markExternalSubmission("submission-accepted"); err != nil {
		t.Fatalf("mark remote submission: %v", err)
	}
	recovered, pending, err := recoverTUIEvolutionCompensations(base)
	if err != nil || recovered != 0 || pending != 1 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v, want pending external blocker", recovered, pending, err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(accepted) {
		t.Fatalf("remote-accepted upload tree was rolled back: %q", data)
	}
	// Test cleanup: the pending row is intentionally retained by recovery, but
	// this isolated fixture must not leak it into subsequent tests.
	_ = os.RemoveAll(tx.record.DirBackupPath)
	_ = skill.ClearEvolutionCompensation(tx.record.RequestID, tx.record.Skill, tx.record.Action)
}
