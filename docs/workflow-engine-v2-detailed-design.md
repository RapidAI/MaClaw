# 工作流引擎 V2 详细设计

## 1. 包结构

```
corelib/workflow/v2/
├── state.go            # WorkflowState + Phase 数据结构
├── machine.go          # StateMachine 状态转换逻辑
├── router.go           # WorkflowRouter 消息路由决策
├── templates.go        # 模板注册表（复用 V1 模板定义数据）
├── phase_prompt.go     # 阶段 system prompt 构建
├── store.go            # WorkflowStore 接口
├── store_sqlite.go     # SQLite 实现
├── store_memory.go     # 测试用内存实现
├── task_parser.go      # 任务列表解析（从 Markdown 提取）
└── *_test.go

gui/
├── workflow_v2_router.go       # GUI 侧 Router 集成
├── workflow_v2_phase_exec.go   # PhaseExecutor（配置 agent loop）
├── workflow_v2_task_runner.go  # TaskRunner（SubAgent 调度）
├── workflow_v2_events.go       # 前端事件发射（doc preview, progress）
└── workflow_v2_integration.go  # 接入 im_message_handler 的胶水代码
```

---

## 2. 数据结构

### 2.1 WorkflowState

```go
package v2

type WorkflowStatus string

const (
    StatusActive    WorkflowStatus = "active"
    StatusCompleted WorkflowStatus = "completed"
    StatusCancelled WorkflowStatus = "cancelled"
)

type PhaseStatus string

const (
    PhasePending        PhaseStatus = "pending"
    PhaseRunning        PhaseStatus = "running"
    PhaseWaitingConfirm PhaseStatus = "waiting_confirm"
    PhaseCompleted      PhaseStatus = "completed"
    PhaseSkipped        PhaseStatus = "skipped"
)

type ToolPolicy string

const (
    ToolPolicyDocOnly ToolPolicy = "doc_only"   // 只允许 read/search/memory 工具
    ToolPolicyFull    ToolPolicy = "full"        // 允许所有工具（执行阶段）
)

type Phase struct {
    ID           string      `json:"id"`
    Name         string      `json:"name"`
    NeedsConfirm bool        `json:"needs_confirm"`
    ToolPolicy   ToolPolicy  `json:"tool_policy"`
    Status       PhaseStatus `json:"status"`
    Output       string      `json:"output,omitempty"`
}

type WorkflowState struct {
    ID           string         `json:"id"`
    UserID       string         `json:"user_id"`
    Type         string         `json:"type"`           // "coding", "presentation_design", etc.
    ProjectPath  string         `json:"project_path"`   // 创建时确定，之后只读
    Summary      string         `json:"summary"`        // 用户需求摘要
    Phases       []Phase        `json:"phases"`
    CurrentPhase int            `json:"current_phase"`  // 索引
    Status       WorkflowStatus `json:"status"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
}

// 只读访问
func (s *WorkflowState) ActivePhase() *Phase
func (s *WorkflowState) IsExecutionPhase() bool
func (s *WorkflowState) PreviousOutputs() map[string]string  // phase_id → output
func (s *WorkflowState) IsWaitingConfirm() bool
```

---

### 2.2 WorkflowTemplate

```go
type PhaseTemplate struct {
    ID           string     `json:"id"`
    Name         string     `json:"name"`
    NeedsConfirm bool       `json:"needs_confirm"`
    ToolPolicy   ToolPolicy `json:"tool_policy"`
    PromptFunc   func(ctx PhasePromptContext) string  // 生成阶段 system prompt
}

type WorkflowTemplate struct {
    Type        string          `json:"type"`
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Keywords    []string        `json:"keywords"`
    Phases      []PhaseTemplate `json:"phases"`
}

