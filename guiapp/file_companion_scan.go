package guiapp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const fileCompanionScanProgress = "正在识别扫描页…"

// fileCompanionOfficeInjectLimit overrides the shared office-read cap for
// companion scan injection. Zero uses agent.MaxOfficeReadFileBytes. Tests set
// it above zero so a tiny image PDF takes the oversized path.
var fileCompanionOfficeInjectLimit int64

// fileCompanionRecognizeScanPage replaces PP-OCRv6 in tests. The real engine
// is not loaded when this is set. The result may be plain page text or a
// "## Page N" transcript.
var fileCompanionRecognizeScanPage func(data []byte, page int) (string, error)

// fileCompanionModelSeesImages overrides SupportsVision in tests.
var fileCompanionModelSeesImages func() bool

var errFileCompanionScanOCRNotReady = errors.New("companion scan ocr not ready")

func fileCompanionOfficeInjectCap() int64 {
	if fileCompanionOfficeInjectLimit > 0 {
		return fileCompanionOfficeInjectLimit
	}
	return agent.MaxOfficeReadFileBytes
}

// fileCompanionPrepareScannedPDF puts one window of an oversized PDF into the
// user message. Files at or under the office-read cap stay with the shared
// injector. The window is wrapped in an auto-extract begin block and omits
// AutoExtractNotice, so the shared injector does not replace it with an
// ocr_recognize instruction the companion cannot run.
func (a *App) fileCompanionPrepareScannedPDF(msg IMUserMessage, canonical, question string, onProgress func(string)) IMUserMessage {
	if a == nil || !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return msg
	}
	if strings.Contains(msg.Text, agent.AutoExtractBeginMarker) {
		return msg
	}
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		return msg
	}
	if info.Size() <= fileCompanionOfficeInjectCap() || info.Size() > fileCompanionPDFReadCapBytes {
		return msg
	}
	data, tree, err := fileCompanionOpenPDFTree(canonical)
	if err != nil || len(data) == 0 || tree == nil {
		return msg
	}
	count := tree.PageCount()
	if count < 1 {
		count = fileCompanionPDFPageCount(data)
	}
	if count < 1 {
		return msg
	}
	sessionID := fileCompanionSessionID(canonical)
	cachePath := a.fileCompanionScanCachePath(sessionID)
	cache := fileCompanionReadScanCache(cachePath)
	start := 1
	if fileCompanionScanQuestionContinues(question) {
		if missing := agent.OCRTranscriptFirstMissing(cache); missing > 1 && missing <= count {
			start = missing
		}
	}
	if start > count {
		start = count - fileCompanionReadPagesMax + 1
		if start < 1 {
			start = 1
		}
	}
	end := start + fileCompanionReadPagesMax - 1
	if end > count {
		end = count
	}

	vision := a.fileCompanionVisionOn()
	slots, delta, ocrCalls, scanPictures, picturesReady := a.fileCompanionScanWindow(tree, cache, start, end, vision, onProgress)
	merged := cache
	if strings.TrimSpace(delta) != "" {
		merged = agent.MergeOCRTranscript(cache, delta)
		if merged != cache {
			fileCompanionWriteScanCache(cachePath, merged)
		}
	}

	scanPages := map[int]struct{}{}
	lead := fileCompanionScanLead{}
	var body strings.Builder
	for _, slot := range slots {
		switch slot.kind {
		case "native":
			lead.native = true
		case "ocr":
			lead.recognized = true
			scanPages[slot.page] = struct{}{}
		case "empty":
			lead.empty = true
			scanPages[slot.page] = struct{}{}
		default:
			lead.missed = true
			scanPages[slot.page] = struct{}{}
			continue
		}
		if strings.TrimSpace(slot.body) == "" {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		fmt.Fprintf(&body, "## Page %d\n%s", slot.page, slot.body)
	}
	pageText := body.String()
	if runes := []rune(pageText); len(runes) > fileCompanionReadPagesCapRunes {
		pageText = string(runes[:fileCompanionReadPagesCapRunes]) + "\n\n（本窗文字已截断）"
	}

	attached := 0
	if vision && len(scanPages) > 0 {
		if picturesReady {
			msg, attached = fileCompanionAttachScanImages(msg, canonical, scanPictures, scanPages)
		} else if payloads, payloadErr := tree.ScanPagePayloads(start, end-start+1); payloadErr == nil {
			msg, attached = fileCompanionAttachScanImages(msg, canonical, payloads, scanPages)
		}
	}
	lead.images = attached > 0
	lead.vision = vision
	lead.start = start
	lead.end = end
	lead.total = count
	if lead.native || lead.recognized || lead.empty {
		lead.next = fileCompanionScanNextPage(merged, start, end, count)
	}

	source := "pending"
	switch {
	case lead.recognized || lead.empty:
		source = "ocr"
	case lead.native:
		source = "native"
	}
	block := fileCompanionScanInjectBlock(canonical, start, end, count, lead.next, source, fileCompanionScanLeadText(lead), pageText)
	msg.Text = strings.TrimRight(msg.Text, "\n") + "\n\n" + block
	logFileCompanion("scan session=%s pages=%d-%d chars=%d vision=%v ocr=%v", sessionID, start, end, len([]rune(pageText)), vision && attached > 0, ocrCalls > 0)
	return msg
}

