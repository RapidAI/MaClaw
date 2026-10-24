package main

import (
	"strings"
	"testing"
)

func TestReplyGetsEditorPostLinkWhenItSaysPublished(t *testing.T) {
	desktopNoteEditLink("r-e1", "https://zhuanlan.zhihu.com/p/42/edit?x=1")
	got := desktopReplyWithLink("r-e1", "文章已发布并核对完成")
	if !strings.Contains(got, "https://zhuanlan.zhihu.com/p/42") || strings.Contains(got, "/edit") {
		t.Fatalf("got %q", got)
	}
	desktopNoteEditLink("r-e2", "https://zhuanlan.zhihu.com/p/43/edit")
	if got := desktopReplyWithLink("r-e2", "还没发出去，卡在上传"); strings.Contains(got, "/p/43") {
		t.Fatalf("unpublished reply got link: %q", got)
	}
}
