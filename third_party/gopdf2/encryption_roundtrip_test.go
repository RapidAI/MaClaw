package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The library must be able to read back its own encrypted output with the
// password, and extraction must see the original text.
func TestEncryptedRoundTripReadBack(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{
		PageSize: *PageSizeA4,
		Protection: PDFProtectionConfig{
			UseProtection: true,
			Permissions:   PermissionsPrint | PermissionsCopy,
			UserPass:      []byte("secret"),
			OwnerPass:     []byte("ownersecret"),
		},
	})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	if err := pdf.Cell(nil, "Round trip secret marker XYZQW"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "enc2.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}

	// 1) without password: extraction must NOT see the plaintext
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	noPass, err := ExtractTextFromAllPages(raw)
	if err != nil {
		t.Logf("no-password extraction error (acceptable): %v", err)
	} else {
		for _, runs := range noPass {
			for _, tx := range runs {
				if strings.Contains(tx.Text, "XYZQW") {
					t.Errorf("plaintext visible without password!")
				}
			}
		}
	}

	// 2) with password: extraction must recover the text
	texts, err := ExtractTextFromAllPagesWithPassword(raw, "secret")
	if err != nil {
		t.Fatalf("with-password extraction failed: %v", err)
	}
	found := false
	for _, runs := range texts {
		for _, tx := range runs {
			if strings.Contains(tx.Text, "XYZQW") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("with-password extraction did not recover the marker")
	}
}
