# /btw 侧查询 SubAgent 设计文档

## 概述

实现 `/btw <message>` 命令——用户在 AI 助手面板中输入 `/btw 最新的 AI 安全进展` 时，系统启动一个独立的轻量 agent loop（BtwSubAgent），在不污染主对话历史的前提下完成信息查询，将结果输出给用户。

类似 Claude Code 的 `/btw` 功能：side query，不打断主任务上下文。

## 机制性设计

### 核心原则

1. **独立 context**：`/btw` 在独立的 agent loop 中运行，不消耗主对话的 token 预算
2. **不污染主历史**：中间的工具调用（web_search、read_file 等）不进入主对话历史，只有最终结果作为单条 assistant 消息追加
3. **精简工具集**：只提供信息查询相关工具（web_search、web_fetch、read_file、memory recall），不提供编码/SSH/浏览器等工具
4. **流式输出**：复用主 agent loop 的 `onToken` 回调，实时显示查询进度
5. **可取消**：用户可以取消 `/btw` 查询，不影响主对话

### 架构

```
用户: "/btw 最新的 AI 安全进展"
  ↓
[handleIMMessageWithLoop — slash command 拦截]
  ↓ 提取 "/btw " 后的 message
  ↓
[BtwSubAgent.Execute(message)]
  ├─ System Prompt: ~1K token（精简的查询助手角色）
  ├─ Tools: web_search, web_fetch, read_file, memory (4 个, ~500 token)
  ├─ Max iterations: 15
  ├─ 独立 conversation history
  └─ 复用主 handler 的工具实现（零重复代码）
  ↓
[RunLoop — corelib/agent.RunLoop]
  ├─ LLM 生成搜索策略
  ├─ 执行 web_search / web_fetch
  ├─ 整理结果
  └─ 返回 LoopResult
  ↓
[结果处理]
  ├─ 最终文本追加到主对话历史（单条 assistant 消息）
  ├─ 前缀 "🔍 /btw 查询结果：" 标识
  └─ 返回 IMAgentResponse
```

### 与 CodingSubAgent 的对称性

| 维度 | CodingSubAgent | BtwSubAgent |
|------|---------------|-------------|
| 用途 | 纯净上下文编码 | 纯净上下文信息查询 |
| System prompt | ~2K（编码规范） | ~1K（查询助手） |
| 工具集 | 6 个（read/write/edit/bash/list） | 4 个（web_search/web_fetch/read_file/memory） |
| Max iterations | 50 | 15 |
| 历史隔离 | ✅ 独立 | ✅ 独立 |
| 工具委托 | handler.toolReadFile 等 | handler.toolWebSearch 等 |
| 取消机制 | loopCtx.IsCancelled() | loopCtx.IsCancelled() |

### 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `gui/btw_subagent.go` | 新增 | BtwSubAgent + btwCallbacks + system prompt + tool definitions |
| `gui/im_message_handler.go` | 修改 | `/btw` 命令拦截 + `handleBtwCommand` + `/help` 更新 |
| `tui/app.go` | 修改 | `/btw` 路由到 async handleChatSend + `tuiBtwCallbacks` + `/help` 更新 + `cancellable` 接口 |
| `docs/btw-subagent-design.md` | 新增 | 本设计文档 |

### 不需要修改的文件

- `corelib/agent/loop.go`：复用 RunLoop，零修改
- 前端：`/btw` 的输出通过 `onToken` 流式推送，前端已有的 streaming 机制自动处理
- 工具定义/注册：BtwSubAgent 内部构建工具定义，不影响主 agent 的工具注册

## 详细设计

### 1. `/btw` 命令拦截（`gui/im_message_handler.go`）

在 `handleIMMessageWithLoop` 的 slash command 处理区域（`/help` 之后），新增：

```go
if strings.HasPrefix(trimmed, "/btw ") {
    btwQuery := strings.TrimSpace(trimmed[5:])
    if btwQuery == "" {
        return &IMAgentResponse{Text: "用法: /btw <查询内容>\n示例: /btw 最新的 Go 1.23 有什么新特性"}
    }
    return h.handleBtwCommand(msg, btwQuery, httpClient, onProgress, onToken)
}
```

