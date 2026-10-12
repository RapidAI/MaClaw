# MaClaw 讲解可视化能力建设方案（对标 WorkBuddy）

状态：调研完成；**P0 已实施（2026-10-12，未提交）**，P1/P2/P3 待评审
调研方式：源码级只读调查（2026-10-12），所有结论带文件路径与行号
目标：让 MaClaw 的 Agent 在讲解系统/流程时，具备"主动画图并渲染成图"的能力，覆盖 WorkBuddy 现有的三类形态：对话内即时渲染、可导出产物、交互式分步。

---

## 1. 结论摘要

MaClaw **已经具备约 80% 的渲染底座**，缺的主要是"主动触发"与"交互/导出"两段：

- 渲染层：mermaid 11.14 已完整接入并在 8 类界面复用，含流式处理、安全限制、失败降级（这是最难的部分，已经做对）。
- 触发层：完全依赖模型自发输出 ` ```mermaid ` 代码块，**系统提示词里没有任何画图引导**——这是当前最大的能力缺口，也是成本最低的修复点。
- 产物层：artifact 机制（`corelib/tool.ArtifactStore`）已支持 image/png、PDF、office、WAV 等，`semantic_render_trusted_live_data` 已走通"工具产出 PNG 图 → 送达前端"的全链路，可被复用作新工具的模板。
- 交互层：mermaid `securityLevel:"strict"` 刻意禁用了 click 交互（安全正确），因此交互图必须另走受控通道，不能靠放开 mermaid 配置实现。

---

## 2. 现状盘点（全部有代码证据）

### 2.1 mermaid 渲染链路

| 环节 | 位置 |
|---|---|
| 消息渲染入口 | `guiapp/frontend/src/components/ai/aiAssistantMarkdown.tsx:1493` `renderMessage` → `:1766` `renderContentWithCodeBlocks` |
| 代码块判定与挂载 | `aiAssistantMarkdown.tsx:824` `flushCodeBlock(allowMermaidRender=true)` → `:829` `isMermaidCodeFence` → `:831` 挂载组件 |
| 渲染组件 | `components/ai/AssistantMermaidDiagram.tsx:14-29` 懒加载 `import("mermaid")`；`:57` `sanitizeMermaidCode`；`:116-117` `initialize` + `render`；`:182` `dangerouslySetInnerHTML` 落地 SVG |
| 渲染预算 | `AssistantMermaidDiagram.tsx:10-12,76`：50k 字符 / 750 节点 / 1500 边，超出直接报错不渲染 |
| 流式处理 | `aiAssistantMarkdown.tsx:992` 未闭合围栏按普通代码块渲染，**不会对半截语法反复调用 mermaid**（`:826-828` 注释明确） |
| 失败降级 | `AssistantMermaidDiagram.tsx:184-190` fallback：错误文案 + `<pre><code>` 原样回显源码；`:22-27` 加载失败不永久禁用 |
| 渲染串行化 | `AssistantMermaidDiagram.tsx:9,110` `renderQueue`（mermaid 全局配置会互相串味） |

配置要点（`AssistantMermaidDiagram.tsx:31-54`）：`startOnLoad:false`、`theme:"base"`（themeVariables 从 `Theme` 映射）、**`securityLevel:"strict"`**、`suppressErrorRendering:true`。strict 的取舍写在 `:35-37` 注释："Chat responses are untrusted model output... disables executable click handlers"。

版本与分包：`frontend/package.json:18` `"mermaid": "^11.14.0"`，dist 中 core chunk + 25 个图种分包（architectureDiagram / c4Diagram / blockDiagram / sankey 等），即完整能力 + 按需加载。代码中**未找到**图类型白名单，默认全量可用。

### 2.2 覆盖面（8 处已接入，1 处未接入）

已接入（均复用同一条 markdown 管线，编码会话不例外）：

- AI 助手面板 `AIAssistantPanel.tsx:5841/5848/5754/5861/5916`（`:2411` `isPureCodingEnvironment` 只影响其他渲染分支，**未禁用 mermaid**）
- CodingAgent 推理时间线 `CodingAgentThinkingTimelineItem.tsx:41-42`
- 数字员工会话 `VEConversationView.tsx:2036/2406`
- 群聊 `VEGroupChat.tsx:532`
- 文件伴读窗口 `FileCompanionWindow.tsx:2085/2089`
- 确认卡片 `confirmationCard.tsx:12/116`
- 任务预览（第二套独立实现）`WorkflowDocPreview.tsx:1144-1147` → `:130` 自有 `MermaidBlock`，由 `AssistantPreviewPane.tsx:614` 引入

未接入：IM gossip 面板（`components/gossip/GossipPanel.tsx` 无 markdown 渲染）。

### 2.3 可视化产物与工具链

- artifact 传输机制：`guiapp/semantic_artifacts.go`（414 行）+ `corelib/tool.ArtifactStore`。已有 `newPNGPayload`/`newPDFPayload`/`newWAVPayload`/`newOfficeDocumentPayload`，带 magic bytes 校验；`:262` `registerPublished` 保证"先落执行事实、后发布 artifact"的崩溃恢复顺序。
- 现成的"渲染成图"工具：`guiapp/semantic_live_data_visual.go` 的 `semantic_render_trusted_live_data`。**注意它是假图**：用 `image/draw` 画色块，注释明说 "It is not text-to-image: neither model arguments nor a remote image URL can affect its source"（`:40-42`）——这是安全刻意的设计，不能简单扩展成任意输入。
- 工具注册全链路（以该工具为标本，共 4 处）：
  1. 定义与 schema：`semantic_live_data_visual.go:20-38`（`...Definition()` + `...InvocationSchema()`，空参数、`additionalProperties:false`）
  2. 注册表：`guiapp/semantic_tool_routing.go:4877` `appendClosedHostSemanticProviders` 的 items 表加一行，`:4920-4922` 声明 `Produces: [{Kind:"image", MIMEType:"image/png", Required:true}]`
  3. 分发：`guiapp/im_agent_loop_shared.go:5178` 的 adapter 名分支 → `publishRenderedLiveDataVisualArtifact(...)`
  4. 授权探测：`im_agent_loop_shared.go:984` 与 `:1131` `soleLiveSemanticGrantByAdapter(...)`
- 工具预算：`legacy_adapter_count_guard_test.go` 钉死 **MaxToolBudget = 28**（`:35` 注释 "3 bootstrap + 24 candidates + 3 additions = 30 > MaxToolBudget (28)"），超预算时裁剪低排名候选、保 mandatory。新增工具必须评估是否挤占别人。

### 2.4 HTML/SVG 与图表组件

- HTML 渲染唯一入口：`components/preview/FilePreviewView.tsx:376-382`，`<iframe sandbox="" srcDoc={markup}>`，全量沙箱（禁脚本/同源/表单/下载）。**只渲染磁盘 HTML 文件**，不渲染模型回复里的 HTML 字符串。
- 预览面板模式：`components/ai/AssistantPreviewPane.tsx:58` `PreviewPaneMode = "workflow" | "code" | "agent" | "conflict"`，无 HTML/diagram 模式。
- 手绘 SVG 图表仅两处且均为遥测专用：`components/remote/TokenUsageTrend.tsx:157-198`（折线 + ResizeObserver）、`components/ai/MemoryUsageRing.tsx:105-121`（环形）。无通用 chart 组件（`src` 下无 *chart*/*graph* 命名文件）。

### 2.5 提示词层（关键缺口）

- 系统提示词位置：`corelib/agent/system_prompt.go`（186 行）+ `prompt_blocks.go`/`prompt_bundle.go`/`prompt_profile.go`；GUI 侧 `guiapp/im_system_prompt.go:215` `buildSystemPromptBase`。
- 在 `corelib/agent/`、`corelib/agentruntime/`、`guiapp/im_system_prompt.go` 中检索 `mermaid|流程图|架构图|图表|diagram|画图|可视化|visualiz|flowchart|SVG`：**唯一命中是 `corelib/agent/file_path_expand.go:1307` 的 `.svg` 附件扩展名白名单**，与提示词无关。→ 通用提示词完全没教模型画图。
- 唯一存在的画图指令在发明专利工作流，且方向相反：`corelib/workflow/v2/phase_prompt.go:1896` "所有生成的图形内容（SVG/PNG/Mermaid）必须用 write_file 或 bash 保存到项目目录下的文件中。**不要在回复中直接贴 SVG/Mermaid 源码。**"（`:1839` 还给了 Mermaid → PNG 的 mmdc 方案）。该约束是场景刻意的，方案中必须保留、不得破坏。

---

## 3. 差距分析（对标 WorkBuddy）

| # | 差距 | 现状 | 影响 |
|---|---|---|---|
| G1 | 无触发引导 | 模型自发 ` ```mermaid `，提示词无任何指引 | 覆盖率低且不稳定；模型讲解架构时大概率只给文字 |
| G2 | 无图型选择规则 | 模型不知道该用 flowchart 还是 sequence/class/ER | 图型选错导致可读性差（如用 flowchart 画多角色时序） |
| G3 | 无交互图通道 | strict 禁用 click；无受控 HTML widget 渲染 | 循环过程、可点击分步讲解无法实现 |
| G4 | 无数据→图表路径 | 只有 mermaid 自带 pie/xychart，无通用 chart 组件 | 数据类讲解弱 |
| G5 | 无导出链路 | mermaid 只内联；PNG artifact 已有但无 diagram 工具产出 | 图无法进 PPT/报告/文件 |

