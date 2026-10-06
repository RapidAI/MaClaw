# MaClaw 陪伴式语音终端 开发计划

> 定位修正版。本文**取代** `docs/maclaw-companion-duplex-plan.md` 的任务划分，两份文档的双工技术细节仍然有效。

- 版本：**v15**　日期：2026-10-05　状态：**阶段 0 已收口，阶段一 M1 已开工**：N0-1/N0-2/N0-3 全部落地，**N1-1 契约 + N1-3 扇出 + N1-4 `device` 渠道 + N1-2 决策核心与设备侧接线 + N1-5（2/3 事件源）+ N1-6（大脑侧 + 设备侧）已闭环**（修正记录见 §9）
- **核心定位**：接入 MaClaw GUI → **控电脑**；接入 MaClawSrv → **干活**。两者是分工，不是降级。
- **已拍板**：D1 不切换大脑（绑定即固定）｜D2 绑设备、换设备重新接入｜D3 首次唤醒+20s 免唤醒窗口｜D4 LAN 与 Hub 中转都支持｜D5 审批仅高危｜D7 主力板 echoear 2ST｜次要三项按建议关闭
- **配套文档**：`docs/maclaw-persona.md`（角色设定，N0-1 已产出，N4 验收依据）
- **v15 变化**：**N1-6 设备侧落地，M1 最后一块闭合**——纯值 `event_decision.h`（待决卡片状态机：eventId/动作集/绝对过期/回执与决策与超时的优先级）+ 主机单测 19 组 + `gateway_event_ack_service.{h,c}`（卡片呈现 + `POST /api/im-gateway/v1/event-ack` 上报）+ `input_binding_handle_event` 的决策分支 + dispatcher `flush_event_ack` 钩子。**推翻 v14 的一处建议**：C77 建议"先只做语音决策（复用既有 ASR）"，核实后**不成立**——设备**无本地 ASR/转写**，语音上行到 Hub、ASR 与 agent 都在 Hub 侧，语音决策无路可走；故走「手势/物理键」路径（`app_intent_service` 已同时抽象两者，板无关）。另**修正两个真 bug**（C88/C89）并新增 ASCII 纯度铁律（C84，见 §9 v15）
- **v14 变化**：**N1-6 大脑侧落地**——`POST /api/im-gateway/v1/event-ack`（`handleEventAck`：eventId 关联、action 白名单、**Hub 侧裁决过期**、终态清快照）+ Hub→GUI 新信封 `im.device_gateway_event_ack` + GUI 复用 `handleRegisteredToolApprovalAgentViewSubmit` 落定（eventId→approvalID 映射）+ **审计 `Source` 记"谁/何时/何种方式"**；**设备侧（手势采集 + ack 上报）当时仍缺**，见 C77（**v15 已补**）
- **v13 变化**：**N1-2 设备侧接线落地**——`GATEWAY_CAPABILITY_EVENT_PUSH` 能力标志（对齐 `Features.eventPush`）+ dispatcher `event` 分类（能力门 + `handled/permanently_invalid` 出参）+ `main.c` 呈现；新增纯值 `event_ingest.h`（category 闭集、审批审计契约、忙碌时延后、会话内幂等环）+ 主机单测 16 组；**"语音播报"仍待 N3-5**，当前降级为显示而非丢弃
- **v12 变化**：**N1-5 接入第一批事件源 2/3 落地**——高危工具审批（`device_approval_event.go`，D5-A 正向判定 + 审计契约 + 具体对象提取）与 VE 工作流注意力事件（`device_ve_event.go`）两条生产链路 + 单测 19 例；**审批源修正为本地工具审批路径**（`handleVEApprovalRequest` 不带 risk，无法实现"仅高危"，见 C58）；**第三源 `task_done` 无现成信号**，停在设计决定（见 C61）
- **v11 变化**：**N1-2 决策核心落地**——`event_presentation_policy.{h,c}` 把 §4.2 准入矩阵做成纯值模块（3x5 表 + 五档状态优先级 + 闭集 severity 解析 + 播报文案），主机单测 10 组、真实 xtensa `-fsyntax-only` 零错误；**关键发现：设备无本地 TTS，`SPEAK` 类动作依赖 N3-5**，故 N1-2 拆为"决策核心（已完成）"+"接线与播报（待 N3-5）"（见 C53）
- **v10 变化**：**N1-4 scheduler `device` 投递渠道落地**——`DeliveryChannelDevice` + CJK/拉丁别名归一 + `SplitScheduledTaskBody` 任务名/正文拆分 + `reviewedHostScheduleDispatchFireChannel` 分支 + GUI 侧事件构造与推送（`deliverDeviceScheduledTarget`）+ 目标目录注册 + 4 处工具描述；**契约常量从 `hub/internal/im` 迁到 `corelib/im`**（Go `internal` 包让导出的常量 GUI 用不到，见 C47）；**MaClawSrv 侧无设备事件推送通道**，故本渠道只在 GUI 落地（见 C50）
- **v9 变化**：**N1-3 `UpdateMachineEvent` 扇出落地**——`eventsByMachine` 快照 + 机器级扇出（逐设备判能力）+ handshake 补发 + GUI 侧 `SendDeviceGatewayEvent`；**C34/C35 两个坑已闭合**（绝对过期时刻持久化、补发重算 `ttlSec`；扇出显式判能力）；顺带修掉 v8 遗留的 `notice` → `soft` 文案错误
- **v8 变化**：**N1-1 `event` reply 契约落地**——`device_event_push.go` 校验器 + `Features.eventPush` 能力声明 + 能力 prompt；契约以 §4.1 为准，两处有理由的偏离见 §9 v8（嵌套 `event`、去 `speak`）；**审批事件的审计契约在 wire 层强制**（`requiresAck`+`persist`+`ttlSec`）
- **v7 变化**：**N0-3 链路健康度可观测落地**——Hub 侧 tracker + 8 处接线 + `GET /api/admin/link-health/metrics`（global admin）+ admin「链路健康度」面板（见 §9 v7）；**阶段 0 三件全部完成，M1 可以开工**
- **v6 变化**：**N0-2 延迟打点框架落地并接线**（8 个里程碑接进真实链路，见 §9 v6）；打点改为显式开合、`response_ms` 回退链补齐
- **v5 变化**：D7 主力板定案；次要三项关闭；N0-1 角色设定产出（−2 人日）；总人日 234→232
- v2 变化：新增故障分层（§3.2）、事件契约与准入矩阵（§4.1/§4.2）、审批回流协议（§4.3）；落地 D2 拍板；修正 4 处依赖/排期/数字错误
- v3 变化：§3.1 从"能力分级"重构为"两种能力域（分工）"，新增越界引导话术与 D6 路由方向；N2-3 扩为"越界引导 + 离线诚实表达"（3→5 人日）；W9 风险重定义
- v4 变化：落地 D1/D3/D4/D5；**N2-1 从 13→6**（不做切换/failover/动态 capabilities）、**N1-6 从 8→6**（仅高危）；**新增 N2-5 轻量换绑**（3 人日，D1 的必要补偿）；W1 升至"致命"、W3 消除；总人日 241→235
- **定位**：ESP32 设备不是独立 AI，而是 **MaClaw GUI 的陪伴式语音接入终端** —— 一个常在线、会说话、能随时处理事件、替用户减少手工操作的"嘴和耳朵"，大脑仍在 GUI / MaClawSrv。
- 范围：`guiapp/`（大脑侧）+ `hub/`（通道侧）+ `iot-agentos`（终端侧）+ `MaClawSrv`（常驻大脑）

---

## 1. 先看清真实拓扑（这是全部决策的前提）

```
   ┌──────────────┐   语音上行     ┌──────────┐   WS 转发    ┌─────────────────┐
   │  ESP32 终端   │ ────────────► │   Hub    │ ──────────► │  MaClaw GUI     │
   │ （耳/嘴/脸）   │ ◄──────────── │ (中继)   │ ◄────────── │ （Agent 大脑）    │
   └──────────────┘   outgoing 队列 └──────────┘   device_   │ loops/tools/    │
                                                  gateway_  │ skills/computer │
                                                  reply     │ use/memory      │
                                                            └─────────────────┘
```

**四个必须记住的事实**（均已源码核实）：

| # | 事实 | 证据 | 影响 |
|---|---|---|---|
| F1 | **Hub 不跑对话大脑**，只做中继。设备消息转发给配对的 GUI，由 GUI 的 Agent 做 ASR + LLM | `device_gateway.go` → `forwardIncomingDeviceMessage` → `RemoteGatewayPlugin.HandleGatewayMessage` | 设备的一切智能来自 PC 上的 GUI |
| F2 | **GUI 离线时设备直接失效**：`503 gui_offline` | `device_gateway.go:1772`、`:2165` | ⚠️ **陪伴的最大敌人**：电脑关机/休眠，设备变砖 |
| F3 | **每个 clientId 已有一套独立 Agent 运行时**（私有 memoryStore / confirmationStore / 工具注册表 / HTTP 池） | `guiapp/hardware_agent_runtime.go` 的 `hardwareAgentRuntimeRegistry.handler(clientID)` | ⭐ **"陪伴 agent"的容器已经存在**，不必在 ESP32 上造 |
| F4 | **主动推送总线已存在**：Hub 模式走 `im.device_gateway_reply` WS；本地模式长轮询用 `enqueue` + `notifyCh` 立即唤醒（准推送） | `remote_hub_client.go`、`thirdparty_gateway.go` 的 `handleOutgoing` | ⭐ **"随时处理事件"不需要新建传输层** |

还有两个加分项：
- `guiapp/thirdparty_gateway.go` 本身**就是** gateway server（默认 `127.0.0.1:18777`，`ThirdPartyGatewayHost` 可改配 `0.0.0.0`）。设备可**局域网直连 GUI**，不经公网。
- `MaClawSrv` 是 **headless MaClaw GUI**，与桌面 GUI 共享同一套 corelib 内核，可作为**常驻大脑**解决 F2。

---

## 2. 定位重构带来的三个关键修正

我上一版计划（`maclaw-companion-duplex-plan.md`）把设备当独立伴侣设计，现在需要修正：

### 修正一：人格与记忆不该放在 ESP32 上

上一版把"情绪状态机""好感度""长期记忆"列为设备端任务（T3-1/T3-2/T3-3）。**这是错的。**

F3 已经给出答案：GUI 侧每个设备有独立运行时，**自带 memoryStore 和 confirmationStore**。人格、记忆、好感度都属于大脑，应该长在 GUI 侧的 per-device runtime 里。设备端只保留：

- 轻量的**情绪呈现**（把大脑给的 mood 渲染成动画/表情）
- **离线时的降级行为**（大脑不在时的兜底）
- 本地感知（被抚摸、电量、时间）——这些是大脑看不到的输入，作为事件上报

**收益**：设备端工作量大幅下降，且人格迭代走 GUI 版本，不需要 OTA 固件。

### 修正二："随时处理事件"的瓶颈不在传输，在事件源

F4 说明总线已有。真正的缺口是：

- 设备能收的 reply 类型是**白名单**：`text` / `image` / `file` / `voice` / `tool_call|tool_plan|tool_cancel` / `hardware_config`，加特性消息 `ambient` / `pet_state` / `meeting_result` / `pet_profile`（`device_gateway.go:3508-3614` 的 `adaptDeviceGatewayReply`）
- ~~**没有"事件"这一类别** —— 没有任务完成、审批请求、日程提醒、数字员工（VE）事件~~ ✅ **已闭环（N1-1 契约 + N1-3 扇出）**，见 §4.1
- ~~**`corelib/scheduler/delivery.go` 的投递渠道只有 `lansenger / weixin / telegram / qq`，没有 device**（`:13-16`）~~ ✅ **已闭环（N1-4）**：`DeliveryChannelDevice` 已加入，见 §9 v10
- Hub 的 notification 系统只推给 GUI 机器，不推设备

所以工作是**把事件接进已有总线**，不是造新总线。

### 修正三："减少手工操作"的真正落点

语音 → GUI Agent → computer use / 文件 / 命令 / 浏览器 / skill，**这条链路现在是通的**（设备文本 → `incoming` → `HandleIMMessageWithProgress` → agent loop）。

真正的缺口是三个体验问题：
1. 语音交互是整包轮次（L1），说一句话要好几秒才出声 → 不够顺手
2. Agent 要执行危险操作时，**确认必须回到电脑前点** → 反而增加操作
3. 结果只回文本/图片，设备小屏读起来费劲

---

## 3. 三个致命缺口与解法

| 缺口 | 症状 | 解法 |
|---|---|---|
| **G-A　GUI 离线 = 设备变砖**（F2） | 电脑关机/休眠后，设备说什么都 503。陪伴产品无法接受 | ① 常驻大脑：把 MaClawSrv 作为 7×24 大脑（它共享 corelib 内核，改造量小）② 大脑不在时设备明确降级为"离线伴侣"（本地时钟/闹钟/缓存 pet/固定短语），**并明确告知用户"我现在脑子不在线"**，而不是静默失败 |
| **G-B　事件进不来** | 没有事件类别，日程提醒无处投递 | ① `adaptDeviceGatewayReply` 增加 `case "event"` ② `corelib/scheduler/delivery.go` 增加 `device` 渠道 ③ 照抄 `UpdateMachineEvent`（范式见 `device_gateway.go:3780-3818` 的 `UpdateMachineAmbient` 扇出）<br>✅ **①②③ 全部已闭环**（N1-1 / N1-4 / N1-3，见 §9 v8/v9/v10）；**设备侧接收与呈现亦已闭环**（N1-2，见 §9 v13）。剩余：N1-5 接入更多事件源、N1-6 审批终端 |
| **G-C　语音不顺手** | L1 整包轮次，延迟高，不能打断 | 双工改造（沿用上一版 A1–A4，但目标下调为"够顺手"，不追求极致） |

### 3.1　两种接入 = 两种能力域（**已拍板：不是降级，是分工**）

**已拍板**：
- **接入 MaClaw GUI → 控电脑**
- **接入 MaClawSrv → 干活**

评审时我按"MaClawSrv 是 GUI 的降级备胎"来设计，这是错的（v2 里我把它写成"L1 降级大脑"）。核实后确认：两者是**互补的两个能力域**，各有独占能力。

- `computer_use_*`（键鼠控制/截屏/YOLO 视觉）**只存在于 `guiapp/`**（`computer_use_routing.go`、`computer_use_task.go`、`app_yolo_model.go`），**`MaClawSrv/` 内零命中**；README 明写 "does not ship a desktop IDE chrome"
- 反过来，MaClawSrv 有 GUI 做不到或做不好的东西：**async job（`202 Accepted` + job resource，真长时后台任务）**、`admin_scheduler.go` 定时任务、`delegate_task` + coding-runtime 适配器

| | **接入 MaClaw GUI —— 控电脑** | **接入 MaClawSrv —— 干活** |
|---|---|---|
| 独占能力 | 键鼠控制、截屏、YOLO 视觉、本地文件、桌面应用 | `delegate_task` 编码外包、async 长时 job、定时任务、MCP、skills、knowledge、memory |
| 典型指令 | "帮我把这个 Excel 整理一下"、"打开浏览器搜 X"、"截个图看看" | "帮我调研 X 写个报告"、"每天 8 点提醒我"、"跑个批处理" |
| **做不到** | 长时间后台任务（电脑会关机/休眠） | **控电脑**（服务器上没有桌面）；host-local bash **默认禁用** |
| 一句话定位 | **它是"手"** | **它是"后台劳力"** |

> 关键认知：切到 MaClawSrv **不是能力缩水，是换了一种能力**。

### 3.1.1　三条硬性设计要求

1. **`handshake` 必须声明能力域**：响应增加 `brain{brain_id, brain_kind: gui|srv, capabilities[]}`
2. **换绑（重新接入）= 切换能力域，必须重新 handshake 刷新设备侧能力视图**（D1 拍板不切换，故只有换绑这一条路径会改变能力域，是静态的、不需要运行时动态更新）
3. **越界指令要"引导"而不是"拒绝"**：
   - srv 模式下说"帮我打开浏览器" → 「我现在接的是干活模式，够不到你的电脑。要我改成后台帮你查这个吗？」
   - gui 模式下说"跑个三小时的分析" → 「这个任务太久，你电脑要是睡了我就断了。要不要交给后台去做，做完我告诉你？」

> 这一条把原来 N2-3 的"诚实表达"从一句口号变成了可执行的机制，而且从"说做不到"升级为"给出替代路径"。

### 3.1.2　未来方向：按指令类型自动路由（记为 D6，暂不排期）

MaClawSrv README 有一句很关键：**"Multiple logical instances can run at the same time under one user."** —— 一个 user 下可跑多个逻辑实例。这意味着未来两个大脑可以**同时在线**，按指令类型路由：涉及桌面/本地文件 → GUI；长时任务/定时/知识工作 → MaClawSrv。

但当前 Hub 的 owner 语义是**一个租户一个 owner**（`ClaimGatewayForTenant`），要支持路由需先放开为"带 capability tag 的多 owner"。

> D1 拍板"不切换大脑"后，D6 与当前架构的距离更远了（连切换都不做，遑论并行路由）。**当前不排期**，但 §3.1.1 的 `capabilities[]` 字段现在就要留好——它同时服务于"当前能力域声明"和将来的路由，成本为零。

### 3.2　故障分层（原设计把三种故障混为一谈）

原 N2-2 只覆盖"断网"，N2-3 只覆盖"大脑不在"，F2/F4 两层完全没有设计：

