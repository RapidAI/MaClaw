package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// desktopAssignmentsOn: MACLAW_ASSIGNMENTS=off hides the assignment tools.
func desktopAssignmentsOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_ASSIGNMENTS"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

func desktopAssignmentToolHint() string {
	if !desktopAssignmentsOn() {
		return ""
	}
	return " action=report with state (progress, completed, blocked, failed, waiting_human), message, reply_to=<assignment id> is how a teammate reports; with expects_reply=true end the turn after it. action=list_assignments shows who is doing what. The main bot (or the named reviewer) checks a result and calls action=accept_result or action=return_result with assignment_id and note. If a site asks the person to sign in, report state=needs_login and stop; never type a password." + desktopSayHint()
}

// desktopRemoteAssign is set by tests; production posts to Hub.
var desktopRemoteAssign func(ctx context.Context, path string, body map[string]any) (map[string]any, error)

func desktopAssignmentAction(ctx context.Context, request agentruntime.TurnRequest, action string, args map[string]any) (string, error) {
	if !desktopAssignmentsOn() {
		return "", fmt.Errorf("%s is turned off on this server", action)
	}
	inst, ok := turnInstance(request)
	if !ok {
		return "", fmt.Errorf("%s: this run has no bot instance", action)
	}
	user := strings.TrimSpace(inst.Metadata["hub_user_id"])
	if user == "" {
		return "", fmt.Errorf("%s: this bot is not a Hub bot", action)
	}
	body := map[string]any{
		"tenant_id":   strings.TrimSpace(inst.Metadata["hub_tenant_id"]),
		"user_id":     user,
		"instance_id": inst.ID,
	}
	path := "/api/v1/desktop-services/assignments"
	if action != "list_assignments" {
		id := strings.TrimSpace(desktopArg(args, "assignment_id"))
		if id == "" {
			return "", fmt.Errorf("%s needs assignment_id (see action=list_assignments)", action)
		}
		path = "/api/v1/desktop-services/assignment-review"
		body["assignment_id"] = id
		body["verdict"] = map[string]string{"accept_result": "accepted", "return_result": "returned"}[action]
		body["note"] = strings.TrimSpace(firstNonEmptyString(desktopArg(args, "note"), desktopArg(args, "message")))
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out map[string]any
	var err error
	if desktopRemoteAssign != nil {
		out, err = desktopRemoteAssign(cctx, path, body)
	} else if client := desktopHubClientFromEnv(); client != nil {
		out = map[string]any{}
		err = client.post(cctx, path, body, &out)
	} else {
		err = fmt.Errorf("Hub is not configured on this server")
	}
	if err != nil {
		return "", fmt.Errorf("%s failed: %v", action, err)
	}
	if action == "list_assignments" {
		items, _ := out["items"].([]any)
		if len(items) == 0 {
			return "No assignments.", nil
		}
		var sb strings.Builder
		for i, it := range items {
			if i >= 20 {
				break
			}
			m, _ := it.(map[string]any)
			task := []rune(fmt.Sprint(m["task"]))
			if len(task) > 80 {
				task = task[:80]
			}
			fmt.Fprintf(&sb, "ASSIGNMENT id=%v to=%v state=%v", m["id"], m["target_bot"], m["state"])
			if q, _ := m["open_question"].(string); q != "" {
				fmt.Fprintf(&sb, " open_question=%s", q)
			}
			if r, _ := m["reviewer"].(string); r != "" {
				fmt.Fprintf(&sb, " reviewer=%s", r)
			}
			fmt.Fprintf(&sb, " task=%q\n", string(task))
		}
		return strings.TrimSpace(sb.String()), nil
	}
	raw, _ := json.Marshal(map[string]any{"id": out["id"], "state": out["state"]})
	return "REVIEWED " + string(raw), nil
}

func desktopSayOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_BOT_SAY"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

func desktopSayHint() string {
	if !desktopSayOn() {
		return ""
	}
	return " action=say with message (and room for a group room) speaks to the person directly, in your own voice as yourself (first person, like a colleague talking, not a relay or a system notice); say only what is new to them (Hub refuses a repeat of what another bot already said). One say per run: put the result in that one message. A later say in the same run is refused unless it carries update=true for information the person has not had yet."
}

func desktopSay(ctx context.Context, request agentruntime.TurnRequest, args map[string]any) (string, error) {
	if !desktopSayOn() {
		return "", fmt.Errorf("say is turned off on this server")
	}
	text := strings.TrimSpace(firstNonEmptyString(desktopArg(args, "message"), desktopArg(args, "text")))
	if text == "" {
		return "", fmt.Errorf("say needs message")
	}
	inst, ok := turnInstance(request)
	if !ok {
		return "", fmt.Errorf("say: this run has no bot instance")
	}
	user := strings.TrimSpace(inst.Metadata["hub_user_id"])
	if user == "" {
		return "", fmt.Errorf("say: this bot is not a Hub bot")
	}
	runKey := inst.ID + "|" + firstNonEmptyString(strings.TrimSpace(request.Scope.RunID), strings.TrimSpace(request.Scope.SessionID))
	room := strings.TrimSpace(desktopArg(args, "room"))
	// Hub only refuses near-identical text, so a reworded repeat of the same
	// result got through (seen live: one assignment, three says). The run is
	// the assignment's unit of work: it gets one say, plus one marked update.
	update := strings.EqualFold(strings.TrimSpace(desktopArg(args, "update")), "true")
	if !desktopSayRuns.allow(runKey+"|"+room, update, time.Now()) {
		return "SKIPPED already_said_this_run: you already told the person in this run. Do not say it again; only call say with update=true if there is information they have not had yet.", nil
	}
	body := map[string]any{
		"tenant_id":   strings.TrimSpace(inst.Metadata["hub_tenant_id"]),
		"user_id":     user,
		"instance_id": inst.ID,
		"room":        room,
		"text":        text,
		"key":         desktopMessageKey(runKey, "say", room, text),
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out map[string]any
	var err error
	if desktopRemoteAssign != nil {
		out, err = desktopRemoteAssign(cctx, "/api/v1/desktop-services/say", body)
	} else if client := desktopHubClientFromEnv(); client != nil {
		out = map[string]any{}
		err = client.post(cctx, "/api/v1/desktop-services/say", body, &out)
	} else {
		err = fmt.Errorf("Hub is not configured on this server")
	}
	if err != nil {
		if !strings.Contains(err.Error(), "ALREADY_SAID") && !strings.Contains(err.Error(), "409") {
			// Nothing reached the person; the run may try again.
			desktopSayRuns.undo(runKey+"|"+room, time.Now())
		}
		if strings.Contains(err.Error(), "ALREADY_SAID") || strings.Contains(err.Error(), "409") {
			return "SKIPPED already_said: another bot already told the person this. Stay silent on it.", nil
		}
		return "", fmt.Errorf("say failed: %v", err)
	}
	if dup, _ := out["duplicate"].(bool); dup {
		return "SKIPPED duplicate: you already said this.", nil
	}
	if list, ok := out["answered_by"].([]any); ok && len(list) > 0 {
		names := make([]string, 0, len(list))
		for _, x := range list {
			names = append(names, fmt.Sprint(x))
		}
		return "SAID already_answered_by=" + strings.Join(names, ",") + ": others had already replied in this room; do not add more unless it is new.", nil
	}
	return desktopSaidResult(text), nil
}

// desktopSaidResult echoes what reached the person. A bare "SAID" left the
// run's outcome check without evidence of the sentence (seen live: "the tool
// only shows SAID", so a finished say task kept running for many minutes).
func desktopSaidResult(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) > 600 {
		r = append(r[:600], '…')
	}
	return "SAID to the person: \u201c" + string(r) + "\u201d"
}

// desktopSayRunGate counts says per run and room.
type desktopSayRunGate struct {
	mu   sync.Mutex
	runs map[string]desktopSayRunCount
}

type desktopSayRunCount struct {
	n    int
	seen time.Time
}

const (
	desktopSayRunMax = 2 // first say, plus one update=true
	desktopSayRunTTL = 6 * time.Hour
)

var desktopSayRuns = &desktopSayRunGate{runs: map[string]desktopSayRunCount{}}

func (g *desktopSayRunGate) allow(key string, update bool, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, v := range g.runs {
		if now.Sub(v.seen) > desktopSayRunTTL {
			delete(g.runs, k)
		}
	}
	c := g.runs[key]
	if c.n >= 1 && (!update || c.n >= desktopSayRunMax) {
		return false
	}
	g.runs[key] = desktopSayRunCount{n: c.n + 1, seen: now}
	return true
}

func (g *desktopSayRunGate) undo(key string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c, ok := g.runs[key]; ok && c.n > 0 {
		g.runs[key] = desktopSayRunCount{n: c.n - 1, seen: now}
	}
}
