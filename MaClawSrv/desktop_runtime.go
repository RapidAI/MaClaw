package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
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

func (desktopRuntimeModule) Tools(_ context.Context, request agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	if !desktopAvailable() || !desktopTurnUsesContainer(request) {
		return nil, nil
	}
	if botPhaseFromTurn(request) == "plan" {
		return []agentruntime.ToolDefinition{{
			Name: desktopToolName,
			Description: "Inspect this user's cloud desktop. " +
				"Use only probe, screenshot, app_list, file_read, or file_list. That limit is this turn only. " +
				"Judge whether this turn already finished the request. A finished look or screenshot is the result: say it is done, and do not leave an arrangement. " +
				"If work remains, the reply is one arrangement. Do not ask the user to choose a method, reply with a number, grant a permission, or carry out the work. The confirm control is the approval. " +
				"Installing software belongs in the arrangement: name the Debian packages. This turn does not run apt-get. " +
				"Starting a program belongs in the arrangement. This turn does not start it. " +
				"file_read and file_list read files in this container. Writing a file or running a command belongs in the arrangement. This turn does not write it or run it. " +
				"A machine outside this desktop uses the ssh tool. This turn may list its sessions. Connecting or running a command there belongs in the arrangement. This turn does not connect or run it. " +
				"A document the user asked for belongs in that arrangement. This turn does not create the file. After confirmation the chat receives the file, or that turn reports the failure. " +
				"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
				"The user message is the task order for this turn. Follow it. " +
				"A login wall, captcha, card field, or consent dialog is the person's step. Report it and wait. It does not hand over the desktop by itself. " +
				"Bots of this user are different instances of the same MaClawSrv user and share this desktop. Other users cannot see it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "description": "probe, screenshot, app_list, file_read, or file_list"},
					"path":   map[string]any{"type": "string", "description": "Container path for file_read or file_list, such as /home/desktop/Desktop/a.c or ~/Desktop/a.c"},
					"query":  map[string]any{"type": "string", "description": "Optional probe filter. Leave it empty to list every control."},
				},
				"required": []string{"action"},
			},
		}}, nil
	}
	description := "Operate this user's cloud desktop. "
	return []agentruntime.ToolDefinition{{
		Name: desktopToolName,
		Description: description +
			"Carry out the arrangement and report the outcome in this chat. " +
			"When the user asked for a file, action=deliver. A file already on this desktop, including a pdf, uses path, and the chat receives that file. name and content send the supplied text under that name. A name ending in .docx is a Word file built from that text. Say that it is attached to this message, or say what failed. Do not ask the user to choose a delivery method or to save the file themselves. Do not refuse a file because of its type. " +
			"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
			"The user message is the confirmed task order. Follow it, then report what happened. " +
			"Do the work in this turn. Do not stop to ask for another confirmation. Involve the person only for a login, captcha, payment, consent, or a fact you still lack. " +
			"Bots of this user are different instances of the same MaClawSrv user and share this desktop. Other users cannot see it. " +
			"For web pages use the browser inside that desktop: action=probe once, then action=task_run with steps " +
			"(navigate, click, type, press, scroll, select, wait) in one call. Do not click the browser window with pixels. " +
			"A probe with no filter that finds no element ref includes one desktop image so you can see the page. If a filter matches nothing, probe again without it. The next web step is still an element ref, not a pixel click. " +
			"For other applications, action=app_open with program starts that program in a window on this desktop. xfce4-terminal is already installed. xterm is another terminal when it is installed. app_open is not a shell. The new window does not use the Chinese input method, so ASCII typed into it is literal. Then action=app_list, then action=app_run with steps focus, type, key, or click. One type step can contain several lines. The terminal is a shell, so write the source as a heredoc in that step and compile on the next line. app_run focus only activates a window that is already open. exit status 1 from focus means that window is not open; start it with app_open. " +
			"action=install with packages installs those Debian packages in this desktop container. " +
			"action=file_read, file_write, file_edit, and file_list use a path inside this container, such as /home/desktop/Desktop/a.c or ~/Desktop/a.c. action=bash runs one command in this container and returns its output. A one-off script is file_write, then bash. " +
			"A machine outside this desktop uses the ssh tool: connect, then exec or exec_background. A command that should keep running uses exec_background, then check_task. action=bash stays inside this container. " +
			"action=screenshot returns an image of the whole desktop; its pixel coordinates are the ones app_run click uses. " +
			"To put that image on the desktop, set name to one file name ending in .png. The file is written to /home/desktop/Desktop and the result says Saved with that path. Without name, nothing is written. Do not invent a file path. " +
			"After an app_run that clicks, types, or sends a key, the result includes one fresh unscaled desktop image. app_run click uses those pixels. " +
			"Single browser actions navigate, click, type, and press remain available.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":      map[string]any{"type": "string", "description": "probe, task_run, app_list, app_run, app_open, install, screenshot, navigate, click, type, press, deliver, file_read, file_write, file_edit, file_list, or bash"},
				"path":        map[string]any{"type": "string", "description": "Container path. file_read, file_write, file_edit, and file_list use it as the file. action=deliver attaches that existing file, such as /home/desktop/Desktop/北京天气.pdf. /home/desktop/Desktop/a.c and ~/Desktop/a.c are the same folder."},
				"old_string":  map[string]any{"type": "string", "description": "Exact passage file_edit replaces. It must occur once."},
				"new_string":  map[string]any{"type": "string", "description": "Replacement text for file_edit."},
				"command":     map[string]any{"type": "string", "description": "Command for action=bash. It runs in this container and the result is its output."},
				"program":     map[string]any{"type": "string", "description": "Program for action=app_open. One name or an absolute path, such as xfce4-terminal. Not a shell and not a browser."},
				"args":        map[string]any{"type": "array", "description": "Arguments for action=app_open. Not a shell command.", "items": map[string]any{"type": "string"}},
				"packages":    map[string]any{"type": "array", "description": "Debian package names for action=install, such as libreoffice", "items": map[string]any{"type": "string"}},
				"name":        map[string]any{"type": "string", "description": "Download name for action=deliver. Any ordinary file name. When path is empty, the chat receives the text in content under this name. action=screenshot: ends in .png and is written to /home/desktop/Desktop."},
				"content":     map[string]any{"type": "string", "description": "Text for action=deliver when path is empty, or the full file text for action=file_write. A file already on the desktop uses path."},
				"url":         map[string]any{"type": "string", "description": "URL for navigate"},
				"ref":         map[string]any{"type": "string", "description": "Element ref from probe, such as e3"},
				"snapshot_id": map[string]any{"type": "string", "description": "snapshot_id returned with the ref"},
				"text":        map[string]any{"type": "string", "description": "Text for type, or visible text for click when ref is empty"},
				"key":         map[string]any{"type": "string", "description": "Key for press"},
				"query":       map[string]any{"type": "string", "description": "Optional probe filter. Leave it empty to list every control."},
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
	if ctx == nil {
		ctx = context.Background()
	}
	// A missing phase is not execution. Direct operateDesktop calls omit this
	// key and keep today's behavior; a bot turn always sets it.
	ctx = context.WithValue(ctx, desktopPhaseKey{}, botPhaseFromTurn(request))
	scope, cloud, err := desktopScopeForTurn(request)
	if err != nil {
		return "", err
	}
	if cloud {
		ctx = context.WithValue(ctx, desktopCloudKey{}, true)
		ctx = context.WithValue(ctx, desktopPersonKey{}, desktopAccount{TenantID: scope.TenantID, UserID: scope.UserID})
	}
	return operateDesktop(ctx, scope, args)
}

func (desktopRuntimeModule) ContributePrompt(_ context.Context, request agentruntime.TurnRequest) (string, error) {
	if !desktopAvailable() || !desktopTurnUsesContainer(request) {
		return "", nil
	}
	if botPhaseFromTurn(request) == "plan" {
		return "The current user has one cloud desktop through the desktop tool. " +
			"This turn only inspects it. " +
			"Use only probe, screenshot, app_list, file_read, or file_list. That limit is this turn only. " +
			"Judge whether this turn already finished the request. A finished look or screenshot is the result: say it is done, and do not leave an arrangement. " +
			"If work remains, the reply is one arrangement. Do not ask the user to choose a method, reply with a number, grant a permission, or carry out the work. The confirm control is the approval. " +
			"Installing software belongs in the arrangement: name the Debian packages. This turn does not run apt-get. " +
			"Starting a program belongs in the arrangement. This turn does not start it. " +
			"file_read and file_list read files in this container. Writing a file or running a command belongs in the arrangement. This turn does not write it or run it. " +
			"A machine outside this desktop uses the ssh tool. This turn may list its sessions. Connecting or running a command there belongs in the arrangement. This turn does not connect or run it. " +
			"A document the user asked for belongs in that arrangement. This turn does not create the file. After confirmation the chat receives the file, or that turn reports the failure. " +
			"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
			"The user message is the task order for this turn. Follow it. " +
			"The screenshot is the front window on this desktop, the same one the person is watching. " +
			"app_list, probe, and screenshot also list every open browser page, including tabs behind that window. probe reads only the attached page. " +
			"If the page is a login wall, captcha, card form, or consent dialog, the next step is the person finishing it in this browser. Describe that and wait. " +
			"This user's bots share this desktop. Other users have separate desktops.", nil
	}
	prompt := "The current user has one cloud desktop through the desktop tool. "
	return prompt +
		"Carry out the arrangement and report the outcome in this chat. " +
		"When the user asked for a file, action=deliver. A file already on this desktop, including a pdf, uses path, and the chat receives that file. name and content send the supplied text under that name. A name ending in .docx is a Word file built from that text. Say that it is attached to this message, or say what failed. Do not ask the user to choose a delivery method or to save the file themselves. Do not refuse a file because of its type. " +
		"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
		"The user message is the confirmed task order. Follow it, then report what happened. " +
		"Do the work in this turn. Do not stop to ask for another confirmation. Involve the person only for a login, captcha, payment, consent, or a fact you still lack. " +
		"The person and this agent share that desktop's browser window, so a login or verification finished by the person stays in this browser. " +
		"A window that only shows a network error is not that session when another page has already loaded. " +
		"app_list, probe, and screenshot list every open browser page, including tabs that are not in front. probe reads only the attached page. " +
		"After they hand it back, continue the original task in this browser. Other pages of this site keep the website login. If this site opens a window, continue there. Do not open another site, profile, or private session. " +
		"web_search and web_fetch use this desktop's network. A site the user may have signed into stays in this desktop browser, because those two tools do not see that login. " +
		"Files and commands use action=file_read, file_write, file_edit, file_list, and action=bash inside this container. " +
		"A machine outside this desktop uses the ssh tool: connect, then exec or exec_background. A command that should keep running uses exec_background, then check_task. action=bash stays inside this container. " +
		"This user's bots are different instances of the same MaClawSrv user, and those instances share this desktop. Other users have separate desktops. " +
		"Web pages: probe the logged-in page once, then one task_run. Navigate only to another page of this site. Do not pixel-click the browser. " +
		"A probe with no filter that finds no element ref includes one desktop image so you can see the page. If a filter matches nothing, probe again without it. The next web step is still an element ref, not a pixel click. " +
		"Other apps: action=app_open with program starts a window. xfce4-terminal is already installed. app_open is not a shell. That window does not use the Chinese input method, so ASCII typed into it is literal. app_run focus only activates a window that is already open; exit status 1 means that window is not open. One type step can contain several lines. The terminal is a shell, so a line break runs that line. Write the source as a heredoc in that step, then compile on the next line. Then app_list and one app_run. After click, type, or key, that result includes one fresh desktop image. app_run click uses those pixels. Use screenshot when app_list is not enough, or to check a result before any input. " +
		"To put a screenshot on the desktop, action=screenshot with name set to one .png file name. The result line Saved is the file that was written under /home/desktop/Desktop. If the result says the screenshot was not written, the desktop folder has no new file. Do not invent a path. " +
		"To install software, action=install with packages set to Debian package names. That runs apt-get update and apt-get install -y in this desktop container. " +
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
	if person, pinned := desktopPinnedPerson(ctx); pinned {
		scope.UserID = person.UserID
		if person.TenantID != "" {
			scope.TenantID = person.TenantID
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	switch action {
	case "navigate", "probe", "click", "type", "press", "task_run", "app_list", "app_run", "app_open", "install", "screenshot", "deliver", "file_read", "file_write", "file_edit", "file_list", "bash":
	default:
		return "", fmt.Errorf("desktop action must be probe, task_run, app_list, app_run, app_open, install, screenshot, navigate, click, type, press, deliver, file_read, file_write, file_edit, file_list, or bash")
	}
	// Deliver attaches one file to this chat. A path reads that file from the
	// desktop. A name and content build the file here. A login in progress
	// must not turn "send me the file" into another question.
	if action == "deliver" {
		if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" {
			return "", fmt.Errorf("plan phase blocks deliver until the user confirms")
		}
		return deliverDesktopDocument(ctx, scope, args)
	}
	if err := desktopBlockedByPerson(scope); err != nil {
		return "", err
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
	if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" {
		switch action {
		case "navigate", "click", "type", "press", "task_run", "app_run", "app_open", "install", "deliver", "file_write", "file_edit", "bash":
			return "", fmt.Errorf("plan phase blocks %s until the user confirms", action)
		}
	}
	if action == "file_read" || action == "file_write" || action == "file_edit" || action == "file_list" || action == "bash" {
		return operateDesktopContainer(ctx, scope, action, args)
	}
	var installPackages []string
	if action == "install" {
		names, nameErr := desktop.PackageNames(args["packages"])
		if nameErr != nil {
			return "", fmt.Errorf("desktop install rejected the package list: %w", nameErr)
		}
		installPackages = names
	}
	var openProgram string
	var openArgs []string
	if action == "app_open" {
		parsed, argErr := desktop.ProgramArgs(args["args"])
		if argErr != nil {
			return "", fmt.Errorf("desktop app_open rejected the program: %w", argErr)
		}
		argv, openErr := desktop.OpenArgv(desktopArg(args, "program"), parsed)
		if openErr != nil {
			return "", fmt.Errorf("desktop app_open rejected the program: %w", openErr)
		}
		openProgram = argv[0]
		openArgs = argv[1:]
	}
	// Refuse before a session exists when this command would type into the
	// control that is already focused. A ref, or a click before the text,
	// is checked again at that target. The error is only the code.
	if desktopInsertsIntoFocusedControl(action, args) && desktopPasswordFocused(scope, nil) {
		return "", fmt.Errorf("not_password_field")
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
	if action == "install" {
		text, installErr := installDesktopPackages(ctx, scope, installPackages)
		if installErr != nil {
			if strings.TrimSpace(text) == "" {
				return "", installErr
			}
			return trimDesktopText(text), installErr
		}
		return trimDesktopText(text), nil
	}
	binding := desktopBindingFor(userKey)
	binding.mu.Lock()
	defer binding.mu.Unlock()
	// The login may have been decided while this bot waited for the browser.
	// The check above ran before that. Acting now would switch the page.
	if err := desktopBlockedByPerson(scope); err != nil {
		return "", err
	}
	if action == "app_open" {
		return operateDesktopOpen(ctx, scope, endpoint.Display, openProgram, openArgs)
	}
	if action == "app_list" || action == "app_run" {
		// A click is delivered before the next type or character key. The
		// focused control is read after that click returns, so a click into
		// a password field is not in the same xdotool command as the text.
		// The typed text is not part of the error. The read attaches the
		// browser and raises Chromium. A named window is focused again
		// after that read. A text step with no named window records the
		// active window before that read only when the read will raise
		// Chromium, then activates the id. key uses the search window
		// stack when one is present, and the terminal ignores those
		// events, so the typing call does not include the search. A failed
		// attach does not block a terminal or another non-browser window.
		var beforeInsert func() error
		if action == "app_run" && appStepsIncludeType(args["steps"]) {
			beforeInsert = func() error {
				session, serr := desktopBrowserSession(binding, scope, userKey, endpoint.CDP)
				if serr != nil {
					return serr
				}
				if desktopPasswordFocused(scope, session) {
					return fmt.Errorf("not_password_field")
				}
				return nil
			}
		}
		if action == "app_list" {
			text, listErr := operateDesktopApp(ctx, scope, endpoint.Display, action, args, nil)
			return withDesktopPages(text, listErr, endpoint.CDP, "")
		}
		text, appErr := desktopAppRun(ctx, scope, endpoint.Display, action, args, beforeInsert, func() bool {
			return desktopAppBrowserRaises(binding, scope, endpoint.CDP)
		})
		if appErr != nil || !appStepsIncludeInput(args["steps"]) {
			return text, appErr
		}
		return desktopAppRunWithShot(ctx, scope, endpoint.Display, text)
	}
	if action == "screenshot" {
		// The shot is the whole display. A network-error window left in front
		// is what gets captured, even when the loaded page is open behind it.
		// A login resume already raises that page. A normal screenshot did not.
		name := strings.TrimSpace(desktopArg(args, "name"))
		if name != "" {
			if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" {
				return "", fmt.Errorf("plan phase blocks saving a screenshot until the user confirms")
			}
			if _, err := desktop.DesktopShotFileName(name); err != nil {
				return "", err
			}
		}
		session, _ := desktopOpenBrowser(binding, scope, userKey, endpoint.CDP)
		attached := ""
		if session != nil {
			_ = desktopFocusVisiblePage(session)
			attached = session.TargetID
		}
		shot, shotErr := desktopScreenshotNamed(ctx, scope, endpoint.Display, name)
		if shotErr != nil {
			return "", shotErr
		}
		return withDesktopPages(shot, nil, endpoint.CDP, attached)
	}

	session, err := desktopBrowserSession(binding, scope, userKey, endpoint.CDP)
	if err != nil {
		return "", err
	}
	// Same rule after attach. A type that names a ref is decided by that
	// ref, not by whichever control happens to be focused.
	if desktopInsertsIntoFocusedControl(action, args) && desktopPasswordFocused(scope, session) {
		return "", fmt.Errorf("not_password_field")
	}
	var text string
	switch action {
	case "navigate":
		result, callErr := session.Navigate(desktopArg(args, "url"))
		text, err = desktopActionText(result, callErr)
	case "probe":
		obs, callErr := session.Probe(desktopArg(args, "query"))
		text, err = desktopObservationText(ctx, scope, endpoint.Display, desktopArg(args, "query"), obs, callErr)
		text, err = withDesktopPages(text, err, endpoint.CDP, session.TargetID)
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
	phase := "execute"
	if value, keyed := desktopPhase(ctx); keyed {
		phase = value
	}
	return finishDesktopActionPhase(scope, text, err, phase)
}

// finishDesktopAction stops the turn when the desktop is waiting for a person.
// The agent loop treats the ask-user marker as a pause, so it does not keep
// clicking. The same instance continues when the user sends the next command.
const desktopLoginQuestion = "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。"

func finishDesktopAction(scope agentruntime.Scope, text string, err error) (string, error) {
	return finishDesktopActionPhase(scope, text, err, "execute")
}

// finishDesktopActionPhase hands the desktop to a person only during execution.
// A plan probe that sees a login wall returns the observation and does not
// record a handoff or the login question.
func finishDesktopActionPhase(scope agentruntime.Scope, text string, err error, phase string) (string, error) {
	reason := desktopAttentionReason(text)
	if reason == "" {
		return text, err
	}
	if phase != "execute" {
		return text, err
	}
	if desktopUnattended(scope.InstanceID) {
		return strings.TrimSpace(text + "\n这次是自动任务，没有人在桌面旁完成登录或验证。"), fmt.Errorf("desktop needs a person")
	}
	noteDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID)
	noteDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID, reason)
	noteDesktopResume(scope)
	question := desktopLoginQuestion
	switch reason {
	case "payment_confirm":
		question = "这一步需要你在当前桌面的浏览器里完成支付。完成后这个 bot 会接着操作。"
	case "consent_dialog":
		question = "这一步需要你在当前桌面的浏览器里确认这项同意。完成后这个 bot 会接着操作。"
	}
	return agent.AskUserResultMarker(&agent.AskUserRequest{
		Question:  question,
		InputType: "text",
	}), nil
}

func desktopNeedsPerson(text string) bool {
	return desktopAttentionReason(text) != ""
}

func desktopAttentionReason(text string) string {
	if strings.Contains(text, "handoff=captcha") || strings.Contains(text, "captcha challenge") {
		return "captcha_widget"
	}
	flags := text
	if index := strings.Index(text, "page flags:"); index >= 0 {
		flags = text[index:]
	} else {
		return ""
	}
	for _, name := range []string{"captcha_widget", "mfa", "payment_confirm", "consent_dialog", "login_wall"} {
		if strings.Contains(flags, name) {
			return name
		}
	}
	return ""
}

type desktopPhaseKey struct{}

type desktopCloudKey struct{}

type desktopPersonKey struct{}

func desktopPhase(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(desktopPhaseKey{}).(string)
	return value, ok
}

// desktopTurnUsesContainer is a bot turn. A digital-employee turn has neither
// hub_bot nor bot_phase, and keeps the host filesystem, shell, and network.
func desktopTurnUsesContainer(request agentruntime.TurnRequest) bool {
	switch botPhaseFromTurn(request) {
	case "plan", "execute":
		return true
	}
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return strings.TrimSpace(input.Instance.Metadata["hub_bot"]) == "1"
	case *agentservice.ExecuteRequest:
		return input != nil && strings.TrimSpace(input.Instance.Metadata["hub_bot"]) == "1"
	default:
		return false
	}
}

