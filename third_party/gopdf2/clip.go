package gopdf

// ============================================================
// Region-scoped extraction and rendering. PyMuPDF parity:
// page.get_pixmap(clip=rect) and page.get_text(clip=rect).
// Coordinates are visual page points (top-down, honoring /Rotate).
// ============================================================

// ClipRect is a rectangular region of a page in visual coordinates
// (top-down points; with /Rotate the visual, not user, space).
type ClipRect struct {
	X, Y float64
	W, H float64
}

// WordsInRect filters words to those intersecting the region. A word counts
// when its center point falls inside the rect.
func WordsInRect(words []TextWord, clip ClipRect) []TextWord {
	var out []TextWord
	for _, w := range words {
		cx := w.X + w.Width/2
		cy := w.Y - w.Height/2 // w.Y is the baseline; center above it
		if cx >= clip.X && cx <= clip.X+clip.W && cy >= clip.Y && cy <= clip.Y+clip.H {
			out = append(out, w)
		}
	}
	return out
}

// ExtractTextWordsInRect extracts the words inside a page region.
func ExtractTextWordsInRect(pdfData []byte, pageIndex int, clip ClipRect) ([]TextWord, error) {
	lines, err := ExtractPageLines(pdfData, pageIndex)
	if err != nil {
		return nil, err
	}
	var words []TextWord
	for _, ln := range lines {
		words = append(words, ln.Words...)
	}
	return WordsInRect(words, clip), nil
}

// ExtractTextInRect extracts the text of a page region as lines joined with
// newlines (reading order).
func ExtractTextInRect(pdfData []byte, pageIndex int, clip ClipRect) (string, error) {
	lines, err := ExtractPageLines(pdfData, pageIndex)
	if err != nil {
		return "", err
	}
	out := ""
	for _, ln := range lines {
		inRect := false
		for _, w := range ln.Words {
			cx := w.X + w.Width/2
			cy := w.Y - w.Height/2
			if cx >= clip.X && cx <= clip.X+clip.W && cy >= clip.Y && cy <= clip.Y+clip.H {
				inRect = true
				break
			}
		}
		if inRect {
			out += ln.Text + "\n"
		}
	}
	return out, nil
}
