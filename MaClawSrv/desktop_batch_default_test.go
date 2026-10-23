package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestBatchDefaultRefusesSecondSingle(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_batch_def_1"}
	desktopRunFor(scope) // init
	_ = desktopBatchDefaultCheck(context.Background(), scope, "click")
	desktopRecordBatchDefault(scope, "click", nil)
	err := desktopBatchDefaultCheck(context.Background(), scope, "type")
	if err == nil || !strings.Contains(err.Error(), "batch default") {
		t.Fatalf("want batch default refuse, got %v", err)
	}
	// Inside a plan, singles are fine.
	ctx := context.WithValue(context.Background(), desktopInsideBatchKey{}, true)
	if err := desktopBatchDefaultCheck(ctx, scope, "type"); err != nil {
		t.Fatalf("inside batch: %v", err)
	}
	// After batch started, singles are fine again (fix a failed step).
	desktopMarkBatchStarted(scope)
	if err := desktopBatchDefaultCheck(context.Background(), scope, "type"); err != nil {
		t.Fatalf("after batch: %v", err)
	}
}

func TestBatchDefaultAllowsFirstSingleAndProbe(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_batch_def_2"}
	if err := desktopBatchDefaultCheck(context.Background(), scope, "probe"); err != nil {
		t.Fatal(err)
	}
	if err := desktopBatchDefaultCheck(context.Background(), scope, "rich_fill"); err != nil {
		t.Fatal(err)
	}
}
