package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestBuildStandaloneTaskPath_DifferentTitles(t *testing.T) {
	dataDir := filepath.Join("C:", "Users", "test", ".maclaw", "data")

	p1 := buildStandaloneTaskPath(dataDir, "找篇新的hugging face上的agent相关论文")
	p2 := buildStandaloneTaskPath(dataDir, "翻译这篇论文成中文")
	p3 := buildStandaloneTaskPath(dataDir, "找篇新的hugging face上的agent相关论文") // same as p1

	if p1 == "" || p2 == "" {
		t.Fatal("standalone paths should not be empty")
	}
	if p1 == p2 {
		t.Error("different titles should produce different paths")
	}
	if p1 != p3 {
		t.Error("same title should produce the same path (idempotent)")
	}

	// Path should be under dataDir/tasks/
	if !strings.HasPrefix(p1, filepath.Join(dataDir, "tasks")) {
		t.Errorf("path should be under dataDir/tasks/, got: %s", p1)
	}
}

func TestBuildStandaloneTaskPath_EmptyDataDir(t *testing.T) {
	p := buildStandaloneTaskPath("", "some task")
	if p != "" {
		t.Errorf("empty dataDir should return empty path, got: %s", p)
	}
}

func TestBuildSedimentTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// This string is under 50 runes, so it's not truncated.
		{"找篇新的hugging face上的agent相关论文，做成中文详细综述，发我pdf版本", "找篇新的hugging face上的agent相关论文，做成中文详细综述，发我pdf版本"},
		{"short title", "short title"},
		{"", ""},
		{"# heading", "heading"},
	}
	for _, tt := range tests {
		got := buildSedimentTitle(tt.input)
		if got != tt.expected {
			t.Errorf("buildSedimentTitle(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestStandaloneTaskPath_PassesLooksLikeProjectPath verifies that the
// synthetic standalone task path passes ProjectIndex's path validation.
// Tests both Windows and Unix style paths.
func TestStandaloneTaskPath_PassesLooksLikeProjectPath(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
	}{
		{"windows", "C:\\Users\\test\\.maclaw\\data"},
		{"unix", "/home/user/.maclaw/data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := buildStandaloneTaskPath(tt.dataDir, "论文摘要任务")
			if p == "" {
				t.Fatal("path should not be empty")
			}

			// Verify the path passes LooksLikeFilePath (the first check
			// in looksLikeProjectPath). Use forward slashes for cross-platform.
			fwd := strings.ReplaceAll(p, "\\", "/")
			if !memory.LooksLikeFilePath(fwd) {
				t.Errorf("path should pass LooksLikeFilePath, got: %s (fwd: %s)", p, fwd)
			}

			// Verify it has the tasks segment.
			if !strings.Contains(fwd, "/tasks/") {
				t.Errorf("path should contain /tasks/ segment, got: %s", fwd)
			}
		})
	}
}

// TestStandaloneTaskPath_HashStability verifies that the hash is stable
// across calls — the same title always produces the same path.
func TestStandaloneTaskPath_HashStability(t *testing.T) {
	dataDir := "/home/user/.maclaw/data"
	first := buildStandaloneTaskPath(dataDir, "稳定性测试")
	for i := 0; i < 100; i++ {
		got := buildStandaloneTaskPath(dataDir, "稳定性测试")
		if got != first {
			t.Fatalf("hash not stable: call %d returned %q, expected %q", i, got, first)
		}
	}
}

// TestStandaloneTaskPath_InferProjectPath verifies the end-to-end flow:
// an entry with standalone path tag is correctly indexed by ProjectIndex.
func TestStandaloneTaskPath_InferProjectPath(t *testing.T) {
	// Use a platform-appropriate data dir so filepath.Join produces valid paths.
	var dataDir, projectPath string
	if filepath.Separator == '\\' {
		// Windows
		dataDir = "C:\\Users\\test\\.maclaw\\data"
		projectPath = "D:\\workprj\\aicoder"
	} else {
		// Unix
		dataDir = "/home/user/.maclaw/data"
		projectPath = "/home/user/projects/aicoder"
	}

	standalonePath := buildStandaloneTaskPath(dataDir, "论文综述")

	// Simulate the tags that sedimentTaskEntry creates.
	tags := []string{"task_sediment", "auto", standalonePath, projectPath}

	// Build a memory entry like sedimentTaskEntry does.
	entry := memory.Entry{
		Content:  "Task: 论文综述\nResult: 完成",
		Title:    "论文综述",
		Category: memory.CategoryProjectKnowledge,
		Tags:     tags,
	}

	// Build a ProjectIndex and index the entry.
	pi := memory.NewProjectIndex()
	pi.IndexEntry(&entry)

	// inferProjectPath normalizes the path (forward slashes on Unix,
	// backslashes on Windows). Use the same normalization for lookup.
	fwd := strings.ReplaceAll(standalonePath, "\\", "/")
	var normalizedKey string
	if len(fwd) >= 2 && fwd[1] == ':' {
		// Windows path: normalizeProjectPath converts back to backslashes.
		normalizedKey = strings.ReplaceAll(fwd, "/", "\\")
	} else {
		normalizedKey = fwd
	}

	rec := pi.Get(normalizedKey)
	if rec == nil {
		// Also try the raw path in case normalization is a no-op.
		rec = pi.Get(standalonePath)
	}
	if rec == nil {
		t.Fatalf("entry should be indexed under standalone path.\n  raw: %q\n  normalized: %q\n  all records: %v",
			standalonePath, normalizedKey, pi.ListRecent(10))
	}
	if rec.Name != "论文综述" {
		t.Errorf("record name should be %q, got %q", "论文综述", rec.Name)
	}

	// The project path should NOT have its own record (it's a secondary tag,
	// not the first path-like tag).
	fwdProj := strings.ReplaceAll(projectPath, "\\", "/")
	var normalizedProj string
	if len(fwdProj) >= 2 && fwdProj[1] == ':' {
		normalizedProj = strings.ReplaceAll(fwdProj, "/", "\\")
	} else {
		normalizedProj = fwdProj
	}
	projRec := pi.Get(normalizedProj)
	if projRec == nil {
		projRec = pi.Get(projectPath)
	}
	if projRec != nil {
		t.Errorf("project path should NOT have its own record, but got: %+v", projRec)
	}
}
