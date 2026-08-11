# 普通 AI 助手与编程 Agent 双模型档案开发计划

## 1. 评审结论

原计划的方向正确：共享服务商与凭据、将“选择哪个模型”拆分为普通 AI 助手和编程 Agent 两个档案，并让编程 Agent 默认跟随普通助手。

复核后需要收紧四项关键设计，避免功能上线后显得复杂或产生误操作：

1. **模型分配与服务商管理必须分开。** 服务商负责连接、认证和可用模型；档案只负责“这个场景使用哪个服务商和模型”。不能继续把二者混在同一个“配置服务商”弹窗中。
2. **跟随状态应是最短路径，而非一组禁用表单。** 开启跟随时只展示实际生效的结果；不要在旁边保留灰掉的服务商和模型输入框。
3. **保存、测试和生效时机要区分。** 对已经配置过的服务商，修改模型分配应能直接保存，不应强制重新测试连接；测试是可选诊断。新配置或编辑服务商连接时，才保留“测试并保存”。
4. **需要明确配置优先级和运行中行为。** 编程档案是基础选择，现有推理/视觉路由仍可覆盖；本次保存只作用于后续 turn，绝不改写正在进行的请求。

本计划据此重写了数据契约、界面布局、状态机、接口边界和验收项。

### 1.1 本轮复核：需在开发前锁定的决策

总体方案可执行，但原文中有几处在实现时会产生歧义，现统一收敛如下：

| 议题 | 收敛后的决定 | 原因 |
| --- | --- | --- |
| 跟随时是否保留独立选择 | **保留为恢复草稿，但 resolver 忽略它。** `coding.inherit_assistant=true` 时仍可保存上一次独立的 `provider_id/model`，重新关闭跟随即可预填；它绝不参与有效配置或请求。 | 同时满足“重新独立时不丢选择”和“跟随时只有 assistant 生效”。 |
| 删除被引用的服务商 | UI 不允许把任一有效档案删成持久化悬空引用。assistant 被引用时必须在同一事务中“改选替代服务商 + 模型”；独立 coding 被引用时可“改选替代服务商 + 模型”或“切回跟随”；仅作为跟随恢复选择的引用可清空并提示。 | 现有 profile 结构没有 `invalid` 持久化状态；保留悬空引用会让后续快捷保存和迁移不可预测。 |
| 快捷切换服务商 | 先选服务商、再选该服务商的模型，**第二步确认模型后才写入**。不得在第一步临时保存空模型、旧模型，或把服务商默认模型偷偷写入。 | 避免短暂无效配置，也让“切服务商”与“选模型”的含义清楚。 |
| 当前档案来源 | 由 AI 标签/任务控制器输出稳定的 `assistant | coding | none`，并同步给 App、底部 chip 和侧栏；App 不根据页面名称、是否出现代码或最近一次选择推断。 | 防止底部切 coding、侧栏却切 assistant 的高风险错位。 |
| 健康状态 | `已连接` 仅代表该有效 profile 的最近一次成功探测；配置完整但未探测为 `未验证`，配置错误/最近确定失败为 `不可用`。跟随 coding 复用 assistant 结果。 | 不能再把现有 assistant-only 在线布尔值解释为两个档案都在线。 |
| 旧用量迁移 | 旧 `provider -> usage` 数据只以 `legacy/unattributed` 只读显示；新请求从创建开始携带 profile、provider ID、基础模型和最终模型。 | 历史数据无法可靠反推归属，猜测会污染成本和审计数据。 |
| 配置生效边界 | 普通对话在**每个 turn 发起时**冻结有效配置；编程任务在**根任务启动时**冻结基础配置，并由其所有嵌套 CodingSubAgent 继承。保存只影响下一个普通 turn 或下一次新建的编程任务。 | 既让用户能在下一次操作用到新模型，也不让一个长编程任务在中途混入新旧模型。 |
| 旧写入入口 | 所有前端和新代码只可通过带 `revision` 的统一 LLM 设置事务写入。旧的 provider/current 写入接口只保留为受控兼容包装，不能成为双档案或服务商删除的可写入口。 | 未携带版本号的旧接口无法提供可靠的并发保护，不能让它绕过引用完整性校验。 |

## 2. 目标、范围与非目标

| 档案 | 使用范围 | 默认策略 |
| --- | --- | --- |
| 普通 AI 助手 | 桌面聊天、IM、工作流、知识问答、技能及其他非编程 Agent 调用 | 使用用户选定的服务商与模型 |
| 编程 Agent | `CodingSubAgent`、编程工作台、本地/远程编程任务，以及嵌套编程子 Agent | 跟随普通 AI 助手 |

用户可为两套档案独立选择服务商和模型。同一个服务商可被两套档案引用并使用不同模型，但 API Key、OAuth/SSO 凭据、端点与协议始终只保存在一个共享服务商目录中。

本期不做：

- 第三类档案、按项目/用户/单任务的持久化覆盖；
- 更改现有推理、视觉与 MoA 路由优先级；
- “锁定模型、禁止路由覆盖”选项；
- 复制服务商凭据或为每个档案建立服务商副本。

## 3. 核心决策与配置契约

### 3.1 配置所有权

服务商目录继续使用现有 `maclaw_llm_providers`：它拥有名称、端点、协议、认证、凭据、超时、上下文长度、视觉能力、可发现模型列表和服务商建议模型。

模型档案新增 `maclaw_llm_profiles`：它只引用服务商的稳定 ID 并保存所选模型，绝不含 URL、密钥、token 或协议字段。服务商名称是可编辑展示名，不能再作为跨配置引用键。

```go
// Profiles 用指针表示是否仍是老版本配置，避免把“配置不存在”
// 和“已明确关闭 follow”混为一谈。
type MaclawLLMProfile struct {
    ProviderID       string `json:"provider_id,omitempty"`
    Model            string `json:"model,omitempty"`
    InheritAssistant bool   `json:"inherit_assistant,omitempty"`
}

type MaclawLLMProfiles struct {
    Version   int                     `json:"version"`
    Assistant MaclawLLMProfile        `json:"assistant"`
    Coding    MaclawLLMProfile        `json:"coding"`
}

// AppConfig
MaclawLLMProfiles *MaclawLLMProfiles `json:"maclaw_llm_profiles,omitempty"`
```

