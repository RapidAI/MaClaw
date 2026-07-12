# MaClaw Mobile 混合产品与架构设计

状态：已定稿（2026-07-11）  
范围：Mobile App + Tenant Hub 协作；不复刻 maclawsrv 重量级隔离。

---

## 1. 产品总则

### 1.1 短任务 / 长任务

| | 短任务 | 长任务 |
|--|--------|--------|
| 耗时 | 约 &lt; 15～30s，用户可盯着 | &gt; 30～60s 或不固定 |
| 交互 | 当前页完成 | 可离页；任务卡 + 通知 |
| 执行 | 手机控制面 + Hub 同步 API / 系统能力 | Hub / 员工机后台 job |
| 典型 | 系统语音听写、轻问答、局部改稿、短命令确认 | 解析/OCR、全文 AI、导出、员工重活、SSH 长任务 |

**一句话：轻的短的当场办；重的长的丢服务器后台，手机发起与查看。**

### 1.2 手机上的「AI」与 Hub 完整 Agent

- **手机主能力：系统语音识别 / 语音输入**（`speech_to_text` → OS STT）。
- **手机不做**：内嵌 corelib / 自研 ASR / 完整 agent runtime / 本地 Skill·MCP 宿主。
- **Hub 官方 Mobile 服务（`assistant_mode=official`）= 完整 Agent，对齐 MaClaw GUI 能力面**：
  - Skill 调用与策略（与 GUI 同一套 skill 目录/市场/授权模型，按租户与用户生效）
  - MCP 工具与密钥绑定（Hub 侧 secrets / bindings，不把 Key 下发手机）
  - 工具策略、审批、权限边界（与 GUI agent 一致的可配置权限）
  - 多步 agent loop、长任务可升级为后台 job
- **手机角色**：控制面与结果面（发消息、看流式/任务卡、确认高风险、绑定文档对象）；**不削弱 Hub 侧 Agent 完整性**。
- **未开通官方时**：不走 Hub 完整 Agent；第一栏改为自己的**数字分身**（员工机上的能力，非 Hub 云端完整 skill/MCP 面）。

### 1.3 数据隔离（简单版）

- Token → `tenant_id` + `user_id`（禁止客户端自报身份）。
- 所有用户资源行：`tenant_id` + `owner_user_id`。
- 磁盘：`{dataDir}/mobile/tenants/{tenant}/users/{user}/docs|jobs/...`。
- 不引入 per-user 进程或 maclawsrv 级沙箱。

---

## 2. 底栏信息架构

登录后主界面底栏 **五项**（顺序固定；`features.documents=false` 时隐藏文档）：

| 顺序 | Tab | 路径 | 职责 |
|------|-----|------|------|
| 1 | **AI助手** / **数字分身** | `/assistant` | 见 §3 模式切换 |
| 2 | **文档** | `/documents` | 查看/编辑 Hub 文稿；系统分享导入；导出与轻编辑 |
| 3 | **后台** | `/tasks` | 长任务统一列表（文档解析/导出、助手、员工、SSH…） |
| 4 | **数字员工** | `/employees` | 简化列表 → 多 Tab 交谈（免费仅自己的） |
| 5 | **我的** | `/account` | 账号、授权卡、语音语言、通知、配额 |

### 2.1 「文档」一级 Tab

- 文稿落在 **当前登录用户的 Hub 文档空间**（与电脑端 MaClaw GUI **同源**轻量库，非完整网盘；列表 API `GET /api/mobile/documents/drafts`）。
- 可打开编辑、摘要/润色等继续处理、导出；**系统分享**可把正文发到微信等。
- 系统分享（微信/文件管理器 → App）优先进入 **文档** Tab。
- 导入/OCR/导出等 **长任务进度** 仍在 **后台** Tab 统一查看。
- 助手侧 **不再提供**「绑定文档 / 应用到绑定文档」（产品已移除）。
- `features.documents=false` 时隐藏 Tab，深链 `/documents` 回落到已启用页。

---

## 3. AI 助手模式：官方 vs 数字分身

由 bootstrap 决定 `assistant_mode`：

| 条件 | `assistant_mode` | 第一栏表现 |
|------|------------------|------------|
| 官方 Mobile 可用（官方 credits 或桌面委托就绪） | `official` | **AI助手**：Hub **完整 Agent**（Skill / MCP / 权限，对齐 GUI） |
| 未使用 / 不可用官方 | `digital_twin` | 与 **自己的数字分身** 交谈（执行在用户电脑，非 Hub 云端完整 Agent） |

