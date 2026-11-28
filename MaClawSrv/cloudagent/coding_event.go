package cloudagent

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingagent"
)

// Cloud coding turns report tool and phase activity with the same
// "Coding Agent Event: <json>" line the desktop coding agent emits, so the
// viewer folds cloud output into the identical activity feed — and does not
// also fold a styled "工具 ·" status line into the bubble's tool timeline,
// which is what used to paint every call twice.
const cloudCodingEventPrefix = "Coding Agent Event:"

// cloudCodingEvent builds one envelope. task_id stays empty: the timeline
// dedup keys on phase+task+title when a task id is present, which would drop
// consecutive tool rows of one step — the text form keeps every row distinct.
func cloudCodingEvent(event, phase string) map[string]any {
	return map[string]any{
		"version": 1,
		"agent":   "coding",
		"event":   event,
		"phase":   phase,
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (rt *Runtime) emitCodingEvent(inst *instance, taskID, requestID string, event map[string]any) {
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	rt.publish(inst, taskID, requestID, "progress", cloudCodingEventPrefix+" "+string(raw), nil)
}

func (rt *Runtime) emitCodingTaskStatus(inst *instance, taskID, requestID, phase, title string) {
	event := cloudCodingEvent("task_status", phase)
	if title = strings.TrimSpace(title); title != "" {
		event["title"] = collapseToolDetail(title)
	}
	rt.emitCodingEvent(inst, taskID, requestID, event)
}

// cloudToolEventArgs reads the operands the feed shows beside a tool row —
// the same fields the desktop fills for its tools (command for bash, files
// for file tools, target for the rest).
func cloudToolEventArgs(event map[string]any, name, argsJSON string) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch strings.TrimSpace(name) {
	case "bash":
		if cmd := joinArgs(args, "argv"); cmd != "" {
			event["command"] = collapseToolDetail(cmd)
		}
	case "read_file", "write_file":
		if path := stringArg(args, "path"); path != "" {
			event["files"] = []string{path}
		}
	case "update_plan_step":
		if idx := intArg(args, "index"); idx > 0 {
			event["target"] = "T" + strconv.Itoa(idx)
		}
	case "git_commit":
		if msg := stringArg(args, "message"); msg != "" {
			event["target"] = collapseToolDetail(msg)
		}
	case "web_search":
		if query := stringArg(args, "query"); query != "" {
			event["command"] = collapseToolDetail(query)
		}
	case "web_fetch":
		if rawURL := stringArg(args, "url"); rawURL != "" {
			event["target"] = collapseToolDetail(rawURL)
		}
	case "generate_pdf":
		if title := stringArg(args, "title"); title != "" {
			event["target"] = collapseToolDetail(title)
		}
	}
}

func (rt *Runtime) emitToolStarted(inst *instance, taskID, requestID, name, argsJSON string) {
	event := cloudCodingEvent("tool_started", "running")
	event["detail"] = strings.TrimSpace(name)
	cloudToolEventArgs(event, name, argsJSON)
	rt.emitCodingEvent(inst, taskID, requestID, event)
}

// emitToolFinished reports the call's close. failed carries the host's
// own verdict — a tool result string alone cannot tell an error body from
// output that merely mentions one. A non-empty write carries the write_file
// call's canonical path and exact line counts, the same operand the
// desktop's Edit cards show.
func (rt *Runtime) emitToolFinished(inst *instance, taskID, requestID, name, argsJSON, result string, failed bool, duration time.Duration, write hostWriteEvent) {
	event := cloudCodingEvent("tool_finished", "running")
	event["detail"] = strings.TrimSpace(name)
	cloudToolEventArgs(event, name, argsJSON)
	outcome := "success"
	if failed {
		outcome = "failed"
		event["summary"] = collapseToolDetail(firstLineOf(result))
	}
	event["outcome"] = outcome
	event["duration_ms"] = duration.Milliseconds()
	if write.path != "" {
		event["files"] = []string{write.path}
		if write.created {
			event["created"] = true
		}
		if write.hasCounts {
			event["added"] = write.added
			event["removed"] = write.removed
		}
	}
	rt.emitCodingEvent(inst, taskID, requestID, event)
}

// emitCodingDiffSummary reports the turn's workspace change set — the same
// summary row the desktop coding trail carries, so the viewer shows what
// changed without opening the final answer.
func (rt *Runtime) emitCodingDiffSummary(inst *instance, taskID, requestID string, changes []codingagent.FinalAnswerFileChangeFact) {
	if len(changes) == 0 {
		return
	}
	event := cloudCodingEvent("diff_summary", "result")
	files := make([]string, 0, len(changes))
	fileChanges := make([]map[string]any, 0, len(changes))
	added, removed := 0, 0
	for _, change := range changes {
		files = append(files, change.Path)
		row := map[string]any{
			"path":    change.Path,
			"added":   change.Added,
			"removed": change.Removed,
		}
		if change.Created {
			row["created"] = true
		}
		if change.Deleted {
			row["deleted"] = true
		}
		fileChanges = append(fileChanges, row)
		added += change.Added
		removed += change.Removed
	}
	event["files"] = files
	event["file_changes"] = fileChanges
	event["added"] = added
	event["removed"] = removed
	rt.emitCodingEvent(inst, taskID, requestID, event)
}

func firstLineOf(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func intArg(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}
