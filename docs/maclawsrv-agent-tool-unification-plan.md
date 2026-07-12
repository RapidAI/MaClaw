# MaClawSrv Agent 工具统一方案

## 问题概述

MaClawSrv 的 `CoreAgentExecutor` 只有 5 类硬编码工具（bash/ssh/ask_user/task/knowledge），而 GUI 桌面版有 40+ 工具。用户通过 REST API 安装的 MCP 服务器和 Skill 在 agent loop 中不可见——LLM 无法发现和调用它们。

**根因**：GUI 的工具实现全在 `gui/` 包中（依赖 `*App` 结构体），`corelib/agentservice/` 无法直接复用，导致 `CoreAgentExecutor` 从零实现了一个精简版。

## 当前状态对比

| 工具类别 | GUI 桌面版 | MaClawSrv | 共享代码位置 |
|---------|-----------|-----------|------------|
| bash | `gui/im_tools_local.go` | `corelib/agent.ToolBash` | `corelib/agent/tool_bash.go` |
| ssh | `gui/im_ssh_tools.go` | `corelib/agent/sshtool/` | `corelib/agent/sshtool/` |
| ask_user | `gui/im_tool_ask_user.go` | `corelib/agent.ToolAskUser` | `corelib/agent/tool_ask_user.go` |
| task | `gui/im_tool_task.go` | `corelib/agent.ToolTask` | `corelib/task/` |
| knowledge | `gui/im_knowledge_*.go` | `knowledge_integration.go` | `corelib/knowledge/` |
| MCP | `gui/local_mcp_manager.go` + `MCPRegistry` | **刚修复** `mcp_integration.go` | `corelib/agentservice/mcp.go` |
| **Skill** | `gui/skill_runner.go` + `manage_skill` | **缺失** | `corelib/skill/`（扫描/搜索共享，执行不共享） |
| **文件操作** | `read_file`/`write_file`/`edit_file`/`list_directory` | **缺失** | 无共享（GUI 实现在 `gui/im_tools_local.go`） |
| **Web** | `web_search`/`web_fetch` | **缺失** | 无共享（GUI 实现在 `gui/im_tools_web.go`） |
| **memory** | `memory(save/recall)` | **缺失**（只有 proactive recall） | `corelib/memory/` |

## 设计原则

1. **单一实现**：工具的核心逻辑只在 `corelib/` 中实现一次，GUI 和 MaClawSrv 共享
2. **Provider 接口**：平台特有的能力（如 GUI 的 Wails 绑定、MaClawSrv 的多租户隔离）通过接口注入
3. **渐进式迁移**：不一次性重构所有工具，按优先级分 Phase 实施
4. **向后兼容**：GUI 现有行为不变，MaClawSrv 新增能力

## 架构设计

### 目标架构

```
┌─────────────────────────────────────────────────────────┐
│                    corelib/agent/                         │
│  ┌─────────────────────────────────────────────────┐    │
│  │  LoopCallbacks interface                         │    │
│  │    BuildTools(userText) []map[string]interface{} │    │
│  │    ExecuteToolStructured(name, args) Result      │    │
│  └─────────────────────────────────────────────────┘    │
│                          ▲                               │
│                          │ implements                    │
│            ┌─────────────┴──────────────┐               │
│            │                            │               │
│  ┌─────────┴─────────┐    ┌────────────┴────────────┐  │
│  │ gui/               │    │ corelib/agentservice/    │  │
│  │ IMMessageHandler   │    │ coreAgentCallbacks       │  │
│  │ (40+ tools)        │    │ (core + providers)       │  │
│  └────────────────────┘    └─────────────────────────┘  │
│                                       │                  │
│                            ┌──────────┼──────────┐      │
│                            ▼          ▼          ▼      │
│                     MCPToolProvider  SkillProvider  ...  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                 corelib/skill/runner.go (新增)            │
│  SkillRunner — 共享的 Skill 执行引擎                      │
│  - 步骤迭代、变量捕获、poll 循环、when 条件               │
│  - 不依赖 gui/*App，通过接口获取外部依赖                  │
└─────────────────────────────────────────────────────────┘
```

