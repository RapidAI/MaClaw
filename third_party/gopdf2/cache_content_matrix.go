package gopdf

import (
	"fmt"
	"io"
)

// cacheContentMatrix writes a raw cm (concat matrix) operator to the content stream.
type cacheContentMatrix struct {
	a, b, c, d, e, f float64
}

func (cc *cacheContentMatrix) write(w io.Writer, protection *PDFProtection) error {
	_, err := fmt.Fprintf(w, "%.6f %.6f %.6f %.6f %.6f %.6f cm\n",
		cc.a, cc.b, cc.c, cc.d, cc.e, cc.f)
	return err
}
