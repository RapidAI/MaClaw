package main

import "testing"

func TestAvoidListCheck(t *testing.T) {
	req := "随机挑一篇论文发布到我的小红书账号。不要选这些已经写过的：2610.30043 2610.24383。"
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "论文 arXiv:2610.30043v1"}, req); err == nil {
		t.Fatal("listed paper must be refused")
	}
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "论文 arXiv:2610.11111"}, req); err != nil {
		t.Fatalf("other paper: %v", err)
	}
	if err := desktopAvoidListCheck("publish", map[string]any{"body": "2610.30043"}, "写 2610.30043 的解读发到知乎"); err != nil {
		t.Fatalf("named paper (no avoid words): %v", err)
	}
}

func TestHandPublishCheck(t *testing.T) {
	req := "写一篇笔记并直接发布到我的小红书账号"
	ed := "https://creator.xiaohongshu.com/publish/publish?from=menu"
	if err := desktopHandPublishCheck("app_run", map[string]any{"steps": "click 900 700"}, req, ed, false); err == nil {
		t.Fatal("desktop click on editor before publish action must be refused")
	}
	if err := desktopHandPublishCheck("script", map[string]any{"script": `[...document.querySelectorAll('button')].find(b=>b.innerText==="发布").click()`}, req, ed, false); err == nil {
		t.Fatal("scripted publish click must be refused")
	}
	if err := desktopHandPublishCheck("script", map[string]any{"script": "document.title"}, req, ed, false); err != nil {
		t.Fatalf("plain script: %v", err)
	}
	if err := desktopHandPublishCheck("app_run", nil, req, ed, true); err != nil {
		t.Fatalf("after publish action: %v", err)
	}
	if err := desktopHandPublishCheck("app_run", nil, req, "https://huggingface.co/papers", false); err != nil {
		t.Fatalf("not an editor: %v", err)
	}
}
