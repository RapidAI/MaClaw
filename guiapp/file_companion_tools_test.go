package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func seedCompanionTestFile(t *testing.T, app *App, name, body string) (string, FileCompanionDocument) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.SessionID == "" {
		t.Fatalf("open %s produced no session id", name)
	}
	return path, doc
}

func TestFileCompanionClosedSurfaceToolsOnlyForPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, pdfDoc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + pdfDoc.SessionID
	tools := h.fileCompanionClosedSurfaceTools(userID)
	want := []string{fileCompanionToolReadPages, fileCompanionToolRenderPage, fileCompanionToolExtractImgs}
	if got := agentLoopToolNamesForLog(tools); len(got) != len(want) {
		t.Fatalf("pdf surface tools = %q, want %q", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("pdf surface tool %d = %q, want %q", i, got[i], want[i])
			}
		}
	}
	_, mdDoc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	if got := h.fileCompanionClosedSurfaceTools("file-companion:" + mdDoc.SessionID); len(got) != 0 {
		t.Fatalf("markdown surface tools = %q, want none", agentLoopToolNamesForLog(got))
	}
}

func TestFileCompanionHostToolReadPagesReportsTheDocument(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadPages, `{"pages":"1"}`)
	if !ok || !strings.Contains(text, "共 1 页") {
		t.Fatalf("read_pages = %q ok=%v", text, ok)
	}
	if out, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadPages, `{"pages":"9"}`); ok || !strings.Contains(out, "共 1 页") {
		t.Fatalf("read_pages out of range = %q ok=%v", out, ok)
	}
}

func TestFileCompanionHostToolExtractImagesRegistersOneFigure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolExtractImgs, `{"page":1}`)
	if !ok || !strings.Contains(text, "图1") {
		t.Fatalf("extract page 1 = %q ok=%v", text, ok)
	}
	figures := app.fileCompanionTurnFigures(userID)
	if len(figures) != 1 {
		t.Fatalf("registered figures = %d, want 1", len(figures))
	}
	if _, err := os.Stat(figures[0].path); err != nil {
		t.Fatalf("extracted figure missing: %v", err)
	}
	// The same page again neither rewrites the file nor earns a second number:
	// one drawing stays one drawing, or the reply shows it twice.
	before := figures[0].path
	again, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolExtractImgs, `{"page":1}`)
	if !ok {
		t.Fatalf("second extract of the same page failed: %s", again)
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != 1 || got[0].path != before {
		t.Fatalf("repeat extract paths = %q, want the one %s", companionFigurePaths(got), before)
	}
	if out, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolExtractImgs, `{"page":9}`); ok || !strings.Contains(out, "共 1 页") {
		t.Fatalf("extract out of range = %q ok=%v", out, ok)
	}
}

func TestFileCompanionHostToolRenderReusesThisTurnsFigure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := app.fileCompanionToolFigurePath(canonical, "render-p1-d140.png")
	if err != nil {
		t.Fatal(err)
	}
	writeCompanionFigurePNG(t, rendered, 200, 150)
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: rendered, caption: "第 1 页整页渲染", size: 1024}})
	h := &IMMessageHandler{app: app}
	// The page is already on the list, so this costs no render at all: the
	// test passes on a machine with no PyMuPDF and no poppler.
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRenderPage, `{"page":1}`)
	if !ok || !strings.Contains(text, "图1") || !strings.Contains(text, "已经在本轮渲染过") {
		t.Fatalf("repeat render = %q ok=%v", text, ok)
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != 1 {
		t.Fatalf("repeat render added figures: %q", companionFigurePaths(got))
	}
	if out, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRenderPage, `{"page":9}`); ok || !strings.Contains(out, "共 1 页") {
		t.Fatalf("render out of range = %q ok=%v", out, ok)
	}
	// A later turn asking for the same page reuses the file already on disk:
	// the document has not changed since it was rendered.
	app.seedFileCompanionTurnFigures(userID, nil)
	text, ok = h.executeFileCompanionHostTool(userID, fileCompanionToolRenderPage, `{"page":1}`)
	if !ok || !strings.Contains(text, "已复用") || !strings.Contains(text, "图1") {
		t.Fatalf("cross-turn render = %q ok=%v", text, ok)
	}
}

