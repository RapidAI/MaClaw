package browser

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPageScriptRejectsSecretsAndAllowsClicks(t *testing.T) {
	if !isCaptchaMutatingAction("script") {
		t.Fatal("a page script must stop on a captcha")
	}
	if err := ValidatePageScript("   "); err == nil {
		t.Fatal("empty script was accepted")
	}
	for _, script := range []string{
		`document.cookie`,
		`localStorage.getItem("token")`,
		`fetch("https://example.com")`,
		`type('input[type="password"]', 'secret')`,
		`location.href = "https://example.com"`,
		`location = "https://example.com"`,
		`navigator.credentials.get()`,
		`HTMLInputElement.prototype.value`,
		`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")`,
		`window.open("https://example.com")`,
		`return document["cookie"]`,
		`return [].constructor.constructor("return this")()`,
		`return globalThis.document.cookie`,
	} {
		if err := ValidatePageScript(script); err == nil {
			t.Fatalf("accepted %s", script)
		}
	}
	for _, script := range []string{
		`await click("Forgot password")`,
		`await click("API credentials")`,
		`if (location.href.includes("/done") || location.href === "https://docs.example.com/draft") await click("item.pdf")`,
		`await click("保存"); await type("#q", "weather"); await select("#state", "完成"); await press("Enter"); await scroll(400); await waitFor("下一步", 1000)`,
	} {
		if err := ValidatePageScript(script); err != nil {
			t.Fatalf("%s: %v", script, err)
		}
	}
	if _, err := (&BrowserAgentSession{}).RunPageScript(`await click("保存")`); err == nil {
		t.Fatal("a script ran without a browser")
	}
	flagged, err := parsePageScriptReport(`{"ok":true,"flags":{"login_wall":true}}`)
	if err != nil || !flagged.Flags.browserFlags().LoginWall {
		t.Fatalf("flags=%#v err=%v", flagged.Flags, err)
	}
	report, err := parsePageScriptReport(`{"ok":false,"error":"not found: 下一步","clicked":["保存"],"typed":["#q"]}`)
	if err != nil {
		t.Fatal(err)
	}
	progress := pageScriptProgress(report)
	if !strings.Contains(progress, "clicked 保存") || !strings.Contains(progress, "typed #q") {
		t.Fatalf("progress=%q", progress)
	}
	blocked, blockErr := pageScriptFailureResult(pageScriptReport{
		Error:   "not found: 下一步",
		Clicked: []string{"保存"},
		Flags:   pageScriptFlagSet{LoginWall: true},
	})
	if blockErr != nil || blocked == nil || !strings.Contains(blocked.Display, "page flags: login_wall") || !strings.Contains(blocked.Display, "clicked 保存") {
		t.Fatalf("failure handoff=%#v err=%v", blocked, blockErr)
	}
	failed, failErr := pageScriptFailureResult(report)
	if failErr == nil || failed != nil || !strings.Contains(failErr.Error(), "not found: 下一步") || !strings.Contains(failErr.Error(), "clicked 保存") {
		t.Fatalf("failure=%v err=%v", failed, failErr)
	}
	fresh := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap": {SnapshotID: "snap", CreatedAt: time.Now().UnixMilli(), PageFlags: BrowserPageFlags{LoginWall: true}},
		},
		lastSnapshotID: "snap",
	}
	if blocked := pageScriptPersonResult(mustRecentFlags(t, fresh)); blocked == nil || !strings.Contains(blocked.Display, "login_wall") {
		t.Fatalf("fresh snapshot was not reused: %#v", blocked)
	}
	stale := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"old": {SnapshotID: "old", CreatedAt: time.Now().Add(-time.Hour).UnixMilli(), PageFlags: BrowserPageFlags{LoginWall: true}},
		},
		lastSnapshotID: "old",
	}
	if _, ok := stale.recentSnapshotFlags(20 * time.Second); ok {
		t.Fatal("a stale snapshot was treated as the current page")
	}
	fresh.markProbeFlagsStale()
	if _, ok := fresh.recentSnapshotFlags(20 * time.Second); ok {
		t.Fatal("flags from before a script were reused")
	}
	fresh.addSnapshot(BrowserSnapshot{SnapshotID: "next", CreatedAt: time.Now().UnixMilli()})
	if _, ok := fresh.recentSnapshotFlags(20 * time.Second); !ok {
		t.Fatal("a new probe did not restore flag reuse")
	}
}

