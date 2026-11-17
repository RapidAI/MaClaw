package guiapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ghostDirClass string

const (
	ghostDirMissing ghostDirClass = "missing"
	ghostDirEmpty   ghostDirClass = "empty"
	ghostDirReady   ghostDirClass = "ready"
	ghostDirForeign ghostDirClass = "foreign"
)

// normalizeGhostDir stores Abs+Clean. EvalSymlinks runs only when the path
// already exists; a path the user has not created yet must still be savable.
func normalizeGhostDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("目录为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if _, err := os.Lstat(abs); err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

// classifyGhostDir reports whether path can hold this owner/repo checkout.
// It does not clone, fetch, reset, or delete anything.
func classifyGhostDir(path, owner, repo string) (ghostDirClass, string, error) {
	normalized, err := normalizeGhostDir(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(normalized)
	if err != nil {
		if os.IsNotExist(err) {
			return ghostDirMissing, normalized, nil
		}
		return "", "", err
	}
	if !info.IsDir() {
		return ghostDirForeign, normalized, nil
	}
	entries, err := os.ReadDir(normalized)
	if err != nil {
		return "", "", err
	}
	if len(entries) == 0 {
		return ghostDirEmpty, normalized, nil
	}
	if !gitWorkTree(normalized) {
		return ghostDirForeign, normalized, nil
	}
	origin, err := gitOriginURL(normalized)
	if err != nil || !sameGitHubRepo(origin, owner, repo) {
		return ghostDirForeign, normalized, nil
	}
	return ghostDirReady, normalized, nil
}

func gitWorkTree(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitOriginURL(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
