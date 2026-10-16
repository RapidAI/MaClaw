package guiapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
)

// codingE3DispositionSuiteVersion pins the hermetic disposition matrix to the
// qualification LifecycleDispositionVersion constant.
const codingE3DispositionSuiteVersion = "coding-dynamic-lifecycle-disposition-v1"

func TestCodingE3LifecycleDispositionVersionMatchesHermeticSuite(t *testing.T) {
	if codingDynamicLifecycleDispositionVersion != codingE3DispositionSuiteVersion {
		t.Fatalf("lifecycle disposition version %q does not match hermetic suite version %q", codingDynamicLifecycleDispositionVersion, codingE3DispositionSuiteVersion)
	}
}

var codingE3DispositionPhases = []string{"pre_dispatch", "in_flight", "bound", "batch", "settled"}

var codingE3DispositionValues = []agent.ToolSurfaceDisposition{
	agent.ToolSurfaceResponseAbandoned,
	agent.ToolSurfaceResponseSettled,
	agent.ToolSurfaceToolBatchSettled,
	agent.ToolSurfaceSteered,
	agent.ToolSurfaceRuntimeTerminal,
	agent.ToolSurfaceTransportFailure,
	agent.ToolSurfaceIntegrityFailure,
}

type codingE3MatrixCell struct {
	disposition agent.ToolSurfaceDisposition
	phase       string
}

type codingE3MatrixEntry struct {
	justification string
	local         func(*testing.T)
	remote        func(*testing.T)
}

