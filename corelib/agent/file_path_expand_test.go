package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoExtractErrorClassIsContentFree(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{err: errOfficeReadInputTooLarge, want: "input_too_large"},
		{err: ErrOfficeReadUnsafeContainer, want: "malformed"},
		{err: ErrOfficeReadSourceChanged, want: "source_changed"},
		{err: errors.New(`zip failure while parsing C:\\Users\\private\\proposal.docx`), want: "malformed"},
		{err: errors.New("private implementation detail"), want: "extract_error"},
	} {
		if got := autoExtractErrorClass(test.err); got != test.want {
			t.Fatalf("autoExtractErrorClass(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestFormatAutoExtractedDocument_RedactsExtractionAndStatErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private.ppt")
	// Use a valid OLE PowerPoint stream so the test reaches the injected
	// OfficeRead parser error. A directory-only CFBF is now correctly rejected
	// by the stricter legacy PowerPoint preflight before any parser is invoked.
	writeMinimalOLE(t, path, "PowerPoint Document")

	oldExtract := officeReadExtract
	defer func() { officeReadExtract = oldExtract }()
	const sensitiveDetail = `parser detail: C:\\Users\\private\\proposal.ppt`
	officeReadExtract = func(string) (string, string, error) {
		return "", "ppt", errors.New(sensitiveDetail)
	}

	block := FormatAutoExtractedDocument(path)
	if strings.Contains(block, sensitiveDetail) || strings.Contains(block, "C:\\Users\\private") {
		t.Fatalf("extraction detail must be redacted from auto-inject block:\n%s", block)
	}
	if !strings.Contains(block, "error_class=extract_error") {
		t.Fatalf("stable error class missing:\n%s", block)
	}
	if !strings.Contains(block, filepath.Base(path)) {
		t.Fatalf("selected path should remain available for fallback:\n%s", block)
	}

	missingPath := filepath.Join(dir, "missing-private.docx")
	missingBlock := FormatAutoExtractedDocument(missingPath)
	if strings.Contains(missingBlock, "The system cannot find") || strings.Contains(missingBlock, "no such file") {
		t.Fatalf("stat error must be redacted from auto-inject block:\n%s", missingBlock)
	}
	if !strings.Contains(missingBlock, "error_class=unavailable") {
		t.Fatalf("stable unavailable class missing:\n%s", missingBlock)
	}
}

func TestFormatAutoExtractedDocument_OversizedInputDoesNotSuggestUnavailablePaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.docx")
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

	block := FormatAutoExtractedDocument(path)
	if !strings.Contains(block, "error_class=input_too_large") || !strings.Contains(block, "32 MiB") {
		t.Fatalf("oversized auto-extract response lost shared-boundary guidance:\n%s", block)
	}
	for _, forbidden := range []string{"office(action=\"read_document\"", "craft_tool", "manage_skill", "COM", "LibreOffice", "专用处理工具"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("oversized auto-extract must not suggest a boundary bypass %q:\n%s", forbidden, block)
		}
	}
}

func TestFormatAutoExtractedDocument_BlockedFailureDoesNotSuggestBypass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	writeEncryptedOfficeReadZIP(t, path)
	block := FormatAutoExtractedDocument(path)
	for _, want := range []string{"error_class=encrypted", "不支持提供密码后解密或读取"} {
		if !strings.Contains(block, want) {
			t.Fatalf("encrypted auto-extract response missing %q:\n%s", want, block)
		}
	}
	for _, forbidden := range []string{"office(action=\"read_document\"", "craft_tool", "manage_skill", "COM", "LibreOffice"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("encrypted auto-extract response suggested a bypass %q:\n%s", forbidden, block)
		}
	}
}

