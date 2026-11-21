package main

// Deterministic checks and event waits backed by the Cua Driver.
//
// Two tool actions are served here, both optional: when no driver socket is
// configured (MACLAW_CUA_SOCKET) they answer with one clear line and the
// existing LLM-readback paths stay in charge.
//
//	action=verify_state   assert something about a window without spending a
//	                      model turn: a window exists or is gone, its frame is
//	                      where it should be, a named control is enabled,
//	                      selected, or holds a value. A window id from
//	                      action=window_list is the usual target.
//	action=wait_until     block inside the driver until a window appears or
//	                      goes away. No model turn is spent while it waits, so
//	                      "wait for the download dialog" costs one call.
//
// Both are read-only: they observe and wait, and never click or type.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/cua"
)

// desktopVerifyDone is the note a satisfied verify_state returns.
const desktopVerifyDone = "verified"

// desktopCuaDriver opens this host's driver, reporting the missing-socket
// case as one line a model can act on rather than an error.
func desktopCuaDriver(ctx context.Context, action string) (*cua.Driver, error) {
	d, err := cua.OpenDefault(ctx)
	if err == cua.ErrNoDriver {
		return nil, fmt.Errorf("%s needs a Cua Driver on this desktop (set %s); without one, read the screen back with action=app_probe and act on what it shows", action, cua.EnvSocket)
	}
	if err != nil {
		return nil, fmt.Errorf("%s could not reach the Cua Driver: %w", action, err)
	}
	return d, nil
}

// operateDesktopVerifyState runs action=verify_state.
//
// Arguments:
//
//	pid, window_id     the target window (from action=window_list)
//	expect             list of checks, each one of:
//	                     {window:{exists:true|false}}
//	                     {window:{bounds:{x,y,width,height},tolerance_px}}
//	                     {element:{selector:{role,label_contains}, enabled,
//	                               selected, value_equals}}
//	                     {element:{selector:{label_contains:x}, exists:true}}
//	stable_samples     how many consecutive samples must agree (default 1)
//	timeout            how long to poll, e.g. "2s" (default 2s, max 30s)
func operateDesktopVerifyState(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	d, err := desktopCuaDriver(ctx, "verify_state")
	if err != nil {
		return "", err
	}
	defer d.Close()
	pid := desktopIntArg(args, "pid")
	windowID := desktopIntArg(args, "window_id", "window")
	if pid == 0 || windowID == 0 {
		return "", fmt.Errorf("verify_state needs pid and window_id; take them from action=window_list")
	}
	raw, ok := args["expect"].([]any)
	if !ok || len(raw) == 0 {
		return "", fmt.Errorf("verify_state needs expect, a list such as [{\"window\":{\"exists\":true}}] or [{\"element\":{\"selector\":{\"label_contains\":\"保存\"},\"enabled\":true}}]")
	}
	expect := make([]cua.Expect, 0, len(raw))
	for i, item := range raw {
		one, _ := item.(map[string]any)
		parsed, parseErr := desktopParseExpect(one)
		if parseErr != nil {
			return "", fmt.Errorf("verify_state expectation %d: %w", i+1, parseErr)
		}
		expect = append(expect, parsed)
	}
	samples := desktopIntArg(args, "stable_samples")
	if samples <= 0 {
		samples = 1
	}
	timeout := desktopSecondsArg(args, "timeout", 2*time.Second, 30*time.Second)
	callCtx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	verdict, err := d.Verify(callCtx, pid, windowID, expect, samples, int(timeout/time.Millisecond))
	if err != nil {
		return "", fmt.Errorf("verify_state: %w", err)
	}
	setDesktopActivity(scope.RunID, "正在核对界面")
	return cua.VerifyText(verdict), nil
}

