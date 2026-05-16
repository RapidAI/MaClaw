# 需求文档：Hub 安全管理

## 简介

在 Hub 上添加集中式安全管理能力，实现安全策略的集中管控。管理员可以在 Hub 管理后台创建用户组（树形结构），将用户分配到不同的用户组，并按用户组或全局维度设置安全策略。系统提供集中管控开关，开启后客户端本地安全设置变为只读模式（仍可查看当前值但不可修改）。安全管控项涵盖现有所有本地安全设置，并新增文件/图片外发权限控制和 Gossip 模块权限开关。

核心设计原则：
1. 树形用户组结构，根节点代表全局策略，所有用户默认归属根节点
2. 子节点策略继承父节点，可覆盖（override）特定项
3. 集中管控开关打开后，客户端安全设置由 Hub 下发，本地不可修改
4. 文件/图片外发定义为 MaClaw 向 IM 通道发送，本地 AI 助手中不受此限制

## 术语表

- **Hub**：组织内部部署的中心服务器（maclaw-hub），负责设备管理、用户认证、IM 路由等
- **Hub_Admin**：Hub 管理后台（Web 界面），管理员通过该界面进行安全策略配置
- **Security_Policy（安全策略）**：一组安全管控项的配置集合，定义了各项权限的允许/禁止状态
- **User_Group（用户组）**：树形结构中的一个节点，包含一组用户和一份安全策略配置
- **Root_Group（根用户组）**：用户组树的根节点，代表全局默认策略，所有未分配到子组的用户归属此组
- **Centralized_Control（集中管控）**：Hub 统一下发安全策略到客户端的模式，开启后客户端本地安全设置变为只读
- **File_Outbound（文件外发）**：MaClaw 客户端通过 IM 通道（飞书、QQ Bot、OpenClaw Bridge 等）向外部发送文件或图片的行为
- **Gossip_Module（Gossip 模块）**：MaClaw 客户端中的社区八卦功能模块，包含浏览、发布、评论等功能
- **Policy_Item（策略项）**：安全策略中的单个可配置项，如"文件外发权限"、"Gossip 模块开关"等
- **Effective_Policy（生效策略）**：用户最终生效的安全策略，由用户所在组的策略与祖先组策略合并计算得出
- **MaClaw**：本地桌面客户端应用，包含 GUI 和 TUI 两种界面
- **IM_Channel（IM 通道）**：Hub 连接的即时通讯平台，包括飞书、QQ Bot、OpenClaw Bridge、Telegram、微信等
- **Org_Structure（组织机构）**：Hub 中的组织架构管理功能，通过 `org_structure_enabled` 开关控制是否在注册流程中启用部门选择
- **Default_Group（默认组）**：管理员指定的新用户默认归属组，通过 `default_group_id` 设置，仅在组织机构开关打开时生效
- **Enrollment_Group_Tree（注册用户组树）**：注册流程中推送给客户端的用户组树形结构，供用户选择所属部门

## 需求

### 需求 1：用户组树形结构管理

**用户故事：** 作为 Hub 管理员，我希望能创建树形结构的用户组，以便按组织架构或业务需求对用户进行分组管理。

#### 验收标准

1. THE Hub SHALL 维护一棵用户组树，根节点（Root_Group）在系统初始化时自动创建，名称为"全局"
2. THE Hub_Admin SHALL 提供创建子用户组的功能，接受组名称和父组 ID 参数
3. THE Hub_Admin SHALL 提供重命名用户组的功能
4. THE Hub_Admin SHALL 提供删除用户组的功能
5. WHEN 管理员删除一个用户组时，THE Hub SHALL 将该组及其所有子组中的用户移回 Root_Group
6. THE Hub_Admin SHALL 提供将用户分配到指定用户组的功能，接受用户邮箱和目标组 ID 参数
7. WHEN 用户被分配到新用户组时，THE Hub SHALL 将该用户从原用户组中移除
8. THE Hub SHALL 禁止删除 Root_Group
9. THE Hub_Admin SHALL 提供查看用户组树完整结构的功能，返回包含组 ID、名称、父组 ID、子组列表和成员数量的树形数据
10. WHEN 新用户注册（通过 enrollment 流程加入 Hub）且 `org_structure_enabled` 为 false 时，THE Hub SHALL 将该用户自动分配到 Root_Group
11. WHEN 新用户注册且 `org_structure_enabled` 为 true 时，THE Hub SHALL 根据用户在注册流程中选择的部门或管理员设置的 `default_group_id` 将该用户分配到对应的 User_Group
12. WHEN 新用户注册且 `org_structure_enabled` 为 true 但用户未选择部门且 `default_group_id` 未设置时，THE Hub SHALL 将该用户分配到 Root_Group

