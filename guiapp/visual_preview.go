package guiapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// visualPreviewState is one agent turn's rendered-page check.
// Tool calls run one at a time, and the slot is cleared when that call returns.
type visualPreviewState struct {
	mu          sync.Mutex
	source      string
	path        string
	projectPath string
	diskProject string
	sessionID   string
	lastHTML    string
	slideOffset int
	browser     *browser.PrivateHeadless
}

const visualPreviewSlideWindow = 3

func (s *visualPreviewState) note(abs, projectPath, diskProject, sessionID string) {
	s.notePage(abs, projectPath, diskProject, sessionID, 0)
}

func (s *visualPreviewState) notePage(abs, projectPath, diskProject, sessionID string, slideOffset int) {
	if s == nil {
		return
	}
	target, kind := classifyVisualPreviewTarget(abs, s.lastHTMLLocked())
	if target == "" || kind == "" {
		return
	}
	if slideOffset < 0 || kind != "pptx" {
		slideOffset = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source = abs
	s.path = target
	s.projectPath = strings.TrimSpace(projectPath)
	s.diskProject = strings.TrimSpace(diskProject)
	s.sessionID = strings.TrimSpace(sessionID)
	s.slideOffset = slideOffset
	if kind == "html" {
		s.lastHTML = target
	}
}

func (s *visualPreviewState) lastHTMLLocked() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHTML
}

func (s *visualPreviewState) hasPath() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.path) != ""
}

func (s *visualPreviewState) take() (source, path, projectPath, diskProject, sessionID string, slideOffset int, headless *browser.PrivateHeadless) {
	if s == nil {
		return "", "", "", "", "", 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, path = s.source, s.path
	projectPath, diskProject, sessionID = s.projectPath, s.diskProject, s.sessionID
	slideOffset = s.slideOffset
	headless = s.browser
	s.source = ""
	s.path = ""
	s.projectPath = ""
	s.diskProject = ""
	s.sessionID = ""
	s.slideOffset = 0
	return source, path, projectPath, diskProject, sessionID, slideOffset, headless
}

func (s *visualPreviewState) keepBrowser(headless *browser.PrivateHeadless) {
	if s == nil || headless == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browser == nil {
		s.browser = headless
	}
}

func (s *visualPreviewState) dropBrowser(headless *browser.PrivateHeadless) {
	if s == nil || headless == nil {
		return
	}
	s.mu.Lock()
	if s.browser == headless {
		s.browser = nil
	}
	s.mu.Unlock()
	headless.Close()
}

func (s *visualPreviewState) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	headless := s.browser
	s.browser = nil
	s.mu.Unlock()
	if headless != nil {
		headless.Close()
	}
}

func classifyVisualPreviewTarget(abs, lastHTML string) (target, kind string) {
	abs = strings.TrimSpace(abs)
	if abs == "" {
		return "", ""
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm", ".xhtml", ".svg":
		return abs, "html"
	case ".css", ".js", ".mjs":
		index := filepath.Join(filepath.Dir(abs), "index.html")
		if info, err := os.Stat(index); err == nil && !info.IsDir() {
			return index, "html"
		}
		lastHTML = strings.TrimSpace(lastHTML)
		if lastHTML != "" && sameDir(filepath.Dir(lastHTML), filepath.Dir(abs)) {
			if info, err := os.Stat(lastHTML); err == nil && !info.IsDir() {
				return lastHTML, "html"
			}
		}
		return "", ""
	case ".pptx":
		return abs, "pptx"
	default:
		return "", ""
	}
}

func sameDir(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

func (s *visualPreviewState) finish(app *App, supportsVision bool, result agent.ToolExecutionResult) agent.ToolExecutionResult {
	source, path, projectPath, diskProject, sessionID, slideOffset, headless := s.take()
	if path == "" {
		return result
	}
	if visualPreviewNeedsPane(source, path) {
		emitVisualPreviewPane(app, source, path, projectPath, diskProject, sessionID)
	}
	if !supportsVision {
		result.Result = visualPreviewBody(result.Result, "预览已在右侧打开。当前模型看不到画面，不要声称已经看过版式。")
		return result
	}
	note, images, next := renderVisualPreview(app, headless, path, result.Result, slideOffset)
	if next == nil && headless != nil {
		s.dropBrowser(headless)
	} else {
		s.keepBrowser(next)
	}
	if note != "" {
		result.Result = visualPreviewBody(result.Result, note)
	}
	if len(images) > 0 {
		result.ModelImages = append(result.ModelImages, images...)
	}
	return result
}

func appendVisualPreviewNote(body, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return body
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return note
	}
	// A full preview_pptx payload lists every rendered path and its own
	// next_offset. The model is looking at one window, so that payload is
	// replaced by the window note. Any other JSON stays intact.
	if isSlidePreviewJSON(trimmed) {
		return note
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return body
	}
	return strings.TrimRight(body, "\n") + "\n" + note
}

