package database

import (
	"context"
	"strings"
	"testing"
)

func TestStrictOperationGateRequiresSuccessfulMatchingQuery(t *testing.T) {
	m := NewManager(nil, nil)
	m.SetStrictOperationGate(true)
	ctx := WithRequestScope(context.Background(), RequestScope{OperationID: "op-1", Attempt: 2})
	fp := sqlFingerprint("select id from orders")
	if err := m.authorizeExecuteOperation(ctx, fp); err == nil {
		t.Fatal("execute was allowed before query")
	}
	m.recordQueryOutcome(ctx, fp, nil)
	if err := m.authorizeExecuteOperation(ctx, fp); err != nil {
		t.Fatalf("matching query did not authorize execute: %v", err)
	}
	if err := m.authorizeExecuteOperation(ctx, fp); err == nil || !strings.Contains(err.Error(), "already been consumed") {
		t.Fatalf("duplicate execute was not rejected: %v", err)
	}
}

func TestStrictOperationGateQueryErrorTerminatesOperation(t *testing.T) {
	m := NewManager(nil, nil)
	m.SetStrictOperationGate(true)
	ctx := WithRequestScope(context.Background(), RequestScope{OperationID: "op-failed", Attempt: 1})
	fp := sqlFingerprint("select id from orders")
	m.recordQueryOutcome(ctx, fp, context.Canceled)
	if err := m.authorizeExecuteOperation(ctx, fp); err == nil || !strings.Contains(err.Error(), "query_error") {
		t.Fatalf("query_error did not terminate operation: %v", err)
	}
	// A later query in the same operation cannot resurrect the terminal state.
	m.recordQueryOutcome(ctx, fp, nil)
	if err := m.authorizeExecuteOperation(ctx, fp); err == nil {
		t.Fatal("successful retry resurrected a failed operation")
	}
}

func TestRequestLineageFieldsAreCopiedToApprovalAndAudit(t *testing.T) {
	ctx := WithRequestScope(context.Background(), RequestScope{
		OwnerID: " owner ", SessionID: " session ", OperationID: " op ", Attempt: 3, ParentActionID: " parent ",
	})
	scope := RequestScopeFrom(ctx)
	if scope.OwnerID != "owner" || scope.SessionID != "session" || scope.OperationID != "op" || scope.Attempt != 3 || scope.ParentActionID != "parent" {
		t.Fatalf("scope lineage was not normalized: %+v", scope)
	}
	event := auditFromArgs(map[string]interface{}{"_owner_id": "owner", "_session_id": "session", "_operation_id": "op", "_attempt": 3, "_parent_action_id": "parent"}, AuditEvent{Action: "query"})
	if event.OperationID != "op" || event.Attempt != 3 || event.ParentActionID != "parent" {
		t.Fatalf("audit lineage missing: %+v", event)
	}
}
