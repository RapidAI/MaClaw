# MaClawSrv API 对接手册（中文）

本文面向需要直接调�?`MaClawSrv` �?AI 工具、桌面客户端、自动化平台、控制平面和其它外部程序集成方。目标是让接入方不必反复阅读源码，也能快速理解接口分层、鉴权方式、典型调用顺序和各主�?API 的用途�?
## 1. MaClawSrv 提供什�?
`MaClawSrv` �?Maclaw 暴露为多租户、多用户�?REST Agent 服务�?
核心特点�?
- 数据隔离层级�?`tenant -> user`
- 同一�?user 下可以同时运行多�?instance
- 同一�?user 的所�?instance 共享同一份配置、记忆、skills、MCP 状态和长期数据
- 核心运行逻辑复用 `corelib/agentservice` 与共�?`corelib/agent`
- `MaClawSrv` 是纯 agent 服务，不负责 coding session 编排
- skill �?MCP 管理能力也通过 REST 暴露

## 2. 基础地址与传�?
默认监听地址�?
```text
http://127.0.0.1:18080
```

健康检查：

```http
GET /health
```

返回示例�?
```json
{
  "status": "ok"
}
```

传输建议�?
- 明文 HTTP 默认只建议用于本�?loopback
- 远程部署请配�?`MACLAW_TLS_CERT_FILE` �?`MACLAW_TLS_KEY_FILE`
- 不要在不安全的远�?HTTP 链路上传�?admin secret、API secret �?bearer token
- 机器可读接口描述可通过 `GET /openapi.json` �?`GET /api/v1/openapi.json` 获取

## 3. 服务端环境变�?
安全相关必配�?
- `MACLAW_ADMIN_SECRET`：管理端密钥，建议至�?24 字符
- `MACLAW_TOKEN_SECRET`：Bearer Token 签名密钥，建议至�?32 字符

常用运行变量�?
- `MACLAW_HTTP_ADDR`：监听地址，默�?`:18080`
- `MACLAW_DATA_ROOT`：服务数据根目录，默�?`~/.maclaw_srv`
- `MACLAW_TLS_CERT_FILE`：TLS 证书路径
- `MACLAW_TLS_KEY_FILE`：TLS 私钥路径
- `MACLAW_ALLOW_INSECURE_HTTP`：设�?`true` 时允许非 loopback 明文 HTTP
- `MACLAW_CREDENTIAL_PEPPER`：凭证摘要额�?pepper，可�?
与本�?bash 相关的变量：

- `MACLAW_ENABLE_LOCAL_BASH`
- `MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER`
- `MACLAW_LOCAL_BASH_TENANT_ID`
- `MACLAW_LOCAL_BASH_USER_ID`

说明�?
- 本地 bash 是高敏感能力，多租户部署一般不要开启，除非外层已有 OS 或容器级隔离

## 4. 鉴权模型

`MaClawSrv` 分为两层鉴权：管理端 API 和用户端 API�?
### 4.1 管理�?API

管理端通过固定 Header 鉴权�?
```http
X-MaClaw-Admin-Secret: <admin-secret>
```

主要用于�?
- tenant 管理
- user 管理
- credential 管理
- audit / overview / dashboard / alerts 等控制平面接�?
### 4.2 用户�?API

用户端通过 Bearer Token 鉴权�?
标准流程�?
1. 管理端先创建 tenant、user、credential
2. 客户端用 `api_key + api_secret` 交换 token
3. 后续所有用户态接口都使用 `Authorization: Bearer <token>`

换取 token�?
```http
POST /api/v1/auth/token
Content-Type: application/json
```

请求体：

```json
{
  "api_key": "ak_xxx",
  "api_secret": "secret_xxx"
}
```

返回示例�?
```json
{
  "access_token": "...",
  "token_type": "Bearer",
  "expires_at": "2026-04-23T20:30:00Z",
  "principal": {
    "tenant_id": "tenant_xxx",
    "user_id": "user_xxx"
  }
}
```

说明�?
- `api_key` 是凭证标�?- `api_secret` 仅客户端保存，服务端不会再次明文返回
- `access_token` 用于后续 Bearer 调用
- `expires_at` �?RFC3339 时间

获取当前用户�?
```http
GET /api/v1/me
Authorization: Bearer <token>
```

## 5. 推荐接入顺序

对于 AI 工具、桌面客户端或自动化调用方，推荐按下面顺序接入：

