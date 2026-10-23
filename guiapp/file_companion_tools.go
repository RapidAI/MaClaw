package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

// The companion's closed tool surface: read-only PDF helpers bound to the one
// open document. Tool arguments carry no path on purpose — the open file is
// the entire reach of these tools, and outputs only ever land in the session's
// own figure directory. Every tool-produced image joins the turn's 图N list,
// so the reply displays it by reference instead of restating a path.
const (
	fileCompanionToolReadPages   = "pdf_read_pages"
	fileCompanionToolRenderPage  = "pdf_render_page"
	fileCompanionToolExtractImgs = "pdf_extract_images"

	// A turn's displayable 图N list is bounded: the model cannot flood the
	// reply with renders, and numbering stays deterministic.
	fileCompanionToolFigureCap = 16
	// How many figures one session keeps servable. Older bubbles lose their
	// drawing before the session can grow without end.
	fileCompanionToolLedgerCap = 64
	// How many documents keep their tool figures on disk. A render costs a
	// second to redo; a directory per document ever opened costs forever.
	fileCompanionFigureDirCap = 12
	// One read_pages call returns at most this many pages and runes, so a
	// "read everything" call cannot become an unbounded context injection.
	fileCompanionReadPagesMax      = 8
	fileCompanionReadPagesCapRunes = 30000
	// One extract call registers at most this many embedded images, and stops
	// decoding once this much image data is in hand. A 200 MB scan is not
	// decoded end to end to answer what is on one page.
	fileCompanionExtractImagesMax      = 8
	fileCompanionExtractImagesMaxBytes = 96 << 20
	// An unfiltered scan stops after this long. Found figures return as soon
	// as the budget above is met, so this only bounds the hopeless case.
	fileCompanionExtractScanBudget = 12 * time.Second
	// Reading a 200 MB scan into memory for one page of text is never the
	// right move; refuse instead of thrashing.
	fileCompanionPDFReadCapBytes = 200 << 20
	// Page render bounds. 140 DPI is crisp enough for a diagram and small
	// enough to keep the PNG in the figure budget. fileCompanionRenderBudget is
	// the hard ceiling on the whole fallback chain (Quartz on macOS, then
	// PyMuPDF, poppler, and mutool) and the only thing that caps a runaway; it
	// is sized to cover one slow PyMuPDF import plus a full fallback pass.
	// Each external attempt gets its own independent fileCompanionRenderAttemptBudget
	// window via renderAttemptContext — never a slice of the remaining chain
	// budget — so a cold PyMuPDF import cannot shrink the poppler/mutool
	// fallback's window to zero (the failure mode this whole repair targets).
	fileCompanionRenderMinDPI        = 72
	fileCompanionRenderMaxDPI        = 220
	fileCompanionRenderBudget        = 60 * time.Second
	fileCompanionRenderAttemptBudget = 30 * time.Second
	// Told to the model when no raster engine exists at all, so the user gets
	// an actionable fix instead of a dead end. macOS normally never reaches
	// this: Quartz is tried first and is not an installable package.
	fileCompanionRenderInstallHint = "可以建议用户安装任一渲染引擎，装好后等一两分钟再试，不必重启：" +
		"macOS 平时用系统自带的 PDF 渲染，只有系统渲染不可用时才需要 brew install poppler；" +
		"Linux 上用系统包管理器安装 poppler-utils 或 mupdf-tools，" +
		"Windows 上安装 Poppler for Windows 或 pip install pymupdf。"
)

// errNoRasterEngine is returned by fileCompanionRenderPDFPagePNG when the
// device has no native renderer and none of pymupdf, poppler, or mutool, so
// the caller can grade the failure without re-probing paperRasterEngines() —
// a second probe risks reading a state the first call already resolved and
// just adds lock contention on a path that already ran the probe.
var errNoRasterEngine = errors.New("no pdf raster engine available")

func fileCompanionToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fileCompanionToolReadPages,
				"description": "读取当前打开的 PDF 指定页的原文。没有文字层的扫描页会用本机 PP-OCRv6 识别后再返回。不要安装其他识别引擎。pages 形如 \"7\"、\"3,5\"、\"7-10\"。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pages": map[string]interface{}{
							"type":        "string",
							"description": "从 1 开始的页码，支持逗号分隔和范围，例如 \"7\" 或 \"3,5,7-9\"。单次最多 8 页。",
						},
					},
					"required": []string{"pages"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fileCompanionToolRenderPage,
				"description": "从当前 PDF 的某一页裁出插图（Figure / 图）及其图注。用户要看架构图、流程图或实验图时使用，不要把同页正文和表格一起截进来。figure 是图号（如 1）；不传则裁出该页每一张能分开的图。只有用户明确要看整页版式时才传 full=true。成功后返回图号，回答里写「图N」。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "图所在的页码，从 1 开始",
						},
						"figure": map[string]interface{}{
							"type":        "integer",
							"description": "可选。要裁的图号，例如 1 表示 Figure 1 或图 1。不传则裁出该页全部分得开的图。",
						},
						"full": map[string]interface{}{
							"type":        "boolean",
							"description": "可选。为 true 时渲染整页。只在用户明确要看版式时使用。",
						},
						"dpi": map[string]interface{}{
							"type":        "integer",
							"description": "可选渲染精度 72-220，默认 140",
						},
					},
					"required": []string{"page"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fileCompanionToolExtractImgs,
				"description": "提取当前打开的 PDF 里的内嵌位图（照片、截图）。矢量图表提取不到，那种情况改用 pdf_render_page 渲染对应页。成功后返回图号列表，回答里写「图N」即可展示对应原图。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"page": map[string]interface{}{
							"type":        "integer",
							"description": "可选，只提取这一页；不传则扫描全文档",
						},
					},
				},
			},
		},
	}
}

func isFileCompanionHostTool(name string) bool {
	switch strings.TrimSpace(name) {
	case fileCompanionToolReadPages, fileCompanionToolRenderPage, fileCompanionToolExtractImgs:
		return true
	}
	return false
}

