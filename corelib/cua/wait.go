package cua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// # Waiting for an external event without spending model turns
//
//	run_script drives the driver itself; its cua.listWindows()/cua.sleep()
//	are enough for a wait-until loop, so a "is the window there yet" or "did
//	the download dialog appear" question costs zero model turns. The result
//	of the script is returned verbatim, so the caller reads the poll count and
//	the match out of it.
//
// The script is constant; only the arguments vary. Nothing from the task is
// interpolated into JavaScript text, so a window title can never become code.

// waitProgram is the wait-until loop the driver runs itself. Its argument
// object is a JSON literal at the top, so nothing from the task can become
// code; the loop polls listWindows and returns the poll count with the hit.
const waitProgram = `
const args = __ARGS__;
const deadline = Date.now() + args.timeoutMs;
let polls = 0;
const hit = (list) => {
  for (const w of list) {
    const title = String(w.title || "");
    const app = String(w.app_name || "");
    if (args.titleContains && title.toLowerCase().indexOf(args.titleContains.toLowerCase()) < 0) continue;
    if (args.appContains && app.toLowerCase().indexOf(args.appContains.toLowerCase()) < 0) continue;
    if (args.onScreenOnly && !w.is_on_screen) continue;
    return { title: title, app: app, pid: w.pid, window_id: w.window_id };
  }
  return null;
};
while (true) {
  const wins = await cua.listWindows();
  polls++;
  const list = Array.isArray(wins) ? wins : (wins.windows || []);
  const found = hit(list);
  const ok = args.gone ? !found : !!found;
  if (ok) return { satisfied: true, polls: polls, waitedMs: args.timeoutMs - Math.max(0, deadline - Date.now()), window: found };
  if (Date.now() >= deadline) break;
  await cua.sleep(args.intervalMs);
}
return { satisfied: false, polls: polls, waitedMs: args.timeoutMs };
`

// WaitSpec is what a caller waits for.
type WaitSpec struct {
	// TitleContains matches a window title, case-insensitively.
	TitleContains string
	// AppContains matches a process or app name.
	AppContains string
	// OnScreenOnly skips minimized and off-screen windows.
	OnScreenOnly bool
	// Gone inverts the test: satisfied when no window matches.
	Gone bool
	// Timeout bounds the wait; Interval is the poll step.
	Timeout  time.Duration
	Interval time.Duration
}

// WaitResult is one wait's outcome.
type WaitResult struct {
	Satisfied bool           `json:"satisfied"`
	Polls     int            `json:"polls"`
	WaitedMs  int64          `json:"waitedMs"`
	Window    map[string]any `json:"window"`
	Text      string         `json:"-"`
}

// WaitUntil polls the window list inside the driver until spec holds or its
// timeout passes. No model turn is spent while it waits.
func (d *Driver) WaitUntil(ctx context.Context, spec WaitSpec) (WaitResult, error) {
	if strings.TrimSpace(spec.TitleContains) == "" && strings.TrimSpace(spec.AppContains) == "" {
		return WaitResult{}, fmt.Errorf("wait_until needs a window title or an app name to wait for")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if timeout > 120*time.Second {
		return WaitResult{}, fmt.Errorf("wait_until waits at most 120s (asked %s)", timeout)
	}
	interval := spec.Interval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	scriptArgs, err := json.Marshal(map[string]any{
		"titleContains": spec.TitleContains,
		"appContains":   spec.AppContains,
		"onScreenOnly":  spec.OnScreenOnly,
		"gone":          spec.Gone,
		"timeoutMs":     int(timeout / time.Millisecond),
		"intervalMs":    int(interval / time.Millisecond),
	})
	if err != nil {
		return WaitResult{}, err
	}
	// The arguments are substituted as a JSON literal, never as text a
	// task could steer.
	program := strings.Replace(waitProgram, "__ARGS__", string(scriptArgs), 1)
	args := map[string]any{
		"script":     program,
		"timeout_ms": int(timeout/time.Millisecond) + 5000,
		"max_calls":  int(timeout/interval) + 20,
	}
	out, text, err := d.CallDetail(ctx, "run_script", args)
	if err != nil {
		return WaitResult{Text: text}, err
	}
	res := WaitResult{Text: text}
	// run_script reports the program's return value under "value".
	value, _ := out["value"].(map[string]any)
	if value == nil {
		value = out
	}
	if v, ok := value["satisfied"].(bool); ok {
		res.Satisfied = v
	}
	if v, ok := numberish(value["polls"]); ok {
		res.Polls = int(v)
	}
	if v, ok := numberish(value["waitedMs"]); ok {
		res.WaitedMs = int64(v)
	}
	if w, ok := value["window"].(map[string]any); ok {
		res.Window = w
	}
	return res, nil
}

// WaitText renders a wait for a model.
func WaitText(spec WaitSpec, res WaitResult) string {
	target := strings.TrimSpace(spec.TitleContains)
	if target == "" {
		target = "app " + strings.TrimSpace(spec.AppContains)
	}
	what := "appear"
	if spec.Gone {
		what = "go away"
	}
	if res.Satisfied {
		line := fmt.Sprintf("%s %s after %d poll(s) in %d ms", target, what, res.Polls, res.WaitedMs)
		if res.Window != nil {
			if title, ok := res.Window["title"].(string); ok && title != "" {
				line += fmt.Sprintf(" (%q, pid %v, window %v)", title, res.Window["pid"], res.Window["window_id"])
			}
		}
		return line
	}
	return fmt.Sprintf("%s did not %s within %d ms (%d polls); it may still be loading, or the name differs", target, what, res.WaitedMs, res.Polls)
}

func numberish(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
