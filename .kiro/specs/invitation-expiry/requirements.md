# 需求文档：邀请码有效期管理

## 简介

为 Hub 的邀请码系统新增有效期管理功能。管理员在生成邀请码时可设置有效期（按天/月/年），邀请码绑定用户后从绑定日起计算剩余有效期。过期用户将无法继续使用 Hub，但可通过新邀请码重新绑定。原有无有效期的邀请码视为长期有效，保持向后兼容。

## 术语表

- **Hub**：自托管的中心服务，管理用户注册、设备绑定和访问控制
- **邀请码服务（Invitation_Service）**：负责邀请码生成、验证、消费和有效期管理的后端服务模块（对应 `hub/internal/invitation/service.go`）
- **邀请码仓储（Invitation_Repository）**：邀请码的数据持久化层接口（对应 `store.InvitationCodeRepository`）
- **身份服务（Identity_Service）**：负责用户注册、登录和身份验证的后端服务模块（对应 `hub/internal/auth/identity_service.go`）
- **管理后台（Admin_Console）**：Hub 的 Web 管理控制台（对应 `hub/web/admin/`）
- **桌面端（Desktop_Client）**：MaClaw Desktop 客户端应用
- **有效期天数（Validity_Days）**：邀请码的有效期，以天为单位存储。0 表示长期有效
- **绑定时间（Bound_At）**：邀请码被用户使用（绑定）的时间戳
- **过期时间（Expires_At）**：根据绑定时间和有效期天数计算得出的用户到期时间

## 需求

### 需求 1：邀请码生成时设置有效期

**用户故事：** 作为管理员，我希望在生成邀请码时设置有效期，以便控制用户的使用时长。

#### 验收标准

1. WHEN 管理员请求生成邀请码时，THE Invitation_Service SHALL 接受一个 Validity_Days 参数，表示有效期天数
2. WHEN Validity_Days 为 0 时，THE Invitation_Service SHALL 将该邀请码标记为长期有效
3. WHEN Validity_Days 大于 0 时，THE Invitation_Service SHALL 将该值存储到邀请码记录的 validity_days 字段中
4. THE Invitation_Repository SHALL 在 InvitationCode 数据模型中包含 validity_days 整数字段，默认值为 0

### 需求 2：管理后台有效期输入支持

**用户故事：** 作为管理员，我希望在管理后台生成邀请码时能方便地选择有效期单位（天/月/年），以便快速设置不同时长。

#### 验收标准

1. THE Admin_Console SHALL 在邀请码生成表单中提供有效期数值输入框和单位选择器（天/月/年）
2. WHEN 管理员选择"月"单位时，THE Admin_Console SHALL 将输入值乘以 30 转换为天数后提交给 Invitation_Service
3. WHEN 管理员选择"年"单位时，THE Admin_Console SHALL 将输入值乘以 365 转换为天数后提交给 Invitation_Service
4. THE Admin_Console SHALL 提供"长期有效"选项，选中时将 Validity_Days 设为 0
5. WHEN 管理员未设置有效期时，THE Admin_Console SHALL 默认使用"长期有效"

### 需求 3：邀请码列表显示有效期信息

**用户故事：** 作为管理员，我希望在邀请码列表中查看每个邀请码的有效期信息，以便了解邀请码的时效状态。

#### 验收标准

1. THE Invitation_Service SHALL 在邀请码列表 API 响应中包含 validity_days 字段
2. THE Invitation_Service SHALL 在邀请码列表 API 响应中包含 bound_at（绑定时间）字段
3. WHEN 邀请码的 validity_days 为 0 时，THE Admin_Console SHALL 显示"长期有效"
4. WHEN 邀请码已被使用且 validity_days 大于 0 时，THE Admin_Console SHALL 显示剩余有效天数（从绑定日起计算）
5. WHEN 邀请码已过期时，THE Admin_Console SHALL 以醒目样式标记该邀请码为"已过期"

### 需求 4：绑定时记录绑定时间

**用户故事：** 作为系统，我需要在邀请码被使用时记录绑定时间，以便后续计算剩余有效期。

#### 验收标准

