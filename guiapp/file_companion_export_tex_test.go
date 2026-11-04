package guiapp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCompanionExportTeXWrapsAWriteFailure(t *testing.T) {
	app, path := newCompanionChatExportApp(t, filepath.Join(t.TempDir(), "unused"))
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := fileCompanionSaveTeXDialog
	t.Cleanup(func() { fileCompanionSaveTeXDialog = prev })
	fileCompanionSaveTeXDialog = func(*App, string) (string, error) {
		return filepath.Join(blocker, "对话.tex"), nil
	}
	_, err := app.FileCompanionExportChatTeX(path, []CompanionChatTurn{{Role: "user", Content: "提问"}})
	if err == nil || !strings.Contains(err.Error(), "写入 LaTeX 失败") {
		t.Fatalf("write: %v", err)
	}
}

func TestFileCompanionExportKeepsPDFAndWritesTeXFragment(t *testing.T) {
	app := newVideoNoteTestApp(t)
	path := writeMedia(t, app, "notes.md", []byte("# hello"))
	dir := t.TempDir()
	prevPDF := fileCompanionSavePDFDialog
	fileCompanionSavePDFDialog = func(*App, string) (string, error) {
		return filepath.Join(dir, "out.pdf"), nil
	}
	t.Cleanup(func() { fileCompanionSavePDFDialog = prevPDF })
	pdf, err := app.FileCompanionExportPDF(path, "正文 $x^2$\n")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "%PDF") {
		t.Fatalf("pdf head = %q", raw[:min(12, len(raw))])
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tex") {
			t.Fatalf("default PDF click wrote %s", entry.Name())
		}
	}

	markdown := "行内 $x^2$ 结束\n$$a$$\n识别不确定：E=mc^2\n![板书 12:05](C:\\frames\\725.jpg)\n"
	var texName string
	prevTeX := fileCompanionSaveTeXDialog
	fileCompanionSaveTeXDialog = func(_ *App, name string) (string, error) {
		texName = name
		return filepath.Join(dir, name), nil
	}
	t.Cleanup(func() { fileCompanionSaveTeXDialog = prevTeX })
	answer, err := app.FileCompanionExportTeX(path, markdown)
	if err != nil {
		t.Fatal(err)
	}
	if texName != "回答.tex" || !strings.HasSuffix(answer, "回答.tex") {
		t.Fatalf("answer name %s %s", texName, answer)
	}
	fragment, err := os.ReadFile(answer)
	if err != nil {
		t.Fatal(err)
	}
	text := string(fragment)
	if !strings.Contains(text, `\documentclass{ctexart}`) || !strings.Contains(text, `\begin{document}`) || !strings.Contains(text, `\end{document}`) {
		t.Fatalf("not a latex document:\n%s", text)
	}
	if strings.Contains(text, "![") || strings.Contains(text, "##") {
		t.Fatalf("markdown left in tex:\n%s", text)
	}
	if !strings.Contains(text, `\(x^2\)`) || !strings.Contains(text, `\[a\]`) {
		t.Fatalf("math = %s", text)
	}
	if !strings.Contains(text, `识别不确定：E=mc\textasciicircum{}2`) || strings.Contains(text, `\(E=mc^2\)`) {
		t.Fatalf("ocr line was reconstructed:\n%s", text)
	}
	if !strings.Contains(text, `\includegraphics`) || !strings.Contains(text, "C:/frames/725.jpg") || !strings.Contains(text, "板书 12:05") {
		t.Fatalf("picture line changed:\n%s", text)
	}

	chat, err := app.FileCompanionExportChatTeX(path, []CompanionChatTurn{{Role: "assistant", Content: "对话 $y$"}})
	if err != nil {
		t.Fatal(err)
	}
	if texName != "对话.tex" || !strings.HasSuffix(chat, "对话.tex") {
		t.Fatalf("chat name %s %s", texName, chat)
	}
	chatBody, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chatBody), `\documentclass{ctexart}`) || !strings.Contains(string(chatBody), `\(y\)`) || strings.Contains(string(chatBody), "##") {
		t.Fatalf("chat document = %s", chatBody)
	}
	if !strings.Contains(string(chatBody), `\subsection*{第 1 个回答}`) {
		t.Fatalf("chat heading = %s", chatBody)
	}

	fileCompanionSaveTeXDialog = func(*App, string) (string, error) { return path, nil }
	if _, err := app.FileCompanionExportTeX(path, "$x$"); err == nil || !strings.Contains(err.Error(), fileCompanionErrCannotOverwrite) {
		t.Fatalf("overwrite = %v", err)
	}
	if _, err := app.FileCompanionExportChatTeX(path, nil); err == nil || !strings.Contains(err.Error(), "没有可导出的内容") {
		t.Fatalf("empty chat = %v", err)
	}
}

