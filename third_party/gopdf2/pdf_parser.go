package gopdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Pre-compiled regexes for PDF parser — avoids recompilation on every call.
var (
	reObjHeader = regexp.MustCompile(`(\d+)\s+0\s+obj\b`)
	reRootRef   = regexp.MustCompile(`/Root\s+(\d+)\s+0\s+R`)
	reObjRef    = regexp.MustCompile(`(\d+)\s+0\s+R`)
	reNamedRef  = regexp.MustCompile(`/(\w+)\s+(\d+)\s+0\s+R`)
	reMediaBox  = regexp.MustCompile(`/MediaBox\s*\[\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\]`)
)

// ============================================================
// PDF raw parser — reads cross-ref table, objects, streams
// from raw PDF bytes. Used for text/image extraction.
// ============================================================

// rawPDFParser parses a raw PDF byte slice to extract objects.
type rawPDFParser struct {
	data    []byte
	objects map[int]rawPDFObject // objNum -> object
	pages   []rawPDFPage
	root    int // root catalog obj number

	// form caches (per parser instance = per extraction call): form streams
	// recur on every page (stamps, watermarks) — fonts, nested XObjects and
	// the un-offset runs are parsed once and translated on reuse. Runs are
	// cached per mediaBox height: base Y depends on the calling page's
	// height, and pages within one document may differ.
	fontCache map[int]map[string]*fontInfo
	xobjCache map[int]map[string]int
	formRuns  map[int]map[float64][]ExtractedText // objNum -> pageH -> runs
}

// rawPDFObject holds a parsed PDF object.
type rawPDFObject struct {
	num    int
	dict   string // dictionary content between << >>
	stream []byte // decompressed stream content (nil if not a stream)
}

// rawPDFPage holds parsed page info.
type rawPDFPage struct {
	objNum    int
	mediaBox  [4]float64
	contents  []int // content stream obj numbers
	resources rawPDFResources
}

// rawPDFResources holds resource references for a page.
type rawPDFResources struct {
	fonts map[string]int // /F1 -> obj number
	xobjs map[string]int // /Im1 -> obj number
}

// newRawPDFParser creates a parser for the given PDF data.
func newRawPDFParser(data []byte) (*rawPDFParser, error) {
	p := &rawPDFParser{
		data:      data,
		objects:   make(map[int]rawPDFObject),
		fontCache: make(map[int]map[string]*fontInfo),
		xobjCache: make(map[int]map[string]int),
		formRuns:  make(map[int]map[float64][]ExtractedText),
	}
	if err := p.parse(); err != nil {
		return nil, fmt.Errorf("pdf parse: %w", err)
	}
	return p, nil
}

func (p *rawPDFParser) parse() error {
	p.parseObjects()
	p.parseObjectStreams()
	p.findRoot()
	p.parsePages()
	return nil
}

// objHeaderLoc locates one "N 0 obj" header: the object number, the start of
// the header (first digit) and the offset just past "obj".
type objHeaderLoc struct {
	num       int
	start     int // offset of the first digit of N
	bodyStart int // offset just past "obj"
}

// isPDFSpace reports whether c is PDF whitespace.
func isPDFSpace(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', '\f', 0:
		return true
	}
	return false
}

// scanObjectHeaders finds every "N 0 obj" header without regexp. The regex
// equivalent dominated extraction CPU on large files; a needle scan for
// " obj" with backward parsing is an order of magnitude faster.
func scanObjectHeaders(data []byte) []objHeaderLoc {
	var out []objHeaderLoc
	const needle = " obj"
	pos := 0
	for {
		idx := bytes.Index(data[pos:], []byte(needle))
		if idx < 0 {
			break
		}
		i := pos + idx
		pos = i + len(needle)
		// parse backwards: skip spaces, expect generation '0', skip spaces,
		// parse digits, verify a space separates N and the generation
		j := i - 1
		for j >= 0 && isPDFSpace(data[j]) {
			j--
		}
		if j < 0 || data[j] != '0' {
			continue
		}
		j--
		for j >= 0 && isPDFSpace(data[j]) {
			j--
		}
		e := j // last digit of N
		for j >= 0 && data[j] >= '0' && data[j] <= '9' {
			j--
		}
		if j == e || e+1 >= len(data) || !isPDFSpace(data[e+1]) {
			continue // no digits, or the "0" was part of a multi-digit generation
		}
		if j >= 0 {
			c := data[j]
			if c != '>' && c != '%' && !isPDFSpace(c) {
				continue // not a token boundary before N
			}
		}
		num, err := strconv.Atoi(string(data[j+1 : e+1]))
		if err != nil {
			continue
		}
		out = append(out, objHeaderLoc{num: num, start: j + 1, bodyStart: i + len(needle)})
	}
	return out
}

