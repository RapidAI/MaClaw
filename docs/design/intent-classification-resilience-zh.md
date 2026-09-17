# 意图分类韧性改进方案（降级自愈 + 本地分类头[条件启用]）

- 状态：路线 A 已实现（2026-09-17：0a/0b-i/0b-ii/0c 全部落地 + 一轮实现后审查修复 16 项）；路线 B 按 §3 门控待决策
- 已知限制（实现后审查确认，接受为 v1 行为）：
  0. **跨 turn 不收养（含缓解）**：turn 替换/结束（`cleanupTurnCtx`/`RegisterSemanticTurnReplacementCancel`）会取消 parentCtx 中止 detached 读——收养窗口靠**完成留存期**（detachedReadCompletedRetention=5s，完成后的 entry 仍可被迟到的重复请求收养）覆盖 late-verdict goroutine 的调度延迟；真正跨 turn（重发新请求）仍退化为重发（hits=2）。同学期内收养正常。
  1. **跨通道同 userID 作废凭据卡**：卡片作废按 userID 收敛，桌面+IM 桥接同 userID 时一侧自由文本会作废另一侧 pending 的凭据卡（代码内已注释；单通道无影响）。
  2. WS（Responses-WebSocket）端点不做 detached，预算超时按 budgetFired 不投毒、走原 late-verdict 重发兜底。
- 日期：2026-09-17
- 起因：生产事故——LLM 端点一次瞬时网络故障导致「保存到知识库」请求静默丢失写工具
- **路线变更（v5，战略评审结论）**：原 Phase 0-3 平铺路线拆分为——**路线 A（Phase 0 全四件，承诺交付）** 与 **路线 B（Phase 1-3 本地分类头闭环，设门控，按路线 A 上线后的实测数据决策）**。理由见 §3。

---

## 1. 背景与事故复盘

### 1.1 事故经过（2026-09-17 11:24，摘自 `~/.maclaw/logs/maclaw.log`）

| 时间 | 事件 | 日志位置 |
|---|---|---|
| 11:24:34 | 用户发送「将以下信息保存于知识库：驱网服务器 www.driverdevelop.com root sunion123」 | `app_wails_bindings.go:2551` |
| 11:24:36 | 轻量 LLM 调用（pending-reply 答案意图分类，`TimeoutSec:2`，`im_history_persistence.go:559`）`context deadline exceeded`；endpoint failure gate 以 ttl=30s 封禁同 key 轻量调用 | `stream.go:166`、`llm_endpoint_failure_gate.go:86` |
| 11:24:36~37 | UIC 对该消息分类：L3 树推理被熔断器拦截失败，回退 L2 embedding 结果模糊，最终 `primary=unknown conf=0.30 Degraded` | `classifier.go:489,943` |
| 11:24:37 | exec-router 按 `ask_user continuation`（上一轮模型以 ask_user 收尾「您选哪个？」）给 full profile；**UIC 仍正常运行**（此路径不跳过分类，已核实 `im_execution_profile.go:99-111`） | `im_agent_loop_start.go:215` |
| 11:24:37~48 | 因无 `LabelKnowledgeWrite`，按 fail-closed 规则 `knowledge_save_*` 未进工具表；循环以 `tools=0` 跑完，模型如实回复"本轮没有接上知识库写入工具" | `router.go:142-146`、`im_agent_loop_shared.go:412` |
| 11:24:48 | 主循环 LLM 调用 11 秒后即成功——端点其实早已恢复，但分类窗口已过 | `stream.go:199` |

### 1.2 根因：三层机制叠加

1. **策略层（核心）**：写类工具完全依赖 L3 LLM 分类授权。`classifier.go:211-219` 明确"本地信号不作降级决策路径"；`router.go:1230` `classificationActivatesTools` 要求 `!Degraded && conf≥0.50/0.70`。LLM 一不可用，写能力静默消失，无任何自愈。
2. **证据层**：L2 是 anchor 质心最近邻（`layer2.go:340`），只能表达"像谁"，学不到判别方向——「保存于知识库」被 embedding 判成 `file_download conf=0.75`。
3. **容错层**：endpoint failure gate 的 key 含 `model`/`wire`（`llm_endpoint_failure_gate.go:113`），轻量调用换模型后（`im_app_accessors.go:197-226`）主链路成功无法解除其封禁；晚到树 verdict（`scheduleLateTreeVerdict`，`classifier.go:956-1006`）只暖缓存，对已跑完的轮次无通知、无重路由（轮内收养点 `adoptLateTreeSemanticIntent` 白名单只有 6 个读类标签，`semantic_late_tree.go:64-75`——**写类 verdict 永远不可能被轮内收养**）。

### 1.3 深层根因：时间预算的错误语义

网络延迟是不可控随机变量，预算必然偶尔失败；不稳定来自失败后的三种错误用法：

