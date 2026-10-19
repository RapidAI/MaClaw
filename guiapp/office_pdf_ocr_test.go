package guiapp

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestBuildUserContentPrefersVisionForImageOnlyPDF(t *testing.T) {
	calls := 0
	agent.SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-only", nil
	})
	t.Cleanup(func() { agent.SetOfficeImagePDFOCR(nil, nil) })

	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}, imagePDFPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := "看图\n" + agent.FilePathPromptPrefix + "\n" + path + "\n"
	got := buildUserContentWithPreparedLocalAttachments(prompt, nil, "openai", true, nil, nil, true, true, 0)
	if calls != 0 {
		t.Fatalf("OCR calls = %d, want 0 when the model can see images", calls)
	}
	blocks, ok := got.([]interface{})
	if !ok {
		t.Fatalf("content type = %T, want vision blocks", got)
	}
	sawText := false
	sawJPEG := false
	for _, block := range blocks {
		bm, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch bm["type"] {
		case string(imContentBlockText):
			text, _ := bm["text"].(string)
			if strings.Contains(text, "# text_source: vision") && strings.Contains(text, "[image attached]") {
				sawText = true
			}
			if strings.Contains(text, "Use OCR only for exact transcription") {
				t.Fatalf("generic OCR note overrides the picture-PDF transcript:\n%s", text)
			}
		case string(imContentBlockImageURL):
			imageURL, _ := bm["image_url"].(map[string]interface{})
			url, _ := imageURL["url"].(string)
			if strings.HasPrefix(url, "data:image/jpeg;base64,") {
				sawJPEG = true
			}
		}
	}
	if !sawText || !sawJPEG {
		t.Fatalf("vision content text=%v jpeg=%v", sawText, sawJPEG)
	}

	plain := buildUserContentWithPreparedLocalAttachments(prompt, nil, "openai", false, nil, nil, true, true, 0)
	text, ok := plain.(string)
	if !ok || calls != 1 || !strings.Contains(text, "# text_source: ocr") || !strings.Contains(text, "ocr-only") {
		t.Fatalf("text-only content type=%T calls=%d text:\n%v", plain, calls, plain)
	}
}

func TestJoinBrowserOCRTextReadsTopToBottom(t *testing.T) {
	got := joinBrowserOCRText([]browser.OCRResult{
		{Text: "  下方  ", BBox: [4]int{10, 80, 40, 12}},
		{Text: "标题", BBox: [4]int{10, 20, 30, 12}},
		{Text: "   ", BBox: [4]int{10, 40, 10, 10}},
		{Text: "右侧", BBox: [4]int{80, 20, 20, 12}},
	})
	want := "标题\n右侧\n下方"
	if got != want {
		t.Fatalf("joinBrowserOCRText = %q, want %q", got, want)
	}
	if joinBrowserOCRText(nil) != "" {
		t.Fatal("empty OCR result should be empty text")
	}
}

func mustJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 20
		img.Pix[i+1] = 40
		img.Pix[i+2] = 80
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type imagePDFPage struct {
	text          string
	jpeg          []byte
	width, height int
}

func buildImagePDF(t *testing.T, pages ...imagePDFPage) []byte {
	t.Helper()
	type built struct {
		resources string
		content   string
		extra     []string
	}
	builtPages := make([]built, len(pages))
	for i, page := range pages {
		switch {
		case len(page.jpeg) > 0:
			extra := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", page.width, page.height, len(page.jpeg), page.jpeg)
			builtPages[i] = built{
				resources: "/XObject << /Im1 0 0 R >>",
				content:   "q 200 0 0 200 72 500 cm /Im1 Do Q",
				extra:     []string{extra},
			}
		case page.text != "":
			builtPages[i] = built{
				resources: "/Font << /F1 3 0 R >>",
				content:   "BT /F1 12 Tf 72 720 Td (" + page.text + ") Tj ET",
			}
		default:
			builtPages[i] = built{content: ""}
		}
	}
	pageNums := make([]int, len(builtPages))
	next := 4
	for i, page := range builtPages {
		pageNums[i] = next
		next += 2 + len(page.extra)
	}
	refs := make([]string, len(pageNums))
	for i, n := range pageNums {
		refs[i] = fmt.Sprintf("%d 0 R", n)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [" + strings.Join(refs, " ") + "] /Count " + strconv.Itoa(len(builtPages)) + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, page := range builtPages {
		objects = append(objects, "")
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page.content), page.content))
		objects = append(objects, page.extra...)
		resources := strings.ReplaceAll(page.resources, "/Im1 0 0 R", fmt.Sprintf("/Im1 %d 0 R", pageNums[i]+2))
		objects[pageNums[i]-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << %s >> /Contents %d 0 R >>", resources, pageNums[i]+1)
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