// parseObjects finds all "N 0 obj ... endobj" blocks.
func (p *rawPDFParser) parseObjects() {
	headers := scanObjectHeaders(p.data)
	// Locate all "endobj" markers once, then pair them with object headers
	// using a two-pointer merge (both are in file order). Scanning from each
	// object start would be O(objects × filesize) on large PDFs.
	endObj := []byte("endobj")
	endPositions := make([]int, 0, len(headers))
	for pos := 0; ; {
		idx := bytes.Index(p.data[pos:], endObj)
		if idx < 0 {
			break
		}
		pos += idx
		endPositions = append(endPositions, pos)
		pos += len(endObj)
	}
	nextEnd := 0
	for _, h := range headers {
		num := h.num
		objStart := h.start
		// find the first endobj at or after this object's start
		for nextEnd < len(endPositions) && endPositions[nextEnd] < objStart {
			nextEnd++
		}
		if nextEnd >= len(endPositions) {
			continue
		}
		endPos := endPositions[nextEnd]
		objData := p.data[objStart : endPos+len(endObj)]
		obj := rawPDFObject{num: num}
		// extract dictionary
		if dictStart := bytes.Index(objData, []byte("<<")); dictStart >= 0 {
			obj.dict = extractDict(objData[dictStart:])
		}
		// extract stream
		if streamStart := bytes.Index(objData, []byte("stream")); streamStart >= 0 {
			streamData := objData[streamStart+6:]
			// skip \r\n or \n after "stream"
			if len(streamData) > 0 && streamData[0] == '\r' {
				streamData = streamData[1:]
			}
			if len(streamData) > 0 && streamData[0] == '\n' {
				streamData = streamData[1:]
			}
			if endStream := bytes.Index(streamData, []byte("endstream")); endStream >= 0 {
				raw := streamData[:endStream]
				// trim trailing whitespace
				raw = bytes.TrimRight(raw, "\r\n")
				obj.stream = p.decodeStream(raw, obj.dict)
			}
		}
		p.objects[num] = obj
	}
}

// extractDict extracts the outermost <<...>> from data.
func extractDict(data []byte) string {
	depth := 0
	start := -1
	for i := 0; i < len(data)-1; i++ {
		if data[i] == '<' && data[i+1] == '<' {
			if depth == 0 {
				start = i
			}
			depth++
			i++
		} else if data[i] == '>' && data[i+1] == '>' {
			depth--
			if depth == 0 {
				return string(data[start : i+2])
			}
			i++
		}
	}
	if start >= 0 {
		return string(data[start:])
	}
	return ""
}

// parseObjectStreams expands objects stored inside object streams
// (/Type /ObjStm), used by PDF 1.5+ files. Without this, pages, fonts and
// ToUnicode CMaps of modern PDFs are invisible to the raw parser.
func (p *rawPDFParser) parseObjectStreams() {
	// Collect ObjStm object numbers first (map iteration + insert is unsafe).
	var stmNums []int
	for num, obj := range p.objects {
		if obj.stream != nil && strings.Contains(obj.dict, "/ObjStm") {
			stmNums = append(stmNums, num)
		}
	}
	for _, num := range stmNums {
		p.expandObjectStream(num)
	}
}

// expandObjectStream decodes one ObjStm into its contained objects.
func (p *rawPDFParser) expandObjectStream(num int) {
	obj := p.objects[num]
	n := extractIntValue(obj.dict, "/N")
	first := extractIntValue(obj.dict, "/First")
	if n <= 0 || first <= 0 || first > len(obj.stream) {
		return
	}
	header := string(obj.stream[:first])
	fields := strings.Fields(header)
	if len(fields) < 2*n {
		return
	}
	type entry struct {
		objNum int
		offset int
	}
	entries := make([]entry, 0, n)
	for i := 0; i < n; i++ {
		on, err1 := strconv.Atoi(fields[2*i])
		off, err2 := strconv.Atoi(fields[2*i+1])
		if err1 != nil || err2 != nil {
			return
		}
		entries = append(entries, entry{on, off})
	}
	for i, e := range entries {
		start := first + e.offset
		if start < 0 || start > len(obj.stream) {
			continue
		}
		end := len(obj.stream)
		if i+1 < len(entries) {
			end = first + entries[i+1].offset
			if end > len(obj.stream) {
				end = len(obj.stream)
			}
		}
		body := obj.stream[start:end]
		inner := p.parseInnerObject(e.objNum, body)
		p.objects[e.objNum] = inner
	}
}

