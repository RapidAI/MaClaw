package agent

import (
	"archive/zip"
	"encoding/binary"

	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pdfinspector"
)

func TestRecoverLegacyOfficeExtractionFailsClosed(t *testing.T) {
	text := "partial sensitive body"
	var err error
	func() {
		defer recoverLegacyOfficeExtraction("DOCX", &text, &err)
		panic("malformed OOXML")
	}()
	if text != "" {
		t.Fatalf("panic retained partial text: %q", text)
	}
	if err == nil || !strings.Contains(err.Error(), "DOCX document parser panicked") {
		t.Fatalf("panic error = %v", err)
	}
}

func TestPromptCorePrinciplesKeepsOfficeFailClosedClasses(t *testing.T) {
	for _, want := range []string{
		"error_class", "encrypted", "malformed", "source_changed", "input_too_large", "output_too_large",
		"禁止", "密码解密",
	} {
		if !strings.Contains(PromptCorePrinciples, want) {
			t.Fatalf("document prompt is missing %q", want)
		}
	}
}

func TestToolReadDocument_Docx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.docx")
	writeMinimalDOCX(t, path, "Hello MaClaw DOC reader")

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if strings.Contains(out, "读取失败") {
		t.Fatalf("unexpected failure: %s", out)
	}
	if !strings.Contains(out, "Hello MaClaw DOC reader") {
		t.Fatalf("expected extracted text, got: %s", out)
	}
	if !strings.Contains(out, "# format: docx") {
		t.Fatalf("expected format header, got: %s", out)
	}
}

func TestToolReadDocument_PathAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alias.docx")
	writeMinimalDOCX(t, path, "via path alias")

	out := ToolReadDocument(map[string]interface{}{"path": path})
	if !strings.Contains(out, "via path alias") {
		t.Fatalf("path alias failed: %s", out)
	}
}

func TestToolReadDocument_MalformedLegacyPPTFailsClosed(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.ppt")
	if err := os.WriteFile(path, []byte("not a real ppt"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolReadDocument(map[string]interface{}{"path": path})
	if !strings.Contains(out, "读取失败") {
		t.Fatalf("expected failure for .ppt, got: %s", out)
	}
	if !strings.Contains(out, "error_class=malformed") || !strings.Contains(out, "不会交给其他解析器重试") {
		t.Fatalf("expected fail-closed malformed rejection, got: %s", out)
	}
	if strings.Contains(out, "craft_tool") {
		t.Fatalf("malformed Office input must not suggest a recovery parser, got: %s", out)
	}
}

func TestToolReadDocument_InvalidDocFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.doc")
	if err := os.WriteFile(path, []byte("not ole2"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "读取失败") {
		t.Fatalf("expected structured failure, got: %s", out)
	}
	if !strings.Contains(out, "error_class=malformed") || !strings.Contains(out, "不会交给其他解析器重试") {
		t.Fatalf("expected fail-closed malformed rejection, got: %s", out)
	}
	if strings.Contains(out, "craft_tool") {
		t.Fatalf("malformed Office input must not suggest a recovery parser, got: %s", out)
	}
}

func TestToolReadDocument_RedactsParserAndFilesystemErrors(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	path := filepath.Join(t.TempDir(), "selected.docx")
	writeMinimalDOCX(t, path, "safe fixture")
	const sensitiveDetail = `parser detail: C:\\Users\\private\\roadmap.docx`
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "", "docx", errors.New(sensitiveDetail)
	})
	defer restore()

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if strings.Contains(out, sensitiveDetail) || strings.Contains(out, "C:\\Users\\private") {
		t.Fatalf("tool output leaked parser detail:\n%s", out)
	}
	if !strings.Contains(out, "error_class=extract_error") || !strings.Contains(out, path) {
		t.Fatalf("tool output lost stable recovery information:\n%s", out)
	}

	missing := filepath.Join(t.TempDir(), "missing.docx")
	missingOut := ToolReadDocument(map[string]interface{}{"file_path": missing})
	if strings.Contains(missingOut, "The system cannot find") || strings.Contains(strings.ToLower(missingOut), "no such file") {
		t.Fatalf("tool output leaked filesystem detail:\n%s", missingOut)
	}
	if !strings.Contains(missingOut, "error_class=unavailable") || !strings.Contains(missingOut, missing) {
		t.Fatalf("missing-file response lost stable recovery information:\n%s", missingOut)
	}
}

func TestFormatOfficeReadFailure_QuotesRecoveryTaskPath(t *testing.T) {
	// This helper may be reached after a parser failure, so it must be safe even
	// for a supplied path that cannot be created on the current platform.
	path := "C:/selected/quote\" + injected_instruction.docx"
	out := formatOfficeReadFailure(path, "docx", errors.New("parser failed"))
	if strings.Contains(out, `文件路径: C:/selected/quote" + injected_instruction.docx；扩展名`) {
		t.Fatalf("recovery task must not let the supplied quote terminate its argument:\n%s", out)
	}
	if !strings.Contains(out, `task="读取本地文件并提取全部可读文本，打印到 stdout。文件路径: C:/selected/quote\" + injected_instruction.docx；扩展名: .docx。优先用 Python；若缺依赖可 pip install。不要打开 GUI。"`) {
		t.Fatalf("recovery task did not safely quote the supplied path:\n%s", out)
	}
}

func TestFormatOfficeReadFailure_EncryptedDoesNotSuggestBypass(t *testing.T) {
	path := "C:/selected/encrypted.docx"
	out := formatOfficeReadFailure(path, "docx", ErrOfficeReadEncryptedContainer)
	for _, want := range []string{"error_class=encrypted", "当前不支持提供密码后解密或读取", path} {
		if !strings.Contains(out, want) {
			t.Fatalf("encrypted failure missing %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice", "密码输入"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("encrypted failure suggested a bypass %q:\n%s", forbidden, out)
		}
	}
}

func TestFormatOfficeReadFailure_BlockedClassesDoNotSuggestBypass(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "encrypted", err: ErrOfficeReadEncryptedContainer, want: "不支持提供密码后解密或读取"},
		{name: "malformed", err: ErrOfficeReadUnsafeContainer, want: "未通过 Office 容器安全校验"},
		{name: "source changed", err: ErrOfficeReadSourceChanged, want: "源文件已变化"},
		{name: "input too large", err: ErrOfficeReadInputTooLarge, want: "超过 32 MiB 的读取上限"},
		{name: "output too large", err: ErrOfficeReadOutputTooLarge, want: "抽取内容超过资源上限"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := formatOfficeReadFailure("C:/selected/input.docx", "docx", test.err)
			if !strings.Contains(out, test.want) {
				t.Fatalf("blocked failure missing %q:\n%s", test.want, out)
			}
			for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice", "密码输入"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("blocked failure suggested a bypass %q:\n%s", forbidden, out)
				}
			}
		})
	}
}

