# CodingSubAgent Codex-Inspired 机制性改进

## 来源

- Codex CLI 源码: `codex-rs/core/src/compact.rs`, `tool_output_summarize.rs`, `thread_rollout_truncation.rs`
- MacLaw 现有实现: `gui/coding_subagent.go`（10000+ 行）
- 分析: 从 Codex 的编程执行层面借鉴，非记忆层面（记忆改进已在 `codex-inspired-memory-improvements.md`）

## 改进总览

| # | 改进 | 机制原理 | 文件 |
|---|------|---------|------|
| 1 | Adaptive Tool Result Truncation | 工具结果按信息密度截断，释放 context 给实际编码 | `coding_subagent.go` |
| 2 | Mid-Task Compaction | 单任务内 context 接近上限时做 in-place 压缩 | `coding_subagent_compaction.go` (新) |
| 3 | Built-in Verify Loop | 代码修改后自动运行验证命令，失败自修复 | `coding_subagent.go` |
| 4 | Rollout 持久化 | 每轮迭代 append-only 持久化，crash 后可恢复 | `coding_subagent_rollout.go` (新) |
| 5 | Git Diff 强制完成验证 | 任务结束强制 diff stat，空 diff = 失败 | `coding_subagent.go` |
| 6 | Prompt Cache Awareness | 标记 system prompt 为 cacheable，减少重复 token | `coding_subagent.go` |


## 改进 1: Adaptive Tool Result Truncation

### 原理

Codex 的 `tool_output_summarize` 不全量返回工具结果。对编程场景，工具结果中
信息密度极不均匀——编译错误的第一行比第 500 行有用 100 倍。

当前 SubAgent 的 `ExecuteTool` 直接返回 handler 的完整输出。一个 3000 行的
`read_file` 结果占 ~6000 token，但 LLM 可能只需要其中 30 行。80 轮迭代中
如果有 20 次 read_file，仅此一项就消耗 120K token——超过 128K 模型的全部容量。

### 机制

在 `codingSubAgentCallbacks.ExecuteToolStructured` 返回前，对结果做 adaptive truncation：

```
工具结果 → 按类型路由：
  read_file:
    > 3000 token → head 80 lines + "[... {N} lines omitted, use offset to read specific section ...]" + tail 30 lines
    ≤ 3000 token → 全量返回
  
  bash:
    > 1500 token → 错误行优先保留 + head 15 lines + tail 15 lines + "[truncated {N} lines]"
    ≤ 1500 token → 全量返回
  
  list_directory:
    > 200 entries → 目录名 + 文件数统计，不含 metadata
    ≤ 200 entries → 全量返回
  
  Glob/ripgrep:
    > 50 matches → 前 30 matches + "[... {N} more matches ...]"
    ≤ 50 matches → 全量返回
  
  git_diff:
    > 2000 token → diff stat 摘要 + 前 50 行实际 diff
    ≤ 2000 token → 全量返回
```

### 关键设计

- **不丢失可达性**：截断后的提示告诉 LLM 如何精准获取被省略的部分（`read_file(offset=80, lines=30)`）
- **错误行优先**：bash 输出中匹配 `error|Error|ERROR|failed|FAILED|panic|PANIC` 的行优先保留
- **不影响短结果**：阈值设置确保典型编辑操作（edit_file 确认、write_file 确认）不被截断

### 预估收益

20 次 read_file × 3000 行文件：
- 修复前：20 × 6000 = 120K token
- 修复后：20 × 1500 = 30K token（head 80 + tail 30 ≈ 110 行 × ~13 token/行）
- 节省：**90K token**，相当于释放了 70% 的 context 给实际编码


## 改进 2: Mid-Task Compaction

### 原理

Codex 的 `compact.rs` 在 context 接近窗口上限时，**同一个任务内**压缩对话历史。
不终止任务，不重启 context——原地压缩后继续。

当前 SubAgent 的 `codingSubAgentPerTaskMaxIterations = 80`。但 context 膨胀
不是迭代次数的函数——5 次 read_file 大文件可能比 50 次 edit_file 消耗更多 token。
SubAgent 依赖 `corelib/agent.RunLoop` 的硬迭代上限退出，没有 mid-loop 的 token 感知。

大型重构任务（如"重构 50 个文件的 import 路径"）在第 30 轮就可能因 context 满而
退化（模型开始产出空响应或幻觉），远早于 80 轮上限。

### 机制