func botPhaseFromTurn(request agentruntime.TurnRequest) string {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		if input.Message.Metadata == nil {
			return ""
		}
		return strings.TrimSpace(input.Message.Metadata["bot_phase"])
	case *agentservice.ExecuteRequest:
		if input == nil || input.Message.Metadata == nil {
			return ""
		}
		return strings.TrimSpace(input.Message.Metadata["bot_phase"])
	default:
		return ""
	}
}

// desktopScopeForTurn is the Hub person whose noVNC desktop this bot drives.
// The MaClaw service principal is a different container. A hub bot with no
// bound person does not fall through to that principal.
func desktopScopeForTurn(request agentruntime.TurnRequest) (agentruntime.Scope, bool, error) {
	scope := request.Scope
	inst, hasInstance := turnInstance(request)
	if hasInstance && strings.TrimSpace(scope.InstanceID) == "" {
		scope.InstanceID = strings.TrimSpace(inst.ID)
	}
	var meta map[string]string
	if hasInstance {
		meta = inst.Metadata
	}
	scope = desktopOwner(scope)
	cloud := false
	if user := strings.TrimSpace(meta["hub_user_id"]); desktop.ValidUserID(user) {
		scope.UserID = user
		cloud = true
		if tenant := strings.TrimSpace(meta["hub_tenant_id"]); validDesktopTenant(tenant) {
			scope.TenantID = tenant
		}
	}
	if strings.TrimSpace(meta["hub_bot"]) == "1" && !cloud {
		account, hit := desktopAccountForInstance(scope.InstanceID)
		if !hit {
			return scope, false, fmt.Errorf("this bot's desktop is the person's cloud desktop, and that account is not bound")
		}
		scope.UserID = account.UserID
		if account.TenantID != "" {
			scope.TenantID = account.TenantID
		}
		cloud = true
	}
	if !cloud {
		if account, hit := desktopAccountForInstance(scope.InstanceID); hit && account.UserID != strings.TrimSpace(request.Scope.UserID) {
			cloud = true
		}
	}
	return scope, cloud, nil
}

