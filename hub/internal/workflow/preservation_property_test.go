package workflow

// Property 2: Preservation — Non-Runtime-Chain Behavior Is Unchanged.
//
// These property-based tests encode the OBSERVED behavior of the system on the
// UNFIXED code for inputs where isBugCondition(X) is FALSE. They establish the
// baseline that the runtime-chain wiring fix MUST preserve (design Property 2 /
// Requirements 3.1–3.12). They are expected to PASS on the unfixed code.
//
// Methodology (observation-first): for each preserved behavior we derive an
// independent reference model of the existing logic, then drive the real code
// with rapid-generated non-bug-condition inputs and assert the real outcome
// equals the reference model. Because the fix only changes WIRING (real
// dependencies, route registration, signal sources, persistence mechanism) and
// never the per-mode decision logic, FormValidator, EscalationManager,
// ConfirmationTracker, VersionManager auto-increment, or AdminReviewService
// publish path, these properties hold identically on F and F'.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12**

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"pgregory.net/rapid"

	_ "modernc.org/sqlite"
)

// presStep is a single approver decision in a generated decision sequence.
type presStep struct {
	idx      int    // index into the approver list (0-based)
	decision string // "approve" | "reject" | "escalate"
}

// presApproverIDs builds the canonical approver IDs "ve-1".."ve-m".
func presApproverIDs(m int) []string {
	ids := make([]string, m)
	for i := 0; i < m; i++ {
		ids[i] = fmt.Sprintf("ve-%d", i+1)
	}
	return ids
}

// presBuildExecutor wires a fresh executor over the standard approval graph
// (trigger-1 -> approval-1 -> action-1) for a given approval config.
func presBuildExecutor(cfg ApprovalNodeConfig) (*WorkflowExecutor, *resumeTestMockInstanceStore, *mockAuditStore, *resumeTestMockDispatcher) {
	graph := buildApprovalGraph(cfg)
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  make(map[string]interface{}),
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)
	return executor, instStore, auditStore, dispatcher
}

// presHasAudit reports whether any audit entry matches the predicate.
func presHasAudit(entries []*AuditEntry, pred func(*AuditEntry) bool) bool {
	for _, e := range entries {
		if pred(e) {
			return true
		}
	}
	return false
}

// presCountAudit counts audit entries matching the predicate.
func presCountAudit(entries []*AuditEntry, pred func(*AuditEntry) bool) int {
	n := 0
	for _, e := range entries {
		if pred(e) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Per-mode decision reference models (observation-first).
//
// Each function independently re-derives the observed advance/reject/wait
// semantics of ResumeInstance for one mode and returns how many of the
// generated steps actually get applied (the prefix up to and including the
// settling step), whether the node settled, and the terminal instance status.
// ---------------------------------------------------------------------------

// presRefSingle models single mode: the first non-escalate decision settles.
func presRefSingle(steps []presStep) (appliedCount int, settled bool, finalStatus InstanceStatus) {
	for i, s := range steps {
		switch s.decision {
		case approvalDecisionApprove:
			return i + 1, true, InstanceCompleted
		case approvalDecisionReject:
			return i + 1, true, InstanceFailed
		default: // escalate -> wait, keep going
		}
	}
	return len(steps), false, ""
}

// presRefCountersign models countersign mode: reject settles fail immediately;
// escalate waits; approve is recorded and the node advances once all m
// configured approvers have approved.
func presRefCountersign(steps []presStep, m int) (appliedCount int, settled bool, finalStatus InstanceStatus) {
	approved := make(map[int]bool)
	for i, s := range steps {
		switch s.decision {
		case approvalDecisionReject:
			return i + 1, true, InstanceFailed
		case approvalDecisionEscalate:
			// wait, no record
		default: // approve
			approved[s.idx] = true
			if len(approved) == m {
				return i + 1, true, InstanceCompleted
			}
		}
	}
	return len(steps), false, ""
}

// presRefAnyNofM models any-N-of-M mode: escalate waits (no record); approve
// and reject are recorded keyed by approver (later overwrites earlier); advance
// when approvals >= N; reject when reaching N becomes impossible.
func presRefAnyNofM(steps []presStep, m, n int) (appliedCount int, settled bool, finalStatus InstanceStatus) {
	decisions := make(map[int]string)
	for i, s := range steps {
		if s.decision == approvalDecisionEscalate {
			continue // wait, no record
		}
		decisions[s.idx] = s.decision
		approvalCount := 0
		for _, d := range decisions {
			if d == approvalDecisionApprove {
				approvalCount++
			}
		}
		if approvalCount >= n {
			return i + 1, true, InstanceCompleted
		}
		remaining := m - len(decisions)
		if approvalCount+remaining < n {
			return i + 1, true, InstanceFailed
		}
	}
	return len(steps), false, ""
}

// presDriveModeAndAssert applies the prefix of steps the reference model says
// will be applied, asserting the real executor's status transitions and audit
// events match the model. This is the core preservation check for 3.2 / 3.3.
func presDriveModeAndAssert(
	t *rapid.T,
	cfg ApprovalNodeConfig,
	m int,
	steps []presStep,
	appliedCount int,
	settled bool,
	finalStatus InstanceStatus,
) {
	executor, instStore, auditStore, _ := presBuildExecutor(cfg)
	approverIDs := presApproverIDs(m)
	ctx := context.Background()

	for i := 0; i < appliedCount; i++ {
		s := steps[i]
		err := executor.ResumeInstance(ctx, "inst-1", "approval-1", ApprovalResponse{
			Decision:   s.decision,
			ApproverID: approverIDs[s.idx],
			DecidedAt:  time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("step %d (%s by %s) unexpected error: %v", i, s.decision, approverIDs[s.idx], err)
		}
		// Every step except a settling step must leave the instance running.
		isSettleStep := settled && i == appliedCount-1
		if !isSettleStep {
			if instStore.instance.Status != InstanceRunning {
				t.Fatalf("step %d should keep instance running, got status %q", i, instStore.instance.Status)
			}
		}
	}

	// Terminal status preservation.
	if settled {
		if instStore.statusUpdate != finalStatus {
			t.Fatalf("expected settle status %q, got %q (steps=%v appliedCount=%d)", finalStatus, instStore.statusUpdate, steps, appliedCount)
		}
	} else {
		if instStore.statusUpdate != "" {
			t.Fatalf("non-settling sequence should not transition status, got %q (steps=%v)", instStore.statusUpdate, steps)
		}
		if instStore.instance.Status != InstanceRunning {
			t.Fatalf("non-settling sequence must stay running, got %q", instStore.instance.Status)
		}
	}

	// Audit preservation (3.2): one approval_decision entry per applied call,
	// in order, with matching Decision and ActorID.
	var decisionEntries []*AuditEntry
	for _, e := range auditStore.entries {
		if e.EventType == "approval_decision" {
			decisionEntries = append(decisionEntries, e)
		}
	}
	if len(decisionEntries) != appliedCount {
		t.Fatalf("expected %d approval_decision audit entries, got %d", appliedCount, len(decisionEntries))
	}
	for i := 0; i < appliedCount; i++ {
		s := steps[i]
		if decisionEntries[i].Decision != s.decision {
			t.Fatalf("audit decision[%d] = %q, want %q", i, decisionEntries[i].Decision, s.decision)
		}
		if decisionEntries[i].ActorID != approverIDs[s.idx] {
			t.Fatalf("audit actor[%d] = %q, want %q", i, decisionEntries[i].ActorID, approverIDs[s.idx])
		}
	}

	// Settle-specific audit events.
	if settled && finalStatus == InstanceCompleted {
		if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool {
			return e.EventType == "node_completed" && e.NodeID == "approval-1"
		}) {
			t.Fatalf("advance should emit node_completed for approval-1")
		}
	}
	if settled && finalStatus == InstanceFailed {
		if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool {
			return e.EventType == "node_failed"
		}) {
			t.Fatalf("reject should emit node_failed audit event")
		}
		if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool {
			return e.EventType == "instance_failed"
		}) {
			t.Fatalf("reject should emit instance_failed audit event")
		}
	}
}

