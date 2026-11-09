package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The export turn queried employees through the legacy database_query overlay,
// then wrote and delivered employees.xlsx. business.data.read stayed pending
// because that overlay never touches the planned selection, and the turn
// hard-exited required_effect_unsettled after the file was already exported.
func TestLegacySQLReadSettlesRequiredBusinessDataRead(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "read", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "read-repeat", NeedID: "need:business.data.read:employees#02", Optional: true, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "office", NeedID: "need:document.write.office:sheet", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, userText: "导出excel"}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("open read and office should keep the turn unfinished")
	}

	cb.noteLegacySQLDataRead("database_query", `{"action":"connect","profile_id":"mysql-192-168-1-242"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: `{"ok":true,"connection_id":"cf4"}`,
	})
	if surface.completed["read"] {
		t.Fatal("connect is not the data read")
	}

	cb.noteLegacySQLDataRead("database", `{"action":"export_excel","file_path":"employees.xlsx"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: "database export_excel failed: permission: operation is not allowed by profile",
	})
	if surface.completed["read"] {
		t.Fatal("a refused export settled the read")
	}

	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT * FROM rapidbi.employees"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeError, Result: `{"error":"profile_not_found","ok":false}`,
	})
	if surface.completed["read"] {
		t.Fatal("a failed query settled the read")
	}

	cb.noteLegacySQLDataRead("database_query", `{"action":"inspect"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: `{"columns":[{"name":"employeeid"}]}`,
	})
	if surface.completed["read"] {
		t.Fatal("inspect is not the table read")
	}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT 1"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: "[json_preview]\nkeys=1",
	})
	if surface.completed["read"] {
		t.Fatal("a preview without a row count settled the read")
	}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT 1"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: `{"row_count":null}`,
	})
	if surface.completed["read"] {
		t.Fatal("a null row count settled the read")
	}

	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT * FROM rapidbi.employees"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK,
		Result:  "[json_preview]\nrow_count: 9\n[raw_head]\n{\"contract_version\":1,\"columns\":[{\"name\":\"notes\"}],\"rows\":[[\"the rollout failed: retry\"]],\"row_count\":9}",
	})
	if !surface.completed["read"] || surface.completed["read-repeat"] || surface.completed["office"] {
		t.Fatalf("query settled the wrong steps: %#v", surface.completed)
	}

	// A petition after the query publishes a new plan. The read fact belongs
	// to the turn, so the replacement still closes its required read.
	replacement, _ := newHopRunTestSurface(t)
	replacement.plan = surface.plan
	replacement.plan.ID = surface.plan.ID + "-child"
	replacement.completed = map[string]bool{}
	cb.semanticSurface = replacement
	if snap := cb.hopRunSnapshot(); snap == nil || !replacement.completed["read"] || replacement.completed["office"] {
		t.Fatalf("replacement plan lost the SQL read: %#v", replacement.completed)
	}

	replacement.completed["office"] = true
	if cb.RequiredEffectUnsettled() {
		t.Fatal("exported workbook still left business.data.read unsettled")
	}
	resp := &IMAgentResponse{
		LocalFilePath: `C:\work\employees.xlsx`,
		FileName:      "employees.xlsx",
		Error:         "required_effect_unsettled",
		HardExit:      true,
	}
	cb.clearRecoveredRequiredEffect(resp)
	if resp.Error != "" || resp.HardExit {
		t.Fatalf("delivered workbook kept the turn open: error=%q hard_exit=%v", resp.Error, resp.HardExit)
	}
}

// One returned payload closes one required read. The next snapshot must not
// spend that same payload on another required read. A read the plan already
// completed does not license a second one either.
func TestLegacySQLReadSpendsOneRequiredSelection(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "employees", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "orders", NeedID: "need:business.data.read:orders", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	surface.completed = map[string]bool{"employees": true}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, userText: "导出excel"}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: `{"row_count":0}`,
	})
	if surface.completed["orders"] {
		t.Fatal("a query closed a second read after the plan had already completed one")
	}

	fresh, _ := newHopRunTestSurface(t)
	fresh.plan.ObligationsRecorded = true
	fresh.plan.Selections = []tool.PlannedSelection{
		{ID: "employees", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "orders", NeedID: "need:business.data.read:orders", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	fresh.completed = map[string]bool{}
	cb = &sharedAgentLoopCallbacks{semanticSurface: fresh, userText: "导出excel"}
	payload := agent.ToolExecutionResult{Outcome: agent.ToolExecutionOutcomeOK, Result: `{"row_count":9}`}
	cb.noteLegacySQLDataRead("database_query", `{"action":"read_table","table":"employees"}`, payload)
	if !fresh.completed["employees"] || fresh.completed["orders"] {
		t.Fatalf("first read settled %#v", fresh.completed)
	}
	if snap := cb.hopRunSnapshot(); snap == nil || fresh.completed["orders"] {
		t.Fatal("the next snapshot spent the same read on orders")
	}
	cb.noteLegacySQLDataRead("database", `{"action":"query","sql":"SELECT * FROM orders"}`, payload)
	if !fresh.completed["orders"] {
		t.Fatal("the second read did not close orders")
	}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","cursor":"page-2","limit":100}`, payload)
	if snap := cb.hopRunSnapshot(); snap == nil || !fresh.completed["orders"] {
		t.Fatal("a later page reopened orders")
	}
}