func turnInstance(request agentruntime.TurnRequest) (agentservice.Instance, bool) {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Instance, true
	case *agentservice.ExecuteRequest:
		if input == nil {
			return agentservice.Instance{}, false
		}
		return input.Instance, true
	default:
		return agentservice.Instance{}, false
	}
}

func desktopAccountForInstance(instanceID string) (desktopAccount, bool) {
	value, ok := desktopUserByInstance.Load(strings.TrimSpace(instanceID))
	if !ok {
		return desktopAccount{}, false
	}
	account, _ := value.(desktopAccount)
	if !desktop.ValidUserID(account.UserID) {
		return desktopAccount{}, false
	}
	return account, true
}

func desktopPinnedPerson(ctx context.Context) (desktopAccount, bool) {
	if ctx == nil {
		return desktopAccount{}, false
	}
	account, ok := ctx.Value(desktopPersonKey{}).(desktopAccount)
	if !ok || !desktop.ValidUserID(account.UserID) {
		return desktopAccount{}, false
	}
	return account, true
}

func refuseLocalDesktop(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	on, _ := ctx.Value(desktopCloudKey{}).(bool)
	if !on {
		return nil
	}
	return fmt.Errorf("this bot's desktop is the person's cloud desktop, and the connection is not configured")
}

// desktopFocusedInputType reports the focused control type. Tests replace it.
// Production uses the same frame walk as secret fill.
var desktopFocusedInputType = desktopLiveFocusedInputType

func desktopLiveFocusedInputType(scope agentruntime.Scope) string {
	session := desktopLiveSession(scope)
	if session == nil {
		return ""
	}
	_, kind := session.FocusedInputKind()
	return kind
}