新增 `coding_subagent_compaction.go`，实现 `SubAgentCompactor`：

**触发条件**：在 `corelib/agent.RunLoop` 的每轮迭代开始前检查：
```
estimatedTokens(conversation) > effectiveContextWindow * 0.75
```

**压缩策略**（借鉴 Codex `build_compacted_history`）：
1. 保留 system prompt（不变）
2. 保留最近 5 轮完整的工具调用+结果（recency window）
3. 保留所有 write_file/edit_file 的文件路径列表（不含 content，作为 "已修改" 锚点）
4. 中间部分用 LLM 摘要替换（如无 LLM，用静态占位符：文件列表 + 命令列表）
5. 注入 recovery prefix（借鉴改进记录中的 `compactionRecoveryPrefix`）

**与 Codex 的差异**：
- Codex 保留用户消息原文（20K token budget）。SubAgent 没有用户消息——只有系统任务描述
- SubAgent 的关键锚点是"已修改的文件路径列表"——告诉 LLM 哪些文件已经被改过，避免重复修改
- 只有无 LLM 时才回退到静态占位符。有 LLM 时用 15s 超时的轻量摘要调用

**实现接口**：
```go
// SubAgentCompactor handles mid-task context compaction.
type SubAgentCompactor struct {
    cfg           corelib.MaclawLLMConfig
    httpClient    *http.Client
    contextWindow int // effective context tokens (from cfg)
}

// ShouldCompact checks if conversation needs compaction.
func (c *SubAgentCompactor) ShouldCompact(conversation []llm.ConversationEntry) bool

// Compact performs in-place compaction, returning the compacted conversation.
func (c *SubAgentCompactor) Compact(conversation []llm.ConversationEntry, 
    filesModified []string, filesCreated []string) []llm.ConversationEntry
```

**注入点**：`corelib/agent/loop.go` 的 `RunLoop` 需要新增 hook：
```go
type LoopHooks struct {
    // ... 已有 hooks ...
    
    // BeforeIteration is called before each LLM request.
    // Returns potentially modified conversation (for compaction).
    BeforeIteration func(conversation []llm.ConversationEntry) []llm.ConversationEntry
}
```

SubAgent 的 `codingSubAgentCallbacks` 注册 `BeforeIteration` hook，
调用 `SubAgentCompactor.ShouldCompact` + `Compact`。

### 预估收益

50 文件重构任务：
- 修复前：第 30 轮 context 满 → 模型退化 → hard exit → orchestrator 重试（新 context）→ 重复前 15 轮的探索
- 修复后：第 30 轮 compaction → 释放 50K token → 继续工作到第 60 轮完成 → 不重试


## 改进 3: Built-in Verify Loop

### 原理

Codex 的 `loop_command.rs` 实现了 modify → verify → fix 循环。代码不是写完
就算完——必须通过验证。MacLaw 的 SubAgent system prompt 已经要求"运行匹配的
验证命令"，但这是 **prompt 层建议**——模型可以（且经常）忽略。

当前架构：SubAgent 执行 → 返回 → orchestrator 检查 `QualityStatus` →
失败则用新 context 重试。问题：重试 = 新 context = 重复探索。

Codex 的做法是在**同一个 context 中**循环修复——模型看到失败输出后立即修改，
不需要重新理解代码库。

### 机制

不改 SubAgent 的 system prompt（它已经要求验证）。改在**工具执行层**注入结构化验证：

**Phase 1：自动发现验证命令**

新增 `detectProjectVerifyCommands(projectPath)` 函数：
```
扫描项目根目录：
  package.json 有 "test" script → "npm test" / "yarn test"
  Makefile 有 test/check target → "make test"
  Cargo.toml 存在 → "cargo test"
  go.mod 存在 → "go build ./... && go test ./..."
  pyproject.toml 有 pytest → "pytest"
  CMakeLists.txt + build/ 存在 → "cmake --build build"
```

**Phase 2：RunLoop 退出前注入验证**

不修改 `corelib/agent.RunLoop` 的主循环。在 SubAgent 的 `ExecuteTask` 中，
`agent.RunLoop` 返回后：

