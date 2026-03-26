package brand

import "fmt"

// BrandConfig 描述一个品牌变体的完整配置。
type BrandConfig struct {
	ID              string         // 品牌标识符，如 "maclaw"、"qianxin"
	DisplayName     string         // 产品显示名称，如 "MaClaw"、"TigerClaw"
	DisplayNameCN   string         // 中文显示名称，如 "码卡龙"、"虎爪"
	WindowTitle     string         // GUI 窗口标题
	TrayTooltip     string         // 系统托盘提示文字
	Slogan          string         // 产品标语
	Author          string         // 作者信息
	BusinessContact string         // 商业合作联系方式
	WebsiteURL      string         // 官方网站
	GitHubURL       string         // GitHub 地址
	IconPath        string         // 桌面图标资源路径
	IcnsPath        string         // macOS icns 路径
	IcoPath         string         // Windows ico 路径
	MobileAppName   string         // 移动端应用名称
	ExtraTools      []ExtraToolDef // 额外工具列表
}

// ExtraToolDef 描述一个 OEM 额外工具。
type ExtraToolDef struct {
	Name           string                                                             // 工具内部名称，如 "tigerclaw"
	DisplayName    string                                                             // 显示名称
	ConfigKey      string                                                             // AppConfig 中的配置键名
	EnvBuilderFunc func(cfg interface{}, model interface{}, projectDir string) map[string]string // 环境变量构建函数
	OnboardingFunc func(projectDir string, env map[string]string)                     // 首次启动预配置函数
}

// currentBrand 存储当前编译时激活的品牌配置，由 init() 设置。
var currentBrand BrandConfig

// Current 返回当前编译时激活的品牌配置。
func Current() BrandConfig {
	return currentBrand
}

// IsDefault 返回当前品牌是否为默认品牌。
func IsDefault() bool {
	return currentBrand.ID == "maclaw"
}

// RegisterExtraTools 遍历当前品牌的 ExtraTools，将它们注册到工具注册表中。
// registry 是一个 map[string]bool，键为已注册的工具名称。
// 如果某个额外工具的 Name 与 registry 中已有的名称冲突，返回 error 且不注册任何工具。
func RegisterExtraTools(registry map[string]bool) error {
	tools := Current().ExtraTools
	// 先检查所有工具名是否冲突
	for _, t := range tools {
		if registry[t.Name] {
			return fmt.Errorf("extra tool %q conflicts with existing tool name", t.Name)
		}
	}
	// 无冲突，全部注册
	for _, t := range tools {
		registry[t.Name] = true
	}
	return nil
}
