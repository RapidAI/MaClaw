# MacLaw Steering 机制设计：声明式规则注入

## 1. 问题分析

### 1.1 现状

MacLaw 的所有行为规则硬编码在 Go 源码中：

| 规则类别 | 当前位置 | 行数（估计） | 修改成本 |
|---------|---------|------------|---------|
| 编码工作流（9 步流程） | `gui/im_system_prompt.go` | ~400 行 | 改 Go → 编译 → 发版 |
| 反循环/反漂移规则 | `gui/im_system_prompt.go` | ~50 行 | 同上 |
| 编码规范（Mermaid/PDF/编码） | `gui/im_system_prompt.go` | ~80 行 | 同上 |
| SSH 操作规则 | `gui/im_system_prompt.go` | ~30 行 | 同上 |
| 条件工具关键词 | `corelib/tool/router.go` | ~200 行 | 同上 |
| 编码意图分类关键词 | `gui/coding_tool_gate.go` | ~100 行 | 同上 |
| 工作流模板关键词 | `corelib/workflow/templates.go` | ~50 行/模板 | 同上 |

问题：

1. **修改成本高**：用户想加一条"所有 Python 代码用 black 格式化"的规则，需要等开发者改代码发版
2. **无法个性化**：不同用户/团队有不同的编码规范、工作流偏好，硬编码无法适配
3. **无法项目级定制**：Go 项目和 Python 项目的编码规范不同，但 system prompt 是全局的
4. **规则分散**：同一个概念（如"编码工作流"）的规则散布在 `im_system_prompt.go`、`coding_tool_gate.go`、`SteeringWorkflowDetector` 中，维护困难
5. **context 浪费**：所有规则始终注入 system prompt，即使大部分对话不涉及编码

### 1.2 Kiro 的 Steering 机制

Kiro 在 `.kiro/steering/` 目录下放置 Markdown 文件，通过 front-matter 声明注入策略：

```yaml
---
inclusion: always          # 每次对话都注入
---
# 编码工作流规则
...
```

```yaml
---
inclusion: fileMatch
fileMatchPattern: '*.go'
---
# Go 编码规范
...
```

```yaml
---
inclusion: manual
---
# 特殊项目约定
...
```

核心优势：
- 用户自己写 Markdown 就能定制 AI 行为，零代码
- 项目级规则跟代码一起版本管理
- `fileMatch` 模式按需注入，不浪费 context
- `manual` 模式用户主动引用，完全可控

---

## 2. 设计

### 2.1 设计原则

1. **机制通用，不做 workaround**：设计一个通用的规则加载/注入框架，而非为每种规则类型写特殊逻辑
2. **渐进式迁移**：硬编码规则作为内置默认值保留，steering 文件可覆盖/追加，不破坏现有行为
3. **两级作用域**：用户级（`~/.maclaw/steering/`）+ 项目级（`.maclaw/steering/`），项目级优先
4. **最小 context 占用**：只注入当前对话需要的规则，不是全量灌入
5. **热加载**：文件修改后下次对话立即生效，不需要重启

### 2.2 文件结构

```
~/.maclaw/steering/           # 用户级（全局）
├── coding-workflow.md        # 编码工作流规则
├── encoding-guidance.md      # 编码规范
└── my-team-rules.md          # 团队自定义规则

<project>/.maclaw/steering/   # 项目级（覆盖用户级同名文件）
├── go-conventions.md         # Go 项目专用规范
└── api-design.md             # API 设计约定
```

### 2.3 文件格式

每个 steering 文件是标准 Markdown，可选 YAML front-matter：

```yaml
---
# 注入策略（默认 always）
inclusion: always | fileMatch | manual | contextMatch

# fileMatch 模式：当对话上下文中出现匹配文件时注入
fileMatchPattern: '*.go'

# contextMatch 模式：当用户消息匹配关键词时注入
contextKeywords: ['ssh', '服务器', '远程']

# 优先级（数字越小越靠前，默认 100）
priority: 50

# 是否可被项目级同名文件覆盖（默认 true）
overridable: true

# 文件引用（注入时自动展开引用文件的内容）
# references:
#   - path: docs/api-spec.yaml
#     maxLines: 200
---

# 规则标题

规则内容...
```

### 2.4 注入策略

