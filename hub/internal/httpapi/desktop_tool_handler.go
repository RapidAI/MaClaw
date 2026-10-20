package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// desktopToolCall is the worker's machine call. The body has no conversation.
type desktopToolCall struct {
	RunID      string         `json:"run_id"`
	ToolCallID string         `json:"tool_call_id"`
	Tenant     string         `json:"tenant"`
	User       string         `json:"user"`
	Action     string         `json:"action"`
	Args       map[string]any `json:"args"`
	Display    string         `json:"display,omitempty"`
}

// desktopToolRoutes are desktopd's existing POST /v1/desktops/* names.
// None of them is a model loop.
var desktopToolRoutes = map[string]string{
	"screenshot":    "screenshot",
	"file_read":     "file",
	"file_write":    "file",
	"file_edit":     "file",
	"file_list":     "file",
	"bash":          "bash",
	"http":          "http",
	"install":       "install",
	"app_open":      "open",
	"app_list":      "app",
	"app_run":       "app",
	"computer":      "app",
	"click":         "ctl",
	"type":          "ctl",
	"press":         "ctl",
	"probe":         "ctl",
	"app_probe":     "ctl",
	"navigate":      "ctl",
	"script":        "ctl",
	"skill_run":     "ctl",
	"task_run":      "ctl",
	"clipboard_get": "ctl",
	"clipboard_set": "ctl",
	"window_list":   "ctl",
	"window":        "ctl",
	"screen_info":   "ctl",
	"wait":          "ctl",
	"session":       "session",
	"stop":          "stop",
}

func desktopToolRoute(action string) (string, error) {
	route, ok := desktopToolRoutes[strings.ToLower(strings.TrimSpace(action))]
	if !ok || route == "" {
		return "", fmt.Errorf("unknown desktop action %s", strings.TrimSpace(action))
	}
	return route, nil
}

func parseDesktopToolCall(raw []byte) (desktopToolCall, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return desktopToolCall{}, fmt.Errorf("invalid desktop tool call")
	}
	for key := range fields {
		switch key {
		case "run_id", "tool_call_id", "tenant", "user", "action", "args", "display":
		default:
			return desktopToolCall{}, fmt.Errorf("desktop tool call rejected %s", key)
		}
	}
	var call desktopToolCall
	if err := json.Unmarshal(raw, &call); err != nil {
		return desktopToolCall{}, fmt.Errorf("invalid desktop tool call")
	}
	if args, ok := fields["args"]; ok {
		var argFields map[string]json.RawMessage
		if err := json.Unmarshal(args, &argFields); err != nil {
			return desktopToolCall{}, fmt.Errorf("invalid desktop tool args")
		}
		for key := range argFields {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "messages", "memory", "send_to_user", "conversation", "history", "transcript":
				return desktopToolCall{}, fmt.Errorf("desktop tool call rejected %s", key)
			}
		}
	}
	call.RunID = strings.TrimSpace(call.RunID)
	call.ToolCallID = strings.TrimSpace(call.ToolCallID)
	call.Tenant = strings.TrimSpace(call.Tenant)
	call.User = strings.TrimSpace(call.User)
	call.Action = strings.ToLower(strings.TrimSpace(call.Action))
	call.Display = strings.TrimSpace(call.Display)
	if call.RunID == "" || call.ToolCallID == "" || call.Tenant == "" || call.User == "" || call.Action == "" {
		return desktopToolCall{}, fmt.Errorf("desktop tool call is incomplete")
	}
	if _, err := desktopToolRoute(call.Action); err != nil {
		return desktopToolCall{}, err
	}
	return call, nil
}

// PostDesktopToolHandler is the worker's one machine call. Hub forwards it
// to desktopd's existing route. The container does not run a model.
func PostDesktopToolHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop tool call")
			return
		}
		call, err := parseDesktopToolCall(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", err.Error())
			return
		}
		text, attachmentID, err := dispatchDesktopTool(r.Context(), pool, call)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"output":        text,
			"attachment_id": attachmentID,
			"executor_id":   "assigned",
			"volumes":       "desktops,home,opt,usr-local",
			"replaced":      false,
		})
	}
}

func dispatchDesktopTool(ctx context.Context, pool *desktoppool.Pool, call desktopToolCall) (string, string, error) {
	route, err := desktopToolRoute(call.Action)
	if err != nil {
		return "", "", err
	}
	switch route {
	case "screenshot":
		_, path, err := pool.Screenshot(ctx, call.Tenant, call.User, call.Display, toolArgString(call.Args, "name"))
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(path) == "" {
			path = "screenshot:" + call.ToolCallID
		}
		return "", path, nil
	case "file":
		wire := strings.TrimPrefix(call.Action, "file_")
		text, err := pool.RunFile(ctx, call.Tenant, call.User, wire, toolArgString(call.Args, "path"), toolArgRaw(call.Args, "content"), toolArgRaw(call.Args, "old_string"), toolArgRaw(call.Args, "new_string"))
		return text, "", err
	case "bash":
		text, err := pool.RunBash(ctx, call.Tenant, call.User, toolArgString(call.Args, "command"))
		return text, "", err
	case "http":
		body, status, kind, err := pool.RunHTTP(ctx, call.Tenant, call.User, toolArgString(call.Args, "url"))
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d %s\n%s", status, kind, body), "", nil
	case "install":
		text, err := pool.RunInstall(ctx, call.Tenant, call.User, toolArgStrings(call.Args, "packages"))
		return text, "", err
	case "open":
		text, err := pool.RunOpen(ctx, call.Tenant, call.User, call.Display, toolArgString(call.Args, "program"), toolArgStrings(call.Args, "args"))
		return text, "", err
	case "app":
		// A steps/actions batch is expanded by the worker (paste guard, one
		// xdotool call per step). RunApp only takes args, so a batch here
		// would run nothing and still report success.
		if desktopToolHasBatch(call.Args) {
			return "", "", fmt.Errorf("desktop %s steps run on the worker; send app args, not a steps batch", call.Action)
		}
		text, err := pool.RunApp(ctx, call.Tenant, call.User, call.Display, toolArgStrings(call.Args, "args"))
		return text, "", err
	case "session":
		session, err := pool.OpenDesktop(ctx, call.Tenant, call.User)
		if err != nil {
			return "", "", err
		}
		raw, err := json.Marshal(map[string]string{"display": session.Display, "cdp_url": session.CDP})
		return string(raw), "", err
	case "stop":
		if _, err := pool.StopDesktop(ctx, call.Tenant, call.User); err != nil {
			return "", "", err
		}
		return "stopped", "", nil
	case "ctl":
		out, err := pool.Ctl(ctx, call.Tenant, call.User, call.Display, map[string]any{
			"action": call.Action,
			"args":   call.Args,
		})
		if err != nil {
			return "", "", err
		}
		raw, err := json.Marshal(out)
		return string(raw), "", err
	default:
		return "", "", fmt.Errorf("unknown desktop route %s", route)
	}
}

func toolArgString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	text, _ := args[key].(string)
	return strings.TrimSpace(text)
}

func toolArgRaw(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	text, _ := args[key].(string)
	return text
}

func toolArgStrings(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	switch value := args[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// desktopToolHasBatch reports a steps or actions list without plain args.
func desktopToolHasBatch(args map[string]any) bool {
	if len(toolArgStrings(args, "args")) > 0 {
		return false
	}
	for _, key := range []string{"steps", "actions"} {
		if list, ok := args[key].([]any); ok && len(list) > 0 {
			return true
		}
	}
	return false
}
