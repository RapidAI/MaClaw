package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"golang.org/x/image/draw"
)

// Images inside the open document. A companion answer can point at "图1" and the
// window shows that drawing next to the prose, so a summary does not have to
// describe a chart the reader can simply look at. Nothing here lets the model
// write; the figures only travel outwards through the reply.
const (
	// fileCompanionFigureMaxScan is how much of a markdown or HTML source is
	// scanned for references. It is far above what gets injected into a turn.
	fileCompanionFigureMaxScan = 8 << 20
	// fileCompanionFigureMaxAttach is the vision budget of one turn. Four
	// drawings still let the model answer about diagrams without turning a
	// screenshot-heavy page into an unpayable request.
	fileCompanionFigureMaxAttach = 4
	// fileCompanionFigureMaxFallback is how many figures are shown when the
	// reply names none but the question asked about the whole document.
	fileCompanionFigureMaxFallback = 3
	// fileCompanionFigureMaxIndexed keeps one inventory bounded. A long manual
	// copies the first figures; later ones are still reachable by reference in
	// the document itself.
	fileCompanionFigureMaxIndexed = 12
	fileCompanionFigureMinBytes   = 512
	fileCompanionFigureMaxBytes   = 24 << 20
	// fileCompanionFigureTurnBytes caps the raw bytes one turn attaches.
	fileCompanionFigureTurnBytes = 16 << 20
	// Below these sizes the original bytes go through unchanged, so the window
	// shows exactly what the document holds. Above them the image is decoded
	// and downscaled instead of pushing a full-print scan through the bridge.
	fileCompanionFigureInlineLimit = 3 << 20
	fileCompanionFigureVisionLimit = 1 << 20
	fileCompanionFigureDisplayEdge = 1600
	fileCompanionFigureVisionEdge  = 1568
	// A PDF is parsed on the first ordinary question, so an archive-sized scan
	// must not turn that turn into a minutes-long read.
	fileCompanionPdfFigureMaxBytes = 64 << 20
	// Decoding caps the pixels, not just the bytes: a small PNG can still be a
	// 40000×40000 decompression bomb.
	fileCompanionFigureMaxPixels = 32 << 20
)

// FileCompanionFigure is one original drawing offered beside a reply. Index and
// Label are the handle the answer uses; Path is how the window asks for bytes.
type FileCompanionFigure struct {
	Index   int    `json:"index"`
	Label   string `json:"label"`
	Caption string `json:"caption"`
	Path    string `json:"path"`
}

// companionFigure is one inventory entry. The inventory is the open document's
// own images in reading order, not a per-turn selection.
type companionFigure struct {
	path    string
	caption string
	size    int64
}

type companionFigureInventory struct {
	size    int64
	mod     int64
	figures []companionFigure
}