type fileCompanionScanSlot struct {
	page int
	body string
	kind string
}

type fileCompanionScanPlan struct {
	slot fileCompanionScanSlot
	need bool
	have bool
}

func (a *App) fileCompanionScanWindow(tree *agent.PDFPageTree, cache string, start, end int, vision bool, onProgress func(string)) ([]fileCompanionScanSlot, string, int, []agent.ImageOnlyPDFPage, bool) {
	plans := make([]fileCompanionScanPlan, 0, end-start+1)
	var delta strings.Builder
	needs := make([]int, 0, end-start+1)
	// Text, and the OCR walk below, share the caller's page tree.
	var nativeText map[int]string
	if tree != nil {
		nativeText, _ = tree.SelectableText(fileCompanionScanPageSpan(start, end))
	}
	for page := start; page <= end; page++ {
		native := strings.TrimSpace(nativeText[page])
		if native != "" {
			plans = append(plans, fileCompanionScanPlan{
				slot: fileCompanionScanSlot{page: page, body: native, kind: "native"},
				have: true,
			})
			cached, ok := agent.OCRTranscriptPageBody(cache, page)
			if !ok || !agent.OCRTranscriptPageDone(cache, page) || strings.TrimSpace(cached) != native {
				fileCompanionAppendScanDelta(&delta, page, native)
			}
			continue
		}
		if agent.OCRTranscriptPageDone(cache, page) {
			body, _ := agent.OCRTranscriptPageBody(cache, page)
			body = strings.TrimSpace(body)
			kind := "ocr"
			if fileCompanionScanBodyBlank(body) {
				kind = "empty"
				body = "(本页没有可提取的文字)"
			}
			plans = append(plans, fileCompanionScanPlan{
				slot: fileCompanionScanSlot{page: page, body: body, kind: kind},
				have: true,
			})
			continue
		}
		plans = append(plans, fileCompanionScanPlan{
			slot: fileCompanionScanSlot{page: page},
			need: true,
		})
		needs = append(needs, page)
	}
	ocrCalls := 0
	var pictures []agent.ImageOnlyPDFPage
	picturesReady := false
	if len(needs) == 0 {
		return fileCompanionSlotsFromPlans(plans), delta.String(), 0, nil, false
	}
	// The test hook is one call per page. A cached or native page between
	// two unread pages stays per page too, so the batch cannot walk past
	// that page and spend the image budget on it.
	if fileCompanionRecognizeScanPage == nil && fileCompanionScanPagesContiguous(needs) {
		pictures, picturesReady = a.fileCompanionFillScanBatch(tree, needs, plans, &delta, &ocrCalls, vision, onProgress)
	} else {
		a.fileCompanionFillScanPages(tree, plans, &delta, &ocrCalls, onProgress)
	}
	return fileCompanionSlotsFromPlans(plans), delta.String(), ocrCalls, pictures, picturesReady
}

