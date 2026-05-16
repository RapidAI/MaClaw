# 实现计划：MaClaw Gossip 集成

## 概述

按照设计文档，分步实现 MaClaw 端 Gossip 功能：先建立 Go 后端 GossipClient 和数据类型，再添加 Wails 绑定，然后改造前端 GossipPanel，接着实现 TUI 子命令，最后实现自动发布触发器。每一步都在前一步基础上递增构建，确保无孤立代码。

## Tasks

- [x] 1. 实现 GossipClient 和数据类型
  - [x] 1.1 在 `gui/gossip_client.go` 中定义数据结构和 GossipClient
    - 定义 GossipPost、GossipComment、GossipPublishResult、GossipBrowseResult、GossipCommentResult、GossipCommentsResult、GossipSnapshotResult 结构体
    - 实现 NewGossipClient(app *App) 构造函数
    - 实现 baseURL() 方法：从 config.RemoteHubCenterURL 读取，为空时回退到 defaultRemoteHubCenterURL
    - 实现 machineID() 和 userEmail() 辅助方法，从配置读取 RemoteMachineID 和 RemoteEmail
    - 实现 PublishPost(ctx, content, category)：POST /api/gossip/publish，body 包含 machine_id、user_email、content、category
    - 实现 BrowsePosts(ctx, page)：GET /api/gossip/browse?page=N
    - 实现 AddComment(ctx, postID, content, rating)：POST /api/gossip/comment
    - 实现 RatePost(ctx, postID, rating)：POST /api/gossip/rate
    - 实现 GetComments(ctx, postID, page)：GET /api/gossip/comments?post_id=X&page=N
    - 实现 GetSnapshot(ctx, etag)：GET /api/gossip/snapshot，支持 If-None-Match 头和 304 处理
    - HTTP 超时统一 30 秒；错误返回包含状态码和错误消息
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 1.11_

  - [ ]* 1.2 编写 GossipClient 属性测试（gui/gossip_client_test.go）
    - **Property 1: GossipClient 请求构造正确性**
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9**

  - [ ]* 1.3 编写 HTTP 错误传播属性测试
    - **Property 2: HTTP 错误传播**
    - **Validates: Requirements 1.10**

- [x] 2. Checkpoint - 确保 GossipClient 编译通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 3. 实现 Wails 绑定和配置扩展
  - [x] 3.1 在 `corelib/app_config.go` 的 AppConfig 中新增 `GossipAutoPublish bool` 字段
    - JSON tag: `json:"gossip_auto_publish,omitempty"`，默认 false
    - _Requirements: 6.3_

  - [x] 3.2 在 `gui/app_gossip.go` 中实现 Wails 绑定方法
    - 实现 App.GossipPublish(content, category string) (*GossipPublishResult, error)
    - 实现 App.GossipBrowse(page int) (*GossipBrowseResult, error)
    - 实现 App.GossipComment(postID, content string, rating int) (*GossipCommentResult, error)
    - 实现 App.GossipRate(postID string, rating int) error
    - 实现 App.GossipGetComments(postID string, page int) (*GossipCommentsResult, error)
    - 实现 App.GossipSnapshot(etag string) (*GossipSnapshotResult, error)
    - 每个方法创建 30 秒超时 context，调用 GossipClient 对应方法
    - 在 App 初始化时创建 GossipClient 实例
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

- [x] 4. 改造 GossipPanel 前端组件
  - [x] 4.1 将 GossipPanel 从直连 HubCenter 切换为 Wails 绑定调用
    - 移除 hubUrl prop 依赖，改为通过 window.go.main.App.GossipSnapshot 获取数据
    - 修改 fetchSnapshot 使用 Wails 绑定，处理 changed==false 时跳过更新
    - 保留 30 秒轮询机制，通过 Wails 绑定轮询
    - 保留搜索、排序（最新/最热/评分）和分页功能
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x] 4.2 新增发布表单 UI
    - 在工具栏区域添加"发布"按钮
    - 点击展开发布表单：textarea 内容输入框 + category 选择器（owner/project/news）
    - 字符计数提示，限制 1-2000 字符
    - 内容为空或超限时禁用提交按钮
    - 提交调用 GossipPublish Wails 绑定
    - 成功后清空表单、收起发布区域、刷新列表
    - 失败时在表单区域显示错误提示
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7_

  - [x] 4.3 新增评论和评分 UI
    - 每条帖子下方显示"评论"按钮和评分星标（1-5 星）
    - 点击"评论"展开评论区域：已有评论列表 + 评论输入框（1-1000 字符）
    - 提交评论调用 GossipComment Wails 绑定
    - 点击评分星标调用 GossipRate Wails 绑定
    - locked 帖子隐藏评论输入框和评分星标，显示"已锁定"提示
    - 成功后刷新评论列表和评分显示
    - 失败时在对应区域显示错误提示
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [ ]* 4.4 编写前端属性测试（GossipPanel.test.tsx）
    - **Property 3: 内容长度验证**（使用 fast-check）
    - **Validates: Requirements 2.3, 2.4, 3.3**

  - [ ]* 4.5 编写前端搜索排序属性测试
    - **Property 11: 搜索排序过滤正确性**（使用 fast-check）
    - **Validates: Requirements 7.3**

