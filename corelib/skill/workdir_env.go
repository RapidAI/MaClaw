package skill

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// MaclawSkillTmpDirName is the per-project directory under the user workbench
// that hosts redirected process temp and skill isolation workspaces.
const MaclawSkillTmpDirName = ".maclaw-tmp"

// InjectSkillWorkDirEnv binds skill subprocess downloads/artifacts to a user
// project/workbench directory without requiring each skill to be modified:
//
//  1. MACLAW_WORKDIR / MACLAW_PROJECT_DIR — explicit workdir for well-behaved skills
//  2. TEMP / TMP / TMPDIR — redirected to <workdir>/.maclaw-tmp so languages that
//     honor tempfile.gettempdir() / os.TempDir() write under the project
//
// workdir must be an existing directory (callers resolve owner/tab paths first).
// Caller-supplied extra_env values are never overwritten. If the caller sets any
// of TEMP/TMP/TMPDIR (case-insensitive on Windows), none of the three are
// auto-filled — avoids splitting Python's TMPDIR-first lookup across roots.
func InjectSkillWorkDirEnv(workdir string, extraEnv map[string]string) map[string]string {
	if extraEnv == nil {
		extraEnv = make(map[string]string)
	}

	// Prefer an already-valid workdir from extra_env over the resolved argument.
	if existing := skillWorkdirFromEnvMap(extraEnv); existing != "" {
		workdir = existing
	} else {
		workdir = strings.TrimSpace(workdir)
		if workdir == "" {
			return extraEnv
		}
		if info, err := os.Stat(workdir); err != nil || !info.IsDir() {
			return extraEnv
		}
		workdir = filepath.Clean(workdir)
	}

	envMapSet(extraEnv, "MACLAW_WORKDIR", workdir)
	if !envMapHasKey(extraEnv, "MACLAW_PROJECT_DIR") {
		envMapSet(extraEnv, "MACLAW_PROJECT_DIR", workdir)
	}

	tmpRoot := filepath.Join(workdir, MaclawSkillTmpDirName)
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		log.Printf("[skill-workdir] temp mkdir failed dir=%q err=%v", tmpRoot, err)
		return extraEnv
	}
	ensureSkillWorkdirTmpGitignore(tmpRoot)

	if !envMapHasAnyKey(extraEnv, "TEMP", "TMP", "TMPDIR") {
		for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
			envMapSet(extraEnv, key, tmpRoot)
		}
	}
	return extraEnv
}

// SkillWorkdirTmpSubdir returns <workdir>/.maclaw-tmp/<subdir> when extraEnv
// carries a valid MACLAW_WORKDIR / MACLAW_PROJECT_DIR. Creates dirs and the
// catch-all gitignore as needed.
func SkillWorkdirTmpSubdir(extraEnv map[string]string, subdir string) string {
	wd := skillWorkdirFromEnvMap(extraEnv)
	if wd == "" {
		return ""
	}
	tmpRoot := filepath.Join(wd, MaclawSkillTmpDirName)
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return ""
	}
	ensureSkillWorkdirTmpGitignore(tmpRoot)
	root := tmpRoot
	if subdir = strings.TrimSpace(subdir); subdir != "" {
		root = filepath.Join(tmpRoot, subdir)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return ""
		}
	}
	return root
}

// SkillWorkdirFromEnv returns the cleaned workdir from extraEnv when present
// and valid. Empty when unset or not a directory.
func SkillWorkdirFromEnv(extraEnv map[string]string) string {
	return skillWorkdirFromEnvMap(extraEnv)
}

func skillWorkdirFromEnvMap(extraEnv map[string]string) string {
	if extraEnv == nil {
		return ""
	}
	for _, key := range []string{"MACLAW_WORKDIR", "MACLAW_PROJECT_DIR"} {
		wd := strings.TrimSpace(envMapGet(extraEnv, key))
		if wd == "" {
			continue
		}
		if info, err := os.Stat(wd); err == nil && info.IsDir() {
			return filepath.Clean(wd)
		}
	}
	return ""
}

func ensureSkillWorkdirTmpGitignore(tmpRoot string) {
	tmpRoot = strings.TrimSpace(tmpRoot)
	if tmpRoot == "" {
		return
	}
	path := filepath.Join(tmpRoot, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return
	}
	_ = os.WriteFile(path, []byte("# MaClaw skill runtime artifacts — do not commit\n*\n"), 0o644)
}

func envMapGet(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		return v
	}
	for k, v := range m {
		if envNameEqual(k, key) {
			return v
		}
	}
	return ""
}

func envMapHasKey(m map[string]string, key string) bool {
	return strings.TrimSpace(envMapGet(m, key)) != ""
}

func envMapHasAnyKey(m map[string]string, keys ...string) bool {
	for _, key := range keys {
		if envMapHasKey(m, key) {
			return true
		}
	}
	return false
}

func envMapSet(m map[string]string, key, value string) {
	if m == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	// Drop case-variants so Windows maps stay consistent.
	for k := range m {
		if envNameEqual(k, key) {
			delete(m, k)
		}
	}
	m[key] = value
}
