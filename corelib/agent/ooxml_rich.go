package agent

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Office comments, formulas, revisions, charts, and SmartArt are read from
// the OOXML package after the main text extract. OfficeRead stays the body
// reader. This pass only adds parts the body usually drops: comment files,
// OMML structure, tracked deletions, chart caches, and diagram text.

const ooxmlRichMaxItems = 80

type ooxmlRich struct {
	comments  []string
	formulas  []string
	revisions []string
	charts    []string
	smartArt  []string
}

func appendOfficeRichSupplement(filePath, format, text string) (out string) {
	out = text
	defer func() {
		if recover() != nil {
			out = text
		}
	}()
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	switch format {
	case "docx", "pptx", "xlsx":
	default:
		return text
	}
	extra := extractOOXMLRichText(filePath, format, text)
	if strings.TrimSpace(extra) == "" {
		return text
	}
	combined := text + "\n\n" + extra
	if err := validateOfficeReadText(combined); err != nil {
		return text
	}
	return combined
}

func extractOOXMLRichText(filePath, format, body string) string {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return ""
	}
	defer r.Close()
	var rich ooxmlRich
	var pendingComments [][]byte
	var pendingRevisions [][]byte
	var sharedStrings []string
	persons := map[string]string{}
	seen := 0
	for _, f := range r.File {
		name := strings.ToLower(strings.ReplaceAll(f.Name, "\\", "/"))
		kind := ooxmlRichPartKind(format, name)
		if kind == "" {
			continue
		}
		seen++
		if seen > 64 {
			break
		}
		data, err := readZipFile(f)
		if err != nil || len(data) == 0 {
			continue
		}
		switch kind {
		case "body":
			collectOOXMLBodyRich(data, &rich)
		case "comment":
			// Names live in a separate part. Zip order is not the document order.
			pendingComments = append(pendingComments, data)
		case "person":
			collectOOXMLPersons(data, persons)
		case "chart":
			if table := renderOOXMLChart(data); table != "" {
				rich.charts = appendCapped(rich.charts, table)
			}
		case "smartart":
			rich.smartArt = appendCapped(rich.smartArt, collectOOXMLSmartArt(data)...)
		case "sheet":
			rich.formulas = appendCapped(rich.formulas, collectOOXMLSheetFormulas(data)...)
		case "revision":
			// Shared strings may appear later in the zip than the revision part.
			pendingRevisions = append(pendingRevisions, data)
		case "shared":
			sharedStrings = parseOOXMLSharedStrings(data)
		}
	}
	for _, data := range pendingComments {
		rich.comments = appendCapped(rich.comments, collectOOXMLComments(data, persons)...)
	}
	for _, data := range pendingRevisions {
		rich.revisions = appendCapped(rich.revisions, collectOOXMLCellRevisions(data, sharedStrings)...)
	}
	return renderOOXMLRich(body, rich)
}

func ooxmlRichPartKind(format, name string) string {
	switch format {
	case "docx":
		switch {
		case name == "word/comments.xml":
			return "comment"
		case strings.HasPrefix(name, "word/charts/") && strings.HasSuffix(name, ".xml"):
			return "chart"
		case strings.HasPrefix(name, "word/diagrams/data") && strings.HasSuffix(name, ".xml"):
			return "smartart"
		case name == "word/document.xml" || strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer") ||
			name == "word/footnotes.xml" || name == "word/endnotes.xml":
			return "body"
		}
	case "pptx":
		switch {
		case strings.HasPrefix(name, "ppt/comments/") && strings.HasSuffix(name, ".xml"):
			return "comment"
		case name == "ppt/authors.xml" || (strings.HasPrefix(name, "ppt/commentauthors") && strings.HasSuffix(name, ".xml")):
			return "person"
		case strings.HasPrefix(name, "ppt/charts/") && strings.HasSuffix(name, ".xml"):
			return "chart"
		case strings.HasPrefix(name, "ppt/diagrams/data") && strings.HasSuffix(name, ".xml"):
			return "smartart"
		case strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml"):
			return "body"
		}
	case "xlsx":
		switch {
		case strings.HasPrefix(name, "xl/persons/") && strings.HasSuffix(name, ".xml"):
			return "person"
		case strings.HasPrefix(name, "xl/comments") && strings.HasSuffix(name, ".xml"):
			return "comment"
		case strings.HasPrefix(name, "xl/threadedcomments/") && strings.HasSuffix(name, ".xml"):
			return "comment"
		case strings.HasPrefix(name, "xl/charts/") && strings.HasSuffix(name, ".xml"):
			return "chart"
		case strings.HasPrefix(name, "xl/diagrams/data") && strings.HasSuffix(name, ".xml"):
			return "smartart"
		case strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml"):
			return "sheet"
		case name == "xl/sharedstrings.xml":
			return "shared"
		case strings.HasPrefix(name, "xl/revisions/") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "header"):
			return "revision"
		}
	}
	return ""
}

