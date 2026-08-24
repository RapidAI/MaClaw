# 语义工具路由：事实层、协议入口与回合收口

状态：设计修订（第六轮）。挂在 [统一语义工具路由设计](semantic-tool-routing-design-zh.md) 与 [文档生成语义切片](document-generate-semantic-slice-zh.md) 之下，不另起规划器。循环入口对规划器未命中的处理见 [兜底工具决策](semantic-routing-miss-fallback-zh.md)。

读者：语义路由、共享 Agent 循环、UIC、桌面 IM 入口。实现与回归必须同时满足父文档 §2.3、文档切片已冻结闸门，以及本文 §0 / §0.1 / §0.2 / §0.3 / §0.4 / §0.5。规划器仍可按 §5 报 unmet。循环入口：`policy_denied` / 确认 / grant 冲突 / `workflow_task` 停轮；其余未命中走有界兜底，见兜底文档。

---

## 0. 相对初稿的修订

| 严重度 | 初稿问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | 「HostReject 只留给已确认能力」放走 `Degraded` 未映射族 | 未映射 capability **一律** `coverage_unmet` |
| P0 | 另起四事实本体 | 认识态 / 感知 / 意图文本是 Fact。Intake 在 Authorizer 前。收口在 PlanExecutor |
| P0 | 「丢掉 dest 再搜」像吞投递 | 丢掉 ≠ 补 deliver、≠ 改 DestinationID |
| P1 | 「词法不得覆盖 confirmed」废掉天气+PDF | 词法可在 lookup 上提交已声明复合 |
| P1 | 「唯一 live grant 就代劳」 | 仅 `generate_pdf`、`semantic_deliver_current_file` |
| P1 | 「GUI 禁止再维护过滤器」 | 一套 marker、两个 sink。Codex/XML 可留块后文本 |

---

## 0.1 第二轮评审修订

| 严重度 | 上一版问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | 用 `unknown` 编码 hint，与 hint 门互相打架 | **废止塌缩。** Primary 保留家族，另标认识态 |
| P0 | 第 6 门「只计划 search」像裁掉 generate | 第 6 门只批准 lookup **半边** |
| P0 | 未重申 L2 跨族不得早退 | lookup 自信且无 generate runner-up ≥ 0.70 才早退 |
| P1 | 无流水线顺序 | 剥意图 → UIC → 认识态 → 感知 → 词法 → §5 → Planner |
| P1 | 无路径无字节的「这张图」被写成可搜网 | 保持 `file_read` |
| P1 | Intake 消耗与 Admission 消耗糊在一起 | 见 §7.0 |

---

## 0.2 第三轮评审修订

第二轮「保留 L2 Primary」若原样落地，会比今天更差。本表取代「词法永不清 Degraded」和「hint 等于无其它标签」的整句用法。