// Paging a query is the same read. It must not close the next required
// selection. A turn that only continues a cursor still closes one.
func TestLegacySQLCursorPageSpendsNoExtraRead(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "employees", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "orders", NeedID: "need:business.data.read:orders", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, userText: "导出excel"}
	page := agent.ToolExecutionResult{Outcome: agent.ToolExecutionOutcomeOK, Result: `{"row_count":100,"truncated":true}`}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT * FROM employees","limit":100}`, page)
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","cursor":"employees-page-2","limit":100}`, page)
	if !surface.completed["employees"] || surface.completed["orders"] {
		t.Fatalf("cursor page settled %#v", surface.completed)
	}
	if snap := cb.hopRunSnapshot(); snap == nil || surface.completed["orders"] {
		t.Fatal("the next snapshot spent the cursor page on orders")
	}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","cursor":null,"sql":"SELECT * FROM orders"}`, page)
	if !surface.completed["orders"] {
		t.Fatal("null cursor dropped a query that returned rows")
	}

	onlyPage, _ := newHopRunTestSurface(t)
	onlyPage.plan.ObligationsRecorded = true
	onlyPage.plan.Selections = []tool.PlannedSelection{
		{ID: "employees", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "orders", NeedID: "need:business.data.read:orders", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	onlyPage.completed = map[string]bool{}
	cb = &sharedAgentLoopCallbacks{semanticSurface: onlyPage, userText: "导出excel"}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","cursor":"employees-page-2"}`, page)
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","cursor":"orders-page-2"}`, page)
	if !onlyPage.completed["employees"] || onlyPage.completed["orders"] {
		t.Fatalf("cursor-only turn settled %#v", onlyPage.completed)
	}
}

// A failed execution stays failed. The query closes the still-open read
// instead of painting over the failure.
func TestLegacySQLReadLeavesFailedSelection(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "employees", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
		{ID: "orders", NeedID: "need:business.data.read:orders", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	surface.completed = map[string]bool{}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "employees"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "employees", tool.PlanExecutionFailed, "", "query_error", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, userText: "导出excel"}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT * FROM orders"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK, Result: `{"row_count":4}`,
	})
	if surface.completed["employees"] {
		t.Fatal("query overwrote a failed read")
	}
	if !surface.completed["orders"] {
		t.Fatal("query did not close the still-open read")
	}
	if snap := cb.hopRunSnapshot(); snap == nil || surface.completed["employees"] {
		t.Fatal("the next snapshot overwrote the failed read")
	}
}

// A successful query is one JSON line. The text classifier treats "失败:"
// anywhere on that line as a failed tool, including a cell. The read still
// happened. A top-level ok:false did not.
func TestLegacySQLReadSettlesWhenClassifierRejectsCellText(t *testing.T) {
	body := `{"contract_version":1,"columns":[{"name":"notes"}],"rows":[[{"ok":false},"保存失败: retry"]],"row_count":9}`
	raw := agentruntime.ToolTextResult(body)
	if raw.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("classifier outcome = %q, want error so this test covers the wording trap", raw.Outcome)
	}
	if got := classifySharedLoopToolResult("database_query", body); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("returned rows were a failed tool: %s", got.Outcome)
	}
	if got := classifySharedLoopToolResult("database_query", "[system rejected] database_query\nrow_count: 9"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("system rejection outcome = %s", got.Outcome)
	}
	surface, _ := newHopRunTestSurface(t)
	surface.plan.ObligationsRecorded = true
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "read", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, userText: "导出excel"}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query","sql":"SELECT * FROM rapidbi.employees"}`, raw)
	if !surface.completed["read"] {
		t.Fatal("cell text 失败: hid a query that returned rows")
	}
	snap := cb.hopRunSnapshot()
	if snap == nil || len(snap.Steps) == 0 || snap.Steps[0].Status != HopStepCompleted {
		t.Fatalf("settled read was not a completed hop step: %#v", snap)
	}

	cb.noteLegacySQLDataRead("database_query", `{"action":"query"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeError,
		Result:  `{"ok":false,"error":"profile_not_found","row_count":9}`,
	})
	if snap := cb.hopRunSnapshot(); snap == nil || snap.Remaining != 0 {
		t.Fatalf("a second failure reopened the read: %#v", snap)
	}
	fresh, _ := newHopRunTestSurface(t)
	fresh.plan.ObligationsRecorded = true
	fresh.plan.Selections = []tool.PlannedSelection{
		{ID: "read", NeedID: "need:business.data.read:employees", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityBusinessDataRead}},
	}
	fresh.completed = map[string]bool{}
	cb = &sharedAgentLoopCallbacks{semanticSurface: fresh, userText: "导出excel"}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK,
		Result:  `{"ok":false,"error":"profile_not_found","row_count":9}`,
	})
	if fresh.completed["read"] {
		t.Fatal("top-level ok:false settled the read")
	}
	cb.noteLegacySQLDataRead("database_query", `{"action":"query"}`, agent.ToolExecutionResult{
		Outcome: agent.ToolExecutionOutcomeOK,
		Result:  `{"meta":{"row_count":null},"columns":[{"name":"n"}],"row_count":2}`,
	})
	if !fresh.completed["read"] {
		t.Fatal("a nested null row_count hid the real count")
	}
}
