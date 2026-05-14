package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBackupSkillsBlocksCriticalRiskBeforeWrite(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	executor := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	if err := executor.Register(corelib.NLSkillEntry{
		Name:        "backup-danger",
		Description: "dangerous skill",
		Source:      "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "rm -rf $HOME/.ssh"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	err := executor.BackupSkills(zipPath)
	if err == nil || !strings.Contains(err.Error(), "blocked by security scan") {
		t.Fatalf("BackupSkills() error = %v, want security scan block", err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatalf("BackupSkills() wrote archive despite block, stat err = %v", statErr)
	}
}
func TestBackupSkillsBlocksHighRiskBeforeWrite(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	executor := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	if err := executor.Register(corelib.NLSkillEntry{
		Name:        "backup-high",
		Description: "high risk skill",
		Source:      "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "chmod 777 /tmp/maclaw-test"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "backup-high.zip")
	err := executor.BackupSkills(zipPath)
	if err == nil || !strings.Contains(err.Error(), "blocked by security scan") {
		t.Fatalf("BackupSkills() error = %v, want high-risk security scan block", err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatalf("BackupSkills() wrote archive despite block, stat err = %v", statErr)
	}
}
func TestBackupSkillsReplacesExistingArchiveAtomically(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	executor := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	if err := executor.Register(corelib.NLSkillEntry{
		Name:        "backup-safe",
		Description: "safe skill",
		Source:      "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "log",
			Params: map[string]interface{}{"message": "ok"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(zipPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(old archive) error = %v", err)
	}
	if err := executor.BackupSkills(zipPath); err != nil {
		t.Fatalf("BackupSkills() error = %v", err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("OpenReader(new archive) error = %v", err)
	}
	defer zr.Close()
	if len(zr.File) == 0 {
		t.Fatal("BackupSkills() wrote empty zip")
	}
}
func TestExportLearnedSkillsZipBlocksCriticalRiskBeforeWrite(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	executor := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	if err := executor.Register(corelib.NLSkillEntry{
		Name:        "export-danger",
		Description: "dangerous learned skill",
		Source:      "learned",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "rm -rf $HOME/.ssh"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "learned.zip")
	err := executor.ExportLearnedSkillsZip([]string{"export-danger"}, zipPath)
	if err == nil || !strings.Contains(err.Error(), "blocked by security scan") {
		t.Fatalf("ExportLearnedSkillsZip() error = %v, want security scan block", err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatalf("ExportLearnedSkillsZip() wrote archive despite block, stat err = %v", statErr)
	}
}
func TestRestoreSkillsBlocksRiskySkill(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	zipPath := filepath.Join(t.TempDir(), "skills.zip")
	writeSkillBackupZip(t, zipPath, corelib.NLSkillEntry{
		Name:        "restore-danger",
		Description: "dangerous restored skill",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "rm -rf $HOME/.ssh"},
		}},
	})

	executor := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	report, err := executor.RestoreSkills(zipPath)
	if err != nil {
		t.Fatalf("RestoreSkills() error = %v", err)
	}
	if report.Restored != 0 || report.Failed != 1 {
		t.Fatalf("RestoreSkills() report = %+v, want 0 restored and 1 failed", report)
	}
	if len(executor.List()) != 0 {
		t.Fatalf("RestoreSkills() persisted blocked skill: %+v", executor.List())
	}
	if got := strings.Join(report.Details, "\n"); !strings.Contains(got, "blocked by security scan") {
		t.Fatalf("RestoreSkills() details = %q, want security scan block", got)
	}
}

func writeSkillBackupZip(t *testing.T, zipPath string, entry corelib.NLSkillEntry) {
	t.Helper()
	out, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", zipPath, err)
	}
	zw := zip.NewWriter(out)
	manifest, err := json.Marshal(SkillManifest{SkillCount: 1})
	if err != nil {
		t.Fatalf("Marshal(manifest) error = %v", err)
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatalf("Create(manifest.json) error = %v", err)
	}
	if _, err := mw.Write(manifest); err != nil {
		t.Fatalf("Write(manifest.json) error = %v", err)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal(entry) error = %v", err)
	}
	sw, err := zw.Create("restore-danger.json")
	if err != nil {
		t.Fatalf("Create(skill json) error = %v", err)
	}
	if _, err := sw.Write(data); err != nil {
		t.Fatalf("Write(skill json) error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close(zip writer) error = %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close(zip file) error = %v", err)
	}
}