| 严重度 | 第二轮问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | L3 失败 / skip-tree 后若既保留 `live_data` 又留下 L2/affordance 的 `document_generate`，现网 `semanticReadOnlyLookupFamily` 为假，`Degraded` 复合走进「classification unavailable」HostReject。天气+PDF 在树超时后会比塌成 unknown 再词法提交更坏。 | **hint 路径禁止 `applyExecutionAffordances`。** L3 失败或 skip-tree 时丢掉一切未确认的可变 Secondary，只留 lookup 半边为 hint。generate 只能由词法按已声明复合重新提交。 |
| P0 | 第二轮写「词法不得把结果改成 confirmed」，没写清现网词法在 **lookup 锚定** 时会 `Degraded=false` 并抬分到 0.78。有人按字面留下 Degraded，复合仍 HostReject。 | 词法 lookup 锚定提交 **可以** 清 Degraded、把分数抬到 `imSemanticMinimumConfidence`（0.78）。这是过第 6/8 门的现网契约，不是 UIC `confirmed`。裸 generate 仍不许清 Degraded。 |
| P0 | 第 6 门只写 floor=0.70，没写 resolver 的 0.78。实现会把 hint 0.70 直接丢给 `MinimumConfidence=0.78` 的 resolver 再 fail-close。 | 两个常数分开。`EmbeddingLookupMinScore`（0.70）= hint 能否进第 6 门。`imSemanticMinimumConfidence`（0.78）= resolver。过第 6 门后只允许 **规划投影** 抬分（现 `semanticLookupClassificationForPlanning`），存盘的 UIC 分数不变。 |
| P1 | `read_only_hint` 一律缓存，会把 L3 超时的 hint 当成稳定路由。 | 认识态之外必须有来源：`skip_tree` 可缓存；`l3_timeout` / 取消 / 供给中断不可缓存。二者都可以是 hint。 |
| P1 | 要求立刻给 `ClassificationResult` 加字段，缓存形状和日志会大面积改。 | 阶段 1 先派生：`(Primary, score, skipTree, l3Failed)` → 认识态。加字段必须 bump 缓存 epoch。 |
| P1 | 未写 SessionGovernedTask。hint 若被当成已授权 need 持久化，「继续」会重放一次没批准的 search。 | 只持久化 planner **已经 granted** 的 needs。hint 不是 granted。 |
| P1 | L3 超时若仍塌成 unknown，无词法就没有 lookup；若保留 hint 且分数已过 0.70，则不必再等词法。这是相对现网的行为变化，必须写死。 | L3 超时 + hint 且分数 ≥ 0.70：**直接批准 lookup 半边**。词法只负责抬起 skip-tree 低于 0.70 的分，以及重新提交 generate。skip-tree 按政策分数 < 0.70，因此永远不能单靠第 6 门出 lookup。 |
| P2 | 宿主证据未排除失败正文 | lookup 证据不得含 `[system rejected]`、`[file_base64]`、malformed 文案 |


---

## 0.3 第四轮评审修订

第三轮把「废止塌缩」和「恰好一个 live grant」写得过宽。按字面落地会 HostReject 非 lookup 的树失败，也会让天气+PDF+投递在 search 消耗后因「还有第二个 grant」而不出文件。

| 严重度 | 第三轮问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | 「废止塌缩」读成对所有 L2 Primary 生效。L3 失败的 `file_read` / `screenshot` / `coding` 若保留 Primary 且 `Degraded`，现网第 8 门或 C-2 会 HostReject。今天这些路径塌成 unknown 再聊天或 fail-closed，比「保留 Primary」更安全。 | **只对 lookup 半边废止塌缩。** skip-tree / L3 失败且 Primary ∈ {search, live_data}：保留该 Primary 为 hint，丢掉一切非 lookup 标签。其它家族仍塌成 unknown（或走现有 fail-closed），不得标 hint。 |
| P0 | §8「恰好一个 live grant」把整轮授权面当成一个槽。现网 `soleLiveSemanticGrantByAdapter` 只要求**该 adapter** 唯一 live。search 消耗后若 generate 与 current-channel deliver 同时 live，按整轮唯一则两步都 no-op。 | 尾部是**有序白名单**：先 `generate_pdf`，再 `semantic_deliver_current_file`。每步只要求该 adapter 恰好一个 live grant，且本步依赖满足。其它家族的 live grant 不阻断。不代劳未消耗的 search。 |
| P0 | hint ≥ 0.70 过第 6 门后，若把原分类（仍 `Degraded`）交给 resolver，会被 0.78 门 fail-close。 | 第 6 门之后必须走规划投影：`Degraded=false` 且分数 ≥ 0.78。存盘 / 日志 / 重规划仍用原文。词法未跑时也走这条投影。 |
| P1 | 未写 fusion。`fusionToClassification` 已经保留 L2 Primary + `Degraded`，与 `ClassifyContext` 的 unknown 塌缩不是同一条路。 | 阶段 1 只改 `ClassifyContext` 的 lookup-hint 出口。不把 fusion 再塌成 unknown，也不把 fusion 的 Degraded `coding` 当 hint。 |
| P1 | L3 失败只写「丢掉 generate Secondary」。残留 `workflow_task` / `coding` / `office` / `document_delivery` 会 coverage_unmet。 | lookup-hint 出口的 Secondary **必须清空**。未声明复合对里的标签一律丢。generate 只许词法再提交。 |
| P1 | 缓存命中后若再跑 affordance，hint `live_data` 会被 PDF 标记加成复合并 HostReject。 | 缓存读出的 hint **禁止** `applyExecutionAffordances`。 |
| P2 | 模型没搜、助手只有「将生成 PDF」时，宿主无证据且正文 < 80 字会 no-op。 | 这是正确失败。宿主不补 search。先靠阶段 2 Intake 让 search 发生。 |


