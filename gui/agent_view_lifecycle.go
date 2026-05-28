package main

import (
	"log"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	if a.ctx == nil {
		return false
	}
	a.recordAgentViewSchema(view)
	payload := map[string]interface{}{"view": view, "seq": seq}
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

func (a *App) clearAgentView(viewID string) bool {
	return a.clearAgentViewWithPayload(viewID, nil)
}

func (a *App) clearAgentViewWithPayload(viewID string, extra map[string]interface{}) bool {
	if a == nil {
		return false
	}
	seq := a.nextAgentViewSeq()
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
	runtime.EventsEmit(a.ctx, name, payload)
	return true
}

func cloneAgentViewPayload(payload map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(payload)+1)
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