func TestToolReadDocument_MaxCharsFloatAndInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.docx")
	writeMinimalDOCX(t, path, strings.Repeat("字", 100))

	for _, args := range []map[string]interface{}{
		{"file_path": path, "max_chars": float64(10)},
		{"file_path": path, "max_chars": 10},
	} {
		out := ToolReadDocument(args)
		if !strings.Contains(out, "truncated: true") {
			t.Fatalf("expected truncation marker for %#v, got: %s", args["max_chars"], out)
		}
		if !strings.Contains(out, "next_offset:") {
			t.Fatalf("expected next_offset for paging, got: %s", out)
		}
	}
}

func TestToolReadDocument_OffsetPaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.docx")
	// 30 chars total
	writeMinimalDOCX(t, path, strings.Repeat("A", 30))
	part1 := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 10,
		"offset":    0,
	})
	if !strings.Contains(part1, "next_offset: 10") {
		t.Fatalf("part1 missing next_offset: %s", part1)
	}
	part2 := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 10,
		"offset":    10,
	})
	if !strings.Contains(part2, "# offset: 10") {
		t.Fatalf("part2 missing offset header: %s", part2)
	}
	// Reconstruct: both chunks should cover full text without inventing content.
	if !strings.Contains(part1, "AAAAAAAAAA") || !strings.Contains(part2, "AAAAAAAAAA") {
		t.Fatalf("unexpected chunk content\npart1=%s\npart2=%s", part1, part2)
	}
	// EOF paging
	end := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"offset":    30,
	})
	if !strings.Contains(end, "已到文档末尾") {
		t.Fatalf("expected EOF message, got: %s", end)
	}
}

func TestToolReadDocument_CacheHelpsOffsetPaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.docx")
	writeMinimalDOCX(t, path, strings.Repeat("Z", 50))
	// First call populates cache; second offset page should still succeed.
	_ = ToolReadDocument(map[string]interface{}{"file_path": path, "max_chars": 20, "offset": 0})
	out := ToolReadDocument(map[string]interface{}{"file_path": path, "max_chars": 20, "offset": 20})
	if strings.Contains(out, "读取失败") || strings.Contains(out, "文件不存在") {
		t.Fatalf("cached page read failed: %s", out)
	}
	if !strings.Contains(out, "# offset: 20") {
		t.Fatalf("expected offset header, got: %s", out)
	}
}

func TestToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "format-rollback.docx")
	writeMinimalDOCX(t, path, "legacy rollback body")
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "OfficeRead staged body", "docx", nil
	})
	defer restoreExtract()

	staged := ToolReadDocument(map[string]interface{}{"file_path": path})
	if strings.Contains(staged, "读取失败") || !strings.Contains(staged, "OfficeRead staged body") || strings.Contains(staged, "legacy rollback body") {
		t.Fatalf("OfficeRead staged result = %s", staged)
	}

	// Remove only .docx from the allowlist. This is the operational rollback
	// path required by the migration plan; it must take effect immediately even
	// while the two-minute paging cache still contains an OfficeRead result.
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "ppt")
	rolledBack := ToolReadDocument(map[string]interface{}{"file_path": path})
	if strings.Contains(rolledBack, "读取失败") || !strings.Contains(rolledBack, "legacy rollback body") || strings.Contains(rolledBack, "OfficeRead staged body") {
		t.Fatalf("format-level rollback served stale cached OfficeRead text: %s", rolledBack)
	}
}

func TestToolReadDocument_ConcurrentPagingStaysWithinContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-pages.docx")
	writeMinimalDOCX(t, path, strings.Repeat("并发分页 ", 200))

	const readers = 12
	var wg sync.WaitGroup
	errs := make(chan string, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			out := ToolReadDocument(map[string]interface{}{"file_path": path, "offset": offset, "max_chars": 40})
			if strings.Contains(out, "读取失败") || !strings.Contains(out, "# format: docx") || !strings.Contains(out, "# offset:") {
				errs <- out
			}
		}(i * 10)
	}
	wg.Wait()
	close(errs)
	for out := range errs {
		t.Fatalf("concurrent paging violated tool contract: %s", out)
	}
}

func TestExtractOfficeTextCached_ConcurrentPagesShareOneExtraction(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "shared-concurrent-pages.docx")
	writeMinimalDOCX(t, path, "cache source")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	previous := officeReadExtract
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	officeReadExtract = func(string) (string, string, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return "shared OfficeRead paging body", "docx", nil
	}
	defer func() { officeReadExtract = previous }()

	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractInFlights = make(map[officeExtractInFlightKey]*officeExtractInFlight)
	officeExtractCacheMu.Unlock()

	const readers = 12
	type extractResult struct {
		text, format string
		err          error
	}
	results := make(chan extractResult, readers)
	for range readers {
		go func() {
			text, format, err := extractOfficeTextCached(path, info)
			results <- extractResult{text: text, format: format, err: err}
		}()
	}
	<-started
	// Give all callers an opportunity to encounter the transient in-flight
	// entry before allowing the lead parse to finish. A timeout keeps this
	// regression test from masking a deadlock with the same symptom it guards.
	deadline := time.NewTimer(2 * time.Second)
	for {
		officeExtractCacheMu.Lock()
		waitersJoined := len(officeExtractInFlights) == 1
		officeExtractCacheMu.Unlock()
		if waitersJoined {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("concurrent callers did not join the shared extraction")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("in-flight cache parsed %d times before release, want 1", got)
	}
	close(release)
	for range readers {
		result := <-results
		if result.err != nil || result.text != "shared OfficeRead paging body" || result.format != "docx" {
			t.Fatalf("shared extraction result = %#v", result)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent readers parsed %d times, want 1", got)
	}
	officeExtractCacheMu.Lock()
	defer officeExtractCacheMu.Unlock()
	if len(officeExtractInFlights) != 0 {
		t.Fatalf("in-flight entries retained after completion: %d", len(officeExtractInFlights))
	}
}

func TestExtractOfficeTextCached_PanicReleasesConcurrentWaiters(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "panic-concurrent-pages.docx")
	writeMinimalDOCX(t, path, "cache source")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	previous := officeReadExtract
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	officeReadExtract = func(string) (string, string, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		panic("test-only legacy parser panic")
	}
	defer func() { officeReadExtract = previous }()

	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractInFlights = make(map[officeExtractInFlightKey]*officeExtractInFlight)
	officeExtractCacheMu.Unlock()

	const readers = 2
	results := make(chan error, readers)
	for range readers {
		go func() {
			_, _, err := extractOfficeTextCached(path, info)
			results <- err
		}()
	}
	<-started
	close(release)
	for range readers {
		select {
		case err := <-results:
			if err == nil || !strings.Contains(err.Error(), "Office document extraction failed") {
				t.Fatalf("panic recovery error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("panic left a concurrent paging caller blocked")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("panic path parsed %d times, want 1", got)
	}
	officeExtractCacheMu.Lock()
	defer officeExtractCacheMu.Unlock()
	if len(officeExtractInFlights) != 0 || len(officeExtractCache) != 0 {
		t.Fatalf("panic retained cache state: inflight=%d cache=%d", len(officeExtractInFlights), len(officeExtractCache))
	}
}

func TestToolReadDocument_MaxCharsCapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cap.docx")
	writeMinimalDOCX(t, path, strings.Repeat("X", 100))
	// Absurd max_chars must be clamped; still returns content.
	out := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 50_000_000,
	})
	if strings.Contains(out, "读取失败") {
		t.Fatalf("unexpected failure: %s", out)
	}
	if !strings.Contains(out, "XXXXXXXXXX") {
		t.Fatalf("missing content: %s", out)
	}
}