---

## 0.4 第五轮评审修订

第四轮修好了「对谁废止塌缩」和「按 adapter 收口」，但没写第 7 门之后的遗留面。`handled=false` 会进入 `prepareAgentLoopTools`。遗留路由器把 `Primary != unknown` 且分数 ≥ 0.50 当成可激活：按 `ToolNames` 钉条件工具，并按 `live_data` 收窄技能为 weather/web。hint 若带着 `live_data` + `affinity.Resolve` 进去，「北京天所」会从 HostReject 变成乱搜。

| 严重度 | 第四轮问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | 事实层 Primary=`live_data` 被同一份结构体交给遗留路由器。`uicResultUsableForToolActivation` 只排除 unknown/ambiguous，**不看 Degraded**。门槛是 0.50。 | **两份视图。** 事实视图给 §5 / 词法 / 缓存 / 日志。第 7 门（以及一切 `handled=false`）交给遗留面的必须是**聊天投影**：`Primary=unknown`、无能力标签、`ToolNames` 空。不改路由器 0.50 门槛，也不把所有 Degraded 标成 unusable（fusion 降级的 ssh 仍要能钉工具）。 |
| P0 | hint 出口若 `affinity.Resolve(live_data)`，即使有人记得第 7 门，其它读者（技能约束、日志、误用 ToolNames）仍会当确认查找。 | hint / skip-tree / L3 失败的 lookup 出口：`ToolNames=nil`，`WorkflowType=""`，**禁止** `affinity.Resolve`。 |
| P1 | 写 skip_tree「可缓存」，但 `cacheAndLog` 从不存 `Degraded`。有人会改 `cacheAndLog` 放行全部 Degraded，把 `l3_timeout` 也缓存。 | skip-tree **继续显式** `cache.Store`。不要改 `cacheAndLog` 的 Degraded 禁令。 |
| P1 | `WorkflowType` 若从 L2 残留，词法 `lexicalLiveDataLookupAllowed` 直接拒绝，天气+PDF 锚不住。 | lookup-hint 出口清空 `WorkflowType`。 |
| P2 | 四张修订表盖住规范正文 | 实现以 §4.5 / §5 / §8 为准；§0.x 只解释为什么。 |


---

## 0.5 第六轮评审修订

第五轮的聊天投影只写到了 `routeTools`。现网 `imSemanticIntentIsManaged` **不看** Degraded / 分数：只要 Primary 是 `live_data` 就是托管族。`LoopContext.SemanticIntent` 在分类时就写上了，第 7 门若仍挂事实视图，会出现：

- dispatcher 按 managed **绕过 strangler**，即使本轮没有 SemanticSurface；
- `loopContextBlocksLegacyToolRouter` 为真，`prepareAgentLoopTools` 直接返回空工具表；
- 若把投影提前到 continuation 之前，`isGenericContinuationPrimary(unknown)` 为真，上一轮 PDF 会被「北京天所」重放。

