package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAssistantBindingFilePathIsConfinedToProfileDirectories(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	docs := filepath.Join(root, "docs")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{work, docs, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	binding := &agent.AssistantBinding{BotProfileID: "support", WorkingDirectory: work, DocumentDirectories: []string{docs}}
	if err := validateAssistantBindingFilePath(filepath.Join(work, "a.txt"), binding); err != nil {
		t.Fatalf("working directory rejected: %v", err)
	}
	if err := validateAssistantBindingFilePath(filepath.Join(docs, "help.md"), binding); err != nil {
		t.Fatalf("document directory rejected: %v", err)
	}
	if err := validateAssistantBindingFilePath(filepath.Join(outside, "secret.txt"), binding); err == nil {
		t.Fatal("outside path was accepted")
	}
}