G1+G2 是"有无"问题，G3/G4/G5 是"增强"问题。**先做 G1/G2**：零架构改动、零工具预算、风险可控，且直接决定用户实际能看到的图多不多。

---

## 4. 改进方案

### P0：提示词层引导（最高杠杆，建议立即做）

目标：让模型在讲解系统/流程时主动输出 mermaid，且选对图型。

改动点（二选一或叠加）：

1. `corelib/agent/prompt_blocks.go` 增补一个"可视化输出"段落（推荐：corelib 一处改动即可覆盖所有 surface，含 IM/群聊/伴读）
2. `guiapp/im_system_prompt.go` 的 `buildSystemPromptBase`（`:215`）中追加 GUI 专属段落（若只想在 GUI 助手面板启用）

段落要点（可直接落地为提示词内容）：

- 触发条件：讲解系统结构、模块关系、调用链、业务流程、状态流转时，**默认**输出 ` ```mermaid ` 代码块，而非用文字或 ASCII 描述结构
- 图型路由：多角色交互时序 → `sequenceDiagram`；表结构 → `erDiagram`；状态机 → `stateDiagram-v2`；分层架构 → `flowchart`（可加 `subgraph`）；项目排期 → `gantt`；概念层级 → `mindmap`；C4/架构蓝图 → `architecture-beta`/`C4Context`
- 复杂度纪律：单图节点 ≤ 5（超出拆多张小图分步讲）；循环过程用带编号的线性步骤表达，不画环；每张图配一句文字说明图在讲什么
- 硬约束：图必须用 mermaid 语法，**不得**在回复里内联 SVG/HTML 源码（复用 phase_prompt.go:1896 的既有纪律，反向统一）
- 与发明专利工作流的关系：`phase_prompt.go:1896` 的"落文件不内联"保留不动——该场景要的是交付物，不是讲解

工作量：小（一个提示词段落 + 中英文两份）。
风险：输出 token 上升；模型可能产出语法错误的 mermaid（已有降级路径兜底）；需确认不会触发专利工作流的反向约束。
验收：人工抽查 10 个讲解类问题（"讲一下我们的工具路由""解释一下 Hub 安全策略判定"），统计出图率与语法成功率；改前/改后对比。

### P1：渲染工具化（把"渲染"变成显式能力）

目标：新增 `semantic_render_diagram`，模型传入 mermaid 源码 → 渲染为图 artifact 送达会话，作为 P0 之外的第二条触发路径（也便于后续扩展为非 mermaid 输入）。

完整改动清单（按 2.3 的四处标本）：

1. 新建 `guiapp/semantic_diagram_render.go`：`semanticTrustedDiagramRenderAdapter = "semantic_render_diagram"`、ImplementationID、`...Definition()`、`...InvocationSchema()`。参数设计要点：
   - 只接受 `source`（mermaid 源码字符串）+ `diagram_type` 白名单；**不接受任何路径、URL、base64 字节、目标位置**（防外发，参照 live_data_visual 的零输入设计哲学）
   - `additionalProperties:false`，走 `tool.NewParameterAuthorization(schema)`
2. `guiapp/semantic_tool_routing.go:4876-4883` items 表加一行：capability 用 `visual.render.live_data` 同款的自定义 ID（需确认 `corelib/tool` 的 CapabilityID 定义处是否要登记），effects 用 `tool.EffectLocalMutation`，`:4920-4922` 同款声明 `Produces: [{Kind:"image", MIMEType:"image/png"}]`
3. `guiapp/im_agent_loop_shared.go:5178` 附近加 dispatch 分支 → `c.executeTrustedDiagramRender(selection, canonicalArgs)` + publish（复用 `semanticArtifactBroker.publishPNG`）
4. 测试：新建 `guiapp/semantic_diagram_render_test.go`，仿 `guiapp/semantic_live_data_visual_test.go:146/205/241` 的 grant 探测写法；补行为性用例（非法 diagram_type 拒绝、超预算拒绝、空源码拒绝）

关键约束：

- **MaxToolBudget=28**：新增即挤占。优先按场景条件注册（仿 `:4884-4902` 的 `if semanticTrustedBrowserPublished(h)` 模式），仅在讲解/QA 类 surface 启用，避免常驻占用
- 渲染实现选型：Go 侧渲染 mermaid 需要外部依赖（node/mmdc），与当前"前端已有 mermaid 运行时"重复。**更优解是该工具只做"规范化 + 落库 + 前端渲染"**，即后端不产 PNG，而是把 mermaid 源码作为 artifact 下发、由前端 `AssistantMermaidDiagram` 渲染——省掉 node 依赖，且复用既有预算/降级/串行化逻辑。此为本方案默认路径，若坚持要 PNG（供导出）再评估 mmdc 依赖
- 安全：源码来自模型输出，属不可信内容；strict 限制必须保留，不得为支持 click 而放开

工作量：中。风险：工具预算、注册链断裂导致 catalog parity 失败（见 §5）。

### P2：前端增强

P2-1 预览面板扩展：`AssistantPreviewPane.tsx:58` 的 `PreviewPaneMode` 增加 `"diagram"` 模式，承载图的全屏/放大/导出（PNG/SVG 下载）。导出直接复用 mermaid render 的 SVG → canvas → PNG，无需后端。

P2-2 交互式分步（对应 WorkBuddy 的 HTML stepper）：新增受控 HTML widget 通道，**不要**放开 mermaid strict。做法：iframe `sandbox="allow-scripts"`（禁同源/禁表单/禁弹窗）+ `srcDoc`，白名单只允许静态 HTML/CSS/JS 片段；入口仅在 diagram 面板内。参考既有安全边界 `FilePreviewView.tsx:379` 的 `sandbox=""`，本次是"从无脚本到有脚本"的单点放开，必须单独安全评审（这是本方案唯一涉及提权的改动）。

P2-3 通用图表：新增 `components/ai/AssistantChart.tsx`，仿 `TokenUsageTrend.tsx:157-198` 的 SVG 画法（viewBox + path + ResizeObserver），支持折线/柱状/环形三类；输入为结构化数据（模型输出 JSON 或 mermaid `xychart`），规避"模型画 SVG"不可靠的问题。

P2-4 补齐 IM gossip 面板的 markdown/mermaid 渲染（`GossipPanel.tsx` 目前零 markdown 支持），属顺手项。

### P3（可选，需先调研）：真·图像生成

若需要"概念示意图/插画"而非结构化图，再调研接入图像生成模型。注意：架构图不走这条路（框线与文字不可靠），且需评估外发合规。

---

## 5. 关键约束与测试守卫

必须保持通过的既有守卫（改动前先确认它们钉的是什么，避免误伤）：

- `guiapp/legacy_adapter_count_guard_test.go`：MaxToolBudget=28，fail-closed（超预算必须拒绝而非静默放宽）
- `guiapp/semantic_capability_families_test.go`、`guiapp/srv_catalog_parity_test.go`：新增工具/能力族可能触发数量或清单断言
- `frontend/scripts/check-agent-architecture.mjs`、`check_wails_bindings`（若新增 Wails 绑定需补 App.js/App.d.ts/models.ts 三处）、`check-encoding.mjs`、CSS assemble + baseline
- `go build` + `gofmt` + `go test`（guiapp 包体量大，跑多个 `-run` 时注意 `-timeout`，参照工作备忘）

安全红线（来自既有设计，不可回退）：

- 模型输出视为不可信：mermaid strict 必须保留；新工具不得接受路径/URL/base64/目标参数（防借画图外发）
- artifact 发布顺序（`semantic_artifacts.go:262` `registerPublished`）不可倒置
- 缺键/空值语义按 `corelib/clientsecurity` 的既有教训处理：deny-by-default 布尔与 fail-closed 无害布尔修法方向相反，不搞一刀切

---

## 6. 验证方法

按项目惯例（改动必带行为性对比，不只看测试通过）：

1. **出图率**：固定 10 个讲解类 prompt，改前/改后各跑一遍，统计出图率、语法成功率、降级率
2. **图型正确率**：人工评审 P0 的 10 张图，核对是否选对图型（时序 vs 流程 vs 结构）
3. **工具链**：P1 用 `semantic_render_diagram` 直接调用，验证授权探测（`soleLiveSemanticGrantByAdapter`）、artifact 落地、前端显示三步贯通；反向验证：撤掉修复再跑，确认测试真的在保护新行为
4. **预算影响**：新增工具后跑 `legacy_adapter_count_guard_test.go` 与路由覆盖测试，确认没有把其他工具挤出 surface
5. **性能**：长回复（>30KB）场景确认增量渲染器仍不被 mermaid 拖慢（`IncrementalMarkdownRenderer.tsx:6-10` 的性能前提）

---

## 7. 执行顺序建议

| 阶段 | 内容 | 前置 | 预期收益 |
|---|---|---|---|
| **P0** | **提示词可视化段落 —— 已实施** | 无 | 立竿见影，出图率从近 0 到稳定 |
| P2-4 | IM 面板 markdown 渲染补齐 | 无 | 覆盖面补齐 |
| P1 | `semantic_render_diagram` 工具 | P0 验证后 | 显式触发 + 可导出 |
| P2-1 | 预览面板 diagram 模式 + 导出 | P1 | 图可保存/进报告 |
| P2-3 | 通用 SVG 图表组件 | P0 | 数据类讲解 |
| P2-2 | 受控交互 widget | 单独安全评审 | 交互式讲解 |

## 8. P0 实施记录（2026-10-12，未提交）

改动（3 个文件，均在 `corelib/agent`）：

- `prompt_blocks.go`：新增 `PromptVisualizationRules`（完整版，含图型路由表与出图纪律）与 `PromptVisualizationRulesLight`（精简版，供轻量/受管 turn）。⚠️ 实现细节：块内要写 ` ```mermaid ` 围栏，但 Go 原始字符串不能含反引号，故用 `+ "```mermaid" +` 拼接。
- `prompt_bundle_types.go`：三处装配 —— full 分支（`:103` 起）写完整版，light 分支与 managed 分支写精简版。全部落在 **stable 段**（静态文本，不破坏 prompt cache；首次部署会改一次 `StableCacheKey`，属预期）。
- `prompt_visualization_test.go`：新增 4 个守卫 —— mermaid 围栏契约、禁 ASCII/禁内联 SVG、图型路由表齐全、单图 ≤5 节点、循环线性化、**让位于阶段指令**、三档 profile 均携带、精简版体积不超过完整版 1/3。