| 严重度 | 第五轮问题 | 修订后的契约 |
| --- | --- | --- |
| P0 | 「LoopContext 可继续挂事实」让第 7 门之后的 dispatcher / managed 谓词看见 `live_data`。 | 第 7 门必须**回写**聊天投影到 `LoopContext.SemanticIntent`。此后 dispatcher、prompt、遗留面只读投影。不改 `imSemanticIntentIsManaged`。 |
| P0 | 未规定投影时机。分类阶段就写成 unknown，continuation 会把 hint 当成 generic。 | 顺序：UIC 事实 → 词法 → **SessionGovernedContinuation** → §5。hint `live_data` 不是 generic，不重放。只有第 7 门之后才投影。缓存仍存事实。 |
| P1 | 第 6 门 hint≥0.70 应保持事实（托管 + 规划投影），与第 7 门回写相反，文档没对照。 | 第 6 门：LoopContext 保持 lookup 事实，走语义面。第 7 门：回写 unknown。两门不得共用一个「事后一律投影」。 |
| P2 | 实现按五张修订表拼规范 | 以 §4.0 / §4.4 / §5 为准。 |

---

## 1. 决策摘要

授权墙不动。改墙前的事实与协议，改墙后的执行收口。

```text
剥后的意图文本
  → UIC（标签 + 分数；hint 路径不加 affordance）
  → 派生认识态（不把 hint 改成 unknown）
  → 感知（字节 / 路径 / 失败加载）
  → 词法提交（lookup 锚定可清 Degraded）
  → §5 路由门（按半边）
  → CapabilityNeed → ToolPlan → Grant
  → 模型输出 → Intake → Admission
  → 宿主计划尾部
```

两个分数门槛不得混用。hint 不是 unknown，也不是 confirmed。

---

## 2. 在父文档里的位置

| 本层 | 对应父文档 | 可以做 | 不可以做 |
| --- | --- | --- | --- |
| 意图 / 感知 / 认识态 | Fact | 给 resolver 可验证输入 | 填工具名、`must_include` |
| 词法提交 | 改 Labels 作为 Need 证据 | 已声明复合；lookup 锚定清 Degraded | 覆盖硬策略；裸 generate 洗白 |
| Tool Intake | Authorizer 之前 | 收方言、丢掉 search 装饰字段 | 改 Grant / dest / 发明 deliver |
| Admission | Authorizer + Journal | 闭 schema、一锤子 | 按函数名合成身份 |
| 计划尾部 | PlanExecutor / attach | 执行已 live 的 generate/deliver | 新增 selection、扩权、换渠道 |

工作流闸门、VE、不发明 dest：沿用文档切片。

---

## 3. 故障与现网编码

| 现象 | 层 | 现网临时编码 |
| --- | --- | --- |
| 「北京天所」HostReject | hint 当确认能力 | skip-tree **塌成 unknown** |
| 「杭州天气，生成 pdf」无文件 | 尾部交给 Flash | `flushHostOwnedDocumentGenerate` |
| 气泡 DSML | marker 两套 | `HoldContentToolCallStream` |
| DSML 无 search | `count` 烧 grant | content 只留 `query` |
| `weather-report.jpg` 毁 lookup | 路径进意图 | 多点剥皮 |

阶段 1 用显式 hint 替换「塌成 unknown」。产品出口必须不变：「北京天所」聊天，「北京天气」仍能查。现网测试若断言 skip-tree 的 `Primary==unknown`，应改成断言 hint + 第 7 门，而不是当回归失败。

---

## 4. 事实模型

### 4.0 流水线