// codingE3DispositionMatrix is the machine-declared cartesian matrix of the D2
// lifecycle disposition contract on the real production assembly. Every cell
// is either mapped to a hermetic test per family or carries an explicit
// unreachable-by-design justification. The meta-test below fails on any gap.
var codingE3DispositionMatrix = map[codingE3MatrixCell]codingE3MatrixEntry{
	{agent.ToolSurfaceIntegrityFailure, "pre_dispatch"}: {local: TestCodingE3PublicationFailureIsIntegrityTerminalLocal, remote: TestCodingE3PublicationFailureIsIntegrityTerminalRemote},
	{agent.ToolSurfaceSteered, "in_flight"}:             {local: TestCodingE3InFlightSteerSettlesPredecessorOnceLocal, remote: TestCodingE3InFlightSteerSettlesPredecessorOnceRemote},
	{agent.ToolSurfaceSteered, "bound"}:                 {local: TestCodingE3FinalizationRaceSealsLocal, remote: TestCodingE3FinalizationRaceSealsRemote},
	{agent.ToolSurfaceSteered, "batch"}:                 {local: TestCodingE3SteerAfterBatchCommitLocal, remote: TestCodingE3SteerAfterBatchCommitRemote},
	{agent.ToolSurfaceRuntimeTerminal, "batch"}:         {local: TestCodingE3CancelDuringBatchLocal, remote: TestCodingE3CancelDuringBatchRemote},
	{agent.ToolSurfaceRuntimeTerminal, "settled"}:       {local: TestCodingE3TerminalCloseAfterSettleLocal, remote: TestCodingE3TerminalCloseAfterSettleRemote},
	{agent.ToolSurfaceTransportFailure, "in_flight"}:    {local: TestCodingE3TransportFailureInFlightLocal, remote: TestCodingE3TransportFailureInFlightRemote},
	{agent.ToolSurfaceResponseAbandoned, "bound"}:       {local: TestCodingE3ResponseAbandonedAfterBindLocal, remote: TestCodingE3ResponseAbandonedAfterBindRemote},
	{agent.ToolSurfaceResponseAbandoned, "batch"}:       {local: TestCodingE3BatchDurabilityFailuresLocal, remote: TestCodingE3BatchDurabilityFailuresRemote},
	{agent.ToolSurfaceResponseSettled, "bound"}:         {local: TestCodingE3FinalTextSettlesLocal, remote: TestCodingE3FinalTextSettlesRemote},
	{agent.ToolSurfaceToolBatchSettled, "batch"}:        {local: TestCodingE3CompleteBatchSettlesExactlyOnceLocal, remote: TestCodingE3CompleteBatchSettlesExactlyOnceRemote},

	{agent.ToolSurfaceIntegrityFailure, "in_flight"}:     {justification: "an in-flight failure of a verified dispatch is transport_failure or steered; integrity failure is a pre-dispatch verdict"},
	{agent.ToolSurfaceIntegrityFailure, "bound"}:         {justification: "a bound response has already passed every integrity gate; later failures are lifecycle dispositions"},
	{agent.ToolSurfaceIntegrityFailure, "batch"}:         {justification: "no integrity verdict is produced once the batch is executing"},
	{agent.ToolSurfaceIntegrityFailure, "settled"}:       {justification: "a settled reservation has exactly one disposition already"},
	{agent.ToolSurfaceSteered, "pre_dispatch"}:           {justification: "a steer visible before dispatch is consumed by the transform boundary and never creates a reservation to steer"},
	{agent.ToolSurfaceSteered, "settled"}:                {justification: "a later steer is inert on a settled reservation; the bound-phase finalization race test proves the seal holds and no second disposition exists"},
	{agent.ToolSurfaceRuntimeTerminal, "pre_dispatch"}:   {justification: "a pre-dispatch stop has no reservation; the loop exits before a disposition can exist"},
	{agent.ToolSurfaceRuntimeTerminal, "in_flight"}:      {justification: "an in-flight cancellation surfaces as transport_failure at the dispatch boundary; runtime_terminal is a post-response decision"},
	{agent.ToolSurfaceRuntimeTerminal, "bound"}:          {justification: "a bound empty response is abandoned first (response_abandoned); the next runtime-terminal decision point is the batch boundary"},
	{agent.ToolSurfaceTransportFailure, "pre_dispatch"}:  {justification: "a pre-dispatch failure is an integrity verdict; no transport exchange exists yet"},
	{agent.ToolSurfaceTransportFailure, "bound"}:         {justification: "the transport is closed after the dispatch; a bound response cannot later become a transport failure"},
	{agent.ToolSurfaceTransportFailure, "batch"}:         {justification: "no transport read exists during batch execution"},
	{agent.ToolSurfaceTransportFailure, "settled"}:       {justification: "a settled reservation has exactly one disposition already"},
	{agent.ToolSurfaceResponseAbandoned, "pre_dispatch"}: {justification: "there is no response to abandon before dispatch"},
	{agent.ToolSurfaceResponseAbandoned, "in_flight"}:    {justification: "an in-flight abandonment is the binder-failure case decided at the bound boundary"},
	{agent.ToolSurfaceResponseAbandoned, "settled"}:      {justification: "a settled reservation has exactly one disposition already"},
	{agent.ToolSurfaceResponseSettled, "pre_dispatch"}:   {justification: "nothing can settle before a response exists"},
	{agent.ToolSurfaceResponseSettled, "in_flight"}:      {justification: "settlement is decided after the response bind, not in flight"},
	{agent.ToolSurfaceResponseSettled, "batch"}:          {justification: "a tool-bearing response settles as a batch or is steered/abandoned, never as text"},
	{agent.ToolSurfaceResponseSettled, "settled"}:        {justification: "a settled reservation has exactly one disposition already"},
	{agent.ToolSurfaceToolBatchSettled, "pre_dispatch"}:  {justification: "no batch exists before dispatch"},
	{agent.ToolSurfaceToolBatchSettled, "in_flight"}:     {justification: "no batch exists before the response"},
	{agent.ToolSurfaceToolBatchSettled, "bound"}:         {justification: "the batch settles only after execution commits at the batch boundary"},
	{agent.ToolSurfaceToolBatchSettled, "settled"}:       {justification: "a settled reservation has exactly one disposition already"},
}

func TestCodingE3DispositionMatrixCoversCartesianProduct(t *testing.T) {
	for _, disposition := range codingE3DispositionValues {
		for _, phase := range codingE3DispositionPhases {
			cell := codingE3MatrixCell{disposition: disposition, phase: phase}
			entry, ok := codingE3DispositionMatrix[cell]
			if !ok {
				t.Errorf("matrix cell %s x %s is undeclared", disposition, phase)
				continue
			}
			if entry.justification != "" {
				if entry.local != nil || entry.remote != nil {
					t.Errorf("matrix cell %s x %s has both a justification and tests", disposition, phase)
				}
				continue
			}
			if entry.local == nil || entry.remote == nil {
				t.Errorf("matrix cell %s x %s lacks a hermetic test for local or remote", disposition, phase)
			}
		}
	}
}

