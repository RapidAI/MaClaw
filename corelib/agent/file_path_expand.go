package agent

// file_path_expand.go implements automatic document extraction for user-selected
// local file paths embedded in the message text (GUI file picker / upload).
//
// 治本: when the user attaches PDF/Word/Excel/PPT/text, the host extracts text
// with native office parsers and injects it into the user turn so the model can
// answer without first calling office(read_document). Tool calls remain available
// for paging truncated extracts and for unsupported formats.
//
// Context safety:
//   - Per-file and per-turn rune caps (far below office tool max)
//   - Max on-disk size for auto-parse (skip huge binaries without loading)
//   - History strip removes injected bodies on subsequent turns

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Auto-extract markers written into the expanded user message.
const (
	// AutoExtractNotice is placed once after the path list when any document was expanded.
	AutoExtractNotice = "[系统已自动解析文档正文 — 优先基于下列内容回答；仅当 truncated=true 或解析失败时，再 office(read_document) 分页]"

	// AutoExtractBeginMarker prefixes each injected document body.
	// Full form: --- auto_extract: begin path="..." format="..." ... ---
	AutoExtractBeginMarker = "--- auto_extract: begin "

	// AutoExtractEndMarker closes each injected document body.
	// Full form: --- auto_extract: end path="..." ---
	AutoExtractEndMarker = "--- auto_extract: end "

	// autoExtractHistoryPlaceholder is written when stripping live extract notice from history.
	autoExtractHistoryPlaceholder = "[之前已自动解析文档正文，正文已省略]"
)

// Caps for automatic injection into the *user turn*.
// Intentionally much smaller than office(read_document) default (120k) / hard max (500k).
//
// Rough budget (Chinese-heavy text ≈ 1 token/rune):
//   - ~20k runes/file ≈ one medium chapter
//   - ~40k runes total across all attachments in one turn
const (
	defaultAutoInjectMaxRunesPerFile = 20_000
	defaultAutoInjectMaxRunesTotal   = 40_000

	// Keep automatic injection and read_document on the same full-source
	// boundary. Paging limits the result sent to the model, but extraction still
	// needs to parse the complete container and therefore cannot safely bypass
	// this input cap.
	defaultAutoInjectMaxFileBytes = MaxOfficeReadFileBytes
)

// ExpandUserSelectedFilePaths finds the GUI marker section, extracts supported
// document files via native parsers, and appends the text bodies. Image and
// other non-document paths are left listed only. Idempotent when already expanded.
func ExpandUserSelectedFilePaths(text string) string {
	return expandUserSelectedFilePathsWithSettings(text, currentOfficeReadSettings())
}

