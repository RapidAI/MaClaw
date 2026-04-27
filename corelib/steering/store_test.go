package steering

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSteeringFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStore_LoadAlwaysFiles(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "rules.md", `---
inclusion: always
priority: 10
---
# My Rules
Do things right.
`)
	s := NewStore(dir, "")
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.FileCount() != 1 {
		t.Fatalf("expected 1 file, got %d", s.FileCount())
	}

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}
	if resolved[0].Name != "rules.md" {
		t.Errorf("expected rules.md, got %s", resolved[0].Name)
	}
	if resolved[0].Priority != 10 {
		t.Errorf("expected priority 10, got %d", resolved[0].Priority)
	}
	if !strings.Contains(resolved[0].Content, "Do things right") {
		t.Errorf("content mismatch: %s", resolved[0].Content)
	}
}

func TestStore_ContextMatchMode(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "ssh-rules.md", `---
inclusion: contextMatch
contextKeywords: ['ssh', '服务器', '远程']
---
# SSH Rules
Use ssh tool for server operations.
`)
	s := NewStore(dir, "")
	s.Load()

	// Should match.
	resolved := s.Resolve(ResolveContext{UserMessage: "登录服务器查看日志"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 match for '服务器', got %d", len(resolved))
	}

	// Should not match.
	resolved = s.Resolve(ResolveContext{UserMessage: "帮我写个贪吃蛇游戏"})
	if len(resolved) != 0 {
		t.Fatalf("expected 0 matches for unrelated message, got %d", len(resolved))
	}
}

func TestStore_FileMatchMode(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "go-rules.md", `---
inclusion: fileMatch
fileMatchPattern: '*.go'
---
# Go Rules
Use gofmt.
`)
	s := NewStore(dir, "")
	s.Load()

	// Should match.
	resolved := s.Resolve(ResolveContext{
		UserMessage:  "fix this",
		ContextFiles: []string{"main.go", "utils.py"},
	})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 match for *.go, got %d", len(resolved))
	}

	// Should not match.
	resolved = s.Resolve(ResolveContext{
		UserMessage:  "fix this",
		ContextFiles: []string{"main.py"},
	})
	if len(resolved) != 0 {
		t.Fatalf("expected 0 matches for *.py, got %d", len(resolved))
	}
}

func TestStore_ManualMode(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "special-rules.md", `---
inclusion: manual
---
# Special Rules
Only when referenced.
`)
	s := NewStore(dir, "")
	s.Load()

	// Not referenced: should not appear.
	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 0 {
		t.Fatalf("expected 0 without manual ref, got %d", len(resolved))
	}

	// Referenced: should appear.
	resolved = s.Resolve(ResolveContext{
		UserMessage: "hello",
		ManualRefs:  []string{"special-rules"},
	})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 with manual ref, got %d", len(resolved))
	}
}

func TestStore_ProjectOverridesUser(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user")
	projDir := filepath.Join(t.TempDir(), "project")

	writeSteeringFile(t, userDir, "rules.md", `---
inclusion: always
---
User version.
`)
	writeSteeringFile(t, projDir, "rules.md", `---
inclusion: always
---
Project version.
`)

	s := NewStore(userDir, projDir)
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 merged file, got %d", len(resolved))
	}
	if !strings.Contains(resolved[0].Content, "Project version") {
		t.Errorf("expected project version to win, got: %s", resolved[0].Content)
	}
	if resolved[0].Scope != ScopeProject {
		t.Errorf("expected ScopeProject, got %s", resolved[0].Scope)
	}
}

func TestStore_TokenBudgetEnforcement(t *testing.T) {
	dir := t.TempDir()

	// Create a file with ~1000 tokens of content (~3000 runes).
	bigContent := "---\ninclusion: always\npriority: 1\n---\n" + strings.Repeat("这是一段测试文本。", 300)
	writeSteeringFile(t, dir, "big.md", bigContent)

	// Create a small file.
	writeSteeringFile(t, dir, "small.md", `---
inclusion: always
priority: 2
---
Small rule.
`)

	s := NewStore(dir, "")
	s.Load()

	// With a very small context (budget will be 600 tokens).
	resolved := s.Resolve(ResolveContext{
		UserMessage:            "hello",
		EffectiveContextTokens: 20000, // 3% = 600 tokens
	})

	// big.md should be truncated to fit the 600 token budget.
	// small.md may or may not fit depending on remaining budget.
	totalTokens := 0
	for _, f := range resolved {
		totalTokens += estimateTokens(f.Content)
	}

	budget := effectiveBudget(20000)
	if totalTokens > budget {
		t.Errorf("total tokens %d exceeds budget %d", totalTokens, budget)
	}

	// big.md should be present (truncated), not skipped.
	if len(resolved) == 0 {
		t.Fatal("expected at least 1 file (big.md truncated to fit), got 0")
	}
	if resolved[0].Name != "big.md" {
		t.Errorf("expected big.md first (priority 1), got %s", resolved[0].Name)
	}
	if !strings.HasSuffix(resolved[0].Content, "[truncated]") {
		t.Error("expected big.md to be truncated")
	}
}

func TestStore_MaxAlwaysFilesLimit(t *testing.T) {
	dir := t.TempDir()

	// Create 7 always files (limit is 5).
	for i := 0; i < 7; i++ {
		name := strings.Replace("rule-X.md", "X", string(rune('a'+i)), 1)
		writeSteeringFile(t, dir, name, "---\ninclusion: always\n---\nRule content.")
	}

	s := NewStore(dir, "")
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) > MaxAlwaysFiles {
		t.Errorf("expected at most %d always files, got %d", MaxAlwaysFiles, len(resolved))
	}
}