1. **超时定案（decide）**：超时把某个用户可见功能定死。本轮 UIC L3 超时 → 工具表定案且无修复通道。（`pending-reply-answer` 是反例：超时按 `new` 绑定，无害。）
2. **超时投毒（poison）**：把延迟信号升格为端点健康判决。2s 预算超时只证明"这次没赶上预算"，不证明端点不可用——却被记成网络失败、熔断 30 秒、株连全部轻量调用。端点慢时功能表现随网络 mood 抖动。
3. **串行链放大方差（serial chain）**：pending-reply（≤2s）→ L2（本地 ~10ms）→ L3（≤30s）→ planner → 主循环（11s+）逐级串行，各环节延迟方差累加，任一撞预算则全链失败。

**设计原则**：

| 原则 | 落点 |
|---|---|
| 超时只能"降级 + 异步修复"，不能定案 | P0-1 轮末自愈 |
| 慢 ≠ 错：超时只决定调度，不否定结果有效性 | P0-3 慢结果收养（detached read） |
| 传输层错误才允许熔断；预算内超时一律只降级、不 observe | P0-2（budgetFired 机制） |
| 敏感写操作的确认挂**工具执行层**，覆盖所有路径 | P0-4 凭据闸 |
| 缓存失效半径最小化（per-key tombstone） | P0-1 |
| 关键决策尽量不经过网络 | 路线 B（本地分类头，条件启用） |
| **用户原文永不因测量/训练目的出网**（战略评审新增） | 路线 B 全链路 |
| 功能静默消失必须有显式提示（无遥测桌面产品的系统性要求） | 嵌入版本不匹配等场景 |

### 1.4 已核实**不是**问题的点（避免误修）

- ask_user continuation 绑定**不会**跳过 UIC 与语义路由（`im_execution_profile.go:99-111`）。
- 主链路成功仅在熔断 key 恰好相同时才清除轻量封禁（`llm_endpoint_failure_gate.go:72-77`）——key 含 `model`/`wire`，多数配置下并不清除。这正是 P0-2 要修的点。
- 模型如实报告"没有工具"是期望行为，不是幻觉，无需检测纠正。

---

## 2. 设计目标与非目标

### 2.1 目标

| # | 目标 | 衡量标准 | 归属 |
|---|---|---|---|
| G1 | LLM 端点短暂故障时，显式写知识库请求不再静默失败 | 复现 1.1 场景，写工具正常挂载（或用户收到明确的"稍等重试"而非"没工具"） | 路线 A |
| G2a | 降级轮次可自愈、慢端点结果不被丢弃、误熔断消除 | P0-1/P0-2/P0-3 各自验收通过 | 路线 A |
| G2b | 端点完全断连（blackout）窗口的分类质量不崩盘 | blackout ≥5 分钟时读类准确率 ≥ L3 可用时的 95% | **路线 B**（条件启用，见 §3 门控） |
| G3 | 降级自愈对用户透明 | 故障恢复后受影响轮次自动续跑，无需用户重发 | 路线 A |

### 2.2 非目标

- 不替换 L3 树分类：WorkflowType、secondary、复合意图、execution affordances 仍由 L3 产出。
- 不改变 `knowledge_save_*` fail-closed 的默认立场：任何本地授权都是**白名单 + 高阈值 + 独立第二票 + 审计**（路线 B 内容）。
- **G4（L3 调用量净削减）降级为非目标**（战略评审）：单用户桌面的 L3 分类成本不值得持续蒸馏闭环；若产品形态转向托管/多用户，以不同架构重新评估。
- 不在本期处理 WorkflowType 的本地判定。

---

## 3. 路线总览与门控

```
路线 A（承诺交付）
  Phase 0a  熔断细化（P0-2）                      —— 约 1 天，独立可交付，直接消除事故触发器
  Phase 0b-i  降级自愈状态机（P0-1，含前端通道）    —— 2~3 天
  Phase 0b-ii 写工具执行层凭据闸（P0-4，含确认卡片） —— 3~5 天，与 0b-i 可并行
  Phase 0c  慢结果收养（P0-3）                     —— 单独排期（改造轻量请求栈）+ 长延迟压测

路线 B（条件启用，本地分类头闭环）
  Phase 1  数据闭环 + 影子分类头（只观测）
  Phase 2  本地权威分级（读类先行，写类高门槛）
  Phase 3  持续蒸馏 + 自适应预算
```

**为什么拆（战略评审结论，如实记录）**：

1. 事故的直接触发器是"2 秒预算撞线 + 误熔断 30 秒"——**P0-2 单独即可消灭**；P0-1/P0-3 补齐自愈与慢结果收养。路线 A 完成后 G1/G2a/G3 从装机第一天对所有用户成立，不依赖任何数据积累。
2. 路线 B 的唯一硬增量 = **blackout 窗口**（端点完全断连）的分类质量与降级窗口免卡片的写 UX。blackout 是否真实高频发生未经实测（本次事故端点 11 秒即恢复，不是 blackout）。为一个未证实场景的舰队级 ML 闭环支付隐私面与运维面，顺序错误。
3. **路线 B 门控判据**：路线 A 上线后，用现有日志（`[UnifiedIntentClassifier] result` 行）统计 degraded 轮次占比与端点断连时长分布，满足任一即启动路线 B 评估：
   - 连续 4 周 degraded 轮次占比 > 0.5%，或单次端点不可用 > 5 分钟的事件月发 ≥2 次；
   - 产品形态变化（托管/多用户/按 token 计费）。
   评估时优先路线 B 的**读类-only、集中预训头**形态（砍掉写类本地授权与 per-device 闭环，建设量约减半）。

