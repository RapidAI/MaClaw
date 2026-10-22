package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// paperFigureCaptionLineRe matches a caption at the start of a line.
// "Figure 1: title", "Fig. 2.", "Figure 1 - chart", and "图1 标题" count.
// "Figure 1 shows" is a sentence. A sentence must not block the whole page.
var paperFigureCaptionLineRe = regexp.MustCompile(`(?im)(?:^\s*(?:figure|fig\.?)\s*[A-Za-z]?\.?\s*\d+(?:\.\d+)?\s*[:：.．\-—–]|^\s*(?:figure|fig\.?)\s*[A-Za-z]?\.?\s*\d+(?:\.\d+)?\s*$|^\s*图\s*[A-Za-z]?\.?\s*\d)`)

// paperFigureCaptionBareRe is a line that is only "Figure 1" or "Fig. 2".
// The following line decides whether that is a caption or a wrapped sentence.
var paperFigureCaptionBareRe = regexp.MustCompile(`(?im)^\s*(?:figure|fig\.?)\s*[A-Za-z]?\.?\s*\d+(?:\.\d+)?\s*$`)

// fileCompanionTextHasFigureCaption reports a real caption in extracted page
// text. "Figure 1: title" and "图1 标题" count. A bare "Figure 1" whose next
// line continues the sentence, as in a wrap of "Figure 1 shows ...", does not.
func fileCompanionTextHasFigureCaption(text string) bool {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !paperFigureCaptionLineRe.MatchString(line) {
			continue
		}
		if figureCaptionLineContinues(line, lines, i) {
			continue
		}
		return true
	}
	return false
}

func figureCaptionLineContinues(line string, lines []string, index int) bool {
	if !paperFigureCaptionBareRe.MatchString(line) || index+1 >= len(lines) {
		return false
	}
	next := strings.TrimSpace(strings.TrimRight(lines[index+1], "\r"))
	if next == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(next)
	return unicode.IsLower(r)
}

// fileCompanionClipPageFiguresHook replaces the crop in tests.
// A nil hook runs PyMuPDF. An empty result means this page has no separable
// figure, and the caller may still render the whole page.
var fileCompanionClipPageFiguresHook func(ctx context.Context, source, dir string, page, dpi int, figure string) ([]companionFigure, error)

