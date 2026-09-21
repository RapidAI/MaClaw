# 文档自动注入截断与续读：机制缺陷与改进计划

日期：2026-09-20
范围：GUI 选文件 / IM 附件的 `auto_extract` 注入、office/read_document 续读、语义 `document.read` 投影、PDF 原生抽取
状态：注入层近拟合与文首/文尾已落地；**续读能力闭环仍未闭合**，本计划面向机制缺陷而非再调预算数字

## 修订记录

| 版本 | 改动 |
|------|------|
| v1 | 初版：KNOSYS-D-26-18030 事故链、四条机制缺陷、已落地补偿、计划 A–D、PR 计划 |

## 1. 事故事实（已核对）

2026-09-19 06:24 桌面「默认任务」评审论文：

| 项 | 值 |
|---|---|
| 文件 | `C:\Users\ma139\Downloads\KNOSYS-D-26-18030_reviewer.pdf` |
| 选择方式 | GUI 路径标记 `[用户选择的本地文件路径]`，**不是**带字节的 MessageAttachment |
| 原生抽取 | 51 页、125396 字符，含 Limitations / Conclusion / References |
| 注入结果 | `injected_chars=106666 truncated=true next_offset=106666` |
| 模型表面 | 语义/light：`knowledge_search`、`memory_recall`、`tools_search`；另有 `invoke_*` 受信文档读取 |
| 续读失败 | 投影失败；`read_file` → `trusted_file_read_path_rejected`（Downloads 不在任务目录）；`tools_search` 达上限；office 从未出现在本轮列表 |
| 用户可见 | 模型声称「第 44 页之后无法提取」，并询问是否授权本地脚本 |

106666 来自预算公式，不是解析器丢页：`ContextLength=400000` → `EffectiveContextTokens=320000` → `DocumentAutoExtractBudget` 总额度 160000、单文件 2/3 = 106666。全文 125396 **小于总额度**，被单文件份额截断。

已落地补偿（注入层，不解决续读闭环）：

- 最后一份文档用剩余总额度；超出 ≤24k 视为短尾整篇留下
- 超出更多时文首+文尾（文尾标 `# tail: offset=`），续读 `max_chars` 只覆盖中间缺口
- 空页不再静默丢弃（`## Page N` + `[empty page]`）
- IM 单附件走同一套窗口函数

## 2. 机制缺陷（设计错误）

**把「截断后的续读」当成模型义务，而宿主自己既制造截断、又拒绝把续读工具当作该截断的能力证据。**

`file_path_expand.go` 的治本注释写的是：注入后「Tool calls remain available for paging truncated extracts」。生产路径同时保证了三件事，使这句话不成立：

1. 系统提示要求 `truncated=true` 时调用 `office(action="read_document", offset=next_offset)`。
2. 路由测试 **明确禁止** 仅凭路径标记 / `truncated=true` 钉住 office（`TestRoute_GUIDocxPathDoesNotGrantOffice`：「host presentation text alone is not capability evidence」）。
3. 语义表面把 office 标成「可请愿」，须 `tools_search` 发现；light allowlist 不含 office；请愿与检索共享有限次数。

这与 SSH 可用性事故同构：有状态生命周期（读 → 分页剩余 → 后续回合）被塞进为无状态能力设计的每回合授予模型，撤掉恒可用 legacy 表面后没有等价退路。SSH 是 connect/exec；这里是 inject/page。

四条独立断裂：

### D1. 指令与能力面分裂

| 层 | 行为 |
|---|---|
| 提示 | 截断必须 office 分页 |
| 路由 | 截断文本不得授予 office |
| 语义 fence | office 可请愿，须 tools_search |
| light | office 不在 allowlist |

模型按提示去调一个本轮列表里没有的名字；发现它要烧 `tools_search`；达上限后只能问用户。**提示里的恢复路径不是能力面上的恢复路径。**

### D2. 三种文档身份互不续接

