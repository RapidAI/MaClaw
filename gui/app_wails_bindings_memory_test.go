package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestDeleteMemoriesDeletesContentInOneBatch(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	for _, content := range []string{"remove first", "keep this", "remove second"} {
		if err := store.Save(memory.Entry{Content: content, Category: memory.CategoryUserFact}); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.List("", "")
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}

	deleted, err := app.DeleteMemories([]string{entries[0].ID, "", entries[2].ID, entries[0].ID})
	if err != nil {
		t.Fatalf("DeleteMemories() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteMemories() deleted = %d, want 2", deleted)
	}
	remaining := store.List("", "")
	if len(remaining) != 1 || remaining[0].Content != "keep this" {
		t.Fatalf("remaining entries = %#v, want only the unselected content", remaining)
	}
}

func TestDeleteMemoriesImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "delete this agent memory", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	entry := store.List("", "")[0]
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	// The frozen static section is deliberately content-free (recall-on-demand
	// via memory(action: recall), keeping the prompt prefix stable); entry
	// bodies never appear in it. The invariant under test is the invalidation
	// wiring: a mutation must drop the cached snapshot.
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if _, err := app.DeleteMemories([]string{entry.ID}); err != nil {
		t.Fatalf("DeleteMemories() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after delete: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if strings.Contains(fresh, entry.Content) {
		t.Fatalf("fresh agent memory still contains deleted content: %q", fresh)
	}
}

func TestDeleteMemoriesImmediatelyInvalidatesGatewayAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "delete this gateway agent memory", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	entry := store.List("", "")[0]
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}
	app := &App{
		memoryStore:   store,
		weixinGateway: &weixinGatewayManager{localHandler: handler},
		testHomeDir:   t.TempDir(),
	}

	if _, err := app.DeleteMemories([]string{entry.ID}); err != nil {
		t.Fatalf("DeleteMemories() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached gateway agent memory remains after delete: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if strings.Contains(fresh, entry.Content) {
		t.Fatalf("fresh gateway agent memory still contains deleted content: %q", fresh)
	}
}

func TestRestoreMemoryBackupImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "memories.json")
	store, err := memory.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "memory before restore", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}

	backupName := "restore_snapshot.json"
	backupEntries := []memory.Entry{{ID: "restored-memory", Content: "memory after restore", Category: memory.CategoryUserFact}}
	backupData, err := json.Marshal(backupEntries)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(filepath.Dir(storePath), "memory_backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, backupName), backupData, 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	if err := app.RestoreMemoryBackup(backupName); err != nil {
		t.Fatalf("RestoreMemoryBackup() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after restore: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if fresh == "" {
		t.Fatal("static memory snapshot not rebuilt after restore")
	}
	if strings.Contains(fresh, "memory before restore") {
		t.Fatalf("fresh agent memory still contains pre-restore content: %q", fresh)
	}
	// Entry bodies reach the model via recall, not the prompt: assert the
	// restored content at the store level.
	if entries := store.List("", ""); len(entries) != 1 || entries[0].Content != "memory after restore" {
		t.Fatalf("store after restore = %#v, want restored entry", entries)
	}
}

func TestMemoryPipelineCompletionImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "memory before pipeline maintenance", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}

	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	guiMemoryEventEmitter{app: app}.Emit("memory:pipeline_completed", nil)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after pipeline completion: %q", cached)
	}
}

func TestCompressMemoriesImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	for _, content := range []string{
		"duplicate memory from before compression with enough detail to be recognized as a longer matching entry",
		"duplicate memory from before compression with enough detail to be recognized as a longer matching entry and supplementary wording",
	} {
		if err := store.Save(memory.Entry{Content: content, Category: memory.CategoryUserFact}); err != nil {
			t.Fatal(err)
		}
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}

	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	maintenance := memory.NewMaintenance(store, nil, nil)
	app.memoryMaintenance = maintenance
	app.memoryCompressor = maintenance.Compressor()
	if _, err := app.CompressMemories(); err != nil {
		t.Fatalf("CompressMemories() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after compression: %q", cached)
	}
}

func TestSaveAndUpdateMemoryImmediatelyInvalidateAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if err := app.SaveMemory("first agent fact", string(memory.CategoryUserFact), nil); err != nil {
		t.Fatalf("SaveMemory() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after save: %q", cached)
	}
	entry := store.List("", "")[0]
	if fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID); fresh == "" {
		t.Fatal("static memory snapshot not rebuilt after save")
	}
	// Saved content reaches the model via recall tools; the prompt section
	// stays content-free, so assert the fact at the store level.
	if entries := store.List("", ""); len(entries) != 1 || entries[0].Content != "first agent fact" {
		t.Fatalf("store after save = %#v, want saved fact", entries)
	}

	if err := app.UpdateMemory(entry.ID, "updated agent fact", string(memory.CategoryUserFact), nil); err != nil {
		t.Fatalf("UpdateMemory() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after update: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if fresh == "" {
		t.Fatal("static memory snapshot not rebuilt after update")
	}
	if strings.Contains(fresh, entry.Content) {
		t.Fatalf("fresh agent memory still contains pre-update content: %q", fresh)
	}
	if entries := store.List("", ""); len(entries) != 1 || entries[0].Content != "updated agent fact" {
		t.Fatalf("store after update = %#v, want updated fact", entries)
	}
}

func TestRestoreArchiveMemoryImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	archived := memory.Entry{ID: "archived-agent-memory", Content: "restored archive agent memory", Category: memory.CategoryUserFact}
	if err := store.Archive().AddDurable(archived); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if err := app.RestoreArchiveMemory(archived.ID); err != nil {
		t.Fatalf("RestoreArchiveMemory() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after archive restore: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if fresh == "" {
		t.Fatal("static memory snapshot not rebuilt after archive restore")
	}
	// Restored content reaches the model via recall; assert it at the store level.
	if entries := store.List("", ""); len(entries) != 1 || entries[0].Content != archived.Content {
		t.Fatalf("store after archive restore = %#v, want restored entry", entries)
	}
}

func TestUserDataMigrationMemoryReplacementImmediatelyInvalidatesAgentMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "memory before migration replacement", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached == "" {
		t.Fatal("precondition: static memory snapshot not built")
	}
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if err := app.replaceUserDataMigrationMemoryEntries([]memory.Entry{{
		ID:       "memory-after-migration",
		Content:  "memory after migration replacement",
		Category: memory.CategoryUserFact,
	}}); err != nil {
		t.Fatalf("replaceUserDataMigrationMemoryEntries() error = %v", err)
	}
	if cached := handler.cachedStaticMemorySnapshot(desktopUserID); cached != "" {
		t.Fatalf("cached agent memory remains after migration replacement: %q", cached)
	}
	fresh, _ := handler.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if fresh == "" {
		t.Fatal("static memory snapshot not rebuilt after migration replacement")
	}
	if strings.Contains(fresh, "memory before migration replacement") {
		t.Fatalf("fresh agent memory still contains pre-migration content: %q", fresh)
	}
	if entries := store.List("", ""); len(entries) != 1 || entries[0].Content != "memory after migration replacement" {
		t.Fatalf("store after migration replacement = %#v, want replacement entry", entries)
	}
}

func TestDeleteMemoriesInvalidatesAllCachedAgentOwners(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	owners := []string{desktopUserID, desktopUserID + ":D:/work/project"}
	contents := []string{"desktop fact removed everywhere", "project fact removed everywhere"}
	for i, ownerID := range owners {
		if err := store.Save(memory.Entry{Content: contents[i], Category: memory.CategoryUserFact, OwnerID: ownerID}); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.List("", "")
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	handler := &IMMessageHandler{memoryStore: store}
	for _, ownerID := range owners {
		handler.WarmFrozenMemorySnapshot(ownerID)
		if cached := handler.cachedStaticMemorySnapshot(ownerID); cached == "" {
			t.Fatalf("precondition: static memory snapshot for %q not built", ownerID)
		}
	}
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if _, err := app.DeleteMemories(ids); err != nil {
		t.Fatalf("DeleteMemories() error = %v", err)
	}
	for i, ownerID := range owners {
		if cached := handler.cachedStaticMemorySnapshot(ownerID); cached != "" {
			t.Fatalf("cached agent memory for %q remains after delete: %q", ownerID, cached)
		}
		fresh, _ := handler.loadOrBuildStaticMemorySnapshot(ownerID)
		if strings.Contains(fresh, contents[i]) {
			t.Fatalf("fresh agent memory for %q still contains deleted content: %q", ownerID, fresh)
		}
	}
}

func TestDeleteMemoriesRejectsAnEmptySelection(t *testing.T) {
	app := &App{}
	if _, err := app.DeleteMemories([]string{"", "  "}); err == nil {
		t.Fatal("DeleteMemories() error = nil, want empty selection error")
	}
}

func TestDeleteMemoriesIsAtomicWhenAnEntryIsMissing(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	for _, content := range []string{"first", "second"} {
		if err := store.Save(memory.Entry{Content: content, Category: memory.CategoryUserFact}); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.List("", "")
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}

	if _, err := app.DeleteMemories([]string{entries[0].ID, "missing-memory-id"}); err == nil {
		t.Fatal("DeleteMemories() error = nil, want missing entry error")
	}
	remaining := store.List("", "")
	if len(remaining) != 2 {
		t.Fatalf("remaining entries = %#v, want all entries retained after failed atomic batch", remaining)
	}
}

func TestDeleteMemoriesReportsOnlyCommittedDeletions(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "only entry", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	entry := store.List("", "")[0]
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}

	deleted, err := app.DeleteMemories([]string{entry.ID, "missing-memory-id"})
	if err == nil {
		t.Fatal("DeleteMemories() error = nil, want missing entry error")
	}
	if deleted != 0 {
		t.Fatalf("DeleteMemories() deleted = %d, want 0 when no mutation committed", deleted)
	}
	if remaining := store.List("", ""); len(remaining) != 1 || remaining[0].ID != entry.ID {
		t.Fatalf("remaining entries = %#v, want original entry retained", remaining)
	}
}

func TestDeleteMemoriesInvalidatesFrozenMemorySnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "delete during frozen snapshot", Category: memory.CategoryTaskArtifact}); err != nil {
		t.Fatal(err)
	}
	entry := store.List("", "")[0]
	ownerID := desktopUserID + ":D:/work/project"
	handler := &IMMessageHandler{memoryStore: store}
	handler.frozenMemorySnapshots.Store(ownerID, "stale user-fact catalog")
	before := handler.snapshotGeneration(ownerID)
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}

	if _, err := app.DeleteMemories([]string{entry.ID}); err != nil {
		t.Fatalf("DeleteMemories() error = %v", err)
	}
	if after := handler.snapshotGeneration(ownerID); after <= before {
		t.Fatalf("frozen snapshot generation = %d, want > %d after delete", after, before)
	}
	if handler.isCurrentMemoryPromptGeneration(ownerID, before) {
		t.Fatal("a snapshot begun before deletion is still considered current")
	}
	if _, ok := handler.frozenMemorySnapshots.Load(ownerID); ok {
		t.Fatal("expected frozen snapshot deleted after DeleteMemories")
	}
}
