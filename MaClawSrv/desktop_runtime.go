package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

const desktopRuntimeModuleID = "maclawsrv.desktop"
const desktopToolName = "desktop"
const desktopCDPEnv = "MACLAW_DESKTOP_CDP"
const desktopContainerEnv = "MACLAW_DESKTOP_CONTAINER"
const desktopSessionOwner = "maclawsrv-desktop"

// The desktop tool's action vocabulary is one list. Plan and execute advertise
// that same list. The plan allow-list is only the phase gate: a mutating call
// stays in the schema and returns the next-step sentence.
var desktopExecuteActions = []string{"batch", "rich_fill", "navigate", "probe", "click", "type", "press", "task_run", "script", "upload", "publish", "skill_list", "skill_run", "app_list", "app_run", "app_open", "install", "screenshot", "deliver", "file_read", "file_write", "file_edit", "file_list", "bash", "computer", "app_probe", "clipboard_get", "clipboard_set", "window_list", "window", "screen_info", "wait", "request_takeover", "notify_main", "check_inbox", "delegate", "shop_readback", "shop_compare_offers"}

var desktopPlanActions = []string{"probe", "screenshot", "app_list", "file_read", "file_list", "skill_list", "app_probe", "window_list", "screen_info", "clipboard_get", "wait", "notify_main", "check_inbox", "shop_readback", "shop_compare_offers"}

func desktopActionAllowed(action string) bool {
	for _, allowed := range desktopExecuteActions {
		if action == allowed {
			return true
		}
	}
	return false
}

