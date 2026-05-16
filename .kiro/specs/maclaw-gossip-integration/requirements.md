# 需求文档：MaClaw Gossip 集成

## 简介

在 MaClaw 端（GUI 和 TUI）接入 HubCenter 的 Gossip（八卦）功能，打通 MaClaw 到 HubCenter Gossip API 的完整链路。当前 GUI 前端已有只读浏览的 GossipPanel 组件（直接调用 HubCenter），但缺少发布、评论、评分功能，且 Go 后端没有任何 Gossip 相关代码。本需求旨在：

1. 在 MaClaw Go 后端新增 GossipClient，封装与 HubCenter Gossip API 的通信
2. 扩展 GUI 前端 GossipPanel，支持发布帖子、评论和评分
3. 在 TUI 端新增 gossip 子命令，提供完整的 CLI 交互能力
4. 设计 Gossip 内容的产生和上传触发机制

## 术语表

- **MaClaw**：本地桌面客户端应用，包含 GUI（Wails 桌面应用）和 TUI（终端命令行）两种界面
- **HubCenter**：远程中心服务器，提供 Gossip API 等公共服务
- **GossipClient**：MaClaw Go 后端中与 HubCenter Gossip API 通信的 HTTP 客户端模块
- **GossipPanel**：GUI 前端中展示 Gossip 内容的 React 组件
- **GossipPost**：一条八卦帖子，包含 id、machine_id、nickname、content、category、score、votes 等字段
- **GossipComment**：一条评论，包含 id、post_id、machine_id、nickname、content、rating 等字段
- **Category**：帖子分类，取值为 "owner"（吐槽老板）、"project"（项目八卦）、"news"（业界新闻）
- **machine_id**：MaClaw 实例的唯一标识，用于匿名发布
- **Nickname**：由 machine_id 哈希生成的匿名昵称，格式为 "MaClaw-xxxx"
- **ETag 缓存**：HTTP 条件请求机制，用于减少 Gossip 快照的重复传输
- **AutoPublishTrigger**：自动发布触发器，基于特定事件自动生成并发布 Gossip 内容

## 需求

### 需求 1：GossipClient Go 后端客户端

**用户故事：** 作为 MaClaw 开发者，我希望有一个统一的 Go 后端 Gossip 客户端，以便 GUI 和 TUI 都能通过 Go 后端与 HubCenter Gossip API 交互。

#### 验收标准

1. THE GossipClient SHALL 使用 config.RemoteHubCenterURL 作为 HubCenter 基础地址
2. WHEN RemoteHubCenterURL 为空时，THE GossipClient SHALL 回退到 DefaultRemoteHubCenterURL（"http://hubs.mypapers.top:9388"）
3. THE GossipClient SHALL 提供 PublishPost 方法，接受 content（字符串）和 category（"owner"|"project"|"news"）参数，调用 POST /api/gossip/publish
4. THE GossipClient SHALL 提供 BrowsePosts 方法，接受 page（整数）参数，调用 GET /api/gossip/browse 并返回帖子列表
5. THE GossipClient SHALL 提供 AddComment 方法，接受 post_id、content、rating（0-5）参数，调用 POST /api/gossip/comment
6. THE GossipClient SHALL 提供 RatePost 方法，接受 post_id、rating（1-5）参数，调用 POST /api/gossip/rate
7. THE GossipClient SHALL 提供 GetComments 方法，接受 post_id、page 参数，调用 GET /api/gossip/comments
8. THE GossipClient SHALL 提供 GetSnapshot 方法，支持 ETag 条件请求，调用 GET /api/gossip/snapshot
9. THE GossipClient SHALL 自动从配置中读取 machine_id（RemoteMachineID）和 user_email（RemoteEmail），调用方无需手动传入
10. WHEN HubCenter 返回 HTTP 错误状态码时，THE GossipClient SHALL 返回包含状态码和错误消息的 error
11. THE GossipClient SHALL 为所有 HTTP 请求设置 30 秒超时

### 需求 2：GUI 前端发布功能

**用户故事：** 作为 MaClaw GUI 用户，我希望能在 GossipPanel 中发布八卦帖子，以便分享我的想法和见闻。

#### 验收标准

1. THE GossipPanel SHALL 在工具栏区域显示一个"发布"按钮
2. WHEN 用户点击"发布"按钮时，THE GossipPanel SHALL 展开一个发布表单，包含内容输入框（textarea）和分类选择器（owner/project/news）
3. THE GossipPanel SHALL 将内容长度限制在 1 到 2000 个字符之间
4. WHEN 内容为空或超过 2000 字符时，THE GossipPanel SHALL 禁用提交按钮并显示字符计数提示
5. WHEN 用户提交发布表单时，THE GossipPanel SHALL 通过 Go 后端 GossipClient 调用 HubCenter 发布 API
6. WHEN 发布成功时，THE GossipPanel SHALL 清空表单、收起发布区域并刷新帖子列表
7. IF 发布请求失败，THEN THE GossipPanel SHALL 在表单区域显示错误提示信息

### 需求 3：GUI 前端评论和评分功能

**用户故事：** 作为 MaClaw GUI 用户，我希望能对八卦帖子进行评论和评分，以便参与社区互动。

#### 验收标准