| 层 | 场景 | 设备可见信号 | 应有行为 |
|---|---|---|---|
| **F1 网络断** | Wi-Fi / 4G 断开 | 连接失败 | 本地降级 + 重连退避 |
| **F2 Hub 不可达** | Hub 5xx / 超时 | HTTP 错误 | 有 LAN 直连则切 LAN；否则本地降级 |
| **F3 大脑不在** | `503 gui_offline` | 明确的业务错误码 | 切备用大脑；都不可用则走"脑子不在线"话术 |
| **F4 大脑在但能力受限** | 大脑是 MaClawSrv | handshake 的 `capabilities` | 桌面类指令明确拒绝并解释原因 |

四种故障的设备表现**必须不同**——否则用户无法分辨"网断了"和"电脑没开"，而这两者的处理方式完全不同。

**可观测口径（N0-3 已落地，见 §9 v7）**：F3 由 `gui_offline_rejections` 直接量化；F3 的**持续时间**由 per-tenant `gui_online_ratio` 反映（在线率低 = 大脑长期不在）；F2 与 F1 目前只能从设备端日志看（`latency_trace` 无 `sent` 之后的里程碑 = 上行没通），**没有 Hub 侧指标**——这是阶段二的补课项（N2-4 传输优选会顺带补齐）。

---

## 4. 杀手级场景：设备作为 GUI Agent 的**审批终端**

这是新定位下最有价值、且成本最低的场景，上一版完全漏了。

GUI Agent 要执行高风险操作（computer use 键鼠、文件删除、命令执行、外发消息）时，现有确认流必须回到电脑前点。而 `hardwareAgentRuntime` **已经自带 `confirmationStore`**。

**做了什么**：把确认请求作为事件推到设备 → 设备屏显 + 语音播报"要我帮你删除 X 吗？" → 用户**说话确认或按按钮** → 回 `tool-result` → GUI 继续执行。

**价值**：不在电脑前也能管控 Agent。这既是"减少手工操作"，也是"随时处理事件"的最佳注脚，且几乎不需要设备端新增能力。

### 4.1　事件消息契约（原设计只给了字段列表，不足以落地）

> ✅ **N1-1 已按本节落地**（`hub/internal/im/device_event_push.go`：校验器 + 单测；`corelib/agent` 的 `Features.eventPush` 能力声明 + 能力 prompt）。两处与下方示意 JSON 的**有意偏离**，见本节「N1-1 落地时的两处修正」。
>
> ✅ **N1-3 已落地**（`UpdateMachineEvent` 扇出 + handshake 补发快照）。下方 `ttlSec` 重算的 ⚠️ 要求已实现：快照存**绝对**过期时刻 `expiresAtUnixMs`（Hub 内部字段，不上 wire），补发时改发剩余秒数，剩余 ≤0 丢弃。详见 §9 v9。

```json
{
  "type": "event",
  "conversationId": "system",
  "event": {
    "eventId": "evt_01H...",
    "category": "approval | task_done | schedule | ve | system",
    "severity": "silent | soft | interrupt",
    "title": "需要你确认",
    "summary": "要删除 ~/Downloads/report-final-v2.xlsx 吗？",
    "ttlSec": 300,
    "dedupeKey": "approval:inst-123:step-2",
    "actions": [
      {"id": "approve", "label": "同意", "kind": "primary", "risk": "high"},
      {"id": "reject",  "label": "拒绝", "kind": "secondary"}
    ],
    "requiresAck": true,
    "persist": true
  }
}
```

**`kind` 与 `risk` 必须分开**：`kind` 是**呈现强调**（哪个按钮是推荐项），`risk` 是**动作后果**（`low|medium|high`）。D5-A 用 `risk=high` 判定"这是高危审批"，若用按钮配色反推语义，那改配色就会改安全行为。未声明 `risk` **不等于** `risk=low`——前者保持字段缺失，让 D5-A 保持"正向判定"。

**N1-1 落地时的两处修正**：

1. **payload 嵌套在 `event` 键下**，不与 `type` 平级。原因：信封 `ThirdPartyOutgoingMessage` 是**类型无关**的，塞入 `eventId`/`dedupeKey` 会污染它；且既有结构化类型（`ambient`，以及 `hardware_config` 的 `extra`）都是嵌套的。GUI 生产端写法同 `SendDeviceGatewayAmbient`。
2. **删除 `speak` 字段**。本节示例本身就把 `summary` 写成了播报问句（"要删除 … 吗？"），说明 `summary` 已承担播报语义。设备播报顺序 = `summary` → 回退 `title`。两个必须保持同步的文本字段一定会漂移。

**五条必须明确的语义**（原设计全缺，缺任何一条都会出线上问题）：

| 语义 | 为什么必须 | 设计 |
|---|---|---|
| **幂等** | 审批事件重试是必然的，否则用户会被同一件事问三遍 | 相同 `dedupeKey` 设备不重复呈现；**未给 `dedupeKey` 时 Hub 回填为 `eventId`**，让幂等能力对生产端默认可用 |
| **超时** | 审批不能无限期挂着——用户不在设备旁时，5 分钟后该请求已无意义 | `ttlSec` 到点自动撤销呈现并回 `expired`；**取值 1..3600，超界拒绝**（防止把决策提示永久钉在设备上） |
| **回执** | 服务端不知道设备收没收到，就无法决定是否重发 | `requiresAck` 时设备回 `event-ack`（见 4.3） |
| **离线补发** | 设备关机期间的事件不能凭空消失 | `persist=true` 的事件存 handshake 快照，重连后补发（复用 `UpdateMachineAmbient` 的持久化机制）；`persist=false`（如"正在输入"）直接丢弃 |
| **动作回传** | 用户在设备上做的决策要回到 Agent | 见 4.3 |

> ⚠️ **`ttlSec` 是相对时间，补发时必须重算**：`persist=true` 的事件在设备重启后补发，若原样重发 `ttlSec` 会**刷新**有效期，让一条 3 小时前的高危审批重新获得 5 分钟窗口——这违反"超时走'不做'而非放行"。**N1-3 的职责**：持久化时一并存下**绝对**过期时刻，补发时改发 `ttlSec = 剩余秒数`，剩余 ≤0 直接丢弃。
>
> ✅ **已按此实现**（v9）：快照条目为 `{event, expiresAtUnixMs}`，`expiresAtUnixMs` 永不进入 wire（`event` 里出现的字段就是契约本身，多一个未声明字段就是契约变更）。补发的 `ttlSec` 只减不增，且被 `deviceEventMaxTTLSec` 夹住——即使快照被手工改成"过期时刻在一年后"，也不会把卡片永久钉在设备上。若快照里只有 `ttlSec` 而丢了绝对时刻，**直接丢弃**而不是猜：猜就是刷新窗口。

**审批事件的审计契约在 wire 层强制**（N1-1 新增的硬规则）：

`category=approval` 必须同时满足 `ttlSec>0` + `requiresAck=true` + `persist=true`，否则整条事件被拒绝。原因：这三条任一缺失，失败都是**静默的**——没有 `requiresAck` 则 Hub 永远不知道卡片到没到；没有 `persist` 则设备重启后一条待决高危审批无声消失；没有 `ttlSec` 则它永远挂着。D5-A 要求"必须可审计"，所以把这三条放在 wire 边界强制，而不是指望每个生产端都记得写。非 `approval` 类事件不受此约束（"正在输入"就该是即发即弃）。

### 4.2　事件准入矩阵（原设计只说了"呈现方式"，没说"何时能打断"）

| severity ＼ 设备状态 | 待机 | 播报 TTS | 对话收音中 | 勿扰/作息 | 会议录音 |
|---|---|---|---|---|---|
| **silent** | 屏显 | 屏显 | 屏显 | 静默丢弃 | 屏显 |
| **soft** | 播报 | 排队（播完再播） | 排队 | 静默丢弃 | 屏显 |
| **interrupt** | 播报 | **打断播报** | **打断收音** | 降级为屏显 + 震动 | 屏显 |

两条硬规则：
- **任何 severity 都要尊重 `sleep_schedule` 的勿扰时段**，interrupt 也只降级不破例（否则半夜会被吵醒）
- **会议录音期间一律不打断**，避免污染正在录的内容

### 4.3　审批回流协议（原设计说"回 tool-result"，语义不对）

`/api/im-gateway/v1/tool-result` 的语义是「**设备执行了工具，回传结果**」。审批确认是「**用户做了决策**」，两者语义不同——硬塞进 tool-result 会让服务端的工具执行状态机混乱。

**建议新增 `POST /api/im-gateway/v1/event-ack`**，回执与决策回流共用一个端点：

```json
{"clientId": "...", "eventId": "evt_01H...",
 "status": "received | approved | rejected | expired",
 "actionId": "approve", "decidedBy": "voice | button | timeout"}
```

- `status=received` → 纯回执（对应 4.1 的 `requiresAck`）
- `approved / rejected / expired` → 决策回流，服务端据此让 Agent 继续或中止

---

## 5. 开发计划

字段：`端` = G(GUI/大脑) / H(Hub) / D(设备固件) / S(MaClawSrv) / 产 / 测　`估` = 人日初估（需校准）

### 阶段 0　地基

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N0-1 | **角色设定一页纸** —— ✅ **已产出：`docs/maclaw-persona.md`**（性格/说话方式/红线/分场景话术/熟悉度语气/工程落点） | 产 | ~~2~~ ✅ | — | ✅ 已完成，待评审；成为 N4 全部任务的验收依据 |
| N0-2 | **延迟打点框架**（设备端 mark/log_marks） —— ✅ **已落地并接线**（`main/services/latency_trace.{h,c}` + `tools/check-latency-trace.ps1` + `tools/host_tests/test_latency_trace.c`） | D | ~~3~~ ✅ | — | ✅ 主机单测 9 例通过；8 个里程碑已接进真实链路（详见 §9 v6） |
| N0-3 | **链路健康度可观测**：GUI 在线率、`gui_offline` 触发次数、事件投递成功率 —— ✅ **已落地**（`hub/internal/im/link_health.go` + `hub/internal/httpapi/link_health_handler.go` + `hub/web/admin/link-health-tab.js`） | G+H | ~~3~~ ✅ | — | ✅ 主机单测 14 例通过；admin 面板 + `GET /api/admin/link-health/metrics`（global admin）；三层指标齐备（详见 §9 v7） |
| ~~N0-4~~ | ~~决策：唤醒词角色~~ —— **已由 D3 拍板关闭**（首次唤醒 + 20s 免唤醒窗口），不再占用人日 | — | ~~1~~ | — | ✅ 已关闭，D3 已给出落地要求（见 §8 D3） |

### 阶段一　事件贯通（里程碑 M1）⭐ 最高价值

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N1-1 | **reply 类型增加 `event`**：`adaptDeviceGatewayReply` 加 `case "event"`，定义 `{category, severity, title, summary, actions[]}` —— ✅ **已落地**（`hub/internal/im/device_event_push.go` + `corelib/agent` 的 `Features.eventPush`） | H | ~~3~~ ✅ | — | ✅ 契约以 §4.1 为准（两处修正见该节）；审批审计契约在 wire 层强制；单测 47 断言通过 |
| N1-2 | **设备侧 event 渲染与语音播报**：按 severity 分级呈现（静默/轻提示/打断播报） —— 🔄 **决策核心 + 设备侧接线已落地**（`event_presentation_policy.{h,c}` §4.2 矩阵 10 组单测；`event_ingest.h` 校验/决策/幂等 16 组单测；`GATEWAY_CAPABILITY_EVENT_PUSH` 能力标志 + dispatcher `event` 分类 + `main.c` 呈现）；**仅"语音播报"待 N3-5**（设备无本地 TTS，当前降级为显示） | D | 8 | N1-1 | 三类 severity 表现符合预期；**已可验证**：矩阵 15 格 + 两条硬规则 + ingest 16 组；端到端待真机 |
| N1-3 | **`UpdateMachineEvent` 扇出**：照抄 `UpdateMachineAmbient` 范式，扇出到该 machine 下所有在线设备并持久化到 handshake 快照 —— ✅ **已落地**（`eventsByMachine` 快照 + 逐设备判能力 + handshake 补发 + GUI 侧 `SendDeviceGatewayEvent`） | H | ~~5~~ ✅ | N1-1 | ✅ 一台 GUI 接 3 台设备时全部收到；只会画文字的设备被跳过；重启后待决事件按剩余窗口补发 |
| N1-4 | **scheduler 增加 `device` 投递渠道**：`corelib/scheduler/delivery.go` 加 `DeliveryChannelDevice` —— ✅ **已落地**（渠道归一 + `SplitScheduledTaskBody` + fire 白名单分支 + GUI 侧 `deliverDeviceScheduledTarget`） | G | ~~5~~ ✅ | N1-3 | ✅ 日程提醒可送达设备；`user_id=self` 以外一律拒绝；任务名进 `title`、正文进 `summary` |
| N1-5 | **接入第一批事件源**：审批请求、VE 数字员工事件、任务完成 —— 🔄 **2/3 已落地**：**高危工具审批**（`device_approval_event.go`，源改为本地工具审批而非 `handleVEApprovalRequest`，见 C58）+ **VE 工作流注意力事件**（`device_ve_event.go`）；**`task_done` 无现成信号**（见 C61） | G | 8 | N1-3 | 审批与 VE 事件已能下发；`task_done` 待定 |
| N1-6 | **设备作为审批终端（仅高危，D5-A）**：`risk=high` 确认请求 → 设备播报**带具体对象**的上下文 → 语音/按钮决策 → `event-ack` 回流。**必须可审计**（谁/何时/何种方式批准） | G+D | 6 | N1-5 | 全程不碰电脑可完成一次高危确认；超时走"不做"而非放行；审计记录可查　✅ **大脑侧 + 设备侧均已落地**（`handleEventAck` + GUI 决策落定 + 审计 `Source`；`event_decision.h` + `gateway_event_ack_service` + `input_binding` 决策分支）。**决策载体为手势/物理键而非语音**（设备无本地 ASR，见 C82/C83）；**语音播报**仍待 N3-5（见 C53/C70） |

### 阶段二　离线韧性（里程碑 M2）⚠️ 陪伴的生死线

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N2-1 | **两种接入模式（绑定即固定，D1 不切换）**：配对时选定 `gui`（控电脑）或 `srv`（干活）；`handshake` 静态声明能力域。**无 failover、无动态 capability 更新** | S+H+D | 6 | — | 两种模式各自可正常对话；设备能力视图与绑定对象一致 |
| N2-2 | **设备离线降级 UX**：内置 pet 帧（非云端缓存）+ 本地时钟/闹钟/天气缓存 + 固定短语响应 | D | 8 | — | 断网 10 min 仍可唤醒并给出合理反馈 |
| N2-3 | **越界引导 + 离线诚实表达**：① 能力域外指令给替代路径（§3.1.1 话术）② `gui_offline` 时明确告知而非假装在思考 | D+产 | 5 | N2-1 | srv 模式说"打开浏览器"能给出替代方案；离线时不误导用户 |
| N2-4 | **传输优选（D4：LAN 与 Hub 中转都支持）**：同网时优选 LAN 直连（`ThirdPartyGatewayHost=0.0.0.0`），否则走 Hub 中转。**Hub 是保底路径，健壮性优先于 LAN 加速**；LAN 探测须带超时回退 | D+G | 8 | — | 非内网环境走 Hub 可用；LAN 可用时延迟下降；两条路径能力域一致 |
| N2-5 | 🆕 **轻量换绑流程**（D1 的必要补偿，**不可省略**）：换接另一大脑 <1 分钟，**且不需要接触电脑**（绑定 GUI 时电脑可能正关着）。形态：设备端选择 + 手机扫码/屏显码确认，或对端发起迁移 | S+H+D+产 | 3 | N2-1 | 从"要换大脑"到可用 <1 分钟；全程无需操作电脑 |

### 阶段三　语音顺手度（里程碑 M3）

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N3-1 | ⚠️ **AEC spike**：AFE 的 RAM/CPU 实测 + 参考信号对齐方案 | D | 3 | N0-2 | 产出实测数据；**失败则整体重估** |
| N3-2 | **AEC 接入 + `audio_arbitration_service` 转正** | D | 8 | N3-1, N0-4 | 满音量播报不误唤醒；插话可检测 |
| N3-3 | **Opus 流式上行**（复用 `meeting_service` 分块骨架） | D 5 + G 3 | 8 | N3-2 | 上行字节降 ≥80%；会议录音并发无抢占 |
| N3-4 | **世代号 `interaction_turn_service`**：`gen++` 全链路作废 | D 3 + G 2 | 5 | N0-2 | 打断后不再"停了又播出来" |
| N3-5 | **流式 TTS 边下边播** | G 8 + D 8 | 16 | N3-4 | 首帧出声时间显著下降 |
| N3-6 | **pre-roll 300ms + `pre_reset()`** | D | 3 | N3-2 | 唤醒词首字不被吞 |
| N5-4 | 🔺 **麦克风状态可见化 + 本地 VAD 先行**（隐私合规） | D 3 + 产 2 | 5 | N3-2 | 收音时有明确指示；未触发 VAD 无上行（抓包验证） |

> 🔺 N5-4 原排在阶段五，但**隐私合规是双工上线的门禁，不是后置增强**，故前移至阶段三与 N3-2 同批。

