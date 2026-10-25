package guiapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/RapidAI/CodeClaw/corelib/doclayout"
	"github.com/RapidAI/CodeClaw/corelib/tablestruct"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestFileCompanionRestoreWritesSearchablePDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	jpegBytes := mustJPEG(t, 40, 20)
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, jpegBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{
			Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}, Formula: true,
		}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })

	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || !strings.Contains(text, "已按原版式生成新的 PDF") || !strings.Contains(text, "公式已用已接入的公式模型还原到原位置") || !strings.Contains(text, "原文件没有改动") {
		t.Fatalf("restore = %q ok=%v", text, ok)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source pdf changed")
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, jpegBytes) {
		t.Fatal("searchable pdf dropped the scan image")
	}
	extracted, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted, "恢复正文") {
		t.Fatalf("extracted = %q", extracted)
	}
	again, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 1 || !strings.Contains(again, "已按原版式生成新的 PDF") {
		t.Fatalf("second = %q ok=%v calls=%d", again, ok, calls)
	}
	afterSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterSecond) {
		t.Fatal("source pdf changed on the second call")
	}
}

func TestFileCompanionRestoreReportsFormulaStillPending(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`); !ok || calls != 1 {
		t.Fatalf("first call ok=%v calls=%d", ok, calls)
	}
	cachePath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json")
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != 1 {
		t.Fatalf("pages = %+v", cache.Pages)
	}
	cache.Pages[0].FormulaPending = true
	raw, err = json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	again, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 1 || !strings.Contains(again, "已按原版式生成新的 PDF") || !strings.Contains(again, "公式模型还在下载") {
		t.Fatalf("second = %q ok=%v calls=%d", again, ok, calls)
	}
}

func TestFileCompanionRestoreLeavesDigitalPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "digital.pdf")
	if err := os.WriteFile(path, buildDigitalCompanionPDF(t, font), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "不应识别", BBox: [4]int{0, 0, 10, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 0 || !strings.Contains(text, "已有文字层") {
		t.Fatalf("digital = %q ok=%v calls=%d", text, ok, calls)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")); !os.IsNotExist(err) {
		t.Fatalf("digital restore wrote a pdf: %v", err)
	}
}

func TestFileCompanionRestoreKeepsLayoutOrder(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{
			{Text: "右栏", BBox: [4]int{20, 0, 20, 10}},
			{Text: "左栏", BBox: [4]int{0, 0, 20, 10}},
		}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{
			{Label: "text", Score: 0.9, Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 20, Y1: 12}},
			{Label: doclayout.TableLabel, Score: 0.8, Order: 1, Box: doclayout.Box{X0: 20, Y0: 0, X1: 40, Y1: 20}},
		}, nil
	}
	fileCompanionRestoreTable = func(image.Image, []tablestruct.Line) (string, error) {
		return "<table><tr><td>右栏</td></tr></table>", nil
	}
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
	})
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolRestore, `{}`)
	if !ok || !strings.Contains(text, "已按原版式生成新的 PDF") || !strings.Contains(text, "版面已按原来的位置还原。") || !strings.Contains(text, "表格已恢复成结构。") {
		t.Fatalf("restore = %q ok=%v", text, ok)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != 1 || len(cache.Pages[0].Lines) != 2 || cache.Pages[0].Lines[0].Text != "左栏" || cache.Pages[0].Lines[1].Text != "右栏" {
		t.Fatalf("lines %+v", cache.Pages[0].Lines)
	}
	if len(cache.Pages[0].Blocks) != 2 || cache.Pages[0].Blocks[1].HTML != "<table><tr><td>右栏</td></tr></table>" || cache.Pages[0].Blocks[0].Text != "左栏" {
		t.Fatalf("blocks %+v", cache.Pages[0].Blocks)
	}
	left, right := cache.Pages[0].Blocks[0], cache.Pages[0].Blocks[1]
	if math.Abs(left.X-72) > 0.6 || math.Abs(left.Y-100) > 0.6 || math.Abs(left.W-100) > 0.6 || math.Abs(left.H-60) > 0.6 {
		t.Fatalf("left block = %+v", left)
	}
	if math.Abs(right.X-172) > 0.6 || math.Abs(right.Y-100) > 0.6 || math.Abs(right.W-100) > 0.6 || math.Abs(right.H-100) > 0.6 {
		t.Fatalf("right block = %+v", right)
	}
	if !cache.Pages[0].LayoutChecked || cache.Pages[0].TablePending {
		t.Fatalf("checked %+v", cache.Pages[0])
	}
}

func TestFileCompanionRestoreWaitsForTableModel(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	prevReady := fileCompanionTableReady
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{
			{Text: "右栏", BBox: [4]int{20, 0, 20, 10}},
			{Text: "左栏", BBox: [4]int{0, 0, 20, 10}},
		}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{
			{Label: "text", Score: 0.9, Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 20, Y1: 12}},
			{Label: doclayout.TableLabel, Score: 0.8, Order: 1, Box: doclayout.Box{X0: 20, Y0: 0, X1: 40, Y1: 20}},
		}, nil
	}
	fileCompanionRestoreTable = nil
	fileCompanionTableReady = func() bool { return false }
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
		fileCompanionTableReady = prevReady
	})
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || !strings.Contains(text, "已按原版式生成新的 PDF") || !strings.Contains(text, "版面已按原来的位置还原。") || !strings.Contains(text, "表格模型还在下载") || strings.Contains(text, "表格已恢复成结构。") {
		t.Fatalf("restore = %q ok=%v", text, ok)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	page := cache.Pages[0]
	if len(cache.Pages) != 1 || page.LayoutChecked || !page.TablePending || len(page.Blocks) != 2 || page.Blocks[1].HTML != "" || len(page.Lines) != 2 || page.Lines[0].Text != "左栏" {
		t.Fatalf("page %+v", page)
	}
	again, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 1 || !strings.Contains(again, "表格模型还在下载") {
		t.Fatalf("second = %q ok=%v calls=%d", again, ok, calls)
	}
}

func TestFileCompanionRestoreFillsTableWhenModelArrives(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	prevReady := fileCompanionTableReady
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "右栏", BBox: [4]int{20, 0, 20, 10}}}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{{
			Label: doclayout.TableLabel, Score: 0.8, Order: 0,
			Box: doclayout.Box{X0: 0, Y0: 0, X1: 40, Y1: 20},
		}}, nil
	}
	fileCompanionRestoreTable = nil
	fileCompanionTableReady = func() bool { return false }
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
		fileCompanionTableReady = prevReady
	})
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`); !ok || calls != 1 {
		t.Fatalf("first call calls=%d", calls)
	}
	fileCompanionTableReady = func() bool { return true }
	fileCompanionRestoreTable = func(image.Image, []tablestruct.Line) (string, error) {
		return "<table><tr><td>右栏</td></tr></table>", nil
	}
	again, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 2 || !strings.Contains(again, "表格已恢复成结构。") || strings.Contains(again, "表格模型还在下载") {
		t.Fatalf("second = %q ok=%v calls=%d", again, ok, calls)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != 1 || !cache.Pages[0].LayoutChecked || cache.Pages[0].TablePending || cache.Pages[0].Blocks[0].HTML != "<table><tr><td>右栏</td></tr></table>" {
		t.Fatalf("page %+v", cache.Pages[0])
	}
}

