package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// fileCompanionLibraryFile is library.json. Titles live beside the rows so a
// cloud refresh that rebuilds a row does not forget the document title.
type fileCompanionLibraryFile struct {
	Items  []FileCompanionLibraryItem          `json:"items"`
	Titles map[string]fileCompanionTitleRecord `json:"titles,omitempty"`
}

// fileCompanionTitleRecord is one file's title at a size and mtime. An entry
// with an empty title means the file was read and had nothing to show, so the
// next list does not open it again until the file changes.
type fileCompanionTitleRecord struct {
	Title string `json:"title,omitempty"`
	Size  int64  `json:"size"`
	Mod   int64  `json:"mod"`
}

var fileCompanionTitleFillRunning atomic.Bool

// fileCompanionTitleKey is the library path folded the way a Windows file is
// one file under either slash direction or drive-letter case. It does not
// resolve symlinks: that walk stays off the library write path.
func fileCompanionTitleKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == "" {
		return ""
	}
	if filepath.VolumeName(cleaned) != "" || strings.HasPrefix(cleaned, `\\`) || strings.HasPrefix(cleaned, `//`) {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func pruneFileCompanionTitles(items []FileCompanionLibraryItem, titles map[string]fileCompanionTitleRecord) map[string]fileCompanionTitleRecord {
	if len(titles) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(items))
	for _, item := range items {
		if key := fileCompanionTitleKey(item.Path); key != "" {
			keep[key] = struct{}{}
		}
	}
	out := make(map[string]fileCompanionTitleRecord, len(keep))
	for key, rec := range titles {
		if _, ok := keep[key]; ok {
			out[key] = rec
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyFileCompanionTitles copies stored titles onto the rows a list sends.
// The caller must not hold fileCompanionLibraryMu.
func (a *App) applyFileCompanionTitles(items []FileCompanionLibraryItem) {
	if a == nil || len(items) == 0 {
		return
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := a.readFileCompanionLibraryFileLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok || len(stored.Titles) == 0 {
		return
	}
	for i := range items {
		key := fileCompanionTitleKey(items[i].Path)
		if key == "" {
			continue
		}
		rec, found := stored.Titles[key]
		if !found || rec.Title == "" {
			continue
		}
		items[i].Title = rec.Title
	}
}

func (a *App) startFileCompanionTitleFill() {
	if a == nil || !fileCompanionLibraryTitleFillAsync {
		return
	}
	if !fileCompanionTitleFillRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer fileCompanionTitleFillRunning.Store(false)
		if !a.fillFileCompanionLibraryTitles() {
			return
		}
		// The refresh stamps whatever titles are already stored, then emits,
		// and only then clears its flag. A fill that writes in that gap used
		// to drop its event, so the list on screen kept the pre-write rows
		// until the next poll. Wait out the refresh that is already running.
		// Its event has been sent once the flag drops. This publish uses the
		// current generation, which the panel ignores while that refresh is
		// still the list on screen, so it does not replace the hub catalog.
		// Past the deadline the refresh is still downloading; it stamps on
		// the way out and will see the titles this fill just stored.
		deadline := time.Now().Add(30 * time.Second)
		for fileCompanionLibrarySyncRunning.Load() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if fileCompanionLibrarySyncRunning.Load() {
			return
		}
		listed := FileCompanionLibraryList{
			Syncing:     false,
			SyncGen:     fileCompanionLibrarySyncGen.Load(),
			NotesReply:  a.fileCompanionNotesReply.Load(),
			NotesReview: a.fileCompanionNotesReview.Load(),
		}
		a.emitEvent("file-companion:library", a.stampFileCompanionLibraryList(listed))
	}()
}

// fillFileCompanionLibraryTitles reads a title for each library file whose
// size or mtime is not already recorded. The file bytes are read outside the
// library lock. A later list stamps the rows from the map.
func (a *App) fillFileCompanionLibraryTitles() bool {
	if a == nil {
		return false
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := a.readFileCompanionLibraryFileLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok || len(stored.Items) == 0 {
		return false
	}
	type pending struct {
		key  string
		path string
	}
	seen := map[string]pending{}
	for _, item := range stored.Items {
		key := fileCompanionTitleKey(item.Path)
		path := strings.TrimSpace(item.Path)
		if key == "" || path == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if rec, known := stored.Titles[key]; known && rec.Size == info.Size() && rec.Mod == info.ModTime().UnixNano() {
			continue
		}
		seen[key] = pending{key: key, path: path}
	}
	if len(seen) == 0 {
		return false
	}
	updates := make(map[string]fileCompanionTitleRecord, len(seen))
	paths := make(map[string]string, len(seen))
	for key, job := range seen {
		title, size, mod, readOK := fileCompanionDocumentTitle(job.path)
		if !readOK {
			continue
		}
		updates[key] = fileCompanionTitleRecord{Title: title, Size: size, Mod: mod}
		paths[key] = job.path
	}
	if len(updates) == 0 {
		return false
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	current, ok := a.readFileCompanionLibraryFileLocked()
	if !ok {
		return false
	}
	if current.Titles == nil {
		current.Titles = map[string]fileCompanionTitleRecord{}
	}
	changed := false
	for key, rec := range updates {
		info, err := os.Stat(paths[key])
		if err != nil || info.IsDir() || info.Size() != rec.Size || info.ModTime().UnixNano() != rec.Mod {
			continue
		}
		prev, exists := current.Titles[key]
		if exists && prev == rec {
			continue
		}
		current.Titles[key] = rec
		changed = true
	}
	if !changed {
		return false
	}
	return a.writeFileCompanionLibraryFileLocked(current.Items, current.Titles, false) == nil
}

func fileCompanionCleanTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 240 {
		value = strings.TrimSpace(string(runes[:240]))
	}
	return value
}

// fileCompanionDocumentTitle reads a short title from the file. ok is false
// when the file could not be read, so the caller tries again later. A file
// with no title returns "", true.
func fileCompanionDocumentTitle(path string) (title string, size int64, mod int64, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", 0, 0, false
	}
	size = info.Size()
	mod = info.ModTime().UnixNano()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		title, ok = fileCompanionPDFTitle(path, size)
	case ".md", ".markdown", ".mdx":
		title, ok = fileCompanionHeadingTitle(path)
	case ".html", ".htm":
		title, ok = fileCompanionHTMLTitle(path)
	case ".docx", ".pptx", ".xlsx":
		title, ok = fileCompanionOfficeCoreTitle(path)
	default:
		return "", size, mod, true
	}
	if !ok {
		return "", size, mod, false
	}
	return fileCompanionCleanTitle(title), size, mod, true
}

func fileCompanionReadHeadTail(path string, size int64, head, tail int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if size < 0 {
		size = 0
	}
	if size <= head+tail {
		buf := make([]byte, size)
		_, err = io.ReadFull(file, buf)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			return buf, nil
		}
		return buf, err
	}
	buf := make([]byte, head+tail)
	if _, err = io.ReadFull(file, buf[:head]); err != nil {
		return nil, err
	}
	if _, err = file.ReadAt(buf[head:], size-tail); err != nil {
		return nil, err
	}
	return buf, nil
}

type pdfTitlePick struct {
	text  string
	score int
}

func fileCompanionPDFTitle(path string, size int64) (string, bool) {
	const head = 64 << 10
	const tail = 192 << 10
	data, err := fileCompanionReadHeadTail(path, size, head, tail)
	if err != nil {
		return "", false
	}
	// A large file is not one buffer. Joining the head to the tail invents a
	// /Title at the seam and lets a mark in the head borrow Info keys from
	// the tail. The Info dictionary lives at the end, so a tie keeps the tail.
	if size <= head+tail {
		return fileCompanionPDFTitleFrom(data).text, true
	}
	if int64(len(data)) < head {
		return fileCompanionPDFTitleFrom(data).text, true
	}
	headPick := fileCompanionPDFTitleFrom(data[:head])
	tailPick := fileCompanionPDFTitleFrom(data[head:])
	if tailPick.score > headPick.score || (tailPick.score == headPick.score && tailPick.text != "") {
		return tailPick.text, true
	}
	return headPick.text, true
}

func fileCompanionPDFTitleFrom(data []byte) pdfTitlePick {
	bestScore := 0
	bestAt := -1
	best := ""
	for from := 0; from < len(data); {
		rel := bytes.Index(data[from:], []byte("/Title"))
		if rel < 0 {
			break
		}
		at := from + rel
		from = at + len("/Title")
		if at > 0 && pdfNameByte(data[at-1]) {
			continue
		}
		next := at + len("/Title")
		if next < len(data) && pdfNameByte(data[next]) {
			continue
		}
		j := skipPDFSpace(data, next)
		if j >= len(data) {
			continue
		}
		var text string
		var parsed bool
		switch data[j] {
		case '(':
			text, _, parsed = pdfLiteralString(data, j)
			if parsed {
				text = pdfTextBytes([]byte(text))
			}
		case '<':
			text, _, parsed = pdfHexString(data, j)
		default:
			text, parsed = pdfIndirectTitle(data, j)
		}
		if !parsed {
			continue
		}
		text = fileCompanionCleanTitle(text)
		if text == "" {
			continue
		}
		// Score only the dictionary that holds this /Title. A shared window
		// gives an Info title and a later outline title the same score, and
		// the later outline entry used to win.
		score := pdfTitleDictScore(data, at)
		if score < 1 {
			continue
		}
		if score > bestScore || (score == bestScore && at > bestAt) {
			bestScore = score
			bestAt = at
			best = text
		}
	}
	if best != "" {
		return pdfTitlePick{text: best, score: bestScore}
	}
	if xmp := fileCompanionXMPTitle(data); xmp != "" {
		return pdfTitlePick{text: xmp, score: 1}
	}
	return pdfTitlePick{}
}

func skipPDFSpace(data []byte, j int) int {
	for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
		j++
	}
	return j
}

