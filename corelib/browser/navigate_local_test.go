package browser

import "testing"

func TestLocalPagesAreNeverALoginWall(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8793/pv.html", "http://localhost/x", "file:///tmp/a.html"} {
		if !localPageURL(u) {
			t.Fatal(u)
		}
	}
	if localPageURL("https://www.xiaohongshu.com/") {
		t.Fatal("remote")
	}
}
