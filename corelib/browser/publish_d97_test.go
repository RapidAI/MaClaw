package browser

import (
	"strings"
	"testing"
)

// papers 11-11 run_50546ca1: a publish with only a title (「测试2」, no body,
// no pictures) submitted the Zhihu editor. Refused before anything is filled.
func TestPublishD97RefusesEmptyAndPlaceholder(t *testing.T) {
	for _, title := range []string{"测试", "测试2", "test", "Test 3", "abc", "untitled", "标题"} {
		if PlaceholderTitleProblem(title) == "" {
			t.Errorf("%q must be a placeholder", title)
		}
	}
	for _, title := range []string{"LingBot-Touch：触觉大模型速读", "t", "隐藏 标题 测试", "测试驱动开发实践", "Test-time scaling 综述"} {
		if why := PlaceholderTitleProblem(title); why != "" {
			t.Errorf("%q flagged: %s", title, why)
		}
	}
	rep, err := RunPublish(nil, PublishSpec{Title: "LingBot-Touch 速读"})
	if err == nil || !strings.Contains(err.Error(), "no body and no pictures") || rep.State != PublishNotSubmitted {
		t.Fatalf("empty post: %v %+v", err, rep)
	}
	rep, err = RunPublish(nil, PublishSpec{Title: "测试2", Body: "正文"})
	if err == nil || !strings.Contains(err.Error(), "placeholder") || rep.State != PublishNotSubmitted {
		t.Fatalf("placeholder title: %v", err)
	}
}

// A raw picture URL is a file, not a sign-in page (d96 S29).
func TestNavigateD97RawResourceIsNotAWall(t *testing.T) {
	for _, st := range []PageReadyState{
		{URL: "https://raw.githubusercontent.com/x/y/main/fig1.png"},
		{URL: "https://example.com/a", ContentType: "image/png"},
		{URL: "https://arxiv.org/pdf/2611.12806", ContentType: "application/pdf"},
	} {
		if !st.rawResource() {
			t.Errorf("%+v not a raw resource", st)
		}
	}
	if (PageReadyState{URL: "https://www.zhihu.com/signin", ContentType: "text/html"}).rawResource() {
		t.Fatal("a site page is not a raw resource")
	}
}
