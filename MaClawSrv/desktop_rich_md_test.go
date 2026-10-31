package main

import "testing"

func TestRichContextAndNoteHosts(t *testing.T) {
	k, h := desktopRichContext("ok RICH_CTX kind=ce host=creator.xiaohongshu.com")
	if k != "ce" || h != "creator.xiaohongshu.com" || !desktopPlainNoteHostRe.MatchString(h) {
		t.Fatalf("%q %q", k, h)
	}
	if desktopPlainNoteHostRe.MatchString("zhuanlan.zhihu.com") {
		t.Fatal("zhihu is a rich editor")
	}
}