func mustRecentFlags(t *testing.T, s *BrowserAgentSession) BrowserPageFlags {
	t.Helper()
	flags, ok := s.recentSnapshotFlags(20 * time.Second)
	if !ok {
		t.Fatal("fresh snapshot flags were not read")
	}
	return flags
}

func TestPageScriptClicksTypesAndRefusesAPassword(t *testing.T) {
	jsdom, err := filepath.Abs(filepath.Join("..", "..", "guiapp", "frontend", "node_modules", "jsdom"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsdom); err != nil {
		t.Skipf("jsdom is not installed: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node is not installed: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "page.js")
	if err := os.WriteFile(scriptPath, []byte(wrapPageScript(`await click("保存"); await click("里面"); await click("item.pdf"); await type("#q", "weather"); await select("#state", "完成"); return "saved"`, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	passwordPath := filepath.Join(dir, "password.js")
	if err := os.WriteFile(passwordPath, []byte(wrapPageScript(`await type('input[name="secret"]', "nope")`, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	sameHostPath := filepath.Join(dir, "same-host.js")
	if err := os.WriteFile(sameHostPath, []byte(wrapPageScript(`await click("保存"); return "saved"`, "www.docs.example.com")), 0o644); err != nil {
		t.Fatal(err)
	}
	otherHostPath := filepath.Join(dir, "other-host.js")
	if err := os.WriteFile(otherHostPath, []byte(wrapPageScript(`await click("保存"); return "saved"`, "mail.example.com")), 0o644); err != nil {
		t.Fatal(err)
	}
	readPath := filepath.Join(dir, "read.js")
	if err := os.WriteFile(readPath, []byte(wrapPageScript(`return document.querySelector('input[name="secret"]').value`, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	ambiguousPath := filepath.Join(dir, "ambiguous.js")
	if err := os.WriteFile(ambiguousPath, []byte(wrapPageScript(`await click("存")`, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	loginPath := filepath.Join(dir, "login.js")
	if err := os.WriteFile(loginPath, []byte(wrapPageScript(`return "ok"`, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "run.js")
	if err := os.WriteFile(program, []byte(`
const fs = require('fs');
const {JSDOM} = require(process.argv[2]);
const expr = fs.readFileSync(process.argv[3], 'utf8');
const dom = new JSDOM('<!doctype html><title>Draft</title><body><div role="button"><button>保存</button></div><button>保存草稿</button><button style="display:none">隐藏</button><item class="pdf">no</item><button>item.pdf</button><div role="dialog" class="cookie" id="cookie">Accept cookies</div><button class="cookie">接受 Cookie</button><p>Privacy Policy</p><input id="q"><input name="secret" type="password" value="hunter2"><select id="state"><option>完成</option></select></body>', {url: 'https://docs.example.com/draft?token=1', pretendToBeVisual: true, runScripts: 'dangerously'});
const window = dom.window;
window.Element.prototype.getBoundingClientRect = function() { return {x:1,y:1,width:20,height:10,top:1,left:1,right:21,bottom:11}; };
const iframe = window.document.createElement('iframe');
window.document.body.appendChild(iframe);
iframe.contentDocument.body.innerHTML = '<button>里面</button>';
if (iframe.contentWindow && iframe.contentWindow.Element) iframe.contentWindow.Element.prototype.getBoundingClientRect = window.Element.prototype.getBoundingClientRect;
if (process.argv[4] === 'login-frame') {
  const login = window.document.createElement('iframe');
  window.document.body.appendChild(login);
  login.contentDocument.body.innerHTML = '<p>请登录</p><input type="password">';
  if (login.contentWindow && login.contentWindow.Element) login.contentWindow.Element.prototype.getBoundingClientRect = window.Element.prototype.getBoundingClientRect;
}
const script = window.document.createElement('script');
script.textContent = 'window.__done = ' + expr;
window.document.body.appendChild(script);
Promise.resolve(window.__done).then(result => {
  process.stdout.write(String(result));
}).catch(err => {
  console.error(err);
  process.exit(1);
});
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, program, jsdom, scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	var report pageScriptReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("report %s: %v", out, err)
	}
	if !report.OK || report.Value != "saved" || strings.Join(report.Clicked, ",") != "保存,里面,item.pdf" || strings.Join(report.Typed, ",") != "#q" || strings.Join(report.Selected, ",") != "完成" || report.Flags.LoginWall || report.Flags.ConsentDialog || !report.Cookie {
		t.Fatalf("report=%s", out)
	}
	if pageScriptURL(report.URL) != "https://docs.example.com/draft" || pageScriptURL(report.StartURL) != "https://docs.example.com/draft" {
		t.Fatalf("url=%s start=%s", report.URL, report.StartURL)
	}
	if strings.Contains(string(out), "weather") {
		t.Fatal("typed text was returned")
	}
	denied, err := exec.Command(node, program, jsdom, passwordPath).CombinedOutput()
	if err != nil {
		t.Fatalf("password script: %v\n%s", err, denied)
	}
	if err := json.Unmarshal(denied, &report); err != nil {
		t.Fatalf("password report %s: %v", denied, err)
	}
	if report.OK || !strings.Contains(report.Error, "password") {
		t.Fatalf("password report=%s", denied)
	}
	same, err := exec.Command(node, program, jsdom, sameHostPath).CombinedOutput()
	if err != nil {
		t.Fatalf("same host: %v\n%s", err, same)
	}
	if err := json.Unmarshal(same, &report); err != nil || !report.OK || strings.Join(report.Clicked, ",") != "保存" {
		t.Fatalf("www host report=%s err=%v", same, err)
	}
	other, err := exec.Command(node, program, jsdom, otherHostPath).CombinedOutput()
	if err != nil {
		t.Fatalf("other host: %v\n%s", err, other)
	}
	if err := json.Unmarshal(other, &report); err != nil || report.OK || len(report.Clicked) != 0 || !strings.Contains(report.Error, "mail.example.com") {
		t.Fatalf("other host report=%s err=%v", other, err)
	}
	read, err := exec.Command(node, program, jsdom, readPath).CombinedOutput()
	if err != nil {
		t.Fatalf("read secret: %v\n%s", err, read)
	}
	if err := json.Unmarshal(read, &report); err != nil || !report.OK || report.Value != "" || strings.Contains(string(read), "hunter2") {
		t.Fatalf("secret report=%s err=%v", read, err)
	}
	ambiguous, err := exec.Command(node, program, jsdom, ambiguousPath).CombinedOutput()
	if err != nil {
		t.Fatalf("ambiguous: %v\n%s", err, ambiguous)
	}
	if err := json.Unmarshal(ambiguous, &report); err != nil || report.OK || len(report.Clicked) != 0 || !strings.Contains(report.Error, "(2:") || !strings.Contains(report.Error, "保存草稿") {
		t.Fatalf("ambiguous report=%s err=%v", ambiguous, err)
	}
	login, err := exec.Command(node, program, jsdom, loginPath, "login-frame").CombinedOutput()
	if err != nil {
		t.Fatalf("login frame: %v\n%s", err, login)
	}
	if err := json.Unmarshal(login, &report); err != nil || !report.OK || !report.Flags.LoginWall {
		t.Fatalf("login frame report=%s err=%v", login, err)
	}
}