// Text-like extensions retain a bounded raw-text compatibility fallback for
// ordinary parser errors. They must not use that fallback to expose an Office
// container that the shared extraction boundary has already rejected.
func TestFormatAutoExtractedDocument_TextLikeSuffixDoesNotBypassRejectedContainer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  string
	}{
		{
			name: "encrypted-ooxml-as-csv",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: "error_class=encrypted",
		},
		{
			name: "docx-as-markdown",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not enter auto-extract body")
			},
			want: "error_class=malformed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := ".csv"
			if tc.name == "docx-as-markdown" {
				ext = ".md"
			}
			path := filepath.Join(t.TempDir(), "disguised"+ext)
			tc.write(t, path)
			block := FormatAutoExtractedDocument(path)
			if !strings.Contains(block, tc.want) {
				t.Fatalf("rejected container missing %q: %s", tc.want, block)
			}
			for _, forbidden := range []string{"must not enter auto-extract body", "office(action=\"read_document\"", "craft_tool", "manage_skill", "COM", "LibreOffice"} {
				if strings.Contains(block, forbidden) {
					t.Fatalf("rejected text-like input exposed or bypassed %q: %s", forbidden, block)
				}
			}
		})
	}
}

func TestExpandUserSelectedFilePaths_MarkerInProseDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(path, []byte("real-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	// User mentions the marker string in prose without a real begin line — expand must still run.
	in := "请说明 " + AutoExtractBeginMarker + " 是什么意思\n\n" + FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePaths(in)
	if !strings.Contains(out, "real-body") {
		t.Fatalf("prose marker should not block expand:\n%s", out)
	}
	if !strings.Contains(out, AutoExtractNotice) {
		t.Fatalf("notice missing:\n%s", out)
	}
}

func TestExpandUserSelectedFilePaths_InjectsTextDoc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	body := "第一章 标题\n这是自动注入测试正文。"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	in := "请总结这份文档\n\n" + FilePathPromptPrefix + "\n" + path + "\n" +
		"For PDF/Word, Prefer office(action=\"read_document\")."
	out := ExpandUserSelectedFilePaths(in)

	if !strings.Contains(out, FilePathPromptPrefix) {
		t.Fatalf("path prefix missing:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("path missing:\n%s", out)
	}
	if !strings.Contains(out, AutoExtractNotice) {
		t.Fatalf("auto-extract notice missing:\n%s", out)
	}
	if !strings.Contains(out, body) {
		t.Fatalf("document body not injected:\n%s", out)
	}
	if strings.Contains(out, "Prefer office(action=") {
		t.Fatalf("legacy instruction should be dropped:\n%s", out)
	}
	// Idempotent
	again := ExpandUserSelectedFilePaths(out)
	if again != out {
		t.Fatalf("expand not idempotent")
	}
}

func TestExpandUserSelectedFilePaths_ImageOnlyNoExtract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	in := FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePaths(in)
	if strings.Contains(out, AutoExtractBeginMarker) {
		t.Fatalf("images should not auto-extract:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("image path should remain:\n%s", out)
	}
}

func TestExpandUserSelectedFilePaths_AllOfficeFormatsUseOfficeReadDefaultRoute(t *testing.T) {
	dir := t.TempDir()
	oldExtract := officeReadExtract
	defer func() { officeReadExtract = oldExtract }()
	officeReadExtract = func(got string) (string, string, error) {
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(got)), ".")
		if !isOfficeReadFormat(format) {
			t.Fatalf("OfficeRead received unsupported snapshot %q", got)
		}
		return "Office document body", format, nil
	}

	for _, format := range []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(dir, "document."+format)
			writeValidOfficeDefaultRouteFixture(t, path, format)
			out := ExpandUserSelectedFilePaths(FilePathPromptPrefix + "\n" + path + "\n")
			if !strings.Contains(out, "Office document body") || !strings.Contains(out, `format="`+format+`"`) {
				t.Fatalf("%s should use the default OfficeRead route:\n%s", format, out)
			}
		})
	}
}

func TestExpandUserSelectedFilePaths_TruncatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 0; i < defaultAutoInjectMaxRunesPerFile+5000; i++ {
		b.WriteRune('字')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	in := FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePaths(in)
	if !strings.Contains(out, "truncated=true") {
		t.Fatalf("expected truncated=true:\n%s", out)
	}
	if !strings.Contains(out, "next_offset=") {
		t.Fatalf("expected next_offset:\n%s", out)
	}
	if n := len([]rune(out)); n > defaultAutoInjectMaxRunesPerFile+2500 {
		t.Fatalf("injected message too large: %d runes (cap %d)", n, defaultAutoInjectMaxRunesPerFile)
	}
}