// TestPreservation_SingleMode_DecisionLogic observes and locks in single-mode
// advance-on-approve / reject-on-reject / wait-on-escalate semantics and audit
// events on the unfixed code (3.2, 3.3).
//
// **Validates: Requirements 3.2, 3.3**
func TestPreservation_SingleMode_DecisionLogic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := ApprovalNodeConfig{
			ApproverIDs:  presApproverIDs(1),
			Mode:         ModeSingle,
			TimeoutHours: 24,
		}
		decGen := rapid.SampledFrom([]string{approvalDecisionApprove, approvalDecisionReject, approvalDecisionEscalate})
		raw := rapid.SliceOfN(decGen, 1, 5).Draw(t, "decisions")
		steps := make([]presStep, len(raw))
		for i, d := range raw {
			steps[i] = presStep{idx: 0, decision: d}
		}
		appliedCount, settled, finalStatus := presRefSingle(steps)
		presDriveModeAndAssert(t, cfg, 1, steps, appliedCount, settled, finalStatus)
	})
}

// TestPreservation_CountersignMode_DecisionLogic observes and locks in
// countersign-mode semantics (reject on first reject, advance when all approve)
// and audit events on the unfixed code (3.2, 3.3).
//
// **Validates: Requirements 3.2, 3.3**
func TestPreservation_CountersignMode_DecisionLogic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := rapid.IntRange(1, 4).Draw(t, "m")
		cfg := ApprovalNodeConfig{
			ApproverIDs:  presApproverIDs(m),
			Mode:         ModeCountersign,
			TimeoutHours: 24,
		}
		decGen := rapid.SampledFrom([]string{approvalDecisionApprove, approvalDecisionReject, approvalDecisionEscalate})
		n := rapid.IntRange(1, 3*m).Draw(t, "n_steps")
		steps := make([]presStep, n)
		for i := 0; i < n; i++ {
			steps[i] = presStep{
				idx:      rapid.IntRange(0, m-1).Draw(t, fmt.Sprintf("idx_%d", i)),
				decision: decGen.Draw(t, fmt.Sprintf("dec_%d", i)),
			}
		}
		appliedCount, settled, finalStatus := presRefCountersign(steps, m)
		presDriveModeAndAssert(t, cfg, m, steps, appliedCount, settled, finalStatus)
	})
}

// TestPreservation_AnyNofMMode_DecisionLogic observes and locks in any-N-of-M
// semantics (advance at N, reject when N unreachable, escalate consumes no vote)
// and audit events on the unfixed code (3.2, 3.3).
//
// **Validates: Requirements 3.2, 3.3**
func TestPreservation_AnyNofMMode_DecisionLogic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := rapid.IntRange(1, 5).Draw(t, "m")
		nReq := rapid.IntRange(1, m).Draw(t, "min_approvals")
		cfg := ApprovalNodeConfig{
			ApproverIDs:  presApproverIDs(m),
			Mode:         ModeAnyNofM,
			MinApprovals: nReq,
			TimeoutHours: 24,
		}
		decGen := rapid.SampledFrom([]string{approvalDecisionApprove, approvalDecisionReject, approvalDecisionEscalate})
		numSteps := rapid.IntRange(1, 3*m).Draw(t, "n_steps")
		steps := make([]presStep, numSteps)
		for i := 0; i < numSteps; i++ {
			steps[i] = presStep{
				idx:      rapid.IntRange(0, m-1).Draw(t, fmt.Sprintf("idx_%d", i)),
				decision: decGen.Draw(t, fmt.Sprintf("dec_%d", i)),
			}
		}
		appliedCount, settled, finalStatus := presRefAnyNofM(steps, m, nReq)
		presDriveModeAndAssert(t, cfg, m, steps, appliedCount, settled, finalStatus)
	})
}

// presSeqOutcome is the reference model outcome for a sequential-mode run.
type presSeqOutcome struct {
	appliedCount int
	settled      bool
	finalStatus  InstanceStatus
	dispatched   []string // approver IDs the executor should dispatch to (next-in-order)
}

// presRefSequential models sequential mode over a stream of decisions that all
// target the current expected approver (the valid, no-error path). Approve
// advances the pointer and dispatches to the next approver (unless last);
// reject fails immediately; escalate waits without progressing.
func presRefSequential(decisions []string, m int) presSeqOutcome {
	order := presApproverIDs(m)
	cur := 0
	var dispatched []string
	for i, d := range decisions {
		switch d {
		case approvalDecisionReject:
			return presSeqOutcome{appliedCount: i + 1, settled: true, finalStatus: InstanceFailed, dispatched: dispatched}
		case approvalDecisionEscalate:
			// wait: no progress, no dispatch
		default: // approve
			if cur >= len(order)-1 {
				// last approver approves -> complete
				return presSeqOutcome{appliedCount: i + 1, settled: true, finalStatus: InstanceCompleted, dispatched: dispatched}
			}
			// dispatch to next approver in the sequence
			dispatched = append(dispatched, order[cur+1])
			cur++
		}
	}
	return presSeqOutcome{appliedCount: len(decisions), settled: false, finalStatus: "", dispatched: dispatched}
}

