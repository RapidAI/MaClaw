package main

import (
	"strings"
	"testing"
)

func TestDropInternalStatusLead(t *testing.T) {
	in := "之前那个 `desktop: error` 是早前某一步的瞬时失败留下的旧状态。\n\n**笔记链接**\nhttps://x.com/explore/1"
	out := dropInternalStatusLead(in)
	if strings.Contains(out, "desktop") || !strings.Contains(out, "https://x.com/explore/1") {
		t.Fatalf("%q", out)
	}
	keep := "已发布：https://x.com/p/1\n\n宿主确认完成"
	if dropInternalStatusLead(keep) != keep {
		t.Fatal("changed a reply whose first paragraph has the link")
	}
}
