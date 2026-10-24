package main

import "testing"

func TestReplaceLeakedMonologue(t *testing.T) {
	leak := "So probe found 0 interactive elements. Hmm, the tab is blank. Let me try clicking. Idea: maybe a dialog. Actually let's try a new tab."
	if got := replaceLeakedMonologue(leak); got == leak {
		t.Fatal("monologue kept")
	}
	ok := "已发布：https://www.xiaohongshu.com/explore/1 Let me know"
	if replaceLeakedMonologue(ok) != ok {
		t.Fatal("real reply changed")
	}
}