`MaclawLLMProvider` 同时新增不可编辑的 `ID`。新建服务商立即分配 UUID；旧服务商在**首次受控写盘**时补齐 ID。对于尚未物化 profiles 的旧配置，纯读取仍按规范化名称在内存中匹配一次，绝不因为读取而写盘；若名称大小写归一后不唯一，返回迁移错误而不是猜测。前端只保存和传递 ID，名称仅用于展示。

这样服务商改名不会影响任何档案。删除被任一**有效**档案引用的服务商时，后端必须在同一 compare-and-swap 配置事务中要求调用方先提交替代的 `ProviderID + Model`；仅当引用者是独立 coding 档案时，也可选择“切回跟随助手”。如果 assistant 正在引用该服务商，则必须先改选替代服务商，不能删除后留下无效 assistant。跟随状态下仅作恢复选择的 coding 引用可被清空，且不影响有效配置。普通“保存服务商目录”不得制造持久化悬空引用；本期也**不**以“删除后保留 invalid profile”为兜底。名称在一个目录中仍要求大小写不敏感唯一，以保证旧客户端、导入和人工编辑配置可诊断。

服务商对象的现有 `Model` 字段保留为“服务商默认/兼容模型”：它用于新建档案预填、尚未迁移的旧配置和旧 API 回退，**不是**已迁移请求的真相来源。普通助手和编程 Agent 的实际模型始终以 profile 为准。为兼容读取 `GetMaclawLLMProviders().current + provider.Model` 的旧消费者，响应层可投影 assistant 的模型到当前服务商副本；不要在保存 coding profile 时改写共享服务商的 `Model`，更不要把两个 profile 的选择互相覆盖。

### 3.2 有效配置解析与优先级

新增内部统一入口：

```text
Resolve(profile) → 档案选择 → 服务商物化与凭据解析 → 全局 thinking mode
                 →（仅编程调用）现有推理/视觉路由 → 最终请求模型
```

规则如下：

1. `assistant` 读取 `profiles.assistant`；`ProviderID` 物化为服务商对象后才取得展示名、凭据和端点。
2. `coding.inherit_assistant=true` 时，coding 直接使用 assistant 的有效基础选择；否则读取 `profiles.coding`。
3. 通过 `ProviderID` 从共享目录物化 `corelib.MaclawLLMConfig`，复用现有 credential store 优先级、OAuth、Wire API、超时和能力判断；再用 profile 的 `Model` 覆盖服务商默认模型。
4. `GetMaclawLLMConfig()` 保持现有语义，返回 assistant 档案；新增 `GetCodingLLMConfig()` 返回 coding 的有效基础配置。`MaterializeProviderByName` 下沉为不依赖“当前服务商”的公用物化助手。
5. CodingSubAgent 只从 `GetCodingLLMConfig()` 取得基础配置，然后才运行现有 `applyCodingRoutePreference`。普通 Agent 不触及 coding 档案。
6. 有效档案指向不存在服务商、模型为空或服务商不可用时，解析必须返回明确错误，不能静默换用其他服务商；`coding.inherit_assistant=true` 时，coding 自身保存的恢复草稿不参与解析。旧配置迁移是唯一例外：仅在尚未建立 profiles 时，从旧主选项生成 assistant，并令 coding 跟随。
7. Resolver 返回“基础选择”和不可变的执行归因 `profile=assistant|coding`；路由层只能生成新的“最终选择”及 `route_source=base|reasoning|vision|…`，不得反向修改 profile、服务商默认模型或兼容镜像。嵌套 CodingSubAgent 继承父任务已解析的基础选择和归因，不能在子 Agent 启动时重新读取全局配置。
8. 配置快照的粒度固定为：普通聊天、IM 与工作流在每个 turn 发起时解析并冻结；编程工作台、本地/远程编程任务在根任务启动时解析并冻结，后续工具调用和嵌套 CodingSubAgent 继承该快照。所有保存仅影响下一普通 turn 或下一次新建编程任务，绝不改写已启动任务。

### 3.3 旧配置迁移与兼容镜像

迁移采用**惰性物化，不在普通读取时写盘**：

- `maclaw_llm_profiles` 缺失：运行时将旧 `MaclawLLMCurrentProvider` 和其当前模型视为 assistant；coding 视为跟随 assistant。这是 `nil` 与 `InheritAssistant=false` 必须区分的原因：前者是尚未迁移，后者是用户明确保存的独立 coding 档案。
- 首次保存模型分配、首次保存服务商目录，或由受控配置迁移流程写盘时，创建 `version: 1` profiles。
- 继续回写现有平铺字段和 `MaclawLLMCurrentProvider`，使其镜像 assistant 档案的有效选择。VS Code 扩展、Hub 心跳、A2A、诊断和未迁移外部调用方保持读取 assistant；profile 保存不能写入 coding 的选择到这些字段。
- 旧 API 的 `current` 和 provider `Model` 只能被当作 assistant 兼容投影。它们不再是新 UI 的可写真相；所有新写入都先更新 profile，再在同一事务生成投影，避免“服务商 Model 被两个 profile 轮流覆盖”。
- 迁移和镜像不得触碰已有 API Key、OAuth access token、refresh token 或独立 credential store。

## 4. 交互与界面布局

### 4.1 页面结构

设置页不新增复杂导航，也不使用“主/副模型”这样的技术化命名。保留现有“大模型设置”位置，在页首加入一个紧凑、可直接编辑的“模型分配”区域；现有服务商配置弹窗改名为“管理服务商”，只处理连接和认证。

```text
大模型设置
说明：为不同工作场景选择模型。服务商连接与凭据在下方统一管理。

模型分配                                                [保存更改]
┌ 普通 AI 助手  桌面聊天、IM、工作流等
│ 服务商 [ OpenAI                 v ]  模型 [ gpt-5.2             v ]
├ 编程 Agent    编程工作台、本地与远程编程任务
│                         [✓] 跟随普通 AI 助手
│ 当前生效：OpenAI · gpt-5.2
└────────────────────────────────────────────────────────────────────

服务商与认证                              [管理服务商]
已配置 3 个服务商。连接、认证、模型发现和建议模型在此处维护。

Agent 最大推理轮数 / 编程 Agent 并发数 / 推理模式 / 用量
```

