# CodingSubAgent 编程经验知识库设计文档

## 1. 概述

### 1.1 问题

CodingSubAgent 执行编码任务时积累的经验（哪些方法有效、哪些技术选型正确、哪些陷阱需要避免）在任务完成后随 conversation history 丢弃。下次遇到相似任务时从零开始，重复犯相同错误。

### 1.2 目标

- 通过独立的编程知识库实现经验积累和复用
- SubAgent 任务完成后自动沉淀经验，任务开始前自动召回相关经验
- 提供完整的管理界面（查看/编辑/删除），防止污染后无法恢复
- 同时查询通用知识库获取项目文档/API 规范等上下文

### 1.3 设计原则

- **独立存储**：编程知识库使用独立 SQLite 文件 (`coding_knowledge.db`)，不污染通用知识库
- **可管控**：完整的 CRUD + 重置能力，出问题可快速恢复
- **渐进式信任**：置信度机制自动淘汰错误经验，无需人工干预
- **双库协作**：编程经验库解决"怎么做"，通用知识库解决"做什么/约束是什么"

## 2. 架构

### 2.1 系统位置

```
┌─────────────────────────────────────────────────────────────────┐
│ GUI 设置 → 编程工具面板                                          │
│  ├─ SubAgent 并发数（从 LLM 配置移入）                            │
│  ├─ 外部编程工具配置（已有）                                      │
│  └─ 编程知识库管理（新增）                                        │
│       ├─ 列表/筛选/搜索                                          │
│       ├─ 查看详情/编辑                                            │
│       ├─ 单条删除/批量删除/按标签清空                              │
│       ├─ 一键重置                                                 │
│       ├─ 经验沉淀开关 + 策略配置                                   │
│       └─ 容量/统计信息                                            │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│ SubAgent 运行时                                                  │
│                                                                  │
│  任务开始前:                                                      │
│    ├─ coding_knowledge.db → ContextPack → system prompt 注入     │
│    └─ knowledge.db        → ContextPack → system prompt 注入     │
│                                                                  │
│  任务执行中:                                                      │
│    ├─ coding_knowledge_search(query) → 查编程经验（只读）          │
│    └─ knowledge_search(query)        → 查项目文档（只读）          │
│                                                                  │
│  任务完成后:                                                      │
│    └─ 经验提取 → coding_knowledge.db 写入                         │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 与现有组件的关系

| 组件 | 关系 |
|------|------|
| `corelib/knowledge/` | 复用 SQLiteStore 的存储/搜索/ContextPack 基础设施 |
| `gui/coding_subagent.go` | 调用方——构建 system prompt 时注入双库知识 |
| `gui/coding_subagent_orchestrator.go` | 任务完成后触发经验沉淀 |
| `gui/app_knowledge.go` | 参照模式——新增编程知识库的 Wails bindings |
| 前端编程工具面板 | 新增知识库管理 UI |

## 3. 数据模型

### 3.1 存储文件

```
~/.maclaw/data/coding_knowledge.db    (独立 SQLite 文件)
```

### 3.2 核心表结构

复用 `corelib/knowledge.SQLiteStore` 的完整表结构（sources / document_nodes / cards / facts / fts_index），但数据完全隔离。

### 3.3 经验条目字段设计

每条经验作为一个 Source（kind=`text`）+ 关联的 Cards/Facts 存储：

```go
// CodingExperience 是面向上层的逻辑视图，底层映射到 Source + Card
type CodingExperience struct {
    ID               string    // Source.ID
    Title            string    // Source.Title
    Category         string    // "pattern" | "decision" | "pitfall" | "convention"
    Scope            string    // "universal" | "language" | "project"
    Language         string    // Scope=language 时必填
    Frameworks       []string  // 可选的框架限定
    TriggerCondition string    // 什么时候该想起这条经验（简短关键词组合）
    Content          string    // 完整经验内容（问题 + 解决 + 适用条件）
    CodeSnippet      string    // 可选的代码模板
    FailedAttempts   []string  // 失败尝试记录（告诉 SubAgent 不要走弯路）
    Contraindications []string // 反面证据（不适用场景）
    
    // 元数据
    Labels           []string  // 标签：框架/经验类型（与 Scope 正交）
    ProjectPath      string    // Scope=project 时必填
    SourceTaskTitle  string    // 产生此经验的原始任务标题
    LanguageVersion  string    // 适用的语言版本（如 "go1.21"）
    ValidUntil       string    // 过期标记（如 "go1.22" 表示 1.22 后不适用）
    
    // 置信度与统计
    Confidence       float64   // 置信度分数 [0, 2.0]，初始 1.0
    RecallCount      int       // 被召回次数
    SuccessCount     int       // 召回后任务成功次数
    FailureCount     int       // 召回后任务失败次数
    
    // 生命周期
    Status           string    // "candidate" | "active" | "verified" | "deprecated"
    CreatedAt        time.Time
    UpdatedAt        time.Time
    LastRecalledAt   time.Time
}
```

### 3.4 经验分类体系

| Category | 含义 | 典型内容 |
|----------|------|---------|
| `pattern` | 解决方案模式 | "Go 并发 map 用 sync.Map"、"大文件先写临时文件再 rename" |
| `decision` | 技术选型决策 | "序列化选 protobuf 因为跨语言+性能"、"状态管理选 Zustand" |
| `pitfall` | 陷阱/反模式 | "range loop 闭包捕获是引用"、"SQLite WAL 事务中不做网络调用" |
| `convention` | 项目约定 | "corelib 不能 import gui"、"错误返回格式 {error, code}" |

### 3.5 Scope 分区（通用 vs 语言专属）

经验按 `Scope` 字段分为三层，检索时按当前任务的语言/框架过滤：

```go
const (
    ScopeUniversal = "universal"  // 通用编程经验，与语言无关
    ScopeLanguage  = "language"   // 语言专属经验
    ScopeProject   = "project"    // 项目专属经验（最窄）
)
```

| Scope | 召回条件 | 典型内容 |
|-------|---------|---------|
| `universal` | 任何任务都召回 | "大文件先写临时文件再 rename"、"修改前先读取理解现有代码"、"测试驱动开发的 red-green-refactor 循环" |
| `language` | 当前任务的语言/框架匹配时召回 | Go："interface 不能嵌套指针"；Python："GIL 下多线程不提升 CPU 密集计算性能"；CMake："Windows 用 MinGW Makefiles" |
| `project` | 当前 ProjectPath 匹配时召回 | "这个项目序列化用 protobuf"、"corelib 不能 import gui" |

**核心字段**：

```go
type CodingExperience struct {
    // ... 其他字段 ...
    
    Scope        string   // "universal" | "language" | "project"
    Language     string   // Scope=language 时必填：go/python/typescript/cpp/rust/java
    Frameworks   []string // 可选的框架限定：react/vue/cmake/docker/protobuf
    ProjectPath  string   // Scope=project 时必填
}
```

**检索时的分区过滤**：

```go
func buildSearchFilter(task *TaskItem, projectPath string) SearchFilter {
    lang := inferLanguageFromTask(task) // 从文件扩展名/任务描述推断
    
    return SearchFilter{
        // 三层同时搜索，但结果分层加权
        Scopes: []ScopeFilter{
            {Scope: "project", ProjectPath: projectPath, Weight: 2.0},
            {Scope: "language", Language: lang, Weight: 1.5},
            {Scope: "universal", Weight: 1.0},
        },
    }
}
```

做 Go 项目时：召回 project(morio) 经验 + Go 语言经验 + 通用经验。
Python 的 GIL 经验不会出现。

**管理面板按 Scope 分 Tab**：

```
[通用] [Go] [Python] [TypeScript] [C++] [...] [项目专属]
```

切换 tab 只显示对应 scope 的经验。删除/重置也可以按 scope 操作（"清空所有 Python 经验"）。

### 3.6 Labels 标签体系

Labels 与 Scope 正交，用于更细粒度的筛选：

```
# 框架/工具（细化 language scope）
react, vue, cmake, docker, protobuf, grpc, sqlite, gin, chi