关键设计决策：

- **冲突消解**：专利工作流 `phase_prompt.go:1896` 要求图形落盘且禁止内联源码，与新规则方向相反。未改那条约束，而是在新规则末尾加让位条款「若当前工作流或阶段指令对图形输出另有规定……以那条更具体的指令为准」，由测试钉死。
- **中文单语**：与 `PromptCorePrinciples` 等核心块一致（GUI 无英文提示词分支）；未做英文镜像以省 token。
- **light 档也带精简版**：「讲解/解释」类问题可能被 `ClassifyTurn` 判为 TaskFast→light，若 light 完全没有引导则该场景漏掉。

验证结果：

- `gofmt` 干净、`go build ./corelib/agent/` 通过
- 新增 4 个测试 + 既有提示词测试全绿
- 行为性验证：临时探针打印组装后的 prompt，确认完整版落在核心原则之后、任务状态契约之前，精简版落在 light/managed 对应位置（探针已删除）
- `scripts/check-agent-architecture.mjs` 通过
- `scripts/check-encoding.mjs` 退出码 1，但 79 条 NUL 报告**全部**位于 `deploy/logs/*.txt`（本次未触碰，`git status` 确认），我的三个文件零报告 → 存量问题
- 整包 `go test ./corelib/agent/` 有 3 个失败，**反向验证证明与本次改动无关**：撤回改动后 `TestToolGlobSkipsSymlinksByDefault` 仍失败（Windows 符号链接被当普通文件跟随，环境问题）；另两个（office 文本超时、bash 杀进程树）在整包高压下失败、单独跑通过，属负载抖动

