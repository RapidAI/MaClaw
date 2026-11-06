package guiapp

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

const (
	paperFigureMinSide  = 160
	paperFigureMinBytes = 500
	paperFigureMaxCount = 4
	// A near full-page image is a scan of the page, not a principle diagram.
	paperFigureMaxDisplayW = 520
	paperFigureMaxDisplayH = 740
	// An on-page box shorter than this on its long side is a template icon
	// or a logo. Elsevier paints the corresponding-author envelope as a
	// 225px JPEG at about 8pt; the pixel buffer alone looks like a figure.
	paperFigureMinDisplay = 36
)

type paperFigure struct {
	page int
	data []byte
	ext  string
	area int
	y    float64
}

// fileCompanionEmbedPaperFigures places principle diagrams directly under the
// 主要原理图 heading, before the written explanation.
// A line-start caption is rasterized whole. Embedded bitmaps come next.
// A page whose only bitmap is a template icon, and whose caption is glued
// onto a running header, is cropped the same way an ordinary question is:
// the strict raster scan never sees that caption, and widening it would
// send a sentence page to the whole-page raster.
func fileCompanionEmbedPaperFigures(source, destDir, markdown, sessionID string) string {
	if lines := fileCompanionRasterizePaperFigures(source, destDir, sessionID); len(lines) > 0 {
		return insertPaperFigureLines(markdown, lines)
	}
	figures, err := fileCompanionExtractPaperFigures(source)
	if err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	if len(figures) == 0 {
		if lines := fileCompanionEmbedClippedPaperFigures(source, destDir); len(lines) > 0 {
			return insertPaperFigureLines(markdown, lines)
		}
		return markdown
	}
	dir := filepath.Join(destDir, "figures")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	lines := make([]string, 0, len(figures))
	for i, fig := range figures {
		name := fmt.Sprintf("p%d-%d%s", fig.page, i+1, fig.ext)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, fig.data, 0o644); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("![原文第%d页](%s)", fig.page, filepath.ToSlash(path)))
	}
	if len(lines) == 0 {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	return insertPaperFigureLines(markdown, lines)
}

// Clip files stay out of the whole-page raster markers. pages.txt means
// pN-1.png screenshots; recording a crop there would make the next
// interpretation redraw the page or skip a figure that was never a screenshot.
// clips.txt is the only crop record. Identity and rows live in that one file,
// and each picture is copied to a name that belongs to this PDF snapshot.
// The crop itself still writes fig-1.png while it runs.
const paperClipListMarker = "clips.txt"

// fileCompanionEmbedClippedPaperFigures crops captioned vector drawings into
// the paper's figure directory. A later interpretation of the same PDF reuses
// those files. An empty result leaves the interpretation as prose: a missing
// crop engine, or a paper with no caption, is not an error.
func fileCompanionEmbedClippedPaperFigures(source, destDir string) []string {
	source = strings.TrimSpace(source)
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return nil
	}
	dir := filepath.Join(destDir, "figures")
	if lines := paperFreshClipLines(source, dir); len(lines) > 0 {
		return lines
	}
	info, err := os.Stat(source)
	if err != nil || info.Size() == 0 {
		return nil
	}
	data, err := os.ReadFile(source)
	if err != nil || int64(len(data)) != info.Size() {
		return nil
	}
	clips, again := fileCompanionClipSavedFigures(source, dir, data)
	if again || len(clips) == 0 {
		return nil
	}
	records := paperClipMarkdownLines(dir, clips)
	if len(records) == 0 {
		return nil
	}
	// Remember the snapshot that was cropped. A later file with a different
	// size or mtime is cropped again, even when its timestamp is older than
	// the pictures already in this directory.
	return paperRememberClipLines(source, dir, records, info.Size(), info.ModTime().UnixNano())
}

type paperClipRecord struct {
	alt  string
	name string
}

func paperClipAlt(caption string) string {
	alt := strings.TrimSpace(caption)
	if alt == "" {
		alt = "图"
	}
	alt = strings.NewReplacer("[", " ", "]", " ", "\n", " ", "\r", " ", "\t", " ").Replace(alt)
	alt = strings.Join(strings.Fields(alt), " ")
	if alt == "" {
		return "图"
	}
	return alt
}

// paperClipMarkdownLines keeps only crops that landed in dir. A path outside
// that directory is not a figure this interpretation can ship. The names are
// the crop's scratch files. paperRememberClipLines copies them aside before
// anything in the reply points at them.
func paperClipMarkdownLines(dir string, clips []companionFigure) []paperClipRecord {
	var records []paperClipRecord
	for _, clip := range clips {
		path := strings.TrimSpace(clip.path)
		if path == "" || !companionFigureInsideDir(dir, path) {
			continue
		}
		name := filepath.Base(path)
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			continue
		}
		records = append(records, paperClipRecord{alt: paperClipAlt(clip.caption), name: name})
	}
	return records
}

