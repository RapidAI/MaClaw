package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestPermissionHandler_AutoApproveMode verifies auto-approve mode still works.
func TestPermissionHandler_AutoApproveMode(t *testing.T) {
	var completed []PermissionCompletion
	h := NewPermissionHandler(PermissionModeAutoApprove, nil, func(c PermissionCompletion) {
		completed = append(completed, c)
	})
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Bash",
		Input:     map[string]interface{}{"command": "rm -rf /"},
	})

	if comp.Decision != PermissionApproved {
		t.Fatalf("expected approved, got %s", comp.Decision)
	}
	if comp.Reason != "auto-approved by permission mode" {
		t.Fatalf("unexpected reason: %s", comp.Reason)
	}
}

// TestPermissionHandler_ReadOnlyMode verifies read-only mode denies writes.
func TestPermissionHandler_ReadOnlyMode(t *testing.T) {
	h := NewPermissionHandler(PermissionModeReadOnly, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Bash",
	})
	if comp.Decision != PermissionDenied {
		t.Fatalf("expected denied for write tool in read-only, got %s", comp.Decision)
	}
}

// TestPermissionHandler_DefaultMode_LowRisk verifies low-risk ops are auto-approved.
func TestPermissionHandler_DefaultMode_LowRisk(t *testing.T) {
	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Read", // read-only tool → low risk → PolicyAllow
	})
	if comp.Decision != PermissionApproved {
		t.Fatalf("expected approved for low-risk read, got %s: %s", comp.Decision, comp.Reason)
	}
}

// TestPermissionHandler_DefaultMode_MediumRisk_Audit verifies medium-risk ops get audit policy.
func TestPermissionHandler_DefaultMode_MediumRisk_Audit(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, al)

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Write", // write tool → medium risk → PolicyAudit
		Input:     map[string]interface{}{"path": "/tmp/test.txt"},
	})
	if comp.Decision != PermissionApproved {
		t.Fatalf("expected approved (audit) for medium-risk write, got %s: %s", comp.Decision, comp.Reason)
	}

	// Verify audit log was written.
	entries, err := al.Query(AuditFilter{ToolName: "Write"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].RiskLevel != RiskMedium {
		t.Fatalf("expected medium risk in audit, got %s", entries[0].RiskLevel)
	}
}

// TestPermissionHandler_DefaultMode_CriticalRisk_Deny verifies critical+dangerous-keyword ops are denied.
func TestPermissionHandler_DefaultMode_CriticalRisk_Deny(t *testing.T) {
	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Bash",
		Input:     map[string]interface{}{"command": "sudo rm -rf /"},
	})
	if comp.Decision != PermissionDenied {
		t.Fatalf("expected denied for critical+dangerous, got %s: %s", comp.Decision, comp.Reason)
	}
}

// TestPermissionHandler_DefaultMode_HighRisk_Ask verifies high-risk ops create pending requests.
func TestPermissionHandler_DefaultMode_HighRisk_Ask(t *testing.T) {
	var requestReceived bool
	h := NewPermissionHandler(PermissionModeDefault, func(req PermissionRequest) {
		requestReceived = true
	}, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)
	h.SetProjectPath("/etc/config")

	var wg sync.WaitGroup
	wg.Add(1)
	var comp PermissionCompletion
	go func() {
		defer wg.Done()
		comp = h.HandleRequest(PermissionRequest{
			RequestID: "r1",
			SessionID: "s1",
			ToolName:  "Write", // write + system dir → high risk → PolicyAsk
			Input:     map[string]interface{}{"path": "/etc/config/test"},
		})
	}()

	// Wait for the pending request to appear.
	time.Sleep(50 * time.Millisecond)
	if !requestReceived {
		t.Fatal("expected onRequest callback to be called")
	}
	if h.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", h.PendingCount())
	}

	// Resolve it.
	if err := h.Resolve("r1", PermissionApproved, "user approved"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	if comp.Decision != PermissionApproved {
		t.Fatalf("expected approved after resolve, got %s", comp.Decision)
	}
}

