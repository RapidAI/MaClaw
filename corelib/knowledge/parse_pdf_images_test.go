package knowledge

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestExtractPDFImagesNativeExtractsDecodableEmbeddedImage(t *testing.T) {
	imageData := nativePDFTestJPEG(t)
	pdfPath := filepath.Join(t.TempDir(), "embedded-image.pdf")
	writePDFWithEmbeddedImage(t, pdfPath, imageData)

	nodes, images, err := extractPDFImagesNative(Source{ID: "pdf-source"}, pdfPath, []DocumentNode{{Page: 1, Text: "Gateway topology explanation"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || len(images) != 1 {
		t.Fatalf("native PDF images: nodes=%#v images=%d", nodes, len(images))
	}
	node := nodes[0]
	if node.Type != NodeTypeImage || node.Page != 1 || node.Metadata[MetaImageFormat] != "jpeg" || node.Metadata["context_before"] != "Gateway topology explanation" {
		t.Fatalf("unexpected image node: %#v", node)
	}
	data := images[node.Metadata["_image_bytes_key"]]
	if len(data) == 0 {
		t.Fatalf("missing native image bytes for node: %#v", node)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" {
		t.Fatalf("native PDF image must be a decodable JPEG: format=%q err=%v", format, err)
	}
}

func TestPDFImageExtractionUsesOwnedSnapshot(t *testing.T) {
	root := t.TempDir()
	pdfPath := filepath.Join(root, "snapshot-image.pdf")
	writePDFWithEmbeddedImage(t, pdfPath, nativePDFTestJPEG(t))

	input, err := prepareKnowledgeDocumentInput(pdfPath, SourceKindPDF)
	if err != nil || input == nil {
		t.Fatalf("prepare PDF snapshot = %#v, %v", input, err)
	}
	defer input.close()
	if err := os.Remove(pdfPath); err != nil {
		t.Fatal(err)
	}

	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	store.SetImageAssetManager(assets)
	nodes := store.ExtractAndProcessDocumentImages(
		context.Background(), Source{ID: "snapshot-pdf-image", Kind: SourceKindPDF, Title: "Snapshot image"}, input.path, SourceKindPDF, nil,
	)
	if len(nodes) != 1 || nodes[0].Metadata[MetaImageAssetID] == "" {
		t.Fatalf("PDF image extraction must use the owned snapshot: %#v", nodes)
	}
}

func TestExtractPDFImagesNativeSkipsUnrecognizableImageBytes(t *testing.T) {
	pdfPath := filepath.Join(t.TempDir(), "not-a-pdf.pdf")
	if err := os.WriteFile(pdfPath, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes, images, err := extractPDFImagesNative(Source{ID: "pdf-source"}, pdfPath, nil)
	if err != nil {
		t.Fatalf("invalid PDF native extractor must fail closed without a crash: %v", err)
	}
	if len(nodes) != 0 || len(images) != 0 {
		t.Fatalf("invalid PDF must not yield image assets: nodes=%#v images=%d", nodes, len(images))
	}
}

func TestSafeExtractPDFImagesRecoversParserPanic(t *testing.T) {
	_, err := safeExtractPDFImagesWith([]byte("fixture"), func([]byte) (map[int][]gopdf2.ExtractedImage, error) {
		panic("malformed image stream")
	})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("image parser panic error = %v", err)
	}
}

func TestSortedPDFImagePagesUsesDocumentOrder(t *testing.T) {
	pages := map[int][]gopdf2.ExtractedImage{
		4: nil,
		0: nil,
		2: nil,
	}
	if got, want := sortedPDFImagePages(pages), []int{0, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted PDF image pages = %#v, want %#v", got, want)
	}
}

func nativePDFTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 2), B: 180, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 88}); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() < 500 {
		t.Fatalf("test JPEG unexpectedly too small: %d", encoded.Len())
	}
	return encoded.Bytes()
}

func writePDFWithEmbeddedImage(t *testing.T, path string, imageData []byte) {
	t.Helper()
	img, err := gopdf2.ImageHolderByBytes(imageData)
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.ImageByHolder(img, 40, 40, &gopdf2.Rect{W: 160, H: 160}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}
