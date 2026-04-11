# 数字员工中心：API 清单与路由规划 v1

## 1. 目标

本文件用于整理数字员工中心 V1 的核心 API 清单与路由规划，明确：
- 哪些对象需要暴露接口
- 管理端、DiWorker、数字员工运行时分别会调用哪些接口
- 协作委托与工作流如何映射到接口
- V1 接口边界如何控制

本文件是在《服务划分与接口草案 v1》基础上的收敛版，偏向后续实现落地。

---

## 2. 设计原则

### 2.1 按业务对象分组
接口优先按业务对象和服务边界分组，而不是按技术模块随意拆分。

### 2.2 管理端接口与运行时接口分层
同一个对象可以有：
- 管理端配置接口
- 客户端/运行时查询接口
- 审计/状态接口

### 2.3 流程接口不绕开协作接口
工作流推进到某一步时，仍应通过中心生成实际协作任务，因此流程接口与通讯接口必须可关联。

### 2.4 V1 优先清晰，不追求过度抽象
先把 REST 风格的主路径定清楚，复杂批处理和高级查询能力后续补充。

### 2.5 优先复用 corelib
如果任务状态机、消息结构、规则判断、配置版本等已有公共实现，应优先复用 `corelib/`。

---

## 3. 调用方划分

建议把调用方分为三类：

### 3.1 管理端
用于：
- 管理数字员工
- 管理能力包
- 管理流程模板
- 管理模型策略
- 管理安全规则
- 查看审计和运行状态

### 3.2 DiWorker 客户端
用于：
- 拉取数字员工列表和展示信息
- 拉取配置版本与变更包
- 发起任务
- 查询历史任务和协作结果
- 通过中心代理发起 LLM / API 请求

### 3.3 数字员工运行时 / 协作执行端
用于：
- 查询可协作员工
- 发起协作请求
- 拉取待处理任务
- 接受 / 拒绝 / 完成任务
- 执行流程步骤

---

## 4. 路由分组建议

建议统一采用以下一级路由分组：

- `/admin/colleagues`
- `/admin/capabilities`
- `/admin/workflows`
- `/admin/model-routing`
- `/admin/security-policies`
- `/admin/config-bundles`
- `/admin/communications`
- `/admin/audit`

- `/runtime/colleagues`
- `/runtime/collaboration`
- `/runtime/workflows`
- `/runtime/tasks`
- `/runtime/config`

- `/client/colleagues`
- `/client/welcome`
- `/client/config`
- `/client/history`

- `/proxy/llm`
- `/proxy/api`

V1 不强制一定要加 `admin/runtime/client` 前缀，但建议保留这种边界，避免后续混乱。

---

## 5. 数字员工接口

## 5.1 管理端接口

### 查询数字员工列表
- `GET /admin/colleagues`

建议支持筛选：
- `status`
- `role_type`
- `keyword`

### 查询数字员工详情
- `GET /admin/colleagues/{id}`

### 新建数字员工
- `POST /admin/colleagues`

### 更新数字员工
- `PUT /admin/colleagues/{id}`

### 更新头像
- `PUT /admin/colleagues/{id}/avatar`

### 启用 / 停用数字员工
- `POST /admin/colleagues/{id}/status`

### 更新工作偏好与协作偏好
- `PUT /admin/colleagues/{id}/preferences`

### 配置协作范围
- `PUT /admin/colleagues/{id}/collaboration-scope`

---

## 5.2 运行时 / 客户端接口

### 获取可见数字员工列表
- `GET /client/colleagues`

### 获取数字员工详情
- `GET /client/colleagues/{id}`

### 获取某员工的会做的事
- `GET /client/colleagues/{id}/jobs`

### 获取某员工可用能力包摘要
- `GET /runtime/colleagues/{id}/capabilities`

### 查询当前员工可协作对象
- `GET /runtime/colleagues/{id}/delegation-candidates`

建议支持参数：
- `task_type`
- `message_type`
- `keyword`

---

## 6. 能力包接口

## 6.1 管理端接口

### 搜索 hubcenter 能力包
- `GET /admin/capabilities/search?source=hubcenter&query=...`

### 导入能力包
- `POST /admin/capabilities/import`

### 查询能力包列表
- `GET /admin/capabilities`

### 查询能力包详情
- `GET /admin/capabilities/{id}`

### 审核能力包
- `POST /admin/capabilities/{id}/review`

### 绑定到数字员工
- `POST /admin/capabilities/{id}/bind`

### 从数字员工解绑
- `POST /admin/capabilities/{id}/unbind`

### 更新能力包状态
- `POST /admin/capabilities/{id}/status`

### 升级能力包版本
- `POST /admin/capabilities/{id}/upgrade`

---

## 6.2 运行时接口

### 查询当前员工可用能力包
- `GET /runtime/capabilities`

### 查询能力包详情摘要
- `GET /runtime/capabilities/{id}`

---

## 7. 协作委托接口