这是一个带两行记录的分组表单，而非两个装饰性大卡片。它在密集工具型界面中更容易扫读，也避免服务商编辑和模型分配发生视觉混淆。保存采用**行级校验、区域级提交**：哪一行无效就在该行显示问题，另一行的草稿不丢失；有效的两行仍作为一个 revision 一起提交，避免一处保存覆盖另一处。

### 4.2 常规桌面布局

- 区域标题、解释和保存动作处于同一行；保存按钮仅在存在未保存修改时可用，保存中显示“正在保存”。
- 每个档案行采用两列：左列是名称和覆盖范围，右列为表单或有效摘要。服务商和模型输入在右列中并排，服务商更宽、模型保持可输入的组合框。
- `普通 AI 助手` 的行始终可编辑；选择服务商后只刷新同一行的模型候选，不影响编程档案草稿。
- `编程 Agent` 行右上角放置“跟随普通 AI 助手”开关。开启时只显示 `当前生效：服务商 · 模型` 与一条简短说明，不显示灰色的独立控件；关闭时再以 150–200ms 的高度/淡入过渡展开服务商和模型选择。
- 在开启跟随前曾保存的独立选择作为恢复草稿保留在配置中，但不参与有效配置；首次关闭跟随时，若没有保存过独立选择，用当前 effective selection 预填表单，用户仍须点“保存更改”才会生效。
- 界面不自动保存下拉选择：单一页脚/标题处的“保存更改”提供清晰提交点；点击离开含未保存更改的页面时使用现有确认机制。保存成功后，焦点留在触发保存的位置并给出“仅影响后续请求”的结果提示；保存失败不收起独立编程表单、不清空任何草稿。

### 4.3 窄窗口、键盘与可访问性

- 宽度小于约 720px 时，每个档案行从“两列”变成标签在上、控件在下；服务商和模型控件各占一行。保存按钮保持可见但不使用悬浮遮挡表单。
- 交互顺序固定为：assistant 服务商 → assistant 模型 → coding 跟随开关 →（独立时）coding 服务商 → coding 模型 → 保存。
- 所有组合框、开关和保存按钮有明确可访问名称；跟随开启时，摘要通过 `aria-live="polite"` 告知有效模型变化。
- 模型候选下拉不得被设置页滚动容器裁切，应沿用已有 portal/fixed popover 方案；Esc 关闭候选，焦点返回触发控件。侧栏或输入框被压缩到只剩图标/短标签时，模型 chip 应降级为“打开当前档案模型菜单”，不挤出模型 ID 或产生第二行横向滚动。
- 动画仅用于展示“跟随切换带来的表单展开/收起”；`prefers-reduced-motion` 下立即切换。

### 4.4 服务商管理与测试路径

“管理服务商”沿用现有大弹窗/面板，但其责任必须收窄为：添加、编辑端点/协议/认证、OAuth 登录、获取模型列表、设置服务商建议模型、连接测试、删除服务商。

- 服务商管理面板不再承担“普通助手当前在用模型”的选择；保存服务商不会自动修改任何独立档案。
- 模型分配区内提供“找不到服务商？管理服务商”文本操作，直接打开服务商管理面板，不嵌套第二个弹窗。
- 修改已有档案分配时，允许直接保存。可提供“测试此选择”作为次级动作：它用当前**有效或独立**草稿的服务商和模型探测，但不写盘；coding 跟随时测试的是 assistant effective selection，并在按钮旁明确写“测试助手当前选择”。连接失败也不自动覆盖已保存配置。
- 添加或编辑服务商连接时，保留现有“测试并保存”门槛。OAuth 成功后回到同一设置页并刷新服务商列表，保留用户尚未保存的两个档案草稿；若草稿引用的服务商已被删除，明确标红而非替换。

### 4.5 关键状态和文案

| 情况 | 界面反馈 | 用户下一步 |
| --- | --- | --- |
| 未配置任何服务商 | 两行模型选择不可用，显示一个“添加服务商”主操作 | 打开服务商管理 |
| 编程 Agent 跟随 | “当前生效：OpenAI · gpt-5.2”，不展示禁用输入框 | 如需不同模型，关闭跟随 |
| 尝试删除正在使用的服务商 | assistant 正在使用时必须“改选替代服务商”；独立 coding 正在使用时可“改选替代服务商”或“切回跟随助手”；不能提交悬空引用 | 选择处置方式后再删除 |
| 模型列表暂不可用 | 保留当前模型，显示“未能获取列表；可输入模型 ID” | 重试或输入模型 ID |
| 推理/视觉路由可能覆盖 | 仅在编程行的辅助文本提示“含图片或推理任务可能使用已配置路由模型” | 查看路由设置；本期不可锁定 |
| 保存冲突 | 非破坏性提示“设置已在另一处更新”；提供“刷新并保留我的草稿”和“放弃我的更改” | 刷新后重新确认草稿，或明确放弃 |

保存后显示轻量 toast：`已更新后续编程任务的模型` 或 `已更新普通 AI 助手与跟随的编程 Agent 模型`。不声称运行中的任务已经切换。

侧栏的模型状态使用固定的两行摘要，避免在窄侧栏出现四行“名称 / 服务商 / 模型”的噪声：左侧为 `助手` / `编程`，右侧仅一行截断的 `服务商 · 模型`。跟随状态的编程行前缀固定为 `跟随助手`，实际模型放入同一行尾部；悬停和键盘聚焦提供完整值。若两套有效选择完全相同，仍保留两行和“跟随助手”语义，不能折叠成一行，否则用户无法确认默认行为是否生效。错误状态用图标、文本和 tooltip 共同表达；长名称以中间省略显示，行高不增长。

### 4.6 底部“模型切换”快捷入口

现有聊天输入框底部的模型 chip 不能再理解为“全局模型切换”。双档案上线后，它必须是**当前工作页的模型快捷入口**：

| 当前页面/标签 | chip 展示 | 点击后的作用范围 |
| --- | --- | --- |
| 普通 AI 助手对话、IM 对话、非编程工作流 | `助手 · gpt-5.2` | 只编辑 assistant 档案 |
| 本地/远程编程任务、编程工作台、CodingSubAgent 可见的任务页 | `编程 · gpt-5.2`，或 `编程 · 跟随助手` | 只查看或编辑 coding 档案 |
| 设置、系统、知识库等非对话页面 | 不展示 chip | 无 |

