package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// fileCompanionLibraryFile is library.json. Titles live beside the rows so a
// cloud refresh that rebuilds a row does not forget the document title.
type fileCompanionLibraryFile struct {
	Items  []FileCompanionLibraryItem          `json:"items"`
	Titles map[string]fileCompanionTitleRecord `json:"titles,omitempty"`
}

// fileCompanionTitleVersion bumps when the extractor can see a title it used
// to miss. Records from an older version are read again even when the file
// itself has not changed.
const fileCompanionTitleVersion = 4

// fileCompanionTitleRecord is one file's title at a size and mtime. An entry
// with an empty title means the file was read and had nothing to show, so the
// next list does not open it again until the file or the extractor changes.
type fileCompanionTitleRecord struct {
	Title   string `json:"title,omitempty"`
	Size    int64  `json:"size"`
	Mod     int64  `json:"mod"`
	Version int    `json:"ver,omitempty"`
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
		if rec, known := stored.Titles[key]; known && rec.Size == info.Size() && rec.Mod == info.ModTime().UnixNano() && rec.Version == fileCompanionTitleVersion {
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
		updates[key] = fileCompanionTitleRecord{Title: title, Size: size, Mod: mod, Version: fileCompanionTitleVersion}
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
	case ".adoc", ".asciidoc":
		title, ok = fileCompanionAdocTitle(path)
	case ".html", ".htm", ".xhtml":
		title, ok = fileCompanionHTMLTitle(path)
	case ".docx", ".pptx", ".xlsx":
		title, ok = fileCompanionOfficeCoreTitle(path)
	case ".epub":
		title, ok = fileCompanionEPUBTitle(path)
	case ".xml":
		title, ok = fileCompanionXMLTitle(path)
	case ".yaml", ".yml":
		title, ok = fileCompanionYAMLTitle(path)
	case ".json":
		title, ok = fileCompanionJSONTitle(path)
	case ".eml":
		title, ok = fileCompanionEMLTitle(path)
	case ".txt", ".text":
		title, ok = fileCompanionPlainTextTitle(path)
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
	lowerRest := bytes.ToLower(rest)
	if li := bytes.Index(lowerRest, []byte("<rdf:li")); li >= 0 {
		if title := xmpElementText(rest[li:], "</rdf:li"); title != "" {
			return title
		}
	}
	return xmpElementText(rest, "</dc:title")
}

func xmpElementText(seg []byte, closeTag string) string {
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 || gt > 200 {
		return ""
	}
	end := bytes.Index(bytes.ToLower(seg[gt:]), []byte(closeTag))
	if end < 0 {
		return ""
	}
	raw := seg[gt+1 : gt+end]
	if bytes.Contains(raw, []byte("<")) {
		return ""
	}
	return fileCompanionCleanTitle(html.UnescapeString(string(raw)))
}

func fileCompanionReadBytesHead(path string, limit int64) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// fileCompanionReadTextHead reads a bounded prefix as text. Every text-format
// extractor goes through it so a GBK or UTF-16 document yields a readable
// title instead of mojibake.
func fileCompanionReadTextHead(path string, limit int64) (string, bool) {
	raw, ok := fileCompanionReadBytesHead(path, limit)
	if !ok {
		return "", false
	}
	return fileCompanionDecodeText(raw), true
}

// fileCompanionDecodeText normalises a text prefix to UTF-8. A BOM decides the
// encoding outright. Without one, invalid UTF-8 is tried as GBK first because
// a Chinese document is the common case, but GBK accepts almost any byte pair,
// so it only wins when the result actually carries Han characters.
func fileCompanionDecodeText(raw []byte) string {
	if len(raw) >= 2 {
		if raw[0] == 0xFF && raw[1] == 0xFE {
			return utf16BytesToString(raw[2:], false)
		}
		if raw[0] == 0xFE && raw[1] == 0xFF {
			return utf16BytesToString(raw[2:], true)
		}
	}
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	if utf8.Valid(raw) {
		return string(raw)
	}
	// A bounded read can cut the final rune in half. That is still UTF-8, and
	// guessing a codepage from it would turn a Chinese heading into noise.
	for cut := 1; cut <= 3 && cut < len(raw); cut++ {
		if utf8.Valid(raw[:len(raw)-cut]) {
			return string(raw[:len(raw)-cut])
		}
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) && decodedTextHasHan(decoded) {
		return string(decoded)
	}
	if decoded, err := charmap.Windows1252.NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return string(raw)
}

func decodedTextHasHan(raw []byte) bool {
	for _, r := range string(raw) {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func fileCompanionHeadingTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 8192)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(text, "---") {
		rest := text[3:]
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			if title, ok := markdownFrontTitle(rest[:end]); ok {
				return title, true
			}
			// A heading between two rules is not front matter. Skip the
			// block only when it actually holds YAML keys, so the heading
			// stays visible to the scan below.
			if yamlFrontMatter(rest[:end]) {
				text = rest[end+1:]
				if nl := strings.IndexByte(text, '\n'); nl >= 0 && strings.HasPrefix(strings.TrimLeft(text[:nl], "\r"), "---") {
					text = text[nl+1:]
				}
			}
		}
	}
	return markdownBodyTitle(text), true
}

