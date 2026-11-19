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
// The edge fields are the in-memory patrol around a visible window. They are
// not saved with the pet.
type DesktopRoam struct {
	Heading  float64
	Slot     int
	Ready    bool
	RemX     float64
	RemY     float64
	FaceLeft bool
	Retarget time.Time
	// EdgeOn is set once she is walking a window border. The host clears it
	// when that window is hidden or fullscreen, so the next visit snaps to
	// the nearest point instead of a stale distance.
	EdgeOn bool
	// Along is the clockwise distance, in screen pixels, from the window's
	// top-left corner to the point under the pet's center.
	Along float64
	// EdgeDir is +1 clockwise and -1 back toward the start. Zero picks clockwise.
	EdgeDir int
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

// WindowRect is a window's outer bounds in screen pixels. Right and Bottom
// are the exclusive edges, the same way a window rectangle is reported.
type WindowRect struct {
	Left, Top, Right, Bottom int
}

// windowEdgeMinPerimeter skips a missing or tiny window and leaves the pet
// on the free roam. A real window is far larger than this.
const windowEdgeMinPerimeter = 32

// StepPetTravel walks the pet back and forth along a window's outer edge
// when that window is on screen. edgeOK is false when the window is hidden
// or covers its whole monitor; the pet then roams the work area, and the
// next visit starts again from the nearest point. A zero speed does not
// pull the pet onto the border.
func StepPetTravel(s DesktopRoam, vx, vy, scale float64, dt time.Duration, x, y, petSize, minX, maxX, minY, maxY int, now time.Time, rect WindowRect, edgeOK bool) (DesktopRoam, int, int) {
	if edgeOK {
		if next, nx, ny, took := StepWindowEdge(s, vx, vy, scale, dt, x, y, petSize, rect, minX, maxX, minY, maxY); took {
			return next, nx, ny
		}
	}
	s.EdgeOn = false
	return StepDesktopRoam(s, vx, vy, scale, dt, x, y, minX, maxX, minY, maxY, now)
}

// StepWindowEdge moves the pet's center along the outer border and back.
// Along is the clockwise distance from the top-left corner. The first visit
// starts at the nearest point of the current center so she does not jump to
// a far corner. took is false when the rectangle cannot be walked; the host
// then uses free roam. A zero speed reports took and leaves the window where
// it is, including a pet that is pinned or between idle clips.
func StepWindowEdge(s DesktopRoam, vx, vy, scale float64, dt time.Duration, x, y, petSize int, rect WindowRect, minX, maxX, minY, maxY int) (DesktopRoam, int, int, bool) {
	_, _, perim, ok := windowEdgeSpan(rect)
	if !ok || petSize <= 0 {
		return s, x, y, false
	}
	speed := math.Hypot(vx, vy)
	if speed == 0 || scale <= 0 || math.IsNaN(speed) || math.IsNaN(scale) {
		s.RemX, s.RemY = 0, 0
		return s, x, y, true
	}
	limit := float64(perim)
	if !s.EdgeOn {
		s.Along = nearestWindowEdgeAlong(rect, x+petSize/2, y+petSize/2)
		if s.EdgeDir == 0 {
			s.EdgeDir = 1
		}
		s.EdgeOn = true
	}
	if s.Along < 0 {
		s.Along = 0
	}
	if s.Along > limit {
		s.Along = limit
		if s.EdgeDir > 0 {
			s.EdgeDir = -1
		}
	}
	dtMS := dt.Milliseconds()
	if dtMS > 0 && dtMS <= 200 {
		s.Along, s.EdgeDir = advanceWindowEdge(s.Along, speed*scale*float64(dtMS), s.EdgeDir, limit)
	}
	nx, ny, face := windowEdgeOrigin(rect, s.Along, petSize, s.EdgeDir, s.FaceLeft)
	nx, ny = clampPetOrigin(nx, ny, minX, maxX, minY, maxY)
	s.FaceLeft = face
	s.RemX, s.RemY = 0, 0
	return s, nx, ny, true
}

func windowEdgeSpan(rect WindowRect) (width, height, perim int, ok bool) {
	width = rect.Right - rect.Left
	height = rect.Bottom - rect.Top
	if width <= 0 || height <= 0 {
		return 0, 0, 0, false
	}
	perim = 2 * (width + height)
	if perim < windowEdgeMinPerimeter {
		return width, height, perim, false
	}
	return width, height, perim, true
}

func nearestWindowEdgeAlong(rect WindowRect, cx, cy int) float64 {
	w := float64(rect.Right - rect.Left)
	h := float64(rect.Bottom - rect.Top)
	px, py := float64(cx), float64(cy)
	bestDist := math.MaxFloat64
	bestAlong := 0.0
	consider := func(ax, ay, bx, by, offset float64) {
		dx, dy := bx-ax, by-ay
		length := math.Hypot(dx, dy)
		t := 0.0
		if length > 0 {
			t = ((px-ax)*dx + (py-ay)*dy) / (length * length)
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		qx := ax + t*dx
		qy := ay + t*dy
		dist := math.Hypot(px-qx, py-qy)
		if dist < bestDist {
			bestDist = dist
			bestAlong = offset + t*length
		}
	}
	left, top := float64(rect.Left), float64(rect.Top)
	right, bottom := float64(rect.Right), float64(rect.Bottom)
	consider(left, top, right, top, 0)
	consider(right, top, right, bottom, w)
	consider(right, bottom, left, bottom, w+h)
	consider(left, bottom, left, top, 2*w+h)
	return bestAlong
}

func advanceWindowEdge(along, dist float64, dir int, perim float64) (float64, int) {
	if dir >= 0 {
		dir = 1
	} else {
		dir = -1
	}
	if perim <= 0 || dist <= 0 || math.IsNaN(dist) {
		return along, dir
	}
	for i := 0; i < 8 && dist > 0; i++ {
		if dir > 0 {
			room := perim - along
			if dist < room {
				return along + dist, dir
			}
			dist -= room
			along = perim
			dir = -1
			if dist == 0 {
				return along, dir
			}
			continue
		}
		if dist < along {
			return along - dist, dir
		}
		dist -= along
		along = 0
		dir = 1
		if dist == 0 {
			return along, dir
		}
	}
	return along, dir
}

// windowEdgeOrigin places the pet window so its center sits on the border.
// dir +1 walks clockwise. A vertical side keeps the side she was facing.
func windowEdgeOrigin(rect WindowRect, along float64, petSize, dir int, faceLeft bool) (int, int, bool) {
	w := float64(rect.Right - rect.Left)
	h := float64(rect.Bottom - rect.Top)
	if along < 0 {
		along = 0
	}
	perim := 2 * (w + h)
	if perim > 0 && along > perim {
		along = perim
	}
	if dir == 0 {
		dir = 1
	}
	var cx, cy float64
	horizontal := 0
	switch {
	case along <= w:
		cx = float64(rect.Left) + along
		cy = float64(rect.Top)
		horizontal = 1
	case along <= w+h:
		cx = float64(rect.Right)
		cy = float64(rect.Top) + (along - w)
	case along <= 2*w+h:
		cx = float64(rect.Right) - (along - (w + h))
		cy = float64(rect.Bottom)
		horizontal = -1
	default:
		cx = float64(rect.Left)
		cy = float64(rect.Bottom) - (along - (2*w + h))
	}
	if dir < 0 {
		horizontal = -horizontal
	}
	if horizontal > 0 {
		faceLeft = false
	} else if horizontal < 0 {
		faceLeft = true
	}
	half := petSize / 2
	return int(math.Round(cx)) - half, int(math.Round(cy)) - half, faceLeft
}

func clampPetOrigin(x, y, minX, maxX, minY, maxY int) (int, int) {
	if maxX < minX {
		maxX = minX
	}
	if maxY < minY {
		maxY = minY
	}
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