1. 入口一次抽出意图文本与 `PickerPaths`。
2. UIC 只看意图文本，产出**事实视图**。**仅 `confirmed` 早退或 L3 成功时** 才 `applyExecutionAffordances`。lookup 的 hint / skip-tree / L3 失败：不加 affordance，Secondary / ToolNames / WorkflowType 清空，Primary 留 search/live_data，禁止 affinity.Resolve。非 lookup 仍塌 unknown。
3. 派生认识态。不把 hint 改成 unknown。
4. 填 `HasImageBytes`、`PickerLoadFailed`。
5. 词法只读意图文本 + 感知。
6. `applySessionGovernedContinuation` 读事实视图。hint lookup **不是** generic，不重放上一轮 generate/deliver。
7. §5 读词法与 continuation 之后的分类。
8. 第 7 门**回写**聊天投影到 `LoopContext.SemanticIntent`。dispatcher / prompt / `prepareAgentLoopTools` 只读这份。第 6 门不回写。
9. Planner。SessionGovernedTask 只记 granted needs。

### 4.1 意图文本

剥 picker、视觉说明、OCR。UIC / 词法 / embedding / prompt / 技能 / PDF 标题只读它。`msg.Text` 只留给附件 materialize。

### 4.2 感知

| 字段 | 真值 | 不是 |
| --- | --- | --- |
| `HasImageBytes` | 非空图像字节 | 路径 |
| `PickerPaths` | 入口一次抽出的路径 | 已看见照片 |
| `PickerLoadFailed` | 有路径无字节 | 用户要读图 |

有字节才视觉 fallthrough，并压住 lookup 词法。  
路径只用于 light→full，以及与天气/PDF 标记一起释放 `file_read` hold。  
无字节无路径：保持 `file_read`。失败加载：lookup/generate 必须能活。

### 4.3 认识态

派生量，不替换 `Primary` / `Labels()`。阶段 1 不必改结构体。

```text
unknown          没有任何能力家族
read_only_hint   Primary ∈ {search, live_data}，且没有未确认的可变标签
confirmed        现有自信规则成立，且无未处理的跨族 runner-up
```

来源（决定缓存，不是第三套能力）：

```text
skip_tree     短句政策跳过 L3，稳定
l3_timeout    已升树但失败/超时，不稳定
l3_ok / l2_confident   确认路径
cancelled / outage
```

赋值（词法之前）：

1. L2 达 lookup/strict 自信，**且** 无 `document_generate` runner-up ≥ 0.70 → `confirmed`，可早退，可跑 affordance。
2. L2 Primary ∈ {search, live_data}、无其它能力标签、分数 < 0.70 → 保留 Primary，`read_only_hint`，Secondary 清空。短句可 `skip_tree`。**不得改成 unknown。**
3. lookup + 跨族 generate runner-up → 不得 lookup 早退；升 L3 或写入 Secondary。L3 成功 → `confirmed`（可带 generate，可跑 affordance）。L3 失败 → Primary 留 lookup，**Secondary 清空**，hint、来源 `l3_timeout`。
4. L2/L3 失败且 Primary **不是** search/live_data → **仍塌成 unknown**（今天的 conservative unknown）。不得把 `file_read` / `screenshot` / `coding` 当 hint。
5. 取消 → cancelled unknown。
6. 词法不把 Epistemic 改成 `confirmed`。lookup 锚定提交只清 `Degraded`、抬规划分数。`Degraded=false` ≠ UIC confirmed。
7. `ClassifyContext` 的 lookup-hint 出口与 fusion 分开。fusion 已经保留 Primary+Degraded 的，不要再塌；也不要把 fusion 的 Degraded 非 lookup 当 hint。

词法之后按半边看：lookup 用 Epistemic + 分数；generate 用「是否已声明复合且 lookup 已锚定」。不要用词法前的「hint = 无其它标签」否决复合。

缓存：

| 结果 | 缓存 |
| --- | --- |
| `confirmed` | 是 |
| `read_only_hint` + `skip_tree` | 是 |
| `read_only_hint` + `l3_timeout` / 取消 / 中断 | 否 |
| 其它 Degraded | 否 |

加 `Epistemic` 字段时必须 bump 缓存 epoch。

### 4.4 两份视图

同一份 `ClassificationResult` 不能既当 §5 的 lookup 事实，又当遗留路由器的激活信号。

