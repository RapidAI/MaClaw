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
	keep := "已发布：https://x.com/p/1\n\n标题已确认"
	if dropInternalStatusLead(keep) != keep {
		t.Fatal("changed a reply whose first paragraph has the link")
	}
}

func TestDropInternalStatusLines(t *testing.T) {
	in := "核验完成。\n\n1. **deliver 报错 `already_published`** — 这不是真的失败。\n- 链接：https://x.com/explore/1"
	out := dropInternalStatusLead(in)
	if strings.Contains(out, "already_published") || !strings.Contains(out, "核验完成") || !strings.Contains(out, "https://x.com/explore/1") {
		t.Fatalf("%q", out)
	}
}

func TestDropInternalStatusProtectionTalk(t *testing.T) {
	in := "**结论一：** 上一轮的 desktop 报错不是发布失败，而是保护被再次触发。\n\n**结论二：** 链接 https://x.com/explore/2\n\n文件路径：/tmp/xhs/1_cover.jpg"
	out := dropInternalStatusLead(in)
	if strings.Contains(out, "报错") || strings.Contains(out, "/tmp/") || !strings.Contains(out, "https://x.com/explore/2") {
		t.Fatalf("%q", out)
	}
}

func TestDropInternalStatusDesktopFail(t *testing.T) {
	in := "核验结论：之前标记的 desktop 失败只有一处。\n\n**结论一：** 已发布。\n\n文章链接：https://x.com/p/3"
	if out := dropInternalStatusLead(in); strings.Contains(out, "desktop") || !strings.Contains(out, "结论一") {
		t.Fatalf("%q", out)
	}
}

func TestDropInternalStatusKeepsDesktopPath(t *testing.T) {
	in := "已发布：https://x.com/p/4\n\n文件在 /home/desktop/Desktop/a.docx"
	if dropInternalStatusLead(in) != in {
		t.Fatal("dropped a Desktop path line")
	}
}
