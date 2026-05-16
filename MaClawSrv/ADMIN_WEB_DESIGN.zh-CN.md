# MaClawSrv Admin Web 管理面设计

## 1. 背景

`MaClawSrv` 已经具备面向用户实例的 REST 能力，包括 tenant、user、credential、instance、session、run、MCP、skill、knowledge 等资源管理。随着本地 bash、本地 MCP server、skill 执行、scheduler、sandbox、日志和备份能力增多，服务还需要一个只面向运维管理员的管理面。

本文设计的 `Admin Web` 不是 Maclaw 用户工作台，也不是某个用户实例的配置页，而是 `maclawsrv` 进程自身的控制台。它用于安装部署、运行诊断、安全策略、全局能力开关、日志查看、备份恢复、sandbox 管理和服务级告警。

## 2. 目标

- 给管理员提供统一入口，管理 `MaClawSrv` 服务级配置。
- 把用户实例配置和服务进程自身配置分离，避免高危运维能力暴露给普通 bearer token 用户。
- 把 sandbox 的检测、安装建议、配置、启停验证和执行审计纳入 admin 能力。
- 提供部署、升级、故障排查能力，包括错误日志、运行日志、审计事件、指标、ready 检查和 async job 状态。
- 保持低侵入：优先复用已有 admin API、readiness、metrics、audit、snapshot、skill-source、knowledge admin 接口。
- 支持无 Web UI 的自动化场景：Admin Web 所有能力都应有对应 `/api/v1/admin/...` JSON API。

## 3. 非目标

- 不把普通用户的 agent 聊天、session 浏览、run 交互搬进 admin web。
- 不让 admin web 直接保存用户 LLM API Key，除非进入明确的 tenant/user provisioning 流程。
- 不默认自动执行 `sudo apt install` 等系统级安装动作。安装可以被显式触发，但默认应以检测和建议为主。
- 不要求 sandbox 支持 Windows/macOS。第一阶段 sandbox 管理能力可以限定 Linux。
- 不把传统容器运行时作为默认方案；Docker/Podman 只作为未来扩展后端。

## 4. 权限模型

Admin Web 需要支持第一次登录初始化，模式参考 `hub` / `hubcenter` 的首启 setup 流程。安全防护沿用现有 MaClawSrv 安全模型：admin API 继续使用 `X-MaClaw-Admin-Secret`、启动期强制校验 `MACLAW_ADMIN_SECRET` / `MACLAW_TOKEN_SECRET`、auth limiter、loopback/TLS 传输约束、敏感字段脱敏和 audit 记录。Admin UI 本身只是 Admin API client，不引入绕过现有模型的新权限通道。

### 4.1 Bootstrap 状态

服务启动后先判断是否已经初始化 admin 身份：

```text
initialized = 存在 admin user/session 配置，且至少一个 owner 账号可用
```

建议新增 bootstrap state：

```text
MACLAW_DATA_ROOT/
  state/
    admin_bootstrap.json
    admin_users.json
    admin_sessions.json
```

`admin_bootstrap.json` 只保存初始化状态、时间、初始化版本、setup token hash，不保存明文密码或明文 token。

### 4.2 首次初始化入口

未初始化时：

- Admin Web 只开放 setup 页面、health/livez/readyz、bootstrap status 和 bootstrap initialize API。
- 所有普通 `/api/v1/admin/...` 写能力返回 `423 Locked` 或 `403 setup_required`。
- UI 显示“初始化 MaClawSrv Admin”，要求创建第一个 owner 账号。
- 初始化完成后立即禁用 bootstrap initialize API。

建议 API：

```text
GET  /api/v1/admin/bootstrap/status
POST /api/v1/admin/bootstrap/initialize
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
POST /api/v1/admin/auth/change-password
```

`GET /api/v1/admin/bootstrap/status` 在未登录时也可访问，但只能返回非敏感状态：

```json
{
  "initialized": false,
  "setup_required": true,
  "setup_token_required": true,
  "password_policy": {
    "min_length": 12,
    "require_mixed_classes": true
  }
}
```

`POST /api/v1/admin/bootstrap/initialize`：

```json
{
  "setup_token": "one-time-token-from-console-or-env",
  "owner": {
    "username": "admin",
    "display_name": "Administrator",
    "password": "..."
  },
  "service_config": {
    "sandbox": {
      "mode": "auto",
      "strict": false
    }
  }
}
```

响应：

```json
{
  "initialized": true,
  "owner_id": "admin_xxx",
  "session": {
    "expires_at": "2026-05-18T00:00:00Z"
  },
  "next_steps": ["run_sandbox_detect", "review_security_posture"]
}
```

### 4.3 Setup Token

为了避免未初始化窗口被远程抢占，bootstrap initialize 必须要求一次性 setup token。推荐来源优先级：

```text
MACLAW_ADMIN_SETUP_TOKEN -> 首次启动自动生成并打印到 console/log -> installer 写入本机 only-readable 文件
```

自动生成时：

- token 只在 loopback 监听时允许使用。
- token 明文只打印一次。
- 文件权限必须 owner-only。
- 初始化成功后删除 token 文件并清空 bootstrap pending 状态。
- 如果服务监听非 loopback 且未启用 TLS，禁止通过 Web 完成首次初始化。