| 视图 | 读者 | 形状 |
| --- | --- | --- |
| 事实 | §5、词法、缓存、日志、规划投影、SessionGovernedTask 之前 | Primary ∈ {search, live_data}，认识态 hint，`Degraded=true`，Secondary/ToolNames/WorkflowType 空 |
| 聊天投影 | 仅 `handled=false` 之后的 `prepareAgentLoopTools` / `routeTools` / `skillCapabilityConstraintForUIC` | `Primary=unknown`，分数 0.30，无能力标签，ToolNames 空 |

第 6 门 `handled=true`：LoopContext 保持 lookup 事实，走语义面，不投影。第 7 门：回写聊天投影到 LoopContext，再 `handled=false`。日志可以另打事实 Reason，不得把事实 Primary 留给 dispatcher。`imSemanticIntentIsManaged` 不改：hint `live_data` 对它为真，所以必须靠回写，不能靠「挂在 context 里无人读」。

`cacheAndLog` 继续拒绝所有 Degraded。skip-tree 的事实视图用现有显式 `cache.Store`；禁止为了 hint 放宽 `cacheAndLog`。

### 4.5 授权面

live/retired grants、schema、artifact 依赖。Intake 与收口只读。

---

## 5. 路由门

读**词法之后**的分类。认识态不放宽 coverage。

两个门槛：

| 常数 | 值 | 用途 |
| --- | --- | --- |
| `EmbeddingLookupMinScore` | 0.70 | hint 能否过第 6 门 |
| `imSemanticMinimumConfidence` | 0.78 | resolver / 词法抬分目标 |

过第 6 门的 hint 用规划投影：Degraded=false 且分数 ≥ 0.78；UIC 原文不变。

| 次序 | 条件 | 出口 |
| --- | --- | --- |
| 1 | 未映射 capability | HostReject `coverage_unmet` |
| 2 | runtime dial 撤回 | 同上 |
| 3 | 有字节且 staged-image-understand | 视觉，不 mint search/PDF |
| 4 | `WorkflowAgentLoop` ∧ `document_generate` | 整轮不 managed |
| 5 | `attachment_delivery` ∧ `document_generate` | HostReject 冲突 |
| 6 | lookup 半边 `confirmed`，或 hint 且分数 ≥ 0.70 | 批准 lookup materialize。已声明 generate 复合继续展开 |
| 7 | lookup 家族（可 hint）且 < 0.70，且无已锚定 generate 复合 | 聊天，不 HostReject。回写聊天投影到 LoopContext 后再 `handled=false`。dispatcher 不得再看见 live_data，遗留面不得钉 search |
| 8 | 已映射可变，且 (Degraded 或 L2/hint < 0.78)，且不是树确认（L3/23 ∧ !Degraded ∧ ≥ 0.70），也不是「第 6 门已批 lookup」 | leftover miss，不是 HostReject。0.78 只卡 resolver 与 L2 可变授权。树确认后规划投影再进 resolver。裸降级 generate / 树 < 0.70（「图上有什么」0.55）仍未命中 |
| 9 | 仅 generic | 聊天 |
| 10 | 已映射且 needs 空 | HostReject |
| 11 | VE 上的 generate/file deliver | unmet |

「北京天所」：skip-tree hint 0.61 → 第 7 门。  
「北京天气」：L2 已确认则第 6 门；skip-tree 低分必须词法锚定后才过第 6 门。  
「杭州气温如何」若 L3 超时且 L2 live_data 0.74：无词法也过第 6 门（只批 lookup）。  
「杭州天气，生成 pdf」：L3 超时先丢掉 generate Secondary；词法再锚定 lookup+generate 并清 Degraded → 第 6 门 + 复合展开。不得靠 L2 残留 generate 过第 8 门。  
裸降级 generate：第 8 门 fail-closed。
「清空当前目录」L3 tree-after-embedding `shell_command` 0.75：树已是路由权威，不得再用 0.78 当未命中。规划投影后发封闭 bash。同句 L2 0.75 未上树仍未命中。树 < 0.70（含未标 Degraded 的 0.55）与 Degraded Layer 3（超时 / skip-tree）仍聊天。聊天投影与执行档案必须用同一条树确认谓词，不得再单独套 0.78。