func markdownBodyTitle(text string) string {
	inFence := false
	prev := ""
	for i, line := range strings.Split(text, "\n") {
		if i > 80 {
			break
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			prev = ""
			continue
		}
		if inFence {
			continue
		}
		if trim == "" {
			prev = ""
			continue
		}
		if title, ok := markdownATX(trim); ok {
			if title != "" {
				return title
			}
			prev = ""
			continue
		}
		if markdownSetext(trim, prev) {
			return stripMarkdownHeading(prev)
		}
		prev = trim
	}
	return ""
}

func markdownATX(trim string) (string, bool) {
	if !strings.HasPrefix(trim, "#") {
		return "", false
	}
	hashes := 0
	for hashes < len(trim) && trim[hashes] == '#' {
		hashes++
	}
	if hashes < 1 || hashes > 6 || hashes >= len(trim) {
		return "", false
	}
	if trim[hashes] != ' ' && trim[hashes] != '\t' {
		return "", false
	}
	return stripMarkdownHeading(strings.TrimSpace(trim[hashes:])), true
}

func markdownSetext(trim, prev string) bool {
	if prev == "" || len(trim) < 3 {
		return false
	}
	mark := trim[0]
	if mark != '=' && mark != '-' {
		return false
	}
	for i := 0; i < len(trim); i++ {
		if trim[i] != mark {
			return false
		}
	}
	// '=' is a heading underline. A short '---' under a longer line is a
	// rule; a dash underline counts only when it is at least as long as the
	// line it sits under.
	if mark == '-' && utf8.RuneCountInString(trim) < utf8.RuneCountInString(prev) {
		return false
	}
	return true
}

func yamlFrontMatter(front string) bool {
	keys := 0
	for _, line := range strings.Split(front, "\n") {
		if yamlIndented(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			continue
		}
		if _, _, ok := markdownFrontKey(trimmed); !ok {
			return false
		}
		keys++
	}
	return keys > 0
}

func markdownFrontKey(line string) (key, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:colon])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, strings.TrimSpace(line[colon+1:]), true
}

// markdownFrontTitle reads a plain YAML title. An empty value and a block
// scalar marker are not titles by themselves: the folded lines are the
// title, and when those lines are missing the body heading is.
func markdownFrontTitle(front string) (string, bool) {
	lines := strings.Split(front, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		key, value, ok := markdownFrontKey(trimmed)
		if !ok || !strings.EqualFold(key, "title") {
			continue
		}
		if marker, marked := yamlBlockMarker(value); marked {
			value = marker
		}
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
			plain := yamlPlainValue(value)
			if strings.TrimSpace(plain) == "" {
				return "", false
			}
			return plain, true
		}
	}
	return "", false
}

