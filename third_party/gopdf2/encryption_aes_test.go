package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aesWriteHelper creates an encrypted PDF with the given config and returns
// the file bytes plus a cleanup function.
func aesWriteHelper(t *testing.T, enc AESEncryptionConfig, text string) []byte {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.SetEncryption(enc); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	if err := pdf.Cell(nil, text); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "enc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestAES256WriteNoLeak verifies AES-256 encryption doesn't leak plaintext
// and the password path recovers the content.
func TestAES256WriteNoLeak(t *testing.T) {
	marker := "AES256 secret marker QWERTY"
	data := aesWriteHelper(t, AESEncryptionConfig{
		Method:        EncryptAES256,
		UserPassword:  "u256",
		OwnerPassword: "o256",
		Permissions:   PermissionsPrint,
	}, marker)

	s := string(data)
	if !strings.Contains(s, "/Encrypt") {
		t.Errorf("no /Encrypt dictionary in AES-256 output")
	}
	if strings.Contains(s, marker) {
		t.Errorf("AES-256 plaintext leaked into raw file")
	}

	texts, err := ExtractTextFromAllPagesWithPassword(data, "u256")
	if err != nil {
		t.Fatalf("AES-256 with-password extraction failed: %v", err)
	}
	found := false
	for _, runs := range texts {
		for _, tx := range runs {
			if strings.Contains(tx.Text, "QWERTY") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("AES-256 password extraction did not recover the marker")
	}
}

// TestAES128WriteNoLeak verifies AES-128 encryption.
func TestAES128WriteNoLeak(t *testing.T) {
	marker := "AES128 marker 12321"
	data := aesWriteHelper(t, AESEncryptionConfig{
		Method:        EncryptAES128,
		UserPassword:  "u128",
		OwnerPassword: "o128",
		Permissions:   PermissionsPrint | PermissionsCopy,
	}, marker)

	if strings.Contains(string(data), marker) {
		t.Errorf("AES-128 plaintext leaked into raw file")
	}
	texts, err := ExtractTextFromAllPagesWithPassword(data, "u128")
	if err != nil {
		t.Fatalf("AES-128 with-password extraction failed: %v", err)
	}
	found := false
	for _, runs := range texts {
		for _, tx := range runs {
			if strings.Contains(tx.Text, "12321") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("AES-128 password extraction did not recover the marker")
	}
}

// TestRC4SetEncryptionTrailer verifies SetEncryption with RC4 methods
// produces a trailer with /Encrypt (the UseProtection flag was previously
// not set, silently omitting the /Encrypt reference).
func TestRC4SetEncryptionTrailer(t *testing.T) {
	data := aesWriteHelper(t, AESEncryptionConfig{
		Method:        EncryptRC4V2,
		UserPassword:  "user",
		OwnerPassword: "ownerpass",
		Permissions:   PermissionsPrint,
	}, "RC4 trailer marker")
	if !strings.Contains(string(data), "/Encrypt") {
		t.Errorf("SetEncryption with RC4V2: no /Encrypt in trailer")
	}
}

// TestOpenPDFAES256Password verifies text extraction from AES-256
// encrypted PDFs. Note: OpenPDF (for modification) currently requires
// RC4-encrypted input; AES-encrypted PDFs are supported for text
// extraction via the WithPassword APIs.
func TestOpenPDFAES256Password(t *testing.T) {
	data := aesWriteHelper(t, AESEncryptionConfig{
		Method:        EncryptAES256,
		UserPassword:  "aespass",
		OwnerPassword: "owner",
		Permissions:   PermissionsPrint,
	}, "AES opened marker")
	texts, err := ExtractTextFromAllPagesWithPassword(data, "aespass")
	if err != nil {
		t.Fatalf("AES-256 with-password extraction failed: %v", err)
	}
	found := false
	for _, runs := range texts {
		for _, tx := range runs {
			if strings.Contains(tx.Text, "AES opened marker") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("AES-256 password extraction did not recover the marker")
	}
}

// TestOpenPDFEncryptedNoPassword verifies OpenPDF returns an error when
// opening an encrypted PDF without a password.
func TestOpenPDFEncryptedNoPassword(t *testing.T) {
	data := aesWriteHelper(t, AESEncryptionConfig{
		Method:        EncryptAES256,
		UserPassword:  "pass",
		OwnerPassword: "ownerpass",
		Permissions:   PermissionsPrint,
	}, "locked")
	mod := &GoPdf{}
	err := mod.OpenPDFFromBytes(data, nil)
	if err == nil {
		t.Errorf("OpenPDF without password on encrypted PDF should fail")
	}
}