func visualPreviewBody(body, note string) string {
	return appendVisualPreviewNote(body, note)
}

func isSlidePreviewJSON(body string) bool {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	var payload struct {
		Images     []string `json:"images"`
		SlideCount int      `json:"slide_count"`
	}
	if json.Unmarshal([]byte(trimmed), &payload) != nil {
		return false
	}
	return len(payload.Images) > 0 && payload.SlideCount > 0
}

func visualPreviewNeedsPane(source, target string) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}
	if strings.EqualFold(filepath.Ext(target), ".pptx") {
		return true
	}
	// HTML and SVG writes already open the pane. A stylesheet or script has
	// to switch that pane onto the page that loads them.
	return !sameFile(source, target)
}

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

func renderVisualPreview(app *App, headless *browser.PrivateHeadless, path, prior string, slideOffset int) (string, []agent.ToolModelImage, *browser.PrivateHeadless) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pptx":
		note, images := renderPPTXVisualPreview(path, prior, slideOffset)
		return note, images, headless
	default:
		return renderHTMLVisualPreview(app, headless, path)
	}
}

func renderPPTXVisualPreview(path, prior string, slideOffset int) (string, []agent.ToolModelImage) {
	if slideOffset < 0 {
		slideOffset = 0
	}
	if note, images, ok := reusePPTXPreviewImages(path, prior, slideOffset); ok {
		return note, images
	}
	if note, images, ok := reuseCachedSlidePreview(path, slideOffset); ok {
		return note, images
	}
	dir, err := os.MkdirTemp("", "maclaw-pptx-preview-")
	if err != nil {
		return visualPreviewMiss(err.Error()), nil
	}
	defer removePreviewTemp(dir)
	result, err := pptx.RenderPreview(path, pptx.PreviewOptions{
		OutputDir:   dir,
		Width:       pptx.DefaultPreviewWidth,
		MaxSlides:   visualPreviewSlideWindow,
		SlideOffset: slideOffset,
	})
	if err != nil || result == nil || len(result.Images) == 0 {
		if err == nil {
			err = fmt.Errorf("没有渲染出页面")
		}
		return visualPreviewMiss(err.Error()), nil
	}
	limit := len(result.Images)
	if limit > visualPreviewSlideWindow {
		limit = visualPreviewSlideWindow
	}
	images, first, last := loadSlidePreviews(result.Images[:limit])
	if len(images) == 0 {
		return visualPreviewMiss("预览文件无法读取"), nil
	}
	if first == 0 {
		first = slideOffset + 1
	}
	if last < first {
		last = first + len(images) - 1
	}
	note := slideWindowNote(first, last, result.SlideCount, result.MissingFonts)
	return note, captionPreviewImages(images, note)
}

func visualPreviewMiss(reason string) string {
	return fmt.Sprintf("没有预览图（%s）。不要声称已经看过版式。", reason)
}

func slideWindowNote(start, end, total int, missing []string) string {
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if total > 0 && end > total {
		end = total
	}
	note := fmt.Sprintf("预览图已附上：第 %d-%d 页 / 共 %d 页。按这几页改溢出、重叠、间距和配色。", start, end, total)
	if total > end {
		note += fmt.Sprintf(" 还有后续页。先改这几页，再继续看：读取该 pptx 时把 start_line 设为 %d；若用 preview_pptx，把 slide_offset 设为 %d，max_slides 设为 %d。", end+1, end, visualPreviewSlideWindow)
	} else if total > 0 {
		note += " 这是最后几页。"
	}
	if len(missing) > 0 {
		note += " 缺字体: " + strings.Join(missing, ", ") + "。"
	}
	return note
}

func captionPreviewImages(images []agent.ToolModelImage, note string) []agent.ToolModelImage {
	if len(images) == 0 || strings.TrimSpace(note) == "" {
		return images
	}
	images[0].Caption = note
	return images
}