func yamlBlockMarker(value string) (string, bool) {
	for _, marker := range []string{">-", ">+", "|-", "|+", ">", "|"} {
		if value == marker || strings.HasPrefix(value, marker+" ") || strings.HasPrefix(value, marker+"#") {
			return marker, true
		}
	}
	return "", false
}

func yamlPlainValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == '"' || value[0] == '\'' {
		quote := value[0]
		for i := 1; i < len(value); i++ {
			if value[i] == '\\' && quote == '"' && i+1 < len(value) {
				i++
				continue
			}
			if value[i] == quote {
				return value[1:i]
			}
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return strings.Trim(value, `"'`)
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
	value = strings.TrimSpace(value)
	if trimmed := strings.TrimRight(value, "#"); trimmed != value && trimmed != "" {
		if last := trimmed[len(trimmed)-1]; last == ' ' || last == '\t' {
			value = strings.TrimSpace(trimmed)
		}
	}
	return strings.TrimSpace(strings.Trim(value, "*_`"))
}

func fileCompanionHTMLTitle(path string) (string, bool) {
	data, ok := fileCompanionReadBytesHead(path, 32<<10)
	if !ok {
		return "", false
	}
	return htmlDocumentTitle(data), true
}

// htmlDocumentTitle keeps <title> first: it is the canonical name and the
// existing extraction depends on it. A saved page that left it empty still
// carries the share card's og:title, and a bare fragment carries only an h1.
func htmlDocumentTitle(data []byte) string {
	if title := htmlTitleElement(data); title != "" {
		return title
	}
	if title := htmlMetaTitle(data); title != "" {
		return title
	}
	return htmlFirstHeading(data)
}

func htmlTitleElement(data []byte) string {
	idx := htmlTitleOpen(data)
	if idx < 0 {
		return ""
	}
	seg := data[idx:]
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 || gt > 200 {
		return ""
	}
	end := htmlTitleClose(seg[gt:])
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(fileCompanionDecodeText(seg[gt+1 : gt+end])))
}

func htmlMetaTitle(data []byte) string {
	lower := bytes.ToLower(data)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("<meta"))
		if rel < 0 {
			return ""
		}
		at := from + rel
		next := at + len("<meta")
		if next < len(data) && !htmlTitleBoundary(data[next]) {
			from = next
			continue
		}
		end := bytes.IndexByte(data[at:], '>')
		if end < 0 {
			return ""
		}
		tag := data[at : at+end+1]
		for _, key := range []string{"property", "name"} {
			switch strings.ToLower(htmlAttr(tag, key)) {
			case "og:title", "twitter:title", "dc.title", "title":
				if content := strings.TrimSpace(htmlAttr(tag, "content")); content != "" {
					return html.UnescapeString(fileCompanionDecodeText([]byte(content)))
				}
			}
		}
		from = at + end + 1
	}
	return ""
}

func htmlFirstHeading(data []byte) string {
	lower := bytes.ToLower(data)
	rel := bytes.Index(lower, []byte("<h1"))
	if rel < 0 {
		return ""
	}
	next := rel + len("<h1")
	if next < len(data) && !htmlTitleBoundary(data[next]) {
		return ""
	}
	seg := data[rel:]
	gt := bytes.IndexByte(seg, '>')
	if gt < 0 || gt > 200 {
		return ""
	}
	end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</h1"))
	if end < 0 {
		return ""
	}
	text := htmlStripTags(seg[gt+1 : gt+end])
	return strings.TrimSpace(html.UnescapeString(fileCompanionDecodeText([]byte(text))))
}

