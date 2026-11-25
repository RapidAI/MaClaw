package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// workflowGate answers official-model calls with the responses-API SSE shape:
// the confirm classifier gets an intent JSON back, every other turn gets a
// fixed assistant text. Stream posts and heartbeats are absorbed like the
// shared llmGate.
type workflowGate struct {
	*llmGate
}

func newWorkflowGate(t *testing.T) (*workflowGate, *httptest.Server) {
	t.Helper()
	g := &llmGate{started: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/api/internal/cloud-agents/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"enabled":true}`))
			return
		}
		g.hits.Add(1)
		g.mu.Lock()
		g.calls = append(g.calls, capturedCall{Path: r.URL.Path, Body: string(raw)})
		g.mu.Unlock()
		g.once.Do(func() { close(g.started) })
		text := "cloud-workflow-answer"
		if strings.Contains(string(raw), "意图分类器") {
			text = `{"intent":"confirm"}`
		}
		encoded, _ := json.Marshal(text)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: response.completed\ndata: {\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":%s}]}]}}\n\n", encoded)
	}))
	t.Cleanup(srv.Close)
	return &workflowGate{g}, srv
}

// modelCalls counts calls that reached the LLM path — official-mode turns all
// funnel through the loopback chat route, so the path is the officialChatPath
// constant, not a generic /api/llm/ prefix.
func (g *workflowGate) modelCalls() []capturedCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []capturedCall
	for _, call := range g.calls {
		if call.Path == officialChatPath {
			out = append(out, call)
		}
	}
	return out
}

func createWorkflowTask(t *testing.T, rt *Runtime, id, body string) (taskID, warning string) {
	t.Helper()
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_create_task", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Accepted bool   `json:"accepted"`
		TaskID   string `json:"task_id"`
		Warning  string `json:"workflow_warning"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted || ack.TaskID == "" {
		t.Fatalf("create ack: %s", raw)
	}
	return ack.TaskID, ack.Warning
}

func sendTask(t *testing.T, rt *Runtime, id, taskID, clientID, text string) {
	t.Helper()
	payload := fmt.Sprintf(`{"client_message_id":%q,"task_id":%q,"text":%q}`, clientID, taskID, text)
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_send_message", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted {
		t.Fatalf("send ack: %s", raw)
	}
}

func waitTranscript(t *testing.T, rt *Runtime, id, taskID, want string) string {
	t.Helper()
	var text string
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		text = transcript(t, rt, id, taskID)
		if strings.Contains(text, want) {
			return text
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("transcript never contained %q:\n%s", want, text)
	return ""
}

func watchWorkflow(t *testing.T, rt *Runtime, id, taskID string) map[string]any {
	t.Helper()
	payload := fmt.Sprintf(`{"task_id":%q,"session_key":"cloud-agent:%s:%s"}`, taskID, id, taskID)
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_watch_task", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Workflow map[string]any `json:"workflow"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("watch snapshot: %s", raw)
	}
	return snap.Workflow
}

// The form gate answers the turn itself: the first send never reaches the
// model, the notice lands on the transcript, and the watch snapshot carries
// the gated phase's schema for the workflow panel.
func TestCloudWorkflowFormGate(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	g, srv := newWorkflowGate(t)
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)

	taskID, warning := createWorkflowTask(t, rt, spec.ID, `{"title":"wf","workflow_template_id":"coding"}`)
	if warning != "" {
		t.Fatalf("workflow_warning = %q", warning)
	}
	wf := watchWorkflow(t, rt, spec.ID, taskID)
	if wf == nil {
		t.Fatal("watch snapshot has no workflow")
	}
	if wf["awaiting_form"] != true || wf["form_schema"] == nil {
		t.Fatalf("workflow snapshot: %+v", wf)
	}
	sendTask(t, rt, spec.ID, taskID, "cm_wf_1", "开始吧")
	waitTranscript(t, rt, spec.ID, taskID, "工作流面板填写")
	time.Sleep(300 * time.Millisecond)
	if calls := g.modelCalls(); len(calls) != 0 {
		t.Fatalf("form gate reached the model: %+v", calls)
	}
}

