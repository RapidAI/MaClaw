package guiapp

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// --- supplementary hermetic scenarios beyond the disposition matrix cells ---

// codingE3NestedRouteInFlightClose retires the reservation through the
// execution-scoped owner while the request is in flight. On the real channel
// the lifecycle close tears the socket, so the in-flight read ends as a
// transport failure (the D2 fake-channel equivalent let the provider answer
// first and failed at bind). Either way the reservation settles exactly once
// via the owner, the loop's disposition is ignored by the retired relay, and
// a successor reservation is admitted undisturbed.
func codingE3NestedRouteInFlightClose(t *testing.T, family string) {
	for _, tc := range []struct {
		name   string
		reason codingBoundDynamicRequestTerminalReason
	}{
		{name: "nested exit", reason: codingBoundDynamicRequestNestedExit},
		{name: "route supersede", reason: codingBoundDynamicRequestRouteSuperseded},
	} {
		t.Run(family+"/"+tc.name, func(t *testing.T) {
			inFlight := make(chan struct{})
			var once sync.Once
			run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, _ []byte) {
				if seq != 1 {
					return
				}
				once.Do(func() { close(inFlight) })
				// The lifecycle close tears this socket from the client side;
				// the handler returns on the resulting read error.
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			})
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				cb.registerDynamicLifecycleOwner()
			case *remoteCodingCallbacks:
				cb.registerDynamicLifecycleOwner()
			}
			done := make(chan agent.LoopResult, 1)
			go func() {
				done <- codingagent.Run(run.host, "terminal closes an in-flight request", nil, nil, nil, run.hooks())
			}()
			select {
			case <-inFlight:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach the provider")
			}
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				cb.subagent.closeCodingSubAgentDynamicLifecycle(tc.reason)
			case *remoteCodingCallbacks:
				cb.agent.closeCodingSubAgentDynamicLifecycle(tc.reason)
			}
			var result agent.LoopResult
			select {
			case result = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("loop did not unwind after the in-flight terminal fact")
			}
			if result.Error == "" || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
				t.Fatalf("result=%+v executions=%d mcp=%d", result, run.control.executions, codingE3MCPCallCount(run.env))
			}
			execution := run.execution(t, 0, "")
			codingE3AssertTerminalSequence(t, run.ledger, execution, "", agent.ToolSurfaceTransportFailure)
			var relay *codingBoundDynamicRequestLifecycleRelay
			switch cb := run.cb.(type) {
			case *codingSubAgentCallbacks:
				relay = cb.dynamicLifecycleRelay
			case *remoteCodingCallbacks:
				relay = cb.dynamicLifecycleRelay
			}
			if relay.TerminalDurabilityError() != nil {
				t.Fatalf("in-flight terminal latched a durability error: %v", relay.TerminalDurabilityError())
			}
			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg()); err != nil {
				t.Fatalf("successor reservation after in-flight terminal: %v", err)
			}
			relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
			relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

func TestCodingE3NestedRouteInFlightCloseLocal(t *testing.T) {
	codingE3NestedRouteInFlightClose(t, "local")
}
func TestCodingE3NestedRouteInFlightCloseRemote(t *testing.T) {
	codingE3NestedRouteInFlightClose(t, "remote")
}

