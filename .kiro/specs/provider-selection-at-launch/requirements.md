# 需求文档：编程会话启动时服务商选择

## 简介

当前 Maclaw 在启动远程编程会话时，始终使用桌面端配置中各工具的 `CurrentModel`（当前选中的服务商），无法在启动时动态选择其他服务商。本需求为 IM Agent 端和 PWA/移动端的会话启动流程增加服务商选择能力，同时过滤掉未配置的无效服务商，确保只能选择 "Original"（原厂）或已配置好 API Key 的服务商。

## 术语表

- **Provider（服务商）**: 编程工具的模型服务提供方，对应 `ModelConfig` 结构体中的一条记录。`ModelConfig.ModelName` 是服务商标识（如 "Original"、"DeepSeek"、"百度千帆"），服务商内部包含具体的模型 ID（`ModelId`）、API 地址（`ModelUrl`）、API 密钥（`ApiKey`）等配置。一个编程工具可配置多个服务商。
- **有效服务商**: `ModelName` 为 "Original"（原厂模式，使用工具自带认证，不需要 API Key）或 `ApiKey` 非空的 `ModelConfig`
- **无效/空服务商**: `ModelName` 不是 "Original" 且 `ApiKey` 为空的 `ModelConfig`，即用户添加了服务商条目但尚未填写配置
- **Maclaw_Agent**: 运行在桌面客户端上的 AI Agent，通过 LLM 驱动工具调用，处理 IM 消息（对应 `IMMessageHandler`）
- **PWA**: 移动端/浏览器端的渐进式 Web 应用，通过 Hub 中继与桌面端通信
- **ToolConfig**: 工具配置结构，包含 `CurrentModel`（当前选中服务商名称）和 `Models`（所有服务商列表）
- **LaunchSpec**: 会话启动规格，包含工具名、项目路径、模型配置、环境变量等

## 需求

### 需求 1：有效服务商统一判定

**用户故事：** 作为开发者，我希望系统能统一判定哪些服务商是有效可用的，以便在所有入口点（Agent/IM、PWA、桌面端）看到一致的可选服务商列表。

#### 验收标准


1. THE system SHALL 判定一个 ModelConfig 为"有效服务商"当且仅当满足以下任一条件：（a）`ModelName` 的小写形式为 "original"（原厂模式），或（b）`ApiKey` 字段非空
2. THE system SHALL 判定一个 ModelConfig 为"无效服务商"当 `ModelName` 的小写形式不是 "original" 且 `ApiKey` 字段为空
3. FOR ALL 涉及服务商列举和选择的功能点，THE system SHALL 使用此统一判定逻辑过滤服务商列表

### 需求 2：IM Agent 端创建会话时选择服务商

**用户故事：** 作为开发者，我希望通过 IM 发起编程会话时能指定使用哪个服务商，以便无需在桌面端切换即可使用不同的模型服务。

#### 验收标准

1. THE `create_session` 工具 SHALL 增加可选参数 `provider`（类型为 string），用于指定启动会话时使用的服务商名称（对应 `ModelConfig.ModelName`，如 "Original"、"DeepSeek"、"百度千帆"）
2. WHEN `provider` 参数为空或未提供时，THE system SHALL 使用桌面端当前选中的服务商（`ToolConfig.CurrentModel`）作为默认值
3. WHEN `provider` 参数指定了一个有效服务商时，THE system SHALL 使用该服务商的完整配置（ModelId、ModelUrl、ApiKey 等）构建 LaunchSpec，覆盖默认的 `CurrentModel`
4. IF `provider` 参数指定了一个无效服务商（未配置 ApiKey 且非 Original），THEN THE system SHALL 返回错误信息，说明该服务商未配置，并列出当前可用的有效服务商
5. IF `provider` 参数指定了一个不存在的服务商名称，THEN THE system SHALL 返回错误信息，说明该服务商不存在

### 需求 3：PWA/移动端启动会话时选择服务商

**用户故事：** 作为开发者，我希望通过 PWA 或移动端启动编程会话时能选择服务商，以便在移动场景下灵活使用不同的模型服务。

#### 验收标准

1. THE `RemoteStartSessionRequest` 结构体 SHALL 增加 `Provider` 字段（类型为 `string`，json tag 为 `"provider,omitempty"`），用于指定启动会话时使用的服务商名称（对应 `ModelConfig.ModelName`，即服务商标识如 "Original"、"DeepSeek"、"百度千帆"，而非具体模型 ID）
2. WHEN `Provider` 字段为空时，THE `StartRemoteSessionForProject` 函数 SHALL 使用桌面端当前选中的服务商（`ToolConfig.CurrentModel`）作为默认值
3. WHEN `Provider` 字段指定了一个有效服务商时，THE `StartRemoteSessionForProject` 函数 SHALL 将该服务商名称传递给 `buildRemoteLaunchSpec`，覆盖默认的 `CurrentModel`
4. IF `Provider` 字段指定了一个无效服务商，THEN THE `StartRemoteSessionForProject` 函数 SHALL 返回错误，说明该服务商未配置