// fileCompanionClosedSurfaceTools is the whole executable surface a companion
// turn gets: the read-only document tools when the open file is a PDF, and
// nothing otherwise. The figures a markdown or docx needs already travel with
// the turn itself.
func (h *IMMessageHandler) fileCompanionClosedSurfaceTools(userID string) []map[string]interface{} {
	if h == nil || h.app == nil {
		return nil
	}
	canonical, err := h.app.fileCompanionOpenDocument(userID)
	if err != nil || !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return nil
	}
	return fileCompanionToolDefinitions()
}

// fileCompanionOpenDocument resolves the one document a companion session may
// touch and revalidates that it is still granted. No tool argument can widen
// this reach.
func (a *App) fileCompanionOpenDocument(userID string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userID), "file-companion:"))
	if sessionID == "" {
		return "", errors.New("companion session is missing")
	}
	canonical := a.fileCompanionSessionPath(sessionID)
	if canonical == "" {
		return "", errors.New("这个会话没有已打开的文档")
	}
	if _, _, err := a.fileCompanionGrant(canonical); err != nil {
		return "", errors.New("这个文档已经不在伴读的打开列表里")
	}
	return canonical, nil
}

// executeFileCompanionHostTool runs one companion document tool. The bool is
// false for a failed call whose text explains why; "ran and found nothing"
// stays true so the model can adapt instead of retrying.
//
// Every call is logged with its outcome. The companion once promised a figure
// and produced none with nothing in the log pointing at why; a silent tool
// failure would bring that back.
func (h *IMMessageHandler) executeFileCompanionHostTool(userID, name, argsJSON string) (string, bool) {
	name = strings.TrimSpace(name)
	text, ok := h.runFileCompanionHostTool(userID, name, argsJSON)
	logFileCompanion("tool %s user=%s ok=%v", name, userID, ok)
	return text, ok
}

func (h *IMMessageHandler) runFileCompanionHostTool(userID, name, argsJSON string) (string, bool) {
	if h == nil || h.app == nil {
		return "伴读宿主不可用", false
	}
	args := map[string]interface{}{}
	if trimmed := strings.TrimSpace(argsJSON); trimmed != "" {
		parsed, err := parseAgentToolArgumentsObject(trimmed)
		if err != nil {
			return fmt.Sprintf("参数必须是 JSON 对象：%v", err), false
		}
		args = parsed
	}
	canonical, err := h.app.fileCompanionOpenDocument(userID)
	if err != nil {
		return "工具不可用：" + err.Error(), false
	}
	if !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return "当前打开的不是 PDF，文档工具只支持 PDF。", false
	}
	switch name {
	case fileCompanionToolReadPages:
		return h.app.fileCompanionToolReadPages(canonical, args)
	case fileCompanionToolRenderPage:
		return h.app.fileCompanionToolRenderPage(userID, canonical, args)
	case fileCompanionToolExtractImgs:
		return h.app.fileCompanionToolExtractImages(userID, canonical, args)
	}
	return "未知工具 " + name, false
}

func fileCompanionToolProgressText(name, argsJSON string) string {
	var args map[string]interface{}
	if trimmed := strings.TrimSpace(argsJSON); trimmed != "" {
		if parsed, err := parseAgentToolArgumentsObject(trimmed); err == nil {
			args = parsed
		}
	}
	switch strings.TrimSpace(name) {
	case fileCompanionToolReadPages:
		return "正在读取文档页原文…"
	case fileCompanionToolRenderPage:
		if fileCompanionFlagArg(args, "full") {
			if page, err := fileCompanionIntArg(args, "page"); err == nil {
				return fmt.Sprintf("正在渲染第 %d 页为图片…", page)
			}
			return "正在渲染页面为图片…"
		}
		if page, err := fileCompanionIntArg(args, "page"); err == nil {
			return fmt.Sprintf("正在裁出第 %d 页的图…", page)
		}
		return "正在裁出页面上的图…"
	case fileCompanionToolExtractImgs:
		return "正在提取文档内嵌图片…"
	}
	return "正在处理文档…"
}

// fileCompanionToolReadPages returns the verbatim text of the requested pages.
func (a *App) fileCompanionToolReadPages(canonical string, args map[string]interface{}) (string, bool) {
	spec, err := fileCompanionPagesArg(args)
	if err != nil {
		return err.Error(), false
	}
	pages, err := fileCompanionParsePageSpec(spec)
	if err != nil {
		return err.Error(), false
	}
	data, tree, err := fileCompanionOpenPDFTree(canonical)
	if err != nil {
		return err.Error(), false
	}
	if len(data) == 0 {
		return "文档内容为空", false
	}
	count := 0
	if tree != nil {
		count = tree.PageCount()
	}
	if count <= 0 {
		count = fileCompanionPDFPageCount(data)
	}
	if count <= 0 {
		return "无法解析这份 PDF 的页数，文档工具读不出原文。", false
	}
	var inRange []int
	dropped := 0
	for _, page := range pages {
		if page > count {
			dropped++
			continue
		}
		inRange = append(inRange, page)
	}
	if len(inRange) == 0 {
		return fmt.Sprintf("页码超出范围：这份 PDF 共 %d 页。", count), false
	}
	if len(inRange) > fileCompanionReadPagesMax {
		dropped += len(inRange) - fileCompanionReadPagesMax
		inRange = inRange[:fileCompanionReadPagesMax]
	}
	var pagesB strings.Builder
	ocrUsed := false
	// Text and OCR share this open. ExtractPageText's empty result falls
	// back to a raw scan of every stream, which is how a scan page is stored.
	var pageText map[int]string
	var textErr error
	if tree != nil {
		pageText, textErr = tree.SelectableText(inRange)
	} else {
		pageText, textErr = agent.PDFSelectableText(data, inRange)
	}
	var scanned map[int]string
	if textErr == nil && fileCompanionRecognizeScanPage == nil {
		empty := make([]int, 0, len(inRange))
		for _, page := range inRange {
			if strings.TrimSpace(pageText[page]) == "" {
				empty = append(empty, page)
			}
		}
		scanned = a.fileCompanionReadEmptyScanPages(canonical, tree, empty)
	}
	for _, page := range inRange {
		var text string
		if textErr != nil {
			text = fmt.Sprintf("(第 %d 页文字提取失败：%v)", page, textErr)
		} else {
			text = strings.TrimSpace(pageText[page])
		}
		if text == "" {
			if fileCompanionRecognizeScanPage != nil {
				if recognized, ok := a.fileCompanionReadScannedPage(canonical, data, page); ok {
					text = recognized
					ocrUsed = true
				} else {
					text = "(本页没有可提取的文字)"
				}
			} else if recognized := strings.TrimSpace(scanned[page]); recognized != "" {
				text = recognized
				ocrUsed = true
			} else {
				text = "(本页没有可提取的文字)"
			}
		}
		fmt.Fprintf(&pagesB, "\n\n--- 第 %d 页 ---\n%s", page, text)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "《%s》共 %d 页，以下为第 %s 页原文：", filepath.Base(canonical), count, fileCompanionJoinPages(inRange))
	if ocrUsed {
		b.WriteString("没有文字层的页已用本机 PP-OCRv6 识别。")
	}
	b.WriteString(pagesB.String())
	out := b.String()
	if dropped > 0 {
		out += fmt.Sprintf("\n\n(另有 %d 页未返回：单次最多 %d 页，需要时请分批调用。)", dropped, fileCompanionReadPagesMax)
	}
	if runes := []rune(out); len(runes) > fileCompanionReadPagesCapRunes {
		out = string(runes[:fileCompanionReadPagesCapRunes]) + "\n\n(文字过长已截断；如需后续内容请按页分批读取。)"
	}
	return out, true
}

