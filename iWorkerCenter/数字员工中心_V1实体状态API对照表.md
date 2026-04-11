# 数字员工中心：V1 实体、状态、API 对照表

## 1. 目标

本文件用于把数字员工中心 V1 已定义的核心设计进一步压缩成一份实现对照表，方便后续直接开展：
- handler 落地
- service 切分
- repo / store 实现
- ORM 映射
- 状态机实现
- 事件审计实现
- 接口联调
- 测试用例编写

本文件不替代详细设计文档，而是作为实现阶段的“总索引”。

---

## 2. 使用方式

实现时建议按以下顺序查看：
1. 先看“核心实体总表”确认对象边界
2. 再看“状态与动作对照”确认状态机
3. 再看“接口与责任归属”确认 handler/service/repo 分层
4. 最后看“V1 最小实现优先级”安排开发顺序

---

## 3. 核心实体总表

| 业务实体 | 主要表 | 核心作用 | 关键状态/版本字段 | 关键关联 |
|---|---|---|---|---|
| ColleagueProfile | `colleagues` | 数字员工主档 | `status` | `default_model_policy_id` |
| CapabilityPackage | `capability_packages` | 能力包主档 | `status`, `review_status`, `version` | 与员工多对多 |
| ColleagueCapabilityBinding | `colleague_capability_bindings` | 员工与能力包绑定关系 | `binding_status` | `colleague_id`, `capability_package_id` |
| CommunicationMessage | `communication_messages` | 实际协作任务载体 | `status` | `from_colleague_id`, `to_colleague_id`, `workflow_instance_id`, `workflow_step_instance_id` |
| CommunicationMessageEvent | `communication_message_events` | 协作任务审计事件 | `event_type` | `communication_message_id` |
| CollaborationPermission | `collaboration_permissions` | 谁可委托谁 | `status`, `approval_required` | `from_colleague_id`, `to_colleague_id` |
| WorkflowDefinition | `workflow_definitions` | 流程模板主档 | `status`, `version` | 与步骤定义一对多 |
| WorkflowStepDefinition | `workflow_step_definitions` | 流程模板步骤定义 | 无统一 `status`，以模板主档控制 | `workflow_definition_id` |
| WorkflowInstance | `workflow_instances` | 某个事务的一次流程运行 | `status` | `workflow_definition_id`, `current_step_id` |
| WorkflowStepInstance | `workflow_step_instances` | 某个流程步骤的实际执行记录 | `status`, `decision` | `workflow_instance_id`, `delegation_message_id` |
| WorkflowInstanceEvent | `workflow_instance_events` | 流程实例审计事件 | `event_type` | `workflow_instance_id` |
| SharedMemoryEntry | `shared_memory_entries` | 企业共享记忆 | `status`, `review_status`, `version` | 按范围下发 |
| ExperienceEntry | `experience_entries` | 可复用经验 | `status`, `review_status` | `source_colleague_id` |
| ExperienceReference | `experience_references` | 经验被引用记录 | 无 | `experience_entry_id` |
| ModelEndpoint | `model_endpoints` | 模型接入注册 | `status` | 被策略引用 |
| ModelRoutingPolicy | `model_routing_policies` | 模型路由策略 | `status`, `version` | `primary_model_ref` |
| ProxyRequestRecord | `proxy_request_records` | LLM / API 请求审计 | `status` | `routing_policy_id`, `source_colleague_id` |
| SecurityPolicy | `security_policies` | 安全规则 | `status`, `version`, `priority` | 被协作/代理/流程校验引用 |
| SecurityPolicyHitRecord | `security_policy_hit_records` | 规则命中记录 | `result` | `security_policy_id` |
| ConfigBundleVersion | `config_bundle_versions` | 可下发配置包 | `status`, `version` | 被客户端应用记录引用 |
| ConfigBundleTarget | `config_bundle_targets` | 配置下发目标 | 无 | `config_bundle_version_id` |
| ConfigApplyRecord | `config_apply_records` | 客户端应用配置结果 | `apply_status` | `config_bundle_version_id`, `client_id` |

---

## 4. 状态与动作对照

