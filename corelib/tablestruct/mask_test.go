package tablestruct

import (
	"reflect"
	"strings"
	"testing"
)

func TestSeparatorsAndHTML(t *testing.T) {
	const w, h = 40, 30
	mask := make([]int, w*h)
	for x := 0; x < w; x++ {
		mask[2*w+x] = classHorizontal
		mask[22*w+x] = classHorizontal
	}
	for y := 0; y < h; y++ {
		mask[y*w+3] = classVertical
		mask[y*w+28] = classVertical
	}
	rows, cols := Separators(mask, w, h, 20, 15)
	if !reflect.DeepEqual(rows, []int{2, 22}) || !reflect.DeepEqual(cols, []int{3, 28}) {
		t.Fatalf("rows %v cols %v", rows, cols)
	}
	html := HTML(rows, cols, []Line{{Text: "A&B", X: 10, Y: 8, W: 4, H: 4}})
	if !strings.Contains(html, "<table>") || !strings.Contains(html, "A&amp;B") {
		t.Fatalf("html %s", html)
	}
	if strings.Count(html, "<td>") != 1 {
		t.Fatalf("cells %s", html)
	}
}

func TestHTMLNeedsTwoRulings(t *testing.T) {
	if HTML([]int{1}, []int{1, 2}, nil) != "" {
		t.Fatal("one row still built a table")
	}
}
