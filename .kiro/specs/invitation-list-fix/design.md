# 邀请列表修复 Bugfix 设计文档

## 概述

Hub 管理后台的邀请管理（Invites）列表存在三个缺陷：字段显示为 "undefined"、缺少删除按钮、后端缺少 DELETE API。经代码分析发现，Email invite 功能已被标记为 deprecated（后端返回 410 Gone），前端 governance 标签页的邀请表单/列表 HTML 已被移除，但 JS 中仍残留 `renderInvites`、`loadInvites`、`addInvite` 等死代码。

修复策略：恢复邀请列表功能，包括后端 API（带正确 JSON tags 的响应结构体）、前端 UI（邀请表单 + 列表 + 删除按钮），以及新增 DELETE endpoint。参考 `InvitationCode` 的 `invitationCodeResponse` 模式实现字段映射。

## 术语表

- **Bug_Condition (C)**：前端调用 `/api/admin/invites` 时，后端返回 410 Gone 或返回的 JSON 字段名为大写（`Email`, `Role`, `Status`），导致前端 `v.email`、`v.role`、`v.status` 读取为 `undefined`
- **Property (P)**：邀请列表 API 应返回小写 JSON 字段名，前端正确显示邮箱、角色、状态，并提供删除功能
- **Preservation**：邀请码（InvitationCode）相关功能、黑名单、用户绑定等现有功能不受影响
- **`EmailInvite`**：`store.go` 中需要新增或恢复的邮箱邀请数据结构
- **`emailInviteResponse`**：`invitation_handler.go` 中需要新增的带 JSON tags 的响应结构体（参考 `invitationCodeResponse` 模式）
- **`renderInvites`**：前端 JS 中渲染邀请列表的函数，使用 `v.email`、`v.role`、`v.status` 小写字段名

## Bug 详情

### Bug Condition

当管理员访问邀请列表页面时，前端调用 `GET /api/admin/invites`，但后端当前返回 410 Gone（`DeprecatedEmailInviteHandler`）。即使恢复了原始 handler，如果 `EmailInvite` struct 缺少 JSON tags，Go 的 `encoding/json` 会序列化为大写字段名（`Email`, `Role`, `Status`），而前端使用小写字段名（`v.email`, `v.role`, `v.status`）读取，导致显示 "undefined"。此外，前端没有删除按钮，后端也没有 DELETE endpoint。

**形式化规约：**
```
FUNCTION isBugCondition(request)
  INPUT: request of type HTTPRequest
  OUTPUT: boolean

  RETURN (request.method == "GET" AND request.path == "/api/admin/invites")
         OR (request.method == "POST" AND request.path == "/api/admin/invites")
         OR (request.method == "DELETE" AND request.path MATCHES "/api/admin/invites/{id}")
END FUNCTION
```

### 示例

- 管理员访问邀请列表 → `GET /api/admin/invites` → 返回 410 Gone，列表为空或报错
- 管理员创建邀请 → `POST /api/admin/invites` → 返回 410 Gone，创建失败
- 即使恢复 handler，返回 `{"Email":"user@example.com","Role":"viewer","Status":"pending"}` → 前端读 `v.email` 得到 `undefined`
- 管理员想删除邀请 → 无删除按钮，无 `DELETE /api/admin/invites/{id}` endpoint

## 期望行为

### Preservation 要求

**不变行为：**
- 邀请码（InvitationCode）的所有 API（生成、列表、导出、解绑、切换）必须继续正常工作
- 黑名单列表（blocklist）的 CRUD 操作不受影响
- 用户绑定（manual-bind）和用户列表不受影响
- 邀请邮箱注册流程（如果存在）继续正常验证

**范围：**
所有不涉及 `/api/admin/invites` 路径的请求完全不受此修复影响，包括：
- `/api/admin/invitation-codes/*` 所有端点
- `/api/admin/blocklist` 所有端点
- `/api/admin/users` 所有端点
- 其他管理页面和 IM 配置

## 假设的根因

基于代码分析，问题的根因是：

