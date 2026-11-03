// Command htmldemo generates a showcase PDF exercising the HTML-to-PDF
// engine, tables, formulas, CJK and pagination — used for visual verification.
package main

import (
	"log"
	"strconv"
	"strings"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func fillRows() string {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(`<tr><td>r`)
		b.WriteString(strconv.Itoa(i + 5))
		b.WriteString(`</td><td>v`)
		b.WriteString(strconv.Itoa(i + 5))
		b.WriteString(`</td></tr>`)
	}
	return b.String()
}

func main() {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()

	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}
	if err := pdf.AddTTFFont("libbold", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}
	// register the CJK font up front so HTML font-family: cjk resolves during
	// the paginated render below
	cjkFont := `C:\Windows\Fonts\simhei.ttf`
	_ = pdf.AddTTFFont("cjk", cjkFont)

	html := `
<style>
	h1 { font-size: 22pt; text-align: center; color: #1a3c6e; }
	h2 { font-size: 15pt; color: #2c5f8a; }
	.lead { text-indent: 24pt; }
	.hl { background-color: #fff3b0; }
	.warn { color: #b00020; text-decoration: underline; }
	p { line-height: 1.4; margin-bottom: 6pt; }
</style>
<h1>GoPDF2 Feature Showcase</h1>
<p class="lead">This paragraph demonstrates <b>bold</b>, <i>italic</i>,
<u>underline</u>, <s>strikethrough</s>, <span class="hl">background highlight</span>,
<span class="warn">warnings</span>, subscripts H<sub>2</sub>O, superscripts
x<sup>2</sup>, and <a href="https://github.com">links</a> — all rendered from HTML.</p>
<p style="text-align: justify">Justified text spreads to fill the full column
width so that both margins align cleanly, exactly like professional
typesetting engines do with long paragraphs of body copy spread across
multiple visual lines of output in this demonstration document.</p>
<h2>Styled Table</h2>
<table border="1" width="100%">
	<caption>GDP Table</caption>
	<thead>
		<tr><th>Region</th><th>2023</th><th>2024</th><th>Growth</th></tr>
	</thead>
	<tbody>
		<tr><td>Asia-Pacific</td><td>31,200</td><td>32,850</td><td style="color: green">+5.3%</td></tr>
		<tr><td>North America</td><td>28,900</td><td>29,710</td><td style="color: green">+2.8%</td></tr>
		<tr><td rowspan="2">Europe</td><td colspan="2">58,100 combined</td><td>+1.9%</td></tr>
		<tr><td>Western</td><td>41,000</td><td>+1.7%</td></tr>
	</tbody>
</table>
<h2>Lists</h2>
<ol type="upper-alpha" start="2">
	<li>Ordered item with nested content:
		<ul><li>Nested bullet one</li><li>Nested bullet two</li></ul>
	</li>
	<li>Second ordered item</li>
</ol>
<hr>
<pre>func main() {
    fmt.Println("preformatted")   // whitespace preserved
}</pre>
<p>Entities: &copy; &mdash; &ldquo;quotes&rdquo; &alpha;&beta;&gamma; &le; &ne; &infin;</p>
<h2>Mathematics</h2>
<p>The famous identity: <b>E = mc<sup>2</sup></b> and the Gaussian integral
appear in the formula section below, followed by Greek letters and operators.</p>
<h2>Block Background</h2>
<div style="background-color: #e8f0ff; padding-bottom: 6pt">A block-level
background draws a full-width band behind every line — this text sits on a
light shade that wraps across lines, supports pagination, and never covers
the glyphs themselves.</div>
<p style="text-align: justify">Justified filler paragraph number one to push
subsequent tables past the page boundary while keeping every rendered line
aligned on both margins for the demonstration.</p>
<table border="1" width="100%">
	<thead><tr><th>ID</th><th>Description</th></tr></thead>
	<tbody>
		<tr><td rowspan="4">Span A</td><td>row a-1</td></tr>
		<tr><td>row a-2</td></tr>
		<tr><td>row a-3</td></tr>
		<tr><td>row a-4</td></tr>` +
		fillRows() + `
	</tbody>
</table>
<h2 style="font-family: cjk">中文排版</h2>
<p style="font-family: cjk">中文文本在没有空格的情况下也能正确换行，而且标点符号不会出现在行首，
这是排版引擎遵循避头尾规则的效果。长文本会自动换行并保持可读性。</p>
`

	_, err := pdf.InsertHTMLPaginated(50, 60, 495, html, gopdf.HTMLBoxOption{
		DefaultFontFamily:  "lib",
		DefaultFontSize:    11,
		BoldFontFamily:     "libbold",
		PageTop:            50,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// formulas on a fresh page
	pdf.AddPage()
	if err := pdf.SetFont("lib", "", 13); err != nil {
		log.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "Formula rendering (LaTeX-lite subset):")

	formulas := []string{
		`E = mc^2`,
		`\int_{-\infty}^{\infty} e^{-x^2} dx = \sqrt{\pi}`,
		`\sum_{k=0}^{n} \binom{n}{k} = 2^n`,
		`\frac{\partial^2 u}{\partial t^2} = c^2 \nabla^2 u`,
		`a^2 + b^2 = c^2 \quad (\triangle ABC)`,
	}
	y := 90.0
	for _, f := range formulas {
		h, err := pdf.DrawFormula(60, y, f, 14, nil)
		if err != nil {
			log.Fatal(err)
		}
		y += h + 34
	}

	// CJK page via system font when available
	pdf.AddPage()
	if err := pdf.SetFont("cjk", "", 14); err == nil {
		if _, err := pdf.InsertHTMLBox(50, 60, 495, 600,
			`<h2 style="color: #7a1f1f">中文与公式混排</h2>
			 <p>中文排版测试：质量守恒定律可以表示为
			 <b>E = mc<sup>2</sup></b>，并且当 x → ∞ 时极限存在。</p>
			 <p>表格中的中文：<table border="1"><tr><th>名称</th><th>数值</th></tr>
			 <tr><td>光速</td><td>299,792 km/s</td></tr></table></p>`,
			gopdf.HTMLBoxOption{DefaultFontFamily: "cjk", DefaultFontSize: 12}); err != nil {
			log.Fatal(err)
		}
	}

	if err := pdf.WritePdf("showcase.pdf"); err != nil {
		log.Fatal(err)
	}
	log.Printf("showcase.pdf written, %d pages", pdf.GetNumberOfPages())
}
