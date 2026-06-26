//go:build oem_metastaff

package brand

func init() {
	currentBrand = BrandConfig{
		ID:                  "metastaff",
		DisplayName:         "MetaStaff",
		DisplayNameCN:       "智员",
		WindowTitle:         "智员 MetaStaff",
		TrayTooltip:         "MetaStaff Dashboard",
		Slogan:              "你的数智时代伙伴。",
		Author:              "Dr. Daniel",
		BusinessContact:     "联系信息：微信 znsoft",
		WebsiteURL:          "https://maclaw.top",
		GitHubURL:           "https://github.com/nicedoc/maclaw",
		IconPath:            "build/appicon.png",
		IcnsPath:            "build/AppIcon.icns",
		IcoPath:             "build/windows/icon.ico",
		MobileAppName:       "MetaStaff",
		ExtraTools:          []ExtraToolDef{},
		DefaultTool:         "claude",
		DefaultToolProvider: "",
	}
}
