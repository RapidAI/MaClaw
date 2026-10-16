# MaClaw Bot 移动端（Flutter）功能设计

> 目标：用 Flutter 实现一个移动端 Bot 客户端，连接 MaClaw Hub 中的 Bot，支持查看 Bot 列表、创建新 Bot、与 Bot 聊天、查看 Bot 工作状态。
> 参考交互：grokbot 式「左侧会话列表 + 右侧主聊天窗」布局（附件截图未能解析像素细节，布局细节列为可调项）。
> 登录方式：邮箱 / 手机号验证码登录，与 MaClaw GUI、现有 mobile 客户端保持一致。

---

## 1. 结论先行：能直接复用的后端契约

调研结论（本设计的全部服务端依据）：

| 能力 | 现有接口 | 位置 |
| --- | --- | --- |
| Bot 能力开关 | `GET /api/v1/bots/access` | `hub/internal/httpapi/bot_user_handler.go:27` |
| Bot 列表 | `GET /api/v1/bots` | `hub/internal/httpapi/router.go:431` |
| 创建 Bot | `POST /api/v1/bots` `{name, description}` | `bot_user_handler.go:68` |
| 重命名 / 删除 | `PATCH /api/v1/bots/{id}`、`DELETE /api/v1/bots/{id}` | `router.go:433-434` |
| 发消息（同步或异步受理） | `POST /api/v1/bots/{id}/messages` `{content, phase}`，`Prefer: respond-async` 或 `?async=true` 返回 `202 {run_id}` | `bot_user_handler.go:140` |
| 轮询运行结果（工作状态核心） | `GET /api/v1/bots/{id}/runs/{runID}`，未完成返回 `202`，完成返回完整回复 | `bot_user_handler.go:209` |
| 远程桌面 / 需人工接管 | `GET /api/v1/bots/{id}/desktop`、`POST /api/v1/bots/{id}/desktop`（hold / release） | `bot_user_handler.go:237,264` |
| 机密填写回填 | `POST /api/v1/bots/{id}/secret-fill` | `router.go:437` |

**重要约束：Bot 接口用的是 machine 认证，不是 viewer 认证。** `botMachine()` → `authenticateVEMachine()`（`ve_admin_handler.go:2162`）要求请求同时带 `Authorization: Bearer <machine_token>` 与 `X-Machine-ID: <machine_id>`。而验证码登录返回的是 **viewer_token**（`miniprogram_auth.go:430`、`identity_service.go:2005`）。移动端必须先解决「viewer token → machine token」的兑换（见 §5）。

Bot 回复数据结构（`hub/internal/botmgmt/access.go:57`）已覆盖移动端所需：`text` / `handoff` / `attention_reason` / `ask_user_*`（问题、选项、机密名）/ `images` / `files` / `novnc_url`。

---

## 2. 现有 Flutter 工程现状

- 工程：`mobile/maclaw_mobile`，包名 `maclaw_mobile`，Android applicationId `top.mypapers.maclaw.mobile`（`android/app/src/main/kotlin/top/mypapers/maclaw/mobile/MainActivity.kt`）。
- 状态管理 Riverpod、路由 go_router、网络 dio、本地库 drift(sqlite)、安全存储 flutter_secure_storage、Markdown flutter_markdown、WebSocket web_socket_channel，均已在 pubspec 中。
- **已有可直接借鉴的模块**：
  - `lib/features/auth/auth_service.dart`：手机号验证码登录（`/api/entry/resolve` 路由发现 → `/api/mobile/auth/phone/send-code` → `/api/mobile/auth/phone/verify-and-start`），并处理了「短信已发出但回执超时」这类边界。
  - `lib/features/digital_employees/`：列表页 + 聊天页 + controller + 输入历史，**结构与 Bot 客户端几乎同构**。
  - `lib/core/storage/secure_vault.dart`、`lib/features/auth/session_controller.dart`：会话持久化。
- ⚠️ **落地前必须先处理**：`mobile/maclaw_mobile/lib/**` 与 `pubspec.yaml` 在当前工作区未检出（git 标记 skip-worktree，`git ls-files -v` 显示 `S`）。开工第一步：`git sparse-checkout reapply` 或临时关闭 skip-worktree 恢复文件。

---

## 3. 信息架构与页面

底部导航 5 Tab（沿用现有 `AppShell`），Bot 作为独立一级 Tab：

```
启动 Splash
  └─ 未登录 → LoginPage（邮箱/手机号 + 验证码）
  └─ 已登录 → AppShell
        ├─ 助手 Assistant（现有）
        ├─ 数字员工 Employees（现有）
        ├─ 任务 Tasks（现有）
        ├─ Bot  ← 新增
        └─ 我的 Account（现有）
```

