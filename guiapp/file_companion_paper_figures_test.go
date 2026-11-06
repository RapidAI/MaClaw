package guiapp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

// A GUI app on macOS runs with launchd's stripped PATH, so the renderer probe
// must know the absolute Homebrew/conda locations directly. Losing one of
// these entries silently reverts macOS companions to "no render component".
func TestPaperDarwinRasterCandidatesCoverHomebrew(t *testing.T) {
	tools := paperPDFToolCandidates("darwin", "pdftocairo")
	wantTools := []string{
		"/opt/homebrew/bin/pdftocairo",
		"/opt/homebrew/opt/poppler/bin/pdftocairo",
		"/usr/local/bin/pdftocairo",
	}
	if len(tools) != len(wantTools) {
		t.Fatalf("darwin pdftocairo candidates = %v, want %v", tools, wantTools)
	}
	for i := range wantTools {
		if tools[i] != wantTools[i] {
			t.Fatalf("darwin pdftocairo candidates = %v, want %v", tools, wantTools)
		}
	}
	py := paperPythonCandidates("darwin", "")
	if len(py) < 2 || py[0] != "python" || py[1] != "python3" {
		t.Fatalf("darwin python candidates must keep the LookPath names first, got %v", py)
	}
	for _, required := range []string{"/opt/homebrew/bin/python3", "/usr/local/bin/python3", "/usr/bin/python3"} {
		found := false
		for _, name := range py {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("darwin python candidates missing %s, got %v", required, py)
		}
	}
	// pyenv and python.org framework builds live outside every static path;
	// they only enter as globs, and the home-relative ones need a home.
	if !containsString(py, "/Library/Frameworks/Python.framework/Versions/*/bin/python3") {
		t.Fatalf("darwin python candidates missing the framework glob, got %v", py)
	}
	withHome := paperPythonCandidates("darwin", "/Users/tester")
	for _, required := range []string{
		"/Users/tester/.pyenv/shims/python3",
		"/Users/tester/.pyenv/versions/*/bin/python3",
		"/Users/tester/.local/bin/python3",
	} {
		if !containsString(withHome, required) {
			t.Fatalf("darwin python candidates missing %s, got %v", required, withHome)
		}
	}
	if containsString(py, "/Users/tester/.pyenv/shims/python3") {
		t.Fatal("home-relative candidates must not appear without a home")
	}
}

// The engine probe caches per field: a slot that came back with a path is
// trusted even when the probe timestamp is zero (a binary does not move under
// a running app), while empty slots are re-probed after paperRasterNegativeTTL
// so installing pymupdf on a poppler-only machine heals without a restart.
// Both cache branches short-circuit before any real probe of the seeded
// fields, so this is deterministic on any machine.
func TestPaperRasterEngineCacheTrustsPositivesAndReProbesStaleNegatives(t *testing.T) {
	defer func() {
		paperRasterMu.Lock()
		paperRasterCached = paperRasterEngine{}
		paperRasterProbedAt = time.Time{}
		paperRasterMu.Unlock()
	}()

	seeded := paperRasterEngine{python: "/fake/bin/python3", cairo: "/fake/bin/pdftocairo"}
	paperRasterMu.Lock()
	paperRasterCached = seeded
	paperRasterProbedAt = time.Time{} // never probed: positives are trusted as-is
	paperRasterMu.Unlock()
	got := paperRasterEngines()
	if got.python != seeded.python || got.cairo != seeded.cairo {
		t.Fatalf("found engines must survive untouched, got %+v", got)
	}

	paperRasterMu.Lock()
	paperRasterCached = paperRasterEngine{}
	paperRasterProbedAt = time.Now() // fresh negative: trusted within the TTL
	paperRasterMu.Unlock()
	if got := paperRasterEngines(); !got.empty() {
		t.Fatalf("negative cache within TTL re-probed: %+v", got)
	}

	// A stale negative must re-probe — and must not drop a slot that was
	// found meanwhile (the per-field granularity this test exists for).
	paperRasterMu.Lock()
	paperRasterCached = paperRasterEngine{cairo: "/fake/bin/pdftocairo"}
	paperRasterProbedAt = time.Now().Add(-2 * paperRasterNegativeTTL)
	paperRasterMu.Unlock()
	got = paperRasterEngines()
	if got.cairo != "/fake/bin/pdftocairo" {
		t.Fatalf("re-probe dropped a found engine: %+v", got)
	}
	paperRasterMu.Lock()
	refreshed := time.Since(paperRasterProbedAt) < time.Minute
	paperRasterMu.Unlock()
	if !refreshed {
		t.Fatal("stale negative cache was not re-probed")
	}
}

// -singlefile is what pins the poppler output to exactly base.png: without it
// both tools append the page number (base-7.png) and the caller's
// os.Stat(outPNG) would miss the file it just paid to render.
func TestPopplerRasterArgsPinSingleFileOutput(t *testing.T) {
	got := popplerRasterArgs("/in/a.pdf", "/out/p7", 7, 140)
	want := []string{"-png", "-f", "7", "-l", "7", "-r", "140", "-singlefile", "/in/a.pdf", "/out/p7"}
	if len(got) != len(want) {
		t.Fatalf("poppler args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("poppler args = %v, want %v", got, want)
		}
	}
}

// mutool draw has no -f/-l/-singlefile family: the page is positional and -o
// names the output exactly when a single page is drawn.
func TestMutoolRasterArgsTakePositionalPage(t *testing.T) {
	got := mutoolRasterArgs("/in/a.pdf", "/out/p7.png", 7, 140)
	want := []string{"draw", "-o", "/out/p7.png", "-r", "140", "/in/a.pdf", "7"}
	if len(got) != len(want) {
		t.Fatalf("mutool args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mutool args = %v, want %v", got, want)
		}
	}
}

// Each engine attempt must get its own full time window, independent of how
// much of the chain budget the earlier engines burned. This is the regression
// guard for the starvation bug: a slow PyMuPDF import must not shrink the
// poppler/mutool fallback's window to zero.
func TestRenderAttemptContextHasOwnIndependentWindow(t *testing.T) {
	child, cancel := renderAttemptContext(context.Background())
	defer cancel()
	deadline, ok := child.Deadline()
	if !ok {
		t.Fatal("attempt context must carry its own deadline")
	}
	remaining := time.Until(deadline)
	if remaining < fileCompanionRenderAttemptBudget-time.Second || remaining > fileCompanionRenderAttemptBudget+2*time.Second {
		t.Fatalf("attempt window = %v, want ~%v", remaining, fileCompanionRenderAttemptBudget)
	}
}

