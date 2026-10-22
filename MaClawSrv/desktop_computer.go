package main

// Mouse, keyboard, clipboard, window, and accessibility steps for programs
// other than the browser, and the computer-use compatibility layer.
//
// app_run steps, Anthropic computer-use actions (computer_20250124 style:
// left_click, right_click, double_click, triple_click, middle_click,
// mouse_move, left_click_drag, left_mouse_down, left_mouse_up, scroll, key,
// hold_key, type, cursor_position, wait, screenshot, zoom), and OpenAI CUA
// actions (click, double_click, scroll, type, keypress, wait, screenshot,
// move, drag) all parse into one computeruse.Step. Coordinates in a step are
// pixels of the last full screenshot this binding took; desktopFrame maps
// them back onto the screen, so a scaled screenshot and the click agree.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// computeruse.MaxSteps bounds one computer batch or app_run.
// computeruse.WaitMaxSeconds bounds one wait or hold inside the app client.
// computeruse.ScrollMax bounds the wheel clicks of one scroll step.
// computeruse.DragMaxPoints bounds the points of one drag path.
// desktopFrame is how model coordinates map onto the screen: the geometry
// of the last full screenshot and the options it used. The zero value is
// the identity (unscaled screenshots).
type desktopFrame struct {
	mu   sync.Mutex
	shot desktop.Shot
	opts desktop.ShotOptions
}