建议 token 文件：

```text
MACLAW_DATA_ROOT/state/admin_setup_token
```

### 4.4 Admin API 安全模型

Admin UI 通过现有 Admin API 完成所有操作。第一阶段不新增独立浏览器权限模型，不要求替换 `X-MaClaw-Admin-Secret`。如果需要登录页体验，可以由 UI 收集 admin secret 并换取短期 admin UI session，但该 session 仍应由后端映射到现有 admin 权限模型，不扩大权限范围。

必须保持：

- `MACLAW_ADMIN_SECRET` 仍是 admin 控制面的根 secret，启动时必须强校验。
- `MACLAW_TOKEN_SECRET` 仍用于用户 bearer token 签发和校验。
- Admin API 继续接受 `X-MaClaw-Admin-Secret`，用于 CLI、安装器、自动化脚本和 Admin UI 后端调用。
- 登录失败、admin secret 错误、bootstrap token 错误都进入现有 auth limiter。
- 远程访问继续遵守 TLS/loopback 规则：非 loopback 明文 HTTP 默认拒绝。
- 所有高危 admin 写操作必须写 audit，并记录 actor、source IP、request id、reason、before/after 摘要。
- Admin UI 不直接读写文件、不直接执行命令、不绕过 `/api/v1/admin/...`。

可选 Admin UI session：

```text
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
```

该 session 只是一层 UI 便利封装，cookie 必须使用 `HttpOnly`、`SameSite=Lax`，TLS 下启用 `Secure`。API client 仍可直接使用 `X-MaClaw-Admin-Secret`。

### 4.5 权限层级

建议权限层级：

- `viewer`：只读查看状态、日志、配置、告警、sandbox 检测结果。
- `operator`：可执行 readiness refresh、log rotate、sandbox smoke test、MCP stop/start、snapshot create/prune。
- `admin`：可修改服务级配置、sandbox 策略、全局 skill source、scheduler、TLS、local bash 策略。
- `owner`：可轮换 admin secret、恢复 snapshot、执行安装命令、修改危险开关、管理 admin 用户。

第一阶段如果仍保留 `adminSecret`，API 设计也应预留 `required_role` 字段，便于后续演进。

### 4.6 首次设置向导

首次 owner 创建后进入 setup wizard：

1. 检查 data root、log root、snapshot root 权限。
2. 设置服务名称、public base URL、TLS/insecure HTTP 策略。
3. 配置 sandbox：执行 detect，选择 `auto|landlock|bwrap|nsjail|none`，生成 install plan。
4. 配置 local execution policy：local bash、local MCP、skill step 是否允许，以及 strict fallback。
5. 配置 snapshot retention 和 async job retention。
6. 展示 security posture，要求确认高风险项。

wizard 的每一步都应可跳过，但跳过会在 Overview 和 Security 中保留 warning。

## 5. 配置边界

### 5.1 用户级配置

用户级配置继续通过现有接口管理：

- `GET /api/v1/config/schema`
- `GET /api/v1/config`
- `PUT /api/v1/config`
- `POST /api/v1/config/validate`
- `POST /api/v1/config/test`

这些配置属于 tenant/user，例如 LLM provider、SSH host label、MCP server、skill 和用户工作流偏好。

### 5.2 服务级配置

服务级配置属于 `maclawsrv` 进程，不绑定单个用户。建议新增 `service_config.json`，存放在：

```text
MACLAW_DATA_ROOT/
  state/
    service_config.json
```

环境变量继续作为启动期 override。推荐规则：

```text
effective = defaults < service_config.json < environment overrides
```

对必须启动前生效的配置，例如 listen addr、TLS 证书、admin/token secret，Admin Web 可以展示和校验，但修改后标记为 `restart_required=true`。

## 6. Admin Web 信息架构

### 6.1 Internationalization

Admin Web 需要支持中文和英文双语言，并允许管理员随时切换。语言切换是 Admin Web 的基础能力，不应影响 API 字段名和存储结构。

#### 6.1.1 语言范围

第一阶段支持：

```text
zh-CN | en-US
```

UI 中所有可见文本都必须走 i18n key，包括：

- 导航、页面标题、按钮、表单 label、placeholder。
- 错误提示、确认弹窗、Danger Zone 警告。
- sandbox backend 状态说明、install plan 提示、smoke test 结果文案。
- logs、security posture、readiness、delete-check、retire-plan 的用户可读说明。
- setup wizard 的步骤标题和帮助说明。

API 返回的机器字段保持英文 snake_case，不做本地化；面向 UI 展示的 `message`、`title`、`suggested_action` 可以支持本地化。

#### 6.1.2 切换和持久化

语言优先级：

```text
用户手动选择 -> admin user preference -> cookie/localStorage -> Accept-Language -> service default -> zh-CN
```

建议配置字段：

```json
{
  "admin_web": {
    "default_locale": "zh-CN",
    "enabled_locales": ["zh-CN", "en-US"]
  }
}
```

建议 API：

