package gopdf

// fpdi_bridge.go implements a gofpdi-compatible Importer using ledongthuc/pdf
// as the PDF parsing backend. This eliminates the dependency on gofpdi which
// panics on many valid PDFs.

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"math"
	"strings"

	lpdf "github.com/ledongthuc/pdf"
)

// fpdiImporter is a drop-in replacement for gofpdi.Importer.
type fpdiImporter struct {
	reader   *lpdf.Reader
	readerAt io.ReaderAt
	size     int64

	tpls         []*fpdiTemplate
	tplIdOffset  int
	nextObjID    int
	writtenObjs  map[int]string
	importedKeys map[string]int // "source-pageNo" → tplN
}

type fpdiTemplate struct {
	content   []byte
	resources string // serialized PDF resources dict
	box       map[string]float64
	boxes     map[string]map[string]float64
	w, h      float64
	rotation  int
	objID     int
}

// newFpdiImporter creates a new importer (no source set yet).
func newFpdiImporter() *fpdiImporter {
	return &fpdiImporter{
		writtenObjs:  make(map[int]string),
		importedKeys: make(map[string]int),
	}
}

// SetSourceStream sets the PDF source from a ReadSeeker.
func (imp *fpdiImporter) SetSourceStream(rs *io.ReadSeeker) {
	data, err := io.ReadAll(*rs)
	if err != nil {
		panic(fmt.Errorf("fpdiImporter: read stream: %v", err))
	}
	imp.readerAt = bytes.NewReader(data)
	imp.size = int64(len(data))
	imp.reader = nil // lazy init
}

// SetSourceFile sets the PDF source from a file path.
func (imp *fpdiImporter) SetSourceFile(f string) {
	data, err := readFileBytes(f)
	if err != nil {
		panic(fmt.Errorf("fpdiImporter: read file %s: %v", f, err))
	}
	imp.readerAt = bytes.NewReader(data)
	imp.size = int64(len(data))
	imp.reader = nil
}

func (imp *fpdiImporter) ensureReader() {
	if imp.reader != nil {
		return
	}
	r, err := lpdf.NewReader(imp.readerAt, imp.size)
	if err != nil {
		// ledongthuc/pdf only supports PDF 1.0-1.7 headers.
		// For PDF 2.0 or other versions, try patching the header.
		if patched := patchPDFHeader(imp.readerAt, imp.size); patched != nil {
			r, err = lpdf.NewReader(patched, imp.size)
		}
		if err != nil {
			panic(fmt.Errorf("fpdiImporter: parse PDF: %v", err))
		}
	}
	imp.reader = r
}

// patchPDFHeader rewrites a non-1.x PDF header to %PDF-1.7 so that
// ledongthuc/pdf (which only supports 1.0-1.7) can parse it.
func patchPDFHeader(ra io.ReaderAt, size int64) io.ReaderAt {
	buf := make([]byte, 10)
	if _, err := ra.ReadAt(buf, 0); err != nil {
		return nil
	}
	if !bytes.HasPrefix(buf, []byte("%PDF-")) {
		return nil
	}
	// Read the full data, patch the version
	full := make([]byte, size)
	if _, err := ra.ReadAt(full, 0); err != nil {
		return nil
	}
	// Replace version with 1.7
	copy(full[5:], []byte("1.7"))
	return bytes.NewReader(full)
}

// GetNumPages returns the number of pages.
func (imp *fpdiImporter) GetNumPages() int {
	imp.ensureReader()
	return imp.reader.NumPage()
}

