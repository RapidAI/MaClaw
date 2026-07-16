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
			"title":      b.TrayTooltip,
			"show":       "Show Main Window",
			"hide":       "Hide Main Window",
			"quit":       "Quit " + b.DisplayName,
			"cu_menu":    "Computer Use",
			"cu_status":  "Status: idle",
			"cu_pause":   "Pause desktop actions",
			"cu_resume":  "Resume desktop actions",
			"cu_stop":    "Stop desktop control",
			"cu_reset":   "Reset control state",
			"cu_idle":    "Status: idle",
			"cu_active":  "Status: active",
			"cu_paused":  "Status: paused",
			"cu_stopped": "Status: stopped",
		},
		"zh-Hans": {
			"title":      zhHansTitle,
			"show":       "显示主窗口",
			"hide":       "隐藏主窗口",
			"quit":       "退出程序",
			"cu_menu":    "桌面操控 (Computer Use)",
			"cu_status":  "状态：空闲",
			"cu_pause":   "暂停桌面动作",
			"cu_resume":  "继续桌面动作",
			"cu_stop":    "停止桌面操控",
			"cu_reset":   "复位控制状态",
			"cu_idle":    "状态：空闲",
			"cu_active":  "状态：活动中",
			"cu_paused":  "状态：已暂停",
			"cu_stopped": "状态：已停止",
		},
		"zh-Hant": {
			"title":      b.DisplayName + " 控制台",
			"show":       "顯示主視窗",
			"hide":       "隱藏主視窗",
			"quit":       "退出程式",
			"cu_menu":    "桌面操控 (Computer Use)",
			"cu_status":  "狀態：空閒",
			"cu_pause":   "暫停桌面動作",
			"cu_resume":  "繼續桌面動作",
			"cu_stop":    "停止桌面操控",
			"cu_reset":   "復位控制狀態",
			"cu_idle":    "狀態：空閒",
			"cu_active":  "狀態：活動中",
			"cu_paused":  "狀態：已暫停",
			"cu_stopped": "狀態：已停止",
		},
	}
}