// fileCompanionToolRenderPage rasterizes one page and joins the turn's 图N
// list, which is what makes it displayable by reference in the reply.
func (a *App) fileCompanionToolRenderPage(userID, canonical string, args map[string]interface{}) (string, bool) {
	// Refuse before rasterizing: a render can cost the whole budget, and
	// spending it on a figure the turn may not show is pure waste.
	if a.fileCompanionTurnFigureRoom(userID) <= 0 {
		return fmt.Sprintf("本轮已经展示了 %d 张图片，无法继续添加。请在本轮回答里先用已有的图号。", fileCompanionToolFigureCap), false
	}
	page, err := fileCompanionIntArg(args, "page")
	if err != nil {
		return err.Error(), false
	}
	dpi := 140
	if _, present := args["dpi"]; present {
		dpi, err = fileCompanionIntArg(args, "dpi")
		if err != nil {
			return err.Error(), false
		}
		if dpi < fileCompanionRenderMinDPI {
			dpi = fileCompanionRenderMinDPI
		} else if dpi > fileCompanionRenderMaxDPI {
			dpi = fileCompanionRenderMaxDPI
		}
	}
	// The page count comes from the file itself: reading a 150 MB document
	// into memory just to learn that it has 12 pages costs more than the
	// render does.
	if count := fileCompanionPDFPageCountOfFile(canonical); count > 0 && page > count {
		return fmt.Sprintf("页码超出范围：这份 PDF 共 %d 页。", count), false
	}
	wanted := fileCompanionOptionalFigureArg(args)
	if !fileCompanionFlagArg(args, "full") {
		if text, ok, hit := a.fileCompanionReusePageClips(userID, canonical, page, dpi, wanted); hit {
			return text, ok
		}
		crop := false
		allowMiss := false
		switch {
		case fileCompanionClipPageFiguresHook != nil:
			crop = true
			allowMiss = wanted == ""
		case wanted != "":
			crop = true
		default:
			switch fileCompanionPageCaptionState(canonical, page) {
			case 1:
				crop = true
			case -1:
				crop = true
				allowMiss = true
			}
		}
		if crop {
			if text, ok, handled := a.fileCompanionDeliverPageClips(userID, canonical, page, dpi, wanted, allowMiss); handled {
				return text, ok
			}
		}
	}
	outPNG, err := a.fileCompanionToolFigurePath(canonical, fmt.Sprintf("render-p%d-d%d.png", page, dpi))
	if err != nil {
		logFileCompanion("tool render stash failed user=%s: %v", userID, err)
		return "渲染图片缓存目录创建失败。", false
	}
	// Reuse before paying for a render: the same page inside this turn (the
	// model restates a step often), or a render an earlier turn already left
	// on disk for this unchanged file.
	if index := a.fileCompanionTurnFigureIndex(userID, outPNG); index > 0 {
		if info, err := os.Stat(outPNG); err == nil && info.Size() >= paperFigureMinBytes {
			return fmt.Sprintf("第 %d 页已经在本轮渲染过，编号「图%d」。回答里写「图%d」即可展示这张原图。", page, index, index), true
		}
	}
	reused := false
	if info, statErr := os.Stat(outPNG); statErr == nil && info.Size() >= paperFigureMinBytes {
		if src, srcErr := os.Stat(canonical); srcErr == nil && info.ModTime().After(src.ModTime()) {
			reused = true
		}
	}
	if !reused {
		ctx, cancel := context.WithTimeout(context.Background(), fileCompanionRenderBudget)
		defer cancel()
		if err := fileCompanionRenderPDFPagePNG(ctx, canonical, outPNG, page, dpi); err != nil {
			logFileCompanion("tool render failed user=%s page=%d: %v", userID, page, err)
			if errors.Is(err, errNoRasterEngine) {
				return fmt.Sprintf("第 %d 页渲染失败：这台设备没有可用的 PDF 渲染组件（没有带 pymupdf 的 Python，也没有 pdftocairo/pdftoppm/mutool）。"+fileCompanionRenderInstallHint, page), false
			}
			return fmt.Sprintf("第 %d 页渲染失败（%v）。可能是渲染超时或该页内容异常，可降低 dpi 重试，或先用 pdf_read_pages 读取该页文字。", page, err), false
		}
	}
	info, err := os.Stat(outPNG)
	if err != nil || info.Size() < paperFigureMinBytes {
		return fmt.Sprintf("第 %d 页渲染结果为空，可能是一页空白。", page), false
	}
	// Only the header is read: the PNG itself can be megabytes, and all that is
	// wanted here is its size.
	width, height := 0, 0
	if file, err := os.Open(outPNG); err == nil {
		if cfg, _, err := image.DecodeConfig(file); err == nil {
			width, height = cfg.Width, cfg.Height
		}
		file.Close()
	}
	figure := companionFigure{path: outPNG, caption: fmt.Sprintf("第 %d 页整页渲染", page), size: info.Size()}
	// A number this turn already gave this page keeps its number: the stale
	// entry above was for the very same file, so re-registering it would show
	// the same drawing twice under two numbers.
	if index := a.fileCompanionTurnFigureIndex(userID, outPNG); index > 0 {
		return fmt.Sprintf("第 %d 页已经渲染过，编号「图%d」。回答里写「图%d」即可展示这张原图。", page, index, index), true
	}
	index, err := a.appendFileCompanionTurnFigures(userID, []companionFigure{figure})
	if err != nil {
		return err.Error(), false
	}
	detail := ""
	if width > 0 && height > 0 {
		detail = fmt.Sprintf("（%d×%d 像素）", width, height)
	}
	verb := "已把第 %d 页整页渲染为图片"
	if reused {
		verb = "已复用上次渲染的第 %d 页图片"
	}
	return fmt.Sprintf(verb+"%s，编号「图%d」。回答里写「图%d」，宿主会把这张原图放在回答下面展示给用户。", page, detail, index, index), true
}

