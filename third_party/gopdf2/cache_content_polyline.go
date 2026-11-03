package gopdf

import (
	"fmt"
	"io"
)

type cacheContentPolyline struct {
	pageHeight float64
	points     []Point
	opts       polylineOptions
}

type polylineOptions struct {
	extGStateIndexes []int
}

func (c *cacheContentPolyline) write(w io.Writer, protection *PDFProtection) error {
	if len(c.points) < 2 {
		return nil
	}

	fmt.Fprintf(w, "q\n")
	for _, extGStateIndex := range c.opts.extGStateIndexes {
		fmt.Fprintf(w, "/GS%d gs\n", extGStateIndex)
	}

	for i, point := range c.points {
		fmt.Fprintf(w, "%.2f %.2f", point.X, c.pageHeight-point.Y)
		if i == 0 {
			fmt.Fprintf(w, " m ")
		} else {
			fmt.Fprintf(w, " l ")
		}
	}

	// Stroke only, no close path (unlike polygon which uses s/f/b)
	fmt.Fprintf(w, "S\n")
	fmt.Fprintf(w, "Q\n")
	return nil
}
