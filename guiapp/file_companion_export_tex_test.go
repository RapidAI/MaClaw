package guiapp

import (
	"os"
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
	if err == nil || !strings.Contains(err.Error(), "写入 PDF 失败") {
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
		`\href{https://example.com/a\_b}{文档}`,
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
}