```text
GET  /api/v1/admin/i18n/locales
GET  /api/v1/admin/i18n/messages?locale=zh-CN
PUT  /api/v1/admin/auth/preferences
```

`PUT /api/v1/admin/auth/preferences` 示例：

```json
{
  "locale": "en-US",
  "timezone": "Asia/Shanghai"
}
```

未登录的 bootstrap/setup 页面也必须允许语言切换，此时语言选择保存在 cookie/localStorage，初始化 owner 后可以写入 admin user preference。

#### 6.1.3 文案组织

前端建议使用 namespace 管理文案：

```text
common
nav
setup
service_config
sandbox
logs
security
tenants
users
snapshots
audit
scheduler
diagnostics
errors
```

示例：

```json
{
  "sandbox.switch.title": "Switch sandbox mode",
  "sandbox.switch.none_warning": "Sandbox protection will be disabled for new local executions.",
  "tenants.delete.confirmation": "Type DELETE {id} to confirm permanent deletion."
}
```

中文：

```json
{
  "sandbox.switch.title": "切换沙箱模式",
  "sandbox.switch.none_warning": "新的本地执行将不再受沙箱保护。",
  "tenants.delete.confirmation": "请输入 DELETE {id} 确认永久删除。"
}
```

#### 6.1.4 后端本地化

后端错误响应建议同时返回稳定错误码和默认英文 message：

```json
{
  "error": "sandbox smoke test failed",
  "code": "SANDBOX_SMOKE_TEST_FAILED",
  "message_key": "errors.sandbox_smoke_test_failed",
  "details": {}
}
```

前端优先根据 `message_key` 渲染本地化文案；没有对应 key 时回退到 `error`。

对于 audit event、log event、sandbox event，存储层保留稳定 action/code，例如 `sandbox.backend.switched`；UI 再根据 action/code 本地化显示。

#### 6.1.5 测试要求

- 首启 setup 页面在未登录状态可以切换中英文。
- 登录后语言偏好跨 session 保留。
- Danger Zone、删除确认、sandbox `none` 警告必须有中英文文案。
- 页面布局要验证中英文长度差异，避免按钮、表格列、弹窗文字溢出。

### 6.2 Overview

首页展示服务总览：

- service version、build commit、启动时间、进程 PID、运行用户。
- data root、state path、log path、snapshot path。
- readiness 状态。
- sandbox 当前模式和健康状态。
- tenant/user/instance/session/run 计数。
- 最近错误日志、最近高危 audit、最近 failed run。
- scheduler 状态。

复用现有接口：

- `GET /health`
- `GET /livez`
- `GET /readyz`
- `GET /version`
- `GET /metrics`
- `GET /api/v1/admin/system/readiness`
- `GET /api/v1/admin/overview`
- `GET /api/v1/admin/dashboard`
- `GET /api/v1/admin/alerts`

### 6.3 Service Config

管理 `MaClawSrv` 进程级设置：

- HTTP listen addr、TLS 状态、insecure HTTP 策略。
- data root、snapshot retention、async job retention。
- local bash 总开关和 scoped tenant/user。
- direct SSH 总开关和 file transfer 总开关。
- scheduler 开关、并发、超时、job 保留。
- web search、knowledge、skill source 全局策略。
- debug flags，例如 tool call debug、trace retention。
- secret 状态展示，例如已配置、长度合规、最后轮换时间，但不显示明文。

建议 API：

```text
GET  /api/v1/admin/service-config/schema
GET  /api/v1/admin/service-config
PUT  /api/v1/admin/service-config
POST /api/v1/admin/service-config/validate
POST /api/v1/admin/service-config/reload
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
GET  /api/v1/admin/service-config/effective
```

响应中每个字段建议带上：

```json
{
  "value": "auto",
  "source": "file|env|default",
  "restart_required": false,
  "sensitive": false,
  "mutable_at_runtime": true
}
```

### 6.4 Sandbox

Sandbox 是 Admin Web 的一等能力，用于管理 `bash`、本地 MCP server、skill step 等本地执行入口。

#### 6.4.1 后端模型

推荐支持：

- `none`：不启用 sandbox。
- `auto`：自动检测并选择可用后端。
- `landlock`：使用 `sandlock`、`landrun`、`rstrict`、`sandboxec` 等 Landlock wrapper。
- `bwrap`：使用 bubblewrap 做用户级 namespace sandbox。
- `nsjail`：高隔离模式，适合高风险执行。

推荐默认优先级：

```text
landlock wrapper -> bwrap -> nsjail -> none
```

如果目标更偏“轻容器视图”，可把 `bwrap` 放在 `landlock` 前面。该优先级应可配置。

#### 6.4.2 配置字段

管理员可以在 Admin Web 中统一切换当前 sandbox 模型，方便调试、兼容性验证或排除 sandbox 相关故障。切换应是服务级配置，影响后续新的 `local_bash`、`local_mcp`、`skill_step` 执行；已经启动的本地 MCP 进程不应被静默替换，需要提示管理员重启对应 MCP server 或执行 `restart_affected=true`。