func TestFileCompanionTeXKeepsADisplayFormulaBlock(t *testing.T) {
	in := "## 章节\n0:00–2:00 令 $x$ 成立\n$$\nE=mc^2\n$$\n$$a+b$$\n\\[\ny\n\\]\n$$E=mc^2\n$$\n$$\n\\begin{aligned}\na &= b\n\\end{aligned}\n$$\n识别不确定：$E=mc^2$\n![板书 12:05](C:\\frames\\725.jpg)\n$$\n## 要点\n结论\n"
	out := fileCompanionMarkdownToTeX(in)
	if !strings.Contains(out, "0:00–2:00 令 \\(x\\) 成立") {
		t.Fatalf("inline = %s", out)
	}
	if !strings.Contains(out, "\\[E=mc^2\\]") || strings.Contains(out, "$$") {
		t.Fatalf("block = %s", out)
	}
	if !strings.Contains(out, "\\[a+b\\]") {
		t.Fatalf("one line = %s", out)
	}
	if !strings.Contains(out, "\\[y\\]") || strings.Contains(out, "\\[\\[") {
		t.Fatalf("bracket = %s", out)
	}
	if !strings.Contains(out, "\\begin{aligned}\na &= b\n\\end{aligned}") {
		t.Fatalf("aligned = %s", out)
	}
	if !strings.Contains(out, `识别不确定：\$E=mc\textasciicircum{}2\$`) || strings.Contains(out, "\\(E=mc^2\\)") {
		t.Fatalf("ocr = %s", out)
	}
	if !strings.Contains(out, `\includegraphics`) || !strings.Contains(out, "C:/frames/725.jpg") || strings.Contains(out, "![") {
		t.Fatalf("picture = %s", out)
	}
	if strings.Contains(out, "\\[\\]") || !strings.Contains(out, `\documentclass{ctexart}`) || !strings.Contains(out, `\begin{document}`) || !strings.Contains(out, `\end{document}`) {
		t.Fatalf("wrapper = %s", out)
	}
	if !strings.Contains(out, `\subsection*{要点}`) || !strings.Contains(out, "结论") || strings.Contains(out, "## ") {
		t.Fatalf("heading swallowed = %s", out)
	}
}