不能根据“用户最近一次切换的服务商”决定 chip 的作用域，也不能在编程页静默修改 assistant。`activeProfile` 必须由活动内容控制器给出的**实际执行模式**推导（例如普通聊天/IM/工作流为 assistant，local/remote coding workbench 和 coding turn 为 coding），而不是以“编程 UI 是否可见”、左侧工具选择或路由名称猜测；无法分类时不显示可写 chip。切换普通聊天标签和编程任务标签后，chip 与其菜单同时刷新。

#### assistant 上下文的快捷菜单

- 标题写明 `普通 AI 助手`，当前项显示 `服务商 · 模型`。
- 服务商与模型候选沿用当前快速菜单的结构；选择后只更新 assistant 档案，并提示“对下一次普通 AI 助手请求生效”。
- 仍保留“管理模型设置…”入口，用于进入完整的两档案设置页。

#### coding 上下文的快捷菜单

- 标题写明 `编程 Agent`，避免用户把它误认为普通聊天模型。
- **跟随状态下不展示可直接切换的服务商/模型列表。** 菜单只显示只读的 `跟随普通 AI 助手：OpenAI · gpt-5.2`，并提供 `改为独立模型…`，跳转模型设置并聚焦编程 Agent 行。这样一次点击不会把“跟随”悄悄变成独立配置。
- **独立状态下**才展示服务商和模型候选；选择后只保存 coding 档案，并提示“对下一次编程 Agent 请求生效”。
- 若当前任务已在运行，快捷入口可仍然允许修改后续任务，但在菜单顶部显示“当前任务继续使用启动时的模型”；不提供对运行中任务的热切换。
- 由推理/视觉路由实际替换模型时，chip 显示基础档案模型；运行状态/轨迹显示最终模型。不要让 chip 在任务中跳成路由模型，以免用户误以为自己的设置被更改。

实现上，快捷菜单的 props 需从单一 `currentModel/onSwitchModel` 升级为 `activeProfile`、两个 profile 的 effective summary、`codingInheritsAssistant` 和对应的 profile-scoped save action。原 `SetMaclawLLMCurrentModel` / `PatchConfigFields({maclaw_llm_current_provider})` 不能继续作为底部 chip 的通用写入路径。

#### 4.6.1 作用域判定与快捷保存契约

`activeProfile` 不是由组件本地 state 猜出来的，而是由任务/内容控制器以显式枚举提供：`assistant`、`coding`、`none`。它应在标签创建时随任务类型确定，并在运行中的任务上保持不变；“用户正在问代码问题”不是切到 coding 的依据。旧的、无法可靠分类的标签宁可返回 `none`，只保留“模型设置”入口，不提供可写快捷切换。

快捷选择必须走一个 profile-scoped 的轻量保存 API（例如 `QuickSaveMaclawLLMProfile(profile, providerID, model, revision)`），而不是把前端旧配置拼回去全量保存。该 API 与设置页使用同一校验、CAS revision、assistant 兼容投影和事件机制；成功后返回最新 panel state。这样可避免侧栏与设置页同时打开时，快捷选择覆盖另一份草稿或把 coding 模型写入 provider 的兼容 `Model` 字段。

- assistant 快捷菜单允许一次选择“服务商 + 该服务商模型”，并只写 assistant；切换服务商后先显示该服务商的建议模型，用户确认模型后才提交，避免短暂保存一个空模型。
- coding 独立菜单采用同一流程，只写 coding；若当前模型不在新服务商的模型目录中，显示待选择状态，不能提交空模型或隐式沿用旧服务商的模型 ID。
- coding 跟随时所有可写候选均隐藏，只展示只读摘要与“在设置中改为独立模型”。该入口打开设置后应将焦点定位到 coding 行的跟随开关，而不是直接关闭跟随。
- 保存冲突、服务商被删除、模型为空、健康状态未知时均保留菜单和原配置，不做乐观的永久 UI 切换；错误用行内提示或 toast 说明“刷新后重试”。

菜单以 portal/fixed popover 挂到应用根部，定位锚点为 chip；打开后冻结当前 profile 快照，Esc 关闭并将焦点还给 chip。窄宽度下 chip 可只保留图标与“助手/编程”短标签，但其 `aria-label` 必须包含完整服务商和模型；不把长模型 ID 撑成第二行。

### 4.7 侧栏系统状态、服务商与用量

系统状态是全局信息，不应只因用户切换标签就把另一套模型从界面上消失。因此采用“**配置双显，统计随上下文聚焦**”的规则：

```text
系统状态
LLM  在线
助手   OpenAI · gpt-5.2
编程   跟随助手                         （同配置时）
-- 或 --
助手   OpenAI · gpt-5.2
编程   DeepSeek · deepseek-coder        （独立时）

当前视图：编程 Agent · DeepSeek
本次/今日用量、缓存命中、Hub 额度
```

具体规则：

1. 现有单一 `LLM 在线/离线` 目前实际探测的是 assistant，不能在双档案后继续作为“两个模型都可用”的总判断。它改为 `LLM` 总标题加两行独立健康状态：助手行和编程行各自显示有效选择及 `已连接 / 未验证 / 不可用`。标题**只反映当前视图**：assistant 为 `LLM · 助手可用/未验证/不可用`，coding 为 `LLM · 编程可用/未验证/不可用`（跟随时仍以 assistant 的健康结论为准），`none` 只显示 `LLM 状态`；不再以“两者均可用”改变标题。绝不把 assistant 在线误标为 coding 在线。两行都可点击，分别打开设置页并定位 assistant/coding 档案。
2. coding 跟随时第二行显示“跟随助手”，同时以次级文字显示实际 `服务商 · 模型`；如果空间不足，保留“跟随助手”与 tooltip，不能伪装成两个独立服务商。
3. coding 独立时完整显示第二套 `服务商 · 模型`。服务商不同、模型不同、离线或配置无效均要有各自状态；不要把两者合并成单一“当前服务商”。
4. 侧栏底部原有的服务商下拉和模型切换不再承担全局切换职责。它改为显示“当前视图”的 profile，并与底部 chip 保持相同作用域；在 coding 跟随时只读并引导到设置，在 coding 独立时可快捷切换 coding 档案。没有可推导执行模式的页面不显示这个可写菜单，双行状态仍保留。
5. token、缓存、Hub 额度等密集指标只聚焦**当前活动标签对应的 profile**，并在指标前标注 `当前视图：助手` 或 `当前视图：编程`。这部分随 tab 切换是合理的，因为它回答的是“我正在使用什么、消耗了什么”。两套档案的配置状态则始终可见。
6. 同一个服务商被两套档案使用时，服务商总额度可共用展示；但 token 用量必须按 `profile + provider + final_model` 记录和查询，不能再仅按 provider 聚合。`route_source` 作为请求/轨迹字段保留，并可在详细用量页筛选；侧栏只显示当前 profile 聚合，以免为一次路由切换制造误导性的多组数字。全局总量可在展开的用量页展示，侧栏不堆叠四套数字。
7. Hub 额度属于服务商/账户维度。当当前 profile 使用 Hub 时展示其额度；另一档案也使用不同 Hub/账户时，不在模型状态行塞入额度或在侧栏主面板并列两组网格。用户可点击该档案状态行进入设置或用量详情查看其账户额度，避免把“配置状态”与“账户余额”混在同一行。

