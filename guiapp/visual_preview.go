package guiapp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// visualPreviewState is one agent turn's rendered-page check.
// Tool calls run one at a time, and the slot is cleared when that call returns.
type visualPreviewState struct {
	mu          sync.Mutex
	path        string
	projectPath string
	sessionID   string
	lastHTML    string
	browser     *browser.PrivateHeadless
}

func (s *visualPreviewState) note(abs, projectPath, sessionID string) {
	if s == nil {
		return
	}
	target, kind := classifyVisualPreviewTarget(abs, s.lastHTMLLocked())
	if target == "" || kind == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = target
	s.projectPath = strings.TrimSpace(projectPath)
	s.sessionID = strings.TrimSpace(sessionID)
	if kind == "html" {
		s.lastHTML = target
	}
}

func (s *visualPreviewState) lastHTMLLocked() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHTML
}

func (s *visualPreviewState) hasPath() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.path) != ""
}

func (s *visualPreviewState) take() (path, projectPath, sessionID string, headless *browser.PrivateHeadless) {
	if s == nil {
		return "", "", "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, projectPath, sessionID = s.path, s.projectPath, s.sessionID
	headless = s.browser
	s.path = ""
	s.projectPath = ""
	s.sessionID = ""
	return path, projectPath, sessionID, headless
}

func (s *visualPreviewState) keepBrowser(headless *browser.PrivateHeadless) {
	if s == nil || headless == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browser == nil {
		s.browser = headless
	}
}

func (s *visualPreviewState) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	headless := s.browser
	s.browser = nil
	s.mu.Unlock()
	if headless != nil {
		headless.Close()
	}
}

func classifyVisualPreviewTarget(abs, lastHTML string) (target, kind string) {
	abs = strings.TrimSpace(abs)
	if abs == "" {
		return "", ""
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm", ".xhtml", ".svg":
		return abs, "html"
	case ".css", ".js", ".mjs":
		index := filepath.Join(filepath.Dir(abs), "index.html")
		if info, err := os.Stat(index); err == nil && !info.IsDir() {
			return index, "html"
		}
		lastHTML = strings.TrimSpace(lastHTML)
		if lastHTML != "" && sameDir(filepath.Dir(lastHTML), filepath.Dir(abs)) {
			return lastHTML, "html"
		}
		return "", ""
	case ".pptx":
		return abs, "pptx"
	default:
		return "", ""
	}
}

func sameDir(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

func (s *visualPreviewState) finish(app *App, supportsVision bool, result agent.ToolExecutionResult) agent.ToolExecutionResult {
	path, projectPath, sessionID, headless := s.take()
	if path == "" {
		return result
	}
	emitVisualPreviewPane(app, path, projectPath, sessionID)
	if !supportsVision {
		result.Result = appendVisualPreviewNote(result.Result, "预览已在右侧打开。当前模型看不到画面，不要声称已经看过版式。")
		return result
	}
	note, images, next := renderVisualPreview(app, headless, path)
	s.keepBrowser(next)
	if note != "" {
		result.Result = appendVisualPreviewNote(result.Result, note)
	}
	if len(images) > 0 {
		result.ModelImages = append(result.ModelImages, images...)
	}
	return result
}

func appendVisualPreviewNote(body, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return note
	}
	return strings.TrimRight(body, "\n") + "\n" + note
}

func renderVisualPreview(app *App, headless *browser.PrivateHeadless, path string) (string, []agent.ToolModelImage, *browser.PrivateHeadless) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pptx":
		note, images := renderPPTXVisualPreview(path)
		return note, images, headless
	default:
		return renderHTMLVisualPreview(app, headless, path)
	}
}

