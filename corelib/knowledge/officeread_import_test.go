package knowledge

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestOfficeReadStructuredKnowledgeContentRequiresExplicitOptIn(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "preview.docx")
	writeOfficeReadDOCX(t, path, "# Structured title\n\nKnowledge preview body", nil)
	source := Source{ID: "source-preview", Kind: SourceKindDOCX, Title: "Preview", RelativePath: "preview.docx"}
	if _, enabled, err := ParseOfficeReadRichContentForKnowledgeFile(source, path); err != nil || enabled {
		t.Fatalf("rich content must stay disabled by default: enabled=%t err=%v", enabled, err)
	}

	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()
	nodes, active, err := ParseOfficeReadRichContentForKnowledgeFile(source, path)
	if err != nil || !active || len(nodes) == 0 {
		t.Fatalf("rich content = nodes=%#v active=%t err=%v", nodes, active, err)
	}
	if !strings.Contains(strings.Join(nodeTexts(nodes), "\n"), "Knowledge preview body") || nodes[0].Metadata["extractor"] != "officeread_structured_markdown" {
		t.Fatalf("structured Markdown nodes not preserved: %#v", nodes)
	}
}

func TestOfficeReadStructuredKnowledgeContentDoesNotActivateDuringDualSampling(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "dual-preview.docx")
	writeOfficeReadDOCX(t, path, "Dual shadow markdown must stay hidden", nil)
	source := Source{ID: "source-dual-preview", Kind: SourceKindDOCX, Title: "Dual preview", RelativePath: "dual-preview.docx"}
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "dual", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	if nodes, active, err := ParseOfficeReadRichContentForKnowledgeFile(source, path); err != nil || active || len(nodes) != 0 {
		t.Fatalf("dual rich preview = nodes=%#v active=%t err=%v", nodes, active, err)
	}
	nodes, err := ParseDocumentNodes(source, path, SourceKindDOCX)
	if err != nil || len(nodes) == 0 || nodes[0].Metadata["extractor"] == "officeread_structured_markdown" {
		t.Fatalf("dual knowledge import must retain legacy nodes: nodes=%#v err=%v", nodes, err)
	}
}

func TestOfficeReadKnowledgeImportUsesRequestScopedPolicy(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "request-scoped.docx")
	writeOfficeReadDOCX(t, path, "Request-scoped rich OfficeRead body", nil)

	// Keep the process-wide desktop provider disabled. Each import below must
	// honor only the policy carried by its own trusted request instead.
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	emitMarkdown := true
	richPolicy := &agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &emitMarkdown}
	richResult, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, OwnerID: "rich-user", TenantID: "tenant", IncludeExts: []string{".docx"}, DistillMode: DistillModeRules, OfficeReadConfig: richPolicy,
	}, []string{path})
	if err != nil || richResult.ImportedFiles != 1 || len(richResult.Items) != 1 {
		t.Fatalf("rich scoped import = %#v, %v", richResult, err)
	}
	richNodes, err := store.ListNodesBySource(t.Context(), richResult.Items[0].SourceID, 20)
	if err != nil || len(richNodes) == 0 || richNodes[0].Metadata["extractor"] != "officeread_structured_markdown" {
		t.Fatalf("rich scoped nodes = %#v, %v", richNodes, err)
	}

	legacyPolicy := &agent.OfficeReadConfig{Engine: "legacy"}
	legacyResult, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, OwnerID: "legacy-user", TenantID: "tenant", IncludeExts: []string{".docx"}, DistillMode: DistillModeRules, OfficeReadConfig: legacyPolicy,
	}, []string{path})
	if err != nil || legacyResult.ImportedFiles != 1 || len(legacyResult.Items) != 1 {
		t.Fatalf("legacy scoped import = %#v, %v", legacyResult, err)
	}
	legacyNodes, err := store.ListNodesBySource(t.Context(), legacyResult.Items[0].SourceID, 20)
	if err != nil || len(legacyNodes) == 0 || legacyNodes[0].Metadata["extractor"] == "officeread_structured_markdown" {
		t.Fatalf("legacy scoped nodes = %#v, %v", legacyNodes, err)
	}
}

func TestOfficeReadKnowledgeRefreshUsesRequestScopedPolicy(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "refresh-request-scoped.docx")
	writeOfficeReadDOCX(t, path, "Refresh request-scoped OfficeRead body", nil)

	// The desktop provider is deliberately incompatible with the first refresh.
	// A multi-tenant refresh must use only the policy received with that request.
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(t.Context(), Source{ID: "refresh-scoped", Kind: SourceKindDOCX, URI: path, Title: "Refresh scoped", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}

	emitMarkdown := true
	richPolicy := &agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &emitMarkdown}
	preview, err := store.PreviewSourceRefreshWithOfficeReadConfig(t.Context(), "refresh-scoped", richPolicy)
	if err != nil || preview.Error != "" || preview.NewNodeCount == 0 || preview.NextSource.Status == StatusFailed {
		t.Fatalf("rich scoped preview = %#v, %v", preview, err)
	}
	if _, err := store.RefreshSourceWithOfficeReadConfig(t.Context(), "refresh-scoped", richPolicy); err != nil {
		t.Fatalf("rich scoped refresh: %v", err)
	}
	richNodes, err := store.ListNodesBySource(t.Context(), "refresh-scoped", 20)
	if err != nil || len(richNodes) == 0 || richNodes[0].Metadata["extractor"] != "officeread_structured_markdown" {
		t.Fatalf("rich scoped refresh nodes = %#v, %v", richNodes, err)
	}

	legacyPolicy := &agent.OfficeReadConfig{Engine: "legacy"}
	if _, err := store.RefreshSourceWithOfficeReadConfig(t.Context(), "refresh-scoped", legacyPolicy); err != nil {
		t.Fatalf("legacy scoped refresh: %v", err)
	}
	legacyNodes, err := store.ListNodesBySource(t.Context(), "refresh-scoped", 20)
	if err != nil || len(legacyNodes) == 0 || legacyNodes[0].Metadata["extractor"] == "officeread_structured_markdown" {
		t.Fatalf("legacy scoped refresh nodes = %#v, %v", legacyNodes, err)
	}
}

func TestOfficeReadKnowledgeImportCopiesRequestScopedPolicy(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "policy-copy.docx")
	writeOfficeReadDOCX(t, path, "Policy copy body", nil)

	emitMarkdown := true
	policy := &agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &emitMarkdown}
	extraction := &officeReadRichExtraction{config: policy}
	// The extraction boundary copies this before any parse path retains it.
	// Mutating the host-owned policy after construction cannot change what the
	// actual parse sees, including pointer-backed rollout booleans.
	policySnapshot := agent.CloneOfficeReadConfigPtr(policy)
	policy.Formats[0] = "pptx"
	emitMarkdown = false
	if policySnapshot == nil || policySnapshot.EmitMarkdown == nil || !*policySnapshot.EmitMarkdown || len(policySnapshot.Formats) != 1 || policySnapshot.Formats[0] != "docx" {
		t.Fatalf("request policy snapshot = %#v", policySnapshot)
	}
	// Cover the importer-specific copy as well: it must resolve the original
	// policy passed at call time, not consult the global provider.
	extraction.config = policySnapshot
	nodes, err := parseOfficeReadNodesFromExtraction(Source{ID: "policy-copy", Kind: SourceKindDOCX, Title: "Policy copy"}, path, SourceKindDOCX, extraction)
	if err != nil || len(nodes) == 0 || nodes[0].Metadata["extractor"] != "officeread_structured_markdown" {
		t.Fatalf("copied request policy nodes = %#v, %v", nodes, err)
	}
}

func TestOfficeReadKnowledgeImagesDoNotActivateDuringDualSampling(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "dual-images.docx")
	imageBytes := officeReadTestPNG(t)
	writeOfficeReadDOCX(t, path, "Dual image source", map[string][]byte{"word/media/image1.png": imageBytes}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "dual", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "source-dual-image", Kind: SourceKindDOCX, Title: "Dual image", RelativePath: "dual-images.docx"}
	textNodes, err := ParseDocumentNodes(source, path, SourceKindDOCX)
	if err != nil || len(textNodes) == 0 || textNodes[0].Metadata["extractor"] == "officeread_structured_markdown" {
		t.Fatalf("dual knowledge nodes = %#v err=%v", textNodes, err)
	}
	// This helper is the direct OfficeRead-image consumer retained for callers
	// outside the import pipeline. During dual sampling it must not materialize
	// the shadow extractor's image payload into an asset.
	if imageNodes := officeReadImagesForImport(t.Context(), store, source, path, SourceKindDOCX, textNodes); len(imageNodes) != 0 {
		t.Fatalf("dual sampling persisted OfficeRead image nodes: %#v", imageNodes)
	}
	if entries, err := os.ReadDir(assets.AssetDir(source.ID)); err == nil && len(entries) != 0 {
		t.Fatalf("dual sampling persisted unexpected OfficeRead assets: %#v", entries)
	}
}