### 需求 2：安全策略配置

**用户故事：** 作为 Hub 管理员，我希望能为每个用户组配置安全策略，以便对不同用户群体实施差异化的安全管控。

#### 验收标准

1. THE Security_Policy SHALL 包含以下 Policy_Item：
   - `file_outbound_enabled`（布尔值）：文件外发权限，控制 MaClaw 是否允许通过 IM_Channel 发送文件
   - `image_outbound_enabled`（布尔值）：图片外发权限，控制 MaClaw 是否允许通过 IM_Channel 发送图片
   - `gossip_enabled`（布尔值）：Gossip 模块开关，控制 MaClaw 是否启用 Gossip 功能
   - `guardrail_mode`（字符串）：安全护栏模式，取值为 "standard"、"strict"、"relaxed"
   - `sandbox_mode`（字符串）：沙箱模式，取值为 "none"、"os"、"docker"
   - `network_level`（字符串）：网络访问级别，取值为 "none"、"intranet"、"allowlist"、"audit"、"full"
   - `yolo_mode_allowed`（布尔值）：是否允许用户开启 YOLO 模式（自动执行不确认）
   - `smart_route_enabled`（布尔值）：是否允许使用 Hub LLM 智能路由
2. THE Hub_Admin SHALL 提供为指定用户组设置 Security_Policy 的功能
3. WHEN 管理员为用户组设置策略时，THE Hub SHALL 仅保存该组显式设置的 Policy_Item（未设置的项不存储，表示继承父组）
4. THE Root_Group SHALL 拥有一份完整的默认 Security_Policy，所有 Policy_Item 均有默认值
5. THE Root_Group 的默认 Security_Policy 值 SHALL 为：`file_outbound_enabled=true`、`image_outbound_enabled=true`、`gossip_enabled=true`、`guardrail_mode="standard"`、`sandbox_mode="none"`、`network_level="full"`、`yolo_mode_allowed=true`、`smart_route_enabled=true`
6. THE Hub_Admin SHALL 提供查看指定用户组当前策略配置的功能，返回该组显式设置的项和从父组继承的项（标注来源）

### 需求 3：策略继承与生效计算

**用户故事：** 作为 Hub 管理员，我希望子用户组能自动继承父组的安全策略，并可选择性覆盖特定项，以便减少重复配置工作。

#### 验收标准

1. WHEN 计算用户的 Effective_Policy 时，THE Hub SHALL 从 Root_Group 开始沿用户所在组的路径逐级合并策略，子组的显式设置覆盖父组的值
2. THE Hub SHALL 提供 API 端点查询指定用户的 Effective_Policy，返回每个 Policy_Item 的最终值和来源组 ID
3. WHEN 管理员修改某用户组的策略时，THE Hub SHALL 使该组及其所有子组中用户的 Effective_Policy 缓存失效
4. THE Hub SHALL 提供 API 端点查询指定用户组的 Effective_Policy（合并后的完整策略），用于管理员预览
5. WHEN 用户组树的层级深度超过 10 层时，THE Hub SHALL 拒绝创建更深层级的子组并返回错误提示

### 需求 4：集中管控开关

**用户故事：** 作为 Hub 管理员，我希望能通过一个全局开关启用集中安全管控，以便统一控制所有客户端的安全策略。

#### 验收标准

1. THE Hub SHALL 在系统设置中提供以下全局开关和配置项：
   - `centralized_security_enabled`（布尔值）：集中安全管控开关，默认为 false
   - `org_structure_enabled`（布尔值）：组织机构开关，默认为 false，控制注册流程是否启用部门选择
   - `default_group_id`（字符串，可选）：新用户默认归属的用户组 ID，仅在 `org_structure_enabled` 为 true 时生效
2. THE Hub_Admin SHALL 提供开启和关闭集中管控开关的功能
3. WHEN 集中管控开关为 true 时，THE Hub SHALL 在客户端心跳响应中下发 `centralized_security: true` 标记和用户的 Effective_Policy
4. WHEN 集中管控开关为 false 时，THE Hub SHALL 在客户端心跳响应中下发 `centralized_security: false` 标记，不下发策略数据
5. WHEN MaClaw 客户端收到 `centralized_security: true` 标记时，THE MaClaw SHALL 将本地安全设置界面切换为只读模式，显示当前生效值但禁止修改
6. WHEN MaClaw 客户端收到 `centralized_security: false` 标记时，THE MaClaw SHALL 恢复本地安全设置界面的可编辑状态
7. WHEN 集中管控开关从 false 切换为 true 时，THE Hub SHALL 记录一条审计日志，包含操作管理员和切换时间
8. WHEN 集中管控开关从 true 切换为 false 时，THE Hub SHALL 记录一条审计日志，包含操作管理员和切换时间

