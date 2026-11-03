package gopdf

import (
	"fmt"
	"io"
	"math"
)

// writeArcSegments approximates a circular arc using cubic Bézier curves.
// Each segment covers at most 90 degrees. The arc goes from startRad to endRad
// counter-clockwise (in standard math coordinates; PDF y-axis is already handled
// by the caller flipping cy).
//
// cx, cy: center in PDF coordinates (y already flipped)
// r: radius
// startRad, endRad: angles in radians
func writeArcSegments(w io.Writer, cx, cy, r, startRad, endRad float64) {
	// Normalize so we always sweep in the positive direction
	sweep := endRad - startRad
	if sweep < 0 {
		sweep += 2 * math.Pi
	}
	if sweep == 0 {
		return
	}

	// Split into segments of at most 90 degrees (π/2)
	maxSeg := math.Pi / 2
	nSegs := int(math.Ceil(sweep / maxSeg))
	segAngle := sweep / float64(nSegs)

	angle := startRad
	for i := 0; i < nSegs; i++ {
		nextAngle := angle + segAngle
		writeArcBezier(w, cx, cy, r, angle, nextAngle)
		angle = nextAngle
	}
}

// writeArcBezier writes a single cubic Bézier curve approximating a circular arc
// from angle a1 to a2 (in radians). The arc must be <= 90 degrees.
// Uses the standard Bézier approximation for circular arcs.
func writeArcBezier(w io.Writer, cx, cy, r, a1, a2 float64) {
	// Half-angle
	halfAngle := (a2 - a1) / 2
	// Bézier control point distance factor
	// k = (4/3) * tan(halfAngle)
	k := (4.0 / 3.0) * math.Tan(halfAngle)

	// Start point (already drawn to by moveto/lineto)
	// End point
	ex := cx + r*math.Cos(a2)
	ey := cy - r*math.Sin(a2)

	// Control point 1: perpendicular to radius at start
	cp1x := cx + r*(math.Cos(a1)-k*math.Sin(a1))
	cp1y := cy - r*(math.Sin(a1)+k*math.Cos(a1))

	// Control point 2: perpendicular to radius at end (opposite direction)
	cp2x := cx + r*(math.Cos(a2)+k*math.Sin(a2))
	cp2y := cy - r*(math.Sin(a2)-k*math.Cos(a2))

	fmt.Fprintf(w, "%.2f %.2f %.2f %.2f %.2f %.2f c\n",
		cp1x, cp1y, cp2x, cp2y, ex, ey)
}