### 阶段四　伴侣人格（里程碑 M4）—— 长在 GUI 侧

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N4-1 | **人格 + per-device 记忆**：persona 作为**产品级常量**（跨设备一致）；记忆挂 `hardwareAgentRuntime` 现有 memoryStore，**沿用 clientId 键**（D2 已拍板绑设备，无需重构为 user+device） | G | 10 | N0-1 | 跨会话人设不漂移；能召回 3 天前事实 |
| N4-2 | **好感度与成长**：有效交互计分 + 日上限防刷，存 GUI 侧 per-device | G | 4 | N0-1 | 念经不涨分；恢复出厂可擦除 |
| N4-3 | **设备端情绪呈现**：接收大脑下发的 `mood` 渲染为动画/表情；离线时用本地轻量状态机兜底 | D 6 + G 2 | 8 | N4-1 | 在线跟随大脑；离线仍有合理表现 |
| N4-4 | **主动关怀**：早间简报 / 天气突变 / 睡前 / 低电量 / 久未互动，复用 N1-4 的 device 渠道 | G | 8 | N1-4 | 可独立开关 |
| N4-5 | **主动频率预算**：初始每天 ≤3 次、同场景 24h 不重复，按被忽略率自动降频 | G | 5 | N0-3 | 连续 3 天被忽略则次日减 1，最低 1 |
| N4-6 | ⭐ **待机行为**：微动作（眨眼/打盹/看向用户）、久置困倦、被拿起的惊喜 | 产 3 + D 8 | 11 | N0-1 | 待机 5 min 内 ≥2 类微动作；电流不恶化 |
| N4-7 | **抚摸/摇晃一级交互 + 状态音效库** | D 8 + 产 3 | 11 | N4-3 | 抚摸有反应+音效；音效抢占走 N3-4 世代号 |
| N4-8 | **本地感知事件上报**：被抚摸、被拿起、电量、环境光上报给大脑。**走 `incoming` 上行，与 reply 白名单无关**（原依赖 N1-1 有误，已修正） | D | 5 | — | 大脑能收到并据此调整行为 |

### 阶段五　工程保障（里程碑 M5）

| ID | 任务 | 端 | 估 | 依赖 | 验收 |
|---|---|---|---|---|---|
| N5-1 | **pet 缓存 LRU 配额**（把 `storage` 压到 ~5MB） | D | 5 | — | 稳态 ≤5MB |
| N5-2 | **分区表改造 + OTA 执行器 + 签名 + 超时回滚** | D | 18 | N5-1 | HIL 上跑通升级与回滚 |
| N5-3 | **能力清单 manifest**：设备上报屏幕/音频/触控/pet/音效能力，GUI 自适应下发 | D 5 + G 5 | 10 | — | 新增板卡 GUI 零改动 |
| N5-5 | **伴侣心跳电源档位** | D | 8 | N4-6 | 伴侣模式续航 ≥ 当前待机 80% |
| N5-6 | **多板卡 CI 回归矩阵**（4 板，含 waveshare AMOLED 体验升级板）+ `tools/host_tests` 接入 CTest | D+测 | 8 | N3-4 | 4 板构建 + 冒烟全绿 |
| N5-7 | **仓库卫生**：补 `.gitignore`，归档根目录约 1248 个 `*.log`，删死依赖 `espressif__mqtt` | D | 2 | — | 根目录无日志残留 |

**总人日初估：约 232**（按阶段：地基 6 / 事件贯通 35 / 离线韧性 30 / 语音顺手度 48 / 伴侣人格 62 / 工程保障 51）

> 相对 v1（~243）：N0-4 由 D3 关闭（−1）、N0-1 已产出（−2）、N2-1 从 13→6（不做切换/failover/动态 capabilities）、N1-6 从 8→6（仅高危）；新增 N2-5 换绑 3 人日作为 D1 的必要补偿。

> ⚠️ 修正记录：上一版写"约 210，其中 G 68 / D 108 / H 11 / 产 17 / 测 6"。该分项与任务表逐项加总（239）**对不上，差 29**，属估算口径错误。按端拆分需团队用同一口径重估，本版只保留可验证的**按阶段小计**。

---

## 6. 排序：如果只能做三件

1. **N1 事件贯通**（尤其 N1-6 高危审批终端）—— 直接兑现"随时处理事件 + 减少手工操作"，且几乎不碰设备端
2. **N2-5 轻量换绑 + N2-2/N2-3 离线韧性** —— D1 拍板不切换大脑后，**W1 已升为"致命"**：绑定 GUI 时电脑一关机设备就彻底没大脑。N2-5 是唯一有效补偿，优先级随之上调
3. **N3-1~N3-4**（AEC 到世代号）—— 语音顺手度的地基

N0-2/N0-3 的可观测性必须第 0 步做，否则前三项收益无法证明。

✅ **N0-1 角色设定已完成**（`docs/maclaw-persona.md`），N4 全部任务的前置解除。

✅ **N0-2 延迟打点已完成并接线**（见 §9 v6），阶段一至三的收益从此可量化。真机跑一轮语音即可拿到第一条 L1 基线（`latency response=…ms latency turn …`）。

✅ **N0-3 链路健康度已完成**（见 §9 v7），F1–F4 故障分层中的 F3（大脑不在）从此有量化口径，admin 面板可直接看出"某个租户的大脑从来没连上"。阶段一至三的离线韧性收益可被证明。

**阶段 0 三件全部收口，阶段一已开工。**

✅ **N1-1 `event` reply 契约已完成**（见 §9 v8），M1 的其余四项（N1-2~N1-6）从此有共同契约，不必再各自发明字段。**§4.1 的两处修正 + 审批审计契约的 wire 强制**在写任何生产端之前就已定稿，这是先做契约的收益。

✅ **N1-3 `UpdateMachineEvent` 扇出已完成**（见 §9 v9）。**§9 v8 里留给 N1-3 的两个坑（C34/C35）都已闭合**，并且暴露出一个范式本身的缺陷：`UpdateMachineAmbient` 在 `restorePersistedCredentials` 里**没有把归一化结果写回 `saved`**，所以持久化的仍是原始 payload——对 ambient 只是白算一次 `expiresAt`，对 event 却会丢掉绝对过期时刻（即 C34 复发）。N1-3 因此显式写回，并在代码里注明这个差异是**承重的**。

✅ **N1-4 scheduler `device` 投递渠道已完成**（见 §9 v10）。这是 M1 里**第一条真正把"事件"从大脑送到设备的产品路径**：`manage_schedule` 的 `delivery.channel` 现在能填 `device`，日程触发时构造一条 `category=schedule` 的事件、经 `SendDeviceGatewayEvent` 走 N1-3 的扇出链路落到设备。三个刻意的取舍：**`severity=soft`**（提醒该按 §4.2 准入矩阵排队，而不是打断用户正在说的话）；**失败任务不升级为 `interrupt`**（失败是大脑的观测，不该变成对用户的打扰）；**不带 `actions`**（提醒没有决策点，带上动作会把一条非决策事件推进 D5-A 的审计路径，那是 N1-6 的地盘）。

✅ **N1-5 接入第一批事件源已完成 2/3**（见 §9 v12）。**高危工具审批**（`device_approval_event.go`，源为本地工具审批路径，不是 `handleVEApprovalRequest`——见 C58）+ **VE 工作流注意力事件**（`device_ve_event.go`）两条生产链路已落地并各有单测锁住契约。**第三源 `task_done` 停在"设计决定"而非"接线"**：本地后台任务完成时无推送（轮询式），`ve:workflow_status` 的分类闭集里也没有完成态（见 C61）。**N1-5 的落地同时解除了 N1-6 的审批源依赖**——D5-A 的"仅高危"判定第一次有了真实的 `security.RiskLevel` 可用。

✅ **N1-2 设备侧接线已完成**（见 §9 v13）。**事件现在能真正落到设备**：`GATEWAY_CAPABILITY_EVENT_PUSH` 能力标志 → dispatcher `event` 分类 → 纯值 `event_ingest.h`（校验 + 决策 + 忙碌时延后 + 会话幂等）→ `main.c` 呈现。三个刻意的取舍：**host 回调回传 `handled/permanently_invalid`**（不吞掉结果，否则 `soft` 事件只能"盖掉回复"或"静默丢失"，见 C67）；**幂等环只在真正呈现前记账**（否则延后的事件会在重试时被自己抑制，见 C68）；**无音频时降级为显示**（"还不会说话"不能变成"什么都没显示"，见 C70）。**仅剩"语音播报"待 N3-5**。

✅ **N1-6 大脑侧已完成**（见 §9 v14）。`POST /api/im-gateway/v1/event-ack` 现在是**决策回流的唯一入口**，Hub 对它刻意不信任设备：**按 `eventId` 关联快照**（没发过的 eventId 一律 409，防注入）、**`actionId` 必须在卡片真实提供过的动作集内**、**窗口是否过期由 Hub 自己裁决**（设备时钟不算证据；过期即强制改写为 `expired`/`timeout`，兑现 D5 的"超时走不做而非放行"）。GUI 侧**复用** `handleRegisteredToolApprovalAgentViewSubmit`（唯一落定与审计路径），只多了一条 `eventId → approvalID` 的内存映射和一个**只增不减的判定**：只有卡片真的叫 `approve` 的那个按钮能批准，其余组合（含 status/action 矛盾）一律"未批准"。**审计第一次能回答"何种方式"**——`security.AuditEntry.Source` 记 `device/voice|button|timeout`，此前只回答了"谁"和"何时"。

✅ **N1-6 设备侧已完成**（见 §9 v15），**M1 的 N1 六项至此全部闭环**。事件卡片现在是**可答的**：带动作的事件走 `gateway_event_ack_service_begin` 上屏（卡片必须点名决策对象，这是 D5 的硬要求），`event_decision.h` 持有这张卡的状态并决定「设备还欠 Hub 什么」（回执 / 决策 / 超时），主键=同意、副键=拒绝，ack 由轮询任务在**读下一页之前**发出。五个刻意的取舍：**先判过期再读手势**（D5 的"超时即不做"是安全属性，不是显示偏好，见 C85 前段）；**决策分支排在会议/录制之后**（把"想停录音"误读成"同意高危"不可逆，而反过来只是多按一次，见 C85）；**ack 不持久化**（重启宁可丢答案，也不重放一个已被服务端裁决为过期的旧同意，见 C86）；**4xx 永久丢弃、5xx 与传输错误重试**（4xx 是 Hub 按是非拒绝，重试改变不了任何一条，见 C87）；**双任务访问加锁**（撕裂读会发出 `approved` + 空 `actionId`，被 Hub 判 400 后永久丢弃，见 C88）。**纯值模块只产出闭集 outcome、不产出文案**（C84 的 ASCII 铁律），超时措辞为"未收到答复，操作未执行"而非"已拒绝"。

**下一步可立即开工的任务**：
- **N1-5 第三源 `task_done`**（1 人日，🔴 **需先拍板**）—— 两条路：① 给 `LocalBackgroundTaskManager` 加完成通知（改动集中在任务管理器，信号最准）；② 由 agent loop 在长任务收尾时发（不碰任务管理器，但依赖 loop 自己知道"什么算完成"）。**这是产品语义决定，不是技术选型**，建议先定"任务完成"对用户意味着什么再动手（见 C61）
- **N1-2 语音播报（剩余）**（依赖 N3-5）—— 决策层已把动作抽象成 `SPEAK`/`QUEUE_SPEAK`/`INTERRUPT_SPEAK`/`INTERRUPT_LISTEN`，N3-5 的流式 TTS 一落地，`main.c` 只需把这四类动作接到音频通路，**决策层不用改**（见 C53）
- **N3-1 AEC spike**（3 人日，go/no-go 闸门，建议尽早启动）—— `N0-2` 前置已解除，与 M1 无冲突，可并行

> ✅ **N1-4 scheduler `device` 投递渠道已完成**（见 §9 v10），从"可立即开工"移入已完成。它验证了 N1-3 扇出链路在生产路径上确实可用（在此之前只有单测覆盖），也把 `SplitScheduledTaskBody` 这类"给设备看的文案"从 `FormatBody` 里拆了出来。

## 7. 风险登记册

| # | 风险 | 影响 | 概率 | 缓解 |
|---|---|---|---|---|
| W1 | ⬆️ **绑定 GUI 时电脑一关机，设备就彻底没有大脑**（D1 拍板不切换，无自动兜底） | **致命**（原为"高"） | 高 | **N2-5 轻量换绑**（<1 分钟、不碰电脑）+ N2-2 离线降级 + N2-3 诚实表达 + §3.2 故障分层。**N2-5 是本风险的唯一有效补偿，不可省略** |
| W9 | 🆕 **越界指令被假装执行或静默失败**（srv 模式收到"打开浏览器"、gui 模式收到"跑三小时任务"） | 用户信任崩塌 | 高（原设计未识别） | §3.1.1 能力域声明 + 越界**引导话术**（给替代路径，而非只说做不到） |
| W2 | **AFE + Opus + MP3 + TLS 算力/内存超限**（ESP32-S3） | 阶段三失败 | 中高 | N3-1 spike 实测；降级预案：退回按键说话 |
| W3 | ~~网关 owner 竞争/接管抖动~~ | — | — | ✅ **D1 拍板"不切换大脑"后基本消除**（无运行时切换即无竞态）。仅换绑瞬间可能短暂冲突，由 N2-5 流程串行化规避 |
| W4 | **事件变骚扰** | 口碑 | 中高 | N4-5 频率预算 + 自动降频 |
| W5 | **AEC 参考信号时延对不齐**导致自激/误唤醒 | 双工不可用 | 中 | spike 阶段硬件环回测量，固定缓冲深度 |
| W6 | **16MB 分区表无空闲**，OTA 挤压 pet 资源 | OTA 延期 | 高 | N5-1 先行；老设备需一次性迁移刷机 |
| W7 | **每 clientId 独立运行时**的内存随设备数线性增长（GUI 侧） | GUI 资源 | 中 | 运行时清理机制已有（`beginRuntimeCleanupLocked`），需压测多设备 |
| W8 | 设备端工作量被低估（4 板逐一验证） | 排期膨胀 | 高 | N5-6 CI 矩阵；阶段一只在主力板跑通 |

## 8. 决策记录（D1–D7）

> **除 D6 外全部已关闭。** D1–D5 已拍板｜D7 主力板已定｜次要三项已按建议关闭｜仅 D6（按指令自动路由）为未来方向，暂不排期。

### D1　双大脑的接管机制 —— **已拍板：不切换大脑**

**已拍板：不做运行时的大脑切换。设备接入哪个大脑（GUI 或 MaClawSrv）就在配对时定下来；要换，必须重新接入。**

这与 D2（换设备需重新接入）是同一套心智模型：**绑定即固定**。

**带来的简化（工作量显著下降）**：
- 不需要设备侧主备 endpoint、failover、双凭据管理
- 不需要 MaClawSrv 常驻 claim 去抢 owner
- 不需要"大脑切换时重新下发 capabilities"的动态更新逻辑 —— 能力域在配对时确定，是**静态**的
- **W3（owner 竞争/接管抖动）风险基本消失**

配套机制（`remote_gateway_plugin.go:370-387` 已核实，仅作背景）：一个租户只能有一个 owner；同 user 换 machine 允许 takeover，不同 user 拒绝。不切换大脑的话这套语义基本用不上。

### ⚠️ 但这个拍板有一个必须正视的衍生后果

**绑定 GUI 时，电脑一关，设备就彻底没有大脑了**——不会自动切到 MaClawSrv，只能离线降级。

这不是缺陷，是选择；但前提是**"换绑"必须足够轻量**。否则用户的实际体验是：电脑关机 → 设备变砖 → 想救回来要重新走一遍完整配对（扫码/输码/等超时），大概率就放弃了。

**因此新增 N2-5：换绑流程必须轻量。** 目标：从"我要换个大脑"到可用，控制在 1 分钟内，且**不需要接触电脑**（绑定 GUI 时电脑可能就是关着的）。建议形态：
- 设备端进入设置 → 选择"接入干活模式" → 用手机扫码或设备屏显码确认
- 或 GUI/MaClawSrv 侧发起"迁移此设备到我这里"

> 这一条是 D1 拍板的**必要补偿**，不做的话 D1 会让 W1（陪伴生死线）从"高"变成"致命"。

**配套还应该做的一件事**：配对/换绑时明确告诉用户「你现在接入的是**控电脑模式**，想让它后台干活要换接 MaClawSrv」——把两种能力域的差异在入口处讲清楚，而不是让用户事后撞墙。

---

### D2　设备绑定的是"机器"还是"用户"？ —— **已拍板**

**已拍板：方案 A，绑设备（维持现状）。一台设备一次只接入一个大脑；换设备必须重新接入，不接续上次会话。**

当前配对绑定的是 **machineID + tenant + user**（结构体 `devicePairing` 见 `device_gateway.go:195`，创建处 `:1170`），一个设备只能绑一台机器——**保持不动**。

**这条拍板带来的简化（工作量下降）**：
- 不需要把 `hardwareAgentRuntime` 的键从 `clientId` 重构为 `user+device`，沿用现有 per-device 隔离即可
- 不需要跨设备的会话恢复；`cursor` 只需服务**同一台设备的断线重连**，不服务设备切换
- N4-1 / N4-2 的工作量相应下调

**但必须明确一件事：人格、记忆、好感度要分层，不能一锅端。**

| 层次 | 归属 | 换设备后 |
|---|---|---|
| **人格（persona）** | **产品级常量**，随 GUI/固件版本发布，与设备无关 | ✅ 保持一致（换设备还是同一个"码卡龙"） |
| **长期记忆** | per-device，存在 `hardwareAgentRuntime` 的 memoryStore | ❌ 重置（用户已知并接受） |
| **好感度 / 成长** | per-device | ❌ 重置（用户已知并接受） |

配套两个体验要求：
1. **首次接入引导**：换设备后应明确"从今天重新开始"，把这变成一个有仪式感的开场，而不是让用户以为它失忆了
2. **旧设备数据处置**：明确丢弃，并在恢复出厂/重新接入时告知用户（纳入 `factory_reset_policy` 的个人数据擦除白名单）

> 注：这一条不影响 D1。同一台设备仍可在 MaClaw GUI 与 MaClawSrv 之间切换（那是大脑切换，不是换设备）。

---

### D3　全双工后唤醒词怎么办？ —— **已拍板：B**

**已拍板：首次唤醒 + 免唤醒窗口（20 s）。**

