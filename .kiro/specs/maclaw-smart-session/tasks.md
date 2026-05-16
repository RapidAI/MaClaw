# 实施计划：Maclaw 智能会话启动与接入

## 概述

基于需求文档和设计文档，将 Maclaw 平台的四个能力升级方向拆分为增量实施任务。每个任务在前一个任务基础上构建，最终通过集成任务将所有组件串联。实施语言为 Go，遵循现有代码库的架构风格（Wails 桌面端 + Hub 后端）。

## 任务

- [x] 1. 长期记忆子系统 — MemoryStore 核心
  - [x] 1.1 创建 `memory_store.go`，实现 MemoryStore
    - 定义 `MemoryCategory`（user_fact/preference/project_knowledge/instruction/conversation_summary）
    - 定义 `MemoryEntry` 结构体（id/content/category/tags/created_at/updated_at/access_count）
    - 实现 `NewMemoryStore(path string)` 构造函数，从 `~/.maclaw/memories.json` 加载已有记忆
    - 实现 `Save(entry MemoryEntry) error`，去重逻辑：内容相同则更新 updated_at 和 access_count
    - 实现 `Delete(id string) error`
    - 实现 `List(category MemoryCategory, keyword string) []MemoryEntry`
    - 实现 `evictLRU()`：超过 500 条时淘汰 access_count 最低且最旧的条目
    - 使用 `sync.RWMutex` 保护并发访问
    - _需求: 17.1, 17.2, 17.3_

  - [x] 1.2 实现 MemoryStore 持久化机制
    - 实现 `persistLoop()` 后台 goroutine：收到写入信号后 debounce 5 秒再写磁盘
    - 实现 `load()` 从磁盘加载、`flush()` 写入磁盘
    - 实现 `Stop()` 优雅关闭（flush 后退出）
    - _需求: 17.4_

  - [x] 1.3 实现 MemoryStore 检索与召回
    - 实现 `Search(category, keyword, limit)` 按类别和关键词检索
    - 实现 `Recall(userMessage string) []MemoryEntry`：始终包含所有 user_fact，其余按 tags/关键词相关性排序，最多 20 条，总 token ≤ 2000
    - 实现 `TouchAccess(ids []string)` 更新访问计数
    - _需求: 19.2, 19.3, 19.4_

  - [ ]* 1.4 为 MemoryStore 编写单元测试
    - 测试 Save/Delete/List/Search/Recall
    - 测试去重逻辑和 LRU 淘汰
    - 测试持久化 load/flush round-trip
    - _需求: 17.1-17.4, 19.2-19.4_

- [x] 2. 长期记忆子系统 — Agent 工具集成
  - [x] 2.1 在 `im_message_handler.go` 中添加记忆工具定义
    - 添加 `save_memory` 工具：参数 content/category/tags
    - 添加 `list_memories` 工具：参数 category(可选)/keyword(可选)
    - 添加 `delete_memory` 工具：参数 id
    - _需求: 18.1, 20.1, 20.2_

  - [x] 2.2 在 `im_message_handler.go` 的 `executeTool()` 中实现记忆工具执行
    - `save_memory` → 调用 MemoryStore.Save
    - `list_memories` → 调用 MemoryStore.List
    - `delete_memory` → 调用 MemoryStore.Delete
    - _需求: 18.2, 20.3, 20.4_

  - [x] 2.3 修改 `buildSystemPrompt()`，注入长期记忆
    - 在系统提示词中添加 "## 用户记忆" 区域
    - 调用 MemoryStore.Recall(userMessage) 获取相关记忆
    - 注入最多 20 条记忆，总 token ≤ 2000
    - 在系统提示词中添加指引：识别到用户偏好/事实时主动调用 save_memory
    - _需求: 19.1, 19.3, 18.3_

  - [ ]* 2.4 为记忆工具编写集成测试
    - 测试通过 executeTool 调用 save_memory/list_memories/delete_memory
    - 测试系统提示词中记忆注入
    - _需求: 18.1-18.4, 19.1, 20.1-20.4_

