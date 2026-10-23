package agent

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// Terminal auto-extract classes are resource or safety stops. They are not
// pageable failures: office/read_document will hit the same wall, and a shell
// must not install another parser to climb over it.
var terminalExtractClasses = map[string]struct{}{
	"encrypted":        {},
	"malformed":        {},
	"source_changed":   {},
	"input_too_large":  {},
	"output_too_large": {},
}

// TerminalExtract is one host auto-extract block the model must not bypass.
type TerminalExtract struct {
	Path   string
	Format string
	Class  string
}

// TextHasAutoExtractRecord reports a live inject block or the one-line summary
// history keeps after the body is stripped.
func TextHasAutoExtractRecord(text string) bool {
	return strings.Contains(text, AutoExtractBeginMarker) || strings.Contains(text, "之前已自动解析文档")
}

// AutoExtractTerminalRecords returns blocked extracts in text. A truncated
// budget cut is not terminal. A generic parse error is not terminal.
func AutoExtractTerminalRecords(text string) []TerminalExtract {
	var out []TerminalExtract
	seen := map[string]struct{}{}
	add := func(path, format, class string) {
		if _, ok := terminalExtractClasses[class]; !ok {
			return
		}
		key := path + "\x00" + class
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, TerminalExtract{Path: path, Format: format, Class: class})
	}
	for _, region := range autoExtractRegions(text) {
		first := regionFirstLine(region)
		add(extractQuotedAttr(first, "path"), extractQuotedAttr(first, "format"), extractErrorClassToken(region))
	}
	// Later turns drop the body and keep one summary line. The class still
	// has to close the file, or 「继续」 walks straight into a shell bypass.
	if strings.Contains(text, "之前已自动解析文档") {
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "之前已自动解析文档") || !strings.Contains(trimmed, "path=") {
				continue
			}
			add(extractQuotedAttr(trimmed, "path"), extractQuotedAttr(trimmed, "format"), extractErrorClassToken(trimmed))
		}
	}
	return out
}

// TerminalExtractOffersBuiltinOCR reports an oversized PDF whose text inject
// was refused. The built-in PP-OCRv6 tool can still read page images to a
// file. Encrypted, malformed, and non-PDF limits do not.
func TerminalExtractOffersBuiltinOCR(records []TerminalExtract) bool {
	for _, rec := range records {
		if rec.Class != "input_too_large" {
			continue
		}
		if strings.EqualFold(rec.Format, "pdf") || strings.EqualFold(filepath.Ext(rec.Path), ".pdf") {
			return true
		}
	}
	return false
}

// AutoExtractRequestsBuiltinOCR reports a scan window that still needs
// ocr_recognize for a later page. A finished window and a non-PDF extract do
// not. The signal lives on the begin line and in the body, so a stripped
// history line still lists the tool.
func AutoExtractRequestsBuiltinOCR(text string) bool {
	if strings.TrimSpace(text) == "" || !strings.Contains(text, "ocr_recognize") {
		return false
	}
	if strings.Contains(text, "# scan_continue: ocr_recognize") {
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "ocr_recognize") {
			continue
		}
		if strings.Contains(trimmed, "之前已自动解析文档") && strings.Contains(strings.ToLower(trimmed), ".pdf") {
			return true
		}
		if !isAutoExtractBeginLine(trimmed) {
			continue
		}
		if !strings.EqualFold(filepath.Ext(extractQuotedAttr(trimmed, "path")), ".pdf") {
			continue
		}
		if strings.Contains(extractQuotedAttr(trimmed, "note"), "ocr_recognize") {
			return true
		}
	}
	return false
}

// substituteOCRNeedles are engines the model installs when it cannot see the
// built-in recognizer. Matching any of them on a terminal turn is a bypass.
var substituteOCRNeedles = []string{
	"rapidocr",
	"paddleocr",
	"easyocr",
	"pytesseract",
	"ocrmypdf",
	"tesseract",
}

