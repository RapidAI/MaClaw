package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAliasWiring_ExplicitAliasSurvivesReloadAndStaysOwnerScoped(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	entry := Entry{
		Content:  "SSH server api.rapidai.tech port 22 user root, GPU is NVIDIA RTX 4090",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"api.rapidai.tech", ExplicitAliasTag("4090服务器", "api.rapidai.tech")},
		OwnerID:  "user-a",
	}
	contextHint := "用户称这台服务器为4090服务器，主机名是api.rapidai.tech"
	if err := store.SaveWithContext(entry, contextHint); err != nil {
		t.Fatalf("SaveWithContext: %v", err)
	}

	if got := store.aliasIndex.ExpandForOwner([]string{"4090服务器"}, "user-b"); len(got) != 0 {
		t.Fatalf("other owner expanded aliases: %v", got)
	}
	results := store.RecallDynamic("4090服务器", "", "", "user-a")
	found := false
	for _, r := range results {
		if r.Content == entry.Content {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("RecallDynamic should find the server entry via the explicit alias")
	}

	store.Stop()
	store2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	defer store2.Stop()
	if got := store2.aliasIndex.ExpandForOwner([]string{"4090服务器"}, "user-a"); len(got) != 1 || got[0] != "api.rapidai.tech" {
		t.Fatalf("explicit alias missing after reload: %v", got)
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
