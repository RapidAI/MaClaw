package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// TestToolKnowledgeSearch_NilStore verifies that toolKnowledgeSearch returns a
// descriptive error when the knowledge store is not configured (nil).
func TestToolKnowledgeSearch_NilStore(t *testing.T) {
	app := &TUIApp{} // knowledgeStore is nil
	result := app.toolKnowledgeSearch(map[string]interface{}{"query": "test"})
	if !strings.Contains(result, "Error: knowledge base is not configured") {
		t.Fatalf("expected nil-store error, got: %q", result)
	}
	if !strings.Contains(result, "maclaw-tui knowledge import") {
		t.Fatalf("nil-store error should include import guidance, got: %q", result)
	}
}

// TestToolKnowledgeContextPack_NilStore verifies that toolKnowledgeContextPack
// returns a descriptive error when the knowledge store is not configured.
func TestToolKnowledgeContextPack_NilStore(t *testing.T) {
	app := &TUIApp{}
	result := app.toolKnowledgeContextPack(map[string]interface{}{"query": "test"})
	if !strings.Contains(result, "Error: knowledge base is not configured") {
		t.Fatalf("expected nil-store error, got: %q", result)
	}
}

// TestToolKnowledgeSaveText_NilStore verifies that toolKnowledgeSaveText
// returns a descriptive error when the knowledge store is not configured.
func TestToolKnowledgeSaveText_NilStore(t *testing.T) {
	app := &TUIApp{}
	result := app.toolKnowledgeSaveText(map[string]interface{}{"text": "hello"})
	if !strings.Contains(result, "Error: knowledge base is not configured") {
		t.Fatalf("expected nil-store error, got: %q", result)
	}
}

// TestToolKnowledgeSaveURL_NilStore verifies that toolKnowledgeSaveURL
// returns a descriptive error when the knowledge store is not configured.
func TestToolKnowledgeSaveURL_NilStore(t *testing.T) {
	app := &TUIApp{}
	result := app.toolKnowledgeSaveURL(map[string]interface{}{"url": "https://example.com"})
	if !strings.Contains(result, "Error: knowledge base is not configured") {
		t.Fatalf("expected nil-store error, got: %q", result)
	}
}

// TestToolKnowledgeSaveText_EmptyTextWithStore verifies that when the store
// is configured but text is empty, the handler returns "text parameter is required".
func TestToolKnowledgeSaveText_EmptyTextWithStore(t *testing.T) {
	store := openTestKnowledgeStore(t)
	defer store.Close()

	app := &TUIApp{knowledgeStore: store}

	// No text or content parameter
	result := app.toolKnowledgeSaveText(map[string]interface{}{})
	if !strings.Contains(result, "Error: text parameter is required") {
		t.Fatalf("expected empty-text error, got: %q", result)
	}

	// Explicit empty string for text
	result = app.toolKnowledgeSaveText(map[string]interface{}{"text": ""})
	if !strings.Contains(result, "Error: text parameter is required") {
		t.Fatalf("expected empty-text error for empty string, got: %q", result)
	}

	// Empty text and empty content alias
	result = app.toolKnowledgeSaveText(map[string]interface{}{"text": "", "content": ""})
	if !strings.Contains(result, "Error: text parameter is required") {
		t.Fatalf("expected empty-text error for empty text+content, got: %q", result)
	}
}

// TestToolKnowledgeSearch_EmptyQuery verifies that toolKnowledgeSearch
// returns an error when the query parameter is empty.
func TestToolKnowledgeSearch_EmptyQuery(t *testing.T) {
	store := openTestKnowledgeStore(t)
	defer store.Close()

	app := &TUIApp{knowledgeStore: store}

	result := app.toolKnowledgeSearch(map[string]interface{}{"query": ""})
	if !strings.Contains(result, "Error: query parameter is required") {
		t.Fatalf("expected empty-query error, got: %q", result)
	}
}

// TestToolKnowledgeSearch_DelegatesToStore verifies that toolKnowledgeSearch
// correctly delegates to the store's Search method. On an empty store, it
// should return "No results found".
func TestToolKnowledgeSearch_DelegatesToStore(t *testing.T) {
	store := openTestKnowledgeStore(t)
	defer store.Close()

	app := &TUIApp{knowledgeStore: store}

	// Search on an empty store should return "No results found"
	result := app.toolKnowledgeSearch(map[string]interface{}{"query": "nonexistent topic"})
	if !strings.Contains(result, "No results found") {
		t.Fatalf("expected no-results message for empty store, got: %q", result)
	}
}

// TestToolKnowledgeSaveURL_EmptyURL verifies that toolKnowledgeSaveURL
// returns an error when the url parameter is empty.
func TestToolKnowledgeSaveURL_EmptyURL(t *testing.T) {
	store := openTestKnowledgeStore(t)
	defer store.Close()

	app := &TUIApp{knowledgeStore: store}

	result := app.toolKnowledgeSaveURL(map[string]interface{}{"url": ""})
	if !strings.Contains(result, "Error: url parameter is required") {
		t.Fatalf("expected empty-url error, got: %q", result)
	}
}

// TestToolKnowledgeContextPack_EmptyQuery verifies that toolKnowledgeContextPack
// returns an error when the query parameter is empty.
func TestToolKnowledgeContextPack_EmptyQuery(t *testing.T) {
	store := openTestKnowledgeStore(t)
	defer store.Close()

	app := &TUIApp{knowledgeStore: store}

	result := app.toolKnowledgeContextPack(map[string]interface{}{"query": ""})
	if !strings.Contains(result, "Error: query parameter is required") {
		t.Fatalf("expected empty-query error, got: %q", result)
	}
}

// openTestKnowledgeStore creates a temporary SQLite knowledge store for testing.
func openTestKnowledgeStore(t *testing.T) *knowledge.SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "knowledge.db")
	// Create the file so NewSQLiteStore can open it.
	if err := os.WriteFile(dbPath, nil, 0644); err != nil {
		t.Fatalf("create test DB file: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open test knowledge store: %v", err)
	}
	return store
}