1. `POST /api/v1/auth/token`
2. `GET /api/v1/me`
3. `GET /api/v1/config/schema`
4. `GET /api/v1/config`
5. `POST /api/v1/config/validate`
6. 如配置不完整�?`PUT /api/v1/config`
7. 如需真实连通性校验则 `POST /api/v1/config/test`
8. `POST /api/v1/instances`
9. `GET /api/v1/instances/{instanceId}/capabilities`
10. `POST /api/v1/instances/{instanceId}/messages`
11. 轮询 `GET /api/v1/instances/{instanceId}/runs/{runId}` 或查看消息历�?
这样做的好处是，能够把“配置问题”和“运行问题”拆开处理，UI 也更容易给出准确提示�?
## 6. 通用 Header

用户态请求：

```http
Authorization: Bearer <token>
Accept: application/json
Content-Type: application/json
```

管理态请求：

```http
X-MaClaw-Admin-Secret: <admin-secret>
Accept: application/json
Content-Type: application/json
```

## 7. 错误模型

常见错误结构�?
```json
{
  "error": "..."
}
```

配置错误可能附带�?
```json
{
  "error": "invalid config",
  "config_validation": {
    "valid": false,
    "issues": [
      {
        "key": "maclaw_llm_key",
        "message": "is required"
      }
    ]
  }
}
```

推荐按状态码处理�?
- `400`：请求字段错误、配置不完整、前置条件不满足
- `401`：鉴权失�?- `404`：tenant、user、instance、session、run、skill、MCP server 不存�?- `409`：状态冲突，例如归档 session 不允许继续写�?- `429`：登录限流、配额超限或相关节流
- `502`：下游执行器或外部依赖失�?- `500`：服务端错误

## 8. 分页模型

多个列表接口支持游标分页�?
查询参数�?
- `limit`：默�?`100`，最�?`500`
- `before`：游标�?
典型返回�?
```json
{
  "items": [],
  "limit": 100,
  "has_more": true,
  "next_before": "2026-04-23T18:00:00Z"
}
```

说明�?
- `items`：当前页数据
- `limit`：实际页大小
- `has_more`：是否还有下一�?- `next_before`：下次请求应携带�?`before`

当前支持分页的接口：

- `GET /api/v1/admin/tenants`��֧�� `status`��`name` ����
- `GET /api/v1/admin/tenants/{tenantId}/users`��֧�� `status`��`name`��`email` ����
- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
- `GET /api/v1/admin/audit-events`
- `GET /api/v1/mcp/servers`
- `GET /api/v1/skills`
- `GET /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `GET /api/v1/instances/{instanceId}/runs`

补充�?
- 大多数列表接口使用时间戳游标
- `GET /api/v1/skills` 使用 skill 名称游标，而不是时间戳

## 9. 管理�?API

### 9.1 总览类接�?
- `GET /api/v1/admin/overview`：返回全局聚合计数
- `GET /api/v1/admin/dashboard`：返�?overview、近期审计事件和趋势聚合
- `GET /api/v1/admin/alerts`：返回未就绪 instance、等待用户输入的 run、失�?run 等告警数�?- `GET /api/v1/admin/audit-events`：返回审计事件列�?
`/api/v1/admin/alerts` 还支持：

- `tenant_id`
- `user_id`
- `kind`
- `since`
- `limit`

`/api/v1/admin/audit-events` 还支持：

- `tenant_id`
- `user_id`
- `action`
- `resource_type`
- `limit`
- `before`

### 9.2 Tenant 管理

- `GET /api/v1/admin/tenants`��֧�� `status`��`name` ����
- `POST /api/v1/admin/tenants`
- `GET /api/v1/admin/tenants/{tenantId}`
- `GET /api/v1/admin/tenants/{tenantId}/summary`
- `PATCH /api/v1/admin/tenants/{tenantId}`

目前 `PATCH` 可更新的重点字段包括�?
- `name`
- `description`
- `status`
- `metadata`
- `max_instances`
- `max_sessions`
- `max_messages`
- `max_runs`

说明�?
- `status` 支持 `active` �?`disabled`
- quota 值为 `0` 表示无限�?- tenant summary 会返�?tenant 汇总和�?user 的聚合数�?
### 9.3 User 管理

- `GET /api/v1/admin/tenants/{tenantId}/users`��֧�� `status`��`name`��`email` ����
- `POST /api/v1/admin/tenants/{tenantId}/users`
- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}`
- `PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}`

`PATCH` 常见可更新字段：

- `name`
- `email`
- `status`
- `metadata`
- `max_instances`
- `max_sessions`
- `max_messages`
- `max_runs`

说明�?
- user 被禁用后不能再签发新 token
- 已有 bearer token 也会在校验时被拒�?
### 9.4 Credential 管理