func expandUserSelectedFilePathsWithSettings(text string, settings officeReadSettings) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if !strings.Contains(text, FilePathPromptPrefix) {
		return text
	}
	// Already expanded this turn — do not double-inject.
	// Require a real begin line (path=) or the host notice; bare marker text in user prose
	// must not block expansion of a genuine path section.
	if strings.Contains(text, AutoExtractNotice) || hasAutoExtractBegin(text) {
		return text
	}

	idx := strings.Index(text, FilePathPromptPrefix)
	if idx < 0 {
		return text
	}
	before := text[:idx]
	section := text[idx+len(FilePathPromptPrefix):]
	section = strings.TrimPrefix(section, "\r\n")
	section = strings.TrimPrefix(section, "\n")

	paths, rest := parseSelectedFilePathLines(section)
	if len(paths) == 0 {
		return text
	}

	docPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		if IsDocumentFilePath(p) {
			docPaths = append(docPaths, p)
		}
	}
	extracted := formatAutoExtractedDocumentsWithSettings(docPaths, defaultAutoInjectMaxRunesPerFile, defaultAutoInjectMaxRunesTotal, nil, settings)
	hasExtract := false
	for _, block := range extracted {
		if block != "" {
			hasExtract = true
			break
		}
	}

	// Rebuild: keep path list, drop legacy tool-call instructions in rest,
	// append auto-extract bodies.
	var b strings.Builder
	// Extract bodies are rune-capped; UTF-8 Chinese ≈ 3 bytes/rune — pre-size to avoid growth thrash.
	b.Grow(len(text) + defaultAutoInjectMaxRunesTotal*3 + 2048)
	b.WriteString(before)
	b.WriteString(FilePathPromptPrefix)
	b.WriteByte('\n')
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	if note := strings.TrimSpace(filterLegacyPathInstructions(rest)); note != "" {
		b.WriteByte('\n')
		b.WriteString(note)
		b.WriteByte('\n')
	}
	if hasExtract {
		b.WriteByte('\n')
		b.WriteString(AutoExtractNotice)
		b.WriteByte('\n')
		for _, block := range extracted {
			if block == "" {
				continue
			}
			b.WriteByte('\n')
			b.WriteString(block)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatAutoExtractedDocument extracts one local document for IM attachment
// injection (bounded). Returns "" only when the path is not a document type.
// Soft failures still return a short error block so the model can fall back.
func FormatAutoExtractedDocument(filePath string) string {
	block, _ := formatAutoExtractedDocumentWithSettings(filePath, defaultAutoInjectMaxRunesPerFile, currentOfficeReadSettings())
	return block
}

// FormatAutoExtractedDocuments extracts multiple documents under a shared total
// rune budget (used by IM multi-attachment paths).
func FormatAutoExtractedDocuments(filePaths []string) []string {
	return formatAutoExtractedDocumentsWithSettings(filePaths, defaultAutoInjectMaxRunesPerFile, defaultAutoInjectMaxRunesTotal, nil, currentOfficeReadSettings())
}

// AppendDocumentExtractsToDescriptions attaches bounded auto-extract blocks to
// "[附件: name → 已保存到 path]" lines. Shares the per-turn budget with any
// path-marker extracts already present in userText and skips duplicate paths.
// Non-attachment lines are left unchanged. Exported so GUI/TUI stay in sync.
func AppendDocumentExtractsToDescriptions(fileDescriptions []string, userText string) []string {
	return appendDocumentExtractsToDescriptionsWithSettings(fileDescriptions, userText, currentOfficeReadSettings())
}

func appendDocumentExtractsToDescriptionsWithSettings(fileDescriptions []string, userText string, settings officeReadSettings) []string {
	if len(fileDescriptions) == 0 {
		return fileDescriptions
	}
	const marker = " → 已保存到 "
	type hit struct {
		idx  int
		path string
	}
	var docs []hit
	for i, desc := range fileDescriptions {
		// Only IM attachment lines — not voice/image fallbacks that may share "已保存到".
		if !strings.HasPrefix(strings.TrimSpace(desc), "[附件:") {
			continue
		}
		j := strings.Index(desc, marker)
		if j < 0 {
			continue
		}
		path := strings.TrimSpace(strings.TrimSuffix(desc[j+len(marker):], "]"))
		if path == "" || !IsDocumentFilePath(path) {
			continue
		}
		docs = append(docs, hit{idx: i, path: path})
	}
	if len(docs) == 0 {
		return fileDescriptions
	}
	paths := make([]string, len(docs))
	for i, d := range docs {
		paths[i] = d.path
	}
	blocks := formatAutoExtractedDocumentsWithSettings(paths, defaultAutoInjectMaxRunesPerFile, RemainingAutoInjectBudget(userText), AlreadyAutoExtractedPaths(userText), settings)
	any := false
	for _, block := range blocks {
		if block != "" {
			any = true
			break
		}
	}
	if !any {
		return fileDescriptions
	}
	out := make([]string, len(fileDescriptions))
	copy(out, fileDescriptions)
	for i, d := range docs {
		if i < len(blocks) && blocks[i] != "" {
			out[d.idx] = out[d.idx] + "\n" + blocks[i]
		}
	}
	if !strings.Contains(userText, AutoExtractNotice) {
		out = append([]string{AutoExtractNotice}, out...)
	}
	return out
}

// FormatAutoExtractedDocumentsWithBudget is like FormatAutoExtractedDocuments but
// accepts a remaining total budget and a set of paths already injected earlier in
// the same turn (e.g. GUI path marker expand before IM attachments).
func FormatAutoExtractedDocumentsWithBudget(filePaths []string, totalBudget int, skipPaths map[string]struct{}) []string {
	return formatAutoExtractedDocuments(filePaths, defaultAutoInjectMaxRunesPerFile, totalBudget, skipPaths)
}

// CountInjectedAutoExtractRunes sums injected_chars= from begin markers in text.
func CountInjectedAutoExtractRunes(text string) int {
	if text == "" || !strings.Contains(text, AutoExtractBeginMarker) {
		return 0
	}
	sum := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isAutoExtractBeginLine(trimmed) {
			continue
		}
		sum += extractIntAttr(trimmed, "injected_chars")
	}
	return sum
}

// RemainingAutoInjectBudget returns how many runes may still be auto-injected
// in this turn given text already expanded (path-marker extracts).
func RemainingAutoInjectBudget(text string) int {
	left := defaultAutoInjectMaxRunesTotal - CountInjectedAutoExtractRunes(text)
	if left < 0 {
		return 0
	}
	return left
}

// AlreadyAutoExtractedPaths returns path= values from auto_extract begin markers.
// Keys include the raw path, filepath.Clean form, and (on Windows) a lower-cased
// form so GUI path-marker vs attachment path variants still de-dupe.
func AlreadyAutoExtractedPaths(text string) map[string]struct{} {
	out := map[string]struct{}{}
	if text == "" || !strings.Contains(text, AutoExtractBeginMarker) {
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isAutoExtractBeginLine(trimmed) {
			continue
		}
		if p := extractQuotedAttr(trimmed, "path"); p != "" {
			indexExtractedPath(out, p)
		}
	}
	return out
}

func indexExtractedPath(out map[string]struct{}, p string) {
	if p == "" {
		return
	}
	out[p] = struct{}{}
	cleaned := filepath.Clean(p)
	out[cleaned] = struct{}{}
	// Windows paths are case-insensitive; also index slash-normalized forms.
	lower := strings.ToLower(cleaned)
	out[lower] = struct{}{}
	out[strings.ToLower(p)] = struct{}{}
	if strings.Contains(cleaned, "\\") {
		out[strings.ReplaceAll(cleaned, "\\", "/")] = struct{}{}
		out[strings.ToLower(strings.ReplaceAll(cleaned, "\\", "/"))] = struct{}{}
	}
	if strings.Contains(cleaned, "/") {
		out[strings.ReplaceAll(cleaned, "/", "\\")] = struct{}{}
		out[strings.ToLower(strings.ReplaceAll(cleaned, "/", "\\"))] = struct{}{}
	}
}

func formatAutoExtractedDocuments(filePaths []string, perFile, totalBudget int, skipPaths map[string]struct{}) []string {
	return formatAutoExtractedDocumentsWithSettings(filePaths, perFile, totalBudget, skipPaths, currentOfficeReadSettings())
}

func formatAutoExtractedDocumentsWithSettings(filePaths []string, perFile, totalBudget int, skipPaths map[string]struct{}, settings officeReadSettings) []string {
	if perFile <= 0 {
		perFile = defaultAutoInjectMaxRunesPerFile
	}
	if totalBudget < 0 {
		totalBudget = 0
	}
	if len(filePaths) == 0 {
		return nil
	}
	// One output slot per input path (may be "") so callers can zip by index.
	out := make([]string, 0, len(filePaths))
	budget := totalBudget
	// When budget is exhausted, emit a single skip note on the first remaining
	// document instead of N identical blocks (keeps context small).
	budgetExhaustedNoted := false
	for _, p := range filePaths {
		p = strings.TrimSpace(p)
		if p == "" || !IsDocumentFilePath(p) {
			out = append(out, "")
			continue
		}
		if skipPaths != nil && pathInSkipSet(skipPaths, p) {
			out = append(out, "") // already injected earlier this turn
			continue
		}
		if budget <= 0 {
			if !budgetExhaustedNoted {
				out = append(out, fmt.Sprintf(
					"%spath=%q note=%q ---\n自动注入总预算已用尽；后续文档请用 office(action=\"read_document\", file_path=...)。\n%s path=%q ---",
					AutoExtractBeginMarker, p, "skipped: auto-inject total budget exhausted; use office(read_document)",
					AutoExtractEndMarker, p,
				))
				budgetExhaustedNoted = true
			} else {
				out = append(out, "")
			}
			continue
		}
		limit := perFile
		if limit > budget {
			limit = budget
		}
		block, used := formatAutoExtractedDocumentWithSettings(p, limit, settings)
		out = append(out, block)
		if used > 0 {
			budget -= used
		}
	}
	return out
}

func isAutoExtractBeginLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, AutoExtractBeginMarker) && strings.Contains(trimmed, "path=")
}