func companionFigurePaths(figures []companionFigure) []string {
	paths := make([]string, 0, len(figures))
	for _, figure := range figures {
		paths = append(paths, figure.path)
	}
	return paths
}

func seedCompanionTestFileAt(t *testing.T, app *App, path string) (string, FileCompanionDocument) {
	t.Helper()
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.SessionID == "" {
		t.Fatalf("open %s produced no session id", path)
	}
	return path, doc
}

// A companion question is often short enough to be routed onto the light
// prompt, and the light allowlist has never heard of these host-owned names.
// The authorizer must answer from the closed surface, or the model is shown a
// tool it is then refused - promised, then denied, with no figure.
func TestFileCompanionAuthorizerAdmitsTheClosedSurfaceOnLightProfile(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		userID:  "file-companion:s1",
		handler: &IMMessageHandler{},
		loopCtx: &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{
			Layer:         string(executionLayerLight),
			PromptProfile: "light",
		}}},
	}
	light := agent.PromptProfileLight
	for _, name := range []string{fileCompanionToolReadPages, fileCompanionToolRenderPage, fileCompanionToolExtractImgs} {
		if !cb.IsToolAllowed(name) {
			t.Fatalf("light turn refuses %s", name)
		}
		if !cb.IsToolAllowedForPromptProfile(name, light) {
			t.Fatalf("light profile refuses %s", name)
		}
		if allowed, _ := cb.IsToolCallAllowed(name, `{}`); !allowed {
			t.Fatalf("intake refuses %s", name)
		}
	}
	for _, name := range []string{"write_file", "bash", "read_file"} {
		if cb.IsToolAllowed(name) || cb.IsToolAllowedForPromptProfile(name, light) {
			t.Fatalf("companion turn admits %s", name)
		}
	}
}

// The end of the wiring that matters: a call the model actually makes has to
// reach the host executor. Every gate between the rendered surface and this
// result (turn denials, legacy petitions, the light allowlist) runs first.
func TestFileCompanionToolCallReachesTheHostExecutor(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		userID:  userID,
		handler: h,
		tools:   h.fileCompanionClosedSurfaceTools(userID),
		loopCtx: &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{
			Layer:         string(executionLayerLight),
			PromptProfile: "light",
		}}},
	}
	progress := ""
	cb.onProgress = func(text string) { progress = text }
	result := cb.executeToolCallWithExecutionContext(fileCompanionToolExtractImgs, `{"page":1}`, "call-1", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(result.Result, "图1") {
		t.Fatalf("tool call result = %+v", result)
	}
	if progress == "" {
		t.Fatal("tool call emitted no progress")
	}
	if len(app.fileCompanionTurnFigures(userID)) != 1 {
		t.Fatalf("figures after call = %d, want 1", len(app.fileCompanionTurnFigures(userID)))
	}
}

func TestFileCompanionClosedSurfaceToolsWithoutApp(t *testing.T) {
	h := &IMMessageHandler{}
	if got := h.fileCompanionClosedSurfaceTools("file-companion:s1"); got != nil {
		t.Fatalf("handler without app returned %q", agentLoopToolNamesForLog(got))
	}
}

func TestFileCompanionHostToolRejectsUnknownSession(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:missing", fileCompanionToolRenderPage, `{"page":1}`)
	if ok {
		t.Fatalf("unknown session executed: %q", text)
	}
	if !strings.Contains(text, "没有已打开的文档") {
		t.Fatalf("unknown session text = %q", text)
	}
}

func TestFileCompanionHostToolRejectsNonPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "notes.md", "# 笔记\n")
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadPages, `{"pages":"1"}`)
	if ok || !strings.Contains(text, "只支持 PDF") {
		t.Fatalf("markdown read_pages = %q ok=%v", text, ok)
	}
}

func TestFileCompanionHostToolReadPagesUnparseablePDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "fake.pdf", "%PDF-1.4\n")
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadPages, `{"pages":"1"}`)
	if ok || !strings.Contains(text, "无法解析") {
		t.Fatalf("fake pdf read_pages = %q ok=%v", text, ok)
	}
}