func htmlStripTags(raw []byte) string {
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '<' {
			b.WriteByte(raw[i])
			continue
		}
		end := bytes.IndexByte(raw[i:], '>')
		if end < 0 {
			break
		}
		i += end
	}
	return b.String()
}

// htmlAttr reads one attribute out of a tag. The name has to stand alone:
// "data-title" is not "title", and the value may be quoted or bare.
func htmlAttr(tag []byte, name string) string {
	lower := bytes.ToLower(tag)
	needle := []byte(strings.ToLower(name))
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], needle)
		if rel < 0 {
			return ""
		}
		at := from + rel
		from = at + len(needle)
		if at > 0 && htmlAttrNameByte(lower[at-1]) {
			continue
		}
		i := from
		for i < len(tag) && htmlSpaceByte(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			continue
		}
		i++
		for i < len(tag) && htmlSpaceByte(tag[i]) {
			i++
		}
		if i >= len(tag) {
			return ""
		}
		if quote := tag[i]; quote == '"' || quote == '\'' {
			i++
			end := bytes.IndexByte(tag[i:], quote)
			if end < 0 {
				return ""
			}
			return string(tag[i : i+end])
		}
		end := i
		for end < len(tag) && !htmlSpaceByte(tag[end]) && tag[end] != '>' {
			end++
		}
		return string(tag[i:end])
	}
	return ""
}

func htmlAttrNameByte(b byte) bool {
	return b == '-' || b == '_' || b == ':' || b == '.' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func htmlSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func htmlTitleOpen(data []byte) int {
	lower := bytes.ToLower(data)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("<title"))
		if rel < 0 {
			return -1
		}
		at := from + rel
		next := at + len("<title")
		if next >= len(data) || htmlTitleBoundary(data[next]) {
			return at
		}
		from = next
	}
	return -1
}

func htmlTitleClose(seg []byte) int {
	lower := bytes.ToLower(seg)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("</title"))
		if rel < 0 {
			return -1
		}
		at := from + rel
		next := at + len("</title")
		if next >= len(seg) || htmlTitleBoundary(seg[next]) {
			return at
		}
		from = next
	}
	return -1
}

func htmlTitleBoundary(b byte) bool {
	return b == '>' || b == '/' || b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func fileCompanionOfficeCoreTitle(path string) (string, bool) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", false
	}
	defer reader.Close()
	var core string
	var document []byte
	var slide []byte
	var failed bool
	for _, file := range reader.File {
		name := strings.ToLower(filepath.ToSlash(file.Name))
		switch name {
		case "docprops/core.xml":
			raw, err := readZipLimited(file, 64<<10)
			if err != nil {
				failed = true
				continue
			}
			core = officeDCTitle(raw)
		case "word/document.xml":
			raw, err := readZipLimited(file, 64<<10)
			if err != nil {
				failed = true
				continue
			}
			document = raw
		case "ppt/slides/slide1.xml":
			raw, err := readZipLimited(file, 64<<10)
			if err != nil {
				failed = true
				continue
			}
			slide = raw
		}
	}
	if strings.TrimSpace(core) != "" {
		return core, true
	}
	// Word leaves dc:title empty unless the author filled File > Info.
	// The heading style, then a short first paragraph, is the title on the page.
	if title := docxDocumentTitle(document); title != "" {
		return title, true
	}
	// PowerPoint leaves it empty too. The title placeholder on the first slide
	// is the name the deck opens with.
	if title := pptxSlideTitle(slide); title != "" {
		return title, true
	}
	if failed {
		return "", false
	}
	return "", true
}

func pptxSlideTitle(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	lower := bytes.ToLower(body)
	at := -1
	for _, marker := range []string{`type="ctrtitle"`, `type="title"`} {
		if idx := bytes.Index(lower, []byte(marker)); idx >= 0 && (at < 0 || idx < at) {
			at = idx
		}
	}
	if at < 0 {
		return ""
	}
	seg := body[at:]
	if end := bytes.Index(bytes.ToLower(seg), []byte("</p:txbody>")); end >= 0 {
		seg = seg[:end]
	}
	title := pptxShapeText(seg)
	if utf8.RuneCountInString(title) > 120 {
		return ""
	}
	return title
}