func renderOOXMLRich(body string, rich ooxmlRich) string {
	var b strings.Builder
	writeRichSection(&b, "## 批注", filterNewRichLines(body, rich.comments))
	writeRichSection(&b, "## 公式", rich.formulas)
	writeRichSection(&b, "## 修订", rich.revisions)
	writeRichSection(&b, "## 图表", rich.charts)
	writeRichSection(&b, "## SmartArt", filterNewRichLines(body, rich.smartArt))
	return strings.TrimSpace(b.String())
}

func writeRichSection(b *strings.Builder, heading string, lines []string) {
	if len(lines) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(heading)
	b.WriteByte('\n')
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func filterNewRichLines(body string, lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		text := richLinePayload(line)
		if text != "" && bodyHasRichPayload(body, text) {
			continue
		}
		out = append(out, line)
	}
	return out
}

// bodyHasRichPayload reports whether payload is already its own span in the
// body. A short comment such as "12" must survive when it only occurs inside
// a longer token such as "12月".
func bodyHasRichPayload(body, payload string) bool {
	if payload == "" {
		return false
	}
	for from := 0; from < len(body); {
		i := strings.Index(body[from:], payload)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(payload)
		if richSpanBounded(body, start, end) {
			return true
		}
		from = start + 1
	}
	return false
}

func richSpanBounded(body string, start, end int) bool {
	if start > 0 {
		r, size := utf8.DecodeLastRuneInString(body[:start])
		if size == 0 || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(body) {
		r, size := utf8.DecodeRuneInString(body[end:])
		if size == 0 || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func richLinePayload(line string) string {
	if i := strings.Index(line, ": "); i >= 0 && i < 40 {
		return strings.TrimSpace(line[i+2:])
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "- "))
}

func appendCapped(dst []string, items ...string) []string {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || len(dst) >= ooxmlRichMaxItems {
			continue
		}
		if len([]rune(item)) > 500 {
			item = string([]rune(item)[:500])
		}
		dup := false
		for _, have := range dst {
			if have == item {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, item)
		}
	}
	return dst
}

type revFrame struct {
	kind   string
	author string
	b      strings.Builder
}

func collectOOXMLBodyRich(data []byte, rich *ooxmlRich) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []revFrame
	inText := false
	var ruby, skipRT int
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "ruby":
				ruby++
			case "rt":
				if ruby > 0 {
					skipRT++
				}
			case "ins", "del", "moveFrom", "moveTo":
				stack = append(stack, revFrame{kind: t.Name.Local, author: xmlLocalAttr(t, "author")})
			case "t", "delText":
				if skipRT == 0 {
					inText = true
				}
			case "oMath":
				node := parseXMLSubtree(dec, t)
				formula := strings.TrimSpace(formatOMML(node))
				if formula == "" {
					break
				}
				rich.formulas = appendCapped(rich.formulas, formula)
				if n := len(stack); n > 0 {
					stack[n-1].b.WriteString(formula)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "rt":
				if skipRT > 0 {
					skipRT--
				}
			case "ruby":
				if ruby > 0 {
					ruby--
				}
			case "t", "delText":
				inText = false
			}
			if n := len(stack); n > 0 && t.Name.Local == stack[n-1].kind {
				frame := stack[n-1]
				stack = stack[:n-1]
				if text := strings.TrimSpace(frame.b.String()); text != "" {
					rich.revisions = appendCapped(rich.revisions, formatRevision(frame.kind, frame.author, text))
				}
			}
		case xml.CharData:
			if inText && len(stack) > 0 {
				stack[len(stack)-1].b.Write(t)
			}
		}
	}
}