func TestFileCompanionParsePageSpec(t *testing.T) {
	cases := []struct {
		spec string
		want []int
	}{
		{"7", []int{7}},
		{"3,5", []int{3, 5}},
		{"3, 5", []int{3, 5}},
		{"7-9", []int{7, 8, 9}},
		{"3,5,7-9,5", []int{3, 5, 7, 8, 9}},
	}
	for _, tc := range cases {
		got, err := fileCompanionParsePageSpec(tc.spec)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.spec, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("parse %q = %v, want %v", tc.spec, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("parse %q = %v, want %v", tc.spec, got, tc.want)
			}
		}
	}
	for _, spec := range []string{"", "0", "-3", "abc", "5,x"} {
		if _, err := fileCompanionParsePageSpec(spec); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", spec)
		}
	}
}

func TestFileCompanionArgsAcceptQuotedAndBareNumbers(t *testing.T) {
	got, err := fileCompanionIntArg(map[string]interface{}{"page": "7"}, "page")
	if err != nil || got != 7 {
		t.Fatalf(`page "7" = %d, %v`, got, err)
	}
	if _, err := fileCompanionIntArg(map[string]interface{}{"page": "七"}, "page"); err == nil {
		t.Fatal("non-numeric page accepted")
	}
	spec, err := fileCompanionPagesArg(map[string]interface{}{"pages": float64(7)})
	if err != nil || spec != "7" {
		t.Fatalf("numeric pages = %q, %v", spec, err)
	}
	if spec, err := fileCompanionPagesArg(map[string]interface{}{"pages": "3,5"}); err != nil || spec != "3,5" {
		t.Fatalf("string pages = %q, %v", spec, err)
	}
	if _, err := fileCompanionPagesArg(map[string]interface{}{}); err == nil {
		t.Fatal("missing pages accepted")
	}
	if _, err := fileCompanionPagesArg(map[string]interface{}{"pages": map[string]interface{}{}}); err == nil {
		t.Fatal("object pages accepted")
	}
}

func TestFileCompanionIntArgRejectsFractionalAndZero(t *testing.T) {
	if _, err := fileCompanionIntArg(map[string]interface{}{"page": float64(2.5)}, "page"); err == nil {
		t.Fatal("fractional page accepted")
	}
	if _, err := fileCompanionIntArg(map[string]interface{}{"page": float64(0)}, "page"); err == nil {
		t.Fatal("zero page accepted")
	}
	got, err := fileCompanionIntArg(map[string]interface{}{"page": float64(7)}, "page")
	if err != nil || got != 7 {
		t.Fatalf("page 7 = %d, %v", got, err)
	}
	if _, err := fileCompanionIntArg(map[string]interface{}{}, "page"); err == nil {
		t.Fatal("missing page accepted")
	}
}

func TestFileCompanionTurnFigureNumberingContinuesAcrossToolCalls(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	userID := "file-companion:s1"
	app.seedFileCompanionTurnFigures(userID, []companionFigure{
		{path: "a.png", caption: "第 1 页", size: 10},
		{path: "b.png", caption: "第 2 页", size: 10},
	})
	first, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: "c.png", size: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if first != 3 {
		t.Fatalf("tool figure numbering = %d, want 3", first)
	}
	if got := len(app.fileCompanionTurnFigures(userID)); got != 3 {
		t.Fatalf("turn figures = %d, want 3", got)
	}
	// A new turn reseeds the list, so numbering restarts with the host figures.
	app.seedFileCompanionTurnFigures(userID, nil)
	if got := app.fileCompanionTurnFigures(userID); len(got) != 0 {
		t.Fatalf("reseeded turn figures = %d, want 0", len(got))
	}
}

// Numbering restarts every turn, but a bubble from two turns ago keeps its
// drawing: the display bridge answers from the session ledger, not from the
// live 图N list.
func TestFileCompanionFigureLedgerOutlivesTheTurn(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	rendered := filepath.Join(dir, "render-p7-d140.png")
	writeCompanionFigurePNG(t, rendered, 200, 150)
	userID := "file-companion:s1"
	if _, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: rendered, caption: "第 7 页整页渲染", size: 4096}}); err != nil {
		t.Fatal(err)
	}
	// A new turn reseeds the numbering list...
	app.seedFileCompanionTurnFigures(userID, nil)
	if len(app.fileCompanionTurnFigures(userID)) != 0 {
		t.Fatal("reseed kept the previous turn's numbering list")
	}
	// ...but the earlier bubble's image is still servable.
	if _, err := app.FileCompanionFigureDataURL(rendered); err != nil {
		t.Fatalf("earlier figure stopped being served: %v", err)
	}
	if err := app.fileCompanionDropChat(userID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionFigureDataURL(rendered); err == nil {
		t.Fatal("cleared chat still serves its figure")
	}
}