## 4.1 数字员工 `ColleagueProfile`

| 对象 | 状态 | 触发动作 | 主要接口 | 主要写表 |
|---|---|---|---|---|
| ColleagueProfile | `active` | 启用员工 | `POST /admin/colleagues/{id}/status` | `colleagues` |
| ColleagueProfile | `disabled` | 停用员工 | `POST /admin/colleagues/{id}/status` | `colleagues` |

实现备注：
- 管理端创建时可默认进入 `active` 或 `disabled`
- `disabled` 员工不应再成为新协作任务或新流程步骤的可分配对象

## 4.2 能力包 `CapabilityPackage`

| 对象 | 状态字段 | 建议值 | 触发动作 | 主要接口 |
|---|---|---|---|---|
| CapabilityPackage | `status` | `active` / `disabled` / `archived` | 启停、归档 | `POST /admin/capabilities/{id}/status` |
| CapabilityPackage | `review_status` | `pending` / `approved` / `rejected` | 审核 | `POST /admin/capabilities/{id}/review` |

实现备注：
- 审核通过不等于自动绑定到员工
- 已绑定但被 `disabled` 的能力包，客户端侧应不可继续正常使用

## 4.3 协作任务 `CommunicationMessage`

| 当前状态 | 动作 | 下一状态 | 主要接口 | 事件 |
|---|---|---|---|---|
| 无 | create | `standby` | `POST /runtime/collaboration/requests` | `created` |
| `standby` | accept | `accepted` | `POST /runtime/collaboration/requests/{id}/accept` | `accepted` |
| `accepted` | start | `processing` | `POST /runtime/collaboration/requests/{id}/start` | `started` |
| `processing` | complete | `completed` | `POST /runtime/collaboration/requests/{id}/complete` | `completed` |
| `standby` / `accepted` / `processing` | reject | `rejected` | `POST /runtime/collaboration/requests/{id}/reject` | `rejected` |
| `standby` / `accepted` | expire | `expired` | 定时任务/后台处理 | `expired` |
| `standby` / `accepted` / `processing` | cancel | `cancelled` | 后续可补管理接口 | `cancelled` |

实现备注：
- `completed` / `rejected` / `expired` / `cancelled` 为终态
- 如果该任务绑定流程步骤，终态需要同步驱动 `WorkflowStepInstance`

## 4.4 工作流实例 `WorkflowInstance`

| 当前状态 | 动作 | 下一状态 | 主要接口 | 事件 |
|---|---|---|---|---|
| 无 | create | `running` | `POST /runtime/workflows/instances` | `created` |
| `running` | pause | `paused` | `POST /runtime/workflows/instances/{id}/pause` | `paused` |
| `paused` | resume | `running` | `POST /runtime/workflows/instances/{id}/resume` | `resumed` |
| `running` | complete_end | `completed` | 由步骤推进内部触发 | `completed` |
| `running` | reject_end | `rejected` | 由步骤拒绝内部触发 | `rejected` |
| `running` / `paused` | cancel | `cancelled` | `POST /runtime/workflows/instances/{id}/cancel` | `cancelled` |
| `running` / `paused` | timeout | `timeout` | 定时任务/后台处理 | `timeout` |

实现备注：
- 实例状态通常不是直接由外部随意改，而是由步骤推进结果驱动
- `current_step_id` 要与当前活跃步骤一致

## 4.5 工作流步骤实例 `WorkflowStepInstance`

| 当前状态 | 动作 | 下一状态 | 主要接口 | 事件来源 |
|---|---|---|---|---|
| 无 | create | `standby` | 流程创建/推进内部逻辑 | 流程推进 |
| `standby` | accept | `accepted` | `POST /runtime/workflows/steps/{id}/accept` | 运行时 |
| `accepted` | start | `processing` | `POST /runtime/workflows/steps/{id}/start` | 运行时 |
| `processing` | complete | `completed` | `POST /runtime/workflows/steps/{id}/complete` | 运行时 |
| `standby` / `accepted` / `processing` | reject | `rejected` | `POST /runtime/workflows/steps/{id}/reject` | 运行时 |
| `standby` / `accepted` / `processing` | timeout | `timeout` | 超时处理 | 后台任务 |
| `standby` | skip | `skipped` | 条件分支/内部逻辑 | 流程推进 |
| `standby` / `accepted` / `processing` | cancel | `cancelled` | 流程取消内部逻辑 | 流程终止 |

