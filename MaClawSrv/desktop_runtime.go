package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const desktopRuntimeModuleID = "maclawsrv.desktop"
const desktopToolName = "desktop"
const desktopCDPEnv = "MACLAW_DESKTOP_CDP"
const desktopContainerEnv = "MACLAW_DESKTOP_CONTAINER"
const desktopSessionOwner = "maclawsrv-desktop"
const desktopMaxSteps = 16

// desktopRuntimeModule exposes a virtual desktop to MaClawSrv turns.
// Every bot belonging to one user shares that user's desktop. A different
// user gets a different display, browser profile, and CDP endpoint.
type desktopRuntimeModule struct{}

func (desktopRuntimeModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{
		ModuleID:        desktopRuntimeModuleID,
		Version:         "1.0.0",
		HeadlessSupport: true,
	}
}

func (desktopRuntimeModule) Tools(context.Context, agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	if !desktopAvailable() {
		return nil, nil
	}
	return []agentruntime.ToolDefinition{{
		Name: desktopToolName,
		Description: "Operate this user's cloud desktop. " +
			"Bots of this user are different instances of the same MaClawSrv user and share this desktop. Other users cannot see it. " +
			"For web pages use the browser inside that desktop: action=probe once, then action=task_run with steps " +
			"(navigate, click, type, press, scroll, select, wait) in one call. Do not click the browser window with pixels. " +
			"For other applications use action=app_list, then action=app_run with steps focus, type, key, or click. " +
			"action=screenshot returns an image of the whole desktop; its pixel coordinates are the ones app_run click uses. " +
			"Single browser actions navigate, click, type, and press remain available.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":      map[string]any{"type": "string", "description": "probe, task_run, app_list, app_run, screenshot, navigate, click, type, or press"},
				"url":         map[string]any{"type": "string", "description": "URL for navigate"},
				"ref":         map[string]any{"type": "string", "description": "Element ref from probe, such as e3"},
				"snapshot_id": map[string]any{"type": "string", "description": "snapshot_id returned with the ref"},
				"text":        map[string]any{"type": "string", "description": "Text for type, or visible text for click when ref is empty"},
				"key":         map[string]any{"type": "string", "description": "Key for press"},
				"query":       map[string]any{"type": "string", "description": "Optional probe filter"},
				"steps":       map[string]any{"type": "array", "description": "task_run or app_run steps. Browser step: {action, params:{url,ref,text,key}}. App step: {action:focus|type|key|click, name, text, key, x, y}."},
			},
			"required": []string{"action"},
		},
	}}, nil
}

func (desktopRuntimeModule) InvokeTool(ctx context.Context, request agentruntime.TurnRequest, name string, args map[string]any) (string, error) {
	if strings.TrimSpace(name) != desktopToolName {
		return "", fmt.Errorf("unknown desktop tool %s", name)
	}
	return operateDesktop(ctx, request.Scope, args)
}

func (desktopRuntimeModule) ContributePrompt(context.Context, agentruntime.TurnRequest) (string, error) {
	if !desktopAvailable() {
		return "", nil
	}
	return "The current user has one cloud desktop through the desktop tool. " +
		"The person and this agent share that desktop's browser window, so a login or verification finished by the person stays in this browser. " +
		"After they hand it back, continue the original task in this browser. Other pages of this site keep the website login. If this site opens a window, continue there. Do not open another site, profile, or private session. " +
		"Do not use web_fetch, web_search, or the host open tool for a site this user may have signed into; those tools cannot see this browser. " +
		"This user's bots are different instances of the same MaClawSrv user, and those instances share this desktop. Other users have separate desktops. " +
		"Web pages: probe the logged-in page once, then one task_run. Navigate only to another page of this site. Do not pixel-click the browser. " +
		"Other apps: app_list, then one app_run. Use screenshot to see the whole screen when app_list is not enough, or to check a result. " +
		"Do not claim the screen changed until the tool result shows the new page or window.", nil
}

func srvDesktopRuntimeModules() []agentruntime.Module {
	if !desktopAvailable() {
		return nil
	}
	return []agentruntime.Module{desktopRuntimeModule{}}
}

func desktopCDPAddr() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv(desktopCDPEnv)), "/")
}

func desktopAvailable() bool {
	return desktopCDPAddr() != "" || desktopRemoteSession != nil || desktopHubConfigured()
}

type desktopEndpoint struct {
	CDP     string
	Display string
}

