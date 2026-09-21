package guiapp

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// pathContainedInBase reports whether absPath is the base directory itself or a
// descendant. Used to keep download_file / web_fetch(save_path) and trusted
// read/write inside the bound workspace.
func pathContainedInBase(absPath, base string) bool {
	_, _, ok := workspaceRelativePath(base, absPath)
	return ok
}

type trustedPathKind int

const (
	trustedPathOK trustedPathKind = iota
	trustedPathNoWorkspace
	trustedPathEmpty
	trustedPathOutside
)

// resolvePathInsideWorkspace confines path to workspace. Callers map kind onto
// their own error tokens; base is the Abs workspace even when path is rejected,
// so a write rejection can name the directory the model is allowed to use.
func resolvePathInsideWorkspace(workspace, path string) (abs, base string, kind trustedPathKind) {
	workspace = strings.TrimSpace(workspace)
	path = corelib.ExpandHomePath(path)
	if workspace == "" {
		return "", "", trustedPathNoWorkspace
	}
	resolved, err := filepath.Abs(workspace)
	if err != nil {
		return "", "", trustedPathNoWorkspace
	}
	base = normalizeProjectSessionPath(resolved)
	if path == "" {
		return "", base, trustedPathEmpty
	}
	candidate := path
	if !filepath.IsAbs(path) {
		candidate = filepath.Join(base, path)
	}
	abs, err = filepath.Abs(candidate)
	if err != nil {
		return "", base, trustedPathOutside
	}
	abs = normalizeProjectSessionPath(abs)
	if !pathContainedInBase(abs, base) {
		return "", base, trustedPathOutside
	}
	return abs, base, trustedPathOK
}

func trustedRelDisplayPath(workspace, absPath, raw string) string {
	workspace = strings.TrimSpace(workspace)
	absPath = strings.TrimSpace(absPath)
	if workspace != "" {
		if w, err := filepath.Abs(workspace); err == nil {
			workspace = normalizeProjectSessionPath(w)
		}
	}
	if absPath != "" {
		if a, err := filepath.Abs(absPath); err == nil {
			absPath = normalizeProjectSessionPath(a)
		}
	}
	if _, rel, ok := workspaceRelativePath(workspace, absPath); ok {
		return filepath.ToSlash(rel)
	}
	return strings.TrimSpace(raw)
}

func workspaceRelativePath(workspace, absPath string) (base, rel string, ok bool) {
	workspace = filepath.Clean(strings.TrimSpace(workspace))
	absPath = filepath.Clean(strings.TrimSpace(absPath))
	if workspace == "" || absPath == "" {
		return "", "", false
	}
	left, right := workspace, absPath
	if runtime.GOOS == "windows" {
		left = strings.ToLower(left)
		right = strings.ToLower(right)
	}
	rel, err := filepath.Rel(left, right)
	if err != nil {
		return workspace, "", false
	}
	slash := filepath.ToSlash(rel)
	if slash == ".." || strings.HasPrefix(slash, "../") {
		return workspace, "", false
	}
	return workspace, rel, true
}
