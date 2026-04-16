//go:build oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:              "qianxin",
		DisplayName:     "TigerClaw",
		DisplayNameCN:   "虎爪",
		WindowTitle:     "TigerClaw",
		TrayTooltip:     "TigerClaw Dashboard",
		Slogan:          "像虎一样灵巧勇猛。",
		Author:          "Dr. Daniel",
		BusinessContact: "联系信息：QianXin",
		WebsiteURL:      "https://www.qianxin.com",
		GitHubURL:       "",
		IconPath:        "assets/qianxin.png",
		IcnsPath:        "assets/qianxin.icns",
		IcoPath:         "assets/tigerclaw.ico",
		MobileAppName:   "TigerClaw",
		ExtraTools: []ExtraToolDef{
			{
				Name:        "tigerclaw",
				DisplayName: "TigerClaw Code",
				ConfigKey:   "tigerclaw",
			},
		},
		DefaultTool:         "claude",
		DefaultToolProvider: "codegen",
	}
}