阶段 1 必须拆开 `semanticReadOnlyLookupFamily`：它一票否决可变标签，天气+PDF 不能再走这个整包谓词。改成 lookup 半边谓词 + 复合谓词。

---

## 6. 词法提交

改 Labels，供 resolver 展开。随后走 §5。

允许：

- `unknown` / hint 上提交天气/股价/航班 → `live_data`；「网上搜」→ `search`。lookup 锚定则清 Degraded、分数至少 0.78。
- lookup 的 hint/confirmed 上提交 `document_generate`（仅已声明复合）。同样 lookup 锚定则清 Degraded。
- 有 `PickerPaths` + 天气/PDF + 无字节：释放 hold，再跑上两条。

禁止：

- 覆盖有字节时的 `file_read` / `screenshot` / `document_read`。
- 无路径无字节时为天气释放 `file_read`。
- write / fetch / clock / 指定 dest → search。
- 抢 screenshot / coding 等其它 `confirmed` 主标签。
- 裸 generate 清 Degraded。
- 给 VE / 未发布渠道补 generate 或 dest。

---

## 7. Tool Intake

### 7.0 谁消耗 grant

| 情况 | 消耗？ |
| --- | --- |
| content 有 `query`，装饰已洗掉，Admission 成功 | 是 |
| 洗完无 `query` / malformed，未进 Admission | 否 |
| 结构化带 `max_results` / dest，Admission 失败 | 是 |
| grant 已被模型消耗 | 宿主 no-op |

阶段 2 不把「Admission 失败不消耗」列入范围。

### 7.1–7.2 解析与字段

结构化 `tool_calls` 不洗。DSML / Codex / `<tool_call>` / 裸 JSON 走 `ParseContentToolCallsDetailed`。

`web_search` 且 `query` 非空时：保留 `query`；丢掉 `count` / `max_results` / `num_results` / `top_k` / `limit`；丢掉 dest 类字段且不补 deliver；其它未知键 content 丢掉。无 `query` 不进 Admission。

### 7.3 展示

与解析共用 marker（全角 `｜`、`function_calls`、`parameter`）。部分 DSML 在 flush 丢掉。散文 `2 <` 发出。Codex/XML 可留块后文本。

---

## 8. 宿主计划尾部

白名单：`generate_pdf`、`semantic_deliver_current_file`。

还须：回合已结束；非交互暂停；非须保留的错误；该 adapter 恰好一个 live grant（该 adapter 已消耗或不唯一则本步 no-op）；先 generate 再 current-channel deliver；不代劳 search；本 revision 已 materialize；依赖满足；参数只有 `{content,title}` 或 `{}`；新 CallID、同一 `Grant.Token`；渠道已发布；非 VE。

generate 正文：**优先**受信 lookup 证据（不得含 `[system rejected]` / `[file_base64]` / malformed）；否则用 `StripXMLToolCalls` 之后、≥80 字且 `ValidatePDFContent` 通过的助手正文。

失败不重试、不换 adapter、不回落 `[file_base64]`。仅文件已附上且错误是陈旧 LLM 超时时清 `Error`。

---

## 9. 阶段

| 阶段 | 改什么 | 不改什么 |
| --- | --- | --- |
| 0 | 本文当清单 | 代码 |
| 1 | 仅 `ClassifyContext` 事实视图；continuation 先于投影；第 7 门回写 LoopContext；skip-tree 显式缓存；禁 affordance/Resolve；非 lookup 仍塌 unknown；§5 半边门 + 规划投影 | unmapped HostReject、工作流闸门、L2 跨族不得早退、Admission 消耗 |
| 2 | Intake + 共享 marker | 结构化闭 schema |
| 3 | 白名单收口 | 规划器、dest、VE |
| 4 | 意图文本只剥一次 | 用 `msg.Text` 分类 |

