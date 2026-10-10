package docgen

import (
	"strings"
	"testing"
)

func TestColorfulPaperHTMLPaintsHeadingsLinksAndMark(t *testing.T) {
	html := markdownToHTMLMode("## 方法原理\n\n## 数据集\n\nhttps://example.com/cifar（可能）", true)
	if !strings.Contains(html, `color:#1d4ed8`) || !strings.Contains(html, `color:#0f766e`) {
		t.Fatalf("heading colors: %s", html)
	}
	if !strings.Contains(html, `<a href="https://example.com/cifar">https://example.com/cifar</a>`) {
		t.Fatalf("link: %s", html)
	}
	if !strings.Contains(html, `color:#c2410c`) || !strings.Contains(html, "（可能）") {
		t.Fatalf("mark: %s", html)
	}
	if strings.Contains(html, "background-color") {
		t.Fatalf("color depends on a background InsertHTMLBox does not paint: %s", html)
	}
	plain := markdownToHTML("## 数据集\n\nhttps://example.com/cifar（可能）")
	if strings.Contains(plain, "<a href") || strings.Contains(plain, "#1d4ed8") || strings.Contains(plain, "#c2410c") {
		t.Fatalf("default document was recolored: %s", plain)
	}
	if !strings.Contains(plain, "color:#1a1a2e") {
		t.Fatalf("default heading: %s", plain)
	}
}

func TestColorfulPaperLiftsFormulasAndColorsSections(t *testing.T) {
	md := "## 方法原理\n\n执行流程为： z_j = Sel_τ(q_j, S) → x_j = Asm_τ(z_j, q_j)。后面继续。\n\n图片不是图题。\n\n$$z_j = q_j$$\n\n## 方法本质\n\n使用 CNN 提取空间特征。\n\n图 1： Leakage regimes\n\nmemory_ehr 保持 q_j\n"
	html := markdownToHTMLMode(md, true)
	if !strings.Contains(html, `text-align:center; font-size:12pt; color:#6d28d9`) || !strings.Contains(html, "Sel<sub>τ</sub>") || !strings.Contains(html, "q<sub>j</sub>") {
		t.Fatalf("formula: %s", html)
	}
	if strings.Count(html, "Sel<sub>τ</sub>") != 1 || strings.Count(html, "执行流程为") != 1 {
		t.Fatalf("formula duplicated: %s", html)
	}
	if !strings.Contains(html, `color:#1e40af">执行流程为：</p>`) || !strings.Contains(html, `color:#1e40af">后面继续。</p>`) {
		t.Fatalf("prose color: %s", html)
	}
	if strings.Contains(html, `color:#6d28d9">执行`) || strings.Contains(html, "color:#6d28d9\">后面") {
		t.Fatalf("prose joined the formula: %s", html)
	}
	if !strings.Contains(html, `color:#0f766e">使用 CNN 提取空间特征。</p>`) {
		t.Fatalf("section color: %s", html)
	}
	if !strings.Contains(html, `color:#0f766e"><i>图 1： Leakage regimes</i></p>`) {
		t.Fatalf("caption: %s", html)
	}
	if strings.Contains(html, "<i>图片") {
		t.Fatalf("caption matched 图片: %s", html)
	}
	if strings.Contains(html, "memory_<sub>") || strings.Contains(html, "memory<sub>") || !strings.Contains(html, "memory_ehr") {
		t.Fatalf("identifier subscript: %s", html)
	}
	linked := markdownToHTMLMode("## 数据集\n\nhttps://example.com/cifar_10（可能）", true)
	if !strings.Contains(linked, `href="https://example.com/cifar_10"`) || strings.Contains(linked, "cifar<sub>") || strings.Contains(linked, "cifar_<sub>") {
		t.Fatalf("url subscript: %s", linked)
	}
	plain := markdownToHTML("执行流程为： z_j = Sel_τ(q_j, S) → x_j = Asm_τ(z_j, q_j)。后面继续。")
	if strings.Contains(plain, "#6d28d9") || strings.Contains(plain, "<sub>") || strings.Contains(plain, "#1e40af") {
		t.Fatalf("default document lifted the formula: %s", plain)
	}
	if !strings.Contains(plain, "q_j") || !strings.Contains(plain, "Sel_τ") {
		t.Fatalf("default text: %s", plain)
	}
	math := markdownToHTMLMode("$$Sel_{\\tau}(q_j) = x_1$$\n\nsoftmax_i 保持 memory_ehr", true)
	if !strings.Contains(math, "Sel<sub>τ</sub>") || !strings.Contains(math, "q<sub>j</sub>") || !strings.Contains(math, "x<sub>1</sub>") {
		t.Fatalf("display math subscript: %s", math)
	}
	if !strings.Contains(math, "softmax<sub>i</sub>") || !strings.Contains(math, "memory_ehr") {
		t.Fatalf("token subscript: %s", math)
	}
}