### Provider 接口模式（已验证）

MCP 修复已验证了这个模式的可行性：

```go
// corelib/agentservice/mcp_integration.go
type MCPToolProvider interface {
    ListAvailableTools(ctx, principal) []MCPToolEntry
    CallTool(ctx, principal, serverID, toolName, args) (string, error)
}

// 注入到 CoreAgentExecutor
executor.SetMCPToolProvider(NewMCPToolBridge(svc))
```

Skill 和其他工具类别使用相同模式。

---

## Phase 1: Skill Provider 接入 Agent Loop（优先级 P0）

### 目标

让 MaClawSrv 的 agent 能够列出、搜索、运行已安装的 Skills。

### 设计

#### 1.1 SkillToolProvider 接口

```go
// corelib/agentservice/skill_integration.go

// SkillToolProvider enables the agent loop to discover and execute Skills.
type SkillToolProvider interface {
    // ListSkills returns all active skills for the principal.
    ListSkills(ctx context.Context, p Principal) []SkillToolEntry
    
    // RunSkill executes a skill by name with the given arguments.
    // Returns the execution result as a string.
    RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error)
    
    // SearchSkills searches for skills across all configured sources.
    SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error)
}

type SkillToolEntry struct {
    Name        string
    Description string
    Params      []SkillParam // from NLSkillParam
    Mode        string       // sequential/interactive/api_workflow
}
```

#### 1.2 SkillToolBridge 实现

```go
// corelib/agentservice/skill_integration.go

type SkillToolBridge struct {
    svc *Service
}

func NewSkillToolBridge(svc *Service) *SkillToolBridge {
    return &SkillToolBridge{svc: svc}
}

func (b *SkillToolBridge) ListSkills(ctx context.Context, p Principal) []SkillToolEntry {
    // 委托给 Service.ListSkills（已有实现）
    views, err := b.svc.ListSkills(ctx, p)
    // 转换为 SkillToolEntry...
}

func (b *SkillToolBridge) RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error) {
    // Phase 1: 委托给 corelib/skill 包的共享 Runner
    // Phase 2: 使用提取后的 corelib/skill/runner.go
}

func (b *SkillToolBridge) SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error) {
    // 委托给 Service.SearchSkills（已有实现）
}
```

#### 1.3 接入 coreAgentCallbacks

```go
// BuildTools 中追加 manage_skill 工具定义
func (c *coreAgentCallbacks) BuildTools(string) []map[string]interface{} {
    // ... existing core tools ...
    // ... existing MCP tools ...
    
    // Append manage_skill if provider is available
    if c.skillProvider != nil {
        tools = append(tools, c.manageSkillToolDef())
    }
    return tools
}

// ExecuteToolStructured 中追加 manage_skill 分发
case "manage_skill":
    return c.executeManageSkill(args)
```

#### 1.4 Skill 执行——复用 corelib/skill 包

Phase 1 的 `RunSkill` 实现：

```go
func (b *SkillToolBridge) RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error) {
    // 1. 从 Service 获取 skill entry
    skillView, err := b.svc.GetSkill(ctx, p, name)
    
    // 2. 使用 corelib/skill 包的共享函数准备执行
    entry := convertViewToEntry(skillView)
    skill.NormalizeSkillForRunner(&entry)
    
    // 3. 执行 bash 步骤（最常见的 skill 类型）
    //    复用 corelib/skill.ExecuteStepsSync()（需要新增）
    result, err := skill.ExecuteStepsSync(ctx, entry, args, skill.ExecConfig{
        WorkDir:  skillDir,
        Env:      buildEnv(p),
        Timeout:  120 * time.Second,
    })
    return result, err
}
```

