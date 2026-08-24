package accessibility

import "testing"

func TestParseCSVWindowBounds(t *testing.T) {
	b, ok := parseCSVWindowBounds("10,20,800,600,Untitled - Notepad")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if b.X != 10 || b.Y != 20 || b.Width != 800 || b.Height != 600 {
		t.Fatalf("geom=%+v", b)
	}
	if b.Title != "Untitled - Notepad" {
		t.Fatalf("title=%q", b.Title)
	}
	if _, ok := parseCSVWindowBounds("0,0,10,10,tiny"); ok {
		t.Fatal("tiny window must be rejected")
	}
	b, ok = parseCSVWindowBounds("1,2,200,200,Hello, world")
	if !ok || b.Title != "Hello, world" {
		t.Fatalf("comma in title: ok=%v title=%q", ok, b.Title)
	}
}