func formatRevision(kind, author, text string) string {
	label := "修订"
	switch kind {
	case "ins":
		label = "插入"
	case "del":
		label = "删除"
	case "moveFrom":
		label = "移出"
	case "moveTo":
		label = "移入"
	}
	if author = strings.TrimSpace(author); author != "" {
		return label + " " + author + ": " + text
	}
	return label + ": " + text
}

func collectOOXMLComments(data []byte, persons map[string]string) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	var authors []string
	var depth int
	var phonetic, ruby int
	var inText bool
	var inAuthors bool
	var inAuthorName bool
	var author, ref string
	var buf, authorName strings.Builder
	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		author = ooxmlCommentAuthor(author, authors, persons)
		line := text
		switch {
		case ref != "" && author != "":
			line = ref + " " + author + ": " + text
		case author != "":
			line = author + ": " + text
		case ref != "":
			line = ref + ": " + text
		}
		out = appendCapped(out, line)
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "ruby" {
				ruby++
			}
			// rPh/phoneticPr are Excel pronunciation. w:rt inside w:ruby is the Word one.
			if t.Name.Local == "rPh" || t.Name.Local == "phoneticPr" || (t.Name.Local == "rt" && ruby > 0) {
				phonetic++
			}
			if depth == 0 && t.Name.Local == "authors" {
				inAuthors = true
				continue
			}
			if inAuthors && depth == 0 && t.Name.Local == "author" {
				inAuthorName = true
				authorName.Reset()
				continue
			}
			if depth == 0 && isOOXMLCommentElement(t.Name.Local) {
				depth = 1
				author = firstXMLAttr(t, "author", "authorId", "personId")
				ref = xmlLocalAttr(t, "ref")
				buf.Reset()
				continue
			}
			if depth > 0 {
				depth++
				if t.Name.Local == "t" || t.Name.Local == "text" {
					inText = true
				}
			}
		case xml.EndElement:
			if t.Name.Local == "rt" && ruby > 0 && phonetic > 0 {
				phonetic--
			}
			if t.Name.Local == "ruby" && ruby > 0 {
				ruby--
			}
			if phonetic > 0 && (t.Name.Local == "rPh" || t.Name.Local == "phoneticPr") {
				phonetic--
			}
			if inAuthorName && t.Name.Local == "author" {
				authors = append(authors, strings.TrimSpace(authorName.String()))
				inAuthorName = false
				continue
			}
			if inAuthors && t.Name.Local == "authors" {
				inAuthors = false
				continue
			}
			if depth == 0 {
				continue
			}
			if t.Name.Local == "t" || t.Name.Local == "text" {
				inText = false
			}
			if depth > 1 && (t.Name.Local == "p" || t.Name.Local == "br") {
				buf.WriteByte('\n')
			}
			depth--
			if depth == 0 {
				flush()
				author, ref = "", ""
			}
		case xml.CharData:
			if inAuthorName {
				authorName.Write(t)
				continue
			}
			if depth > 0 && inText && phonetic == 0 {
				buf.Write(t)
			}
		}
	}
	return out
}

func ooxmlCommentAuthor(raw string, authors []string, persons map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if name := persons[raw]; name != "" {
		return name
	}
	if len(authors) == 0 {
		return raw
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index >= len(authors) || authors[index] == "" {
		return raw
	}
	return authors[index]
}

func collectOOXMLPersons(data []byte, persons map[string]string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "person", "cmAuthor", "author":
			id := firstXMLAttr(start, "id", "authorId")
			name := firstXMLAttr(start, "displayName", "name")
			if id != "" && name != "" {
				persons[id] = name
			}
		}
	}
}

func isOOXMLCommentElement(local string) bool {
	switch local {
	case "comment", "cm", "threadedComment":
		return true
	default:
		return false
	}
}

type pendingSheetFormula struct {
	cell, formula, sharedID, ref, kind string
}

