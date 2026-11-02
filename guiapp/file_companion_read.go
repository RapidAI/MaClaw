package guiapp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const fileCompanionReadDocumentMaxChars = 30000

func fileCompanionReadDocumentDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        fileCompanionToolReadDocument,
			"description": "读取当前打开的这一份文档正文。支持 Word（.doc/.docx）、Excel（.xls/.xlsx/.csv）、PowerPoint（.ppt/.pptx）、PDF、纯文本、Markdown、JSON、XML、YAML、日志、EPUB、MOBI、AsciiDoc、邮件（.eml），以及 .dwg/.dxf/.glb/.gltf。不要传路径。长文按返回的 offset 续读。PDF 要按页、看图或识别扫描页时改用 pdf_read_pages、pdf_render_page、pdf_extract_images。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "从第几个字符继续。第一次不传或传 0。",
					},
					"max_chars": map[string]interface{}{
						"type":        "integer",
						"description": "这一段最多返回多少字，默认 30000，上限 30000。",
					},
					"line_numbers": map[string]interface{}{
						"type":        "boolean",
						"description": "为 true 时在每行前加行号。",
					},
				},
			},
		},
	}
}

func (h *IMMessageHandler) fileCompanionReadDocument(canonical string, args map[string]interface{}) (string, bool) {
	if supplied := fileCompanionSuppliedReadPath(args); supplied != "" && !fileCompanionSuppliedPathIsOpen(supplied, canonical) {
		return "只能读取当前打开的这一份文档，不能改读其它路径。", false
	}
	ext := strings.ToLower(filepath.Ext(canonical))
	if fileCompanionPlainTextReadExt(ext) || (fileCompanionNativeTextExt(ext) && fileCompanionHasUTF16BOM(canonical)) {
		return fileCompanionReadPlainText(canonical, args)
	}
	if !fileCompanionNativeDocumentExt(ext) {
		return "当前文件没有可抽取的文档正文。read_document 可读 Word（.doc/.docx）、Excel（.xls/.xlsx/.csv）、PowerPoint（.ppt/.pptx）、PDF、纯文本、Markdown、EPUB、MOBI、邮件（.eml），以及图纸和三维预览（.dwg/.dxf/.glb/.gltf）。音视频请让用户点「生成笔记」。", false
	}
	raw := agent.ToolReadDocument(fileCompanionReadDocumentCallArgs(canonical, args))
	return fileCompanionPresentDocumentRead(canonical, raw)
}

func fileCompanionSuppliedReadPath(args map[string]interface{}) string {
	if path := fileCompanionStringArg(args, "file_path"); path != "" {
		return path
	}
	return fileCompanionStringArg(args, "path")
}

func fileCompanionSuppliedPathIsOpen(supplied, canonical string) bool {
	supplied = strings.TrimSpace(supplied)
	if supplied == "" {
		return true
	}
	if !strings.ContainsAny(supplied, `\/`) && strings.EqualFold(supplied, filepath.Base(canonical)) {
		return true
	}
	cleaned := filepath.Clean(supplied)
	if strings.EqualFold(cleaned, canonical) {
		return true
	}
	abs, err := filepath.Abs(supplied)
	return err == nil && strings.EqualFold(filepath.Clean(abs), canonical)
}

func fileCompanionNativeDocumentExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx",
		".txt", ".md", ".markdown", ".json", ".xml", ".yaml", ".yml", ".log",
		".dwg", ".dxf", ".glb", ".gltf", ".epub", ".mobi", ".adoc", ".asciidoc", ".eml":
		return true
	default:
		return false
	}
}

func fileCompanionPlainTextReadExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".html", ".htm", ".text":
		return true
	default:
		return false
	}
}

func fileCompanionNativeTextExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".txt", ".md", ".markdown", ".json", ".xml", ".yaml", ".yml", ".log":
		return true
	default:
		return false
	}
}

func fileCompanionHasUTF16BOM(canonical string) bool {
	file, err := os.Open(canonical)
	if err != nil {
		return false
	}
	defer file.Close()
	var bom [2]byte
	n, err := file.Read(bom[:])
	if err != nil || n < 2 {
		return false
	}
	return (bom[0] == 0xFF && bom[1] == 0xFE) || (bom[0] == 0xFE && bom[1] == 0xFF)
}

func fileCompanionReadDocumentCallArgs(canonical string, args map[string]interface{}) map[string]interface{} {
	call := map[string]interface{}{
		"file_path": canonical,
		"offset":    fileCompanionNonNegativeInt(args, "offset", 0),
		"max_chars": fileCompanionReadMaxChars(args),
	}
	if fileCompanionFlagArg(args, "line_numbers") {
		call["line_numbers"] = true
	}
	return call
}

func fileCompanionReadMaxChars(args map[string]interface{}) int {
	maxChars := fileCompanionReadDocumentMaxChars
	if args == nil {
		return maxChars
	}
	if _, ok := args["max_chars"]; !ok {
		return maxChars
	}
	if n := fileCompanionNonNegativeInt(args, "max_chars", 0); n > 0 && n < maxChars {
		return n
	}
	return maxChars
}

func fileCompanionNonNegativeInt(args map[string]interface{}, name string, fallback int) int {
	if args == nil {
		return fallback
	}
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback
	}
	switch value := raw.(type) {
	case int:
		if value < 0 {
			return fallback
		}
		return value
	case float64:
		if value < 0 || value != float64(int(value)) {
			return fallback
		}
		return int(value)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < 0 {
			return fallback
		}
		return number
	default:
		return fallback
	}
}