// A crop is the only engine on that path. Its window is the full render
// budget, so a cold pymupdf import that runs past the per-engine slice can
// finish. A cancelled parent still stops it. Background starts no watcher.
func TestClipRenderContextCoversAColdImport(t *testing.T) {
	child, cancel := clipRenderContext(context.Background())
	defer cancel()
	deadline, ok := child.Deadline()
	if !ok {
		t.Fatal("clip context must carry its own deadline")
	}
	remaining := time.Until(deadline)
	if remaining < fileCompanionRenderBudget-time.Second || remaining > fileCompanionRenderBudget+2*time.Second {
		t.Fatalf("clip window = %v, want ~%v", remaining, fileCompanionRenderBudget)
	}
	if remaining < fileCompanionRenderAttemptBudget+time.Second {
		t.Fatal("clip window collapsed to the per-engine slice")
	}

	parent, cancelParent := context.WithCancel(context.Background())
	followed, cancelFollowed := clipRenderContext(parent)
	defer cancelFollowed()
	cancelParent()
	select {
	case <-followed.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("clip context did not cancel when its parent did")
	}
}

// The attempt still aborts the instant the parent (the request) is cancelled —
// the independent window is a floor, not a way to ignore a user abort.
func TestRenderAttemptContextFollowsParentCancel(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	child, cancelChild := renderAttemptContext(parent)
	defer cancelChild()
	cancelParent()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("attempt context did not cancel when its parent did")
	}
}

// Windows keeps its texlive glob as the only fallback, and linux needs the
// plain system locations a desktop-launched app may miss.
func TestPaperOtherOSRasterCandidates(t *testing.T) {
	if got := paperPDFToolCandidates("windows", "pdftocairo"); len(got) != 0 {
		t.Fatalf("windows pdftocairo candidates = %v, want none", got)
	}
	linuxTools := paperPDFToolCandidates("linux", "pdftoppm")
	wantLinux := []string{"/usr/local/bin/pdftoppm", "/usr/bin/pdftoppm"}
	if len(linuxTools) != len(wantLinux) {
		t.Fatalf("linux pdftoppm candidates = %v, want %v", linuxTools, wantLinux)
	}
	for i := range wantLinux {
		if linuxTools[i] != wantLinux[i] {
			t.Fatalf("linux pdftoppm candidates = %v, want %v", linuxTools, wantLinux)
		}
	}
	linuxPy := paperPythonCandidates("linux", "/home/tester")
	for _, required := range []string{"/usr/local/bin/python3", "/usr/bin/python3", "/home/tester/.pyenv/shims/python3", "/home/tester/.pyenv/versions/*/bin/python3"} {
		if !containsString(linuxPy, required) {
			t.Fatalf("linux python candidates missing %s, got %v", required, linuxPy)
		}
	}
	if len(linuxPy) < 2 || linuxPy[0] != "python" || linuxPy[1] != "python3" {
		t.Fatalf("linux python candidates must keep the LookPath names first, got %v", linuxPy)
	}
}

func TestFileCompanionPaperChoosesBodyFiguresFirst(t *testing.T) {
	got := paperChooseFigurePages([]paperFigurePage{
		{page: 30, appendix: true},
		{page: 14},
		{page: 11},
		{page: 45, appendix: true},
		{page: 2},
	})
	want := []int{2, 11, 14, 30, 45}
	if len(got) != len(want) {
		t.Fatalf("pages = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pages = %v", got)
		}
	}
	ordered := paperChooseFigurePages([]paperFigurePage{
		{page: 30, appendix: true},
		{page: 14},
		{page: 11},
		{page: 45, appendix: true},
	})
	body := []int{11, 14, 30, 45}
	if len(ordered) != len(body) {
		t.Fatalf("pages = %v", ordered)
	}
	for i := range body {
		if ordered[i] != body[i] {
			t.Fatalf("pages = %v", ordered)
		}
	}
}

func TestFileCompanionPaperCaptionLines(t *testing.T) {
	cases := []struct {
		line     string
		ok       bool
		appendix bool
	}{
		{"Figure 1: Leakage regimes", true, false},
		{"Figure C.1: BrowserUse", true, true},
		{"图 2：流程", true, false},
		{"图 H.1 附录", true, true},
		{"图片说明", false, false},
		{"  Fig. 3 something", true, false},
	}
	for _, item := range cases {
		appendix, ok := paperFigureCaptionHit(item.line)
		if ok != item.ok || appendix != item.appendix {
			t.Fatalf("%q ok=%v appendix=%v", item.line, ok, appendix)
		}
	}
}

func TestInsertPaperFigureLinesUnderNumberedHeading(t *testing.T) {
	markdown := "## 方法原理\n公式。\n\n## 3. 主要原理图及说明\n图 1：热力图。\n\n## 实验方法\n对比。\n"
	out := insertPaperFigureLines(markdown, []string{"![原文第11页](figures/p11-1.png)"})
	fig := strings.Index(out, "![原文第11页]")
	note := strings.Index(out, "图 1：热力图")
	method := strings.Index(out, "## 实验方法")
	if fig < 0 || note < 0 || method < 0 || fig > note || note > method {
		t.Fatalf("placement:\n%s", out)
	}
	if !strings.Contains(out, "![原文第11页](figures/p11-1.png)\n\n图 1：热力图") {
		t.Fatalf("image is glued to the caption:\n%s", out)
	}
	if strings.Count(out, "原理图") != 1 {
		t.Fatalf("heading duplicated:\n%s", out)
	}
	if !strings.Contains(out, "公式。") {
		t.Fatalf("other section moved:\n%s", out)
	}
}

// macOS renders with Quartz before it goes looking for Homebrew. A native
// success must return without treating a missing external engine as failure,
// which is the "install poppler or mupdf" reply in the screenshot.
func TestFileCompanionRenderPrefersNativePDFRenderer(t *testing.T) {
	prev := fileCompanionNativePDFRender
	t.Cleanup(func() { fileCompanionNativePDFRender = prev })
	called := false
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		called = true
		if source != "paper.pdf" || page != 5 || dpi != 140 {
			t.Fatalf("native render source=%s page=%d dpi=%d", source, page, dpi)
		}
		return os.WriteFile(outPNG, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644)
	}
	out := filepath.Join(t.TempDir(), "page.png")
	if err := fileCompanionRenderPDFPagePNG(context.Background(), "paper.pdf", out, 5, 140); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("native renderer was not used")
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= paperFigureMinBytes {
		t.Fatalf("native render output size=%d", info.Size())
	}
}

// Quartz failing is a render error. It must not collapse into errNoRasterEngine,
// or the companion tells a Mac user to install poppler for a file Quartz rejected.
func TestFileCompanionRenderFailureKeepsNativeError(t *testing.T) {
	quartzErr := errors.New("quartz pdf render failed (3)")
	got := fileCompanionRenderFailure(true, paperRasterEngine{}, quartzErr)
	if !errors.Is(got, quartzErr) {
		t.Fatalf("native failure = %v, want the quartz error", got)
	}
	if errors.Is(got, errNoRasterEngine) {
		t.Fatal("native failure was graded as no raster engine")
	}
	if got := fileCompanionRenderFailure(false, paperRasterEngine{}, nil); !errors.Is(got, errNoRasterEngine) {
		t.Fatalf("no engine at all = %v, want errNoRasterEngine", got)
	}
	toolErr := errors.New("pdftocairo failed")
	if got := fileCompanionRenderFailure(false, paperRasterEngine{cairo: "/bin/pdftocairo"}, toolErr); !errors.Is(got, toolErr) {
		t.Fatalf("external failure = %v, want the tool error", got)
	}
}

