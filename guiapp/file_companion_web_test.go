package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestFileCompanionHostKeepsWritersParticipantNotesDoNot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := seedCompanionTestFileAt(t, app, path)
	hostID := "file-companion:" + doc.SessionID
	noteID := "file-companion:" + fileCompanionShareDiscussionSessionID("cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := app.ensureFileCompanionShareDiscussionDocument(strings.TrimPrefix(noteID, "file-companion:"), path); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	host := agentLoopToolNamesForLog(h.fileCompanionClosedSurfaceTools(hostID))
	note := agentLoopToolNamesForLog(h.fileCompanionClosedSurfaceTools(noteID))
	hostSet := strings.Join(host, ",")
	for _, name := range []string{fileCompanionToolWebSearch, fileCompanionToolWebFetch, fileCompanionToolReadDocument, fileCompanionToolCraft, fileCompanionToolReadPages, fileCompanionToolGenerate, fileCompanionToolRestore} {
		if !strings.Contains(hostSet, name) {
			t.Fatalf("host list lost %s: %q", name, host)
		}
	}
	noteSet := map[string]bool{}
	for _, name := range note {
		noteSet[name] = true
		if fileCompanionToolWritesFile(name) {
			t.Fatalf("participant note lists writer %s", name)
		}
	}
	for _, name := range []string{fileCompanionToolWebSearch, fileCompanionToolWebFetch, fileCompanionToolReadDocument, fileCompanionToolReadPages} {
		if !noteSet[name] {
			t.Fatalf("participant note lost %s: %q", name, note)
		}
	}
	for _, name := range []string{fileCompanionToolGenerate, fileCompanionToolRestore, fileCompanionToolCraft} {
		text, ok := h.executeFileCompanionHostTool(noteID, name, `{"markdown":"# 笔记\n"}`)
		if ok || !strings.Contains(text, "只能阅读") {
			t.Fatalf("participant %s = %q ok=%v", name, text, ok)
		}
	}
}

func TestFileCompanionWebLookupWorksForMarkdownAndDoesNotWrite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolWebSearch, `{}`)
	if ok || !strings.Contains(text, "query") || strings.Contains(text, "不是 PDF") {
		t.Fatalf("search on markdown = %q ok=%v", text, ok)
	}
	long, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolWebSearch, `{"query":"`+strings.Repeat("正", fileCompanionWebSearchMaxRunes+1)+`"}`)
	if ok || !strings.Contains(long, "过长") {
		t.Fatalf("long query = %q ok=%v", long, ok)
	}
	page, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolReadPages, `{"pages":"1"}`)
	if ok || !strings.Contains(page, "不是 PDF") {
		t.Fatalf("read pages on markdown = %q ok=%v", page, ok)
	}
	save := filepath.Join(t.TempDir(), "stolen.pdf")
	fetched, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolWebFetch, `{"url":"http://127.0.0.1/secret","save_path":"`+filepath.ToSlash(save)+`","headers":{"Cookie":"a=b"},"via_browser":true}`)
	if ok {
		t.Fatalf("private fetch succeeded: %s", fetched)
	}
	if _, err := os.Stat(save); !os.IsNotExist(err) {
		t.Fatalf("fetch wrote %s (%v)", save, err)
	}
	narrowed, errText := fileCompanionWebFetchArgs(map[string]interface{}{
		"url":         "https://example.com/paper",
		"save_path":   save,
		"max_chars":   999999,
		"via_browser": true,
	})
	if errText != "" {
		t.Fatal(errText)
	}
	if _, exists := narrowed["save_path"]; exists {
		t.Fatalf("save_path survived: %#v", narrowed)
	}
	if got := intArg(narrowed, "max_chars", 0); got != fileCompanionWebFetchMaxChars {
		t.Fatalf("max_chars = %d", got)
	}
	if _, errText = fileCompanionWebFetchArgs(map[string]interface{}{"url": "https://user:secret@example.com/paper"}); errText == "" || !strings.Contains(errText, "抓取失败") {
		t.Fatalf("credential url accepted: %q", errText)
	}
	binary := fileCompanionPresentWebFetch("URL: https://example.com/paper.pdf\n\n[二进制内容: application/pdf, 100 字节。如需下载请使用 save_path 参数]")
	if strings.Contains(binary, "save_path") || !strings.Contains(binary, "不能下载") || !strings.Contains(binary, "example.com/paper.pdf") {
		t.Fatalf("binary fetch = %q", binary)
	}
	sliced := fileCompanionPresentWebFetch("URL: https://example.com/paper.pdf\n\n容: application/pdf, 100 字节。如需下载请使用 save_path 参数]")
	if strings.Contains(sliced, "save_path") || !strings.Contains(sliced, "不能下载") || !strings.Contains(sliced, "example.com/paper.pdf") {
		t.Fatalf("sliced binary advisory = %q", sliced)
	}
	prose := fileCompanionPresentWebFetch("正文讨论了 save_path 参数该不该公开。")
	if prose != "正文讨论了 save_path 参数该不该公开。" {
		t.Fatalf("page text changed: %q", prose)
	}
	group := fileCompanionPresentWebFetch("抓取失败: 403（目标站点要求反爬或登录验证；蓝信群聊的公开网络权限不允许使用浏览器、会话或 Cookie，请改用无需登录的公开来源）")
	if strings.Contains(group, "蓝信") || strings.Contains(group, "群聊") || !strings.Contains(group, "伴读只能打开公开页面") {
		t.Fatalf("group wording = %q", group)
	}
	browser := fileCompanionPresentWebFetch("抓取失败: 请先用 browser 工具打开 https://example.com；仍失败则用 download_file(url, save_path, via_browser=true)")
	if strings.Contains(browser, "download_file") || strings.Contains(browser, "browser 工具") || strings.Contains(browser, "save_path") || !strings.Contains(browser, "不能下载") {
		t.Fatalf("browser advice = %q", browser)
	}
}

