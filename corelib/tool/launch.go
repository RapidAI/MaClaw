// Package tool 包含工具系统的核心类型和接口。
package tool

import "context"

// ToolLaunchMode 定义工具启动模式。
type ToolLaunchMode int

const (
	// LaunchInteractive 交互模式：工具接管终端（TUI 前台 exec）或弹出新窗口（GUI）。
	LaunchInteractive ToolLaunchMode = iota
	// LaunchHeadless 无头模式：工具以非交互方式运行（--print / --output-format json）。
	LaunchHeadless
)

// LaunchOptions 工具启动选项。
type LaunchOptions struct {
	ProjectDir string
	Tool       string // "claude", "codex", "opencode", ...
	Mode       ToolLaunchMode
	Env        map[string]string // 额外环境变量
	Args       []string          // 额外命令行参数
	YoloMode   bool
	AdminMode  bool
}

// ToolLauncher 是工具启动的抽象接口。
// GUI 和 TUI 各自提供不同的实现。
type ToolLauncher interface {
	// Launch 启动指定工具。返回 nil 表示工具正常退出。
	Launch(ctx context.Context, opts LaunchOptions) error

	// SupportsMode 检查当前环境是否支持指定的启动模式。
	SupportsMode(mode ToolLaunchMode) bool
}