func (f *desktopFrame) get() (desktop.Shot, desktop.ShotOptions) {
	if f == nil {
		return desktop.Shot{Scale: 1}, desktop.ShotOptions{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	shot := f.shot
	if shot.Scale <= 0 {
		shot.Scale = 1
	}
	return shot, f.opts
}

func (f *desktopFrame) set(shot desktop.Shot, opts desktop.ShotOptions) {
	if f == nil {
		return
	}
	shot.Data = nil
	opts.Region = desktop.ShotRegion{}
	opts.Marks = false
	f.mu.Lock()
	f.shot, f.opts = shot, opts
	f.mu.Unlock()
}

var desktopFrames sync.Map

// desktopPageNeed remembers the last observation that handed the page to
// the person. A later computer batch must not fill a captcha by pixels.
// The record is process-local, like the prompt pin and the screen lease.
var desktopPageNeed sync.Map

func noteDesktopPageNeed(scope agentruntime.Scope, text string) {
	if !strings.Contains(text, "page flags:") && !strings.Contains(text, "handoff=captcha") && !strings.Contains(text, "captcha challenge") {
		return
	}
	key := desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID)
	switch desktopAttentionReason(text) {
	case "captcha_widget", "login_qr", "payment_confirm":
		desktopPageNeed.Store(key, desktopAttentionReason(text))
	default:
		desktopPageNeed.Delete(key)
	}
}

func desktopPageNeedFor(scope agentruntime.Scope) string {
	value, ok := desktopPageNeed.Load(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
	if !ok {
		return ""
	}
	reason, _ := value.(string)
	return strings.TrimSpace(reason)
}

// desktopFrameFor is the frame of one user's desktop.
func desktopFrameFor(scope agentruntime.Scope) *desktopFrame {
	key := desktopRunKey(scope.TenantID, scope.UserID)
	value, _ := desktopFrames.LoadOrStore(key, &desktopFrame{})
	return value.(*desktopFrame)
}

// computeruse.Step is one parsed action.
// desktopStepInput reports whether a step changes the screen.
// insertsText reports whether the step can put characters into the focus.
// desktopKeyAliases maps OpenAI CUA and common key names to X keysyms.
// computeruse.KeyCombo turns "ctrl+s", "CTRL+S", or ["CTRL","S"] into one keysym chord.
// computeruse.ParseSteps reads app_run steps or a computer batch.
// computeruse.ParseStep reads one action in any of the three dialects.
// computeruse.StepArgv is the xdotool argv of one step, with coordinates mapped
// through the frame. Steps that are not xdotool (screenshot, zoom, paste,
// set_text, element ids) return nil.
// desktopAppStepsArgv keeps the old app_run contract: every step is one
// xdotool argv. A step that needs more than xdotool is an error here.
func desktopAppStepsArgv(steps []computeruse.Step, frame desktop.Shot) ([][]string, error) {
	out := make([][]string, 0, len(steps))
	for _, step := range steps {
		argv, err := computeruse.StepArgv(step, frame)
		if err != nil {
			return nil, err
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("desktop step %s needs the desktop control service", step.Kind)
		}
		out = append(out, argv)
	}
	return out, nil
}

// computeruse.CtlOK turns an "ok":false answer into an error.
// computeruse.ResultsText lists every step; steps after a failure are named
// as not executed, like the computer-use reference implementation.
// computeruse.ShotOptionsFromArgs reads screenshot options from tool arguments.
// region is in screenshot pixels of the current frame (zoom); it is mapped
// to screen pixels here.
// desktopCaptureShot takes one screenshot with options and returns the tool
// text with the image attached. A full (not zoomed) shot becomes the frame
// that later coordinates use. Plain options keep the original capture.
func desktopCaptureShot(ctx context.Context, scope agentruntime.Scope, display, name string, opts desktop.ShotOptions, clickPixels bool) (string, desktop.Shot, error) {
	frame := desktopFrameFor(scope)
	if opts.Plain() {
		text, err := desktopCapture(ctx, scope, display, clickPixels, name)
		if err == nil {
			frame.set(desktop.Shot{Scale: 1}, opts)
		}
		return text, desktop.Shot{Scale: 1}, err
	}
	var shot desktop.Shot
	var saved string
	var err error
	switch {
	case desktopRemoteShot != nil:
		shot, err = desktopRemoteShot(ctx, scope.TenantID, scope.UserID, display, opts)
	case desktopHubClientFromEnv() != nil:
		shot, saved, err = desktopHubClientFromEnv().screenshotWith(ctx, scope.TenantID, scope.UserID, display, name, opts)
	default:
		if err = refuseLocalDesktop(ctx); err != nil {
			return "", desktop.Shot{}, err
		}
		shot, err = desktopShotLocal(display, opts)
	}
	if err != nil {
		return "", desktop.Shot{}, err
	}
	zoom := !opts.Region.Empty()
	if !zoom {
		frame.set(shot, opts)
	}
	encoded := base64.StdEncoding.EncodeToString(shot.Data)
	fits := len(encoded) <= desktopScreenshotMaxBase64
	shown := fits && noteDesktopShot(ctx, shot.MIME, encoded)
	visible := "The image could not be attached (too large); use max_width or format=jpeg."
	if fits {
		visible = "The image is attached for models that can see images."
	}
	if shown {
		visible = "The image is attached for models that can see images and is shown in the chat."
	}
	text := fmt.Sprintf("Desktop screenshot of display %s: image %dx%d (%s, %d KB) of a %dx%d screen, scale %.4f.",
		strings.TrimSpace(display), shot.ImageWidth, shot.ImageHeight, shot.MIME, len(shot.Data)/1024, shot.ScreenWidth, shot.ScreenHeight, shot.Scale)
	if zoom {
		text += fmt.Sprintf(" Zoom of screen region x=%d y=%d %dx%d. Coordinates for actions still use the last full screenshot, not this zoom.",
			shot.Region.X, shot.Region.Y, shot.Region.Width, shot.Region.Height)
	} else if clickPixels {
		text += " app_run and computer coordinates are pixels of this image; they are converted to screen pixels."
	}
	if shot.HasCursor {
		ix, iy := shot.FromScreen(shot.CursorX, shot.CursorY)
		text += fmt.Sprintf(" Mouse pointer at (%d, %d) in this image.", ix, iy)
	}
	text += " " + visible
	if saved != "" {
		text += " Saved " + saved + ". That is the only file this action wrote. Report this path."
		noteDesktopSavedPath(ctx, saved)
	}
	if shot.MIME == "image/png" {
		if vision := desktopVisionText(ctx, shot.Data, clickPixels && !zoom); vision != "" {
			text += "\n" + vision
		}
	}
	if !fits {
		return text, shot, nil
	}
	return agentruntime.AttachModelImage(text, shot.MIME, encoded), shot, nil
}

// desktopCtlCall runs one control request for this user.
func desktopCtlCall(ctx context.Context, scope agentruntime.Scope, display string, request map[string]any) (map[string]any, error) {
	if desktopRemoteCtl != nil {
		return desktopRemoteCtl(ctx, scope.TenantID, scope.UserID, display, request)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.Ctl(ctx, scope.TenantID, scope.UserID, display, request)
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return nil, err
	}
	return desktopCtlLocal(display, request)
}

// desktopCtlResultText renders a control answer for the model.
func desktopCtlResultText(action string, out map[string]any, frame desktop.Shot) string {
	switch action {
	case "app_probe":
		return computeruse.ProbeText(out, frame)
	case "window_list":
		windows, _ := out["windows"].([]any)
		var b strings.Builder
		fmt.Fprintf(&b, "%d window(s):\n", len(windows))
		for _, raw := range windows {
			w, _ := raw.(map[string]any)
			x, _ := computeruse.Number(w["x"])
			y, _ := computeruse.Number(w["y"])
			ww, _ := computeruse.Number(w["width"])
			hh, _ := computeruse.Number(w["height"])
			flags := []string{}
			for _, flag := range []string{"active", "maximized", "minimized"} {
				if v, _ := w[flag].(bool); v {
					flags = append(flags, flag)
				}
			}
			fmt.Fprintf(&b, "%v %q class=%v pid=%v desktop=%v screen %d,%d %dx%d %s\n", w["id"], w["title"], w["class"], w["pid"], w["desktop"], int(x), int(y), int(ww), int(hh), strings.Join(flags, ","))
		}
		return strings.TrimSpace(b.String())
	case "clipboard_get":
		if image, _ := out["image_base64"].(string); image != "" {
			text := fmt.Sprintf("Clipboard holds a PNG image (%d KB).", len(image)*3/4/1024)
			if len(image) <= desktopScreenshotMaxBase64 {
				return agentruntime.AttachModelImage(text, "image/png", image)
			}
			return text
		}
		text, _ := out["text"].(string)
		if empty, _ := out["empty"].(bool); empty {
			return "Clipboard is empty."
		}
		return "Clipboard text:\n" + trimDesktopText(text)
	case "screen_info":
		w, _ := computeruse.Number(out["width"])
		h, _ := computeruse.Number(out["height"])
		text := fmt.Sprintf("Screen %dx%d.", int(w), int(h))
		if frame.ScreenWidth > 0 {
			text += fmt.Sprintf(" The last screenshot was %dx%d (scale %.4f); coordinates for actions use that image.", frame.ImageWidth, frame.ImageHeight, frame.Scale)
		}
		return text
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

// desktopCtlAction reports whether a tool action is a control request.
func desktopCtlAction(action string) bool {
	switch action {
	case "app_probe", "clipboard_get", "clipboard_set", "window_list", "window", "screen_info", "wait":
		return true
	}
	return false
}

// desktopComputerActions is the batch of action=computer: actions (or
// steps), or the single action described by the arguments themselves
// (computer_action, coordinate, ...).
func desktopComputerActions(args map[string]any) any {
	if list, ok := args["actions"].([]any); ok {
		return list
	}
	if list, ok := args["steps"].([]any); ok {
		return list
	}
	if name := computeruse.StringArg(args, "computer_action"); name != "" {
		single := map[string]any{}
		for key, value := range args {
			single[key] = value
		}
		single["action"] = name
		delete(single, "computer_action")
		return []any{single}
	}
	return nil
}

// desktopAppStepsLegacy reports whether app_run can take the original path:
// every step is focus, type, key, or click by x,y, and screenshots are not
// scaled, so model pixels are screen pixels.
func desktopAppStepsLegacy(scope agentruntime.Scope, raw any) bool {
	if _, err := desktopAppSteps(raw); err != nil {
		return false
	}
	frame, _ := desktopFrameFor(scope).get()
	return frame.Scale == 1 && frame.Region.Empty()
}

// desktopStepsInsertIntoFocus is appStepsInsertIntoFocus for parsed steps:
// text typed before anything moved the focus lands in the focused control.
func desktopStepsInsertIntoFocus(raw any) bool {
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return false
	}
	for _, step := range steps {
		switch {
		case step.ID != 0 && step.Kind != "set_text":
			return false
		case step.Kind == "type" || step.Kind == "paste":
			return true
		case step.Kind == "key":
			return step.InsertsText()
		case step.Input():
			return false
		}
	}
	return false
}

// operateDesktopSteps runs app_run steps with the new step kinds, or a
// computer batch, and reports every step.
func operateDesktopSteps(ctx context.Context, scope agentruntime.Scope, display, action string, args map[string]any, raw any, beforeInsert func() error, browserRaises func() bool) (string, error) {
	raw, groundNotes, groundErr := groundDesktopSteps(ctx, scope, display, raw)
	if groundErr != nil {
		return "", fmt.Errorf("desktop %s: %w", action, groundErr)
	}
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return "", fmt.Errorf("desktop %s: %w", action, err)
	}
	frame := desktopFrameFor(scope)
	shotFrame, base := frame.get()
	if opts, changed, optErr := computeruse.ShotOptionsFromArgs(args, shotFrame, base); optErr != nil {
		return "", optErr
	} else if changed {
		// New screenshot options apply from this batch's first shot on.
		opts.Region = desktop.ShotRegion{}
		opts = desktopPinShotSize(opts)
		frame.mu.Lock()
		frame.opts = opts
		frame.mu.Unlock()
	}
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
	env := desktopComputerEnv(ctx, scope, display, frame, run, beforeInsert, browserRaises)
	if action == "computer" {
		env.StructureFirst = true
		env.PageNeedsPerson = desktopPageNeedFor(scope)
	}
	results, image, runErr := computeruse.Run(env, steps, action == "computer")
	text := computeruse.ResultsText(steps, results, runErr)
	for _, note := range groundNotes {
		text = note + "\n" + text
	}
	if image != "" {
		text += "\n" + image
	}
	if runErr != nil {
		return text, runErr
	}
	return text, nil
}

// operateDesktopCtl runs clipboard, window, screen, wait, and app_probe.
func operateDesktopCtl(ctx context.Context, scope agentruntime.Scope, display, action string, args map[string]any) (string, error) {
	frame, opts := desktopFrameFor(scope).get()
	request, err := computeruse.CtlRequest(action, args, frame)
	if err != nil {
		return "", err
	}
	out, err := computeruse.CtlOK(desktopCtlCall(ctx, scope, display, request))
	if err != nil {
		return "", fmt.Errorf("desktop %s: %w", action, err)
	}
	text := desktopCtlResultText(action, out, frame)
	if action == "app_probe" {
		if marks, _ := args["marks"].(bool); marks {
			opts.Marks = true
			if shot, _, shotErr := desktopCaptureShot(ctx, scope, display, "", opts, true); shotErr == nil {
				text += "\nNumbered boxes on this screenshot are the element ids above.\n" + shot
			}
		}
	}
	return text, nil
}

// desktopComputerEnv connects the shared step runner to this turn: xdotool
// through the guarded app_run path, control requests, screenshots with the
// frame, and the takeover check between steps.
func desktopComputerEnv(ctx context.Context, scope agentruntime.Scope, display string, frame *desktopFrame, run func(...string) (string, error), beforeInsert func() error, browserRaises func() bool) computeruse.Env {
	return computeruse.Env{
		Ctx:   ctx,
		Frame: frame.get,
		Exec: func(batch [][]string) (string, error) {
			return runGuardedDesktopAppSteps(batch, beforeInsert, browserRaises, run, nil)
		},
		Ctl: func(request map[string]any) (map[string]any, error) {
			return desktopCtlCall(ctx, scope, display, request)
		},
		Capture: func(opts desktop.ShotOptions, clickPixels bool) (string, error) {
			text, _, err := desktopCaptureShot(ctx, scope, display, "", opts, clickPixels)
			return text, err
		},
		Blocked: func() error { return desktopBlockedByPerson(scope) },
		WindowClass: func() string {
			argv := append([]string{}, desktopWindowClassRead...)
			out, err := run(argv...)
			if err != nil {
				return ""
			}
			return strings.TrimSpace(out)
		},
		Settle: func() { desktopAppWait(ctx, desktopAppSettle) },
	}
}
