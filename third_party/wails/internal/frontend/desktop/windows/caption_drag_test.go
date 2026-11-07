//go:build windows

package windows

import "testing"

func TestCaptionDragBlocked(t *testing.T) {
	cases := []struct {
		name                       string
		fullscreen, zoomed, covers bool
		want                       bool
	}{
		{name: "normal", want: false},
		{name: "fullscreen", fullscreen: true, want: true},
		{name: "zoomed", zoomed: true, want: true},
		{name: "clamped work area", covers: true, want: true},
		{name: "smaller than work area", covers: false, want: false},
	}
	for _, c := range cases {
		got := captionDragBlocked(c.fullscreen, c.zoomed, c.covers)
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