// 内置模板
func CodingTemplate() *WorkflowTemplate
func PresentationTemplate() *WorkflowTemplate
func ProductDesignTemplate() *WorkflowTemplate
// ... 其他模板
```

**编码模板定义**：
```go
func CodingTemplate() *WorkflowTemplate {
    return &WorkflowTemplate{
        Type: "coding",
        Name: "编程项目",
        Keywords: []string{"开发", "编写", "实现", "写代码", "游戏", "应用", "工具"},
        Phases: []PhaseTemplate{
            {ID: "requirements", Name: "需求文档", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
            {ID: "design",       Name: "技术设计", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
            {ID: "tasks",        Name: "任务分解", NeedsConfirm: true, ToolPolicy: ToolPolicyDocOnly},
            {ID: "implementation", Name: "编码执行", NeedsConfirm: false, ToolPolicy: ToolPolicyFull},
        },
    }
}
```

---

## 3. StateMachine

### 3.1 接口

```go
type StateMachine struct {
    store     WorkflowStore
    templates *TemplateRegistry
}

// 创建工作流
func (m *StateMachine) Create(userID, workflowType, projectPath, summary string) (*WorkflowState, error)

// 处理用户输入（确认/修改/取消/无关消息）
func (m *StateMachine) HandleInput(userID, text string) (*HandleResult, error)

// 记录阶段产出物（由 PhaseExecutor 调用）
func (m *StateMachine) RecordOutput(userID, output string) error

// 推进到下一阶段（用户确认后调用）
func (m *StateMachine) Advance(userID string) error

// 取消工作流
func (m *StateMachine) Cancel(userID string) error

// 查询
func (m *StateMachine) GetActive(userID string) *WorkflowState
func (m *StateMachine) IsWaitingConfirm(userID string) bool
```

### 3.2 HandleResult

```go
type HandleAction string

const (
    ActionRunPhase     HandleAction = "run_phase"       // 执行当前阶段
    ActionConfirmed    HandleAction = "confirmed"       // 用户确认，推进
    ActionModify       HandleAction = "modify"          // 用户要修改，重新执行当前阶段
    ActionPassThrough  HandleAction = "pass_through"    // 与工作流无关，走普通 agent loop
    ActionCancelled    HandleAction = "cancelled"       // 用户取消
)

type HandleResult struct {
    Action      HandleAction
    Phase       *Phase              // 当前要执行的阶段（ActionRunPhase 时有值）
    PhasePrompt string              // 注入到 agent loop 的 system prompt
    ModifyHint  string              // 用户的修改意见（ActionModify 时有值）
}
```

### 3.3 状态转换规则

```go
func (m *StateMachine) HandleInput(userID, text string) (*HandleResult, error) {
    state := m.store.Load(userID)
    
    // 没有活跃工作流 → pass through
    if state == nil {
        return &HandleResult{Action: ActionPassThrough}, nil
    }
    
    phase := state.ActivePhase()
    
    switch phase.Status {
    case PhasePending, PhaseRunning:
        // 阶段正在执行中（首次进入或修改后重新执行）
        return &HandleResult{
            Action:      ActionRunPhase,
            Phase:       phase,
            PhasePrompt: m.buildPhasePrompt(state),
        }, nil
        
    case PhaseWaitingConfirm:
        // 等待用户确认
        intent := classifyConfirmIntent(text)  // confirm / modify / cancel / unrelated
        switch intent {
        case "confirm":
            m.Advance(userID)
            next := state.ActivePhase()
            if next == nil {
                // 所有阶段完成
                state.Status = StatusCompleted
                m.store.Save(state)
                return &HandleResult{Action: ActionConfirmed}, nil
            }
            return &HandleResult{
                Action:      ActionRunPhase,
                Phase:       next,
                PhasePrompt: m.buildPhasePrompt(state),
            }, nil
        case "modify":
            phase.Status = PhaseRunning
            m.store.Save(state)
            return &HandleResult{
                Action:      ActionModify,
                Phase:       phase,
                PhasePrompt: m.buildPhasePrompt(state),
                ModifyHint:  text,
            }, nil
        case "cancel":
            m.Cancel(userID)
            return &HandleResult{Action: ActionCancelled}, nil
        default:
            // 无关消息 → pass through
            return &HandleResult{Action: ActionPassThrough}, nil
        }
    }
    
    return &HandleResult{Action: ActionPassThrough}, nil
}
```

### 3.4 确认意图分类

```go
// classifyConfirmIntent 判断用户消息的意图
// 不用 LLM，纯关键词。简单可靠。
func classifyConfirmIntent(text string) string {
    lower := strings.ToLower(strings.TrimSpace(text))
    
    // 确认
    confirmWords := []string{"确认", "ok", "好", "可以", "没问题", "通过", "确定", "继续", "confirm", "yes", "lgtm"}
    for _, w := range confirmWords {
        if strings.Contains(lower, w) {
            return "confirm"
        }
    }
    
    // 取消
    cancelWords := []string{"取消", "cancel", "放弃", "不做了", "算了"}
    for _, w := range cancelWords {
        if strings.Contains(lower, w) {
            return "cancel"
        }
    }
    
    // 修改（包含具体内容的消息默认为修改意见）
    if len([]rune(text)) > 4 {
        return "modify"
    }
    
    // 太短且不匹配以上 → 无关
    return "unrelated"
}
```

---

## 4. WorkflowRouter

### 4.1 接口

```go
type RouteResult struct {
    Target       RouteTarget
    WorkflowType string           // 新建工作流时的类型
    ProjectPath  string           // 从消息中提取的项目路径
    HandleResult *HandleResult    // 已有工作流时的处理结果
}

type RouteTarget string

const (
    RouteToAgentLoop RouteTarget = "agent_loop"
    RouteToWorkflow  RouteTarget = "workflow"
)

type WorkflowRouter struct {
    machine   *StateMachine
    templates *TemplateRegistry
    llmFunc   func(system, user string) (string, error)  // 可选的 LLM 确认
}

func (r *WorkflowRouter) Route(userID, text string, attachments []Attachment) *RouteResult
```

### 4.2 路由逻辑

```go
func (r *WorkflowRouter) Route(userID, text string, attachments []Attachment) *RouteResult {
    // Step 1: 已有活跃工作流 → 交给 StateMachine
    if state := r.machine.GetActive(userID); state != nil {
        result, err := r.machine.HandleInput(userID, text)
        if err != nil {
            return &RouteResult{Target: RouteToAgentLoop}  // 出错降级
        }
        if result.Action == ActionPassThrough {
            return &RouteResult{Target: RouteToAgentLoop}
        }
        return &RouteResult{Target: RouteToWorkflow, HandleResult: result}
    }
    
    // Step 2: 有附件（图片等）→ 直接 agent loop
    if len(attachments) > 0 && len([]rune(text)) < 50 {
        return &RouteResult{Target: RouteToAgentLoop}
    }
    
    // Step 3: 跳过信号 → agent loop
    if hasSkipSignal(text) {
        return &RouteResult{Target: RouteToAgentLoop}
    }
    
    // Step 4: 关键词匹配工作流模板
    matched := r.templates.MatchByKeywords(text)
    if matched == nil {
        return &RouteResult{Target: RouteToAgentLoop}
    }
    
    // Step 5: 提取 projectPath
    projectPath := extractProjectPathFromText(text)
    
    // Step 6: 确认是编码任务（可选 LLM 确认，失败则按关键词结果走）
    if r.llmFunc != nil && matched.Type == "coding" {
        confirmed := r.confirmWithLLM(text, matched)
        if !confirmed {
            return &RouteResult{Target: RouteToAgentLoop}
        }
    }
    
    return &RouteResult{
        Target:       RouteToWorkflow,
        WorkflowType: matched.Type,
        ProjectPath:  projectPath,
    }
}
```

### 4.3 跳过信号

```go
var skipSignals = []string{
    "直接做", "不用问了", "按你的想法来", "跳过文档", "不需要文档",
    "直接开始", "不用三阶段", "skip workflow",
}

func hasSkipSignal(text string) bool {
    lower := strings.ToLower(text)
    for _, sig := range skipSignals {
        if strings.Contains(lower, sig) {
            return true
        }
    }
    return false
}
```

### 4.4 项目路径提取

```go
// extractProjectPathFromText 从用户消息中提取明确的项目路径
// 只提取用户明确指定的路径，不猜测
func extractProjectPathFromText(text string) string {
    // Windows 路径: d:\xxx, c:\users\xxx
    // Unix 路径: /home/xxx, ~/xxx
    // "在 xxx 下" 模式
    patterns := []string{
        `(?:在|到|去)\s*([a-zA-Z]:\\[^\s,，。]+)`,   // "在 d:\game2 下"
        `(?:在|到|去)\s*(\/[^\s,，。]+)`,            // "在 /home/user/project 下"
        `(?:在|到|去)\s*(~\/[^\s,，。]+)`,           // "在 ~/project 下"
        `([a-zA-Z]:\\(?:[^\s\\]+\\)*[^\s\\,，。]+)`, // 独立出现的 Windows 路径
    }
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(text); len(matches) > 1 {
            return strings.TrimRight(matches[1], " 下里中内")
        }
    }
    return ""  // 没提取到则使用默认项目路径
}
```

---

## 5. PhaseExecutor

### 5.1 与 agent loop 的集成

PhaseExecutor 不是独立的执行器，而是**配置 agent loop 的一组参数**。

```go
// PhaseLoopConfig 注入到 LoopContext 中，控制 agent loop 的行为
type PhaseLoopConfig struct {
    Phase        *Phase              // 当前阶段
    PhasePrompt  string              // 阶段 system prompt（追加到基础 prompt 之后）
    ToolPolicy   ToolPolicy          // 工具过滤策略
    ModifyHint   string              // 用户修改意见（非空时注入到 prompt）
    OnOutput     func(text string)   // LLM 产出实质性文本时回调
}
```

### 5.2 agent loop 中的行为

在 `runAgentLoop` 中，当 `ctx.PhaseLoopConfig != nil` 时：

```go
// 1. System prompt 注入
systemPrompt += "\n\n" + ctx.PhaseLoopConfig.PhasePrompt
if ctx.PhaseLoopConfig.ModifyHint != "" {
    systemPrompt += "\n\n用户修改意见：" + ctx.PhaseLoopConfig.ModifyHint
}

// 2. 工具过滤
if ctx.PhaseLoopConfig.ToolPolicy == ToolPolicyDocOnly {
    tools = filterDocOnlyTools(tools)
}

// 3. no-tool 分支：检查是否产出了实质性文档
if isSubstantiveDocument(msgContent) && ctx.PhaseLoopConfig.Phase.NeedsConfirm {
    // 记录产出物
    ctx.PhaseLoopConfig.OnOutput(msgContent)
    // force-return
    return response
}
```

**就这三个 hook**。取代了现有的 `coding-gate`、`NeedsConfirm from engine`、`NeedsConfirm from steering`、`SteeringWorkflowDetector`、`HasPhaseOutput` 检查、`isSubstantivePhaseDocument` + `engineGateActive` 组合等一堆逻辑。

### 5.3 阶段 prompt 构建

```go
type PhasePromptContext struct {
    WorkflowType    string
    PhaseName       string
    PhaseID         string
    Summary         string            // 用户需求摘要
    PreviousOutputs map[string]string  // 前序阶段产出物（截断到 500 rune）
    ProjectPath     string
}

func buildPhasePrompt(ctx PhasePromptContext) string {
    var sb strings.Builder
    
    sb.WriteString(fmt.Sprintf("## 当前任务\n\n你正在执行「%s」工作流的「%s」阶段。\n\n", ctx.WorkflowType, ctx.PhaseName))
    sb.WriteString(fmt.Sprintf("用户需求：%s\n\n", ctx.Summary))
    
    if ctx.ProjectPath != "" {
        sb.WriteString(fmt.Sprintf("项目路径：%s\n\n", ctx.ProjectPath))
    }
    
    // 前序阶段摘要（每个截断到 500 rune）
    if len(ctx.PreviousOutputs) > 0 {
        sb.WriteString("## 前序阶段产出物（摘要）\n\n")
        for id, output := range ctx.PreviousOutputs {
            truncated := truncateRunes(output, 500)
            sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", id, truncated))
        }
    }
    
    // 阶段专用指令
    sb.WriteString(phaseSpecificInstruction(ctx.PhaseID))
    
    return sb.String()
}

func phaseSpecificInstruction(phaseID string) string {
    switch phaseID {
    case "requirements":
        return `请生成需求文档（Markdown 格式），包含：
- 功能需求
- 非功能需求
- 边界情况
- 验收标准

信息不足的部分标记为「⚠️ 待确认」。直接生成文档，不要先问澄清问题。`
    case "design":
        return `基于已确认的需求，生成技术设计文档（Markdown 格式），包含：
- 架构设计
- 技术选型
- 模块划分
- 接口设计
- 数据结构`
    case "tasks":
        return `基于已确认的设计，生成任务拆分文档。使用以下格式：

### T1: 任务标题
- **描述**：具体要做什么
- **涉及文件**：file1.cpp, file2.h
- **依赖**：无 / 依赖 T0
- **优先级**：P0/P1/P2
- **工作量**：预估说明

每个任务必须包含以上五个字段。`
    case "implementation":
        return ""  // 执行阶段由 TaskRunner 接管
    default:
        return ""
    }
}
```

---

## 6. TaskRunner

### 6.1 设计

```go
type TaskRunner struct {
    handler     *IMMessageHandler
    llmConfig   corelib.MaclawLLMConfig
    httpClient  *http.Client
}

type TaskItem struct {
    Index       int
    Title       string
    Description string
    Files       []string
    DependsOn   []int
}

type TaskRunResult struct {
    TaskIndex    int
    Status       string  // "passed", "failed", "skipped"
    Summary      string
    FilesCreated []string
    FilesModified []string
    Error        string
}

func (r *TaskRunner) RunAll(
    ctx context.Context,
    tasks []*TaskItem,
    projectPath string,
    requirementsCtx string,
    designCtx string,
    onToken func(string),
    onProgress func(string),
) []TaskRunResult
```

### 6.2 SubAgent 配置

```go
func (r *TaskRunner) runSingleTask(ctx context.Context, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string) *TaskRunResult {
    systemPrompt := buildSubAgentPrompt(task, projectPath, reqCtx, designCtx, prevOutputs)
    
    tools := []string{"read_file", "write_file", "edit_file", "bash", "list_directory"}
    
    // 工具安全策略：
    // - write_file: 路径必须在 projectPath 内（或其子目录）
    // - edit_file: 同上
    // - bash: 不限制路径（需要编译、安装依赖等），但禁止 rm -rf /、format 等
    // - read_file: 不限制（需要读系统头文件等）
    // - list_directory: 不限制
    
    result := runSubAgentLoop(ctx, systemPrompt, tools, projectPath, r.llmConfig, r.httpClient)
    return result
}
```

### 6.3 SubAgent 安全策略（简化版）

```go
func validateSubAgentToolCall(toolName string, args map[string]interface{}, projectPath string) error {
    switch toolName {
    case "write_file", "edit_file":
        path := args["path"].(string)
        if !isWithinProject(path, projectPath) {
            return fmt.Errorf("write/edit 路径必须在项目目录 %s 内", projectPath)
        }
        return nil
    case "bash":
        cmd := args["command"].(string)
        if isDangerousCommand(cmd) {
            return fmt.Errorf("禁止执行危险命令: %s", cmd)
        }
        return nil  // bash 不限制工作目录
    default:
        return nil  // read_file, list_directory 不限制
    }
}

func isWithinProject(path, projectPath string) bool {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return false
    }
    absProject, err := filepath.Abs(projectPath)
    if err != nil {
        return false
    }
    rel, err := filepath.Rel(absProject, absPath)
    if err != nil {
        return false
    }
    return !strings.HasPrefix(rel, "..")
}

