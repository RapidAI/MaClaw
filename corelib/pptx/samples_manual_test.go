package pptx

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestManualRenderStyleSamples renders every built-in style to a deck plus
// preview PNGs for visual acceptance. Run explicitly:
//
//	MACLAW_STYLE_SAMPLES=<outDir> go test ./corelib/pptx -run TestManualRenderStyleSamples
func TestManualRenderStyleSamples(t *testing.T) {
	outDir := os.Getenv("MACLAW_STYLE_SAMPLES")
	if outDir == "" {
		t.Skip("set MACLAW_STYLE_SAMPLES to render samples")
	}
	for _, spec := range styleCatalog {
		dir := filepath.Join(outDir, spec.id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "deck.pptx")
		outline := Outline{
			Title:    spec.label + " · 风格样张",
			Subtitle: "全版式验收：目录 / 要点 / 指标 / 阶段 / 对比 / 引述",
			Theme:    spec.id,
			Purpose:  "季度经营汇报",
			Slides: []OutlineSlide{
				{Title: "目录", Kicker: "AGENDA", Layout: "agenda", Bullets: []string{"经营回顾：整体节奏与结果", "关键举措：三件事", "下一阶段：计划与资源"}},
				{Title: "三个要点", Kicker: "POINTS", Bullets: []string{"清楚：一页只讲一个观点", "节奏：从封面到收束一条线", "配色：让风格语言自己说话"}},
				{Title: "增长", Kicker: "SECTION", Layout: "section"},
				{Title: "核心指标", Kicker: "KPI", Layout: "kpi", Bullets: []string{"12.6亿 | 全年营收", "87% | 目标达成率", "3.2x | 同比增速", "42天 | 平均回款"}},
				{Title: "推进节奏", Kicker: "TIMELINE", Layout: "timeline", Bullets: []string{"第一阶段：试点验证", "第二阶段：区域推广", "第三阶段：全面铺开"}},
				{Title: "方案对比", Kicker: "COMPARE", Layout: "comparison", Bullets: []string{"自建团队：周期长、可控性强", "外部合作：上线快、依赖供方"}},
				{Title: "一句话", Kicker: "QUOTE", Layout: "quote", Bullets: []string{"把复杂留给系统，把清晰交给用户。"}},
				{Title: "谢谢", Kicker: "THANKS", Layout: "closing", Bullets: []string{"欢迎讨论"}},
			},
		}
		if err := WriteFile(path, outline); err != nil {
			t.Fatalf("%s: %v", spec.id, err)
		}
		result, err := RenderPreview(path, PreviewOptions{OutputDir: filepath.Join(dir, "png"), Width: 1280, MaxSlides: 10})
		if err != nil {
			t.Fatalf("%s: preview: %v", spec.id, err)
		}
		if len(result.MissingFonts) > 0 {
			fmt.Printf("[%s] missing fonts: %v\n", spec.id, result.MissingFonts)
		}
	}
}