func TestToolReadDocument_DefaultPageFitsDocumentPreviewBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default-page.json")
	text := strings.Repeat("文", defaultOfficeReadMaxRunes+1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "# truncated: true") || !strings.Contains(out, "# next_offset: ") {
		t.Fatalf("default document page was not bounded: %q", out)
	}
	projection, err := ProjectToolResult("read_document", "", out)
	if err != nil {
		t.Fatalf("ProjectToolResult() error = %v", err)
	}
	if projection.Spilled || projection.Handle != nil {
		t.Fatalf("default document page unexpectedly spilled: %+v", projection)
	}
	if projection.Preview != out {
		t.Fatalf("default document page was unexpectedly truncated: got %d bytes, want %d", len(projection.Preview), len(out))
	}
}

func TestDocumentReadMaxRunesForContextScalesWithProjectionBudget(t *testing.T) {
	if got := DocumentReadMaxRunesForContext(200_000); got != 40_301 {
		t.Fatalf("200K context max runes = %d, want 40301", got)
	}
	if got := DocumentReadMaxRunesForContext(400_000); got != 81_968 {
		t.Fatalf("400K context max runes = %d, want 81968", got)
	}
	// Small windows: the page must stay within the projection budget instead
	// of being raised to the legacy 30k floor, which would exceed the budget
	// and get its middle cut by head/tail truncation.
	if got := DocumentReadMaxRunesForContext(8_000); got != 9_557 {
		t.Fatalf("8K context max runes = %d, want 9557", got)
	}
	for _, ctx := range []int{8_000, 32_000, 100_000, 200_000, 400_000} {
		runes := DocumentReadMaxRunesForContext(ctx)
		limit := DocumentReadToolResultLimit(ctx)
		if bytes := runes*3 + 4*1024; bytes > limit {
			t.Fatalf("context %d: page %d runes needs %d bytes, exceeds projection budget %d", ctx, runes, bytes, limit)
		}
	}
}

func TestToolReadDocumentWithContextLineNumbersFitsProjectionBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbered-lines.json")
	// Each source rune is its own line. This maximizes the LNNN: expansion and
	// verifies that the automatic context-sized page still stays inline.
	text := strings.Repeat("\u4e2d\n", DocumentReadMaxRunesForContext(400_000)+1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	out := ToolReadDocumentWithContext(map[string]interface{}{
		"file_path":    path,
		"line_numbers": true,
	}, 400_000)
	if !strings.Contains(out, "# truncated: true") || !strings.Contains(out, "# next_offset: ") {
		t.Fatalf("numbered document page was not bounded: %q", out)
	}
	projection, err := ProjectToolResultWithContext("read_document", "", out, 400_000)
	if err != nil {
		t.Fatalf("ProjectToolResultWithContext() error = %v", err)
	}
	if projection.Spilled || projection.Handle != nil {
		t.Fatalf("automatic numbered page unexpectedly spilled: %+v", projection)
	}
	if projection.Preview != out {
		t.Fatalf("automatic numbered page was unexpectedly truncated: got %d bytes, want %d", len(projection.Preview), len(out))
	}
}

func TestExtractOfficeTextCached_DoesNotRetainLargeResult(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "large-cache.docx")
	writeMinimalDOCX(t, path, "cache source")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	previous := officeReadExtract
	calls := 0
	largeText := strings.Repeat("😀", maxOfficeReadTextRunes)
	if len(largeText) <= officeExtractCacheMaxTextBytes {
		t.Fatalf("test fixture must exceed cache byte limit: %d", len(largeText))
	}
	officeReadExtract = func(string) (string, string, error) {
		calls++
		return largeText, "docx", nil
	}
	defer func() { officeReadExtract = previous }()

	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractCacheMu.Unlock()
	// The cache key deliberately includes the dynamic OfficeRead policy. Build
	// it only after setting the test environment and clearing prior entries.
	for range 2 {
		text, _, err := extractOfficeTextCached(path, info)
		if err != nil || len(text) != len(largeText) {
			t.Fatalf("large extract text=%d err=%v", len(text), err)
		}
	}
	if calls != 2 {
		t.Fatalf("large result was retained in cache: calls=%d", calls)
	}
	officeExtractCacheMu.Lock()
	defer officeExtractCacheMu.Unlock()
	if len(officeExtractCache) != 0 {
		t.Fatalf("large result cache entries=%d", len(officeExtractCache))
	}
}