type desktopBinding struct {
	mu      sync.Mutex
	session *browser.BrowserAgentSession
	addr    string
}

var (
	desktopBindings sync.Map
	desktopEnsureFn = ensureUserDesktop
)

func desktopBlockedByPerson(scope agentruntime.Scope) error {
	if person, ok := desktopPersonInstanceID(scope); ok && (person != strings.TrimSpace(scope.InstanceID) || desktopUnattended(scope.InstanceID)) {
		return fmt.Errorf("用户正在这个桌面里登录或验证，请等这次完成后再操作")
	}
	return nil
}

func operateDesktop(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	scope = desktopOwner(scope)
	if err := desktopBlockedByPerson(scope); err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	switch action {
	case "navigate", "probe", "click", "type", "press", "task_run", "app_list", "app_run", "screenshot":
	default:
		return "", fmt.Errorf("desktop action must be probe, task_run, app_list, app_run, screenshot, navigate, click, type, or press")
	}
	if action == "navigate" && desktopArg(args, "url") == "" {
		return "", fmt.Errorf("desktop navigate requires url")
	}
	if action == "click" && desktopArg(args, "ref") == "" && desktopArg(args, "text") == "" {
		return "", fmt.Errorf("desktop click requires ref or text")
	}
	if action == "type" && desktopArg(args, "text") == "" {
		return "", fmt.Errorf("desktop type requires text")
	}
	if action == "press" && desktopArg(args, "key") == "" {
		return "", fmt.Errorf("desktop press requires key")
	}
	userKey, err := desktopUserKey(scope)
	if err != nil {
		return "", err
	}
	endpoint, err := desktopSession(ctx, scope, userKey)
	if err != nil {
		return "", err
	}
	if err := validateDesktopEndpoint(endpoint); err != nil {
		return "", err
	}
	binding := desktopBindingFor(userKey)
	binding.mu.Lock()
	defer binding.mu.Unlock()
	// The login may have been decided while this bot waited for the browser.
	// The check above ran before that. Acting now would switch the page.
	if err := desktopBlockedByPerson(scope); err != nil {
		return "", err
	}
	if action == "app_list" || action == "app_run" {
		return operateDesktopApp(ctx, scope, endpoint.Display, action, args)
	}
	if action == "screenshot" {
		return desktopScreenshot(ctx, scope, endpoint.Display)
	}

	session, err := desktopBrowserSession(binding, scope, userKey, endpoint.CDP)
	if err != nil {
		return "", err
	}
	var text string
	switch action {
	case "navigate":
		result, callErr := session.Navigate(desktopArg(args, "url"))
		text, err = desktopActionText(result, callErr)
	case "probe":
		obs, callErr := session.Probe(desktopArg(args, "query"))
		text, err = desktopObservationText(obs, callErr)
	case "task_run":
		steps, stepErr := desktopBrowserSteps(args["steps"])
		if stepErr != nil {
			return "", stepErr
		}
		state, callErr := browser.RunAgentFastBatchUntilPerson(session, steps, desktopArg(args, "text"))
		text, err = desktopTaskText(state, callErr)
	case "click":
		var result *browser.BrowserActionResult
		var callErr error
		if ref := desktopArg(args, "ref"); ref != "" {
			result, callErr = session.Click(desktopArg(args, "snapshot_id"), ref, "")
		} else {
			result, callErr = session.ClickText(desktopArg(args, "snapshot_id"), desktopArg(args, "text"))
		}
		text, err = desktopActionText(result, callErr)
	case "type":
		result, callErr := session.Type(desktopArg(args, "snapshot_id"), desktopArg(args, "ref"), "", desktopArg(args, "text"))
		text, err = desktopActionText(result, callErr)
	case "press":
		result, callErr := session.Press(desktopArg(args, "key"))
		text, err = desktopActionText(result, callErr)
	default:
		return "", fmt.Errorf("desktop action must be navigate, probe, click, type, or press")
	}
	return finishDesktopAction(scope, text, err)
}

// finishDesktopAction stops the turn when the desktop is waiting for a person.
// The agent loop treats the ask-user marker as a pause, so it does not keep
// clicking. The same instance continues when the user sends the next command.
func finishDesktopAction(scope agentruntime.Scope, text string, err error) (string, error) {
	if desktopNeedsPerson(text) {
		if desktopUnattended(scope.InstanceID) {
			return strings.TrimSpace(text + "\n这次是自动任务，没有人在桌面旁完成登录或验证。"), fmt.Errorf("desktop needs a person")
		}
		noteDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID)
		noteDesktopResume(scope)
		return agent.AskUserResultMarker(&agent.AskUserRequest{
			Question:  "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。",
			InputType: "text",
		}), nil
	}
	return text, err
}

