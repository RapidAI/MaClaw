package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSearchImagesReturnsOnlyMatchingImageNodes(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "image-source", Kind: SourceKindImage, URI: "file://diagram", OwnerID: "owner", TenantID: "tenant", Title: "System diagram", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "image-node", SourceID: "image-source", Type: NodeTypeImage, Title: "Architecture", Text: "Architecture diagram showing the API gateway and database", Metadata: map[string]string{MetaImageAssetID: "image-source"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "text-source", Kind: SourceKindText, URI: "memory://text", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "text-node", SourceID: "text-source", Type: "paragraph", Text: "Architecture diagram documentation", Metadata: map[string]string{}}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "architecture diagram", OwnerID: "owner", TenantID: "tenant", Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].NodeID != "image-node" || results[0].NodeType != NodeTypeImage {
		t.Fatalf("image results = %#v", results)
	}
	if results[0].Media == nil || results[0].Media.AssetID != "image-source" {
		t.Fatalf("image result lost stable asset ID: %#v", results[0].Media)
	}
}

func TestSearchImagesUsesRecordedAssetIDForEmbeddedImage(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "document-source", Kind: SourceKindDOCX, URI: "file://design.docx", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	// Embedded image asset IDs use the extractor's bytes key, which must not be
	// inferred from the independently generated document-node ID.
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "image-node-generated-id",
		SourceID: "document-source",
		Type:     NodeTypeImage,
		Title:    "Gateway topology",
		Text:     "Gateway topology architecture diagram",
		Metadata: map[string]string{MetaImageAssetID: "document-source_media-image-7"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "gateway topology", OwnerID: "owner", TenantID: "tenant", Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Media == nil || results[0].Media.AssetID != "document-source_media-image-7" {
		t.Fatalf("embedded image asset mapping = %#v", results)
	}
}

func TestImageAssetLookupAndSearchRejectWhitespacePaddedAssetIDs(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "document-source", Kind: SourceKindDOCX, URI: "file://design.docx", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "image-node",
		SourceID: source.ID,
		Type:     NodeTypeImage,
		Text:     "gateway topology architecture diagram",
		Metadata: map[string]string{MetaImageAssetID: " document-source_media-image-7 "},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImageAssetSource(ctx, " document-source "); err == nil {
		t.Fatal("whitespace-padded asset lookup must not resolve a canonical source")
	}
	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "gateway topology", OwnerID: "owner", TenantID: "tenant", Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Media != nil {
		t.Fatalf("whitespace-padded persisted asset ID was exposed: %#v", results)
	}
}

func TestFindImageAssetSourceDoesNotUseLegacyAssetPath(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "document-source", Kind: SourceKindDOCX, URI: "file://design.docx", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	// Simulate a legacy/direct-write row after startup. image_asset_path is not
	// an authorization index and must never bind an opaque ID to a source.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, metadata_json) VALUES (?, ?, ?, ?)`, "legacy-image-node", source.ID, NodeTypeImage, `{"image_asset_path":"C:\\private\\knowledge_assets\\embedded-asset\\original.png"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImageAssetSource(ctx, "embedded-asset"); err == nil {
		t.Fatal("legacy image asset path must not authorize an opaque asset lookup")
	}
}