# 经验类型
compile_error, runtime_error, performance, concurrency
architecture, api_design, testing, deployment
tool_usage, dependency_management, file_io, networking

# 特殊标签
verified      — 已验证（confidence > 1.5 且召回 5+ 次）
deprecated    — 已过期
manual        — 用户手动添加（非 SubAgent 自动沉淀）
```

**Scope vs Labels 的关系**：
- Scope 是硬分区（检索时直接过滤，不匹配的不召回）
- Labels 是软标签（检索时作为亲和度加权，匹配多的排在前面）

## 4. 写入路径：经验沉淀

### 4.1 触发时机

在 `SubAgentTaskRunner.RunCurrentTask()` 返回后，由 orchestrator 触发经验提取。

```go
func (r *SubAgentTaskRunner) RunCurrentTask(ctx context.Context) TaskRunResult {
    result := r.subagent.RunTask(ctx, task)
    
    // 任务完成后沉淀经验
    if r.codingKnowledgeEnabled() {
        r.extractAndSaveExperience(ctx, task, result)
    }
    
    return result
}
```

### 4.2 沉淀策略（用户可配置）

| 策略 | 说明 |
|------|------|
| `always` | 成功和失败后都沉淀 |
| `on_success` | 仅任务成功时沉淀 |
| `on_retry_success` | 仅"失败→重试→成功"时沉淀（最有价值的经验） |
| `off` | 关闭自动沉淀 |

默认：`on_retry_success`（最保守，避免初期噪音）

### 4.3 经验提取流程

```go
func (r *SubAgentTaskRunner) extractAndSaveExperience(ctx context.Context, task *TaskItem, result TaskRunResult) {
    // 1. 构建提取 prompt
    extractPrompt := buildExperienceExtractionPrompt(task, result)
    
    // 2. 用轻量 LLM 调用提取结构化经验（复用 SubAgent 的 LLM config）
    //    超时 15s，失败则跳过（经验沉淀是 best-effort）
    extracted := callLLMForExperienceExtraction(ctx, extractPrompt, r.llmConfig)
    
    // 3. 去重检查：在编程知识库中搜索相似经验
    existing := r.codingKB.Search(ctx, SearchOptions{
        Query: extracted.TriggerCondition,
        Limit: 3,
    })
    if hasSimilarExperience(existing, extracted) {
        // 更新已有经验的 confidence / 追加反面证据
        r.codingKB.UpdateExperience(ctx, existing[0].ID, mergeUpdate)
        return
    }
    
    // 4. 写入新经验（初始状态 = candidate）
    r.codingKB.SaveExperience(ctx, extracted)
}
```

### 4.4 经验提取 Prompt

```
你是一个编程经验提取器。根据以下编码任务的执行过程，提取可复用的经验。