// desktopPasswordFocused is true when a model type would land in a password
// field. A session already in hand is read directly so the binding lock is
// not taken twice. With no session, the reader is the production frame walk
// unless a test replaced it. The typed text is not part of this.
func desktopPasswordFocused(scope agentruntime.Scope, session *browser.BrowserAgentSession) bool {
	if session != nil {
		_, kind := session.FocusedInputKind()
		return browser.FocusBlocksModelType(kind)
	}
	if desktopFocusedInputType == nil {
		return false
	}
	return browser.FocusBlocksModelType(desktopFocusedInputType(scope))
}

// desktopInsertsIntoFocusedControl reports whether the command would type
// into the control that is focused before any of its steps run. A later
// click or a named ref can land somewhere else, so those stay out of this
// check and are read at the insert.
func desktopInsertsIntoFocusedControl(action string, args map[string]any) bool {
	switch action {
	case "type":
		return desktopArg(args, "ref") == "" && desktopArg(args, "selector") == ""
	case "press":
		return browser.KeyInsertsText(desktopArg(args, "key"))
	case "task_run":
		return browserStepsInsertIntoFocus(args["steps"])
	case "app_run":
		return appStepsInsertIntoFocus(args["steps"])
	default:
		return false
	}
}

func browserStepsInsertIntoFocus(raw any) bool {
	steps, err := desktopBrowserSteps(raw)
	if err != nil {
		return false
	}
	moved := false
	for _, step := range steps {
		switch step.Action {
		case "click", "navigate":
			moved = true
		case "press":
			key := step.Params["key"]
			if strings.TrimSpace(key) == "" {
				key = step.Params["text"]
			}
			if browser.KeyInsertsText(key) {
				if !moved {
					return true
				}
			} else if strings.TrimSpace(key) != "" {
				moved = true
			}
		case "type":
			if strings.TrimSpace(step.Params["ref"]) == "" && strings.TrimSpace(step.Params["selector"]) == "" && !moved {
				return true
			}
		}
	}
	return false
}

func appStepsInsertIntoFocus(raw any) bool {
	steps, err := desktopAppSteps(raw)
	if err != nil {
		return false
	}
	moved := false
	for _, step := range steps {
		if len(step) == 0 {
			continue
		}
		switch step[0] {
		case "search", "mousemove":
			moved = true
		case "key":
			if len(step) >= 2 && browser.KeyInsertsText(step[1]) {
				if !moved {
					return true
				}
			} else {
				moved = true
			}
		case "type":
			if !moved {
				return true
			}
		}
	}
	return false
}