| 策略 | 触发条件 | 典型用途 | context 成本 |
|------|---------|---------|-------------|
| `always` | 每次对话 | 核心工作流、安全规则 | 固定 |
| `fileMatch` | 对话中读取/编辑了匹配文件 | 语言/框架专用规范 | 按需 |
| `contextMatch` | 用户消息匹配关键词 | SSH 操作规则、浏览器规则 | 按需 |
| `manual` | 用户在 IM 中 `#规则名` 引用 | 特殊项目约定 | 按需 |

`contextMatch` 是 MacLaw 特有的扩展——Kiro 没有这个模式。它对应 MacLaw 现有的 `conditionalKeepRules` 关键词匹配机制，但从"激活工具"扩展到"注入规则"。

### 2.5 核心数据结构

```go
package steering

// SteeringFile represents a loaded steering rule file.
type SteeringFile struct {
    Name            string            // 文件名（不含路径），用于同名覆盖
    Scope           Scope             // User / Project
    Inclusion       InclusionMode     // always / fileMatch / contextMatch / manual
    FileMatchPattern string           // glob pattern for fileMatch mode
    ContextKeywords []string          // keywords for contextMatch mode
    Priority        int               // injection order (lower = earlier)
    Overridable     bool              // can be overridden by project-level file
    Content         string            // Markdown body (without front-matter)
    References      []FileReference   // referenced files to expand
    ModTime         time.Time         // for hot-reload detection
}

type Scope string
const (
    ScopeUser    Scope = "user"
    ScopeProject Scope = "project"
)

type InclusionMode string
const (
    InclusionAlways       InclusionMode = "always"
    InclusionFileMatch    InclusionMode = "fileMatch"
    InclusionContextMatch InclusionMode = "contextMatch"
    InclusionManual       InclusionMode = "manual"
)

type FileReference struct {
    Path     string
    MaxLines int // 0 = unlimited
}
```

### 2.6 Store 接口

```go
// Store loads, caches, and queries steering files.
type Store struct {
    userDir    string              // ~/.maclaw/steering/
    projectDir string              // <project>/.maclaw/steering/
    files      []SteeringFile      // merged & sorted by priority
    mu         sync.RWMutex
    lastScan   time.Time
}

// Load scans both directories, parses front-matter, merges by name
// (project overrides user), sorts by priority.
func (s *Store) Load() error

// GetAlways returns all always-inclusion files, sorted by priority.
func (s *Store) GetAlways() []SteeringFile

// GetFileMatched returns files whose fileMatchPattern matches any of
// the given file paths (from tool calls like read_file, edit_file).
func (s *Store) GetFileMatched(contextFiles []string) []SteeringFile

// GetContextMatched returns files whose contextKeywords match the
// user message text. Uses the same substring matching as
// conditionalKeepRules for consistency.
func (s *Store) GetContextMatched(userMessage string) []SteeringFile

// GetManual returns the file with the given name, or nil.
func (s *Store) GetManual(name string) *SteeringFile

// Resolve returns all files that should be injected for the current
// context. This is the main entry point for system prompt construction.
// It deduplicates, sorts by priority, and respects token budget.
func (s *Store) Resolve(ctx ResolveContext) []SteeringFile

type ResolveContext struct {
    UserMessage            string
    ContextFiles           []string   // files read/edited in current conversation
    ManualRefs             []string   // user-referenced steering names
    EffectiveContextTokens int        // from cfg.EffectiveContextTokens(), for dynamic budget scaling
}
```

### 2.7 System Prompt 注入点

在 `buildSystemPromptBase()` 中，steering 内容注入在核心身份之后、记忆之前：

```go
func (h *IMMessageHandler) buildSystemPromptBase(...) string {
    var b strings.Builder

    // 1. 核心身份（硬编码，不可覆盖）
    b.WriteString(coreIdentityPrompt(roleName, roleDesc))

    // 2. 输出格式约束（硬编码，不可覆盖）
    b.WriteString(outputFormatRules())

    // 3. Steering 规则注入（新增）
    resolved := h.steeringStore.Resolve(steering.ResolveContext{
        UserMessage:            userMessage,
        ContextFiles:           h.getContextFiles(userID),
        ManualRefs:             h.getManualSteeringRefs(userID),
        EffectiveContextTokens: cfg.EffectiveContextTokens(),
    })
    for _, sf := range resolved {
        b.WriteString("\n\n")
        b.WriteString(sf.Content)
    }

    // 4. 设备状态（硬编码）
    b.WriteString(deviceStatusSection())

    // 5. 记忆（动态）
    h.appendMemorySection(&b, ...)

    // 6. 知识技能（动态）
    h.appendKnowledgeSkillSection(&b, ...)

    return b.String()
}
```

