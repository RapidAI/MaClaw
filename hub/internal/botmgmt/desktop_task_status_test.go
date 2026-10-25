package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDesktopTaskStatusQueryAndOfflineDelivery(t *testing.T) {
	var gate sync.Mutex
	ready := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			goNow := ready
			gate.Unlock()
			if !goNow {
				_, _ = w.Write([]byte(`{"id":"run_report","status":"running"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"run_report","status":"succeeded","desktop_ready":true,"message":{"content":"我点了下载，文件在下载目录。","metadata":{"send_to_user":"报表已下载"}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "下载报表") {
				http.Error(w, "task", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_report","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.admitPollInterval = 10 * time.Millisecond
	svc.admitReadyGrace = 20 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}

	idle, idleItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(idleItems) != 0 || idle != "现在没有在做的事。" || strings.Contains(idle, "没有找到") {
		t.Fatalf("idle = %q %#v", idle, idleItems)
	}

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "下载报表并保存", "execute", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	text, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "那个下载报表怎么样了", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "running" || items[0].RunID != admission.RunID {
		t.Fatalf("running query = %q %#v", text, items)
	}
	if !strings.Contains(text, "在做。这件是「") || !strings.Contains(text, "下载报表") || strings.Contains(text, "还在进行") {
		t.Fatalf("running sentence = %q", text)
	}
	live, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(live, "在做。这件是「") || !strings.Contains(live, "下载报表") || strings.Contains(live, "没有找到") {
		t.Fatalf("live sentence = %q", live)
	}

	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 && pending[0].Reply.Text == "我点了下载，文件在下载目录。" && pending[0].Reply.SendToUser == "报表已下载" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending = %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}

	restarted := NewService(settings)
	restarted.HTTP = srv.Client()
	restarted.Desktop = svc.Desktop
	pending, err := restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RunID != admission.RunID || pending[0].Reply.Text != "我点了下载，文件在下载目录。" || pending[0].Reply.SendToUser != "报表已下载" {
		t.Fatalf("restarted pending = %#v", pending)
	}
	shown, shownItems, err := restarted.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "下载报表", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "已经做完") || !strings.Contains(shown, "报表已下载") || strings.Contains(shown, "没有找到") || strings.Contains(shown, "下载目录") {
		t.Fatalf("finished sentence = %q", shown)
	}
	if len(shownItems) != 1 || shownItems[0].Text != "报表已下载" {
		t.Fatalf("status item = %#v", shownItems)
	}
	miss, _, err := restarted.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "另一个完全无关的名字", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(miss, "我没对上你说的那件") || !strings.Contains(miss, "下载报表") || strings.Contains(miss, "没有找到") {
		t.Fatalf("miss = %q", miss)
	}
	if err := restarted.AckDesktopTask(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID); err != nil {
		t.Fatal(err)
	}
	pending, err = restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("acked pending = %#v", pending)
	}
	again := NewService(settings)
	again.HTTP = srv.Client()
	pending, err = again.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("ack did not stick across restart: %#v", pending)
	}
}

func TestDesktopTaskHintDropsStatusWords(t *testing.T) {
	if got := desktopTaskHint("那个下载报表怎么样了"); got != "下载报表" {
		t.Fatalf("hint = %q", got)
	}
	if got := desktopTaskHint("后台在运行吗"); got != "" {
		t.Fatalf("generic run hint = %q", got)
	}
	if got := desktopTaskHint("现在在跑吗"); got != "" {
		t.Fatalf("generic running hint = %q", got)
	}
	if got := desktopTaskHint("跑着吗"); got != "" {
		t.Fatalf("generic going hint = %q", got)
	}
	if got := desktopTaskHint("运行窗口怎么样了"); got != "运行窗口" {
		t.Fatalf("window name was swallowed: %q", got)
	}
	if got := desktopTaskHint("後台在跑嗎"); got != "" {
		t.Fatalf("traditional generic hint = %q", got)
	}
	if got := desktopTaskHint("任务状态"); got != "" {
		t.Fatalf("generic hint = %q", got)
	}
	if !desktopTaskMatches("下载的进度", "下载报表并保存") {
		t.Fatal("progress question did not match the download")
	}
	if !desktopTaskMatches("了解竞品怎么样了", "了解竞品") {
		t.Fatal("a particle inside the task name was stripped")
	}
	if desktopTaskMatches("了解竞品怎么样了", "打开报表") {
		t.Fatal("an unrelated task matched")
	}
	if got := desktopTaskHint("任务分配怎么样了"); got != "任务分配" {
		t.Fatalf("task name was split: %q", got)
	}
	if desktopTaskMatches("任务分配怎么样了", "分配座位") {
		t.Fatal("a split name matched a different task")
	}
	if got := desktopTaskHint("项目进度怎么样了"); got != "项目进度" {
		t.Fatalf("progress in the name was stripped: %q", got)
	}
	if desktopTaskMatches("项目进度怎么样了", "项目计划") {
		t.Fatal("a shortened name matched a different task")
	}
	if desktopTaskMatches("download report", "annual report") {
		t.Fatal("one shared word matched a different task")
	}
	if !desktopTaskMatches("download report", "download the quarterly report") {
		t.Fatal("both words should match the report task")
	}
	if !desktopTaskMatches("下载的报表怎么样了", "下载报表并保存") {
		t.Fatal("a linker 的 hid the report task")
	}
	if !desktopTaskMatches("目的地怎么样了", "目的地") {
		t.Fatal("的 inside the task name was removed")
	}
	narrowed := preferDesktopTaskMatches("项目的进度怎么样了", []DesktopTaskStatus{
		{Task: "项目计划", Status: "succeeded"},
		{Task: "项目进度", Status: "running"},
	})
	if len(narrowed) != 1 || narrowed[0].Task != "项目进度" {
		t.Fatalf("narrowed = %#v", narrowed)
	}
	kept := preferDesktopTaskMatches("下载的进度", []DesktopTaskStatus{{Task: "下载报表并保存", Status: "running"}})
	if len(kept) != 1 || kept[0].Task != "下载报表并保存" {
		t.Fatalf("kept = %#v", kept)
	}
}

func TestDesktopTaskStatusOrdersRunningThenNewest(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	items := []DesktopTaskStatus{
		{RunID: "old", Status: "succeeded", Started: older},
		{RunID: "new", Status: "succeeded", Started: newer},
		{RunID: "run", Status: "running", Started: older},
	}
	sortDesktopTaskStatus(items)
	if items[0].RunID != "run" || items[1].RunID != "new" || items[2].RunID != "old" {
		t.Fatalf("%#v", items)
	}
}

func TestDesktopTaskQueryStripsLeadAndSpeaksEachLanguage(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	for _, lang := range []struct{ lang, want string }{
		{"en", "Nothing is running."},
		{"zh-Hant", "現在沒有在做的事。"},
	} {
		got, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", lang.lang)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 || got != lang.want {
			t.Fatalf("%s idle = %q %#v", lang.lang, got, items)
		}
	}

	// Relative to now: a finished task older than the undelivered keep
	// window (seven days) is expired and would read as idle.
	started := time.Now().UTC().Add(-time.Hour)
	lead := "现在就执行下面这项安排。同意已经给出：文中的「下一轮」「确认后」「等你点头」以及「不实际」「先不要」都是确认前的说法，现在按其中的动作做完，不要再陈述，也不要再征求确认。顺带提到的其他还在等待确认的事不要做。\n安装 Evince"
	evince := &desktopAdmitState{
		task: lead, status: "running", userID: "alice", botID: "bot_alice", runID: "run_evince", started: started,
	}
	other := &desktopAdmitState{
		task: "打开百度", status: "running", userID: "alice", botID: "bot_alice", runID: "run_baidu", started: started.Add(time.Minute),
	}
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_evince"): evince,
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_baidu"):  other,
	}
	several, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !strings.Contains(several, "在做这几件：") || !strings.Contains(several, "安装 Evince") || !strings.Contains(several, "打开百度") {
		t.Fatalf("several = %q %#v", several, items)
	}
	if strings.Contains(several, "现在就执行") || strings.Contains(several, "不实际") || strings.Contains(several, "没有找到") {
		t.Fatalf("lead leaked = %q", several)
	}
	for _, item := range items {
		if strings.Contains(item.Task, "现在就执行") || strings.Contains(item.Task, "不实际") {
			t.Fatalf("item task = %q", item.Task)
		}
	}

	delete(svc.desktopAdmits, desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_baidu"))
	en, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "how's it going", "en")
	if err != nil {
		t.Fatal(err)
	}
	if en != `Working on "安装 Evince".` {
		t.Fatalf("en = %q", en)
	}
	hant, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "後台在跑嗎", "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	if hant != "在做。這件是「安装 Evince」。" {
		t.Fatalf("hant = %q", hant)
	}
	miss, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "另一个完全无关的名字", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(miss, "I could not match that task.") || !strings.Contains(miss, "安装 Evince") || strings.Contains(miss, "No task matches") {
		t.Fatalf("en miss = %q", miss)
	}

	evince.status = "succeeded"
	evince.done = true
	evince.finished = started.Add(time.Hour)
	evince.reply.Text = "安装过程写在这里，不给人看。"
	evince.reply.SendToUser = "装好了"
	done, doneItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(done, "已经做完") || !strings.Contains(done, "装好了") || strings.Contains(done, "在做。这件是") || strings.Contains(done, "安装过程") {
		t.Fatalf("done = %q", done)
	}
	if len(doneItems) != 1 || doneItems[0].Text != "装好了" {
		t.Fatalf("done item = %#v", doneItems)
	}
	evince.reply.SendToUser = ""
	proseOnly, proseItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proseOnly, "已经做完") || strings.Contains(proseOnly, "安装过程") || strings.Contains(proseOnly, "装好了") {
		t.Fatalf("prose leaked = %q", proseOnly)
	}
	if len(proseItems) != 1 || proseItems[0].Text != "" {
		t.Fatalf("prose item = %#v", proseItems)
	}
	evince.reply.AskUserQuestion = "下一步？"
	asked, askedItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "已经做完") || !strings.Contains(asked, "下一步？") || strings.Contains(asked, "安装过程") {
		t.Fatalf("asked = %q", asked)
	}
	if len(askedItems) != 1 || askedItems[0].Text != "下一步？" {
		t.Fatalf("asked item = %#v", askedItems)
	}
	evince.reply.AskUserQuestion = "无变化"
	fillerAsk, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fillerAsk, "无变化") || strings.Contains(fillerAsk, "安装过程") {
		t.Fatalf("filler ask = %q", fillerAsk)
	}
	evince.reply.AskUserOptionsJSON = `["继续","停下"]`
	choiceAsk, choiceItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(choiceAsk, "已经做完") || !strings.Contains(choiceAsk, "继续") || !strings.Contains(choiceAsk, "停下") || strings.Contains(choiceAsk, "无变化") || strings.Contains(choiceAsk, "安装过程") {
		t.Fatalf("choice ask = %q", choiceAsk)
	}
	if len(choiceItems) != 1 || choiceItems[0].Text != "继续\n停下" {
		t.Fatalf("choice item = %#v", choiceItems)
	}
	evince.reply.AskUserOptionsJSON = `["", "无变化"]`
	blankChoices, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blankChoices, "无变化") || strings.Contains(blankChoices, "继续") {
		t.Fatalf("blank choices = %q", blankChoices)
	}
	evince.reply.AskUserSecretName = "SITE_PASSWORD"
	secretAsk, secretItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secretAsk, "已经做完") || !strings.Contains(secretAsk, "SITE_PASSWORD") || strings.Contains(secretAsk, "无变化") || strings.Contains(secretAsk, "安装过程") {
		t.Fatalf("secret ask = %q", secretAsk)
	}
	if len(secretItems) != 1 || secretItems[0].Text != "SITE_PASSWORD" {
		t.Fatalf("secret item = %#v", secretItems)
	}
	evince.reply.AskUserQuestion = "下一步？"
	questionOverSecret, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(questionOverSecret, "下一步？") || strings.Contains(questionOverSecret, "SITE_PASSWORD") || strings.Contains(questionOverSecret, "无变化") {
		t.Fatalf("question over secret = %q", questionOverSecret)
	}
	evince.reply.AskUserQuestion = "无变化"
	evince.reply.AskUserOptionsJSON = `["继续","停下"]`
	choiceOverSecret, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(choiceOverSecret, "继续") || !strings.Contains(choiceOverSecret, "停下") || strings.Contains(choiceOverSecret, "SITE_PASSWORD") || strings.Contains(choiceOverSecret, "无变化") {
		t.Fatalf("choices over secret = %q", choiceOverSecret)
	}
	evince.reply.AskUserSecretName = ""
	evince.reply.AskUserOptionsJSON = ""
	evince.status = "failed"
	evince.err = taskStatusError("browser broke")
	evince.reply.AskUserQuestion = "下一步？"
	failedAsk, failedItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedAsk, "没有完成") || !strings.Contains(failedAsk, "browser broke") || strings.Contains(failedAsk, "下一步？") || strings.Contains(failedAsk, "安装过程") {
		t.Fatalf("failed ask = %q", failedAsk)
	}
	if len(failedItems) != 1 || failedItems[0].Text != "" {
		t.Fatalf("failed ask item = %#v", failedItems)
	}
	evince.reply.AskUserQuestion = "无变化"
	evince.reply.AskUserOptionsJSON = `["继续","停下"]`
	failedChoices, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedChoices, "没有完成") || !strings.Contains(failedChoices, "browser broke") || strings.Contains(failedChoices, "继续") || strings.Contains(failedChoices, "无变化") || strings.Contains(failedChoices, "安装过程") {
		t.Fatalf("failed choices = %q", failedChoices)
	}
	evince.err = taskStatusError("run run_evince ended as failed without a result")
	missedChoices, missedItems, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missedChoices, "没有完成") || !strings.Contains(missedChoices, "继续") || !strings.Contains(missedChoices, "停下") || strings.Contains(missedChoices, "without a result") || strings.Contains(missedChoices, "无变化") || strings.Contains(missedChoices, "安装过程") {
		t.Fatalf("missed choices = %q", missedChoices)
	}
	if len(missedItems) != 1 || missedItems[0].Text != "继续\n停下" {
		t.Fatalf("missed choice item = %#v", missedItems)
	}
	evince.reply.AskUserOptionsJSON = ""
	evince.reply.AskUserQuestion = "下一步？"
	missedQuestion, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missedQuestion, "没有完成") || !strings.Contains(missedQuestion, "下一步？") || strings.Contains(missedQuestion, "without a result") || strings.Contains(missedQuestion, "安装过程") {
		t.Fatalf("missed question = %q", missedQuestion)
	}
	evince.err = nil
	emptyErrAsk, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyErrAsk, "没有完成") || !strings.Contains(emptyErrAsk, "下一步？") || strings.Contains(emptyErrAsk, "安装过程") {
		t.Fatalf("empty error ask = %q", emptyErrAsk)
	}
	evince.err = taskStatusError("run run_evince ended as cancelled without a result")
	evince.status = "cancelled"
	evince.reply.AskUserQuestion = "无变化"
	evince.reply.AskUserSecretName = "SITE_PASSWORD"
	missedSecret, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missedSecret, "没有完成") || !strings.Contains(missedSecret, "SITE_PASSWORD") || strings.Contains(missedSecret, "without a result") || strings.Contains(missedSecret, "无变化") || strings.Contains(missedSecret, "安装过程") {
		t.Fatalf("missed secret = %q", missedSecret)
	}
	evince.status = "failed"
	evince.err = taskStatusError("browser broke")
	failedSecret, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "安装 Evince", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedSecret, "没有完成") || !strings.Contains(failedSecret, "browser broke") || strings.Contains(failedSecret, "SITE_PASSWORD") || strings.Contains(failedSecret, "无变化") || strings.Contains(failedSecret, "安装过程") {
		t.Fatalf("failed secret = %q", failedSecret)
	}
}

func TestDesktopTaskAskSurvivesRestart(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	now := time.Now()
	state := &desktopAdmitState{
		task: "打开首页", status: "succeeded", done: true, userID: "alice", botID: "bot_alice", runID: "run_ask",
		phase:   "execute",
		started: now.Add(-time.Minute), finished: now,
		reply: Reply{
			Text:               "页面已打开",
			AskUserQuestion:    "下一步？",
			AskUserInputType:   "secret",
			AskUserSecretName:  "SITE_PASSWORD",
			AskUserOptionsJSON: `["继续","停下"]`,
		},
	}
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_ask"): state,
	}
	svc.persistDesktopTasks("tenant-a")

	restarted := NewService(settings)
	pending, err := restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Phase != "execute" {
		t.Fatalf("pending = %#v", pending)
	}
	reply := pending[0].Reply
	if reply.Text != "页面已打开" || reply.SendToUser != "" || reply.AskUserQuestion != "下一步？" || reply.AskUserSecretName != "SITE_PASSWORD" || reply.AskUserInputType != "secret" || reply.AskUserOptionsJSON != `["继续","停下"]` {
		t.Fatalf("restarted reply = %#v", reply)
	}
	shown, items, err := restarted.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "下一步？") || strings.Contains(shown, "页面已打开") || len(items) != 1 || items[0].Text != "下一步？" {
		t.Fatalf("shown = %q %#v", shown, items)
	}
}

func TestDesktopTaskSendToUserSurvivesRestart(t *testing.T) {
	var gate sync.Mutex
	withSentence := false
	hideSentence := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			sentence, hidden := withSentence, hideSentence
			gate.Unlock()
			if !sentence {
				_, _ = w.Write([]byte(`{"id":"run_sentence","status":"running"}`))
				return
			}
			if hidden {
				_, _ = w.Write([]byte(`{"id":"run_sentence","status":"succeeded","desktop_ready":true,"message":{"content":"无变化"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"run_sentence","status":"succeeded","desktop_ready":true,"message":{"content":"无变化","metadata":{"send_to_user":"首页没打开。"}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "打开首页") {
				http.Error(w, "task", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_sentence","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.admitPollInterval = 10 * time.Millisecond
	svc.admitReadyGrace = 20 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}

	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "execute", "turn-sentence"); err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	withSentence = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 && pending[0].Reply.Text == "无变化" && pending[0].Reply.SendToUser == "首页没打开。" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending = %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		saved, err := settings.Get(context.Background(), storageKey("tenant-a"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(saved, `"send_to_user":"首页没打开。"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pin was not saved: %s", saved)
		}
		time.Sleep(10 * time.Millisecond)
	}
	gate.Lock()
	hideSentence = true
	gate.Unlock()

	restarted := NewService(settings)
	restarted.HTTP = srv.Client()
	restarted.Desktop = svc.Desktop
	pending, err := restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Reply.Text != "无变化" || pending[0].Reply.SendToUser != "首页没打开。" {
		t.Fatalf("restarted pending = %#v", pending)
	}
}

func TestDesktopTaskSendToUserFillsFromRunMetadata(t *testing.T) {
	var gate sync.Mutex
	ready := false
	withSentence := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			goNow, sentence := ready, withSentence
			gate.Unlock()
			if !goNow {
				_, _ = w.Write([]byte(`{"id":"run_backfill","status":"running"}`))
				return
			}
			if sentence {
				_, _ = w.Write([]byte(`{"id":"run_backfill","status":"succeeded","desktop_ready":true,"message":{"content":"无变化","metadata":{"send_to_user":"首页没打开。","ask_user_question":"下一步？","ask_user_secret_name":"SITE_PASSWORD"}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"run_backfill","status":"succeeded","desktop_ready":true,"message":{"content":"无变化"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "打开首页") {
				http.Error(w, "task", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_backfill","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.admitPollInterval = 10 * time.Millisecond
	svc.admitReadyGrace = 20 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}

	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "execute", "turn-backfill"); err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 && pending[0].Reply.Text == "无变化" && pending[0].Reply.SendToUser == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending = %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		saved, err := settings.Get(context.Background(), storageKey("tenant-a"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(saved, `"text":"无变化"`) && !strings.Contains(saved, `"send_to_user"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old pin = %s", saved)
		}
		time.Sleep(10 * time.Millisecond)
	}
	gate.Lock()
	withSentence = true
	gate.Unlock()

	restarted := NewService(settings)
	restarted.HTTP = srv.Client()
	restarted.Desktop = svc.Desktop
	var pending []DesktopTaskDelivery
	deadline = time.Now().Add(2 * time.Second)
	for {
		pending, err = restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 && pending[0].Reply.SendToUser == "首页没打开。" && pending[0].Reply.Text == "无变化" && pending[0].Reply.AskUserQuestion == "下一步？" && pending[0].Reply.AskUserSecretName == "SITE_PASSWORD" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backfill pending = %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		saved, err := settings.Get(context.Background(), storageKey("tenant-a"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(saved, `"send_to_user":"首页没打开。"`) && strings.Contains(saved, `"ask_user_question":"下一步？"`) && strings.Contains(saved, `"ask_user_secret_name":"SITE_PASSWORD"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backfill was not saved: %s", saved)
		}
		time.Sleep(10 * time.Millisecond)
	}
	gate.Lock()
	withSentence = false
	gate.Unlock()
	again := NewService(settings)
	again.HTTP = srv.Client()
	pending, err = again.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Reply.Text != "无变化" || pending[0].Reply.SendToUser != "首页没打开。" || pending[0].Reply.AskUserQuestion != "下一步？" || pending[0].Reply.AskUserSecretName != "SITE_PASSWORD" {
		t.Fatalf("saved backfill = %#v", pending)
	}
}
