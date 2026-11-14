package petpack

import "math"

// StepDesktopTravel moves a pet window by delta screen pixels and stops at the
// work-area edges. hitEdge is true when the step was clamped, so the host can
// turn the pet around instead of pinning it to the bezel.
func StepDesktopTravel(x int, delta float64, minX, maxX int) (next int, hitEdge bool) {
	if maxX < minX {
		maxX = minX
	}
	pos := float64(x) + delta
	if pos < float64(minX) {
		return minX, delta != 0
	}
	if pos > float64(maxX) {
		return maxX, delta != 0
	}
	return int(math.Round(pos)), false
}