实现备注：
- 若存在 `delegation_message_id`，最简实现是让任务态驱动步骤态
- `decision` 建议在 `complete` 时一并记录

## 4.6 配置下发 `ConfigBundleVersion` / `ConfigApplyRecord`

| 对象 | 当前状态 | 动作 | 下一状态 | 主要接口 |
|---|---|---|---|---|
| ConfigBundleVersion | 无 | create | `draft` | `POST /admin/config-bundles` |
| ConfigBundleVersion | `draft` | publish | `published` | `POST /admin/config-bundles/{id}/publish` |
| ConfigBundleVersion | `published` | rollback | `rolled_back` | `POST /admin/config-bundles/{id}/rollback` |
| ConfigApplyRecord | 无 | create | `pending` | 发布内部逻辑 |
| ConfigApplyRecord | `pending` | apply_success | `success` | `POST /client/config/apply-result` |
| ConfigApplyRecord | `pending` | apply_failed | `failed` | `POST /client/config/apply-result` |
| ConfigApplyRecord | `pending` | skip | `skipped` | `POST /client/config/apply-result` |

---

## 5. 接口与实体映射总表

## 5.1 数字员工

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/colleagues` | 查询员工列表 | `colleagues` | ColleagueQueryService |
| `GET /admin/colleagues/{id}` | 查询员工详情 | `colleagues` | ColleagueQueryService |
| `POST /admin/colleagues` | 创建员工 | `colleagues` | ColleagueService |
| `PUT /admin/colleagues/{id}` | 更新员工 | `colleagues` | ColleagueService |
| `PUT /admin/colleagues/{id}/avatar` | 更新头像 | `colleagues` | ColleagueService |
| `POST /admin/colleagues/{id}/status` | 启停员工 | `colleagues` | ColleagueService |
| `PUT /admin/colleagues/{id}/preferences` | 更新偏好 | `colleagues` | ColleagueService |
| `PUT /admin/colleagues/{id}/collaboration-scope` | 更新协作范围 | `colleagues` | ColleagueService |
| `GET /client/colleagues` | 客户端可见员工 | `colleagues` | ClientColleagueQueryService |
| `GET /client/colleagues/{id}` | 客户端员工详情 | `colleagues` | ClientColleagueQueryService |
| `GET /client/colleagues/{id}/jobs` | 会做的事展示 | `colleagues`, `colleague_capability_bindings`, `capability_packages` | ClientColleagueQueryService |

## 5.2 能力包

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/capabilities/search` | 搜索外部能力包 | 外部源 + 本地映射 | CapabilityImportService |
| `POST /admin/capabilities/import` | 导入能力包 | `capability_packages`, `capability_package_versions` | CapabilityImportService |
| `GET /admin/capabilities` | 查询能力包列表 | `capability_packages` | CapabilityQueryService |
| `GET /admin/capabilities/{id}` | 查询能力包详情 | `capability_packages`, `capability_package_versions` | CapabilityQueryService |
| `POST /admin/capabilities/{id}/review` | 审核 | `capability_packages` | CapabilityReviewService |
| `POST /admin/capabilities/{id}/bind` | 绑定到员工 | `colleague_capability_bindings` | CapabilityBindingService |
| `POST /admin/capabilities/{id}/unbind` | 员工解绑 | `colleague_capability_bindings` | CapabilityBindingService |
| `POST /admin/capabilities/{id}/status` | 更新状态 | `capability_packages` | CapabilityService |
| `POST /admin/capabilities/{id}/upgrade` | 升级版本 | `capability_package_versions`, `capability_packages` | CapabilityUpgradeService |
| `GET /runtime/capabilities` | 当前员工可用能力包 | `colleague_capability_bindings`, `capability_packages` | RuntimeCapabilityQueryService |

