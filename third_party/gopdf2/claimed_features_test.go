package gopdf

import (
	"os"
	"path/filepath"
	"testing"
)

// Probe ColorspaceConvert
func TestColorspaceConvertRoundTrip(t *testing.T) {
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
	_ = pdf.Cell(nil, "Colorspace test")
	pdf.SetXY(50, 80)
	_ = pdf.Cell(nil, "Second line")
	path := filepath.Join(t.TempDir(), "cs.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	converted, err := ConvertColorspace(data, ConvertColorspaceOption{Target: ColorspaceGray})
	t.Logf("ColorspaceConvert: err=%v outLen=%d", err, len(converted))
	if err != nil {
		t.Errorf("ColorspaceConvert failed: %v", err)
	}
}

// Probe ContentStreamClean
func TestContentStreamCleanRoundTrip(t *testing.T) {
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
	_ = pdf.Cell(nil, "Content cleaning test")
	path := filepath.Join(t.TempDir(), "cs.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	cleaned, err := CleanContentStreams(data)
	t.Logf("CleanContentStream: err=%v outLen=%d (orig %d)", err, len(cleaned), len(data))
	if err != nil {
		t.Errorf("CleanContentStream failed: %v", err)
	}
}

// Probe GarbageCollect
func TestGarbageCollectPagesPreserved(t *testing.T) {
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
	_ = pdf.Cell(nil, "GC test")
	pdf.AddPage()
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "page 2")
	n := pdf.GarbageCollect(GCCompact)
	t.Logf("GarbageCollect: removed %d objects", n)
	path := filepath.Join(t.TempDir(), "gc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if cnt, err := GetSourcePDFPageCountFromBytes(written); err != nil || cnt != 2 {
		t.Errorf("post-GC page count = %d, err %v; want 2", cnt, err)
	}
}

// Probe Journal undo/redo
func TestJournalUndoRedoNoPanic(t *testing.T) {
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
	_ = pdf.Cell(nil, "before journal")
	pdf.JournalEnable()
	pdf.JournalStartOp("test op")
	pdf.SetXY(50, 80)
	_ = pdf.Cell(nil, "after journal start")
	if _, err := pdf.JournalUndo(); err != nil {
		t.Logf("JournalUndo: %v", err)
	}
	if _, err := pdf.JournalRedo(); err != nil {
		t.Logf("JournalRedo: %v", err)
	}
	path := filepath.Join(t.TempDir(), "journal.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Logf("journal write: %v", err)
	}
}
