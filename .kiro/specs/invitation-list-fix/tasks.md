# 实施计划

- [x] 1. 编写 Bug Condition 探索性测试
  - **Property 1: Bug Condition** - 邀请列表 API 返回 410 Gone 或字段名大写
  - **重要**: 此属性基测试必须在实施修复之前编写
  - **关键**: 此测试在未修复代码上必须失败 — 失败即确认 bug 存在
  - **请勿**在测试失败时尝试修复测试或代码
  - **说明**: 此测试编码了期望行为 — 修复实施后测试通过即验证修复正确
  - **目标**: 产生反例，证明 bug 存在
  - **Scoped PBT 方法**: 将属性范围限定到具体失败场景：对 `GET /api/admin/invites` 和 `DELETE /api/admin/invites/{id}` 的请求
  - 测试文件: `hub/internal/httpapi/invitation_handler_test.go`
  - 构建测试用 HTTP handler，发送 `GET /api/admin/invites` 请求
  - 断言: 响应状态码应为 200（非 410 Gone）
  - 断言: 响应 JSON 中每个邀请对象应包含小写字段名 `email`, `role`, `status`（非 `Email`, `Role`, `Status`）
  - 断言: `DELETE /api/admin/invites/{id}` 路由应存在且返回 200（非 404）
  - 在未修复代码上运行测试
  - **预期结果**: 测试失败（这是正确的 — 证明 bug 存在：API 返回 410 Gone，DELETE 路由不存在）
  - 记录发现的反例（如 "GET /api/admin/invites 返回 410 Gone 而非邀请数据"）
  - 测试编写、运行并记录失败后，标记任务完成
  - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. 编写 Preservation 属性测试（在实施修复之前）
  - **Property 2: Preservation** - 非邀请列表功能行为不变
  - **重要**: 遵循观察优先方法论
  - 测试文件: `hub/internal/httpapi/invitation_handler_preservation_test.go`
  - 观察: 在未修复代码上调用 `GET /api/admin/invitation-codes` 返回邀请码列表
  - 观察: 在未修复代码上调用 `GET /api/admin/blocklist` 返回黑名单列表
  - 观察: 在未修复代码上调用 `GET /api/admin/users` 返回用户列表
  - 观察: 在未修复代码上调用 `POST /api/admin/invitation-codes/generate` 正确生成邀请码
  - 编写属性基测试: 对所有非 `/api/admin/invites` 路径的管理 API 请求，修复前后行为完全一致
  - 验证 `invitationCodeResponse` 的 JSON 序列化字段名保持小写（`id`, `code`, `status`, `used_by_email` 等）
  - 验证 `toInvitationCodeResponse` 转换函数输出不变
  - 在未修复代码上运行测试
  - **预期结果**: 测试通过（确认需要保持的基线行为）
  - 测试编写、运行并通过后，标记任务完成
  - _Requirements: 3.1, 3.2, 3.3, 3.4_

