//go:build !oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:              "maclaw",
		DisplayName:     "MaClaw",
		DisplayNameCN:   "码卡龙",
		WindowTitle:     "MaClaw",
		TrayTooltip:     "MaClaw Dashboard",
		Slogan:          "让远程编程像品尝甜点一样丝滑。",
		Author:          "Dr. Daniel",
		BusinessContact: "商业合作：微信 znsoft",
		WebsiteURL:      "https://maclaw.top",
		GitHubURL:       "https://github.com/nicedoc/maclaw",
		IconPath:        "build/appicon.png",
		IcnsPath:        "build/AppIcon.icns",
		IcoPath:         "build/windows/icon.ico",
		MobileAppName:   "MaClaw Chat",
		ExtraTools:      []ExtraToolDef{},
	}
}