// fileCompanionToolExtractImages pulls the embedded raster pictures the
// document carries. Vector diagrams never show up here; the error text points
// the model at pdf_render_page for those.
func (a *App) fileCompanionToolExtractImages(userID, canonical string, args map[string]interface{}) (string, bool) {
	// Nothing here is worth paying for when the turn cannot show one more
	// figure: not the read, not the scan, not the files.
	room := a.fileCompanionTurnFigureRoom(userID)
	if room <= 0 {
		return fmt.Sprintf("本轮已经展示了 %d 张图片，无法继续添加。请在本轮回答里先用已有的图号。", fileCompanionToolFigureCap), false
	}
	data, err := fileCompanionReadPDFBytes(canonical)
	if err != nil {
		return err.Error(), false
	}
	count := fileCompanionPDFPageCount(data)
	if count <= 0 {
		return "无法解析这份 PDF 的页数，文档工具提取不出图片。", false
	}
	pageFilter := 0
	if _, present := args["page"]; present {
		pageFilter, err = fileCompanionIntArg(args, "page")
		if err != nil {
			return err.Error(), false
		}
	}
	if pageFilter > count {
		return fmt.Sprintf("页码超出范围：这份 PDF 共 %d 页。", count), false
	}
	// One page at a time, and only until the answer is complete. Extracting
	// every page would decode every picture in a scanned book to answer a
	// question about page 7, so an unfiltered scan also gets a time budget:
	// the library re-parses the document per call, and a 400-page scan with
	// nothing in it must not hold the turn for minutes. A filtered request
	// always runs: it costs exactly one page.
	first := 1
	last := count
	if pageFilter > 0 {
		first = pageFilter
		last = pageFilter
	}
	deadline := time.Now().Add(fileCompanionExtractScanBudget)
	seenObj := map[int]bool{}
	var found []paperFigure
	decoded := 0
	scanned := 0
	for page := first; page <= last; page++ {
		images, err := fileCompanionExtractPageImages(data, page-1)
		scanned++
		if err != nil {
			logFileCompanion("tool extract page failed user=%s page=%d: %v", userID, page, err)
			continue
		}
		for _, extracted := range images {
			if len(found) >= fileCompanionExtractImagesMax || decoded >= fileCompanionExtractImagesMaxBytes {
				break
			}
			if extracted.ObjNum != 0 && seenObj[extracted.ObjNum] {
				continue
			}
			figure, ok := fileCompanionToolFigureFromExtracted(page, extracted)
			if !ok {
				continue
			}
			if extracted.ObjNum != 0 {
				seenObj[extracted.ObjNum] = true
			}
			decoded += len(figure.data)
			found = append(found, figure)
		}
		if len(found) >= fileCompanionExtractImagesMax || decoded >= fileCompanionExtractImagesMaxBytes {
			break
		}
		if pageFilter == 0 && time.Now().After(deadline) {
			break
		}
	}
	if len(found) == 0 {
		if pageFilter > 0 {
			return fmt.Sprintf("第 %d 页没有可提取的内嵌位图。要展示这一页的图表或架构图（矢量图），请改用 pdf_render_page 渲染对应页面。", pageFilter), true
		}
		if scanned < last {
			return fmt.Sprintf("已扫描前 %d 页（共 %d 页）没有找到可提取的内嵌位图。要展示图表或架构图（矢量图），请改用 pdf_render_page 渲染具体页面；要找更靠后的图片，请带 page 参数指定页码。", scanned, count), true
		}
		return "这份 PDF 没有可提取的内嵌位图。要展示图表或架构图（矢量图），请改用 pdf_render_page 渲染对应页面。", true
	}
	// Nothing is written before it is known to be displayable. Writing first
	// and losing the registration to the cap would leave files behind that no
	// bubble refers to, and tell the model only that it failed.
	if len(found) > room {
		found = found[:room]
	}
	registered := make([]companionFigure, 0, len(found))
	perPage := map[int]int{}
	repeated := 0
	for i, figure := range found {
		perPage[figure.page]++
		path, err := a.fileCompanionToolFigurePath(canonical, fmt.Sprintf("image-p%d-%d%s", figure.page, perPage[figure.page], figure.ext))
		if err != nil {
			continue
		}
		// Re-extracting a page this turn already produced rewrites the same
		// file and would earn it a second number; the old one still holds.
		if a.fileCompanionTurnFigureIndex(userID, path) > 0 {
			repeated++
			continue
		}
		if err := os.WriteFile(path, figure.data, 0o644); err != nil {
			continue
		}
		registered = append(registered, companionFigure{
			path:    path,
			caption: fmt.Sprintf("第 %d 页内嵌图%d", figure.page, i+1),
			size:    int64(len(figure.data)),
		})
	}
	if len(registered) == 0 {
		if repeated > 0 {
			return fmt.Sprintf("这 %d 张内嵌图片本轮已经提取过，请沿用之前给出的图号，不要重复提取。", repeated), true
		}
		return "提取到的图片都无法写入展示缓存。", false
	}
	index, err := a.appendFileCompanionTurnFigures(userID, registered)
	if err != nil {
		return err.Error(), false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已提取 %d 张内嵌图片：", len(registered))
	for i, figure := range registered {
		fmt.Fprintf(&b, "\n图%d %s", index+i, figure.caption)
	}
	b.WriteString("\n回答里写对应「图N」，宿主会把那张原图放在回答下面展示给用户。")
	return b.String(), true
}

// fileCompanionToolFigureFromExtracted keeps a usable picture and drops icons,
// rules and undecodable bytes. Unlike the automatic inventory it keeps
// full-page scans: an explicit extract asked for exactly those.
func fileCompanionToolFigureFromExtracted(page int, extracted gopdf2.ExtractedImage) (paperFigure, bool) {
	if len(extracted.Data) < paperFigureMinBytes {
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

const fileCompanionPageRenderPyScript = `import sys

import pymupdf

pdf_path = sys.argv[1]
out_path = sys.argv[2]
page_no = int(sys.argv[3])
dpi = float(sys.argv[4])
doc = pymupdf.open(pdf_path)
page = doc[page_no - 1]
pix = page.get_pixmap(matrix=pymupdf.Matrix(dpi / 72.0, dpi / 72.0), alpha=False)
pix.save(out_path)
`

// popplerRasterArgs builds the argv shared by pdftocairo and pdftoppm. Both
// accept the same -png/-f/-l/-r family and both understand -singlefile:
// without it they append the page number to the output (base-7.png), with it
// the file lands at exactly base.png where the caller looks for it.
func popplerRasterArgs(source, base string, page, dpi int) []string {
	return []string{"-png", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-r", strconv.Itoa(dpi), "-singlefile", source, base}
}

// mutoolRasterArgs builds argv for mupdf's mutool draw. mutool has no
// -f/-l/-singlefile family: the page is a positional argument and -o names
// the output exactly when a single page is drawn.
func mutoolRasterArgs(source, outPNG string, page, dpi int) []string {
	return []string{"draw", "-o", outPNG, "-r", strconv.Itoa(dpi), source, strconv.Itoa(page)}
}

// fileCompanionNativePDFRender draws one PDF page with the operating system's
// PDF engine. darwin sets it to Quartz; every other platform leaves it nil.
// A nil renderer is not an error — the companion then uses pymupdf, poppler,
// and mutool. Tests replace it and must restore the previous value.
var fileCompanionNativePDFRender func(source, outPNG string, page, dpi int) error

// fileCompanionRenderPDFPagePNG rasterizes one page. On macOS the system
// Quartz renderer goes first: a Finder-launched app usually has none of the
// external engines, and probing them can spend the whole turn before
// reporting that. PyMuPDF, then poppler, then mutool still run when Quartz
// is absent or declines the file.
//
// Each external attempt gets its own slice of the caller's budget: a cold
// PyMuPDF import alone can burn tens of seconds, and the poppler fallback —
// which renders a page in about two — must still get a fair shot after it.
// Sharing one deadline across the chain starved the fallback on exactly the
// loaded machines that needed it.
func fileCompanionRenderPDFPagePNG(ctx context.Context, source, outPNG string, page, dpi int) error {
	// Drop any prior output first so a success below means this run produced
	// it. Without this, a stale file from an earlier render would be mistaken
	// for a fresh result when every engine fails this time — the model would
	// then present a drawing from a different document.
	_ = os.Remove(outPNG)
	var lastErr error
	nativeTried := false
	if render := fileCompanionNativePDFRender; render != nil {
		nativeTried = true
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = render(source, outPNG, page, dpi)
		if lastErr == nil {
			// Same floor as the paper path: a few hundred bytes is not a page,
			// and treating it as success would hide the poppler fallback.
			lastErr = fileCompanionRenderedPNG(outPNG)
		}
		if lastErr == nil {
			return nil
		}
		// A failed native pass must not leave a partial PNG for a later engine
		// to mistake for its own output.
		_ = os.Remove(outPNG)
		logFileCompanion("native pdf render failed page=%d: %v", page, lastErr)
	}
	// A cancelled turn must not start the Python probe. That probe is the
	// slow part, and its result would be thrown away.
	if ctx != nil && ctx.Err() != nil {
		if lastErr != nil {
			return lastErr
		}
		return ctx.Err()
	}
	engine := paperRasterEngines()
	if engine.python != "" {
		pyCtx, pyCancel := renderAttemptContext(ctx)
		lastErr = fileCompanionRenderPagePyMuPDF(pyCtx, engine.python, source, outPNG, page, dpi)
		pyCancel()
		if lastErr == nil {
			return nil
		}
	}
	base := strings.TrimSuffix(outPNG, ".png")
	poppler := popplerRasterArgs(source, base, page, dpi)
	for _, tool := range []string{engine.cairo, engine.ppm} {
		if tool == "" {
			continue
		}
		if lastErr = fileCompanionRenderPageCLI(ctx, tool, poppler, outPNG); lastErr == nil {
			return nil
		}
	}
	if engine.mutool != "" {
		if lastErr = fileCompanionRenderPageCLI(ctx, engine.mutool, mutoolRasterArgs(source, outPNG, page, dpi), outPNG); lastErr == nil {
			return nil
		}
	}
	err := fileCompanionRenderFailure(nativeTried, engine, lastErr)
	// The caller reuses a large PNG that is newer than the PDF. A failed
	// engine can leave exactly that file behind, so the next turn would
	// show it without rendering again.
	_ = os.Remove(outPNG)
	return err
}

// fileCompanionRenderFailure grades a chain that produced no page. Quartz
// having been tried is itself an engine: a Mac with no poppler must report
// the Quartz error, not the "install poppler or mupdf" dead end.
func fileCompanionRenderFailure(nativeTried bool, engine paperRasterEngine, lastErr error) error {
	if engine.empty() && !nativeTried {
		return errNoRasterEngine
	}
	if lastErr == nil {
		if nativeTried && engine.empty() {
			return errors.New("native pdf renderer produced no output")
		}
		return errors.New("no pdf raster engine produced output")
	}
	return lastErr
}

// renderAttemptContext returns an attempt-scoped context that expires after
// fileCompanionRenderAttemptBudget, and is cancelled the moment parent is
// done. Each engine attempt uses this instead of deriving its window from the
// shared chain context: deriving would give a later engine only the slice of
// the chain budget the earlier engines had already burned, starving poppler
// or mutool to zero on exactly the loaded machines that need the fallback.
// The parent still cancels the attempt on request abort, and the chain's hard
// outer budget still applies because the parent carries that deadline.
func renderAttemptContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), fileCompanionRenderAttemptBudget)
	if parent != nil {
		go func() {
			select {
			case <-parent.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}

// fileCompanionRenderPageCLI runs one external rasterizer with its own
// attempt budget, then verifies the expected output actually landed — a tool
// can exit zero and still write nothing (bad page range, unwritable path).
func fileCompanionRenderPageCLI(parent context.Context, tool string, args []string, outPNG string) error {
	toolCtx, toolCancel := renderAttemptContext(parent)
	defer toolCancel()
	cmd := exec.CommandContext(toolCtx, tool, args...)
	hideCommandWindow(cmd)
	if err := cmd.Run(); err != nil {
		// The next engine exits zero and writes nothing often enough. Leaving
		// this file in place would make that empty success look like a page.
		_ = os.Remove(outPNG)
		return err
	}
	return fileCompanionRenderedPNG(outPNG)
}

// fileCompanionRenderedPNG reports whether outPNG is large enough to be a
// page. A short file is removed so a later engine can write the same path.
func fileCompanionRenderedPNG(outPNG string) error {
	info, err := os.Stat(outPNG)
	if err != nil {
		return err
	}
	if info.Size() <= paperFigureMinBytes {
		_ = os.Remove(outPNG)
		return errors.New("pdf renderer produced no output")
	}
	return nil
}

func fileCompanionRenderPagePyMuPDF(ctx context.Context, python, source, outPNG string, page, dpi int) error {
	script, err := os.CreateTemp("", "companion-render-*.py")
	if err != nil {
		return err
	}
	name := script.Name()
	defer os.Remove(name)
	if _, err := script.WriteString(fileCompanionPageRenderPyScript); err != nil {
		script.Close()
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, python, name, source, outPNG, strconv.Itoa(page), strconv.Itoa(dpi))
	hideCommandWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		_ = os.Remove(outPNG)
		return fmt.Errorf("pymupdf render failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	// pymupdf can exit zero and still write nothing, or only a few hundred
	// bytes, on a malformed page. That must fall through to poppler or mutool
	// rather than be mistaken for a fresh render.
	if err := fileCompanionRenderedPNG(outPNG); err != nil {
		return fmt.Errorf("pymupdf produced no output at %s: %w", outPNG, err)
	}
	return nil
}

// fileCompanionPDFPageCount reads the page count without the paper pipeline's
// 500-page scan cap: a companion question may very well name page 520 of a
// long manual. Both library entry points can panic on a malformed file, so the
// answer is 0 ("unknown") rather than a crash.
func fileCompanionPDFPageCount(data []byte) (n int) {
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	count, err := gopdf2.GetSourcePDFPageCountV2(data)
	if err != nil || count <= 0 {
		return 0
	}
	return count
}

// fileCompanionPDFPageCountOfFile asks the library to read the file itself, so
// a page count never costs a full read of the document into memory.
func fileCompanionPDFPageCountOfFile(canonical string) (n int) {
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	count, err := gopdf2.GetSourcePDFPageCount(canonical)
	if err != nil || count <= 0 {
		return 0
	}
	return count
}

// fileCompanionExtractPageImages pulls one page's embedded pictures. The
// library re-parses per call, so the caller decides how many pages to pay for.
func fileCompanionExtractPageImages(data []byte, pageIndex int) (images []gopdf2.ExtractedImage, err error) {
	defer func() {
		if recover() != nil {
			images = nil
			err = errors.New("extract pdf images panicked")
		}
	}()
	return gopdf2.ExtractImagesFromPage(data, pageIndex)
}

// fileCompanionOpenPDFTree returns the PDF bytes and one page tree. A scan
// already opened in this process, with the same size and mtime, is reused
// so the next read does not parse the object streams again.
func fileCompanionOpenPDFTree(canonical string) ([]byte, *agent.PDFPageTree, error) {
	info, statErr := os.Stat(canonical)
	if statErr == nil && info != nil && !info.IsDir() {
		if cached, ok := agent.CachedPDFPageTree(canonical, info); ok && cached != nil && len(cached.Bytes()) > 0 {
			return cached.Bytes(), cached, nil
		}
	}
	data, err := fileCompanionReadPDFBytes(canonical)
	if err != nil {
		return nil, nil, err
	}
	tree, openErr := agent.OpenPDFPageTree(data)
	if openErr != nil || tree == nil {
		return data, nil, nil
	}
	if statErr == nil && info != nil && !info.IsDir() && info.Size() == int64(len(data)) && info.Size() <= agent.OCRPDFMaxBytes {
		agent.RememberPDFPageTree(canonical, info, tree)
	}
	return data, tree, nil
}

func fileCompanionReadPDFBytes(canonical string) ([]byte, error) {
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("读取文档失败：%v", err)
	}
	if info.Size() > fileCompanionPDFReadCapBytes {
		return nil, errors.New("这份 PDF 太大，文档工具无法处理")
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		return nil, fmt.Errorf("读取文档失败：%v", err)
	}
	if len(data) == 0 {
		return nil, errors.New("文档内容为空")
	}
	return data, nil
}

// fileCompanionPagesArg reads the page list whether it arrives as "7", "3,5"
// or as the bare number 7. One page is one page, however the model quotes it.
func fileCompanionPagesArg(args map[string]interface{}) (string, error) {
	raw, ok := args["pages"]
	if !ok || raw == nil {
		return "", errors.New("pages 缺失，形如 \"7\" 或 \"3,5,7-9\"")
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", errors.New("pages 不能为空，形如 \"7\" 或 \"3,5,7-9\"")
		}
		return value, nil
	case float64:
		if value != math.Trunc(value) || value < 1 {
			return "", errors.New("pages 必须是正整数或用引号包裹的页码，例如 \"7\" 或 \"3,5,7-9\"")
		}
		return strconv.Itoa(int(value)), nil
	case int:
		if value < 1 {
			return "", errors.New("pages 必须是正整数或用引号包裹的页码，例如 \"7\" 或 \"3,5,7-9\"")
		}
		return strconv.Itoa(value), nil
	case json.Number:
		number, err := value.Int64()
		if err != nil || number < 1 {
			return "", errors.New("pages 必须是正整数或用引号包裹的页码，例如 \"7\" 或 \"3,5,7-9\"")
		}
		return value.String(), nil
	default:
		return "", errors.New("pages 必须是字符串或正整数，例如 \"7\" 或 \"3,5,7-9\"")
	}
}

// fileCompanionParsePageSpec parses "7", "3,5", "7-10" into unique ascending
// 1-based pages. Errors name the offending token so the model can retry.
func fileCompanionParsePageSpec(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("pages 不能为空，例如 \"7\" 或 \"3,5,7-9\"")
	}
	seen := map[int]bool{}
	var out []int
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if lo, hi, ok := fileCompanionPageRange(token); ok {
			for page := lo; page <= hi && len(out) < fileCompanionReadPagesMax*8; page++ {
				if !seen[page] {
					seen[page] = true
					out = append(out, page)
				}
			}
			continue
		}
		page, err := strconv.Atoi(token)
		if err != nil || page <= 0 {
			return nil, fmt.Errorf("页码 %q 不是正整数，pages 形如 \"7\" 或 \"3,5,7-9\"", token)
		}
		if !seen[page] {
			seen[page] = true
			out = append(out, page)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("pages 里没有可用页码")
	}
	sort.Ints(out)
	return out, nil
}

func fileCompanionPageRange(token string) (int, int, bool) {
	parts := strings.SplitN(token, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	lo, errLo := strconv.Atoi(strings.TrimSpace(parts[0]))
	hi, errHi := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errLo != nil || errHi != nil || lo <= 0 || hi < lo {
		return 0, 0, false
	}
	return lo, hi, true
}

func fileCompanionJoinPages(pages []int) string {
	parts := make([]string, 0, len(pages))
	for _, page := range pages {
		parts = append(parts, strconv.Itoa(page))
	}
	return strings.Join(parts, "、")
}

// fileCompanionFlagArg reads an optional boolean. A missing value is false.
// Models send true, "true", or 1 for the same switch.
func fileCompanionFlagArg(args map[string]interface{}, name string) bool {
	raw, ok := args[name]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "y":
			return true
		}
	case float64:
		return value != 0
	}
	return false
}