func fileCompanionScanPageSpan(start, end int) []int {
	if end < start {
		return nil
	}
	pages := make([]int, 0, end-start+1)
	for page := start; page <= end; page++ {
		pages = append(pages, page)
	}
	return pages
}

func fileCompanionScanPagesContiguous(pages []int) bool {
	if len(pages) == 0 {
		return false
	}
	return pages[len(pages)-1]-pages[0]+1 == len(pages)
}

func fileCompanionSlotsFromPlans(plans []fileCompanionScanPlan) []fileCompanionScanSlot {
	out := make([]fileCompanionScanSlot, 0, len(plans))
	for _, plan := range plans {
		out = append(out, plan.slot)
	}
	return out
}

func (a *App) fileCompanionFillScanPages(tree *agent.PDFPageTree, plans []fileCompanionScanPlan, delta *strings.Builder, ocrCalls *int, onProgress func(string)) {
	engineDown := false
	for i := range plans {
		if !plans[i].need {
			continue
		}
		page := plans[i].slot.page
		if engineDown {
			plans[i].slot.kind = "miss"
			plans[i].have = true
			continue
		}
		if *ocrCalls == 0 && onProgress != nil {
			onProgress(fileCompanionScanProgress)
		}
		*ocrCalls++
		raw, recErr := a.fileCompanionRecognizeOneScanPage(tree, page)
		if errors.Is(recErr, errFileCompanionScanOCRNotReady) {
			engineDown = true
		}
		slot, remember := fileCompanionScanSlotFromRecognition(page, raw, recErr)
		plans[i].slot = slot
		plans[i].have = true
		if remember {
			fileCompanionAppendScanDelta(delta, page, slot.body)
		}
	}
}

func (a *App) fileCompanionFillScanBatch(tree *agent.PDFPageTree, needs []int, plans []fileCompanionScanPlan, delta *strings.Builder, ocrCalls *int, vision bool, onProgress func(string)) ([]agent.ImageOnlyPDFPage, bool) {
	if a == nil || tree == nil || len(needs) == 0 {
		fileCompanionMarkScanMisses(plans)
		return nil, false
	}
	if _, _, ok := a.ensureOCRModelFiles(); !ok || !officeImagePDFOCRReady() {
		fileCompanionMarkScanMisses(plans)
		return nil, false
	}
	if onProgress != nil {
		onProgress(fileCompanionScanProgress)
	}
	*ocrCalls = len(needs)
	start := needs[0]
	last := needs[len(needs)-1]
	var batch agent.OCRPDFPageBatch
	var pictures []agent.ImageOnlyPDFPage
	var err error
	if vision {
		batch, pictures, err = tree.OCRImagePages(start, len(needs), last+1, officeImagePDFOCRText, true)
	} else {
		batch, _, err = tree.OCRImagePages(start, len(needs), last+1, officeImagePDFOCRText, false)
	}
	if errors.Is(err, agent.ErrOCRPDFNoPageImage) {
		for i := range plans {
			if !plans[i].need {
				continue
			}
			plans[i].slot = fileCompanionScanSlot{page: plans[i].slot.page, body: "(本页没有可提取的文字)", kind: "empty"}
			plans[i].have = true
			fileCompanionAppendScanDelta(delta, plans[i].slot.page, plans[i].slot.body)
		}
		return pictures, true
	}
	if err != nil {
		fileCompanionMarkScanMisses(plans)
		return nil, false
	}
	for i := range plans {
		if !plans[i].need {
			continue
		}
		page := plans[i].slot.page
		body, ok := agent.OCRTranscriptPageBody(batch.Text, page)
		if !ok {
			plans[i].slot.kind = "miss"
			plans[i].have = true
			continue
		}
		slot, remember := fileCompanionScanSlotFromRecognition(page, body, nil)
		plans[i].slot = slot
		plans[i].have = true
		if remember {
			fileCompanionAppendScanDelta(delta, page, slot.body)
		}
	}
	return pictures, vision
}

