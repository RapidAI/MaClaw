package guiapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

const paperFigurePageLimit = 80

// paperFigureCaptionRe matches a figure caption at the start of a line.
// A letter before the number, as in Figure C.1, marks an appendix figure.
var paperFigureCaptionRe = regexp.MustCompile(`(?im)^\s*(?:figure|fig\.?|图)\s*([A-Z])?\.?\s*(\d+(?:\.\d+)?)`)

const paperFigurePyScript = `import os
import re
import sys
import pymupdf

pdf_path = sys.argv[1]
out_dir = sys.argv[2]
pages = sys.argv[3]
caption_re = re.compile(r"(?i)^\s*(?:figure|fig\.?|\u56fe)\s*([A-Za-z])?\.?\s*\d")
doc = pymupdf.open(pdf_path)
saved = 0

def overlaps(a, b):
    return a.x0 < b.x1 and a.x1 > b.x0 and a.y0 < b.y1 and a.y1 > b.y0

for token in pages.split(","):
    if saved >= 4:
        break
    token = token.strip()
    if not token:
        continue
    try:
        page_no = int(token)
        page = doc[page_no - 1]
        pw = page.rect.width
        ph = page.rect.height
        rects = []
        for info in page.get_image_info():
            bbox = info.get("bbox")
            if not bbox:
                continue
            rect = pymupdf.Rect(bbox)
            if rect.width >= 80 and rect.height >= 80:
                rects.append(rect)
        for drawing in page.get_drawings():
            raw = drawing.get("rect")
            if raw is None:
                continue
            rect = pymupdf.Rect(raw)
            if rect.width < 2 or rect.height < 2:
                continue
            if rect.width > 0.96 * pw or rect.height > 0.85 * ph:
                continue
            if rect.width > 0.9 * pw and rect.height < 4:
                continue
            rects.append(rect)
        if not rects:
            continue
        graphic = rects[0]
        for rect in rects[1:]:
            graphic |= rect
        # Pad the drawing itself. Do not pad again after the caption, or the
        # next body line is sliced in half.
        clip = (graphic + (-6, -6, 6, 4)) & page.rect
        for block in page.get_text("dict").get("blocks", []):
            if block.get("type") != 0:
                continue
            for line in block.get("lines", []):
                spans = line.get("spans") or []
                text = "".join(span.get("text", "") for span in spans).strip()
                if not text or "bbox" not in line:
                    continue
                bb = pymupdf.Rect(line["bbox"])
                gap = bb.y0 - graphic.y1
                across = bb.x1 > graphic.x0 and bb.x0 < graphic.x1
                short = len(text) <= 40 and across and -4 <= gap <= 18
                caption = across and 0 <= gap <= 42 and caption_re.match(text) is not None
                if overlaps(bb, graphic) or short or caption:
                    clip |= bb
        clip = clip & page.rect
        if clip is None or clip.width < 40 or clip.height < 24:
            continue
        pix = page.get_pixmap(matrix=pymupdf.Matrix(130.0 / 72.0, 130.0 / 72.0), clip=clip, alpha=False)
        path = os.path.join(out_dir, "p%d-1.png" % page_no)
        pix.save(path)
        saved += 1
    except Exception:
        continue
`

type paperFigurePage struct {
	page     int
	appendix bool
}

type paperRasterEngine struct {
	python string
	cairo  string
	ppm    string
	mutool string
}

// empty reports whether no raster engine exists at all — the only case where
// "install one" is the right advice, as opposed to a real render failure.
func (e paperRasterEngine) empty() bool {
	return e.python == "" && e.cairo == "" && e.ppm == "" && e.mutool == ""
}

var (
	paperRasterMu       sync.Mutex
	paperRasterCached   paperRasterEngine
	paperRasterProbedAt time.Time
)

// paperRasterNegativeTTL bounds how long an empty engine slot is trusted. A
// user who installs pymupdf or poppler while the app runs must not need a
// restart for the companion to find it.
const paperRasterNegativeTTL = 5 * time.Minute