func TestFileCompanionLiteratureSearchKeepsAPIEngines(t *testing.T) {
	in := corelib.WebSearchStrategy{
		BrowserFallbackEnabled:    true,
		BrowserHumanAssistEnabled: true,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "tavily", Enabled: true, Transport: corelib.WebSearchTransportAPI},
			{ID: "browser", Enabled: true, Transport: corelib.WebSearchTransportBrowser},
		},
	}
	out := fileCompanionLiteratureSearchStrategy(in)
	if out.BrowserFallbackEnabled || out.BrowserHumanAssistEnabled {
		t.Fatal("browser fallback stayed on")
	}
	if !in.Engines[1].Enabled {
		t.Fatal("input strategy was mutated")
	}
	if !out.Engines[0].Enabled || out.Engines[1].Enabled {
		t.Fatalf("engines = %+v", out.Engines)
	}
}

func TestFileCompanionGenerateWorksForMarkdown(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 复习笔记\n\n第一章。","title":"复习笔记"}`)
	if !ok || !strings.Contains(text, "已生成 PDF") {
		t.Fatalf("generate on markdown = %q ok=%v", text, ok)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
}

func TestFileCompanionWebFetchDownloadsAndReadsAPaper(t *testing.T) {
	const page = "https://example.org/papers/attention.pdf"
	const sentence = "Attention is all you need"
	paper := companionTestTextPDF(t, sentence)
	dir := t.TempDir()
	fetches := 0
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if !opts.PublicNetworkOnly || opts.HubDownload != nil {
			t.Fatalf("paper fetch opts=%+v", opts)
		}
		if opts.SavePath == "" {
			return &websearch.FetchResult{URL: rawURL, ContentType: "text/html", Content: "<html><body>notes</body></html>"}, nil
		}
		fetches++
		if rawURL != page {
			t.Fatalf("fetched %s", rawURL)
		}
		if err := os.WriteFile(opts.SavePath, paper, 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{URL: rawURL, SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	})
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	st := app.companionRuntime()
	st.mu.Lock()
	st.uiReady = true
	st.mu.Unlock()
	_, doc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	stolen := filepath.Join(t.TempDir(), "stolen.pdf")
	first, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolWebFetch, `{"url":"`+page+`","save_path":"`+filepath.ToSlash(stolen)+`","max_chars":20}`)
	if !ok || !strings.Contains(first, "已下载论文并在伴读中打开") || strings.Contains(first, "save_path") || strings.Contains(first, "read_document(") {
		t.Fatalf("first read = %q ok=%v", first, ok)
	}
	if !strings.Contains(first, "# continue: web_fetch(") || !strings.Contains(first, "offset=") {
		t.Fatalf("continuation was not pointed at web_fetch:\n%s", first)
	}
	if _, err := os.Stat(stolen); !os.IsNotExist(err) {
		t.Fatalf("caller save_path was written: %v", err)
	}
	saved := filepath.Join(dir, companionPaperFileName(canonicalPaperURL(page)))
	if !fileIsPDF(saved) || !app.fileCompanionGranted(saved) {
		t.Fatalf("paper was not opened from %s", saved)
	}
	st.mu.Lock()
	opened := len(st.emitted)
	st.mu.Unlock()
	if opened != 1 {
		t.Fatalf("open events = %d", opened)
	}
	openedPath, openErr := app.fileCompanionOpenDocument(userID)
	if openErr != nil || !strings.HasSuffix(openedPath, "notes.md") {
		t.Fatalf("session moved to %s (%v)", openedPath, openErr)
	}
	second, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolWebFetch, `{"url":"`+page+`","offset":20}`)
	if !ok || fetches != 1 || !strings.Contains(first, "Attention") || !strings.Contains(second, "all you need") {
		t.Fatalf("second read fetches=%d ok=%v text=%q", fetches, ok, second)
	}
	st.mu.Lock()
	opened = len(st.emitted)
	st.mu.Unlock()
	if opened != 1 {
		t.Fatalf("continuation opened the paper again: %d", opened)
	}
	plain, pageOK, handled := h.fileCompanionFetchPaper("https://example.org/notes", nil)
	if !handled || !pageOK || !strings.Contains(plain, "notes") || fetches != 1 {
		t.Fatalf("html page refetched or dropped: %q ok=%v handled=%v fetches=%d", plain, pageOK, handled, fetches)
	}
}

