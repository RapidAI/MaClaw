package agent

// tools_office_read.go implements native text extraction for Word/Excel/PPT/PDF
// (both modern OOXML and legacy binary formats) so agent tools do not depend
// on Python, COM, or LibreOffice for ordinary reads.

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pdfinspector"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
	legacydoc "github.com/shakinm/xlsReader/doc"
	"github.com/shakinm/xlsReader/xls"
)

// Default max runes returned by read_document when the caller does not set
// max_chars. Context-aware callers select their own default; this remains a
// conservative compatibility value for direct and legacy calls.
const defaultOfficeReadMaxRunes = 30_000

// Hard upper bound for callers that deliberately request a larger page. The
// model-facing projection still spills it safely when it exceeds its preview
// budget, and the document's offset/max_chars contract remains authoritative.
const maxOfficeReadMaxRunes = 500_000

// OfficeRead extracts an entire Office container before ToolReadDocument
// applies rune paging. Keep the tool path inside the same 32 MiB input bound
// used by automatic attachment extraction, rather than letting a manual tool
// call bypass the migration's memory/latency guard.
const MaxOfficeReadFileBytes int64 = 32 << 20

// officeExtractFileVersion is the identity used by the page cache and its
// in-flight de-duplication. mtime and size make cache inspection cheap to
// understand, while digest is the authoritative content identity. In
// particular, a synchronizer can replace a file in place while preserving its
// size and timestamp; serving a cached page in that case would mix versions
// of the same document in one conversation.
type officeExtractFileVersion struct {
	modTime time.Time
	size    int64
	digest  [sha256.Size]byte
}

// extractCache avoids re-parsing the same file on every offset page. Entries
// are keyed by absolute path and extractor policy, then verified against the
// complete bounded source digest before use.
type officeExtractCacheEntry struct {
	version officeExtractFileVersion
	format  string
	text    string
	loaded  time.Time
}

// officeExtractInFlightKey distinguishes not only the active engine policy but
// also the observed file version. A document can be replaced while another
// caller is reading a page; the later version must never join the earlier
// extraction merely because it has the same path.
type officeExtractInFlightKey struct {
	cacheKey string
	version  officeExtractFileVersion
}

// officeExtractInFlight lets concurrent page reads share one complete parse.
// The full extract is still cached only under the existing size/TTL policy;
// this transient entry exists solely until the leading reader finishes.
type officeExtractInFlight struct {
	done   chan struct{}
	text   string
	format string
	err    error
}

var (
	officeExtractCache     = map[string]officeExtractCacheEntry{}
	officeExtractInFlights = map[officeExtractInFlightKey]*officeExtractInFlight{}
	officeExtractCacheMu   sync.Mutex
)

const officeExtractCacheTTL = 2 * time.Minute
const officeExtractCacheMaxEntries = 16

// Large documents remain readable and pageable, but retaining many whole
// extracts is unnecessary process pressure. Do not cache a result above this
// byte bound; a later offset request will re-extract it instead of pinning it
// in the two-minute cache.
const officeExtractCacheMaxTextBytes = 3 * 1024 * 1024

const officeExtractVersionAttempts = 2

// ToolReadDocument extracts text from Office/PDF and text-based files using
// native readers.
//
// Supported extensions:
//   - Word: .docx, .doc
//   - Excel: .xlsx, .xls, .csv
//   - PowerPoint: .pptx, .ppt (the latter through the staged OfficeRead path)
//   - PDF: .pdf
//   - Text-based data: .txt, .md, .markdown, .json, .xml, .yaml, .yml, .log
//
// Args:
//   - file_path | path: required file path
//   - max_chars: optional rune limit for this chunk (default 30000)
//   - offset: optional rune offset into the full extract (default 0)
//   - line_numbers: optional bool; when true, prefix each line with L1:/L2: markers
func ToolReadDocument(args map[string]interface{}) string {
	return toolReadDocumentWithSettings(args, currentOfficeReadSettings())
}

// ToolReadDocumentWithContext applies the current host's OfficeRead policy
// while deriving the default page size from the active model context.
func ToolReadDocumentWithContext(args map[string]interface{}, contextTokens int) string {
	return toolReadDocumentWithSettingsAndDefault(args, currentOfficeReadSettings(), DocumentReadMaxRunesForContext(contextTokens), DocumentReadToolResultLimit(contextTokens))
}

// ToolReadDocumentWithOfficeReadConfig reads a document under an explicit
// trusted host policy. It is for multi-tenant hosts whose request configuration
// cannot safely be represented by the process-wide desktop provider.
func ToolReadDocumentWithOfficeReadConfig(args map[string]interface{}, config OfficeReadConfig) string {
	return toolReadDocumentWithSettings(args, officeReadSettingsForConfig(config))
}

// ToolReadDocumentWithOfficeReadConfigAndContext keeps the page emitted by
// read_document aligned with the active model's usable context budget. An
// explicit max_chars always wins, so agents can still request smaller focused
// pages or resume from an offset deterministically.
func ToolReadDocumentWithOfficeReadConfigAndContext(args map[string]interface{}, config OfficeReadConfig, contextTokens int) string {
	return toolReadDocumentWithSettingsAndDefault(args, officeReadSettingsForConfig(config), DocumentReadMaxRunesForContext(contextTokens), DocumentReadToolResultLimit(contextTokens))
}

