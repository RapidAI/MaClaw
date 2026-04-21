package steering

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegration_FullWorkflow tests the complete steering lifecycle:
// user-level + project-level files, all four inclusion modes, token budget,
// and priority ordering.
func TestIntegration_FullWorkflow(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user")
	projDir := filepath.Join(t.TempDir(), "project")

	// User-level: always + contextMatch + manual files.
	writeSteeringFile(t, userDir, "core-rules.md", `---
inclusion: always
priority: 10
---
# Core Rules
Always follow these rules.
`)
	writeSteeringFile(t, userDir, "ssh-rules.md", `---
inclusion: contextMatch
contextKeywords: ['ssh', '服务器']
priority: 50
---
# SSH Rules
Use ssh tool for server operations.
`)
	writeSteeringFile(t, userDir, "go-rules.md", `---
inclusion: fileMatch
fileMatchPattern: '*.go'
priority: 60
---
# Go Rules
Use gofmt. Run go vet.
`)
	writeSteeringFile(t, userDir, "secret-rules.md", `---
inclusion: manual
priority: 70
---
# Secret Rules
Only when explicitly referenced.
`)

	// Project-level: override core-rules with project-specific version.
	writeSteeringFile(t, projDir, "core-rules.md", `---
inclusion: always
priority: 10
---
# Project Core Rules
Project-specific rules override user-level.
`)

	s := NewStore(userDir, projDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	// Test 1: Basic message — only always files.
	t.Run("basic_message", func(t *testing.T) {
		resolved := s.Resolve(ResolveContext{
			UserMessage:            "你好",
			EffectiveContextTokens: 102400,
		})
		if len(resolved) != 1 {
			t.Fatalf("expected 1 (always), got %d", len(resolved))
		}
		if !strings.Contains(resolved[0].Content, "Project-specific") {
			t.Error("expected project override to win")
		}
	})

	// Test 2: SSH message — always + contextMatch.
	t.Run("ssh_message", func(t *testing.T) {
		resolved := s.Resolve(ResolveContext{
			UserMessage:            "登录服务器查看日志",
			EffectiveContextTokens: 102400,
		})
		if len(resolved) != 2 {
			t.Fatalf("expected 2 (always + ssh), got %d", len(resolved))
		}
		names := make([]string, len(resolved))
		for i, f := range resolved {
			names[i] = f.Name
		}
		// Priority order: core-rules(10) < ssh-rules(50)
		if names[0] != "core-rules.md" || names[1] != "ssh-rules.md" {
			t.Errorf("unexpected order: %v", names)
		}
	})

	// Test 3: Go file context — always + fileMatch.
	t.Run("go_file_context", func(t *testing.T) {
		resolved := s.Resolve(ResolveContext{
			UserMessage:            "fix this bug",
			ContextFiles:           []string{"main.go"},
			EffectiveContextTokens: 102400,
		})
		if len(resolved) != 2 {
			t.Fatalf("expected 2 (always + go), got %d", len(resolved))
		}
	})

	// Test 4: Manual reference — always + manual.
	t.Run("manual_reference", func(t *testing.T) {
		resolved := s.Resolve(ResolveContext{
			UserMessage:            "apply rules",
			ManualRefs:             []string{"secret-rules"},
			EffectiveContextTokens: 102400,
		})
		if len(resolved) != 2 {
			t.Fatalf("expected 2 (always + manual), got %d", len(resolved))
		}
	})

	// Test 5: Combined — SSH message + Go file + manual ref.
	t.Run("combined", func(t *testing.T) {
		resolved := s.Resolve(ResolveContext{
			UserMessage:            "登录服务器部署 Go 代码",
			ContextFiles:           []string{"deploy.go"},
			ManualRefs:             []string{"secret-rules"},
			EffectiveContextTokens: 102400,
		})
		// always(1) + contextMatch(1) + fileMatch(1) + manual(1) = 4
		if len(resolved) != 4 {
			names := make([]string, len(resolved))
			for i, f := range resolved {
				names[i] = f.Name
			}
			t.Fatalf("expected 4 combined, got %d: %v", len(resolved), names)
		}
	})

	// Test 6: EnsureDefaults creates files.
	t.Run("ensure_defaults", func(t *testing.T) {
		freshDir := filepath.Join(t.TempDir(), "fresh")
		if err := EnsureDefaults(freshDir); err != nil {
			t.Fatal(err)
		}
		s2 := NewStore(freshDir, "")
		s2.Load()
		if s2.FileCount() < 2 {
			t.Errorf("expected at least 2 default files, got %d", s2.FileCount())
		}
	})
}