// pdfIndirectTitle reads /Title N G R when that object is in the same slice.
func pdfIndirectTitle(data []byte, at int) (string, bool) {
	num, at, ok := pdfInt(data, at)
	if !ok || num <= 0 {
		return "", false
	}
	at = skipPDFSpace(data, at)
	gen, at, ok := pdfInt(data, at)
	if !ok || gen < 0 {
		return "", false
	}
	at = skipPDFSpace(data, at)
	if at >= len(data) || data[at] != 'R' || (at+1 < len(data) && pdfNameByte(data[at+1])) {
		return "", false
	}
	return pdfObjectString(data, num, gen)
}

func pdfInt(data []byte, at int) (int, int, bool) {
	if at >= len(data) || data[at] < '0' || data[at] > '9' {
		return 0, at, false
	}
	n := 0
	for at < len(data) && data[at] >= '0' && data[at] <= '9' {
		n = n*10 + int(data[at]-'0')
		if n > 9_999_999 {
			return 0, at, false
		}
		at++
	}
	return n, at, true
}

func pdfObjectString(data []byte, num, gen int) (string, bool) {
	needle := strconv.AppendInt(nil, int64(num), 10)
	for from := 0; from < len(data); {
		rel := bytes.Index(data[from:], needle)
		if rel < 0 {
			return "", false
		}
		at := from + rel
		from = at + len(needle)
		if at > 0 && data[at-1] >= '0' && data[at-1] <= '9' {
			continue
		}
		j := skipPDFSpace(data, at+len(needle))
		got, j, ok := pdfInt(data, j)
		if !ok || got != gen {
			continue
		}
		j = skipPDFSpace(data, j)
		if j+3 > len(data) || string(data[j:j+3]) != "obj" || (j+3 < len(data) && pdfNameByte(data[j+3])) {
			continue
		}
		j = skipPDFSpace(data, j+3)
		if j >= len(data) {
			return "", false
		}
		switch data[j] {
		case '(':
			text, _, ok := pdfLiteralString(data, j)
			if !ok {
				continue
			}
			return pdfTextBytes([]byte(text)), true
		case '<':
			if j+1 < len(data) && data[j+1] == '<' {
				return "", false
			}
			text, _, ok := pdfHexString(data, j)
			if !ok {
				continue
			}
			return text, true
		default:
			continue
		}
	}
	return "", false
}

