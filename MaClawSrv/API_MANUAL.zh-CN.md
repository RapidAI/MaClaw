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




- `MACLAW_ADMIN_SECRET`：管理端密钥，建议至少 24 字符。它就是 Admin Web 里 “Admin Secret” 登录方式使用的密钥，也会作为 `X-MaClaw-Admin-Secret` 发送到管理接口；适合自动化、紧急管理或直接控制面调用，不是管理员账号密码。




- `MACLAW_ADMIN_SETUP_TOKEN`：可选的首次初始化令牌，只用于创建第一个 owner 管理员；初始化完成后不要把它当作 Admin Secret 使用。




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




- ????? `include_archived=true`?????????????`true` / `false`?




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









- `role`???? `user`?`assistant`?`system`




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




- run ???? `status`?`session_id`?`response_source`?`waiting_for_user`?`limit`?`before`??? `status` ??? `running`?`succeeded`?`failed`?`cancelled`?`response_source` ????? `ask_user`?`waiting_for_user` ????????




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















## ????

?????`DELETE /api/v1/admin/tenants/{tenantId}` ? `DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}` ?????? `?confirm=true`?

?????
1. ?????? `delete-check` ? `retire-plan` ???
2. ????????????????
3. ???????? `?confirm=true` ???????

?? `confirm=true` ?????? `400`????????????????

## ????

?????????? `delete_protected` ? `delete_protection_reason` ???????

?????
1. ??????????????????
2. `delete-check` ??? `delete_protected=true`??? `blockers` ??? `delete_protected` ????
3. ???????? `?confirm=true`????????????????? `409`?

## ???????

?????????? credential ????? `api_key` ?/? `api_secret`?

`POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials` ????????????????????????????????????????????? `api_secret`???? credential ????? masked key ???

## Credential ????

Credential ?????`GET /api/v1/admin/alerts` ????????????? credential?

???????
- `kind=credential_expiring` ?? `expires_at` ???????? credential?
- `kind=credential_expired` ?? `expires_at` ????? credential?
- `credential_expiry_window_days` ?????????????????? `7`????? `365`?

Credential ???????????????? `api_secret`?`secret_digest` ? `api_key_hash`?

## ????? credential ???

??????????? `rotate-secret` ????? `api_secret`??? `rotate-key` ????? `api_key`?

????????????????????????? credential ?????? masked key ????????? secret?

## Admin overview credential ??

Admin overview credential ???overview ????? credential ??????????

???? `credentials`?`active_credentials`?`suspended_credentials`?`revoked_credentials`?`expired_credentials` ? `expiring_credentials`??? `expiring_credentials` ???? 7 ??????

## Tenant summary 凭证计数

`GET /api/v1/admin/tenants/{tenantId}/summary` 会在租户汇总和每个 `user_summaries[]` 条目里返回 credential 生命周期计数。

字段包括 `credentials`、`active_credentials`、`suspended_credentials`、`revoked_credentials`、`expired_credentials` 和 `expiring_credentials`。其中 `expiring_credentials` 使用与 credential 到期告警一致的默认 7 天前瞻窗口。

## Credential metrics 指标

`GET /metrics` 会输出 Prometheus 格式的 credential 生命周期指标：

- `maclaw_credentials_total`
- `maclaw_credentials_by_status{status="active"}`
- `maclaw_credentials_by_status{status="suspended"}`
- `maclaw_credentials_by_status{status="revoked"}`
- `maclaw_credentials_expired_total`
- `maclaw_credentials_expiring_total`

## Usage summary 凭证计数

`GET /api/v1/usage/summary` 会返回当前认证租户/用户自己的 credential 生命周期计数。字段包括 `credentials`、`active_credentials`、`suspended_credentials`、`revoked_credentials`、`expired_credentials` 和 `expiring_credentials`。

这些字段只是聚合计数，不会暴露 credential secret、key hash 或明文 API key。

## Credential 创建时设置 expires_at

`POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials` 支持在创建体里传入可选的 `expires_at`，格式为 RFC3339/RFC3339Nano。

示例：

```json
{
  "name": "CI runner",
  "expires_at": "2026-05-01T00:00:00Z"
}
```

如果省略 `api_key` 或 `api_secret`，服务仍会自动生成，并且只在创建响应里返回一次明文值。

## Credential 列表过滤

`GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials` 支持先过滤再分页。

支持的查询参数：

- `status=active|suspended|revoked`
- `expired=true|false`
- `expiring=true|false`
- `limit` 和 `before` 用于分页

`expiring=true` 使用默认 7 天前瞻窗口。`status`、`expired` 或 `expiring` 参数非法时返回 `400`。

## Audit event 资源过滤

`GET /api/v1/admin/audit-events` 支持额外过滤参数：