// parserRuntimeNeedles are runtimes that read the refused file. They count
// only together with that file's path, so an unrelated python -c still runs.
var parserRuntimeNeedles = []string{
	"pythonw",
	"python",
	"pip",
	"pymupdf",
	"fitz",
	"pdftotext",
	"pdftoppm",
	"pdfinfo",
	"qpdf",
	"mutool",
	"soffice",
	"libreoffice",
	"ghostscript",
	"gswin",
	"magick",
	"pyw",
	"py",
}

// CommandBypassesTerminalDocument reports a shell or script that would parse
// a terminally refused file, or would install a substitute OCR engine.
func CommandBypassesTerminalDocument(command string, records []TerminalExtract) bool {
	if strings.TrimSpace(command) == "" || len(records) == 0 {
		return false
	}
	if commandHasNeedle(command, substituteOCRNeedles) {
		return true
	}
	// A token inside the refused path is the file, not a runtime. py must not
	// turn `dir C:\py\scan.pdf` into a parser, and notes.py is not the Windows launcher.
	if !commandHasNeedle(commandWithoutRecordPaths(command, records), parserRuntimeNeedles) {
		return false
	}
	for _, rec := range records {
		if pathInCommand(command, rec.Path) {
			return true
		}
	}
	return false
}

// ToolCallBypassesTerminalDocument is the host denial for one tool call.
// An empty return means the call may proceed. ocr_recognize is the built-in
// engine and is never denied here.
func ToolCallBypassesTerminalDocument(name, argsJSON string, records []TerminalExtract) string {
	if len(records) == 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ocr_recognize":
		return ""
	case "bash", "shell":
		cmd := jsonStringField(argsJSON, "command")
		if cmd == "" {
			cmd = argsJSON
		}
		if CommandBypassesTerminalDocument(cmd, records) {
			return TerminalDocumentDenial(records)
		}
	case "craft_tool":
		task := jsonStringField(argsJSON, "task")
		if task == "" {
			task = argsJSON
		}
		if CommandBypassesTerminalDocument(task, records) {
			return TerminalDocumentDenial(records)
		}
	case "write_file", "edit_file", "edit_lines":
		if CommandBypassesTerminalDocument(argsJSON, records) {
			return TerminalDocumentDenial(records)
		}
	case "read_document":
		path := terminalArgPath(argsJSON)
		if path == "" || terminalPathMatch(path, records) {
			return TerminalDocumentDenial(records)
		}
	case "office":
		path := terminalArgPath(argsJSON)
		// A read with no path reopens the refused file. generate_pdf and
		// write_excel do not: the text version is produced after OCR.
		if terminalPathMatch(path, records) || (path == "" && officeActionRereadsDocument(jsonStringField(argsJSON, "action"))) {
			return TerminalDocumentDenial(records)
		}
	case "read_file":
		if terminalPathMatch(jsonStringField(argsJSON, "path"), records) {
			return TerminalDocumentDenial(records)
		}
	}
	return ""
}

// TerminalDocumentDenial is the model-facing stop. It names the built-in OCR
// tool only when the refused file is an oversized PDF.
func TerminalDocumentDenial(records []TerminalExtract) string {
	rec := records[0]
	for _, item := range records {
		if item.Class == "input_too_large" && (strings.EqualFold(item.Format, "pdf") || strings.EqualFold(filepath.Ext(item.Path), ".pdf")) {
			rec = item
			break
		}
	}
	msg := "该文件已由宿主终止解析（error_class=" + rec.Class + "）。禁止用 bash、pip、Python 或其他 OCR/解析器处理同一文件。"
	if TerminalExtractOffersBuiltinOCR(records) {
		return msg + " 扫描件请调用 ocr_recognize（本机 PP-OCRv6）：image_path 传该 PDF，output_path 传结果文本，start_page 从 1 起，按返回的下一页继续。不要安装 rapidocr、paddleocr、tesseract 或 ocrmypdf。"
	}
	return msg + " 请按该 error_class 的说明处理，不要改走命令行。"
}