### 需求 5：文件/图片外发权限控制

**用户故事：** 作为 Hub 管理员，我希望能控制用户是否可以通过 IM 通道外发文件和图片，以便防止敏感信息泄露。

#### 验收标准

1. WHEN 用户的 Effective_Policy 中 `file_outbound_enabled` 为 false 时，THE Hub IM 路由 SHALL 拦截该用户通过 IM_Channel 发送的文件类消息，并向用户返回"文件外发已被管理员禁止"的提示
2. WHEN 用户的 Effective_Policy 中 `image_outbound_enabled` 为 false 时，THE Hub IM 路由 SHALL 拦截该用户通过 IM_Channel 发送的图片类消息，并向用户返回"图片外发已被管理员禁止"的提示
3. THE Hub IM 路由 SHALL 仅对通过 IM_Channel（飞书、QQ Bot、OpenClaw Bridge、Telegram、微信）的外发消息执行权限检查
4. THE Hub SHALL 不对本地 AI 助手对话中的文件和图片传输施加外发权限限制
5. WHEN 文件或图片外发被拦截时，THE Hub SHALL 记录一条审计日志，包含用户邮箱、目标 IM 平台和文件类型

### 需求 6：Gossip 模块权限控制

**用户故事：** 作为 Hub 管理员，我希望能控制用户是否可以使用 Gossip 功能，以便在需要时关闭社区互动功能。

#### 验收标准

1. WHEN 集中管控开关为 true 且用户的 Effective_Policy 中 `gossip_enabled` 为 false 时，THE MaClaw GUI SHALL 隐藏 Gossip 入口（侧边栏图标和面板）
2. WHEN 集中管控开关为 true 且用户的 Effective_Policy 中 `gossip_enabled` 为 false 时，THE MaClaw TUI SHALL 禁用 `gossip` 子命令并返回"Gossip 功能已被管理员禁止"的提示
3. WHEN 集中管控开关为 true 且用户的 Effective_Policy 中 `gossip_enabled` 为 false 时，THE AutoPublishTrigger SHALL 跳过所有自动发布逻辑
4. WHEN 集中管控开关为 true 且用户的 Effective_Policy 中 `gossip_enabled` 为 false 时，THE GossipClient SHALL 拒绝发布和上传请求并返回权限错误
5. WHEN 集中管控开关为 false 时，THE MaClaw SHALL 按本地配置决定 Gossip 功能的启用状态

### 需求 7：客户端策略同步与执行

**用户故事：** 作为 MaClaw 用户，我希望客户端能自动从 Hub 获取并执行安全策略，以便无需手动配置即可遵守组织安全规范。

#### 验收标准

1. WHEN MaClaw 客户端连接到 Hub 并收到集中管控策略时，THE MaClaw SHALL 将 Effective_Policy 缓存到本地内存
2. WHEN MaClaw 客户端与 Hub 断开连接时，THE MaClaw SHALL 继续使用最后一次收到的 Effective_Policy
3. WHEN MaClaw 客户端收到新的 Effective_Policy 且与缓存不同时，THE MaClaw SHALL 立即应用新策略
4. THE MaClaw SHALL 在每次心跳时检查 Hub 下发的策略是否有变更
5. WHEN Effective_Policy 中 `guardrail_mode` 值发生变化时，THE MaClaw SHALL 调用 PolicyEngine.SetMode 切换安全护栏模式
6. WHEN Effective_Policy 中 `sandbox_mode` 值发生变化时，THE MaClaw SHALL 更新 Firewall 的沙箱配置
7. WHEN Effective_Policy 中 `network_level` 值发生变化时，THE MaClaw SHALL 更新 Firewall 的网络访问级别
8. WHEN Effective_Policy 中 `yolo_mode_allowed` 为 false 时，THE MaClaw SHALL 强制关闭 YOLO 模式，即使用户在项目配置中开启了该选项

### 需求 8：Hub 管理后台安全管理界面

**用户故事：** 作为 Hub 管理员，我希望在管理后台有一个直观的安全管理界面，以便方便地管理用户组和安全策略。

#### 验收标准

1. THE Hub_Admin SHALL 在导航栏中新增"安全管理"菜单项
2. THE Hub_Admin 安全管理页面 SHALL 分为两个区域：左侧为用户组树形视图，右侧为选中组的策略配置面板
3. THE 用户组树形视图 SHALL 支持展开/折叠子组、显示每个组的成员数量
4. THE 用户组树形视图 SHALL 支持右键菜单操作：创建子组、重命名、删除、分配用户
5. THE 策略配置面板 SHALL 以表单形式展示所有 Policy_Item，每项旁标注"继承自 [父组名]"或"本组设置"
6. THE 策略配置面板 SHALL 支持对每个 Policy_Item 进行"设置/清除（恢复继承）"操作
7. THE Hub_Admin 安全管理页面 SHALL 在顶部显示集中管控开关的当前状态，并提供切换按钮
8. WHEN 管理员切换集中管控开关时，THE Hub_Admin SHALL 弹出确认对话框，说明切换影响