// fileCompanionFigureClipPyScript crops each figure on one page.
// The graphic is the cluster that sits against that figure's caption.
// Drawings that belong to a later table stay out: unioning every vector on
// the page is what turned Figure 1 into a screenshot of the whole page.
const fileCompanionFigureClipPyScript = `import json
import os
import re
import sys
import pymupdf

pdf_path = sys.argv[1]
out_dir = sys.argv[2]
page_no = int(sys.argv[3])
dpi = float(sys.argv[4])
want = sys.argv[5].strip()
caption_re = re.compile(r"(?i)^\s*(?:figure|fig\.?|图)\s*([A-Za-z])?\.?\s*(\d+(?:\.\d+)?)")
doc = pymupdf.open(pdf_path)
page = doc[page_no - 1]
pw = page.rect.width
ph = page.rect.height
graphics = []
for info in page.get_image_info():
    bbox = info.get("bbox")
    if not bbox:
        continue
    rect = pymupdf.Rect(bbox)
    if rect.width >= 40 and rect.height >= 40:
        graphics.append(rect)
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
    graphics.append(rect)

def overlaps_x(a, b):
    return a.x0 - 24 < b.x1 and a.x1 + 24 > b.x0

def caption_parts(text):
    # "Figure 1: title" and "图1 标题" are captions. "Figure 1 shows" is a
    # sentence, and treating it as a caption pulls in the table under it.
    match = caption_re.match(text)
    if match is None:
        return None
    if not text.lstrip().startswith("图"):
        rest = text[match.end():].lstrip()
        if rest and rest[0] not in ":：.．-—–":
            return None
    return match

def gap_of(a, b):
    gap_y = 0.0
    if a.y1 < b.y0:
        gap_y = b.y0 - a.y1
    elif b.y1 < a.y0:
        gap_y = a.y0 - b.y1
    gap_x = 0.0
    if a.x1 < b.x0:
        gap_x = b.x0 - a.x1
    elif b.x1 < a.x0:
        gap_x = a.x0 - b.x1
    return gap_y, gap_x

lines = []
for block in page.get_text("dict").get("blocks", []):
    if block.get("type") != 0:
        continue
    for line in block.get("lines", []):
        spans = line.get("spans") or []
        text = "".join(span.get("text", "") for span in spans).strip()
        if not text or "bbox" not in line:
            continue
        lines.append((pymupdf.Rect(line["bbox"]), text))
lines.sort(key=lambda item: (item[0].y0, item[0].x0))

def sentence_continues(index):
    # A bare "Figure 1" with "shows ..." on the next line is one sentence.
    # The drawing above that sentence is not a second figure.
    bb, text = lines[index]
    if text.lstrip().startswith("图"):
        return False
    match = caption_re.match(text)
    if match is None or text[match.end():].strip():
        return False
    if index + 1 >= len(lines):
        return False
    bb2, text2 = lines[index + 1]
    if bb2.y0 < bb.y1 - 2 or not overlaps_x(bb2, bb) or bb2.y0 - bb.y1 > 14:
        return False
    head = text2.lstrip()
    return bool(head) and head[0].islower()

captions = []
for index, (bb, text) in enumerate(lines):
    match = caption_parts(text)
    if match is None or sentence_continues(index):
        continue
    cap = pymupdf.Rect(bb)
    prev = bb
    taken = 1
    for bb2, text2 in lines[index + 1:]:
        if taken >= 6 or bb2.y0 - prev.y1 > 8:
            break
        if caption_parts(text2):
            break
        if not overlaps_x(bb2, bb):
            continue
        cap |= bb2
        prev = bb2
        taken += 1
    letter = match.group(1) or ""
    number = match.group(2) or ""
    ident = (letter + "." + number) if letter else number
    label = ("图 " if text.lstrip().startswith("图") else "Figure ") + ident
    captions.append((bb, cap, label, ident, number))
captions.sort(key=lambda item: (item[0].y0, item[0].x0))

def cluster(caption_bb, cap, prev_bottom, next_top, below):
    # A seed touches the caption. Other boxes of the same diagram join across
    # a gutter. A drawing that merely shares the page does not.
    def eligible(rect):
        if rect.y1 < prev_bottom - 2:
            return False
        if next_top < ph and rect.y0 > next_top + 2:
            return False
        if below:
            return cap.y1 - 4 <= rect.y0 <= cap.y1 + 140
        if rect.y0 > caption_bb.y1 + 6:
            return False
        if rect.y1 > caption_bb.y0 + 14 and rect.y0 > caption_bb.y0:
            return False
        if not overlaps_x(rect, cap) and not overlaps_x(rect, caption_bb):
            if rect.y1 < caption_bb.y0 - 4:
                return False
        return True
    pool = []
    seeds = []
    for index, rect in enumerate(graphics):
        if not eligible(rect):
            continue
        pool.append(index)
        if below or rect.y1 >= caption_bb.y0 - 48:
            seeds.append(index)
    if not seeds:
        return None
    taken = [False] * len(graphics)
    chosen = []
    graphic = None
    for index in seeds:
        taken[index] = True
        chosen.append(index)
        graphic = graphics[index] if graphic is None else graphic | graphics[index]
    grew = True
    while grew:
        grew = False
        for index in pool:
            if taken[index]:
                continue
            rect = graphics[index]
            gap_y, gap_x = gap_of(rect, graphic)
            if gap_y <= 72 and gap_x <= 72:
                taken[index] = True
                chosen.append(index)
                graphic |= rect
                grew = True
    return graphic

saved = []
for index, (bb, cap, label, ident, number) in enumerate(captions):
    if want and want != ident and want != number and want != label:
        continue
    prev_bottom = 0
    if index > 0 and captions[index - 1][0].y1 < bb.y0 - 12:
        prev_bottom = captions[index - 1][1].y1
    next_top = ph
    if index + 1 < len(captions) and captions[index + 1][0].y0 > cap.y1 + 12:
        next_top = captions[index + 1][0].y0
    graphic = cluster(bb, cap, prev_bottom, next_top, False)
    if graphic is None:
        graphic = cluster(bb, cap, prev_bottom, next_top, True)
    if graphic is None:
        continue
    clip = (graphic + (-6, -6, 6, 4)) & page.rect
    clip |= cap
    clip &= page.rect
    if clip.width < 40 or clip.height < 24:
        continue
    if clip.height > 0.78 * ph and clip.width > 0.85 * pw:
        continue
    safe = ident.replace(".", "_") or str(index + 1)
    path = os.path.join(out_dir, "fig-%s.png" % safe)
    pix = page.get_pixmap(matrix=pymupdf.Matrix(dpi / 72.0, dpi / 72.0), clip=clip, alpha=False)
    pix.save(path)
    saved.append({"label": label, "file": path})
    if len(saved) >= 4:
        break
doc.close()
sys.stdout.write(json.dumps(saved, ensure_ascii=False))
`

type figureClipFile struct {
	Label string `json:"label"`
	File  string `json:"file"`
}

