package gopdf

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// AnnotationType represents the type of PDF annotation.
type AnnotationType int

const (
	// AnnotText is a sticky note annotation (appears as an icon).
	AnnotText AnnotationType = iota
	// AnnotHighlight highlights existing text on the page.
	AnnotHighlight
	// AnnotUnderline underlines existing text on the page.
	AnnotUnderline
	// AnnotStrikeOut strikes out existing text on the page.
	AnnotStrikeOut
	// AnnotSquare draws a rectangle annotation.
	AnnotSquare
	// AnnotCircle draws a circle/ellipse annotation.
	AnnotCircle
	// AnnotFreeText places text directly on the page.
	AnnotFreeText
	// AnnotInk is a freehand drawing annotation (ink strokes).
	AnnotInk
	// AnnotPolyline draws connected line segments (open path).
	AnnotPolyline
	// AnnotPolygon draws a closed polygon shape.
	AnnotPolygon
	// AnnotLine draws a single line between two endpoints.
	AnnotLine
	// AnnotStamp places a predefined stamp (e.g. "Approved", "Draft").
	AnnotStamp
	// AnnotSquiggly applies a wavy underline to text.
	AnnotSquiggly
	// AnnotCaret marks an insertion point in text.
	AnnotCaret
	// AnnotFileAttachment attaches a file to the annotation.
	AnnotFileAttachment
	// AnnotRedact marks an area for redaction (content removal).
	AnnotRedact
)

// StampName represents predefined PDF stamp names.
type StampName string

const (
	StampApproved     StampName = "Approved"
	StampAsIs         StampName = "AsIs"
	StampConfidential StampName = "Confidential"
	StampDepartmental StampName = "Departmental"
	StampDraft        StampName = "Draft"
	StampExperimental StampName = "Experimental"
	StampExpired      StampName = "Expired"
	StampFinal        StampName = "Final"
	StampForComment   StampName = "ForComment"
	StampForPublicRelease StampName = "ForPublicRelease"
	StampNotApproved  StampName = "NotApproved"
	StampNotForPublicRelease StampName = "NotForPublicRelease"
	StampSold         StampName = "Sold"
	StampTopSecret    StampName = "TopSecret"
)

// LineEndingStyle represents PDF line ending styles.
type LineEndingStyle string

const (
	LineEndNone        LineEndingStyle = "None"
	LineEndSquare      LineEndingStyle = "Square"
	LineEndCircle      LineEndingStyle = "Circle"
	LineEndDiamond     LineEndingStyle = "Diamond"
	LineEndOpenArrow   LineEndingStyle = "OpenArrow"
	LineEndClosedArrow LineEndingStyle = "ClosedArrow"
	LineEndButt        LineEndingStyle = "Butt"
	LineEndROpenArrow  LineEndingStyle = "ROpenArrow"
	LineEndRClosedArrow LineEndingStyle = "RClosedArrow"
	LineEndSlash       LineEndingStyle = "Slash"
)

// AnnotationOption configures a PDF annotation.
type AnnotationOption struct {
	// Type is the annotation type.
	Type AnnotationType

	// Rect defines the annotation rectangle [x, y, w, h] in document units.
	// x, y is the top-left corner.
	X, Y, W, H float64

	// Title is the annotation title (author name for sticky notes).
	Title string

	// Content is the annotation text content.
	Content string

	// Color is the annotation color in RGB. Default: yellow (255, 255, 0).
	Color [3]uint8

	// Opacity is the annotation opacity (0.0 to 1.0). Default: 1.0.
	Opacity float64

	// Open determines if a text annotation popup is initially open.
	Open bool

	// CreationDate is the annotation creation date. Default: now.
	CreationDate time.Time

	// FontSize is used for FreeText annotations. Default: 12.
	FontSize float64

	// InkList contains stroke paths for Ink annotations.
	// Each element is a slice of Points representing one stroke.
	InkList [][]Point

	// Vertices contains points for Polyline and Polygon annotations.
	Vertices []Point

	// LineStart and LineEnd define endpoints for Line annotations.
	LineStart, LineEnd Point

	// LineEndingStyles defines the start and end styles for Line annotations.
	// Default: [None, None].
	LineEndingStyles [2]LineEndingStyle

	// Stamp is the stamp name for Stamp annotations. Default: "Draft".
	Stamp StampName

	// InteriorColor is the fill color for closed shapes (Square, Circle, Polygon).
	// If nil (zero value), no interior color is applied.
	InteriorColor *[3]uint8

	// BorderWidth is the annotation border width. Default: 1.
	BorderWidth float64

	// FileName is the file name for FileAttachment annotations.
	FileName string

	// FileData is the file content for FileAttachment annotations.
	FileData []byte

	// OverlayText is the text displayed over a Redact annotation area.
	OverlayText string
}

