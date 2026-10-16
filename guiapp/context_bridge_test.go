package guiapp

import (
	"strings"
	"testing"
	"time"
)

func TestContextBridge_ExtractFromEvents_FileChange(t *testing.T) {
	b := NewContextBridge()
	events := []ImportantEvent{
		{Type: "file.change", Summary: "main.go", SessionID: "s1", CreatedAt: time.Now().Unix()},
		{Type: "file.create", Summary: "new.go", SessionID: "s1", CreatedAt: time.Now().Unix()},
		{Type: "file.delete", Summary: "old.go", SessionID: "s1", CreatedAt: time.Now().Unix()},
	}
	b.ExtractFromEvents("/project", events)
	ctx := b.GetContext("/project")
	if ctx == nil {
		t.Fatal("context is nil")
	}
	if len(ctx.FileChanges) != 3 {
		t.Fatalf("FileChanges len = %d, want 3", len(ctx.FileChanges))
	}
	if ctx.FileChanges[0].Action != "modify" {
		t.Errorf("file.change action = %s, want modify", ctx.FileChanges[0].Action)
	}
	if ctx.FileChanges[1].Action != "create" {
		t.Errorf("file.create action = %s, want create", ctx.FileChanges[1].Action)
	}
	if ctx.FileChanges[2].Action != "delete" {
		t.Errorf("file.delete action = %s, want delete", ctx.FileChanges[2].Action)
	}
}

func TestContextBridge_ExtractFromEvents_Command(t *testing.T) {
	b := NewContextBridge()
	events := []ImportantEvent{
		{Type: "command.execute", Summary: "git commit -m 'fix'", SessionID: "s1", CreatedAt: time.Now().Unix()},
		{Type: "command.execute", Summary: "echo hello", SessionID: "s1", CreatedAt: time.Now().Unix()},
	}
	b.ExtractFromEvents("/project", events)
	ctx := b.GetContext("/project")
	if len(ctx.Decisions) != 1 {
		t.Errorf("Decisions len = %d, want 1 (only significant commands)", len(ctx.Decisions))
	}
}

func TestContextBridge_ExtractFromEvents_MaxRecords(t *testing.T) {
	b := NewContextBridge()
	events := make([]ImportantEvent, 150)
	for i := range events {
		events[i] = ImportantEvent{Type: "file.change", Summary: "file.go", CreatedAt: time.Now().Unix()}
	}
	b.ExtractFromEvents("/project", events)
	ctx := b.GetContext("/project")
	if len(ctx.FileChanges) > maxContextRecords {
		t.Errorf("FileChanges len = %d, want <= %d", len(ctx.FileChanges), maxContextRecords)
	}
}

func TestContextBridge_BuildContextPrompt_Empty(t *testing.T) {
	b := NewContextBridge()
	prompt := b.BuildContextPrompt("/nonexistent")
	if prompt != "" {
		t.Errorf("expected empty prompt for nonexistent project, got %q", prompt)
	}
}

func TestContextBridge_BuildContextPrompt_WithData(t *testing.T) {
	b := NewContextBridge()
	events := []ImportantEvent{
		{Type: "file.change", Summary: "app.go", CreatedAt: time.Now().Unix()},
		{Type: "command.execute", Summary: "git commit -m 'init'", CreatedAt: time.Now().Unix()},
	}
	b.ExtractFromEvents("/project", events)
	b.AddNote("/project", "这是一个 Go 项目")

	prompt := b.BuildContextPrompt("/project")
	if !strings.Contains(prompt, "项目上下文") {
		t.Error("prompt missing header")
	}
	if !strings.Contains(prompt, "app.go") {
		t.Error("prompt missing file change")
	}
	if !strings.Contains(prompt, "git commit") {
		t.Error("prompt missing decision")
	}
	if !strings.Contains(prompt, "Go 项目") {
		t.Error("prompt missing user note")
	}
}

func TestContextBridge_AddNote(t *testing.T) {
	b := NewContextBridge()
	b.AddNote("/project", "note1")
	b.AddNote("/project", "note2")
	ctx := b.GetContext("/project")
	if len(ctx.Notes) != 2 {
		t.Errorf("Notes len = %d, want 2", len(ctx.Notes))
	}
}

func TestContextBridge_AddNote_MaxLimit(t *testing.T) {
	b := NewContextBridge()
	for i := 0; i < 60; i++ {
		b.AddNote("/project", "note")
	}
	ctx := b.GetContext("/project")
	if len(ctx.Notes) > 50 {
		t.Errorf("Notes len = %d, want <= 50", len(ctx.Notes))
	}
}

func TestIsSignificantCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git commit -m 'fix'", true},
		{"npm install express", true},
		{"go mod tidy", true},
		{"make build", true},
		{"echo hello", false},
		{"ls -la", false},
		{"deploy to production", true},
	}
	for _, tt := range tests {
		got := isSignificantCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("isSignificantCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