这意味着“随 tab 切换”只用于快捷操作与用量焦点，不用于隐藏全局的双档案配置。用户从编程标签回到普通聊天标签时，快捷 chip、下拉候选、`当前视图` 用量和 Hub 卡片切换到 assistant；侧栏的两行模型状态仍保持不变。

#### 4.7.0 快捷入口的统一交互矩阵

底部 chip 与侧栏快捷菜单必须使用同一个菜单组件、同一份冻结的 profile 快照、同一个 `QuickSaveMaclawLLMProfile` 调用；两处只允许锚点与可用空间不同。这样可以避免一处把服务商切换理解为 assistant，另一处理解为 coding。

| `activeProfile` | 当前档案状态 | chip / 侧栏快捷菜单 | 写入与反馈 |
| --- | --- | --- | --- |
| `assistant` | 已配置 | 显示 `助手 · 模型`；可选服务商，随后必须选模型 | 仅保存 assistant；成功后刷新两条状态（coding 若跟随则同步变化） |
| `assistant` / `coding` | 未配置或无效 | 显示不可写状态摘要和 `管理服务商/打开设置`；不猜测可用模型 | 不写入；先完成服务商连接或完整模型选择 |
| `coding` | 独立 | 显示 `编程 · 模型`；可选服务商，随后必须选模型 | 仅保存 coding；不改 assistant 镜像、不清理无关状态 |
| `coding` | 跟随 assistant | 显示 `编程 · 跟随助手`；菜单仅显示只读有效摘要和“在设置中改为独立模型” | 不写入；跳转后聚焦 coding 跟随开关 |
| `none` | 任意 | 不显示可写 chip 与下拉，仅保留“打开模型设置” | 不写入 |

菜单打开时冻结 `{ activeProfile, revision, effectiveSummary }`；成功保存后以 API 返回的最新 panel state 重绘。遇到 revision 冲突时保留待选的 provider/model，并提供“刷新并重试”；服务商删除、模型目录刷新失败或模型校验失败时保留原摘要、清晰标出不可提交原因。不得把一次临时选择持久化为乐观状态。正在运行的任务上，菜单顶部只提示“当前任务继续使用启动时的模型”，不提供热切换承诺。

#### 4.7.1 状态汇总规则

侧栏标题不再是含糊的单一“LLM 在线”。它由活动 profile 的健康状态决定，并始终保留两份档案明细：

| 当前视图 | 标题 | 助手行 | 编程行 |
| --- | --- | --- | --- |
| assistant | `LLM · 助手可用/未验证/不可用` | assistant 的实际状态 | coding 的独立状态，或“跟随助手” |
| coding（独立） | `LLM · 编程可用/未验证/不可用` | assistant 的实际状态 | coding 的实际状态 |
| coding（跟随） | `LLM · 编程跟随助手`，并使用 assistant 的实际健康结论 | assistant 的实际状态 | `跟随助手 · 服务商 · 模型` |
| none | `LLM 状态`，不作总体在线承诺 | assistant 的实际状态 | coding 的实际状态/跟随关系 |

“未验证”表示配置完整但尚未成功探测；“不可用”表示配置错误、服务商被删除或最近一次可判定探测失败；网络短暂失败不能把另一套档案标为不可用。跟随状态不是第二次独立探测，也不能显示出与 assistant 不一致的健康色。有效配置相同但 coding 已明确独立时仍写“编程”，不要错误标成“跟随助手”。

建议数据契约为每个 effective profile 返回 `{ profile, configured, inheritsAssistant, providerID, providerDisplayName, model, health, checkedAt, reasonCode }`；`reasonCode` 用于前端本地化，不能向前端返回 URL、密钥、原始网关错误或诊断请求体。侧栏在状态变更事件到达时按这份摘要刷新，避免继续以旧的 assistant-only `maclawLLMOnline` 推断两行状态。

#### 4.7.2 侧栏信息层级和空间行为

侧栏将“配置状态”和“当前视图数据”拆成两个紧凑区块，中间只用普通分隔线，不额外套卡片：

```text
系统状态                         [打开模型设置]
LLM · 编程可用
● 助手     OpenAI · gpt-5.2
↳ 编程     跟随助手 · OpenAI · gpt-5.2

当前视图：编程 Agent
DeepSeek · deepseek-coder          [快捷菜单]
本次用量 / 今日用量 / 缓存命中
Hub 额度（仅当前视图服务商属于 Hub 时）
```

第一块始终双显，并且两行都是可聚焦的设置入口；第二块才随活动标签切换。标题、当前视图标签和快捷菜单必须使用同一个 `activeProfile` 来源，避免出现“标题显示编程、菜单却改助手”的错位。当前视图为 `none` 时，隐藏快捷菜单与 profile 用量，保留双行状态和“打开模型设置”。

在窄侧栏中，状态行用 CSS grid 固定左列（状态点 + 档案名）与弹性右列（中间省略的 `服务商 · 模型`）；跟随标记优先保留，实际服务商与模型允许省略并用 title/tooltip 补全。不可把两个服务商名称都塞进标题，也不使用“当前服务商”这类单数文案。Hub 额度只对当前视图的 provider/account 展示一次；若两个档案各自使用 Hub，只随 tab 改变焦点而不把两个额度面板并列堆叠。

#### 4.7.3 健康状态的生命周期与并发规则