func TestFileCompanionFigureLedgerStaysBounded(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	userID := "file-companion:s1"
	dir := t.TempDir()
	for i := 0; i < fileCompanionToolLedgerCap+8; i++ {
		path := filepath.Join(dir, fmt.Sprintf("render-p%d-d140.png", i+1))
		writeCompanionFigurePNG(t, path, 8, 8)
		if _, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: path, size: 512}}); err != nil {
			t.Fatal(err)
		}
		app.seedFileCompanionTurnFigures(userID, nil)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	kept := len(st.figureLedger[userID])
	st.mu.Unlock()
	if kept != fileCompanionToolLedgerCap {
		t.Fatalf("ledger kept %d paths, want %d", kept, fileCompanionToolLedgerCap)
	}
	newest := filepath.Join(dir, fmt.Sprintf("render-p%d-d140.png", fileCompanionToolLedgerCap+8))
	if _, err := app.FileCompanionFigureDataURL(newest); err != nil {
		t.Fatalf("newest figure dropped: %v", err)
	}
	oldest := filepath.Join(dir, "render-p1-d140.png")
	if _, err := app.FileCompanionFigureDataURL(oldest); err == nil {
		t.Fatal("oldest figure survived the cap")
	}
}

func TestFileCompanionTurnFigureCapRejectsOverflow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	userID := "file-companion:s1"
	seed := make([]companionFigure, 0, fileCompanionToolFigureCap)
	for i := 0; i < fileCompanionToolFigureCap; i++ {
		seed = append(seed, companionFigure{path: fmt.Sprintf("p%d.png", i), size: 1})
	}
	app.seedFileCompanionTurnFigures(userID, seed)
	if _, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: "x.png", size: 1}}); err == nil {
		t.Fatal("overflowing append accepted")
	}
}

func TestFileCompanionAppendNeverNumbersTheSameFigureTwice(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	userID := "file-companion:s1"
	first, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: "page7.png", size: 10}})
	if err != nil || first != 1 {
		t.Fatalf("first append = %d, %v", first, err)
	}
	// A re-render or a re-extract of the same file must not earn a second
	// number, or the reply shows one drawing under two of them.
	again, err := app.appendFileCompanionTurnFigures(userID, []companionFigure{{path: "page7.png", size: 10}})
	if err != nil {
		t.Fatalf("repeat append refused: %v", err)
	}
	if again != first {
		t.Fatalf("repeat append = 图%d, want 图%d", again, first)
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != 1 {
		t.Fatalf("turn figures = %q, want one", companionFigurePaths(got))
	}
}

// A turn that is already at its figure cap must refuse before writing: files
// no bubble refers to would pile up in the session directory.
func TestFileCompanionExtractStopsAtTheTurnFigureCap(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID
	seed := make([]companionFigure, 0, fileCompanionToolFigureCap-1)
	for i := 0; i < fileCompanionToolFigureCap-1; i++ {
		seed = append(seed, companionFigure{path: filepath.Join(t.TempDir(), fmt.Sprintf("seeded-%d.png", i)), size: 1})
	}
	app.seedFileCompanionTurnFigures(userID, seed)
	h := &IMMessageHandler{app: app}
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolExtractImgs, `{"page":1}`); !ok {
		t.Fatal("extract with one slot left failed")
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != fileCompanionToolFigureCap {
		t.Fatalf("turn figures = %d, want the cap %d", len(got), fileCompanionToolFigureCap)
	}
	figuresDir := filepath.Join(app.GetDataDir(), "file-companion", "figures", doc.SessionID)
	before := countCompanionDirEntries(t, figuresDir)
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolExtractImgs, `{"page":1}`); ok {
		t.Fatal("extract at the cap succeeded")
	}
	if after := countCompanionDirEntries(t, figuresDir); after != before {
		t.Fatalf("refused extract wrote %d files, directory went from %d to %d", after-before, before, after)
	}
}