```json
{
  "sandbox": {
    "mode": "auto",
    "active_backend": "landlock",
    "previous_backend": "bwrap",
    "strict": false,
    "install_policy": "suggest",
    "preferred_backends": ["landlock", "bwrap", "nsjail"],
    "backend_bins": {
      "sandlock": "",
      "landrun": "",
      "rstrict": "",
      "sandboxec": "",
      "bwrap": "",
      "nsjail": ""
    },
    "profile": "default",
    "allow_network": false,
    "allowed_hosts": [],
    "workspace_write": true,
    "tmp_write": true,
    "home_read": false,
    "extra_read_paths": [],
    "extra_write_paths": [],
    "resource_limits": {
      "timeout_seconds": 120,
      "max_processes": 128,
      "memory_mb": 0,
      "cpu_seconds": 0,
      "output_bytes": 131072
    },
    "apply_to": {
      "local_bash": true,
      "local_mcp": true,
      "skill_steps": true
    }
  }
}
```

#### 6.4.3 检测能力

Admin Web 应展示：

- OS、kernel version、architecture。
- user namespace 是否可用。
- Landlock ABI 是否可用。
- `bwrap`、`nsjail`、`sandlock`、`landrun`、`rstrict`、`sandboxec` 是否存在。
- 每个后端的 smoke test 是否通过。
- 当前 `effective_backend` 和 fallback 原因。
- 哪些执行入口已受保护。

建议 API：

```text
GET  /api/v1/admin/sandbox/status
POST /api/v1/admin/sandbox/detect
POST /api/v1/admin/sandbox/smoke-test
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/reports
GET  /api/v1/admin/sandbox/reports/{reportId}
POST /api/v1/admin/sandbox/switch
POST /api/v1/admin/sandbox/rollback
GET  /api/v1/admin/sandbox/profiles
PUT  /api/v1/admin/sandbox/profiles/{profileName}
POST /api/v1/admin/sandbox/profiles/{profileName}/validate
GET  /api/v1/admin/sandbox/install-plan
POST /api/v1/admin/sandbox/install
GET  /api/v1/admin/sandbox/events
```

`install-plan` 只生成建议命令，例如：

```json
{
  "platform": "debian",
  "commands": [
    "sudo apt-get update",
    "sudo apt-get install -y bubblewrap"
  ],
  "requires_privilege": true,
  "will_execute": false
}
```

`install` 必须要求显式确认：

```json
{
  "backend": "bwrap",
  "confirm": true,
  "mode": "run|print_only"
}
```

默认 `install_policy=suggest` 时，Admin Web 只展示命令，不执行。

#### 6.4.4 沙箱健康检测报告

沙箱启用后，Admin Web 必须提供一键检测功能，用于确认当前 sandbox 是否真正生效，并输出管理员可读报告。该检测不仅检查 binary 是否存在，还要验证隔离策略是否按预期工作。

建议入口：

```text
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/reports
GET  /api/v1/admin/sandbox/reports/{reportId}
```

检测应覆盖：

- 当前模式、effective backend、profile、strict/fallback 状态。
- 后端 binary 路径和版本。
- OS、kernel、user namespace、Landlock ABI、seccomp、cgroup 能力。
- smoke test：执行 `/bin/true` 或等价命令。
- workspace 读写：允许写入 workspace 的临时测试文件，并清理。
- forbidden path：尝试读取未授权路径，例如 `/etc/shadow`，期望失败。
- tmp 写入：按 profile 验证 `/tmp` 或私有 tmpfs。
- network：按 `allow_network` 验证断网或允许访问；如果配置了 allowed hosts，验证允许和拒绝各一个样例。
- process isolation：验证可见 `/proc` 范围、pid namespace 行为，bwrap/nsjail 模式下必须检查。
- env sanitization：验证敏感环境变量是否被清理或按策略传递。
- MCP stdio compatibility：可选启动一个 echo MCP probe，确认 stdin/stdout 没被 wrapper 破坏。
- resource limits：可选验证 timeout、输出截断、进程数限制。

请求示例：

```json
{
  "profile": "default",
  "include_network_tests": false,
  "include_mcp_stdio_test": true,
  "include_resource_limit_tests": false,
  "write_report": true
}
```

报告结构：

```json
{
  "report_id": "sandbox_report_xxx",
  "generated_at": "2026-05-17T12:00:00Z",
  "status": "pass|warn|fail",
  "summary": "Sandbox is active and core isolation checks passed.",
  "mode": "auto",
  "effective_backend": "bwrap",
  "profile": "default",
  "strict": false,
  "checks": [
    {
      "id": "forbidden_path_read",
      "title": "Forbidden path is blocked",
      "status": "pass",
      "expected": "read denied",
      "actual": "permission denied",
      "severity": "critical",
      "duration_ms": 18
    }
  ],
  "warnings": [
    "Network is not isolated because allow_network=true."
  ],
  "recommendations": [
    "Run MCP stdio test before enabling sandbox for all local MCP servers."
  ],
  "raw": {
    "redacted_stdout": "...",
    "redacted_stderr": "..."
  }
}
```

状态判定：

- `pass`：核心隔离测试通过，当前配置可用于受保护执行。
- `warn`：沙箱可运行，但存在弱隔离、跳过测试、fallback、网络未隔离等风险。
- `fail`：后端不可用、smoke test 失败、禁止路径可读、profile 无法加载、stdio 被破坏等。