---

## 4. 路线 A：Phase 0

### P0-2 熔断闸门细化（0a）

**问题**：① key 含 `model`/`wire`，换模型后封禁无法被主链路成功解除；② 所有轻量调用共用一个 key，误伤面大；③ **预算内超时被误投毒**（本次事故直接触发器）。

**方案**：

1. **预算中止不投毒（最优先）**：`IsNetworkLLMRetryError`（`corelib/agentruntime/llm_retry_error_kind.go:56`）对 `context.DeadlineExceeded` 一律返回 true——错误层面无法区分"预算烧了"与"传输层超时"。正确机制在调用点用证据判定：`observe` 增加 `budgetFired bool`（各调用点 `ctx.Err() != nil` 得出）；`budgetFired==true` 一律不 observe。`classifyLLMRetryError` 保持原样。
2. key 增加 `category` 维度（`lightweight-classify` / `uic-tree` / `main-stream`），独立封禁。`llmEndpointFailureKey` 加 category 参数（现只吃 cfg，`llm_endpoint_failure_gate.go:113`），调用点透传。
3. 主链路成功时，按端点前缀（URL+protocol+provider，去 model/wire）把该端点下所有轻量类别封禁**缩短至 2s**（非直接清除）。
4. 轻量类别 ttl 30s → 10s。

**改动点**：`guiapp/llm_endpoint_failure_gate.go`、`guiapp/llm_stream.go:1024`、`guiapp/app_embedding.go:909-989`、`guiapp/llm_lightweight.go:107`。

**顺带**：`pending-reply-answer-fast` 的 `TimeoutSec: 2 → 5`（`im_history_persistence.go:559`）。

### P0-1 降级轮次能力缺口自愈（0b-i）

**触发**（缺一不可）：本轮 `SemanticIntent.Degraded==true`（含 `ControlPlaneFailure` 子类）；用户文本命中窄词法"显式能力请求"模式（「保存/存到/记入…知识库」等，**仅触发器不作授权**）；本轮工具表不含该类写工具。

**方案**：

1. **Per-key 失效 + tombstone（不用全局 epoch bump）**：`cacheEpoch` 挂在 App 级单例 UIC 上（desktop/hub handler 共享），全局 bump 会孤儿化所有用户的缓存。改为：P0-1 只把 `(UserID, Text, history哈希)` 加入 tombstone 集合（key→过期时间）；`cacheAndLog` / `scheduleLateTreeVerdict` 落缓存前查 tombstone。失效半径精确到一条消息；全局 epoch 保留给接线期换线专用。已入缓存的 `tree` 来源 `Layer==3` 条目**直接采信**（缓存条目带来源标记 `tree`/`l2`；重分类只跳过 `l2` 来源），不必重发。
2. **成功判定**：仅 `Layer==3` 或 `Layer==4` 算成功；超时或仍降级 → 推送明确提示。
3. **续跑与消息通道（含前端改动）**：后端现有事件全绑定 request 槽位，前端 `useAIAssistant.ts:4653` 对无活动 round 的响应直接丢弃——"同轮追加第二条消息"今天不可能落地。改法二选一（实现时定）：(a) 续跑走新 requestID + 允许无 userMsg 的 assistant round（前端小改）；(b) 新增 `ai-continuation` 事件 + 前端追加渲染（`ai-btw-*` 独立通道是先例，`app_wails_bindings.go:3214`）。续跑 = 增量工具挂表 + **模型重新决策执行** + 追加新消息说明（不改写原回复）。
4. **去重与状态机**：去重键 = request_id + 用户文本哈希，存轮次 runtime（重启不补跑）；续跑 pending 期间用户发新消息或取消 → 立即作废。已知竞态（有界可接受）：tombstone 写入与 late-verdict 落缓存的交错窗口可能使一条刚到的 Layer=3 结果作废并多烧一次调用——声明为已知项，验收用例需容许一次重试。
5. **秘密载荷（0b-i 过渡策略）**：P0-4 未上线前，命中秘密扫描的轮次**不自动续跑**，只推送明确提示；0b-ii 上线后恢复自动续跑（确认由执行层闸把关）。
6. **扩大 late verdict 收养窗口（仅读类）**：轮末 finalize 前再 poll 一次 `adoptLateTreeSemanticIntent`；写类轮内收养有意不放开（`semantic_late_tree.go:64-75` 白名单仅 6 读类标签，本期不扩）。

