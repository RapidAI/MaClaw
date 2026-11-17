package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop/inputcorrect"
)

// The page script runs in a real Chromium on a virtual display. Trust comes
// from document.hasFocus in that window, not from a test double.

func TestDesktopInputPageScriptInBrowser(t *testing.T) {
	chrome := os.Getenv("MACLAW_CHROME")
	if chrome == "" {
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				chrome = path
				break
			}
		}
	}
	if chrome == "" {
		t.Fatal("a local Chrome or Chromium binary is required")
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Fatal("Xvfb is required so the page can hold focus")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Fatal("xdotool is required to focus the page window")
	}

	page := filepath.Join(t.TempDir(), "fields.html")
	gif := "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
	html := `<!doctype html><meta charset="utf-8"><title>fields</title>
<body>
<input id="other" name="other" value="keep-other">
<input id="body" name="body" value="plain">
<textarea id="note" name="note"></textarea>
<div id="row"><input id="busy-self" name="busy-self" aria-busy="true" value="wait"><span id="sib" aria-busy="true"></span></div>
<div id="editor" contenteditable="true"><img alt="" src="` + gif + `">old</div>
<iframe id="inner" name="inner" srcdoc="<input id='inside' name='inside' value='iframe-text'>"></iframe>
</body>`
	if err := os.WriteFile(page, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}

	focused := startChrome(t, chrome, false)
	focused.activate(t)
	ws := focused.pageSocket(t)
	client, err := browser.ConnectCDP(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	eval := func(expr string) string {
		t.Helper()
		raw, err := client.Send("Runtime.evaluate", map[string]any{
			"expression": expr, "returnByValue": true, "awaitPromise": true,
		}, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
			ExceptionDetails any `json:"exceptionDetails"`
		}
		if json.Unmarshal(raw, &parsed) != nil || parsed.ExceptionDetails != nil {
			t.Fatalf("eval: %s", raw)
		}
		switch v := parsed.Result.Value.(type) {
		case string:
			return v
		default:
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	nav, err := client.Send("Page.navigate", map[string]any{"url": "file://" + page}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = nav
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if eval("document.readyState") == "complete" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := client.Send("Page.bringToFront", map[string]any{}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	focused.activate(t)
	eval("document.getElementById('note').value = '甲\\n乙'; document.getElementById('note').focus()")
	if eval("document.hasFocus()") != "true" {
		t.Fatal("the page is not the foreground document; refusing to stub trust")
	}
	obs := readField(t, eval(desktopInputObserveExpr))
	if !obs.Trusted || !obs.Identified || obs.ElementID == 0 {
		t.Fatalf("untrusted or placeholder identity: %+v", obs)
	}
	if ok, _ := inputcorrect.CompareExact("甲\n乙", obs.Text); !ok {
		t.Fatalf("multiline text %q", obs.Text)
	}
	noteID := obs.ElementID
	eval("document.getElementById('body').focus()")
	wrong := actField(t, eval, obs, "CHANGED")
	if wrong.OK || wrong.Error != "wrong_field" {
		t.Fatalf("wrong field act: %+v", wrong)
	}
	if eval("document.getElementById('other').value") != "keep-other" || eval("document.getElementById('note').value") != "甲\n乙" {
		t.Fatal("wrong-field act changed a value")
	}

	eval("document.getElementById('body').focus()")
	obs = readField(t, eval(desktopInputObserveExpr))
	bodyID := obs.ElementID
	eval("document.body.appendChild(document.createElement('input'))")
	stale := actField(t, eval, obs, "nope")
	if stale.OK || stale.Error != "stale_target" {
		t.Fatalf("stale act: %+v", stale)
	}

	eval("document.getElementById('note').focus(); document.getElementById('note').value = '甲\\n乙'")
	note := readField(t, eval(desktopInputObserveExpr))
	if ok, _ := inputcorrect.CompareExact("甲\n乙", note.Text); !note.Trusted || !ok || note.ElementID == bodyID || note.ElementID != noteID {
		t.Fatalf("textarea %+v", note)
	}

	eval("var f=document.getElementById('inner'); f.contentWindow.focus(); f.contentDocument.getElementById('inside').focus()")
	inner := readField(t, eval(desktopInputObserveExpr))
	if !strings.HasPrefix(inner.FrameID, "frame:") || inner.Text != "iframe-text" || !inner.Trusted {
		t.Fatalf("iframe %+v hasFocus=%s", inner, eval("document.hasFocus()"))
	}

	eval("document.getElementById('busy-self').focus()")
	busy := readField(t, eval(desktopInputObserveExpr))
	if !busy.UploadPending || !busy.EditorBusy {
		t.Fatalf("self/sibling busy was missed: %+v", busy)
	}

	eval("document.getElementById('editor').focus()")
	editor := readField(t, eval(desktopInputObserveExpr))
	if editor.Images != 1 || !editor.EditorReplace {
		t.Fatalf("editor %+v", editor)
	}
	replaced := actField(t, eval, editor, "kept")
	if !replaced.OK || replaced.Images != 1 {
		t.Fatalf("replace %+v", replaced)
	}
	if eval("document.getElementById('editor').querySelectorAll('img').length") != "1" {
		t.Fatal("image was dropped")
	}
	if ok, _ := inputcorrect.CompareExact("kept", readField(t, eval(desktopInputObserveExpr)).Text); !ok {
		t.Fatal("editor text was not exact")
	}

	headless := startChrome(t, chrome, true)
	hws := headless.pageSocket(t)
	hclient, err := browser.ConnectCDP(hws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hclient.Close() })
	heval := func(expr string) string {
		t.Helper()
		raw, err := hclient.Send("Runtime.evaluate", map[string]any{
			"expression": expr, "returnByValue": true,
		}, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &parsed) != nil {
			t.Fatalf("headless eval: %s", raw)
		}
		if s, ok := parsed.Result.Value.(string); ok {
			return s
		}
		b, _ := json.Marshal(parsed.Result.Value)
		return string(b)
	}
	_, _ = hclient.Send("Page.navigate", map[string]any{"url": "file://" + page}, 10*time.Second)
	time.Sleep(200 * time.Millisecond)
	if heval("document.hasFocus()") == "true" {
		t.Fatal("headless page reported focus; this case is the unreadable one")
	}
	unread := readField(t, heval(desktopInputObserveExpr))
	if unread.Trusted || unread.Identified {
		t.Fatalf("unfocused page was trusted: %+v", unread)
	}
	prev := desktopPageEval
	desktopInputProcedureRoot = t.TempDir()
	desktopPageEval = func(*browser.BrowserAgentSession, string) (string, error) {
		return heval(desktopInputObserveExpr), nil
	}
	t.Cleanup(func() { desktopPageEval = prev })
	_, err = desktopVerifyBrowserType(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "bot"}, &browser.BrowserAgentSession{}, "甲\n乙", "ok", nil)
	if err == nil {
		t.Fatal("an unfocused read was success")
	}
}

type fieldSnap struct {
	Text          string `json:"text"`
	Trusted       bool   `json:"trusted"`
	Identified    bool   `json:"identified"`
	ElementID     int    `json:"element_id"`
	ElementKey    string `json:"element_key"`
	FrameID       string `json:"frame_id"`
	RefEpoch      int    `json:"ref_epoch"`
	LayoutScale   int    `json:"layout_scale"`
	Images        int    `json:"images"`
	UploadPending bool   `json:"upload_pending"`
	EditorBusy    bool   `json:"editor_busy"`
	EditorReplace bool   `json:"editor_replace"`
}

func readField(t *testing.T, raw string) fieldSnap {
	t.Helper()
	var snap fieldSnap
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		t.Fatalf("field json %q: %v", raw, err)
	}
	return snap
}

type actResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	Images int    `json:"images"`
}