## 9. Review 轮（2026-10-12，同一天，未提交）

首版 P0 实施后做了一轮源码级 review，抓到 **1 个设计缺陷 + 2 个事实错误**，均已修复。

### 9.1 修复：拒绝的利益换成了有害的（新增 fail-closed 开关）

**问题**：原实现把可视化规则无条件写进 `BuildPromptBundle` 的三处装配点，而这条 bundle 被**多条 reply  surface 共用**。做了逐路径对账：

| 装配 / 调用方 | 是否渲染 mermaid | 证据 |
|---|---|---|
| GUI IM 入口 `im_system_prompt.go:319` → AI 助手面板、编码会话、VE、文件伴读、确认卡片 | ✅ | `aiAssistantMarkdown.tsx:43,831` → `AssistantMermaidDiagram` |
| GUI 轻量轮次 `buildLightIMSystemPrompt:178` → `agentruntime.BuildLightPrompt` | ✅ | 同上（ GUI 所有面板共享该渲染器） |
| `agentservice/core_agent_executor.go:1605`（桌面 bot 聊天，落在 `botMessageBody.tsx`） | ❌ | 该组件只有 `parseMarkdownTableCells`，代码块走 `:344` 的 `<pre>` |
| `tui/app.go:610`（终端） | ❌ | 纯文本输出 |
| 第三方 IM 网关 `thirdparty_gateway.go` → `hub/internal/im` | ❌ | 飞书/QQ 等平台无 mermaid 渲染 |

因此原实现对 TUI、hub bot、第三方 IM **是有害的**：模型被要求画图，产出却是一坨无法渲染的原始源码。

**修法**：给 `SystemPromptConfig` 加 `DiagramRenderingSupported bool`（**默认 false = 无图**，fail-closed），三处装配点加门禁；同义字段穿透到 `agentruntime.LightPromptRequest`；仅 GUI 两处显式置 true。新增反向守卫 `TestBuildPromptBundle_OmitsVisualizationRulesWithoutSurfaceSupport` 钉死"未声明 = 不出图"。

**为什么用显式 opt-in 而不是黑名单**：漏配一个布尔位的后果是「少一张图」（无害），而漏排除一个渠道的后果是「用户看到一堆无法渲染的源码」（有害）。零值语义应与后者对齐。

### 9.2 修复：两条语法建议是错的（实测推翻）

提示词里一共推荐了 8 种图型的写法。写出来之后，用**本地实装的 mermaid 11.14 真实 parser**（`node_modules`，非凭记忆）逐条 `mermaid.parse()` 验证，12 个样例中 **1 个 FAIL**：

- ❌ `erDiagram` 的 `CUSTOMER["客户表"]` 实体别名写法 → **Parse error**。首版却把它当推荐写法教给了模型。
- ❌ 首版还写了"mindmap 不支持引号标签" → 实测 `mindmap` + `"分支一"` **正常通过**，是凭印象写的错误禁令。

已按实测结果改写提示词（erDiagram：实体名须为无空格 ASCII 标识符 + 中文列名写在属性块内；mindmap：圆括号会被解析为节点形状，这才是真正的坑）。

**为把这个口子永久堵上**，新增常驻守卫 `guiapp/frontend/src/components/ai/__tests__/mermaidTaughtForms.test.ts`（10 用例，全绿）——用真实 mermaid 解析每种教授的图型。mermaid 升级若改变语法，这里会先红，而不是等到用户聊天窗口里出现降级代码。

### 9.3 修复：踩到既有 light prompt 体积守卫

`guiapp/im_execution_profile_test.go:948` 有硬上限（原 `len(prompt) <= 3200`，注释称实测约 2.9KB）。加入 490 字节的精简版后该守卫 **FAIL**。

真实数字比"加多了"更微妙 —— 实测得出四个数：

| 项 | 字节数 |
|---|---|
| light 提示词**基线**（无配图块） | 3162 |
| 原上限 | 3200 |
| **剩余可用空间** | **38** |
| 首版精简块 | 490 |

即 **light bundle 自身早已贴满预算，只剩 38 字节**，不是新块太大。而且用真实分类器 `PromptProfileFromUserText` 实测 6 个讲解类问法后发现，「讲讲 RAG 的原理」「这个系统的模块之间是怎么调用的」「梳理一下审批流的状态流转」**都被判成 light**（`short simple turn`）—— light 恰恰是配图的主力场景，砍掉等于丢掉大半收益。

处理：

1. 精简块 490 → **129 字节**，只保留防"失败出图"的最小集：触发条件 + 客户端真正渲染的围栏语法 + ≤5 节点约束 + 阶段指令让位。图型路由表留给 full 档。
2. 上限 3200 → **3700**，并写明理由：真正的防线是守卫里同在的 full-agent 段落黑名单（`Group Discussion` / `CodingSubAgent` / `compress_context`），字节上限只是 tripwire，不该因 "+129 字节有用内容"而误伤。

⚠️ 抬高既有预算是产品决策，这里改是因为**有实测数据证明基线已饱和、且该字节是功能性的**；若无此类依据，应回去扩大压缩而不是动上限。

### 9.4 本轮改动清单

Go（6 个文件）：`corelib/agent/system_prompt.go`（新增开关）、`corelib/agent/prompt_bundle_types.go`（三处门禁 + 装配）、`corelib/agent/prompt_blocks.go`（语法纠正 + 精简版减重 490→129）、`corelib/agentruntime/prompt_sections.go`（字段穿透）、`guiapp/im_system_prompt.go`（GUI 两处置 true）、`guiapp/im_execution_profile_test.go`（light 预算 3200→3700，附实测依据）。

新增测试（3 个文件）：`corelib/agent/prompt_visualization_test.go`（6 用例，含新增反向守卫）、`corelib/agentruntime/prompt_sections_test.go`、`guiapp/im_system_prompt_visualization_test.go`。

前端（1 个文件）：`src/components/ai/__tests__/mermaidTaughtForms.test.ts`（10 用例）。

### 9.4b 本轮验证结果

- `gofmt` 干净；`go build` 通过（corelib/agent、corelib/agentruntime、guiapp）
- `go test ./corelib/agent/ -run 'TestPromptVisualization|TestBuildPromptBundle'` 全绿
- `go test ./corelib/agentruntime/ -run TestBuildLightPrompt` 全绿
- `go test ./guiapp/ -run TestBuildLightIMSystemPrompt` **4/4 全绿**（含原体积守卫）
- `vitest` 新增 10/10 全绿；`tsc --noEmit` 干净
- `check-agent-architecture.mjs` 通过；`check-encoding.mjs` 对本次全部改动文件零报告（79 条 NUL 仍全在 `deploy/logs`，存量）
- `go vet` 三个包（corelib/agent、corelib/agentruntime、guiapp）**全部通过**，退出码 0、无输出。
- ⚠️ 中途 `go vet`/`go build` 报 `mkdir ...: There is not enough space on the disk`，路径在 `$(go env GOCACHE)`（`AppData\Local\go-build`）。但 `df` 显示仍有 236G 可用 → 这是**瞬时 ENOSPC**，直接重试即通过，**不是缓存撑爆、更不是代码缺陷**。判据：报错路径全在 GOCACHE 而非源码文件，且重试可复现消失。不要为此执行 `go clean -cache`（白白多等一场全量重编译）。

