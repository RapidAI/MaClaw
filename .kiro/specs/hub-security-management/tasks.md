# 实现计划：Hub 安全管理

## 概述

基于设计文档中定义的组件和接口，将 Hub 安全管理功能分解为增量实现步骤。从数据层（SQLite 表 + Store）开始，逐步构建 SecurityService 业务逻辑、REST API、IM 外发拦截、心跳策略注入、Admin UI 安全管理 Tab，最后在客户端（GUI/TUI）侧实现策略接收与执行。使用 Go 语言实现，属性测试使用 gopter 库。

## 任务

- [x] 1. 定义数据模型和核心类型
  - [x] 1.1 创建 `hub/internal/security/types.go`，定义所有数据结构
    - 定义 SecurityGroup、GroupTreeNode、EffectivePolicy、DefaultPolicy 常量
    - 定义 GroupPolicyView、PolicyItemView、SecuritySettings、HeartbeatSecurityPayload
    - 定义 SecurityPolicyProvider 接口（供 OutboundInterceptor 和 ws.Gateway 使用）
    - _需求: 2.1, 2.4, 2.5, 3.2, 4.1_

- [x] 2. 实现 SecurityStore 数据访问层
  - [x] 2.1 创建 `hub/internal/security/store.go`，实现 SecurityStore
    - 实现 SQLite 表创建（security_groups、security_group_members、security_policies）
    - 实现 InitRootGroup：系统初始化时自动创建根节点（名称"全局"）
    - 实现 CreateGroup / GetGroupByID / ListGroups / UpdateGroupName / DeleteGroup
    - 实现 GetRootGroup / GetGroupDepth（计算树深度）
    - 实现 AssignUser / RemoveUser / GetUserGroup / ListGroupMembers / CountGroupMembers / MoveUsersToRoot
    - 实现 GetGroupPolicy / SetGroupPolicy（稀疏 JSON 存储）
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.8, 2.2, 2.3, 3.5_

  - [ ]* 2.2 编写属性测试：策略稀疏存储
    - **Property 7: 策略稀疏存储**
    - **验证: 需求 2.3**

- [x] 3. 实现 SecurityService 业务逻辑层
  - [x] 3.1 创建 `hub/internal/security/service.go`，实现 SecurityService
    - 实现 CreateGroup：校验父组存在、树深度不超过 10 层
    - 实现 RenameGroup / DeleteGroup（级联将用户移回 Root_Group）
    - 实现 GetGroupTree：构建完整树形结构（含成员数量）
    - 实现 AssignUser / RemoveUser：用户单组归属，分配到新组时自动从旧组移除
    - 实现 GetGroupPolicy / UpdateGroupPolicy
    - 实现 GetEffectivePolicy：从 Root_Group 沿路径逐级合并策略
    - 实现 GetGroupEffectivePolicy：预览指定组的合并后策略
    - 实现 GetSettings / UpdateSettings（含审计日志记录）
    - 实现 SetDefaultGroup：校验目标组存在
    - 实现 InvalidateCache / InvalidateCacheForSubtree：缓存失效
    - 实现 GetHeartbeatPolicy：根据集中管控开关返回心跳策略数据
    - 实现 IsCentralizedEnabled / GetEffectivePolicyByUserID（供 OutboundInterceptor 使用）
    - _需求: 1.1-1.12, 2.1-2.6, 3.1-3.5, 4.1-4.8_

  - [ ]* 3.2 编写属性测试：创建子组扩展树
    - **Property 1: 创建子组扩展树**
    - **验证: 需求 1.2**

  - [ ]* 3.3 编写属性测试：重命名组的往返一致性
    - **Property 2: 重命名组的往返一致性**
    - **验证: 需求 1.3**

  - [ ]* 3.4 编写属性测试：删除组从树中移除
    - **Property 3: 删除组从树中移除**
    - **验证: 需求 1.4**

  - [ ]* 3.5 编写属性测试：级联删除将用户移回根组
    - **Property 4: 级联删除将用户移回根组**
    - **验证: 需求 1.5**

  - [ ]* 3.6 编写属性测试：用户单组归属不变量
    - **Property 5: 用户单组归属不变量**
    - **验证: 需求 1.6, 1.7**

  - [ ]* 3.7 编写属性测试：策略继承合并
    - **Property 8: 策略继承合并**
    - **验证: 需求 3.1, 3.3, 3.4**

  - [ ]* 3.8 编写属性测试：策略视图标注来源
    - **Property 9: 策略视图标注来源**
    - **验证: 需求 2.6**

- [x] 4. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。