### 2. `handleBtwCommand`（`gui/im_message_handler.go`）

```go
func (h *IMMessageHandler) handleBtwCommand(msg IMUserMessage, query string, httpClient *http.Client, onProgress tool.ProgressCallback, onToken llm.TokenCallback) *IMAgentResponse {
    cfg := h.getMaclawLLMConfig()
    
    // 创建独立的 LoopContext（不复用主 loop 的 context）
    loopCtx := NewLoopContext("btw", 15, httpClient)
    
    btw := NewBtwSubAgent(h, cfg, httpClient, loopCtx)
    btw.SetCallbacks(onToken, onProgress)
    
    result := btw.Execute(query)
    
    // 将最终结果追加到主对话历史（单条消息，不含中间步骤）
    history := h.memory.Load(msg.UserID)
    history = append(history,
        agent.ConversationEntry{Role: "user", Content: "/btw " + query},
        agent.ConversationEntry{Role: "assistant", Content: result.Text},
    )
    h.saveConversationHistoryTimed(msg.UserID, history, nil)
    
    return &IMAgentResponse{Text: result.Text}
}
```

### 3. BtwSubAgent（`gui/btw_subagent.go`）

核心结构体和 Execute 方法：

```go
type BtwSubAgent struct {
    handler    *IMMessageHandler
    cfg        corelib.MaclawLLMConfig
    httpClient *http.Client
    loopCtx    *LoopContext
    onToken    func(string)
    onProgress func(string)
}

func (b *BtwSubAgent) Execute(query string) *BtwResult {
    cb := &btwCallbacks{subagent: b}
    result := agent.RunLoop(cb, query, nil, b.httpClient)
    // ...
}
```

### 4. btwCallbacks（`gui/btw_subagent.go`）

实现 `agent.LoopCallbacks`：

- `GetLLMConfig()` → 复用主 handler 的 LLM 配置
- `GetMaxIterations()` → 15（信息查询不需要太多轮）
- `BuildSystemPrompt()` → 精简的查询助手 prompt
- `BuildTools()` → 4 个工具定义
- `ExecuteTool()` → 委托给 handler 的现有实现
- `OnToken()` → 转发到 `b.onToken`
- `ShouldStop()` → 检查 `loopCtx.IsCancelled()`

### 5. System Prompt（~1K token）

```
你是一个信息查询助手。用户通过 /btw 命令发起了一个侧查询，请高效地回答。

规则：
1. 优先使用 web_search 搜索最新信息
2. 找到相关结果后用 web_fetch 获取详细内容
3. 如果问题涉及本地项目，使用 read_file 查看相关文件
4. 如果问题涉及之前的对话记忆，使用 memory(action=recall) 召回
5. 回答要简洁、结构化，直接给出关键信息
6. 不要做编码、文件修改等操作——这是一个只读查询
7. 引用来源时附上 URL
```

### 6. 工具定义（4 个）

```go
var btwToolNames = map[string]bool{
    "web_search": true,
    "web_fetch":  true,
    "read_file":  true,
    "memory":     true,
}
```

工具定义从主 handler 的 `tool_registry_builtin.go` 中已注册的定义复制 schema，或直接内联构建。

### 7. /help 更新

`/help` 命令的输出新增 `/btw` 说明。

## Context 效率

| 指标 | 主 Agent | BtwSubAgent |
|------|---------|-------------|
| System prompt | ~12,000 token | ~1,000 token |
| 工具定义 | ~15,000 token (40+) | ~500 token (4) |
| 初始开销 | ~40,000 token | ~1,500 token |
| 可用查询空间 | ~62,000 token | ~100,000 token |

## 验收标准

- `/btw 最新的 Go 1.23 有什么新特性` → 搜索网页 → 返回结构化结果
- `/btw` （无参数）→ 显示用法提示
- `/btw` 查询期间用户可以取消
- `/btw` 的中间工具调用不出现在主对话历史中
- `/btw` 的最终结果作为单条消息追加到主对话历史
- 流式输出正常工作（用户实时看到查询进度）
- 主对话上下文不受 `/btw` 影响
