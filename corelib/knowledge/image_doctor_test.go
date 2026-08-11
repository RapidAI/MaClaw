package knowledge

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorUsesRecordedAssetIDForEmbeddedImageThumbnails(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dataDir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	if err := store.SaveSource(ctx, Source{ID: "document-source", Kind: SourceKindDOCX, URI: "file://architecture.docx", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	const assetID = "document-source_media-image-7"
	if _, err := assets.SaveImageFromBytes(assetID, doctorTinyPNG(t), ".png"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "generated-node-id",
		SourceID: "document-source",
		Type:     NodeTypeImage,
		Text:     "Architecture diagram",
		Metadata: map[string]string{MetaImageAssetID: assetID},
	}); err != nil {
		t.Fatal(err)
	}

	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if doctorFindingCount(doctor, "missing_image_thumbnails") != 0 {
		t.Fatalf("embedded image thumbnail was incorrectly reported missing: %#v", doctor.Findings)
	}

	if err := os.Remove(assets.ThumbPath(assetID)); err != nil {
		t.Fatal(err)
	}
	doctor, err = store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if doctorFindingCount(doctor, "missing_image_thumbnails") != 1 {
		t.Fatalf("missing embedded image thumbnail was not reported: %#v", doctor.Findings)
	}
	if err := os.Remove(assets.OriginalPath(assetID, ".png")); err != nil {
		t.Fatal(err)
	}
	doctor, err = store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if doctorFindingCount(doctor, "missing_image_assets") != 1 {
		t.Fatalf("missing asset resolved by ID was not reported: %#v", doctor.Findings)
	}
}

func TestDoctorDoesNotNormalizeWhitespacePaddedImageAssetID(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dataDir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := NewImageAssetManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	if err := store.SaveSource(ctx, Source{ID: "image-source", Kind: SourceKindImage, URI: "file://diagram.png", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.SaveImageFromBytes("image-source", doctorTinyPNG(t), ".png"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "image-node",
		SourceID: "image-source",
		Type:     NodeTypeImage,
		Metadata: map[string]string{MetaImageAssetID: " image-source "},
	}); err != nil {
		t.Fatal(err)
	}
	doctor, err := store.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if doctorFindingCount(doctor, "missing_image_assets") != 0 || doctorFindingCount(doctor, "missing_image_thumbnails") != 0 {
		t.Fatalf("doctor treated the rejected asset ID as a missing managed asset: %#v", doctor.Findings)
	}
}

func doctorTinyPNG(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
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

func doctorFindingCount(result DoctorResult, code string) int {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return finding.Count
		}
	}
	return 0
}