func desktopNeedsPerson(text string) bool {
	if strings.Contains(text, "handoff=captcha") || strings.Contains(text, "captcha challenge") {
		return true
	}
	flags := text
	if index := strings.Index(text, "page flags:"); index >= 0 {
		flags = text[index:]
	} else {
		return false
	}
	return strings.Contains(flags, "login_wall") || strings.Contains(flags, "mfa") || strings.Contains(flags, "captcha_widget")
}

func desktopBrowserSession(binding *desktopBinding, scope agentruntime.Scope, userKey, addr string) (*browser.BrowserAgentSession, error) {
	if binding.session != nil && binding.addr == addr && binding.session.DesktopConnected() {
		followDesktopAfterPerson(scope, binding.session)
		return binding.session, nil
	}
	if binding.session != nil {
		_ = browser.StopAgentSession(binding.session.ID, false)
		binding.session = nil
		binding.addr = ""
	}
	// Reattach to the browser the person just used. Its profile and login stay put.
	session, err := browser.StartSharedDesktopSession(desktopSessionOwner+"-"+userKey, addr)
	if err != nil {
		return nil, fmt.Errorf("desktop browser: %w", err)
	}
	binding.session = session
	binding.addr = addr
	followDesktopAfterPerson(scope, session)
	return session, nil
}

// desktopKeepLoggedInDocument is replaced by tests. Production marks the
// browser so the continuation does not open a new document.
var desktopKeepLoggedInDocument = browser.KeepLoggedInDocument

func followDesktopAfterPerson(scope agentruntime.Scope, session *browser.BrowserAgentSession) {
	resumed := consumeDesktopResume(scope)
	// The first attach consumes the resume flag. A later attach in the same
	// continuation, after the CDP connection drops, must still refuse to
	// replace the logged-in document.
	if session != nil && (resumed || desktopLoginDocumentHeld(scope.TenantID, scope.UserID)) {
		desktopKeepLoggedInDocument(session, true)
	}
	if !resumed {
		return
	}
	_ = browser.FocusDesktopAfterPerson(session)
}

func desktopUserKey(scope agentruntime.Scope) (string, error) {
	// InstanceID is another agent of this same MaClawSrv user. The desktop
	// belongs to the user, so every instance of that user shares it.
	key, err := desktop.UserKey(scope.TenantID, scope.UserID)
	if err != nil {
		return "", fmt.Errorf("desktop requires a user")
	}
	return key, nil
}

func desktopBindingFor(userKey string) *desktopBinding {
	value, _ := desktopBindings.LoadOrStore(userKey, &desktopBinding{})
	return value.(*desktopBinding)
}

func validateDesktopEndpoint(endpoint desktopEndpoint) error {
	if !strings.HasPrefix(endpoint.CDP, "http://") && !strings.HasPrefix(endpoint.CDP, "https://") {
		return fmt.Errorf("desktop CDP endpoint is invalid")
	}
	if !desktop.ValidDisplay(endpoint.Display) {
		return fmt.Errorf("desktop display is invalid")
	}
	return nil
}

func desktopArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func desktopActionText(result *browser.BrowserActionResult, err error) (string, error) {
	if result != nil && (result.AskUser != nil || result.Status == "ask") {
		text := strings.TrimSpace(result.Display)
		if text == "" {
			text = "captcha challenge"
		}
		if !strings.Contains(text, "handoff=captcha") && !strings.Contains(text, "captcha challenge") {
			text += "\nhandoff=captcha"
		}
		return trimDesktopText(text), nil
	}
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("desktop action returned no result")
	}
	text := strings.TrimSpace(result.Display)
	if text == "" {
		text = strings.TrimSpace(result.Detail)
	}
	if text == "" {
		text = strings.TrimSpace(result.Status)
	}
	if text == "" {
		return "", fmt.Errorf("desktop action returned an empty result")
	}
	text = appendDesktopPageFlags(text, result.Data)
	return trimDesktopText(text), nil
}

