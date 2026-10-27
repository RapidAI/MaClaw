package browser

import (
	"testing"
	"time"
)

func TestNetWriteKind(t *testing.T) {
	for u, want := range map[string]string{
		"https://x.test/api/v1/upload?x=1":                  netKindUpload,
		"https://x.test/bfs/article/image":                  netKindUpload,
		"https://x.test/api/profile/save":                   netKindWrite,
		"https://x.test/api/articles/1/draft":               netKindDraft,
		"https://x.test/api/auto-save":                      netKindDraft,
		"https://x.test/api/article/publish":                netKindWrite,
		"https://x.test/x/article/creative/draft/addupdate": netKindDraft,
	} {
		if got := netWriteKind(u, nil); got != want {
			t.Errorf("%s: %s want %s", u, got, want)
		}
	}
	if netWriteKind("https://x.test/api/save", map[string]any{"Content-Type": "multipart/form-data; boundary=x"}) != netKindUpload {
		t.Error("multipart is an upload")
	}
}

func TestJSONPostRef(t *testing.T) {
	id, u := jsonPostRef(`{"code":0,"data":{"article_id":"123456","url":"https://x.test/p/123456"}}`)
	if id != "123456" || u != "https://x.test/p/123456" {
		t.Fatal(id, u)
	}
	if id, u := jsonPostRef(`{"ok":true}`); id != "" || u != "" {
		t.Fatal(id, u)
	}
}

// failedState: what the page sent after the click decides the state.
func TestFailedStateFromWrites(t *testing.T) {
	at := time.Now()
	mk := func(ws ...netWrite) *publishRun {
		p := &publishRun{rep: &PublishReport{}, net: &netTracker{open: map[string]string{}, json: map[string]bool{}}}
		p.submitClicked.Store(true)
		p.clickedAt = at
		p.net.log = ws
		return p
	}
	before := netWrite{at: at.Add(-time.Second), kind: netKindWrite, path: "/api/save", done: true, ok: true}
	after := func(kind, path string) netWrite {
		return netWrite{at: at.Add(time.Second), kind: kind, path: path, done: true, ok: true}
	}
	if s := mk(after(netKindWrite, "/api/publish")).failedState(); s != PublishSubmitted {
		t.Fatal("submission write:", s)
	}
	if s := mk(after(netKindDraft, "/api/draft")).failedState(); s != PublishDraftOnly {
		t.Fatal("draft write:", s)
	}
	if s := mk(before, after(netKindWrite, "/api/save")).failedState(); s != PublishDraftOnly {
		t.Fatal("autosave endpoint written again:", s)
	}
	p := mk(after(netKindDraft, "/api/draft"))
	p.panelConfirmed = true
	if s := p.failedState(); s != PublishSubmitted {
		t.Fatal("panel confirmed:", s)
	}
	if s := mk(after(netKindUpload, "/upload")).failedState(); s != PublishNotSubmitted {
		t.Fatal("upload only:", s)
	}
}

func TestContentTagsAndOptions(t *testing.T) {
	if g := contentTags("x", "正文 #AI #论文"); len(g) != 2 || g[0] != "AI" {
		t.Fatal(g)
	}
	if g := contentTags("HF 每日论文｜多模态检索", ""); len(g) == 0 {
		t.Fatal("title pieces")
	}
	if pickOption([]string{"请选择", "原创", "转载"}, "") != 1 || pickOption([]string{"原创", "转载"}, "转载") != 1 {
		t.Fatal("pickOption")
	}
}

func TestMarkdownDrift(t *testing.T) {
	if markdownDrift("> q\na\nb\nc", "> q\n> a\n> b\n> c") == "" {
		t.Fatal("quote carry-over not seen")
	}
	if markdownDrift("a ![x](u) b", "a b") == "" {
		t.Fatal("lost picture link not seen")
	}
	if markdownDrift("> q\na", "> q\na") != "" {
		t.Fatal("false drift")
	}
}
