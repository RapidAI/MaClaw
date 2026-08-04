package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMobileDocumentPrepareStoredSourcePolicy(t *testing.T) {
	raw := bytes.Repeat([]byte("plain text row\n"), 4096)
	stored, encoding, err := mobileDocumentPrepareStoredSource("notes.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	if encoding != "gzip" || len(stored) >= len(raw) {
		t.Fatalf("txt encoding=%q stored=%d raw=%d", encoding, len(stored), len(raw))
	}
	decoded, err := mobileDecodeStoredDocument(stored, encoding)
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("decode err=%v equal=%v", err, bytes.Equal(decoded, raw))
	}

	for _, name := range []string{"report.docx", "sheet.xlsx", "slides.pptx", "bundle.zip", "bundle.7z", "bundle.zst", "bundle.lz4", "bundle.cab"} {
		got, gotEncoding, gotErr := mobileDocumentPrepareStoredSource(name, raw)
		if gotErr != nil || gotEncoding != "" || !bytes.Equal(got, raw) {
			t.Fatalf("%s should stay verbatim: encoding=%q err=%v", name, gotEncoding, gotErr)
		}
	}
}

func TestMobileDocumentLooksPrecompressedByMagic(t *testing.T) {
	cases := [][]byte{
		{'P', 'K', 0x03, 0x04},
		{'R', 'a', 'r', '!', 0x1a, 0x07},
		{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c},
		{0x1f, 0x8b},
		{'B', 'Z', 'h'},
		{0xfd, '7', 'z', 'X', 'Z', 0x00},
		{0x28, 0xb5, 0x2f, 0xfd},
		{0x04, 0x22, 0x4d, 0x18},
	}
	for _, head := range cases {
		if !mobileDocumentLooksPrecompressed("renamed.bin", head) {
			t.Fatalf("magic %x should bypass recompression", head)
		}
	}
	if mobileDocumentLooksPrecompressed("notes.txt", []byte("hello")) {
		t.Fatal("plain text should remain compressible")
	}
}

func TestMobilePersistUploadedDocumentStreamsAndBounds(t *testing.T) {
	root := t.TempDir()
	t.Setenv(mobileBlobDirEnv, root)
	raw := bytes.Repeat([]byte("legacy document row\n"), 8192)
	path, storedSize, originalSize, encoding, _, err := mobilePersistUploadedDocument("owner", "task", bytes.NewReader(raw), true)
	if err != nil {
		t.Fatal(err)
	}
	if encoding != "gzip" || storedSize >= originalSize || originalSize != len(raw) {
		t.Fatalf("encoding=%q stored=%d original=%d", encoding, storedSize, originalSize)
	}
	stored, err := os.ReadFile(mustMobileBlobAbsPath(t, path))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(stored))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("decode err=%v equal=%v", err, bytes.Equal(decoded, raw))
	}
}

func mustMobileBlobAbsPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := mobileBlobAbsPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestMobileDecodeStoredDocumentRejectsSizeMismatch(t *testing.T) {
	raw := bytes.Repeat([]byte("x"), 2048)
	stored, encoding, err := mobileDocumentPrepareStoredSource("a.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mobileDecodeStoredDocumentBounded(stored, encoding, len(raw)-1); err == nil {
		t.Fatal("expected decoded-size mismatch")
	}
}

func TestMobileStoredDownloadRejectsCorruptSizeBeforeHeaders(t *testing.T) {
	raw := bytes.Repeat([]byte("download row\n"), 512)
	stored, encoding, err := mobileDocumentPrepareStoredSource("a.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if mobileWriteStoredOriginalHTTP(rec, "text/plain", "a.txt", stored, "", encoding, len(raw)-1) {
		t.Fatal("mismatched original size should be rejected")
	}
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("response was committed: code=%d bytes=%d", rec.Code, rec.Body.Len())
	}
}

func TestMobileStoredDownloadStreamsOriginal(t *testing.T) {
	raw := bytes.Repeat([]byte("download row\n"), 512)
	stored, encoding, err := mobileDocumentPrepareStoredSource("a.txt", raw)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if !mobileWriteStoredOriginalHTTP(rec, "text/plain", "a.txt", stored, "", encoding, len(raw)) {
		t.Fatal("download failed")
	}
	if !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("download mismatch: got=%d want=%d", rec.Body.Len(), len(raw))
	}
}

func TestMobileDraftUnsupportedPreviewMarkdown(t *testing.T) {
	if got := mobileDraftUnsupportedPreviewMarkdown("model.bin"); got != "不支持内容预览" {
		t.Fatalf("got %q", got)
	}
	if got := mobileDraftWorkingText(mobileDocumentDraftRecord{Markdown: mobileDocumentUnsupportedPreviewText}); got != "" {
		t.Fatalf("unsupported preview leaked into AI working text: %q", got)
	}
}
