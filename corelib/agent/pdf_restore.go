package agent

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

// ScanTextLine is one OCR line in the recognized image's pixel space.
// BBox is x, y, width, height with the origin at the image's top-left.
type ScanTextLine struct {
	Text    string `json:"text"`
	BBox    [4]int `json:"bbox"`
	Formula bool   `json:"formula,omitempty"`
}

// ScanImagePlacement maps that image onto the PDF page.
// X and Y are the image's top-left in the same space GoPDF2 Cell uses.
// A quarter turn stores the pre-rotation rectangle there. Turn is the
// rotation about that rectangle's center, in degrees. TurnA..TurnF are the
// image CTM, and TurnPageH is the page height those numbers use.
type ScanImagePlacement struct {
	X, Y               float64
	DisplayW, DisplayH float64
	PixelW, PixelH     int
	// FullPage is set when the content stream has no usable display size,
	// so the image is treated as covering the page.
	FullPage bool
	// MirrorX and MirrorY reflect the samples inside the rectangle.
	// On an axis-aligned image a negative component sets them, and the text
	// layer uses the same reflection. A turned image sets MirrorY when the
	// matrix flips; its text layer uses TurnA..TurnF instead.
	MirrorX, MirrorY                         bool
	Turn                                     float64
	TurnPageH                                float64
	TurnA, TurnB, TurnC, TurnD, TurnE, TurnF float64
}

