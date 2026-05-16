# 需求文档

## 简介

maclaw 项目当前通过三套独立的扩展机制（MCP Server、NLSkill、Tool Registry）实现插件功能，导致配置格式、发现路径、生命周期管理和 CLI 命令碎片化。本需求定义一个统一的 Plugin 接口抽象层，将 MCP Server、NLSkill、Local MCP 统一为 plugin 的不同实现类型，通过三层发现机制（用户级、项目级、包级）和统一的 PluginRegistry 进行生命周期管理，同时通过 Adapter 模式实现渐进式迁移，确保零破坏性变更。

## 术语表

- **Plugin**：统一插件接口的实例，可以是 MCP Server、Local MCP、NLSkill 或原生 Go 插件
- **PluginRegistry**：管理所有已注册插件生命周期的中央注册表
- **DiscoveryManager**：负责从三层目录扫描和发现插件的组件
- **PluginManifest**：描述插件静态元数据的结构体，从 `plugin.yaml` 或代码中获取
- **PluginType**：插件的底层实现类型，包括 `mcp`、`local_mcp`、`nlskill`、`native`
- **PluginScope**：插件的发现来源层级，包括 `user`（用户级）、`project`（项目级）、`package`（包级）、`builtin`（内置）
- **PluginConfig**：传递给插件 Init 方法的运行时配置
- **ToolDefinition**：插件暴露的工具定义，包含名称、描述、输入模式和处理函数
- **HookDefinition**：插件暴露的 hook 定义，包含事件类型和处理函数
- **PluginAdapter**：将现有 MCP Server、NLSkill 等桥接到统一 Plugin 接口的适配器
- **Tool_Registry**：现有的 `corelib/tool` 工具注册表，Plugin 的工具注册到此处
- **plugin.yaml**：插件的声明式配置文件，定义插件元数据和类型特有配置
- **HealthStatus**：描述插件当前健康状态的结构体（healthy、degraded、unhealthy）

## 需求

### 需求 1：Plugin 统一接口

**用户故事：** 作为插件开发者，我希望有一个统一的 Plugin 接口，以便所有类型的插件都遵循相同的生命周期和工具暴露规范。

#### 验收标准

1. THE Plugin 接口 SHALL 定义 Manifest()、Init()、Start()、Stop()、Tools()、Health() 六个方法
2. WHEN 插件实现 HookProvider 接口时，THE PluginRegistry SHALL 注册该插件提供的所有 hooks
3. WHEN 插件实现 CLIProvider 接口时，THE PluginRegistry SHALL 注册该插件提供的所有 CLI 命令
4. THE Plugin 接口 SHALL 支持四种 PluginType：mcp、local_mcp、nlskill、native

### 需求 2：PluginManifest 解析与验证

**用户故事：** 作为系统管理员，我希望插件的元数据通过 `plugin.yaml` 声明式定义，以便统一管理和验证插件配置。

#### 验收标准

1. WHEN 解析 `plugin.yaml` 文件时，THE DiscoveryManager SHALL 提取 name、version、description、type、author、tags、platforms 字段
2. WHEN `plugin.yaml` 的 name 字段为空时，THE DiscoveryManager SHALL 跳过该插件并记录 WARN 日志
3. WHEN `plugin.yaml` 的 type 字段不是已知的 PluginType 时，THE DiscoveryManager SHALL 跳过该插件并记录 WARN 日志
4. WHEN `plugin.yaml` 格式错误（非法 YAML）时，THE DiscoveryManager SHALL 跳过该插件并记录 WARN 日志
5. THE PluginManifest 解析 SHALL 支持 mcp、local_mcp、nlskill 类型的特有配置段
6. WHEN `plugin.yaml` 包含 settings 字段时，THE PluginConfig SHALL 将其作为 Settings map 传递给插件的 Init 方法

### 需求 3：三层插件发现

**用户故事：** 作为用户，我希望插件可以从用户级、项目级和包级三个层级被发现，以便灵活管理不同范围的插件。

#### 验收标准

1. THE DiscoveryManager SHALL 扫描 `~/.maclaw/plugins/` 目录发现用户级插件
2. THE DiscoveryManager SHALL 扫描 `.maclaw/plugins/` 目录发现项目级插件
3. THE DiscoveryManager SHALL 通过 EntryPointProvider 接口收集包级插件
4. WHEN 不同层级存在同名插件时，THE DiscoveryManager SHALL 按优先级 project > user > package 选择高优先级的插件
5. WHEN 插件目录不存在时，THE DiscoveryManager SHALL 返回空列表且不报错
6. THE DiscoveryManager 的 DiscoverAll 方法 SHALL 返回 Name 唯一的 manifest 列表

### 需求 4：插件生命周期管理

**用户故事：** 作为系统运维人员，我希望插件有统一的生命周期管理（初始化、启动、停止），以便可靠地管理插件状态。

#### 验收标准

1. WHEN 加载插件时，THE PluginRegistry SHALL 按 Init → Start 的顺序调用插件方法
2. WHEN 插件的 Init 方法返回错误时，THE PluginRegistry SHALL 将该插件状态设为 "error" 并跳过 Start
3. WHEN 插件的 Start 方法返回错误时，THE PluginRegistry SHALL 将该插件状态设为 "error"
4. WHEN 插件成功启动时，THE PluginRegistry SHALL 将该插件状态设为 "running"
5. WHEN 单个插件启动失败时，THE PluginRegistry SHALL 继续加载其余插件而不中断
6. WHEN 注销插件时，THE PluginRegistry SHALL 调用该插件的 Stop 方法
7. IF 插件的 Stop 方法在 10 秒内未返回，THEN THE PluginRegistry SHALL 强制取消 context 并记录 ERROR 日志