func autoExtractRegions(text string) []string {
	if text == "" || !strings.Contains(text, AutoExtractBeginMarker) {
		return nil
	}
	lines := strings.Split(text, "\n")
	var regions []string
	var cur strings.Builder
	in := false
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		regions = append(regions, cur.String())
		cur.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isAutoExtractBeginLine(trimmed) {
			if in {
				flush()
			}
			in = true
			cur.WriteString(trimmed)
			cur.WriteByte('\n')
			continue
		}
		if !in {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasPrefix(trimmed, AutoExtractEndMarker) {
			flush()
			in = false
		}
	}
	if in {
		flush()
	}
	return regions
}

func regionFirstLine(region string) string {
	if i := strings.IndexByte(region, '\n'); i >= 0 {
		return region[:i]
	}
	return region
}

func extractErrorClassToken(text string) string {
	const key = "error_class="
	i := strings.Index(text, key)
	if i < 0 {
		return ""
	}
	rest := text[i+len(key):]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= 'a' && c <= 'z') || c == '_' {
			end++
			continue
		}
		break
	}
	return rest[:end]
}

func commandHasNeedle(command string, needles []string) bool {
	folded := strings.ToLower(command)
	for _, needle := range needles {
		if tokenHasNeedle(folded, needle) {
			return true
		}
	}
	return false
}

// tokenHasNeedle matches a command token, not a substring. pip matches pip3.
// It does not match pipeline. py matches the Windows launcher and py.exe, not
// pytest and not a notes.py suffix. A dot before the needle is an extension.
func tokenHasNeedle(command, needle string) bool {
	if needle == "" || len(needle) > len(command) {
		return false
	}
	for i := 0; i+len(needle) <= len(command); i++ {
		if command[i:i+len(needle)] != needle {
			continue
		}
		if i > 0 && (isNeedleLetter(command[i-1]) || command[i-1] == '.') {
			continue
		}
		end := i + len(needle)
		if end < len(command) && isNeedleLetter(command[end]) {
			continue
		}
		return true
	}
	return false
}

// commandWithoutRecordPaths removes each refused path so a directory named py
// or qpdf is not mistaken for the runtime that opens the file.
func commandWithoutRecordPaths(command string, records []TerminalExtract) string {
	normalized := strings.ReplaceAll(strings.ToLower(command), `\`, `/`)
	seen := map[string]struct{}{}
	var paths []string
	for _, rec := range records {
		path := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rec.Path), `\`, `/`))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		normalized = strings.ReplaceAll(normalized, path, " ")
	}
	return normalized
}

func isNeedleLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || c == '_'
}

func terminalArgPath(argsJSON string) string {
	path := jsonStringField(argsJSON, "file_path")
	if path == "" {
		path = jsonStringField(argsJSON, "path")
	}
	return path
}

func officeActionRereadsDocument(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "read", "read_document", "read_doc", "read_docx", "read_pdf", "read_word", "read_excel", "read_pptx", "preview_pptx":
		return true
	default:
		return false
	}
}

func pathInCommand(command, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	folded := strings.ToLower(strings.ReplaceAll(command, `\`, `/`))
	want := strings.ToLower(strings.ReplaceAll(path, `\`, `/`))
	return strings.Contains(folded, want)
}

func terminalPathMatch(path string, records []TerminalExtract) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	got := strings.ToLower(strings.ReplaceAll(path, `\`, `/`))
	for _, rec := range records {
		want := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rec.Path), `\`, `/`))
		if want != "" && got == want {
			return true
		}
	}
	return false
}

func jsonStringField(raw, key string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return ""
	}
	value, ok := fields[key].(string)
	if !ok {
		return ""
	}
	return value
}
