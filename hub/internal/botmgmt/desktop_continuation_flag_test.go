package botmgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A hand-back or resume turn is admitted as a continuation. That mark rides
// the run, the durable pin, and the delivery, so no layer has to guess from
// the control text again.
func TestDesktopContinuationFlagReachesDelivery(t *testing.T) {
	job := admittedDesktopFollow{
		TenantID: "tenant-a", UserID: "alice", BotID: "bot_p", RunID: "run-cont",
		Task:         "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。",
		Continuation: true,
	}
	state := newRunningDesktopTask(job)
	state.mu.Lock()
	state.done = true
	state.reply = Reply{Text: "做成了。", SendToUser: "做成了。"}
	delivery := deliveryFromState(state)
	if !delivery.Continuation {
		t.Fatal("delivery lost the continuation mark")
	}
	if delivery.Task == "" {
		t.Fatal("delivery dropped the run's own task text")
	}
	pin := state.pinLocked()
	state.mu.Unlock()
	if pin == nil || !pin.Continuation {
		t.Fatalf("pin = %+v", pin)
	}

	plain := newRunningDesktopTask(admittedDesktopFollow{TenantID: "tenant-a", UserID: "alice", BotID: "bot_p", RunID: "run-plain", Task: "发小红书"})
	plain.mu.Lock()
	plainDelivery := deliveryFromState(plain)
	plain.mu.Unlock()
	if plainDelivery.Continuation {
		t.Fatal("a new task must not be marked as a continuation")
	}
}

// A restart restores the pin, and the restored run still reports the mark.
func TestDesktopContinuationFlagSurvivesRestore(t *testing.T) {
	settings := &memSettings{}
	svc := NewService(settings)
	job := admittedDesktopFollow{
		TenantID: "tenant-a", UserID: "alice", BotID: "bot_p", RunID: "run-cont",
		Task: "交还给你", Continuation: true,
	}
	state := newRunningDesktopTask(job)
	state.mu.Lock()
	state.done = true
	state.reply = Reply{Text: "做成了。"}
	pin := state.pinLocked()
	state.mu.Unlock()

	svc.mu.Lock()
	svc.restoreDesktopTasks("tenant-a", map[string]*desktopTaskPin{
		desktopAdmitKey("tenant-a", "alice", "bot_p", "run-cont"): pin,
	})
	restored := svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_p", "run-cont")]
	svc.mu.Unlock()
	if restored == nil {
		t.Fatal("run not restored")
	}
	restored.mu.Lock()
	delivery := deliveryFromState(restored)
	restored.mu.Unlock()
	if !delivery.Continuation {
		t.Fatal("restored run lost the continuation mark")
	}
}

// A status answer never names the control instruction. The continuation is
// named for what it is — the work the person handed back.
func TestDesktopContinuationStatusLabel(t *testing.T) {
	cont := DesktopTaskStatus{RunID: "run-cont", Task: "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。", Status: "running", Continuation: true}
	line := formatDesktopTaskRunning([]DesktopTaskStatus{cont}, "")
	if line != "在做。这件是「刚才交还的那件」。" {
		t.Fatalf("running line = %q", line)
	}
	if got := formatDesktopTaskRunning([]DesktopTaskStatus{cont}, "en"); got != `Working on "the one you handed back".` {
		t.Fatalf("en line = %q", got)
	}
	plain := DesktopTaskStatus{RunID: "run-plain", Task: "发小红书", Status: "running"}
	if got := formatDesktopTaskRunning([]DesktopTaskStatus{plain}, ""); got != "在做。这件是「发小红书」。" {
		t.Fatalf("plain line = %q", got)
	}
}

// End to end through the real admit path: a hand-back with nothing to join
// starts its own run, and that run carries the mark — the delivery no client
// has to guess about.
func TestDesktopContinuationAdmitMarksTheNewRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_cont","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_cont","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	adm, err := svc.AdmitDesktopMessage(WithDesktopContinuation(context.Background()), "tenant-a", "alice", "bot_alice", "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。", "execute", "turn-hb")
	if err != nil || adm.RunID != "run_cont" {
		t.Fatalf("admit = %+v %v", adm, err)
	}
	svc.mu.Lock()
	state := svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_cont")]
	svc.mu.Unlock()
	if state == nil {
		t.Fatal("continuation run never adopted")
	}
	state.mu.Lock()
	marked := state.continuation
	state.mu.Unlock()
	if !marked {
		t.Fatal("a hand-back run lost its continuation mark at admission")
	}
}

// An ack never quotes the control instruction. The continuation falls back to
// the generic line; a real request is still named.
func TestDesktopContinuationAckStaysGeneric(t *testing.T) {
	control := "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"
	if got := desktopStartedAck(desktopAckContent(control, true)); got != "收到，这就开始做。" {
		t.Fatalf("started ack = %q", got)
	}
	if got := desktopQueuedAck(desktopAckContent(control, true)); got != "收到，这条先排着，忙完就接着做。" {
		t.Fatalf("queued ack = %q", got)
	}
	if got := desktopStartedAck(desktopAckContent("发小红书", false)); got != "收到「发小红书」，这就开始做。" {
		t.Fatalf("plain ack = %q", got)
	}
}