func desktopPlanActionAllowed(action string) bool {
	for _, allowed := range desktopPlanActions {
		if action == allowed {
			return true
		}
	}
	return false
}

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
	// Plan and execute publish this same definition. A plan turn that calls a
	// mutating action gets the next-step sentence. The action stays listed.
	description := "Operate this user's cloud desktop. "
	def := agentruntime.ToolDefinition{
		Name: desktopToolName,
		Description: description +
			"Carry out the arrangement. Return three conclusion lines and one file path. Do not write the person's chat reply. " +
			"When the user asked for a file, action=deliver. A file already on this desktop, including a pdf, uses path, and the chat receives that file. name and content send the supplied text under that name. A name ending in .docx is a Word file built from that text. Say that it is attached to this message, or say what failed. Do not ask the user to choose a delivery method or to save the file themselves. Do not refuse a file because of its type. " +
			"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
			"The user message is the card for this helper. Follow it. The result is three conclusion lines and a file path. " +
			"Do the work in this turn. Do not stop to ask for another confirmation, except before submitting an order or paying: stop there, tell the user the items and the total, and wait for them to reply 确认支付 with the amount. A cookie notice is accepted in the script. A password, phone number, or verification code is typed into the input that appears in this chat, then filled in; do not ask the person to type it on the desktop. Hand the desktop back only for a QR-code login, a captcha, or a payment confirmation. After they hand it back, continue the same task on the page that is already open. Do not open another tab of that page. " +
			"Bots of this user are different instances of the same MaClawSrv user and share this desktop. Other users cannot see it. " +
			"For web pages use the browser inside that desktop: action=probe once, then action=script. " +
			"The script is statements that click, type, select, press, scroll, and waitFor every control that probe listed. Do not probe or click one control per turn. " +
			"click(\"保存\") or click(\"button.primary\") clicks. type(\"#q\", \"weather\") types. select(\"状态\", \"完成\") chooses an option. press(\"Enter\"), scroll(600), and waitFor(\"下一步\", 3000) stay in that same script. " +
			"The script must not read cookies, storage, or passwords, and it must not type into a password or card field. " +
			"Pass name as kebab-case, such as save-draft, to keep a script that worked. action=skill_list shows saved scripts. action=skill_run with that name runs one again without another probe. " +
			"action=task_run remains for a short ref sequence. Do not click the browser window with pixels. " +
			"A probe with no filter that finds no element ref includes one desktop image so you can see the page. If a filter matches nothing, probe again without it. The next web step is still an element ref, not a pixel click. " +
			"For other applications, action=app_open with program starts that program in a window on this desktop. xfce4-terminal is already installed. xterm is another terminal when it is installed. app_open is not a shell. The new window does not use the Chinese input method, so ASCII typed into it is literal. Then action=app_list, then action=app_run with steps focus, type, key, or click. One type step can contain several lines. The terminal is a shell, so write the source as a heredoc in that step and compile on the next line. app_run focus only activates a window that is already open. exit status 1 from focus means that window is not open; start it with app_open. " +
			"action=install with packages installs those Debian packages in this desktop container. " +
			"action=file_read, file_write, file_edit, and file_list use a path inside this container, such as /home/desktop/Desktop/a.c or ~/Desktop/a.c. action=bash runs one command in this container and returns its output. A one-off script is file_write, then bash. " +
			"A machine outside this desktop uses the ssh tool: connect, then exec or exec_background. A command that should keep running uses exec_background, then check_task. action=bash stays inside this container. " +
			"action=screenshot returns an image of the whole desktop; its pixel coordinates are the ones app_run click uses. " +
			desktopScreenVisionPrompt(desktopTurnSeesImages(request)) +
			"To put that image on the desktop, set name to one file name ending in .png. The file is written to /home/desktop/Desktop and the result says Saved with that path. Without name, nothing is written. Do not invent a file path. " +
			"After an app_run that clicks, types, or sends a key, the result includes one fresh unscaled desktop image. app_run click uses those pixels. " +
			"Single browser actions navigate, click, type, and press remain available. " +
			"Publishing a post/article on any site (editor page already open): ONE call action=publish with title=, body= (full text), files=[images] if the post needs them, submit_texts=[the exact publish button text] and verify_url= (a page where the new post is listed, if known). It uploads, fills title and body, reads them back, clicks publish (shadow DOM included), opens the result and verifies; each step has its own check and time budget. On failure it names the stuck step: fix only that parameter (e.g. title_selector, body_selector, upload_trigger) and call publish again; never re-fill by hand. " +
			"File upload on any site: action=upload with files=[absolute paths] (and text= or ref= of the page's upload button when the file input only appears after a click). It sets the files on the page's file input over CDP (shadow DOM and frames included) and never opens the OS file chooser; use it instead of clicking an upload button and typing a path into a dialog. " +
			"For non-browser windows: action=app_probe lists the accessible elements (role, name, states, bounds) of the focused window with numbered ids; app_run or computer steps then target {\"id\":N} (click, focus, set_text). marks=true adds a screenshot with those numbers drawn. " +
			"action=computer runs a batch of computer-use actions (Anthropic or OpenAI names) and reports each step; it stops at the first failure. " +
			"Batch mode (DEFAULT for any multi-step browser/form flow on every site): observe once (one probe or one script that reads the page), then ONE action=batch whose plan is the whole sequence as desktop steps, e.g. [{action:script,script:fill title},{action:rich_fill,text:BODY},{action:script,script:set topics},{action:script,script:click 发布}], then ONE read-back script on the result page. batch runs the steps in order through the same guards, stops at the first failure and reports each step; only then fix the failed step alone. " +
			"For a rich-text editor body on any site use ONE action=rich_fill with the full text (replace=true to overwrite); it focuses the page's main editor itself (ref, or x,y in SCREEN pixels, only for a different editor), raises the browser, pastes with a real Ctrl+V and reads back what the field now holds (len, has=yes/no). For a title use rich_fill with selector=<title input>. Once title and body read back has=yes, the next step is the submit/publish click, not more fills or probes. Coordinates a page script reports are viewport/CSS pixels, not screen pixels: click page elements by ref, never by script coordinates. " +
			"action=clipboard_set then a paste step (or app_run paste{text}) inserts long or Chinese text without the input method. clipboard_get reads it. window_list, window (activate, maximize, minimize, restore, move, resize, close), screen_info, and wait (for screen change or a window) manage windows. " +
			"action=screenshot accepts region (zoom), format jpeg/webp, quality, and cursor. Full screenshots always come at one fixed size, so coordinates keep the same scale from shot to shot. " +
			"navigate to another site keeps the current tab and opens the target in its own tab (or switches to a tab already on that site); do not retry a navigate that was refused, and do not detour through redirect links. " +
			desktopAXFirstPrompt() +
			"Rich-text editors (Zhihu, WeChat, Xiaohongshu, Draft.js/ProseMirror boxes): script- or DOM-set text and synthetic paste are often ignored. Click inside the editor first, put the text on the clipboard with clipboard_set, then paste with a real key event (app_run steps [click{x,y}, key{key:\"ctrl+v\"}] or paste{text}), then check that the text shows and that the publish (发布) button is enabled. If the same method fails twice, switch to another method instead of retrying it. " +
			"When a site needs a login, password, 2FA/SMS code, or a captcha, do not try to get past it yourself: call action=request_takeover with reason, then stop and end your turn, telling the person what to do on the desktop and to press hand back (交还) when done. You resume after the hand back. " +
			"action=notify_main with kind and message tells the account's main bot (or target, another bot) something mid-task: a milestone (progress), a question, being blocked, a file ready (artifact, with paths), or done. It does not end your turn and does not wait for an answer; keep working. Use it sparingly: at most a few per task, never to acknowledge. action=check_inbox reads new messages other bots left for you on Hub (answers to your question, new instructions); messages that arrived before this turn are already in the instructions. action=delegate with target (a teammate bot's name) and message hands a task to that bot (main bot only); Hub starts it and its result comes back to your mailbox.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":          map[string]any{"type": "string", "description": "probe, script, upload, skill_list, skill_run, task_run, app_list, app_run, app_open, install, screenshot, navigate, click, type, press, deliver, file_read, file_write, file_edit, file_list, bash, computer, app_probe, clipboard_get, clipboard_set, window_list, window, screen_info, wait, request_takeover, notify_main, check_inbox, delegate, shop_readback (read-only cart/orders/tracking summary of the current shopping page), or shop_compare_offers (rank offers[] by price+shipping). Posting to a site: prepare the title, body and pictures with bash under /tmp, open the editor, then call action=publish (it uploads, fills, submits and reads back); a publish task follows a fixed plan and refuses steps outside its phase", "enum": desktopExecuteActions},
				"kind":            map[string]any{"type": "string", "description": "notify_main: done, blocked, progress, question, artifact, info, or reply.", "enum": []string{"done", "blocked", "progress", "question", "artifact", "info", "reply"}},
				"message":         map[string]any{"type": "string", "description": "notify_main: one or two sentences for the main bot."},
				"target":          map[string]any{"type": "string", "description": "notify_main: empty (the main bot) or another bot's name or id. delegate: the teammate bot's name or id."},
				"paths":           map[string]any{"type": "array", "description": "notify_main: desktop file paths you hand over.", "items": map[string]any{"type": "string"}},
				"reply_to":        map[string]any{"type": "string", "description": "notify_main: id of the message you answer."},
				"reason":          map[string]any{"type": "string", "description": "request_takeover: one short sentence for the person, what they need to do on this desktop (log in, enter the 2FA code, solve the captcha)."},
				"path":            map[string]any{"type": "string", "description": "Container path. file_read, file_write, file_edit, and file_list use it as the file. action=deliver attaches that existing file, such as /home/desktop/Desktop/北京天气.pdf. /home/desktop/Desktop/a.c and ~/Desktop/a.c are the same folder."},
				"old_string":      map[string]any{"type": "string", "description": "Exact passage file_edit replaces. It must occur once."},
				"new_string":      map[string]any{"type": "string", "description": "Replacement text for file_edit."},
				"command":         map[string]any{"type": "string", "description": "Command for action=bash. It runs in this container and the result is its output."},
				"program":         map[string]any{"type": "string", "description": "Program for action=app_open. One name or an absolute path, such as xfce4-terminal. Not a shell and not a browser."},
				"args":            map[string]any{"type": "array", "description": "Arguments for action=app_open. Not a shell command.", "items": map[string]any{"type": "string"}},
				"packages":        map[string]any{"type": "array", "description": "Debian package names for action=install, such as libreoffice", "items": map[string]any{"type": "string"}},
				"name":            map[string]any{"type": "string", "description": "Download name for action=deliver. Any ordinary file name. When path is empty, the chat receives the text in content under this name. action=screenshot: ends in .png and is written to /home/desktop/Desktop. action=script stores the script under a kebab-case name, such as save-draft, when it succeeds. action=skill_run runs that saved script."},
				"content":         map[string]any{"type": "string", "description": "Text for action=deliver when path is empty, or the full file text for action=file_write. A file already on the desktop uses path."},
				"url":             map[string]any{"type": "string", "description": "URL for navigate"},
				"ref":             map[string]any{"type": "string", "description": "Element ref from probe, such as e3"},
				"snapshot_id":     map[string]any{"type": "string", "description": "snapshot_id returned with the ref"},
				"text":            map[string]any{"type": "string", "description": "Text for type, or visible text for click when ref is empty"},
				"replace":         map[string]any{"type": "boolean", "description": "rich_fill: select all in the editor first so the text replaces it"},
				"selector":        map[string]any{"type": "string", "description": "rich_fill: CSS selector of the field to fill when it is not the page's main editor, e.g. the title input (input/textarea/contenteditable)"},
				"key":             map[string]any{"type": "string", "description": "Key for press"},
				"query":           map[string]any{"type": "string", "description": "Optional probe filter. Leave it empty to list every control."},
				"script":          map[string]any{"type": "string", "description": "Statements for action=script. click(\"保存\") or click(\"button.primary\") clicks. type(\"#q\", \"weather\") types. select(\"状态\", \"完成\") chooses an option. press(\"Enter\"), scroll(600), and waitFor(\"下一步\", 3000) run in the same script. Do not read cookies, storage, or passwords."},
				"files":           map[string]any{"type": "array", "description": "upload: absolute file paths on the desktop to put into the page's file input. publish: the pictures, in the order the body's [[img:N]] anchors name them", "items": map[string]any{"type": "string"}},
				"body":            map[string]any{"type": "string", "description": "publish: full body. Write it structured in light Markdown: ## headings per section, **bold** on each key point and number, - lists, > quotes. Put each picture on its own line where it belongs: [[img:N|caption]] places files[N-1] there (rich editors get formatted text with every picture at its anchor; note sites with a picture gallery get plain text with bold as 【…】). The read-back fails the publish if a picture is missing or out of place or the formatting is lost."},
				"title_selector":  map[string]any{"type": "string", "description": "publish: CSS selector of the title field if not auto-found"},
				"body_selector":   map[string]any{"type": "string", "description": "publish: CSS selector of the body editor if not auto-found"},
				"upload_selector": map[string]any{"type": "string", "description": "publish: CSS selector of the file input"},
				"upload_trigger":  map[string]any{"type": "string", "description": "publish: visible text of the upload button when there is no file input yet"},
				"submit_texts":    map[string]any{"type": "array", "description": "publish: exact text of the publish button(s), e.g. [\"发布\"]", "items": map[string]any{"type": "string"}},
				"confirm_texts":   map[string]any{"type": "array", "description": "publish: exact text of a second confirm button if the site asks", "items": map[string]any{"type": "string"}},
				"verify_url":      map[string]any{"type": "string", "description": "publish: page to open after submit where the new post must be listed"},
				"edit_url":        map[string]any{"type": "string", "description": "publish: edit page of a published post ({url} = post URL), used to put back a lost picture"},
				"plan":            map[string]any{"type": "array", "description": "batch: desktop steps run in order in one call, each an object with its own action (script, rich_fill, navigate, probe, click, type, press, wait) and that action's fields; stops at the first failure", "items": map[string]any{"type": "object"}},
				"offers":          desktopShopOffersSchema(),
				"steps":           map[string]any{"type": "array", "description": "task_run or app_run steps. Browser step: {action, params:{url,ref,text,key}}. App step: {action, ...} with action focus{name|id}, type{text}, key{key, repeat}, click{x,y | id, button left|right|middle, clicks 1-3, modifiers \"ctrl+shift\"}, right_click, double_click, triple_click, middle_click, move{x,y}, mouse_down, mouse_up, drag{from_x,from_y,to_x,to_y}, scroll{x,y,direction up|down|left|right, amount}, cursor_position, wait{duration}, hold_key{key,duration}, paste{text} (clipboard + Ctrl+V, best for long or Chinese text), set_text{id,text}, screenshot, zoom{region:[x0,y0,x1,y1]}. x,y are pixels of the last screenshot.", "items": map[string]any{"type": "object"}},
				"actions":         map[string]any{"type": "array", "description": "action=computer: a batch run in order that stops at the first failure. Each item is an Anthropic computer-use action ({action:left_click, coordinate:[x,y], text:modifiers}, left_click_drag with start_coordinate, scroll with scroll_direction/scroll_amount, key/hold_key with text, type, cursor_position, wait, screenshot, zoom with region), an OpenAI computer-use action ({type:click|double_click|scroll|type|keypress|wait|screenshot|move|drag, x, y, button, scroll_x, scroll_y, keys, path}), or an app_run step. A click may give target (words, e.g. \"the blue 发送 button\") instead of x,y as the first step of a batch: a grounding model places it when one is configured; otherwise give x,y.", "items": map[string]any{"type": "object"}},
				"max_width":       map[string]any{"type": "integer", "description": "Ignored for full screenshots, which use one fixed size (MACLAW_DESKTOP_SHOT_WIDTH, default native). Kept for compatibility."},
				"max_height":      map[string]any{"type": "integer", "description": "Like max_width for the height."},
				"format":          map[string]any{"type": "string", "description": "Screenshot format: png (default), jpeg, or webp.", "enum": []string{"png", "jpeg", "webp"}},
				"quality":         map[string]any{"type": "integer", "description": "jpeg/webp quality 1-100 (default 80)."},
				"cursor":          map[string]any{"type": "boolean", "description": "Draw the mouse pointer on the screenshot."},
				"marks":           map[string]any{"type": "boolean", "description": "app_probe or screenshot: draw numbered boxes of the last app_probe elements."},
				"region":          map[string]any{"type": "array", "description": "action=screenshot zoom: [x0,y0,x1,y1] in pixels of the last full screenshot.", "items": map[string]any{"type": "number"}},
				"window":          map[string]any{"type": "string", "description": "app_probe: focused (default), all, or part of a window title."},
				"max_nodes":       map[string]any{"type": "integer", "description": "app_probe: most elements to list (default 120, at most 400)."},
				"do":              map[string]any{"type": "string", "description": "action=window: activate, maximize, minimize, restore, move, resize, move_resize, or close."},
				"window_id":       map[string]any{"type": "string", "description": "action=window: id from window_list. title also selects a window."},
				"title":           map[string]any{"type": "string", "description": "action=window or wait: part of a window title. publish: the full post title."},
				"x":               map[string]any{"type": "number", "description": "action=window move: left edge in screenshot pixels."},
				"y":               map[string]any{"type": "number", "description": "action=window move: top edge in screenshot pixels."},
				"width":           map[string]any{"type": "number", "description": "action=window resize: width in screenshot pixels."},
				"height":          map[string]any{"type": "number", "description": "action=window resize: height in screenshot pixels."},
				"for":             map[string]any{"type": "string", "description": "action=wait: change (the screen changes) or window (a window with title appears; gone=true waits for it to close)."},
				"timeout":         map[string]any{"type": "number", "description": "action=wait: seconds, at most 60."},
			},
			"required": []string{"action"},
		},
	}
	if desktopMainAgentTurn(request) {
		def = desktopStripMainDriving(def)
	}
	return []agentruntime.ToolDefinition{def}, nil
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
	ctx = context.WithValue(ctx, desktopPhaseKey{}, desktopTurnPhase(request))
	ctx = context.WithValue(ctx, desktopLLMSeesImagesKey{}, desktopTurnSeesImages(request))
	ctx = context.WithValue(ctx, desktopComputerHelperKey{}, strings.TrimSpace(desktopMessageMeta(request)["computer_helper"]) == "1")
	ctx = context.WithValue(ctx, desktopPublishApprovedKey{}, desktopTurnPublishApproved(request))
	scope, cloud, err := desktopScopeForTurn(request)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(desktopArg(args, "action")), "notify_main") {
		return desktopNotifyMain(ctx, request, args)
	}
	if strings.EqualFold(strings.TrimSpace(desktopArg(args, "action")), "delegate") {
		return desktopDelegate(ctx, request, args)
	}
	if strings.EqualFold(strings.TrimSpace(desktopArg(args, "action")), "check_inbox") {
		if text := desktopInboxText(ctx, request); text != "" {
			return text, nil
		}
		return "No new messages from other bots.", nil
	}
	if strings.EqualFold(strings.TrimSpace(desktopArg(args, "action")), "batch") {
		ctx = context.WithValue(ctx, desktopInsideBatchKey{}, true)
		desktopMarkBatchStarted(desktopOwner(scope))
		return desktopRuntimeModule{}.invokePlan(ctx, request, name, args)
	}
	if inst, ok := turnInstance(request); ok && inst.Metadata != nil {
		if folder := strings.TrimSpace(inst.Metadata[desktopMaterialsMetadataKey]); folder != "" {
			ctx = context.WithValue(ctx, desktopMaterialsFolderKey{}, folder)
		}
	}
	if cloud {
		ctx = context.WithValue(ctx, desktopCloudKey{}, true)
		ctx = context.WithValue(ctx, desktopPersonKey{}, desktopAccount{TenantID: scope.TenantID, UserID: scope.UserID})
	}
	if fixed, ok := desktopBrowserOpenAsNavigate(args); ok {
		log.Printf("[desktop-step] app_open of the browser with a URL runs as navigate")
		args = fixed
	}
	if fixed, ok := recoverDesktopLeakedArgs(args); ok {
		log.Printf("[desktop-step] recovered leaked tool-call markup into action=%s", desktopArg(fixed, "action"))
		args = fixed
	}
	if desktopMainAgentTurn(request) && desktopbot.MainDropsTool(strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))) {
		return "", fmt.Errorf("这一步由操作电脑的帮手做")
	}
	desktopTidyOnce(ctx, desktopOwner(scope), strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))))
	if err := desktopBudgetCheck(desktopOwner(scope), strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))); err != nil {
		log.Print(desktopStepLogLine(scope.UserID, args, "", err, 0) + " model=" + desktopModelLabel(request) + " " + desktopBudgetLogFields(desktopOwner(scope)))
		desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), "", err, 0, "budget")
		return "", err
	}
	repeatWarn, repeatErr := desktopRepeatCheck(desktopOwner(scope), strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))), args)
	if repeatErr != nil {
		log.Print(desktopStepLogLine(scope.UserID, args, "", repeatErr, 0) + " model=" + desktopModelLabel(request) + " " + desktopBudgetLogFields(desktopOwner(scope)) + " repeat=refused")
		desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), "", repeatErr, 0, "refused")
		return "", repeatErr
	}
	if pubErr := desktopPublishSideCheck(ctx, strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))), args); pubErr != nil {
		log.Print(desktopStepLogLine(scope.UserID, args, "", pubErr, 0) + " model=" + desktopModelLabel(request) + " publish=refused")
		desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), "", pubErr, 0, "guard")
		return "", pubErr
	}
	if failErr := desktopFailClassCheck(desktopOwner(scope), strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))), args); failErr != nil {
		log.Print(desktopStepLogLine(scope.UserID, args, "", failErr, 0) + " model=" + desktopModelLabel(request) + " " + desktopBudgetLogFields(desktopOwner(scope)) + " failclass=refused")
		desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), "", failErr, 0, "guard")
		return "", failErr
	}
	started := time.Now()
	desktopEmitStepStart(scope, desktopOwner(scope), args, desktopModelLabel(request))
	actionName := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	npErr := desktopNoProgressCheckStep(ctx, desktopOwner(scope), actionName, args)
	if npErr == nil {
		npErr = desktopBatchDefaultCheck(ctx, desktopOwner(scope), actionName)
	}
	if npErr == nil {
		npErr = desktopAfterPublishCheck(desktopOwner(scope), actionName, args)
	}
	if npErr == nil {
		npErr = desktopDriftCheck(desktopOwner(scope), actionName, args, desktopMessageText(request))
	}
	if npErr == nil && actionName == "deliver" {
		npErr = desktopAfterPublishDeliverCheck(desktopOwner(scope), desktopMessageText(request))
	}
	if npErr != nil {
		desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), "", npErr, 0, "guard")
		return "", npErr
	}
	var out string
	if actionName == "rich_fill" {
		out, err = desktopRichFill(ctx, scope, args)
	} else {
		out, err = operateDesktopWithTimeout(ctx, scope, args)
	}
	noteDesktopPageNeed(scope, out)
	err = desktopDeliverSizeHint(args, err)
	err = desktopbot.ModelFacingError(err)
	took := time.Since(started)
	if err == nil && strings.EqualFold(strings.TrimSpace(desktopArg(args, "action")), "clipboard_set") {
		desktopNoteClipboard(desktopOwner(scope), args)
	}
	desktopRecordStep(ctx, desktopOwner(scope), args, out, err, took)
	desktopExperienceRecord(scope, desktopRunSite(desktopOwner(scope)), strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))), args, out, err)
	failWarn := desktopFailClassRecord(desktopOwner(scope), strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))), args, out, err)
	desktopNotePublishAttempt(desktopOwner(scope), actionName, args)
	desktopNoteMedia(scope, actionName, args, out, err)
	desktopRecordBatchDefault(desktopOwner(scope), actionName, err)
	desktopRecipeObserve(desktopOwner(scope), actionName, args, out, err)
	if err == nil && (actionName == "deliver" || strings.Contains(out, desktopAppliedMarker)) {
		// Generic verified outcome: a delivered file, or any write that a
		// read-back confirmed (every site), validates the gathering recipe.
		desktopRecipeValidate(desktopOwner(scope))
	}
	coach := desktopProgressNote(desktopOwner(scope), actionName, out, err)
	if err == nil {
		out = spillDesktopToolText(ctx, scope, out, actionName)
		out += repeatWarn + failWarn + coach
	} else if failWarn != "" {
		err = fmt.Errorf("%w%s", err, failWarn)
	}
	log.Print(desktopStepLogLine(scope.UserID, args, out, err, took) + " model=" + desktopModelLabel(request) + " " + desktopBudgetLogFields(desktopOwner(scope)))
	desktopEmitStep(scope, desktopOwner(scope), args, desktopModelLabel(request), out, err, took, "")
	return out, err
}