- `GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
- `POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials`
- `DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}`

建议理解为：

- 创建时返回一次明�?secret
- 列表接口主要用于管理展示，不应指望再次拿�?secret
- credential �?revoke 后，不能再用于换取新 token
- 新签发的 bearer token �?issuing credential 绑定

## 10. 用户配置 API

### 10.1 配置 schema

```http
GET /api/v1/config/schema
Authorization: Bearer <token>
```

用途：

- 让前端或外部调用方知道当前需要哪些配置项
- 避免把配置字段写死在客户�?
### 10.2 获取配置

```http
GET /api/v1/config
Authorization: Bearer <token>
```

### 10.3 更新配置

```http
PUT /api/v1/config
Authorization: Bearer <token>
Content-Type: application/json
```

通常可以提交�?
```json
{
  "app_config": {
    "llm_endpoint": "openai",
    "llm_api_key": "...",
    "llm_model": "gpt-4.1"
  }
}
```

也支持直接提交原�?`AppConfig` JSON�?
### 10.4 校验配置

```http
POST /api/v1/config/validate
Authorization: Bearer <token>
```

说明�?
- �?body 表示校验已保存配�?- 也可以提交候选配置做 dry-run 校验

### 10.5 测试配置

```http
POST /api/v1/config/test
Authorization: Bearer <token>
```

说明�?
- �?body 表示测试已保存配�?- 也可以提交候选配置做真实连通性测�?
## 11. Instance API

### 11.1 实例列表与创�?
- `GET /api/v1/instances`
- `POST /api/v1/instances`

说明�?
- 创建 instance 前会检查配置是否满足启动条�?- 如果缺少必要参数，返回中会附�?`config_validation.issues`
- instance 是逻辑运行入口，不是独立用户数据副�?
### 11.2 实例详情与更�?
- `GET /api/v1/instances/{instanceId}`
- `PATCH /api/v1/instances/{instanceId}`
- `DELETE /api/v1/instances/{instanceId}`
- `GET /api/v1/instances/{instanceId}/capabilities`
- `GET /api/v1/instances/{instanceId}/summary`
- `GET /api/v1/instances/{instanceId}/bootstrap`

补充�?
- instance 返回中会包含 `ready`、`ready_reason`、`readiness`
- `PATCH` 主要用于更新名称、描述、metadata 等可变字�?- `bootstrap` 用于给未�?runner 或外部宿主读取启动所需上下�?
### 11.3 实例动作接口

- `POST /api/v1/instances/{instanceId}/stop`
- `POST /api/v1/instances/{instanceId}/resume`
- `POST /api/v1/instances/{instanceId}/refresh-readiness`

说明�?
- `stop` 会让实例进入停止态，不再接受新消�?- `resume` 会重新校验配置，并在通过时恢�?ready 状�?- `refresh-readiness` 只刷新就绪性，不改变预期生命周期状�?
## 12. Session / Message / Run API

### 12.1 Session

- `GET /api/v1/instances/{instanceId}/sessions`
- `POST /api/v1/instances/{instanceId}/sessions`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `DELETE /api/v1/instances/{instanceId}/sessions/{sessionId}`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore`

说明�?
- 默认列表不返�?archived session
- 需要时可带 `include_archived=true`
- archived session 可直接读取，但不能继续发送消�?
### 12.2 Message

- `POST /api/v1/instances/{instanceId}/messages`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`

`POST /api/v1/instances/{instanceId}/messages` 是一步式入口�?
- 如果提供 `session_id`，消息发到现�?session
- 如果不提�?`session_id`，服务会创建�?session
- 返回中通常包含 `session`、`run` �?assistant `message`

消息列表支持的过滤参数：

- `role`
- `since`
- `until`
- `limit`
- `before`

### 12.3 Run

- `GET /api/v1/instances/{instanceId}/runs`
- `GET /api/v1/instances/{instanceId}/runs/{runId}`
- `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
- `POST /api/v1/instances/{instanceId}/runs/{runId}/cancel`

说明�?
- `/events` �?SSE
- run 列表支持 `status`、`session_id`、`response_source`、`waiting_for_user`、`limit`、`before`
- `before` 使用 `started_at` 作为游标

如果共享 agent loop 触发 `ask_user`，session 会进入等待态，下一条用户消息会被当作该问题的回答�?
## 13. Usage Summary API

```http
GET /api/v1/usage/summary
Authorization: Bearer <token>
```

用途：

- 返回当前 bearer token 所�?user 的汇总使用情�?- 包括 instance、session、message、run、run 状态分布、最后活跃时间等

## 14. Skill API