## 7.1 管理端接口

### 查询协作记录列表
- `GET /admin/communications`

建议支持筛选：
- `status`
- `from_colleague_id`
- `to_colleague_id`
- `message_type`
- `task_type`
- `workflow_instance_id`

### 查询协作详情
- `GET /admin/communications/{id}`

### 导出审计记录
- `GET /admin/communications/{id}/audit-export`

---

## 7.2 运行时接口

### 发起协作请求
- `POST /runtime/collaboration/requests`

请求体建议包含：
- `task_id`
- `from_colleague_id`
- `to_colleague_id`
- `message_type`
- `subject`
- `content`
- `context_summary`
- `attachment_refs`
- `delegated_by_colleague_id`

### 查询我发起的协作
- `GET /runtime/collaboration/outgoing`

### 查询我收到的协作
- `GET /runtime/collaboration/incoming`

### 查询协作详情
- `GET /runtime/collaboration/requests/{id}`

### 接受协作任务
- `POST /runtime/collaboration/requests/{id}/accept`

### 拒绝协作任务
- `POST /runtime/collaboration/requests/{id}/reject`

请求体建议包含：
- `reason`
- `detail`

### 标记开始执行
- `POST /runtime/collaboration/requests/{id}/start`

### 提交处理结果
- `POST /runtime/collaboration/requests/{id}/complete`

请求体建议包含：
- `result_summary`
- `result_payload_ref`

### 查询当前员工待处理任务
- `GET /runtime/tasks/inbox`

建议支持筛选：
- `task_kind=delegation|workflow`
- `status`
- `timeout_only`

---

## 8. 协作权限接口

### 查询委托权限列表
- `GET /admin/collaboration-permissions`

### 新建委托权限
- `POST /admin/collaboration-permissions`

### 更新委托权限
- `PUT /admin/collaboration-permissions/{id}`

### 启用 / 停用委托权限
- `POST /admin/collaboration-permissions/{id}/status`

### 查询某员工可委托哪些员工
- `GET /runtime/colleagues/{id}/permissions/delegation-targets`

---

## 9. 工作流接口

## 9.1 管理端：流程模板接口

### 查询流程模板列表
- `GET /admin/workflows`

建议支持筛选：
- `status`
- `category`
- `keyword`

### 查询流程模板详情
- `GET /admin/workflows/{id}`

### 新建流程模板
- `POST /admin/workflows`

### 更新流程模板
- `PUT /admin/workflows/{id}`

### 复制流程模板
- `POST /admin/workflows/{id}/copy`

### 启用 / 停用流程模板
- `POST /admin/workflows/{id}/status`

### 发布新版本
- `POST /admin/workflows/{id}/publish`

---

## 9.2 管理端：流程步骤接口

### 查询流程步骤列表
- `GET /admin/workflows/{id}/steps`

### 新增流程步骤
- `POST /admin/workflows/{id}/steps`

### 更新流程步骤
- `PUT /admin/workflows/{id}/steps/{stepId}`

### 删除流程步骤
- `DELETE /admin/workflows/{id}/steps/{stepId}`

### 调整步骤顺序
- `POST /admin/workflows/{id}/steps/reorder`

---

## 9.3 运行时：流程实例接口

### 发起流程实例
- `POST /runtime/workflows/instances`

请求体建议包含：
- `workflow_definition_id`
- `biz_object_type`
- `biz_object_id`
- `title`
- `initiator_id`
- `context_summary`

### 查询流程实例列表
- `GET /runtime/workflows/instances`

### 查询流程实例详情
- `GET /runtime/workflows/instances/{id}`

### 查询流程步骤实例列表
- `GET /runtime/workflows/instances/{id}/steps`

### 取消流程实例
- `POST /runtime/workflows/instances/{id}/cancel`

### 暂停流程实例
- `POST /runtime/workflows/instances/{id}/pause`

### 恢复流程实例
- `POST /runtime/workflows/instances/{id}/resume`

---

## 9.4 流程步骤推进接口

### 接受当前步骤任务
- `POST /runtime/workflows/steps/{id}/accept`

### 拒绝当前步骤任务
- `POST /runtime/workflows/steps/{id}/reject`

请求体建议包含：
- `reason`
- `detail`
- `reject_action`

### 开始执行步骤
- `POST /runtime/workflows/steps/{id}/start`

### 完成步骤
- `POST /runtime/workflows/steps/{id}/complete`

请求体建议包含：
- `result_summary`
- `result_payload_ref`
- `decision`

### 人工改派步骤处理人
- `POST /admin/workflows/steps/{id}/reassign`

### 超时处理
- `POST /admin/workflows/steps/{id}/timeout-handle`

---

## 9.5 流程与协作任务打通规则

当调用以下接口后：
- `POST /runtime/workflows/instances`
- `POST /runtime/workflows/steps/{id}/complete`