func pptxShapeText(block []byte) string {
	var b strings.Builder
	lower := bytes.ToLower(block)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("<a:t"))
		if rel < 0 {
			break
		}
		at := from + rel
		next := at + len("<a:t")
		if next < len(block) && !htmlTitleBoundary(block[next]) {
			from = next
			continue
		}
		seg := block[at:]
		gt := bytes.IndexByte(seg, '>')
		if gt <= 0 {
			break
		}
		if seg[gt-1] == '/' {
			from = at + gt + 1
			continue
		}
		end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</a:t"))
		if end < 0 {
			break
		}
		b.WriteString(html.UnescapeString(string(seg[gt+1 : gt+end])))
		nextFrom := at + gt + end + len("</a:t")
		if nextFrom <= from {
			break
		}
		from = nextFrom
	}
	return fileCompanionCleanTitle(b.String())
}

func readZipLimited(file *zip.File, limit int64) ([]byte, error) {
	body, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, limit))
}

func docxDocumentTitle(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if title := docxStyledParagraph(body, "Title"); title != "" {
		return title
	}
	if title := docxStyledParagraph(body, "Heading1"); title != "" {
		return title
	}
	text := docxFirstParagraph(body)
	if utf8.RuneCountInString(text) > 120 {
		return ""
	}
	return text
}

func docxStyledParagraph(body []byte, style string) string {
	lower := bytes.ToLower(body)
	needle := []byte(`w:val="` + strings.ToLower(style) + `"`)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], needle)
		if rel < 0 {
			break
		}
		at := from + rel
		win := at - 80
		if win < 0 {
			win = 0
		}
		if !bytes.Contains(lower[win:at], []byte("w:pstyle")) {
			from = at + len(needle)
			continue
		}
		start := bytes.LastIndex(lower[:at], []byte("<w:p"))
		if start < 0 {
			from = at + len(needle)
			continue
		}
		endRel := bytes.Index(lower[start:], []byte("</w:p>"))
		if endRel < 0 {
			return ""
		}
		if text := docxRunText(body[start : start+endRel]); text != "" {
			return text
		}
		from = start + endRel + len("</w:p>")
	}
	return ""
}

func docxFirstParagraph(body []byte) string {
	lower := bytes.ToLower(body)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("<w:p"))
		if rel < 0 {
			return ""
		}
		start := from + rel
		next := start + len("<w:p")
		if next < len(body) && body[next] != '>' && body[next] != ' ' && body[next] != '/' && body[next] != '\t' && body[next] != '\n' && body[next] != '\r' {
			from = next
			continue
		}
		endRel := bytes.Index(lower[start:], []byte("</w:p>"))
		if endRel < 0 {
			return ""
		}
		if text := docxRunText(body[start : start+endRel]); text != "" {
			return text
		}
		from = start + endRel + len("</w:p>")
	}
	return ""
}