func TestPaperRenderNativePagesFillsMissingFigures(t *testing.T) {
	prev := fileCompanionNativePDFRender
	t.Cleanup(func() { fileCompanionNativePDFRender = prev })
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		if dpi != paperFigureCLIDPI || page != 5 {
			t.Fatalf("page=%d dpi=%d", page, dpi)
		}
		return os.WriteFile(outPNG, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644)
	}
	dir := t.TempDir()
	if err := paperRenderNativePages(context.Background(), "paper.pdf", dir, []int{5}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "p5-1.png"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= paperFigureMinBytes {
		t.Fatalf("figure raster size=%d", info.Size())
	}
}

func TestPaperPythonSkipsSystemStub(t *testing.T) {
	if !paperPythonSkipsProbe("/usr/bin/python3") || !paperPythonSkipsProbe("/usr/bin/python") {
		t.Fatal("Apple's python stub must not be executed")
	}
	if paperPythonSkipsProbe("/opt/homebrew/bin/python3") || paperPythonSkipsProbe("/usr/local/bin/python3") {
		t.Fatal("a Homebrew python must stay eligible")
	}
}

func TestPaperRenderFigurePagesReusesFreshRasters(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(source, past, past); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "p5-1.png")
	if err := os.WriteFile(png, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigureSourceMarker), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	prevRender := fileCompanionNativePDFRender
	prevQuick := paperQuickPyMuPDF
	t.Cleanup(func() {
		fileCompanionNativePDFRender = prevRender
		paperQuickPyMuPDF = prevQuick
	})
	fileCompanionNativePDFRender = func(string, string, int, int) error {
		t.Fatal("fresh raster was drawn again")
		return nil
	}
	paperQuickPyMuPDF = func(context.Context) string {
		t.Fatal("fresh raster searched for pymupdf")
		return ""
	}
	lines := paperRenderFigurePages(source, dir, []int{5}, "sess")
	if len(lines) != 1 || !strings.Contains(lines[0], "p5-1.png") {
		t.Fatalf("lines = %v", lines)
	}
	pages, err := os.ReadFile(filepath.Join(dir, paperFigurePagesMarker))
	if err != nil {
		t.Fatal(err)
	}
	if string(pages) != "5" {
		t.Fatalf("pages = %q, want the fresh page recorded so the next turn skips the PDF", pages)
	}
}

func TestPaperFigureCaptionPagesStopsAfterEnoughBodyFigures(t *testing.T) {
	prev := paperExtractPageText
	t.Cleanup(func() { paperExtractPageText = prev })
	calls := 0
	paperExtractPageText = func([]byte, int) (string, error) {
		calls++
		return "Figure 1: chart", nil
	}
	got := paperFigureCaptionPages([]byte("%PDF-1.4"))
	if calls != paperFigureMaxCount {
		t.Fatalf("extract calls = %d, want %d", calls, paperFigureMaxCount)
	}
	if len(got) != paperFigureMaxCount {
		t.Fatalf("pages = %d, want %d", len(got), paperFigureMaxCount)
	}
}

// The figures directory belongs to the session, not the document. A fresh
// raster from the previous PDF must not be shown as this PDF's page.
func TestPaperRenderFigurePagesRedrawsWhenSourceChanges(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.pdf")
	second := filepath.Join(dir, "b.pdf")
	for _, name := range []string{first, second} {
		if err := os.WriteFile(name, []byte("%PDF-1.4"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(second, past, past); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "p5-1.png")
	if err := os.WriteFile(png, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	// A page this request does not ask for still belongs to the previous PDF.
	// Leaving it would show that document's page 7 the next time page 7 is asked.
	other := filepath.Join(dir, "p7-1.png")
	if err := os.WriteFile(other, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigureSourceMarker), []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigurePagesMarker), []byte("5,7"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevRender := fileCompanionNativePDFRender
	prevQuick := paperQuickPyMuPDF
	t.Cleanup(func() {
		fileCompanionNativePDFRender = prevRender
		paperQuickPyMuPDF = prevQuick
	})
	drawn := 0
	paperQuickPyMuPDF = func(context.Context) string { return "" }
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		drawn++
		return os.WriteFile(outPNG, bytes.Repeat([]byte("y"), paperFigureMinBytes+1), 0o644)
	}
	lines := paperRenderFigurePages(second, dir, []int{5}, "sess")
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	if drawn != 1 {
		t.Fatalf("draws = %d, want the new PDF rendered once", drawn)
	}
	recorded, err := os.ReadFile(filepath.Join(dir, paperFigureSourceMarker))
	if err != nil {
		t.Fatal(err)
	}
	if string(recorded) != second {
		t.Fatalf("marker = %q, want %q", recorded, second)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("page raster from the previous PDF is still in the session figures directory")
	}
	pages, err := os.ReadFile(filepath.Join(dir, paperFigurePagesMarker))
	if err != nil {
		t.Fatal(err)
	}
	if string(pages) != "5" {
		t.Fatalf("pages = %q, want only the page drawn from the new PDF", pages)
	}
}

// A partial render of the new PDF must not keep the previous document's
// marker. Otherwise the new page image is newer than the old PDF and the
// next turn shows it as the old document.
func TestPaperRenderFigurePagesPartialForeignRenderDropsOldMarker(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.pdf")
	second := filepath.Join(dir, "b.pdf")
	for _, name := range []string{first, second} {
		if err := os.WriteFile(name, []byte("%PDF-1.4"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "p5-1.png"), bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigureSourceMarker), []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigurePagesMarker), []byte("5"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevRender := fileCompanionNativePDFRender
	prevQuick := paperQuickPyMuPDF
	prevCache := paperRasterCached
	prevProbed := paperRasterProbedAt
	t.Cleanup(func() {
		fileCompanionNativePDFRender = prevRender
		paperQuickPyMuPDF = prevQuick
		paperRasterCached = prevCache
		paperRasterProbedAt = prevProbed
	})
	paperRasterCached = paperRasterEngine{}
	paperRasterProbedAt = time.Now()
	paperQuickPyMuPDF = func(context.Context) string { return "" }
	draws := 0
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		if page == 6 {
			return errors.New("page failed")
		}
		draws++
		return os.WriteFile(outPNG, bytes.Repeat([]byte("y"), paperFigureMinBytes+1), 0o644)
	}
	lines := paperRenderFigurePages(second, dir, []int{5, 6}, "sess")
	if len(lines) != 1 || !strings.Contains(lines[0], "p5-1.png") {
		t.Fatalf("lines = %v", lines)
	}
	recorded, err := os.ReadFile(filepath.Join(dir, paperFigureSourceMarker))
	if err == nil && strings.TrimSpace(string(recorded)) == first {
		t.Fatal("partial render kept the previous PDF as the source of the new page image")
	}
	if _, err := os.Stat(filepath.Join(dir, paperFigurePagesMarker)); !os.IsNotExist(err) {
		t.Fatal("partial render kept the previous PDF's page list")
	}
	draws = 0
	again := paperRenderFigurePages(first, dir, []int{5}, "sess")
	if draws != 1 {
		t.Fatalf("draws = %d, want the original PDF redrawn", draws)
	}
	if len(again) != 1 {
		t.Fatalf("lines = %v", again)
	}
}

func TestFileCompanionRasterizeReusesFreshFiguresWithoutReadingPDF(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(source, past, past); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "figures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p5-1.png"), bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigureSourceMarker), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, paperFigurePagesMarker), []byte("5"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := paperReadFigureSource
	t.Cleanup(func() { paperReadFigureSource = prev })
	paperReadFigureSource = func(string) ([]byte, error) {
		t.Fatal("fresh figures opened the PDF")
		return nil, errors.New("opened")
	}
	lines := fileCompanionRasterizePaperFigures(source, root, "sess")
	if len(lines) != 1 || !strings.Contains(lines[0], "p5-1.png") {
		t.Fatalf("lines = %v", lines)
	}
}