**改动点**：`guiapp/im_degraded_turn_recovery.go`（新增）、`guiapp/im_agent_loop_shared.go`（轮末钩子）、`guiapp/semantic_late_tree.go`（二次 poll）、`corelib/intent/classifier.go`（tombstone + provenance）、**前端 useAIAssistant.ts（续跑消息通道）**。

### P0-4 写工具执行层凭据闸（0b-ii）

**问题（挂点经评审修正）**：主路径（端点健康、L3 正常授权）今天存秘密就是模型直接调 `knowledge_save_*`，零确认——那才是流量大头；扫描"用户文本"扫不到"把刚才那个密码存一下"的指代。确认必须挂**工具执行层**、扫**工具载荷**，一处覆盖所有路径。

**确认机制**：对口的是已接线的确认闸：`aiConfirmationStore`（`guiapp/im_confirmation_store.go:12`，按 userID 单槽、TTL 2h、落盘持久化）+ `__confirm_execution__ <id>` / `__cancel_execution__ <id>` 结构化命令协议（`guiapp/im_confirmation_gate.go:19-20,454`）。P0-4 新建独立卡片类型挂这套协议。

**确认语义（收敛性改动，对凭据闸致命的问题必须修）**：

1. **禁用自由文本确认**：现有闸的 LLM 分类（`im_confirmation_gate.go:56-118`）会把用户随手一句"好的/行"判为 confirm——对凭据写等于"未审阅即入库"。凭据卡片**只允许按钮/结构化命令匹配**（`parseConfirmationActionCommand` 的 ID 校验，`im_confirmation_gate.go:35-48`）；modify 分支对一次性凭据写无意义 → 收到修订文本直接作废卡片并提示重发。
2. **收敛语义按卡片类型隔离**："pending 期间自由文本 = 作废卡片"**仅对凭据卡片类型生效**（以 `pendingConfirmation.TaskType` 判别），不改变现有 plan 确认的既有行为。
3. **路由优先级**：internal 确认命令 > 凭据卡片 pending 检查 > `pendingAskUser` 检查 > 新轮次启动。卡片回复**不产生新轮次、不进 `isAskUserResponse` 分类**。
4. **超时**：2 分钟未确认按拒绝。
5. **通道互踩修复（既有 bug，顺带修）**：`consumePendingAskUserAnswer`（`im_entry_context.go:193` → `im_pending_reply.go:369`）会把 `__confirm_execution__` 这类 internal command 文本 LoadAndDelete 吞成 ask_user 答案——`isInternalCommand` 文本必须跳过 pending 绑定。无论 P0-4 做不做都应修。

**确认结果回送与栅栏语义（评审新增，0b-ii 开工前必备）**：

现有确认闸是**执行前**语义（preflight 拦截 → 确认 → `ConfirmedResume` 重放原文开新 loop）；凭据闸是 **loop 进行中**的 tool-call 栅栏——`knowledge_save_*` 已派发、loop 阻塞在中途，必须显式定义：

1. **唤醒通路**：tool-call 侧阻塞在 result channel 等待确认结果（借用 `corelib/security/approval_flow.go` `RequestApproval` 的 resultCh 模式），preflight handler 消费确认命令后经该 channel 回送；不用轮询。
2. **取消穿透**：loop ctx 取消（用户打断/关窗）必须穿透栅栏——等待点同时 `select` loop ctx，取消即让 tool-call 返回错误并正常终止 loop，不泄漏挂起点。
3. **重启语义**：loop 不持久化而 store 落盘（TTL 2h）——**loop 死亡（取消/重启）时其凭据卡片必须作废，不可走 `ConfirmedResume` 重放**（对一个已死的 mid-loop 工具调用做 resume = 无上下文重跑写入）。卡片状态与 loop 生命周期绑定：loop 终止 → 作废同 turn 的 pending 凭据卡片；用户重启后点一张已作废的卡片 → 提示"该操作已过期，请重新发起"。

**扫描器**：新建权威秘密扫描器（升级 `corelib/security/sensitive_detector.go:32`——现有 5 正则漏"root sunion123"、中文口令形态）：中英口令 keyword、`user + password` 对、token/私钥/JWT。必过测试 = §1.1 事故原文 + 中文形态。路线 B 的 intentdata 复用同一扫描器。

**本闸同时是路线 B 写类在降级窗口的独立第二票**（见 Phase 2）。

**改动点**：`guiapp/im_confirmation_gate.go`（新卡片类型 + 收敛语义 + 类型隔离）、`guiapp/im_confirmation_store.go`、凭据栅栏与唤醒通道（`knowledge_save_*` 执行入口 `router.go:1511-1532` 链路 + `guiapp/semantic_knowledge_ingest.go`）、Wails 卡片组件 + binding、`im_entry_context.go:193`（internal command 跳过）。

### P0-3 慢结果收养 detached read（0c）

**问题**：到点即弃，慢端点双倍负载（重发）、双倍计费；慢不是错，错的是把"调度截止"当"结果失效"。

**方案**：

