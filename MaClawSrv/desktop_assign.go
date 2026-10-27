package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	return " action=report with state (progress, completed, blocked, failed, waiting_human), message, reply_to=<assignment id> is how a teammate reports; with expects_reply=true end the turn after it. action=list_assignments shows who is doing what. The main bot (or the named reviewer) checks a result and calls action=accept_result or action=return_result with assignment_id and note."
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
