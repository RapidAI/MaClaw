package computeruse

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Env is what running steps needs from the caller.
type Env struct {
	Ctx context.Context
	// Frame is the geometry of the last full screenshot and its options.
	Frame func() (desktop.Shot, desktop.ShotOptions)
	// Exec runs xdotool argvs in order in one call; MaClawSrv guards it
	// (password fields, stuck modifiers, the Whisker Menu).
	Exec func(batch [][]string) (string, error)
	// Ctl runs one desktop control request (desktop.Ctl* ops).
	Ctl func(map[string]any) (map[string]any, error)
	// Capture takes a screenshot and returns the caller's rendering of it.
	Capture func(opts desktop.ShotOptions, clickPixels bool) (string, error)
	// Blocked returns an error while a person has taken the desktop over.
	Blocked func() error
	// WindowClass is the WM_CLASS of the focused window, or "".
	WindowClass func() string
	// Settle waits for the screen to draw before the closing screenshot.
	Settle func()
}

// Run runs parsed steps in order and stops at the first failure.
// perStep runs every xdotool step in its own guarded call so each result is
// known; otherwise neighbouring xdotool steps share one guarded call, as
// app_run always did. The last screenshot or zoom image (or a fresh shot
// after input) is returned separately so only one image is attached.
func Run(env Env, steps []Step, perStep bool) ([]Result, string, error) {
	results := make([]Result, 0, len(steps))
	frame, _ := env.Frame()
	var batch [][]string
	var batchKinds []Step
	image := ""
	inputAfterImage := false
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		out, err := env.Exec(batch)
		kinds := batchKinds
		batch, batchKinds = nil, nil
		for i, step := range kinds {
			res := Result{Kind: step.Kind, OK: err == nil}
			if i == len(kinds)-1 {
				res.Detail = strings.TrimSpace(out)
				if err != nil {
					res.Detail = strings.TrimSpace(strings.TrimSpace(out) + " " + err.Error())
				} else if step.Kind == "cursor_position" {
					res.Detail = CursorText(out, frame)
				}
			}
			results = append(results, res)
		}
		return err
	}
	for _, step := range steps {
		if err := blocked(env); err != nil {
			if ferr := flush(); ferr != nil {
				return results, image, ferr
			}
			return results, image, err
		}
		if env.Ctx != nil && env.Ctx.Err() != nil {
			return results, image, env.Ctx.Err()
		}
		argv, err := StepArgv(step, frame)
		if err != nil {
			return results, image, err
		}
		if len(argv) > 0 {
			batch = append(batch, argv)
			batchKinds = append(batchKinds, step)
			if step.Input() {
				inputAfterImage = true
			}
			if perStep || argCount(batch) > desktop.MaxAppArgs-24 {
				if err := flush(); err != nil {
					return results, image, err
				}
			}
			continue
		}
		if err := flush(); err != nil {
			return results, image, err
		}
		res, shot, err := runSpecial(env, step, frame)
		if shot != "" {
			image = shot
			inputAfterImage = false
		}
		if step.Kind == "screenshot" {
			frame, _ = env.Frame()
		} else if step.Input() {
			inputAfterImage = true
		}
		results = append(results, res)
		if err != nil {
			return results, image, err
		}
	}
	if err := flush(); err != nil {
		return results, image, err
	}
	if inputAfterImage && env.Capture != nil && (env.Ctx == nil || env.Ctx.Err() == nil) {
		if env.Settle != nil {
			env.Settle()
		}
		_, opts := env.Frame()
		if text, err := env.Capture(opts, true); err == nil {
			image = text
		}
	}
	return results, image, nil
}