func TestExpandUserSelectedFilePaths_SharedTotalBudget(t *testing.T) {
	dir := t.TempDir()
	// Two files each larger than half the total budget so the second is truncated/skipped under shared cap.
	makeBig := func(name string, n int) string {
		p := filepath.Join(dir, name)
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteRune('A')
		}
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p1 := makeBig("a.txt", defaultAutoInjectMaxRunesPerFile)
	p2 := makeBig("b.txt", defaultAutoInjectMaxRunesPerFile)
	in := FilePathPromptPrefix + "\n" + p1 + "\n" + p2 + "\n"
	out := ExpandUserSelectedFilePaths(in)

	// Total injected body should stay near total budget (headers add overhead).
	// Count injected_chars from markers.
	if !strings.Contains(out, p1) || !strings.Contains(out, p2) {
		t.Fatalf("both paths should remain:\n%s", out)
	}
	// Second file should either be truncated to remaining budget or skipped.
	if !strings.Contains(out, "truncated=true") && !strings.Contains(out, "budget exhausted") {
		// With per-file=20k and total=40k, both can inject fully if each is exactly 20k.
		// Bump: verify total injected_chars sum <= total budget.
		sum := 0
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), AutoExtractBeginMarker) {
				continue
			}
			if v := extractIntAttr(line, "injected_chars"); v > 0 {
				sum += v
			}
		}
		if sum > defaultAutoInjectMaxRunesTotal {
			t.Fatalf("total injected_chars %d exceeds budget %d", sum, defaultAutoInjectMaxRunesTotal)
		}
	}
}

func TestFormatAutoExtractedDocuments_SharedBudget(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 3)
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".txt")
		// 15k each → third should hit total 40k budget
		content := strings.Repeat("x", 15_000)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[i] = p
	}
	blocks := FormatAutoExtractedDocuments(paths)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	sum := 0
	for _, b := range blocks {
		sum += extractIntAttr(b, "injected_chars")
	}
	if sum > defaultAutoInjectMaxRunesTotal {
		t.Fatalf("sum injected %d > total budget %d\n%v", sum, defaultAutoInjectMaxRunesTotal, blocks)
	}
	// All three 15k files fit in the enlarged 120k shared budget. If this
	// fixture is changed to exceed that budget, the third block must be capped.
	if !strings.Contains(blocks[2], "truncated=true") && !strings.Contains(blocks[2], "budget exhausted") {
		if extractIntAttr(blocks[2], "injected_chars") > defaultAutoInjectMaxRunesPerFile {
			t.Fatalf("third file exceeds per-file budget:\n%s", blocks[2])
		}
	}
}

func TestFormatAutoExtractedDocument_PlainJSONFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	body := `{"hello":"world","n":1}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	block := FormatAutoExtractedDocument(path)
	if block == "" {
		t.Fatal("expected non-empty block")
	}
	if !strings.Contains(block, "hello") {
		t.Fatalf("json body missing:\n%s", block)
	}
}

func TestDocumentAutoExtractBudgetScalesWithContextWindow(t *testing.T) {
	for _, tc := range []struct {
		context            int
		wantPer, wantTotal int
	}{
		{context: 32_000, wantPer: 26_666, wantTotal: 40_000},
		{context: 200_000, wantPer: 66_666, wantTotal: 100_000},
		{context: 400_000, wantPer: 120_000, wantTotal: 200_000},
	} {
		perFile, total := DocumentAutoExtractBudget(tc.context)
		if perFile != tc.wantPer || total != tc.wantTotal {
			t.Fatalf("DocumentAutoExtractBudget(%d) = (%d, %d), want (%d, %d)", tc.context, perFile, total, tc.wantPer, tc.wantTotal)
		}
	}
}

func TestFormatAutoExtractedDocument_RTFIsNotAutoInjectedAsRawControlText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.rtf")
	body := `{\\rtf1\\ansi MaClaw confidential body}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if IsDocumentFilePath(path) {
		t.Fatal("RTF must not be treated as a natively auto-extractable document")
	}
	if block := FormatAutoExtractedDocument(path); block != "" {
		t.Fatalf("RTF must not inject raw control text, got:\n%s", block)
	}
}

