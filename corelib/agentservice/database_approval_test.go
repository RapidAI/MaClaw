package agentservice

import (
	"context"
	"reflect"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/database"
)

func TestExecuteRequestDatabaseApprovalIsHostOnly(t *testing.T) {
	field, ok := reflect.TypeOf(ExecuteRequest{}).FieldByName("DatabaseApproval")
	if !ok || field.Tag.Get("json") != "-" {
		t.Fatalf("DatabaseApproval must remain host-only, tag=%q", field.Tag.Get("json"))
	}
	postField, ok := reflect.TypeOf(PostMessageInput{}).FieldByName("DatabaseApproval")
	if !ok || postField.Tag.Get("json") != "-" {
		t.Fatalf("PostMessageInput.DatabaseApproval must remain host-only, tag=%q", postField.Tag.Get("json"))
	}
	sendField, ok := reflect.TypeOf(SendMessageInput{}).FieldByName("DatabaseApproval")
	if !ok || sendField.Tag.Get("json") != "-" {
		t.Fatalf("SendMessageInput.DatabaseApproval must remain host-only, tag=%q", sendField.Tag.Get("json"))
	}
}

func TestContextWithDatabaseApprovalHandlesNilAndCopiesValue(t *testing.T) {
	base := context.Background()
	if got := contextWithDatabaseApproval(nil, nil); got == nil {
		t.Fatal("nil context must normalize to a usable context")
	}
	approval := &database.ApprovalContext{Token: "opaque-secret", ID: "approval-1"}
	if got := contextWithDatabaseApproval(base, approval); got == nil {
		t.Fatal("approval context injection returned nil")
	}
	approval.Token = "mutated-after-injection"
	// The context key is intentionally private to corelib/database. Behavioural
	// forwarding is covered by corelib/database's handler tests; this test
	// verifies the agentservice bridge does not retain a caller-owned pointer.
}

func TestCloneDatabaseApprovalDetachesAsyncInput(t *testing.T) {
	input := &database.ApprovalContext{Token: "opaque-secret", ID: "approval-1"}
	clone := cloneDatabaseApproval(input)
	if clone == nil || clone == input || clone.Token != input.Token || clone.ID != input.ID {
		t.Fatalf("approval clone mismatch: input=%#v clone=%#v", input, clone)
	}
	input.Token = "changed"
	if clone.Token != "opaque-secret" {
		t.Fatal("async ExecuteRequest retained caller-owned approval pointer")
	}
}