## 5.3 协作委托

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `POST /runtime/collaboration/requests` | 创建协作任务 | `communication_messages`, `communication_message_events` | CollaborationService |
| `GET /runtime/collaboration/outgoing` | 查询我发起的协作 | `communication_messages` | CollaborationQueryService |
| `GET /runtime/collaboration/incoming` | 查询我收到的协作 | `communication_messages` | CollaborationQueryService |
| `GET /runtime/collaboration/requests/{id}` | 协作详情 | `communication_messages`, `communication_message_events` | CollaborationQueryService |
| `POST /runtime/collaboration/requests/{id}/accept` | 接受任务 | `communication_messages`, `communication_message_events` | CollaborationLifecycleService |
| `POST /runtime/collaboration/requests/{id}/start` | 开始处理 | `communication_messages`, `communication_message_events` | CollaborationLifecycleService |
| `POST /runtime/collaboration/requests/{id}/complete` | 完成任务 | `communication_messages`, `communication_message_events` | CollaborationLifecycleService |
| `POST /runtime/collaboration/requests/{id}/reject` | 拒绝任务 | `communication_messages`, `communication_message_events` | CollaborationLifecycleService |
| `GET /runtime/tasks/inbox` | 待办箱 | `communication_messages`, `workflow_step_instances` | InboxQueryService |
| `GET /admin/communications` | 管理端协作记录 | `communication_messages` | AdminCommunicationQueryService |
| `GET /admin/communications/{id}` | 管理端协作详情 | `communication_messages`, `communication_message_events` | AdminCommunicationQueryService |

## 5.4 协作权限

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/collaboration-permissions` | 查询权限列表 | `collaboration_permissions` | CollaborationPermissionQueryService |
| `POST /admin/collaboration-permissions` | 新建权限 | `collaboration_permissions` | CollaborationPermissionService |
| `PUT /admin/collaboration-permissions/{id}` | 更新权限 | `collaboration_permissions` | CollaborationPermissionService |
| `POST /admin/collaboration-permissions/{id}/status` | 启停权限 | `collaboration_permissions` | CollaborationPermissionService |
| `GET /runtime/colleagues/{id}/permissions/delegation-targets` | 查询可委托目标 | `collaboration_permissions`, `colleagues` | CollaborationPermissionQueryService |

## 5.5 工作流

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/workflows` | 模板列表 | `workflow_definitions` | WorkflowDefinitionQueryService |
| `GET /admin/workflows/{id}` | 模板详情 | `workflow_definitions`, `workflow_step_definitions` | WorkflowDefinitionQueryService |
| `POST /admin/workflows` | 新建模板 | `workflow_definitions` | WorkflowDefinitionService |
| `PUT /admin/workflows/{id}` | 更新模板 | `workflow_definitions` | WorkflowDefinitionService |
| `POST /admin/workflows/{id}/copy` | 复制模板 | `workflow_definitions`, `workflow_step_definitions` | WorkflowDefinitionService |
| `POST /admin/workflows/{id}/status` | 启停模板 | `workflow_definitions` | WorkflowDefinitionService |
| `POST /admin/workflows/{id}/publish` | 发布版本 | `workflow_definitions` | WorkflowDefinitionService |
| `GET /admin/workflows/{id}/steps` | 步骤列表 | `workflow_step_definitions` | WorkflowStepDefinitionQueryService |
| `POST /admin/workflows/{id}/steps` | 新增步骤 | `workflow_step_definitions` | WorkflowStepDefinitionService |
| `PUT /admin/workflows/{id}/steps/{stepId}` | 更新步骤 | `workflow_step_definitions` | WorkflowStepDefinitionService |
| `DELETE /admin/workflows/{id}/steps/{stepId}` | 删除步骤 | `workflow_step_definitions` | WorkflowStepDefinitionService |
| `POST /admin/workflows/{id}/steps/reorder` | 调整顺序 | `workflow_step_definitions` | WorkflowStepDefinitionService |
| `POST /runtime/workflows/instances` | 启动实例 | `workflow_instances`, `workflow_step_instances`, `communication_messages`, `workflow_instance_events` | WorkflowRuntimeService |
| `GET /runtime/workflows/instances` | 实例列表 | `workflow_instances` | WorkflowInstanceQueryService |
| `GET /runtime/workflows/instances/{id}` | 实例详情 | `workflow_instances`, `workflow_step_instances` | WorkflowInstanceQueryService |
| `GET /runtime/workflows/instances/{id}/steps` | 步骤实例列表 | `workflow_step_instances` | WorkflowInstanceQueryService |
| `POST /runtime/workflows/instances/{id}/pause` | 暂停实例 | `workflow_instances`, `workflow_instance_events` | WorkflowLifecycleService |
| `POST /runtime/workflows/instances/{id}/resume` | 恢复实例 | `workflow_instances`, `workflow_instance_events` | WorkflowLifecycleService |
| `POST /runtime/workflows/instances/{id}/cancel` | 取消实例 | `workflow_instances`, `workflow_step_instances`, `workflow_instance_events` | WorkflowLifecycleService |
| `POST /runtime/workflows/steps/{id}/accept` | 接受步骤 | `workflow_step_instances` + 关联消息 | WorkflowStepLifecycleService |
| `POST /runtime/workflows/steps/{id}/start` | 开始步骤 | `workflow_step_instances` + 关联消息 | WorkflowStepLifecycleService |
| `POST /runtime/workflows/steps/{id}/complete` | 完成步骤并推进下一步 | `workflow_step_instances`, `workflow_instances`, `communication_messages`, `workflow_instance_events` | WorkflowStepLifecycleService |
| `POST /runtime/workflows/steps/{id}/reject` | 拒绝步骤 | `workflow_step_instances`, `workflow_instances`, `communication_messages`, `workflow_instance_events` | WorkflowStepLifecycleService |
| `POST /admin/workflows/steps/{id}/reassign` | 改派 | `workflow_step_instances`, `communication_messages`, `workflow_instance_events` | WorkflowAdminActionService |
| `POST /admin/workflows/steps/{id}/timeout-handle` | 超时处理 | `workflow_step_instances`, `workflow_instances`, `workflow_instance_events` | WorkflowAdminActionService |