```go
func (s *CodingSubAgent) ExecuteTask(...) *CodingSubAgentResult {
    // ... 现有 RunLoop 调用 ...
    result := agent.RunLoop(cb, ...)
    
    // Phase 3: Post-loop verification (if model didn't verify itself)
    if !cb.hasRunVerification() && len(cb.getFilesModified()) > 0 {
        verifyCmd := s.detectOrFallbackVerifyCommand()
        if verifyCmd != "" {
            verifyResult := cb.ExecuteTool("bash", verifyCmd)
            if isVerificationFailure(verifyResult) {
                // 在同一个 context 中注入失败信息，让模型修复
                fixResult := agent.RunLoopWithUserContent(cb, 
                    fmt.Sprintf("验证命令 `%s` 失败。错误输出：\n%s\n\n请修复代码使验证通过。", 
                        verifyCmd, truncateVerifyOutput(verifyResult, 2000)),
                    result.Conversation, // 复用已有 conversation
                    s.httpClient,
                )
                // 合并结果
                result = mergeLoopResults(result, fixResult)
            }
        }
    }
    // ... 现有 audit/quality 逻辑 ...
}
```

**关键设计**：
- `RunLoopWithUserContent` 接受已有 conversation（Codex 的做法），不创建新 context
- 修复循环最多 2 轮（verify → fix → verify → fix → 放弃），防止无限循环
- `hasRunVerification()` 检测模型是否已经在 RunLoop 内部自行运行了验证命令，避免重复
- 只在有文件修改时验证（纯读取任务不需要）

### 与现有 VerificationStatus 的关系

当前 `summarizeSubAgentVerification` 从 `commandsRun` 审计日志中检测模型是否
运行了验证命令。改进 3 不替换这个审计——它在模型**未自行验证**时补充一次外部验证。

两者关系：
- 模型自行验证 + 通过 → VerificationStatus=passed（现有逻辑，不触发改进 3）
- 模型自行验证 + 失败 → VerificationStatus=failed（现有逻辑，不触发改进 3）
- 模型未验证 → 改进 3 注入验证 → 成功/失败均更新 VerificationStatus


## 改进 4: Rollout 持久化

### 原理

Codex 每个 session 的完整交互写入 `.codex/runs/{session_id}.jsonl`。crash 后
从 rollout 恢复，不从头重试。

当前 SubAgent 的对话历史是纯内存 `[]llm.ConversationEntry`。任务执行到第 60 轮
（可能已修改了 30 个文件）时进程 crash → 全部对话丢失 → orchestrator 标记为 failed
→ 从头重试 → 重新读取和修改那 30 个文件（文件已被改过，重试可能产生冲突）。

更严重的是：crash 后文件已被修改但 context 丢失。重试时 LLM 看到的是修改后的
文件但不知道是自己改的，可能进一步错误修改。

### 机制

新增 `coding_subagent_rollout.go`：

```go
// SubAgentRollout manages append-only persistence of coding task execution.
type SubAgentRollout struct {
    file     *os.File
    taskID   string
    encoder  *json.Encoder
    seqNum   uint64
}

type RolloutEntry struct {
    Seq       uint64    `json:"seq"`
    Timestamp time.Time `json:"ts"`
    Type      string    `json:"type"` // "tool_call" | "tool_result" | "assistant" | "compaction" | "status"
    Name      string    `json:"name,omitempty"`  // tool name
    ArgsHash  string    `json:"args_hash,omitempty"` // SHA256 前 8 字符（不存完整 args，太大）
    Result    string    `json:"result,omitempty"` // 截断到 500 rune
    FilePath  string    `json:"file,omitempty"`  // write_file/edit_file 的目标路径
    Status    string    `json:"status,omitempty"` // "running" | "completed" | "failed" | "crashed"
}
```

**写入时机**：
- `ExecuteToolStructured` 返回后：写入 `tool_call` + `tool_result` entry
- `RunLoop` 每轮 assistant 响应后：写入 `assistant` entry（只存摘要，不存完整 content）
- Compaction 发生后：写入 `compaction` entry（标记压缩点）
- 任务开始/结束：写入 `status` entry

**恢复时机**：
- `ExecuteTask` 开始时检查 `{dataDir}/coding_runs/{task_id}.jsonl` 是否存在
- 存在且 status != "completed"/"failed" → crash recovery
- 从 rollout 中提取已修改文件列表 → 注入到 system prompt："以下文件已被上一次执行修改过"
- 不恢复完整 conversation（太大），只恢复关键状态

**清理**：
- 任务正常完成后：rollout 文件保留 24h 后删除
- 项目变更时：`StartNewTask` 清理旧 rollout

### 存储预算

单任务 80 轮迭代 × 每轮 ~200 bytes = ~16KB/任务。极轻量。

