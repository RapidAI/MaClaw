# 实现计划：maclaw-unified-plugin（统一 Plugin 接口抽象层）

## 概述

将 MCP Server、NLSkill、Local MCP 统一为 Plugin 的不同实现类型，通过三层发现机制和 PluginRegistry 进行生命周期管理。采用 Adapter 模式渐进式迁移，确保零破坏性变更。实现语言为 Go，核心代码放在 `corelib/plugin/` 包下，CLI 命令放在 `tui/commands/` 下。

## Tasks

- [x] 1. 定义核心类型和接口
  - [x] 1.1 创建 `corelib/plugin/types.go`，定义 PluginType、PluginScope、PluginManifest、PluginConfig、PluginLogger、ToolDefinition、HookDefinition、CLICommand、HealthStatus、PluginInfo 类型
    - 定义 `PluginType` 常量：`mcp`、`local_mcp`、`nlskill`、`native`
    - 定义 `PluginScope` 常量：`user`、`project`、`package`、`builtin`
    - 定义 `PluginManifest` 结构体，包含 Name、Version、Description、Type、Scope、Author、Tags、Platforms、Dir 字段
    - 定义 `PluginConfig` 结构体，包含 DataDir、Settings、Logger 字段
    - 定义 `ToolDefinition`、`HookDefinition`、`CLICommand`、`HealthStatus`、`PluginInfo` 结构体
    - _Requirements: 1.1, 1.4, 2.1_

  - [x] 1.2 创建 `corelib/plugin/plugin.go`，定义 Plugin、HookProvider、CLIProvider 接口
    - `Plugin` 接口包含 Manifest()、Init()、Start()、Stop()、Tools()、Health() 六个方法
    - `HookProvider` 可选接口包含 Hooks() 方法
    - `CLIProvider` 可选接口包含 Commands() 方法
    - _Requirements: 1.1, 1.2, 1.3_

  - [x]* 1.3 为核心类型编写单元测试
    - 测试 PluginType、PluginScope 常量值
    - 测试 PluginManifest 的 JSON/YAML 序列化
    - _Requirements: 1.1, 1.4_

- [x] 2. 实现 plugin.yaml 解析与验证
  - [x] 2.1 创建 `corelib/plugin/manifest.go`，实现 plugin.yaml 解析器
    - 实现 `ParseManifestFile(path string) (*PluginManifest, error)` 函数
    - 解析 name、version、description、type、author、tags、platforms 字段
    - 解析 mcp、local_mcp、nlskill 类型的特有配置段到 `RawTypeConfig map[string]interface{}`
    - 解析 settings 字段到 `Settings map[string]interface{}`
    - 验证 name 非空、type 为已知 PluginType，否则返回错误
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [x] 2.2 实现 PluginManifest 序列化为 YAML
    - 实现 `FormatManifestFile(m *PluginManifest) ([]byte, error)` 函数
    - 确保往返一致性（序列化后反序列化产生等价对象）
    - _Requirements: 11.1, 11.2, 11.3_

  - [x]* 2.3 为 manifest 解析编写属性测试
    - **Property 3: 往返一致性** — 任意有效 PluginManifest 序列化后再反序列化产生等价对象
    - **Validates: Requirements 11.3**

  - [x]* 2.4 为 manifest 解析编写单元测试
    - 测试正常 plugin.yaml 解析
    - 测试 name 为空时返回错误
    - 测试 type 为未知值时返回错误
    - 测试非法 YAML 格式时返回错误
    - 测试包含 mcp/local_mcp/nlskill 特有配置段的解析
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 3. 实现环境变量解析和安全机制
  - [x] 3.1 创建 `corelib/plugin/envresolve.go`，实现 `${ENV_VAR}` 语法解析
    - 实现 `ResolveEnvVars(settings map[string]interface{}) map[string]interface{}` 函数
    - 递归遍历 settings map，将 `${VAR_NAME}` 替换为 `os.Getenv("VAR_NAME")`
    - _Requirements: 9.1_

  - [x]* 3.2 为环境变量解析编写单元测试
    - 测试正常替换、嵌套 map 替换、环境变量不存在时保留原值
    - _Requirements: 9.1_

- [x] 4. Checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户提问。

