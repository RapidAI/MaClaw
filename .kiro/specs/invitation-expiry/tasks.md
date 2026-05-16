# 实施计划：邀请码有效期管理

## 概述

基于设计文档，按数据层 → 服务层 → HTTP 层 → 前端 → 桌面端的顺序逐步实现邀请码有效期功能，同时移除已废弃的 Email 邀请功能。每一步都在前一步基础上构建，确保增量可验证。

## 任务

- [x] 1. 数据层扩展：InvitationCode 模型与仓储接口
  - [x] 1.1 在 `hub/internal/store/store.go` 的 `InvitationCode` 结构体中新增 `ValidityDays int` 字段
    - 放在 `UsedAt` 字段之后、`CreatedAt` 之前
    - _需求: 1.4, 8.1_
  - [x] 1.2 在 `hub/internal/store/store.go` 的 `InvitationCodeRepository` 接口中新增 `GetByEmail(ctx context.Context, email string) (*InvitationCode, error)` 方法
    - 按 `used_by_email` 查询状态为 `used` 的邀请码，返回最新一条
    - _需求: 5.1, 7.1_
  - [x] 1.3 在 `hub/internal/store/sqlite/migrations.go` 中添加 `ALTER TABLE invitation_codes ADD COLUMN validity_days INTEGER NOT NULL DEFAULT 0` 迁移语句
    - 追加到现有迁移语句列表末尾
    - 默认值 0 确保旧数据视为长期有效
    - _需求: 8.3, 8.4_
  - [x] 1.4 在 `hub/internal/store/sqlite/repositories_stub.go` 中更新 `invitationCodeRepo` 的所有 SQL 方法以包含 `validity_days` 列
    - `Create`: INSERT 语句新增 `validity_days` 列和 `item.ValidityDays` 参数
    - `GetByCode`: SELECT 和 Scan 新增 `validity_days`
    - `List`: SELECT 和 Scan 新增 `validity_days`
    - `ListPaged`: SELECT 和 Scan 新增 `validity_days`
    - _需求: 1.4, 3.1_
  - [x] 1.5 在 `hub/internal/store/sqlite/repositories_stub.go` 中实现 `GetByEmail` 方法
    - SQL: `SELECT ... FROM invitation_codes WHERE used_by_email = ? AND status = 'used' ORDER BY used_at DESC LIMIT 1`
    - 返回 `*store.InvitationCode` 或 `nil`
    - _需求: 5.1, 7.1_

- [x] 2. Invitation Service 扩展
  - [x] 2.1 修改 `hub/internal/invitation/service.go` 中 `GenerateCodes` 方法签名，新增 `validityDays int` 参数
    - 将 `validityDays` 赋值到新建的 `InvitationCode.ValidityDays` 字段
    - 当 `validityDays < 0` 时视为 0（长期有效）
    - _需求: 1.1, 1.2, 1.3_
  - [x] 2.2 在 `hub/internal/invitation/service.go` 中新增 `CheckExpiry(ctx context.Context, email string) (bool, *time.Time, error)` 方法
    - 调用 `s.repo.GetByEmail(ctx, email)` 获取用户关联的邀请码
    - 若未找到或 `validity_days == 0`，返回 `false, nil, nil`（未过期/长期有效）
    - 若 `validity_days > 0`，计算 `expiresAt = usedAt + validityDays * 24h`，判断 `time.Now().After(expiresAt)`
    - 返回 `expired, &expiresAt, nil`
    - _需求: 4.3, 5.1, 5.2, 5.3, 5.4, 8.1, 8.2_
  - [ ]* 2.3 在 `hub/internal/invitation/service_test.go` 中为 `GenerateCodes` 新增有效期相关测试
    - 测试 `validityDays > 0` 时生成的邀请码包含正确的 `ValidityDays` 值
    - 测试 `validityDays == 0` 时生成的邀请码 `ValidityDays` 为 0
    - 更新 `memInvitationCodeRepo` mock 以支持 `ValidityDays` 字段和 `GetByEmail` 方法
    - _需求: 1.1, 1.2, 1.3_
  - [ ]* 2.4 在 `hub/internal/invitation/service_test.go` 中为 `CheckExpiry` 新增测试
    - 测试长期有效邀请码（validity_days=0）返回未过期
    - 测试未过期邀请码（validity_days=30，used_at 为 10 天前）返回未过期
    - 测试已过期邀请码（validity_days=7，used_at 为 10 天前）返回已过期及正确的 expiresAt
    - 测试无关联邀请码的邮箱返回未过期
    - _需求: 5.2, 5.3, 5.4_