func TestFileCompanionRestoreReadsFormulaRegion(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevFormula := fileCompanionRestoreFormula
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{
			{Text: "分子", BBox: [4]int{0, 0, 20, 6}},
			{Text: "分母", BBox: [4]int{0, 6, 20, 6}},
			{Text: "旁边正文", BBox: [4]int{0, 14, 12, 6}},
			{Text: "a=b", BBox: [4]int{14, 14, 12, 6}},
		}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{{
			Label: "display_formula", Score: 0.95, Order: 0,
			Box: doclayout.Box{X0: 0, Y0: 0, X1: 24, Y1: 12},
		}}, nil
	}
	fileCompanionRestoreFormula = func(image.Image) (string, error) {
		calls++
		if calls == 1 {
			return `\frac{a}{b}`, nil
		}
		return `a=b`, nil
	}
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreFormula = prevFormula
	})
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolRestore, `{}`)
	if !ok || calls != 2 || !strings.Contains(text, "公式已用已接入的公式模型还原到原位置") {
		t.Fatalf("restore = %q ok=%v calls=%d", text, ok, calls)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	lines := cache.Pages[0].Lines
	if len(cache.Pages) != 1 || len(lines) != 3 || lines[0].Text != `\frac{a}{b}` || !lines[0].Formula || lines[1].Text != "旁边正文" || lines[1].Formula || lines[2].Text != "a=b" || !lines[2].Formula {
		t.Fatalf("lines %+v", lines)
	}
	if len(cache.Pages[0].Blocks) != 1 {
		t.Fatalf("blocks %+v", cache.Pages[0].Blocks)
	}
	formula := cache.Pages[0].Blocks[0]
	if math.Abs(formula.X-72) > 0.6 || math.Abs(formula.Y-100) > 0.6 || math.Abs(formula.W-120) > 0.6 || math.Abs(formula.H-60) > 0.6 || formula.Text != `\frac{a}{b}` {
		t.Fatalf("formula block = %+v", formula)
	}
}

func TestRestoreTableHTMLLeavesFormulaLinesOut(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	var got []tablestruct.Line
	prev := fileCompanionRestoreTable
	fileCompanionRestoreTable = func(_ image.Image, lines []tablestruct.Line) (string, error) {
		got = append([]tablestruct.Line(nil), lines...)
		return "<table><tr><td>格内</td></tr></table>", nil
	}
	t.Cleanup(func() { fileCompanionRestoreTable = prev })
	html := restoreTableHTML(img, doclayout.Box{X0: 0, Y0: 0, X1: 40, Y1: 20}, []agent.ScanTextLine{
		{Text: `\frac{a}{b}`, Formula: true, BBox: [4]int{0, 0, 20, 8}},
		{Text: "格内", BBox: [4]int{0, 10, 20, 8}},
	})
	if html != "<table><tr><td>格内</td></tr></table>" || len(got) != 1 || got[0].Text != "格内" {
		t.Fatalf("html %q lines %+v", html, got)
	}
}

func TestBlockTextGivesALineOneOwner(t *testing.T) {
	lines := []agent.ScanTextLine{
		{Text: `\frac{a}{b}`, Formula: true, BBox: [4]int{0, 0, 24, 8}},
		{Text: "旁边正文", BBox: [4]int{0, 8, 20, 6}},
	}
	regions := []doclayout.Region{
		{Label: "display_formula", Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 24, Y1: 8}},
		{Label: "text", Order: 1, Box: doclayout.Box{X0: 0, Y0: 0, X1: 40, Y1: 20}},
	}
	formula := blockText(lines, regions, 0)
	prose := blockText(lines, regions, 1)
	if formula != `\frac{a}{b}` || prose != "旁边正文" {
		t.Fatalf("formula %q prose %q", formula, prose)
	}
	regions[0].Order = 1
	regions[1].Order = 0
	formula = blockText(lines, regions, 0)
	prose = blockText(lines, regions, 1)
	if formula != "" || prose != `\frac{a}{b}`+"\n旁边正文" {
		t.Fatalf("reversed formula %q prose %q", formula, prose)
	}
}