func isDangerousCommand(cmd string) bool {
    dangerous := []string{
        "rm -rf /", "rmdir /s /q c:", "format c:",
        "del /s /q c:", "rd /s /q c:",
    }
    lower := strings.ToLower(cmd)
    for _, d := range dangerous {
        if strings.Contains(lower, d) {
            return true
        }
    }
    return false
}
```

---

## 7. WorkflowStore

### 7.1 接口

```go
type WorkflowStore interface {
    Save(state *WorkflowState) error
    Load(userID string) (*WorkflowState, error)
    Delete(userID string) error
    ListActive() ([]*WorkflowState, error)
}
```

### 7.2 SQLite 实现

```go
// 使用新文件 ~/.maclaw/workflow_v2.db，不复用旧 workflow.db
type SQLiteStoreV2 struct {
    db *sql.DB
}

func NewSQLiteStoreV2(dbPath string) (*SQLiteStoreV2, error) {
    db, err := sql.Open("sqlite3", dbPath)
    if err != nil {
        return nil, err
    }
    _, err = db.Exec(`CREATE TABLE IF NOT EXISTS workflows (
        user_id    TEXT PRIMARY KEY,
        state_json TEXT NOT NULL,
        updated_at DATETIME NOT NULL
    )`)
    return &SQLiteStoreV2{db: db}, err
}

