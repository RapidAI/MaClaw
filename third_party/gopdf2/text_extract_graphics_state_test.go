package gopdf

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

// buildGraphicsStatePDF writes a one-page PDF whose content stream scales
// and translates inside a q...Q group, then places two text blocks in user
// space:
//
//	q
//	200 0 0 100 72 642 cm
//	Q
//	BT
//	72 700.47 TD
//	/F1 24 Tf
//	(Hi) Tj
//	ET
//	BT
//	300 500 TD
//	/F1 24 Tf
//	(Lo) Tj
//	ET
//
// Regression: the extractor carried the q-group cm (a=200, d=100, e=72,
// f=642) into every later fragment, so "Hi" landed at X=200*72+72=14472 and
// Y=842-(100*700.47+642)=-69847 instead of its user-space position.
func buildGraphicsStatePDF(t *testing.T) []byte {
	t.Helper()
	content := "q\n200 0 0 100 72 642 cm\nQ\n" +
		"BT\n72 700.47 TD\n/F1 24 Tf\n(Hi) Tj\nET\n" +
		"BT\n300 500 TD\n/F1 24 Tf\n(Lo) Tj\nET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 0, len(objects))
	for i, body := range objects {
		offsets = append(offsets, buf.Len())
		buf.WriteString(fmt.Sprintf("%d 0 obj\n", i+1))
		buf.WriteString(body)
		buf.WriteString("\nendobj\n")
	}
	xrefStart := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefStart))
	return buf.Bytes()
}

func TestTextExtractionRestoresGraphicsStateAtQ(t *testing.T) {
	data := buildGraphicsStatePDF(t)
	fragments, err := ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 2 {
		t.Fatalf("fragments = %+v", fragments)
	}
	if fragments[0].Text != "Hi" || fragments[1].Text != "Lo" {
		t.Fatalf("texts = %q, %q", fragments[0].Text, fragments[1].Text)
	}
	for _, want := range []struct {
		name string
		frag ExtractedText
		x    float64
	}{{"Hi", fragments[0], 72}, {"Lo", fragments[1], 300}} {
		if d := math.Abs(want.frag.X - want.x); d > 1 {
			t.Errorf("%q X = %v, want ~%v", want.name, want.frag.X, want.x)
		}
		if want.frag.Y < 0 || want.frag.Y > 842 {
			t.Errorf("%q Y = %v", want.name, want.frag.Y)
		}
	}
}
