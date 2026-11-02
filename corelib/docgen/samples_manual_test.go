package docgen

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManualRenderStyleSamples renders every built-in PDF style for visual
// acceptance. Run explicitly:
//
//	MACLAW_PDF_SAMPLES=<outDir> go test ./corelib/docgen -run TestManualRenderStyleSamples
func TestManualRenderStyleSamples(t *testing.T) {
	outDir := os.Getenv("MACLAW_PDF_SAMPLES")
	if outDir == "" {
		t.Skip("set MACLAW_PDF_SAMPLES to render samples")
	}
	gen := New()
	if !gen.HasFont() {
		t.Skip("no font")
	}
	content := "## 经营回顾\n\n本季度营收 12.6 亿，同比增长 218%。\n\n- 一线产能恢复到计划的 96%\n- 重点区域签约 18 家渠道\n\n### 关键数字\n\n| 指标 | 本季 | 同比 |\n| --- | --- | --- |\n| 营收 | 12.6亿 | +218% |\n| 毛利率 | 41.2% | +3.1pt |\n\n## 部署示例\n\n```go\nfmt.Println(\"hello maclaw\")\n```\n\n> 把复杂留给系统，把清晰交给用户。\n\n## 下一阶段\n\n1. 试点验证\n2. 区域推广\n3. 全面铺开\n"
	for _, style := range builtinStyles() {
		dir := filepath.Join(outDir, style.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "sample.pdf")
		if _, err := GenerateToFile(Spec{
			Title:       style.Label + " · 样张",
			Subtitle:    "封面 / 页脚 / 标题层级 / 表格 / 面板验收",
			ProjectName: "季度经营汇报",
			Brand:       "MaClaw",
			FooterHint:  "由 MaClaw 生成",
			Content:     content,
			StyleID:     style.ID,
			PaperSize:   "a4",
		}, path); err != nil {
			t.Fatalf("%s: %v", style.ID, err)
		}
	}
}