- `resource_id`：精确匹配资源 ID，例如 credential id、run id、user id 或 tenant id。
- `actor_type`：精确匹配操作者类型，例如 `admin`、`user`、`credential`、`system` 或 `anonymous`。

这些参数可以和 `tenant_id`、`user_id`、`action`、`resource_type`、`limit`、`before` 组合使用。

## Audit event 时间窗口过滤

`GET /api/v1/admin/audit-events` 支持 `since` 和 `until`，格式为 RFC3339/RFC3339Nano。它们会按 audit event 的 `created_at` 做时间窗口过滤，并且先过滤再分页。

`since/until` 用于逻辑时间窗口；`before` 仍然是分页游标，对应响应里的 `next_before`。

## Admin overview 快照计数

`GET /api/v1/admin/overview` 会返回持久化快照观测字段：

- `snapshots`：`MACLAW_DATA_ROOT/snapshots` 下的快照数量。
- `snapshot_bytes`：这些快照文件占用的总字节数。

## Snapshot metrics 指标

`GET /metrics` 会输出 Prometheus 格式的快照指标：

- `maclaw_snapshots_total`
- `maclaw_snapshot_bytes_total`

这两个指标分别表示持久化快照数量和总字节数，适合接入监控系统做容量告警。

### 9.13 管理端快照

快照接口把已有导出能力资源化，会把导出结果保存到 `MACLAW_DATA_ROOT/snapshots`，方便备份、迁移前留档和运维巡检。

创建快照：

```http
POST /api/v1/admin/snapshots
X-MaClaw-Admin-Secret: <admin-secret>
Content-Type: application/json

{
  "name": "tenant-a nightly backup",
  "tenant_id": "tenant_xxx",
  "user_id": "user_xxx",
  "include_messages": true,
  "include_runs": true,
  "include_audit": true,
  "include_secrets": false
}
```

说明：

- `tenant_id` 可选；不传表示创建全服务快照。
- `user_id` 可选，但传 `user_id` 时必须同时传 `tenant_id`。
- `include_messages`、`include_runs`、`include_audit` 默认是 `true`；`include_secrets` 默认是 `false`。
- 快照文件会写入服务数据目录下的私有 JSON 文件。
- 返回结构是 `{ "snapshot": <metadata>, "data": <ExportServiceStateOutput> }`。
- 快照数量和总字节数可以通过 `GET /api/v1/admin/overview` 和 `GET /metrics` 查看。

列出快照：

```http
GET /api/v1/admin/snapshots?tenant_id=tenant_xxx&user_id=user_xxx&scope=user&name=nightly&since=2026-04-01T00:00:00Z&until=2026-04-28T00:00:00Z&limit=100&before=2026-04-28T00:00:00Z
X-MaClaw-Admin-Secret: <admin-secret>
```

列表过滤支持 `tenant_id`、`user_id`、`scope=service|tenant|user`、不区分大小写的 `name`，以及 RFC3339 格式的 `since`/`until` 时间窗口。

读取单个快照：

```http
GET /api/v1/admin/snapshots/{snapshot_id}
X-MaClaw-Admin-Secret: <admin-secret>
```

恢复快照：

```http
POST /api/v1/admin/snapshots/{snapshot_id}/restore?dry_run=true
X-MaClaw-Admin-Secret: <admin-secret>
```

```http
POST /api/v1/admin/snapshots/{snapshot_id}/restore?overwrite=true
X-MaClaw-Admin-Secret: <admin-secret>
```

恢复说明：

- 恢复会复用 `POST /api/v1/admin/import` 的同一套导入管线。
- `dry_run=true` 只返回冲突、警告和执行计划，不修改状态。
- 当快照中的 tenant/user ID 已存在时，真正恢复需要 `overwrite=true`。
- `overwrite` 和 `dry_run` 既可以放 query，也可以放 JSON body。
- 如果要完整恢复 credential，创建快照时需要 `include_secrets=true`。

清理快照：

```http
POST /api/v1/admin/snapshots/prune?tenant_id=tenant_xxx&user_id=user_xxx&older_than=2026-04-28T00:00:00Z&keep_latest=3&dry_run=true
X-MaClaw-Admin-Secret: <admin-secret>
```

清理说明：

- `older_than` 是 RFC3339 时间戳。
- `keep_latest=N` 会保护筛选范围内最新的 N 个快照。
- `older_than` 和 `keep_latest` 至少需要传一个。
- 建议先用 `dry_run=true` 预览 `snapshots`、`kept_snapshots` 和 `freed_bytes`。

删除快照：

```http
DELETE /api/v1/admin/snapshots/{snapshot_id}?confirm=true
X-MaClaw-Admin-Secret: <admin-secret>
```