// runSpecial runs one step that is not plain xdotool.
func runSpecial(env Env, step Step, frame desktop.Shot) (Result, string, error) {
	res := Result{Kind: step.Kind}
	fail := func(err error) (Result, string, error) {
		res.Detail = err.Error()
		return res, "", err
	}
	switch {
	case step.Kind == "screenshot":
		if env.Capture == nil {
			return fail(fmt.Errorf("screenshot is not available here"))
		}
		_, opts := env.Frame()
		text, err := env.Capture(opts, true)
		if err != nil {
			return fail(err)
		}
		res.OK, res.Detail = true, "screenshot taken"
		return res, text, nil
	case step.Kind == "zoom":
		if env.Capture == nil {
			return fail(fmt.Errorf("zoom is not available here"))
		}
		x0, y0 := frame.ToScreen(step.Region[0], step.Region[1])
		x1, y1 := frame.ToScreen(step.Region[2], step.Region[3])
		if x1 <= x0 || y1 <= y0 {
			return fail(fmt.Errorf("zoom region is empty on the screen"))
		}
		_, opts := env.Frame()
		opts.Region = desktop.ShotRegion{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
		text, err := env.Capture(opts, false)
		if err != nil {
			return fail(err)
		}
		res.OK, res.Detail = true, fmt.Sprintf("zoomed screen region %d,%d %dx%d", x0, y0, x1-x0, y1-y0)
		return res, text, nil
	case step.Kind == "paste":
		if env.Ctl == nil {
			return fail(fmt.Errorf("paste needs the desktop control service"))
		}
		if _, err := CtlOK(env.Ctl(map[string]any{"op": desktop.CtlClipboardSet, "text": step.Text})); err != nil {
			return fail(err)
		}
		chord := "ctrl+v"
		if env.WindowClass != nil && WindowIsTerminal(env.WindowClass()) {
			chord = "ctrl+shift+v"
		}
		if out, err := env.Exec([][]string{{"key", chord}}); err != nil {
			return fail(fmt.Errorf("%s %v", strings.TrimSpace(out), err))
		}
		res.OK, res.Detail = true, fmt.Sprintf("pasted %d characters with %s", len([]rune(step.Text)), chord)
		return res, "", nil
	case step.ID != 0:
		return runElementStep(env, step)
	}
	return fail(fmt.Errorf("step %s is not supported", step.Kind))
}

// runElementStep acts on an element of the last app_probe.
func runElementStep(env Env, step Step) (Result, string, error) {
	res := Result{Kind: step.Kind}
	if env.Ctl == nil {
		res.Detail = "element steps need the desktop control service"
		return res, "", fmt.Errorf("%s", res.Detail)
	}
	do := map[string]string{"click": "click", "focus": "focus", "set_text": "set_text", "type": "set_text", "move": "locate"}[step.Kind]
	if do == "" {
		res.Detail = fmt.Sprintf("%s does not take an element id", step.Kind)
		return res, "", fmt.Errorf("%s", res.Detail)
	}
	if step.Kind == "click" && (step.Button != 1 || step.Clicks != 1 || len(step.Modifiers) > 0) {
		do = "locate"
	}
	request := map[string]any{"op": desktop.CtlA11yAct, "id": step.ID, "do": do}
	if do == "set_text" {
		request["text"] = step.Text
	}
	out, err := CtlOK(env.Ctl(request))
	if err != nil {
		res.Detail = err.Error()
		return res, "", err
	}
	method, _ := out["method"].(string)
	center, _ := out["center"].([]any)
	cx, cy, hasCenter := 0.0, 0.0, false
	if len(center) == 2 {
		cx, _ = Number(center[0])
		cy, _ = Number(center[1])
		hasCenter = true
	}
	label := fmt.Sprintf("element %d (%v %q)", step.ID, out["role"], out["name"])
	if method != "xdotool" && method != "locate" {
		res.OK, res.Detail = true, fmt.Sprintf("%s: %s via %s", label, do, method)
		return res, "", nil
	}
	if !hasCenter {
		res.Detail = label + " has no position on screen"
		return res, "", fmt.Errorf("%s", res.Detail)
	}
	// The fallback acts at the element's center in screen pixels.
	screen := desktop.Shot{Scale: 1}
	pointer := step
	pointer.ID, pointer.HasXY, pointer.X, pointer.Y = 0, true, cx, cy
	var argvs [][]string
	switch step.Kind {
	case "click", "move":
		argv, _ := StepArgv(pointer, screen)
		argvs = append(argvs, argv)
	case "focus":
		pointer.Kind, pointer.Button, pointer.Clicks, pointer.Modifiers = "click", 1, 1, nil
		argv, _ := StepArgv(pointer, screen)
		argvs = append(argvs, argv)
	case "set_text", "type":
		pointer.Kind, pointer.Button, pointer.Clicks, pointer.Modifiers = "click", 1, 1, nil
		argv, _ := StepArgv(pointer, screen)
		argvs = append(argvs, argv, []string{"key", "ctrl+a"})
		if strings.TrimSpace(step.Text) != "" {
			argvs = append(argvs, []string{"type", "--delay", "20", step.Text})
		} else {
			argvs = append(argvs, []string{"key", "BackSpace"})
		}
	}
	if text, err := env.Exec(argvs); err != nil {
		res.Detail = strings.TrimSpace(label + ": " + text + " " + err.Error())
		return res, "", err
	}
	res.OK, res.Detail = true, fmt.Sprintf("%s: %s at screen (%d, %d) with xdotool", label, step.Kind, int(cx), int(cy))
	return res, "", nil
}

func blocked(env Env) error {
	if env.Blocked == nil {
		return nil
	}
	return env.Blocked()
}

// ProbeText formats an app_probe answer. Bounds are screen pixels;
// with a scaled frame the screenshot pixels are listed too.
func ProbeText(out map[string]any, frame desktop.Shot) string {
	elements, _ := out["elements"].([]any)
	scaled := frame.Scale != 1 || !frame.Region.Empty()
	var b strings.Builder
	fmt.Fprintf(&b, "Accessible elements (%v window(s), %d listed", out["windows"], len(elements))
	if t, _ := out["truncated"].(bool); t {
		b.WriteString(", truncated: interactive first; raise max_nodes or narrow window")
	}
	b.WriteString("). Act on one with app_run or computer steps: {\"action\":\"click\",\"id\":N}, {\"action\":\"focus\",\"id\":N}, {\"action\":\"set_text\",\"id\":N,\"text\":\"...\"}.\n")
	for _, raw := range elements {
		e, _ := raw.(map[string]any)
		bounds, _ := e["bounds"].([]any)
		var v [4]int
		for i := 0; i < 4 && i < len(bounds); i++ {
			n, _ := Number(bounds[i])
			v[i] = int(n)
		}
		fmt.Fprintf(&b, "[%v] %v %q", e["id"], e["role"], e["name"])
		if app, _ := e["app"].(string); app != "" {
			fmt.Fprintf(&b, " (window of %s)", app)
		}
		fmt.Fprintf(&b, " screen %d,%d %dx%d", v[0], v[1], v[2], v[3])
		if scaled {
			x0, y0 := frame.FromScreen(v[0], v[1])
			x1, y1 := frame.FromScreen(v[0]+v[2], v[1]+v[3])
			fmt.Fprintf(&b, " = image %d,%d %dx%d", x0, y0, x1-x0, y1-y0)
		}
		if states, _ := e["states"].([]any); len(states) > 0 {
			parts := make([]string, 0, len(states))
			for _, s := range states {
				parts = append(parts, fmt.Sprint(s))
			}
			sort.Strings(parts)
			fmt.Fprintf(&b, " [%s]", strings.Join(parts, ","))
		}
		if value, _ := e["value"].(string); value != "" {
			fmt.Fprintf(&b, " value=%q", value)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// CtlRequest builds the control request for the tool actions
// clipboard_get, clipboard_set, window_list, window, screen_info, wait, and
// app_probe. Window coordinates are screenshot pixels like clicks.
func CtlRequest(action string, args map[string]any, frame desktop.Shot) (map[string]any, error) {
	switch action {
	case "clipboard_get":
		kind := StringArg(args, "kind", "format")
		if kind != "image" {
			kind = "text"
		}
		return map[string]any{"op": desktop.CtlClipboardGet, "type": kind}, nil
	case "clipboard_set":
		text, ok := args["text"].(string)
		if !ok {
			text, ok = args["content"].(string)
		}
		if !ok {
			return nil, fmt.Errorf("clipboard_set requires text")
		}
		return map[string]any{"op": desktop.CtlClipboardSet, "text": text}, nil
	case "window_list":
		return map[string]any{"op": desktop.CtlWindows}, nil
	case "screen_info":
		return map[string]any{"op": desktop.CtlScreen}, nil
	case "window":
		do := strings.ToLower(StringArg(args, "do", "op", "window_action"))
		request := map[string]any{"op": desktop.CtlWindow, "do": do, "id": StringArg(args, "window_id", "id"), "title": StringArg(args, "title", "name")}
		if x, ok := Number(args["x"]); ok {
			y, _ := Number(args["y"])
			sx, sy := frame.ToScreen(x, y)
			request["x"], request["y"] = sx, sy
		}
		if w, ok := Number(args["width"]); ok {
			h, _ := Number(args["height"])
			request["width"], request["height"] = int(w/frame.Scale+0.5), int(h/frame.Scale+0.5)
		}
		return request, nil
	case "wait":
		request := map[string]any{"op": desktop.CtlWait, "for": StringArg(args, "for", "until")}
		if request["for"] == "" {
			request["for"] = "change"
		}
		if title := StringArg(args, "title", "name"); title != "" {
			request["title"] = title
		}
		if gone, ok := args["gone"].(bool); ok {
			request["gone"] = gone
		}
		if t, ok := Number(args["timeout"]); ok {
			request["timeout"] = math.Min(t, desktop.CtlTimeoutSeconds)
		}
		return request, nil
	case "app_probe":
		request := map[string]any{"op": desktop.CtlA11yProbe, "window": StringArg(args, "window", "title", "name")}
		for _, key := range []string{"max_nodes", "max_depth"} {
			if n, ok := Number(args[key]); ok {
				request[key] = int(n)
			}
		}
		if v, ok := args["interactive_only"].(bool); ok {
			request["interactive_only"] = v
		}
		return request, nil
	}
	return nil, fmt.Errorf("unknown desktop control action %s", action)
}

func argCount(batch [][]string) int {
	n := 0
	for _, argv := range batch {
		n += len(argv)
	}
	return n
}
