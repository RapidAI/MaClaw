package guiapp

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
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