func TestColorfulPaperReadsLatexCommandsInProse(t *testing.T) {
	html := markdownToHTMLMode("## 方法原理\n\n执行流程为： z_j = Sel_{\\tau}(q_j) \\to x_1 = y_2。后面继续。", true)
	if !strings.Contains(html, `text-align:center; font-size:12pt; color:#6d28d9`) || !strings.Contains(html, "Sel<sub>τ</sub>") || !strings.Contains(html, "→") || !strings.Contains(html, "x<sub>1</sub>") {
		t.Fatalf("latex prose: %s", html)
	}
	if strings.Contains(html, `\tau`) || strings.Contains(html, `\to`) {
		t.Fatalf("command left in html: %s", html)
	}
	if !strings.Contains(html, `color:#1e40af">执行流程为：</p>`) || !strings.Contains(html, `color:#1e40af">后面继续。</p>`) {
		t.Fatalf("prose color: %s", html)
	}
	brace := markdownToHTMLMode("## 方法原理\n\n更新为 y = W_{\\theta}(x_{t})。", true)
	if !strings.Contains(brace, `color:#6d28d9`) || !strings.Contains(brace, "W<sub>θ</sub>") || !strings.Contains(brace, "x<sub>t</sub>") {
		t.Fatalf("brace formula: %s", brace)
	}
	if !strings.Contains(brace, `color:#1e40af">更新为</p>`) {
		t.Fatalf("brace prose: %s", brace)
	}
	arrow := markdownToHTMLMode("## 方法原理\n\n方向为 a_i \\leftarrow b_j = c_k。上界 d_i \\top e_j = f_k。", true)
	if !strings.Contains(arrow, "←") || !strings.Contains(arrow, "⊤") || strings.Contains(arrow, "≤ft") || strings.Contains(arrow, "→p") {
		t.Fatalf("prefix command: %s", arrow)
	}
	grouped := markdownToHTMLMode("## 方法原理\n\n分组 z_j = \\left(a_i + b_j\\right)。", true)
	if !strings.Contains(grouped, "a<sub>i</sub>") || strings.Contains(grouped, "≤ft") || strings.Contains(grouped, `\left`) {
		t.Fatalf("delimiter: %s", grouped)
	}
	kept := markdownToHTMLMode("## 方法原理\n\n代码 `\\tau` 保持原样。", true)
	if !strings.Contains(kept, `\tau`) || strings.Contains(kept, "τ") {
		t.Fatalf("code span: %s", kept)
	}
	plain := markdownToHTML("z_j = Sel_{\\tau}(q_j) \\to x_1 = y_2")
	if strings.Contains(plain, "<sub>") || strings.Contains(plain, "τ") || strings.Contains(plain, "→") || strings.Contains(plain, "#6d28d9") {
		t.Fatalf("default document rewrote latex: %s", plain)
	}
}

func TestColorfulSpecKeepsTheTargetAddressInThePDF(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("no cjk font")
	}
	data, err := gen.Generate(Spec{
		Title:     "论文解读",
		Subtitle:  "paper.pdf",
		Brand:     "MaClaw",
		Content:   "## 数据集\n\nhttps://example.com/cifar（可能）",
		Colorful:  true,
		PaperSize: "A4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Fatal("not a pdf")
	}
	if !strings.Contains(string(data), "https://example.com/cifar") {
		t.Fatal("pdf dropped the target address")
	}
}
