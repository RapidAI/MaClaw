package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// The self-correction mechanism end to end through the real tool entry
// (desktopRuntimeModule.InvokeTool -> invokeDesktopTool guards -> recovery
// gate -> operation -> recovery observation -> text the model reads). Only
// the machine underneath is replaced: publish runs the real pipeline in a
// headless Chrome against an offline caret-keeping editor, bash runs real
// bash in a temp dir. The "model" is a deterministic stub that reads the
// tool results and decides the next call, as the LLM does in the run loop.

func recoveryTestEnv(t *testing.T) {
	t.Helper()
	oldRoot, oldEng, oldSite := desktopRecoveryRoot, desktopRecoveryEng, siteExpRoot
	desktopRecoveryRoot, desktopRecoveryEng, siteExpRoot = t.TempDir(), nil, t.TempDir()
	oldOp, oldFile := operateDesktopFunc, desktopRemoteFile
	t.Cleanup(func() {
		desktopRecoveryRoot, desktopRecoveryEng, siteExpRoot = oldRoot, oldEng, oldSite
		operateDesktopFunc, desktopRemoteFile = oldOp, oldFile
	})
}

// recoveryTurn is one run of a person's conversation (one session per
// tenant+user in these tests).
func recoveryTurn(tenant, user, run, task string) agentruntime.TurnRequest {
	return agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: tenant, UserID: user, InstanceID: "bot-rec", RunID: run},
		Input: agentservice.ExecuteRequest{
			Instance: agentservice.Instance{ID: "bot-rec"},
			Session:  agentservice.Session{ID: "sess-" + tenant + "-" + user},
			Message:  agentservice.Message{Content: task, Metadata: map[string]string{"bot_phase": "execute"}},
		},
	}
}

func invoke(t *testing.T, req agentruntime.TurnRequest, args map[string]any) (string, error) {
	t.Helper()
	out, err := desktopRuntimeModule{}.InvokeTool(context.Background(), req, desktopToolName, args)
	text := out
	if err != nil {
		text = "ERROR " + err.Error()
	}
	t.Logf("-> %v\n<- %s", desktopRecArgsLog(args), text)
	return out, err
}

func desktopRecArgsLog(a map[string]any) string {
	cp := map[string]any{}
	for k, v := range a {
		if k == "body" {
			v = "…"
		}
		cp[k] = v
	}
	b, _ := json.Marshal(cp)
	return string(b)
}

// ---- offline editor + Chrome ----

type recSite struct {
	mu        sync.Mutex
	published []map[string]any
	uploads   int
}

func recCaretServer(t *testing.T) (*httptest.Server, *recSite) {
	html, err := os.ReadFile(filepath.Join("..", "corelib", "browser", "testdata", "publish", "csdn_caret.html"))
	if err != nil {
		t.Fatal(err)
	}
	f := &recSite{}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/md", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "UserName", Value: "tester", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		time.Sleep(800 * time.Millisecond)
		f.mu.Lock()
		f.uploads++
		n := f.uploads
		f.mu.Unlock()
		fmt.Fprintf(w, "%s/i/direct/pic%d.png", srv.URL, n)
	})
	mux.HandleFunc("/saveArticle", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		f.mu.Lock()
		if st, _ := m["status"].(float64); st == 0 {
			f.published = append(f.published, m)
		}
		id := 170300010 + len(f.published)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":200,"data":{"id":"%d"}}`, id)
	})
	mux.HandleFunc("/mp_blog/creation/success/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><body><h2>发布成功！正在审核中</h2><a href="/mp_blog/manage/article">管理文章</a></body>`)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, f
}

func (s *recSite) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.published)
}

type recDriver struct {
	c    *browser.CDPClient
	mu   sync.Mutex
	subs map[chan browser.CDPEvent]bool
}

func (d *recDriver) Send(m string, p interface{}) (json.RawMessage, error) {
	return d.c.Send(m, p, 15*time.Second)
}
func (d *recDriver) ArmChooser([]string) error               { return nil }
func (d *recDriver) WaitChooser(time.Duration) (bool, error) { return false, nil }
func (d *recDriver) DisarmChooser()                          {}
func (d *recDriver) Subscribe() (<-chan browser.CDPEvent, func()) {
	ch := make(chan browser.CDPEvent, 256)
	d.mu.Lock()
	if d.subs == nil {
		d.subs = map[chan browser.CDPEvent]bool{}
	}
	d.subs[ch] = true
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		if d.subs[ch] {
			delete(d.subs, ch)
			close(ch)
		}
		d.mu.Unlock()
	}
}