#### 1.5 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/agentservice/skill_integration.go` | 新增 | SkillToolProvider 接口 + SkillToolBridge 实现 |
| `corelib/agentservice/core_agent_executor.go` | 修改 | 新增 `skillProvider` 字段 + `SetSkillToolProvider` + BuildTools/ExecuteTool 接入 |
| `corelib/skill/exec_sync.go` | 新增 | `ExecuteStepsSync()` — 从 gui/skill_runner.go 提取的同步执行核心 |
| `MaClawSrv/main.go` | 修改 | `executor.SetSkillToolProvider(NewSkillToolBridge(svc))` |

---

## Phase 2: Skill Runner 核心提取到 corelib（优先级 P1）

### 目标

将 `gui/skill_runner.go` 的执行引擎提取到 `corelib/skill/runner.go`，GUI 和 MaClawSrv 共享同一套代码。

### 需要提取的核心逻辑

从 `gui/skill_runner.go` 中提取（约 1500 行核心逻辑）：

1. **步骤迭代**：`executeAsync` → `executeStepWithContext` 循环
2. **变量捕获**：`captureOutputVariables` — 正则提取输出变量
3. **变量替换**：`substituteSkillVarsInString` — `{{key}}` / `${key}` 占位符替换
4. **Poll 循环**：`executeStepWithPoll` — 异步任务轮询
5. **When 条件**：`evaluateSimpleCondition` — 条件步骤
6. **Operations 路由**：operation → labels 映射
7. **Bash 步骤执行**：`runBashStepWithContextFull` — 跨平台 shell 选择
8. **Craft Tool 步骤**：`executeCraftToolCore` — LLM 动态生成脚本
9. **错误分类**：`classifyBashError` — 友好错误提示

### 提取后的接口设计

```go
// corelib/skill/runner.go

// RunnerDeps 是 Runner 的外部依赖接口，由宿主平台实现。
type RunnerDeps interface {
    // ExecuteBash 执行一条 bash 命令，返回 stdout+stderr。
    ExecuteBash(ctx context.Context, command, workDir string, env []string) (string, error)
    
    // GenerateScript 调用 LLM 生成脚本（craft_tool 步骤需要）。
    // 返回 nil 表示不支持 craft_tool。
    GenerateScript(ctx context.Context, task, language string) (string, error)
    
    // OnProgress 报告执行进度。
    OnProgress(stepIndex int, total int, status string)
}

// Runner 是共享的 Skill 执行引擎。
type Runner struct {
    deps RunnerDeps
}

func NewRunner(deps RunnerDeps) *Runner { ... }

// Run 同步执行一个 Skill 的所有步骤，返回最终输出。
func (r *Runner) Run(ctx context.Context, entry *NLSkillEntry, vars map[string]string) (string, error) { ... }
```

### GUI 侧适配

```go
// gui/skill_runner.go — 改为委托给 corelib/skill.Runner

func (r *SkillRunner) executeAsync(...) {
    runner := skill.NewRunner(&guiRunnerDeps{app: r.executor.app, ...})
    result, err := runner.Run(ctx, target, templateVars)
    // ... 处理结果、发送进度事件等 GUI 特有逻辑 ...
}

type guiRunnerDeps struct {
    app *App
    // ... GUI 特有的依赖 ...
}

func (d *guiRunnerDeps) ExecuteBash(ctx context.Context, command, workDir string, env []string) (string, error) {
    // 复用现有的 runBashStepWithContextFull 逻辑
}

func (d *guiRunnerDeps) GenerateScript(ctx context.Context, task, language string) (string, error) {
    // 复用现有的 executeCraftToolCore 逻辑
}
```

### MaClawSrv 侧适配