// paperRememberClipLines publishes one snapshot. The crop writes fig-1.png
// before this runs, so those bytes are copied to a name that includes the PDF
// size and mtime first. The list is then one file: the identity and the rows
// are renamed into place together. A crash keeps the previous complete list,
// or no list. It cannot keep the old PDF's identity next to the new rows, and
// the next crop's fig-1.png cannot replace the previous paper's picture.
func paperRememberClipLines(source, dir string, records []paperClipRecord, size, mod int64) []string {
	if len(records) == 0 || size <= 0 || strings.TrimSpace(source) == "" {
		return nil
	}
	prefix := fmt.Sprintf("%d-%d-", size, mod)
	published := make([]paperClipRecord, 0, len(records))
	for _, record := range records {
		name := record.name
		if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || record.alt == "" {
			return nil
		}
		destName := prefix + name
		if destName == name || destName != filepath.Base(destName) || strings.ContainsAny(destName, `/\`) {
			return nil
		}
		if err := paperCopyClipFile(filepath.Join(dir, name), filepath.Join(dir, destName)); err != nil {
			return nil
		}
		published = append(published, paperClipRecord{alt: record.alt, name: destName})
	}
	var body bytes.Buffer
	fmt.Fprintf(&body, "%d\t%d\t%d\n%s\n", size, mod, len(source), source)
	for _, record := range published {
		body.WriteString(record.alt)
		body.WriteByte('\t')
		body.WriteString(record.name)
		body.WriteByte('\n')
	}
	lines := make([]string, 0, len(published))
	for _, record := range published {
		line, ok := paperClipSnapshotLine(dir, record.alt+"\t"+record.name)
		if !ok {
			return nil
		}
		lines = append(lines, line)
	}
	if err := paperReplaceFile(filepath.Join(dir, paperClipListMarker), body.Bytes()); err != nil {
		// The pictures already have this snapshot's names. This interpretation
		// can use them. The next one crops again, because the list still names
		// the previous snapshot.
		return lines
	}
	// The previous cache was two files. Nothing reads clip-source.txt now.
	// Leaving it would still name a PDF the list no longer describes.
	_ = os.Remove(filepath.Join(dir, "clip-source.txt"))
	return lines
}

// paperFreshClipLines returns the crops saved for this exact PDF snapshot.
// Size and mtime must both match. A replaced file whose timestamp is older
// than the pictures on disk is still cropped again. A list left by the
// two-file cache does not parse, so it is cropped again instead of trusting
// rows that may belong to a different write.
func paperFreshClipLines(source, dir string) []string {
	info, err := os.Stat(source)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, paperClipListMarker))
	if err != nil {
		return nil
	}
	size, mod, path, rows, ok := paperParseClipSnapshot(data)
	if !ok || path != source || size != info.Size() || mod != info.ModTime().UnixNano() {
		return nil
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		line, ok := paperClipSnapshotLine(dir, row)
		if !ok {
			return nil
		}
		lines = append(lines, line)
	}
	return lines
}

func paperClipSnapshotLine(dir, row string) (string, bool) {
	alt, name, found := strings.Cut(row, "\t")
	if !found {
		return "", false
	}
	alt = strings.TrimSpace(alt)
	name = strings.TrimSpace(name)
	if alt == "" || name == "" || strings.ContainsAny(alt, "[]\r\n\t") || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", false
	}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if err != nil || info.Size() <= int64(paperFigureMinBytes) || !companionFigureInsideDir(dir, path) {
		return "", false
	}
	return fmt.Sprintf("![%s](%s)", alt, filepath.ToSlash(path)), true
}

// paperParseClipSnapshot reads one complete cache file:
//
//	size \t mtime \t pathByteLen \n
//	<path bytes> \n
//	alt \t picture \n
func paperParseClipSnapshot(data []byte) (size, mod int64, path string, rows []string, ok bool) {
	header, rest, found := bytes.Cut(data, []byte{'\n'})
	if !found {
		return
	}
	fields := strings.Split(string(header), "\t")
	if len(fields) != 3 {
		return
	}
	var err error
	size, err = strconv.ParseInt(fields[0], 10, 64)
	if err != nil || size <= 0 {
		return
	}
	mod, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return
	}
	pathLen, err := strconv.Atoi(fields[2])
	if err != nil || pathLen <= 0 || pathLen > len(rest) {
		return
	}
	path = string(rest[:pathLen])
	rest = rest[pathLen:]
	if len(rest) == 0 || rest[0] != '\n' {
		return
	}
	rest = rest[1:]
	for _, row := range strings.Split(string(rest), "\n") {
		row = strings.TrimRight(row, "\r")
		if strings.TrimSpace(row) == "" {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return 0, 0, "", nil, false
	}
	return size, mod, path, rows, true
}

// paperCopyClipFile copies one scratch crop onto its snapshot name. The
// scratch name stays, because the crop writes that name again next time.
func paperCopyClipFile(src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return fmt.Errorf("clip snapshot overwrites its scratch file")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) <= paperFigureMinBytes {
		return fmt.Errorf("clip snapshot is too small")
	}
	return paperReplaceFile(dst, data)
}

// paperReplaceFile publishes a complete temp file by rename. The previous
// file stays in place until the new bytes are complete. Removing it first
// would leave a window with neither snapshot.
func paperReplaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".clip-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameReplacing(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func fileCompanionExtractPaperFigures(source string) ([]paperFigure, error) {
	source = strings.TrimSpace(source)
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return nil, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	return paperFiguresFromPDFBytes(data)
}

func paperFiguresFromPDFBytes(data []byte) ([]paperFigure, error) {
	if len(data) == 0 {
		return nil, nil
	}
	pages, err := safeExtractPaperPDFImages(data)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var found []paperFigure
	pageIndexes := make([]int, 0, len(pages))
	for pageIndex := range pages {
		pageIndexes = append(pageIndexes, pageIndex)
	}
	sort.Ints(pageIndexes)
	for _, pageIndex := range pageIndexes {
		for _, extracted := range pages[pageIndex] {
			if extracted.ObjNum != 0 && seen[extracted.ObjNum] {
				continue
			}
			fig, ok := paperFigureFromExtracted(pageIndex+1, extracted)
			if !ok {
				continue
			}
			if extracted.ObjNum != 0 {
				seen[extracted.ObjNum] = true
			}
			found = append(found, fig)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.SliceStable(found, func(i, j int) bool {
		return found[i].area > found[j].area
	})
	if len(found) > paperFigureMaxCount {
		found = found[:paperFigureMaxCount]
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].page != found[j].page {
			return found[i].page < found[j].page
		}
		return found[i].y > found[j].y
	})
	return found, nil
}

// paperFigurePaintedAsIcon reports a bitmap placed smaller than a figure.
// A missing placement (either side near zero) is not an icon: the pixel
// check still decides, so a chart whose matrix was not recovered stays.
func paperFigurePaintedAsIcon(extracted gopdf2.ExtractedImage) bool {
	w := math.Abs(extracted.DisplayWidth)
	h := math.Abs(extracted.DisplayHeight)
	if w < 1 || h < 1 {
		return false
	}
	long := w
	if h > long {
		long = h
	}
	return long < paperFigureMinDisplay
}

func paperFigureFromExtracted(page int, extracted gopdf2.ExtractedImage) (paperFigure, bool) {
	if len(extracted.Data) < paperFigureMinBytes {
		return paperFigure{}, false
	}
	if paperFigurePaintedAsIcon(extracted) {
		return paperFigure{}, false
	}
	// A mirrored image stores a negative side. Magnitude is the on-page size.
	if math.Abs(extracted.DisplayWidth) > paperFigureMaxDisplayW && math.Abs(extracted.DisplayHeight) > paperFigureMaxDisplayH {
		return paperFigure{}, false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(extracted.Data))
	if err != nil {
		return paperFigure{}, false
	}
	ext := ""
	switch format {
	case "jpeg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	default:
		return paperFigure{}, false
	}
	longSide, shortSide := cfg.Width, cfg.Height
	if shortSide > longSide {
		longSide, shortSide = shortSide, longSide
	}
	// Wide principle diagrams stay. Thin rules and icons do not.
	if longSide < paperFigureMinSide || shortSide < 100 {
		return paperFigure{}, false
	}
	return paperFigure{
		page: page,
		data: extracted.Data,
		ext:  ext,
		area: cfg.Width * cfg.Height,
		y:    extracted.Y,
	}, true
}

func safeExtractPaperPDFImages(pdfData []byte) (pages map[int][]gopdf2.ExtractedImage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			err = fmt.Errorf("extract pdf images panicked")
		}
	}()
	return gopdf2.ExtractImagesFromAllPages(pdfData)
}

func insertPaperFigureLines(markdown string, lines []string) string {
	// A blank line after the image keeps it a separate block. Glued to the
	// following sentence, the page filler can slice the image path.
	block := "\n" + strings.Join(lines, "\n\n") + "\n\n"
	if at, ok := paperFigureHeadingEnd(markdown); ok {
		return markdown[:at] + block + markdown[at:]
	}
	return strings.TrimRight(markdown, "\n") + "\n\n## 主要原理图及说明\n" + strings.Trim(block, "\n") + "\n"
}

// paperFigureHeadingEnd is the index just after the heading line. A numbered
// heading such as "## 3. 主要原理图及说明" counts; the dataset parser already
// accepts that shape, and a strict prefix would append a second section.
func paperFigureHeadingEnd(markdown string) (int, bool) {
	offset := 0
	for _, line := range strings.Split(markdown, "\n") {
		end := offset + len(line)
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			title := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(trim, "## "), "0123456789.、．)）（( \t"))
			if strings.Contains(title, "原理图") {
				if end < len(markdown) && markdown[end] == '\n' {
					end++
				}
				return end, true
			}
		}
		offset = end + 1
	}
	return 0, false
}