// A turn at its figure cap must not rasterize: the render costs seconds and
// its result could not be shown anyway.
func TestFileCompanionRenderRefusesAtTheTurnFigureCap(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "paper.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID
	seed := make([]companionFigure, 0, fileCompanionToolFigureCap)
	for i := 0; i < fileCompanionToolFigureCap; i++ {
		seed = append(seed, companionFigure{path: filepath.Join(t.TempDir(), fmt.Sprintf("seeded-%d.png", i)), size: 1})
	}
	app.seedFileCompanionTurnFigures(userID, seed)
	h := &IMMessageHandler{app: app}
	// No raster engine would be needed: the refusal comes first, so this stays
	// fast and passes on a machine without PyMuPDF or poppler.
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolRenderPage, `{"page":1}`)
	if ok {
		t.Fatalf("render at the cap accepted: %q", text)
	}
	if !strings.Contains(text, "无法继续添加") {
		t.Fatalf("render at the cap = %q", text)
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != fileCompanionToolFigureCap {
		t.Fatalf("turn figures = %d, want %d", len(got), fileCompanionToolFigureCap)
	}
}

// Pruning is disk housekeeping, not a license to pull the rug out from under a
// document that is still open: its bubbles are on screen.
func TestFileCompanionPruneKeepsOpenDocuments(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path, doc := seedCompanionTestFile(t, app, "open.pdf", "%PDF-1.4\n")
	root := filepath.Join(app.GetDataDir(), "file-companion", "figures")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := func(dir string, at time.Time) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
	}
	// The open document's directory is the oldest one in the whole set.
	long := time.Now().Add(-100 * time.Hour)
	stamp(filepath.Join(root, doc.SessionID), long)
	for i := 0; i < fileCompanionFigureDirCap+1; i++ {
		stamp(filepath.Join(root, fmt.Sprintf("fc_%02d", i)), long.Add(time.Duration(i+1)*time.Hour))
	}
	// A tool product for some other, unopened file is what triggers the sweep.
	other := filepath.Join(t.TempDir(), "elsewhere.pdf")
	if _, err := app.fileCompanionToolFigurePath(other, "render-p1-d140.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, doc.SessionID)); err != nil {
		t.Fatalf("open document's figures pruned while %s is open: %v", path, err)
	}
}

// One directory per document ever opened would grow without end; the oldest
// go, and the ledger stops vouching for their files.
func TestFileCompanionFigureDirsStayBounded(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	root := filepath.Join(app.GetDataDir(), "file-companion", "figures")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, fileCompanionFigureDirCap+3)
	stamp := time.Now().Add(-time.Hour)
	for i := 0; i < fileCompanionFigureDirCap+3; i++ {
		name := fmt.Sprintf("fc_%02d", i)
		names = append(names, name)
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Newest last: modification time decides what is kept.
		at := stamp.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
	}
	oldest := names[0]
	kept := names[len(names)-1]
	if _, err := app.appendFileCompanionTurnFigures("file-companion:"+oldest, []companionFigure{{path: filepath.Join(root, oldest, "p.png"), size: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.appendFileCompanionTurnFigures("file-companion:"+kept, []companionFigure{{path: filepath.Join(root, kept, "p.png"), size: 1}}); err != nil {
		t.Fatal(err)
	}
	path, _ := seedCompanionTestFile(t, app, "doc.pdf", "%PDF-1.4\n")
	if _, err := app.fileCompanionToolFigurePath(path, "render-p1-d140.png"); err != nil {
		t.Fatal(err)
	}
	if countCompanionDirEntries(t, root) != fileCompanionFigureDirCap {
		t.Fatalf("figure dirs = %d, want %d", countCompanionDirEntries(t, root), fileCompanionFigureDirCap)
	}
	if _, err := os.Stat(filepath.Join(root, oldest)); err == nil {
		t.Fatal("oldest figure directory survived the cap")
	}
	if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
		t.Fatalf("recent figure directory pruned: %v", err)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	_, droppedKept := st.figureLedger["file-companion:"+oldest]
	_, keptKept := st.figureLedger["file-companion:"+kept]
	st.mu.Unlock()
	if droppedKept {
		t.Fatal("pruned session kept its ledger entry")
	}
	if !keptKept {
		t.Fatal("live session lost its ledger entry")
	}
}

func countCompanionDirEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestFileCompanionFigureDataURLServesToolFigures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	rendered := filepath.Join(dir, "render-p7.png")
	writeCompanionFigurePNG(t, rendered, 200, 150)
	userID := "file-companion:s1"
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: rendered, caption: "第 7 页整页渲染", size: 1024}})
	data, err := app.FileCompanionFigureDataURL(rendered)
	if err != nil {
		t.Fatalf("tool figure refused: %v", err)
	}
	if !strings.HasPrefix(data, "data:image/png;base64,") {
		t.Fatalf("data url prefix = %q", data[:32])
	}
	if _, err := app.FileCompanionFigureDataURL(filepath.Join(dir, "elsewhere.png")); err == nil {
		t.Fatal("unlisted path served")
	}
}

