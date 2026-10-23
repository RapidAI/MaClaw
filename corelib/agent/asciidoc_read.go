package agent

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// extractAsciiDocText turns AsciiDoc into Markdown-like text: headings, lists,
// tables, fenced code, and image captions. It is a line scanner, not a full
// AsciiDoc implementation.
func extractAsciiDocText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("AsciiDoc", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	out := renderAsciiDoc(string(data))
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("AsciiDoc 里没有可读取的正文")
	}
	return out, nil
}

func renderAsciiDoc(src string) string {
	src = strings.TrimPrefix(src, "\uFEFF")
	if strings.IndexByte(src, '\n') < 0 && strings.IndexByte(src, '\r') >= 0 {
		src = strings.ReplaceAll(src, "\r", "\n")
	}
	lines := strings.Split(src, "\n")
	var b strings.Builder
	var para []string
	var caption string
	var hardBreak bool
	flushPara := func() {
		hardBreak = false
		if len(para) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(para, " "))
		para = nil
	}
	flushCaption := func() {
		if caption == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(caption)
		caption = ""
	}
	inComment := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "////" {
			inComment = !inComment
			continue
		}
		if inComment || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if trimmed == "...." || trimmed == "----" || (strings.HasPrefix(trimmed, "[source") && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "----") {
			flushPara()
			flushCaption()
			lang := ""
			closer := trimmed
			start := i + 1
			if strings.HasPrefix(trimmed, "[source") {
				lang = asciiDocSourceLang(trimmed)
				closer = "----"
				start = i + 2
			}
			var body []string
			j := start
			for ; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == closer {
					break
				}
				body = append(body, strings.TrimRight(lines[j], "\r"))
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("```")
			b.WriteString(lang)
			b.WriteByte('\n')
			b.WriteString(strings.Join(body, "\n"))
			b.WriteString("\n```")
			if j < len(lines) {
				i = j
			} else {
				i = len(lines) - 1
			}
			continue
		}
		if strings.HasPrefix(line, "=") && (strings.HasPrefix(line, "= ") || asciiDocHeading(line)) {
			flushPara()
			level, title := asciiDocHeadingText(line)
			if title == "" {
				continue
			}
			flushCaption()
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(strings.Repeat("#", level))
			b.WriteByte(' ')
			b.WriteString(title)
			continue
		}
		if marker, item, ok := asciiDocListItem(line); ok {
			item, _ = stripAsciiDocHardBreak(item)
			item = rewriteAsciiDocInline(item)
			flushPara()
			flushCaption()
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			writeAsciiDocListMarker(&b, marker)
			b.WriteString(item)
			continue
		}
		if term, def, ok := asciiDocDefinition(trimmed); ok {
			flushPara()
			flushCaption()
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(term)
			b.WriteString(": ")
			b.WriteString(def)
			continue
		}
		if trimmed == "|===" {
			flushPara()
			var rows [][]string
			j := i + 1
			for ; j < len(lines); j++ {
				rowLine := strings.TrimSpace(lines[j])
				if rowLine == "|===" {
					break
				}
				// A line with no cell marker continues the previous cell.
				if rowLine == "" || rowLine == "+" || strings.HasPrefix(rowLine, "//") {
					continue
				}
				if _, skip := asciiDocAttributeLine(rowLine); skip {
					continue
				}
				if !strings.Contains(rowLine, "|") {
					if n := len(rows); n > 0 && len(rows[n-1]) > 0 {
						last := len(rows[n-1]) - 1
						rows[n-1][last] = strings.TrimSpace(rows[n-1][last] + " " + rowLine)
					}
					continue
				}
				rows = append(rows, splitAsciiDocRow(rowLine))
			}
			if len(rows) > 0 {
				// cols="2" and cols="2,2" are a column count. A line that already
				// holds several cells stays one row, so an explicit empty cell remains.
				rows = packAsciiDocRows(rows, asciiDocColsBefore(lines, i))
				writeAsciiDocTable(&b, caption, rows)
				caption = ""
			}
			if j < len(lines) {
				i = j
			} else {
				i = len(lines) - 1
			}
			continue
		}
		if strings.HasPrefix(trimmed, "image::") {
			flushPara()
			flushCaption()
			path, alt := asciiDocImage(trimmed)
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			if alt != "" {
				b.WriteString("图片：")
				b.WriteString(path)
				b.WriteString("（")
				b.WriteString(alt)
				b.WriteString("）")
			} else if path != "" {
				b.WriteString("图片：")
				b.WriteString(path)
			}
			continue
		}
		// A block title is a dot glued to the title. ". item" is an ordered list.
		if len(trimmed) > 1 && trimmed[0] == '.' && trimmed[1] != ' ' && trimmed[1] != '\t' && trimmed[1] != '.' {
			flushPara()
			flushCaption()
			caption = strings.TrimSpace(trimmed[1:])
			continue
		}
		if label, skip := asciiDocAttributeLine(trimmed); skip {
			flushPara()
			if label != "" && caption == "" {
				caption = label
			}
			continue
		}
		if trimmed == "" {
			flushPara()
			continue
		}
		text, br := stripAsciiDocHardBreak(trimmed)
		if text == "" && br {
			hardBreak = true
			continue
		}
		if len(para) == 0 {
			flushCaption()
		}
		text = rewriteAsciiDocInline(text)
		if hardBreak && len(para) > 0 {
			para[len(para)-1] += "\n" + text
		} else if text != "" {
			para = append(para, text)
		}
		hardBreak = br
	}
	flushPara()
	flushCaption()
	return strings.TrimSpace(b.String())
}