// A submitted form continues as a phase turn: the model sees the phase prompt
// plus the inlined form fields, and the recorded output parks the phase at
// the review gate the snapshot reports as pending_review.
func TestCloudWorkflowSubmitRunsPhase(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	g, srv := newWorkflowGate(t)
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)

	taskID, _ := createWorkflowTask(t, rt, spec.ID, `{"title":"wf","workflow_template_id":"coding"}`)
	payload := `{"task_id":` + fmt.Sprintf("%q", taskID) + `,"values":{"project_name":"demo","description":"做一个贪吃蛇"}}`
	if _, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_submit_workflow_form", []byte(payload)); err != nil {
		t.Fatal(err)
	}
	waitTranscript(t, rt, spec.ID, taskID, "cloud-workflow-answer")
	calls := g.modelCalls()
	if len(calls) == 0 {
		t.Fatal("phase turn did not reach the model")
	}
	var phaseBody string
	for _, call := range calls {
		if strings.Contains(call.Body, "需求文档") {
			phaseBody = call.Body
		}
	}
	if phaseBody == "" {
		t.Fatalf("no model call carried the phase prompt: %+v", calls)
	}
	if !strings.Contains(phaseBody, "demo") || !strings.Contains(phaseBody, "贪吃蛇") {
		t.Fatalf("form values missing from phase turn: %s", phaseBody[:min(len(phaseBody), 400)])
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		wf := watchWorkflow(t, rt, spec.ID, taskID)
		if wf != nil && wf["pending_review"] == true {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("phase never reached the review gate: %+v", watchWorkflow(t, rt, spec.ID, taskID))
}

// "确认" at the review gate classifies through the instance model and advances
// the workflow instead of being treated as an ordinary question.
func TestCloudWorkflowReviewConfirmAdvances(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	_, srv := newWorkflowGate(t)
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)

	taskID, _ := createWorkflowTask(t, rt, spec.ID, `{"title":"wf","workflow_template_id":"coding"}`)
	payload := `{"task_id":` + fmt.Sprintf("%q", taskID) + `,"values":{"project_name":"demo","description":"做一个贪吃蛇"}}`
	if _, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_submit_workflow_form", []byte(payload)); err != nil {
		t.Fatal(err)
	}
	waitTranscript(t, rt, spec.ID, taskID, "cloud-workflow-answer")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if wf := watchWorkflow(t, rt, spec.ID, taskID); wf != nil && wf["pending_review"] == true {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	sendTask(t, rt, spec.ID, taskID, "cm_wf_2", "确认")
	// The confirm lands the next phase on its own form gate ("设计" has no
	// schema — it runs; whichever lands first, the workflow must move off the
	// requirements review rather than answering the notice again).
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		wf := watchWorkflow(t, rt, spec.ID, taskID)
		if wf != nil && wf["current_phase"] != "" && wf["current_phase"] != "requirements" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("confirm did not advance the workflow: %+v", watchWorkflow(t, rt, spec.ID, taskID))
}

// cloud_create_task's expert fields persist as the task persona and lead the
// model turn's system prompt on every send.
func TestCloudTaskExpertPersona(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	g, srv := newWorkflowGate(t)
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)

	taskID, _ := createWorkflowTask(t, rt, spec.ID, `{"title":"exp","expert_name":"论文评审专家","expert_prompt":"你专门评审学术论文"}`)
	sendTask(t, rt, spec.ID, taskID, "cm_exp_1", "帮我看看这篇稿子")
	waitTranscript(t, rt, spec.ID, taskID, "cloud-workflow-answer")
	calls := g.modelCalls()
	if len(calls) == 0 {
		t.Fatal("turn did not reach the model")
	}
	if !strings.Contains(calls[len(calls)-1].Body, "论文评审专家") || !strings.Contains(calls[len(calls)-1].Body, "评审学术论文") {
		t.Fatalf("persona missing from system prompt: %s", calls[len(calls)-1].Body[:min(len(calls[len(calls)-1].Body), 600)])
	}
}

// Stopping a cloud task also cancels its workflow — the machine must not
// leave an active flow parked on the task.
func TestCloudStopTaskCancelsWorkflow(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	taskID, _ := createWorkflowTask(t, rt, spec.ID, `{"title":"wf","workflow_template_id":"coding"}`)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		t.Fatal(err)
	}
	if state := machine.GetActive(taskID); state == nil || state.Status != "active" {
		t.Fatalf("workflow not active: %+v", state)
	}
	if _, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_stop_task", []byte(`{"task_id":`+fmt.Sprintf("%q", taskID)+`}`)); err != nil {
		t.Fatal(err)
	}
	// GetActive only surfaces running flows — a cancelled one is read back
	// straight from the store.
	state, err := machine.GetStore().Load(taskID)
	if err != nil || state == nil || state.Status != "cancelled" {
		t.Fatalf("workflow not cancelled: %+v (err=%v)", state, err)
	}
	// The watch snapshot must keep reporting the terminal flow — GetActive's
	// active-only filter would blank the panel on the next poll.
	snap := rt.workflowSnapshot(inst, taskID)
	if snap == nil || snap["status"] != "cancelled" {
		t.Fatalf("snapshot lost terminal workflow: %+v", snap)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
