package codesearch

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	maxFileBytes = 2 << 20
	nulScanBytes = 8000
)

func skipDirName(name string) bool {
	switch name {
	case "node_modules", "vendor", "dist", "build", "out", "target", "coverage",
		".next", ".cache", ".gomodcache", ".gocache", ".venv", "venv", "env",
		"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".gradle", ".terraform":
		return true
	default:
		return false
	}
}

func skipComponent(name string, isDir bool) bool {
	if name == ".git" {
		return true
	}
	if isDir && skipDirName(name) {
		return true
	}
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func sourceExt(name string) bool {
	switch strings.ToLower(extOf(name)) {
	case ".go", ".py", ".pyi", ".c", ".h", ".hh", ".hpp", ".cc", ".cpp", ".cxx",
		".java", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".rs", ".rb",
		".php", ".cs", ".kt", ".kts", ".swift", ".scala", ".lua", ".sh", ".bash",
		".zsh", ".zig", ".ex", ".exs", ".hs", ".ml", ".mli", ".vue", ".svelte",
		".sql", ".proto", ".dart", ".pl", ".pm", ".r", ".jl", ".fs", ".fsx",
		".clj", ".cljs", ".erl", ".hrl", ".elm", ".nim", ".v", ".sv", ".css", ".scss":
		return true
	default:
		return false
	}
}

type rule struct {
	base     string
	negate   bool
	dirOnly  bool
	anchored bool
	pattern  string
}

func parseGitignore(base, text string) []rule {
	var out []rule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := false
		if strings.HasPrefix(line, "!") {
			neg = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		// A leading slash anchors the pattern to this gitignore's directory.
		// Stripping it and matching the basename would also hide pkg/generated
		// when the rule was only /generated.
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		out = append(out, rule{base: base, negate: neg, dirOnly: dirOnly, anchored: anchored, pattern: line})
	}
	return out
}

func (r rule) matches(rel string, isDir bool) bool {
	target, ok := trimIgnoreBase(rel, r.base)
	if !ok || target == "" {
		return false
	}
	if !strings.Contains(r.pattern, "/") {
		if r.anchored && strings.Contains(target, "/") {
			return false
		}
		if r.dirOnly {
			if isDir {
				ok, err := pathMatch(r.pattern, targetBase(target))
				return err == nil && ok
			}
			dir := target
			if i := strings.LastIndex(target, "/"); i >= 0 {
				dir = target[:i]
			} else {
				return false
			}
			for _, seg := range strings.Split(dir, "/") {
				ok, err := pathMatch(r.pattern, seg)
				if err == nil && ok {
					return true
				}
			}
			return false
		}
		ok, err := pathMatch(r.pattern, targetBase(target))
		return err == nil && ok
	}
	ok, err := pathMatch(r.pattern, target)
	return err == nil && ok
}

func targetBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func pathMatch(pattern, name string) (bool, error) {
	if runtime.GOOS == "windows" {
		pattern = strings.ToLower(pattern)
		name = strings.ToLower(name)
	}
	return path.Match(pattern, name)
}

func trimIgnoreBase(rel, base string) (string, bool) {
	if base == "" {
		return rel, true
	}
	relKey := foldPathKey(rel)
	baseKey := foldPathKey(base)
	if relKey == baseKey {
		return "", true
	}
	prefix := baseKey + "/"
	if !strings.HasPrefix(relKey, prefix) {
		return "", false
	}
	if relKey == rel && baseKey == base {
		return rel[len(prefix):], true
	}
	return relKey[len(prefix):], true
}

func ignored(rel string, isDir bool, rules []rule) bool {
	on := false
	for _, r := range rules {
		if r.matches(rel, isDir) {
			on = !r.negate
		}
	}
	return on
}

// ignoredRel applies the walk's decision to one file. The walk never enters
// an ignored directory, so a later note for a file inside it stays out too.
// A pattern with no slash matches only the directory name, which is why the
// parent has to be checked again here.
func ignoredRel(rel string, rules []rule) bool {
	rel = filepath.ToSlash(rel)
	if ignored(rel, false, rules) {
		return true
	}
	for {
		i := strings.LastIndex(rel, "/")
		if i <= 0 {
			return false
		}
		rel = rel[:i]
		if ignored(rel, true, rules) {
			return true
		}
	}
}

func pathSkipped(rel string) bool {
	base := targetBase(rel)
	if strings.HasPrefix(base, ".") {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == base {
			continue
		}
		if skipComponent(seg, true) {
			return true
		}
	}
	return false
}

type walkedFile struct {
	rel  string
	body string
	size int64
	mod  time.Time
}

func collectFiles(root, only string, limit int) (files []walkedFile, rules []rule, dirs map[string]time.Time, truncated bool) {
	if limit < 1 {
		limit = 1
	}
	dirs = map[string]time.Time{}
	var walk func(dir, rel string, inherited []rule)
	walk = func(dir, rel string, inherited []rule) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs[rel] = info.ModTime()
		}
		if truncated {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		local := append([]rule{}, inherited...)
		if data, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
			parsed := parseGitignore(rel, string(data))
			local = append(local, parsed...)
			rules = append(rules, parsed...)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if truncated {
				return
			}
			name := entry.Name()
			if name == ".gitignore" {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			// Lstat does not call a Windows junction or a symlink a directory.
			// ReadDir can. Follow info so the walk stays inside the project.
			if mode&os.ModeSymlink != 0 || (mode&os.ModeIrregular != 0 && !info.IsDir()) {
				continue
			}
			child := name
			if rel != "" {
				child = rel + "/" + name
			}
			isDir := info.IsDir()
			if skipComponent(name, isDir) {
				continue
			}
			if ignored(child, isDir, local) {
				continue
			}
			if isDir {
				if only != "" && !dirMayHold(child, only) {
					continue
				}
				walk(filepath.Join(dir, name), child, local)
				continue
			}
			if only != "" && !underPath(child, only) {
				continue
			}
			if !sourceExt(name) || info.Size() == 0 || info.Size() > maxFileBytes {
				continue
			}
			if len(files) >= limit {
				truncated = true
				return
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || hasNUL(body) {
				continue
			}
			files = append(files, walkedFile{rel: child, body: string(body), size: info.Size(), mod: info.ModTime()})
		}
	}
	walk(root, "", nil)
	return files, rules, dirs, truncated
}

func dirMayHold(dir, only string) bool {
	dir = foldPathKey(dir)
	only = foldPathKey(only)
	return dir == only || strings.HasPrefix(only, dir+"/") || strings.HasPrefix(dir, only+"/")
}

func underPath(child, parent string) bool {
	parent = strings.Trim(strings.ReplaceAll(parent, "\\", "/"), "/")
	child = strings.ReplaceAll(child, "\\", "/")
	if parent == "" || parent == "." {
		return true
	}
	// Walked names keep the on-disk casing. A Windows query path often does
	// not, and a case-sensitive prefix then drops every hit in that directory.
	parent = foldPathKey(parent)
	child = foldPathKey(child)
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func foldPathKey(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func hasNUL(b []byte) bool {
	n := len(b)
	if n > nulScanBytes {
		n = nulScanBytes
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}
