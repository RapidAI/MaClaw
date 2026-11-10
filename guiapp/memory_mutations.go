package guiapp

import "github.com/RapidAI/CodeClaw/corelib/memory"

func (a *App) subscribeMemoryMutations(runtime *memoryRuntimeState) {
	if a == nil || runtime == nil || runtime.Store == nil {
		return
	}
	runtime.Store.SubscribeMutations(func(event memory.MutationEvent) {
		a.handleMemoryMutationForRuntime(runtime, event)
	})
}

func (a *App) handleMemoryMutationForRuntime(runtime *memoryRuntimeState, event memory.MutationEvent) {
	if a == nil || runtime == nil {
		return
	}
	current := a.memRuntime.Load()
	if event.Kind == memory.MutationReset {
		a.refreshAgentMemorySnapshotsForStore(runtime.Store)
	} else {
		change := memoryWarehouseChange{OwnerID: event.OwnerID}
		if event.Before != nil && (event.Kind == memory.MutationUpdate || event.Kind == memory.MutationSupersede || event.Kind == memory.MutationDelete) {
			replacement := ""
			if event.After != nil && event.After.IsActive() {
				replacement = event.After.Content
			}
			kind := string(event.Kind)
			if event.Kind == memory.MutationUpdate {
				kind = "updated"
			} else if event.Kind == memory.MutationDelete {
				kind = "deleted"
			}
			change = warehouseChangeFromEntries([]memory.Entry{*event.Before}, kind, replacement)
			change.OwnerID = event.OwnerID
		}
		a.notifyMemoryWarehouseChangedForStore(runtime.Store, change)
	}
	if current != runtime {
		return
	}
	a.emitEvent("memory:changed", map[string]interface{}{
		"kind":        string(event.Kind),
		"owner_id":    event.OwnerID,
		"mutation_id": event.MutationID,
	})
}
