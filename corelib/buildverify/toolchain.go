package buildverify

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// logonPathDirs is the search list used when the process PATH does not
// contain the reviewed program. Tests replace it. The name being resolved
// always comes from the reviewed command table, never from the model.
var logonPathDirs = defaultLogonPathDirs

func defaultLookupReviewedProgram(name string) string {
	path, _ := resolveReviewedProgram(name)
	return path
}

// resolveReviewedProgram returns the program to execute and, when that
// program was found outside the process PATH, the directory that must be
// visible to the child. A name LookPath already resolves is left unchanged
// so Command keeps its own resolution, including batch-file argv.
func resolveReviewedProgram(name string) (path, prependDir string) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return name, ""
	}
	if _, err := exec.LookPath(name); err == nil {
		return name, ""
	}
	if found, ok := findProgramInDirs(logonPathDirs(), name); ok {
		return found, filepath.Dir(found)
	}
	return name, ""
}

// prependPathDir returns a child environment whose PATH starts with dir.
// The existing key's spelling is kept: Windows often stores "Path".
func prependPathDir(dir string) []string {
	env := os.Environ()
	prefix := dir + string(os.PathListSeparator)
	for i, entry := range env {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || !strings.EqualFold(key, "PATH") {
			continue
		}
		env[i] = key + "=" + prefix + val
		return env
	}
	return append(env, "PATH="+dir)
}

func findProgramInDirs(dirs []string, name string) (string, bool) {
	cands := programCandidates(name)
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, cand := range cands {
			path := filepath.Join(dir, cand)
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			return path, true
		}
	}
	return "", false
}

func programCandidates(name string) []string {
	if runtime.GOOS != "windows" || strings.Contains(name, ".") {
		return []string{name}
	}
	// Match LookPath: an extensionless name is searched only with PATHEXT.
	// A bare file named "go" is not the Go toolchain.
	exts := windowsPathExts()
	out := make([]string, 0, len(exts))
	for _, ext := range exts {
		out = append(out, name+ext)
	}
	return out
}

func windowsPathExts() []string {
	raw := os.Getenv("PATHEXT")
	if strings.TrimSpace(raw) == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}
	var exts []string
	seen := map[string]bool{}
	for _, ext := range strings.Split(raw, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		key := strings.ToLower(ext)
		if seen[key] {
			continue
		}
		seen[key] = true
		exts = append(exts, ext)
	}
	return exts
}
