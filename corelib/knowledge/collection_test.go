package knowledge

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCollectionManualProjectAndBackfill(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	owner := desktopMembershipUser + ":task-9"
	projectPath := "D:/Work/Alpha"
	source, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "CollectionFilterToken lives in the project note.",
		Title:       "Project note",
		OwnerID:     owner,
		ProjectPath: projectPath,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerID != owner {
		t.Fatalf("owner_id = %q", got.OwnerID)
	}
	var userScope string
	if err := store.db.QueryRowContext(ctx, `SELECT scope_key FROM knowledge_memberships WHERE source_id = ? AND kind = ?`, source.ID, MembershipKindUser).Scan(&userScope); err != nil {
		t.Fatal(err)
	}
	if userScope != desktopMembershipUser {
		t.Fatalf("user membership scope = %q", userScope)
	}

	collections, err := store.ListCollections(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	project := findCollection(collections, CollectionKindProject, projectPath)
	if project.ID == "" || project.Title != "Alpha" || project.OwnerUser != desktopMembershipUser {
		t.Fatalf("project collection = %#v", project)
	}
	members, err := store.ListCollectionMembers(ctx, owner, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].SourceID != source.ID || members[0].AddedBy != CollectionAddedByProject {
		t.Fatalf("project members = %#v", members)
	}

	manual, err := store.CreateManualCollection(ctx, owner, "Reading list")
	if err != nil {
		t.Fatal(err)
	}
	if manual.Kind != CollectionKindManual || manual.OwnerUser != desktopMembershipUser || manual.FolderPath != "" {
		t.Fatalf("manual collection = %#v", manual)
	}
	if err := store.AddCollectionMember(ctx, owner, manual.ID, source.ID, CollectionAddedByUser); err != nil {
		t.Fatal(err)
	}
	manualMembers, err := store.ListCollectionMembers(ctx, desktopMembershipUser, manual.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manualMembers) != 1 || manualMembers[0].AddedBy != CollectionAddedByUser {
		t.Fatalf("manual members = %#v", manualMembers)
	}
	if err := store.RemoveCollectionMember(ctx, owner, manual.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSource(ctx, source.ID); err != nil {
		t.Fatalf("removing a member deleted the source: %v", err)
	}
	if err := store.AddCollectionMember(ctx, owner, manual.ID, source.ID, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM knowledge_collections WHERE id = ?`, manual.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSource(ctx, source.ID); err != nil {
		t.Fatalf("deleting a collection deleted the source: %v", err)
	}
	var left int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_collection_members WHERE collection_id = ?`, manual.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("collection members left = %d", left)
	}

	if err := store.DeleteSource(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_collection_members WHERE source_id = ?`, source.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("source delete left collection members = %d", left)
	}
}

func TestCollectionProjectBackfillIsIdempotent(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	owner := desktopMembershipUser + ":task-9"
	projectPath := "D:/Work/Alpha"
	source, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "BackfillToken belongs to the project collection.",
		Title:       "Backfill note",
		OwnerID:     owner,
		ProjectPath: projectPath,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM knowledge_collection_members`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM knowledge_collections`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM kb_meta WHERE key = ?`, collectionProjectBackfillMetaKey); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureCollectionProjectBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	collections, err := store.ListCollections(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	project := findCollection(collections, CollectionKindProject, projectPath)
	if project.ID == "" || project.Title != "Alpha" || project.FolderPath != projectPath {
		t.Fatalf("backfill collection = %#v", project)
	}
	members, err := store.ListCollectionMembers(ctx, owner, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].SourceID != source.ID || members[0].AddedBy != CollectionAddedByProject {
		t.Fatalf("backfill members = %#v", members)
	}
	meta, err := knowledgeMetaValue(ctx, store.db, collectionProjectBackfillMetaKey)
	if err != nil {
		t.Fatal(err)
	}
	if meta != collectionProjectBackfillDone {
		t.Fatalf("backfill meta = %q", meta)
	}
	if err := store.EnsureCollectionProjectBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := store.ListCollectionMembers(ctx, owner, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].SourceID != source.ID {
		t.Fatalf("second backfill members = %#v", again)
	}
}

func TestCollectionEmptyOwnerDoesNotMarkBackfillDone(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	source, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "EmptyOwnerToken has a project path and no owner.",
		Title:       "Empty owner",
		ProjectPath: "D:/empty/proj",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.OwnerID != "" {
		t.Fatalf("owner_id = %q", source.OwnerID)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM kb_meta WHERE key = ?`, collectionProjectBackfillMetaKey); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureCollectionProjectBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	meta, err := knowledgeMetaValue(ctx, store.db, collectionProjectBackfillMetaKey)
	if err != nil {
		t.Fatal(err)
	}
	if meta == collectionProjectBackfillDone {
		t.Fatal("empty owner project membership marked collection backfill done")
	}
	collections, err := store.ListCollections(ctx, desktopMembershipUser)
	if err != nil {
		t.Fatal(err)
	}
	if project := findCollection(collections, CollectionKindProject, "D:/empty/proj"); project.ID != "" {
		t.Fatalf("empty owner created a project collection: %#v", project)
	}
}

func TestCollectionFolderAttachFindsNormalizedPath(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	root := t.TempDir()
	firstDir := filepath.Join(root, "a", "Notes")
	secondDir := filepath.Join(root, "b", "Notes")
	source, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "FolderWatchToken was imported from a watched directory.",
		Title:       "Watched note",
		OwnerID:     desktopMembershipUser,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := store.AttachFolderCollection(ctx, desktopMembershipUser, firstDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := NormalizeFolderCollectionPath(firstDir)
	wantTitle := collectionTitleFromPath(wantPath)
	if empty.Kind != CollectionKindFolder || empty.MemberCount != 0 || empty.Title != wantTitle || empty.FolderPath != wantPath {
		t.Fatalf("empty folder collection = %#v", empty)
	}
	attached, err := store.AttachFolderCollection(ctx, desktopMembershipUser, firstDir, []string{source.ID, source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if attached.ID != empty.ID {
		t.Fatalf("second attach id = %s, want %s", attached.ID, empty.ID)
	}
	members, err := store.ListCollectionMembers(ctx, desktopMembershipUser, attached.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].AddedBy != CollectionAddedByWatch || members[0].SourceID != source.ID {
		t.Fatalf("folder members = %#v", members)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_collection_members SET added_by = ? WHERE collection_id = ? AND source_id = ?`, CollectionAddedByUser, attached.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	again, err := store.AttachFolderCollection(ctx, desktopMembershipUser, firstDir, []string{source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != attached.ID {
		t.Fatalf("repeat attach id = %s", again.ID)
	}
	members, err = store.ListCollectionMembers(ctx, desktopMembershipUser, attached.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].AddedBy != CollectionAddedByUser {
		t.Fatalf("repeat attach changed added_by: %#v", members)
	}
	other, err := store.AttachFolderCollection(ctx, desktopMembershipUser, secondDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == attached.ID || other.FolderPath == attached.FolderPath {
		t.Fatalf("same base name collapsed two folders: %#v %#v", attached, other)
	}
	if runtime.GOOS == "windows" {
		folded := strings.ToUpper(firstDir)
		found, err := store.AttachFolderCollection(ctx, desktopMembershipUser, folded, []string{source.ID})
		if err != nil {
			t.Fatal(err)
		}
		if found.ID != attached.ID || found.FolderPath != NormalizeFolderCollectionPath(firstDir) {
			t.Fatalf("windows path fold = %#v", found)
		}
	}
}

func TestCollectionLabelWithoutCollectionStaysALabel(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	labeled, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "LegacyLabelToken is only a label.",
		Title:       "Labeled note",
		OwnerID:     desktopMembershipUser,
		TopicHint:   "not-a-collection",
		Labels:      []string{"legacy-label"},
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "OtherLabelToken has no legacy label.",
		Title:       "Other note",
		OwnerID:     desktopMembershipUser,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListSources(ctx, ListSourcesOptions{Labels: []string{"legacy-label"}, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ID != labeled.ID {
		t.Fatalf("label filter = %#v", sourceIDs(sources))
	}
	refs, err := store.ListSourceCollectionRefs(ctx, desktopMembershipUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.SourceID == labeled.ID || ref.Title == "legacy-label" || ref.Title == "not-a-collection" {
			t.Fatalf("label invented a collection ref: %#v", ref)
		}
	}
	facets, err := store.SearchFacetsBrowse(ctx, SearchOptions{OwnerID: desktopMembershipUser, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !facetHasLabel(facets.Labels, "legacy-label") {
		t.Fatalf("browse labels = %#v", facets.Labels)
	}
	for _, bucket := range facets.Collections {
		if bucket.Label == "legacy-label" || bucket.Label == "not-a-collection" || bucket.CollectionID == "" {
			t.Fatalf("browse invented a collection: %#v", bucket)
		}
	}
	if len(facets.Collections) != 0 {
		t.Fatalf("browse collections = %#v", facets.Collections)
	}
	if other.ID == "" {
		t.Fatal("missing other source")
	}
}

func TestCollectionSearchFilter(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	member, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "CollectionFilterToken is filed in the manual collection.",
		Title:       "Filed",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "CollectionFilterToken stays outside the collection.",
		Title:       "Outside",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := store.CreateManualCollection(ctx, desktopMembershipUser, "Filed notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddCollectionMember(ctx, desktopMembershipUser, manual.ID, member.ID, CollectionAddedByUser); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(ctx, SearchOptions{Query: "CollectionFilterToken", CollectionIDs: []string{manual.ID}, Limit: 20, Retrieval: "fts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("collection filter returned no hits")
	}
	for _, hit := range hits {
		if hit.Source.ID != member.ID {
			t.Fatalf("collection filter hit %s, want %s", hit.Source.ID, member.ID)
		}
	}
	all, err := store.Search(ctx, SearchOptions{Query: "CollectionFilterToken", Limit: 20, Retrieval: "fts"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, hit := range all {
		seen[hit.Source.ID] = true
	}
	if !seen[member.ID] || !seen[outsider.ID] {
		t.Fatalf("unfiltered hits = %#v", seen)
	}
}

func TestSuggestedSourceLinksAreNotWalked(t *testing.T) {
	store := openCollectionStore(t)
	defer store.Close()
	ctx := context.Background()
	left, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "SuggestedLinkToken alpha note.",
		Title:       "Alpha",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := store.SaveText(ctx, TextSaveRequest{
		Text:        "SuggestedLinkToken beta note.",
		Title:       "Beta",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSourceLinkTx(ctx, tx, SourceLink{
		SourceID:        left.ID,
		RelatedSourceID: right.ID,
		Relation:        SourceRelationTopicRelated,
		Status:          SourceLinkStatusSuggested,
		Terms:           []string{"suggestedlinktoken"},
	}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	links, err := store.ListSourceLinks(ctx, left.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Status != SourceLinkStatusSuggested {
		t.Fatalf("listed links = %#v", links)
	}
	neighborhood, err := store.SourceNeighborhood(ctx, left.ID, 2, 20, 50)
	if err != nil {
		t.Fatal(err)
	}
	if neighborhood.Count != 1 || neighborhood.Depth != 2 {
		t.Fatalf("suggested neighborhood = %#v", neighborhood)
	}
	path, err := store.SourcePath(ctx, left.ID, right.ID, 4, 50)
	if err != nil {
		t.Fatal(err)
	}
	if path.Found {
		t.Fatalf("suggested path was walked: %#v", path)
	}

	confirmed, err := store.LinkSources(ctx, SourceLink{
		SourceID:        left.ID,
		RelatedSourceID: right.ID,
		Relation:        SourceRelationTopicRelated,
		Terms:           []string{"manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != SourceLinkStatusConfirmed {
		t.Fatalf("manual link = %#v", confirmed)
	}
	neighborhood, err = store.SourceNeighborhood(ctx, left.ID, 2, 20, 50)
	if err != nil {
		t.Fatal(err)
	}
	if neighborhood.Count != 2 {
		t.Fatalf("confirmed neighborhood = %#v", neighborhood)
	}
	path, err = store.SourcePath(ctx, left.ID, right.ID, 4, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !path.Found {
		t.Fatalf("confirmed path was not walked: %#v", path)
	}

	tx, err = store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSourceLinkTx(ctx, tx, SourceLink{
		SourceID:        left.ID,
		RelatedSourceID: right.ID,
		Relation:        SourceRelationTopicRelated,
		Status:          SourceLinkStatusSuggested,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	links, err = store.ListSourceLinks(ctx, left.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, link := range links {
		if link.RelatedSourceID == right.ID && link.Relation == SourceRelationTopicRelated {
			kept = link.Status == SourceLinkStatusConfirmed
		}
	}
	if !kept {
		t.Fatalf("suggested write demoted a confirmed link: %#v", links)
	}
}

func openCollectionStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func findCollection(collections []Collection, kind, folderPath string) Collection {
	for _, collection := range collections {
		if collection.Kind == kind && collection.FolderPath == folderPath {
			return collection
		}
	}
	return Collection{}
}

func sourceIDs(sources []Source) []string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return ids
}

func facetHasLabel(buckets []SearchFacetBucket, label string) bool {
	for _, bucket := range buckets {
		if strings.EqualFold(bucket.Label, label) {
			return true
		}
	}
	return false
}
