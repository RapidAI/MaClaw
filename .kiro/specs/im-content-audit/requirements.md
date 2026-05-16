# 需求文档：IM 内容审核

## 简介

在奇安信专用版（OEM `qianxin`）上，为 Hub 的 IM 出站通道添加内容审核能力。所有由 MaClaw Agent 产生、即将发送到 IM 接收者的文本、图片、文件内容，在投递前经过管理员配置的外部审核程序检查。审核程序通过标准输入输出（stdin/stdout）接收内容并返回审核结果，Hub 根据返回码决定放行、拦截或延迟投递，并始终向 IM 接收者发送反馈消息，确保接收者不会感知到消息丢失。

## 术语表

- **Hub**：MaClaw 服务端，负责 IM 消息路由、设备管理和安全策略
- **Content_Auditor**：Hub 中负责调用外部审核程序并处理返回结果的模块
- **Audit_Program**：管理员配置的外部可执行程序，通过 stdin/stdout 协议与 Hub 交互，执行内容合规检查
- **Outbound_Interceptor**：Hub 中已有的 IM 出站拦截器（`hub/internal/im/outbound_interceptor.go`），负责在消息投递前执行安全检查
- **IM_Adapter**：Hub 中的 IM 适配层（`hub/internal/im/core.go`），负责消息路由和投递
- **GenericResponse**：Hub 中 IM 消息的统一响应模型
- **Audit_Log**：审核日志记录，包含每次审核的输入摘要、返回码、时间戳等信息
- **OEM_Brand**：编译时品牌标识，通过 `corelib/brand` 包确定当前构建的品牌变体

## 需求

### 需求 1：审核程序路径配置

**用户故事：** 作为 Hub 管理员，我希望在设置中配置 IM 内容审核程序的路径，以便启用出站内容审核功能。

#### 验收标准

1. THE Hub Config SHALL 包含 `content_audit` 配置段，含 `program_path` 字段（字符串类型）和 `timeout_seconds` 字段（整数类型，默认 30）
2. THE Hub Config SHALL 包含 `content_audit.timeout_policy` 字段（字符串类型，取值 `"block"` 或 `"pass"`，默认 `"block"`），用于控制审核程序超时时的默认策略
3. WHEN `program_path` 为空字符串时，THE Content_Auditor SHALL 跳过审核流程，直接放行所有内容
4. WHEN `program_path` 指向不存在或不可执行的文件时，THE Content_Auditor SHALL 在启动时记录警告日志，并在每次审核调用时返回程序错误（等同返回码 -1）
5. WHERE 当前构建品牌为 `qianxin`，THE Hub SHALL 默认启用多机模式（无开关），审核配置在管理后台的"IM 内容审核"设置项中展示

### 需求 2：审核程序 stdin/stdout 协议

**用户故事：** 作为审核程序开发者，我希望通过标准化的 stdin/stdout 协议与 Hub 交互，以便实现内容合规检查逻辑。

#### 验收标准

1. WHEN Content_Auditor 调用 Audit_Program 时，THE Content_Auditor SHALL 将审核请求以 JSON 格式写入 Audit_Program 的 stdin，JSON 包含 `type`（`"text"` / `"image"` / `"file"`）、`content`（文本内容或 base64 编码数据）、`user_id`（发送者标识）、`platform`（IM 平台名称）四个字段
2. THE Content_Auditor SHALL 从 Audit_Program 的 stdout 读取 JSON 响应，响应包含 `code`（整数返回码）和 `message`（可选的附加说明）两个字段
3. THE Content_Auditor SHALL 在 `timeout_seconds` 配置的时间内等待 Audit_Program 响应，超时后终止 Audit_Program 进程
4. WHEN Audit_Program 的 stdout 输出不是合法 JSON 时，THE Content_Auditor SHALL 将该次审核视为返回码 -1（程序错误）

### 需求 3：返回码处理逻辑

**用户故事：** 作为 Hub 管理员，我希望审核程序的返回码能驱动不同的消息投递行为，以便实现灵活的内容管控策略。

#### 验收标准