// spillDesktopToolText writes a long desktop result into the container and
// leaves the path plus one line in the conversation. A failed write keeps the
// original text. The executor then sees a short string and does not spill again.
func spillDesktopToolText(ctx context.Context, scope agentruntime.Scope, body string, action string) string {
	if utf8.RuneCountInString(body) <= desktopSpillLimit(action) {
		return body
	}
	// A deliver record stays whole in the conversation: the outcome check
	// reads it to know a file already reached the person.
	if strings.HasPrefix(strings.TrimSpace(body), "Delivered ") {
		return body
	}
	taskID := strings.TrimSpace(scope.SessionID)
	if taskID == "" {
		taskID = strings.TrimSpace(scope.RunID)
	}
	if taskID == "" {
		taskID = strings.TrimSpace(scope.InstanceID)
	}
	seq := desktopbot.NextSpillSeq(scope.InstanceID, taskID)
	path := desktopbot.ToolResultPath(taskID, seq)
	payload := body
	if len(payload) > desktop.FileContentMax {
		payload = truncateDesktopSpill(payload, desktop.FileContentMax)
	}
	if _, err := desktopContainerFile(ctx, scope, "file_write", map[string]any{"path": path, "content": payload}); err != nil {
		return body
	}
	return desktopbot.ToolResultShown(path, body)
}

// desktopSpillLimit: gathered material (bash/file_read/script output) stays
// inline up to a larger cap. With the 4000-rune cap a 8 KB source dump was
// spilled to a file, and reading that file spilled again, so the model paged
// it with head/sed/file_read for many steps (RUN30/RUN31). Screen/page
// results keep the small cap.
func desktopSpillLimit(action string) int {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "bash", "file_read", "script":
		return desktopbot.ToolResultSpillRunes * 6
	}
	return desktopbot.ToolResultSpillRunes
}

