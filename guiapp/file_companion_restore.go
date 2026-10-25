package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/RapidAI/CodeClaw/corelib/doclayout"
	"github.com/RapidAI/CodeClaw/corelib/formula"
	"github.com/RapidAI/CodeClaw/corelib/tablestruct"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// fileCompanionRestoreRecognize replaces PP-OCRv6 in tests. It receives the
// embedded page image and returns lines in that image's pixel space.
var fileCompanionRestoreRecognize func([]byte) ([]agent.ScanTextLine, error)

// fileCompanionRestoreLayout replaces PP-DocLayoutV3 in tests.
var fileCompanionRestoreLayout func(image.Image) ([]doclayout.Region, error)

// fileCompanionRestoreTable replaces the wired table model in tests.
var fileCompanionRestoreTable func(image.Image, []tablestruct.Line) (string, error)

// fileCompanionTableReady replaces the on-disk UNET check in tests.
var fileCompanionTableReady func() bool

// fileCompanionRestoreFormula replaces FormulaNet. Layout formula regions
// are cropped once each; a nil hook uses the loaded formula engine.
var fileCompanionRestoreFormula func(image.Image) (string, error)

var errFileCompanionRestoreOCRNotReady = errors.New("ocr not ready")

var errFileCompanionRestoreFont = errors.New("no cjk font")

const fileCompanionRestorePassword = "这份 PDF 有打开密码，无法按原版式生成新的 PDF。"

const fileCompanionRestoreOCRNotReadyText = "这是扫描件，本机识别尚未就绪。请稍后再试。不要安装其他识别引擎。"

type fileCompanionRestoreCache struct {
	Size  int64 `json:"size"`
	Mtime int64 `json:"mtime"`
	// PDFSize is the byte length of searchable.pdf written for this cache.
	// Zero means the file on disk is not this cache, even when the file exists.
	PDFSize int64 `json:"pdf_size,omitempty"`
	// Count is the source page count recorded with this cache. Zero means the
	// count was not stored yet, so a click cannot tell a finished conversion
	// from a partial one.
	Count int                             `json:"count,omitempty"`
	Pages []fileCompanionRestoreCachePage `json:"pages"`
}

type fileCompanionRestoreCachePage struct {
	Page           int                    `json:"page"`
	AlreadyText    bool                   `json:"already_text,omitempty"`
	NoPicture      bool                   `json:"no_picture,omitempty"`
	FormulaPending bool                   `json:"formula_pending,omitempty"`
	LayoutChecked  bool                   `json:"layout_checked,omitempty"`
	TablePending   bool                   `json:"table_pending,omitempty"`
	Failed         string                 `json:"failed,omitempty"`
	FailTries      int                    `json:"fail_tries,omitempty"`
	Lines          []agent.PlacedScanLine `json:"lines,omitempty"`
	Blocks         []restoreBlock         `json:"blocks,omitempty"`
}