func TestFindImageAssetSourceRejectsCrossSourceAndNonImageNodeClaims(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	owner := Source{ID: "document-owner", Kind: SourceKindDOCX, URI: "file://owner.docx", OwnerID: "owner-a", TenantID: "tenant", Status: StatusParsed}
	other := Source{ID: "other-document", Kind: SourceKindDOCX, URI: "file://other.docx", OwnerID: "owner-b", TenantID: "tenant", Status: StatusParsed}
	textOnly := Source{ID: "text-only-document", Kind: SourceKindDOCX, URI: "file://text-only.docx", OwnerID: "owner-a", TenantID: "tenant", Status: StatusParsed}
	fallbackOnly := Source{ID: "document-id-fallback", Kind: SourceKindDOCX, URI: "file://fallback.docx", OwnerID: "owner-a", TenantID: "tenant", Status: StatusParsed}
	for _, source := range []Source{owner, other, textOnly, fallbackOnly} {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	assetID := owner.ID + "_embedded-1"
	// Insert the malformed rows first. Lookup must not use either a text node
	// or a foreign document's image node to authorize the owner's asset.
	for _, node := range []DocumentNode{
		{ID: "foreign-image", SourceID: other.ID, Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: assetID}},
		{ID: "owner-text", SourceID: owner.ID, Type: "paragraph", Metadata: map[string]string{MetaImageAssetID: assetID}},
		{ID: "owner-image", SourceID: owner.ID, Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: assetID}},
	} {
		if err := store.SaveDocumentNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := store.FindImageAssetSource(ctx, assetID)
	if err != nil || resolved.ID != owner.ID {
		t.Fatalf("asset source = %#v, %v; want owner %q", resolved, err, owner.ID)
	}

	if err := store.DeleteSource(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImageAssetSource(ctx, assetID); err == nil {
		t.Fatal("foreign image node must not authorize another source's asset")
	}
	textOnlyAssetID := textOnly.ID + "_embedded-1"
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "text-only-node", SourceID: textOnly.ID, Type: "paragraph", Metadata: map[string]string{MetaImageAssetID: textOnlyAssetID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImageAssetSource(ctx, textOnlyAssetID); err == nil {
		t.Fatal("non-image node must not authorize an image asset")
	}
	if _, err := store.FindImageAssetSource(ctx, fallbackOnly.ID); err == nil {
		t.Fatal("non-image source ID must not authorize an asset without an image node")
	}
}

func TestSearchImagesDropsCrossSourceImageAssetID(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := Source{ID: "document-source", Kind: SourceKindDOCX, URI: "file://design.docx", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}
	if err := store.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "image-node",
		SourceID: source.ID,
		Type:     NodeTypeImage,
		Text:     "gateway topology architecture diagram",
		Metadata: map[string]string{MetaImageAssetID: "other-source_embedded-image"},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "gateway topology", OwnerID: "owner", TenantID: "tenant", Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Media != nil {
		t.Fatalf("cross-source asset ID survived image search: %#v", results)
	}
}

func TestSearchImagesDoesNotLoseImageBehindTextNodeCandidateWindow(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 8; i++ {
		sourceID := "text-source-" + string(rune('a'+i))
		if err := store.SaveSource(ctx, Source{ID: sourceID, Kind: SourceKindText, URI: "memory://" + sourceID, OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "text-node-" + string(rune('a'+i)), SourceID: sourceID, Type: "paragraph", Text: "architecture gateway deployment diagram reference " + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveSource(ctx, Source{ID: "image-source", Kind: SourceKindImage, URI: "file://gateway.png", OwnerID: "owner", TenantID: "tenant", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "image-node", SourceID: "image-source", Type: NodeTypeImage, Title: "Gateway diagram", Text: "architecture gateway deployment diagram"}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "architecture gateway deployment diagram", OwnerID: "owner", TenantID: "tenant", Limit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].NodeID != "image-node" {
		t.Fatalf("image results = %#v; want image-node", results)
	}
}

func TestSearchImagesRespectsSearchFilters(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, source := range []Source{
		{ID: "tenant-a", Kind: SourceKindImage, URI: "file://a.png", OwnerID: "owner-a", TenantID: "tenant-a", ProjectPath: "project-a", Status: StatusParsed},
		{ID: "tenant-b", Kind: SourceKindImage, URI: "file://b.png", OwnerID: "owner-b", TenantID: "tenant-b", ProjectPath: "project-b", Status: StatusParsed},
	} {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDocumentNode(ctx, DocumentNode{ID: source.ID + "-node", SourceID: source.ID, Type: NodeTypeImage, Text: "private architecture diagram"}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "architecture diagram", OwnerID: "owner-a", TenantID: "tenant-a", ProjectPath: "project-a", Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Source.ID != "tenant-a" {
		t.Fatalf("filtered image results = %#v; want only tenant-a", results)
	}
}