func truncateDesktopSpill(s string, max int) string {
	if len(s) <= max || max <= 0 {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func (desktopRuntimeModule) ContributePrompt(ctx context.Context, request agentruntime.TurnRequest) (string, error) {
	if !desktopAvailable() || !desktopTurnUsesContainer(request) {
		return "", nil
	}
	if botPhaseFromTurn(request) == "plan" {
		return "The current user has one cloud desktop through the desktop tool. " +
			"This turn only inspects it. " +
			"Use only probe, screenshot, app_list, file_read, file_list, or skill_list. That limit is this turn only. " +
			"action=skill_list shows browser scripts already saved. This turn does not run them. " +
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
			desktopScreenVisionPrompt(desktopTurnSeesImages(request)) +
			"app_list, probe, and screenshot also list every open browser page, including tabs behind that window. probe reads only the attached page. " +
			"If the page needs a password, phone number, or verification code, the arrangement says this chat will ask for it. A cookie notice is accepted while carrying out the work. Only a QR-code login, a captcha, or a payment confirmation is finished by the person on this computer. Describe that and stop. Do not keep looking. " +
			"This user's bots share this desktop. Other users have separate desktops.", nil
	}
	prompt := "The current user has one cloud desktop through the desktop tool. "
	if scope, _, err := desktopScopeForTurn(request); err == nil {
		prompt += desktopExperienceText(scope, "")
		task := desktopTurnTask(request)
		desktopRecipeBegin(scope, task)
		prompt += desktopRecipeText(scope, task)
	}
	prompt += desktopInboxText(ctx, request)
	return prompt +
		"Carry out the arrangement. Return three conclusion lines and one file path. Do not write the person's chat reply. " +
		"When the user asked for a file, action=deliver. A file already on this desktop, including a pdf, uses path, and the chat receives that file. name and content send the supplied text under that name. A name ending in .docx is a Word file built from that text. Say that it is attached to this message, or say what failed. Do not ask the user to choose a delivery method or to save the file themselves. Do not refuse a file because of its type. " +
		"Do not paste the document into the chat, and do not tell the user to copy it into Word. " +
		"The user message is the card for this helper. Follow it. The result is three conclusion lines and a file path. " +
		"When this phase is already execute, wording left in that order such as 本回合不要, 只说明, 等你点头, 确认后, or 下一轮 is leftover arrangement wording, so carry out the actions and do not ask for confirmation again. " +
		"Do the work in this turn. Do not stop to ask for another confirmation, except before submitting an order or paying: stop there, tell the user the items and the total, and wait for them to reply 确认支付 with the amount. A cookie notice is accepted in the script. A password, phone number, or verification code is typed into the input that appears in this chat, then filled in; do not ask the person to type it on the desktop. Hand the desktop back only for a QR-code login, a captcha, or a payment confirmation. After they hand it back, continue the same task on the page that is already open. Do not open another tab of that page. " +
		"The person and this agent share that desktop's browser window, so a login or verification finished by the person stays in this browser. " +
		"A window that only shows a network error is not that session when another page has already loaded. " +
		"app_list, probe, and screenshot list every open browser page, including tabs that are not in front. probe reads only the attached page. " +
		"After they hand it back, continue the original task in this browser. Other pages of this site keep the website login. If this site opens a window, continue there. Do not open another site, profile, or private session. " +
		"web_search and web_fetch use this desktop's network. A site the user may have signed into stays in this desktop browser, because those two tools do not see that login. " +
		"Files and commands use action=file_read, file_write, file_edit, file_list, and action=bash inside this container. " +
		"A machine outside this desktop uses the ssh tool: connect, then exec or exec_background. A command that should keep running uses exec_background, then check_task. action=bash stays inside this container. " +
		"This user's bots are different instances of the same MaClawSrv user, and those instances share this desktop. Other users have separate desktops. " +
		"Web pages: probe once. When the controls are listed, one action=script clicks, types, selects, presses, and scrolls them, including a cookie accept button. Do not probe again between those steps. name on that script keeps it. skill_list then skill_run repeats a saved script without another probe. A password, phone number, or verification code is requested in this chat and then filled in. Hand the desktop back only for a QR-code login, a captcha, or a payment confirmation. Navigate only to another page of this site. Do not pixel-click the browser. " +
		"A probe with no filter that finds no element ref includes one desktop image so you can see the page. If a filter matches nothing, probe again without it. The next web step is still an element ref, not a pixel click. " +
		"Other apps: action=app_open with program starts a window. xfce4-terminal is already installed. app_open is not a shell. That window does not use the Chinese input method, so ASCII typed into it is literal. app_run focus only activates a window that is already open; exit status 1 means that window is not open. One type step can contain several lines. The terminal is a shell, so a line break runs that line. Write the source as a heredoc in that step, then compile on the next line. Then app_list and one app_run. After click, type, or key, that result includes one fresh desktop image. app_run click uses those pixels. " + desktopScreenVisionClickPrompt(desktopTurnSeesImages(request)) + desktopScreenCheckPrompt(desktopTurnSeesImages(request)) + "Use screenshot when app_list is not enough, or to check a result before any input. " +
		"To put a screenshot on the desktop, action=screenshot with name set to one .png file name. The result line Saved is the file that was written under /home/desktop/Desktop. If the result says the screenshot was not written, the desktop folder has no new file. Do not invent a path. " +
		desktopDocumentPrompt +
		"To install software, action=install with packages set to Debian package names. That runs apt-get update and apt-get install -y in this desktop container. " +
		"Do not claim the screen changed until the tool result shows the new page or window.", nil
}

// desktopDocumentPrompt steers Office/document tasks to the fast, reliable
// path. Seen live: a "write an illustrated Office document" task spent 30+
// model rounds re-editing a generator script and never reached a file.
const desktopDocumentPrompt = "Office documents (Word, report, illustrated document): build the file in one action=bash step with python3 and python-docx (installed), or write ODT/HTML and convert with soffice --headless --convert-to docx. " +
	"To check a downloaded picture, use ImageMagick identify (installed); older desktops may lack file and xxd. " +
	"Save it under /home/desktop/Desktop with a fixed name, insert pictures with their file paths (add_picture), and print the saved path and size in the same step. " +
	"If that step fails, fix the error it printed and run it again; do not rewrite the whole script for each small change. " +
	"Do not type the document into the LibreOffice window key by key, and do not drive its Save As dialog. " +
	"Before reporting, app_open with program libreoffice and the saved path so the front window shows the finished document: the result check compares your report with that screen, and a run whose screen still shows an older page is sent back. " +
	"deliver attaches at most about 145 KB, so shrink pictures before inserting them (ImageMagick: convert in.png -resize 800x800> -quality 70 out.jpg) and check the saved size. " +
	"Do not write your own CDP/DevTools or headless-browser scripts in bash to read or screenshot pages; use navigate, probe, script, and screenshot, which work on the visible browser and are faster. " +
	"The person watches this desktop: when you write or edit a document, open it visibly in its program (app_open, e.g. lowriter ~/Desktop/file.docx) so its window is in front, and keep the browser behind unless the step is a browser step; headless conversion is only for the final export. " +
	"If LibreOffice shows a document-recovery, tip, or first-start dialog, answer that dialog; never delete ~/.config/libreoffice or other profile folders, which hold the person's settings. " +
	"Then action=deliver with that path. Chinese text goes into the file content or through clipboard_set, never through typed keystrokes. " +
	"For a dialog or button in a non-browser window, use app_probe and its element ids before pixel clicks; keyboard shortcuts (ctrl+s, ctrl+o, alt+f4 only on windows you opened) beat menus. "

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
	desktopBindings   sync.Map
	desktopRoundWakes sync.Map
	desktopEnsureFn   = ensureUserDesktop
)

// desktopRoundWake is one round's machine wake. The first tool that needs
// the machine fills it. Later tools in that round reuse it.
type desktopRoundWake struct {
	mu       sync.Mutex
	woken    bool
	have     bool
	endpoint desktopEndpoint
	// userKey is the desktop (owner, or owner plus bot when isolated) whose
	// browser binding this round used. finishDesktopRound drops only that one.
	userKey string
}

func desktopBlockedByPerson(scope agentruntime.Scope) error {
	if person, ok := desktopPersonInstanceID(scope); ok && (person != strings.TrimSpace(scope.InstanceID) || desktopUnattended(scope.InstanceID)) {
		return fmt.Errorf("用户正在这个桌面里登录或验证，请等这次完成后再操作")
	}
	return nil
}

func operateDesktop(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	command := desktopArg(args, "command")
	if !desktopbot.SideEffect(action, command) {
		return operateDesktopBody(ctx, scope, args)
	}
	steps := currentDesktopSteps()
	if steps == nil {
		return operateDesktopBody(ctx, scope, args)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// The first attempt still enters the body, so an install, git push, or
	// compose up still asks. A key that already succeeded returns here.
	step := desktopbot.Step{
		RunID: scope.RunID,
		Tool:  action,
		Key:   desktopStepKey(scope.RunID, agentruntime.ToolCallID(ctx)),
	}
	text, _, err := steps.RunDesktopStep(step, command, func() (string, error) {
		return operateDesktopBody(ctx, scope, args)
	})
	return text, err
}

// desktopStepKey is the journal key of one side effect. The journal table is
// keyed by this string alone, across runs. A tool_call_id by itself is not
// unique there: local models and recovered tool-call markup reuse ids such as
// call_0, so a later run's install or git push would get the earlier run's
// result without running. The run id scopes it. No call id, no journal.
func desktopStepKey(runID, toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return desktopbot.StepKey("", toolCallID, 0)
	}
	return runID + ":" + toolCallID
}

func operateDesktopBody(ctx context.Context, scope agentruntime.Scope, args map[string]any) (out string, err error) {
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
	if !desktopActionAllowed(action) {
		return "", fmt.Errorf("desktop action must be %s", strings.Join(desktopExecuteActions, ", "))
	}
	// Deliver attaches one file to this chat. A path reads that file from the
	// desktop. A name and content build the file here. A login in progress
	// must not turn "send me the file" into another question.
	if action == "deliver" {
		if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" {
			return "", fmt.Errorf("%s", desktopbot.PlanPhaseNextStep)
		}
		defer beginDesktopActivity(scope.RunID, "正在交付")()
		return deliverDesktopDocument(ctx, scope, args)
	}
	if action == "request_takeover" {
		return requestDesktopTakeover(ctx, scope, desktopArg(args, "reason"))
	}
	if err := desktopBlockedByPerson(scope); err != nil {
		return "", err
	}
	if err := desktopBotYield(ctx, scope); err != nil {
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
		// Plan may only observe. Every action outside the plan vocabulary is
		// blocked by derivation, so adding an action to the execute list
		// decides its plan-phase posture in the same stroke.
		if !desktopPlanActionAllowed(action) {
			return "", fmt.Errorf("%s", desktopbot.PlanPhaseNextStep)
		}
	}
	if action == "script" {
		if desktopArg(args, "script") == "" {
			return "", fmt.Errorf("desktop script requires script")
		}
		if err := browser.ValidatePageScript(desktopArg(args, "script")); err != nil {
			return "", err
		}
		if name := desktopArg(args, "name"); name != "" {
			if _, err := desktopSkillName(name); err != nil {
				return "", err
			}
		}
	}
	if action == "skill_run" && desktopArg(args, "name") == "" {
		return "", fmt.Errorf("desktop skill_run requires name")
	}
	if action == "bash" && desktopBashLaunchesGUI(desktopArg(args, "command")) {
		return operateDesktopBashGUI(ctx, scope, args)
	}
	if action == "file_read" || action == "file_write" || action == "file_edit" || action == "file_list" || action == "bash" {
		if err := refuseDesktopFileBeforeWake(action, args); err != nil {
			return "", err
		}
		if action == "bash" {
			if err := refuseDesktopDanger(ctx, scope, action, args); err != nil {
				return "", err
			}
		}
		if desktopHubExecutesTool() {
			return runDesktopHubTool(ctx, scope, action, args)
		}
		userKey, keyErr := desktopUserKey(scope)
		if keyErr != nil {
			return "", keyErr
		}
		if _, err := ensureDesktopRound(ctx, scope, userKey, false); err != nil {
			return "", err
		}
		defer beginDesktopActivity(scope.RunID, desktopContainerPhrase(action))()
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
	browserOpen := action == "app_open" && desktopIsBrowserProgram(desktopArg(args, "program"))
	if action == "app_open" && !browserOpen {
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
	if action == "shop_compare_offers" {
		return desktopShopCompareOffers(args)
	}
	if action == "skill_list" {
		defer beginDesktopActivity(scope.RunID, "正在查看")()
		return listDesktopBrowserSkills(userKey)
	}
	// Only a call with no worker-side guard goes to Hub as one machine call.
	// Screen actions keep the round's display (an isolated bot's own screen),
	// the window raise after app_open, the paste guard, the captcha/page-need
	// read, and the browser session. Those run here and reach the computer
	// through Hub's session, app, open, ctl, and screenshot routes below.
	if desktopHubExecutesTool() && desktopHubRPCDirect(action) {
		if action == "install" {
			if err := refuseDesktopDanger(ctx, scope, action, args); err != nil {
				return "", err
			}
		}
		return runDesktopHubTool(ctx, scope, action, args)
	}
	endpoint, err := ensureDesktopRound(ctx, scope, userKey, true)
	if err != nil {
		return "", err
	}
	if err := validateDesktopEndpoint(endpoint); err != nil {
		return "", err
	}
	if action == "install" {
		if err := refuseDesktopDanger(ctx, scope, action, args); err != nil {
			return "", err
		}
		defer beginDesktopActivity(scope.RunID, "正在安装")()
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
	if browserOpen {
		defer beginDesktopActivity(scope.RunID, "正在打开")()
		return desktopActivateBrowser(ctx, scope, endpoint.Display)
	}
	if action == "app_open" {
		defer beginDesktopActivity(scope.RunID, "正在打开")()
		before, listErr := desktopWindowsNow(ctx, scope, endpoint.Display)
		text, openErr := operateDesktopOpen(ctx, scope, endpoint.Display, openProgram, openArgs)
		if openErr != nil || listErr != nil {
			return text, openErr
		}
		hint := openProgram
		if i := strings.LastIndex(hint, "/"); i >= 0 {
			hint = hint[i+1:]
		}
		if note := desktopRaiseNewWindow(ctx, scope, endpoint.Display, desktopWindowIDs(before), hint); note != "" {
			text = strings.TrimSpace(text + "\n" + note)
		}
		return text, nil
	}
	if desktopCtlAction(action) {
		return operateDesktopCtl(ctx, scope, endpoint.Display, action, args)
	}
	if action == "app_list" || action == "app_run" || action == "computer" {
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
		stepsArg := args["steps"]
		if action == "computer" {
			stepsArg = desktopComputerActions(args)
		}
		if (action == "app_run" || action == "computer") && appStepsIncludeType(stepsArg) {
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
			defer beginDesktopActivity(scope.RunID, "正在查看")()
			text, listErr := operateDesktopApp(ctx, scope, endpoint.Display, action, args, nil)
			return withDesktopPages(text, listErr, endpoint.CDP, "")
		}
		// Steps replace this with the verb they are actually performing.
		// The shot after an input step says the screen is being read.
		// When the whole command returns, the run is choosing its next step.
		defer beginDesktopActivity(scope.RunID, "正在查看")()
		browserRaises := func() bool {
			return desktopAppBrowserRaises(binding, scope, endpoint.CDP)
		}
		verifySession := func() *browser.BrowserAgentSession {
			// Only a browser already attached is read; verifying must not
			// attach (and raise) Chromium over the window that was typed in.
			if binding == nil || binding.session == nil || binding.addr != endpoint.CDP || !desktopSessionConnected(binding.session) {
				return nil
			}
			if site := binding.session.PageHost(); site != "" {
				desktopRunNoteSite(desktopOwner(scope), site)
			}
			return binding.session
		}
		if label, hit := desktopClickHitsPublish(ctx, scope, endpoint.Display, stepsArg); hit {
			log.Printf("[desktop-guard] refused publish_needs_confirmation (coordinate click on %q)", label)
			return "", fmt.Errorf("%s (That point is the %q button.)", desktopPublishRefusal, label)
		}
		if action == "computer" || !desktopAppStepsLegacy(scope, args["steps"]) {
			text, stepErr := operateDesktopSteps(ctx, scope, endpoint.Display, action, args, stepsArg, beforeInsert, browserRaises)
			text, stepErr = desktopVerifySteps(ctx, scope, endpoint.Display, stepsArg, verifySession, text, stepErr)
			return desktopVerifyRichBody(desktopOwner(scope), verifySession(), action, args, text, stepErr)
		}
		text, appErr := desktopAppRun(ctx, scope, endpoint.Display, action, args, beforeInsert, browserRaises)
		if appErr != nil || !appStepsIncludeInput(args["steps"]) {
			return text, appErr
		}
		setDesktopActivity(scope.RunID, "正在看屏幕")
		text, appErr = desktopAppRunWithShot(ctx, scope, endpoint.Display, text)
		text, appErr = desktopVerifySteps(ctx, scope, endpoint.Display, stepsArg, verifySession, text, appErr)
		return desktopVerifyRichBody(desktopOwner(scope), verifySession(), action, args, text, appErr)
	}
	if action == "screenshot" {
		// The shot is the whole display. A network-error window left in front
		// is what gets captured, even when the loaded page is open behind it.
		// A login resume already raises that page. A normal screenshot did not.
		name := strings.TrimSpace(desktopArg(args, "name"))
		if name != "" {
			if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" {
				return "", fmt.Errorf("%s", desktopbot.PlanPhaseNextStep)
			}
			if _, err := desktop.DesktopShotFileName(name); err != nil {
				return "", err
			}
		}
		defer beginDesktopActivity(scope.RunID, "正在看屏幕")()
		session, _ := desktopOpenBrowser(binding, scope, userKey, endpoint.CDP)
		attached := ""
		if session != nil {
			_ = desktopFocusVisiblePage(session)
			attached = session.TargetID
		}
		frame, base := desktopFrameFor(scope).get()
		opts, _, optErr := computeruse.ShotOptionsFromArgs(args, frame, base)
		if optErr != nil {
			return "", optErr
		}
		opts = desktopPinShotSize(opts)
		// The accessibility list is read before the capture so the screen
		// YOLO gate knows how many elements it already has.
		axBlock, shotCtx := "", ctx
		if name == "" && opts.Region.Empty() {
			axBlock = desktopAXFirstElements(ctx, scope, endpoint.Display)
			shotCtx = withDesktopVisionHints(ctx, desktopVisionHints{AXCount: desktopAXElementCount(axBlock)})
		}
		shot, _, shotErr := desktopCaptureShot(shotCtx, scope, endpoint.Display, name, opts, opts.Region.Empty())
		if shotErr != nil {
			return "", shotErr
		}
		shot = axBlock + shot
		return withDesktopPages(shot, nil, endpoint.CDP, attached)
	}

	session, err := desktopBrowserSession(binding, scope, userKey, endpoint.CDP)
	if err != nil {
		return "", err
	}
	defer func() {
		// Registered first so it runs after the disarm below, which waits briefly for a late chooser.
		if session != nil && session.FileChooserBlocked() > 0 {
			if err != nil {
				err = fmt.Errorf("%w (%s)", err, desktopChooserBlockedNote)
			} else {
				out = strings.TrimRight(out, "\n") + "\n" + desktopChooserBlockedNote
			}
		}
	}()
	defer desktopArmChooserForRun(scope, action, session)()
	// Extra copies of the same URL hide the control the next click needs.
	// Close them before this step reads or changes the page.
	switch action {
	case "navigate", "probe", "task_run", "script", "skill_run":
		_ = session.PruneDuplicatePages()
	}
	// Same rule after attach. A type that names a ref is decided by that
	// ref, not by whichever control happens to be focused.
	if desktopInsertsIntoFocusedControl(action, args) && desktopPasswordFocused(scope, session) {
		return "", fmt.Errorf("not_password_field")
	}
	if err := desktopRichPreCheck(ctx, session, action, args); err != nil {
		return "", err
	}
	var text string
	switch action {
	case "navigate":
		defer beginDesktopActivity(scope.RunID, "正在打开网页")()
		result, callErr := session.Navigate(desktopArg(args, "url"))
		text, err = desktopActionText(result, callErr)
	case "probe":
		probePhrase := "正在看屏幕"
		if desktopArg(args, "query") != "" {
			probePhrase = "正在查找"
		}
		defer beginDesktopActivity(scope.RunID, probePhrase)()
		obs, callErr := session.Probe(desktopArg(args, "query"))
		text, err = desktopObservationText(ctx, scope, endpoint.Display, desktopArg(args, "query"), obs, callErr)
		text, err = withDesktopPages(text, err, endpoint.CDP, session.TargetID)
	case "task_run":
		steps, stepErr := desktopBrowserSteps(args["steps"])
		if stepErr != nil {
			return "", stepErr
		}
		defer beginDesktopActivity(scope.RunID, desktopBrowserStepsPhrase(steps))()
		state, callErr := browser.RunAgentFastBatchUntilPerson(session, steps, desktopArg(args, "text"))
		text, err = desktopTaskText(state, callErr)
	case "click":
		defer beginDesktopActivity(scope.RunID, "正在点击")()
		var result *browser.BrowserActionResult
		var callErr error
		if ref := desktopArg(args, "ref"); ref != "" {
			result, callErr = session.Click(desktopArg(args, "snapshot_id"), ref, "")
		} else {
			result, callErr = session.ClickText(desktopArg(args, "snapshot_id"), desktopArg(args, "text"))
		}
		text, err = desktopActionText(result, callErr)
	case "type":
		defer beginDesktopActivity(scope.RunID, "正在输入")()
		result, callErr := session.Type(desktopArg(args, "snapshot_id"), desktopArg(args, "ref"), "", desktopArg(args, "text"))
		text, err = desktopActionText(result, callErr)
		text, err = desktopVerifyBrowserType(session, desktopArg(args, "text"), text, err)
	case "press":
		defer beginDesktopActivity(scope.RunID, desktopKeyPhrase(desktopArg(args, "key")))()
		result, callErr := session.Press(desktopArg(args, "key"))
		text, err = desktopActionText(result, callErr)
	case "script":
		defer beginDesktopActivity(scope.RunID, desktopScriptPhrase(desktopArg(args, "script")))()
		result, callErr := browser.RunPageScriptBatch(session, desktopArg(args, "script"), desktopArg(args, "name"), userKey, desktopBrowserSkillRoot)
		text, err = desktopActionText(result, callErr)
		text, err = desktopVerifyBrowserScript(session, desktopArg(args, "script"), text, err)
		text, err = desktopVerifyRichBody(desktopOwner(scope), session, action, args, text, err)
		if err != nil && desktopScriptErrIsNavigation(err) {
			// The page navigated (e.g. after a submit) while the script ran.
			// That is a page change, not a failure: report where it went so
			// the model does not retry or wait (RUN33).
			pages, _ := withDesktopPages("", nil, endpoint.CDP, session.TargetID)
			return "script ended because the page navigated (a submit or link took effect). Do not rerun it; read the new page.\n" + strings.TrimSpace(pages), nil
		}
		if scriptErr := err; scriptErr != nil {
			// A script that threw did not do its job; withDesktopPages would
			// turn it into an ok result with a page list.
			pages, _ := withDesktopPages("", nil, endpoint.CDP, session.TargetID)
			return "", fmt.Errorf("page script failed: %s\n%s", desktopStepClip(scriptErr.Error(), 300), strings.TrimSpace(pages))
		}
		text, err = withDesktopPages(text, err, endpoint.CDP, session.TargetID)
	case "upload":
		defer beginDesktopActivity(scope.RunID, "正在上传文件")()
		text, err = desktopUpload(session, args)
		text, err = withDesktopPages(text, err, endpoint.CDP, session.TargetID)
	case "publish":
		defer beginDesktopActivity(scope.RunID, "正在发布")()
		text, err = desktopPublish(desktopOwner(scope), session, args)
	case "shop_readback":
		defer beginDesktopActivity(scope.RunID, "正在查看")()
		text, err = desktopShopReadback(session)
	case "skill_run":
		defer beginDesktopActivity(scope.RunID, "正在操作网页")()
		result, callErr := browser.RunSavedPageScript(session, userKey, desktopArg(args, "name"), desktopBrowserSkillRoot)
		text, err = desktopActionText(result, callErr)
		text, err = withDesktopPages(text, err, endpoint.CDP, session.TargetID)
	default:
		return "", fmt.Errorf("desktop action must be navigate, probe, click, type, or press")
	}
	if site := desktopSiteOf(action, args, session); site != "" {
		desktopRunNoteSite(desktopOwner(scope), site)
		desktopExperienceNoteSite(scope, site)
		if err == nil {
			if lesson := desktopExperienceOnFirstVisit(scope, site); lesson != "" {
				text = strings.TrimRight(text, "\n") + "\n" + lesson
			}
		}
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
	if reason == "login_wall" || reason == "mfa" {
		if desktopUnattended(scope.InstanceID) {
			return strings.TrimSpace(text + "\n这次是自动任务，没有人在对话里填写密码、手机号或验证码。"), fmt.Errorf("desktop needs a person")
		}
		question := "请输入这个网站要求的密码或手机号。提交后会填进当前输入框，并继续这次操作。"
		name := desktopFillSecretName(reason, text)
		if reason == "mfa" {
			question = "请输入验证码。提交后会填进当前输入框，并继续这次操作。"
		}
		desktopFocusCredential(scope, reason)
		return agent.AskUserResultMarker(&agent.AskUserRequest{
			Question:   question,
			InputType:  "secret_fill",
			SecretName: name,
		}), nil
	}
	if desktopUnattended(scope.InstanceID) {
		return strings.TrimSpace(text + "\n这次是自动任务，没有人在桌面旁完成登录或验证。"), fmt.Errorf("desktop needs a person")
	}
	noteDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID)
	noteDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID, reason)
	noteDesktopResume(scope)
	question := desktopLoginQuestion
	if reason == "payment_confirm" {
		question = "这一步需要你在当前桌面的浏览器里完成支付。完成后这个 bot 会接着操作。"
	}
	return agent.AskUserResultMarker(&agent.AskUserRequest{
		Question:  question,
		InputType: "text",
	}), nil
}

var desktopPageURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func desktopFillSecretName(reason, text string) string {
	base := "SITE_LOGIN"
	if reason == "mfa" {
		base = "SITE_CODE"
	}
	host := desktopTextHost(text)
	if host == "" {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteByte('_')
	prevUnderscore := false
	for _, r := range strings.ToUpper(host) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if prevUnderscore {
			continue
		}
		b.WriteByte('_')
		prevUnderscore = true
	}
	name := strings.Trim(b.String(), "_")
	if len(name) > 64 {
		name = strings.Trim(name[:64], "_")
	}
	if !agent.ValidSecretName(name) {
		return base
	}
	return name
}

func desktopTextHost(text string) string {
	// The open-tab list is appended after the action. Its last address is
	// some other tab, and the saved login must stay with this page.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- attached ") {
			if host := desktopHostFromURLs(line); host != "" {
				return host
			}
		}
	}
	if index := strings.Index(text, "\nOpen browser pages"); index >= 0 {
		text = text[:index]
	}
	return desktopHostFromURLs(text)
}

