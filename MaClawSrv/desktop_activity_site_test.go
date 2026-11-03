package main

import "testing"

func TestDesktopActivityNamesSiteAndLogin(t *testing.T) {
	run := "run_site_test"
	defer clearDesktopActivity(run)
	desktopNoteActivityPage(run, "https://passport.csdn.net/login?code=x")
	setDesktopActivity(run, desktopWebStepPhrase(run, "正在输入"))
	if got := loadDesktopActivity(run); got != "正在登录 CSDN" {
		t.Fatalf("got %q", got)
	}
	desktopNoteActivityPage(run, "https://editor.csdn.net/md")
	setDesktopActivity(run, desktopWebStepPhrase(run, "正在输入"))
	if got := loadDesktopActivity(run); got != "正在填写 CSDN" {
		t.Fatalf("got %q", got)
	}
	setDesktopActivity(run, "正在执行命令")
	if got := loadDesktopActivity(run); got != "正在执行命令" {
		t.Fatalf("container step took a site: %q", got)
	}
	desktopNoteActivityPage(run, "https://unknown.example/x")
	setDesktopActivity(run, "正在点击")
	if got := loadDesktopActivity(run); got != "正在点击" {
		t.Fatalf("unknown host leaked: %q", got)
	}
}