1. **功能被错误废弃**：`invitation_handler.go` 中 `POST /api/admin/invites` 和 `GET /api/admin/invites` 被替换为 `DeprecatedEmailInviteHandler()`，返回 410 Gone。但前端 JS 中 `loadInvites()`、`addInvite()`、`renderInvites()` 仍然引用这些端点。

2. **HTML 元素被移除**：governance 标签页的 HTML 中不再包含邀请表单（`inviteEmail` 输入框、`inviteRole` 选择框）和邀请列表容器（`id="invites"`），但 JS 函数仍然尝试操作这些 DOM 元素。

3. **缺少 JSON tags**：如果恢复原始 handler，`EmailInvite` struct（如果存在于 store 中）缺少 `json:"..."` tags，Go 默认序列化为大写字段名，与前端小写字段名不匹配。

4. **缺少 DELETE 功能**：从未实现过邀请记录的删除功能——既没有后端 endpoint，也没有前端删除按钮。

## 正确性属性

Property 1: Bug Condition - 邀请列表 API 返回正确 JSON 字段名

_For any_ 对 `GET /api/admin/invites` 的请求，修复后的 handler SHALL 返回 JSON 数组，其中每个邀请对象包含小写字段名（`id`, `email`, `role`, `status`, `created_at`, `updated_at`），前端 `renderInvites` 能正确读取 `v.email`、`v.role`、`v.status` 并显示非 "undefined" 的值。

**验证: 需求 2.1, 2.2**

Property 2: Preservation - 非邀请列表功能不受影响

_For any_ 不涉及 `/api/admin/invites` 路径的请求，修复后的代码 SHALL 产生与修复前完全相同的行为，保留邀请码管理、黑名单、用户绑定等所有现有功能。

**验证: 需求 3.1, 3.2, 3.3, 3.4**

## 修复实现

### 所需变更

假设根因分析正确：

**文件**: `hub/internal/store/store.go`

**变更 1 - 新增 EmailInvite 结构体**：
- 在 store 包中新增 `EmailInvite` struct，包含 `ID`, `Email`, `Role`, `Status`, `CreatedAt`, `UpdatedAt` 字段
- 新增 `EmailInviteRepository` 接口，包含 `Create`, `List`, `GetByID`, `DeleteByID` 方法
- 在 `Store` struct 中添加 `EmailInvites EmailInviteRepository` 字段

**文件**: `hub/internal/store/sqlite/` (对应的 SQLite 实现)

**变更 2 - 实现 Repository**：
- 创建 `email_invite_repo.go`，实现 `EmailInviteRepository` 接口
- 创建 `email_invites` 表（如不存在）

**文件**: `hub/internal/httpapi/invitation_handler.go`

**变更 3 - 新增 emailInviteResponse 结构体**：
- 参考 `invitationCodeResponse` 模式，新增带 JSON tags 的 `emailInviteResponse` struct
- 新增 `toEmailInviteResponse` 转换函数
- 新增 `CreateEmailInviteHandler` — 处理 `POST /api/admin/invites`
- 新增 `ListEmailInvitesHandler` — 处理 `GET /api/admin/invites`，返回 `{"invites": [...]}`
- 新增 `DeleteEmailInviteHandler` — 处理 `DELETE /api/admin/invites/{id}`

**文件**: `hub/internal/httpapi/router.go`

**变更 4 - 恢复路由**：
- 将 `POST /api/admin/invites` 和 `GET /api/admin/invites` 从 `DeprecatedEmailInviteHandler()` 改为新的 handler
- 新增 `DELETE /api/admin/invites/{id}` 路由

**文件**: `hub/web/admin/index.html`

**变更 5 - 恢复前端 HTML**：
- 在 governance 标签页中恢复邀请表单（email 输入框、role 选择框、创建按钮）
- 恢复邀请列表容器 `<div id="invites">`
- 添加 `inviteCountHero` 指标显示

**文件**: `hub/web/admin/index.inline.js` (及 `admin-check.js`, `hub-admin-check.js`)