### 2.8 与现有机制的关系

#### 替代关系（渐进迁移）

| 现有机制 | Steering 替代方案 | 迁移策略 |
|---------|-----------------|---------|
| `im_system_prompt.go` 中的编码工作流规则 | `~/.maclaw/steering/coding-workflow.md` (always) | 内置默认文件，用户可覆盖 |
| `im_system_prompt.go` 中的编码规范 | `~/.maclaw/steering/encoding-guidance.md` (always) | 同上 |
| `im_system_prompt.go` 中的 SSH 规则 | `~/.maclaw/steering/ssh-operations.md` (contextMatch: ssh/服务器) | 按需注入，节省 context |
| `desktopWorkflowDocOverride()` | `~/.maclaw/steering/desktop-doc-rules.md` (always, 仅桌面) | 需要扩展 inclusion 支持 platform 条件 |

#### 互补关系（不替代）

| 现有机制 | 为什么不替代 |
|---------|------------|
| `conditionalKeepRules`（工具激活） | Steering 注入规则文本，不控制工具列表。工具激活仍由 router 负责 |
| `CodingToolGate`（工具拦截） | Gate 是代码级别的硬逻辑（拦截 tool call），不是 prompt 级别的软约束 |
| `WorkflowEngine`（阶段流转） | 引擎是状态机，steering 是无状态的文本注入 |
| `IntentClassifier`（意图分类） | 分类器是算法，不是规则文本 |

#### 协作关系

`contextMatch` 模式可以与 `conditionalKeepRules` 联动：当 router 激活了 SSH 工具时，steering store 同时注入 SSH 操作规则。两者共享关键词列表，但职责不同——router 管工具，steering 管规则。

```go
// router.go 中的 conditionalKeepRules 激活 ssh 工具
// steering 中的 contextMatch 注入 ssh 操作规则
// 两者由相同的关键词触发，但独立执行
```

### 2.9 内置默认文件

首次安装时，MacLaw 在 `~/.maclaw/steering/` 下生成内置默认文件。这些文件的内容从当前硬编码的 system prompt 中提取，用户可以自由修改：

```go
// corelib/steering/defaults.go
//go:embed defaults/coding-workflow.md
var defaultCodingWorkflow string

//go:embed defaults/encoding-guidance.md
var defaultEncodingGuidance string

// EnsureDefaults creates default steering files if they don't exist.
// Never overwrites user-modified files.
func EnsureDefaults(userDir string) error {
    defaults := map[string]string{
        "coding-workflow.md":   defaultCodingWorkflow,
        "encoding-guidance.md": defaultEncodingGuidance,
    }
    for name, content := range defaults {
        path := filepath.Join(userDir, name)
        if _, err := os.Stat(path); err == nil {
            continue // user file exists, don't overwrite
        }
        if err := os.WriteFile(path, []byte(content), 0644); err != nil {
            return err
        }
    }
    return nil
}
```

### 2.10 Token 预算管理

#### Context 预算推算

MacLaw 的 context 窗口分配（以默认 128K token 为例）：

```
总 context window:           128,000 token
├── 输出预留 (20%):           -25,600 token
├── 有效输入预算:             102,400 token
│   ├── System Prompt 固定部分:  ~3,000 token  (身份/核心原则/设备状态)
│   ├── 编码工作流规则:          ~2,500 token  (Pro 模式 9 步流程)
│   ├── 工具定义:               ~3,000-5,000 token  (15-40 个工具)
│   ├── 记忆召回:               ~800-1,500 token  (最多 12 条)
│   ├── 知识技能:               ~2,000 token  (defaultKnowledgeSkillTokenBudget)
│   ├── 工作流阶段 prompt:       ~500-1,000 token  (活跃工作流时)
│   ├── Steering 预算:        ~3,000 token  ← 新增
│   └── 对话历史:               ~85,000-90,000 token  (剩余全部)
```

对话历史是 LLM 的核心工作记忆，必须保证充足。Steering 预算不能挤占太多。

#### 限制设计

