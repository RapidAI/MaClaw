package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

type receiptTurn struct {
	content  string
	result   string
	userMeta map[string]string
	meta     map[string]string
	shot     bool
	file     bool
	fileName string
}

type receiptExecutor struct {
	turn receiptTurn
}

func (e *receiptExecutor) Execute(ctx context.Context, _ agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	if e.turn.shot && !noteDesktopShot(ctx, "image/png", "aGVsbG8=") {
		return nil, context.Canceled
	}
	if e.turn.file {
		body := []byte("%PDF-1.4")
		mime := "application/pdf"
		if strings.HasSuffix(e.turn.fileName, ".png") {
			body = []byte("aGVsbG8=")
			mime = "image/png"
		}
		if err := noteDesktopFile(ctx, e.turn.fileName, mime, body); err != nil {
			return nil, err
		}
	}
	return &agentservice.ExecuteResult{Content: e.turn.result, Metadata: e.turn.meta}, nil
}

type receiptView struct {
	svc          *agentservice.Service
	server       *HTTPServer
	principal    agentservice.Principal
	token        string
	run          *agentservice.Run
	message      *agentservice.Message
	ready        bool
	receiptError string
}

func TestDesktopReceiptPersistsVisibleOutcome(t *testing.T) {
	answer := "巴黎是法国的首都，塞纳河从城中穿过。"
	cases := []struct {
		name, wantOutcome, wantPart, rejectPart string
		turn                                    receiptTurn
		forbidDone                              bool
	}{
		{
			name:        "text answer on a computer",
			turn:        receiptTurn{content: "法国的首都是哪里", result: answer},
			wantOutcome: "",
			wantPart:    answer,
			rejectPart:  "没做完",
		},
		{
			name:        "text answer without a computer",
			turn:        receiptTurn{content: "法国的首都是哪里", result: answer, userMeta: map[string]string{"bot_desktop": "none"}},
			wantOutcome: agentservice.TaskOutcomeDone,
			wantPart:    answer,
			rejectPart:  "没做完",
		},
		{
			name:       "process screenshot does not complete a publish",
			turn:       receiptTurn{content: "在屏幕上把文章发到知乎", result: "已经全部发好了", shot: true},
			wantPart:   "已经全部发好了",
			rejectPart: "没做完",
			forbidDone: true,
		},
		{
			name:        "real file delivery",
			turn:        receiptTurn{content: "做一份报告", file: true, fileName: "report.pdf"},
			wantOutcome: agentservice.TaskOutcomeDone,
			wantPart:    "report.pdf",
		},
		{
			name:        "delivered screenshot file",
			turn:        receiptTurn{content: "把当前画面交给我", file: true, fileName: "screen.png"},
			wantOutcome: agentservice.TaskOutcomeDone,
			wantPart:    "screen.png",
		},
		{
			name:        "nothing said and nothing delivered",
			turn:        receiptTurn{content: "把报表交出来"},
			wantOutcome: agentservice.TaskOutcomeBlocked,
			wantPart:    "没做完",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			view := runReceiptTurn(t, root, tc.turn)
			assertReceipt(t, view.message, view.run, tc.wantOutcome, tc.wantPart, tc.rejectPart, tc.forbidDone)
			revised := desktopEnsureReceipt(agentruntime.Scope{TenantID: view.principal.TenantID, UserID: view.principal.UserID, InstanceID: view.run.InstanceID}, view.run, view.message, nil, false, false)
			if err := view.svc.PersistRunReceipt(view.run, revised); err != nil {
				t.Fatal(err)
			}
			stored, err := view.svc.GetMessage(context.Background(), view.principal, view.run.InstanceID, view.run.SessionID, view.run.AssistantMessageID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(stored.Content, "没做完") > 1 || strings.Count(stored.Metadata["send_to_user"], "没做完") > 1 {
				t.Fatalf("second finalize stacked the sentence: %#v", stored)
			}
			assertReceipt(t, stored, view.run, tc.wantOutcome, tc.wantPart, tc.rejectPart, tc.forbidDone)
			_ = view.svc.Close()
			view.server.Close()
			reopened := reopenReceipt(t, root, view)
			assertReceipt(t, reopened.message, reopened.run, tc.wantOutcome, tc.wantPart, tc.rejectPart, tc.forbidDone)
			_ = reopened.svc.Close()
		})
	}
}