// paperRasterEngines probes once and caches per field: a slot that came back
// with a path is trusted forever (a binary does not move under a running
// app), while empty slots are re-probed after paperRasterNegativeTTL. The
// per-field granularity is the point — a machine that has poppler but no
// pymupdf must keep re-checking for pymupdf after the user installs it,
// instead of freezing at "poppler is good enough" forever.
func paperRasterEngines() paperRasterEngine {
	paperRasterMu.Lock()
	defer paperRasterMu.Unlock()
	cached := paperRasterCached
	if cached.python != "" && cached.cairo != "" && cached.ppm != "" && cached.mutool != "" {
		return cached
	}
	if !paperRasterProbedAt.IsZero() && time.Since(paperRasterProbedAt) < paperRasterNegativeTTL {
		return cached
	}
	paperRasterProbedAt = time.Now()
	if cached.python == "" {
		cached.python = paperFindPyMuPDF()
	}
	if cached.cairo == "" {
		cached.cairo = paperFindPDFTool("pdftocairo")
	}
	if cached.ppm == "" {
		cached.ppm = paperFindPDFTool("pdftoppm")
	}
	if cached.mutool == "" {
		cached.mutool = paperFindPDFTool("mutool")
	}
	paperRasterCached = cached
	return cached
}

// paperReadFigureSource loads the PDF for caption scanning. Tests replace it
// to prove a fresh figure set does not open the file again.
var paperReadFigureSource = os.ReadFile

func fileCompanionRasterizePaperFigures(source, destDir, sessionID string) []string {
	source = strings.TrimSpace(source)
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return nil
	}
	dir := filepath.Join(destDir, "figures")
	// The caption scan reads the whole PDF. A follow-up turn whose rasters
	// still belong to this file skips that read.
	if lines := paperFreshFigureLines(source, dir); len(lines) > 0 {
		return lines
	}
	data, err := paperReadFigureSource(source)
	if err != nil {
		return nil
	}
	chosen := paperChooseFigurePages(paperFigureCaptionPages(data))
	if len(chosen) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return nil
	}
	return paperRenderFigurePages(source, dir, chosen, sessionID)
}

func paperFigureCaptionHit(line string) (appendix bool, ok bool) {
	match := paperFigureCaptionRe.FindStringSubmatch(line)
	if match == nil {
		return false, false
	}
	return match[1] != "", true
}

// paperExtractPageText is the per-page text read. Tests replace it; the
// production function re-parses the file, so the scan stops once it has
// enough body figures to fill the reply.
var paperExtractPageText = safeExtractPaperPageText

func paperFigureCaptionPages(data []byte) []paperFigurePage {
	if len(data) == 0 {
		return nil
	}
	limit := paperSourcePageCount(data)
	if limit <= 0 {
		limit = paperFigurePageLimit
	}
	var found []paperFigurePage
	body := 0
	for i := 0; i < limit; i++ {
		text, err := paperExtractPageText(data, i)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "out of range") {
				break
			}
			continue
		}
		seen := false
		appendix := false
		for _, line := range strings.Split(text, "\n") {
			lineAppendix, ok := paperFigureCaptionHit(line)
			if !ok {
				continue
			}
			seen = true
			appendix = lineAppendix
			if !lineAppendix {
				break
			}
		}
		if !seen {
			continue
		}
		found = append(found, paperFigurePage{page: i + 1, appendix: appendix})
		// Later body figures sort after these and are cut by the reply cap.
		// Appendix figures only fill slots the body did not take.
		if !appendix {
			body++
			if body >= paperFigureMaxCount {
				break
			}
		}
	}
	return found
}

func paperChooseFigurePages(pages []paperFigurePage) []int {
	if len(pages) == 0 {
		return nil
	}
	sorted := append([]paperFigurePage(nil), pages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].appendix != sorted[j].appendix {
			return !sorted[i].appendix
		}
		return sorted[i].page < sorted[j].page
	})
	var out []int
	seen := map[int]bool{}
	for _, page := range sorted {
		if page.page <= 0 || seen[page.page] {
			continue
		}
		seen[page.page] = true
		out = append(out, page.page)
	}
	return out
}