func TestStore_NoFrontMatterDefaultsToAlways(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "plain.md", "# Plain Rules\nJust content, no front-matter.")

	s := NewStore(dir, "")
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 file (default always), got %d", len(resolved))
	}
	if resolved[0].Inclusion != InclusionAlways {
		t.Errorf("expected InclusionAlways, got %s", resolved[0].Inclusion)
	}
}

func TestStore_SkipsOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a file larger than MaxFileBytes.
	bigContent := strings.Repeat("x", MaxFileBytes+100)
	writeSteeringFile(t, dir, "huge.md", bigContent)

	s := NewStore(dir, "")
	s.Load()

	if s.FileCount() != 0 {
		t.Errorf("expected 0 files (oversized skipped), got %d", s.FileCount())
	}
}

func TestStore_EmptyDirectories(t *testing.T) {
	s := NewStore("/nonexistent/user", "/nonexistent/project")
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 0 {
		t.Errorf("expected 0 files from nonexistent dirs, got %d", len(resolved))
	}
}

func TestStore_PriorityOrdering(t *testing.T) {
	dir := t.TempDir()
	writeSteeringFile(t, dir, "low.md", "---\ninclusion: always\npriority: 200\n---\nLow priority.")
	writeSteeringFile(t, dir, "high.md", "---\ninclusion: always\npriority: 10\n---\nHigh priority.")
	writeSteeringFile(t, dir, "mid.md", "---\ninclusion: always\npriority: 50\n---\nMid priority.")

	s := NewStore(dir, "")
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 3 {
		t.Fatalf("expected 3 files, got %d", len(resolved))
	}
	if resolved[0].Name != "high.md" {
		t.Errorf("expected high.md first, got %s", resolved[0].Name)
	}
	if resolved[1].Name != "mid.md" {
		t.Errorf("expected mid.md second, got %s", resolved[1].Name)
	}
	if resolved[2].Name != "low.md" {
		t.Errorf("expected low.md third, got %s", resolved[2].Name)
	}
}

func TestEffectiveBudget(t *testing.T) {
	tests := []struct {
		name     string
		ctx      int
		expected int
	}{
		{"default 110K", 88000, MaxSteeringTokenBudget},
		{"large 200K", 160000, MaxSteeringTokenBudget},
		{"zero (fallback)", 0, MaxSteeringTokenBudget},
		{"small 32K", 25600, 768},
		{"tiny 10K", 8000, minBudgetTokens},
		{"very tiny 5K", 4000, minBudgetTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveBudget(tt.ctx)
			if got != tt.expected {
				t.Errorf("effectiveBudget(%d) = %d, want %d", tt.ctx, got, tt.expected)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"abc", 1},                           // 3 ASCII → ceil(3/4) = 1
		{"你好世界", 3},                       // 4 CJK → ceil(4/1.5) = ceil(8/3) = 3
		{strings.Repeat("a", 9), 3},          // 9 ASCII → ceil(9/4) = 3
		{strings.Repeat("你", 9), 6},          // 9 CJK → ceil(9/1.5) = ceil(18/3) = 6
		{strings.Repeat("hello ", 100), 150}, // 600 ASCII → ceil(600/4) = 150
	}
	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.expected {
			t.Errorf("estimateTokens(%q...) = %d, want %d", tt.input[:min(len(tt.input), 20)], got, tt.expected)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func TestStore_ProjectOnlyFilesNotDuplicated(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user")
	projDir := filepath.Join(t.TempDir(), "project")

	// User has file A, project has file A (override) + file B (project-only).
	writeSteeringFile(t, userDir, "shared.md", "---\ninclusion: always\n---\nUser shared.")
	writeSteeringFile(t, projDir, "shared.md", "---\ninclusion: always\n---\nProject shared.")
	writeSteeringFile(t, projDir, "extra.md", "---\ninclusion: always\n---\nProject extra.")

	s := NewStore(userDir, projDir)
	s.Load()

	resolved := s.Resolve(ResolveContext{UserMessage: "hello"})
	if len(resolved) != 2 {
		names := make([]string, len(resolved))
		for i, f := range resolved {
			names[i] = f.Name
		}
		t.Fatalf("expected 2 files (shared override + extra), got %d: %v", len(resolved), names)
	}

	// Verify no duplicates.
	seen := make(map[string]int)
	for _, f := range resolved {
		seen[f.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("file %q appeared %d times (expected 1)", name, count)
		}
	}
}

func TestSplitFrontMatter_WindowsCRLF(t *testing.T) {
	content := "---\r\ninclusion: always\r\npriority: 10\r\n---\r\n# Rules\r\nDo things.\r\n"
	fm, body := splitFrontMatter(content)
	if fm == nil {
		t.Fatal("expected front-matter to be parsed")
	}
	if fm.Inclusion != "always" {
		t.Errorf("expected inclusion=always, got %q", fm.Inclusion)
	}
	if strings.HasPrefix(body, "\r") {
		t.Errorf("body should not start with \\r: %q", body[:min(len(body), 20)])
	}
	if !strings.Contains(body, "# Rules") {
		t.Errorf("body should contain '# Rules': %q", body)
	}
}
