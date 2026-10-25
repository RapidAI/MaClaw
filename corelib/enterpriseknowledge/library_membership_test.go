package enterpriseknowledge

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func countLibraryMemberships(t *testing.T, dataDir, sourceID, libraryID string) int {
	t.Helper()
	db, err := sql.Open("sqlite", KnowledgeDBPath(dataDir)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = 'library' AND scope_key = ?`, sourceID, libraryID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func libraryMembershipCreatedAt(t *testing.T, dataDir, sourceID, libraryID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", KnowledgeDBPath(dataDir)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var created string
	if err := db.QueryRow(`SELECT created_at FROM knowledge_memberships WHERE source_id = ? AND kind = 'library' AND scope_key = ?`, sourceID, libraryID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestLibraryMembershipBackfillAssignsOnce(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	seed, err := knowledge.NewSQLiteStore(KnowledgeDBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := seed.SaveSource(ctx, knowledge.Source{
		ID: "dal_lib_once_src", Kind: knowledge.SourceKindText, URI: "memory://once",
		Title: "Once", ContentHash: "once", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.meta.Exec(`INSERT INTO enterprise_source_map (library_id, remote_source_id, local_source_id) VALUES (?, ?, ?), (?, ?, ?)`,
		"lib_once", "remote", "dal_lib_once_src", "lib_once", "missing", "dal_lib_once_missing"); err != nil {
		t.Fatal(err)
	}
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	if n := countLibraryMemberships(t, dir, "dal_lib_once_src", "lib_once"); n != 1 {
		t.Fatalf("assigned memberships = %d", n)
	}
	if n := countLibraryMemberships(t, dir, "dal_lib_once_missing", "lib_once"); n != 0 {
		t.Fatalf("missing source memberships = %d", n)
	}
	created := libraryMembershipCreatedAt(t, dir, "dal_lib_once_src", "lib_once")
	if _, err := c.EnsureStore(); err != nil {
		t.Fatal(err)
	}
	if again := libraryMembershipCreatedAt(t, dir, "dal_lib_once_src", "lib_once"); again != created {
		t.Fatalf("created_at changed from %q to %q", created, again)
	}
	done, err := store.LibraryMembershipBackfillCompleted(ctx)
	if err != nil || !done {
		t.Fatalf("backfill done=%v err=%v", done, err)
	}
}

func TestSearchActiveSetsLibraryID(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib1", "One", "active", true); err != nil {
		t.Fatal(err)
	}
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, item := range []knowledge.TextSaveRequest{
		{ForceID: "dal_lib1_alpha", Text: "EnterpriseLibraryToken alpha policy", Title: "Alpha", DistillMode: knowledge.DistillModeRules},
		{ForceID: "legacy-note", Text: "EnterpriseLibraryToken legacy policy", Title: "Legacy", DistillMode: knowledge.DistillModeRules},
	} {
		if _, err := store.SaveText(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := c.SearchActive(ctx, "EnterpriseLibraryToken", "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, hit := range hits {
		got[hit.Source.ID] = hit.LibraryID
	}
	if got["dal_lib1_alpha"] != "lib1" || got["legacy-note"] != "lib1" {
		t.Fatalf("library ids = %#v", got)
	}
	for _, hit := range hits {
		if strings.HasPrefix(hit.Source.Title, "[企业]") {
			t.Fatalf("title rewritten: %+v", hit.Source)
		}
	}
}
