package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// hubRuntimeExecutorAdapter bridges the 5-arg RuntimeExecutor.StartInstance
// (initiate via the published-version form schema) to the existing 2-arg
// WorkflowExecutor.StartInstance (trigger-data based), so the RuntimeAPI can be
// registered on the live surface without changing either signature.
//
// The RuntimeAPI's handleInitiateWorkflow already validates form_data against
// the published version's schema via FormValidator before calling this adapter;
// the adapter's sole job is to marshal the runtime-specific fields
// (form_data, initiator_id, channel, submission_timestamp) into the trigger-data
// JSON that WorkflowExecutor.StartInstance parses into InstanceData. This mirrors
// the enrichTriggerDataWithUser convention used by TriggerFromMarket and the
// adapter shape in runtime_integration_test.go / the bug-condition test.
//
// initiator_id is persisted so the existing withdrawal (extractInitiatorID) and
// directory (QueryMyInitiated filters on instance_data.initiator_id) paths bind
// the instance to its initiator unchanged.
type hubRuntimeExecutorAdapter struct {
	executor *workflow.WorkflowExecutor
}

// Compile-time assertion that the adapter satisfies the RuntimeExecutor
// interface the RuntimeAPI depends on.
var _ workflow.RuntimeExecutor = (*hubRuntimeExecutorAdapter)(nil)

// newHubRuntimeExecutorAdapter wraps a WorkflowExecutor so it can serve the
// RuntimeAPI initiation path.
func newHubRuntimeExecutorAdapter(executor *workflow.WorkflowExecutor) *hubRuntimeExecutorAdapter {
	return &hubRuntimeExecutorAdapter{executor: executor}
}

// StartInstance marshals the runtime initiation fields into trigger data and
// delegates to the underlying 2-arg WorkflowExecutor.StartInstance.
func (a *hubRuntimeExecutorAdapter) StartInstance(ctx context.Context, workflowID, initiatorID string, formData map[string]interface{}, channel string) (*workflow.WorkflowInstance, error) {
	payload := map[string]interface{}{
		"form_data":            formData,
		"initiator_id":         initiatorID,
		"channel":              channel,
		"submission_timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Fall back to an initiator-only payload so the instance is still
		// bound to its initiator if form_data is somehow unmarshalable.
		raw, _ = json.Marshal(map[string]interface{}{"initiator_id": initiatorID})
	}
	return a.executor.StartInstance(ctx, workflowID, string(raw))
}

// hubNodeExecStoreAdapter bridges the InstanceStore.GetPendingApprovals method
// to the NodeExecutionStore.GetPendingApprovalsForUser method the
// DirectoryService depends on for the pending-action view. Both return the
// running approval node executions; the DirectoryService then enriches and
// filters them against the per-user instance data. This adapter exists only so
// the existing production InstanceStore (PgInstanceStore) can satisfy the
// DirectoryService dependency without changing either interface.
type hubNodeExecStoreAdapter struct {
	instanceStore workflow.InstanceStore
}

// Compile-time assertion that the adapter satisfies the NodeExecutionStore
// interface the DirectoryService depends on.
var _ workflow.NodeExecutionStore = (*hubNodeExecStoreAdapter)(nil)

// newHubNodeExecStoreAdapter wraps an InstanceStore so it can serve the
// DirectoryService's pending-action query.
func newHubNodeExecStoreAdapter(instanceStore workflow.InstanceStore) *hubNodeExecStoreAdapter {
	return &hubNodeExecStoreAdapter{instanceStore: instanceStore}
}

// GetPendingApprovalsForUser delegates to InstanceStore.GetPendingApprovals,
// which returns the running approval node executions.
func (a *hubNodeExecStoreAdapter) GetPendingApprovalsForUser(ctx context.Context, userID string) ([]workflow.NodeExecution, error) {
	return a.instanceStore.GetPendingApprovals(ctx, userID)
}