- [x] 3. 长期记忆子系统 — 对话摘要归档
  - [x] 3.1 创建 `conversation_archiver.go`，实现 ConversationArchiver
    - 实现 `Archive(userID string, entries []conversationEntry) error`
    - 调用 LLM 对对话历史生成摘要，提取用户偏好、决策结论和重要事实
    - 将摘要存为 category="conversation_summary" 的 MemoryEntry
    - 简单问答对话跳过归档
    - LLM 未配置时跳过
    - _需求: 21.1, 21.2, 21.3, 21.4_

  - [x] 3.2 修改 `conversationMemory.evictExpired()`，在清除前触发归档
    - 在删除过期对话前调用 ConversationArchiver.Archive
    - 归档失败不阻塞清除流程
    - _需求: 21.1_

  - [ ]* 3.3 为 ConversationArchiver 编写单元测试
    - 测试摘要提取和记忆存储
    - 测试简单对话跳过逻辑
    - _需求: 21.1-21.4_

- [x] 4. 检查点 — 长期记忆子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 5. 智能会话启动子系统 — 上下文推断与预检
  - [x] 5.1 创建 `session_context_resolver.go`，实现 SessionContextResolver
    - 实现 `ResolveProject() (string, string)`：按优先级推断项目路径（当前打开 → 最近使用 → 默认项目）
    - 实现 `ResolveTool(projectPath, taskDescription string) (string, string)`：根据项目语言/框架推荐工具
    - 无法推断时返回空字符串，由调用方展示项目列表
    - _需求: 1.1, 1.2, 1.3, 2.1, 2.2_

  - [x] 5.2 创建 `session_precheck.go`，实现 SessionPrecheck
    - 实现 `Check(toolName, projectPath string) PrecheckResult`
    - 检查项：工具二进制存在且可执行、项目路径存在、模型配置已设置
    - 3 秒超时，超时项标记为 "未知"
    - 返回安装指引和配置提示
    - _需求: 9.1, 9.2, 9.3, 9.4, 9.5_

  - [ ]* 5.3 为 SessionContextResolver 和 SessionPrecheck 编写单元测试
    - 测试项目推断优先级
    - 测试工具推荐逻辑
    - 测试预检各种场景（工具缺失、项目不存在、模型未配置）
    - _需求: 1.1-1.4, 9.1-9.5_

- [x] 6. 智能会话启动子系统 — 模板与启动反馈
  - [x] 6.1 创建 `session_template.go`，实现 SessionTemplateManager
    - 定义 `SessionTemplate` 结构体
    - 实现 Create/Get/List/Delete CRUD 操作
    - 持久化到 `~/.maclaw/templates.json`
    - _需求: 3.1, 3.3_

  - [x] 6.2 创建 `session_startup_feedback.go`，实现 SessionStartupFeedback
    - 实现 `WatchStartup(sessionID string, callback ProgressCallback)`
    - 每 3 秒推送启动进度（"正在初始化工具"/"正在加载项目"/"等待工具就绪"）
    - 状态变为 running 时发送成功通知
    - 60 秒超时发送警告
    - _需求: 4.1, 4.2, 4.3, 4.4_

  - [x] 6.3 实现 SessionTemplate 序列化与反序列化
    - JSON round-trip 正确性
    - 缺少必填字段（名称或工具名）时返回错误
    - _需求: 11.1, 11.2, 11.3, 11.4_

  - [ ]* 6.4 为 SessionTemplate 编写属性测试
    - **属性 1: Round-trip 一致性 — 任意有效 SessionTemplate 序列化后反序列化应与原始对象等价**
    - **验证: 需求 11.3**

  - [ ]* 6.5 为 SessionTemplateManager 和 SessionStartupFeedback 编写单元测试
    - 测试模板 CRUD 和持久化
    - 测试启动反馈超时逻辑
    - _需求: 3.1-3.5, 4.1-4.4_

- [x] 7. 智能会话启动子系统 — Agent 工具集成
  - [x] 7.1 在 `im_message_handler.go` 中添加会话启动增强工具
    - 添加 `create_template` 工具：参数 name/tool/project_path/model_config/yolo_mode
    - 添加 `list_templates` 工具：无参数
    - 添加 `launch_template` 工具：参数 template_name
    - _需求: 3.2, 3.4_

  - [x] 7.2 修改 `toolCreateSession` 增强逻辑
    - tool 参数为空时调用 SessionContextResolver.ResolveTool 推荐
    - project_path 参数为空时调用 SessionContextResolver.ResolveProject 推断
    - 创建前调用 SessionPrecheck.Check 执行预检
    - 创建后调用 SessionStartupFeedback.WatchStartup 监控启动
    - _需求: 1.1-1.4, 2.1-2.4, 4.1-4.4, 9.1-9.5_

  - [x] 7.3 实现 IM 端会话恢复逻辑
    - 在 Agent 系统提示词中添加会话恢复指引
    - 用户发送"继续"/"恢复"时列出可恢复会话
    - 恢复后自动进入交互模式
    - _需求: 5.1, 5.2, 5.3, 5.4_

  - [x] 7.4 实现自然语言启动解析
    - 在 Agent 系统提示词中添加自然语言启动指引
    - Agent 从用户消息中提取工具名、项目标识、任务描述
    - 缺失参数时联动需求 1（项目推断）和需求 2（工具推荐）
    - 创建前向用户确认参数
    - _需求: 10.1, 10.2, 10.3, 10.4, 10.5_