func TestFileCompanionReplyMatchesToolRenderedFigure(t *testing.T) {
	figures := []companionFigure{{path: "render.png", caption: "第 7 页整页渲染"}}
	got := fileCompanionFiguresForReply(figures, "展示 主要架构图", "这是整体架构图，见图1。")
	if len(got) != 1 || got[0].Index != 1 || got[0].Label != "图1" {
		t.Fatalf("reply figures = %#v", got)
	}
	if out := fileCompanionFiguresForReply(figures, "展示 主要架构图", "架构图在第 7 页。"); len(out) != 0 {
		t.Fatalf("unreferenced reply matched %#v", out)
	}
}

func TestFileCompanionPromptDescribesPDFTools(t *testing.T) {
	pdf := fileCompanionPrompt("报告.pdf", ".pdf")
	if !strings.Contains(pdf, fileCompanionToolRenderPage) || !strings.Contains(pdf, "图N") {
		t.Fatalf("pdf prompt lost the tool contract:\n%s", pdf)
	}
	if !strings.Contains(pdf, "不要声称已经生成或发送了 PDF") {
		t.Fatalf("pdf prompt lost the host-PDF ban:\n%s", pdf)
	}
	if strings.Contains(pdf, "不要使用工具") {
		t.Fatalf("pdf prompt forbids the tools it names:\n%s", pdf)
	}
	for _, ext := range []string{".md", ".docx"} {
		if plain := fileCompanionPrompt("n"+ext, ext); !strings.Contains(plain, "不要使用工具") {
			t.Fatalf("%s prompt lost the no-tools rule:\n%s", ext, plain)
		}
	}
	if xlsx := fileCompanionPrompt("t.xlsx", ".xlsx"); !strings.Contains(xlsx, "sheets JSON") {
		t.Fatalf("xlsx prompt lost the office contract:\n%s", xlsx)
	}
	video := fileCompanionPrompt("clip.mp4", ".mp4")
	for _, want := range []string{"不要使用工具", "只有文件名", "没有画面", "用中文直接回答"} {
		if !strings.Contains(video, want) {
			t.Fatalf("video prompt missing %q:\n%s", want, video)
		}
	}
}

func TestFileCompanionHistoryRestoresToolFigures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	sessionID := "s1"
	userID := "file-companion:" + sessionID
	dir := t.TempDir()
	rendered := filepath.Join(dir, "render-p7.png")
	writeCompanionFigurePNG(t, rendered, 200, 150)
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "展示 主要架构图"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "整体架构图见图1。"})
	app.imHandler = &IMMessageHandler{app: app, memory: mem}
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: rendered, caption: "第 7 页整页渲染", size: 1024}})
	got := app.fileCompanionHistory(sessionID)
	if len(got) != 2 {
		t.Fatalf("history messages = %d, want 2", len(got))
	}
	if len(got[1].Figures) != 1 || got[1].Figures[0].Label != "图1" {
		t.Fatalf("restored figures = %#v", got[1].Figures)
	}
}

func TestFileCompanionDropChatForgetsTurnFigures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	userID := "file-companion:s1"
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: "a.png", size: 1}})
	if err := app.fileCompanionDropChat(userID, ""); err != nil {
		t.Fatal(err)
	}
	if got := app.fileCompanionTurnFigures(userID); len(got) != 0 {
		t.Fatalf("turn figures after clear = %d, want 0", len(got))
	}
}