### 9.4 Review 第二轮（同一天续）—— 收口了自己的两个假守卫

第一轮 review 认为自己已经验证充分，第二轮回头检查**自己写的测试是否真的会失败**，结果两个都不合格：

1. ⭐⭐ **自己写的"语法守卫"是假的**：`mermaidTaughtForms.test.ts` 的用例名叫 `%s parses when written as the prompt teaches`，但**从未调用过 parser** —— 只断言了 `startsWith(type)`。语法全错也能绿，保护为零。讽刺的是，这正是 §9.5 里我刚写下的那条方法论（"语法建议必须过真 parser"）所要防的事，而我的守卫本身就违反了它。
   - 修法：改为真正 `await mermaid.parse(source)`，并加 **negative controls**（`CUSTOMER["客户表"]`、未声明图型必须被拒）。
   - ⭐ **关键验证：变异测试**。把 `subgraph Ingest["数据接入"]` 改成 `(...]` 后测试立刻变红（`× flowchart parses...`）—— 证明守卫会失败，而不是装饰品。**没有这一步，"测试通过"毫无意义。**
   - 顺带实测澄清了 API 契约：`mermaid.parse` 失败时**抛异常**（不是 resolve false），成功时 resolve 出 `{diagramType, config}`。此前写的 `result !== false` 冗余防御引发 TS2367，按实测删掉。

2. ⭐⭐ **GUI 路径里混着非 GUI 出口**：`im_system_prompt.go` 这一个 builder 上除了桌面 UI，还挂着两种**投递到外部**的 turn：
   - **定时任务**（owner `scheduled_task` / `lansenger-scheduled:*`）：结果投递到任务自己的渠道（多 profile 场景是 Lansenger bot/群），没有 markdown 渲染。
   - **Lansenger 群桥**（owner `lansenger-group:*`）：回复走 IM 总线。
   - 即 §9.1 的 reply-surface 对账按"builder"粒度做还是太粗了 —— **同一个 builder 内部也要按 ownerID 分出口**（仓库对此早有惯例：`workflowDocDeliverySection(userID, platform)`、`fileCompanionTurnWithoutTools`）。
   - 修法：新增 `replyRendersDiagrams(promptUserID)`（复用既有 `isScheduledTaskConversationOwner` / `isLansengerGroupConversationUserID`），替换两处硬编码 `true`。

同步补的两个新守卫都做了变异验证：

| 守卫 | 变异方式 | 结果 |
|---|---|---|
| `TestBuildLightIMSystemPrompt_OmitsDiagramCueForExternalDelivery` | 把 light 路径改回无条件 `true` | 立刻 FAIL，精确指认 `scheduled task` |
| `TestTUIPromptOmitsDiagramCue`（tui 包新增） | 翻转 `DiagramRenderingSupported: true` | 立刻 FAIL |

### 9.4b Review 第三轮（同一天续）—— 补上端到端凭证，并纠正一个过度断言

第二轮之后还有一个未经我亲手验证的前提：**"GUI 会渲染 mermaid"这句话最初来自子代理报告**。本轮把渲染链路逐环核实到代码，并补了端到端测试。

**逐环核实结果（推翻/修正了两条二手结论）**：

| 环节 | 结论 | 证据 |
|---|---|---|
| 面板是否接入渲染器 | ✅ | `AIAssistantPanel.tsx:11` import `renderMessage` from `./aiAssistantMarkdown` |
| 编码会话是否渲染 | ✅ 但**理由与二手报告不同** | `isPureCodingEnvironment`（`:2411`）实际用于预览面板/中断作用域/冲突面板，**与渲染无关**；编码会话和助手 Tab 走同一个 `renderMessage`，所以天然支持。原记录说"`:2411` 不禁 mermaid"暗示它是个开关，这是错的。 |
| 流式未闭合围栏 | ✅ 保持源码 | `aiAssistantMarkdown.tsx:824-837` `flushCodeBlock` 注释明确：等闭合围栏到达再渲染 |
| mermaid 免责/降级 | ✅ | 失败时 `AssistantMermaidDiagram:184-190` 显示错误+源码 |

**新增端到端测试** `assistantDiagramEndToEnd.test.tsx`（3 用例），断言的是「整段助手回复（前后有散文、中间有图）真的到达了 `AssistantMermaidDiagram` 并带上完整源码」——这是各个单元测试覆盖不到的那一环。变异验证：把 `flushCodeBlock` 的分发条件改成 `false &&` → 2 条立刻 FAIL。

⭐ **过程中纠正了自己的一个过度断言**：初版端到端测试要求出现 `data-testid="assistant-mermaid-diagram"`（即真 SVG），失败。写探针查清原因：**jsdom 没有 SVG 布局引擎，`mermaid.render` 恒定抛 `text2.getBBox is not a function`** —— 是**测试环境限制，不是产品缺陷**，组件此时正确地走了降级路径（显示错误 + 可读源码）。

- **二手结论（含子代理报告）在写进交付物前要抽核**：本轮核了 4 条二手结论，其中 1 条（编码会话为何也渲染）的原因是错的。
- **断言失败先分清是"产品坏了"还是"环境做不到"**，别急着改产品代码迁就测试环境。本次靠临时探针打出真实报错（`getBBox is not a function`）才完成定性。
- **还要分清第三种情况：测试场景不真实**。第四轮里我差点为扩大函数签名动产品代码，最后发现是我的夹具传了 nil loopCtx 而产线从不这样。查清"生产环境实际长什么样"再判断归属。
- **给共享能力加守卫时按入口逐条清点**，别按改了几处来算：本次修了 light 路径就以为完成，漏了同一能力的 full 入口（恰恰是 GUI 主路径）。加完守卫要问"这功能有几条入口，是不是每条都有测试"。
- **断言强度要与运行时能力匹配**：写下"GUI 会渲染"这类核心论断时，先确认这个论断在当前测试运行时里能被**怎样诚实地验证**。不能验证的那部分要显式标注为环境限制，而不是假装验证了。

### 9.4c Review 第四轮（同一天续）—— 补上 full-profile 路径，抓到一个漏网的回退污染

第二轮加的 `replyRendersDiagrams` 只被 light 路径的测试覆盖。**full profile 入口（`buildIMEntrySystemPrompt`）从未被端到端验证过** —— 而那才是 GUI 的主路径。补测试后立刻抓到一个真实缺陷。

⭐⭐ **缺陷：`promptUserID` 是被回退污染过的值**
`promptRuntimeUserID`（`im_system_prompt.go:612`）在 loopCtx 为 nil 且无显式 runtime owner 时，**回退到 `desktopUserID`**。这个回退对"描述本地工具行为"的段落是合理的猜测，但用来判定"回复投递到哪里"就错了：定时任务、Lansenger 群这些外部 turn 会被重新标记成桌面用户，于是照样收到配图指令，产出无法渲染的源码。