1. WHEN 邀请码被用户使用（消费）时，THE Invitation_Service SHALL 记录当前时间为 Bound_At
2. THE Invitation_Repository SHALL 在 InvitationCode 数据模型中包含 bound_at 时间戳字段
3. THE Invitation_Service SHALL 使用已有的 UsedAt 字段作为 Bound_At 的数据来源，保持数据一致性

### 需求 5：过期用户注册拦截

**用户故事：** 作为系统，我需要阻止过期用户继续使用 Hub，以确保有效期策略得到执行。

#### 验收标准

1. WHEN 用户尝试注册（Enroll）到 Hub 时，THE Identity_Service SHALL 检查该用户关联的邀请码是否已过期
2. WHEN 用户关联的邀请码 validity_days 大于 0 且当前时间超过 Bound_At 加上 validity_days 天时，THE Identity_Service SHALL 拒绝该注册请求
3. WHEN 用户关联的邀请码 validity_days 为 0 时，THE Identity_Service SHALL 允许该注册请求（长期有效）
4. IF 用户关联的邀请码不存在有效期信息（旧数据），THEN THE Identity_Service SHALL 视为长期有效并允许注册

### 需求 6：桌面端过期提示

**用户故事：** 作为用户，我希望在桌面端注册时收到清晰的过期提示，以便了解需要获取新邀请码。

#### 验收标准

1. WHEN Identity_Service 因邀请码过期拒绝注册时，THE Identity_Service SHALL 返回特定的错误码 "INVITATION_EXPIRED"
2. WHEN 桌面端收到 "INVITATION_EXPIRED" 错误码时，THE Desktop_Client SHALL 显示"用户已失效，请使用新的邀请码重新绑定"提示信息
3. THE Identity_Service SHALL 在错误响应中包含过期时间信息，以便客户端展示具体的过期日期

### 需求 7：失效用户重新绑定

**用户故事：** 作为失效用户，我希望能使用新的邀请码重新绑定，以便恢复对 Hub 的访问。

#### 验收标准

1. WHEN 失效用户提供新的有效邀请码进行注册时，THE Identity_Service SHALL 接受该注册请求
2. WHEN 失效用户使用新邀请码成功绑定时，THE Identity_Service SHALL 更新该用户关联的邀请码为新邀请码
3. WHEN 失效用户使用新邀请码成功绑定时，THE Invitation_Service SHALL 将新邀请码标记为已使用并记录新的 Bound_At
4. THE Identity_Service SHALL 基于新邀请码的 validity_days 和新的 Bound_At 重新计算用户的有效期

### 需求 8：向后兼容

**用户故事：** 作为系统，我需要确保升级后原有的邀请码和用户不受影响。

#### 验收标准

1. WHEN 数据库中存在无 validity_days 字段的旧邀请码记录时，THE Invitation_Service SHALL 将其视为 validity_days = 0（长期有效）
2. WHEN 数据库中存在无 bound_at 字段的旧邀请码记录时，THE Invitation_Service SHALL 将 UsedAt 作为 Bound_At 的回退值
3. THE Invitation_Repository SHALL 在数据库迁移时为 validity_days 字段设置默认值 0
4. THE Invitation_Repository SHALL 在数据库迁移时不修改已有邀请码记录的数据


### 需求 9：移除 Email 邀请功能

**用户故事：** 作为管理员，我不再需要通过 Email 邀请用户注册 Hub，因此系统应完全移除 Email 邀请相关功能，以简化管理界面和后端代码。

#### 验收标准

1. THE Admin_Console SHALL 移除 Governance 页面中的"邀请管理"（Invites）区域，包括创建邀请的表单和邀请列表显示
2. THE Hub SHALL 废弃 Email 邀请相关的 API endpoint（POST /api/admin/invites 和 GET /api/admin/invites）
3. WHEN 客户端调用已废弃的 Email 邀请 API 时，THE Hub SHALL 返回 HTTP 410 Gone 状态码
4. THE Admin_Console SHALL 在移除 Email 邀请功能后保持 Governance 页面中其他管理功能（邀请码管理、黑名单管理、机器管理等）正常运作
5. THE Hub SHALL 移除 Email 邀请相关的后端处理逻辑，包括 invitation_handler.go 中 EmailInvite 相关的 handler 函数
6. WHEN 数据库中存在历史 Email 邀请记录时，THE Hub SHALL 保留这些记录不做删除，仅停止提供新建和查询接口
