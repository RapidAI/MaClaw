package browser

import (
	"encoding/json"
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
)

// Offline fixtures for the generic publish pipeline, driven by a real
// headless Chrome. Skipped when no Chrome binary is installed.

type fixtureDriver struct {
	c       *CDPClient
	mu      sync.Mutex
	files   []string
	handled chan error
}

func (d *fixtureDriver) Send(m string, p interface{}) (json.RawMessage, error) {
	return d.c.Send(m, p, 15*time.Second)
}

func (d *fixtureDriver) ArmChooser(files []string) error {
	d.mu.Lock()
	d.files, d.handled = files, make(chan error, 1)
	d.mu.Unlock()
	_, err := d.c.Send("Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": true}, 5*time.Second)
	return err
}

func (d *fixtureDriver) WaitChooser(t time.Duration) (bool, error) {
	d.mu.Lock()
	ch := d.handled
	d.mu.Unlock()
	select {
	case err := <-ch:
		return true, err
	case <-time.After(t):
		return false, nil
	}
}

func (d *fixtureDriver) DisarmChooser() {
	_, _ = d.c.Send("Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": false}, 5*time.Second)
}

func (d *fixtureDriver) pump() {
	for ev := range d.c.Events() {
		if ev.Method != "Page.fileChooserOpened" {
			continue
		}
		node, _ := parseFileChooserOpened(ev.Params)
		d.mu.Lock()
		files, ch := d.files, d.handled
		d.mu.Unlock()
		go func() {
			_, err := d.c.Send("DOM.setFileInputFiles", map[string]interface{}{"backendNodeId": node, "files": files}, 5*time.Second)
			select {
			case ch <- err:
			default:
			}
		}()
	}
}

func chromeBin() string {
	for _, b := range []string{os.Getenv("CHROME_BIN"), "google-chrome", "chromium", "chromium-browser"} {
		if b == "" {
			continue
		}
		if p, err := exec.LookPath(b); err == nil {
			return p
		}
	}
	return ""
}