func TestFileCompanionRenderedPNGRejectsTinyFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(out, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fileCompanionRenderedPNG(out); err == nil {
		t.Fatal("tiny output was accepted as a page")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("tiny output was left for the next engine")
	}
	if err := os.WriteFile(out, bytes.Repeat([]byte("y"), paperFigureMinBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fileCompanionRenderedPNG(out); err != nil {
		t.Fatal(err)
	}
}

func TestPaperSearchPyMuPDFStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := paperSearchPyMuPDF(ctx, 3); got != "" {
		t.Fatalf("cancelled search returned %q", got)
	}
}

// A native fill is the whole result. Probing pymupdf afterward spends up to
// paperPyMuPDFProbeBudget on a Mac that has already drawn the page.
func TestPaperRenderFigurePagesSkipsExternalProbeWhenNativeFills(t *testing.T) {
	prevRender := fileCompanionNativePDFRender
	prevQuick := paperQuickPyMuPDF
	paperRasterMu.Lock()
	prevCache := paperRasterCached
	prevProbed := paperRasterProbedAt
	paperRasterCached = paperRasterEngine{}
	paperRasterProbedAt = time.Time{}
	paperRasterMu.Unlock()
	t.Cleanup(func() {
		fileCompanionNativePDFRender = prevRender
		paperQuickPyMuPDF = prevQuick
		paperRasterMu.Lock()
		paperRasterCached = prevCache
		paperRasterProbedAt = prevProbed
		paperRasterMu.Unlock()
	})

	paperQuickPyMuPDF = func(context.Context) string { return "" }
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		return os.WriteFile(outPNG, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644)
	}
	dir := t.TempDir()
	lines := paperRenderFigurePages(filepath.Join(dir, "missing.pdf"), dir, []int{5}, "sess")
	if len(lines) != 1 || !strings.Contains(lines[0], "p5-1.png") {
		t.Fatalf("lines = %v", lines)
	}
	paperRasterMu.Lock()
	probed := !paperRasterProbedAt.IsZero()
	paperRasterMu.Unlock()
	if probed {
		t.Fatal("native fill still probed external pdf engines")
	}
}

// A cached pymupdf path that cannot run must not block the page. Quartz (or
// the test double) still draws it, and the long engine probe stays idle.
func TestPaperRenderFigurePagesUsesNativeAfterQuickPyMuPDFMiss(t *testing.T) {
	prevRender := fileCompanionNativePDFRender
	prevQuick := paperQuickPyMuPDF
	paperRasterMu.Lock()
	prevCache := paperRasterCached
	prevProbed := paperRasterProbedAt
	paperRasterCached = paperRasterEngine{}
	paperRasterProbedAt = time.Time{}
	paperRasterMu.Unlock()
	t.Cleanup(func() {
		fileCompanionNativePDFRender = prevRender
		paperQuickPyMuPDF = prevQuick
		paperRasterMu.Lock()
		paperRasterCached = prevCache
		paperRasterProbedAt = prevProbed
		paperRasterMu.Unlock()
	})

	drawn := 0
	paperQuickPyMuPDF = func(context.Context) string { return filepath.Join(t.TempDir(), "missing-python") }
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		drawn++
		if page != 5 {
			t.Fatalf("page=%d", page)
		}
		return os.WriteFile(outPNG, bytes.Repeat([]byte("x"), paperFigureMinBytes+1), 0o644)
	}
	dir := t.TempDir()
	lines := paperRenderFigurePages(filepath.Join(dir, "paper.pdf"), dir, []int{5}, "sess")
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	if drawn != 1 {
		t.Fatalf("native draws = %d, want 1", drawn)
	}
	paperRasterMu.Lock()
	probed := !paperRasterProbedAt.IsZero()
	paperRasterMu.Unlock()
	if probed {
		t.Fatal("quick pymupdf miss still probed every external engine")
	}
}

func TestPaperRenderNativePagesDropsFailedPages(t *testing.T) {
	prev := fileCompanionNativePDFRender
	t.Cleanup(func() { fileCompanionNativePDFRender = prev })
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		if err := os.WriteFile(outPNG, []byte("partial"), 0o644); err != nil {
			t.Fatal(err)
		}
		return errors.New("quartz pdf render failed (3)")
	}
	dir := t.TempDir()
	if err := paperRenderNativePages(context.Background(), "paper.pdf", dir, []int{5}); err == nil {
		t.Fatal("expected native failure")
	}
	if _, err := os.Stat(filepath.Join(dir, "p5-1.png")); !os.IsNotExist(err) {
		t.Fatalf("failed page left a file: %v", err)
	}
}