// --- shared scenario driver ---

type codingE3Run struct {
	env      *codingE3HermeticEnv
	ledger   *testCodingReservationLedger
	control  *testCodingDynamicRunLoopControl
	host     agent.LoopCallbacks
	hostBase *codingE3LoopHostBase
	loopCtx  *LoopContext
	cb       agent.LoopCallbacks
}

func codingE3StartRun(t *testing.T, family string, script func(int, *websocket.Conn, []byte)) *codingE3Run {
	t.Helper()
	env := newCodingE3HermeticEnv(t)
	env.wsScript = script
	loopCtx := NewLoopContext("e3-matrix-"+family, 1, nil)
	cb := env.newCallbacks(t, family, codingE3Identity(), loopCtx)
	run := &codingE3Run{env: env, ledger: newTestCodingReservationLedger(), control: &testCodingDynamicRunLoopControl{}, loopCtx: loopCtx, cb: cb}
	run.hostBase = &codingE3LoopHostBase{LoopCallbacks: cb, ledger: run.ledger, control: run.control}
	if family == "local" {
		run.host = &codingE3LocalLoopHost{codingE3LoopHostBase: run.hostBase}
	} else {
		run.host = &codingE3RemoteLoopHost{codingE3LoopHostBase: run.hostBase}
	}
	codingE3AssertHostCompositionParity(t, family, run.host)
	return run
}

func (r *codingE3Run) hooks() *testCodingDynamicLifecycleBatchHooks {
	hooks := &testCodingDynamicLifecycleBatchHooks{replanLoopCtx: r.loopCtx}
	switch cb := r.cb.(type) {
	case *codingSubAgentCallbacks:
		hooks.replanRevision = &cb.llmReplanRevision
	case *remoteCodingCallbacks:
		hooks.replanRevision = &cb.llmReplanRevision
	}
	return hooks
}

func (r *codingE3Run) execution(t *testing.T, index int, responseID string) agent.ToolCallExecutionContext {
	t.Helper()
	if len(r.hostBase.executions) <= index {
		t.Fatalf("host rendered %d reservations, want at least %d", len(r.hostBase.executions), index+1)
	}
	execution := r.hostBase.executions[index]
	execution.ResponseID = responseID
	return execution
}

func (r *codingE3Run) staticFallbackProbe(t *testing.T, execution agent.ToolCallExecutionContext) {
	t.Helper()
	name := "read_file"
	if _, ok := r.cb.(*remoteCodingCallbacks); ok {
		name = "ssh_read_file"
	}
	got := r.cb.(agent.ToolCallContextExecutor).ExecuteToolCallWithContext(name, `{}`, "late-static", execution)
	if got.Outcome != agent.ToolExecutionOutcomeError || got.Result != "[system rejected] stale_surface" {
		t.Fatalf("terminal composition fell back to static name dispatch: %#v", got)
	}
}

func codingE3AssertTerminalSequence(t *testing.T, ledger *testCodingReservationLedger, execution agent.ToolCallExecutionContext, boundResponseID string, disposition agent.ToolSurfaceDisposition) {
	t.Helper()
	events := ledger.eventsFor(t, execution)
	wantLen := 4
	if boundResponseID != "" {
		wantLen = 5
	}
	if len(events) != wantLen || events[0] != "reserved" || events[1] != "prepared" || !strings.HasPrefix(events[2], "receipt:") {
		t.Fatalf("reservation ledger=%v", events)
	}
	if boundResponseID != "" && events[3] != "bound:"+boundResponseID {
		t.Fatalf("reservation ledger=%v, want bound:%s", events, boundResponseID)
	}
	if events[len(events)-1] != "terminal:"+string(disposition) || len(testCodingReservationTerminalEvents(events)) != 1 {
		t.Fatalf("reservation ledger=%v, want exactly one terminal:%s", events, disposition)
	}
}

// --- matrix cell scenarios ---

