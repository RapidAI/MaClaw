package main

import (
	"fmt"
	"strings"
	"time"
)

// AgentView revision protection (AppView phase 0 foundation).
//
// Each opened/updated AgentView gets a monotic viewRevision for its view id.
// Submits that still carry an older revision after a newer open/update are
// rejected with a recoverable error so users do not act on a stale panel.

const (
	agentViewRevisionField       = "_agent_view_revision"
	agentViewStaleRevisionError  = "stale_view_revision"
	agentViewSchemaMismatchError = "schema_version_mismatch"
)

type agentViewOpenRecord struct {
	ViewID        string
	Revision      int64
	SchemaVersion string
	SchemaSource  string
	SchemaID      string
	AppID         string
	SessionID     string
	// Strict is true for type=app_view or any view with appId: client must send
	// view_revision + schema_version (+ matching app/session when known).
	Strict    bool
	OpenedAt  time.Time
	UpdatedAt time.Time
}

// agentViewOpen is stored on App (see app.go) to avoid cross-instance pollution in tests.

func (a *App) rememberAgentViewOpen(view map[string]interface{}, emissionSeq int64) int64 {
	if a == nil || view == nil {
		return 0
	}
	viewID := strings.TrimSpace(fmt.Sprint(view["id"]))
	if viewID == "" {
		return 0
	}
	meta, _ := view["meta"].(map[string]interface{})
	if meta == nil {
		meta = map[string]interface{}{}
		view["meta"] = meta
	}
	schemaVersion := strings.TrimSpace(fmt.Sprint(meta["schemaVersion"]))
	if schemaVersion == "" {
		// Fall back to hidden field when meta was not populated yet.
		schemaVersion = agentViewHiddenFieldString(view, agentViewSchemaVersionField)
	}

	a.agentViewOpenMu.Lock()
	defer a.agentViewOpenMu.Unlock()
	if a.agentViewOpen == nil {
		a.agentViewOpen = map[string]agentViewOpenRecord{}
	}
	now := time.Now()
	prev := a.agentViewOpen[viewID]
	rev := prev.Revision + 1
	if rev <= 0 {
		rev = 1
	}
	// Prefer emission seq as a high-water mark when available so restarts
	// that re-open the same id still move forward relative to the client seq.
	if emissionSeq > rev {
		rev = emissionSeq
	}
	appID := AppViewAppID(view)
	sessionID := AppViewSessionID(view)
	strict := IsAppView(view) || appID != ""
	rec := agentViewOpenRecord{
		ViewID:        viewID,
		Revision:      rev,
		SchemaVersion: schemaVersion,
		SchemaSource:  strings.TrimSpace(fmt.Sprint(meta["schemaSource"])),
		SchemaID:      strings.TrimSpace(fmt.Sprint(meta["schemaID"])),
		AppID:         appID,
		SessionID:     sessionID,
		Strict:        strict,
		OpenedAt:      prev.OpenedAt,
		UpdatedAt:     now,
	}
	if rec.OpenedAt.IsZero() {
		rec.OpenedAt = now
	}
	a.agentViewOpen[viewID] = rec

	meta["viewRevision"] = rev
	if schemaVersion != "" {
		meta["schemaVersion"] = schemaVersion
	}
	if appID != "" {
		meta["appId"] = appID
		view["appId"] = appID
	}
	if sessionID != "" {
		meta["sessionId"] = sessionID
		view["sessionId"] = sessionID
	}
	// Top-level viewRevision mirrors design (AppView.viewRevision).
	view["viewRevision"] = rev
	appendAgentViewHiddenField(view, agentViewRevisionField, rev)
	if appID != "" {
		appendAgentViewHiddenField(view, appViewIDField, appID)
	}
	if sessionID != "" {
		appendAgentViewHiddenField(view, appViewSessionField, sessionID)
	}
	return rev
}

func (a *App) forgetAgentViewOpen(viewID string) {
	if a == nil {
		return
	}
	viewID = strings.TrimSpace(viewID)
	if viewID == "" {
		return
	}
	a.agentViewOpenMu.Lock()
	defer a.agentViewOpenMu.Unlock()
	if a.agentViewOpen != nil {
		delete(a.agentViewOpen, viewID)
	}
}