| 方案 | 说明 | 状态 |
|---|---|---|
| A. 全程保留唤醒词 | 每轮都要说"码卡龙" | 否 |
| **B. 首次唤醒 + 免唤醒窗口（20 s）** | 唤醒后开窗，窗口内可直接说话；静默超时或显式结束语（如"退下"）关窗 | ✅ **已定** |
| C. 全免唤醒 | 纯靠语义端点判断 | 否 |

**落地要求**（窗口机制比看起来复杂，三件事必须一起做）：
1. **窗口期的隐私可见化要更强** —— 窗口内等于一直开着麦，N5-4 的指示状态必须区分"待机监听唤醒词"和"对话窗口进行中"两种，让用户一眼看出区别
2. **窗口退出条件要显式** —— 除静默超时外，要有明确的结束语；同时设备播报开始时不应误判为用户插话而关窗
3. **窗口内误触发容忍度更高** —— 20s 内不该再要求唤醒词，否则窗口形同虚设

阻塞解除：N3-2、N3-3 可开工。

---

### D4　局域网直连的定位？ —— **已拍板：两种都支持**

**已拍板：局域网直连和 Hub 中转都要支持；非内网环境必须能走 Hub 中转。**

`guiapp/thirdparty_gateway.go` 本身就是 gateway server（默认 `127.0.0.1:18777`，`ThirdPartyGatewayHost` 可配 `0.0.0.0`），LAN 直连在技术上已具备。

**落地形态**：设备侧做**传输优选**——

```
探测同网 LAN 直连可用  ──是──►  走 LAN（低延迟、不出公网）
        │
       否（不在内网 / 对方未开放 / 探测超时）
        ▼
   走 Hub 中转（必须始终可用，这是底线）
```

三条硬性要求：
1. **Hub 中转是保底路径，不能因为优选 LAN 而弱化**。D4 的原话是"非内网环境要支持 Hub 中转"，所以 Hub 链路的健壮性优先级**高于** LAN 加速
2. **LAN 探测必须有超时与失败回退**，探测卡住不能拖死启动
3. **两条路径的能力域必须一致**——不能出现"走 LAN 能控电脑、走 Hub 就不能"。能力由绑定对象决定（D1），与传输路径无关

阻塞解除：N2-4 可开工。

---

### D5　设备审批终端开放到什么范围？ —— **已拍板：A，仅高危**

**已拍板：只推高危操作**（删除、外发、支付、不可逆动作）。中低危一律不打扰，由 GUI 侧按现有策略处理。

这个选择偏保守，我认同——审批终端的价值在于**兜住不可逆的后果**，而不是接管所有确认。范围开太大，设备会变成打扰源，用户最后会把整个能力关掉，那就什么都没了。

**因此 N1-6 的验收标准要收紧，三条硬要求**：

1. **高危必须有明确定义，不能靠运行时灵机一动**。建议给工具打静态 `risk` 标记（复用 `device_tool_registry.c` 已有的 `risk` 字段语义），只有 `risk=high` 才推设备
2. **设备上必须显示足够上下文** —— 「要删除 `~/Downloads/report-final-v2.xlsx` 吗？」，而不是「要执行一个删除操作吗？」。**盲确认等于没确认**
3. **高危确认必须可审计** —— 谁、什么时候、通过什么方式（语音/按键）批准的，要能追溯

**范围收窄带来的简化**：事件量很小，`§4.2` 准入矩阵里 `severity=interrupt` 基本只服务这一类事件，`§4.1` 的 `ttlSec` 也只需为高危审批设置（建议 300s，超时即拒绝而非放行——**高危操作超时必须走"不做"**）。

阻塞解除：N1-6 可开工。

---

### D6　未来：是否支持按指令类型自动路由到合适的大脑？

MaClawSrv README 写明"一个 user 下可跑多个逻辑实例"，技术上允许 GUI 与 MaClawSrv **同时在线**，按指令类型分流（桌面类→GUI，长时/定时→MaClawSrv）。

但当前 Hub 是"一个租户一个 owner"（`ClaimGatewayForTenant`），需先放开为**带 capability tag 的多 owner** 才能路由。

- **当前阶段：不排期**。D1 已拍板"不切换大脑"，连运行时切换都不做，并行路由更远
- 但 §3.1.1 的 `capabilities[]` 字段现在就要留好 —— 它同时服务"声明当前能力域"（N5-3 就要用）和将来的路由，**现在留成本为零，将来补要改协议+设备+Hub 且需兼容老固件**

### D7　主力板 —— **已定：echoear 2ST**

**已定：`echoear-2st-r8` 为阶段一至阶段四的主力开发板。**

核实依据（`main/boards/echoear_2st/board_profile.c`）：
- `360×360` **圆屏** + `DEVICE_CAPABILITY_ROUND_DISPLAY`
- **`DEVICE_CAPABILITY_TOUCH_INPUT`**，CST8xx 触摸控制器（I2C `0x15`）
- `primary_interaction_source = TOUCH`，标签"屏幕"，且已有完整手势系统（`double_tap_window_ms` / `long_hold_ms` / `touch_regular_min_tap_ms` / `touch_drag`）
- **是 `main/Kconfig.projbuild` 的默认选项**（`choice MACLAW_BOARD` 默认 `ECHOEAR_2ST`），成熟度最高

**选它的理由**：伴侣所需的硬件它全有（圆屏 + 触摸 + 手势），而**手势系统正是 N4-7「抚摸/摇晃」的现成基础**，不需要新硬件。同时它是默认配置，回归风险最低。

**waveshare AMOLED 1.75C**（`466×466` 圆屏 + AMOLED 触摸）作为**体验升级板**，在 M5 多板卡回归（N5-6）时一并验证，不进主力路径。

### 次要决策 —— **已按建议关闭**

| 议题 | 决定 | 理由 |
|---|---|---|
| 主动搭话默认开关 | **默认开，但首周只开"早间简报"一类** | 先让用户体验到"它会主动"，但用最不容易出错的一类事件试水 |
| 好感度是否可见 | **隐藏，只在阶段跃迁时给惊喜** | 显示数值会诱导刷分，破坏"陪伴"感 |
| 是否立项 L4 端到端语音预研 | **仅跟踪，不立项** | L3 已能给约 90% 体验；L4 依赖模型侧能力，成本不可控 |

---

## 9　评审与修订记录（v1 → v15）

### v1 → v2

| # | 类型 | 问题 | 处理 |
|---|---|---|---|
| C1 | 🔴 设计漏洞 | **误把 MaClawSrv 当成 GUI 的降级备胎** —— 核实时发现 `computer_use_*` 只在 `guiapp/`、MaClawSrv 零命中（v2 写成"L1 降级大脑"是错的） | v3 重构为 §3.1 **两种接入 = 两种能力域（分工）**：GUI=手（控电脑），MaClawSrv=后台劳力（干活）；并新增 §3.1.1 越界**引导**话术（给替代路径而非只说做不到） |
| C2 | 🔴 设计缺失 | 故障只有"断网"和"大脑不在"两层，Hub 不可达、大脑能力受限没设计 | 新增 §3.2 故障分层（F1–F4），四层表现必须不同 |
| C3 | 🟠 设计不足 | 事件只有字段列表，缺幂等/超时/回执/离线补发语义 —— 审批事件必然重复提问 | 新增 §4.1 五条语义 + `dedupeKey` / `ttlSec` / `requiresAck` / `persist` |
| C4 | 🟠 设计不足 | severity 只定义了呈现方式，没定义"何时能打断" | 新增 §4.2 事件准入矩阵 + 两条硬规则（勿扰不破例、会议录音不打断） |
| C5 | 🟠 协议错配 | 审批确认要走 `tool-result`，但该端点语义是"设备执行了工具" | 新增 §4.3 独立 `event-ack` 端点 |
| C6 | 🟡 依赖错误 | N0-4 验收写"阻塞 N2-2、N2-3"，实际应阻塞 N3-2/N3-3 | 已修正 |
| C7 | 🟡 依赖错误 | N4-8 本地感知上报依赖 N1-1（reply 白名单），但它是上行走 `incoming`，无关 | 已改为无依赖并注明 |
| C8 | 🟡 排期错误 | N5-4 隐私可见化排在阶段五，但它是双工上线门禁 | 前移至阶段三，与 N3-2 同批 |
| C9 | 🟡 数字错误 | 总人日写 210，任务表逐项加总为 239（差 29）；按端分项也与表不符 | 改为可验证的按阶段小计，按端拆分标为需重估 |
| C10 | 🟢 拍板落地 | D2 已定：绑设备、换设备重新接入、不接续会话 | 人格改为产品级常量，记忆/好感度 per-device 且换设备重置；N4-1/N4-2 工作量下调（13→10、5→4） |

### v3 → v4

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C11 | 🟢 拍板落地 | **D1 不切换大脑**（绑定即固定） | N2-1 从 13→6：删去主备 endpoint / failover / MaClawSrv 常驻 claim / 动态 capabilities 更新；能力域变为**静态**，仅换绑时刷新 |
| C12 | 🔴 拍板衍生后果 | **D1 让"绑定 GUI 时电脑一关机设备就没大脑"从可选风险变成确定事实** | 新增 **N2-5 轻量换绑**（3 人日）：<1 分钟、**不需要接触电脑**。标注为 D1 的**必要补偿**，不可省略；W1 由"高"升为**致命** |
| C13 | 🟢 拍板落地 | **D3 = B**（首次唤醒 + 20s 免唤醒窗口） | 补充三条落地要求：窗口期隐私指示要区分"待机监听"与"对话中"、退出条件显式化、窗口内不再要求唤醒词。N3-2/N3-3 解除阻塞 |
| C14 | 🟢 拍板落地 | **D4 = LAN 与 Hub 中转都支持，非内网必须走 Hub** | 明确 Hub 中转是**保底路径，健壮性优先级高于 LAN 加速**；LAN 探测需超时回退；两条路径能力域必须一致。N2-4 解除阻塞 |
| C15 | 🟢 拍板落地 | **D5 = A 仅高危** | N1-6 从 8→6；补三条硬要求：高危用静态 `risk` 标记、**必须显示具体操作对象**（盲确认等于没确认）、决策可审计；**高危超时走"不做"而非放行**。N1-6 解除阻塞 |
| C16 | ✅ 风险消除 | D1 不切换后，**W3（owner 竞争/接管抖动）基本消失** | 标注为已消除，仅换绑瞬间需串行化 |

### v4 → v5

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C17 | 🟢 拍板落地 | **D7 主力板定案** | 核实 `echoear_2st/board_profile.c`：360×360 圆屏 + `TOUCH_INPUT`（CST8xx, I2C 0x15）+ 完整手势系统 + 是 Kconfig 默认。选为阶段一至四主力板；**手势系统是 N4-7 抚摸交互的现成基础，不需新硬件**。waveshare AMOLED 1.75C 作体验升级板，进 M5 回归 |
| C18 | 🟢 拍板落地 | **次要三项按建议关闭** | 主动搭话默认开（首周仅"早间简报"）｜好感度隐藏（仅阶段跃迁给惊喜）｜L4 仅跟踪不立项 |
| C19 | ✅ 任务完成 | **N0-1 角色设定已产出** `docs/maclaw-persona.md` | 含性格三条、说话五铁律、红线、**10 个分场景正反例**（越界/离线/高危确认等）、熟悉度三阶段、工程落点。−2 人日；N4 前置解除 |
| C20 | 🟡 数字修正 | 总人日 234 → **232**（阶段 0：8→6） | 已核对逐项加总 |

### v14 → v15

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C82 | 🔴 前提纠正 | **"先做语音决策"不成立：设备没有本地 ASR** | C77 建议 ③（复用既有 ASR 做语音决策），核实后**推翻**：`iot-agentos` 只有语音**上行**（`gateway_transport_upload_voice` + `send_voice_event`），ASR 与 agent 都在 Hub 侧，设备**拿不到任何转写文本**，语音决策无从谈起。改为走「手势/物理键」：`app_intent_service` 已把触摸与物理键统一抽象为 `APP_INTENT_PRIMARY_ACTIVATE`/`SECONDARY_ACTIVATE`（ABI v3），板级适配器各自翻译，故这一条路径**板无关**，且不需要任何新硬件 |
| C83 | 🟢 设计决定 | **`intent → 决策` 的映射只写在输入绑定里** | 纯值模块收的是抽象 `EVENT_DECISION_INPUT_PRIMARY/SECONDARY`，不是 `APP_INTENT_*`。理由是双向的：把 `app_intent_service.h` 拉进 `event_decision.h` 会让它**无法在主机编译**（该头带 ESP-IDF 依赖），安全规则随之失去单测；而"哪个手势是确认"本就是**呈现层的板级事实**，换板不该动安全规则 |
| C84 | 🟢 铁律 | **纯值头与主机单测保持纯 ASCII；UI 文案留在胶水** | 本树既有的 `event_ingest.h` / `event_presentation_policy.h` 与它们的主机单测**非 ASCII 字节数均为 0**。最初 `event_decision.h` 直接写"已同意/已拒绝/已超时"，破了这条规矩。改为**只产出闭集 `event_decision_outcome_t`**：语义（超时是独立结果、绝不是拒绝）仍在纯值侧并被单测锁住，中文措辞下沉到 `gateway_event_ack_service.c`（与 `main.c`/`input_binding.c` 的既有 UI 文案一致）。check 脚本把 ASCII 纯度做成断言，防止再漂移 |
| C85 | 🔴 安全取舍 | **决策分支排位：晚于会议/录制，早于语音/取消** | `input_binding_handle_event` 的插入点在 `meeting_service_is_active()` 之后、`SECONDARY_ACTIVATE` 语音分支之前。方向是**故意的**：若排在录制之前，用户"想停录音"的一按会被读成"同意高危操作"——**不可逆的外部动作**；反过来只是"先停录音再按同意"，多一次操作而已。同理由也决定了它必须早于语音：按在卡片上的一下绝不能变成开始录音 |
| C86 | 🟢 设计决定 | **决策 ack 不持久化** | 重启丢答案 vs 重放一个旧答案：后者更危险。Hub 已把窗口过期**改写**为 `expired`（C73），设备重放的"同意"会被判成超时，GUI 随之**关掉桌面待决审批**（C78 的反向风险），而用户以为自己批准了。且 `persist=true` 的卡片会在重连后**重新补发**，用户重按一次即可。故：内存单槽、重启即空 |
| C87 | 🟢 设计决定 | **4xx 永久丢弃，5xx 与传输错误重试** | Hub 的 `handleEventAck` 用状态码区分了两件事：4xx（401/403/400/409）是**按是非拒绝**（未知事件、clientId 不符、动作没提供过），重试改变不了；5xx（503 `gui_unavailable`）是**暂时不可用**，必须重试。`gateway_transport_post_json` 会把非接受码一律压成 `ESP_FAIL`，丢失这个区分，故服务改用 `gateway_transport_request` 自己读 `response.status` |
| C88 | 🔴 并发 bug | **待决槽被两个任务访问却无保护** | 轮询任务（装填/上报）与输入任务（记录答案）都会读写 `s_pending`。撕裂读的后果不是显示错，而是**发出 `status=approved` 配空 `actionId`** —— Hub 判 400，按 C87 永久丢弃，**用户答案永久丢失**。故全部访问进 `portMUX` 临界区，且锁**绝不跨 POST 或重绘**（各入口拷贝快照、出锁后再干活）；回执在飞行中被决策取代是唯一合法的交错，故 `mark_ack_delivered_if_same` 回读 `eventId` 复核 |
| C89 | 🔴 逻辑 bug | **`settled()` 会把"仍在屏上待答"的卡片判为已结算** | 原实现：`!active && !busy` 直接返回 true，否则看 `next_ack`。一张 `requiresAck=false` 的卡片**不欠 Hub 任何东西**，但仍在等用户作答，却被判 settled → 槽被清空 → 用户按下去什么也不做，卡片**静默失去回答能力**。改为：可答（`active`）的卡片**永不 settled** |
| C90 | 🔴 逻辑 bug | **`mark_ack_delivered(TIMEOUT)` 不置 `timed_out`，会无限重发** | `next_ack` 有一条分支能在 `note_window` 尚未运行前就返回 `TIMEOUT`（"没人看着也该告诉 Hub"）。若调用方只 `mark_ack_delivered` 而不先 `note_window`，`timed_out` 仍为 false，下一次 `next_ack` 又返回 `TIMEOUT` → 每次轮询重发一次超时。改为该函数**同时置 `timed_out`**，使两种调用顺序都自洽；单测补了这条乱序路径 |

