package agent

// tools_office_read.go implements native text extraction for Word/Excel/PPT/PDF
// (both modern OOXML and legacy binary formats) so agent tools do not depend
// on Python, COM, or LibreOffice for ordinary reads.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	legacydoc "github.com/shakinm/xlsReader/doc"
	"github.com/shakinm/xlsReader/xls"
)

// Default max runes returned by read_document when the caller does not set max_chars.
// Large bid packages / specs can exceed this; callers can page with offset + max_chars.
const defaultOfficeReadMaxRunes = 120_000

// Hard upper bound so a single tool result cannot blow up the model context.
const maxOfficeReadMaxRunes = 500_000

// extractCache avoids re-parsing the same file on every offset page.
// Keyed by absolute path + mtime + size; entries expire after a short TTL.
type officeExtractCacheEntry struct {
	modTime time.Time
	size    int64
	format  string
	text    string
	loaded  time.Time
}

var (
	officeExtractCache   = map[string]officeExtractCacheEntry{}
	officeExtractCacheMu sync.Mutex
)

const officeExtractCacheTTL = 2 * time.Minute
const officeExtractCacheMaxEntries = 16

// ToolReadDocument extracts text from office/PDF files using native parsers.
//
// Supported extensions:
//   - Word: .docx, .doc
//   - Excel: .xlsx, .xls, .csv
//   - PowerPoint: .pptx (.ppt is not supported natively)
//   - PDF: .pdf
//   - Text: .txt, .md, .markdown
//
// Args:
//   - file_path | path: required file path
//   - max_chars: optional rune limit for this chunk (default 120000)
//   - offset: optional rune offset into the full extract (default 0)
//   - line_numbers: optional bool; when true, prefix each line with L1:/L2: markers
func ToolReadDocument(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	// Prefer absolute paths from the host (GUI already resolves against the
	// session workdir). ResolvePath still handles relative paths for TUI/core.
	filePath = resolveOfficeToolPath(filePath)

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %v", err)
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，请指定具体文件路径", filePath)
	}

	text, format, err := extractOfficeTextCached(filePath, info)
	if err != nil {
		return formatOfficeReadFailure(filePath, format, err)
	}
	if strings.TrimSpace(text) == "" {
		return formatOfficeReadFailure(filePath, format, fmt.Errorf("文件中没有可读取的文本内容"))
	}

	maxRunes := intArg(args, "max_chars", defaultOfficeReadMaxRunes)
	if maxRunes <= 0 {
		maxRunes = defaultOfficeReadMaxRunes
	}
	if maxRunes > maxOfficeReadMaxRunes {
		maxRunes = maxOfficeReadMaxRunes
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	withLineNumbers := boolArg(args, "line_numbers", false)

	fullRunes := []rune(text)
	totalChars := len(fullRunes)
	if totalChars == 0 {
		return formatOfficeReadFailure(filePath, format, fmt.Errorf("文件中没有可读取的文本内容"))
	}
	if offset >= totalChars {
		return fmt.Sprintf("已到文档末尾（offset=%d, total_chars=%d）。没有更多内容。\n# path: %s\n# format: %s\n# truncated: false\n",
			offset, totalChars, filePath, format)
	}
	chunk := fullRunes[offset:]
	truncated := false
	nextOffset := -1
	if len(chunk) > maxRunes {
		chunk = chunk[:maxRunes]
		truncated = true
		nextOffset = offset + maxRunes
	}
	// Line numbers are absolute within the full extract so paging stays consistent.
	startLine := 1
	if withLineNumbers && offset > 0 {
		startLine = 1 + strings.Count(string(fullRunes[:offset]), "\n")
	}
	outBody := string(chunk)
	if withLineNumbers {
		outBody = prefixLineNumbers(outBody, startLine)
	}

	var b strings.Builder
	b.Grow(len(outBody) + 256)
	fmt.Fprintf(&b, "# format: %s\n# path: %s\n# total_chars: %d\n# offset: %d\n# chars: %d\n",
		format, filePath, totalChars, offset, len(chunk))
	if withLineNumbers {
		fmt.Fprintf(&b, "# line_start: %d\n", startLine)
	}
	if truncated {
		fmt.Fprintf(&b, "# truncated: true\n# next_offset: %d\n", nextOffset)
		if withLineNumbers {
			fmt.Fprintf(&b, "# continue: office(action=\"read_document\", file_path=%q, offset=%d, max_chars=%d, line_numbers=true)\n",
				filePath, nextOffset, maxRunes)
		} else {
			fmt.Fprintf(&b, "# continue: office(action=\"read_document\", file_path=%q, offset=%d, max_chars=%d)\n",
				filePath, nextOffset, maxRunes)
		}
		b.WriteString("# note: 当前仅为文档片段。不要根据片段推断后续章节标题/页码；请用 next_offset 继续读取。\n")
	} else {
		b.WriteString("# truncated: false\n")
	}
	b.WriteByte('\n')
	b.WriteString(outBody)
	return b.String()
}