func startFixtureChrome(t *testing.T) string {
	bin := chromeBin()
	if bin == "" {
		t.Skip("no chrome binary")
	}
	dir, _ := os.MkdirTemp("", "pubfix")
	cmd := exec.Command(bin, "--headless=new", "--no-sandbox", "--disable-gpu", "--remote-debugging-port=0", "--user-data-dir="+dir, "--window-size=1280,900", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Skipf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait(); _ = os.RemoveAll(dir) })
	for i := 0; i < 100; i++ {
		b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err == nil {
			if lines := strings.Split(string(b), "\n"); len(lines) > 0 && lines[0] != "" {
				return "127.0.0.1:" + strings.TrimSpace(lines[0])
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("chrome did not start")
	return ""
}

func fixtureTab(t *testing.T, addr, url string) *fixtureDriver {
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/json/new?"+url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tgt struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tgt)
	c, err := ConnectCDP(tgt.WS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	d := &fixtureDriver{c: c}
	go d.pump()
	_, _ = c.Send("Page.enable", nil, 5*time.Second)
	_, _ = c.Send("DOM.enable", nil, 5*time.Second)
	for i := 0; i < 50; i++ {
		raw, _ := c.Send("Runtime.evaluate", map[string]interface{}{"expression": "document.readyState", "returnByValue": true}, 5*time.Second)
		if strings.Contains(string(raw), "complete") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return d
}

type fixtureSite struct {
	mu        sync.Mutex
	published []map[string]string
	views     int
}

func fixtureServer(t *testing.T, page string) (*httptest.Server, *fixtureSite) {
	site := &fixtureSite{}
	mux := http.NewServeMux()
	html, err := os.ReadFile(filepath.Join("testdata", "publish", page))
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/edit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="120"><rect width="200" height="120" fill="#48c"/></svg>`)
	})
	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]string
		_ = json.Unmarshal(b, &m)
		site.mu.Lock()
		site.published = append(site.published, m)
		site.mu.Unlock()
		fmt.Fprint(w, "/p/123")
	})
	page1 := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><meta charset=utf-8><body>"+body+"</body>")
	}
	last := func() map[string]string {
		site.mu.Lock()
		defer site.mu.Unlock()
		if len(site.published) == 0 {
			return map[string]string{}
		}
		return site.published[len(site.published)-1]
	}
	mux.HandleFunc("/success", func(w http.ResponseWriter, r *http.Request) { page1(w, "发布成功") })
	mux.HandleFunc("/p/123", func(w http.ResponseWriter, r *http.Request) {
		m := last()
		b := m["body"]
		if m["html"] != "" {
			b = m["html"]
		}
		busy := ""
		qp := r.URL.Query()
		site.mu.Lock()
		site.views++
		views := site.views
		site.mu.Unlock()
		if qp.Get("droppic") != "" || (qp.Get("latepic") != "" && views == 1) {
			if i := strings.LastIndex(b, "<img"); i >= 0 {
				b = b[:i] + "<span>" + b[i+4:]
			}
		}
		if r.URL.Query().Get("busy") != "" {
			busy = "<script>setTimeout(()=>{const t=Date.now();while(Date.now()-t<9000){}},200)</script>"
		}
		page1(w, "<header><img src='data:,' width=32 alt=avatar></header><h1>"+m["title"]+"</h1><article>"+b+"</article>"+busy)
	})
	mux.HandleFunc("/notes", func(w http.ResponseWriter, r *http.Request) {
		m := last()
		page1(w, `<li><a href="/explore/abc">`+m["title"]+`</a></li>`)
	})
	mux.HandleFunc("/cards", func(w http.ResponseWriter, r *http.Request) {
		m := last()
		page1(w, `<div class="note" data-impression='{"noteId":"6adbcee50000000031026525"}'><div class="cover"></div><span class="title">`+m["title"]+`</span><span>审核中</span></div>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, site
}

func fixtureImage(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "cover.png")
	// 1x1 PNG
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 0x49, 0x48, 0x44, 0x52, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0, 1, 0, 0, 5, 0, 1, 0x0d, 0x0a, 0x2d, 0xb4, 0, 0, 0, 0, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const fixtureBody = "HF 每日论文 10.18 精选\n\n1. 第一篇：一种更快的推理方法。\n2. 第二篇：多模态检索新基准。\n\n#AI #论文"

func TestPublishFlowXHSLikeClosedShadowSubmit(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "xhs_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "10.18 HF论文精选｜速读", Body: fixtureBody, VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 || !strings.Contains(site.published[0]["body"], "多模态检索新基准") {
		t.Fatalf("published %+v", site.published)
	}
	if !strings.HasSuffix(rep.ResultURL, "/explore/abc") {
		t.Fatalf("result url %q", rep.ResultURL)
	}
}

func TestPublishFlowZhihuLikeDraftBody(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "zhihu_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "Hugging Face Daily Papers 10.18 摘要", Body: fixtureBody, StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 || !strings.Contains(site.published[0]["body"], "第一篇") {
		t.Fatalf("published %+v", site.published)
	}
	if !strings.Contains(rep.ResultURL, "/p/123") {
		t.Fatalf("result url %q", rep.ResultURL)
	}
}

func TestPublishFlowChooserOnlyUpload(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "chooser_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, UploadTrigger: "添加图片", Title: "chooser title", Body: "line one\nline two", SubmitTexts: []string{"Post"}, VerifyURL: srv.URL + "/notes", StepBudget: 15 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 || site.published[0]["title"] != "chooser title" {
		t.Fatalf("published %+v", site.published)
	}
}

func TestPublishFlowReportsTheStuckStep(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "zhihu_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Title: "t", Body: "b", SubmitTexts: []string{"不存在的按钮"}, StepBudget: 5 * time.Second})
	if err == nil || rep.Stuck != "submit" || !strings.Contains(rep.String(), "submit: FAILED") {
		t.Fatalf("err=%v rep=%s", err, rep.String())
	}
	if len(site.published) != 0 {
		t.Fatal("published anyway")
	}
}

// A list of cards without links still yields the post id.
func TestPublishFlowCardListReportsPostID(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, _ := fixtureServer(t, "xhs_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "扩散语言模型量化每步不同", Body: fixtureBody, VerifyURL: srv.URL + "/cards", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if rep.PostID != "6adbcee50000000031026525" {
		t.Fatalf("post id %q url %q", rep.PostID, rep.ResultURL)
	}
}

// The image input only appears after choosing the image tab.
func TestPublishFlowPicksImageTab(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "tabs_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "10.18 HF论文精选｜速读", Body: fixtureBody, VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 {
		t.Fatalf("published %+v", site.published)
	}
}

// A trigger naming the image tab (not a chooser button) still uploads.
func TestPublishFlowTabAsTrigger(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "tabs_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, UploadTrigger: "上传图文", Title: "10.18 HF论文精选｜速读", Body: fixtureBody, VerifyURL: srv.URL + "/notes", StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 {
		t.Fatalf("published %+v", site.published)
	}
}

// No file input: images are pasted into the rich body editor.
func TestPublishFlowPastesImagesIntoEditor(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := fixtureServer(t, "paste_like.html")
	d := fixtureTab(t, addr, srv.URL+"/edit")
	rep, err := RunPublish(d, PublishSpec{Files: []string{fixtureImage(t)}, Title: "Hugging Face Daily Papers 10.18 摘要", Body: fixtureBody, StepBudget: 20 * time.Second})
	t.Log(rep.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.published) != 1 || !strings.Contains(rep.String(), "pasted 1 image") {
		t.Fatalf("published %+v", site.published)
	}
}
