package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFullIntegration exercises ALL major features in a single document
// and verifies the output through multiple validation paths:
// structural integrity, text extraction, AES encryption, text
// rasterization, and password-aware extraction.
func TestFullIntegration(t *testing.T) {
	// === Phase 1: Create a multi-feature PDF (unencrypted) ===
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}

	// Page 1: HTML content with table, background, entities
	pdf.AddPage()
	pdf.SetMargins(50, 50, 50, 50)
	html := `<h1>Integration Test</h1>
<div style="background-color: #e8f0ff">Shaded intro block with <b>bold</b> and <i>italic</i>.</div>
<table border="1" width="100%">
	<thead><tr><th>Key</th><th>Value</th></tr></thead>
	<tbody><tr><td>alpha</td><td>42</td></tr><tr><td>beta</td><td>99</td></tr></tbody>
</table>
<p>Entities: &copy; &mdash; &alpha;&beta; &le; &infin;</p>`
	if _, err := pdf.InsertHTMLPaginated(50, 60, 495, html, HTMLBoxOption{
		DefaultFontFamily:  "lib",
		DefaultFontSize:    11,
		PageTop:            50,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	}); err != nil {
		t.Fatalf("InsertHTMLPaginated: %v", err)
	}

	// Page 2: formula
	pdf.AddPage()
	if err := pdf.SetFont("lib", "", 13); err != nil {
		t.Fatal(err)
	}
	if _, err := pdf.DrawFormula(60, 100, `E = mc^2`, 14, nil); err != nil {
		t.Fatalf("DrawFormula: %v", err)
	}

	// Page 2: annotations
	pdf.AddAnnotation(AnnotationOption{X: 50, Y: 200, W: 100, H: 20, Type: AnnotText, Content: "integration note"})
	pdf.AddAnnotation(AnnotationOption{X: 50, Y: 240, W: 100, H: 20, Type: AnnotHighlight, Content: "highlight"})

	// === Phase 2: Write unencrypted and verify structurally ===
	out := filepath.Join(t.TempDir(), "integration.pdf")
	if err := pdf.WritePdf(out); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// Page count
	if n, err := GetSourcePDFPageCountFromBytes(written); err != nil || n < 2 {
		t.Errorf("page count = %d, err %v; want >= 2", n, err)
	}

	// Text extraction
	texts, err := ExtractTextFromAllPages(written)
	if err != nil {
		t.Fatalf("extraction: %v", err)
	}
	allText := ""
	for _, runs := range texts {
		for _, tx := range runs {
			allText += tx.Text + " "
		}
	}
	for _, want := range []string{"Integration", "alpha", "beta", "Entities"} {
		if !strings.Contains(allText, want) {
			t.Errorf("missing %q in extracted text", want)
		}
	}

	// Text rasterization
	if _, err := RenderPageToImage(written, 0, RenderOption{DPI: 96}); err != nil {
		t.Fatalf("RenderPageToImage: %v", err)
	}

	// === Phase 3: AES-encrypted copy: no leak + password extraction ===
	pdf.SetEncryption(AESEncryptionConfig{
		Method:        EncryptAES256,
		UserPassword:  "integration",
		OwnerPassword: "owner",
		Permissions:   PermissionsPrint | PermissionsCopy,
	})
	encOut := filepath.Join(t.TempDir(), "integration_enc.pdf")
	if err := pdf.WritePdf(encOut); err != nil {
		t.Fatalf("WritePdf encrypted: %v", err)
	}
	encData, err := os.ReadFile(encOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encData), "Integration Test") {
		t.Errorf("plaintext leaked in encrypted PDF")
	}
	encTexts, err := ExtractTextFromAllPagesWithPassword(encData, "integration")
	if err != nil {
		t.Fatalf("password extraction: %v", err)
	}
	encAll := ""
	for _, runs := range encTexts {
		for _, tx := range runs {
			encAll += tx.Text + " "
		}
	}
	for _, want := range []string{"Integration", "alpha"} {
		if !strings.Contains(encAll, want) {
			t.Errorf("missing %q in encrypted extraction", want)
		}
	}
}
