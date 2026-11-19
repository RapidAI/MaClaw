package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

const (
	mcpBrowserTool       = "browser_test"
	mcpBrowserTextRunes  = 4000
	mcpBrowserPNGLimit   = 4 << 20
	mcpBrowserClosedText = "浏览器测试没有打开"
	mcpBrowserNoPageText = "还没有打开的任务页面"
)

// mcpBrowserDriveFunc replaces the page driver in tests. Production leaves it nil.
var mcpBrowserDriveFunc func(mcpBrowserDriveRequest) (mcpBrowserDriveResult, error)

type mcpBrowserDriveRequest struct {
	Action   string
	URL      string
	Selector string
	Text     string
	Key      string
	Expect   string
	StepID   string
}

type mcpBrowserDriveResult struct {
	OK     bool
	Detail string
	URL    string
	Title  string
	Text   string
	PNG    []byte
}

type mcpBrowserResult struct {
	ProjectPath      string        `json:"project_path"`
	Action           string        `json:"action"`
	StepID           string        `json:"step_id,omitempty"`
	OK               bool          `json:"ok"`
	Detail           string        `json:"detail,omitempty"`
	URL              string        `json:"url,omitempty"`
	Title            string        `json:"title,omitempty"`
	Text             string        `json:"text,omitempty"`
	Files            []mcpStepFile `json:"files,omitempty"`
	ScreenshotSHA256 string        `json:"screenshot_sha256,omitempty"`
	Encoding         string        `json:"encoding,omitempty"`
	Content          string        `json:"content,omitempty"`
}

func mcpBrowserMethods(available bool) []mcpBrowserMethod {
	one := func(name, action string, args ...string) mcpBrowserMethod {
		return mcpBrowserMethod{Name: name, Tool: mcpBrowserTool, Action: action, Available: available, Arguments: args}
	}
	return []mcpBrowserMethod{
		one("open", "open", "project_path", "url"),
		one("observe", "observe", "project_path"),
		one("click", "click", "project_path", "selector", "text"),
		one("key", "key", "project_path", "key"),
		one("screenshot", "screenshot", "project_path", "step_id"),
		one("assert", "assert", "project_path", "step_id", "expect"),
	}
}

func (a *App) mcpComputerUseEnabled() bool {
	if a == nil {
		return true
	}
	cfg := a.peekConfigOrEmpty()
	return computerUseEnabledFromConfig(&cfg)
}

func (a *App) mcpBrowserTest(ctx context.Context, projectPath, action, pageURL, selector, text, key, expect, stepID string, paths []string) (mcpBrowserResult, error) {
	_ = ctx
	action = strings.ToLower(strings.TrimSpace(action))
	if err := mcpBrowserArgsOK(action, pageURL, selector, text, key, expect, stepID); err != nil {
		return mcpBrowserResult{}, err
	}
	if a == nil || !a.mcpComputerUseEnabled() {
		return mcpBrowserResult{}, errors.New(mcpBrowserClosedText)
	}
	view, err := a.mcpBrowserTask(projectPath)
	if err != nil {
		return mcpBrowserResult{}, err
	}
	canonicalStep := ""
	if action == "screenshot" || action == "assert" || strings.TrimSpace(stepID) != "" {
		_, canonical, ok := parseCodingStepID(stepID)
		if ok {
			canonicalStep = canonical
		}
	}
	if (action == "screenshot" || action == "assert") && len(view.Steps) > 0 {
		if _, found := mcpStepByID(view.Steps, canonicalStep); !found {
			return mcpBrowserResult{}, errors.New("没有步骤 " + canonicalStep)
		}
	}
	if action == "open" {
		if err := a.mcpBrowserOpenAllowed(view, pageURL); err != nil {
			return mcpBrowserResult{}, err
		}
	}
	driven, err := mcpDriveBrowser(mcpBrowserDriveRequest{
		Action:   action,
		URL:      strings.TrimSpace(pageURL),
		Selector: strings.TrimSpace(selector),
		Text:     strings.TrimSpace(text),
		Key:      strings.TrimSpace(key),
		Expect:   expect,
		StepID:   canonicalStep,
	})
	if err != nil {
		return mcpBrowserResult{}, err
	}
	files := a.mcpEvidenceFiles(view, canonicalStep, paths)
	result := mcpBrowserResult{
		ProjectPath: view.ProjectPath,
		Action:      action,
		StepID:      canonicalStep,
		OK:          driven.OK,
		Detail:      strings.TrimSpace(driven.Detail),
		URL:         strings.TrimSpace(driven.URL),
		Title:       strings.TrimSpace(driven.Title),
		Text:        mcpClipRunes(driven.Text, mcpBrowserTextRunes),
		Files:       files,
	}
	if len(driven.PNG) > 0 {
		sum := sha256.Sum256(driven.PNG)
		result.ScreenshotSHA256 = hex.EncodeToString(sum[:])
		if len(driven.PNG) <= mcpBrowserPNGLimit {
			result.Encoding = "base64"
			result.Content = base64.StdEncoding.EncodeToString(driven.PNG)
		}
	}
	if canonicalStep != "" {
		a.mcpAppendCheck(view.ProjectPath, mcpCheckView{
			Action:           action,
			StepID:           canonicalStep,
			At:               mcpFormatTime(time.Now()),
			OK:               result.OK,
			Detail:           result.Detail,
			Files:            mcpCloneStepFiles(files),
			ScreenshotSHA256: result.ScreenshotSHA256,
		})
	}
	return result, nil
}

