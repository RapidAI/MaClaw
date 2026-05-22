# MaClawSrv 5 分钟接入

这份文档给第一次接 `MaClawSrv` 的前端、第三方工具和外部服务用，只保留最短可用链路。更完整的字段说明见 `API_MANUAL.zh-CN.md`。

## 1. 你最少要知道什么

- `MaClawSrv` 是 Maclaw 的 REST 服务入口。
- 鉴权分两层：管理接口用 `X-MaClaw-Admin-Secret`，用户接口用 Bearer token。
- Admin Web 有两种管理登录方式：用初始化后创建的 owner/operator 管理员账号密码登录，或用服务启动环境变量 `MACLAW_ADMIN_SECRET` 直接登录。`MACLAW_ADMIN_SETUP_TOKEN` 只用于首次创建 owner。
- config、skill、MCP 都是 user 级共享资源，不是 instance 私有资源。
- 真正聊天时，优先使用 `POST /api/v1/instances/{instanceId}/messages`。

## 2. 最短可用链路

### 第一步：管理端创建 tenant / user / credential

```http
POST /api/v1/admin/tenants
POST /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials
```

### 第二步：客户端换 token

```http
POST /api/v1/auth/token
```

请求体：

```json
{
  "api_key": "ak_demo",
  "api_secret": "demo-secret"
}
```

### 第三步：读取并补全配置

```http
GET /api/v1/config/schema
GET /api/v1/config
POST /api/v1/config/validate
PUT /api/v1/config
```

最小示例：

```json
{
  "maclaw_llm_url": "https://api.openai.com/v1",
  "maclaw_llm_key": "sk-xxx",
  "maclaw_llm_model": "gpt-5.4"
}
```

### 第四步：创建 instance

```http
POST /api/v1/instances
```

请求体：

```json
{
  "name": "primary-agent",
  "description": "demo"
}
```

### 第五步：查询 capabilities

```http
GET /api/v1/instances/{instanceId}/capabilities
```

这个接口决定：

- 是否支持 sessions
- 是否支持 ask-user
- 是否开放本地 bash / SSH
- 当前有哪些工具能展示给用户

### 第六步：发第一条消息

```http
POST /api/v1/instances/{instanceId}/messages
```

请求体：

```json
{
  "title": "Demo",
  "content": "Please introduce your capabilities."
}
```

成功返回通常包含：

- `session`
- `run`
- `message`

## 3. 两种拿结果方式

### 方式 A：轮询

```http
GET /api/v1/instances/{instanceId}/runs/{runId}
GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages
```

适合普通 Web 前端，最稳妥。

### 方式 B：SSE

```http
GET /api/v1/instances/{instanceId}/runs/{runId}/events
```

适合更实时的聊天体验。收到 `done` 后，再拉一次 `messages` 做最终一致性。

## 4. 最常见坑

- 不要把 config 当成 instance 级配置。
- 不要把 skill 和 MCP 当成 instance 私有资源。
- 不要假设本地 bash 一定可用，必须看 `capabilities`。
- 不要只信 `run` 返回体，聊天记录要以 `messages` 列表为准。
- 如果返回 `config_validation.issues`，应直接把问题展示给用户。

## 5. 你真正常用的接口

- `POST /api/v1/auth/token`
- `GET /api/v1/me`
- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `POST /api/v1/config/validate`
- `PUT /api/v1/config`
- `POST /api/v1/instances`
- `GET /api/v1/instances/{instanceId}/capabilities`
- `POST /api/v1/instances/{instanceId}/messages`
- `GET /api/v1/instances/{instanceId}/runs/{runId}`
- `GET /api/v1/instances/{instanceId}/runs/{runId}/events`
- `GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages`

## 6. 下一步读什么

- 需要字段级说明：看 `API_MANUAL.zh-CN.md`
- 需要管理端接入：看 `API_MANUAL.zh-CN.md` 的管理接口章节
- 需要 Skill / MCP 接入：看 `API_MANUAL.zh-CN.md` 的 skill 和 MCP 章节