// parseInnerObject parses the body of an object inside an ObjStm. Bodies lack
// the "N 0 obj" wrapper but otherwise look like normal objects.
func (p *rawPDFParser) parseInnerObject(num int, body []byte) rawPDFObject {
	res := rawPDFObject{num: num}
	s := string(body)
	// strip leading "N 0 obj" if a producer included it
	if m := reObjHeader.FindSubmatchIndex(body); m != nil && m[0] == 0 {
		body = body[m[1]:]
		s = string(body)
	}
	if dictStart := strings.Index(s, "<<"); dictStart >= 0 {
		res.dict = extractDict(body[dictStart:])
	}
	if streamStart := strings.Index(s, "stream"); streamStart >= 0 {
		streamData := body[streamStart+6:]
		if len(streamData) > 0 && streamData[0] == '\r' {
			streamData = streamData[1:]
		}
		if len(streamData) > 0 && streamData[0] == '\n' {
			streamData = streamData[1:]
		}
		if endStream := bytes.Index(streamData, []byte("endstream")); endStream >= 0 {
			raw := streamData[:endStream]
			raw = bytes.TrimRight(raw, "\r\n")
			res.stream = p.decodeStream(raw, res.dict)
		}
	}
	return res
}

// decodeStream applies the filter chain named in dict to raw stream data.
// Supports FlateDecode (with or without predictors), ASCIIHexDecode and
// ASCII85Decode — the combinations covering virtually all content streams,
// ToUnicode CMaps and object streams.
func (p *rawPDFParser) decodeStream(raw []byte, dict string) []byte {
	data := raw
	for _, name := range streamFilterNames(dict) {
		data = p.applyFilter(name, data, dict)
	}
	return data
}

// streamFilterNames extracts the filter list from a dict's /Filter entry —
// a single name (/FlateDecode) or an array ([/ASCII85Decode /FlateDecode]) —
// in application order.
func streamFilterNames(dict string) []string {
	idx := strings.Index(dict, "/Filter")
	if idx < 0 {
		return nil
	}
	rest := strings.TrimLeft(dict[idx+len("/Filter"):], " \t\r\n")
	if strings.HasPrefix(rest, "[") {
		// array of names
		end := strings.Index(rest, "]")
		if end < 0 {
			rest = rest[1:]
		} else {
			rest = rest[1:end]
		}
		var names []string
		for _, m := range reFilterEntry.FindAllStringSubmatch(rest, -1) {
			names = append(names, m[1])
		}
		return names
	}
	if !strings.HasPrefix(rest, "/") {
		return nil
	}
	rest = rest[1:]
	// single name: ends at the next delimiter
	end := strings.IndexAny(rest, " \t\r\n/<>[]")
	if end < 0 {
		end = len(rest)
	}
	if end == 0 {
		return nil
	}
	return []string{rest[:end]}
}

var reFilterEntry = regexp.MustCompile(`/([A-Za-z0-9]+)`)

// applyFilter decodes raw data with a single named filter.
func (p *rawPDFParser) applyFilter(filter string, data []byte, dict string) []byte {
	switch filter {
	case "FlateDecode", "Fl":
		if decoded, err := zlibDecompress(data); err == nil {
			return decodePredictor(decoded, dict)
		}
		return data
	case "ASCIIHexDecode", "AHx":
		return decodeASCIIHex(data)
	case "ASCII85Decode", "A85":
		return decodeASCII85(data)
	default:
		// LZWDecode, DCTDecode, RunLength etc. — return as-is
		return data
	}
}