// extractOfficeTextCached returns ExtractOfficeText results with a short-lived
// in-process cache so offset paging does not re-parse multi-MB PDFs each time.
// info may be nil; when non-nil, avoids a second Stat.
func extractOfficeTextCached(filePath string, info os.FileInfo) (string, string, error) {
	var err error
	if info == nil {
		info, err = os.Stat(filePath)
		if err != nil {
			return "", "", err
		}
	}
	key := filepath.Clean(filePath)
	mod, size := info.ModTime(), info.Size()

	officeExtractCacheMu.Lock()
	if ent, ok := officeExtractCache[key]; ok {
		if ent.modTime.Equal(mod) && ent.size == size && time.Since(ent.loaded) < officeExtractCacheTTL {
			text, format := ent.text, ent.format
			officeExtractCacheMu.Unlock()
			return text, format, nil
		}
		delete(officeExtractCache, key)
	}
	officeExtractCacheMu.Unlock()

	text, format, err := ExtractOfficeText(filePath)
	if err != nil {
		return text, format, err
	}
	text = strings.TrimSpace(text)

	officeExtractCacheMu.Lock()
	// Bound memory: drop arbitrary entries when full (soft cap for a small map).
	if len(officeExtractCache) >= officeExtractCacheMaxEntries {
		for k := range officeExtractCache {
			delete(officeExtractCache, k)
			if len(officeExtractCache) < officeExtractCacheMaxEntries {
				break
			}
		}
	}
	officeExtractCache[key] = officeExtractCacheEntry{
		modTime: mod,
		size:    size,
		format:  format,
		text:    text,
		loaded:  time.Now(),
	}
	officeExtractCacheMu.Unlock()
	return text, format, nil
}

func prefixLineNumbers(text string, start int) string {
	if text == "" {
		return text
	}
	if start < 1 {
		start = 1
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.Grow(len(text) + len(lines)*10)
	n := start
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		// Use plain decimal so documents with >9999 lines stay readable.
		fmt.Fprintf(&b, "L%d: %s", n, line)
		n++
	}
	return b.String()
}

// ToolReadDoc reads a legacy Word 97-2003 .doc file.
func ToolReadDoc(args map[string]interface{}) string {
	return toolReadForced(args, ".doc")
}

// ToolReadDocx reads a modern Word .docx file.
func ToolReadDocx(args map[string]interface{}) string {
	return toolReadForced(args, ".docx")
}

// ToolReadPDF reads a PDF file via the native GoPDF2 extractor.
func ToolReadPDF(args map[string]interface{}) string {
	return toolReadForced(args, ".pdf")
}

func toolReadForced(args map[string]interface{}, wantExt string) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	filePath = resolveOfficeToolPath(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != "" && ext != wantExt {
		// Still try — some files have wrong extensions — but warn in header via ExtractOfficeText path.
		// Prefer explicit mismatch message when clearly wrong modern/legacy pair.
		if (wantExt == ".doc" && ext == ".docx") || (wantExt == ".docx" && ext == ".doc") ||
			(wantExt == ".pdf" && ext != ".pdf") {
			return fmt.Sprintf("扩展名是 %s，但当前 action 期望 %s。请改用 office(action=\"read_document\", file_path=...) 自动识别。", ext, wantExt)
		}
	}
	// Reuse unified reader (includes header + truncation).
	return ToolReadDocument(args)
}

