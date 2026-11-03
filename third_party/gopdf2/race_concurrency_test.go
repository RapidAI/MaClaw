package gopdf

// Concurrency smoke test: extraction and rendering from multiple goroutines
// must not race (run with -race).
import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentExtraction(t *testing.T) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skip("font not available")
	}

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "Concurrent extraction smoke test content.")
	pdf.SetXY(50, 80)
	_ = pdf.Cell(nil, "Second line for extraction.")
	path := filepath.Join(t.TempDir(), "conc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_, _ = ExtractTextFromAllPages(data)
				_, _ = ExtractPageLines(data, 0)
				_, _ = GetSourcePDFPageCountFromBytes(data)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentRender exercises the renderer from multiple goroutines.
func TestConcurrentRender(t *testing.T) {
	files, _ := filepath.Glob("test/corpus/*.pdf")
	if len(files) == 0 {
		t.Skip("corpus not available")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				_, _ = RenderPageToImage(data, 0, RenderOption{DPI: 72})
			}
		}()
	}
	wg.Wait()
}
