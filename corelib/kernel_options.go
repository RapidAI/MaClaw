package corelib

import "github.com/RapidAI/CodeClaw/corelib/tool"

// KernelOptions 是创建 Kernel 实例时的配置选项。
type KernelOptions struct {
	// DataDir 数据目录（配置、数据库、日志等的根目录）。
	DataDir string

	// HubURL Hub 服务器 WebSocket 地址。
	HubURL string

	// HubToken Hub 认证令牌。
	HubToken string

	// MachineID 当前机器唯一标识。
	MachineID string

	// Logger 外部注入的日志实现。为 nil 时使用 DefaultLogger。
	Logger Logger

	// EventEmitter 外部注入的事件分发器。为 nil 时使用 NoopEmitter。
	EventEmitter EventEmitter

	// PlatformOverride 覆盖自动检测的平台能力。为 nil 时自动检测。
	PlatformOverride *PlatformCapabilities

	// ConfigPath 配置文件路径。为空则使用 DataDir 下的默认路径。
	ConfigPath string

	// AgentMaxIterations Agent 最大迭代次数（0=默认300，30-300=固定上限）。
	AgentMaxIterations int

	// ToolLauncher 工具启动器，由上层（GUI/TUI）注入。
	ToolLauncher tool.ToolLauncher

	// AgentNetEnabled 是否启用智网 (AgentNet) 自动任务拾取。
	AgentNetEnabled bool
}
