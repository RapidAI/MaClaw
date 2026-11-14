package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFlattenFormFields verifies values bake into page content and the
// interactive fields disappear.
func TestFlattenFormFields(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 40)
	_ = pdf.Cell(nil, "Name:")
	if err := pdf.AddTextField("fullname", 120, 35, 200, 20); err != nil {
		t.Fatal(err)
	}
	if err := pdf.AddFormField(FormField{
		Type: FormFieldCheckbox,
		Name: "agree",
		X:    120, Y: 70, W: 16, H: 16,
		Value:      "yes",
		FontFamily: "lib",
	}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ModifyFormFieldValue("fullname", "Jane Q. Tester"); err != nil {
		t.Fatal(err)
	}
	if len(pdf.GetFormFields()) != 2 {
		t.Fatalf("fields = %d, want 2", len(pdf.GetFormFields()))
	}
	if err := pdf.FlattenFormFields(); err != nil {
		t.Fatal(err)
	}
	if n := len(pdf.GetFormFields()); n != 0 {
		t.Errorf("fields after flatten = %d, want 0", n)
	}
	path := filepath.Join(t.TempDir(), "flat.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines, err := ExtractPageLines(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, ln := range lines {
		text += ln.Text + "\n"
	}
	if !strings.Contains(text, "Jane Q. Tester") {
		t.Errorf("baked value missing: %q", text)
	}
	if !strings.Contains(text, "X") {
		t.Errorf("checkbox X missing: %q", text)
	}
	// widgets no longer reported as annotations
	annots, err := ExtractAnnotationsFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range annots {
		if a.Type == "Widget" {
			t.Errorf("widget still present after flatten: %+v", a)
		}
	}
}
