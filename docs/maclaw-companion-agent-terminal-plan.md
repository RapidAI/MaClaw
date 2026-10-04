# MaClaw 陪伴式语音终端 开发计划

> 定位修正版。本文**取代** `docs/maclaw-companion-duplex-plan.md` 的任务划分，两份文档的双工技术细节仍然有效。

- 版本：**v8**　日期：2026-10-04　状态：**阶段 0 已收口，阶段一 M1 已开工**：N0-1/N0-2/N0-3 全部落地，**N1-1 `event` 契约已落地**（修正记录见 §9）
- **核心定位**：接入 MaClaw GUI → **控电脑**；接入 MaClawSrv → **干活**。两者是分工，不是降级。
- **已拍板**：D1 不切换大脑（绑定即固定）｜D2 绑设备、换设备重新接入｜D3 首次唤醒+20s 免唤醒窗口｜D4 LAN 与 Hub 中转都支持｜D5 审批仅高危｜D7 主力板 echoear 2ST｜次要三项按建议关闭
- **配套文档**：`docs/maclaw-persona.md`（角色设定，N0-1 已产出，N4 验收依据）
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
- **没有"事件"这一类别** —— 没有任务完成、审批请求、日程提醒、数字员工（VE）事件
- **`corelib/scheduler/delivery.go` 的投递渠道只有 `lansenger / weixin / telegram / qq`，没有 device**（`:13-16`）
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
| **G-B　事件进不来** | 没有事件类别，日程提醒无处投递 | ① `adaptDeviceGatewayReply` 增加 `case "event"` ② `corelib/scheduler/delivery.go` 增加 `device` 渠道 ③ 照抄 `UpdateMachineEvent`（范式见 `device_gateway.go:3780-3818` 的 `UpdateMachineAmbient` 扇出） |
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
| N1-2 | **设备侧 event 渲染与语音播报**：按 severity 分级呈现（静默/轻提示/打断播报） | D | 8 | N1-1 | 三类 severity 表现符合预期 |
| N1-3 | **`UpdateMachineEvent` 扇出**：照抄 `UpdateMachineAmbient` 范式，扇出到该 machine 下所有在线设备并持久化到 handshake 快照 | H | 5 | N1-1 | 一台 GUI 接 3 台设备时全部收到 |
| N1-4 | **scheduler 增加 `device` 投递渠道**：`corelib/scheduler/delivery.go` 加 `DeliveryChannelDevice` | G | 5 | N1-3 | 日程提醒可送达设备 |
| N1-5 | **接入第一批事件源**：审批请求（`handleVEApprovalRequest`）、任务完成、VE 数字员工事件 | G | 8 | N1-3 | 三类事件能在设备上正确呈现 |
| N1-6 | **设备作为审批终端（仅高危，D5-A）**：`risk=high` 确认请求 → 设备播报**带具体对象**的上下文 → 语音/按钮决策 → `event-ack` 回流。**必须可审计**（谁/何时/何种方式批准） | G+D | 6 | N1-5 | 全程不碰电脑可完成一次高危确认；超时走"不做"而非放行；审计记录可查 |

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

**下一步可立即开工的任务**：
- **N1-3 `UpdateMachineEvent` 扇出**（5 人日）—— 契约已定，可以照抄 `UpdateMachineAmbient` 范式；**注意两处已记录的坑**：扇出必须显式调 `DeviceEventPushSupported()`（范式本身没有能力检查），补发必须重算 `ttlSec`（见 §4.1 与 §9 v8 的 C34/C35）
- **N1-4 scheduler `device` 投递渠道**（5 人日）—— 依赖 N1-3
- **N3-1 AEC spike**（3 人日，go/no-go 闸门，建议尽早启动）—— `N0-2` 前置已解除，与 M1 无冲突，可并行
- **N1-2 设备侧 event 渲染与语音播报**（8 人日）—— 契约已定，设备端可独立开工；播报顺序 = `summary` → 回退 `title`
- N1-5 接入第一批事件源（8）、N1-6 审批终端仅高危（6）—— 建议在 N1-3 打通后再接

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

## 9　评审与修订记录（v1 → v8）

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


### 决策状态总览（v8 收口）

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

## 附录　关键源码索引（新定位下最重要的 14 处）

| 用途 | 路径 |
|---|---|
| GUI 侧 device-gateway 服务端（本地模式，可 LAN 直连） | `guiapp/thirdparty_gateway.go`（`handleHandshake`/`handleIncoming`/`handleOutgoing`/`enqueue`） |
| Hub 侧网关路由与 reply 类型白名单 | `hub/internal/im/device_gateway.go:1178`（路由）、`:3508-3614`（`adaptDeviceGatewayReply`） |
| **GUI 离线 503** | `hub/internal/im/device_gateway.go:1772`、`:2165` |
| **F3 量化（N0-3）** | `hub/internal/im/link_health.go`（tracker）、`hub/internal/httpapi/link_health_handler.go`（`GET /api/admin/link-health/metrics`）、`hub/web/admin/link-health-tab.js`（面板） |
| **事件契约（N1-1）** | `hub/internal/im/device_event_push.go`（校验归一 + `DeviceEventPushSupported`）、`corelib/agent/client_capabilities.go`（`Features.eventPush` + 能力 prompt） |
| **L1 延迟基线（N0-2）** | `iot-agentos/main/services/latency_trace.h`（纯值模块）、`main/services/latency_trace.c`（胶水）、`iot-agentos/tools/check-latency-trace.ps1`（接线断言） |
| 扇出范式（照抄做 event） | `hub/internal/im/device_gateway.go:3780-3818`（`UpdateMachineAmbient`） |
| **每设备独立 Agent 运行时**（人格/记忆的容器） | `guiapp/hardware_agent_runtime.go` |
| GUI→设备主动推送总线 | `guiapp/remote_hub_client.go`（`SendDeviceGatewayReply`/`Ambient`/`ToolMessage`/`PetProfile`） |
| 文本 → Agent 总入口 | `guiapp/im_message_handler.go`（`HandleIMMessageWithProgress`） |
| 数字员工/审批事件 | `guiapp/remote_hub_ve_events.go`、`app_maclaw_app_approval_hub.go` |
| **调度投递渠道（缺 device）** | `corelib/scheduler/delivery.go:13-16` |
| pet 帧渲染下发 | `guiapp/device_pet_asset.go`（RGB565A8 8 帧） |
| CJK 字形按需下发 | `guiapp/device_glyphs.go` |
| 设备工具注册表 | `iot-agentos/main/device_tool_registry.c`（14 个工具） |
| 设备端长轮询（待优化） | `iot-agentos/main/services/gateway_dispatcher.c:345` |
| 常驻大脑候选 | `MaClawSrv/`（headless GUI，共享 corelib 内核） |
| **MaClawSrv 的"干活"能力证据** | `admin_scheduler.go`（定时任务）、`delegate_task` + coding-runtime、async job（`202 Accepted` + job resource）；README："Multiple logical instances can run at the same time under one user." |
| **MaClawSrv 的能力边界** | `computer_use` 零命中；"does not ship a desktop IDE chrome"；host-local bash **默认禁用** |
| **GUI 的"控电脑"能力证据** | `computer_use_routing.go`、`computer_use_task.go`、`app_yolo_model.go`（仅存在于 `guiapp/`） |
