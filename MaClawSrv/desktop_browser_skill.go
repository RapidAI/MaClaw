package main

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopBrowserSkillRoot overrides the shared skill directory in tests.
// Empty uses the corelib data directory, the same store the GUI reads.
var desktopBrowserSkillRoot = ""

type desktopBrowserSkill struct {
	Name    string
	Host    string
	Script  string
	Summary string
	SavedAt time.Time
}

func desktopSkillName(name string) (string, error) {
	return browser.NormalizePageScriptSkillName(name)
}

func saveDesktopBrowserSkill(userKey, name, script, pageURL, summary string) (string, error) {
	if !desktop.ValidUserKey(userKey) {
		return "", fmt.Errorf("desktop requires a user")
	}
	return browser.SavePageScriptSkill(desktopBrowserSkillRoot, userKey, name, script, pageURL, summary)
}

func loadDesktopBrowserSkill(userKey, name string) (desktopBrowserSkill, error) {
	if !desktop.ValidUserKey(userKey) {
		return desktopBrowserSkill{}, fmt.Errorf("desktop requires a user")
	}
	skill, err := browser.LoadPageScriptSkill(desktopBrowserSkillRoot, userKey, name)
	if err != nil {
		return desktopBrowserSkill{}, err
	}
	return desktopBrowserSkill{
		Name:    skill.Name,
		Host:    skill.Host,
		Script:  skill.Script,
		Summary: skill.Summary,
		SavedAt: skill.SavedAt,
	}, nil
}

func listDesktopBrowserSkills(userKey string) (string, error) {
	if !desktop.ValidUserKey(userKey) {
		return "", fmt.Errorf("desktop requires a user")
	}
	return browser.ListPageScriptSkills(desktopBrowserSkillRoot, userKey)
}

func desktopSkillHost(raw string) string {
	return browser.PageScriptSkillHost(raw)
}

func normalizeDesktopSiteHost(host string) string {
	return browser.NormalizePageScriptHost(host)
}

func desktopSkillHostAllowed(skillHost, pageURL string) error {
	return browser.PageScriptSkillHostAllowed(skillHost, pageURL)
}