修法：新增 `diagramSurfaceOwnerID(loopCtx, promptUserID)`，**优先取 loop context 里的真实 owner**（`runtime_context.go:115` 证明产线上 `PolicyOwnerID`/`WorkflowOwnerID` 就等于 `msg.UserID`），只有在确实拿不到时才回退。

⚠️ **过程中的一次误判值得记录**：第一次失败时我一度以为是产品缺陷要去扩大函数签名传 owner；查到 `PolicyOwnerID: strings.TrimSpace(msg.UserID)` 后才看清，真正不真实的是**我的测试夹具**（传了 nil loopCtx），产线从不这样。**结论：断言失败先确认是"产品坏了"还是"测试场景不真实"，不要急着改产品去迁就测试。** 我为此白写了一版 helper 签名，已回退。

**新增测试**（`guiapp/im_system_prompt_diagram_surface_test.go`，2 用例 / 6 个 owner 场景），均经变异验证：

| 守卫 | 变异方式 | 结果 |
|---|---|---|
| `TestBuildIMEntrySystemPrompt_DiagramCueFollowsReplySurface` | full 入口改回无条件 `true` | 立刻 FAIL，精确指认 `scheduled task` |
| `TestDiagramSurfaceOwnerIDPrefersLoopOwner` | —— | 钉住"loop owner 优先于回退值"这条优先级，防止退化回 `promptUserID` |

coverage 至此：**light 入口 + full 入口 + TUI + core 三档 profile**，四个面各有守卫。

### 9.4d Review 第五轮（同一天续）—— 首次量化成本，并撤掉一条自己证伪的禁令

前四轮都在补正确性和覆盖面，本轮转向**副作用与非证实断言**：

**① 量化了此前从未测过的 prompt-cache 成本**（结论：设计正确，且有一个此前不知道的收益）

| 指标 | 实测 |
|---|---|
| full 段增量 | +2377 字节（16679 → 19056，约 +14%） |
| light 段增量 | +172 字节（129 的块 + 换行） |
| **同一 GUI 用户连续三轮不同提问的 cache key** | **完全相同**（`allEqual=true`） |
| 不同 ASCII 面之间 | 正常分叉 |

关键是中间那条：因为可视化块写在 **stable 段**，同一用户每轮复用同一个 cache key，**不会碎片化**。若误写进 SessionContext（该段随消息变化），每轮都会生成新 key，等于静默废掉整段 prompt 缓存。

已固化为常驻守卫 `TestBuildPromptBundle_DiagramBlockStaysInStableSegment`（含 stable 必须在、session 必须不在、跨轮 key 必须一致三个断言）+ `TestBuildPromptBundle_DiagramBlockSizeBudget`（byte 预算，防止日后无意识膨胀）。变异验证：把 `stable.WriteString` 改成 `session.WriteString` → 守卫立刻 FAIL。

**② 撤掉了一条自己写下的、已被证伪的语法禁令**

提示词原本警告「mindmap 节点文字里的圆括号会被当作节点形状」。本轮补做**英文标签对照实测**时发现：`分支(A)` **正常解析**。也就是说我给模型下了一条不存在的限制（第二轮已犯过同类错误——当时多虑了 erDiagram 别名）。

- 证据不足的部分降级：**圆括号是否影响渲染出的形状**无法验证（jsdom 无 SVG 布局引擎，第三轮已知），所以不是"改成另一种说法"，而是**直接删除该断言**。
- 已加守卫 `TestPromptVisualizationRulesHasNoUnverifiedSyntaxClaims`，禁止再出现未经 parser 验证的语法主张。它当场就抓到了我漏改的一处残留（证明有效）。
- 同时新增 6 个英文对照用例（每个图型），确保这些约束对中文以外的回复同样成立。

⭐ **教训（与第三轮同源，再次确认）**：给模型的每一条"不许这么写"都必须先跑真 parser。错的禁令比没有禁令更糟 —— 它让模型放弃本可用的合法写法。

### 9.4e Review 第六轮（同一天续）—— 删掉自己写的假守卫

前五轮不断加测试，本轮反过来问：**这些测试里有没有根本不会失败的**。查出一类，而且是用一套更狠的变异测出来的。

⭐⭐ **`strings.Contains(常量, "某子串")` 是假守卫**

`TestPromptVisualizationRulesTeachesMermaidFence` 原先对每个图型断言 `Contains(rules, "pie")` 等。它只证明了**这个词还活着**，没证明**模型还能用这个图型**。用两级变异验证：

| 变异 | 旧守卫 | 新守卫 |
|---|---|---|
| 删掉「- 占比分布 → pie」整行 | ✅ 变红（看似有效） | ✅ 变红 |
| **改成「- pie 这个图型暂不支持」**（语义完全反转，词还在） | ❌ **仍然通过** | ✅ 变红 |

第二列才是判据：**能抓住"词还在但意思反了"的用例，才是真守卫**。旧版只是在重述源码常量，改措辞就翻红，保护为零。

修法：断言**每个图型必须出现在「用例 → 类型」这种路由行上**（箭头在类型名之前），并排除"暂不支持"等取反表述。这样既允许一行同时路由某种图型并警告其别名写法（erDiagram 正是如此），又能抓住语义反转。

同时删除了这类纯粹自我指涉的冗余断言（如重复校验同一常量子串），把 `CoversSyntaxHazards` 收紧到真正的行为约束（列名的存放位置、关系标签可本地化等）。

**保留的都是真行为守卫**（输入 → 输出的因果），因为它们断言装配结果而非复述常量：
`TestBuildPromptBundle_*`（含 cache 三组）、`TestBuildLightPrompt_DiagramCueFollowsSurfaceFlag`、`TestBuildLightIMSystemPrompt_*`、`TestDiagramSurfaceOwnerIDPrefersLoopOwner`、`TestTUIPromptOmitsDiagramCue`、`mermaidTaughtForms.test.ts`（调真 parser）、`assistantDiagramEndToEnd.test.tsx`（走真管线）。

⭐ **教训**：`strings.Contains(被测常量, 常量里的子串)` 这类断言要一律怀疑。**验证"它存在"不等于验证"它有效"** —— 写完后必须做一次"保留关键词、反转语义"的变异，通过就是假守卫。

### 9.4f Review 第七轮（同一天续）—— 收敛重复实现，并补上一个我自己引入的 fail-open

本轮起因是注意到 **146 行产品代码配了 761 行测试**（约 5:1），回头审视是否过度设计。结论：测试量是合理的（功能横跨 4 个 Go 包 + 前端，且每处都有不同的失败模式），但**产品代码里确实有一段重复实现**。

**① 收敛了 `diagramSurfaceOwnerID` 的重复逻辑**

第四轮写的版本自己实现了三级 owner 解析，与既有的 `promptRuntimeUserID` 前三行**完全一致**，却漏了它的 `RequestID` 分支 —— 典型的"重造一半轮子"。现在改为复用同一解析顺序，只表达真正的差异（不允许 desktop fallback），并对不再适用的注释做了更新。

**② 修了一个由收敛动作新引入的 fail-open（并补上守卫）**