func recChrome(t *testing.T) *recDriver {
	var bin string
	for _, b := range []string{os.Getenv("CHROME_BIN"), "google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(b); b != "" && err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skip("no chrome binary")
	}
	// Not t.TempDir: Chrome's helpers may still write the profile while the
	// test cleans up.
	dir, _ := os.MkdirTemp("", "recchrome")
	cmd := exec.Command(bin, "--headless=new", "--no-sandbox", "--disable-gpu", "--remote-debugging-port=0", "--user-data-dir="+dir, "--window-size=1280,900", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Skipf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait(); _ = os.RemoveAll(dir) })
	addr := ""
	for i := 0; i < 100 && addr == ""; i++ {
		if b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort")); err == nil {
			if l := strings.Split(string(b), "\n"); l[0] != "" {
				addr = "127.0.0.1:" + strings.TrimSpace(l[0])
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if addr == "" {
		t.Skip("chrome did not start")
	}
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/json/new?about:blank", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tgt struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tgt)
	resp.Body.Close()
	c, err := browser.ConnectCDP(tgt.WS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	d := &recDriver{c: c}
	go func() {
		for ev := range c.Events() {
			d.mu.Lock()
			for ch := range d.subs {
				select {
				case ch <- ev:
				default:
				}
			}
			d.mu.Unlock()
		}
	}()
	_, _ = c.Send("Page.enable", nil, 5*time.Second)
	_, _ = c.Send("DOM.enable", nil, 5*time.Second)
	return d
}

func recPictures(t *testing.T) []string {
	dir := t.TempDir()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	var out []string
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.png", i+1))
		_ = os.WriteFile(p, append(append([]byte(nil), png...), byte(i)), 0o644)
		out = append(out, p)
	}
	return out
}

// recPublishOp is the machine under action=publish: the production spec
// mapping for strategy args and the production finish, the real pipeline
// in Chrome against the offline editor.
func recPublishOp(srv *httptest.Server, d *recDriver) func(context.Context, agentruntime.Scope, map[string]any) (string, error) {
	return func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if desktopArg(args, "action") != "publish" {
			return "", errors.New("unexpected action " + desktopArg(args, "action"))
		}
		spec := browser.PublishSpec{Files: desktopUploadFiles(args), Title: desktopArg(args, "title"), Body: desktopArg(args, "body"), StepBudget: 30 * time.Second, Deadline: time.Now().Add(3 * time.Minute)}
		if err := desktopPublishApplyStrategy(args, &spec); err != nil {
			return "", err
		}
		browser.ProfileForURL("https://editor.csdn.net/md/").Apply(&spec)
		spec.EditorURL = srv.URL + "/md"
		spec.SuccessURL = "/mp_blog/creation/success/(\\d+)"
		spec.MinImages = len(spec.Files)
		return desktopPublishExecute(scope, spec, "csdn", false, func(spec browser.PublishSpec) (*browser.PublishReport, error) {
			return browser.RunPublish(d, spec)
		})
	}
}

// recRunID is unique per test run (publish records and watchdogs are
// process-wide and keyed by run).
func recRunID(name string) string { return fmt.Sprintf("%s-%d", name, time.Now().UnixNano()) }

// recModel is the stub LLM: it only reads the last tool result.
type recModel struct {
	title, body string
	files       []string
	editor      string
	step        int
	refuted     bool
}