// TestPermissionHandler_NoSecurityChain verifies default mode without security chain
// falls through to pending request (backward compatible).
func TestPermissionHandler_NoSecurityChain(t *testing.T) {
	var requestReceived bool
	h := NewPermissionHandler(PermissionModeDefault, func(req PermissionRequest) {
		requestReceived = true
	}, nil)
	// No SetSecurityChain call — fields remain nil.

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.HandleRequest(PermissionRequest{
			RequestID: "r1",
			SessionID: "s1",
			ToolName:  "Read",
		})
	}()

	time.Sleep(50 * time.Millisecond)
	if !requestReceived {
		t.Fatal("expected pending request without security chain")
	}
	_ = h.Resolve("r1", PermissionApproved, "ok")
	wg.Wait()
}

// TestPermissionHandler_CallCountTracking verifies call count increments per session+tool.
func TestPermissionHandler_CallCountTracking(t *testing.T) {
	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	// Call a read tool multiple times — should all be low risk / auto-approved.
	for i := 0; i < 5; i++ {
		comp := h.HandleRequest(PermissionRequest{
			RequestID: "r" + string(rune('0'+i)),
			SessionID: "s1",
			ToolName:  "Read",
		})
		if comp.Decision != PermissionApproved {
			t.Fatalf("call %d: expected approved, got %s", i, comp.Decision)
		}
	}

	// Verify internal call count.
	h.mu.Lock()
	count := h.callCount["s1:Read"]
	h.mu.Unlock()
	if count != 5 {
		t.Fatalf("expected callCount=5, got %d", count)
	}
}

// TestPermissionHandler_Reset_ClearsCallCount verifies Reset clears call counts.
func TestPermissionHandler_Reset_ClearsCallCount(t *testing.T) {
	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, nil)

	h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Read",
	})

	h.Reset()

	h.mu.Lock()
	count := h.callCount["s1:Read"]
	h.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected callCount=0 after reset, got %d", count)
	}
}

// TestExtractArgs verifies the extractArgs helper.
func TestExtractArgs(t *testing.T) {
	// nil input
	if args := extractArgs(nil); args != nil {
		t.Fatalf("expected nil, got %v", args)
	}

	// map input
	m := map[string]interface{}{"key": "val"}
	if args := extractArgs(m); args["key"] != "val" {
		t.Fatalf("expected map passthrough, got %v", args)
	}

	// string input
	args := extractArgs("hello")
	if args["input"] != "hello" {
		t.Fatalf("expected wrapped string, got %v", args)
	}
}

// TestPermissionHandler_SessionRulesStillWork verifies session-level approvals bypass security chain.
func TestPermissionHandler_SessionRulesStillWork(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	var requestReceived bool
	h := NewPermissionHandler(PermissionModeDefault, func(req PermissionRequest) {
		requestReceived = true
	}, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, al)

	// Simulate a session-level approval for "Write".
	h.mu.Lock()
	h.sessionRules["Write"] = true
	h.mu.Unlock()

	comp := h.HandleRequest(PermissionRequest{
		RequestID: "r1",
		SessionID: "s1",
		ToolName:  "Write",
		Input:     map[string]interface{}{"path": "/tmp/test"},
	})

	if comp.Decision != PermissionApproved {
		t.Fatalf("expected approved via session rule, got %s", comp.Decision)
	}
	if requestReceived {
		t.Fatal("should not have created pending request for session-approved tool")
	}
}

// TestPermissionHandler_AuditLogIntegration verifies audit entries are written for security chain decisions.
func TestPermissionHandler_AuditLogIntegration(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	h := NewPermissionHandler(PermissionModeDefault, nil, nil)
	h.SetSecurityChain(&RiskAssessor{}, NewPolicyEngine(), nil, al)

	// Low risk → allow
	h.HandleRequest(PermissionRequest{
		RequestID: "r1", SessionID: "s1", ToolName: "Read",
	})
	// Medium risk → audit
	h.HandleRequest(PermissionRequest{
		RequestID: "r2", SessionID: "s1", ToolName: "Write",
		Input: map[string]interface{}{"path": "/tmp/x"},
	})

	entries, _ := al.Query(AuditFilter{})
	if len(entries) != 2 {
		// List files in dir for debugging.
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			t.Logf("  file: %s", f.Name())
		}
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
}