func appStepsIncludeInput(raw any) bool {
	if raw == nil {
		return false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var loose []struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(encoded, &loose); err != nil {
		return false
	}
	for _, step := range loose {
		switch strings.ToLower(strings.TrimSpace(step.Action)) {
		case "click", "type", "key":
			return true
		}
	}
	return false
}

func appStepsIncludeType(raw any) bool {
	if raw == nil {
		return false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var loose []struct {
		Action string `json:"action"`
		Key    string `json:"key"`
	}
	if err := json.Unmarshal(encoded, &loose); err != nil {
		return false
	}
	for _, step := range loose {
		switch strings.ToLower(strings.TrimSpace(step.Action)) {
		case "type":
			return true
		case "key":
			if browser.KeyInsertsText(step.Key) {
				return true
			}
		}
	}
	return false
}

func desktopBrowserSession(binding *desktopBinding, scope agentruntime.Scope, userKey, addr string) (*browser.BrowserAgentSession, error) {
	if binding.session != nil && binding.addr == addr && desktopSessionConnected(binding.session) {
		binding.session.BlockModelPasswordTyping()
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
	session.BlockModelPasswordTyping()
	followDesktopAfterPerson(scope, session)
	return session, nil
}

// desktopSessionConnected is replaced by tests. Production asks the live
// CDP connection, not only whether the tab id is still remembered.
var desktopSessionConnected = func(session *browser.BrowserAgentSession) bool {
	return session != nil && session.DesktopConnected()
}

// desktopAppBrowserRaises reports whether the password read will put
// Chromium in front. The first attach and a dead connection do. A login
// resume does. A browser that is already connected, with no resume waiting,
// does not, so typing must not pay for a window restore or stop when that
// restore fails.
func desktopAppBrowserRaises(binding *desktopBinding, scope agentruntime.Scope, addr string) bool {
	if binding == nil || binding.session == nil || binding.addr != addr || !desktopSessionConnected(binding.session) {
		return true
	}
	_, pending := desktopResumeFocus.Load(desktopResumeKey(scope))
	return pending
}

// desktopOpenBrowser and desktopFocusVisiblePage are replaced by tests.
// Production reattaches to the shared browser, then raises the page the
// person can use so a screenshot does not show a network-error window.
var desktopOpenBrowser = desktopBrowserSession
var desktopFocusVisiblePage = browser.FocusDesktopAfterPerson

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

// desktopOpenPages is replaced by tests. Production asks the browser for
// every page, not only the tab the agent session is attached to.
var desktopOpenPages = listDesktopOpenPages

func withDesktopPages(text string, err error, addr, attachedID string) (string, error) {
	// The last line of a probe or a screenshot tells the model what to do
	// next. The open-page list is context. Leaving the list after that
	// line is what the model follows, so a no-ref probe was read as a page
	// list instead of "do not pixel-click".
	instruction := ""
	if err == nil {
		text, instruction = detachDesktopInstruction(text)
	}
	pages := desktopOpenPages(addr, attachedID)
	if pages == "" {
		if instruction == "" {
			return text, err
		}
		return joinDesktopInstruction(text, instruction), err
	}
	if err != nil {
		note := desktopViewError(err)
		if note != "" {
			pages += "\nThe attached page did not answer: " + note
		}
		return pages, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = pages
	} else {
		text = text + "\n" + pages
	}
	return joinDesktopInstruction(text, instruction), nil
}

// detachDesktopInstruction splits off a trailing line that tells the model
// how to use this result. Other last lines, including a page observation,
// stay where they are.
func detachDesktopInstruction(text string) (body, instruction string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	prefix := ""
	last := trimmed
	if nl := strings.LastIndex(trimmed, "\n"); nl >= 0 {
		prefix = strings.TrimSpace(trimmed[:nl])
		last = strings.TrimSpace(trimmed[nl+1:])
	}
	if strings.Contains(last, "Do not pixel-click the browser.") || strings.Contains(last, "app_run click uses these pixel coordinates.") {
		return prefix, last
	}
	return text, ""
}

func joinDesktopInstruction(text, instruction string) string {
	text = strings.TrimSpace(text)
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return text
	}
	if text == "" {
		return instruction
	}
	return text + "\n" + instruction
}

func desktopViewError(err error) string {
	if err == nil {
		return ""
	}
	note := strings.TrimSpace(err.Error())
	if strings.Contains(note, "://") || strings.Contains(note, "@") {
		return "the attached page did not answer"
	}
	if len(note) > 180 {
		note = note[:180]
	}
	return note
}

func listDesktopOpenPages(addr, attachedID string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	targets, err := browser.DiscoverTargetsContext(ctx, addr)
	if err != nil {
		return ""
	}
	attachedID = strings.TrimSpace(attachedID)
	lines := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Type != "page" || strings.TrimSpace(target.ID) == "" {
			continue
		}
		title := desktopOneLine(target.Title, 80)
		if title == "" {
			title = "(untitled)"
		}
		mark := "-"
		if target.ID == attachedID {
			mark = "- attached"
		}
		lines = append(lines, mark+" "+title+" — "+desktopPageLocation(target.URL))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Open browser pages (" + strconv.Itoa(len(lines)) + "), including tabs behind the front window:\n" + strings.Join(lines, "\n")
}

func desktopPageLocation(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(no address)"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
			raw = raw[:cut]
		}
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func desktopOneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}

func desktopObservationText(ctx context.Context, scope agentruntime.Scope, display, query string, obs *browser.BrowserObservation, err error) (string, error) {
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
	text = trimDesktopText(text)
	if len(obs.Snapshot.Refs) > 0 {
		return text, nil
	}
	// A filter that matched nothing is not an empty page. Another probe
	// without that filter still finds the controls, so do not spend a
	// desktop capture or invite a pixel click.
	if strings.TrimSpace(query) != "" {
		return text + "\nNo element ref matched that filter. Probe again without a filter. Do not pixel-click the browser.", nil
	}
	if ctx != nil && ctx.Err() != nil {
		return text + "\nNo element ref was found. Do not pixel-click the browser.", nil
	}
	shot, shotErr := desktopCapture(ctx, scope, display, false, "")
	if shotErr != nil {
		return text + "\nNo element ref was found. The desktop image could not be captured. Do not pixel-click the browser.", nil
	}
	// The prohibition stays after the image caption. The shared screenshot
	// caption used by app_run says to click those pixels, which would undo
	// this and send the next web step into the browser window.
	return text + "\n" + shot + "\nNo element ref was found, so this image shows the desktop. Do not pixel-click the browser.", nil
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
	if err := refuseLocalDesktop(ctx); err != nil {
		return desktopEndpoint{}, err
	}
	return desktopEnsureFn(userKey)
}

func operateDesktopApp(ctx context.Context, scope agentruntime.Scope, display, action string, args map[string]any, beforeInsert func() error) (string, error) {
	return desktopAppRun(ctx, scope, display, action, args, beforeInsert, nil)
}

func desktopAppRun(ctx context.Context, scope agentruntime.Scope, display, action string, args map[string]any, beforeInsert func() error, browserRaises func() bool) (string, error) {
	run := func(argv ...string) (string, error) {
		if desktopRemoteApp != nil {
			return desktopRemoteApp(ctx, scope.TenantID, scope.UserID, display, argv)
		}
		if client := desktopHubClientFromEnv(); client != nil {
			return client.App(ctx, scope.TenantID, scope.UserID, display, argv)
		}
		if err := refuseLocalDesktop(ctx); err != nil {
			return "", err
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
		return runGuardedDesktopAppSteps(steps, beforeInsert, browserRaises, run)
	default:
		return "", fmt.Errorf("unknown desktop app action %s", action)
	}
}

func desktopAppResult(out string, err error) (string, error) {
	if err != nil {
		text := strings.TrimSpace(out)
		if text == "" {
			text = err.Error()
		}
		message := desktop.AppFailureText(text, err)
		if message == desktop.AppWindowMiss {
			return trimDesktopText(text), fmt.Errorf("%s", message)
		}
		return trimDesktopText(text), err
	}
	return trimDesktopText(strings.TrimSpace(out)), nil
}

func operateDesktopOpen(ctx context.Context, scope agentruntime.Scope, display, program string, args []string) (string, error) {
	if desktopRemoteOpen != nil {
		return desktopRemoteOpen(ctx, scope.TenantID, scope.UserID, display, program, args)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.Open(ctx, scope.TenantID, scope.UserID, display, program, args)
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return "", err
	}
	return desktopOpenCommand(ctx, display, program, args)
}

// runGuardedDesktopAppSteps delivers clicks, then reads the focused control,
// then releases stuck modifiers, then focuses the named window, then sends
// keys. Attaching the browser calls Target.activateTarget and raises
// Chromium, so the focus happens after that read. The release is also
// before that focus: a keyup between windowactivate and the typing
// process can move the window, and the typing process would then miss it.
// The typing process starts with an empty window stack. key uses the stack
// left by search and then sends XSendEvent, which the terminal ignores, so
// search stays in the focus process. A line and its Enter share the typing
// process. Nothing else runs between focus and that process.
// Super keyup opens the modal Whisker Menu. windowactivate --sync polls for
// up to 15s and then returns success even when that menu still holds the
// active window, so the click would wait and still land on the menu. The
// plugin's untranslated title is "Whisker Menu". XUnmapWindow is not
// gtk_widget_hide: the widget stays visible, and the next focus change
// calls gtk_window_present and puts the menu back. After the release, a
// visible menu is closed with key Escape on its own process, which is the
// handler that hides it. A search miss does not send Escape. The popup
// command toggles and drops the first event after the shortcut that opened
// the menu, so it is not used. The recorded window's name is read with
// getwindowname. A Whisker Menu id is not activated. A click records that
// window before the release and activates it before the click. A failed
// activate does not click. A later click does not record again. A text
// step with no named window records the active window before a password
// read that will raise Chromium. When the modifier release has not already
// run, that record happens before the release and the window is activated
// after the menu is closed, including a warm browser and a bare Return. A
// later key does not record again. A named focus still wins. The id is not
// the tool result. browserRaises nil means the read might raise.
func runGuardedDesktopAppSteps(steps [][]string, beforeInsert func() error, browserRaises func() bool, run func(...string) (string, error)) (string, error) {
	var pending [][]string
	var lastFocus []string
	focusPending := false
	released := false
	var last string
	// xdotool stops the rest of one process when keyup cannot resolve a
	// keysym, so the release is its own call and its error is ignored.
	// One release covers the whole app_run: a stuck modifier comes from
	// the viewer losing focus, not from each line this bot types.
	releaseOnce := func() bool {
		if released {
			return false
		}
		released = true
		argv := make([]string, len(desktopModifierRelease))
		copy(argv, desktopModifierRelease)
		_, _ = run(argv...)
		return true
	}
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		release := false
		argv := make([]string, 0, 8)
		for _, step := range pending {
			if len(step) > 0 && step[0] == "mousemove" {
				release = true
			}
			argv = append(argv, step...)
		}
		pending = nil
		// Super keyup opens the modal Whisker Menu. Record the window
		// first, release, close that menu, then activate before the
		// click. A Whisker Menu id is not restored. A failed activate
		// must not click. A non-numeric id is ignored. None of those
		// outputs is the tool result. A later click in this app_run
		// already released, so it does not record.
		if release && !released {
			restore := desktopReadActiveWindow(run)
			if releaseOnce() {
				dismissDesktopWhisker(run)
			}
			if restore != "" {
				activated, activateErr := run("windowactivate", "--sync", restore)
				if activateErr != nil {
					if strings.TrimSpace(activated) != "" {
						last = activated
					}
					return activateErr
				}
			}
		} else if release {
			releaseOnce()
		}
		out, err := run(argv...)
		if strings.TrimSpace(out) != "" {
			last = out
		}
		return err
	}
	for _, step := range steps {
		if desktopArgvSendsKeys(step) {
			kept := make([][]string, 0, len(pending))
			for _, earlier := range pending {
				if windowActivateStep(earlier) {
					continue
				}
				kept = append(kept, earlier)
			}
			pending = kept
			if err := flush(); err != nil {
				return desktopAppResult(last, err)
			}
			// Record the focused window before something in this step moves
			// it. The password read does, when it will raise Chromium.
			// Otherwise the modifier release does, and only the first
			// time. A click already recorded its window and activated it
			// before the click. A named window is activated below instead.
			// A non-numeric reply is not restored. This output is not the
			// tool result.
			restoreID := ""
			unnamed := len(lastFocus) == 0
			raiseSteals := unnamed && desktopArgvInsertsText(step) && beforeInsert != nil && (browserRaises == nil || browserRaises())
			releaseSteals := unnamed && !released && !raiseSteals
			if raiseSteals || releaseSteals {
				restoreID = desktopReadActiveWindow(run)
			}
			// The password read runs before the keystrokes. Its failure is
			// kept until the target window is focused. A terminal's class is
			// not a browser, so a dead debug socket does not eat the command.
			// A browser class, or a class that cannot be read, still stops
			// here and the text is not typed.
			var insertErr error
			if desktopArgvInsertsText(step) && beforeInsert != nil {
				insertErr = beforeInsert()
				focusPending = len(lastFocus) > 0
			}
			// No window is waiting to be focused, and the active window was
			// not recorded. The class cannot show that the keystrokes leave
			// the browser, so the read's error stops the type before any key.
			if insertErr != nil && !focusPending && restoreID == "" {
				return "", insertErr
			}
			if releaseOnce() {
				// Close the menu before focus. --sync would otherwise
				// wait out its 15s poll while the modal menu stays up,
				// and a later activate would present a menu that had
				// only been unmapped.
				dismissDesktopWhisker(run)
			}
			activatedTarget := false
			if focusPending {
				out, err := run(lastFocus...)
				if err != nil {
					return desktopAppResult(out, err)
				}
				if strings.TrimSpace(out) != "" {
					last = out
				}
				focusPending = false
				activatedTarget = true
			} else if restoreID != "" {
				// Activate by id, not search. A failed activate must not
				// type: the browser is the focused window then.
				out, err := run("windowactivate", "--sync", restoreID)
				if err != nil {
					return desktopAppResult(out, err)
				}
				activatedTarget = true
			}
			if insertErr != nil {
				if !activatedTarget || !desktopFocusedWindowOutsideBrowser(run) {
					return "", insertErr
				}
			}
			calls := [][]string{step}
			if step[0] == "type" {
				expanded, expandErr := expandTypeStep(step)
				if expandErr != nil {
					return "", expandErr
				}
				calls = expanded
			}
			for _, call := range calls {
				out, err := run(call...)
				if err != nil {
					return desktopAppResult(out, err)
				}
				if strings.TrimSpace(out) != "" {
					last = out
				}
			}
			continue
		}
		if windowActivateStep(step) {
			lastFocus = append([]string{}, step...)
			focusPending = true
		} else if len(step) > 0 && step[0] == "mousemove" {
			lastFocus = nil
			focusPending = false
		}
		pending = append(pending, step)
	}
	if err := flush(); err != nil {
		return desktopAppResult(last, err)
	}
	return trimDesktopText(strings.TrimSpace(last)), nil
}

// desktopWhiskerMenuTitle is the untranslated title set with
// gtk_window_set_title so a window manager can identify the menu.
// desktopWhiskerMenuName is that title as an xdotool search regex.
const desktopWhiskerMenuTitle = "Whisker Menu"
const desktopWhiskerMenuName = "^Whisker Menu$"

// desktopActiveWindowRead prints the focused X11 id and then its name.
// getwindowname sees _NET_WM_NAME, which search --name on this xdotool
// does not. The two commands share one process so the id stays %1.
var desktopActiveWindowRead = []string{"getactivewindow", "getwindowname"}

// desktopWindowClassRead prints the WM_CLASS class of the focused window.
// It is its own process so a preceding search does not replace the active
// window with the search stack.
var desktopWindowClassRead = []string{"getactivewindow", "getwindowclassname"}

// desktopWindowIsBrowser reports whether this WM_CLASS can hold a browser
// password field. A terminal class is not in this set.
func desktopWindowIsBrowser(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "chromium", "chromium-browser", "chrome", "google-chrome", "google-chrome-stable",
		"firefox", "firefox-esr", "navigator", "epiphany", "microsoft-edge", "msedge":
		return true
	default:
		return false
	}
}

