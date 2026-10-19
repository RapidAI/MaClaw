package desktopbot

import (
	"strings"
	"sync"
)

// Driving actions stay on the computer helper. The main agent does not
// receive them, and a read-only screenshot has no coordinates.
var mainDrivingDesktopActions = map[string]bool{
	"click": true, "type": true, "script": true, "computer": true,
}

var screenshotClickParams = map[string]bool{
	"x": true, "y": true, "coordinate": true, "coordinates": true,
	"click": true, "type": true, "ref": true, "region": true,
	"steps": true, "actions": true,
}

// MainDropsTool reports a name the main agent must not be offered.
// computer_* and gui_* drive the local desktop. click, type, script, and
// computer are the desktop actions that do the same on the cloud desktop.
func MainDropsTool(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "computer_") || strings.HasPrefix(name, "gui_") {
		return true
	}
	if mainDrivingDesktopActions[name] {
		return true
	}
	if action, ok := strings.CutPrefix(name, "desktop."); ok {
		return mainDrivingDesktopActions[action]
	}
	return false
}

// MainAgentToolNames keeps the main agent's list. Driving names are dropped.
// At most one screenshot remains.
func MainAgentToolNames(names []string) []string {
	out := make([]string, 0, len(names))
	sawShot := false
	for _, name := range names {
		if MainDropsTool(name) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "screenshot") {
			if sawShot {
				continue
			}
			sawShot = true
		}
		out = append(out, name)
	}
	return out
}

// ReadonlyScreenshotParams is the only parameter set a main-agent screenshot
// may advertise. It has no coordinates, click, or type.
func ReadonlyScreenshotParams() []string {
	return []string{"session_id"}
}

// ScreenshotParamsHaveClick reports a coordinate, click, or type parameter.
func ScreenshotParamsHaveClick(params []string) bool {
	for _, param := range params {
		if screenshotClickParams[strings.ToLower(strings.TrimSpace(param))] {
			return true
		}
	}
	return false
}

// StripScreenshotClickParams removes coordinate, click, and type parameters.
func StripScreenshotClickParams(params []string) []string {
	out := make([]string, 0, len(params))
	for _, param := range params {
		if screenshotClickParams[strings.ToLower(strings.TrimSpace(param))] {
			continue
		}
		out = append(out, param)
	}
	return out
}

// HelperCard is the only text the computer helper receives from the main agent.
type HelperCard struct {
	Goal            string
	Constraints     string
	SuccessCriteria string
}

// Text is the card. It does not name a driving action and does not ask for a chat reply.
func (c HelperCard) Text() string {
	return "goal: " + strings.TrimSpace(c.Goal) +
		"\nconstraints: " + strings.TrimSpace(c.Constraints) +
		"\nsuccess: " + strings.TrimSpace(c.SuccessCriteria)
}

// CardForGoal builds the helper card for one desktop job.
func CardForGoal(goal string) HelperCard {
	return HelperCard{
		Goal:            strings.TrimSpace(goal),
		Constraints:     "不发送，不付款，不新开站点。",
		SuccessCriteria: "交回三行结论和文件路径。空结论且没有路径算失败。",
	}
}

// GateInput is one decision about opening the computer helper.
type GateInput struct {
	Open     bool
	Screen   string
	HelperID string
	Goal     string
	Held     map[string]string
}

// GateResult is what the main loop is allowed to do. MainActive is always false.
type GateResult struct {
	MainActive bool
	Dispatched bool
	Queued     bool
	Card       HelperCard
	Held       map[string]string
}

// DispatchComputerGate opens one computer helper for a screen.
// The main loop does not become the operator. A second helper on that screen waits.
func DispatchComputerGate(in GateInput) GateResult {
	out := GateResult{Held: cloneScreenHold(in.Held)}
	if !in.Open {
		return out
	}
	out.Card = CardForGoal(in.Goal)
	screen := strings.TrimSpace(in.Screen)
	if screen == "" {
		screen = "default"
	}
	id := strings.TrimSpace(in.HelperID)
	if id == "" {
		id = "helper"
	}
	if out.Held == nil {
		out.Held = map[string]string{}
	}
	if cur := out.Held[screen]; cur != "" && cur != id {
		out.Queued = true
		return out
	}
	out.Held[screen] = id
	out.Dispatched = true
	return out
}

func cloneScreenHold(held map[string]string) map[string]string {
	if len(held) == 0 {
		return nil
	}
	out := make(map[string]string, len(held))
	for key, value := range held {
		out[key] = value
	}
	return out
}

// MainHelperView is what the main agent keeps from a helper. The trace stays out.
func MainHelperView(trace, report string) (string, error) {
	shown, err := CollapseHelperReport(report, nil)
	if err != nil {
		return "", err
	}
	if trace != "" && strings.Contains(shown, trace) {
		return "", errTraceInMain
	}
	return shown, nil
}

type traceError string

func (e traceError) Error() string { return string(e) }

const errTraceInMain traceError = "helper trace entered the main context"

var computerScreenHold sync.Map

// ClaimComputerScreen keeps one computer helper on a screen.
// The same helper may claim again. A different helper is queued.
func ClaimComputerScreen(screen, helperID string) (started, queued bool) {
	screen, helperID = screenKey(screen, helperID)
	actual, loaded := computerScreenHold.LoadOrStore(screen, helperID)
	if !loaded {
		return true, false
	}
	if actual.(string) == helperID {
		return true, false
	}
	return false, true
}

// ReleaseComputerScreen frees a screen when that helper finishes.
func ReleaseComputerScreen(screen, helperID string) {
	screen, helperID = screenKey(screen, helperID)
	if cur, ok := computerScreenHold.Load(screen); ok && cur.(string) == helperID {
		computerScreenHold.Delete(screen)
	}
}

// ResetComputerScreens drops the process-local screen lease. Tests use it.
func ResetComputerScreens() {
	computerScreenHold.Range(func(key, _ any) bool {
		computerScreenHold.Delete(key)
		return true
	})
}

func screenKey(screen, helperID string) (string, string) {
	screen = strings.TrimSpace(screen)
	if screen == "" {
		screen = "default"
	}
	helperID = strings.TrimSpace(helperID)
	if helperID == "" {
		helperID = "helper"
	}
	return screen, helperID
}