### 需求 9：安全管理 API

**用户故事：** 作为 Hub 开发者，我希望有一套完整的安全管理 REST API，以便管理后台和未来的自动化工具能调用。

#### 验收标准

1. THE Hub SHALL 提供 `GET /api/admin/security/groups` API，返回完整的用户组树结构
2. THE Hub SHALL 提供 `POST /api/admin/security/groups` API，创建新用户组，接受 `name` 和 `parent_id` 参数
3. THE Hub SHALL 提供 `PUT /api/admin/security/groups/{id}` API，更新用户组名称
4. THE Hub SHALL 提供 `DELETE /api/admin/security/groups/{id}` API，删除用户组
5. THE Hub SHALL 提供 `POST /api/admin/security/groups/{id}/members` API，将用户分配到指定组
6. THE Hub SHALL 提供 `DELETE /api/admin/security/groups/{id}/members/{email}` API，将用户从指定组移除（回到 Root_Group）
7. THE Hub SHALL 提供 `GET /api/admin/security/groups/{id}/policy` API，返回指定组的策略配置（含继承信息）
8. THE Hub SHALL 提供 `PUT /api/admin/security/groups/{id}/policy` API，更新指定组的策略配置
9. THE Hub SHALL 提供 `GET /api/admin/security/users/{email}/effective-policy` API，返回指定用户的 Effective_Policy
10. THE Hub SHALL 提供 `GET /api/admin/security/settings` API，返回系统设置（含 `centralized_security_enabled`、`org_structure_enabled`、`default_group_id`）
11. THE Hub SHALL 提供 `PUT /api/admin/security/settings` API，更新系统设置（含 `centralized_security_enabled`、`org_structure_enabled`、`default_group_id`）
12. 所有安全管理 API SHALL 要求管理员认证（RequireAdmin 中间件）
13. THE Hub SHALL 提供 `GET /api/enroll/group-tree` API（无需管理员认证），返回用户组树结构供注册流程中的部门选择使用
14. WHEN `org_structure_enabled` 为 false 时，THE Hub 的 `GET /api/enroll/group-tree` API SHALL 返回空列表
15. THE Hub SHALL 提供 `PUT /api/admin/security/settings/default-group` API，设置新用户的默认组（接受 `group_id` 参数）
16. WHEN 管理员设置 `default_group_id` 指向一个不存在的 User_Group 时，THE Hub SHALL 返回错误提示


### 需求 10：组织机构开关与注册时部门选择

**用户故事：** 作为 Hub 管理员，我希望能通过组织机构开关控制注册流程是否展示部门选择，以便在需要时让新用户自主选择所属部门。

#### 验收标准

1. THE Hub SHALL 在系统设置中提供 `org_structure_enabled`（布尔值）开关，独立于 `centralized_security_enabled`，默认为 false
2. WHEN `org_structure_enabled` 为 true 时，THE Hub SHALL 在 enrollment 流程的响应中包含 `org_structure_enabled: true` 标记和用户组树结构数据（Enrollment_Group_Tree）
3. WHEN `org_structure_enabled` 为 true 时，THE MaClaw 注册界面 SHALL 弹出树形结构选择器，供用户选择所属部门
4. WHEN `org_structure_enabled` 为 false 时，THE Hub SHALL 在 enrollment 流程的响应中包含 `org_structure_enabled: false` 标记，不包含用户组树数据
5. WHEN `org_structure_enabled` 为 false 时，THE MaClaw 注册界面 SHALL 不展示部门选择步骤
6. THE Hub_Admin SHALL 提供设置 `default_group_id` 的功能，指定新用户在未选择部门时的默认归属组
7. WHEN `org_structure_enabled` 为 true 且管理员已设置 `default_group_id` 时，THE MaClaw 注册界面的部门选择器 SHALL 将 Default_Group 作为默认选中项
8. WHEN `org_structure_enabled` 从 false 切换为 true 时，THE Hub SHALL 记录一条审计日志，包含操作管理员和切换时间
9. WHEN `org_structure_enabled` 从 true 切换为 false 时，THE Hub SHALL 记录一条审计日志，包含操作管理员和切换时间
10. THE Hub_Admin 安全管理页面 SHALL 在顶部显示组织机构开关的当前状态，并提供切换按钮和默认组设置入口