func (s *SQLiteStoreV2) Save(state *WorkflowState) error {
    state.UpdatedAt = time.Now()
    data, err := json.Marshal(state)
    if err != nil {
        return err
    }
    _, err = s.db.Exec(
        `INSERT OR REPLACE INTO workflows (user_id, state_json, updated_at) VALUES (?, ?, ?)`,
        state.UserID, string(data), state.UpdatedAt,
    )
    return err
}
```

### 7.3 Memory 实现（测试专用）

```go
type MemoryStore struct {
    mu    sync.RWMutex
    states map[string]*WorkflowState
}

func NewMemoryStore() *MemoryStore {
    return &MemoryStore{states: make(map[string]*WorkflowState)}
}
```

---

## 8. GUI 集成

### 8.1 接入点（替代现有 handleWorkflowInterception）

```go
// workflow_v2_integration.go

func (h *IMMessageHandler) routeWithWorkflowV2(msg IMUserMessage) *IMAgentResponse {
    router := h.workflowRouterV2
    if router == nil {
        return nil  // V2 未启用，走原有逻辑
    }
    
    result := router.Route(msg.UserID, msg.Text, msg.Attachments)
    
    switch result.Target {
    case RouteToAgentLoop:
        return nil  // 返回 nil 表示走普通 agent loop
        
    case RouteToWorkflow:
        if result.HandleResult != nil {
            // 已有工作流
            return h.executeWorkflowAction(msg, result.HandleResult)
        }
        // 新建工作流
        return h.startNewWorkflowV2(msg, result)
    }
    
    return nil
}