func TestFilenameOnlyReferenceIsNotADelivery(t *testing.T) {
	root := t.TempDir()
	svc, principal := newReceiptService(t, root, &receiptExecutor{})
	inst := createReceiptInstance(t, svc, principal)
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	done := time.Now().Add(-time.Minute)
	run := &agentservice.Run{ID: "run_name_only", TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, Metadata: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}, StartedAt: done, CompletedAt: &done}
	msg := &agentservice.Message{
		ID: "msg_name_only", SessionID: sess.ID, TenantID: principal.TenantID, UserID: principal.UserID, InstanceID: inst.ID,
		Role:        agentservice.MessageRoleAssistant,
		Metadata:    map[string]string{"task_outcome": agentservice.TaskOutcomeDone},
		Attachments: []agent.MessageAttachment{{Type: "file", FileName: "report.pdf", SourceMediaID: "media_1"}},
		CreatedAt:   time.Now(),
	}
	if err := svc.PersistRunReceipt(run, msg); err != nil {
		t.Fatal(err)
	}
	prepare, finish := desktopAsyncHooks(svc, principal.TenantID, principal.UserID, inst.ID, "bot_pub", nil, false, "把报表交出来")
	ctx := prepare(context.Background())
	finish(ctx, run, msg, nil)
	stored, err := svc.GetMessage(context.Background(), principal, inst.ID, sess.ID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertReceipt(t, stored, run, agentservice.TaskOutcomeBlocked, "没做完", "结果文件已经附", false)
	if strings.Contains(stored.Metadata["send_to_user"], "report.pdf") {
		t.Fatalf("filename was treated as the delivery: %q", stored.Metadata["send_to_user"])
	}
	finish(ctx, run, stored, nil)
	stored2, err := svc.GetMessage(context.Background(), principal, inst.ID, sess.ID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(stored2.Content, "没做完") != 1 || stored2.Metadata["send_to_user"] != stored.Metadata["send_to_user"] {
		t.Fatalf("repeat finalize changed the stored receipt: %#v", stored2)
	}
	visible := desktopbot.Visible(desktopbot.Turn{Prose: stored2.Content, SendToUser: stored2.Metadata["send_to_user"]})
	if !visible.Bubble || !strings.Contains(visible.Sentence, "没做完") || strings.Contains(visible.Sentence, "已经全部发好了") {
		t.Fatalf("visible protocol = %#v", visible)
	}
	_ = svc.Close()
	reopened, err := agentservice.NewService(agentservice.Config{DataRoot: root, TokenSecret: "test-token-secret-0123456789012345"}, nil, &receiptExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	again, err := reopened.GetMessage(context.Background(), principal, inst.ID, sess.ID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertReceipt(t, again, run, agentservice.TaskOutcomeBlocked, "没做完", "已经全部发好了", false)
	reopenedRun, err := reopened.GetRun(context.Background(), principal, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedRun.Metadata["task_outcome"] != again.Metadata["task_outcome"] {
		t.Fatalf("reopened run %q message %q", reopenedRun.Metadata["task_outcome"], again.Metadata["task_outcome"])
	}
}

func TestDesktopReceiptPersistFailureIsNotReady(t *testing.T) {
	previous := persistDesktopReceipt
	persistDesktopReceipt = func(*agentservice.Service, *agentservice.Run, *agentservice.Message) error {
		return io.ErrClosedPipe
	}
	t.Cleanup(func() { persistDesktopReceipt = previous })
	root := t.TempDir()
	view := runReceiptTurn(t, root, receiptTurn{content: "做一份报告", file: true, fileName: "report.pdf"})
	t.Cleanup(func() {
		_ = view.svc.Close()
		view.server.Close()
	})
	if view.ready {
		t.Fatal("a failed receipt write was published as ready")
	}
	if view.receiptError == "" {
		t.Fatal("persist failure was silent")
	}
	stored, err := view.svc.GetMessage(context.Background(), view.principal, view.run.InstanceID, view.run.SessionID, view.run.AssistantMessageID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := view.svc.GetRun(context.Background(), view.principal, view.run.InstanceID, view.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["task_outcome"] != saved.Metadata["task_outcome"] {
		t.Fatalf("failed write left message %q and run %q", stored.Metadata["task_outcome"], saved.Metadata["task_outcome"])
	}
}

func assertReceipt(t *testing.T, msg *agentservice.Message, run *agentservice.Run, outcome, want, reject string, forbidDone bool) {
	t.Helper()
	if msg == nil {
		t.Fatal("missing message")
	}
	got := msg.Metadata["task_outcome"]
	if forbidDone {
		if got == agentservice.TaskOutcomeDone {
			t.Fatalf("process screenshot marked the task done: %#v", msg)
		}
	} else if got != outcome {
		t.Fatalf("message outcome = %q, want %q (%#v)", got, outcome, msg.Metadata)
	}
	visible := msg.Metadata["send_to_user"]
	if visible == "" {
		visible = msg.Content
	}
	missing := !strings.Contains(visible, want) && !strings.Contains(msg.Content, want)
	rejected := reject != "" && (strings.Contains(visible, reject) || strings.Contains(msg.Content, reject))
	if missing || rejected {
		t.Fatalf("visible text content=%q send=%q", msg.Content, msg.Metadata["send_to_user"])
	}
	shown := desktopbot.Visible(desktopbot.Turn{Prose: msg.Content, SendToUser: msg.Metadata["send_to_user"]})
	if msg.Metadata["send_to_user"] != "" && (!shown.Bubble || !strings.Contains(shown.Sentence, want)) {
		t.Fatalf("visible protocol = %#v", shown)
	}
	if run == nil {
		return
	}
	if run.Metadata["task_outcome"] != got {
		t.Fatalf("run outcome %q != message outcome %q", run.Metadata["task_outcome"], got)
	}
}

func runReceiptTurn(t *testing.T, root string, turn receiptTurn) receiptView {
	t.Helper()
	svc, principal := newReceiptService(t, root, &receiptExecutor{turn: turn})
	server := NewHTTPServer(svc, "admin-secret", nil)
	inst := createReceiptInstance(t, svc, principal)
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	runKey := desktopRunKey(principal.TenantID, principal.UserID)
	t.Cleanup(func() {
		desktopRunsMu.Lock()
		delete(desktopRuns, runKey)
		delete(desktopHolds, runKey)
		desktopRunsMu.Unlock()
	})
	payload, _ := json.Marshal(map[string]any{"content": turn.content, "client_session_key": "bot_receipt", "metadata": turn.userMeta})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "respond-async")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var admitted struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &admitted); err != nil || admitted.Run.ID == "" {
		t.Fatalf("admission %s: %v", w.Body.String(), err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var body struct {
		agentservice.Run
		DesktopReady bool                  `json:"desktop_ready"`
		ReceiptError string                `json:"receipt_error"`
		Message      *agentservice.Message `json:"message"`
	}
	for {
		read := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
		read.Header.Set("Authorization", "Bearer "+token)
		got := httptest.NewRecorder()
		server.Handler().ServeHTTP(got, read)
		if got.Code != http.StatusOK {
			t.Fatalf("run status = %d body = %s", got.Code, got.Body.String())
		}
		if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.ReceiptError != "" || (body.DesktopReady && body.Status == agentservice.RunStatusSucceeded && body.Message != nil) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish: %#v", body.Run)
		}
		time.Sleep(15 * time.Millisecond)
	}
	run := body.Run
	return receiptView{svc: svc, server: server, principal: principal, token: token, run: &run, message: body.Message, ready: body.DesktopReady, receiptError: body.ReceiptError}
}

func reopenReceipt(t *testing.T, root string, view receiptView) receiptView {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: root, TokenSecret: "test-token-secret-0123456789012345"}, nil, &receiptExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.GetRun(context.Background(), view.principal, view.run.InstanceID, view.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := svc.GetMessage(context.Background(), view.principal, view.run.InstanceID, view.run.SessionID, view.run.AssistantMessageID)
	if err != nil {
		t.Fatal(err)
	}
	return receiptView{svc: svc, principal: view.principal, run: run, message: msg}
}

func newReceiptService(t *testing.T, root string, exec agentservice.Executor) (*agentservice.Service, agentservice.Principal) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    root,
		TokenSecret: "test-token-secret-0123456789012345",
	}, nil, exec)
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	return svc, principal
}

func createReceiptInstance(t *testing.T, svc *agentservice.Service, principal agentservice.Principal) *agentservice.Instance {
	t.Helper()
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}