func fileCompanionMarkScanMisses(plans []fileCompanionScanPlan) {
	for i := range plans {
		if !plans[i].need {
			continue
		}
		plans[i].slot.kind = "miss"
		plans[i].have = true
	}
}

func fileCompanionScanSlotFromRecognition(page int, raw string, recErr error) (fileCompanionScanSlot, bool) {
	if recErr != nil {
		if errors.Is(recErr, agent.ErrOCRPDFNoPageImage) {
			return fileCompanionScanSlot{page: page, body: "(本页没有可提取的文字)", kind: "empty"}, true
		}
		return fileCompanionScanSlot{page: page, kind: "miss"}, false
	}
	body, broken := fileCompanionScanPageTranscript(page, raw)
	if broken {
		return fileCompanionScanSlot{page: page, kind: "miss"}, false
	}
	kind := "ocr"
	if fileCompanionScanBodyBlank(body) {
		kind = "empty"
		body = "(本页没有可提取的文字)"
	}
	return fileCompanionScanSlot{page: page, body: body, kind: kind}, true
}

// fileCompanionReadScannedPage recognizes one page that has no text layer and
// remembers a finished body in the session cache. An engine failure is not
// stored, so the next read tries that page again.
func (a *App) fileCompanionReadScannedPage(canonical string, data []byte, page int) (string, bool) {
	if a == nil || page < 1 || len(data) == 0 {
		return "", false
	}
	cachePath := a.fileCompanionScanCachePath(fileCompanionSessionID(canonical))
	cache := fileCompanionReadScanCache(cachePath)
	if agent.OCRTranscriptPageDone(cache, page) {
		body, _ := agent.OCRTranscriptPageBody(cache, page)
		body = strings.TrimSpace(body)
		if fileCompanionScanBodyBlank(body) {
			return "", false
		}
		return body, true
	}
	var tree *agent.PDFPageTree
	if len(data) > 0 {
		tree, _ = agent.OpenPDFPageTree(data)
	}
	raw, err := a.fileCompanionRecognizeOneScanPage(tree, page)
	if err != nil {
		if errors.Is(err, agent.ErrOCRPDFNoPageImage) {
			fileCompanionRememberScanPage(cachePath, cache, page, "(本页没有可提取的文字)")
		}
		return "", false
	}
	body, broken := fileCompanionScanPageTranscript(page, raw)
	if broken {
		return "", false
	}
	fileCompanionRememberScanPage(cachePath, cache, page, body)
	if fileCompanionScanBodyBlank(body) {
		return "", false
	}
	return body, true
}

// fileCompanionReadEmptyScanPages OCRs pages that have no text layer, using
// the caller's page tree. A contiguous run is one walk. A hole stays one page
// so the walk does not spend the image budget on a page that already has text.
// The test hook does not come here; that path stays one call per page.
func (a *App) fileCompanionReadEmptyScanPages(canonical string, tree *agent.PDFPageTree, pages []int) map[int]string {
	out := map[int]string{}
	if a == nil || tree == nil || len(pages) == 0 {
		return out
	}
	cachePath := a.fileCompanionScanCachePath(fileCompanionSessionID(canonical))
	cache := fileCompanionReadScanCache(cachePath)
	needs := make([]int, 0, len(pages))
	for _, page := range pages {
		if page < 1 {
			continue
		}
		if agent.OCRTranscriptPageDone(cache, page) {
			body, _ := agent.OCRTranscriptPageBody(cache, page)
			body = strings.TrimSpace(body)
			if !fileCompanionScanBodyBlank(body) {
				out[page] = body
			}
			continue
		}
		needs = append(needs, page)
	}
	if len(needs) == 0 {
		return out
	}
	if _, _, ok := a.ensureOCRModelFiles(); !ok || !officeImagePDFOCRReady() {
		return out
	}
	remember := func(page int, raw string, recErr error) {
		if recErr != nil {
			if errors.Is(recErr, agent.ErrOCRPDFNoPageImage) {
				cache = fileCompanionRememberScanPage(cachePath, cache, page, "(本页没有可提取的文字)")
			}
			return
		}
		body, broken := fileCompanionScanPageTranscript(page, raw)
		if broken {
			return
		}
		cache = fileCompanionRememberScanPage(cachePath, cache, page, body)
		if !fileCompanionScanBodyBlank(body) {
			out[page] = body
		}
	}
	apply := func(start, count, end int, wanted []int) {
		batch, _, err := tree.OCRImagePages(start, count, end, officeImagePDFOCRText, false)
		if errors.Is(err, agent.ErrOCRPDFNoPageImage) {
			for _, page := range wanted {
				remember(page, "", err)
			}
			return
		}
		if err != nil {
			return
		}
		for _, page := range wanted {
			body, ok := agent.OCRTranscriptPageBody(batch.Text, page)
			if !ok {
				continue
			}
			remember(page, body, nil)
		}
	}
	if fileCompanionScanPagesContiguous(needs) {
		apply(needs[0], len(needs), needs[len(needs)-1]+1, needs)
		return out
	}
	for _, page := range needs {
		apply(page, 1, page+1, []int{page})
	}
	return out
}