func officeFilePathArg(args map[string]interface{}) string {
	if p := StringArg(args, "file_path"); p != "" {
		return p
	}
	return StringArg(args, "path")
}

// resolveOfficeToolPath expands ~ and resolves relative paths against the
// process workspace. Absolute paths are cleaned only.
func resolveOfficeToolPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	p = corelib.ExpandHomePath(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return ResolvePath(p)
}

// ExtractOfficeText detects format by extension (with content sniff fallback)
// and extracts plain text. Returns (text, formatKind, error).
func ExtractOfficeText(filePath string) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	format := strings.TrimPrefix(ext, ".")
	if format == "" {
		format = "unknown"
	}

	// Unknown / exotic extension → sniff before giving up.
	switch ext {
	case ".txt", ".md", ".markdown", ".pdf", ".docx", ".doc", ".xlsx", ".csv", ".xls", ".pptx", ".ppt":
	default:
		if sniffed := sniffOfficeFormat(filePath); sniffed != "" {
			format = sniffed
		}
	}

	text, kind, err := ExtractOfficeTextWithFormat(filePath, format)
	if err == nil {
		return text, kind, nil
	}
	// Mislabeled extension (e.g. .doc that is actually .docx): sniff once and retry.
	if sniffed := sniffOfficeFormat(filePath); sniffed != "" && sniffed != format {
		if text2, kind2, err2 := ExtractOfficeTextWithFormat(filePath, sniffed); err2 == nil {
			return text2, kind2, nil
		}
	}
	if format == "unknown" || format == "" {
		return "", format, fmt.Errorf("原生解析暂不支持文件类型 %s（内置支持: .pdf .doc .docx .xls .xlsx .csv .pptx .txt .md）", ext)
	}
	return "", kind, err
}

// ExtractOfficeTextWithFormat extracts using an explicit format kind (no extension lookup).
func ExtractOfficeTextWithFormat(filePath, format string) (string, string, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	switch format {
	case "pdf":
		text, err := ExtractPDFText(filePath)
		return text, "pdf", err
	case "docx":
		text, err := ExtractDocxText(filePath)
		return text, "docx", err
	case "doc":
		text, err := ExtractDocText(filePath)
		return text, "doc", err
	case "xlsx", "csv":
		text, err := extractSpreadsheetText(filePath, "")
		return text, format, err
	case "xls":
		text, err := ExtractXLSText(filePath)
		return text, "xls", err
	case "pptx":
		text, err := ExtractPPTXText(filePath)
		return text, "pptx", err
	case "txt", "md", "markdown":
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", format, err
		}
		return string(data), format, nil
	case "ppt":
		return "", "ppt", fmt.Errorf("原生解析暂不支持旧版 PowerPoint .ppt")
	default:
		return "", format, fmt.Errorf("未知格式 %s", format)
	}
}

// sniffOfficeFormat returns a format kind from file magic bytes, or "".
// Distinguishes PDF, OLE2 (.doc/.xls), and OOXML zip (.docx/.xlsx/.pptx).
func sniffOfficeFormat(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var hdr [8]byte
	n, err := f.Read(hdr[:])
	if err != nil || n < 4 {
		return ""
	}
	// PDF
	if n >= 4 && string(hdr[:4]) == "%PDF" {
		return "pdf"
	}
	// OLE2 compound document (legacy .doc / .xls / .ppt).
	// Avoid full parse during sniff — use extension when it is already OLE family.
	if n >= 8 && hdr[0] == 0xD0 && hdr[1] == 0xCF && hdr[2] == 0x11 && hdr[3] == 0xE0 {
		ext := strings.ToLower(filepath.Ext(filePath))
		switch ext {
		case ".doc", ".xls", ".ppt":
			return strings.TrimPrefix(ext, ".")
		}
		// Default OLE → doc (callers may retry xls via extract error paths).
		return "doc"
	}
	// ZIP / OOXML
	if n >= 2 && hdr[0] == 'P' && hdr[1] == 'K' {
		return sniffOOXMLKind(filePath)
	}
	return ""
}

