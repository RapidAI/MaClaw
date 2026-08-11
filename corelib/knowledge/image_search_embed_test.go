package knowledge

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func knowledgeImageMarkerJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return out.Bytes()
}

func TestFormatKBImageMarkerDoesNotExposeOriginalPath(t *testing.T) {
	thumbnail := knowledgeImageMarkerJPEG(t, 1, 1)
	marker := FormatKBImageMarker(&SearchResultImageEmbed{
		AssetID: "asset_1",
		DataURL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbnail),
	})
	if !strings.HasPrefix(marker, "[KB_IMAGE:asset_1|data:image/jpeg;base64,") {
		t.Fatalf("marker = %q", marker)
	}
	if parsed := ParseKBImageMarker(marker); parsed == nil || parsed.AssetID != "asset_1" {
		t.Fatalf("parsed marker = %#v", parsed)
	}
}

func TestEmbedImageThumbForSearchResultRejectsNonThumbnailAndOversizedDimensions(t *testing.T) {
	assetBaseDir := t.TempDir()
	assetID := "safe_asset"
	assetDir := filepath.Join(assetBaseDir, assetID)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	result := SearchResult{NodeType: NodeTypeImage, Source: Source{ID: assetID, Kind: SourceKindImage}}

	if err := os.WriteFile(filepath.Join(assetDir, "thumb_120.jpg"), []byte("not jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := EmbedImageThumbForSearchResult(result, assetBaseDir); got != nil {
		t.Fatalf("invalid thumbnail embed = %#v", got)
	}

	if err := os.WriteFile(filepath.Join(assetDir, "thumb_120.jpg"), knowledgeImageMarkerJPEG(t, ThumbSize+1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := EmbedImageThumbForSearchResult(result, assetBaseDir); got != nil {
		t.Fatalf("oversized thumbnail embed = %#v", got)
	}
}

func TestEmbedImageThumbForSearchResultRejectsUnsafeAssetMetadata(t *testing.T) {
	root := t.TempDir()
	outsideAssetDir := filepath.Join(filepath.Dir(root), "outside")
	if err := os.MkdirAll(outsideAssetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideAssetDir, "thumb_120.jpg"), knowledgeImageMarkerJPEG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	result := SearchResult{
		NodeType: NodeTypeImage,
		Source:   Source{ID: "..", Kind: SourceKindImage},
		Media:    &SearchResultMedia{AssetID: "../outside"},
	}
	if got := EmbedImageThumbForSearchResult(result, root); got != nil {
		t.Fatalf("unsafe metadata produced an embed: %#v", got)
	}
}

func TestEmbedImageThumbForSearchResultDoesNotGuessAnAssetID(t *testing.T) {
	root := t.TempDir()
	assets, err := NewImageAssetManager(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assets.SaveImageFromBytes("document-source_embedded-1", knowledgeImageMarkerJPEG(t, 1, 1), ".jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.SaveImageFromBytes("document-source_node-id", knowledgeImageMarkerJPEG(t, 1, 1), ".jpg"); err != nil {
		t.Fatal(err)
	}
	baseDir := assets.BaseDir()
	for _, result := range []SearchResult{
		{NodeType: NodeTypeImage, NodeID: "embedded-1", Source: Source{ID: "document-source", Kind: SourceKindDOCX}},
		{NodeType: NodeTypeImage, NodeID: "node-id", Source: Source{ID: "document-source", Kind: SourceKindDOCX}, Media: &SearchResultMedia{AssetID: "foreign-source_asset"}},
		{NodeType: NodeTypeImage, Source: Source{ID: "document-source", Kind: SourceKindDOCX}, Media: &SearchResultMedia{AssetID: ""}},
	} {
		if got := EmbedImageThumbForSearchResult(result, baseDir); got != nil {
			t.Fatalf("guessed or foreign asset was embedded: %#v", got)
		}
	}

	result := SearchResult{NodeType: NodeTypeImage, Source: Source{ID: "document-source", Kind: SourceKindDOCX}, Media: &SearchResultMedia{AssetID: "document-source_embedded-1"}}
	if got := EmbedImageThumbForSearchResult(result, baseDir); got == nil || got.AssetID != "document-source_embedded-1" {
		t.Fatalf("recorded embedded asset was not embedded: %#v", got)
	}
}

func TestKBImageMarkerRejectsNonJPEGAndOversizedDimensions(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("not jpeg"),
		knowledgeImageMarkerJPEG(t, ThumbSize+1, 1),
	} {
		marker := "[KB_IMAGE:asset_1|data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data) + "]"
		if got := ParseKBImageMarker(marker); got != nil {
			t.Fatalf("ParseKBImageMarker accepted invalid thumbnail: %#v", got)
		}
	}
}

func TestParseKBImageMarkerRejectsPathAndUntrustedData(t *testing.T) {
	for _, marker := range []string{
		`[KB_IMAGE:asset_1|data:image/jpeg;base64,YWJj|C:\\private\\original.jpg]`,
		`[KB_IMAGE:../../outside|data:image/jpeg;base64,YWJj]`,
		`[KB_IMAGE:asset_1|https://tracker.example/image.jpg]`,
		`[KB_IMAGE:asset_1|data:text/html;base64,PHNjcmlwdD4=]`,
		`[KB_IMAGE:asset_1|data:image/jpeg;base64,%%%]`,
	} {
		if got := ParseKBImageMarker(marker); got != nil {
			t.Fatalf("ParseKBImageMarker(%q) = %#v, want nil", marker, got)
		}
	}
}

func TestKBImageMarkerRejectsOversizedThumbnailData(t *testing.T) {
	// The encoded payload is syntactically valid base64 but exceeds the same
	// bound used when loading a generated thumbnail from the asset store.
	overlarge := strings.Repeat("A", ((MaxKBImageMarkerDataBytes+1+2)/3)*4)
	marker := "[KB_IMAGE:asset_1|data:image/jpeg;base64," + overlarge + "]"
	if got := ParseKBImageMarker(marker); got != nil {
		t.Fatalf("ParseKBImageMarker accepted oversized data: %#v", got)
	}
	if got := FormatKBImageMarker(&SearchResultImageEmbed{AssetID: "asset_1", DataURL: "data:image/jpeg;base64," + overlarge}); got != "" {
		t.Fatalf("FormatKBImageMarker accepted oversized data: %q", got)
	}
}

func TestIsSafeImageAssetIDMatchesMarkerProtocol(t *testing.T) {
	for _, assetID := range []string{"ksrc_123_abcdef", "source_node", "asset-123"} {
		if !IsSafeImageAssetID(assetID) {
			t.Fatalf("safe asset ID %q rejected", assetID)
		}
	}
	for _, assetID := range []string{"asset.123", "asset space", "asset@123", "../secret", " asset-123", "asset-123 "} {
		if IsSafeImageAssetID(assetID) {
			t.Fatalf("unsafe asset ID %q accepted", assetID)
		}
	}
}
