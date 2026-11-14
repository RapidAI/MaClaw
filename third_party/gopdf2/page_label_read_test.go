package gopdf

import (
	"testing"
)

// TestPageLabelsRoundTrip writes page labels and reads them back.
func TestPageLabelsRoundTrip(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()
	pdf.SetPageLabels([]PageLabel{
		{PageIndex: 0, Style: PageLabelRomanLower, Start: 1},
		{PageIndex: 2, Style: PageLabelDecimal, Start: 1},
	})
	path := t.TempDir() + "/labels.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	labels, err := ExtractPageLabelsFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		for _, l := range labels {
			t.Logf("label: %+v", l)
		}
		t.Fatalf("got %d label ranges, want 2", len(labels))
	}
	if labels[0].PageIndex != 0 || labels[0].Style != PageLabelRomanLower {
		t.Errorf("range 0 = %+v", labels[0])
	}
	if labels[1].PageIndex != 2 || labels[1].Style != PageLabelDecimal || labels[1].Start != 1 {
		t.Errorf("range 1 = %+v", labels[1])
	}
	// handle access
	doc, err := OpenPDFDocumentFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.PageLabels()) != 2 {
		t.Errorf("doc.PageLabels = %d", len(doc.PageLabels()))
	}
	rot, err := doc.Rotation(0)
	if err != nil || rot != 0 {
		t.Errorf("Rotation = %d, %v", rot, err)
	}
}
