package guiapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
}

var (
	paperRasterOnce   sync.Once
	paperRasterCached paperRasterEngine
)

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

func paperRasterEngines() paperRasterEngine {
	paperRasterOnce.Do(func() {
		paperRasterCached.python = paperFindPyMuPDF()
		paperRasterCached.cairo = paperFindPDFTool("pdftocairo")
		paperRasterCached.ppm = paperFindPDFTool("pdftoppm")
	})
	return paperRasterCached
}

func paperFindPyMuPDF() string {
	for _, name := range []string{"python", "python3"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		cmd := exec.CommandContext(ctx, path, "-c", "import pymupdf")
		hideCommandWindow(cmd)
		err = cmd.Run()
		cancel()
		if err == nil {
			return path
		}
	}
	return ""
}

func paperFindPDFTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	engine := paperRasterEngines()
	var pyErr error
	if engine.python != "" {
		pyErr = paperRenderPyMuPDF(ctx, engine.python, source, dir, pages)
		if pyErr == nil {
			return paperFigurePNGLines(dir, pages)
		}
		logFileCompanion("paper figures failed session=%s", sessionID)
	}
	tool := engine.cairo
	if tool == "" {
		tool = engine.ppm
	}
	if tool == "" {
		return nil
	}
	if len(pages) > paperFigureMaxCount {
		pages = pages[:paperFigureMaxCount]
	}
	if err := paperRenderCairoPages(ctx, tool, source, dir, pages); err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
	}
	return paperFigurePNGLines(dir, pages)
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

func paperRenderCairoPages(ctx context.Context, tool, source, dir string, pages []int) error {
	var failed error
	for _, page := range pages {
		base := filepath.Join(dir, fmt.Sprintf("p%d-1", page))
		cmd := exec.CommandContext(ctx, tool, "-png", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-r", "110", "-singlefile", source, base)
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