- [x] 5. Checkpoint - 确保 GUI 端编译通过且前端可构建
  - 确保所有测试通过，如有问题请询问用户。

- [x] 6. 实现 TUI gossip 子命令
  - [x] 6.1 在 `tui/commands/gossip.go` 中实现 gossip 子命令
    - 实现 RunGossip(args) 入口分发函数
    - 实现 gossipBrowse：gossip browse [--page N] [--json]，调用 GET /api/gossip/browse
    - 实现 gossipPublish：gossip publish --content "..." --category owner|project|news，调用 POST /api/gossip/publish
    - 实现 gossipComment：gossip comment --post-id ID --content "..." [--rating 0-5]，调用 POST /api/gossip/comment
    - 实现 gossipRate：gossip rate --post-id ID --rating 1-5，调用 POST /api/gossip/rate
    - 实现 gossipComments：gossip comments --post-id ID [--page N] [--json]，调用 GET /api/gossip/comments
    - 缺少必需参数时返回 UsageError
    - 支持 --json 标志输出有效 JSON
    - 错误消息使用中文
    - 从配置读取 hubcenter URL、machine_id、email（复用 resolveHubCenterURL 等辅助函数）
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8_

  - [x] 6.2 在 `tui/main.go` 中注册 gossip 命令
    - 在 main() switch 中添加 "gossip" case，调用 commands.RunGossip
    - 在 printUsage() 中添加 gossip 命令说明
    - _Requirements: 5.1_

  - [ ]* 6.3 编写 TUI gossip 属性测试（tui/commands/gossip_test.go）
    - **Property 5: TUI 缺少必需参数时返回用法提示**
    - **Validates: Requirements 5.6**

  - [ ]* 6.4 编写 TUI --json 输出属性测试
    - **Property 6: TUI --json 输出有效 JSON**
    - **Validates: Requirements 5.7**

- [x] 7. Checkpoint - 确保 TUI 编译通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 8. 实现 AutoPublishTrigger 自动发布触发器
  - [x] 8.1 在 `gui/gossip_auto_publish.go` 中实现 AutoPublishTrigger
    - 实现 sanitizeContent 函数：正则移除文件路径（/xxx/xxx、C:\xxx）、邮箱（xxx@xxx）、IP 地址
    - 实现 AutoPublishTrigger 结构体：包含 GossipClient、lastPublish 时间、enabled 函数
    - 实现 NewAutoPublishTrigger(client, enabledFn) 构造函数
    - 实现 OnSkillUploaded(skillName, description)：生成 category="news" 的帖子
    - 实现 OnSessionCompleted(sessionSummary, durationMin)：仅当 durationMin > 5 时生成 category="project" 的帖子
    - 实现 tryPublish(content, category)：检查 enabled、冷却间隔（10 分钟）、调用 GossipClient.PublishPost、失败只记录日志
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

  - [x] 8.2 在 GUI App 初始化中集成 AutoPublishTrigger
    - 创建 AutoPublishTrigger 实例，enabledFn 从 AppConfig.GossipAutoPublish 读取
    - 在 Skill 上传成功和远程会话完成的回调点调用 OnSkillUploaded / OnSessionCompleted
    - _Requirements: 6.1, 6.2_

  - [ ]* 8.3 编写 sanitizeContent 属性测试
    - **Property 7: 内容脱敏移除敏感信息**
    - **Validates: Requirements 6.5**

  - [ ]* 8.4 编写自动发布分类/内容属性测试
    - **Property 8: 自动发布生成正确的分类和内容**
    - **Validates: Requirements 6.1, 6.2**

  - [ ]* 8.5 编写自动发布禁用配置属性测试
    - **Property 9: 自动发布尊重禁用配置**
    - **Validates: Requirements 6.3, 6.4**

  - [ ]* 8.6 编写自动发布冷却间隔属性测试
    - **Property 10: 自动发布冷却间隔**
    - **Validates: Requirements 6.6**

- [x] 9. Final Checkpoint - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

## Notes

- 标记 `*` 的任务为可选测试任务，可跳过以加速 MVP
- 每个任务引用了具体的需求编号，确保可追溯
- Checkpoint 任务确保增量验证
- 属性测试验证通用正确性属性（Go 端使用 pgregory.net/rapid，前端使用 fast-check）
- 单元测试验证具体示例和边界情况