// desktopFocusedWindowOutsideBrowser reads the focused window's class.
// True means the keystrokes land outside the browser, so a dead debug
// socket must not discard them. A failed read, an empty class, or a
// browser class is false and the password gate stays closed.
func desktopFocusedWindowOutsideBrowser(run func(...string) (string, error)) bool {
	argv := make([]string, len(desktopWindowClassRead))
	copy(argv, desktopWindowClassRead)
	out, err := run(argv...)
	if err != nil {
		return false
	}
	class := strings.TrimSpace(out)
	if i := strings.IndexAny(class, "\r\n"); i >= 0 {
		class = strings.TrimSpace(class[:i])
	}
	if class == "" || desktopWindowIsBrowser(class) {
		return false
	}
	return true
}

// desktopWhiskerDismiss finds the menu when Super keyup opened it. search
// with no match returns non-zero. That is the menu-not-open case.
var desktopWhiskerDismiss = []string{
	"search", "--onlyvisible", "--name", desktopWhiskerMenuName,
}

func dismissDesktopWhisker(run func(...string) (string, error)) {
	argv := make([]string, len(desktopWhiskerDismiss))
	copy(argv, desktopWhiskerDismiss)
	out, err := run(argv...)
	if err != nil || desktopXWindowID(out) == "" {
		return
	}
	// The plugin hides on Escape when the search entry is empty, which
	// is gtk_widget_hide. This call is its own process: key after search
	// would use the window stack and XSendEvent, and this GTK window
	// ignores that. A failed Escape must not cancel the click or type.
	_, _ = run("key", "Escape")
}

// desktopModifierRelease drops modifiers a noVNC viewer can leave held
// after it loses focus. Both sides are named. --delay 0 skips the default
// 12ms gap between those keys. A focus-only command does not send this.
var desktopModifierRelease = []string{
	"keyup", "--delay", "0",
	"Alt_L", "Alt_R",
	"Control_L", "Control_R",
	"Shift_L", "Shift_R",
	"Super_L", "Super_R",
}

// desktopAppSettle lets the window draw the click, type, or key before the
// screenshot. Those pixels are the ones a later app_run click uses.
const desktopAppSettle = 500 * time.Millisecond

// desktopAppWait sleeps, and returns early when the turn is cancelled.
// Tests replace it and check the duration.
var desktopAppWait = func(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	if ctx == nil {
		<-timer.C
		return
	}
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func desktopAppRunWithShot(ctx context.Context, scope agentruntime.Scope, display, actionText string) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return actionText, nil
	}
	desktopAppWait(ctx, desktopAppSettle)
	if ctx != nil && ctx.Err() != nil {
		return actionText, nil
	}
	shot, shotErr := desktopScreenshot(ctx, scope, display)
	if shotErr != nil {
		return actionText, nil
	}
	actionText = strings.TrimSpace(actionText)
	if actionText == "" {
		return shot, nil
	}
	return actionText + "\n" + shot, nil
}

func windowActivateStep(step []string) bool {
	for _, arg := range step {
		if arg == "windowactivate" {
			return true
		}
	}
	return false
}

// desktopXWindowID is the decimal X11 id printed by xdotool getactivewindow.
// A stub reply, a name, or zero is not a window to restore.
func desktopXWindowID(raw string) string {
	id := strings.TrimSpace(raw)
	if desktopXWindowIDPattern.MatchString(id) {
		return id
	}
	return ""
}

// desktopReadActiveWindow records the focused window before Super keyup can
// open the Whisker Menu. The menu's own id is not restored: activating it
// raises the menu again. A failed read is ignored and is not the tool result.
func desktopReadActiveWindow(run func(...string) (string, error)) string {
	argv := make([]string, len(desktopActiveWindowRead))
	copy(argv, desktopActiveWindowRead)
	out, err := run(argv...)
	if err != nil {
		return ""
	}
	return desktopRestoreWindowID(out)
}

// desktopRestoreWindowID parses "id\nname" from getactivewindow getwindowname.
// A missing name still restores the id. The Whisker Menu id does not.
func desktopRestoreWindowID(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 {
		return ""
	}
	id := desktopXWindowID(lines[0])
	if id == "" {
		return ""
	}
	if len(lines) > 1 && strings.TrimSpace(lines[1]) == desktopWhiskerMenuTitle {
		return ""
	}
	return id
}

var desktopXWindowIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)

func desktopArgvSendsKeys(step []string) bool {
	return len(step) > 0 && (step[0] == "type" || step[0] == "key")
}

func desktopArgvInsertsText(step []string) bool {
	if len(step) == 0 {
		return false
	}
	switch step[0] {
	case "type":
		return true
	case "key":
		return len(step) >= 2 && browser.KeyInsertsText(step[1])
	default:
		return false
	}
}

// desktopTypeMaxBytes keeps one xdotool type inside the two-minute app
// client. Each character waits 20ms.
const desktopTypeMaxBytes = 4000

// desktopTypeText checks one type step. Newlines stay in the text.
// expandTypeStep sends each line and its Enter in one xdotool call.
// xdotool type turns a newline into Linefeed, which the terminal does
// not treat as Enter, and a carriage return is not the Return key.
func desktopTypeText(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" || strings.ContainsRune(text, '\x00') {
		return "", fmt.Errorf("desktop app type requires text")
	}
	if len(text) > desktopTypeMaxBytes {
		return "", fmt.Errorf("desktop app type text is too long")
	}
	return text, nil
}