func TestPictorialCropsKeepASmallMiss(t *testing.T) {
	jpeg := mustJPEG(t, 300, 400)
	place := agent.ScanImagePlacement{X: 20, Y: 30, DisplayW: 550, DisplayH: 800, PixelW: 300, PixelH: 400}
	blocks := []restoreBlock{
		{Label: "image", X: 20, Y: 30, W: 2, H: 2},
		{Label: "image", X: 40, Y: 50, W: 120, H: 80},
	}
	pics, hold := pictorialCrops(place, jpeg, blocks)
	if hold || len(pics) != 1 || math.Abs(pics[0].X-40) > 0.1 || math.Abs(pics[0].W-120) > 0.1 {
		t.Fatalf("pics=%d hold=%v", len(pics), hold)
	}
	place.MirrorY = true
	pics, hold = pictorialCrops(place, jpeg, blocks)
	if !hold || len(pics) != 0 {
		t.Fatalf("mirror pics=%d hold=%v", len(pics), hold)
	}
	place.MirrorY = false
	place.Turn = 90
	pics, hold = pictorialCrops(place, jpeg, blocks)
	if !hold || len(pics) != 0 {
		t.Fatalf("turn pics=%d hold=%v", len(pics), hold)
	}
}

func loadCompanionRestoreFont(t *testing.T) ([]byte, error) {
	t.Helper()
	return docgen.SystemCJKFontBytes()
}

func buildRestorableCompanionPDF(t *testing.T, jpeg []byte) []byte {
	t.Helper()
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	holder, err := gopdf2.ImageHolderByBytes(jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(holder, 72, 100, &gopdf2.Rect{W: 200, H: 100}); err != nil {
		t.Fatal(err)
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFileCompanionPDFScanDetectsAScan(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	scanPath := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(scanPath, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, scanPath)
	got, err := app.FileCompanionPDFScan(scanPath)
	if err != nil || !got.Scan || got.Path != "" {
		t.Fatalf("scan = %+v err=%v", got, err)
	}
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(t.TempDir(), "digital.pdf")
	if err := os.WriteFile(textPath, buildDigitalCompanionPDF(t, font), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, textPath)
	digital, err := app.FileCompanionPDFScan(textPath)
	if err != nil || digital.Scan || digital.Path != "" {
		t.Fatalf("digital = %+v err=%v", digital, err)
	}
	coveredPath := filepath.Join(t.TempDir(), "covered.pdf")
	if err := os.WriteFile(coveredPath, buildCompanionPDFWithImageAndText(t, font, mustJPEG(t, 300, 400), 20, 30, 550, 800, "页码"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, coveredPath)
	covered, err := app.FileCompanionPDFScan(coveredPath)
	if err != nil || !covered.Scan {
		t.Fatalf("scan with a page number = %+v err=%v", covered, err)
	}
	logoPath := filepath.Join(t.TempDir(), "logo.pdf")
	if err := os.WriteFile(logoPath, buildCompanionPDFWithImageAndText(t, font, mustJPEG(t, 40, 20), 72, 72, 36, 18, "已有正文"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, logoPath)
	logo, err := app.FileCompanionPDFScan(logoPath)
	if err != nil || logo.Scan {
		t.Fatalf("text page with a logo = %+v err=%v", logo, err)
	}
}

func TestFileCompanionPDFScanDropsThePDFWhenAScanBecomesDigital(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" {
		t.Fatalf("convert = %+v", got)
	}
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buildDigitalCompanionPDF(t, font), 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err != nil || probe.Scan || probe.Path != "" {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("stale pdf kept: %v", statErr)
	}
}

func TestFileCompanionPDFScanKeepsThePDFWhenTheSameFileCannotBeRead(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	prevRecognize := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prevRecognize })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" {
		t.Fatalf("convert = %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same size and mtime, but the bytes no longer parse. This is still the
	// file the cache names, so the conversion stays.
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(info.Size())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err == nil || probe.Scan || probe.Path != "" {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if _, statErr := os.Stat(outPath); statErr != nil {
		t.Fatalf("same file dropped the pdf: %v", statErr)
	}
}

func TestFileCompanionPDFScanDropsThePDFWhenAReadFailsForOtherBytes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	prevRecognize := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prevRecognize })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" {
		t.Fatalf("convert = %+v", got)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err != nil || !probe.Scan || probe.Path != "" {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("other bytes kept the pdf: %v", statErr)
	}
}

func TestFileCompanionConvertScanDropsThePDFWhenAScanBecomesDigital(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" || calls != 1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buildDigitalCompanionPDF(t, font), 0o644); err != nil {
		t.Fatal(err)
	}
	again := finishFileCompanionConvert(t, app, path)
	if !again.OK || again.Scan || again.Path != "" || calls != 1 || !strings.Contains(again.Message, "已有文字层") {
		t.Fatalf("digital = %+v calls=%d", again, calls)
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("stale pdf kept: %v", statErr)
	}
}

