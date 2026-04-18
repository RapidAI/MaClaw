//go:build !oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:              "maclaw",
		DisplayName:     "MaClaw",
		DisplayNameCN:   "码卡龙",
		WindowTitle:     "码卡龙 MaClaw（磐石）",
		TrayTooltip:     "MaClaw Dashboard",
		Slogan:          "你的数智时代伙伴。",
		Author:          "Dr. Daniel",
		BusinessContact: "联系信息：微信 znsoft",
		WebsiteURL:      "https://maclaw.top",
		GitHubURL:       "https://github.com/nicedoc/maclaw",
		IconPath:        "build/appicon.png",
		IcnsPath:        "build/AppIcon.icns",
		IcoPath:         "build/windows/icon.ico",
		MobileAppName:   "MaClaw Chat",
		ExtraTools:      []ExtraToolDef{},
		DefaultTool:         "claude",
		DefaultToolProvider: "",
	}
}