Bot 模块内部（横向左列表 + 右详情的二栏结构，窄屏自动切换为「列表 → 详情」两段式）：

```
BotHomePage
├── BotRail（左侧列表，可折叠）
│   ├── 搜索框
│   ├── 运行中筛选（全部 / 进行中 / 待确认）
│   ├── BotCard：头像色块、名称、描述、状态徽标、最后活跃时间、未读数
│   └── FAB「新建 Bot」
├── BotChatPane（右侧主窗）
│   ├── 顶栏：Bot 名、状态徽标、操作菜单（重命名 / 删除 / 定时任务 / 远程桌面）
│   ├── 消息流：用户气泡、Bot 气泡、Markdown、代码块、附件、图片
│   ├── 内联卡片：确认卡（plan 阶段）、提问卡（ask_user_*）、机密填写卡、图片/文件卡、接管桌面卡
│   └── 输入区：文本框、发送、语音、附件、常用语
└── 页面：BotDetailPage（宽屏右栏 / 窄屏独立页）— Bot 资料、状态、历史会话
```

新建 Bot：`BotCreateSheet`（名称必填、描述可选、可选头像色），提交后自动进入该 Bot 的聊天页。

---

## 4. 功能清单

### F1 登录（邮箱 / 手机号验证码）
1. 首屏进入 `LoginPage`，顶部 Tab 切换「邮箱 / 手机号」。
2. 输入 → 发送验证码：60s 倒计时、防重复点击、`post_send` 超时提示（沿用 `auth_service.dart` 已处理的 `sent_unconfirmed` 语义）。
3. 校验验证码 → 保存会话到 SecureVault。
4. 邮箱通道走 `POST /api/enroll/email/send-code` + `POST /api/enroll/email/verify-and-start`（`router.go:948-949`）；手机号通道走 `/api/mobile/auth/phone/send-code` + `/api/mobile/auth/phone/verify-and-start`（`router.go:954-955`）。
5. 启动时用会话校验接口复验；失效则清库回登录页。
6. 错误码中文映射：`INVALID_VERIFY_CODE`、`VERIFY_LOCKED`（429，要求重发）、`ACCOUNT_NOT_FOUND`、`ACCOUNT_INACTIVE`、`PHONE_ALREADY_REGISTERED`、`TENANT_AMBIGUOUS`、`REGISTRATION_DISABLED`、`PHONE_REGISTRATION_DISABLED`。

### F2 Bot 列表
- 首次进入先调 `GET /api/v1/bots/access`；`enabled=false` 时展示全屏说明页（展示 Hub 返回的 `message`），不进入列表。
- 列表数据来自 `GET /api/v1/bots`，字段：`id / name / description / instance_id / created_at`。
- 左滑：置顶、重命名、删除（二次确认）。
- 下拉刷新 + 进入前台自动刷新 + 空态插画。

### F3 创建 Bot
- `POST /api/v1/bots`，成功返回 201 与新 Bot，直接进聊天页。
- 前端校验：名称 1–40 字符；描述 ≤200。

### F4 聊天（核心）
- **发送**：`POST /api/v1/bots/{id}/messages`，带 `Prefer: respond-async`。
  - `202 {run_id, status:"running"}` → 进入「运行中」态，开始轮询。
  - `200 {text,...}` → 老 Hub 同步返回，直接渲染。
- **轮询**：`GET /api/v1/bots/{id}/runs/{runID}`，1s 间隔，超时/网络抖动重试 3 次，单次读超时 15s；总上限 30 分钟（对齐桌面端 `BOT_PENDING_REPLY_WAIT_MS`）。运行中退到后台时停止轮询，回前台恢复（用 run_id 续读）。
- **渲染**：Markdown + 代码块（可复制）；`images[]` base64 → 本地缓存文件后展示；`files[]` → 保存到应用目录，提供分享（`share_plus` 已有）。
- **消息本地持久化**：drift 表 `bot_messages(bot_id, message_id, role, content, created_at, ...)`，登录按用户隔离；支持断点续读与离线草稿。

### F5 工作状态查看
状态模型（由服务端信号推导，非独立接口）：

| 状态 | 判定依据 |
| --- | --- |
| `idle` 空闲 | 无在途 run，最近一次已结束 |
| `running` 运行中 | 已受理 run 未完成（`GET runs/{id}` 返回 202） |
| `waiting` 待人工 | 回复含 `handoff=true` 或 `attention_reason` 非空 |
| `ask` 待回答 | 回复含 `ask_user_question` / `ask_user_options_json` |
| `failed` 失败 | 本轮请求或轮询最终失败 |

