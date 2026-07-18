package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ACPToolEvent is a tool lifecycle signal for ACP Mode B (Cursor-like tool UI).
// The thin bridge only forwards session/update; all semantics live in the GUI brain.
type ACPToolEvent struct {
	Phase      string // "start" | "end"
	ToolCallID string
	Name       string
	ArgsJSON   string
	Result     string
	OK         bool
	// Kind is ACP tool kind: edit | read | search | execute | delete | move | other
	Kind string
	// Paths are workspace file locations when known (for clients that show locations).
	Paths []string
	// Title is a short human label for the tool chip.
	Title string
}

// acpToolSinkRegistry maps request_id → listener for in-flight ACP turns.
type acpToolSinkRegistry struct {
	mu    sync.Mutex
	sinks map[string]func(ACPToolEvent)
}

func (r *acpToolSinkRegistry) set(requestID string, fn func(ACPToolEvent)) (clear func()) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || fn == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.sinks == nil {
		r.sinks = make(map[string]func(ACPToolEvent))
	}
	r.sinks[requestID] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.sinks, requestID)
		r.mu.Unlock()
	}
}

func (r *acpToolSinkRegistry) emit(requestID string, ev ACPToolEvent) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	r.mu.Lock()
	fn := r.sinks[requestID]
	r.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// App-level registry (wired in acp host + agent loop).
var globalACPToolSinks acpToolSinkRegistry

func emitACPToolEventForRequest(requestID string, ev ACPToolEvent) {
	if !isACPProgrammingRequestID(requestID) {
		return
	}
	globalACPToolSinks.emit(requestID, ev)
}

func acpToolKind(name string) string {
	switch strings.TrimSpace(name) {
	case "write_file", "edit_file", "apply_patch", "str_replace", "create_file":
		return "edit"
	case "read_file", "read_files":
		return "read"
	case "ripgrep", "grep", "Glob", "glob", "list_directory", "list_dir", "search_files", "session_search", "web_search":
		return "search"
	case "bash", "run_terminal", "shell", "powershell":
		return "execute"
	case "delete_file", "remove_file":
		return "delete"
	case "move_file", "rename_file":
		return "move"
	default:
		return "other"
	}
}

func acpToolTitle(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	paths := acpPathsFromToolArgs(name, argsJSON)
	if len(paths) > 0 {
		base := filepath.Base(paths[0])
		if base != "" && base != "." && base != string(filepath.Separator) {
			return name + " · " + base
		}
	}
	if name == "" {
		return "tool"
	}
	return name
}

func acpPathsFromToolArgs(name, argsJSON string) []string {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" || argsJSON == "{}" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(argsJSON), &m) != nil {
		return nil
	}
	var keys []string
	switch strings.TrimSpace(name) {
	case "write_file", "edit_file", "read_file", "create_file", "delete_file", "remove_file":
		keys = []string{"path", "file", "file_path", "filepath", "target"}
	case "bash", "run_terminal", "shell", "powershell":
		// no path keys by default
		keys = []string{"cwd", "workdir", "working_directory"}
	case "list_directory", "list_dir":
		keys = []string{"path", "dir", "directory"}
	default:
		keys = []string{"path", "file", "file_path", "filepath", "target", "cwd"}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func newACPToolCallID(name string) string {
	return fmt.Sprintf("tc_%s_%d", sanitizeACPToolID(name), time.Now().UnixNano())
}

func sanitizeACPToolID(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		return "tool"
	}
	if len(s) > 32 {
		return s[:32]
	}
	return s
}

// acpToolEventToUpdate builds a session/update payload for formulahendry.acp-client
// (tool_call / tool_call_update chips in the chat panel).
func acpToolEventToUpdate(ev ACPToolEvent) map[string]any {
	status := "pending"
	sessionUpdate := "tool_call"
	switch strings.ToLower(strings.TrimSpace(ev.Phase)) {
	case "end", "completed", "failed":
		sessionUpdate = "tool_call_update"
		if ev.OK {
			status = "completed"
		} else {
			status = "failed"
		}
	case "start", "in_progress", "pending":
		sessionUpdate = "tool_call"
		status = "in_progress"
	default:
		if ev.Phase == "" && (ev.Result != "" || !ev.OK) {
			sessionUpdate = "tool_call_update"
			if ev.OK {
				status = "completed"
			} else {
				status = "failed"
			}
		}
	}
	title := strings.TrimSpace(ev.Title)
	if title == "" {
		title = acpToolTitle(ev.Name, ev.ArgsJSON)
	}
	kind := strings.TrimSpace(ev.Kind)
	if kind == "" {
		kind = acpToolKind(ev.Name)
	}
	update := map[string]any{
		"sessionUpdate": sessionUpdate,
		"toolCallId":    ev.ToolCallID,
		"title":         title,
		"status":        status,
		"kind":          kind,
	}
	if strings.TrimSpace(ev.ArgsJSON) != "" && sessionUpdate == "tool_call" {
		var raw any
		if json.Unmarshal([]byte(ev.ArgsJSON), &raw) == nil {
			update["rawInput"] = raw
		} else {
			update["rawInput"] = ev.ArgsJSON
		}
	}
	if sessionUpdate == "tool_call_update" && strings.TrimSpace(ev.Result) != "" {
		// Keep short; full result stays in agent message when needed.
		out := strings.TrimSpace(ev.Result)
		if len([]rune(out)) > 800 {
			out = string([]rune(out)[:800]) + "…"
		}
		update["rawOutput"] = out
	}
	// Prefer showing write content snippet on start for edit tools (diff clients).
	if sessionUpdate == "tool_call" && acpToolKind(ev.Name) == "edit" {
		if body := acpContentFromArgs(ev.ArgsJSON); body != "" {
			preview := truncateRunesACP(body, 400)
			update["content"] = []map[string]any{
				{"type": "text", "text": "```\n" + preview + "\n```"},
			}
		}
	}
	paths := ev.Paths
	if len(paths) == 0 {
		paths = acpPathsFromToolArgs(ev.Name, ev.ArgsJSON)
	}
	if len(paths) > 0 {
		locs := make([]map[string]any, 0, len(paths))
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			locs = append(locs, map[string]any{"path": p})
		}
		if len(locs) > 0 {
			update["locations"] = locs
		}
	}
	return update
}