// decodePredictor handles /DecodeParms predictors (PNG predictors used by
// xref and object streams).
func decodePredictor(data []byte, dict string) []byte {
	idx := strings.Index(dict, "/Predictor")
	if idx < 0 {
		return data
	}
	rest := dict[idx:]
	// predictor number
	pred := 1
	if m := regexpNum.FindStringSubmatch(rest); m != nil {
		pred, _ = strconv.Atoi(m[1])
	}
	if pred <= 1 {
		return data
	}
	columns := 1
	if m := regexpColumns.FindStringSubmatch(rest); m != nil {
		columns, _ = strconv.Atoi(m[1])
	}
	if columns <= 0 {
		columns = 1
	}
	colors := 1
	bpc := 8
	if m := regexpColors.FindStringSubmatch(rest); m != nil {
		colors, _ = strconv.Atoi(m[1])
	}
	if m := regexpBPC.FindStringSubmatch(rest); m != nil {
		bpc, _ = strconv.Atoi(m[1])
	}
	if bpc != 8 {
		return data // only 8-bit PNG predictors supported
	}
	bpp := colors // bytes per pixel for PNG filters
	rowLen := columns * bpp
	if rowLen <= 0 || len(data) < rowLen+1 {
		return data
	}
	out := make([]byte, 0, len(data))
	prev := make([]byte, rowLen)
	pos := 0
	for pos < len(data) {
		ftype := data[pos]
		pos++
		if pos+rowLen > len(data) {
			break
		}
		row := make([]byte, rowLen)
		copy(row, data[pos:pos+rowLen])
		pos += rowLen
		switch ftype {
		case 0: // None
		case 1: // Sub
			for i := bpp; i < rowLen; i++ {
				row[i] += row[i-bpp]
			}
		case 2: // Up
			for i := 0; i < rowLen; i++ {
				row[i] += prev[i]
			}
		case 3: // Average
			for i := 0; i < rowLen; i++ {
				var left byte
				if i >= bpp {
					left = row[i-bpp]
				}
				row[i] += byte((int(left) + int(prev[i])) / 2)
			}
		case 4: // Paeth
			for i := 0; i < rowLen; i++ {
				var left, upLeft byte
				if i >= bpp {
					left = row[i-bpp]
					upLeft = prev[i-bpp]
				}
				row[i] += paeth(left, prev[i], upLeft)
			}
		default:
			// unknown filter type — stop
			return data
		}
		out = append(out, row...)
		copy(prev, row)
	}
	return out
}