func hasAutoExtractBegin(text string) bool {
	if !strings.Contains(text, AutoExtractBeginMarker) {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if isAutoExtractBeginLine(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func pathInSkipSet(skip map[string]struct{}, p string) bool {
	if skip == nil || p == "" {
		return false
	}
	if _, ok := skip[p]; ok {
		return true
	}
	cleaned := filepath.Clean(p)
	if _, ok := skip[cleaned]; ok {
		return true
	}
	if _, ok := skip[strings.ToLower(p)]; ok {
		return true
	}
	if _, ok := skip[strings.ToLower(cleaned)]; ok {
		return true
	}
	altSlash := strings.ReplaceAll(cleaned, "\\", "/")
	if _, ok := skip[altSlash]; ok {
		return true
	}
	if _, ok := skip[strings.ToLower(altSlash)]; ok {
		return true
	}
	altBack := strings.ReplaceAll(cleaned, "/", "\\")
	if _, ok := skip[altBack]; ok {
		return true
	}
	if _, ok := skip[strings.ToLower(altBack)]; ok {
		return true
	}
	return false
}

func extractIntAttr(line, key string) int {
	// injected_chars=123 (unquoted int attrs)
	needle := key + "="
	i := strings.Index(line, needle)
	if i < 0 {
		return 0
	}
	rest := line[i+len(needle):]
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// formatAutoExtractedDocument returns (block, injectedRuneCount).
// injectedRuneCount is 0 for soft-error blocks (they do not consume the budget).
func formatAutoExtractedDocument(filePath string, maxRunes int) (string, int) {
	return formatAutoExtractedDocumentWithSettings(filePath, maxRunes, currentOfficeReadSettings())
}

func formatAutoExtractedDocumentWithSettings(filePath string, maxRunes int, settings officeReadSettings) (string, int) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" || !IsDocumentFilePath(filePath) {
		return "", 0
	}
	if maxRunes <= 0 {
		maxRunes = defaultAutoInjectMaxRunesPerFile
	}
	if maxRunes > maxOfficeReadMaxRunes {
		maxRunes = maxOfficeReadMaxRunes
	}

	resolved := resolveOfficeToolPath(filePath)
	info, err := os.Stat(resolved)
	if err != nil {
		// OS errors commonly contain the resolved absolute path. The selected path
		// is already present in the block envelope, so do not echo the error text.
		return autoExtractErrorBlock(filePath, "", "读取失败（error_class=unavailable）"), 0
	}
	if info.IsDir() {
		return autoExtractErrorBlock(filePath, "", "路径是目录"), 0
	}
	if info.Size() > defaultAutoInjectMaxFileBytes {
		return autoExtractPolicyErrorBlock(filePath, "", "input_too_large"), 0
	}

	text, format, err := extractOfficeTextCachedWithSettings(resolved, info, settings)
	// ExtractOfficeText intentionally auto-routes a misnamed OOXML/PDF source
	// for generic document reads. Auto-injection is different: its path is
	// retained in the chat envelope and establishes the attachment's text-like
	// identity. Do not inject a signature-routed Office/PDF payload as .md/.csv
	// (or similar), because that would label binary-origin content incorrectly.
	if err == nil && isPlainTextDocumentFormat(plainFormat(resolved)) && !isPlainTextDocumentFormat(format) {
		text = ""
		err = ErrOfficeReadFormatMismatch
	}
	if err != nil {
		// Plain-text-ish types: fall back to raw read when native parsers reject them.
		// Do not reopen an input already rejected by the shared Office boundary:
		// an encrypted/malformed/oversized container or a reliable format mismatch
		// must never enter chat context merely because it has a text-like suffix.
		if !IsOfficeReadRichContentBlocked(err) && isPlainTextDocumentFormat(plainFormat(resolved)) {
			// A malformed PDF/Office container can fail in its own extractor before
			// that extractor returns text. Its signature is nevertheless reliable,
			// so do not fall back to raw bytes under a text-like extension.
			if sniffed := sniffOfficeFormat(resolved); sniffed != "" && !isPlainTextDocumentFormat(sniffed) {
				err = ErrOfficeReadFormatMismatch
			} else {
				if plain, plainErr := tryReadPlainDocument(resolved, format, info.Size()); plainErr == nil && strings.TrimSpace(plain) != "" {
					text, format, err = plain, plainFormat(resolved), nil
				}
			}
		}
	}
	if err != nil {
		// Extraction errors can embed an absolute path, document metadata, or
		// third-party parser details. Auto-extract logging is rollout telemetry,
		// not a diagnostic dump; keep it safe to collect outside the user turn.
		log.Printf("[auto-extract] native parse failed format=%q error_class=%s", format, autoExtractErrorClass(err))
		errorClass := autoExtractErrorClass(err)
		if guidance, blocked := officeReadBlockedFailureGuidance(errorClass); blocked {
			return autoExtractPolicyErrorBlock(filePath, format, errorClass, guidance), 0
		}
		return autoExtractErrorBlock(filePath, format,
			fmt.Sprintf("原生解析失败（error_class=%s）\n建议: office(action=\"read_document\", file_path=%q) 或 craft_tool 生成解析脚本。", errorClass, filePath)), 0
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return autoExtractErrorBlock(filePath, format, "文件中没有可读取的文本内容"), 0
	}

	runes := []rune(text)
	total := len(runes)
	truncated := false
	nextOffset := -1
	injected := total
	body := text
	if total > maxRunes {
		body = string(runes[:maxRunes])
		injected = maxRunes
		truncated = true
		nextOffset = maxRunes
	}

	var b strings.Builder
	b.Grow(len(body) + 256)
	fmt.Fprintf(&b, "%spath=%q format=%q total_chars=%d injected_chars=%d truncated=%v",
		AutoExtractBeginMarker, filePath, format, total, injected, truncated)
	if truncated {
		fmt.Fprintf(&b, " next_offset=%d", nextOffset)
	}
	b.WriteString(" ---\n")
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n# truncated: true\n# next_offset: %d\n# continue: office(action=\"read_document\", file_path=%q, offset=%d, max_chars=%d)\n",
			nextOffset, filePath, nextOffset, maxRunes)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%spath=%q ---", AutoExtractEndMarker, filePath)
	return b.String(), injected
}

func autoExtractErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errOfficeReadInputTooLarge) {
		return "input_too_large"
	}
	if errors.Is(err, ErrOfficeReadUnsafeContainer) {
		return "malformed"
	}
	return officeReadErrorClass(err, "")
}