func mcpBrowserArgsOK(action, pageURL, selector, text, key, expect, stepID string) error {
	switch action {
	case "open":
		if strings.TrimSpace(pageURL) == "" {
			return errors.New("要指定要打开的地址")
		}
	case "observe":
	case "click":
		if strings.TrimSpace(selector) == "" && strings.TrimSpace(text) == "" {
			return errors.New("要点击的目标不能为空")
		}
	case "key":
		if strings.TrimSpace(key) == "" {
			return errors.New("要指定按键")
		}
	case "screenshot", "assert":
		if _, _, ok := parseCodingStepID(stepID); !ok {
			return errors.New("截图和断言要指定步骤编号")
		}
		if action == "assert" && strings.TrimSpace(expect) == "" {
			return errors.New("要指定断言内容")
		}
	default:
		return errors.New("浏览器动作无法识别")
	}
	return nil
}

func (a *App) mcpBrowserTask(projectPath string) (mcpTaskView, error) {
	path := normalizeProjectSessionPath(projectPath)
	if a == nil || path == "" {
		return mcpTaskView{}, errors.New("没有找到这个任务")
	}
	if view, ok := a.mcpViewFromRegistry(path); ok {
		if strings.TrimSpace(view.ProjectPath) == "" {
			view.ProjectPath = path
		}
		return view, nil
	}
	known, err := a.remoteControlKnownTask(projectPath)
	if err != nil {
		return mcpTaskView{}, err
	}
	return mcpTaskView{ProjectPath: known}, nil
}

func (a *App) mcpBrowserOpenAllowed(view mcpTaskView, raw string) error {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return errors.New("只能打开任务页面的地址")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	case "file":
		if mcpRemoteArtifactError(view) != nil {
			return mcpRemoteArtifactError(view)
		}
		root := mcpEvidenceRoot(view)
		if root == "" {
			return errors.New("任务还没有工作目录")
		}
		return mcpBrowserFileInWorkspace(root, parsed)
	default:
		return errors.New("只能打开任务页面的地址")
	}
}

func mcpBrowserFileInWorkspace(root string, parsed *url.URL) error {
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return errors.New("只能打开任务工作目录里的页面")
	}
	path := parsed.Path
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("只能打开任务工作目录里的页面")
	}
	rel := mcpStepRel(root, path)
	if rel == "" {
		return errors.New("只能打开任务工作目录里的页面")
	}
	if _, err := mcpResolveInside(root, rel); err != nil {
		return err
	}
	return nil
}

func mcpEvidenceRoot(view mcpTaskView) string {
	if mcpRemoteArtifactError(view) != nil {
		return ""
	}
	if root := mcpExistingDir(view.workspace); root != "" {
		return root
	}
	if root := mcpExistingDir(view.WorkingDir); root != "" {
		return root
	}
	return mcpExistingDir(view.ProjectPath)
}

func (a *App) mcpEvidenceFiles(view mcpTaskView, stepID string, extra []string) []mcpStepFile {
	var listed []mcpStepFile
	if step, ok := mcpStepByID(view.Steps, stepID); ok {
		listed = append(listed, step.Files...)
	}
	for _, path := range extra {
		path = strings.TrimSpace(path)
		if path != "" {
			listed = append(listed, mcpStepFile{Path: path})
		}
	}
	root := mcpEvidenceRoot(view)
	if root == "" {
		return mcpCloneStepFiles(listed)
	}
	return mcpHashStepFiles(root, listed)
}

func mcpStepByID(steps []mcpStepView, id string) (mcpStepView, bool) {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return mcpStepView{}, false
	}
	for _, step := range steps {
		if strings.EqualFold(strings.TrimSpace(step.ID), id) {
			return step, true
		}
	}
	return mcpStepView{}, false
}

func (a *App) mcpAppendCheck(path string, check mcpCheckView) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil {
		return
	}
	stored := check
	stored.Files = mcpCloneStepFiles(check.Files)
	rec.Checks = append(rec.Checks, stored)
	if strings.TrimSpace(check.StepID) == "" {
		return
	}
	for i := range rec.Steps {
		if strings.EqualFold(rec.Steps[i].ID, check.StepID) {
			rec.Steps[i].Checks = append(rec.Steps[i].Checks, stored)
		}
	}
}

func mcpClipRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

func mcpDriveBrowser(req mcpBrowserDriveRequest) (mcpBrowserDriveResult, error) {
	if mcpBrowserDriveFunc != nil {
		return mcpBrowserDriveFunc(req)
	}
	if testing.Testing() {
		return mcpBrowserDriveResult{}, errors.New(mcpBrowserNoPageText)
	}
	return mcpLiveBrowserDrive(req)
}

func mcpPickBrowserSession() *browser.BrowserAgentSession {
	sessions := browser.ListAgentSessions()
	for i := len(sessions) - 1; i >= 0; i-- {
		if sessions[i] != nil {
			return sessions[i]
		}
	}
	return nil
}

func mcpLiveBrowserDrive(req mcpBrowserDriveRequest) (mcpBrowserDriveResult, error) {
	sess := mcpPickBrowserSession()
	if req.Action == "open" && sess == nil {
		started, err := browser.StartAgentSessionForOwner("", "", browser.BrowserPolicy{
			AllowCrossOriginNavigation: true,
			ContentBoundary:            true,
		}, true, browser.SessionModePersistent)
		if err != nil {
			return mcpBrowserDriveResult{}, err
		}
		sess = started
	}
	if sess == nil {
		return mcpBrowserDriveResult{}, errors.New(mcpBrowserNoPageText)
	}
	switch req.Action {
	case "open":
		if err := sess.OpenURL(req.URL); err != nil {
			return mcpBrowserDriveResult{}, err
		}
		return mcpBrowserObserve(sess)
	case "observe":
		return mcpBrowserObserve(sess)
	case "click":
		var res *browser.BrowserActionResult
		var err error
		if req.Selector != "" {
			res, err = sess.Click("", "", req.Selector)
		} else {
			res, err = sess.ClickText("", req.Text)
		}
		return mcpBrowserFromAction(res, err)
	case "key":
		res, err := sess.Press(req.Key)
		return mcpBrowserFromAction(res, err)
	case "screenshot":
		png, err := sess.CaptureScreenshotPNG()
		if err != nil {
			return mcpBrowserDriveResult{}, err
		}
		out := mcpBrowserDriveResult{OK: true, PNG: png}
		if obs, obsErr := sess.Observe(false); obsErr == nil && obs != nil {
			out.URL = obs.Snapshot.URL
			out.Title = obs.Snapshot.Title
		}
		return out, nil
	case "assert":
		obs, err := sess.Observe(false)
		if err != nil {
			return mcpBrowserDriveResult{}, err
		}
		if obs == nil {
			return mcpBrowserDriveResult{}, errors.New("浏览器没有返回结果")
		}
		page := obs.Snapshot.PageTextExcerpt
		ok := strings.Contains(page, req.Expect)
		detail := "页面里没有这段结果"
		if ok {
			detail = "页面包含这段结果"
		}
		return mcpBrowserDriveResult{OK: ok, URL: obs.Snapshot.URL, Title: obs.Snapshot.Title, Text: page, Detail: detail}, nil
	default:
		return mcpBrowserDriveResult{}, errors.New("浏览器动作无法识别")
	}
}

func mcpBrowserObserve(sess *browser.BrowserAgentSession) (mcpBrowserDriveResult, error) {
	obs, err := sess.Observe(false)
	if err != nil {
		return mcpBrowserDriveResult{}, err
	}
	if obs == nil {
		return mcpBrowserDriveResult{}, errors.New("浏览器没有返回结果")
	}
	return mcpBrowserDriveResult{
		OK:     true,
		URL:    obs.Snapshot.URL,
		Title:  obs.Snapshot.Title,
		Text:   obs.Snapshot.PageTextExcerpt,
		Detail: strings.TrimSpace(obs.Display),
	}, nil
}

func mcpBrowserFromAction(res *browser.BrowserActionResult, err error) (mcpBrowserDriveResult, error) {
	if err != nil {
		return mcpBrowserDriveResult{}, err
	}
	if res == nil {
		return mcpBrowserDriveResult{}, errors.New("浏览器没有返回结果")
	}
	if res.AskUser != nil {
		return mcpBrowserDriveResult{}, errors.New("这一步需要人在桌面上确认")
	}
	out := mcpBrowserDriveResult{OK: res.Status == "" || res.Status == "ok", Detail: strings.TrimSpace(res.Detail)}
	if out.Detail == "" {
		out.Detail = strings.TrimSpace(res.Display)
	}
	if res.Status != "" && res.Status != "ok" {
		out.OK = false
		if out.Detail == "" {
			out.Detail = res.Status
		}
	}
	return out, nil
}