var (
	regexpNum     = regexp.MustCompile(`/Predictor\s+(\d+)`)
	regexpColumns = regexp.MustCompile(`/Columns\s+(\d+)`)
	regexpColors  = regexp.MustCompile(`/Colors\s+(\d+)`)
	regexpBPC     = regexp.MustCompile(`/BitsPerComponent\s+(\d+)`)
)

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs64(p-int(a)), abs64(p-int(b)), abs64(p-int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func abs64(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// decodeASCIIHex decodes an ASCIIHexDecode stream.
func decodeASCIIHex(data []byte) []byte {
	out := make([]byte, 0, len(data)/2)
	var hi byte = 0xFF
	for _, c := range data {
		if c == '>' {
			break
		}
		v := hexVal(c)
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' || (c == 0 && v == 0 && c != '0') {
			continue
		}
		if isHexDigit(c) {
			if hi == 0xFF {
				hi = v
			} else {
				out = append(out, hi<<4|v)
				hi = 0xFF
			}
		}
	}
	return out
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// decodeASCII85 decodes an ASCII85Decode stream (up to '->' terminator).
func decodeASCII85(data []byte) []byte {
	out := make([]byte, 0, len(data)*4/5)
	var group [5]byte
	groupLen := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == 0 {
			continue
		}
		if c == '~' {
			break
		}
		if c == 'z' && groupLen == 0 {
			out = append(out, 0, 0, 0, 0)
			continue
		}
		if c < '!' || c > 'u' {
			continue
		}
		group[groupLen] = c - '!'
		groupLen++
		if groupLen == 5 {
			v := uint32(0)
			for _, g := range group {
				v = v*85 + uint32(g)
			}
			out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
			groupLen = 0
		}
	}
	if groupLen > 1 {
		// pad with 'u' (84)
		for j := groupLen; j < 5; j++ {
			group[j] = 84
		}
		v := uint32(0)
		for _, g := range group {
			v = v*85 + uint32(g)
		}
		for j := 0; j < groupLen-1; j++ {
			out = append(out, byte(v>>(24-8*j)))
		}
	}
	return out
}

func zlibDecompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (p *rawPDFParser) findRoot() {
	// Look for /Root N 0 R in trailer or xref stream
	m := reRootRef.FindSubmatch(p.data)
	if m != nil {
		p.root, _ = strconv.Atoi(string(m[1]))
		return
	}
}

func (p *rawPDFParser) parsePages() {
	rootObj, ok := p.objects[p.root]
	if !ok {
		return
	}
	// Find /Pages reference
	pagesRef := extractRef(rootObj.dict, "/Pages")
	if pagesRef <= 0 {
		return
	}
	p.collectPages(pagesRef)
}

func (p *rawPDFParser) collectPages(objNum int) {
	p.collectPagesInherited(objNum, [4]float64{})
}

// collectPagesInherited walks the page tree, propagating inherited
// attributes (MediaBox) from Pages nodes down to their Page leaves.
func (p *rawPDFParser) collectPagesInherited(objNum int, inherited [4]float64) {
	obj, ok := p.objects[objNum]
	if !ok {
		return
	}
	switch pdfObjectType(obj.dict) {
	case "Page":
		page := rawPDFPage{objNum: objNum}
		page.mediaBox = extractMediaBox(obj.dict)
		if page.mediaBox == [4]float64{0, 0, 612, 792} && inherited != [4]float64{} {
			// page lacks its own MediaBox — inherit from the Pages tree
			page.mediaBox = inherited
		}
		page.contents = extractContentRefs(obj.dict)
		page.resources = p.extractResources(obj.dict, objNum)
		p.pages = append(p.pages, page)
	case "Pages":
		// It's a /Pages node — recurse into /Kids with its MediaBox
		inheritedMedia := extractMediaBox(obj.dict)
		if inheritedMedia == [4]float64{0, 0, 612, 792} && inherited != [4]float64{} {
			inheritedMedia = inherited
		}
		kids := extractRefArray(obj.dict, "/Kids")
		for _, kid := range kids {
			p.collectPagesInherited(kid, inheritedMedia)
		}
	}
}

// pdfObjectType extracts the /Type value from a dict string with proper token
// boundaries ("/Page" must not match "/Pages").
func pdfObjectType(dict string) string {
	idx := 0
	for {
		pos := strings.Index(dict[idx:], "/Type")
		if pos < 0 {
			return ""
		}
		pos += idx + len("/Type")
		// skip whitespace
		for pos < len(dict) && (dict[pos] == ' ' || dict[pos] == '\t' || dict[pos] == '\r' || dict[pos] == '\n') {
			pos++
		}
		if pos >= len(dict) || dict[pos] != '/' {
			idx = pos
			continue
		}
		pos++
		start := pos
		for pos < len(dict) && isNameChar(dict[pos]) {
			pos++
		}
		if pos > start {
			return dict[start:pos]
		}
		idx = pos
	}
}

func isNameChar(c byte) bool {
	return c > 0x20 && c != '/' && c != '<' && c != '>' && c != '[' && c != ']' &&
		c != '(' && c != ')' && c != '%' && c != '{' && c != '}'
}

// extractRef extracts a single "N 0 R" reference for a given key.
// Also accepts array-wrapped values (e.g. /DescendantFonts [9 0 R]), taking
// the first reference. Uses string search + pre-compiled regex.
func extractRef(dict, key string) int {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return 0
	}
	rest := dict[idx+len(key):]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if strings.HasPrefix(rest, "[") {
		// Array form: use the first reference inside the brackets.
		end := strings.Index(rest, "]")
		if end < 0 {
			return 0
		}
		if m := reObjRef.FindStringSubmatch(rest[1:end]); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}
	m := reObjRef.FindStringSubmatch(rest)
	if m != nil {
		// Verify the match starts at the beginning (the ref follows the key directly)
		if strings.HasPrefix(rest, m[0]) {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}

// extractRefArray extracts an array of "N 0 R" references for a given key.
func extractRefArray(dict, key string) []int {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return nil
	}
	rest := dict[idx+len(key):]
	// find [ ... ]
	start := strings.Index(rest, "[")
	if start < 0 {
		return nil
	}
	end := strings.Index(rest[start:], "]")
	if end < 0 {
		return nil
	}
	arr := rest[start+1 : start+end]
	matches := reObjRef.FindAllStringSubmatch(arr, -1)
	var refs []int
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		refs = append(refs, n)
	}
	return refs
}