#### Hub 完整 Agent 与「短/长」

| 交互 | 说明 |
|------|------|
| 短 | 对话内完成的一轮/少量 tool 调用；SSE 流式回手机 |
| 长 | 多步 skill/MCP、重 I/O、长跑 → 后台 job +「后台」Tab |
| 权限 | 与 GUI 相同：skill 启用策略、MCP 绑定、危险操作确认；手机只做确认 UI |

### 3.1 `digital_twin` 行为

- 仅连接 **owner = 当前用户** 的数字员工（分身）。
- 分身 **在线且可接单** → 可多轮交谈 / 派任务到本机。
- 分身 **离线或没有** → 发送区禁用；引导：
  1. **购买/激活服务授权卡** → 开通 MaClaw 官方 Mobile；
  2. **打开电脑上的 MaClaw** → 让分身上线。

### 3.2 免费档数字员工范围

- 免费：列表与交谈 **仅自己的分身**。
- 付费/企业：可扩展租户共享、公共池等（后置）。

### 3.3 Token 与完整 Agent 成本

- Hub 完整 Agent（含 skill/MCP 多步）**必绑计费账户**（默认官方 credits）。
- Skill/MCP 调用本身可能叠加模型与外部 API 成本；usage 记在同一用户/租户。
- 无官方授权 **不启动** Hub 完整 Agent（避免白嫖 token 与 MCP）。
- 分身侧：执行在用户电脑；是否再调云端模型由员工机/GUI 策略决定，与「官方 Mobile 完整 Agent」分账。

---

## 4. 文档

### 4.1 定位

应急轻文稿：模板起草、多格式导入 → 统一 Markdown 草稿 → 轻改 / AI → 导出分享。  
**不是**手机原生 Office/WPS。

### 4.2 查看 vs 修改

- **查看**：手机渲染。
- **修改生效**：一律 Hub（人改 PATCH / AI 写回 / 解析写回 / 导出）。
- 手机可有编辑框，提交后以 Hub `revision` 为准。

### 4.3 格式

| 格式 | 手机操作 |
|------|----------|
| txt/md/csv/json/log | 导入即草稿 |
| docx/xlsx/pdf | 上传 → 抽取/解析 → 草稿 |
| 图片 | OCR 后台 → 草稿 |
| doc/xls | 排队解析 |
| pptx/wps | 默认不支持；后置抽大纲/转换 |

手机编辑的是 **草稿对象**（`document_id`），不是二进制版式。

### 4.4 文档 × AI 助手

- 可绑定为操作对象 `document_id`。
- 局部改 → 短（sync）；全文/未 ready/导出 → 长（async job）。
- 超时短路径可升级为后台 job。

### 4.5 配额

- 默认共享文档空间 **100MB / 用户**（`document_quota_bytes`）。
- 付费扩容；单文件上限 `max_upload_bytes`（默认可与配额对齐或更小）。
- 上传前：`used + size ≤ quota`。

### 4.6 Hub 目录

```text
{HUB_DATA_DIR}/mobile/tenants/{tenant_id}/users/{user_id}/
  docs/{document_id}/source|derived|export/
  jobs/{job_id}/   # 可选
```

元数据与权限在 DB；路径仅服务端按身份拼接。

---

## 5. 后台（任务）Tab

统一展示当前用户的长任务：

- 文档导入 / OCR / 导出  
- 助手长任务  
- 数字员工任务  
- SSH 长命令 / 文件操作（若启用）

字段建议：`job_id, kind, title, status, progress, updated_at, deep_link`。  
底栏角标：进行中数量。

---

## 6. 数字员工

- 简化列表：名称、在线状态、一行说明。
- 点击交谈；**多 Tab** 可同时与多个员工会话（免费仅自己的多个分身）。
- 对话内嵌任务卡；重活在员工机/Hub 后台。
- AI 助手 handoff → 打开对应员工会话并预填。

---

## 7. 官方服务与 entitlement

| 概念 | 含义 |
|------|------|
| MaClaw 官方服务 | 官方 Hub + 官方 LLM 计费 |
| Mobile 服务 | 手机侧能力（助手 agent、文档 AI、后台 job 等） |
| 默认 | 手机走官方；有授权才耗 token |

Bootstrap 建议字段：

