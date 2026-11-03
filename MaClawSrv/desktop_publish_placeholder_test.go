package main

import "testing"

func TestDesktopPlaceholderText(t *testing.T) {
	for _, s := range []string{"…", "...", "正文", " <body> ", "【正文】", "TODO", "……"} {
		if !desktopPlaceholderText(s) {
			t.Fatalf("not caught %q", s)
		}
	}
	for _, s := range []string{"", "MaClaw 让桌面机器人替你发文章", "## 简介\n\n这是正文"} {
		if desktopPlaceholderText(s) {
			t.Fatalf("false hit %q", s)
		}
	}
}