// expandTypeStep splits a type step into xdotool calls. A line and its
// break are one call: type the line, then key Return. Debian 12 xdotool
// stops type after --args N, so Return is a key in that same process
// and still sees an empty window stack. Without --args, type consumes
// key and types those letters. The man page says type always consumes
// the rest. "--" keeps a line that starts with "-" from being an option.
// A whitespace-only line is a blank line. desktopd rejects that
// argument, so the call is only key Return.
func expandTypeStep(step []string) ([][]string, error) {
	if len(step) < 2 {
		return nil, fmt.Errorf("desktop app type requires text")
	}
	text := step[len(step)-1]
	prefix := append(append([]string{}, step[:len(step)-1]...), "--args", "1", "--")
	parts := strings.Split(text, "\n")
	calls := make([][]string, 0, len(parts))
	for i, part := range parts {
		var call []string
		if strings.TrimSpace(part) != "" {
			call = append(append([]string{}, prefix...), part)
		}
		if i < len(parts)-1 {
			if len(call) == 0 {
				call = []string{"key", "Return"}
			} else {
				call = append(call, "key", "Return")
			}
		}
		if len(call) > 0 {
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("desktop app type requires text")
	}
	return calls, nil
}

// desktopScreenshotMaxBase64 is the largest screenshot placed on a model
// call or a chat reply. Hub and the GUI add one mebibyte to this when they
// read a message response. A larger body is cut mid-JSON, and the chat
// loses the text as well as the picture.
const desktopScreenshotMaxBase64 = 1_200_000

// desktopScreenshot captures the user's desktop. The text tells every model
// what was captured. The image is attached for vision models and never
// appears in the text, history, or logs (see agentruntime.AttachModelImage).
// The same image is kept on this request and copied onto the chat reply.
func desktopScreenshot(ctx context.Context, scope agentruntime.Scope, display string) (string, error) {
	return desktopCapture(ctx, scope, display, true, "")
}

func desktopScreenshotNamed(ctx context.Context, scope agentruntime.Scope, display, name string) (string, error) {
	return desktopCapture(ctx, scope, display, true, name)
}

// desktopCapture records one unscaled desktop image. clickPixels is set for
// action=screenshot and for the shot after an app_run input, where those
// pixels are the next click. A probe that found no element ref passes false:
// that image is only so the page can be seen, and a click sentence would
// tell the model to pixel-click the browser.
func desktopCapture(ctx context.Context, scope agentruntime.Scope, display string, clickPixels bool, name string) (string, error) {
	name = strings.TrimSpace(name)
	var data []byte
	var saved string
	var err error
	switch {
	case desktopRemoteScreenshot != nil:
		data, err = desktopRemoteScreenshot(ctx, scope.TenantID, scope.UserID, display)
		if err == nil && name != "" && desktopRemoteSavedPath != nil {
			saved = desktopRemoteSavedPath(name)
		}
	case desktopHubClientFromEnv() != nil:
		data, saved, err = desktopHubClientFromEnv().screenshot(ctx, scope.TenantID, scope.UserID, display, name)
	default:
		if err = refuseLocalDesktop(ctx); err != nil {
			return "", err
		}
		data, saved, err = desktopLocalScreenshotNamed(display, name)
	}
	if err != nil {
		return "", err
	}
	if name != "" && saved != desktop.DesktopShotPath(name) {
		saved = ""
	}
	if !desktop.IsPNG(data) {
		return "", fmt.Errorf("desktop screenshot is not a PNG image")
	}
	config, decodeErr := png.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil {
		return "", fmt.Errorf("desktop screenshot is not a PNG image")
	}
	mime, encoded, fits := desktopScreenshotEncode(data)
	shown := fits && noteDesktopShot(ctx, mime, encoded)
	visible := "The image could not be attached."
	if encoded != "" {
		visible = "The image is attached for models that can see images."
	}
	if shown {
		visible = "The image is attached for models that can see images and is shown in the chat."
	}
	kind := mime
	if kind == "" {
		kind = "image/png"
	}
	size := len(data)
	if encoded != "" {
		size = len(encoded)*3/4 + 1023
	}
	next := "If no image is visible to you, use probe for web pages and app_list for windows instead."
	if clickPixels {
		next = "app_run click uses these pixel coordinates. " + next
	}
	text := fmt.Sprintf("Desktop screenshot of display %s: %dx%d pixels (%s, %d KB). %s %s",
		strings.TrimSpace(display), config.Width, config.Height, kind, size/1024, visible, next)
	if saved != "" {
		text += " Saved " + saved + ". That is the only file this action wrote. Report this path."
		noteDesktopSavedPath(ctx, saved)
	} else if name != "" {
		text += " The screenshot was not written to the desktop. Do not tell the user a file path."
	}
	if encoded == "" {
		return text, nil
	}
	return agentruntime.AttachModelImage(text, mime, encoded), nil
}

// desktopShotSlot is this request's screenshot. The tool writes it and the
// message handler reads it. A later request has its own slot, so a picture
// cannot move onto the next reply or onto another instance's reply.
type desktopShotSlot struct {
	mu        sync.Mutex
	shot      agent.MessageAttachment
	ready     bool
	file      agent.MessageAttachment
	fileReady bool
	savedPath string
}

type desktopShotSlotKey struct{}

func withDesktopShotSlot(ctx context.Context) (context.Context, *desktopShotSlot) {
	if ctx == nil {
		ctx = context.Background()
	}
	slot := &desktopShotSlot{}
	return context.WithValue(ctx, desktopShotSlotKey{}, slot), slot
}

func desktopShotSlotFrom(ctx context.Context) *desktopShotSlot {
	if ctx == nil {
		return nil
	}
	slot, _ := ctx.Value(desktopShotSlotKey{}).(*desktopShotSlot)
	return slot
}

func noteDesktopSavedPath(ctx context.Context, path string) {
	path = strings.TrimSpace(path)
	slot := desktopShotSlotFrom(ctx)
	if slot == nil || path == "" {
		return
	}
	slot.mu.Lock()
	slot.savedPath = path
	slot.mu.Unlock()
}

func desktopSavedPath(slot *desktopShotSlot) string {
	if slot == nil {
		return ""
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return slot.savedPath
}

// correctDesktopPathClaim keeps a desktop file path in the reply only when
// this turn's screenshot save wrote that path. A path the tool did not write
// is removed, and the reply says the folder has no new file.
func correctDesktopPathClaim(text, saved string) string {
	saved = strings.TrimSpace(saved)
	if saved != "" {
		lines := strings.Split(text, "\n")
		kept := make([]string, 0, len(lines))
		for _, line := range lines {
			if (strings.Contains(line, "~/Desktop/") || strings.Contains(line, "/Desktop/")) && !strings.Contains(line, saved) {
				continue
			}
			kept = append(kept, line)
		}
		body := strings.TrimSpace(strings.Join(kept, "\n"))
		if strings.Contains(body, saved) {
			return body
		}
		if body == "" {
			return "文件已写到 " + saved + "。"
		}
		return body + "\n文件已写到 " + saved + "。"
	}
	if !strings.Contains(text, "~/Desktop/") && !strings.Contains(text, "/Desktop/") {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "~/Desktop/") || strings.Contains(line, "/Desktop/") || strings.Contains(line, "保存到桌面") {
			continue
		}
		kept = append(kept, line)
	}
	body := strings.TrimSpace(strings.Join(kept, "\n"))
	const note = "截图没有写到桌面文件夹。"
	if body == "" {
		return note
	}
	return body + "\n" + note
}

func publishDesktopReply(msg *agentservice.Message, slot *desktopShotSlot) (images, files int) {
	images = attachDesktopShot(msg, slot)
	files = attachDesktopFile(msg, slot)
	if msg != nil {
		msg.Content = correctDesktopPathClaim(msg.Content, desktopSavedPath(slot))
	}
	return images, files
}

func noteDesktopShot(ctx context.Context, mime, data string) bool {
	slot := desktopShotSlotFrom(ctx)
	if slot == nil {
		return false
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	data = strings.TrimSpace(data)
	if (mime != "image/png" && mime != "image/jpeg") || !desktopShotData(data) {
		return false
	}
	shot := agent.MessageAttachment{
		Type:     "image",
		FileName: desktopShotName(mime),
		MimeType: mime,
		Data:     data,
	}
	slot.mu.Lock()
	slot.shot = shot
	slot.ready = true
	slot.mu.Unlock()
	return true
}

func desktopShotName(mime string) string {
	if mime == "image/jpeg" {
		return "desktop.jpg"
	}
	return "desktop.png"
}

func deliverDesktopDocument(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	if filePath := desktopArg(args, "path"); filePath != "" {
		return deliverDesktopPath(ctx, scope, filePath, desktopArg(args, "name"))
	}
	name, mime, raw, err := desktopDocument(desktopArg(args, "name"), desktopArg(args, "content"))
	if err != nil {
		return "", err
	}
	if err := noteDesktopFile(ctx, name, mime, raw); err != nil {
		return "", err
	}
	return desktopDelivered(name, len(raw)), nil
}

// deliverDesktopPath attaches the file that is already on the desktop.
// The bytes are that file, including a pdf. The download name is its base
// name unless name is set.
func deliverDesktopPath(ctx context.Context, scope agentruntime.Scope, filePath, name string) (string, error) {
	resolved, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", fmt.Errorf("desktop deliver rejected the path: %w", err)
	}
	if name == "" {
		name = path.Base(resolved)
	}
	name, err = desktopDeliverName(name)
	if err != nil {
		return "", err
	}
	encoded, err := desktopContainerFile(ctx, scope, "file_bytes", map[string]any{"path": resolved})
	if err != nil {
		return "", err
	}
	raw, err := desktop.DecodeFileBytes(encoded)
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			return "", fmt.Errorf("desktop document is too large to send")
		}
		if strings.Contains(err.Error(), "empty") {
			return "", fmt.Errorf("desktop deliver file is empty")
		}
		return "", fmt.Errorf("desktop file could not be read")
	}
	if err := noteDesktopFile(ctx, name, desktopDeliverMIME(name), raw); err != nil {
		return "", err
	}
	return desktopDelivered(name, len(raw)), nil
}