func TestExtractOfficeTextCached_SameSizeAndTimestampReplacementDoesNotServeStaleText(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "same-version-metadata.docx")
	writeMinimalDOCX(t, path, "AAAAAAAA")
	padTo := func(target int) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > target {
			t.Fatalf("fixture is larger than padding target: %d", len(data))
		}
		data = append(data, make([]byte, target-len(data))...)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The ZIP reader accepts trailing bytes. Padding makes the content change
	// independent from DEFLATE's output length, so this test genuinely models
	// an equal-size replacement instead of relying on compressor coincidence.
	padTo(4096)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	previous := officeReadExtract
	var calls atomic.Int32
	var sourceGeneration atomic.Int32
	officeReadExtract = func(string) (string, string, error) {
		calls.Add(1)
		if sourceGeneration.Load() == 0 {
			return "old extracted body", "docx", nil
		}
		return "new extracted body", "docx", nil
	}
	defer func() { officeReadExtract = previous }()

	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractInFlights = make(map[officeExtractInFlightKey]*officeExtractInFlight)
	officeExtractCacheMu.Unlock()

	text, _, err := extractOfficeTextCached(path, before)
	if err != nil || text != "old extracted body" {
		t.Fatalf("first extraction = %q, %v", text, err)
	}

	// Simulate an editor/sync client replacing the file but preserving the two
	// metadata values the old cache identity used. Both payloads deliberately
	// have the same length; fail loudly if ZIP compression happens to make the
	// fixture unsuitable for this regression.
	writeMinimalDOCX(t, path, "BBBBBBBB")
	padTo(4096)
	sourceGeneration.Store(1)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("fixture changed size: before=%d after=%d", before.Size(), after.Size())
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("fixture did not preserve old cache metadata: before=%v/%d after=%v/%d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}

	text, _, err = extractOfficeTextCached(path, before) // deliberately stale Stat
	if err != nil || text != "new extracted body" {
		t.Fatalf("same-size/timestamp replacement served stale text = %q, %v", text, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("replacement reused cached extraction: calls=%d, want 2", got)
	}
}

func TestExtractOfficeTextCached_FileReplacementDoesNotJoinOldInFlightExtraction(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "inflight-version-change.docx")
	writeMinimalDOCX(t, path, "AAAAAAAA")
	padTo := func(target int) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, make([]byte, target-len(data))...)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	padTo(4096)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	previous := officeReadExtract
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	officeReadExtract = func(string) (string, string, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-releaseFirst
			return "old in-flight body", "docx", nil
		case 2:
			close(secondStarted)
			return "new replacement body", "docx", nil
		default:
			return "new replacement body", "docx", nil
		}
	}
	defer func() { officeReadExtract = previous }()

	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractInFlights = make(map[officeExtractInFlightKey]*officeExtractInFlight)
	officeExtractCacheMu.Unlock()

	firstResult := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, _, err := extractOfficeTextCached(path, before)
		firstResult <- struct {
			text string
			err  error
		}{text, err}
	}()
	<-firstStarted

	writeMinimalDOCX(t, path, "BBBBBBBB")
	padTo(4096)
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, _, err := extractOfficeTextCached(path, before) // stale caller Stat must not matter
		secondResult <- struct {
			text string
			err  error
		}{text, err}
	}()
	select {
	case <-secondStarted:
		// A different digest must start a new extraction instead of joining the
		// old in-flight entry keyed solely by the preserved metadata.
	case <-time.After(2 * time.Second):
		t.Fatal("replacement joined the old in-flight extraction")
	}
	close(releaseFirst)
	for _, resultCh := range []<-chan struct {
		text string
		err  error
	}{firstResult, secondResult} {
		select {
		case result := <-resultCh:
			if result.err != nil || result.text != "new replacement body" {
				t.Fatalf("replacement result = %q, %v", result.text, result.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("replacement extraction did not complete")
		}
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("replacement did not start a distinct extraction: calls=%d", got)
	}
}

func TestToolReadDocument_RejectsOversizedInputBeforeExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "error_class=input_too_large") || !strings.Contains(out, "32 MiB") {
		t.Fatalf("missing oversized-input boundary: %s", out)
	}
	for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("oversized document response suggested a resource-limit bypass %q: %s", forbidden, out)
		}
	}
}

func TestToolReadDocument_LineNumbersContinueAcrossOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.docx")
	// Three lines
	writeMinimalDOCXMultiPara(t, path, []string{"alpha", "beta", "gamma"})
	// First chunk: enough for "alpha\nbe" roughly — use large max to get first two lines then page.
	full := ToolReadDocument(map[string]interface{}{
		"file_path":    path,
		"line_numbers": true,
	})
	if !strings.Contains(full, "L1: alpha") || !strings.Contains(full, "L2: beta") {
		t.Fatalf("expected absolute line numbers in full read: %s", full)
	}
	// Offset past "alpha\n" (6 runes) should start numbering at L2
	part := ToolReadDocument(map[string]interface{}{
		"file_path":    path,
		"offset":       6,
		"line_numbers": true,
	})
	if !strings.Contains(part, "L2:") {
		t.Fatalf("expected line numbers to continue at L2 after offset, got: %s", part)
	}
	if strings.Contains(part, "L1:") {
		t.Fatalf("should not restart at L1 after offset: %s", part)
	}
}

func writeMinimalDOCXMultiPara(t *testing.T, path string, paras []string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(p)
		body.WriteString(`<w:p><w:r><w:t>`)
		body.WriteString(esc)
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	body.WriteString(`</w:body></w:document>`)
	_, _ = w.Write([]byte(body.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractDocxText_TableCells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.docx")
	// Minimal docx with a 1x2 table
	writeMinimalDOCXTable(t, path, [][]string{{"评分标准", "分值"}})
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "评分标准") || !strings.Contains(text, "分值") {
		t.Fatalf("table cells missing: %q", text)
	}
	if !strings.Contains(text, "\t") {
		t.Fatalf("expected tab-separated cells, got %q", text)
	}
}

func TestExtractDocxText_NestedTableKeepsOuterCells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.docx")
	// Outer row: [outer-left | nested-table-cell]; nested has one cell "inner"
	writeMinimalDOCXNestedTable(t, path)
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "outer-left") {
		t.Fatalf("outer cell lost: %q", text)
	}
	if !strings.Contains(text, "inner") {
		t.Fatalf("nested cell lost: %q", text)
	}
	// Outer structure should still produce a tab-separated row containing outer-left.
	if !strings.Contains(text, "outer-left\t") && !strings.Contains(text, "\touter-left") {
		// nested content may be appended inside the second cell; require outer-left present with a tab somewhere on its line.
		found := false
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "outer-left") && strings.Contains(line, "\t") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected outer row tab structure, got %q", text)
		}
	}
}

func writeMinimalDOCXNestedTable(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	// Outer table 1x2; second cell contains nested 1x1 table.
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>` +
		`<w:tr>` +
		`<w:tc><w:p><w:r><w:t>outer-left</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tbl><w:tr><w:tc><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:tc>` +
		`</w:tr>` +
		`</w:tbl></w:body></w:document>`
	_, _ = w.Write([]byte(xmlBody))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalDOCXTable(t *testing.T, path string, rows [][]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>`)
	for _, row := range rows {
		body.WriteString(`<w:tr>`)
		for _, cell := range row {
			esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(cell)
			body.WriteString(`<w:tc><w:p><w:r><w:t>`)
			body.WriteString(esc)
			body.WriteString(`</w:t></w:r></w:p></w:tc>`)
		}
		body.WriteString(`</w:tr>`)
	}
	body.WriteString(`</w:tbl></w:body></w:document>`)
	_, _ = w.Write([]byte(body.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolReadPPTX_LegacyPPTFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.ppt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ToolReadPPTX(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "error_class=malformed") {
		t.Fatalf("legacy PPT must be rejected by PPTX-only structured reader: %s", out)
	}
	for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("legacy PPT failure offered bypass %q: %s", forbidden, out)
		}
	}
}

