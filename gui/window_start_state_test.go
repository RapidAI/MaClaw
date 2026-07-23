package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestShouldMaximiseMainWindowForConstrainedScreens(t *testing.T) {
	tests := []struct {
		name      string
		sw, sh    int
		maximised bool
	}{
		{"1366x768 laptop", 1366, 768, true},
		{"1280x1024 desktop", 1280, 1024, true},
		{"1080p at 150 percent scaling", 1280, 720, true},
		{"1440x900 laptop", 1440, 900, false},
		{"1080p desktop", 1920, 1080, false},
		{"screen detection failed", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldMaximiseMainWindow(tt.sw, tt.sh); got != tt.maximised {
				t.Errorf("shouldMaximiseMainWindow(%d, %d) = %t, want %t", tt.sw, tt.sh, got, tt.maximised)
			}
		})
	}
}

func TestWindowStartState(t *testing.T) {
	if got := windowStartState(true); got != options.Maximised {
		t.Errorf("maximised start state = %v, want %v", got, options.Maximised)
	}
	if got := windowStartState(false); got != options.Normal {
		t.Errorf("normal start state = %v, want %v", got, options.Normal)
	}
}

func TestPostEnvironmentCheckPreservesTheStartupMaximisePolicy(t *testing.T) {
	tests := [][2]int{
		{1366, 768},
		{1280, 720},
		{1440, 900},
		{1920, 1080},
	}

	for _, screen := range tests {
		startup := shouldMaximiseMainWindow(screen[0], screen[1])
		postCheck := shouldPreserveMaximisedWindowAfterEnvironmentCheck(screen[0], screen[1])
		if startup != postCheck {
			t.Errorf("screen %dx%d: startup policy = %t, post-check policy = %t", screen[0], screen[1], startup, postCheck)
		}
	}
}