- Bot 列表页卡片显示状态徽标；聊天页顶栏显示进行中动画 + 已耗时。
- 任务面板联动：把 `running` 的 bot 汇入现有 Tasks Tab。
- **确认交互**：`phase='plan'` 时 Bot 返回安排方案，渲染确认卡；用户点「确认」以 `phase='execute'` 续发。沿用桌面端 `phase: plan|execute` 语义（`desktopBots.ts`）。
- **接管桌面**：`handoff` 或需要登录/验证码时展示 noVNC 入口。用 `webview_flutter` 内嵌 `novnc_url`（Phase 2；Phase 1 只给跳转按钮）。轮询 `POST /desktop` 保持 hold，离开页面调 release。

### F6 Bot 管理
- 重命名 / 改描述、删除。
- 定时任务（Phase 2）：桌面端 `ArmDesktopBotSchedule` 走 GUI 本地调度器，Hub 无对应移动端接口 → 移动端 Phase 1 不做，Phase 2 需先给 Hub 补接口。

---

## 5. 关键问题：viewer token 无法直接调 Bot 接口（已解决，采用方案 A）

登录拿到的是 viewer_token，Bot 接口要 machine token。桌面端是靠 GUI 自己 enroll 拿到的 `machine_id/machine_token`（`guiapp/app_ve.go:2238`），移动端没有这一步。三个方案中采用了**方案 A**：

改 `botMachine()`（`hub/internal/httpapi/bot_user_handler.go`）让它同时接受 viewer token。viewer token 已经能解析出 `TenantID/UserID`（`identity_service.go:2035`），与 `MachinePrincipal` 同构。落地细节：

- 新增 `botAuthenticator` 接口（`AuthenticateMachine` + `AuthenticateViewer`），替换原来的 `veMachineAuthenticator`，全部 10 个 bot 处理器随之切换。
- 新增 `botViewerPrincipal()`：先试 machine（带 `X-Machine-ID` 时），失败再回退 viewer。带上 `X-Machine-ID` 但 token 已失效时也会回退，避免把用户锁在自己的 bot 外面。
- 空 user/tenant 的 `BOT_DISABLED` 检查对两条路径都保留。

回归测试见 `hub/internal/httpapi/bot_viewer_auth_test.go`：viewer token 能看到自己的 bot、看不到别人的 bot、未知 token 仍 401、原有 machine 鉴权不受影响。`go test ./hub/internal/httpapi/ -run TestBot`、`./hub/internal/botmgmt/`、`./hub/internal/auth/` 均通过。

> viewer token 有效期 30 天且滑动续期，泄露面比 machine token 大。若日后需要吊销能力，可升级为方案 B（Hub 新增 viewer→machine 凭据兑换接口），前端改动很小。

---

## 6. 模块划分（`lib/features/bots/`，已落地）

```
lib/features/bots/
├── bot.dart                    # Bot / BotReply / BotRun / BotQuestion / BotAccess + 工作状态推导
├── bot_message.dart            # 消息模型、JSON 往返、pending 老化、upsert
├── bot_api.dart                # dio 封装 /api/v1/bots 全部调用 + 异步受理/轮询 + 错误码映射
├── bots_controller.dart         # Riverpod providers、列表 CRUD、run 轮询生命周期、BotActivity
├── bots_screen.dart            # Bot Tab：左列表 + 右聊天（窄屏两段式）
├── bot_rail.dart               # 状态徽标、头像、行卡片
├── bot_chat_screen.dart        # 消息流 + 输入区 + Markdown
├── bot_inline_cards.dart       # 确认卡 / 提问卡 / 机密卡 / 接管卡 / 附件
└── bot_create_sheet.dart       # 新建与编辑 Bot
```

复用：`core/api/official_service.dart` 的 dio 工厂、`core/storage/secure_vault.dart`、`shared/surface.dart` 的 `ChatWorkspaceScaffold`/`ChatBubble`/`ChatComposerDock`/`EmptyStatePanel`、`shared/theme.dart` 的 `MaClawColors`。写法对齐 `features/digital_employees/*`。

消息持久化没有新建 drift 数据层，而是在既有 `MobileLocalStore` 上加 `bot_messages` 表与 4 个方法，与该工程其它本地数据保持一致。

---

## 7. 数据流与状态