func desktopHostFromURLs(text string) string {
	matches := desktopPageURLPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return ""
	}
	raw := strings.TrimRight(matches[len(matches)-1], ".,);]")
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeDesktopSiteHost(parsed.Hostname())
}

func desktopFocusCredential(scope agentruntime.Scope, reason string) {
	session := desktopLiveSession(scope)
	if session == nil {
		return
	}
	kind := "password"
	if reason == "mfa" {
		kind = "code"
	}
	_ = session.FocusCredential(kind)
}

func desktopNeedsPerson(text string) bool {
	return desktopAttentionReason(text) != ""
}

// desktopReplyNeedsComputer is the person's step when the reply itself says
// so. A QR login often has no password field, so the page-flag handoff never
// fires and the turn ends as an ordinary sentence.
func desktopReplyNeedsComputer(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.Contains(text, "接管电脑") || strings.Contains(text, "请接管") || strings.Contains(text, "需要你接管") {
		return "computer_takeover"
	}
	// 尚未登录 contains 未登录, not 已登录. A finished scan is a report.
	succeeded := (strings.Contains(text, "已登录") || strings.Contains(text, "登录成功") || strings.Contains(text, "扫码成功") || strings.Contains(text, "已通过扫码登录")) &&
		!strings.Contains(text, "尚未登录") && !strings.Contains(text, "未登录")
	asks := strings.Contains(text, "缺少") || strings.Contains(text, "需要你") || strings.Contains(text, "请你") || strings.Contains(text, "请在") || strings.Contains(text, "尚未登录") || strings.Contains(text, "未登录")
	if succeeded && !asks {
		return ""
	}
	qrAsk := strings.Contains(text, "扫描二维码") || strings.Contains(text, "扫码登录") || strings.Contains(text, "二维码登录")
	if qrAsk && asks && !succeeded {
		return "login_wall"
	}
	if asks && !succeeded && (strings.Contains(text, "登录") || strings.Contains(text, "验证码") || strings.Contains(text, "人机验证")) {
		return "login_wall"
	}
	return ""
}