// Structured readers are deliberately format-specific.  They must not turn a
// correctly formed document of another Office family into a generic parser
// failure that suggests reopening it with an unconstrained recovery tool.
func TestStructuredOfficeToolsRejectUnsupportedExtensionsBeforeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		write    func(*testing.T, string)
		read     func(map[string]interface{}) string
	}{
		{
			name:     "excel-gets-pptx-suffix",
			filename: "deck.pptx",
			write: func(t *testing.T, path string) {
				writeMinimalOfficeReadOOXMLFixture(t, path, "ppt/presentation.xml", "presentation body")
			},
			read: ToolReadExcel,
		},
		{
			name:     "pptx-gets-xlsx-suffix",
			filename: "book.xlsx",
			write:    func(t *testing.T, path string) { writeStructuredOfficeTestXLSX(t, path, "workbook body") },
			read:     ToolReadPPTX,
		},
		{
			name:     "pptx-gets-legacy-ppt",
			filename: "legacy.ppt",
			write:    func(t *testing.T, path string) { writeMinimalOLE(t, path, "PowerPoint Document") },
			read:     ToolReadPPTX,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			tc.write(t, path)
			out := tc.read(map[string]interface{}{"file_path": path})
			if !strings.Contains(out, "error_class=malformed") {
				t.Fatalf("unsupported structured reader suffix must fail closed: %s", out)
			}
			for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("unsupported structured reader suffix offered bypass %q: %s", forbidden, out)
				}
			}
		})
	}
}

func TestToolReadExcel_MissingFile(t *testing.T) {
	out := ToolReadExcel(map[string]interface{}{"file_path": filepath.Join(t.TempDir(), "nope.xlsx")})
	if !strings.Contains(out, "文件不存在") {
		t.Fatalf("expected missing file message, got: %s", out)
	}
}

func TestStructuredOfficeToolsRejectEncryptedContainersBeforeParsing(t *testing.T) {
	for _, tc := range []struct {
		name string
		ext  string
		read func(map[string]interface{}) string
	}{
		{name: "excel", ext: ".xlsx", read: ToolReadExcel},
		{name: "pptx", ext: ".pptx", read: ToolReadPPTX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encrypted"+tc.ext)
			writeEncryptedOfficeReadZIP(t, path)
			out := tc.read(map[string]interface{}{"file_path": path})
			if !strings.Contains(out, "error_class=encrypted") {
				t.Fatalf("encrypted structured Office input must fail before parser: %s", out)
			}
		})
	}
}

// Structured tools select a document family themselves, unlike
// read_document's signature router.  A renamed OOXML package must therefore
// be rejected before its format-specific parser opens the private snapshot.
func TestStructuredOfficeToolsRejectMismatchedOOXMLFamily(t *testing.T) {
	for _, tc := range []struct {
		name         string
		filename     string
		documentPart string
		read         func(map[string]interface{}) string
	}{
		{name: "excel-gets-presentation", filename: "deck.xlsx", documentPart: "ppt/presentation.xml", read: ToolReadExcel},
		{name: "pptx-gets-workbook", filename: "book.pptx", documentPart: "xl/workbook.xml", read: ToolReadPPTX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			writeMinimalOfficeReadOOXMLFixture(t, path, tc.documentPart, "wrong OOXML family")
			out := tc.read(map[string]interface{}{"file_path": path})
			if !strings.Contains(out, "error_class=malformed") {
				t.Fatalf("mismatched structured Office input must stop before parsing: %s", out)
			}
		})
	}
}

// Passwords are deliberately not part of the Office extraction contract.  An
// arbitrary caller can still include one in its tool argument map, so verify
// that every read entry point keeps the encrypted container fail-closed rather
// than forwarding, logging, or otherwise using it.
func TestOfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided(t *testing.T) {
	const suppliedPassword = "must-not-be-used-or-echoed"
	for _, tc := range []struct {
		name string
		ext  string
		read func(map[string]interface{}) string
	}{
		{name: "document", ext: ".docx", read: ToolReadDocument},
		{name: "excel", ext: ".xlsx", read: ToolReadExcel},
		{name: "pptx", ext: ".pptx", read: ToolReadPPTX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encrypted"+tc.ext)
			writeEncryptedOfficeReadZIP(t, path)
			out := tc.read(map[string]interface{}{
				"file_path": path,
				"password":  suppliedPassword,
			})
			if !strings.Contains(out, "error_class=encrypted") {
				t.Fatalf("encrypted Office input with supplied password must fail closed: %s", out)
			}
			if strings.Contains(out, suppliedPassword) {
				t.Fatalf("Office failure leaked supplied password: %s", out)
			}
		})
	}
}

// read_document is the common user-facing path for all six Office formats.
// Keep its password boundary independent of the specialised Excel/PPTX tools:
// the supplied password must neither reach a parser nor appear in model
// context, whether encryption is signalled by OOXML metadata or a legacy OLE
// stream.
func TestToolReadDocumentRejectsEncryptedSixOfficeFormatsEvenWhenPasswordIsProvided(t *testing.T) {
	const suppliedPassword = "must-not-be-used-or-echoed-six-formats"
	for _, tc := range []struct {
		format string
		write  func(*testing.T, string)
	}{
		{format: "doc", write: func(t *testing.T, path string) {
			encryptedFIB := make([]byte, 32)
			binary.LittleEndian.PutUint16(encryptedFIB[0:2], 0xa5ec)
			binary.LittleEndian.PutUint16(encryptedFIB[10:12], 0x0100)
			writeOLEWithStream(t, path, "WordDocument", encryptedFIB)
		}},
		{format: "docx", write: writeEncryptedOfficeReadZIP},
		{format: "ppt", write: func(t *testing.T, path string) {
			writeMinimalOLE(t, path, "EncryptedSummary")
		}},
		{format: "pptx", write: writeEncryptedOfficeReadZIP},
		{format: "xls", write: func(t *testing.T, path string) {
			writeOLEWithWorkbook(t, path, []byte{0x09, 0x08, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00})
		}},
		{format: "xlsx", write: writeEncryptedOfficeReadZIP},
	} {
		t.Run(tc.format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encrypted."+tc.format)
			tc.write(t, path)
			out := ToolReadDocument(map[string]interface{}{
				"file_path": path,
				"password":  suppliedPassword,
			})
			if !strings.Contains(out, "error_class=encrypted") {
				t.Fatalf("encrypted %s input with supplied password must fail closed: %s", tc.format, out)
			}
			if strings.Contains(out, suppliedPassword) {
				t.Fatalf("encrypted %s failure leaked supplied password: %s", tc.format, out)
			}
		})
	}
}
func TestToolReadExcelRejectsOversizedInputBeforeParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.xlsx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	out := ToolReadExcel(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "error_class=input_too_large") {
		t.Fatalf("oversized structured Excel input must fail before parser: %s", out)
	}
}

func TestToolReadExcelRejectsOversizedCSVBeforeParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	out := ToolReadExcel(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "error_class=input_too_large") {
		t.Fatalf("oversized CSV input must fail before full-grid parsing: %s", out)
	}
}