```
BotApi(dio + SecureVault token)
   └─► botApiProvider ──► botListProvider        （列表 CRUD）
                └─► botConversationProvider(botId)  （聊天 + run 轮询）
                              ├─► MobileLocalStore.saveBotMessages
                              ├─► botLatestMessagesProvider（rail 预览）
                              └─► botActivityProvider（跨页面运行/待办状态）
```

轮询生命周期放在 controller 而非 widget 里，因此旋转屏幕、切 Tab、App 退到后台都不会重启 Hub 上已在跑的 turn。

---

## 8. 实施状态（P0 + P1 已完成）

### Hub 侧（Go 测试已跑通）
- `hub/internal/httpapi/bot_user_handler.go`：新增 `botAuthenticator` 接口与 `botViewerPrincipal()` viewer 回退。
- `hub/internal/httpapi/bot_secret_fill.go`：处理器签名同步切换。
- `hub/internal/httpapi/ve_admin_handler_test.go`：测试替身补 `AuthenticateViewer`。
- `hub/internal/httpapi/bot_viewer_auth_test.go`：4 个回归测试（viewer 能看到自己的 bot / 看不到别人的 / 未知 token 仍 401 / machine 鉴权不回退）。
- 验证：`go build ./hub/...`、`go vet`、`go test -run TestBot`、`botmgmt`、`auth` 全部通过。
  注：httpapi 包内另有 20 个既有失败（mobile core agent / group discussion / admin page），已在未改动的干净树上复现，与本次改动无关。

### Flutter 侧（`mobile/maclaw_mobile/lib/features/bots/`，新增）
| 文件 | 职责 |
| --- | --- |
| `bot.dart` | Bot / BotReply / BotRun / BotQuestion / BotDesktop / BotAccess 模型、`BotWorkState` 推导、`parseBotAskOptions` |
| `bot_message.dart` | 消息模型、JSON 往返、pending 气泡老化判定、`upsertBotMessage` |
| `bot_api.dart` | 全部 `/api/v1/bots` 流量、异步受理与轮询协议、错误码中文映射 |
| `bot_rail.dart` | 状态徽标、头像、行卡片 |
| `bot_inline_cards.dart` | 方案确认卡、提问/机密卡、桌面接管卡、图片与文件附件 |
| `bot_chat_screen.dart` | 聊天页（消息流 + 输入区 + Markdown） |
| `bot_create_sheet.dart` | 新建/编辑 Bot，含前端长度校验 |
| `bots_screen.dart` | Bot Tab：宽屏左列表+右聊天，窄屏列表→全屏聊天 |
| `bots_controller.dart` | providers、列表 CRUD、run 轮询生命周期、`BotActivity` |

改动既有文件：`mobile_local_store.dart`（新增 `bot_messages` 表 + 4 个读写方法）、`mobile_bootstrap.dart`（`MobileFeatures.bots`）、`app_shell.dart`（Bot Tab）、`app.dart`（`/bots` 路由）、`app_strings.dart`（`tabBots`）。

### 测试（已写并全部执行通过）
`test/bot_model_test.dart`、`test/bot_message_test.dart`、`test/bot_api_test.dart`、`test/bots_controller_test.dart`，共 75 个用例全通过；`test/mobile_feature_flags_test.dart` 的底部导航断言已同步 Bot Tab。

测试替身 `test/support/fake_hub.dart` 用 `HttpClientAdapter` 注入假 Hub（Dio 只有工厂构造器，不能被继承）。请求仍走完整 Dio 管线——拦截器、`validateStatus`、JSON 解码——所以测的是 App 真实走的那条路径。

### 验证结果（Flutter 3.41.5 / Dart 3.11.3）
- `flutter analyze lib`：**0 error、0 warning**，Bot 模块零告警。
- `flutter test`（Bot 相关 4 个文件）：**75 passed**。
- `flutter test`（全量）：失败集合与改动前**完全一致**，零新增回归。
  基线：516 个用例 / 63 个失败；现态：530 个用例 / 61 个失败。新增 14 个用例全过，同时消掉了 2 个原有失败。
  其余 61 个失败为仓库既有问题（release/QA 脚本、assistant/documents/account/servers 旧 test double 签名过时），与本次改动无关。

