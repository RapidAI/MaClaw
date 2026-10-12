package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

func TestProbeColdFigureListing(t *testing.T) {
	gen := docgen.New()
	if !gen.HasFont() {
		t.Skip("no font")
	}
	// 造一份内嵌图片的 PDF，当作伴读文档
	dir := t.TempDir()
	figure := writeCompanionTestJPEGDataURL(t)
	pdfBytes, err := gen.Generate(docgen.Spec{
		Title: "report", Brand: "b", PaperSize: "A4",
		Content: "## 图\n\n![](" + figure + ")\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)
	if _, err := app.FileCompanionOpen(pdfPath); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(pdfPath)
	if err != nil {
		t.Fatal(err)
	}

	figs := app.fileCompanionFigures(canonical)
	t.Logf("提取到配图 %d 张", len(figs))
	if len(figs) == 0 {
		t.Skip("这份 PDF 没有可提取的配图")
	}
	figPath := figs[0].path
	if _, err := app.FileCompanionFigureDataURL(figPath); err != nil {
		t.Logf("热缓存取图: 失败 %v", err)
	} else {
		t.Logf("热缓存取图: 成功")
	}

	// 模拟应用重启：内存态清空，磁盘文件仍在
	st := app.companionRuntime()
	st.mu.Lock()
	st.figures = map[string]*companionFigureInventory{}
	st.figureLedger = map[string][]string{}
	st.mu.Unlock()

	if _, err := app.FileCompanionFigureDataURL(figPath); err != nil {
		t.Logf(">>> 冷缓存取图: 失败 %v  ← 这就是「图缺失」", err)
	} else {
		t.Logf("冷缓存取图: 成功（无此缺陷）")
	}
}
