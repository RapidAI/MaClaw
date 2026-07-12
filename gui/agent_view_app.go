package main

import (
	"fmt"
	"strings"
)

// AppView (maclaw.appview.v1) — controlled multi-region workspace on top of AgentView.
// See docs/agent-dynamic-ui-runtime-design-zh.md §4.1.1 and appview-phase1-shell.md.

const (
	appViewSchemaV1             = "maclaw.appview.v1"
	appViewType                 = "app_view"
	appViewIDField              = "_app_id"
	appViewSessionField         = "_session_id"
	agentViewMissingRevisionErr = "missing_view_revision"
	agentViewMissingAppIDErr    = "missing_app_id"
	agentViewAppIDMismatchErr   = "app_id_mismatch"
	agentViewSessionMismatchErr = "session_id_mismatch"
	agentViewMissingSchemaErr   = "missing_schema_version"
)

// AppViewBuildInput is the server-side constructor for type=app_view payloads.
type AppViewBuildInput struct {
	AppID       string
	SessionID   string
	Title       string
	Description string
	Layout      string // workspace | record | report | tool
	// Main is required: one AgentView map or a list of them.
	Main interface{}
	// Side / Nav / Actions are optional controlled chrome.
	Side    interface{}
	Nav     []map[string]interface{}
	Actions []map[string]interface{}
	// SchemaContract feeds attachAgentViewSchemaVersion (stable hash input).
	SchemaContract interface{}
}

// BuildAppView constructs a validated app_view map ready for emitAgentView.
// It does not emit; callers pass the result to emitAgentView.
func BuildAppView(in AppViewBuildInput) (map[string]interface{}, error) {
	appID := strings.TrimSpace(in.AppID)
	if appID == "" {
		return nil, fmt.Errorf("app_view requires appId")
	}
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		sessionID = "desktop-user"
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = appID
	}
	layout := strings.ToLower(strings.TrimSpace(in.Layout))
	switch layout {
	case "", "workspace", "record", "report", "tool":
		if layout == "" {
			layout = "workspace"
		}
	default:
		return nil, fmt.Errorf("app_view invalid layout %q", in.Layout)
	}
	main, err := normalizeAppViewRegionViews(in.Main, "main")
	if err != nil {
		return nil, err
	}
	if len(main) == 0 {
		return nil, fmt.Errorf("app_view requires at least one main region view")
	}
	var side []map[string]interface{}
	if in.Side != nil {
		side, err = normalizeAppViewRegionViews(in.Side, "side")
		if err != nil {
			return nil, err
		}
	}

	viewID := "app:" + appID + ":" + sessionID
	view := map[string]interface{}{
		"schema":    appViewSchemaV1,
		"type":      appViewType,
		"id":        viewID,
		"appId":     appID,
		"sessionId": sessionID,
		"title":     title,
		"layout":    layout,
		"regions": map[string]interface{}{
			"main": mainAsRegionValue(main),
		},
	}
	if desc := strings.TrimSpace(in.Description); desc != "" {
		view["description"] = desc
	}
	if len(side) > 0 {
		regions := view["regions"].(map[string]interface{})
		regions["side"] = mainAsRegionValue(side)
	}
	if len(in.Nav) > 0 {
		regions := view["regions"].(map[string]interface{})
		regions["nav"] = cloneAppViewNav(in.Nav)
	}
	if len(in.Actions) > 0 {
		view["actions"] = cloneAppViewActions(in.Actions)
	}

	// Hidden tokens so nested form submits can carry app identity even without top-level payload fields.
	appendAgentViewHiddenField(view, appViewIDField, appID)
	appendAgentViewHiddenField(view, appViewSessionField, sessionID)

	contract := in.SchemaContract
	if contract == nil {
		contract = map[string]interface{}{
			"schema":  appViewSchemaV1,
			"appId":   appID,
			"layout":  layout,
			"mainIds": appViewRegionIDs(main),
		}
	}
	attachAgentViewSchemaVersion(view, "app.adapter", appID, contract)
	return view, nil
}

func mainAsRegionValue(views []map[string]interface{}) interface{} {
	if len(views) == 1 {
		return views[0]
	}
	out := make([]interface{}, 0, len(views))
	for _, v := range views {
		out = append(out, v)
	}
	return out
}

func normalizeAppViewRegionViews(raw interface{}, region string) ([]map[string]interface{}, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case map[string]interface{}:
		if err := validateEmbeddedAgentView(typed, region); err != nil {
			return nil, err
		}
		return []map[string]interface{}{typed}, nil
	case []map[string]interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for i, item := range typed {
			if err := validateEmbeddedAgentView(item, fmt.Sprintf("%s[%d]", region, i)); err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for i, item := range typed {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("app_view %s[%d] must be an object", region, i)
			}
			if err := validateEmbeddedAgentView(m, fmt.Sprintf("%s[%d]", region, i)); err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("app_view %s must be an AgentView object or array", region)
	}
}

func validateEmbeddedAgentView(view map[string]interface{}, where string) error {
	if view == nil {
		return fmt.Errorf("app_view %s is nil", where)
	}
	typ := strings.TrimSpace(fmt.Sprint(view["type"]))
	if typ == "" {
		return fmt.Errorf("app_view %s missing type", where)
	}
	if typ == appViewType {
		return fmt.Errorf("app_view %s must not nest another app_view", where)
	}
	title := strings.TrimSpace(fmt.Sprint(view["title"]))
	if title == "" {
		return fmt.Errorf("app_view %s missing title", where)
	}
	return nil
}

func appViewRegionIDs(views []map[string]interface{}) []string {
	ids := make([]string, 0, len(views))
	for _, v := range views {
		if id := strings.TrimSpace(fmt.Sprint(v["id"])); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func cloneAppViewNav(nav []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(nav))
	for _, item := range nav {
		if item == nil {
			continue
		}
		cp := map[string]interface{}{}
		for k, v := range item {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func cloneAppViewActions(actions []map[string]interface{}) []map[string]interface{} {
	return cloneAppViewNav(actions)
}

// IsAppView reports whether the payload is a maclaw app_view workspace.
func IsAppView(view map[string]interface{}) bool {
	if view == nil {
		return false
	}
	typ := strings.TrimSpace(fmt.Sprint(view["type"]))
	if typ == appViewType {
		return true
	}
	schema := strings.TrimSpace(fmt.Sprint(view["schema"]))
	return schema == appViewSchemaV1
}

// AppViewAppID extracts appId from a view map.
func AppViewAppID(view map[string]interface{}) string {
	if view == nil {
		return ""
	}
	if id := strings.TrimSpace(fmt.Sprint(view["appId"])); id != "" && id != "<nil>" {
		return id
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta != nil {
		if id := strings.TrimSpace(fmt.Sprint(meta["appId"])); id != "" && id != "<nil>" {
			return id
		}
	}
	return agentViewHiddenFieldString(view, appViewIDField)
}

// AppViewSessionID extracts sessionId from a view map.
func AppViewSessionID(view map[string]interface{}) string {
	if view == nil {
		return ""
	}
	if id := strings.TrimSpace(fmt.Sprint(view["sessionId"])); id != "" && id != "<nil>" {
		return id
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta != nil {
		if id := strings.TrimSpace(fmt.Sprint(meta["sessionId"])); id != "" && id != "<nil>" {
			return id
		}
	}
	return agentViewHiddenFieldString(view, appViewSessionField)
}