**N1-6 设备侧的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 待决卡片状态机（纯值） | `iot-agentos/main/services/event_decision.h` | eventId / 动作集（`kind=="primary"` 才算确认，**未声明 ≠ 低风险**）/ 绝对过期时刻 / 回执-决策-超时优先级；**无 ESP-IDF、无 cJSON、纯 ASCII、主机可编译** |
| 主机单测 | `iot-agentos/tools/host_tests/test_event_decision.c`（19 组） | 覆盖：晚手势拒绝、无窗口卡片永不过期、决策压制回执、答案落地前新卡片不得覆盖、闭集 outcome（超时≠拒绝）、越界 id 截断不溢出 |
| 漂移守卫 | `iot-agentos/tools/check-event-decision.ps1` | 纯度 + 必需符号 + outcome 闭集 + **ASCII 断言** + 与 `corelib/im/device_event.go` 逐值比对 ack 状态/`decidedBy`/三个长度上限 + 单测 |
| 卡片呈现与 ack 上报（胶水） | `iot-agentos/main/services/gateway_event_ack_service.{h,c}` | 卡片**必须点名决策对象**（D5）；`portMUX` 保护；4xx 丢弃 / 5xx 重试（C87）；不持久化（C86） |
| 手势映射 | `iot-agentos/main/presentation/input_binding.c` · `approval_gesture_for` + 决策分支 | 唯一业务派发点；排在会议/录制之后、语音之前（C85） |
| 事件进入决策槽 | `iot-agentos/main/main.c` · `gateway_host_apply_event` | **先问槽是否空闲、再记幂等键**（否则被推迟的卡片会在重试时被自己抑制）；带动作的卡片走应答卡，其余仍是普通消息卡 |
| 上报时机 | `gateway_dispatcher_host_t.flush_event_ack` → `poll_reply()` | 排在消息 ACK outbox **之后**（它持有页游标顺序保证）、tool-result 队列**之前**（决策有截止时间，不能排在可能卡住的持久队列后面）；**失败不阻塞读页**，ack 留欠下次重试 |
| 端点 | `POST /api/im-gateway/v1/event-ack` | 载荷只发契约要求的最小集：`clientId/eventId/status` + 决策时 `actionId/decidedBy`。**不发 `ackId`**：Hub 的去重键是 `(eventId,status,actionId)`，本就设计为不依赖设备 ackId；不发的路径已被单测覆盖 |
| 两端契约钉死 | `corelib/im/device_event_ack_test.go` · `TestNormalizeEventAckAcceptsEveryTupleTheTerminalEmits` | 把设备侧**能发出的全部四种元组**（receipt / approved+approve+button / rejected+reject+button / expired 无 action 无 modality）列为表，逐条喂给 `NormalizeThirdPartyEventAckRequest` 并断言归一化后的 `DecidedBy`。设备侧不管怎么改，只要发出第五种形状就会在这里炸。**写这条时先写错过一版**（把"approved 不带 modality"当成合法），被契约拒绝后才发现——正是这类测试该起的作用 |

**验证状态**：主机单测 **19 组全过**（`gcc -std=c11 -Wall -Wextra -Werror`，零警告）；`tools/check-event-decision.ps1` **EXIT=0**。四个改动/新增编译单元（`main.c` / `input_binding.c` / `gateway_dispatcher.c` / `gateway_event_ack_service.c`）用**真实 `xtensa-esp32s3-elf-gcc` + 真实工程 include 路径 `-fsyntax-only`** 全部零错误（`tools/_syntax_check.py`；新文件无编译记录，借用同组件 `gateway_dispatcher.c` 的参数）。新增的 `gateway_event_ack_service.c` 进一步**编译到目标码成功**（84,828 字节）并用 `xtensa-esp32s3-elf-nm` 核对：**6/6 公开符号已定义**，`-u` 的未定义项逐个可归源（cJSON / `esp_log` / `esp_timer_get_time` / FreeRTOS 临界区 / libc / 同组件的 `gateway_transport_*` 与 `scene_presenter_*`）——链接层面无隐患。既有 `check-event-ingest.ps1`（16 组）、`check-event-presentation-policy.ps1`（10 组）、`check-gateway-capability-projection.ps1`、`check-gateway-tool-result-outbox.ps1`、`check-gateway-transport-asset-cancellation.ps1` 全部仍 **EXIT=0**。纯值头与主机单测**非 ASCII 字节数 0**（C84）。另在 `corelib/im/device_event_ack_test.go` 补上原本缺失的一环：**从"设备会发出什么"出发的「黄金元组」测试**（`TestNormalizeEventAckAcceptsEveryTupleTheTerminalEmits`）——把设备侧能发出的全部四种元组列成表喂给 `NormalizeThirdPartyEventAckRequest` 并断言归一化后的 `DecidedBy`，外加一条"设备不发 `ackId`"。全包 `go test ./corelib/im/` 通过。**写这条时我先写错了一版**：把"`approved` 不带 `decidedBy`"当成合法载荷，被契约以 `decidedBy must be voice or button for a decision` 拒绝——这正说明它有用："按了什么却不说怎么按的"正是审计契约拒绝写的记录。

**⚠️ 整机 `idf.py build` 未成功**（不是"没跑"，是**跑不通**），真机端到端验收因此仍未达成。实测过程与结论：
- 补齐了 `iot-agentos/scripts/idf_run.py`（Git Bash 强制注入 `MSYSTEM=MINGW64` → `idf.py` 只打警告不调 `main()`；唯一可靠做法是在 Python 里过滤 `os.environ` 再 exec）。加 `-D SDKCONFIG=sdkconfig.echoear-2st -D MACLAW_PROFILE=echoear-2st` 后 cmake 正常起步，交叉编译器识别正确。
- **cmake 阶段就失败**：`Manifest files have changed, solving dependencies` → 组件管理器**就地改写**了受版本控制的 `dependencies.lock.echoear-2st`（删掉 `cmake_utilities`/`esp_lcd_st77916` 条目、`dl_fft` 0.7.0→0.8.0），随后要删 3 个 `managed_components/` 目录，被环境的批量删除守卫挡下（`[safe-delete][SAFE_DELETE_BULK_CONFIRM_REQUIRED]`，`scope:"turn"` 是本回合累计计数），cmake 以 `exit 1` 结束，**一行代码都没编译**。
- **已还原** `dependencies.lock.echoear-2st`（`git checkout --`），工作区不含这次的连带污染。**任何人跑整机构建前请先确认该文件干净、跑完再确认一次。**
- 因此本轮**不声称**"已通过整机构建"。已有验证覆盖到"每个 TU 能用真实工具链编出目标码 + 符号齐备"，未覆盖的是链接成完整固件与真机行为。**下一步建议**：在一个允许删除 `managed_components/` 缓存、或已为 `echoear-2st` 求解好依赖的环境里跑一次 `idf.py build`，再做真机端到端验收（高危审批 → 按键 → GUI 落定 → 审计 `Source=device/button`）。

### v13 → v14

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C72 | ✅ 任务完成 | **`POST /api/im-gateway/v1/event-ack` 落地** | `hub/internal/im/device_event_ack.go` · `handleEventAck`：`principal` → `machineHardwareEnabled` → `decodeDeviceJSON` → `clientId` 比对 → `coreim.NormalizeThirdPartyEventAckRequest` → **按 `eventId` 关联 `eventsByMachine` 快照** → **`actionId` 白名单** → **过期裁决** → 去重 → 投递 → 终态清快照。`ServeHTTP` 加路由；新增只读的 `deviceEventHandled`（`markDeviceEvent` 的预检半边）；Hub 单测 11 例 |
| C73 | 🔴 设计修正 | **过期由 Hub 裁决，且"改写"而非"拒绝"** | 设备时钟不是证据：一条晚到的 `approved` 若直接采信，就等于让设备自己决定"窗口还在不在"。但**也不能拒绝**——事件是真的，只是没有有效决策了；返回 400/409 会让"晚按的同意"看起来像服务端故障，审计里则什么都没有。故 Hub 把 `status` 改写为 `expired`、清空 `actionId`、`decidedBy` 置 `timeout` 后**照常投递**，让审计留下"无人作答"这个**正确的事实**，兑现 D5 的"超时走不做而非放行" |
| C74 | 🔴 设计修正 | **先投递、后记账（去重顺序不能照抄 `handleToolResult`）** | `handleToolResult` 是"先 `markDeviceEvent` 再异步转发"，对**用户消息**合理（丢了就是丢了）；对**审批决策**不行：若先记账、投递失败（GUI 送信缓冲满），用户答复**永久消失**，且每次重试都得到 `duplicate:true`。故拆出只读 `deviceEventHandled` 做预检，`markDeviceEvent` 只在**投递成功之后**调用。代价是"并发重复投递"窗口——但 GUI 侧本身幂等（审批被消费后第二次决策返回 `approval not found`），窗口无害 |
| C75 | 🔴 设计修正 | **`SendToMachine` 的"离线"不是失败，"缓冲满"才是** | 核实 `hub/internal/device/runtime.go:476`：GUI 离线时 `SendToMachine` 会把消息 **`PendingMessages.Enqueue` 缓冲**并返回 `ErrMachineOffline`（重连后 `DrainPendingMessages` 补投）；只有送信缓冲满才返回 `fmt.Errorf("%w: %w", ErrMachineOffline, ErrMachineSendBufferFull)` 且**不缓冲**。若把 `ErrMachineOffline` 当 503，设备会重试 → 同一决策被缓冲多次；若把缓冲满当成功，决策静默丢失。故只对 `errors.Is(err, ErrMachineSendBufferFull)` 返回 503，其余记日志后接受。**判据是 `errors.Is` 而非错误串**：缓冲满同时包装了 `ErrMachineOffline` |
| C76 | 🟢 设计决定 | **新信封 `im.device_gateway_event_ack`，不复用 `im.gateway_reply`** | `im.gateway_reply` 是 IM 适配器的出站通道，`guiapp` 按 `payload.Platform` 派发给 `qqBotGateway`/`weixinGateway`/`lansengerGateway`——**按键不是 IM 消息**，塞进去会被当成平台回复。设备网关自己已有一族 `im.device_gateway_*` 信封（`playback_receipt`/`devices`），新成员沿用该族，`guiapp` 侧只需加一个消息类型常量（Hub 的 `internal` 包不可被 `guiapp` import，字面量与 `playback_receipt` 同样重复一份） |
| C77 | ✅ 任务完成（v15 落地） | **N1-6 设备侧：输入层存在，但事件卡片没有接入它** | 先纠正一处**误判**：设备**并非**没有输入抽象——`iot-agentos/main/app_intent_service.{h,c}`（`APP_INTENT_PRIMARY_ACTIVATE`/`SECONDARY_ACTIVATE`/`*_CONTACT_DOWN`，ABI v3）+ 板级适配器（`echoear_input_adapter.h` 等）+ `main/presentation/input_binding.c` 的 `input_binding_handle_event` 是**唯一的业务派发点**，且已有"前台面板优先占用输入"的成熟范式（疑似跌倒提示、会议、回复页）。**真正的缺口是**：`main.c` 的 `gateway_host_apply_event` 只 `scene_presenter_publish_message(title, body)`——**动作集被解析却从未用于呈现，卡片也没有"待决策"状态**，所以按键无处可去。落地清单：① 纯值模块（待决卡片状态：eventId/actions/绝对过期）+ `intent → actionId` 映射（`PRIMARY_ACTIVATE`=approve、`SECONDARY_ACTIVATE`=reject）；② `input_binding_handle_event` 加一条**先于语音/会议**的分支；③ `gateway_transport` 加 `POST /api/im-gateway/v1/event-ack`（`gateway_transport_request` 已有）；④ 超时上报（复用 C73 的服务端裁决，设备侧只需在窗口结束时发 `expired`）。**四条已于 v15 全部落地**；~~建议先只做语音决策~~ → **该建议作废**，见 C82（设备无本地 ASR，语音决策无路可走），实际走手势/物理键路径 |
| C78 | 🟢 设计决定 | **只有 `approve` 能批准；`expired` 不关桌面提示** | GUI 侧判定是"只增不减"的一条：`status==approved && actionId=="approve"` 才批准，**其余一律"未批准"**——含 `status/action` 矛盾（`approved`+`reject`）、未知 `actionId`、大小写不同（`APPROVE`）。`actionId` 常量 `deviceApprovalApproveActionID` 与 `buildToolApprovalDeviceEvent` 共用，两处不可能漂移。`expired` **不触碰待决审批**：设备超时只是设备放弃，桌面提示仍然可答（关掉它会把一次设备侧小故障变成一次决策丢失），故只写一条 `device_approval_timed_out` 审计 |
| C79 | 🟢 设计决定 | **"何种方式"进 `security.AuditEntry.Source`，不进 `Result`** | N1-6 的三条硬要求里，"何种方式"此前**无处可落**：`UserID` 答"谁"、`Timestamp` 答"何时"，而 `Result` 是机器可读的动作名（`agent_view_approval_approved`）。把 `device/voice` 拼进 `Result` 会让机器可读字段开始承载散文。`AuditEntry.Source` 字段本就存在且语义正是"这条审计是谁产生的"，故把 `recordAudit` 拆出 `recordAuditFromSource`（8 个既有调用点**行为逐字节不变**，源码路径传 `""`），设备路径传 `device/voice|button|timeout` |
| C80 | 🟢 设计决定 | **`eventId → approvalID` 映射：内存、有界 64、不持久、决策后不消费** | 终端永远看不到 `approvalID`（它只拿到生成的 `eventId`），所以关联必须留在生产端。**不持久化**：Hub 模式的 GUI 重启后**根本没有待决审批**可解析，持久化的链接只会指向空。**决策后不删除**：`expired` 要让桌面提示继续可答（C78），重复 ack 也应被识别为"已处理"而不是"未知事件"；有界 + TTL（30 分钟，与 `pruneRegisteredToolPendingApprovals` 对齐）负责回收 |
| C81 | 🟢 复用 | **GUI 决策复用 `handleRegisteredToolApprovalAgentViewSubmit`，不新开落定路径** | 该函数是审批**唯一的落定与审计路径**（拒绝 → `deleteRegisteredToolPendingApproval` + `agent_view_approval_rejected`；同意 → `firewall.ApproveForSession` + `agent_view_approval_approved` + `removeRegisteredToolPendingApprovalPreservingExecutionToken` + 执行）。设备路径**只多传一个 `_device_decision_source`**（C79），因此自动继承执行令牌语义与全部既有审计，不可能与桌面路径分叉出第二套安全策略。**新加的三处都必须是"只增不减"的判定**：未知事件丢弃、未知 action 视为未批准、`expired` 不关提示 |

**N1-6 大脑侧的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 端点与路由 | `hub/internal/im/device_event_ack.go` · `handleEventAck` + `device_gateway.go` 的 `ServeHTTP` | 与 `/tool-result` 同族但**语义不同**（决策 ≠ 工具执行结果，见 §4.3 / C5） |
| 事件关联 | `pendingDeviceEventLocked(machineID, eventID)` | 按 `eventId` 关联**本机**快照；没发过的 eventId 一律 409（防注入） |
| 动作白名单 | `deviceEventActionDeclared(stored, actionID)` | 决策必须命名卡片**真实提供过**的动作；`received`/`expired` 不带 action 直接放行 |
| 过期裁决 | `deviceEventWindowClosed(stored, now)` | 用 Hub 自己的 `ExpiresAtUnixMs`；过期即**改写**为 `expired`/`timeout`（C73） |
| 去重 | `deviceEventHandled`（预检）+ `markDeviceEvent`（投递后记账） | 顺序不可颠倒（C74）；键为 `coreim.ThirdPartyEventAckEventID` = `event_ack:<eventId>:<status>[:<actionId>]`，**不依赖设备 `ackId`** |
| 投递 | `deliverDeviceEventAck` → `sender.SendToMachine(machineID, {type: "im.device_gateway_event_ack", payload})` | 只把 `ErrMachineSendBufferFull` 当失败（C75） |
| 快照生命周期 | `clearMachineEvent(machineID)` | **仅终态**（`approved`/`rejected`/`expired`）清除；`received` 不清（卡片还在等答复，重启应能重现）——兑现 C69 说的"跨重启簿记与快照生命周期合并" |
| GUI 分发 | `guiapp/remote_hub_message_type.go`（常量）+ `remote_hub_client.go`（`go c.handleDeviceGatewayEventAck(msg)`） | 必须 `go`：决策可能以**真实工具执行**收尾，读循环不能被它挡住 |
| GUI 决策 | `guiapp/device_event_ack.go` · `applyDeviceEventAck` → `resolveDeviceApproval` → `handleRegisteredToolApprovalAgentViewSubmit` | 只有 `approve` 能批准（C78）；复用唯一落定路径（C81） |
| GUI 映射 | `deviceEventApprovalStore` + `rememberDeviceEventApproval`（在 `pushToolApprovalToDevices` 投递成功后记） | 内存、有界 64、TTL 30 分钟、决策后不消费（C80） |
| 审计"何种方式" | `security.AuditEntry.Source`（`guiapp/security_firewall.go` · `recordAuditFromSource`） | 既有 8 个调用点行为不变（C79） |
| 超时审计 | `recordDeviceApprovalTimeout` → `device_approval_timed_out` + `Source=device/timeout` | **不**拒绝待决审批（C78） |

**验证状态**：`go build ./hub/...` 与 `go build ./guiapp/` 全过。Hub 单测 `go test ./hub/internal/im/ -run 'EventAck|DeviceEventActionDeclared|DeviceEventWindowClosed'` **11 例全过**；`hub/internal/im` 全量 **25.2s 全过**。GUI 单测 `go test ./guiapp/ -run 'DeviceEventAck|DeviceDecisionAuditSource|DeviceEventApprovalLink|RememberDeviceEventApproval'` **9 例全过**。**宽回归**（`Approval|Audit|Firewall|DeviceApproval|DeviceEvent|DeviceVe|DeviceAmbient|ToolDatabase|MisData|SemanticAudit`）3 个 FAIL，**均已定性为与本次改动无关**：① ② `TestInstallSharedPublishedApprovalFixture*` 为已知的 `t.TempDir()` 清理竞态（**单跑即过**，见 v12 的同一定性）；③ `TestRecordExperienceTraceFollowUpTriggeredRollbackReviewKeepsAuditKind` **单跑仍失败**，但**被测实现（`guiapp/tools_experience_learning.go`、`experience_learning_snapshot.go`）与测试文件（`experience_learning_followup_test.go`）相对 HEAD 均未被修改**，且该用例的 `-run` 选择器不含任何本次新增用例——即失败发生在**未经本次改动触碰**的代码上，是工作区既有的先存失败。**设备侧已于 v15 补上**（C77），但**端到端验收（真机 + 真实 Hub + 真实 GUI 走完一次高危确认）仍未跑**，需整机 `idf.py build` 与联调。