type restoreBlock struct {
	Order int     `json:"order"`
	Label string  `json:"label"`
	Score float64 `json:"score,omitempty"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Text  string  `json:"text,omitempty"`
	HTML  string  `json:"html,omitempty"`
}

// restoreCall is one pdf_restore batch. rest is how many selected pages did
// not fit in this batch. alreadyText means the file already has a text layer
// and was not rewritten. The convert button stops on these fields.
type restoreCall struct {
	text        string
	ok          bool
	alreadyText bool
	rest        int
	pdfPath     string
	failed      []string
	wave        int
}

// fileCompanionToolRestore writes a selectable PDF beside the session cache.
// The open file is left unchanged. One call recognizes at most
// fileCompanionRestorePagesMax pages and continues from the cache.
func (a *App) fileCompanionToolRestore(canonical string, args map[string]interface{}) (string, bool) {
	call := a.restoreCompanionPDF(canonical, args, 0, true, nil)
	return call.text, call.ok
}

// publish writes searchable.pdf in this call. The convert button passes false
// and publishes once when the click stops, so a long scan is drawn once.
// reuse is that click's reader. While the file's size and mtime still match,
// pages read here stay on it and the draw does not extract them again.
func (a *App) restoreCompanionPDF(canonical string, args map[string]interface{}, failWave int, publish bool, reuse *agent.ScanRestore) (call restoreCall) {
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	defer func() { call.wave = failWave }()
	info, err := os.Stat(canonical)
	if err != nil {
		return restoreCall{text: "读取文档失败：" + err.Error()}
	}
	var data []byte
	var doc *agent.ScanRestore
	if reuse != nil && reuse.Matches(info.Size(), info.ModTime().UnixNano()) {
		data = reuse.Bytes()
		doc = reuse
	} else {
		data, err = fileCompanionReadPDFBytes(canonical)
		if err != nil {
			return restoreCall{text: err.Error()}
		}
		info, err = os.Stat(canonical)
		if err != nil {
			return restoreCall{text: "读取文档失败：" + err.Error()}
		}
		doc = agent.NewScanRestore(data)
		doc.BindSource(info.Size(), info.ModTime().UnixNano())
	}
	if agent.PDFIsEncrypted(data) {
		return restoreCall{text: fileCompanionRestorePassword}
	}
	count, countErr := doc.PageCount()
	if countErr != nil || count < 1 {
		return restoreCall{text: "无法读取这份 PDF 的页数。"}
	}
	requested, explicit, err := fileCompanionOptionalPages(args)
	if err != nil {
		return restoreCall{text: err.Error()}
	}
	for _, page := range requested {
		if page > count {
			return restoreCall{text: fmt.Sprintf("这份 PDF 共 %d 页，没有第 %d 页。", count, page)}
		}
	}
	cache, cachePath, pdfPath, pdfSize, _, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil {
		return restoreCall{text: "读取恢复进度失败：" + err.Error()}
	}
	if !explicit && len(cache) == 0 && doc.EveryPageIsDigital(count) {
		return restoreCall{text: "这份 PDF 已有文字层，可以直接选择和搜索，不需要再恢复。", ok: true, alreadyText: true}
	}
	if failWave < 1 {
		failWave = nextFailWave(cache)
	}
	formulaEng, formulaReady := a.restoreFormulaEngine()
	layoutReady := a.restoreLayoutReady()
	todo, rest := selectRestorePages(count, requested, explicit, cache, formulaReady, layoutReady, failWave)
	if len(todo) == 0 {
		return a.finishRestoreFromCache(doc, cache, cachePath, pdfPath, info.Size(), info.ModTime().UnixNano(), pdfSize, count, formulaReady, failWave, publish)
	}
	if fontErr := docgen.SystemCJKFontAvailable(); fontErr != nil {
		return restoreCall{text: "没有找到可嵌入的中文字体，无法生成可选中的 PDF。"}
	}
	fresh := make([]fileCompanionRestoreCachePage, 0, len(todo))
	var failed []string
	ocrStopped := false
	ocrRest := rest
	for i, page := range todo {
		entry, recErr := a.recognizeRestorePage(doc, page, formulaEng, formulaReady)
		if errors.Is(recErr, errFileCompanionRestoreOCRNotReady) {
			// Pages already recognized in this batch stay in the cache. The
			// page that stopped, and the pages after it, stay uncached so the
			// next call starts there instead of repeating this batch.
			ocrStopped = true
			ocrRest = rest + len(todo) - i
			break
		}
		if recErr != nil {
			// A failed page is a result. Leaving it out of the cache makes it
			// look unprocessed, so the next window is filled with the same
			// pages and a later page never runs.
			failed = append(failed, fmt.Sprintf("第 %d 页识别失败：%v", page, recErr))
			fresh = append(fresh, fileCompanionRestoreCachePage{Page: page, Failed: recErr.Error(), FailTries: failWave})
			continue
		}
		fresh = append(fresh, entry)
	}
	if ocrStopped && len(fresh) == 0 {
		return restoreCall{text: fileCompanionRestoreOCRNotReadyText, rest: ocrRest}
	}
	if len(fresh) == 0 {
		if len(failed) > 0 {
			return restoreCall{text: strings.Join(failed, "\n"), failed: failed, rest: rest}
		}
		return restoreCall{text: "没有可恢复的页面。", rest: rest}
	}
	merged := mergeRestoreCache(cache, fresh)
	sourceSize, sourceMtime := info.Size(), info.ModTime().UnixNano()
	if restoreCacheAllText(merged, count) {
		discardSearchablePDF(pdfPath)
		if err := a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, 0, count, merged); err != nil {
			return restoreCall{text: "保存恢复进度失败：" + err.Error()}
		}
		return restoreCallStoppedForOCR(restoreCall{text: "这份 PDF 已有文字层，可以直接选择和搜索，不需要再恢复。", ok: true, alreadyText: true}, ocrStopped, ocrRest)
	}
	if !restoreHasLines(merged) {
		discardSearchablePDF(pdfPath)
		if err := a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, 0, count, merged); err != nil {
			return restoreCall{text: "保存恢复进度失败：" + err.Error()}
		}
		return restoreCallStoppedForOCR(restoreCall{text: fileCompanionRestoreNoLayerMessage(fresh, rest, failed), ok: true, rest: rest, failed: failed}, ocrStopped, ocrRest)
	}
	if !publish {
		// The button draws the PDF once, when the click stops. Recording a
		// zero size keeps an older file from being treated as this cache.
		if err := a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, 0, count, merged); err != nil {
			return restoreCall{text: "保存恢复进度失败：" + err.Error()}
		}
		return restoreCallStoppedForOCR(restoreCall{
			text: fileCompanionRestoreMessage(restoreMessage{
				done:           fresh,
				count:          count,
				rest:           rest,
				failed:         failed,
				formulaLines:   countFormulaLines(fresh),
				formulaPending: cacheHasFormulaPending(fresh),
				layoutBlocks:   countLayoutBlocks(fresh),
				tableHTML:      countTableHTML(fresh),
				layoutPending:  a.restoreLayoutPending(),
				tablePending:   cacheHasTablePending(fresh),
			}),
			ok:     true,
			rest:   rest,
			failed: failed,
		}, ocrStopped, ocrRest)
	}
	font, fontErr := docgen.SystemCJKFontBytes()
	if fontErr != nil {
		return restoreCall{text: "没有找到可嵌入的中文字体，无法生成可选中的 PDF。"}
	}
	out, writeErr := agent.RestoreRebuiltPDF(doc, restoreLinesByPage(merged), restorePageAssets(doc, merged), font)
	if writeErr != nil {
		if errors.Is(writeErr, gopdf2.ErrEncryptedPDF) {
			return restoreCall{text: fileCompanionRestorePassword}
		}
		return restoreCall{text: "生成可选中的 PDF 失败：" + writeErr.Error()}
	}
	if err := writeCompanionBytesAtomic(pdfPath, out); err != nil {
		return restoreCall{text: "写入可选中的 PDF 失败：" + err.Error()}
	}
	if err := a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, int64(len(out)), count, merged); err != nil {
		return restoreCall{text: "可选中的 PDF 已写入，但保存进度失败：" + err.Error(), pdfPath: pdfPath}
	}
	return restoreCallStoppedForOCR(restoreCall{
		text: fileCompanionRestoreMessage(restoreMessage{
			done:           fresh,
			pdfPath:        pdfPath,
			count:          count,
			rest:           rest,
			failed:         failed,
			formulaLines:   countFormulaLines(fresh),
			formulaPending: cacheHasFormulaPending(fresh),
			layoutBlocks:   countLayoutBlocks(fresh),
			tableHTML:      countTableHTML(fresh),
			layoutPending:  a.restoreLayoutPending(),
			tablePending:   cacheHasTablePending(fresh),
		}),
		ok:      true,
		rest:    rest,
		pdfPath: pdfPath,
		failed:  failed,
	}, ocrStopped, ocrRest)
}

func restoreCallStoppedForOCR(call restoreCall, stopped bool, rest int) restoreCall {
	if !stopped {
		return call
	}
	call.ok = false
	call.text = fileCompanionRestoreOCRNotReadyText
	call.rest = rest
	return call
}

func (a *App) finishRestoreFromCache(doc *agent.ScanRestore, cache map[int]fileCompanionRestoreCachePage, cachePath, pdfPath string, sourceSize, sourceMtime, pdfSize int64, count int, formulaReady bool, failWave int, publish bool) restoreCall {
	if restoreCacheAllText(cache, count) {
		discardSearchablePDF(pdfPath)
		if pdfSize != 0 {
			_ = a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, 0, count, cache)
		}
		return restoreCall{text: "这份 PDF 已有文字层，可以直接选择和搜索，不需要再恢复。", ok: true, alreadyText: true}
	}
	path, matErr := a.materializeSearchablePDF(doc, cache, cachePath, pdfPath, sourceSize, sourceMtime, pdfSize, count, publish)
	if matErr != nil {
		return restoreCall{text: restorePublishError(matErr)}
	}
	_, rest := selectRestorePages(count, nil, false, cache, formulaReady, a.restoreLayoutReady(), failWave)
	waiting := formulaPendingSuffix(cache, formulaReady) + tablePendingSuffix(cache)
	if path == "" {
		if !restoreHasLines(cache) {
			return restoreCall{text: fileCompanionRestoreNoLayerMessage(nil, rest, nil), ok: true, rest: rest}
		}
		if rest > 0 {
			return restoreCall{
				text: a.withLayoutDownloadNote(fmt.Sprintf("已按原版式生成新的 PDF，原文件没有改动。后面还有 %d 页。请再次调用 pdf_restore 继续。", rest) + waiting),
				ok:   true,
				rest: rest,
			}
		}
		return restoreCall{
			text: a.withLayoutDownloadNote("已按原版式生成新的 PDF，原文件没有改动。" + waiting),
			ok:   true,
		}
	}
	if rest > 0 {
		return restoreCall{
			text:    a.withLayoutDownloadNote(fmt.Sprintf("已按原版式生成新的 PDF，原文件没有改动。后面还有 %d 页。请再次调用 pdf_restore 继续。结果文件：%s", rest, path) + waiting),
			ok:      true,
			rest:    rest,
			pdfPath: path,
		}
	}
	return restoreCall{
		text:    a.withLayoutDownloadNote(fmt.Sprintf("已按原版式生成新的 PDF，原文件没有改动。结果文件：%s", path) + waiting),
		ok:      true,
		pdfPath: path,
	}
}

func (a *App) restoreFormulaEngine() (*formula.Engine, bool) {
	if fileCompanionRestoreRecognize != nil || a == nil || !a.GetFormulaEnabled() {
		return nil, false
	}
	eng, err := a.ensureFormulaReady()
	if err != nil || eng == nil {
		return nil, false
	}
	return eng, true
}

func (a *App) recognizeRestorePage(doc *agent.ScanRestore, page int, formulaEng *formula.Engine, formulaReady bool) (fileCompanionRestoreCachePage, error) {
	view, err := doc.Page(page)
	if err != nil {
		return fileCompanionRestoreCachePage{}, err
	}
	entry := fileCompanionRestoreCachePage{Page: page}
	if view.HasText {
		entry.AlreadyText = true
		return entry, nil
	}
	if view.NoPicture || len(view.Payload) == 0 {
		entry.NoPicture = true
		return entry, nil
	}
	lines, pending, err := a.recognizeRestoreImage(view.Payload, formulaEng, formulaReady, a.restoreLayoutReady())
	if err != nil {
		return fileCompanionRestoreCachePage{}, err
	}
	lines = a.applyRestoreLayout(&entry, view, lines)
	entry.Lines = agent.PlaceScanLines(view.Placement, lines)
	entry.FormulaPending = pending || entry.FormulaPending
	return entry, nil
}

func (a *App) recognizeRestoreImage(payload []byte, formulaEng *formula.Engine, formulaReady, layoutWillRun bool) ([]agent.ScanTextLine, bool, error) {
	if fileCompanionRestoreRecognize != nil {
		lines, err := fileCompanionRestoreRecognize(payload)
		return lines, false, err
	}
	if _, _, ok := a.ensureOCRModelFiles(); !ok {
		return nil, false, errFileCompanionRestoreOCRNotReady
	}
	results, err := sharedNativeOCRProvider().Recognize(base64.StdEncoding.EncodeToString(payload))
	if err != nil {
		return nil, false, err
	}
	lines := make([]agent.ScanTextLine, 0, len(results))
	for _, result := range results {
		text := strings.TrimSpace(result.Text)
		if text == "" {
			continue
		}
		lines = append(lines, agent.ScanTextLine{Text: text, BBox: result.BBox})
	}
	// Layout formula boxes are the crop. Running the line check first would
	// send each OCR fragment of one formula to the model separately.
	if layoutWillRun {
		return lines, false, nil
	}
	if formulaEng != nil {
		decoded, _, decErr := image.Decode(bytes.NewReader(payload))
		if decErr == nil && decoded != nil {
			lines = agent.ApplyFormulaLines(lines, decoded, formulaEng.Recognize)
		}
		return lines, false, nil
	}
	if !formulaReady && a != nil && a.GetFormulaEnabled() {
		for _, line := range lines {
			if agent.ScanLineLooksLikeFormula(line.Text) {
				return lines, true, nil
			}
		}
	}
	return lines, false, nil
}

func (a *App) restoreLayoutReady() bool {
	if fileCompanionRestoreLayout != nil {
		return true
	}
	if fileCompanionRestoreRecognize != nil {
		return false
	}
	if !doclayout.Ready() || !tablestruct.Ready() {
		a.backgroundEnsureRestoreModels()
	}
	return doclayout.Ready()
}

func (a *App) restoreLayoutPending() bool {
	if fileCompanionRestoreLayout != nil || fileCompanionRestoreRecognize != nil {
		return false
	}
	return !doclayout.Ready()
}

const formulaPendingNote = "有些像公式的行仍是文字识别结果，公式模型还在下载。下载完成后再调用 pdf_restore，这些页会改用公式模型。"

const tablePendingNote = "有表格仍是版面里的文字，表格模型还在下载。下载完成后再调用 pdf_restore，这些页会补上表格结构。"

func formulaPendingSuffix(cache map[int]fileCompanionRestoreCachePage, formulaReady bool) string {
	if formulaReady {
		return ""
	}
	for _, page := range cache {
		if page.FormulaPending {
			return formulaPendingNote
		}
	}
	return ""
}

func tablePendingSuffix(cache map[int]fileCompanionRestoreCachePage) string {
	if restoreTableModelReady() {
		return ""
	}
	for _, page := range cache {
		if page.TablePending {
			return tablePendingNote
		}
	}
	return ""
}

func layoutDownloadNote(pending bool) string {
	if !pending {
		return ""
	}
	return "版面模型还在下载。下载完成后再调用 pdf_restore，这些页会按原来的位置还原。"
}

func (a *App) withLayoutDownloadNote(msg string) string {
	if note := layoutDownloadNote(a.restoreLayoutPending()); note != "" {
		return msg + note
	}
	return msg
}

func restoreModelDownloadAllowed() bool {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	return !strings.Contains(base, ".test")
}

var (
	restoreModelEnsureMu sync.Mutex
	restoreModelEnsuring bool
)

func (a *App) backgroundEnsureRestoreModels() {
	if !restoreModelDownloadAllowed() {
		return
	}
	wantLayout := !doclayout.Ready()
	wantTable := !tablestruct.Ready()
	if !wantLayout && !wantTable {
		return
	}
	restoreModelEnsureMu.Lock()
	if restoreModelEnsuring {
		restoreModelEnsureMu.Unlock()
		return
	}
	restoreModelEnsuring = true
	restoreModelEnsureMu.Unlock()
	hub := ""
	if a != nil {
		hub = a.modelHubURL()
	}
	go func() {
		defer func() {
			restoreModelEnsureMu.Lock()
			restoreModelEnsuring = false
			restoreModelEnsureMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if wantLayout {
			if err := doclayout.EnsureModels(ctx, hub); err != nil {
				log.Printf("[doclayout] download: %v", err)
			}
		}
		if wantTable {
			if err := tablestruct.EnsureModels(ctx, hub); err != nil {
				log.Printf("[tablestruct] download: %v", err)
			}
		}
	}()
}

func layoutRefresh(entry fileCompanionRestoreCachePage, layoutReady bool) bool {
	if !layoutReady || len(entry.Lines) == 0 || entry.AlreadyText || entry.NoPicture || entry.LayoutChecked {
		return false
	}
	// Layout already ran. The page stays open only because the wired-table
	// model was missing, and applyRestoreLayout cleared LayoutChecked so the
	// HTML can be filled later. Recognizing it again cannot fill that HTML
	// until the model is ready.
	if entry.TablePending && !restoreTableModelReady() {
		return false
	}
	return true
}

func (a *App) applyRestoreLayout(entry *fileCompanionRestoreCachePage, view agent.ScanPageView, lines []agent.ScanTextLine) []agent.ScanTextLine {
	if entry == nil || !a.restoreLayoutReady() || len(view.Payload) == 0 {
		return lines
	}
	decoded, _, err := image.Decode(bytes.NewReader(view.Payload))
	if err != nil || decoded == nil {
		entry.LayoutChecked = true
		return a.recognizeLayoutFormulas(entry, nil, lines, nil)
	}
	regions, err := restoreRegions(decoded)
	if err != nil {
		// A missing or corrupt weight file stays unchecked so the next call
		// can run after the download replaces it. A model that is present
		// and still fails has nothing newer to wait for. The line check still
		// runs, because this call has no formula regions.
		lines = a.recognizeLayoutFormulas(entry, decoded, lines, nil)
		if fileCompanionRestoreLayout == nil && !doclayout.Ready() {
			return lines
		}
		entry.LayoutChecked = true
		return lines
	}
	lines = a.recognizeLayoutFormulas(entry, decoded, lines, regions)
	entry.LayoutChecked = true
	if len(regions) == 0 {
		return lines
	}
	place := view.Placement
	if place.PixelW < 1 || place.PixelH < 1 || place.DisplayW < 1 || place.DisplayH < 1 {
		return lines
	}
	tableWaiting := false
	blocks := make([]restoreBlock, 0, len(regions))
	for i, region := range regions {
		x, y, w, h, ok := agent.PlacePixelBox(place, region.Box.X0, region.Box.Y0, region.Box.Width(), region.Box.Height())
		if !ok {
			continue
		}
		block := restoreBlock{
			Order: region.Order,
			Label: region.Label,
			Score: region.Score,
			X:     x,
			Y:     y,
			W:     w,
			H:     h,
			Text:  blockText(lines, regions, i),
		}
		if region.Label == doclayout.TableLabel {
			if restoreTableModelReady() {
				block.HTML = restoreTableHTML(decoded, region.Box, lines)
			} else {
				tableWaiting = true
			}
		}
		blocks = append(blocks, block)
	}
	entry.Blocks = blocks
	entry.TablePending = tableWaiting
	if tableWaiting {
		entry.LayoutChecked = false
	}
	return reorderScanLines(lines, regions)
}

func (a *App) recognizeLayoutFormulas(entry *fileCompanionRestoreCachePage, img image.Image, lines []agent.ScanTextLine, regions []doclayout.Region) []agent.ScanTextLine {
	var boxes []doclayout.Box
	formulaRegions := make([]agent.FormulaRegion, 0, len(regions))
	for _, region := range regions {
		if !doclayout.FormulaLabel(region.Label) {
			continue
		}
		boxes = append(boxes, region.Box)
		formulaRegions = append(formulaRegions, agent.FormulaRegion{
			X0: region.Box.X0, Y0: region.Box.Y0, X1: region.Box.X1, Y1: region.Box.Y1,
		})
	}
	recognize := fileCompanionRestoreFormula
	if recognize == nil && fileCompanionRestoreRecognize == nil {
		if eng, ok := a.restoreFormulaEngine(); ok && eng != nil {
			recognize = eng.Recognize
		}
	}
	if recognize == nil {
		if entry != nil && a != nil && a.GetFormulaEnabled() && fileCompanionRestoreRecognize == nil && formulaStillPending(lines, boxes) {
			entry.FormulaPending = true
		}
		return lines
	}
	lines = agent.ApplyFormulaRegions(lines, img, recognize, formulaRegions)
	return agent.ApplyFormulaCrops(lines, img, recognize, func(line agent.ScanTextLine) bool {
		return !lineInsideBoxes(line, boxes) && agent.ScanLineLooksLikeFormula(line.Text)
	})
}

// formulaStillPending reports a formula the line check or a layout box has
// not turned into LaTeX. A formula box with no OCR line still counts: the
// region is the image FormulaNet has to see.
func formulaStillPending(lines []agent.ScanTextLine, boxes []doclayout.Box) bool {
	if len(boxes) > 0 {
		return true
	}
	for _, line := range lines {
		if !line.Formula && agent.ScanLineLooksLikeFormula(line.Text) {
			return true
		}
	}
	return false
}

func lineInsideBoxes(line agent.ScanTextLine, boxes []doclayout.Box) bool {
	cx := float64(line.BBox[0]) + float64(line.BBox[2])/2
	cy := float64(line.BBox[1]) + float64(line.BBox[3])/2
	for _, box := range boxes {
		if box.Contains(cx, cy) {
			return true
		}
	}
	return false
}

func restoreTableModelReady() bool {
	if fileCompanionRestoreTable != nil {
		return true
	}
	if fileCompanionTableReady != nil {
		return fileCompanionTableReady()
	}
	if fileCompanionRestoreRecognize != nil {
		return false
	}
	return tablestruct.Ready()
}

func restoreRegions(img image.Image) ([]doclayout.Region, error) {
	if fileCompanionRestoreLayout != nil {
		return fileCompanionRestoreLayout(img)
	}
	eng, err := doclayout.Shared()
	if err != nil || eng == nil {
		return nil, err
	}
	return eng.Analyze(img)
}

func restoreTableHTML(img image.Image, box doclayout.Box, lines []agent.ScanTextLine) string {
	crop, x0, y0 := cropLayoutBox(img, box)
	if crop == nil {
		return ""
	}
	var tableLines []tablestruct.Line
	for _, line := range lines {
		// A formula line is typeset in the formula rectangle. The grid keeps
		// the rules, and the cell does not also store the raw command.
		if strings.TrimSpace(line.Text) == "" || line.Formula {
			continue
		}
		cx := float64(line.BBox[0]) + float64(line.BBox[2])/2
		cy := float64(line.BBox[1]) + float64(line.BBox[3])/2
		if !box.Contains(cx, cy) {
			continue
		}
		tableLines = append(tableLines, tablestruct.Line{
			Text: line.Text,
			X:    line.BBox[0] - x0,
			Y:    line.BBox[1] - y0,
			W:    line.BBox[2],
			H:    line.BBox[3],
		})
	}
	var html string
	var err error
	if fileCompanionRestoreTable != nil {
		html, err = fileCompanionRestoreTable(crop, tableLines)
	} else if fileCompanionRestoreRecognize == nil && tablestruct.Ready() {
		eng, openErr := tablestruct.Shared()
		if openErr == nil && eng != nil {
			html, err = eng.HTML(crop, tableLines)
		}
	}
	if err != nil {
		return ""
	}
	return html
}

func cropLayoutBox(img image.Image, box doclayout.Box) (*image.RGBA, int, int) {
	bounds := img.Bounds()
	x0 := int(math.Floor(box.X0)) + bounds.Min.X
	y0 := int(math.Floor(box.Y0)) + bounds.Min.Y
	x1 := int(math.Ceil(box.X1)) + bounds.Min.X
	y1 := int(math.Ceil(box.Y1)) + bounds.Min.Y
	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}
	if x1-x0 < 2 || y1-y0 < 2 {
		return nil, 0, 0
	}
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), img, image.Point{X: x0, Y: y0}, draw.Src)
	return dst, x0 - bounds.Min.X, y0 - bounds.Min.Y
}

// blockText keeps the lines whose earliest containing region is regionIndex.
// Earliest means the smallest Order, with the input order breaking a tie.
// That is the same owner ReadingOrder uses, so a line inside a formula and
// the paragraph around it is stored once.
func blockText(lines []agent.ScanTextLine, regions []doclayout.Region, regionIndex int) string {
	if regionIndex < 0 || regionIndex >= len(regions) {
		return ""
	}
	order := make([]int, len(regions))
	for i := range regions {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return regions[order[i]].Order < regions[order[j]].Order
	})
	var parts []string
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		cx := float64(line.BBox[0]) + float64(line.BBox[2])/2
		cy := float64(line.BBox[1]) + float64(line.BBox[3])/2
		owner := -1
		for _, idx := range order {
			if regions[idx].Box.Contains(cx, cy) {
				owner = idx
				break
			}
		}
		if owner == regionIndex {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func reorderScanLines(lines []agent.ScanTextLine, regions []doclayout.Region) []agent.ScanTextLine {
	pix := make([]doclayout.PixelLine, len(lines))
	for i, line := range lines {
		pix[i] = doclayout.PixelLine{
			X: float64(line.BBox[0]), Y: float64(line.BBox[1]),
			W: float64(line.BBox[2]), H: float64(line.BBox[3]),
		}
	}
	idx := doclayout.ReadingOrder(pix, regions)
	out := make([]agent.ScanTextLine, len(idx))
	for i, j := range idx {
		out[i] = lines[j]
	}
	return out
}

func selectRestorePages(count int, requested []int, explicit bool, cache map[int]fileCompanionRestoreCachePage, formulaReady, layoutReady bool, failWave int) (todo []int, rest int) {
	var candidates []int
	if explicit {
		candidates = requested
	} else {
		for page := 1; page <= count; page++ {
			candidates = append(candidates, page)
		}
	}
	// Pages that have never been recognized go first. A refresh (formula model
	// arrived, layout never ran, or the wired-table model just arrived) must
	// not keep a later page out of the 16-page window. A page that is only
	// waiting for a table model that is still absent is not a refresh.
	// A page that already failed is not mixed into that work: retrying it
	// first would crowd out pages that have never been tried. It is retried
	// only when nothing else is left, so the next click tries it once.
	var fresh, again, retry []int
	for _, page := range candidates {
		entry, ok := cache[page]
		if !ok {
			fresh = append(fresh, page)
			continue
		}
		if pageRestoreFailed(entry) {
			// Only the oldest failure group is eligible, and only below this
			// click's wave. A page that fails during the click is stored at
			// the current wave, so it is not selected again until the next one.
			if failWave > 0 && entry.FailTries < failWave && (len(retry) == 0 || entry.FailTries <= cache[retry[0]].FailTries) {
				if len(retry) > 0 && entry.FailTries < cache[retry[0]].FailTries {
					retry = []int{page}
				} else {
					retry = append(retry, page)
				}
			}
			continue
		}
		if (entry.FormulaPending && formulaReady) || layoutRefresh(entry, layoutReady) {
			again = append(again, page)
		}
	}
	var ordered []int
	if explicit {
		for _, page := range candidates {
			entry, ok := cache[page]
			if ok && !pageRestoreFailed(entry) && !(entry.FormulaPending && formulaReady) && !layoutRefresh(entry, layoutReady) {
				continue
			}
			ordered = append(ordered, page)
		}
	} else if len(fresh)+len(again) > 0 {
		ordered = append(fresh, again...)
	} else {
		ordered = retry
	}
	if len(ordered) > fileCompanionRestorePagesMax {
		rest = len(ordered) - fileCompanionRestorePagesMax
		ordered = ordered[:fileCompanionRestorePagesMax]
	}
	return ordered, rest
}

func pageRestoreFailed(entry fileCompanionRestoreCachePage) bool {
	return entry.Failed != "" && len(entry.Lines) == 0 && !entry.AlreadyText && !entry.NoPicture
}

func nextFailWave(cache map[int]fileCompanionRestoreCachePage) int {
	maxTries := 0
	for _, entry := range cache {
		if entry.FailTries > maxTries {
			maxTries = entry.FailTries
		}
	}
	return maxTries + 1
}

func mergeRestoreCache(prev map[int]fileCompanionRestoreCachePage, fresh []fileCompanionRestoreCachePage) map[int]fileCompanionRestoreCachePage {
	out := make(map[int]fileCompanionRestoreCachePage, len(prev)+len(fresh))
	for page, entry := range prev {
		out[page] = entry
	}
	for _, entry := range fresh {
		out[entry.Page] = entry
	}
	return out
}

func restoreCacheAllText(cache map[int]fileCompanionRestoreCachePage, count int) bool {
	if count < 1 || len(cache) < count {
		return false
	}
	for page := 1; page <= count; page++ {
		entry, ok := cache[page]
		if !ok || !entry.AlreadyText {
			return false
		}
	}
	return true
}

func restoreHasLines(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, entry := range cache {
		for _, line := range entry.Lines {
			if strings.TrimSpace(line.Text) != "" {
				return true
			}
		}
	}
	return false
}

func fileCompanionRestoreNoLayerMessage(fresh []fileCompanionRestoreCachePage, rest int, failed []string) string {
	allText := len(fresh) > 0
	for _, entry := range fresh {
		if !entry.AlreadyText {
			allText = false
			break
		}
	}
	var b strings.Builder
	if allText {
		b.WriteString("这几页本来就有文字层，没有重新识别。")
	} else {
		b.WriteString("这几页没有识别出可写入文字层的内容，没有生成新的 PDF。")
	}
	if rest > 0 {
		fmt.Fprintf(&b, "后面还有 %d 页。请再次调用 pdf_restore 继续。", rest)
	}
	if len(failed) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(failed, "\n"))
	}
	return b.String()
}

func restoreLinesByPage(cache map[int]fileCompanionRestoreCachePage) map[int][]agent.PlacedScanLine {
	out := make(map[int][]agent.PlacedScanLine, len(cache))
	for page, entry := range cache {
		if len(entry.Lines) > 0 {
			out[page] = entry.Lines
		}
	}
	return out
}

func pictorialRestoreLabel(label string) bool {
	switch label {
	case "image", "chart", "seal", "header_image", "footer_image":
		return true
	default:
		return false
	}
}

// restorePageAssets crops pictorial layout boxes out of the scan and turns
// wired-table HTML into a grid. A recognized formula is the only text block
// copied onto the page: its LaTeX is typeset in the formula rectangle, and
// the lines inside that rectangle are not drawn again. Prose stays on each
// OCR line's own rectangle. A turned or mirrored figure cannot be cropped,
// so that page keeps its photo. A box that is only too small does not.
func restorePageAssets(doc *agent.ScanRestore, cache map[int]fileCompanionRestoreCachePage) map[int]agent.RestoredPage {
	if doc == nil {
		return nil
	}
	var out map[int]agent.RestoredPage
	for page, entry := range cache {
		var asset agent.RestoredPage
		view, viewErr := doc.Page(page)
		if viewErr == nil {
			asset.Pictures, asset.HoldScan = pictorialCrops(view.Placement, view.Payload, entry.Blocks)
		}
		for _, block := range entry.Blocks {
			if block.Label != doclayout.TableLabel || block.W < 1 || block.H < 1 {
				continue
			}
			cells := agent.ParseRestoredTableHTML(block.HTML)
			if len(cells) == 0 {
				continue
			}
			asset.Tables = append(asset.Tables, agent.RestoredTable{
				X: block.X, Y: block.Y, W: block.W, H: block.H, Cells: cells,
			})
		}
		for _, block := range entry.Blocks {
			if !blockIsRecognizedFormula(block, entry.Lines) || block.W < 1 || block.H < 1 {
				continue
			}
			asset.Layout = append(asset.Layout, agent.RestoredLayout{
				X: block.X, Y: block.Y, W: block.W, H: block.H,
				Text: strings.TrimSpace(block.Text), Formula: true,
			})
		}
		if len(asset.Pictures) > 0 || len(asset.Tables) > 0 || len(asset.Layout) > 0 || asset.HoldScan {
			if out == nil {
				out = make(map[int]agent.RestoredPage)
			}
			out[page] = asset
		}
	}
	return out
}

// pictorialCrops returns the figures that can be cut out of an upright scan.
// Hold is set only when the placement is turned or mirrored: the crop would
// not land on the ink, so the page photo stays and no partial crop is drawn.
func pictorialCrops(place agent.ScanImagePlacement, payload []byte, blocks []restoreBlock) ([]agent.RestoredPicture, bool) {
	var pics []agent.RestoredPicture
	wanted := 0
	uncroppable := place.Turn != 0 || place.MirrorX || place.MirrorY
	for _, block := range blocks {
		if !pictorialRestoreLabel(block.Label) || block.W < 1 || block.H < 1 {
			continue
		}
		wanted++
		if uncroppable || len(payload) == 0 {
			continue
		}
		cropped, ok := agent.CropScanRegion(payload, place, block.X, block.Y, block.W, block.H)
		if !ok {
			continue
		}
		pics = append(pics, agent.RestoredPicture{
			Payload: cropped, X: block.X, Y: block.Y, W: block.W, H: block.H,
		})
	}
	if wanted > 0 && uncroppable && len(payload) > 0 {
		return nil, true
	}
	return pics, false
}

func blockIsRecognizedFormula(block restoreBlock, lines []agent.PlacedScanLine) bool {
	if !doclayout.FormulaLabel(block.Label) {
		return false
	}
	text := strings.TrimSpace(block.Text)
	if text == "" {
		return false
	}
	for _, line := range lines {
		if line.Formula && strings.TrimSpace(line.Text) == text {
			return true
		}
	}
	return false
}

func countFormulaLines(pages []fileCompanionRestoreCachePage) int {
	n := 0
	for _, page := range pages {
		for _, line := range page.Lines {
			if line.Formula {
				n++
			}
		}
	}
	return n
}

func cacheHasFormulaPending(pages []fileCompanionRestoreCachePage) bool {
	for _, page := range pages {
		if page.FormulaPending {
			return true
		}
	}
	return false
}

func cacheMapHasFormulaPending(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, page := range cache {
		if page.FormulaPending {
			return true
		}
	}
	return false
}

func cacheHasTablePending(pages []fileCompanionRestoreCachePage) bool {
	for _, page := range pages {
		if page.TablePending {
			return true
		}
	}
	return false
}

func countLayoutBlocks(pages []fileCompanionRestoreCachePage) int {
	n := 0
	for _, page := range pages {
		n += len(page.Blocks)
	}
	return n
}

func countTableHTML(pages []fileCompanionRestoreCachePage) int {
	n := 0
	for _, page := range pages {
		for _, block := range page.Blocks {
			if strings.TrimSpace(block.HTML) != "" {
				n++
			}
		}
	}
	return n
}

type restoreMessage struct {
	done           []fileCompanionRestoreCachePage
	pdfPath        string
	count          int
	rest           int
	failed         []string
	formulaLines   int
	formulaPending bool
	layoutBlocks   int
	tableHTML      int
	layoutPending  bool
	tablePending   bool
}

func fileCompanionRestoreMessage(s restoreMessage) string {
	var b strings.Builder
	b.WriteString("已按原版式生成新的 PDF，原文件没有改动。")
	if pages := restoreDonePages(s.done); pages != "" {
		b.WriteString("这次处理了")
		b.WriteString(pages)
		b.WriteString("。")
	}
	if s.formulaLines > 0 {
		b.WriteString("公式已用已接入的公式模型还原到原位置。")
	}
	if s.formulaPending {
		b.WriteString(formulaPendingNote)
	}
	if s.layoutBlocks > 0 {
		b.WriteString("版面已按原来的位置还原。")
	}
	if note := layoutDownloadNote(s.layoutPending); note != "" {
		b.WriteString(note)
	}
	if s.tableHTML > 0 {
		b.WriteString("表格已恢复成结构。")
	}
	if s.tablePending {
		b.WriteString(tablePendingNote)
	}
	if s.rest > 0 {
		fmt.Fprintf(&b, "后面还有 %d 页。请再次调用 pdf_restore 继续。", s.rest)
	}
	if s.pdfPath != "" {
		b.WriteString("结果文件：")
		b.WriteString(s.pdfPath)
	}
	if len(s.failed) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(s.failed, "\n"))
	}
	return b.String()
}

func restoreDonePages(pages []fileCompanionRestoreCachePage) string {
	if len(pages) == 0 {
		return ""
	}
	nums := make([]int, 0, len(pages))
	for _, page := range pages {
		nums = append(nums, page.Page)
	}
	sort.Ints(nums)
	return "第 " + fileCompanionJoinPages(nums) + " 页"
}

func fileCompanionOptionalPages(args map[string]interface{}) ([]int, bool, error) {
	if args == nil {
		return nil, false, nil
	}
	raw, ok := args["pages"]
	if !ok || raw == nil {
		return nil, false, nil
	}
	if text, isString := raw.(string); isString && strings.TrimSpace(text) == "" {
		return nil, false, nil
	}
	spec, err := fileCompanionPagesArg(args)
	if err != nil {
		return nil, true, err
	}
	pages, err := fileCompanionParsePageSpec(spec)
	if err != nil {
		return nil, true, err
	}
	return pages, true, nil
}

func (a *App) fileCompanionRestorePaths(canonical string) (pdfPath, cachePath string, err error) {
	if a == nil {
		return "", "", errors.New("app unavailable")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", fileCompanionSessionID(canonical))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "searchable.pdf"), filepath.Join(dir, "searchable.json"), nil
}

// loadFileCompanionRestoreCache returns the page cache only when its size and
// mtime are the source file's. A rejected cache is not this document, so the
// searchable PDF built from it is removed. A missing cache file keeps that
// PDF: the write can succeed before the progress save.
func (a *App) loadFileCompanionRestoreCache(canonical string, size, mtime int64) (map[int]fileCompanionRestoreCachePage, string, string, int64, int, error) {
	pdfPath, cachePath, err := a.fileCompanionRestorePaths(canonical)
	if err != nil {
		return nil, "", "", 0, 0, err
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int]fileCompanionRestoreCachePage{}, cachePath, pdfPath, 0, 0, nil
		}
		return nil, "", "", 0, 0, err
	}
	var file fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &file); err != nil || file.Size != size || file.Mtime != mtime {
		discardSearchablePDF(pdfPath)
		return map[int]fileCompanionRestoreCachePage{}, cachePath, pdfPath, 0, 0, nil
	}
	out := make(map[int]fileCompanionRestoreCachePage, len(file.Pages))
	for _, page := range file.Pages {
		if page.Page > 0 {
			out[page.Page] = page
		}
	}
	return out, cachePath, pdfPath, file.PDFSize, file.Count, nil
}

// releaseSearchablePDFIfSourceDiffers removes searchable.pdf when the cache
// belongs to a different size or mtime. A missing cache, or one that still
// matches this file, is left alone. The caller can stat the file even when
// its bytes cannot be read.
func (a *App) releaseSearchablePDFIfSourceDiffers(canonical string) bool {
	if a == nil || canonical == "" {
		return false
	}
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return false
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", fileCompanionSessionID(canonical))
	raw, err := os.ReadFile(filepath.Join(dir, "searchable.json"))
	if err != nil {
		return false
	}
	var file fileCompanionRestoreCache
	if json.Unmarshal(raw, &file) == nil && file.Size == info.Size() && file.Mtime == info.ModTime().UnixNano() {
		return false
	}
	discardSearchablePDF(filepath.Join(dir, "searchable.pdf"))
	return true
}

// unreadableScanProbe is the scan decision when the bytes could not be
// classified. A different file loses the old PDF and stays convertible once
// it can be read. The same file keeps the error so the window leaves its
// card in place.
func (a *App) unreadableScanProbe(canonical string, cause error) (FileCompanionPDFScan, error) {
	if cause == nil {
		cause = errors.New("无法读取这份 PDF 的页数。")
	}
	if a.releaseSearchablePDFIfSourceDiffers(canonical) {
		return FileCompanionPDFScan{Scan: true}, nil
	}
	return FileCompanionPDFScan{}, cause
}

func (a *App) saveFileCompanionRestoreCache(cachePath string, size, mtime, pdfSize int64, count int, pages map[int]fileCompanionRestoreCachePage) error {
	file := fileCompanionRestoreCache{Size: size, Mtime: mtime, PDFSize: pdfSize, Count: count, Pages: make([]fileCompanionRestoreCachePage, 0, len(pages))}
	for _, page := range pages {
		file.Pages = append(file.Pages, page)
	}
	sort.Slice(file.Pages, func(i, j int) bool { return file.Pages[i].Page < file.Pages[j].Page })
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return writeCompanionBytesAtomic(cachePath, raw)
}

// FileCompanionPDFScan reports whether the open PDF needs conversion. A page
// with extractable text and no page-covering image is digital. A scan that
// also has a page number still needs conversion. It does not run OCR. Path
// is set only when searchable.pdf belongs to this source's cache and matches
// the recorded size.
type FileCompanionPDFScan struct {
	Scan bool   `json:"scan"`
	Path string `json:"path,omitempty"`
}

// FileCompanionConvertResult is one layout-preserving conversion of the open
// scan. Path is the new selectable PDF. The open file is not modified.
// Complete means every page is cached and this pass has nothing left to
// recognize. Pending means a formula, layout, or table model is still missing,
// so the message tells the user to convert again after the download.
// Started means this value is only the acknowledgement: the file is converting
// on its own goroutine. Busy means that goroutine is already running. A cache
// that already matches this file, with nothing left to recognize, returns the
// finished value directly and does not emit file-companion:convert-done.
// Every other finished value is that event, which the window turns into the
// completion notice.
type FileCompanionConvertResult struct {
	OK         bool   `json:"ok"`
	Scan       bool   `json:"scan"`
	Complete   bool   `json:"complete"`
	Pending    bool   `json:"pending"`
	Started    bool   `json:"started"`
	Busy       bool   `json:"busy"`
	Path       string `json:"path,omitempty"`
	Message    string `json:"message"`
	SaveFailed bool   `json:"save_failed,omitempty"`
}

func (a *App) FileCompanionPDFScan(path string) (FileCompanionPDFScan, error) {
	if a == nil {
		return FileCompanionPDFScan{}, errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionPDFScan{}, err
	}
	if !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return FileCompanionPDFScan{}, nil
	}
	data, readErr := fileCompanionReadPDFBytes(canonical)
	if readErr != nil {
		return a.unreadableScanProbe(canonical, readErr)
	}
	if agent.PDFIsEncrypted(data) {
		_ = a.currentSearchablePDF(canonical)
		return FileCompanionPDFScan{}, nil
	}
	doc := agent.NewScanRestore(data)
	count, countErr := doc.PageCount()
	if countErr != nil || count < 1 {
		return a.unreadableScanProbe(canonical, countErr)
	}
	if doc.EveryPageIsDigital(count) {
		// The cache is dropped when it belongs to a different size or mtime.
		// Returning before that check kept the previous conversion's PDF after
		// the source was replaced with a text PDF.
		_ = a.currentSearchablePDF(canonical)
		return FileCompanionPDFScan{}, nil
	}
	out := FileCompanionPDFScan{Scan: true}
	if ready := a.currentSearchablePDF(canonical); ready != "" {
		out.Path = ready
		if chosen := readConvertDestination(ready); chosen != "" {
			out.Path = chosen
		}
	}
	// The open already counted the pages. Remember that count so a later click
	// can see a finished cache without reading the scan again.
	a.noteRestorePageCount(canonical, count)
	return out, nil
}

// fileCompanionConvertSaveDialog is the save dialog for a converted PDF.
// Tests pick a path without opening a window. An empty path is cancel.
var fileCompanionConvertSaveDialog = func(a *App, defaultDir, defaultName string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "保存转换后的 PDF",
		DefaultDirectory: defaultDir,
		DefaultFilename:  defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF (*.pdf)", Pattern: "*.pdf"},
		},
	})
}

func fileCompanionConvertFileName(source string) string {
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, fileCompanionPaperSourceName(source))
	base = strings.Trim(base, " .")
	rs := []rune(base)
	if len(rs) > 60 {
		base = strings.TrimRight(string(rs[:60]), " .")
	}
	if base == "" {
		base = "document"
	}
	return base + "-可选中.pdf"
}

// cleanConvertDestination is the path the reader picked. An empty path is
// cancel. The open scan is never the destination.
func cleanConvertDestination(source, dest string) (string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(dest), ".pdf") {
		dest += ".pdf"
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	if err := convertDestinationWritable(dest); err != nil {
		return "", err
	}
	return dest, nil
}

// convertDestinationWritable refuses a folder the conversion cannot write.
// The check runs before recognition. It does not create or truncate dest.
func convertDestinationWritable(dest string) error {
	parent := filepath.Dir(dest)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return errors.New("无法保存到该位置")
	}
	info, err := os.Stat(dest)
	if err == nil {
		if info.IsDir() {
			return errors.New("无法保存到该位置")
		}
		file, openErr := os.OpenFile(dest, os.O_WRONLY, 0)
		if openErr != nil {
			return errors.New("无法保存到该位置")
		}
		_ = file.Close()
		return nil
	}
	if !os.IsNotExist(err) {
		return errors.New("无法保存到该位置")
	}
	file, err := os.CreateTemp(parent, ".maclaw-*")
	if err != nil {
		return errors.New("无法保存到该位置")
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return nil
}

// FileCompanionChooseConvertDestination asks where the converted PDF should
// be saved. An empty result is cancel, which is not an error.
func (a *App) FileCompanionChooseConvertDestination(path string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return "", errors.New("只有扫描版 PDF 可以转换。")
	}
	dir, name := suggestedConvertDialog(a.convertSessionPDF(canonical), filepath.Dir(canonical), fileCompanionConvertFileName(canonical))
	picked, err := fileCompanionConvertSaveDialog(a, dir, name)
	if err != nil {
		return "", err
	}
	return cleanConvertDestination(canonical, picked)
}

// FileCompanionConvertScan starts the searchable-PDF pipeline for every page of
// the open scan and returns before that work finishes. One click keeps going
// past the 16-page tool window. Layout, formula crops, and wired-table HTML
// run when those models are ready. A missing model still writes the phase-1
// PDF and says to convert again after the download. The open file is not
// modified. dest is the path the reader picked. The session searchable.pdf
// stays the cache, and a copy is written to dest when this call produces a
// PDF. An empty dest keeps the session path. When the cache already belongs
// to this file and nothing remains to recognize, the call copies that PDF and
// does not start the pipeline. Otherwise the finished result is
// file-companion:convert-done.
func (a *App) FileCompanionConvertScan(path, dest string) (FileCompanionConvertResult, error) {
	if a == nil {
		return FileCompanionConvertResult{}, errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionConvertResult{}, err
	}
	if !strings.EqualFold(filepath.Ext(canonical), ".pdf") {
		return FileCompanionConvertResult{Message: "只有扫描版 PDF 可以转换。"}, nil
	}
	if strings.TrimSpace(dest) != "" {
		dest, err = cleanConvertDestination(canonical, dest)
		if err != nil {
			return FileCompanionConvertResult{}, err
		}
	}
	unlock, locked := lockCompanionConvert(canonical)
	if !locked {
		return FileCompanionConvertResult{Scan: true, Busy: true, Message: "正在转换，请稍候。"}, nil
	}
	// The lock stays held across the cache check. Releasing it before the
	// goroutine starts would let a second click both locate the file and start
	// another conversion.
	held := true
	defer func() {
		if held {
			unlock()
		}
	}()
	if ready, ok := a.finishedCompanionConvert(canonical); ok {
		return a.deliverConvertedPDF(canonical, ready, dest), nil
	}
	held = false
	a.launchCompanionConvert(path, canonical, dest, unlock)
	return FileCompanionConvertResult{OK: true, Scan: true, Started: true, Message: "正在后台转换，完成后会提示。"}, nil
}

// finishedCompanionConvert is the searchable PDF this source already has when
// every page is cached and nothing can still be recognized. The caller holds
// the convert lock. A cache written before the page count was stored is not
// finished: the count is what distinguishes a partial file from a complete one.
func (a *App) finishedCompanionConvert(canonical string) (FileCompanionConvertResult, bool) {
	view, ok := a.restoreFinishedView(canonical)
	if !ok || a.restoreWorkRemains(view.cache, view.count) {
		return FileCompanionConvertResult{}, false
	}
	msg, complete, pending := a.convertMessageFromCache(view.cache, view.count, view.pdfPath, nil, 0)
	if !complete || pending {
		return FileCompanionConvertResult{}, false
	}
	again, still := a.restoreFinishedView(canonical)
	if !still || again.key != view.key || again.pdfPath != view.pdfPath || again.count != view.count {
		return FileCompanionConvertResult{}, false
	}
	return FileCompanionConvertResult{
		OK: true, Scan: true, Complete: true, Pending: false,
		Path: view.pdfPath, Message: msg,
	}, true
}

type restoreFinishedView struct {
	key     string
	cache   map[int]fileCompanionRestoreCachePage
	pdfPath string
	count   int
}

func (a *App) restoreFinishedView(canonical string) (restoreFinishedView, bool) {
	if a == nil || canonical == "" {
		return restoreFinishedView{}, false
	}
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return restoreFinishedView{}, false
	}
	cache, _, pdfPath, pdfSize, count, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil || count < 1 || pdfSize <= 0 || pdfPath == "" || !restoreHasLines(cache) || restoreCacheAllText(cache, count) {
		return restoreFinishedView{}, false
	}
	st, err := os.Stat(pdfPath)
	if err != nil || st.IsDir() || st.Size() != pdfSize {
		return restoreFinishedView{}, false
	}
	return restoreFinishedView{key: restoreCacheKey(cache), cache: cache, pdfPath: pdfPath, count: count}, true
}

func (a *App) restoreWorkRemains(cache map[int]fileCompanionRestoreCachePage, count int) bool {
	formulaReady := false
	if cacheMapHasFormulaPending(cache) {
		_, formulaReady = a.restoreFormulaEngine()
	}
	layoutReady := false
	if cacheCouldRefreshLayout(cache) {
		layoutReady = a.restoreLayoutReady()
	}
	todo, rest := selectRestorePages(count, nil, false, cache, formulaReady, layoutReady, nextFailWave(cache))
	return len(todo) > 0 || rest > 0
}

func cacheCouldRefreshLayout(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, entry := range cache {
		if len(entry.Lines) == 0 || entry.AlreadyText || entry.NoPicture || entry.LayoutChecked {
			continue
		}
		// The same gate as layoutRefresh: a page that is only waiting for a
		// table model that is still absent is not work this click can do.
		if entry.TablePending && !restoreTableModelReady() {
			continue
		}
		return true
	}
	return false
}

// noteRestorePageCount stores the source page count on a cache that does not
// have it yet. The PDF bytes stay where they are. A later click uses the count
// to tell a finished conversion from one that still has pages left.
func (a *App) noteRestorePageCount(canonical string, count int) {
	if a == nil || canonical == "" || count < 1 {
		return
	}
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return
	}
	cache, cachePath, _, pdfSize, stored, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil || len(cache) == 0 || stored == count {
		return
	}
	_ = a.saveFileCompanionRestoreCache(cachePath, info.Size(), info.ModTime().UnixNano(), pdfSize, count, cache)
}

// companionConvertRun is one background conversion. finish delivers the result
// to every waiter, including a waiter that arrives after the work has ended.
type companionConvertRun struct {
	mu       sync.Mutex
	result   FileCompanionConvertResult
	finished bool
	waiters  []chan FileCompanionConvertResult
}

func (r *companionConvertRun) finish(result FileCompanionConvertResult) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	r.result = result
	for _, waiter := range r.waiters {
		waiter <- result
	}
	r.waiters = nil
}

func (r *companionConvertRun) waitFor(timeout time.Duration) (FileCompanionConvertResult, bool) {
	if r == nil {
		return FileCompanionConvertResult{}, false
	}
	r.mu.Lock()
	if r.finished {
		result := r.result
		r.mu.Unlock()
		return result, true
	}
	waiter := make(chan FileCompanionConvertResult, 1)
	r.waiters = append(r.waiters, waiter)
	r.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-waiter:
		return result, true
	case <-timer.C:
		return FileCompanionConvertResult{}, false
	}
}

// launchCompanionConvert runs the pipeline on the caller's convert lock.
// The goroutine releases that lock before it delivers the result.
func (a *App) launchCompanionConvert(caller, canonical, dest string, unlock func()) {
	run := &companionConvertRun{}
	gate := companionRestoreGateFor(canonical)
	gate.mu.Lock()
	gate.run = run
	gate.mu.Unlock()
	go func() {
		// Release the convert lock before waking waiters. A waiter that goes
		// on to start the next click must not observe the finished run as busy.
		unlocked := false
		defer func() {
			if !unlocked {
				unlock()
			}
		}()
		result := FileCompanionConvertResult{Scan: true, Message: "转换中断，请再点「转换」。"}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[companion-convert] %s: %v", canonical, rec)
				}
			}()
			result = a.convertScannedCompanionPDF(caller, canonical)
			result = a.deliverConvertedPDF(canonical, result, dest)
		}()
		unlock()
		unlocked = true
		a.deliverCompanionConvert(caller, run, result)
	}()
}

func (a *App) deliverCompanionConvert(caller string, run *companionConvertRun, result FileCompanionConvertResult) {
	run.finish(result)
	a.emitEvent("file-companion:convert-done", companionConvertNotice(caller, result))
}

// companionConvertNotice is the completion payload. path is the source the
// window asked to convert. pdf is the selectable file, when one was written.
func companionConvertNotice(source string, result FileCompanionConvertResult) map[string]interface{} {
	return map[string]interface{}{
		"path":        source,
		"ok":          result.OK,
		"scan":        result.Scan,
		"complete":    result.Complete,
		"pending":     result.Pending,
		"pdf":         result.Path,
		"message":     result.Message,
		"save_failed": result.SaveFailed,
	}
}

// waitFileCompanionConvert blocks until the background conversion of path
// finishes. Tests use it. The window waits on file-companion:convert-done.
func (a *App) waitFileCompanionConvert(path string, timeout time.Duration) (FileCompanionConvertResult, bool) {
	if a == nil {
		return FileCompanionConvertResult{}, false
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionConvertResult{Message: err.Error()}, false
	}
	gate := companionRestoreGateFor(canonical)
	gate.mu.Lock()
	run := gate.run
	gate.mu.Unlock()
	return run.waitFor(timeout)
}

// companionRestoreGate keeps one convert click from overlapping itself, and
// keeps a chat pdf_restore from writing the same cache during a batch.
// busy is not held inside the batch: the batch takes work, and Go mutexes
// are not reentrant.
type companionRestoreGate struct {
	busy sync.Mutex
	work sync.Mutex
	mu   sync.Mutex
	run  *companionConvertRun
}

var companionRestoreGates sync.Map

func companionRestoreGateFor(canonical string) *companionRestoreGate {
	value, _ := companionRestoreGates.LoadOrStore(canonical, &companionRestoreGate{})
	return value.(*companionRestoreGate)
}

func lockCompanionConvert(canonical string) (func(), bool) {
	gate := companionRestoreGateFor(canonical)
	if !gate.busy.TryLock() {
		return func() {}, false
	}
	return gate.busy.Unlock, true
}

func lockCompanionRestoreWork(canonical string) func() {
	gate := companionRestoreGateFor(canonical)
	gate.work.Lock()
	return gate.work.Unlock
}

// convertScannedCompanionPDF recognizes every page of one click. The caller
// holds that file's convert lock until this returns.
func (a *App) convertScannedCompanionPDF(caller, canonical string) FileCompanionConvertResult {
	data, err := fileCompanionReadPDFBytes(canonical)
	if err != nil {
		a.releaseSearchablePDFIfSourceDiffers(canonical)
		return FileCompanionConvertResult{Scan: true, Message: err.Error()}
	}
	if agent.PDFIsEncrypted(data) {
		_ = a.currentSearchablePDF(canonical)
		return FileCompanionConvertResult{Scan: false, Message: fileCompanionRestorePassword}
	}
	info, err := os.Stat(canonical)
	if err != nil {
		a.releaseSearchablePDFIfSourceDiffers(canonical)
		return FileCompanionConvertResult{Scan: true, Message: "读取文档失败：" + err.Error()}
	}
	doc := agent.NewScanRestore(data)
	doc.BindSource(info.Size(), info.ModTime().UnixNano())
	count, countErr := doc.PageCount()
	if countErr != nil || count < 1 {
		a.releaseSearchablePDFIfSourceDiffers(canonical)
		return FileCompanionConvertResult{Scan: true, Message: "无法读取这份 PDF 的页数。"}
	}
	a.noteRestorePageCount(canonical, count)
	var failed []string
	seenFail := map[string]struct{}{}
	prevKey := ""
	haveKey := false
	var last restoreCall
	failWave := 0
	limit := count*3 + 4
	for step := 0; step < limit; step++ {
		call := a.restoreCompanionPDF(canonical, nil, failWave, false, doc)
		if failWave == 0 && call.wave > 0 {
			failWave = call.wave
		}
		last = call
		for _, line := range call.failed {
			if _, ok := seenFail[line]; ok {
				continue
			}
			seenFail[line] = struct{}{}
			failed = append(failed, line)
		}
		key, cache := a.companionRestoreSnapshot(canonical)
		a.emitEvent("file-companion:convert-progress", map[string]interface{}{
			"path":  caller,
			"done":  len(cache),
			"total": count,
		})
		if call.alreadyText {
			return FileCompanionConvertResult{OK: true, Scan: false, Message: call.text}
		}
		pdfPath := call.pdfPath
		if !call.ok {
			// This batch failed before it could record pages. Pages saved by
			// an earlier batch of this click are still drawn into the PDF.
			if pdfPath == "" {
				published, pubErr := a.publishCachedRestorePDF(canonical, doc)
				if published == "" && pubErr != nil {
					return FileCompanionConvertResult{Scan: true, Message: restorePublishError(pubErr)}
				}
				pdfPath = published
			}
			if pdfPath != "" {
				msg, _, pending := a.convertMessageFromCache(cache, count, pdfPath, failed, call.rest)
				msg = appendRestoreNote(msg, convertRestoreWording(call.text))
				return FileCompanionConvertResult{
					OK: true, Scan: true, Complete: false, Pending: pending,
					Path: pdfPath, Message: msg,
				}
			}
			return FileCompanionConvertResult{Scan: true, Message: convertRestoreWording(call.text)}
		}
		stalled := haveKey && key == prevKey
		if call.rest == 0 || stalled {
			if pdfPath == "" {
				published, pubErr := a.publishCachedRestorePDF(canonical, doc)
				if published == "" && pubErr != nil {
					return FileCompanionConvertResult{Scan: true, Message: restorePublishError(pubErr)}
				}
				pdfPath = published
			}
			if pdfPath != "" {
				msg, complete, pending := a.convertMessageFromCache(cache, count, pdfPath, failed, call.rest)
				return FileCompanionConvertResult{
					OK: true, Scan: true, Complete: complete, Pending: pending,
					Path: pdfPath, Message: msg,
				}
			}
			return FileCompanionConvertResult{Scan: true, Message: convertRestoreWording(call.text)}
		}
		prevKey = key
		haveKey = true
	}
	if pdfPath, pubErr := a.publishCachedRestorePDF(canonical, doc); pdfPath != "" {
		_, cache := a.companionRestoreSnapshot(canonical)
		msg, _, pending := a.convertMessageFromCache(cache, count, pdfPath, failed, last.rest)
		return FileCompanionConvertResult{
			OK: true, Scan: true, Complete: false, Pending: pending,
			Path: pdfPath, Message: msg,
		}
	} else if pubErr != nil {
		return FileCompanionConvertResult{Scan: true, Message: restorePublishError(pubErr)}
	}
	return FileCompanionConvertResult{Scan: true, Message: convertRestoreWording(last.text)}
}

func (a *App) convertMessageFromCache(cache map[int]fileCompanionRestoreCachePage, count int, pdfPath string, failed []string, rest int) (string, bool, bool) {
	missing := 0
	for page := 1; page <= count; page++ {
		if _, ok := cache[page]; !ok {
			missing++
		}
	}
	cachedFailed := cacheFailedLines(cache)
	complete := missing == 0 && rest == 0 && pdfPath != "" && len(cachedFailed) == 0
	var b strings.Builder
	if complete {
		fmt.Fprintf(&b, "已按原版式生成新的 PDF，原文件没有改动。共 %d 页。", count)
	} else {
		b.WriteString("已按原版式生成新的 PDF，原文件没有改动。")
		if missing > 0 {
			fmt.Fprintf(&b, "后面还有 %d 页。请再点「转换」继续。", missing)
		} else if rest > 0 || len(cachedFailed) > 0 {
			b.WriteString("请再点「转换」继续。")
		}
	}
	if cacheHasFormulaLine(cache) {
		b.WriteString("公式已用已接入的公式模型还原到原位置。")
	}
	formulaReady := false
	if cacheMapHasFormulaPending(cache) {
		_, formulaReady = a.restoreFormulaEngine()
	}
	formulaNote := convertRestoreWording(formulaPendingSuffix(cache, formulaReady))
	if formulaNote != "" {
		b.WriteString(formulaNote)
	}
	if cacheHasLayoutBlock(cache) {
		b.WriteString("版面已按原来的位置还原。")
	}
	layoutNote := convertRestoreWording(layoutDownloadNote(a.restoreLayoutPending()))
	if layoutNote != "" {
		b.WriteString(layoutNote)
	}
	if cacheHasTableHTML(cache) {
		b.WriteString("表格已恢复成结构。")
	}
	tableNote := convertRestoreWording(tablePendingSuffix(cache))
	if tableNote != "" {
		b.WriteString(tableNote)
	}
	pending := formulaNote != "" || layoutNote != "" || tableNote != ""
	for _, line := range mergeFailedLines(failed, cachedFailed) {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	if pdfPath != "" {
		b.WriteString("结果文件：")
		b.WriteString(pdfPath)
	}
	return b.String(), complete, pending
}

func cacheHasFormulaLine(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, page := range cache {
		for _, line := range page.Lines {
			if line.Formula {
				return true
			}
		}
	}
	return false
}

func cacheHasLayoutBlock(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, page := range cache {
		if len(page.Blocks) > 0 {
			return true
		}
	}
	return false
}

func cacheHasTableHTML(cache map[int]fileCompanionRestoreCachePage) bool {
	for _, page := range cache {
		for _, block := range page.Blocks {
			if strings.TrimSpace(block.HTML) != "" {
				return true
			}
		}
	}
	return false
}

func convertRestoreWording(msg string) string {
	msg = strings.ReplaceAll(msg, "请再次调用 pdf_restore 继续。", "请再点「转换」继续。")
	msg = strings.ReplaceAll(msg, "下载完成后再调用 pdf_restore", "下载完成后再点「转换」")
	return msg
}

func appendRestoreNote(msg, note string) string {
	note = strings.TrimSpace(note)
	if note == "" || strings.Contains(msg, note) {
		return msg
	}
	if strings.TrimSpace(msg) == "" {
		return note
	}
	return msg + "\n" + note
}

func cacheFailedLines(cache map[int]fileCompanionRestoreCachePage) []string {
	if len(cache) == 0 {
		return nil
	}
	nums := make([]int, 0)
	for page, entry := range cache {
		if pageRestoreFailed(entry) {
			nums = append(nums, page)
		}
	}
	sort.Ints(nums)
	out := make([]string, 0, len(nums))
	for _, page := range nums {
		out = append(out, fmt.Sprintf("第 %d 页识别失败：%s", page, cache[page].Failed))
	}
	return out
}

func mergeFailedLines(batches, cached []string) []string {
	seen := make(map[string]struct{}, len(batches)+len(cached))
	out := make([]string, 0, len(batches)+len(cached))
	for _, group := range [][]string{batches, cached} {
		for _, line := range group {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			out = append(out, line)
		}
	}
	return out
}

func convertDestSidecar(sessionPDF string) string {
	return strings.TrimSuffix(sessionPDF, filepath.Ext(sessionPDF)) + ".dest"
}

func (a *App) convertSessionPDF(canonical string) string {
	if a == nil || canonical == "" {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "results", fileCompanionSessionID(canonical), "searchable.pdf")
}

// convertDestRecord is the last copy written for this session PDF.
// Size and Mtime are that session file at the moment the copy succeeded.
type convertDestRecord struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

func loadConvertDestRecord(sessionPDF string) (convertDestRecord, bool) {
	raw, err := os.ReadFile(convertDestSidecar(sessionPDF))
	if err != nil {
		return convertDestRecord{}, false
	}
	var rec convertDestRecord
	if json.Unmarshal(raw, &rec) != nil {
		return convertDestRecord{}, false
	}
	rec.Path = strings.TrimSpace(rec.Path)
	if rec.Path == "" || rec.Size <= 0 {
		return convertDestRecord{}, false
	}
	return rec, true
}

// readConvertDestination is the reader's copy while it is still the current
// session PDF. A newer session file, or a replaced copy, stays off this path.
func readConvertDestination(sessionPDF string) string {
	rec, ok := loadConvertDestRecord(sessionPDF)
	if !ok {
		return ""
	}
	session, err := os.Stat(sessionPDF)
	if err != nil || session.IsDir() || session.Size() != rec.Size || session.ModTime().UnixNano() != rec.Mtime {
		return ""
	}
	dest, err := os.Stat(rec.Path)
	if err != nil || dest.IsDir() || dest.Size() != rec.Size {
		return ""
	}
	return rec.Path
}

// suggestedConvertDialog opens the next save dialog on the path the reader
// last chose. A missing folder falls back to the source file's folder.
func suggestedConvertDialog(sessionPDF, sourceDir, sourceName string) (string, string) {
	rec, ok := loadConvertDestRecord(sessionPDF)
	if !ok {
		return sourceDir, sourceName
	}
	dir := filepath.Dir(rec.Path)
	name := filepath.Base(rec.Path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || name == "" || name == "." || !strings.EqualFold(filepath.Ext(name), ".pdf") {
		return sourceDir, sourceName
	}
	return dir, name
}

// deliverConvertedPDF copies the session PDF to the path the reader picked.
// The session file stays the cache. The notice and the card use the copy.
func (a *App) deliverConvertedPDF(canonical string, result FileCompanionConvertResult, dest string) FileCompanionConvertResult {
	dest = strings.TrimSpace(dest)
	if dest == "" || !result.OK || result.Path == "" {
		return result
	}
	sessionPDF, _, err := a.fileCompanionRestorePaths(canonical)
	if err != nil || !strings.EqualFold(filepath.Clean(sessionPDF), filepath.Clean(result.Path)) {
		return result
	}
	if strings.EqualFold(filepath.Clean(sessionPDF), filepath.Clean(dest)) {
		return result
	}
	data, readErr := os.ReadFile(sessionPDF)
	if readErr != nil || len(data) == 0 {
		result.SaveFailed = true
		result.Message = "无法保存到该位置"
		return result
	}
	if writeErr := writeCompanionBytesAtomic(dest, data); writeErr != nil {
		result.SaveFailed = true
		result.Message = "无法保存到该位置"
		return result
	}
	info, statErr := os.Stat(sessionPDF)
	if statErr != nil || info.IsDir() || info.Size() != int64(len(data)) {
		result.SaveFailed = true
		result.Message = "无法保存到该位置"
		return result
	}
	if raw, marshalErr := json.Marshal(convertDestRecord{Path: dest, Size: info.Size(), Mtime: info.ModTime().UnixNano()}); marshalErr == nil {
		_ = os.WriteFile(convertDestSidecar(sessionPDF), raw, 0o644)
	}
	const marker = "结果文件："
	if i := strings.LastIndex(result.Message, marker); i >= 0 {
		result.Message = result.Message[:i+len(marker)] + dest
	}
	result.Path = dest
	return result
}

func discardSearchablePDF(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(convertDestSidecar(path))
}

func restorePublishError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errFileCompanionRestoreFont) {
		return "没有找到可嵌入的中文字体，无法生成可选中的 PDF。"
	}
	if errors.Is(err, gopdf2.ErrEncryptedPDF) {
		return fileCompanionRestorePassword
	}
	return "生成可选中的 PDF 失败：" + err.Error()
}

// materializeSearchablePDF returns searchable.pdf when it is the PDF this
// cache recorded. publish draws and records it when the bytes are missing or
// the recorded size does not match. The caller holds the work lock.
func (a *App) materializeSearchablePDF(doc *agent.ScanRestore, cache map[int]fileCompanionRestoreCachePage, cachePath, pdfPath string, sourceSize, sourceMtime, pdfSize int64, count int, publish bool) (string, error) {
	if !restoreHasLines(cache) {
		if len(cache) > 0 {
			discardSearchablePDF(pdfPath)
			if pdfSize != 0 {
				_ = a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, 0, count, cache)
			}
		}
		return "", nil
	}
	if pdfSize > 0 {
		if st, err := os.Stat(pdfPath); err == nil && !st.IsDir() && st.Size() == pdfSize {
			return pdfPath, nil
		}
	}
	if !publish {
		return "", nil
	}
	font, fontErr := docgen.SystemCJKFontBytes()
	if fontErr != nil {
		return "", errFileCompanionRestoreFont
	}
	out, writeErr := agent.RestoreRebuiltPDF(doc, restoreLinesByPage(cache), restorePageAssets(doc, cache), font)
	if writeErr != nil {
		return "", writeErr
	}
	if err := writeCompanionBytesAtomic(pdfPath, out); err != nil {
		return "", err
	}
	if err := a.saveFileCompanionRestoreCache(cachePath, sourceSize, sourceMtime, int64(len(out)), count, cache); err != nil {
		return pdfPath, nil
	}
	return pdfPath, nil
}

// publishCachedRestorePDF draws the cached lines once. A PDF whose recorded
// size already matches is left in place.
func (a *App) publishCachedRestorePDF(canonical string, reuse *agent.ScanRestore) (string, error) {
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	var doc *agent.ScanRestore
	if reuse != nil && reuse.Matches(info.Size(), info.ModTime().UnixNano()) {
		if agent.PDFIsEncrypted(reuse.Bytes()) {
			return "", gopdf2.ErrEncryptedPDF
		}
		doc = reuse
	} else {
		data, readErr := fileCompanionReadPDFBytes(canonical)
		if readErr != nil {
			return "", readErr
		}
		if agent.PDFIsEncrypted(data) {
			return "", gopdf2.ErrEncryptedPDF
		}
		info, err = os.Stat(canonical)
		if err != nil {
			return "", err
		}
		doc = agent.NewScanRestore(data)
		doc.BindSource(info.Size(), info.ModTime().UnixNano())
	}
	cache, cachePath, pdfPath, pdfSize, _, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil {
		return "", err
	}
	count, countErr := doc.PageCount()
	if countErr != nil || count < 1 {
		count = 0
	}
	return a.materializeSearchablePDF(doc, cache, cachePath, pdfPath, info.Size(), info.ModTime().UnixNano(), pdfSize, count, true)
}

// currentSearchablePDF is the conversion result for this source. A file that
// merely exists is not enough: the cache must match the source, contain
// lines, and record the same byte length.
func (a *App) currentSearchablePDF(canonical string) string {
	if a == nil || canonical == "" {
		return ""
	}
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return ""
	}
	cache, _, pdfPath, pdfSize, _, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil || pdfSize <= 0 || !restoreHasLines(cache) {
		return ""
	}
	st, err := os.Stat(pdfPath)
	if err != nil || st.IsDir() || st.Size() != pdfSize {
		return ""
	}
	return pdfPath
}

func (a *App) companionRestoreSnapshot(canonical string) (key string, cache map[int]fileCompanionRestoreCachePage) {
	unlock := lockCompanionRestoreWork(canonical)
	defer unlock()
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil
	}
	loaded, _, _, _, _, err := a.loadFileCompanionRestoreCache(canonical, info.Size(), info.ModTime().UnixNano())
	if err != nil || len(loaded) == 0 {
		return "", nil
	}
	return restoreCacheKey(loaded), loaded
}

func restoreCacheKey(cache map[int]fileCompanionRestoreCachePage) string {
	if len(cache) == 0 {
		return ""
	}
	nums := make([]int, 0, len(cache))
	for page := range cache {
		nums = append(nums, page)
	}
	sort.Ints(nums)
	var b strings.Builder
	for _, page := range nums {
		entry := cache[page]
		html := 0
		for _, block := range entry.Blocks {
			if strings.TrimSpace(block.HTML) != "" {
				html++
			}
		}
		fmt.Fprintf(&b, "%d|%t|%t|%t|%t|%t|%t|%d|%d|%d;", page, entry.AlreadyText, entry.NoPicture, entry.LayoutChecked, entry.FormulaPending, entry.TablePending, pageRestoreFailed(entry), len(entry.Lines), html, entry.FailTries)
	}
	return b.String()
}

func writeCompanionBytesAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	backup, err := fileCompanionReplaceFile(tmp, path)
	if backup != "" {
		_ = os.Remove(backup)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