// codingE3TransformRaceCommitsSnapshotAcrossInFlightRequest drives the
// transform/request race through the production codingSubAgentHooks: the
// injection and its replan revision arrive from another goroutine while the
// first request is on the wire; the predecessor retires unbound and the
// successor conversation carries exactly the payload the transform boundary
// committed.
func codingE3TransformRaceCommitsSnapshotAcrossInFlightRequest(t *testing.T, family string) {
	inFlight := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var successorConversation []interface{}
	run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, frame []byte) {
		if seq == 1 {
			once.Do(func() { close(inFlight) })
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
		// The successor socket answered this reservation; the conversation it
		// received is captured from the second dispatch below, not from this
		// frame read (the frame only carries the request payload).
		_ = frame
		codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-transform", "transform race successor answer")...)
	})
	userID := "desktop-user:e3-transform-" + family
	run.loopCtx.UserID = userID
	var hooks *codingSubAgentHooks
	switch cb := run.cb.(type) {
	case *codingSubAgentCallbacks:
		hooks = cb.subagent.buildLoopHooks(cb)
	case *remoteCodingCallbacks:
		hooks = cb.agent.buildRemoteCodingLoopHooks(cb)
	}
	_ = mu
	_ = successorConversation
	go func() {
		select {
		case <-inFlight:
		case <-time.After(5 * time.Second):
			return
		}
		run.env.handler.accumulateInjection(userID, "[用户补充] redirect to the migration notes")
		run.loopCtx.RequestReplan()
	}()
	result := codingagent.Run(run.host, "transform race", nil, nil, nil, hooks)
	if result.Error != "" || result.Text != "transform race successor answer" {
		t.Fatalf("result=%+v", result)
	}
	if codingE3WSConnCount(run.env) != 2 || len(run.hostBase.executions) != 2 {
		t.Fatalf("transform race produced %d sockets / %d reservations, want 2/2", codingE3WSConnCount(run.env), len(run.hostBase.executions))
	}
	if run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("transform race executed tools: executions=%d mcp=%d", run.control.executions, codingE3MCPCallCount(run.env))
	}
	predecessor := run.execution(t, 0, "")
	successor := run.execution(t, 1, "resp-e3-transform")
	codingE3AssertTerminalSequence(t, run.ledger, predecessor, "", agent.ToolSurfaceSteered)
	codingE3AssertTerminalSequence(t, run.ledger, successor, "resp-e3-transform", agent.ToolSurfaceResponseSettled)
	// The transform boundary committed the injection: the successor request's
	// conversation must carry it. The fake provider captured it from the second
	// response.create frame.
	if !codingE3FrameContains(run.env, 2, "redirect to the migration notes") {
		t.Fatal("successor request omitted the steering payload committed by the transform boundary")
	}
}

func TestCodingE3TransformRaceCommitsSnapshotAcrossInFlightRequestLocal(t *testing.T) {
	codingE3TransformRaceCommitsSnapshotAcrossInFlightRequest(t, "local")
}
func TestCodingE3TransformRaceCommitsSnapshotAcrossInFlightRequestRemote(t *testing.T) {
	codingE3TransformRaceCommitsSnapshotAcrossInFlightRequest(t, "remote")
}

// codingE3CancelSteerRace delivers a runtime stop and a live steer together
// while the request is in flight: exactly one terminal disposition, no tool
// work, no successor reservation created to be torn down.
func codingE3CancelSteerRace(t *testing.T, family string) {
	inFlight := make(chan struct{})
	var once sync.Once
	run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, _ []byte) {
		if seq != 1 {
			return
		}
		once.Do(func() { close(inFlight) })
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	done := make(chan agent.LoopResult, 1)
	go func() { done <- codingagent.Run(run.host, "cancel steer race", nil, nil, nil, run.hooks()) }()
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach the provider")
	}
	run.control.stopFlag.Store(true)
	run.loopCtx.RequestReplan()
	var result agent.LoopResult
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel/steer race did not unwind")
	}
	if result.Error == "" || run.control.executions != 0 || codingE3MCPCallCount(run.env) != 0 {
		t.Fatalf("result=%+v executions=%d mcp=%d", result, run.control.executions, codingE3MCPCallCount(run.env))
	}
	if codingE3WSConnCount(run.env) != 1 || len(run.hostBase.executions) != 1 {
		t.Fatalf("cancel/steer race created %d sockets / %d reservations, want 1/1", codingE3WSConnCount(run.env), len(run.hostBase.executions))
	}
	execution := run.execution(t, 0, "")
	codingE3AssertTerminalSequence(t, run.ledger, execution, "", agent.ToolSurfaceSteered)
}

func TestCodingE3CancelSteerRaceSettlesReservationOnceLocal(t *testing.T) {
	codingE3CancelSteerRace(t, "local")
}
func TestCodingE3CancelSteerRaceSettlesReservationOnceRemote(t *testing.T) {
	codingE3CancelSteerRace(t, "remote")
}