func (m *recModel) publish(extra map[string]any) map[string]any {
	files := []any{}
	for _, f := range m.files {
		files = append(files, f)
	}
	a := map[string]any{"action": "publish", "title": m.title, "body": m.body, "files": files, "editor_url": m.editor}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

// next decides the next call from the last result, like a model would:
// a regression with an upload that succeeded points at where the editor
// inserts, not at the site refusing pictures.
func (m *recModel) next(last string) map[string]any {
	m.step++
	switch {
	case last == "":
		return m.publish(nil)
	case strings.Contains(last, "RECOVERY_REQUIRED") && !m.refuted:
		m.refuted = true
		return map[string]any{"action": "recover", "hypothesis": "the editor is still saving the previous picture when the next one is pasted, so the new insert replaces it", "uncertainty": "the upload itself succeeded, so the save timing is only a guess", "strategy": "wait for the editor to settle before each picture", "params": map[string]any{"insert_settle_ms": 2500}, "test": "publish with trial_pictures=2 (no submit)", "expect": []any{"pictures_kept>=2"}}
	case strings.Contains(last, "RECOVERY_REQUIRED"):
		return map[string]any{"action": "recover", "hypothesis": "picture 2 replaced picture 1's link although both uploads were stored: the editor inserts at a caret it remembers itself, which still sits on picture 1's caption; a script-set selection does not move it", "uncertainty": "whether native caret keys update the editor's own caret", "strategy": "move the caret with native keys to a new line at the end before each picture", "params": map[string]any{"insert_prep_keys": "ctrl+End,Enter"}, "test": "publish with trial_pictures=2 (no submit)", "expect": []any{"pictures_kept>=2", "regression==0"}}
	case strings.HasPrefix(last, "Hypothesis"):
		return m.publish(map[string]any{"trial_pictures": 2})
	case strings.Contains(last, "verdict=hypothesis_supported"):
		return m.publish(nil)
	}
	return nil
}

func TestRecoveryCaretEditorEndToEndThroughInvokeTool(t *testing.T) {
	ridCaret2 := recRunID("run-recov-caret-2")
	ridCaret1 := recRunID("run-recov-caret-1")
	recoveryTestEnv(t)
	// This test exercises recovery finding the caret fix itself; the
	// d99 default would otherwise prevent the regression it diagnoses.
	browser.MarkdownPictureCaretDefault = false
	t.Cleanup(func() { browser.MarkdownPictureCaretDefault = true })
	d := recChrome(t)
	srv, site := recCaretServer(t)
	operateDesktopFunc = recPublishOp(srv, d)
	files := recPictures(t)
	body := "## 一句话\n\nVFlash 把一次视觉规划拆成整块并行解码。\n\n[[img:1]]\n\n## 方法\n\n块长度在解码前确定。\n\n[[img:2]]\n\n## 结果\n\n解码时间从 72ms 到 78ms。\n\n[[img:3]]\n\n[[img:4]]\n\n## 冷水\n\n25 倍是初步测量。"
	m := &recModel{title: "VFlash：以图元为单位的并行解码", body: body, files: files, editor: srv.URL + "/md"}
	req := recoveryTurn("tenant-rec", "alice", ridCaret1, "把这篇文章连同 4 张图发到 CSDN")
	last := ""
	var transcript []string
	verified := false
	for i := 0; i < 8 && !verified; i++ {
		args := m.next(last)
		if args == nil {
			t.Fatalf("model has no next step after:\n%s", last)
		}
		out, err := invoke(t, req, args)
		last = out
		if err != nil {
			last = err.Error()
		}
		transcript = append(transcript, last)
		verified = strings.Contains(last, "goal verified")
	}
	if !verified {
		t.Fatalf("not verified:\n%s", strings.Join(transcript, "\n----\n"))
	}
	all := strings.Join(transcript, "\n")
	for _, want := range []string{"verdict=regression", "lost: item 1", "hypothesis 1 refuted", "verdict=hypothesis_supported", "recipe r"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(strings.ToLower(all), "does not allow") || strings.Contains(all, "不支持多图") {
		t.Fatal("the site was blamed")
	}
	if n := site.count(); n != 1 {
		t.Fatalf("submitted %d times (trials must never submit)", n)
	}
	site.mu.Lock()
	md, _ := site.published[0]["markdowncontent"].(string)
	site.mu.Unlock()
	if n := strings.Count(md, "](http"); n != 4 {
		t.Fatalf("published body holds %d pictures:\n%s", n, md)
	}
	eng := desktopRecovery()
	sk := recovery.ScopeKey("tenant-rec", "alice")
	rs := eng.Store.AllRecipes(sk)
	if len(rs) != 1 || rs[0].Params["insert_prep_keys"] != "ctrl+End,Enter" || len(rs[0].Params) != 1 {
		t.Fatalf("recipe %+v", rs)
	}
	raw, _ := json.Marshal(rs)
	if strings.Contains(string(raw), "VFlash") || strings.Contains(string(raw), "remembers") {
		t.Fatalf("recipe holds content or hypothesis text: %s", raw)
	}
	if out, _ := desktopRecoveryOutcome(ridCaret1); out != "done" {
		t.Fatalf("verified run has outcome %q", out)
	}

	// A later run of the same person on the same editor starts with the
	// verified recipe, which is re-verified by the real result.
	m2 := &recModel{title: "第二篇：同一编辑器", body: body, files: files, editor: srv.URL + "/md"}
	req2 := recoveryTurn("tenant-rec", "alice", ridCaret2, "再发一篇")
	out, err := invoke(t, req2, m2.publish(nil))
	if err != nil || !strings.Contains(out, "applied verified recipe") || !strings.Contains(out, "goal verified") {
		t.Fatalf("recipe not applied/verified: out=%s err=%v", out, err)
	}
	if rs = eng.Store.AllRecipes(sk); rs[0].Verified != 2 {
		t.Fatalf("recipe not re-verified: %+v", rs[0])
	}
	// The first article again in the same task is not posted twice.
	before := site.count()
	if _, err := invoke(t, req2, m.publish(nil)); err == nil || site.count() != before {
		t.Fatalf("a verified article was published again: %v", err)
	}
	// Another person on the same site gets nothing from alice.
	if got := eng.Recipes(recovery.ScopeKey("tenant-rec", "bob"), rs[0].Surface, "publish"); len(got) != 0 {
		t.Fatal("recipe crossed users")
	}
}

// A file transform that loses rows: the measured regression stops the
// mutating calls until a diagnosis; the last good result is kept.
func TestRecoveryFileTransformRegressionThroughInvokeTool(t *testing.T) {
	ridFile1 := recRunID("run-recov-file-1")
	recoveryTestEnv(t)
	root := t.TempDir()
	home := "/home/desktop"
	local := func(p string) string { return filepath.Join(root, strings.TrimPrefix(p, home)) }
	_ = os.MkdirAll(local(home+"/work"), 0o755)
	var csv strings.Builder
	csv.WriteString("id,name,city\n")
	for i := 1; i <= 120; i++ {
		if i%10 == 0 {
			fmt.Fprintf(&csv, "%d,\"Lee, Ann\",Paris\n", i)
		} else {
			fmt.Fprintf(&csv, "%d,user%d,Rome\n", i, i)
		}
	}
	_ = os.WriteFile(local(home+"/work/orders.csv"), []byte(csv.String()), 0o644)
	desktopRemoteFile = func(ctx context.Context, tenant, user, action, p, content, oldS, newS string) (string, error) {
		switch action {
		case "read":
			b, err := os.ReadFile(local(p))
			return string(b), err
		case "write":
			return "ok", os.WriteFile(local(p), []byte(content), 0o644)
		}
		return "", errors.New("unsupported")
	}
	bashRuns := 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if desktopArg(args, "action") != "bash" {
			return "", errors.New("unexpected")
		}
		bashRuns++
		// bash consumes POSIX spellings; a Windows root like C:\… loses its
		// backslashes as escape sequences inside the shell string.
		cmd := strings.ReplaceAll(desktopArg(args, "command"), home, filepath.ToSlash(root))
		b, err := exec.Command("bash", "-c", cmd).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%s", b)
		}
		return string(b), nil
	}
	req := recoveryTurn("tenant-rec", "carol", ridFile1, "把 orders.csv 的 120 行转成 orders.json")
	check := map[string]any{"action": "goal_check", "goal": "orders.json", "criteria": []any{map[string]any{"kind": "json_min_items", "path": home + "/work/orders.json", "value": 120}}}
	out, err := invoke(t, req, check)
	if err != nil || !strings.Contains(out, "baseline") {
		t.Fatalf("baseline: %s %v", out, err)
	}
	// Attempt 1: a naive awk split (quoted commas break rows) keeps 108.
	naive := `cd /home/desktop/work && awk -F, 'NR>1 && NF==3 {printf "%s{\"id\":%s,\"name\":\"%s\",\"city\":\"%s\"}", (n++?",":"["), $1,$2,$3} END {print "]"}' orders.csv > orders.json`
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": naive}); err != nil {
		t.Fatal(err)
	}
	out, _ = invoke(t, req, map[string]any{"action": "goal_check", "goal": "orders.json"})
	if !strings.Contains(out, "items:orders.json=108") || !strings.Contains(out, "last good copy kept") {
		t.Fatalf("attempt 1: %s", out)
	}
	// Relaxing the criteria is refused.
	if _, err := invoke(t, req, map[string]any{"action": "goal_check", "goal": "orders.json", "criteria": []any{map[string]any{"kind": "json_min_items", "path": home + "/work/orders.json", "value": 100}}}); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("criteria relaxed: %v", err)
	}
	// Attempt 2: "fix" by dropping the bad lines first — overwrites with fewer.
	worse := `cd /home/desktop/work && head -20 orders.csv | awk -F, 'NR>1 && NF==3 {printf "%s{\"id\":%s}", (n++?",":"["), $1} END {print "]"}' > orders.json`
	_, _ = invoke(t, req, map[string]any{"action": "bash", "command": worse})
	out, _ = invoke(t, req, map[string]any{"action": "goal_check", "goal": "orders.json"})
	if !strings.Contains(out, "verdict=regression") || !strings.Contains(out, "RECOVERY_REQUIRED") || !strings.Contains(out, "orders.json.maclaw-lastgood") {
		t.Fatalf("regression not reported: %s", out)
	}
	before := bashRuns
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "cp /home/desktop/work/orders.json.maclaw-lastgood /home/desktop/work/orders.json"}); err == nil || !strings.Contains(err.Error(), "action=recover") || bashRuns != before {
		t.Fatalf("mutation allowed without a diagnosis: %v", err)
	}
	// Naming a fresh goal does not escape the one waiting for a diagnosis.
	if _, err := invoke(t, req, map[string]any{"action": "goal_check", "goal": "orders-v2.json", "criteria": []any{map[string]any{"kind": "file_exists", "path": home + "/work/orders-v2.json"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "true"}); err == nil || !strings.Contains(err.Error(), "action=recover") || bashRuns != before {
		t.Fatalf("a renamed goal bypassed the diagnosis: %v", err)
	}
	if _, err := invoke(t, req, map[string]any{"action": "recover", "goal": "orders.json", "hypothesis": "awk splits on every comma, so quoted names with a comma make 4 fields and those rows are dropped", "uncertainty": "whether other rows have quotes", "strategy": "parse with a real CSV parser (python csv module) instead of splitting on commas", "test": "convert and goal_check", "expect": []any{"items:orders.json>=120"}}); err != nil {
		t.Fatal(err)
	}
	good := `cd /home/desktop/work && python3 -c 'import csv,json;print(json.dumps(list(csv.DictReader(open("orders.csv")))))' > orders.json`
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": good}); err != nil {
		t.Fatal(err)
	}
	out, _ = invoke(t, req, map[string]any{"action": "goal_check", "goal": "orders.json"})
	if !strings.Contains(out, "all criteria met") || !strings.Contains(out, "verdict=verified") {
		t.Fatalf("not verified: %s", out)
	}
	var rows []map[string]any
	raw, _ := os.ReadFile(local(home + "/work/orders.json"))
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 120 {
		t.Fatalf("result has %d rows", len(rows))
	}
}

