package enterpriseknowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestMetaDBExists(t *testing.T) {
	dir := t.TempDir()
	if MetaDBExists(dir) {
		t.Fatal("expected no meta file")
	}
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if !MetaDBExists(dir) {
		t.Fatal("expected meta file after open")
	}
	if MetaDBExists("") {
		t.Fatal("empty dataDir")
	}
}

func TestHasActiveLibrariesAndSearchSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.HasActiveLibraries() {
		t.Fatal("expected no active libs")
	}
	hits, err := c.SearchActive(context.Background(), "anything", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("want empty hits, got %d", len(hits))
	}
	// Store should still be nil (meta-only path avoided EnsureStore).
	if c.Store() != nil {
		t.Fatal("expected store not opened when no active libraries")
	}
	if err := SeedLibraryForTest(c, "lib_a", "A", "active", true); err != nil {
		t.Fatal(err)
	}
	if !c.HasActiveLibraries() {
		t.Fatal("expected active library")
	}
	if err := SeedLibraryForTest(c, "lib_b", "B", "sync_disabled", true); err != nil {
		t.Fatal(err)
	}
	// Still true because lib_a is active.
	if !c.HasActiveLibraries() {
		t.Fatal("expected still active")
	}
	ids, err := c.ActiveLibraryIDs("")
	if err != nil || len(ids) != 1 {
		t.Fatalf("active ids = %v err=%v", ids, err)
	}
}

func TestListLibrariesIncludesRevoked(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "a", "A", "active", true); err != nil {
		t.Fatal(err)
	}
	if err := SeedLibraryForTest(c, "r", "R", "revoked", true); err != nil {
		t.Fatal(err)
	}
	all, err := c.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListLibraries want 2 (incl revoked), got %d", len(all))
	}
	activeOnly, err := c.ListLibrariesActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 1 || activeOnly[0].LibraryID != "a" {
		t.Fatalf("ListLibrariesActive got %+v", activeOnly)
	}
}

func TestPurgeLibraryNotFound(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.PurgeLibrary("missing"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestPurgeLibrarySweepsNamespacedSources(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_x", "X", "active", true); err != nil {
		t.Fatal(err)
	}
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Namespaced source id as sync would write (dal_{libraryID}_…).
	if err := store.SaveSource(ctx, knowledge.Source{
		ID:     "dal_lib_x_doc1",
		Kind:   knowledge.SourceKindText,
		URI:    "memory://dal_lib_x_doc1",
		Title:  "Doc",
		Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	// Orphan without map entry — still must be swept by prefix delete.
	if err := store.SaveSource(ctx, knowledge.Source{
		ID:     "dal_lib_x_orphan",
		Kind:   knowledge.SourceKindText,
		URI:    "memory://dal_lib_x_orphan",
		Title:  "Orphan",
		Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.meta.Exec(`INSERT INTO enterprise_source_map (library_id, remote_source_id, local_source_id) VALUES ('lib_x', 'r1', 'dal_lib_x_doc1')`); err != nil {
		t.Fatal(err)
	}
	if err := c.PurgeLibrary("lib_x"); err != nil {
		t.Fatal(err)
	}
	srcs, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 100, IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if strings.HasPrefix(s.ID, "dal_lib_x_") {
			t.Fatalf("expected namespaced sources purged, still have %s", s.ID)
		}
	}
	libs, err := c.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 0 {
		t.Fatalf("expected library meta removed, got %+v", libs)
	}
}

func TestClientUserSyncAndList(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.meta.Exec(`INSERT INTO enterprise_library_state
		(library_id, name, last_rev, access_state, last_error, user_sync_enabled)
		VALUES ('lib_a', 'Alpha', 1, 'active', '', 1),
		       ('lib_b', 'Beta', 0, 'revoked', '', 1),
		       ('lib_c', 'Gamma', 2, 'sync_disabled', '', 1)`); err != nil {
		t.Fatal(err)
	}

	libs, err := c.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 3 {
		t.Fatalf("ListLibraries want 3 (incl revoked), got %d", len(libs))
	}
	activeOnly, err := c.ListLibrariesActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 2 {
		t.Fatalf("ListLibrariesActive want 2 non-revoked, got %d", len(activeOnly))
	}

	if err := c.SetUserSync("lib_a", false); err != nil {
		t.Fatal(err)
	}
	libs, _ = c.ListLibraries()
	var found bool
	for _, lib := range libs {
		if lib.LibraryID == "lib_a" {
			found = true
			if lib.UserSyncEnabled {
				t.Fatal("expected user sync disabled")
			}
		}
	}
	if !found {
		t.Fatal("lib_a missing")
	}

	ids, err := c.ActiveLibraryIDs("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["lib_a"]; !ok {
		t.Fatal("lib_a should still be active for search")
	}
	if _, ok := ids["lib_c"]; ok {
		t.Fatal("sync_disabled should not be active")
	}

	if err := c.SetUserSync("missing", false); err == nil {
		t.Fatal("expected not found")
	}

	// Paths
	if KnowledgeDBPath(dir) != filepath.Join(dir, "enterprise_knowledge.db") {
		t.Fatal("knowledge path")
	}
}

func TestMigrateLegacyMeta(t *testing.T) {
	dir := t.TempDir()
	path := MetaDBPath(dir)
	// Open via package path (creates schema with column).
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	// Re-open is idempotent.
	c2, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	var n int
	if err := c2.meta.QueryRow(`SELECT COUNT(*) FROM enterprise_library_state`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	_ = path
}
