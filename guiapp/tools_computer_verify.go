package guiapp

// computer_verify and computer_wait_until: deterministic checks and event
// waits served by the Cua Driver, shared with the bot side through
// corelib/cua.
//
// They are additive. When no driver socket is configured they answer with a
// single line that names the missing setting, and the existing paths
// (observe, wait) stay in charge, so a host without the driver behaves
// exactly as before.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/cua"
)

// cuVerifyDescription is shown to the model in the tool catalog.
const cuVerifyDescription = "Assert something about a desktop window without spending a model turn: " +
	"a window exists or is gone, its frame is where it should be, or a named control is enabled, " +
	"selected, or holds a value. Uses the Cua Driver; needs MACLAW_CUA_SOCKET."

// cuWaitUntilDescription is shown to the model in the tool catalog.
const cuWaitUntilDescription = "Block until a desktop window appears or disappears, polling inside the " +
	"driver, so a wait costs one call and no model turns. Uses the Cua Driver; needs MACLAW_CUA_SOCKET."

// cuDriver opens this host's Cua Driver, or explains what is missing.
func cuDriver(tool string) (*cua.Driver, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := cua.OpenDefault(ctx)
	if err == cua.ErrNoDriver {
		return nil, fmt.Errorf("%s needs a Cua Driver on this machine (set %s); without one, read the screen with computer_observe and act on what it shows", tool, cua.EnvSocket)
	}
	if err != nil {
		return nil, fmt.Errorf("%s could not reach the Cua Driver: %w", tool, err)
	}
	return d, nil
}

// cuHandleVerify serves computer_verify.
func cuHandleVerify(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_verify"); msg != "" {
		return msg
	}
	pid := cuIntArg(args, "pid")
	windowID := cuIntArg(args, "window_id", "window")
	if pid == 0 || windowID == 0 {
		return "computer_verify: give pid and window_id (from a window list), plus expect"
	}
	raw, ok := args["expect"].([]interface{})
	if !ok || len(raw) == 0 {
		return "computer_verify: give expect, a list such as [{\"window\":{\"exists\":true}}]"
	}
	expect := make([]cua.Expect, 0, len(raw))
	for i, item := range raw {
		m, _ := item.(map[string]interface{})
		one, err := cuExpect(m)
		if err != nil {
			return fmt.Sprintf("computer_verify: expectation %d: %v", i+1, err)
		}
		expect = append(expect, one)
	}
	d, err := cuDriver("computer_verify")
	if err != nil {
		return err.Error()
	}
	defer d.Close()
	timeout := cuSeconds(args, "timeout", 2*time.Second, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout+15*time.Second)
	defer cancel()
	verdict, err := d.Verify(ctx, pid, windowID, expect, max(1, cuIntArg(args, "stable_samples")), int(timeout/time.Millisecond))
	if err != nil {
		return fmt.Sprintf("computer_verify: %v", err)
	}
	return cua.VerifyText(verdict)
}

// cuHandleWaitUntil serves computer_wait_until.
func cuHandleWaitUntil(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_wait_until"); msg != "" {
		return msg
	}
	spec := cua.WaitSpec{
		TitleContains: cuStrArg(args, "title", "name", "window"),
		AppContains:   cuStrArg(args, "app"),
		Gone:          cuBoolArg(args, "gone"),
		OnScreenOnly:  true,
	}
	if v, ok := args["on_screen"].(bool); ok {
		spec.OnScreenOnly = v
	}
	spec.Timeout = cuSeconds(args, "timeout", 20*time.Second, 120*time.Second)
	d, err := cuDriver("computer_wait_until")
	if err != nil {
		return err.Error()
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), spec.Timeout+15*time.Second)
	defer cancel()
	res, err := d.WaitUntil(ctx, spec)
	if err != nil {
		return fmt.Sprintf("computer_wait_until: %v", err)
	}
	return cua.WaitText(spec, res)
}

// cuExpect turns one expectation object into a cua.Expect.
func cuExpect(item map[string]interface{}) (cua.Expect, error) {
	if w, ok := item["window"].(map[string]interface{}); ok {
		we := &cua.WindowExpect{}
		if v, ok := w["exists"].(bool); ok {
			we.Exists = &v
		}
		if b, ok := w["bounds"].(map[string]interface{}); ok {
			we.Bounds = &cua.Frame{
				X: cuIntArg(b, "x"), Y: cuIntArg(b, "y"),
				W: cuIntArg(b, "width", "w"), H: cuIntArg(b, "height", "h"),
			}
			we.TolerancePx = cuIntArg(w, "tolerance_px", "tolerance")
		}
		if we.Exists == nil && we.Bounds == nil {
			return cua.Expect{}, fmt.Errorf("a window check needs exists or bounds")
		}
		return cua.Expect{Window: we}, nil
	}
	e, ok := item["element"].(map[string]interface{})
	if !ok {
		return cua.Expect{}, fmt.Errorf("each expectation is a window or an element check")
	}
	sel := cua.Selector{}
	if s, ok := e["selector"].(map[string]interface{}); ok {
		sel.Role = cuStrArg(s, "role")
		sel.Label = cuStrArg(s, "label_contains", "label", "name", "text")
	}
	ee := &cua.ElementExpect{Selector: sel, ValueEquals: cuStrArg(e, "value_equals", "value")}
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
	return cua.Expect{Element: ee}, nil
}

func cuIntArg(m map[string]interface{}, keys ...string) int {
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
			n := 0
			ok := true
			for _, r := range strings.TrimSpace(v) {
				if r < '0' || r > '9' {
					ok = false
					break
				}
				n = n*10 + int(r-'0')
			}
			if ok {
				return n
			}
		}
	}
	return 0
}

func cuStrArg(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cuBoolArg(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

// cuSeconds reads a duration in seconds (number) or as a duration string.
func cuSeconds(m map[string]interface{}, key string, def, max time.Duration) time.Duration {
	switch v := m[key].(type) {
	case float64:
		if d := time.Duration(v * float64(time.Second)); d > 0 {
			return clampDur(d, max)
		}
	case json.Number:
		if f, err := v.Float64(); err == nil {
			if d := time.Duration(f * float64(time.Second)); d > 0 {
				return clampDur(d, max)
			}
		}
	case string:
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			return clampDur(d, max)
		}
	}
	return def
}

func clampDur(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	return d
}