func (h *IMMessageHandler) startNewWorkflowV2(msg IMUserMessage, result *RouteResult) *IMAgentResponse {
    projectPath := result.ProjectPath
    if projectPath == "" {
        projectPath = h.app.GetCurrentProjectPath()
    }
    
    state, err := h.workflowMachine.Create(msg.UserID, result.WorkflowType, projectPath, msg.Text)
    if err != nil {
        return &IMAgentResponse{Error: fmt.Sprintf("工作流创建失败: %v", err)}
    }
    
    // 直接开始第一阶段
    phase := state.ActivePhase()
    return h.runPhaseAgentLoop(msg.UserID, phase, h.workflowMachine.BuildPhasePrompt(state), "")
}

func (h *IMMessageHandler) executeWorkflowAction(msg IMUserMessage, result *HandleResult) *IMAgentResponse {
    switch result.Action {
    case ActionRunPhase:
        return h.runPhaseAgentLoop(msg.UserID, result.Phase, result.PhasePrompt, result.ModifyHint)
    case ActionConfirmed:
        if result.Phase != nil {
            // 推进到下一阶段
            return h.runPhaseAgentLoop(msg.UserID, result.Phase, result.PhasePrompt, "")
        }
        return &IMAgentResponse{Text: "✅ 工作流已完成"}
    case ActionModify:
        return h.runPhaseAgentLoop(msg.UserID, result.Phase, result.PhasePrompt, result.ModifyHint)
    case ActionCancelled:
        return &IMAgentResponse{Text: "❌ 工作流已取消"}
    }
    return nil
}
```

### 8.2 PhaseExecutor 集成

```go
func (h *IMMessageHandler) runPhaseAgentLoop(userID string, phase *Phase, phasePrompt, modifyHint string) *IMAgentResponse {
    if phase.ID == "implementation" {
        // 执行阶段 → TaskRunner
        return h.runTaskRunnerV2(userID)
    }
    
    // 文档阶段 → 配置 agent loop
    loopCtx := &LoopContext{
        PhaseConfig: &PhaseLoopConfig{
            Phase:       phase,
            PhasePrompt: phasePrompt,
            ToolPolicy:  phase.ToolPolicy,
            ModifyHint:  modifyHint,
            OnOutput: func(text string) {
                h.workflowMachine.RecordOutput(userID, text)
                phase.Status = PhaseWaitingConfirm
                h.workflowMachine.store.Save(h.workflowMachine.GetActive(userID))
                // 发射前端事件
                h.emitDocUpdate(userID, phase.ID, text)
            },
        },
    }
    
    return h.runAgentLoopWithContext(userID, loopCtx)
}
```

### 8.3 前端事件

```go
// workflow_v2_events.go