1. **可分离请求**：请求 ctx 用 `detachedCtx, _ := context.WithCancel(用户ctx)`——派生自用户/父 ctx，**不派生自预算 ctx**；前台阶段照常 `lease.SetCancel(detachCancel)`（scheduler 抢占仍可杀）；到达调度截止时 `lease.Release()`（**必须释放**——UIC L3 是前台优先级，caller 名不含 background 子串，不释放白占 6 个 FG 槽之一）但不断开连接；用户 cancel 经 parent ctx 天然生效。
   - **签名改动**：`LLMClassifyContextFunc`（`app_embedding.go:963-994`）目前只能看到 fusion 截止 ctx——必须改签名传 parent ctx 或 detach 回调，否则"用户取消仍中止"不可兑现。
   - 三个 `do*` 变体（OpenAI/Responses/Anthropic，`llm_request_helper.go`）内嵌 `defer cancel()` 一并改可分离模式；**Responses-WebSocket 变体例外**（`cfg.IsResponsesWebSocket`）：WS 是长连接消息流不存在"续读 body"，detached 不适用 → 该变体回退 late-verdict 重发。
2. **去 client 级 Timeout**：`app_embedding.go:915、:946、:976` 三处 `http.Client{Timeout:35s}` 移除，超时收敛 per-request ctx。
3. **宽限窗**：`min(2×预算, 60s, 连接保活上限)`；验收长延迟用例取值与此式一致（预算 30s 档用 >35s）。
4. **收养**：宽限窗内正文完整到达 → 读类走 `semantic_late_tree.go:33` 收养点、降级轮次走 P0-1 续跑通道。
5. **正向健康信号回流**：detached 续读成功 → `observe(success)` 提前解除/缩短该端点封禁。
6. **single-flight（键 epoch 无关）**：功能性 in-flight 键 = `UserID + Text + history哈希`（不含 epoch），detached 续读 / late-verdict 重发 / P0-1 重分类共用，先到先得。路线 B 的金丝雀除外（刻意双跑，不互斥）。

**改动点**：`guiapp/llm_request_helper.go`、`guiapp/app_embedding.go`、`corelib/intent/classifier.go`。

**成本论证**：对照组是重发完整请求——已完成的上游计算本来计费，续读不产生第二个请求，成本不更差，且避免慢端点重试风暴。

---

## 5. 路线 B：数据闭环 + 影子分类头（条件启用）

> 门控见 §3。本节为启动路线 B 时的完整设计；启用时优先读类-only、集中预训形态。

### B-0 架构定案（战略评审新增，启用前先定死）

1. **训练架构 = 集中预训 + 设备端校准**：开发方用自有语料集中训练读类头并随应用打包（40×768 fp32 ≈ 120KB）；设备端 intentdata 只用于本地校准阈值与（可选的）本地微调。**设备端产出（校准后权重、intentdata）永不离开设备**——问题报告打包排除清单在 P1-1 基础上追加权重文件。
2. **新装设备条款**：无本地数据的设备 = 全程路线 A 行为；读类本地路由随"打包预训头通过验收 + 设备 shadow 抽验达标"渐进启用。P1-4 的每设备样本门槛只适用于设备校准环节，验收以"集中预训评估集 + 设备 shadow 数据"合并判定。
3. **嵌入版本对齐**：`intent-head.json` 记录**嵌入模型内容哈希**（非文件名——`embedder.go:10` 文件名是写死的常量，无轮转机制，按名校验会失效）。启动时哈希不匹配 → **显式 UI 提示**（"本地分类模型需要更新"）+ 用本地 intentdata 重嵌入重训的迁移任务；迁移完成前行为 = 路线 A。不允许静默回退（无遥测桌面产品的功能蒸发必须可见）。
4. **运维形态**：不新建 nightly/cron 框架——重训与回标挂在 post-conversation 链上按计数器触发（每累计 N 条新样本跑一次，关机即跳过、无堆积语义，`im_history_persistence.go:323-338` 范式）；校准存档只保留最近 2 版；金丝雀默认关（见 P2-4）。

### P1-1 生产数据记录器 `corelib/intentdata/`

- **挂点**：UIC 内部 `ClassifyContext` 返回处单点记录——guiapp 各调用点会漏 workflow 拦截、planner 自分类、动态路由；`ClassifyEmbeddingOnly`（`:718-752`）不记录。
- **来源字段**：`source` 按 UIC 内部可判定的生产者路径命名——`"tree"`（L3 verdict 被采用）/ `"l2"`（未升级 L3）；去重键 `(UserID, Text, history哈希)` 每进程一次。
- **脱敏**：落盘前过 **P0-4 权威扫描器**（现有 `SensitiveDetector` 漏"root sunion123"/中文口令）。`scrubbed: true/false` 字段用于局部分层分析。
- **历史字段**：记录近 6 条历史的哈希列表；本期语义 = history-free（回标时统一降权；哈希→会话库解析管道留作增强）。
- 配置：`intentdata.disabled` **默认关，首次开启需用户确认**；`intentdata.retain_days`（默认 90）；问题报告打包默认排除（含权重文件）。
- **出网红线（战略评审新增）**：intentdata 只留本地；**任何用户原文都不因测量/训练目的出网**（回标/金丝雀只送 scrub 后文本，且见 P1-2/P2-4 的排除规则）。