// GetPageSizes returns page box sizes in the same format as gofpdi:
// map[pageNum]map[boxName]map["w"|"h"|"llx"|"lly"|"urx"|"ury"]float64
func (imp *fpdiImporter) GetPageSizes() map[int]map[string]map[string]float64 {
	imp.ensureReader()
	n := imp.reader.NumPage()
	result := make(map[int]map[string]map[string]float64, n)

	for i := 1; i <= n; i++ {
		page := imp.reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		boxes := make(map[string]map[string]float64)
		for _, boxName := range []string{"/MediaBox", "/CropBox", "/BleedBox", "/TrimBox", "/ArtBox"} {
			key := boxName[1:] // strip leading /
			boxVal := page.V.Key(key)
			if boxVal.Kind() != lpdf.Array || boxVal.Len() < 4 {
				continue
			}
			llx := boxVal.Index(0).Float64()
			lly := boxVal.Index(1).Float64()
			urx := boxVal.Index(2).Float64()
			ury := boxVal.Index(3).Float64()
			boxes[boxName] = map[string]float64{
				"llx": llx, "lly": lly,
				"urx": urx, "ury": ury,
				"w": urx - llx, "h": ury - lly,
				"x": llx, "y": lly,
			}
		}
		// If no MediaBox found directly, try inherited
		if _, ok := boxes["/MediaBox"]; !ok {
			// Walk up parent chain
			mb := findInheritedBox(page.V, "MediaBox")
			if mb != nil {
				boxes["/MediaBox"] = mb
			}
		}
		if len(boxes) > 0 {
			result[i] = boxes
		}
	}
	return result
}

func findInheritedBox(v lpdf.Value, key string) map[string]float64 {
	for cur := v; !cur.IsNull(); cur = cur.Key("Parent") {
		boxVal := cur.Key(key)
		if boxVal.Kind() == lpdf.Array && boxVal.Len() >= 4 {
			llx := boxVal.Index(0).Float64()
			lly := boxVal.Index(1).Float64()
			urx := boxVal.Index(2).Float64()
			ury := boxVal.Index(3).Float64()
			return map[string]float64{
				"llx": llx, "lly": lly,
				"urx": urx, "ury": ury,
				"w": urx - llx, "h": ury - lly,
				"x": llx, "y": lly,
			}
		}
	}
	return nil
}

// ImportPage imports a page as a template. Returns the template index.
func (imp *fpdiImporter) ImportPage(pageno int, box string) int {
	imp.ensureReader()

	key := fmt.Sprintf("page-%d", pageno)
	if idx, ok := imp.importedKeys[key]; ok {
		return idx
	}

	page := imp.reader.Page(pageno)
	if page.V.IsNull() {
		panic(fmt.Errorf("fpdiImporter: page %d is null", pageno))
	}

	// Get page boxes
	sizes := imp.GetPageSizes()
	pageBoxes, ok := sizes[pageno]
	if !ok {
		panic(fmt.Errorf("fpdiImporter: no boxes for page %d", pageno))
	}

	// Resolve box name with fallbacks
	if _, ok := pageBoxes[box]; !ok {
		if box == "/BleedBox" || box == "/TrimBox" || box == "/ArtBox" {
			box = "/CropBox"
		}
		if _, ok := pageBoxes[box]; !ok {
			box = "/MediaBox"
		}
	}
	if _, ok := pageBoxes[box]; !ok {
		panic(fmt.Errorf("fpdiImporter: box %s not found for page %d", box, pageno))
	}

	theBox := pageBoxes[box]

	// Extract content stream
	content := extractPageContentBytes(page)

	// Extract resources dict as PDF string
	resources := serializeResources(page)

	// Get rotation
	rotation := 0
	rotVal := page.V.Key("Rotate")
	if rotVal.Kind() == lpdf.Integer {
		rotation = int(rotVal.Int64()) % 360
	}

	tpl := &fpdiTemplate{
		content:   content,
		resources: resources,
		box:       theBox,
		boxes:     pageBoxes,
		w:         theBox["w"],
		h:         theBox["h"],
		rotation:  rotation,
	}

	// Handle rotation
	if rotation != 0 {
		angle := rotation
		if angle < 0 {
			angle += 360
		}
		if (angle/90)%2 != 0 {
			tpl.w, tpl.h = tpl.h, tpl.w
		}
		tpl.rotation = -angle
	}

	idx := len(imp.tpls)
	imp.tpls = append(imp.tpls, tpl)
	imp.importedKeys[key] = idx
	return idx
}

