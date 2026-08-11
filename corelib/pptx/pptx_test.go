package pptx

import (
	"strings"
	"testing"
)

func TestRecoverPresentationReadFailsClosed(t *testing.T) {
	presentation := &Presentation{SlideCount: 1}
	var err error
	func() {
		defer recoverPresentationRead(&presentation, &err)
		panic("malformed presentation")
	}()
	if presentation != nil {
		t.Fatalf("panic retained partial presentation: %#v", presentation)
	}
	if err == nil || !strings.Contains(err.Error(), "presentation parser panicked") {
		t.Fatalf("panic error = %v", err)
	}
}

func TestPaginatePresentationUsesSlideOffsetAndContinuation(t *testing.T) {
	presentation := &Presentation{SlideCount: 7, Slides: []Slide{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5}, {Number: 6}, {Number: 7}}}
	page := Paginate(presentation, 2, 2)
	if page.SlideCount != 7 || len(page.Slides) != 2 || page.Slides[0].Number != 3 || !page.Truncated || page.NextOffset != 4 {
		t.Fatalf("presentation page = %#v", page)
	}
	finalPage := Paginate(presentation, page.NextOffset, 10)
	if len(finalPage.Slides) != 3 || finalPage.Slides[0].Number != 5 || finalPage.Truncated || finalPage.NextOffset != 7 {
		t.Fatalf("final presentation page = %#v", finalPage)
	}
}