func paperSourcePageCount(data []byte) (n int) {
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	count, err := gopdf2.GetSourcePDFPageCountV2(data)
	if err != nil || count <= 0 {
		return 0
	}
	if count > 500 {
		return 500
	}
	return count
}

func safeExtractPaperPageText(data []byte, page int) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			err = fmt.Errorf("extract pdf text panicked")
		}
	}()
	return gopdf2.ExtractPageText(data, page)
}

// paperPythonCandidates lists the Pythons worth probing for pymupdf, in order.
// goos is a parameter so the darwin list is assertable from a Windows test;
// home is the user's home directory ("" in tests) and only contributes the
// per-user installs — pyenv shims and versioned environments, which live
// under it and nowhere else.
func paperPythonCandidates(goos, home string) []string {
	candidates := []string{"python", "python3"}
	switch goos {
	case "darwin":
		// A GUI app launched from Finder inherits launchd's stripped PATH
		// (/usr/bin:/bin:/usr/sbin:/sbin), so LookPath never sees the
		// Homebrew, conda or user-installed Pythons that actually carry
		// pymupdf. Probe those absolute locations directly.
		candidates = append(candidates,
			"/opt/homebrew/bin/python3",
			"/usr/local/bin/python3",
			"/usr/bin/python3",
			"/opt/homebrew/Caskroom/miniconda/base/bin/python3",
			"/opt/homebrew/Caskroom/anaconda/base/bin/python3",
			"/opt/homebrew/anaconda3/bin/python3",
			"/opt/homebrew/miniconda3/bin/python3",
			"/opt/anaconda3/bin/python3",
			"/opt/miniconda3/bin/python3",
			"/usr/local/anaconda3/bin/python3",
			"/usr/local/miniconda3/bin/python3",
			// python.org framework builds and pyenv version dirs are globs;
			// paperFindPyMuPDF expands them.
			"/Library/Frameworks/Python.framework/Versions/*/bin/python3",
		)
		if home != "" {
			candidates = append(candidates,
				filepath.ToSlash(filepath.Join(home, ".pyenv", "shims", "python3")),
				filepath.ToSlash(filepath.Join(home, ".pyenv", "versions", "*", "bin", "python3")),
				filepath.ToSlash(filepath.Join(home, ".local", "bin", "python3")),
			)
		}
	case "linux":
		// Desktop-launched apps can miss /usr/local/bin in PATH too.
		candidates = append(candidates,
			"/usr/local/bin/python3",
			"/usr/bin/python3",
		)
		if home != "" {
			candidates = append(candidates,
				filepath.ToSlash(filepath.Join(home, ".pyenv", "shims", "python3")),
				filepath.ToSlash(filepath.Join(home, ".pyenv", "versions", "*", "bin", "python3")),
			)
		}
	}
	return candidates
}

// paperPDFToolCandidates lists the absolute locations a poppler CLI may live
// when it is not on this process's PATH. The paths are POSIX literals joined
// with "/" — not filepath.Join, which would emit separators of the host OS.
// goos is a parameter so tests can assert the darwin list from any platform.
func paperPDFToolCandidates(goos, name string) []string {
	switch goos {
	case "darwin":
		candidates := []string{
			"/opt/homebrew/bin/" + name,
			"/opt/homebrew/opt/poppler/bin/" + name,
			"/usr/local/bin/" + name,
		}
		if name == "mutool" {
			// mupdf lives in its own keg, not under the poppler one; the
			// /opt/homebrew/bin symlink covers the normal install, this
			// covers a keg-only one.
			candidates = append(candidates, "/opt/homebrew/opt/mupdf/bin/"+name)
		}
		return candidates
	case "linux":
		return []string{
			"/usr/local/bin/" + name,
			"/usr/bin/" + name,
		}
	}
	return nil
}