### P1-2 导出、回标与训练

- 导出 `maclaw-tool export-intent --since 30d -o intent.jsonl`（设备本地命令，用于用户自携/诊断）；`--include-raw` 显式开关默认禁用。
- **L3 回标（只送 scrub 后文本；scrub 敏感样本排除）**：
  - `source:"l2"` 样本按计数器触发送 L3 补教师标签（挂 post-conversation 链，非 nightly）；
  - **排除规则（修正 v4 的隐私矛盾）**：scrub 会改变 L3 判定的类别（预计 knowledge_write 首当其冲——凭据形态本身是强特征）**整体排除在回标与金丝雀样本池之外**，这些样本只留本地用于阈值校准，不送网。"原文 vs scrub 后判定差异"的测量由开发方在自有语料上集中完成，不经用户设备；
  - 隐私口径诚实化：回标是这些文本首次离机——独立开关、独立审计；history-free 弱教师统一降权。
- **教师唯一**：训练标签只来自 L3（含回标）；无 L2 弱标签路径；Degraded/unknown 不入集。
- 训练 `scripts/intent_head_train.py`：集中预训为主；导出 `--with-embedding` 仅用于设备本地校准；one-vs-rest logistic regression，样本量上来换 MLP；冷启动用 anchor 均值初始化（`definitions.go:1131`）；`intent-head.json` 含版本、标签集、权重、**嵌入内容哈希**、每标签校准阈值、统计、指标。可选优化：利用 MRL 性质按 256 维训练，权重缩 3 倍、重训成本更低（`init.go:9-12`）。

### P1-3 Go 侧推理 + shadow 模式

- `corelib/intent/head.go`：`HeadClassifier`，`Predict(vec)` 一次 GEMV（复用 `corelib/embedding/tensor` SIMD）。
- 接入为 UIC "L2.5"：`classifyByEmbedding` 之后、L3 升级判定之前，与 L2 质心分数融合（初版取 max，调参并入 `RunGridSearch`）。
- **shadow**：只打日志不改路由；设备端抽验期用 shadow 数据做逐标签一致率（含回标样本，按 `scrubbed` 分层）。

### P1-4 评估门槛（准入，读/写类解耦）

- 评估集 = `ProductionCases()` + 真实模型回归 harness + 集中训练留出集 + 设备 shadow 样本（含回标）：
  - **读类**：macro-F1 ≥ 0.95 且不低于 L2 质心；
  - **写类（首期仅 `knowledge_write`）**：precision ≥ 0.98、recall ≥ 0.95，校准后高置信区 precision ≥ 0.99；
  - **最小样本量**：写类 ≥500 正例、读类每标签 ≥100 正例（集中预训集侧判定）。达不到的标签留在 L3-only。

---

## 6. 路线 B：本地权威分级

### P2-1 头判定升格为 Layer=4 本地权威

- `Layer` 新增 4，`Reason` 标 `local-head v<version>`，`Degraded=false`；同步 `types.go:258` 注释。
- **门改造清单（四个，漏一个写类被饿死）**：`router.go:1230`（不看 Layer，天然通过）；`semantic_tool_routing.go:964`（现要求 3/23）；`semantic_tool_routing.go:1026`（排除 3/23 后只放只读族，写类需独立通道）；`im_message_handler_workflow.go:701,703`（逃逸门要求 3/23）。
- 白名单驱动：仅 `intent-head.json` 标 `authority: true` 且过 P1-4 的标签。

### P2-2 分级策略与独立第二票

| 层级 | 标签范围 | 授权条件 | 网络开销 |
|---|---|---|---|
| 读类 | search, live_data, office, knowledge_read, file_read 等 | 头 conf ≥ 校准阈值 | **纯本地**（健康/降级都不经网络） |
| 写类 | **首期仅 `knowledge_write`** | 头 conf ≥ 校准阈值 ＋ **独立第二票** ＋ 审计；秘密载荷强制非缓存 L3 | 健康时**同步 L3 复核**；降级时确认卡片 |
| 其余 | — | 维持 L3-only，fail-closed 不变 | — |

**第二票（同步复核定案）**：

- **端点健康 → 同步 L3 复核票**：写类裁决先送 L3 确认，一致才执行。写是稀有事件（N_w ≪ N_r，本文档声明的假设；批量导入类场景使假设失效时需重新评估），同步复核成本可忽略，换来单请求级独立性。
- **端点降级 → P0-4 确认卡片作为第二票**（人工确认，真独立）。
- 金丝雀一致率是**群体健康指标**（跌破门槛自动退白名单），不作单请求授权条件。
- 词法只作召回触发器，永不作授权。审计：时间、文本哈希、头版本、置信度、第二票类型与结果、授予工具，进 `database_audit.jsonl` 同类通道。兜底：头不可用/未加载/不在白名单/嵌入哈希不匹配 → 路线 A 行为。

