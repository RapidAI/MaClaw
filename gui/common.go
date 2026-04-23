package main

import "github.com/RapidAI/CodeClaw/corelib/brand"

func trayTranslations() map[string]map[string]string {
	b := brand.Current()

	// zh-Hans title: prefer DisplayNameCN if non-empty, otherwise DisplayName
	zhHansTitle := b.DisplayNameCN + " 控制台"
	if b.DisplayNameCN == "" {
		zhHansTitle = b.DisplayName + " 控制台"
	}

	return map[string]map[string]string{
		"en": {
			"title": b.TrayTooltip,
			"show":  "Show Main Window",
			"hide":  "Hide Main Window",
			"quit":  "Quit " + b.DisplayName,
		},
		"zh-Hans": {
			"title": zhHansTitle,
			"show":  "显示主窗口",
			"hide":  "隐藏主窗口",
			"quit":  "退出程序",
		},
		"zh-Hant": {
			"title": b.DisplayName + " 控制台",
			"show":  "顯示主視窗",
			"hide":  "隱藏主視窗",
			"quit":  "退出程式",
		},
	}
}
