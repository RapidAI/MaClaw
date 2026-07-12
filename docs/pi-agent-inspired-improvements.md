# Pi Agent 借鉴分析报告

## 项目信息

- **源项目**: [earendil-works/pi](https://github.com/earendil-works/pi) — AI agent toolkit: unified LLM API, agent loop, TUI, coding agent CLI
- **作者**: Mario Zechner (badlogic) + Armin Ronacher (mitsuhiko)
- **架构**: TypeScript monorepo（pi-ai / pi-agent-core / pi-tui / pi-coding-agent）
- **分析日期**: 2026-07-09

---

## 已实施的对齐改进

### OAuth CredentialStore 独立化（#119）

**Pi 的做法**: 独立 `auth.json` + `CredentialStore` 接口 + `modify()` 串行化

**MaClaw 实施**:
- `corelib/oauth/credential_store.go`: CredentialStore 接口 + FileCredentialStore（独立 mutex + 原子写入）
- `gui/app_oauth_credential_store.go`: GUI 集成层
- `gui/app_oauth_providers.go`: Anthropic + GitHub Copilot Wails bindings
- 双写 config.json 向后兼容 TUI

### 新增 Anthropic + GitHub Copilot OAuth

**Pi 的做法**: 6 个 OAuth provider（OpenAI Codex、Anthropic、GitHub Copilot、Gemini CLI、Antigravity、Vertex AI）

**MaClaw 实施**:
- `corelib/oauth/anthropic.go`: Claude Pro/Max PKCE flow（远程回调 + code 粘贴）
- `corelib/oauth/github_copilot.go`: Device Code flow（RFC 8628）
- 前端 LLMConfigPanel 多 provider OAuth 分发

---

## MaClaw 已有但实现方式不同的能力

以下能力 Pi 有，MaClaw 也有但实现方式不同。**不需要对齐**——MaClaw 的方式更适合 GUI + IM 多端场景。

### 1. Token/Cost Tracking

| 维度 | Pi | MaClaw |
|------|-----|--------|
| 实现 | `AssistantMessage.usage.cost` 内建 | `CostTracker`（corelib/llm）+ `recordLLMCost` |
| 价格表 | Provider metadata 静态声明 | `DefaultPriceTable` + prefix 匹配 |
| 前端展示 | TUI footer 实时显示 | TokenUsagePanel（LLM 设置面板内） |
| Budget | 无 | `isOverDailyBudget()` 每日预算限制 |

**MaClaw 优势**: 每日预算限制（Pi 没有）。缺少: 聊天面板中实时 cost 展示（仅 LLM 设置面板有）。

### 2. Steering/Follow-up 双队列

| 维度 | Pi | MaClaw |
|------|-----|--------|
| Steering | Enter 提交，工具批次完成后注入 | `InjectSupplementary` → `accumulateInjection` |
| Follow-up | Alt+Enter 提交，agent_end 后注入 | 无显式区分（都走 injection） |
| 消费时机 | turn_end 后检查 steering → follow-up | iteration 开头 `consumeInjection` |

**MaClaw 的单队列已够用**——GUI 面板的交互模型（Buffer Queue Fire）不需要 steering/follow-up 的语义区分。

### 3. Cross-Provider Handoff

| 维度 | Pi | MaClaw |
|------|-----|--------|
| Thinking block 转换 | `<thinking>` 标签文本 | 按目标 provider 移除 `reasoning_content` |
| Tool calls | 原样保留 | `normalizeOpenAIChatToolCallLinkage` 修复 |
| 消息格式 | typed `AgentMessage` + `convertToLlm()` | `[]interface{}` + `sanitize*Messages` 系列函数 |

**MaClaw 已完整覆盖**——`sanitizeOpenAIChatMessagesForSDKCompatibility` 按 `preserveReasoningContent` 参数决定保留或移除。

### 4. Compaction 可扩展性

| 维度 | Pi | MaClaw |
|------|-----|--------|
| 自定义 | Extension 可替换 compaction 逻辑 | `trimHistoryWithSummary` 接受 `summarizer`/`memorySink` 回调 |
| 完整历史保留 | JSONL 文件 | `memory.Store` 长期记忆 + task_artifact |
| 手动触发 | `/compact [instructions]` | 自动触发（基于 token/entry 阈值） |

---

## 可借鉴但优先级较低的能力

### 5. Session 树形分支（P2）

**Pi**: JSONL 文件中每条记录有 `id`/`parentId`，形成原地分支树。`/tree` 导航，`/fork` 分叉。

**MaClaw 缺失**: 线性对话历史，无分支。用户无法"回到设计阶段重新来"。

**实施建议**: 
- `ConversationEntry` 新增 `ID`/`ParentID` 字段
- `memory.Save` 改为追加写入（不覆盖旧 entries）
- 新增 `BranchAt(entryID)` 方法切换活跃分支
- 前端新增"对话树"视图（类似 git log --graph）

**风险**: 改动面大（对话历史的核心数据模型变化），需要迁移。

### 6. RPC Mode（P2）

**Pi**: `pi --mode rpc` 通过 stdin/stdout JSONL 通信。

**MaClaw 缺失**: 外部应用集成只能通过 IM 网关或 Wails binding。

**实施建议**:
- 新增 `maclaw --mode rpc` 命令，基于 `corelib/agent.RunLoop` 但走 stdin/stdout
- 协议: LF-delimited JSONL（与 Pi 一致）
- IDE 插件可通过 RPC 集成
- 实现位置: `tui/rpc_mode.go`（已有 `tui/pipe_mode.go` 作为参考）

### 7. Extension 一等公民（P3）

**Pi**: TypeScript 模块可替换内建工具、注册命令、hook 生命周期事件。

**MaClaw 的差距**: Skill 是声明式步骤序列，不能替换内建工具。Steering 是规则注入，不是代码逻辑。

**实施建议（长期）**:
- WASM 或 Go plugin 形式的 Extension 接口
- Hook 点: `before_tool_call`, `after_tool_call`, `on_message_end`, `on_session_start`
- Extension 可注册自定义工具（替换或增强内建工具）
- 与 Skill 共存: Skill（轻量 Markdown 步骤）+ Extension（重量 Go/WASM 代码逻辑）

### 8. Project Trust（P3）

**Pi**: 首次进入含 `.pi/settings.json` 的项目时询问信任。

**MaClaw 缺失**: `.maclaw/steering/` 文件无信任检查——任何仓库的 steering 文件直接加载。

**实施建议**:
- 首次扫描到项目级 `.maclaw/steering/` 时弹确认框
- 持久化到 `~/.maclaw/trust.json`
- 非交互模式（IM 通道）默认不加载未信任项目的 steering

### 9. 供应链安全加固（P3）

**Pi**: shrinkwrap, min-release-age, lifecycle script 审核。

**MaClaw**: Skill 系统从 Hub/GitHub 下载执行代码。

**实施建议**:
- `skill.lock` 文件锁定 Skill 版本和 content hash
- Hub Skill 发布签名验证（#117 Skill ID 已设计 publisher 绑定）
- 更新时 hash 变化超阈值要求用户确认

### 10. 容器化替代权限弹窗（P4）

**Pi**: 不做权限弹窗，提供 Docker/micro-VM/sandbox 方案。

**MaClaw**: PolicyEngine + SecurityFirewall（#80 已有 developer mode）。

**实施建议（远期）**:
- 提供 `maclaw --container` 模式——agent 的 bash/write_file 在 Docker 容器内执行
- 与 developer mode 互补: developer mode 关闭安全检查，container mode 提供物理隔离

---

## 架构层面的根本差异

| 维度 | Pi | MaClaw | 评价 |
|------|-----|--------|------|
| 语言 | TypeScript (Node.js) | Go + TypeScript (Wails) | Go 编译型更适合桌面应用 |
| 定位 | 开发者终端工具 | 普通用户桌面 + 企业 IM | 用户群不同 |
| 核心工具 | 4 个（read/write/edit/bash） | 40+ 内建工具 | MaClaw 面向非开发者 |
| 扩展模型 | Extension 可替换一切 | Skill + Steering + Tool registration | 各有优劣 |
| Session | JSONL + 树形分支 | ConversationMemory + 长期记忆 | MaClaw 更重记忆 |
| 权限 | 无——容器化 | PolicyEngine + SecurityFirewall | MaClaw 面向企业 |
| 多用户 | 不支持 | Hub + maclawsrv 多租户 | MaClaw 更复杂 |

**结论**: Pi 是极简哲学的开发者工具，MaClaw 是面向普通用户的全功能产品。大部分 Pi 的"缺失"在 MaClaw 中是有意的设计选择（如 40+ 工具 vs 4 工具）。真正值得借鉴的是：
1. CredentialStore 独立化（已实施）
2. 多 Provider OAuth（已实施）
3. Session 树形分支（中期，体验提升大）
4. RPC Mode（中期，生态建设）
5. Extension 一等公民（长期，生态建设）