func autoExtractErrorBlock(filePath, format, msg string) string {
	short := msg
	// Keep attribute short; full message stays in the body for the model.
	if r := []rune(short); len(r) > 120 {
		short = string(r[:120]) + "…"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%spath=%q", AutoExtractBeginMarker, filePath)
	if format != "" {
		fmt.Fprintf(&b, " format=%q", format)
	}
	fmt.Fprintf(&b, " error=%q ---\n%s\n%spath=%q ---", short, msg, AutoExtractEndMarker, filePath)
	return b.String()
}

// autoExtractPolicyErrorBlock keeps automatic attachment extraction aligned
// with read_document: policy and container-safety failures may not advertise a
// fallback parser for the same rejected input.
func autoExtractPolicyErrorBlock(filePath, format, errorClass string, guidance ...string) string {
	msg := fmt.Sprintf("自动注入已跳过（error_class=%s）。", errorClass)
	if len(guidance) > 0 && guidance[0] != "" {
		msg += "\n" + guidance[0]
	} else if fallback, blocked := officeReadBlockedFailureGuidance(errorClass); blocked {
		msg += "\n" + fallback
	}
	return autoExtractErrorBlock(filePath, format, msg)
}

// tryReadPlainDocument reads small text-like files when office extract fails.
// fileSize is optional (pass info.Size() or -1); used to size the read buffer.
func tryReadPlainDocument(filePath, format string, fileSize int64) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt", ".md", ".markdown", ".json", ".xml", ".yaml", ".yml", ".log", ".csv", ".rtf":
	default:
		switch strings.ToLower(format) {
		case "txt", "md", "markdown", "json", "xml", "yaml", "yml", "log", "csv", "rtf":
		default:
			return "", fmt.Errorf("not plain text")
		}
	}
	// Bound plain read to avoid loading huge logs into memory before truncation.
	const maxPlainBytes = 4 << 20 // 4 MiB raw
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Size buffer from file size when known; still cap at maxPlainBytes+1 to detect overflow.
	alloc := maxPlainBytes + 1
	if fileSize >= 0 && fileSize < int64(alloc) {
		alloc = int(fileSize) + 1
		if alloc < 64 {
			alloc = 64
		}
	}
	buf := make([]byte, alloc)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	data := buf[:n]
	if n > maxPlainBytes {
		data = data[:maxPlainBytes]
	}
	return string(data), nil
}