1. WHEN Audit_Program 返回码为 0 时，THE Content_Auditor SHALL 放行原始内容，IM_Adapter 正常投递给接收者
2. WHEN Audit_Program 返回码为 1 时，THE Content_Auditor SHALL 先向接收者发送提示消息"内容正在审核中，请稍候"，待审核通过后补发实际内容
3. WHEN Audit_Program 返回码为 2 时，THE Content_Auditor SHALL 拦截原始内容，向接收者发送提示消息"内容不符合数据安全规则，已被拦截"
4. WHEN Audit_Program 返回码为 3 时，THE Content_Auditor SHALL 拦截原始内容，向接收者发送提示消息"内容包含非法信息，已被拦截"
5. WHEN Audit_Program 返回码为 -1 时，THE Content_Auditor SHALL 根据 `timeout_policy` 配置决定放行或拦截，并记录错误日志
6. WHEN Audit_Program 返回码为 4 时，THE Content_Auditor SHALL 拦截原始内容，向接收者发送提示消息"内容需要人工审核，请等待管理员审批"，并将审核请求标记为待人工处理
7. WHEN Audit_Program 返回码为 5 时，THE Content_Auditor SHALL 从 Audit_Program 的 stdout 响应中读取 `sanitized_content` 字段，用脱敏后的内容替换原始内容后投递给接收者
8. IF Audit_Program 返回未定义的返回码，THEN THE Content_Auditor SHALL 将该次审核视为返回码 -1（程序错误）处理

### 需求 4：延迟审核流程

**用户故事：** 作为 IM 接收者，我希望在内容延迟审核期间收到明确的等待提示，审核通过后能收到实际内容，以便我不会觉得消息丢失。

#### 验收标准

1. WHEN 返回码为 1（延迟发送）时，THE Content_Auditor SHALL 立即向接收者投递占位提示消息
2. THE Content_Auditor SHALL 启动后台轮询，以可配置的间隔（默认 5 秒）重新调用 Audit_Program 检查同一内容
3. WHEN 后台轮询中 Audit_Program 返回码变为 0 时，THE Content_Auditor SHALL 将原始内容投递给接收者
4. WHEN 后台轮询中 Audit_Program 返回码变为 2 或 3 时，THE Content_Auditor SHALL 向接收者发送对应的拦截提示消息
5. IF 后台轮询超过 10 次仍返回码为 1，THEN THE Content_Auditor SHALL 停止轮询，向接收者发送"审核超时，内容未通过"提示消息，并记录超时日志

### 需求 5：审核日志

**用户故事：** 作为合规审计人员，我希望每次内容审核的结果都被记录，以便进行合规审计和问题追溯。

#### 验收标准

1. THE Content_Auditor SHALL 为每次审核调用记录一条 Audit_Log，包含：时间戳、用户标识、IM 平台名称、内容类型、内容摘要（文本前 200 字符或文件名）、返回码、审核耗时、Audit_Program 返回的 message 字段
2. WHEN 审核结果为拦截（返回码 2、3、4）时，THE Content_Auditor SHALL 在 Audit_Log 中额外记录完整的原始内容哈希值（SHA-256）
3. THE Audit_Log SHALL 持久化存储到 Hub 的数据库中
4. IF 写入 Audit_Log 失败，THEN THE Content_Auditor SHALL 记录错误日志，但审核流程的放行/拦截决策不受影响

### 需求 6：与现有出站拦截器集成

**用户故事：** 作为 Hub 开发者，我希望内容审核与现有的 OutboundInterceptor 安全检查协同工作，以便两层安全机制互不干扰。

#### 验收标准

1. THE Content_Auditor SHALL 在 Outbound_Interceptor 的安全策略检查之后执行，即先检查文件/图片外发权限，通过后再进行内容审核
2. WHEN Outbound_Interceptor 已拦截消息时，THE Content_Auditor SHALL 不被调用，避免不必要的审核程序执行
3. THE Content_Auditor SHALL 通过与 Outbound_Interceptor 相同的集成点（`sendResponse` 方法）接入 IM_Adapter 的消息投递流程
4. WHILE Content_Auditor 的 `program_path` 配置为空时，THE IM_Adapter SHALL 保持与当前完全一致的行为，无任何性能或功能影响