// fileCompanionOptionalFigureArg is the figure number, or "" when the call
// did not name one. "1", 1, and "Figure 1" all mean figure 1.
func fileCompanionOptionalFigureArg(args map[string]interface{}) string {
	raw, ok := args["figure"]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return fileCompanionFigureNumber(value)
	case float64:
		if value != math.Trunc(value) || value < 1 {
			return ""
		}
		return strconv.Itoa(int(value))
	case int:
		if value < 1 {
			return ""
		}
		return strconv.Itoa(value)
	case json.Number:
		number, err := value.Int64()
		if err != nil || number < 1 {
			return ""
		}
		return strconv.FormatInt(number, 10)
	default:
		return ""
	}
}

var fileCompanionFigureNumberRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

func fileCompanionFigureNumber(text string) string {
	return fileCompanionFigureNumberRe.FindString(strings.TrimSpace(text))
}

// fileCompanionIntArg reads one 1-based integer argument. JSON numbers arrive
// as float64; a fractional or sub-1 value is the model's mistake to retry. A
// quoted number is accepted too: models write {"page":"7"} often enough that
// rejecting it costs a whole extra round trip.
func fileCompanionIntArg(args map[string]interface{}, name string) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0, fmt.Errorf("%s 缺失，必须是正整数", name)
	}
	switch value := raw.(type) {
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < 1 {
			return 0, fmt.Errorf("%s 必须是正整数", name)
		}
		return number, nil
	case float64:
		if value != math.Trunc(value) || value < 1 {
			return 0, fmt.Errorf("%s 必须是正整数", name)
		}
		return int(value), nil
	case int:
		if value < 1 {
			return 0, fmt.Errorf("%s 必须是正整数", name)
		}
		return value, nil
	case json.Number:
		number, err := value.Int64()
		if err != nil || number < 1 {
			return 0, fmt.Errorf("%s 必须是正整数", name)
		}
		return int(number), nil
	default:
		return 0, fmt.Errorf("%s 必须是整数", name)
	}
}