```json
{
  "assistant_mode": "official" | "digital_twin",
  "entitlements": {
    "mobile_official": true,
    "mobile_agent": true,
    "document_ai": true,
    "shared_employees": false,
    "plan": "free"
  },
  "limits": {
    "max_upload_bytes": 104857600,
    "document_quota_bytes": 104857600,
    "document_quota_used_bytes": 0,
    "max_export_jobs": 3
  },
  "llm_access": { "...": "existing" },
  "features": {
    "assistant": true,
    "tasks": true,
    "documents": true,
    "digital_employees": true,
    "backend_ssh_sessions": true,
    "push_notifications": false
  }
}
```

客户端：`assistant_mode` 优先用服务端；缺省时由 `isMobileLlmConfigured` 推导。

---

## 8. SSH（目标方向）

- 目标：Hub 执行、不依赖用户 PC 在线；凭据 Hub 加密托管。
- 现状：桌面 worker claim；演进双模式 `hub_exec` / `desktop_exec`。
- 短命令交互；长命令进后台 Tab。

---

## 9. 实施分期

### Phase A（本文开工）

1. 设计文档入库  
2. 底栏：AI助手 / 后台 / 数字员工 / 我的  
3. Bootstrap：`assistant_mode`、`entitlements`、文档配额字段  
4. `/tasks` 任务中心壳 + 文档路由保留  
5. 未官方：`digital_twin` 入口与离线引导（不强制卡死 llm-setup）

### Phase B（进行中）

- **Hub 官方助手对齐 GUI 完整 Agent**：同一 skill / MCP / 权限与审批管线（Mobile 仅会话与确认 UI）  
  - **已接入**：`corelib/agentservice.CoreAgentExecutor` + `agent.RunLoop`  
  - **已挂载**（与 MaClawSrv 同款）：`NewSkillToolBridge` + `NewMCPToolBridge` + **Knowledge SQLite（FTS + 可选本地 Gemma 向量）**  
  - Hub 启动 `InitMobileCoreAgent(runtimeDataDir)`；用户态 `EnsurePrincipal`  
  - **技能种子**：`{data}/skills` 市场 JSON / 技能包 → 用户 skills 目录（幂等 marker；`POST .../skills/reseed` 可强制）  
  - **技能列表 API**：`GET /api/mobile/agent/skills`、`POST /api/mobile/agent/skills/reseed`  
  - AppConfig 注入 HubCenter 候选与本 Hub `SkillHubURLs`，便于 skill 搜索/安装  
  - **MCP 管理 API**：`GET/PUT /api/mobile/agent/mcp`（remote only；secret 不回传）  
  - **MCP 健康探测**：`GET/POST /api/mobile/agent/mcp/health`（App 侧懒探测）  
  - **知识状态 API**：`GET /api/mobile/agent/knowledge/status`（mode=`fts`|`vector+fts`）  
  - **知识手写入库**：`POST /api/mobile/agent/knowledge/ingest`（手机备忘 → knowledge `SaveText`）  
  - **Mobile「我的」**：`AccountAgentStatusCard` + `AccountMcpScreen` + `AccountSkillsScreen` + 写入备忘  
  - **Mobile「后台」**：官方 Agent 摘要卡（知识库 + MCP + 技能数）  
  - **文档入库**：草稿创建/更新/处理/OCR ready → knowledge `SaveText`（best-effort）  
  - 技能/MCP/知识落在 `data/.../mobile-agent`  
  - LLM 仍走 Hub 官方 `/api/llm/v1/chat/completions`（viewer 计费）或桌面委托  
  - 旧版 web-only 小循环仅作 fallback  
- **统一 job 列表 API**：GET /api/mobile/jobs（文档导入/导出、数字员工、SSH；字段 job_id/kind/title/status/progress/updated_at/deep_link）  
  - Mobile「后台」统一任务卡消费该 API  
- **文档作助手操作对象（sync）**：搜索请求 document_id；Hub 注入 owned draft Markdown；回答可用 maclaw-document-edit 围栏，App「应用到绑定文档」写回 PATCH  
- **助手长任务 async job**：  
  - POST /api/mobile/agent/jobs 入队；GET .../jobs/{id} 查结果  
  - POST /api/mobile/search + async:true → 202 + job_id  
  - 纳入 GET /api/mobile/jobs（kind=assistant）  
  - App 发送旁「后台执行」+ 轮询写回对话 + 通知  
  - **SSE 超时自动升级**：交互流超过 35s → 自动 enqueue 后台 job，用户可离页  
