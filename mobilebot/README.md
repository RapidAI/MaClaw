# MobileBot

桌面 Bot（MaClaw / CodeClaw desktop bot）的手机客户端，Flutter 实现
（Android / iOS）。功能对齐桌面 GUI 的 `DesktopBotWorkspace`：创建和配置
Bot、与 Bot 聊天、查看工作状态、查看并操作 Bot 的电脑画面；按手机布局
（底部导航 + 列表 + 底部表单 + 全屏页面）重新组织。

## 功能

| 桌面 GUI | 手机端 |
| --- | --- |
| Bot 侧栏 / 新建 Bot | `Bot` tab：列表 + 底部新建表单（名称/简介/颜色/形状） |
| DesktopBotWorkspace 对话区 | Bot 聊天页：Markdown 回复、图片/文件气泡（点开保存并分享）、ask 卡片、选项 chips、发送失败一键重发、停止任务、📎素材上传、🎙语音输入（端侧 STT）；排队语义完整：Bot 忙时消息入队显示"排队中 · 第 N 位"（`GET /queue/{cmid}` 实时位置，held 显示"已暂存"，running/done 自动清除，串行消费逐条清除）、ack 应答行/转派提示落成 ack 气泡；输入 `/clear` 本地清空转录（保留发送中/排队气泡）；重进聊天自动接管仍在运行的 run（恢复执行中状态/补充/取消） |
| 电脑画面（noVNC / h264 stage、唤醒、观看/接管、交还） | 电脑画面页：WebView 渲染 `novnc_url`（默认 h264.html，可切回 vnc.html），watch 持有 / 唤醒 / 交还 / 接管开关；「电脑维护」面板：代理模式（跟随默认/智能/直连/始终代理，租户锁定时只读）+ 重启/重置云电脑（可勾选强制） |
| 任务中补充说明（supplement） | 任务运行中输入框旁「补充说明」按钮 → `POST /runs/{id}/supplement`，展示 Hub 返回的接收结果 |
| 继续这个任务（continue_session） | `bot#<id>` 线程的回复气泡显示「继续这个任务」chip → 下一条消息带 `continue_session` 回到该线程，输入框上方横幅可取消 |
| 计划/执行阶段 + ask 路由 | plan 阶段回复显示「按这个安排执行」chip（带 `phase=execute` 重发）；ask 卡片/选项应答带 `ask_run` + `continue_session` 路由回被问的 run |
| 长期偏好（preference） | 设置页 → 长期偏好：记录一条持久偏好（`POST /preference`）或撤回全部（`DELETE /preference`），服务端只写不读 |
| 任务 tab / 运行状态 | `工作状态` tab：所有 Bot 的活动 run + 待查看结果；单 Bot 状态页支持暂停/继续/停止 |
| 动态（Bot 间消息 feed） | `工作状态` 内的「动态」分段：`GET/POST /bots/feed`，展示后标记 shown，点击跳转对应 Bot；note 附件可点开下载分享 |
| 协作任务板（BotCollabStatus） | 「动态」顶部：进行中的委派 `GET /bots/assignments`，谁派给谁、状态、待答问题 |
| BotSaidInline（Bot 对你说） | 「动态」分区：`GET /bots/said`，Bot 直接对人说的话 + 附件下载 |
| BotRoomsDialog（房间群聊） | 工作状态 AppBar → 房间：列表/新建/编辑（名称+成员+attention 全部\|提及\|关闭）/删除；房间内 `GET /bots/said?room=` 对话 + `POST /bot-rooms/{id}/messages` 发言唤醒成员 |
| 演示录制（demo/*） | 设置页 → 演示录制：开始/旁白（打字或语音，语音走 Hub ASR）/结束 → 时间线 + 截图 → `demo/distill` 提炼技能；队列中的委派 feed 可一键 `cross-notes/{id}/start` 启动 |
| 设为主 Bot | Bot 列表「主」徽标 + 设置页「设为主 Bot」：`POST /bots/{id}/primary` |
| 素材传送（desktop/files） | 聊天输入框 📎：选文件分块上传 `POST /bots/{id}/desktop/files` → `~/素材/<bot>/<name>`，路径回填输入框 |
| Style 历史版本 | 人格与记忆页底部：历史版本列表 + `POST /bots/{id}/style/rollback` 回滚 |
| BotEditDialog / 电脑模式 / BotStyleDialog | Bot 设置页：改名、简介、外观（桌面同款 10 色 × 6 形状）、`desktop_mode`（共享/独立）、人格与记忆（style soul/memory）、删除 |
| BotLLMSettingsDialog | `我的` → 模型设置：默认/自定义提供方（name/url/key/model/protocol/wire_api），展示各角色当前生效配置 |
| 额度显示 | `我的` → Bot 额度卡：`GET /bots/credits`，用量进度或不限量 |
| 登录 / 多账号 | 邮箱验证码或手机验证码登录；多账号并存、一键切换、退出/移除账号；token 过期（401/403）自动跳转重新验证，账号保留可一键恢复 |

## 架构

```
lib/
  main.dart                ProviderScope 入口
  app.dart                 go_router：登录守卫、底部导航 Shell、/bot/:id 系列路由
  core/
    hub_config.dart        官方 HubCenter 端点、超时、HubApiException
    api_client.dart        绑定账号的 Dio（Bearer viewer_token）
    account_store.dart     flutter_secure_storage 多账号存储 + SharedPreferences 当前账号
    providers.dart         SessionController（accounts/active/switch/remove）、clientId
  features/
    auth/                  HubCenter resolve → send-code → verify-and-start（邮箱 & 手机）
    bots/                  /api/v1/bots 全部调用、模型、列表/聊天/设置/电脑画面
    status/                runs/active、tasks、pending deliveries 聚合
    account/               账号卡、切换、移除、退出
```

登录链路（与 `mobile/maclaw_mobile` 相同的官方 Hub 发现流程）：

1. `POST {hubCenter}/api/entry/resolve`（`{email}` 或 `{phone_number}`）→
   选 `default_hub_id` / online / 信息最全的 hub；
2. `POST {hub}/api/enroll/email/send-code` 或
   `POST {hub}/api/mobile/auth/phone/send-code`；
3. `POST {hub}/api/enroll/email/verify-and-start` 或
   `POST {hub}/api/mobile/auth/phone/verify-and-start`
   （携带 `machine_name=MobileBot`、`platform=mobile`、持久 `client_id`）→
   `access_token`/`viewer_token`、`machine_id`、`machine_token`。
   `status=pending` 表示待管理员审批。

之后所有 `/api/v1/bots/**` 请求带 `Authorization: Bearer <token>`。

聊天同步与桌面 GUI 同一份 journal：`GET/POST /api/v1/bots/conversation`
（kind `message`，data 为 `syncProjection` 投影），`wait=20` 长轮询接收
其他客户端写入；`GET .../runs/{runID}` 轮询 admitted run；
`.../runs/active`、`.../tasks`、`/pending` 提供工作状态；
`POST .../desktop {hold|wake|release, epoch}` 维护画面持有期，
`POST .../desktop/handback` 交还，`POST .../secret-fill` 递交密钥。

## 构建

```bat
cd mobilebot
flutter pub get
flutter analyze
flutter test                  :: 模型解析 + 控制器单测（fake BotApi）+ 列表/聊天页 widget 冒烟测试（43 例）
flutter build apk --debug     :: 或 flutter run
```

已验证：`flutter analyze` 零告警；`flutter test` 全过；`flutter build apk --debug` 通过。

## 范围说明

- 电脑画面的“操作”沿用 WebView 内 noVNC/h264 页面自身的触控→鼠标键盘
  映射，需 `user_control=true` 时在页面顶部打开“接管”开关；
- 桌面 GUI 中依赖本地 Wails 通道的能力（本地文件附件、演示技能面板、
  网络/语音细项设置、bot 协作房间）未纳入；LLM 多角色细分配置
  （前台/后台/电脑操作/定位）在桌面端才有，手机端提供单一提供方配置，
  Hub 会将其展开到所有角色；
- 仅支持三个官方 HubCenter 发现端点，不提供自建 Hub 地址输入。

## 实时性说明

bot 会话/feed 的同步走 `GET /api/v1/bots/conversation?after=&wait=20`
长轮询——与桌面 GUI（`guiapp/desktop_bot_conversation.go`）完全同链路。
已核实 Hub 没有 bot 事件的 WebSocket 推送：`/ws` 网关只面向桌面机的
session 订阅（session.summary/preview_delta 等），`/api/mobile/realtime`
只推 SSH/文档/会议任务，`/api/chat/ws` 是租户聊天频道，均不承载 bot
journal 事件。长轮询是桌面与手机共同的官方同步路径。