func collectOOXMLSheetFormulas(data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var pending []pendingSheetFormula
	shared := map[string]string{}
	var cell, formula, sharedID, ref, kind string
	inF := false
	var buf strings.Builder
	flushCell := func() {
		if formula != "" && sharedID != "" {
			shared[sharedID] = formula
		}
		if cell != "" || formula != "" || sharedID != "" {
			pending = append(pending, pendingSheetFormula{cell: cell, formula: formula, sharedID: sharedID, ref: ref, kind: kind})
		}
		cell, formula, sharedID, ref, kind = "", "", "", "", ""
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				cell = xmlLocalAttr(t, "r")
				formula = ""
				sharedID = ""
				ref = ""
				kind = ""
			case "f":
				inF = true
				buf.Reset()
				sharedID = xmlLocalAttr(t, "si")
				ref = xmlLocalAttr(t, "ref")
				kind = xmlLocalAttr(t, "t")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "f":
				inF = false
				formula = strings.TrimSpace(buf.String())
			case "c":
				flushCell()
			}
		case xml.CharData:
			if inF {
				buf.Write(t)
			}
		}
	}
	var out []string
	for _, item := range pending {
		formula := item.formula
		if formula == "" && item.sharedID != "" {
			formula = shared[item.sharedID]
		}
		if formula == "" {
			continue
		}
		if !strings.HasPrefix(formula, "=") {
			formula = "=" + formula
		}
		// An array formula is stored only on the anchor cell. ref is the range it fills.
		label := item.cell
		if strings.EqualFold(item.kind, "array") && item.ref != "" {
			label = item.ref
		}
		if label == "" {
			out = appendCapped(out, formula)
		} else {
			out = appendCapped(out, label+": "+formula)
		}
	}
	return out
}

// collectOOXMLCellRevisions reads a cell-change record. Excel stores the
// changed range on rcc@sqref (the inner c@r is the full-cell form). A value
// with t="s" is an index into xl/sharedStrings.xml, not the text itself.
func collectOOXMLCellRevisions(data []byte, shared []string) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	var cell, side, cellType string
	var oldText, newText, vbuf strings.Builder
	inV := false
	phonetic := 0
	writeValue := func(local string) {
		if phonetic > 0 {
			return
		}
		text := strings.TrimSpace(vbuf.String())
		if local == "v" && strings.EqualFold(cellType, "s") {
			text = lookupSharedString(shared, text)
		}
		switch side {
		case "oc":
			oldText.WriteString(text)
		case "nc":
			newText.WriteString(text)
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "rPh" || t.Name.Local == "phoneticPr" {
				phonetic++
			}
			switch t.Name.Local {
			case "rcc":
				cell = xmlLocalAttr(t, "sqref")
				if cell == "" {
					cell = xmlLocalAttr(t, "r")
				}
				cellType = ""
				oldText.Reset()
				newText.Reset()
			case "oc", "nc":
				side = t.Name.Local
				if typ := xmlLocalAttr(t, "t"); typ != "" {
					cellType = typ
				}
			case "c":
				if ref := xmlLocalAttr(t, "r"); ref != "" {
					cell = ref
				}
				if typ := xmlLocalAttr(t, "t"); typ != "" {
					cellType = typ
				}
			case "v", "t":
				inV = true
				vbuf.Reset()
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inV = false
				writeValue(t.Name.Local)
			case "rPh", "phoneticPr":
				if phonetic > 0 {
					phonetic--
				}
			case "oc", "nc":
				side = ""
				cellType = ""
			case "rcc":
				oldS := strings.TrimSpace(oldText.String())
				newS := strings.TrimSpace(newText.String())
				if oldS != "" || newS != "" {
					line := oldS + " → " + newS
					if cell != "" {
						line = cell + ": " + line
					}
					out = appendCapped(out, line)
				}
				cell = ""
				cellType = ""
			}
		case xml.CharData:
			if inV && phonetic == 0 {
				vbuf.Write(t)
			}
		}
	}
	return out
}