### v12 → v13

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C65 | ✅ 任务完成 | **设备侧 event 能力标志落地** | `GATEWAY_CAPABILITY_EVENT_PUSH = 1u<<13` + `KNOWN_MASK`；`gateway_transport.c` 的 `local_gateway_capabilities()` 与**两个** `feature_flags[]`（声明 + 解析 Hub 接受集）各加 `{"eventPush", ...}`。**必须同时改声明与解析两处**：只改声明则设备宣告了、却读不回 Hub 的接受，能力租约永远不生效 |
| C66 | ✅ 任务完成 | **dispatcher `event` 分类落地** | `type=="event"` 走独立分支，能力门为 `GATEWAY_CAPABILITY_EVENT_PUSH`；事件对象从嵌套 `item["event"]` 取（与 `ambient` 同约定，信封保持类型无关）。纳入 `ack_message` 与 `permanently_failed` 聚合 |
| C67 | 🔴 设计修正 | **host 回调必须回传结果，不能吞掉** | 最初写成 `void apply_event(node)`（dispatcher 一律 ACK）。核实 `scene_presenter_publish_message` → `app_ui_show_text` 会**直接接管屏幕**、不排队：若 `soft` 事件在用户读回复时到达，一律 ACK 就只有两种结果——盖掉回复，或静默丢失。改为 `apply_event(node, &handled, &permanently_invalid)`（沿用 `handle_pet_profile` 的既有模式）：`handled=false` 让消息**留待下次轮询**（这正是 §4.2 `QUEUE_SPEAK` 的字面含义），`permanently_invalid=true` 则按失败 ACK，避免一条渲染不了的坏事件**永久钉住**共享页游标 |
| C68 | 🟢 设计决定 | **幂等环只在真正呈现前记账** | §4.1 要求「相同 `dedupeKey` 不重复呈现」。但记账点若放在校验之后、延后判定之前，**延后的事件会在重试时被自己刚记的 key 抑制**——事件永久丢失。故记账点移到「确定要呈现」之后；延后路径**不记账** |
| C69 | 🟢 设计决定 | **幂等环不持久化** | 会话内、有界（8 条）、非持久。`persist=true` 的事件**本就该在重启后重现**，一个能跨重启的环会抑制契约要求的补发。跨重启的簿记属于 N1-6 的快照生命周期（与「`event-ack` 后清快照」同属一处） |
| C70 | 🟢 设计决定 | **无音频时降级为显示，绝不静默丢弃** | 设备无本地 TTS，`SPEAK`/`INTERRUPT_*` 目前只打日志。但 `event_action_should_display` 对这些动作**也返回 true**（N1-2 决策核心早已如此设计），所以事件仍会显示在屏上。**「还不会说话」不能变成「什么都没显示」**——否则 N1-6 的高危审批会在设备上凭空消失 |
| C71 | 🟢 纵深防御 | **设备侧独立校验，不只依赖 Hub** | `event_ingest.h` 重做 Hub 的同套规则（category/severity 闭集、审批审计契约三件套、ttl 边界、动作字段）。理由：D4 的**非内网直连**路径前面没有 Hub 校验器，且被攻陷/有 bug 的生产端不该靠跳过 Hub 就把卡片送进屏幕。check 脚本加**漂移守卫**：把设备侧重复的 wire 闭集字符串与 `corelib/im/device_event.go` 逐值比对 |

**N1-2 设备侧接线的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 能力声明 | `gateway_capability_projection.h` · `GATEWAY_CAPABILITY_EVENT_PUSH` | 与 `corelib/agent` 的 `Features.eventPush` 同名同位；漏加 `KNOWN_MASK` 会被 `gateway_capability_projection.c` 的越界校验拒掉 |
| 能力上报/接受 | `gateway_transport.c` · `local_gateway_capabilities()` + 两个 `feature_flags[]` | 声明与解析**两处都要**（见 C65） |
| 消息分类 | `gateway_dispatcher.c` · `event_message` | 能力门 `gateway_message_capability_allowed(EVENT_PUSH, type)`；未声明即 `permanently_invalid`（不重试） |
| 呈现入口 | `gateway_dispatcher.h` · `apply_event` | 出参 `handled`/`permanently_invalid`（见 C67） |
| 校验与决策 | `event_ingest.h` · `event_ingest_decide` | 纯值、无 ESP-IDF/cJSON；非法事件**不进矩阵**（`DROP` 是决策，不是「不可信」） |
| 忙碌时延后 | `event_ingest.h` · `event_ingest_defers_while_busy` | 忙碌时只有抢占类动作与 `DROP` 放行；其余延后（C67） |
| 会话幂等 | `event_ingest.h` · `event_ingest_dedupe_seen` | 有界 8 条、非持久（C68/C69） |
| 实际呈现 | `main.c` · `gateway_host_apply_event` | cJSON 提取 → 校验 → 决策 → `scene_presenter_publish_message`；`SPEAK` 类降级为显示（C70） |

**验证状态**：新增 `tools/check-event-ingest.ps1`（`EXIT=0`）——头文件纯度（无 ESP-IDF/cJSON/`esp_err_t`）、12 个必需纯函数、category 闭集、审批审计契约、**wire 漂移守卫**、主机单测 **16 组全过**。四个改动/新增编译单元用**真实 `xtensa-esp32s3-elf-gcc` + 真实工程头文件路径 `-fsyntax-only`** 全过（`main.c` / `gateway_dispatcher.c` / `gateway_transport.c` / `gateway_capability_projection.c`）。既有 `check-gateway-capability-projection.ps1`、`check-gateway-transport-asset-cancellation.ps1`、`check-gateway-tool-result-outbox.ps1`、`check-event-presentation-policy.ps1` 全部仍 `EXIT=0`。三个新文件非 ASCII 字节数 **0**。**整机 `idf.py build` 未跑**（须带 `MACLAW_PROFILE`，见 §9 v6 的组件求解陷阱）。

### v11 → v12

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C58 | 🔴 计划偏差修正 | **N1-5 的审批源不该是 `handleVEApprovalRequest`，而是本地工具审批路径** | 计划原写"审批请求（`handleVEApprovalRequest`）"。核实该路径的 payload：`{envelope, is_fallback?, fallback_reason?}` —— **不带任何 risk 等级**。D5-A 要求"仅高危"，而这条路径无法判定高危；若把工作流审批一律标成 `risk=high`，**每一条**审批都会变成设备卡片，直接触发 W4（事件变骚扰）。正确的源是 `emitRegisteredToolApprovalAgentViewIfNeeded` / `emitDatabaseMutationApproval` / `emitArchiveExternalApprovalIfNeeded` 三条**本地工具审批**路径——那里有真实的 `security.RiskLevel`（`RiskHigh`/`RiskCritical`）。**这不是接线差异，是"能不能实现 D5-A"的差异** |
| C59 | ✅ 任务完成 | **高危审批事件生产端落地** | 新增 `guiapp/device_approval_event.go` + 3 个 hook（`agent_view_tool.go` 的三条审批发射路径，仅在 `emitAgentView` 真的打开了面板后推送）。D5-A 是**正向判定**：只认 `RiskHigh`/`RiskCritical`，缺失等级不推送也不降级为"够安全" |
| C60 | ✅ 任务完成 | **VE 工作流注意力事件生产端落地** | 新增 `guiapp/device_ve_event.go` + hook（`handleVEWorkflowStatus` 的注意力分支）。severity **由 Hub 自己的 urgency 分类决定**（`critical`→`interrupt` / `overdue`→`soft` / 其余→`silent`），不在 GUI 侧重算一遍 |
| C61 | ⚠️ 依赖缺口 | **`task_done` 没有现成信号，是设计决定不是接线** | 两处核实：① 本地后台任务是**轮询式**的（`bash(background=true)` 提交 + `async_wait` 的 check/wait 拉取），完成时不推送；② `ve:workflow_status` 的 `classifyWorkflowStatusEvent` **闭集里只有 blocked/escalation**，没有完成态。所以"任务完成"要么给 `LocalBackgroundTaskManager` 加完成通知，要么由 agent loop 在长任务收尾时发。**N1-5 因此停在 2/3**，第三源需要先做这个决定 |
| C62 | 🟢 设计约束 | **设备推送是加法，绝不回灌失败** | 两条推送都是 fire-and-forget：Hub 未连接即静默返回，发送失败只 log。**桌面 AgentView 仍是权威审批面**——让"设备不可达"变成工具调用失败，等于给用户的工作引入一个新的单点故障，而设备本该是"多一个选择"而不是"多一个前提" |
| C63 | 🟢 设计修正 | **审批卡片的 `dedupeKey` 用审批 ID，不是参数的哈希** | 每次工具调用都会 `storeRegisteredToolPendingApprovalForPrincipal` **铸一个新 ID**（`tool-approval-N`）。若用参数哈希做 key，"用户拒绝过一次"会**静默压制**之后一条参数相同但独立创建的新请求——而"拒绝这一次"不等于"授权以后都不用问"。ID 保证的是"同一个待决审批不重复呈现"，这才是幂等该管的范围 |
| C64 | 🟢 设计修正 | **VE 事件的 `dedupeKey` 按 (instance, event) 稳定** | 与审批相反：被阻塞的工作流**每次重新评估都会再通知一次**，用户要的是"知道一次"，不是"每次评估都知道一次"。所以这里必须用稳定的业务键，而不是每次推送都变的 `eventId` |

**N1-5 两条生产链路的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 审批：判定 | `shouldPushApprovalToDevice(level)` | 正向判定 `high`/`critical`；缺失等级既不推也不当"低危" |
| 审批：审计契约 | `buildToolApprovalDeviceEvent` | `ttlSec=300` + `requiresAck=true` + `persist=true`（Hub 在 wire 层强制，缺一即拒） |
| 审批：具体对象 | `toolApprovalDeviceTarget` | `bash`→命令、`database`→`sql`/`action`、文件类→路径；取不到则回退 `Risk.Reason`；参数先过桌面同一套 `redactAgentViewSubmitSecrets` |
| 审批：动作 | 同上 | `approve`（`primary` + `risk=high`）/ `reject`（`secondary`，**不带 risk**——拒绝永远是安全的） |
| 审批：hook | `agent_view_tool.go` × 3 | 只在 `emitAgentView` 返回 true 后推送（面板没打开就不该有设备卡片） |
| VE：severity | `workflowStatusSeverity(urgency)` | `critical`→`interrupt` / `overdue`→`soft` / `attention`→`silent`（默认桶最不具体，不该买到"声音"） |
| VE：hook | `remote_hub_ve_events.go` · `handleVEWorkflowStatus` | 只在已有的 blocked/escalation 注意力分支内推送 |

**验证状态**：`guiapp` 新增单测 **19 例**（`device_approval_event_test.go` 11 例 + `device_ve_event_test.go` 8 例）全过，覆盖：D5-A 正向判定（`low`/`medium`/`none`/空 全部不推）、审计契约三件套、动作的 primary/secondary 与 risk 分布、具体对象提取（4 个子用例）、无对象时回退 `Risk.Reason`、**密钥值绝不进摘要**、无 ID 拒绝、rune 安全截断、`dedupeKey` 随审批 ID 而变但同一审批稳定、urgency→severity 映射（含大小写与空白）、VE 的 `dedupeKey` 随 (instance, event) 稳定、VE 无 actions、不可寻址的推送被拒、无 Hub 时两个 push 都安全 no-op。`go build ./guiapp/` 通过；改动文件 `gofmt` 干净；`go vet` 未点名任何改动文件。

**回归定性**：宽过滤 `-run 'Approval|AgentView|WorkflowStatus|VEApproval|RegisteredTool'` 报 2 个 FAIL（`TestInstallSharedPublishedApprovalFixture*`），失败原因均为 `testing.go:1464: TempDir RemoveAll cleanup: ...: The directory is not empty`（**无断言失败**）。对照实验（`git stash` 我的 6 个文件 + **相同宽过滤**）失败集合与原因**逐字一致** → **与 N1-5 无关**，是 Windows 上 `t.TempDir()` 清理与残留进程句柄的竞态（这 2 个用例单跑即过）。恢复后工作区指纹逐字节一致。

### v10 → v11

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C53 | 🔴 依赖发现 | **N1-2 的"语音播报"依赖 N3-5，设备没有本地 TTS** | 核实 `iot-agentos/`：无任何 TTS 引擎（`tts` 只出现在延迟打点里程碑与 reply 服务里），音频一律来自服务端（`server_audio_*`）。所以"按 severity 播报"在 N3-5（流式 TTS 边下边播）落地前**无法端到端完成**。处理：N1-2 的决策层把动作**抽象**成 `SPEAK`/`QUEUE_SPEAK`/`INTERRUPT_SPEAK`/`INTERRUPT_LISTEN`，而不是写死"现在没有音频通路"，这样 N3-5 落地后决策层不需要改。**N1-2 因此拆成"决策核心（已完成）"+"接线与播报（待 N3-5）"** |
| C54 | ✅ 任务完成 | **§4.2 准入矩阵落为纯值模块 + 主机单测** | 新增 `main/services/event_presentation_policy.{h,c}` + `tools/host_tests/test_event_presentation_policy.c` + `tools/check-event-presentation-policy.ps1`。矩阵写成 `static const [3][5]` 表，可对着 §4.2 逐行读；单测**把矩阵按 §4.2 手工复述一遍**再比对，避免"测试和实现用同一套错逻辑"而双双通过 |
| C55 | 🟢 设计修正 | **设备状态取自 `foreground_coordinator_current()`，不从像素/音频电平推断** | 该枚举已有 `COMMAND_VOICE`（收音）/`COMMAND_RESULT`（播报）/`MEETING`（会议）三档，正是 §4.2 的三列。勿扰取 `sleep_schedule` 的 `active_window` **且 `!override_active`**：用户刚手动唤醒说明人醒着，把他当睡着会吞掉事件 |
| C56 | 🟢 测试抓到的缺陷 | **`interrupt-listen` 也是"可听"的** | 单测的"抢占必可听"不变式失败：`event_action_is_audible` 漏了 `INTERRUPT_LISTEN`。它的语义是"停止收音**后播报**"，不是静默停止。这正是"把不变式写成断言"的价值——否则调用方会按 `is_audible` 决定要不要取音频，于是这条最需要用户听到的动作反而不播 |
| C57 | 🟢 设计决定 | **响铃中的闹钟记为 `standby`，不是 `speaking`** | §4.2 没有这一列。记为 `speaking` 会让 `interrupt` 事件**切断正在响的闹钟**——切断一个"职责就是把人叫醒"的东西，比与它音频重叠更糟（音频仲裁本来就会串行化）。故保守记为 `standby`，并在代码里标明这是待定的产品问题，而不是悄悄决定 |

**N1-2 决策核心的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 闭集 severity | `event_presentation_policy.h` · `event_severity_parse` | `silent`/`soft`/`interrupt`，未知即拒（含 `notice` —— Hub prompt 曾用错的档位名） |
| 状态归属 | `event_device_state_dominant` | 优先级 `会议 > 勿扰 > 收音 > 播报 > 待机`，每条都有理由（会议最严：震动会录进去；收音高于播报：收音握着用户的话，切掉不可重来） |
| 准入矩阵 | `event_presentation_decide` | §4.2 逐格转写；越界输入**降级为屏显**（不是 `interrupt`）——未知状态绝不能成为设备开口的理由 |
| 播报文案 | `event_presentation_speech_text` | `summary` → 回退 `title`（§4.1）；只选一个字段，因为 Hub 已把 `summary` 写成播报句，两个都念会说两遍 |
| 设备状态快照 | `event_presentation_policy.c` · `event_presentation_current_flags` | 唯一允许触碰设备服务的地方；header 保持零 ESP-IDF 依赖（同 `latency_trace` 的切分） |

**验证状态**：主机单测 **10 组**全过（`test_event_presentation_policy.c`：完整 3x5 矩阵、两条硬规则、五档状态优先级、闭集解析、播报文案回退、动作谓词划分、越界降级、`silent` 永不可听）；`gcc -std=c11 -Wall -Wextra -Werror` 编译零警告；`tools/check-event-presentation-policy.ps1` **exit=0**（含 header 无 ESP-IDF 依赖、12 个纯 helper 齐备、7 个动作枚举齐备、5 个状态枚举齐备、胶水 5 处接线断言、CMake 登记、主机单测 PASS）；**用真实 `xtensa-esp32s3-elf-gcc` + 真实工程 include 路径对 `event_presentation_policy.c` 做 `-fsyntax-only`：exit=0，零错误**。四个新/改文件非 ASCII 字节数为 **0**（与既有 30 个 check 脚本一致）。

**一个环境发现（已影响本轮验证方式）**：本沙箱里 `& script.ps1` 与 `. script.ps1` **静默不执行**——不产出任何 stdout、`$LASTEXITCODE` 为空。早先"退出码 0"其实是调用方的兜底默认值，不是脚本结果。可用的形式是**子进程**：`& powershell.exe -NoProfile -ExecutionPolicy Bypass -File <script>`，它能正常返回输出与退出码。已据此复验本轮脚本（详见 `.workbuddy-ai/memory/2026-10-04.md` 与 `sandbox-false-failure-triage` 技能）。