Admin Web 展示要求：

- Overview 和 Sandbox 页面显示最近一次检测状态、时间和报告链接。
- 每个 check 用 `pass/warn/fail/skipped` 展示，并给出“期望/实际/建议”。
- `fail` 时提供直接操作：切换模式、rollback、查看日志、生成 install plan。
- 报告可下载 JSON，但默认脱敏 stdout/stderr、路径和环境变量。
- 检测报告写入 audit：`sandbox.diagnose.started`、`sandbox.diagnose.completed`、`sandbox.diagnose.failed`。

检测频率建议：

- 手动切换 sandbox 后自动跑一次轻量 diagnose。
- 服务启动后如果 sandbox enabled，可异步跑一次轻量 diagnose 并缓存结果。
- 管理员可手动运行完整 diagnose。
- 报告保留最近 20 份，或按 `service_config.sandbox.report_retention` 控制。

#### 6.4.5 统一切换和故障排除

Sandbox 页面需要提供全局模式切换器：

```text
Auto | Landlock | bwrap | nsjail | None
```

切换流程：

1. 管理员选择目标模式。
2. 服务执行目标后端 detect 和 smoke test。
3. 展示影响范围：local bash、local MCP、skill step，以及需要重启的本地 MCP server 数量。
4. 管理员确认后写入 `service_config.json`。
5. 新执行请求立即使用新模式；已运行的本地 MCP server 维持旧模式直到重启。
6. 写入 audit 和 sandbox event。

建议请求：

```json
{
  "mode": "bwrap",
  "profile": "default",
  "reason": "debug mcp startup failure",
  "run_smoke_test": true,
  "restart_affected_mcp": false,
  "fallback_if_unavailable": false,
  "confirm": true
}
```

响应：

```json
{
  "previous_mode": "landlock",
  "current_mode": "bwrap",
  "effective_backend": "bwrap",
  "restart_required": false,
  "affected": {
    "local_mcp_running": 3,
    "local_mcp_needs_restart": true
  },
  "smoke_test": {
    "status": "passed",
    "duration_ms": 42
  },
  "audit_event_id": "audit_xxx"
}
```

`none` 模式是危险调试模式：

- UI 必须显示红色警告，说明本地执行将不再被 sandbox 保护。
- 需要 `admin` 或更高权限；如果 `strict=true`，需要 `owner` 权限或先关闭 strict。
- 可以要求填写 `reason`。
- 可以支持 `expires_at` 或 `ttl_minutes`，到期自动恢复上一模式。

建议 rollback 行为：

```text
POST /api/v1/admin/sandbox/rollback
```

用于快速恢复上一可用模式。每次 switch 应保存 `previous_backend`、`previous_profile`、切换人和切换时间。

#### 6.4.6 Sandbox 事件和审计

所有 sandbox 相关动作都应写入 audit：

- `sandbox.detected`
- `sandbox.config.updated`
- `sandbox.smoke_test.succeeded`
- `sandbox.smoke_test.failed`
- `sandbox.diagnose.started`
- `sandbox.diagnose.completed`
- `sandbox.diagnose.failed`
- `sandbox.install_plan.generated`
- `sandbox.install.started`
- `sandbox.install.failed`
- `sandbox.backend.selected`
- `sandbox.backend.switched`
- `sandbox.backend.rollback`
- `sandbox.execution.blocked`

执行时还应记录轻量事件，不保存完整命令敏感参数：

```json
{
  "backend": "bwrap",
  "entrypoint": "local_mcp|local_bash|skill_step",
  "profile": "default",
  "workspace": "/srv/workspaces/example",
  "allowed_network": false,
  "exit_code": 0,
  "duration_ms": 238
}
```

### 6.5 Logs

日志查看是 Admin Web 的核心运维能力。

建议日志分类：

- `service`：maclawsrv 标准运行日志。
- `error`：stderr 或 error-level 日志。
- `access`：HTTP access 日志，第一阶段可选。
- `audit`：admin/user 资源操作审计，复用现有 audit events。
- `sandbox`：sandbox 检测、选择、执行和阻断事件。
- `scheduler`：定时任务日志。
- `mcp`：本地 MCP server 启停、health check、tools/list 错误。
- `agent`：run 级错误摘要，不包含用户敏感内容。

建议 API：

```text
GET  /api/v1/admin/logs/sources
GET  /api/v1/admin/logs/{source}
GET  /api/v1/admin/logs/{source}/tail
POST /api/v1/admin/logs/{source}/rotate
POST /api/v1/admin/logs/search
GET  /api/v1/admin/logs/errors/recent
```

查询参数：

```text
level=debug|info|warn|error
since=2026-05-17T00:00:00Z
until=2026-05-17T01:00:00Z
limit=200
cursor=...
q=...
follow=true
```

安全要求：

- 日志读取必须限制在受信任 log root 下，禁止任意路径读取。
- 默认对 secrets、bearer token、API key、Authorization header 做脱敏。
- UI 默认展示最近 200 行，下载完整日志需要更高权限。
- tail/follow 应有连接数和时间限制。