// TestCodingE3DualExecutorsStayIsolatedOnOneCoordinator runs two verified
// executors through the real assembly on the same coordinator and proves one
// executor's terminal never cancels the other's reservation.
func TestCodingE3DualExecutorsStayIsolatedOnOneCoordinator(t *testing.T) {
	for _, family := range []string{"local", "remote"} {
		t.Run(family, func(t *testing.T) {
			env := newCodingE3HermeticEnv(t)
			identityA := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root-e3-a", TurnID: "turn-a"}
			identityB := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root-e3-b", TurnID: "turn-b"}
			cbA := env.newCallbacks(t, family, identityA, NewLoopContext("e3-dual-a-"+family, 1, nil))
			cbB := env.newCallbacks(t, family, identityB, NewLoopContext("e3-dual-b-"+family, 1, nil))
			relayOf := func(cb agent.LoopCallbacks) *codingBoundDynamicRequestLifecycleRelay {
				if local, ok := cb.(*codingSubAgentCallbacks); ok {
					return local.dynamicLifecycleRelay
				}
				return cb.(*remoteCodingCallbacks).dynamicLifecycleRelay
			}
			relayA, relayB := relayOf(cbA), relayOf(cbB)
			reserveRenderBind := func(relay *codingBoundDynamicRequestLifecycleRelay, epoch, responseID string) agent.ToolCallExecutionContext {
				t.Helper()
				if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), env.cfg()); err != nil {
					t.Fatalf("reserve: %v", err)
				}
				adapter := relay.active
				if adapter == nil {
					t.Fatal("reserve retained no active holder")
				}
				execution := adapter.ExecutionContext()
				execution.SurfaceEpoch = epoch
				if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
					t.Fatal("executor did not publish its surface")
				}
				execution.ResponseID = responseID
				if err := relay.BindToolSurfaceResponse(execution); err != nil {
					t.Fatalf("bind: %v", err)
				}
				return execution
			}
			execA := reserveRenderBind(relayA, "e3-dual-a-epoch", "e3-dual-a-response")
			execB := reserveRenderBind(relayB, "e3-dual-b-epoch", "e3-dual-b-response")
			aliasOf := func(relay *codingBoundDynamicRequestLifecycleRelay) string {
				// Pick the alias bound to a read-only selection so the
				// executor's dispatch needs no external-receipt coordinator.
				for name, grant := range relay.active.surface.aliases {
					if strings.Contains(grant.SelectionID, "fs.read.local") || strings.Contains(grant.SelectionID, "repo.inspect.vcs") {
						return name
					}
				}
				return ""
			}
			aliasA, aliasB := aliasOf(relayA), aliasOf(relayB)
			if aliasA == "" || aliasB == "" || aliasA == aliasB {
				t.Fatalf("executor aliases=%q %q", aliasA, aliasB)
			}

			relayA.CloseForLifecycle(codingBoundDynamicRequestRouteSuperseded)
			if got := relayA.ExecuteToolCallWithContext(aliasA, `{"query":"late"}`, "late-a", execA); got.Result != "[system rejected] stale_surface" {
				t.Fatalf("retired executor dispatched: %#v", got)
			}
			// The second executor keeps its own bound surface on the same
			// coordinator: its alias still resolves and its dispatch still
			// reaches the loopback provider.
			if got := relayB.ExecuteToolCallWithContext(aliasB, `{"query":"still-live"}`, "call-b", execB); got.Result == "[system rejected] stale_surface" {
				t.Fatalf("second executor was cancelled by the first terminal: %#v", got)
			}
			if codingE3MCPCallCount(env) != 1 {
				t.Fatalf("second executor did not reach the loopback provider exactly once: %d", codingE3MCPCallCount(env))
			}
			relayB.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

// TestCodingE3RestartRecoveryRetainsOnlyDurableBoundAuthority drives the real
// factory directly, then proves process-loss recovery restores only the
// durable bound authority: no definitions, aliases, or catalog come back.
func TestCodingE3RestartRecoveryRetainsOnlyDurableBoundAuthority(t *testing.T) {
	env := newCodingE3HermeticEnv(t)
	identity := codingE3Identity()
	adapter, err := reserveCodingBoundDynamicRequestAdapter(context.Background(), env.handler, identity, env.cfg())
	if err != nil || adapter == nil {
		t.Fatalf("real factory reservation=%#v err=%v", adapter, err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "e3-recovery-epoch"
	if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("real factory holder did not publish")
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
		break
	}
	execution.ResponseID = "e3-recovery-response"
	if err := adapter.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after the response bind. The recovered helper gets
	// only the verified identity and durable tuple: no adapter, definitions,
	// alias map, channel, or cached provider match.
	env.app.closeSemanticInvocationStore()
	recovered, err := env.handler.app.recoverCodingDurableDynamicSurface(identity, execution.Protocol, execution.ConnectionID, execution.SurfaceEpoch)
	if err != nil {
		t.Fatalf("recover bound surface: %v", err)
	}
	if len(recovered.aliases) != 0 || len(recovered.definitions) != 0 {
		t.Fatalf("recovery restored process-local dispatch state: %#v", recovered)
	}
	if _, scope, err := recovered.ResolveAlias(execution.ResponseID, alias); err != nil || scope != adapter.surface.scope {
		t.Fatalf("recovery did not retain the durable bound alias: scope=%#v err=%v", scope, err)
	}
	got := recovered.ExecuteBoundSelection(context.Background(), identity, codingDynamicCatalogSnapshot{}, env.handler, execution.Protocol, execution.ConnectionID, execution.ResponseID, "recovered-call", alias, `{"query":"after restart"}`, time.Now().UTC())
	if got.ReasonCode != "catalog_incomplete" {
		t.Fatalf("recovered bridge result=%#v, want catalog_incomplete", got)
	}
	if err := recovered.Cancel(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := recovered.ResolveAlias(execution.ResponseID, alias); err == nil {
		t.Fatal("recovered terminal route retained its alias")
	}
	if _, err := env.handler.app.recoverCodingDurableDynamicSurface(identity, execution.Protocol, execution.ConnectionID, execution.SurfaceEpoch); err == nil {
		t.Fatal("terminal route recovered after cancellation")
	}
}

// TestCodingE3ChildHandoffRetiresParentReservationBeforeChildValidation drives
// the real nested-spawn path: the parent reservation must be retired before
// any child validation/creation path returns.
func TestCodingE3ChildHandoffRetiresParentReservationBeforeChildValidation(t *testing.T) {
	env := newCodingE3HermeticEnv(t)
	identity := codingE3Identity()
	loopCtx := NewLoopContext("e3-handoff", 1, nil)
	parent := &CodingSubAgent{
		handler: env.handler, loopCtx: loopCtx, cfg: env.cfg(),
		dynamicInvocationIdentity: identity, fullEnvironment: true, projectPath: t.TempDir(),
	}
	cb := newCodingSubAgentCallbacks(parent, &TaskItem{Title: "e3 child handoff"}, "", "", nil)
	if cb.dynamicLifecycleRelay == nil {
		t.Fatal("production constructor did not attach the qualified relay")
	}
	cb.registerDynamicLifecycleOwner()
	defer cb.closeDynamicLifecycleOwner(codingBoundDynamicRequestRuntimeClosed)
	if _, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), env.cfg()); err != nil {
		t.Fatal(err)
	}
	adapter := cb.dynamicLifecycleRelay.active
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "e3-handoff-epoch"
	if definitions := cb.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("parent did not publish its surface")
	}
	execution.ResponseID = "e3-handoff-response"
	if err := cb.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	result := parent.runNestedCodingAgent(codingSpawnSpec{Role: codingRoleWorker, Task: "child work"}, nil, nil)
	if result == nil || result.Status != TaskExecFailed || !strings.Contains(result.Error, "isolated workspace") {
		t.Fatalf("nested child validation result=%#v", result)
	}
	if !adapter.terminal || cb.dynamicLifecycleRelay.active != nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("child handoff left the parent reservation live: adapter=%#v", adapter)
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
		break
	}
	if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "late-handoff-call", execution); got.Result != "[system rejected] stale_surface" {
		t.Fatalf("child handoff left an executable alias: %#v", got)
	}
}

