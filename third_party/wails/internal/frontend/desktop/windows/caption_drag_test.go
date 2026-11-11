//go:build windows

package windows

import "testing"

func TestCaptionDragBlocked(t *testing.T) {
	cases := []struct {
		name       string
		fullscreen bool
		want       bool
	}{
		{name: "normal window moves", want: false},
		{name: "maximised restores then moves", want: false},
		{name: "clamped work-area frame moves", want: false},
		{name: "fullscreen stays put", fullscreen: true, want: true},
	}
	for _, c := range cases {
		got := captionDragBlocked(c.fullscreen)
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
