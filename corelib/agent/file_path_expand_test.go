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
	// 200k CJK runes are about 133k tokens, past the default 120k token budget.
	// The same count of runes used to be the budget itself.
	const suffix = "\n结论见此\n"
	body := strings.Repeat("字", 200_000) + suffix
	if EstimateTextTokens(body) <= defaultAutoInjectMaxTokensTotal {
		t.Fatalf("fixture tokens=%d, want above %d", EstimateTextTokens(body), defaultAutoInjectMaxTokensTotal)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in := FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePaths(in)
	if !strings.Contains(out, "truncated=true next_offset=") {
		t.Fatalf("expected truncated=true:\n%s", beginMarkerPreview(out))
	}
	requireInjectedTokensNear(t, out, defaultAutoInjectMaxTokensTotal)
	if !strings.Contains(out, "结论见此") {
		t.Fatal("truncated last document lost its tail")
	}
}

func TestExpandUserSelectedFilePaths_SharedTotalBudget(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	// 200k CJK runes are about 133k tokens, above both the 80k per-file cap
	// and the 120k total. ASCII of the same length would fit.
	const firstTail = "FIRST_FILE_TAIL_MARKER"
	const secondTail = "SECOND_FILE_TAIL_MARKER"
	if err := os.WriteFile(p1, []byte(strings.Repeat("甲", 200_000)+firstTail), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte(strings.Repeat("乙", 200_000)+secondTail), 0o644); err != nil {
		t.Fatal(err)
	}
	in := FilePathPromptPrefix + "\n" + p1 + "\n" + p2 + "\n"
	out := ExpandUserSelectedFilePaths(in)
	if !strings.Contains(out, p1) || !strings.Contains(out, p2) {
		t.Fatalf("both paths should remain:\n%s", out)
	}
	sum := 0
	first, second := 0, 0
	var firstBlock, secondBlock string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isAutoExtractBeginLine(trimmed) {
			continue
		}
		n := extractIntAttr(trimmed, "injected_tokens")
		sum += n
		path := extractQuotedAttr(trimmed, "path")
		switch {
		case strings.EqualFold(filepath.Clean(path), filepath.Clean(p1)):
			first = n
			firstBlock = out
		case strings.EqualFold(filepath.Clean(path), filepath.Clean(p2)):
			second = n
			secondBlock = out
		}
	}
	if first > defaultAutoInjectMaxTokensPerFile || defaultAutoInjectMaxTokensPerFile-first > 8 {
		t.Fatalf("first injected_tokens=%d, want within 8 of per-file %d", first, defaultAutoInjectMaxTokensPerFile)
	}
	if second <= 0 {
		t.Fatal("second file was not injected")
	}
	if sum > defaultAutoInjectMaxTokensTotal || defaultAutoInjectMaxTokensTotal-sum > 16 {
		t.Fatalf("total injected_tokens %d, want within 16 of %d", sum, defaultAutoInjectMaxTokensTotal)
	}
	if strings.Contains(firstBlock, firstTail) {
		t.Fatal("first file is not last; its tail must stay outside the prefix")
	}
	if !strings.Contains(secondBlock, secondTail) || !strings.Contains(secondBlock, "# tail:") {
		t.Fatal("last file must keep a marked tail inside the leftover budget")
	}
}

