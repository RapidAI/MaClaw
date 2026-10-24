package main

import "testing"

func TestDesktopNavigateIntentPerHost(t *testing.T) {
	a := desktopIntentClass("navigate", map[string]any{"url": "https://zhuanlan.zhihu.com/write"})
	b := desktopIntentClass("navigate", map[string]any{"url": "https://creator.xiaohongshu.com/publish/publish"})
	if a == b || a != "navigate:zhuanlan.zhihu.com" {
		t.Fatalf("a=%s b=%s", a, b)
	}
}