func codingE3CompleteBatchSettlesExactlyOnce(t *testing.T, family string) {
	run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, frame []byte) {
		if seq == 1 {
			codingE3WSWrite(t, conn, codingE3WSToolCallFrames("resp-e3-batch", codingE3WSReadOnlyToolName(t, frame), `{"query":"e3"}`)...)
			return
		}
		// The settled batch advanced the repeat exposure; the next request gets
		// a final answer so the loop ends deterministically inside the real
		// iteration budget.
		codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-after-batch", "batch complete")...)
	})
	hooks := run.hooks()
	result := codingagent.Run(run.host, "run the batch", nil, nil, nil, hooks)
	if result.Error != "" || result.Text != "batch complete" || hooks.started != 1 || hooks.committed != 1 || run.control.executions != 1 {
		t.Fatalf("result=%+v started=%d committed=%d executions=%d", result, hooks.started, hooks.committed, run.control.executions)
	}
	if codingE3MCPCallCount(run.env) != 1 {
		t.Fatalf("fixed bridge did not reach the loopback provider exactly once: %d", codingE3MCPCallCount(run.env))
	}
	if codingE3WSConnCount(run.env) != 2 || len(run.hostBase.executions) != 2 {
		t.Fatalf("batch continuity used %d sockets / %d reservations, want 2/2", codingE3WSConnCount(run.env), len(run.hostBase.executions))
	}
	batch := run.execution(t, 0, "resp-e3-batch")
	codingE3AssertTerminalSequence(t, run.ledger, batch, "resp-e3-batch", agent.ToolSurfaceToolBatchSettled)
	successor := run.execution(t, 1, "resp-e3-after-batch")
	if successor.ConnectionID == batch.ConnectionID || successor.SurfaceEpoch == batch.SurfaceEpoch {
		t.Fatalf("successor reused the batch reservation tuple: %+v vs %+v", batch, successor)
	}
	codingE3AssertTerminalSequence(t, run.ledger, successor, "resp-e3-after-batch", agent.ToolSurfaceResponseSettled)
	run.staticFallbackProbe(t, successor)
}

func TestCodingE3CompleteBatchSettlesExactlyOnceLocal(t *testing.T) {
	codingE3CompleteBatchSettlesExactlyOnce(t, "local")
}
func TestCodingE3CompleteBatchSettlesExactlyOnceRemote(t *testing.T) {
	codingE3CompleteBatchSettlesExactlyOnce(t, "remote")
}

func codingE3FinalTextSettles(t *testing.T, family string) {
	run := codingE3StartRun(t, family, codingE3ScriptFinalText(t, "resp-e3-final", "hermetic final answer"))
	result := codingagent.Run(run.host, "final text", nil, nil, nil, run.hooks())
	if result.Error != "" || result.Text != "hermetic final answer" {
		t.Fatalf("result=%+v", result)
	}
	if run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("final text produced tool work: executions=%d mcp=%d", run.control.executions, codingE3MCPCallCount(run.env))
	}
	execution := run.execution(t, 0, "resp-e3-final")
	codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-final", agent.ToolSurfaceResponseSettled)
	run.staticFallbackProbe(t, execution)
}

func TestCodingE3FinalTextSettlesLocal(t *testing.T)  { codingE3FinalTextSettles(t, "local") }
func TestCodingE3FinalTextSettlesRemote(t *testing.T) { codingE3FinalTextSettles(t, "remote") }