### 不存储完整 args/result 的原因

tool_call 的 args 可能是 15K 的文件内容（write_file）。存储完整 args 会使
rollout 文件膨胀到 MB 级别。只存 hash + 文件路径足够恢复关键状态。
实际文件内容已经在磁盘上（write_file 已经写过了）。


## 改进 5: Git Diff 强制完成验证

### 原理

当前 SubAgent 已有 `ensureFinalGitDiff` 和 `applySubAgentDiffOutcome`——
如果模型没调用 git_diff，SubAgent 在 `ExecuteTask` 末尾补调一次。
diff 为空 + 有文件修改记录 → 标记为 failed。

这个机制**已经实现**。但有一个盲点：

`CodingSubAgentResult.GitDiffSummary` 只存储了 diff 的文本摘要，不存储
结构化的 `DiffStat`（多少文件、多少 insertions/deletions）。orchestrator
的进度报告和最终报告只能展示文字摘要，无法展示类似 GitHub PR 的 `+500 -200`
统计。

### 机制

增强 `ensureFinalGitDiff`，解析 `git diff --stat` 的输出为结构化数据：

```go
type SubAgentDiffStat struct {
    FilesChanged int
    Insertions   int
    Deletions    int
    // 每个文件的变更行数
    FileStats    []SubAgentFileDiffStat
}

type SubAgentFileDiffStat struct {
    Path       string
    Insertions int
    Deletions  int
}
```

`CodingSubAgentResult` 新增 `DiffStat *SubAgentDiffStat` 字段。

orchestrator 的 `FinalReport()` 使用 `DiffStat` 生成结构化报告：
```
## Task T3: 实现用户登录 API
✅ Passed | 12 iterations | 3 files changed (+145 -23)
  src/auth/login.go    +98 -5
  src/auth/login_test.go +42 -3
  src/routes/router.go  +5 -15
```

### 与已有实现的关系

不替换 `ensureFinalGitDiff`——在其基础上增加 `--stat` 解析。
`applySubAgentDiffOutcome` 逻辑不变（空 diff + 有修改 = failed）。


## 改进 6: Prompt Cache Awareness

### 原理

SubAgent 80 轮迭代中，system prompt 完全不变（`cachedSystemPrompt` 已在
Go 侧缓存字符串构建）。但每次 LLM API 调用仍然在 messages[0] 发送完整的
system prompt tokens。

Anthropic 的 `cache_control: {type: "ephemeral"}` 和 OpenAI Responses API
的 `previous_response_id` 都能让 provider 侧缓存 system prompt 的 KV cache。
80 轮迭代中：第 1 轮 cache miss（冷启动），第 2-80 轮 cache hit → 节省
~2000 token × 79 轮 = ~158K input tokens 的计费/延迟。

### 机制

**方案 A（Anthropic cache_control）**：

如果 LLM config 的 provider 支持 cache_control，SubAgent 在构建 messages 时
对 system message 标记 `cache_control`：

```go
// corelib/llm/client.go or client_anthropic.go
func buildAnthropicMessages(conversation []ConversationEntry, cfg MaclawLLMConfig) []map[string]interface{} {
    messages := ...
    // Mark system prompt for caching when in SubAgent mode (prompt doesn't change)
    if cfg.EnablePromptCache && len(messages) > 0 && messages[0]["role"] == "system" {
        content := messages[0]["content"]
        messages[0]["content"] = []map[string]interface{}{
            {
                "type": "text",
                "text": content,
                "cache_control": map[string]string{"type": "ephemeral"},
            },
        }
    }
    return messages
}
```

**方案 B（OpenAI Responses API）**：

如果 LLM config 使用 Responses API（`protocol: "responses"`），使用
`previous_response_id` 链式引用，避免重发 system prompt：

```go
// RunLoop 中维护 previousResponseID
var previousResponseID string
for iteration := 0; iteration < maxIterations; iteration++ {
    resp := doLLMRequest(conversation, tools, previousResponseID)
    previousResponseID = resp.ID
    // ...
}
```

**方案 C（通用 fallback，无 provider 依赖）**：

当 provider 不支持上述机制时，不做额外处理——当前行为不变。
token 节省是成本优化，不影响质量。

### 实现选择

优先方案 A（Anthropic），因为 MacLaw 的主力编程模型（DeepSeek/智谱 GLM）
大多走 OpenAI 兼容 API，不支持 `cache_control`。但如果用户配置了 Anthropic
模型（Claude），应自动启用。

