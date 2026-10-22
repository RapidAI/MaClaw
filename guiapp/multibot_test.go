package guiapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/botregistry"
)

type fakeMBHub struct {
	mu      sync.Mutex
	bots    []desktopBotRosterEntry
	runs    map[string][]DesktopBotActiveRun
	crosses []string
	cancels []string
	uploads []string
}

func (f *fakeMBHub) roster(ctx context.Context) ([]desktopBotRosterEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]desktopBotRosterEntry(nil), f.bots...), nil
}
func (f *fakeMBHub) activeRuns(ctx context.Context, id string) ([]DesktopBotActiveRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[id], nil
}
func (f *fakeMBHub) cross(ctx context.Context, from, kind, target, text, order string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.crosses = append(f.crosses, fmt.Sprintf("%s|%s|%s|%s|%s", from, kind, target, text, order))
	return nil
}
func (f *fakeMBHub) cancelRun(ctx context.Context, bot, run string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, bot+"/"+run)
	f.runs[bot] = nil
	return nil
}
func (f *fakeMBHub) uploadFile(ctx context.Context, bot, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, bot+":"+filepath.Base(path))
	return "/home/desktop/Materials/" + filepath.Base(path), nil
}

func newTestMultiBot(t *testing.T) (*multiBot, *fakeMBHub) {
	t.Helper()
	hub := &fakeMBHub{
		bots: []desktopBotRosterEntry{{ID: "bot_1", Name: "值班", Primary: true}, {ID: "bot_2", Name: "发布员"}},
		runs: map[string][]DesktopBotActiveRun{},
	}
	mb := newMultiBot(hub, t.TempDir(), nil)
	t.Cleanup(mb.close)
	return mb, hub
}