先 0+1，再 2。阶段 1 必须同时绿：「北京天所」聊天，且 `Primary` 可以是 `live_data`+hint 而不是 unknown。

---

## 10. 验收

1. `weather-report.jpg` + 「北京天气」→ lookup。
2. 有字节 + 「这张图里的天气」→ 视觉，不 mint search/PDF。
3. 选图失败 + 「北京天气」→ lookup。
4. 无路径无字节「这张图里的天气如何」→ `file_read`。
5. 「北京天所」→ 聊天；显式 hint 后仍不 HostReject。
6. 「北京天气」在 skip-tree / L3 超时后仍 lookup。
14. L3 超时 + live_data hint ≥ 0.70 且无天气/PDF 词法：只计划 lookup；不 HostReject；不 mint generate。
7. 「杭州天气，生成pdf报告」在 L3 超时后仍能出 PDF：不得靠 L2 残留 generate 过门，必须词法锚定；DSML `count` → search → 宿主附文件；气泡无 DSML；L2 不得独自裁掉 generate。
8. 结构化 `max_results` 仍拒且消耗 grant。
9. `Degraded` 未映射 `coding` 仍 HostReject。
10. 工作流 + `document_generate` 整轮不 managed。
11. 暂停不消耗 generate；模型已调用则宿主 no-op；裸降级 generate fail-closed。
12. hint+`l3_timeout` 不缓存；hint+`skip_tree` 可缓存。
13. 「继续」不重放未 granted 的 hint search。
15. L3 超时的 `file_read` / `screenshot` 仍塌 unknown，不 HostReject、不当 hint。
16. search 已消耗且 generate+deliver 同时 live：仍先出 PDF，再尝试当前渠道投递；不因「整轮不止一个 grant」 no-op。
17. 缓存命中的 hint 不再跑 affordance。
18. 「北京天所」不 HostReject、遗留面不钉 `web_search`、不按 weather 收窄技能。事实视图可以是 `live_data`+hint。
19. hint 出口 `ToolNames` 与 `WorkflowType` 为空；`cacheAndLog` 仍不存 Degraded。
20. 上一轮刚出过 PDF 后发「北京天所」：continuation 不重放 generate；dispatcher 不按 managed 绕过 strangler。
21. 第 6 门的 hint≥0.70：LoopContext 仍是 live_data，走共享循环 + 语义面。

重建二进制之前物理桌面不算验收。

---

## 11. 非目标

1. 不另起规划器。
2. 不放宽 C-2。
3. 不改一锤子；不把 Admission 失败改成不消耗。
4. 不把 PDF/write/fetch/clock 映射成 search。
5. 不发明 dest，不为 VE 发布 generate/file deliver。
6. 不缓存 `l3_timeout`。
7. 不让路径/OCR 成为视觉或天气事实。
8. 不代劳白名单外 grant。
9. 阶段 1 不强制改 `ClassificationResult` 线格式。
10. 不把非 lookup 的 L3 失败改成保留 Primary。
11. 不改 fusion 的 Verdict/Degraded 语义来套 hint。
12. 宿主不代劳未消耗的 search，也不把「整轮唯一 grant」当前提。
13. 不改遗留路由器 0.50 激活门槛，不把全部 Degraded 标成对路由器不可用。
14. 不改 `cacheAndLog` 禁存 Degraded。
15. 不改 `imSemanticIntentIsManaged`的「有规则即 managed」。
16. 不把聊天投影提前到 continuation 之前。

---

## 12. 一句话

hint 事实只供词法、continuation 和第 6 门使用。第 7 门必须把 LoopContext 回写成 unknown，否则 dispatcher 会把打错当成已托管回合。宿主按 adapter 顺序收口。