func renderPPTXVisualPreview(path string) (string, []agent.ToolModelImage) {
	result, err := pptx.RenderPreview(path, pptx.PreviewOptions{
		Width:     pptx.DefaultPreviewWidth,
		MaxSlides: 3,
	})
	if err != nil || result == nil || len(result.Images) == 0 {
		if err == nil {
			err = fmt.Errorf("没有渲染出页面")
		}
		return fmt.Sprintf("没有预览图（%s）。写入已完成。不要声称已经看过版式。", err), nil
	}
	images := make([]agent.ToolModelImage, 0, len(result.Images))
	limit := len(result.Images)
	if limit > 3 {
		limit = 3
	}
	for _, imagePath := range result.Images[:limit] {
		data, err := os.ReadFile(imagePath)
		if err != nil || len(data) == 0 {
			continue
		}
		png, _, _ := prepareVisionScreenshot(base64.StdEncoding.EncodeToString(data))
		if strings.TrimSpace(png) == "" {
			continue
		}
		images = append(images, agent.ToolModelImage{
			MIME:    "image/png",
			Base64:  png,
			Purpose: agent.VisualPreviewImagePurpose,
		})
	}
	if len(images) == 0 {
		return "没有预览图（预览文件无法读取）。写入已完成。不要声称已经看过版式。", nil
	}
	end := len(images)
	note := fmt.Sprintf("预览图已附上：幻灯片 1-%d / %d。按图检查溢出、重叠、间距和配色。", end, result.SlideCount)
	if result.Truncated && result.NextOffset > 0 {
		note += fmt.Sprintf(" 后续页从 slide_offset=%d 查看。", result.NextOffset)
	}
	if len(result.MissingFonts) > 0 {
		note += " 缺字体: " + strings.Join(result.MissingFonts, ", ") + "。"
	}
	return note, images
}

func renderHTMLVisualPreview(app *App, headless *browser.PrivateHeadless, path string) (string, []agent.ToolModelImage, *browser.PrivateHeadless) {
	if app == nil {
		return "没有预览图（预览服务不可用）。写入已完成。不要声称已经看过版式。", nil, headless
	}
	info, err := app.OpenWebPreview(path)
	if err != nil || info == nil || strings.TrimSpace(info.EntryURL) == "" {
		if err == nil {
			err = fmt.Errorf("没有入口地址")
		}
		return fmt.Sprintf("没有预览图（%s）。写入已完成。不要声称已经看过版式。", err), nil, headless
	}
	defer app.CloseWebPreview(info.ID)
	if headless == nil {
		started, startErr := browser.StartPrivateHeadless()
		if startErr != nil {
			return fmt.Sprintf("没有预览图（%s）。写入已完成。不要声称已经看过版式。", startErr), nil, nil
		}
		headless = started
	}
	png, err := headless.ScreenshotURL(info.EntryURL)
	if err != nil || strings.TrimSpace(png) == "" {
		if err == nil {
			err = fmt.Errorf("空截图")
		}
		return fmt.Sprintf("没有预览图（%s）。写入已完成。不要声称已经看过版式。", err), nil, headless
	}
	png, _, _ = prepareVisionScreenshot(png)
	if strings.TrimSpace(png) == "" {
		return "没有预览图（截图无法缩放）。写入已完成。不要声称已经看过版式。", nil, headless
	}
	return "预览图已附上。按图检查布局、间距、配色和样式是否生效。", []agent.ToolModelImage{{
		MIME:    "image/png",
		Base64:  png,
		Purpose: agent.VisualPreviewImagePurpose,
	}}, headless
}