```go
// corelib/agentservice/skill_integration.go

type srvRunnerDeps struct {
    workspace string
    env       []string
}

func (d *srvRunnerDeps) ExecuteBash(ctx context.Context, command, workDir string, env []string) (string, error) {
    // 复用 agent.ToolBash 的核心逻辑
}

func (d *srvRunnerDeps) GenerateScript(ctx context.Context, task, language string) (string, error) {
    // MaClawSrv 暂不支持 craft_tool，返回 nil
    return "", fmt.Errorf("craft_tool is not supported in MaClawSrv")
}
```

### 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/skill/runner.go` | 新增 | 共享 Runner 引擎（从 gui/skill_runner.go 提取） |
| `corelib/skill/runner_deps.go` | 新增 | RunnerDeps 接口定义 |
| `corelib/skill/runner_test.go` | 新增 | Runner 单元测试 |
| `gui/skill_runner.go` | 重写 | 委托给 corelib/skill.Runner + GUI 特有逻辑 |
| `corelib/agentservice/skill_integration.go` | 修改 | RunSkill 改为使用 corelib/skill.Runner |

---

## Phase 3: 文件操作工具提取（优先级 P1）

### 目标

让 MaClawSrv 的 agent 能够读写工作区内的文件。

### 设计

```go
// corelib/agent/tool_file.go（新增）

// ToolReadFile 读取文件内容，支持行范围和 offset。
func ToolReadFile(args map[string]interface{}, baseDir string) string { ... }

// ToolWriteFile 写入文件内容，支持 mode=overwrite/append。
func ToolWriteFile(args map[string]interface{}, baseDir string) string { ... }

// ToolEditFile 增量编辑文件（find/replace）。
func ToolEditFile(args map[string]interface{}, baseDir string) string { ... }

// ToolListDirectory 列出目录内容。
func ToolListDirectory(args map[string]interface{}, baseDir string) string { ... }
```

**安全约束**：所有文件操作限制在 `Instance.Workspace` 目录内（复用 `ensurePathWithinBase`）。

### 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/agent/tool_file.go` | 新增 | 文件操作工具的共享实现 |
| `corelib/agent/tool_file_test.go` | 新增 | 测试 |
| `gui/im_tools_local.go` | 修改 | `toolReadFile` 等委托给 `agent.ToolReadFile` |
| `corelib/agentservice/core_agent_executor.go` | 修改 | coreToolSpecs 新增文件工具 + ExecuteTool 分发 |

---

## Phase 4: Web 工具提取（优先级 P2）

### 目标

让 MaClawSrv 的 agent 能够搜索网页和抓取 URL 内容。

### 设计

```go
// corelib/agent/tool_web.go（新增）

type WebSearchProvider interface {
    Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error)
}

type WebFetchProvider interface {
    Fetch(ctx context.Context, url string, maxBytes int) (string, error)
}

func ToolWebSearch(ctx context.Context, args map[string]interface{}, provider WebSearchProvider) string { ... }
func ToolWebFetch(ctx context.Context, args map[string]interface{}, provider WebFetchProvider) string { ... }
```

### 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/agent/tool_web.go` | 新增 | Web 工具的共享实现 |
| `corelib/agent/tool_web_test.go` | 新增 | 测试 |
| `gui/im_tools_web.go` | 修改 | 委托给共享实现 |
| `corelib/agentservice/core_agent_executor.go` | 修改 | 新增 web 工具 |

---

## Phase 5: Memory 工具接入（优先级 P2）

### 目标

让 MaClawSrv 的 agent 能够主动保存和召回长期记忆。

### 设计

`corelib/agent/tool_memory.go` 已存在（`ToolMemory` 函数），但 `CoreAgentExecutor` 没有将其暴露为工具。只需在 `coreToolSpecs` 中新增 `memory` 工具定义，在 `ExecuteToolStructured` 中新增 case 调用 `agent.ToolMemory`。

### 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/agentservice/core_agent_executor.go` | 修改 | coreToolSpecs 新增 memory + ExecuteTool 分发 |

---

## 实施优先级与时间线

