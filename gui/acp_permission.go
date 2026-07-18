package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// acpPermissionGate asks the VS Code client (via bridge reverse RPC) before
// running risky tools. Workspace-local file edits auto-allow; bash/delete ask.
// allow_always is remembered per ACP sessionId + tool name until the TCP session ends.
type acpPermissionFn func(ctx context.Context, toolName, argsJSON string) (allowed bool, reason string)

type acpPermissionRegistry struct {
	mu    sync.Mutex
	gates map[string]acpPermissionFn // requestID
}

var globalACPPermission acpPermissionRegistry

func (r *acpPermissionRegistry) set(requestID string, fn acpPermissionFn) (clear func()) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || fn == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.gates == nil {
		r.gates = make(map[string]acpPermissionFn)
	}
	r.gates[requestID] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.gates, requestID)
		r.mu.Unlock()
	}
}

func (r *acpPermissionRegistry) check(ctx context.Context, requestID, toolName, argsJSON string) (allowed bool, reason string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || !isACPProgrammingRequestID(requestID) {
		return true, ""
	}
	if !acpToolNeedsPermission(toolName, argsJSON) {
		return true, ""
	}
	r.mu.Lock()
	fn := r.gates[requestID]
	r.mu.Unlock()
	if fn == nil {
		// No gate registered (non-Mode-B or tests): allow to avoid hard break.
		return true, ""
	}
	return fn(ctx, toolName, argsJSON)
}

func acpToolNeedsPermission(name, argsJSON string) bool {
	switch strings.TrimSpace(name) {
	case "bash", "run_terminal", "shell", "powershell":
		return true
	case "delete_file", "remove_file":
		return true
	case "write_file", "edit_file", "create_file":
		// Always enter the gate so requestClientPermission can auto-allow
		// workspace paths and prompt for escapes outside cwd.
		return true
	default:
		return false
	}
}

// requestClientPermission implements session/request_permission against VS Code.
func (s *acpHostSession) requestClientPermission(ctx context.Context, sessionID, toolName, argsJSON, cwd string) (bool, string) {
	if s == nil {
		return false, "no acp session"
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
	}
	toolName = strings.TrimSpace(toolName)
	sessionID = strings.TrimSpace(sessionID)

	// Session-scoped allow_always (per tool name).
	if s.isAllowAlways(sessionID, toolName) {
		return true, ""
	}

	title := acpToolTitle(toolName, argsJSON)
	kind := acpToolKind(toolName)
	paths := acpPathsFromToolArgs(toolName, argsJSON)

	// Writes: auto-allow only when every path is under the VS Code workspace.
	// Missing path or escape outside cwd → user confirmation (or reject if no client).
	if nameIsWriteTool(toolName) || kind == "edit" {
		if len(paths) == 0 {
			return false, "write tool missing path"
		}
		if cwd != "" && pathUnderWorkspace(cwd, paths) {
			return true, ""
		}
		// fall through to VS Code permission prompt
	}

	params := map[string]any{
		"sessionId": sessionID,
		"toolCall": map[string]any{
			"toolCallId": newACPToolCallID(toolName),
			"title":      title,
			"kind":       kind,
			"status":     "pending",
		},
		"options": []map[string]any{
			{"optionId": "allow_once", "name": "Allow once", "kind": "allow_once"},
			{"optionId": "allow_always", "name": "Allow always (this VS Code session)", "kind": "allow_always"},
			{"optionId": "reject_once", "name": "Reject", "kind": "reject_once"},
		},
	}
	if tc, ok := params["toolCall"].(map[string]any); ok {
		if len(paths) > 0 {
			locs := make([]map[string]any, 0, len(paths))
			for _, p := range paths {
				locs = append(locs, map[string]any{"path": p})
			}
			tc["locations"] = locs
		}
		if strings.TrimSpace(argsJSON) != "" {
			var raw any
			if json.Unmarshal([]byte(argsJSON), &raw) == nil {
				tc["rawInput"] = raw
			}
		}
	}
	raw, err := s.callClient(ctx, "session/request_permission", params)
	if err != nil {
		return false, "permission request failed: " + err.Error()
	}
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return false, "invalid permission response"
	}
	switch res.Outcome.Outcome {
	case "selected":
		switch res.Outcome.OptionID {
		case "allow_once":
			return true, ""
		case "allow_always":
			s.rememberAllowAlways(sessionID, toolName)
			return true, ""
		case "reject_once", "reject_always":
			return false, "user rejected tool in VS Code"
		default:
			if strings.HasPrefix(res.Outcome.OptionID, "allow_always") || res.Outcome.OptionID == "allowAlways" {
				s.rememberAllowAlways(sessionID, toolName)
				return true, ""
			}
			if strings.HasPrefix(res.Outcome.OptionID, "allow") {
				return true, ""
			}
			return false, "user rejected tool in VS Code"
		}
	case "cancelled":
		return false, "permission cancelled in VS Code"
	default:
		return false, fmt.Sprintf("permission outcome %q", res.Outcome.Outcome)
	}
}

func (s *acpHostSession) isAllowAlways(sessionID, toolName string) bool {
	if s == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	toolName = strings.TrimSpace(toolName)
	s.allowAlwaysMu.Lock()
	defer s.allowAlwaysMu.Unlock()
	if s.allowAlways == nil {
		return false
	}
	if s.allowAlways["*"][toolName] || s.allowAlways[sessionID][toolName] {
		return true
	}
	// Also honor allow-all-tools for this session.
	if s.allowAlways[sessionID]["*"] {
		return true
	}
	return false
}

func (s *acpHostSession) rememberAllowAlways(sessionID, toolName string) {
	if s == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	toolName = strings.TrimSpace(toolName)
	if sessionID == "" || toolName == "" {
		return
	}
	s.allowAlwaysMu.Lock()
	defer s.allowAlwaysMu.Unlock()
	if s.allowAlways == nil {
		s.allowAlways = make(map[string]map[string]bool)
	}
	if s.allowAlways[sessionID] == nil {
		s.allowAlways[sessionID] = make(map[string]bool)
	}
	s.allowAlways[sessionID][toolName] = true
}

func nameIsWriteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "write_file", "edit_file", "create_file":
		return true
	default:
		return false
	}
}

func pathUnderWorkspace(cwd string, paths []string) bool {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || len(paths) == 0 {
		return false
	}
	// Windows: compare paths case-insensitively for drive/letters.
	norm := func(p string) string {
		p = filepath.Clean(p)
		if filepath.Separator == '\\' {
			return strings.ToLower(p)
		}
		return p
	}
	cwdN := norm(cwd)
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			return false
		}
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(cwd, p)
		}
		abs = filepath.Clean(abs)
		// Prefer case-normalized Rel on Windows so mixed drive case never
		// mis-classifies in-workspace paths as escapes (fail-open for edits).
		relBase, relAbs := cwd, abs
		if filepath.Separator == '\\' {
			relBase, relAbs = cwdN, norm(abs)
		}
		rel, err := filepath.Rel(relBase, relAbs)
		if err != nil {
			return false
		}
		rel = filepath.Clean(rel)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
		// Extra guard: absolute path must still sit under cwd after norm.
		if filepath.Separator == '\\' {
			absN := norm(abs)
			if absN != cwdN && !strings.HasPrefix(absN, cwdN+string(filepath.Separator)) {
				return false
			}
		}
	}
	return true
}