| 身份 | 谁持有 | 续读入口 |
|---|---|---|
| GUI 路径标记 | 用户消息里的绝对路径 + auto_extract 文本 | 提示说 office(file_path=...) |
| 受信附件 ArtifactRef | IM/通道字节，`UniqueTrustedInputCount==1` | 语义 `document.read` / `invoke_*`，无 path 参数 |
| 任务工作区文件 | `resolvePathInsideWorkspace` | `read_file` / trusted file read |

本次文件在 Downloads，属于第一类。第二类要求附件字节，路径选择不产生 ArtifactRef → `trusted_document_input_missing` / 投影失败。第三类拒绝工作区外路径 → `trusted_file_read_path_rejected`。

**截断续读的 `next_offset` 绑在路径身份上，本轮唯一的文档读取器绑在附件身份上。**

### D3. 截断是宿主策略，续读不是宿主状态机

对比已做对的 `toolresult.Handle` + `read_tool_result`：溢出结果有 handle、有 offset、工具留在表面上。

auto_extract 相反：

- 宿主按 rune 预算切开，把 `next_offset` 写进用户消息
- 不创建可寻址的 spilled handle
- 历史回合 `StripAutoExtractBodies` 丢掉正文，只留「之前已自动解析」
- 后续回合既没有正文，也没有 office，更没有 handle

分页状态活在模型记忆里。模型选择问用户而不是再请愿时，生命周期结束。

### D4. 注入预算与真实请求窗口不是同一个数

`ExpandUserSelectedFilePathsWithContext` 使用 `MaclawLLMConfig.EffectiveContextTokens()`（本次 400k×0.8=320k）。活动供应商配置可为 200k / 128k。注入按更大窗口计，请求按更小窗口发，提供商侧可能再截用户消息——表现为另一类「输入不完整」。注入层无法区分「宿主故意截断」与「提供商静默截断」。

附带：PDF 原生抽取在「部分页有字」时不会对空白页走 OCR。图页/扫描尾页会被标 `[empty page]`，仍无字。这是抽取质量问题，与 D1–D3 独立，但会被模型说成「无法提取」。

## 3. 不变量（目标）

> 用户本回合选中的文档，在截断后仍可被宿主完成阅读，不依赖分类器是否钉住 office、不依赖 tools_search 配额、不依赖文件是否位于任务工作区。

四条可测场景（对标 SSH `TestSSHAvailabilityIndependentOfClassifierAndSession`）：

1. **注入已完整**：全文 ≤ 剩余额度或短尾近拟合 → `truncated=false`，模型无需任何文档工具。
2. **注入截断 + 语义表面**：`truncated=true` 时，本轮必须出现**一种**可按 `next_offset` 续读的工具（office、read_document、或已绑定该文件的 `invoke_*`），且不消耗 tools_search。
3. **路径选择而非附件**：Downloads 等任务目录外路径，续读不得因 `trusted_document_input_missing` 或 `trusted_file_read_path_rejected` 失败。
4. **后续回合**：历史剥离正文后，仍能凭路径 + 上次 `next_offset` 续读（handle 或会话级 continuation），而不是要求用户重新选文件。

## 4. 改进计划

每项带验收标准。无验收标准的条目不算计划项。