- [x] 8. 检查点 — 智能会话启动子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 9. 自然语言配置管理子系统 — ConfigManager 核心
  - [x] 9.1 创建 `config_manager.go`，实现 ConfigManager
    - 定义 `ConfigSection`、`ConfigKeySchema`、`ConfigChange`、`ImportReport` 类型
    - 实现 `NewConfigManager(app *App)` 构造函数，初始化配置 schema
    - 实现 `GetConfig(section string) (string, error)`：读取配置并脱敏
    - 实现 `UpdateConfig(section, key, value string) (string, error)`：修改配置并持久化
    - 实现 `BatchUpdate(changes []ConfigChange) error`：原子性批量修改
    - 实现 `GetSchema() []ConfigSection`：返回配置 schema
    - 实现 `maskSensitive(value string) string`：API Key/Token 脱敏
    - _需求: 12.1-12.4, 13.1-13.5, 14.1-14.4, 15.1-15.3_

  - [x] 9.2 实现配置 Schema 定义
    - 覆盖所有配置区域：工具模型（claude/gemini/codex/opencode/iflow/kilo/cursor）、项目管理、远程设置、代理设置、Maclaw LLM、MCP Server、通用设置
    - 每个 key 定义类型、描述、默认值、合法取值
    - _需求: 14.1, 14.2, 14.4_

  - [x] 9.3 实现配置导出与导入
    - 实现 `ExportConfig() (string, error)`：序列化并脱敏
    - 实现 `ImportConfig(jsonData string) (*ImportReport, error)`：校验、差异预览、保留本机字段
    - _需求: 16.1, 16.2, 16.3, 16.4_

  - [ ]* 9.4 为 ConfigManager 编写单元测试
    - 测试 GetConfig/UpdateConfig/BatchUpdate
    - 测试脱敏逻辑
    - 测试非法值校验
    - 测试导出导入 round-trip
    - _需求: 12.1-12.4, 13.1-13.5, 16.1-16.4_

- [x] 10. 自然语言配置管理子系统 — Agent 工具集成
  - [x] 10.1 在 `im_message_handler.go` 中添加配置管理工具定义
    - 添加 `get_config` 工具：参数 section
    - 添加 `update_config` 工具：参数 section/key/value
    - 添加 `batch_update_config` 工具：参数 changes（JSON 数组）
    - 添加 `list_config_schema` 工具：无参数
    - 添加 `export_config` 工具：无参数
    - 添加 `import_config` 工具：参数 json_data
    - _需求: 12.1, 13.2, 14.3, 15.1, 16.1, 16.2_

  - [x] 10.2 在 `executeTool()` 中实现配置工具执行
    - 各工具调用 ConfigManager 对应方法
    - update_config 执行前返回变更预览，需用户确认
    - _需求: 13.3, 13.4, 13.5_

  - [ ]* 10.3 为配置工具编写集成测试
    - 测试通过 executeTool 调用各配置工具
    - 测试 OnConfigChanged 回调触发
    - _需求: 12.1-16.4_

- [x] 11. 检查点 — 自然语言配置管理子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 12. 新接入方式子系统 — CLI 客户端
  - [x] 12.1 创建 `cmd/maclaw-cli/main.go`，实现 CLI 客户端框架
    - 使用 cobra 或 flag 包实现子命令：session list/start/attach/kill
    - 实现 Hub WebSocket 连接和认证
    - _需求: 6.1, 6.4_

  - [x] 12.2 实现 `session start` 子命令
    - 接受 --tool、--project、--template 参数
    - 通过 Hub API 创建会话
    - _需求: 6.2_

  - [x] 12.3 实现 `session attach` 子命令
    - 建立 WebSocket 实时连接
    - 终端显示会话输出，接受用户输入
    - Ctrl+C 断开连接（不终止会话）
    - _需求: 6.3_

  - [ ]* 12.4 为 CLI 客户端编写集成测试
    - 测试连接、列出会话、创建会话
    - _需求: 6.1-6.5_

