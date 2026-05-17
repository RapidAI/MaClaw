# VE Agent Loop 统一设计：从手工隔离到策略驱动

## 问题本质

当前 VE 的 agent loop（`veAgentCallbacks`）是从零手写的简化版：
- 手动维护工具列表（`veRemoteToolDefinitions`）——每次主 agent 新增能力都需要手动补
- 手动维护 system prompt——与主 agent 的 `BuildSystemPrompt` 完全隔离
- 不经过主 agent 的任何中间件（知识库自动召回、memory proactive recall、steering 注入等）

这导致 VE 永远落后于主 agent 的能力演进。知识库缺失只是第一个被发现的症状。

## 设计目标

VE 应该拥有主 agent 的**信息获取能力**（知识库、memory、web_search、read_file），但**不能修改本地系统**（write_file、edit_file、bash、create_session、ssh 等）。

核心原则：**VE 复用主 agent 的能力基础设施，通过声明式安全策略控制工具可用性。**

## 方案：Tool Policy Layer

### 1. 声明式工具策略

```go
// VEToolPolicy defines which tools are available in VE mode.
// Design: deny-list (block dangerous tools) rather than allow-list (enumerate safe tools).
// Reason: deny-list automatically inherits new read-only tools added to the main agent.
type VEToolPolicy struct {
    // BlockedTools: tools that are never available in VE mode.
    // These are tools that modify the local system or execute arbitrary code.
    BlockedTools map[string]bool
    
    // BlockedCategories: tool categories that are blocked.
    // More maintainable than enumerating individual tools.
    BlockedCategories map[string]bool
}
```

**Blocked tools/categories（VE 不可用）**：
- `write_file`, `edit_file` — 修改文件
- `bash` — 执行任意命令
- `create_session`, `send_and_observe` — 编程会话
- `ssh` — 远程服务器操作
- `browser`, `browser_*`, `gui_observe`, `gui_verify` — 浏览器/GUI 自动化
- `manage_config`, `manage_schedule`, `manage_template` — 系统配置
- `generate_pdf` — 生成文件（可选，看需求）
- `task`, `delegate_task` — 任务管理
- `ask_user` — 结构化提问（VE 直接在文本中提问即可）
- `run_skill` — Skill 执行（可能修改系统）
- `open` — 打开文件/URL（本地操作）

**Allowed tools（VE 可用，自动继承）**：
- `read_file`, `list_directory` — 文件读取
- `knowledge_search`, `knowledge_context_pack`, `knowledge_explain`, `knowledge_fact_graph` 等 — 知识库（全部只读）
- `memory` (action=recall only) — 记忆召回
- `web_search`, `web_fetch` — 网络搜索（信息获取）
- `discover_tool` — 工具发现
- 未来新增的只读工具 — 自动可用（deny-list 的优势）

### 2. VE 复用主 agent 的 system prompt 构建

不再手写 system prompt，而是复用 `corelib/agent.BuildSystemPrompt` 的核心逻辑，加上 VE 特有的覆盖：
- 身份：数字员工名称 + 技能描述
- 能力边界：明确说明不能修改文件/执行命令
- 知识库规则：自动包含（`HasKnowledgeBase` 从 store 检测）
- Memory proactive recall：自动包含
- Steering 规则：自动包含（如果有）

### 3. VE 复用主 agent 的中间件

- **知识库自动召回**：复用 `appendKnowledgeAutoRecall` 的全局 store 单例
- **Memory proactive recall**：复用 `appendProactiveRecall`（如果 VE 所有者有 memory）
- **Steering 注入**：复用 steering store 的 `Resolve()`

### 4. 安全隔离保留

VE 仍然不经过以下主 agent 中间件（这些是 IM/桌面面板特有的交互逻辑）：
- WorkflowEngine（工作流引擎）
- CodingToolGate（编码门控）
- IntentUnderstandingManager（意图理解）
- TaskExecutionOrchestrator（任务编排）
- CapabilityGapDetector（能力缺口检测）
- DriftDetector（漂移检测）——VE 的 agent loop 迭代次数有限（默认 max iterations），不需要漂移检测

### 5. memory tool 的 action 限制

`memory` 工具在 VE 模式下只允许 `action=recall`，不允许 `save`/`delete`/`update`。通过 `ExecuteTool` 中的 action 检查实现，不需要改工具定义。

## 实施路径

### Phase 1（当前已完成）：知识库工具接入
- ✅ 添加 knowledge_search / knowledge_context_pack 到 VE 工具列表
- ✅ 添加知识库自动召回到 VE system prompt
- ✅ 添加知识库使用规则到 VE system prompt

### Phase 2：Tool Policy Layer + deny-list 过滤
- ✅ 定义 `VEToolPolicy` 结构体和 blocked tools 列表（`gui/ve_tool_policy.go`）
- ✅ `BuildTools` 从主 agent 的 `ToolRegistry` 获取完整工具列表，然后按 policy 过滤
- ✅ `ExecuteTool` 添加 policy 检查（双重保险：即使工具定义泄漏，执行层也拦截）
- ✅ `veRemoteToolDefinitions` 保留为 fallback（registry 不可用时）
- ✅ VE-local 安全加固工具（read_file/list_directory 的敏感文件拦截）优先于 registry handler

### Phase 3：System Prompt 统一
- VE 的 `BuildSystemPrompt` 复用 `corelib/agent.BuildSystemPrompt` 的核心 section
- 覆盖身份 section（数字员工名称 + 技能）
- 覆盖能力边界 section（不能修改文件/执行命令）
- 自动包含知识库规则、memory 规则、steering 规则

### Phase 4：中间件复用
- 知识库自动召回：已完成（Phase 1）
- Memory proactive recall：接入
- Steering 注入：接入

## 不变量

- VE 的 blocked tools 列表是**单一数据源**——新增工具时只需决定"是否修改本地系统"，是则加入 blocked list
- VE 自动继承主 agent 的所有只读工具——不需要手动维护 allow-list
- 安全策略在两层生效：工具定义过滤（LLM 看不到）+ 执行层拦截（即使 LLM 幻觉调用也被拦截）
- VE 不经过交互逻辑中间件（workflow/gate/intent/drift）——这些是 IM/桌面面板特有的

## 与当前修复的关系

Phase 1 已完成，解决了用户报告的知识库问题。Phase 2-4 是机制性重构，确保 VE 不再落后于主 agent 的能力演进。可以在后续迭代中实施。
