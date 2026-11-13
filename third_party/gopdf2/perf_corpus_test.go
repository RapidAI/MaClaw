package gopdf

import (
	"os"
	"testing"
)

func BenchmarkCorpusTAMReview(b *testing.B) {
	data, err := os.ReadFile("test/corpus/TAMReview.pdf")
	if err != nil {
		b.Fatal(err)
	}
	count, _ := GetSourcePDFPageCountFromBytes(data)
	for i := 0; i < b.N; i++ {
		for p := 0; p < count; p++ {
			_, _ = ExtractPageLines(data, p)
			_, _ = ExtractPageDrawings(data, p)
			_, _ = ExtractTablesFromPage(data, p)
		}
	}
}

func BenchmarkCorpusTAMReviewDoc(b *testing.B) {
	data, err := os.ReadFile("test/corpus/TAMReview.pdf")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		doc, err := OpenPDFDocument(data)
		if err != nil {
			b.Fatal(err)
		}
		for p := 0; p < doc.PageCount(); p++ {
			_, _ = doc.Lines(p)
			_, _ = doc.Drawings(p)
			_, _ = doc.Tables(p)
		}
	}
}
