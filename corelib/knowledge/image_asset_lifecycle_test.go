package knowledge

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestImageAssetManagerRejectsOversizedPayloadBeforeWrite(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetID := "oversized-image"
	_, err = assets.SaveImageFromBytes(assetID, make([]byte, MaxKnowledgeImageAssetBytes+1), ".png")
	if !errors.Is(err, errKnowledgeImageAssetTooLarge) {
		t.Fatalf("SaveImageFromBytes error = %v, want size-limit error", err)
	}
	if assets.HasAssets(assetID) {
		t.Fatal("oversized image must not create an asset directory")
	}
}

func TestDeleteSourceReclaimsStandaloneAndEmbeddedImageAssets(t *testing.T) {
	ctx := context.Background()
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

	for _, id := range []string{"source-1", "source-1_embedded-1", "source-1-neighbor"} {
		if _, err := assets.SaveImageFromBytes(id, []byte("not-a-decodable-image"), ".png"); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	if err := store.SaveSource(ctx, Source{ID: "source-1", Kind: SourceKindImage, URI: "file://image", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "source-1-image", SourceID: "source-1", Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: "source-1_embedded-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSource(ctx, "source-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"source-1", "source-1_embedded-1"} {
		if _, err := os.Stat(assets.AssetDir(id)); !os.IsNotExist(err) {
			t.Fatalf("asset directory %s still exists or returned unexpected error: %v", id, err)
		}
	}
	if _, err := os.Stat(assets.AssetDir("source-1-neighbor")); err != nil {
		t.Fatalf("neighbor asset must be preserved: %v", err)
	}
}

func TestDeleteSourceDoesNotNormalizeWhitespacePaddedEmbeddedAssetID(t *testing.T) {
	ctx := context.Background()
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
	for _, id := range []string{"source-1", "source-1_embedded-1"} {
		if _, err := assets.SaveImageFromBytes(id, []byte("not-a-decodable-image"), ".png"); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	if err := store.SaveSource(ctx, Source{ID: "source-1", Kind: SourceKindImage, URI: "file://image", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	// Simulate a malformed historic row that predates persistence sanitation.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, metadata_json) VALUES (?, ?, ?, ?)`, "image-node", "source-1", NodeTypeImage, `{"image_asset_id":" source-1_embedded-1 "}`); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSource(ctx, "source-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(assets.AssetDir("source-1")); !os.IsNotExist(err) {
		t.Fatalf("standalone source asset was not removed: %v", err)
	}
	if _, err := os.Stat(assets.AssetDir("source-1_embedded-1")); err != nil {
		t.Fatalf("whitespace-padded metadata deleted a canonical embedded asset: %v", err)
	}
}

func TestDeleteSourceDoesNotReclaimForeignOrNonImageAssetClaims(t *testing.T) {
	ctx := context.Background()
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

	for _, assetID := range []string{"deleting-source_foreign", "deleting-source_text-only", "neighbor-source_owned"} {
		if _, err := assets.SaveImageFromBytes(assetID, []byte("not-a-decodable-image"), ".png"); err != nil {
			t.Fatalf("save %s: %v", assetID, err)
		}
	}
	deleting := Source{ID: "deleting-source", Kind: SourceKindDOCX, URI: "file://deleting.docx", Status: StatusParsed}
	neighbor := Source{ID: "neighbor-source", Kind: SourceKindDOCX, URI: "file://neighbor.docx", Status: StatusParsed}
	for _, source := range []Source{deleting, neighbor} {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	// These rows simulate direct historic writes. Neither may be used by source
	// deletion to reclaim an asset it does not own or an asset not held by an
	// image node.
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "foreign-image", SourceID: deleting.ID, Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: "neighbor-source_owned"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "non-image", SourceID: deleting.ID, Type: "paragraph", Metadata: map[string]string{MetaImageAssetID: "deleting-source_text-only"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSource(ctx, deleting.ID); err != nil {
		t.Fatal(err)
	}
	for _, assetID := range []string{"deleting-source_foreign", "deleting-source_text-only", "neighbor-source_owned"} {
		if _, err := os.Stat(assets.AssetDir(assetID)); err != nil {
			t.Fatalf("unowned or non-image claimed asset %s was reclaimed: %v", assetID, err)
		}
	}
}

func TestDeleteSourcesByFilterAndImportBatchReclaimImageAssets(t *testing.T) {
	ctx := context.Background()
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

	for _, id := range []string{"owner-a", "owner-a_embedded", "owner-b"} {
		if _, err := assets.SaveImageFromBytes(id, []byte("not-a-decodable-image"), ".png"); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	for _, source := range []Source{
		{ID: "owner-a", Kind: SourceKindImage, URI: "file://a", OwnerID: "owner-a", TenantID: "tenant", Status: StatusParsed},
		{ID: "owner-b", Kind: SourceKindImage, URI: "file://b", OwnerID: "owner-b", TenantID: "tenant", Status: StatusParsed},
	} {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "owner-a-image", SourceID: "owner-a", Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: "owner-a_embedded"}}); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteSourcesByFilter(ctx, ListSourcesOptions{OwnerID: "owner-a", TenantID: "tenant"})
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteSourcesByFilter = %d, %v; want 1, nil", deleted, err)
	}
	for _, id := range []string{"owner-a", "owner-a_embedded"} {
		if _, err := os.Stat(assets.AssetDir(id)); !os.IsNotExist(err) {
			t.Fatalf("filtered asset directory %s still exists or returned unexpected error: %v", id, err)
		}
	}
	if _, err := os.Stat(assets.AssetDir("owner-b")); err != nil {
		t.Fatalf("other owner asset must be preserved: %v", err)
	}

	root := t.TempDir()
	imagePath := filepath.Join(root, "batch.png")
	writeLifecyclePNG(t, imagePath)
	imported, err := store.ImportDirectory(ctx, DirectoryImportRequest{RootPath: root, OwnerID: "owner-c", TenantID: "tenant", IncludeExts: []string{".png"}})
	if err != nil || imported.ImportedFiles != 1 || len(imported.Items) != 1 || imported.Items[0].SourceID == "" {
		t.Fatalf("ImportDirectory = %#v, %v", imported, err)
	}
	batchAssetID := imported.Items[0].SourceID
	if _, err := os.Stat(assets.AssetDir(batchAssetID)); err != nil {
		t.Fatalf("imported asset missing: %v", err)
	}
	result, err := store.DeleteImportBatch(ctx, ImportBatchDeleteRequest{BatchID: imported.BatchID, OwnerID: "owner-c", TenantID: "tenant"})
	if err != nil || result.DeletedSources != 1 {
		t.Fatalf("DeleteImportBatch = %#v, %v", result, err)
	}
	if _, err := os.Stat(assets.AssetDir(batchAssetID)); !os.IsNotExist(err) {
		t.Fatalf("batch asset still exists or returned unexpected error: %v", err)
	}
}

func TestImageAssetPresentationRejectsSymlinkedAssetContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a file symlink requires elevated privileges on many Windows hosts")
	}
	root := t.TempDir()
	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	assetID := "linked-image"
	if _, err := assets.SaveImageFromBytes(assetID, lifecyclePNG(t), ".png"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outside, lifecycleJPEG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	thumb := assets.ThumbPath(assetID)
	if err := os.Remove(thumb); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, thumb); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.DerivedImage(assetID, "thumbnail"); err == nil {
		t.Fatal("symlinked thumbnail must not be presented")
	}
	if _, err := ReadKnowledgeImageThumbnail(assets.BaseDir(), assetID); err == nil {
		t.Fatal("symlinked thumbnail must not be embedded")
	}

	original := assets.OriginalPath(assetID, ".png")
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, original); err != nil {
		t.Fatal(err)
	}
	if _, _, err := assets.OriginalImage(assetID); err == nil {
		t.Fatal("symlinked original must not be opened")
	}
}

func TestImageAssetHealthRejectsNonRasterAndSymlinkedContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	if err := store.SaveSource(ctx, Source{ID: "unhealthy-image", Kind: SourceKindImage, URI: "file://image", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "unhealthy-node", SourceID: "unhealthy-image", Type: NodeTypeImage, Metadata: map[string]string{MetaImageAssetID: "unhealthy-image"}}); err != nil {
		t.Fatal(err)
	}
	assetDir := assets.AssetDir("unhealthy-image")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "original.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "thumb_120.jpg"), []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	health, err := store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, finding := range health.Findings {
		counts[finding.Code] = finding.Count
	}
	if counts["missing_image_assets"] != 1 || counts["missing_image_thumbnails"] != 1 {
		t.Fatalf("unsafe image cache must be unhealthy, findings=%#v", health.Findings)
	}
}

func lifecyclePNG(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.png")
	writeLifecyclePNG(t, path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func lifecycleJPEG(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeLifecyclePNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