### A. 立即（闭合续读，本周）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| A1 | **截断即能力证据**：当本回合 auto_extract 出现 `truncated=true` 或 `error=`，宿主把 `office`/`read_document`（或已绑定该 path 的语义 document.read）加入本轮可见列表。提示文本本身仍不是能力证据；**截断结果是宿主自己的策略输出**，与 UIC 文本检索分开。修改 `TestRoute_GUIDocxPathDoesNotGrantOffice`：无 truncated 仍不钉；有 truncated 必须钉 | 单测：路径+完整注入 → 不钉 office；路径+truncated=true → 列表含续读工具。回归：无文件的闲聊仍不钉 office |
| A2 | **路径选中文件升级为受信输入**：GUI 路径标记在 auto_extract 同时登记为 TrustedInput（只读快照，字节来自 `SnapshotBoundedDocumentInput`），使语义 `document.read` 的 `invoke_*` 能对**同一份文件**做 offset 分页，schema 仍不含 path | `UniqueTrustedInputCount` 对「仅路径选中、无 IM 附件」为 1；invoke 续读 `offset=next_offset` 返回与 office 相同的 rune 窗口；Downloads 路径不再投影失败 |
| A3 | **禁止把截断说成解析失败**：注入块已有 `truncated=true` 时，系统提示增加一句硬规则：不得对用户说「无法提取/解析失败」；应续读或声明已基于前缀+文尾作答。轨迹断言：截断回合的最终用户可见文本不含「无法提取」除非 `error_class` 为 malformed/encrypted | 用本次论文轨迹作回归夹具：注入 truncated 后模型面有续读工具；golden 禁止「无法提取」 |

### B. 短期（状态机与窗口一致，1–2 周）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| B1 | **auto_extract spilled handle**：截断时写入与 `toolresult.Handle` 同族的 continuation（path 快照 digest、total_chars、next_offset、tail_offset）。`read_tool_result` 或专用 `read_document_continuation` 按 handle 分页，不把 offset 只写在会被 Strip 的用户消息里 | 第二回合 Strip 正文后，凭 handle 仍能读缺口；digest 变化则 `source_changed`，禁止静默混版本 |
| B2 | **注入预算用实际请求窗口**：`ExpandUserSelectedFilePathsWithContext` 的 contextTokens 取**本回合将发往的供应商** `context_length` 的 Effective 值，而不是全局 `maclaw_llm_context_length` 与供应商不一致时的较大者 | 供应商 128k / 配置 400k 时，注入总额度按 128k×0.8 计；单测钉住；禁止按 320k 注入后在 128k 请求里再被提供商截断 |
| B3 | **部分页 OCR 回退**：`renderPDFPageExtracts` 在 `gotAny && 存在 empty/error 页` 时，对空页走已有 OCR 管道（与 knowledge `extractPDFOCRNodesWithNativeFallback` 对齐），而不是留下 `[empty page]` 当作终态 | 夹具：前 40 页有字、后 5 页为图；空页产生 OCR 文本或明确 `ocr_failed`，不再是静默空页；全扫描件仍走「需 OCR」总失败 |

### C. 中期（与语义框架对齐）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| C1 | **document.read 可用性策略**：按 `semantic-tool-routing-design-zh.md` 与 SSH 计划 C2 的草案，为 `document.read` 声明 `degraded: keep`、`host_absent: publish_if_active_document`、`legacy_overlay: office`。截断/活动文档视为 host_present，不是 UIC 文本命中 | 降级回合仍能续读活动文档；B3 家族矩阵里 document.read 四场景有结果（场景 3 overlay office 适用） |
| C2 | **单一续读适配器**：office(read_document) 与语义 invoke_document_read 共用 `ToolReadDocumentWithContext` + 同一 snapshot digest。提示只保留一种续读名字（或由表面渲染成当前列表里的那个名字） | 删除提示中写死 `office(action="read_document")` 而列表是 `invoke_*` 的分裂；双入口对同一文件同一 offset 字节级一致 |
| C3 | **light 表面例外**：light allowlist 默认仍无 office；**本回合存在 truncated auto_extract 或活动文档 handle 时**，light 不得剥掉续读工具（对标 ask_user：非效果、为闭合任务所必需） | `FilterToolDefsForLightTurn` + 语义 Filter 在 truncated 回合保留续读名；无文档的 light 回合仍无 office |

### D. 观测

| 项 | 内容 | 验收标准 |
|----|------|---------|
| D1 | 结构化事件 `[doc-extract]`：`truncated`、`injected_chars`、`total_chars`、`continuation_tool_present`、`continuation_used`、`error_class` | 本次论文类回合能读出「截断但续读工具在列表」；`truncated && !continuation_tool_present` 为红灯 |
| D2 | 用户可见「无法提取」且同回合 `truncated=true` 且无 `error_class` 计入误报 | 误报率可按周看；A3 落地后新轨迹应为 0 |