func (a *App) agentViewOpenRecord(viewID string) (agentViewOpenRecord, bool) {
	if a == nil {
		return agentViewOpenRecord{}, false
	}
	viewID = strings.TrimSpace(viewID)
	if viewID == "" {
		return agentViewOpenRecord{}, false
	}
	a.agentViewOpenMu.Lock()
	defer a.agentViewOpenMu.Unlock()
	if a.agentViewOpen == nil {
		return agentViewOpenRecord{}, false
	}
	rec, ok := a.agentViewOpen[viewID]
	return rec, ok
}

// validateAgentViewSubmitRevision rejects stale panels after a newer open/update.
//
// Compatibility (non-strict AgentView):
//   - No open record for view_id → accept (legacy / non-tracked views).
//   - Client omits view_revision → accept (old frontends); still checks schema
//     when both sides have a schema version.
//   - Client sends view_revision < current → reject with stale_view_revision.
//
// Strict mode (type=app_view or appId present on open record / payload):
//   - Requires view_revision (exact match) and schema_version when known.
//   - Requires matching appId; sessionId when both sides set.
func (a *App) validateAgentViewSubmitRevision(payload AgentViewSubmitPayload) *IMAgentResponse {
	if a == nil {
		return nil
	}
	viewID := strings.TrimSpace(payload.ViewID)
	if viewID == "" {
		return nil
	}
	rec, ok := a.agentViewOpenRecord(viewID)
	if !ok || rec.Revision <= 0 {
		// No open record: still enforce strict tokens when client declares appId.
		if strings.TrimSpace(payload.AppID) != "" || agentViewSubmitAppID(payload) != "" {
			return validateStrictAppViewSubmitWithoutRecord(payload)
		}
		return nil
	}

	clientRev := agentViewSubmitRevision(payload)
	clientSchema := agentViewSubmitSchemaVersion(payload)
	strict := rec.Strict || strings.TrimSpace(payload.AppID) != "" || agentViewSubmitAppID(payload) != ""

	if strict {
		if clientRev <= 0 {
			return &IMAgentResponse{
				Text: avTr(
					"App view submit requires view_revision. Re-open the app workspace and try again.",
					"应用视图提交必须携带 view_revision。请重新打开应用工作区后再试。",
				),
				Error:          agentViewMissingRevisionErr,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
		if clientRev != rec.Revision {
			return &IMAgentResponse{
				Text: avTr(
					"This app workspace is out of date. Close it and use the latest view.",
					"应用工作区已过期。请关闭后使用最新界面再提交。",
				),
				Error:          agentViewStaleRevisionError,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
		if rec.SchemaVersion != "" {
			if clientSchema == "" {
				return &IMAgentResponse{
					Text: avTr(
						"App view submit requires schema_version.",
						"应用视图提交必须携带 schema_version。",
					),
					Error:          agentViewMissingSchemaErr,
					ResponseSource: imResponseSourceAgentViewSubmit.String(),
				}
			}
			if clientSchema != rec.SchemaVersion {
				return &IMAgentResponse{
					Text: avTr(
						"This app workspace schema changed. Refresh and submit again.",
						"应用工作区 schema 已变更。请刷新后重新提交。",
					),
					Error:          agentViewSchemaMismatchError,
					ResponseSource: imResponseSourceAgentViewSubmit.String(),
				}
			}
		}
		if err := validateAppViewIdentity(payload, rec.AppID, rec.SessionID, true); err != nil {
			return err
		}
		return nil
	}

	if clientRev > 0 && clientRev < rec.Revision {
		return &IMAgentResponse{
			Text: avTr(
				"This task panel is out of date. Close it and use the latest form that just opened.",
				"任务面板已过期。请关闭后使用刚打开的最新表单再提交。",
			),
			Error:          agentViewStaleRevisionError,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	if clientSchema != "" && rec.SchemaVersion != "" && clientSchema != rec.SchemaVersion {
		return &IMAgentResponse{
			Text: avTr(
				"This task panel schema changed. Refresh the form and submit again.",
				"任务面板 schema 已变更。请刷新表单后重新提交。",
			),
			Error:          agentViewSchemaMismatchError,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	return nil
}

func validateStrictAppViewSubmitWithoutRecord(payload AgentViewSubmitPayload) *IMAgentResponse {
	if agentViewSubmitRevision(payload) <= 0 {
		return &IMAgentResponse{
			Text: avTr(
				"App view submit requires view_revision.",
				"应用视图提交必须携带 view_revision。",
			),
			Error:          agentViewMissingRevisionErr,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	if agentViewSubmitAppID(payload) == "" && strings.TrimSpace(payload.AppID) == "" {
		return &IMAgentResponse{
			Text: avTr(
				"App view submit requires appId.",
				"应用视图提交必须携带 appId。",
			),
			Error:          agentViewMissingAppIDErr,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	return nil
}

func validateAppViewIdentity(payload AgentViewSubmitPayload, expectedAppID, expectedSessionID string, requireApp bool) *IMAgentResponse {
	clientApp := strings.TrimSpace(payload.AppID)
	if clientApp == "" {
		clientApp = agentViewSubmitAppID(payload)
	}
	if requireApp && expectedAppID != "" {
		if clientApp == "" {
			return &IMAgentResponse{
				Text: avTr(
					"App view submit requires appId.",
					"应用视图提交必须携带 appId。",
				),
				Error:          agentViewMissingAppIDErr,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
		if clientApp != expectedAppID {
			return &IMAgentResponse{
				Text: avTr(
					"App id does not match the open workspace.",
					"提交的 appId 与当前打开的应用工作区不一致。",
				),
				Error:          agentViewAppIDMismatchErr,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
	}
	clientSession := strings.TrimSpace(payload.SessionID)
	if clientSession == "" {
		clientSession = agentViewSubmitSessionID(payload)
	}
	if expectedSessionID != "" && clientSession != "" && clientSession != expectedSessionID {
		return &IMAgentResponse{
			Text: avTr(
				"Session id does not match the open workspace.",
				"提交的 sessionId 与当前打开的应用工作区不一致。",
			),
			Error:          agentViewSessionMismatchErr,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	return nil
}

func agentViewSubmitRevision(payload AgentViewSubmitPayload) int64 {
	if payload.ViewRevision > 0 {
		return payload.ViewRevision
	}
	if payload.Data == nil {
		return 0
	}
	return parseAgentViewInt64(payload.Data[agentViewRevisionField])
}

func agentViewSubmitSchemaVersion(payload AgentViewSubmitPayload) string {
	if s := strings.TrimSpace(payload.SchemaVersion); s != "" {
		return s
	}
	if payload.Data == nil {
		return ""
	}
	if s := strings.TrimSpace(fmt.Sprint(payload.Data[agentViewSchemaVersionField])); s != "" && s != "<nil>" {
		return s
	}
	return ""
}

func agentViewSubmitAppID(payload AgentViewSubmitPayload) string {
	if s := strings.TrimSpace(payload.AppID); s != "" {
		return s
	}
	if payload.Data == nil {
		return ""
	}
	if s := strings.TrimSpace(fmt.Sprint(payload.Data[appViewIDField])); s != "" && s != "<nil>" {
		return s
	}
	return ""
}

func agentViewSubmitSessionID(payload AgentViewSubmitPayload) string {
	if s := strings.TrimSpace(payload.SessionID); s != "" {
		return s
	}
	if payload.Data == nil {
		return ""
	}
	if s := strings.TrimSpace(fmt.Sprint(payload.Data[appViewSessionField])); s != "" && s != "<nil>" {
		return s
	}
	return ""
}

func parseAgentViewInt64(raw interface{}) int64 {
	switch v := raw.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case string:
		var n int64
		fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		return n
	default:
		return 0
	}
}

func agentViewHiddenFieldString(view map[string]interface{}, name string) string {
	rawFields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		// Some builders use []interface{} of maps.
		if list, ok := view["fields"].([]interface{}); ok {
			for _, item := range list {
				field, _ := item.(map[string]interface{})
				if field == nil {
					continue
				}
				if strings.TrimSpace(fmt.Sprint(field["name"])) == name {
					return strings.TrimSpace(fmt.Sprint(field["value"]))
				}
			}
		}
		return ""
	}
	for _, field := range rawFields {
		if strings.TrimSpace(fmt.Sprint(field["name"])) == name {
			return strings.TrimSpace(fmt.Sprint(field["value"]))
		}
	}
	return ""
}