func (h *IMMessageHandler) emitDocUpdate(userID, phaseID, content string) {
    if h.app == nil || h.app.ctx == nil {
        return
    }
    runtime.EventsEmit(h.app.ctx, "workflow:doc_update", map[string]interface{}{
        "user_id":  userID,
        "phase_id": phaseID,
        "content":  content,
    })
}

func (h *IMMessageHandler) emitWorkflowProgress(userID string, state *WorkflowState) {
    runtime.EventsEmit(h.app.ctx, "workflow:progress", map[string]interface{}{
        "user_id":       userID,
        "current_phase": state.CurrentPhase,
        "total_phases":  len(state.Phases),
        "phase_name":    state.ActivePhase().Name,
        "status":        state.Status,
    })
}
```

---

## 9. 错误处理与降级

### 9.1 原则

- **LLM 不可用时不阻塞用户**：Router 的 LLM 确认失败 → 按关键词结果走
- **状态机操作失败时降级到 agent loop**：任何 StateMachine 方法返回 error → 走普通 agent loop
- **SubAgent 失败时给出明确报告**：TaskRunner 中 3 次重试后标记任务 failed，继续下一个

### 9.2 项目路径校验

```go
func (m *StateMachine) Create(userID, workflowType, projectPath, summary string) (*WorkflowState, error) {
    // 校验 projectPath
    if projectPath == "" {
        return nil, fmt.Errorf("projectPath is required")
    }
    // 不要求目录已存在（用户可能要从头创建项目）
    // 但要求路径格式合法
    if !isValidProjectPath(projectPath) {
        return nil, fmt.Errorf("invalid project path: %s", projectPath)
    }
    // 绝对不允许临时目录、测试目录
    if looksLikeTempPath(projectPath) {
        return nil, fmt.Errorf("project path cannot be a temp directory: %s", projectPath)
    }
    ...
}