// paperPyMuPDFProbeBudget bounds the whole pymupdf search. Each candidate
// gets its own short timeout below; the search as a whole must not hold a
// turn hostage on a machine with many slow-starting Pythons — the probe runs
// outside the render budget, so without a cap it can double the worst case.
const paperPyMuPDFProbeBudget = 20 * time.Second

func paperFindPyMuPDF() string {
	ctx, cancel := context.WithTimeout(context.Background(), paperPyMuPDFProbeBudget)
	defer cancel()
	return paperSearchPyMuPDF(ctx, 0)
}

// paperQuickPyMuPDFBudget is the most the paper path will spend looking for
// pymupdf before drawing with Quartz. Three existing interpreters are enough
// to see Homebrew, the system Python, and one pyenv; the long search stays on
// the no-renderer path.
const paperQuickPyMuPDFBudget = 4 * time.Second

var (
	paperQuickMu       sync.Mutex
	paperQuickPython   string
	paperQuickProbedAt time.Time
	// Replaced by tests so a figure render does not exec a machine Python.
	paperQuickPyMuPDF = findPaperQuickPyMuPDF
)

// findPaperQuickPyMuPDF returns a Python that imports pymupdf, or "". A hit
// already stored by the full probe is reused. Otherwise at most three
// interpreters are tried, and the answer is kept for paperRasterNegativeTTL
// so a Mac without pymupdf does not pay the import on every figure. The
// caller's context bounds it: a cancelled turn must not sit out the budget.
func findPaperQuickPyMuPDF(ctx context.Context) string {
	paperRasterMu.Lock()
	known := paperRasterCached.python
	paperRasterMu.Unlock()
	if known != "" {
		return known
	}
	paperQuickMu.Lock()
	defer paperQuickMu.Unlock()
	if !paperQuickProbedAt.IsZero() && time.Since(paperQuickProbedAt) < paperRasterNegativeTTL {
		return paperQuickPython
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, paperQuickPyMuPDFBudget)
	defer cancel()
	found := paperSearchPyMuPDF(probeCtx, 3)
	// A cancel is not evidence that pymupdf is missing. Caching that would
	// hide a real interpreter from the next turn.
	if found != "" || probeCtx.Err() == nil {
		paperQuickPython = found
		paperQuickProbedAt = time.Now()
	}
	return found
}

// paperPythonSkipsProbe reports interpreters that must not be executed.
// /usr/bin/python3 on macOS is Apple's command-line-tools stub: running it
// from a GUI app raises the "install developer tools" dialog, and it does not
// carry pymupdf.
func paperPythonSkipsProbe(python string) bool {
	if paperPythonIsSystemStub(python) {
		return true
	}
	// A Homebrew or /usr/local name can be a symlink to Apple's stub. Executing
	// that stub from a GUI app opens the command-line-tools install dialog.
	resolved, err := filepath.EvalSymlinks(python)
	if err != nil || resolved == python {
		return false
	}
	return paperPythonIsSystemStub(resolved)
}

func paperPythonIsSystemStub(python string) bool {
	// path.Clean, not filepath.Clean: the stub paths are POSIX literals, and
	// filepath.Clean on Windows rewrites them to backslashes so the comparison
	// never matches.
	switch path.Clean(filepath.ToSlash(python)) {
	case "/usr/bin/python", "/usr/bin/python3":
		return true
	default:
		return false
	}
}