任务标题：{task.Title}
任务描述：{task.Description}
执行结果：{成功/失败后重试成功/失败}
对话历史摘要：{conversation 的关键片段，截断到 3000 chars}

请提取 0-2 条经验（没有值得记录的就返回空数组），每条包含：
- title: 一句话标题
- category: pattern/decision/pitfall/convention
- trigger_condition: 什么情况下该想起这条经验（20 字以内的关键词组合）
- content: 完整描述（问题 + 解决方案 + 原因）
- code_snippet: 如果有固定模式的代码，给出可复制的代码模板（可选）
- failed_attempts: 如果经历了失败尝试，列出走过的弯路（可选）
- labels: 标签列表
- project_specific: 这条经验是项目专属的(true)还是通用的(false)

输出 JSON 格式。如果没有值得记录的经验，返回 {"experiences": []}。
```

### 4.5 观察模式（初始阶段）

配置项 `coding_knowledge_auto_save_mode`：
- `observe`（默认）：提取经验但标记为 `candidate` 状态，不参与自动召回，需用户在面板中确认后升级为 `active`
- `auto`：提取后直接标记为 `active`，参与自动召回
- `off`：关闭自动沉淀

用户可随时在面板中切换。积累了信任度后再打开 `auto`。

## 5. 读取路径：经验召回

### 5.1 任务开始前的 System Prompt 注入

```go
func buildCodingSubAgentSystemPrompt(task *TaskItem, projectPath string, ...) string {
    var b strings.Builder
    
    // ... 现有 prompt 内容 ...
    
    // 编程知识库召回
    if codingKB != nil {
        codingPack := codingKB.ContextPack(ctx, ContextPackOptions{
            SearchOptions: SearchOptions{
                Query:       task.Title + " " + task.Description,
                ProjectPath: projectPath,
                Labels:      inferLabelsFromTask(task), // 从任务推断语言/框架标签
                Limit:       10,
            },
            MaxItems: 4,
            MaxChars: 1500,
            // 自定义排序：confidence 加权
            ScoreModifier: func(result SearchResult) float64 {
                return result.Score * getConfidence(result)
            },
        })
        if len(codingPack.Items) > 0 {
            b.WriteString("\n## 相关编码经验\n")
            b.WriteString("以下是从历史编码经验中召回的相关知识，供参考：\n")
            for _, item := range codingPack.Items {
                b.WriteString(formatExperienceForPrompt(item))
            }
        }
    }
    
    // 通用知识库召回
    if generalKB != nil {
        generalPack := generalKB.ContextPack(ctx, ContextPackOptions{
            SearchOptions: SearchOptions{
                Query: task.Title + " " + strings.Join(task.Files, " "),
                Limit: 10,
            },
            MaxItems: 3,
            MaxChars: 2000,
        })
        if len(generalPack.Items) > 0 {
            b.WriteString("\n## 项目参考资料\n")
            b.WriteString("以下是从项目知识库中召回的相关文档：\n")
            b.WriteString(FormatContextPackForLLM(generalPack))
        }
    }
    
    return b.String()
}
```

### 5.2 检索优先级（Scope 分区 + 权重）

```
三层 Scope 同时检索，结果按以下权重排序：