func (a *App) fileCompanionRecognizeOneScanPage(tree *agent.PDFPageTree, page int) (string, error) {
	if fileCompanionRecognizeScanPage != nil {
		var data []byte
		if tree != nil {
			data = tree.Bytes()
		}
		return fileCompanionRecognizeScanPage(data, page)
	}
	if a == nil || tree == nil {
		return "", errFileCompanionScanOCRNotReady
	}
	if _, _, ok := a.ensureOCRModelFiles(); !ok || !officeImagePDFOCRReady() {
		return "", errFileCompanionScanOCRNotReady
	}
	batch, _, err := tree.OCRImagePages(page, 1, page+1, officeImagePDFOCRText, false)
	if err != nil {
		return "", err
	}
	return batch.Text, nil
}

func (a *App) fileCompanionVisionOn() bool {
	if fileCompanionModelSeesImages != nil {
		return fileCompanionModelSeesImages()
	}
	if a == nil || a.imHandler == nil {
		return false
	}
	return a.imHandler.getMaclawLLMConfig().SupportsVision
}

func (a *App) fileCompanionScanCachePath(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if a == nil || sessionID == "" {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID, "scan-ocr.txt")
}

func fileCompanionReadScanCache(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

func fileCompanionWriteScanCache(path, text string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return
	}
	if err := renameReplacing(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func fileCompanionRememberScanPage(path, cache string, page int, body string) string {
	merged := agent.MergeOCRTranscript(cache, fmt.Sprintf("## Page %d\n%s", page, body))
	if merged != cache {
		fileCompanionWriteScanCache(path, merged)
	}
	return merged
}

func fileCompanionAppendScanDelta(b *strings.Builder, page int, body string) {
	if b == nil {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "## Page %d\n%s", page, body)
}

func fileCompanionScanQuestionContinues(question string) bool {
	switch strings.TrimSpace(question) {
	case "继续", "接着", "接着做":
		return true
	default:
		return false
	}
}

func fileCompanionScanBodyBlank(body string) bool {
	switch strings.TrimSpace(body) {
	case "", "(本页没有可提取的文字)", "[no text recognized]", "[empty page]":
		return true
	default:
		return false
	}
}

func fileCompanionScanPageTranscript(page int, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || fileCompanionScanBodyBlank(raw) {
		return "(本页没有可提取的文字)", false
	}
	if strings.HasPrefix(raw, "[page extract error]") {
		return "", true
	}
	if body, ok := agent.OCRTranscriptPageBody(raw, page); ok {
		body = strings.TrimSpace(body)
		if strings.HasPrefix(body, "[page extract error]") {
			return "", true
		}
		if fileCompanionScanBodyBlank(body) {
			return "(本页没有可提取的文字)", false
		}
		return body, false
	}
	if agent.OCRTranscriptMaxPage(raw) > 0 {
		return "", true
	}
	return raw, false
}

func fileCompanionScanNextPage(cache string, start, end, count int) int {
	for page := start; page <= end; page++ {
		if !agent.OCRTranscriptPageDone(cache, page) {
			return page
		}
	}
	if end < count {
		return end + 1
	}
	return 0
}

type fileCompanionScanLead struct {
	start, end, next, total   int
	native, recognized, empty bool
	missed, vision, images    bool
}

func fileCompanionScanSpan(start, end int) string {
	if start == end {
		return fmt.Sprintf("第 %d 页", start)
	}
	return fmt.Sprintf("第 %d 到 %d 页", start, end)
}

func fileCompanionScanLeadText(s fileCompanionScanLead) string {
	span := fileCompanionScanSpan(s.start, s.end)
	more := "这几页已经到文末。"
	if s.next > 0 {
		more = fmt.Sprintf("后面的页还没读。用户说「继续」时从第 %d 页接着识别。", s.next)
	}
	switch {
	case s.recognized && s.vision && s.images:
		return fmt.Sprintf("这是扫描件。宿主已用本机 PP-OCRv6 识别%s，页图也附在这条消息里。公式以图片为准。%s不要安装其他识别引擎，不要写文档无法读取。", span, more)
	case s.recognized:
		return fmt.Sprintf("这是扫描件。宿主已用本机 PP-OCRv6 识别%s。%s不要安装其他识别引擎，不要写文档无法读取。", span, more)
	case s.empty && !s.missed && !s.native:
		return fmt.Sprintf("这是扫描件。宿主已用本机 PP-OCRv6 看过%s，没有识别出文字。%s不要安装其他识别引擎，不要写文档无法读取。", span, more)
	case s.native && !s.recognized && !s.empty:
		if s.next > 0 {
			return fmt.Sprintf("这份 PDF 超过正文注入上限。宿主已读出%s原文。后面的页还没读。用户说「继续」时从第 %d 页接着读。", span, s.next)
		}
		return fmt.Sprintf("这份 PDF 超过正文注入上限。宿主已读出%s原文。这几页已经到文末。", span)
	case s.vision && s.images:
		return "识别模型还在准备，这几页以图片附上。不要安装其他识别引擎。"
	default:
		return "这是扫描件，本机识别尚未就绪。请稍后再试。不要安装其他识别引擎。"
	}
}

func fileCompanionScanInjectBlock(path string, start, end, total, next int, source, lead, body string) string {
	var b strings.Builder
	// truncated stays false. A true flag asks the shared expander for office,
	// and office stops at 32 MiB. next_page is how the next window is chosen.
	fmt.Fprintf(&b, "%spath=%q format=%q text_source=%q pages=%q total_pages=%d truncated=false",
		agent.AutoExtractBeginMarker, path, "pdf", source, fmt.Sprintf("%d-%d", start, end), total)
	if next > 0 {
		fmt.Fprintf(&b, " next_page=%d", next)
	}
	b.WriteString(" ---\n")
	if strings.TrimSpace(lead) != "" {
		b.WriteString(lead)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(body) != "" {
		b.WriteString(strings.TrimSpace(body))
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%spath=%q ---", agent.AutoExtractEndMarker, path)
	return b.String()
}

func fileCompanionAttachScanImages(msg IMUserMessage, canonical string, payloads []agent.ImageOnlyPDFPage, scanPages map[int]struct{}) (IMUserMessage, int) {
	if len(payloads) == 0 || len(scanPages) == 0 {
		return msg, 0
	}
	stem := strings.TrimSuffix(filepath.Base(canonical), filepath.Ext(canonical))
	attached := 0
	for _, img := range payloads {
		if _, ok := scanPages[img.PageNumber]; !ok || len(img.Data) == 0 {
			continue
		}
		ext := ".png"
		if img.MIME == "image/jpeg" {
			ext = ".jpg"
		}
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		msg.Attachments = append(msg.Attachments, agent.MessageAttachment{
			Type:     "image",
			FileName: fmt.Sprintf("%s-page-%d%s", stem, img.PageNumber, ext),
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(img.Data),
			Size:     int64(len(img.Data)),
		})
		attached++
	}
	return msg, attached
}
