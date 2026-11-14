package petpack

import (
	"math"
	"time"
)

// RoamHeadings is the direction cycle for a clip that travels. Zero is
// screen-right, positive angles move down, and the list mixes horizontal,
// vertical, and diagonal headings so a walk is not locked to one axis.
var RoamHeadings = []float64{
	0,
	math.Pi / 6,
	math.Pi / 2,
	2 * math.Pi / 3,
	math.Pi,
	-2 * math.Pi / 3,
	-math.Pi / 2,
	-math.Pi / 6,
	math.Pi / 4,
	-math.Pi / 4,
	math.Pi / 3,
	-math.Pi / 3,
}

// RoamRetargetEvery is how long one heading lasts before the next one in
// RoamHeadings. An edge bounce also starts a new interval.
const RoamRetargetEvery = 3600 * time.Millisecond

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

// StepDesktopTravel2D clamps one step on both screen axes.
func StepDesktopTravel2D(x, y int, dx, dy float64, minX, maxX, minY, maxY int) (nx, ny int, hitX, hitY bool) {
	nx, hitX = StepDesktopTravel(x, dx, minX, maxX)
	ny, hitY = StepDesktopTravel(y, dy, minY, maxY)
	return nx, ny, hitX, hitY
}

// ReflectTravelHeading bounces a heading off the work-area edges. Screen Y
// grows downward. Hitting both edges reflects a corner.
func ReflectTravelHeading(heading float64, hitX, hitY bool) float64 {
	if hitX {
		heading = math.Pi - heading
	}
	if hitY {
		heading = -heading
	}
	return math.Atan2(math.Sin(heading), math.Cos(heading))
}

// DesktopRoam is the host's memory of which way a traveling pet is going.
type DesktopRoam struct {
	Heading  float64
	Slot     int
	Ready    bool
	RemX     float64
	RemY     float64
	FaceLeft bool
	Retarget time.Time
}

// StepDesktopRoam advances a pet window from a clip's design-pixel velocity.
// vx and vy are design pixels per millisecond; their length is the speed and
// their angle seeds the first heading. Later headings come from RoamHeadings,
// so the same clip walks across, up and down, and on a diagonal. scale is
// petSize/256. A non-positive or stalled dt does not move the window.
func StepDesktopRoam(s DesktopRoam, vx, vy, scale float64, dt time.Duration, x, y, minX, maxX, minY, maxY int, now time.Time) (DesktopRoam, int, int) {
	speed := math.Hypot(vx, vy)
	if speed == 0 || scale <= 0 || math.IsNaN(speed) || math.IsNaN(scale) {
		s.RemX, s.RemY = 0, 0
		return s, x, y
	}
	if !s.Ready {
		s.Heading = math.Atan2(vy, vx)
		s.Slot = 0
		s.Ready = true
		s.Retarget = now
	}
	dtMS := dt.Milliseconds()
	if dtMS <= 0 || dtMS > 200 {
		return s, x, y
	}
	if s.Retarget.IsZero() || now.Sub(s.Retarget) >= RoamRetargetEvery {
		if len(RoamHeadings) > 0 {
			s.Slot = (s.Slot + 1) % len(RoamHeadings)
			s.Heading = RoamHeadings[s.Slot]
		}
		s.Retarget = now
	}
	dist := speed * scale * float64(dtMS)
	s.RemX += math.Cos(s.Heading) * dist
	s.RemY += math.Sin(s.Heading) * dist
	stepX := math.Trunc(s.RemX)
	stepY := math.Trunc(s.RemY)
	s.RemX -= stepX
	s.RemY -= stepY
	s.FaceLeft = keepFacing(s.Heading, s.FaceLeft)
	if stepX == 0 && stepY == 0 {
		return s, x, y
	}
	nx, ny, hitX, hitY := StepDesktopTravel2D(x, y, stepX, stepY, minX, maxX, minY, maxY)
	if hitX || hitY {
		s.Heading = ReflectTravelHeading(s.Heading, hitX, hitY)
		s.FaceLeft = keepFacing(s.Heading, s.FaceLeft)
		s.RemX, s.RemY = 0, 0
		s.Retarget = now
	}
	return s, nx, ny
}

// keepFacing turns the sprite to the horizontal part of a heading. A nearly
// vertical heading keeps the side she was already showing.
func keepFacing(heading float64, current bool) bool {
	c := math.Cos(heading)
	if c < -0.2 {
		return true
	}
	if c > 0.2 {
		return false
	}
	return current
}