- [x] 5. 实现 DiscoveryManager（三层插件发现）
  - [x] 5.1 创建 `corelib/plugin/discovery.go`，实现 DiscoveryManager
    - 实现 `NewDiscoveryManager()` 构造函数
    - 实现 `DiscoverAll(projectDir string) ([]PluginManifest, error)` 方法
    - 按优先级扫描三层目录：project（`.maclaw/plugins/`）> user（`~/.maclaw/plugins/`）> package（EntryPointProvider）
    - 同名插件按优先级去重，高优先级覆盖低优先级
    - 目录不存在时返回空列表且不报错
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [x] 5.2 实现 `scanDirectory(dir string, scope PluginScope) []PluginManifest` 内部方法
    - 遍历目录下的子目录，查找 `plugin.yaml` 文件
    - 解析有效的 manifest，跳过无效的并记录 WARN 日志
    - 设置 manifest 的 Scope 和 Dir 字段
    - _Requirements: 3.1, 3.2, 3.5_

  - [x] 5.3 定义 `EntryPointProvider` 接口并实现包级插件收集
    - 定义 `EntryPointProvider` 接口：`Plugins() []Plugin`
    - 在 DiscoveryManager 中支持注册 EntryPointProvider
    - _Requirements: 3.3_

  - [x]* 5.4 为 DiscoveryManager 编写属性测试
    - **Property 1: 名称唯一性** — 任意数量的 manifest 经过 DiscoverAll 后，结果中 Name 唯一
    - **Validates: Requirements 3.6, 8.2**

  - [x]* 5.5 为 DiscoveryManager 编写单元测试
    - 使用临时目录模拟三层目录结构
    - 测试优先级覆盖逻辑（project > user > package）
    - 测试目录不存在时返回空列表
    - 测试无效 plugin.yaml 被跳过
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 6. 实现 PluginRegistry（生命周期管理）
  - [x] 6.1 创建 `corelib/plugin/registry.go`，实现 PluginRegistry
    - 实现 `NewPluginRegistry(toolReg *tool.Registry)` 构造函数
    - 实现 `Register(p Plugin) error` 方法，同名插件返回错误
    - 实现 `Unregister(name string) error` 方法，调用 Stop() 并移除 tools
    - 实现 `List() []PluginInfo` 方法，返回所有插件信息
    - 使用 `sync.RWMutex` 保证并发安全
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 8.1, 8.2_

  - [x] 6.2 实现 `LoadAndStart(ctx context.Context, manifests []PluginManifest) error` 方法
    - 根据 manifest.Type 创建对应的 Adapter（工厂模式）
    - 按 Init → Start 顺序调用插件方法
    - Init 失败时设状态为 "error" 并跳过 Start
    - Start 失败时设状态为 "error"
    - 成功启动时设状态为 "running"
    - 单个插件失败不影响其余插件
    - 将插件的 Tools 注册到 `tool.Registry`
    - 检查并注册 HookProvider 和 CLIProvider
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 5.1, 6.6_

  - [x] 6.3 实现 Unregister 中的 Stop 超时机制
    - Stop 方法使用 `context.WithTimeout(ctx, 10*time.Second)`
    - 超时后强制取消 context 并记录 ERROR 日志
    - 从 `tool.Registry` 移除该插件注册的所有工具
    - _Requirements: 4.6, 4.7, 5.2_

  - [x]* 6.4 为 PluginRegistry 编写属性测试
    - **Property 2: 注册/注销一致性** — 任意顺序注册/注销插件后，registry 中 Name 唯一且状态一致
    - **Validates: Requirements 8.1, 8.2**

  - [x]* 6.5 为 PluginRegistry 编写单元测试
    - 测试注册、注销、生命周期状态转换
    - 测试同名插件注册返回错误
    - 测试 Init 失败时状态为 "error"
    - 测试 Start 失败时状态为 "error"
    - 测试单个插件失败不影响其余插件
    - 测试 Unregister 调用 Stop 并移除 tools
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 5.1, 5.2, 8.1_

- [x] 7. Checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户提问。

- [x] 8. 实现 Adapter 层（桥接现有系统）
  - [x] 8.1 创建 `corelib/plugin/adapter_mcp.go`，实现 MCPPluginAdapter
    - 将远程 MCP Server 封装为 Plugin 接口实现
    - Start 时从远程 MCP Server 获取工具列表并转换为 ToolDefinition
    - 每个 ToolDefinition 的 Handler 封装对 MCP Server 的 RPC 调用
    - 服务不可达时返回空工具列表且 Health 状态为 "unhealthy"
    - 复用 `corelib.MCPServerEntry` 中的配置字段
    - _Requirements: 6.1, 6.4, 6.5_

  - [x] 8.2 创建 `corelib/plugin/adapter_local_mcp.go`，实现 LocalMCPPluginAdapter
    - 将本地 stdio MCP Server 封装为 Plugin 接口实现
    - Start 时启动本地进程并获取工具列表
    - Stop 时优雅终止本地进程
    - 复用 `corelib.LocalMCPServerEntry` 中的配置字段
    - _Requirements: 6.2_

  - [x] 8.3 创建 `corelib/plugin/adapter_nlskill.go`，实现 NLSkillPluginAdapter
    - 将 NLSkill 封装为 Plugin 接口实现
    - 将 NLSkillEntry 的 Steps 转换为单个 ToolDefinition（Handler 执行 skill steps）
    - 复用 `corelib.NLSkillEntry` 和 `corelib/skill` 包的逻辑
    - _Requirements: 6.3_

  - [x] 8.4 实现 Adapter 工厂函数 `createAdapter(manifest PluginManifest) Plugin`
    - 根据 manifest.Type 返回对应的 Adapter 实例
    - 未知类型返回 nil
    - _Requirements: 6.6_

  - [x]* 8.5 为 Adapter 层编写单元测试
    - 测试 MCPPluginAdapter 的 Tools() 输出格式
    - 测试 LocalMCPPluginAdapter 的生命周期
    - 测试 NLSkillPluginAdapter 的 ToolDefinition 转换
    - 测试工厂函数对各类型的分发
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