// fileCompanionToolFigurePath is the path of one tool product inside the
// session's own figure directory. Tools never write anywhere else.
//
// The name is deterministic: re-rendering page 7 at the same DPI rewrites the
// same file instead of piling up a timestamped copy per turn, and a repeat
// call inside one turn can be answered from what is already registered.
func (a *App) fileCompanionToolFigurePath(canonical, name string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	session := fileCompanionSessionID(canonical)
	dir := filepath.Join(a.GetDataDir(), "file-companion", "figures", session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// One directory per document ever opened is a slow leak: renders are cheap
	// to redo, and the oldest ones belong to files the user long closed.
	a.fileCompanionPruneFigureDirs(session)
	return filepath.Join(dir, name), nil
}

// fileCompanionPruneFigureDirs keeps the newest figure directories and drops
// the rest. A dropped session's ledger goes with it: the files it vouched for
// are gone, so leaving the entries would only turn a refused request into a
// missing file later.
func (a *App) fileCompanionPruneFigureDirs(keep string) {
	if a == nil {
		return
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "figures")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	if len(entries) <= fileCompanionFigureDirCap {
		return
	}
	type figureDir struct {
		name  string
		mtime time.Time
	}
	dirs := make([]figureDir, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, figureDir{name: entry.Name(), mtime: info.ModTime()})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mtime.After(dirs[j].mtime) })
	kept := map[string]bool{keep: true}
	for i := 0; i < len(dirs) && i < fileCompanionFigureDirCap; i++ {
		kept[dirs[i].name] = true
	}
	st := a.companionRuntime()
	// A document that is still open keeps its drawings however long ago they
	// were made: its bubbles are on screen, not history.
	st.mu.Lock()
	for canonical := range st.grants {
		kept[fileCompanionSessionID(canonical)] = true
	}
	st.mu.Unlock()
	for _, dir := range dirs {
		if kept[dir.name] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, dir.name)); err != nil {
			continue
		}
		st.mu.Lock()
		delete(st.figureLedger, "file-companion:"+dir.name)
		st.mu.Unlock()
		logFileCompanion("figures pruned session=%s", dir.name)
	}
}