func parseOOXMLSharedStrings(data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	var buf strings.Builder
	inSI, inT := false, false
	phonetic := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "rPh" || t.Name.Local == "phoneticPr" {
				phonetic++
			}
			switch t.Name.Local {
			case "si":
				if !inSI {
					inSI = true
					buf.Reset()
				}
			case "t":
				if inSI {
					inT = true
				}
			}
		case xml.EndElement:
			if phonetic > 0 && (t.Name.Local == "rPh" || t.Name.Local == "phoneticPr") {
				phonetic--
			}
			switch t.Name.Local {
			case "t":
				inT = false
			case "si":
				if inSI {
					// Keep empty entries so later indexes stay aligned.
					out = append(out, strings.TrimSpace(buf.String()))
					inSI = false
					inT = false
				}
			}
		case xml.CharData:
			if inSI && inT && phonetic == 0 {
				buf.Write(t)
			}
		}
	}
	return out
}

func lookupSharedString(shared []string, raw string) string {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 || n >= len(shared) {
		return raw
	}
	return shared[n]
}

type chartSeries struct {
	name  string
	cats  []string
	vals  []string
	sizes []string
}

const chartMaxPoints = 512

func chartPointPut(dst []string, idx int, text string) []string {
	if idx < 0 || idx >= chartMaxPoints {
		return append(dst, text)
	}
	for len(dst) <= idx {
		dst = append(dst, "")
	}
	if dst[idx] == "" {
		dst[idx] = text
	} else {
		dst[idx] += text
	}
	return dst
}

func renderOOXMLChart(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var title strings.Builder
	var series []chartSeries
	var cur *chartSeries
	titleDepth, serDepth, txDepth, catDepth, valDepth, sizeDepth := 0, 0, 0, 0, 0, 0
	inText := false
	pointIdx := -1
	var buf strings.Builder
	flushText := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		switch {
		case txDepth > 0 && catDepth == 0 && valDepth == 0 && sizeDepth == 0 && cur != nil:
			if cur.name == "" {
				cur.name = text
			} else {
				cur.name += text
			}
		case catDepth > 0 && sizeDepth == 0 && cur != nil:
			cur.cats = chartPointPut(cur.cats, pointIdx, text)
		case valDepth > 0 && sizeDepth == 0 && cur != nil:
			cur.vals = chartPointPut(cur.vals, pointIdx, text)
		case sizeDepth > 0 && cur != nil:
			cur.sizes = chartPointPut(cur.sizes, pointIdx, text)
		case titleDepth > 0 && serDepth == 0:
			if title.Len() > 0 {
				title.WriteByte(' ')
			}
			title.WriteString(text)
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			startTitle := t.Name.Local == "title" && titleDepth == 0
			startSer := t.Name.Local == "ser" && serDepth == 0
			startTx := t.Name.Local == "tx" && txDepth == 0 && (serDepth > 0 || startSer)
			// Bar and line charts use cat/val. Scatter and bubble charts use xVal/yVal.
			startCat := (t.Name.Local == "cat" || t.Name.Local == "xVal") && catDepth == 0 && (serDepth > 0 || startSer)
			startVal := (t.Name.Local == "val" || t.Name.Local == "yVal") && valDepth == 0 && (serDepth > 0 || startSer)
			startSize := t.Name.Local == "bubbleSize" && sizeDepth == 0 && (serDepth > 0 || startSer)
			if titleDepth > 0 {
				titleDepth++
			}
			if serDepth > 0 {
				serDepth++
			}
			if txDepth > 0 {
				txDepth++
			}
			if catDepth > 0 {
				catDepth++
			}
			if valDepth > 0 {
				valDepth++
			}
			if sizeDepth > 0 {
				sizeDepth++
			}
			if startTitle {
				titleDepth = 1
			}
			if startSer {
				if cur != nil {
					series = append(series, *cur)
				}
				cur = &chartSeries{}
				serDepth = 1
			}
			if startTx {
				txDepth = 1
			}
			if startCat {
				catDepth = 1
			}
			if startVal {
				valDepth = 1
			}
			if startSize {
				sizeDepth = 1
			}
			if t.Name.Local == "pt" {
				// A missing cache point is omitted. idx is its row, not its position in the file.
				pointIdx = -1
				if n, err := strconv.Atoi(strings.TrimSpace(xmlLocalAttr(t, "idx"))); err == nil && n >= 0 {
					pointIdx = n
				}
			}
			if t.Name.Local == "t" || t.Name.Local == "v" {
				inText = true
				buf.Reset()
			}
		case xml.EndElement:
			if inText && (t.Name.Local == "t" || t.Name.Local == "v") {
				flushText()
				inText = false
			}
			if t.Name.Local == "pt" {
				pointIdx = -1
			}
			if titleDepth > 0 {
				titleDepth--
			}
			if txDepth > 0 {
				txDepth--
			}
			if catDepth > 0 {
				catDepth--
			}
			if valDepth > 0 {
				valDepth--
			}
			if sizeDepth > 0 {
				sizeDepth--
			}
			if serDepth > 0 {
				serDepth--
				if serDepth == 0 && cur != nil {
					series = append(series, *cur)
					cur = nil
				}
			}
		case xml.CharData:
			if inText {
				buf.Write(t)
			}
		}
	}
	if cur != nil {
		series = append(series, *cur)
	}
	return formatChartTable(strings.TrimSpace(title.String()), series)
}