| 限制项 | 值 | 理由 |
|-------|-----|------|
| **总 steering 预算** | 3,000 token（~9,000 rune） | 占有效输入的 ~3%，与知识技能预算持平 |
| **单文件上限** | 1,500 token（~4,500 rune） | 防止一个文件吃掉全部预算 |
| **always 文件数上限** | 5 个 | 防止用户放太多 always 文件 |
| **总文件数上限** | 20 个 | 包含所有 inclusion 模式 |
| **单文件大小硬限制** | 15 KB | 文件系统层面拒绝加载超大文件 |

Token 估算公式（与 `im_system_prompt.go` 的 `estimateTokens` 一致）：
- 1 token ≈ 3 rune（中英文混合场景的中间值）
- 3,000 token ≈ 9,000 rune ≈ 约 4,500 个中文字

#### 预算分配策略

```go
const (
    // MaxSteeringTokenBudget is the total token budget for all steering
    // files injected into the system prompt. This is ~3% of the default
    // 128K context window's effective input budget (102,400 tokens).
    MaxSteeringTokenBudget = 3000

    // MaxSingleFileTokens caps any individual steering file to prevent
    // one file from consuming the entire budget.
    MaxSingleFileTokens = 1500

    // MaxAlwaysFiles limits the number of always-inclusion files.
    MaxAlwaysFiles = 5

    // MaxTotalFiles limits the total number of steering files loaded.
    MaxTotalFiles = 20

    // MaxFileBytes is the hard filesystem-level size limit per file.
    MaxFileBytes = 15 * 1024
)
```

#### 注入顺序与截断

```go
func (s *Store) Resolve(ctx ResolveContext) []SteeringFile {
    candidates := s.collectCandidates(ctx) // always + matched + manual
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Priority < candidates[j].Priority
    })

    budget := MaxSteeringTokenBudget
    // 小 context 模型（如 32K）按比例缩减
    if ctx.EffectiveContextTokens > 0 && ctx.EffectiveContextTokens < 80000 {
        budget = ctx.EffectiveContextTokens * 3 / 100 // 3% of effective context
        if budget < 500 {
            budget = 500 // absolute minimum
        }
    }

    var result []SteeringFile
    usedTokens := 0
    for _, sf := range candidates {
        tokens := estimateTokens(sf.Content)
        // 单文件超限：截断到 MaxSingleFileTokens
        if tokens > MaxSingleFileTokens {
            sf.Content = truncateToTokenBudget(sf.Content, MaxSingleFileTokens)
            tokens = MaxSingleFileTokens
        }
        // 总预算超限：停止注入
        if usedTokens+tokens > budget {
            break
        }
        result = append(result, sf)
        usedTokens += tokens
    }
    return result
}
```

#### 用户反馈

当文件被截断或跳过时，在日志中记录警告：

```
[steering] file "my-rules.md" truncated from 2100 to 1500 tokens (single file limit)
[steering] file "extra-rules.md" skipped: budget exhausted (2800/3000 tokens used)
[steering] WARNING: 3 always-inclusion files loaded, approaching limit of 5
```

GUI 设置面板中显示 steering 文件列表和各自的 token 占用，帮助用户管理预算。

### 2.11 热加载

不使用 fsnotify（增加依赖和复杂度）。采用惰性检查：每次 `Resolve()` 调用时，如果距离上次扫描超过 30 秒，重新扫描目录。对话频率远低于 30 秒，实际效果等同于实时加载。

```go
func (s *Store) Resolve(ctx ResolveContext) []SteeringFile {
    s.mu.RLock()
    stale := time.Since(s.lastScan) > 30*time.Second
    s.mu.RUnlock()

    if stale {
        s.Load() // re-scan directories
    }
    // ... resolve logic
}
```

### 2.12 IM 通道 `#` 引用（manual 模式）

用户在飞书/微信/QQ 中发送 `#ssh规则` 时，系统从 steering store 中查找名为 `ssh规则.md` 的文件并注入。这与 Kiro 的 `#` 引用机制一致。

解析逻辑放在 `IMMessageHandler.preprocessMessage()` 中：

