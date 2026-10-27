package main

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
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
		"read pages as text (probe/script/fetch with bash), prepare the title, the full body with [[img:N]] anchors and the picture files under /tmp, then call action=publish; it opens the site's editor itself"},
	phaseCompose: {allow("bash", "file_read", "file_write", "file_edit", "file_list", "navigate", "probe", "request_takeover", "publish", "app_open"),
		"the editor is open: put everything in with action=publish (title=, body=, files=, verify_url=); it uploads, fills, submits and reads the post back"},
	phaseSubmitted:  {allow(), "the submit was clicked but publish found no sign of the post, not even in the post list: the run is over; answer that it was submitted but not confirmed, do not publish again"},
	phaseLive:       {allow(), "the post is published and verified: give the final answer now with the link"},
	phaseIncomplete: {allow(), "the post is live but incomplete: give the final answer now with the link and say exactly what is missing"},
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
	pl := desktopPublishPlan[phase]
	if promptparts.Enabled() && len(pl.allowed) == 0 {
		// A finished phase: facts only, the bot words the final answer.
		return fmt.Errorf("refused: publish_plan(%s) — %s", phase, promptparts.Fact("REFUSED", "reason", "publish_plan", "phase", string(phase), "action", action, "next_allowed", "final_answer"))
	}
	return fmt.Errorf("refused: publish_plan(%s) — %s is not part of this phase. Next: %s", phase, action, pl.next)
}