### P2-3 与 P0-1 的协同

P0-1 续跑优先等待 Layer=4 判定（毫秒级），L3 后备。P0-4 凭据闸在所有路径生效，Layer=4 不免确认。

### P2-4 本地优先路由与质量保障

**三级路由**：① 读类头命中白名单且 conf ≥ 阈值 → 纯本地直接路由（~10ms）；写类头命中 → 本地裁决 + 同步 L3 复核（P2-2）；② 不确定 → L3（慢/不可用由 P0-1/P0-3 兜底）；③ 头未加载 → 路线 A 链路。

**质量保障**：

- **影子对照**（P1-3）；
- **金丝雀抽样（默认关，opt-in 诊断开启）**：开启后，本地路由轮次按 requestID 哈希取可配比例（默认 5%）后台双跑 L3：
  - 挂点：planning 入口 `adoptLateTreeSemanticIntent` 旁（`semantic_tool_routing.go:1255-1259`）或绑定点判定 Layer=4 时入队；
  - caller 命名含 background 子串（如 `background-intent-canary`），否则落前台占 FG 槽（`llm_concurrency_scheduler.go:434-449`）；
  - 绕过 `shouldSkipLightweightLLM`（价值就在端点恢复期观测）；错误不进 failure gate（ctx-cancel 守卫）；**不写主缓存**；**不共用功能性 single-flight 键**；
  - **只送 scrub 后文本；scrub 敏感类别（见 P1-2 排除规则）不进金丝雀池**；
  - 跌破门槛（读类 0.97 / 写类 0.99）自动退白名单，无需发版；
- **校准回归门槛**：头权重版本升级必过 P1-4，存档只留最近 2 版；
- **逐标签独立阈值**：互不拖累。

**远期（路线 B 内评估项）**：本地 gemma 小模型承担 L3 树分类的"本地副本"（`corelib/embedding/gemma.go` 基建现成）。

---

## 7. 路线 B 持续优化（Phase 3，随门控启用）

1. **定期重训 + 回标**：计数器触发（挂 post-conversation 链，非 nightly 框架；关机跳过无堆积）：回标 → 重训 → 回归门槛 → 版本化发布（带校准报告、可回滚）。
2. **L3 流量观测**：报读类本地命中率 h 与 L3 升级量变化（G4 已降为非目标，此项仅作路线 B 自身健康度观测，不做成本承诺）。scrub 敏感类别与写类复核不计入"可避免"侧。
3. **样本主动补强**：shadow/金丝雀中头与 L3 不一致的样本自动入下轮训练集（hard negative mining）。
4. **自适应预算**：硬编码超时改为按端点实测延迟动态设定（每次调用已记 `elapsed`）；按 endpoint key 维护 EMA/p99，预算 = max(调用方下限, p99+余量)，受绝对上限约束。

---

## 8. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 训练数据偏斜 | anchor 冷启动 + `ProductionCases` 兜底 + L3 回标补分布；不达标不进白名单 |
| 教师分布偏差（L3 样本全是难例） | UIC 内记录 l2 样本 + 回标（history-free 弱教师降权） |
| 头权重损坏/版本不匹配 | 加载失败显式 UI 提示（非静默回退）；嵌入内容哈希校验 + 本地重训迁移任务 |
| 嵌入模型升级致头失效 | intent-head.json 存内容哈希；迁移完成前 = 路线 A 行为 |
| 构造文本骗写工具 | 授权 = 高置信 + 真独立第二票（同步 L3 复核/确认卡片）；载荷收窄；审计；precision ≥0.99 |
| 主路径存秘密零确认 | P0-4 执行层凭据闸：载荷级扫描 + 确认卡片，覆盖全部路径 |
| 扫描器漏检 | 权威扫描器以事故原文 + 中文口令形态为必过用例；intentdata 复用 |
| 自由文本误确认 | 凭据卡片禁用自由文本确认，仅按钮/命令匹配；卡片 pending 期间自由文本 = 作废（仅凭据卡片类型）；路由优先级 internal 命令 > 卡片 > ask_user > 新轮次 |
| **测量通道泄露出网（战略评审）** | 用户原文永不出网：回标/金丝雀只送 scrub 文本，scrub 敏感类别整体排除在送网样本池外 |
| **设备端权重流出** | 设备端校准权重永不离开设备；问题报告排除清单含权重文件 |
| 缓存吞掉重分类 | per-key tombstone（非全局 epoch）+ provenance（tree 条目直接采信） |
| 多通道在途并发双发 | 功能性 in-flight 键 epoch 无关；金丝雀除外（刻意双跑） |
| P0-1 续跑副作用 | 增量工具 + 模型重新决策；去重键；新消息/取消即作废；秘密载荷 0b-i 期不续跑 |
| 续跑消息被前端丢弃 | 新增/复用独立消息通道（ai-continuation 或无 userMsg round） |
| loop 死亡遗留 pending 卡片 | 卡片与 loop 生命周期绑定：loop 终止即作废；不可 resume 重放 |
| detached 与 scheduler 冲突 | detachedCtx 绑用户 ctx；截止即 lease.Release；签名传 parent ctx；WS 变体回退重发 |
| 词法触发器被滥用 | 触发器只决定"是否重试"，永不作授权 |