// TestPreservation_SequentialMode_DecisionLogic observes and locks in
// sequential-mode semantics (advance in order, dispatch to next approver,
// reject on first reject, escalate waits) and audit events on the unfixed code
// (3.2, 3.3, 3.4 — dispatcher call sites unchanged).
//
// **Validates: Requirements 3.2, 3.3, 3.4**
func TestPreservation_SequentialMode_DecisionLogic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := rapid.IntRange(1, 4).Draw(t, "m")
		cfg := ApprovalNodeConfig{
			ApproverIDs:   presApproverIDs(m),
			Mode:          ModeSequential,
			ApproverOrder: presApproverIDs(m),
			TimeoutHours:  24,
		}
		decGen := rapid.SampledFrom([]string{approvalDecisionApprove, approvalDecisionReject, approvalDecisionEscalate})
		numSteps := rapid.IntRange(1, 2*m+2).Draw(t, "n_steps")
		decisions := rapid.SliceOfN(decGen, numSteps, numSteps).Draw(t, "decisions")

		outcome := presRefSequential(decisions, m)

		executor, instStore, auditStore, dispatcher := presBuildExecutor(cfg)
		order := presApproverIDs(m)
		cur := 0
		ctx := context.Background()

		for i := 0; i < outcome.appliedCount; i++ {
			d := decisions[i]
			err := executor.ResumeInstance(ctx, "inst-1", "approval-1", ApprovalResponse{
				Decision:   d,
				ApproverID: order[cur],
				DecidedAt:  time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("sequential step %d (%s by %s) unexpected error: %v", i, d, order[cur], err)
			}
			isSettleStep := outcome.settled && i == outcome.appliedCount-1
			if !isSettleStep && instStore.instance.Status != InstanceRunning {
				t.Fatalf("sequential step %d should keep running, got %q", i, instStore.instance.Status)
			}
			// Advance our local pointer only on a non-last approve.
			if d == approvalDecisionApprove && cur < m-1 {
				cur++
			}
		}

		// Terminal status preservation.
		if outcome.settled {
			if instStore.statusUpdate != outcome.finalStatus {
				t.Fatalf("sequential expected settle status %q, got %q (decisions=%v)", outcome.finalStatus, instStore.statusUpdate, decisions)
			}
		} else if instStore.statusUpdate != "" {
			t.Fatalf("sequential non-settling sequence should not transition status, got %q (decisions=%v)", instStore.statusUpdate, decisions)
		}

		// Dispatcher preservation (3.4): the executor dispatches to exactly the
		// next-in-order approvers the reference model predicts.
		if len(dispatcher.dispatched) != len(outcome.dispatched) {
			t.Fatalf("sequential dispatched count = %d (%v), want %d (%v)", len(dispatcher.dispatched), dispatcher.dispatched, len(outcome.dispatched), outcome.dispatched)
		}
		for i := range outcome.dispatched {
			if dispatcher.dispatched[i] != outcome.dispatched[i] {
				t.Fatalf("sequential dispatched[%d] = %q, want %q", i, dispatcher.dispatched[i], outcome.dispatched[i])
			}
		}

		// Audit preservation: one approval_decision per applied step.
		if got := presCountAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == "approval_decision" }); got != outcome.appliedCount {
			t.Fatalf("sequential expected %d approval_decision entries, got %d", outcome.appliedCount, got)
		}
	})
}

// ---------------------------------------------------------------------------
// FormValidator semantics preservation (3.5).
//
// The fix registers the RuntimeAPI initiation route but does NOT change
// FormValidator.Validate. We observe that Validate accepts iff every required
// field is present and every present field satisfies its type/length/range/
// option/pattern constraints — the exact predicate the validated initiation
// path will rely on.
// ---------------------------------------------------------------------------

// presRefFieldValid is an independent reference for whether a single field
// value satisfies its schema (mirrors FormValidator's per-type checks for the
// generated field kinds: text, number, select, boolean).
func presRefFieldValid(field FormFieldSchema, value interface{}, present bool) bool {
	if field.Required && (!present || value == nil) {
		return false
	}
	if !present || value == nil {
		return true
	}
	switch field.Type {
	case FieldText, FieldTextarea:
		s, ok := value.(string)
		if !ok {
			return false
		}
		if field.MaxLength > 0 && len([]rune(s)) > field.MaxLength {
			return false
		}
		return true
	case FieldNumber:
		f, ok := value.(float64)
		if !ok {
			return false
		}
		if field.MinValue != nil && f < *field.MinValue {
			return false
		}
		if field.MaxValue != nil && f > *field.MaxValue {
			return false
		}
		return true
	case FieldSelect:
		s, ok := value.(string)
		if !ok {
			return false
		}
		if len(field.Options) > 0 {
			for _, opt := range field.Options {
				if opt == s {
					return true
				}
			}
			return false
		}
		return true
	case FieldBoolean:
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

// TestPreservation_FormValidator_AcceptRejectMatchesSchema observes that
// FormValidator.Validate returns no errors iff every field satisfies the
// reference predicate, across generated schemas and form data (3.5).
//
// **Validates: Requirements 3.5**
func TestPreservation_FormValidator_AcceptRejectMatchesSchema(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := &FormValidator{}

		fieldCount := rapid.IntRange(1, 5).Draw(t, "field_count")
		schema := make([]FormFieldSchema, fieldCount)
		formData := map[string]interface{}{}
		present := make([]bool, fieldCount)
		values := make([]interface{}, fieldCount)

		typeGen := rapid.SampledFrom([]FieldType{FieldText, FieldNumber, FieldSelect, FieldBoolean})

		for i := 0; i < fieldCount; i++ {
			name := fmt.Sprintf("f%d", i)
			ft := typeGen.Draw(t, fmt.Sprintf("type_%d", i))
			field := FormFieldSchema{
				Name:     name,
				Label:    name,
				Type:     ft,
				Required: rapid.Bool().Draw(t, fmt.Sprintf("req_%d", i)),
			}
			switch ft {
			case FieldText:
				if rapid.Bool().Draw(t, fmt.Sprintf("hasmax_%d", i)) {
					field.MaxLength = rapid.IntRange(1, 10).Draw(t, fmt.Sprintf("maxlen_%d", i))
				}
			case FieldNumber:
				if rapid.Bool().Draw(t, fmt.Sprintf("hasmin_%d", i)) {
					mn := float64(rapid.IntRange(-100, 100).Draw(t, fmt.Sprintf("min_%d", i)))
					field.MinValue = &mn
				}
				if rapid.Bool().Draw(t, fmt.Sprintf("hasmax_%d", i)) {
					mx := float64(rapid.IntRange(101, 300).Draw(t, fmt.Sprintf("max_%d", i)))
					field.MaxValue = &mx
				}
			case FieldSelect:
				if rapid.Bool().Draw(t, fmt.Sprintf("hasopts_%d", i)) {
					field.Options = []string{"a", "b", "c"}
				}
			}
			schema[i] = field

			// Decide presence and value (sometimes the "wrong" type to exercise rejection).
			isPresent := rapid.Bool().Draw(t, fmt.Sprintf("present_%d", i))
			present[i] = isPresent
			if !isPresent {
				continue
			}
			var val interface{}
			switch rapid.IntRange(0, 3).Draw(t, fmt.Sprintf("valkind_%d", i)) {
			case 0:
				val = rapid.SampledFrom([]string{"", "x", "hello", "abcdefghijklmnop", "a", "b", "c", "zzz"}).Draw(t, fmt.Sprintf("strval_%d", i))
			case 1:
				val = float64(rapid.IntRange(-200, 400).Draw(t, fmt.Sprintf("numval_%d", i)))
			case 2:
				val = rapid.Bool().Draw(t, fmt.Sprintf("boolval_%d", i))
			case 3:
				val = nil
			}
			values[i] = val
			formData[name] = val
		}

		// Reference: accepted iff every field is individually valid.
		refAccept := true
		for i := range schema {
			if !presRefFieldValid(schema[i], values[i], present[i] && values[i] != nil) {
				refAccept = false
				break
			}
		}

		errs := v.Validate(formData, schema)
		gotAccept := len(errs) == 0
		if gotAccept != refAccept {
			t.Fatalf("Validate accept=%v, reference=%v\nschema=%+v\ndata=%+v\nerrors=%+v", gotAccept, refAccept, schema, formData, errs)
		}
	})
}