func fileCompanionReadPlainText(canonical string, args map[string]interface{}) (string, bool) {
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		return "当前文档读不出来。", false
	}
	if info.Size() > agent.MaxOfficeReadFileBytes {
		return "这份文档超过读取上限。可以缩小文件后再读。", false
	}
	file, err := os.Open(canonical)
	if err != nil {
		return "当前文档读不出来。", false
	}
	data, err := io.ReadAll(io.LimitReader(file, agent.MaxOfficeReadFileBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "当前文档读不出来。", false
	}
	if int64(len(data)) > agent.MaxOfficeReadFileBytes {
		return "这份文档超过读取上限。可以缩小文件后再读。", false
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(canonical)), ".")
	return fileCompanionFormatDocumentPage(filepath.Base(canonical), format, fileCompanionDecodePlainText(data), fileCompanionNonNegativeInt(args, "offset", 0), fileCompanionReadMaxChars(args), fileCompanionFlagArg(args, "line_numbers"))
}

func fileCompanionDecodePlainText(data []byte) string {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		return fileCompanionDecodeUTF16(data[2:], true)
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return fileCompanionDecodeUTF16(data[2:], false)
	}
	return strings.TrimPrefix(string(data), "\uFEFF")
}

func fileCompanionDecodeUTF16(data []byte, little bool) string {
	if len(data)%2 == 1 {
		data = data[:len(data)-1]
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		if little {
			units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
		} else {
			units[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
	}
	return string(utf16.Decode(units))
}

func fileCompanionTrimBOM(text string) string {
	return strings.TrimPrefix(text, "\uFEFF")
}

func fileCompanionFormatDocumentPage(base, format, text string, offset, maxChars int, lineNumbers bool) (string, bool) {
	runes := []rune(text)
	if len(runes) == 0 {
		return fmt.Sprintf("这份文档没有可读取的正文。\n# document: %s\n# format: %s\n# truncated: false", base, format), true
	}
	if offset >= len(runes) {
		return fmt.Sprintf("已到文档末尾（offset=%d, total_chars=%d）。没有更多内容。\n# document: %s\n# format: %s\n# truncated: false", offset, len(runes), base, format), true
	}
	chunk := runes[offset:]
	truncated := false
	next := 0
	if len(chunk) > maxChars {
		chunk = chunk[:maxChars]
		truncated = true
		next = offset + maxChars
	}
	body := string(chunk)
	if lineNumbers {
		start := 1
		if offset > 0 {
			start = 1 + strings.Count(string(runes[:offset]), "\n")
		}
		body = fileCompanionPrefixLineNumbers(body, start)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# format: %s\n# document: %s\n# total_chars: %d\n# offset: %d\n# chars: %d\n", format, base, len(runes), offset, len(chunk))
	if truncated {
		fmt.Fprintf(&b, "# truncated: true\n# next_offset: %d\n# continue: read_document(offset=%d, max_chars=%d", next, next, maxChars)
		if lineNumbers {
			b.WriteString(", line_numbers=true")
		}
		b.WriteString(")\n")
	} else {
		b.WriteString("# truncated: false\n")
	}
	b.WriteByte('\n')
	b.WriteString(body)
	return b.String(), true
}

func fileCompanionPrefixLineNumbers(text string, start int) string {
	if start < 1 {
		start = 1
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "L%d: %s", start+i, line)
	}
	return b.String()
}

func fileCompanionPresentDocumentRead(canonical, raw string) (string, bool) {
	if class, failed := agent.DocumentReadFailure(raw); failed {
		return fileCompanionDocumentReadFailureText(canonical, class), false
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "缺少 file_path") {
		return "当前文档读不出来。", false
	}
	cleaned := fileCompanionScrubDocumentRead(canonical, raw)
	if strings.TrimSpace(cleaned) == "" {
		return "没有从当前文档读出正文。", false
	}
	return cleaned, true
}

func fileCompanionDocumentReadFailureText(canonical, class string) string {
	switch class {
	case "encrypted":
		return "这份文档已加密，伴读不能解密。请在本地办公软件里另存为未加密文件后再打开。"
	case "input_too_large", "output_too_large":
		return "这份文档超过读取上限。可以缩小文件，或换一个较短的部分再读。"
	case "malformed":
		return "这份文档没有通过安全校验，伴读不会用其它解析器重试。"
	case "source_changed":
		return "读取期间文档变了，请再读一次。"
	case "unavailable", "invalid_path":
		return "当前打开的不是一份可读文档。"
	}
	if strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return "按整篇文本没有读出正文。扫描页请改用 pdf_read_pages。"
	}
	return "没有从当前文档读出正文。"
}

func fileCompanionScrubDocumentRead(canonical, raw string) string {
	base := filepath.Base(canonical)
	raw = fileCompanionReplacePath(raw, canonical, base)
	// The reader puts its own # path and # continue lines above a blank line.
	// A Markdown heading in the document uses the same spelling and must stay.
	inBody := false
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if !inBody && trim == "" {
			inBody = true
			b.WriteByte('\n')
			continue
		}
		switch {
		case !inBody && strings.HasPrefix(trim, "# path:"):
			fmt.Fprintf(&b, "# document: %s\n", base)
		case !inBody && strings.HasPrefix(trim, "# continue:"):
			b.WriteString(fileCompanionRewriteReadContinue(line))
			b.WriteByte('\n')
		default:
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fileCompanionRewriteReadContinue(line string) string {
	extra := ""
	if strings.Contains(line, "line_numbers=true") {
		extra = ", line_numbers=true"
	}
	return fmt.Sprintf("# continue: read_document(offset=%s, max_chars=%s%s)", fileCompanionHeaderValue(line, "offset="), fileCompanionHeaderValue(line, "max_chars="), extra)
}

func fileCompanionHeaderValue(line, key string) string {
	index := strings.LastIndex(line, key)
	if index < 0 {
		return "0"
	}
	rest := line[index+len(key):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return "0"
	}
	return rest[:end]
}