健康状态不是配置的一部分，也不能由某一次 assistant 探测结果推断全部档案。为避免“已换模型但仍显示旧模型在线”或慢请求回写新状态，健康缓存必须有明确的生命周期：

1. 缓存键为有效选择的 `profile + provider_id + model`；coding 跟随时直接复用 assistant 的缓存键和结果，不发起第二次请求。coding 显式独立即使恰好与 assistant 选择相同，也仍保留 `coding` 归属，方便切换独立/跟随时不混淆。
2. 保存档案、删除/编辑服务商连接、OAuth 状态变化或模型变化后，立即废弃受影响键的旧结果，并显示“未验证”；不能先显示旧的“已连接”。正在运行的任务继续使用启动快照，其状态不被设置页的健康结果反向修改。
3. 显式“测试连接/重新检查”只探测其目标档案；应用启动或后台刷新可并行探测 assistant 与独立 coding，但必须去重跟随档案。每次探测带递增序号或配置 revision，只有与当前有效选择一致的最新结果可以写回缓存和事件。
4. 本地可判定的空模型、悬空服务商等为 `invalid`；认证、协议或明确的服务端拒绝为 `unavailable`；超时、断网、5xx 等暂态故障保留为 `unverified` 并给出可重试的 reason code，绝不影响另一档案的状态。前端只接收枚举、时间和 reason code，不接收 URL、凭据、请求体或原始错误文本。
5. `ProfilePanelState` 与 `llm-profiles-changed` 事件均携带两份无敏感健康摘要。侧栏标题只读取 `activeProfile` 对应摘要；`none` 只写“LLM 状态”，不作在线承诺。两条状态行都可打开设置，但不应因为点击状态行而隐式切换档案或修改模型。

## 5. 后端与前端实施计划

### 阶段 A：建立配置契约与迁移测试

1. 在 `corelib.AppConfig` 增加 provider `ID`、profile 类型及安全的 normalize/validate 函数；用 `*MaclawLLMProfiles` 区分“缺失的旧配置”和明确保存的 `InheritAssistant=false`。
2. 建立惰性旧配置解析、首次受控写入时的 provider-ID/profile 物化，以及 assistant 平铺字段兼容投影逻辑；纯读取不得写盘。把普通 turn 的启动快照与编程根任务的启动快照作为这一阶段的配置不变量一并覆盖测试。
3. 更新配置 merge、导出、脱敏、远程同步和恢复逻辑；profile 只允许 `provider_id/model/inherit_assistant`。校验器必须拒绝重复 ID、空 ID（新格式）、大小写不敏感重名和悬空引用。
4. 对服务商改名、删除和 Hub 服务商同步建立原子引用更新；改名只改展示名。删除 assistant 有效引用时必须同一请求提供替代 `ProviderID + Model`；删除独立 coding 有效引用时可提供替代选择或显式切回跟随。不得将无效引用持久化为“可恢复错误信息”。

### 阶段 B：运行时解析与调用点收口

1. 提取按 provider ID 物化的逻辑，新增 `ResolveMaclawLLMProfile`、`GetCodingLLMConfig`，并使 `GetMaclawLLMConfig` 解析 assistant；resolver 返回基础 config 与不可变 profile attribution。
2. 清点并分类所有 `GetMaclawLLMConfig`、`MaclawLLMCurrentProvider`、`MaclawLLMModel` 与 `SaveMaclawLLMProviders` 调用：普通助手保持 assistant；CodingSubAgent、本地/远程/嵌套编程入口全部改用 coding。
3. 使 coding 路由以 coding 基础配置为输入。模型路由若覆盖基础模型，要在运行记录中标记最终模型与来源（profile/route），但不记录密钥或 URL。
4. 明确副作用：当前 `SaveMaclawLLMProviders` 与 `PatchConfigFields({maclaw_llm_current_provider})` 会无条件清理**全局** MoA sticky，因此新 `SaveMaclawLLMProfiles` 不能复用它们的副作用。只有 assistant profile 的实际选择发生变化时，才调用现有全局清理；仅保存 coding 档案、保存 assistant 的无关字段或仅刷新 provider 目录都不得清理。两种保存均发出无敏感数据的 `llm-profiles-changed` 事件。

### 阶段 C：原子 API、并发与 Wails 契约

1. 新增 `GetMaclawLLMProfilePanelState()`，一次返回服务商列表（含 ID、展示名、可用模型和无敏感健康摘要）、两份持久化档案、两份 effective summary、路由提示和 `revision`。
2. 新增 `SaveMaclawLLMProfiles(profiles, revision)`。后端校验 provider ID 存在、模型非空、Hub 可用性和 revision；冲突返回可识别错误，绝不让较晚的页面覆盖较新的设置。保存时在同一个 CAS 事务生成 assistant 兼容投影。
3. 新增 `TestMaclawLLMProfile(profileDraft)`，仅测试草稿，不保存、不更新当前选择；返回可本地化的 reason code，不能返回 URL、凭据或网关原始错误。
4. 新增 `QuickSaveMaclawLLMProfile(profile, providerID, model, revision)`，供底部 chip 和侧栏复用；它不得绕过 profile resolver、revision、校验或 assistant 镜像。coding 跟随状态下直接拒绝写 coding selection，前端据此转到设置而非自动解除跟随。接口必须返回最新 `ProfilePanelState`，供菜单用服务端真相结束乐观状态。
5. provider 管理的重命名/删除 API 必须接收当前 revision 与引用处置策略。删除 assistant 有效引用时只接受“替换为另一个 provider + model”；删除独立 coding 有效引用时接受“替换”或“切回跟随”；不允许写入悬空引用。若被引用仅是 coding 跟随恢复选择，可清空它而不改变有效配置。
6. 保留 `SaveMaclawLLMProviders(providers, current)` 作为过渡兼容接口；其 `current` 只同步 assistant 镜像。新模型分配、provider 删除和快速入口不得调用这个无 revision 的旧接口；兼容调用必须在服务端转入同一 CAS 校验路径，或明确拒绝双档案配置下的写入。
7. 通过生成流程更新 Wails JS/TS bindings；若构建环境阻塞生成，需要在 CI 增加 bindings schema/check，避免手工补齐的绑定静默漂移。
8. 新增 profile 维度的用量查询与事件契约。新记录主键为 `profile + provider_id + final_model`，并附 `provider_display_name`、`route_source`；保留现有 provider-only map 的历史读取和汇总接口。升级前的 provider 维度历史数据标记为 `legacy/unattributed`，不尝试臆测属于哪个 profile 或模型；侧栏默认只显示新 profile 聚合，必要时以“含历史汇总”提示。Hub 额度仍按 Hub 账户/服务商查询，不能被 profile 维度重复扣减。
9. 以每个 effective profile 的无敏感健康摘要替代当前 assistant-only 在线布尔值；健康缓存与刷新事件必须按 profile + provider ID + model 区分，跟随 coding 复用 assistant 结果。实现独立的探测/缓存层，而不是在前端把旧 `PingMaclawLLM()` 的结果复制给两行；保留旧接口仅供旧 onboarding 调用，并使其语义明确为 assistant。

