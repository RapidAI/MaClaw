package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentNodePersistenceStripsLegacyImageAssetPath(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "source-path-free", Kind: SourceKindImage, URI: "file://image.png", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID: "node-path-free", SourceID: source.ID, Type: NodeTypeImage,
		Metadata: map[string]string{MetaImageAssetID: source.ID, MetaImageAssetPath: `C:\private\knowledge_assets\source-path-free\original.png`},
	}); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("stored nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetPath]; got != "" || nodes[0].Metadata[MetaImageAssetID] != source.ID {
		t.Fatalf("unsafe or missing asset metadata: %#v", nodes[0].Metadata)
	}
}

func TestDocumentNodePersistenceDropsInvalidImageAssetID(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "image-source", Kind: SourceKindImage, URI: "file://image.png", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID: "image-node", SourceID: source.ID, Type: NodeTypeImage,
		Metadata: map[string]string{MetaImageAssetID: " image-source ", "caption": "diagram"},
	}); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("stored nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetID]; got != "" || nodes[0].Metadata["caption"] != "diagram" {
		t.Fatalf("invalid asset ID survived persistence sanitization: %#v", nodes[0].Metadata)
	}
}

func TestDocumentNodeReadDropsInvalidImageAssetIDInsertedAfterOpen(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "image-source", Kind: SourceKindImage, URI: "file://image.png", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	// Simulate a maintenance/restore path that bypasses SaveDocumentNode after
	// the database has already completed its open-time legacy scrub.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, metadata_json) VALUES (?, ?, ?, ?)`, "image-node", source.ID, NodeTypeImage, `{"image_asset_id":" image-source ","caption":"diagram"}`); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("stored nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetID]; got != "" || nodes[0].Metadata["caption"] != "diagram" {
		t.Fatalf("read path exposed invalid asset ID: %#v", nodes[0].Metadata)
	}
}

func TestOpeningLegacyKnowledgeStoreScrubsImageAssetPath(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "legacy-image-source", Kind: SourceKindImage, URI: "file://legacy.png", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	legacyMetadata, err := json.Marshal(map[string]string{
		MetaImageAssetID:   source.ID,
		MetaImageAssetPath: `C:\private\knowledge_assets\legacy-image-source\original.png`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, metadata_json) VALUES (?, ?, ?, ?)`, "legacy-image-node", source.ID, NodeTypeImage, string(legacyMetadata)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("reopened nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetPath]; got != "" || nodes[0].Metadata[MetaImageAssetID] != source.ID {
		t.Fatalf("legacy image asset path not scrubbed: %#v", nodes[0].Metadata)
	}
}

func TestOpeningLegacyKnowledgeStoreScrubsInvalidImageAssetID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "legacy-image-source", Kind: SourceKindImage, URI: "file://legacy.png", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	legacyMetadata, err := json.Marshal(map[string]string{MetaImageAssetID: " legacy-image-source "})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, metadata_json) VALUES (?, ?, ?, ?)`, "legacy-image-node", source.ID, NodeTypeImage, string(legacyMetadata)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("reopened nodes = %#v, %v", nodes, err)
	}
	if got := nodes[0].Metadata[MetaImageAssetID]; got != "" {
		t.Fatalf("legacy invalid image asset ID survived open-time scrub: %#v", nodes[0].Metadata)
	}
}

func TestSnapshotBoundariesStripLegacyImageAssetPath(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	inputPath := filepath.Join(dataDir, "legacy-image-snapshot.jsonl")
	source := Source{ID: "snapshot-image-source", Kind: SourceKindImage, URI: "file://snapshot.png", Status: StatusParsed}
	sourceJSON, err := json.Marshal(exportRecord{Type: "source", Data: source})
	if err != nil {
		t.Fatal(err)
	}
	node := DocumentNode{ID: "snapshot-image-node", SourceID: source.ID, Type: NodeTypeImage, Metadata: map[string]string{
		MetaImageAssetID: source.ID, MetaImageAssetPath: `C:\private\knowledge_assets\snapshot-image-source\original.png`,
	}}
	nodeJSON, err := json.Marshal(exportRecord{Type: "node", Data: node})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, append(append(sourceJSON, '\n'), append(nodeJSON, '\n')...), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dataDir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ImportSnapshot(ctx, SnapshotImportOptions{InputPath: inputPath, SkipSafetyBackup: true}); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodesBySource(ctx, source.ID, 10)
	if err != nil || len(nodes) != 1 || nodes[0].Metadata[MetaImageAssetPath] != "" {
		t.Fatalf("snapshot import kept image path: nodes=%#v err=%v", nodes, err)
	}

	// Simulate a database created before the migration, then verify exporting it
	// still cannot leak the path even before a subsequent reopen takes place.
	legacyMetadata, err := json.Marshal(map[string]string{MetaImageAssetID: source.ID, MetaImageAssetPath: `C:\private\knowledge_assets\snapshot-image-source\original.png`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE document_nodes SET metadata_json = ? WHERE id = ?`, string(legacyMetadata), node.ID); err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(dataDir, "export.jsonl")
	if _, err := store.ExportSnapshot(ctx, ExportOptions{OutputPath: exportPath}); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(exported), "image_asset_path") || strings.Contains(string(exported), "C:\\private\\knowledge_assets") {
		t.Fatalf("snapshot export leaked legacy image path: %s", exported)
	}
}