func extractMediaBox(dict string) [4]float64 {
	m := reMediaBox.FindStringSubmatch(dict)
	if m == nil {
		return [4]float64{0, 0, 612, 792} // default letter
	}
	var box [4]float64
	for i := 0; i < 4; i++ {
		box[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	return box
}

func extractContentRefs(dict string) []int {
	// /Contents N 0 R  or  /Contents [N 0 R M 0 R]
	idx := strings.Index(dict, "/Contents")
	if idx < 0 {
		return nil
	}
	rest := dict[idx+9:]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if len(rest) > 0 && rest[0] == '[' {
		return extractRefArray(dict, "/Contents")
	}
	ref := extractRef(dict, "/Contents")
	if ref > 0 {
		return []int{ref}
	}
	return nil
}

func (p *rawPDFParser) extractResources(dict string, pageObjNum int) rawPDFResources {
	res := rawPDFResources{
		fonts: make(map[string]int),
		xobjs: make(map[string]int),
	}
	// Resources can be inline or a reference
	resDict := dict
	resRef := extractRef(dict, "/Resources")
	if resRef > 0 {
		if obj, ok := p.objects[resRef]; ok {
			resDict = obj.dict
		}
	}
	// Extract /Font << /F1 N 0 R ... >>
	p.extractNamedRefs(resDict, "/Font", res.fonts)
	// Extract /XObject << /Im1 N 0 R ... >>
	p.extractNamedRefs(resDict, "/XObject", res.xobjs)
	return res
}

func (p *rawPDFParser) extractNamedRefs(dict, key string, out map[string]int) {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return
	}
	rest := dict[idx+len(key):]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if len(rest) == 0 {
		return
	}
	if rest[0] == '<' && len(rest) > 1 && rest[1] == '<' {
		// inline dict
		inner := extractDict([]byte(rest))
		matches := reNamedRef.FindAllStringSubmatch(inner, -1)
		for _, m := range matches {
			n, _ := strconv.Atoi(m[2])
			out["/"+m[1]] = n
		}
	} else {
		// reference to another object
		m := reObjRef.FindStringSubmatch(rest)
		if m != nil {
			n, _ := strconv.Atoi(m[1])
			if obj, ok := p.objects[n]; ok {
				matches := reNamedRef.FindAllStringSubmatch(obj.dict, -1)
				for _, mm := range matches {
					nn, _ := strconv.Atoi(mm[2])
					out["/"+mm[1]] = nn
				}
			}
		}
	}
}

// getPageContentStream returns the concatenated, decompressed content
// stream(s) for a page.
func (p *rawPDFParser) getPageContentStream(pageIdx int) []byte {
	if pageIdx < 0 || pageIdx >= len(p.pages) {
		return nil
	}
	page := p.pages[pageIdx]
	var buf bytes.Buffer
	for _, ref := range page.contents {
		obj, ok := p.objects[ref]
		if !ok {
			continue
		}
		if obj.stream != nil {
			buf.Write(obj.stream)
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

// ============================================================
// Content stream text operator parser
// ============================================================

// ExtractedText represents a piece of text extracted from a PDF page.
type ExtractedText struct {
	// Text is the extracted text string.
	Text string
	// X is the horizontal position.
	X float64
	// Y is the vertical position.
	Y float64
	// FontName is the PDF font resource name (e.g. "/F1").
	FontName string
	// FontSize is the font size in points.
	FontSize float64
	// Width is the estimated advance width of the text (set by MergeTextRuns
	// and GroupIntoTextLines; 0 when unknown).
	Width float64
}

// ExtractedImage represents an image found on a PDF page.
type ExtractedImage struct {
	// Name is the XObject resource name (e.g. "/Im1").
	Name string
	// Width is the image width in pixels.
	Width int
	// Height is the image height in pixels.
	Height int
	// BitsPerComponent is the bits per color component.
	BitsPerComponent int
	// ColorSpace is the color space name.
	ColorSpace string
	// Filter is the compression filter.
	Filter string
	// Data is the raw (possibly compressed) image data.
	Data []byte
	// ObjNum is the PDF object number.
	ObjNum int
	// X, Y are the position on the page (from the CTM).
	X float64
	// Y position on the page.
	Y float64
	// DisplayWidth is the rendered width on the page.
	DisplayWidth float64
	// DisplayHeight is the rendered height on the page.
	DisplayHeight float64
}