## 5. 关键决策

1. **截断结果是宿主策略输出，不是模型提示里的能力证据。** 因此 A1 钉续读工具不违反「presentation text is not capability evidence」——钉的是 extract 子系统的输出，不是用户散文。
2. **路径选中与通道附件必须收敛到同一 TrustedInput。** 否则语义 document.read 永远无法续读 GUI 选文件。快照仍走现有 `SnapshotBoundedDocumentInput`，不把任意 path 交给模型填。
3. **续读状态放到 handle，不放到会被 Strip 的用户消息。** 与 toolresult 溢出同构；用户消息里的 next_offset 只是给模型看的副本。
4. **不把 office 加回 light 默认表面。** 只在本回合确实有截断/活动文档时保留续读入口，避免再打开「路径文本钉 office」的预算口子。
5. **注入层预算修补保留，但不算闭合。** 近拟合与文首/文尾减少了截断概率、改善了超长文审稿，但不能替代 D1–D3。

## 6. 与既有文档的关系

- `semantic-tool-routing-design-zh.md`：office 可请愿、presentation 非能力证据 —— 仍然成立；本计划把「截断」从 presentation 划到 host extract 结果。
- `docs/design/ssh-availability-incident-review-and-improvement-plan-zh.md`：可用性不变量四场景 —— document.read 按 B3/C1 纳入同一审计方法。
- `docs/design/managed-surface-migration-checklist-zh.md`：迁移前四问 —— auto_extract 续读补答第 ① 问（降级时从哪条路径可达）和第 ② 问（无附件字节时 provider 是否仍发布）。

## 7. 风险

| 风险 | 缓解 |
|------|------|
| A1 被理解成「任何提到 pdf 的闲聊都钉 office」 | 触发器仅限本回合 extract 子系统产出 truncated/error，单测钉负例 |
| A2 把 Downloads 大文件读进进程 | 沿用 32MiB 与页数上限；快照失败则 error_class=input_too_large，仍禁止 bash 绕过 |
| Handle 跨回合文件被替换 | digest + `source_changed`（office 已有） |
| 文首+文尾被模型当成连续正文 | 已有 `# tail: offset=`；A3 禁止「无法提取」话术 |

## 8. PR Plan

| PR | 标题 | 主要文件 | 依赖 |
|----|------|---------|------|
| PR1 | 截断即暴露续读工具（A1）+ 误报话术（A3） | `corelib/tool/router*.go`、`corelib/agent/prompt_blocks.go`、`corelib/agentruntime/prompt_sections.go`、office pin 测试 | 无 |
| PR2 | 路径选中登记 TrustedInput（A2） | `guiapp/im_attachment.go`、`corelib/agentservice/dynamic_host_docread.go`、`guiapp/semantic_tool_routing.go` | PR1 可并行，合入后联调 |
| PR3 | spilled handle 与第二回合续读（B1） | `corelib/agent/file_path_expand.go`、`corelib/agent/conversation_trim.go` / toolresult | PR2（同一 digest） |
| PR4 | 注入预算对齐活动供应商窗口（B2） | `guiapp/im_agent_loop_conversation.go`、`corelib/types.go` 调用点 | 无，可与 PR1 并行 |
| PR5 | 部分页 OCR 回退（B3） | `corelib/agent/tools_office_read.go`、`corelib/knowledge/parse_pdf_ocr.go` | 无 |
| PR6 | document.read 可用性策略 + light 例外（C1/C3） | 语义 routing、`light_tools.go` | PR1、SSH 计划 C2 策略表形状 |

建议落地顺序：PR1 → PR2 → PR3；PR4/PR5 可并行。PR6 等策略表形状与 SSH C2 对齐后再做，避免每个家族私有特判。