func TestMultiBotListIncludesSelfAndRemoteWithLocation(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	hub.runs["bot_2"] = []DesktopBotActiveRun{{RunID: "run_9", Status: "running", Task: "发文章", Step: "打开后台"}}
	out := mb.toolListBots(map[string]interface{}{"refresh": true})
	for _, want := range []string{`"id": "local:main"`, `"location": "local"`, `"shell"`, `"id": "bot_2"`, `"location": "remote"`, `"browser"`, `"state": "running"`, `"step": "打开后台"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	bots := mb.reg.List()
	if bots[0].ID != multiBotSelfID || !bots[0].Primary {
		t.Fatalf("self not first/primary: %+v", bots[0])
	}
}

func TestMultiBotSelfTurnState(t *testing.T) {
	mb, _ := newTestMultiBot(t)
	end1 := mb.beginSelfTurn("整理本地文件")
	end2 := mb.beginSelfTurn("回答问题")
	end1()
	if b, _ := mb.reg.Get(multiBotSelfID); b.State != botregistry.StateRunning {
		t.Fatalf("self idle while a turn is still open: %s", b.State)
	}
	end2()
	if b, _ := mb.reg.Get(multiBotSelfID); b.State != botregistry.StateIdle {
		t.Fatalf("self state = %s", b.State)
	}
}

func TestMultiBotAssignUploadsFilesAndDelegatesFromHubPrimary(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	local := filepath.Join(t.TempDir(), "article.md")
	if err := os.WriteFile(local, []byte("# hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := mb.toolAssignTask(map[string]interface{}{"bot": "发布员", "task": "把文章发布到博客", "local_paths": []interface{}{local}})
	if !strings.HasPrefix(out, "handed to 发布员") {
		t.Fatalf("out = %s", out)
	}
	if len(hub.uploads) != 1 || hub.uploads[0] != "bot_2:article.md" {
		t.Fatalf("uploads = %v", hub.uploads)
	}
	if len(hub.crosses) != 1 || !strings.HasPrefix(hub.crosses[0], "bot_1|delegate|bot_2|") || !strings.Contains(hub.crosses[0], "/home/desktop/Materials/article.md") {
		t.Fatalf("crosses = %v", hub.crosses)
	}
	if b, _ := mb.reg.Get("bot_2"); b.State != botregistry.StateQueued {
		t.Fatalf("state = %s", b.State)
	}
}

func TestMultiBotAssignDestructiveNeedsConfirm(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	out := mb.toolAssignTask(map[string]interface{}{"bot": "bot_2", "task": "删除旧文章"})
	if !strings.HasPrefix(out, multiBotNeedsConfirm) || len(hub.crosses) != 0 {
		t.Fatalf("out = %s crosses=%v", out, hub.crosses)
	}
	out = mb.toolAssignTask(map[string]interface{}{"bot": "bot_2", "task": "删除旧文章", "confirm": true})
	if !strings.HasPrefix(out, "handed to") || len(hub.crosses) != 1 {
		t.Fatalf("confirmed out = %s", out)
	}
}

func TestMultiBotRefusesSecretFiles(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	secret := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(secret, []byte("K=V"), 0o600)
	out := mb.toolAssignTask(map[string]interface{}{"bot": "bot_2", "task": "use it", "local_paths": []interface{}{secret}})
	if !strings.HasPrefix(out, "error:") || len(hub.uploads) != 0 {
		t.Fatalf("out = %s", out)
	}
	if out := mb.toolShareFile(map[string]interface{}{"path": secret, "bot": "bot_2"}); !strings.HasPrefix(out, "error:") {
		t.Fatalf("share = %s", out)
	}
}

func TestMultiBotPauseHoldsThenResumeDispatches(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	mb.toolListBots(map[string]interface{}{"refresh": true})
	mb.toolPauseResume(map[string]interface{}{"bot": "bot_2"}, true)
	out := mb.toolAssignTask(map[string]interface{}{"bot": "bot_2", "task": "发布"})
	if !strings.Contains(out, "paused") || len(hub.crosses) != 0 {
		t.Fatalf("out = %s", out)
	}
	out = mb.toolPauseResume(map[string]interface{}{"bot": "bot_2"}, false)
	if !strings.Contains(out, "resumed") || len(hub.crosses) != 1 {
		t.Fatalf("resume = %s crosses=%v", out, hub.crosses)
	}
}

func TestMultiBotStopAndEvents(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	hub.runs["bot_2"] = []DesktopBotActiveRun{{RunID: "run_1", Status: "running", Task: "发文章"}}
	mb.toolListBots(map[string]interface{}{"refresh": true})
	_ = mb.unread(false)
	out := mb.toolStopBot(map[string]interface{}{"bot": "发布员"})
	if !strings.Contains(out, "run_1") || len(hub.cancels) != 1 {
		t.Fatalf("stop = %s", out)
	}
	ev := mb.toolBotEvents(map[string]interface{}{})
	if !strings.Contains(ev, `"kind": "stopped"`) || !strings.Contains(ev, `"kind": "finished"`) {
		t.Fatalf("events = %s", ev)
	}
	if again := mb.toolBotEvents(map[string]interface{}{}); strings.Contains(again, "stopped") {
		t.Fatalf("events not marked read: %s", again)
	}
}

func TestMultiBotStopWaitingUserNeedsConfirm(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	hub.runs["bot_2"] = []DesktopBotActiveRun{{RunID: "run_1", Status: "running", WaitingForUser: true}}
	out := mb.toolStopBot(map[string]interface{}{"bot": "bot_2"})
	if !strings.HasPrefix(out, multiBotNeedsConfirm) || len(hub.cancels) != 0 {
		t.Fatalf("out = %s", out)
	}
}

func TestMultiBotNoHubPrimaryExplains(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	hub.bots[0].Primary = false
	out := mb.toolSendToBot(map[string]interface{}{"bot": "bot_2", "text": "你好"})
	if !strings.Contains(out, "primary") {
		t.Fatalf("out = %s", out)
	}
}

func TestMultiBotDeliveryFilesBecomeArtifacts(t *testing.T) {
	mb, _ := newTestMultiBot(t)
	mb.recordDeliveryArtifacts("bot_2", "run_1", []DesktopBotFile{{Name: "shot.png", MIME: "image/png", Data: "aGVsbG8="}})
	out := mb.toolListArtifacts(nil)
	if !strings.Contains(out, "shot.png") || !strings.Contains(out, `"from": "bot_2"`) {
		t.Fatalf("artifacts = %s", out)
	}
}

func TestMultiBotRosterLine(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	hub.runs["bot_2"] = []DesktopBotActiveRun{{RunID: "r", Status: "running", Task: "发文章", Step: "上传图片"}}
	mb.toolListBots(map[string]interface{}{"refresh": true})
	if got := mb.rosterLine("bot_2"); !strings.Contains(got, "state=running") || !strings.Contains(got, "step=上传图片") {
		t.Fatalf("line = %s", got)
	}
}

func TestRegisterMultiBotTools(t *testing.T) {
	mb, _ := newTestMultiBot(t)
	reg := NewToolRegistry()
	registerMultiBotTools(reg, mb)
	for _, name := range multiBotToolNames() {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("%s not registered", name)
		}
	}
}
