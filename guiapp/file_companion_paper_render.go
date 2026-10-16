package guiapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func fileCompanionRasterizePaperFigures(source, destDir, sessionID string) []string {
	source = strings.TrimSpace(source)
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil
	}
	chosen := paperChooseFigurePages(paperFigureCaptionPages(data))
	if len(chosen) == 0 {
		return nil
	}
	dir := filepath.Join(destDir, "figures")
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

func paperFigureCaptionPages(data []byte) []paperFigurePage {
	if len(data) == 0 {
		return nil
	}
	limit := paperSourcePageCount(data)
	if limit <= 0 {
		limit = paperFigurePageLimit
	}
	var found []paperFigurePage
	for i := 0; i < limit; i++ {
		text, err := safeExtractPaperPageText(data, i)
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
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
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
				if probePyMuPDF(ctx, match, seen) {
					return match
				}
			}
			continue
		}
		if filepath.IsAbs(name) {
			if _, err := os.Stat(name); err != nil {
				continue
			}
		} else {
			path, err := exec.LookPath(name)
			if err != nil {
				continue
			}
			name = path
		}
		if probePyMuPDF(ctx, name, seen) {
			return name
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
	// Matches the companion's own page render: on a loaded machine a first
	// PyMuPDF call (importing the module, warming the page tree) takes well
	// past half a minute, and a timeout here degrades the answer to extracted
	// images rather than failing. Each engine tries the whole page set in
	// turn, but only fills the pages the earlier engine left blank — a
	// partial PyMuPDF run must not be redone, and a page it skipped must not
	// be dropped. Each attempt gets its own slice of the caller's budget so
	// the poppler/mutool fallbacks still run after a starving PyMuPDF attempt.
	ctx, cancel := context.WithTimeout(context.Background(), fileCompanionRenderBudget)
	defer cancel()
	if len(pages) > paperFigureMaxCount {
		pages = pages[:paperFigureMaxCount]
	}
	engine := paperRasterEngines()
	if engine.python != "" {
		pyCtx, pyCancel := renderAttemptContext(ctx)
		err := paperRenderPyMuPDF(pyCtx, engine.python, source, dir, pages)
		pyCancel()
		if err != nil {
			logFileCompanion("paper figures pymupdf attempt failed session=%s: %v", sessionID, err)
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
	return paperFigurePNGLines(dir, pages)
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

// paperRenderPopplerPages renders one PNG per page with pdftocairo or
// pdftoppm — both take the same flags, see popplerRasterArgs.
func paperRenderPopplerPages(ctx context.Context, tool, source, dir string, pages []int) error {
	var failed error
	for _, page := range pages {
		base := filepath.Join(dir, fmt.Sprintf("p%d-1", page))
		args := popplerRasterArgs(source, base, page, paperFigureCLIDPI)
		cmd := exec.CommandContext(ctx, tool, args...)
		hideCommandWindow(cmd)
		if err := cmd.Run(); err != nil && failed == nil {
			failed = err
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
		if err := cmd.Run(); err != nil && failed == nil {
			failed = err
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