`MaclawLLMConfig` 新增 `EnablePromptCache bool` 字段，默认对已知支持
cache_control 的 provider（Anthropic、Fireworks）自动启用。

### 预估收益

80 轮迭代 × system prompt 2000 token：
- 修复前：80 × 2000 = 160K input tokens 计费
- 修复后：2000（miss）+ 79 × 2000 × 0.1（cached rate）= 17.8K input tokens 等价计费
- 节省：~89% 的 system prompt 成本（Anthropic 的 cached token 按 0.1× 计费）


## 实施方案

### Phase 1: Adaptive Tool Result Truncation（立即实施）

修改 `gui/coding_subagent.go` 的 `ExecuteToolStructured` 方法。

在 `result := c.executeToolWithOutcome(name, argsJSON)` 返回后、返回给
RunLoop 之前，对 `result.Text` 做 adaptive truncation。

新增函数：

```go
// truncateToolResultForSubAgent applies adaptive truncation to tool results
// based on tool type and information density.
func truncateToolResultForSubAgent(toolName string, result string) string {
    tokenEstimate := estimateBytesToTokens(len(result))
    
    switch {
    case toolName == "read_file":
        if tokenEstimate <= 3000 { return result }
        return truncateReadFileResult(result)
        
    case toolName == "bash":
        if tokenEstimate <= 1500 { return result }
        return truncateBashResult(result)
        
    case toolName == "list_directory":
        lines := strings.Count(result, "\n")
        if lines <= 200 { return result }
        return truncateListDirResult(result)
        
    case toolName == "Glob" || toolName == "ripgrep":
        matches := countSearchMatches(result)
        if matches <= 50 { return result }
        return truncateSearchResult(result, 30)
        
    case toolName == "git_diff":
        if tokenEstimate <= 2000 { return result }
        return truncateGitDiffResult(result)
        
    default:
        return result
    }
}

func truncateReadFileResult(result string) string {
    lines := strings.Split(result, "\n")
    if len(lines) <= 120 { return result }
    
    headLines := 80
    tailLines := 30
    omitted := len(lines) - headLines - tailLines
    
    var b strings.Builder
    for _, line := range lines[:headLines] {
        b.WriteString(line)
        b.WriteString("\n")
    }
    b.WriteString(fmt.Sprintf("\n[... %d lines omitted. Use read_file with offset=%d to read from this point ...]\n\n", 
        omitted, headLines+1))
    for _, line := range lines[len(lines)-tailLines:] {
        b.WriteString(line)
        b.WriteString("\n")
    }
    return b.String()
}

func truncateBashResult(result string) string {
    lines := strings.Split(result, "\n")
    if len(lines) <= 40 { return result }
    
    // Priority: error lines first
    var errorLines []string
    errorRe := regexp.MustCompile(`(?i)(error|Error|ERROR|failed|FAILED|panic|PANIC|fatal|FATAL)`)
    for _, line := range lines {
        if errorRe.MatchString(line) {
            errorLines = append(errorLines, line)
        }
    }
    
    headLines := 15
    tailLines := 15
    
    var b strings.Builder
    if len(errorLines) > 0 {
        b.WriteString("=== Error lines (prioritized) ===\n")
        for _, line := range errorLines[:min(len(errorLines), 10)] {
            b.WriteString(line)
            b.WriteString("\n")
        }
        b.WriteString("\n=== Output (head + tail) ===\n")
    }
    
    for _, line := range lines[:min(headLines, len(lines))] {
        b.WriteString(line)
        b.WriteString("\n")
    }
    if len(lines) > headLines+tailLines {
        b.WriteString(fmt.Sprintf("\n[... %d lines truncated ...]\n\n", len(lines)-headLines-tailLines))
    }
    for _, line := range lines[max(headLines, len(lines)-tailLines):] {
        b.WriteString(line)
        b.WriteString("\n")
    }
    return b.String()
}
```

注入点在 `ExecuteToolStructured` 返回前：

```go
func (c *codingSubAgentCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
    result := c.executeToolWithOutcome(name, argsJSON)
    // ... 现有的 tracking 逻辑 ...
    
    // Adaptive truncation: reduce token consumption for verbose tool outputs
    result.Result = truncateToolResultForSubAgent(name, result.Result)
    
    return agent.ToolExecutionResult{Result: result.Result}
}
```

### Phase 2: Mid-Task Compaction

