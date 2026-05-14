package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type appLanguage string

const (
	langEnglish appLanguage = "en"
	langChinese appLanguage = "zh"
)

var activeLanguage = detectLanguage()

var translations = map[appLanguage]map[string]string{
	langChinese: {
		"app.title":           "Ins-maclaw \u5b89\u88c5\u5411\u5bfc",
		"language":            "\u8bed\u8a00\uff1a\u7b80\u4f53\u4e2d\u6587",
		"sidebar.subtitle":    "\u5728\u7ebf\u5b89\u88c5\u5668",
		"sidebar.secure":      "\u5b98\u65b9\u955c\u50cf \u00b7 \u6821\u9a8c\u4e0b\u8f7d",
		"choose.brand":        "\u9009\u62e9\u4ea7\u54c1\u54c1\u724c",
		"brand.maclaw.desc":   "\u9ed8\u8ba4\u539f\u5382\u53d1\u5e03\u901a\u9053\uff0c\u9002\u5408\u5927\u591a\u6570\u7528\u6237\u3002",
		"brand.tiger.desc":    "\u5947\u5b89\u4fe1 OEM \u7248\u672c\uff0c\u4f7f\u7528\u5bf9\u5e94\u54c1\u724c\u5b89\u88c5\u5305\u3002",
		"welcome.title":       "\u6b22\u8fce\u4f7f\u7528 Ins-maclaw",
		"welcome.body":        "\u6b64\u5411\u5bfc\u5c06\u6309\u5728\u7ebf\u66f4\u65b0\u76f8\u540c\u903b\u8f91\u68c0\u6d4b\u6700\u65b0\u7248\uff0c\u5e76\u4ece GitHub / R2 / COS \u955c\u50cf\u4e0b\u8f7d\u5b98\u65b9\u5b89\u88c5\u5305\u3002",
		"hint.tui":            "\u4e5f\u53ef\u8fd0\u884c\uff1aIns-maclaw.exe -mode tui \u6216 -brand tigerclaw",
		"next":                "\u4e0b\u4e00\u6b65",
		"cancel":              "\u53d6\u6d88",
		"cancelling":          "\u6b63\u5728\u53d6\u6d88...",
		"close":               "\u5173\u95ed",
		"working":             "\u5904\u7406\u4e2d",
		"installing":          "\u6b63\u5728\u5b89\u88c5 %s",
		"checking":            "\u6b63\u5728\u68c0\u6d4b\u6700\u65b0\u7248\u548c\u4e0b\u8f7d\u955c\u50cf...",
		"step.select":         "\u6b65\u9aa4 1/3\uff1a\u9009\u62e9\u4ea7\u54c1\u54c1\u724c",
		"step.download":       "\u6b65\u9aa4 2/3\uff1a\u4e0b\u8f7d\u5e76\u51c6\u5907\u5b89\u88c5\u5305",
		"step.done":           "\u6b65\u9aa4 3/3\uff1a\u5b8c\u6210",
		"side.step.select":    "\u9009\u62e9\u54c1\u724c",
		"side.step.download":  "\u4e0b\u8f7d\u5b89\u88c5",
		"side.step.done":      "\u5b8c\u6210",
		"downloading.bytes":   "\u6b63\u5728\u4e0b\u8f7d\uff1a%s",
		"downloading.percent": "\u6b63\u5728\u4e0b\u8f7d\uff1a%d%% (%s / %s)",
		"failed.title":        "\u5b89\u88c5\u5931\u8d25",
		"failed.body":         "Ins-maclaw \u672a\u80fd\u5b8c\u6210\u4e0b\u8f7d\u6216\u542f\u52a8\u5b89\u88c5\u6b65\u9aa4\u3002",
		"completed.title":     "\u5b8c\u6210",
		"completed.body":      "\u6240\u9009\u5b89\u88c5\u5305\u5df2\u51c6\u5907\u5c31\u7eea\u3002",
		"confirm.install":     "\u662f\u5426\u4e0b\u8f7d\u5e76\u5b89\u88c5\u6700\u65b0\u7248 %s\uff1f",
		"status.checking":     "%s\n\n\u6b63\u5728\u68c0\u6d4b\u6700\u65b0\u7248\u548c\u4e0b\u8f7d\u955c\u50cf\u3002\u5173\u95ed\u6b64\u7a97\u53e3\u540e\u5c06\u7ee7\u7eed\u5b89\u88c5\u3002",
		"result.uptodate":     "\u5df2\u662f\u6700\u65b0\u7248\u672c\u3002\n\n\u6700\u65b0\u7248\u672c\uff1a%s\n\u5b89\u88c5\u5305\uff1a%s",
		"result.check":        "\u5df2\u627e\u5230\u6700\u65b0\u7248\u672c\u3002\n\n\u6700\u65b0\u7248\u672c\uff1a%s\n\u5b89\u88c5\u5305\uff1a%s\n\u6765\u6e90\uff1a%s",
		"result.downloaded":   "\u4e0b\u8f7d\u5b8c\u6210\u3002\n\n\u6700\u65b0\u7248\u672c\uff1a%s\n\u6587\u4ef6\uff1a%s",
		"result.launched":     "\u5b89\u88c5\u5668\u5df2\u542f\u52a8\u3002\n\n\u6700\u65b0\u7248\u672c\uff1a%s\n\u6587\u4ef6\uff1a%s",
		"tui.choose":          "\u9009\u62e9\u4ea7\u54c1\u54c1\u724c\uff1a",
		"tui.select":          "\u8bf7\u9009\u62e9 [%d]\uff1a",
		"tui.install.confirm": "\u73b0\u5728\u5b89\u88c5\u6700\u65b0\u7248 %s\uff1f[Y/n]\uff1a",
		"tui.cancelled":       "\u5df2\u53d6\u6d88\u3002",
		"cli.product":         "\u4ea7\u54c1\uff1a%s",
		"cli.target":          "\u76ee\u6807\uff1a%s/%s -> %s",
		"cli.checking":        "\u6b63\u5728\u68c0\u6d4b\u6700\u65b0\u7248...",
		"cli.latest":          "\u6700\u65b0\u7248\uff1a%s via %s",
		"cli.uptodate":        "\u5df2\u662f\u6700\u65b0\u7248\u672c\uff1acurrent=%s latest=%s",
		"cli.downloading":     "\u6b63\u5728\u4e0b\u8f7d\u5b89\u88c5\u5305...",
		"cli.downloaded":      "\u5df2\u4e0b\u8f7d\uff1a%s",
		"cli.launching":       "\u6b63\u5728\u542f\u52a8\u5b89\u88c5\u5668...",
		"invalid.brand":       "\u672a\u77e5\u54c1\u724c %q\uff1b\u652f\u6301\uff1amaclaw, tigerclaw",
		"invalid.selection":   "\u65e0\u6548\u54c1\u724c\u9009\u62e9\uff1a%s",
	},
	langEnglish: {
		"app.title":           "Ins-maclaw Setup Wizard",
		"language":            "Language: English",
		"sidebar.subtitle":    "Online installer",
		"sidebar.secure":      "Official mirrors - Verified download",
		"choose.brand":        "Choose product brand",
		"brand.maclaw.desc":   "Default official release channel for most users.",
		"brand.tiger.desc":    "QiAnXin OEM edition with the matching branded package.",
		"welcome.title":       "Welcome to Ins-maclaw",
		"welcome.body":        "This wizard checks the latest release with the same online update logic and downloads the official installer from GitHub / R2 / COS mirrors.",
		"hint.tui":            "You can also run: Ins-maclaw.exe -mode tui or -brand tigerclaw",
		"next":                "Next",
		"cancel":              "Cancel",
		"cancelling":          "Cancelling...",
		"close":               "Close",
		"working":             "Working",
		"installing":          "Installing %s",
		"checking":            "Checking latest release and download mirrors...",
		"step.select":         "Step 1 of 3: Choose product brand",
		"step.download":       "Step 2 of 3: Download and prepare installer",
		"step.done":           "Step 3 of 3: Complete",
		"side.step.select":    "Select",
		"side.step.download":  "Download",
		"side.step.done":      "Finish",
		"downloading.bytes":   "Downloading: %s",
		"downloading.percent": "Downloading: %d%% (%s / %s)",
		"failed.title":        "Installation failed",
		"failed.body":         "Ins-maclaw could not complete the download or launch step.",
		"completed.title":     "Completed",
		"completed.body":      "The selected installer is ready.",
		"confirm.install":     "Download and install latest %s?",
		"status.checking":     "%s\n\nChecking the latest release and download mirrors. The installer will continue after this dialog closes.",
		"result.uptodate":     "Already up to date.\n\nLatest: %s\nAsset: %s",
		"result.check":        "Latest release found.\n\nLatest: %s\nAsset: %s\nSource: %s",
		"result.downloaded":   "Download completed.\n\nLatest: %s\nFile: %s",
		"result.launched":     "Installer launched.\n\nLatest: %s\nFile: %s",
		"tui.choose":          "Choose product brand:",
		"tui.select":          "Select [%d]: ",
		"tui.install.confirm": "Install latest %s now? [Y/n]: ",
		"tui.cancelled":       "Cancelled.",
		"cli.product":         "Product: %s",
		"cli.target":          "Target:  %s/%s -> %s",
		"cli.checking":        "Checking latest release...",
		"cli.latest":          "Latest: %s via %s",
		"cli.uptodate":        "Already up to date: current=%s latest=%s",
		"cli.downloading":     "Downloading installer...",
		"cli.downloaded":      "Downloaded: %s",
		"cli.launching":       "Launching installer...",
		"invalid.brand":       "unknown brand %q; supported: maclaw, tigerclaw",
		"invalid.selection":   "invalid brand selection: %s",
	},
}