// paperSearchPyMuPDF walks the usual Python locations. maxProbes caps how many
// interpreters are executed; zero means no cap. Missing paths are not executions.
func paperSearchPyMuPDF(ctx context.Context, maxProbes int) string {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	probes := 0
	try := func(python string) (found string, stop bool) {
		if ctx.Err() != nil {
			return "", true
		}
		if paperPythonSkipsProbe(python) {
			return "", false
		}
		if seen[python] {
			return "", false
		}
		if maxProbes > 0 && probes >= maxProbes {
			return "", true
		}
		probes++
		if probePyMuPDF(ctx, python, seen) {
			return python, true
		}
		return "", false
	}
	for _, name := range paperPythonCandidates(runtime.GOOS, home) {
		if ctx.Err() != nil {
			return ""
		}
		// Versioned installs (pyenv, python.org framework) arrive as globs;
		// expand them into the concrete interpreters they currently name.
		if strings.ContainsAny(name, "*?[") {
			matches, _ := filepath.Glob(name)
			for _, match := range matches {
				if info, err := os.Stat(match); err != nil || info.IsDir() {
					continue
				}
				if found, stop := try(match); stop {
					return found
				}
			}
			continue
		}
		if filepath.IsAbs(name) {
			if info, err := os.Stat(name); err != nil || info.IsDir() {
				continue
			}
		} else {
			path, err := exec.LookPath(name)
			if err != nil {
				continue
			}
			name = path
		}
		if found, stop := try(name); stop {
			return found
		}
	}
	return ""
}

// probePyMuPDF asks one interpreter whether pymupdf imports. seen dedups the
// candidates that resolve to the same binary (a LookPath hit and an absolute
// path usually do).
func probePyMuPDF(ctx context.Context, python string, seen map[string]bool) bool {
	if seen[python] {
		return false
	}
	seen[python] = true
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, python, "-c", "import pymupdf")
	hideCommandWindow(cmd)
	return cmd.Run() == nil
}

func paperFindPDFTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, path := range paperPDFToolCandidates(runtime.GOOS, name) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	var found []string
	for _, pattern := range []string{
		filepath.Join(`C:\texlive`, "*", "bin", "windows", name+".exe"),
		filepath.Join(`D:\texlive`, "*", "bin", "windows", name+".exe"),
	} {
		matches, _ := filepath.Glob(pattern)
		found = append(found, matches...)
	}
	if len(found) == 0 {
		return ""
	}
	sort.Strings(found)
	return found[len(found)-1]
}