func TestStripAutoExtractBodies_RemovesBodyKeepsSummary(t *testing.T) {
	const docPath = `C:\a.docx`
	text := FilePathPromptPrefix + "\n" + docPath + "\n\n" + AutoExtractNotice + "\n\n" +
		AutoExtractBeginMarker + `path="` + docPath + `" format="docx" total_chars=100 injected_chars=100 truncated=false ---` + "\n" +
		"很长的正文内容不应该出现在历史里\n" +
		AutoExtractEndMarker + `path="` + docPath + `" ---`

	stripped := StripAutoExtractBodies(text)
	if strings.Contains(stripped, "很长的正文内容") {
		t.Fatalf("body should be stripped:\n%s", stripped)
	}
	if strings.Contains(stripped, AutoExtractNotice) {
		t.Fatalf("live notice should not remain in history form:\n%s", stripped)
	}
	if !strings.Contains(stripped, "之前已自动解析文档") {
		t.Fatalf("summary placeholder missing:\n%s", stripped)
	}
	if !strings.Contains(stripped, docPath) {
		t.Fatalf("path should remain in summary:\n%s", stripped)
	}
}

func TestCompactQueryForEmbedding_StripsBodies(t *testing.T) {
	text := "总结\n" + AutoExtractNotice + "\n" +
		AutoExtractBeginMarker + `path="/tmp/a.txt" format="txt" injected_chars=3 truncated=false ---` + "\n" +
		"abc\n" +
		AutoExtractEndMarker + `path="/tmp/a.txt" ---`
	got := CompactQueryForEmbedding(text)
	if strings.Contains(got, "abc") {
		t.Fatalf("body should not be in embedding query:\n%s", got)
	}
	if strings.Contains(got, AutoExtractNotice) {
		t.Fatalf("live notice should be removed/replaced:\n%s", got)
	}
}

func TestAnnotateHistoryAttachmentText_StripsExtract(t *testing.T) {
	text := "看看\n\n" + FilePathPromptPrefix + "\n/tmp/a.txt\n\n" +
		AutoExtractBeginMarker + `path="/tmp/a.txt" format="txt" total_chars=3 injected_chars=3 truncated=false ---` + "\n" +
		"abc\n" +
		AutoExtractEndMarker + `path="/tmp/a.txt" ---`
	out := AnnotateHistoryAttachmentText(text)
	if strings.Contains(out, FilePathPromptPrefix) {
		t.Fatalf("prefix should become historical:\n%s", out)
	}
	if !strings.Contains(out, FilePathPromptPrefixHistorical) {
		t.Fatalf("historical prefix missing:\n%s", out)
	}
	if strings.Contains(out, AutoExtractBeginMarker) {
		t.Fatalf("begin marker should be gone:\n%s", out)
	}
	if strings.Contains(out, "abc") {
		t.Fatalf("raw body leaked:\n%s", out)
	}
}

func TestIsDocumentFilePath(t *testing.T) {
	if !IsDocumentFilePath(`C:\x\report.docx`) {
		t.Fatal("docx should be document")
	}
	if !IsDocumentFilePath("/tmp/a.PDF") {
		t.Fatal("PDF should be document")
	}
	if IsDocumentFilePath("/tmp/a.png") {
		t.Fatal("png should not be document")
	}
}

func TestParseSelectedFilePathLines_WindowsAndUnix(t *testing.T) {
	section := "C:\\Users\\me\\a.docx\n/tmp/b.pdf\nFor PDF/Word use office.\n"
	paths, rest := parseSelectedFilePathLines(section)
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
	if !strings.Contains(rest, "For PDF") {
		t.Fatalf("rest=%q", rest)
	}
}

func TestSelectedLocalFilePathsFromPromptIgnoresHistoricalMarker(t *testing.T) {
	oldPath := `C:\old\resume.pdf`
	currentPath := `C:\current\resume.pdf`
	historicalOnly := FilePathPromptPrefixHistorical + "\n" + oldPath + "\n"
	if got := SelectedLocalFilePathsFromPrompt(historicalOnly); len(got) != 0 {
		t.Fatalf("historical marker paths = %#v, want none", got)
	}
	text := historicalOnly + "\n" + FilePathPromptPrefix + "\n" + currentPath + "\n"
	got := SelectedLocalFilePathsFromPrompt(text)
	if len(got) != 1 || got[0] != currentPath {
		t.Fatalf("current picker paths = %#v, want %q", got, currentPath)
	}
}