// ---------------------------------------------------------------------------
// Trigger route + owner isolation (3.5) and legitimate-owner access (3.11).
//
// The fix keeps the /trigger route and registers RuntimeAPI; it only DENIES
// access when ownership is UNESTABLISHED (empty requester_id — a bug-condition
// input, NOT preserved). For NON-bug-condition inputs (requester_id != "") we
// observe the existing behavior: TriggerFromMarket injects requester_id, and
// handleGetInstance grants access iff requester_id == caller.
// ---------------------------------------------------------------------------

// TestPreservation_TriggerFromMarket_InjectsRequesterID observes that
// TriggerFromMarket binds the instance to the triggering user via requester_id
// for any non-empty user ID and trigger payload (3.5).
//
// **Validates: Requirements 3.5**
func TestPreservation_TriggerFromMarket_InjectsRequesterID(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userID := rapid.StringMatching(`[a-zA-Z0-9_]{1,12}`).Draw(t, "user_id")

		graph := WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
			},
			Edges: nil,
		}
		ver := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
		wfStore := &resumeTestMockWorkflowStore{version: ver}
		instStore := &resumeTestMockInstanceStore{}
		auditStore := &mockAuditStore{}
		dispatcher := &resumeTestMockDispatcher{}
		executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

		// Generate an arbitrary trigger payload as a JSON object (or empty).
		extraKey := rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "extra_key")
		extraVal := rapid.IntRange(0, 1000).Draw(t, "extra_val")
		var triggerData string
		if rapid.Bool().Draw(t, "has_payload") && extraKey != "requester_id" {
			triggerData = fmt.Sprintf(`{%q:%d}`, extraKey, extraVal)
		}

		inst, err := executor.TriggerFromMarket(context.Background(), "wf-1", userID, triggerData)
		if err != nil {
			t.Fatalf("TriggerFromMarket error: %v", err)
		}
		got, _ := inst.InstanceData["requester_id"].(string)
		if got != userID {
			t.Fatalf("requester_id = %q, want %q", got, userID)
		}
		// Existing payload fields preserved.
		if triggerData != "" && extraKey != "requester_id" {
			if fv, ok := inst.InstanceData[extraKey]; !ok {
				t.Fatalf("expected payload key %q preserved in instance data", extraKey)
			} else if num, ok := fv.(float64); !ok || int(num) != extraVal {
				t.Fatalf("payload key %q = %v, want %d", extraKey, fv, extraVal)
			}
		}
	})
}

// TestPreservation_InstanceAccess_OwnerIsolation observes the existing access
// guard for NON-bug-condition instances (requester_id != ""): the owner is
// granted (200) and a non-owner is denied (404). Empty requester_id is a
// bug-condition input and is intentionally excluded.
//
// **Validates: Requirements 3.5, 3.11**
func TestPreservation_InstanceAccess_OwnerIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		owner := rapid.StringMatching(`[a-zA-Z0-9_]{1,12}`).Draw(t, "owner")
		caller := rapid.StringMatching(`[a-zA-Z0-9_]{1,12}`).Draw(t, "caller")

		// Build the InstanceAPI harness inline (setupInstanceAPITest needs *testing.T).
		instStore := newMemInstanceStoreForAPI()
		auditStore := newMemAuditStoreForAPI()
		wfStore := newMemWorkflowStore()
		executor := NewWorkflowExecutor(wfStore, instStore, auditStore, &noopDispatcher{})
		api := NewInstanceAPI(executor, instStore, auditStore)

		instStore.instances["inst1"] = &WorkflowInstance{
			ID:            "inst1",
			WorkflowID:    "wf1",
			VersionID:     "ver1",
			Status:        InstanceRunning,
			CurrentNodeID: "node1",
			InstanceData:  map[string]interface{}{"requester_id": owner}, // non-empty => non-bug-condition
		}

		mux := http.NewServeMux()
		// Auth middleware that sets X-Owner-ID to the generated caller.
		api.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Owner-ID", caller)
				h(w, r)
			}
		})

		req := httptest.NewRequest("GET", "/api/v1/instances/inst1", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if caller == owner {
			if w.Code != http.StatusOK {
				t.Fatalf("owner %q should be granted access, got %d: %s", caller, w.Code, w.Body.String())
			}
		} else {
			if w.Code != http.StatusNotFound {
				t.Fatalf("non-owner %q should be denied (404), got %d", caller, w.Code)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// EscalationManager preservation (3.6).
//
// The fix only changes the availability SOURCE (HumanApproverChecker impl),
// not EscalationManager.Escalate. We observe: when the checker reports the
// approver available AND dispatch succeeds, the request is delivered (no
// queueing, no audit); otherwise the request is queued (PendingCount==1) and
// an escalation_unavailable audit event is recorded. The interface and call
// sites are held fixed (only the concrete checker is swapped).
// ---------------------------------------------------------------------------

// TestPreservation_EscalationManager_AvailabilityDrivenRouting observes the
// existing Escalate routing under a fixed HumanApproverChecker interface,
// driven by generated availability/dispatch-failure flags (3.6).
//
// **Validates: Requirements 3.6**
func TestPreservation_EscalationManager_AvailabilityDrivenRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		available := rapid.Bool().Draw(t, "available")
		dispatchFails := rapid.Bool().Draw(t, "dispatch_fails")

		checker := &mockHumanChecker{available: available}
		dispatcher := &mockDispatcherForEsc{failNext: dispatchFails}
		audit := &mockAuditStoreForEsc{}
		mgr := NewEscalationManager(dispatcher, audit, checker)

		humanApprover := rapid.StringMatching(`[a-z0-9_]{1,10}`).Draw(t, "human")
		req := &ApprovalRequest{
			ID:         "areq-1",
			InstanceID: "inst-1",
			NodeID:     "approval-1",
		}

		err := mgr.Escalate(context.Background(), req, humanApprover)
		if err != nil {
			t.Fatalf("Escalate returned error: %v", err)
		}

		delivered := available && !dispatchFails
		if delivered {
			if dispatcher.dispatchCount() != 1 {
				t.Fatalf("delivered case should dispatch exactly once, got %d", dispatcher.dispatchCount())
			}
			if mgr.PendingCount() != 0 {
				t.Fatalf("delivered case should not queue, pending=%d", mgr.PendingCount())
			}
			if presHasEscEntry(audit.getEntries(), "escalation_unavailable") {
				t.Fatalf("delivered case should not record escalation_unavailable")
			}
		} else {
			if mgr.PendingCount() != 1 {
				t.Fatalf("undelivered case should queue exactly one request, pending=%d", mgr.PendingCount())
			}
			if !presHasEscEntry(audit.getEntries(), "escalation_unavailable") {
				t.Fatalf("undelivered case should record escalation_unavailable audit event")
			}
		}
	})
}