// extractPageContentBytes reads the page content stream as raw bytes.
func extractPageContentBytes(page lpdf.Page) []byte {
	contentsVal := page.V.Key("Contents")
	if contentsVal.IsNull() {
		return nil
	}

	switch contentsVal.Kind() {
	case lpdf.Stream:
		rc := contentsVal.Reader()
		defer rc.Close()
		data, _ := io.ReadAll(rc)
		return data
	case lpdf.Array:
		var buf bytes.Buffer
		for i := 0; i < contentsVal.Len(); i++ {
			item := contentsVal.Index(i)
			if item.Kind() == lpdf.Stream {
				rc := item.Reader()
				data, _ := io.ReadAll(rc)
				buf.Write(data)
				buf.WriteByte('\n')
				rc.Close()
			}
		}
		return buf.Bytes()
	}
	return nil
}

// serializeResources produces a minimal PDF resources dictionary string
// from the page's Resources entry.
func serializeResources(page lpdf.Page) string {
	res := page.V.Key("Resources")
	if res.IsNull() {
		// Try parent
		for p := page.V.Key("Parent"); !p.IsNull(); p = p.Key("Parent") {
			res = p.Key("Resources")
			if !res.IsNull() {
				break
			}
		}
	}
	if res.IsNull() {
		return "<< >>"
	}
	return serializeValue(res)
}

// serializeValue converts a ledongthuc/pdf Value to a PDF syntax string.
func serializeValue(v lpdf.Value) string {
	switch v.Kind() {
	case lpdf.Null:
		return "null"
	case lpdf.Bool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case lpdf.Integer:
		return fmt.Sprintf("%d", v.Int64())
	case lpdf.Real:
		return fmt.Sprintf("%f", v.Float64())
	case lpdf.String:
		return "(" + escapePDFStringForWrite(v.RawString()) + ")"
	case lpdf.Name:
		return "/" + v.Name()
	case lpdf.Dict:
		var sb strings.Builder
		sb.WriteString("<< ")
		for _, key := range v.Keys() {
			sb.WriteString("/" + key + " ")
			sb.WriteString(serializeValue(v.Key(key)))
			sb.WriteString(" ")
		}
		sb.WriteString(">>")
		return sb.String()
	case lpdf.Array:
		var sb strings.Builder
		sb.WriteString("[ ")
		for i := 0; i < v.Len(); i++ {
			sb.WriteString(serializeValue(v.Index(i)))
			sb.WriteString(" ")
		}
		sb.WriteString("]")
		return sb.String()
	case lpdf.Stream:
		// For streams in resources (like fonts), we need to inline them.
		// Read the stream data and write it as a stream object reference.
		// This is complex — for now, serialize the header dict.
		return serializeStreamInline(v)
	default:
		return "null"
	}
}

func serializeStreamInline(v lpdf.Value) string {
	rc := v.Reader()
	defer rc.Close()
	data, _ := io.ReadAll(rc)

	// Build header dict without Length (we'll set our own)
	var sb strings.Builder
	sb.WriteString("<< ")
	for _, key := range v.Keys() {
		if key == "Length" || key == "Filter" || key == "DecodeParms" {
			continue // we re-encode
		}
		sb.WriteString("/" + key + " ")
		sb.WriteString(serializeValue(v.Key(key)))
		sb.WriteString(" ")
	}
	sb.WriteString(fmt.Sprintf("/Length %d ", len(data)))
	sb.WriteString(">>\nstream\n")
	sb.Write(data)
	sb.WriteString("\nendstream")
	return sb.String()
}

