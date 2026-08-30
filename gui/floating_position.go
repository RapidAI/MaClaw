package main

// clampFloatingPosition keeps a pet window fully inside the screen.
// A persisted or default coordinate computed against a larger display (or the
// 1920×1080 GDK fallback) otherwise lands off-screen on 1280×800 remotes.
func clampFloatingPosition(x, y, winSize, screenW, screenH int) (int, int) {
	if winSize < 1 {
		winSize = defaultPetSize
	}
	if screenW > 0 {
		maxX := screenW - winSize
		if maxX < 0 {
			maxX = 0
		}
		if x > maxX {
			x = maxX
		}
		if x < 0 {
			x = 0
		}
	}
	if screenH > 0 {
		maxY := screenH - winSize
		if maxY < 0 {
			maxY = 0
		}
		if y > maxY {
			y = maxY
		}
		if y < 0 {
			y = 0
		}
	}
	return x, y
}
