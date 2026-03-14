package main

var trayTranslations = map[string]map[string]string{
	"en": {
		"title":   "MaClaw Dashboard",
		"show":    "Show Main Window",
		"hide":    "Hide Main Window",
		"launch":  "Start Coding",
		"quit":    "Quit MaClaw",
		"models":  "Providers",
		"actions": "Actions",
	},
	"zh-Hans": {
		"title":   "码卡龙 控制台",
		"show":    "显示主窗口",
		"hide":    "隐藏主窗口",
		"launch":  "开始编码",
		"quit":    "退出程序",
		"models":  "服务商选择",
		"actions": "操作",
	},
	"zh-Hant": {
		"title":   "碼卡龍 控制台",
		"show":    "顯示主視窗",
		"hide":    "隱藏主視窗",
		"launch":  "開始編碼",
		"quit":    "退出程式",
		"models":  "服務商選擇",
		"actions": "操作",
	},
}

const RequiredNodeVersion = "24.13.0"
