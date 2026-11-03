package gopdf

import (
	"bytes"
	"io"
	"os"
	"testing"
)

const testPDFPath = "./examples/outline_example/outline_demo.pdf"
const testFontPath = "./test/res/LiberationSerif-Regular.ttf"

func TestOpenPDFFromBytes(t *testing.T) {
	data, err := os.ReadFile(testPDFPath)
	if err != nil {
		t.Skipf("test PDF not available: %v", err)
	}

	pdf := GoPdf{}
	if err := pdf.OpenPDFFromBytes(data, nil); err != nil {
		t.Fatalf("OpenPDFFromBytes: %v", err)
	}

	if got := pdf.GetNumberOfPages(); got == 0 {
		t.Fatal("expected at least 1 page")
	}

	// Should be able to switch to page 1.
	if err := pdf.SetPage(1); err != nil {
		t.Fatalf("SetPage(1): %v", err)
	}

	// Should produce valid output.
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output does not start with %PDF-")
	}
}

func TestOpenPDFFromStream(t *testing.T) {
	data, err := os.ReadFile(testPDFPath)
	if err != nil {
		t.Skipf("test PDF not available: %v", err)
	}

	rs := io.ReadSeeker(bytes.NewReader(data))
	pdf := GoPdf{}
	if err := pdf.OpenPDFFromStream(&rs, nil); err != nil {
		t.Fatalf("OpenPDFFromStream: %v", err)
	}

	if got := pdf.GetNumberOfPages(); got == 0 {
		t.Fatal("expected at least 1 page")
	}
}

func TestOpenPDF_File(t *testing.T) {
	pdf := GoPdf{}
	if err := pdf.OpenPDF(testPDFPath, nil); err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	if got := pdf.GetNumberOfPages(); got == 0 {
		t.Fatal("expected at least 1 page")
	}
}

func TestOpenPDF_OverlayContent(t *testing.T) {
	pdf := GoPdf{}
	if err := pdf.OpenPDF(testPDFPath, nil); err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	if err := pdf.AddTTFFont("liberation", testFontPath); err != nil {
		t.Skipf("test font not available: %v", err)
	}
	if err := pdf.SetFont("liberation", "", 14); err != nil {
		t.Fatalf("SetFont: %v", err)
	}

	// Draw on page 1.
	if err := pdf.SetPage(1); err != nil {
		t.Fatalf("SetPage(1): %v", err)
	}
	pdf.SetXY(100, 100)
	if err := pdf.Cell(nil, "Overlay text on page 1"); err != nil {
		t.Fatalf("Cell: %v", err)
	}

	// Draw on page 2 if it exists.
	if pdf.GetNumberOfPages() >= 2 {
		if err := pdf.SetPage(2); err != nil {
			t.Fatalf("SetPage(2): %v", err)
		}
		pdf.SetXY(200, 200)
		if err := pdf.Cell(nil, "Overlay text on page 2"); err != nil {
			t.Fatalf("Cell: %v", err)
		}
	}

	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output PDF is empty")
	}
}

func TestOpenPDF_CustomBox(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFPath, &OpenPDFOption{Box: "/MediaBox"})
	if err != nil {
		t.Fatalf("OpenPDF with custom box: %v", err)
	}

	if got := pdf.GetNumberOfPages(); got == 0 {
		t.Fatal("expected at least 1 page")
	}
}

func TestOpenPDF_InvalidFile(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF("nonexistent.pdf", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestOpenPDF_InvalidData(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDFFromBytes([]byte("not a pdf"), nil)
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}