func detectLanguage() appLanguage {
	if forced := strings.ToLower(strings.TrimSpace(os.Getenv("INS_MACLAW_LANG"))); forced != "" {
		if strings.HasPrefix(forced, "zh") || strings.Contains(forced, "cn") {
			return langChinese
		}
		return langEnglish
	}
	if runtime.GOOS == "windows" && windowsUILanguageIsChinese() {
		return langChinese
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		value := strings.ToLower(os.Getenv(key))
		if strings.HasPrefix(value, "zh") || strings.Contains(value, "zh_cn") || strings.Contains(value, "zh-hans") {
			return langChinese
		}
	}
	return langEnglish
}

func setLanguage(input string) error {
	input = strings.ToLower(strings.TrimSpace(input))
	switch input {
	case "", "auto":
		activeLanguage = detectLanguage()
	case "zh", "zh-cn", "cn", "chinese", "\u4e2d\u6587":
		activeLanguage = langChinese
	case "en", "en-us", "english":
		activeLanguage = langEnglish
	default:
		return fmt.Errorf("unknown language %q; supported: auto, zh, en", input)
	}
	return nil
}

func languageName() string {
	if activeLanguage == langChinese {
		return "\u7b80\u4f53\u4e2d\u6587"
	}
	return "English"
}

func brandLabel(brand brandOption) string {
	if brand.ID == "qianxin" {
		if activeLanguage == langChinese {
			return "TigerClaw (\u5947\u5b89\u4fe1 OEM \u7248)"
		}
		return "TigerClaw (QiAnXin OEM Edition)"
	}
	if activeLanguage == langChinese {
		return "MaClaw (\u539f\u5382\u54c1\u724c)"
	}
	return "MaClaw (Original Brand)"
}

func tr(key string) string {
	if lang, ok := translations[activeLanguage]; ok {
		if value, ok := lang[key]; ok {
			return value
		}
	}
	if value, ok := translations[langEnglish][key]; ok {
		return value
	}
	return key
}