func TestOfficeReadStructuredKnowledgeContentRejectsHeadingStorm(t *testing.T) {
	// The adapter's Markdown rune cap permits a document made entirely of small
	// headings. Verify that the knowledge consumer also bounds the resulting
	// node fan-out before FTS/card work or persistence begins.
	var markdown strings.Builder
	for i := 0; i <= maxKnowledgeOfficeReadMarkdownNodes; i++ {
		markdown.WriteString("# h\n")
	}
	extraction := &officeReadRichExtraction{
		loaded:  true,
		enabled: true,
		content: agent.OfficeReadRichContent{Format: SourceKindDOCX, Markdown: markdown.String()},
	}
	nodes, err := parseOfficeReadNodesFromExtraction(Source{ID: "heading-storm", Title: "Heading storm"}, "ignored.docx", SourceKindDOCX, extraction)
	if !errors.Is(err, agent.ErrOfficeReadOutputTooLarge) {
		t.Fatalf("heading storm error = %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("heading storm returned nodes: %d", len(nodes))
	}
}

func TestOfficeReadStructuredKnowledgePreviewSanitizesFilesystemError(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "customer-secret-plan.docx")
	_, active, err := ParseOfficeReadRichContentForKnowledgeFile(Source{}, path)
	if active || !errors.Is(err, agent.ErrOfficeReadExtractionFailed) {
		t.Fatalf("missing rich preview = active=%t err=%v", active, err)
	}
	if strings.Contains(err.Error(), "customer-secret-plan") || strings.Contains(err.Error(), path) {
		t.Fatalf("preview error leaked source path: %q", err)
	}
}

func TestOfficeReadStructuredKnowledgePreviewRejectsMissingExtensionWithoutDetail(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "customer-secret-plan")
	_, active, err := ParseOfficeReadRichContentForKnowledgeFile(Source{}, path)
	if active || !errors.Is(err, agent.ErrOfficeReadExtractionFailed) {
		t.Fatalf("extensionless rich preview = active=%t err=%v", active, err)
	}
	if strings.Contains(err.Error(), "extension") {
		t.Fatalf("preview error revealed implementation detail: %q", err)
	}
}

func TestSanitizeOfficeReadKnowledgeErrorNeverPersistsParserDetail(t *testing.T) {
	const sensitiveDetail = `OfficeRead parser failed at C:\\Users\\private\\customer-plan.docx`
	if got := sanitizeOfficeReadKnowledgeError(errors.New(sensitiveDetail)); got == nil || strings.Contains(got.Error(), "customer-plan") || !errors.Is(got, agent.ErrOfficeReadExtractionFailed) {
		t.Fatalf("parser error sanitization = %v", got)
	}
	if got := sanitizeOfficeReadKnowledgeError(agent.ErrOfficeReadEncryptedContainer); !errors.Is(got, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("stable encrypted error identity was lost: %v", got)
	}
	if got := sanitizeOfficeReadKnowledgeError(agent.ErrOfficeReadInputTooLarge); !errors.Is(got, agent.ErrOfficeReadInputTooLarge) {
		t.Fatalf("stable input-limit error identity was lost: %v", got)
	}
	if got := sanitizeOfficeReadKnowledgeError(agent.ErrOfficeReadTimedOut); !errors.Is(got, agent.ErrOfficeReadTimedOut) {
		t.Fatalf("stable timeout error identity was lost: %v", got)
	}
	if got := sanitizeOfficeReadKnowledgeError(nil); got != nil {
		t.Fatalf("nil error should remain nil: %v", got)
	}
}

func TestSanitizeKnowledgeParseErrorOnlyRedactsOfficeAndCSV(t *testing.T) {
	const sensitiveDetail = `parser opened C:\\Users\\private\\customer-plan.docx with body: confidential roadmap`
	for _, kind := range []string{SourceKindDOC, SourceKindDOCX, SourceKindXLS, SourceKindXLSX, SourceKindPPT, SourceKindPPTX, SourceKindCSV} {
		got := sanitizeKnowledgeParseError(kind, errors.New(sensitiveDetail))
		if !errors.Is(got, agent.ErrOfficeReadExtractionFailed) || strings.Contains(got.Error(), "customer-plan") || strings.Contains(got.Error(), "confidential") {
			t.Fatalf("kind %s was not safely redacted: %v", kind, got)
		}
	}
	if got := sanitizeKnowledgeParseError(SourceKindPDF, errors.New(sensitiveDetail)); got == nil || got.Error() != sensitiveDetail {
		t.Fatalf("non-Office parser diagnostic changed: %v", got)
	}
}

func TestOfficeReadKnowledgeImportPersistsStableEncryptedError(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "customer-secret-plan.docx")
	writeOfficeReadEncryptedDOCX(t, path)

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".docx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.FailedFiles != 1 || len(result.FailedItems) != 1 {
		t.Fatalf("encrypted Office import = %#v, %v", result, err)
	}
	if got := result.FailedItems[0].Error; got != agent.ErrOfficeReadEncryptedContainer.Error() || strings.Contains(got, "customer-secret") {
		t.Fatalf("failed import item leaked parse detail: %q", got)
	}
	items, err := store.ListImportItems(t.Context(), result.BatchID, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListImportItems = %#v, %v", items, err)
	}
	if got := items[0].ErrorMessage; got != agent.ErrOfficeReadEncryptedContainer.Error() || strings.Contains(got, "customer-secret") {
		t.Fatalf("persisted import item leaked parse detail: %q", got)
	}
	source, err := store.GetSource(t.Context(), items[0].SourceID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if source.Status != StatusFailed || source.ErrorMessage != agent.ErrOfficeReadEncryptedContainer.Error() || strings.Contains(source.ErrorMessage, "customer-secret") {
		t.Fatalf("persisted Office source error = %#v", source)
	}
}

func TestOfficeReadKnowledgeRefreshPersistsStableEncryptedError(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "customer-secret-refresh.docx")
	writeOfficeReadEncryptedDOCX(t, path)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(t.Context(), Source{ID: "encrypted-refresh", Kind: SourceKindDOCX, URI: path, Title: "Encrypted", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshSource(t.Context(), "encrypted-refresh")
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if refreshed.Status != StatusFailed || refreshed.ErrorMessage != agent.ErrOfficeReadEncryptedContainer.Error() || strings.Contains(refreshed.ErrorMessage, "customer-secret") {
		t.Fatalf("refresh result leaked parse detail: %#v", refreshed)
	}
	persisted, err := store.GetSource(t.Context(), "encrypted-refresh")
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if persisted.Status != StatusFailed || persisted.ErrorMessage != agent.ErrOfficeReadEncryptedContainer.Error() || strings.Contains(persisted.ErrorMessage, "customer-secret") {
		t.Fatalf("persisted refresh source leaked parse detail: %#v", persisted)
	}
}

func TestKnowledgeCSVImportKeepsSnapshotForStructuredIndex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "customer-secret-grid.csv")
	mustWrite(t, path, []byte("name,plan\nAlice,confidential roadmap\n"))
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	removed := false
	store.SetImportProgressCallback(func(progress DirectoryImportResult) {
		if !removed && progress.CurrentStep == "indexing" && progress.CurrentFile == "customer-secret-grid.csv" {
			removed = true
			if err := os.Remove(path); err != nil {
				t.Errorf("remove source before spreadsheet index: %v", err)
			}
		}
	})
	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".csv"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || !removed || result.ImportedFiles != 1 || result.FailedFiles != 0 {
		t.Fatalf("CSV snapshot import = %#v, removed=%t err=%v", result, removed, err)
	}
	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_rows", 1)
	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&rowText); err != nil {
		t.Fatalf("read CSV snapshot row: %v", err)
	}
	if !strings.Contains(rowText, "confidential roadmap") {
		t.Fatalf("CSV structured index did not use its private snapshot: %q", rowText)
	}
	items, err := store.ListImportItems(t.Context(), result.BatchID, 10)
	if err != nil || len(items) != 1 || items[0].Status != ItemStatusImported {
		t.Fatalf("ListImportItems = %#v, %v", items, err)
	}
	source, err := store.GetSource(t.Context(), items[0].SourceID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if source.Status != StatusParsed && source.Status != StatusDistilled || source.ContentHash == "" || source.ErrorMessage != "" {
		t.Fatalf("persisted CSV snapshot source = %#v", source)
	}
}