// --- verified-ingress runner scenarios ---

func TestCodingE3VerifiedIngressRunLoopBatchSettlesThroughRealAssembly(t *testing.T) {
	codingE3VerifiedIngress(t, "batch")
}

func TestCodingE3VerifiedIngressRunLoopCancellationSettlesOnce(t *testing.T) {
	codingE3VerifiedIngress(t, "cancel")
}

func TestCodingE3VerifiedIngressChildHandoffRetiresParent(t *testing.T) {
	codingE3VerifiedIngress(t, "handoff")
}

func codingE3VerifiedIngress(t *testing.T, scenario string) {
	t.Helper()
	env := newCodingE3HermeticEnv(t)
	relations, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relations.Close() })
	subject, err := newVerifiedCodingSubject("desktop-e3", "principal-e3", "session-e3")
	if err != nil {
		t.Fatal(err)
	}
	env.seedContracts(t, "desktop-e3", "principal-e3")
	handle, err := relations.CreateCodingTask(subject, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := codingruntime.NewMemoryStore()
	loop := NewLoopContext("e3-verified-"+scenario, 1, nil)
	ctx, cancel := loop.Context()
	defer cancel()
	ledger := newTestCodingReservationLedger()
	control := &testCodingDynamicRunLoopControl{}

	switch scenario {
	case "batch":
		env.wsScript = func(seq int, conn *websocket.Conn, frame []byte) {
			if seq == 1 {
				codingE3WSWrite(t, conn, codingE3WSToolCallFrames("resp-e3-verified-batch", codingE3WSReadOnlyToolName(t, frame), `{"query":"e3"}`)...)
				return
			}
			codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e3-verified-final", "verified batch complete")...)
		}
	case "cancel":
		// The provider holds the response until the runtime cancel closes the
		// socket; the handler returns on the resulting read error.
		env.wsScript = func(_ int, conn *websocket.Conn, _ []byte) {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	case "handoff":
		env.wsScript = nil
	}

	var runResult *CodingSubAgentResult
	var attempt *codingruntime.Attempt
	var runErr error
	// The cancel scenario's frame hook must be installed before the runner
	// goroutine can dial: the WS handler reads it on its own goroutine.
	frameRead := make(chan struct{})
	if scenario == "cancel" {
		var frameOnce sync.Once
		env.wsOnFrame = func(int) { frameOnce.Do(func() { close(frameRead) }) }
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runResult, attempt, runErr = runGUICodingTaskWithLedgerWithStart(ctx, store, "gui:e3-verified-"+scenario, "workflow", "phase", "D:/verified", "read", nil, func(request codingruntime.ExecutionRequest) {
			identity, ok := bindVerifiedCodingTaskHandle(relations, subject, &handle, store, request)
			if !ok || identity == nil {
				t.Error("verified ingress did not bind runtime identity")
				return
			}
			subagent := &CodingSubAgent{
				handler: env.handler, loopCtx: loop, runtimeStore: store, runtimeAttempt: &request.Attempt,
				dynamicInvocationIdentity: identity, cfg: env.cfg(), projectPath: t.TempDir(),
			}
			cb := newCodingSubAgentCallbacks(subagent, &TaskItem{Title: "e3 verified"}, "", "", nil)
			if cb.dynamicLifecycleRelay == nil {
				t.Error("verified ingress did not attach the qualified relay through the production path")
				return
			}
			cb.registerDynamicLifecycleOwner()
			defer cb.closeDynamicLifecycleOwner(codingBoundDynamicRequestRuntimeClosed)
			host := &codingE3LocalLoopHost{codingE3LoopHostBase: &codingE3LoopHostBase{LoopCallbacks: cb, ledger: ledger, control: control}}

			switch scenario {
			case "batch":
				hooks := &testCodingDynamicLifecycleBatchHooks{}
				loopResult := codingagent.Run(host, "verified batch", nil, nil, nil, hooks)
				if loopResult.Error != "" || loopResult.Text != "verified batch complete" || hooks.started != 1 || hooks.committed != 1 || control.executions != 1 {
					t.Errorf("verified batch result=%+v started=%d committed=%d executions=%d", loopResult, hooks.started, hooks.committed, control.executions)
				}
				if codingE3MCPCallCount(env) != 1 {
					t.Errorf("verified batch did not reach the loopback provider exactly once: %d", codingE3MCPCallCount(env))
				}
			case "cancel":
				loop.RegisterCancelHook(func() { _, _ = store.CancelTask(request.Task.TaskID, time.Now().UTC()) })
				loopResult := codingagent.Run(host, "verified cancel", nil, nil, nil, &testCodingDynamicLifecycleBatchHooks{})
				if loopResult.Error == "" || control.executions != 0 {
					t.Errorf("verified cancel result=%+v executions=%d", loopResult, control.executions)
				}
			case "handoff":
				parent := subagent
				parent.fullEnvironment = true
				if _, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), env.cfg()); err != nil {
					t.Errorf("handoff reserve: %v", err)
					return
				}
				adapter := cb.dynamicLifecycleRelay.active
				execution := adapter.ExecutionContext()
				execution.SurfaceEpoch = "e3-verified-handoff-epoch"
				if definitions := cb.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
					t.Errorf("handoff parent did not publish")
					return
				}
				execution.ResponseID = "e3-verified-handoff-response"
				if err := cb.BindToolSurfaceResponse(execution); err != nil {
					t.Errorf("handoff bind: %v", err)
					return
				}
				handoff := parent.runNestedCodingAgent(codingSpawnSpec{Role: codingRoleWorker, Task: "child work"}, nil, nil)
				if handoff == nil || handoff.Status != TaskExecFailed || !strings.Contains(handoff.Error, "isolated workspace") {
					t.Errorf("handoff result=%#v", handoff)
				}
				if !adapter.terminal || cb.dynamicLifecycleRelay.active != nil {
					t.Errorf("verified child handoff left parent reservation live: adapter=%#v", adapter)
				}
			}
		}, func() *CodingSubAgentResult {
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "e3 verified scenario complete"}
		})
	}()

	if scenario == "cancel" {
		select {
		case <-frameRead:
		case <-time.After(5 * time.Second):
			t.Fatal("verified request did not reach the provider")
		}
		loop.Cancel()
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("verified scenario did not finish")
	}

	switch scenario {
	case "batch":
		if runErr != nil || runResult == nil || runResult.Status != TaskExecPassed || attempt == nil || attempt.Status != codingruntime.TaskCompleted {
			t.Fatalf("verified batch result=%#v attempt=%#v err=%v", runResult, attempt, runErr)
		}
		executions := ledgerExecutionKeys(ledger)
		if len(executions) != 2 {
			t.Fatalf("verified batch reservations=%d", len(executions))
		}
		terminals := map[string]int{}
		for _, execution := range executions {
			events := ledger.eventsFor(t, execution)
			if len(events) != 5 {
				t.Fatalf("verified batch ledger=%v", events)
			}
			terminals[events[4]]++
		}
		if terminals["terminal:"+string(agent.ToolSurfaceToolBatchSettled)] != 1 || terminals["terminal:"+string(agent.ToolSurfaceResponseSettled)] != 1 {
			t.Fatalf("verified batch terminals=%v", terminals)
		}
	case "cancel":
		if runErr != nil || runResult == nil || runResult.Status != TaskExecInterrupted || attempt == nil || attempt.Status != codingruntime.TaskCancelled {
			t.Fatalf("verified cancel result=%#v attempt=%#v err=%v", runResult, attempt, runErr)
		}
		executions := ledgerExecutionKeys(ledger)
		if len(executions) != 1 {
			t.Fatalf("verified cancel reservations=%d", len(executions))
		}
		events := ledger.eventsFor(t, executions[0])
		if len(testCodingReservationTerminalEvents(events)) != 1 || !strings.HasPrefix(events[len(events)-1], "terminal:") {
			t.Fatalf("verified cancel ledger=%v", events)
		}
	case "handoff":
		if runErr != nil || runResult == nil || runResult.Status != TaskExecPassed || attempt == nil || attempt.Status != codingruntime.TaskCompleted {
			t.Fatalf("verified handoff result=%#v attempt=%#v err=%v", runResult, attempt, runErr)
		}
		if codingE3MCPCallCount(env) != 0 {
			t.Fatalf("verified handoff reached the provider: %d", codingE3MCPCallCount(env))
		}
	}
	env.handler.app.closeSemanticInvocationStore()
}

