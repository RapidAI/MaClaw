package gopdf

import (
	"errors"
	"fmt"
)

// ContentElementType identifies the type of a content element.
type ContentElementType int

const (
	// ElementText represents a text element.
	ElementText ContentElementType = iota
	// ElementImage represents an image element.
	ElementImage
	// ElementLine represents a line element.
	ElementLine
	// ElementRectangle represents a rectangle element.
	ElementRectangle
	// ElementOval represents an oval/ellipse element.
	ElementOval
	// ElementPolygon represents a polygon element.
	ElementPolygon
	// ElementCurve represents a Bézier curve element.
	ElementCurve
	// ElementPolyline represents a polyline (open path) element.
	ElementPolyline
	// ElementSector represents a sector (pie/fan shape) element.
	ElementSector
	// ElementImportedTemplate represents an imported PDF template.
	ElementImportedTemplate
	// ElementLineWidth represents a line width setting.
	ElementLineWidth
	// ElementLineType represents a line type setting.
	ElementLineType
	// ElementCustomLineType represents a custom dash pattern.
	ElementCustomLineType
	// ElementGray represents a grayscale fill/stroke setting.
	ElementGray
	// ElementColorRGB represents an RGB color setting.
	ElementColorRGB
	// ElementColorCMYK represents a CMYK color setting.
	ElementColorCMYK
	// ElementColorSpace represents a color space setting.
	ElementColorSpace
	// ElementRotate represents a rotation transform.
	ElementRotate
	// ElementClipPolygon represents a clipping path.
	ElementClipPolygon
	// ElementSaveGState represents a graphics state save (q).
	ElementSaveGState
	// ElementRestoreGState represents a graphics state restore (Q).
	ElementRestoreGState
	// ElementUnknown represents an unrecognized element type.
	ElementUnknown
)

// String returns a human-readable name for the element type.
func (t ContentElementType) String() string {
	switch t {
	case ElementText:
		return "Text"
	case ElementImage:
		return "Image"
	case ElementLine:
		return "Line"
	case ElementRectangle:
		return "Rectangle"
	case ElementOval:
		return "Oval"
	case ElementPolygon:
		return "Polygon"
	case ElementCurve:
		return "Curve"
	case ElementPolyline:
		return "Polyline"
	case ElementSector:
		return "Sector"
	case ElementImportedTemplate:
		return "ImportedTemplate"
	case ElementLineWidth:
		return "LineWidth"
	case ElementLineType:
		return "LineType"
	case ElementCustomLineType:
		return "CustomLineType"
	case ElementGray:
		return "Gray"
	case ElementColorRGB:
		return "ColorRGB"
	case ElementColorCMYK:
		return "ColorCMYK"
	case ElementColorSpace:
		return "ColorSpace"
	case ElementRotate:
		return "Rotate"
	case ElementClipPolygon:
		return "ClipPolygon"
	case ElementSaveGState:
		return "SaveGState"
	case ElementRestoreGState:
		return "RestoreGState"
	default:
		return "Unknown"
	}
}

// ContentElement is a public descriptor for a single content element on a page.
// It exposes the element's type, position, dimensions, and (for text) the string content.
// The Index field is the 0-based position in the page's content stream cache.
type ContentElement struct {
	// Index is the 0-based position in the page's content cache array.
	Index int
	// Type is the element type.
	Type ContentElementType
	// X is the X coordinate (for elements that have one).
	X float64
	// Y is the Y coordinate (for elements that have one).
	Y float64
	// X2 is the second X coordinate (for lines, ovals).
	X2 float64
	// Y2 is the second Y coordinate (for lines, ovals).
	Y2 float64
	// Width is the width (for rectangles, images).
	Width float64
	// Height is the height (for rectangles, images).
	Height float64
	// Text is the text content (for text elements only).
	Text string
	// FontSize is the font size (for text elements only).
	FontSize float64
}

var (
	ErrElementIndexOutOfRange = errors.New("element index out of range")
	ErrElementTypeMismatch    = errors.New("element type mismatch")
	ErrContentObjNotFound     = errors.New("content object not found for page")
)

