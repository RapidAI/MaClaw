package agentservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDrainInterruptsInsteadOfCancelling(t *testing.T) {
	executor := &blockingAsyncExecutor{started: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	defer svc.Close()
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "drain"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "keep going"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	svc.Drain(ctx)
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusInterrupted {
		t.Fatalf("status %s error %s", got.Status, got.Error)
	}
	if _, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "too late"}); !errors.Is(err, ErrServiceDraining) {
		t.Fatalf("new run during drain: %v", err)
	}
}

func TestResumeInterruptedRunOnce(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	updated, err := svc.UpdateInstance(context.Background(), principal, inst.ID, UpdateInstanceInput{Metadata: map[string]string{"hub_bot": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	inst = *updated
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "go on"})
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Status: RunStatusInterrupted, Error: runInterruptedError, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{
			metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `","uncertain_action":"publish"}`,
			metaRestartTrajectory: `{"entries":[{"role":"user","content":"keep going"},{"role":"tool","content":"page now","tool_name":"desktop"}]}`,
		},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 1 {
		t.Fatalf("resumed %d", n)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 0 {
		t.Fatalf("resumed again %d", n)
	}
	msgs, err := svc.store.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, msg := range msgs {
		if msg.Content == restartNoticeContinue || (msg.Metadata != nil && msg.Metadata["send_to_user"] == restartNoticeContinue) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("notice missing: %+v", msgs)
	}
}

func TestResumeInstallsTheReplySlotWithThePersonsAsk(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	updated, err := svc.UpdateInstance(context.Background(), principal, inst.ID, UpdateInstanceInput{Metadata: map[string]string{"hub_bot": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	inst = *updated
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "shot"})
	if err != nil {
		t.Fatal(err)
	}
	msgID := NewID("msg")
	if err := svc.store.SaveMessage(Message{
		ID: msgID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Role: MessageRoleUser, Content: "运行后截屏发给我", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var gotAsk string
	svc.ResumeExecutionHooks = func(_ Principal, _ Instance, ask string) (func(context.Context) context.Context, func(context.Context, *Run, *Message, error)) {
		gotAsk = ask
		return func(ctx context.Context) context.Context { return ctx }, func(context.Context, *Run, *Message, error) {
			close(done)
		}
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		UserMessageID: msgID,
		Status:        RunStatusInterrupted, Error: runInterruptedError, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{
			metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `"}`,
			metaRestartTrajectory: `{"entries":[{"role":"user","content":"keep going"}]}`,
		},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 1 {
		t.Fatalf("resumed %d", n)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not finish the reply slot")
	}
	if gotAsk != "运行后截屏发给我" {
		t.Fatalf("ask=%q", gotAsk)
	}
}

func TestResumeImpossibleIsTold(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Status: RunStatusInterrupted, Error: runInterruptedError, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `"}`},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 0 {
		t.Fatalf("resumed a non-desktop instance %d", n)
	}
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusFailed || got.Metadata["task_outcome_evidence"] != "interrupted by restart" {
		t.Fatalf("got %+v", got)
	}
	msgs, _ := svc.store.ListMessages(sess.ID)
	var saw bool
	for _, msg := range msgs {
		if strings.Contains(msg.Content, restartNoticeStopped) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("stop notice missing: %+v", msgs)
	}
}

func TestResumeWithoutTrajectoryIsTold(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	updated, err := svc.UpdateInstance(context.Background(), principal, inst.ID, UpdateInstanceInput{Metadata: map[string]string{"hub_bot": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	inst = *updated
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "no trail"})
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Status: RunStatusInterrupted, Error: runInterruptedError, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `"}`},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 0 {
		t.Fatalf("resumed without a trajectory %d", n)
	}
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusFailed || got.Metadata["task_outcome_evidence"] != "interrupted by restart" || got.AssistantMessageID == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestResumeClaimIsOnce(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	updated, err := svc.UpdateInstance(context.Background(), principal, inst.ID, UpdateInstanceInput{Metadata: map[string]string{"hub_bot": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	inst = *updated
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "once"})
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Status: RunStatusInterrupted, Error: runInterruptedError, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{
			metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `","resumed":true}`,
			metaRestartTrajectory: `{"entries":[{"role":"user","content":"keep going"}]}`,
		},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 0 {
		t.Fatalf("started a run that was already claimed %d", n)
	}
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusFailed {
		t.Fatalf("status %s", got.Status)
	}
}

func TestFinishInterruptedStoresTrajectory(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		Status: RunStatusRunning, StartedAt: time.Now().Add(-time.Minute),
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	stashRestartTrajectory(run.ID, []agent.ConversationEntry{
		{Role: "user", Content: "publish the answer"},
		{Role: "tool", Content: "the page", ToolName: "desktop"},
	}, nil)
	svc.finishInterrupted(&run)
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusInterrupted {
		t.Fatalf("status %s", got.Status)
	}
	traj, ok := loadRestartTrajectory(got.Metadata[metaRestartTrajectory])
	if !ok || len(traj.Entries) != 2 || traj.Entries[1].ToolName != "desktop" {
		t.Fatalf("trajectory %+v", got.Metadata[metaRestartTrajectory])
	}
}

func TestFinishInterruptedDoesNotResumeTwice(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	updated, err := svc.UpdateInstance(context.Background(), principal, inst.ID, UpdateInstanceInput{Metadata: map[string]string{"hub_bot": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	inst = *updated
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "crash"})
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		Status: RunStatusRunning, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{
			metaRestartCheckpoint: `{"session_id":"` + sess.ID + `","instance_id":"` + inst.ID + `","resumed":true,"attempts":1}`,
			metaRestartTrajectory: `{"entries":[{"role":"user","content":"keep going"},{"role":"tool","content":"cancelled","tool_call_id":"call_type"}]}`,
		},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	// The resumed run dies. The next interrupt must keep the claim and drop
	// the trajectory. Writing a fresh checkpoint here used to resume forever.
	svc.finishInterrupted(&run)
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Metadata[metaRestartCheckpoint], `"resumed":true`) {
		t.Fatalf("claim dropped: %s", got.Metadata[metaRestartCheckpoint])
	}
	if strings.TrimSpace(got.Metadata[metaRestartTrajectory]) != "" {
		t.Fatalf("stale trajectory kept: %s", got.Metadata[metaRestartTrajectory])
	}
	if n := svc.ResumeInterruptedRuns(context.Background()); n != 0 {
		t.Fatalf("resumed the crashed continuation %d", n)
	}
	got, err = svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusFailed || got.Metadata["task_outcome_evidence"] != "interrupted by restart" {
		t.Fatalf("got %+v", got.Status)
	}
}

func TestInFlightIsItsOwnRow(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		Status: RunStatusRunning, StartedAt: time.Now().Add(-time.Minute),
		Metadata: map[string]string{"keep": "1"},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordInFlight(principal, inst.ID, run.ID, RestartInFlight{ID: "c_write", Name: "write_file", Args: `{"path":"a"}`}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordInFlight(principal, inst.ID, run.ID, RestartInFlight{ID: "c_bash", Name: "bash", Args: `{"command":"make"}`}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata["keep"] != "1" || strings.Contains(got.Metadata[metaRestartCheckpoint], "write_file") {
		t.Fatalf("run row was rewritten: %+v", got.Metadata)
	}
	if len(svc.listInFlight(run.ID)) != 2 {
		t.Fatalf("calls %+v", svc.listInFlight(run.ID))
	}
	if err := svc.ClearInFlight(principal, inst.ID, run.ID, "c_write"); err != nil {
		t.Fatal(err)
	}
	left := svc.listInFlight(run.ID)
	if len(left) != 1 || left[0].ID != "c_bash" {
		t.Fatalf("cleared the wrong call: %+v", left)
	}
}

func TestUnclassifiedToolChangesState(t *testing.T) {
	desktop := []agentruntime.ToolDefinition{{
		Name: "desktop",
		ChangesState: func(args map[string]any) bool {
			action, _ := args["action"].(string)
			return action != "probe"
		},
	}}
	for _, name := range []string{"write_file", "edit_file", "memory", "memory_write", "mcp_custom", "send_to_im"} {
		if !toolCallChangesState(name, "", nil) {
			t.Fatalf("%s was treated as a read", name)
		}
	}
	for _, name := range []string{"read_file", "glob", "ripgrep", "web_search", "knowledge_search", "knowledge_context_pack"} {
		if toolCallChangesState(name, "", nil) {
			t.Fatalf("%s was treated as a side effect", name)
		}
	}
	if toolCallChangesState("desktop", `{"action":"probe"}`, desktop) {
		t.Fatal("probe changes state")
	}
	if !toolCallChangesState("desktop", `{"action":"click"}`, desktop) {
		t.Fatal("click was treated as a read")
	}
}

func TestInFlightCutOffIsTheCancelledCall(t *testing.T) {
	if inFlightCutOff(context.Background()) || inFlightCutOff(nil) {
		t.Fatal("a finished call was kept")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !inFlightCutOff(ctx) {
		t.Fatal("a cancelled call would be cleared")
	}
}

func TestInterruptedSideEffectIsNotReplayed(t *testing.T) {
	entries := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []any{map[string]any{"id": "call_type"}}},
		{Role: "tool", ToolCallID: "call_type", ToolName: "desktop", Content: "cancelled"},
		{Role: "assistant", ToolCalls: []any{map[string]any{"id": "call_click"}}},
		{Role: "tool", ToolCallID: "call_click", ToolName: "desktop", Content: "clicked save"},
	}
	for _, action := range []string{"type", "bash", "say"} {
		got := rewriteInterruptedResult(entries, RestartInFlight{ID: "call_type", Name: "desktop", Args: `{"action":"` + action + `"}`}, "probe: editor open")
		if got[1].Content == "cancelled" || !strings.Contains(fmt.Sprint(got[1].Content), restartOutcomeUnknown) || !strings.Contains(fmt.Sprint(got[1].Content), "probe: editor open") {
			t.Fatalf("%s result %v", action, got[1].Content)
		}
		if got[3].Content != "clicked save" {
			t.Fatalf("%s swallowed the other click: %v", action, got[3].Content)
		}
	}
}

func TestAdoptRestartTrajectoryKeepsTheCommittedSteps(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "trail"})
	if err != nil {
		t.Fatal(err)
	}
	prior := Message{ID: "msg_prior", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, Role: MessageRoleUser, Content: "earlier"}
	original := Message{ID: "msg_task", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, Role: MessageRoleUser, Content: "publish the answer"}
	noise := Message{ID: "msg_noise", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, Role: MessageRoleAssistant, Content: "flat text that must not replace the steps"}
	for _, msg := range []Message{prior, original, noise} {
		if err := svc.store.SaveMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	run := Run{
		ID: NewID("run"), TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID,
		UserMessageID: original.ID, Status: RunStatusInterrupted,
		Metadata: map[string]string{metaRestartTrajectory: `{"entries":[{"role":"user","content":"publish the answer"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"publish\"}"}}]},{"role":"tool","content":"the page now","tool_call_id":"call_1","tool_name":"desktop"}]}`},
	}
	if err := svc.store.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	cb := &coreAgentCallbacks{executor: &CoreAgentExecutor{service: svc}}
	cb.adoptRestartTrajectory(ExecuteRequest{
		Principal: principal,
		Instance:  inst,
		Session:   *sess,
		Message:   Message{ID: "msg_continue", Metadata: map[string]string{"restart_from": run.ID}},
	})
	if len(cb.history) != 4 {
		t.Fatalf("history len %d %+v", len(cb.history), cb.history)
	}
	if cb.history[0].Content != "earlier" || cb.history[1].Content != "publish the answer" || cb.history[3].ToolName != "desktop" {
		t.Fatalf("history %+v", cb.history)
	}
	if cb.history[2].ToolCalls == nil {
		t.Fatal("the committed tool call was flattened")
	}
}
