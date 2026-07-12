package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	agentViewEvent          = "agent-view"
	agentViewClearEvent     = "agent-view-clear"
	agentViewLifecycleEvent = "agent-view:lifecycle"

	agentViewLifecycleOpen     = "open"
	agentViewLifecycleUpdate   = "update"
	agentViewLifecycleSubmit   = "submit"
	agentViewLifecycleDismiss  = "dismiss"
	agentViewLifecycleError    = "error"
	agentViewLifecycleComplete = "complete"
)

func (a *App) emitAgentViewLifecycle(action string, payload map[string]interface{}) bool {
	if a == nil {
		return false
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["action"] = action
	if _, ok := payload["seq"]; !ok {
		payload["seq"] = a.nextAgentViewSeq()
	}
	if a.ctx == nil {
		return false
	}
	return a.emitAgentViewEvent(agentViewLifecycleEvent, payload)
}

func (a *App) emitAgentView(view map[string]interface{}) bool {
	if a == nil || view == nil {
		return false
	}
	seq := a.nextAgentViewSeq()
	// Attach/register monotic viewRevision before clients receive the payload.
	a.rememberAgentViewOpen(view, seq)
	if a.ctx == nil {
		return false
	}
	a.recordAgentViewSchema(view)
	payload := map[string]interface{}{"view": view, "seq": seq}
	if meta, _ := view["meta"].(map[string]interface{}); meta != nil {
		if rev, ok := meta["viewRevision"]; ok {
			payload["view_revision"] = rev
		}
		if ver := strings.TrimSpace(fmt.Sprint(meta["schemaVersion"])); ver != "" && ver != "<nil>" {
			payload["schema_version"] = ver
		}
	}
	// Single event channel: agent-view:lifecycle is the formal protocol.
	// The legacy "agent-view" event is retained only for external consumers
	// (e.g. IM gateway, older Wails bindings) that haven't migrated.
	legacyOK := a.emitAgentViewEvent(agentViewEvent, payload)
	lifecycleOK := a.emitAgentViewLifecycle(agentViewLifecycleOpen, cloneAgentViewPayload(payload))
	if !legacyOK && !lifecycleOK {
		return false
	}
	return true
}

func (a *App) agentViewSeq() int64 {
	if a == nil {
		return 0
	}
	return a.agentViewEmissionSeq.Load()
}

func (a *App) nextAgentViewSeq() int64 {
	if a == nil {
		return 0
	}
	return a.agentViewEmissionSeq.Add(1)
}

// initAgentViewSeqEpoch sets the initial agentViewEmissionSeq to a time-based
// epoch value. This ensures that after a backend restart, emitted seq values
// are always greater than any value a surviving frontend WebView might have
// cached from the previous session.
//
// Without this, a backend restart resets the atomic counter to 0, but the
// frontend's agentViewLifecycleSeqBySessionRef retains the high seq values
// from before the restart. All post-restart AgentView events (seq=1,2,3...)
// are rejected as "stale" because they're less than the frontend's cached seq.
//
// Using Unix seconds (not millis) keeps values well within int64 range and
// provides ~1B of headroom per second for Add(1) calls.
func (a *App) initAgentViewSeqEpoch() {
	epoch := time.Now().Unix()
	a.agentViewEmissionSeq.Store(epoch)
	log.Printf("[agent-view] seq epoch initialized: %d", epoch)
}

func (a *App) clearAgentView(viewID string) bool {
	return a.clearAgentViewWithPayload(viewID, nil)
}

func (a *App) clearAgentViewWithPayload(viewID string, extra map[string]interface{}) bool {
	if a == nil {
		return false
	}
	seq := a.nextAgentViewSeq()
	a.forgetAgentViewOpen(viewID)
	if a.ctx == nil {
		return false
	}
	payload := map[string]interface{}{"view_id": viewID, "seq": seq}
	for key, value := range extra {
		payload[key] = value
	}
	legacyOK := a.emitAgentViewEvent(agentViewClearEvent, payload)
	lifecycleOK := a.emitAgentViewLifecycle(agentViewLifecycleDismiss, cloneAgentViewPayload(payload))
	if !legacyOK && !lifecycleOK {
		return false
	}
	return true
}

func (a *App) emitAgentViewEvent(name string, payload map[string]interface{}) (ok bool) {
	if a == nil || a.ctx == nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[agent-view] emit %s panic: %v", name, r)
			ok = false
		}
	}()
	a.emitEvent(name, payload)
	return true
}

func cloneAgentViewPayload(payload map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(payload)+1)
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