func renderHTMLVisualPreview(app *App, headless *browser.PrivateHeadless, path string) (string, []agent.ToolModelImage, *browser.PrivateHeadless) {
	if app == nil {
		return visualPreviewMiss("预览服务不可用"), nil, headless
	}
	info, err := app.OpenWebPreview(path)
	if err != nil || info == nil || strings.TrimSpace(info.EntryURL) == "" {
		if err == nil {
			err = fmt.Errorf("没有入口地址")
		}
		return visualPreviewMiss(err.Error()), nil, headless
	}
	defer app.CloseWebPreview(info.ID)
	if headless == nil {
		started, startErr := browser.StartPrivateHeadless()
		if startErr != nil {
			return visualPreviewMiss(startErr.Error()), nil, nil
		}
		headless = started
	}
	png, err := headless.ScreenshotURL(info.EntryURL)
	if err != nil || strings.TrimSpace(png) == "" {
		if err == nil {
			err = fmt.Errorf("空截图")
		}
		if previewBrowserDead(err) {
			return visualPreviewMiss(err.Error()), nil, nil
		}
		return visualPreviewMiss(err.Error()), nil, headless
	}
	png, _, _ = prepareVisionScreenshot(png)
	if strings.TrimSpace(png) == "" {
		return visualPreviewMiss("截图无法缩放"), nil, headless
	}
	return "预览图已附上。按图检查布局、间距、配色和样式是否生效。", []agent.ToolModelImage{{
		MIME:    "image/png",
		Base64:  png,
		Purpose: agent.VisualPreviewImagePurpose,
	}}, headless
}

func previewBrowserDead(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	// A page that did not finish, an empty capture, or a refused URL leaves
	// the browser usable. Connection and navigation failures do not.
	switch {
	case strings.Contains(text, "未在时限内加载完成"),
		strings.Contains(text, "预览截图为空"),
		strings.Contains(text, "只打开本地预览地址"):
		return false
	default:
		return true
	}
}