// classifyElement returns the ContentElementType for an ICacheContent.
func classifyElement(c ICacheContent) ContentElementType {
	switch c.(type) {
	case *cacheContentText:
		return ElementText
	case *cacheContentImage:
		return ElementImage
	case *cacheContentLine:
		return ElementLine
	case cacheContentRectangle:
		return ElementRectangle
	case *cacheContentOval:
		return ElementOval
	case *cacheContentPolygon:
		return ElementPolygon
	case *cacheContentCurve:
		return ElementCurve
	case *cacheContentPolyline:
		return ElementPolyline
	case *cacheContentSector:
		return ElementSector
	case *cacheContentImportedTemplate:
		return ElementImportedTemplate
	case *cacheContentLineWidth:
		return ElementLineWidth
	case *cacheContentLineType:
		return ElementLineType
	case *cacheContentCustomLineType:
		return ElementCustomLineType
	case *cacheContentGray:
		return ElementGray
	case *cacheContentColorRGB:
		return ElementColorRGB
	case *cacheContentColorCMYK:
		return ElementColorCMYK
	case *cacheColorSpace:
		return ElementColorSpace
	case *cacheContentRotate:
		return ElementRotate
	case *cacheContentClipPolygon:
		return ElementClipPolygon
	case *cacheContentSaveGraphicsState:
		return ElementSaveGState
	case *cacheContentRestoreGraphicsState:
		return ElementRestoreGState
	default:
		return ElementUnknown
	}
}

// buildElement creates a ContentElement descriptor from an ICacheContent at the given index.
func buildElement(index int, c ICacheContent) ContentElement {
	elem := ContentElement{
		Index: index,
		Type:  classifyElement(c),
	}
	switch v := c.(type) {
	case *cacheContentText:
		elem.X = v.x
		elem.Y = v.y
		elem.Text = v.text
		elem.FontSize = v.fontSize
	case *cacheContentImage:
		elem.X = v.x
		elem.Y = v.y
		elem.Width = v.rect.W
		elem.Height = v.rect.H
	case *cacheContentLine:
		elem.X = v.x1
		elem.Y = v.y1
		elem.X2 = v.x2
		elem.Y2 = v.y2
	case cacheContentRectangle:
		elem.X = v.x
		elem.Y = v.y
		elem.Width = v.width
		elem.Height = v.height
	case *cacheContentOval:
		elem.X = v.x1
		elem.Y = v.y1
		elem.X2 = v.x2
		elem.Y2 = v.y2
	case *cacheContentPolygon:
		if len(v.points) > 0 {
			elem.X = v.points[0].X
			elem.Y = v.points[0].Y
		}
	case *cacheContentCurve:
		elem.X = v.x0
		elem.Y = v.y0
		elem.X2 = v.x3
		elem.Y2 = v.y3
	case *cacheContentPolyline:
		if len(v.points) > 0 {
			elem.X = v.points[0].X
			elem.Y = v.points[0].Y
		}
	case *cacheContentSector:
		elem.X = v.cx
		elem.Y = v.cy
		elem.Width = v.r
	case *cacheContentImportedTemplate:
		elem.X = v.tX
		elem.Y = v.tY
	case *cacheContentLineWidth:
		elem.Width = v.width
	}
	return elem
}

// findContentObj finds the ContentObj for the given 1-based page number.
// This mirrors SetPage logic: iterate pdfObjs counting ContentObj instances.
func (gp *GoPdf) findContentObj(pageNo int) *ContentObj {
	count := 0
	for _, obj := range gp.pdfObjs {
		if c, ok := obj.(*ContentObj); ok {
			count++
			if count == pageNo {
				return c
			}
		}
	}
	return nil
}

// GetPageElements returns all content elements on the specified page (1-based).
// Each element includes its type, position, dimensions, and index for further operations.
//
// Example:
//
//	elements := pdf.GetPageElements(1)
//	for _, e := range elements {
//	    fmt.Printf("[%d] %s at (%.1f, %.1f)\n", e.Index, e.Type, e.X, e.Y)
//	}
func (gp *GoPdf) GetPageElements(pageNo int) ([]ContentElement, error) {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return nil, fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	caches := content.listCache.caches
	elements := make([]ContentElement, len(caches))
	for i, c := range caches {
		elements[i] = buildElement(i, c)
	}
	return elements, nil
}

// GetPageElementsByType returns only elements of the specified type on a page.
func (gp *GoPdf) GetPageElementsByType(pageNo int, elemType ContentElementType) ([]ContentElement, error) {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return nil, fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	var result []ContentElement
	for i, c := range content.listCache.caches {
		if classifyElement(c) == elemType {
			result = append(result, buildElement(i, c))
		}
	}
	return result, nil
}