- [x] 5. 实现安全管理 REST API
  - [x] 5.1 创建 `hub/internal/httpapi/security_handler.go`，实现所有安全管理 API Handler
    - 实现 SecurityGroupsHandler（GET /api/admin/security/groups）：返回完整用户组树
    - 实现 CreateSecurityGroupHandler（POST /api/admin/security/groups）：创建子组
    - 实现 UpdateSecurityGroupHandler（PUT /api/admin/security/groups/{id}）：重命名
    - 实现 DeleteSecurityGroupHandler（DELETE /api/admin/security/groups/{id}）：删除组
    - 实现 AddGroupMemberHandler（POST /api/admin/security/groups/{id}/members）：分配用户
    - 实现 RemoveGroupMemberHandler（DELETE /api/admin/security/groups/{id}/members/{email}）：移除用户
    - 实现 GetGroupPolicyHandler（GET /api/admin/security/groups/{id}/policy）：查看组策略
    - 实现 UpdateGroupPolicyHandler（PUT /api/admin/security/groups/{id}/policy）：更新组策略
    - 实现 GetUserEffectivePolicyHandler（GET /api/admin/security/users/{email}/effective-policy）：查询用户生效策略
    - 实现 GetSecuritySettingsHandler（GET /api/admin/security/settings）：获取系统设置
    - 实现 UpdateSecuritySettingsHandler（PUT /api/admin/security/settings）：更新系统设置
    - 实现 SetDefaultGroupHandler（PUT /api/admin/security/settings/default-group）：设置默认组
    - 实现 EnrollGroupTreeHandler（GET /api/enroll/group-tree）：注册流程用户组树（公开端点）
    - _需求: 9.1-9.16_

  - [x] 5.2 在 `hub/internal/httpapi/router.go` 中注册安全管理路由
    - 所有 `/api/admin/security/*` 路由通过 RequireAdmin 中间件保护
    - `/api/enroll/group-tree` 为公开端点
    - NewRouter 函数签名新增 SecurityService 参数
    - _需求: 9.12, 9.13_

- [x] 6. 实现 IM 外发拦截器
  - [x] 6.1 创建 `hub/internal/im/outbound_interceptor.go`，实现 OutboundInterceptor
    - 实现 CheckOutbound：检查集中管控开关和用户的文件/图片外发权限
    - 文件外发被拦截时替换为 GenericResponse{StatusCode: 403, Body: "文件外发已被管理员禁止"}
    - 图片外发被拦截时替换为 GenericResponse{StatusCode: 403, Body: "图片外发已被管理员禁止"}
    - 策略查询失败时 fail-open（放行），记录错误日志
    - 拦截时记录审计日志（用户邮箱、目标 IM 平台、文件类型）
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 6.2 在 `hub/internal/im/` 中集成 OutboundInterceptor
    - 在 im.Adapter 的 sendResponse 路径中插入拦截检查
    - 仅对 IM 通道（飞书、QQ Bot、OpenClaw Bridge、Telegram、微信）的外发消息执行检查
    - 本地 AI 助手对话不受限制
    - _需求: 5.3, 5.4_

  - [ ]* 6.3 编写属性测试：IM 外发拦截
    - **Property 11: IM 外发拦截**
    - **验证: 需求 5.1, 5.2, 5.3**

- [x] 7. 实现心跳策略注入
  - [x] 7.1 修改 `hub/internal/ws/` 心跳处理，注入安全策略
    - 在 ws.Gateway 中新增 SecurityProvider 字段（SecurityPolicyProvider 接口）
    - 在 handleMachineHeartbeat 的 ack 响应中附带 security_policy 字段
    - 集中管控开启时下发 centralized_security: true + EffectivePolicy
    - 集中管控关闭时下发 centralized_security: false，不下发策略数据
    - _需求: 4.3, 4.4_

  - [ ]* 7.2 编写属性测试：心跳响应根据集中管控状态下发策略
    - **Property 10: 心跳响应根据集中管控状态下发策略**
    - **验证: 需求 4.3, 4.4**

- [x] 8. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。


- [x] 9. Bootstrap 集成
  - [x] 9.1 修改 `hub/internal/app/bootstrap.go`，初始化 SecurityService 并注入依赖
    - 创建 SecurityStore（复用现有 SQLite Provider）
    - 创建 SecurityService，注入 SecurityStore、SystemSettingsRepository、AdminAuditRepository
    - 调用 InitRootGroup 确保根节点存在
    - 将 SecurityService 传入 NewRouter
    - 将 SecurityService 作为 SecurityProvider 注入 ws.Gateway
    - 将 SecurityService 作为 SecurityPolicyProvider 注入 OutboundInterceptor，并连接到 im.Adapter
    - _需求: 1.1, 4.3, 5.1_