### 编译期发现并修掉的问题（装上 SDK 后暴露）
1. **`_loadedMessages()` 竞态（真实缺陷）**：`send()` 写入用户气泡时若 transcript 仍在加载，读到的是空列表，随后 `build()` 返回值会把状态覆盖，**用户刚发的消息会丢失**。聊天页在加载时输入框是启用的，用户真的能踩到。修复：所有写入先 `await future` 等加载完成再读；`_settle` 也改为读单次快照，避免跨 await 混用两个时刻的 transcript。
2. `Navigator.pop<T>` 返回 void，误当 Future await —— 改为同步 `_submit()`。
3. `FamilyAsyncNotifier.build` 的参数是 `build(String arg)`，不是无参加 `arg`。
4. `Size.isCompact` 不存在；720 断点在两处重复定义 —— 提取为 `botWideLayoutBreakpoint`。
5. `use_build_context_synchronously`：在第一个 await 前捕获 `ScaffoldMessenger` / `Navigator`，不再跨异步间隙持有 context。
6. 测试替身 `extends Dio` 全部错误（Dio 无生成式构造器、override 签名不符）—— 改为 `HttpClientAdapter`。

### 已实现的关键行为
- 发消息带 `Prefer: respond-async`；响应含 `run_id` 即视为受理并开始轮询，否则按同步回复处理（兼容老 Hub）。
- 轮询 1s 间隔，单次读 15s 超时，**连续** 3 次失败才放弃 —— 单次网络抖动不取消 Hub 仍在跑的 turn。
- pending 气泡 31 分钟老化，且只清理本进程从未接管过的遗留 run；本进程已接管的 turn 不受此钟影响。
- 机密答案走 `secret-fill` 且不写入聊天记录；自由文本/选项答案作为普通消息发出。
- 非 2xx 一律转 `BotApiException`（含 `BOT_DISABLED` 等错误码），不会把 500 的响应体误当成 Bot 回复。

---

## 9. 后续分期

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| P0 | 恢复 lib 检出；viewer 鉴权改造；Bot API 层 | ✅ 完成 |
| P1 | 登录、Bot 列表、创建/改名/删除、聊天+轮询+持久化、状态徽标、方案确认卡 | ✅ 完成 |
| P2 | 附件落盘与分享（当前复制到剪贴板）、任务面板联动、未读数与推送、桌面接管入口跳转 | 未开始 |
| P3 | noVNC 内嵌（需 `webview_flutter`）、定时任务（需 Hub 补移动端接口） | 未开始 |

### 环境
Flutter SDK 3.41.5（Dart 3.11.3）已装在 `D:\flutter`，与工程 `.dart_tool` 记录的生成器版本一致；`android/local.properties` 的 `flutter.sdk=D:\flutter` 原本就指向该路径，无需改动。Android SDK（platforms 34/35/36、build-tools 35/36）与 JDK 17 均已就位，可直接 `flutter build apk`。

---

## 10. 风险与待确认

1. **附件体积**：Bot 回复内联 base64 图片/文件。当前只做内存解码 + 4MB 上限保护（超限降级为复制），P2 应改为落盘。
2. **Hub TLS 常为自签证书**：桌面端 `NewHubHTTPClient` 直接 `InsecureSkipVerify`（`corelib/remote/enrollment.go`）。移动端**未**全局关校验，保持默认严格行为；若自签 Hub 连不上，需要显式的「允许自签证书」开关，而不是静默降级。
3. **长任务与 App 被杀**：run_id 只存在内存（`_liveRuns`），进程被杀后无法续读。需确认 Hub 侧 run 结果的保留窗口，再决定是否把 run_id 落盘。
4. **截图视觉未对齐**：附件截图无法解析像素细节，左栏宽度、卡片与气泡风格需真机比对。
5. **仓库既有 61 个测试失败**：release/QA 脚本与旧 test double 签名过时。与 Bot 无关，但会让 CI 长期红着，值得单独排期修。

---

## 附：关键代码位置索引

- Bot 用户接口：`hub/internal/httpapi/bot_user_handler.go`
- Bot 路由注册：`hub/internal/httpapi/router.go:430-439`
- Bot 业务逻辑与 Reply 结构：`hub/internal/botmgmt/service.go`、`access.go`、`desktop_admit.go`
- machine 鉴权：`hub/internal/httpapi/ve_admin_handler.go:2162`
- viewer token 签发/校验：`hub/internal/auth/identity_service.go:2005,2035`
- 验证码注册入口：`hub/internal/httpapi/registration_email_handlers.go`、`registration_sms_handlers.go`、`miniprogram_auth.go`
- 桌面端参考实现：`guiapp/desktop_bot_hub.go`、`guiapp/frontend/src/components/bots/desktopBots.ts`
- 移动端现有实现：`mobile/maclaw_mobile/lib/features/auth/`、`lib/features/digital_employees/`
- 本次新增：`mobile/maclaw_mobile/lib/features/bots/`、`hub/internal/httpapi/bot_viewer_auth_test.go`