// The angle-bracket form is how markdown spells a target with spaces in it.
var fileCompanionMarkdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(\s*(?:<([^<>]*)>|([^()\s]+))`)

// HTML is matched tag-first so alt can be read no matter which attribute comes
// first; a strict src-then-alt order would drop half the real documents.
var fileCompanionHTMLImgTagRe = regexp.MustCompile(`(?i)<img\b[^>]*>`)
var (
	fileCompanionHTMLSrcRe = regexp.MustCompile(`(?i)\bsrc\s*=\s*["']([^"']+)["']`)
	fileCompanionHTMLAltRe = regexp.MustCompile(`(?i)\balt\s*=\s*["']([^"']*)["']`)
)
var fileCompanionFigureRefRe = regexp.MustCompile(`(?i)(?:图\s*|figure\s*|fig\.\s*|fig\s*)(\d+)`)

// Host labels are 「图N」 only. "Figure 3" in a paper is the document's own
// caption, not the number this turn assigned to a render.
var fileCompanionHostFigureRefRe = regexp.MustCompile(`图\s*(\d+)`)
var fileCompanionSummaryRe = regexp.MustCompile(`(?i)(总结|概括|摘要|概述|主要内容|主要讲|讲的什么|说什么|讲什么|summar|overview|abstract|outline)`)

// fileCompanionFigures is the open document's image inventory. The inventory is
// recomputed only when the file itself changed, so asking several questions
// about one document does not re-extract it every time.
func (a *App) fileCompanionFigures(canonical string) []companionFigure {
	return a.fileCompanionFiguresBounded(canonical, false)
}

// fileCompanionFiguresBounded is the inventory with a cost dial. cheapOnly keeps
// the cache and, when cold, extracts only the text formats whose scan is a
// millisecond. It exists for paths that run without a user asking: opening a
// tab and serving a thumbnail must not start parsing a Word file or a PDF; the
// first ordinary question pays for that instead.
func (a *App) fileCompanionFiguresBounded(canonical string, cheapOnly bool) []companionFigure {
	if a == nil {
		return nil
	}
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		return nil
	}
	st := a.companionRuntime()
	st.mu.Lock()
	cached := st.figures[canonical]
	st.mu.Unlock()
	if cached != nil && cached.size == info.Size() && cached.mod == info.ModTime().UnixNano() {
		return cached.figures
	}
	if cheapOnly {
		switch strings.ToLower(filepath.Ext(canonical)) {
		case ".md", ".markdown", ".html", ".htm":
		default:
			return nil
		}
	}
	figures := a.extractFileCompanionFigures(canonical)
	st.mu.Lock()
	if st.figures == nil {
		st.figures = map[string]*companionFigureInventory{}
	}
	st.figures[canonical] = &companionFigureInventory{size: info.Size(), mod: info.ModTime().UnixNano(), figures: figures}
	st.mu.Unlock()
	return figures
}

func (a *App) extractFileCompanionFigures(canonical string) []companionFigure {
	switch strings.ToLower(filepath.Ext(canonical)) {
	case ".md", ".markdown":
		return a.fileCompanionTextFigures(canonical, true)
	case ".html", ".htm":
		return a.fileCompanionTextFigures(canonical, false)
	case ".docx":
		return a.fileCompanionDocxFigures(canonical)
	case ".pdf":
		return a.fileCompanionPdfFigures(canonical)
	default:
		return nil
	}
}

// fileCompanionTextFigures reads image references out of markdown or HTML. These
// documents point at files that already sit next to them, so nothing is copied.
func (a *App) fileCompanionTextFigures(canonical string, markdown bool) []companionFigure {
	file, err := os.Open(canonical)
	if err != nil {
		return nil
	}
	defer file.Close()
	if _, err := file.Stat(); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, fileCompanionFigureMaxScan))
	if err != nil || len(raw) == 0 {
		return nil
	}
	dir := filepath.Dir(canonical)
	var out []companionFigure
	seen := map[string]bool{}
	consider := func(target, caption string) {
		abs, ok := companionFigureResolve(dir, target)
		if !ok {
			return
		}
		key := fileCompanionFigureKey(abs)
		if seen[key] {
			return
		}
		fig, ok := companionFigureCandidate(abs)
		if !ok {
			return
		}
		// The grant table covers the document itself. A drawing it references
		// may only come from beside it: a document is untrusted input, and its
		// image tags would otherwise aim the vision request at any local file.
		if !companionFigureInsideDir(dir, abs) {
			return
		}
		seen[key] = true
		if caption == "" {
			caption = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
		}
		fig.caption = caption
		out = append(out, fig)
	}
	if markdown {
		for _, match := range fileCompanionMarkdownImageRe.FindAllStringSubmatch(string(raw), -1) {
			// Group 2 is the angle-bracket spelling, group 3 the plain one.
			target := match[2]
			if target == "" {
				target = match[3]
			}
			consider(target, strings.TrimSpace(match[1]))
			if len(out) >= fileCompanionFigureMaxIndexed {
				break
			}
		}
		return out
	}
	for _, tag := range fileCompanionHTMLImgTagRe.FindAllString(string(raw), -1) {
		src := fileCompanionHTMLSrcRe.FindStringSubmatch(tag)
		if len(src) < 2 {
			continue
		}
		caption := ""
		if alt := fileCompanionHTMLAltRe.FindStringSubmatch(tag); len(alt) >= 2 {
			caption = strings.TrimSpace(alt[1])
		}
		consider(src[1], caption)
		if len(out) >= fileCompanionFigureMaxIndexed {
			break
		}
	}
	return out
}

// fileCompanionDocxFigures pulls the embedded media out of a Word file into the
// companion's own cache directory, because those images have no path of their
// own to render from.
func (a *App) fileCompanionDocxFigures(canonical string) []companionFigure {
	nodes, images, err := knowledge.ExtractDOCXImages(knowledge.Source{}, canonical, nil)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	var out []companionFigure
	for i, node := range nodes {
		if node.Metadata[knowledge.MetaImageIsVector] == "true" {
			continue
		}
		data := images[node.Metadata["_image_bytes_key"]]
		if len(data) == 0 {
			continue
		}
		path, ok := a.writeFileCompanionFigure(canonical, i+1, "docx", node.Metadata[knowledge.MetaImageFormat], data)
		if !ok {
			continue
		}
		caption := strings.TrimSpace(node.Metadata[knowledge.MetaImageAltText])
		if caption == "" {
			caption = strings.TrimSpace(node.Title)
		}
		out = append(out, companionFigure{path: path, caption: caption, size: int64(len(data))})
		if len(out) >= fileCompanionFigureMaxIndexed {
			break
		}
	}
	return out
}

// fileCompanionPdfFigures uses the same XObject extraction the paper
// interpretation already relies on. Page renders need an external rasteriser,
// which an ordinary companion turn must not depend on.
func (a *App) fileCompanionPdfFigures(canonical string) []companionFigure {
	if info, err := os.Stat(canonical); err != nil || info.Size() > fileCompanionPdfFigureMaxBytes {
		return nil
	}
	found, err := fileCompanionExtractPaperFigures(canonical)
	if err != nil || len(found) == 0 {
		return nil
	}
	var out []companionFigure
	for i, fig := range found {
		path, ok := a.writeFileCompanionFigure(canonical, i+1, "pdf", strings.TrimPrefix(fig.ext, "."), fig.data)
		if !ok {
			continue
		}
		out = append(out, companionFigure{
			path:    path,
			caption: fmt.Sprintf("第 %d 页", fig.page),
			size:    int64(len(fig.data)),
		})
		if len(out) >= fileCompanionFigureMaxIndexed {
			break
		}
	}
	return out
}

// writeFileCompanionFigure drops one extracted image into the session's figure
// directory. Every session owns a directory of its own, so two tabs cannot
// overwrite each other's cached drawings.
func (a *App) writeFileCompanionFigure(canonical string, order int, kind, format string, data []byte) (string, bool) {
	if a == nil || len(data) == 0 {
		return "", false
	}
	ext := ""
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpg", "jpeg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	case "gif":
		ext = ".gif"
	case "webp":
		ext = ".webp"
	}
	if ext == "" {
		// The metadata lied or said nothing. The bytes themselves decide; an
		// image no decoder recognizes is dropped rather than served with a
		// made-up extension.
		switch _, real, err := image.DecodeConfig(bytes.NewReader(data)); {
		case err != nil:
			return "", false
		case real == "jpeg":
			ext = ".jpg"
		case real == "png":
			ext = ".png"
		case real == "gif":
			ext = ".gif"
		case real == "webp":
			ext = ".webp"
		default:
			return "", false
		}
	}
	dir := a.fileCompanionFigureDir(canonical)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logFileCompanion("figure cache failed")
		return "", false
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%02d%s", kind, order, ext))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logFileCompanion("figure cache failed")
		return "", false
	}
	return path, true
}

// fileCompanionFigureDir is where one document's extracted drawings live. The
// name comes from the document path alone, so a figure can be traced back to
// its document without any in-memory bookkeeping.
func (a *App) fileCompanionFigureDir(canonical string) string {
	return filepath.Join(a.GetDataDir(), "file-companion", "figures", fileCompanionSessionID(canonical))
}

// companionFigureResolve turns one reference into a real path. Remote images and
// inline data leave no local file to show, so they are not inventory entries.
func companionFigureResolve(dir, target string) (string, bool) {
	target = strings.TrimSpace(strings.Trim(target, "<>\"'"))
	if target == "" {
		return "", false
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "//") {
		return "", false
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		// A file URL keeps the root slash in front of a Windows drive letter,
		// so drop it before the path is treated as a volume name.
		target = strings.TrimPrefix(target[len("file://"):], "/")
	}
	if cut := strings.IndexAny(target, "?#"); cut >= 0 {
		target = target[:cut]
	}
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsRune(target, 0) {
		return "", false
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path), true
}

func companionFigureCandidate(path string) (companionFigure, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		return companionFigure{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return companionFigure{}, false
	}
	if info.Size() < fileCompanionFigureMinBytes || info.Size() > fileCompanionFigureMaxBytes {
		return companionFigure{}, false
	}
	// The extension is a claim from the document, not a fact. Only images the
	// app can really decode enter the inventory, so a renamed text file never
	// rides along as a vision attachment or a broken thumbnail.
	file, err := os.Open(path)
	if err != nil {
		return companionFigure{}, false
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return companionFigure{}, false
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(head)); err != nil {
		// The header can hide behind a long EXIF chain; decide on the whole file.
		full, err := os.ReadFile(path)
		if err != nil {
			return companionFigure{}, false
		}
		if _, _, err := image.DecodeConfig(bytes.NewReader(full)); err != nil {
			return companionFigure{}, false
		}
	}
	return companionFigure{path: path, size: info.Size()}, true
}

// companionFigureInsideDir reports whether a referenced drawing lives inside the
// document's own directory tree. Case and symlinks are folded through the same
// canonicalization the grant table uses, so a link cannot smuggle a path out.
func companionFigureInsideDir(dir, path string) bool {
	root := fileCompanionFigureKey(dir)
	full := fileCompanionFigureKey(path)
	if root == "" || full == "" || root == full {
		return false
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(full, root)
}

func fileCompanionFigureKey(path string) string {
	if canonical, err := fileCompanionCanonicalPath(path); err == nil {
		return canonical
	}
	return strings.ToLower(filepath.Clean(path))
}

// fileCompanionTurnUsesFigures reports whether a turn should carry the open
// document's drawings. A selection rewrite is written back into the file as
// plain text, and the paper interpretation places figures itself, so neither
// turn is given a figure list to talk about.
func fileCompanionTurnUsesFigures(selection string, paper, isImage bool) bool {
	return !isImage && strings.TrimSpace(selection) == "" && !paper
}

// fileCompanionAttachFigures puts the document's own drawings behind one turn
// and returns the figures the model can now refer to by number. The existing
// left pane already shows the picture when the open file is the image.
func (a *App) fileCompanionAttachFigures(msg IMUserMessage, figures []companionFigure) (IMUserMessage, []companionFigure) {
	picked := make([]companionFigure, 0, fileCompanionFigureMaxAttach)
	for _, fig := range figures {
		if len(picked) >= fileCompanionFigureMaxAttach {
			break
		}
		if fig.size <= 0 || fig.size > fileCompanionFigureMaxBytes {
			continue
		}
		picked = append(picked, fig)
	}
	if len(picked) == 0 {
		return msg, nil
	}
	vision := a != nil && a.imHandler != nil && a.imHandler.getMaclawLLMConfig().SupportsVision
	sent := picked
	if vision {
		sent = nil
		var bytesSent int64
		for _, fig := range picked {
			data, mime, err := companionFigureImage(fig.path, fileCompanionFigureVisionEdge, fileCompanionFigureVisionLimit)
			if err != nil {
				continue
			}
			// A drawing that no longer fits is skipped, not the rest: a small
			// later figure still deserves the remaining budget.
			if int64(len(data)) > fileCompanionFigureTurnBytes-bytesSent {
				continue
			}
			bytesSent += int64(len(data))
			msg.Attachments = append(msg.Attachments, agent.MessageAttachment{
				Type:     "image",
				FileName: filepath.Base(fig.path),
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(data),
				Size:     int64(len(data)),
			})
			sent = append(sent, fig)
		}
	}
	if len(sent) == 0 {
		return msg, nil
	}
	msg.Text = strings.TrimRight(msg.Text, "\n") + "\n\n" + fileCompanionFigureNote(sent, vision)
	return msg, sent
}

// fileCompanionFigureNote names every figure that went out, in the same order as
// the image blocks. The model then answers with "图2" instead of restating a
// caption that may not be unique.
func fileCompanionFigureNote(figures []companionFigure, vision bool) string {
	var b strings.Builder
	if vision {
		b.WriteString("原文中的图片已按出现顺序附在下面：\n")
	} else {
		b.WriteString("原文中的图片按出现顺序编号如下（当前模型不能看图，只能依据图题引用，不要描述画面内容）：\n")
	}
	for i, fig := range figures {
		caption := strings.TrimSpace(fig.caption)
		if caption == "" {
			caption = filepath.Base(fig.path)
		}
		b.WriteString(fmt.Sprintf("图%d %s", i+1, caption))
		b.WriteString("\n")
	}
	b.WriteString("回答里提到某张图时，直接写「图N」，宿主会把这张原图放在回答下面一起展示给用户。")
	return b.String()
}

// fileCompanionFiguresForReply picks the drawings an answer is talking about.
// Without a reference the answer stays text only, except for a whole-document
// question, where the first few figures are exactly what the reader wants.
func fileCompanionFiguresForReply(figures []companionFigure, asked, reply string) []FileCompanionFigure {
	if len(figures) == 0 {
		return nil
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return nil
	}
	matched := make([]bool, len(figures))
	found := 0
	for i, fig := range figures {
		if fileCompanionReplyMentionsFigure(reply, i+1, fig) {
			matched[i] = true
			found++
		}
	}
	// Numbering restarts at 图1 each turn. The model often continues from the
	// previous answer, so the only render is 图1 and the reply says 图2.
	// Show that render under the number the reply used. A farther number is
	// the paper's own figure, not a guess at this picture.
	cited := 0
	if found == 0 && len(figures) == 1 && fileCompanionReplyCitesNextHostFigure(reply, 1) {
		matched[0] = true
		found = 1
		cited = 2
	}
	if found == 0 && fileCompanionSummaryRe.MatchString(asked) {
		for i := 0; i < len(figures) && i < fileCompanionFigureMaxFallback; i++ {
			matched[i] = true
			found++
		}
	}
	if found == 0 {
		return nil
	}
	out := make([]FileCompanionFigure, 0, found)
	for i, fig := range figures {
		if !matched[i] {
			continue
		}
		number := i + 1
		if cited > 0 {
			number = cited
		}
		out = append(out, FileCompanionFigure{
			Index:   number,
			Label:   fmt.Sprintf("图%d", number),
			Caption: strings.TrimSpace(fig.caption),
			Path:    fig.path,
		})
	}
	return out
}

// fileCompanionReplyCitesNextHostFigure is true when the reply's 「图N」 labels
// include count+1 and none of them name a picture this turn actually has.
// An in-range 图N stays with the normal match.
func fileCompanionReplyCitesNextHostFigure(reply string, count int) bool {
	if count < 1 {
		return false
	}
	matches := fileCompanionHostFigureRefRe.FindAllStringSubmatch(reply, -1)
	if len(matches) == 0 {
		return false
	}
	next := count + 1
	sawNext := false
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil || value <= 0 {
			continue
		}
		if value <= count {
			return false
		}
		if value == next {
			sawNext = true
		}
	}
	return sawNext
}

func fileCompanionReplyMentionsFigure(reply string, index int, fig companionFigure) bool {
	for _, match := range fileCompanionFigureRefRe.FindAllStringSubmatch(reply, -1) {
		if len(match) < 2 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err == nil && value == index {
			return true
		}
	}
	name := strings.TrimSpace(filepath.Base(fig.path))
	if name != "" && strings.Contains(reply, name) {
		return true
	}
	caption := strings.TrimSpace(fig.caption)
	if len([]rune(caption)) >= 4 && strings.Contains(reply, caption) {
		return true
	}
	return false
}

// companionFigureImage returns bounded bytes for one figure. Small images keep
// their original bytes; anything larger is decoded and scaled down so neither
// the bridge nor a vision endpoint has to carry a full-print scan.
func companionFigureImage(path string, maxEdge int, inlineLimit int64) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if info.Size() <= 0 || info.Size() > fileCompanionFigureMaxBytes {
		return nil, "", errors.New("figure image is out of range")
	}
	raw, err := io.ReadAll(io.LimitReader(file, fileCompanionFigureMaxBytes))
	if err != nil {
		return nil, "", err
	}
	mime := fileCompanionImageMime(path)
	if int64(len(raw)) <= inlineLimit {
		return raw, mime, nil
	}
	// Refuse the decompression bomb before the pixels exist.
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && cfg.Width*cfg.Height > fileCompanionFigureMaxPixels {
		return nil, "", errors.New("figure image is too large to decode")
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scaled := false
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = max(1, height*maxEdge/width)
			width = maxEdge
		} else {
			width = max(1, width*maxEdge/height)
			height = maxEdge
		}
		scaledImage := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(scaledImage, scaledImage.Bounds(), source, bounds, draw.Over, nil)
		bounds = scaledImage.Bounds()
		source = scaledImage
		scaled = true
	}
	var out bytes.Buffer
	if mime == "image/jpeg" {
		if err := jpeg.Encode(&out, source, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", err
		}
		// Re-encoding is only worth it when it shrinks; otherwise the original
		// keeps its quality.
		if !scaled && int64(out.Len()) >= int64(len(raw)) {
			return raw, mime, nil
		}
		return out.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&out, source); err != nil {
		return nil, "", err
	}
	if !scaled && int64(out.Len()) >= int64(len(raw)) {
		return raw, mime, nil
	}
	return out.Bytes(), "image/png", nil
}

// FileCompanionFigureDataURL hands the window bytes for one figure it was
// already shown. Only images listed for an open document are served, so this
// method cannot be used to read arbitrary paths off the disk.
func (a *App) FileCompanionFigureDataURL(path string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return "", errors.New("file companion is not running")
	}
	if strings.ContainsRune(path, 0) {
		return "", errFileCompanionNULPath
	}
	canonical, err := fileCompanionCanonicalPath(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if !a.fileCompanionFigureListed(canonical) {
		return "", errFileCompanionFigureUnlisted
	}
	data, mime, err := companionFigureImage(canonical, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

var errFileCompanionFigureUnlisted = errors.New("that image is not part of the open file")

// fileCompanionFigureListed reports whether the path belongs to the figure
// inventory of a currently open document.
func (a *App) fileCompanionFigureListed(canonical string) bool {
	st := a.companionRuntime()
	st.mu.Lock()
	open := make([]string, 0, len(st.grants))
	for granted := range st.grants {
		open = append(open, granted)
	}
	st.mu.Unlock()
	for _, granted := range open {
		for _, fig := range a.fileCompanionFiguresBounded(granted, true) {
			if fileCompanionFigureKey(fig.path) == canonical {
				return true
			}
		}
	}
	// A drawing this document produced lives in its own figure directory, whose
	// name is derived from the document. Recognising that directory is what lets
	// a reopened window serve figures extracted before the app restarted: the
	// inventory is in memory, but the files on disk are not.
	//
	// Without this, a PDF's figures disappear from anything built after a
	// restart — an export would silently lose every picture while the window
	// still displayed them.
	for _, granted := range open {
		if companionFigureInsideDir(a.fileCompanionFigureDir(granted), canonical) {
			return true
		}
	}
	// Tool-produced figures (page renders, extracted images) live outside any
	// document inventory, so the turn registry vouches for them instead.
	return a.fileCompanionTurnFigureListed(canonical)
}