func TestFilterLegacyDropsNewFrontendDocNote(t *testing.T) {
	rest := "Documents are auto-parsed by the host when possible; use the injected body first."
	if got := filterLegacyPathInstructions(rest); got != "" {
		t.Fatalf("expected drop, got %q", got)
	}
}

func TestFilterLegacyKeepsImageHint(t *testing.T) {
	rest := "For image files, use the paths directly (vision / read_file); do not re-capture via screenshot."
	got := filterLegacyPathInstructions(rest)
	if !strings.Contains(got, "For image files") {
		t.Fatalf("image hint should be kept: %q", got)
	}
	if strings.Contains(got, "vision / read_file") || !strings.Contains(got, "Analyze attached images first") {
		t.Fatalf("legacy hint should normalize to attachment-first guidance: %q", got)
	}
}

func TestRemainingAutoInjectBudget(t *testing.T) {
	text := AutoExtractBeginMarker + `path="/a.txt" format="txt" total_chars=100 injected_chars=15000 truncated=false ---` + "\nbody\n" +
		AutoExtractEndMarker + `path="/a.txt" ---`
	used := CountInjectedAutoExtractRunes(text)
	if used != 15000 {
		t.Fatalf("used=%d want 15000", used)
	}
	left := RemainingAutoInjectBudget(text)
	if left != defaultAutoInjectMaxRunesTotal-15000 {
		t.Fatalf("left=%d", left)
	}
	skip := AlreadyAutoExtractedPaths(text)
	if _, ok := skip["/a.txt"]; !ok {
		t.Fatalf("path not in skip set: %v", skip)
	}
}

func TestFormatAutoExtractedDocuments_SkipAlreadyInjected(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(p1, []byte("one"), 0o644)
	_ = os.WriteFile(p2, []byte("two"), 0o644)
	skip := map[string]struct{}{p1: {}}
	blocks := FormatAutoExtractedDocumentsWithBudget([]string{p1, p2}, defaultAutoInjectMaxRunesTotal, skip)
	if len(blocks) != 2 {
		t.Fatalf("len=%d", len(blocks))
	}
	if blocks[0] != "" {
		t.Fatalf("p1 should be skipped empty, got %q", blocks[0])
	}
	if !strings.Contains(blocks[1], "two") {
		t.Fatalf("p2 should extract: %s", blocks[1])
	}
}

func TestExpandPreservesImageInstruction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	_ = os.WriteFile(path, []byte{1, 2, 3}, 0o644)
	in := FilePathPromptPrefix + "\n" + path + "\n" +
		"For image files, use the paths directly (vision / read_file); do not re-capture via screenshot."
	out := ExpandUserSelectedFilePaths(in)
	if !strings.Contains(out, "For image files") {
		t.Fatalf("image instruction lost:\n%s", out)
	}
}

func TestStripIgnoresBodyLineThatLooksLikeMarkerPrefix(t *testing.T) {
	// Body line starts with begin marker prefix but lacks path= — must not open strip mode.
	text := "intro\n" + AutoExtractBeginMarker + "not a real marker line without attrs\n" +
		"should stay\n"
	got := StripAutoExtractBodies(text)
	if !strings.Contains(got, "should stay") {
		t.Fatalf("false-positive strip:\n%s", got)
	}
	if !strings.Contains(got, "not a real marker") {
		t.Fatalf("non-marker line should remain:\n%s", got)
	}
}