func looksLikeTempPath(path string) bool {
    lower := strings.ToLower(filepath.Clean(path))
    return strings.Contains(lower, string(os.PathSeparator)+"temp"+string(os.PathSeparator)) &&
        (strings.Contains(lower, "test") || strings.Contains(lower, "tmp"))
}
```

---

## 10. 测试策略

### 10.1 单元测试（纯逻辑，无 IO）

```go
func TestStateMachine_CreateAndAdvance(t *testing.T) {
    store := NewMemoryStore()
    templates := NewTemplateRegistry()
    templates.Register(CodingTemplate())
    machine := NewStateMachine(store, templates)
    
    // 创建
    state, err := machine.Create("user1", "coding", "d:\\game2", "开发贪吃蛇")
    require.NoError(t, err)
    assert.Equal(t, "requirements", state.ActivePhase().ID)
    assert.Equal(t, PhaseRunning, state.ActivePhase().Status)
    
    // 记录产出物
    machine.RecordOutput("user1", "# 需求文档\n...")
    assert.Equal(t, PhaseWaitingConfirm, state.ActivePhase().Status)
    
    // 用户确认
    result, err := machine.HandleInput("user1", "确认")
    require.NoError(t, err)
    assert.Equal(t, ActionRunPhase, result.Action)
    assert.Equal(t, "design", result.Phase.ID)
}
```

### 10.2 集成测试

```go
func TestWorkflowRouter_CodingTask(t *testing.T) {
    store := NewMemoryStore()  // 永远不碰文件系统
    router := NewWorkflowRouter(store, nil)  // llmFunc=nil, 只用关键词
    
    result := router.Route("user1", "在 d:\\game2 下开发贪吃蛇 C++", nil)
    assert.Equal(t, RouteToWorkflow, result.Target)
    assert.Equal(t, "coding", result.WorkflowType)
    assert.Equal(t, "d:\\game2", result.ProjectPath)
}

