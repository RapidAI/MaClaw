package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// TestIntegration_ImportFile_ThenSearch imports a real .txt file into the
// knowledge store, then searches for content and verifies the result is found.
// Validates: Requirements 1.1, 3.1
func TestIntegration_ImportFile_ThenSearch(t *testing.T) {
	ctx := context.Background()

	// Create a temp directory with a .txt file containing known content.
	tmpDir := t.TempDir()
	txtContent := "The quick brown fox jumps over the lazy dog. This is a unique test phrase for knowledge base integration testing."
	txtPath := filepath.Join(tmpDir, "sample_document.txt")
	if err := os.WriteFile(txtPath, []byte(txtContent), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create a knowledge store in a temp DB.
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create knowledge store: %v", err)
	}
	defer store.Close()

	// Import the file.
	req := knowledge.DirectoryImportRequest{
		RootPath:  tmpDir,
		Recursive: false,
	}
	result, err := store.ImportFiles(ctx, req, []string{txtPath})
	if err != nil {
		t.Fatalf("ImportFiles failed: %v", err)
	}

	// Verify import succeeded.
	if result.ImportedFiles == 0 {
		t.Fatalf("expected at least 1 imported file, got ImportedFiles=%d (failed=%d, skipped=%d)",
			result.ImportedFiles, result.FailedFiles, result.SkippedFiles)
	}

	// Search for content from the imported file.
	searchResults, err := store.Search(ctx, knowledge.SearchOptions{
		Query: "quick brown fox",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Verify search returns at least one result containing our content.
	if len(searchResults) == 0 {
		t.Fatal("expected at least one search result, got 0")
	}

	found := false
	for _, sr := range searchResults {
		if strings.Contains(sr.Snippet, "quick brown fox") ||
			strings.Contains(sr.Snippet, "unique test phrase") ||
			strings.Contains(sr.Summary, "quick brown fox") ||
			strings.Contains(sr.Summary, "unique test phrase") ||
			strings.Contains(sr.CardTitle, "quick brown fox") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("search results do not contain expected content; got %d results", len(searchResults))
		for i, sr := range searchResults {
			t.Logf("  result[%d]: type=%s score=%.2f snippet=%q", i, sr.ResultType, sr.Score, sr.Snippet)
		}
	}
}

// TestIntegration_ImportDirectory_MixedFormats imports a directory with
// multiple file formats (.txt, .md) and verifies the summary counts.
// Validates: Requirements 3.1
func TestIntegration_ImportDirectory_MixedFormats(t *testing.T) {
	ctx := context.Background()

	// Create a temp directory with mixed format files.
	tmpDir := t.TempDir()

	// Create .txt file
	txtContent := "This is a plain text document about machine learning algorithms."
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte(txtContent), 0o644); err != nil {
		t.Fatalf("failed to write txt file: %v", err)
	}

	// Create .md file
	mdContent := "# Architecture Design\n\nThis document describes the system architecture for the knowledge base module.\n\n## Components\n\n- SQLite Store\n- FTS5 Index\n- Auto-Recall Engine"
	if err := os.WriteFile(filepath.Join(tmpDir, "design.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatalf("failed to write md file: %v", err)
	}

	// Create an unsupported file (should be skipped)
	if err := os.WriteFile(filepath.Join(tmpDir, "image.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0o644); err != nil {
		t.Fatalf("failed to write png file: %v", err)
	}

	// Create a knowledge store in a temp DB.
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create knowledge store: %v", err)
	}
	defer store.Close()

	// Import the directory.
	req := knowledge.DirectoryImportRequest{
		RootPath:  tmpDir,
		Recursive: true,
	}
	result, err := store.ImportDirectory(ctx, req)
	if err != nil {
		t.Fatalf("ImportDirectory failed: %v", err)
	}

	// Verify summary counts.
	// We expect at least 2 files imported (.txt and .md), and .png skipped.
	if result.TotalFiles < 2 {
		t.Errorf("expected TotalFiles >= 2, got %d", result.TotalFiles)
	}
	if result.ImportedFiles < 2 {
		t.Errorf("expected ImportedFiles >= 2, got %d (failed=%d, skipped=%d)",
			result.ImportedFiles, result.FailedFiles, result.SkippedFiles)
	}

	// The .png file should be skipped (unsupported format).
	// TotalFiles only counts files matching the extension filter, so .png
	// may not even appear in TotalFiles. Verify imported >= 2 is sufficient.
	t.Logf("ImportDirectory result: total=%d imported=%d skipped=%d failed=%d",
		result.TotalFiles, result.ImportedFiles, result.SkippedFiles, result.FailedFiles)
}

// TestIntegration_SystemPrompt_HasKnowledgeBase verifies that when a TUIApp
// has a non-nil knowledgeStore, buildSystemPromptDeps sets HasKnowledgeBase=true
// and the resulting system prompt contains knowledge tool guidance text.
// Validates: Requirements 7.1
func TestIntegration_SystemPrompt_HasKnowledgeBase(t *testing.T) {
	// Create a real knowledge store (even if empty, non-nil means HasKnowledgeBase=true).
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create knowledge store: %v", err)
	}
	defer store.Close()

	// Create a TUIApp with the knowledge store set.
	app := &TUIApp{
		knowledgeStore: store,
	}

	// Build system prompt deps.
	deps := app.buildSystemPromptDeps()

	// Verify HasKnowledgeBase is true.
	if !deps.HasKnowledgeBase {
		t.Fatal("expected HasKnowledgeBase=true when knowledgeStore is non-nil")
	}

	// Build the full system prompt and verify it contains knowledge base rules.
	prompt := agent.BuildSystemPrompt(deps, "test message", true)

	if !strings.Contains(prompt, agent.PromptKnowledgeBaseRules) {
		t.Error("system prompt does not contain PromptKnowledgeBaseRules when HasKnowledgeBase=true")
	}

	// Verify the knowledge base rules section contains expected guidance text.
	if !strings.Contains(prompt, "知识库外脑规则") {
		t.Error("system prompt does not contain '知识库外脑规则' section header")
	}

	// Also verify the negative case: nil store means HasKnowledgeBase=false.
	appNoKB := &TUIApp{
		knowledgeStore: nil,
	}
	depsNoKB := appNoKB.buildSystemPromptDeps()
	if depsNoKB.HasKnowledgeBase {
		t.Fatal("expected HasKnowledgeBase=false when knowledgeStore is nil")
	}

	promptNoKB := agent.BuildSystemPrompt(depsNoKB, "test message", true)
	if strings.Contains(promptNoKB, "知识库外脑规则") {
		t.Error("system prompt should NOT contain '知识库外脑规则' when HasKnowledgeBase=false")
	}
}
