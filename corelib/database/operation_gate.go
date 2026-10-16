package database

import (
	"context"
	"fmt"
	"strings"
)

// operationGateState is deliberately process-local. The approval/pending
// store remains the durable authorization record; this state only proves that
// the current operation observed a successful query before committing it.
type operationGateState struct {
	Fingerprint    string
	Attempt        int
	ParentActionID string
	Failed         bool
	Executed       bool
}

// SetStrictOperationGate enables the query-success lineage gate. It is off by
// default for compatibility with hosts that have not yet started injecting
// operation metadata. Enabling it makes committing execute calls require a
// matching successful query in the same RequestScope operation.
func (m *Manager) SetStrictOperationGate(enabled bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.strictOperationGate = enabled
		if m.operationStates == nil {
			m.operationStates = make(map[string]operationGateState)
		}
	}
	m.mu.Unlock()
}

// StrictOperationGateEnabled reports whether query-to-execute lineage checks
// are active.
func (m *Manager) StrictOperationGateEnabled() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.strictOperationGate
}

// SetStrictQuerySuccessGate is an expressive compatibility alias for hosts
// that refer to the gate by its query-success behavior.
func (m *Manager) SetStrictQuerySuccessGate(enabled bool) { m.SetStrictOperationGate(enabled) }

func (m *Manager) recordQueryOutcome(ctx context.Context, fingerprint string, err error) {
	scope := requestScopeFromContext(ctx)
	m.recordQueryOutcomeMeta(scope.OperationID, scope.Attempt, scope.ParentActionID, fingerprint, err)
}

func (m *Manager) recordQueryOutcomeMeta(operationID string, attempt int, parentActionID, fingerprint string, err error) {
	if m == nil {
		return
	}
	op := strings.TrimSpace(operationID)
	if op == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.strictOperationGate {
		return
	}
	if m.operationStates == nil {
		m.operationStates = make(map[string]operationGateState)
	}
	state := m.operationStates[op]
	if err != nil {
		state.Fingerprint = strings.TrimSpace(fingerprint)
		state.Attempt = attempt
		state.ParentActionID = strings.TrimSpace(parentActionID)
		state.Failed = true
		state.Executed = false
		m.operationStates[op] = state
		return
	}
	// A failed operation is terminal. A later query in the same operation may
	// be useful for diagnostics but cannot resurrect a write authorization.
	if state.Failed {
		return
	}
	if state.Executed {
		// An operation is idempotent: a later query cannot reopen a consumed
		// execute slot under the same operation ID.
		return
	}
	state.Fingerprint = strings.TrimSpace(fingerprint)
	state.Attempt = attempt
	state.ParentActionID = strings.TrimSpace(parentActionID)
	m.operationStates[op] = state
}

func (m *Manager) authorizeExecuteOperation(ctx context.Context, fingerprint string) error {
	if m == nil {
		return fmt.Errorf("permission: database manager unavailable")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.strictOperationGate {
		return nil
	}
	scope := requestScopeFromContext(ctx)
	op := strings.TrimSpace(scope.OperationID)
	if op == "" {
		return fmt.Errorf("permission: strict operation gate requires operation_id")
	}
	state, ok := m.operationStates[op]
	if !ok {
		return fmt.Errorf("permission: execute requires a successful query in the same operation")
	}
	if state.Failed {
		return fmt.Errorf("permission: operation was terminated by query_error")
	}
	if state.Fingerprint == "" || !strings.EqualFold(state.Fingerprint, fingerprint) {
		return fmt.Errorf("permission: execute fingerprint does not match the successful query")
	}
	if state.Attempt != 0 && scope.Attempt != 0 && state.Attempt != scope.Attempt {
		return fmt.Errorf("permission: execute attempt does not match the successful query")
	}
	if state.ParentActionID != "" && scope.ParentActionID != "" && state.ParentActionID != scope.ParentActionID {
		return fmt.Errorf("permission: execute parent_action_id does not match the successful query")
	}
	if state.Executed {
		return fmt.Errorf("permission: operation execute has already been consumed")
	}
	state.Executed = true
	m.operationStates[op] = state
	return nil
}

// terminateOperation is used when approval is rejected after a query. It
// prevents a caller from reusing the same operation lineage after a terminal
// decision.
func (m *Manager) terminateOperation(ctx context.Context, fingerprint string) {
	if m == nil {
		return
	}
	scope := requestScopeFromContext(ctx)
	op := strings.TrimSpace(scope.OperationID)
	if op == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.strictOperationGate {
		return
	}
	m.operationStates[op] = operationGateState{Fingerprint: strings.TrimSpace(fingerprint), Attempt: scope.Attempt, ParentActionID: scope.ParentActionID, Failed: true}
}