- [x] 10. 注册流程集成组织机构开关
  - [x] 10.1 修改 `hub/internal/httpapi/enroll_handler.go`，在 enrollment 响应中注入组织机构数据
    - 当 org_structure_enabled 为 true 时，响应包含 org_structure_enabled: true 和用户组树
    - 当 org_structure_enabled 为 false 时，响应包含 org_structure_enabled: false
    - _需求: 10.2, 10.4_

  - [x] 10.2 修改注册流程中的用户分组逻辑
    - org_structure_enabled 为 false 时，新用户分配到 Root_Group
    - org_structure_enabled 为 true 且用户选择了部门时，分配到所选组
    - org_structure_enabled 为 true 但未选择部门且设置了 default_group_id 时，分配到默认组
    - 以上均不满足时，分配到 Root_Group
    - _需求: 1.10, 1.11, 1.12_

  - [ ]* 10.3 编写属性测试：新用户自动分组
    - **Property 6: 新用户自动分组**
    - **验证: 需求 1.10, 1.11, 1.12**

  - [ ]* 10.4 编写属性测试：注册响应根据组织机构开关返回数据
    - **Property 16: 注册响应根据组织机构开关返回数据**
    - **验证: 需求 10.2, 10.4**

  - [ ]* 10.5 编写属性测试：设置变更审计日志
    - **Property 12: 设置变更审计日志**
    - **验证: 需求 4.7, 4.8, 5.5, 10.8, 10.9**

- [x] 11. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 12. Hub 管理后台安全管理界面
  - [x] 12.1 在 `hub/web/admin/index.html` 中新增安全管理 Tab
    - 在导航栏新增"安全管理"菜单项
    - 顶部区域：集中管控开关 + 组织机构开关 + 默认组设置入口
    - 左侧区域：用户组树形视图（展开/折叠、右键菜单：创建子组、重命名、删除、分配用户）
    - 右侧区域：策略配置面板（表单展示所有 Policy_Item，标注"继承自 [父组名]"或"本组设置"）
    - 策略配置面板支持"设置/清除（恢复继承）"操作
    - 切换集中管控开关时弹出确认对话框
    - _需求: 8.1-8.8, 10.10_

- [x] 13. 客户端策略接收与执行（GUI）
  - [x] 13.1 修改 `gui/app.go`，实现心跳 ack 中安全策略的解析和缓存
    - 解析心跳 ack 中的 security_policy 字段
    - 维护 HubSecurityPolicy 本地缓存
    - 策略变更时通知前端 React 组件切换只读模式
    - Hub 断开连接时继续使用最后缓存的策略
    - _需求: 7.1, 7.2, 7.3, 7.4_

  - [x] 13.2 实现 GUI 侧策略执行
    - guardrail_mode 变化时调用 PolicyEngine.SetMode
    - sandbox_mode 变化时更新 Firewall 沙箱配置
    - network_level 变化时更新网络访问级别
    - yolo_mode_allowed 为 false 时强制关闭 YOLO 模式
    - centralized_security 为 true 时安全设置界面切换为只读模式
    - _需求: 4.5, 7.5, 7.6, 7.7, 7.8_

  - [x] 13.3 实现 GUI 侧 Gossip 模块权限控制
    - gossip_enabled 为 false 时隐藏 Gossip 入口（侧边栏图标和面板）
    - AutoPublishTrigger 跳过自动发布逻辑
    - GossipClient 拒绝发布和上传请求并返回权限错误
    - _需求: 6.1, 6.3, 6.4_

  - [ ]* 13.4 编写属性测试：Gossip 禁用执行
    - **Property 13: Gossip 禁用执行**
    - **验证: 需求 6.1, 6.3, 6.4, 6.5**

- [x] 14. 客户端策略接收与执行（TUI）
  - [x] 14.1 修改 `tui/config_watcher.go`，实现 TUI 侧心跳策略解析和缓存
    - 解析心跳 ack 中的 security_policy 字段
    - 维护本地策略缓存
    - Hub 断开连接时继续使用最后缓存的策略
    - _需求: 7.1, 7.2, 7.4_

  - [x] 14.2 实现 TUI 侧策略执行
    - guardrail_mode / sandbox_mode / network_level 变化时更新对应配置
    - yolo_mode_allowed 为 false 时强制关闭 YOLO 模式
    - gossip_enabled 为 false 时禁用 gossip 子命令并返回"Gossip 功能已被管理员禁止"
    - centralized_security 为 true 时安全设置命令切换为只读模式
    - _需求: 4.6, 6.2, 7.5, 7.6, 7.7, 7.8_

  - [ ]* 14.3 编写属性测试：客户端策略应用
    - **Property 14: 客户端策略应用**
    - **验证: 需求 7.1, 7.3, 7.5, 7.6, 7.7**

  - [ ]* 14.4 编写属性测试：YOLO 模式强制覆盖
    - **Property 15: YOLO 模式强制覆盖**
    - **验证: 需求 7.8**

  - [ ]* 14.5 编写属性测试：离线策略持久化
    - **Property 17: 离线策略持久化**
    - **验证: 需求 7.2**

- [x] 15. 最终检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 开发
- 每个任务引用了具体的需求编号以确保可追溯性
- 检查点确保增量验证
- 属性测试使用 [gopter](https://github.com/leanovate/gopter) 库验证通用正确性属性
- 单元测试验证具体示例和边界情况
- 所有 17 个正确性属性均已分配到对应的实现任务中