## 5.6 模型路由与代理

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/model-routing/models` | 模型列表 | `model_endpoints` | ModelEndpointQueryService |
| `POST /admin/model-routing/models` | 新增模型接入 | `model_endpoints` | ModelEndpointService |
| `PUT /admin/model-routing/models/{id}` | 更新模型接入 | `model_endpoints` | ModelEndpointService |
| `GET /admin/model-routing/policies` | 策略列表 | `model_routing_policies` | ModelRoutingPolicyQueryService |
| `GET /admin/model-routing/policies/{id}` | 策略详情 | `model_routing_policies` | ModelRoutingPolicyQueryService |
| `POST /admin/model-routing/policies` | 新建策略 | `model_routing_policies` | ModelRoutingPolicyService |
| `PUT /admin/model-routing/policies/{id}` | 更新策略 | `model_routing_policies` | ModelRoutingPolicyService |
| `POST /admin/model-routing/default-model` | 设置默认模型 | `model_routing_policies` / 全局配置 | ModelRoutingPolicyService |
| `POST /runtime/model-routing/recommend` | 推荐模型 | `model_routing_policies`, `model_endpoints` | ModelRoutingRecommendService |
| `POST /proxy/llm` | 代理 LLM 请求 | `proxy_request_records` | ProxyLLMService |
| `POST /proxy/api` | 代理外部 API 请求 | `proxy_request_records` | ProxyAPIService |

## 5.7 安全规则

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /admin/security-policies` | 规则列表 | `security_policies` | SecurityPolicyQueryService |
| `GET /admin/security-policies/{id}` | 规则详情 | `security_policies` | SecurityPolicyQueryService |
| `POST /admin/security-policies` | 新建规则 | `security_policies` | SecurityPolicyService |
| `PUT /admin/security-policies/{id}` | 更新规则 | `security_policies` | SecurityPolicyService |
| `POST /admin/security-policies/{id}/publish` | 发布规则 | `security_policies` | SecurityPolicyService |
| `POST /admin/security-policies/{id}/disable` | 停用规则 | `security_policies` | SecurityPolicyService |
| `POST /admin/security-policies/{id}/rollback` | 回滚规则 | `security_policies` | SecurityPolicyService |
| `POST /runtime/security/check` | 运行时校验 | `security_policies`, `security_policy_hit_records` | SecurityCheckService |