func codingE3BatchDurabilityFailures(t *testing.T, family string) {
	t.Run("starter failure abandons before execution", func(t *testing.T) {
		run := codingE3StartRun(t, family, codingE3ScriptToolCall(t, "resp-e3-starter"))
		hooks := run.hooks()
		hooks.failStart = true
		result := codingagent.Run(run.host, "starter failure", nil, nil, nil, hooks)
		if result.Error != "recovery_checkpoint_failed" || hooks.started != 1 || hooks.committed != 0 || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
			t.Fatalf("result=%+v started=%d committed=%d executions=%d mcp=%d", result, hooks.started, hooks.committed, run.control.executions, codingE3MCPCallCount(run.env))
		}
		execution := run.execution(t, 0, "resp-e3-starter")
		codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-starter", agent.ToolSurfaceResponseAbandoned)
	})
	t.Run("committer failure abandons the paired batch", func(t *testing.T) {
		run := codingE3StartRun(t, family, codingE3ScriptToolCall(t, "resp-e3-committer"))
		hooks := run.hooks()
		hooks.failCommit = true
		result := codingagent.Run(run.host, "committer failure", nil, nil, nil, hooks)
		if result.Error != "recovery_checkpoint_failed" || hooks.started != 1 || hooks.committed != 1 || run.control.executions != 1 || codingE3MCPCallCount(run.env) != 1 {
			t.Fatalf("result=%+v started=%d committed=%d executions=%d mcp=%d", result, hooks.started, hooks.committed, run.control.executions, codingE3MCPCallCount(run.env))
		}
		execution := run.execution(t, 0, "resp-e3-committer")
		codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-committer", agent.ToolSurfaceResponseAbandoned)
	})
	// The interactive-pause early return is not reproducible through this
	// hermetic bridge: the fixed executor's tool result is the JSON-RPC
	// envelope of the provider answer, never a bare __ASK_USER__ marker, so
	// no provider script can drive RunLoop's pause branch without faking the
	// result shape. That branch stays covered by the D2 fixture and the
	// verified-ingress D2 audit; the batch-phase abandonment cell is covered
	// here by the starter/committer failure cases above.
}

func TestCodingE3BatchDurabilityFailuresLocal(t *testing.T) {
	codingE3BatchDurabilityFailures(t, "local")
}
func TestCodingE3BatchDurabilityFailuresRemote(t *testing.T) {
	codingE3BatchDurabilityFailures(t, "remote")
}

func codingE3ResponseAbandonedAfterBind(t *testing.T, family string) {
	t.Run("missing provider response id fails the bind", func(t *testing.T) {
		run := codingE3StartRun(t, family, func(_ int, conn *websocket.Conn, _ []byte) {
			codingE3WSWrite(t, conn, `{"type":"response.completed","response":{"status":"completed"}}`)
		})
		result := codingagent.Run(run.host, "binder failure", nil, nil, nil, run.hooks())
		if !strings.Contains(result.Error, "response binding failed") || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
			t.Fatalf("result=%+v executions=%d mcp=%d", result, run.control.executions, codingE3MCPCallCount(run.env))
		}
		execution := run.execution(t, 0, "")
		codingE3AssertTerminalSequence(t, run.ledger, execution, "", agent.ToolSurfaceResponseAbandoned)
	})
	t.Run("empty response is abandoned and the next reservation settles", func(t *testing.T) {
		run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, _ []byte) {
			if seq == 1 {
				codingE3WSWrite(t, conn, `{"type":"response.completed","response":{"id":"resp-e3-empty","status":"completed"}}`)
				return
			}
			codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-after-empty", "recovered after empty")...)
		})
		result := codingagent.Run(run.host, "empty response", nil, nil, nil, run.hooks())
		if result.Error != "" || result.Text != "recovered after empty" || run.control.executions != 0 {
			t.Fatalf("result=%+v executions=%d", result, run.control.executions)
		}
		if len(run.hostBase.executions) != 2 {
			t.Fatalf("empty response reservations=%d", len(run.hostBase.executions))
		}
		empty := run.execution(t, 0, "resp-e3-empty")
		codingE3AssertTerminalSequence(t, run.ledger, empty, "resp-e3-empty", agent.ToolSurfaceResponseAbandoned)
		successor := run.execution(t, 1, "resp-e3-after-empty")
		codingE3AssertTerminalSequence(t, run.ledger, successor, "resp-e3-after-empty", agent.ToolSurfaceResponseSettled)
	})
}

func TestCodingE3ResponseAbandonedAfterBindLocal(t *testing.T) {
	codingE3ResponseAbandonedAfterBind(t, "local")
}
func TestCodingE3ResponseAbandonedAfterBindRemote(t *testing.T) {
	codingE3ResponseAbandonedAfterBind(t, "remote")
}

