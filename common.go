package main

var trayTranslations = map[string]map[string]string{
	"en": {
		"title": "MaClaw Dashboard",
		"show":  "Show Main Window",
		"hide":  "Hide Main Window",
		"quit":  "Quit MaClaw",
	},
	"zh-Hans": {
		"title": "码卡龙 控制台",
		"show":  "显示主窗口",
		"hide":  "隐藏主窗口",
		"quit":  "退出程序",
	},
	"zh-Hant": {
		"title": "碼卡龍 控制台",
		"show":  "顯示主視窗",
		"hide":  "隱藏主視窗",
		"quit":  "退出程式",
	},
}

const RequiredNodeVersion = "24.13.0"
