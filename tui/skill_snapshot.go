package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// snapshotSkillDirTUI captures a strict, symlink-free process-local copy for
// upload preflight. The auto-fix validation path uses the durable
// directory-move transaction in validate_compensation.go; this helper remains
// lightweight for operations that must not hold a durable transaction across a
// remote submission.
func snapshotSkillDirTUI(skillDir string) (string, func(), error) {
	if strings.TrimSpace(skillDir) == "" {
		return "", func() {}, fmt.Errorf("skill directory is empty")
	}
	skillDir = filepath.Clean(skillDir)
	info, err := os.Lstat(skillDir)
	if err != nil {
		return "", func() {}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", func() {}, fmt.Errorf("skill directory must be a real directory")
	}
	snapshotDir, err := os.MkdirTemp(filepath.Dir(skillDir), ".skill-rollback-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(snapshotDir) }
	if err := copySkillDirContentsTUI(skillDir, snapshotDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return snapshotDir, cleanup, nil
}

// restoreSkillDirTUI replaces the current directory contents with the
// captured pre-image. Callers only invoke this before a remote submission has
// succeeded; a restore failure is surfaced to keep the operation blocked.
func restoreSkillDirTUI(skillDir, snapshotDir string) error {
	if strings.TrimSpace(skillDir) == "" || strings.TrimSpace(snapshotDir) == "" {
		return fmt.Errorf("skill and snapshot directories are required")
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	return copySkillDirContentsTUI(snapshotDir, skillDir)
}

func copySkillDirContentsTUI(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to snapshot symlink %q", rel)
		}
		target := filepath.Join(dst, rel)
		if !pathWithinTUIDir(dst, target) {
			return fmt.Errorf("illegal snapshot target %q", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported Skill file type %q", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		return closeInErr
	})
}

func pathWithinTUIDir(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
