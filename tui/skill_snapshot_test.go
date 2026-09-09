package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillDirSnapshotRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(filepath.Join(skillDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "nested", "run.sh"), []byte("echo ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	snapshot, cleanup, err := snapshotSkillDirTUI(skillDir)
	if err != nil {
		t.Fatalf("snapshotSkillDirTUI: %v", err)
	}
	defer cleanup()
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "new.txt"), []byte("transient\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreSkillDirTUI(skillDir, snapshot); err != nil {
		t.Fatalf("restoreSkillDirTUI: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "name: demo\n" {
		t.Fatalf("restored skill.yaml = %q", got)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("transient file survived restore, err=%v", err)
	}
}

func TestSkillDirSnapshotRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(skillDir, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, cleanup, err := snapshotSkillDirTUI(skillDir); err == nil {
		cleanup()
		t.Fatal("snapshotSkillDirTUI accepted symlink")
	}
}

func TestZipDirectoryTUIRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(skillDir, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	zipPath := filepath.Join(root, "skill.zip")
	if err := zipDirectoryTUI(skillDir, zipPath); err == nil {
		t.Fatal("zipDirectoryTUI accepted symlink")
	}
}

func TestZipDirectoryTUIRejectsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real-skill")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "skill.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "skill-link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := zipDirectoryTUI(linkDir, filepath.Join(root, "skill.zip")); err == nil {
		t.Fatal("zipDirectoryTUI accepted symlink root")
	}
}