func codingE3InFlightSteerSettlesPredecessorOnce(t *testing.T, family string) {
	inFlight := make(chan struct{})
	var once sync.Once
	run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, frame []byte) {
		if seq == 1 {
			once.Do(func() { close(inFlight) })
			// The watchdog closes this socket when the steer cancels the request
			// context; the handler returns on the resulting read error.
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
		codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-steered", "steered successor answer")...)
	})
	done := make(chan agent.LoopResult, 1)
	go func() { done <- codingagent.Run(run.host, "steer an in-flight request", nil, nil, nil, run.hooks()) }()
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not reach the provider")
	}
	run.loopCtx.RequestReplan()
	var result agent.LoopResult
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("steered loop did not unwind")
	}
	if result.Error != "" || result.Text != "steered successor answer" {
		t.Fatalf("result=%+v", result)
	}
	if codingE3WSConnCount(run.env) != 2 || len(run.hostBase.executions) != 2 {
		t.Fatalf("steer produced %d sockets / %d reservations, want 2/2", codingE3WSConnCount(run.env), len(run.hostBase.executions))
	}
	if run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("steer executed tools: executions=%d mcp=%d", run.control.executions, codingE3MCPCallCount(run.env))
	}
	predecessor := run.execution(t, 0, "")
	successor := run.execution(t, 1, "resp-e3-steered")
	if predecessor.ConnectionID == successor.ConnectionID || predecessor.SurfaceEpoch == successor.SurfaceEpoch {
		t.Fatalf("successor reused the predecessor tuple: %+v vs %+v", predecessor, successor)
	}
	codingE3AssertTerminalSequence(t, run.ledger, predecessor, "", agent.ToolSurfaceSteered)
	codingE3AssertTerminalSequence(t, run.ledger, successor, "resp-e3-steered", agent.ToolSurfaceResponseSettled)
	run.staticFallbackProbe(t, successor)
}

func TestCodingE3InFlightSteerSettlesPredecessorOnceLocal(t *testing.T) {
	codingE3InFlightSteerSettlesPredecessorOnce(t, "local")
}
func TestCodingE3InFlightSteerSettlesPredecessorOnceRemote(t *testing.T) {
	codingE3InFlightSteerSettlesPredecessorOnce(t, "remote")
}

func codingE3FinalizationRace(t *testing.T, family string, lateSteer bool) {
	run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, _ []byte) {
		codingE3WSWrite(t, conn, codingE3WSFinalTextFrames(fmt.Sprintf("resp-e3-fin-%d", seq), "final answer")...)
	})
	if lateSteer {
		var steerOnce sync.Once
		run.control.afterBind = func() { steerOnce.Do(func() { run.loopCtx.RequestReplan() }) }
	}
	result := codingagent.Run(run.host, "finalization race", nil, nil, nil, run.hooks())
	if result.Error != "" || result.Text != "final answer" {
		t.Fatalf("result=%+v", result)
	}
	if !lateSteer {
		execution := run.execution(t, 0, "resp-e3-fin-1")
		codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-fin-1", agent.ToolSurfaceResponseSettled)
		// A steer arriving after the reservation settled is inert: it must not
		// emit another disposition or reopen the retired holder.
		before := run.ledger.eventsFor(t, execution)
		run.loopCtx.RequestReplan()
		if after := run.ledger.eventsFor(t, execution); fmt.Sprint(after) != fmt.Sprint(before) {
			t.Fatalf("post-settlement steer changed the ledger: before=%v after=%v", before, after)
		}
		return
	}
	if codingE3WSConnCount(run.env) != 2 || len(run.hostBase.executions) != 2 {
		t.Fatalf("late steer produced %d sockets / %d reservations, want 2/2", codingE3WSConnCount(run.env), len(run.hostBase.executions))
	}
	predecessor := run.execution(t, 0, "resp-e3-fin-1")
	successor := run.execution(t, 1, "resp-e3-fin-2")
	codingE3AssertTerminalSequence(t, run.ledger, predecessor, "resp-e3-fin-1", agent.ToolSurfaceSteered)
	codingE3AssertTerminalSequence(t, run.ledger, successor, "resp-e3-fin-2", agent.ToolSurfaceResponseSettled)
}

func codingE3FinalizationRaceSeals(t *testing.T, family string) {
	codingE3FinalizationRace(t, family, true)
}
func codingE3FinalizationInertAfterSettle(t *testing.T, family string) {
	codingE3FinalizationRace(t, family, false)
}

