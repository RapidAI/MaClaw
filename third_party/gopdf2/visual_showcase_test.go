package gopdf

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestVisualShowcase generates a comprehensive multi-page PDF exercising the
// library's generation capabilities (html2pdf, formulas, tables, CJK,
// images) and renders every page to PNG for visual review.
//
// Output: test/out/showcase/*.pdf and page-*.png — reviewed by the visual
// judge agent, not asserted programmatically.
func TestVisualShowcase(t *testing.T) {
	outDir := filepath.Join("test", "out", "showcase")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("serif", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.AddTTFFont("serif-b", "test/res/LiberationSerif-Bold.ttf"); err != nil {
		// bold face optional
		t.Log("no bold face:", err)
	}

	// ---------------- Page 1: HTML rendering ----------------
	pdf.AddPage()
	if err := pdf.SetFont("serif", "", 11); err != nil {
		t.Fatal(err)
	}
	html := `<h1 style="color:#1a3c6e">Quarterly Report — Q3 2026</h1>
<p>Revenue grew <b>12.4%</b> quarter over quarter, driven by <i>subscription
upgrades</i> and improved retention. Operating costs held flat at
<span style="color:#8b0000">$2.1M</span>.</p>
<div style="background-color:#eef4ff; padding:8px; border-left:4px solid #1a3c6e">
<b>Key metrics</b>: NRR 118%, churn 1.8%, CAC payback 14 months —
the best figures since the company was founded in 2019.
</div>
<table border="1" width="100%" style="border-collapse:collapse">
<thead><tr><th style="background-color:#1a3c6e;color:#ffffff">Region</th><th style="background-color:#1a3c6e;color:#ffffff">Revenue</th><th style="background-color:#1a3c6e;color:#ffffff">Growth</th></tr></thead>
<tbody>
<tr><td>North America</td><td>$4.2M</td><td>+14%</td></tr>
<tr><td>Europe</td><td>$2.8M</td><td>+9%</td></tr>
<tr><td>Asia-Pacific</td><td>$1.6M</td><td>+21%</td></tr>
</tbody>
</table>
<p>Highlights from the field:</p>
<ul>
<li>Launched the analytics dashboard to <u>all</u> enterprise tiers</li>
<li>Signed three strategic partners &mdash; total pipeline &plusmn;$900K</li>
<li>Reduced median support response time to &lt; 2 hours</li>
</ul>
<blockquote style="border-left:3px solid #999; color:#555">“The dashboard
changed how our team works.” — customer survey, September 2026</blockquote>
<div style="border:1.5px solid #1a3c6e; padding:8px; background-color:#fffbe8">
<b>Next quarter</b>: launch the mobile companion app, expand the European
data center, and complete SOC 2 Type II certification.
</div>
<p><img src="test/res/gopher02.png" width="80" style="vertical-align:middle">
The mascot approves these numbers.</p>`
	if _, err := pdf.InsertHTMLPaginated(50, 50, 495, html, HTMLBoxOption{
		DefaultFontFamily: "serif",
		DefaultFontSize:   10.5,
		PageTop:           50,
		PageBottom:        792 - 50,
	}); err != nil {
		t.Fatalf("InsertHTMLPaginated: %v", err)
	}

	// ---------------- Page 2: formulas ----------------
	pdf.AddPage()
	if err := pdf.SetFont("serif", "", 11); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "Mathematical formulas rendered by DrawFormula:")
	formulas := []struct {
		y    float64
		tex  string
		note string
	}{
		{90, `E = mc^2`, "mass-energy equivalence"},
		{120, `x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`, "quadratic formula"},
		{155, `f(x) = \sum_{i=0}^{n} a_i x^i`, "polynomial via sum"},
		{185, `\binom{n}{k} = \frac{n!}{k!(n-k)!}`, "binomial coefficient"},
		{215, `e^{i\pi} + 1 = 0`, "Euler's identity"},
		{245, `\int_0^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}`, "Gaussian integral"},
	}
	for _, f := range formulas {
		if err := pdf.SetFont("serif", "", 10); err != nil {
			t.Fatal(err)
		}
		w, err := pdf.DrawFormula(60, f.y, f.tex, 13, nil)
		if err != nil {
			t.Errorf("DrawFormula(%q): %v", f.tex, err)
		}
		_ = w
		if err := pdf.SetFont("serif", "", 9); err != nil {
			t.Fatal(err)
		}
		pdf.SetXY(360, f.y-4)
		_ = pdf.Cell(nil, "— "+f.note)
	}

	// ---------------- Page 3: table generation API ----------------
	pdf.AddPage()
	if err := pdf.SetFont("serif", "", 10); err != nil {
		t.Fatal(err)
	}
	planets := [][]string{
		{"Mercury", "0.39 AU", "88 days"},
		{"Venus", "0.72 AU", "225 days"},
		{"Earth", "1.00 AU", "365 days"},
		{"Mars", "1.52 AU", "687 days"},
		{"Jupiter", "5.20 AU", "4,333 days"},
	}
	table := pdf.NewTableLayout(60, 60, 22, 40)
	table.AddColumn("Planet", 180, "left")
	table.AddColumn("Semi-major axis", 120, "center")
	table.AddColumn("Orbital period", 120, "right")
	for _, row := range planets {
		table.AddRow(row)
	}
	if err := table.DrawTable(); err != nil {
		t.Fatalf("table render: %v", err)
	}
	if err := pdf.SetFont("serif", "", 9); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(60, 260)
	_ = pdf.Cell(nil, "Orbital parameters of the inner solar system (table API).")

	// ---------------- Page 4: CJK text ----------------
	pdf.AddPage()
	// CJK glyphs need a CJK-capable font; try common system fonts so the
	// showcase still exercises CJK rendering on machines that have one.
	cjkFont := ""
	for _, cand := range []string{
		`C:\Windows\Fonts\Deng.ttf`,
		`C:\Windows\Fonts\NotoSansSC-VF.ttf`,
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\simhei.ttf`,
	} {
		if _, err := os.Stat(cand); err == nil {
			if err := pdf.AddTTFFont("cjk", cand); err == nil {
				cjkFont = "cjk"
			}
			break
		}
	}
	if cjkFont != "" {
		if err := pdf.SetFont("cjk", "", 11); err != nil {
			t.Fatal(err)
		}
	}
	cjkHTML := `<h2>中文字体渲染测试</h2>
<p>这段文字用于测试 CJK 字符的渲染与断行规则。中文排版遵循避头尾原则：
标点符号不应出现在行首，括号不应孤立在行尾。句子可以在任意汉字之间断开，
但不能把「的」字留在上一行行尾形成孤字。</p>
<p>English inline text mixes freely with 中文内容 in the same paragraph,
which exercises the fragment layout engine's CJK detection.</p>`
	if _, err := pdf.InsertHTMLPaginated(50, 60, 495, cjkHTML, HTMLBoxOption{
		DefaultFontFamily: cjkFont,
		DefaultFontSize:   11,
		PageTop:           50,
		PageBottom:        792 - 50,
	}); err != nil {
		t.Logf("CJK HTML: %v (expected if no CJK TTF present)", err)
	}

	out := filepath.Join(outDir, "showcase.pdf")
	if err := pdf.WritePdf(out); err != nil {
		t.Fatal(err)
	}

	// Render pages to PNG for visual review.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	n, err := GetSourcePDFPageCountFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	for p := 0; p < n; p++ {
		img, err := RenderPageToImage(data, p, RenderOption{DPI: 110})
		if err != nil {
			t.Errorf("render page %d: %v", p, err)
			continue
		}
		pngPath := filepath.Join(outDir, fmt.Sprintf("page-%d.png", p+1))
		f, err := os.Create(pngPath)
		if err != nil {
			t.Errorf("create %s: %v", pngPath, err)
			continue
		}
		if err := png.Encode(f, img); err != nil {
			t.Errorf("encode %s: %v", pngPath, err)
		}
		f.Close()
	}
	t.Logf("showcase written to %s (%d pages)", out, n)
}