// DocumentReadMaxRunesForContext chooses a CJK-safe page body size from the
// projection budget, leaving 4 KiB for headers and continuation metadata.
func DocumentReadMaxRunesForContext(contextTokens int) int {
	bytes := DocumentReadToolResultLimit(contextTokens) - 4*1024
	if bytes <= 0 {
		return defaultOfficeReadMaxRunes
	}
	runes := bytes / 3
	if runes < defaultOfficeReadMaxRunes {
		return defaultOfficeReadMaxRunes
	}
	if runes > maxOfficeReadMaxRunes {
		return maxOfficeReadMaxRunes
	}
	return runes
}

// officeReadErrorClassMarker introduces the failure class on the first line of
// every unsuccessful read. The formatters below write it and DocumentReadFailure
// reads it, so the two must never name it separately.
const officeReadErrorClassMarker = "error_class="

// DocumentReadFailure reports the failure class named by the stable envelope
// that every unsuccessful read carries, and whether the result is a failure at
// all. A document page returns ("", false).
//
// The envelope exists so a host can tell a failed read from a page without
// reading the prose, but until now each host wrote that judgement itself, and
// most of them simply skipped it. Keeping it next to the formatters that emit
// the envelope is what makes the two sides impossible to drift apart.
func DocumentReadFailure(result string) (string, bool) {
	firstLine, _, _ := strings.Cut(result, "\n")
	marker := strings.Index(firstLine, officeReadErrorClassMarker)
	if marker < 0 {
		return "", false
	}
	class := firstLine[marker+len(officeReadErrorClassMarker):]
	// The class sits inside a parenthesised suffix, so it ends at the first
	// character that cannot belong to a class name.
	if cut := strings.IndexFunc(class, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}); cut >= 0 {
		class = class[:cut]
	}
	if class == "" {
		// The envelope said the read failed even though the class is
		// unreadable; believing the marker over the parse keeps an
		// unrecognised envelope from being served as a document.
		return "unknown", true
	}
	return class, true
}

func toolReadDocumentWithSettings(args map[string]interface{}, settings officeReadSettings) string {
	return toolReadDocumentWithSettingsAndDefault(args, settings, defaultOfficeReadMaxRunes, DocumentReadMaxToolResult)
}

