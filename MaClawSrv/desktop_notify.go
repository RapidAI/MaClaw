package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// action=notify_main lets a desktop bot tell the account's main bot (or a
// peer bot) something without ending its run: the task is done, it is
// blocked, a milestone, a question, or a file it produced. Hub stores it as
// a report note; the main bot's GUI picks it up and wakes the main bot.
// It is a message, not control, and never touches the desktop.

const (
	desktopNotifyPerRun  = 8
	desktopNotifyTextMax = 1500
)

var desktopNotifyKinds = map[string]bool{"done": true, "blocked": true, "progress": true, "question": true, "artifact": true, "info": true, "reply": true}

// desktopRemoteNotify is set by tests; production posts to Hub.
var desktopRemoteNotify func(ctx context.Context, body map[string]any) (map[string]any, error)

var desktopNotifyCount sync.Map // run key -> *int (bounded by desktopNotifyPerRun)
var desktopNotifyMu sync.Mutex

func (c *desktopHubClient) Notify(ctx context.Context, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if err := c.post(ctx, "/api/v1/desktop-services/notify", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func desktopNotifyMain(ctx context.Context, request agentruntime.TurnRequest, args map[string]any) (string, error) {
	text := strings.TrimSpace(desktopArg(args, "message"))
	if text == "" {
		text = strings.TrimSpace(desktopArg(args, "text"))
	}
	if r := []rune(text); len(r) > desktopNotifyTextMax {
		text = string(r[:desktopNotifyTextMax])
	}
	kind := strings.ToLower(strings.TrimSpace(desktopArg(args, "kind")))
	if !desktopNotifyKinds[kind] {
		kind = "info"
	}
	var artifacts []string
	if p := strings.TrimSpace(desktopArg(args, "path")); p != "" {
		artifacts = append(artifacts, p)
	}
	if list, ok := args["paths"].([]any); ok {
		for _, item := range list {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && len(artifacts) < 10 {
				artifacts = append(artifacts, s)
			}
		}
	}
	if text == "" && len(artifacts) == 0 {
		return "", fmt.Errorf("notify_main needs message (and kind: done, blocked, progress, question, artifact)")
	}
	inst, ok := turnInstance(request)
	if !ok {
		return "", fmt.Errorf("notify_main: this run has no bot instance")
	}
	meta := inst.Metadata
	user := strings.TrimSpace(meta["hub_user_id"])
	if user == "" {
		return "", fmt.Errorf("notify_main: this bot is not a Hub bot, there is no main bot to tell")
	}
	runKey := strings.TrimSpace(request.Scope.RunID)
	if runKey == "" {
		runKey = strings.TrimSpace(request.Scope.SessionID)
	}
	runKey = inst.ID + "|" + runKey
	desktopNotifyMu.Lock()
	v, _ := desktopNotifyCount.LoadOrStore(runKey, new(int))
	n := v.(*int)
	if *n >= desktopNotifyPerRun {
		desktopNotifyMu.Unlock()
		return "", fmt.Errorf("notify_main: already sent %d messages in this run; finish the task and put the rest in the final reply", desktopNotifyPerRun)
	}
	*n++
	desktopNotifyMu.Unlock()
	body := map[string]any{
		"tenant_id":   strings.TrimSpace(meta["hub_tenant_id"]),
		"user_id":     user,
		"instance_id": inst.ID,
		"run_id":      strings.TrimSpace(request.Scope.RunID),
		"target":      strings.TrimSpace(desktopArg(args, "target")),
		"kind":        kind,
		"text":        text,
		"artifacts":   artifacts,
		"reply_to":    strings.TrimSpace(desktopArg(args, "reply_to")),
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out map[string]any
	var err error
	if desktopRemoteNotify != nil {
		out, err = desktopRemoteNotify(cctx, body)
	} else if client := desktopHubClientFromEnv(); client != nil {
		out, err = client.Notify(cctx, body)
	} else {
		err = fmt.Errorf("Hub is not configured on this server")
	}
	if err != nil {
		desktopNotifyMu.Lock()
		*n--
		desktopNotifyMu.Unlock()
		return "", fmt.Errorf("notify_main failed: %v", err)
	}
	if dup, _ := out["duplicate"].(bool); dup {
		return "Already sent the same message; do not send it again. Continue the task.", nil
	}
	return "Sent to the main bot. It sees it now, or right after its current turn. Do not wait for an answer here; continue the task (an answer arrives as a new message).", nil
}
