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

// Paper content that happens to use these words must survive.
func TestDropInternalStatusKeepsContentWords(t *testing.T) {
	in := "已发布：https://x.com/p/5\n\n- LlamaGuard 3 在隐私保护任务上提升 4.2 分\n- 论文 published 于 NeurIPS，代码可 deliver 到边缘设备\n- 方法在 desktop GUI 智能体上测试"
	if out := dropInternalStatusLead(in); out != in {
		t.Fatalf("over-trimmed: %q", out)
	}
}

func TestDropInternalStatusTrimsScratchPathList(t *testing.T) {
	in := "笔记链接：https://www.xiaohongshu.com/explore/6add28bf000000002403999f\n\n图片文件：/tmp/xhs/01.png、/tmp/xhs/02.png、/tmp/xhs/03.png"
	got := dropInternalStatusLead(in)
	if strings.Contains(got, "/tmp/") || !strings.Contains(got, "explore/6add28bf") {
		t.Fatalf("got %q", got)
	}
	keep := "链接 https://zhuanlan.zhihu.com/p/1\n\n代码放在 /tmp/x 下面，可以自己跑一下。"
	if dropInternalStatusLead(keep) != keep {
		t.Fatalf("over-trim: %q", dropInternalStatusLead(keep))
	}
}