func TestFileCompanionWebFetchRefusesAFakePaperAndAParticipantDownload(t *testing.T) {
	dir := t.TempDir()
	fetches := 0
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		fetches++
		if opts.SavePath == "" {
			return &websearch.FetchResult{URL: rawURL, ContentType: "text/html", Content: "<html>no pdf</html>"}, nil
		}
		if err := os.WriteFile(opts.SavePath, []byte("not a pdf"), 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{URL: rawURL, SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	})
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	h := &IMMessageHandler{app: app}
	hostID := "file-companion:" + doc.SessionID
	fake, ok := h.executeFileCompanionHostTool(hostID, fileCompanionToolWebFetch, `{"url":"https://example.org/not-paper.pdf"}`)
	if ok || !strings.Contains(fake, "不是 PDF") || fetches == 0 {
		t.Fatalf("fake pdf = %q ok=%v fetches=%d", fake, ok, fetches)
	}
	noteID := "file-companion:" + fileCompanionShareDiscussionSessionID("cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := app.ensureFileCompanionShareDiscussionDocument(strings.TrimPrefix(noteID, "file-companion:"), doc.Path); err != nil {
		t.Fatal(err)
	}
	before := fetches
	denied, ok := h.executeFileCompanionHostTool(noteID, fileCompanionToolWebFetch, `{"url":"https://example.org/secret.pdf"}`)
	if ok || fetches != before || !strings.Contains(denied, "不能下载") {
		t.Fatalf("participant download = %q ok=%v fetches=%d", denied, ok, fetches)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".pdf") && fileIsPDF(filepath.Join(dir, entry.Name())) {
			t.Fatalf("a paper file was kept: %s", entry.Name())
		}
	}
	desc := ""
	var props map[string]interface{}
	for _, tool := range fileCompanionWebToolDefinitions() {
		fn, _ := tool["function"].(map[string]interface{})
		if fn["name"] != fileCompanionToolWebFetch {
			continue
		}
		desc, _ = fn["description"].(string)
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ = params["properties"].(map[string]interface{})
	}
	if !strings.Contains(desc, "论文下载") || !strings.Contains(desc, "不能传 save_path") {
		t.Fatalf("tool description = %q", desc)
	}
	if _, exists := props["save_path"]; exists {
		t.Fatal("web_fetch advertises save_path")
	}
}

func companionTestTextPDF(t *testing.T, sentence string) []byte {
	t.Helper()
	font, err := os.ReadFile(filepath.Join("..", "third_party", "gopdf2", "test", "res", "LiberationSerif-Regular.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFontData("body", font); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("body", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(72, 72)
	if err := pdf.Cell(nil, sentence); err != nil {
		t.Fatal(err)
	}
	raw, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty pdf")
	}
	return raw
}