- [x] 3. 修复邀请列表三个缺陷（字段 undefined、缺少删除按钮、缺少 DELETE API）

  - [x] 3.1 在 store.go 中新增 EmailInvite 结构体和 EmailInviteRepository 接口
    - 新增 `EmailInvite` struct，包含 `ID`, `Email`, `Role`, `Status`, `CreatedAt`, `UpdatedAt` 字段
    - 新增 `EmailInviteRepository` 接口，包含 `Create`, `List`, `GetByID`, `DeleteByID` 方法
    - 在 `Store` struct 中添加 `EmailInvites EmailInviteRepository` 字段
    - _Bug_Condition: isBugCondition(request) where request.path == "/api/admin/invites"_
    - _Expected_Behavior: EmailInvite struct 提供正确的数据模型支撑_
    - _Preservation: 不修改任何现有 struct 和 interface_
    - _Requirements: 2.1, 2.2_

  - [x] 3.2 创建 SQLite EmailInviteRepository 实现
    - 创建 `hub/internal/store/sqlite/email_invite_repo.go`
    - 实现 `EmailInviteRepository` 接口的所有方法
    - 创建 `email_invites` 表（如不存在），包含 id, email, role, status, created_at, updated_at 字段
    - _Bug_Condition: 后端缺少邀请数据的持久化层_
    - _Expected_Behavior: 邀请数据可正确 CRUD_
    - _Preservation: 不修改现有 repository 实现_
    - _Requirements: 2.1, 2.3_

  - [x] 3.3 在 invitation_handler.go 中新增 emailInviteResponse 和三个 Handler
    - 参考 `invitationCodeResponse` 模式，新增带 JSON tags 的 `emailInviteResponse` struct（字段: `id`, `email`, `role`, `status`, `created_at`, `updated_at`）
    - 新增 `toEmailInviteResponse` 转换函数
    - 新增 `CreateEmailInviteHandler` — 处理 `POST /api/admin/invites`，接收 email 和 role 参数
    - 新增 `ListEmailInvitesHandler` — 处理 `GET /api/admin/invites`，返回 `{"invites": [...]}`
    - 新增 `DeleteEmailInviteHandler` — 处理 `DELETE /api/admin/invites/{id}`
    - _Bug_Condition: isBugCondition(request) — 当前返回 410 Gone 或 JSON 字段名大写_
    - _Expected_Behavior: expectedBehavior(response) — 返回小写 JSON 字段名，前端 v.email/v.role/v.status 可正确读取_
    - _Preservation: 保留 DeprecatedEmailInviteHandler 函数（不删除），保留 invitationCodeResponse 及相关 handler 不变_
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2, 2.3_

  - [x] 3.4 在 router.go 中替换 deprecated 路由并新增 DELETE 路由
    - 将 `POST /api/admin/invites` 从 `DeprecatedEmailInviteHandler()` 改为 `CreateEmailInviteHandler`
    - 将 `GET /api/admin/invites` 从 `DeprecatedEmailInviteHandler()` 改为 `ListEmailInvitesHandler`
    - 新增 `DELETE /api/admin/invites/{id}` 路由，指向 `DeleteEmailInviteHandler`
    - 更新 `NewRouter` 函数签名，接收 `EmailInviteRepository` 参数（或通过 Store 传入）
    - _Bug_Condition: 当前路由指向 DeprecatedEmailInviteHandler 返回 410 Gone_
    - _Expected_Behavior: 路由指向新 handler，API 正常工作_
    - _Preservation: 所有其他路由不变_
    - _Requirements: 1.1, 1.3, 2.1, 2.3_

  - [x] 3.5 恢复前端 HTML 邀请表单和列表容器
    - 在 `hub/web/admin/index.html` governance 标签页中恢复邀请表单（email 输入框、role 选择框、创建按钮）
    - 恢复邀请列表容器 `<div id="invites">`
    - 添加 `inviteCountHero` 指标显示
    - _Bug_Condition: 前端 HTML 中缺少邀请相关 DOM 元素_
    - _Expected_Behavior: DOM 元素存在，JS 函数可正确操作_
    - _Preservation: 不修改其他标签页的 HTML 结构_
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 3.6 更新前端 JS 添加删除按钮和 deleteInvite 函数
    - 在 `hub/web/admin/index.inline.js` 的 `renderInvites` 函数中为每条邀请记录添加删除按钮
    - 新增 `deleteInvite(id)` 函数，调用 `DELETE /api/admin/invites/{id}`，成功后刷新列表
    - 确保 `loadInvites` 在 `refreshAll` 中被调用
    - _Bug_Condition: 前端缺少删除按钮和删除功能_
    - _Expected_Behavior: 每条邀请旁有删除按钮，点击后删除并刷新_
    - _Preservation: 不修改 renderInvitationCodes、renderBlocklist 等其他渲染函数_
    - _Requirements: 1.3, 2.3_

  - [x] 3.7 验证 Bug Condition 探索性测试现在通过
    - **Property 1: Expected Behavior** - 邀请列表 API 返回正确 JSON 字段名
    - **重要**: 重新运行任务 1 中的同一测试 — 不要编写新测试
    - 任务 1 的测试编码了期望行为
    - 当此测试通过时，确认期望行为已满足
    - 运行任务 1 中的 bug condition 探索性测试
    - **预期结果**: 测试通过（确认 bug 已修复）
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 3.8 验证 Preservation 测试仍然通过
    - **Property 2: Preservation** - 非邀请列表功能行为不变
    - **重要**: 重新运行任务 2 中的同一测试 — 不要编写新测试
    - 运行任务 2 中的 preservation 属性测试
    - **预期结果**: 测试通过（确认无回归）
    - 确认修复后所有测试仍然通过（无回归）

- [x] 4. 检查点 - 确保所有测试通过
  - 运行所有相关测试，确保全部通过
  - 如有问题，询问用户
