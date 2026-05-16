# Bugfix 需求文档

## 简介

Hub 管理后台的邀请管理（Invites）列表存在三个缺陷：
1. 邮箱和角色字段显示为 "undefined"，因为后端 `EmailInvite` struct 缺少 JSON tags，Go 默认序列化为大写字段名（`Email`, `Role`, `Status`），而前端 `renderInvites` 使用小写字段名（`v.email`, `v.role`, `v.status`）。
2. 邀请列表缺少删除按钮，管理员无法删除已创建的邀请记录。
3. 后端缺少删除邀请的 API endpoint。

对比参考：`InvitationCode` 的处理方式是正确的 — `invitation_handler.go` 中有 `toInvitationCodeResponse` 函数做了字段映射，使用了带 json tags 的 `invitationCodeResponse` struct。

## Bug 分析

### 当前行为（缺陷）

1.1 WHEN 管理员访问邀请列表页面 THEN 系统返回的 JSON 字段名为大写开头（`Email`, `Role`, `Status`），但前端使用小写字段名（`v.email`, `v.role`, `v.status`）读取，导致邮箱和角色字段显示为 "undefined"

1.2 WHEN 管理员创建邀请后查看列表 THEN 邀请的角色信息虽然在数据库中正确存储，但因 JSON 字段名不匹配而无法在前端正确显示

1.3 WHEN 管理员想要删除一条邀请记录 THEN 系统没有提供删除按钮，也没有对应的后端 DELETE API endpoint，管理员无法执行删除操作

### 期望行为（正确）

2.1 WHEN 管理员访问邀请列表页面 THEN 系统 SHALL 返回小写 JSON 字段名（`id`, `email`, `role`, `status`, `created_at`, `updated_at`），前端能正确显示邮箱、角色和状态信息

2.2 WHEN 管理员创建邀请后查看列表 THEN 系统 SHALL 正确显示创建时选择的角色（viewer / member / admin）

2.3 WHEN 管理员想要删除一条邀请记录 THEN 系统 SHALL 在每条邀请记录旁提供删除按钮，点击后调用 DELETE API 删除该邀请，并刷新列表

### 不变行为（回归防护）

3.1 WHEN 管理员创建新邀请（POST /api/admin/invites） THEN 系统 SHALL CONTINUE TO 正确接收 email 和 role 参数并存入数据库

3.2 WHEN 管理员访问其他管理页面（黑名单列表、邀请码列表、机器列表等） THEN 系统 SHALL CONTINUE TO 正常显示数据，不受邀请列表修复的影响

3.3 WHEN 用户通过邀请邮箱进行注册流程 THEN 系统 SHALL CONTINUE TO 正确验证邀请状态并完成注册

3.4 WHEN `InvitationCode` 相关 API 被调用 THEN 系统 SHALL CONTINUE TO 使用现有的 `invitationCodeResponse` 映射方式正常工作