Layer 1 — project scope（当前项目专属）
  条件: Scope="project" AND ProjectPath=当前项目
  权重: score × 2.5
  
Layer 2 — language scope（当前语言专属）
  条件: Scope="language" AND Language=当前语言
  权重: score × 1.8
  附加: Frameworks 标签匹配时额外 × 1.2

Layer 3 — universal scope（通用）
  条件: Scope="universal"
  权重: score × 1.0

所有结果再乘以 confidence 系数（0~2.0）。
```

语言推断逻辑：

```go
func inferLanguageFromTask(task *TaskItem) string {
    // 1. 从涉及文件的扩展名推断
    for _, f := range task.Files {
        switch filepath.Ext(f) {
        case ".go":   return "go"
        case ".py":   return "python"
        case ".ts", ".tsx": return "typescript"
        case ".js", ".jsx": return "javascript"
        case ".cpp", ".cc", ".h", ".hpp": return "cpp"
        case ".rs":   return "rust"
        case ".java": return "java"
        }
    }
    // 2. 从任务描述关键词推断
    // 3. 从项目根目录的构建文件推断（go.mod/package.json/CMakeLists.txt）
    return ""  // 空表示未知，只召回 universal + project
}
```

### 5.3 SubAgent 运行中的主动查询

SubAgent 工具列表新增两个只读工具：

```go
// 工具 1：查编程经验库
{
    Name: "coding_knowledge_search",
    Description: "搜索编程经验知识库，获取算法选型、技术方案、常见陷阱等编码经验。当你不确定某个技术决策或遇到不熟悉的问题时使用。",
    Parameters: {
        "query": "搜索关键词，描述你想了解的技术问题或决策",
    },
}

// 工具 2：查通用知识库
{
    Name: "knowledge_search",
    Description: "搜索项目知识库，获取 API 文档、数据库结构、接口规范、设计文档等项目资料。当你需要了解项目的具体约定或接口细节时使用。",
    Parameters: {
        "query": "搜索关键词，描述你需要的项目信息",
    },
}
```

## 6. 置信度机制

### 6.1 置信度计算

```go
const (
    initialConfidence     = 1.0
    maxConfidence         = 2.0
    minConfidence         = 0.0
    successBoost          = 0.15  // 召回后任务成功
    failurePenalty        = 0.25  // 召回后任务失败
    verifiedThreshold     = 1.5   // 升级为 "verified"
    deprecatedThreshold   = 0.3   // 降级为 "deprecated"
    minRecallsForVerified = 5     // 升级需要的最少召回次数
)

