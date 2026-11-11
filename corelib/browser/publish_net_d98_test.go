package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func d98Ev(method string, params any) CDPEvent {
	raw, _ := json.Marshal(params)
	return CDPEvent{Method: method, Params: raw}
}

func d98Write(t *netTracker, id, url string, headers map[string]any, status int) {
	t.on(d98Ev("Network.requestWillBeSent", map[string]any{"requestId": id, "type": "XHR", "request": map[string]any{"method": "POST", "url": url, "headers": headers}}))
	t.on(d98Ev("Network.responseReceived", map[string]any{"requestId": id, "response": map[string]any{"status": status, "mimeType": "text/html"}}))
	t.on(d98Ev("Network.loadingFinished", map[string]any{"requestId": id}))
}

// Zhihu, Nov 11 10:34: a signed-in editor's background sign_in POST answered
// 400 and the picture check called that "the site's upload failed".
func TestD98UnrelatedWriteFailureIsNotAnUploadFailure(t *testing.T) {
	tr := &netTracker{open: map[string]string{}, last: time.Now(), json: map[string]bool{}}
	t0 := time.Now().Add(-time.Second)
	d98Write(tr, "1", "https://www.zhihu.com/api/v3/oauth/sign_in", nil, 400)
	if got := tr.uploadFailedSince(t0); len(got) != 0 {
		t.Fatalf("sign_in counted as upload failure: %v", got)
	}
	if got := tr.failedSince(t0); len(got) != 1 {
		t.Fatalf("all failures = %v", got)
	}
	d98Write(tr, "2", "https://api.zhihu.com/images", map[string]any{"Content-Type": "multipart/form-data; boundary=x"}, 500)
	got := tr.uploadFailedSince(t0)
	if len(got) != 1 || got[0] != "POST https://api.zhihu.com/images → HTTP 500" {
		t.Fatalf("upload failures = %v", got)
	}
	if txt := tr.uploadFailureText(t0); txt != got[0] {
		t.Fatalf("text = %q", txt)
	}
}

// CSDN, Nov 11 10:41: the editor's picture input inserts at the caret and
// leaves the new link selected; picture 2 replaced picture 1's link. A
// markdown body's pictures now go in after ctrl+End,Enter by default.
func TestD99CSDNCaretInsertKeepsEveryPicture(t *testing.T) {
	addr := startFixtureChrome(t)
	run := func() (*PublishReport, error) {
		srv, _ := csdnServerPage(t, "csdn_md_caret.html")
		d := fixtureTab(t, addr, "about:blank")
		files := csdnFiles(t)
		return RunPublish(d, csdnSpec(srv, files))
	}
	prevKeys := mdDefaultPrepKeys
	mdDefaultPrepKeys = nil
	_, err := run()
	mdDefaultPrepKeys = prevKeys
	if err == nil || !strings.Contains(err.Error(), "removed picture 1") {
		t.Skipf("fixture does not reproduce the lost link without the default (err=%v)", err)
	}
	rep, err := run()
	if err != nil {
		t.Fatalf("with the default caret move: %v\n%s", err, rep)
	}
	if rep.State != PublishLive {
		t.Fatalf("state=%s\n%s", rep.State, rep)
	}
}
