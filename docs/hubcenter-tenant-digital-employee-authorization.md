# HubCenter 租户级数字员工数授权设计

## 背景

HubCenter 负责给 Hub 下发数字员工数授权。旧模型把授权挂在 Hub 上：一个 Hub 只有一份 `digital_employee_authorization`，Hub 内所有租户共享同一数字员工额度。

多租户 Hub 场景下，这会导致两个问题：

- 一个租户购买或续费后，其他租户可能共享同一 Hub 级额度。
- HubCenter 管理界面无法按租户展示、续费和审计数字员工数。

目标是把数字员工数授权从 Hub 级改为租户级：HubCenter 对某个 `hub_id + tenant_id` 发放额度，Hub 在注册、审批、发现数字员工时按当前租户校验。

## 目标行为

- HubCenter 管理员为指定 Hub 的指定租户配置数字员工授权。
- 授权字段仍使用 `quota / enabled / expires_at / active / reason`。
- Hub 心跳从 HubCenter 获取 `digital_employee_authorizations`，其 key 为 `tenant_id`。
- Hub 内数字员工注册、自动审批、手动审批、发现列表都按租户读取授权。
- 非默认租户没有租户级授权时，不允许使用 Hub 级授权兜底。
- 旧 Hub 级 `digital_employee_authorization` 只保留给默认租户或无租户上下文的遗留路径。

## 接口约定

### 更新授权

`POST /api/admin/hubs/{id}/digital-employee-authorization`

请求体：

```json
{
  "tenant_id": "tenant_a",
  "quota": 5,
  "years": 1,
  "enabled": true,
  "start_date": "2026-05-28"
}
```

规则：

- `tenant_id` 必填，管理入口只允许更新租户级授权。
- 旧 Hub 级授权只保留内部兼容读取，不再作为非默认租户兜底，也不再通过管理接口更新。
- `quota` 只能增加，不能降低。
- 启用授权时 `quota > 0` 且 `years >= 1`。
- 禁用授权时保留历史额度和到期时间，但 `active=false`。
- 如果租户级授权存储不可用，接口返回 `503 DIGITAL_EMPLOYEE_AUTHORIZATION_STORE_UNAVAILABLE`，避免误写或回落到 Hub 级授权。

### Hub 心跳响应

HubCenter 对 Hub 心跳返回：

```json
{
  "ok": true,
  "status": "online",
  "digital_employee_authorizations": {
    "tenant_a": {
      "quota": 5,
      "enabled": true,
      "expires_at": "2027-05-28T00:00:00Z",
      "active": true
    }
  }
}
```

如存在遗留 Hub 级授权，可继续返回 `digital_employee_authorization`，但 Hub 只给默认租户或无租户上下文使用。

## 数据模型

当前实现把租户授权存入 HubCenter `system_settings`：

```text
key: tenant_digital_employee_authorizations:{hub_id}
value: { "tenant_a": DigitalEmployeeAuthorization }
```

旧字段 `hub_instances.digital_employee_quota / digital_employee_authorization_enabled / digital_employee_authorization_expires_at` 保留兼容读取，不再作为非默认租户授权来源。

## Hub 侧生效点

- 机器鉴权得到 `MachinePrincipal.TenantID`。
- VE 相关接口使用租户 scoped settings 读取授权。
- `center.LoadDigitalEmployeeAuthorizationForTenant` 对非默认租户只读取 `DigitalEmployeeAuthorizations[tenant_id]`。
- 如果当前租户没有授权，返回 nil；数字员工注册、发现、审批额度逻辑按未授权处理。

## 验收用例

- HubCenter 给 `tenant_a` 授权 2 个数字员工，Hub 心跳后 `tenant_a` 可注册，`tenant_b` 不可注册。
- Hub 级授权存在但 `tenant_a` 没有租户授权时，`tenant_a` 注册返回 `VE_AUTHORIZATION_INACTIVE`。
- 租户授权自动审批只在该租户自己的 registry 内按 quota 生效。
- HubCenter 列表接口展示 Hub 下各租户及 `digital_employee_authorizations`，但过滤遗留 Hub 级授权的空 key。
- 旧默认租户或无租户上下文仍可读取遗留 Hub 级授权，避免老环境瞬时失效。
- 只有注册策略、尚无用户盘点数据的租户也应出现在 HubCenter 列表中，管理员可先按 `tenant_id` 发放授权。
- 服务层和管理 HTTP 接口都拒绝空 `tenant_id` 的新增/续费/禁用操作，避免重新写入 Hub 级授权。