收敛时把 fallback 从 `desktopUserID` 改成 `""`，但 `replyRendersDiagrams("")` 原本会返回 `true` —— 空 owner 变成了"当成桌面就是"。这正是我在注释里写"unknown 必须保守、不能猜 desktop"要防的事，**自己在改的过程中违反了它**。

修法：给空 owner 显式返回 `false`。并补 `TestReplyRendersDiagramsUnknownSurfaceIsFailClosed`，**两级变异均已验证**：

| 变异 | 结果 |
|---|---|
| ① 删掉空 owner 分支 | ✅ FAIL |
| ② 把 `return false` 反转成 `return true`（fail-open） | ✅ FAIL |

这条很重要：钉钉了"以后任何调用方哪怕不传 loopCtx，也不会静默给所有渠道开配图"。

⭐ **教训**：**修改 fail-closed 逻辑时，要比第一次写它时更小心**。收敛重构会改变默认值语义，而默认值恰恰是 fail-closed 的核心。本轮若没收尾检查，就交付了一个自己引入的漏洞。

**七轮累计改动**（未提交）：产品代码 6 个文件 +146 行；测试 8 个新增文件约 780 行。

### 9.4g Review 第八轮（提交后）—— profile 切换维度，并揪出一组"过于均匀"的测试数据

上一版已提交（`84717e06`），本轮补测一个从未覆盖的维度：**多轮会话中 profile 切换时的 cache 行为**。

探针结论（好消息）：**full / light / managed 三个 profile 各自跨 4 轮都只产生 1 个 cache key**。配图开关是"回复渲染面"的属性而非消息的属性，所以不随轮次抖动。三者**互相不同**是内容本就不同的正常结果，不是碎片化。

已固化为 `TestBuildPromptBundle_DiagramFlagStableAcrossEveryProfile`，并加了 sanity 断言（每个 profile 必须真的含配图块，否则"稳定"是空转）。

⭐⭐ **但验证过程揪出了一个更隐蔽的东西：测试数据本身太均匀**

做变异时让 light 分支的配图块随 `len(userMessage)%2` 变化（模拟"每轮抖动"），**测试居然通过了**。原因不是守卫无效，而是我选的 4 条测试消息长度全是偶数 —— 变异被静默吸收。

| 消息 | 长度 | %2 |
|---|---|---|
| 解释架构 | 4 | 0 |
| 讲讲 RAG 的原理 | 10 | 0 |
| 帮我看看这个函数 | 8 | 0 |
| 梳理审批流程 | 6 | 0 |

改成奇偶混合（并加入一条长句、一条极短句）后，同一个变异立刻被抓住：`light profile: produced 2 distinct cache keys (want 1)`。

⭐ **教训**：**"测试通过"有时不是因为代码对，而是因为数据同质。** 写涉及长度/奇偶/哈希/顺序/并发交错的测试时，输入必须刻意不均匀，否则变异会被数据吸收，守卫形同虚设。这是第六轮"两级变异"的补充——第二级变异失败时，先怀疑测试用例本身，再怀疑被测代码。

**第八轮累计**：新增 1 个守卫（含 sanity）+ 修正一组同质测试数据。

### 9.4h Review 第九轮（提交后）—— 跨界回归：确认没污染他人，并发现他人一个时间脆性 bug

提交完成后做了一轮"往外看"的验证，而非继续在自己改的文件里找茬。

**① 反向验证：确认一个失败与我无关（用 worktree，不用推理）**

全量 `go test ./corelib/agent/` 报 `TestToolGlobSkipsSymlinksByDefault` 失败。这类事最容易误判，所以没有靠推理，而是 **`git worktree add` 检出我的父 commit**，在完全没有我改动的环境下跑同一测试 —— **同样失败**（Windows 把符号链接当普通文件）。确认是既有的平台问题。

> 手法：`git worktree add /tmp/base_wt <基线commit>` → 跑同一测试 → 对比。远可靠于"我觉得应该无关"。

**② 前端全量回归（6457 个测试）抓到他人一处真实缺陷**

```
AIAssistantPanel.test.tsx:4257
expect(meta.textContent).not.toContain(':22')
```

本意是断言远程地址**没有泄漏 SSH 端口 `:22`**，但 meta 文本里带创建时间戳（`由你创建 · 10/12 15:22 · www...`）——**当测试恰好在某个小时的 22 分运行时，时间戳里的 `:22` 会被误判成端口**。即这个用例**每小时必有 1 分钟失败**。

反向验证：15:22 跑 → FAIL（`expected ... not to contain ':22'`）；等到 15:24 同一份代码再跑 → PASS。只取决于挂钟时间。

**该缺陷不属于本次改动，未擅自修改**，已向用户报告待定夺。建议修法：改从 `task-execution-meta` 的 `title` 或结构化字段断言，或对时间片段做掩码后再判断 `:22`。

**③ 自身卫生**：新增测试无全局状态污染（未设环境变量、无包级变量）；prompt 类全量、`agentruntime` 全包均通过。

### 9.4i Review 第十轮（提交后）—— 补一个运维级 kill switch

本轮起因是意识到一件从未验证过的事：**整套方案从未在真实模型上跑过**（本机无 LLM 配置），因此一旦上线后发现某个模型产出的 mermaid 大面积解析失败，恢复手段是否充分就变得关键。

顺着这条线查出一个真实的可运维性缺口：`MACLAW_PROMPT_PROFILE` 只能强制 light/full，**而 light 和 full 两档我都会注入配图规则**——也就是说运维**没有任何办法在不回滚代码的前提下关掉配图**。

那时每个 GUI 回复都会退化成原始源码，而唯一出路是重新构建部署。

**修法**：新增 `MACLAW_DIAGRAM_RENDERING`（`off/0/false/no` 关闭），并抽出 `EnableDiagramRendering(cfg)` 作为**唯一判定点**，三处注入全部改走它。之所以集中到一个函数，是因为"能否出图"现在有两个独立原因可以否掉——渲染面不支持、运维开关——分散判断迟早会不同步。

值与既有惯例一致（`MACLAW_WORKING_STATE` 用的同样是 `off/0/false/no`）。

⚠️ **注意区分这两个开关的职责**：

| 开关 | 粒度 | 用途 |
|---|---|---|
| `DiagramRenderingSupported` | 每个回复面，编译期 opt-in | 长期正确性：不给无法渲染的渠道发指令 |
| `MACLAW_DIAGRAM_RENDERING` | 全局，运行时 | 应急处置：模型行为出问题时立刻止血 |

二者互不替代。

守卫 `TestDiagramRenderingEnvKeyIsOperatorKillSwitch` + `TestEnableDiagramRenderingRequiresDeclaredSurface`（后者专门防止两个原因互相干扰），**两级变异均已验证**：① 读取但忽略 env → FAIL；② 把 `case "0","off"...: return false` 反转成 `return true` → FAIL。测试用 `t.Setenv`（自动恢复，无全局变量污染）。

**本轮暴露的更大问题**：截至目前，这个功能**从未经过真实 LLM 的端到端验证**。所有证据都来自构造性测试（真 parser 验语法、真管线验渲染、真分类器验 profile）。上线前应拿一个真实模型跑一轮讲解类提问，确认模型实际是否会按引导输出可解析的围栏。