### 6.6 Runtime Controls

服务运行控制包括：

- 查看进程状态、goroutine 数、内存、磁盘、打开文件数。
- graceful shutdown 或 restart 请求。
- reload runtime config。
- 清理过期 async jobs。
- 清理临时文件。
- 执行 readiness refresh。

建议 API：

```text
GET  /api/v1/admin/runtime/status
POST /api/v1/admin/runtime/reload
POST /api/v1/admin/runtime/shutdown
POST /api/v1/admin/runtime/restart
POST /api/v1/admin/runtime/cleanup
```

`shutdown` 和 `restart` 默认可先只设计，不实现；如果实现，应要求二次确认和 owner 权限。

### 6.7 Security

安全页展示和管理：

- admin secret 状态和轮换提醒。
- token secret 状态和轮换提醒。
- TLS 状态、证书有效期、证书链检查。
- insecure HTTP 风险提示。
- local bash 是否启用、绑定 tenant/user 是否完整。
- direct SSH 是否启用、file transfer 是否启用。
- sandbox 是否启用。
- 最近高危审计事件。
- auth limiter 状态。

建议 API：

```text
GET  /api/v1/admin/security/posture
POST /api/v1/admin/security/rotate-admin-secret
POST /api/v1/admin/security/rotate-token-secret
POST /api/v1/admin/security/check-tls
GET  /api/v1/admin/security/risk-events
```

secret 轮换可以第一阶段仅输出操作计划，因为环境变量或 service manager 往往是实际 secret 来源。

### 6.8 Backup and Migration

复用现有 export/import/snapshot 能力，并在 Admin Web 做成操作向导：

- 创建全量 snapshot。
- 创建 tenant/user scoped snapshot。
- 列表、下载、删除、恢复。
- prune retention。
- import precheck。
- restore dry-run。

复用接口：

- `GET /api/v1/admin/export`
- `POST /api/v1/admin/import`
- `GET /api/v1/admin/snapshots`
- `POST /api/v1/admin/snapshots`
- `POST /api/v1/admin/snapshots/prune`
- `GET /api/v1/admin/snapshots/{snapshotId}`
- `POST /api/v1/admin/snapshots/{snapshotId}/restore`
- `DELETE /api/v1/admin/snapshots/{snapshotId}`

### 6.9 Tenants & Users

Admin Web 必须提供多租户管理视图。管理员可以查看全部租户、查看每个租户下的用户，并对租户或用户执行新增、暂停、恢复、删除等生命周期操作。

#### 6.9.1 租户列表和详情

租户列表展示：

- tenant id、name、status、created_at、updated_at。
- 用户数、credential 数、instance 数、session/run/message 统计。
- 数据占用估算：config、memory、skills、MCP、knowledge、records、snapshots。
- 最近活动时间、最近失败 run、最近高危 audit。
- delete protection / managed 标记。

复用或扩展现有 API：

```text
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}
PATCH /api/v1/admin/tenants/{tenantId}
GET  /api/v1/admin/tenants/{tenantId}/summary
GET  /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
DELETE /api/v1/admin/tenants/{tenantId}
```

建议新增显式状态动作，避免仅靠 PATCH 表达高风险生命周期：

```text
POST /api/v1/admin/tenants/{tenantId}/pause
POST /api/v1/admin/tenants/{tenantId}/resume
POST /api/v1/admin/tenants/{tenantId}/archive
POST /api/v1/admin/tenants/{tenantId}/restore
```

#### 6.9.2 用户列表和详情

在租户详情下展示用户列表。也保留跨租户用户搜索。

用户列表展示：

- user id、email、display name、status、created_at、updated_at。
- credential 数、instance 数、session/run/message 统计。
- config 是否完整、LLM provider 是否可用、最后一次 ready 状态。
- skill/MCP/knowledge/record 数据大小估算。
- 最近活动时间、最近 failed run、最近 audit。

复用或扩展现有 API：

```text
GET  /api/v1/admin/users
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}
PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}
```

建议新增显式状态动作：

```text
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/pause
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/resume
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/archive
POST /api/v1/admin/tenants/{tenantId}/users/{userId}/restore
```

#### 6.9.3 暂停语义

暂停租户：

- 该租户下所有用户无法签发新 bearer token。
- 已有 token 可以立即失效，或按配置进入宽限期。
- 新 run 不允许创建，已有 run 可选择 cancel 或 allow-to-finish。
- Admin 仍可查看和导出该租户数据。

暂停用户：

- 该用户无法签发新 bearer token。
- 新 instance/session/run 写操作被拒绝。
- Admin 仍可查看、导出、恢复或删除该用户数据。

建议暂停请求体：

```json
{
  "reason": "billing overdue",
  "revoke_active_tokens": true,
  "cancel_running_runs": false
}
```

#### 6.9.4 删除和数据清理

删除租户或用户必须是强提示、可预检、可 dry-run 的危险操作。删除后需要清理对应数据目录和索引记录。

删除用户应清理：