func TestFormatAutoExtractedDocuments_SharedBudget(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 3)
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".txt")
		// 15k each: all three fit in the 120k shared budget.
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
	if sum > defaultAutoInjectMaxTokensTotal {
		t.Fatalf("sum injected %d > total budget %d\n%v", sum, defaultAutoInjectMaxTokensTotal, blocks)
	}
	// All three 15k files fit in the enlarged 120k shared budget. If this
	// fixture is changed to exceed that budget, the third block must be capped.
	if !strings.Contains(blocks[2], "truncated=true") && !strings.Contains(blocks[2], "budget exhausted") {
		if extractIntAttr(blocks[2], "injected_chars") > defaultAutoInjectMaxTokensPerFile {
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

func TestExpandUserSelectedFilePathsWithContext_SingleFileUsesFullTotalBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	// 200k CJK runes are about 133k tokens. At a 320k context the per-file
	// cap is 106666 tokens and the total is 160000. The only file must use
	// the total, so this body is kept whole.
	const bodyRunes = 200_000
	body := strings.Repeat("文", bodyRunes)
	perFile, total := DocumentAutoExtractBudget(320_000)
	tokens := EstimateTextTokens(body)
	if tokens <= perFile || tokens > total {
		t.Fatalf("fixture tokens=%d, want (%d, %d]", tokens, perFile, total)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in := FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePathsWithContext(in, 320_000)
	if strings.Contains(out, "truncated=true next_offset=") {
		t.Fatalf("single document that fits the total budget must not truncate:\n%s", beginMarkerPreview(out))
	}
	if got := extractIntAttr(out, "injected_chars"); got != bodyRunes {
		t.Fatalf("injected_chars=%d, want %d", got, bodyRunes)
	}
	if got := extractIntAttr(out, "total_chars"); got != bodyRunes {
		t.Fatalf("total_chars=%d, want %d", got, bodyRunes)
	}
}

func TestFormatAutoExtractedDocuments_SingleFileUsesTotalNotPerFileFraction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	body := strings.Repeat("测", 200_000)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	perFile, total := DocumentAutoExtractBudget(320_000)
	tokens := EstimateTextTokens(body)
	if tokens <= perFile {
		t.Fatalf("fixture tokens %d should exceed per-file %d", tokens, perFile)
	}
	if tokens > total {
		t.Fatalf("fixture tokens %d should fit total %d", tokens, total)
	}
	blocks := formatAutoExtractedDocumentsWithSettings([]string{path}, perFile, total, nil, currentOfficeReadSettings(), "")
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if strings.Contains(blocks[0], "truncated=true next_offset=") {
		t.Fatalf("single file must use leftover total budget, got truncated:\n%s", beginMarkerPreview(blocks[0]))
	}
	if got := extractIntAttr(blocks[0], "injected_chars"); got != 200_000 {
		t.Fatalf("injected_chars=%d, want 200000", got)
	}
}

func TestFormatAutoExtractedDocuments_SingleFileTruncatesTokenOvershoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	// 250k CJK runes are about 167k tokens. The total budget at a 320k
	// context is 160k. The overshoot is under the old 24k slack, and must
	// still truncate: slack only reserves a tail inside the budget.
	const suffix = "S7 Conclusion"
	body := strings.Repeat("文", 250_000) + suffix
	perFile, total := DocumentAutoExtractBudget(320_000)
	overshoot := EstimateTextTokens(body) - total
	if overshoot <= 0 || overshoot > autoExtractRemainderSlack {
		t.Fatalf("fixture overshoot=%d, want 1..%d", overshoot, autoExtractRemainderSlack)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	blocks := formatAutoExtractedDocumentsWithSettings([]string{path}, perFile, total, nil, currentOfficeReadSettings(), "")
	if !strings.Contains(blocks[0], "truncated=true next_offset=") {
		t.Fatalf("token overshoot must truncate:\n%s", beginMarkerPreview(blocks[0]))
	}
	requireInjectedTokensNear(t, blocks[0], total)
	if !strings.Contains(blocks[0], suffix) {
		t.Fatal("tail reservation dropped the conclusion")
	}
}

func TestFormatAutoExtractedDocuments_TwoFilesKeepPerFileCapOnFirst(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	const firstTail = "FIRST_FILE_TAIL_MARKER"
	const secondTail = "SECOND_FILE_TAIL_MARKER"
	// 200k CJK runes are about 133k tokens, above the 106666 per-file cap.
	if err := os.WriteFile(p1, []byte(strings.Repeat("甲", 200_000)+firstTail), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte(strings.Repeat("乙", 200_000)+secondTail), 0o644); err != nil {
		t.Fatal(err)
	}
	perFile, total := DocumentAutoExtractBudget(320_000)
	blocks := formatAutoExtractedDocumentsWithSettings([]string{p1, p2}, perFile, total, nil, currentOfficeReadSettings(), "")
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	requireInjectedTokensNear(t, blocks[0], perFile)
	if strings.Contains(blocks[0], firstTail) || strings.Contains(blocks[0], "# tail:") {
		t.Fatal("first file is not last; must not keep a document tail window")
	}
	if !strings.Contains(blocks[1], secondTail) || !strings.Contains(blocks[1], "# tail:") {
		t.Fatal("last file overshoot must keep a marked document tail")
	}
	sum := extractIntAttr(blocks[0], "injected_tokens") + extractIntAttr(blocks[1], "injected_tokens")
	if sum > total || total-sum > 16 {
		t.Fatalf("injected_tokens sum=%d, want within 16 of total %d", sum, total)
	}
}

func TestFormatAutoExtractedDocuments_LastFileTruncatesFarOvershoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	const suffix = "\nS6 Limitations\nS7 Conclusion\nReferences\n"
	// 300k CJK runes are about 200k tokens, past a 160k total by more than slack.
	body := strings.Repeat("文", 300_000) + suffix
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	perFile, total := DocumentAutoExtractBudget(320_000)
	if EstimateTextTokens(strings.TrimSpace(body))-total <= autoExtractRemainderSlack {
		t.Fatal("fixture must overshoot by more than the tail reservation")
	}
	blocks := formatAutoExtractedDocumentsWithSettings([]string{path}, perFile, total, nil, currentOfficeReadSettings(), "")
	if !strings.Contains(blocks[0], "truncated=true next_offset=") {
		t.Fatalf("far overshoot must truncate:\n%s", beginMarkerPreview(blocks[0]))
	}
	requireInjectedTokensNear(t, blocks[0], total)
	gotTotal := extractIntAttr(blocks[0], "total_chars")
	next := extractIntAttr(blocks[0], "next_offset")
	tailAt := tailOffsetFromBlock(blocks[0])
	if next <= 0 || tailAt <= next || tailAt > gotTotal {
		t.Fatalf("next_offset=%d tail=%d total=%d", next, tailAt, gotTotal)
	}
	gap := tailAt - next
	if !strings.Contains(blocks[0], fmt.Sprintf("# tail: offset=%d", tailAt)) {
		t.Fatalf("preserved suffix must be marked as document tail:\n%s", beginMarkerPreview(blocks[0]))
	}
	if !strings.Contains(blocks[0], fmt.Sprintf("offset=%d, max_chars=%d", next, gap)) {
		t.Fatalf("continue hint should read the missing middle (gap=%d) from head %d", gap, next)
	}
	for _, want := range []string{"S6 Limitations", "S7 Conclusion", "References"} {
		if !strings.Contains(blocks[0], want) {
			t.Fatalf("truncated last document lost tail %q", want)
		}
	}
}