func updateConfidence(exp *CodingExperience, taskSucceeded bool) {
    exp.RecallCount++
    if taskSucceeded {
        exp.SuccessCount++
        exp.Confidence = min(exp.Confidence + successBoost, maxConfidence)
    } else {
        exp.FailureCount++
        exp.Confidence = max(exp.Confidence - failurePenalty, minConfidence)
    }
    
    // 状态升级
    if exp.Confidence >= verifiedThreshold && exp.RecallCount >= minRecallsForVerified {
        exp.Status = "verified"
    }
    // 状态降级
    if exp.Confidence <= deprecatedThreshold {
        exp.Status = "deprecated"
    }
    
    exp.LastRecalledAt = time.Now()
    exp.UpdatedAt = time.Now()
}
```

### 6.2 召回过滤

```go
// 只召回 active 和 verified 状态的经验
// candidate 不参与自动召回（需用户确认）
// deprecated 不参与召回（自动淘汰）
func recallFilter(exp CodingExperience) bool {
    return exp.Status == "active" || exp.Status == "verified"
}
```

### 6.3 反面证据追加

```go
// 当经验被召回但任务失败时，如果失败原因与经验相关，
// 追加 contraindication
func appendContraindication(exp *CodingExperience, failureContext string) {
    // 用 LLM 判断失败是否与经验建议相关
    related := classifyFailureRelation(exp.Content, failureContext)
    if related {
        exp.Contraindications = append(exp.Contraindications, failureContext)
    }
}
```

## 7. GUI 面板设计

### 7.1 设置位置

`设置 → 编程工具` 面板新增 "编程知识库" 区域：

```
┌──────────────────────────────────────────────────────────┐
│ 编程工具设置                                              │
├──────────────────────────────────────────────────────────┤
│                                                          │
│ ▸ SubAgent 并发数    [  3  ▼]                            │
│                                                          │
│ ▸ 外部编程工具                                            │
│   ...（已有）                                             │
│                                                          │
│ ▸ 编程知识库                                              │
│   ┌────────────────────────────────────────────────────┐ │
│   │ 状态: 42 条经验 (28 active / 8 verified / 6 候选)  │ │
│   │                                                    │ │
│   │ 自动沉淀: [观察模式 ▼]  策略: [重试成功时 ▼]       │ │
│   │                                                    │ │
│   │ [管理经验库]  [一键重置]  [导出]  [导入]           │ │
│   └────────────────────────────────────────────────────┘ │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

### 7.2 经验管理面板（点击"管理经验库"后展开）

```
┌──────────────────────────────────────────────────────────────┐
│ 编程知识库管理                                    [搜索...]   │
├──────────────────────────────────────────────────────────────┤
│ Scope: [通用] [Go] [Python] [TypeScript] [C++] [项目专属]    │
│ 筛选: [全部分类▼] [全部状态▼] [全部标签▼]                    │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│ ✅ Go interface 不能嵌套指针                    conf: 1.8    │
│    pitfall | go/compile_error | 语言:Go | 召回 7 次          │
│    ─────────────────────────────────────                     │
│ ✅ 这个项目序列化用 protobuf                    conf: 1.6    │
│    decision | protobuf | 项目:d:\workprj\morio | 召回 5 次   │
│    ─────────────────────────────────────                     │
│ ● 大文件先写临时文件再 rename                    conf: 1.2   │
│    pattern | file_io | 通用 | 召回 3 次                      │
│    ─────────────────────────────────────                     │
│ ○ range loop 闭包捕获（候选）                   conf: 1.0   │
│    pitfall | concurrency | 语言:Go | 未召回                  │
│    [确认] [删除]                                            │
│    ─────────────────────────────────────                     │
│ ⚠️ 用 mutex 包装 map（低置信度）                conf: 0.4   │
│    pattern | concurrency | 语言:Go | 召回 4 次 失败 3 次     │
│    [查看详情] [编辑] [删除]                                 │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│ 图例: ✅ verified  ● active  ○ candidate  ⚠️ 低置信度       │
│ 当前 tab: Go (15 条) | 总计: 42/1000 条                     │
│ [清空当前语言] [清空全部]                                    │
└──────────────────────────────────────────────────────────────┘
```

点击语言 Tab 只显示该语言的经验。"项目专属" Tab 再按项目路径分组。"通用" Tab 显示 scope=universal 的经验。清空操作也按 scope 粒度。

### 7.3 详情/编辑视图

