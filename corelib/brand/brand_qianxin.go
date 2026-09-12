//go:build oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:              "qianxin",
		DisplayName:     "QAgent",
		DisplayNameCN:   "虎爪",
		WindowTitle:     "QAgent",
		TrayTooltip:     "QAgent Dashboard",
		Slogan:          "像虎一样灵巧勇猛。",
		Author:          "Dr. Daniel",
		BusinessContact: "联系信息：QianXin",
		WebsiteURL:      "https://www.qianxin.com",
		GitHubURL:       "",
		IconPath:        "assets/qianxin.png",
		IcnsPath:        "assets/qianxin.icns",
		IcoPath:         "assets/tigerclaw.ico",
		MobileAppName:   "QAgent",
		ExtraTools: []ExtraToolDef{
			{
				Name:        "QAgent",
				DisplayName: "QAgent Code",
				ConfigKey:   "QAgent",
			},
		},
		DefaultTool:         "claude",
		DefaultToolProvider: "codegen",
	}
}