| Phase | 内容 | 优先级 | 状态 | 说明 |
|-------|------|--------|------|------|
| MCP | MCP Provider 接入 | P0 | 已完成 | `mcp_integration.go` |
| 1 | Skill Provider 接入 | P0 | 已完成 | `skill_integration.go` |
| 2 | Skill Runner 提取到 corelib | P1 | 已完成 | `corelib/skill/exec_sync.go` |
| 3 | 文件操作工具 | P1 | 已完成 | `file_tools.go` |
| 4 | Web 工具 | P2 | 已完成 | `web_tools.go` |
| 5 | Memory 工具接入 | P2 | 已完成 | 加入 `coreToolSpecs` |

---

## 验收标准

### Phase 1 验收
- MaClawSrv agent 调用 `manage_skill(action="list")` → 返回已安装 Skills 列表
- MaClawSrv agent 调用 `manage_skill(action="search", query="pdf")` → 返回搜索结果
- MaClawSrv agent 调用 `manage_skill(action="run", name="xxx", args={...})` → Skill 执行并返回结果
- 新安装的 Skill 在下一次 agent Execute() 时立即可见（无需重启）
- 所有现有 agentservice 测试通过

### Phase 2 验收
- `gui/skill_runner.go` 的核心逻辑移到 `corelib/skill/runner.go`
- GUI 的 Skill 执行行为不变（所有现有 GUI 测试通过）
- MaClawSrv 的 Skill 执行支持：变量捕获、poll 循环、when 条件、operations 路由
- `corelib/skill/runner_test.go` 覆盖所有核心逻辑

### Phase 3 验收
- MaClawSrv agent 调用 `read_file(path="src/main.go")` → 返回文件内容
- 路径限制在 Instance.Workspace 内（越界返回错误）
- GUI 的文件操作行为不变

### Phase 4 验收
- MaClawSrv agent 调用 `web_search(query="...")` → 返回搜索结果
- MaClawSrv agent 调用 `web_fetch(url="...")` → 返回页面内容

### Phase 5 验收
- MaClawSrv agent 调用 `memory(action="save", content="...")` → 保存到长期记忆
- MaClawSrv agent 调用 `memory(action="recall", query="...")` → 召回相关记忆

---

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| Phase 2 提取 Runner 时破坏 GUI 行为 | 高 | 先写测试覆盖现有行为，再提取 |
| 文件操作安全（路径逃逸） | 高 | 复用 `ensurePathWithinBase`，MaClawSrv 强制 workspace 限制 |
| Skill 执行超时 | 中 | 统一 120s 超时，与 GUI 一致 |
| craft_tool 步骤需要 LLM | 中 | Phase 1 不支持，Phase 2 通过 RunnerDeps 接口注入 |

---

## 与已完成的 MCP 修复的关系

本次 MCP 修复（`mcp_integration.go`）已验证了 Provider 接口模式的可行性：

1. 定义 `MCPToolProvider` 接口
2. 实现 `MCPToolBridge`（委托给 Service 的已有方法）
3. 注入到 `CoreAgentExecutor`
4. `BuildTools` 动态追加工具定义
5. `ExecuteToolStructured` 的 default 分支分发到 Provider

Skill、文件、Web、Memory 工具全部复用这个模式。区别只在于：
- MCP/Skill 是**动态工具**（数量和内容随用户配置变化）→ Provider 接口
- 文件/Web/Memory 是**静态工具**（定义固定）→ 直接加入 `coreToolSpecs`

---

## 长期愿景

最终目标是 `CoreAgentExecutor` 的工具能力与 GUI 桌面版对齐（除 GUI 自动化等桌面专属工具外）。通过 Provider 接口模式，新增工具类别只需：

1. 在 `corelib/` 中实现共享逻辑
2. 定义 Provider 接口（如果是动态工具）
3. 在 `CoreAgentExecutor` 中注入
4. GUI 侧委托给共享实现

不再出现"两份不同的代码"。