- user metadata、credentials、config。
- instances、sessions、messages、runs、run events。
- memory、skills、MCP config/runtime state、knowledge sources/index、structured records。
- async jobs、uploads、temporary files。
- user scoped snapshots，默认保留或删除应由请求参数决定。
- audit events 默认保留脱敏摘要，不建议物理删除；可通过 `purge_audit=true` 显式清理。

删除租户应清理：

- tenant metadata。
- 该租户下所有 users 及其上述全部数据。
- tenant scoped knowledge、skill-source override、policy override、snapshots。
- async jobs、uploads、temporary files。
- audit events 默认保留脱敏摘要。

删除请求必须先调用 `delete-check` 或 `retire-plan`，UI 显示影响范围：

```json
{
  "resource": "tenant/tenant_xxx",
  "can_delete": true,
  "blocked_by": [],
  "warnings": ["2 running runs will be cancelled"],
  "counts": {
    "users": 12,
    "credentials": 18,
    "instances": 42,
    "sessions": 310,
    "messages": 12048,
    "skills": 37,
    "mcp_servers": 9,
    "knowledge_sources": 86,
    "snapshots": 4
  },
  "estimated_bytes": 2147483648
}
```

删除请求体建议：

```json
{
  "confirm": true,
  "confirmation_text": "DELETE tenant_xxx",
  "dry_run": false,
  "create_snapshot_before_delete": true,
  "delete_snapshots": false,
  "purge_audit": false,
  "cancel_running_runs": true,
  "reason": "tenant offboarding"
}
```

安全要求：

- UI 必须显示红色危险区域、影响数量、数据不可恢复提示。
- 必须输入确认短语，例如 `DELETE tenant_xxx` 或 `DELETE user_xxx`。
- 默认建议先创建 snapshot。
- 删除应进入 async job，避免 HTTP 请求长时间阻塞。
- 删除过程中应分阶段记录进度，可恢复地标记 `deleting` 状态。
- 删除完成后写入 audit：`tenant.deleted`、`user.deleted`、`tenant.data_purged`、`user.data_purged`。
- 如果部分清理失败，资源进入 `delete_failed` 状态，并展示剩余路径和补救动作。

#### 6.9.5 UI 交互

`Tenants & Users` 页面建议结构：

```text
Tenants list -> Tenant detail -> Users tab -> User detail
```

租户详情 tabs：

```text
Overview | Users | Credentials | Instances | Usage | Data | Audit | Danger Zone
```

用户详情 tabs：

```text
Overview | Credentials | Instances | Config Status | Skills & MCP | Knowledge | Data | Audit | Danger Zone
```

`Danger Zone` 包含暂停、恢复、归档、删除。删除按钮默认禁用，必须先完成 delete-check 并展开影响清单。

### 6.10 Global Skill and MCP Governance

Admin Web 应能管理非用户实例私有的 skill/MCP 策略：

- 全局 skill source。
- tenant/user skill source override。
- capability market policy。
- 禁用高危 skill action。
- 本地 MCP server 默认是否允许。
- MCP 启动是否必须走 sandbox。
- MCP server 全局 allowlist/denylist。

现有 skill source admin API 可直接复用。

建议新增策略 API：

```text
GET  /api/v1/admin/execution-policy
PUT  /api/v1/admin/execution-policy
POST /api/v1/admin/execution-policy/validate
```

### 6.11 Scheduler and Jobs

管理 scheduler 与 async jobs：

- scheduler enabled、tick interval、worker 数。
- 当前 scheduled tasks。
- 上次执行时间、下次执行时间、失败次数。
- async jobs 列表、取消、删除、清理。

建议 API：

```text
GET  /api/v1/admin/scheduler/status
GET  /api/v1/admin/scheduler/tasks
POST /api/v1/admin/scheduler/tasks/{taskId}/pause
POST /api/v1/admin/scheduler/tasks/{taskId}/resume
POST /api/v1/admin/scheduler/tasks/{taskId}/run-now
GET  /api/v1/admin/jobs
POST /api/v1/admin/jobs/cleanup
```

用户 bearer token 的 `/api/v1/jobs` 继续只看当前用户 jobs；admin jobs 可以跨 tenant/user 查看。

## 7. 页面结构

建议 Admin Web 左侧导航：

```text
Overview
Service Config
Sandbox
Logs
Security
Tenants & Users
Credentials
Execution Policy
MCP & Skills
Scheduler & Jobs
Snapshots
Audit
Metrics
Diagnostics
```

关键页面说明：

- `Overview`：只读为主，展示健康状态、告警、最近错误。
- `Service Config`：表单 + effective source + restart required 标记。
- `Sandbox`：检测结果、当前后端、profile 编辑、install plan、smoke test。
- `Logs`：source selector、level filter、tail、search、download、rotate。
- `Security`：风险姿态和 secret/TLS/local execution 检查。
- `Language`：中英文切换，未登录 setup 页面和登录后管理页面均可使用。
- `Execution Policy`：local bash、MCP、skill、SSH、network 策略总览。
- `Diagnostics`：ready report、dependency check、filesystem check、sandbox smoke test、LLM provider quick check。

## 8. Admin API 命名规范

