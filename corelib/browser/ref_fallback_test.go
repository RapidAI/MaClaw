package browser

import "testing"

func TestRefFindableAgain(t *testing.T) {
	if refFindableAgain(&BrowserElementRef{Ref: "@e1"}) {
		t.Fatal("empty ref is not findable")
	}
	if !refFindableAgain(&BrowserElementRef{Ref: "@e2", Name: "发布"}) {
		t.Fatal("named ref is findable")
	}
	if !refFindableAgain(&BrowserElementRef{Ref: "@e3", BoundingBox: BrowserBoundingBox{X: 1, Y: 1, Width: 40, Height: 20}}) {
		t.Fatal("boxed ref is findable")
	}
	s := &Session{}
	p, how, err := s.refFallbackPoint(&BrowserElementRef{Ref: "@e3", BoundingBox: BrowserBoundingBox{X: 10, Y: 20, Width: 40, Height: 20}})
	if err != nil || how != "bbox" || p.X != 30 || p.Y != 30 {
		t.Fatalf("bbox fallback got %+v %s %v", p, how, err)
	}
}
