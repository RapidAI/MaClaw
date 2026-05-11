package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStoreImportDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "guide.md"), []byte("# Guide\n\nUse SQLite for the first external brain slice."))
	mustWrite(t, filepath.Join(root, "notes.txt"), []byte("plain notes"))
	mustWrite(t, filepath.Join(root, "copy.md"), []byte("plain notes"))
	mustWrite(t, filepath.Join(root, "ignored.bin"), []byte("bin"))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	res, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		Labels:       []string{"project-alpha", "bulk docs"},
		AutoLabels:   true,
		Recursive:    true,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if res.Status != ImportStatusCompleted {
		t.Fatalf("status = %s", res.Status)
	}
	if res.ImportedFiles != 2 {
		t.Fatalf("imported = %d, want 2", res.ImportedFiles)
	}
	if res.DuplicateFiles != 1 {
		t.Fatalf("duplicates = %d, want 1", res.DuplicateFiles)
	}

	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(sources))
	}
	for _, source := range sources {
		if source.NodeCount == 0 || source.CardCount == 0 {
			t.Fatalf("expected source counts to be hydrated: %#v", source)
		}
		if !stringSliceContains(source.Labels, "project-alpha") || !stringSliceContains(source.Labels, "bulk docs") {
			t.Fatalf("expected import labels to be attached: %#v", source)
		}
		if !stringSliceContains(source.Labels, "scope:project") || !stringSliceContains(source.Labels, "kind:"+source.Kind) {
			t.Fatalf("expected import auto labels to be attached: %#v", source)
		}
	}
	filteredSources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", SourceKinds: []string{SourceKindMarkdown}, Query: "guide", Limit: 10})
	if err != nil {
		t.Fatalf("filtered ListSources: %v", err)
	}
	if len(filteredSources) != 1 || filteredSources[0].Kind != SourceKindMarkdown || filteredSources[0].RelativePath != "guide.md" {
		t.Fatalf("unexpected filtered sources: %#v", filteredSources)
	}
	initialVersions, err := store.ListSourceVersions(ctx, filteredSources[0].ID, 10)
	if err != nil {
		t.Fatalf("ListSourceVersions initial: %v", err)
	}
	if len(initialVersions) == 0 || initialVersions[0].Reason != "import" || initialVersions[0].NodeCount == 0 || initialVersions[0].CardCount == 0 {
		t.Fatalf("unexpected initial source versions: %#v", initialVersions)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx for derived cleanup: %v", err)
	}
	if err := deleteSourceCardsAndFacts(ctx, tx, filteredSources[0].ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("deleteSourceCardsAndFacts: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit derived cleanup: %v", err)
	}
	missingCards, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", CoverageFilter: "missing_cards", Query: "guide", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_cards: %v", err)
	}
	if len(missingCards) != 1 || missingCards[0].ID != filteredSources[0].ID {
		t.Fatalf("expected guide source to need card rebuild: %#v", missingCards)
	}
	rebuiltSource, err := store.RebuildSourceDerived(ctx, filteredSources[0].ID, DistillModeRules)
	if err != nil {
		t.Fatalf("RebuildSourceDerived: %v", err)
	}
	if rebuiltSource.CardCount == 0 {
		t.Fatalf("expected rebuilt cards: %#v", rebuiltSource)
	}
	rebuildVersions, err := store.ListSourceVersions(ctx, rebuiltSource.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceVersions rebuild: %v", err)
	}
	if len(rebuildVersions) < 2 || rebuildVersions[0].Reason != "rebuild_derived" || rebuildVersions[0].CardCount == 0 {
		t.Fatalf("unexpected rebuild source versions: %#v", rebuildVersions)
	}
	oldHash := filteredSources[0].ContentHash
	mustWrite(t, filepath.Join(root, "guide.md"), []byte("# Guide\n\nGuide uses vectorless external brain storage for refreshed notes."))
	preview, err := store.PreviewSourceRefresh(ctx, filteredSources[0].ID)
	if err != nil {
		t.Fatalf("PreviewSourceRefresh: %v", err)
	}
	if !preview.Refreshable || !preview.Changed || !preview.HashChanged || !preview.RequiresRefresh || preview.OldHash != oldHash || preview.NewHash == oldHash || preview.NewNodeCount == 0 || len(preview.Samples) == 0 {
		t.Fatalf("unexpected refresh preview: %#v", preview)
	}
	changedRefresh, err := store.RefreshChangedSourcesByFilter(ctx, ListSourcesOptions{ProjectPath: "D:/project", SourceKinds: []string{SourceKindMarkdown}, Limit: 10})
	if err != nil {
		t.Fatalf("RefreshChangedSourcesByFilter: %v", err)
	}
	if changedRefresh.Preview.Requested != 2 || changedRefresh.Preview.Changed != 1 || changedRefresh.Refresh.Requested != 1 || changedRefresh.Refresh.Refreshed != 1 || len(changedRefresh.SourceIDs) != 1 || changedRefresh.SourceIDs[0] != filteredSources[0].ID {
		t.Fatalf("unexpected changed refresh result: %#v", changedRefresh)
	}
	refreshedSource := changedRefresh.Refresh.Sources[0]
	if refreshedSource.ContentHash == oldHash || refreshedSource.NodeCount == 0 || refreshedSource.CardCount == 0 {
		t.Fatalf("unexpected refreshed source: %#v", refreshedSource)
	}
	refreshedVersions, err := store.ListSourceVersions(ctx, refreshedSource.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceVersions refreshed: %v", err)
	}
	if len(refreshedVersions) < 2 || refreshedVersions[0].Reason != "refresh" || refreshedVersions[0].ContentHash != refreshedSource.ContentHash || refreshedVersions[0].NodeCount == 0 {
		t.Fatalf("unexpected refreshed source versions: %#v", refreshedVersions)
	}
	unchangedPreview, err := store.PreviewSourceRefresh(ctx, refreshedSource.ID)
	if err != nil {
		t.Fatalf("PreviewSourceRefresh unchanged: %v", err)
	}
	if unchangedPreview.Changed || unchangedPreview.RequiresRefresh || unchangedPreview.HashChanged {
		t.Fatalf("unexpected unchanged refresh preview: %#v", unchangedPreview)
	}
	batchPreview, err := store.PreviewSourcesRefreshByFilter(ctx, ListSourcesOptions{ProjectPath: "D:/project", SourceKinds: []string{SourceKindMarkdown}, Query: "guide", Limit: 10})
	if err != nil {
		t.Fatalf("PreviewSourcesRefreshByFilter: %v", err)
	}
	if batchPreview.Requested != 1 || batchPreview.Unchanged != 1 || batchPreview.Changed != 0 || len(batchPreview.Previews) != 1 {
		t.Fatalf("unexpected batch refresh preview: %#v", batchPreview)
	}
	if err := os.Remove(filepath.Join(root, "guide.md")); err != nil {
		t.Fatalf("remove guide: %v", err)
	}
	missingPreview := store.PreviewSourcesRefresh(ctx, []string{refreshedSource.ID})
	if missingPreview.Requested != 1 || missingPreview.Failed != 1 || len(missingPreview.Failures) != 1 {
		t.Fatalf("unexpected missing-file refresh preview: %#v", missingPreview)
	}
	oldRefreshResults, err := store.Search(ctx, SearchOptions{Query: "SQLite", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("old refreshed Search: %v", err)
	}
	if len(oldRefreshResults) != 0 {
		t.Fatalf("stale refreshed content leaked into search: %#v", oldRefreshResults)
	}
	newRefreshResults, err := store.Search(ctx, SearchOptions{Query: "vectorless", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("new refreshed Search: %v", err)
	}
	if len(newRefreshResults) == 0 || newRefreshResults[0].Source.ID != refreshedSource.ID {
		t.Fatalf("refreshed content was not searchable: %#v", newRefreshResults)
	}
	statusSources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", Kind: SourceKindMarkdown, Status: StatusDistilled, Query: "copy", Limit: 10})
	if err != nil {
		t.Fatalf("status/kind ListSources: %v", err)
	}
	if len(statusSources) != 1 || statusSources[0].Kind != SourceKindMarkdown || statusSources[0].Status != StatusDistilled {
		t.Fatalf("unexpected status/kind sources: %#v", statusSources)
	}
	nodes, err := store.ListNodesBySource(ctx, sources[0].ID, 10)
	if err != nil {
		t.Fatalf("ListNodesBySource: %v", err)
	}
	if len(nodes) == 0 || nodes[0].SourceID != sources[0].ID || nodes[0].Text == "" {
		t.Fatalf("unexpected source nodes: %#v", nodes)
	}
	cards, err := store.ListCardsBySource(ctx, sources[0].ID, 10)
	if err != nil {
		t.Fatalf("ListCardsBySource: %v", err)
	}
	if len(cards) == 0 || cards[0].SourceID != sources[0].ID || cards[0].Claim == "" {
		t.Fatalf("unexpected source cards: %#v", cards)
	}
	var facts []Fact
	for _, source := range sources {
		facts, err = store.ListFactsBySource(ctx, source.ID, 20)
		if err != nil {
			t.Fatalf("ListFactsBySource: %v", err)
		}
		if len(facts) > 0 {
			break
		}
	}
	if len(facts) == 0 || facts[0].SourceID == "" || facts[0].Subject == "" {
		t.Fatalf("unexpected source facts: %#v", facts)
	}
	if err := store.ReplaceSourceLabels(ctx, sources[0].ID, []string{"manual review"}); err != nil {
		t.Fatalf("ReplaceSourceLabels before stats: %v", err)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Sources != 2 || stats.DocumentNodes != 2 || stats.Cards != 2 || stats.Facts == 0 || stats.Batches != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.SourcesByKind[SourceKindMarkdown] != 2 || stats.SourcesByStatus[StatusDistilled] != 2 || stats.SourcesByLabel["manual review"] != 1 || stats.BatchesByStatus[ImportStatusCompleted] != 1 {
		t.Fatalf("unexpected stats distributions: %#v", stats)
	}
	if stats.ImportItemsByStatus[ItemStatusImported] != 2 || stats.ImportItemsByStatus[ItemStatusSkippedDuplicate] != 1 || stats.ImportItemsByStatus[ItemStatusSkippedType] != 1 {
		t.Fatalf("unexpected import item status stats: %#v", stats.ImportItemsByStatus)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if doctor.Status != "warning" || doctor.Score <= 0 || doctor.Stats.Sources != stats.Sources || !hasDoctorFinding(doctor, "unsupported_file_types") || !hasDoctorFinding(doctor, "duplicate_files") {
		t.Fatalf("unexpected doctor result: %#v", doctor)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "external brain", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	if results[0].ResultType != "card" || results[0].CardID == "" {
		t.Fatalf("expected card-first search result, got %#v", results[0])
	}
	if results[0].Citation == "" {
		t.Fatalf("expected search result citation, got %#v", results[0])
	}
	explain, err := store.Explain(ctx, SearchOptions{Query: "external brain", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if explain.Count == 0 || len(explain.Citations) == 0 || explain.Citations[0].Label == "" {
		t.Fatalf("unexpected explain result: %#v", explain)
	}

	factResults, err := store.Search(ctx, SearchOptions{Query: "uses", ProjectPath: "D:/project", ResultTypes: []string{"fact"}, Limit: 5})
	if err != nil {
		t.Fatalf("fact Search: %v", err)
	}
	if len(factResults) == 0 || factResults[0].ResultType != "fact" || factResults[0].FactID == "" {
		t.Fatalf("expected fact search result, got %#v", factResults)
	}
	for _, result := range factResults {
		if result.ResultType != "fact" {
			t.Fatalf("fact-only search returned %s", result.ResultType)
		}
	}

	nodeResults, err := store.Search(ctx, SearchOptions{Query: "external brain", ProjectPath: "D:/project", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil {
		t.Fatalf("node Search: %v", err)
	}
	if len(nodeResults) == 0 || nodeResults[0].ResultType != "node" || nodeResults[0].NodeID == "" {
		t.Fatalf("expected node-only search result, got %#v", nodeResults)
	}

	disabledSource, err := store.DisableSource(ctx, results[0].Source.ID)
	if err != nil {
		t.Fatalf("DisableSource: %v", err)
	}
	if disabledSource.Status != StatusDisabled {
		t.Fatalf("disabled source status = %s", disabledSource.Status)
	}
	disabledResults, err := store.Search(ctx, SearchOptions{Query: "external brain", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("disabled source Search: %v", err)
	}
	if len(disabledResults) != 0 {
		t.Fatalf("disabled source leaked into default search: %#v", disabledResults)
	}
	includeDisabledResults, err := store.Search(ctx, SearchOptions{Query: "external brain", ProjectPath: "D:/project", IncludeDisabled: true, Limit: 5})
	if err != nil {
		t.Fatalf("include disabled Search: %v", err)
	}
	if len(includeDisabledResults) == 0 {
		t.Fatalf("expected disabled source when IncludeDisabled is true")
	}
	enabledSource, err := store.EnableSource(ctx, disabledSource.ID)
	if err != nil {
		t.Fatalf("EnableSource: %v", err)
	}
	if enabledSource.Status != StatusDistilled {
		t.Fatalf("enabled source status = %s", enabledSource.Status)
	}

	batches, err := store.ListImportBatches(ctx, 10)
	if err != nil {
		t.Fatalf("ListImportBatches: %v", err)
	}
	if len(batches) != 1 || batches[0].ID != res.BatchID || batches[0].Imported != 2 {
		t.Fatalf("unexpected batches: %#v", batches)
	}
	items, err := store.ListImportItems(ctx, res.BatchID, 10)
	if err != nil {
		t.Fatalf("ListImportItems: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected import items")
	}

	personalRoot := t.TempDir()
	mustWrite(t, filepath.Join(personalRoot, "private.md"), []byte("Private lighthouse note for personal brain."))
	_, err = store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     personalRoot,
		SaveScope:    SaveScopePersonal,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatalf("personal ImportDirectory: %v", err)
	}
	personalResults, err := store.Search(ctx, SearchOptions{Query: "lighthouse", SearchScope: SaveScopePersonal, Limit: 5})
	if err != nil {
		t.Fatalf("personal Search: %v", err)
	}
	if len(personalResults) == 0 {
		t.Fatalf("expected personal scope result")
	}
	projectResults, err := store.Search(ctx, SearchOptions{Query: "lighthouse", SearchScope: SaveScopeProject, ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("project scoped Search: %v", err)
	}
	if len(projectResults) != 0 {
		t.Fatalf("project scope leaked personal result: %#v", projectResults)
	}
	personalSources, err := store.ListSources(ctx, ListSourcesOptions{SearchScope: SaveScopePersonal, Query: "private", Limit: 10})
	if err != nil {
		t.Fatalf("personal ListSources: %v", err)
	}
	if len(personalSources) != 1 || personalSources[0].ProjectPath != "" {
		t.Fatalf("expected personal source list to stay in empty project scope: %#v", personalSources)
	}
	projectScopedSources, err := store.ListSources(ctx, ListSourcesOptions{SearchScope: SaveScopeProject, ProjectPath: "D:/project", Query: "private", Limit: 10})
	if err != nil {
		t.Fatalf("project ListSources: %v", err)
	}
	if len(projectScopedSources) != 0 {
		t.Fatalf("project ListSources leaked personal source: %#v", projectScopedSources)
	}

	mustWrite(t, filepath.Join(root, "guide.md"), []byte("# Guide\n\nGuide uses directory upsert storage for changed files."))
	res2, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:     root,
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		Recursive:    true,
		IncludeExts:  []string{".md", ".txt"},
		MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatalf("second ImportDirectory: %v", err)
	}
	if res2.ImportedFiles != 1 || res2.DuplicateFiles != 2 {
		t.Fatalf("second import imported=%d duplicates=%d, want 1/2", res2.ImportedFiles, res2.DuplicateFiles)
	}
	secondSources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project"})
	if err != nil {
		t.Fatalf("second ListSources: %v", err)
	}
	if len(secondSources) != 2 {
		t.Fatalf("second import should update by path without duplicating sources: %#v", secondSources)
	}
	directoryUpsertResults, err := store.Search(ctx, SearchOptions{Query: "directory upsert", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("directory upsert Search: %v", err)
	}
	if len(directoryUpsertResults) == 0 {
		t.Fatalf("updated directory content was not searchable")
	}
	staleDirectoryResults, err := store.Search(ctx, SearchOptions{Query: "vectorless", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("stale directory Search: %v", err)
	}
	if len(staleDirectoryResults) != 0 {
		t.Fatalf("stale directory content leaked into search: %#v", staleDirectoryResults)
	}
}

func TestSQLiteStoreImportFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	selected := filepath.Join(root, "selected.md")
	sibling := filepath.Join(root, "sibling.md")
	mustWrite(t, selected, []byte("Selected policy note for direct file import."))
	mustWrite(t, sibling, []byte("Sibling file should not be imported by direct selection."))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	scan, err := store.ScanFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{selected})
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if scan.TotalFiles != 1 || scan.QueuedFiles != 1 {
		t.Fatalf("unexpected file scan result: %#v", scan)
	}

	progressSnapshots := make([]DirectoryImportResult, 0)
	store.SetImportProgressCallback(func(progress DirectoryImportResult) {
		progressSnapshots = append(progressSnapshots, progress)
	})

	res, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{selected})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if res.TotalFiles != 1 || res.ImportedFiles != 1 || res.SkippedFiles != 0 {
		t.Fatalf("unexpected file import result: %#v", res)
	}
	if len(progressSnapshots) == 0 {
		t.Fatalf("expected import progress snapshots")
	}
	lastProgress := progressSnapshots[len(progressSnapshots)-1]
	if lastProgress.ProcessedFiles != 1 || lastProgress.ImportedFiles != 1 || lastProgress.CurrentFile == "" {
		t.Fatalf("unexpected final progress snapshot: %#v", lastProgress)
	}

	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 || sources[0].URI != selected {
		t.Fatalf("unexpected sources after ImportFiles: %#v", sources)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "Selected policy", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search selected file: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected selected file to be searchable")
	}
	siblingResults, err := store.Search(ctx, SearchOptions{Query: "Sibling", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search sibling file: %v", err)
	}
	if len(siblingResults) != 0 {
		t.Fatalf("sibling file was imported unexpectedly: %#v", siblingResults)
	}
	refreshResult, err := store.RefreshSourcesByFilter(ctx, ListSourcesOptions{Query: "selected", Limit: 10})
	if err != nil {
		t.Fatalf("RefreshSourcesByFilter: %v", err)
	}
	if refreshResult.Requested != 1 || refreshResult.Refreshed != 1 || refreshResult.Failed != 0 {
		t.Fatalf("unexpected refresh by filter result: %#v", refreshResult)
	}
	disableResult, err := store.DisableSourcesByFilter(ctx, ListSourcesOptions{Query: "selected", Limit: 10})
	if err != nil {
		t.Fatalf("DisableSourcesByFilter: %v", err)
	}
	if disableResult.Requested != 1 || disableResult.Updated != 1 || disableResult.Failed != 0 {
		t.Fatalf("unexpected disable by filter result: %#v", disableResult)
	}
	disabledResults, err := store.Search(ctx, SearchOptions{Query: "Selected policy", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search disabled selected file: %v", err)
	}
	if len(disabledResults) != 0 {
		t.Fatalf("disabled source was returned by default search: %#v", disabledResults)
	}
	enableResult, err := store.EnableSourcesByFilter(ctx, ListSourcesOptions{Query: "selected", Status: StatusDisabled, Limit: 10})
	if err != nil {
		t.Fatalf("EnableSourcesByFilter: %v", err)
	}
	if enableResult.Requested != 1 || enableResult.Updated != 1 || enableResult.Failed != 0 {
		t.Fatalf("unexpected enable by filter result: %#v", enableResult)
	}
	enabledResults, err := store.Search(ctx, SearchOptions{Query: "Selected policy", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search enabled selected file: %v", err)
	}
	if len(enabledResults) == 0 {
		t.Fatalf("enabled source should return to default search")
	}
}

func TestSQLiteStoreImportFilesSplitsMultilinePathInput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	mustWrite(t, first, []byte("First multiline direct import note."))
	mustWrite(t, second, []byte("Second multiline direct import note."))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	res, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{first + "\n" + second})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if res.TotalFiles != 2 || res.ImportedFiles != 2 {
		t.Fatalf("unexpected multiline file import result: %#v", res)
	}

	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2: %#v", len(sources), sources)
	}
}

func TestSQLiteStoreRetryImportBatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	missing := filepath.Join(root, "retry.md")

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	initial, err := store.ImportFiles(ctx, DirectoryImportRequest{
		ProjectPath:  "D:/project",
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	}, []string{missing})
	if err != nil {
		t.Fatalf("ImportFiles missing: %v", err)
	}
	if initial.BatchID == "" || initial.FailedFiles != 1 {
		t.Fatalf("expected failed initial import batch: %#v", initial)
	}
	mustWrite(t, missing, []byte("Retry import anchor should be stored after the missing file appears."))
	retry, err := store.RetryImportBatch(ctx, ImportRetryRequest{BatchID: initial.BatchID})
	if err != nil {
		t.Fatalf("RetryImportBatch: %v", err)
	}
	if retry.BatchID == "" || retry.BatchID == initial.BatchID || retry.ImportedFiles != 1 || retry.FailedFiles != 0 {
		t.Fatalf("unexpected retry result: %#v", retry)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "Retry import anchor", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search retry import: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("retry import result was not searchable")
	}
}

func TestSQLiteStoreSaveText(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	source, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Conversation decision",
		Text:        "The current project knowledge trigger should save explicit user-approved notes.",
		ProjectPath: "D:/project",
		TopicHint:   "knowledge trigger",
		SaveScope:   SaveScopeProject,
		Labels:      []string{"inbox", "decisions"},
		AutoLabels:  true,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	if source.Kind != SourceKindConversation || source.NodeCount == 0 || source.CardCount == 0 {
		t.Fatalf("unexpected saved text source: %#v", source)
	}
	if !stringSliceContains(source.Labels, "inbox") || !stringSliceContains(source.Labels, "decisions") {
		t.Fatalf("expected saved text labels: %#v", source.Labels)
	}
	if !stringSliceContains(source.Labels, "kind:conversation") || !stringSliceContains(source.Labels, "scope:project") {
		t.Fatalf("expected saved text auto labels: %#v", source.Labels)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "user-approved notes", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search saved text: %v", err)
	}
	if len(results) == 0 || results[0].Source.ID != source.ID {
		t.Fatalf("saved text was not searchable: %#v", results)
	}
	pack, err := store.ContextPack(ctx, ContextPackOptions{
		SearchOptions: SearchOptions{Query: "user-approved notes", ProjectPath: "D:/project", Limit: 5},
		MaxItems:      3,
		MaxChars:      400,
	})
	if err != nil {
		t.Fatalf("ContextPack saved text: %v", err)
	}
	if pack.Count == 0 || len(pack.Items) == 0 || len(pack.Citations) == 0 || !strings.Contains(pack.Items[0].Text, "user-approved notes") || pack.CharacterCount > 400 {
		t.Fatalf("unexpected context pack: %#v", pack)
	}
	second, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Conversation decision updated title",
		Text:        "The current project knowledge trigger should save explicit user-approved notes.",
		ProjectPath: "D:/project",
		TopicHint:   "knowledge trigger",
		SaveScope:   SaveScopeProject,
		Labels:      []string{"reviewed"},
		AutoLabels:  true,
	})
	if err != nil {
		t.Fatalf("second SaveText: %v", err)
	}
	if second.ID != source.ID || second.Title != "Conversation decision updated title" {
		t.Fatalf("expected text save to upsert by content hash and scope: first=%#v second=%#v", source, second)
	}
	if source.SaveStatus != SaveStatusCreated {
		t.Fatalf("expected first save to have SaveStatus=%q, got %q", SaveStatusCreated, source.SaveStatus)
	}
	if second.SaveStatus != SaveStatusDuplicate {
		t.Fatalf("expected second save to have SaveStatus=%q, got %q", SaveStatusDuplicate, second.SaveStatus)
	}
	if !stringSliceContains(second.Labels, "inbox") || !stringSliceContains(second.Labels, "reviewed") {
		t.Fatalf("expected repeated text save to append labels: %#v", second.Labels)
	}
	updated, err := store.UpdateSourceMetadata(ctx, SourceUpdateRequest{
		ID:          source.ID,
		Title:       "Conversation decision governed",
		TopicHint:   "governed recall topic",
		SourceTrust: 0.92,
		Labels:      []string{"Governed", " Project Notes ", "governed", "治理，文档；项目、知识库"},
	})
	if err != nil {
		t.Fatalf("UpdateSourceMetadata: %v", err)
	}
	if updated.Title != "Conversation decision governed" || updated.TopicHint != "governed recall topic" || updated.SourceTrust != 0.92 || len(updated.Labels) != 6 || updated.Labels[0] != "governed" || updated.Labels[1] != "project notes" || !stringSliceContains(updated.Labels, "治理") || !stringSliceContains(updated.Labels, "文档") || !stringSliceContains(updated.Labels, "项目") || !stringSliceContains(updated.Labels, "知识库") {
		t.Fatalf("unexpected updated source metadata: %#v", updated)
	}
	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", Kind: SourceKindConversation, Labels: []string{"governed"}, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources conversation: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("duplicate conversation sources after upsert: %#v", sources)
	}
	chineseLabelSources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", Kind: SourceKindConversation, Labels: []string{"治理，文档"}, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources Chinese labels: %v", err)
	}
	if len(chineseLabelSources) != 1 || chineseLabelSources[0].ID != source.ID {
		t.Fatalf("expected Chinese label separators to filter as separate labels: %#v", chineseLabelSources)
	}
	labelResults, err := store.Search(ctx, SearchOptions{Query: "user-approved notes", ProjectPath: "D:/project", Labels: []string{"project notes"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search with labels: %v", err)
	}
	if len(labelResults) == 0 || labelResults[0].Source.ID != source.ID || len(labelResults[0].Source.Labels) == 0 {
		t.Fatalf("expected labeled source to be searchable with hydrated labels: %#v", labelResults)
	}
	unlabeled, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Unlabeled note",
		Text:        "This local note is intentionally left without labels for doctor governance checks.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText unlabeled: %v", err)
	}
	if len(unlabeled.Labels) != 0 {
		t.Fatalf("expected unlabeled source, got %#v", unlabeled.Labels)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor unlabeled: %v", err)
	}
	if !hasDoctorFinding(doctor, "unlabeled_sources") {
		t.Fatalf("expected unlabeled_sources doctor finding: %#v", doctor)
	}
	unlabeledFinding, ok := doctorFinding(doctor, "unlabeled_sources")
	if !ok || unlabeledFinding.Filter == nil || unlabeledFinding.Filter.CoverageFilter != "missing_labels" {
		t.Fatalf("expected unlabeled_sources filter: %#v", unlabeledFinding)
	}
	unlabeledSources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project", CoverageFilter: "missing_labels", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_labels: %v", err)
	}
	if len(unlabeledSources) != 1 || unlabeledSources[0].ID != unlabeled.ID {
		t.Fatalf("unexpected missing_labels sources: %#v", unlabeledSources)
	}
	sourceIDSources, err := store.ListSources(ctx, ListSourcesOptions{SourceIDs: []string{unlabeled.ID}, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources source_ids: %v", err)
	}
	if len(sourceIDSources) != 1 || sourceIDSources[0].ID != unlabeled.ID {
		t.Fatalf("unexpected source_ids sources: %#v", sourceIDSources)
	}
	backfillPreview, err := store.BackfillSourceAutoLabels(ctx, SourceAutoLabelBackfillRequest{
		SourceIDs: []string{unlabeled.ID},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("BackfillSourceAutoLabels dry-run: %v", err)
	}
	if backfillPreview.Updated != 1 || !backfillPreview.DryRun || len(backfillPreview.LabelChanges) != 1 || !stringSliceContains(backfillPreview.LabelChanges[0].After, "kind:conversation") {
		t.Fatalf("unexpected auto-label backfill preview: %#v", backfillPreview)
	}
	backfill, err := store.BackfillSourceAutoLabels(ctx, SourceAutoLabelBackfillRequest{
		Filter: ListSourcesOptions{ProjectPath: "D:/project", CoverageFilter: "missing_labels"},
	})
	if err != nil {
		t.Fatalf("BackfillSourceAutoLabels: %v", err)
	}
	if backfill.Updated != 1 {
		t.Fatalf("expected one auto-label backfill update: %#v", backfill)
	}
	backfilled, err := store.GetSource(ctx, unlabeled.ID)
	if err != nil {
		t.Fatalf("GetSource backfilled: %v", err)
	}
	if !stringSliceContains(backfilled.Labels, "kind:conversation") || !stringSliceContains(backfilled.Labels, "scope:project") {
		t.Fatalf("expected backfilled auto labels: %#v", backfilled.Labels)
	}
}

type countingCardDistiller struct {
	calls int
}

func (d *countingCardDistiller) DistillCards(ctx context.Context, source Source, nodes []DocumentNode) ([]Card, error) {
	d.calls++
	return []Card{{
		Title:      "LLM structured card",
		Claim:      "LLM structured mode produced this card.",
		Summary:    "The optional LLM path was used for write-time structure.",
		Facts:      []Fact{{Subject: "LLM distiller", Predicate: "emits", Object: "grounded fact triples", Confidence: 0.92}},
		Confidence: 0.9,
		Importance: 1.1,
	}}, nil
}

func TestSQLiteStoreDistillModeControlsLLMUse(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	distiller := &countingCardDistiller{}
	store.SetCardDistiller(distiller)

	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Rules only",
		Text:        "Rules-only structuring should avoid LLM even when the distiller is configured.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	}); err != nil {
		t.Fatalf("SaveText rules only: %v", err)
	}
	if distiller.calls != 0 {
		t.Fatalf("rules_only should not call distiller, got %d calls", distiller.calls)
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "LLM optional",
		Text:        "Short text can still request LLM-if-available structuring explicitly.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeLLMIfAny,
	}); err != nil {
		t.Fatalf("SaveText llm optional: %v", err)
	}
	if distiller.calls != 1 {
		t.Fatalf("llm_if_available should call distiller once, got %d calls", distiller.calls)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "LLM structured mode", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search LLM mode: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected LLM-distilled card to be searchable")
	}
	factResults, err := store.Search(ctx, SearchOptions{Query: "grounded fact triples", ProjectPath: "D:/project", ResultTypes: []string{"fact"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search LLM fact: %v", err)
	}
	if len(factResults) == 0 || factResults[0].Subject != "LLM distiller" || factResults[0].Predicate != "emits" {
		t.Fatalf("expected LLM-distilled fact to be searchable: %#v", factResults)
	}
}

func TestSQLiteStoreSaveTextIndexesChineseSummaryFacts(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	source, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "中文知识结构化",
		Text:        "本记录用于测试。导入服务负责批量文档解析。知识库接口提供来源摘要。",
		ProjectPath: "D:/project",
		TopicHint:   "知识库",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	facts, err := store.ListFactsBySource(ctx, source.ID, 20)
	if err != nil {
		t.Fatalf("ListFactsBySource: %v", err)
	}
	if !containsStoredFact(facts, "负责", "批量文档解析") || !containsStoredFact(facts, "提供", "来源摘要") {
		t.Fatalf("expected saved Chinese summary facts: %#v", facts)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "来源摘要", ProjectPath: "D:/project", ResultTypes: []string{"fact"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search Chinese fact: %v", err)
	}
	if len(results) == 0 || results[0].ResultType != "fact" || results[0].Source.ID != source.ID {
		t.Fatalf("expected saved Chinese fact to be searchable: %#v", results)
	}
	defaultResults, err := store.Search(ctx, SearchOptions{Query: "知识库", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("default Search Chinese fact: %v", err)
	}
	if !hasSearchResultType(defaultResults, "card") || !hasSearchResultType(defaultResults, "fact") {
		t.Fatalf("default search should include both card context and fact evidence: %#v", defaultResults)
	}
	pack, err := store.ContextPack(ctx, ContextPackOptions{
		SearchOptions: SearchOptions{Query: "来源摘要", ProjectPath: "D:/project", ResultTypes: []string{"fact"}, Limit: 5},
		MaxItems:      2,
		MaxChars:      300,
	})
	if err != nil {
		t.Fatalf("ContextPack Chinese fact: %v", err)
	}
	if pack.Count == 0 || strings.Count(pack.Items[0].Text, "知识库接口 提供 来源摘要") != 1 {
		t.Fatalf("expected deduplicated fact context text: %#v", pack)
	}
}

func TestSourceQualityReportLimitCoversExplicitSourceIDs(t *testing.T) {
	sourceIDs := make([]string, 0, 1003)
	sourceIDs = append(sourceIDs, " src-0000 ", "src-0000", "")
	for i := 1; i <= 1001; i++ {
		sourceIDs = append(sourceIDs, "src-"+strconv.Itoa(i))
	}
	if got := sourceQualityReportLimit(ListSourcesOptions{SourceIDs: sourceIDs, Limit: 1}); got != 1002 {
		t.Fatalf("expected explicit source IDs to raise report limit, got %d", got)
	}
	if got := sourceQualityReportLimit(ListSourcesOptions{SourceIDs: sourceIDs, Limit: 50000}); got != 5000 {
		t.Fatalf("expected report limit to stay capped at ListSources max, got %d", got)
	}
}

func TestSourceFilterLimitCoversExplicitSourceIDs(t *testing.T) {
	sourceIDs := make([]string, 0, 703)
	sourceIDs = append(sourceIDs, " src-0000 ", "src-0000", "")
	for i := 1; i <= 701; i++ {
		sourceIDs = append(sourceIDs, "src-"+strconv.Itoa(i))
	}
	if got := sourceFilterLimit(ListSourcesOptions{SourceIDs: sourceIDs, Limit: 1}, 100, 500, 5000); got != 702 {
		t.Fatalf("expected explicit source IDs to raise source filter limit, got %d", got)
	}
	if got := sourceFilterLimit(ListSourcesOptions{SourceIDs: sourceIDs, Limit: 50000}, 100, 500, 5000); got != 5000 {
		t.Fatalf("expected explicit source ID filter limit to cap at explicit max, got %d", got)
	}
	if got := sourceFilterLimit(ListSourcesOptions{Limit: 50000}, 100, 500, 5000); got != 500 {
		t.Fatalf("expected unscoped source filter limit to keep normal max, got %d", got)
	}
}

func TestSQLiteLockedErrorDetection(t *testing.T) {
	for _, err := range []error{
		errors.New("database is locked (5)"),
		errors.New("database table is locked"),
		errors.New("SQLITE_BUSY: database is locked"),
	} {
		if !isSQLiteLockedError(err) {
			t.Fatalf("expected sqlite locked error to be detected: %v", err)
		}
	}
	if isSQLiteLockedError(errors.New("disk I/O error")) {
		t.Fatalf("non-lock sqlite errors should not be treated as transient lock errors")
	}
}

func TestSQLiteStoreScanSensitiveContent(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	source, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Secret note",
		Text:        "Never store password = supersecretvalue in long-term knowledge.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	scan, err := store.ScanSensitiveContent(ctx, 10)
	if err != nil {
		t.Fatalf("ScanSensitiveContent: %v", err)
	}
	if scan.Count == 0 || scan.MaxSeverity == "" {
		t.Fatalf("expected sensitive finding: %#v", scan)
	}
	if strings.Contains(scan.Findings[0].Snippet, "supersecretvalue") || strings.Contains(scan.Findings[0].Redacted, "supersecretvalue") {
		t.Fatalf("sensitive finding should be redacted: %#v", scan.Findings[0])
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !hasDoctorFinding(doctor, "possible_sensitive_content") {
		t.Fatalf("expected Doctor possible_sensitive_content finding: %#v", doctor.Findings)
	}
	isolation, err := store.DisableSensitiveSources(ctx, 10)
	if err != nil {
		t.Fatalf("DisableSensitiveSources: %v", err)
	}
	if isolation.Update.Updated != 1 || len(isolation.SourceIDs) != 1 || isolation.SourceIDs[0] != source.ID {
		t.Fatalf("unexpected sensitive isolation result: %#v", isolation)
	}
	disabled, err := store.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetSource disabled: %v", err)
	}
	if disabled.Status != StatusDisabled {
		t.Fatalf("expected sensitive source disabled, got %#v", disabled)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "supersecretvalue", ProjectPath: "D:/project", Limit: 5})
	if err != nil {
		t.Fatalf("Search after sensitive isolation: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("disabled sensitive source should not be searchable by default: %#v", results)
	}
	if _, err := store.UpdateURLDomainPolicies(ctx, URLDomainPolicyUpdateRequest{
		AllowDomains: []string{"example.com"},
		BlockDomains: []string{"blocked.example.com"},
		Replace:      true,
		Reason:       "backup-test",
	}); err != nil {
		t.Fatalf("UpdateURLDomainPolicies before export: %v", err)
	}
	if err := store.ReplaceSourceLabels(ctx, source.ID, []string{"Sensitive Review", "backup"}); err != nil {
		t.Fatalf("ReplaceSourceLabels before export: %v", err)
	}
	linkSource, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Secret note backup guidance",
		Text:        "Secret note backup guidance discusses password handling and long-term knowledge retention without storing the raw secret.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText link source: %v", err)
	}
	linkBuild, err := store.RefreshSourceTopicLinks(ctx, source.ID, 8)
	if err != nil {
		t.Fatalf("RefreshSourceTopicLinks before export: %v", err)
	}
	if linkBuild.Linked == 0 {
		t.Fatalf("expected topic source links before export: %#v", linkBuild)
	}
	if _, err := store.LinkSources(ctx, SourceLink{
		SourceID:        source.ID,
		RelatedSourceID: linkSource.ID,
		Terms:           []string{"backup-audit"},
		Evidence:        []string{"manual export audit password=supersecretvalue"},
	}); err != nil {
		t.Fatalf("LinkSources before export: %v", err)
	}
	exportPath := filepath.Join(t.TempDir(), "knowledge-export.jsonl")
	export, err := store.ExportSnapshot(ctx, ExportOptions{OutputPath: exportPath, RedactSensitive: true})
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if export.Sources != 2 || export.SourceLabels == 0 || export.SourceVersions == 0 || export.SourceLinks == 0 || export.SourceLinkEvents == 0 || export.Nodes == 0 || export.URLPolicies != 2 || export.OutputPath != exportPath || export.Format != "jsonl" {
		t.Fatalf("unexpected export result: %#v", export)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if strings.Contains(string(exported), "supersecretvalue") || !strings.Contains(string(exported), "pass...alue") {
		t.Fatalf("export should redact sensitive content: %s", string(exported))
	}
	if !strings.Contains(string(exported), `"type":"url_domain_policy"`) {
		t.Fatalf("export should include URL domain policies: %s", string(exported))
	}
	if !strings.Contains(string(exported), `"type":"source_version"`) || !strings.Contains(string(exported), `"reason":"save_text"`) {
		t.Fatalf("export should include source versions: %s", string(exported))
	}
	if !strings.Contains(string(exported), `"type":"source_label"`) || !strings.Contains(string(exported), "sensitive review") {
		t.Fatalf("export should include source labels: %s", string(exported))
	}
	if !strings.Contains(string(exported), `"type":"source_link"`) || !strings.Contains(string(exported), linkSource.ID) {
		t.Fatalf("export should include source links: %s", string(exported))
	}
	if !strings.Contains(string(exported), `"type":"source_link_event"`) || !strings.Contains(string(exported), "backup-audit") || strings.Contains(string(exported), "password=supersecretvalue") {
		t.Fatalf("export should include redacted source link events: %s", string(exported))
	}
	otherSource, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Public restore note",
		Text:        "Public restore note should stay outside scoped export.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText other source: %v", err)
	}
	scopedPath := filepath.Join(t.TempDir(), "knowledge-scoped-export.jsonl")
	scopedExport, err := store.ExportSnapshot(ctx, ExportOptions{OutputPath: scopedPath, RedactSensitive: true, SourceIDs: []string{source.ID}})
	if err != nil {
		t.Fatalf("ExportSnapshot scoped: %v", err)
	}
	if !scopedExport.Scoped || scopedExport.Sources != 1 || scopedExport.SourceLabels == 0 || scopedExport.SourceVersions == 0 || scopedExport.SourceLinks != 0 || scopedExport.SourceIDs[0] != source.ID {
		t.Fatalf("unexpected scoped export result: %#v", scopedExport)
	}
	scopedBytes, err := os.ReadFile(scopedPath)
	if err != nil {
		t.Fatalf("read scoped export: %v", err)
	}
	if strings.Contains(string(scopedBytes), otherSource.ID) || strings.Contains(string(scopedBytes), "Public restore note") {
		t.Fatalf("scoped export should not contain non-selected source: %s", string(scopedBytes))
	}
	if strings.Contains(string(scopedBytes), linkSource.ID) || strings.Contains(string(scopedBytes), `"type":"source_link"`) {
		t.Fatalf("scoped export should omit links to non-selected sources: %s", string(scopedBytes))
	}
	if strings.Contains(string(scopedBytes), `"type":"source_link_event"`) {
		t.Fatalf("scoped export should omit link events to non-selected sources: %s", string(scopedBytes))
	}
	if strings.Contains(string(scopedBytes), `"type":"url_domain_policy"`) {
		t.Fatalf("scoped export should not include global URL domain policies: %s", string(scopedBytes))
	}
	restoreStore, err := NewSQLiteStore(filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore restored: %v", err)
	}
	defer restoreStore.Close()
	dryRun, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: exportPath, DryRun: true})
	if err != nil {
		t.Fatalf("ImportSnapshot dry-run: %v", err)
	}
	if dryRun.Records == 0 || dryRun.Imported != 0 || dryRun.WouldImport == 0 || dryRun.URLPolicies != 2 || dryRun.Sources != 2 || dryRun.SourceLabels == 0 || dryRun.SourceVersions == 0 || dryRun.SourceLinks == 0 || dryRun.SourceLinkEvents == 0 || dryRun.Nodes == 0 {
		t.Fatalf("unexpected dry-run import result: %#v", dryRun)
	}
	restored, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: exportPath})
	if err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}
	if restored.Imported == 0 || restored.Sources != 2 || restored.SourceLabels == 0 || restored.SourceVersions == 0 || restored.SourceLinks == 0 || restored.SourceLinkEvents == 0 || restored.Nodes == 0 {
		t.Fatalf("unexpected import result: %#v", restored)
	}
	restoredPolicies, err := restoreStore.ListURLDomainPolicies(ctx)
	if err != nil {
		t.Fatalf("ListURLDomainPolicies restored: %v", err)
	}
	if len(restoredPolicies) != 2 {
		t.Fatalf("expected restored URL domain policies, got %#v", restoredPolicies)
	}
	if restored.SafetyBackupPath == "" || restored.SafetyBackup == nil {
		t.Fatalf("expected automatic safety backup before restore: %#v", restored)
	}
	if _, err := os.Stat(restored.SafetyBackupPath); err != nil {
		t.Fatalf("stat safety backup: %v", err)
	}
	importedSource, err := restoreStore.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetSource restored: %v", err)
	}
	if importedSource.Status != StatusDisabled {
		t.Fatalf("expected restored sensitive source to keep disabled status, got %#v", importedSource)
	}
	if len(importedSource.Labels) != 2 || importedSource.Labels[0] != "backup" || importedSource.Labels[1] != "sensitive review" {
		t.Fatalf("expected restored source labels, got %#v", importedSource)
	}
	restoredVersions, err := restoreStore.ListSourceVersions(ctx, source.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceVersions restored: %v", err)
	}
	if len(restoredVersions) == 0 || restoredVersions[0].Reason != "save_text" {
		t.Fatalf("expected restored source versions: %#v", restoredVersions)
	}
	restoredLinks, err := restoreStore.ListSourceLinks(ctx, source.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceLinks restored: %v", err)
	}
	if len(restoredLinks) == 0 || restoredLinks[0].RelatedSourceID != linkSource.ID {
		t.Fatalf("expected restored source links: %#v", restoredLinks)
	}
	restoredEvents, err := restoreStore.ListSourceLinkEvents(ctx, source.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceLinkEvents restored: %v", err)
	}
	if len(restoredEvents) == 0 || restoredEvents[0].Action != "link" || !stringSliceContains(restoredEvents[0].Terms, "backup-audit") || !strings.Contains(restoredEvents[0].Evidence[0], "pass...alue") {
		t.Fatalf("expected restored redacted source link events: %#v", restoredEvents)
	}
	conflictRun, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: exportPath, DryRun: true})
	if err != nil {
		t.Fatalf("ImportSnapshot conflict dry-run: %v", err)
	}
	if conflictRun.Conflicts == 0 || len(conflictRun.ConflictItems) == 0 || conflictRun.WouldImport != 0 {
		t.Fatalf("expected conflict preview for already restored snapshot: %#v", conflictRun)
	}
	overwriteDryRun, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: exportPath, DryRun: true, Overwrite: true})
	if err != nil {
		t.Fatalf("ImportSnapshot overwrite dry-run: %v", err)
	}
	if overwriteDryRun.Conflicts != 0 || overwriteDryRun.WouldImport == 0 {
		t.Fatalf("expected overwrite dry-run to treat existing records as importable: %#v", overwriteDryRun)
	}
	overwriteRun, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: exportPath, Overwrite: true})
	if err != nil {
		t.Fatalf("ImportSnapshot overwrite: %v", err)
	}
	if overwriteRun.Imported == 0 || overwriteRun.Conflicts != 0 {
		t.Fatalf("unexpected overwrite import result: %#v", overwriteRun)
	}
	if overwriteRun.SafetyBackupPath == "" || overwriteRun.SafetyBackup == nil || overwriteRun.SafetyBackup.Sources == 0 {
		t.Fatalf("expected populated safety backup before overwrite restore: %#v", overwriteRun)
	}
	brokenPath := filepath.Join(t.TempDir(), "broken-snapshot.jsonl")
	if err := os.WriteFile(brokenPath, []byte(`{"type":"node","data":{"id":"kdn_missing_source","source_id":"ksrc_missing_source","type":"section","text":"broken"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}
	brokenRun, err := restoreStore.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: brokenPath, DryRun: true})
	if err != nil {
		t.Fatalf("ImportSnapshot broken dry-run: %v", err)
	}
	if brokenRun.MissingReferences != 1 || brokenRun.Failed != 1 || len(brokenRun.Failures) != 1 {
		t.Fatalf("expected missing reference diagnostic for broken snapshot: %#v", brokenRun)
	}
}

func TestSQLiteStoreSourceQualityReport(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	good, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Quality good source",
		Text:        "Project Quality uses local source quality scoring. Project Quality improves maintenance decisions.",
		Kind:        SourceKindText,
		ProjectPath: "D:/project",
		Labels:      []string{"quality"},
		AutoLabels:  true,
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText good: %v", err)
	}
	poor := Source{ID: "ksrc_quality_poor", Kind: SourceKindText, URI: "knowledge://text/quality-poor", Title: "Quality poor source", ContentHash: "quality-poor", ProjectPath: "D:/project", Status: StatusDistilled, SourceTrust: 0.2}
	if err := store.SaveSource(ctx, poor); err != nil {
		t.Fatalf("SaveSource poor: %v", err)
	}
	sensitive, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Quality sensitive source",
		Text:        "Temporary token = supersecretqualityvalue should lower the quality score.",
		Kind:        SourceKindText,
		ProjectPath: "D:/project",
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("SaveText sensitive: %v", err)
	}
	isolated := Source{ID: "ksrc_quality_isolated", Kind: SourceKindText, URI: "knowledge://text/quality-isolated", Title: "Quality isolated source", ContentHash: "quality-isolated", ProjectPath: "D:/project", Status: StatusDistilled, SourceTrust: 0.8}
	if err := store.SaveSource(ctx, isolated); err != nil {
		t.Fatalf("SaveSource isolated: %v", err)
	}
	derivedGap := Source{ID: "ksrc_quality_derived_gap", Kind: SourceKindText, URI: "knowledge://text/quality-derived-gap", Title: "Quality derived gap", ContentHash: "quality-derived-gap", ProjectPath: "D:/project", Status: StatusParsed, SourceTrust: 0.8}
	if err := store.SaveSource(ctx, derivedGap); err != nil {
		t.Fatalf("SaveSource derivedGap: %v", err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:         "kdn_quality_derived_gap",
		SourceID:   derivedGap.ID,
		Type:       "document",
		Title:      "Quality derived gap",
		Text:       "Quality maintenance can rebuild derived cards and facts from parsed nodes.",
		TokenCount: 80,
	}); err != nil {
		t.Fatalf("SaveDocumentNode derivedGap: %v", err)
	}

	report, err := store.SourceQualityReport(ctx, ListSourcesOptions{ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("SourceQualityReport: %v", err)
	}
	if report.Count != 5 || report.AverageScore <= 0 || !stringSliceContains(report.Notes, "local_source_quality_no_llm") {
		t.Fatalf("unexpected report summary: %#v", report)
	}
	if report.Signals["missing_nodes"] == 0 || report.Actions["rebuild_cards_and_facts_from_existing_nodes"] == 0 {
		t.Fatalf("expected aggregated quality signals/actions: %#v", report)
	}
	if len(report.Items) < 2 || report.Items[0].Score > report.Items[1].Score {
		t.Fatalf("expected quality report to sort lowest score first: %#v", report.Items)
	}
	lowScoreReport, err := store.SourceQualityReport(ctx, ListSourcesOptions{ProjectPath: "D:/project", MaxQualityScore: 54, Limit: 10})
	if err != nil {
		t.Fatalf("SourceQualityReport max score: %v", err)
	}
	if lowScoreReport.Count == 0 {
		t.Fatalf("expected low score quality slice")
	}
	for _, item := range lowScoreReport.Items {
		if item.Score > 54 {
			t.Fatalf("quality max score filter leaked high score item: %#v", lowScoreReport.Items)
		}
	}
	goodItem, ok := sourceQualityItemByID(report, good.ID)
	if !ok || goodItem.Score < 70 {
		t.Fatalf("expected good source to score well: %#v", goodItem)
	}
	if good.FactCount == 0 {
		if !stringSliceContains(goodItem.Signals, "missing_facts") || !stringSliceContains(goodItem.Actions, "rebuild_cards_and_facts_from_existing_nodes") {
			t.Fatalf("expected missing facts to point at local rebuild: %#v", goodItem)
		}
	}
	poorItem, ok := sourceQualityItemByID(report, poor.ID)
	if !ok || poorItem.Score >= goodItem.Score || !stringSliceContains(poorItem.Signals, "missing_nodes") || !stringSliceContains(poorItem.Signals, "missing_cards") || !stringSliceContains(poorItem.Signals, "missing_labels") {
		t.Fatalf("expected poor source coverage signals: %#v", poorItem)
	}
	if stringSliceContains(poorItem.Actions, "rebuild_cards_and_facts_from_existing_nodes") || !stringSliceContains(poorItem.Actions, "refresh_or_reimport_to_rebuild_parsed_nodes") {
		t.Fatalf("missing-node source should prefer refresh/reimport over local rebuild: %#v", poorItem)
	}
	derivedGapItem, ok := sourceQualityItemByID(report, derivedGap.ID)
	if !ok || !stringSliceContains(derivedGapItem.Signals, "missing_cards") || !stringSliceContains(derivedGapItem.Actions, "rebuild_cards_and_facts_from_existing_nodes") {
		t.Fatalf("parsed source with missing derived data should recommend local rebuild: %#v", derivedGapItem)
	}
	sensitiveItem, ok := sourceQualityItemByID(report, sensitive.ID)
	if !ok || sensitiveItem.SensitiveFindings == 0 || !stringSliceContains(sensitiveItem.Signals, "possible_sensitive_content") {
		t.Fatalf("expected sensitive source signal: %#v", sensitiveItem)
	}
	plan, err := store.SourceQualityMaintenancePlan(ctx, ListSourcesOptions{ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("SourceQualityMaintenancePlan: %v", err)
	}
	if plan.Count == 0 || !stringSliceContains(plan.Notes, "local_quality_maintenance_plan_no_llm") {
		t.Fatalf("unexpected maintenance plan: %#v", plan)
	}
	assertSourceQualityActionOrder(t, plan, "disable_sensitive_sources", "refresh_or_reimport_missing_nodes")
	assertSourceQualityActionOrder(t, plan, "refresh_or_reimport_missing_nodes", "rebuild_derived_gaps")
	assertSourceQualityActionOrder(t, plan, "rebuild_derived_gaps", "refresh_topic_links")
	if action, ok := sourceQualityActionByKind(plan, "disable_sensitive_sources"); !ok || action.Count != 1 || !stringSliceContains(action.SourceIDs, sensitive.ID) || action.Tool != "knowledge_disable_quality_sensitive_sources" {
		t.Fatalf("expected sensitive maintenance action: %#v", plan.Actions)
	}
	if action, ok := sourceQualityActionByKind(plan, "rebuild_derived_gaps"); !ok || action.Count == 0 || action.Tool != "knowledge_rebuild_quality_gaps" {
		t.Fatalf("expected derived rebuild maintenance action: %#v", plan.Actions)
	} else if stringSliceContains(action.SourceIDs, poor.ID) {
		t.Fatalf("sources without parsed nodes should not be sent to derived rebuild: %#v", action)
	}
	if action, ok := sourceQualityActionByKind(plan, "refresh_or_reimport_missing_nodes"); !ok || action.Count == 0 || !stringSliceContains(action.SourceIDs, poor.ID) || action.Tool != "knowledge_refresh_sources" {
		t.Fatalf("expected missing-node refresh/reimport maintenance action: %#v", plan.Actions)
	}
	if action, ok := sourceQualityActionByKind(plan, "refresh_topic_links"); !ok || action.Count == 0 || action.Tool != "knowledge_refresh_topic_links" {
		t.Fatalf("expected topic-link maintenance action: %#v", plan.Actions)
	}
	missingNodesPreview, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{SourceIDs: []string{poor.ID}, Limit: 10},
		Actions: []string{"refresh_or_reimport_missing_nodes"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan missing nodes dry-run: %v", err)
	}
	if missingNodesPreview.Count != 1 || !missingNodesPreview.Results[0].DryRun || missingNodesPreview.Results[0].Failed != 1 || missingNodesPreview.Results[0].Error != "refresh_or_reimport_preview_failed" {
		t.Fatalf("expected missing-node dry-run to preview refresh failures without writing: %#v", missingNodesPreview)
	}
	previewResult, ok := missingNodesPreview.Results[0].Result.(SourceChangePreviewResult)
	if !ok || previewResult.Requested != 1 || previewResult.Failed != 1 || len(previewResult.Failures) != 1 || previewResult.Failures[0].SourceID != poor.ID {
		t.Fatalf("expected missing-node dry-run result to carry refresh preview failures: %#v", missingNodesPreview.Results[0].Result)
	}
	missingNodesExecution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{SourceIDs: []string{poor.ID}, Limit: 10},
		Actions: []string{"refresh_or_reimport_missing_nodes"},
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan missing nodes: %v", err)
	}
	if missingNodesExecution.Count != 1 || missingNodesExecution.Results[0].Failed != 1 || missingNodesExecution.Results[0].Error != "refresh_or_reimport_failed" {
		t.Fatalf("expected missing-node maintenance to attempt refresh/reimport and report source-level failure: %#v", missingNodesExecution)
	}
	refreshResult, ok := missingNodesExecution.Results[0].Result.(SourceRefreshResult)
	if !ok || refreshResult.Requested != 1 || refreshResult.Failed != 1 || len(refreshResult.Failures) != 1 || refreshResult.Failures[0].SourceID != poor.ID {
		t.Fatalf("expected missing-node maintenance result to carry refresh failures: %#v", missingNodesExecution.Results[0].Result)
	}
	executionPreview, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{ProjectPath: "D:/project", Limit: 10},
		Actions: []string{"disable_sensitive_sources", "backfill_labels"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan dry-run: %v", err)
	}
	if !executionPreview.DryRun || executionPreview.Count != 2 || !stringSliceContains(executionPreview.Notes, "local_quality_maintenance_execute_no_llm") {
		t.Fatalf("unexpected quality maintenance execution preview: %#v", executionPreview)
	}
	explicitSourcesPreview, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:              ListSourcesOptions{SourceIDs: []string{poor.ID, derivedGap.ID}, Limit: 1},
		Actions:             []string{"refresh_or_reimport_missing_nodes", "rebuild_derived_gaps"},
		DryRun:              true,
		MaxSourcesPerAction: 1,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan explicit source limit preview: %v", err)
	}
	for _, actionResult := range explicitSourcesPreview.Results {
		if actionResult.Error == "max_sources_per_action_exceeded" {
			t.Fatalf("explicit source filter should raise max_sources_per_action: %#v", explicitSourcesPreview)
		}
	}
	if explicitSourcesPreview.Plan.Quality.Count < 2 {
		t.Fatalf("explicit source filter should raise plan filter limit to include requested sources: %#v", explicitSourcesPreview.Plan.Quality)
	}
	policies := SourceQualityMaintenancePolicies()
	if len(policies) < 4 {
		t.Fatalf("expected maintenance policies: %#v", policies)
	}
	for _, policy := range policies {
		if !stringSliceContains(policy.Actions, "refresh_or_reimport_missing_nodes") {
			t.Fatalf("policy %s should include missing-node refresh before derived rebuild: %#v", policy.Name, policy.Actions)
		}
		if stringSliceContains(policy.Actions, "rebuild_derived_gaps") {
			refreshIdx := stringSliceIndex(policy.Actions, "refresh_or_reimport_missing_nodes")
			rebuildIdx := stringSliceIndex(policy.Actions, "rebuild_derived_gaps")
			if refreshIdx < 0 || rebuildIdx < 0 || refreshIdx > rebuildIdx {
				t.Fatalf("policy %s should refresh missing nodes before derived rebuild: %#v", policy.Name, policy.Actions)
			}
		}
	}
	enriched, ok := SourceQualityMaintenancePolicyByName("enriched")
	if !ok || !enriched.MayUseLLMForStructuring || enriched.QueryRequiresLLM {
		t.Fatalf("unexpected enriched maintenance policy: %#v", enriched)
	}
	policyPreview, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter: ListSourcesOptions{SourceIDs: []string{poor.ID}, Limit: 10},
		Policy: "enriched",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan policy preview: %v", err)
	}
	if !stringSliceContains(policyPreview.Notes, "policy_enriched") || !stringSliceContains(policyPreview.Notes, "storage_may_use_llm_for_structuring") || policyPreview.Count == 0 {
		t.Fatalf("unexpected policy preview: %#v", policyPreview)
	}
	blockedSensitiveExecution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{SourceIDs: []string{sensitive.ID}, Limit: 10},
		Actions: []string{"disable_sensitive_sources"},
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan blocked sensitive: %v", err)
	}
	if blockedSensitiveExecution.Count != 1 || blockedSensitiveExecution.Results[0].Error != "allow_sensitive_disable_required" || blockedSensitiveExecution.Results[0].Skipped == 0 {
		t.Fatalf("expected sensitive disable guardrail: %#v", blockedSensitiveExecution)
	}
	limitedExecution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:              ListSourcesOptions{ProjectPath: "D:/project", Limit: 10},
		Actions:             []string{"backfill_labels"},
		MaxSourcesPerAction: 1,
		DryRun:              true,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan max sources: %v", err)
	}
	if limitedExecution.Count != 1 || limitedExecution.Results[0].Error != "max_sources_per_action_exceeded" {
		t.Fatalf("expected max source guardrail: %#v", limitedExecution)
	}
	labelExecution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{SourceIDs: []string{poor.ID}, Limit: 10},
		Actions: []string{"backfill_labels"},
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan labels: %v", err)
	}
	if labelExecution.Count != 1 || labelExecution.Results[0].Updated == 0 {
		t.Fatalf("expected label execution to update poor source: %#v", labelExecution)
	}
	linkExecution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:  ListSourcesOptions{ProjectPath: "D:/project", Limit: 10},
		Actions: []string{"refresh_topic_links"},
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan topic links: %v", err)
	}
	if linkExecution.Count != 1 || linkExecution.Results[0].Updated == 0 {
		t.Fatalf("expected topic-link execution to update sources: %#v", linkExecution)
	}
	linkedStats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after quality topic links: %v", err)
	}
	if linkedStats.SourceLinks == 0 {
		t.Fatalf("expected source links after quality maintenance: %#v", linkedStats)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if finding, ok := doctorFinding(doctor, "low_quality_sources"); !ok || finding.Filter == nil || len(finding.SourceIDs) == 0 {
		t.Fatalf("expected low_quality_sources doctor finding: %#v", doctor.Findings)
	}
}

func TestSourceQualityMaintenanceRebuildsDerivedFactsFromNodes(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:          "ksrc_quality_rebuild",
		Kind:        SourceKindText,
		URI:         "knowledge://text/quality-rebuild",
		Title:       "中文维护回填",
		ContentHash: "quality-rebuild",
		ProjectPath: "D:/project",
		SourceTrust: 0.8,
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:         "kdn_quality_rebuild",
		SourceID:   source.ID,
		Type:       "document",
		Title:      "中文维护回填",
		Text:       "知识库接口用于本地召回，并提供来源摘要。维护计划通过本地重建回填结构化事实。",
		TokenCount: 80,
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}

	plan, err := store.SourceQualityMaintenancePlan(ctx, ListSourcesOptions{SourceIDs: []string{source.ID}, Limit: 10})
	if err != nil {
		t.Fatalf("SourceQualityMaintenancePlan: %v", err)
	}
	if action, ok := sourceQualityActionByKind(plan, "rebuild_derived_gaps"); !ok || action.Count != 1 || !stringSliceContains(action.SourceIDs, source.ID) {
		t.Fatalf("expected rebuild action for parsed source with missing derived data: %#v", plan.Actions)
	}

	execution, err := store.ExecuteSourceQualityMaintenancePlan(ctx, SourceQualityMaintenanceExecuteRequest{
		Filter:      ListSourcesOptions{SourceIDs: []string{source.ID}, Limit: 10},
		Actions:     []string{"rebuild_derived_gaps"},
		DistillMode: DistillModeRules,
	})
	if err != nil {
		t.Fatalf("ExecuteSourceQualityMaintenancePlan: %v", err)
	}
	if execution.Count != 1 || execution.Results[0].Kind != "rebuild_derived_gaps" || execution.Results[0].Updated != 1 || execution.Results[0].Failed != 0 {
		t.Fatalf("unexpected rebuild execution result: %#v", execution)
	}

	rebuilt, err := store.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetSource rebuilt: %v", err)
	}
	if rebuilt.CardCount == 0 || rebuilt.FactCount == 0 || rebuilt.Status != StatusDistilled {
		t.Fatalf("expected derived cards and facts after rebuild: %#v", rebuilt)
	}
	facts, err := store.ListFactsBySource(ctx, source.ID, 20)
	if err != nil {
		t.Fatalf("ListFactsBySource: %v", err)
	}
	if !containsStoredFact(facts, "用于", "本地召回") || !containsStoredFact(facts, "提供", "来源摘要") {
		t.Fatalf("expected rebuilt Chinese facts from parsed node: %#v", facts)
	}
}

func TestRebuildSourcesDerivedReportsPartialFailures(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	sources := []Source{
		{ID: "ksrc_quality_rebuild_ok", Kind: SourceKindText, URI: "knowledge://text/rebuild-ok", Title: "Rebuild ok", ContentHash: "rebuild-ok", ProjectPath: "D:/project", SourceTrust: 0.8, Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_quality_rebuild_missing_nodes", Kind: SourceKindText, URI: "knowledge://text/rebuild-missing-nodes", Title: "Rebuild missing nodes", ContentHash: "rebuild-missing-nodes", ProjectPath: "D:/project", SourceTrust: 0.8, Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatalf("SaveSource %s: %v", source.ID, err)
		}
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:         "kdn_quality_rebuild_ok",
		SourceID:   sources[0].ID,
		Type:       "document",
		Title:      "Rebuild ok",
		Text:       "知识库维护用于本地回填，并提供结构化事实。",
		TokenCount: 80,
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}

	rebuild := store.RebuildSourcesDerived(ctx, []string{sources[0].ID, sources[1].ID}, DistillModeRules)
	if rebuild.Requested != 2 || rebuild.Rebuilt != 1 || rebuild.Failed != 1 || len(rebuild.Failures) != 1 || rebuild.Failures[0].SourceID != sources[1].ID {
		t.Fatalf("unexpected partial rebuild payload: %#v", rebuild)
	}
	rebuilt, err := store.GetSource(ctx, sources[0].ID)
	if err != nil {
		t.Fatalf("GetSource rebuilt: %v", err)
	}
	if rebuilt.CardCount == 0 || rebuilt.FactCount == 0 {
		t.Fatalf("successful source should still be rebuilt: %#v", rebuilt)
	}
}

func TestSourceRefreshAndRebuildResultsExposeLockWarnings(t *testing.T) {
	refresh := SourceRefreshResult{}
	refresh.Failed++
	appendSourceRefreshFailure(&refresh, "ksrc_locked", errors.New("database is locked"))
	if len(refresh.Warnings) != 1 || !strings.Contains(refresh.Warnings[0], "retry later") {
		t.Fatalf("refresh lock warning missing: %#v", refresh)
	}

	rebuild := SourceRebuildResult{}
	rebuild.Failed++
	appendSourceRebuildFailure(&rebuild, "ksrc_locked", errors.New("SQLITE_BUSY: database is locked"))
	if len(rebuild.Warnings) != 1 || !strings.Contains(rebuild.Warnings[0], "rebuild") {
		t.Fatalf("rebuild lock warning missing: %#v", rebuild)
	}
}

func TestSourceQualityMaintenanceAggregatesActionWarnings(t *testing.T) {
	result := SourceQualityMaintenanceExecuteResult{}
	action := SourceQualityMaintenanceActionResult{
		Kind:     "refresh_or_reimport_missing_nodes",
		Warnings: []string{"ksrc_locked: transient sqlite lock during refresh; retry later"},
	}
	result.Results = append(result.Results, action)
	result.Warnings = append(result.Warnings, action.Warnings...)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "sqlite lock") {
		t.Fatalf("quality maintenance warning aggregation missing: %#v", result)
	}
}

func TestSQLiteStoreMaintain(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title:       "Maintenance note",
		Text:        "Knowledge database maintenance should optimize FTS indexes.",
		ProjectPath: "D:/project",
		SaveScope:   SaveScopeProject,
		DistillMode: DistillModeRules,
	}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	result := store.Maintain(ctx, false)
	if !result.IntegrityOK || !result.Checkpointed || len(result.OptimizedFTS) != 3 || len(result.Errors) != 0 {
		t.Fatalf("unexpected maintenance result: %#v", result)
	}
}

func TestMaintenanceOperationLockedErrorsAreWarnings(t *testing.T) {
	var result MaintenanceResult
	appendMaintenanceOperationError(&result, "wal_checkpoint", errors.New("database is locked"))
	if len(result.Errors) != 0 {
		t.Fatalf("locked maintenance operation should not be an error: %#v", result)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "wal_checkpoint") {
		t.Fatalf("locked maintenance operation warning missing: %#v", result)
	}

	appendMaintenanceOperationError(&result, "vacuum", errors.New("disk I/O error"))
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "disk I/O error") {
		t.Fatalf("non-lock maintenance operation should stay an error: %#v", result)
	}
}

func TestSQLiteStoreSaveURLsReportsPerItemFailures(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	result := store.SaveURLs(ctx, URLBatchSaveRequest{
		URLs:      []string{"notaurl，notaurl；http://127.0.0.1/private"},
		SaveScope: SaveScopePersonal,
	})
	if result.Requested != 2 || result.Skipped != 1 || result.Failed != 2 || len(result.Items) != 3 {
		t.Fatalf("unexpected SaveURLs result: %#v", result)
	}
	if result.Items[1].Status != "skipped_duplicate" {
		t.Fatalf("duplicate URL should be reported as skipped: %#v", result.Items)
	}
}

func TestDiscoverURLsFromText(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.UpdateURLDomainPolicies(ctx, URLDomainPolicyUpdateRequest{
		BlockDomains: []string{"blocked.example.com"},
		Replace:      true,
		Reason:       "test block",
	}); err != nil {
		t.Fatalf("UpdateURLDomainPolicies: %v", err)
	}
	text := `See https://example.com/a?x=1). <a href="/docs/b">B</a>
		<loc>https://sub.example.com/sitemap-page</loc>
		<a href="https://blocked.example.com/private">blocked</a>
		http://127.0.0.1/private example.org/path
		https://example.com/chinese-a，https://example.com/chinese-b；example.com/chinese-c、docs.example.com/chinese-d`
	result, err := store.DiscoverURLs(ctx, URLDiscoveryRequest{
		Text:           text,
		BaseURL:        "https://example.com/root/index.html",
		SameDomainOnly: true,
		Limit:          20,
	})
	if err != nil {
		t.Fatalf("DiscoverURLs: %v", err)
	}
	if result.Candidates != 7 || result.Rejected < 2 || len(result.URLs) != 7 {
		t.Fatalf("unexpected discovery result: %#v", result)
	}
	joined := strings.Join(result.URLs, "\n")
	for _, want := range []string{"https://example.com/a?x=1", "https://example.com/docs/b", "https://sub.example.com/sitemap-page", "https://example.com/chinese-a", "https://example.com/chinese-b", "https://example.com/chinese-c", "https://docs.example.com/chinese-d"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing discovered URL %s in %#v", want, result.URLs)
		}
	}
	if strings.Contains(joined, "blocked.example.com") || strings.Contains(joined, "127.0.0.1") || strings.Contains(joined, "example.org") {
		t.Fatalf("discovery should reject blocked, local, and outside-domain URLs: %#v", result)
	}
}

