package gopdf

import (
	"fmt"
	"io"
	"math"
)

type cacheContentSector struct {
	pageHeight float64
	cx         float64 // center X
	cy         float64 // center Y
	r          float64 // radius
	startDeg   float64 // start angle in degrees
	endDeg     float64 // end angle in degrees
	style      string
	opts       sectorOptions
}

type sectorOptions struct {
	extGStateIndexes []int
}

func (c *cacheContentSector) write(w io.Writer, protection *PDFProtection) error {
	h := c.pageHeight
	cx := c.cx
	cy := h - c.cy
	r := c.r

	fmt.Fprintf(w, "q\n")
	for _, extGStateIndex := range c.opts.extGStateIndexes {
		fmt.Fprintf(w, "/GS%d gs\n", extGStateIndex)
	}

	// Move to center
	fmt.Fprintf(w, "%.2f %.2f m\n", cx, cy)

	// Line to arc start point
	startRad := c.startDeg * math.Pi / 180
	endRad := c.endDeg * math.Pi / 180
	sx := cx + r*math.Cos(startRad)
	sy := cy - r*math.Sin(startRad) // PDF y-axis is inverted
	fmt.Fprintf(w, "%.2f %.2f l\n", sx, sy)

	// Draw arc using Bézier curve segments (max 90 degrees each)
	writeArcSegments(w, cx, cy, r, startRad, endRad)

	// Close path back to center
	fmt.Fprintf(w, "%.2f %.2f l\n", cx, cy)

	if c.style == "F" {
		fmt.Fprintf(w, "f\n")
	} else if c.style == "FD" || c.style == "DF" {
		fmt.Fprintf(w, "b\n")
	} else {
		fmt.Fprintf(w, "s\n")
	}

	fmt.Fprintf(w, "Q\n")
	return nil
}