// fileCompanionClipPageFigures crops the figures on one page into dir.
// figure is a number such as "1", or empty for every figure on the page.
// An empty slice and a nil error means the page has no separable figure.
func fileCompanionClipPageFigures(ctx context.Context, source, dir string, page, dpi int, figure string) ([]companionFigure, error) {
	if fileCompanionClipPageFiguresHook != nil {
		return fileCompanionClipPageFiguresHook(ctx, source, dir, page, dpi, figure)
	}
	python := paperRasterEngines().python
	if python == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	script, err := os.CreateTemp("", "companion-clip-*.py")
	if err != nil {
		return nil, err
	}
	name := script.Name()
	defer os.Remove(name)
	if _, err := script.WriteString(fileCompanionFigureClipPyScript); err != nil {
		script.Close()
		return nil, err
	}
	if err := script.Close(); err != nil {
		return nil, err
	}
	pyCtx, cancel := renderAttemptContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(pyCtx, python, name, source, dir, strconv.Itoa(page), strconv.Itoa(dpi), figure)
	hideCommandWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	output = []byte(strings.TrimSpace(string(output)))
	if len(output) == 0 || string(output) == "[]" || string(output) == "null" {
		return nil, nil
	}
	var files []figureClipFile
	if err := json.Unmarshal(output, &files); err != nil {
		return nil, err
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var figures []companionFigure
	for _, file := range files {
		label := strings.TrimSpace(file.Label)
		raw := strings.TrimSpace(file.File)
		if label == "" || raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil || !companionFigureInsideDir(dirAbs, abs) {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() < paperFigureMinBytes {
			continue
		}
		figures = append(figures, companionFigure{
			path:    abs,
			caption: fmt.Sprintf("%s（第 %d 页）", label, page),
			size:    info.Size(),
		})
	}
	return figures, nil
}

// fileCompanionPageCaptionState is 1 when the page text has a figure caption,
// 0 when it does not, and -1 when the text could not be read. An unknown page
// still tries a crop, and a miss there falls back to the whole page.
func fileCompanionPageCaptionState(canonical string, page int) int {
	data, err := fileCompanionReadPDFBytes(canonical)
	if err != nil {
		return -1
	}
	text, err := safeExtractPaperPageText(data, page-1)
	if err != nil {
		return -1
	}
	if fileCompanionTextHasFigureCaption(text) {
		return 1
	}
	return 0
}

// fileCompanionDeliverPageClips registers cropped figures for one page.
// handled is false only when this call should still render the whole page:
// no figure was named, and the crop found nothing.
func (a *App) fileCompanionDeliverPageClips(userID, canonical string, page, dpi int, figure string, allowMiss bool) (string, bool, bool) {
	clipDir, err := os.MkdirTemp("", "companion-clip-*")
	if err != nil {
		logFileCompanion("tool clip dir failed page=%d", page)
		// A known caption still must not become the whole page. Only an
		// unreadable page, or a test hook that named no figure, may fall through.
		if allowMiss && figure == "" {
			return "", false, false
		}
		if figure != "" {
			return fmt.Sprintf("第 %d 页没有裁出图 %s。没有改用整页截图。", page, figure), false, true
		}
		return fmt.Sprintf("第 %d 页裁图失败。没有改用整页截图。", page), false, true
	}
	defer os.RemoveAll(clipDir)
	clips, err := fileCompanionClipPageFigures(context.Background(), canonical, clipDir, page, dpi, figure)
	if err != nil {
		logFileCompanion("tool clip failed page=%d", page)
	}
	if len(clips) == 0 {
		// A page that names a figure must not become a full-page screenshot.
		// This function is only called without a figure number when the page
		// text mentioned one, or when a test hook is installed. A hook that
		// finds nothing still falls through to the whole page.
		// allowMiss is a test hook that found nothing, or a page whose text
		// could not be read. A named figure and a known caption stay cropped.
		if allowMiss && figure == "" {
			return "", false, false
		}
		if err != nil {
			return fmt.Sprintf("第 %d 页裁图失败。没有改用整页截图。", page), false, true
		}
		if figure != "" {
			return fmt.Sprintf("第 %d 页没有裁出图 %s。没有改用整页截图。", page, figure), false, true
		}
		if paperRasterEngines().python == "" {
			return fmt.Sprintf("第 %d 页的图没有裁开：这台设备没有带 pymupdf 的 Python。没有改用整页截图。可以 pip install pymupdf 后再试。", page), false, true
		}
		return fmt.Sprintf("第 %d 页有图注，但没有把图和正文分开。没有改用整页截图。", page), false, true
	}
	room := a.fileCompanionTurnFigureRoom(userID)
	var registered []companionFigure
	for _, clip := range clips {
		if len(registered) >= room {
			break
		}
		dest, destErr := a.fileCompanionToolFigurePath(canonical, fmt.Sprintf("clip-p%d-%s-d%d.png", page, clipFileToken(clip.caption), dpi))
		if destErr != nil {
			continue
		}
		data, readErr := os.ReadFile(clip.path)
		if readErr != nil || len(data) < paperFigureMinBytes {
			continue
		}
		if writeErr := os.WriteFile(dest, data, 0o644); writeErr != nil {
			continue
		}
		info, statErr := os.Stat(dest)
		if statErr != nil || info.Size() < paperFigureMinBytes {
			continue
		}
		registered = append(registered, companionFigure{path: dest, caption: clip.caption, size: info.Size()})
	}
	if len(registered) == 0 {
		return fmt.Sprintf("第 %d 页没有裁出可用的图。没有改用整页截图。", page), false, true
	}
	if len(registered) == len(clips) {
		a.fileCompanionStoreClipCache(canonical, page, dpi, figure, registered)
	}
	text, ok := a.fileCompanionDescribeClips(userID, page, registered, false)
	return text, ok, true
}

type clipCacheFile struct {
	Name    string `json:"name"`
	Caption string `json:"caption"`
}

func clipCacheName(page, dpi int, figure string) string {
	token := "all"
	if figure != "" {
		token = strings.ReplaceAll(figure, ".", "_")
	}
	return fmt.Sprintf("clip-p%d-q%s-d%d.json", page, token, dpi)
}

func (a *App) fileCompanionStoreClipCache(canonical string, page, dpi int, figure string, clips []companionFigure) {
	manifest, err := a.fileCompanionToolFigurePath(canonical, clipCacheName(page, dpi, figure))
	if err != nil {
		return
	}
	files := make([]clipCacheFile, 0, len(clips))
	for _, clip := range clips {
		name := filepath.Base(clip.path)
		if name == "" || name == "." || strings.ContainsAny(name, `/\`) {
			return
		}
		files = append(files, clipCacheFile{Name: name, Caption: clip.caption})
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return
	}
	_ = os.WriteFile(manifest, raw, 0o644)
}

// fileCompanionReusePageClips returns a crop this document already has on disk.
// hit is false when the page needs a new crop. A repeat call then skips Python
// and the full-file text read.
func (a *App) fileCompanionReusePageClips(userID, canonical string, page, dpi int, figure string) (string, bool, bool) {
	manifest, err := a.fileCompanionToolFigurePath(canonical, clipCacheName(page, dpi, figure))
	if err != nil {
		return "", false, false
	}
	info, err := os.Stat(manifest)
	src, srcErr := os.Stat(canonical)
	if err != nil || srcErr != nil || !info.ModTime().After(src.ModTime()) || info.Size() == 0 {
		return "", false, false
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return "", false, false
	}
	var files []clipCacheFile
	if err := json.Unmarshal(raw, &files); err != nil || len(files) == 0 {
		return "", false, false
	}
	clips := make([]companionFigure, 0, len(files))
	for _, file := range files {
		name := file.Name
		if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
			return "", false, false
		}
		dest, destErr := a.fileCompanionToolFigurePath(canonical, name)
		if destErr != nil {
			return "", false, false
		}
		st, statErr := os.Stat(dest)
		if statErr != nil || st.Size() < paperFigureMinBytes || !st.ModTime().After(src.ModTime()) {
			return "", false, false
		}
		clips = append(clips, companionFigure{path: dest, caption: file.Caption, size: st.Size()})
	}
	text, ok := a.fileCompanionDescribeClips(userID, page, clips, true)
	return text, ok, true
}

func (a *App) fileCompanionDescribeClips(userID string, page int, clips []companionFigure, reused bool) (string, bool) {
	first, err := a.appendFileCompanionTurnFigures(userID, clips)
	if err != nil {
		return err.Error(), false
	}
	var b strings.Builder
	if reused {
		fmt.Fprintf(&b, "已复用第 %d 页裁好的", page)
	} else {
		fmt.Fprintf(&b, "已从第 %d 页裁出", page)
	}
	for i, clip := range clips {
		if i > 0 {
			b.WriteString("、")
		}
		index := a.fileCompanionTurnFigureIndex(userID, clip.path)
		if index <= 0 {
			index = first + i
		}
		fmt.Fprintf(&b, "%s，编号「图%d」", clip.caption, index)
	}
	b.WriteString("。这是图本身，不含同页正文和表格。回答里写对应的「图N」，宿主会把裁好的原图放在回答下面。")
	return b.String(), true
}

func clipFileToken(caption string) string {
	caption = strings.TrimSpace(caption)
	if i := strings.Index(caption, "（"); i >= 0 {
		caption = caption[:i]
	}
	var b strings.Builder
	b.WriteByte('f')
	for _, r := range caption {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.':
			b.WriteByte('_')
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			b.WriteRune(r)
		}
	}
	if b.Len() <= 1 {
		return "fig"
	}
	return b.String()
}