func paperRenderFigurePages(source, dir string, pages []int, sessionID string) []string {
	if len(pages) > paperFigureMaxCount {
		pages = pages[:paperFigureMaxCount]
	}
	// A follow-up turn on the same PDF must not open it again. The figures
	// directory is per session, so a raster is reusable only when it was drawn
	// from this file and is newer than it. Anything else is removed first:
	// a large PNG left by the previous document would otherwise count as done.
	// Recording the page list here is what lets the next turn skip the caption
	// scan: a fresh set used to return without writing pages.txt, so every
	// later turn opened the PDF again.
	stale := paperStaleFigurePages(source, dir, pages)
	if len(stale) == 0 {
		if len(pages) == 0 {
			return nil
		}
		return paperFinishFigurePages(source, dir, pages)
	}
	// On a loaded machine a first PyMuPDF call (importing the module, warming
	// the page tree) takes well past half a minute, and a timeout here degrades
	// the answer to extracted images rather than failing. Each engine tries the
	// pages still missing, so a partial run is not redone and a skipped page is
	// not dropped. Each external attempt gets its own slice of the caller's
	// budget so the poppler/mutool fallbacks still run after a starving
	// PyMuPDF attempt.
	ctx, cancel := context.WithTimeout(context.Background(), fileCompanionRenderBudget)
	defer cancel()
	if paperFigureSourceMatches(source, dir) {
		for _, page := range stale {
			_ = os.Remove(filepath.Join(dir, fmt.Sprintf("p%d-1.png", page)))
		}
	} else {
		// The marker names one PDF for the whole directory. Dropping only the
		// pages in this request would leave the previous document's other
		// pages in place, and a later request for those page numbers would
		// treat them as this PDF once the marker is rewritten.
		paperRemoveFigureRasters(dir)
		_ = os.Remove(filepath.Join(dir, paperFigureSourceMarker))
		_ = os.Remove(filepath.Join(dir, paperFigurePagesMarker))
	}
	// Quartz is the fast full-page path. A short pymupdf lookup runs first so a
	// machine that already has it still gets the cropped figure; the lookup is
	// capped, and a miss does not start the 20s search. External engines run
	// only for pages both of those left blank.
	if fileCompanionNativePDFRender != nil {
		if python := paperQuickPyMuPDF(ctx); python != "" && ctx.Err() == nil {
			if missing := paperMissingFigurePages(dir, pages); len(missing) > 0 {
				pyCtx, pyCancel := renderAttemptContext(ctx)
				err := paperRenderPyMuPDF(pyCtx, python, source, dir, missing)
				pyCancel()
				if err != nil {
					// A timeout kills the process after it has already written
					// a large, truncated PNG. The size check would keep it.
					paperDiscardFigurePages(dir, missing)
					if ctx.Err() == nil {
						logFileCompanion("paper figures pymupdf attempt failed session=%s: %v", sessionID, err)
					}
				}
			}
		}
		if missing := paperMissingFigurePages(dir, pages); len(missing) > 0 && ctx.Err() == nil {
			if err := paperRenderNativePages(ctx, source, dir, missing); err != nil && ctx.Err() == nil {
				logFileCompanion("paper figures native attempt failed session=%s: %v", sessionID, err)
			}
		}
		if len(paperMissingFigurePages(dir, pages)) == 0 || ctx.Err() != nil {
			return paperFinishFigurePages(source, dir, pages)
		}
	}
	engine := paperRasterEngines()
	if engine.python != "" {
		if missing := paperMissingFigurePages(dir, pages); len(missing) > 0 {
			pyCtx, pyCancel := renderAttemptContext(ctx)
			err := paperRenderPyMuPDF(pyCtx, engine.python, source, dir, missing)
			pyCancel()
			if err != nil {
				paperDiscardFigurePages(dir, missing)
				logFileCompanion("paper figures pymupdf attempt failed session=%s: %v", sessionID, err)
			}
		}
	}
	poppler := engine.cairo
	if poppler == "" {
		poppler = engine.ppm
	}
	if poppler != "" {
		if missing := paperMissingFigurePages(dir, pages); len(missing) > 0 {
			// One independent window for the whole poppler pass, not a slice
			// of what PyMuPDF left — see renderAttemptContext.
			popplerCtx, popplerCancel := renderAttemptContext(ctx)
			err := paperRenderPopplerPages(popplerCtx, poppler, source, dir, missing)
			popplerCancel()
			if err != nil {
				logFileCompanion("paper figures poppler attempt failed session=%s: %v", sessionID, err)
			}
		}
	}
	// mutool is the rarest engine: only render the pages the earlier engines
	// did not land (a partial run must not be redone, and a skipped page must
	// not be dropped).
	if engine.mutool != "" {
		if missing := paperMissingFigurePages(dir, pages); len(missing) > 0 {
			mCtx, mCancel := renderAttemptContext(ctx)
			err := paperRenderMutoolPages(mCtx, engine.mutool, source, dir, missing)
			mCancel()
			if err != nil {
				logFileCompanion("paper figures mutool attempt failed session=%s: %v", sessionID, err)
			}
		}
	}
	return paperFinishFigurePages(source, dir, pages)
}

// paperFigureSourceMarker records which PDF the rasters in this directory
// were drawn from. The directory is shared by every document in the session.
const paperFigureSourceMarker = "source.txt"

// paperFigurePagesMarker records which pages those rasters cover, so a
// follow-up turn can reuse them without scanning the PDF for captions again.
const paperFigurePagesMarker = "pages.txt"

// paperFigureRasterName matches the page rasters this package writes.
var paperFigureRasterName = regexp.MustCompile(`^p\d+-1\.png$`)

func paperFigureSourceMatches(source, dir string) bool {
	recorded, err := os.ReadFile(filepath.Join(dir, paperFigureSourceMarker))
	return err == nil && strings.TrimSpace(string(recorded)) == source
}