func actField(t *testing.T, eval func(string) string, obs fieldSnap, text string) actResult {
	t.Helper()
	raw := eval(desktopInputActExpr("semantic_set", text, obs.ElementKey, obs.FrameID, obs.ElementID, obs.RefEpoch, obs.LayoutScale))
	var res actResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("act json %q: %v", raw, err)
	}
	return res
}

type chromeProc struct {
	cmd     *exec.Cmd
	url     string
	display string
}

func startChrome(t *testing.T, bin string, headless bool) *chromeProc {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	args := []string{
		"--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu", "--ozone-platform=x11",
		"--no-first-run", "--no-default-browser-check",
		"--user-data-dir=" + chromeProfile(t),
		"--remote-debugging-port=" + fmt.Sprint(port),
		"--window-size=1280,800", "about:blank",
	}
	var cmd *exec.Cmd
	display := ""
	if headless {
		args = append([]string{"--headless=new"}, args...)
		cmd = exec.Command(bin, args...)
	} else {
		display = freeXDisplay(t)
		xvfb := exec.Command("Xvfb", display, "-screen", "0", "1280x800x24", "-nolisten", "tcp")
		if err := xvfb.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = xvfb.Process.Kill(); _, _ = xvfb.Process.Wait() })
		time.Sleep(200 * time.Millisecond)
		if wm, err := exec.LookPath("xfwm4"); err == nil {
			wmCmd := exec.Command(wm, "--replace")
			wmCmd.Env = append(os.Environ(), "DISPLAY="+display)
			_ = wmCmd.Start()
			t.Cleanup(func() {
				if wmCmd.Process != nil {
					_ = wmCmd.Process.Kill()
					_, _ = wmCmd.Process.Wait()
				}
			})
			time.Sleep(300 * time.Millisecond)
		}
		cmd = exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "DISPLAY="+display)
	}
	logFile, _ := os.Create(filepath.Join(t.TempDir(), "chrome.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	var body []byte
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
		if err == nil {
			body, _ = readAll(resp)
			resp.Body.Close()
			if resp.StatusCode == 200 && len(body) > 0 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.Unmarshal(body, &version) != nil || version.WebSocketDebuggerURL == "" {
		t.Fatalf("chrome did not open a debugging port: %s", body)
	}
	return &chromeProc{cmd: cmd, url: fmt.Sprintf("http://127.0.0.1:%d", port), display: display}
}

func (c *chromeProc) activate(t *testing.T) {
	t.Helper()
	if c.display == "" {
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	var last []byte
	for time.Now().Before(deadline) {
		find := exec.Command("xdotool", "search", "--onlyvisible", "--name", "Chrome")
		find.Env = append(os.Environ(), "DISPLAY="+c.display)
		ids, err := find.Output()
		last = ids
		if err == nil {
			for _, id := range strings.Fields(string(ids)) {
				nameCmd := exec.Command("xdotool", "getwindowname", id)
				nameCmd.Env = find.Env
				name, _ := nameCmd.Output()
				if !strings.Contains(string(name), "Google Chrome") {
					continue
				}
				act := exec.Command("xdotool", "windowactivate", "--sync", id)
				act.Env = find.Env
				_ = act.Run()
				focus := exec.Command("xdotool", "windowfocus", "--sync", id)
				focus.Env = find.Env
				if err := focus.Run(); err == nil {
					return
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = last
}

func chromeProfile(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "maclaw-chrome-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func freeXDisplay(t *testing.T) string {
	t.Helper()
	for n := 80; n < 200; n++ {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); os.IsNotExist(err) {
			return fmt.Sprintf(":%d", n)
		}
	}
	t.Fatal("no free X display")
	return ""
}

func (c *chromeProc) pageSocket(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(c.url + "/json/list")
		if err == nil {
			body, _ := readAll(resp)
			resp.Body.Close()
			var pages []struct {
				Type                 string `json:"type"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if json.Unmarshal(body, &pages) == nil {
				for _, page := range pages {
					if page.Type == "page" && page.WebSocketDebuggerURL != "" {
						return page.WebSocketDebuggerURL
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("chrome listed no page")
	return ""
}

func readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}