// CSV has no Office container grammar, but it must not become a bypass for a
// ZIP/OLE/PDF payload merely renamed with a .csv suffix. The structured parser
// receives only the private snapshot after its lightweight safety probe has
// established that this is actually CSV data.
func TestToolReadExcelCSVRejectsDisguisedDocumentContainers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  string
	}{
		{
			name: "encrypted-ooxml",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: "error_class=encrypted",
		},
		{
			name: "valid-docx",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not reach CSV parser")
			},
			want: "error_class=malformed",
		},
		{
			name: "pdf",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("%PDF-1.4\n% csv disguise\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "error_class=malformed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.csv")
			tc.write(t, path)
			out := ToolReadExcel(map[string]interface{}{"file_path": path})
			if !strings.Contains(out, tc.want) {
				t.Fatalf("disguised CSV result missing %q: %s", tc.want, out)
			}
			for _, forbidden := range []string{"craft_tool", "manage_skill", "COM", "LibreOffice"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("disguised CSV offered bypass %q: %s", forbidden, out)
				}
			}
		})
	}
}

func TestToolReadExcelCSVUsesLightweightSafetyProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv")
	if err := os.WriteFile(path, []byte("id,value\n1,ordinary CSV\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousProbe := structuredCSVInputProbe
	probeCalls := 0
	structuredCSVInputProbe = func(snapshot string) error {
		probeCalls++
		return probeStructuredCSVInput(snapshot)
	}
	defer func() { structuredCSVInputProbe = previousProbe }()

	out := ToolReadExcel(map[string]interface{}{"file_path": path})
	var result excel.ReadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ToolReadExcel returned non-JSON: %q (%v)", out, err)
	}
	if probeCalls != 1 || len(result.Rows) != 2 || result.Rows[1][1].Value != "ordinary CSV" {
		t.Fatalf("CSV probe calls=%d result=%#v", probeCalls, result)
	}
}

func TestToolReadExcelCapsCSVRowsAndReportsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv")
	if err := os.WriteFile(path, []byte("id,value\n1,alpha\n2,beta\n3,gamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := ToolReadExcel(map[string]interface{}{"file_path": path, "max_rows": 2})
	var result excel.ReadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ToolReadExcel returned non-JSON: %q (%v)", out, err)
	}
	if result.RowCount != 2 || !result.Truncated || result.Rows[1][0].Value != "1" {
		t.Fatalf("bounded ToolReadExcel result = %#v", result)
	}
}

func TestToolReadExcelLegacyXLSMatchesPublicRangeAndRowLimitContract(t *testing.T) {
	path := legacyXLSToolFixture(t, "small_1_sheet.xls")
	for _, opts := range []excel.ReadOptions{
		{Range: "A1:A1"},
		{MaxRows: 1},
	} {
		effectiveOpts := opts
		if effectiveOpts.MaxRows == 0 {
			effectiveOpts.MaxRows = defaultStructuredOfficeMaxRows
		}
		want, err := excel.ReadFile(path, effectiveOpts)
		if err != nil {
			t.Fatalf("public legacy XLS read (%+v): %v", opts, err)
		}
		args := map[string]interface{}{"file_path": path}
		if opts.Range != "" {
			args["range"] = opts.Range
		}
		if opts.MaxRows != 0 {
			args["max_rows"] = opts.MaxRows
		}
		out := ToolReadExcel(args)
		var got excel.ReadResult
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("ToolReadExcel legacy XLS (%+v) returned non-JSON: %q (%v)", opts, out, err)
		}
		if !reflect.DeepEqual(got, *want) {
			t.Fatalf("ToolReadExcel legacy XLS (%+v) = %#v, want public result %#v", opts, got, *want)
		}
	}
}

func legacyXLSToolFixture(t *testing.T, name string) string {
	t.Helper()
	moduleCache := os.Getenv("GOMODCACHE")
	if moduleCache == "" {
		moduleCache = filepath.Join(os.Getenv("USERPROFILE"), "go", "pkg", "mod")
	}
	fixture := filepath.Join(moduleCache, "github.com", "!vantagics", "!legacy!office!reader@v0.0.0-20260621074012-a324c1dbb18b", "testfie", name)
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("legacy XLS module fixture unavailable: %v", err)
	}
	return fixture
}

func TestToolReadExcelParsesPrivateSnapshotAcrossReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replace-after-snapshot.csv")
	if err := os.WriteFile(path, []byte("id,value\n1,validated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousSnapshot := structuredOfficeToolSnapshot
	structuredOfficeToolSnapshot = func(source, format string) (string, func(), error) {
		snapshot, cleanup, err := snapshotStructuredOfficeToolInput(source, format)
		if err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(path, []byte("id,value\n1,replacement\n"), 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
		return snapshot, cleanup, nil
	}
	defer func() { structuredOfficeToolSnapshot = previousSnapshot }()

	out := ToolReadExcel(map[string]interface{}{"file_path": path})
	var result excel.ReadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ToolReadExcel returned non-JSON: %q (%v)", out, err)
	}
	if len(result.Rows) < 2 || result.Rows[1][1].Value != "validated" {
		t.Fatalf("ToolReadExcel parsed replacement instead of snapshot: %#v", result)
	}
}

func TestToolReadExcelParsesVerifiedXLSXSnapshotAcrossReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replace-after-preflight.xlsx")
	writeStructuredOfficeTestXLSX(t, path, "validated")
	previousSnapshot := structuredOfficeToolSnapshot
	structuredOfficeToolSnapshot = func(source, format string) (string, func(), error) {
		snapshot, cleanup, err := snapshotStructuredOfficeToolInput(source, format)
		if err != nil {
			return "", nil, err
		}
		writeStructuredOfficeTestXLSX(t, path, "replacement")
		return snapshot, cleanup, nil
	}
	defer func() { structuredOfficeToolSnapshot = previousSnapshot }()

	out := ToolReadExcel(map[string]interface{}{"file_path": path})
	var result excel.ReadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ToolReadExcel returned non-JSON: %q (%v)", out, err)
	}
	if len(result.Rows) == 0 || result.Rows[0][0].Value != "validated" {
		t.Fatalf("ToolReadExcel parsed replacement instead of verified XLSX snapshot: %#v", result)
	}
}

func TestExtractDocxTextParsesVerifiedSnapshotAcrossReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replace-after-preflight.docx")
	writeMinimalDOCX(t, path, "validated direct extractor source")
	previousPreflight := officeReadPreflight
	var replaced sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		replaced.Do(func() { writeMinimalDOCX(t, path, "replacement direct extractor source") })
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()

	text, err := ExtractDocxText(path)
	if err != nil || !strings.Contains(text, "validated direct extractor source") || strings.Contains(text, "replacement direct extractor source") {
		t.Fatalf("ExtractDocxText snapshot result = %q, %v", text, err)
	}
}

