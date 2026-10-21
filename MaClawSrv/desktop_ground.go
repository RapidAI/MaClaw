package main

// Planner/grounder split for desktop computer steps.
//
// A click step may name its target in words instead of pixels:
// {"action":"left_click","target":"the blue 发送 button"}. When a grounding
// model is configured (MACLAW_DESKTOP_GROUNDER_URL / _MODEL / _KEY /
// _PROTOCOL, e.g. UI-TARS or Qwen3-VL behind an OpenAI-compatible endpoint),
// the step's screenshot is sent to it and its answer becomes the step's x,y
// in that screenshot's pixels, the same space the planner's own coordinates
// use. Without a grounder, or when it cannot find the target, the step fails
// with a sentence telling the planner to give x,y or an element id itself:
// the single-model behaviour before this change.

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/grounding"
)

var desktopGroundHTTP = &http.Client{Timeout: grounding.CallTimeout}

// desktopGrounderConfig is the optional grounding model. Tests replace it.
var desktopGrounderConfig = func() (corelib.MaclawLLMConfig, bool) {
	cfg := corelib.MaclawLLMConfig{
		URL:      strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_GROUNDER_URL")),
		Key:      strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_GROUNDER_KEY")),
		Model:    strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_GROUNDER_MODEL")),
		Protocol: strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_GROUNDER_PROTOCOL")),
	}
	cfg.SupportsVision = true
	return cfg, grounding.Configured(cfg)
}

// desktopGroundShot captures the image a target is grounded on. Tests
// replace it.
var desktopGroundShot = func(ctx context.Context, scope agentruntime.Scope, display string, opts desktop.ShotOptions) (desktop.Shot, error) {
	opts.Region = desktop.ShotRegion{}
	opts.Marks = false
	switch {
	case desktopRemoteShot != nil:
		return desktopRemoteShot(ctx, scope.TenantID, scope.UserID, display, opts)
	case desktopHubClientFromEnv() != nil:
		shot, _, err := desktopHubClientFromEnv().screenshotWith(ctx, scope.TenantID, scope.UserID, display, "", opts)
		return shot, err
	default:
		if err := refuseLocalDesktop(ctx); err != nil {
			return desktop.Shot{}, err
		}
		return desktopShotLocal(display, opts)
	}
}

var desktopClickKinds = map[string]bool{
	"click": true, "left_click": true, "right_click": true, "middle_click": true,
	"double_click": true, "triple_click": true, "mouse_move": true, "move": true,
}

func desktopStepNeedsGround(m map[string]any) (string, bool) {
	target, _ := m["target"].(string)
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	kind, _ := m["action"].(string)
	if kind == "" {
		kind, _ = m["type"].(string)
	}
	if !desktopClickKinds[strings.ToLower(strings.TrimSpace(kind))] {
		return "", false
	}
	for _, key := range []string{"x", "y", "coordinate", "id"} {
		if v, ok := m[key]; ok && v != nil {
			return "", false
		}
	}
	return target, true
}

// groundDesktopSteps fills x,y for click steps that only name a target.
// It returns the steps to run and one note per grounded step.
func groundDesktopSteps(ctx context.Context, scope agentruntime.Scope, display string, raw any) (any, []string, error) {
	list, ok := raw.([]any)
	if !ok {
		return raw, nil, nil
	}
	var need []int
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, yes := desktopStepNeedsGround(m); yes {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return raw, nil, nil
	}
	cfg, ok := desktopGrounderConfig()
	if !ok {
		return raw, nil, fmt.Errorf("step %d names a target but no grounding model is configured; give x,y from the last screenshot (or an id from app_probe) instead of target", need[0]+1)
	}
	if len(need) > 1 || need[0] != 0 {
		// The screen changes after each step, so only a leading target can be
		// grounded on the screenshot taken now.
		return raw, nil, fmt.Errorf("only the first step of a batch may use target (the screen changes after each step); send the targeted click as its own batch, or give x,y")
	}
	frame := desktopFrameFor(scope)
	_, opts := frame.get()
	shot, err := desktopGroundShot(ctx, scope, display, opts)
	if err != nil || len(shot.Data) == 0 {
		return raw, nil, fmt.Errorf("could not take the screenshot to ground the target: %v; give x,y instead", err)
	}
	if shot.Scale <= 0 {
		shot.Scale = 1
	}
	frame.set(shot, opts)
	m := list[0].(map[string]any)
	target, _ := desktopStepNeedsGround(m)
	mime := shot.MIME
	if mime == "" {
		mime = "image/png"
	}
	started := time.Now()
	res, gerr := grounding.Ground(ctx, cfg, desktopGroundHTTP, base64.StdEncoding.EncodeToString(shot.Data), mime, shot.ImageWidth, shot.ImageHeight, target, nil)
	log.Printf("[desktop-ground] model=%s target_len=%d took=%s ok=%v", cfg.Model, len(target), time.Since(started).Round(10*time.Millisecond), gerr == nil)
	if gerr != nil {
		return raw, nil, fmt.Errorf("the grounding model could not place %q (%v); look at a screenshot and give x,y, or use app_probe ids", target, gerr)
	}
	if res.Ref != "" {
		return raw, nil, fmt.Errorf("the grounding model answered element %s, not a point; give x,y for %q", res.Ref, target)
	}
	out := make([]any, len(list))
	copy(out, list)
	step := map[string]any{}
	for k, v := range m {
		step[k] = v
	}
	delete(step, "target")
	step["x"], step["y"] = res.X, res.Y
	out[0] = step
	note := fmt.Sprintf("target %q grounded at (%d,%d) in a %dx%d screenshot by %s", target, res.X, res.Y, shot.ImageWidth, shot.ImageHeight, cfg.Model)
	return out, []string{note}, nil
}