新增 `gui/coding_subagent_compaction.go`。

**注入方式**：不修改 `corelib/agent/loop.go` 的 `RunLoop`（避免影响其他消费方）。
改为在 SubAgent 的 `ExecuteToolStructured` 开头检查是否需要 compaction：

```go
func (c *codingSubAgentCallbacks) maybeCompactBeforeNextTool() {
    if c.compactor == nil { return }
    if !c.compactor.ShouldCompact(c.getConversation()) { return }
    
    compacted := c.compactor.Compact(
        c.getConversation(),
        c.getFilesModified(),
        c.getFilesCreated(),
    )
    c.replaceConversation(compacted)
    c.trackCompaction()
}
```

但问题：`codingSubAgentCallbacks` 无法直接访问 `RunLoop` 内部的 conversation。
`LoopCallbacks` 接口没有暴露 conversation 的 getter/setter。

**解决方案**：利用已有的 `LoopHooks`。`corelib/agent/loop.go` 的 `LoopHooks` 
接口已有 `PreLLMCall` hook。新增 `ConversationTransformer` hook：

```go
// LoopHooks (in corelib/agent/loop.go)
type LoopHooks struct {
    // ... existing hooks ...
    
    // ConversationTransformer is called before each LLM request with the current
    // conversation. It may return a modified conversation (e.g., compacted).
    // Return nil to keep the conversation unchanged.
    ConversationTransformer func([]ConversationEntry) []ConversationEntry
}
```

`RunLoop` 在构建 LLM request 前调用：
```go
if hooks.ConversationTransformer != nil {
    if transformed := hooks.ConversationTransformer(conversation); transformed != nil {
        conversation = transformed
    }
}
```

SubAgent 注册这个 hook 来做 mid-task compaction。

### Phase 3: Built-in Verify Loop

在 `ExecuteTask` 中，`agent.RunLoop(cb, ...)` 返回后、audit 逻辑之前：

```go
// Post-loop verification: if model didn't run verification itself
if !hasVerificationInAudit(cb.getCommandsRun(), audit.LastEditSeq) && 
   len(cb.getFilesModified()) > 0 {
    
    verifyCmd := detectProjectVerifyCommand(s.projectPath)
    if verifyCmd != "" {
        s.runPostLoopVerifyFixCycle(cb, &result, verifyCmd, maxVerifyFixRounds)
    }
}
```

`runPostLoopVerifyFixCycle` 实现 verify → fix → verify 循环：

```go
func (s *CodingSubAgent) runPostLoopVerifyFixCycle(
    cb *codingSubAgentCallbacks, 
    result *agent.LoopResult, 
    verifyCmd string,
    maxRounds int,
) {
    for round := 0; round < maxRounds; round++ {
        // Run verification
        verifyResult := cb.ExecuteTool("bash", fmt.Sprintf(`{"command":%q,"timeout":60}`, verifyCmd))
        
        if !isVerificationFailure(verifyResult) {
            // Passed! Update result
            log.Printf("[coding-subagent] post-loop verify passed on round %d", round+1)
            return
        }
        
        // Failed — ask model to fix in same context
        fixPrompt := fmt.Sprintf(
            "验证命令 `%s` 失败。错误输出：\n```\n%s\n```\n请修复代码使验证通过。只做最小必要修改。",
            verifyCmd, truncateForSubAgent(verifyResult, 2000))
        
        fixResult := agent.RunLoopWithUserContent(cb, fixPrompt, result.Conversation, s.httpClient)
        result.Conversation = fixResult.Conversation
        result.Iterations += fixResult.Iterations
        result.ToolCalls += fixResult.ToolCalls
    }
}
```

### Phase 4: Rollout 持久化

新增 `gui/coding_subagent_rollout.go`。

**写入时机**——hook 到 `ExecuteToolStructured`：
```go
func (c *codingSubAgentCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
    // ... execution ...
    
    // Persist to rollout (non-blocking, best-effort)
    if c.rollout != nil {
        c.rollout.AppendToolCall(name, argsHashShort(argsJSON), result.filePath())
        c.rollout.AppendToolResult(name, truncateForRollout(result.Text, 500))
    }
    
    return ...
}
```

**恢复时机**——`ExecuteTask` 开头：
```go
func (s *CodingSubAgent) ExecuteTask(task *TaskItem, ...) *CodingSubAgentResult {
    // Check for crash recovery
    rolloutPath := s.rolloutPath(task)
    if existing := LoadRollout(rolloutPath); existing != nil && existing.Status == "running" {
        log.Printf("[coding-subagent] recovering crashed task T%d from rollout", taskDisplayNumber(task))
        // Inject recovery context into system prompt
        task.RecoveryContext = existing.BuildRecoveryContext()
        // Don't replay conversation — just tell model what was already done
    }
    
    // Create new rollout
    rollout, _ := NewRollout(rolloutPath, task.ID())
    defer rollout.Close()
    // ...
}
```

**`BuildRecoveryContext()` 生成的恢复信息**：
```
## ⚠️ Crash Recovery
上一次执行此任务时进程中断。以下是已完成的工作：
- 已修改文件：src/auth/login.go, src/auth/login_test.go
- 已创建文件：src/auth/session.go
- 最后执行的命令：go test ./src/auth/... (失败)
- 中断点：第 45 轮迭代

请检查这些文件的当前状态，确认上次修改的正确性，然后继续完成任务。
```

### Phase 5: Git Diff Stat 结构化

增强 `ensureFinalGitDiff`：

```go
func (c *codingSubAgentCallbacks) ensureFinalGitDiff(...) (bool, string) {
    // ... 现有 git diff 逻辑 ...
    
    // 额外调用 git diff --stat 解析结构化数据
    statResult := executeCodingBashWithContext(ctx, map[string]interface{}{
        "command":     "git diff --stat -- .",
        "working_dir": workDir,
        "timeout":     float64(15),
    }, nil)
    
    c.mu.Lock()
    c.diffStat = parseGitDiffStat(statResult.toolResult().Text)
    c.mu.Unlock()
    
    return diffChecked, diffSummary
}

func parseGitDiffStat(statOutput string) *SubAgentDiffStat {
    // Parse "N files changed, M insertions(+), K deletions(-)" 格式
    // Parse per-file stats
}
```

`CodingSubAgentResult` 新增 `DiffStat *SubAgentDiffStat`。

### Phase 6: Prompt Cache Awareness

在 `codingSubAgentCallbacks.GetLLMConfig()` 中：

```go
func (c *codingSubAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
    cfg := c.subagent.cfg
    // Enable prompt caching for SubAgent — system prompt never changes
    cfg.EnablePromptCache = true
    return cfg
}
```

`corelib/llm/client_anthropic.go` 的 `buildAnthropicSystemMessages` 检查
`cfg.EnablePromptCache` 后标记 `cache_control`。

对 OpenAI 兼容 API（DeepSeek/智谱 GLM），当前无 cache_control 机制——
Provider 侧可能有隐式 prompt cache（DeepSeek 有，按前缀匹配）。
不需要客户端额外操作，但确保 system message 在 messages[0] 且内容稳定
（当前 `cachedSystemPrompt` 已保证）。


## 实施顺序

1. **Phase 1（改进 1）**：✅ 已完成。Adaptive Tool Result Truncation。
2. **Phase 5（改进 5）**：✅ 已完成。Git Diff Stat 结构化。
3. **Phase 6（改进 6）**：✅ 已完成。Prompt Cache Awareness。
4. **Phase 3（改进 3）**：✅ 已完成。Built-in Verify Loop。
5. **Phase 2（改进 2）**：✅ 已完成。Mid-Task Compaction。
6. **Phase 4（改进 4）**：✅ 已完成。Rollout 持久化。

总工作量：全部 6 项均已实施。


## 验收标准

- 改进 1：3000 行文件的 read_file 结果 ≤ 1500 token（head 80 + tail 30 = 110 行）
- 改进 2：50 文件重构任务能在单次 ExecuteTask 中完成（不因 context 满退化）
- 改进 3：模型未自行验证时，`go build` 失败 → 自动修复 → 通过
- 改进 4：进程 crash 后重启 → rollout 恢复 → 不从头重试
- 改进 5：`CodingSubAgentResult.DiffStat.FilesChanged > 0` 当有文件修改时
- 改进 6：Anthropic 模型第 2-80 轮 `cache_read_input_tokens > 0`（API 返回的计量字段）
- 所有已有 SubAgent 测试通过（10 个 coding_subagent_test.go + 14 个 skills test）


## 实施状态

### ✅ Phase 1: Adaptive Tool Result Truncation（已完成）

**修改文件**：
- `gui/coding_subagent_truncation.go`（新增）：truncation 逻辑
- `gui/coding_subagent_truncation_test.go`（新增）：8 个测试
- `gui/coding_subagent.go`：`ExecuteToolStructured` 注入截断调用

**测试结果**：9/9 PASS（含 1 个已有的关联测试）

### ✅ Phase 5: Git Diff Stat 结构化（已完成）

**修改文件**：
- `gui/coding_subagent_diff_stat.go`（新增）：`SubAgentDiffStat` 类型 + `parseGitDiffStat` 解析器
- `gui/coding_subagent_diff_stat_test.go`（新增）：7 个测试
- `gui/coding_subagent.go`：`CodingSubAgentResult.DiffStat` 字段 + `ensureFinalGitDiff` 增强 + `ensureDiffStat`/`getDiffStat` 方法

**测试结果**：7/7 PASS

### ✅ Phase 3: Built-in Verify Loop（已完成）

**修改文件**：
- `gui/coding_subagent_verify.go`（新增）：verify loop 实现 + 项目验证命令检测 + 自验证检测
- `gui/coding_subagent.go`：`ExecuteTask` 中 RunLoop 返回后注入 verify loop

**机制**：
- `detectProjectVerifyCommand`：自动发现 go.mod/Cargo.toml/package.json/pyproject.toml/CMakeLists.txt/Makefile 的验证命令
- `hasSubAgentSelfVerified`：检测模型是否已在 loop 内自行验证
- `runPostLoopVerifyFixCycle`：verify→fix→verify→fix 循环（最多 2 轮），在同一 conversation context 中

### ✅ Phase 2: Mid-Task Compaction（已完成）

**修改文件**：
- `gui/coding_subagent_compaction.go`（新增）：`SubAgentCompactor` + `codingSubAgentHooks` + `buildLoopHooks`
- `corelib/agent/loop.go`：`LoopHooks` 接口新增 `TransformConversation` 方法 + `LoopResult` 新增 `Conversation` 字段 + 循环中注入 transform 调用
- `gui/coding_subagent.go`：`RunLoop` 调用传入 hooks

**机制**：
- `ShouldCompact`：token 估算 > context window × 0.75 时触发
- `Compact`：保留 system + task + recent 5 轮 + 静态摘要（已修改文件/已执行命令/统计）
- 最多 3 次 compaction/任务（防止无限压缩）
- 通过 `LoopHooks.TransformConversation` 注入 loop 内部，零侵入

### ✅ Phase 4: Rollout 持久化（已完成）

**修改文件**：
- `gui/coding_subagent_rollout.go`（新增）：`SubAgentRollout` + `LoadRolloutRecovery` + `CleanOldRollouts`

**机制**：
- `NewSubAgentRollout`：创建 append-only JSONL 文件，记录 `status: running`
- `AppendToolCall`/`AppendToolResult`：每次工具执行后追加记录（路径 + hash + 结果摘要）
- `Complete`/`Fail`：标记终态
- `LoadRolloutRecovery`：检测 `status: running`（crash 标志）→ 提取已修改文件/已执行命令 → 生成恢复 prompt
- `CleanOldRollouts`：24h 后自动清理
- 只存 metadata（路径 + hash + 300 rune 摘要），单任务 ~16KB

### ✅ Phase 6: Prompt Cache Awareness（已完成）

**修改文件**：
- `corelib/types.go`：`MaclawLLMConfig` 新增 `EnablePromptCache bool` 字段
- `corelib/llm/client_anthropic.go`：`BuildAnthropicMessagesRequestBody` 中 system 字段根据 `EnablePromptCache` 标记 `cache_control:{type:"ephemeral"}`
- `gui/coding_subagent.go`：`GetLLMConfig()` 始终设置 `EnablePromptCache = true`

**机制**：
- SubAgent 的 `GetLLMConfig()` 返回 `EnablePromptCache=true`
- Anthropic 路径：system prompt 从 `"system": "text"` 变为 `"system": [{type: "text", text: "...", cache_control: {type: "ephemeral"}}]`
- OpenAI/DeepSeek 路径：无变化（这些 provider 有隐式 prefix caching，不需要客户端标记）
- 主 Agent 不受影响（其 `GetLLMConfig()` 不设置 `EnablePromptCache`）

**预估收益**（Anthropic Claude 模型时）：
- 80 轮迭代 × 2000 token system prompt
- Cache miss: 1 次（第一轮）
- Cache hit: 79 次 → 按 0.1× 计费
- 等效从 160K → 17.8K input tokens 计费（-89%）