func toolReadDocumentWithSettingsAndDefault(args map[string]interface{}, settings officeReadSettings, defaultMaxRunes, previewLimitBytes int) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	// Prefer absolute paths from the host (GUI already resolves against the
	// session workdir). ResolvePath still handles relative paths for TUI/core.
	filePath = resolveOfficeToolPath(filePath)

	info, err := os.Stat(filePath)
	if err != nil {
		// The caller already supplied this path, but an OS error can add a
		// resolved absolute path, account name, or other host-specific detail.
		// Keep tool failures aligned with auto-injection and rich-content
		// extraction: expose only a stable class to the model.
		return formatOfficeReadUnavailable(filePath)
	}
	if info.IsDir() {
		// Keep every unsuccessful read on the stable error_class envelope. Hosts
		// use it to distinguish a failed tool call from a successful document
		// page, and a directory must never be recorded as a readable document.
		return formatOfficeReadInvalidPath(filePath, "路径是目录，请指定具体文件路径")
	}
	if info.Size() > MaxOfficeReadFileBytes {
		return formatOfficeReadFailure(filePath, strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), "."), errOfficeReadInputTooLarge)
	}

	text, format, err := extractOfficeTextCachedWithSettings(filePath, info, settings)
	if err != nil {
		return formatOfficeReadFailure(filePath, format, err)
	}
	if strings.TrimSpace(text) == "" {
		return formatOfficeReadFailure(filePath, format, fmt.Errorf("文件中没有可读取的文本内容"))
	}

	if defaultMaxRunes <= 0 {
		defaultMaxRunes = defaultOfficeReadMaxRunes
	}
	maxRunes := intArg(args, "max_chars", defaultMaxRunes)
	if maxRunes <= 0 {
		maxRunes = defaultMaxRunes
	}
	if maxRunes > maxOfficeReadMaxRunes {
		maxRunes = maxOfficeReadMaxRunes
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	withLineNumbers := boolArg(args, "line_numbers", false)
	// A numbered line can add up to ten ASCII bytes ("L500000: ") for
	// every source rune. Keep the automatic page safely below the same
	// projection budget as the result envelope; explicit max_chars remains an
	// intentional caller choice and is still protected by spill/read-back.
	if withLineNumbers {
		if _, explicitlySet := args["max_chars"]; !explicitlySet {
			maxRunes = minOfficeReadRunes(maxRunes, documentReadLineNumberedDefaultMaxRunes(previewLimitBytes))
		}
	}

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

func documentReadLineNumberedDefaultMaxRunes(previewLimitBytes int) int {
	const (
		metadataReserve = 4 * 1024
		maxBytesPerRune = 13 // 3 UTF-8 bytes plus a 10-byte LNNNNNN: prefix/newline.
	)
	if previewLimitBytes <= metadataReserve {
		return 1
	}
	return (previewLimitBytes - metadataReserve) / maxBytesPerRune
}

func minOfficeReadRunes(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractOfficeTextCached returns ExtractOfficeText results with a short-lived
// in-process cache so offset paging does not re-parse multi-MB documents each
// time. info is only a caller-side early Stat optimization; cache correctness
// intentionally never trusts it because the file can change after that Stat.
func extractOfficeTextCached(filePath string, info os.FileInfo) (text, format string, err error) {
	return extractOfficeTextCachedWithSettings(filePath, info, currentOfficeReadSettings())
}

func extractOfficeTextCachedWithSettings(filePath string, info os.FileInfo, settings officeReadSettings) (text, format string, err error) {
	_ = info
	// The selected extractor can change at runtime during a staged OfficeRead
	// rollout. Include its stable setting fingerprint so a page generated by
	// one engine is never served after an operator switches engines.
	key := filepath.Clean(filePath) + "|" + officeReadCacheKeySuffixForSettings(settings)
	for attempt := 0; attempt < officeExtractVersionAttempts; attempt++ {
		version, versionErr := officeExtractSourceVersion(filePath)
		if versionErr != nil {
			if errors.Is(versionErr, errOfficeReadSourceChanged) && attempt+1 < officeExtractVersionAttempts {
				continue
			}
			return "", "", versionErr
		}
		text, format, err = extractOfficeTextCachedVersion(filePath, key, version, settings)
		if !errors.Is(err, errOfficeReadSourceChanged) || attempt+1 == officeExtractVersionAttempts {
			return text, format, err
		}
	}
	return "", "", errOfficeReadSourceChanged
}

// officeExtractFileVersion hashes the complete bounded source through one open
// descriptor. Stat before and after prevents a cache identity from being made
// from a file that changed while it was read. The digest, rather than mtime or
// size, is what makes same-size/timestamp-preserving replacement safe.
func officeExtractSourceVersion(filePath string) (officeExtractFileVersion, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return officeExtractFileVersion{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return officeExtractFileVersion{}, err
	}
	if before.IsDir() {
		return officeExtractFileVersion{}, errOfficeReadUnsafeContainer
	}
	if before.Size() > MaxOfficeReadFileBytes {
		return officeExtractFileVersion{}, errOfficeReadInputTooLarge
	}
	hasher := sha256.New()
	n, err := io.Copy(hasher, io.LimitReader(f, MaxOfficeReadFileBytes+1))
	if err != nil {
		return officeExtractFileVersion{}, err
	}
	if n > MaxOfficeReadFileBytes {
		return officeExtractFileVersion{}, errOfficeReadInputTooLarge
	}
	after, err := f.Stat()
	if err != nil {
		return officeExtractFileVersion{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return officeExtractFileVersion{}, errOfficeReadSourceChanged
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return officeExtractFileVersion{modTime: after.ModTime(), size: after.Size(), digest: digest}, nil
}

func extractOfficeTextCachedVersion(filePath, key string, version officeExtractFileVersion, settings officeReadSettings) (text, format string, err error) {
	inFlightKey := officeExtractInFlightKey{cacheKey: key, version: version}

	officeExtractCacheMu.Lock()
	if ent, ok := officeExtractCache[key]; ok {
		if ent.version == version && time.Since(ent.loaded) < officeExtractCacheTTL {
			text, format := ent.text, ent.format
			officeExtractCacheMu.Unlock()
			return text, format, nil
		}
		delete(officeExtractCache, key)
	}
	if inFlight, ok := officeExtractInFlights[inFlightKey]; ok {
		officeExtractCacheMu.Unlock()
		<-inFlight.done
		return inFlight.text, inFlight.format, inFlight.err
	}
	inFlight := &officeExtractInFlight{done: make(chan struct{})}
	officeExtractInFlights[inFlightKey] = inFlight
	officeExtractCacheMu.Unlock()

	// Every waiter must be released, including if a legacy parser unexpectedly
	// panics. The adapter already contains OfficeRead panics; this final guard
	// prevents a future parser integration from converting concurrent paging
	// into a permanent wait and keeps the tool's normal error-class boundary.
	defer func() {
		if recover() != nil {
			text = ""
			format = ""
			err = errors.New("Office document extraction failed")
		}
		if err == nil {
			finalVersion, versionErr := officeExtractSourceVersion(filePath)
			if versionErr != nil {
				err = versionErr
				text = ""
				format = ""
			} else if finalVersion != version {
				err = errOfficeReadSourceChanged
				text = ""
				format = ""
			}
		}
		officeExtractCacheMu.Lock()
		if err == nil && len(text) <= officeExtractCacheMaxTextBytes {
			// Bound memory: drop arbitrary entries when full (soft cap for a small map).
			if len(officeExtractCache) >= officeExtractCacheMaxEntries {
				for cacheKey := range officeExtractCache {
					delete(officeExtractCache, cacheKey)
					if len(officeExtractCache) < officeExtractCacheMaxEntries {
						break
					}
				}
			}
			officeExtractCache[key] = officeExtractCacheEntry{
				version: version,
				format:  format,
				text:    text,
				loaded:  time.Now(),
			}
		}
		inFlight.text = text
		inFlight.format = format
		inFlight.err = err
		delete(officeExtractInFlights, inFlightKey)
		close(inFlight.done)
		officeExtractCacheMu.Unlock()
	}()

	text, format, err = extractOfficeTextWithSettings(filePath, settings)
	if err == nil {
		text = strings.TrimSpace(text)
	}
	return text, format, err
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
	return extractOfficeTextWithSettings(filePath, currentOfficeReadSettings())
}

// ExtractOfficeTextWithOfficeReadConfig extracts using an explicit trusted host
// policy. Environment overrides still apply when the policy is resolved.
func ExtractOfficeTextWithOfficeReadConfig(filePath string, config OfficeReadConfig) (string, string, error) {
	return extractOfficeTextWithSettings(filePath, officeReadSettingsForConfig(config))
}

func extractOfficeTextWithSettings(filePath string, settings officeReadSettings) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	format := strings.TrimPrefix(ext, ".")
	if format == "" {
		format = "unknown"
	}
	// Preserve the established primary-engine oversized-input observation: it
	// must be emitted before taking a private copy, whose own hard cap would
	// otherwise hide the rollout diagnostic. Dual mode remains exempt because
	// it deliberately returns the legacy result without a shadow parse.
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() && info.Size() > maxOfficeReadRichContentBytes &&
		settings.enabledFor(format) && settings.engine == OfficeExtractEngineOfficeRead {
		observeOfficeReadInputTooLarge(filePath, format, info.Size())
		return "", format, errOfficeReadInputTooLarge
	}
	// Do not copy an oversized source merely to discover a reliable PDF/OOXML
	// signature. The size policy has already rejected its body; this bounded
	// four/central-directory probe preserves the returned format identity for
	// callers without authorizing any parser to consume the oversized file.
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() && info.Size() > MaxOfficeReadFileBytes {
		if sniffed := sniffOfficeFormatBounded(filePath); sniffed != "" {
			format = sniffed
		}
		return "", format, errOfficeReadInputTooLarge
	}
	sourcePath, snapshotErr := snapshotBoundedDocumentSourceWithExtension(filePath, ext)
	if snapshotErr != nil {
		return "", format, snapshotErr
	}
	defer func() { _ = os.Remove(sourcePath) }()
	// All format sniffing, container checks, and parsers below operate on one
	// descriptor-derived private copy. Legacy mode is a rollback of extraction
	// semantics, not permission to preflight one pathname and parse a later
	// replacement at that pathname. We take it after the existing bounded
	// oversized-input decisions so their rollout observability remains intact.
	// The legacy engine is a rollback for extraction semantics, not a bypass
	// for container safety. Apply the shared Office input boundary before any
	// extension-led parser can open a known Office container. This also keeps
	// an OfficeRead allowlist change from changing whether encrypted/malformed
	// documents are rejected.
	// Decide a non-container signature before applying the extension-specific
	// Office preflight. This preserves the established behavior for a PDF saved
	// with a legacy Office suffix, while ZIP/OLE inputs remain protected by the
	// shared preflight below before their format can be redirected.
	if sniffed := sniffOfficeFormat(sourcePath); sniffed == "pdf" && sniffed != format {
		format = sniffed
	}
	// OfficeRead reads the complete source, and even its metadata preflight can
	// traverse a substantial ZIP/OLE directory. Enforce the shared 32 MiB
	// boundary before either operation for an enabled OfficeRead primary route.
	// Dual mode is deliberately excluded: its engine-layer size guard skips the
	// shadow read but still returns the legacy result, which is its compatibility
	// contract during rollout.
	sourceOversized := false
	if settings.enabledFor(format) {
		if info, err := os.Stat(sourcePath); err == nil && !info.IsDir() && info.Size() > maxOfficeReadRichContentBytes {
			if settings.engine == OfficeExtractEngineOfficeRead {
				observeOfficeReadInputTooLarge(sourcePath, format, info.Size())
				return "", format, errOfficeReadInputTooLarge
			}
			// Dual mode will make the same size decision in the engine layer:
			// retain legacy compatibility but skip both the shadow parser and an
			// otherwise unnecessary container traversal.
			sourceOversized = true
		}
	}
	// Validate a present ZIP/OLE container before content sniffing opens its
	// directory, but deliberately do so without an expected OOXML family. The
	// generic API owns signature routing: a real PPTX named .docx must route to
	// PPTX rather than being rejected as a mismatch. Explicit-format APIs and
	// structured tools still call the format-aware preflight through their own
	// private snapshots. Once no container is present, retain the declared
	// legacy Office check so arbitrary bytes named .doc/.xls/.ppt remain
	// fail-closed.
	preflightDone := false
	if !preflightDone && !sourceOversized {
		isContainer, err := preflightOfficeReadContainerIfPresent(sourcePath)
		if err != nil {
			// Preserve rollout diagnostics under the declared filename policy.
			// The generic container probe intentionally does not know a family,
			// but an encrypted/malformed source must remain observable as an
			// attempted configured format and must never be sniff-retried.
			if isOfficeReadFormat(format) {
				observeOfficeReadPreflightRejection(sourcePath, format, err)
			}
			return "", format, err
		}
		if isContainer {
			preflightDone = true
		}
	}
	if !preflightDone && !sourceOversized && isOfficeReadFormat(format) {
		if err := PreflightOfficeReadInput(sourcePath, format); err != nil {
			observeOfficeReadPreflightRejection(sourcePath, format, err)
			return "", format, err
		}
		preflightDone = true
	}

	// Prefer a reliable content signature when it identifies a different
	// supported format. This keeps a ZIP-backed DOCX/XLSX/PPTX or PDF from
	// being routed and measured as its misleading filename extension. OLE
	// remains extension-led because its first bytes do not distinguish Word,
	// Excel, and PowerPoint without opening its directory.
	if !sourceOversized {
		if sniffed := sniffOfficeFormat(sourcePath); sniffed != "" && sniffed != format {
			format = sniffed
		}
	}
	// PDF and CSV are not Office ZIP/OLE containers, so they do not pass through
	// the format-aware Office preflight above. Their parsers still retain the
	// complete source in memory, however. Apply the same public extraction
	// boundary after signature routing so a PDF disguised as (for example) a
	// .doc cannot avoid the size check by entering through this exported helper.
	// Keep the Office dual-mode exception above intact: its deliberate legacy
	// compatibility behavior is implemented in the engine layer.
	if err := preflightNonOfficeExtractInput(sourcePath, format); err != nil {
		return "", format, err
	}
	// The snapshot copy was digest-verified above and every route decision has
	// now been made against it, so the engine can keep using these exact bytes.
	preflightDone = isOfficeReadFormat(format) || preflightDone

	text, kind, err := extractOfficeTextWithEngineAfterPreflightWithSettings(sourcePath, format, preflightDone, settings)
	if err == nil {
		return text, kind, nil
	}
	// A shared OfficeRead preflight has already identified this container as
	// malformed or encrypted. Do not let extension sniffing reopen the same
	// bytes through a different legacy parser (for example, a ZIP named .doc
	// that would otherwise retry as .docx). This remains scoped to the explicit
	// safety result: ordinary parser failures retain the compatibility retry.
	if IsOfficeReadContainerSafetyError(err) {
		return "", kind, err
	}
	// Mislabeled extension (e.g. .doc that is actually .docx): sniff once and retry.
	if !sourceOversized {
		if sniffed := sniffOfficeFormat(sourcePath); sniffed != "" && sniffed != format {
			if text2, kind2, err2 := extractOfficeTextWithEngineAfterPreflightWithSettings(sourcePath, sniffed, preflightDone, settings); err2 == nil {
				return text2, kind2, nil
			}
		}
	}
	if format == "unknown" || format == "" {
		return "", format, fmt.Errorf("原生解析暂不支持文件类型 %s（内置支持: .pdf .doc .docx .xls .xlsx .csv .ppt .pptx .txt .md .markdown .json .xml .yaml .yml .log）", ext)
	}
	return "", kind, err
}

// preflightOfficeReadContainerIfPresent checks only enough bytes to decide
// whether the subsequent OOXML/OLE sniffer would open a container. It does not
// classify the document family itself; preflightOfficeReadContainer owns the
// bounded ZIP central-directory or OLE directory validation. Non-container
// input retains the existing extension and signature-routing behavior.
func preflightOfficeReadContainerIfPresent(filePath string) (bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, nil
	}
	header := make([]byte, 4)
	_, readErr := io.ReadFull(f, header)
	closeErr := f.Close()
	if readErr != nil || closeErr != nil {
		return false, nil
	}
	isZIP := header[0] == 'P' && header[1] == 'K' &&
		(header[2] == 3 || header[2] == 5 || header[2] == 7) &&
		(header[3] == 4 || header[3] == 6 || header[3] == 8)
	isOLE := header[0] == 0xd0 && header[1] == 0xcf && header[2] == 0x11 && header[3] == 0xe0
	if !isZIP && !isOLE {
		return false, nil
	}
	return true, officeReadPreflight(filePath, "")
}

// ExtractOfficeTextWithFormat extracts using an explicit format kind (no extension lookup).
func ExtractOfficeTextWithFormat(filePath, format string) (string, string, error) {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	settings := currentOfficeReadSettings()
	// Focused adapter tests install a text-only seam which intentionally does
	// not require a real file. Preserve that contract, while production always
	// snapshots before it preflights or parses a path-based input.
	if officeReadExtract != nil {
		if isOfficeReadFormat(format) && !settings.enabledFor(format) {
			if err := PreflightOfficeReadInput(filePath, format); err != nil {
				observeOfficeReadPreflightRejection(filePath, format, err)
				return "", format, err
			}
		}
		if err := preflightNonOfficeExtractInput(filePath, format); err != nil {
			return "", format, err
		}
		return extractOfficeTextWithEngine(filePath, format)
	}
	// Retain the public entry point's fail-closed source-version decision. In
	// particular, if the source changes while the original preflight traverses
	// it, do not silently continue with whichever version a later copy happens
	// to capture. The actual parser still receives only the separately verified
	// private snapshot below.
	if isOfficeReadFormat(format) {
		if err := PreflightOfficeReadInput(filePath, format); err != nil {
			observeOfficeReadPreflightRejection(filePath, format, err)
			return "", format, err
		}
	}
	if info, statErr := os.Stat(filePath); statErr == nil && !info.IsDir() && info.Size() > maxOfficeReadRichContentBytes && settings.enabledFor(format) {
		if settings.engine == OfficeExtractEngineOfficeRead {
			observeOfficeReadInputTooLarge(filePath, format, info.Size())
			return "", format, errOfficeReadInputTooLarge
		}
		// Preserve dual's compatibility contract: it reports the omitted shadow
		// reader and returns the legacy result without copying an oversized file.
		if settings.engine == OfficeExtractEngineDual {
			return extractOfficeTextWithEngine(filePath, format)
		}
	}
	snapshot, err := snapshotBoundedDocumentSource(filePath)
	if err != nil {
		return "", format, err
	}
	defer func() { _ = os.Remove(snapshot) }()
	// Validate the snapshot, not the mutable user pathname. Otherwise a file
	// can be replaced after a successful preflight but before a legacy parser
	// reopens it. Enabled OfficeRead routes repeat this lightweight check inside
	// the engine today; disabled legacy routes need it here as well. Preserve a
	// source-change detected by the preflight above as a fail-closed result; it
	// proves the caller's pathname changed while the original policy check ran.
	if isOfficeReadFormat(format) {
		if err := PreflightOfficeReadInput(snapshot, format); err != nil {
			observeOfficeReadPreflightRejection(snapshot, format, err)
			return "", format, err
		}
	}
	if err := preflightNonOfficeExtractInput(snapshot, format); err != nil {
		return "", format, err
	}
	// Unlike ExtractOfficeText's generic signature router, this explicit entry
	// is extension-led: a caller that declares CSV must not use it to parse an
	// Office/PDF container renamed as .csv. Keep this aligned with read_excel
	// and knowledge-table imports while avoiding a redundant full CSV extract.
	if format == "csv" {
		if err := ValidateCSVInput(snapshot); err != nil {
			return "", format, err
		}
	}
	// ExtractOfficeTextWithFormat is an explicit-format API. Like
	// ExtractPDFText, its PDF branch must not allow an Office/OLE container
	// renamed as .pdf to reach GoPDF2. Check a present container first so an
	// encrypted or malformed one retains its stable safety identity, then
	// require the PDF signature. ExtractOfficeText deliberately remains the
	// separate signature-routing API for callers that do not declare a format.
	if format == "pdf" {
		if _, err := preflightOfficeReadContainerIfPresent(snapshot); err != nil {
			return "", format, err
		}
		if sniffOfficeFormat(snapshot) != "pdf" {
			return "", format, errOfficeReadFormatMismatch
		}
	}
	// Plain-text formats also have an explicit identity contract here.  The
	// generic ExtractOfficeText entry point is responsible for signature-based
	// routing; this one must not turn a renamed DOCX/XLSX/PPTX/PDF into raw text
	// merely because a caller claimed "txt" or "markdown".  Preflight a
	// present ZIP/OLE container first so encryption and malformed-container
	// outcomes retain their stable, fail-closed identities.
	if isExplicitPlainTextFormat(format) {
		if _, err := preflightOfficeReadContainerIfPresent(snapshot); err != nil {
			return "", format, err
		}
		if sniffOfficeFormat(snapshot) != "" {
			return "", format, errOfficeReadFormatMismatch
		}
	}
	// This exported explicit-format helper has no extension/signature router to
	// correct a caller-supplied Office kind. Do not let a ZIP-backed PPTX/XLSX
	// be parsed under a claimed DOCX kind (or vice versa): the returned format
	// labels cache entries and downstream metadata. The generic ExtractOfficeText
	// entry point deliberately routes such files by their reliable signature;
	// this explicit entry point instead fails closed so its caller can correct
	// the declared kind. OLE remains extension-led because its shared magic
	// bytes cannot safely distinguish Word, Excel, and PowerPoint.
	if isOfficeReadFormat(format) {
		if sniffed := sniffOfficeFormat(snapshot); sniffed != "" && sniffed != format {
			return "", format, errOfficeReadFormatMismatch
		}
	}
	return extractOfficeTextWithEngine(snapshot, format)
}

// preflightNonOfficeExtractInput applies the shared full-source input bound to
// formats handled by ExtractOfficeText that are not Office ZIP/OLE containers.
// The public direct helpers already have equivalent checks for PDF, but the
// extension/signature router must enforce it itself before calling its private
// parser implementation. CSV receives the same boundary because encoding/csv
// materializes the requested table before it can be converted to text.
func preflightNonOfficeExtractInput(filePath, format string) error {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".") {
	case "pdf", "csv":
		return preflightDirectDocumentInput(filePath)
	default:
		return nil
	}
}

func isExplicitPlainTextFormat(format string) bool {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".") {
	case "txt", "text", "md", "markdown", "json", "xml", "yaml", "yml", "log":
		return true
	default:
		return false
	}
}

