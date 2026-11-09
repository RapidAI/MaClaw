package browser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A CSDN-like markdown editor (testdata/publish/csdn_md.html): pictures
// upload one at a time (a paste during an upload is lost and the finished
// link doubled, as in run_b0a85008), SVG is refused, the 发布文章 panel needs
// a tag, and the post ends on /mp_blog/creation/success/<id> saying 审核中.

type csdnFixture struct {
	mu        sync.Mutex
	published []map[string]any
	uploads   int
	refuse    bool
}

func csdnServer(t *testing.T) (*httptest.Server, *csdnFixture) {
	f := &csdnFixture{}
	html, err := os.ReadFile(filepath.Join("testdata", "publish", "csdn_md.html"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/md", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "UserName", Value: "znsoft", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		time.Sleep(1500 * time.Millisecond)
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
		w.Header().Set("Content-Type", "application/json")
		if st, _ := m["status"].(float64); st == 0 {
			f.mu.Lock()
			refuse := f.refuse
			if !refuse {
				f.published = append(f.published, m)
			}
			f.mu.Unlock()
			if refuse {
				fmt.Fprint(w, `{"code":400,"msg":"内容审核未通过"}`)
				return
			}
		}
		fmt.Fprint(w, `{"code":200,"data":{"id":"170300001"}}`)
	})
	mux.HandleFunc("/mp_blog/creation/success/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><body><h2>发布成功！正在审核中</h2><p>审核通过后文章将对外展示</p><a href="/mp_blog/manage/article">管理文章</a></body>`)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, f
}

// 1x1 PNGs (distinct bytes) and one SVG.
func csdnFiles(t *testing.T) []string {
	dir := t.TempDir()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	var out []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, fmt.Sprintf("p%d.png", i+1))
		_ = os.WriteFile(p, append(append([]byte(nil), png...), byte(i)), 0o644)
		out = append(out, p)
	}
	svg := filepath.Join(dir, "fig.svg")
	_ = os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="160"><rect width="300" height="160" fill="#48c"/></svg>`), 0o644)
	return append(out, svg)
}

func csdnSpec(srv *httptest.Server, files []string) PublishSpec {
	p := ProfileForURL("https://editor.csdn.net/md/")
	s := PublishSpec{
		Files:      files,
		Title:      "VFlash：以图元为单位的并行解码",
		Body:       "## 一句话\n\nVFlash 把一次视觉规划拆成整块并行解码，速度快 25 倍。\n\n[[img:1]]\n\n## 方法\n\n块长度在解码前确定，由基元类型决定，**不再逐坐标解码**。\n\n[[img:2]]\n\n## 结果\n\nThor 上解码时间从 72ms 到 78ms，轨迹 token 从 36 增到 84。\n\n[[img:3]]\n\n[[img:4]]\n\n## 冷水\n\n25 倍是初步的单次 rollout 测量，去掉 AR 兜底后 F1 会掉约 20 个点。",
		EditorURL:  srv.URL + "/md",
		SuccessURL: "/mp_blog/creation/success/(\\d+)",
		StepBudget: 30 * time.Second,
		Deadline:   time.Now().Add(6 * time.Minute),
		MinImages:  len(files),
	}
	p.Apply(&s)
	s.EditorURL = srv.URL + "/md"
	s.SuccessURL = "/mp_blog/creation/success/(\\d+)"
	return s
}

func TestPublishCSDNMarkdownPicturesOneByOneAndSuccessPage(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := csdnServer(t)
	d := fixtureTab(t, addr, "about:blank")
	files := csdnFiles(t)
	rep, err := RunPublish(d, csdnSpec(srv, files))
	if err != nil {
		t.Fatalf("publish: %v\n%s", err, rep)
	}
	if rep.State != PublishLive || rep.ResultURL != "https://blog.csdn.net/znsoft/article/details/170300001" || rep.PostID != "170300001" {
		t.Fatalf("state=%s url=%q id=%q\n%s", rep.State, rep.ResultURL, rep.PostID, rep)
	}
	if !strings.Contains(rep.Pending, "审核") {
		t.Fatalf("pending not reported: %q", rep.Pending)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.published) != 1 {
		t.Fatalf("published %d times", len(site.published))
	}
	body, _ := site.published[0]["markdowncontent"].(string)
	if n := len(markdownImageURLs(body)); n != len(files) {
		t.Fatalf("published body has %d distinct picture links, want %d:\n%s", n, len(files), body)
	}
	if tags, _ := site.published[0]["tags"].(string); tags == "" {
		t.Fatal("publish panel tag not filled")
	}
	if !strings.Contains(body, "去掉 AR 兜底") || !strings.Contains(body, "VFlash 把一次视觉规划") {
		t.Fatalf("body incomplete: %q", body)
	}
}

func TestPublishCSDNRefusedIsNotPublishedAndNoStaleLink(t *testing.T) {
	addr := startFixtureChrome(t)
	srv, site := csdnServer(t)
	site.refuse = true
	// The tab starts on an earlier post's success page.
	d := fixtureTab(t, addr, srv.URL+"/mp_blog/creation/success/169744427")
	s := csdnSpec(srv, nil)
	rep, err := RunPublish(d, s)
	if err == nil || rep.State == PublishLive || rep.State == PublishLiveIncomplete {
		t.Fatalf("refused post reported as published: state=%s err=%v\n%s", rep.State, err, rep)
	}
	if strings.Contains(rep.ResultURL, "169744427") || strings.Contains(rep.ResultURL, "170300001") {
		t.Fatalf("stale/unproven link %q", rep.ResultURL)
	}
}