func writeStructuredOfficeTestXLSX(t *testing.T, path, value string) {
	t.Helper()
	if err := excel.WriteFile(path, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: value}}}}}}); err != nil {
		t.Fatalf("write XLSX fixture: %v", err)
	}
}

func TestToolReadExcelCapsMaxRowsArgument(t *testing.T) {
	if got := structuredOfficeMaxRows(map[string]interface{}{"max_rows": 999999}); got != maxStructuredOfficeMaxRows {
		t.Fatalf("max_rows cap = %d, want %d", got, maxStructuredOfficeMaxRows)
	}
}

func TestExportedOfficeExtractorsRejectEncryptedContainers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ext     string
		extract func(string) (string, error)
	}{
		{name: "docx", ext: ".docx", extract: ExtractDocxText},
		{name: "pptx", ext: ".pptx", extract: ExtractPPTXText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encrypted"+tc.ext)
			writeEncryptedOfficeReadZIP(t, path)
			if _, err := tc.extract(path); !errors.Is(err, ErrOfficeReadEncryptedContainer) {
				t.Fatalf("%s exported extractor error = %v", tc.name, err)
			}
		})
	}
}

func TestExtractPDFTextRejectsOversizedInputBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.pdf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractPDFText(path); !errors.Is(err, ErrOfficeReadInputTooLarge) {
		t.Fatalf("ExtractPDFText oversized error = %v", err)
	}
}

func TestExtractPDFTextRejectsNonPDFAfterSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  error
	}{
		{
			name: "docx",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not reach PDF parser")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "unencrypted ole",
			write: func(t *testing.T, path string) {
				writeMinimalOLE(t, path, "WordDocument")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "encrypted ooxml",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: ErrOfficeReadEncryptedContainer,
		},
		{
			name: "plain text",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("not a PDF"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrOfficeReadFormatMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.pdf")
			tc.write(t, path)
			text, err := ExtractPDFText(path)
			if text != "" || !errors.Is(err, tc.want) {
				t.Fatalf("ExtractPDFText = text=%q err=%v, want %v", text, err, tc.want)
			}
		})
	}
}

func TestExtractOfficeTextWithFormatPDFRejectsNonPDFAfterSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  error
	}{
		{
			name: "docx",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not reach PDF parser")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "unencrypted ole",
			write: func(t *testing.T, path string) {
				writeMinimalOLE(t, path, "WordDocument")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "encrypted ooxml",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: ErrOfficeReadEncryptedContainer,
		},
		{
			name: "plain text",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("not a PDF"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrOfficeReadFormatMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.pdf")
			tc.write(t, path)
			text, format, err := ExtractOfficeTextWithFormat(path, "pdf")
			if text != "" || format != "pdf" || !errors.Is(err, tc.want) {
				t.Fatalf("ExtractOfficeTextWithFormat = text=%q format=%q err=%v, want pdf/%v", text, format, err, tc.want)
			}
		})
	}
}

func TestValidatePDFPageCountRejectsExcessivePages(t *testing.T) {
	if err := validatePDFPageCount(pdfinspector.MaxPages); err != nil {
		t.Fatalf("page count at the limit = %v", err)
	}
	if err := validatePDFPageCount(pdfinspector.MaxPages + 1); err == nil || !strings.Contains(err.Error(), "too many pages") {
		t.Fatalf("excessive page count error = %v", err)
	}
}

func TestValidateLegacyOfficeTextUsesSharedRetainedContentLimit(t *testing.T) {
	atLimit := strings.Repeat("文", MaxOfficeReadTextRunes)
	if text, format, err := validateLegacyOfficeText(atLimit, "docx", nil); err != nil || format != "docx" || text != atLimit {
		t.Fatalf("at-limit legacy text = text length %d format=%q err=%v", len([]rune(text)), format, err)
	}
	if text, format, err := validateLegacyOfficeText(atLimit+"文", "docx", nil); !errors.Is(err, ErrOfficeReadOutputTooLarge) || text != "" || format != "docx" {
		t.Fatalf("oversized legacy text = text length %d format=%q err=%v", len([]rune(text)), format, err)
	}
}

func TestReadZipFileRejectsOversizedDeclaredPartBeforeInflation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "declared-oversized.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	part, err := writer.Create("word/document.xml")
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("small fixture")); err != nil {
		_ = writer.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	reader.File[0].UncompressedSize64 = uint64(maxOfficeReadZIPEntryBytes + 1)
	if data, err := readZipFile(reader.File[0]); data != nil || !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("readZipFile = data=%q err=%v, want part-size rejection", data, err)
	}
}

func TestExtractDocxText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.docx")
	writeMinimalDOCX(t, path, "段落一")
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "段落一") {
		t.Fatalf("got %q", text)
	}
}

func TestExtractOfficeText_UnknownExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.odt")
	if err := os.WriteFile(path, []byte("{\\rtf1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, format, err := ExtractOfficeText(path)
	if err == nil {
		t.Fatal("expected unsupported error")
	}
	if format != "odt" && format != "unknown" {
		// format may be "rtf" from extension
		t.Fatalf("format=%q", format)
	}
	// Tool-level wrapper must still include craft_tool guidance.
	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "craft_tool") {
		t.Fatalf("expected craft_tool, got: %s", out)
	}
}

func TestToolReadDocumentPagesTextBasedDataFormats(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		content  string
	}{
		{name: "json", filename: "payload.json", content: `{"name":"MaClaw","items":[1,2,3]}`},
		{name: "xml", filename: "payload.xml", content: `<root><item>MaClaw</item></root>`},
		{name: "yaml", filename: "payload.yaml", content: "name: MaClaw\nitems:\n  - one\n"},
		{name: "log", filename: "service.log", content: "2026-08-15 started\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			out := ToolReadDocument(map[string]interface{}{"file_path": path})
			if !strings.Contains(out, "# format: "+tc.name) || !strings.Contains(out, strings.TrimSpace(tc.content)) || !strings.Contains(out, "# truncated: false") {
				t.Fatalf("ToolReadDocument() = %q", out)
			}
		})
	}
}

func TestExtractOfficeTextRejectsOversizedCSVBeforeParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, format, err := ExtractOfficeText(path); !errors.Is(err, ErrOfficeReadInputTooLarge) || format != "csv" {
		t.Fatalf("ExtractOfficeText oversized CSV = format %q, err %v; want csv/input-too-large", format, err)
	}
	if _, format, err := ExtractOfficeTextWithFormat(path, "csv"); !errors.Is(err, ErrOfficeReadInputTooLarge) || format != "csv" {
		t.Fatalf("ExtractOfficeTextWithFormat oversized CSV = format %q, err %v; want csv/input-too-large", format, err)
	}
}