// Budgets across strategies and a resumed run, an unconfirmed submit that is
// never repeated, and the honest receipt.
func TestRecoveryBudgetResumeUnconfirmedAndReceipt(t *testing.T) {
	ridE1 := recRunID("run-recov-e-1")
	ridC1 := recRunID("run-recov-c-1")
	ridB2 := recRunID("run-recov-b-2")
	ridB1 := recRunID("run-recov-b-1")
	recoveryTestEnv(t)
	t.Setenv("MACLAW_RECOVERY_ATTEMPTS", "3")
	calls := 0
	state := browser.PublishNotSubmitted
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		calls++
		spec := browser.PublishSpec{Title: desktopArg(args, "title")}
		if err := desktopPublishApplyStrategy(args, &spec); err != nil {
			return "", err
		}
		spec.Body = desktopArg(args, "body")
		rep := &browser.PublishReport{State: state, Facts: map[string]float64{"pictures_wanted": 3, "pictures_kept": float64(calls % 2)}}
		return desktopPublishExecute(scope, spec, "example", false, func(spec browser.PublishSpec) (*browser.PublishReport, error) {
			if spec.TrialPictures > 0 {
				rep.Trial, rep.Facts = true, map[string]float64{"pictures_wanted": 2, "pictures_kept": 2, "trial": 1}
				return rep, nil
			}
			if state == browser.PublishSubmitted && spec.BeforeSubmit != nil {
				if err := spec.BeforeSubmit(); err != nil {
					return rep, err
				}
			}
			return rep, fmt.Errorf("publish step paste_images failed: picture %d of 3 was not stored", calls)
		})
	}
	pub := func(extra map[string]any) map[string]any {
		a := map[string]any{"action": "publish", "title": "预算测试", "body": "正文内容足够长的一段文字。", "editor_url": "https://write.example.org/new"}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	req := recoveryTurn("tenant-b", "dave", ridB1, "发到 example")
	_, _ = invoke(t, req, pub(nil))
	_, _ = invoke(t, req, pub(map[string]any{"insert_settle_ms": 1000}))
	// Resumed in a new run (after a restart: a fresh engine on the same store).
	desktopRecoveryEng = nil
	req2 := recoveryTurn("tenant-b", "dave", ridB2, "继续")
	// ... and the model renamed the article: still the same goal.
	if _, err := invoke(t, req2, pub(map[string]any{"body_selector": ".editor", "title": "换了一个新标题的同一篇文章"})); err == nil || !strings.Contains(err.Error(), "action=recover") {
		t.Fatalf("a renamed title in a resumed run skipped the pending diagnosis: %v", err)
	}
	// No goal= needed: the run's one goal waiting for a diagnosis is used.
	if _, err := invoke(t, req2, map[string]any{"action": "recover", "hypothesis": "the editor drops a picture inserted while the previous one is still being placed", "strategy": "native caret keys before each picture", "params": map[string]any{"insert_prep_keys": "End"}, "test": "trial_pictures=2", "expect": []any{"pictures_kept>=2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, req2, pub(nil)); err == nil || !strings.Contains(err.Error(), "must not submit") {
		t.Fatalf("a hypothesis test that would submit was allowed: %v", err)
	}
	out, err := invoke(t, req2, pub(map[string]any{"trial_pictures": 2}))
	if err != nil || !strings.Contains(out, "RECOVERY_EXHAUSTED") || !strings.Contains(out, "applied hypothesis 1") {
		t.Fatalf("count budget not shared across strategies and runs: %s %v", out, err)
	}
	before := calls
	if _, err := invoke(t, req2, pub(map[string]any{"insert_prep_keys": "End"})); err == nil || !strings.Contains(err.Error(), "budget") || calls != before {
		t.Fatalf("attempt after exhaustion ran: %v calls=%d", err, calls)
	}
	// A genuinely new instruction in the same conversation is a new task
	// with its own budget.
	ridB3 := recRunID("run-recov-b-3")
	callsB3 := calls
	if _, err := invoke(t, recoveryTurn("tenant-b", "dave", ridB3, "另外写一篇关于 Rust 异步运行时的新文章，发到 example 网站"), pub(map[string]any{"title": "Rust 异步运行时"})); (err != nil && strings.Contains(err.Error(), "refused")) || calls != callsB3+1 {
		t.Fatalf("a new task was blocked by the old one: %v", err)
	}
	msg := &agentservice.Message{Content: "已经发好了", Metadata: map[string]string{"response_source": "ask_user"}}
	msg = desktopEnsureReceipt(agentruntime.Scope{TenantID: "tenant-b", UserID: "dave"}, &agentservice.Run{ID: ridB2, Status: agentservice.RunStatusSucceeded}, msg, nil, false)
	if o := msg.Metadata["task_outcome"]; o != "partial" && o != "blocked" {
		t.Fatalf("outcome %q", o)
	}
	if !strings.HasPrefix(msg.Content, "这件") || !strings.Contains(msg.Content, "依据") {
		t.Fatalf("receipt not honest: %q", msg.Content)
	}
	// Another tenant's identical task has its own budget.
	_, err = invoke(t, recoveryTurn("tenant-c", "dave", ridC1, "发到 example"), pub(nil))
	if err != nil && strings.Contains(err.Error(), "budget") {
		t.Fatalf("budget leaked across tenants: %v", err)
	}
	// An unconfirmed submit is never repeated.
	state = browser.PublishSubmitted
	req3 := recoveryTurn("tenant-b", "erin", ridE1, "发到 example")
	out, err = invoke(t, req3, pub(nil))
	if !strings.Contains(out, "STOPPED") || !strings.Contains(out, "submitted_unconfirmed") {
		t.Fatalf("unconfirmed not stopped: %s %v", out, err)
	}
	before = calls
	if _, err := invoke(t, req3, pub(map[string]any{"insert_settle_ms": 500})); err == nil || calls != before {
		t.Fatalf("submit repeated after unconfirmed: %v", err)
	}
	msg = desktopEnsureReceipt(agentruntime.Scope{TenantID: "tenant-b", UserID: "erin"}, &agentservice.Run{ID: ridE1, Status: agentservice.RunStatusSucceeded}, &agentservice.Message{Content: "好了"}, nil, false)
	if msg.Metadata["task_outcome"] != "unconfirmed" {
		t.Fatalf("outcome %q", msg.Metadata["task_outcome"])
	}
	// A question-only chat (no desktop step) has no task outcome ...
	msg = desktopEnsureReceipt(agentruntime.Scope{TenantID: "x", UserID: "y"}, &agentservice.Run{ID: "run-question", Status: agentservice.RunStatusSucceeded}, &agentservice.Message{Content: "你想发到哪个网站？", Metadata: map[string]string{"response_source": "ask_user"}}, nil, false)
	if msg.Metadata["task_outcome"] != "" {
		t.Fatalf("question-only outcome %q", msg.Metadata["task_outcome"])
	}
	// ... a run that worked on the desktop and ends asking is awaiting_user.
	desktopRecStateMu.Lock()
	desktopRecRunSteps["run-none"] = true
	desktopRecStateMu.Unlock()
	msg = desktopEnsureReceipt(agentruntime.Scope{TenantID: "x", UserID: "y"}, &agentservice.Run{ID: "run-none", Status: agentservice.RunStatusSucceeded}, &agentservice.Message{Content: "请你点一下发布", Metadata: map[string]string{"response_source": "ask_user"}}, nil, false)
	if msg.Metadata["task_outcome"] != "awaiting_user" {
		t.Fatalf("ask_user outcome %q", msg.Metadata["task_outcome"])
	}
}

func TestRecoveryToolSchemaAndParamWhitelist(t *testing.T) {
	if !desktopActionAllowed("recover") || !desktopActionAllowed("goal_check") {
		t.Fatal("actions not in the tool vocabulary")
	}
	for _, bad := range []map[string]string{{"command": "rm -rf /"}, {"insert_prep_keys": "ctrl+Enter"}, {"insert_prep_keys": "Delete"}, {"body_selector": "x</style><script>"}, {"insert_settle_ms": "999999"}} {
		if _, err := desktopRecPublishParams(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	p, err := desktopRecPublishParams(map[string]string{"insert_prep_keys": " ctrl+End , Enter "})
	if err != nil || p["insert_prep_keys"] != "ctrl+End,Enter" {
		t.Fatalf("%v %v", p, err)
	}
	if !strings.Contains(desktopRecoveryPrompt(), "action=recover") {
		t.Fatal("prompt")
	}
}

// One attempt at a time per task goal, across runs of the same task, a
// second engine on the same ledger (another process) and a tool call that
// timed out while its pipeline still runs; the late result is recorded, the
// lease ends with it, and a verified article is not posted again.
func TestRecoveryOneAttemptAtATimeAcrossRunsAndTimeouts(t *testing.T) {
	recoveryTestEnv(t)
	t.Setenv("MACLAW_DESKTOP_STEP_TIMEOUT_PUBLISH", "0.5")
	release := make(chan struct{})
	var mu sync.Mutex
	started, posted := 0, 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		spec := browser.PublishSpec{Title: desktopArg(args, "title"), Body: desktopArg(args, "body"), Deadline: time.Now().Add(time.Minute)}
		return desktopPublishExecute(scope, spec, "example", false, func(spec browser.PublishSpec) (*browser.PublishReport, error) {
			mu.Lock()
			started++
			mu.Unlock()
			<-release // the pipeline outlives the tool call
			if err := spec.BeforeSubmit(); err != nil {
				return &browser.PublishReport{State: browser.PublishNotSubmitted}, err
			}
			mu.Lock()
			posted++
			mu.Unlock()
			return &browser.PublishReport{State: browser.PublishLive, ResultURL: "https://write.example.org/p/1", Evidence: "response id", Facts: map[string]float64{"published": 1}}, nil
		})
	}
	pub := map[string]any{"action": "publish", "title": "并发测试", "body": "一段足够长的正文内容，用来测试同一个任务不会被并发发布两次。", "editor_url": "https://write.example.org/new"}
	ridA, ridB := recRunID("run-recov-lease-a"), recRunID("run-recov-lease-b")
	reqA := recoveryTurn("tenant-l", "frank", ridA, "把这篇并发测试文章发到 example 网站上")
	out, err := invoke(t, reqA, pub)
	text := out
	if err != nil {
		text = err.Error()
	}
	if !strings.Contains(text, "timed out") || !strings.Contains(text, "still running") {
		t.Fatalf("timeout not reported as a running attempt: %s", text)
	}
	// A resumed run of the same task, a renamed title, and another engine
	// on the same ledger: none starts a second attempt.
	reqB := recoveryTurn("tenant-l", "frank", ridB, "继续")
	renamed := map[string]any{}
	for k, v := range pub {
		renamed[k] = v
	}
	renamed["title"] = "并发测试（改名）"
	if _, err := invoke(t, reqB, renamed); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("second attempt while the first runs: %v", err)
	}
	other := recovery.New(recovery.DefaultConfig(), recovery.NewStore(desktopRecoveryRoot))
	sk := recovery.ScopeKey("tenant-l", "frank")
	gs := other.Goals(sk, "publish|")
	if len(gs) != 1 || gs[0].Lease == nil {
		t.Fatalf("goals %+v", gs)
	}
	if _, _, err := other.Acquire(sk, recovery.AttemptRequest{Goal: gs[0].Key, Method: "publish", Consequential: true}); err == nil {
		t.Fatal("another process acquired the goal")
	}
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if g := other.Status(sk, gs[0].Key); g != nil && g.Lease == nil {
			if g.State != recovery.StateDone {
				t.Fatalf("late result not recorded: %s %s", g.State, g.Reason)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lease never released")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if o, _ := desktopRecoveryOutcome(ridA); o != "done" {
		t.Fatalf("outcome %q", o)
	}
	if _, err := invoke(t, reqB, pub); err == nil {
		t.Fatal("verified article published again")
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 1 || posted != 1 {
		t.Fatalf("started=%d posted=%d", started, posted)
	}
}

// The service hook gets the measured outcome with its notice.
func TestRecoveryTaskOutcomeMetadataForTheService(t *testing.T) {
	recoveryTestEnv(t)
	rid := recRunID("run-recov-meta")
	desktopRecStateMu.Lock()
	desktopRecRunSteps[rid] = true
	desktopRecStateMu.Unlock()
	if m := desktopTaskOutcomeMetadata(rid, agentservice.Message{Content: "你想发到哪里？", Metadata: map[string]string{"response_source": "ask_user"}}); m[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeAwaitingUser {
		t.Fatalf("%v", m)
	}
	if m := desktopTaskOutcomeMetadata(recRunID("run-recov-q"), agentservice.Message{Content: "你想发到哪里？", Metadata: map[string]string{"response_source": "ask_user"}}); m != nil {
		t.Fatalf("question-only chat got %v", m)
	}
	eng := desktopRecovery()
	sk := recovery.ScopeKey("t", "u")
	l, _, err := eng.Acquire(sk, recovery.AttemptRequest{Goal: "publish|x#1|example", Method: "publish", Consequential: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = eng.SideEffect(sk, "publish|x#1|example", l.ID)
	eng.Finish(sk, l.ID, recovery.Observation{Goal: "publish|x#1|example", Method: "publish", Side: "unconfirmed", Error: "submitted, no result"})
	desktopRecNoteRunGoal(rid, sk+"\x01publish|x#1|example")
	m := desktopTaskOutcomeMetadata(rid, agentservice.Message{Content: "发好了！"})
	if m[agentservice.TaskOutcomeKey] != "unconfirmed" || !strings.Contains(m[agentservice.TaskOutcomeNoticeKey], "没能确认") {
		t.Fatalf("%v", m)
	}
}