- [x] 9. 实现插件健康检查
  - [x] 9.1 在 `corelib/plugin/registry.go` 中添加后台健康检查 goroutine
    - 启动时创建后台 goroutine，每 60 秒调用所有 running 插件的 Health() 方法
    - 远程 MCP Server 连接恢复时自动更新 Health 状态
    - 查询插件列表时返回每个插件的当前健康状态
    - 支持通过 context 取消健康检查
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [x]* 9.2 为健康检查编写单元测试
    - 使用 mock Plugin 测试健康状态更新
    - 测试健康检查 goroutine 的启动和停止
    - _Requirements: 7.1, 7.3_

- [x] 10. 实现项目级插件信任确认
  - [x] 10.1 在 `corelib/plugin/trust.go` 中实现信任确认机制
    - 实现 `TrustStore` 管理已信任的项目级插件列表
    - 首次加载项目级插件时，通过回调函数提示用户确认信任
    - 已信任的插件记录到 `~/.maclaw/data/trusted_plugins.json`
    - _Requirements: 9.2_

  - [x]* 10.2 为信任确认编写单元测试
    - 测试首次加载提示确认
    - 测试已信任插件跳过确认
    - _Requirements: 9.2_

- [x] 11. Checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户提问。

- [x] 12. 实现 CLI 插件管理命令
  - [x] 12.1 创建 `tui/commands/plugin.go`，实现 `RunPlugin(args []string) error` 入口函数
    - 支持子命令分发：list、info、enable、disable
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

  - [x] 12.2 实现 `plugin list` 子命令
    - 显示所有已注册插件的名称、类型、状态和工具数量
    - 支持 `--json` 参数以 JSON 格式输出
    - _Requirements: 10.1, 10.2_

  - [x] 12.3 实现 `plugin info <name>` 子命令
    - 显示指定插件的详细信息（名称、版本、描述、类型、Scope、状态、工具列表、Hook 列表）
    - _Requirements: 10.3_

  - [x] 12.4 实现 `plugin enable <name>` 和 `plugin disable <name>` 子命令
    - enable：启动指定插件并将状态设为 "running"
    - disable：停止指定插件并将状态设为 "stopped"
    - _Requirements: 10.4, 10.5_

  - [x] 12.5 在 `tui/main.go` 中注册 `plugin` 子命令
    - 将 `RunPlugin` 添加到 TUI 命令路由表
    - _Requirements: 10.1_

  - [ ]* 12.6 为 CLI 命令编写单元测试
    - 测试 list、info、enable、disable 子命令的输出格式
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

- [x] 13. 集成到应用启动流程
  - [x] 13.1 在应用启动代码中集成 PluginRegistry
    - 在 GUI（`gui/app.go`）和 TUI（`tui/main.go`）启动流程中创建 DiscoveryManager 和 PluginRegistry
    - 调用 `DiscoverAll()` 和 `LoadAndStart()` 加载所有插件
    - 将 PluginRegistry 的 tool.Registry 与现有的 DynamicToolBuilder/Router 共享
    - _Requirements: 5.1, 5.4_

  - [x] 13.2 确保插件工具与现有工具调用路径兼容
    - 通过 Plugin 接口注册的工具与直接通过 `tool.Registry.Register()` 注册的工具行为一致
    - Agent 调用插件工具时通过 ToolDefinition 的 Handler 转发到对应 Adapter
    - _Requirements: 5.3, 5.4_

  - [ ]* 13.3 编写集成测试
    - 创建临时 plugin 目录，放入 plugin.yaml，验证 DiscoverAll → LoadAndStart → tool 可调用
    - 验证通过 plugin 接口注册的 MCP tools 与直接通过 `mcp add` 注册的行为一致
    - _Requirements: 5.1, 5.3, 6.1, 6.2, 6.3_

- [x] 14. Final checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户提问。

## Notes

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保可追溯性
- Checkpoint 任务确保增量验证
- 属性测试使用 Go 标准库 `testing/quick` 验证正确性属性
- 单元测试验证具体示例和边界情况
- Adapter 模式确保现有 MCP/NLSkill/Tool Registry 代码零修改