func TestFileCompanionTeXConvertsMarkdownStructure(t *testing.T) {
	out := fileCompanionMarkdownToTeX("# 标题\n\n这是 **重点** 与 *斜体*，以及 `code`。\n\n- 一项\n- 第二项 $z$\n\n1. 有序\n\n见 [文档](https://example.com/a_b)\n\n> 引用\n\n---\n\n| 列 | 值 |\n| --- | --- |\n| 甲 | 100% |\n\n```\nfunc a() {}\n```\n")
	for _, want := range []string{
		`\section*{标题}`,
		`\textbf{重点}`,
		`\emph{斜体}`,
		`\texttt{code}`,
		`\begin{itemize}`,
		`\item 一项`,
		`\item 第二项 \(z\)`,
		`\begin{enumerate}`,
		`\item 有序`,
		`\href{https://example.com/a_b}{文档}`,
		`\begin{quote}`,
		"引用",
		`\rule{\linewidth}{0.4pt}`,
		`\begin{tabular}{ll}`,
		`100\%`,
		`\begin{verbatim}`,
		"func a() {}",
		`\end{document}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s\n%s", want, out)
		}
	}
	for _, gone := range []string{"# 标题", "**重点**", "*斜体*", "`code`", "- 一项", "[文档](https://example.com/a_b)", "| 列 | 值 |"} {
		if strings.Contains(out, gone) {
			t.Fatalf("markdown %q left in\n%s", gone, out)
		}
	}
}

func TestFileCompanionTeXKeepsATimedFormulaRow(t *testing.T) {
	out := fileCompanionMarkdownToTeX("0:00–2:00 分段\n$$\n0:00 & t_0\n$$\n12:05 下一句\n")
	if !strings.Contains(out, "\\[0:00 & t_0\\]") || strings.Contains(out, "$$") {
		t.Fatalf("row = %s", out)
	}
	if !strings.Contains(out, "0:00–2:00 分段") || !strings.Contains(out, "12:05 下一句") {
		t.Fatalf("chapters = %s", out)
	}
	plain := fileCompanionMarkdownToTeX("$$\n0:00 = t_0\n$$\n12:05 下一句\n")
	if !strings.Contains(plain, "\\[0:00 = t_0\\]") || strings.Contains(plain, "$$") || !strings.Contains(plain, "12:05 下一句") {
		t.Fatalf("symbolic = %s", plain)
	}
}

func TestFileCompanionTeXKeepsDisplayMathInsideASentence(t *testing.T) {
	out := fileCompanionMarkdownToTeX("0:00–2:00 令 $$E=mc^2$$ 成立\n$$a$$\n")
	if !strings.Contains(out, "令 \\(E=mc^2\\) 成立") || strings.Contains(out, "$$") {
		t.Fatalf("sentence = %s", out)
	}
	if !strings.Contains(out, "\\[a\\]") {
		t.Fatalf("whole line = %s", out)
	}
	brackets := fileCompanionMarkdownToTeX("令 \\[E=mc^2\\] 与 \\(a+b\\) 成立\n")
	if !strings.Contains(brackets, `\(E=mc^2\)`) || !strings.Contains(brackets, `\(a+b\)`) || strings.Contains(brackets, `\textbackslash`) {
		t.Fatalf("brackets = %s", brackets)
	}
	literal := fileCompanionMarkdownToTeX("价格 \\$100 与 50\\%\n")
	if strings.Contains(literal, `\textbackslash`) || !strings.Contains(literal, `\$100`) || !strings.Contains(literal, `50\%`) {
		t.Fatalf("literal = %s", literal)
	}
}

func TestFileCompanionTeXDoesNotSwallowTextAfterAnUnclosedFormula(t *testing.T) {
	out := fileCompanionMarkdownToTeX("$$\n后面的正文有 50% 把握\n")
	if strings.Contains(out, `\[后面`) || strings.Contains(out, "$$") {
		t.Fatalf("unclosed formula swallowed text:\n%s", out)
	}
	if !strings.Contains(out, "后面的正文") || !strings.Contains(out, `50\%`) {
		t.Fatalf("prose = %s", out)
	}
	math := fileCompanionMarkdownToTeX("$$\n50%\n$$\n比例 $a & b$\n")
	if !strings.Contains(math, `\[50\%\]`) || !strings.Contains(math, `\(a \& b\)`) {
		t.Fatalf("math specials = %s", math)
	}
	if !strings.Contains(math, "比例") {
		t.Fatalf("text after math commented out:\n%s", math)
	}
	prose := fileCompanionMarkdownToTeX("$$\nE=mc^2\n这是说明\n$$\n结论\n")
	if strings.Contains(prose, `\[这是说明\]`) || strings.Contains(prose, "$$") || !strings.Contains(prose, `\[E=mc^2\]`) {
		t.Fatalf("chinese inside math = %s", prose)
	}
	if !strings.Contains(prose, "这是说明") || !strings.Contains(prose, "结论") {
		t.Fatalf("chinese dropped = %s", prose)
	}
	kept := fileCompanionMarkdownToTeX("$$\n\\text{中文}\n$$\n")
	if !strings.Contains(kept, `\text{中文}`) {
		t.Fatalf("text command = %s", kept)
	}
	same := fileCompanionMarkdownToTeX("$$\nE=mc^2\n这是说明$$\n结论\n")
	if strings.Contains(same, `这是说明\]`) || strings.Contains(same, `\[这是说明`) || strings.Contains(same, "$$") || !strings.Contains(same, `\[E=mc^2\]`) {
		t.Fatalf("closer glued to prose = %s", same)
	}
	if !strings.Contains(same, "这是说明") || !strings.Contains(same, "结论") {
		t.Fatalf("glued prose dropped = %s", same)
	}
	after := fileCompanionMarkdownToTeX("$$\n说明\nE=mc^2\n$$\n结论\n")
	if strings.Contains(after, "$$") || !strings.Contains(after, `\[E=mc^2\]`) || !strings.Contains(after, "说明") || !strings.Contains(after, "结论") {
		t.Fatalf("formula after prose = %s", after)
	}
	open := fileCompanionMarkdownToTeX("$$\nE=mc^2\na + b\n")
	if !strings.Contains(open, `\[E=mc^2`) || !strings.Contains(open, `a + b\]`) || strings.Contains(open, "$$") || strings.Contains(open, `\textasciicircum`) {
		t.Fatalf("unclosed formula = %s", open)
	}
	sentence := fileCompanionMarkdownToTeX("$$\nE=mc^2\nmore english\n$$\n")
	if !strings.Contains(sentence, `\[E=mc^2\]`) || !strings.Contains(sentence, "more english") || strings.Contains(sentence, `\[more english\]`) {
		t.Fatalf("sentence inside math = %s", sentence)
	}
}

func TestFileCompanionTeXKeepsListedFormulasAndDisplayEnvs(t *testing.T) {
	listed := fileCompanionMarkdownToTeX("- $$E=mc^2$$\n- $$\nE=mc^2\n- $$\n* \\[y\\]\n1. \\[z\\]\n-1 不是公式\n")
	if strings.Count(listed, `\[E=mc^2\]`) != 2 || !strings.Contains(listed, `\[y\]`) || !strings.Contains(listed, `\[z\]`) {
		t.Fatalf("listed = %s", listed)
	}
	if strings.Contains(listed, "$$") || strings.Contains(listed, `\textbackslash`) || strings.Contains(listed, `\item`) {
		t.Fatalf("markers left = %s", listed)
	}
	if !strings.Contains(listed, "不是公式") {
		t.Fatalf("prose = %s", listed)
	}
	align := fileCompanionMarkdownToTeX("$$\n\\begin{align}\na &= b\n\\end{align}\n$$\n")
	if strings.Contains(align, `\[\begin{align}`) || !strings.Contains(align, `\begin{align}`) || !strings.Contains(align, `a &= b`) {
		t.Fatalf("align = %s", align)
	}
	aligned := fileCompanionMarkdownToTeX("$$\n\\begin{aligned}\na &= b\n\\end{aligned}\n$$\n")
	if !strings.Contains(aligned, `\[\begin{aligned}`) || !strings.Contains(aligned, `\end{aligned}`) {
		t.Fatalf("aligned = %s", aligned)
	}
	whole := fileCompanionMarkdownToTeX("$$这是说明$$\n")
	if strings.Contains(whole, `\[这是说明\]`) || !strings.Contains(whole, "这是说明") {
		t.Fatalf("whole prose = %s", whole)
	}
	commented := fileCompanionMarkdownToTeX("$$\n% 注释\n\\begin{align}\na &= b\n\\end{align}\n$$\n后文\n")
	if strings.Contains(commented, `\[\begin{align}`) || strings.Contains(commented, `\[\%`) || !strings.Contains(commented, `\begin{align}`) || !strings.Contains(commented, "后文") {
		t.Fatalf("align after comment = %s", commented)
	}
	dollar := fileCompanionMarkdownToTeX("$$\nE=$mc^2\n$$\n后文\n")
	if !strings.Contains(dollar, `\[E=\$mc^2\]`) || !strings.Contains(dollar, "后文") || strings.Contains(dollar, "$$") {
		t.Fatalf("dollar in math = %s", dollar)
	}
	before := fileCompanionMarkdownToTeX("$$\nE=mc^2\n\\begin{align}\na &= b\n\\end{align}\n$$\n")
	if strings.Contains(before, `\[\begin{align}`) || strings.Contains(before, "$$") || !strings.Contains(before, `\[E=mc^2\]`) || !strings.Contains(before, "\\begin{align}\na &= b\n\\end{align}") {
		t.Fatalf("formula before align = %s", before)
	}
	sentence := fileCompanionMarkdownToTeX("$$\nabout 50% of the sample\n$$\n")
	if strings.Contains(sentence, `\[about`) || !strings.Contains(sentence, `50\%`) || !strings.Contains(sentence, "about 50") {
		t.Fatalf("percent sentence = %s", sentence)
	}
}

func TestFileCompanionTeXDocumentCompiles(t *testing.T) {
	xelatex, err := exec.LookPath("xelatex")
	if err != nil {
		t.Skip("xelatex not installed")
	}
	src := strings.Join([]string{
		"# 标题",
		"",
		"第一句。",
		"第二句有 50% 把握。",
		"",
		"令 $x^2$ 与 $$E=mc^2$$ 成立。令 \\[a+b\\] 成立。",
		"",
		"$$",
		"E=$mc^2",
		"$$",
		"",
		"$$",
		"% 注释",
		`\begin{align}`,
		`u &= v`,
		`\end{align}`,
		"$$",
		"",
		"$$",
		"E=mc^2",
		`\begin{align}`,
		`p &= q`,
		`\end{align}`,
		"$$",
		"",
		"$$",
		"E=mc^2",
		"这是说明",
		"$$",
		"",
		"- $$",
		"a + b",
		"- $$",
		"",
		"$$",
		`\begin{align}`,
		`a &= b`,
		`\end{align}`,
		"$$",
		"",
		"$$",
		`\begin{aligned}`,
		`c &= d`,
		`\end{aligned}`,
		"$$",
		"",
		"见 [文档](https://example.com/a_b%20c)",
		"",
		"| 列 | 值 |",
		"| --- | --- |",
		"| 甲 | 100% |",
		"",
		"![图](C:/no such_file.png)",
		"",
		"```",
		"func a() {}",
		"```",
	}, "\n")
	pngLine := "\n\n![点](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==)\n"
	out, assets := fileCompanionRenderTeX(src + pngLine)
	if len(assets) != 1 || !strings.Contains(out, `\includegraphics`) {
		t.Fatalf("picture = %d\n%s", len(assets), out)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "note.tex")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if err := os.WriteFile(filepath.Join(dir, asset.name), asset.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(xelatex, "-interaction=nonstopmode", "-halt-on-error", "note.tex")
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xelatex: %v\n%s\n%s", err, raw, out)
	}
}

func TestFileCompanionTeXJoinsProseWithoutBreakingCJK(t *testing.T) {
	out := fileCompanionMarkdownToTeX("第一句。\n第二句。\n\nHello\nworld\n")
	if !strings.Contains(out, "第一句。%\n第二句。\n\nHello %\nworld") {
		t.Fatalf("paragraphs = %s", out)
	}
}

func TestFileCompanionTeXWritesDecodedFigures(t *testing.T) {
	app := newVideoNoteTestApp(t)
	path := writeMedia(t, app, "notes.md", []byte("x"))
	dir := t.TempDir()
	prev := fileCompanionSaveTeXDialog
	t.Cleanup(func() { fileCompanionSaveTeXDialog = prev })
	fileCompanionSaveTeXDialog = func(*App, string) (string, error) {
		return filepath.Join(dir, "回答.tex"), nil
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(png)
	sum := sha256.Sum256(png)
	name := "companion-" + hex.EncodeToString(sum[:8]) + ".png"
	dest, err := app.FileCompanionExportTeX(path, "见图\n\n![板书](data:image/png;base64,"+payload+")\n\n![再看](data:image/png;base64,"+payload+")\n")
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "data:image") || strings.Count(string(text), `\includegraphics[width=\linewidth]{`+name+`}`) != 2 {
		t.Fatalf("figure = %s", text)
	}
	fig, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(fig) != string(png) {
		t.Fatalf("png = %q", fig)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	pngs := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".png") {
			pngs++
		}
	}
	if pngs != 1 {
		t.Fatalf("png files = %d", pngs)
	}
	broken := fileCompanionMarkdownToTeX("![说明](data:image/png;base64,!!!!)\n")
	if strings.Contains(broken, "!!!!") || strings.Contains(broken, "data:image") || !strings.Contains(broken, "说明") {
		t.Fatalf("bad data url = %s", broken)
	}
	textish := fileCompanionMarkdownToTeX("![说明](data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes")) + ")\n")
	if strings.Contains(textish, `\includegraphics`) || strings.Contains(textish, "png-bytes") || !strings.Contains(textish, "说明") {
		t.Fatalf("not a picture = %s", textish)
	}
	spaced := fileCompanionMarkdownToTeX("![图](C:/my file.png)\n")
	if !strings.Contains(spaced, `\includegraphics[width=\linewidth]{C:/my file.png}`) || strings.Contains(spaced, `"C:/my file.png"`) {
		t.Fatalf("spaced path = %s", spaced)
	}
	unsafe := fileCompanionMarkdownToTeX("![图](C:/a%b.png)\n")
	if strings.Contains(unsafe, `\includegraphics`) || !strings.Contains(unsafe, `C:/a\%b.png`) || !strings.Contains(unsafe, "图") {
		t.Fatalf("percent path = %s", unsafe)
	}
	brace := fileCompanionMarkdownToTeX("![图](C:/a{b}.png)\n")
	if strings.Contains(brace, `\includegraphics`) || !strings.Contains(brace, `C:/a\{b\}.png`) || !strings.Contains(brace, "图") {
		t.Fatalf("brace path = %s", brace)
	}
	link := fileCompanionMarkdownToTeX("见 [文档](https://example.com/a{b})\n")
	if strings.Contains(link, "{b}") || !strings.Contains(link, `\href{https://example.com/ab}{文档}`) {
		t.Fatalf("brace url = %s", link)
	}
}