func plainFormat(filePath string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	if ext == "" {
		return "txt"
	}
	return ext
}

func isPlainTextDocumentFormat(format string) bool {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".") {
	case "txt", "text", "md", "markdown", "json", "xml", "yaml", "yml", "log", "csv", "rtf":
		return true
	default:
		return false
	}
}

// parseSelectedFilePathLines splits the section after FilePathPromptPrefix into
// path lines and the leftover (old frontend instructions / notes).
func parseSelectedFilePathLines(section string) (paths []string, rest string) {
	lines := strings.Split(section, "\n")
	var pathLines []string
	var restLines []string
	seenNonPath := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if !seenNonPath {
			if trimmed == "" {
				// Blank line after paths starts the instruction block.
				if len(pathLines) > 0 {
					seenNonPath = true
				}
				continue
			}
			if isLikelyLocalPathLine(trimmed) {
				pathLines = append(pathLines, trimmed)
				continue
			}
			seenNonPath = true
			restLines = append(restLines, line)
			continue
		}
		restLines = append(restLines, line)
	}
	return pathLines, strings.Join(restLines, "\n")
}

// isLikelyLocalPathLine heuristically detects a file path line (not instruction prose).
func isLikelyLocalPathLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Reject obvious prose / frontend instruction lines.
	lower := strings.ToLower(s)
	for _, prefix := range []string{
		"for ", "prefer ", "use ", "do not ", "don't ", "if ", "when ",
		"documents ", "images ", "please ", "only if ", "请", "对于", "优先",
	} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	// Absolute Unix / UNC / home
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "\\\\") {
		return true
	}
	// Windows drive: C:\... or C:/...
	if len(s) >= 3 {
		r0 := rune(s[0])
		if unicode.IsLetter(r0) && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
			return true
		}
	}
	// Relative-ish path with separator and extension
	if (strings.Contains(s, "/") || strings.Contains(s, "\\")) && filepath.Ext(s) != "" {
		return true
	}
	// Bare filename with a known document/image extension (no spaces)
	ext := strings.ToLower(filepath.Ext(s))
	if ext != "" && (isDocumentExt(ext) || isImageExt(ext)) && !strings.Contains(s, " ") {
		return true
	}
	return false
}