func sniffOOXMLKind(filePath string) string {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return ""
	}
	defer r.Close()
	var hasWord, hasXL, hasPPT bool
	for _, f := range r.File {
		name := strings.ToLower(f.Name)
		switch {
		case name == "word/document.xml" || strings.HasPrefix(name, "word/"):
			hasWord = true
		case name == "xl/workbook.xml" || strings.HasPrefix(name, "xl/"):
			hasXL = true
		case strings.HasPrefix(name, "ppt/"):
			hasPPT = true
		}
	}
	switch {
	case hasWord:
		return "docx"
	case hasXL:
		return "xlsx"
	case hasPPT:
		return "pptx"
	default:
		return ""
	}
}

// formatOfficeReadFailure returns a structured failure message that steers the
// agent toward craft_tool / skills / conversion instead of giving up.
func formatOfficeReadFailure(filePath, format string, err error) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if format == "" {
		format = strings.TrimPrefix(ext, ".")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("读取失败: %v\n", err))
	b.WriteString(fmt.Sprintf("# path: %s\n# format: %s\n", filePath, format))
	b.WriteString("\n## 下一步（请继续尝试，不要直接告诉用户无法读取）\n")
	b.WriteString("1. **优先 craft_tool** 生成一次性解析脚本并输出纯文本，例如：\n")
	b.WriteString(fmt.Sprintf("   craft_tool(task=\"读取本地文件并提取全部可读文本，打印到 stdout。文件路径: %s；扩展名: %s。优先用 Python；若缺依赖可 pip install。不要打开 GUI。\")\n", filePath, ext))
	b.WriteString("2. 或 manage_skill(action=\"search\", query=\"文档解析 document parse "+format+"\") 查找/安装解析 Skill，再 manage_skill(action=\"run\", ...)\n")
	switch ext {
	case ".ppt":
		b.WriteString("3. .ppt 备选：请用户用 PowerPoint/WPS 另存为 .pptx 后 office(action=\"read_document\")；或 craft_tool 用 LibreOffice/COM 转换后读取\n")
	case ".doc", ".xls":
		b.WriteString("3. 旧 Office 备选：craft_tool 用 PowerShell Word/Excel COM，或 LibreOffice 转为 docx/xlsx 后再 read_document\n")
	case ".pdf":
		b.WriteString("3. 扫描版 PDF 备选：craft_tool 做 OCR（如 paddleocr/tesseract），或请用户提供可选中文本的 PDF\n")
	default:
		b.WriteString("3. 仍失败时：bash 调用已安装的转换工具；最后才请用户另存为 .docx/.pdf/.txt\n")
	}
	b.WriteString("4. 【禁止】未尝试 craft_tool/Skill 就回复「无法解析/无法读取」\n")
	return b.String()
}

// ExtractPDFText extracts all page text from a PDF using GoPDF2.
// Prefers page-by-page extraction with ## Page N markers for long/structured docs.
func ExtractPDFText(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取 PDF 失败: %v", err)
	}

	// Page-by-page is more reliable for long Chinese bid/spec PDFs than a single
	// all-pages dump (better order, recoverable partial failures).
	if numPages, pageErr := gopdf2.GetSourcePDFPageCountFromBytes(data); pageErr == nil && numPages > 0 {
		var b strings.Builder
		gotAny := false
		for i := 0; i < numPages; i++ {
			pageText, pErr := gopdf2.ExtractPageText(data, i)
			if pErr != nil {
				// Non-fatal: keep going; mark the gap so the agent can re-try that page.
				fmt.Fprintf(&b, "\n\n## Page %d\n[page extract error: %v]\n", i+1, pErr)
				continue
			}
			pageText = strings.TrimSpace(pageText)
			if pageText == "" {
				continue
			}
			gotAny = true
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "## Page %d\n", i+1)
			b.WriteString(pageText)
		}
		if gotAny {
			return b.String(), nil
		}
	}

	text, err := gopdf2.ExtractAllPagesText(data)
	if err != nil {
		return "", fmt.Errorf("解析 PDF 失败: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("PDF 中没有可读取的文本内容（可能是扫描件，需 OCR）")
	}
	return text, nil
}

