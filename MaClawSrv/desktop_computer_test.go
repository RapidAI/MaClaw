package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

func parseOne(t *testing.T, m map[string]any) computeruse.Step {
	t.Helper()
	step, err := computeruse.ParseStep(m)
	if err != nil {
		t.Fatalf("%v: %v", m, err)
	}
	return step
}

func argvOf(t *testing.T, m map[string]any, frame desktop.Shot) string {
	t.Helper()
	argv, err := computeruse.StepArgv(parseOne(t, m), frame)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(argv, " ")
}

func TestComputerActionMappingAnthropic(t *testing.T) {
	id := desktop.Shot{Scale: 1}
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"action": "left_click", "coordinate": []any{10.0, 20.0}}, "mousemove --sync 10 20 click 1"},
		{map[string]any{"action": "right_click", "coordinate": []any{10.0, 20.0}}, "mousemove --sync 10 20 click 3"},
		{map[string]any{"action": "middle_click", "coordinate": []any{1.0, 2.0}}, "mousemove --sync 1 2 click 2"},
		{map[string]any{"action": "double_click", "coordinate": []any{5.0, 6.0}}, "mousemove --sync 5 6 click --repeat 2 --delay 80 1"},
		{map[string]any{"action": "triple_click", "coordinate": []any{5.0, 6.0}}, "mousemove --sync 5 6 click --repeat 3 --delay 80 1"},
		{map[string]any{"action": "left_click", "coordinate": []any{5.0, 6.0}, "text": "ctrl+shift"}, "mousemove --sync 5 6 keydown ctrl keydown shift click 1 keyup shift keyup ctrl"},
		{map[string]any{"action": "left_click"}, "click 1"},
		{map[string]any{"action": "mouse_move", "coordinate": []any{7.0, 8.0}}, "mousemove --sync 7 8"},
		{map[string]any{"action": "left_mouse_down"}, "mousedown 1"},
		{map[string]any{"action": "left_mouse_up", "coordinate": []any{3.0, 4.0}}, "mousemove --sync 3 4 mouseup 1"},
		{map[string]any{"action": "left_click_drag", "start_coordinate": []any{0.0, 0.0}, "coordinate": []any{100.0, 50.0}}, "mousemove --sync 0 0 mousedown 1 sleep 0.1 mousemove --sync 50 25 sleep 0.05 mousemove --sync 100 50 sleep 0.05 sleep 0.1 mouseup 1"},
		{map[string]any{"action": "scroll", "coordinate": []any{9.0, 9.0}, "scroll_direction": "up", "scroll_amount": 4.0, "text": "shift"}, "mousemove --sync 9 9 keydown shift click --repeat 4 --delay 30 4 keyup shift"},
		{map[string]any{"action": "key", "text": "ctrl+s"}, "key ctrl+s"},
		{map[string]any{"action": "key", "text": "Enter", "repeat": 3.0}, "key --repeat 3 --delay 50 Return"},
		{map[string]any{"action": "hold_key", "text": "shift", "duration": 0.5}, "keydown shift sleep 0.5 keyup shift"},
		{map[string]any{"action": "type", "text": "hi"}, "type --delay 20 hi"},
		{map[string]any{"action": "cursor_position"}, "getmouselocation --shell"},
		{map[string]any{"action": "wait", "duration": 2.0}, "sleep 2"},
		{map[string]any{"name": "computer", "input": map[string]any{"action": "left_click", "coordinate": []any{1.0, 1.0}}}, "mousemove --sync 1 1 click 1"},
	}
	for _, c := range cases {
		if got := argvOf(t, c.in, id); got != c.want {
			t.Errorf("%v\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	for _, kind := range []string{"screenshot", "zoom"} {
		m := map[string]any{"action": kind}
		if kind == "zoom" {
			m["region"] = []any{0.0, 0.0, 10.0, 10.0}
		}
		if argv, err := computeruse.StepArgv(parseOne(t, m), id); err != nil || argv != nil {
			t.Errorf("%s is not xdotool: %v %v", kind, argv, err)
		}
	}
	for _, bad := range []map[string]any{
		{"action": "hold_key", "text": "a"},
		{"action": "scroll", "scroll_direction": "sideways"},
		{"action": "wait", "duration": 120.0},
		{"action": "left_click", "coordinate": []any{-1.0, 3.0}},
		{"action": "key", "text": "ctrl+$(id)"},
		{"action": "zoom", "region": []any{10.0, 10.0, 5.0, 5.0}},
		{"action": "rm"},
		{"action": "left_click", "text": "hyper"},
	} {
		if _, err := computeruse.ParseStep(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestComputerActionMappingOpenAI(t *testing.T) {
	id := desktop.Shot{Scale: 1}
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"type": "click", "button": "right", "x": 3.0, "y": 4.0}, "mousemove --sync 3 4 click 3"},
		{map[string]any{"type": "click", "button": "wheel", "x": 3.0, "y": 4.0}, "mousemove --sync 3 4 click 2"},
		{map[string]any{"type": "double_click", "x": 3.0, "y": 4.0}, "mousemove --sync 3 4 click --repeat 2 --delay 80 1"},
		{map[string]any{"type": "scroll", "x": 5.0, "y": 5.0, "scroll_x": 0.0, "scroll_y": 350.0}, "mousemove --sync 5 5 click --repeat 4 --delay 30 5"},
		{map[string]any{"type": "scroll", "x": 5.0, "y": 5.0, "scroll_x": -120.0, "scroll_y": 0.0}, "mousemove --sync 5 5 click --repeat 2 --delay 30 6"},
		{map[string]any{"type": "keypress", "keys": []any{"CTRL", "SHIFT", "T"}}, "key ctrl+shift+t"},
		{map[string]any{"type": "keypress", "keys": []any{"ENTER"}}, "key Return"},
		{map[string]any{"type": "move", "x": 1.0, "y": 2.0}, "mousemove --sync 1 2"},
		{map[string]any{"type": "type", "text": "ok"}, "type --delay 20 ok"},
		{map[string]any{"type": "wait"}, "sleep 1"},
		{map[string]any{"type": "drag", "path": []any{map[string]any{"x": 1.0, "y": 1.0}, map[string]any{"x": 5.0, "y": 5.0}, map[string]any{"x": 9.0, "y": 1.0}}},
			"mousemove --sync 1 1 mousedown 1 sleep 0.1 mousemove --sync 5 5 sleep 0.05 mousemove --sync 9 1 sleep 0.05 sleep 0.1 mouseup 1"},
	}
	for _, c := range cases {
		if got := argvOf(t, c.in, id); got != c.want {
			t.Errorf("%v\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestComputerCoordinatesUseTheScaledFrame(t *testing.T) {
	frame := desktop.Shot{ScreenWidth: 1280, ScreenHeight: 800, ImageWidth: 1024, ImageHeight: 640, Region: desktop.ShotRegion{Width: 1280, Height: 800}, Scale: 0.8}
	if got := argvOf(t, map[string]any{"action": "left_click", "coordinate": []any{512.0, 320.0}}, frame); got != "mousemove --sync 640 400 click 1" {
		t.Fatalf("scaled click = %s", got)
	}
	if got := argvOf(t, map[string]any{"action": "click", "x": 2000.0, "y": 10.0}, frame); got != "mousemove --sync 1279 13 click 1" {
		t.Fatalf("clamped click = %s", got)
	}
	if got := computeruse.CursorText("X=640\nY=400\nSCREEN=0\nWINDOW=1\n", frame); !strings.Contains(got, "(512, 320) in screenshot pixels") {
		t.Fatalf("cursor = %s", got)
	}
	opts, _, err := computeruse.ShotOptionsFromArgs(map[string]any{"region": []any{0.0, 0.0, 512.0, 320.0}, "max_width": 1024.0}, frame, desktop.ShotOptions{})
	if err != nil || opts.Region != (desktop.ShotRegion{X: 0, Y: 0, Width: 640, Height: 400}) || opts.MaxWidth != 1024 {
		t.Fatalf("zoom region = %+v %v", opts, err)
	}
	req, _ := computeruse.CtlRequest("window", map[string]any{"do": "move_resize", "title": "Mousepad", "x": 80.0, "y": 40.0, "width": 400.0, "height": 320.0}, frame)
	if req["x"] != 100 || req["y"] != 50 || req["width"] != 500 || req["height"] != 400 {
		t.Fatalf("window request = %v", req)
	}
}

func TestComputerBatchStopsAtFirstFailureAndReportsEveryStep(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "computer-batch"}
	var calls []string
	var run func(argv ...string) (string, error)
	run = func(argv ...string) (string, error) {
		line := strings.Join(argv, " ")
		calls = append(calls, line)
		if strings.Contains(line, "click 3") {
			return "", fmt.Errorf("exit status 1")
		}
		if strings.HasPrefix(line, "getmouselocation") {
			return "X=5\nY=6\n", nil
		}
		return "", nil
	}
	env := desktopComputerEnv(context.Background(), scope, ":20", &desktopFrame{}, run, nil, nil)
	steps, err := computeruse.ParseSteps([]any{
		map[string]any{"action": "cursor_position"},
		map[string]any{"action": "left_click", "coordinate": []any{1.0, 1.0}},
		map[string]any{"action": "right_click", "coordinate": []any{2.0, 2.0}},
		map[string]any{"action": "type", "text": "never"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _, runErr := computeruse.Run(env, steps, true)
	if runErr == nil || len(results) != 3 || !results[0].OK || !results[1].OK || results[2].OK {
		t.Fatalf("results=%+v err=%v", results, runErr)
	}
	text := computeruse.ResultsText(steps, results, runErr)
	for _, want := range []string{"1. cursor_position: ok - cursor at (5, 6)", "2. click: ok", "3. click: failed", "4. type: not executed: an earlier computer action in this turn failed."} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	for _, call := range calls {
		if strings.Contains(call, "never") {
			t.Fatal("a step after the failure ran")
		}
	}
}

func TestElementStepsUseAccessibilityThenXdotool(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "element-steps"}
	var calls []string
	var requests []map[string]any
	env := desktopComputerEnv(context.Background(), scope, ":20", &desktopFrame{}, func(argv ...string) (string, error) {
		calls = append(calls, strings.Join(argv, " "))
		return "", nil
	}, nil, nil)
	env.Ctl = func(request map[string]any) (map[string]any, error) {
		requests = append(requests, request)
		if request["do"] == "set_text" {
			return map[string]any{"ok": true, "method": "atspi", "role": "text", "name": "", "center": []any{50.0, 60.0}}, nil
		}
		return map[string]any{"ok": true, "method": "xdotool", "role": "push button", "name": "OK", "center": []any{34.0, 70.0}}, nil
	}
	steps, err := computeruse.ParseSteps([]any{
		map[string]any{"action": "click", "id": 2.0},
		map[string]any{"action": "set_text", "id": 9.0, "text": "你好"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _, runErr := computeruse.Run(env, steps, true)
	if runErr != nil || len(results) != 2 || !results[0].OK || !results[1].OK {
		t.Fatalf("results=%+v err=%v", results, runErr)
	}
	if !strings.Contains(strings.Join(calls, "|"), "mousemove --sync 34 70 click 1") {
		t.Fatalf("click fallback calls=%v", calls)
	}
	if requests[0]["op"] != desktop.CtlA11yAct || requests[0]["id"] != 2 || requests[1]["text"] != "你好" {
		t.Fatalf("requests=%v", requests)
	}
	if strings.Contains(strings.Join(calls, "|"), "你好") {
		t.Fatal("set_text through AT-SPI also typed the text")
	}
}

func TestComputerActionsAcceptSingleAndBatchForms(t *testing.T) {
	single := desktopComputerActions(map[string]any{"computer_action": "left_click", "coordinate": []any{1.0, 2.0}})
	steps, err := computeruse.ParseSteps(single)
	if err != nil || len(steps) != 1 || steps[0].Kind != "click" || steps[0].X != 1 {
		t.Fatalf("single=%+v err=%v", steps, err)
	}
	if !desktopStepsInsertIntoFocus([]any{map[string]any{"action": "paste", "text": "x"}}) {
		t.Fatal("paste into the focus must be guarded")
	}
	if desktopStepsInsertIntoFocus([]any{map[string]any{"action": "left_click", "coordinate": []any{1.0, 1.0}}, map[string]any{"action": "type", "text": "x"}}) {
		t.Fatal("type after a click is checked at the click target")
	}
	if !desktopArgvInsertsText([]string{"key", "--repeat", "3", "--delay", "50", "a"}) || desktopArgvInsertsText([]string{"key", "--repeat", "3", "--delay", "50", "Return"}) {
		t.Fatal("key --repeat guard is wrong")
	}
}