### 阶段 D：模型分配界面

1. 以单个 `ProfilePanelState` 驱动 `LLMConfigPanel`，减少多个并行读取对配置锁的竞争。
2. 实现两行模型分配表单、草稿状态、跟随状态机、有效摘要、保存/冲突/未保存离开提示以及响应式布局。
3. 复用既有 `ProviderModelCombobox`、服务商 logo、错误提示和 toast，不再复制一套模型发现实现。
4. 将现有“Configure”操作重命名为“管理服务商”；检查二维码、token 用量、OAuth、Hub 服务权益等辅助 UI，只在它们真正对应的服务商管理上下文展示。
5. 补全简中、繁中、英文文案和可访问性属性。

6. 改造聊天底部 quick model chip 与侧栏 provider 下拉：两者从活动内容控制器给出的显式 profile scope 推导行为；普通助手直接保存 assistant，编程独立模式直接保存 coding，编程跟随模式只读并跳转到设置，未知 scope 不显示可写入口。两个入口复用同一个 profile-scoped 菜单逻辑和 `QuickSaveMaclawLLMProfile`，禁止各自维护不同的切换语义。

7. 改造侧栏系统状态为“助手/编程”双状态摘要，加上“当前视图”用量焦点。双状态始终可见，只有快捷菜单和用量/额度区域随 tab 变化。

### 阶段 D.1：先建立可验证的活动档案边界

1. 在 `AITab`（或等价的不可变任务元数据）增加显式 `executionProfile: 'assistant' | 'coding' | 'none'`；新建 local/IM/普通工作流标签写入 `assistant`，本地/远程 `coding_dev` 标签写入 `coding`，系统页和无法判定的历史标签写入 `none`。
2. 为长生命周期的正在运行任务保存启动时 profile/config 快照；切换标签只能改变 UI 焦点，不能改变已启动任务或嵌套 CodingSubAgent 的归因。
3. 将此一个 `activeProfile` 向上发布给 App shell，再同时下发给底部 quick bar、侧栏标题、快捷菜单、profile 用量和 Hub 额度选择器；禁止这些组件各自根据 tab 名称、路由名或内容文本推断。
4. 在接入快捷写入前先添加四组组件/集成测试：普通聊天、独立 coding、跟随 coding、未配置/无效 profile；`none` 必须无可写入口。这一步完成后才替换旧 `SetMaclawLLMCurrentModel` 和 `PatchConfigFields({ maclaw_llm_current_provider })` 快捷调用。

### 阶段 E：观测、文档和发布控制

1. 在普通 Agent 和 CodingSubAgent 的使用统计、轨迹与启动事件中记录基础 `provider_id/profile/model`、最终 `ProviderName/Model` 和 route source；严格排除 URL 与凭据。
2. 为开发诊断提供“基础档案选择 → 路由后最终选择”的只读快照，便于定位用户看到的配置与实际请求不一致的问题。
3. 更新用户手册、配置示例与 VS Code/Hub 兼容说明：它们本期读取 assistant 镜像，编程独立档案只由 MaClaw 编程运行时消费。
4. 先以默认跟随的灰度配置验证升级兼容性，再开放独立编程档案编辑 UI；出现配置解析失败时可关闭新 UI，但 resolver 必须仍能按旧字段安全回退。

## 6. 测试与验收

### 6.1 配置、迁移和并发

- 无 profiles 的老配置解析为“assistant=旧当前选择、coding=跟随”，且普通读取不写盘。
- 首次保存物化 profiles 并正确同步旧 assistant 镜像，不改变 OAuth/SSO 凭据。
- 同一个服务商、不同 profile 模型能各自物化为不同 `MaclawLLMConfig.Model`。
- coding 跟随时改 assistant 会立即影响后续 coding 解析；coding 独立时不受影响；重新跟随即恢复 assistant。
- 旧名称引用迁移到 provider ID、重复/大小写冲突名称、服务商改名、删除、别名归一、Hub 不可用、模型为空和未知服务商均产生稳定且可恢复的结果；读旧配置不会写盘。
- 两个设置窗口基于同一 revision 保存时，后保存者得到冲突而不会覆盖先保存者。
- provider 管理与 profile 保存交错执行时，删除/改名不能产生悬空引用；删除 assistant 引用必须替换，删除独立 coding 引用可替换或切回跟随；旧 `current + Model` 投影始终等于 assistant，不会因 coding 保存改变。

### 6.2 请求级运行时验证

- 普通 Agent HTTP 请求使用 assistant 档案模型。
- 本地、远程、任务编排和嵌套 CodingSubAgent 的请求均使用 coding 档案模型。
- 推理/视觉路由仍能在 coding 基础档案上覆盖；没有可用路由时回到 coding 基础模型。
- 全局 Thinking Mode 正确应用于两类有效配置。
- 任务运行中修改档案不会改变已发起的请求；后续 turn 才使用新模型。
- 子 CodingSubAgent 继承父任务启动时已解析的 coding 选择；父任务运行期间保存设置不会让嵌套任务混用新旧配置。
- 普通聊天在一个 turn 完成后保存设置，下一 turn 使用新选择；正在流式返回的同一 turn 继续使用旧快照。

### 6.3 前端和可访问性验证