func emitVisualPreviewPane(app *App, abs, projectPath, sessionID string) {
	if app == nil || app.codeEventEmitter == nil || strings.TrimSpace(abs) == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	name := filepath.Base(abs)
	evt := CodeFileEvent{
		SessionID:   sessionID,
		FilePath:    name,
		FileName:    name,
		AbsPath:     abs,
		OpType:      "modify",
		ForceOpen:   true,
		ProjectPath: projectPath,
	}
	if ext == ".pptx" {
		evt.Content = ""
		evt.Language = "pptx"
		app.codeEventEmitter.EmitCodeFileEvent(evt)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil || len(data) > maxCodeFileSize || !isCodePreviewTextContent(data) {
		return
	}
	evt.Content = string(data)
	switch ext {
	case ".svg":
		evt.Language = "svg"
	default:
		evt.Language = "html"
	}
	app.codeEventEmitter.EmitCodeFileEvent(evt)
}

func legacyVisualPreviewPath(name, argsJSON string) string {
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write_file", "edit_file", "edit_lines":
		return firstVisualArg(args, "path", "file_path")
	case "office":
		action := strings.ToLower(strings.TrimSpace(firstVisualArg(args, "action")))
		switch action {
		case "write_pptx", "generate_pptx", "preview_pptx":
			return firstVisualArg(args, "file_path", "path")
		default:
			return ""
		}
	default:
		return ""
	}
}

func firstVisualArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if text, ok := args[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

const visualPreviewPromptSentence = "工具结果里附带了页面或幻灯片预览图时，按图改溢出、重叠、间距、配色和没生效的样式。没有图就不要说已经看过版式。"

func (c *sharedAgentLoopCallbacks) noteTrustedVisualPreview(path string) {
	if c == nil || c.handler == nil {
		return
	}
	workspace := trustedPrincipalBoundWorkspace(c.handler, c.semanticPrincipalID())
	abs, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return
	}
	c.noteVisualPreview(abs)
}

func (c *sharedAgentLoopCallbacks) noteVisualPreview(abs string) {
	if c == nil || strings.TrimSpace(abs) == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	owner := c.semanticPrincipalID()
	project := ""
	session := ""
	if c.handler != nil {
		project = localToolCodePreviewProjectPath(c.handler, owner)
		if route := codePreviewRouteProjectPath(owner, project); route != "" {
			project = route
		}
		session = localToolCodePreviewSessionID(owner)
	}
	c.visualPreview.note(abs, project, session)
}

func (c *sharedAgentLoopCallbacks) finishVisualPreview(name, argsJSON string, result agent.ToolExecutionResult) agent.ToolExecutionResult {
	if c == nil {
		return result
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	if result.Outcome == agent.ToolExecutionOutcomeOK && !c.visualPreview.hasPath() {
		if raw := legacyVisualPreviewPath(name, argsJSON); raw != "" && c.handler != nil {
			if abs, err := c.handler.resolveFileToolPathForOwner(raw, c.userID); err == nil {
				c.noteVisualPreview(abs)
			}
		}
	}
	var app *App
	if c.handler != nil {
		app = c.handler.app
	}
	return c.visualPreview.finish(app, c.llmCfg.SupportsVision, result)
}

func (c *sharedAgentLoopCallbacks) CloseVisualPreview() {
	if c == nil || c.visualPreview == nil {
		return
	}
	c.visualPreview.close()
}

func (c *codingSubAgentCallbacks) noteCodingVisualPreview(path string) {
	if c == nil {
		return
	}
	abs := codingVisualPreviewAbs(path, c.projectPath())
	if abs == "" {
		return
	}
	if c.visualPreview == nil {
		c.visualPreview = &visualPreviewState{}
	}
	c.visualPreview.note(abs, c.previewRouteProjectPath(), c.codeSessionID())
}

func (c *codingSubAgentCallbacks) finishCodingVisualPreview(result agent.ToolExecutionResult) agent.ToolExecutionResult {
	if c == nil || c.visualPreview == nil {
		return result
	}
	var app *App
	if c.subagent != nil && c.subagent.handler != nil {
		app = c.subagent.handler.app
	}
	return c.visualPreview.finish(app, c.GetLLMConfig().SupportsVision, result)
}

func (c *codingSubAgentCallbacks) CloseVisualPreview() {
	if c == nil || c.visualPreview == nil {
		return
	}
	c.visualPreview.close()
}

func codingVisualPreviewAbs(path, project string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		return abs
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return ""
	}
	abs, err := filepath.Abs(filepath.Join(project, path))
	if err != nil {
		return filepath.Join(project, filepath.Clean(path))
	}
	return abs
}