// operateDesktopWaitUntil runs action=wait_until.
//
// Arguments:
//
//	title       part of the window title to wait for (or to see leave)
//	app         part of the app or process name instead of a title
//	gone        wait for the window to disappear instead of to appear
//	on_screen   only count windows that are not minimized (default true)
//	timeout     how long to wait, e.g. "20s" (default 20s, max 120s)
func operateDesktopWaitUntil(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	spec := cua.WaitSpec{
		TitleContains: desktopStringArg(args, "title", "name", "window"),
		AppContains:   desktopStringArg(args, "app"),
		Gone:          desktopBoolArg(args, "gone"),
		OnScreenOnly:  true,
	}
	if v, ok := args["on_screen"].(bool); ok {
		spec.OnScreenOnly = v
	}
	spec.Timeout = desktopSecondsArg(args, "timeout", 20*time.Second, 120*time.Second)
	d, err := desktopCuaDriver(ctx, "wait_until")
	if err != nil {
		return "", err
	}
	defer d.Close()
	callCtx, cancel := context.WithTimeout(ctx, spec.Timeout+15*time.Second)
	defer cancel()
	setDesktopActivity(scope.RunID, "正在等待窗口变化")
	res, err := d.WaitUntil(callCtx, spec)
	if err != nil {
		return "", fmt.Errorf("wait_until: %w", err)
	}
	return cua.WaitText(spec, res), nil
}

// desktopParseExpect turns one expectation object into a cua.Expect.
func desktopParseExpect(item map[string]any) (cua.Expect, error) {
	if w, ok := item["window"].(map[string]any); ok {
		we := &cua.WindowExpect{}
		if v, ok := w["exists"].(bool); ok {
			we.Exists = &v
		}
		if b, ok := w["bounds"].(map[string]any); ok {
			we.Bounds = &cua.Frame{
				X: desktopIntArg(b, "x"),
				Y: desktopIntArg(b, "y"),
				W: desktopIntArg(b, "width", "w"),
				H: desktopIntArg(b, "height", "h"),
			}
			we.TolerancePx = desktopIntArg(w, "tolerance_px", "tolerance")
		}
		if we.Exists == nil && we.Bounds == nil {
			return cua.Expect{}, fmt.Errorf("a window check needs exists or bounds")
		}
		return cua.Expect{Window: we}, nil
	}
	e, ok := item["element"].(map[string]any)
	if !ok {
		return cua.Expect{}, fmt.Errorf("each expectation is a window or an element check")
	}
	sel := cua.Selector{}
	if rawSel, ok := e["selector"].(map[string]any); ok {
		sel.Role = desktopStringArg(rawSel, "role")
		sel.Label = desktopStringArg(rawSel, "label_contains", "label", "name", "text")
	}
	ee := &cua.ElementExpect{Selector: sel, ValueEquals: desktopStringArg(e, "value_equals", "value")}
	if v, ok := e["enabled"].(bool); ok {
		ee.Enabled = &v
	}
	if v, ok := e["selected"].(bool); ok {
		ee.Selected = &v
	}
	if v, ok := e["exists"].(bool); ok {
		ee.Exists = &v
	}
	if sel.Role == "" && sel.Label == "" {
		return cua.Expect{}, fmt.Errorf("an element check needs selector.role or selector.label_contains")
	}
	if ee.Exists != nil && !*ee.Exists {
		return cua.Expect{}, fmt.Errorf("element absence cannot be checked; assert {\"window\":{\"exists\":false}} instead")
	}
	return cua.Expect{Element: ee}, nil
}

// desktopIntArg reads an integer argument, accepting JSON numbers (float64
// or json.Number) and numeric strings.
func desktopIntArg(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		case string:
			if n, ok := parseIntStrict(v); ok {
				return n
			}
		}
	}
	return 0
}

func parseIntStrict(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// desktopStringArg reads the first non-empty string argument.
func desktopStringArg(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func desktopBoolArg(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

// desktopSecondsArg reads a duration argument in seconds, as a JSON number
// or a Go duration string, clamped to (0, max].
func desktopSecondsArg(m map[string]any, key string, def, max time.Duration) time.Duration {
	switch v := m[key].(type) {
	case float64:
		if d := time.Duration(v * float64(time.Second)); d > 0 {
			if d > max {
				return max
			}
			return d
		}
	case json.Number:
		if f, err := v.Float64(); err == nil {
			if d := time.Duration(f * float64(time.Second)); d > 0 {
				if d > max {
					return max
				}
				return d
			}
		}
	case string:
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			if d > max {
				return max
			}
			return d
		}
	}
	return def
}