中心需要在内部触发：
1. 计算下一步
2. 校验权限与安全规则
3. 生成对应 `CommunicationMessage`
4. 将该任务放入目标员工任务列表
5. 在 `WorkflowStepInstance` 中回填 `delegation_message_id`

所以流程接口和协作接口在实现上必须互相可追踪。

---

## 10. 模型调度与代理接口

## 10.1 管理端接口

### 查询模型列表
- `GET /admin/model-routing/models`

### 新增模型接入
- `POST /admin/model-routing/models`

### 更新模型接入
- `PUT /admin/model-routing/models/{id}`

### 查询路由策略列表
- `GET /admin/model-routing/policies`

### 查询路由策略详情
- `GET /admin/model-routing/policies/{id}`

### 新建路由策略
- `POST /admin/model-routing/policies`

### 更新路由策略
- `PUT /admin/model-routing/policies/{id}`

### 设置默认模型
- `POST /admin/model-routing/default-model`

---

## 10.2 运行时 / 客户端接口

### 查询某任务推荐模型
- `POST /runtime/model-routing/recommend`

### 统一代理 LLM 请求
- `POST /proxy/llm`

### 统一代理外部 API 请求
- `POST /proxy/api`

建议附带：
- `source_client_id`
- `source_colleague_id`
- `task_id`
- `routing_policy_id`
- `request_summary`

---

## 11. 安全规则接口

### 查询安全规则列表
- `GET /admin/security-policies`

### 查询安全规则详情
- `GET /admin/security-policies/{id}`

### 新建安全规则
- `POST /admin/security-policies`

### 更新安全规则
- `PUT /admin/security-policies/{id}`

### 发布规则
- `POST /admin/security-policies/{id}/publish`

### 停用规则
- `POST /admin/security-policies/{id}/disable`

### 回滚规则
- `POST /admin/security-policies/{id}/rollback`

### 运行时规则检查
- `POST /runtime/security/check`

适用于：
- 协作委托前
- 流程分配前
- 能力调用前
- 外发前

---

## 12. 配置下发接口

### 获取最新配置版本
- `GET /client/config/version`

### 拉取变更包列表
- `GET /client/config/bundles`

### 拉取指定配置包详情
- `GET /client/config/bundles/{id}`

### 上报客户端应用结果
- `POST /client/config/apply-result`

### 管理端查询配置包列表
- `GET /admin/config-bundles`

### 管理端创建配置包
- `POST /admin/config-bundles`

### 管理端发布配置包
- `POST /admin/config-bundles/{id}/publish`

### 管理端回滚配置包
- `POST /admin/config-bundles/{id}/rollback`

---

## 13. Welcome 页与历史任务接口

### 获取 Welcome 页配置
- `GET /client/welcome`

### 获取常用任务入口
- `GET /client/welcome/pinned-tasks`

### 获取历史任务列表
- `GET /client/history/tasks`

建议支持筛选：
- `keyword`
- `colleague_id`
- `status`
- `from_date`
- `to_date`

### 获取历史任务详情
- `GET /client/history/tasks/{id}`

---

## 14. 审计与统计接口

### 查询审计记录
- `GET /admin/audit/events`

### 查询流程审计
- `GET /admin/audit/workflows`

### 查询协作审计
- `GET /admin/audit/communications`

### 查询模型调用统计
- `GET /admin/audit/model-usage`

### 查询规则命中统计
- `GET /admin/audit/security-hits`

### 导出报表
- `GET /admin/audit/export`

---

## 15. V1 最小必做接口集

如果只做最小闭环，建议第一批先实现：

### 15.1 数字员工
- `GET /admin/colleagues`
- `GET /admin/colleagues/{id}`
- `POST /admin/colleagues`
- `PUT /admin/colleagues/{id}`
- `GET /client/colleagues`

### 15.2 协作委托
- `GET /runtime/colleagues/{id}/delegation-candidates`
- `POST /runtime/collaboration/requests`
- `GET /runtime/tasks/inbox`
- `POST /runtime/collaboration/requests/{id}/accept`
- `POST /runtime/collaboration/requests/{id}/reject`
- `POST /runtime/collaboration/requests/{id}/complete`

### 15.3 工作流
- `GET /admin/workflows`
- `POST /admin/workflows`
- `POST /admin/workflows/{id}/steps`
- `POST /runtime/workflows/instances`
- `GET /runtime/workflows/instances/{id}`
- `POST /runtime/workflows/steps/{id}/accept`
- `POST /runtime/workflows/steps/{id}/reject`
- `POST /runtime/workflows/steps/{id}/complete`

### 15.4 模型代理
- `POST /proxy/llm`
- `POST /proxy/api`

### 15.5 配置同步
- `GET /client/config/version`
- `GET /client/config/bundles`
- `POST /client/config/apply-result`

---

## 16. 一句话总结

**数字员工中心 V1 的 API 设计应围绕“数字员工管理、协作委托、流程编排、统一代理、规则治理、配置下发”六条主线展开，并确保工作流步骤最终都能落成受控协作任务。**