// extractLegacyOfficeTextWithFormat is the pre-OfficeRead implementation.
// Keep it private and stable so the migration adapter can use it for dual-read
// verification and an immediate format-level fallback.
func extractLegacyOfficeTextWithFormat(filePath, format string) (string, string, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	switch format {
	case "pdf":
		text, err := extractPDFText(filePath)
		return validateLegacyOfficeText(text, "pdf", err)
	case "docx":
		text, err := extractDocxText(filePath)
		return validateLegacyOfficeText(text, "docx", err)
	case "doc":
		text, err := extractDocText(filePath)
		return validateLegacyOfficeText(text, "doc", err)
	case "xlsx", "csv":
		text, err := extractSpreadsheetText(filePath, "")
		return validateLegacyOfficeText(text, format, err)
	case "xls":
		text, err := extractXLSText(filePath)
		return validateLegacyOfficeText(text, "xls", err)
	case "pptx":
		text, err := extractPPTXText(filePath)
		return validateLegacyOfficeText(text, "pptx", err)
	case "txt", "text", "md", "markdown", "json", "xml", "yaml", "yml", "log":
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

func validateLegacyOfficeText(text, format string, err error) (string, string, error) {
	if err != nil {
		return text, format, err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", format, err
	}
	return text, format, nil
}

// recoverLegacyOfficeExtraction keeps every legacy parser behind the same
// fail-closed boundary. Third-party Office/PDF readers handle attacker-
// controlled binary and XML records, and a panic must never crash a GUI worker
// or leave a partially extracted body available to a caller.
func recoverLegacyOfficeExtraction(kind string, text *string, err *error) {
	if recovered := recover(); recovered != nil {
		*text = ""
		*err = fmt.Errorf("%s document parser panicked", kind)
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
	// Use the extension only after the signature itself proves this is an OLE
	// package. A PDF or arbitrary file called .doc/.xls/.ppt must not be pinned
	// to that legacy format before its own signature can be considered.
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

// sniffOfficeFormatBounded is the size-rejection counterpart to
// sniffOfficeFormat. A full OOXML central-directory walk is safe only after
// the 32 MiB source boundary has admitted the file; using zip.OpenReader here
// would let an oversized ZIP force unbounded metadata allocation merely to
// improve an error's format label. PDFs and OLE containers have reliable
// fixed-size magic bytes. Oversized ZIPs intentionally retain their extension
// identity, which is sufficient for the fail-closed input-too-large result.
func sniffOfficeFormatBounded(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()
	var header [8]byte
	n, err := io.ReadFull(f, header[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ""
	}
	if n >= 4 && string(header[:4]) == "%PDF" {
		return "pdf"
	}
	if n == len(header) && header[0] == 0xD0 && header[1] == 0xCF && header[2] == 0x11 && header[3] == 0xE0 {
		ext := strings.ToLower(filepath.Ext(filePath))
		switch ext {
		case ".doc", ".xls", ".ppt":
			return strings.TrimPrefix(ext, ".")
		default:
			return "doc"
		}
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
	// The OfficeRead preflight rejects packages with multiple main document
	// families before parsing. Keep signature routing consistent with that
	// decision: guessing the first family here would direct an ambiguous ZIP
	// to a legacy parser when OfficeRead is disabled for its filename format.
	if (hasWord && hasXL) || (hasWord && hasPPT) || (hasXL && hasPPT) {
		return ""
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

// formatOfficeReadFailure returns a structured failure message. Ordinary
// parser failures get recovery guidance, while policy and container-safety
// failures stay fail-closed instead of suggesting another parser.
func formatOfficeReadFailure(filePath, format string, err error) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if format == "" {
		format = strings.TrimPrefix(ext, ".")
	}
	errorClass := officeReadFailureClass(err)
	var b strings.Builder
	// Parser and filesystem errors may contain source paths, document-derived
	// labels, or implementation details. The tool response becomes model
	// context, so never serialize err itself here. The stable class preserves
	// actionable routing without turning the response into a diagnostics dump.
	b.WriteString(fmt.Sprintf("读取失败（%s%s）\n", officeReadErrorClassMarker, errorClass))
	b.WriteString(fmt.Sprintf("# path: %s\n# format: %s\n", filePath, format))
	if guidance, blocked := officeReadBlockedFailureGuidance(errorClass); blocked {
		// These errors were produced by a boundary that has already rejected this
		// exact input or its version. Do not suggest a generic script, converter,
		// or fallback parser: doing so would defeat the fail-closed or resource
		// policy that protected the normal extraction path.
		b.WriteString("\n" + guidance + "\n")
		return b.String()
	}
	b.WriteString("\n## 下一步（请继续尝试，不要直接告诉用户无法读取）\n")
	b.WriteString("1. **优先 craft_tool** 生成一次性解析脚本并输出纯文本，例如：\n")
	// Quote the whole task argument instead of interpolating the path inside a
	// quoted pseudo-call. A selected filename can legally contain punctuation
	// on supported platforms; it must not be able to terminate task="..." and
	// alter the recovery instruction seen by the model.
	craftTask := fmt.Sprintf("读取本地文件并提取全部可读文本，打印到 stdout。文件路径: %s；扩展名: %s。优先用 Python；若缺依赖可 pip install。不要打开 GUI。", filePath, ext)
	b.WriteString("   craft_tool(task=" + strconv.Quote(craftTask) + ")\n")
	b.WriteString("2. 或 manage_skill(action=\"search\", query=\"文档解析 document parse " + format + "\") 查找/安装解析 Skill，再 manage_skill(action=\"run\", ...)\n")
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

// officeReadBlockedFailureGuidance returns user-safe guidance for errors that
// must not be retried on the same file through a less constrained parser.
func officeReadBlockedFailureGuidance(errorClass string) (string, bool) {
	switch errorClass {
	case "encrypted":
		// Passwords deliberately are not an input to this extraction boundary.
		return "该 Office 文档已加密。当前不支持提供密码后解密或读取；请在受信任的本地 Office 应用中自行解密并另存未加密副本后，再重新上传或读取。", true
	case "malformed":
		return "该文件未通过 Office 容器安全校验，当前不会交给其他解析器重试。请从可信来源重新获取或修复文件后，再重新上传或读取。", true
	case "source_changed":
		return "读取期间检测到源文件已变化。为避免混合不同版本的内容，本次不会继续解析；请确认文件写入完成后重新上传或读取。", true
	case "input_too_large":
		return fmt.Sprintf("文件超过 %d MiB 的读取上限，当前不会交给其他解析器绕过该资源限制。请拆分或缩小文件后重新上传或读取。", MaxOfficeReadFileBytes>>20), true
	case "output_too_large":
		return "可保留的抽取内容超过资源上限，当前不会交给其他解析器绕过该资源限制。请拆分文档或缩小可读内容后重新上传或读取。", true
	default:
		return "", false
	}
}

func formatOfficeReadUnavailable(filePath string) string {
	return fmt.Sprintf("文件不存在或无法访问（%sunavailable）\n# path: %s\n", officeReadErrorClassMarker, filePath)
}

// formatOfficeReadInvalidPath reports a caller path that names something other
// than a document. It deliberately does not use the generic parser-recovery
// guidance: attempting another parser on a directory cannot recover a read and
// would make the model treat a tool validation failure as document content.
func formatOfficeReadInvalidPath(filePath, detail string) string {
	return fmt.Sprintf("读取失败（%sinvalid_path）\n# path: %s\n\n%s\n", officeReadErrorClassMarker, filePath, detail)
}

func officeReadFailureClass(err error) string {
	if err == nil {
		return "extract_error"
	}
	if errors.Is(err, errOfficeReadInputTooLarge) {
		return "input_too_large"
	}
	return officeReadErrorClass(err, "")
}

// ExtractPDFText extracts all page text from a PDF using GoPDF2.
// Prefers page-by-page extraction with ## Page N markers for long/structured docs.
// General application callers must use ExtractOfficeText so file-size and
// format routing remain centralized.
func ExtractPDFText(filePath string) (string, error) {
	snapshot, cleanup, err := SnapshotBoundedDocumentInput(filePath, ".pdf")
	if err != nil {
		return "", err
	}
	defer cleanup()
	// This exported helper has an explicit PDF contract, unlike the generic
	// ExtractOfficeText signature router. Do not let a ZIP/OLE Office package
	// renamed as .pdf reach GoPDF2 through this direct API. Container preflight
	// runs first to preserve encrypted/malformed identities; a genuine PDF has
	// the only signature accepted by this extension-led entry point.
	if _, err := preflightOfficeReadContainerIfPresent(snapshot); err != nil {
		return "", err
	}
	if sniffOfficeFormat(snapshot) != "pdf" {
		return "", errOfficeReadFormatMismatch
	}
	text, err := extractPDFText(snapshot)
	if err != nil {
		return "", err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", err
	}
	return text, nil
}

func extractPDFText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("PDF", &text, &err)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取 PDF 失败: %v", err)
	}

	// Page-by-page is more reliable for long Chinese bid/spec PDFs than a single
	// all-pages dump (better order, recoverable partial failures).
	if numPages, pageErr := gopdf2.GetSourcePDFPageCountFromBytes(data); pageErr == nil && numPages > 0 {
		if err := validatePDFPageCount(numPages); err != nil {
			return "", err
		}
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

	text, err = gopdf2.ExtractAllPagesText(data)
	if err != nil {
		return "", fmt.Errorf("解析 PDF 失败: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("PDF 中没有可读取的文本内容（可能是扫描件，需 OCR）")
	}
	return text, nil
}

// validatePDFPageCount keeps the agent-facing extractor within the same page
// budget as PDF inspection and knowledge import. A small PDF can still encode
// a pathological page tree, so the byte-size input limit alone is insufficient.
func validatePDFPageCount(pageCount int) error {
	if pageCount > pdfinspector.MaxPages {
		return fmt.Errorf("PDF has too many pages (%d; maximum %d)", pageCount, pdfinspector.MaxPages)
	}
	return nil
}

// ExtractDocxText extracts text from a .docx (OOXML) file including body,
// tables, headers and footers. Text is taken from w:t runs only (not arbitrary
// CharData) so field codes / revision markup do not corrupt mid-document text.
// General application callers must use ExtractOfficeText to respect staged
// OfficeRead policy and container preflight.
func ExtractDocxText(filePath string) (string, error) {
	snapshot, cleanup, err := SnapshotOfficeReadInput(filePath, "docx")
	if err != nil {
		return "", err
	}
	defer cleanup()
	text, err := extractDocxText(snapshot)
	if err != nil {
		return "", err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", err
	}
	return text, nil
}

// preflightDirectDocumentInput keeps exported legacy extraction helpers from
// becoming an unbounded bypass around the public Office routing API. PDF does
// not use Office ZIP/OLE rules, so it receives the shared input-size check
// only; Office helpers add their own format-aware container preflight.
func preflightDirectDocumentInput(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return errOfficeReadUnsafeContainer
	}
	if info.Size() > MaxOfficeReadFileBytes {
		return errOfficeReadInputTooLarge
	}
	return nil
}

func extractDocxText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("DOCX", &text, &err)
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
	text = strings.Join(parts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("DOCX 文件中没有可读取的文本内容")
	}
	return text, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	// Legacy DOCX extraction still reads selected XML parts into memory before
	// streaming their text tokens. The shared OOXML preflight limits every part
	// to the same maximum, but direct legacy helpers and future callers must
	// enforce the bound at the actual inflation point as well: ZIP metadata can
	// be forged and an extracted private snapshot is not itself a guarantee
	// about one part's expansion.
	if f == nil || f.UncompressedSize64 > uint64(maxOfficeReadZIPEntryBytes) {
		return nil, errOfficeReadUnsafeContainer
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxOfficeReadZIPEntryBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxOfficeReadZIPEntryBytes {
		return nil, errOfficeReadUnsafeContainer
	}
	return data, nil
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
// General application callers must use ExtractOfficeText to respect staged
// OfficeRead policy and container preflight.
func ExtractDocText(filePath string) (text string, err error) {
	snapshot, cleanup, err := SnapshotOfficeReadInput(filePath, "doc")
	if err != nil {
		return "", err
	}
	defer cleanup()
	text, err = extractDocText(snapshot)
	if err != nil {
		return "", err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", err
	}
	return text, nil
}

func extractDocText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("DOC", &text, &err)
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
// General application callers must use ExtractOfficeText to respect staged
// OfficeRead policy and container preflight.
func ExtractXLSText(filePath string) (text string, err error) {
	snapshot, cleanup, err := SnapshotOfficeReadInput(filePath, "xls")
	if err != nil {
		return "", err
	}
	defer cleanup()
	text, err = extractXLSText(snapshot)
	if err != nil {
		return "", err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", err
	}
	return text, nil
}

func extractXLSText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("XLS", &text, &err)
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
// General application callers must use ExtractOfficeText to respect staged
// OfficeRead policy and container preflight.
func ExtractPPTXText(filePath string) (string, error) {
	snapshot, cleanup, err := SnapshotOfficeReadInput(filePath, "pptx")
	if err != nil {
		return "", err
	}
	defer cleanup()
	text, err := extractPPTXText(snapshot)
	if err != nil {
		return "", err
	}
	if err := validateOfficeReadText(text); err != nil {
		return "", err
	}
	return text, nil
}

func extractPPTXText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("PPTX", &text, &err)
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

func extractSpreadsheetText(filePath, sheet string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("spreadsheet", &text, &err)
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