### 需求 7：超时与错误处理

**用户故事：** 作为 Hub 管理员，我希望审核程序异常时有明确的降级策略，以便系统不会因审核程序故障而完全阻塞 IM 通信。

#### 验收标准

1. WHEN Audit_Program 执行超时时，THE Content_Auditor SHALL 终止 Audit_Program 进程，并根据 `timeout_policy` 配置执行放行（`"pass"`）或拦截（`"block"`）
2. WHEN Audit_Program 进程启动失败时，THE Content_Auditor SHALL 将该次审核视为返回码 -1 处理
3. WHEN Audit_Program 进程以非零退出码退出且 stdout 无有效 JSON 输出时，THE Content_Auditor SHALL 将该次审核视为返回码 -1 处理
4. THE Content_Auditor SHALL 对并发审核请求进行限流，同时运行的 Audit_Program 进程数量上限为 10

### 需求 8：默认审核程序（内置 Audit_Program）

**用户故事：** 作为 Hub 部署者，我希望 Hub 自带一个可用的默认审核程序，以便开箱即用地启用内容审核功能，无需额外开发或部署外部程序。

#### 验收标准

1. THE Hub SHALL 内置一个默认审核程序（Default_Audit_Program），作为 `hub/cmd/audit_program/` 下的独立 Go 可执行文件，随 Hub 一起编译部署
2. THE Default_Audit_Program SHALL 完整实现需求 2 定义的 stdin/stdout JSON 协议，支持 `text`、`image`、`file` 三种内容类型的审核
3. WHEN 审核类型为 `text` 时，THE Default_Audit_Program SHALL 基于可配置的关键字列表进行文本匹配检查，命中任一关键字则返回码 2（拦截），否则返回码 0（放行）
4. WHEN 审核类型为 `image` 时，THE Default_Audit_Program SHALL 直接返回码 0（放行），不做实际图片内容分析
5. WHEN 审核类型为 `file` 时，THE Default_Audit_Program SHALL 直接返回码 0（放行），不做实际文件内容分析
6. THE Default_Audit_Program SHALL 从 stdin 的 JSON 请求中读取 `keywords` 字段（字符串数组）作为关键字列表；若 `keywords` 字段缺失或为空，则从命令行参数 `--keywords-file` 指定的文件路径加载关键字（每行一个关键字）
7. THE Default_Audit_Program SHALL 在返回的 JSON 响应 `message` 字段中包含命中的关键字信息（如 `"命中关键字: xxx"`），便于审核日志记录
8. THE Hub 的 Content_Auditor SHALL 在调用 Default_Audit_Program 时，将管理后台配置的关键字列表通过 stdin JSON 的 `keywords` 字段传递给审核程序

### 需求 9：管理后台内容审核配置

**用户故事：** 作为 Hub 管理员，我希望在管理后台的 Web 界面中配置内容审核的关键字列表和审核参数，以便无需修改配置文件即可动态调整审核策略。

#### 验收标准

1. THE Hub 管理后台（`hub/web/admin/index.html`）SHALL 在 IM 插件 tab 内新增"内容审核"子 tab（Content Audit），展示审核配置界面
2. THE 内容审核配置界面 SHALL 包含以下可编辑字段：审核程序路径（`program_path`）、超时时间（`timeout_seconds`）、超时策略（`timeout_policy`）、关键字列表（多行文本框，每行一个关键字）
3. WHEN 管理员保存配置时，THE 管理后台 SHALL 通过 API 将配置持久化到 Hub 的 SystemSettings 存储中（key: `content_audit_config`）
4. WHEN 管理员加载配置页面时，THE 管理后台 SHALL 从 API 读取已保存的配置并回填到表单中
5. THE Hub SHALL 提供 `GET /api/admin/content_audit/config` 和 `PUT /api/admin/content_audit/config` 两个 API 端点，均需管理员认证（RequireAdmin）
6. THE 关键字列表 SHALL 以 JSON 字符串数组格式存储，管理后台在展示时转换为多行文本，保存时转换回数组格式