// stripAsciiDocHardBreak removes a trailing " +" or a lone "+".
// That marker is a line break, not part of the sentence.
func stripAsciiDocHardBreak(line string) (string, bool) {
	if line == "+" {
		return "", true
	}
	if strings.HasSuffix(line, " +") {
		return strings.TrimRight(line[:len(line)-2], " \t"), true
	}
	return line, false
}

func asciiDocHeading(line string) bool {
	i := 0
	for i < len(line) && line[i] == '=' {
		i++
	}
	return i >= 2 && i < len(line) && line[i] == ' '
}

func asciiDocHeadingText(line string) (int, string) {
	i := 0
	for i < len(line) && line[i] == '=' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ' ' {
		return 0, ""
	}
	level := i
	if level > 6 {
		level = 6
	}
	return level, strings.TrimSpace(line[i:])
}

// asciiDocDefinition matches "term:: definition". The "::" must end the term,
// so a C++ name such as std::vector stays in the sentence.
func asciiDocDefinition(line string) (term, def string, ok bool) {
	for i := 0; i+1 < len(line); i++ {
		if line[i] != ':' || line[i+1] != ':' {
			continue
		}
		if i > 0 && line[i-1] == ':' {
			continue
		}
		rest := line[i+2:]
		if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
			continue
		}
		term = strings.TrimSpace(line[:i])
		if term == "" {
			return "", "", false
		}
		return term, strings.TrimSpace(rest), true
	}
	return "", "", false
}

func asciiDocListItem(line string) (marker, item string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", "", false
	}
	if marker, item, ok = asciiDocRepeatMarker(trimmed, '*'); ok {
		return marker, item, true
	}
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") {
		return "-", strings.TrimSpace(trimmed[1:]), true
	}
	if marker, item, ok = asciiDocRepeatMarker(trimmed, '.'); ok {
		return marker, item, true
	}
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(trimmed) && trimmed[i] == '.' && (trimmed[i+1] == ' ' || trimmed[i+1] == '\t') {
		return trimmed[:i+1], strings.TrimSpace(trimmed[i+2:]), true
	}
	return "", "", false
}

// asciiDocRepeatMarker matches "*", "**", ".", ".." and deeper runs.
// The run must be followed by whitespace and item text, so ".参数表" stays a title.
func asciiDocRepeatMarker(line string, mark byte) (marker, item string, ok bool) {
	i := 0
	for i < len(line) && line[i] == mark {
		i++
	}
	if i == 0 || i >= len(line) || (line[i] != ' ' && line[i] != '\t') {
		return "", "", false
	}
	item = strings.TrimSpace(line[i:])
	if item == "" {
		return "", "", false
	}
	return line[:i], item, true
}

func writeAsciiDocListMarker(b *strings.Builder, marker string) {
	if marker != "" && strings.Trim(marker, "*") == "" {
		if n := len(marker); n > 1 {
			b.WriteString(strings.Repeat("  ", n-1))
		}
		b.WriteString("- ")
		return
	}
	if marker == "-" {
		b.WriteString("- ")
		return
	}
	b.WriteString(marker)
	b.WriteByte(' ')
}