// ExtractDocxText extracts text from a .docx (OOXML) file including body,
// tables, headers and footers. Text is taken from w:t runs only (not arbitrary
// CharData) so field codes / revision markup do not corrupt mid-document text.
func ExtractDocxText(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开 DOCX 文件: %v", err)
	}
	defer r.Close()

	// Collect body first, then headers/footers (common tender/spec structure).
	var bodyXML []byte
	var extraParts [][]byte
	for _, f := range r.File {
		// OOXML paths are usually forward-slash; normalize for odd producers.
		name := strings.ReplaceAll(f.Name, "\\", "/")
		lower := strings.ToLower(name)
		switch {
		case lower == "word/document.xml":
			bodyXML, err = readZipFile(f)
			if err != nil {
				return "", err
			}
		case strings.HasPrefix(lower, "word/header") && strings.HasSuffix(lower, ".xml"),
			strings.HasPrefix(lower, "word/footer") && strings.HasSuffix(lower, ".xml"):
			// Skip empty / relationship-only stubs; order after body is fine for search.
			if data, rErr := readZipFile(f); rErr == nil && len(data) > 64 {
				extraParts = append(extraParts, data)
			}
		}
	}
	if len(bodyXML) == 0 {
		return "", fmt.Errorf("DOCX 文件中未找到 document.xml")
	}

	var parts []string
	bodyParas, err := docxParagraphs(bodyXML)
	if err != nil {
		return "", fmt.Errorf("解析 DOCX XML 失败: %v", err)
	}
	parts = append(parts, bodyParas...)
	for _, extra := range extraParts {
		if paras, pErr := docxParagraphs(extra); pErr == nil {
			parts = append(parts, paras...)
		}
	}
	text := strings.Join(parts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("DOCX 文件中没有可读取的文本内容")
	}
	return text, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// docxParagraphs walks OOXML and emits one string per paragraph.
// Only w:t text runs are collected (plus tab/br). Table cells become
// tab-separated fields when multiple cells appear in a row.
// Nested tables (common in Word) are folded into the enclosing cell text
// instead of resetting the outer row state.
func docxParagraphs(data []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var paragraphs []string
	var paragraph strings.Builder
	inParagraph := false
	inText := false // inside <w:t>
	inTableRow := false
	var rowCells []string
	var cellText strings.Builder
	inCell := false
	tableDepth := 0 // outermost table = 1

	flushParagraph := func() {
		if !inParagraph {
			return
		}
		text := strings.TrimSpace(paragraph.String())
		paragraph.Reset()
		inParagraph = false
		inText = false
		if text == "" {
			return
		}
		if inCell {
			if cellText.Len() > 0 {
				cellText.WriteByte('\n')
			}
			cellText.WriteString(text)
			return
		}
		paragraphs = append(paragraphs, text)
	}

	flushCell := func() {
		if !inCell {
			return
		}
		// Flush any open paragraph inside the cell first.
		flushParagraph()
		rowCells = append(rowCells, strings.TrimSpace(cellText.String()))
		cellText.Reset()
		inCell = false
	}

	flushRow := func() {
		flushCell()
		if !inTableRow {
			return
		}
		// Drop fully empty rows.
		nonEmpty := false
		for _, c := range rowCells {
			if strings.TrimSpace(c) != "" {
				nonEmpty = true
				break
			}
		}
		if nonEmpty {
			// Join cells with tab so column structure survives plain-text extract.
			paragraphs = append(paragraphs, strings.Join(rowCells, "\t"))
		}
		rowCells = nil
		inTableRow = false
	}

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				tableDepth++
			case "tr":
				// Only outermost table rows drive tab-separated structure.
				if tableDepth == 1 {
					inTableRow = true
					rowCells = nil
				}
			case "tc":
				if tableDepth == 1 {
					inCell = true
					cellText.Reset()
				}
			case "p":
				if inParagraph {
					flushParagraph()
				}
				inParagraph = true
				inText = false
			case "t":
				// Only real Word text runs.
				if inParagraph {
					inText = true
				}
			case "tab":
				if inParagraph {
					paragraph.WriteString("\t")
				}
			case "br", "cr":
				if inParagraph {
					paragraph.WriteString("\n")
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				flushParagraph()
			case "tc":
				if tableDepth == 1 {
					flushCell()
				}
			case "tr":
				if tableDepth == 1 {
					flushRow()
				}
			case "tbl":
				if tableDepth > 0 {
					tableDepth--
				}
			}
		case xml.CharData:
			if inParagraph && inText {
				paragraph.Write(t)
			}
		}
	}
	flushParagraph()
	flushRow()
	return paragraphs, nil
}