// filterLegacyPathInstructions drops host/frontend tool-call instruction blocks.
// Backend AutoExtractNotice already covers document paging guidance.
// Image-path guidance is normalized to the host's attachment-first policy.
func filterLegacyPathInstructions(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	const imageHint = "For image files, the host sends them directly to a vision-capable model when available. Analyze attached images first; do not re-capture them or use read_file on image bytes. Use OCR only for exact text when needed."
	hasImageHint := strings.Contains(rest, "For image files") ||
		strings.Contains(rest, "do not re-capture via screenshot") ||
		strings.Contains(rest, "do not call screenshot")

	isDocRouting := strings.Contains(rest, "office(action=") ||
		strings.Contains(rest, "office(read_document") ||
		strings.Contains(rest, "read_document") ||
		strings.Contains(rest, "do NOT use read_file") ||
		strings.Contains(rest, "Prefer office") ||
		strings.Contains(rest, "Use these paths directly") ||
		strings.Contains(rest, "请直接使用这些路径") ||
		strings.Contains(rest, "Documents are auto-parsed") ||
		strings.Contains(rest, "auto-parsed by the host")

	if isDocRouting {
		if hasImageHint {
			return imageHint
		}
		return ""
	}
	if hasImageHint {
		// Pure image guidance — keep stable short form.
		return imageHint
	}
	return rest
}

