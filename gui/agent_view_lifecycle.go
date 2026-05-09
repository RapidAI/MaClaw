package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

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
	if a == nil || a.ctx == nil {
		return false
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["action"] = action
	runtime.EventsEmit(a.ctx, agentViewLifecycleEvent, payload)
	return true
}

func (a *App) emitAgentView(view map[string]interface{}) bool {
	if a == nil || a.ctx == nil || view == nil {
		return false
	}
	a.recordAgentViewSchema(view)
	payload := map[string]interface{}{"view": view}
	runtime.EventsEmit(a.ctx, agentViewEvent, payload)
	a.emitAgentViewLifecycle(agentViewLifecycleOpen, cloneAgentViewPayload(payload))
	return true
}

func (a *App) clearAgentView(viewID string) bool {
	if a == nil || a.ctx == nil {
		return false
	}
	payload := map[string]interface{}{"view_id": viewID}
	runtime.EventsEmit(a.ctx, agentViewClearEvent, payload)
	a.emitAgentViewLifecycle(agentViewLifecycleDismiss, cloneAgentViewPayload(payload))
	return true
}

func cloneAgentViewPayload(payload map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(payload)+1)
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