```
┌──────────────────────────────────────────────────────────────┐
│ 编辑经验                                          [保存] [×] │
├──────────────────────────────────────────────────────────────┤
│ 标题:    [Go interface 不能嵌套指针              ]           │
│ 分类:    [pitfall ▼]                                        │
│ 触发条件: [Go + interface + 组合/嵌套            ]           │
│ 标签:    [go] [compile_error] [+添加]                       │
│ 项目:    [全局通用 ▼]                                       │
│ 过期版本: [                    ] (如 go1.22)                 │
│                                                              │
│ 内容:                                                        │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ ## 问题                                                  │ │
│ │ 在 Go 中定义 interface 时嵌套 *OtherInterface，         │ │
│ │ 编译报错 "interface contains embedded non-interface"     │ │
│ │                                                          │ │
│ │ ## 解决                                                  │ │
│ │ Go interface 只能嵌套 interface 本身，不能嵌套指针。     │ │
│ │ 移除 * 即可。                                            │ │
│ │                                                          │ │
│ │ ## 适用条件                                              │ │
│ │ - Go 项目                                                │ │
│ │ - 定义 interface 时引用其他 interface                    │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ 代码片段:                                                    │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ // 错误                                                  │ │
│ │ type MyInterface interface {                              │ │
│ │     *OtherInterface  // ← 编译错误                       │ │
│ │ }                                                        │ │
│ │ // 正确                                                  │ │
│ │ type MyInterface interface {                              │ │
│ │     OtherInterface   // ← 嵌套 interface 本身            │ │
│ │ }                                                        │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ 失败尝试:                                                    │
│  1. 尝试用 type assertion 绕过 → 运行时 panic              │
│  [+添加]                                                     │
│                                                              │
│ 不适用场景:                                                  │
│  (无)  [+添加]                                               │
│                                                              │
│ ── 统计 ──                                                   │
│ 置信度: 1.8 | 召回: 7 次 | 成功: 6 | 失败: 1               │
│ 来源任务: "实现 SSHManager 接口组合"                         │
│ 创建: 2026-06-10 | 最近召回: 2026-06-13                     │
│ 状态: verified                                               │
└──────────────────────────────────────────────────────────────┘
```

## 8. 配置项

### 8.1 AppConfig 新增字段

```go
// AppConfig (corelib/app_config.go)
type AppConfig struct {
    // ... 现有字段 ...
    
    // 编程工具设置（从 LLM 配置移入）
    CodingSubAgentConcurrency int `json:"coding_subagent_concurrency,omitempty"` // SubAgent 并发数
    
    // 编程知识库设置
    CodingKnowledgeEnabled       *bool  `json:"coding_knowledge_enabled,omitempty"`        // 总开关
    CodingKnowledgeAutoSaveMode  string `json:"coding_knowledge_auto_save_mode,omitempty"` // observe/auto/off
    CodingKnowledgeSaveStrategy  string `json:"coding_knowledge_save_strategy,omitempty"`  // always/on_success/on_retry_success/off
    CodingKnowledgeMaxPerProject int    `json:"coding_knowledge_max_per_project,omitempty"`// 单项目上限，默认 200
    CodingKnowledgeMaxTotal      int    `json:"coding_knowledge_max_total,omitempty"`      // 全局上限，默认 1000
}
```

### 8.2 默认值

```go
func (c *AppConfig) CodingKnowledgeDefaults() {
    if c.CodingKnowledgeEnabled == nil {
        t := true
        c.CodingKnowledgeEnabled = &t
    }
    if c.CodingKnowledgeAutoSaveMode == "" {
        c.CodingKnowledgeAutoSaveMode = "observe" // 初始保守
    }
    if c.CodingKnowledgeSaveStrategy == "" {
        c.CodingKnowledgeSaveStrategy = "on_retry_success" // 最有价值的经验
    }
    if c.CodingKnowledgeMaxPerProject <= 0 {
        c.CodingKnowledgeMaxPerProject = 200
    }
    if c.CodingKnowledgeMaxTotal <= 0 {
        c.CodingKnowledgeMaxTotal = 1000
    }
}
```

## 9. 实现计划

### Phase 1: 基础存储 + 读写接口

- `corelib/knowledge/coding_store.go`：基于 SQLiteStore 的编程知识库封装
  - `NewCodingKnowledgeStore(dbPath)` 构造
  - `SaveExperience(ctx, CodingExperience)` 写入
  - `SearchExperiences(ctx, query, opts)` 搜索
  - `UpdateConfidence(ctx, id, success)` 更新置信度
  - `ListExperiences(ctx, filter)` 列表
  - `GetExperience(ctx, id)` 详情
  - `UpdateExperience(ctx, id, patch)` 编辑
  - `DeleteExperience(ctx, id)` 删除
  - `Reset()` 清空
- `gui/app_coding_knowledge.go`：Wails bindings

### Phase 2: SubAgent 集成（读取路径）

- `gui/coding_subagent.go`：system prompt 注入双库知识
- `gui/coding_subagent.go`：工具列表新增 `coding_knowledge_search` + `knowledge_search`
- `gui/coding_subagent.go`：`codingSubAgentCallbacks` 持有双库引用