- [x] 13. 新接入方式子系统 — Webhook 端点
  - [x] 13.1 创建 `hub/internal/httpapi/webhook_session_handlers.go`
    - 实现 POST /api/webhook/session 端点
    - 接受 tool/project_path/prompt/callback_url 参数
    - Bearer Token 认证
    - 创建会话并发送初始指令，返回 session_id
    - _需求: 7.1, 7.2, 7.4_

  - [x] 13.2 实现 Webhook 回调机制
    - 会话完成时向 callback_url 发送 POST 请求
    - 包含 session_id、status、summary
    - _需求: 7.3_

  - [x] 13.3 在 Hub 路由中注册 Webhook 端点
    - 在 `hub/internal/httpapi/router.go` 中添加路由
    - 添加认证中间件
    - _需求: 7.4, 7.5_

  - [ ]* 13.4 为 Webhook 端点编写单元测试
    - 测试认证、创建会话、回调
    - 测试无效参数错误处理
    - _需求: 7.1-7.5_

- [x] 14. 新接入方式子系统 — 多设备会话漫游
  - [x] 14.1 扩展 `RemoteHubClient`，实现会话元数据同步
    - 在心跳中附带活跃会话元数据（ID/工具/项目/状态）
    - Hub 端存储并广播给同一用户的其他设备
    - _需求: 8.1_

  - [x] 14.2 实现会话 IO 中继
    - Hub 端为远程设备建立到目标会话的 WebSocket 中继
    - 输出广播到所有连接设备
    - 输入接受最近发送者（last-writer-wins）
    - _需求: 8.2, 8.3_

  - [x] 14.3 实现设备断开处理
    - 设备断开时保持会话运行
    - 其他设备不受影响
    - _需求: 8.4_

  - [ ]* 14.4 为会话漫游编写集成测试
    - 测试多设备同时接入
    - 测试设备断开后会话继续
    - _需求: 8.1-8.4_

- [x] 15. 检查点 — 新接入方式子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 16. 全局集成 — 将所有子系统接入 App 和 IMMessageHandler
  - [x] 16.1 修改 `app.go`，在 App 结构体中添加新组件字段并初始化
    - 添加 `memoryStore *MemoryStore` 字段
    - 添加 `configManager *ConfigManager` 字段
    - 添加 `templateManager *SessionTemplateManager` 字段
    - 添加 `contextResolver *SessionContextResolver` 字段
    - 添加 `sessionPrecheck *SessionPrecheck` 字段
    - 添加 `conversationArchiver *ConversationArchiver` 字段
    - 在 `startup()` 中初始化所有组件
    - 在 `shutdown()` 中调用 MemoryStore.Stop() 确保记忆持久化
    - _需求: 全部_

  - [x] 16.2 修改 `im_message_handler.go`，集成所有新工具
    - 在 `buildToolDefinitions()` 中添加所有新工具定义（记忆/配置/模板）
    - 在 `executeTool()` 中添加所有新工具的执行分支
    - 修改 `buildSystemPrompt()` 注入长期记忆和配置管理指引
    - _需求: 全部_

  - [x] 16.3 添加 Wails 绑定函数
    - 添加 `ListMemories`、`DeleteMemory` 的 Wails 绑定（供桌面端 UI 使用）
    - 添加 `ListTemplates`、`CreateTemplate`、`DeleteTemplate` 的 Wails 绑定
    - 添加 `GetConfigSchema`、`UpdateConfig` 的 Wails 绑定
    - _需求: 20.1, 3.1, 14.3_

  - [ ]* 16.4 为全局集成编写集成测试
    - 测试完整消息处理流程：用户消息 → 记忆召回 → 工具路由 → 工具执行 → 记忆存储 → 结果返回
    - _需求: 全部_

- [x] 17. 最终检查点 — 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户确认。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保需求可追溯
- 检查点任务确保增量验证，及时发现问题
- 属性测试验证核心正确性属性，单元测试覆盖边界情况
- 任务优先级：长期记忆（1-4）→ 智能启动（5-8）→ 配置管理（9-11）→ 新接入方式（12-15）→ 集成（16-17）
- 长期记忆排在最前面，因为它是截图中用户反馈的核心痛点（"记住我叫马二"但下次就忘了）
- 所有新文件遵循现有代码库的 Go 惯用法：`sync.RWMutex` 并发保护、`interface` 多态、`context.Context` 超时控制