- 首屏加载稳定展示两个档案摘要，不闪现“未配置”。
- 关闭跟随、编辑独立草稿、取消离开、保存、重开跟随与恢复上次独立选择的状态机正确。
- 跟随开启时页面没有禁用的服务商/模型输入框；独立时表单按桌面和窄窗口布局正确展开。
- “管理服务商”与模型分配不存在嵌套弹窗；OAuth 返回后草稿保持。
- 保存 payload 不含凭据，编辑一个档案不覆盖另一个档案。
- 键盘焦点、组合框行为、错误提示和 reduced-motion 行为符合现有产品控件标准。
- 普通聊天标签的底部 chip 只改变 assistant；编程标签的 chip 在独立模式只改变 coding，在跟随模式不可隐式解除跟随。
- 标签的 `assistant/coding/none` scope 由任务类型稳定决定；含代码文本的普通对话不误切到 coding，无法分类的旧标签没有可写快捷入口。
- 同时打开设置页和快捷菜单时，快捷保存、完整保存和服务商删除均遵守同一 revision；任何一个陈旧动作只得到冲突提示，并可刷新后保留草稿，不覆盖另一处变更。
- 从普通聊天切换到编程任务、再切回时，快捷 chip、侧栏“当前视图”用量和 Hub 额度焦点正确切换；助手/编程两行全局状态始终可见。
- 侧栏标题只陈述当前视图的健康结论；两行状态独立且 coding 跟随时严格复用 assistant 的健康结果，不能因 assistant 在线而把独立 coding 标为在线。
- 修改服务商、模型或 OAuth 状态后，旧健康结果立即失效为“未验证”；慢探测、并行探测和设置 revision 变化均不能将过期结果写回。暂态网络错误只提供重试提示，不把任一未被探测的档案标成“不可用”。
- 两个档案共用同一服务商但采用不同模型时，用量统计不会错误合并为同一模型；路由覆盖会记录最终模型而不会篡改基础档案摘要。
- 旧 provider-only 用量升级后仍可见为“历史汇总”，不被错误归属到 assistant 或 coding；Hub 额度只按账户/服务商显示一次。
- 当 assistant 在线而独立 coding 不可用（及反向情况）时，侧栏两行健康状态、标题和快捷入口均不误导为“双双可用”。
- Go 单测、前端组件测试、TypeScript 检查、Wails binding 校验和关联回归测试全部通过。

### 6.3.1 集成与发布门槛

- 同时打开设置页、底部菜单和侧栏菜单：任一入口保存后，另外两个入口收到 `llm-profiles-changed` 后刷新为同一 revision；陈旧菜单不能覆盖较新配置。
- assistant 引用服务商的删除流程必须覆盖“替换”“取消”；独立 coding 引用服务商必须覆盖“替换”“切回跟随”“取消”。取消后配置与恢复草稿均不变。
- 同一服务商被 assistant/coding 使用不同模型时，侧栏两行、快捷菜单和用量聚合均分别显示；Hub 额度只按当前焦点 provider/account 显示一次。
- 默认跟随升级、独立 coding、回到跟随三条路径均做一次真实请求级 smoke test，并确认进行中请求没有变化、下一次请求才使用新模型。
- 在构建机运行 bindings 检查和关键前端测试；若 Wails 自动生成不可用，发布不得仅依赖手工 bindings，必须有 schema/签名校验阻断漂移。

### 6.4 发布验收标准

1. 升级后不做任何操作时，编程 Agent 与普通 AI 助手继续使用同一服务商和模型。
2. 用户可关闭跟随，为编程 Agent 独立选择服务商和模型；普通助手变化不会修改已保存的独立档案。
3. 两档案可复用同一服务商而选择不同模型，不要求重复录入密钥或重新 OAuth 登录。
4. 所有编程执行路径使用 coding 档案；所有普通 Agent 路径使用 assistant 档案；实际请求、用量和轨迹可验证最终选型。
5. 无效选择不会静默降级为其他模型；用户能在对应行看到问题和修复操作。
6. 模型分配在现有设置页中可直接理解，服务商连接维护与模型使用选择清楚分离，窄窗口与键盘操作均可完成任务。
7. 底部快捷模型入口和侧栏快捷入口始终只操作当前标签所属档案；编程 Agent 的“跟随”不会被快捷选择悄悄关闭。
8. 侧栏同时展示助手与编程 Agent 的有效模型状态；仅用量、Hub 额度和快捷菜单随当前标签聚焦。
9. 服务商改名不影响两个档案；删除 assistant 引用必须在同一操作中完成替换，删除独立 coding 引用必须替换或切回跟随；不得持久化悬空的无效状态。
10. 升级前的 provider 汇总用量保持可查但明确未归因；新的 assistant/coding 用量和路由后的最终模型可分别核对，Hub 额度不被重复计算。

## 7. 风险与控制措施

| 风险 | 控制措施 |
| --- | --- |
| “服务商模型”与“档案模型”混淆 | 将服务商 `Model` 定义为默认/兼容投影；所有新请求必须经过 profile resolver，coding 保存不得改写该共享字段 |
| 以服务商名称作引用，改名后档案失效 | provider 使用稳定 ID；名称只展示且大小写不敏感唯一；旧配置只在受控写入时迁移 |
| 服务商编辑意外改变另一档案 | 服务商管理和模型分配分离；档案保存只提交 provider/model/follow |
| 编程入口遗漏、退回普通模型 | 以 `GetCodingLLMConfig` 作为所有 CodingSubAgent 构造入口，并进行请求级覆盖测试 |
| 页面多处打开造成陈旧写入 | panel state 带 revision，保存采用乐观并发校验并提示刷新 |
| 路由覆盖让用户误以为配置无效 | UI 只给相关提示；轨迹记录基础与最终选择；本期不暗中改变路由优先级 |
| 旧扩展或 Hub 读取平铺字段 | 继续将 assistant 档案镜像到旧字段，明确这是一条兼容契约 |
| 保存影响进行中的高价值任务 | 配置在 turn 创建时快照；保存仅作用于后续请求 |
| 双档案却沿用单一在线状态 | 每个 effective profile 独立探测/展示；标题只汇总，不把 assistant 健康度借给 coding |
| 新统计与旧 provider 汇总混杂 | 新事件携带 profile/provider ID/final model；历史数据显式标为未归因，Hub 额度继续按账户 |

## 8. 建议交付顺序

先完成阶段 A、B 及其请求级测试，确保默认跟随在没有 UI 的情况下完全兼容；随后交付阶段 C、D 的原子 API 与模型分配界面；最后完成阶段 E 的观测、文档、灰度与回归。此顺序使 UI 上线前已经能验证“编程路径不会误用普通助手模型”，也让任何回退都保留现有单模型行为。