// ExtractDocText extracts text from a legacy Word 97-2003 .doc file.
func ExtractDocText(filePath string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("DOC 文件解析异常: %v", r)
		}
	}()
	document, openErr := legacydoc.OpenFile(filePath)
	if openErr != nil {
		return "", fmt.Errorf("无法打开 DOC 文件: %v", openErr)
	}
	// Prefer structured paragraphs when available.
	if formatted := document.GetFormattedContent(); formatted != nil && len(formatted.Paragraphs) > 0 {
		var parts []string
		for _, para := range formatted.Paragraphs {
			var sb strings.Builder
			for _, run := range para.Runs {
				sb.WriteString(run.Text)
			}
			if para.TextBoxText != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(para.TextBoxText)
			}
			if t := strings.TrimSpace(sb.String()); t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}
	}
	text = document.GetText()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("DOC 文件中没有可读取的文本内容")
	}
	return text, nil
}

// ExtractXLSText extracts tab-separated text from a legacy Excel .xls workbook.
func ExtractXLSText(filePath string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("XLS 文件解析异常: %v", r)
		}
	}()
	wb, openErr := xls.OpenFile(filePath)
	if openErr != nil {
		return "", fmt.Errorf("无法打开 XLS 文件: %v", openErr)
	}
	numSheets := wb.GetNumberSheets()
	if numSheets == 0 {
		return "", fmt.Errorf("XLS 文件中没有工作表")
	}
	var b strings.Builder
	for sheetIdx := 0; sheetIdx < numSheets; sheetIdx++ {
		sheet, sErr := wb.GetSheet(sheetIdx)
		if sErr != nil || sheet == nil {
			continue
		}
		name := sheet.GetName()
		if name == "" {
			name = fmt.Sprintf("Sheet%d", sheetIdx+1)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(name)
		b.WriteByte('\n')
		numRows := sheet.GetNumberRows()
		for rowIdx := 0; rowIdx < numRows; rowIdx++ {
			row, rErr := sheet.GetRow(rowIdx)
			if rErr != nil || row == nil {
				continue
			}
			cols := row.GetCols()
			cells := make([]string, 0, len(cols))
			for _, col := range cols {
				cells = append(cells, strings.TrimSpace(col.GetString()))
			}
			line := strings.Join(cells, "\t")
			if strings.TrimSpace(line) == "" {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("XLS 文件中没有可读取的文本内容")
	}
	return out, nil
}

// ExtractPPTXText flattens a PPTX into readable slide text.
func ExtractPPTXText(filePath string) (string, error) {
	pres, err := pptx.Read(filePath)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, slide := range pres.Slides {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("## Slide %d\n", slide.Number))
		for _, shape := range slide.Shapes {
			if shape.Text != nil {
				for _, para := range shape.Text.Paragraphs {
					var line strings.Builder
					for _, run := range para.Runs {
						line.WriteString(run.Text)
					}
					if t := strings.TrimSpace(line.String()); t != "" {
						b.WriteString(t)
						b.WriteByte('\n')
					}
				}
			}
			if shape.Table != nil {
				for _, row := range shape.Table.Rows {
					var cells []string
					for _, cell := range row.Cells {
						cells = append(cells, strings.TrimSpace(cell.Text))
					}
					if line := strings.TrimSpace(strings.Join(cells, "\t")); line != "" {
						b.WriteString(line)
						b.WriteByte('\n')
					}
				}
			}
		}
		if notes := strings.TrimSpace(slide.Notes); notes != "" {
			b.WriteString("\n[Notes]\n")
			b.WriteString(notes)
			b.WriteByte('\n')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("PPTX 中没有可读取的文本内容")
	}
	return out, nil
}

func extractSpreadsheetText(filePath, sheet string) (string, error) {
	result, err := excel.ReadFile(filePath, excel.ReadOptions{SheetName: sheet})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if result.SheetName != "" {
		b.WriteString("## ")
		b.WriteString(result.SheetName)
		b.WriteByte('\n')
	}
	for _, row := range result.Rows {
		cells := make([]string, 0, len(row))
		for _, cell := range row {
			cells = append(cells, cellValueString(cell))
		}
		// Trim trailing empty cells for readability.
		for len(cells) > 0 && cells[len(cells)-1] == "" {
			cells = cells[:len(cells)-1]
		}
		if len(cells) == 0 {
			continue
		}
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteByte('\n')
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("表格中没有可读取的内容")
	}
	return out, nil
}

func cellValueString(cell excel.CellValue) string {
	if cell.Value == nil {
		return ""
	}
	switch v := cell.Value.(type) {
	case string:
		return v
	case float64:
		// Avoid scientific notation for integers.
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// readXLSAsExcelJSON returns legacy .xls data in the same JSON shape as read_excel.
// Opens the workbook once (no intermediate full-text pass).
func readXLSAsExcelJSON(filePath, sheetFilter string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("XLS 文件解析异常: %v", r)
		}
	}()
	wb, openErr := xls.OpenFile(filePath)
	if openErr != nil {
		return "", fmt.Errorf("无法打开 XLS 文件: %v", openErr)
	}
	numSheets := wb.GetNumberSheets()
	if numSheets == 0 {
		return "", fmt.Errorf("XLS 文件中没有工作表")
	}

	// read_excel historically returns a single sheet. Match that behavior.
	chosenIdx := 0
	if sheetFilter != "" {
		found := false
		for i := 0; i < numSheets; i++ {
			sh, e := wb.GetSheet(i)
			if e != nil || sh == nil {
				continue
			}
			if strings.EqualFold(sh.GetName(), sheetFilter) {
				chosenIdx = i
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("工作表 %q 不存在", sheetFilter)
		}
	}
	sh, getErr := wb.GetSheet(chosenIdx)
	if getErr != nil || sh == nil {
		return "", fmt.Errorf("无法读取工作表")
	}
	name := sh.GetName()
	if name == "" {
		name = fmt.Sprintf("Sheet%d", chosenIdx+1)
	}
	numRows := sh.GetNumberRows()
	rows := make([][]excel.CellValue, 0, numRows)
	maxCols := 0
	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		row, rErr := sh.GetRow(rowIdx)
		if rErr != nil || row == nil {
			continue
		}
		cols := row.GetCols()
		cells := make([]excel.CellValue, 0, len(cols))
		empty := true
		for _, col := range cols {
			s := col.GetString()
			if s == "" {
				cells = append(cells, excel.CellValue{Type: excel.CellTypeEmpty})
			} else {
				empty = false
				cells = append(cells, excel.CellValue{Value: s, Type: excel.CellTypeString})
			}
		}
		if empty {
			continue
		}
		if len(cells) > maxCols {
			maxCols = len(cells)
		}
		rows = append(rows, cells)
	}
	result := excel.ReadResult{
		SheetName: name,
		Rows:      rows,
		RowCount:  len(rows),
		ColCount:  maxCols,
	}
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(data), nil
}
