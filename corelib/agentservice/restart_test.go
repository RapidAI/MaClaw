package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	var resumed []string
	prev := OnRunResumed
	OnRunResumed = func(sessionID, action string) { resumed = append(resumed, sessionID+" "+action) }
	t.Cleanup(func() { OnRunResumed = prev })
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
	if len(resumed) != 1 || !strings.Contains(resumed[0], "publish") {
		t.Fatalf("replay arm %v", resumed)
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
	stashRestartTrajectory(run.ID, []agent.ConversationEntry{
		{Role: "user", Content: "publish the answer"},
		{Role: "tool", Content: "the page", ToolName: "desktop"},
	}, nil)
	svc.finishInterrupted(&run, "publish")
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
	if !strings.Contains(got.Metadata[metaRestartCheckpoint], `"uncertain_action":"publish"`) {
		t.Fatalf("checkpoint %s", got.Metadata[metaRestartCheckpoint])
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