### v9 → v10

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C47 | 🔴 结构性发现 | **契约常量从 `hub/internal/im` 迁到 `corelib/im`** | N1-1 把 `DeviceEventCategory*`/`DeviceEventSeverity*` 等常量定义在 `hub/internal/im`。Go 的 `internal` 包规则让**只有 `hub/` 子树**能 import 它——GUI（`guiapp/`）作为事件生产端根本拿不到这些常量，只能复制字面量。复制就会漂移：改一处 `severity` 档位名，另一边静默写出非法值。故把**导出常量 + wire 形态注释**下沉到 `corelib/im/device_event.go`（`corelib` 无 internal 限制），**校验逻辑仍留 Hub**（Hub 是 wire 边界的守卫，GUI 只做生产） |
| C48 | 🟢 渠道归一 | **`DeliveryChannelDevice = "device"`，并接受 CJK 与拉丁别名** | CJK：`设备`/`码卡龙`/`小卡`/`硬件设备`/`终端`；拉丁：`device`/`devicelocal`/`hardware`/`hardwarelocal`/`esp32`/`companion`/`maclawdevice`。理由是渠道名来自 LLM 填参，用户会说"推到码卡龙"——只认英文 `device` 会让这类请求**静默落到默认渠道 lansenger**（比报错更糟） |
| C49 | 🟢 设计修正 | **`SplitScheduledTaskBody` 把任务名从正文里拆出来** | `FormatBody` 产出的是给人看的整段文本（首行 `【定时任务】<名>` + 状态行）。设备要的是**结构化** `title` + `summary`。新函数按行找**第一个** `【定时任务】` 前缀行作为 `title`，其余 trim 后作 `summary`；无标记则 `title=""` 且整段作 `summary`。失败状态行（`❌`/`失败`）因此保留在 `summary` 里，不会被当成任务名 |
| C50 | ⚠️ 范围边界 | **MaClawSrv 侧没有设备事件推送通道，故 `device` 渠道只在 GUI 落地** | 核实：`MaClawSrv/` 有 `device_pairing.go`、`device_update_catalog.go`（配对 + OTA 目录）与设备直连的 `thirdparty_gateway.go`（`handshake`/`incoming`/`outgoing`/`ack`），但**不 import `hub/internal/im`、无 Hub 客户端、无 `SendDeviceGatewayEvent` / `UpdateMachineEvent` / `device_gateway_reply`**。所以 srv 模式下"日程 → 设备"目前不通。不在此轮补：srv 模式的设备通道要与 N2-1（两种接入模式）一起设计，否则会造出第二套扇出 |
| C51 | 🟢 设计约束 | **`device` 渠道只认 `user_id=self`，其余一律报错** | 与 IM 渠道不同，`device` **没有 per-recipient 的平台 id**——目标是"本机已配对的全部终端"，唯一可寻址的收件人就是机主。`Kind != user` 或 `UserID != self` 时**返回错误**（而非静默 no-op），因为静默 no-op 会让"给某人推提醒"看起来成功了却什么都没发生 |
| C52 | ⚠️ 静默陷阱 | **`reviewedHostScheduleDispatchFireChannel` 是白名单，未命中会静默 no-op** | 该函数在 `corelib/agentservice/schedule_dispatch_fire.go`：未列出的渠道**不报错**，只是让整次 fire 什么都不做。所以新增渠道必须显式加分支。**`maclaw` 故意不重映射为 `device`**——它是桌面自己的网关模式、已解析为 `lansenger`，把它挪到 `device` 会静默改掉既有绑定的路由 |

**N1-4 `device` 投递渠道的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 渠道常量 | `corelib/scheduler/delivery.go` · `DeliveryChannelDevice = "device"` | 与 IM 渠道并列，`TaskDelivery.Channel` 直接可用 |
| 归一 | `corelib/scheduler/im_message.go` · `CanonicalDeliveryChannel` | CJK/拉丁别名 → `device`；**不得吞掉默认的 `lansenger`** |
| 文案拆分 | `corelib/scheduler/delivery.go` · `SplitScheduledTaskBody` | 首个 `【定时任务】` 行 → `title`，其余 → `summary` |
| fire 白名单 | `corelib/agentservice/schedule_dispatch_fire.go` · `reviewedHostScheduleDispatchFireChannel` | 必须显式加 `device` 分支，否则整次 fire 静默 no-op |
| 投递 | `guiapp/device_event_delivery.go` · `deliverDeviceScheduledTarget` | 仅 `kind=user` + `user_id=self`；Hub 未连接即报错 |
| 事件构造 | 同文件 · `buildScheduledTaskDeviceEvent` | `category=schedule`、`severity=soft`、`ttlSec=900`、`persist=true`、**无 `actions`**、**不设 `dedupeKey`** |
| 分发接线 | `guiapp/scheduled_task_delivery.go` · `deliverScheduledTaskTarget` | 加 `case DeliveryChannelDevice` |
| 目标目录 | `guiapp/schedule_target_catalog.go` · `listDeviceDeliveryTargets` | 唯一目标 `{kind:user, id:self}`，供 `list_targets` 展示 |
| 工具描述 | `tool_registry_builtin.go` / `im_tool_definitions.go` / `corelib/agent/tool_register_core.go` / `corelib/tool/enrichment.go` | 渠道列表补 `device`；关键词数组加 `device`/`hardware`/`码卡龙` |

**三个刻意取舍**（都有代码注释）：① **`severity=soft`**——提醒该按 §4.2 准入矩阵排队（待机播报 / 播报中排队 / 勿扰静默），而不是打断用户正在说的话；② **失败任务不升级为 `interrupt`**——调度失败是大脑的观测，不该变成对用户的打扰；③ **不带 `actions`**——提醒没有决策点，带上动作会把一条非决策事件推进 D5-A 的审计路径（那是 N1-6 的地盘，D5-A 只覆盖高危审批）。另外**故意不设 `dedupeKey`**：每次 fire 都是独立的一次提醒，Hub 回填的 `eventId` 正是对的幂等键。

**验证状态**：`corelib/scheduler` 新增单测 **6 例**（`device_channel_test.go`：10 种拼写归一 + 不吞默认渠道、`SplitScheduledTaskBody` 与 `FormatBody` 往返、失败状态行保留、无标记文本、取首个标记、`TaskDelivery` 校验通过）全过，整包 `ok 3.616s`；`guiapp` 新增单测 **6 例**（`device_event_delivery_test.go`：任务名拆出 summary、无任务行时用中性标题、每次事件 ID 唯一、拒绝非机主目标、上报传输失败、目标目录只暴露机主）全过；`go test ./corelib/agentservice/ -run 'ScheduleDispatch|ReviewedHost'` **ok 119s**；`go build` 覆盖 8 个包通过；改动文件 `gofmt` 干净。
**一次对照实验**（§9 铁律）：`go test ./guiapp/ -run 'Schedule|Deliver|Catalog|Target'` 出现 5 个 FAIL。用 `git stash push -u -- <我的 21 个文件>` 移除本轮全部改动后**复跑，5 个用例以完全相同的报错失败**（`coding_knowledge_search` 缺 provision、symlink 权限、mock LLM 时序），证明**与 N1-3/N1-4 无关**——是工作区里既有的他人 WIP + 沙箱环境限制。恢复后改动指纹 md5 与实验前逐字节一致。

### v8 → v9

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C38 | ✅ 任务完成 | **N1-3 `UpdateMachineEvent` 扇出落地** | `DeviceGateway.eventsByMachine map[string]storedDeviceEvent` + `UpdateMachineEvent`（照抄 `UpdateMachineAmbient`，逐设备判能力）+ `latestDeviceEventForMachineLocked` + handshake `response["event"]` 回放；`ws/handlers_machine.go` 加 `payload.Reply["event"]` 分支（`clientId="*"` 机器级扇出）；`guiapp.SendDeviceGatewayEvent` 提供 GUI 侧通道 |
| C39 | 🟢 闭合 C34 | **绝对过期时刻随事件持久化，补发重算 `ttlSec`** | 快照条目 = `{event, expiresAtUnixMs}`；`expiresAtUnixMs` 是 Hub 内部字段，**不进 wire**（`event` 里出现的字段就是契约，多一个是契约变更）。补发的 `ttlSec = ceil(剩余/1000)`，只减不增，并被 `deviceEventMaxTTLSec` 夹住 |
| C40 | 🟢 闭合 C35 | **扇出逐设备判能力，而非机器级一刀切** | `UpdateMachineEvent` 对每台设备调 `DeviceEventPushSupported(state.capabilities)`，不合格的**跳过**（不是降级为文本：那会丢掉用户要按的按钮）。快照仍照写，所以"当时没有能力设备在线"不会导致事件消失——这正是离线补发的价值 |
| C41 | 🟢 设计修正 | **快照里只有 `ttlSec` 而丢了绝对时刻 → 丢弃，不猜** | 猜就是刷新窗口。判据：`ExpiresAtUnixMs <= 0` 且事件声明了 `ttlSec` ⇒ 返回 false。无窗口概念的事件（如 `silent` 通知）不受影响，原样补发 |
| C42 | 🟢 设计修正 | **`persist=true` 但未声明 `ttlSec` 的事件，快照窗口封顶 `deviceEventPersistTTLSec`(3600s)** | 只有审批被强制声明 `ttlSec`。若 `persist` 事件完全没有期限，"重启后补发"就会变成"几天后还在补发一张旧卡片"。封顶只作用于**快照**，不写入 wire（不发明设备没声明的 `ttlSec`） |
| C43 | ⚠️ 承重差异 | **归一化后的快照必须写回 `saved` 再 marshal，与 ambient 范式不同** | `restorePersistedCredentials` 里 ambient 只把归一化结果放进局部变量 `ambientByMachine`，**没有写回 `saved.AmbientByMachine`**，于是持久化的仍是原始 payload。对 ambient 只是白算一次 `expiresAt`；对 event 却会丢掉 `expiresAtUnixMs`，让 C34 在**每次重启后复发**。N1-3 因此显式写回 `saved.EventsByMachine`，并在代码注释里标明这一处差异是承重的 |
| C44 | 🟢 设计修正 | **恢复时单条坏事件只丢弃、不整体失败；恢复出坏事件仍报错** | 与 ambient 的"失败即中止恢复"相反。理由是后果不对称：快照的其余部分是**配对状态**，为一条显示缓存而中止恢复会让"每台设备都要重新配对"（一次客服事故），而丢一条待补发卡片是隐形的。重新 marshal 时自愈（`TestMachineEventSnapshotDropsUnreadableEntryWithoutFailingRestore` 锁住） |
| C45 | 🟢 健壮性 | **`normalizeDeviceEventActions` 接受 `[]map[string]any`** | 同一份 payload 有三种形状：JSON 解码得 `[]any`、内存中已归一的事件是 `[]map[string]any`、手写 payload 可能是单个对象。少了第一种之外的容忍，`cloneStoredDeviceEvent` 重新校验内存快照时会**静默失败**，于是持久化路径把有效事件全部丢掉 |
| C46 | 🟢 文案修正 | **能力 prompt 里的 `notice` 改为 `soft`** | v8 遗留：`BuildClientCapabilityPrompt` 写的是 `(silent, notice, interrupt)`，而契约是 `silent/soft/interrupt`。这行是大脑唯一的事件指引，报错档位名会让它写出非法 `severity` 而被 wire 层拒收（`client_capabilities_test.go` 同步改断言） |

**N1-3 扇出与补发的落点**

| 环节 | 实现 | 关键约束 |
|---|---|---|
| 入口（GUI→Hub） | `HubEnvelope{Type:"im.device_gateway_reply"}`，`payload.clientId="*"`，`reply.event` | `clientId="*"` 是既有的机器级扇出约定（ambient 同款） |
| 扇出（Hub） | `ws/handlers_machine.go` 识别 `reply["event"]` → `UpdateMachineEvent(ctx.MachineID, event)` → `return false, nil` | 必须在 `clientId="*"` 时提前返回，否则同一条事件还会走单设备下发路径 |
| 快照 | `eventsByMachine[machineID] = {event, expiresAtUnixMs}`，仅 `persist=true` | 无能力设备在线也照写（离线补发的价值所在） |
| 补发 | handshake `response["event"]`，由 `replayDeviceEventPush` 重算 `ttlSec` | 用**本次握手声明的**能力判定；过期即不补发 |
| 单设备路径 | `adaptDeviceGatewayReply` 的 `case "event"`（N1-1） | 两条路径共用 `DeviceEventPushSupported` 与同一校验器 |

**验证状态**：`hub/internal/im` 新增 N1-3 单测 **16 例**（含 JSON 往返、Hub 重启往返、坏快照容错、远未来过期夹取、窗口边界丢弃），事件相关用例合计 **36 例全过**；整包 `go test ./hub/internal/im/` 通过（29.5s）；`go vet ./hub/internal/im/ ./hub/internal/ws/` 干净；`go build ./hub/... ./guiapp ./corelib/agent` 通过；改动文件 `gofmt` 干净。
**一处既有测试的整理**：把 `TestDeviceGatewayEventPushRequiresDeclaredCapability` 里内联的"只有文本"能力对象提为 `textOnlyCapabilities` 辅助，供 N1-3 的跳过用例复用（行为未变）。

### v7 → v8

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C30 | ✅ 任务完成 | **N1-1 `event` reply 契约落地** | 新增 `hub/internal/im/device_event_push.go`（闭集常量 + 校验归一 + `DeviceEventPushSupported` 能力判定）；`adaptDeviceGatewayReply` 加 `case "event"`；`corelib/agent` 加 `Features.eventPush` 并在能力 prompt 里声明；新增 `deviceReplyBool` 辅助 |
| C31 | 🟢 契约对齐 | **以 §4.1 为准重写初版实现** | 初版自行发明了 `notice`/`task`/`style` 且漏了 `dedupeKey`/`requiresAck`/`persist`/`risk`。§4.1 明写"原设计只给了字段列表，不足以落地"，它就是契约本身 → 改为 `soft`/`task_done`/`kind`+`risk`，补齐四个缺失字段 |
| C32 | 🟢 设计修正 | **`kind` 与 `risk` 分开**：前者是呈现强调，后者是动作后果 | D5-A 用 `risk=high` 判定高危审批。若设备从按钮配色反推语义，改配色就会改安全行为。未声明 `risk` ≠ `risk=low`：保持字段缺失，让 D5-A 是正向判定 |
| C33 | 🟢 设计修正 | **审批的审计契约在 wire 层强制**：`category=approval` ⇒ `ttlSec>0` + `requiresAck=true` + `persist=true` | 三者任一缺失的失败都是**静默的**：没有 `requiresAck` 则 Hub 不知卡片到没到；没有 `persist` 则重启后待决高危审批无声消失；没有 `ttlSec` 则永远挂着。D5-A 要求可审计，所以放在 wire 边界强制，不指望每个生产端记得写 |
| C34 | ⚠️ 留给 N1-3 | **`ttlSec` 是相对时间，补发必须重算** | `persist=true` 的事件在设备重启后补发时，若原样重发 `ttlSec` 会**刷新**有效期，让 3 小时前的高危审批重获 5 分钟窗口 → 违反"超时走'不做'而非放行"。N1-3 需持久化**绝对**过期时刻，补发时改发剩余秒数，≤0 丢弃 |
| C35 | ⚠️ 留给 N1-3 | **扇出必须显式判能力** | `UpdateMachineAmbient`（N1-3 要照抄的范式）**直接构造消息、绕过 `adaptDeviceGatewayReply`**，因此没有任何能力检查——只会画文字的设备也会收到 `ambient`。故把 `DeviceEventPushSupported()` 单独导出，让扇出与 reply 两条路径共用同一判定 |
| C36 | 🟢 设计修正 | **`eventId`/`dedupeKey`/`actionId` 超长一律拒绝，不做截断** | 截断 eventId 会静默破坏 `event-ack` 关联；截断 dedupeKey 会把两个不同的逻辑请求合并成一次呈现。只有自由文本（`title`/`summary`/动作 `label`）才截断 |
| C37 | 🟢 能力 prompt | **`BuildClientCapabilityPrompt` 在声明 `eventPush` 时输出一行事件指引** | 大脑不知道设备能收事件，就永远不会产生事件——N1-5 的三个事件源会全部哑火。未声明时不输出（有单测锁住） |

**N1-1 契约要点**

| 项 | 取值 |
|---|---|
| `category` | `approval` / `task_done` / `schedule` / `ve` / `system`（闭集，未知即拒） |
| `severity` | `silent` / `soft` / `interrupt`（闭集，对应 §4.2 准入矩阵三档） |
| `actions[].kind` | `primary` / `secondary`（缺省 `secondary`） |
| `actions[].risk` | `low` / `medium` / `high`（可选；缺失≠`low`） |
| 上限 | `eventId`≤64B、`dedupeKey`≤128B、动作 ≤4 个、`actionId`≤32B、`label`≤24 字、`title`≤120 字、`summary`≤400 字、`ttlSec` 1..3600 |
| 文本截断 | 有效上限 = **min(协议上限, 设备声明的 `text.maxChars`)**，按 rune 边界切（CJK 不会被切成非法 UTF-8） |

**验证状态**：`hub/internal/im` 新增单测 **47 个断言全过**（含子测试）；`corelib/agent` 能力 prompt 单测通过；`go build ./hub/internal/im/ ./corelib/agent/` 通过；三个新/改文件 `gofmt` 干净。
**两个包的既有失败与本次无关**（已用 `git stash` 证明在原始工作区同样失败）：`corelib/agent` 的 `TestToolGlobSkipsSymlinksByDefault`、`guiapp` 的 `TestCapabilityGapDetectorPackageLocalFilesSkipsSymlink` / `TestCodingSurfaceCapabilityCoverageProbe` —— 三者都依赖 `os.Symlink`，而本环境无创建符号链接的权限。