// PlacedScanLine is a line mapped onto the page, ready to draw.
type PlacedScanLine struct {
	Text    string  `json:"text"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	Formula bool    `json:"formula,omitempty"`
}

// ScanPageImage is one embedded image drawn back onto the page.
type ScanPageImage struct {
	Payload   []byte
	Placement ScanImagePlacement
}

// ScanPageView is what one page offers a restore pass.
// Payload and Placement are the largest image, which is the one OCR reads.
// Images is every image on the page, in reading order, and all of them are drawn.
type ScanPageView struct {
	Page      int
	PageCount int
	// HasText means the page is digital: extractable text, and no image that
	// covers the page. A small logo does not clear this.
	HasText   bool
	NoPicture bool
	Payload   []byte
	Placement ScanImagePlacement
	Images    []ScanPageImage
}

// PDFIsEncrypted reports a classic trailer /Encrypt entry.
// OpenPDF still returns ErrEncryptedPDF for files this check misses.
func PDFIsEncrypted(data []byte) bool {
	idx := bytes.LastIndex(data, []byte("trailer"))
	if idx < 0 {
		return false
	}
	return bytes.Contains(data[idx:], []byte("/Encrypt"))
}

// ScanRestore reads pages of one PDF. Page count, page size, and whether a
// page object itself omits MediaBox come from the one ledongthuc reader that
// also checks text. The image reader keeps that page tree for later pages.
type ScanRestore struct {
	data       []byte
	reader     pdfPageImageReader
	pages      map[int]ScanPageView
	extracts   int
	geomReads  int
	geomDone   bool
	geom       map[int][2]float64
	stampSize  int64
	stampMtime int64
	stamped    bool
}

// NewScanRestore starts a restore read of data. The caller uses one value
// for every page of that file.
func NewScanRestore(data []byte) *ScanRestore {
	return &ScanRestore{data: data, reader: pdfPageImageReader{data: data, retainClassic: true}}
}

// ScanPageForRestore reads one 1-based page. A page is digital when it has
// extractable text and no page-covering image. A page number or a watermark
// on a scan does not make the page digital. OCR reads the largest image. A
// header mark above the scan is not that image. Every image is still drawn.
// Pixel size comes from the image bytes, then the dictionary.
func ScanPageForRestore(data []byte, page int) (ScanPageView, error) {
	return NewScanRestore(data).Page(page)
}

// BindSource records the source file identity these bytes were read from.
// A later open of the same size and mtime can keep this value.
func (s *ScanRestore) BindSource(size, mtime int64) {
	if s == nil || s.stamped {
		return
	}
	s.stampSize = size
	s.stampMtime = mtime
	s.stamped = true
}

// Matches reports that this read is still the file at size and mtime.
func (s *ScanRestore) Matches(size, mtime int64) bool {
	return s != nil && s.stamped && s.stampSize == size && s.stampMtime == mtime
}

// Bytes is the PDF this value reads.
func (s *ScanRestore) Bytes() []byte {
	if s == nil {
		return nil
	}
	return s.data
}

// Page reads one 1-based page from this document. A page already read on
// this value is returned again without walking the file.
func (s *ScanRestore) Page(page int) (ScanPageView, error) {
	if s != nil {
		if view, ok := s.pages[page]; ok {
			return view, nil
		}
	}
	view, err := s.readPage(page)
	if err != nil || s == nil {
		return view, err
	}
	if s.pages == nil {
		s.pages = make(map[int]ScanPageView)
	}
	s.pages[page] = view
	return view, nil
}

func (s *ScanRestore) readPage(page int) (view ScanPageView, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			view = ScanPageView{}
			err = fmt.Errorf("read scan page: %v", recovered)
		}
	}()
	if s == nil || page < 1 || len(s.data) == 0 {
		return ScanPageView{}, fmt.Errorf("page %d is out of range", page)
	}
	count, countErr := s.reader.numPages()
	if countErr != nil || count < 1 {
		if countErr != nil {
			return ScanPageView{}, countErr
		}
		return ScanPageView{}, fmt.Errorf("PDF has no pages")
	}
	if err := validatePDFPageCount(count); err != nil {
		return ScanPageView{}, err
	}
	if page > count {
		return ScanPageView{}, fmt.Errorf("page %d is out of range", page)
	}
	view.Page = page
	view.PageCount = count
	// Text with no image XObject is digital. Looking at the content stream
	// for a placement would parse every text page. A page with no text is a
	// scan even when its image is small; the cover test applies only when
	// text is present, so a logo cannot hide a scan and a page number cannot
	// hide one either.
	hasText := s.reader.pageHasExtractableText(page - 1)
	if hasText {
		hasImage, imageErr := s.reader.dictHasSelectableImage(page - 1)
		if imageErr == nil && !hasImage {
			view.HasText = true
			return view, nil
		}
	}
	s.extracts++
	images, imageErr := s.reader.extract(page - 1)
	if imageErr != nil {
		return ScanPageView{}, imageErr
	}
	// The extractor's display size is the last cm, and Q clears that matrix.
	// Replace it with the matrix accumulated up to Do before measuring.
	images = s.applyComposedImageCTM(page, images)
	selected := selectablePDFImages(images)
	if len(selected) == 0 {
		if hasText {
			view.HasText = true
			return view, nil
		}
		view.NoPicture = true
		return view, nil
	}
	pictures := make([]ScanPageImage, 0, len(selected))
	primary := 0
	primaryArea := 0
	for _, img := range selected {
		payload, payloadErr := embeddedPDFImagePayload(img)
		if payloadErr != nil {
			return ScanPageView{}, payloadErr
		}
		place, placeErr := s.placement(page, img, payload)
		if placeErr != nil {
			return ScanPageView{}, placeErr
		}
		pictures = append(pictures, ScanPageImage{Payload: payload, Placement: place})
		area := place.PixelW * place.PixelH
		if area > primaryArea {
			primaryArea = area
			primary = len(pictures) - 1
		}
	}
	view.Images = pictures
	view.Payload = pictures[primary].Payload
	view.Placement = pictures[primary].Placement
	if hasText {
		pageW, pageH := s.pageSize(page)
		if !s.selectionCoversPage(page, selected, pictures, pageW, pageH) {
			// The picture is a logo or an unplaced XObject. Drawing it in
			// place of the page would drop the text and keep the mark.
			view.HasText = true
			view.Images = nil
			view.Payload = nil
			view.Placement = ScanImagePlacement{}
		}
	}
	return view, nil
}

// A covering image is the scan itself: at least scanCoverMinPixels on each
// side, and at least scanCoverPageFraction of both page sides. One side is
// not enough, so a running header does not become the page.
const (
	scanCoverMinPixels    = 200
	scanCoverPageFraction = 0.70
)

func scanImageCoversPage(img gopdf2.ExtractedImage, payload []byte, pageW, pageH float64) bool {
	if pageW < 1 || pageH < 1 {
		return false
	}
	pixelW, pixelH := scanImagePixels(payload, img.Width, img.Height)
	if pixelW < scanCoverMinPixels || pixelH < scanCoverMinPixels {
		return false
	}
	// A negative CTM component is the mirrored size, not a missing placement.
	// A missing Do leaves the display size at 0 and does not cover the page.
	dw, dh := math.Abs(img.DisplayWidth), math.Abs(img.DisplayHeight)
	return dw >= pageW*scanCoverPageFraction && dh >= pageH*scanCoverPageFraction
}

// selectionCoversPage reports whether any selected image occupies the page.
// A composed matrix is measured by the axis-aligned bounds of its four
// corners. A quarter turn has a and d near 0, so the raw components do not
// show that occupancy. A missing Do has no matrix and uses the extractor size,
// which stays 0 and does not cover.
func (s *ScanRestore) selectionCoversPage(page int, selected []gopdf2.ExtractedImage, pictures []ScanPageImage, pageW, pageH float64) bool {
	var byName map[string]scanCTM
	haveCTM := false
	if s != nil {
		byName, haveCTM = s.reader.composedImageCTM(page - 1)
	}
	for i, img := range selected {
		payload := img.Data
		if i < len(pictures) && len(pictures[i].Payload) > 0 {
			payload = pictures[i].Payload
		}
		if haveCTM {
			if m, found := byName[img.Name]; found {
				if ctmCoversPage(m, payload, img, pageW, pageH) {
					return true
				}
				continue
			}
		}
		if scanImageCoversPage(img, payload, pageW, pageH) {
			return true
		}
	}
	return false
}

func ctmExtent(m scanCTM) (w, h float64) {
	xs := [...]float64{m.e, m.a + m.e, m.c + m.e, m.a + m.c + m.e}
	ys := [...]float64{m.f, m.b + m.f, m.d + m.f, m.b + m.d + m.f}
	minX, maxX := xs[0], xs[0]
	minY, maxY := ys[0], ys[0]
	for i := 1; i < len(xs); i++ {
		if xs[i] < minX {
			minX = xs[i]
		}
		if xs[i] > maxX {
			maxX = xs[i]
		}
		if ys[i] < minY {
			minY = ys[i]
		}
		if ys[i] > maxY {
			maxY = ys[i]
		}
	}
	return maxX - minX, maxY - minY
}

func ctmCoversPage(m scanCTM, payload []byte, img gopdf2.ExtractedImage, pageW, pageH float64) bool {
	if pageW < 1 || pageH < 1 {
		return false
	}
	pixelW, pixelH := scanImagePixels(payload, img.Width, img.Height)
	if pixelW < scanCoverMinPixels || pixelH < scanCoverMinPixels {
		return false
	}
	dw, dh := ctmExtent(m)
	return dw >= pageW*scanCoverPageFraction && dh >= pageH*scanCoverPageFraction
}

// PageCount is the number of pages. Later calls on this value do not open
// the file again.
func (s *ScanRestore) PageCount() (int, error) {
	if s == nil || len(s.data) == 0 {
		return 0, fmt.Errorf("empty pdf")
	}
	return s.reader.numPages()
}

// EveryPageHasText reports that each page already has selectable text.
// It does not look at images. A scan with a page number is still text here.
// Pages answered here are not asked again when they are read.
func (s *ScanRestore) EveryPageHasText(count int) bool {
	if s == nil || count < 1 {
		return false
	}
	for page := 1; page <= count; page++ {
		if !s.reader.pageHasExtractableText(page - 1) {
			return false
		}
	}
	return true
}

// EveryPageIsDigital reports that every page has extractable text and no
// page-covering image. A page with no text returns immediately, so rejecting
// a scan does not parse its images. A page that has both is read through
// Page, and that view stays cached for the draw.
func (s *ScanRestore) EveryPageIsDigital(count int) bool {
	if s == nil || count < 1 {
		return false
	}
	for page := 1; page <= count; page++ {
		if !s.reader.pageHasExtractableText(page - 1) {
			return false
		}
		view, err := s.Page(page)
		if err != nil || !view.HasText {
			return false
		}
	}
	return true
}

// applyComposedImageCTM copies images whose Do matrix is the product of the
// content stream, including a cm restored by Q. The cached extract result is
// left alone. A stream that cannot be read keeps the extractor geometry.
func (s *ScanRestore) applyComposedImageCTM(page int, images []gopdf2.ExtractedImage) []gopdf2.ExtractedImage {
	if s == nil || len(images) == 0 {
		return images
	}
	byName, ok := s.reader.composedImageCTM(page - 1)
	if !ok || len(byName) == 0 {
		return images
	}
	parserH := s.extractorPageHeight(page)
	out := make([]gopdf2.ExtractedImage, len(images))
	copy(out, images)
	changed := false
	for i := range out {
		m, found := byName[out[i].Name]
		if !found {
			continue
		}
		// a and d are the axis-aligned edges. A quarter turn leaves them near
		// 0; placement reads b and c from this matrix before treating that
		// as a missing Do.
		out[i].X = m.e
		out[i].DisplayWidth = m.a
		out[i].DisplayHeight = m.d
		out[i].Y = parserH - m.f - m.d
		changed = true
	}
	if !changed {
		return images
	}
	return out
}

// extractorPageHeight is the height GoPDF2's image extractor uses for Y.
// A page object without a direct MediaBox array is measured from US Letter.
func (s *ScanRestore) extractorPageHeight(page int) float64 {
	if s == nil || s.reader.pageDictOmitsMediaBox(page-1) {
		return rawPDFFallbackPageHeight
	}
	_, h := s.pageSize(page)
	if h <= 0 {
		return rawPDFFallbackPageHeight
	}
	return h
}

func (s *ScanRestore) placement(page int, img gopdf2.ExtractedImage, payload []byte) (ScanImagePlacement, error) {
	pixelW, pixelH := scanImagePixels(payload, img.Width, img.Height)
	if pixelW < 1 || pixelH < 1 {
		return ScanImagePlacement{}, fmt.Errorf("scan image has no pixel size")
	}
	// A quarter turn keeps its size in b and c. a and d are then about 0,
	// which is the same shape as a missing Do. 180 degrees stays here: both
	// edges are negative and b and c are about 0, so the fold below still
	// mirrors it without a page rotation.
	if m, ok := s.imageCTM(page, img.Name); ok && (math.Abs(m.b) >= 1 || math.Abs(m.c) >= 1) {
		return s.turnedPlacement(page, m, pixelW, pixelH)
	}
	place := ScanImagePlacement{
		X: img.X, Y: s.correctedTop(page, img),
		DisplayW: img.DisplayWidth, DisplayH: img.DisplayHeight,
		PixelW: pixelW, PixelH: pixelH,
	}
	// Negative CTM a or d mirrors the samples inside the rectangle
	// (w 0 0 -h x y+h). Fold the edge into a positive top-left, and keep the
	// reflection for the draw and the text layer. A missing Do stays at 0,
	// covers the page, and is not reflected.
	mirrorX := place.DisplayW < 0
	mirrorY := place.DisplayH < 0
	if mirrorX {
		place.X += place.DisplayW
		place.DisplayW = -place.DisplayW
	}
	if mirrorY {
		place.Y += place.DisplayH
		place.DisplayH = -place.DisplayH
	}
	if place.DisplayW >= 1 && place.DisplayH >= 1 {
		place.MirrorX = mirrorX
		place.MirrorY = mirrorY
		return place, nil
	}
	pageW, pageH := s.pageSize(page)
	if pageW < 1 || pageH < 1 {
		return ScanImagePlacement{}, fmt.Errorf("scan image has no page placement")
	}
	place.X, place.Y = 0, 0
	place.DisplayW, place.DisplayH = pageW, pageH
	place.FullPage = true
	return place, nil
}

func (s *ScanRestore) imageCTM(page int, name string) (scanCTM, bool) {
	if s == nil || name == "" {
		return scanCTM{}, false
	}
	byName, ok := s.reader.composedImageCTM(page - 1)
	if !ok {
		return scanCTM{}, false
	}
	m, found := byName[name]
	return m, found
}

// turnedPlacement is the rectangle GoPDF2 rotates about its center.
// The CTM is already in the page's user space, so the US Letter default
// does not shift it. MirrorY is a reflection (det < 0). The angle matches
// atan2(b, a), which is the rotation GoPDF2 writes for DegreeAngle.
func (s *ScanRestore) turnedPlacement(page int, m scanCTM, pixelW, pixelH int) (ScanImagePlacement, error) {
	edgeW := math.Hypot(m.a, m.b)
	edgeH := math.Hypot(m.c, m.d)
	pageW, pageH := s.pageSize(page)
	if edgeW < 1 || edgeH < 1 || pageW < 1 || pageH < 1 {
		if pageW < 1 || pageH < 1 {
			return ScanImagePlacement{}, fmt.Errorf("scan image has no page placement")
		}
		return ScanImagePlacement{
			DisplayW: pageW, DisplayH: pageH,
			PixelW: pixelW, PixelH: pixelH,
			FullPage: true,
		}, nil
	}
	cx := m.e + (m.a+m.c)/2
	cy := m.f + (m.b+m.d)/2
	return ScanImagePlacement{
		X:         cx - edgeW/2,
		Y:         (pageH - cy) - edgeH/2,
		DisplayW:  edgeW,
		DisplayH:  edgeH,
		PixelW:    pixelW,
		PixelH:    pixelH,
		MirrorY:   m.a*m.d-m.b*m.c < 0,
		Turn:      math.Atan2(m.b, m.a) * 180 / math.Pi,
		TurnPageH: pageH,
		TurnA:     m.a, TurnB: m.b, TurnC: m.c,
		TurnD: m.d, TurnE: m.e, TurnF: m.f,
	}, nil
}

// rawPDFFallbackPageHeight is the height extractMediaBox uses when a page
// dictionary omits MediaBox. The image CTM was written against the real
// page height, so that default shifts the reported top.
const rawPDFFallbackPageHeight = 792

func (s *ScanRestore) correctedTop(page int, img gopdf2.ExtractedImage) float64 {
	_, pageH := s.pageSize(page)
	if pageH <= 0 || math.Abs(pageH-rawPDFFallbackPageHeight) < 0.5 {
		return img.Y
	}
	// The raw image extractor defaults a page object with no direct MediaBox
	// to US Letter. That fact is on the page dictionary, in page-tree order.
	if !s.reader.pageDictOmitsMediaBox(page - 1) {
		return img.Y
	}
	return img.Y + (pageH - rawPDFFallbackPageHeight)
}

func (s *ScanRestore) pageSize(page int) (float64, float64) {
	if s == nil {
		return 0, 0
	}
	if !s.geomDone {
		s.geomDone = true
		s.geomReads++
		s.geom = s.reader.pageSizes()
	}
	wh, ok := s.geom[page]
	if !ok {
		return 0, 0
	}
	return wh[0], wh[1]
}

func scanImagePixels(payload []byte, dictW, dictH int) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err == nil && cfg.Width > 0 && cfg.Height > 0 {
		return cfg.Width, cfg.Height
	}
	if dictW > 0 && dictH > 0 {
		return dictW, dictH
	}
	return 0, 0
}

// PlaceScanLines maps pixel boxes onto the page. Empty text and boxes that
// would draw smaller than one point are left out.
func PlaceScanLines(place ScanImagePlacement, lines []ScanTextLine) []PlacedScanLine {
	if place.PixelW < 1 || place.PixelH < 1 || place.DisplayW < 1 || place.DisplayH < 1 {
		return nil
	}
	if place.Turn != 0 {
		return placeTurnedScanLines(place, lines)
	}
	scaleX := place.DisplayW / float64(place.PixelW)
	scaleY := place.DisplayH / float64(place.PixelH)
	out := make([]PlacedScanLine, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" || line.BBox[2] < 1 || line.BBox[3] < 1 {
			continue
		}
		x := float64(line.BBox[0]) * scaleX
		y := float64(line.BBox[1]) * scaleY
		w := float64(line.BBox[2]) * scaleX
		h := float64(line.BBox[3]) * scaleY
		// Pixel origin is the file's top-left. A mirrored axis paints that
		// edge on the far side of the rectangle.
		if place.MirrorX {
			x = place.DisplayW - x - w
		}
		if place.MirrorY {
			y = place.DisplayH - y - h
		}
		placed := PlacedScanLine{
			Text:    text,
			X:       place.X + x,
			Y:       place.Y + y,
			W:       w,
			H:       h,
			Formula: line.Formula,
		}
		if placed.W < 1 || placed.H < 1 || scanOverlayFontSize(placed.W, placed.H, text, line.Formula) < 1 {
			continue
		}
		out = append(out, placed)
	}
	return out
}

// placeTurnedScanLines maps a pixel box through the image CTM and keeps the
// axis-aligned bounds. The matrix already includes a reflection, so MirrorX
// and MirrorY are not applied again. Glyphs stay upright; the box moves
// onto the ink.
func placeTurnedScanLines(place ScanImagePlacement, lines []ScanTextLine) []PlacedScanLine {
	pw := float64(place.PixelW)
	ph := float64(place.PixelH)
	out := make([]PlacedScanLine, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" || line.BBox[2] < 1 || line.BBox[3] < 1 {
			continue
		}
		u0 := float64(line.BBox[0]) / pw
		u1 := float64(line.BBox[0]+line.BBox[2]) / pw
		// Pixel y grows down from the file top. PDF v grows up from the image bottom.
		v1 := 1 - float64(line.BBox[1])/ph
		v0 := 1 - float64(line.BBox[1]+line.BBox[3])/ph
		minX, minY, maxX, maxY := turnedCorners(place, u0, u1, v0, v1)
		placed := PlacedScanLine{
			Text:    text,
			X:       minX,
			Y:       place.TurnPageH - maxY,
			W:       maxX - minX,
			H:       maxY - minY,
			Formula: line.Formula,
		}
		if placed.W < 1 || placed.H < 1 || scanOverlayFontSize(placed.W, placed.H, text, line.Formula) < 1 {
			continue
		}
		out = append(out, placed)
	}
	return out
}

func turnedCorners(place ScanImagePlacement, u0, u1, v0, v1 float64) (minX, minY, maxX, maxY float64) {
	xs := [4]float64{
		place.TurnA*u0 + place.TurnC*v0 + place.TurnE,
		place.TurnA*u1 + place.TurnC*v0 + place.TurnE,
		place.TurnA*u0 + place.TurnC*v1 + place.TurnE,
		place.TurnA*u1 + place.TurnC*v1 + place.TurnE,
	}
	ys := [4]float64{
		place.TurnB*u0 + place.TurnD*v0 + place.TurnF,
		place.TurnB*u1 + place.TurnD*v0 + place.TurnF,
		place.TurnB*u0 + place.TurnD*v1 + place.TurnF,
		place.TurnB*u1 + place.TurnD*v1 + place.TurnF,
	}
	minX, maxX = xs[0], xs[0]
	minY, maxY = ys[0], ys[0]
	for i := 1; i < 4; i++ {
		if xs[i] < minX {
			minX = xs[i]
		}
		if xs[i] > maxX {
			maxX = xs[i]
		}
		if ys[i] < minY {
			minY = ys[i]
		}
		if ys[i] > maxY {
			maxY = ys[i]
		}
	}
	return minX, minY, maxX, maxY
}

func scanOverlayFontSize(w, h float64, text string, formula bool) float64 {
	n := utf8.RuneCountInString(text)
	if n < 1 || w < 1 || h < 1 {
		return 0
	}
	// The overlay is transparent. Formula source is longer than the glyphs
	// it stands for, so fitting every rune inside the box drops the line.
	size := h * 0.92
	if !formula {
		if byWidth := w / float64(n); byWidth < size {
			size = byWidth
		}
	}
	if size < 1 {
		if !formula {
			return 0
		}
		size = 1
	}
	if size > 72 {
		return 72
	}
	return size
}

// ScanLineLooksLikeFormula reports a math line that should be cropped and
// sent to the formula model. A line that is mostly Chinese stays prose.
// One hyphen, as in "well-known", stays prose. "Table 1" and "第3章" stay prose.
func ScanLineLooksLikeFormula(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	total, han, letters, weak := 0, 0, 0, 0
	strong := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			han++
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters++
		}
		if isStrongMathRune(r) {
			strong = true
		}
		if isWeakMathRune(r) {
			weak++
		}
	}
	if total == 0 || han*2 >= total {
		return false
	}
	if strong {
		return true
	}
	return weak >= 2 && han == 0 && letters > 0
}

func isStrongMathRune(r rune) bool {
	switch r {
	case '=', '^', '_', '\\', '∑', '∫', '√', '≤', '≥', '≈', '≠', '±', '×', '÷', '∞', '∈', '∉', '∂':
		return true
	}
	return (r >= 'α' && r <= 'ω') || (r >= 'Α' && r <= 'Ω')
}

func isWeakMathRune(r rune) bool {
	switch r {
	case '+', '-', '*', '/', '<', '>':
		return true
	}
	return false
}

// ApplyFormulaLines crops formula-like lines and replaces them when the
// recognizer returns LaTeX. An empty result or an error keeps the OCR text.
// recognize is the already-loaded formula engine; a nil func changes nothing.
func ApplyFormulaLines(lines []ScanTextLine, img image.Image, recognize func(image.Image) (string, error)) []ScanTextLine {
	return ApplyFormulaCrops(lines, img, recognize, func(line ScanTextLine) bool {
		return ScanLineLooksLikeFormula(line.Text)
	})
}

// FormulaRegion is one layout formula box in image pixels, origin at the
// top-left. The recognizer sees this box, not each OCR line inside it.
type FormulaRegion struct {
	X0, Y0, X1, Y1 float64
}

func (r FormulaRegion) contains(x, y float64) bool {
	return x >= r.X0 && x < r.X1 && y >= r.Y0 && y < r.Y1
}

// ApplyFormulaRegions recognizes each region once. OCR lines whose centers
// lie in that region become one LaTeX line covering the region. A region
// with no OCR line still inserts that LaTeX line. An empty result or an
// error leaves the region's OCR lines in place. Lines outside every region
// are unchanged.
func ApplyFormulaRegions(lines []ScanTextLine, img image.Image, recognize func(image.Image) (string, error), regions []FormulaRegion) []ScanTextLine {
	if img == nil || recognize == nil || len(regions) == 0 {
		return lines
	}
	type replacement struct {
		line ScanTextLine
		ok   bool
	}
	replaced := make([]replacement, len(lines))
	drop := make([]bool, len(lines))
	var extra []ScanTextLine
	for _, region := range regions {
		box := formulaRegionPixels(img, region)
		if box[2] < 1 || box[3] < 1 {
			continue
		}
		crop := cropFormulaLine(img, box)
		if crop == nil {
			continue
		}
		latex, err := recognize(crop)
		latex = strings.TrimSpace(latex)
		if err != nil || latex == "" {
			continue
		}
		next := ScanTextLine{Text: latex, Formula: true, BBox: box}
		first := -1
		for i := range lines {
			if drop[i] || replaced[i].ok {
				continue
			}
			cx := float64(lines[i].BBox[0]) + float64(lines[i].BBox[2])/2
			cy := float64(lines[i].BBox[1]) + float64(lines[i].BBox[3])/2
			if !region.contains(cx, cy) {
				continue
			}
			if first < 0 {
				first = i
				continue
			}
			drop[i] = true
		}
		if first < 0 {
			extra = append(extra, next)
			continue
		}
		replaced[first] = replacement{line: next, ok: true}
	}
	if len(extra) == 0 {
		changed := false
		for i := range lines {
			if drop[i] || replaced[i].ok {
				changed = true
				break
			}
		}
		if !changed {
			return lines
		}
	}
	out := make([]ScanTextLine, 0, len(lines)+len(extra))
	for i := range lines {
		if drop[i] {
			continue
		}
		if replaced[i].ok {
			out = append(out, replaced[i].line)
			continue
		}
		out = append(out, lines[i])
	}
	return append(out, extra...)
}

func formulaRegionPixels(img image.Image, region FormulaRegion) [4]int {
	bounds := img.Bounds()
	x0 := int(math.Floor(region.X0))
	y0 := int(math.Floor(region.Y0))
	x1 := int(math.Ceil(region.X1))
	y1 := int(math.Ceil(region.Y1))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > bounds.Dx() {
		x1 = bounds.Dx()
	}
	if y1 > bounds.Dy() {
		y1 = bounds.Dy()
	}
	if x1 <= x0 || y1 <= y0 {
		return [4]int{}
	}
	return [4]int{x0, y0, x1 - x0, y1 - y0}
}

// ApplyFormulaCrops sends the lines include selects to recognize.
// A line already marked Formula is left unchanged. An empty result or an
// error keeps that line's OCR text. include is how layout formula regions
// reach the formula model without sending every prose line.
func ApplyFormulaCrops(lines []ScanTextLine, img image.Image, recognize func(image.Image) (string, error), include func(ScanTextLine) bool) []ScanTextLine {
	if len(lines) == 0 || img == nil || recognize == nil || include == nil {
		return lines
	}
	out := make([]ScanTextLine, len(lines))
	copy(out, lines)
	for i := range out {
		if out[i].Formula || !include(out[i]) {
			continue
		}
		crop := cropFormulaLine(img, out[i].BBox)
		if crop == nil {
			continue
		}
		latex, err := recognize(crop)
		latex = strings.TrimSpace(latex)
		if err != nil || latex == "" {
			continue
		}
		out[i].Text = latex
		out[i].Formula = true
	}
	return out
}

func cropFormulaLine(img image.Image, box [4]int) image.Image {
	if img == nil || box[2] < 1 || box[3] < 1 {
		return nil
	}
	pad := formulaCropPad(box[2], box[3])
	bounds := img.Bounds()
	rect := image.Rect(
		bounds.Min.X+box[0]-pad,
		bounds.Min.Y+box[1]-pad,
		bounds.Min.X+box[0]+box[2]+pad,
		bounds.Min.Y+box[1]+box[3]+pad,
	).Intersect(bounds)
	if rect.Empty() {
		return nil
	}
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(rect)
	}
	dst := image.NewNRGBA(rect)
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	return dst
}

func formulaCropPad(w, h int) int {
	side := w
	if h < side {
		side = h
	}
	pad := side / 10
	if pad < 4 {
		pad = 4
	}
	if pad > 24 {
		pad = 24
	}
	return pad
}

// RestoreScannedPDF writes a new PDF. Each source page is rebuilt at its own
// page size: the scan image is drawn again, then a transparent text layer.
// JPEG scans use DCTDecode, which the page importer cannot read, so the scan
// is not imported as a form. A page that already has extractable text is kept
// at the same size; its original vectors are not copied. The source bytes are
// not modified. Alpha 0 text is still extracted.
func RestoreScannedPDF(src []byte, byPage map[int][]PlacedScanLine, font []byte) ([]byte, error) {
	return restoreScannedPDF(NewScanRestore(src), byPage, font)
}

// RestoreScannedPDFWith draws with doc. Pages doc has already read are not
// extracted again.
func RestoreScannedPDFWith(doc *ScanRestore, byPage map[int][]PlacedScanLine, font []byte) ([]byte, error) {
	return restoreScannedPDF(doc, byPage, font)
}

func restoreScannedPDF(doc *ScanRestore, byPage map[int][]PlacedScanLine, font []byte) (out []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			out = nil
			err = fmt.Errorf("restore scanned pdf: %v", recovered)
		}
	}()
	if doc == nil || len(doc.data) == 0 {
		return nil, fmt.Errorf("empty pdf")
	}
	src := doc.data
	if PDFIsEncrypted(src) {
		return nil, gopdf2.ErrEncryptedPDF
	}
	if !placedLinesDraw(byPage) {
		return nil, fmt.Errorf("no overlay text")
	}
	if len(font) == 0 {
		return nil, fmt.Errorf("missing CJK font")
	}
	count, countErr := doc.reader.numPages()
	if countErr != nil || count < 1 {
		if countErr != nil {
			return nil, countErr
		}
		return nil, fmt.Errorf("PDF has no pages")
	}
	if err := validatePDFPageCount(count); err != nil {
		return nil, err
	}
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	const family = "scan"
	if err := pdf.AddTTFFontData(family, font); err != nil {
		return nil, err
	}
	tr, err := gopdf2.NewTransparency(0, "")
	if err != nil {
		return nil, err
	}
	for page := 1; page <= count; page++ {
		view, viewErr := doc.Page(page)
		if viewErr != nil {
			return nil, viewErr
		}
		pageW, pageH := doc.pageSize(page)
		if pageW < 1 || pageH < 1 {
			return nil, fmt.Errorf("page %d has no page size", page)
		}
		pdf.AddPageWithOption(gopdf2.PageOption{PageSize: &gopdf2.Rect{W: pageW, H: pageH}})
		if err := drawScanPageImages(&pdf, view); err != nil {
			return nil, err
		}
		if err := drawPlacedScanLines(&pdf, family, &tr, byPage[page]); err != nil {
			return nil, err
		}
	}
	return pdf.GetBytesPdfReturnErr()
}

func drawScanPageImages(pdf *gopdf2.GoPdf, view ScanPageView) error {
	images := view.Images
	if len(images) == 0 && len(view.Payload) > 0 {
		images = []ScanPageImage{{Payload: view.Payload, Placement: view.Placement}}
	}
	for _, img := range images {
		if len(img.Payload) == 0 {
			continue
		}
		holder, err := gopdf2.ImageHolderByBytes(img.Payload)
		if err != nil {
			return err
		}
		w, h := img.Placement.DisplayW, img.Placement.DisplayH
		if w < 1 || h < 1 {
			return fmt.Errorf("page %d scan image has no page placement", view.Page)
		}
		var drawErr error
		// DegreeAngle is only set for a real turn. Angle 0 stays on the
		// holder call, or on the flip options without a rotation, so an
		// axis-aligned mirror keeps the content stream it already writes.
		if img.Placement.Turn != 0 || img.Placement.MirrorX || img.Placement.MirrorY {
			opts := gopdf2.ImageOptions{
				X:              img.Placement.X,
				Y:              img.Placement.Y,
				Rect:           &gopdf2.Rect{W: w, H: h},
				HorizontalFlip: img.Placement.MirrorX,
				VerticalFlip:   img.Placement.MirrorY,
			}
			if img.Placement.Turn != 0 {
				opts.DegreeAngle = img.Placement.Turn
			}
			drawErr = pdf.ImageByHolderWithOptions(holder, opts)
		} else {
			drawErr = pdf.ImageByHolder(holder, img.Placement.X, img.Placement.Y, &gopdf2.Rect{W: w, H: h})
		}
		if drawErr != nil {
			return drawErr
		}
	}
	return nil
}

func placedLinesDraw(byPage map[int][]PlacedScanLine) bool {
	for _, lines := range byPage {
		for _, line := range lines {
			if strings.TrimSpace(line.Text) != "" {
				return true
			}
		}
	}
	return false
}

func drawPlacedScanLines(pdf *gopdf2.GoPdf, family string, tr *gopdf2.Transparency, lines []PlacedScanLine) error {
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		size := scanOverlayFontSize(line.W, line.H, text, line.Formula)
		if text == "" || size < 1 {
			continue
		}
		if err := pdf.SetFont(family, "", size); err != nil {
			return err
		}
		pdf.SetXY(line.X, line.Y)
		if err := pdf.CellWithOption(&gopdf2.Rect{W: line.W, H: line.H}, text, gopdf2.CellOption{
			Align:        gopdf2.Left | gopdf2.Middle,
			Transparency: tr,
		}); err != nil {
			return err
		}
	}
	return nil
}
