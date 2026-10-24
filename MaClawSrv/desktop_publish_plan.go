package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// A publish task runs a fixed plan. Its phase follows from where the run is
// (the publish record, the editor page) and each phase allows a fixed set of
// actions; anything else is refused with the phase's named next step. This
// replaces the per-symptom guards (hand publish, prepublish detours,
// screenshots, after-verify checks) with one table.
type desktopPublishPhase string

const (
	phaseResearch   desktopPublishPhase = "research"   // pick and read the source, prepare text and pictures
	phaseCompose    desktopPublishPhase = "compose"    // editor open: only the publish action goes in
	phaseSubmitted  desktopPublishPhase = "submitted"  // submit went out, no read-back yet
	phaseLive       desktopPublishPhase = "live"       // verified: answer with the link
	phaseIncomplete desktopPublishPhase = "incomplete" // live but missing parts the run could not put back
)

func allow(a ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	return m
}

var desktopPublishPlan = map[desktopPublishPhase]struct {
	allowed map[string]bool
	next    string
}{
	phaseResearch: {allow("probe", "script", "navigate", "bash", "file_read", "file_write", "file_edit", "file_list", "skill_list", "skill_run", "wait", "request_takeover", "publish", "check_inbox", "batch", "app_open"),
		"read pages as text (probe/script/fetch with bash), prepare the title, the full body with [[img:N]] anchors and the picture files under /tmp, open the editor, then call action=publish"},
	phaseCompose: {allow("bash", "file_read", "file_write", "file_edit", "file_list", "navigate", "probe", "request_takeover", "publish", "app_open"),
		"the editor is open: put everything in with action=publish (title=, body=, files=, verify_url=); it uploads, fills, submits and reads the post back"},
	phaseSubmitted: {allow("navigate", "probe", "publish", "request_takeover"),
		"the submit went out but was not read back: open the account's post list (navigate/probe) to find the post; do not publish again"},
	phaseLive:       {allow("publish"), "the post is published and verified: give the final answer now with the link"},
	phaseIncomplete: {allow("publish"), "the post is live but incomplete: give the final answer now with the link and say exactly what is missing"},
}

// desktopPublishPhaseOf derives the phase of a publish task.
func desktopPublishPhaseOf(rec desktopPublishRecord, hasRec bool, url string) desktopPublishPhase {
	if hasRec {
		switch rec.State {
		case browser.PublishLive:
			return phaseLive
		case browser.PublishLiveIncomplete:
			return phaseIncomplete
		case browser.PublishSubmitted:
			return phaseSubmitted
		}
	}
	if desktopEditURLRe.MatchString(strings.SplitN(url, "?", 2)[0]) {
		return phaseCompose
	}
	return phaseResearch
}

// desktopPublishPlanCheck refuses an action the current phase does not allow.
// A batch is checked step by step; a click on the publish button by script
// counts as a hand publish in every phase.
func desktopPublishPlanCheck(phase desktopPublishPhase, action string, args map[string]any) error {
	pl := desktopPublishPlan[phase]
	if action == "batch" {
		if !pl.allowed["batch"] {
			return desktopPlanRefusal(phase, action)
		}
		if steps, ok := args["plan"].([]any); ok {
			for _, s := range steps {
				if m, ok := s.(map[string]any); ok {
					if err := desktopPublishPlanCheck(phase, desktopArg(m, "action"), m); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if !pl.allowed[action] {
		return desktopPlanRefusal(phase, action)
	}
	if action == "script" || action == "click" {
		t := desktopArgsText(args)
		if desktopPublishButtonRe.MatchString(strings.ReplaceAll(t, `\"`, `"`)) && strings.Contains(strings.ToLower(t), "click") {
			return desktopPlanRefusal(phase, "click publish by hand")
		}
	}
	return nil
}

func desktopPlanRefusal(phase desktopPublishPhase, action string) error {
	return fmt.Errorf("refused: publish_plan(%s) — %s is not part of this phase. Next: %s", phase, action, desktopPublishPlan[phase].next)
}