- [x] 3. 检查点 - 确保数据层和服务层测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 4. Identity Service 过期检查与重新绑定
  - [x] 4.1 在 `hub/internal/auth/identity_service.go` 中新增 `ErrInvitationExpired` 错误变量
    - `var ErrInvitationExpired = errors.New("invitation code has expired")`
    - _需求: 6.1_
  - [x] 4.2 扩展 `hub/internal/auth/identity_service.go` 中的 `InvitationCodeValidator` 接口，新增 `CheckExpiry(ctx context.Context, email string) (bool, *time.Time, error)` 方法
    - _需求: 5.1_
  - [x] 4.3 修改 `hub/internal/auth/identity_service.go` 中 `StartEnrollment` 方法，在已存在用户的分支中增加过期检查逻辑
    - 调用 `s.invitationSvc.CheckExpiry(ctx, email)` 检查是否过期
    - 若已过期且提供了新邀请码：调用 `ValidateAndConsume` 重新绑定，继续正常流程
    - 若已过期且未提供新邀请码：返回包含 `expires_at` 的 `EnrollmentResult` 和 `ErrInvitationExpired`
    - 在 `EnrollmentResult` 结构体中新增 `ExpiresAt string` 字段（json tag: `expires_at,omitempty`）
    - _需求: 5.1, 5.2, 5.3, 5.4, 7.1, 7.2, 7.3, 7.4_

- [x] 5. HTTP Handler 扩展
  - [x] 5.1 修改 `hub/internal/httpapi/invitation_handler.go` 中的 `generateCodesRequest` 结构体，新增 `ValidityDays int` 字段（json tag: `validity_days`）
    - 更新 `GenerateInvitationCodesHandler` 将 `req.ValidityDays` 传递给 `svc.GenerateCodes`
    - _需求: 1.1_
  - [x] 5.2 修改 `hub/internal/httpapi/invitation_handler.go` 中的 `invitationCodeResponse` 结构体，新增 `ValidityDays int` 字段（json tag: `validity_days`）和 `BoundAt *string` 字段（json tag: `bound_at`）
    - 更新 `toInvitationCodeResponse` 函数，映射 `ValidityDays` 和将 `UsedAt` 映射为 `BoundAt`
    - _需求: 3.1, 3.2_
  - [x] 5.3 修改 `hub/internal/httpapi/enroll_handler.go` 中的 `EnrollStartHandler`，新增 `auth.ErrInvitationExpired` 错误处理分支
    - 返回 HTTP 403，错误码 `INVITATION_EXPIRED`
    - 若 `resp` 不为 nil 且包含 `ExpiresAt`，在响应中包含 `expires_at` 字段
    - _需求: 6.1, 6.3_

- [x] 6. 检查点 - 确保后端 API 层编译通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 7. Admin Console 前端变更
  - [x] 7.1 修改 `hub/web/admin/index.inline.js` 中邀请码生成表单，新增有效期输入控件
    - 新增有效期数值输入框（默认为空）
    - 新增单位选择器：天(×1) / 月(×30) / 年(×365)
    - 新增"长期有效"复选框（默认选中），选中时 validity_days = 0
    - 取消选中时显示数值输入框和单位选择器
    - 提交时根据单位转换为天数，传入 `validity_days` 字段
    - 在 I18N 对象中添加相关中英文翻译键
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5_
  - [x] 7.2 修改 `hub/web/admin/index.inline.js` 中邀请码列表显示，新增有效期信息列
    - 显示 `validity_days`：0 显示"长期有效"，>0 显示具体天数
    - 已使用的邀请码：根据 `bound_at` 和 `validity_days` 计算并显示剩余天数
    - 已过期的邀请码：以红色/醒目样式标记"已过期"
    - _需求: 3.3, 3.4, 3.5_
  - [x] 7.3 从 `hub/web/admin/index.inline.js` 的 Governance 页面中移除 Email 邀请（Invites）区域
    - 移除 `inviteEmail` 输入框、`role` 选择器、`createInvite` 按钮
    - 移除 `invitesTitle`/`invitesDesc` 列表区域及其加载/渲染逻辑
    - 保留邀请码管理（Invitation Codes）Tab 不受影响
    - _需求: 9.1, 9.4_

- [x] 8. 后端移除 Email 邀请 API
  - [x] 8.1 在 `hub/internal/httpapi/invitation_handler.go` 中移除或废弃 Email 邀请相关的 handler 函数
    - 将 `POST /api/admin/invites` 和 `GET /api/admin/invites` 路由改为返回 HTTP 410 Gone
    - 移除 EmailInvite 相关的 handler 实现代码
    - _需求: 9.2, 9.3, 9.5_

- [x] 9. 桌面端过期错误处理
  - [x] 9.1 在桌面端 Enrollment 错误处理逻辑中新增 `INVITATION_EXPIRED` 分支
    - 当收到 `INVITATION_EXPIRED` 错误码时，显示"用户已失效，请使用新的邀请码重新绑定"提示
    - 如果响应中包含 `expires_at`，显示具体的过期日期
    - _需求: 6.2, 6.3_

- [x] 10. 最终检查点 - 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号以确保可追溯性
- 检查点确保增量验证
- 后端使用 Go 语言，前端为内联 JavaScript
