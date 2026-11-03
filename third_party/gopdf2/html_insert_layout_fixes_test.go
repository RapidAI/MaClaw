package gopdf

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// solidJPEGFixes builds a tiny JPEG so an <img> carries a real intrinsic size.
func solidJPEGFixes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 130, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Regression 2: an inline <img> participates in the line height. A 600pt
// figure inside a <p> previously advanced the cursor by one text line (~14pt),
// so the flow drew later text over the bitmap and measured boxes lied about
// the real height.
func TestHTMLBoxImageAdvancesTheCursorByTheImageHeight(t *testing.T) {
	jpegPath := filepath.Join(t.TempDir(), "probe-fixes.jpg")
	if err := os.WriteFile(jpegPath, solidJPEGFixes(t, 480, 600), 0o600); err != nil {
		t.Fatal(err)
	}
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	endY, err := pdf.InsertHTMLBox(50, 50, 400, 1e6, `<p><img src="`+filepath.ToSlash(jpegPath)+`" width="450" height="600"/></p><p>after</p>`, HTMLBoxOption{DefaultFontFamily: "lib", DefaultFontSize: 12})
	if err != nil {
		t.Fatal(err)
	}
	// Fit within the 400pt-wide box scales 450x600 down to 400x533; the
	// cursor must still skip it plus the line that follows. Before the fix
	// the whole paragraph block measured ~14pt.
	got := endY - 50
	if got < 520 {
		t.Fatalf("image advanced only %.1fpt, want ~533+", got)
	}
}

// Regression 3: the first line of a tight box still draws. Restored default
// <p> margins previously pushed a 10pt brand line out of an 18pt strip box,
// because the clip condition required cursor+lineHeight <= boxH.
func TestHTMLBoxTightBoxKeepsItsFirstLine(t *testing.T) {
	for _, boxH := range []float64{18, 20} {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
			t.Fatal(err)
		}
		pdf.AddPage()
		endY, err := pdf.InsertHTMLBox(24, 96, 500, boxH, `<p style="font-size:10pt">MaClaw</p>`, HTMLBoxOption{DefaultFontFamily: "lib", DefaultFontSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		data, err := pdf.GetBytesPdfReturnErr()
		if err != nil {
			t.Fatal(err)
		}
		texts, err := ExtractTextFromPage(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, item := range texts {
			text.WriteString(item.Text)
		}
		if !strings.Contains(text.String(), "MaClaw") {
			t.Fatalf("boxH=%.0f dropped the brand line: %q (endY=%.2f)", boxH, text.String(), endY)
		}
	}
}
