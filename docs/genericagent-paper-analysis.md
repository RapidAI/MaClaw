# GenericAgent 论文分析：可借鉴的优点与 MacLaw 改进方向

**论文**：[GenericAgent: A Token-Efficient Self-Evolving LLM Agent via Contextual Information Density Maximization (V1.0)](https://arxiv.org/abs/2604.17091)

**核心论点**：长程 Agent 的性能不取决于 context 长度，而取决于有限 context 预算内维持了多少决策相关信息（context information density maximization）。

---

## 一、论文四大核心组件概述

GenericAgent 围绕"上下文信息密度最大化"这一原则，通过四个紧密关联的组件实现：

| 组件 | 核心思想 | 实现方式 |
|------|---------|---------|
| **最小原子工具集** | 工具不是越多越好，每多一个工具模型多一层判断成本 | 9 个原子工具 + `code_run` 动态扩展 |
| **分层按需记忆** | 默认只展示高层摘要视图，按需展开细节 | L0-L4 五层记忆，L1 索引层做路由 |
| **自我进化机制** | 将验证过的执行轨迹固化为可复用的 SOP 和可执行代码 | 任务完成后自动提炼 Skill，写入 L3 |
| **上下文截断与压缩** | 在长程执行中维持信息密度 | 每 10 轮重置工具描述，working checkpoint 压缩 |

---

## 二、值得借鉴的优点

### 1. 极简工具集 + `code_run` 动态扩展（最大亮点）

**GenericAgent 做法**：只有 9 个原子工具（code_run / file_read / file_write / file_patch / web_scan / web_execute_js / ask_user + 2 个记忆工具）。复杂能力不通过预定义工具实现，而是通过 `code_run` 在运行时动态安装依赖、编写脚本、调用 API。

**为什么有效**：
- 工具定义占 context 的 token 极少（9 个 vs MacLaw 的 40+）
- 模型选择负担极低——不需要在 40 个工具中做判断
- 能力上限不受预定义工具限制——`code_run` 可以做任何事

**MacLaw 现状**：
- 已做了工具合并（#14，13→3）和渐进式暴露（#14，deferred tools）
- 已做了 browser 工具合并（#79，30→1）
- 但仍有 ~15 个核心工具 + 条件工具，初始 prompt 工具定义占 ~10K token

**可借鉴的改进方向**：

MacLaw 不适合直接照搬 GenericAgent 的 9 工具模型——MacLaw 面向的是 IM 通道的非技术用户，不能要求 LLM 每次都通过 `code_run` 写 Python 脚本来完成文件操作。但核心思想可以借鉴：

- **工具定义的"摘要模式"**：当前每个工具的 JSON Schema 定义包含完整的参数描述。可以引入两级工具描述——初始 prompt 只包含工具名 + 一句话描述（类似 GenericAgent 的 L1 索引），LLM 决定使用某工具时再通过 `discover_tool` 获取完整 schema。这比当前的 deferred tools 更进一步——deferred tools 是整个工具定义延迟加载，摘要模式是所有工具都可见但定义精简。
- **`code_run` 等价物**：MacLaw 的 `bash` 工具已经是 `code_run` 的等价物，但 system prompt 没有引导 LLM 将其作为"万能工具"使用。可以在 steering 中明确告知 LLM：当没有专用工具时，优先通过 `bash` + Python/Node 脚本解决，而非报告"无法完成"。

### 2. 分层按需记忆（L0-L4 架构）

**GenericAgent 做法**：

| 层 | 内容 | 注入时机 | 大小 |
|---|------|---------|------|
| L0 | 元规则（system prompt） | 始终注入 | 固定 |
| L1 | 记忆索引（insight index） | 始终注入，极简 | ~几百 token |
| L2 | 全局事实（global facts） | 始终注入 | 可增长 |
| L3 | 任务 Skills/SOPs | **按需召回** | 大量 |
| L4 | 会话归档 | **按需召回** | 大量 |

关键设计：L1 是一个极简的索引层，只包含"我知道什么"的目录，不包含具体内容。LLM 通过 L1 判断需要什么信息，再通过记忆工具按需加载 L3/L4 的具体内容。

**为什么有效**：
- 默认 context 中只有 L0+L1+L2（几千 token），不会被大量记忆淹没
- L1 索引让 LLM 知道"有什么可用"，避免盲目搜索
- L3 的 Skill/SOP 是结构化的可执行流程，不是自由文本——信息密度极高

**MacLaw 现状**：
- 记忆系统有 `proactive recall`（主动召回）+ `memory tool recall`（按需召回）
- 但没有"索引层"——proactive recall 直接返回完整 entry 内容（截断到 200 字符）
- `task_artifact`（#62）是阶段产出物摘要，但不是结构化的可执行 SOP

**可借鉴的改进方向**：

- **引入 L1 等价物——记忆索引注入**：在 proactive recall 之前，先注入一个极简的"记忆目录"，列出当前记忆库中的 category 分布和关键 tag。例如：`[记忆索引] 项目知识: 3条(C++游戏/SSH服务器/Python工具) | 用户偏好: 2条 | 任务产出: 1条(贪吃蛇需求文档)`。这让 LLM 知道有什么可用，而不是被 proactive recall 的完整内容淹没。
- **Skill/SOP 结构化存储**：当前 MacLaw 的 Skill 是外部脚本（skill.yaml / SKILL.md），不在记忆系统中。GenericAgent 的 L3 将 SOP 作为记忆的一部分，LLM 可以直接在 context 中看到"上次做类似任务的步骤"。MacLaw 可以在 `task_artifact` 的基础上，新增 `task_sop` 类别——当任务成功完成时，自动提炼执行步骤为结构化 SOP，存入记忆。下次类似任务时，proactive recall 召回 SOP，LLM 直接复用而非从头探索。

### 3. 自我进化机制（Skill 固化）

**GenericAgent 做法**：每次成功完成新任务后，自动将执行路径固化为 Skill（L3 记忆），包含：
- 任务描述（什么时候触发）
- 执行步骤（具体怎么做）
- 依赖信息（需要什么环境）
- 验证方法（怎么确认成功）

下次遇到类似任务时，L1 索引匹配到相关 Skill，直接调用而非重新探索。

**为什么有效**：
- 消除了重复探索的 token 浪费——第一次做"读微信消息"花 50 轮，之后只需 1 轮
- Skill 是经过验证的——只有成功完成的任务才会被固化
- 能力随使用增长——形成个人化的"技能树"

**MacLaw 现状**：
- 有 Skill 系统（skill.yaml / SKILL.md），但 Skill 来自外部（Hub/ClawHub/GitHub），不是从用户任务中自动生成的
- 有 `task_artifact`（#62）沉淀阶段产出物，但不是可执行的 SOP
- 有 `KnowledgeExtractor` 从对话中提取知识点，但提取的是事实性知识，不是操作流程

**可借鉴的改进方向**：

- **任务完成后自动提炼 SOP**：当 agent loop 成功完成一个多步骤任务（≥5 轮迭代，有工具调用）时，用轻量 LLM 调用从 trajectory 中提炼结构化 SOP：`{trigger: "用户要求...", steps: [{tool: "bash", args: "...", purpose: "..."}, ...], dependencies: [...], verification: "..."}`。存入记忆的 `task_sop` 类别。
- **SOP 匹配与复用**：proactive recall 时，如果召回了 `task_sop` 条目，在 system prompt 中以"参考流程"的形式注入。LLM 可以直接按步骤执行，也可以根据当前情况调整。
- **SOP 验证与更新**：如果按 SOP 执行失败，标记该 SOP 为"需要更新"。下次成功完成同类任务后，用新的 trajectory 更新 SOP。

### 4. 上下文截断与压缩层

**GenericAgent 做法**：
- **working checkpoint**：Agent 通过 `update_working_checkpoint` 工具主动压缩当前工作状态为一段摘要（key_info），作为下一轮的上下文基础
- **每 10 轮重置工具描述**：`if turn%10 == 0: client.last_tools = ''`——定期清除缓存的工具描述，强制重新生成，避免过时信息占据 context
- **历史不在 messages 中累积**：`messages = [{"role": "user", "content": next_prompt, "tool_results": tool_results}]`——每轮只发送新消息，完整历史由 Session 对象管理

**为什么有效**：
- Context 窗口始终保持在 <30K token（vs 其他 Agent 的 200K-1M）
- 信息密度高——只有决策相关的信息在 context 中
- 不依赖模型的长 context 能力——对小模型也友好

**MacLaw 现状**：
- 有 `trimHistory`（#56，两层截断保留 turn boundaries）
- 有 `trimConversation`（token 预算裁剪）
- 有 `compactHistory`（LLM 摘要压缩，#66）
- 有实际 token 校准（#74，用 API 返回的实际 token 数校准估算）
- 但所有历史仍在 messages 中累积，依赖 trimming 被动裁剪

**可借鉴的改进方向**：

- **Agent 主动压缩（working checkpoint 等价物）**：给 LLM 一个 `update_context` 工具，让它在关键节点主动压缩当前工作状态。例如，完成一个子任务后，LLM 调用 `update_context(summary="已完成文件结构创建，下一步是实现核心逻辑")`，系统将之前的详细工具调用历史替换为这段摘要。这比被动的 `trimHistory` 更精准——LLM 知道什么信息是后续决策需要的。
- **工具结果压缩**：当前工具返回的结果（如 `read_file` 的完整文件内容、`bash` 的完整输出）直接进入对话历史。可以在工具返回后立即压缩——保留前 N 行 + 后 N 行 + 中间摘要，或者对结构化输出（JSON/表格）只保留 schema + 样本行。GenericAgent 的 `_clean_content` 函数就做了类似的事（代码块超过 6 行只保留前 5 行 + 行数统计）。

---

## 三、MacLaw 已有但 GenericAgent 缺少的优势

对比分析不应只看"别人有什么我没有"，也要看"我有什么别人没有"：

| 能力 | MacLaw | GenericAgent |
|------|--------|-------------|
| 多用户隔离 | OwnerID 多租户隔离（#67/#71） | 单用户 |
| 工作流引擎 | 19 个模板的多阶段流程 | 无 |
| 安全护栏 | ThreatPattern Guard + PolicyEngine（#80） | 无 |
| 编码 SubAgent | 纯净上下文编程执行器（#75） | 无（单体 Agent） |
| 漂移检测 | 三层漂移检测（同参数+同结果、频率异常、慢速轮询）| 无 |
| IM 多通道 | 飞书/微信/QQ/Telegram + 桌面面板 | 飞书/微信/QQ/Telegram + Streamlit |
| 意图分类 | UIC 三层融合（keyword/embedding/LLM） | 无（LLM 自行判断） |

MacLaw 在企业级特性（安全、多租户、工作流）上远超 GenericAgent。GenericAgent 的优势在于极简和 token 效率。

---

## 四、具体改进建议（按优先级排序）

### P0: 工具结果即时压缩

**问题**：`read_file` 返回 5000 行文件、`bash` 返回大量日志，这些原始内容直接进入对话历史，是 context 膨胀的主要来源（#74 的 134K token 膨胀就是这个原因）。

**方案**：在 `executeTool()` 返回结果后、写入 conversation 之前，对工具结果做即时压缩：
- `read_file`：超过 200 行时，保留前 50 行 + 后 50 行 + `[... 省略 N 行 ...]`
- `bash`：超过 100 行时，保留前 30 行 + 后 30 行 + 行数统计
- `list_directory`：超过 50 项时，保留前 20 项 + 统计信息
- 工具结果中的重复行（如编译警告）去重

这是 GenericAgent `_clean_content` 思想的直接应用，但在工具结果层面而非显示层面。

### P1: 记忆索引层（L1 等价物）

**问题**：proactive recall 注入 8-12 条完整记忆条目（每条 200 字符），占 ~2000 token，但 LLM 可能只需要其中 1-2 条。

**方案**：在 proactive recall 之前，先注入一个 ~200 token 的记忆索引：
```
[记忆索引] 
- 项目: C++打飞机游戏(test5), SSH服务器(api.rapidai.tech)
- 偏好: 中文交互, CMake构建
- 近期任务: 贪吃蛇需求文档(已完成), PPT设计(进行中)
```
LLM 看到索引后，如果需要具体内容，通过 `memory(action=recall, query="...")` 按需获取。proactive recall 的条目数可以从 12 降到 5，节省 ~1000 token。

### P1: 任务 SOP 自动提炼与复用

**问题**：用户重复做类似任务时（如"翻译论文"、"查看服务器资源"），LLM 每次都从头探索，浪费大量迭代和 token。

**方案**：
1. 任务成功完成后（agent loop 正常退出，≥5 轮迭代），用轻量 LLM 从 trajectory 提炼 SOP
2. SOP 存入 `task_sop` 记忆类别，tags 包含任务类型关键词
3. proactive recall 召回 SOP 时，以"参考流程"形式注入
4. LLM 可以直接按 SOP 执行，也可以调整

### P2: Agent 主动上下文压缩工具

**问题**：长程任务中，对话历史被动增长，`trimHistory` 只能按时间顺序裁剪，可能丢失关键信息。

**方案**：新增 `compress_context` 工具（类似 GenericAgent 的 `update_working_checkpoint`）：
- LLM 在完成一个子任务后主动调用：`compress_context(summary="已完成模块A的实现，创建了 src/module_a.go 和 src/module_a_test.go，测试全部通过")`
- 系统将之前的详细工具调用历史替换为这段摘要
- 摘要同时写入 `task_artifact` 记忆，防止丢失

### P2: 工具描述摘要模式

**问题**：15 个核心工具的完整 JSON Schema 定义占 ~10K token。

**方案**：初始 prompt 中工具定义使用"摘要模式"——每个工具只包含名称 + 一句话描述 + 必需参数名，不包含参数的详细描述和 enum 值。LLM 决定使用某工具时，系统自动注入该工具的完整 schema 到下一轮的 system message 中。预计可将工具定义从 ~10K 降到 ~3K token。

---

## 五、不建议借鉴的部分

### 1. 单消息历史模式

GenericAgent 每轮只发送 `[{"role": "user", "content": next_prompt}]`，完整历史由 Session 对象管理。这对 MacLaw 不适用——MacLaw 的 IM 通道需要保持对话连贯性，用户可能在任意时刻插入新消息，单消息模式会丢失对话上下文。

### 2. `code_run` 作为唯一扩展机制

GenericAgent 通过 `code_run` 动态安装依赖和编写脚本来扩展能力。这对技术用户可行，但 MacLaw 面向的 IM 用户（飞书/微信）不期望 Agent 在他们的机器上随意安装软件包。MacLaw 的 Skill 生态（Hub/ClawHub）是更安全的扩展机制。

### 3. 无安全护栏

GenericAgent 完全没有安全机制——`code_run` 可以执行任意代码。这在个人桌面场景可接受，但在 MacLaw 的企业 IM 场景中不可接受。

---

## 六、总结

GenericAgent 论文的核心贡献不是具体的技术实现（代码只有 3K 行，实现相当简单），而是**设计哲学**：

> 长程 Agent 的性能瓶颈不是 context 长度，而是 context 中的信息密度。

这个观点与 MacLaw 在 #74（context 膨胀到 134K 导致空响应）、#75（SubAgent 纯净上下文）、#79（browser 工具 token 密度导致幻觉）中的实践经验完全一致。

MacLaw 已经在多个维度做了信息密度优化（工具合并、渐进式暴露、两层截断、实际 token 校准），但还有提升空间。上述 P0-P2 建议的共同主题是：**从被动裁剪转向主动压缩**——让系统和 LLM 共同管理 context 中的信息密度，而不是等 context 膨胀后再裁剪。

---

*Content was rephrased for compliance with licensing restrictions. Sources: [arxiv.org/abs/2604.17091](https://arxiv.org/abs/2604.17091), [github.com/lsdefine/GenericAgent](https://github.com/lsdefine/GenericAgent)*

---

## 七、已实施的优化（2026-04-29）

基于上述分析，已实施以下三项优化：

### 优化 1: 工具结果语义压缩（P0）

**文件**：`gui/im_conversation_trim.go`

在 `truncateToolResultForTool` 的 size truncation 之前，新增 `compressToolResultSemantic` 语义压缩层：

1. **行去重**：连续 ≥3 行相同内容（如编译器警告、npm install 输出）折叠为 1 行 + `"... (重复 N 行) ..."`
2. **同质块折叠**：连续 >10 行共享相同前缀（如 `PASS `、`warning: `、`import `）保留首 3 行 + 末 2 行 + 摘要

效果：在 size truncation 之前先移除冗余内容，让更多唯一信息在 4KB 预算内存活。对 bash、read_file、list_directory、get_session_output 等高频工具生效。

### 优化 2: 记忆索引层（P1）

**文件**：`gui/im_system_prompt.go`

在 proactive recall 注入完整记忆条目之前，先注入一行紧凑的记忆索引：

```
[记忆索引] 项目: 3条(C++游戏, SSH服务器) | 偏好: 2条 | 任务产出: 1条(需求文档)
```

`buildMemoryIndex` 函数按 category 分组，每组显示条目数和最多 3 个代表性 tag。LLM 看到索引后可以快速判断是否需要通过 `memory(action=recall)` 获取更多细节，而不是逐条扫描 200 字符的摘要。

### 优化 3: 主动上下文压缩工具（P2）

**文件**：`gui/im_tool_compress_context.go`（新文件）、`gui/im_tool_definitions.go`、`gui/tool_registry_builtin.go`、`corelib/tool/router.go`、`gui/im_message_handler.go`

新增 `compress_context` 工具，让 LLM 在长程任务的关键检查点主动压缩对话历史：

- LLM 调用 `compress_context(summary="已完成文件结构创建，下一步实现核心逻辑")`
- 系统将之前的详细工具调用历史替换为这段摘要（保留 system prompt + 首条用户消息 + 最近 N 条）
- 摘要同时写入 `task_artifact` 记忆，防止丢失

System prompt 中新增"上下文管理"section，引导 LLM 在以下时机使用：
- 完成一个独立子任务后
- 连续执行了 10+ 轮工具调用后
- 切换到不同类型的工作时

### 测试覆盖

新增 10 个测试（`gui/im_conversation_trim_compress_test.go`）：
- 语义压缩：短输入不变、非目标工具不变、行去重、同质块折叠、混合内容保留唯一行、大文件读取
- 前缀提取：常见模式（PASS/warning/error/tab）
- 上下文压缩：基本压缩、条目过少不压缩、nil 请求不变

所有 10 个新测试 + 所有现有测试通过。全项目编译通过。
