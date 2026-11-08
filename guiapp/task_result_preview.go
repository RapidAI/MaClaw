package guiapp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// TaskResultPreview is the in-app preview payload for a generated assistant
// document (PPTX slide deck, PDF bytes, or extracted/plain text).
type TaskResultPreview struct {
	Path       string `json:"path"`
	FileName   string `json:"file_name"`
	Kind       string `json:"kind"`
	Language   string `json:"language,omitempty"`
	Content    string `json:"content,omitempty"`
	DataURL    string `json:"data_url,omitempty"`
	PreviewURL string `json:"preview_url,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

const (
	taskResultPreviewHTTPPath     = "/maclaw-preview/v1/file"
	taskResultPreviewTokenTTL     = 10 * time.Minute
	taskResultPreviewMaxTokens    = 32
	taskResultPreviewPDFPeekBytes = 1024
)

type taskResultPreviewLease struct {
	path    string
	expires time.Time
}

var (
	taskResultPreviewMu     sync.Mutex
	taskResultPreviewLeases = map[string]taskResultPreviewLease{}
)

// PreviewTaskResultFile loads a generated task-result file for the assistant
// preview pane. PPTX is opened by the existing slide renderer; PDFs are served
// through a short-lived AssetServer URL; everything else is bounded text.
func (a *App) PreviewTaskResultFile(filePath string) (*TaskResultPreview, error) {
	cleaned, err := a.normalizeOpenPathForApp(filePath)
	if err != nil {
		return nil, fmt.Errorf("文件路径无效")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, fmt.Errorf("文件路径为空")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在")
		}
		return nil, fmt.Errorf("无法打开文件")
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不是文件")
	}
	if info.Size() > taskResultPreviewMaxBytes(cleaned) {
		return nil, fmt.Errorf("文件过大，无法预览")
	}

	name := info.Name()
	out := &TaskResultPreview{Path: cleaned, FileName: name}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pptx":
		out.Kind = "pptx"
		out.Language = "pptx"
		return out, nil
	case ".docx":
		return previewTaskResultBinary(cleaned, "docx", out)
	}

	if kind, _ := previewHTTPContentType(ext); kind != "" {
		if kind == "pdf" {
			return previewTaskResultPDF(cleaned, info.Size(), out)
		}
		if kind == "xlsx" {
			return previewTaskResultSpreadsheet(cleaned, out)
		}
		return previewTaskResultBinary(cleaned, kind, out)
	}

	if isTaskResultOfficeExt(ext) {
		text, _, extractErr := agent.ExtractOfficeText(cleaned)
		if extractErr != nil {
			return nil, fmt.Errorf("无法预览该文档")
		}
		out.Kind = "text"
		out.Language = "markdown"
		out.Content = text
		return out, nil
	}
	if ext == ".rtf" {
		return previewTaskResultRTF(cleaned, out)
	}

	content, truncated, readErr := readCodingWorkbenchBrowserTextFile(cleaned)
	if readErr != nil {
		return nil, fmt.Errorf("无法预览该文件")
	}
	out.Kind = "text"
	out.Language = detectLanguageFromExt(name)
	out.Content = content
	out.Truncated = truncated
	return out, nil
}

func previewTaskResultPDF(path string, size int64, out *TaskResultPreview) (*TaskResultPreview, error) {
	if size <= 0 {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件")
	}
	ok, peekErr := peekPDFHeader(file)
	closeErr := file.Close()
	if peekErr != nil {
		return nil, fmt.Errorf("无法读取文件")
	}
	if closeErr != nil {
		return nil, fmt.Errorf("无法读取文件")
	}
	if !ok {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	token := issueTaskResultPreviewToken(path)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	out.Kind = "pdf"
	out.Language = "pdf"
	out.PreviewURL = taskResultPreviewHTTPPath + "?t=" + token
	return out, nil
}

func issueTaskResultPreviewToken(path string) string {
	return issueTaskResultPreviewLease(path, taskResultPreviewTokenTTL)
}

// issueTaskResultPreviewLease mints or refreshes the short-lived file URL.
// A longer ttl, used by chat attachment previews, is kept: a later request
// never shortens a lease that still has more time left.
func issueTaskResultPreviewLease(path string, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = taskResultPreviewTokenTTL
	}
	now := time.Now()
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	for id, lease := range taskResultPreviewLeases {
		if lease.path == path && now.Before(lease.expires) {
			if until := now.Add(ttl); until.After(lease.expires) {
				lease.expires = until
				taskResultPreviewLeases[id] = lease
			}
			return id
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(raw[:])
	for id, lease := range taskResultPreviewLeases {
		if now.After(lease.expires) {
			delete(taskResultPreviewLeases, id)
		}
	}
	for len(taskResultPreviewLeases) >= taskResultPreviewMaxTokens {
		var oldest string
		var oldestExp time.Time
		for id, lease := range taskResultPreviewLeases {
			if oldest == "" || lease.expires.Before(oldestExp) {
				oldest = id
				oldestExp = lease.expires
			}
		}
		if oldest == "" {
			break
		}
		delete(taskResultPreviewLeases, oldest)
	}
	taskResultPreviewLeases[token] = taskResultPreviewLease{path: path, expires: now.Add(ttl)}
	return token
}

func lookupTaskResultPreviewPath(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if len(token) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", false
	}
	now := time.Now()
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	lease, ok := taskResultPreviewLeases[token]
	if !ok || now.After(lease.expires) {
		if ok {
			delete(taskResultPreviewLeases, token)
		}
		return "", false
	}
	if until := now.Add(taskResultPreviewTokenTTL); until.After(lease.expires) {
		lease.expires = until
		taskResultPreviewLeases[token] = lease
	}
	return lease.path, true
}

// Video, audio, and PDF are served with HTTP Range, so a file past the
// text-extraction ceiling still opens in the viewer. Word, slides, and
// workbooks are read whole by their page renderers and stop at a higher
// document cap. Images and legacy Office text extracts stay on the
// extraction ceiling.
const (
	taskResultPreviewMediaMaxBytes    int64 = 8 << 30
	taskResultPreviewDocumentMaxBytes int64 = 512 << 20
)

func taskResultPreviewMaxBytes(path string) int64 {
	ext := strings.ToLower(filepath.Ext(path))
	kind, _ := previewHTTPContentType(ext)
	switch kind {
	case "video", "audio", "pdf":
		return taskResultPreviewMediaMaxBytes
	case "docx", "xlsx":
		return taskResultPreviewDocumentMaxBytes
	}
	if ext == ".pptx" {
		return taskResultPreviewDocumentMaxBytes
	}
	return agent.MaxOfficeReadFileBytes
}

func previewHTTPContentType(ext string) (kind, contentType string) {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "pdf", "application/pdf"
	case ".docx":
		return "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xlsm":
		return "xlsx", "application/vnd.ms-excel.sheet.macroEnabled.12"
	case ".xlsb":
		return "xlsx", "application/vnd.ms-excel.sheet.binary.macroEnabled.12"
	case ".xls":
		return "xlsx", "application/vnd.ms-excel"
	case ".et", ".ett":
		return "xlsx", "application/vnd.ms-excel"
	case ".ods":
		return "xlsx", "application/vnd.oasis.opendocument.spreadsheet"
	case ".png":
		return "image", "image/png"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".gif":
		return "image", "image/gif"
	case ".webp":
		return "image", "image/webp"
	case ".bmp":
		return "image", "image/bmp"
	case ".svg":
		return "image", "image/svg+xml"
	case ".ico":
		return "image", "image/x-icon"
	case ".tif", ".tiff":
		return "image", "image/tiff"
	case ".heic":
		return "image", "image/heic"
	case ".mp4", ".m4v":
		return "video", "video/mp4"
	case ".webm":
		return "video", "video/webm"
	case ".mov":
		return "video", "video/quicktime"
	case ".avi":
		return "video", "video/x-msvideo"
	case ".mkv":
		return "video", "video/x-matroska"
	case ".mp3":
		return "audio", "audio/mpeg"
	case ".wav":
		return "audio", "audio/wav"
	case ".ogg", ".oga":
		return "audio", "audio/ogg"
	case ".m4a":
		return "audio", "audio/mp4"
	case ".aac":
		return "audio", "audio/aac"
	case ".flac":
		return "audio", "audio/flac"
	default:
		return "", ""
	}
}

func previewTaskResultBinary(path, kind string, out *TaskResultPreview) (*TaskResultPreview, error) {
	token := issueTaskResultPreviewToken(path)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	out.Kind = kind
	out.Language = kind
	out.PreviewURL = taskResultPreviewHTTPPath + "?t=" + token
	return out, nil
}

// previewTaskResultSpreadsheet serves the original workbook bytes for the
// in-app grid renderer and keeps the text extract as the fallback content, so
// a workbook the JS parser cannot read still previews as text. A file past
// the extract ceiling is not opened by the office reader; the grid still
// receives the original bytes.
func previewTaskResultSpreadsheet(path string, out *TaskResultPreview) (*TaskResultPreview, error) {
	if info, statErr := os.Stat(path); statErr != nil || info.Size() <= agent.MaxOfficeReadFileBytes {
		if text, _, extractErr := agent.ExtractOfficeText(path); extractErr == nil && strings.TrimSpace(text) != "" {
			out.Content = text
		}
	}
	return previewTaskResultBinary(path, "xlsx", out)
}

func handleTaskResultPreviewHTTP(_ *App, rw http.ResponseWriter, req *http.Request) {
	if req == nil || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
		if rw != nil {
			rw.Header().Set("Allow", "GET, HEAD")
		}
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, ok := lookupTaskResultPreviewPath(req.URL.Query().Get("t"))
	if !ok {
		http.NotFound(rw, req)
		return
	}
	kind, contentType := previewHTTPContentType(filepath.Ext(path))
	if contentType == "" {
		http.NotFound(rw, req)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(rw, req)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > taskResultPreviewMaxBytes(path) {
		http.NotFound(rw, req)
		return
	}
	if kind == "pdf" {
		okPDF, peekErr := peekPDFHeader(file)
		if peekErr != nil || !okPDF {
			http.NotFound(rw, req)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.NotFound(rw, req)
			return
		}
	}
	rw.Header().Set("Content-Type", contentType)
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Disposition", "inline")
	http.ServeContent(rw, req, filepath.Base(path), info.ModTime(), file)
}

func peekPDFHeader(file *os.File) (bool, error) {
	if file == nil {
		return false, io.ErrUnexpectedEOF
	}
	header := make([]byte, taskResultPreviewPDFPeekBytes)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}
	return looksLikePDF(header[:n]), nil
}

func looksLikePDF(prefix []byte) bool {
	return bytes.Contains(prefix, []byte("%PDF"))
}

// previewTaskResultRTF shows the words in an RTF file. The text reader keeps
// the control words, and the office pane then renders those words as the
// document. Bytes that are not UTF-8 still parse: Word stores characters as
// \u escapes, which are ASCII.
func previewTaskResultRTF(path string, out *TaskResultPreview) (*TaskResultPreview, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法预览该文件")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(codingWorkbenchBrowserMaxReadBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("无法预览该文件")
	}
	truncated := len(raw) > codingWorkbenchBrowserMaxReadBytes
	if truncated {
		raw = raw[:codingWorkbenchBrowserMaxReadBytes]
	}
	text := strings.TrimSpace(rtfPreviewPlainText(string(raw)))
	if text == "" {
		return nil, fmt.Errorf("无法预览该文件")
	}
	if utf8.RuneCountInString(text) > codingWorkbenchBrowserMaxRunes {
		text = string([]rune(text)[:codingWorkbenchBrowserMaxRunes])
		truncated = true
	}
	out.Kind = "text"
	out.Language = "plaintext"
	out.Content = text
	out.Truncated = truncated
	return out, nil
}

// rtfPreviewPlainText returns the visible text of an RTF document. A file
// that is not RTF is returned unchanged. An empty extract keeps the raw
// text so a preview is not blanked.
func rtfPreviewPlainText(raw string) string {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	if !strings.HasPrefix(strings.TrimSpace(raw), "{\\rtf") {
		return raw
	}
	text := strings.TrimSpace(extractRTFVisibleText(raw))
	if text == "" {
		return raw
	}
	return text
}

// rtfReader walks one RTF document. cp is the \ansicpg value: Word writes
// Chinese as GBK bytes (936) inside \'hh escapes, not as Latin-1.
// \uc and the code page are group-scoped. A group that changes them must
// not leave the fallback-byte count changed for the rest of the file, or
// every later \u character keeps its '?' substitute.
type rtfReader struct {
	s       string
	b       strings.Builder
	pending []byte
	stack   []rtfGroupState
	cp      int
	ucSkip  int
	depth   int
	skipAt  int
	high    rune
}

type rtfGroupState struct {
	cp     int
	ucSkip int
}

func extractRTFVisibleText(s string) string {
	r := &rtfReader{s: strings.TrimPrefix(s, "\uFEFF"), cp: 1252, ucSkip: 1, skipAt: -1}
	r.walk()
	r.flush()
	r.flushHigh()
	return r.b.String()
}

func (r *rtfReader) skipping() bool {
	return r.skipAt >= 0
}

func (r *rtfReader) walk() {
	for i := 0; i < len(r.s); {
		switch r.s[i] {
		case '{':
			r.flush()
			r.stack = append(r.stack, rtfGroupState{cp: r.cp, ucSkip: r.ucSkip})
			r.depth++
			if r.skipAt < 0 && rtfGroupSkipped(r.s, i) {
				r.skipAt = r.depth
			}
			i++
		case '}':
			r.flush()
			if r.skipAt == r.depth {
				r.skipAt = -1
			}
			if n := len(r.stack); n > 0 {
				top := r.stack[n-1]
				r.stack = r.stack[:n-1]
				r.cp = top.cp
				r.ucSkip = top.ucSkip
			}
			if r.depth > 0 {
				r.depth--
			}
			i++
		case '\\':
			i = r.consumeControl(i)
		case '\r', '\n':
			i++
		default:
			if r.skipping() {
				i++
				continue
			}
			ch, size := utf8.DecodeRuneInString(r.s[i:])
			if ch == utf8.RuneError && size == 1 {
				r.pending = append(r.pending, r.s[i])
				i++
				continue
			}
			r.flush()
			r.flushHigh()
			r.b.WriteRune(ch)
			i += size
		}
	}
}

func (r *rtfReader) writeEscapedByte(c byte) {
	if r.skipping() {
		return
	}
	r.pending = append(r.pending, c)
	if !rtfCodePageMultibyte(r.cp) {
		r.flush()
	}
}

func (r *rtfReader) flush() {
	if len(r.pending) == 0 {
		return
	}
	buf := r.pending
	r.pending = nil
	if r.cp == 65001 {
		r.b.WriteString(strings.ToValidUTF8(string(buf), ""))
		return
	}
	decoded, _ := rtfCodePageEncoding(r.cp).NewDecoder().Bytes(buf)
	if len(decoded) == 0 {
		return
	}
	r.b.WriteString(strings.ToValidUTF8(string(decoded), ""))
}

func rtfGroupSkipped(s string, brace int) bool {
	if brace+1 >= len(s) || s[brace+1] != '\\' {
		return false
	}
	word, _, _, _ := readRTFControl(s, brace+1)
	switch strings.ToLower(word) {
	case "*", "fonttbl", "colortbl", "stylesheet", "info", "pict", "object",
		"datastore", "themedata", "listtable", "listoverridetable", "revtbl",
		"rsidtbl", "generator", "xmlnstbl":
		return true
	default:
		return false
	}
}

func (r *rtfReader) consumeControl(i int) int {
	word, arg, hasArg, next := readRTFControl(r.s, i)
	if next <= i {
		return i + 1
	}
	switch word {
	case "\\", "{", "}":
		if !r.skipping() {
			r.flush()
			r.flushHigh()
			r.b.WriteByte(word[0])
		}
	case "~":
		if !r.skipping() {
			r.flush()
			r.flushHigh()
			r.b.WriteByte(' ')
		}
	case "'":
		if hasArg {
			r.writeEscapedByte(byte(arg))
		}
	case "par", "line", "page", "row":
		if !r.skipping() {
			r.flush()
			r.flushHigh()
			r.b.WriteByte('\n')
		}
	case "tab", "cell":
		if !r.skipping() {
			r.flush()
			r.flushHigh()
			r.b.WriteByte('\t')
		}
	case "uc":
		if hasArg && arg >= 0 && arg <= 16 {
			r.ucSkip = arg
		}
	case "ansicpg":
		if !r.skipping() && hasArg && arg > 0 {
			r.flush()
			r.cp = arg
		}
	case "bin":
		r.flush()
		if hasArg && arg > 0 {
			end := next + arg
			if end < next || end > len(r.s) {
				end = len(r.s)
			}
			return end
		}
	case "u":
		if !r.skipping() && hasArg {
			r.flush()
			r.writeUnicode(arg)
		}
		return skipRTFFallback(r.s, next, r.ucSkip)
	}
	if text, ok := rtfNamedChar(word); ok && !r.skipping() {
		r.flush()
		r.flushHigh()
		r.b.WriteString(text)
	}
	return next
}

// rtfNamedChar is punctuation Word writes as a control word. The space after
// the word is only a delimiter, so dropping the word glues the two sides together.
func rtfNamedChar(word string) (string, bool) {
	switch strings.ToLower(word) {
	case "emdash":
		return "\u2014", true
	case "endash":
		return "\u2013", true
	case "lquote":
		return "\u2018", true
	case "rquote":
		return "\u2019", true
	case "ldblquote":
		return "\u201c", true
	case "rdblquote":
		return "\u201d", true
	case "bullet":
		return "\u2022", true
	case "_":
		return "-", true
	default:
		return "", false
	}
}

func (r *rtfReader) writeUnicode(arg int) {
	if arg < 0 {
		arg += 65536
	}
	ru := rune(arg)
	if ru >= 0xD800 && ru <= 0xDBFF {
		r.flushHigh()
		r.high = ru
		return
	}
	if r.high != 0 && ru >= 0xDC00 && ru <= 0xDFFF {
		r.b.WriteRune(utf16.DecodeRune(r.high, ru))
		r.high = 0
		return
	}
	r.flushHigh()
	if ru > utf8.MaxRune {
		r.b.WriteRune('\uFFFD')
		return
	}
	r.b.WriteRune(ru)
}

func (r *rtfReader) flushHigh() {
	if r.high == 0 {
		return
	}
	r.b.WriteRune('\uFFFD')
	r.high = 0
}

func rtfCodePageMultibyte(cp int) bool {
	switch cp {
	case 936, 949, 950, 932, 54936, 65001:
		return true
	default:
		return false
	}
}

func rtfCodePageEncoding(cp int) encoding.Encoding {
	switch cp {
	case 936:
		return simplifiedchinese.GBK
	case 54936:
		return simplifiedchinese.GB18030
	case 950:
		return traditionalchinese.Big5
	case 932:
		return japanese.ShiftJIS
	case 949:
		return korean.EUCKR
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	default:
		return charmap.Windows1252
	}
}

func readRTFControl(s string, i int) (word string, arg int, hasArg bool, next int) {
	if i >= len(s) || s[i] != '\\' {
		return "", 0, false, i
	}
	i++
	if i >= len(s) {
		return "", 0, false, i
	}
	switch s[i] {
	case '\\', '{', '}', '~', '_', '-':
		return string(s[i]), 0, false, i + 1
	case '*':
		return "*", 0, false, i + 1
	case '\'':
		if i+2 >= len(s) {
			return "'", 0, false, len(s)
		}
		hi, ok1 := rtfHex(s[i+1])
		lo, ok2 := rtfHex(s[i+2])
		if !ok1 || !ok2 {
			return "'", 0, false, i + 1
		}
		return "'", int(hi<<4 | lo), true, i + 3
	}
	start := i
	for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
		i++
	}
	word = s[start:i]
	if i < len(s) && (s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
		hasArg = true
		sign := 1
		if s[i] == '-' {
			sign = -1
			i++
		}
		n := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			digit := int(s[i] - '0')
			if n > (1<<27) || n*10+digit < n {
				i++
				continue
			}
			n = n*10 + digit
			i++
		}
		arg = sign * n
	}
	if i < len(s) && s[i] == ' ' {
		i++
	}
	return word, arg, hasArg, i
}

func rtfHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func skipRTFFallback(s string, i, n int) int {
	for n > 0 && i < len(s) {
		n--
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '\'' {
			_, _, _, next := readRTFControl(s, i)
			if next > i {
				i = next
				continue
			}
		}
		i++
	}
	return i
}

// isTaskResultOfficeExt selects the legacy text extract. .rtf is text, and
// ExtractOfficeText has no rtf parser, so previewTaskResultRTF reads it.
func isTaskResultOfficeExt(ext string) bool {
	switch ext {
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".odt", ".ods", ".odp":
		return true
	default:
		return false
	}
}

const taskResultExportMaxFileBytes int64 = 512 << 20

var taskResultSaveDialog = func(a *App, title, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(defaultFilename))
	filters := []runtime.FileFilter{{DisplayName: "All Files (*.*)", Pattern: "*.*"}}
	if ext != "" {
		filters = append([]runtime.FileFilter{{
			DisplayName: strings.ToUpper(strings.TrimPrefix(ext, ".")) + " (*" + ext + ")",
			Pattern:     "*" + ext,
		}}, filters...)
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
		Filters:         filters,
	})
}

// ExportTaskResultFile copies a generated assistant document to a path the user
// picks in the save dialog. Empty dest means the user cancelled.
func (a *App) ExportTaskResultFile(filePath string) (string, error) {
	cleaned, err := a.normalizeOpenPathForApp(filePath)
	if err != nil {
		return "", fmt.Errorf("文件路径无效")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", fmt.Errorf("文件路径为空")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("文件不存在")
		}
		return "", fmt.Errorf("无法读取文件")
	}
	if info.IsDir() {
		return "", fmt.Errorf("不是文件")
	}
	if info.Size() > taskResultExportMaxFileBytes {
		return "", fmt.Errorf("文件过大，无法导出")
	}

	name := info.Name()
	dest, _ := taskResultSaveDialog(a, "导出文档 / Export document", name)
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	ext := filepath.Ext(name)
	if filepath.Ext(dest) == "" && ext != "" {
		dest += ext
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if sameTaskResultExportPath(cleaned, dest) {
		return dest, nil
	}
	if destInfo, destErr := os.Stat(dest); destErr == nil && destInfo.IsDir() {
		return "", fmt.Errorf("无法保存文件")
	}
	if err := copyTaskResultExportFile(cleaned, dest, taskResultExportMaxFileBytes); err != nil {
		return "", err
	}
	_ = os.Chmod(dest, 0o644)
	return dest, nil
}

func sameTaskResultExportPath(src, dest string) bool {
	src = filepath.Clean(src)
	dest = filepath.Clean(dest)
	if src == dest {
		return true
	}
	si, err1 := os.Stat(src)
	if err1 != nil {
		return false
	}
	di, err2 := os.Stat(dest)
	if err2 != nil {
		return false
	}
	return os.SameFile(si, di)
}

func copyTaskResultExportFile(src, dest string, maxBytes int64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("无法读取文件")
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".maclaw-export-*")
	if err != nil {
		return fmt.Errorf("无法保存文件")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	n, err := io.Copy(tmp, io.LimitReader(in, maxBytes+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("无法保存文件")
	}
	if n > maxBytes {
		return fmt.Errorf("文件过大，无法导出")
	}
	if err := replaceTaskResultExportFile(tmpName, dest); err != nil {
		return fmt.Errorf("无法保存文件")
	}
	return nil
}

func replaceTaskResultExportFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	in, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("无法保存文件")
	}
	_ = os.Remove(tmp)
	return nil
}

func resetTaskResultPreviewLeasesForTest() {
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	taskResultPreviewLeases = map[string]taskResultPreviewLease{}
}
