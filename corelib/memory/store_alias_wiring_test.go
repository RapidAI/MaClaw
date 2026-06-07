package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreAliasWiring_ExpansionProducesAdditionalBM25Hits verifies that the
// alias index wiring in RecallDynamic produces additional BM25 hits when
// querying by an alias registered during SaveWithContext.
//
// Scenario:
// 1. Save an entry about "api.rapidai.tech" with contextHint containing "4090服务器"
// 2. Query using "4090服务器" — without alias expansion, BM25 would not match
//    "api.rapidai.tech" in the content. With alias expansion, the query expands
//    to include "api.rapidai.tech" which matches via BM25.
func TestStoreAliasWiring_ExpansionProducesAdditionalBM25Hits(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save an entry about the server with context providing the alias.
	entry := Entry{
		Content:  "SSH server api.rapidai.tech port 22 user root, GPU is NVIDIA RTX 4090",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"api.rapidai.tech"},
	}
	contextHint := "用户称这台服务器为4090服务器，主机名是api.rapidai.tech"

	if err := store.SaveWithContext(entry, contextHint); err != nil {
		t.Fatalf("SaveWithContext: %v", err)
	}

	// Verify the alias index was populated by the SaveWithContext call.
	if store.aliasIndex.Len() == 0 {
		t.Fatal("aliasIndex should have been populated by SaveWithContext")
	}

	// Verify that querying by the alias "4090服务器" finds the entry.
	// This relies on alias expansion in RecallDynamic augmenting the BM25 multi-query set.
	results := store.RecallDynamic("4090服务器", "", "", "")
	if len(results) == 0 {
		t.Fatal("RecallDynamic with alias query should return results via alias expansion")
	}

	found := false
	for _, r := range results {
		if r.Content == entry.Content {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find the server entry via alias '4090服务器' query expansion")
	}

	// Also verify rebuild populates alias index after store reload.
	store.Stop()
	store2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	defer store2.Stop()

	if store2.aliasIndex.Len() == 0 {
		t.Error("aliasIndex should be populated after store reload (via rebuildDerivedIndexesLocked)")
	}

	_ = os.RemoveAll(dir)
}

// TestStoreAliasWiring_AccessorsNonNil verifies that all multi-page recall
// accessors return non-nil values after NewStore.
func TestStoreAliasWiring_AccessorsNonNil(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	if store.Paginator() == nil {
		t.Error("Paginator() should not be nil")
	}
	if store.ScrollSessions() == nil {
		t.Error("ScrollSessions() should not be nil")
	}
	if store.PageIdx() == nil {
		t.Error("PageIdx() should not be nil")
	}
	if store.AliasIdx() == nil {
		t.Error("AliasIdx() should not be nil")
	}
}