func TestKnowledgeCSVRefreshRebuildsStructuredIndexFromSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "refresh-snapshot.csv")
	mustWrite(t, path, []byte("name,plan\nAlice,initial CSV snapshot\n"))
	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	existing := Source{ID: "refresh-csv-snapshot", Kind: SourceKindCSV, URI: path, Title: "refresh CSV snapshot", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), existing); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshSource(t.Context(), existing.ID)
	if err != nil {
		t.Fatalf("initial CSV RefreshSource: %v", err)
	}
	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_rows", 1)

	mustWrite(t, path, []byte("name,plan\nAlice,replacement CSV snapshot\n"))
	next, nodes, _, _, distill, input, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(refreshed)
	if err != nil || input == nil || len(nodes) == 0 {
		if input != nil {
			input.close()
		}
		t.Fatalf("CSV refresh snapshot parse = source=%#v nodes=%#v input=%#v err=%v", next, nodes, input, err)
	}
	snapshotPath := input.path
	defer input.close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	updated, err := store.replaceSourceDerivedRowsWithSpreadsheet(t.Context(), next, nodes, distill, "refresh", snapshotPath)
	if err != nil {
		t.Fatalf("replace refreshed CSV snapshot: %v", err)
	}
	if updated.ContentHash != next.ContentHash {
		t.Fatalf("CSV refresh hash = %q, want snapshot hash %q", updated.ContentHash, next.ContentHash)
	}
	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&rowText); err != nil {
		t.Fatalf("read refreshed CSV row: %v", err)
	}
	if !strings.Contains(rowText, "replacement CSV snapshot") || strings.Contains(rowText, "initial CSV snapshot") {
		t.Fatalf("CSV structured refresh did not use replacement snapshot: %q", rowText)
	}
	input.close()
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("refresh-owned CSV snapshot not cleaned up: %v", err)
	}
}

func TestOfficeReadKnowledgeXLSXImportKeepsSnapshotForStructuredIndex(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "snapshot-index.xlsx")
	writeOfficeReadXLSX(t, path, "name", "plan", "Alice", "verified spreadsheet snapshot")
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open XLSX test package: %v", err)
	}
	entries := make([]string, 0, len(archive.File))
	for _, entry := range archive.File {
		entries = append(entries, entry.Name)
	}
	_ = archive.Close()
	if err := agent.PreflightOfficeReadInput(path, SourceKindXLSX); err != nil {
		t.Fatalf("generated XLSX preflight = %v; entries=%v", err, entries)
	}
	// Use the legacy node path so both text parsing and the structured table
	// importer reopen the import-owned pathname. Removing the live file at the
	// indexing boundary proves the row importer still consumes that snapshot.
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()
	if nodes, err := ParseDocumentNodes(Source{ID: "snapshot-index", Kind: SourceKindXLSX, Title: "snapshot index"}, path, SourceKindXLSX); err != nil || len(nodes) == 0 {
		t.Fatalf("generated XLSX document parse = nodes=%#v err=%v", nodes, err)
	}

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	removed := false
	store.SetImportProgressCallback(func(progress DirectoryImportResult) {
		if removed || progress.CurrentStep != "indexing" || progress.CurrentFile != "snapshot-index.xlsx" {
			return
		}
		removed = true
		if err := os.Remove(path); err != nil {
			t.Errorf("remove live workbook after snapshot parse: %v", err)
		}
	})
	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".xlsx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || !removed || result.ImportedFiles != 1 || result.FailedFiles != 0 {
		t.Fatalf("snapshot XLSX import = %#v, removed=%t err=%v", result, removed, err)
	}
	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_rows", 1)
	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&rowText); err != nil {
		t.Fatalf("read indexed row: %v", err)
	}
	if !strings.Contains(rowText, "verified spreadsheet snapshot") {
		t.Fatalf("structured index did not use verified snapshot: %q", rowText)
	}
}

func TestOfficeReadKnowledgeXLSXImportPersistsRichImages(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "rich-images.xlsx")
	writeOfficeReadXLSXWithImage(t, path, "name", "plan", "Alice", "spreadsheet image evidence", officeReadTestPNG(t))
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"xlsx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)

	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: dataRoot, OwnerID: "owner", TenantID: "tenant", IncludeExts: []string{".xlsx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.ImportedFiles != 1 || result.FailedFiles != 0 || len(result.Items) != 1 || result.Items[0].SourceID == "" {
		t.Fatalf("rich XLSX import = %#v, %v", result, err)
	}
	assertCount(t, store.db, "kb_rows", 1)
	nodes, err := store.ListNodesBySource(t.Context(), result.Items[0].SourceID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var assetID string
	for _, node := range nodes {
		if node.Type == NodeTypeImage && node.Metadata[MetaImageAssetID] != "" {
			assetID = node.Metadata[MetaImageAssetID]
			break
		}
	}
	if assetID == "" {
		t.Fatalf("rich XLSX import did not retain an image asset node: %#v", nodes)
	}
	if original, _, err := assets.OriginalImage(assetID); err != nil {
		t.Fatalf("managed XLSX image asset %q is not readable: %v", assetID, err)
	} else if _, err := os.Stat(original); err != nil {
		t.Fatalf("managed XLSX image asset %q is missing: %v", assetID, err)
	}
}

func TestOfficeReadKnowledgeXLSXRefreshRebuildsStructuredIndexFromSnapshot(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "refresh-snapshot-index.xlsx")
	writeOfficeReadXLSX(t, path, "name", "plan", "Alice", "initial spreadsheet snapshot")
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	existing := Source{ID: "refresh-xlsx-snapshot", Kind: SourceKindXLSX, URI: path, Title: "refresh snapshot", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), existing); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshSource(t.Context(), existing.ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if refreshed.ContentHash == "" {
		t.Fatal("refresh did not persist verified XLSX hash")
	}
	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_rows", 1)

	// Build the same owned refresh input used by RefreshSource, then remove the
	// live workbook before replacement. The table row must still come from the
	// private snapshot and replace the initial structured content atomically.
	writeOfficeReadXLSX(t, path, "name", "plan", "Alice", "replacement spreadsheet snapshot")
	next, nodes, _, _, distill, input, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(refreshed)
	if err != nil || input == nil || len(nodes) == 0 {
		if input != nil {
			input.close()
		}
		t.Fatalf("refresh snapshot parse = source=%#v nodes=%#v input=%#v err=%v", next, nodes, input, err)
	}
	snapshotPath := input.path
	defer input.close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	updated, err := store.replaceSourceDerivedRowsWithSpreadsheet(t.Context(), next, nodes, distill, "refresh", snapshotPath)
	if err != nil {
		t.Fatalf("replace refreshed XLSX snapshot: %v", err)
	}
	if updated.ContentHash != next.ContentHash {
		t.Fatalf("refresh hash = %q, want snapshot hash %q", updated.ContentHash, next.ContentHash)
	}
	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&rowText); err != nil {
		t.Fatalf("read refreshed row: %v", err)
	}
	if !strings.Contains(rowText, "replacement spreadsheet snapshot") || strings.Contains(rowText, "initial spreadsheet snapshot") {
		t.Fatalf("structured refresh did not use replacement snapshot: %q", rowText)
	}
	input.close()
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("refresh-owned XLSX snapshot not cleaned up: %v", err)
	}
}