## 5.8 配置下发

| 接口 | 读/写对象 | 主要表 | 典型服务 |
|---|---|---|---|
| `GET /client/config/version` | 当前有效版本 | `config_bundle_versions` | ClientConfigQueryService |
| `GET /client/config/bundles` | 待应用配置包 | `config_bundle_versions`, `config_bundle_targets` | ClientConfigQueryService |
| `GET /client/config/bundles/{id}` | 指定配置包详情 | `config_bundle_versions` | ClientConfigQueryService |
| `POST /client/config/apply-result` | 上报应用结果 | `config_apply_records` | ClientConfigApplyService |
| `GET /admin/config-bundles` | 配置包列表 | `config_bundle_versions` | ConfigBundleQueryService |
| `POST /admin/config-bundles` | 创建配置包 | `config_bundle_versions`, `config_bundle_targets` | ConfigBundleService |
| `POST /admin/config-bundles/{id}/publish` | 发布配置包 | `config_bundle_versions`, `config_apply_records` | ConfigBundleService |
| `POST /admin/config-bundles/{id}/rollback` | 回滚配置包 | `config_bundle_versions` | ConfigBundleService |

---

## 6. 关键联动规则

## 6.1 工作流步骤与协作任务联动

| 来源对象 | 目标对象 | 关键字段 | 联动规则 |
|---|---|---|---|
| WorkflowStepInstance | CommunicationMessage | `delegation_message_id` | 步骤落地成实际协作任务 |
| CommunicationMessage | WorkflowStepInstance | `workflow_step_instance_id` | 任务终态驱动步骤终态 |
| WorkflowInstance | WorkflowStepInstance | `current_step_id` | 当前步骤推进时更新实例指针 |

关键要求：
- 一个活跃步骤应最多对应一个当前有效执行任务
- 若做改派，可新建任务并回填新的 `delegation_message_id`
- 不应出现步骤已完成但消息仍长期停在 `standby`

## 6.2 协作权限与安全规则联动

| 校验点 | 校验对象 | 责任服务 |
|---|---|---|
| 是否允许 from -> to 委托 | `collaboration_permissions` | CollaborationPermissionService |
| 是否允许该消息类型/任务类型 | `collaboration_permissions` + `security_policies` | CollaborationService + SecurityCheckService |
| 是否允许分配某步骤处理人 | `collaboration_permissions` + `security_policies` | WorkflowRuntimeService |
| 是否允许代理外发请求 | `security_policies` | ProxyLLMService / ProxyAPIService |

## 6.3 配置包与业务对象联动

| bundle_type | 可能聚合对象 |
|---|---|
| `colleague_profile` | `colleagues` |
| `capability_binding` | `colleague_capability_bindings`, `capability_packages` |
| `shared_memory` | `shared_memory_entries` |
| `model_routing` | `model_routing_policies`, `model_endpoints` |
| `security_policy` | `security_policies` |
| `welcome_page` | 欢迎页配置对象 |

---

## 7. 建议的实现分层

## 7.1 Handler 层
职责：
- 接收 HTTP 请求
- 参数解析与基础校验
- 调用对应 service
- 返回统一响应结构

不建议承担：
- 状态机判断
- 多表事务
- 路由策略计算
- 流程推进逻辑

## 7.2 Service 层
职责：
- 承担业务主逻辑
- 负责状态机判断
- 负责权限/规则校验
- 负责跨表事务协调
- 负责事件写入

建议优先拆成：
- ColleagueService
- CapabilityService / CapabilityBindingService
- CollaborationService / CollaborationLifecycleService
- WorkflowRuntimeService / WorkflowStepLifecycleService
- SecurityCheckService
- ModelRoutingPolicyService / ProxyService
- ConfigBundleService

