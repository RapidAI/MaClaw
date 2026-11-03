package gopdf

import (
	"errors"
	"fmt"
)

var (
	ErrBookmarkNotFound = errors.New("bookmark not found")
	ErrBookmarkOutOfRange = errors.New("bookmark index out of range")
)

// BookmarkStyle defines visual styling for a bookmark entry.
type BookmarkStyle struct {
	// Bold makes the bookmark title bold.
	Bold bool
	// Italic makes the bookmark title italic.
	Italic bool
	// Color is the bookmark text color [R, G, B] (0.0-1.0). Zero value = default (black).
	Color [3]float64
	// Collapsed controls whether child bookmarks are initially hidden.
	Collapsed bool
}

// ModifyBookmark modifies the title of a bookmark at the given 0-based index
// in the flat TOC list (as returned by GetTOC).
//
// Example:
//
//	pdf.ModifyBookmark(0, "New Chapter Title")
func (gp *GoPdf) ModifyBookmark(index int, newTitle string) error {
	outlineObjs := gp.getOutlineObjList()
	if index < 0 || index >= len(outlineObjs) {
		return ErrBookmarkOutOfRange
	}
	outlineObjs[index].title = newTitle
	return nil
}

// DeleteBookmark removes a bookmark at the given 0-based index in the flat
// TOC list. Child bookmarks are also removed.
//
// Example:
//
//	pdf.DeleteBookmark(2) // remove the 3rd bookmark
func (gp *GoPdf) DeleteBookmark(index int) error {
	outlineObjs := gp.getOutlineObjList()
	if index < 0 || index >= len(outlineObjs) {
		return ErrBookmarkOutOfRange
	}

	target := outlineObjs[index]

	// Fix linked list: connect prev to next.
	if target.prev > 0 {
		for _, o := range outlineObjs {
			if o.index == target.prev {
				o.next = target.next
				break
			}
		}
	}
	if target.next > 0 {
		for _, o := range outlineObjs {
			if o.index == target.next {
				o.prev = target.prev
				break
			}
		}
	}

	// Update parent's first/last if needed.
	if target.prev <= 0 {
		// This was the first child — update parent's first.
		if target.parent == gp.indexOfOutlinesObj+1 {
			gp.outlines.first = target.next
		} else {
			for _, o := range outlineObjs {
				if o.index == target.parent {
					o.first = target.next
					break
				}
			}
		}
	}
	if target.next <= 0 {
		// This was the last child — update parent's last.
		if target.parent == gp.indexOfOutlinesObj+1 {
			gp.outlines.last = target.prev
		} else {
			for _, o := range outlineObjs {
				if o.index == target.parent {
					o.last = target.prev
					break
				}
			}
		}
	}

	// Null out the object.
	objIdx := target.index - 1 // convert 1-based to 0-based
	if objIdx >= 0 && objIdx < len(gp.pdfObjs) {
		gp.pdfObjs[objIdx] = nullObj{}
	}
	gp.outlines.count--

	return nil
}

// SetBookmarkStyle sets the visual style (color, bold, italic, collapsed)
// for a bookmark at the given 0-based index.
//
// Example:
//
//	pdf.SetBookmarkStyle(0, gopdf.BookmarkStyle{
//	    Bold:   true,
//	    Color:  [3]float64{1, 0, 0}, // red
//	    Collapsed: true,
//	})
func (gp *GoPdf) SetBookmarkStyle(index int, style BookmarkStyle) error {
	outlineObjs := gp.getOutlineObjList()
	if index < 0 || index >= len(outlineObjs) {
		return ErrBookmarkOutOfRange
	}
	outlineObjs[index].color = style.Color
	outlineObjs[index].bold = style.Bold
	outlineObjs[index].italic = style.Italic
	outlineObjs[index].collapsed = style.Collapsed
	return nil
}

// getOutlineObjList returns all OutlineObj instances in document order.
func (gp *GoPdf) getOutlineObjList() []*OutlineObj {
	if gp.outlines == nil || gp.outlines.Count() == 0 {
		return nil
	}

	outlineMap := make(map[int]*OutlineObj)
	for i, obj := range gp.pdfObjs {
		if o, ok := obj.(*OutlineObj); ok {
			outlineMap[i+1] = o
		}
	}

	// Traverse in linked-list order starting from first.
	var result []*OutlineObj
	visited := make(map[int]bool)
	gp.collectOutlineObjs(outlineMap, gp.outlines.first, &result, visited)
	return result
}

func (gp *GoPdf) collectOutlineObjs(m map[int]*OutlineObj, objID int, result *[]*OutlineObj, visited map[int]bool) {
	if objID <= 0 || visited[objID] {
		return
	}
	visited[objID] = true
	o, ok := m[objID]
	if !ok {
		return
	}
	*result = append(*result, o)
	if o.first > 0 {
		gp.collectOutlineObjs(m, o.first, result, visited)
	}
	if o.next > 0 {
		gp.collectOutlineObjs(m, o.next, result, visited)
	}
}

// Ensure fmt is used.
var _ = fmt.Sprintf