// GetPageElementCount returns the number of content elements on a page.
func (gp *GoPdf) GetPageElementCount(pageNo int) (int, error) {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return 0, fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	return len(content.listCache.caches), nil
}

// DeleteElement removes a single content element by its 0-based index on the given page.
//
// Example:
//
//	pdf.DeleteElement(1, 0) // remove first element on page 1
func (gp *GoPdf) DeleteElement(pageNo int, elementIndex int) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	if elementIndex < 0 || elementIndex >= len(content.listCache.caches) {
		return ErrElementIndexOutOfRange
	}
	content.listCache.caches = append(
		content.listCache.caches[:elementIndex],
		content.listCache.caches[elementIndex+1:]...,
	)
	return nil
}

// DeleteElementsByType removes all elements of the specified type from a page.
// Returns the number of elements removed.
//
// Example:
//
//	n := pdf.DeleteElementsByType(1, gopdf.ElementLine) // remove all lines from page 1
func (gp *GoPdf) DeleteElementsByType(pageNo int, elemType ContentElementType) (int, error) {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return 0, fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	original := len(content.listCache.caches)
	filtered := make([]ICacheContent, 0, original)
	for _, c := range content.listCache.caches {
		if classifyElement(c) != elemType {
			filtered = append(filtered, c)
		}
	}
	content.listCache.caches = filtered
	return original - len(filtered), nil
}

// DeleteElementsInRect removes all elements whose primary position (X, Y)
// falls within the given rectangle. Returns the number removed.
//
// Example:
//
//	n := pdf.DeleteElementsInRect(1, 0, 0, 100, 100) // remove elements in top-left 100x100 area
func (gp *GoPdf) DeleteElementsInRect(pageNo int, rx, ry, rw, rh float64) (int, error) {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return 0, fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	original := len(content.listCache.caches)
	filtered := make([]ICacheContent, 0, original)
	for _, c := range content.listCache.caches {
		elem := buildElement(0, c)
		if elem.X >= rx && elem.X <= rx+rw && elem.Y >= ry && elem.Y <= ry+rh {
			continue // inside rect, remove
		}
		filtered = append(filtered, c)
	}
	content.listCache.caches = filtered
	return original - len(filtered), nil
}

// ClearPage removes all content elements from a page, leaving it blank.
//
// Example:
//
//	pdf.ClearPage(1) // clear all content from page 1
func (gp *GoPdf) ClearPage(pageNo int) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	content.listCache.caches = nil
	return nil
}

// ModifyTextElement changes the text content of a text element at the given index.
// Returns an error if the element is not a text element.
//
// Example:
//
//	pdf.ModifyTextElement(1, 0, "New text content")
func (gp *GoPdf) ModifyTextElement(pageNo int, elementIndex int, newText string) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	if elementIndex < 0 || elementIndex >= len(content.listCache.caches) {
		return ErrElementIndexOutOfRange
	}
	textElem, ok := content.listCache.caches[elementIndex].(*cacheContentText)
	if !ok {
		return fmt.Errorf("%w: element %d is %T, not text", ErrElementTypeMismatch, elementIndex, content.listCache.caches[elementIndex])
	}
	textElem.text = newText
	// Recalculate content dimensions if font subset is available.
	if textElem.fontSubset != nil {
		textElem.createContent()
	}
	return nil
}

// ModifyElementPosition moves an element to a new (x, y) position.
// Works for text, image, line (moves start point), rectangle, oval, and polygon elements.
//
// Example:
//
//	pdf.ModifyElementPosition(1, 0, 100, 200) // move element to (100, 200)
func (gp *GoPdf) ModifyElementPosition(pageNo int, elementIndex int, x, y float64) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	if elementIndex < 0 || elementIndex >= len(content.listCache.caches) {
		return ErrElementIndexOutOfRange
	}
	c := content.listCache.caches[elementIndex]
	switch v := c.(type) {
	case *cacheContentText:
		v.x = x
		v.y = y
	case *cacheContentImage:
		v.x = x
		v.y = y
	case *cacheContentLine:
		// Shift both endpoints by the delta.
		dx := x - v.x1
		dy := y - v.y1
		v.x1 = x
		v.y1 = y
		v.x2 += dx
		v.y2 += dy
	case cacheContentRectangle:
		v.x = x
		v.y = y
		content.listCache.caches[elementIndex] = v // value type, must reassign
	case *cacheContentOval:
		dx := x - v.x1
		dy := y - v.y1
		v.x1 = x
		v.y1 = y
		v.x2 += dx
		v.y2 += dy
	case *cacheContentPolygon:
		if len(v.points) > 0 {
			dx := x - v.points[0].X
			dy := y - v.points[0].Y
			for i := range v.points {
				v.points[i].X += dx
				v.points[i].Y += dy
			}
		}
	case *cacheContentPolyline:
		if len(v.points) > 0 {
			dx := x - v.points[0].X
			dy := y - v.points[0].Y
			for i := range v.points {
				v.points[i].X += dx
				v.points[i].Y += dy
			}
		}
	case *cacheContentSector:
		v.cx = x
		v.cy = y
	case *cacheContentCurve:
		dx := x - v.x0
		dy := y - v.y0
		v.x0 = x
		v.y0 = y
		v.x1 += dx
		v.y1 += dy
		v.x2 += dx
		v.y2 += dy
		v.x3 += dx
		v.y3 += dy
	default:
		return fmt.Errorf("%w: cannot reposition element type %T", ErrElementTypeMismatch, c)
	}
	return nil
}