```go
func (h *IMMessageHandler) extractSteeringRefs(text string) (cleanText string, refs []string) {
    // Match #name patterns (Chinese/English/digits/hyphens)
    re := regexp.MustCompile(`#([\p{Han}\w-]+)`)
    refs = re.FindAllString(text, -1)
    cleanText = re.ReplaceAllString(text, "")
    return
}
```

---

## 3. 实现计划

### Phase 1: 核心框架 
- `corelib/steering/types.go`：`File`、`Scope`、`InclusionMode`、`ResolveContext`
- `corelib/steering/budget.go`：Token 预算常量 + 动态缩放 + token 估算
- `corelib/steering/parser.go`：YAML front-matter 解析 + Markdown body 提取
- `corelib/steering/store.go`：Store（Load/Resolve/合并/截断/30 秒热加载）
- `corelib/steering/store_test.go`：13 个单元测试

### Phase 2: 内置默认文件 + System Prompt 注入 
- `corelib/steering/defaults.go`：`EnsureDefaults()` + 3 个内置默认文件
- `gui/app.go`：`steeringStore` 字段 + 启动初始化
- `gui/app_steering_init.go`：`initSteeringStore()`
- `gui/im_system_prompt_steering.go`：`appendSteeringSection()` + `extractSteeringRefs()`
- `gui/im_system_prompt.go`：注入点（核心原则之后、记忆之前）
- `tui/agent_handler.go`：TUI 侧 system prompt 注入
- `tui/app.go`：`steeringStore` 字段 + `WithSteeringStore` option
- `tui/app_workflow_init.go`：`initTUISteeringStore()`

### Phase 3: contextMatch + 文件追踪 
- `corelib/steering/defaults.go`：新增 `ssh-operations.md`（contextMatch 模式）
- `gui/im_system_prompt_steering.go`：`trackSteeringFile()` / `trackSteeringFileFromArgs()` / `resetSteeringContextFiles()`
- `gui/im_tool_execution.go`：`executeTool()` 中 hook 文件追踪
- `gui/im_message_handler.go`：`steeringContextFiles sync.Map` + `/new` 重置

### Phase 4: 集成测试 
- `corelib/steering/integration_test.go`：6 个端到端测试覆盖全部四种模式

---

## 4. 与 Kiro Steering 的对比

| 维度 | Kiro | MacLaw（本设计） |
|------|------|----------------|
| 作用域 | 工作区级 | 用户级 + 项目级（两级合并） |
| 注入策略 | always / fileMatch / manual | always / fileMatch / contextMatch / manual |
| contextMatch | 无 | 有（对应 conditionalKeepRules 的关键词匹配） |
| Token 预算 | 无显式控制 | 有（Resolve 时按优先级截断） |
| 热加载 | 文件变更即时生效 | 惰性检查（30 秒间隔） |
| 文件引用 | `#[[file:path]]` 语法 | `references` front-matter 字段 |
| 内置默认 | 无（用户自建） | 有（首次安装生成，可覆盖） |
| 平台条件 | 无 | 可扩展（desktop/im 条件注入） |

MacLaw 的 `contextMatch` 是关键差异——Kiro 是 IDE，文件上下文天然可用；MacLaw 是 IM agent，用户消息的关键词是主要上下文信号。

---

## 5. 预期收益

| 指标 | 当前 | 升级后 |
|------|------|--------|
| 用户自定义规则 | 不可能 | Markdown 文件即可 |
| 规则修改周期 | 改代码→编译→发版 | 改文件→下次对话生效 |
| 项目级定制 | 不可能 | `.maclaw/steering/` 跟代码版本管理 |
| System prompt 大小 | ~8,000-10,000 token（全量注入） | ~5,000-7,000 token（按需注入，steering 上限 3,000） |
| 团队规则共享 | 口头约定 | 项目级 steering 文件 |
| SSH 规则 context 占用 | 始终注入（~200 token） | 仅 SSH 对话注入（contextMatch） |

---

## 6. 风险与缓解

1. **用户写错 front-matter**：解析失败时 fallback 到 `inclusion: always`，记录 warning 日志
2. **steering 文件过大**：单文件 1,500 token 硬限制 + 总预算 3,000 token + 文件系统 15KB 硬限制，三层防护
3. **项目级文件安全**：恶意项目可能在 `.maclaw/steering/` 中放置注入攻击。缓解：steering 内容作为 system prompt 的一部分，受 LLM 的 system prompt 保护；不执行代码，只是文本注入
4. **迁移期间双重注入**：Phase 2 中硬编码规则和 steering 文件可能同时存在。缓解：硬编码规则检查 steering store 是否有同名文件，有则跳过
5. **小 context 模型预算不足**：32K 模型的有效输入仅 ~25,600 token，3% = 768 token。缓解：`Resolve()` 按比例缩减预算，最低 500 token（约 1,500 rune / 750 中文字），足够放一个精简的核心规则文件