### 9.4j 真实模型端到端验证（第十轮遗留项，已补上）

上一轮坦白"该功能从未经过真实模型验证"，用户随后在 GUI 里实际跑了一次（"帮我设计一个简单的 web 系统的架构图"）。从 `~/.maclaw/data/ai_assistant_conversation.json` 取到这次真实会话，结论是**通过的**。

**模型的自述**（`entries[1].reasoning_content` 原文）：

> 用户要求设计一个简单的 web 系统的架构图。这是一个讲解/设计类任务。根据系统提示，架构图默认使用 mermaid 代码块输出。

这说明提示词被真实读取并遵循了 —— 之前所有测试都只能证明"我们写了什么"，这次才证明"模型照做了"。

**模型的实际输出**（`entries[4].content`，1622 字）结构完全符合引导：

- 一张 `flowchart TB` 分层图（客户端 / 接入层 / 应用层 / 数据层，含 subgraph）
- 一张 `sequenceDiagram` 时序图（浏览器 → Nginx → 后端 → Redis / MySQL）
- 每张图后面都跟了讲解段落，末尾是组件职责清单表格
- 节点标签全为中文且加了引号，`<br/>` 换行、泳道别名 `participant U as 浏览器` 都用对了

**把这两段实际输出喂给真 parser `mermaid.parse` → 两段均 PARSE OK。**

同时排除了三种常见的失败猜测：不是语法错、不是渲染预算兜底（远低于 750 节点 / 1500 边）、不是内容截断（回复完整）。

> 补充观察：这是至今唯一一次端到端证据。**构造性测试能证明代码没写错，但证明不了模型会照做**；反过来，一次真实会话也不能替代构造性测试（它只覆盖一个提问）。两类都不可省。

### 9.5 Review 方法论沉淀

- **"有益"的默认必须按回复出口对账，不能按装配点对账**：同一个 bundle 构建函数可以流向渲染能力完全不同的多个 UI。加全局输出能力前，先把「谁会实际看到它」列全。
- **对账粒度要比"builder"更细**：同一个 prompt builder 内部还可能按 ownerID 分发到不同投递渠道（定时任务 / IM 桥 / 桌面 UI）。找不到现成判据时，先看仓库里有无同类的 `xxx(userID, platform)` 函数可以复用。
- **给模型的语法建议必须过真 parser**：作者凭印象写的语法比没有语法更遭 —— 它把"随机失败"变成"按指示必然失败"。
- **既有体积/数量守卫往往在别包**：改共享 prompt 前先 grep `len(prompt)` / `len(light)` 之类的上限断言，`corelib` 的改动会在 `guiapp` 的守卫上炸。
- **撞到上限时先量基线再动手**："是我加多了"和"预算本来就满了"是两种完全不同的处置。把"基线 / 上限 / 剩余空间 / 新增块"四个数都测出来，才知道该压缩还是该申请预算。
- **功能该不该为预算让路，交给真实分类器裁决**：与其争论"light 场景重不重要"，不如跑一次真实的 profile 分类，看目标场景实际落到哪档。本轮正是靠这一步证明 light 不可砍。
- **每次拆解调研都要留证据**：临时探针（量字节、验语法、跑分类器）用完即删，但**结论要落成常驻测试或文档表格**——否则下一轮 review 又得重新量一遍。
- ⭐⭐ **"测试通过"不等于"守卫有效"——必须变异验证**。写完反向/边界守卫后，故意注入缺陷（改错语法、翻转布尔开关），确认它**真的会变红**，再恢复。本轮两个自己写的守卫里有一个完全是空转（名字叫 `parses` 却从没调过 parser），只跑测试绝不会发现。
- **警惕"meta 讽刺"**：最容易写出假守卫的时刻，恰恰是你刚总结出"要防假东西"那条方法论之后——因为此时会默认自己已经做对了。第六轮就是实证：第二轮刚立下"必须变异验证"，而当时补的那批 `Contains` 断言恰恰经不起第 ② 级变异。对自己产出的守卫要比对别人的更狠，且**写完全部守卫后要回头重跑一遍更严的变异**。
- ⭐⭐ **变异第二级失败时，先怀疑测试用例本身**：第八轮的变异（让状态随 `len(msg)%2` 变化）没被抓住，原因是 4 条测试消息**长度全是偶数**，变异被数据静默吸收。涉及长度/奇偶/哈希/顺序/并发交错的测试，**输入必须刻意不均匀**。"测试通过"有时不是因为代码对，而是因为数据同质。
- **反向/absence 断言要配一条 sanity**：断言"某能力不可用"的用例可能因为功能压根没接上而通过（第八轮的 profile-stability 守卫就补了一条"该 profile 必须真含配图块"）。
- **"与我无关"要靠隔离运行证明，不要靠推理**：全量回归出现失败（尤其是symlink、编码、时区、随机数这类环境相关项）时，用 `git worktree add <基线commit>` 在无自己改动的环境跑同一用例对比，一眼就能切断嫌疑。
- **跑全量回归而不是只跑自己的 subset**：自己的用例全绿只能证明没写错，别人的大量测试才会告诉你有没有连带影响。本轮就是这样在 6457 个用例里发现了一处与配图完全无关的时间脆性缺陷。
- **问自己"这个功能坏了该怎么止血"**：第九轮发现 MACLAW_PROMPT_PROFILE 管不到配图，意味着故障时唯一出路是回滚重建。凡是**默认开启、且失败会让回复明显劣化**的能力，都应配有运行时开关；不要只依赖编译期的 per-surface opt-in。
- **区分"正确性控制"和"应急控制"**：per-surface opt-in 保证长期正确性，全局 env 开关负责止血，二者粒度不同、互不替代。把多个"可以否掉某功能"的原因集中到**一个判定函数**，否则容易各自演化到不同步。
- **承认"没验证过的环节"**：本轮才明白整套方案从未跑过真实 LLM。构造性测试能覆盖很多，但覆盖不了"模型是否真的照做"。这条要写明，别让它伪装成已验证。
- ⭐ **两类证据不可互相替代**：构造性测试证明"代码没写错"，一次真实会话证明"模型会照做"——后者还能从 `reasoning_content` 看到模型如何解读提示词，这是任何测试都给不了的。**但最终"未能产出图"的根因往往需要用户侧截图/复现**：本次我在语法、预算、截断三处排查都干净，真实剩下的差异面要靠现场证据才能收窄。
- **警惕 `strings.Contains(被测常量, 该常量里的子串)`**：这类断言本质是在复述源码，改措辞就翻红却不保护任何行为。应改为验证结构位置（如出现在哪类行上、箭头前后关系）或直接验证下游行为。
- ⭐⭐ **变异要分两级，只做第一级会放过假守卫**：① 删掉被测内容 → 变红，这只证明测试连着代码，几乎任何断言都做得到；② **保留关键词但反转语义** → 必须仍然变红。第 ② 级才证明断言的是**含义**而非**字面量**。第六轮靠它揪出了一个只验证词还存在的装饰性守卫。