func TestFormatAutoExtractedDocuments_LastFileKeepsShortTail(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(p1, []byte(strings.Repeat("A", 80_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	// 80k and 50k ASCII letters are 20k and 12.5k tokens. Both fit the default
	// token budget, so the last file is kept whole.
	if err := os.WriteFile(p2, []byte(strings.Repeat("B", 50_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	blocks := formatAutoExtractedDocumentsWithSettings([]string{p1, p2}, defaultAutoInjectMaxTokensPerFile, defaultAutoInjectMaxTokensTotal, nil, currentOfficeReadSettings(), "")
	if got := extractIntAttr(blocks[0], "injected_chars"); got != 80_000 {
		t.Fatalf("first injected_chars=%d", got)
	}
	if strings.Contains(blocks[1], "truncated=true next_offset=") {
		t.Fatalf("short tail must be kept:\n%s", beginMarkerPreview(blocks[1]))
	}
	if got := extractIntAttr(blocks[1], "injected_chars"); got != 50_000 {
		t.Fatalf("second injected_chars=%d, want 50000", got)
	}
}

func TestFormatAutoExtractedDocument_SingleAttachmentKeepsNearFitOvershoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paper.txt")
	// 125396 CJK runes are about 84k tokens: over the 80k per-file cap, inside
	// the 120k default total. The only attachment uses the total.
	const bodyRunes = 125_396
	body := strings.Repeat("文", bodyRunes)
	tokens := EstimateTextTokens(body)
	if tokens <= defaultAutoInjectMaxTokensPerFile || tokens > defaultAutoInjectMaxTokensTotal {
		t.Fatalf("fixture tokens=%d, want (%d, %d]", tokens, defaultAutoInjectMaxTokensPerFile, defaultAutoInjectMaxTokensTotal)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	block := FormatAutoExtractedDocument(path)
	if strings.Contains(block, "truncated=true next_offset=") {
		t.Fatalf("IM single attachment that fits the total token budget was truncated:\n%s", beginMarkerPreview(block))
	}
	if got := extractIntAttr(block, "injected_chars"); got != bodyRunes {
		t.Fatalf("injected_chars=%d, want %d", got, bodyRunes)
	}
}

func TestAutoExtractNeedsContinuation(t *testing.T) {
	if AutoExtractNeedsContinuation(AutoExtractNotice) {
		t.Fatal("notice instruction must not count as extract output")
	}
	if AutoExtractNeedsContinuation(FilePathPromptPrefix + "\nC:\\a.pdf\n") {
		t.Fatal("path marker alone must not request continuation")
	}
	complete := AutoExtractBeginMarker + `path="C:\a.pdf" format="pdf" total_chars=10 injected_chars=10 truncated=false ---`
	if AutoExtractNeedsContinuation(complete) {
		t.Fatal("complete inject must not request continuation")
	}
	truncated := AutoExtractBeginMarker + `path="C:\a.pdf" format="pdf" truncated=true next_offset=10 ---`
	if !AutoExtractNeedsContinuation(truncated) {
		t.Fatal("truncated=true on begin marker is host extract output")
	}
	failed := AutoExtractBeginMarker + `path="C:\a.pdf" error="自动注入已跳过（error_class=encrypted）。" ---`
	if !AutoExtractNeedsContinuation(failed) {
		t.Fatal("error= on begin marker is host extract output")
	}
}

func TestAutoExtractFitsBudgetDoesNotSpendSlack(t *testing.T) {
	if !autoExtractFitsBudget(125_396, 160_000) {
		t.Fatal("body under budget must keep")
	}
	if autoExtractFitsBudget(180_000, 160_000) {
		t.Fatal("overshoot must truncate; slack is a tail inside the budget, not extra tokens")
	}
	if autoExtractFitsBudget(100_000, 1) {
		t.Fatal("over budget must truncate")
	}
	if !autoExtractFitsBudget(80_000, 80_000) {
		t.Fatal("exact fit must keep")
	}
}

func TestAutoExtractHeadTail(t *testing.T) {
	head, tail := autoExtractHeadTail(200_000, 160_000, autoExtractRemainderSlack)
	if head+tail != 160_000 {
		t.Fatalf("head+tail=%d, want 160000", head+tail)
	}
	if tail != autoExtractRemainderSlack {
		t.Fatalf("tail=%d, want slack %d", tail, autoExtractRemainderSlack)
	}
	head, tail = autoExtractHeadTail(200_000, 160_000, 0)
	if head != 160_000 || tail != 0 {
		t.Fatalf("no slack should be prefix-only, got head=%d tail=%d", head, tail)
	}
	head, tail = autoExtractHeadTail(125_396, 160_000, autoExtractRemainderSlack)
	if head != 125_396 || tail != 0 {
		t.Fatalf("under budget should not split, got head=%d tail=%d", head, tail)
	}
	head, tail = autoExtractHeadTail(100, 4, autoExtractRemainderSlack)
	if head != 4 || tail != 0 {
		t.Fatalf("tiny page should stay prefix-only, got head=%d tail=%d", head, tail)
	}
}

func TestAutoExtractContinueCharsReadsGapOnly(t *testing.T) {
	gap := strings.Repeat("A", 40_000)
	if got := autoExtractContinueChars(gap, 160_000); got != 40_000 {
		t.Fatalf("continue chars=%d, want the whole gap", got)
	}
	big := strings.Repeat("A", 800_000)
	got := autoExtractContinueChars(big, 160_000)
	if got >= len([]rune(big)) {
		t.Fatal("a gap past the token budget was requested whole")
	}
	if EstimateTextTokens(string([]rune(big)[:got])) > 160_000 {
		t.Fatalf("continue chars exceed the token budget, runes=%d", got)
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
	text := AutoExtractBeginMarker + `path="/a.txt" format="txt" total_chars=100 injected_chars=15000 injected_tokens=3750 truncated=false ---` + "\nbody\n" +
		AutoExtractEndMarker + `path="/a.txt" ---`
	if used := CountInjectedAutoExtractRunes(text); used != 15000 {
		t.Fatalf("runes=%d want 15000", used)
	}
	if used := CountInjectedAutoExtractTokens(text); used != 3750 {
		t.Fatalf("tokens=%d want 3750", used)
	}
	left := RemainingAutoInjectBudget(text)
	if left != defaultAutoInjectMaxTokensTotal-3750 {
		t.Fatalf("left=%d", left)
	}
	legacy := AutoExtractBeginMarker + `path="/b.txt" format="txt" total_chars=100 injected_chars=15000 truncated=false ---`
	if used := CountInjectedAutoExtractTokens(legacy); used != 15000 {
		t.Fatalf("legacy marker tokens=%d, want the rune count so the next file cannot overflow", used)
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
	blocks := FormatAutoExtractedDocumentsWithBudget([]string{p1, p2}, defaultAutoInjectMaxTokensTotal, skip)
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
		if err := os.WriteFile(p, []byte(strings.Repeat("Z", defaultAutoInjectMaxTokensTotal+100)), 0o644); err != nil {
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

func TestPackPDFPagesKeepsEndsAndMatchingPage(t *testing.T) {
	pages := make([]string, 6)
	for i := 1; i <= 6; i++ {
		switch i {
		case 4:
			pages[i-1] = fmt.Sprintf("## Page %d\nretriever finds similar chunks %s", i, strings.Repeat("y", 160))
		case 6:
			pages[i-1] = fmt.Sprintf("## Page %d\n%s", i, strings.Repeat("z", 40))
		default:
			pages[i-1] = fmt.Sprintf("## Page %d\n%s", i, strings.Repeat("x", 800))
		}
	}
	var b strings.Builder
	for i, page := range pages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(page)
	}
	note := "\n\n# omitted-pages: middle pages were left out of this excerpt\n\n"
	kept := pages[0] + "\n\n" + pages[3] + "\n\n" + pages[5]
	maxTokens := EstimateTextTokens(kept) + EstimateTextTokens(note)
	packed, original, omitted := packPDFPages(b.String(), "retriever", maxTokens, 10_000)
	if !omitted {
		t.Fatal("expected omitted pages")
	}
	if EstimateTextTokens(packed) > maxTokens {
		t.Fatalf("packed tokens=%d exceed budget %d", EstimateTextTokens(packed), maxTokens)
	}
	for _, want := range []string{"## Page 1\n", "## Page 4\n", "## Page 6\n", "retriever finds similar chunks", "omitted-pages"} {
		if !strings.Contains(packed, want) {
			t.Fatalf("packed text missing %q:\n%s", want, packed)
		}
	}
	for _, absent := range []string{"## Page 2\n", "## Page 3\n", "## Page 5\n"} {
		if strings.Contains(packed, absent) {
			t.Fatalf("packed text kept %q:\n%s", absent, packed)
		}
	}
	if original <= len([]rune(packed)) {
		t.Fatalf("original=%d packed=%d", original, len([]rune(packed)))
	}
}

func TestPackPDFPagesSkipsOversizedLastPage(t *testing.T) {
	page1 := "## Page 1\nstart"
	page2 := "## Page 2\n" + strings.Repeat("m", 800)
	page3 := "## Page 3\nending-keep"
	page4 := "## Page 4\n" + strings.Repeat("z", 4000)
	text := page1 + "\n\n" + page2 + "\n\n" + page3 + "\n\n" + page4
	note := "\n\n# omitted-pages: middle pages were left out of this excerpt\n\n"
	maxTokens := EstimateTextTokens(page1+"\n\n"+page3) + EstimateTextTokens(note)
	packed, _, omitted := packPDFPages(text, "", maxTokens, 10_000)
	if !omitted {
		t.Fatal("expected omitted pages")
	}
	for _, want := range []string{"## Page 1\n", "## Page 3\n", "ending-keep"} {
		if !strings.Contains(packed, want) {
			t.Fatalf("packed text missing %q:\n%s", want, packed)
		}
	}
	for _, absent := range []string{"## Page 2\n", "## Page 4\n"} {
		if strings.Contains(packed, absent) {
			t.Fatalf("packed text kept %q:\n%s", absent, packed)
		}
	}
}

func TestPackPDFPagesKeepsEnglishPagesThatFitTheTokenBudget(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		if i > 1 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## Page %d\n%s\n", i, strings.Repeat("w", 500))
	}
	text := b.String()
	runes := len([]rune(text))
	tokens := EstimateTextTokens(text)
	if runes <= 44_000 {
		t.Fatalf("fixture runes=%d, want above the old half-context rune cap", runes)
	}
	if tokens > 44_000 {
		t.Fatalf("fixture tokens=%d, want inside half of 88000", tokens)
	}
	packed, _, omitted := packPDFPages(text, "你好", 44_000, autoExtractRemainderSlack)
	if omitted || packed != text {
		t.Fatalf("english pages that fit the token budget were packed, omitted=%v", omitted)
	}
}

func TestFormatAutoExtractedDocument_OversizedOutputKeepsHeadAndTail(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")
	path := filepath.Join(t.TempDir(), "large.docx")
	writeMinimalDOCX(t, path, "cache source")
	body := strings.Repeat("头", 20) + strings.Repeat("中", maxOfficeReadTextRunes) + strings.Repeat("尾", 20)
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return body, "docx", nil
	})
	defer restore()
	officeExtractCacheMu.Lock()
	officeExtractCache = make(map[string]officeExtractCacheEntry)
	officeExtractCacheMu.Unlock()

	block, n := formatAutoExtractedDocumentWithSettings(path, 8_000, 2_000, currentOfficeReadSettings(), "")
	if n == 0 || strings.Contains(block, "自动注入已跳过") || strings.Contains(block, "output_too_large") {
		t.Fatalf("oversized document was dropped: n=%d block=%s", n, beginMarkerPreview(block))
	}
	if !strings.HasPrefix(extractBodyForTest(block), "头") || !strings.Contains(block, "尾尾尾") {
		t.Fatalf("head or tail missing, n=%d preview=%s", n, beginMarkerPreview(block))
	}
	if !strings.Contains(block, "truncated=true") {
		t.Fatalf("retained document was marked complete:\n%s", beginMarkerPreview(block))
	}
}

func extractBodyForTest(block string) string {
	start := strings.Index(block, "---\n")
	if start < 0 {
		return block
	}
	return block[start+4:]
}

func TestExpandUserSelectedFilePathsWithContext_ASCIIFileFitsTokenBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	// 100k ASCII letters are 25k tokens. Half of an 88k window is 44k tokens.
	// Counting those letters as tokens used to mark the file truncated.
	body := strings.Repeat("A", 100_000)
	if EstimateTextTokens(body) > 44_000 {
		t.Fatalf("fixture tokens=%d", EstimateTextTokens(body))
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in := "文件是什么？\n\n" + FilePathPromptPrefix + "\n" + path + "\n"
	out := ExpandUserSelectedFilePathsWithContext(in, 88_000)
	if !strings.Contains(out, "truncated=false ---") {
		t.Fatalf("token budget cut a file that fits: %s", beginMarkerPreview(out))
	}
	if !strings.Contains(out, body) {
		t.Fatal("full body was not injected")
	}
	if again := ExpandUserSelectedFilePathsWithContext(out, 88_000); again != out {
		t.Fatal("expand was not idempotent")
	}
}

func requireInjectedTokensNear(t *testing.T, block string, budget int) {
	t.Helper()
	got := extractIntAttr(block, "injected_tokens")
	if got > budget || budget-got > 8 {
		t.Fatalf("injected_tokens=%d, want <= %d and within 8 (%s)", got, budget, beginMarkerPreview(block))
	}
}

func tailOffsetFromBlock(block string) int {
	const key = "# tail: offset="
	i := strings.Index(block, key)
	if i < 0 {
		return 0
	}
	return extractIntAttr(block[i:], "offset")
}

func beginMarkerPreview(text string) string {
	idx := strings.Index(text, AutoExtractBeginMarker)
	if idx < 0 {
		if len(text) > 400 {
			return text[:400]
		}
		return text
	}
	end := idx + 240
	if end > len(text) {
		end = len(text)
	}
	return text[idx:end]
}