func TestSQLiteStoreURLDomainPolicies(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	update, err := store.UpdateURLDomainPolicies(ctx, URLDomainPolicyUpdateRequest{
		AllowDomains: []string{"example.com，docs.example.com；example.com"},
		BlockDomains: []string{"blocked.example.com、private.example.com"},
		Replace:      true,
		Reason:       "test policy",
	})
	if err != nil {
		t.Fatalf("UpdateURLDomainPolicies: %v", err)
	}
	if update.Updated != 4 || len(update.Policies) != 4 {
		t.Fatalf("unexpected URL policy update: %#v", update)
	}
	allowed, err := store.CheckURLDomainPolicy(ctx, "https://docs.example.com/path#frag")
	if err != nil || !allowed.Allowed || allowed.Host != "docs.example.com" || allowed.MatchedPolicy == nil || allowed.MatchedPolicy.Action != URLDomainActionAllow {
		t.Fatalf("expected allowed example.com policy, got %#v err=%v", allowed, err)
	}
	blocked, err := store.CheckURLDomainPolicy(ctx, "https://blocked.example.com/secret")
	if err != nil || blocked.Allowed || blocked.MatchedPolicy == nil || blocked.MatchedPolicy.Action != URLDomainActionBlock {
		t.Fatalf("expected blocked policy, got %#v err=%v", blocked, err)
	}
	privateBlocked, err := store.CheckURLDomainPolicy(ctx, "https://private.example.com/secret")
	if err != nil || privateBlocked.Allowed || privateBlocked.MatchedPolicy == nil || privateBlocked.MatchedPolicy.Action != URLDomainActionBlock {
		t.Fatalf("expected private blocked policy, got %#v err=%v", privateBlocked, err)
	}
	implicitDeny, err := store.CheckURLDomainPolicy(ctx, "https://other.example.org")
	if err != nil || implicitDeny.Allowed || implicitDeny.Reason != "no allow policy matched" {
		t.Fatalf("expected allow-list implicit deny, got %#v err=%v", implicitDeny, err)
	}
	saveBlocked := store.SaveURLs(ctx, URLBatchSaveRequest{URLs: []string{"https://blocked.example.com/secret"}, SaveScope: SaveScopePersonal})
	if saveBlocked.Failed != 1 || !strings.Contains(saveBlocked.Items[0].Error, "domain policy") {
		t.Fatalf("blocked URL save should fail before storing: %#v", saveBlocked)
	}
}