func appendDesktopPageFlags(text string, data map[string]interface{}) string {
	if strings.Contains(text, "page flags:") || data == nil {
		return text
	}
	flags, ok := data["page_flags"].(browser.BrowserPageFlags)
	if !ok {
		return text
	}
	var names []string
	if flags.LoginWall {
		names = append(names, "login_wall")
	}
	if flags.MFA {
		names = append(names, "mfa")
	}
	if flags.CaptchaWidget {
		names = append(names, "captcha_widget")
	}
	if len(names) == 0 {
		return text
	}
	return text + "; page flags: " + strings.Join(names, ",")
}

func desktopObservationText(obs *browser.BrowserObservation, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if obs == nil {
		return "", fmt.Errorf("desktop probe returned no observation")
	}
	text := strings.TrimSpace(obs.Display)
	if text == "" {
		text = strings.TrimSpace(obs.Snapshot.Title)
	}
	if text == "" {
		return "", fmt.Errorf("desktop probe returned an empty observation")
	}
	return trimDesktopText(text), nil
}

func trimDesktopText(text string) string {
	const limit = 12000
	if len(text) <= limit {
		return text
	}
	// Login and captcha signals sit at the end of a long page observation.
	// Cutting only the head would let the bot keep clicking a login wall.
	tail := desktopKeepTail(text)
	marker := "\n…[truncated]\n"
	budget := limit - len(marker) - len(tail)
	if budget < 0 {
		budget = 0
	}
	head := text[:budget]
	if tail == "" || strings.Contains(head, tail) {
		return head + marker
	}
	return head + marker + tail
}

func desktopKeepTail(text string) string {
	var kept []string
	if index := strings.LastIndex(text, "page flags:"); index >= 0 {
		flags := text[index:]
		if end := strings.IndexByte(flags, '\n'); end >= 0 {
			flags = flags[:end]
		}
		kept = append(kept, flags)
	}
	if strings.Contains(text, "handoff=captcha") {
		kept = append(kept, "handoff=captcha")
	}
	return strings.Join(kept, "\n")
}

func desktopTaskText(state *browser.TaskState, err error) (string, error) {
	if state == nil {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("desktop task_run returned no state")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "status=%s step=%d/%d", state.Status, state.CurrentStep, state.TotalSteps)
	if state.StoppedReason != "" {
		fmt.Fprintf(&b, " stopped=%s", state.StoppedReason)
	}
	if state.LastError != "" {
		fmt.Fprintf(&b, " error=%s", state.LastError)
	}
	if strings.TrimSpace(state.Observation) != "" {
		fmt.Fprintf(&b, "\n%s", strings.TrimSpace(state.Observation))
	}
	if state.AskUser != nil || state.LastResultStatus == "ask" {
		b.WriteString("\nhandoff=captcha")
	}
	if err != nil && state.LastError == "" {
		fmt.Fprintf(&b, "\n%v", err)
	}
	return trimDesktopText(b.String()), nil
}

