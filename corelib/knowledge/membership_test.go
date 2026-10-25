package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMembershipProjectFilter(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	ctx := context.Background()
	src, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "UniqueMembershipToken lives in the project note.",
		Title:       "Alpha note",
		OwnerID:     desktopMembershipUser,
		ProjectPath: "D:/one",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMembership(ctx, src.ID, MembershipKindProject, "D:/two"); err != nil {
		t.Fatal(err)
	}
	base := SearchOptions{Query: "UniqueMembershipToken", OwnerID: desktopMembershipUser, OwnerLineage: true, UseMembership: true, Limit: 20}
	if !membershipSearchHas(t, store, withScope(base, SaveScopeProject, "D:/one"), src.ID) {
		t.Fatal("project D:/one missed")
	}
	if !membershipSearchHas(t, store, withScope(base, SaveScopeProject, "D:/two"), src.ID) {
		t.Fatal("project D:/two missed")
	}
	if membershipSearchHas(t, store, withScope(base, SaveScopeProject, "D:/three"), src.ID) {
		t.Fatal("project D:/three hit")
	}
	if !membershipSearchHas(t, store, withScope(base, "all", ""), src.ID) {
		t.Fatal("all missed")
	}
	if !membershipSearchHas(t, store, withScope(base, SaveScopePersonal, ""), src.ID) {
		t.Fatal("personal missed")
	}
}