func TestFileCompanionConvertScanReadsAScanThatAlsoHasText(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "covered.pdf")
	jpegBytes := mustJPEG(t, 300, 400)
	if err := os.WriteFile(path, buildCompanionPDFWithImageAndText(t, font, jpegBytes, 20, 30, 550, 800, "页码"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "扫描正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || !got.Complete || calls != 1 || got.Path == "" || strings.Contains(got.Message, "已有文字层") {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	out, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, jpegBytes) {
		t.Fatal("rebuilt pdf still embeds the page-covering scan")
	}
	if bytes.Contains(out, []byte("/ca 0.000")) {
		t.Fatal("rebuilt text is still invisible")
	}
	extracted, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted, "扫描正文") {
		t.Fatalf("extracted = %q", extracted)
	}
}

func TestFileCompanionConvertScanRebuildsAFigureAndTable(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "layout.pdf")
	scan := mustJPEG(t, 300, 400)
	src := buildCompanionPDFWithImageAndText(t, font, scan, 20, 30, 550, 800, "页码")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, path)
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		return []agent.ScanTextLine{
			{Text: "图中", BBox: [4]int{8, 8, 24, 12}},
			{Text: "格内", BBox: [4]int{90, 16, 30, 12}},
			{Text: "图外", BBox: [4]int{8, 200, 40, 16}},
		}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{
			{Label: "image", Score: 0.9, Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 70, Y1: 50}},
			{Label: doclayout.TableLabel, Score: 0.9, Order: 1, Box: doclayout.Box{X0: 80, Y0: 0, X1: 180, Y1: 70}},
			{Label: "text", Score: 0.9, Order: 2, Box: doclayout.Box{X0: 0, Y0: 180, X1: 80, Y1: 240}},
		}, nil
	}
	fileCompanionRestoreTable = func(image.Image, []tablestruct.Line) (string, error) {
		return "<table><tr><td>甲</td><td>乙</td></tr><tr><td>丙</td><td>丁</td></tr></table>", nil
	}
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
	})
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || got.Path == "" || !strings.Contains(got.Message, "表格已恢复成结构") || !strings.Contains(got.Message, "已按原版式生成新的 PDF") {
		t.Fatalf("convert = %+v", got)
	}
	out, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, scan) {
		t.Fatal("rebuilt pdf still embeds the page-covering scan")
	}
	view, err := agent.NewScanRestore(src).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	place := view.Placement
	scaleX := place.DisplayW / float64(place.PixelW)
	scaleY := place.DisplayH / float64(place.PixelH)
	crop, ok := agent.CropScanRegion(view.Payload, place, place.X, place.Y, 70*scaleX, 50*scaleY)
	if !ok || !bytes.Contains(out, crop) {
		t.Fatal("figure crop missing")
	}
	extracted, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted, "图外") || !strings.Contains(extracted, "甲") || !strings.Contains(extracted, "乙") {
		t.Fatalf("extracted = %q", extracted)
	}
	if strings.Contains(extracted, "图中") || strings.Contains(extracted, "格内") {
		t.Fatalf("covered line still drawn: %q", extracted)
	}
}

