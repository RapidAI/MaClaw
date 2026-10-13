package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopBrowserSkillRoundTrip(t *testing.T) {
	dir := t.TempDir()
	previous := desktopBrowserSkillRoot
	desktopBrowserSkillRoot = dir
	t.Cleanup(func() { desktopBrowserSkillRoot = previous })
	userKey := "0123456789abcdef"
	script := `await click("保存"); await type("#q", "weather")`
	if _, err := saveDesktopBrowserSkill(userKey, "Save Draft", script, "https://docs.example.com/a?token=1", "script ok: clicked 保存"); err == nil {
		t.Fatal("an uppercase name was stored")
	}
	if _, err := saveDesktopBrowserSkill(userKey, "save-draft", script, "https://docs.example.com/a?token=1", "cookie=secret"); err != nil {
		t.Fatal(err)
	}
	listed, err := listDesktopBrowserSkills(userKey)
	if err != nil || !strings.Contains(listed, "save-draft") || !strings.Contains(listed, "host=docs.example.com") || strings.Contains(listed, "cookie") || strings.Contains(listed, "token") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	loaded, err := loadDesktopBrowserSkill(userKey, "save-draft")
	if err != nil || loaded.Script != script || loaded.Host != "docs.example.com" || loaded.Summary != "" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if err := desktopSkillHostAllowed(loaded.Host, "https://www.docs.example.com/other"); err != nil {
		t.Fatal(err)
	}
	if host := desktopSkillHost("https://www.docs.example.com/a"); host != "docs.example.com" {
		t.Fatalf("host=%s", host)
	}
	if err := desktopSkillHostAllowed(loaded.Host, "https://mail.example.com/"); err == nil {
		t.Fatal("a skill ran on another site")
	}
	if err := desktopSkillHostAllowed("", "https://docs.example.com/"); err == nil {
		t.Fatal("a skill with no site was allowed to run")
	}
	if _, err := saveDesktopBrowserSkill(userKey, "save-draft", `await click("发布")`, "https://docs.example.com/b", "script ok: clicked 发布"); err != nil {
		t.Fatal(err)
	}
	updated, err := loadDesktopBrowserSkill(userKey, "save-draft")
	if err != nil || updated.Script != `await click("发布")` {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err := saveDesktopBrowserSkill(userKey, "anywhere", `await click("保存")`, "about:blank", ""); err == nil {
		t.Fatal("a skill without a website was stored")
	}
	if _, err := listDesktopBrowserSkills("not a key"); err == nil {
		t.Fatal("an invalid user listed skills")
	}
	if err := browser.ValidatePageScript(loaded.Script); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopControlsMarkDuplicateNames(t *testing.T) {
	text := formatDesktopControls(browser.BrowserSnapshot{Refs: []browser.BrowserElementRef{
		{Ref: "e1", Role: "button", Name: "保存", Selector: "button.save"},
		{Ref: "e2", Role: "button", Name: "保存", Selector: "button.draft"},
		{Ref: "e3", Role: "link", Name: "下一步"},
	}})
	if !strings.Contains(text, "e1 button 保存 button.save") || !strings.Contains(text, "e2 button 保存 button.draft") {
		t.Fatalf("duplicates were not disambiguated: %s", text)
	}
	if !strings.Contains(text, "e3 link 下一步\n") || strings.Contains(text, "e3 link 下一步 ") {
		t.Fatalf("unique control gained a selector: %s", text)
	}
}