func desktopDelivered(name string, size int) string {
	return fmt.Sprintf("Delivered %s (%d bytes) on this chat reply. Tell the user the file is attached to this message. Do not ask them to choose another way to receive it.", name, size)
}

func noteDesktopFile(ctx context.Context, name, mime string, raw []byte) error {
	slot := desktopShotSlotFrom(ctx)
	if slot == nil {
		return fmt.Errorf("desktop document could not be attached to this reply")
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if !desktopBoundedData(encoded, desktop.FileDeliverBase64Max) {
		return fmt.Errorf("desktop document is too large to send")
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if !desktopMIMEOK(mime) {
		return fmt.Errorf("desktop document could not be attached to this reply")
	}
	file := agent.MessageAttachment{
		Type:     "file",
		FileName: name,
		MimeType: mime,
		Data:     encoded,
		Size:     int64(len(raw)),
	}
	slot.mu.Lock()
	slot.file = file
	slot.fileReady = true
	slot.mu.Unlock()
	return nil
}

func desktopShotData(data string) bool {
	return desktopBoundedData(data, desktopScreenshotMaxBase64)
}

func desktopBoundedData(data string, max int) bool {
	if data == "" || len(data) > max || len(data)%4 != 0 {
		return false
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == '=' {
			if i < len(data)-2 {
				return false
			}
			continue
		}
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return false
		}
	}
	return true
}

// attachDesktopShot moves this request's screenshot onto the reply. The
// stored message is already a copy, so the image is only on the HTTP
// response. The slot is emptied either way, including when the reply has
// no message.
func attachDesktopShot(msg *agentservice.Message, slot *desktopShotSlot) int {
	if slot == nil {
		return 0
	}
	slot.mu.Lock()
	shot, ok := slot.shot, slot.ready
	slot.shot = agent.MessageAttachment{}
	slot.ready = false
	slot.mu.Unlock()
	if !ok || msg == nil || shot.Data == "" {
		return 0
	}
	next := make([]agent.MessageAttachment, len(msg.Attachments)+1)
	copy(next, msg.Attachments)
	next[len(msg.Attachments)] = shot
	msg.Attachments = next
	return 1
}

// attachDesktopFile moves this request's document onto the reply. The slot
// is emptied either way, including when the reply has no message.
func attachDesktopFile(msg *agentservice.Message, slot *desktopShotSlot) int {
	if slot == nil {
		return 0
	}
	slot.mu.Lock()
	file, ok := slot.file, slot.fileReady
	slot.file = agent.MessageAttachment{}
	slot.fileReady = false
	slot.mu.Unlock()
	if !ok || msg == nil || file.Data == "" {
		return 0
	}
	next := make([]agent.MessageAttachment, len(msg.Attachments)+1)
	copy(next, msg.Attachments)
	next[len(msg.Attachments)] = file
	msg.Attachments = next
	return 1
}

// desktopScreenshotEncode keeps the display's pixel grid. app_run clicks use
// those coordinates, so a screenshot is never scaled down. A PNG that fits
// stays a PNG. A larger one is re-encoded as JPEG at a lower quality until
// it fits the reply limit. The bool is false when the smallest full-size
// JPEG still exceeds that limit: the model can still see it, and the chat
// reply must not carry it.
func desktopScreenshotEncode(data []byte) (string, string, bool) {
	encoded := base64.StdEncoding.EncodeToString(data)
	if len(encoded) <= desktopScreenshotMaxBase64 {
		return "image/png", encoded, true
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", "", false
	}
	var best []byte
	for _, quality := range []int{80, 60, 45, 30, 20, 12} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			continue
		}
		if buf.Len() >= len(data) {
			continue
		}
		if best == nil || buf.Len() < len(best) {
			best = append([]byte(nil), buf.Bytes()...)
		}
		encoded = base64.StdEncoding.EncodeToString(buf.Bytes())
		if len(encoded) <= desktopScreenshotMaxBase64 {
			return "image/jpeg", encoded, true
		}
	}
	if best == nil {
		return "", "", false
	}
	return "image/jpeg", base64.StdEncoding.EncodeToString(best), false
}

// desktopLocalScreenshotRunner is set by tests.
var desktopLocalScreenshotRunner func(display string) (string, error)

// desktopLocalScreenshot captures the local desktop container (no Hub).
func desktopLocalScreenshot(display string) ([]byte, error) {
	data, _, err := desktopLocalScreenshotNamed(display, "")
	return data, err
}

// desktopLocalScreenshotNamed captures the display. A file name copies the PNG
// to /home/desktop/Desktop inside the same command. The test runner does not execute
// that copy, so a name then returns no path.
func desktopLocalScreenshotNamed(display, name string) ([]byte, string, error) {
	if !desktop.ValidDisplay(display) {
		return nil, "", fmt.Errorf("desktop display is invalid")
	}
	script, err := desktop.ScreenshotScriptFor(name)
	if err != nil {
		return nil, "", err
	}
	var out string
	if desktopLocalScreenshotRunner != nil {
		text, err := desktopLocalScreenshotRunner(display)
		if err != nil {
			return nil, "", err
		}
		out = text
	} else {
		raw, err := exec.Command("docker", "exec", "-e", "DISPLAY="+display, desktopContainerName(), "sh", "-c", script).Output()
		if err != nil {
			return nil, "", fmt.Errorf("desktop screenshot failed: %s", err.Error())
		}
		out = string(raw)
	}
	data, err := desktop.DecodeScreenshot(out)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(name) == "" || desktopLocalScreenshotRunner != nil {
		return data, "", nil
	}
	return data, desktop.DesktopShotPath(name), nil
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
			// --sync returns only after the window manager has focused this
			// window. The following type is a separate xdotool call.
			steps = append(steps, []string{"search", "--onlyvisible", "--name", name, "windowactivate", "--sync"})
		case "type":
			text, textErr := desktopTypeText(step.Text)
			if textErr != nil {
				return nil, textErr
			}
			// The text stays one argument. expandTypeStep adds --args and
			// "--", and puts key Return in that same call, when it runs.
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

func installDesktopPackages(ctx context.Context, scope agentruntime.Scope, packages []string) (string, error) {
	if desktopRemoteInstall != nil {
		return desktopRemoteInstall(ctx, scope.TenantID, scope.UserID, packages)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.Install(ctx, scope.TenantID, scope.UserID, packages)
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return "", err
	}
	return desktopInstallCommand(ctx, packages)
}

// desktopInstallRunner is set by tests. Production runs apt-get in the local
// desktop container when Hub is not configured.
var desktopInstallRunner func(ctx context.Context, packages []string) (string, error)

func desktopInstallCommand(ctx context.Context, packages []string) (string, error) {
	if desktopInstallRunner != nil {
		return desktopInstallRunner(ctx, packages)
	}
	ctx, cancel := desktop.BoundInstall(ctx)
	defer cancel()
	container := desktopContainerName()
	// The session ensure starts apt-mirror in the background before that
	// process takes the lock. Wait configures the sources.
	// A wait failure still falls through.
	wait := exec.CommandContext(ctx, "docker", desktop.AptMirrorWaitArgs(container)...)
	_, _ = wait.CombinedOutput()
	if stop := desktop.InstallStopError(ctx); stop != nil {
		return "", stop
	}
	text, err := desktop.RunPackageInstall(ctx, func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		raw, runErr := cmd.CombinedOutput()
		return strings.TrimSpace(string(raw)), runErr
	}, container, packages)
	if err != nil {
		if stop := desktop.InstallStopError(ctx); stop != nil {
			return "", stop
		}
		reason := desktop.AptFailureText(text, err)
		return reason, fmt.Errorf("desktop install failed: %s", reason)
	}
	return "installed: " + strings.Join(packages, " ") + "\n" + text, nil
}

// desktopOpenRunner is set by tests. Production starts the program in the
// local desktop container when Hub is not configured.
var desktopOpenRunner func(ctx context.Context, display, program string, args []string) (string, error)

func desktopOpenCommand(ctx context.Context, display, program string, args []string) (string, error) {
	if desktopOpenRunner != nil {
		return desktopOpenRunner(ctx, display, program, args)
	}
	argv, err := desktop.OpenExecArgs(desktopContainerName(), display, program, args)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "docker", argv...)
	out, runErr := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if runErr != nil {
		return text, fmt.Errorf("%s", desktop.OpenFailureText(program, text, runErr))
	}
	if text == "" {
		return desktop.OpenedText(program), nil
	}
	return text, nil
}

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