func paperDiscardFigurePages(dir string, pages []int) {
	for _, page := range pages {
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("p%d-1.png", page)))
	}
}

// paperRemoveFigureRasters deletes every page raster in the session figures
// directory. It runs when the directory belongs to a different PDF, so a page
// this request did not ask for cannot be reused later under the new marker.
func paperRemoveFigureRasters(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !paperFigureRasterName.MatchString(entry.Name()) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// paperStaleFigurePages returns the pages that still need a raster of this
// PDF. A page left by another document in the same session is stale even when
// the file is large and newer. Equal timestamps do not count as fresh: a
// render that lands in the same second as a rewrite would otherwise be kept.
func paperStaleFigurePages(source, dir string, pages []int) []int {
	if len(pages) == 0 {
		return nil
	}
	same := paperFigureSourceMatches(source, dir)
	var srcTime time.Time
	if same {
		info, err := os.Stat(source)
		if err != nil {
			same = false
		} else {
			srcTime = info.ModTime()
		}
	}
	var stale []int
	for _, page := range pages {
		if !same {
			stale = append(stale, page)
			continue
		}
		info, err := os.Stat(filepath.Join(dir, fmt.Sprintf("p%d-1.png", page)))
		if err != nil || info.Size() <= paperFigureMinBytes || !info.ModTime().After(srcTime) {
			stale = append(stale, page)
		}
	}
	return stale
}

// paperFinishFigurePages remembers the source once every requested page is on
// disk. A partial run leaves the marker alone so the next turn redraws.
func paperFinishFigurePages(source, dir string, pages []int) []string {
	if len(paperMissingFigurePages(dir, pages)) == 0 {
		_ = os.WriteFile(filepath.Join(dir, paperFigureSourceMarker), []byte(source), 0o644)
		_ = os.WriteFile(filepath.Join(dir, paperFigurePagesMarker), []byte(paperFormatFigurePages(pages)), 0o644)
	}
	return paperFigurePNGLines(dir, pages)
}

func paperFormatFigurePages(pages []int) string {
	parts := make([]string, len(pages))
	for i, page := range pages {
		parts[i] = strconv.Itoa(page)
	}
	return strings.Join(parts, ",")
}

func paperReadFigurePages(dir string) []int {
	data, err := os.ReadFile(filepath.Join(dir, paperFigurePagesMarker))
	if err != nil {
		return nil
	}
	fields := strings.Split(string(data), ",")
	var pages []int
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		page, err := strconv.Atoi(field)
		if err != nil || page < 1 {
			return nil
		}
		pages = append(pages, page)
		if len(pages) == paperFigureMaxCount {
			break
		}
	}
	return pages
}

// paperFreshFigureLines returns the cached rasters when they still belong to
// this PDF. A nil result means the caller must scan the file.
func paperFreshFigureLines(source, dir string) []string {
	pages := paperReadFigurePages(dir)
	if len(pages) == 0 || !paperFigureSourceMatches(source, dir) {
		return nil
	}
	if len(paperStaleFigurePages(source, dir, pages)) > 0 {
		return nil
	}
	lines := paperFigurePNGLines(dir, pages)
	if len(lines) != len(pages) {
		return nil
	}
	return lines
}

// paperMissingFigurePages returns the pages whose raster output is not yet on
// disk (or is suspiciously small), so a fallback engine fills exactly and
// only the gaps the first engine left.
func paperMissingFigurePages(dir string, pages []int) []int {
	var missing []int
	for _, page := range pages {
		path := filepath.Join(dir, fmt.Sprintf("p%d-1.png", page))
		info, err := os.Stat(path)
		if err != nil || info.Size() <= paperFigureMinBytes {
			missing = append(missing, page)
		}
	}
	return missing
}