// seedFileCompanionTurnFigures resets one turn's 图N list to the figures the
// host attached before the model spoke. Called at turn start; tool calls then
// extend the same list so numbering stays continuous.
func (a *App) seedFileCompanionTurnFigures(userID string, figures []companionFigure) {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	if st.turnFigures == nil {
		st.turnFigures = map[string][]companionFigure{}
	}
	st.turnFigures[userID] = append([]companionFigure(nil), figures...)
	// Seeded drawings are shown in bubbles too, so the ledger vouches for them
	// as well: the next turn reseeds the numbering list, and this turn's image
	// must stay servable after that.
	for _, figure := range figures {
		st.figureLedger = noteFileCompanionLedgerFigure(st.figureLedger, userID, figure.path)
	}
	st.mu.Unlock()
}

// fileCompanionTurnFigureRoom is how many more figures this turn may show. A
// tool checks it before writing anything to disk, so a turn at the cap fails
// without leaving files no bubble refers to.
func (a *App) fileCompanionTurnFigureRoom(userID string) int {
	if a == nil {
		return 0
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	room := fileCompanionToolFigureCap - len(st.turnFigures[userID])
	if room < 0 {
		return 0
	}
	return room
}

// appendFileCompanionTurnFigures registers tool-produced figures and returns
// the 1-based 图N of the first one. A path this turn already carries keeps its
// own number: the same drawing is never listed twice.
func (a *App) appendFileCompanionTurnFigures(userID string, figures []companionFigure) (int, error) {
	if a == nil || len(figures) == 0 {
		return 0, errors.New("没有可注册的图片")
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.turnFigures == nil {
		st.turnFigures = map[string][]companionFigure{}
	}
	current := st.turnFigures[userID]
	fresh := make([]companionFigure, 0, len(figures))
	for _, figure := range figures {
		if fileCompanionTurnFigureIndexLocked(current, figure.path) > 0 {
			continue
		}
		fresh = append(fresh, figure)
	}
	if len(fresh) == 0 {
		return fileCompanionTurnFigureIndexLocked(current, figures[0].path), nil
	}
	if len(current)+len(fresh) > fileCompanionToolFigureCap {
		return 0, fmt.Errorf("本轮展示图片数已达上限 %d 张，无法继续添加", fileCompanionToolFigureCap)
	}
	first := len(current) + 1
	st.turnFigures[userID] = append(current, fresh...)
	// The ledger outlives the turn: this figure stays servable after the next
	// turn reseeds the numbering list.
	for _, figure := range fresh {
		st.figureLedger = noteFileCompanionLedgerFigure(st.figureLedger, userID, figure.path)
	}
	return first, nil
}

// noteFileCompanionLedgerFigure records one servable figure path. The list is
// bounded, so a long session forgets its oldest drawing rather than growing
// without end; the forgotten one is also the least likely to be on screen.
func noteFileCompanionLedgerFigure(ledger map[string][]string, userID, path string) map[string][]string {
	if ledger == nil {
		ledger = map[string][]string{}
	}
	key := fileCompanionFigureKey(path)
	list := ledger[userID]
	for _, existing := range list {
		if fileCompanionFigureKey(existing) == key {
			return ledger
		}
	}
	list = append(list, path)
	if len(list) > fileCompanionToolLedgerCap {
		list = append([]string(nil), list[len(list)-fileCompanionToolLedgerCap:]...)
	}
	ledger[userID] = list
	return ledger
}

// fileCompanionTurnFigures is the turn's complete 图N list: the figures the
// host attached at start, plus everything companion tools produced since.
func (a *App) fileCompanionTurnFigures(userID string) []companionFigure {
	if a == nil {
		return nil
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]companionFigure(nil), st.turnFigures[userID]...)
}

// fileCompanionTurnFigureIndex returns the 1-based 图N already registered for
// a path, or 0 when this turn has no such figure.
func (a *App) fileCompanionTurnFigureIndex(userID, path string) int {
	if a == nil {
		return 0
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	return fileCompanionTurnFigureIndexLocked(st.turnFigures[userID], path)
}

// fileCompanionTurnFigureIndexLocked scans one turn's list without taking the
// lock again: append uses it while holding it.
func fileCompanionTurnFigureIndexLocked(list []companionFigure, path string) int {
	key := fileCompanionFigureKey(path)
	for i, figure := range list {
		if fileCompanionFigureKey(figure.path) == key {
			return i + 1
		}
	}
	return 0
}

// fileCompanionTurnFigureListed reports whether a path was produced by this
// session's companion tools, so the display bridge may serve its bytes. A
// rendered page never sits next to the document, so the per-document inventory
// alone cannot vouch for it. The ledger, not the live 图N list, decides: a
// bubble from two turns ago keeps its drawing.
func (a *App) fileCompanionTurnFigureListed(canonical string) bool {
	if a == nil {
		return false
	}
	key := fileCompanionFigureKey(canonical)
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, paths := range st.figureLedger {
		for _, path := range paths {
			if fileCompanionFigureKey(path) == key {
				return true
			}
		}
	}
	return false
}
