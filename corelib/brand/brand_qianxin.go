//go:build oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:              "qianxin",
		DisplayName:     "TigerClaw",
		DisplayNameCN:   "虎爪",
		WindowTitle:     "TigerClaw",
		TrayTooltip:     "TigerClaw Dashboard",
		Slogan:          "AI Native 组织操作系统",
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
				Name:        "QAgent",
				DisplayName: "QAgent Code",
				ConfigKey:   "QAgent",
			},
		},
		DefaultTool:         "claude",
		DefaultToolProvider: "codegen",
	}
}