func paperRenderPyMuPDF(ctx context.Context, python, source, dir string, pages []int) error {
	script, err := os.CreateTemp("", "paper-fig-*.py")
	if err != nil {
		return err
	}
	name := script.Name()
	defer os.Remove(name)
	if _, err := script.WriteString(paperFigurePyScript); err != nil {
		script.Close()
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	list := make([]string, len(pages))
	for i, page := range pages {
		list[i] = strconv.Itoa(page)
	}
	cmd := exec.CommandContext(ctx, python, name, source, dir, strings.Join(list, ","))
	hideCommandWindow(cmd)
	_, err = cmd.Output()
	return err
}

// paperFigureCLIDPI is the raster resolution the CLI engines use for figure
// pages. The pymupdf script embeds its own (130) and stays independent.
const paperFigureCLIDPI = 110

// paperRenderNativePages draws each missing figure page with the OS PDF
// renderer. A nil renderer reports success and writes nothing, so callers
// can invoke it unconditionally. A failed page is removed: paperMissingFigurePages
// treats any file larger than paperFigureMinBytes as done, and a partial PNG
// would hide the poppler/mutool fallback.
func paperRenderNativePages(ctx context.Context, source, dir string, pages []int) error {
	render := fileCompanionNativePDFRender
	if render == nil || len(pages) == 0 {
		return nil
	}
	// Quartz opens the document once for the whole set. A test double keeps
	// the per-page loop below.
	if handled, err := fileCompanionQuartzRenderPages(ctx, source, dir, pages, paperFigureCLIDPI); handled {
		return err
	}
	var failed error
	for _, page := range pages {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		out := filepath.Join(dir, fmt.Sprintf("p%d-1.png", page))
		err := render(source, out, page, paperFigureCLIDPI)
		if err == nil {
			info, statErr := os.Stat(out)
			if statErr != nil || info.Size() <= paperFigureMinBytes {
				err = fmt.Errorf("native pdf renderer produced no output for page %d", page)
			}
		}
		if err != nil {
			_ = os.Remove(out)
			if failed == nil {
				failed = err
			}
		}
	}
	return failed
}

// paperRenderPopplerPages renders one PNG per page with pdftocairo or
// pdftoppm — both take the same flags, see popplerRasterArgs.
func paperRenderPopplerPages(ctx context.Context, tool, source, dir string, pages []int) error {
	var failed error
	for _, page := range pages {
		base := filepath.Join(dir, fmt.Sprintf("p%d-1", page))
		args := popplerRasterArgs(source, base, page, paperFigureCLIDPI)
		cmd := exec.CommandContext(ctx, tool, args...)
		hideCommandWindow(cmd)
		if err := cmd.Run(); err != nil {
			// A non-zero exit can still leave a large PNG. The next engine
			// treats any file over the size floor as done.
			_ = os.Remove(base + ".png")
			if failed == nil {
				failed = err
			}
		}
	}
	return failed
}

// paperRenderMutoolPages renders the same p<page>-1.png layout with mupdf's
// mutool, whose output is named exactly by -o for a single page.
func paperRenderMutoolPages(ctx context.Context, tool, source, dir string, pages []int) error {
	var failed error
	for _, page := range pages {
		out := filepath.Join(dir, fmt.Sprintf("p%d-1.png", page))
		args := mutoolRasterArgs(source, out, page, paperFigureCLIDPI)
		cmd := exec.CommandContext(ctx, tool, args...)
		hideCommandWindow(cmd)
		if err := cmd.Run(); err != nil {
			_ = os.Remove(out)
			if failed == nil {
				failed = err
			}
		}
	}
	return failed
}

func paperFigurePNGLines(dir string, pages []int) []string {
	var lines []string
	for _, page := range pages {
		path := filepath.Join(dir, fmt.Sprintf("p%d-1.png", page))
		info, err := os.Stat(path)
		if err != nil || info.Size() <= paperFigureMinBytes {
			continue
		}
		lines = append(lines, fmt.Sprintf("![原文第%d页](%s)", page, filepath.ToSlash(path)))
		if len(lines) == paperFigureMaxCount {
			break
		}
	}
	return lines
}