func TestCodingE3FinalizationRaceSealsLocal(t *testing.T) { codingE3FinalizationRaceSeals(t, "local") }
func TestCodingE3FinalizationRaceSealsRemote(t *testing.T) {
	codingE3FinalizationRaceSeals(t, "remote")
}
func TestCodingE3FinalizationInertAfterSettleLocal(t *testing.T) {
	codingE3FinalizationInertAfterSettle(t, "local")
}
func TestCodingE3FinalizationInertAfterSettleRemote(t *testing.T) {
	codingE3FinalizationInertAfterSettle(t, "remote")
}

func codingE3SteerAfterBatchCommit(t *testing.T, family string) {
	run := codingE3StartRun(t, family, codingE3ScriptToolCall(t, "resp-e3-commit-steer"))
	hooks := run.hooks()
	hooks.afterCommit = func() {
		run.loopCtx.RequestReplan()
		run.control.stopFlag.Store(true)
	}
	result := codingagent.Run(run.host, "steer after commit", nil, nil, nil, hooks)
	if result.Error != "cancelled" || hooks.started != 1 || hooks.committed != 1 || run.control.executions != 1 || codingE3MCPCallCount(run.env) != 1 {
		t.Fatalf("result=%+v started=%d committed=%d executions=%d mcp=%d", result, hooks.started, hooks.committed, run.control.executions, codingE3MCPCallCount(run.env))
	}
	if codingE3WSConnCount(run.env) != 1 {
		t.Fatalf("post-commit steer created %d sockets, want exactly one", codingE3WSConnCount(run.env))
	}
	execution := run.execution(t, 0, "resp-e3-commit-steer")
	codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-commit-steer", agent.ToolSurfaceSteered)
}

func TestCodingE3SteerAfterBatchCommitLocal(t *testing.T) { codingE3SteerAfterBatchCommit(t, "local") }
func TestCodingE3SteerAfterBatchCommitRemote(t *testing.T) {
	codingE3SteerAfterBatchCommit(t, "remote")
}

func codingE3CancelDuringBatch(t *testing.T, family string) {
	run := codingE3StartRun(t, family, codingE3ScriptToolCall(t, "resp-e3-cancel-batch"))
	run.control.afterBind = func() { run.control.stopFlag.Store(true) }
	hooks := run.hooks()
	result := codingagent.Run(run.host, "cancel during batch", nil, nil, nil, hooks)
	if result.Error != "cancelled" || hooks.started != 1 || hooks.committed != 0 || hooks.abandoned != 1 || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("result=%+v started=%d committed=%d abandoned=%d executions=%d mcp=%d", result, hooks.started, hooks.committed, hooks.abandoned, run.control.executions, codingE3MCPCallCount(run.env))
	}
	execution := run.execution(t, 0, "resp-e3-cancel-batch")
	codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-cancel-batch", agent.ToolSurfaceRuntimeTerminal)
}

func TestCodingE3CancelDuringBatchLocal(t *testing.T)  { codingE3CancelDuringBatch(t, "local") }
func TestCodingE3CancelDuringBatchRemote(t *testing.T) { codingE3CancelDuringBatch(t, "remote") }

func codingE3TransportFailureInFlight(t *testing.T, family string) {
	run := codingE3StartRun(t, family, func(_ int, _ *websocket.Conn, _ []byte) {
		// The provider accepts the frame and then dies without any response:
		// the socket close is an abnormal transport outcome for this request.
	})
	result := codingagent.Run(run.host, "transport failure", nil, nil, nil, run.hooks())
	if result.Error == "" || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("result=%+v executions=%d mcp=%d", result, run.control.executions, codingE3MCPCallCount(run.env))
	}
	execution := run.execution(t, 0, "")
	codingE3AssertTerminalSequence(t, run.ledger, execution, "", agent.ToolSurfaceTransportFailure)
	run.staticFallbackProbe(t, execution)
}

func TestCodingE3TransportFailureInFlightLocal(t *testing.T) {
	codingE3TransportFailureInFlight(t, "local")
}
func TestCodingE3TransportFailureInFlightRemote(t *testing.T) {
	codingE3TransportFailureInFlight(t, "remote")
}