func docxRunText(block []byte) string {
	var b strings.Builder
	lower := bytes.ToLower(block)
	from := 0
	for from < len(lower) {
		rel := bytes.Index(lower[from:], []byte("<w:t"))
		if rel < 0 {
			break
		}
		at := from + rel
		next := at + len("<w:t")
		if next < len(block) && block[next] != '>' && block[next] != ' ' && block[next] != '/' && block[next] != '\t' && block[next] != '\n' && block[next] != '\r' {
			from = next
			continue
		}
		seg := block[at:]
		gt := bytes.IndexByte(seg, '>')
		if gt <= 0 {
			break
		}
		if seg[gt-1] == '/' {
			from = at + gt + 1
			continue
		}
		end := bytes.Index(bytes.ToLower(seg[gt:]), []byte("</w:t"))
		if end < 0 {
			break
		}
		b.WriteString(html.UnescapeString(string(seg[gt+1 : gt+end])))
		nextFrom := at + gt + end + len("</w:t")
		if nextFrom <= from {
			break
		}
		from = nextFrom
	}
	return fileCompanionCleanTitle(b.String())
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

// fileCompanionEPUBTitle follows container.xml to the package document. The
// OPF holds the book's dc:title; a spine entry or a chapter heading does not.
func fileCompanionEPUBTitle(path string) (string, bool) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", false
	}
	defer reader.Close()
	opfPath := ""
	var fallback *zip.File
	for _, file := range reader.File {
		name := epubEntryName(file.Name)
		switch {
		case name == "meta-inf/container.xml":
			raw, err := readZipLimited(file, 32<<10)
			if err == nil {
				opfPath = epubRootfilePath(raw)
			}
		case strings.HasSuffix(name, ".opf") && fallback == nil:
			fallback = file
		}
	}
	target := fallback
	if opfPath != "" {
		want := epubEntryName(opfPath)
		for _, file := range reader.File {
			if epubEntryName(file.Name) == want {
				target = file
				break
			}
		}
	}
	if target == nil {
		return "", true
	}
	raw, err := readZipLimited(target, 128<<10)
	if err != nil {
		return "", false
	}
	return officeDCTitle(raw), true
}

func epubEntryName(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.ToSlash(name), "/"))
}

func epubRootfilePath(data []byte) string {
	rel := bytes.Index(bytes.ToLower(data), []byte("<rootfile"))
	if rel < 0 {
		return ""
	}
	end := bytes.IndexByte(data[rel:], '>')
	if end < 0 {
		return ""
	}
	return htmlAttr(data[rel:rel+end+1], "full-path")
}

// fileCompanionXMLTitle covers the documents that carry a title element:
// DocBook and DITA use <title>, SVG uses <title>, and a feed uses it for the
// channel. Dublin Core wins where both are present.
func fileCompanionXMLTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 64<<10)
	if !ok {
		return "", false
	}
	data := []byte(text)
	if title := strings.TrimSpace(officeDCTitle(data)); title != "" {
		return title, true
	}
	return htmlTitleElement(data), true
}

// fileCompanionYAMLTitle reads a top-level title. An indented one belongs to a
// nested record, so the scan stops at the first unindented key that names it
// and hands the rest to the front-matter reader, which already folds block
// scalars and strips quotes and comments.
func fileCompanionYAMLTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 32<<10)
	if !ok {
		return "", false
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 400 {
			break
		}
		if yamlIndented(line) {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "- ") {
			continue
		}
		key, _, found := markdownFrontKey(trim)
		if found && strings.EqualFold(key, "title") {
			title, _ := markdownFrontTitle(strings.Join(lines[i:], "\n"))
			return title, true
		}
	}
	return "", true
}

// fileCompanionJSONTitle reads a top-level "title", then "name". A nested key
// belongs to a record inside the document, so the scan tracks depth rather
// than searching the buffer for the first match.
func fileCompanionJSONTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 256<<10)
	if !ok {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(text))
	depth := 0
	rootObject := false
	expectKey := false
	key := ""
	title := ""
	name := ""
	for {
		token, err := dec.Token()
		if err != nil {
			break
		}
		if delim, isDelim := token.(json.Delim); isDelim {
			switch delim {
			case '{':
				depth++
				if depth == 1 {
					rootObject = true
					expectKey = true
				}
			case '[':
				depth++
				if depth == 1 {
					rootObject = false
				}
			case '}', ']':
				depth--
				expectKey = depth == 1 && rootObject
			}
			key = ""
			if depth <= 0 {
				break
			}
			continue
		}
		if depth != 1 || !rootObject {
			continue
		}
		value, isText := token.(string)
		if expectKey {
			key = value
			expectKey = false
			continue
		}
		if isText {
			switch key {
			case "title":
				if title == "" {
					title = value
				}
			case "name":
				if name == "" {
					name = value
				}
			}
		}
		key = ""
		expectKey = true
	}
	if strings.TrimSpace(title) != "" {
		return title, true
	}
	return name, true
}