func TestNormalizeURLPolicyDomainHandlesURLLikeInputs(t *testing.T) {
	cases := map[string]string{
		" https://Docs.Example.com/path?q=1 ":         "docs.example.com",
		"Docs.Example.com/path?q=1":                   "docs.example.com",
		"Example.COM:8443/path":                       "example.com",
		" https://*.Example.COM./path ":               "example.com",
		" *.Example.COM. ":                            "example.com",
		" https://user:pass@Docs.Example.com/path ":   "docs.example.com",
		" user:pass@Docs.Example.com/private?q=keep ": "docs.example.com",
	}
	for input, want := range cases {
		if got := normalizeURLPolicyDomain(input); got != want {
			t.Fatalf("normalizeURLPolicyDomain(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSQLiteStoreListSourcesByDomain(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	sources := []Source{
		{ID: "ksrc_domain_1", Kind: SourceKindURL, URI: "https://example.com/a", CanonicalURI: "https://example.com/a", SiteName: "example.com", Title: "A", ContentHash: "domain-1", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_domain_2", Kind: SourceKindURL, URI: "https://docs.example.com/b", CanonicalURI: "https://docs.example.com/b", SiteName: "docs.example.com", Title: "B", ContentHash: "domain-2", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_domain_3", Kind: SourceKindURL, URI: "https://other.example.org/c", CanonicalURI: "https://other.example.org/c", SiteName: "other.example.org", Title: "C", ContentHash: "domain-3", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_domain_4", Kind: SourceKindURL, URI: "https://cdn.example.com", CanonicalURI: "https://cdn.example.com", Title: "D", ContentHash: "domain-4", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatalf("SaveSource: %v", err)
		}
		if err := store.SaveCard(ctx, Card{
			ID:          "kcard_" + source.ID,
			SourceID:    source.ID,
			Title:       source.Title,
			Claim:       "Domain recall anchor keeps public URL knowledge searchable by site.",
			Summary:     "Domain recall anchor",
			Confidence:  0.8,
			Importance:  0.8,
			SourceTrust: 0.8,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("SaveCard: %v", err)
		}
	}
	exampleSources, err := store.ListSources(ctx, ListSourcesOptions{Domain: "example.com", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources by domain: %v", err)
	}
	if len(exampleSources) != 3 {
		t.Fatalf("domain filter should include exact domain and subdomains: %#v", exampleSources)
	}
	docsSources, err := store.ListSources(ctx, ListSourcesOptions{Domain: "https://docs.example.com/path", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources by URL-like domain: %v", err)
	}
	if len(docsSources) != 1 || docsSources[0].ID != "ksrc_domain_2" {
		t.Fatalf("domain filter should normalize URL-like values: %#v", docsSources)
	}
	docsUserinfoSources, err := store.ListSources(ctx, ListSourcesOptions{Domain: "user:pass@Docs.Example.com/private", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources by protocol-less userinfo domain: %v", err)
	}
	if len(docsUserinfoSources) != 1 || docsUserinfoSources[0].ID != "ksrc_domain_2" {
		t.Fatalf("domain filter should normalize protocol-less userinfo values: %#v", docsUserinfoSources)
	}
	exampleResults, err := store.Search(ctx, SearchOptions{Query: "domain recall", Domain: "example.com", Limit: 10})
	if err != nil {
		t.Fatalf("Search by domain: %v", err)
	}
	if len(exampleResults) != 3 {
		t.Fatalf("search domain filter should include exact domain and subdomains: %#v", exampleResults)
	}
	docsResults, err := store.Search(ctx, SearchOptions{Query: "domain recall", Domain: "https://docs.example.com/path", Limit: 10})
	if err != nil {
		t.Fatalf("Search by URL-like domain: %v", err)
	}
	if len(docsResults) != 1 || docsResults[0].Source.ID != "ksrc_domain_2" {
		t.Fatalf("search domain filter should normalize URL-like values: %#v", docsResults)
	}
	docsUserinfoResults, err := store.Search(ctx, SearchOptions{Query: "domain recall", Domain: "user:pass@Docs.Example.com/private", Limit: 10})
	if err != nil {
		t.Fatalf("Search by protocol-less userinfo domain: %v", err)
	}
	if len(docsUserinfoResults) != 1 || docsUserinfoResults[0].Source.ID != "ksrc_domain_2" {
		t.Fatalf("search domain filter should normalize protocol-less userinfo values: %#v", docsUserinfoResults)
	}
	missingResults, err := store.Search(ctx, SearchOptions{Query: "domain recall", Domain: "missing.example.net", Limit: 10})
	if err != nil {
		t.Fatalf("Search by missing domain: %v", err)
	}
	if len(missingResults) != 0 {
		t.Fatalf("search domain filter leaked other domains: %#v", missingResults)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SourcesByDomain["example.com"] != 1 || stats.SourcesByDomain["docs.example.com"] != 1 {
		t.Fatalf("unexpected domain stats: %#v", stats.SourcesByDomain)
	}
}

func TestSQLiteStoreNormalizesDirectListAndSearchFilterScalars(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:          "ksrc_scalar_filters",
		Kind:        SourceKindMarkdown,
		URI:         "scalar.md",
		Title:       "Scalar filters",
		ContentHash: "scalar-filters",
		OwnerID:     "owner-a",
		TenantID:    "tenant-a",
		ProjectPath: "D:/project",
		Status:      StatusDistilled,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:          "kcard_scalar_filters",
		SourceID:    source.ID,
		Title:       "Scalar filters",
		Claim:       "Scalar filter normalization keeps direct knowledge store calls reliable.",
		Summary:     "Scalar filter normalization",
		ProjectPath: source.ProjectPath,
		OwnerID:     source.OwnerID,
		TenantID:    source.TenantID,
		Confidence:  0.8,
		Importance:  0.8,
		SourceTrust: 0.8,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	listed, err := store.ListSources(ctx, ListSourcesOptions{
		OwnerID:     " owner-a ",
		TenantID:    " tenant-a ",
		SearchScope: " Project ",
		ProjectPath: " D:/project ",
		Status:      " Distilled ",
		Kind:        " Markdown ",
		Query:       " scalar ",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != source.ID {
		t.Fatalf("direct ListSources should normalize scalar filters: %#v", listed)
	}
	results, err := store.Search(ctx, SearchOptions{
		Query:       " scalar filter ",
		OwnerID:     " owner-a ",
		TenantID:    " tenant-a ",
		SearchScope: " Project ",
		ProjectPath: " D:/project ",
		SourceKinds: []string{" PDF，Markdown；text "},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Source.ID != source.ID {
		t.Fatalf("direct Search should normalize scalar filters: %#v", results)
	}
	listedByKinds, err := store.ListSources(ctx, ListSourcesOptions{
		ProjectPath: "D:/project",
		SourceKinds: []string{"pdf，markdown；text"},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListSources source kinds: %v", err)
	}
	if len(listedByKinds) != 1 || listedByKinds[0].ID != source.ID {
		t.Fatalf("direct ListSources should split common source kind separators: %#v", listedByKinds)
	}
}

func TestSQLiteStoreFactGraphFiltersAndSummaries(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:          "ksrc_graph_1",
		Kind:        SourceKindText,
		URI:         "knowledge://text/graph",
		Title:       "Graph note",
		ContentHash: "graph-1",
		ProjectPath: "D:/project",
		Status:      StatusDistilled,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.ReplaceSourceLabels(ctx, source.ID, []string{"alpha", "local"}); err != nil {
		t.Fatalf("ReplaceSourceLabels source: %v", err)
	}
	urlSource := Source{
		ID:           "ksrc_graph_url",
		Kind:         SourceKindURL,
		URI:          "https://docs.example.com/alpha",
		CanonicalURI: "https://docs.example.com/alpha",
		Title:        "Graph URL note",
		SiteName:     "docs.example.com",
		ContentHash:  "graph-url-1",
		ProjectPath:  "D:/project",
		Status:       StatusDistilled,
		FetchedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.SaveSource(ctx, urlSource); err != nil {
		t.Fatalf("SaveSource URL: %v", err)
	}
	if err := store.ReplaceSourceLabels(ctx, urlSource.ID, []string{"alpha", "docs"}); err != nil {
		t.Fatalf("ReplaceSourceLabels URL: %v", err)
	}
	card := Card{
		ID:          "kcard_graph_1",
		SourceID:    source.ID,
		Title:       "Graph card",
		Claim:       "Project Alpha uses SQLite and depends on FTS5.",
		ProjectPath: source.ProjectPath,
		Confidence:  0.9,
		Importance:  0.8,
		SourceTrust: 0.8,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "kfact_graph_1", CardID: card.ID, SourceID: source.ID, Subject: "Project Alpha", Predicate: "uses", Object: "SQLite", Confidence: 0.95}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertFact uses: %v", err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "kfact_graph_2", CardID: card.ID, SourceID: source.ID, Subject: "Project Alpha", Predicate: "depends_on", Object: "FTS5", Confidence: 0.85}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertFact depends_on: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	graph, err := store.FactGraph(ctx, SearchOptions{Entity: "Project Alpha", Predicate: "uses", ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("FactGraph: %v", err)
	}
	if graph.Count != 1 || len(graph.Edges) != 1 || graph.Edges[0].Subject != "Project Alpha" || graph.Edges[0].Predicate != "uses" || graph.Edges[0].Object != "SQLite" {
		t.Fatalf("unexpected filtered graph: %#v", graph)
	}
	if graph.Entity != "Project Alpha" || graph.Predicate != "uses" || graph.Edges[0].Citation == "" {
		t.Fatalf("graph should echo filters and include citations: %#v", graph)
	}
	if len(graph.TopEntities) == 0 || graph.TopEntities[0].Label != "Project Alpha" || len(graph.TopPredicates) == 0 || graph.TopPredicates[0].Label != "uses" {
		t.Fatalf("graph should return top entity and predicate summaries: %#v", graph)
	}
	entityIndex, err := store.FactIndex(ctx, FactIndexOptions{SearchOptions: SearchOptions{ProjectPath: "D:/project", Limit: 10}, Kind: "entity"})
	if err != nil {
		t.Fatalf("FactIndex entity: %v", err)
	}
	if entityIndex.Count == 0 || entityIndex.Items[0].Label != "Project Alpha" || entityIndex.Items[0].Count != 2 || entityIndex.Items[0].SourceCount != 1 {
		t.Fatalf("unexpected entity index: %#v", entityIndex)
	}
	predicateIndex, err := store.FactIndex(ctx, FactIndexOptions{SearchOptions: SearchOptions{Query: "sqlite", ProjectPath: "D:/project", Limit: 10}, Kind: "predicate"})
	if err != nil {
		t.Fatalf("FactIndex predicate: %v", err)
	}
	if predicateIndex.Count != 1 || predicateIndex.Items[0].Label != "uses" || len(predicateIndex.Items[0].Examples) == 0 {
		t.Fatalf("unexpected predicate index: %#v", predicateIndex)
	}
	profile, err := store.EntityProfile(ctx, SearchOptions{Entity: "Project Alpha", ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("EntityProfile: %v", err)
	}
	if profile.Entity != "Project Alpha" || profile.Count != 2 || len(profile.RelatedEntities) != 2 || len(profile.Predicates) != 2 || len(profile.Citations) == 0 {
		t.Fatalf("unexpected entity profile: %#v", profile)
	}
	suggest, err := store.Suggest(ctx, KnowledgeSuggestOptions{SearchOptions: SearchOptions{Query: "Alpha", ProjectPath: "D:/project", Limit: 20}})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if suggest.Count == 0 || !hasKnowledgeSuggestion(suggest.Items, "entity", "Project Alpha") || !hasKnowledgeSuggestion(suggest.Items, "source", "Graph URL note") {
		t.Fatalf("unexpected suggestions: %#v", suggest)
	}
	domainSuggest, err := store.Suggest(ctx, KnowledgeSuggestOptions{SearchOptions: SearchOptions{Query: "docs", ProjectPath: "D:/project", Limit: 20}, Kinds: []string{"domain"}})
	if err != nil {
		t.Fatalf("Suggest domain: %v", err)
	}
	if !hasKnowledgeSuggestion(domainSuggest.Items, "domain", "docs.example.com") {
		t.Fatalf("unexpected domain suggestions: %#v", domainSuggest)
	}
	labelSuggest, err := store.Suggest(ctx, KnowledgeSuggestOptions{SearchOptions: SearchOptions{Query: "alp", ProjectPath: "D:/project", Limit: 20}, Kinds: []string{"label"}})
	if err != nil {
		t.Fatalf("Suggest label: %v", err)
	}
	if !hasKnowledgeSuggestion(labelSuggest.Items, "label", "alpha") {
		t.Fatalf("unexpected label suggestions: %#v", labelSuggest)
	}
	labelSummaries, err := store.ListSourceLabels(ctx, ListSourcesOptions{ProjectPath: "D:/project", Limit: 20})
	if err != nil {
		t.Fatalf("ListSourceLabels: %v", err)
	}
	if !hasSourceLabelSummary(labelSummaries, "alpha", 2) || !hasSourceLabelSummary(labelSummaries, "docs", 1) {
		t.Fatalf("unexpected source label summaries: %#v", labelSummaries)
	}
	dryRunLabels, err := store.UpdateSourceLabels(ctx, SourceLabelUpdateRequest{
		Filter:    ListSourcesOptions{ProjectPath: "D:/project", Kind: SourceKindURL},
		AddLabels: []string{"review"},
		DryRun:    true,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("UpdateSourceLabels dry-run: %v", err)
	}
	if dryRunLabels.Requested != 1 || dryRunLabels.Updated != 1 || dryRunLabels.LabelChanges[0].SourceID != urlSource.ID || !stringSliceContains(dryRunLabels.LabelChanges[0].After, "review") {
		t.Fatalf("unexpected dry-run label update: %#v", dryRunLabels)
	}
	updateLabels, err := store.UpdateSourceLabels(ctx, SourceLabelUpdateRequest{
		SourceIDs:     []string{urlSource.ID},
		RemoveLabels:  []string{"docs"},
		ReplaceLabels: nil,
		AddLabels:     []string{"review"},
		Limit:         20,
	})
	if err != nil {
		t.Fatalf("UpdateSourceLabels: %v", err)
	}
	if updateLabels.Requested != 1 || updateLabels.Updated != 1 || updateLabels.Failed != 0 {
		t.Fatalf("unexpected label update result: %#v", updateLabels)
	}
	updatedURLSource, err := store.GetSource(ctx, urlSource.ID)
	if err != nil {
		t.Fatalf("GetSource after label update: %v", err)
	}
	if !stringSliceContains(updatedURLSource.Labels, "review") || stringSliceContains(updatedURLSource.Labels, "docs") {
		t.Fatalf("unexpected updated source labels: %#v", updatedURLSource.Labels)
	}
	renameLabels, err := store.UpdateSourceLabels(ctx, SourceLabelUpdateRequest{
		RenameFrom: "review",
		RenameTo:   "approved",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("UpdateSourceLabels rename: %v", err)
	}
	if renameLabels.Requested != 1 || renameLabels.Updated != 1 || renameLabels.Mode != "rename" {
		t.Fatalf("unexpected label rename result: %#v", renameLabels)
	}
	renamedURLSource, err := store.GetSource(ctx, urlSource.ID)
	if err != nil {
		t.Fatalf("GetSource after label rename: %v", err)
	}
	if !stringSliceContains(renamedURLSource.Labels, "approved") || stringSliceContains(renamedURLSource.Labels, "review") {
		t.Fatalf("unexpected renamed source labels: %#v", renamedURLSource.Labels)
	}
	facets, err := store.SearchFacets(ctx, SearchOptions{Query: "Project Alpha SQLite", ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("SearchFacets: %v", err)
	}
	if facets.Count == 0 || !hasSearchFacet(facets.ResultTypes, "card") || !hasSearchFacet(facets.SourceKinds, SourceKindText) || !hasSearchFacet(facets.Domains, "local") || !hasSearchFacet(facets.Labels, "alpha") || !hasSearchFacet(facets.Entities, "Project Alpha") || !hasSearchFacet(facets.Predicates, "uses") {
		t.Fatalf("unexpected search facets: %#v", facets)
	}
	browseFacets, err := store.SearchFacets(ctx, SearchOptions{ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("SearchFacets browse: %v", err)
	}
	if browseFacets.Count != 2 || !hasSearchFacet(browseFacets.ResultTypes, "card") || !hasSearchFacet(browseFacets.ResultTypes, "fact") || !hasSearchFacet(browseFacets.SourceKinds, SourceKindText) || !hasSearchFacet(browseFacets.SourceKinds, SourceKindURL) || !hasSearchFacet(browseFacets.Domains, "docs.example.com") || !hasSearchFacet(browseFacets.Labels, "approved") || !hasSearchFacet(browseFacets.Entities, "Project Alpha") || !hasSearchFacet(browseFacets.Predicates, "depends_on") {
		t.Fatalf("unexpected browse facets: %#v", browseFacets)
	}
	sourceScoped, err := store.Search(ctx, SearchOptions{Query: "Project Alpha", SourceIDs: []string{source.ID}, ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("source-scoped Search: %v", err)
	}
	if len(sourceScoped) == 0 || sourceScoped[0].Source.ID != source.ID {
		t.Fatalf("expected source-scoped search hit, got %#v", sourceScoped)
	}
	otherScoped, err := store.Search(ctx, SearchOptions{Query: "Project Alpha", SourceIDs: []string{"ksrc_missing"}, ProjectPath: "D:/project", Limit: 10})
	if err != nil {
		t.Fatalf("missing source-scoped Search: %v", err)
	}
	if len(otherScoped) != 0 {
		t.Fatalf("unexpected source-scoped hits: %#v", otherScoped)
	}
}

func hasKnowledgeSuggestion(items []KnowledgeSuggestion, kind, label string) bool {
	for _, item := range items {
		if item.Kind == kind && strings.EqualFold(item.Label, label) {
			return true
		}
	}
	return false
}

func hasSearchFacet(items []SearchFacetBucket, label string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Label, label) && item.Count > 0 {
			return true
		}
	}
	return false
}

func hasSourceLabelSummary(items []SourceLabelSummary, label string, count int) bool {
	for _, item := range items {
		if strings.EqualFold(item.Label, label) && item.Count == count {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func stringSliceIndex(values []string, want string) int {
	for idx, value := range values {
		if strings.EqualFold(value, want) {
			return idx
		}
	}
	return -1
}

func TestSQLiteStoreListDuplicateCards(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	sources := []Source{
		{ID: "ksrc_dup_1", Kind: SourceKindText, URI: "knowledge://text/1", Title: "Dup one", ContentHash: "dup-1", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_dup_2", Kind: SourceKindText, URI: "knowledge://text/2", Title: "Dup two", ContentHash: "dup-2", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatalf("SaveSource: %v", err)
		}
	}
	claim := "Repeated project policy claims should be detected across imported knowledge cards"
	for i, source := range sources {
		if err := store.SaveCard(ctx, Card{ID: "kcard_dup_" + source.ID, SourceID: source.ID, Title: source.Title, Claim: claim, ProjectPath: source.ProjectPath, Confidence: 0.8, Importance: float64(i + 1), SourceTrust: 0.8}); err != nil {
			t.Fatalf("SaveCard: %v", err)
		}
	}
	groups, err := store.ListDuplicateCards(ctx, 10)
	if err != nil {
		t.Fatalf("ListDuplicateCards: %v", err)
	}
	if len(groups) != 1 || groups[0].Count != 2 || len(groups[0].SourceIDs) != 2 {
		t.Fatalf("unexpected duplicate groups: %#v", groups)
	}
	suppressed, err := store.SuppressDuplicateCards(ctx, DuplicateCardSuppressionRequest{Key: groups[0].Key, ProjectPath: "D:/project"})
	if err != nil {
		t.Fatalf("SuppressDuplicateCards: %v", err)
	}
	if suppressed.Suppressed != 1 || suppressed.KeptCardID == "" || len(suppressed.CardIDs) != 1 {
		t.Fatalf("unexpected suppression result: %#v", suppressed)
	}
	groups, err = store.ListDuplicateCards(ctx, 10)
	if err != nil {
		t.Fatalf("ListDuplicateCards after suppression: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("suppressed duplicate group should be hidden: %#v", groups)
	}
	items, err := store.ListSuppressedCards(ctx, 10)
	if err != nil {
		t.Fatalf("ListSuppressedCards: %v", err)
	}
	if len(items) != 1 || items[0].CardID != suppressed.CardIDs[0] {
		t.Fatalf("unexpected suppressed cards: %#v", items)
	}
	restored, err := store.RestoreSuppressedCards(ctx, suppressed.CardIDs)
	if err != nil {
		t.Fatalf("RestoreSuppressedCards: %v", err)
	}
	if restored.Restored != 1 {
		t.Fatalf("unexpected restore result: %#v", restored)
	}
	groups, err = store.ListDuplicateCards(ctx, 10)
	if err != nil {
		t.Fatalf("ListDuplicateCards after restore: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("restored duplicate group should reappear: %#v", groups)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !hasDoctorFinding(doctor, "duplicate_card_claims") {
		t.Fatalf("doctor should report duplicate card claims: %#v", doctor)
	}
}

func TestSQLiteStoreListDuplicateCardsNormalizesChinesePunctuation(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	sources := []Source{
		{ID: "ksrc_zh_dup_1", Kind: SourceKindText, URI: "knowledge://text/zh-1", Title: "中文重复一", ContentHash: "zh-dup-1", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_zh_dup_2", Kind: SourceKindText, URI: "knowledge://text/zh-2", Title: "中文重复二", ContentHash: "zh-dup-2", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatalf("SaveSource: %v", err)
		}
	}

	claims := []string{
		"知识库系统负责本地检索，并提供来源摘要与事实图谱。",
		"知识库系统负责本地检索；并提供来源摘要与事实图谱！",
	}
	for i, source := range sources {
		if err := store.SaveCard(ctx, Card{ID: "kcard_zh_dup_" + source.ID, SourceID: source.ID, Title: source.Title, Claim: claims[i], ProjectPath: source.ProjectPath, Confidence: 0.8, Importance: float64(i + 1), SourceTrust: 0.8}); err != nil {
			t.Fatalf("SaveCard: %v", err)
		}
	}

	groups, err := store.ListDuplicateCards(ctx, 10)
	if err != nil {
		t.Fatalf("ListDuplicateCards: %v", err)
	}
	if len(groups) != 1 || groups[0].Count != 2 || len(groups[0].SourceIDs) != 2 {
		t.Fatalf("Chinese punctuation variants should be grouped as duplicates: %#v", groups)
	}
}

func TestSQLiteStoreSearchTopicRerank(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taxFile := filepath.Join(root, "tax.md")
	hrFile := filepath.Join(root, "hr.md")
	mustWrite(t, taxFile, []byte("Common recall note uses shared engine. TaxAlpha policy is stored here."))
	mustWrite(t, hrFile, []byte("Common recall note uses shared engine. HRBeta policy is stored here."))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if _, err := store.ImportFiles(ctx, DirectoryImportRequest{ProjectPath: "D:/project", SaveScope: SaveScopeProject, TopicHint: "tax", IncludeExts: []string{".md"}, MaxFileBytes: 1024}, []string{taxFile}); err != nil {
		t.Fatalf("ImportFiles tax: %v", err)
	}
	if _, err := store.ImportFiles(ctx, DirectoryImportRequest{ProjectPath: "D:/project", SaveScope: SaveScopeProject, TopicHint: "hr", IncludeExts: []string{".md"}, MaxFileBytes: 1024}, []string{hrFile}); err != nil {
		t.Fatalf("ImportFiles hr: %v", err)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "common recall", ProjectPath: "D:/project", TopicHint: "hr", Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected two rerank candidates: %#v", results)
	}
	if results[0].Source.TopicHint != "hr" {
		t.Fatalf("topic hint did not rerank matching source first: %#v", results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("expected boosted score for topic-matching result: %#v", results)
	}
	relevance, err := store.TopicRelevance(ctx, SearchOptions{ProjectPath: "D:/project", TopicHint: "hr", Limit: 5})
	if err != nil {
		t.Fatalf("TopicRelevance: %v", err)
	}
	if relevance.Count == 0 || !stringSliceContains(relevance.Notes, "local_topic_relevance_no_llm") || !stringSliceContains(relevance.Terms, "hr") {
		t.Fatalf("unexpected topic relevance report: %#v", relevance)
	}
	if relevance.Sources[0].Source.TopicHint != "hr" || !stringSliceContains(relevance.Sources[0].MatchedTerms, "hr") || relevance.Sources[0].Score <= 0 {
		t.Fatalf("expected hr source to be topic-relevant: %#v", relevance.Sources)
	}
	previewLinks, err := store.PreviewSourceTopicLinks(ctx, relevance.Sources[0].Source.ID, 5)
	if err != nil {
		t.Fatalf("PreviewSourceTopicLinks: %v", err)
	}
	if !stringSliceContains(previewLinks.Notes, "local_source_topic_link_preview_no_llm") || previewLinks.Linked != 0 {
		t.Fatalf("unexpected topic link preview: %#v", previewLinks)
	}
	if len(previewLinks.Links) > 0 {
		manualLink, err := store.LinkSources(ctx, SourceLink{
			SourceID:        relevance.Sources[0].Source.ID,
			RelatedSourceID: previewLinks.Links[0].RelatedSourceID,
			Terms:           []string{"manual-test"},
			Evidence:        []string{"unit-test"},
		})
		if err != nil {
			t.Fatalf("LinkSources: %v", err)
		}
		if manualLink.SourceID != relevance.Sources[0].Source.ID || manualLink.RelatedSourceID != previewLinks.Links[0].RelatedSourceID || !stringSliceContains(manualLink.Terms, "manual-test") {
			t.Fatalf("unexpected manual source link: %#v", manualLink)
		}
		unlink, err := store.UnlinkSources(ctx, relevance.Sources[0].Source.ID, previewLinks.Links[0].RelatedSourceID, "")
		if err != nil {
			t.Fatalf("UnlinkSources: %v", err)
		}
		if unlink.Deleted == 0 || !stringSliceContains(unlink.Notes, "local_source_unlink_no_llm") {
			t.Fatalf("unexpected source unlink: %#v", unlink)
		}
		events, err := store.ListSourceLinkEvents(ctx, relevance.Sources[0].Source.ID, 10)
		if err != nil {
			t.Fatalf("ListSourceLinkEvents: %v", err)
		}
		if len(events) < 2 || !hasSourceLinkEventAction(events, "link") || !hasSourceLinkEventAction(events, "unlink") {
			t.Fatalf("unexpected source link events: %#v", events)
		}
		timeline, err := store.SourceTimeline(ctx, relevance.Sources[0].Source.ID, 20)
		if err != nil {
			t.Fatalf("SourceTimeline: %v", err)
		}
		if timeline.Count == 0 || !stringSliceContains(timeline.Notes, "local_source_timeline_no_llm") || !hasSourceTimelineEventKind(timeline.Events, "source_version") || !hasSourceTimelineEventKind(timeline.Events, "source_link_event") {
			t.Fatalf("unexpected source timeline: %#v", timeline)
		}
		digest, err := store.SourceDigest(ctx, relevance.Sources[0].Source.ID, 4, 4, 6, 4)
		if err != nil {
			t.Fatalf("SourceDigest: %v", err)
		}
		if digest.SourceID == "" || digest.Title == "" || len(digest.Cards) == 0 || len(digest.Timeline) == 0 || !stringSliceContains(digest.Notes, "local_source_digest_no_llm") {
			t.Fatalf("unexpected source digest: %#v", digest)
		}
	}
	linkResult, err := store.RefreshSourceTopicLinks(ctx, relevance.Sources[0].Source.ID, 5)
	if err != nil {
		t.Fatalf("RefreshSourceTopicLinks: %v", err)
	}
	if !stringSliceContains(linkResult.Notes, "local_source_topic_links_no_llm") {
		t.Fatalf("unexpected topic link notes: %#v", linkResult)
	}
	links, err := store.ListSourceLinks(ctx, relevance.Sources[0].Source.ID, 10)
	if err != nil {
		t.Fatalf("ListSourceLinks: %v", err)
	}
	if linkResult.Linked > 0 && len(links) == 0 {
		t.Fatalf("expected persisted topic links: %#v", linkResult)
	}
	if linkResult.Linked > 0 {
		stats, err := store.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats after topic links: %v", err)
		}
		if stats.SourceLinks == 0 || stats.SourcesWithoutLinks != 0 {
			t.Fatalf("unexpected source-link stats after refresh: %#v", stats)
		}
		if stats.SourceLinkEvents < 2 || stats.LinkEventsByAction["link"] == 0 || stats.LinkEventsByAction["unlink"] == 0 {
			t.Fatalf("unexpected source-link event stats after manual governance: %#v", stats)
		}
		linkedSources, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "has_links", Limit: 10})
		if err != nil {
			t.Fatalf("ListSources has_links: %v", err)
		}
		if len(linkedSources) == 0 {
			t.Fatalf("expected has_links filter to find sources")
		}
		graph, err := store.SourceGraph(ctx, ListSourcesOptions{ProjectPath: "D:/project", Limit: 10}, 20)
		if err != nil {
			t.Fatalf("SourceGraph: %v", err)
		}
		if graph.Count != 2 || graph.EdgeCount == 0 || len(graph.Nodes) != 2 || len(graph.Edges) == 0 || !stringSliceContains(graph.Notes, "local_source_graph_no_llm") {
			t.Fatalf("unexpected source graph: %#v", graph)
		}
		if graph.ComponentCount != 1 || graph.LargestComponentSize != 2 || len(graph.Components) != 1 || !stringSliceContains(graph.Notes, "local_connected_components") {
			t.Fatalf("expected connected source graph component summary: %#v", graph)
		}
		if graph.Components[0].Count != 2 || graph.Components[0].EdgeCount == 0 || graph.Components[0].AverageDegree <= 0 || graph.Components[0].Density <= 0 {
			t.Fatalf("unexpected source graph component metrics: %#v", graph.Components)
		}
		if graph.Nodes[0].Degree == 0 {
			t.Fatalf("expected graph nodes to include degree: %#v", graph.Nodes)
		}
		if graph.Nodes[0].ComponentID == 0 {
			t.Fatalf("expected graph nodes to include component id: %#v", graph.Nodes)
		}
		neighborhood, err := store.SourceNeighborhood(ctx, relevance.Sources[0].Source.ID, 2, 20, 50)
		if err != nil {
			t.Fatalf("SourceNeighborhood: %v", err)
		}
		if neighborhood.FocusSourceID != relevance.Sources[0].Source.ID || neighborhood.Depth != 2 || neighborhood.Count != 2 || !stringSliceContains(neighborhood.Notes, "local_source_neighborhood_no_llm") {
			t.Fatalf("unexpected source neighborhood: %#v", neighborhood)
		}
		path, err := store.SourcePath(ctx, relevance.Sources[0].Source.ID, links[0].RelatedSourceID, 4, 50)
		if err != nil {
			t.Fatalf("SourcePath: %v", err)
		}
		if !path.Found || path.HopCount != 1 || len(path.Nodes) != 2 || len(path.Steps) != 1 || path.VisitedCount == 0 || path.SearchedEdgeCount == 0 || !stringSliceContains(path.Notes, "local_source_path_no_llm") {
			t.Fatalf("unexpected source path: %#v", path)
		}
	}
}

func TestSQLiteStoreSaveURLSource(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	source, err := BuildSourceFromURL("example.com/article", "", "", "D:/project", "topic")
	if err != nil {
		t.Fatalf("BuildSourceFromURL: %v", err)
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	sources, err := store.ListSources(ctx, ListSourcesOptions{ProjectPath: "D:/project"})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources len=%d err=%v", len(sources), err)
	}
}

func TestFindExistingURLSourceForSave(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	existing := Source{
		ID:           "ksrc_url_existing",
		Kind:         SourceKindURL,
		URI:          "https://example.com/articles/a",
		CanonicalURI: "https://example.com/articles/a",
		Title:        "Existing URL",
		ContentHash:  "hash-existing",
		ProjectPath:  "D:/project",
		TopicHint:    "contracts",
		SourceTrust:  0.7,
		Status:       StatusDistilled,
		FetchedAt:    now,
		CreatedAt:    now.Add(-time.Hour),
		UpdatedAt:    now,
	}
	if err := store.SaveSource(ctx, existing); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	matchByCanonical := Source{
		Kind:         SourceKindURL,
		URI:          "https://example.com/redirected",
		CanonicalURI: existing.CanonicalURI,
		ProjectPath:  existing.ProjectPath,
	}
	got, ok, err := findExistingURLSourceForSave(ctx, tx, matchByCanonical)
	if err != nil {
		t.Fatalf("findExistingURLSourceForSave: %v", err)
	}
	if !ok || got.ID != existing.ID || got.CreatedAt.IsZero() {
		t.Fatalf("expected existing source by canonical URL, ok=%v got=%#v", ok, got)
	}

	otherProject := matchByCanonical
	otherProject.ProjectPath = "D:/other"
	if got, ok, err := findExistingURLSourceForSave(ctx, tx, otherProject); err != nil || ok {
		t.Fatalf("expected project scope to isolate URL sources, ok=%v got=%#v err=%v", ok, got, err)
	}
}

func TestDoctorReportsSourceCoverageGaps(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:           "ksrc_gap",
		Kind:         SourceKindMarkdown,
		URI:          "gap.md",
		Title:        "Coverage gap",
		ContentHash:  "gap-hash",
		ProjectPath:  "D:/project",
		SourceTrust:  0.8,
		Status:       StatusDistilled,
		FetchedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
		RelativePath: "gap.md",
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SourcesWithoutNodes != 1 || stats.SourcesWithoutCards != 1 || stats.SourcesWithoutFacts != 1 {
		t.Fatalf("unexpected coverage stats: %#v", stats)
	}
	if stats.SourcesRebuildCards != 0 || stats.SourcesRebuildFacts != 0 {
		t.Fatalf("missing-node source should not count as rebuildable derived gap: %#v", stats)
	}
	if stats.SourcesWithoutLinks != 1 {
		t.Fatalf("unexpected source link coverage stats: %#v", stats)
	}
	missingNodes, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_nodes", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_nodes: %v", err)
	}
	if len(missingNodes) != 1 || missingNodes[0].ID != source.ID || missingNodes[0].NodeCount != 0 {
		t.Fatalf("unexpected missing_nodes sources: %#v", missingNodes)
	}
	missingCards, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_cards", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_cards: %v", err)
	}
	if len(missingCards) != 0 {
		t.Fatalf("missing_cards should only include sources with parsed nodes: %#v", missingCards)
	}
	missingFacts, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_facts", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_facts: %v", err)
	}
	if len(missingFacts) != 0 {
		t.Fatalf("missing_facts should only include sources with parsed nodes: %#v", missingFacts)
	}
	missingLinks, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_links", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_links: %v", err)
	}
	if len(missingLinks) != 1 || missingLinks[0].ID != source.ID {
		t.Fatalf("unexpected missing_links sources: %#v", missingLinks)
	}
	complete, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "complete", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources complete: %v", err)
	}
	if len(complete) != 0 {
		t.Fatalf("coverage complete should not include gap source: %#v", complete)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, code := range []string{"sources_without_nodes"} {
		finding, ok := doctorFinding(doctor, code)
		if !ok {
			t.Fatalf("missing doctor finding %s in %#v", code, doctor)
		}
		if len(finding.SourceIDs) == 0 || finding.SourceIDs[0] != source.ID || len(finding.Examples) == 0 {
			t.Fatalf("doctor finding %s should include source refs: %#v", code, finding)
		}
	}
	if _, ok := doctorFinding(doctor, "sources_without_cards"); ok {
		t.Fatalf("missing-node source should not also get derived card rebuild finding: %#v", doctor.Findings)
	}
	if _, ok := doctorFinding(doctor, "sources_without_facts"); ok {
		t.Fatalf("missing-node source should not also get derived fact rebuild finding: %#v", doctor.Findings)
	}
}

func TestDoctorReportsDerivedCoverageGapsOnlyWhenNodesExist(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:          "ksrc_derived_gap",
		Kind:        SourceKindText,
		URI:         "knowledge://text/derived-gap",
		Title:       "Derived gap",
		ContentHash: "derived-gap",
		ProjectPath: "D:/project",
		SourceTrust: 0.8,
		Status:      StatusDistilled,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:         "kdn_derived_gap",
		SourceID:   source.ID,
		Type:       "document",
		Title:      "Derived gap",
		Text:       "Knowledge doctor can rebuild cards and facts when parsed nodes exist.",
		TokenCount: 80,
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SourcesWithoutCards != 1 || stats.SourcesWithoutFacts != 1 || stats.SourcesRebuildCards != 1 || stats.SourcesRebuildFacts != 1 {
		t.Fatalf("parsed derived gap should count as rebuildable: %#v", stats)
	}

	missingCards, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_cards", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_cards: %v", err)
	}
	if len(missingCards) != 1 || missingCards[0].ID != source.ID {
		t.Fatalf("missing_cards should include parsed sources missing cards: %#v", missingCards)
	}
	missingFacts, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "missing_facts", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources missing_facts: %v", err)
	}
	if len(missingFacts) != 1 || missingFacts[0].ID != source.ID {
		t.Fatalf("missing_facts should include parsed sources missing facts: %#v", missingFacts)
	}
	rebuildableCards, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "rebuild_cards", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources rebuild_cards: %v", err)
	}
	if !sameSourceIDs(rebuildableCards, missingCards) {
		t.Fatalf("rebuild_cards alias should match missing_cards: %#v vs %#v", rebuildableCards, missingCards)
	}
	rebuildableFacts, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "facts_rebuildable", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources facts_rebuildable: %v", err)
	}
	if !sameSourceIDs(rebuildableFacts, missingFacts) {
		t.Fatalf("facts_rebuildable alias should match missing_facts: %#v vs %#v", rebuildableFacts, missingFacts)
	}

	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, code := range []string{"sources_without_cards", "sources_without_facts"} {
		finding, ok := doctorFinding(doctor, code)
		if !ok {
			t.Fatalf("missing doctor finding %s in %#v", code, doctor)
		}
		if finding.Count != 1 || len(finding.SourceIDs) == 0 || finding.SourceIDs[0] != source.ID {
			t.Fatalf("doctor finding %s should point at derived-gap source: %#v", code, finding)
		}
		if !strings.Contains(finding.Action, "Rebuild cards and facts") {
			t.Fatalf("derived gap finding %s should recommend local rebuild: %#v", code, finding)
		}
	}
}

func TestDoctorNoFactsRecommendsLocalRebuild(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:          "ksrc_no_facts",
		Kind:        SourceKindText,
		URI:         "knowledge://text/no-facts",
		Title:       "No facts source",
		ContentHash: "no-facts-hash",
		ProjectPath: "D:/project",
		SourceTrust: 0.8,
		Status:      StatusDistilled,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:         "kdn_no_facts",
		SourceID:   source.ID,
		Type:       "document",
		Title:      "No facts source",
		Text:       "Knowledge maintenance can rebuild relation facts from parsed nodes.",
		TokenCount: 80,
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:          "kcard_no_facts",
		SourceID:    source.ID,
		Title:       "No facts card",
		Claim:       "Knowledge maintenance can rebuild derived cards and facts from parsed nodes.",
		Summary:     "This card intentionally has no structured facts.",
		ProjectPath: source.ProjectPath,
		Confidence:  0.8,
		Importance:  0.8,
		SourceTrust: 0.8,
	}); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}

	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	finding, ok := doctorFinding(doctor, "no_facts")
	if !ok {
		t.Fatalf("missing no_facts finding: %#v", doctor)
	}
	if !strings.Contains(finding.Action, "Rebuild cards and facts") {
		t.Fatalf("no_facts should recommend local rebuild: %#v", finding)
	}
}

func TestDoctorReportsSourceGraphGaps(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	sources := []Source{
		{ID: "ksrc_graph_alpha", Kind: SourceKindText, URI: "knowledge://text/graph-alpha", Title: "Graph Alpha", ContentHash: "graph-alpha", ProjectPath: "D:/project", Status: StatusDistilled, SourceTrust: 0.8},
		{ID: "ksrc_graph_beta", Kind: SourceKindText, URI: "knowledge://text/graph-beta", Title: "Graph Beta", ContentHash: "graph-beta", ProjectPath: "D:/project", Status: StatusDistilled, SourceTrust: 0.8},
		{ID: "ksrc_graph_isolate", Kind: SourceKindText, URI: "knowledge://text/graph-isolate", Title: "Graph Isolate", ContentHash: "graph-isolate", ProjectPath: "D:/project", Status: StatusDistilled, SourceTrust: 0.8},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatalf("SaveSource %s: %v", source.ID, err)
		}
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := insertSourceLinkTx(ctx, tx, SourceLink{SourceID: sources[0].ID, RelatedSourceID: sources[1].ID, Relation: SourceRelationTopicRelated, Score: 0.9, Terms: []string{"graph"}}); err != nil {
		t.Fatalf("insert link alpha beta: %v", err)
	}
	if err := insertSourceLinkTx(ctx, tx, SourceLink{SourceID: sources[1].ID, RelatedSourceID: sources[0].ID, Relation: SourceRelationTopicRelated, Score: 0.9, Terms: []string{"graph"}}); err != nil {
		t.Fatalf("insert link beta alpha: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	fragmented, ok := doctorFinding(doctor, "source_graph_fragmented")
	if !ok || fragmented.Count < 2 || len(fragmented.SourceIDs) == 0 {
		t.Fatalf("expected source_graph_fragmented finding: %#v", doctor.Findings)
	}
	isolates, ok := doctorFinding(doctor, "source_graph_isolates")
	if !ok || isolates.Count != 1 || len(isolates.SourceIDs) == 0 || isolates.SourceIDs[0] != sources[2].ID {
		t.Fatalf("expected source_graph_isolates finding: %#v", doctor.Findings)
	}
}

func TestDoctorReportsPDFOCRNeeded(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	pdfPath := filepath.Join(root, "scanned.pdf")
	mustWrite(t, pdfPath, []byte("%PDF-1.4\n% image-only placeholder\n"))
	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID:           "ksrc_pdf_ocr",
		Kind:         SourceKindPDF,
		URI:          pdfPath,
		Title:        "Scanned PDF",
		ContentHash:  "pdf-ocr-hash",
		Status:       StatusParsed,
		FetchedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
		RelativePath: "scanned.pdf",
	}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	finding, ok := doctorFinding(doctor, "pdf_ocr_needed")
	if !ok {
		t.Fatalf("expected pdf_ocr_needed finding: %#v", doctor.Findings)
	}
	if finding.Count != 1 || len(finding.SourceIDs) == 0 || finding.SourceIDs[0] != source.ID {
		t.Fatalf("unexpected pdf_ocr_needed finding: %#v", finding)
	}
	ocrSources, err := store.ListSources(ctx, ListSourcesOptions{CoverageFilter: "pdf_ocr_needed", Limit: 10})
	if err != nil {
		t.Fatalf("ListSources pdf_ocr_needed: %v", err)
	}
	if len(ocrSources) != 1 || ocrSources[0].ID != source.ID {
		t.Fatalf("unexpected pdf_ocr_needed sources: %#v", ocrSources)
	}
}

func TestDoctorReportsLocalFileDrift(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	changedPath := filepath.Join(root, "changed.md")
	missingPath := filepath.Join(root, "missing.md")
	mustWrite(t, changedPath, []byte("Changed file uses original knowledge."))
	mustWrite(t, missingPath, []byte("Missing file uses original knowledge."))

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := store.ImportFiles(ctx, DirectoryImportRequest{ProjectPath: "D:/project", SaveScope: SaveScopeProject, IncludeExts: []string{".md"}, MaxFileBytes: 1024}, []string{changedPath, missingPath}); err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	mustWrite(t, changedPath, []byte("Changed file uses updated knowledge."))
	if err := os.Remove(missingPath); err != nil {
		t.Fatalf("Remove missingPath: %v", err)
	}

	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, code := range []string{"changed_local_files", "missing_local_files"} {
		if !hasDoctorFinding(doctor, code) {
			t.Fatalf("missing doctor finding %s in %#v", code, doctor)
		}
	}
	if finding, ok := doctorFinding(doctor, "changed_local_files"); !ok || len(finding.SourceIDs) == 0 || len(finding.Examples) == 0 {
		t.Fatalf("changed file finding should include source ids and examples: %#v", finding)
	}
	if finding, ok := doctorFinding(doctor, "missing_local_files"); !ok || len(finding.SourceIDs) == 0 || len(finding.Examples) == 0 {
		t.Fatalf("missing file finding should include source ids and examples: %#v", finding)
	}
	changedFinding, _ := doctorFinding(doctor, "changed_local_files")
	refreshResult := store.RefreshSources(ctx, changedFinding.SourceIDs)
	if refreshResult.Refreshed != 1 || refreshResult.Failed != 0 {
		t.Fatalf("unexpected RefreshSources result: %#v", refreshResult)
	}
	doctor, err = store.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor after refresh: %v", err)
	}
	if hasDoctorFinding(doctor, "changed_local_files") {
		t.Fatalf("changed file finding should clear after refresh: %#v", doctor)
	}
}

func hasDoctorFinding(result DoctorResult, code string) bool {
	_, ok := doctorFinding(result, code)
	return ok
}

func doctorFinding(result DoctorResult, code string) (DoctorFinding, bool) {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return finding, true
		}
	}
	return DoctorFinding{}, false
}

func hasSourceLinkEventAction(events []SourceLinkEvent, action string) bool {
	for _, event := range events {
		if event.Action == action {
			return true
		}
	}
	return false
}

func hasSourceTimelineEventKind(events []SourceTimelineEvent, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func containsStoredFact(facts []Fact, predicate, object string) bool {
	for _, fact := range facts {
		if fact.Predicate == predicate && fact.Object == object {
			return true
		}
	}
	return false
}

func hasSearchResultType(results []SearchResult, resultType string) bool {
	for _, result := range results {
		if result.ResultType == resultType {
			return true
		}
	}
	return false
}

func sameSourceIDs(left, right []Source) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, source := range left {
		counts[source.ID]++
	}
	for _, source := range right {
		counts[source.ID]--
		if counts[source.ID] < 0 {
			return false
		}
	}
	return true
}

func sourceQualityItemByID(report SourceQualityReport, sourceID string) (SourceQualityItem, bool) {
	for _, item := range report.Items {
		if item.Source.ID == sourceID {
			return item, true
		}
	}
	return SourceQualityItem{}, false
}

func sourceQualityActionByKind(plan SourceQualityMaintenancePlan, kind string) (SourceQualityMaintenanceAction, bool) {
	for _, action := range plan.Actions {
		if action.Kind == kind {
			return action, true
		}
	}
	return SourceQualityMaintenanceAction{}, false
}

func assertSourceQualityActionOrder(t *testing.T, plan SourceQualityMaintenancePlan, before, after string) {
	t.Helper()
	beforeIndex := -1
	afterIndex := -1
	for i, action := range plan.Actions {
		switch action.Kind {
		case before:
			beforeIndex = i
		case after:
			afterIndex = i
		}
	}
	if beforeIndex == -1 || afterIndex == -1 {
		t.Fatalf("expected maintenance actions %q and %q in plan: %#v", before, after, plan.Actions)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("expected maintenance action %q before %q: %#v", before, after, plan.Actions)
	}
}