func TestWorkflowRouter_NonCodingTask(t *testing.T) {
    store := NewMemoryStore()
    router := NewWorkflowRouter(store, nil)
    
    result := router.Route("user1", "帮我查一下杭州天气", nil)
    assert.Equal(t, RouteToAgentLoop, result.Target)
}
```

---

## 11. 迁移策略——直接替换，不共存

### 11.1 实施顺序

1. **先写 V2 代码**（新包 `corelib/workflow/v2/` + GUI 集成文件）
2. **V2 编译通过 + 测试通过后，一次性删除 V1 代码**
3. 不搞灰度开关、不搞共存期、不留死代码

### 11.2 删除清单（V2 就绪后执行）

**corelib/workflow/（V1 核心）**：
- `engine.go` — 旧引擎主文件
- `engine_review_state.go` — 复杂的 review/confirm 子状态
- `intent_understanding.go` — 多轮 IUM
- `quick_filter.go` — 6 层中的第 1 层
- `prompt_builder.go` — 旧 prompt 构建
- `experience_provider.go` — 工作流经验
- `sqlite_store.go` — 旧 SQLite store（V2 用新文件新表）
- 所有对应的 `*_test.go`

**gui/（V1 集成层，30+ 文件）**：
- `im_message_handler_workflow.go` — 1800 行的工作流拦截
- `im_agent_loop_coding_gate.go` — CodingToolGate
- `coding_tool_gate.go` — gate 逻辑
- `workflow_orchestrator_activation.go` — orchestrator 激活
- `workflow_adapter_persistence.go` — doc 持久化
- `coding_subagent.go` — 旧 SubAgent（V2 重写）
- `coding_subagent_orchestrator.go` — 旧 orchestrator
- `coding_subagent_admission.go` — 入场检查
- `task_execution_orchestrator.go` — 任务调度
- `im_subagent_route.go` — SubAgent 路由
- `gate_intent_classifier.go` — 6 层中的第 4 层
- 所有 `workflow_v1_*` 或 V1 专用的 test 文件

**清理旧 DB**：
- 删除 `workflow.db`（旧 V1 数据）
- V2 使用新文件名 `workflow_v2.db`

### 11.3 不做数据迁移

工作流状态是临时性的（进行中的任务），不是用户永久数据。V1 遗留的旧状态直接丢弃。

---

## 12. 与 V1 的对比

| 维度 | V1 | V2 |
|------|----|----|
| 决策层数 | 6-7 层 | 1 层 (Router) |
| 状态传递 | 8+ sync.Map flags | 1 个 LoopContext.PhaseConfig |
| projectPath 来源 | 5 个 fallback 路径 | Create 时确定，不可变 |
| NeedsConfirm 判断 | 3 个独立条件 OR | 1 个 Phase.NeedsConfirm |
| 测试隔离 | 共享 workflow.db | MemoryStore |
| 门控逻辑位置 | agent loop 内部 30 行 if-else | PhaseLoopConfig 3 个 hook |
| SubAgent 沙箱 | 全路径限制 | 只限 write_file |
| 文件数 | 30+ | 10-12 |
| 降级策略 | 每层独立降级 | 统一：失败 → agent loop |