当前暴露�?skill 能力包括�?
- `GET /api/v1/skills`
- `POST /api/v1/skills/search`
- `POST /api/v1/skills/install`
- `POST /api/v1/skills/import`
- `GET /api/v1/skills/{skillName}`
- `DELETE /api/v1/skills/{skillName}`
- `GET /api/v1/skills/{skillName}/export`
- `POST /api/v1/skills/{skillName}/validate`
- `POST /api/v1/skills/{skillName}/improve`
- `POST /api/v1/skills/{skillName}/upload`
- `GET /api/v1/skill-uploads/{submissionId}`
- `GET /api/v1/skill-market/account`

适用场景�?
- 搜索 skill 市场
- 安装 skill
- 导入本地或外�?skill
- 校验 skill �?- 基于现有 skill �?improve
- 上传 skill
- 查询上传状�?
## 15. MCP API

当前暴露�?MCP 能力包括�?
- `GET /api/v1/mcp/servers`
- `POST /api/v1/mcp/servers`
- `GET /api/v1/mcp/servers/{serverId}`
- `PATCH /api/v1/mcp/servers/{serverId}`
- `DELETE /api/v1/mcp/servers/{serverId}`
- `POST /api/v1/mcp/servers/{serverId}/start`
- `POST /api/v1/mcp/servers/{serverId}/stop`
- `POST /api/v1/mcp/servers/{serverId}/health-check`
- `GET /api/v1/mcp/servers/{serverId}/tools`

适用场景�?
- 创建或更�?MCP server 配置
- 启停 MCP server
- 做健康检�?- 列出可用 MCP tools

## 16. 接口风格约定

对接时建议明确这套约定：

- 资源型操作用 `GET / POST / PATCH / PUT / DELETE`
- 动作型操作用显式 action path，例�?`/stop`、`/resume`、`/archive`、`/restore`、`/health-check`
- 管理面和用户面鉴权头不同，不要混�?- 机器可读真相�?OpenAPI 为准，手册更偏集成解�?
## 17. 最小接入清�?
一个最小可用客户端至少需要支持：

- 管理端创�?tenant / user / credential
- 用户�?token 交换
- 读取配置 schema
- 更新配置
- 校验配置
- 创建 instance
- 发消�?- 读取 run �?SSE 事件
- 读取 session / message 历史

## 18. 当前已知缺口

虽然主链路已经可用，但如果要�?`MaClawSrv` 当作成熟控制平面，当前还存在这些缺口�?
- 还没�?tenant 删除接口
- 还没�?user 删除接口
- credential 还缺少单项详�?/ 更新 / rotate secret
- 管理�?tenant / user 搜索过滤还不够强
- 还没有导�?/ 导入 / 备份接口
- 还没有统一异步 job 模型
- 还没有服务级 webhook / 事件订阅
- 还没�?`/readyz`、`/livez`、`/version`、`/metrics`

这部分可进一步参�?`GAP_ANALYSIS.zh-CN.md`�?
## 19. 推荐文档入口

- `README.md`：项目定位、接口分组、当前范�?- `API_MANUAL.md`：英文详细手�?- `API_MANUAL.zh-CN.md`：中文详细手�?- `QUICKSTART.md`：英文快速接�?- `QUICKSTART.zh-CN.md`：中文快速接�?- `GAP_ANALYSIS.md`：英文缺口分�?- `GAP_ANALYSIS.zh-CN.md`：中文缺口分�?


## 20. �첽 Job

- `POST /api/v1/mcp/servers?async=true`���첽���� MCP server���������� job��
- `PATCH /api/v1/mcp/servers/{serverId}?async=true`���첽���� MCP server���������� job��
- `POST /api/v1/mcp/servers/{serverId}/start?async=true`���첽���� MCP server���������� job��
- `POST /api/v1/mcp/servers/{serverId}/stop?async=true`���첽ֹͣ MCP server���������� job��
- `POST /api/v1/mcp/servers/{serverId}/health-check?async=true`���첽������� MCP server���������� job��

- `POST /api/v1/skills/install?async=true`���첽��װ skill���������� job��
- `POST /api/v1/skills/import?async=true`���첽���� skill ѹ�������������� job��
- `POST /api/v1/skills/{skillName}/upload?async=true`���첽�ϴ� skill���������� job��
- `GET /api/v1/jobs/{jobId}`����ѯ job ״̬��
- `status` ����Ϊ `pending`��`running`��`succeeded`��`failed`��
- job ��Դ�� `tenant/user` ���룬ֻ�ܲ�ѯ�Լ��� job��
- `GET /api/v1/jobs`���г���ǰ�û�������첽 job���ɰ� `kind`��`status` ���ˡ�
- `POST /api/v1/jobs/{jobId}/cancel`��ȡ������ `pending` �� `running` �� job��
- `DELETE /api/v1/jobs/{jobId}`��ɾ���Ѿ������� job ��¼��


