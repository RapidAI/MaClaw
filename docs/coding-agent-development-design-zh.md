# MaClaw 编程专用 Agent 开发设计文档

> 状态：待确认  
> 日期：2026-05-06  
> 目标：在正式开发前，确认 MaClaw 是否以及如何引入编程专用 Agent。

## 1. 背景

当前 MaClaw 主 Agent 承担了通用对话、工作流、IM 交互、记忆召回、工具路由、文档生成和编程执行等多类职责。用于普通任务时这是优势，但用于真实代码开发时会出现明显问题：

- 初始上下文过重：主 Agent system prompt、记忆、IM 规则、工作流规则和大量工具定义挤占编码上下文。
- 工具选择噪音大：编程任务只需要少量高频工具，却会暴露大量非编程工具。
- 执行闭环弱：读代码、改代码、跑测试、修复失败、总结变更没有形成稳定协议。
- 会话污染：非编程对话历史和编程迭代混在一起，长任务容易漂移。
- 质量门槛不稳定：是否先读文件、是否保护用户改动、是否运行验证，多数依赖提示词约束。

结论：需要引入编程专用 Agent，但它应作为 MaClaw 的执行层，而不是替代主 Agent。

## 2. 设计目标

### 2.1 核心目标

1. 主 Agent 负责需求理解、用户确认、任务拆解和结果交付。
2. 编程 Agent 负责代码库探索、补丁实现、测试验证和失败修复。
3. 编程 Agent 使用纯净上下文，只注入编码相关规则、任务上下文和精简工具。
4. 编程任务形成稳定闭环：探索 -> 计划 -> 编辑 -> 验证 -> 修复 -> 汇报。
5. 支持后续扩展为多 Agent：Explorer、Worker、Reviewer、Browser/UI Tester。

### 2.2 非目标

首期不做以下内容：

- 不重做一个完整 Claude Code CLI。
- 不替换现有主 Agent。
- 不一次性迁移所有外部编程工具会话能力。
- 不引入远程沙箱或容器隔离。
- 不默认并行多 Agent 写同一代码区域。

## 3. 总体架构

```text
用户
 |
主 Agent
 |  负责：需求理解、澄清确认、任务拆分、调度、交付说明
 |
Coding Agent Orchestrator
 |  负责：创建编程任务、上下文压缩、状态管理、进度回传
 |
编程专用 Agent
 |  负责：读代码、编辑文件、运行命令、修复失败、输出补丁摘要
 |
编码工具层
    search_files / read_file / edit_file / list_directory / bash / git_diff
```

推荐实现方式：同一个 Go 进程内创建独立的编程会话和精简工具集，复用 `corelib/agent.RunLoop`、LLM 配置、HTTP client 和已有本地工具能力。

## 4. 角色边界

| 能力 | 主 Agent | 编程 Agent |
|---|---|---|
| 理解用户业务目标 | 是 | 否 |
| 需求澄清和确认 | 是 | 否 |
| 技术方案选择 | 是，给方向 | 是，基于代码修正细节 |
| 代码搜索和阅读 | 调度 | 是 |
| 文件编辑 | 否，默认不直接做 | 是 |
| 构建和测试 | 调度 | 是 |
| 用户可见进度 | 是 | 产出结构化进度 |
| 记忆写入 | 是 | 否，首期只回传摘要 |
| IM/GUI 交付 | 是 | 否 |

原则：主 Agent 不再亲自执行大规模编码；编程 Agent 不直接和最终用户谈需求。

## 5. 编程 Agent 工作协议

编程 Agent 的 system prompt 应短而硬，包含以下规则：

1. 未读取相关文件前，不得编辑。
2. 优先使用搜索定位代码，不盲猜路径和 API。
3. 只做任务要求范围内的改动。
4. 发现脏工作区或用户改动时，必须绕开或兼容，不能回滚。
5. 优先使用补丁级编辑，避免整文件重写。
6. 每个实现阶段后尽量运行最小验证命令。
7. 测试失败时先诊断，再小步修复。
8. 最终必须输出结构化结果：改动文件、验证命令、结果、残余风险。

## 6. 精简工具集

MVP 只暴露编程必需工具：

| 工具 | 用途 |
|---|---|
| `search_files` | 搜索文件名和代码内容，优先对应 `rg`，不可用时降级 |
| `list_directory` | 查看目录结构 |
| `read_file` | 分段读取文件 |
| `edit_file` | 补丁式修改文件 |
| `bash` | 运行构建、测试、格式化和 Git 只读命令 |
| `git_diff` | 查看当前变更摘要 |

可选工具：

- `ask_orchestrator`：编程 Agent 需要任务澄清时，向主 Agent 返回阻塞问题。
- `browser_check`：前端任务后续接入，用于截图和交互验证。

首期不向编程 Agent 暴露 office、IM、memory、skill market、web search、MCP 管理、定时任务等非编码工具。

## 7. 数据结构草案

```go
type CodingTask struct {
    ID                 string
    Title              string
    UserRequest        string
    ProjectPath        string
    ScopeHints         []string
    AcceptanceCriteria []string
    Constraints        []string
    ContextSummary     string
    MaxIterations      int
}

type CodingTaskResult struct {
    TaskID          string
    Status          string // completed, failed, blocked
    Summary         string
    FilesRead       []string
    FilesModified   []string
    CommandsRun     []CommandResult
    TestsPassed     bool
    VerificationNote string
    Blockers        []string
    ResidualRisks   []string
}

type CommandResult struct {
    Command  string
    ExitCode int
    Summary  string
}
```

## 8. 编程 Agent 执行流程

```text
1. 主 Agent 判断这是编程任务
2. 主 Agent 生成或更新需求/设计/任务拆分
3. 用户确认方案
4. 主 Agent 创建 CodingTask
5. Orchestrator 启动独立 Coding Agent 会话
6. Coding Agent 搜索和阅读相关代码
7. Coding Agent 输出内部计划并执行小步补丁
8. Coding Agent 运行验证命令
9. 如果失败，进入诊断和修复循环
10. Coding Agent 返回 CodingTaskResult
11. 主 Agent 整理为用户可读交付说明
```

## 9. 与现有代码的关系

已有基础可复用：

- `corelib/agent/loop.go`：共享 Agent loop，可作为编程 Agent 执行循环基础。
- `corelib/agent/tool_registry.go` 和 `tool_register_core.go`：可注册精简工具集。
- `corelib/agent/tools_local.go`：已有本地文件和命令能力，可筛选复用。
- `gui/coding_tool_gate.go`：已有编程工具门控思路，可沉淀到 corelib。
- `corelib/agent/tool_subagent.go`、`gui/im_tool_subagent.go`：已有 subagent 入口，可评估复用或重构。
- `docs/coding-subagent-architecture-design.md`：已有纯净上下文方案，本设计作为开发确认版。

建议新增：

```text
corelib/codingagent/
  agent.go              // CodingAgent 类型和 ExecuteTask
  prompt.go             // 编程专用 system prompt
  task.go               // CodingTask / CodingTaskResult
  tools.go              // 精简工具注册
  orchestrator.go       // 单任务调度和进度回调
  result_parser.go      // 结构化结果解析和兜底
```

首期尽量不动 GUI 和 TUI 的大块逻辑，只增加一个可被主 Agent 调用的内部能力。

## 10. MVP 范围

MVP 目标：让 MaClaw 能稳定完成一个中小型代码修改任务，并给出可验证结果。

包含：

1. `CodingAgent.ExecuteTask(ctx, task)`。
2. 独立 conversation history。
3. 编程专用 prompt。
4. 精简工具集。
5. 结构化结果输出。
6. 命令执行和测试结果摘要。
7. 最大迭代数和超时保护。
8. 失败/阻塞状态回传。

不包含：

- 并行 worker。
- 自动创建分支、提交、推送。
- 前端浏览器截图验证。
- 远程 SSH 代码修改。
- 多模型投票和 reviewer agent。

## 11. 确认点

开发前需要确认以下设计选择：

1. 是否同意“主 Agent 调度，编程 Agent 执行”的职责拆分。
2. 首期是否只做进程内 Coding Agent，不做独立 CLI。
3. 首期工具集是否控制在 6 个核心工具。
4. 首期是否只支持本地 workspace，不支持 SSH 远程仓库。
5. 首期是否默认不自动提交 Git commit。
6. 编程 Agent 失败时，是直接返回主 Agent 汇报，还是允许主 Agent 自动重试一次。

建议默认答案：

| 确认项 | 建议 |
|---|---|
| 职责拆分 | 同意 |
| 实现形态 | 进程内 Coding Agent |
| 工具集 | 6 个核心工具 |
| Workspace | 仅本地 |
| Git commit | 不自动提交 |
| 失败重试 | 同一任务最多自动重试 1 次 |

## 12. 实施计划

### Phase 1：核心闭环

- 新增 `corelib/codingagent` 包。
- 定义 `CodingTask` 和 `CodingTaskResult`。
- 编写编程专用 prompt。
- 注册精简工具集。
- 复用 `corelib/agent.RunLoop` 执行独立会话。
- 添加单元测试覆盖任务创建、工具集过滤、结果解析。

验收标准：

- 能对一个测试 fixture 仓库执行简单修改。
- 会先读取相关文件再编辑。
- 能运行指定测试命令并回传结果。
- 不暴露非编码工具。

### Phase 2：主 Agent 接入

- 新增内部工具或 handler：`coding_agent_execute`。
- 主 Agent 在编程任务确认后调用 Coding Agent。
- 将 Coding Agent 进度映射到 GUI/TUI/IM 现有进度回调。
- 将结果摘要交给主 Agent 生成用户可读回复。

验收标准：

- GUI/TUI 至少一个入口能跑通完整编程任务。
- 用户能看到阶段性进度。
- 失败时能看到明确阻塞原因。

### Phase 3：质量增强

- 引入 `git diff` 自检。
- 增加工作区脏状态检查。
- 增加前端任务的浏览器验证接口。
- 增加 Reviewer Agent，只读审查补丁。
- 支持任务拆分后顺序执行多个 CodingTask。

验收标准：

- 能完成多文件修改并给出验证记录。
- 对用户已有改动没有覆盖风险。
- Reviewer 能发现明显缺陷并触发修复。

## 13. 风险与对策

| 风险 | 对策 |
|---|---|
| 编程 Agent 仍然乱改 | prompt 硬规则 + 工具层限制 + diff 自检 |
| 上下文不够 | 单任务独立会话 + 主 Agent 传摘要，不传完整历史 |
| 工具实现重复 | 复用 `corelib/agent` 本地工具，只做筛选包装 |
| 测试命令耗时 | 支持任务级 timeout 和最小验证命令 |
| 结果结构化失败 | 要求 JSON 输出，同时提供文本兜底解析 |
| 主 Agent 和编程 Agent 状态不一致 | 所有执行结果通过 `CodingTaskResult` 单向回传 |

## 14. 推荐决策

建议批准 Phase 1 和 Phase 2，先实现“单个编程 Agent + 本地 workspace + 精简工具 + 验证闭环”。这能最快验证核心假设：MaClaw 的编程质量差，主要原因是否来自主 Agent 上下文污染和工具闭环不足。

若 MVP 明显改善，再进入 Phase 3，扩展 reviewer、browser tester 和多 worker。