func formatChartTable(title string, series []chartSeries) string {
	usable := make([]chartSeries, 0, len(series))
	for _, ser := range series {
		if ser.name == "" && len(ser.vals) == 0 && len(ser.cats) == 0 && len(ser.sizes) == 0 {
			continue
		}
		usable = append(usable, ser)
	}
	series = usable
	if title == "" && len(series) == 0 {
		return ""
	}
	var b strings.Builder
	if title != "" {
		b.WriteString("### ")
		b.WriteString(title)
		b.WriteByte('\n')
	}
	if len(series) == 0 {
		return strings.TrimSpace(b.String())
	}
	rows := 0
	for _, ser := range series {
		n := len(ser.vals)
		if len(ser.cats) > n {
			n = len(ser.cats)
		}
		if len(ser.sizes) > n {
			n = len(ser.sizes)
		}
		if n > rows {
			rows = n
		}
	}
	b.WriteString("| |")
	for _, ser := range series {
		name := ser.name
		if name == "" {
			name = "系列"
		}
		b.WriteString(" ")
		b.WriteString(strings.ReplaceAll(name, "|", "/"))
		b.WriteString(" |")
	}
	b.WriteString("\n| --- |")
	for range series {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')
	if rows == 0 {
		return strings.TrimSpace(b.String())
	}
	for i := 0; i < rows; i++ {
		cat := ""
		for _, ser := range series {
			if i < len(ser.cats) && ser.cats[i] != "" {
				cat = ser.cats[i]
				break
			}
		}
		b.WriteString("| ")
		b.WriteString(strings.ReplaceAll(cat, "|", "/"))
		b.WriteString(" |")
		for _, ser := range series {
			val := ""
			if i < len(ser.vals) {
				val = ser.vals[i]
			}
			if i < len(ser.sizes) && ser.sizes[i] != "" {
				if val != "" {
					val += "（" + ser.sizes[i] + "）"
				} else {
					val = ser.sizes[i]
				}
			}
			b.WriteString(" ")
			b.WriteString(strings.ReplaceAll(val, "|", "/"))
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func collectOOXMLSmartArt(data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	inText := false
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
				buf.Reset()
			}
		case xml.EndElement:
			if t.Name.Local == "t" && inText {
				if text := strings.TrimSpace(buf.String()); text != "" {
					out = appendCapped(out, "- "+text)
				}
				inText = false
			}
		case xml.CharData:
			if inText {
				buf.Write(t)
			}
		}
	}
	return out
}

type xmlNode struct {
	name  string
	attrs []xml.Attr
	text  string
	kids  []*xmlNode
}

func parseXMLSubtree(dec *xml.Decoder, start xml.StartElement) *xmlNode {
	node := &xmlNode{name: start.Name.Local, attrs: start.Attr}
	for {
		tok, err := dec.Token()
		if err != nil {
			return node
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node.kids = append(node.kids, parseXMLSubtree(dec, t))
		case xml.EndElement:
			return node
		case xml.CharData:
			node.text += string(t)
		}
	}
}

func formatOMML(n *xmlNode) string {
	if n == nil {
		return ""
	}
	switch n.name {
	case "t":
		return strings.TrimSpace(n.text)
	case "chr":
		return xmlAttrList(n.attrs, "val")
	case "f":
		return "(" + ommlChild(n, "num") + ")/(" + ommlChild(n, "den") + ")"
	case "sSup":
		return ommlChild(n, "e") + "^" + wrapMath(ommlChild(n, "sup"))
	case "sSub":
		return ommlChild(n, "e") + "_" + wrapMath(ommlChild(n, "sub"))
	case "sSubSup":
		return ommlChild(n, "e") + "_" + wrapMath(ommlChild(n, "sub")) + "^" + wrapMath(ommlChild(n, "sup"))
	case "rad":
		body := ommlChild(n, "e")
		deg := ommlChild(n, "deg")
		if deg == "" {
			return "√(" + body + ")"
		}
		return "√[" + deg + "](" + body + ")"
	case "nary":
		// The operator lives at nary/naryPr/chr, not as a direct child.
		op := ommlFind(n, "chr")
		sub := ommlFind(n, "sub")
		sup := ommlFind(n, "sup")
		body := ommlChild(n, "e")
		var b strings.Builder
		b.WriteString(op)
		if sub != "" {
			b.WriteByte('_')
			b.WriteString(wrapMath(sub))
		}
		if sup != "" {
			b.WriteByte('^')
			b.WriteString(wrapMath(sup))
		}
		if body != "" {
			b.WriteString("(")
			b.WriteString(body)
			b.WriteString(")")
		}
		return b.String()
	case "func":
		return ommlChild(n, "fName") + "(" + ommlChild(n, "e") + ")"
	case "m":
		var rows []string
		for _, kid := range n.kids {
			if kid.name != "mr" {
				continue
			}
			rows = append(rows, strings.Join(ommlChildren(kid, "e"), ", "))
		}
		if len(rows) == 0 {
			var b strings.Builder
			for _, kid := range n.kids {
				b.WriteString(formatOMML(kid))
			}
			return b.String()
		}
		return strings.Join(rows, "; ")
	case "d":
		beg, begOK := ommlProp(n, "begChr")
		end, endOK := ommlProp(n, "endChr")
		if !begOK {
			beg = "("
		}
		if !endOK {
			end = ")"
		}
		sep := ", "
		if value, ok := ommlProp(n, "sepChr"); ok {
			sep = value
		}
		return beg + strings.Join(ommlChildren(n, "e"), sep) + end
	default:
		var b strings.Builder
		if text := strings.TrimSpace(n.text); text != "" {
			b.WriteString(text)
		}
		for _, kid := range n.kids {
			b.WriteString(formatOMML(kid))
		}
		return b.String()
	}
}

func ommlChild(n *xmlNode, name string) string {
	if n == nil {
		return ""
	}
	for _, kid := range n.kids {
		if kid.name == name {
			return formatOMML(kid)
		}
	}
	return ""
}

func ommlFind(n *xmlNode, name string) string {
	if direct := ommlChild(n, name); direct != "" {
		return direct
	}
	for _, kid := range n.kids {
		if strings.HasSuffix(kid.name, "Pr") {
			if found := ommlChild(kid, name); found != "" {
				return found
			}
		}
	}
	return ""
}

func ommlChildren(n *xmlNode, name string) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, kid := range n.kids {
		if kid.name == name {
			out = append(out, formatOMML(kid))
		}
	}
	return out
}

// ommlProp reads an attribute stored on the element or under a *Pr wrapper.
// ok is false when the element is absent, so an explicit empty fence stays empty.
func ommlProp(n *xmlNode, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	nodes := append([]*xmlNode(nil), n.kids...)
	for _, kid := range n.kids {
		if strings.HasSuffix(kid.name, "Pr") {
			nodes = append(nodes, kid.kids...)
		}
	}
	for _, kid := range nodes {
		if kid.name == name {
			return xmlAttrList(kid.attrs, "val"), true
		}
	}
	return "", false
}

func wrapMath(s string) string {
	if len([]rune(s)) > 1 {
		return "(" + s + ")"
	}
	return s
}

func xmlLocalAttr(start xml.StartElement, local string) string {
	return xmlAttrList(start.Attr, local)
}

func xmlAttrList(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

func firstXMLAttr(start xml.StartElement, names ...string) string {
	for _, name := range names {
		if value := xmlLocalAttr(start, name); value != "" {
			return value
		}
	}
	return ""
}