// InsertLineElement adds a line to an existing page's content stream.
// Coordinates are in the same units as Line().
//
// Example:
//
//	pdf.InsertLineElement(1, 10, 10, 200, 10) // horizontal line on page 1
func (gp *GoPdf) InsertLineElement(pageNo int, x1, y1, x2, y2 float64) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	page := gp.findPageObj(pageNo)
	pageHeight := gp.config.PageSize.H
	if page != nil && !page.pageOption.isEmpty() {
		pageHeight = page.pageOption.PageSize.H
	}
	cache := &cacheContentLine{
		pageHeight: pageHeight,
		x1:         x1,
		y1:         y1,
		x2:         x2,
		y2:         y2,
	}
	content.listCache.append(cache)
	return nil
}

// InsertRectElement adds a rectangle to an existing page's content stream.
// style: "D" (draw/stroke), "F" (fill), "DF"/"FD" (draw and fill).
//
// Example:
//
//	pdf.InsertRectElement(1, 50, 50, 100, 80, "D")
func (gp *GoPdf) InsertRectElement(pageNo int, x, y, w, h float64, style string) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	page := gp.findPageObj(pageNo)
	pageHeight := gp.config.PageSize.H
	if page != nil && !page.pageOption.isEmpty() {
		pageHeight = page.pageOption.PageSize.H
	}
	ps := PaintStyle(style)
	if ps == "" {
		ps = DrawPaintStyle
	}
	cache := cacheContentRectangle{
		pageHeight: pageHeight,
		x:          x,
		y:          y,
		width:      w,
		height:     h,
		style:      ps,
	}
	content.listCache.append(cache)
	return nil
}

// InsertOvalElement adds an oval/ellipse to an existing page's content stream.
//
// Example:
//
//	pdf.InsertOvalElement(1, 50, 50, 150, 100)
func (gp *GoPdf) InsertOvalElement(pageNo int, x1, y1, x2, y2 float64) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	page := gp.findPageObj(pageNo)
	pageHeight := gp.config.PageSize.H
	if page != nil && !page.pageOption.isEmpty() {
		pageHeight = page.pageOption.PageSize.H
	}
	cache := &cacheContentOval{
		pageHeight: pageHeight,
		x1:         x1,
		y1:         y1,
		x2:         x2,
		y2:         y2,
	}
	content.listCache.append(cache)
	return nil
}

// ReplaceElement replaces the content element at the given index with a new ICacheContent.
// This is a low-level method for advanced use cases.
func (gp *GoPdf) ReplaceElement(pageNo int, elementIndex int, newElement ICacheContent) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	if elementIndex < 0 || elementIndex >= len(content.listCache.caches) {
		return ErrElementIndexOutOfRange
	}
	content.listCache.caches[elementIndex] = newElement
	return nil
}

// InsertElementAt inserts a new ICacheContent at the specified index position.
// Elements at and after the index are shifted right.
func (gp *GoPdf) InsertElementAt(pageNo int, elementIndex int, newElement ICacheContent) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}
	caches := content.listCache.caches
	if elementIndex < 0 || elementIndex > len(caches) {
		return ErrElementIndexOutOfRange
	}
	// Insert at position.
	content.listCache.caches = append(caches[:elementIndex], append([]ICacheContent{newElement}, caches[elementIndex:]...)...)
	return nil
}