func escapePDFStringForWrite(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			sb.WriteString("\\\\")
		case '(':
			sb.WriteString("\\(")
		case ')':
			sb.WriteString("\\)")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// SetNextObjectID sets the starting object ID for the next PutFormXobjects call.
func (imp *fpdiImporter) SetNextObjectID(id int) {
	imp.nextObjID = id
}

// PutFormXobjects serializes templates as Form XObjects.
// Returns map of template name (e.g. "/GOFPDITPL1") to object ID.
func (imp *fpdiImporter) PutFormXobjects() map[string]int {
	result := make(map[string]int)
	objID := imp.nextObjID

	for i, tpl := range imp.tpls {
		if tpl.objID > 0 {
			// Already serialized
			result[fmt.Sprintf("/GOFPDITPL%d", i+imp.tplIdOffset)] = tpl.objID
			continue
		}

		// Compress content
		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		zw.Write(tpl.content)
		zw.Close()
		p := compressed.String()

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d 0 obj\n", objID))
		sb.WriteString("<< /Filter /FlateDecode /Type /XObject\n")
		sb.WriteString("/Subtype /Form\n")
		sb.WriteString("/FormType 1\n")

		// BBox
		sb.WriteString(fmt.Sprintf("/BBox [%.2f %.2f %.2f %.2f]\n",
			tpl.box["llx"], tpl.box["lly"], tpl.box["urx"], tpl.box["ury"]))

		// Matrix for rotation
		c := 1.0
		s := 0.0
		tx := -tpl.box["llx"]
		ty := -tpl.box["lly"]

		if tpl.rotation != 0 {
			angle := float64(tpl.rotation) * math.Pi / 180.0
			c = math.Cos(angle)
			s = math.Sin(angle)

			switch tpl.rotation {
			case -90:
				tx = -tpl.box["lly"]
				ty = tpl.box["urx"]
			case -180:
				tx = tpl.box["urx"]
				ty = tpl.box["ury"]
			case -270:
				tx = tpl.box["ury"]
				ty = -tpl.box["llx"]
			}
		}

		if c != 1 || s != 0 || tx != 0 || ty != 0 {
			sb.WriteString(fmt.Sprintf("/Matrix [%.5f %.5f %.5f %.5f %.5f %.5f]\n",
				c, s, -s, c, tx, ty))
		}

		// Resources
		sb.WriteString("/Resources ")
		sb.WriteString(tpl.resources)
		sb.WriteString("\n")

		// Stream
		sb.WriteString(fmt.Sprintf("/Length %d >>\n", len(p)))
		sb.WriteString("stream\n")
		sb.WriteString(p)
		sb.WriteString("\nendstream\n")
		sb.WriteString("endobj\n")

		tpl.objID = objID
		imp.writtenObjs[objID] = sb.String()
		result[fmt.Sprintf("/GOFPDITPL%d", i+imp.tplIdOffset)] = objID
		objID++
	}

	imp.nextObjID = objID
	return result
}

// GetImportedObjects returns map of object ID to PDF object string.
func (imp *fpdiImporter) GetImportedObjects() map[int]string {
	return imp.writtenObjs
}

// UseTemplate returns the template name and transform values for drawing.
func (imp *fpdiImporter) UseTemplate(tplid int, x, y, w, h float64) (string, float64, float64, float64, float64) {
	if tplid < 0 || tplid >= len(imp.tpls) {
		return "", 0, 0, 0, 0
	}
	tpl := imp.tpls[tplid]

	tw := tpl.w
	th := tpl.h

	if w == 0 && h == 0 {
		w = tw
		h = th
	} else if w == 0 {
		w = h * tw / th
	} else if h == 0 {
		h = w * th / tw
	}

	scaleX := w / tw
	scaleY := h / th
	tx := x
	ty := -y - h

	return fmt.Sprintf("/GOFPDITPL%d", tplid+imp.tplIdOffset), scaleX, scaleY, tx, ty
}
