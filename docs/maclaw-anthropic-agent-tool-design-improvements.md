# MaClaw 改进方案：基于 Anthropic Claude Code 工具设计复盘

> 来源文章：[Anthropic最新复盘：做Agent，最难的不是加工具，而是站到模型那边](https://mp.weixin.qq.com/s/87KU4nc6yAymEbLQv-hkyg)（2026-04-13）
>
> 文章核心观点：Agent 工具设计的核心不是让模型拥有更多按钮，而是让系统更贴近模型真实的思考方式。工具要长在模型当前能力边界上——它能理解、愿意调用、调用后结果稳定、下一步还能接得住。

---

## 一、文章核心洞察与 MaClaw 对照

### 洞察 1：重要高频动作应从"文本约定"升级为"系统能力"

Anthropic 发现让 Claude 在文本中提问不稳定（格式漂移、选项遗漏），最终做成独立的 `AskUserQuestion` 工具。判断标准：**动作足够重要、足够高频、做错会打断流程时，就应该升级为工具**。

**MaClaw 现状**：maclaw 的 LLM 在需要向用户澄清需求时，只能在文本中自由发挥。coding-workflow 规则要求"阶段确认"，但这完全依赖 prompt 约束，模型经常跳过或混淆。

### 洞察 2：旧工具可能变成新束缚

Anthropic 的 `TodoWrite` 在模型弱时有用（防跑偏），但模型变强后反而把模型"锁"在旧计划里。他们替换为支持多 Agent 协作的 `Task` 工具。

**MaClaw 现状**：maclaw 的 `send_and_observe` + `get_session_output` 轮询模式是早期设计，当时模型不够强需要频繁观察。现在模型能力提升后，这种"盯着看"的模式消耗大量 token 且经常导致模型在等待中迷失主线。

### 洞察 3：让 Agent 自己找上下文，而非系统替它塞满

Anthropic 放弃了 RAG 预填充，改为给 Claude Grep 工具让它自己搜索代码库。进一步通过"渐进式暴露"让 Agent 按需发现资料。

**MaClaw 现状**：maclaw 的 `appendMemorySection` 主动召回机制已经在做类似的事（proactive recall + entity expansion），但工具描述全部塞在初始 prompt 里，40+ 工具的描述占据大量 context。

---

## 二、具体改进方案

### 方案 1：AskUser 结构化提问工具

**问题**：模型在需要澄清需求时，经常在文本中自由发挥，格式不一致，用户难以结构化回答。coding-workflow 的"阶段确认"完全靠 prompt 硬拧，模型遵从率不稳定。

**方案**：新增 `ask_user` 工具，让模型在需要用户输入时调用结构化工具而非自由文本。

```go
// corelib/tool/definition.go 或 gui/im_tool_definitions.go
toolDef("ask_user", "向用户提出结构化问题并等待回答。当你需要用户确认方案、选择选项、或提供缺失信息时使用此工具，而不是在文本中直接提问。",
    map[string]interface{}{
        "question":    map[string]string{"type": "string", "description": "要问用户的问题"},
        "options":     map[string]interface{}{"type": "array", "description": "可选：预设选项列表", "items": map[string]string{"type": "string"}},
        "context":     map[string]string{"type": "string", "description": "可选：问题的背景说明"},
        "required":    map[string]string{"type": "boolean", "description": "是否必须回答才能继续（默认 true）"},
        "input_type":  map[string]string{"type": "string", "description": "期望的回答类型: choice/text/confirm（默认 text）"},
    }, []string{"question"})
```

**实现要点**：
- GUI 侧：收到 `ask_user` 调用后，在聊天界面渲染结构化 UI（选项按钮/确认框/文本输入框），而非纯文本
- IM 网关侧（飞书/微信/QQ）：渲染为消息卡片（飞书 interactive card / 微信模板消息）
- 用户回答后，将结构化结果注入下一轮 tool_result，模型可以直接解析而非从自由文本中提取
- coding-workflow 的阶段确认可以从 prompt 约束迁移为 `ask_user(question="需求文档已生成，请确认", input_type="confirm")` 调用

**预期收益**：
- 消除模型"问了但没等回答就继续"的问题
- coding-workflow 阶段确认的遵从率从 prompt 依赖提升为工具强制
- IM 侧用户体验提升（卡片交互 vs 纯文本）

---

### 方案 2：Task 任务管理工具替代轮询模式

**问题**：当前 `send_and_observe` + `get_session_output` 的轮询模式消耗大量 token。模型经常在等待编程会话输出时迷失主线，或者过度轮询浪费推理轮次。Swarm 多 Agent 场景下，各 Agent 之间缺乏共享任务状态的机制。

**方案**：新增 `task` 工具，统一管理任务生命周期，支持依赖关系和多 Agent 共享。

```go
toolDef("task", "管理任务（create/update/complete/list/delegate）。用于跟踪复杂任务的进度、依赖关系和子任务分配。",
    map[string]interface{}{
        "action":      map[string]string{"type": "string", "description": "操作: create/update/complete/fail/list/delegate"},
        "task_id":     map[string]string{"type": "string", "description": "任务 ID（update/complete/fail/delegate 时必填）"},
        "title":       map[string]string{"type": "string", "description": "任务标题（create 时必填）"},
        "description": map[string]string{"type": "string", "description": "任务描述"},
        "depends_on":  map[string]interface{}{"type": "array", "description": "依赖的任务 ID 列表", "items": map[string]string{"type": "string"}},
        "status_note": map[string]string{"type": "string", "description": "状态更新说明（update 时使用）"},
        "delegate_to": map[string]string{"type": "string", "description": "委派给哪个 Agent/会话（delegate 时使用）"},
    }, []string{"action"})
```

**实现要点**：
- 任务状态存储在 `corelib/task/store.go`，内存态 + 可选持久化
- 编程会话（claude/codex/opencode）完成时，自动更新关联 task 状态（通过 session event 监听）
- Swarm orchestrator 可以通过 task 工具查看所有子 Agent 的任务进度
- 替代当前 `send_and_observe` 的"盯着看"模式：创建 task → 委派给会话 → 会话完成时自动回调 → 模型收到 task 完成通知

**与现有系统的关系**：
- `send_and_observe` / `get_session_output` 保留但降级为底层工具
- `task` 成为高层抽象，模型优先使用 task 而非直接轮询
- Swarm 的 `task_splitter.go` 可以直接对接 task 工具

---

### 方案 3：工具渐进式暴露（Progressive Tool Discovery）

**问题**：maclaw 当前有 40+ 工具定义，全部塞在初始 prompt 中。`MaxToolBudget=28` 的路由机制虽然做了裁剪，但仍然占据大量 context。模型面对太多选项时判断成本增加，容易选错工具。

**方案**：将工具分为三层，渐进式暴露。

**第一层：核心工具（始终可见，约 12 个）**
```
bash, read_file, write_file, edit_file, list_directory,
memory, ask_user, task, send_file, screenshot,
discover_tool, web_fetch
```

**第二层：场景工具（条件触发，约 15 个）**
```
ssh, web_search, browser_*, create_session, run_skill,
craft_tool, generate_pdf, parallel_execute, ...
```
已有的 `conditionalKeepRules` 机制继续使用，但扩大覆盖范围。

**第三层：延迟工具（按需发现，约 15 个）**
```
配置管理工具（get_config, update_config, batch_update_config, ...）
定时任务工具（create_scheduled_task, list_scheduled_tasks, ...）
模板工具（create_template, list_templates, launch_template）
AgentNet 工具（agentnet_search, agentnet_publish）
MCP 工具（list_mcp_tools, call_mcp_tool）
```

**实现要点**：
- 已有 `DefinitionGenerator.SetDeferredTools()` 和 `SearchDeferred()` 机制，直接复用
- `discover_tool` 已在 `CoreToolNames` 中，作为模型发现延迟工具的入口
- 需要增强 `discover_tool` 的描述，让模型知道哪些能力可以通过它发现：

```go
toolDef("discover_tool", 
    "发现更多可用工具。当你需要以下能力但找不到对应工具时调用："+
    "配置管理、定时任务、会话模板、AgentNet 知识网络、MCP 扩展工具、"+
    "Skill 市场搜索安装。传入你需要的能力描述，返回匹配的工具定义。",
    map[string]interface{}{
        "query": map[string]string{"type": "string", "description": "需要的能力描述（如'修改配置'、'定时执行'、'搜索知识网络'）"},
    }, []string{"query"})
```

**预期收益**：
- 初始 prompt 中工具定义从 40+ 降到 ~12，节省约 3000-5000 token
- 模型在简单任务中不会被无关工具干扰
- 复杂任务时通过 `discover_tool` 按需加载，不影响能力完整性

---

### 方案 4：子 Agent 模式替代 context 膨胀

**问题**：maclaw 的系统 prompt 中注入了大量指导信息（编码工作流规则、记忆内容、工具描述、人设信息等），导致 context 膨胀。Anthropic 的经验是"不是所有知识都要塞进主上下文"。

**方案**：将特定领域知识封装为子 Agent，主 Agent 按需调用。

**4.1 CodingWorkflow 子 Agent**

当前 coding-workflow 的三阶段流程（需求→设计→任务拆分）完全靠 system prompt 中的规则约束。改为：

```go
toolDef("coding_workflow", "启动编码工作流。当用户提出编码需求时调用此工具，它会引导完成需求分析→技术设计→任务拆分的完整流程。",
    map[string]interface{}{
        "user_request": map[string]string{"type": "string", "description": "用户的编码需求描述"},
        "phase":        map[string]string{"type": "string", "description": "可选：跳转到指定阶段 requirements/design/tasks"},
    }, []string{"user_request"})
```

实现为内部子 Agent：主 Agent 调用 `coding_workflow` → 启动子 Agent（带有完整的编码工作流 prompt）→ 子 Agent 生成文档并通过 `ask_user` 确认 → 确认后返回结果给主 Agent → 主 Agent 开始执行。

**4.2 Help 子 Agent**

参考 Anthropic 的 "Claude Code Guide 子代理"：当用户问"maclaw 怎么用"、"怎么配置 SSH"等使用问题时，主 Agent 调用 help 子 Agent 查文档，而非在主 context 中塞满使用说明。

**预期收益**：
- 主 system prompt 可以移除 coding-workflow 的详细规则（约 2000 字），改为一句"编码任务请调用 coding_workflow 工具"
- 编码工作流的遵从率提升（子 Agent 专注于此，不会被其他上下文干扰）
- 主 Agent 的 context 更干净，主线任务不被辅助信息污染

---

### 方案 5：工具使用反馈闭环（Tool Outcome Learning）

**问题**：Anthropic 强调"多实验，认真读输出"。maclaw 的 `UsageTracker` 已经记录工具使用频率，但缺少**结果反馈**——模型调用工具后成功还是失败、用户是否满意，这些信号没有回流到路由决策中。

**方案**：增强 `UsageTracker`，记录工具调用的结果质量。

```go
// corelib/tool/usage_tracker.go
type ToolOutcome struct {
    ToolName    string
    Success     bool      // 工具执行是否成功
    UserRating  int       // 用户反馈（-1=差, 0=无, 1=好）
    FollowUp    string    // 模型下一步动作（retry=重试, continue=继续, abandon=放弃）
    Timestamp   time.Time
}

// RecordOutcome records the outcome of a tool invocation.
func (t *UsageTracker) RecordOutcome(outcome ToolOutcome) { ... }

// OutcomeScore returns a quality score for the tool based on recent outcomes.
// High failure rate or frequent retries lower the score.
func (t *UsageTracker) OutcomeScore(toolName string) float64 { ... }
```

**融入路由决策**：
- `Router.Route()` 的多信号评分中加入 outcome 信号：
  - 工具近期成功率高 → 加分
  - 工具近期频繁失败或被用户否定 → 降分
  - 工具被调用后模型立即 retry 或 abandon → 降分

**数据采集**：
- 工具执行返回 error → `Success=false`
- 模型连续两次调用同一工具（参数不同）→ 第一次标记为 `FollowUp=retry`
- 用户在工具执行后说"不对"、"重来"→ `UserRating=-1`
- 用户在工具执行后继续推进任务 → `UserRating=1`（隐式正反馈）

---

### 方案 6：定期审计工具列表（Tool Hygiene）

**问题**：Anthropic 明确指出"今天有用的工具，半年后可能会拖后腿"。maclaw 的工具列表只增不减，`BuiltinToolNames` 已经有 50+ 条目。

**方案**：建立工具审计机制。

**6.1 使用率监控**

基于 `UsageTracker` 数据，定期生成工具使用报告：
- 过去 30 天零调用的工具 → 候选降级为延迟工具
- 调用后失败率 > 50% 的工具 → 候选重新设计或移除
- 模型经常误调用的工具（调用后立即 abandon）→ 候选改进描述或合并

**6.2 工具合并建议**

当前一些工具可以考虑合并：
- `get_config` + `update_config` + `batch_update_config` + `list_config_schema` + `export_config` + `import_config` → 合并为 `config(action=get/set/batch/schema/export/import)`
- `create_scheduled_task` + `list_scheduled_tasks` + `delete_scheduled_task` + `update_scheduled_task` → 合并为 `schedule(action=create/list/delete/update)`
- `create_template` + `list_templates` + `launch_template` → 合并为 `template(action=create/list/launch)`

这样可以减少约 10 个独立工具定义，降低模型的选择负担。

---

## 三、实施优先级

| 优先级 | 方案 | 预期工作量 | 预期收益 |
|--------|------|-----------|---------|
| P0 | 方案 3：工具渐进式暴露 | 中（复用已有 deferred 机制） | 立即减少 context 占用，降低模型选择负担 |
| P0 | 方案 6.2：工具合并 | 低（重构工具定义 + handler 路由） | 减少 10+ 工具定义，简化模型决策 |
| P1 | 方案 1：AskUser 结构化提问 | 中（新工具 + GUI/IM 渲染） | 提升交互质量，解决 coding-workflow 遵从率问题 |
| P1 | 方案 5：工具使用反馈闭环 | 中（增强 UsageTracker） | 路由决策更智能，自动淘汰低质量工具 |
| P2 | 方案 2：Task 任务管理工具 | 高（新子系统 + 会话集成） | 减少轮询 token 消耗，支持 Swarm 协作 |
| P2 | 方案 4：子 Agent 模式 | 高（子 Agent 框架 + prompt 重构） | 主 context 更干净，专业任务质量提升 |
| P3 | 方案 6.1：使用率监控 | 低（报告生成脚本） | 长期工具健康度维护 |

---

## 四、核心原则总结

从 Anthropic 的复盘中提炼出适用于 MaClaw 的设计原则：

1. **工具数量克制**：不是越多越好。每多一个工具，模型多一层判断成本。目标是核心工具 ≤ 15 个。
2. **从模型视角设计**：观察模型在哪里犯错、犹豫、混淆，然后决定是否需要新工具或改造旧工具。
3. **渐进式暴露**：不要一次性塞满所有能力，让模型按需发现。
4. **定期回顾**：模型能力在变，工具设计也要跟着变。今天的好工具可能是明天的束缚。
5. **结构化优于文本约定**：重要的交互（提问、确认、任务管理）应该是工具调用，而非 prompt 约束。