1. THE GossipPanel SHALL 在每条帖子下方显示"评论"按钮和评分星标（1-5 星）
2. WHEN 用户点击"评论"按钮时，THE GossipPanel SHALL 展开该帖子的评论区域，显示已有评论列表和评论输入框
3. THE GossipPanel SHALL 将评论内容长度限制在 1 到 1000 个字符之间
4. WHEN 用户提交评论时，THE GossipPanel SHALL 通过 Go 后端 GossipClient 调用 HubCenter 评论 API
5. WHEN 用户点击评分星标时，THE GossipPanel SHALL 通过 Go 后端 GossipClient 调用 HubCenter 评分 API
6. WHEN 帖子处于 locked 状态时，THE GossipPanel SHALL 隐藏评论输入框和评分星标，并显示"已锁定"提示
7. WHEN 评论或评分成功时，THE GossipPanel SHALL 刷新该帖子的评论列表和评分显示
8. IF 评论或评分请求失败，THEN THE GossipPanel SHALL 在对应区域显示错误提示信息

### 需求 4：GUI 后端 Gossip API 绑定

**用户故事：** 作为 MaClaw 开发者，我希望 GUI 前端能通过 Wails 绑定调用 Go 后端的 GossipClient，以便前端不再直接调用 HubCenter。

#### 验收标准

1. THE MaClaw_GUI SHALL 暴露 GossipPublish Wails 绑定方法，接受 content 和 category 参数
2. THE MaClaw_GUI SHALL 暴露 GossipBrowse Wails 绑定方法，接受 page 参数
3. THE MaClaw_GUI SHALL 暴露 GossipComment Wails 绑定方法，接受 post_id、content、rating 参数
4. THE MaClaw_GUI SHALL 暴露 GossipRate Wails 绑定方法，接受 post_id、rating 参数
5. THE MaClaw_GUI SHALL 暴露 GossipGetComments Wails 绑定方法，接受 post_id、page 参数
6. THE MaClaw_GUI SHALL 暴露 GossipSnapshot Wails 绑定方法，支持 ETag 缓存
7. WHEN 前端调用 Gossip 绑定方法时，THE MaClaw_GUI SHALL 将请求转发给 GossipClient 并返回结果

### 需求 5：TUI Gossip 子命令

**用户故事：** 作为 MaClaw TUI 用户，我希望通过命令行浏览、发布、评论和评分八卦帖子，以便在终端环境中也能参与社区互动。

#### 验收标准

1. THE MaClaw_TUI SHALL 提供 `gossip browse` 子命令，显示帖子列表（支持 --page 和 --json 参数）
2. THE MaClaw_TUI SHALL 提供 `gossip publish` 子命令，接受 --content 和 --category 参数发布帖子
3. THE MaClaw_TUI SHALL 提供 `gossip comment` 子命令，接受 --post-id、--content、--rating 参数提交评论
4. THE MaClaw_TUI SHALL 提供 `gossip rate` 子命令，接受 --post-id、--rating 参数提交评分
5. THE MaClaw_TUI SHALL 提供 `gossip comments` 子命令，接受 --post-id 和 --page 参数查看评论列表
6. WHEN 未提供必需参数时，THE MaClaw_TUI SHALL 输出用法提示信息
7. THE MaClaw_TUI SHALL 支持 --json 标志，以 JSON 格式输出所有命令结果
8. WHEN HubCenter 请求失败时，THE MaClaw_TUI SHALL 输出包含错误原因的中文提示信息

### 需求 6：Gossip 自动发布触发机制

**用户故事：** 作为 MaClaw 用户，我希望系统能在特定事件发生时自动生成并发布八卦内容，以便社区能自动获取有价值的动态信息。

#### 验收标准

1. WHEN 用户成功上传一个 Skill 到 SkillMarket 时，THE AutoPublishTrigger SHALL 自动生成一条 category 为 "news" 的 Gossip 帖子，内容包含 Skill 名称和简要描述
2. WHEN 用户完成一次远程编码会话（session）且会话时长超过 5 分钟时，THE AutoPublishTrigger SHALL 自动生成一条 category 为 "project" 的 Gossip 帖子，内容包含会话摘要
3. THE AutoPublishTrigger SHALL 提供 enable/disable 配置项（gossip_auto_publish），默认为 false（关闭）
4. WHILE gossip_auto_publish 配置为 false 时，THE AutoPublishTrigger SHALL 跳过所有自动发布逻辑
5. THE AutoPublishTrigger SHALL 对自动生成的内容进行脱敏处理，移除文件路径、邮箱地址和 IP 地址等敏感信息
6. THE AutoPublishTrigger SHALL 在两次自动发布之间保持至少 10 分钟的冷却间隔，防止刷屏
7. IF 自动发布请求失败，THEN THE AutoPublishTrigger SHALL 记录错误日志但不中断主流程

### 需求 7：GUI 前端从直连切换到后端中转

**用户故事：** 作为 MaClaw 开发者，我希望 GUI 前端的 Gossip 数据获取从直连 HubCenter 切换为通过 Go 后端中转，以便统一管理 machine_id 注入和错误处理。

#### 验收标准

1. THE GossipPanel SHALL 通过 Wails 绑定方法（而非直接 fetch）获取 Gossip 快照数据
2. THE GossipPanel SHALL 保留 30 秒轮询刷新机制，通过 Wails 绑定方法轮询
3. THE GossipPanel SHALL 保留现有的搜索、排序（最新/最热/评分）和分页功能
4. WHEN Go 后端返回 ETag 未变化标记时，THE GossipPanel SHALL 跳过数据更新（等效于 HTTP 304）
5. THE GossipPanel SHALL 移除对 hubUrl prop 的直接依赖，改为通过 Go 后端获取数据