func TestExtractOfficeTextWithFormatCSVRejectsDisguisedDocumentContainers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  error
	}{
		{
			name: "docx",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not be read as CSV")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "pdf",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("%PDF-1.4\n% CSV disguise\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "encrypted ooxml",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: ErrOfficeReadEncryptedContainer,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.csv")
			tc.write(t, path)
			text, format, err := ExtractOfficeTextWithFormat(path, "csv")
			if text != "" || format != "csv" || !errors.Is(err, tc.want) {
				t.Fatalf("ExtractOfficeTextWithFormat = text=%q format=%q err=%v, want csv/%v", text, format, err, tc.want)
			}
		})
	}
}

func TestExtractOfficeTextWithFormatPlainTextRejectsDisguisedDocumentContainers(t *testing.T) {
	for _, format := range []string{"txt", "text", "md", "markdown"} {
		for _, tc := range []struct {
			name  string
			write func(*testing.T, string)
			want  error
		}{
			{
				name: "docx",
				write: func(t *testing.T, path string) {
					writeMinimalDOCX(t, path, "must not be read as plain text")
				},
				want: ErrOfficeReadFormatMismatch,
			},
			{
				name: "pdf",
				write: func(t *testing.T, path string) {
					if err := os.WriteFile(path, []byte("%PDF-1.4\n% plain-text disguise\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				},
				want: ErrOfficeReadFormatMismatch,
			},
			{
				name: "encrypted ooxml",
				write: func(t *testing.T, path string) {
					writeEncryptedOfficeReadZIP(t, path)
				},
				want: ErrOfficeReadEncryptedContainer,
			},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "disguised."+format)
				tc.write(t, path)
				text, kind, err := ExtractOfficeTextWithFormat(path, format)
				if text != "" || kind != format || !errors.Is(err, tc.want) {
					t.Fatalf("ExtractOfficeTextWithFormat = text=%q format=%q err=%v, want %s/%v", text, kind, err, format, tc.want)
				}
			})
		}
	}
}

func TestExtractOfficeTextRejectsOversizedPDFAfterSignatureRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-disguised.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("%PDF-1.7\n")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, format, err := ExtractOfficeText(path); !errors.Is(err, ErrOfficeReadInputTooLarge) || format != "pdf" {
		t.Fatalf("ExtractOfficeText oversized signature-routed PDF = format %q, err %v; want pdf/input-too-large", format, err)
	}
}

func TestExtractOfficeTextOversizedOOXMLDoesNotOpenZIPDirectoryForFormatSniffing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// A ZIP local-file header is enough to exercise the oversized ZIP branch;
	// it deliberately has no central directory. The input limit must win before
	// zip.OpenReader is ever allowed to inspect it.
	if _, err := file.Write([]byte("PK\x03\x04")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if got := sniffOfficeFormatBounded(path); got != "" {
		t.Fatalf("bounded oversized ZIP sniff = %q, want no directory-derived format", got)
	}
	if _, format, err := ExtractOfficeText(path); !errors.Is(err, ErrOfficeReadInputTooLarge) || format != "docx" {
		t.Fatalf("oversized OOXML = format %q, err %v; want docx/input-too-large", format, err)
	}
}

func TestExtractOfficeText_SniffPDFWrongExt(t *testing.T) {
	dir := t.TempDir()
	// Minimal PDF header; extractor may still fail content, but sniff should route to pdf.
	path := filepath.Join(dir, "misnamed.bin")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without extension knowledge, sniff returns pdf; parse may fail on tiny stub.
	sniffed := sniffOfficeFormat(path)
	if sniffed != "pdf" {
		t.Fatalf("sniff=%q want pdf", sniffed)
	}
}

func TestExtractOfficeText_DocExtensionPDFKeepsSignatureRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actually-a-pdf.doc")
	// The minimal body is intentionally not a complete PDF. The important
	// contract is that the Office preflight does not call it malformed before
	// the unified router identifies the authoritative PDF format.
	if err := os.WriteFile(path, []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, format, err := ExtractOfficeText(path)
	if format != "pdf" {
		t.Fatalf("format = %q, want signature-routed pdf", format)
	}
	if errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("PDF with .doc suffix was rejected as malformed Office input: %v", err)
	}
}

func TestExtractOfficeText_SniffDOCXWrongExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "misnamed.bin")
	writeMinimalDOCX(t, path, "sniffed docx body")
	text, format, err := ExtractOfficeText(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if format != "docx" {
		t.Fatalf("format=%q want docx", format)
	}
	if !strings.Contains(text, "sniffed docx body") {
		t.Fatalf("text=%q", text)
	}
}

func TestExtractOfficeText_UnknownExtensionRejectsEncryptedZIPBeforeSniff(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "customer-upload.bin")
	writeEncryptedOfficeReadZIP(t, path)

	_, format, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("unknown-extension encrypted ZIP err=%v, want shared encrypted rejection", err)
	}
	if format != "bin" {
		t.Fatalf("unknown-extension rejection format=%q, want original extension", format)
	}
}

func TestExtractOfficeText_UnknownExtensionRejectsEncryptedOLEBeforeSniff(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "customer-upload.bin")
	writeMinimalOLE(t, path, "EncryptedSummary")

	_, format, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("unknown-extension encrypted OLE err=%v, want shared encrypted rejection", err)
	}
	if format != "bin" {
		t.Fatalf("unknown-extension rejection format=%q, want original extension", format)
	}
}

func TestExtractOfficeText_UnknownZIPPreflightIsRevalidatedAfterSniff(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")
	path := filepath.Join(t.TempDir(), "customer-upload.bin")
	writeMinimalDOCX(t, path, "sniffed document with revalidated preflight")

	previousPreflight := officeReadPreflight
	preflightCalls := 0
	officeReadPreflight = func(filePath, format string) error {
		preflightCalls++
		return preflightOfficeReadContainer(filePath, format)
	}
	defer func() { officeReadPreflight = previousPreflight }()

	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "docx" || !strings.Contains(text, "sniffed document with revalidated preflight") {
		t.Fatalf("unknown extension extraction text=%q format=%q err=%v", text, format, err)
	}
	if preflightCalls != 2 {
		t.Fatalf("unknown ZIP preflight calls=%d, want 2", preflightCalls)
	}
}

func TestExtractOfficeText_SniffDOCXWrongOfficeExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "misnamed.doc")
	writeMinimalDOCX(t, path, "sniffed DOCX despite DOC extension")
	text, format, err := ExtractOfficeText(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if format != "docx" {
		t.Fatalf("format=%q want docx", format)
	}
	if !strings.Contains(text, "sniffed DOCX despite DOC extension") {
		t.Fatalf("text=%q", text)
	}
}

func writeMinimalDOCX(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document xml: %v", err)
	}
	// Escape XML special chars in text for valid docx fixtures.
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + escaped + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}