func TestMembershipDesktopStampAndRawEmptyOwner(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	historical, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "HistoricalEmptyToken was saved before the desktop stamp.",
		Title:       "Historical",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if historical.OwnerID != "" {
		t.Fatalf("unstamped save owner = %q", historical.OwnerID)
	}
	if err := store.EnsureMembershipBackfill(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSource(ctx, historical.ID); err != nil || got.OwnerID != "" {
		t.Fatalf("server backfill rewrote owner: %+v err=%v", got.OwnerID, err)
	}
	taskOwner := desktopMembershipUser + `:C:\tasks\old`
	task, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "TaskLineageToken stays on the original owner.",
		Title:       "Task note",
		OwnerID:     taskOwner,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	if err := store.EnsureMembershipBackfill(ctx, desktopMembershipUser); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSource(ctx, historical.ID)
	if err != nil || got.OwnerID != desktopMembershipUser {
		t.Fatalf("stamped owner = %q err=%v", got.OwnerID, err)
	}
	taskGot, err := store.GetSource(ctx, task.ID)
	if err != nil || taskGot.OwnerID != taskOwner {
		t.Fatalf("task owner = %q err=%v", taskGot.OwnerID, err)
	}
	if key := userMembershipKey(t, store, task.ID); key != desktopMembershipUser {
		t.Fatalf("task membership = %q", key)
	}
	created := userMembershipCreatedAt(t, store, historical.ID)
	if err := store.EnsureMembershipBackfill(ctx, desktopMembershipUser); err != nil {
		t.Fatal(err)
	}
	if again := userMembershipCreatedAt(t, store, historical.ID); again != created {
		t.Fatalf("created_at changed from %q to %q", created, again)
	}
	base := SearchOptions{Query: "HistoricalEmptyToken", OwnerID: desktopMembershipUser, OwnerLineage: true, UseMembership: true, Limit: 20}
	if !membershipSearchHas(t, store, base, historical.ID) {
		t.Fatal("stamped historical note missed")
	}
	taskSearch := base
	taskSearch.Query = "TaskLineageToken"
	if !membershipSearchHas(t, store, taskSearch, task.ID) {
		t.Fatal("lineage task note missed")
	}

	rawID := "raw-empty-owner"
	insertRawEmptySource(t, store, rawID, "RawEmptyOwnerToken stays outside membership search.")
	raw, err := store.GetSource(ctx, rawID)
	if err != nil || strings.TrimSpace(raw.OwnerID) != "" {
		t.Fatalf("raw owner = %q err=%v", raw.OwnerID, err)
	}
	if key := userMembershipKey(t, store, rawID); key != "" {
		t.Fatalf("raw membership = %q", key)
	}
	rawSearch := base
	rawSearch.Query = "RawEmptyOwnerToken"
	if membershipSearchHas(t, store, rawSearch, rawID) {
		t.Fatal("raw empty owner appeared in membership search")
	}

	stamped, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "StampedEmptyToken is stored as the desktop user.",
		Title:       "Stamped",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stamped.OwnerID != desktopMembershipUser {
		t.Fatalf("stamped save owner = %q", stamped.OwnerID)
	}
	stampedSearch := base
	stampedSearch.Query = "StampedEmptyToken"
	if !membershipSearchHas(t, store, stampedSearch, stamped.ID) {
		t.Fatal("stamped empty save missed")
	}
}

func TestLibraryMembershipAssignKeepsCreatedAtAndReplaceSkipsMissing(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	for _, id := range []string{"lib-a", "lib-b"} {
		if err := store.SaveSource(ctx, Source{
			ID: id, Kind: SourceKindText, URI: "memory://" + id, Title: id,
			ContentHash: id, Status: StatusParsed, OwnerID: desktopMembershipUser,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AssignLibraryMemberships(ctx, "lib-policies", []string{"lib-a", "lib-b", "missing"}); err != nil {
		t.Fatal(err)
	}
	created := libraryMembershipCreatedAt(t, store, "lib-a", "lib-policies")
	if err := store.AssignLibraryMemberships(ctx, "lib-policies", []string{"lib-a", "lib-b"}); err != nil {
		t.Fatal(err)
	}
	if again := libraryMembershipCreatedAt(t, store, "lib-a", "lib-policies"); again != created {
		t.Fatalf("created_at changed from %q to %q", created, again)
	}
	if n := libraryMembershipCount(t, store, "missing", "lib-policies"); n != 0 {
		t.Fatalf("missing memberships = %d", n)
	}
	if err := store.ReplaceLibraryMemberships(ctx, "lib-policies", []string{"lib-b", "missing"}); err != nil {
		t.Fatal(err)
	}
	if n := libraryMembershipCount(t, store, "lib-a", "lib-policies"); n != 0 {
		t.Fatalf("replaced-away memberships = %d", n)
	}
	if n := libraryMembershipCount(t, store, "lib-b", "lib-policies"); n != 1 {
		t.Fatalf("kept memberships = %d", n)
	}
	if n := libraryMembershipCount(t, store, "missing", "lib-policies"); n != 0 {
		t.Fatalf("missing after replace = %d", n)
	}
}

func TestSaveScopeSessionRejected(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	_, err := store.SaveText(ctx, TextSaveRequest{Text: "nope", SaveScope: SaveScopeSession, DistillMode: DistillModeRules})
	if err == nil || !strings.Contains(err.Error(), `save scope "session" is not supported`) {
		t.Fatalf("SaveText err = %v", err)
	}
	_, err = store.SaveURL(ctx, URLSaveRequest{URL: "https://example.com/note", SaveScope: SaveScopeSession})
	if err == nil || !strings.Contains(err.Error(), `save scope "session" is not supported`) {
		t.Fatalf("SaveURL err = %v", err)
	}
	_, err = store.ImportDirectory(ctx, DirectoryImportRequest{RootPath: t.TempDir(), SaveScope: SaveScopeSession})
	if err == nil || !strings.Contains(err.Error(), `save scope "session" is not supported`) {
		t.Fatalf("ImportDirectory err = %v", err)
	}
}

func openMembershipStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func withScope(opts SearchOptions, scope, projectPath string) SearchOptions {
	opts.SearchScope = scope
	opts.ProjectPath = projectPath
	return opts
}

func membershipSearchHas(t *testing.T, store *SQLiteStore, opts SearchOptions, sourceID string) bool {
	t.Helper()
	hits, err := store.Search(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if hit.Source.ID == sourceID {
			return true
		}
	}
	return false
}

func insertRawEmptySource(t *testing.T, store *SQLiteStore, id, text string) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := insertSource(ctx, tx, Source{
		ID: id, Kind: SourceKindText, URI: "memory://" + id, Title: id,
		ContentHash: id, Status: StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID: id + "-node", SourceID: id, Type: "paragraph", Title: id, Text: text,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopStampSaveTextDedupes(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	text := "同一段桌面笔记应当只保留一条来源。"
	first, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "桌面笔记", DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "桌面笔记", DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.OwnerID != desktopMembershipUser {
		t.Fatalf("second = %+v first = %s", second.OwnerID, first.ID)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	if second.ID != first.ID {
		t.Fatalf("id %s -> %s", first.ID, second.ID)
	}
	var mismatched int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = ? AND COALESCE(owner_id, '') <> ?`, first.ID, desktopMembershipUser).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Fatalf("card owners diverged: %d", mismatched)
	}
}

func TestDesktopSaveTextKeepsProjectPath(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	text := "同一段笔记出现在两个项目里，原项目路径应留下来。"
	first, err := store.SaveText(ctx, TextSaveRequest{
		Text: text, Title: "项目笔记", ProjectPath: "D:/Alpha", SaveScope: SaveScopeProject, DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveText(ctx, TextSaveRequest{
		Text: text, Title: "项目笔记", ProjectPath: "D:/Beta", SaveScope: SaveScopeProject, DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ProjectPath != "D:/Alpha" {
		t.Fatalf("second id=%s project=%q", second.ID, second.ProjectPath)
	}
	var n, alpha, beta, cardBeta int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, first.ID, MembershipKindProject, "D:/Alpha").Scan(&alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, first.ID, MembershipKindProject, "D:/Beta").Scan(&beta); err != nil {
		t.Fatal(err)
	}
	if alpha != 1 || beta != 1 {
		t.Fatalf("project memberships alpha=%d beta=%d", alpha, beta)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = ? AND TRIM(COALESCE(project_path, '')) = ?`, first.ID, "D:/Beta").Scan(&cardBeta); err != nil {
		t.Fatal(err)
	}
	if cardBeta != 0 {
		t.Fatalf("cards filed under Beta = %d", cardBeta)
	}
	third, err := store.SaveText(ctx, TextSaveRequest{
		Text: text, Title: "项目笔记", SaveScope: SaveScopePersonal, DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if third.ID != first.ID || third.ProjectPath != "D:/Alpha" {
		t.Fatalf("personal resave id=%s project=%q", third.ID, third.ProjectPath)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, first.ID, MembershipKindProject, "D:/Beta").Scan(&beta); err != nil {
		t.Fatal(err)
	}
	if beta != 1 {
		t.Fatalf("beta membership after personal resave = %d", beta)
	}
}

func TestDesktopStampSaveTextKeepsTaskOwner(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	taskOwner := desktopMembershipUser + ":task-9"
	text := "任务笔记再次保存仍沿用原来的归属。"
	first, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "任务笔记", OwnerID: taskOwner, DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	second, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "任务笔记", DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.OwnerID != taskOwner {
		t.Fatalf("second id=%s owner=%q", second.ID, second.OwnerID)
	}
	var cardOwner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_cards WHERE source_id = ?`, first.ID).Scan(&cardOwner); err != nil {
		t.Fatal(err)
	}
	if cardOwner != taskOwner {
		t.Fatalf("card owner = %q", cardOwner)
	}

	alien, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "别人的笔记", OwnerID: "alice", DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	if alien.ID == first.ID || alien.OwnerID != "alice" {
		t.Fatalf("alien = %+v", alien)
	}
	again, err := store.SaveText(ctx, TextSaveRequest{Text: text, Title: "桌面再存", DistillMode: DistillModeRules})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.OwnerID != taskOwner {
		t.Fatalf("desktop save adopted id=%s owner=%q", again.ID, again.OwnerID)
	}
}

func TestAlignEmptyCardOwnersFromSources(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	taskOwner := desktopMembershipUser + ":task-9"
	saved, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "卡片归属应在打开库时跟上来源。",
		Title:       "对齐",
		OwnerID:     taskOwner,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_cards SET owner_id = '' WHERE source_id = ?`, saved.ID); err != nil {
		t.Fatal(err)
	}
	keptID := saved.ID + "-kept"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_cards(id, source_id, title, claim, summary, entities_json, topics_json, tags_json, owner_id, confidence, importance, source_trust, created_at, updated_at)
		SELECT ?, source_id, title, claim, summary, entities_json, topics_json, tags_json, 'keep-me', confidence, importance, source_trust, created_at, updated_at
		FROM knowledge_cards WHERE source_id = ? LIMIT 1`, keptID, saved.ID); err != nil {
		t.Fatal(err)
	}
	emptyID := "empty-owner-source"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_sources(id, kind, uri, title, fetched_at, content_hash, owner_id, status, created_at, updated_at)
		VALUES (?, 'text', 'memory://empty', 'empty', '2026-01-01T00:00:00Z', 'empty-hash', '', 'parsed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, emptyID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_cards(id, source_id, title, claim, summary, entities_json, topics_json, tags_json, owner_id, confidence, importance, source_trust, created_at, updated_at)
		VALUES ('empty-card', ?, 'empty', 'empty claim', '', '[]', '[]', '[]', '', 0.5, 1, 0.5, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, emptyID); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureMembershipBackfill(ctx, ""); err != nil {
		t.Fatal(err)
	}
	var aligned, kept, emptyOwner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_cards WHERE source_id = ? AND id <> ?`, saved.ID, keptID).Scan(&aligned); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_cards WHERE id = ?`, keptID).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(owner_id, '') FROM knowledge_cards WHERE id = 'empty-card'`).Scan(&emptyOwner); err != nil {
		t.Fatal(err)
	}
	if aligned != taskOwner || kept != "keep-me" || emptyOwner != "" {
		t.Fatalf("aligned=%q kept=%q empty=%q", aligned, kept, emptyOwner)
	}
}

func TestFindExistingURLSourceMatchesDesktopLineage(t *testing.T) {
	store := openMembershipStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	taskOwner := desktopMembershipUser + ":task-9"
	existing := Source{
		ID: "ksrc_url_lineage", Kind: SourceKindURL, URI: "https://example.com/articles/a",
		CanonicalURI: "https://example.com/articles/a", Title: "Existing URL", ContentHash: "hash-lineage",
		OwnerID: taskOwner, ProjectPath: "D:/project", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveSource(ctx, existing); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	lookup := Source{
		Kind: SourceKindURL, URI: "https://example.com/redirected", CanonicalURI: existing.CanonicalURI,
		ProjectPath: existing.ProjectPath, OwnerID: desktopMembershipUser,
	}
	got, ok, err := findExistingURLSourceForSave(ctx, tx, lookup)
	if err != nil || !ok || got.ID != existing.ID || got.OwnerID != taskOwner {
		t.Fatalf("lineage ok=%v id=%s owner=%q err=%v", ok, got.ID, got.OwnerID, err)
	}
	lookup.OwnerID = "alice"
	if _, ok, err := findExistingURLSourceForSave(ctx, tx, lookup); err != nil || ok {
		t.Fatalf("alice matched ok=%v err=%v", ok, err)
	}
	lookup.OwnerID = ""
	if _, ok, err := findExistingURLSourceForSave(ctx, tx, lookup); err != nil || ok {
		t.Fatalf("empty owner matched ok=%v err=%v", ok, err)
	}
}

func userMembershipKey(t *testing.T, store *SQLiteStore, sourceID string) string {
	t.Helper()
	var key string
	err := store.db.QueryRow(`SELECT scope_key FROM knowledge_memberships WHERE source_id = ? AND kind = ?`, sourceID, MembershipKindUser).Scan(&key)
	if err != nil {
		return ""
	}
	return key
}

func userMembershipCreatedAt(t *testing.T, store *SQLiteStore, sourceID string) string {
	t.Helper()
	var created string
	if err := store.db.QueryRow(`SELECT created_at FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, sourceID, MembershipKindUser, desktopMembershipUser).Scan(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func libraryMembershipCreatedAt(t *testing.T, store *SQLiteStore, sourceID, libraryID string) string {
	t.Helper()
	var created string
	if err := store.db.QueryRow(`SELECT created_at FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, sourceID, MembershipKindLibrary, libraryID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func libraryMembershipCount(t *testing.T, store *SQLiteStore, sourceID, libraryID string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, sourceID, MembershipKindLibrary, libraryID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