// asciiDocAttributeLine reports a full-line attribute block. [source…] stays
// with the fence handler. A known admonition becomes a label; every other
// bracket line is syntax, including [NOTE] and [cols="2,2"].
func asciiDocAttributeLine(line string) (label string, skip bool) {
	if len(line) < 3 || line[0] != '[' || line[len(line)-1] != ']' || strings.HasPrefix(line, "[source") {
		return "", false
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	name := inner
	if i := strings.IndexAny(inner, ", "); i >= 0 {
		name = inner[:i]
	}
	switch strings.ToLower(name) {
	case "note":
		return "注意", true
	case "tip":
		return "提示", true
	case "warning", "caution":
		return "警告", true
	case "important":
		return "重要", true
	default:
		return "", true
	}
}

func rewriteAsciiDocInline(line string) string {
	var b strings.Builder
	rest := line
	for {
		i := strings.Index(rest, "image:")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		if i > 0 {
			prev := rest[i-1]
			if prev != ' ' && prev != '\t' && prev != '(' {
				b.WriteString(rest[:i+len("image:")])
				rest = rest[i+len("image:"):]
				continue
			}
		}
		b.WriteString(rest[:i])
		macro := rest[i:]
		prefix := "image:"
		if strings.HasPrefix(macro, "image::") {
			prefix = "image::"
		}
		body := macro[len(prefix):]
		pathEnd := strings.IndexByte(body, '[')
		if pathEnd <= 0 {
			b.WriteString(prefix)
			rest = body
			continue
		}
		altRel := strings.IndexByte(body[pathEnd:], ']')
		if altRel < 0 {
			b.WriteString(prefix)
			rest = body
			continue
		}
		path := body[:pathEnd]
		alt := body[pathEnd+1 : pathEnd+altRel]
		b.WriteString("图片：")
		b.WriteString(path)
		if alt != "" {
			b.WriteString("（")
			b.WriteString(alt)
			b.WriteString("）")
		}
		rest = body[pathEnd+altRel+1:]
	}
}

func splitAsciiDocRow(line string) []string {
	var cells []string
	var cur strings.Builder
	started := false
	flush := func() {
		if !started {
			return
		}
		cells = append(cells, strings.TrimSpace(cur.String()))
		cur.Reset()
	}
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			if started {
				cur.WriteByte('|')
			}
			i += 2
			continue
		}
		// A glued spec such as a|, h|, 2+|, or ^.^h| is the cell marker, not text.
		if n := asciiDocCellSpec(line[i:]); n > 0 {
			flush()
			started = true
			i += n + 1
			continue
		}
		if line[i] == '|' {
			flush()
			started = true
			i++
			if n := asciiDocCellSpec(line[i:]); n > 0 {
				i += n + 1
			}
			continue
		}
		if started {
			cur.WriteByte(line[i])
		}
		i++
	}
	flush()
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// asciiDocCellSpec returns the length of a cell spec glued to a following "|".
// The spec is span digits, alignment, and one style letter (a, d, e, h, l, m, s).
func asciiDocCellSpec(s string) int {
	pos := 0
	for {
		j := pos
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == pos {
			break
		}
		if j < len(s) && s[j] == '.' {
			k := j + 1
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			if k > j+1 {
				j = k
			}
		}
		if j < len(s) && (s[j] == '*' || s[j] == '+') {
			pos = j + 1
			continue
		}
		break
	}
	best := 0
	for a := 0; a <= 1; a++ {
		p1 := pos
		if a == 1 {
			if p1 >= len(s) || (s[p1] != '<' && s[p1] != '>' && s[p1] != '^') {
				continue
			}
			p1++
		}
		for d := 0; d <= 1; d++ {
			p2 := p1
			if d == 1 {
				if p2+1 >= len(s) || s[p2] != '.' || (s[p2+1] != '<' && s[p2+1] != '>' && s[p2+1] != '^') {
					continue
				}
				p2 += 2
			}
			for st := 0; st <= 1; st++ {
				p3 := p2
				if st == 1 {
					if p3 >= len(s) || !asciiDocStyleByte(s[p3]) {
						continue
					}
					p3++
				}
				if p3 > best && p3 < len(s) && s[p3] == '|' {
					best = p3
				}
			}
		}
	}
	return best
}

func asciiDocStyleByte(b byte) bool {
	switch b {
	case 'a', 'd', 'e', 'h', 'l', 'm', 's':
		return true
	default:
		return false
	}
}