func removePreviewTemp(dir string) {
	var err error
	for i := 0; i < 5; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func reusePPTXPreviewImages(deck, prior string, fallbackOffset int) (string, []agent.ToolModelImage, bool) {
	var payload struct {
		Images       []string `json:"images"`
		SlideCount   int      `json:"slide_count"`
		MissingFonts []string `json:"missing_fonts"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(prior)), &payload) != nil || len(payload.Images) == 0 {
		return "", nil, false
	}
	chosen := selectSlidePreviewPaths(payload.Images, fallbackOffset, visualPreviewSlideWindow)
	if len(chosen) == 0 {
		return "", nil, false
	}
	for _, imagePath := range chosen {
		if !slidePreviewImageAllowed(deck, imagePath) {
			return "", nil, false
		}
	}
	blobs, ok := readSlideBlobs(deck, chosen)
	if !ok {
		return "", nil, false
	}
	images, first, last := loadSlidePreviewBlobs(chosen, blobs)
	if len(images) != len(chosen) || first != fallbackOffset+1 {
		return "", nil, false
	}
	if last < first {
		last = first + len(images) - 1
	}
	note := slideWindowNote(first, last, payload.SlideCount, payload.MissingFonts)
	return note, captionPreviewImages(images, note), true
}

func selectSlidePreviewPaths(paths []string, offset, window int) []string {
	if window < 1 {
		window = visualPreviewSlideWindow
	}
	if offset < 0 {
		offset = 0
	}
	wantStart := offset + 1
	wantEnd := wantStart + window - 1
	type item struct {
		n    int
		path string
	}
	items := make([]item, 0, window)
	for _, path := range paths {
		n := slideNumberFromPreviewName(path)
		if n < wantStart || n > wantEnd {
			continue
		}
		items = append(items, item{n: n, path: path})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].n < items[j].n })
	if len(items) == 0 || items[0].n != wantStart {
		return nil
	}
	out := []string{items[0].path}
	prev := items[0].n
	for _, it := range items[1:] {
		if it.n != prev+1 {
			break
		}
		out = append(out, it.path)
		prev = it.n
	}
	return out
}

func readSlideBlobs(deck string, paths []string) ([][]byte, bool) {
	lock := pptxPreviewLock(deck)
	lock.Lock()
	defer lock.Unlock()
	blobs := make([][]byte, 0, len(paths))
	for _, imagePath := range paths {
		data, err := os.ReadFile(imagePath)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		blobs = append(blobs, data)
	}
	return blobs, true
}

func loadSlidePreviews(paths []string) (images []agent.ToolModelImage, first, last int) {
	blobs := make([][]byte, 0, len(paths))
	names := make([]string, 0, len(paths))
	for _, imagePath := range paths {
		data, err := os.ReadFile(imagePath)
		if err != nil || len(data) == 0 {
			break
		}
		blobs = append(blobs, data)
		names = append(names, imagePath)
	}
	return loadSlidePreviewBlobs(names, blobs)
}

func loadSlidePreviewBlobs(names []string, blobs [][]byte) (images []agent.ToolModelImage, first, last int) {
	images = make([]agent.ToolModelImage, 0, len(blobs))
	for i, data := range blobs {
		if i >= len(names) || !pngBytesOK(data) {
			break
		}
		encoded := slidePNGForVision(data)
		if encoded == "" {
			break
		}
		n := slideNumberFromPreviewName(names[i])
		if n <= 0 {
			break
		}
		if first == 0 {
			first = n
		}
		last = n
		images = append(images, agent.ToolModelImage{
			MIME:    "image/png",
			Base64:  encoded,
			Purpose: agent.VisualPreviewImagePurpose,
		})
	}
	return images, first, last
}

// slidePNGForVision sends the original bytes when the slide is already small
// enough for the model. A larger slide is resized. pngBytesOK has already
// decoded the file, so this does not decode a second time unless a resize is
// required.
func slidePNGForVision(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	long := cfg.Width
	if cfg.Height > long {
		long = cfg.Height
	}
	if err != nil || long <= 0 || long > computerUseVisionMaxEdge || ExceedsImageSizeLimit(encoded, computerUseVisionMaxBytes) {
		resized, _, _ := prepareVisionScreenshot(encoded)
		if strings.TrimSpace(resized) == "" || !pngBase64OK(resized) {
			return ""
		}
		return resized
	}
	return encoded
}

func pngBase64OK(encoded string) bool {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && pngBytesOK(raw)
}

func pngBytesOK(data []byte) bool {
	if len(data) < 8 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		return false
	}
	_, err := png.Decode(bytes.NewReader(data))
	return err == nil
}

func reuseCachedSlidePreview(deck string, slideOffset int) (string, []agent.ToolModelImage, bool) {
	deck = strings.TrimSpace(deck)
	if deck == "" {
		return "", nil, false
	}
	// The pane renders into this same directory. Read the files while holding
	// its lock, then decode after releasing it.
	names, blobs, total, miss, ok := snapshotCachedSlideWindow(deck, slideOffset)
	if !ok {
		return "", nil, false
	}
	if miss != "" {
		return miss, nil, true
	}
	images, first, last := loadSlidePreviewBlobs(names, blobs)
	if len(images) != len(names) || first != slideOffset+1 {
		return "", nil, false
	}
	if last < first {
		last = first + len(images) - 1
	}
	note := slideWindowNote(first, last, total, nil)
	return note, captionPreviewImages(images, note), true
}

func snapshotCachedSlideWindow(deck string, slideOffset int) (names []string, blobs [][]byte, total int, miss string, ok bool) {
	lock := pptxPreviewLock(deck)
	lock.Lock()
	defer lock.Unlock()
	info, err := os.Stat(deck)
	if err != nil || info.IsDir() {
		return nil, nil, 0, "", false
	}
	cached := cachedPptxPreview(deck, pptx.DefaultPreviewDir(deck), info.ModTime())
	if cached == nil {
		return nil, nil, 0, "", false
	}
	if slideOffset >= cached.SlideCount {
		return nil, nil, cached.SlideCount, visualPreviewMiss(fmt.Sprintf("slide_offset %d 超出范围（共 %d 页）", slideOffset, cached.SlideCount)), true
	}
	chosen := selectSlidePreviewPaths(cached.Images, slideOffset, visualPreviewSlideWindow)
	if len(chosen) == 0 {
		return nil, nil, 0, "", false
	}
	blobs = make([][]byte, 0, len(chosen))
	for _, imagePath := range chosen {
		data, err := os.ReadFile(imagePath)
		if err != nil || len(data) == 0 {
			return nil, nil, 0, "", false
		}
		blobs = append(blobs, data)
	}
	// A rewrite that lands while the files are being copied must not be
	// paired with pictures of the previous deck.
	again, err := os.Stat(deck)
	if err != nil || !again.ModTime().Equal(info.ModTime()) || again.Size() != info.Size() {
		return nil, nil, 0, "", false
	}
	return chosen, blobs, cached.SlideCount, "", true
}

func slideNumberFromPreviewName(path string) int {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	n, err := strconv.Atoi(strings.TrimPrefix(base, "slide_"))
	if err != nil || n <= 0 || !strings.HasPrefix(base, "slide_") {
		return 0
	}
	return n
}

func slidePreviewImageAllowed(deck, image string) bool {
	if !strings.EqualFold(filepath.Ext(image), ".png") {
		return false
	}
	deckDir, err := filepath.Abs(filepath.Dir(deck))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(image)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(deckDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// pptxPreviewRevision identifies the deck bytes the right pane is showing.
// A later write changes the modification time or size, so the pane loads the
// new slides. A page check of the same file keeps the same revision.
func pptxPreviewRevision(abs string) string {
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
}

func visualPreviewDisplayPath(abs, diskProject string) string {
	normalized := normalizeSubAgentCodeEventPath(abs, diskProject)
	if normalized.displayPath != "" {
		return normalized.displayPath
	}
	return filepath.Base(abs)
}

// visualPreviewRevisionPath is the file whose bytes should invalidate the
// open preview. A stylesheet or script write keeps the HTML entry as the
// page, so the revision has to follow the file that was just written.
func visualPreviewRevisionPath(source, target string) string {
	source = strings.TrimSpace(source)
	if source != "" && !sameFile(source, target) {
		return source
	}
	return target
}

func emitVisualPreviewPane(app *App, source, abs, projectPath, diskProject, sessionID string) {
	if app == nil || app.codeEventEmitter == nil || strings.TrimSpace(abs) == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	name := filepath.Base(abs)
	evt := CodeFileEvent{
		SessionID:       sessionID,
		FilePath:        visualPreviewDisplayPath(abs, diskProject),
		FileName:        name,
		AbsPath:         abs,
		OpType:          "modify",
		ForceOpen:       true,
		ProjectPath:     projectPath,
		PreviewRevision: pptxPreviewRevision(visualPreviewRevisionPath(source, abs)),
	}
	if ext == ".pptx" {
		evt.Content = ""
		evt.Language = "pptx"
		app.codeEventEmitter.EmitCodeFileEvent(evt)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil || len(data) > maxCodeFileSize || !isCodePreviewTextContent(data) {
		return
	}
	evt.Content = string(data)
	switch ext {
	case ".svg":
		evt.Language = "svg"
	default:
		evt.Language = "html"
	}
	app.codeEventEmitter.EmitCodeFileEvent(evt)
}

func legacyVisualPreviewPath(name, argsJSON string) string {
	path, _ := legacyVisualPreviewRequest(name, argsJSON)
	return path
}

func legacyVisualPreviewRequest(name, argsJSON string) (string, int) {
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return "", 0
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write_file", "edit_file", "edit_lines":
		return firstVisualArg(args, "path", "file_path"), 0
	case "office":
		action := strings.ToLower(strings.TrimSpace(firstVisualArg(args, "action")))
		switch action {
		case "write_pptx", "generate_pptx":
			return firstVisualArg(args, "file_path", "path"), 0
		case "preview_pptx":
			return firstVisualArg(args, "file_path", "path"), visualIntArg(args, "slide_offset")
		default:
			return "", 0
		}
	default:
		return "", 0
	}
}

func visualIntArg(args map[string]interface{}, key string) int {
	switch n := args[key].(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	case json.Number:
		i, err := n.Int64()
		if err == nil && i > 0 {
			return int(i)
		}
	}
	return 0
}

func firstVisualArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if text, ok := args[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

const visualPreviewPromptSentence = "工具结果里附带了页面或幻灯片预览图时，按图改溢出、重叠、间距、配色和没生效的样式。幻灯片每次给 3 页；结果给出下一页页码时，先改这几页，再用同一 pptx 的 start_line（或 preview_pptx 的 slide_offset）继续看，直到最后一页。没有图就不要说已经看过版式。"

func (c *sharedAgentLoopCallbacks) noteTrustedVisualPreview(path string) {
	if c == nil || c.handler == nil {
		return
	}
	workspace := trustedPrincipalBoundWorkspace(c.handler, c.semanticPrincipalID())
	abs, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return
	}
	c.noteVisualPreview(abs)
}

func (c *sharedAgentLoopCallbacks) noteVisualPreview(abs string) {
	if c == nil || strings.TrimSpace(abs) == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	c.noteVisualPreviewFor(abs, c.semanticPrincipalID())
}

func (c *sharedAgentLoopCallbacks) noteVisualPreviewFor(abs, owner string) {
	c.noteVisualPreviewForPage(abs, owner, 0)
}

func (c *sharedAgentLoopCallbacks) noteVisualPreviewForPage(abs, owner string, slideOffset int) {
	if c == nil || strings.TrimSpace(abs) == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	project := ""
	disk := ""
	session := ""
	if c.handler != nil {
		disk = localToolCodePreviewProjectPath(c.handler, owner)
		project = disk
		if route := codePreviewRouteProjectPath(owner, disk); route != "" {
			project = route
		}
		session = localToolCodePreviewSessionID(owner)
	}
	c.visualPreview.notePage(abs, project, disk, session, slideOffset)
}

func (c *sharedAgentLoopCallbacks) noteTrustedVisualPreviewPage(path string, startLine int) {
	if c == nil || c.handler == nil {
		return
	}
	offset := 0
	if startLine > 1 {
		offset = startLine - 1
	}
	workspace := trustedPrincipalBoundWorkspace(c.handler, c.semanticPrincipalID())
	abs, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return
	}
	c.noteVisualPreviewForPage(abs, c.semanticPrincipalID(), offset)
}

func (c *sharedAgentLoopCallbacks) finishVisualPreview(name, argsJSON string, result agent.ToolExecutionResult) agent.ToolExecutionResult {
	if c == nil {
		return result
	}
	if c.visualPreview == nil || !c.visualPreview.hasPath() {
		if result.Outcome != agent.ToolExecutionOutcomeOK || c.handler == nil {
			return result
		}
		raw, offset := legacyVisualPreviewRequest(name, argsJSON)
		if raw == "" {
			return result
		}
		abs, err := c.handler.resolveFileToolPathForOwner(raw, c.userID)
		if err != nil {
			return result
		}
		c.noteVisualPreviewForPage(abs, c.userID, offset)
	}
	if c.visualPreview == nil || !c.visualPreview.hasPath() {
		return result
	}
	var app *App
	if c.handler != nil {
		app = c.handler.app
	}
	return c.visualPreview.finish(app, c.llmCfg.SupportsVision, result)
}

func (c *sharedAgentLoopCallbacks) CloseVisualPreview() {
	if c == nil || c.visualPreview == nil {
		return
	}
	c.visualPreview.close()
}

func (c *codingSubAgentCallbacks) noteCodingVisualPreview(path string) {
	if c == nil {
		return
	}
	abs := codingVisualPreviewAbs(path, c.projectPath())
	if abs == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	c.visualPreview.note(abs, c.previewRouteProjectPath(), c.projectPath(), c.codeSessionID())
}

func (c *codingSubAgentCallbacks) noteCodingVisualPreviewPage(path string, slideOffset int) {
	if c == nil {
		return
	}
	abs := codingVisualPreviewAbs(path, c.projectPath())
	if abs == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	c.visualPreview.notePage(abs, c.previewRouteProjectPath(), c.projectPath(), c.codeSessionID(), slideOffset)
}

func (c *codingSubAgentCallbacks) finishCodingVisualPreview(result agent.ToolExecutionResult) agent.ToolExecutionResult {
	if c == nil || c.visualPreview == nil {
		return result
	}
	var app *App
	if c.subagent != nil && c.subagent.handler != nil {
		app = c.subagent.handler.app
	}
	return c.visualPreview.finish(app, c.GetLLMConfig().SupportsVision, result)
}

func (c *codingSubAgentCallbacks) CloseVisualPreview() {
	if c == nil || c.visualPreview == nil {
		return
	}
	c.visualPreview.close()
}

func codingVisualPreviewAbs(path, project string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		return abs
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return ""
	}
	abs, err := filepath.Abs(filepath.Join(project, path))
	if err != nil {
		return filepath.Join(project, filepath.Clean(path))
	}
	return abs
}