### 需求 5：工具注册与调用

**用户故事：** 作为 MaClaw Agent，我希望插件提供的工具自动注册到现有的 Tool Registry，以便通过统一的工具调用路径使用所有插件工具。

#### 验收标准

1. WHEN 插件成功启动时，THE PluginRegistry SHALL 将该插件的所有 ToolDefinition 注册到现有的 Tool_Registry
2. WHEN 注销插件时，THE PluginRegistry SHALL 从 Tool_Registry 移除该插件注册的所有工具
3. WHEN Agent 调用插件工具时，THE Tool_Registry SHALL 通过 ToolDefinition 的 Handler 将调用转发到对应的 PluginAdapter
4. WHILE 插件状态为 "running"，THE Tool_Registry SHALL 包含该插件的所有工具定义

### 需求 6：Adapter 模式桥接

**用户故事：** 作为开发者，我希望现有的 MCP Server、NLSkill 代码保持不变，通过 Adapter 模式桥接到统一 Plugin 接口，以便实现零破坏性迁移。

#### 验收标准

1. THE MCPPluginAdapter SHALL 将远程 MCP Server 封装为 Plugin 接口实现
2. THE LocalMCPPluginAdapter SHALL 将本地 stdio MCP Server 封装为 Plugin 接口实现
3. THE NLSkillPluginAdapter SHALL 将 NLSkill 封装为 Plugin 接口实现
4. WHEN MCPPluginAdapter 启动时，THE MCPPluginAdapter SHALL 从远程 MCP Server 获取工具列表并转换为 ToolDefinition
5. IF 远程 MCP Server 不可达，THEN THE MCPPluginAdapter SHALL 返回空工具列表且 Health 状态为 "unhealthy"
6. THE PluginRegistry SHALL 根据 PluginManifest 的 Type 字段自动选择对应的 Adapter 创建插件实例

### 需求 7：插件健康检查

**用户故事：** 作为系统运维人员，我希望能够监控插件的健康状态，以便及时发现和处理插件故障。

#### 验收标准

1. THE Plugin 接口 SHALL 通过 Health() 方法返回当前健康状态（healthy、degraded、unhealthy）
2. WHEN 远程 MCP Server 连接恢复时，THE MCPPluginAdapter SHALL 自动将 Health 状态从 "unhealthy" 更新为 "healthy"
3. THE PluginRegistry SHALL 通过后台 goroutine 定期执行健康检查，间隔为 60 秒
4. WHEN 查询插件列表时，THE PluginRegistry SHALL 返回每个插件的当前健康状态

### 需求 8：插件注册唯一性

**用户故事：** 作为系统管理员，我希望 PluginRegistry 中不存在同名插件，以便避免工具名称冲突和管理混乱。

#### 验收标准

1. WHEN 注册同名插件时，THE PluginRegistry SHALL 返回错误并拒绝注册
2. THE PluginRegistry 中的所有已注册插件 SHALL 具有唯一的 Name
3. WHEN DiscoverAll 返回的 manifest 列表中存在同名项时，THE DiscoveryManager SHALL 仅保留最高优先级的一个

### 需求 9：安全性

**用户故事：** 作为安全工程师，我希望插件系统遵循安全最佳实践，以便防止敏感信息泄露和未授权操作。

#### 验收标准

1. WHEN `plugin.yaml` 中的 auth_secret 字段使用 `${ENV_VAR}` 语法时，THE PluginConfig 构建器 SHALL 从环境变量中解析实际值
2. WHEN 首次加载项目级插件（`.maclaw/plugins/`）时，THE PluginRegistry SHALL 提示用户确认信任
3. THE 原生 Go 插件 SHALL 仅支持编译时注册（通过 EntryPointProvider），不支持运行时动态加载 `.so` 文件
4. THE 插件执行的 bash 命令 SHALL 受现有安全策略（`corelib/security`）约束

### 需求 10：CLI 插件管理

**用户故事：** 作为用户，我希望通过 CLI 命令管理插件（列表、启用、禁用、查看详情），以便方便地操作插件。

#### 验收标准

1. WHEN 用户执行 `maclaw-tui plugin list` 时，THE CLI SHALL 显示所有已注册插件的名称、类型、状态和工具数量
2. WHEN 用户执行 `maclaw-tui plugin list --json` 时，THE CLI SHALL 以 JSON 格式输出插件列表
3. WHEN 用户执行 `maclaw-tui plugin info <name>` 时，THE CLI SHALL 显示指定插件的详细信息
4. WHEN 用户执行 `maclaw-tui plugin enable <name>` 时，THE CLI SHALL 启动指定插件并将状态设为 "running"
5. WHEN 用户执行 `maclaw-tui plugin disable <name>` 时，THE CLI SHALL 停止指定插件并将状态设为 "stopped"

### 需求 11：plugin.yaml 序列化与反序列化

**用户故事：** 作为插件开发者，我希望 `plugin.yaml` 的解析和生成是可靠的，以便插件配置的读写不会丢失数据。

#### 验收标准

1. WHEN 解析有效的 `plugin.yaml` 文件时，THE 解析器 SHALL 返回完整的 PluginManifest 结构体
2. WHEN 将 PluginManifest 序列化为 YAML 时，THE 序列化器 SHALL 生成有效的 `plugin.yaml` 格式
3. FOR ALL 有效的 PluginManifest 对象，序列化后再反序列化 SHALL 产生等价的对象（往返一致性）