func ledgerExecutionKeys(ledger *testCodingReservationLedger) []agent.ToolCallExecutionContext {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	var executions []agent.ToolCallExecutionContext
	for key := range ledger.events {
		parts := strings.Split(key, "\x00")
		if len(parts) == 3 {
			executions = append(executions, agent.ToolCallExecutionContext{Protocol: parts[0], ConnectionID: parts[1], SurfaceEpoch: parts[2]})
		}
	}
	return executions
}

// --- assembly entry-point regressions ---

// TestCodingE3QualifiedRelayRequiresVerifiedIdentityAndStaysTestScoped proves
// the relay's only construction path requires a complete verified identity,
// and that the E3 override cannot leak into the production default: the
// registry stays disabled and aliases stay closed while it is installed.
func TestCodingE3QualifiedRelayRequiresVerifiedIdentityAndStaysTestScoped(t *testing.T) {
	env := newCodingE3HermeticEnv(t)
	cfg := env.cfg()
	if relay := newCodingBoundDynamicRequestLifecycleRelay(env.handler, nil, reserveCodingBoundDynamicRequestAdapter); relay != nil {
		t.Fatalf("relay constructed without an identity: %#v", relay)
	}
	incomplete := &trustedCodingInvocationIdentity{TenantID: "desktop"}
	if relay := newCodingBoundDynamicRequestLifecycleRelay(env.handler, incomplete, reserveCodingBoundDynamicRequestAdapter); relay != nil {
		t.Fatalf("relay constructed with an incomplete identity: %#v", relay)
	}
	if relay := newQualifiedCodingBoundDynamicRequestLifecycleRelay(env.handler, nil, cfg); relay != nil {
		t.Fatalf("qualified path constructed a relay without verified identity: %#v", relay)
	}
	if relay := newQualifiedCodingBoundDynamicRequestLifecycleRelay(env.handler, codingE3Identity(), cfg); relay == nil {
		t.Fatal("qualified path did not construct a relay for a complete verified identity")
	}
	// E5 derives the production gates from the same machine evidence used by
	// the hermetic rehearsal; the override is still isolated from alias
	// materialization because the callback has no production relay attached.
	if got := codingDynamicProductionAdapterForConfig(cfg); !got.eligible() || !got.Wired || !got.Enabled {
		t.Fatalf("production qualification did not derive complete evidence: %#v", got)
	}
	local := &codingSubAgentCallbacks{subagent: &CodingSubAgent{cfg: cfg, dynamicInvocationIdentity: codingE3Identity()}}
	if local.codingDynamicAliasesMayMaterialize() {
		t.Fatal("local callback materialized aliases while the override was installed")
	}
	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{cfg: cfg, dynamicInvocationIdentity: codingE3Identity()}}
	if remote.codingDynamicAliasesMayMaterialize() {
		t.Fatal("remote callback materialized aliases while the override was installed")
	}
}

// TestCodingE3QualificationOverrideDefaultsToProductionDisabled asserts the
// substitution point is inert outside the hermetic suite.
func TestCodingE3QualificationOverrideDefaultsToProductionDisabled(t *testing.T) {
	if codingDynamicProductionAdapterQualificationOverride != nil {
		t.Fatal("qualification override leaked outside the hermetic suite")
	}
	if got := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}); !got.eligible() || !got.Wired || !got.Enabled {
		t.Fatalf("production qualification is not derived: %#v", got)
	}
}