func buildCompanionPDFWithImageAndText(t *testing.T, font, jpeg []byte, x, y, w, h float64, text string) []byte {
	t.Helper()
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFontData("body", font); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	holder, err := gopdf2.ImageHolderByBytes(jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(holder, x, y, &gopdf2.Rect{W: w, H: h}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("body", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(72, 18)
	if err := pdf.Cell(nil, text); err != nil {
		t.Fatal(err)
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFileCompanionConvertScanWritesTheChosenPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(blocker, "picked.pdf")
	if _, err := app.FileCompanionConvertScan(path, bad); err == nil || !strings.Contains(err.Error(), "无法保存到该位置") || calls != 0 {
		t.Fatalf("early unwritable err=%v calls=%d", err, calls)
	}
	dest := filepath.Join(t.TempDir(), "picked.pdf")
	got := finishFileCompanionConvertTo(t, app, path, dest)
	if !got.OK || got.Path != dest || calls != 1 || !strings.Contains(got.Message, dest) {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("convert changed the open file")
	}
	sessionPDF := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	want, err := os.ReadFile(sessionPDF)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(want, copied) {
		t.Fatal("chosen pdf is not the conversion")
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err != nil || !probe.Scan || probe.Path != dest {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	prevDialog := fileCompanionConvertSaveDialog
	var dialogDir, dialogName string
	fileCompanionConvertSaveDialog = func(_ *App, dir, name string) (string, error) {
		dialogDir, dialogName = dir, name
		return "", nil
	}
	t.Cleanup(func() { fileCompanionConvertSaveDialog = prevDialog })
	chosen, chooseErr := app.FileCompanionChooseConvertDestination(path)
	if chooseErr != nil || chosen != "" || !strings.EqualFold(dialogDir, filepath.Dir(dest)) || dialogName != filepath.Base(dest) {
		t.Fatalf("next dialog = %q %q chosen=%q err=%v", dialogDir, dialogName, chosen, chooseErr)
	}
	other := filepath.Join(t.TempDir(), "again.pdf")
	located := finishFileCompanionConvertTo(t, app, path, other)
	if !located.OK || located.Started || located.Path != other || calls != 1 {
		t.Fatalf("locate = %+v calls=%d", located, calls)
	}
	again, err := os.ReadFile(other)
	if err != nil || !bytes.Equal(want, again) {
		t.Fatal("second click did not copy the finished pdf")
	}
	follow, err := app.FileCompanionPDFScan(path)
	if err != nil || follow.Path != other {
		t.Fatalf("probe after second = %+v err=%v", follow, err)
	}
	if err := os.WriteFile(other, []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := app.FileCompanionPDFScan(path)
	if err != nil || !stale.Scan || stale.Path != sessionPDF {
		t.Fatalf("replaced copy probe = %+v err=%v", stale, err)
	}
	if err := os.WriteFile(other, again, 0o644); err != nil {
		t.Fatal(err)
	}
	restored, err := app.FileCompanionPDFScan(path)
	if err != nil || restored.Path != other {
		t.Fatalf("restored copy probe = %+v err=%v", restored, err)
	}
	raw, err := json.Marshal(convertDestRecord{Path: other, Size: 1, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(convertDestSidecar(sessionPDF), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	older, err := app.FileCompanionPDFScan(path)
	if err != nil || !older.Scan || older.Path != sessionPDF {
		t.Fatalf("stale record probe = %+v err=%v", older, err)
	}
	fileCompanionConvertSaveDialog = func(_ *App, dir, name string) (string, error) {
		dialogDir, dialogName = dir, name
		return "", nil
	}
	if _, err := app.FileCompanionChooseConvertDestination(path); err != nil || !strings.EqualFold(dialogDir, filepath.Dir(other)) || dialogName != filepath.Base(other) {
		t.Fatalf("stale dialog = %q %q err=%v", dialogDir, dialogName, err)
	}
	_, canonical, err := app.fileCompanionGrant(path)
	if err != nil {
		t.Fatal(err)
	}
	failed := app.deliverConvertedPDF(canonical, FileCompanionConvertResult{
		OK: true, Scan: true, Complete: true, Path: sessionPDF,
		Message: "已按原版式生成新的 PDF，原文件没有改动。共 1 页。结果文件：" + sessionPDF,
	}, bad)
	if !failed.SaveFailed || !failed.OK || !failed.Complete || failed.Path != sessionPDF || failed.Message != "无法保存到该位置" || calls != 1 {
		t.Fatalf("save failed = %+v calls=%d", failed, calls)
	}
	refused, err := app.FileCompanionConvertScan(path, path)
	if err == nil || refused.Path != "" || !strings.Contains(err.Error(), "不能覆盖当前文件") {
		t.Fatalf("overwrite = %+v err=%v", refused, err)
	}
}

func TestFileCompanionChooseConvertDestinationCancel(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, path)
	prev := fileCompanionConvertSaveDialog
	var gotDir, gotName string
	fileCompanionConvertSaveDialog = func(_ *App, dir, name string) (string, error) {
		gotDir, gotName = dir, name
		return "", nil
	}
	t.Cleanup(func() { fileCompanionConvertSaveDialog = prev })
	got, err := app.FileCompanionChooseConvertDestination(path)
	if err != nil || got != "" || !strings.EqualFold(gotDir, filepath.Dir(path)) || gotName != "scan-可选中.pdf" {
		t.Fatalf("cancel = %q dialog=%q want dir %q name %q err=%v", got, gotDir, filepath.Dir(path), gotName, err)
	}
	fileCompanionConvertSaveDialog = func(*App, string, string) (string, error) {
		return path, nil
	}
	if _, err := app.FileCompanionChooseConvertDestination(path); err == nil || !strings.Contains(err.Error(), "不能覆盖当前文件") {
		t.Fatalf("source dest err=%v", err)
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionConvertSaveDialog = func(*App, string, string) (string, error) {
		return filepath.Join(blocker, "out.pdf"), nil
	}
	if _, err := app.FileCompanionChooseConvertDestination(path); err == nil || !strings.Contains(err.Error(), "无法保存到该位置") {
		t.Fatalf("unwritable choose err=%v", err)
	}
}

func finishFileCompanionConvert(t *testing.T, app *App, path string) FileCompanionConvertResult {
	t.Helper()
	return finishFileCompanionConvertTo(t, app, path, "")
}

func finishFileCompanionConvertTo(t *testing.T, app *App, path, dest string) FileCompanionConvertResult {
	t.Helper()
	got, err := app.FileCompanionConvertScan(path, dest)
	if err != nil {
		t.Fatalf("start convert: %v", err)
	}
	if !got.Started {
		return got
	}
	done, ok := app.waitFileCompanionConvert(path, time.Minute)
	if !ok {
		t.Fatal("convert did not finish")
	}
	return done
}

func TestCompanionConvertNoticeNamesTheSourceFile(t *testing.T) {
	notice := companionConvertNotice("C:/scans/book.pdf", FileCompanionConvertResult{
		OK: true, Scan: true, Complete: true, Path: "C:/out/searchable.pdf",
		Message: "已按原版式生成新的 PDF，原文件没有改动。共 17 页。",
	})
	if notice["path"] != "C:/scans/book.pdf" || notice["pdf"] != "C:/out/searchable.pdf" || notice["ok"] != true || notice["complete"] != true {
		t.Fatalf("notice = %#v", notice)
	}
	message, _ := notice["message"].(string)
	if !strings.Contains(message, "已按原版式生成新的 PDF") {
		t.Fatalf("message = %q", message)
	}
}

func TestFileCompanionConvertScanReturnsBeforeThePagesFinish(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	seedCompanionTestFileAt(t, app, path)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		once.Do(func() { close(entered) })
		<-release
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prev
		select {
		case <-release:
		default:
			close(release)
		}
	})
	got, err := app.FileCompanionConvertScan(path, "")
	if err != nil || !got.Started || !got.OK || got.Path != "" || got.Busy {
		t.Fatalf("start = %+v err=%v", got, err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("ocr did not start")
	}
	busy, err := app.FileCompanionConvertScan(path, "")
	if err != nil || !busy.Busy || busy.Started || busy.Path != "" || !strings.Contains(busy.Message, "正在转换") {
		t.Fatalf("second = %+v err=%v", busy, err)
	}
	close(release)
	done, ok := app.waitFileCompanionConvert(path, time.Minute)
	if !ok || !done.OK || !done.Complete || done.Path == "" || !strings.Contains(done.Message, "已按原版式生成新的 PDF") {
		t.Fatalf("done = %+v ok=%v", done, ok)
	}
}

func TestFileCompanionConvertScanFinishesEveryPage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	jpegBytes := mustJPEG(t, 40, 20)
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, fileCompanionRestorePagesMax+1, jpegBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })

	copiesBefore := docgen.SystemCJKFontCopies()
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || !got.Complete || got.Pending || calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	if copied := docgen.SystemCJKFontCopies() - copiesBefore; copied != 1 {
		t.Fatalf("font copies = %d", copied)
	}
	if !strings.Contains(got.Message, "已按原版式生成新的 PDF") || !strings.Contains(got.Message, "原文件没有改动") || !strings.Contains(got.Message, "共 17 页") {
		t.Fatalf("message = %q", got.Message)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source pdf changed")
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if got.Path != outPath {
		t.Fatalf("path = %q want %q", got.Path, outPath)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, jpegBytes) {
		t.Fatal("searchable pdf dropped the scan image")
	}
	extracted, err := gopdf2.ExtractPageText(out, fileCompanionRestorePagesMax)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted, "恢复正文") {
		t.Fatalf("last page = %q", extracted)
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err != nil || !probe.Scan || probe.Path != outPath {
		t.Fatalf("probe after convert = %+v err=%v", probe, err)
	}
	infoBefore, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	copiesBeforeSecond := docgen.SystemCJKFontCopies()
	again, err := app.FileCompanionConvertScan(path, "")
	if err != nil || again.Started || again.Busy || !again.OK || !again.Complete || again.Pending || again.Path != outPath || calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("locate = %+v calls=%d err=%v", again, calls, err)
	}
	if !strings.Contains(again.Message, "共 17 页") {
		t.Fatalf("locate message = %q", again.Message)
	}
	if docgen.SystemCJKFontCopies() != copiesBeforeSecond {
		t.Fatalf("second click copied the font %d times", docgen.SystemCJKFontCopies()-copiesBeforeSecond)
	}
	infoAfter, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) || infoBefore.Size() != infoAfter.Size() {
		t.Fatal("second click rewrote searchable.pdf")
	}
}

func TestFileCompanionConvertScanLearnsThePageCountThenLocates(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Complete || got.Path == "" || calls != 1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	cachePath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json")
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var stored fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Count != 1 || stored.PDFSize == 0 {
		t.Fatalf("cache count = %d pdf = %d", stored.Count, stored.PDFSize)
	}
	infoBefore, err := os.Stat(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	stored.Count = 0
	rewritten, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
	learning, err := app.FileCompanionConvertScan(path, "")
	if err != nil || !learning.Started {
		t.Fatalf("without a page count = %+v err=%v", learning, err)
	}
	learned, ok := app.waitFileCompanionConvert(path, time.Minute)
	if !ok || !learned.Complete || learned.Path != got.Path || calls != 1 {
		t.Fatalf("learned = %+v ok=%v calls=%d", learned, ok, calls)
	}
	infoAfter, err := os.Stat(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) || infoBefore.Size() != infoAfter.Size() {
		t.Fatal("learning the page count rewrote searchable.pdf")
	}
	raw, err = os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	stored = fileCompanionRestoreCache{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Count != 1 {
		t.Fatalf("stored count = %d", stored.Count)
	}
	located, err := app.FileCompanionConvertScan(path, "")
	if err != nil || located.Started || located.Busy || !located.Complete || located.Pending || located.Path != got.Path || calls != 1 {
		t.Fatalf("locate = %+v calls=%d err=%v", located, calls, err)
	}
}

func TestFileCompanionConvertScanLeavesDigitalPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	font, err := loadCompanionRestoreFont(t)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "digital.pdf")
	if err := os.WriteFile(path, buildDigitalCompanionPDF(t, font), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "不应识别", BBox: [4]int{0, 0, 10, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Scan || calls != 0 || got.Path != "" || !strings.Contains(got.Message, "已有文字层") {
		t.Fatalf("digital = %+v calls=%d", got, calls)
	}
	if _, statErr := os.Stat(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")); !os.IsNotExist(statErr) {
		t.Fatalf("digital convert wrote a pdf: %v", statErr)
	}
}

func TestFileCompanionConvertScanKeepsLayoutAndStopsWhileTablesWait(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, fileCompanionRestorePagesMax+1, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	prevReady := fileCompanionTableReady
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "单元格", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{
			{Label: doclayout.TableLabel, Score: 0.8, Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 40, Y1: 20}},
		}, nil
	}
	fileCompanionRestoreTable = nil
	fileCompanionTableReady = func() bool { return false }
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
		fileCompanionTableReady = prevReady
	})
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || got.Path == "" {
		t.Fatalf("convert = %+v", got)
	}
	if calls != fileCompanionRestorePagesMax+1 || !got.Complete || !got.Pending {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	if !strings.Contains(got.Message, "已按原版式生成新的 PDF") || !strings.Contains(got.Message, "版面已按原来的位置还原") || !strings.Contains(got.Message, "表格模型还在下载") || !strings.Contains(got.Message, "再点「转换」") || strings.Contains(got.Message, "表格已恢复成结构") {
		t.Fatalf("message = %q calls=%d", got.Message, calls)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source pdf changed")
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != fileCompanionRestorePagesMax+1 {
		t.Fatalf("pages = %d", len(cache.Pages))
	}
	ack, err := app.FileCompanionConvertScan(path, "")
	if err != nil || !ack.Started || ack.Path != "" {
		t.Fatalf("pending click = %+v err=%v", ack, err)
	}
	done, ok := app.waitFileCompanionConvert(path, time.Minute)
	if !ok || !done.Pending || done.Path == "" || calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("pending done = %+v ok=%v calls=%d", done, ok, calls)
	}
}

func TestFileCompanionRestoreReachesLaterPagesWhileTablesWait(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, fileCompanionRestorePagesMax+1, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prevRec := fileCompanionRestoreRecognize
	prevLayout := fileCompanionRestoreLayout
	prevTable := fileCompanionRestoreTable
	prevReady := fileCompanionTableReady
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "单元格", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	fileCompanionRestoreLayout = func(image.Image) ([]doclayout.Region, error) {
		return []doclayout.Region{
			{Label: doclayout.TableLabel, Score: 0.8, Order: 0, Box: doclayout.Box{X0: 0, Y0: 0, X1: 40, Y1: 20}},
		}, nil
	}
	fileCompanionRestoreTable = nil
	fileCompanionTableReady = func() bool { return false }
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionRestoreLayout = prevLayout
		fileCompanionRestoreTable = prevTable
		fileCompanionTableReady = prevReady
	})
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`); !ok {
		t.Fatal("first restore failed")
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != fileCompanionRestorePagesMax || calls != fileCompanionRestorePagesMax {
		t.Fatalf("first window = %d calls=%d", len(cache.Pages), calls)
	}
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRestore, `{}`); !ok {
		t.Fatal("second restore failed")
	}
	if calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("second call re-recognized waiting pages: calls=%d", calls)
	}
	raw, err = os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	cache = fileCompanionRestoreCache{}
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != fileCompanionRestorePagesMax+1 {
		t.Fatalf("second window = %d", len(cache.Pages))
	}
}

func TestConvertMessageFromCacheCountsOnlyCachedPages(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prevRec := fileCompanionRestoreRecognize
	prevReady := fileCompanionTableReady
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) { return nil, nil }
	fileCompanionTableReady = func() bool { return false }
	t.Cleanup(func() {
		fileCompanionRestoreRecognize = prevRec
		fileCompanionTableReady = prevReady
	})
	partial := map[int]fileCompanionRestoreCachePage{
		1: {Page: 1, Lines: []agent.PlacedScanLine{{Text: "甲"}}},
	}
	msg, complete, pending := app.convertMessageFromCache(partial, 3, "out.pdf", nil, 2)
	if complete || pending || strings.Contains(msg, "共 3 页") || !strings.Contains(msg, "后面还有 2 页") || !strings.Contains(msg, "再点「转换」") {
		t.Fatalf("partial = %q complete=%v pending=%v", msg, complete, pending)
	}
	full := map[int]fileCompanionRestoreCachePage{
		1: {Page: 1, TablePending: true, Lines: []agent.PlacedScanLine{{Text: "甲"}}, Blocks: []restoreBlock{{Label: "table"}}},
		2: {Page: 2, Lines: []agent.PlacedScanLine{{Text: "乙"}}},
	}
	msg, complete, pending = app.convertMessageFromCache(full, 2, "out.pdf", nil, 1)
	if complete || pending == false || strings.Contains(msg, "共 2 页") || !strings.Contains(msg, "请再点「转换」继续。") || !strings.Contains(msg, "表格模型还在下载") || strings.Contains(msg, "表格已恢复成结构") {
		t.Fatalf("refresh left = %q complete=%v pending=%v", msg, complete, pending)
	}
	msg, complete, pending = app.convertMessageFromCache(full, 2, "out.pdf", nil, 0)
	if !complete || !pending || !strings.Contains(msg, "共 2 页") || !strings.Contains(msg, "表格模型还在下载") || !strings.Contains(msg, "再点「转换」") || strings.Contains(msg, "表格已恢复成结构") {
		t.Fatalf("waiting = %q complete=%v pending=%v", msg, complete, pending)
	}
}

func TestFileCompanionConvertScanReachesLaterPagesWhenEarlierPagesFail(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, fileCompanionRestorePagesMax+1, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		if calls <= fileCompanionRestorePagesMax {
			return nil, errors.New("本页无法识别")
		}
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || got.Complete || got.Path == "" || calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	if strings.Contains(got.Message, "共 17 页") || !strings.Contains(got.Message, "第 1 页识别失败") || !strings.Contains(got.Message, "请再点「转换」继续") {
		t.Fatalf("message = %q", got.Message)
	}
	out, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := gopdf2.ExtractPageText(out, fileCompanionRestorePagesMax)
	if err != nil || !strings.Contains(extracted, "恢复正文") {
		t.Fatalf("last page = %q err=%v", extracted, err)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != fileCompanionRestorePagesMax+1 || cache.Pages[0].Failed == "" || len(cache.Pages[0].Lines) != 0 {
		t.Fatalf("cache pages=%d first=%+v", len(cache.Pages), cache.Pages[0])
	}
	again := finishFileCompanionConvert(t, app, path)
	if !again.OK || !again.Complete || calls != (fileCompanionRestorePagesMax*2)+1 {
		t.Fatalf("retry = %+v calls=%d", again, calls)
	}
	if !strings.Contains(again.Message, "共 17 页") || strings.Contains(again.Message, "识别失败") {
		t.Fatalf("retry message = %q", again.Message)
	}
}

func TestFileCompanionConvertScanKeepsPDFWhenOCRStops(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, fileCompanionRestorePagesMax+1, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		if calls > fileCompanionRestorePagesMax {
			return nil, errFileCompanionRestoreOCRNotReady
		}
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || !got.Scan || got.Complete || got.Path == "" || calls != fileCompanionRestorePagesMax+1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	if !strings.Contains(got.Message, "本机识别尚未就绪") || !strings.Contains(got.Message, "后面还有 1 页") {
		t.Fatalf("message = %q", got.Message)
	}
	out, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := gopdf2.ExtractPageText(out, 0)
	if err != nil || !strings.Contains(extracted, "恢复正文") {
		t.Fatalf("first page = %q err=%v", extracted, err)
	}
}

func TestFileCompanionConvertScanKeepsPagesRecognizedBeforeOCRStops(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDFPages(t, 2, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		if calls > 1 {
			return nil, errFileCompanionRestoreOCRNotReady
		}
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" || got.Complete || calls != 2 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	if !strings.Contains(got.Message, "本机识别尚未就绪") || !strings.Contains(got.Message, "后面还有 1 页") {
		t.Fatalf("message = %q", got.Message)
	}
	out, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := gopdf2.ExtractPageText(out, 0)
	if err != nil || !strings.Contains(extracted, "恢复正文") {
		t.Fatalf("first page = %q err=%v", extracted, err)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Pages) != 1 || cache.Pages[0].Page != 1 || len(cache.Pages[0].Lines) != 1 {
		t.Fatalf("cache = %+v", cache.Pages)
	}
	again := finishFileCompanionConvert(t, app, path)
	if !again.OK || again.Path == "" || calls != 3 {
		t.Fatalf("second = %+v calls=%d", again, calls)
	}
}

func TestFileCompanionConvertScanDropsPDFWhenSourceChanges(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildRestorableCompanionPDF(t, mustJPEG(t, 40, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	calls := 0
	prev := fileCompanionRestoreRecognize
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return []agent.ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}}, nil
	}
	t.Cleanup(func() { fileCompanionRestoreRecognize = prev })
	got := finishFileCompanionConvert(t, app, path)
	if !got.OK || got.Path == "" || calls != 1 {
		t.Fatalf("convert = %+v calls=%d", got, calls)
	}
	outPath := filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.pdf")
	if got.Path != outPath {
		t.Fatalf("path = %q", got.Path)
	}
	oldPDF, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	infoBefore, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", doc.SessionID, "searchable.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cache fileCompanionRestoreCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	if cache.PDFSize != infoBefore.Size() || cache.PDFSize == 0 {
		t.Fatalf("pdf size = %d file = %d", cache.PDFSize, infoBefore.Size())
	}
	again := finishFileCompanionConvert(t, app, path)
	if !again.OK || !again.Complete || calls != 1 || again.Path != outPath {
		t.Fatalf("second = %+v calls=%d", again, calls)
	}
	infoAfter, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) || infoBefore.Size() != infoAfter.Size() {
		t.Fatal("second click rewrote searchable.pdf")
	}
	replacement := buildRestorableCompanionPDFPages(t, 2, mustJPEG(t, 48, 24))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == len(replacement) {
		t.Fatal("replacement did not change the source size")
	}
	if err := os.WriteFile(path, replacement, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := app.FileCompanionPDFScan(path)
	if err != nil || !probe.Scan || probe.Path != "" {
		t.Fatalf("probe = %+v err=%v", probe, err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("stale pdf kept: %v", statErr)
	}
	fileCompanionRestoreRecognize = func([]byte) ([]agent.ScanTextLine, error) {
		calls++
		return nil, errors.New("本页无法识别")
	}
	failed := finishFileCompanionConvert(t, app, path)
	if failed.Path != "" || !strings.Contains(failed.Message, "识别失败") {
		t.Fatalf("replaced = %+v", failed)
	}
	if leftover, readErr := os.ReadFile(outPath); readErr == nil && bytes.Equal(leftover, oldPDF) {
		t.Fatal("replaced scan returned the previous pdf")
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func buildRestorableCompanionPDFPages(t *testing.T, pages int, jpeg []byte) []byte {
	t.Helper()
	if pages < 1 {
		t.Fatal("pages")
	}
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	for i := 0; i < pages; i++ {
		holder, err := gopdf2.ImageHolderByBytes(jpeg)
		if err != nil {
			t.Fatal(err)
		}
		pdf.AddPage()
		if err := pdf.ImageByHolder(holder, 72, 100, &gopdf2.Rect{W: 200, H: 100}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func buildDigitalCompanionPDF(t *testing.T, font []byte) []byte {
	t.Helper()
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFontData("body", font); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("body", "", float64(14)); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(72, 72)
	if err := pdf.Cell(nil, "已有正文"); err != nil {
		t.Fatal(err)
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
