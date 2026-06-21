package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderVerify_RealWorldContent generates a PDF with content matching
// the user's screenshot to visually verify the rendering fixes.
// Run with: go test ./corelib/docgen/ -run TestRenderVerify -v
// Then open the generated PDF to visually inspect.
func TestRenderVerify_RealWorldContent(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	content := `## 一、一页结论摘要

### 市场格局总览

中国终端安全管理市场已进入"存量竞争 + 信创替代"双轮驱动阶段。以天擎为代表的综合型终端管理平台面临来自
**轻量专业派（火绒）**、**信创深耕派（北信源）**、**方案整合派（深信服）**、**生态巨头派（360安全）**
四类竞品的差异化竞争。

### 核心判断

| 维度 | 结论 |
|------|------|
| 功能完整性 | 天擎处于第一梯队，与深信服、360安全并列全面型方案，火绒功能精简但核心体验极佳，北信源信创适配最深 |
| 价格竞争力 | 天擎中端定位，火绒性价比最高（企业版免费试用+低价），深信服偏高端，360安全走SaaS低价规模化路线 |
| 渠道渗透力 | 天擎渠道覆盖中等，深信服渠道体系最强，北信源政企关系最深，火绒依赖口碑传播 |

### 一句话销售定位建议

>"比火绒更全面、比深信服更实惠、比北信源更开放、比360更专注企业服务"

## 二、竞品对比表

### 3.3 竞争优势矩阵

` + "```" + `
轻量/纯净 ←———————————————→ 全面/厚重
  |   |
低价 火绒(左上) 天擎(右上) 360安全(右上)
  |   |
中价 北信源(中) |
  |   |
高价 深信服(右下)
` + "```" + `

**天擎定位：**右上象限——全面功能覆盖 + 中等价格区间，与竞品形成差异化。

### 3.4 竞品克星话术

| 面对竞品 | 客户顾虑 | 我方应对话术 |
|----------|----------|-------------|
| 火绒 | "火绒轻量好用" | "火绒确实好，但如果您的企业需要USB管控、上网行为审计、DLP数据防泄密，天擎一套全搞定，省去部署多套系统的麻烦" |
| 北信源 | "北信源信创做得好" | "天擎信创适配同样完善，且我们在非信创场景功能更丰富、体验更好，一套系统统一管理全平台终端" |
| 深信服 | "深信服方案强" | "深信服能力确实强，但同级别功能下天擎价格低30-40%，且管理更轻便，不需要复杂的运维团队" |

## 四、下一步行动建议

### 4.1 短期（1-3个月）

- 针对火绒用户：突出 ` + "`" + `USB管控` + "`" + ` 和 ` + "`" + `DLP数据防泄密` + "`" + ` 差异化功能
- 针对深信服用户：准备 ` + "`" + `TCO对比表` + "`" + `，突出性价比优势
- 输出标准化的竞品应对话术手册
`

	spec := Spec{
		Title:       "天擎终端管理软件 竞品分析报告",
		ProjectName: "天擎终端管理软件 竞品分析报告",
		Content:     content,
	}

	data, err := gen.Generate(spec)
	if err != nil {
		t.Fatalf("PDF generation failed: %v", err)
	}
	if len(data) < 500 {
		t.Fatalf("PDF too small: %d bytes", len(data))
	}

	// Write to temp file for visual inspection
	outPath := filepath.Join(os.TempDir(), "docgen_render_verify.pdf")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		t.Fatalf("Failed to write PDF: %v", err)
	}
	t.Logf("PDF written to: %s (%d bytes) — open to visually verify rendering", outPath, len(data))

	// Verify the HTML intermediate output for correctness
	html := markdownToHTML(content)

	// Blockquote should have border-left styling, no raw ">"
	if !containsSubstring(html, "border-left") {
		t.Error("blockquote missing border-left style")
	}
	if containsSubstring(html, "&gt;&ldquo;") || containsSubstring(html, "&gt;\u201c\u6bd4") {
		t.Error("raw '>' character leaked into blockquote output")
	}

	// Code block should have monospace + background
	if !containsSubstring(html, "monospace") {
		t.Error("code block missing monospace style")
	}
	if !containsSubstring(html, "&nbsp;&nbsp;|") {
		t.Error("code block indentation not preserved")
	}

	// Inline code should render with monospace span
	if containsSubstring(html, "`USB管控`") || containsSubstring(html, "`DLP") {
		t.Error("raw backticks leaked into inline code output")
	}
	if !containsSubstring(html, "USB管控") {
		t.Error("inline code content missing")
	}

	// Bold should render
	if !containsSubstring(html, "<b>轻量专业派（火绒）</b>") {
		t.Error("bold text not rendered")
	}
	if !containsSubstring(html, "<b>天擎定位：</b>") {
		t.Error("bold key-value pattern not rendered")
	}

	// Table should render
	if !containsSubstring(html, "<table>") {
		t.Error("table not rendered")
	}
	if !containsSubstring(html, "<th>") {
		t.Error("table headers not rendered")
	}

	// No raw markdown markers should remain
	if containsSubstring(html, "```") {
		t.Error("raw fence markers leaked into output")
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && strings.Contains(s, sub)
}