### Phase 3: 经验沉淀（写入路径）

- `gui/coding_subagent_experience.go`：经验提取 prompt + LLM 调用 + 去重 + 写入
- `gui/coding_subagent_orchestrator.go`：任务完成后触发沉淀
- 置信度更新逻辑

### Phase 4: GUI 管理面板

- 前端：编程工具设置页新增知识库管理区域
- 前端：经验列表/详情/编辑/删除视图
- 前端：状态统计 + 容量显示
- SubAgent 并发配置从 LLM 设置迁移到编程工具设置

### Phase 5: 演进机制

- 经验"毕业"→ steering 升级
- 容量淘汰策略
- 反面证据追加
- 导出/导入功能

## 10. Token 预算

```
SubAgent system prompt 构成（双库注入后）：

编码规范 + 任务描述        ~1500 token
编程经验召回（3-4 条）     ~1000 token
项目知识召回（2-3 条）     ~1500 token
工具定义（7 个）           ~1200 token
Windows shell contract     ~300 token
Skill 注入（如有）         ~500 token
──────────────────────────────────────
总计                       ~6000-7000 token

可用编码空间（128K 模型）：~95,000 token（不变）
```

## 11. 容量管理

### 11.1 淘汰策略

当经验数量达到上限时：

```go
func evictExperiences(store *CodingKnowledgeStore, projectPath string) {
    // 优先淘汰：
    // 1. deprecated 状态（confidence <= 0.3）
    // 2. candidate 状态且创建超过 30 天未被确认
    // 3. confidence 最低 + 最久未召回 的 active 经验
    
    candidates := store.ListByEvictionPriority(projectPath)
    toDelete := candidates[:overLimitCount]
    store.BatchDelete(toDelete)
}
```

### 11.2 统计接口

```go
type CodingKnowledgeStats struct {
    TotalCount     int            // 总条数
    ActiveCount    int            // active 状态
    VerifiedCount  int            // verified 状态
    CandidateCount int            // candidate 状态
    DeprecatedCount int           // deprecated 状态
    ByProject      map[string]int // 按项目统计
    ByCategory     map[string]int // 按分类统计
    ByLanguage     map[string]int // 按语言统计
    AvgConfidence  float64        // 平均置信度
    MaxPerProject  int            // 单项目上限
    MaxTotal       int            // 全局上限
}
```

## 12. 失败链追踪（经验关联）

### 12.1 依赖关系

```go
type ExperienceDependency struct {
    FromID   string // 前置经验 ID
    ToID     string // 后续经验 ID
    Relation string // "leads_to" | "conflicts_with" | "supersedes"
}
```

### 12.2 召回时自动带入关联经验

当一条经验被召回时，检查它的 `leads_to` 依赖，将依赖经验一并注入（在 token 预算允许的情况下）。

## 13. 安全与恢复

### 13.1 防止污染

- 新经验默认 `candidate` 状态，不参与自动召回
- 置信度 < 0.3 自动降级为 `deprecated`
- 面板中"低置信度"经验高亮显示，方便手动审核
- "一键重置"直接删除 `coding_knowledge.db` 文件

### 13.2 备份策略

- 每次写入前自动 checkpoint（SQLite WAL checkpoint）
- 导出功能：JSON 格式全量导出，可用于分享和恢复
- 独立文件的好处：文件级 copy 即备份

## 14. 与主 Agent 的交互

### 14.1 主 Agent 手动注入经验

主 Agent（非 SubAgent）也可以通过工具调用写入编程经验：

```go
// manage_coding_knowledge(action="save", ...)
// 场景：用户告诉主 Agent "这个项目用 tab 缩进"，主 Agent 保存到编程知识库
```

### 14.2 经验上报给主 Agent

SubAgent 任务完成后的经验总结，除了写入知识库，也可以作为 tool_result 返回给主 Agent。主 Agent 决定是否需要更新更上层的 steering 规则。

---

## 15. 未来演进

- **经验"毕业"**：verified 经验一键升级为 `.maclaw/steering/` 规则文件
- **团队共享**：导出/导入经验包，通过 Hub 发布
- **跨项目迁移**：全局通用经验自动应用到新项目
- **embedding 检索**：当 FTS 召回不够精准时，用 embedding 向量做语义匹配
- **经验冲突检测**：两条经验建议矛盾时自动标记，人工裁定