---

## 9. 验收

**路线 A：**
- **P0-2**：故障注入（gate 封禁 + "保存到知识库 X"）→ 工具挂载或明确重试提示；主链路成功后轻量封禁缩短至 ≤2s 放行；**预算撞线用例**：2s 预算超时后端点正常时 gate 无新条目（`budgetFired` 生效）。
- **P0-1**：预置已入缓存的错误标签，重分类不被吞（tombstone 生效）；tree 来源缓存条目直接采信不重发；竞态窗口多烧一次调用为已知容许项；新消息到达即作废续跑；**续跑消息在 UI 完整呈现**。
- **P0-4**：扫描器必过 = §1.1 事故原文 + 中文口令形态；**主路径**含秘密载荷写库 → 出现卡片、按钮确认后入库、未确认/超时/期间发新消息均不入库；自由文本"好的"不被判为确认；internal command 不被 ask_user 吞掉；**loop 取消/重启后 pending 凭据卡片作废、不可 resume**；非秘密载荷主路径零干扰。
- **P0-3**：延迟 5s 与 >35s（预算 30s 档）端点，宽限窗内收养、上游计数 = 1、无并发重发；用户取消在 detached 期间即中止；detached 成功解除 gate 封禁；WS 变体回退重发路径可用；长延迟压测不占 FG 槽。

**路线 B（门控启用后）：**
- intentdata 经权威扫描器（事故原文 grep 无明文）；设备端产出不出网（审计打包清单核对）；嵌入哈希不匹配 → 显式提示 + 路线 A 行为。
- shadow 报告产出；P1-4 达标清单（含最小样本量，集中评估集 + 设备 shadow）。
- 故障注入「保存于知识库」端到端：非秘密载荷 + 降级窗口 → 卡片确认后写库成功；审计含第二票类型；金丝雀跌破门槛自动退白名单（演练）；四个门改造后 `go test ./corelib/intent/... ./guiapp/...` 零回退。

---

## 10. 关键文件索引

| 关注点 | 位置 |
|---|---|
| UIC 主分类 / cache / tombstone 新增 | `corelib/intent/classifier.go:229`、`:940-955`、`:956-1006` |
| L2 质心 / 阈值 | `corelib/intent/layer2.go:260-307,340` |
| 收养白名单（6 读类） | `guiapp/semantic_late_tree.go:33,64-75` |
| 写工具 fail-closed | `corelib/tool/router.go:142-146,1230,1511-1532` |
| Layer=4 门改造面 | `guiapp/semantic_tool_routing.go:964,1026,1255-1259`；`guiapp/im_message_handler_workflow.go:701,703` |
| 熔断闸门 | `guiapp/llm_endpoint_failure_gate.go:14,39,62,113`（加 category + `budgetFired`） |
| 轻量请求栈（P0-3） | `guiapp/llm_request_helper.go:58-79`；`guiapp/app_embedding.go:915,946,976,963-994` |
| 确认闸（P0-4 复用） | `guiapp/im_confirmation_store.go:12`；`guiapp/im_confirmation_gate.go:19-20,35-48,56-118,454`；唤醒模式借鉴 `corelib/security/approval_flow.go`（RequestApproval resultCh） |
| pending 绑定漏洞（顺带修） | `guiapp/im_entry_context.go:193`；`guiapp/im_pending_reply.go:299-303,369` |
| 现有秘密检测（漏检基线） | `corelib/security/sensitive_detector.go:32` |
| 前端消息通道（P0-1） | `guiapp/frontend/useAIAssistant.ts:4616-4751`（:4653 丢弃逻辑）；先例 `app_wails_bindings.go:3214` |
| 结果绑定 | `guiapp/im_entry_execution.go:217-246` |
| 校准 | `corelib/intent/calibration.go:77`；用例 `calibration_cases.go:13` |
| 嵌入 / MRL | `corelib/embedding/embedder.go:42`；`gemma.go`；`init.go:9-12` |
| 数据闭环先例 | `corelib/needledata/`；`cmd/maclaw-needle export-needle` |
| 后台链范式（回标/重训触发） | `guiapp/im_history_persistence.go:323-338`；priority `llm_concurrency_scheduler.go:434-449` |
| P0 新增 | `guiapp/im_degraded_turn_recovery.go`；凭据栅栏 + 唤醒通道；凭据卡片组件；`corelib/security/` 扫描器升级 |
| 路线 B 新增 | `corelib/intent/head.go`；`corelib/intentdata/`；`scripts/intent_head_train.py` |