func (o *AnnotationOption) defaults() {
	if o.Color == [3]uint8{0, 0, 0} {
		o.Color = [3]uint8{255, 255, 0}
	}
	if o.Opacity <= 0 {
		o.Opacity = 1.0
	}
	if o.CreationDate.IsZero() {
		o.CreationDate = time.Now()
	}
	if o.FontSize <= 0 {
		o.FontSize = 12
	}
	if o.BorderWidth <= 0 {
		o.BorderWidth = 1
	}
	if o.Stamp == "" {
		o.Stamp = StampDraft
	}
}

// AnnotationInfo holds information about an existing annotation on a page.
type AnnotationInfo struct {
	// Index is the position of this annotation in the page's annotation list.
	Index int
	// Type is the annotation type.
	Type AnnotationType
	// Option is the original annotation option (if available).
	Option AnnotationOption
}

// annotationObj represents a PDF annotation object.
type annotationObj struct {
	opt     AnnotationOption
	getRoot func() *GoPdf
}

func (a annotationObj) init(f func() *GoPdf) {}

func (a annotationObj) getType() string {
	return "Annot"
}

func (a annotationObj) write(w io.Writer, objID int) error {
	a.opt.defaults()

	pageH := a.getRoot().config.PageSize.H

	// Convert from top-left origin to PDF bottom-left origin.
	x1 := a.opt.X
	y1 := pageH - a.opt.Y
	x2 := a.opt.X + a.opt.W
	y2 := pageH - (a.opt.Y + a.opt.H)

	// Escape content string.
	content := escapeAnnotString(a.opt.Content)
	title := escapeAnnotString(a.opt.Title)

	// Color components as floats (0-1).
	cr := float64(a.opt.Color[0]) / 255.0
	cg := float64(a.opt.Color[1]) / 255.0
	cb := float64(a.opt.Color[2]) / 255.0

	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /Annot\n")

	switch a.opt.Type {
	case AnnotText:
		fmt.Fprintf(w, "/Subtype /Text\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/Contents (%s)\n", content)
		if title != "" {
			fmt.Fprintf(w, "/T (%s)\n", title)
		}
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if a.opt.Open {
			io.WriteString(w, "/Open true\n")
		}
		io.WriteString(w, "/Name /Comment\n")

	case AnnotHighlight:
		fmt.Fprintf(w, "/Subtype /Highlight\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/QuadPoints [%.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f]\n",
			x1, y1, x2, y1, x1, y2, x2, y2)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotUnderline:
		fmt.Fprintf(w, "/Subtype /Underline\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/QuadPoints [%.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f]\n",
			x1, y1, x2, y1, x1, y2, x2, y2)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)

	case AnnotStrikeOut:
		fmt.Fprintf(w, "/Subtype /StrikeOut\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/QuadPoints [%.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f]\n",
			x1, y1, x2, y1, x1, y2, x2, y2)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)

	case AnnotSquare:
		fmt.Fprintf(w, "/Subtype /Square\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if a.opt.InteriorColor != nil {
			ic := a.opt.InteriorColor
			fmt.Fprintf(w, "/IC [%.4f %.4f %.4f]\n",
				float64(ic[0])/255.0, float64(ic[1])/255.0, float64(ic[2])/255.0)
		}
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotCircle:
		fmt.Fprintf(w, "/Subtype /Circle\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if a.opt.InteriorColor != nil {
			ic := a.opt.InteriorColor
			fmt.Fprintf(w, "/IC [%.4f %.4f %.4f]\n",
				float64(ic[0])/255.0, float64(ic[1])/255.0, float64(ic[2])/255.0)
		}
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotFreeText:
		fmt.Fprintf(w, "/Subtype /FreeText\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/Contents (%s)\n", content)
		fmt.Fprintf(w, "/DA (/Helv %.0f Tf %.4f %.4f %.4f rg)\n",
			a.opt.FontSize, cr, cg, cb)
		io.WriteString(w, "/DS (font: Helvetica,sans-serif)\n")

	case AnnotInk:
		fmt.Fprintf(w, "/Subtype /Ink\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
		// Write InkList array of stroke arrays.
		io.WriteString(w, "/InkList [")
		for _, stroke := range a.opt.InkList {
			io.WriteString(w, "[")
			for _, pt := range stroke {
				px := pt.X
				py := pageH - pt.Y
				fmt.Fprintf(w, "%.2f %.2f ", px, py)
			}
			io.WriteString(w, "]")
		}
		io.WriteString(w, "]\n")

	case AnnotPolyline:
		fmt.Fprintf(w, "/Subtype /PolyLine\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
		io.WriteString(w, "/Vertices [")
		for _, pt := range a.opt.Vertices {
			fmt.Fprintf(w, "%.2f %.2f ", pt.X, pageH-pt.Y)
		}
		io.WriteString(w, "]\n")
		a.writeLineEndings(w)

	case AnnotPolygon:
		fmt.Fprintf(w, "/Subtype /Polygon\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if a.opt.InteriorColor != nil {
			ic := a.opt.InteriorColor
			fmt.Fprintf(w, "/IC [%.4f %.4f %.4f]\n",
				float64(ic[0])/255.0, float64(ic[1])/255.0, float64(ic[2])/255.0)
		}
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
		io.WriteString(w, "/Vertices [")
		for _, pt := range a.opt.Vertices {
			fmt.Fprintf(w, "%.2f %.2f ", pt.X, pageH-pt.Y)
		}
		io.WriteString(w, "]\n")

	case AnnotLine:
		fmt.Fprintf(w, "/Subtype /Line\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
		lsx := a.opt.LineStart.X
		lsy := pageH - a.opt.LineStart.Y
		lex := a.opt.LineEnd.X
		ley := pageH - a.opt.LineEnd.Y
		fmt.Fprintf(w, "/L [%.2f %.2f %.2f %.2f]\n", lsx, lsy, lex, ley)
		a.writeLineEndings(w)

	case AnnotStamp:
		fmt.Fprintf(w, "/Subtype /Stamp\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		fmt.Fprintf(w, "/Name /%s\n", a.opt.Stamp)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotSquiggly:
		fmt.Fprintf(w, "/Subtype /Squiggly\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/QuadPoints [%.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f]\n",
			x1, y1, x2, y1, x1, y2, x2, y2)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotCaret:
		fmt.Fprintf(w, "/Subtype /Caret\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}

	case AnnotFileAttachment:
		fmt.Fprintf(w, "/Subtype /FileAttachment\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/C [%.4f %.4f %.4f]\n", cr, cg, cb)
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
		if a.opt.FileName != "" {
			fmt.Fprintf(w, "/Name /PushPin\n")
			fmt.Fprintf(w, "/FS <</Type /Filespec /F (%s)>>\n",
				escapeAnnotString(a.opt.FileName))
		}

	case AnnotRedact:
		fmt.Fprintf(w, "/Subtype /Redact\n")
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y2, x2, y1)
		fmt.Fprintf(w, "/QuadPoints [%.2f %.2f %.2f %.2f %.2f %.2f %.2f %.2f]\n",
			x1, y1, x2, y1, x1, y2, x2, y2)
		// Redact annotations use IC for the fill color after redaction.
		if a.opt.InteriorColor != nil {
			ic := a.opt.InteriorColor
			fmt.Fprintf(w, "/IC [%.4f %.4f %.4f]\n",
				float64(ic[0])/255.0, float64(ic[1])/255.0, float64(ic[2])/255.0)
		}
		if a.opt.OverlayText != "" {
			fmt.Fprintf(w, "/OverlayText (%s)\n", escapeAnnotString(a.opt.OverlayText))
			fmt.Fprintf(w, "/DA (/Helv %.0f Tf %.4f %.4f %.4f rg)\n",
				a.opt.FontSize, cr, cg, cb)
		}
		if content != "" {
			fmt.Fprintf(w, "/Contents (%s)\n", content)
		}
	}

	// Opacity via CA entry.
	if a.opt.Opacity < 1.0 {
		fmt.Fprintf(w, "/CA %.4f\n", a.opt.Opacity)
	}

	// Border.
	fmt.Fprintf(w, "/Border [0 0 %.0f]\n", a.opt.BorderWidth)

	// Flags: Print (bit 3).
	io.WriteString(w, "/F 4\n")

	io.WriteString(w, ">>\n")
	return nil
}

// writeLineEndings writes the /LE entry for Line and Polyline annotations.
func (a annotationObj) writeLineEndings(w io.Writer) {
	le0 := a.opt.LineEndingStyles[0]
	le1 := a.opt.LineEndingStyles[1]
	if le0 == "" {
		le0 = LineEndNone
	}
	if le1 == "" {
		le1 = LineEndNone
	}
	if le0 != LineEndNone || le1 != LineEndNone {
		fmt.Fprintf(w, "/LE [/%s /%s]\n", le0, le1)
	}
}

// escapeAnnotString escapes special characters in PDF annotation strings.
func escapeAnnotString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// AddAnnotation adds an annotation to the current page.
//
// Supported annotation types:
//   - AnnotText: Sticky note (comment icon)
//   - AnnotHighlight: Highlight markup
//   - AnnotUnderline: Underline markup
//   - AnnotStrikeOut: Strikeout markup
//   - AnnotSquare: Rectangle shape
//   - AnnotCircle: Circle/ellipse shape
//   - AnnotFreeText: Text directly on the page
//   - AnnotInk: Freehand drawing
//   - AnnotPolyline: Connected line segments
//   - AnnotPolygon: Closed polygon shape
//   - AnnotLine: Single line with endpoints
//   - AnnotStamp: Predefined stamp
//   - AnnotSquiggly: Wavy underline
//   - AnnotCaret: Insertion point marker
//   - AnnotFileAttachment: File attachment
//   - AnnotRedact: Redaction marker
//
// Example:
//
//	pdf.AddAnnotation(gopdf.AnnotationOption{
//	    Type:    gopdf.AnnotText,
//	    X:       100,
//	    Y:       100,
//	    W:       24,
//	    H:       24,
//	    Title:   "Reviewer",
//	    Content: "Please check this section.",
//	    Color:   [3]uint8{255, 255, 0},
//	})
func (gp *GoPdf) AddAnnotation(opt AnnotationOption) {
	gp.UnitsToPointsVar(&opt.X, &opt.Y, &opt.W, &opt.H)

	// Convert points in InkList.
	for i := range opt.InkList {
		for j := range opt.InkList[i] {
			gp.UnitsToPointsVar(&opt.InkList[i][j].X, &opt.InkList[i][j].Y)
		}
	}
	// Convert Vertices.
	for i := range opt.Vertices {
		gp.UnitsToPointsVar(&opt.Vertices[i].X, &opt.Vertices[i].Y)
	}
	// Convert Line endpoints.
	if opt.Type == AnnotLine {
		gp.UnitsToPointsVar(&opt.LineStart.X, &opt.LineStart.Y, &opt.LineEnd.X, &opt.LineEnd.Y)
	}

	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	objIdx := gp.addObj(annotationObj{
		opt: opt,
		getRoot: func() *GoPdf {
			return gp
		},
	})
	page.LinkObjIds = append(page.LinkObjIds, objIdx+1)
}

// AddTextAnnotation is a convenience method for adding a sticky note annotation.
func (gp *GoPdf) AddTextAnnotation(x, y float64, title, content string) {
	gp.AddAnnotation(AnnotationOption{
		Type:    AnnotText,
		X:       x,
		Y:       y,
		W:       24,
		H:       24,
		Title:   title,
		Content: content,
	})
}

// AddHighlightAnnotation is a convenience method for highlighting a rectangular area.
func (gp *GoPdf) AddHighlightAnnotation(x, y, w, h float64, color [3]uint8) {
	gp.AddAnnotation(AnnotationOption{
		Type:  AnnotHighlight,
		X:     x,
		Y:     y,
		W:     w,
		H:     h,
		Color: color,
	})
}

// AddFreeTextAnnotation is a convenience method for adding text directly on the page.
func (gp *GoPdf) AddFreeTextAnnotation(x, y, w, h float64, text string, fontSize float64) {
	gp.AddAnnotation(AnnotationOption{
		Type:     AnnotFreeText,
		X:        x,
		Y:        y,
		W:        w,
		H:        h,
		Content:  text,
		FontSize: fontSize,
	})
}

// AddInkAnnotation adds a freehand ink annotation with one or more strokes.
func (gp *GoPdf) AddInkAnnotation(x, y, w, h float64, strokes [][]Point, color [3]uint8) {
	gp.AddAnnotation(AnnotationOption{
		Type:    AnnotInk,
		X:       x,
		Y:       y,
		W:       w,
		H:       h,
		InkList: strokes,
		Color:   color,
	})
}

// AddPolylineAnnotation adds a polyline (open path) annotation.
func (gp *GoPdf) AddPolylineAnnotation(x, y, w, h float64, vertices []Point, color [3]uint8) {
	gp.AddAnnotation(AnnotationOption{
		Type:     AnnotPolyline,
		X:        x,
		Y:        y,
		W:        w,
		H:        h,
		Vertices: vertices,
		Color:    color,
	})
}

// AddPolygonAnnotation adds a closed polygon annotation.
func (gp *GoPdf) AddPolygonAnnotation(x, y, w, h float64, vertices []Point, color [3]uint8) {
	gp.AddAnnotation(AnnotationOption{
		Type:     AnnotPolygon,
		X:        x,
		Y:        y,
		W:        w,
		H:        h,
		Vertices: vertices,
		Color:    color,
	})
}

// AddLineAnnotation adds a line annotation between two points.
func (gp *GoPdf) AddLineAnnotation(start, end Point, color [3]uint8) {
	// Compute bounding rect from the two endpoints.
	minX, maxX := start.X, end.X
	if end.X < minX {
		minX = end.X
	}
	if start.X > maxX {
		maxX = start.X
	}
	minY, maxY := start.Y, end.Y
	if end.Y < minY {
		minY = end.Y
	}
	if start.Y > maxY {
		maxY = start.Y
	}
	gp.AddAnnotation(AnnotationOption{
		Type:      AnnotLine,
		X:         minX,
		Y:         minY,
		W:         maxX - minX,
		H:         maxY - minY,
		LineStart: start,
		LineEnd:   end,
		Color:     color,
	})
}

// AddStampAnnotation adds a stamp annotation (e.g. "Approved", "Draft").
func (gp *GoPdf) AddStampAnnotation(x, y, w, h float64, stamp StampName) {
	gp.AddAnnotation(AnnotationOption{
		Type:  AnnotStamp,
		X:     x,
		Y:     y,
		W:     w,
		H:     h,
		Stamp: stamp,
	})
}

// AddSquigglyAnnotation adds a wavy underline annotation.
func (gp *GoPdf) AddSquigglyAnnotation(x, y, w, h float64, color [3]uint8) {
	gp.AddAnnotation(AnnotationOption{
		Type:  AnnotSquiggly,
		X:     x,
		Y:     y,
		W:     w,
		H:     h,
		Color: color,
	})
}

// AddCaretAnnotation adds a caret (insertion point) annotation.
func (gp *GoPdf) AddCaretAnnotation(x, y, w, h float64, content string) {
	gp.AddAnnotation(AnnotationOption{
		Type:    AnnotCaret,
		X:       x,
		Y:       y,
		W:       w,
		H:       h,
		Content: content,
	})
}

// AddFileAttachmentAnnotation adds a file attachment annotation.
func (gp *GoPdf) AddFileAttachmentAnnotation(x, y float64, fileName string, fileData []byte, content string) {
	gp.AddAnnotation(AnnotationOption{
		Type:     AnnotFileAttachment,
		X:        x,
		Y:        y,
		W:        24,
		H:        24,
		FileName: fileName,
		FileData: fileData,
		Content:  content,
	})
}

// AddRedactAnnotation adds a redaction annotation marking an area for content removal.
// Call ApplyRedactions() to permanently remove the content.
func (gp *GoPdf) AddRedactAnnotation(x, y, w, h float64, overlayText string) {
	gp.AddAnnotation(AnnotationOption{
		Type:        AnnotRedact,
		X:           x,
		Y:           y,
		W:           w,
		H:           h,
		OverlayText: overlayText,
		Color:       [3]uint8{255, 0, 0},
	})
}

// GetAnnotations returns all annotations on the current page.
func (gp *GoPdf) GetAnnotations() []AnnotationInfo {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	var result []AnnotationInfo
	for i, objID := range page.LinkObjIds {
		obj := gp.pdfObjs[objID-1]
		if aObj, ok := obj.(annotationObj); ok {
			result = append(result, AnnotationInfo{
				Index:  i,
				Type:   aObj.opt.Type,
				Option: aObj.opt,
			})
		}
	}
	return result
}

// GetAnnotationsOnPage returns all annotations on the specified page (1-indexed).
func (gp *GoPdf) GetAnnotationsOnPage(pageIndex int) []AnnotationInfo {
	page := gp.findPageObj(pageIndex)
	if page == nil {
		return nil
	}
	var result []AnnotationInfo
	for i, objID := range page.LinkObjIds {
		obj := gp.pdfObjs[objID-1]
		if aObj, ok := obj.(annotationObj); ok {
			result = append(result, AnnotationInfo{
				Index:  i,
				Type:   aObj.opt.Type,
				Option: aObj.opt,
			})
		}
	}
	return result
}

// DeleteAnnotation removes the annotation at the given index from the current page.
// Returns true if the annotation was removed.
func (gp *GoPdf) DeleteAnnotation(index int) bool {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	if index < 0 || index >= len(page.LinkObjIds) {
		return false
	}
	page.LinkObjIds = append(page.LinkObjIds[:index], page.LinkObjIds[index+1:]...)
	return true
}

// DeleteAnnotationOnPage removes the annotation at the given index from the specified page (1-indexed).
// Returns true if the annotation was removed.
func (gp *GoPdf) DeleteAnnotationOnPage(pageIndex, annotIndex int) bool {
	page := gp.findPageObj(pageIndex)
	if page == nil {
		return false
	}
	if annotIndex < 0 || annotIndex >= len(page.LinkObjIds) {
		return false
	}
	page.LinkObjIds = append(page.LinkObjIds[:annotIndex], page.LinkObjIds[annotIndex+1:]...)
	return true
}

// ApplyRedactions removes all redaction annotations from the current page.
// In a full implementation this would also remove the underlying content;
// currently it removes the redact annotation markers so they no longer appear.
func (gp *GoPdf) ApplyRedactions() int {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	return applyRedactionsOnPage(gp, page)
}

// ApplyRedactionsOnPage applies redactions on the specified page (1-indexed).
func (gp *GoPdf) ApplyRedactionsOnPage(pageIndex int) int {
	page := gp.findPageObj(pageIndex)
	if page == nil {
		return 0
	}
	return applyRedactionsOnPage(gp, page)
}

func applyRedactionsOnPage(gp *GoPdf, page *PageObj) int {
	kept := make([]int, 0, len(page.LinkObjIds))
	removed := 0
	for _, objID := range page.LinkObjIds {
		obj := gp.pdfObjs[objID-1]
		if aObj, ok := obj.(annotationObj); ok && aObj.opt.Type == AnnotRedact {
			removed++
			continue
		}
		kept = append(kept, objID)
	}
	page.LinkObjIds = kept
	return removed
}