func TestOfficeReadKnowledgeXLSXRefreshStructuredIndexFailureRollsBack(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "refresh-rollback.xlsx")
	writeOfficeReadXLSX(t, path, "name", "plan", "Alice", "stable spreadsheet content")
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	existing := Source{ID: "refresh-xlsx-rollback", Kind: SourceKindXLSX, URI: path, Title: "refresh rollback", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), existing); err != nil {
		t.Fatal(err)
	}
	before, err := store.RefreshSource(t.Context(), existing.ID)
	if err != nil {
		t.Fatalf("initial RefreshSource: %v", err)
	}
	beforeNodes, err := store.ListNodesBySource(t.Context(), before.ID, 10)
	if err != nil || len(beforeNodes) != 1 || !strings.Contains(beforeNodes[0].Text, "stable spreadsheet content") {
		t.Fatalf("initial XLSX nodes = %#v, %v", beforeNodes, err)
	}
	var beforeRow string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&beforeRow); err != nil || !strings.Contains(beforeRow, "stable spreadsheet content") {
		t.Fatalf("initial XLSX row = %q, %v", beforeRow, err)
	}

	writeOfficeReadXLSX(t, path, "name", "plan", "Alice", "replacement that must roll back")
	next, nodes, _, _, distill, input, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(before)
	if err != nil || input == nil || len(nodes) == 0 {
		if input != nil {
			input.close()
		}
		t.Fatalf("replacement refresh parse = source=%#v nodes=%#v input=%#v err=%v", next, nodes, input, err)
	}
	snapshotPath := input.path
	defer input.close()
	// Force the structured-row phase to fail only after the replacement
	// transaction has deleted and staged new derived rows. The rollback must
	// restore the previous source, document nodes, and table rows together.
	if err := os.Remove(snapshotPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.replaceSourceDerivedRowsWithSpreadsheet(t.Context(), next, nodes, distill, "refresh", snapshotPath); err == nil || !errors.Is(err, agent.ErrOfficeReadUnsafeContainer) {
		t.Fatalf("refresh replacement error = %v, want sanitized spreadsheet safety failure", err)
	}
	after, err := store.GetSource(t.Context(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ContentHash != before.ContentHash || after.Status != before.Status || after.ErrorMessage != before.ErrorMessage || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("failed XLSX refresh modified source: before=%#v after=%#v", before, after)
	}
	afterNodes, err := store.ListNodesBySource(t.Context(), before.ID, 10)
	if err != nil || len(afterNodes) != 1 || afterNodes[0].Text != beforeNodes[0].Text {
		t.Fatalf("failed XLSX refresh modified nodes: %#v, %v", afterNodes, err)
	}
	var afterRow string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows LIMIT 1`).Scan(&afterRow); err != nil || afterRow != beforeRow {
		t.Fatalf("failed XLSX refresh modified structured row: before=%q after=%q err=%v", beforeRow, afterRow, err)
	}
}

func TestOfficeReadStructuredKnowledgeContentBlocksOversizedInputBeforeLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	path := filepath.Join(t.TempDir(), "oversized.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(32*1024*1024 + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	source := Source{ID: "oversized-office-content", Kind: SourceKindDOCX, Title: "Oversized", RelativePath: "oversized.docx"}
	if _, err := ParseDocumentNodes(source, path, SourceKindDOCX); !errors.Is(err, agent.ErrOfficeReadInputTooLarge) {
		t.Fatalf("oversized rich Office package must stop before legacy DOCX fallback: %v", err)
	}
}

func TestOfficeReadStructuredKnowledgeContentRejectsMisnamedOOXML(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "misnamed.doc")
	writeOfficeReadDOCX(t, path, "actual DOCX content", nil)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"doc", "docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	source := Source{ID: "misnamed-office-content", Kind: SourceKindDOC, Title: "Misnamed", RelativePath: "misnamed.doc"}
	if _, active, err := ParseOfficeReadRichContentForKnowledgeFile(source, path); !active || err == nil || !strings.Contains(err.Error(), "format does not match") {
		t.Fatalf("rich preview mismatch = active=%t err=%v", active, err)
	}
	if _, err := ParseDocumentNodes(source, path, SourceKindDOC); !errors.Is(err, agent.ErrOfficeReadFormatMismatch) {
		t.Fatalf("misnamed rich Office package must stop before legacy DOC fallback: %v", err)
	}
}

func TestOfficeReadStructuredKnowledgeContentRejectsCallerKindMismatch(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "actual.docx")
	writeOfficeReadDOCX(t, path, "actual DOCX content", nil)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	source := Source{ID: "caller-kind-mismatch", Kind: SourceKindDOC, Title: "Mismatched", RelativePath: "actual.docx"}
	if _, active, err := ParseOfficeReadRichContentForKnowledge(source, path, SourceKindDOC); !active || !errors.Is(err, agent.ErrOfficeReadFormatMismatch) {
		t.Fatalf("rich preview caller-kind mismatch = active=%t err=%v", active, err)
	}
}

func TestOfficeReadImageImportRejectsMisnamedOOXMLWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "misnamed.doc")
	writeOfficeReadDOCX(t, path, "actual DOCX content", nil)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"doc", "docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "misnamed-office-image", Kind: SourceKindDOC, Title: "Misnamed", RelativePath: "misnamed.doc"}
	if nodes := store.ExtractAndProcessOfficeDocumentImages(t.Context(), source, path, SourceKindDOC, nil); len(nodes) != 0 {
		t.Fatalf("misnamed Office package produced image nodes: %#v", nodes)
	}
}

func TestOfficeReadImageImportRejectsCallerKindMismatch(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "caller-kind-mismatch-image", Kind: SourceKindDOC, Title: "Mismatched"}
	content := agent.OfficeReadRichContent{
		Format: "docx",
		Images: []agent.OfficeReadImage{{Name: "image.png", Ext: ".png", Data: officeReadTestPNG(t)}},
	}
	if nodes := store.extractAndProcessOfficeDocumentImagesFromRichContent(t.Context(), source, "actual.docx", SourceKindDOC, nil, content, true); len(nodes) != 0 {
		t.Fatalf("mismatched rich content produced image nodes: %#v", nodes)
	}
	if nodes := officeReadImagesFromRichContentForImport(t.Context(), store, source, SourceKindDOC, nil, content); len(nodes) != 0 {
		t.Fatalf("direct rich image import accepted mismatched content: %#v", nodes)
	}
}

func TestParseDocumentNodesPPTUsesOfficeReadOnlyWhenEnabled(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "legacy.ppt")
	if err := os.WriteFile(path, []byte("not a real presentation"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "source-ppt", Kind: SourceKindPPT, Title: "Legacy PPT", RelativePath: "legacy.ppt"}
	if _, err := ParseDocumentNodes(source, path, SourceKindPPT); err == nil {
		t.Fatal("disabled OfficeRead rich content must not make legacy .ppt importable")
	}
}

func TestParseDocumentNodesRejectsUnsafeOfficeReadContainerWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "unsafe.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for range 2 {
		part, err := archive.Create("word/document.xml")
		if err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("<w:document/>")); err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	source := Source{ID: "unsafe-office-container", Kind: SourceKindDOCX, Title: "Unsafe", RelativePath: "unsafe.docx"}
	_, err = ParseDocumentNodes(source, path, SourceKindDOCX)
	if !errors.Is(err, agent.ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want shared OfficeRead unsafe-container error", err)
	}
}

func TestOfficeReadImageImportRejectsUnsafeContainerWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "unsafe.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for range 2 {
		part, err := archive.Create("word/document.xml")
		if err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("<w:document/>")); err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "unsafe-office-image", Kind: SourceKindDOCX, Title: "Unsafe", RelativePath: "unsafe.docx"}
	if nodes := store.ExtractAndProcessOfficeDocumentImages(t.Context(), source, path, SourceKindDOCX, nil); len(nodes) != 0 {
		t.Fatalf("unsafe Office container produced image nodes: %#v", nodes)
	}
}

func TestLegacyOfficeImageImportPreflightsUnsafeContainer(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "unsafe-legacy-image.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for range 2 {
		part, err := archive.Create("word/document.xml")
		if err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("<w:document/>")); err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "unsafe-legacy-office-image", Kind: SourceKindDOCX, Title: "Unsafe", RelativePath: "unsafe-legacy-image.docx"}
	if nodes := store.ExtractAndProcessDocumentImages(t.Context(), source, path, SourceKindDOCX, nil); len(nodes) != 0 {
		t.Fatalf("legacy image import reopened unsafe Office container: %#v", nodes)
	}
}

func TestLegacyOfficeImageImportPreflightsEncryptedContainer(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted-legacy-image.docx")
	writeOfficeReadEncryptedDOCX(t, path)

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "encrypted-legacy-office-image", Kind: SourceKindDOCX, Title: "Encrypted", RelativePath: "encrypted-legacy-image.docx"}
	if nodes := store.ExtractAndProcessDocumentImages(t.Context(), source, path, SourceKindDOCX, nil); len(nodes) != 0 {
		t.Fatalf("legacy image import reopened encrypted Office container: %#v", nodes)
	}
}

func TestParseDocumentNodesRejectsEncryptedOfficeReadContainerWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	part, err := archive.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate, Flags: 1})
	if err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("<w:document/>")); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()
	source := Source{ID: "encrypted-office-container", Kind: SourceKindDOCX, Title: "Encrypted", RelativePath: "encrypted.docx"}
	_, err = ParseDocumentNodes(source, path, SourceKindDOCX)
	if !errors.Is(err, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted-container rejection", err)
	}
}

func TestParseDocumentNodesRejectsEncryptedOfficeContainerWhenRichContentDisabled(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	part, err := archive.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate, Flags: 1})
	if err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("<w:document/>")); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	source := Source{ID: "encrypted-office-disabled", Kind: SourceKindDOCX, Title: "Encrypted", RelativePath: "encrypted.docx"}
	if _, err := ParseDocumentNodes(source, path, SourceKindDOCX); !errors.Is(err, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("disabled rich content must not reopen encrypted DOCX through legacy parser: %v", err)
	}
}

func TestParseDocumentNodesRejectsEncryptedLegacyOLEWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted.ppt")
	writeOfficeReadTestOLE(t, path, "EncryptedSummary")
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"ppt"}, EmitMarkdown: &enabled}
	})
	defer restore()
	source := Source{ID: "encrypted-legacy-ole", Kind: SourceKindPPT, Title: "Encrypted", RelativePath: "encrypted.ppt"}
	_, err := ParseDocumentNodes(source, path, SourceKindPPT)
	if !errors.Is(err, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted-container rejection", err)
	}
}

func TestParseDocumentNodesRejectsBIFFFilePassWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted.xls")
	writeOfficeReadTestOLEWorkbook(t, path, []byte{0x09, 0x08, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00})
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"xls"}, EmitMarkdown: &enabled}
	})
	defer restore()
	source := Source{ID: "encrypted-biff-filepass", Kind: SourceKindXLS, Title: "Encrypted", RelativePath: "encrypted.xls"}
	_, err := ParseDocumentNodes(source, path, SourceKindXLS)
	if !errors.Is(err, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted-container rejection", err)
	}
}

func TestParseDocumentNodesRejectsEncryptedWordFIBWithoutLegacyFallback(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	fib := make([]byte, 32)
	binary.LittleEndian.PutUint16(fib[0:2], 0xa5ec)
	binary.LittleEndian.PutUint16(fib[10:12], 0x0100)
	writeOfficeReadTestOLEStream(t, path, "WordDocument", fib)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"doc"}, EmitMarkdown: &enabled}
	})
	defer restore()
	source := Source{ID: "encrypted-word-fib", Kind: SourceKindDOC, Title: "Encrypted", RelativePath: "encrypted.doc"}
	_, err := ParseDocumentNodes(source, path, SourceKindDOC)
	if !errors.Is(err, agent.ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted-container rejection", err)
	}
}

func TestOfficeReadKnowledgeImagesUseManagedAssets(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "images.docx")
	imageBytes := officeReadTestPNG(t)
	writeOfficeReadDOCX(t, path, "Image document body", map[string][]byte{"word/media/image1.png": imageBytes}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "source-image", Kind: SourceKindDOCX, Title: "Image document", RelativePath: "images.docx"}
	textNodes, active, err := ParseOfficeReadRichContentForKnowledgeFile(source, path)
	if err != nil || !active {
		t.Fatalf("rich text parse: active=%t err=%v", active, err)
	}
	rich, enabled, err := agent.ExtractOfficeReadRichContent(path)
	if err != nil || !enabled || len(rich.Images) != 1 {
		t.Fatalf("OfficeRead rich images: enabled=%t err=%v markdown=%q images=%#v", enabled, err, rich.Markdown, rich.Images)
	}
	imageNodes := officeReadImagesForImport(t.Context(), store, source, path, SourceKindDOCX, textNodes)
	if len(imageNodes) != 1 {
		content, _, extractErr := agent.ExtractOfficeReadRichContent(path)
		t.Fatalf("images=%#v office_images=%d extract_err=%v", imageNodes, len(content.Images), extractErr)
	}
	node := imageNodes[0]
	assetID := node.Metadata[MetaImageAssetID]
	if assetID == "" || !strings.HasPrefix(assetID, source.ID+"_") || node.Metadata[MetaImageAssetPath] != "" {
		t.Fatalf("asset metadata unsafe or missing: %#v", node.Metadata)
	}
	assetPath := assets.OriginalPath(assetID, ".png")
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("managed asset missing: %v", err)
	}
	if strings.Contains(node.Text, string(imageBytes)) || node.Metadata["_image_bytes_key"] != "" {
		t.Fatalf("binary data leaked into persisted node: %#v", node)
	}
	if err := assets.DeleteAssetsForSource(source.ID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(assetPath); !os.IsNotExist(err) {
		t.Fatalf("asset was not reclaimed: %v", err)
	}
}

func TestOfficeReadKnowledgeImportSharesOneRichExtraction(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "images.docx")
	writeOfficeReadDOCX(t, path, "Shared extraction body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "source-shared-extract", Kind: SourceKindDOCX, Title: "Shared extraction", RelativePath: "images.docx"}

	nodes, content, richAvailable, err := parseDocumentNodesWithOfficeReadRichContent(source, path, SourceKindDOCX)
	if err != nil || !richAvailable || len(nodes) == 0 || len(content.Images) != 1 {
		t.Fatalf("rich parse = nodes=%d available=%t images=%d err=%v", len(nodes), richAvailable, len(content.Images), err)
	}
	imageNodes := store.extractAndProcessOfficeDocumentImagesFromRichContent(context.Background(), source, path, SourceKindDOCX, nodes, content, richAvailable)
	if len(imageNodes) != 1 {
		t.Fatalf("image nodes = %#v", imageNodes)
	}
	assetID := imageNodes[0].Metadata[MetaImageAssetID]
	if assetID == "" {
		t.Fatalf("image asset id missing: %#v", imageNodes[0])
	}
	if _, err := os.Stat(assets.OriginalPath(assetID, ".png")); err != nil {
		t.Fatalf("shared extraction asset missing: %v", err)
	}
}

func TestOfficeReadImportSnapshotKeepsLegacyTextAndImagesOnOneVersion(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "replace-between-stages.docx")
	originalImage := append(officeReadTestPNG(t), make([]byte, 512)...)
	writeOfficeReadDOCX(t, path, "snapshot text version", map[string][]byte{"word/media/image1.png": originalImage}, true)

	// Rich consumption is disabled, so the image phase follows the legacy path
	// that would previously reopen path after text-node parsing.
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "one-version-legacy", Kind: SourceKindDOCX, Title: "one version"}

	parsed, err := parseDocumentNodesForOfficeReadImport(source, path, SourceKindDOCX)
	if err != nil || parsed == nil || len(parsed.nodes) == 0 || parsed.input == nil {
		t.Fatalf("import parse = %#v, %v", parsed, err)
	}
	snapshotPath := parsed.input.path
	if strings.Contains(strings.Join(nodeTexts(parsed.nodes), "\n"), "replacement text version") {
		t.Fatalf("text nodes unexpectedly used replacement: %#v", parsed.nodes)
	}
	defer parsed.close()

	// Replace the source after text parsing. The legacy image parser must still
	// read the import-owned snapshot, never this new live pathname.
	writeOfficeReadDOCX(t, path, "replacement text version", nil, true)
	imageNodes := store.extractAndProcessDocumentImagesUsingRichOfficeContent(t.Context(), source, snapshotPath, SourceKindDOCX, parsed.nodes, agent.OfficeReadRichContent{}, false)
	if len(imageNodes) != 1 {
		t.Fatalf("legacy snapshot image nodes = %#v", imageNodes)
	}
	assetID := imageNodes[0].Metadata[MetaImageAssetID]
	assetPath := assets.OriginalPath(assetID, ".png")
	got, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalImage) {
		t.Fatal("legacy image phase read bytes from a different document version")
	}
	if parsed.contentHash == "" {
		t.Fatal("import parse did not retain the verified snapshot hash")
	}
	if liveHash, err := fileSHA256(path); err != nil || liveHash == parsed.contentHash {
		t.Fatalf("verified snapshot hash=%q must not identify replacement live file=%q, err=%v", parsed.contentHash, liveHash, err)
	}

	parsed.close()
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("import-owned snapshot not cleaned up: %v", err)
	}
}

func TestOfficeReadRefreshSnapshotKeepsLegacyTextAndImagesOnOneVersion(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "refresh-replace-between-stages.docx")
	originalImage := append(officeReadTestPNG(t), make([]byte, 512)...)
	writeOfficeReadDOCX(t, path, "refresh snapshot text", map[string][]byte{"word/media/image1.png": originalImage}, true)
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	existing := Source{ID: "one-version-refresh", Kind: SourceKindDOCX, URI: path, Title: "refresh one version"}

	source, nodes, content, richAvailable, _, input, err := buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport(existing)
	if err != nil || input == nil || len(nodes) == 0 {
		t.Fatalf("refresh parse = source=%#v nodes=%#v input=%#v err=%v", source, nodes, input, err)
	}
	snapshotPath := input.path
	defer input.close()
	writeOfficeReadDOCX(t, path, "refresh replacement text", nil, true)
	imageNodes := store.extractAndProcessDocumentImagesUsingRichOfficeContent(t.Context(), source, snapshotPath, SourceKindDOCX, nodes, content, richAvailable)
	if len(imageNodes) != 1 {
		t.Fatalf("refresh legacy snapshot image nodes = %#v", imageNodes)
	}
	assetID := imageNodes[0].Metadata[MetaImageAssetID]
	got, err := os.ReadFile(assets.OriginalPath(assetID, ".png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalImage) {
		t.Fatal("refresh legacy image phase read bytes from a different document version")
	}
	snapshotHash, err := fileSHA256(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if source.ContentHash != snapshotHash {
		t.Fatalf("refresh source hash=%q, want verified snapshot hash=%q", source.ContentHash, snapshotHash)
	}
	if liveHash, err := fileSHA256(path); err != nil || liveHash == source.ContentHash {
		t.Fatalf("refresh source hash=%q must not identify replacement live file=%q, err=%v", source.ContentHash, liveHash, err)
	}
	input.close()
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("refresh-owned snapshot not cleaned up: %v", err)
	}
}

func TestOfficeReadImportPersistsVerifiedSnapshotHash(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "verified-import-hash.docx")
	writeOfficeReadDOCX(t, path, "verified snapshot hash body", nil, true)
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".docx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.ImportedFiles != 1 {
		t.Fatalf("ImportFiles = %#v, %v", result, err)
	}
	if len(result.Items) != 1 || result.Items[0].SourceID == "" {
		t.Fatalf("import items = %#v", result.Items)
	}
	source, err := store.GetSource(t.Context(), result.Items[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if source.ContentHash != expected {
		t.Fatalf("persisted import hash=%q, want parsed snapshot hash=%q", source.ContentHash, expected)
	}
}

func TestOfficeReadImportPersistsVerifiedSnapshotHashWithoutEmbeddedImages(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "verified-empty-import-hash.docx")
	writeOfficeReadDOCX(t, path, "image-free verified snapshot", nil)
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".docx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.ImportedFiles != 1 || len(result.Items) != 1 {
		t.Fatalf("ImportFiles = %#v, %v", result, err)
	}
	source, err := store.GetSource(t.Context(), result.Items[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if source.ContentHash != expected || source.NodeCount == 0 {
		t.Fatalf("image-free Office source = %#v; want hash=%q and parsed nodes", source, expected)
	}
}

func TestOfficeReadReimportRejectsSourceChangedAfterScanBeforeReplacingExisting(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "replace-after-scan.docx")
	writeOfficeReadDOCX(t, path, "scan version", nil)
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	originalHash, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	existing := Source{ID: "replace-after-scan", Kind: SourceKindDOCX, URI: path, Title: "existing", ContentHash: originalHash, Status: StatusParsed}
	if err := store.SaveSource(t.Context(), existing); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(t.Context(), DocumentNode{ID: "replace-after-scan-node", SourceID: existing.ID, Type: "document", Text: "previous indexed content"}); err != nil {
		t.Fatal(err)
	}

	req := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".docx"}, DistillMode: DistillModeRules}
	scanned, items, err := ScanFiles(t.Context(), req, []string{path}, nil)
	if err != nil || len(items) != 1 || items[0].FileHash != originalHash {
		t.Fatalf("ScanFiles = result=%#v items=%#v err=%v", scanned, items, err)
	}
	// The selected file changes after the duplicate/scan digest is established.
	// Re-import must keep the previous source intact rather than delete it and
	// index a byte version that the batch never scanned.
	writeOfficeReadDOCX(t, path, "replacement after scan", nil)
	result, err := store.importScannedItems(t.Context(), req, scanned, items)
	if err != nil || result.FailedFiles != 1 || len(result.Items) != 1 || result.Items[0].ErrorMessage != agent.ErrOfficeReadSourceChanged.Error() {
		t.Fatalf("reimport result = %#v, %v", result, err)
	}
	after, err := store.GetSource(t.Context(), existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ContentHash != originalHash || after.Status != StatusParsed {
		t.Fatalf("source changed despite rejected reimport: %#v", after)
	}
	nodes, err := store.ListNodesBySource(t.Context(), existing.ID, 10)
	if err != nil || len(nodes) != 1 || nodes[0].Text != "previous indexed content" {
		t.Fatalf("previous nodes were replaced: %#v, %v", nodes, err)
	}
}

func TestKnowledgeCSVReimportRejectsSourceChangedAfterScanBeforeReplacingExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "replace-after-scan.csv")
	mustWrite(t, path, []byte("name,plan\nAlice,scan version\n"))
	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	originalHash, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	existing := Source{ID: "replace-after-scan-csv", Kind: SourceKindCSV, URI: path, Title: "existing CSV", ContentHash: originalHash, Status: StatusParsed}
	if err := store.SaveSource(t.Context(), existing); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(t.Context(), DocumentNode{ID: "replace-after-scan-csv-node", SourceID: existing.ID, Type: "document", Text: "previous CSV indexed content"}); err != nil {
		t.Fatal(err)
	}

	req := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".csv"}, DistillMode: DistillModeRules}
	scanned, items, err := ScanFiles(t.Context(), req, []string{path}, nil)
	if err != nil || len(items) != 1 || items[0].FileHash != originalHash {
		t.Fatalf("CSV ScanFiles = result=%#v items=%#v err=%v", scanned, items, err)
	}
	mustWrite(t, path, []byte("name,plan\nAlice,replacement after scan\n"))
	result, err := store.importScannedItems(t.Context(), req, scanned, items)
	if err != nil || result.FailedFiles != 1 || len(result.Items) != 1 || result.Items[0].ErrorMessage != agent.ErrOfficeReadSourceChanged.Error() {
		t.Fatalf("CSV reimport result = %#v, %v", result, err)
	}
	after, err := store.GetSource(t.Context(), existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ContentHash != originalHash || after.Status != StatusParsed {
		t.Fatalf("CSV source changed despite rejected reimport: %#v", after)
	}
	nodes, err := store.ListNodesBySource(t.Context(), existing.ID, 10)
	if err != nil || len(nodes) != 1 || nodes[0].Text != "previous CSV indexed content" {
		t.Fatalf("previous CSV nodes were replaced: %#v, %v", nodes, err)
	}
}

func TestOfficeReadFirstImportRejectsSourceChangedAfterScan(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	root := t.TempDir()
	path := filepath.Join(root, "first-import-replace-after-scan.docx")
	writeOfficeReadDOCX(t, path, "first scan version", nil)
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "legacy"}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	req := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".docx"}, DistillMode: DistillModeRules}
	scanned, items, err := ScanFiles(t.Context(), req, []string{path}, nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("ScanFiles = result=%#v items=%#v err=%v", scanned, items, err)
	}
	writeOfficeReadDOCX(t, path, "first replacement after scan", nil)
	result, err := store.importScannedItems(t.Context(), req, scanned, items)
	if err != nil || result.ImportedFiles != 0 || result.FailedFiles != 1 || len(result.Items) != 1 || result.Items[0].ErrorMessage != agent.ErrOfficeReadSourceChanged.Error() {
		t.Fatalf("first import result = %#v, %v", result, err)
	}
	if result.Items[0].SourceID == "" {
		t.Fatalf("rejected first import must retain its failed source identity: %#v", result.Items[0])
	}
	source, err := store.GetSource(t.Context(), result.Items[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Status != StatusFailed || source.ErrorMessage != agent.ErrOfficeReadSourceChanged.Error() {
		t.Fatalf("first import source = %#v", source)
	}
	nodes, err := store.ListNodesBySource(t.Context(), source.ID, 10)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("rejected first import persisted nodes: %#v, %v", nodes, err)
	}
}

func TestOfficeReadKnowledgeRefreshRebuildsAndReclaimsImageAssets(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "refresh-images.docx")
	writeOfficeReadDOCX(t, path, "Refresh body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source := Source{ID: "refresh-office-image", Kind: SourceKindDOCX, URI: path, Title: "Refresh image", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	oldAssetID := source.ID + "_old-image"
	if _, err := assets.SaveImageFromBytes(oldAssetID, officeReadTestPNG(t), ".png"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(t.Context(), DocumentNode{
		ID:       "refresh-old-image-node",
		SourceID: source.ID,
		Type:     NodeTypeImage,
		Metadata: map[string]string{MetaImageAssetID: oldAssetID},
	}); err != nil {
		t.Fatal(err)
	}

	refreshed, err := store.RefreshSource(t.Context(), source.ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if refreshed.ID != source.ID || refreshed.NodeCount < 2 {
		t.Fatalf("refreshed source = %#v", refreshed)
	}
	nodes, err := store.ListNodesBySource(t.Context(), source.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var freshAssetID string
	for _, node := range nodes {
		if node.Type == NodeTypeImage {
			freshAssetID = node.Metadata[MetaImageAssetID]
		}
	}
	if freshAssetID == "" || freshAssetID == oldAssetID {
		t.Fatalf("refreshed image asset = %q; nodes=%#v", freshAssetID, nodes)
	}
	if _, err := os.Stat(assets.OriginalPath(freshAssetID, ".png")); err != nil {
		t.Fatalf("refreshed asset missing: %v", err)
	}
	if _, err := os.Stat(assets.AssetDir(oldAssetID)); !os.IsNotExist(err) {
		t.Fatalf("superseded asset still exists or stat failed: %v", err)
	}
}

func TestOfficeReadKnowledgeReimportReclaimsSupersededImageAssets(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "reimport-images.docx")
	writeOfficeReadDOCX(t, path, "Reimport body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	req := DirectoryImportRequest{RootPath: dataRoot, OwnerID: "owner", TenantID: "tenant", IncludeExts: []string{".docx"}, DistillMode: DistillModeRules}
	first, err := store.ImportFiles(t.Context(), req, []string{path})
	if err != nil || first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v, %v", first, err)
	}
	sources, err := store.ListSources(t.Context(), ListSourcesOptions{OwnerID: "owner", TenantID: "tenant", Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("first sources = %#v, %v", sources, err)
	}
	oldAssetID := sources[0].ID + "_obsolete-image"
	if _, err := assets.SaveImageFromBytes(oldAssetID, officeReadTestPNG(t), ".png"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(t.Context(), DocumentNode{ID: "reimport-obsolete-node", SourceID: sources[0].ID, Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: oldAssetID}}); err != nil {
		t.Fatal(err)
	}
	writeOfficeReadDOCX(t, path, "Reimport body changed", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)

	second, err := store.ImportFiles(t.Context(), req, []string{path})
	if err != nil || second.ImportedFiles != 1 {
		t.Fatalf("second import = %#v, %v", second, err)
	}
	if _, err := os.Stat(assets.AssetDir(oldAssetID)); !os.IsNotExist(err) {
		t.Fatalf("superseded reimport asset still exists or stat failed: %v", err)
	}
	nodes, err := store.ListNodesBySource(t.Context(), sources[0].ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !hasImageAssetNode(nodes) {
		t.Fatalf("reimport discarded OfficeRead image nodes: %#v", nodes)
	}
}

func TestOfficeReadKnowledgeImportCleansProvisionalRichAssetsWhenNodeInsertFails(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "rollback-images.docx")
	writeOfficeReadDOCX(t, path, "Rollback body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	if _, err := store.db.Exec(`CREATE TRIGGER reject_document_node_insert BEFORE INSERT ON document_nodes BEGIN SELECT RAISE(FAIL, 'test node insert failure'); END`); err != nil {
		t.Fatal(err)
	}

	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: dataRoot, OwnerID: "owner", TenantID: "tenant", IncludeExts: []string{".docx"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.FailedFiles != 1 {
		t.Fatalf("import = %#v, %v; want one recorded failure", result, err)
	}
	entries, err := os.ReadDir(assets.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed rich import left provisional assets: %#v", entries)
	}
}

func TestOfficeReadKnowledgeRefreshCleansProvisionalRichAssetsWhenNodeInsertFails(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "refresh-rollback-images.docx")
	writeOfficeReadDOCX(t, path, "Refresh rollback body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source := Source{ID: "refresh-rollback-image", Kind: SourceKindDOCX, URI: path, Title: "Refresh rollback", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_refresh_document_node_insert BEFORE INSERT ON document_nodes BEGIN SELECT RAISE(FAIL, 'test refresh node insert failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RefreshSource(t.Context(), source.ID); err == nil {
		t.Fatal("RefreshSource succeeded despite document-node insert rejection")
	}
	entries, err := os.ReadDir(assets.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed rich refresh left provisional assets: %#v", entries)
	}
}

func TestOfficeReadKnowledgeXLSXRefreshCleansProvisionalRichAssetsWhenNodeInsertFails(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "refresh-rollback-images.xlsx")
	writeOfficeReadXLSXWithImage(t, path, "name", "plan", "Alice", "Refresh spreadsheet image evidence", officeReadTestPNG(t))
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"xlsx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source := Source{ID: "refresh-rollback-xlsx-image", Kind: SourceKindXLSX, URI: path, Title: "Refresh spreadsheet rollback", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_refresh_xlsx_document_node_insert BEFORE INSERT ON document_nodes BEGIN SELECT RAISE(FAIL, 'test refresh XLSX node insert failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RefreshSource(t.Context(), source.ID); err == nil {
		t.Fatal("RefreshSource succeeded despite XLSX document-node insert rejection")
	}
	entries, err := os.ReadDir(assets.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed rich XLSX refresh left provisional assets: %#v", entries)
	}
}

func TestOfficeReadRichParseSnapshotDoesNotReopenLegacyImagesAfterPolicyChanges(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "policy-snapshot.docx")
	writeOfficeReadDOCX(t, path, "Policy snapshot body", nil, true)
	enabled := true
	engine := "officeread"
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: engine, Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source := Source{ID: "policy-snapshot", Kind: SourceKindDOCX, URI: path, Title: "Policy snapshot", Status: StatusParsed}

	nodes, content, richActive, err := parseDocumentNodesWithOfficeReadRichContent(source, path, SourceKindDOCX)
	if err != nil || !richActive || len(nodes) == 0 || len(content.Images) != 0 {
		t.Fatalf("rich parse = nodes=%d active=%t images=%d err=%v", len(nodes), richActive, len(content.Images), err)
	}
	// Simulate a GUI rollback after text parsing but before optional image
	// ingestion. A single import must retain its original parse-time policy.
	engine = "legacy"
	if imageNodes := store.extractAndProcessDocumentImagesUsingRichOfficeContent(t.Context(), source, path, SourceKindDOCX, nodes, content, richActive); len(imageNodes) != 0 {
		t.Fatalf("policy-switched import reopened DOCX through legacy image parser: %#v", imageNodes)
	}
}

func hasImageAssetNode(nodes []DocumentNode) bool {
	for _, node := range nodes {
		if node.Type == NodeTypeImage && node.Metadata[MetaImageAssetID] != "" {
			return true
		}
	}
	return false
}

func TestOfficeReadKnowledgeImagesDoNotPersistHostAssetPath(t *testing.T) {
	clearOfficeReadRichContentEnvironment(t)
	path := filepath.Join(t.TempDir(), "images.docx")
	writeOfficeReadDOCX(t, path, "Image document body", map[string][]byte{"word/media/image1.png": officeReadTestPNG(t)}, true)
	enabled := true
	restore := agent.SetOfficeReadConfigProvider(func() agent.OfficeReadConfig {
		return agent.OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	dataRoot := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	source := Source{ID: "path-free-image", Kind: SourceKindDOCX, URI: "file://images.docx", Title: "Image document", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}
	if err := store.SaveSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	textNodes, active, err := ParseOfficeReadRichContentForKnowledgeFile(source, path)
	if err != nil || !active {
		t.Fatalf("rich text parse: active=%t err=%v", active, err)
	}
	imageNodes := officeReadImagesForImport(t.Context(), store, source, path, SourceKindDOCX, textNodes)
	if len(imageNodes) != 1 {
		t.Fatalf("images = %#v", imageNodes)
	}
	if err := store.SaveDocumentNode(t.Context(), imageNodes[0]); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodesBySource(t.Context(), source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("stored nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetPath]; got != "" {
		t.Fatalf("image node persisted host asset path: %q", got)
	}
	assetID := nodes[0].Metadata[MetaImageAssetID]
	if assetID == "" {
		t.Fatalf("image node lost opaque asset ID: %#v", nodes[0].Metadata)
	}
	resolved, err := store.FindImageAssetSource(t.Context(), assetID)
	if err != nil || resolved.ID != source.ID {
		t.Fatalf("asset lookup by opaque ID = %#v, %v", resolved, err)
	}
}

func TestOfficeReadKnowledgeImagePayloadLimit(t *testing.T) {
	if shouldImportOfficeReadImageData(nil) {
		t.Fatal("empty OfficeRead image must not enter the asset pipeline")
	}
	if !shouldImportOfficeReadImageData(make([]byte, MaxKnowledgeImageAssetBytes)) {
		t.Fatal("image at the controlled asset decode limit must be accepted")
	}
	if shouldImportOfficeReadImageData(make([]byte, MaxKnowledgeImageAssetBytes+1)) {
		t.Fatal("oversized OfficeRead image must not enter the asset pipeline")
	}
}

func TestLimitKnowledgeDocumentImagePayloadsEnforcesAggregateBudget(t *testing.T) {
	firstKey := "first"
	secondKey := "second"
	imageNodes := []DocumentNode{
		{ID: firstKey, Metadata: map[string]string{"_image_bytes_key": firstKey}},
		{ID: secondKey, Metadata: map[string]string{"_image_bytes_key": secondKey}},
	}
	imageBytes := map[string][]byte{
		firstKey:  make([]byte, 20*1024*1024),
		secondKey: make([]byte, 20*1024*1024),
	}

	kept := limitKnowledgeDocumentImagePayloads(imageNodes, imageBytes)
	if len(kept) != 1 || kept[0].ID != firstKey {
		t.Fatalf("aggregate image budget retained nodes = %#v", kept)
	}
	if _, ok := imageBytes[firstKey]; !ok {
		t.Fatal("first image payload must remain available")
	}
	if _, ok := imageBytes[secondKey]; ok {
		t.Fatal("payload beyond aggregate document budget must be released")
	}
}

func TestLimitKnowledgeDocumentImagePayloadsDropsOversizedAndDuplicatePayloads(t *testing.T) {
	oversizedKey := "oversized"
	keptKey := "kept"
	imageNodes := []DocumentNode{
		{ID: oversizedKey, Metadata: map[string]string{"_image_bytes_key": oversizedKey}},
		{ID: keptKey, Metadata: map[string]string{"_image_bytes_key": keptKey}},
		{ID: "duplicate", Metadata: map[string]string{"_image_bytes_key": keptKey}},
	}
	imageBytes := map[string][]byte{
		oversizedKey: make([]byte, MaxKnowledgeImageAssetBytes+1),
		keptKey:      make([]byte, 1024),
		"orphan":     make([]byte, 1024),
	}

	kept := limitKnowledgeDocumentImagePayloads(imageNodes, imageBytes)
	if len(kept) != 1 || kept[0].ID != keptKey {
		t.Fatalf("retained image nodes = %#v", kept)
	}
	if len(imageBytes) != 1 || imageBytes[keptKey] == nil {
		t.Fatalf("only referenced retained payload must remain: %#v", imageBytes)
	}
}

func TestExtractDOCXImagesSkipsUnreferencedLargeMedia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "images.docx")
	writeOfficeReadDOCX(t, path, "body", map[string][]byte{
		"word/media/image1.png": make([]byte, 1024),
		"word/media/unused.bin": make([]byte, MaxKnowledgeImageAssetBytes+1),
	}, true)
	nodes, imageBytes, err := ExtractDOCXImages(Source{ID: "docx-images"}, path, nil)
	if err != nil {
		t.Fatalf("ExtractDOCXImages: %v", err)
	}
	if len(nodes) != 1 || len(imageBytes) != 1 {
		t.Fatalf("referenced image extraction = nodes=%#v bytes=%d", nodes, len(imageBytes))
	}
}

func TestReadZipFileAtMostRejectsDeclaredOversizedPart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-part.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	part, err := archive.Create("large.xml")
	if err == nil {
		_, err = part.Write(make([]byte, 1024))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := readZipFileAtMost(reader.File[0], 100); !errors.Is(err, errKnowledgeOfficeImagePartTooLarge) {
		t.Fatalf("oversized ZIP part error = %v", err)
	}
}

func TestOfficeReadKnowledgeImageImportOmitsEmptyDerivedMetadata(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	source := Source{ID: "source-empty-derived", Kind: SourceKindDOCX}
	nodes := officeReadImagesFromRichContentForImport(t.Context(), store, source, SourceKindDOCX, nil, agent.OfficeReadRichContent{
		Images: []agent.OfficeReadImage{{Name: "plain.png", Ext: ".png", Data: officeReadTestPNG(t)}},
	})
	if len(nodes) != 1 {
		t.Fatalf("image nodes = %#v", nodes)
	}
	for _, key := range []string{MetaImageAltText, "context_before", "_image_bytes_key"} {
		if _, exists := nodes[0].Metadata[key]; exists {
			t.Fatalf("persisted node must omit empty/internal metadata %q: %#v", key, nodes[0].Metadata)
		}
	}
}

func clearOfficeReadRichContentEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "")
	t.Setenv("MACLAW_OFFICE_READ_EMIT_MARKDOWN", "")
}

func nodeTexts(nodes []DocumentNode) []string {
	result := make([]string, len(nodes))
	for i := range nodes {
		result[i] = nodes[i].Text
	}
	return result
}

func officeReadTestPNG(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 2), B: uint8((x + y) % 255), A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeOfficeReadDOCX(t *testing.T, path, text string, media map[string][]byte, imageRef ...bool) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	body := `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	if len(imageRef) > 0 && imageRef[0] {
		body += `<w:p><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:blipFill><a:blip xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:embed="rIdImage1"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	}
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` + body + `</w:body></w:document>`))
	if len(imageRef) > 0 && imageRef[0] {
		rels, err := zw.Create("word/_rels/document.xml.rels")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdImage1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`))
	}
	for name, data := range media {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeOfficeReadXLSX(t *testing.T, path, headerA, headerB, valueA, valueB string) {
	writeOfficeReadXLSXWithImage(t, path, headerA, headerB, valueA, valueB, nil)
}

func writeOfficeReadXLSXWithImage(t *testing.T, path, headerA, headerB, valueA, valueB string, imageData []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	contentTypes := `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
	sheetXML := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData></worksheet>`
	parts := map[string]string{
		"[Content_Types].xml":        contentTypes + `</Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="4" uniqueCount="4"><si><t>` + headerA + `</t></si><si><t>` + headerB + `</t></si><si><t>` + valueA + `</t></si><si><t>` + valueB + `</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   sheetXML,
	}
	if len(imageData) > 0 {
		parts["[Content_Types].xml"] = contentTypes + `<Default Extension="png" ContentType="image/png"/><Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/></Types>`
		parts["xl/worksheets/sheet1.xml"] = `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData><drawing r:id="rIdDrawing1"/></worksheet>`
		parts["xl/worksheets/_rels/sheet1.xml.rels"] = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdDrawing1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/></Relationships>`
		parts["xl/drawings/drawing1.xml"] = `<?xml version="1.0" encoding="UTF-8"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><xdr:twoCellAnchor><xdr:from><xdr:col>0</xdr:col><xdr:row>0</xdr:row></xdr:from><xdr:to><xdr:col>2</xdr:col><xdr:row>4</xdr:row></xdr:to><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="1" name="Spreadsheet evidence" descr="Spreadsheet image evidence"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:embed="rIdImage1"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr/></xdr:pic><xdr:clientData/></xdr:twoCellAnchor></xdr:wsDr>`
		parts["xl/drawings/_rels/drawing1.xml.rels"] = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdImage1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`
	}
	for name, body := range parts {
		part, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(body)); err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if len(imageData) > 0 {
		part, err := zw.Create("xl/media/image1.png")
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
		if _, err := part.Write(imageData); err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeOfficeReadEncryptedDOCX(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	part, err := archive.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate, Flags: 1})
	if err == nil {
		_, err = part.Write([]byte("<w:document/>"))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func writeOfficeReadTestOLE(t *testing.T, filePath string, streams ...string) {
	t.Helper()
	const (
		sectorSize = 512
		endOfChain = 0xfffffffe
		noStream   = 0xffffffff
		fatSector  = 0xfffffffd
	)
	data := make([]byte, sectorSize*3)
	copy(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	binary.LittleEndian.PutUint16(data[24:26], 0x003e)
	binary.LittleEndian.PutUint16(data[26:28], 3)
	binary.LittleEndian.PutUint16(data[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:32], 9)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1)
	binary.LittleEndian.PutUint32(data[48:52], 1)
	binary.LittleEndian.PutUint32(data[60:64], endOfChain)
	binary.LittleEndian.PutUint32(data[68:72], endOfChain)
	for off := 76; off < 512; off += 4 {
		binary.LittleEndian.PutUint32(data[off:off+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[76:80], 0)
	for off := sectorSize; off < sectorSize*2; off += 4 {
		binary.LittleEndian.PutUint32(data[off:off+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[sectorSize:sectorSize+4], fatSector)
	binary.LittleEndian.PutUint32(data[sectorSize+4:sectorSize+8], endOfChain)
	directory := data[sectorSize*2:]
	writeOfficeReadOLEDirectoryEntry(directory[:128], "Root Entry", 5, noStream)
	if len(streams) > 0 {
		binary.LittleEndian.PutUint32(directory[76:80], 1)
	}
	for i, name := range streams {
		entry := directory[(i+1)*128 : (i+2)*128]
		writeOfficeReadOLEDirectoryEntry(entry, name, 2, noStream)
		if i+1 < len(streams) {
			binary.LittleEndian.PutUint32(entry[72:76], uint32(i+2))
		}
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeOfficeReadOLEDirectoryEntry(entry []byte, name string, objectType byte, childID uint32) {
	encoded := []rune(name)
	for i, r := range encoded {
		binary.LittleEndian.PutUint16(entry[i*2:i*2+2], uint16(r))
	}
	binary.LittleEndian.PutUint16(entry[len(encoded)*2:len(encoded)*2+2], 0)
	binary.LittleEndian.PutUint16(entry[64:66], uint16((len(encoded)+1)*2))
	entry[66] = objectType
	entry[67] = 1
	binary.LittleEndian.PutUint32(entry[68:72], 0xffffffff)
	binary.LittleEndian.PutUint32(entry[72:76], 0xffffffff)
	binary.LittleEndian.PutUint32(entry[76:80], childID)
	binary.LittleEndian.PutUint32(entry[116:120], 0xfffffffe)
}

func writeOfficeReadTestOLEWorkbook(t *testing.T, filePath string, workbookPrefix []byte) {
	writeOfficeReadTestOLEStream(t, filePath, "Workbook", workbookPrefix)
}

func writeOfficeReadTestOLEStream(t *testing.T, filePath, name string, prefix []byte) {
	t.Helper()
	const (
		sectorSize    = 512
		endOfChain    = 0xfffffffe
		noStream      = 0xffffffff
		fatSector     = 0xfffffffd
		streamSectors = 8
	)
	data := make([]byte, sectorSize*(1+2+streamSectors))
	copy(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	binary.LittleEndian.PutUint16(data[24:26], 0x003e)
	binary.LittleEndian.PutUint16(data[26:28], 3)
	binary.LittleEndian.PutUint16(data[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:32], 9)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1)
	binary.LittleEndian.PutUint32(data[48:52], 1)
	binary.LittleEndian.PutUint32(data[60:64], endOfChain)
	binary.LittleEndian.PutUint32(data[68:72], endOfChain)
	for offset := 76; offset < 512; offset += 4 {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[76:80], 0)
	fat := data[sectorSize : sectorSize*2]
	for offset := 0; offset < len(fat); offset += 4 {
		binary.LittleEndian.PutUint32(fat[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(fat[0:4], fatSector)
	binary.LittleEndian.PutUint32(fat[4:8], endOfChain)
	for sector := 2; sector < 2+streamSectors; sector++ {
		next := uint32(endOfChain)
		if sector+1 < 2+streamSectors {
			next = uint32(sector + 1)
		}
		binary.LittleEndian.PutUint32(fat[sector*4:sector*4+4], next)
	}
	directory := data[sectorSize*2 : sectorSize*3]
	writeOfficeReadOLEDirectoryEntry(directory[:128], "Root Entry", 5, 1)
	writeOfficeReadOLEDirectoryEntry(directory[128:256], name, 2, noStream)
	binary.LittleEndian.PutUint32(directory[128+116:128+120], 2)
	binary.LittleEndian.PutUint32(directory[128+120:128+124], sectorSize*streamSectors)
	copy(data[sectorSize*3:], prefix)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
