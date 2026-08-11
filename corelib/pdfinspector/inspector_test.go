package pdfinspector

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestDetectTextBasedPDF(t *testing.T) {
	result, err := Detect(makePDF(textPage("Alpha", "Bravo", "Charlie")))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != TextBased || result.PagesWithText != 1 || result.OCRRecommended {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDetectTextBasedPDFWithSingleLongTextOperation(t *testing.T) {
	result, err := Detect(makePDF(textPage("A deliberately long native text operation should not be mistaken for a page-number overlay.")))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != TextBased || result.OCRRecommended || result.Pages[0].VisibleTextRunes < 20 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDetectScannedPDF(t *testing.T) {
	result, err := Detect(makePDF(imagePage()))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != Scanned || !result.OCRRecommended || !sameInts(result.PagesNeedingOCR, []int{1}) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Pages[0].OCRReason != "scanned" {
		t.Fatalf("OCRReason = %q", result.Pages[0].OCRReason)
	}
}

func TestDetectImageBasedPDFWithTinyOverlay(t *testing.T) {
	result, err := Detect(makePDF(imageTextPage("1")))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != ImageBased || !sameInts(result.PagesNeedingOCR, []int{1}) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDetectWithOptionsHonorsMinTextItems(t *testing.T) {
	result, err := DetectWithOptions(makePDF(textPage("Alpha", "Bravo", "Charlie")), Options{MinTextItems: 32, MinTextRunes: 1})
	if err != nil {
		t.Fatalf("DetectWithOptions: %v", err)
	}
	if result.PDFType != ImageBased || !result.OCRRecommended || result.Pages[0].OCRReason != "insufficient_text" {
		t.Fatalf("MinTextItems must affect classification: %#v", result)
	}
}

func TestDetectMixedPDF(t *testing.T) {
	result, err := Detect(makePDF(textPage("Alpha", "Bravo", "Charlie"), imagePage()))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != Mixed || result.PagesWithText != 1 || !sameInts(result.PagesNeedingOCR, []int{2}) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDetectEmptyPageRoutesToOCR(t *testing.T) {
	result, err := Detect(makePDF(testPage{}))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.PDFType != Scanned || !result.OCRRecommended || !sameInts(result.PagesNeedingOCR, []int{1}) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Pages[0].OCRReason != "no_text" {
		t.Fatalf("OCRReason = %q, want no_text", result.Pages[0].OCRReason)
	}
}

func TestDetectRejectsInvalidPDF(t *testing.T) {
	if _, err := Detect([]byte("not a PDF")); err == nil {
		t.Fatal("Detect accepted invalid data")
	}
}

func TestDetectRejectsExcessivePageCountBeforeExtraction(t *testing.T) {
	_, err := detectWithExtractors(
		makePDF(textPage("fixture")),
		DefaultOptions,
		func([]byte) (int, error) { return MaxPages + 1, nil },
		func([]byte) (map[int][]gopdf.ExtractedText, error) {
			t.Fatal("text extraction must not run")
			return nil, nil
		},
		func([]byte) (map[int][]gopdf.ExtractedImage, error) {
			t.Fatal("image extraction must not run")
			return nil, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "too many pages") {
		t.Fatalf("excessive page count error = %v", err)
	}
}

func TestDetectRecoversParserPanic(t *testing.T) {
	_, err := detectWithExtractors(
		makePDF(textPage("Alpha", "Bravo", "Charlie")),
		DefaultOptions,
		gopdf.GetSourcePDFPageCountFromBytes,
		func([]byte) (map[int][]gopdf.ExtractedText, error) { panic("malformed text stream") },
		func([]byte) (map[int][]gopdf.ExtractedImage, error) { return nil, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "parser panicked") {
		t.Fatalf("parser panic error = %v", err)
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type testPage struct {
	resources string
	content   string
	extra     []pdfObject
}

type pdfObject struct{ body string }

func textPage(lines ...string) testPage {
	var body strings.Builder
	body.WriteString("BT /F1 12 Tf 72 720 Td ")
	for _, line := range lines {
		fmt.Fprintf(&body, "(%s) Tj 0 -16 Td ", line)
	}
	body.WriteString("ET")
	return testPage{resources: "/Font << /F1 3 0 R >>", content: body.String()}
}

func imagePage() testPage { return imageTextPage("") }

func imageTextPage(overlay string) testPage {
	content := "q 200 0 0 200 0 0 cm /Im1 Do Q"
	if overlay != "" {
		content += " BT /F1 10 Tf 10 10 Td (" + overlay + ") Tj ET"
	}
	resources := "/XObject << /Im1 4 0 R >>"
	if overlay != "" {
		resources += " /Font << /F1 3 0 R >>"
	}
	return testPage{
		resources: resources,
		content:   content,
		extra:     []pdfObject{{body: "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 3 >>\nstream\n\xFF\xFF\xFF\nendstream"}},
	}
}

func makePDF(pages ...testPage) []byte {
	pageObjectNumbers := make([]int, len(pages))
	nextObject := 4
	for i, page := range pages {
		pageObjectNumbers[i] = nextObject
		nextObject += 2 + len(page.extra) // page + contents + XObjects
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [" + pageReferences(pageObjectNumbers) + "] /Count " + strconv.Itoa(len(pages)) + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, page := range pages {
		if got, want := len(objects)+1, pageObjectNumbers[i]; got != want {
			panic(fmt.Sprintf("test PDF object layout mismatch: got %d, want %d", got, want))
		}
		objects = append(objects, "") // Reserve page object first for stable references.
		objects = append(objects, page.contentStreamObject())
		for _, extra := range page.extra {
			objects = append(objects, extra.body)
		}
		contentRef := pageObjectNumbers[i] + 1
		resources := strings.ReplaceAll(page.resources, "/Im1 4 0 R", fmt.Sprintf("/Im1 %d 0 R", contentRef+1))
		objects[pageObjectNumbers[i]-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << %s >> /Contents %d 0 R >>", resources, contentRef)
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func (page testPage) contentStreamObject() string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page.content), page.content)
}

func pageReferences(numbers []int) string {
	refs := make([]string, 0, len(numbers))
	for _, number := range numbers {
		refs = append(refs, fmt.Sprintf("%d 0 R", number))
	}
	return strings.Join(refs, " ")
}