func desktopBrowserSteps(raw any) ([]browser.StepSpec, error) {
	if raw == nil {
		return nil, fmt.Errorf("desktop task_run requires steps")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("desktop steps: %w", err)
	}
	var loose []struct {
		Action string         `json:"action"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(encoded, &loose); err != nil {
		return nil, fmt.Errorf("desktop steps must be an array")
	}
	if len(loose) == 0 || len(loose) > desktopMaxSteps {
		return nil, fmt.Errorf("desktop task_run accepts 1 to %d steps", desktopMaxSteps)
	}
	steps := make([]browser.StepSpec, 0, len(loose))
	for _, step := range loose {
		action := strings.ToLower(strings.TrimSpace(step.Action))
		switch action {
		case "navigate", "click", "type", "press", "scroll", "select", "wait", "hover":
		default:
			return nil, fmt.Errorf("desktop browser step %q is not allowed", action)
		}
		params := make(map[string]string, len(step.Params))
		for key, value := range step.Params {
			params[key] = strings.TrimSpace(fmt.Sprint(value))
		}
		steps = append(steps, browser.StepSpec{Action: action, Params: params})
	}
	return steps, nil
}

func desktopSession(ctx context.Context, scope agentruntime.Scope, userKey string) (desktopEndpoint, error) {
	if desktopRemoteSession != nil {
		return desktopRemoteSession(ctx, scope.TenantID, scope.UserID)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.Session(ctx, scope.TenantID, scope.UserID)
	}
	return desktopEnsureFn(userKey)
}

func operateDesktopApp(ctx context.Context, scope agentruntime.Scope, display, action string, args map[string]any) (string, error) {
	run := func(argv ...string) (string, error) {
		if desktopRemoteApp != nil {
			return desktopRemoteApp(ctx, scope.TenantID, scope.UserID, display, argv)
		}
		if client := desktopHubClientFromEnv(); client != nil {
			return client.App(ctx, scope.TenantID, scope.UserID, display, argv)
		}
		return desktopAppCommand(display, argv...)
	}
	switch action {
	case "app_list":
		return run("search", "--onlyvisible", "--name", ".", "getwindowname", "%@")
	case "app_run":
		steps, err := desktopAppSteps(args["steps"])
		if err != nil {
			return "", err
		}
		out, err := run(desktopAppArgv(steps)...)
		if err != nil {
			text := strings.TrimSpace(out)
			if text == "" {
				text = err.Error()
			}
			return trimDesktopText(text), err
		}
		return trimDesktopText(strings.TrimSpace(out)), nil
	default:
		return "", fmt.Errorf("unknown desktop app action %s", action)
	}
}

// desktopScreenshotMaxBase64 keeps the image a model receives small enough
// for vision APIs. Larger PNGs (photo-heavy pages) are re-encoded as JPEG.
const desktopScreenshotMaxBase64 = 1_200_000

// desktopScreenshot captures the user's desktop. The text tells every model
// what was captured; the image itself is attached for vision models and never
// appears in the text, history, or logs (see agentruntime.AttachModelImage).
func desktopScreenshot(ctx context.Context, scope agentruntime.Scope, display string) (string, error) {
	var data []byte
	var err error
	switch {
	case desktopRemoteScreenshot != nil:
		data, err = desktopRemoteScreenshot(ctx, scope.TenantID, scope.UserID, display)
	case desktopHubClientFromEnv() != nil:
		data, err = desktopHubClientFromEnv().Screenshot(ctx, scope.TenantID, scope.UserID, display)
	default:
		data, err = desktopLocalScreenshot(display)
	}
	if err != nil {
		return "", err
	}
	if !desktop.IsPNG(data) {
		return "", fmt.Errorf("desktop screenshot is not a PNG image")
	}
	mime := "image/png"
	encoded := base64.StdEncoding.EncodeToString(data)
	config, decodeErr := png.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil {
		return "", fmt.Errorf("desktop screenshot is not a PNG image")
	}
	if len(encoded) > desktopScreenshotMaxBase64 {
		if smaller, ok := desktopScreenshotJPEG(data); ok {
			mime, encoded = "image/jpeg", base64.StdEncoding.EncodeToString(smaller)
		}
	}
	text := fmt.Sprintf("Desktop screenshot of display %s: %dx%d pixels (%s, %d KB). "+
		"The image is attached for models that can see images; app_run click uses these pixel coordinates. "+
		"If no image is visible to you, use probe for web pages and app_list for windows instead.",
		strings.TrimSpace(display), config.Width, config.Height, mime, (len(encoded)*3/4+1023)/1024)
	return agentruntime.AttachModelImage(text, mime, encoded), nil
}

func desktopScreenshotJPEG(data []byte) ([]byte, bool) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil || buf.Len() >= len(data) {
		return nil, false
	}
	return buf.Bytes(), true
}

// desktopLocalScreenshotRunner is set by tests.
var desktopLocalScreenshotRunner func(display string) (string, error)

// desktopLocalScreenshot captures the local desktop container (no Hub).
func desktopLocalScreenshot(display string) ([]byte, error) {
	if !desktop.ValidDisplay(display) {
		return nil, fmt.Errorf("desktop display is invalid")
	}
	var out string
	if desktopLocalScreenshotRunner != nil {
		text, err := desktopLocalScreenshotRunner(display)
		if err != nil {
			return nil, err
		}
		out = text
	} else {
		raw, err := exec.Command("docker", "exec", "-e", "DISPLAY="+display, desktopContainerName(), "sh", "-c", desktop.ScreenshotScript).Output()
		if err != nil {
			return nil, fmt.Errorf("desktop screenshot failed: %s", err.Error())
		}
		out = string(raw)
	}
	return desktop.DecodeScreenshot(out)
}

func desktopAppSteps(raw any) ([][]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("desktop app_run requires steps")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var loose []struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		Text   string `json:"text"`
		Key    string `json:"key"`
		X      any    `json:"x"`
		Y      any    `json:"y"`
	}
	if err := json.Unmarshal(encoded, &loose); err != nil {
		return nil, fmt.Errorf("desktop app steps must be an array")
	}
	if len(loose) == 0 || len(loose) > desktopMaxSteps {
		return nil, fmt.Errorf("desktop app_run accepts 1 to %d steps", desktopMaxSteps)
	}
	steps := make([][]string, 0, len(loose))
	for _, step := range loose {
		switch strings.ToLower(strings.TrimSpace(step.Action)) {
		case "focus":
			name := strings.TrimSpace(step.Name)
			if name == "" || strings.ContainsAny(name, "\r\n") {
				return nil, fmt.Errorf("desktop app focus requires a window name")
			}
			steps = append(steps, []string{"search", "--onlyvisible", "--name", name, "windowactivate"})
		case "type":
			text := step.Text
			if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "-") || strings.ContainsAny(text, "\r\n\x00") {
				return nil, fmt.Errorf("desktop app type requires text")
			}
			// One argument, and no "--". xdotool treats everything after
			// "--" as the text, so a later key or click would be typed.
			steps = append(steps, []string{"type", "--delay", "20", text})
		case "key":
			key := strings.TrimSpace(step.Key)
			if !desktopKeyPattern.MatchString(key) {
				return nil, fmt.Errorf("desktop app key %q is not allowed", key)
			}
			steps = append(steps, []string{"key", key})
		case "click":
			x, xOK := desktopCoord(step.X)
			y, yOK := desktopCoord(step.Y)
			if !xOK || !yOK {
				return nil, fmt.Errorf("desktop app click requires x and y")
			}
			steps = append(steps, []string{"mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y), "click", "1"})
		default:
			return nil, fmt.Errorf("desktop app step %q is not allowed", step.Action)
		}
	}
	return steps, nil
}

func desktopAppArgv(steps [][]string) []string {
	var argv []string
	for _, step := range steps {
		argv = append(argv, step...)
	}
	return argv
}

var desktopKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_+-]{1,40}$`)

func desktopCoord(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed > 4000 {
			return 0, false
		}
		return int(typed), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || n < 0 || n > 4000 {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func desktopContainerName() string {
	if name := strings.TrimSpace(os.Getenv(desktopContainerEnv)); name != "" {
		return name
	}
	return "maclaw-gui"
}

var desktopAppRunner func(display string, args ...string) (string, error)

func desktopAppCommand(display string, args ...string) (string, error) {
	if desktopAppRunner != nil {
		return desktopAppRunner(display, args...)
	}
	if len(args) == 0 {
		return "", fmt.Errorf("desktop app command is empty")
	}
	if !desktop.ValidDisplay(display) {
		return "", fmt.Errorf("desktop display is invalid")
	}
	cmd := exec.Command("docker", append([]string{"exec", "-e", "DISPLAY=" + display, desktopContainerName(), "xdotool"}, args...)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("desktop app command failed: %s", text)
	}
	return text, nil
}

func ensureUserDesktop(userKey string) (desktopEndpoint, error) {
	if !desktop.ValidUserKey(userKey) {
		return desktopEndpoint{}, fmt.Errorf("desktop user is invalid")
	}
	cmd := exec.Command("docker", "exec", desktopContainerName(), "python3", "/desktop_supervisor.py", "ensure", userKey)
	out, err := cmd.CombinedOutput()
	payload, parseErr := desktop.ParseEnsure(string(out))
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return desktopEndpoint{}, fmt.Errorf("desktop for this user is unavailable: %s", detail)
	}
	if parseErr != nil {
		return desktopEndpoint{}, parseErr
	}
	ip, err := desktopContainerIP()
	if err != nil {
		return desktopEndpoint{}, err
	}
	// The supervisor's CDP gate requires this bearer token; it travels as URL
	// userinfo and the browser client turns it into an Authorization header.
	cdp := "http://" + ip + ":" + strconv.Itoa(payload.CDPPort)
	if payload.Token != "" {
		cdp = "http://desktop:" + payload.Token + "@" + ip + ":" + strconv.Itoa(payload.CDPPort)
	}
	return desktopEndpoint{CDP: cdp, Display: payload.Display}, nil
}

func desktopContainerIP() (string, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", desktopContainerName())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("desktop container address is unavailable")
	}
	for _, field := range strings.Fields(string(out)) {
		if desktopIPv4Pattern.MatchString(field) {
			return field, nil
		}
	}
	return "", fmt.Errorf("desktop container address is unavailable")
}

var desktopIPv4Pattern = regexp.MustCompile(`^[0-9]{1,3}(?:\.[0-9]{1,3}){3}$`)