func pdfNameByte(b byte) bool {
	if b < 33 || b > 126 {
		return false
	}
	switch b {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return false
	default:
		return true
	}
}

func pdfLiteralString(data []byte, start int) (string, int, bool) {
	if start >= len(data) || data[start] != '(' {
		return "", start, false
	}
	var out strings.Builder
	depth := 1
	for i := start + 1; i < len(data) && out.Len() < 2048; i++ {
		c := data[i]
		switch c {
		case '\\':
			if i+1 >= len(data) {
				return "", start, false
			}
			i++
			n := data[i]
			switch n {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'b':
				out.WriteByte('\b')
			case 'f':
				out.WriteByte('\f')
			case '(', ')', '\\':
				out.WriteByte(n)
			case '\n':
			case '\r':
				if i+1 < len(data) && data[i+1] == '\n' {
					i++
				}
			default:
				if n >= '0' && n <= '7' {
					oct := int(n - '0')
					for k := 0; k < 2 && i+1 < len(data) && data[i+1] >= '0' && data[i+1] <= '7'; k++ {
						i++
						oct = oct*8 + int(data[i]-'0')
					}
					out.WriteByte(byte(oct))
				} else {
					out.WriteByte(n)
				}
			}
		case '(':
			depth++
			out.WriteByte(c)
		case ')':
			depth--
			if depth == 0 {
				return out.String(), i + 1, true
			}
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return "", start, false
}

func pdfHexString(data []byte, start int) (string, int, bool) {
	if start >= len(data) || data[start] != '<' {
		return "", start, false
	}
	if start+1 < len(data) && data[start+1] == '<' {
		return "", start, false
	}
	end := bytes.IndexByte(data[start+1:], '>')
	if end < 0 || end > 4096 {
		return "", start, false
	}
	raw := data[start+1 : start+1+end]
	closeAt := start + 1 + end + 1
	digits := make([]byte, 0, len(raw))
	for _, c := range raw {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			digits = append(digits, c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		default:
			return "", start, false
		}
	}
	if len(digits) == 0 {
		return "", closeAt, true
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	decoded := make([]byte, len(digits)/2)
	if _, err := hex.Decode(decoded, digits); err != nil {
		return "", start, false
	}
	return pdfTextBytes(decoded), closeAt, true
}

// pdfTitleDictScore is 1, plus 10 when this dictionary looks like document
// info, minus 8 when it looks like an outline entry. The scan stops at the
// dictionary's own closing bracket, so a later outline does not borrow the
// info keys and the info title does not inherit /Parent.
func pdfTitleDictScore(data []byte, titleAt int) int {
	i := titleAt + len("/Title")
	end := i + 800
	if end > len(data) {
		end = len(data)
	}
	depth := 1
	bonus := false
	penalty := false
	for i < end && depth > 0 {
		switch data[i] {
		case '(':
			_, n, ok := pdfLiteralString(data, i)
			if !ok || n <= i {
				return pdfTitleScore(bonus, penalty)
			}
			i = n
		case '<':
			if i+1 < len(data) && data[i+1] == '<' {
				depth++
				i += 2
				continue
			}
			_, n, ok := pdfHexString(data, i)
			if !ok || n <= i {
				return pdfTitleScore(bonus, penalty)
			}
			i = n
		case '>':
			if i+1 < len(data) && data[i+1] == '>' {
				depth--
				i += 2
				continue
			}
			i++
		case '/':
			name, n := pdfDictName(data, i)
			if n <= i {
				i++
				continue
			}
			switch name {
			case "Producer", "Creator", "Author", "ModDate", "CreationDate", "Subject":
				bonus = true
			case "Parent", "Dest", "Prev", "Next":
				penalty = true
			}
			i = n
		default:
			i++
		}
	}
	return pdfTitleScore(bonus, penalty)
}

func pdfTitleScore(bonus, penalty bool) int {
	score := 1
	if bonus {
		score += 10
	}
	if penalty {
		score -= 8
	}
	return score
}

func pdfDictName(data []byte, at int) (string, int) {
	if at >= len(data) || data[at] != '/' {
		return "", at
	}
	i := at + 1
	for i < len(data) && pdfNameByte(data[i]) {
		i++
	}
	if i == at+1 {
		return "", at
	}
	return string(data[at+1 : i]), i
}

func pdfTextBytes(decoded []byte) string {
	if len(decoded) >= 2 && decoded[0] == 0xFE && decoded[1] == 0xFF {
		return utf16BytesToString(decoded[2:], true)
	}
	if len(decoded) >= 2 && decoded[0] == 0xFF && decoded[1] == 0xFE {
		return utf16BytesToString(decoded[2:], false)
	}
	if utf8.Valid(decoded) {
		return string(decoded)
	}
	runes := make([]rune, len(decoded))
	for i, b := range decoded {
		runes[i] = rune(b)
	}
	return string(runes)
}

func utf16BytesToString(raw []byte, big bool) string {
	if len(raw)%2 == 1 {
		raw = raw[:len(raw)-1]
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		if big {
			units[i] = uint16(raw[i*2])<<8 | uint16(raw[i*2+1])
		} else {
			units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
		}
	}
	return string(utf16.Decode(units))
}

func fileCompanionXMPTitle(data []byte) string {
	lower := bytes.ToLower(data)
	idx := bytes.Index(lower, []byte("dc:title"))
	if idx < 0 {
		return ""
	}
	rest := data[idx:]
	if len(rest) > 4000 {
		rest = rest[:4000]
	}
	li := bytes.Index(bytes.ToLower(rest), []byte("<rdf:li"))
	if li < 0 {
		return ""
	}
	seg := rest[li:]
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 {
		return ""
	}
	end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</rdf:li"))
	if end < 0 {
		return ""
	}
	return fileCompanionCleanTitle(html.UnescapeString(string(seg[gt+1 : gt+end])))
}

func fileCompanionHeadingTitle(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "", false
	}
	text := strings.TrimPrefix(string(buf[:n]), "\uFEFF")
	if strings.HasPrefix(text, "---") {
		rest := text[3:]
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			if title, ok := markdownFrontTitle(rest[:end]); ok {
				return title, true
			}
			text = rest[end+1:]
			if nl := strings.IndexByte(text, '\n'); nl >= 0 && strings.HasPrefix(strings.TrimLeft(text[:nl], "\r"), "---") {
				text = text[nl+1:]
			}
		}
	}
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		if i > 80 {
			break
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trim, "#") {
			continue
		}
		hashes := 0
		for hashes < len(trim) && trim[hashes] == '#' {
			hashes++
		}
		if hashes < 1 || hashes > 6 || hashes >= len(trim) {
			continue
		}
		if trim[hashes] != ' ' && trim[hashes] != '\t' {
			continue
		}
		return stripMarkdownHeading(strings.TrimSpace(trim[hashes:])), true
	}
	return "", true
}