### 需求 4：核心构建函数支持服务商覆盖

**用户故事：** 作为开发者，我希望 `buildRemoteLaunchSpec` 函数能接受外部传入的服务商覆盖，以便所有入口点（Agent、PWA、桌面端）都能统一使用此机制选择服务商。

#### 验收标准

1. THE `buildRemoteLaunchSpec` 函数 SHALL 增加 `providerOverride` 参数（类型为 string），当非空时用于替代 `toolCfg.CurrentModel` 进行服务商查找
2. WHEN `providerOverride` 为空时，THE 函数 SHALL 保持现有行为，使用 `toolCfg.CurrentModel`
3. WHEN `providerOverride` 非空时，THE 函数 SHALL 在 `toolCfg.Models` 中查找 `ModelName` 匹配的 ModelConfig，并使用该服务商的完整配置（ModelId、ModelUrl、ApiKey、WireApi 等）
4. WHEN `providerOverride` 指定的服务商在 `toolCfg.Models` 中找到但为无效服务商时，THE 函数 SHALL 返回错误 `"provider %s is not configured (missing API key)"`
5. WHEN `providerOverride` 指定的服务商在 `toolCfg.Models` 中未找到时，THE 函数 SHALL 返回错误 `"provider %s not found for tool %s"`

### 需求 5：列出可用服务商工具

**用户故事：** 作为开发者，我希望能通过 IM 查询某个编程工具有哪些可用的服务商，以便在创建会话前了解可选项。

#### 验收标准

1. THE Maclaw_Agent SHALL 拥有 `list_providers` 工具，接受必填参数 `tool`（工具名称，如 "claude"、"codex"），返回该工具的所有有效服务商列表
2. THE `list_providers` 工具 SHALL 对每个有效服务商返回以下信息：服务商名称（ModelName）、模型 ID（ModelId，脱敏）、是否为当前默认（与 `CurrentModel` 比较）
3. THE `list_providers` 工具 SHALL 过滤掉所有无效服务商（非 Original 且 ApiKey 为空的），不在返回结果中展示
4. IF 指定工具没有任何有效服务商，THEN THE `list_providers` 工具 SHALL 返回提示信息，建议用户在桌面端配置服务商

### 需求 6：PWA 前端服务商选择界面

**用户故事：** 作为开发者，我希望在 PWA 启动会话的界面中能看到一个服务商下拉选择器，以便直观地选择要使用的服务商。

#### 验收标准

1. THE PWA 启动会话界面 SHALL 在工具选择之后展示一个服务商下拉选择器，列出所选工具的所有有效服务商
2. THE 服务商下拉选择器 SHALL 默认选中桌面端当前使用的服务商（`ToolConfig.CurrentModel`）
3. THE 服务商下拉选择器 SHALL 仅展示有效服务商（Original 或 ApiKey 非空的），不展示无效/空服务商
4. WHEN 用户选择一个服务商并点击启动时，THE PWA SHALL 将选中的服务商名称通过 `RemoteStartSessionRequest.Provider` 字段传递给后端
5. WHEN 用户切换工具选择时，THE 服务商下拉选择器 SHALL 刷新为新工具的有效服务商列表，并默认选中该工具的当前服务商

### 需求 7：启动时服务商有效性校验

**用户故事：** 作为开发者，我希望系统在实际启动会话前校验所选服务商的有效性，以便在启动前就发现配置问题而非启动后才失败。

#### 验收标准

1. WHEN `buildRemoteLaunchSpec` 选定了一个 ModelConfig 后，THE 函数 SHALL 校验该 ModelConfig 是否为有效服务商
2. IF 选定的 ModelConfig 为无效服务商（非 Original 且 ApiKey 为空），THEN THE 函数 SHALL 返回错误 `"provider %s has no API key configured"`，阻止会话启动
3. THE 校验逻辑 SHALL 同时适用于默认选择（CurrentModel）和覆盖选择（providerOverride）两种场景，确保任何路径都不会使用无效服务商启动会话

## 正确性属性

### P1: 有效服务商判定一致性
FOR ALL ModelConfig m: isValidProvider(m) ⟺ (lowercase(m.ModelName) == "original" ∨ m.ApiKey ≠ "")

### P2: 默认值保持
FOR ALL 启动请求 r WHERE r.provider 为空: 实际使用的服务商 == ToolConfig.CurrentModel

### P3: 覆盖生效
FOR ALL 启动请求 r WHERE r.provider 非空且为有效服务商: 实际使用的服务商 == r.provider

### P4: 无效服务商不可启动
FOR ALL 启动请求 r: 如果最终选定的 ModelConfig 为无效服务商，则启动必须失败并返回错误

### P5: 列表过滤完整性
FOR ALL list_providers 返回的服务商 p: isValidProvider(p) == true（列表中不包含任何无效服务商）
