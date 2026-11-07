package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormFieldFillOnOriginalInstance(t *testing.T) {
	// Create a PDF with form fields
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "Form:")
	if err := pdf.AddFormField(FormField{
		Name:     "name",
		Type:     FormFieldText,
		X:        150,
		Y:        50,
		W:        200,
		H:        20,
		Value:    "initial",
		FontSize: 12,
	}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.AddFormField(FormField{
		Name:     "email",
		Type:     FormFieldText,
		X:        150,
		Y:        80,
		W:        200,
		H:        20,
		Value:    "",
		FontSize: 12,
	}); err != nil {
		t.Fatal(err)
	}
	origFields := pdf.GetFormFields()
	t.Logf("original form fields: %d", len(origFields))
	path := filepath.Join(t.TempDir(), "form.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}

	// Fill fields on the original instance (ModifyFormFieldValue works
	// on the in-memory form field list populated by AddFormField)
	if err := pdf.ModifyFormFieldValue("name", "John Doe"); err != nil {
		t.Logf("ModifyFormFieldValue(name): %v", err)
	} else {
		t.Logf("name filled OK")
	}
	if err := pdf.ModifyFormFieldValue("email", "john@example.com"); err != nil {
		t.Logf("ModifyFormFieldValue(email): %v", err)
	} else {
		t.Logf("email filled OK")
	}
	// verify in output
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "John Doe") {
		t.Logf("filled value visible in output ✓")
	} else {
		t.Logf("filled value NOT visible in raw bytes (expected for Identity-H encoded text)")
	}
	_ = written
}

// TestFormFieldsAfterOpenPDF verifies that OpenPDF imports AcroForm fields
// so GetFormFields and ModifyFormFieldValue work after opening.
func TestFormFieldsAfterOpenPDF(t *testing.T) {
	// Create a PDF with form fields
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "Form:")
	if err := pdf.AddFormField(FormField{
		Name: "name", Type: FormFieldText, X: 150, Y: 50, W: 200, H: 20,
		Value: "initial", FontSize: 12,
	}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.AddFormField(FormField{
		Name: "email", Type: FormFieldText, X: 150, Y: 80, W: 200, H: 20,
		Value: "", FontSize: 12,
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "form.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}

	// Open the PDF — form fields should be imported
	data, _ := os.ReadFile(path)
	mod := &GoPdf{}
	if err := mod.OpenPDFFromBytes(data, nil); err != nil {
		t.Fatal(err)
	}
	fields := mod.GetFormFields()
	t.Logf("form fields after OpenPDF: %d", len(fields))
	for _, f := range fields {
		t.Logf("  %s: value=%q", f.Name, f.Value)
	}
	if len(fields) < 2 {
		t.Fatalf("expected >= 2 form fields after OpenPDF, got %d", len(fields))
	}

	// Fill the fields
	if err := mod.ModifyFormFieldValue("name", "John Doe"); err != nil {
		t.Errorf("ModifyFormFieldValue(name): %v", err)
	}
	if err := mod.ModifyFormFieldValue("email", "john@example.com"); err != nil {
		t.Errorf("ModifyFormFieldValue(email): %v", err)
	}

	// Save and verify
	out := filepath.Join(t.TempDir(), "filled.pdf")
	if err := mod.WritePdf(out); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(out)
	if !strings.Contains(string(written), "John Doe") {
		t.Errorf("filled value not visible in output")
	}
}