// IsDocumentFilePath reports whether path looks like a document the native
// office extractors (or plain-text fallback) can handle.
func IsDocumentFilePath(filePath string) bool {
	return isDocumentExt(strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))))
}

// IsImageFilePath reports common image extensions.
func IsImageFilePath(filePath string) bool {
	return isImageExt(strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))))
}

func isDocumentExt(ext string) bool {
	switch ext {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx",
		".txt", ".md", ".markdown", ".rtf", ".json", ".xml", ".yaml", ".yml", ".log":
		return true
	default:
		return false
	}
}

func isImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

// CompactQueryForEmbedding removes auto-extracted document bodies so embedding
// warmup / proactive recall keys on user intent + paths, not the full body.
// Safe to call on any user text (no-op when no extract markers).
func CompactQueryForEmbedding(text string) string {
	if text == "" {
		return text
	}
	if strings.Contains(text, AutoExtractBeginMarker) ||
		strings.Contains(text, AutoExtractNotice) ||
		strings.Contains(text, "[系统已自动解析文档正文") {
		return StripAutoExtractBodies(text)
	}
	return text
}

// StripAutoExtractBodies removes injected document bodies from historical user
// turns so multi-turn context does not re-send large extracts. Paths and a short
// placeholder are kept.
func StripAutoExtractBodies(text string) string {
	if text == "" {
		return text
	}
	if !strings.Contains(text, AutoExtractBeginMarker) {
		if strings.Contains(text, AutoExtractNotice) {
			return strings.ReplaceAll(text, AutoExtractNotice, autoExtractHistoryPlaceholder)
		}
		return text
	}

	var b strings.Builder
	b.Grow(len(text) / 4)
	lines := strings.Split(text, "\n")
	inBody := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Require path= so accidental body lines containing the marker prefix are not treated as structure.
		if isAutoExtractBeginLine(trimmed) {
			inBody = true
			path := extractQuotedAttr(trimmed, "path")
			format := extractQuotedAttr(trimmed, "format")
			note := extractQuotedAttr(trimmed, "note")
			errAttr := extractQuotedAttr(trimmed, "error")
			if path != "" {
				summary := fmt.Sprintf("[之前已自动解析文档，正文已省略 path=%q", path)
				if format != "" {
					summary += fmt.Sprintf(" format=%q", format)
				}
				if note != "" {
					summary += fmt.Sprintf(" note=%q", note)
				}
				if errAttr != "" {
					// Keep errors short in history.
					if len([]rune(errAttr)) > 80 {
						errAttr = string([]rune(errAttr)[:80]) + "…"
					}
					summary += fmt.Sprintf(" error=%q", errAttr)
				}
				summary += "]"
				b.WriteString(summary)
				b.WriteByte('\n')
			}
			continue
		}
		if strings.HasPrefix(trimmed, AutoExtractEndMarker) && strings.Contains(trimmed, "path=") {
			inBody = false
			continue
		}
		if inBody {
			continue
		}
		if trimmed == AutoExtractNotice || strings.HasPrefix(trimmed, "[系统已自动解析文档正文") {
			// Drop current and legacy notice variants.
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func extractQuotedAttr(line, key string) string {
	// Prefer key="..." (Go %q style).
	needle := key + "=\""
	i := strings.Index(line, needle)
	if i < 0 {
		return ""
	}
	rest := line[i+len(needle):]
	// Handle Go-quoted escapes: \" inside value.
	var out strings.Builder
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == '\\' && j+1 < len(rest) {
			out.WriteByte(rest[j+1])
			j++
			continue
		}
		if c == '"' {
			return out.String()
		}
		out.WriteByte(c)
	}
	return out.String()
}
