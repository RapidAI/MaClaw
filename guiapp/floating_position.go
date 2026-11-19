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

// petTravelBounds picks the origin box for one step. A patrol uses the
// window's monitor. Roaming uses the monitor the pet is already on, so a
// pet who was walking a window on another display is not pulled back to the
// primary when that window hides. An empty monitor falls back to the primary
// size measured from the origin.
func petTravelBounds(screenW, screenH, sz int, windowWork, petWork petpack.WindowRect, patrol bool) (minX, maxX, minY, maxY int) {
	work := petWork
	if patrol && windowWork.Right > windowWork.Left && windowWork.Bottom > windowWork.Top {
		work = windowWork
	}
	onMonitor := work.Right > work.Left && work.Bottom > work.Top
	return petTravelBox(screenW, screenH, sz, work, onMonitor)
}

// petWorkAreaAt is the work area under a pet rectangle. Windows fills it
// with the nearest monitor. A nil hook means the host only knows the primary
// size, measured from the origin.
var petWorkAreaAt func(x, y, sz int) (petpack.WindowRect, bool)

// clampPetScreenPosition keeps a saved or restored origin on the monitor it
// belongs to. A display left of the primary has a negative X; folding that
// to the origin pulls the pet onto the primary the next time she is shown.
func clampPetScreenPosition(x, y, sz int) (int, int) {
	if petWorkAreaAt != nil {
		if work, ok := petWorkAreaAt(x, y, sz); ok && work.Right > work.Left && work.Bottom > work.Top {
			minX, maxX, minY, maxY := petTravelBox(0, 0, sz, work, true)
			return clampOriginToBox(x, y, minX, maxX, minY, maxY)
		}
	}
	return clampFloatingPosition(x, y, sz, getScreenWidth(), getScreenHeight())
}

func clampOriginToBox(x, y, minX, maxX, minY, maxY int) (int, int) {
	if x < minX {
		x = minX
	} else if x > maxX {
		x = maxX
	}
	if y < minY {
		y = minY
	} else if y > maxY {
		y = maxY
	}
	return x, y
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