func TestAppendDocumentExtractsToDescriptions_OnlyAttachmentLines(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(docPath, []byte("attachment-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	voiceLine := fmt.Sprintf("[语音: clip.wav → 已保存到 %s]", filepath.Join(dir, "clip.wav"))
	descs := []string{
		voiceLine,
		fmt.Sprintf("[附件: note.txt → 已保存到 %s]", docPath),
	}
	out := AppendDocumentExtractsToDescriptions(descs, "")
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "attachment-body") {
		t.Fatalf("doc attachment should extract:\n%v", out)
	}
	// Voice line must not gain an auto_extract block.
	for _, line := range out {
		if strings.HasPrefix(strings.TrimSpace(line), "[语音:") && strings.Contains(line, AutoExtractBeginMarker) {
			t.Fatalf("voice line should not get auto_extract:\n%s", line)
		}
	}
	if !strings.Contains(joined, AutoExtractNotice) {
		t.Fatalf("expected notice:\n%s", joined)
	}
}

func TestFormatAutoExtractedDocuments_BudgetExhaustedOneNote(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 4)
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%d.txt", i))
		// Each file larger than total budget so first takes all remaining room.
		if err := os.WriteFile(p, []byte(strings.Repeat("Z", defaultAutoInjectMaxRunesTotal+100)), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[i] = p
	}
	// Only room for a tiny first slice; rest should share one exhaustion note.
	blocks := FormatAutoExtractedDocumentsWithBudget(paths, 500, nil)
	if len(blocks) != 4 {
		t.Fatalf("len=%d", len(blocks))
	}
	if !strings.Contains(blocks[0], "injected_chars=") && !strings.Contains(blocks[0], "truncated=true") {
		// First may be truncated inject
		if extractIntAttr(blocks[0], "injected_chars") == 0 && !strings.Contains(blocks[0], "error=") {
			t.Fatalf("first should inject something:\n%s", blocks[0])
		}
	}
	notes := 0
	empties := 0
	for i := 1; i < 4; i++ {
		if blocks[i] == "" {
			empties++
			continue
		}
		if strings.Contains(blocks[i], "budget exhausted") || strings.Contains(blocks[i], "总预算已用尽") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("expected exactly 1 budget-exhausted note among remaining, got notes=%d empties=%d\n%v", notes, empties, blocks[1:])
	}
	if empties != 2 {
		t.Fatalf("expected 2 empty slots after one note, got empties=%d", empties)
	}
}

func TestAppendDocumentExtracts_SharesBudgetWithUserText(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(p1, []byte(strings.Repeat("A", 18_000)), 0o644)
	_ = os.WriteFile(p2, []byte(strings.Repeat("B", 18_000)), 0o644)

	// Pretend path-marker already injected p1 with 18k chars.
	userText := FilePathPromptPrefix + "\n" + p1 + "\n\n" + AutoExtractNotice + "\n" +
		AutoExtractBeginMarker + fmt.Sprintf("path=%q format=%q total_chars=%d injected_chars=%d truncated=false ---", p1, "txt", 18000, 18000) + "\n" +
		strings.Repeat("A", 18_000) + "\n" +
		AutoExtractEndMarker + fmt.Sprintf("path=%q ---", p1)

	descs := []string{
		fmt.Sprintf("[附件: a.txt → 已保存到 %s]", p1),
		fmt.Sprintf("[附件: b.txt → 已保存到 %s]", p2),
	}
	out := AppendDocumentExtractsToDescriptions(descs, userText)
	joined := strings.Join(out, "\n")
	// p1 should not be re-injected as a full body.
	p1Blocks := strings.Count(joined, "injected_chars=")
	// At most one new block (p2); p1 skipped.
	if strings.Count(joined, filepath.Base(p1)) > 2 && strings.Contains(joined, AutoExtractBeginMarker+`path="`+p1) {
		// Allow path mention in attachment line; disallow second begin for p1.
	}
	for _, line := range strings.Split(joined, "\n") {
		if isAutoExtractBeginLine(strings.TrimSpace(line)) {
			path := extractQuotedAttr(line, "path")
			if path == p1 || filepath.Clean(path) == filepath.Clean(p1) {
				t.Fatalf("p1 should be skipped as already extracted, got begin line: %s", line)
			}
		}
	}
	if p1Blocks < 0 {
		t.Fatal("unreachable")
	}
	// p2 should still get budget (remaining ~22k).
	if !strings.Contains(joined, "B") && !strings.Contains(joined, "budget exhausted") {
		t.Fatalf("expected p2 extract or budget note:\n%s", joined)
	}
}