## 7.3 Repo / Store 层
职责：
- 单表或少量固定查询封装
- 不包含复杂业务决策
- 提供事务内读写能力

建议按实体拆：
- ColleagueRepo
- CapabilityPackageRepo
- CommunicationMessageRepo
- CommunicationMessageEventRepo
- CollaborationPermissionRepo
- WorkflowDefinitionRepo
- WorkflowStepDefinitionRepo
- WorkflowInstanceRepo
- WorkflowStepInstanceRepo
- WorkflowInstanceEventRepo
- SecurityPolicyRepo
- ConfigBundleRepo
- ConfigApplyRecordRepo

---

## 8. 建议的测试覆盖矩阵

## 8.1 单元测试优先对象

| 服务 | 必测点 |
|---|---|
| CollaborationLifecycleService | accept/start/complete/reject 的合法流转 |
| WorkflowRuntimeService | 创建实例时首步任务自动生成 |
| WorkflowStepLifecycleService | 完成步骤后下一步生成；最后一步后实例完成 |
| SecurityCheckService | 允许/拒绝/命中记录生成 |
| ConfigBundleService | publish/rollback 与 apply record 生成 |

## 8.2 集成测试优先链路

| 链路 | 验收点 |
|---|---|
| 新建员工 -> 客户端可见 | 基础主档闭环 |
| 导入能力包 -> 绑定员工 -> 客户端展示 jobs | 能力闭环 |
| 发起协作 -> accept -> start -> complete | 协作闭环 |
| 发起流程 -> 首步任务创建 -> 完成首步 -> 下一步任务创建 | 流程闭环 |
| 发布配置包 -> 客户端拉取 -> 上报成功 | 下发闭环 |
| 代理请求 -> 规则校验 -> 记录审计 | 代理治理闭环 |

---

## 9. V1 最小实现优先级

## 9.1 第一批：必须打通
1. `colleagues`
2. `capability_packages`
3. `colleague_capability_bindings`
4. `communication_messages`
5. `collaboration_permissions`
6. `workflow_definitions`
7. `workflow_step_definitions`
8. `workflow_instances`
9. `workflow_step_instances`
10. `model_routing_policies`
11. `security_policies`
12. `config_bundle_versions`
13. `config_apply_records`

## 9.2 第二批：补审计与增强
1. `communication_message_events`
2. `workflow_instance_events`
3. `proxy_request_records`
4. `security_policy_hit_records`
5. `config_bundle_targets`
6. `shared_memory_entries`
7. `experience_entries`
8. `experience_references`

## 9.3 第一批优先接口
- `POST /admin/colleagues`
- `GET /client/colleagues`
- `POST /admin/capabilities/import`
- `POST /admin/capabilities/{id}/bind`
- `POST /runtime/collaboration/requests`
- `POST /runtime/collaboration/requests/{id}/accept`
- `POST /runtime/collaboration/requests/{id}/complete`
- `POST /runtime/workflows/instances`
- `POST /runtime/workflows/steps/{id}/complete`
- `POST /runtime/workflows/steps/{id}/reject`
- `POST /proxy/llm`
- `GET /client/config/version`
- `POST /client/config/apply-result`

---

## 10. 推荐的开发落地顺序

### 阶段 A：主档与查询
- Colleague
- CapabilityPackage
- CapabilityBinding

### 阶段 B：协作闭环
- CollaborationPermission
- CommunicationMessage
- CommunicationMessageEvent
- Inbox 查询

### 阶段 C：流程闭环
- WorkflowDefinition
- WorkflowStepDefinition
- WorkflowInstance
- WorkflowStepInstance
- WorkflowInstanceEvent

### 阶段 D：治理闭环
- ModelRoutingPolicy
- SecurityPolicy
- ProxyRequestRecord

### 阶段 E：下发闭环
- ConfigBundleVersion
- ConfigBundleTarget
- ConfigApplyRecord

---

## 11. 一句话总结

**V1 实现时应围绕“实体表清晰、状态机统一、接口责任明确、步骤任务联动稳定”四个原则推进，并优先打通数字员工、协作委托、工作流推进、配置下发这四条最小闭环链路。**
