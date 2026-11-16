package guiapp

import "github.com/RapidAI/CodeClaw/guiapp/petpack"

// petTravelBox is the origin range for one locomotion step. screenW and
// screenH are the primary work-area sizes. A patrol passes the monitor work
// area the window is on, so a display left of the primary (negative origin)
// is not folded back onto the primary.
func petTravelBox(screenW, screenH, sz int, work petpack.WindowRect, patrol bool) (minX, maxX, minY, maxY int) {
	if sz < 1 {
		sz = 1
	}
	maxX = screenW - sz
	maxY = screenH - sz
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if !patrol || work.Right <= work.Left || work.Bottom <= work.Top {
		return 0, maxX, 0, maxY
	}
	minX = work.Left
	minY = work.Top
	maxX = work.Right - sz
	maxY = work.Bottom - sz
	if maxX < minX {
		maxX = minX
	}
	if maxY < minY {
		maxY = minY
	}
	return minX, maxX, minY, maxY
}

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