**变更 6 - 更新前端 JS**：
- 在 `renderInvites` 函数中为每条邀请记录添加删除按钮
- 新增 `deleteInvite(id)` 函数，调用 `DELETE /api/admin/invites/{id}`
- 确保 `loadInvites` 在 `refreshAll` 中被调用

## 测试策略

### 验证方法

测试策略分两阶段：首先在未修复代码上验证 bug 存在（探索性测试），然后验证修复后的正确性和行为保持。

### 探索性 Bug Condition 检查

**目标**: 在实施修复前，验证 bug 确实存在，确认或否定根因分析。

**测试计划**: 对未修复代码发送 HTTP 请求到 `/api/admin/invites`，验证返回 410 Gone。

**测试用例**:
1. **GET 邀请列表**: 发送 `GET /api/admin/invites` → 预期返回 410 Gone（未修复代码上会失败）
2. **POST 创建邀请**: 发送 `POST /api/admin/invites` → 预期返回 410 Gone（未修复代码上会失败）
3. **DELETE 删除邀请**: 发送 `DELETE /api/admin/invites/xxx` → 预期返回 404（路由不存在）
4. **前端字段映射**: 如果绕过 410，验证返回的 JSON 字段名是否为大写

**预期反例**:
- API 返回 410 Gone 而非邀请数据
- 前端 `renderInvites` 因 DOM 元素不存在而静默失败

### Fix Checking

**目标**: 验证对所有满足 bug condition 的输入，修复后的函数产生期望行为。

**伪代码:**
```
FOR ALL request WHERE isBugCondition(request) DO
  response := handleRequest_fixed(request)
  IF request.method == "GET" THEN
    ASSERT response.status == 200
    ASSERT response.body.invites IS array
    FOR EACH invite IN response.body.invites DO
      ASSERT invite.email IS NOT undefined
      ASSERT invite.role IS NOT undefined
      ASSERT invite.status IS NOT undefined
    END FOR
  ELSE IF request.method == "DELETE" THEN
    ASSERT response.status == 200
    ASSERT invite no longer in list
  END IF
END FOR
```

### Preservation Checking

**目标**: 验证对所有不满足 bug condition 的输入，修复后的函数产生与原函数相同的结果。

**伪代码:**
```
FOR ALL request WHERE NOT isBugCondition(request) DO
  ASSERT handleRequest_original(request) = handleRequest_fixed(request)
END FOR
```

**测试方法**: 推荐使用属性基测试（Property-Based Testing）进行 preservation checking，因为：
- 自动生成大量测试用例覆盖输入域
- 捕获手动单元测试可能遗漏的边界情况
- 对非 bug 输入的行为不变提供强保证

**测试计划**: 先在未修复代码上观察邀请码、黑名单等功能的行为，然后编写属性基测试验证修复后行为一致。

**测试用例**:
1. **邀请码 API 保持**: 验证 `GET /api/admin/invitation-codes` 修复前后返回相同结果
2. **黑名单 API 保持**: 验证 `GET /api/admin/blocklist` 修复前后返回相同结果
3. **用户列表 API 保持**: 验证 `GET /api/admin/users` 修复前后返回相同结果
4. **邀请码生成保持**: 验证 `POST /api/admin/invitation-codes/generate` 修复前后行为一致

### 单元测试

- 测试 `emailInviteResponse` JSON 序列化输出字段名为小写
- 测试 `CreateEmailInviteHandler` 正确创建邀请并返回小写字段
- 测试 `ListEmailInvitesHandler` 返回正确格式的邀请列表
- 测试 `DeleteEmailInviteHandler` 正确删除指定邀请
- 测试边界情况：空邮箱、无效角色、不存在的 ID

### 属性基测试

- 生成随机邮箱和角色组合，验证创建后列表中能正确显示
- 生成随机邀请 ID，验证删除后列表中不再包含该邀请
- 对非 `/api/admin/invites` 路径的请求，验证修复前后行为完全一致

### 集成测试

- 完整流程：创建邀请 → 列表显示 → 删除邀请 → 列表更新
- 验证邀请列表与邀请码列表互不干扰
- 验证前端 `renderInvites` 正确渲染所有字段（非 "undefined"）