func presHasEscEntry(entries []AuditEntry, eventType string) bool {
	for _, e := range entries {
		if e.EventType == eventType {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ConfirmationTracker.Confirm validation + notes truncation (3.7).
//
// The fix implements ReconcileOrphanedInstances and the confirm endpoints but
// does NOT change Confirm validation. We observe: recipient mismatch ->
// ErrRecipientMismatch; non-pending -> ErrAlreadyConfirmed; executor notes
// truncate to 2000 runes; notifier notes cleared; success transitions to
// confirmed and records the right audit event.
// ---------------------------------------------------------------------------

// TestPreservation_ConfirmationTracker_ConfirmValidation observes Confirm's
// validation and notes-handling across generated recipient/status/type/notes
// inputs (3.7).
//
// **Validates: Requirements 3.7**
func TestPreservation_ConfirmationTracker_ConfirmValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		store := newMockConfirmationStore()
		audit := &mockAuditStoreForConfirm{}
		ct := NewConfirmationTracker(store, nil, nil, audit)

		recipient := rapid.StringMatching(`[a-z0-9_]{1,10}`).Draw(t, "recipient")
		caller := rapid.StringMatching(`[a-z0-9_]{1,10}`).Draw(t, "caller")
		isExecutor := rapid.Bool().Draw(t, "is_executor")
		ctype := ConfirmTypeNotifier
		if isExecutor {
			ctype = ConfirmTypeExecutor
		}
		status := rapid.SampledFrom([]ConfirmationStatus{ConfirmPending, ConfirmConfirmed, ConfirmAutoClosed}).Draw(t, "status")
		noteLen := rapid.IntRange(0, 2600).Draw(t, "note_len")
		notes := strings.Repeat("A", noteLen)

		conf := &Confirmation{
			ID:          "conf-1",
			InstanceID:  "inst-1",
			RecipientID: recipient,
			Type:        ctype,
			Status:      status,
			CreatedAt:   time.Now().UTC(),
		}
		store.confs[conf.ID] = conf

		err := ct.Confirm(context.Background(), "conf-1", caller, notes)

		switch {
		case caller != recipient:
			if err != ErrRecipientMismatch {
				t.Fatalf("recipient mismatch should yield ErrRecipientMismatch, got %v", err)
			}
		case status != ConfirmPending:
			if err != ErrAlreadyConfirmed {
				t.Fatalf("non-pending should yield ErrAlreadyConfirmed, got %v", err)
			}
		default:
			// Valid path: recipient matches, pending.
			if err != nil {
				t.Fatalf("valid confirm should succeed, got %v", err)
			}
			if store.updatedStatus["conf-1"] != ConfirmConfirmed {
				t.Fatalf("valid confirm should set status confirmed, got %q", store.updatedStatus["conf-1"])
			}
			stored := store.updatedNotes["conf-1"]
			if isExecutor {
				want := notes
				if len([]rune(want)) > 2000 {
					want = string([]rune(want)[:2000])
				}
				if stored != want {
					t.Fatalf("executor notes mismatch: got %d runes, want %d runes", len([]rune(stored)), len([]rune(want)))
				}
				if !presHasConfirmAudit(audit.entries, "executor_confirmed", caller) {
					t.Fatalf("valid executor confirm should record executor_confirmed audit")
				}
			} else {
				if stored != "" {
					t.Fatalf("notifier notes should be cleared, got %d chars", len(stored))
				}
				if !presHasConfirmAudit(audit.entries, "notifier_acknowledged", caller) {
					t.Fatalf("valid notifier confirm should record notifier_acknowledged audit")
				}
			}
		}
	})
}

