# 需求文档：快速启动会话 (Quick Launch Session)

## 简介

通过飞书/QQ 等 IM 渠道启动编程会话时，实现"快速启动"体验。核心目标：用户无需手动指定服务商和项目即可一键启动会话，系统自动选择默认服务商并在失败时自动降级到其他可用服务商，同时支持用户手动指定服务商和两种项目选择模式（预设项目 / 指定目录）。

## 术语表

- **Quick_Launch_Engine**: 快速启动引擎，负责协调服务商选择、降级回退和项目解析的核心模块
- **Provider_Resolver**: 服务商解析器，负责确定目标服务商并在失败时执行降级回退逻辑
- **Provider**: 服务商，即 LLM 模型提供方（如 Original、DeepSeek、百度千帆等），对应 `ModelConfig`
- **Default_Provider**: 默认服务商，工具配置中 `CurrentModel` 指向的服务商
- **Fallback_Chain**: 降级链，按优先级排列的可用服务商列表，用于默认服务商失败时的自动回退
- **Project_Selector**: 项目选择器，负责解析用户的项目选择意图（预设项目或指定目录）
- **Preset_Project**: 预设项目，已在桌面端配置的项目列表中的项目
- **Session_Precheck**: 会话预检模块，在启动前检查工具、项目路径和模型配置的可用性
- **IM_Channel**: 即时通讯渠道，如飞书或 QQ，用户通过该渠道发起会话创建请求
- **ToolConfig**: 工具配置，包含某个编程工具的所有服务商模型列表和当前选中的模型

## 需求

### 需求 1：默认服务商优先启动

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望系统自动使用默认服务商启动会话，这样我不需要每次手动选择服务商。

#### 验收标准

1. WHEN 用户通过 IM_Channel 发起创建会话请求且未指定 provider 参数, THE Provider_Resolver SHALL 从 ToolConfig 中读取 Default_Provider 作为目标服务商
2. WHEN Default_Provider 已配置且通过 API Key 验证, THE Quick_Launch_Engine SHALL 使用该 Default_Provider 启动会话
3. WHEN 会话使用非用户显式指定的服务商启动成功, THE Quick_Launch_Engine SHALL 在 IM_Channel 返回的消息中包含实际使用的服务商名称
4. THE Provider_Resolver SHALL 在 200 毫秒内完成服务商解析（不含网络请求）

### 需求 2：服务商自动降级回退

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望当默认服务商不可用时系统自动尝试其他已配置的服务商，这样我不会因为单个服务商故障而无法工作。

#### 验收标准

1. WHEN Default_Provider 的 API Key 未配置或验证失败, THE Provider_Resolver SHALL 构建 Fallback_Chain 并按顺序尝试下一个可用服务商
2. THE Provider_Resolver SHALL 按照 ToolConfig 中 Models 列表的顺序构建 Fallback_Chain，仅包含通过 `isValidProvider` 检查的服务商
3. WHEN Provider_Resolver 通过降级找到可用服务商, THE Quick_Launch_Engine SHALL 使用该服务商启动会话，并在 IM_Channel 返回的消息中注明"已降级到 [服务商名称]"
4. IF Fallback_Chain 中所有服务商均不可用, THEN THE Quick_Launch_Engine SHALL 返回错误消息，列出所有已尝试的服务商及各自的失败原因
5. WHEN 降级发生, THE Quick_Launch_Engine SHALL 记录降级事件到日志，包含原始服务商名称、降级目标服务商名称和降级原因

### 需求 3：用户手动指定服务商

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望能手动指定使用哪个服务商，这样我可以根据任务需要选择合适的模型。

#### 验收标准

1. WHEN 用户在创建会话请求中指定了 provider 参数, THE Provider_Resolver SHALL 直接使用该指定服务商，跳过默认服务商和降级逻辑
2. WHEN 用户指定的服务商名称在 ToolConfig 的 Models 列表中不存在, THE Provider_Resolver SHALL 返回错误消息，包含该服务商名称和当前可用服务商列表
3. WHEN 用户指定的服务商存在但 API Key 未配置, THE Provider_Resolver SHALL 返回错误消息，提示用户为该服务商配置 API Key
4. WHEN 用户手动指定服务商且启动成功, THE Quick_Launch_Engine SHALL 在 IM_Channel 返回的消息中确认使用了用户指定的服务商

### 需求 4：预设项目选择模式（模式 A）

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望能从已配置的项目列表中选择项目启动会话，这样我可以快速切换不同项目。

#### 验收标准

1. WHEN 用户请求列出可用项目, THE Project_Selector SHALL 返回桌面端已配置的所有 Preset_Project 列表，包含项目名称和路径
2. WHEN 用户通过项目 ID 或项目名称选择 Preset_Project, THE Project_Selector SHALL 解析到对应的项目配置并用于会话启动
3. WHEN 用户未指定项目且未指定目录, THE Project_Selector SHALL 按以下优先级自动选择项目：当前桌面端打开的项目 → 最近使用的项目 → 项目列表中的第一个项目
4. IF 用户指定的项目 ID 或名称在 Preset_Project 列表中不存在, THEN THE Project_Selector SHALL 返回错误消息并附带可用项目列表

### 需求 5：指定目录启动模式（模式 B）

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望能通过指定目录路径启动会话，这样我可以在任意项目目录上工作。

#### 验收标准

1. WHEN 用户在创建会话请求中指定了 project_path 参数, THE Project_Selector SHALL 使用该路径作为项目目录
2. WHEN 指定的 project_path 对应一个已配置的 Preset_Project 路径, THE Project_Selector SHALL 使用该 Preset_Project 的完整配置（包括 UseProxy、YoloMode 等设置）
3. WHEN 指定的 project_path 不对应任何 Preset_Project, THE Project_Selector SHALL 使用默认项目配置并以该路径作为项目目录
4. IF 指定的 project_path 在文件系统中不存在, THEN THE Session_Precheck SHALL 在预检阶段报告项目路径不可访问的错误

### 需求 6：启动反馈与状态通知

**用户故事：** 作为一个通过 IM 启动编程会话的用户，我希望在启动过程中收到清晰的状态反馈，这样我知道会话是否成功创建以及使用了什么配置。

#### 验收标准

1. WHEN 会话启动成功, THE Quick_Launch_Engine SHALL 在 IM_Channel 返回包含以下信息的消息：会话 ID、使用的工具名称、使用的服务商名称、项目路径
2. WHEN 会话启动过程中发生服务商降级, THE Quick_Launch_Engine SHALL 在成功消息中额外包含降级说明（原始服务商 → 实际服务商）
3. WHEN 会话启动失败, THE Quick_Launch_Engine SHALL 返回包含失败原因的错误消息，并提供可操作的修复建议
4. WHILE 会话启动过程中, THE Quick_Launch_Engine SHALL 在 3 秒内完成预检并返回预检结果给用户

### 需求 7：服务商解析的幂等性与确定性

**用户故事：** 作为一个系统维护者，我希望服务商解析逻辑是确定性的，这样相同的输入总是产生相同的结果，便于调试和测试。

#### 验收标准

1. THE Provider_Resolver SHALL 对相同的输入参数（tool、provider、ToolConfig）产生相同的服务商选择结果
2. THE Provider_Resolver SHALL 保证 Fallback_Chain 的顺序与 ToolConfig.Models 列表顺序一致
3. FOR ALL 有效的 ToolConfig 输入, 对 Provider_Resolver 执行解析后再次执行解析 SHALL 产生相同的结果（幂等性）
4. THE Provider_Resolver SHALL 对服务商名称进行大小写不敏感的匹配
