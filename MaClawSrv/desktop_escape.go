package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// No-deadlock invariant for the desktop guards (run_854d0758, 2026-11-10).
// Every guard is right on its own; together they locked a run: the publish
// method was blocked after two CDP timeouts, the guard's advice was "take a
// screenshot or call request_takeover", the compose phase refused the
// screenshot and takeover_for_publish refused the takeover. So: once a run is
// impaired (a method is blocked, a recovery goal waits for a diagnosis, is
// blocked, exhausted or stopped, or the last executed step failed),
// observing (screenshot, probe, reading files) and asking the person
// (request_takeover) are always permitted, in every phase, and every refusal
// carries the facts for an honest final answer. Mutating steps stay guarded.

// desktopObserveActions do not change the page, the files or the site.
var desktopObserveActions = map[string]bool{"screenshot": true, "probe": true, "file_read": true, "file_list": true, "skill_list": true, "check_inbox": true, "wait": true}

// desktopEscapeAction: observation, or handing the step to the person.
func desktopEscapeAction(action string) bool {
	return desktopObserveActions[action] || action == "request_takeover"
}

// desktopGuardRefusalLimit consecutive refusals of an impaired run turn every
// further refusal into a stop-and-report instruction.
const desktopGuardRefusalLimit = 3

// desktopRunImpaired reports whether the run is stuck or recovering, with
// the reason.
func desktopRunImpaired(scope agentruntime.Scope) (bool, string) {
	if st := desktopRunFor(scope); st != nil {
		st.mu.Lock()
		blocked := make([]string, 0, len(st.blocked))
		for intent, class := range st.blocked {
			blocked = append(blocked, intent+" ("+class+")")
		}
		lastFailed := st.lastFailed
		st.mu.Unlock()
		if len(blocked) > 0 {
			sort.Strings(blocked)
			return true, "blocked method: " + strings.Join(blocked, ", ")
		}
		if lastFailed {
			return true, "the last step failed"
		}
	}
	for _, g := range desktopRecRunGoalStates(scope.RunID) {
		switch g.State {
		case recovery.StateDiagnose, recovery.StateTrial, recovery.StateBlocked, recovery.StateExhausted, recovery.StateStopped:
			return true, "goal " + desktopRecGoalLabel(g.Key) + " is " + g.State
		}
	}
	return false, ""
}

// desktopRecRunGoalStates: the recovery goals this run touched.
func desktopRecRunGoalStates(runID string) []*recovery.Goal {
	if runID == "" {
		return nil
	}
	eng := desktopRecovery()
	var out []*recovery.Goal
	for _, k := range desktopRecRunGoals(runID) {
		sk, goal, _ := strings.Cut(k, "\x01")
		if g := eng.Status(sk, goal); g != nil {
			out = append(out, g)
		}
	}
	return out
}

// desktopRecGoalLabel: "publish zhihu" / "check report" for a goal key.
func desktopRecGoalLabel(key string) string {
	parts := strings.Split(key, "|")
	if len(parts) >= 3 {
		return parts[0] + " " + parts[2]
	}
	return key
}

// desktopNoteStepResult records how an executed step ended.
func desktopNoteStepResult(scope agentruntime.Scope, err error) {
	if st := desktopRunFor(scope); st != nil {
		st.mu.Lock()
		st.lastFailed = err != nil
		st.refusals = 0
		st.mu.Unlock()
	}
}

// desktopGuardRefused counts a refusal and, for an impaired run, adds what
// is still permitted and the facts the final answer must carry.
func desktopGuardRefused(scope agentruntime.Scope, err error) error {
	if err == nil {
		return nil
	}
	n := 0
	if st := desktopRunFor(scope); st != nil {
		st.mu.Lock()
		st.refusals++
		n = st.refusals
		st.mu.Unlock()
	}
	impaired, why := desktopRunImpaired(scope)
	if !impaired {
		return err
	}
	facts := desktopHonestFacts(scope)
	if n >= desktopGuardRefusalLimit {
		return fmt.Errorf("%w\n\nSTOP_AND_REPORT: %d steps in a row were refused (%s). Do not try more variants. Give the final answer now and state per part exactly what is done (with links) and what is not, with the evidence: %s", err, n, why, facts)
	}
	return fmt.Errorf("%w\n\nStill permitted (%s): screenshot or probe to observe, request_takeover to hand this step to the person, or the final answer. Facts for the final answer: %s", err, why, facts)
}

// desktopHonestFacts: per requested part, what the run verifiably reached.
func desktopHonestFacts(scope agentruntime.Scope) string {
	var parts []string
	seen := map[string]bool{}
	for _, e := range desktopLedgerEntries(desktopLedgerKey(scope)) {
		if e.Kind != "posted" || e.Run != scope.RunID {
			continue
		}
		site := siteExpKey("https://" + e.Host)
		if site == "" {
			site = e.Host
		}
		if e.Detail == "submitted_unconfirmed" {
			parts = append(parts, site+": submitted, NOT confirmed (do not submit again)")
		} else {
			parts = append(parts, fmt.Sprintf("%s: published %s 《%s》", site, e.URL, e.Title))
		}
		seen[site] = true
	}
	for _, g := range desktopRecRunGoalStates(scope.RunID) {
		label := desktopRecGoalLabel(g.Key)
		site := label
		if i := strings.Index(label, " "); i > 0 {
			site = label[i+1:]
		}
		if seen[site] || g.State == recovery.StateDone {
			continue
		}
		seen[site] = true
		why := g.Reason
		if why == "" {
			why = g.LastKind
		}
		parts = append(parts, fmt.Sprintf("%s: NOT done (%s%s)", site, g.State, desktopClipWhy(why)))
	}
	for _, p := range desktopTaskSitesPending(scope.RunID) {
		if !seen[p.ID] {
			parts = append(parts, p.ID+": not attempted yet")
		}
	}
	if len(parts) == 0 {
		return "nothing was published or verified in this run"
	}
	return strings.Join(parts, "; ")
}

func desktopClipWhy(why string) string {
	why = strings.TrimSpace(why)
	if why == "" {
		return ""
	}
	return ": " + desktopRuneHead(why, 140)
}

// desktopRunSubmittedUnconfirmed: a submit of this run went out without
// proof of the post (record or ledger); it must not be repeated by anyone.
func desktopRunSubmittedUnconfirmed(scope agentruntime.Scope) bool {
	for _, r := range desktopPublishRunRecords(scope.RunID) {
		if r.State == browser.PublishSubmitted {
			return true
		}
	}
	for _, e := range desktopLedgerEntries(desktopLedgerKey(scope)) {
		if e.Kind == "posted" && e.Run == scope.RunID && e.Detail == "submitted_unconfirmed" {
			return true
		}
	}
	for _, g := range desktopRecRunGoalStates(scope.RunID) {
		if g.State == recovery.StateStopped {
			return true
		}
	}
	return false
}
