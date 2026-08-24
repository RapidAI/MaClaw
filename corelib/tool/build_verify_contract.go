package tool

import (
	"os"
	"path/filepath"
	"strings"
)

// The reviewed verification contract lives here, not in either host, because
// it answers "which programs may this capability start". Two copies of that
// answer would drift, and drift in this particular table is a security
// regression rather than an inconsistency.

// BuildVerifyTasks is the closed set of outcomes build.verify.local exposes.
// A managed plan names one of these; it never supplies a command line.
func BuildVerifyTasks() []string {
	return []string{"build", "test", "lint", "format_check"}
}

func BuildVerifyTaskAllowed(task string) bool {
	for _, allowed := range BuildVerifyTasks() {
		if task == allowed {
			return true
		}
	}
	return false
}

// buildVerifyCommands resolves (project kind, task) to a fixed argv. A missing
// entry is a refusal, not a fallback: inventing a command for a project nobody
// reviewed is how a verification grant turns into arbitrary execution.
var buildVerifyCommands = map[string]map[string][]string{
	"go": {
		"build":        {"go", "build", "./..."},
		"test":         {"go", "test", "./..."},
		"lint":         {"go", "vet", "./..."},
		"format_check": {"gofmt", "-l", "."},
	},
	"rust": {
		"build":        {"cargo", "build"},
		"test":         {"cargo", "test"},
		"lint":         {"cargo", "clippy"},
		"format_check": {"cargo", "fmt", "--check"},
	},
	"node": {
		"build":        {"npm", "run", "build"},
		"test":         {"npm", "test"},
		"lint":         {"npm", "run", "lint"},
		"format_check": {"npm", "run", "format:check"},
	},
	"python": {
		"test":         {"pytest"},
		"lint":         {"ruff", "check", "."},
		"format_check": {"ruff", "format", "--check", "."},
	},
}

// BuildVerifyCommand returns the reviewed argv for one project kind and task.
// The returned slice is a copy so a caller cannot edit the table in place.
func BuildVerifyCommand(kind, task string) ([]string, bool) {
	argv, ok := buildVerifyCommands[kind][task]
	if !ok || len(argv) == 0 {
		return nil, false
	}
	return append([]string(nil), argv...), true
}

// BuildVerifyProjectKinds lists the recognised kinds. It exists so tests can
// enumerate the table without reaching into it.
func BuildVerifyProjectKinds() []string {
	return []string{"go", "rust", "node", "python"}
}

var buildVerifyMarkers = []struct {
	file string
	kind string
}{
	{"go.mod", "go"},
	{"Cargo.toml", "rust"},
	{"package.json", "node"},
	{"pyproject.toml", "python"},
}

// BuildVerifyProjectKind walks from the run directory up to the workspace
// root. A package several directories down carries no marker of its own, so
// detecting only in the run directory would refuse most real targets. The walk
// stops at the workspace root: a marker above it belongs to a project the plan
// never bound.
func BuildVerifyProjectKind(workspace, runDir string) (string, bool) {
	base, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return "", false
	}
	dir, err := filepath.Abs(strings.TrimSpace(runDir))
	if err != nil {
		return "", false
	}
	if rel, err := filepath.Rel(base, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	for {
		for _, marker := range buildVerifyMarkers {
			if _, err := os.Stat(filepath.Join(dir, marker.file)); err == nil {
				return marker.kind, true
			}
		}
		if dir == base {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// BuildVerifyMaxOutput bounds what either host hands back. Compiler and test
// output is unbounded and the failures a model needs are at the end.
const BuildVerifyMaxOutput = 64 * 1024

// BuildVerifyProjection shapes command output identically on both hosts, so a
// managed plan sees the same result whichever one served it.
func BuildVerifyProjection(stdout, stderr string) string {
	combined := strings.TrimSpace(strings.TrimSpace(stdout) + "\n" + strings.TrimSpace(stderr))
	if len(combined) <= BuildVerifyMaxOutput {
		return combined
	}
	return "[output truncated to last 65536 bytes]\n" + combined[len(combined)-BuildVerifyMaxOutput:]
}

// BuildVerifyWorkspaceSubdir keeps a model-supplied target inside the bound
// workspace and confirms it is a directory. Callers map the boolean reasons
// onto their own error vocabulary.
func BuildVerifyWorkspaceSubdir(workspace, target string) (dir string, escaped bool, notDir bool) {
	base, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return "", true, false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return base, false, false
	}
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, target)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", true, false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", true, false
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false, true
	}
	return abs, false, false
}