### v6 → v7

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C25 | ✅ 任务完成 | **N0-3 链路健康度已落地**，三层指标齐备 | Hub 侧 `im/link_health.go`（tracker，进程生命周期累计、只读、不重置）+ `httpapi/link_health_handler.go` + `router.go` 注册 `GET /api/admin/link-health/metrics`（`requireGlobalAdmin`）；GUI 侧 `web/admin/link-health-tab.js` + index.html nav/panel/script + `professional.css` 布局 |
| C26 | 🟢 设计修正 | **fleet 在线率 = 各租户比值的均值，不是 `总在线秒数 / 观测窗口`** | 后者在多租户下会系统性高报，且被 0-1 截断后**恰好掩盖**"某个租户的大脑从来没连上"这一最需要被看见的情况。单测 `TestLinkHealthFleetRatioIsMeanNotSum` 锁死（2 租户全在线 → 100s / ratio 1.0） |
| C27 | 🟢 设计修正 | **`ObserveDeviceEvent` 遇到未知 outcome 不创建租户条目** | 原实现先 `tenantLocked()` 再 switch，未知 outcome 会凭空造出一个租户条目并污染 fleet 分母。改为在 switch 内按 outcome 才创建（`TestLinkHealthUnknownOutcomeIsIgnored`） |
| C28 | 🟢 设计修正 | **takeover 不重置开启中的在线期** | 同一 user 换 machine 会 `setOwnerForTenantLocked` 再 `clearOwnerForTenantLocked`，若把 release 当成"下线"会凭空造出一段离线缺口，把在线率打下去（`TestLinkHealthTakeoverDoesNotFabricateOfflineGap`） |
| C29 | 🟢 归属 | **`linkhealth` 标签是 global-only** | 面板数字描述本 Hub 服务的**全部**租户，接口由 `requireGlobalAdmin` 守卫，因此 `link-health-tab.js` 把 `linkhealth: true` 注册进 `window.adminGlobalOnlyTabs`，tenant admin 不会看到入口 |

**N0-3 三层指标与接线点**

| 指标 | 语义 | 接线点 |
|---|---|---|
| GUI 在线期开启 | 大脑开始可达 | `im/remote_gateway_plugin.go` · `setOwnerForTenantLocked()` → `ObserveGuiClaim(tenantID, owner.ClaimedAt)` |
| GUI 在线期结束 | 大脑不再可达 | `im/remote_gateway_plugin.go` · `clearOwnerForTenantLocked()` → `ObserveGuiRelease(tenantID, now)` |
| 事件被拒 `unavailable` | 网关未装配（Hub 侧故障） | `im/device_gateway.go` · `g.plugin == nil` 两处 |
| 事件被拒 `gui_offline` | **F3：大脑不在**（503） | `im/device_gateway.go` · `ownerID == ""` 两处；`writeDeviceError` 第 4 参数改用常量 `deviceErrorCodeGuiOffline` |
| 事件重放 | 设备没收到响应而重试 | `im/device_gateway.go` · `markDeviceEvent` 命中（duplicate）两处 |
| 事件接收 | 成功投递给活的大脑 | `im/device_gateway.go` · `handleIncoming` 的 `go g.forwardIncomingDeviceMessage(...)` 后、tool-result 的 `go g.plugin.HandleGatewayMessage(...)` 后 |

**指标口径**：`gui_online_ratio`（per-tenant + fleet 均值）、`gui_offline_rejections`（头条指标，F3 量化）、`device_event_success_ratio` = (accepted + duplicate) / (accepted + duplicate + rejected)。**重放计入投递成功**——设备只在我们没回响应时重试，因此它证明"事件最终被送达"，与"从未送达"必须区分开。快照同时给出 `device_event_rejected_by_reason` 供面板画拒绝原因 chips。

**验证状态**：`go test ./hub/internal/im/ -run LinkHealth` **14/14 PASS**；`go build ./hub/internal/im/` 与 `./hub/internal/httpapi/` 均通过；三个新 Go 文件 `gofmt` 干净；`node hub/web/admin/validate-admin-modules.js` 除最后一项 `admin-ui-dialog.test.js`（`spawnSync` 在本环境一律 `EBUSY`，**原始未改动工作区同样失败**，属环境限制）外**全部通过**；`link-health-tab.js` / `professional.css` / `admin-module-health.js` 非 ASCII 字节数均为 0。N0-2 的 `check-latency-trace.ps1` 复跑仍 PASS。


### v5 → v6

| # | 类型 | 内容 | 处理 |
|---|---|---|---|
| C21 | ✅ 任务完成 | **N0-2 延迟打点框架已落地并接线** | 新增 `main/services/latency_trace.{h,c}`、`tools/host_tests/test_latency_trace.c`、`tools/check-latency-trace.ps1`；`main/CMakeLists.txt` 登记新源文件（共享 SRCS，全板卡生效） |
| C22 | 🟢 设计修正 | **打点从"有 origin 即活跃"改为显式开合**：`begin()` 开、`flush()` 合，`mark()` 在闭合期为空操作 | 否则欢迎语/GUI 试听音/上次开机遗留的回复会给一条无关的 `latency turn` 造出 origin。`latency_trace_active()` 语义随之改为"turn 是否打开"，`current()` 闭合时返回 NULL |
| C23 | 🟢 设计修正 | **`response_ms` 回退链补齐为 `audio → decode → tts`** | 设备自己解码，MP3 有独立 decode 步；WAV 路径没有，只有首个音频字节。补上 TTS 回退后 WAV 回复也能给出数值（此前会返回 −1） |
| C24 | 🟢 打点归属 | **flush 点收敛为三处**：音频播完（主路径，含 `audio` 帧）、终端文本且 `speech_parts_pending == 0`、`speech_end` | 交互 worker 退出**不 flush**——TTS 回复里 worker 先于音频退出，在此 flush 会把 `tts/decode/audio` 全砍掉 |

**N0-2 里程碑与接线点（8 个 mark）**

| mark | 语义 | 接线点 |
|---|---|---|
| `release` | 用户说完 / 停止采集 | `services/interaction_service.c` · `command_service_timing_capture_done()` 之后（**origin**） |
| `sent` | 上行被传输层接受 | `services/interaction_service.c` · `send_voice_event` 成功后 |
| `ack` | 本 turn 第一帧出队 | `services/gateway_dispatcher.c` · `active_reply \|\| result_speech_reply` 判定后（首写胜出，天然幂等） |
| `text` | 首个助手文本 | `services/gateway_dispatcher.c` · 终端文本 publish 之前 |
| `done` | 助手文本终结 | 同上 · publish 之后（`latency_close_turn()`） |
| `tts` | 首个服务端音频字节 | `main.c` · `server_audio_play_mp3` / `server_audio_play_wav` 入口 |
| `decode` | 首个解码帧 | `main/mp3_player.c` · 首次 `decoded_size > 0` 且格式校验通过后 |
| `audio` | 首个真正送出的音频帧 | `main/mp3_player.c` · 首次 `audio_arbitration_playback_write()` 成功后 |

日志形如：`latency response=1850ms latency turn release+0 sent+120 ack+340 text+890 done+1500 tts+1600 decode+1700 audio+1850`；被取消的 turn（既无 `done` 也无 `audio`）静默丢弃。

**验证状态**：主机单测 9 例全过；`tools/check-latency-trace.ps1` PASS（含 8 条接线断言）；7 个受影响编译单元用真实 `xtensa-esp32s3-elf-gcc` + 真实工程头文件路径 `-fsyntax-only` 全过。**整机 `idf.py build` 未跑**——该工程必须带 `MACLAW_PROFILE` 才会使用 `dependencies.lock.<profile>`，否则组件管理器会按 `main/idf_component.yml` 重新求解并删除其它 profile 的组件（已实测会删掉 `78__uart-uhci`，已从组件缓存恢复）。请按你惯用的 `idf.py -B build-unified-echoear -D SDKCONFIG=sdkconfig.echoear-2st build` 复核。


### 决策状态总览（v13 收口）

| 决策 | 状态 |
|---|---|
| D1 不切换大脑 | ✅ 已拍板 |
| D2 绑设备、换设备重新接入 | ✅ 已拍板 |
| D3 首次唤醒 + 20s 免唤醒窗口 | ✅ 已拍板 |
| D4 LAN 与 Hub 中转都支持 | ✅ 已拍板 |
| D5 审批仅高危 | ✅ 已拍板 |
| D6 按指令自动路由 | ⏸ 暂不排期（`capabilities[]` 字段现在留） |
| D7 主力板 echoear 2ST | ✅ 已定 |
| 次要三项 | ✅ 已关闭 |

---

## 附录　关键源码索引

| 用途 | 路径 |
|---|---|
| GUI 侧 device-gateway 服务端（本地模式，可 LAN 直连） | `guiapp/thirdparty_gateway.go`（`handleHandshake`/`handleIncoming`/`handleOutgoing`/`enqueue`） |
| Hub 侧网关路由与 reply 类型白名单 | `hub/internal/im/device_gateway.go:1178`（路由）、`:3508-3614`（`adaptDeviceGatewayReply`） |
| **GUI 离线 503** | `hub/internal/im/device_gateway.go:1772`、`:2165` |
| **F3 量化（N0-3）** | `hub/internal/im/link_health.go`（tracker）、`hub/internal/httpapi/link_health_handler.go`（`GET /api/admin/link-health/metrics`）、`hub/web/admin/link-health-tab.js`（面板） |
| **事件契约（N1-1）** | `hub/internal/im/device_event_push.go`（校验归一 + `DeviceEventPushSupported`）、`corelib/agent/client_capabilities.go`（`Features.eventPush` + 能力 prompt） |
| **事件扇出与补发（N1-3）** | `hub/internal/im/device_gateway.go`（`UpdateMachineEvent` / `eventsByMachine` / `latestDeviceEventForMachineLocked` / handshake `response["event"]`）、`hub/internal/ws/handlers_machine.go`（`reply["event"]` 分支）、`guiapp/remote_hub_client.go`（`SendDeviceGatewayEvent`） |
| **事件准入策略（N1-2 决策核心）** | `iot-agentos/main/services/event_presentation_policy.h`（§4.2 矩阵 + 状态优先级 + 闭集 severity + 播报文案，纯值）、`.../event_presentation_policy.c`（设备状态快照胶水）、`iot-agentos/tools/check-event-presentation-policy.ps1`（校验 + 主机单测） |
| **事件 ingest（N1-2 设备侧接线）** | `iot-agentos/main/services/event_ingest.h`（category 闭集 + 审批审计契约 + `event_ingest_decide` + 忙碌时延后 + 会话幂等环，纯值）、`iot-agentos/tools/host_tests/test_event_ingest.c`（16 组）、`iot-agentos/tools/check-event-ingest.ps1`（纯度 + 闭集 + wire 漂移守卫 + 单测） |
| **事件下发与呈现（N1-2 设备侧接线）** | `iot-agentos/main/services/gateway_dispatcher.c`（`type=="event"` 分支 + 能力门 + `handled/permanently_invalid`）、`.../gateway_dispatcher.h`（`apply_event` 回调）、`iot-agentos/main/main.c`（`gateway_host_apply_event`：提取→校验→决策→呈现）、`iot-agentos/main/services/gateway_transport.c`（`eventPush` 声明与接受）、`.../gateway_capability_projection.h`（`GATEWAY_CAPABILITY_EVENT_PUSH`） |
| **事件契约常量（共享）** | `corelib/im/device_event.go`（wire 闭集；设备侧 `event_ingest.h` 重复同一组值，由 `check-event-ingest.ps1` 的漂移守卫逐值比对） |
| **设备前台归属（准入矩阵的状态来源）** | `iot-agentos/main/services/foreground_coordinator.h`（`FOREGROUND_OWNER_COMMAND_VOICE`/`COMMAND_RESULT`/`MEETING` 即收音/播报/会议三列） |
| **勿扰时段** | `iot-agentos/main/sleep_schedule_service.h`（`sleep_schedule_status_t.active_window` + `override_active`） |
| **L1 延迟基线（N0-2）** | `iot-agentos/main/services/latency_trace.h`（纯值模块）、`main/services/latency_trace.c`（胶水）、`iot-agentos/tools/check-latency-trace.ps1`（接线断言） |
| 扇出范式（N1-3 的照抄对象） | `hub/internal/im/device_gateway.go`（`UpdateMachineAmbient`；**注意它没有能力检查，且归一化结果不写回 `saved`** —— 见 §9 v9 的 C40/C43） |
| **每设备独立 Agent 运行时**（人格/记忆的容器） | `guiapp/hardware_agent_runtime.go` |
| GUI→设备主动推送总线 | `guiapp/remote_hub_client.go`（`SendDeviceGatewayReply`/`Ambient`/`ToolMessage`/`PetProfile`） |
| 文本 → Agent 总入口 | `guiapp/im_message_handler.go`（`HandleIMMessageWithProgress`） |
| 数字员工/审批事件 | `guiapp/remote_hub_ve_events.go`、`app_maclaw_app_approval_hub.go` |
| **高危审批 → 设备（N1-5）** | `guiapp/device_approval_event.go`（D5-A 正向判定 + 审计契约 + 具体对象提取）、hook 在 `guiapp/agent_view_tool.go` × 3（`emitRegisteredToolApprovalAgentViewIfNeeded`/`emitDatabaseMutationApproval`/`emitArchiveExternalApprovalIfNeeded`） |
| **决策回流端点（N1-6 大脑侧）** | `hub/internal/im/device_event_ack.go`（`handleEventAck` + 关联/白名单/过期裁决/投递/清快照）、`hub/internal/im/device_gateway.go`（`ServeHTTP` 路由 + `deviceEventHandled`）、`hub/internal/im/device_event_ack_test.go`（11 例） |
| **决策落定与审计（N1-6 大脑侧）** | `guiapp/device_event_ack.go`（`applyDeviceEventAck` + `resolveDeviceApproval` + `deviceEventApprovalStore` + `deviceDecisionAuditSource`）、`guiapp/agent_view_tool.go` · `handleRegisteredToolApprovalAgentViewSubmit`（**唯一落定路径**，见 C81）、`guiapp/security_firewall.go` · `recordAuditFromSource`（`AuditEntry.Source` 记"何种方式"，见 C79）、`guiapp/remote_hub_client.go` · `handleDeviceGatewayEventAck`、`guiapp/remote_hub_message_type.go` |
| **ack wire 契约（共享）** | `corelib/im/device_event.go`（`DeviceEventAckStatus*`/`DeviceEventDecidedBy*` 闭集 + 谓词 + `ThirdPartyEventAckRequest` + `NormalizeThirdPartyEventAckRequest` 的审计规则 + `ThirdPartyEventAckEventID`）、`corelib/im/device_event_ack_test.go`（14 例） |
| **设备输入层（N1-6 设备侧的落点）** | `iot-agentos/main/app_intent_service.{h,c}`（`APP_INTENT_*` 意图词表 + 绑定表）、`iot-agentos/main/presentation/input_binding.c`（`input_binding_handle_event`，**唯一业务派发点** + `approval_gesture_for` 手势映射，见 C83/C85）、板级适配器 `iot-agentos/main/boards/echoear_2st/echoear_input_adapter.h` |
| **待决卡片与 ack 上报（N1-6 设备侧）** | `iot-agentos/main/services/event_decision.h`（纯值状态机，纯 ASCII，见 C84）、`iot-agentos/main/services/gateway_event_ack_service.{h,c}`（卡片呈现 + `POST /api/im-gateway/v1/event-ack` + `portMUX` 保护 + 4xx/5xx 分流，见 C86/C87/C88）、`iot-agentos/tools/host_tests/test_event_decision.c`（19 组）、`iot-agentos/tools/check-event-decision.ps1`（含 wire 漂移守卫 + ASCII 断言） |
| **设备侧接线点（N1-6 设备侧）** | `iot-agentos/main/main.c` · `gateway_host_apply_event`（带动作的卡片进应答槽）+ `gateway_host_flush_event_ack`、`iot-agentos/main/services/gateway_dispatcher.{h,c}`（`flush_event_ack` 主机钩子 + `poll_reply()` 中的排位）、`iot-agentos/main/CMakeLists.txt`（登记 `gateway_event_ack_service.c`） |
| **VE 工作流事件 → 设备（N1-5）** | `guiapp/device_ve_event.go`（urgency→severity 映射 + 稳定 dedupeKey）、hook 在 `guiapp/remote_hub_ve_events.go` · `handleVEWorkflowStatus` 的注意力分支 |
| **本地工具审批（D5-A 的真实 risk 来源）** | `guiapp/agent_view_tool.go`（`storeRegisteredToolPendingApprovalForPrincipal` 的三条调用路径，带真实 `security.RiskLevel`） |
| **事件契约常量（共享）** | `corelib/im/device_event.go`（导出常量 + wire 注释；**校验仍在 Hub**，见 C47） |
| **调度投递渠道（含 device，N1-4）** | `corelib/scheduler/delivery.go`（`DeliveryChannelDevice` + `SplitScheduledTaskBody`）、`corelib/scheduler/im_message.go`（别名归一）、`corelib/agentservice/schedule_dispatch_fire.go`（fire 白名单分支）、`guiapp/device_event_delivery.go`（事件构造与推送）、`guiapp/schedule_target_catalog.go`（目标目录） |
| pet 帧渲染下发 | `guiapp/device_pet_asset.go`（RGB565A8 8 帧） |
| CJK 字形按需下发 | `guiapp/device_glyphs.go` |
| 设备工具注册表 | `iot-agentos/main/device_tool_registry.c`（14 个工具） |
| 设备端长轮询（待优化） | `iot-agentos/main/services/gateway_dispatcher.c:345` |
| 常驻大脑候选 | `MaClawSrv/`（headless GUI，共享 corelib 内核） |
| **MaClawSrv 的"干活"能力证据** | `admin_scheduler.go`（定时任务）、`delegate_task` + coding-runtime、async job（`202 Accepted` + job resource）；README："Multiple logical instances can run at the same time under one user." |
| **MaClawSrv 的能力边界** | `computer_use` 零命中；"does not ship a desktop IDE chrome"；host-local bash **默认禁用**；**无 Hub 客户端、不 import `hub/internal/im`，故无 `device_gateway_reply` / `UpdateMachineEvent` 设备事件推送通道**（有 `device_pairing.go` + `device_update_catalog.go` + 设备直连 `thirdparty_gateway.go`，见 §9 v10 C50） |
| **GUI 的"控电脑"能力证据** | `computer_use_routing.go`、`computer_use_task.go`、`app_yolo_model.go`（仅存在于 `guiapp/`） |