// asciiDocColsBefore reads a cols attribute on the bracket line that
// introduces this table. A caption or a comment may sit between them.
func asciiDocColsBefore(lines []string, tableAt int) int {
	found := 0
	for k := tableAt - 1; k >= 0; k-- {
		line := strings.TrimSpace(strings.TrimRight(lines[k], "\r"))
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if len(line) > 1 && line[0] == '.' && line[1] != ' ' && line[1] != '\t' && line[1] != '.' {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && !strings.HasPrefix(line, "[source") {
			if found == 0 {
				found = asciiDocColsCount(line)
			}
			continue
		}
		break
	}
	return found
}

func asciiDocColsCount(line string) int {
	lower := strings.ToLower(line)
	const key = "cols"
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], key)
		if i < 0 {
			return 0
		}
		i += from
		endKey := i + len(key)
		if (i > 0 && isHTMLAttrNameByte(lower[i-1])) || (endKey < len(lower) && isHTMLAttrNameByte(lower[endKey])) {
			from = endKey
			continue
		}
		j := endKey
		for j < len(line) && isASCIISpace(line[j]) {
			j++
		}
		if j >= len(line) || line[j] != '=' {
			from = endKey
			continue
		}
		j++
		for j < len(line) && isASCIISpace(line[j]) {
			j++
		}
		if j >= len(line) {
			return 0
		}
		var spec string
		if line[j] == '"' || line[j] == '\'' {
			q := line[j]
			j++
			k := strings.IndexByte(line[j:], q)
			if k < 0 {
				spec = line[j:]
			} else {
				spec = line[j : j+k]
			}
		} else {
			k := j
			for k < len(line) && line[k] != ',' && line[k] != ']' && !isASCIISpace(line[k]) {
				k++
			}
			spec = line[j:k]
		}
		return asciiDocColSpecCount(spec)
	}
	return 0
}

// asciiDocColSpecCount treats a bare integer as the column count.
// cols="2" is two columns. cols="2,2" and cols="2*" are also two.
func asciiDocColSpecCount(spec string) int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0
	}
	if n, err := strconv.Atoi(spec); err == nil && n > 0 {
		if n > 50 {
			return 50
		}
		return n
	}
	count := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i > 0 && i < len(part) && part[i] == '*' {
			if n, err := strconv.Atoi(part[:i]); err == nil && n > 0 {
				count += n
				continue
			}
		}
		count++
	}
	if count > 50 {
		return 50
	}
	return count
}

// packAsciiDocRows fills rows across the column count.
// cols="2" does that. With no cols attribute, a wide first row sets the count
// when every later row is a single cell, which is the usual AsciiDoc table.
// A row wider than the count is left as written, so "| 泵 | | 12" stays one row.
func packAsciiDocRows(rows [][]string, cols int) [][]string {
	if cols <= 1 {
		cols = inferAsciiDocCols(rows)
	}
	if cols <= 1 {
		return rows
	}
	short := false
	for _, row := range rows {
		if len(row) > cols {
			return rows
		}
		if len(row) > 0 && len(row) < cols {
			short = true
		}
	}
	if !short {
		return rows
	}
	var cells []string
	for _, row := range rows {
		cells = append(cells, row...)
	}
	if len(cells) <= 1 {
		return rows
	}
	packed := make([][]string, 0, (len(cells)+cols-1)/cols)
	for i := 0; i < len(cells); i += cols {
		end := i + cols
		if end > len(cells) {
			end = len(cells)
		}
		packed = append(packed, append([]string(nil), cells[i:end]...))
	}
	return packed
}

// inferAsciiDocCols uses the first row when the body is one cell per line.
func inferAsciiDocCols(rows [][]string) int {
	if len(rows) < 2 || len(rows[0]) <= 1 {
		return 0
	}
	for _, row := range rows[1:] {
		if len(row) != 1 {
			return 0
		}
	}
	width := len(rows[0])
	if width > 50 {
		return 50
	}
	return width
}

func writeAsciiDocTable(b *strings.Builder, caption string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	if caption != "" {
		b.WriteString(caption)
		b.WriteString("\n\n")
	}
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	writeAsciiDocRow(b, rows[0], cols)
	b.WriteByte('\n')
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString("| ---")
	}
	b.WriteString(" |")
	for _, row := range rows[1:] {
		b.WriteByte('\n')
		writeAsciiDocRow(b, row, cols)
	}
}

func writeAsciiDocRow(b *strings.Builder, row []string, cols int) {
	b.WriteString("|")
	for i := 0; i < cols; i++ {
		cell := ""
		if i < len(row) {
			cell = strings.ReplaceAll(row[i], "|", `\|`)
		}
		b.WriteByte(' ')
		b.WriteString(cell)
		b.WriteString(" |")
	}
}

func asciiDocSourceLang(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "[source")
	line = strings.TrimSuffix(line, "]")
	line = strings.TrimPrefix(line, ",")
	if i := strings.IndexAny(line, ",]"); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

func asciiDocImage(line string) (path, alt string) {
	rest := strings.TrimPrefix(line, "image::")
	bracket := strings.IndexByte(rest, '[')
	if bracket < 0 {
		return strings.TrimSpace(rest), ""
	}
	path = strings.TrimSpace(rest[:bracket])
	attrs := strings.TrimSuffix(rest[bracket+1:], "]")
	first := attrs
	if i := strings.IndexByte(attrs, ','); i >= 0 {
		first = attrs[:i]
	}
	if !strings.Contains(first, "=") {
		alt = strings.TrimSpace(first)
	}
	return path, alt
}
