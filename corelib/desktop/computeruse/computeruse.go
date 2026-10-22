// Package computeruse parses mouse, keyboard, and screenshot actions for a
// desktop: MaClaw app_run steps, Anthropic computer-use actions
// (computer_20250124 style), and OpenAI computer-use actions, into one Step,
// and turns a Step into an xdotool argv with coordinates mapped from
// screenshot pixels onto the screen (desktop.Shot.ToScreen).
package computeruse

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// ClickMax is the farthest pixel a coordinate may name.
const ClickMax = 16384

// KeyPattern is one xdotool key chord.
var KeyPattern = regexp.MustCompile(`^[A-Za-z0-9_+-]{1,40}$`)

// TypeMaxBytes keeps one typed text inside the two-minute app client.
const TypeMaxBytes = 4000

// TypeText checks text for a type or paste step. Newlines stay.
func TypeText(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" || strings.ContainsRune(text, '\x00') {
		return "", fmt.Errorf("type requires text")
	}
	if len(text) > TypeMaxBytes {
		return "", fmt.Errorf("type text is too long")
	}
	return text, nil
}

func coordPresent(value any) bool {
	if value == nil {
		return false
	}
	text, ok := value.(string)
	return !ok || strings.TrimSpace(text) != ""
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > limit {
		return string([]rune(text)[:limit]) + "…"
	}
	return text
}

const MaxSteps = 32

const WaitMaxSeconds = 30.0

const ScrollMax = 30

const DragMaxPoints = 16

type Step struct {
	Kind      string // see StepKinds
	Name      string
	Text      string
	Keys      []string
	X, Y      float64
	HasXY     bool
	Path      [][2]float64
	Button    int
	Clicks    int
	Modifiers []string
	Direction string
	Amount    int
	Repeat    int
	Duration  float64
	Region    [4]float64
	HasRegion bool
	ID        int
}

var StepKinds = []string{
	"focus", "type", "key", "click", "move", "mouse_down", "mouse_up", "drag", "scroll",
	"cursor_position", "wait", "hold_key", "screenshot", "zoom", "paste", "set_text",
}

func (s Step) Input() bool {
	switch s.Kind {
	case "cursor_position", "wait", "screenshot", "zoom":
		return false
	case "focus":
		// Raising a window is not input; app_run never shot after it.
		return s.ID != 0
	}
	return true
}

func (s Step) InsertsText() bool {
	switch s.Kind {
	case "type", "paste":
		return true
	case "set_text":
		return s.ID == 0
	case "key":
		for _, key := range s.Keys {
			if browser.KeyInsertsText(key) {
				return true
			}
		}
	}
	return false
}

func Number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case int:
		return float64(typed), true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n, err == nil && strings.TrimSpace(typed) != ""
	}
	return 0, false
}

func pair(value any) (float64, float64, bool) {
	list, ok := value.([]any)
	if !ok || len(list) != 2 {
		return 0, 0, false
	}
	x, xok := Number(list[0])
	y, yok := Number(list[1])
	return x, y, xok && yok
}

func StringArg(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := m[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

var keyAliases = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "shift": "shift", "alt": "alt", "option": "alt",
	"cmd": "super", "command": "super", "meta": "super", "super": "super", "win": "super",
	"enter": "Return", "return": "Return", "esc": "Escape", "escape": "Escape", "space": "space",
	"backspace": "BackSpace", "tab": "Tab", "delete": "Delete", "del": "Delete", "insert": "Insert",
	"home": "Home", "end": "End", "pageup": "Page_Up", "pagedown": "Page_Down",
	"arrowup": "Up", "arrowdown": "Down", "arrowleft": "Left", "arrowright": "Right",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "capslock": "Caps_Lock",
}

func KeyName(raw string) string {
	key := strings.TrimSpace(raw)
	if alias, ok := keyAliases[strings.ToLower(key)]; ok {
		return alias
	}
	if len(key) == 1 {
		return strings.ToLower(key)
	}
	if len(key) >= 2 && (key[0] == 'F' || key[0] == 'f') {
		if _, err := strconv.Atoi(key[1:]); err == nil {
			return "F" + key[1:]
		}
	}
	return key
}

func KeyCombo(parts []string) (string, error) {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, piece := range strings.Split(part, "+") {
			if strings.TrimSpace(piece) == "" {
				continue
			}
			out = append(out, KeyName(piece))
		}
	}
	combo := strings.Join(out, "+")
	if !KeyPattern.MatchString(combo) {
		return "", fmt.Errorf("desktop key %q is not allowed", combo)
	}
	return combo, nil
}