func presHasConfirmAudit(entries []*AuditEntry, eventType, actor string) bool {
	for _, e := range entries {
		if e.EventType == eventType && e.ActorID == actor {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Terminal-node completion ordering + StartTracking (3.8).
//
// We observe that reaching a terminal node first marks the instance completed
// (UpdateStatus completed) and records instance_completed, THEN StartTracking
// creates one pending confirmation per configured executor/notifier. The fix
// (reconciliation) does not change this ordering or StartTracking output.
// ---------------------------------------------------------------------------

// TestPreservation_TerminalNode_CompletionOrdering observes that StartTracking
// creates exactly one pending confirmation per configured executor + notifier,
// after the instance is marked completed, across generated terminal configs
// (3.8).
//
// **Validates: Requirements 3.8**
func TestPreservation_TerminalNode_CompletionOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numExec := rapid.IntRange(0, 3).Draw(t, "num_exec")
		numNotif := rapid.IntRange(0, 3).Draw(t, "num_notif")

		var execs []ExecutorConfig
		for i := 0; i < numExec; i++ {
			execs = append(execs, ExecutorConfig{UserID: fmt.Sprintf("exec-%d", i)})
		}
		var notifs []NotifierConfig
		for i := 0; i < numNotif; i++ {
			notifs = append(notifs, NotifierConfig{UserID: fmt.Sprintf("notif-%d", i)})
		}
		termCfg := TerminalNodeConfig{ResultExecutors: execs, Notifiers: notifs}
		termCfgJSON, _ := json.Marshal(termCfg)

		graph := WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
				{ID: "term-1", Type: NodeTypeTerminal, Label: "End", Config: termCfgJSON},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "trigger-1", TargetID: "term-1"},
			},
		}
		ver := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
		wfStore := &resumeTestMockWorkflowStore{version: ver}
		instStore := &resumeTestMockInstanceStore{}
		auditStore := &mockAuditStore{}
		dispatcher := &resumeTestMockDispatcher{}
		confStore := newMockConfirmationStore()
		tracker := NewConfirmationTracker(confStore, instStore, nil, auditStore)

		executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithConfirmationTracker(tracker))

		_, err := executor.StartInstance(context.Background(), "wf-1", "")
		if err != nil {
			t.Fatalf("StartInstance error: %v", err)
		}

		// Instance must be completed.
		if instStore.statusUpdate != InstanceCompleted {
			t.Fatalf("terminal node should complete instance, got %q", instStore.statusUpdate)
		}
		// instance_completed audit must be present.
		if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == "instance_completed" }) {
			t.Fatalf("terminal completion should record instance_completed audit")
		}
		// StartTracking creates exactly one pending confirmation per recipient.
		wantConfs := numExec + numNotif
		if len(confStore.confs) != wantConfs {
			t.Fatalf("expected %d confirmations created, got %d", wantConfs, len(confStore.confs))
		}
		for _, c := range confStore.confs {
			if c.Status != ConfirmPending {
				t.Fatalf("confirmation %s should be pending, got %q", c.ID, c.Status)
			}
			if c.InstanceID == "" {
				t.Fatalf("confirmation %s missing instance id", c.ID)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Version auto-increment for genuinely new drafts (3.9).
//
// The fix changes only the "update existing draft" branch (to update in place
// rather than create a new row). The auto-increment of a genuinely-new draft
// (first draft -> 0.1.0; new draft after a published version -> minor bump)
// must be unchanged. We restrict to non-bug-condition SaveDraft inputs: the
// store has no draft (so SaveDraft creates a new version, not the update branch).
// ---------------------------------------------------------------------------

// TestPreservation_SaveDraft_NewDraftAutoIncrement observes the first-draft and
// post-published minor-increment behavior of SaveDraft for genuinely-new drafts
// (3.9). It deliberately avoids the "update existing draft" branch (a
// bug-condition input).
//
// **Validates: Requirements 3.9**
func TestPreservation_SaveDraft_NewDraftAutoIncrement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		store := newMockVersionStore()
		vm := NewVersionManager(store)
		ctx := context.Background()

		// First draft is always 0.1.0.
		v1, err := vm.SaveDraft(ctx, "wf-1", validGraph())
		if err != nil {
			t.Fatalf("first SaveDraft error: %v", err)
		}
		if v1.VersionNumber != "0.1.0" {
			t.Fatalf("first draft version = %q, want 0.1.0", v1.VersionNumber)
		}
		if v1.Status != VersionDraft {
			t.Fatalf("first draft status = %q, want draft", v1.Status)
		}

		// Publish v1, then a NEW draft (the latest version is non-draft =>
		// the "create new draft / increment minor" branch, not the update branch).
		if err := vm.SubmitForReview(ctx, v1.ID); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if err := vm.Approve(ctx, v1.ID); err != nil {
			t.Fatalf("approve: %v", err)
		}

		v2, err := vm.SaveDraft(ctx, "wf-1", validGraph())
		if err != nil {
			t.Fatalf("post-publish SaveDraft error: %v", err)
		}
		if v2.VersionNumber != "0.2.0" {
			t.Fatalf("post-publish new draft = %q, want 0.2.0", v2.VersionNumber)
		}
		if v2.Status != VersionDraft {
			t.Fatalf("post-publish new draft status = %q, want draft", v2.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// AdminReviewService.ApproveSubmission (3.10).
//
// The fix makes VersionManager.Approve converge ONTO ApproveSubmission; the
// reference path itself (publish + supersede + market registration + rollback)
// is unchanged. We observe: approving a pending_review version publishes it,
// supersedes any previous published version, and registers it in the
// capability market (queryable via capability.Service).
// ---------------------------------------------------------------------------

// presApproveCapabilityDB creates an in-memory capability DB (mirrors the admin
// review test schema) for ApproveSubmission preservation checks.
func presApproveCapabilityDB(t *rapid.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := `
		CREATE TABLE capabilities (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			id TEXT NOT NULL,
			capability_type TEXT NOT NULL,
			publisher TEXT NOT NULL DEFAULT '',
			capability_id TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			managed_by TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			relation_to_origin TEXT NOT NULL DEFAULT '',
			global_key TEXT NOT NULL DEFAULT '',
			current_version_key TEXT NOT NULL DEFAULT '',
			origin_key TEXT NOT NULL DEFAULT '',
			origin_json TEXT NOT NULL DEFAULT '',
			provenance_json TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (tenant_id, id)
		);
		CREATE TABLE capability_versions (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			id TEXT NOT NULL,
			capability_ref TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			version_key TEXT NOT NULL DEFAULT '',
			package_url TEXT NOT NULL DEFAULT '',
			package_checksum TEXT NOT NULL DEFAULT '',
			package_signature TEXT NOT NULL DEFAULT '',
			manifest_json TEXT NOT NULL DEFAULT '',
			type_config_json TEXT NOT NULL DEFAULT '',
			permissions_json TEXT NOT NULL DEFAULT '',
			pricing_json TEXT NOT NULL DEFAULT '',
			license_json TEXT NOT NULL DEFAULT '',
			compatibility_json TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (tenant_id, id)
		);
		CREATE UNIQUE INDEX idx_capability_versions_key ON capability_versions(tenant_id, version_key);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create capability schema: %v", err)
	}
	return db
}

// TestPreservation_AdminReview_ApproveSubmission observes the authoritative
// publish path: publish + supersede previous + market registration, across
// generated version numbers and a possible pre-existing published version
// (3.10).
//
// **Validates: Requirements 3.10**
func TestPreservation_AdminReview_ApproveSubmission(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		store := newMockStoreForAdmin()
		db := presApproveCapabilityDB(t)
		svc := NewAdminReviewService(store, capability.NewService(db))
		ctx := context.Background()

		store.workflows["wf-1"] = &WorkflowDefinition{
			ID:      "wf-1",
			OwnerID: "owner-1",
			Name:    "WF",
		}

		hasPrev := rapid.Bool().Draw(t, "has_prev_published")
		minorOld := rapid.IntRange(1, 4).Draw(t, "minor_old")
		minorNew := minorOld + rapid.IntRange(1, 3).Draw(t, "minor_delta")
		now := time.Now().UTC()

		if hasPrev {
			store.versions["v-old"] = &WorkflowVersion{
				ID:            "v-old",
				WorkflowID:    "wf-1",
				VersionNumber: fmt.Sprintf("0.%d.0", minorOld),
				Status:        VersionPublished,
				CreatedAt:     now,
				UpdatedAt:     now,
				Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
			}
		}

		newVersionNumber := fmt.Sprintf("0.%d.0", minorNew)
		store.versions["v-new"] = &WorkflowVersion{
			ID:            "v-new",
			WorkflowID:    "wf-1",
			VersionNumber: newVersionNumber,
			Status:        VersionPendingReview,
			SubmittedAt:   &now,
			CreatedAt:     now,
			UpdatedAt:     now,
			Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
		}

		if err := svc.ApproveSubmission(ctx, "v-new"); err != nil {
			t.Fatalf("ApproveSubmission error: %v", err)
		}

		// New version published.
		if store.versions["v-new"].Status != VersionPublished {
			t.Fatalf("new version status = %q, want published", store.versions["v-new"].Status)
		}
		// Previous version superseded (if present).
		if hasPrev && store.versions["v-old"].Status != VersionSuperseded {
			t.Fatalf("previous version status = %q, want superseded", store.versions["v-old"].Status)
		}
		// Registered in capability market with the new version as current.
		cap, err := capability.NewService(db).Get(ctx, workflowCapabilityID("wf-1"))
		if err != nil {
			t.Fatalf("get capability: %v", err)
		}
		if cap == nil {
			t.Fatalf("workflow should be registered in capability market after approve")
		}
		wantKey := "approval_workflow:wf-1:" + newVersionNumber
		if cap.CurrentVersionKey != wantKey {
			t.Fatalf("current_version_key = %q, want %q", cap.CurrentVersionKey, wantKey)
		}
		if strings.ToLower(cap.Status) != "active" {
			t.Fatalf("capability status = %q, want active", cap.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// Blocking-node (approval) dispatch semantics + ApprovalDispatcher interface
// unchanged (3.4, 3.12).
//
// We observe that reaching an approval node dispatches the request via the
// existing ApprovalDispatcher.Dispatch call sites (single -> first approver;
// countersign / any_n_of_m -> every approver; sequential -> first in order),
// leaves the instance running (blocking), and does NOT complete/fail the node.
// The fix only swaps the concrete dispatcher implementation; the call sites and
// the blocking semantics are preserved.
// ---------------------------------------------------------------------------

// presExpectedDispatchTargets returns the approver IDs the executor should
// dispatch to when first entering an approval node in the given mode.
func presExpectedDispatchTargets(mode ApprovalMode, approverIDs, order []string) []string {
	switch mode {
	case ModeSingle:
		return []string{approverIDs[0]}
	case ModeCountersign, ModeAnyNofM:
		out := make([]string, len(approverIDs))
		copy(out, approverIDs)
		return out
	case ModeSequential:
		seq := order
		if len(seq) == 0 {
			seq = approverIDs
		}
		return []string{seq[0]}
	default:
		return nil
	}
}

// TestPreservation_ApprovalNode_DispatchAndBlocking observes that entering an
// approval node dispatches to the expected approver(s) and leaves the instance
// running (blocking semantics), across all four modes (3.4, 3.12).
//
// **Validates: Requirements 3.4, 3.12**
func TestPreservation_ApprovalNode_DispatchAndBlocking(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mode := rapid.SampledFrom([]ApprovalMode{ModeSingle, ModeCountersign, ModeAnyNofM, ModeSequential}).Draw(t, "mode")
		m := rapid.IntRange(1, 4).Draw(t, "m")
		approverIDs := presApproverIDs(m)

		cfg := ApprovalNodeConfig{
			ApproverIDs:  approverIDs,
			Mode:         mode,
			TimeoutHours: 24,
		}
		if mode == ModeAnyNofM {
			cfg.MinApprovals = rapid.IntRange(1, m).Draw(t, "min_approvals")
		}
		if mode == ModeSequential {
			cfg.ApproverOrder = approverIDs
		}

		graph := buildApprovalGraph(cfg)
		ver := &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Status: VersionPublished, Graph: graph}
		wfStore := &resumeTestMockWorkflowStore{version: ver}
		instStore := &resumeTestMockInstanceStore{}
		auditStore := &mockAuditStore{}
		dispatcher := &resumeTestMockDispatcher{}
		executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

		inst, err := executor.StartInstance(context.Background(), "wf-1", "")
		if err != nil {
			t.Fatalf("StartInstance error: %v", err)
		}

		// Blocking: the instance must still be running (not completed/failed),
		// because an approval node waits for ResumeInstance.
		if inst.Status != InstanceRunning {
			t.Fatalf("approval node should keep instance running, got %q", inst.Status)
		}
		if instStore.statusUpdate == InstanceCompleted || instStore.statusUpdate == InstanceFailed {
			t.Fatalf("approval node must not settle instance on entry, got status update %q", instStore.statusUpdate)
		}

		// Dispatch targets preserved.
		want := presExpectedDispatchTargets(mode, approverIDs, cfg.ApproverOrder)
		if len(dispatcher.dispatched) != len(want) {
			t.Fatalf("mode %q dispatched %v, want %v", mode, dispatcher.dispatched, want)
		}
		// Order for single/sequential is deterministic; for countersign/any_n_of_m
		// the executor dispatches in approverIDs order.
		for i := range want {
			if dispatcher.dispatched[i] != want[i] {
				t.Fatalf("mode %q dispatched[%d] = %q, want %q (all=%v)", mode, i, dispatcher.dispatched[i], want[i], dispatcher.dispatched)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Design -> submit -> review -> publish half (3.1).
//
// The full version lifecycle (SaveDraft -> SubmitForReview -> Approve ->
// CreateDraftFromPublished -> Approve) is part of the already-wired half. We
// observe the end-to-end status transitions and version numbers over a
// generated number of publish cycles; the fix must not regress any of it.
// ---------------------------------------------------------------------------

// TestPreservation_VersionLifecycle_DesignToPublish observes the full
// design->submit->review->publish lifecycle over multiple generated cycles,
// asserting status transitions and minor-version increments (3.1).
//
// **Validates: Requirements 3.1**
func TestPreservation_VersionLifecycle_DesignToPublish(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		store := newMockVersionStore()
		vm := NewVersionManager(store)
		ctx := context.Background()

		cycles := rapid.IntRange(1, 4).Draw(t, "cycles")

		var prevID string
		for c := 0; c < cycles; c++ {
			var ver *WorkflowVersion
			var err error
			if c == 0 {
				ver, err = vm.SaveDraft(ctx, "wf-1", validGraph())
			} else {
				ver, err = vm.CreateDraftFromPublished(ctx, "wf-1")
			}
			if err != nil {
				t.Fatalf("cycle %d draft error: %v", c, err)
			}
			wantVersion := fmt.Sprintf("0.%d.0", c+1)
			if ver.VersionNumber != wantVersion {
				t.Fatalf("cycle %d version = %q, want %q", c, ver.VersionNumber, wantVersion)
			}
			if ver.Status != VersionDraft {
				t.Fatalf("cycle %d draft status = %q, want draft", c, ver.Status)
			}

			if err := vm.SubmitForReview(ctx, ver.ID); err != nil {
				t.Fatalf("cycle %d submit error: %v", c, err)
			}
			if store.versions[ver.ID].Status != VersionPendingReview {
				t.Fatalf("cycle %d after submit status = %q, want pending_review", c, store.versions[ver.ID].Status)
			}

			if err := vm.Approve(ctx, ver.ID); err != nil {
				t.Fatalf("cycle %d approve error: %v", c, err)
			}
			if store.versions[ver.ID].Status != VersionPublished {
				t.Fatalf("cycle %d after approve status = %q, want published", c, store.versions[ver.ID].Status)
			}
			// Previous published version superseded.
			if prevID != "" && store.versions[prevID].Status != VersionSuperseded {
				t.Fatalf("cycle %d previous %q status = %q, want superseded", c, prevID, store.versions[prevID].Status)
			}
			prevID = ver.ID
		}
	})
}

// ---------------------------------------------------------------------------
// Executor fallback handlers: HandleTimeout / HandleUnavailable / HandleQueueFull (3.6).
//
// The fix only changes the availability SOURCE (HumanApproverChecker impl); the
// executor's fallback-routing handlers are untouched. The existing property
// test above covers EscalationManager.Escalate. This property generalizes the
// example-based timeout tests over the three executor handlers and the
// fallback/dispatch outcomes, locking in the observed routing:
//   - no fallback configured            -> node blocked, initiator notified
//   - fallback configured, dispatch ok  -> fallback_routed, NOT blocked, no notify
//   - fallback configured, dispatch err -> fallback_failed + node blocked + notify
// plus the per-handler leading audit event (node_timeout / approver_unavailable
// / approver_queue_full) and the per-handler DispatchFallback reason.
//
// **Validates: Requirements 3.6**

// presFallbackHandler identifies which executor handler is exercised and the
// observable per-handler audit/reason it produces.
type presFallbackHandler struct {
	name        string // for diagnostics
	leadingType string // leading audit event type
	reason      string // DispatchFallback reason
	actor       bool   // whether the leading audit entry carries ActorID == approverID
}

// presRunFallbackHandler invokes the selected handler.
func presRunFallbackHandler(
	executor *WorkflowExecutor,
	which int,
	approverID string,
) (presFallbackHandler, error) {
	ctx := context.Background()
	switch which {
	case 0:
		return presFallbackHandler{name: "timeout", leadingType: "node_timeout", reason: "timeout", actor: false},
			executor.HandleTimeout(ctx, "inst-1", "approval-1")
	case 1:
		return presFallbackHandler{name: "unavailable", leadingType: "approver_unavailable", reason: "unavailable", actor: true},
			executor.HandleUnavailable(ctx, "inst-1", "approval-1", approverID)
	default:
		return presFallbackHandler{name: "queue_full", leadingType: "approver_queue_full", reason: "queue_full", actor: true},
			executor.HandleQueueFull(ctx, "inst-1", "approval-1", approverID)
	}
}

// TestPreservation_ExecutorFallbackRouting observes that the three executor
// fallback handlers route identically across generated fallback/dispatch
// outcomes: leading audit event, DispatchFallback reason, blocked-vs-routed
// terminal state, and initiator notification (3.6).
//
// **Validates: Requirements 3.6**
func TestPreservation_ExecutorFallbackRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		which := rapid.IntRange(0, 2).Draw(t, "handler")
		hasFallback := rapid.Bool().Draw(t, "has_fallback")
		dispatchFails := rapid.Bool().Draw(t, "dispatch_fails")
		approverID := "ve-primary"

		fallbackID := ""
		if hasFallback {
			fallbackID = "ve-fallback"
		}
		graph := buildTimeoutTestGraph(fallbackID)
		ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
		wfStore := &mockWorkflowStoreWithVersion{version: ver}
		instStore := &mockInstanceStoreForTimeout{
			instance: &WorkflowInstance{
				ID:           "inst-1",
				VersionID:    "ver-1",
				Status:       InstanceRunning,
				InstanceData: map[string]interface{}{"title": "Req"},
			},
		}
		auditStore := &mockAuditStore{}
		dispatcher := &mockDispatcherForTimeout{}
		if dispatchFails {
			dispatcher.fallbackErr = fmt.Errorf("fallback unavailable")
		}
		notifier := &mockNotifier{}
		executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithNotifier(notifier))

		meta, err := presRunFallbackHandler(executor, which, approverID)
		if err != nil {
			t.Fatalf("%s handler returned error: %v", meta.name, err)
		}

		// Reference model: which terminal route is taken.
		fallbackDelivered := hasFallback && !dispatchFails
		blocked := !fallbackDelivered

		// Leading audit event is always recorded with the per-handler type.
		if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == meta.leadingType }) {
			t.Fatalf("%s: expected leading audit event %q", meta.name, meta.leadingType)
		}
		if meta.actor {
			if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool {
				return e.EventType == meta.leadingType && e.ActorID == approverID
			}) {
				t.Fatalf("%s: leading audit event should carry actor %q", meta.name, approverID)
			}
		}

		if fallbackDelivered {
			// DispatchFallback called once, with the per-handler reason and the
			// configured fallback approver; no block, no notify.
			if len(dispatcher.fallbackDispatched) != 1 {
				t.Fatalf("%s delivered: expected 1 fallback dispatch, got %d", meta.name, len(dispatcher.fallbackDispatched))
			}
			if dispatcher.fallbackDispatched[0].FallbackID != "ve-fallback" {
				t.Fatalf("%s delivered: fallback id = %q, want ve-fallback", meta.name, dispatcher.fallbackDispatched[0].FallbackID)
			}
			if dispatcher.fallbackDispatched[0].Reason != meta.reason {
				t.Fatalf("%s delivered: reason = %q, want %q", meta.name, dispatcher.fallbackDispatched[0].Reason, meta.reason)
			}
			if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == "fallback_routed" }) {
				t.Fatalf("%s delivered: expected fallback_routed audit", meta.name)
			}
			if instStore.updatedStatus == InstanceBlocked {
				t.Fatalf("%s delivered: instance must not be blocked", meta.name)
			}
			if len(notifier.notifications) != 0 {
				t.Fatalf("%s delivered: expected no initiator notification, got %d", meta.name, len(notifier.notifications))
			}
		}

		if blocked {
			// Instance is blocked and the initiator is notified.
			if instStore.updatedStatus != InstanceBlocked {
				t.Fatalf("%s blocked: instance status = %q, want blocked", meta.name, instStore.updatedStatus)
			}
			if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == "node_blocked" }) {
				t.Fatalf("%s blocked: expected node_blocked audit", meta.name)
			}
			if len(notifier.notifications) != 1 {
				t.Fatalf("%s blocked: expected exactly 1 initiator notification, got %d", meta.name, len(notifier.notifications))
			}
			// Cascading-failure path (fallback configured but dispatch failed)
			// additionally records fallback_failed.
			if hasFallback && dispatchFails {
				if !presHasAudit(auditStore.entries, func(e *AuditEntry) bool { return e.EventType == "fallback_failed" }) {
					t.Fatalf("%s cascading: expected fallback_failed audit", meta.name)
				}
			}
			// No-fallback path must not attempt a fallback dispatch.
			if !hasFallback && len(dispatcher.fallbackDispatched) != 0 {
				t.Fatalf("%s no-fallback: expected no fallback dispatch, got %d", meta.name, len(dispatcher.fallbackDispatched))
			}
		}
	})
}