func TestPaperRenderNativePagesHonorsCancel(t *testing.T) {
	prev := fileCompanionNativePDFRender
	t.Cleanup(func() { fileCompanionNativePDFRender = prev })
	called := false
	fileCompanionNativePDFRender = func(source, outPNG string, page, dpi int) error {
		called = true
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := paperRenderNativePages(ctx, "paper.pdf", t.TempDir(), []int{1}); err == nil {
		t.Fatal("expected cancellation")
	}
	if called {
		t.Fatal("renderer ran after cancel")
	}
}

func TestFileCompanionPaperRasterizesCaptionPages(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	engine := paperRasterEngines()
	if engine.python == "" && engine.cairo == "" && engine.ppm == "" {
		t.Skip("no pdf renderer")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithCaption(t, pdfPath, font)
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(paperChooseFigurePages(paperFigureCaptionPages(data))) == 0 {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	markdown := "## 主要原理图及说明\n文字说明。\n\n## 实验方法\n对比。\n"
	out := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_raster")
	if strings.Count(out, "![原文第1页](") != 1 || !strings.Contains(out, ".png") {
		t.Fatalf("rasterized figure:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "figures", "p1-1.png")); err != nil {
		t.Fatal(err)
	}
	methodAt := strings.Index(out, "## 实验方法")
	figureAt := strings.Index(out, "![原文第1页](")
	noteAt := strings.Index(out, "文字说明。")
	if figureAt < 0 || methodAt < 0 || noteAt < 0 || figureAt > noteAt || noteAt > methodAt {
		t.Fatalf("figure placement:\n%s", out)
	}
}

func TestFileCompanionPaperEmbedsExtractedFigures(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, pdfPath)
	markdown := "## 主要原理图及说明\n文字说明模块和数据流。\n\n## 实验方法\n对比基线。\n"
	out := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_fig")
	if !strings.Contains(out, "文字说明模块和数据流。") || !strings.Contains(out, "## 实验方法") {
		t.Fatalf("section text moved: %s", out)
	}
	figureAt := strings.Index(out, "![原文第1页](")
	noteAt := strings.Index(out, "文字说明模块和数据流。")
	methodAt := strings.Index(out, "## 实验方法")
	if figureAt < 0 || noteAt < 0 || methodAt < 0 || figureAt > noteAt || noteAt > methodAt {
		t.Fatalf("figure was not placed under the heading:\n%s", out)
	}
	if strings.Count(out, "![原文第") != 1 {
		t.Fatalf("figure count:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "figures", "p1-1.jpg")); err != nil {
		t.Fatal(err)
	}
	plain := fileCompanionEmbedPaperFigures(filepath.Join(dir, "notes.md"), dir, markdown, "fc_fig")
	if plain != markdown {
		t.Fatalf("non-pdf changed: %s", plain)
	}
}

// A glued running header hides the caption from the whole-page raster, and the
// only embedded bitmap is the template icon. The interpretation still receives
// the cropped figure. A real bitmap is not replaced by that crop.
func TestFileCompanionPaperEmbedsClippedFiguresWhenRasterMisses(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFIconAndCaption(t, pdfPath, font)
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(paperFigureClipPages(data)) == 0 {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	prevText := paperExtractPageText
	paperExtractPageText = func(_ []byte, page int) (string, error) {
		if page > 0 {
			return "", errors.New("page out of range")
		}
		return "C. Xiao et al./ Knowledge-Based System", nil
	}
	t.Cleanup(func() { paperExtractPageText = prevText })
	if pages := paperChooseFigurePages(paperFigureCaptionPages(data)); len(pages) != 0 {
		t.Fatalf("raster pages = %v, want none when the caption is glued to the header", pages)
	}
	clip := filepath.Join(dir, "figures", "fig-1.png")
	writeCompanionFigurePNG(t, clip, 220, 160)
	prevHook := fileCompanionClipPageFiguresHook
	calls := 0
	fileCompanionClipPageFiguresHook = func(_ context.Context, _, _ string, page, dpi int, figure string) ([]companionFigure, error) {
		calls++
		if calls > 1 {
			// The next paper's crop writes the same scratch name. The previous
			// snapshot's picture has to stay the bytes from the first crop.
			writeCompanionFigurePNG(t, clip, 240, 180)
		}
		if page != 1 || dpi != paperFigureCLIDPI || figure != "" {
			t.Errorf("clip args page=%d dpi=%d figure=%q", page, dpi, figure)
		}
		return []companionFigure{{path: clip, caption: "Figure 1（第 1 页）", size: 2048}}, nil
	}
	t.Cleanup(func() { fileCompanionClipPageFiguresHook = prevHook })
	markdown := "## 主要原理图及说明\n文字说明。\n\n## 实验方法\n对比。\n"
	out := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_clip")
	if !strings.Contains(out, "Figure 1（第 1 页）") || strings.Contains(out, filepath.ToSlash(clip)) {
		t.Fatalf("clipped figure:\n%s", out)
	}
	published := paperMarkdownImagePath(out, "Figure 1（第 1 页）")
	if !strings.HasSuffix(filepath.Base(published), "-fig-1.png") {
		t.Fatalf("published name = %s", published)
	}
	firstBytes, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "figures", "clip-source.txt")); !os.IsNotExist(statErr) {
		t.Fatal("clip identity is still a second file")
	}
	figureAt := strings.Index(out, "![Figure 1（第 1 页）](")
	noteAt := strings.Index(out, "文字说明。")
	methodAt := strings.Index(out, "## 实验方法")
	if figureAt < 0 || noteAt < 0 || methodAt < 0 || figureAt > noteAt || noteAt > methodAt {
		t.Fatalf("figure placement:\n%s", out)
	}
	if calls != 1 {
		t.Fatalf("crop calls = %d, want 1", calls)
	}
	again := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_clip")
	if again != out {
		t.Fatalf("second interpretation:\n%s", again)
	}
	if calls != 1 {
		t.Fatal("the saved crop was drawn again")
	}
	older := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(pdfPath, older, older); err != nil {
		t.Fatal(err)
	}
	replaced := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_clip")
	if replaced == "" {
		t.Fatal("a replaced pdf produced no interpretation")
	}
	if calls != 2 {
		t.Fatal("a pdf with an older timestamp reused the previous crop")
	}
	if strings.Contains(replaced, filepath.ToSlash(published)) {
		t.Fatal("the replaced pdf still shows the previous snapshot")
	}
	kept, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, firstBytes) {
		t.Fatal("the next crop replaced the previous snapshot's picture")
	}

	chartDir := t.TempDir()
	chartPath := filepath.Join(chartDir, "chart.pdf")
	writePaperPDFWithFigures(t, chartPath)
	called := false
	fileCompanionClipPageFiguresHook = func(context.Context, string, string, int, int, string) ([]companionFigure, error) {
		called = true
		return nil, errors.New("bitmap paper must not be cropped")
	}
	chartMarkdown := "## 主要原理图及说明\n文字说明模块和数据流。\n\n## 实验方法\n对比基线。\n"
	chartOut := fileCompanionEmbedPaperFigures(chartPath, chartDir, chartMarkdown, "fc_chart")
	if called {
		t.Fatal("a decoded chart was sent to the vector crop")
	}
	if !strings.Contains(chartOut, "![原文第1页](") {
		t.Fatalf("chart embed:\n%s", chartOut)
	}
}

func paperMarkdownImagePath(markdown, alt string) string {
	needle := "![" + alt + "]("
	at := strings.Index(markdown, needle)
	if at < 0 {
		return ""
	}
	rest := markdown[at+len(needle):]
	end := strings.Index(rest, ")")
	if end <= 0 {
		return ""
	}
	return filepath.FromSlash(rest[:end])
}

// The crop writes fig-1.png before the list is published. A second file used
// to name the PDF, so a failed update could pair the old PDF with the new
// picture. One list names pictures that the next scratch write does not touch.
func TestPaperClipSnapshotKeepsThePreviousPicture(t *testing.T) {
	dir := t.TempDir()
	figures := filepath.Join(dir, "figures")
	scratch := filepath.Join(figures, "fig-1.png")
	writeCompanionFigurePNG(t, scratch, 220, 160)
	source := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figures, paperClipListMarker), []byte("Figure 1（第 3 页）\tfig-1.png\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := strconv.FormatInt(info.Size(), 10) + "\t" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n" + source
	if err := os.WriteFile(filepath.Join(figures, "clip-source.txt"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if paperFreshClipLines(source, figures) != nil {
		t.Fatal("the split cache was reused")
	}
	lines := paperRememberClipLines(source, figures, []paperClipRecord{{alt: "Figure 1（第 3 页）", name: "fig-1.png"}}, info.Size(), info.ModTime().UnixNano())
	if len(lines) != 1 {
		t.Fatalf("remember = %v", lines)
	}
	if _, statErr := os.Stat(filepath.Join(figures, "clip-source.txt")); !os.IsNotExist(statErr) {
		t.Fatal("clip identity is still a second file")
	}
	published := paperMarkdownImagePath(lines[0], "Figure 1（第 3 页）")
	first, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	writeCompanionFigurePNG(t, scratch, 240, 180)
	kept, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, kept) {
		t.Fatal("overwriting the scratch crop replaced the published picture")
	}
	if fresh := paperFreshClipLines(source, figures); len(fresh) != 1 || fresh[0] != lines[0] {
		t.Fatalf("fresh = %v", fresh)
	}
	older := info.ModTime().Add(-2 * time.Hour)
	if err := os.Chtimes(source, older, older); err != nil {
		t.Fatal(err)
	}
	if paperFreshClipLines(source, figures) != nil {
		t.Fatal("an older timestamp reused the previous snapshot")
	}
	info, err = os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	next := paperRememberClipLines(source, figures, []paperClipRecord{{alt: "Figure 1（第 3 页）", name: "fig-1.png"}}, info.Size(), info.ModTime().UnixNano())
	if len(next) != 1 || next[0] == lines[0] {
		t.Fatalf("next snapshot = %v", next)
	}
	kept, err = os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, kept) {
		t.Fatal("publishing the next snapshot replaced the previous picture")
	}
	nextBytes, err := os.ReadFile(paperMarkdownImagePath(next[0], "Figure 1（第 3 页）"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(nextBytes, first) {
		t.Fatal("the next snapshot kept the previous picture bytes")
	}
	weird := source + "\nextra"
	_, _, parsedPath, parsedRows, ok := paperParseClipSnapshot([]byte(
		strconv.FormatInt(info.Size(), 10) + "\t" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\t" + strconv.Itoa(len(weird)) + "\n" + weird + "\nFigure 1（第 3 页）\tfig-1.png\n",
	))
	if !ok || parsedPath != weird || len(parsedRows) != 1 {
		t.Fatalf("newline path ok=%v path=%q rows=%v", ok, parsedPath, parsedRows)
	}
}

func TestPaperPageMentionsClipCaptionAfterARunningHeader(t *testing.T) {
	glued := " C. Xiao et al./ Knowledge-Based System  Figure 1: The overall architecture of NeurRank."
	if !paperPageMentionsClipCaption(glued) {
		t.Fatal("a caption glued to the running header was missed")
	}
	if paperPageMentionsClipCaption("As shown in Figure 1, the method stays prose.") {
		t.Fatal("a sentence mention was treated as a caption")
	}
	if !paperPageMentionsClipCaption("Figure 2 : Effect of the threshold.") {
		t.Fatal("a caption with a space before the colon was missed")
	}
	if paperPageMentionsClipCaption("Figure 1 shows the method and stays prose.") {
		t.Fatal("a sentence was treated as a caption")
	}
	if paperPageMentionsClipCaption("see Fig. 1. The method stays prose.") {
		t.Fatal("a sentence ending in a period was treated as a caption")
	}
	if !paperPageMentionsClipCaption("Figure 1. The architecture of the method.") {
		t.Fatal("a line-start caption with a period was missed")
	}
	if !paperPageMentionsClipCaption("Figure 1") {
		t.Fatal("a bare figure line was missed")
	}
	if !paperPageMentionsClipCaption("图 2 附录") {
		t.Fatal("a line-start 图 caption was missed")
	}
	if paperPageMentionsClipCaption("见图1：方法保持为正文。") {
		t.Fatal("a 见图 reference was treated as a caption")
	}
	if paperPageMentionsClipCaption("如图1：所示，方法保持为正文。") {
		t.Fatal("a 如图 reference was treated as a caption")
	}
	if !paperPageMentionsClipCaption("学报  图 1：整体架构") {
		t.Fatal("a 图 caption glued after a header was missed")
	}
	appendix, ok := paperClipCaptionKind("Figure C.1: BrowserUse")
	if !ok || !appendix {
		t.Fatalf("appendix caption ok=%v appendix=%v", ok, appendix)
	}
	appendix, ok = paperClipCaptionKind("Figure C.1: extra\nFigure 1: body")
	if !ok || appendix {
		t.Fatalf("mixed captions ok=%v appendix=%v", ok, appendix)
	}
}

func TestPaperFigureClipPagesReadsTheFileOnce(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	writePaperPDFIconAndCaption(t, path, font)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, previewErr := gopdf2.PlainTextByPage(data, 2)
	if previewErr != nil || len(preview) == 0 || !strings.Contains(preview[0], "Figure") {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	if got := paperFigureCaptionPages(data); len(got) != 1 || got[0].page != 1 || got[0].appendix {
		t.Fatalf("caption pages = %+v, want page 1", got)
	}
	calls := 0
	prev := paperExtractPageText
	paperExtractPageText = func(pdf []byte, page int) (string, error) {
		calls++
		return "", nil
	}
	t.Cleanup(func() { paperExtractPageText = prev })
	pages := paperFigureClipPages(data)
	if calls != 0 {
		t.Fatalf("per-page extracts = %d, want one open", calls)
	}
	if len(pages) != 1 || pages[0] != 1 {
		t.Fatalf("pages = %v, want page 1", pages)
	}
}

// The clip names every caption in the window. The whole-page raster still
// stops at paperFigureMaxCount, because each extra page is drawn in full.
// An early caption that the crop cannot keep must not hide a later figure.
func TestPaperFigureClipPagesReadsPastTheRasterBudget(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	const pages = paperFigureMaxCount + 2
	dir := t.TempDir()
	path := filepath.Join(dir, "captions.pdf")
	writePaperPDFCaptionPages(t, path, font, pages)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, previewErr := gopdf2.PlainTextByPage(data, pages)
	if previewErr != nil || len(preview) < pages {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	for i, text := range preview {
		if !strings.Contains(text, "Figure") {
			t.Skipf("page %d caption was not extracted", i+1)
		}
	}
	raster := paperFigureCaptionPages(data)
	if len(raster) != paperFigureMaxCount {
		t.Fatalf("raster pages = %d, want the render budget", len(raster))
	}
	clip := paperFigureClipPages(data)
	if len(clip) != pages {
		t.Fatalf("clip pages = %v, want all %d caption pages", clip, pages)
	}
}

// A crop that errors is not "this paper has no figures". The empty inventory
// is asked again after the probe window. A crop that answers with nothing
// stays cached.
func TestFileCompanionPdfFigureCacheRetriesAFailedCrop(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFIconAndCaption(t, pdfPath, font)
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(paperFigureClipPages(data)) == 0 {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	canonical, err := fileCompanionCanonicalPath(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	prev := fileCompanionClipPageFiguresHook
	t.Cleanup(func() { fileCompanionClipPageFiguresHook = prev })
	calls := 0
	fileCompanionClipPageFiguresHook = func(context.Context, string, string, int, int, string) ([]companionFigure, error) {
		calls++
		return nil, errors.New("crop failed")
	}
	if figures := app.fileCompanionFigures(canonical); len(figures) != 0 {
		t.Fatalf("figures = %+v, want none after a failed crop", figures)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	cached := st.figures[canonical]
	st.mu.Unlock()
	if cached == nil || cached.retry.IsZero() || !cached.retry.After(time.Now()) {
		t.Fatal("a failed crop froze an empty inventory")
	}
	if calls != 1 {
		t.Fatalf("crop calls = %d, want 1", calls)
	}
	kept := cached.retry
	if again := app.fileCompanionFigures(canonical); len(again) != 0 {
		t.Fatalf("figures = %+v", again)
	}
	if calls != 1 {
		t.Fatal("the failed crop ran again before the probe was due")
	}
	st.mu.Lock()
	cached = st.figures[canonical]
	st.mu.Unlock()
	if !cached.retry.Equal(kept) {
		t.Fatal("the empty inventory was rebuilt before the probe was due")
	}

	fileCompanionClipPageFiguresHook = func(context.Context, string, string, int, int, string) ([]companionFigure, error) {
		calls++
		return nil, nil
	}
	st.mu.Lock()
	cached.retry = time.Now().Add(-time.Second)
	st.mu.Unlock()
	if figures := app.fileCompanionFigures(canonical); len(figures) != 0 {
		t.Fatalf("figures = %+v, want none when the crop saves nothing", figures)
	}
	st.mu.Lock()
	cached = st.figures[canonical]
	st.mu.Unlock()
	if cached == nil || !cached.retry.IsZero() {
		t.Fatal("a crop that saved nothing was scheduled to run again")
	}
	if calls != 2 {
		t.Fatalf("crop calls = %d, want the one retry after the probe", calls)
	}
	_ = app.fileCompanionFigures(canonical)
	if calls != 2 {
		t.Fatal("a finished empty crop ran again")
	}
}

func TestPaperFigureFromExtractedDropsAHighResolutionIcon(t *testing.T) {
	jpeg := paperTestJPEG(t, 225, 225)
	icon := gopdf2.ExtractedImage{
		Data:          jpeg,
		Width:         225,
		Height:        225,
		DisplayWidth:  8,
		DisplayHeight: 8,
	}
	if _, ok := paperFigureFromExtracted(1, icon); ok {
		t.Fatal("an 8pt email icon was kept as a figure")
	}
	if _, ok := fileCompanionToolFigureFromExtracted(1, icon); ok {
		t.Fatal("extract kept the 8pt email icon")
	}
	mirrored := icon
	mirrored.DisplayWidth = -8
	mirrored.DisplayHeight = 8
	if _, ok := paperFigureFromExtracted(1, mirrored); ok {
		t.Fatal("a mirrored 8pt icon was kept as a figure")
	}
	chart := icon
	chart.DisplayWidth = 220
	chart.DisplayHeight = 180
	if _, ok := paperFigureFromExtracted(1, chart); !ok {
		t.Fatal("a page-sized chart was dropped")
	}
	unknown := icon
	unknown.DisplayWidth = 0
	unknown.DisplayHeight = 0
	if _, ok := paperFigureFromExtracted(1, unknown); !ok {
		t.Fatal("a large bitmap with no placement was dropped")
	}
	scan := chart
	scan.DisplayWidth = -600
	scan.DisplayHeight = -800
	if _, ok := paperFigureFromExtracted(1, scan); ok {
		t.Fatal("a mirrored full-page scan was kept")
	}
}

func TestFileCompanionExtractDropsTemplateEmailIcon(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	icon, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 225, 225))
	if err != nil {
		t.Fatal(err)
	}
	chart, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 180))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.ImageByHolder(icon, 69, 74, &gopdf2.Rect{W: 8, H: 8}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(chart, 48, 160, &gopdf2.Rect{W: 220, H: 180}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	found, err := fileCompanionExtractPaperFigures(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].area != 220*180 {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		pages, extractErr := safeExtractPaperPDFImages(data)
		if extractErr != nil {
			t.Fatal(extractErr)
		}
		for page, images := range pages {
			for _, img := range images {
				t.Logf("page %d %dx%d display %.2fx%.2f bytes %d", page, img.Width, img.Height, img.DisplayWidth, img.DisplayHeight, len(img.Data))
			}
		}
		t.Fatalf("kept %d figures, want the 220x180 chart only", len(found))
	}
}

func TestFileCompanionPdfFiguresClipsWhenOnlyIconsRemain(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFIconAndCaption(t, pdfPath, font)
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(paperChooseFigurePages(paperFigureCaptionPages(data))) == 0 {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	clip := filepath.Join(dir, "architecture.png")
	writeCompanionFigurePNG(t, clip, 220, 160)
	prev := fileCompanionClipPageFiguresHook
	fileCompanionClipPageFiguresHook = func(ctx context.Context, source, outDir string, page, dpi int, figure string) ([]companionFigure, error) {
		if page != 1 || dpi != paperFigureCLIDPI || figure != "" {
			t.Errorf("clip args page=%d dpi=%d figure=%q", page, dpi, figure)
		}
		return []companionFigure{{path: clip, caption: "Figure 1: architecture", size: 2048}}, nil
	}
	t.Cleanup(func() { fileCompanionClipPageFiguresHook = prev })
	canonical, err := fileCompanionCanonicalPath(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 1 || figures[0].path != clip {
		t.Fatalf("figures = %+v, want the caption crop and not the email icon", figures)
	}
	if !strings.Contains(figures[0].caption, "Figure 1") {
		t.Fatalf("caption = %q", figures[0].caption)
	}
}

func writePaperPDFCaptionPages(t *testing.T, path, font string, pages int) {
	t.Helper()
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		if err := pdf.SetFont("arial", "", 12); err != nil {
			t.Fatal(err)
		}
		pdf.SetXY(48, 200)
		line := "Figure " + strconv.Itoa(i) + ": chart on this page"
		if err := pdf.Cell(&gopdf2.Rect{W: 460, H: 18}, line); err != nil {
			t.Fatal(err)
		}
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func writePaperPDFIconAndCaption(t *testing.T, path, font string) {
	t.Helper()
	icon, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 225, 225))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 12); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(icon, 69, 74, &gopdf2.Rect{W: 8, H: 8}); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 200)
	if err := pdf.Cell(&gopdf2.Rect{W: 460, H: 18}, "Figure 1: architecture of the method"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func writePaperPDFWithFigures(t *testing.T, path string) {
	t.Helper()
	large, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 180))
	if err != nil {
		t.Fatal(err)
	}
	small, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.ImageByHolder(small, 40, 700, &gopdf2.Rect{W: 24, H: 24}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(large, 40, 200, &gopdf2.Rect{W: 220, H: 180}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

// The figure sits above its caption, the way a paper page does, and a second
// drawing lower on the page stands in for a table. The crop must keep the
// figure and its caption, and leave that lower drawing on the page.
func TestFileCompanionClipKeepsTheFigureAboveTheCaption(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	if paperRasterEngines().python == "" {
		t.Skip("no pymupdf")
	}
	dir := t.TempDir()
	above := filepath.Join(dir, "above.pdf")
	writePaperPDFFigureThenCaption(t, above, font)
	assertCompanionClipIsTheFigure(t, above)
	below := filepath.Join(dir, "below.pdf")
	writePaperPDFCaptionThenFigure(t, below, font)
	assertCompanionClipIsTheFigure(t, below)
}

func assertCompanionClipIsTheFigure(t *testing.T, path string) {
	t.Helper()
	clips, err := fileCompanionClipPageFigures(context.Background(), path, t.TempDir(), 1, 72, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != 1 {
		t.Fatalf("%s clips = %d, want the one figure", path, len(clips))
	}
	if !strings.Contains(clips[0].caption, "Figure 1") || !strings.Contains(clips[0].caption, "第 1 页") {
		t.Fatalf("%s caption = %q", path, clips[0].caption)
	}
	file, err := os.Open(clips[0].path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	// A4 at 72 dpi is about 595 by 842. Taking the lower drawing as well makes
	// the clip most of the page. Taking only that lower drawing makes it narrow.
	if cfg.Height > 370 || cfg.Width < 400 {
		t.Fatalf("%s clip is %dx%d, want the wide figure and not the lower drawing", path, cfg.Width, cfg.Height)
	}
}

func writePaperPDFFigureThenCaption(t *testing.T, path, font string) {
	t.Helper()
	writePaperPDFFigureLayout(t, path, font, true)
}

func writePaperPDFCaptionThenFigure(t *testing.T, path, font string) {
	t.Helper()
	writePaperPDFFigureLayout(t, path, font, false)
}

func writePaperPDFFigureLayout(t *testing.T, path, font string, figureFirst bool) {
	t.Helper()
	left, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 130))
	if err != nil {
		t.Fatal(err)
	}
	right, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 130))
	if err != nil {
		t.Fatal(err)
	}
	lower, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 140, 90))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 14); err != nil {
		t.Fatal(err)
	}
	// The two panels are 60pt apart, wider than a stroke, and still one figure.
	if figureFirst {
		if err := pdf.ImageByHolder(left, 40, 36, &gopdf2.Rect{W: 220, H: 130}); err != nil {
			t.Fatal(err)
		}
		if err := pdf.ImageByHolder(right, 320, 36, &gopdf2.Rect{W: 220, H: 130}); err != nil {
			t.Fatal(err)
		}
		pdf.SetXY(40, 188)
	} else {
		pdf.SetXY(40, 36)
	}
	if err := pdf.Cell(&gopdf2.Rect{W: 520, H: 20}, "Figure 1: Overview of the pipeline and the gates beside it"); err != nil {
		t.Fatal(err)
	}
	if !figureFirst {
		if err := pdf.ImageByHolder(left, 40, 70, &gopdf2.Rect{W: 220, H: 130}); err != nil {
			t.Fatal(err)
		}
		if err := pdf.ImageByHolder(right, 320, 70, &gopdf2.Rect{W: 220, H: 130}); err != nil {
			t.Fatal(err)
		}
	}
	pdf.SetXY(40, 360)
	if err := pdf.Cell(&gopdf2.Rect{W: 500, H: 20}, "Body text stays on the page and is not part of the figure."); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(lower, 40, 470, &gopdf2.Rect{W: 140, H: 90}); err != nil {
		t.Fatal(err)
	}
	// A wrapped sentence is not a second caption. "Figure 1" on its own line
	// with "shows ..." under it must not take the drawing above those words.
	pdf.SetXY(40, 580)
	if err := pdf.SetFont("arial", "", 12); err != nil {
		t.Fatal(err)
	}
	if err := pdf.Cell(&gopdf2.Rect{W: 120, H: 16}, "Figure 1"); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(40, 598)
	if err := pdf.Cell(&gopdf2.Rect{W: 460, H: 16}, "shows why the lower drawing stays on the page."); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

// writePaperPDFProseLine is a page whose only text names a figure in a sentence.
func writePaperPDFProseLine(t *testing.T, path, font string) {
	t.Helper()
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 72)
	if err := pdf.Cell(&gopdf2.Rect{W: 480, H: 24}, "Figure 1 shows the method and stays prose."); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

// writePaperPDFWrappedProse splits "Figure 1 shows ..." across two lines.
func writePaperPDFWrappedProse(t *testing.T, path, font string) {
	t.Helper()
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 72)
	if err := pdf.Cell(&gopdf2.Rect{W: 120, H: 18}, "Figure 1"); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 92)
	if err := pdf.Cell(&gopdf2.Rect{W: 420, H: 18}, "shows the method and stays prose."); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func writePaperPDFWithCaption(t *testing.T, path, font string) {
	t.Helper()
	large, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 180))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 48)
	if err := pdf.Cell(&gopdf2.Rect{W: 460, H: 24}, "Figure 1: demo chart"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(large, 48, 120, &gopdf2.Rect{W: 220, H: 180}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func paperTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 160, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() < paperFigureMinBytes {
		t.Fatalf("jpeg too small: %d", encoded.Len())
	}
	return encoded.Bytes()
}