func modifierList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []string
	for _, piece := range strings.FieldsFunc(raw, func(r rune) bool { return r == '+' || r == ' ' || r == ',' }) {
		name := KeyName(piece)
		switch name {
		case "ctrl", "shift", "alt", "super":
			out = append(out, name)
		default:
			return nil, fmt.Errorf("desktop click modifier %q must be ctrl, shift, alt, or super", piece)
		}
	}
	return out, nil
}

func ParseSteps(raw any) ([]Step, error) {
	if raw == nil {
		return nil, fmt.Errorf("desktop steps are required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var loose []map[string]any
	if err := json.Unmarshal(encoded, &loose); err != nil {
		return nil, fmt.Errorf("desktop steps must be an array of objects")
	}
	if len(loose) == 0 || len(loose) > MaxSteps {
		return nil, fmt.Errorf("desktop steps accept 1 to %d actions", MaxSteps)
	}
	steps := make([]Step, 0, len(loose))
	for i, item := range loose {
		step, err := ParseStep(item)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func ParseStep(m map[string]any) (Step, error) {
	// Anthropic tool_use block {name:"computer", input:{...}}.
	if input, ok := m["input"].(map[string]any); ok {
		m = input
	}
	m = shorthandStep(m)
	kind := strings.ToLower(strings.TrimSpace(StringArg(m, "action", "type")))
	_, hasActionField := m["action"]
	if !hasActionField {
		// OpenAI CUA uses "type" for the action and "text" for typed text.
		return parseOpenAI(kind, m)
	}
	return parseNative(kind, m)
}

func stepXY(step *Step, m map[string]any) error {
	if x, y, ok := pair(m["coordinate"]); ok {
		step.X, step.Y, step.HasXY = x, y, true
	} else if m["coordinate"] != nil {
		return fmt.Errorf("coordinate must be [x, y]")
	}
	xv, xok := m["x"]
	yv, yok := m["y"]
	if xok || yok {
		if !coordPresent(xv) || !coordPresent(yv) {
			return fmt.Errorf("x and y go together")
		}
		x, xgood := Number(xv)
		y, ygood := Number(yv)
		if !xgood || !ygood {
			return fmt.Errorf("x and y must be numbers")
		}
		step.X, step.Y, step.HasXY = x, y, true
	}
	if step.HasXY && (step.X < 0 || step.Y < 0 || step.X > ClickMax || step.Y > ClickMax) {
		return fmt.Errorf("coordinate is outside 0..%d", ClickMax)
	}
	return nil
}

func stepID(step *Step, m map[string]any) error {
	value, ok := m["id"]
	if !ok || value == nil {
		return nil
	}
	n, good := Number(value)
	if !good || n < 1 || n > 10000 || n != math.Trunc(n) {
		return fmt.Errorf("id must be an element number from app_probe")
	}
	step.ID = int(n)
	return nil
}

func parseNative(kind string, m map[string]any) (Step, error) {
	step := Step{Kind: kind, Button: 1, Clicks: 1}
	if err := stepXY(&step, m); err != nil {
		return step, err
	}
	if err := stepID(&step, m); err != nil {
		return step, err
	}
	text, _ := m["text"].(string)
	if n, ok := Number(m["duration"]); ok {
		step.Duration = n
	}
	if n, ok := Number(m["repeat"]); ok {
		step.Repeat = int(n)
	}
	switch kind {
	case "left_click", "right_click", "middle_click", "double_click", "triple_click", "click":
		step.Kind = "click"
		switch kind {
		case "right_click":
			step.Button = 3
		case "middle_click":
			step.Button = 2
		case "double_click":
			step.Clicks = 2
		case "triple_click":
			step.Clicks = 3
		}
		if b := strings.ToLower(StringArg(m, "button")); b != "" {
			switch b {
			case "left":
				step.Button = 1
			case "right":
				step.Button = 3
			case "middle", "wheel":
				step.Button = 2
			default:
				return step, fmt.Errorf("button must be left, right, or middle")
			}
		}
		if n, ok := Number(m["clicks"]); ok {
			step.Clicks = int(n)
		}
		if step.Clicks < 1 || step.Clicks > 3 {
			return step, fmt.Errorf("clicks must be 1 to 3")
		}
		mods := StringArg(m, "modifiers", "modifier")
		if mods == "" && kind != "click" {
			// Anthropic puts the held keys of a click in text.
			mods = text
		}
		list, err := modifierList(mods)
		if err != nil {
			return step, err
		}
		step.Modifiers = list
		if !step.HasXY && step.ID == 0 && kind == "click" {
			return step, fmt.Errorf("click requires x and y, a coordinate, or an element id")
		}
	case "mouse_move", "move":
		step.Kind = "move"
		if !step.HasXY {
			return step, fmt.Errorf("mouse_move requires a coordinate")
		}
	case "left_mouse_down", "mouse_down":
		step.Kind = "mouse_down"
	case "left_mouse_up", "mouse_up":
		step.Kind = "mouse_up"
	case "left_click_drag", "drag":
		step.Kind = "drag"
		sx, sy, ok := pair(m["start_coordinate"])
		if !ok {
			if fx, fok := Number(m["from_x"]); fok {
				fy, _ := Number(m["from_y"])
				sx, sy, ok = fx, fy, true
			}
		}
		end := [2]float64{step.X, step.Y}
		if tx, tok := Number(m["to_x"]); tok {
			ty, _ := Number(m["to_y"])
			if step.HasXY && !ok {
				sx, sy, ok = step.X, step.Y, true
			}
			end = [2]float64{tx, ty}
		} else if !step.HasXY {
			return step, fmt.Errorf("drag requires start_coordinate and coordinate")
		}
		if !ok {
			return step, fmt.Errorf("drag requires start_coordinate")
		}
		step.Path = [][2]float64{{sx, sy}, end}
	case "scroll":
		step.Direction = strings.ToLower(StringArg(m, "scroll_direction", "direction"))
		if step.Direction == "" {
			step.Direction = "down"
		}
		step.Amount = 3
		if n, ok := Number(m["scroll_amount"]); ok {
			step.Amount = int(n)
		} else if n, ok := Number(m["amount"]); ok {
			step.Amount = int(n)
		}
		mods := StringArg(m, "modifiers", "modifier")
		if mods == "" {
			mods = text
		}
		list, err := modifierList(mods)
		if err != nil {
			return step, err
		}
		step.Modifiers = list
	case "key", "hold_key":
		raw := StringArg(m, "key")
		if raw == "" {
			raw = text
		}
		if strings.TrimSpace(raw) == "" {
			return step, fmt.Errorf("%s requires key or text", kind)
		}
		for _, chord := range strings.Fields(raw) {
			combo, err := KeyCombo([]string{chord})
			if err != nil {
				return step, err
			}
			step.Keys = append(step.Keys, combo)
		}
		if kind == "hold_key" {
			if len(step.Keys) != 1 {
				return step, fmt.Errorf("hold_key holds one key")
			}
			if browser.KeyInsertsText(step.Keys[0]) {
				return step, fmt.Errorf("hold_key cannot hold a key that types text")
			}
			if step.Duration <= 0 {
				step.Duration = 1
			}
		}
	case "type":
		if step.ID != 0 {
			step.Kind = "set_text"
		}
		step.Text = text
	case "focus":
		step.Name = strings.TrimSpace(StringArg(m, "name", "title"))
		if step.Name == "" && step.ID == 0 {
			return step, fmt.Errorf("focus requires a window name or an element id")
		}
		if strings.ContainsAny(step.Name, "\r\n") {
			return step, fmt.Errorf("window name must be one line")
		}
	case "set_text", "paste":
		step.Text = text
		if kind == "set_text" && step.ID == 0 {
			return step, fmt.Errorf("set_text requires an element id from app_probe")
		}
	case "cursor_position", "screenshot":
	case "wait":
		if step.Duration <= 0 {
			step.Duration = 1
		}
	case "zoom":
		region, ok := m["region"].([]any)
		if !ok || len(region) != 4 {
			return step, fmt.Errorf("zoom requires region [x0, y0, x1, y1]")
		}
		for i, v := range region {
			n, good := Number(v)
			if !good || n < 0 || n > ClickMax {
				return step, fmt.Errorf("zoom region must be four numbers")
			}
			step.Region[i] = n
		}
		if step.Region[2] <= step.Region[0] || step.Region[3] <= step.Region[1] {
			return step, fmt.Errorf("zoom region must have x1 > x0 and y1 > y0")
		}
		step.HasRegion = true
	default:
		return step, fmt.Errorf("action %q is not allowed (use one of %s, or the computer-use names)", kind, strings.Join(StepKinds, ", "))
	}
	return step, checkStep(step)
}

func parseOpenAI(kind string, m map[string]any) (Step, error) {
	step := Step{Kind: kind, Button: 1, Clicks: 1}
	if err := stepXY(&step, m); err != nil {
		return step, err
	}
	switch kind {
	case "click":
		switch strings.ToLower(StringArg(m, "button")) {
		case "", "left":
		case "right":
			step.Button = 3
		case "wheel", "middle":
			step.Button = 2
		case "back":
			step.Button = 8
		case "forward":
			step.Button = 9
		default:
			return step, fmt.Errorf("button must be left, right, wheel, back, or forward")
		}
		if !step.HasXY {
			return step, fmt.Errorf("click requires x and y")
		}
	case "double_click":
		step.Kind, step.Clicks = "click", 2
		if !step.HasXY {
			return step, fmt.Errorf("double_click requires x and y")
		}
	case "move":
		if !step.HasXY {
			return step, fmt.Errorf("move requires x and y")
		}
	case "scroll":
		dx, _ := Number(m["scroll_x"])
		dy, _ := Number(m["scroll_y"])
		// One wheel click scrolls about 100 CSS pixels in Chromium and GTK.
		if math.Abs(dy) >= math.Abs(dx) {
			step.Direction = "down"
			if dy < 0 {
				step.Direction = "up"
			}
			step.Amount = int(math.Ceil(math.Abs(dy) / 100))
		} else {
			step.Direction = "right"
			if dx < 0 {
				step.Direction = "left"
			}
			step.Amount = int(math.Ceil(math.Abs(dx) / 100))
		}
		if step.Amount < 1 {
			step.Amount = 1
		}
		if step.Amount > ScrollMax {
			step.Amount = ScrollMax
		}
	case "type":
		step.Text, _ = m["text"].(string)
	case "keypress":
		step.Kind = "key"
		var keys []string
		if list, ok := m["keys"].([]any); ok {
			for _, k := range list {
				if s, ok := k.(string); ok {
					keys = append(keys, s)
				}
			}
		}
		if len(keys) == 0 {
			return step, fmt.Errorf("keypress requires keys")
		}
		combo, err := KeyCombo(keys)
		if err != nil {
			return step, err
		}
		step.Keys = []string{combo}
	case "wait":
		step.Duration = 1
		if ms, ok := Number(m["ms"]); ok {
			step.Duration = ms / 1000
		}
	case "screenshot":
	case "drag":
		list, ok := m["path"].([]any)
		if !ok || len(list) < 2 || len(list) > DragMaxPoints {
			return step, fmt.Errorf("drag requires a path of 2 to %d points", DragMaxPoints)
		}
		for _, item := range list {
			point, _ := item.(map[string]any)
			x, xok := Number(point["x"])
			y, yok := Number(point["y"])
			if !xok || !yok || x < 0 || y < 0 || x > ClickMax || y > ClickMax {
				return step, fmt.Errorf("drag path points need x and y inside 0..%d", ClickMax)
			}
			step.Path = append(step.Path, [2]float64{x, y})
		}
	default:
		return step, fmt.Errorf("action type %q is not allowed", kind)
	}
	return step, checkStep(step)
}

func checkStep(step Step) error {
	switch step.Kind {
	case "type", "paste":
		if _, err := TypeText(step.Text); err != nil {
			return err
		}
	case "set_text":
		if strings.ContainsRune(step.Text, 0) || len(step.Text) > TypeMaxBytes {
			return fmt.Errorf("set_text text is too long or invalid")
		}
	case "scroll":
		switch step.Direction {
		case "up", "down", "left", "right":
		default:
			return fmt.Errorf("scroll direction must be up, down, left, or right")
		}
		if step.Amount < 1 || step.Amount > ScrollMax {
			return fmt.Errorf("scroll amount must be 1 to %d", ScrollMax)
		}
	case "wait", "hold_key":
		if step.Duration <= 0 || step.Duration > WaitMaxSeconds {
			return fmt.Errorf("duration must be above 0 and at most %.0f seconds", WaitMaxSeconds)
		}
	case "key":
		if step.Repeat < 0 || step.Repeat > 50 {
			return fmt.Errorf("repeat must be 0 to 50")
		}
	}
	return nil
}

func seconds(d float64) string {
	return strconv.FormatFloat(d, 'f', -1, 64)
}

func StepArgv(step Step, frame desktop.Shot) ([]string, error) {
	if step.ID != 0 {
		return nil, nil
	}
	pt := func(x, y float64) (string, string) {
		sx, sy := frame.ToScreen(x, y)
		return strconv.Itoa(sx), strconv.Itoa(sy)
	}
	var argv []string
	move := func() {
		if step.HasXY {
			x, y := pt(step.X, step.Y)
			argv = append(argv, "mousemove", "--sync", x, y)
		}
	}
	withMods := func(inner ...string) {
		for _, mod := range step.Modifiers {
			argv = append(argv, "keydown", mod)
		}
		argv = append(argv, inner...)
		for i := len(step.Modifiers) - 1; i >= 0; i-- {
			argv = append(argv, "keyup", step.Modifiers[i])
		}
	}
	switch step.Kind {
	case "focus":
		return []string{"search", "--onlyvisible", "--name", step.Name, "windowactivate", "--sync"}, nil
	case "type":
		return []string{"type", "--delay", "20", step.Text}, nil
	case "key":
		argv = []string{"key"}
		if step.Repeat > 1 {
			argv = append(argv, "--repeat", strconv.Itoa(step.Repeat), "--delay", "50")
		}
		return append(argv, step.Keys...), nil
	case "click":
		move()
		button := strconv.Itoa(step.Button)
		if step.Clicks > 1 {
			withMods("click", "--repeat", strconv.Itoa(step.Clicks), "--delay", "80", button)
		} else {
			withMods("click", button)
		}
		return argv, nil
	case "move":
		move()
		return argv, nil
	case "mouse_down":
		move()
		return append(argv, "mousedown", "1"), nil
	case "mouse_up":
		move()
		return append(argv, "mouseup", "1"), nil
	case "drag":
		x0, y0 := pt(step.Path[0][0], step.Path[0][1])
		argv = append(argv, "mousemove", "--sync", x0, y0, "mousedown", "1", "sleep", "0.1")
		if len(step.Path) == 2 {
			// One point between keeps toolkits that need motion to start a drag.
			mx, my := pt((step.Path[0][0]+step.Path[1][0])/2, (step.Path[0][1]+step.Path[1][1])/2)
			argv = append(argv, "mousemove", "--sync", mx, my, "sleep", "0.05")
		}
		for _, p := range step.Path[1:] {
			x, y := pt(p[0], p[1])
			argv = append(argv, "mousemove", "--sync", x, y, "sleep", "0.05")
		}
		return append(argv, "sleep", "0.1", "mouseup", "1"), nil
	case "scroll":
		move()
		button := map[string]string{"up": "4", "down": "5", "left": "6", "right": "7"}[step.Direction]
		withMods("click", "--repeat", strconv.Itoa(step.Amount), "--delay", "30", button)
		return argv, nil
	case "cursor_position":
		return []string{"getmouselocation", "--shell"}, nil
	case "wait":
		return []string{"sleep", seconds(step.Duration)}, nil
	case "hold_key":
		return []string{"keydown", step.Keys[0], "sleep", seconds(step.Duration), "keyup", step.Keys[0]}, nil
	}
	return nil, nil
}

type Result struct {
	Kind   string
	OK     bool
	Detail string
}

func CursorText(out string, frame desktop.Shot) string {
	var x, y int
	found := 0
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		switch key {
		case "X":
			x, found = n, found|1
		case "Y":
			y, found = n, found|2
		}
	}
	if found != 3 {
		return strings.TrimSpace(out)
	}
	ix, iy := frame.FromScreen(x, y)
	if frame.Scale == 1 && frame.Region.Empty() {
		return fmt.Sprintf("cursor at (%d, %d)", x, y)
	}
	return fmt.Sprintf("cursor at (%d, %d) in screenshot pixels; screen (%d, %d)", ix, iy, x, y)
}

func WindowIsTerminal(class string) bool {
	class = strings.ToLower(class)
	for _, name := range []string{"terminal", "xterm", "konsole", "tilix", "alacritty", "kitty", "urxvt"} {
		if strings.Contains(class, name) {
			return true
		}
	}
	return false
}

func CtlOK(out map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	if ok, _ := out["ok"].(bool); !ok {
		message, _ := out["error"].(string)
		if strings.TrimSpace(message) == "" {
			message = "desktop control failed"
		}
		return out, fmt.Errorf("%s", message)
	}
	return out, nil
}

func ResultsText(steps []Step, results []Result, err error) string {
	var b strings.Builder
	recorded := false
	for _, r := range results {
		if !r.OK {
			recorded = true
		}
	}
	for i, step := range steps {
		if i < len(results) {
			r := results[i]
			status := "ok"
			if !r.OK {
				status = "failed"
			}
			fmt.Fprintf(&b, "%d. %s: %s", i+1, step.Kind, status)
			if r.Detail != "" {
				fmt.Fprintf(&b, " - %s", oneLine(r.Detail, 300))
			}
			b.WriteString("\n")
			continue
		}
		if i == len(results) && err != nil && !recorded {
			fmt.Fprintf(&b, "%d. %s: failed - %s\n", i+1, step.Kind, oneLine(err.Error(), 300))
			continue
		}
		fmt.Fprintf(&b, "%d. %s: not executed: an earlier computer action in this turn failed.\n", i+1, step.Kind)
	}
	return strings.TrimSpace(b.String())
}

func ShotOptionsFromArgs(args map[string]any, frame desktop.Shot, base desktop.ShotOptions) (desktop.ShotOptions, bool, error) {
	opts := base
	changed := false
	if n, ok := Number(args["max_width"]); ok {
		opts.MaxWidth, changed = int(n), true
	}
	if n, ok := Number(args["max_height"]); ok {
		opts.MaxHeight, changed = int(n), true
	}
	if f := StringArg(args, "format"); f != "" {
		opts.Format, changed = f, true
	}
	if n, ok := Number(args["quality"]); ok {
		opts.Quality, changed = int(n), true
	}
	if v, ok := args["cursor"].(bool); ok {
		opts.Cursor, changed = v, true
	}
	if v, ok := args["marks"].(bool); ok {
		opts.Marks = v
	}
	var region []any
	if r, ok := args["region"].([]any); ok {
		region = r
	} else if r, ok := args["zoom"].([]any); ok {
		region = r
	}
	if region != nil {
		if len(region) != 4 {
			return opts, false, fmt.Errorf("region must be [x0, y0, x1, y1] in screenshot pixels")
		}
		var v [4]float64
		for i := range v {
			n, ok := Number(region[i])
			if !ok || n < 0 {
				return opts, false, fmt.Errorf("region must be four non-negative numbers")
			}
			v[i] = n
		}
		x0, y0 := frame.ToScreen(v[0], v[1])
		x1, y1 := frame.ToScreen(v[2], v[3])
		if x1 <= x0 || y1 <= y0 {
			return opts, false, fmt.Errorf("region must have x1 > x0 and y1 > y0")
		}
		opts.Region = desktop.ShotRegion{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	}
	normalized, err := opts.Normalize()
	if err != nil {
		return opts, false, err
	}
	if opts.Format == "" {
		normalized.Format = ""
	}
	return normalized, changed, nil
}

// KeyInserts reports whether a key chord types text or pastes (the
// password-field guard applies to it).
func KeyInserts(key string) bool { return browser.KeyInsertsText(key) }

// shorthandStep reads the one-key step forms our own prompts and helpers use,
// such as {"key":"ctrl+v"} or {"click":{"x":10,"y":20}}, as native actions.
// Without this they carry neither action nor type and fail as action "".
func shorthandStep(m map[string]any) map[string]any {
	if _, ok := m["action"]; ok {
		return m
	}
	if _, ok := m["type"]; ok {
		return m
	}
	if k, ok := m["key"].(string); ok && strings.TrimSpace(k) != "" {
		out := map[string]any{"action": "key"}
		for name, v := range m {
			out[name] = v
		}
		return out
	}
	for _, kind := range []string{"click", "double_click", "right_click", "scroll", "move"} {
		inner, ok := m[kind].(map[string]any)
		if !ok {
			continue
		}
		out := map[string]any{"action": kind}
		for name, v := range inner {
			out[name] = v
		}
		return out
	}
	return m
}
