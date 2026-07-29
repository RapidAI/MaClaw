//go:build windows

package main

import "testing"

func TestWindowsFramelessShellIsOpaque(t *testing.T) {
	app := &App{}
	webviewTransparent, windowTranslucent := app.PlatformTransparencyFlags()
	if webviewTransparent || windowTranslucent {
		t.Fatalf("Win10-safe frameless shell must be opaque, got webviewTransparent=%t windowTranslucent=%t", webviewTransparent, windowTranslucent)
	}
}

func TestNormalizeFramelessTopInsetUsesCSSPixels(t *testing.T) {
	tests := []struct {
		name     string
		physical int
		dpi      int
		want     int
	}{
		{name: "100 percent", physical: 8, dpi: 96, want: 8},
		{name: "125 percent", physical: 10, dpi: 120, want: 8},
		{name: "150 percent", physical: 12, dpi: 144, want: 8},
		{name: "invalid values", physical: -1, dpi: 144, want: 0},
		{name: "excessive inset", physical: 40, dpi: 96, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeFramelessTopInset(tt.physical, tt.dpi); got != tt.want {
				t.Fatalf("normalizeFramelessTopInset(%d, %d) = %d, want %d", tt.physical, tt.dpi, got, tt.want)
			}
		})
	}
}