// noteDesktopReplyHandoff records a computer handoff from the finished reply
// when the tool path did not already see a login wall. A scheduled run has
// nobody at the desktop, so it does not hold the keyboard.
func noteDesktopReplyHandoff(tenantID, userID, instanceID, text string) {
	if desktopUnattended(instanceID) {
		return
	}
	// The tool path already held the desktop for this turn. A second note
	// would count the hold twice and could replace a more specific reason.
	if _, held := desktopHandoff.Load(desktopHandoffKey(tenantID, userID, instanceID)); held {
		return
	}
	reason := desktopReplyNeedsComputer(text)
	if reason == "" {
		return
	}
	noteDesktopHandoff(tenantID, userID, instanceID)
	noteDesktopAttention(tenantID, userID, instanceID, reason)
	noteDesktopResume(agentruntime.Scope{TenantID: tenantID, UserID: userID, InstanceID: instanceID})
}

func desktopAttentionReason(text string) string {
	// The screenshot and its screen_vision are appended after the page
	// observation. Words in that reading, including "page flags:" and
	// "captcha challenge", are on-screen text, not a handoff signal.
	text = desktopAttentionText(text)
	if strings.Contains(text, "handoff=captcha") || strings.Contains(text, "captcha challenge") {
		return "captcha_widget"
	}
	flags := desktopPageFlagsLine(text)
	if flags == "" {
		return ""
	}
	for _, name := range []string{"captcha_widget", "login_qr", "payment_confirm", "login_wall", "mfa"} {
		if strings.Contains(flags, name) {
			return name
		}
	}
	return ""
}

func desktopAttentionText(text string) string {
	cut := len(text)
	for _, mark := range []string{"\nscreen_vision:", "\x00[maclaw-model-image "} {
		if i := strings.Index(text, mark); i >= 0 && i < cut {
			cut = i
		}
	}
	if strings.HasPrefix(text, "screen_vision:") {
		return ""
	}
	return text[:cut]
}

func desktopPageFlagsLine(text string) string {
	index := strings.Index(text, "page flags:")
	if index < 0 {
		return ""
	}
	flags := text[index:]
	if end := strings.IndexByte(flags, '\n'); end >= 0 {
		flags = flags[:end]
	}
	return flags
}

type desktopPhaseKey struct{}

type desktopInsideBatchKey struct{}

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

func desktopMessageMeta(request agentruntime.TurnRequest) map[string]string {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Message.Metadata
	case *agentservice.ExecuteRequest:
		if input == nil {
			return nil
		}
		return input.Message.Metadata
	default:
		return nil
	}
}

// desktopMainAgentTurn is the call that must not drive the desktop.
// The computer helper is the execute call marked computer_helper.
func desktopMainAgentTurn(request agentruntime.TurnRequest) bool {
	meta := desktopMessageMeta(request)
	return strings.TrimSpace(meta["main_agent"]) == "1"
}

func desktopStripMainDriving(def agentruntime.ToolDefinition) agentruntime.ToolDefinition {
	def.Description = "Read this user's cloud desktop. action=screenshot returns one image and has no coordinates. Driving the desktop is the computer helper's call."
	props, _ := def.Parameters["properties"].(map[string]any)
	if props == nil {
		return def
	}
	actions := desktopMainAgentActions()
	if action, ok := props["action"].(map[string]any); ok {
		action["enum"] = actions
		action["description"] = strings.Join(actions, ", ")
	}
	for _, key := range []string{"x", "y", "region", "steps", "actions", "ref", "script"} {
		delete(props, key)
	}
	return def
}

func desktopMainAgentActions() []string {
	out := make([]string, 0, len(desktopExecuteActions))
	for _, action := range desktopExecuteActions {
		if action == "click" || action == "type" || action == "script" || action == "computer" {
			continue
		}
		out = append(out, action)
	}
	return out
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
		scope.UserID = desktopBotUser(user, meta)
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

type desktopLLMSeesImagesKey struct{}

// desktopTurnSeesImages reports whether this turn's chat model accepts images.
// That is the same switch the GUI uses for Computer Use: a vision model looks
// at the screenshot, and local OCR plus OmniParser run only for a text model.
func desktopTurnSeesImages(request agentruntime.TurnRequest) bool {
	cfg, ok := turnAppConfig(request)
	if !ok {
		return false
	}
	llm, err := agentservice.ResolveLLMConfig(cfg)
	if err != nil {
		return false
	}
	return llm.SupportsVision
}

func turnAppConfig(request agentruntime.TurnRequest) (corelib.AppConfig, bool) {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Config, true
	case *agentservice.ExecuteRequest:
		if input == nil {
			return corelib.AppConfig{}, false
		}
		return input.Config, true
	default:
		return corelib.AppConfig{}, false
	}
}

func desktopContextSeesImages(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	sees, _ := ctx.Value(desktopLLMSeesImagesKey{}).(bool)
	return sees
}

func desktopScreenVisionPrompt(seesImages bool) string {
	if seesImages {
		return "This model can see the attached image, so local OCR and OmniParser are not used. "
	}
	return "When screen_vision is in that result, local OCR and OmniParser listed the on-screen text and controls with those pixel centers. "
}

func desktopScreenVisionClickPrompt(seesImages bool) string {
	if seesImages {
		return "Look at the attached image. "
	}
	return "When screen_vision is present, click the center it gives for a labeled control. "
}