- 所有服务级能力放在 `/api/v1/admin/...`。
- 资源名使用复数，例如 `/logs`、`/snapshots`、`/sandbox/profiles`。
- 动作用子路径，例如 `/reload`、`/rotate`、`/smoke-test`、`/validate`。
- 高风险写操作必须支持 `dry_run` 或 `print_only`。
- 高风险写操作必须返回 `audit_event_id`。

统一错误响应：

```json
{
  "error": "sandbox smoke test failed",
  "code": "SANDBOX_SMOKE_TEST_FAILED",
  "details": {
    "backend": "bwrap",
    "stderr": "...redacted..."
  }
}
```

## 9. 存储设计

建议新增：

```text
MACLAW_DATA_ROOT/
  state/
    service_config.json
    sandbox_profiles.json
    sandbox_status_cache.json
  logs/
    maclawsrv.log
    maclawsrv.err.log
    sandbox.log
    scheduler.log
    access.log
```

如果在 macOS pkg 或 Linux systemd 部署中日志写入 `/Library/Logs/MaClawSrv` 或 `/var/log/maclawsrv`，Admin Web 应通过 service config 暴露实际 log root。

## 10. Sandbox Runner 集成点

代码层面建议只抽一层 runner，不改变上层 tool 协议。

需要接入的现有执行点：

- 本地 bash：`corelib/agent/tools_local.go` 的 `ToolBash`。
- 本地 MCP server：`corelib/agentservice/mcp.go` 的 `localMCPClient.Start`。
- Skill bash step：`corelib/agentservice/skill_integration.go` 的 `executeBashCommand`。

推荐接口：

```go
type CommandRunner interface {
    CommandContext(ctx context.Context, spec CommandSpec) (*exec.Cmd, error)
}

type CommandSpec struct {
    Entrypoint string // local_bash, local_mcp, skill_step
    Command    string
    Args       []string
    Dir        string
    Env        []string
    Workspace  string
    Profile    string
}
```

`DirectRunner` 保持现状，`SandboxRunner` 只改变最终 argv，例如把真实命令包成 `bwrap ... -- real-command` 或 `sandlock run ... -- real-command`。

## 11. 安全原则

- Admin Web 默认只监听 loopback；远程访问必须启用 TLS 或反向代理认证。
- 所有 admin 写操作写 audit。
- 所有 shell/install 类能力默认 `print_only`，必须显式确认才执行。
- 日志和错误输出统一脱敏。
- Sandbox strict 模式下，如果检测不到可用后端，本地 bash、本地 MCP、skill step 应 fail closed。
- 非 strict 模式下，可以 fallback 到 direct，但 UI 必须显示红色风险状态。
- 服务级配置更新要返回 diff、source、restart_required。

## 12. 分阶段计划

### Phase 1: 只读 Admin Web + Sandbox Doctor

- Overview、Readiness、Metrics、Alerts。
- Service Config effective view。
- Sandbox detect/status/smoke-test。
- Logs sources + recent errors + tail。
- 复用现有 tenants/users/credentials/audit/snapshots 页面。

### Phase 2: 可写服务级配置

- `service_config.json`。
- service-config schema/get/put/validate/effective。
- sandbox profiles get/put/validate。
- execution policy get/put/validate。
- 所有写操作 audit。

### Phase 3: Sandbox Runner 生效

- 接入 local bash。
- 接入 skill step。
- 接入 local MCP server。
- 增加 sandbox execution events。
- strict/fallback 策略生效。

### Phase 4: 运维增强

- log rotate/download/search。
- scheduler admin。
- admin jobs 跨用户视图。
- runtime reload/cleanup。
- secret rotation plan。

## 13. 最小 API 清单

第一阶段建议至少实现：

```text
GET  /api/v1/admin/bootstrap/status
POST /api/v1/admin/bootstrap/initialize
POST /api/v1/admin/auth/login
POST /api/v1/admin/auth/logout
GET  /api/v1/admin/auth/me
GET  /api/v1/admin/tenants
POST /api/v1/admin/tenants
GET  /api/v1/admin/tenants/{tenantId}/users
POST /api/v1/admin/tenants/{tenantId}/users
GET  /api/v1/admin/tenants/{tenantId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check
GET  /api/v1/admin/tenants/{tenantId}/retire-plan
GET  /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan
GET  /api/v1/admin/service-config/effective
GET  /api/v1/admin/sandbox/status
POST /api/v1/admin/sandbox/detect
POST /api/v1/admin/sandbox/smoke-test
POST /api/v1/admin/sandbox/diagnose
GET  /api/v1/admin/sandbox/install-plan
GET  /api/v1/admin/logs/sources
GET  /api/v1/admin/logs/errors/recent
GET  /api/v1/admin/logs/{source}/tail
GET  /api/v1/admin/security/posture
```

这些接口足以支撑一个有价值的 Admin Web 首版，同时不会立刻改变 `MaClawSrv` 的执行路径。

## 14. 决策建议

Admin Web 应先做“观察和诊断”，再做“写配置”，最后做“执行路径接管”。Sandbox 也应按这个顺序落地：

```text
detect/status -> install-plan -> profile validate -> smoke-test -> runner integration -> strict mode
```

这样可以把风险控制在可回滚范围内，并且让管理员在启用 sandbox 前看到当前机器到底支持什么。













