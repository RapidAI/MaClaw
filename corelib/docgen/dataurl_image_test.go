package docgen

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	gopdf "github.com/VantageDataChat/GoPDF2"
	"golang.org/x/image/bmp"
)

// writeJPEGDataURL encodes a solid picture as a Markdown-ready data URL, which
// is how the desktop companion hands figures to this package.
func writeJPEGDataURL(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// A figure carried as a data URL must reach the PDF as pixels. The duplicate
// guard means this is also the test for "the image was not silently dropped":
// the old behaviour produced a valid PDF with no picture in it, which every
// weaker assertion (file size, no error, alt text absent) would have passed.
func TestGenerateEmbedsADataURLImage(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	content := "## 说明\n\n![](" + writeJPEGDataURL(t, 320, 240) + ")\n\n图中给出结果。\n"
	data, err := gen.Generate(Spec{
		Title:     "导出测试",
		Brand:     "MaClaw",
		PaperSize: "A4",
		Content:   content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("DCTDecode")) != 1 {
		t.Fatalf("data URL image was dropped: DCTDecode occurrences = %d, want 1", bytes.Count(data, []byte("DCTDecode")))
	}
	text, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var pages strings.Builder
	for _, item := range text {
		pages.WriteString(item.Text)
	}
	// The raw base64 must not leak into the page as text.
	if strings.Contains(pages.String(), "base64") {
		t.Fatalf("data URL leaked into the page text: %s", pages.String())
	}
}

// Two figures on one page must each appear. They differ on purpose: identical
// bytes become one shared XObject, which is correct PDF behaviour rather than a
// dropped image, so counting XObjects with a repeated source would be testing
// the deduplicator and not this package.
func TestGenerateEmbedsTwoDataURLImages(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	content := "## 一\n\n![](" + writeJPEGDataURL(t, 200, 150) + ")\n\n## 二\n\n![](" + writeJPEGDataURL(t, 240, 160) + ")\n"
	data, err := gen.Generate(Spec{Title: "t", Brand: "b", PaperSize: "A4", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(data, []byte("DCTDecode")); got != 2 {
		t.Fatalf("embedded figures = %d, want 2", got)
	}
}

// The same figure used twice is written once. Asserting this pins the dedup
// that makes a repeated figure cheap, so a future change to per-occurrence
// embedding is noticed rather than absorbed silently.
func TestGenerateSharesOneXObjectForARepeatedDataURL(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	figure := writeJPEGDataURL(t, 200, 150)
	content := "## 一\n\n![](" + figure + ")\n\n## 二\n\n![](" + figure + ")\n"
	data, err := gen.Generate(Spec{Title: "t", Brand: "b", PaperSize: "A4", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(data, []byte("DCTDecode")); got != 1 {
		t.Fatalf("repeated figure produced %d XObjects, want 1 shared", got)
	}
}

// Length limits guard against pasted blobs. A figure is not one, so a picture
// whose bytes exceed the base64-run threshold must still export.
func TestValidateContentIgnoresDataURLPayload(t *testing.T) {
	figure := writeJPEGDataURL(t, 900, 900)
	if longestSuspiciousBase64Run(dataURLViewForValidation("![]("+figure+")")) >= 8192 {
		t.Fatal("data URL payload still trips the base64 guard")
	}
	if err := ValidateContent("## 图\n\n![](" + figure + ")\n"); err != nil {
		t.Fatalf("a legitimate figure was rejected: %v", err)
	}
}

// Where a figure sits in the sentence must not decide whether it exports. The
// renderer matches images per whole line, so a mid-sentence figure used to
// stay uncollapsed — and then failed the base64 guard purely for being large,
// while the same picture on its own line exported fine.
func TestValidateContentCollapsesAFigureAnywhereOnTheLine(t *testing.T) {
	figure := writeJPEGDataURL(t, 900, 700)
	forms := map[string]string{
		"独占一行":   "看图\n\n![](" + figure + ")\n\n结束",
		"写在句中":   "看图 ![](" + figure + ") 结束",
		"列表项内":   "- 看图 ![](" + figure + ")",
		"带 alt":  "看图 ![说明](" + figure + ") 结束",
		"HTML标签": `<p><img src="` + figure + `"/></p>`,
		"尖括号目标":  "看图 ![](<" + figure + ">) 结束",
	}
	for name, content := range forms {
		if collapsed := dataURLViewForValidation(content); !strings.Contains(collapsed, "<") || len(collapsed) > 400 {
			t.Fatalf("%s was not collapsed: %d bytes", name, len(collapsed))
		}
		if err := ValidateContent(content); err != nil {
			t.Fatalf("%s was rejected as a payload: %v", name, err)
		}
	}
}

// The guard must keep working for the thing it was written for: base64 that is
// not inside an image target.
func TestValidateContentStillRejectsPastedBase64(t *testing.T) {
	blob := strings.Repeat("QUJD", 3000)
	// A data prefix is not enough: the suspicious run has to stay counted when
	// it is prose rather than an image target.
	if err := ValidateContent("正文\n\n" + blob + "\n"); err == nil {
		t.Fatal("pasted base64 outside an image must stay rejected")
	}
}

// A data URL that does not decode to a picture must fall back to the honest
// "image not found" text rather than crash or emit a broken file.
func TestGenerateReportsAnUndecodableDataURL(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	content := "## 图\n\n![ broken](data:image/png;base64,not-a-picture)\n"
	data, err := gen.Generate(Spec{Title: "t", Brand: "b", PaperSize: "A4", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("DCTDecode")) {
		t.Fatal("undecodable data URL was embedded anyway")
	}
	text, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for _, item := range text {
		got.WriteString(item.Text)
	}
	if !strings.Contains(got.String(), "broken") {
		t.Fatalf("the alt text should survive so the reader knows what is missing: %s", got.String())
	}
}

// The extension drives both the decoded file's name and the placeholder shown
// for it. "image/png;base64" is the whole metadata; the media type is the part
// before the first semicolon, and taking the rest yields "".
func TestDataURLExtensionReadsTheMediaTypePastItsParameters(t *testing.T) {
	cases := map[string]string{
		"data:image/png;base64,AAAA":  ".png",
		"data:image/jpeg;base64,AAAA": ".jpg",
		"data:image/gif;base64,AAAA":  ".gif",
		"data:image/png,AAAA":         ".png",
		"data:image/tiff;base64,AAAA": "",
	}
	for raw, want := range cases {
		if got := dataURLExtension(raw); got != want {
			t.Fatalf("dataURLExtension(%q) = %q, want %q", raw, got, want)
		}
	}
	if !strings.Contains(dataURLPlaceholder("data:image/png;base64,AAAA"), "image/png") {
		t.Fatalf("placeholder lost its media type: %q", dataURLPlaceholder("data:image/png;base64,AAAA"))
	}
}

// Decoded images are temporary, and the temp directory is theirs alone. Nothing
// removes files the process wrote unless this package does, so a long-lived
// app exporting every hour would keep every figure it ever rendered.
func TestGenerateRemovesItsDecodedImages(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	scratch := newDataURLScratch()
	if _, ok := scratch.resolve(writeJPEGDataURL(t, 240, 180)); !ok {
		t.Fatal("resolve failed")
	}
	dir, err := scratch.dirFor()
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("expected one decoded file, got %d (%v)", len(entries), err)
	}
	scratch.cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("scratch directory survived cleanup: %v", err)
	}
}

// A render must leave nothing behind even when it fails part-way, which is
// when a stray directory is most likely: nobody reaches the happy path's end.
func TestGenerateLeavesNoImagesBehind(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	before := countDocgenScratchDirs(t)
	if _, err := gen.Generate(Spec{
		Title:     "t",
		Brand:     "b",
		PaperSize: "A4",
		Content:   "## 图\n\n![](" + writeJPEGDataURL(t, 200, 150) + ")\n",
	}); err != nil {
		t.Fatal(err)
	}
	if after := countDocgenScratchDirs(t); after != before {
		t.Fatalf("export left %d scratch directory(s) behind", after-before)
	}
}

// countDocgenScratchDirs counts this package's own temporary image directories.
func countDocgenScratchDirs(t *testing.T) int {
	t.Helper()
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "docgen-images-") {
			n++
		}
	}
	return n
}

// The companion can hand over a BMP figure. GoPDF cannot embed those bytes, so
// they are re-encoded to PNG; without that, the export reports the picture as
// missing even though the reader can see it on screen.
func TestGenerateEmbedsAFigureThatNeedsReencoding(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, testSolidImage(160, 120)); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/bmp;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	ext, data, ok := splitDataURL(dataURL)
	if !ok || ext != ".png" {
		t.Fatalf("bmp should re-encode to png: ext=%q ok=%v", ext, ok)
	}
	if !strings.HasPrefix(string(data), "\x89PNG") {
		t.Fatal("the re-encoded bytes are not PNG")
	}
	pdf, err := gen.Generate(Spec{
		Title:     "t",
		Brand:     "b",
		PaperSize: "A4",
		Content:   "## 图\n\n![](" + dataURL + ")\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A PNG travels in a PDF as a Flate stream. Asserting it keeps anyone from
	// "simplifying" this back to a rejected format without noticing.
	if !strings.Contains(string(pdf), "FlateDecode") {
		t.Fatal("re-encoded figure was not embedded")
	}
}

// An oversized figure must be refused before its bytes are materialised.
//
// Base64 is four bytes of text for every three of image, so decoding is the
// expensive step — and it is also the allocation this bound exists to avoid.
// Measuring cost here is the point: a check placed after DecodeString would
// still have paid for the whole picture.
func TestReencodingRefusesAnOversizedFigureBeforeDecoding(t *testing.T) {
	// Enough that three quarters of it — what the base64 will decode to — is
	// past the bound. Adding a fixed amount instead would leave the payload
	// under it, since every four characters collapse to three bytes.
	bodyLen := maxReencodeSourceBytes*4/3 + 64
	var body strings.Builder
	body.WriteString("data:image/bmp;base64,")
	body.WriteString(strings.Repeat("A", bodyLen))
	payload := body.String()

	// Building that string costs tens of megabytes on its own, so the payload
	// is built before the measurement begins. Otherwise the test fails for
	// having constructed its own input.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, _, ok := splitDataURL(payload)
	runtime.ReadMemStats(&after)
	if ok {
		t.Fatal("an oversized figure was accepted")
	}
	// TotalAlloc counts every allocation, whether or not it has since been
	// collected, so it is what answers "did this cost anything". HeapAlloc
	// would not: the decoded bytes become garbage on the way out and often
	// read back as zero.
	if grew := int64(after.TotalAlloc) - int64(before.TotalAlloc); grew > 1<<20 {
		t.Fatalf("refusing it still allocated %.1fMB; the bound is too late", float64(grew)/1048576)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("refusing it took %v; the bound is too late", elapsed)
	}
}

// The bound must not reject what the companion is allowed to send. It permits
// a single figure of 24MB, so a figure of that size has still to be accepted.
func TestReencodingAcceptsAFigureAtTheCompanionLimit(t *testing.T) {
	pixels := 2000 * 1500 // well inside maxReencodePixels
	if int64(pixels) > maxReencodePixels {
		t.Fatal("test assumption broken: sample exceeds the pixel bound")
	}
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, testSolidImage(2000, 1500)); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/bmp;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(buf.Bytes()) >= maxReencodeSourceBytes {
		t.Fatalf("sample %d bytes is past the %d byte bound; fix the test", buf.Len(), maxReencodeSourceBytes)
	}
	ext, data, ok := splitDataURL(dataURL)
	if !ok || ext != ".png" {
		t.Fatalf("a figure inside the bound was refused: ext=%q ok=%v", ext, ok)
	}
	if !strings.HasPrefix(string(data), "\x89PNG") {
		t.Fatal("accepted figure was not re-encoded to PNG")
	}
}

// A re-encode must not widen what this package accepts: bytes that are not a
// picture at all still have to be refused.
func TestReencodingRejectsBytesThatAreNotAnImage(t *testing.T) {
	if _, _, ok := splitDataURL("data:image/bmp;base64," + base64.StdEncoding.EncodeToString([]byte("not a picture"))); ok {
		t.Fatal("non-image bytes were accepted for re-encoding")
	}
}

// testSolidImage is a small picture whose every pixel differs, so re-encoding
// cannot pass by producing a trivially small output.
func testSolidImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	return img
}

// splitDataURL is the boundary every embedded figure crosses; each rejection
// below is a format this package deliberately does not guess at.
func TestSplitDataURLRejectsUnsupportedForms(t *testing.T) {
	cases := map[string]string{
		"percent-encoded":  "data:image/png,%89PNG",
		"unknown type":     "data:image/tiff;base64,AAAA",
		"no base64 marker": "data:image/png,AAAA",
		"empty payload":    "data:image/png;base64,",
		"not a data url":   "/tmp/p.png",
		"no comma":         "data:image/png;base64",
	}
	for name, raw := range cases {
		if _, _, ok := splitDataURL(raw); ok {
			t.Fatalf("%s must be rejected: %s", name, raw)
		}
	}
	if ext, _, ok := splitDataURL("data:image/gif;base64,AAAA"); !ok || ext != ".gif" {
		t.Fatalf("gif support broken: ext=%q ok=%v", ext, ok)
	}
}