func desktopScreenCheckPrompt(seesImages bool) string {
	if seesImages {
		return "Before you finish, look at the attached screenshot and confirm it is the screen the user asked for. Do not send a different screen. "
	}
	return "Before you finish, read screen_vision on the latest screenshot and confirm that reading is the screen the user asked for. Do not send a screenshot that does not show it. "
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
	case "computer":
		return desktopStepsInsertIntoFocus(desktopComputerActions(args))
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
		return desktopStepsInsertIntoFocus(raw)
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
	if steps, err := computeruse.ParseSteps(raw); err == nil {
		for _, step := range steps {
			if step.Input() {
				return true
			}
		}
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
	if steps, err := computeruse.ParseSteps(raw); err == nil {
		for _, step := range steps {
			if step.InsertsText() {
				return true
			}
		}
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
		return "", fmt.Errorf("%s", desktopbot.DesktopNeedsUser)
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
		return "", fmt.Errorf("%s", desktopbot.DesktopEmptyResult)
	}
	text := strings.TrimSpace(result.Display)
	if text == "" {
		text = strings.TrimSpace(result.Detail)
	}
	if text == "" {
		text = strings.TrimSpace(result.Status)
	}
	if text == "" {
		return "", fmt.Errorf("%s", desktopbot.DesktopEmptyResult)
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
	if flags.LoginQR {
		names = append(names, "login_qr")
	}
	if flags.LoginWall {
		names = append(names, "login_wall")
	}
	if flags.MFA {
		names = append(names, "mfa")
	}
	if flags.CaptchaWidget {
		names = append(names, "captcha_widget")
	}
	if flags.PaymentConfirm {
		names = append(names, "payment_confirm")
	}
	if flags.ConsentDialog {
		names = append(names, "consent_dialog")
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
	// The screenshot image is glued on after the last line. Leave it out of
	// the split so a newline before that glue cannot hide the click sentence.
	plain, images := agentruntime.ExtractModelImages(text)
	out, outErr := withDesktopPagesPlain(plain, err, addr, attachedID)
	if err != nil {
		return out, outErr
	}
	for _, image := range images {
		out = agentruntime.AttachModelImage(out, image.MIME, image.Base64)
	}
	return out, outErr
}

func withDesktopPagesPlain(text string, err error, addr, attachedID string) (string, error) {
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
		// Never fold a failure into an ok result: the step did not happen.
		return "", fmt.Errorf("FAILED: %s\n%s", desktopStepClip(err.Error(), 300), strings.TrimSpace(pages))
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
	if !desktopInstructionLine(last) {
		return text, ""
	}
	// The open-page list is inserted above this instruction. A screen_vision
	// block left above that list would be separated from the click sentence
	// that tells the model to use its coordinates.
	head, reading := splitTrailingScreenVision(prefix)
	if reading == "" {
		return prefix, last
	}
	return head, reading + "\n" + last
}

func desktopInstructionLine(line string) bool {
	return strings.Contains(line, "Do not pixel-click the browser.") ||
		strings.Contains(line, "app_run click uses these pixel coordinates.") ||
		strings.Contains(line, "Do not probe again between those steps.")
}

func splitTrailingScreenVision(prefix string) (head, reading string) {
	const mark = "\nscreen_vision:"
	idx := strings.LastIndex(prefix, mark)
	if idx >= 0 {
		return strings.TrimSpace(prefix[:idx]), strings.TrimSpace(prefix[idx+1:])
	}
	if strings.HasPrefix(prefix, "screen_vision:") {
		return "", prefix
	}
	return prefix, ""
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
	targets = trimDesktopTabs(addr, attachedID, targets)
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

func desktopControlName(ref browser.BrowserElementRef) string {
	if strings.EqualFold(ref.InputType, "password") {
		return "password field"
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		name = strings.TrimSpace(ref.Text)
	}
	return desktopOneLine(name, 80)
}

func formatDesktopControls(snapshot browser.BrowserSnapshot) string {
	refs := snapshot.Refs
	extra := 0
	if len(refs) > 80 {
		extra = len(refs) - 80
		refs = refs[:80]
	}
	var b strings.Builder
	if id := strings.TrimSpace(snapshot.SnapshotID); id != "" {
		fmt.Fprintf(&b, "snapshot_id=%s\n", id)
	}
	names := map[string]int{}
	for _, ref := range refs {
		if name := desktopControlName(ref); name != "" {
			names[strings.ToLower(name)]++
		}
	}
	b.WriteString("Controls:\n")
	for _, ref := range refs {
		role := strings.TrimSpace(ref.Role)
		if role == "" {
			role = strings.TrimSpace(ref.Tag)
		}
		name := desktopControlName(ref)
		line := strings.TrimSpace(ref.Ref + " " + role)
		if name != "" {
			line += " " + name
		}
		if name != "" && names[strings.ToLower(name)] > 1 {
			if sel := desktopOneLine(strings.TrimSpace(ref.Selector), 80); sel != "" {
				line += " " + sel
			}
		}
		if ref.Disabled {
			line += " disabled"
		}
		b.WriteString(line + "\n")
	}
	if extra > 0 {
		fmt.Fprintf(&b, "and %d more controls\n", extra)
	}
	b.WriteString("One action=script can click, type, select, press, and scroll these controls. Do not probe again between those steps.")
	return b.String()
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
		return text + "\n" + formatDesktopControls(obs.Snapshot), nil
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
	head := trimDesktopHead(text, budget)
	if tail == "" || strings.Contains(head, tail) {
		return head + marker
	}
	return head + marker + tail
}

func trimDesktopHead(text string, budget int) string {
	if budget >= len(text) {
		return text
	}
	if budget < 0 {
		budget = 0
	}
	head := text[:budget]
	for len(head) > 0 {
		r, size := utf8.DecodeLastRuneInString(head)
		if r != utf8.RuneError || size != 1 {
			break
		}
		if head[len(head)-1] < utf8.RuneSelf {
			break
		}
		head = head[:len(head)-1]
	}
	return head
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
	if len(loose) == 0 || len(loose) > desktopMaxBatchSteps() {
		return nil, fmt.Errorf("desktop task_run accepts 1 to %d steps", desktopMaxBatchSteps())
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

// refuseDesktopFileBeforeWake rejects a file or shell that cannot run, before
// the round wakes a machine.
func refuseDesktopFileBeforeWake(action string, args map[string]any) error {
	switch action {
	case "file_read", "file_write", "file_edit", "file_list":
		filePath := desktopArg(args, "path")
		if action == "file_list" && filePath == "" {
			filePath = desktop.ContainerHome
		}
		if _, err := desktop.ContainerPath(filePath); err != nil {
			return fmt.Errorf("desktop %s rejected the path: %w", action, err)
		}
		if action == "file_write" {
			if _, ok := args["content"].(string); !ok {
				return fmt.Errorf("%s", desktopbot.FileWriteNeedsContent)
			}
			if len(desktopRawArg(args, "content")) > desktop.FileContentMax {
				return fmt.Errorf("%s", desktopbot.FileTooLarge)
			}
		}
		if action == "file_edit" && desktopRawArg(args, "old_string") == "" {
			return fmt.Errorf("%s", desktopbot.FileEditNeedsOld)
		}
	case "bash":
		if _, err := desktop.BashArgs(desktopContainerName(), desktopArg(args, "command")); err != nil {
			return fmt.Errorf("desktop bash rejected the command: %w", err)
		}
	}
	return nil
}

// desktopFileWakeIsTheCall reports that the file or shell call itself is the
// wake. A stubbed session is the wake the test counts, so that still goes
// through desktopSession once.
func desktopFileWakeIsTheCall() bool {
	if desktopRemoteSession != nil {
		return false
	}
	if desktopHubClientFromEnv() != nil {
		return true
	}
	return desktopRemoteFile != nil || desktopRemoteBash != nil
}

// ensureDesktopRound wakes the machine on the first tool of this round that
// needs it. A later tool in the same round reuses that wake. skill_list does
// not call this. A missing run id wakes for that call only.
func ensureDesktopRound(ctx context.Context, scope agentruntime.Scope, userKey string, needEndpoint bool) (desktopEndpoint, error) {
	runID := strings.TrimSpace(scope.RunID)
	var wake *desktopRoundWake
	if runID != "" {
		value, _ := desktopRoundWakes.LoadOrStore(runID, &desktopRoundWake{})
		wake = value.(*desktopRoundWake)
		wake.mu.Lock()
		defer wake.mu.Unlock()
		if strings.TrimSpace(userKey) != "" {
			wake.userKey = strings.TrimSpace(userKey)
		}
		if wake.have {
			return wake.endpoint, nil
		}
		if wake.woken && !needEndpoint {
			return desktopEndpoint{}, nil
		}
	}
	if !needEndpoint && desktopFileWakeIsTheCall() {
		if wake != nil {
			wake.woken = true
		}
		return desktopEndpoint{}, nil
	}
	endpoint, err := desktopSession(ctx, scope, userKey)
	if err != nil {
		return desktopEndpoint{}, err
	}
	if wake != nil {
		wake.endpoint = endpoint
		wake.have = true
		wake.woken = true
	}
	return endpoint, nil
}

// finishDesktopRound drops this round's wake and the browser session of the
// desktop this round used. That is the bot's own display for an isolated bot,
// or the shared owner desktop otherwise. A binding another running round
// still uses is kept, so one bot finishing does not cut another bot's browser.
// The next tool after the computer sleeps calls Ensure again.
func finishDesktopRound(tenantID, userID, runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		// No round to scope by: keep the old per-user behavior.
		if key, err := desktop.UserKey(tenantID, userID); err == nil {
			dropDesktopBinding(key)
		}
		return
	}
	var key string
	if value, ok := desktopRoundWakes.LoadAndDelete(runID); ok {
		if wake, _ := value.(*desktopRoundWake); wake != nil {
			wake.mu.Lock()
			key = wake.userKey
			wake.mu.Unlock()
		}
	}
	dropDesktopToolResults(runID)
	if key == "" {
		// A round that never recorded its desktop: the owner's desktop.
		key, _ = desktop.UserKey(tenantID, userID)
	}
	if key == "" || desktopBindingInUse(key) {
		return
	}
	dropDesktopBinding(key)
}

// desktopBindingInUse reports another open round on the same desktop.
func desktopBindingInUse(userKey string) bool {
	inUse := false
	desktopRoundWakes.Range(func(_, value any) bool {
		wake, _ := value.(*desktopRoundWake)
		if wake == nil {
			return true
		}
		wake.mu.Lock()
		same := wake.userKey == userKey
		wake.mu.Unlock()
		if same {
			inUse = true
			return false
		}
		return true
	})
	return inUse
}

func dropDesktopBinding(userKey string) {
	value, ok := desktopBindings.Load(userKey)
	if !ok {
		return
	}
	binding, _ := value.(*desktopBinding)
	if binding != nil {
		binding.mu.Lock()
		binding.session = nil
		binding.addr = ""
		binding.mu.Unlock()
	}
	desktopBindings.Delete(userKey)
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
		return runGuardedDesktopAppSteps(steps, beforeInsert, browserRaises, run, func(phrase string) {
			setDesktopActivity(scope.RunID, phrase)
		})
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
func runGuardedDesktopAppSteps(steps [][]string, beforeInsert func() error, browserRaises func() bool, run func(...string) (string, error), onStep func(string)) (string, error) {
	if onStep == nil {
		onStep = func(string) {}
	}
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
		// already released, so it does not record. The click phrase
		// waits until that activate returns: the poll can sit for
		// about 15s, and that wait is a window change, not a click.
		if release && !released {
			restore := desktopReadActiveWindow(run)
			if releaseOnce() {
				dismissDesktopWhisker(run)
			}
			if restore != "" {
				onStep(desktopArgvPhrase([]string{"windowactivate"}))
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
		onStep(desktopClickPhrase(argv))
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
			// Text going to a focused non-browser window (editor, Office,
			// file manager) cannot land in a browser password field. Skip
			// the browser read there: attaching CDP raised Chromium, and
			// the windowactivate --sync back to the target made a Chinese
			// paste into Mousepad take ~17s.
			stepGuard := beforeInsert
			if unnamed && stepGuard != nil && desktopArgvInsertsText(step) && desktopFocusedWindowOutsideBrowser(run) {
				stepGuard = nil
			}
			raiseSteals := unnamed && desktopArgvInsertsText(step) && stepGuard != nil && (browserRaises == nil || browserRaises())
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
			if desktopArgvInsertsText(step) && stepGuard != nil {
				insertErr = stepGuard()
				focusPending = len(lastFocus) > 0
			}
			// No window is waiting to be focused, and the active window was
			// not recorded. The class cannot show that the keystrokes leave
			// the browser, so the read's error stops the type before any key.
			if insertErr != nil && desktopRaiseFileDialog(run) && desktopFocusedWindowOutsideBrowser(run) {
				// The refusal read the page behind an open native file
				// chooser; the text goes to that dialog (a path), raised now.
				insertErr = nil
				focusPending, restoreID = false, ""
			}
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
				onStep(desktopArgvPhrase(lastFocus))
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
				// type: the browser is the focused window then. The poll
				// can sit for about 15s, so the row says the window is
				// changing until that call returns.
				onStep(desktopArgvPhrase([]string{"windowactivate"}))
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
				onStep(desktopArgvPhrase(call))
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
	if class == "" {
		return false
	}
	if desktopWindowIsBrowser(class) {
		// The browser's own native file chooser (GTK "Open File") has the
		// browser class but holds no web page, so no password field: text
		// typed there is a file path. Every site's upload uses it.
		nameOut, nerr := run("getactivewindow", "getwindowname")
		return nerr == nil && desktopFileDialogTitleRe.MatchString(strings.TrimSpace(nameOut))
	}
	return true
}

var desktopFileDialogTitleRe = regexp.MustCompile(`(?i)^(open|open files?|save|save files?|save as|select files?|choose files?|file upload|upload files?|打开|打开文件|保存|保存文件|另存为|选择文件|上传文件)$`)

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
	_, opts := desktopFrameFor(scope).get()
	shot, _, shotErr := desktopCaptureShot(ctx, scope, display, "", opts, true)
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
		// key [--repeat N] [--delay N] [--clearmodifiers] K...: any K counts.
		for i := 1; i < len(step); i++ {
			switch step[i] {
			case "--repeat", "--delay", "--window":
				i++
				continue
			case "--clearmodifiers", "--sync":
				continue
			}
			if browser.KeyInsertsText(step[i]) {
				return true
			}
		}
		return false
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
	if desktopPixelsTooLarge(config.Width, config.Height) {
		return "", fmt.Errorf("desktop screenshot is too large")
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
	// Saved stays on the caption line. screen_vision is a block whose last
	// line is the click instruction; a note after that block would keep the
	// open-page list from being followed by that instruction.
	if saved != "" {
		text += " Saved " + saved + ". That is the only file this action wrote. Report this path."
		noteDesktopSavedPath(ctx, saved)
	} else if name != "" {
		text += " The screenshot was not written to the desktop. Do not tell the user a file path."
	}
	if ctx != nil && mime == "image/png" && encoded != "" {
		ctx = context.WithValue(ctx, desktopVisionEncodedKey{}, encoded)
	}
	if vision := desktopVisionText(desktopEnsureVisionHints(ctx, scope, display), data, clickPixels); vision != "" {
		// The vision block ends with the same click sentence. Leaving a
		// second copy on the caption puts that order above the page list.
		text = strings.Replace(text, "app_run click uses these pixel coordinates. ", "", 1)
		text += "\n" + vision
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
	claimedShot := false
	for _, line := range lines {
		if strings.Contains(line, "~/Desktop/") || strings.Contains(line, "/Desktop/") || strings.Contains(line, "保存到桌面") {
			low := strings.ToLower(line)
			claimedShot = claimedShot || strings.Contains(line, "截图") || strings.Contains(low, "screenshot")
			continue
		}
		kept = append(kept, line)
	}
	body := strings.TrimSpace(strings.Join(kept, "\n"))
	if !claimedShot && body != "" {
		// Working paths in a reply about other work (a published post)
		// are dropped quietly: a screenshot note there is noise.
		return body
	}
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
		msg.Content = replaceLeakedMonologue(msg.Content)
		msg.Content = dropInternalStatusLead(msg.Content)
	}
	return images, files
}

var desktopMonologueMarkers = []string{"Let me ", "I need ", "Hmm", "Idea:", "Maybe ", "Actually ", "So probe", "I can't attach", "Alternative approach", "Let's try", "I should "}

// replaceLeakedMonologue swaps a reply that is the model's own working notes
// (English think-aloud, no user-facing result) for a short honest status.
func replaceLeakedMonologue(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.Contains(t, "http") {
		return text
	}
	n := 0
	for _, m := range desktopMonologueMarkers {
		n += strings.Count(t, m)
	}
	cjk := 0
	for _, r := range t {
		if r >= 0x4e00 && r <= 0x9fff {
			cjk++
		}
	}
	if n < 3 || cjk*10 > len([]rune(t)) {
		return text
	}
	return "这次没有完成：浏览器页面卡住了，我没能把任务做完，也没有发布任何内容。可以让我再试一次。"
}

// Words that only make sense inside the runtime (tool status lines, guards).
var desktopInternalStatusRe = regexp.MustCompile("(^|[^/\\w])desktop ?(:|报错|失败|error|出错|异常)|PUBLISH STATE|写入保护|保护(被|再次)?.{0,4}触发|^\\s*[^/\\s]{0,10}[：:]?\\s*(/tmp/\\S+?[、,，;；\\s]*)+$|progress_where|runtime tool|refused|already_published|unasked_file|宿主|`deliver`|deliver ?(报错|调用|被拦|失败)|guard ?(拦|refus)")

// dropInternalStatusLead removes leading paragraphs that talk about the
// runtime's own status lines instead of the result, when a later paragraph
// carries the result (a link).
func dropInternalStatusLead(text string) string {
	paras := strings.Split(strings.TrimSpace(text), "\n\n")
	i := 0
	for i < len(paras)-1 && desktopInternalStatusRe.MatchString(paras[i]) && !strings.Contains(paras[i], "http") {
		i++
	}
	rest := strings.Join(paras[i:], "\n\n")
	if !strings.Contains(rest, "http") {
		return text
	}
	// Single lines anywhere that explain a guard or tool refusal.
	var keep []string
	for _, ln := range strings.Split(rest, "\n") {
		if desktopInternalStatusRe.MatchString(ln) && !strings.Contains(ln, "http") {
			continue
		}
		keep = append(keep, ln)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
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
	if len(loose) == 0 || len(loose) > desktopMaxBatchSteps() {
		return nil, fmt.Errorf("desktop app_run accepts 1 to %d steps", desktopMaxBatchSteps())
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
			if !desktopCoordPresent(step.X) || !desktopCoordPresent(step.Y) {
				return nil, fmt.Errorf("desktop app click requires x and y")
			}
			x, xOK := desktopCoord(step.X)
			y, yOK := desktopCoord(step.Y)
			if !xOK || !yOK {
				return nil, fmt.Errorf("desktop app click coordinate is outside 0..%d", desktopClickMax)
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

// desktopClickMax is the farthest pixel app_run click accepts. A 4K desktop
// and a side-by-side pair of them stay inside it. A larger number is not a
// coordinate the screenshot can show.
const desktopClickMax = 16384

func desktopCoordPresent(value any) bool {
	if value == nil {
		return false
	}
	text, ok := value.(string)
	return !ok || strings.TrimSpace(text) != ""
}

func desktopCoord(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return desktopCoordFloat(typed)
	case string:
		text := strings.TrimSpace(typed)
		if n, err := strconv.Atoi(text); err == nil {
			return desktopCoordInt(n)
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return desktopCoordFloat(parsed)
	default:
		return 0, false
	}
}

func desktopCoordFloat(value float64) (int, bool) {
	if value < 0 || value > desktopClickMax {
		return 0, false
	}
	return int(value), true
}

func desktopCoordInt(value int) (int, bool) {
	if value < 0 || value > desktopClickMax {
		return 0, false
	}
	return value, true
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

// desktopBatchStepActions may run inside one plan call. Each step goes through the
// normal InvokeTool path (guards, coach, step records).
var desktopBatchStepActions = map[string]bool{"upload": true, "script": true, "rich_fill": true, "navigate": true, "probe": true, "click": true, "type": true, "press": true, "wait": true, "clipboard_set": true}

const desktopPlanMaxSteps = 12

// invokePlan runs a planned browser sequence in one model step: observe once,
// act in one shot, read back once. Only CDP, page scripts and real OS input
// are used (the browser is stock Chromium).
func (m desktopRuntimeModule) invokePlan(ctx context.Context, request agentruntime.TurnRequest, name string, args map[string]any) (string, error) {
	raw, _ := args["plan"].([]any)
	if raw == nil {
		raw, _ = args["steps"].([]any)
	}
	if len(raw) == 0 || len(raw) > desktopPlanMaxSteps {
		return "", fmt.Errorf("plan needs 1 to %d steps, each {action:..., ...}", desktopPlanMaxSteps)
	}
	var lines []string
	last := ""
	for i, item := range raw {
		step, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("plan step %d is not an object", i+1)
		}
		act := strings.ToLower(strings.TrimSpace(desktopArg(step, "action")))
		if !desktopBatchStepActions[act] {
			return "", fmt.Errorf("plan step %d: action %q is not allowed in a plan (use script, rich_fill, navigate, probe, click, type, press, wait, clipboard_set)", i+1, act)
		}
		out, err := m.InvokeTool(ctx, request, name, step)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%d. %s: FAILED: %v", i+1, act, err))
			return strings.Join(lines, "\n") + fmt.Sprintf("\nplan stopped at step %d of %d; steps before it are done — fix only that step, then continue with the rest.", i+1, len(raw)), nil
		}
		lines = append(lines, fmt.Sprintf("%d. %s: ok: %s", i+1, act, desktopPlanSummary(out)))
		last = out
	}
	settle := ""
	if lastAct := strings.ToLower(strings.TrimSpace(desktopArg(raw[len(raw)-1].(map[string]any), "action"))); lastAct == "script" || lastAct == "click" || lastAct == "press" {
		// The last step often submits a form and the page moves on its own.
		// Report where the page settled so the model does not spend a
		// wait/probe call (or fail a "screen changed" wait) to learn it.
		if out, err := m.InvokeTool(ctx, request, name, map[string]any{"action": "script", "script": desktopPlanSettleScript}); err == nil {
			settle = "\npage after plan: " + desktopPlanSummary(out)
		}
	}
	return strings.Join(lines, "\n") + fmt.Sprintf("\nplan done (%d steps). Last step result:\n%s", len(raw), last) + settle, nil
}

// desktopPlanSettleScript waits briefly for a navigation the plan started,
// then reports the page it landed on.
const desktopPlanSettleScript = `const u0=location.href; for (let i=0;i<10;i++){ await new Promise(r=>setTimeout(r,300)); if (location.href!==u0) break; } await new Promise(r=>setTimeout(r,500)); return 'url '+location.href+' ; title '+document.title`

func desktopPlanSummary(out string) string {
	out = strings.Join(strings.Fields(out), " ")
	if i := strings.Index(out, " Open browser pages"); i > 0 {
		out = out[:i]
	}
	if r := []rune(out); len(r) > 300 {
		out = string(r[:300]) + "…"
	}
	return out
}

func desktopMessageText(request agentruntime.TurnRequest) string {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Message.Content
	case *agentservice.ExecuteRequest:
		if input != nil {
			return input.Message.Content
		}
	}
	return ""
}

func desktopScriptErrIsNavigation(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "inspected target navigated") || strings.Contains(m, "execution context was destroyed") || strings.Contains(m, "cannot find context with specified id")
}

func desktopTurnTask(request agentruntime.TurnRequest) string {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Message.Content
	case *agentservice.ExecuteRequest:
		if input != nil {
			return input.Message.Content
		}
	}
	return ""
}

// desktopRaiseFileDialog: a browser's native file chooser is a separate
// window that is often not the focused one (XHS-3: Chromium stayed active,
// so the password read of the page refused the path as a password). When
// such a dialog is open and this app_run types, raise it first; the
// class+title rule then knows the text goes to a file dialog, not a page.
func desktopRaiseFileDialog(run func(...string) (string, error)) bool {
	if run == nil {
		return false
	}
	out, err := run("search", "--onlyvisible", "--name", desktopFileDialogSearchPattern)
	if err != nil {
		return false
	}
	for _, id := range strings.Fields(out) {
		if _, convErr := strconv.ParseUint(id, 10, 64); convErr == nil {
			_, aerr := run("windowactivate", "--sync", id)
			return aerr == nil
		}
	}
	return false
}

const desktopFileDialogSearchPattern = `^(Open|Open Files?|Save|Save Files?|Save As|Select Files?|Choose Files?|File Upload|Upload Files?|打开|打开文件|保存|保存文件|另存为|选择文件|上传文件)$`

// desktopMaxTabs bounds the open pages: every run opens a few, and a browser
// with a dozen heavy editor tabs hangs (XHS-22 ended on a blank tab with 12
// open). Closing a tab keeps cookies and logins.
const desktopMaxTabs = 6

var desktopCloseTab = func(addr, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return browser.CloseTargetHTTP(ctx, addr, id)
}

// trimDesktopTabs closes the oldest pages beyond desktopMaxTabs, never the
// attached one. DevTools lists the most recently used pages first.
func trimDesktopTabs(addr, attachedID string, targets []browser.TargetInfo) []browser.TargetInfo {
	pages := 0
	out := make([]browser.TargetInfo, 0, len(targets))
	for _, t := range targets {
		if t.Type != "page" || strings.TrimSpace(t.ID) == "" {
			out = append(out, t)
			continue
		}
		pages++
		if pages > desktopMaxTabs && t.ID != attachedID {
			if err := desktopCloseTab(addr, t.ID); err == nil {
				log.Printf("[desktop] closed extra tab %s (%d open > %d)", desktopPageLocation(t.URL), pages, desktopMaxTabs)
				continue
			}
		}
		out = append(out, t)
	}
	return out
}