// markdownFrontTitle reads a plain YAML title. An empty value and a block
// scalar marker are not titles by themselves: the folded lines are the
// title, and when those lines are missing the body heading is.
func markdownFrontTitle(front string) (string, bool) {
	lines := strings.Split(front, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 6 || !strings.EqualFold(trimmed[:6], "title:") {
			continue
		}
		value := strings.TrimSpace(trimmed[6:])
		switch value {
		case ">", ">-", ">+", "|", "|-", "|+":
			var parts []string
			for _, next := range lines[i+1:] {
				if strings.TrimSpace(next) == "" {
					if len(parts) > 0 {
						break
					}
					continue
				}
				if !yamlIndented(next) {
					break
				}
				parts = append(parts, strings.TrimSpace(next))
			}
			joined := strings.TrimSpace(strings.Join(parts, " "))
			if joined == "" {
				return "", false
			}
			return joined, true
		default:
			plain := strings.Trim(value, `"'`)
			if strings.TrimSpace(plain) == "" {
				return "", false
			}
			return plain, true
		}
	}
	return "", false
}

func yamlIndented(line string) bool {
	line = strings.TrimRight(line, "\r")
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

func stripMarkdownHeading(value string) string {
	for {
		open := strings.Index(value, "[")
		if open < 0 {
			break
		}
		close := strings.Index(value[open:], "](")
		if close < 0 {
			break
		}
		close += open
		paren := strings.Index(value[close+2:], ")")
		if paren < 0 {
			break
		}
		value = value[:open] + value[open+1:close] + value[close+2+paren+1:]
	}
	return strings.TrimSpace(strings.Trim(value, "*_`"))
}

func fileCompanionHTMLTitle(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	buf := make([]byte, 32<<10)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "", false
	}
	data := buf[:n]
	lower := bytes.ToLower(data)
	idx := bytes.Index(lower, []byte("<title"))
	if idx < 0 {
		return "", true
	}
	seg := data[idx:]
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 || gt > 200 {
		return "", true
	}
	end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</title"))
	if end < 0 {
		return "", true
	}
	return html.UnescapeString(string(seg[gt+1 : gt+end])), true
}

func fileCompanionOfficeCoreTitle(path string) (string, bool) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", false
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !strings.EqualFold(filepath.ToSlash(file.Name), "docProps/core.xml") {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return "", false
		}
		raw, readErr := io.ReadAll(io.LimitReader(body, 64<<10))
		body.Close()
		if readErr != nil {
			return "", false
		}
		return officeDCTitle(raw), true
	}
	return "", true
}

func officeDCTitle(body []byte) string {
	lower := bytes.ToLower(body)
	idx := bytes.Index(lower, []byte("<dc:title"))
	if idx < 0 {
		return ""
	}
	seg := body[idx:]
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 || gt > 200 {
		return ""
	}
	end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</dc:title"))
	if end < 0 {
		return ""
	}
	return html.UnescapeString(string(seg[gt+1 : gt+end]))
}