func codingE3PublicationFailureIsIntegrityTerminal(t *testing.T, family string) {
	run := codingE3StartRun(t, family, codingE3ScriptFinalText(t, "resp-e3-pubfail", "unreachable"))
	coordinator, err := run.env.handler.app.semanticExecutionCoordinatorForApp()
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	result := codingagent.Run(run.host, "publication failure", nil, nil, nil, run.hooks())
	// With the durable coordinator closed, the reservation's own lineage read
	// fails before any render: the loop surfaces one integrity failure, no
	// surface is published, no frame is written, and admission stays closed.
	if !strings.Contains(result.Error, "surface_integrity_failure") || run.control.executions != 0 {
		t.Fatalf("result=%+v executions=%d", result, run.control.executions)
	}
	if len(run.hostBase.executions) != 0 {
		t.Fatalf("publication failure rendered a surface: %+v", run.hostBase.executions)
	}
	var relay *codingBoundDynamicRequestLifecycleRelay
	switch cb := run.cb.(type) {
	case *codingSubAgentCallbacks:
		relay = cb.dynamicLifecycleRelay
	case *remoteCodingCallbacks:
		relay = cb.dynamicLifecycleRelay
	}
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg()); err == nil || !strings.Contains(err.Error(), "lineage is unavailable") {
		t.Fatalf("publication failure admitted a successor: %v", err)
	}
}

func TestCodingE3PublicationFailureIsIntegrityTerminalLocal(t *testing.T) {
	codingE3PublicationFailureIsIntegrityTerminal(t, "local")
}
func TestCodingE3PublicationFailureIsIntegrityTerminalRemote(t *testing.T) {
	codingE3PublicationFailureIsIntegrityTerminal(t, "remote")
}

func codingE3TerminalCloseAfterSettle(t *testing.T, family string) {
	for _, tc := range []struct {
		name   string
		reason codingBoundDynamicRequestTerminalReason
	}{
		{name: "runtime terminal", reason: codingBoundDynamicRequestRuntimeClosed},
		{name: "nested exit", reason: codingBoundDynamicRequestNestedExit},
		{name: "route supersede", reason: codingBoundDynamicRequestRouteSuperseded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, frame []byte) {
				if seq == 1 {
					codingE3WSWrite(t, conn, codingE3WSToolCallFrames("resp-e3-settle-close", codingE3WSReadOnlyToolName(t, frame), `{"query":"e3"}`)...)
					return
				}
				codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-settle-final", "settle complete")...)
			})
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				cb.registerDynamicLifecycleOwner()
			case *remoteCodingCallbacks:
				cb.registerDynamicLifecycleOwner()
			}
			result := codingagent.Run(run.host, "settle then close", nil, nil, nil, run.hooks())
			if result.Error != "" || result.Text != "settle complete" || run.control.executions != 1 {
				t.Fatalf("result=%+v executions=%d", result, run.control.executions)
			}
			execution := run.execution(t, 0, "resp-e3-settle-close")
			before := run.ledger.eventsFor(t, execution)
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				cb.subagent.closeCodingSubAgentDynamicLifecycle(tc.reason)
			case *remoteCodingCallbacks:
				cb.agent.closeCodingSubAgentDynamicLifecycle(tc.reason)
			}
			// The reservation already emitted its one disposition; the same host
			// terminal fact afterwards must neither add a disposition nor reopen
			// the retired holder.
			if after := run.ledger.eventsFor(t, execution); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("duplicate terminal fact changed the ledger: before=%v after=%v", before, after)
			}
			codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e3-settle-close", agent.ToolSurfaceToolBatchSettled)
			// A successor reservation is admitted only after the predecessor's
			// terminal write and is never disturbed by the predecessor.
			var relay *codingBoundDynamicRequestLifecycleRelay
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				relay = cb.dynamicLifecycleRelay
			case *remoteCodingCallbacks:
				relay = cb.dynamicLifecycleRelay
			}
			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg()); err != nil {
				t.Fatalf("successor reservation after terminal: %v", err)
			}
			relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
			if relay.TerminalDurabilityError() != nil {
				t.Fatalf("late predecessor disposition latched an error: %v", relay.TerminalDurabilityError())
			}
			relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

func TestCodingE3TerminalCloseAfterSettleLocal(t *testing.T) {
	codingE3TerminalCloseAfterSettle(t, "local")
}
func TestCodingE3TerminalCloseAfterSettleRemote(t *testing.T) {
	codingE3TerminalCloseAfterSettle(t, "remote")
}
