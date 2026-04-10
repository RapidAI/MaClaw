package plugin

import "context"

// Plugin 是所有插件必须实现的统一接口。
type Plugin interface {
	// Manifest 返回插件的静态元数据。
	Manifest() PluginManifest

	// Init 初始化插件，传入配置。在 Start 之前调用。
	Init(cfg PluginConfig) error

	// Start 启动插件（建立连接、注册 webhook 等）。
	Start(ctx context.Context) error

	// Stop 优雅停止插件。
	Stop(ctx context.Context) error

	// Tools 返回该插件提供的所有工具定义。
	Tools() []ToolDefinition

	// Health 返回插件当前健康状态。
	Health() HealthStatus
}

// HookProvider 是可选接口，支持注册 hooks 的插件实现此接口。
type HookProvider interface {
	Hooks() []HookDefinition
}

// CLIProvider 是可选接口，支持注册 CLI 命令的插件实现此接口。
type CLIProvider interface {
	Commands() []CLICommand
}