// fileCompanionEMLTitle reads the Subject header. A folded continuation line
// belongs to it, and a "Subject:" in the body is content. Encoded words carry
// their own charset, so the decoder is told how to read one.
func fileCompanionEMLTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 64<<10)
	if !ok {
		return "", false
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	headers := normalized
	if idx := strings.Index(normalized, "\n\n"); idx >= 0 {
		headers = normalized[:idx]
	}
	subject := ""
	collecting := false
	for _, line := range strings.Split(headers, "\n") {
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if collecting {
				subject += " " + strings.TrimSpace(line)
			}
			continue
		}
		collecting = false
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:colon]), "subject") {
			subject = strings.TrimSpace(line[colon+1:])
			collecting = true
		}
	}
	if subject == "" {
		return "", true
	}
	decoder := &mime.WordDecoder{
		CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
			enc, err := ianaindex.MIME.Encoding(charset)
			if err != nil {
				return nil, err
			}
			if enc == nil {
				return nil, errors.New("unsupported charset")
			}
			return enc.NewDecoder().Reader(input), nil
		},
	}
	decoded, err := decoder.DecodeHeader(subject)
	if err != nil || !utf8.ValidString(decoded) {
		return "", true
	}
	return decoded, true
}

// fileCompanionAdocTitle reads the level-0 heading. An explicit :doctitle:
// attribute overrides it, which is how an author names a document whose first
// line is a section heading.
func fileCompanionAdocTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 16<<10)
	if !ok {
		return "", false
	}
	heading := ""
	for i, line := range strings.Split(text, "\n") {
		if i > 120 {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.HasPrefix(trim, ":") {
			rest := trim[1:]
			colon := strings.IndexByte(rest, ':')
			if colon > 0 && strings.EqualFold(strings.TrimSpace(rest[:colon]), "doctitle") {
				if value := strings.TrimSpace(rest[colon+1:]); value != "" {
					return value, true
				}
			}
			continue
		}
		if heading == "" && strings.HasPrefix(trim, "= ") {
			heading = strings.TrimSpace(trim[2:])
		}
	}
	return heading, true
}

// fileCompanionPlainTextTitle takes the opening line as a name only when a
// body follows it. A single-line file is all content, and a log or table
// opening line is not a title.
func fileCompanionPlainTextTitle(path string) (string, bool) {
	text, ok := fileCompanionReadTextHead(path, 16<<10)
	if !ok {
		return "", false
	}
	first := ""
	hasBody := false
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if first == "" {
			first = trim
			continue
		}
		hasBody = true
		break
	}
	if !hasBody || !plainTextTitleLine(first) {
		return "", true
	}
	return first, true
}

func plainTextTitleLine(line string) bool {
	runes := []rune(line)
	if len(runes) == 0 || len(runes) > 120 || strings.ContainsRune(line, '\t') {
		return false
	}
	switch runes[0] {
	case '[', '{', '<', '>', '|', '#':
		return false
	}
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	separator := true
	alnum := false
	for _, r := range runes {
		if r != '-' && r != '=' && r != '*' && r != '_' && r != '~' && r != ' ' {
			separator = false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnum = true
		}
	}
	if separator || !alnum {
		return false
	}
	// 2026-11-10, 10/11/2026 and 07:23:46 open a log line, not a name.
	if unicode.IsDigit(runes[0]) {
		rest := strings.TrimLeftFunc(line, unicode.IsDigit)
		if rest != "" && (rest[0] == '-' || rest[0] == '/' || rest[0] == ':') {
			return false
		}
	}
	return true
}
