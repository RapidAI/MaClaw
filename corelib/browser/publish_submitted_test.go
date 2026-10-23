package browser

import "testing"

func TestPublishSubmittedURL(t *testing.T) {
	for _, u := range []string{"https://creator.xiaohongshu.com/publish/publish?source=&published=true", "https://x.test/post?status=success"} {
		if !publishSubmittedURL.MatchString(u) {
			t.Fatalf("missed %s", u)
		}
	}
	for _, u := range []string{"https://zhuanlan.zhihu.com/p/1/edit", "https://x.test/publish?draft=1"} {
		if publishSubmittedURL.MatchString(u) {
			t.Fatalf("matched %s", u)
		}
	}
}
