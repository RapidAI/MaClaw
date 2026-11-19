package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type receiptTurn struct {
	content   string
	result    string
	userMeta  map[string]string
	meta      map[string]string
	shot      bool
	file      bool
	fileName  string
	contract  []string
	slots     []string
	files     []string
	posts     []browser.PublishState
	modelJSON string
	fileIDs   []string
	postIDs   []string
	sameBytes bool
	samePost  bool
	partial   bool
	textIDs   []string
}

type receiptExecutor struct {
	turn receiptTurn
}

func (e *receiptExecutor) Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	if e.turn.modelJSON != "" {
		prev := agent.CompleteDesktopOutcome
		agent.CompleteDesktopOutcome = func(context.Context, corelib.MaclawLLMConfig, *http.Client, []interface{}) (*llm.Response, error) {
			return &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: e.turn.modelJSON}}}}, nil
		}
		items, err := agent.ExtractDesktopDeliverableContract(ctx, corelib.MaclawLLMConfig{}, nil, e.turn.content)
		agent.CompleteDesktopOutcome = prev
		agentservice.RecordDesktopDeliverableExtraction(req.RunID, items, err)
		if len(e.turn.textIDs) > 0 {
			desktopNoteTextClaims(req.RunID, e.turn.textIDs)
		}
	} else if e.turn.slots != nil {
		noteDesktopOutputSlots(req.RunID, e.turn.slots)
	} else if e.turn.contract != nil {
		noteDesktopOutputContract(req.RunID, e.turn.contract)
	}
	if e.turn.shot && !noteDesktopShot(ctx, "image/png", "aGVsbG8=") {
		return nil, context.Canceled
	}
	for i, state := range e.turn.posts {
		itemID := ""
		if i < len(e.turn.postIDs) {
			itemID = e.turn.postIDs[i]
		}
		n := i + 1
		if e.turn.samePost {
			n = 1
		}
		url := "https://zhuanlan.zhihu.com/p/" + strconv.Itoa(n)
		if itemID != "" {
			_ = desktopRecordPublish(agentruntime.Scope{RunID: req.RunID}, browser.PublishSpec{ItemID: itemID, Title: "t", EditorURL: "https://zhuanlan.zhihu.com/write"}, &browser.PublishReport{State: state, ResultURL: url, PostID: strconv.Itoa(n)})
			continue
		}
		if i > 0 {
			desktopPublishParkCurrent(req.RunID)
		}
		posted := state
		desktopPublishRecordUpdate(agentruntime.Scope{RunID: req.RunID}, func(r *desktopPublishRecord) {
			r.State = posted
			r.URL = url
			r.PostID = strconv.Itoa(i + 1)
		})
	}
	names := append([]string(nil), e.turn.files...)
	if e.turn.file {
		names = append(names, e.turn.fileName)
	}
	for i, name := range names {
		body := []byte("%PDF-1.4-" + name)
		mime := "application/pdf"
		if strings.HasSuffix(name, ".png") {
			body = []byte("aGVsbG8=" + name)
			mime = "image/png"
		}
		if e.turn.sameBytes {
			body = []byte("%PDF-1.4-same")
		}
		itemID := ""
		if i < len(e.turn.fileIDs) {
			itemID = e.turn.fileIDs[i]
		}
		if err := noteDesktopFileItem(ctx, name, mime, body, itemID); err != nil {
			return nil, err
		}
	}
	meta := e.turn.meta
	if e.turn.partial {
		if meta == nil {
			meta = map[string]string{}
		}
		meta["task_outcome"] = agentservice.TaskOutcomePartial
	}
	return &agentservice.ExecuteResult{Content: e.turn.result, Metadata: meta}, nil
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
			turn:        receiptTurn{content: "法国的首都是哪里", result: answer, contract: []string{}},
			wantOutcome: agentservice.TaskOutcomeDone,
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
			name:        "process screenshot does not complete a publish",
			turn:        receiptTurn{content: "在屏幕上把文章发到知乎", result: "已经全部发好了", shot: true, contract: []string{"已发布的帖子"}},
			wantOutcome: agentservice.TaskOutcomeUnverified,
			wantPart:    "已经全部发好了",
			rejectPart:  "没做完",
			forbidDone:  true,
		},
		{
			name:        "partial result is not washed by a file",
			turn:        receiptTurn{content: "把文章发出去", result: "只做了一部分", file: true, fileName: "note.txt", partial: true},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "只做了一部分",
			rejectPart:  "结果文件已经附",
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
		{
			name:        "file and post, only the file",
			turn:        receiptTurn{content: "交报告并发布", file: true, fileName: "report.pdf", slots: []string{desktopContractFile, desktopContractPost}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "report.pdf",
			forbidDone:  true,
		},
		{
			name:        "opaque two items, only one file",
			turn:        receiptTurn{content: "交报告并发布", file: true, fileName: "report.pdf", contract: []string{"报告.pdf", "知乎帖子链接"}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "report.pdf",
			forbidDone:  true,
		},
		{
			name:        "file and post, incomplete post",
			turn:        receiptTurn{content: "交报告并发布", result: "已经全部发好了", posts: []browser.PublishState{browser.PublishLiveIncomplete}, slots: []string{desktopContractFile, desktopContractPost}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "但不完整",
			forbidDone:  true,
		},
		{
			name:        "file and post counts are not identity",
			turn:        receiptTurn{content: "交报告并发布", file: true, fileName: "report.pdf", posts: []browser.PublishState{browser.PublishLive}, slots: []string{desktopContractFile, desktopContractPost}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "zhuanlan.zhihu.com/p/1",
			forbidDone:  true,
		},
		{
			name:        "two files, one delivered",
			turn:        receiptTurn{content: "交两份文件", files: []string{"a.pdf"}, slots: []string{desktopContractFile, desktopContractFile}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "a.pdf",
			forbidDone:  true,
		},
		{
			name:        "two files without item ids",
			turn:        receiptTurn{content: "交两份文件", files: []string{"a.pdf", "b.pdf"}, slots: []string{desktopContractFile, desktopContractFile}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "a.pdf",
			forbidDone:  true,
		},
		{
			name:        "two posts, one live",
			turn:        receiptTurn{content: "发两篇", result: "发了一篇", posts: []browser.PublishState{browser.PublishLive}, slots: []string{desktopContractPost, desktopContractPost}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "zhuanlan.zhihu.com/p/1",
			forbidDone:  true,
		},
		{
			name:        "two posts without item ids",
			turn:        receiptTurn{content: "发两篇", result: "两篇都发出去了", posts: []browser.PublishState{browser.PublishLive, browser.PublishLive}, slots: []string{desktopContractPost, desktopContractPost}},
			wantOutcome: agentservice.TaskOutcomePartial,
			wantPart:    "zhuanlan.zhihu.com/p/2",
			forbidDone:  true,
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

var desktopReceiptWatch func(ready bool, status agentservice.RunStatus, meta map[string]string, msg *agentservice.Message)

func TestDesktopDeliverableExtractionChain(t *testing.T) {
	answer := "巴黎是法国的首都，塞纳河从城中穿过。"
	cases := []struct {
		name, model, want, part string
		turn                    receiptTurn
		forbid                  bool
		contractID              string
	}{
		{
			name: "mixed file and post",
			turn: receiptTurn{
				content: "交报告并发布", modelJSON: `{"items":[{"id":"file-1","kind":"file","text":"报告"},{"id":"post-1","kind":"post","text":"帖子"}]}`,
				file: true, fileName: "report.pdf", fileIDs: []string{"file-1"},
				posts: []browser.PublishState{browser.PublishLive}, postIDs: []string{"post-1"},
			},
			want: agentservice.TaskOutcomeDone, part: "zhuanlan.zhihu.com/p/1", contractID: "file-1",
		},
		{
			name: "two distinct files",
			turn: receiptTurn{
				content: "交两份文件", modelJSON: `{"items":[{"id":"a","kind":"file","text":"甲"},{"id":"b","kind":"file","text":"乙"}]}`,
				files: []string{"a.pdf", "b.pdf"}, fileIDs: []string{"a", "b"},
			},
			want: agentservice.TaskOutcomeDone, part: "a.pdf", contractID: `"a"`,
		},
		{
			name: "missing second file",
			turn: receiptTurn{
				content: "交两份文件", modelJSON: `{"items":[{"id":"a","kind":"file","text":"甲"},{"id":"b","kind":"file","text":"乙"}]}`,
				files: []string{"a.pdf"}, fileIDs: []string{"a"},
			},
			want: agentservice.TaskOutcomePartial, part: "a.pdf", forbid: true,
		},
		{
			name: "duplicate file bytes",
			turn: receiptTurn{
				content: "交两份文件", modelJSON: `{"items":[{"id":"a","kind":"file","text":"甲"},{"id":"b","kind":"file","text":"乙"}]}`,
				files: []string{"a.pdf", "b.pdf"}, fileIDs: []string{"a", "b"}, sameBytes: true,
			},
			want: agentservice.TaskOutcomePartial, part: "a.pdf", forbid: true,
		},
		{
			name: "duplicate post",
			turn: receiptTurn{
				content: "发两篇", modelJSON: `{"items":[{"id":"p1","kind":"post","text":"第一篇"},{"id":"p2","kind":"post","text":"第二篇"}]}`,
				posts: []browser.PublishState{browser.PublishLive, browser.PublishLive}, postIDs: []string{"p1", "p2"}, samePost: true,
			},
			want: agentservice.TaskOutcomePartial, part: "zhuanlan.zhihu.com/p/1", forbid: true,
		},
		{
			name: "unknown kind",
			turn: receiptTurn{content: "这是什么", result: answer, modelJSON: `{"items":[{"id":"d1","kind":"widget","text":"东西"}]}`},
			want: agentservice.TaskOutcomeUnverified, part: answer, forbid: true,
		},
		{
			name: "invalid model response",
			turn: receiptTurn{content: "这是什么", result: answer, modelJSON: "not json"},
			want: agentservice.TaskOutcomeUnverified, part: answer, forbid: true,
		},
		{
			name: "text only answer",
			turn: receiptTurn{content: "法国的首都是哪里", result: answer, modelJSON: `{"items":[]}`},
			want: agentservice.TaskOutcomeDone, part: answer,
		},
		{
			name: "legacy string checklist",
			turn: receiptTurn{content: "交报告并发布", modelJSON: `{"items":["报告","帖子"]}`, file: true, fileName: "report.pdf"},
			want: agentservice.TaskOutcomePartial, part: "report.pdf", forbid: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			view := runReceiptTurn(t, root, tc.turn)
			assertReceipt(t, view.message, view.run, tc.want, tc.part, "", tc.forbid)
			if tc.contractID != "" && !strings.Contains(view.run.Metadata["output_contract"], tc.contractID) {
				t.Fatalf("stored contract = %s", view.run.Metadata["output_contract"])
			}
			_ = view.svc.Close()
			view.server.Close()
			reopened := reopenReceipt(t, root, view)
			assertReceipt(t, reopened.message, reopened.run, tc.want, tc.part, "", tc.forbid)
			_ = reopened.svc.Close()
		})
	}
}

func TestPlanFileIsNotDelivery(t *testing.T) {
	view := runReceiptTurn(t, t.TempDir(), receiptTurn{
		content: "做一份报告", file: true, fileName: "report.pdf",
		userMeta: map[string]string{"bot_phase": "plan"},
	})
	t.Cleanup(func() {
		_ = view.svc.Close()
		view.server.Close()
	})
	assertReceipt(t, view.message, view.run, agentservice.TaskOutcomeUnverified, "不能确认", "", true)
}

func TestTextItemsNeedExplicitIDs(t *testing.T) {
	answer := "第一段说明原因。第二段说明做法。"
	doc := `{"items":[{"id":"t1","kind":"text","text":"原因"},{"id":"t2","kind":"text","text":"做法"}]}`
	missing := runReceiptTurn(t, t.TempDir(), receiptTurn{content: "写两段", result: answer, modelJSON: doc})
	t.Cleanup(func() {
		_ = missing.svc.Close()
		missing.server.Close()
	})
	assertReceipt(t, missing.message, missing.run, agentservice.TaskOutcomeUnverified, answer, "", true)
	both := runReceiptTurn(t, t.TempDir(), receiptTurn{content: "写两段", result: answer, modelJSON: doc, textIDs: []string{"t1", "t2"}})
	t.Cleanup(func() {
		_ = both.svc.Close()
		both.server.Close()
	})
	assertReceipt(t, both.message, both.run, agentservice.TaskOutcomeDone, answer, "", false)
}

func TestHTTPReadDuringFinalize(t *testing.T) {
	release := make(chan struct{})
	desktopReceiptHold = func() { <-release }
	t.Cleanup(func() {
		desktopReceiptHold = nil
		select {
		case <-release:
		default:
			close(release)
		}
	})
	svc, principal := newReceiptService(t, t.TempDir(), &receiptExecutor{turn: receiptTurn{
		content: "交两份文件", modelJSON: `{"items":[{"id":"a","kind":"file","text":"甲"},{"id":"b","kind":"file","text":"乙"}]}`,
		files: []string{"a.pdf", "b.pdf"}, fileIDs: []string{"a", "b"},
	}})
	server := NewHTTPServer(svc, "admin-secret", nil)
	t.Cleanup(func() {
		_ = svc.Close()
		server.Close()
	})
	inst := createReceiptInstance(t, svc, principal)
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"content": "交两份文件", "client_session_key": "bot_race"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "respond-async")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var admitted struct {
		Run agentservice.Run `json:"run"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &admitted); err != nil || admitted.Run.ID == "" {
		t.Fatalf("admission %s: %v", w.Body.String(), err)
	}
	// GET while finish is still running. The handler sanitizes a stored
	// snapshot; a shared metadata map fails under -race.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				read := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
				read.Header.Set("Authorization", "Bearer "+token)
				got := httptest.NewRecorder()
				server.Handler().ServeHTTP(got, read)
				if got.Code != http.StatusOK {
					return
				}
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	deadline := time.Now().Add(8 * time.Second)
	var body struct {
		agentservice.Run
		DesktopReady bool                  `json:"desktop_ready"`
		Message      *agentservice.Message `json:"message"`
	}
	for {
		read := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
		read.Header.Set("Authorization", "Bearer "+token)
		got := httptest.NewRecorder()
		server.Handler().ServeHTTP(got, read)
		if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.DesktopReady && body.Message != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish: status=%s", body.Status)
		}
		time.Sleep(15 * time.Millisecond)
	}
	close(stop)
	readers.Wait()
	assertReceipt(t, body.Message, &body.Run, agentservice.TaskOutcomeDone, "a.pdf", "", false)
}

func TestDesktopReceiptStaysUnreadyPastTwoSeconds(t *testing.T) {
	started := time.Now()
	desktopReceiptHold = func() { time.Sleep(2500 * time.Millisecond) }
	desktopReceiptWatch = func(ready bool, _ agentservice.RunStatus, meta map[string]string, _ *agentservice.Message) {
		if ready && time.Since(started) < 2*time.Second {
			t.Errorf("pre-receipt row became ready after %s meta=%v", time.Since(started), meta)
		}
	}
	t.Cleanup(func() {
		desktopReceiptHold = nil
		desktopReceiptWatch = nil
	})
	view := runReceiptTurn(t, t.TempDir(), receiptTurn{content: "做一份报告", file: true, fileName: "report.pdf", meta: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}})
	t.Cleanup(func() {
		_ = view.svc.Close()
		view.server.Close()
	})
	if time.Since(started) < 2*time.Second {
		t.Fatal("receipt finished before the hold")
	}
	assertReceipt(t, view.message, view.run, agentservice.TaskOutcomeDone, "report.pdf", "", false)
}

func TestDesktopAbandonedReceiptBecomesUnverified(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	desktopReceiptHold = func() { <-release }
	desktopReceiptSkipFinish = true
	desktopReceiptWatch = func(ready bool, status agentservice.RunStatus, meta map[string]string, msg *agentservice.Message) {
		outcome := ""
		if msg != nil && msg.Metadata != nil {
			outcome = msg.Metadata[agentservice.TaskOutcomeKey]
		}
		if ready && outcome == agentservice.TaskOutcomeDone {
			t.Errorf("pre-receipt done was published as final")
		}
		if !ready && status == agentservice.RunStatusSucceeded && meta[agentservice.ReceiptPendingKey] == "1" {
			once.Do(func() { close(release) })
		}
	}
	t.Cleanup(func() {
		desktopReceiptHold = nil
		desktopReceiptSkipFinish = false
		desktopReceiptWatch = nil
		select {
		case <-release:
		default:
			close(release)
		}
	})
	root := t.TempDir()
	view := runReceiptTurn(t, root, receiptTurn{content: "做一份报告", result: "已经全部发好了", meta: map[string]string{"task_outcome": agentservice.TaskOutcomeDone}})
	t.Cleanup(func() {
		_ = view.svc.Close()
		view.server.Close()
	})
	assertReceipt(t, view.message, view.run, agentservice.TaskOutcomeUnverified, "已经全部发好了", "", true)
	_ = view.svc.Close()
	reopened := reopenReceipt(t, root, view)
	t.Cleanup(func() { _ = reopened.svc.Close() })
	assertReceipt(t, reopened.message, reopened.run, agentservice.TaskOutcomeUnverified, "已经全部发好了", "", true)
	if reopened.run.Metadata[agentservice.ReceiptPendingKey] != "" || reopened.run.Metadata[agentservice.ReceiptFinalKey] != "1" {
		t.Fatalf("reopened flags = %v", reopened.run.Metadata)
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
	deadline := time.Now().Add(8 * time.Second)
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
		if desktopReceiptWatch != nil {
			desktopReceiptWatch(body.DesktopReady, body.Status, body.Metadata, body.Message)
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
