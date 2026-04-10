package plugin

import "context"

// PluginType 标识插件的底层实现类型。
type PluginType string

const (
	PluginTypeMCP      PluginType = "mcp"       // 远程 MCP Server
	PluginTypeLocalMCP PluginType = "local_mcp"  // 本地 stdio MCP Server
	PluginTypeNLSkill  PluginType = "nlskill"    // NL 技能
	PluginTypeNative   PluginType = "native"     // 原生 Go 插件
	PluginTypeScript   PluginType = "script"     // 脚本工具（shell/python 等，无需 MCP 协议）
)

// PluginScope 标识插件的发现来源。
type PluginScope string

const (
	ScopeUser    PluginScope = "user"    // ~/.maclaw/plugins/
	ScopeProject PluginScope = "project" // .maclaw/plugins/
	ScopePackage PluginScope = "package" // Go entry points
	ScopeBuiltin PluginScope = "builtin" // 内置
)

// PluginManifest 描述插件的静态元数据。
type PluginManifest struct {
	Name        string     `yaml:"name" json:"name"`
	Version     string     `yaml:"version" json:"version"`
	Description string     `yaml:"description" json:"description"`
	Type        PluginType `yaml:"type" json:"type"`
	Scope       PluginScope `json:"scope"`
	Author      string     `yaml:"author" json:"author"`
	Tags        []string   `yaml:"tags" json:"tags"`
	Platforms   []string   `yaml:"platforms" json:"platforms"` // 空=全平台
	Dir         string     `json:"dir"`                        // 插件目录绝对路径

	// RawTypeConfig 存储 mcp/local_mcp/nlskill 类型的特有配置段。
	RawTypeConfig map[string]interface{} `yaml:"-" json:"-"`

	// Settings 存储 plugin.yaml 中的自定义设置。
	Settings map[string]interface{} `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// PluginConfig 是传递给 Plugin.Init() 的运行时配置。
type PluginConfig struct {
	// DataDir 是插件可以存储持久化数据的目录。
	DataDir string

	// Settings 是从 plugin.yaml 的 settings 字段解析的自定义配置。
	Settings map[string]interface{}

	// Logger 是插件可以使用的日志接口。
	Logger PluginLogger
}

// PluginLogger 是插件使用的日志接口。
type PluginLogger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// ToolDefinition 描述插件提供的一个工具。
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Required    []string               `json:"required"`
	Tags        []string               `json:"tags"`
	Handler     func(args map[string]interface{}) (string, error) `json:"-"`
}

// HookDefinition 描述插件提供的一个 hook。
type HookDefinition struct {
	Name    string                                              `json:"name"`
	Event   string                                              `json:"event"` // "pre_tool_call", "post_tool_call", "on_message", etc.
	Handler func(ctx context.Context, payload interface{}) error `json:"-"`
}

// CLICommand 描述插件提供的一个 CLI 子命令。
type CLICommand struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Run         func(args []string) error `json:"-"`
}

// HealthStatus 描述插件的健康状态。
type HealthStatus struct {
	Status  string `json:"status"` // "healthy", "degraded", "unhealthy"
	Message string `json:"message,omitempty"`
}

// PluginInfo 是面向用户的插件信息视图。
type PluginInfo struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Type        PluginType   `json:"type"`
	Scope       PluginScope  `json:"scope"`
	Status      string       `json:"status"`
	ToolCount   int          `json:"tool_count"`
	HookCount   int          `json:"hook_count"`
	Health      HealthStatus `json:"health"`
	Error       string       `json:"error,omitempty"`
}