- **文档处理 async job**：  
  - POST .../drafts/{id}/process + async 或正文 >= 6000 字 → 202 + job_id  
  - GET /api/mobile/documents/process-jobs/{jobId}；kind=document_process 入统一 jobs  
  - 短文档仍同步 processed  
- **SSE 中途「转后台」**：生成中可手动 handoff（不依赖 35s 超时）  
- **GUI 顶栏文稿共享**：  
  - Hub GET /api/mobile/documents/drafts（列表）与 .../drafts/{id}（正文）  
  - 桌面 ListMobileDocumentDrafts / GetMobileDocumentDraft + 顶栏「Mobile 文稿」面板  

### Phase C（已基本完成）

- **Hub SSH 凭据库 + hub_exec**  
  - Vault：GET /api/mobile/ssh/vault 列表；PUT/GET/DELETE .../vault/{profileId}（AES-GCM；不回传密钥）  
  - 会话：exec_mode=hub_exec|desktop_exec；hub_exec 短命令 Hub 直连  
  - Mobile：档案列表钥匙按钮录入/清除 Hub 密钥；有 vault 时连接优先 hub_exec  
  - Bootstrap：plan + hub_ssh_exec；账号页展示套餐与 Hub SSH 能力  
- **付费套餐 / 授权卡**  
  - Bootstrap 拉取 service registry grants：plan=service_card、credits_*、has_service_card_grant  
  - 有可用额度时文档配额升至 500MiB、max_export_jobs=10  
  - Mobile 账号页：套餐/服务授权摘要 +「兑换服务卡」+「购买服务卡」（card-store products/orders）  
  - **文档配额真实占用**：draft markdown + upload source 计 used；create/update/upload 超限 507  
  - 强制限额与 bootstrap 一致：有可用 credits 时 500MiB，否则 100MiB  
  - GET /api/mobile/documents/quota  
  - Mobile：`documentQuotaProvider` 实时拉取；后台页进度条 + 账号页「额度与限制」合并 bootstrap/live  
  - 文档 create/update/upload 成功后 invalidate 配额；HTTP 507 映射中文「文档空间不足」  
  - **hub_exec 长命令异步**：管道/journalctl 等 202 + 后台执行；短 input 直跑  
  - **hub_exec 持久连接 / 交互 shell**：按 session 复用 TCP；PTY shell 保 cwd/env；input 优先 shell，失败回退 oneshot；空闲 10min GC；DELETE session 立即 closed + 释放 live 资源  
  - **hub_exec 控制面**：interrupt→shell Ctrl-C；reconnect→关 live 再 probe；attach→重开 shell；均即时完成不排队桌面  
  - **hub_exec 任务终止**：kill 取消 in-flight `session.Run`（context）；queued 直接 cancelled；wait 仅轮询状态  
  - **hub_exec 文件**：stat/list/**read(预览≤64KB)** 在 Hub 直跑；upload/download 仍需 desktop_exec  
  - Mobile 会话卡区分 hub_exec / desktop_exec 文案与关闭态；列表 subtitle 显示 exec_mode；关闭后 `connected=false`  
  - 后台统一任务 kind/status 中文标签（导入/导出/助手/SSH…）  
  - **免费档数字员工**：列表 `scope=own`，仅 `OwnerUserID` 匹配 + 本机绑定机器；`shared_employees` 企业池后置  

### Phase D（已基本完成）

1. hub_exec PTY/输出实时流与终端 UX  
   - 长任务 `session.Run` 分片 onPartial → session.output_chunk + realtime；状态 `hub_streaming`  
   - Mobile 终端面板：深色 monospace、自动滚底、逐 chunk 刷新  
   - **WS `pty_input`**：realtime 双向；`pty_ack` 回执；Mobile 优先 WS，HTTP 回退；PTY 栏显示 WS/HTTP  
   - 账号页实时通道在线态（sender 存活）  
2. hub_exec 小文件中转：download ≤2MiB → Hub blob + 限时 URL；本机保存/分享  
3. `shared_employees`：service_card/授权卡+credits → shared；free = own  
4. 付费套餐权益表：`mobilePlanCapsFor` 单源 + env 覆盖 + `GET .../entitlements/caps` + 账号页实时卡片  
5. 统一 jobs 筛选 + 未关闭 `ssh_session`  
6. SSH 完成/下载通知  
7. raw PTY：Tab/Enter/Esc/^C/^D/方向键；输出 HTTP<->realtime 去重  

### Phase E（已完成）

1. **管理向 caps 覆盖（完成）**  
   - `PUT /api/mobile/entitlements/caps` + `X-Maclaw-Caps-Admin-Token`（env `MACLAW_MOBILE_CAPS_ADMIN_TOKEN`）  
   - 进程内 runtime override，优先于 env；`clear:true` 清空  
   - GET caps 返回 `runtime_overrides` + chunk 元数据  
   - **Mobile 账号页**：套餐权益卡 →「运维覆盖」sheet（token 仅内存、字段 partial apply / 清空）  
2. **PTY 二进制帧 MCP1（完成）**  
   - realtime `pty_input` + HTTP `/input` 支持 `data_b64`（std/raw base64，≤16KiB）  
   - **MCP1 二进制 WS 帧**（magic `MCP1`）：`pty_in` / `pty_out` / `pty_ack`  
   - 连接后 `hello` caps=`pty_binary`；Hub `ready` 声明 caps；输出 chunk **JSON+二进制双写**  
   - App：raw/命令优先发 MCP1 binary；解析 binary `pty_out` 为会话 output_chunk  
   - 无 binary 时仍可用 JSON `data_b64` / HTTP 回退  
3. **hub_exec 大文件分块下载（完成）**  
   - ≤2MiB 单次 base64；更大文件 `dd` 512KiB 分块 base64 组装（`mobileHubFileDownloadPlan`）  
   - 绝对上限默认 **32MiB**（`MACLAW_MOBILE_CAP_HUB_FILE_DOWNLOAD_MIB` / runtime）  
   - 分块进度：file op `running` + message 含 **`a/b bytes`**（可定比）+ realtime；transcript `[download n/m]`  
   - 完成后限时 blob URL（`download_url`）供本机保存/分享  
   - 账号页展示 chunk 元数据与 runtime 覆盖  
4. **企业共享员工可见性（完成）**  
   - 列表 API `scope`/`shared_employees`；`mobileFilterEmployeesForViewer` 单源过滤  
   - **组可见性**：shared 池解析 `VisibleGroupIDs`（security 组路径）；**owner 始终可见自己的 VE**  
   - free/own：仅 owner + 绑定机器；组限制不挡 owner  
   - App 员工页 / 分身入口：横幅与空态按 shared/own 分文案（不在 shared 时写免费档口吻）  
5. **推送通道与后台完成通知（完成，FCM SDK 残留）**  
   - 文件下载分块进度：列表 running 进度条（解析 `a/b bytes`）+ 系统进度通知  
   - **离线 pending 队列（已落盘）**：SSH/文档/员工/助手 job 终态入队；App 恢复后拉取 → 本地通知 → ack  
   - 持久化：并入 `MACLAW_MOBILE_STATE_PATH`；未设置时默认 `{runtimeDataDir}/mobile/state.json`  
   - **设备注册** `POST /api/mobile/push/devices`（platform=device|fcm|apns|hms）；登录后自动注册并落盘  
   - **远程投递**仅当 env 配置时 bootstrap `push_notifications:true`（webhook / FCM server key / log-only）  
   - **残留（非阻塞）**：Firebase Messaging / APNs SDK 真 device token（现用 device token + pending/webhook）  

---

## 10. 相关代码入口

| 区域 | 路径 |
|------|------|
| 底栏 | `lib/shared/app_shell.dart` |
| 路由 | `lib/app.dart` |
| Bootstrap | `lib/core/api/mobile_bootstrap.dart` |
| Hub bootstrap | `hub/internal/httpapi/mobile_handlers.go` `mobileBootstrapPayload*` |
| 文档 | `lib/features/documents/` |
| 员工 | `lib/features/digital_employees/` |
| 语音 | `lib/features/assistant/assistant_voice_input.dart` |

---

## 11. 决策记录摘要

1. 短手机 / 长服务器后台。  
2. 语音优先 OS STT。  
3. 文档：手机看、Hub 改；轻量跨端同步非网盘。  
4. 存储按 tenant→user；默认 100MB。  
5. 官方授权才耗 Hub LLM。  
6. 免费仅自己的数字分身。  
7. 未官方时第一栏改为分身；离线引导买卡或开电脑。  
8. 底栏五项：助手(分身) / 文档 / 后台 / 数字员工 / 我的。
