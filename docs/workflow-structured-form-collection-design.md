# 工作流结构化表单信息收集设计文档

## 概述

将工作流模板中"信息收集"阶段从自然语言对话改为 **AG UI 结构化表单**。用户通过右侧任务面板的表单控件（输入框、下拉选择、多选、必填校验等）提交信息，系统将表单数据注入后续阶段的 PhasePrompt。

**核心原则**：完全复用现有 AG UI 组件体系（`AgentTaskPanel` + `AgentView type="form"`），不生成任何新 UI 代码。

## 现有基础设施

| 层 | 已有能力 | 复用方式 |
|---|---------|---------|
| 前端渲染 | `AgentTaskPanel` 组件 | 直接使用，零改动 |
| 字段类型 | text/textarea/number/date/select/multiselect/boolean/file/hidden | 直接使用 |
| 校验 | required/min/max/minLength/maxLength/pattern/options | 直接使用 |
| 事件协议 | `agent-view:lifecycle` (open/submit/dismiss/complete) | 直接使用 |
| 提交路由 | `handleAgentViewSubmitPayload` → `classifyMISAgentViewID` switch | 新增一个 case |
| 本地表单生成 | `resolveIntentLocalFallback` | 参考模式 |

## 数据模型设计

### 1. PhaseTemplate 新增 `InputSchema` 字段

```go
// corelib/workflow/types.go

// PhaseInputField defines a single form field for structured information collection.
// It maps directly to AgentViewField on the frontend — same field names, same semantics.
type PhaseInputField struct {
    Name        string                 `json:"name"`
    Label       string                 `json:"label"`
    Type        string                 `json:"type"`         // text|textarea|number|date|datetime|select|multiselect|boolean|file|directory|hidden|object_form|array_table
    Required    bool                   `json:"required,omitempty"`
    Description string                 `json:"description,omitempty"`
    Placeholder string                 `json:"placeholder,omitempty"`
    Options     []PhaseInputOption     `json:"options,omitempty"`     // for select/multiselect
    Default     interface{}            `json:"default,omitempty"`
    Min         *float64               `json:"min,omitempty"`
    Max         *float64               `json:"max,omitempty"`
    MinLength   *int                   `json:"min_length,omitempty"`
    MaxLength   *int                   `json:"max_length,omitempty"`
    Pattern     string                 `json:"pattern,omitempty"`
}

type PhaseInputOption struct {
    Label string `json:"label"`
    Value string `json:"value"`
}

// PhaseInputSchema declares the structured form for a phase's information collection.
// When non-nil, the engine emits an AgentView form instead of running the agent loop
// directly. The form data is injected into the PhasePrompt as structured context.
type PhaseInputSchema struct {
    Title       string            `json:"title"`
    Description string            `json:"description,omitempty"`
    Fields      []PhaseInputField `json:"fields"`
}

// PhaseTemplate 新增字段
type PhaseTemplate struct {
    // ... existing fields ...

    // InputSchema declares a structured form for this phase's information collection.
    // When set, the engine emits an AG UI form before running the agent loop.
    // The user's form submission is injected into the PhasePrompt as structured context.
    // Nil means the phase uses natural language interaction (current behavior).
    InputSchema *PhaseInputSchema `json:"input_schema,omitempty"`
}
```

### 2. WorkflowState 新增 `PhaseFormData` 字段

```go
// corelib/workflow/types.go — WorkflowState

type WorkflowState struct {
    // ... existing fields ...

    // PhaseFormData stores the user's form submission for the current phase.
    // Populated when the user submits a PhaseInputSchema form.
    // Cleared when the phase advances.
    PhaseFormData map[string]interface{} `json:"phase_form_data,omitempty"`
}
```

### 3. WorkflowResponse 新增 `ShowForm` 信号

```go
// corelib/workflow/types.go — WorkflowResponse

type WorkflowResponse struct {
    // ... existing fields ...

    // ShowForm is true when the engine wants the caller to emit an AG UI form
    // for structured information collection. The caller should build the form
    // from the current phase's InputSchema and emit it via emitAgentView.
    ShowForm bool

    // FormSchema is the phase's InputSchema, provided when ShowForm=true.
    // The caller converts this to an AgentView map and emits it.
    FormSchema *PhaseInputSchema
}
```

## 引擎层改动

### HandleInput 逻辑扩展

在 `HandleInput` 的 default 分支（正常阶段输入）中，检查当前阶段是否有 `InputSchema`：

```go
// corelib/workflow/engine.go — HandleInput default branch

// Check if this phase requires structured form input.
if phase.InputSchema != nil && ws.PhaseFormData == nil {
    // Phase has a form schema but user hasn't submitted form data yet.
    // Signal the caller to show the form.
    return &WorkflowResponse{
        ShowForm:     true,
        FormSchema:   phase.InputSchema,
        RunAgentLoop: false,
    }, nil
}

// Normal execution: build phase prompt (with form data if available).
phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
```

### 新增 SubmitPhaseForm 方法

```go
// corelib/workflow/engine.go

// SubmitPhaseForm receives the user's form submission for the current phase.
// It stores the form data and triggers the agent loop with the form context
// injected into the PhasePrompt.
func (e *WorkflowEngine) SubmitPhaseForm(userID string, formData map[string]interface{}) (*WorkflowResponse, error) {
    e.mu.Lock()
    defer e.mu.Unlock()

    ws := e.workflows[userID]
    if ws == nil || ws.Status != WorkflowActive {
        return nil, fmt.Errorf("no active workflow for user %s", userID)
    }
    tmpl := e.registry.Match(ws.Type)
    if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
        return nil, fmt.Errorf("workflow template or phase index is invalid")
    }
    phase := &tmpl.Phases[ws.PhaseIndex]

    // Store form data.
    ws.PhaseFormData = formData
    ws.UpdatedAt = time.Now()
    if e.store != nil {
        _ = e.store.SaveWorkflowState(ws)
    }

    // Build phase prompt with form data injected.
    phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)

    return &WorkflowResponse{
        PhasePrompt:  phasePrompt,
        ToolFilter:   phase.ToolPolicy,
        RunAgentLoop: true,
    }, nil
}
```

### BuildPhaseSystemPrompt 注入表单数据

```go
// corelib/workflow/prompt_builder.go — BuildPhaseSystemPrompt 增强

// 在构建 phase prompt 时，如果 ws.PhaseFormData 非空，将其格式化为结构化上下文注入：
if len(ws.PhaseFormData) > 0 && phase.InputSchema != nil {
    sb.WriteString("\n\n## 用户提供的结构化信息\n\n")
    for _, field := range phase.InputSchema.Fields {
        if val, ok := ws.PhaseFormData[field.Name]; ok && val != nil {
            sb.WriteString(fmt.Sprintf("- **%s**：%v\n", field.Label, val))
        }
    }
    sb.WriteString("\n请基于以上信息生成本阶段的交付物。\n")
}
```

## GUI 层改动

### 1. 新增 AgentView ID Kind

```go
// gui/mis_agent_view_id_kind.go

const (
    // ... existing kinds ...
    misAgentViewIDWorkflowForm  // 新增
)

// classifyMISAgentViewID 新增匹配：
// case strings.HasPrefix(trimmed, "workflow:form:"):
//     return misAgentViewID{Kind: misAgentViewIDWorkflowForm, Arg: strings.TrimPrefix(trimmed, "workflow:form:")}
```

### 2. handleActiveWorkflow 处理 ShowForm 信号

```go
// gui/im_message_handler_workflow.go — handleActiveWorkflow

// 在 HandleInput 返回后，检查 ShowForm 信号：
if resp.ShowForm && resp.FormSchema != nil {
    // Emit AG UI form for structured information collection.
    h.emitWorkflowPhaseForm(engine, userID, resp.FormSchema)
    return &IMAgentResponse{Text: "📋 请在右侧面板中填写信息后提交。"}
}
```

### 3. emitWorkflowPhaseForm 构建并发射 AgentView

```go
// gui/im_workflow_form.go (新文件)

func (h *IMMessageHandler) emitWorkflowPhaseForm(engine *workflow.WorkflowEngine, userID string, schema *workflow.PhaseInputSchema) {
    fields := make([]map[string]interface{}, 0, len(schema.Fields))
    for _, f := range schema.Fields {
        field := map[string]interface{}{
            "name":  f.Name,
            "label": f.Label,
            "type":  f.Type,
        }
        if f.Required {
            field["required"] = true
        }
        if f.Description != "" {
            field["description"] = f.Description
        }
        if f.Placeholder != "" {
            field["placeholder"] = f.Placeholder
        }
        if len(f.Options) > 0 {
            opts := make([]map[string]string, len(f.Options))
            for i, o := range f.Options {
                opts[i] = map[string]string{"label": o.Label, "value": o.Value}
            }
            field["options"] = opts
        }
        if f.Default != nil {
            field["value"] = f.Default
        }
        if f.Min != nil {
            field["min"] = *f.Min
        }
        if f.Max != nil {
            field["max"] = *f.Max
        }
        if f.MinLength != nil {
            field["minLength"] = *f.MinLength
        }
        if f.MaxLength != nil {
            field["maxLength"] = *f.MaxLength
        }
        if f.Pattern != "" {
            field["pattern"] = f.Pattern
        }
        fields = append(fields, field)
    }

    // Add hidden field for workflow context.
    ws := engine.GetActiveWorkflow(userID)
    phaseID := ""
    if ws != nil {
        phaseID = ws.CurrentPhase
    }
    fields = append(fields, map[string]interface{}{
        "name":  "_workflow_phase",
        "type":  "hidden",
        "value": phaseID,
    })

    view := map[string]interface{}{
        "type":        "form",
        "id":          "workflow:form:" + phaseID,
        "title":       schema.Title,
        "description": schema.Description,
        "fields":      fields,
        "submitLabel": "提交",
    }
    h.app.emitAgentView(view)
}
```

### 4. handleAgentViewSubmitPayload 新增路由

```go
// gui/mis_data_tool.go — handleAgentViewSubmitPayload switch

case misAgentViewIDWorkflowForm:
    return a.handleWorkflowFormSubmit(viewID.Arg, payload.Data)
```

### 5. handleWorkflowFormSubmit 实现

```go
// gui/im_workflow_form.go

func (a *App) handleWorkflowFormSubmit(phaseID string, data map[string]interface{}) *IMAgentResponse {
    hubClient := a.ensureHubClient()
    if hubClient == nil {
        return &IMAgentResponse{Text: "AI assistant not initialized.", Error: "missing hub client"}
    }
    handler := hubClient.ensureIMHandler()
    engine := handler.getWorkflowEngine()
    if engine == nil {
        return &IMAgentResponse{Text: "Workflow engine not available.", Error: "no engine"}
    }

    userID := handler.lastUserID
    ws := engine.GetActiveWorkflow(userID)
    if ws == nil || ws.CurrentPhase != phaseID {
        return &IMAgentResponse{Text: "工作流状态已变更，请重新操作。", Error: "phase mismatch"}
    }

    // Remove hidden fields from form data.
    cleanData := make(map[string]interface{}, len(data))
    for k, v := range data {
        if !strings.HasPrefix(k, "_") {
            cleanData[k] = v
        }
    }

    // Submit form data to engine.
    resp, err := engine.SubmitPhaseForm(userID, cleanData)
    if err != nil {
        return &IMAgentResponse{Text: "表单提交失败。", Error: err.Error()}
    }

    // Clear the AG UI form.
    a.clearAgentView("workflow:form:" + phaseID)

    // Trigger agent loop with form data injected into phase prompt.
    if resp.RunAgentLoop && resp.PhasePrompt != "" {
        handler.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
        handler.workflowAgentLoopMarker.Store(userID, true)
        // Return nil to let the agent loop run.
        return nil
    }

    return &IMAgentResponse{Text: resp.Text}
}
```

## 模板示例

### coding 模板 requirements 阶段

```go
{
    ID:           "requirements",
    Name:         "需求分析",
    InputSchema: &PhaseInputSchema{
        Title:       "项目信息",
        Description: "请填写以下信息，帮助生成更准确的需求文档",
        Fields: []PhaseInputField{
            {Name: "project_name", Label: "项目名称", Type: "text", Required: true, Placeholder: "如：贪吃蛇游戏"},
            {Name: "tech_stack", Label: "技术栈", Type: "select", Required: true, Options: []PhaseInputOption{
                {Label: "C/C++", Value: "cpp"},
                {Label: "Python", Value: "python"},
                {Label: "Go", Value: "go"},
                {Label: "JavaScript/TypeScript", Value: "js"},
                {Label: "Java", Value: "java"},
                {Label: "Rust", Value: "rust"},
                {Label: "其他", Value: "other"},
            }},
            {Name: "platform", Label: "目标平台", Type: "multiselect", Options: []PhaseInputOption{
                {Label: "Windows", Value: "windows"},
                {Label: "macOS", Value: "macos"},
                {Label: "Linux", Value: "linux"},
                {Label: "Web 浏览器", Value: "web"},
                {Label: "移动端", Value: "mobile"},
            }},
            {Name: "build_tool", Label: "构建工具", Type: "select", Options: []PhaseInputOption{
                {Label: "自动选择", Value: "auto"},
                {Label: "CMake", Value: "cmake"},
                {Label: "Makefile", Value: "makefile"},
                {Label: "npm/yarn", Value: "npm"},
                {Label: "Gradle", Value: "gradle"},
                {Label: "Cargo", Value: "cargo"},
            }},
            {Name: "description", Label: "功能描述", Type: "textarea", Required: true, Placeholder: "描述你想要的功能、玩法、界面等..."},
            {Name: "constraints", Label: "特殊要求", Type: "textarea", Placeholder: "性能要求、依赖限制、UI 风格等（可选）"},
            {Name: "project_path", Label: "项目目录", Type: "directory", Placeholder: "如：D:\\workprj\\my-game（可选，默认当前目录）"},
        },
    },
    // ... rest of phase config unchanged ...
}
```

### presentation_design 模板 audience_goal 阶段

```go
{
    ID:           "audience_goal",
    Name:         "受众与目标",
    InputSchema: &PhaseInputSchema{
        Title:       "PPT 基本信息",
        Description: "请填写演示文稿的基本信息",
        Fields: []PhaseInputField{
            {Name: "topic", Label: "演讲主题", Type: "text", Required: true, Placeholder: "如：Q3 产品发布会"},
            {Name: "audience", Label: "目标受众", Type: "select", Required: true, Options: []PhaseInputOption{
                {Label: "公司内部团队", Value: "internal"},
                {Label: "客户/合作伙伴", Value: "client"},
                {Label: "投资人", Value: "investor"},
                {Label: "学术会议", Value: "academic"},
                {Label: "公众/媒体", Value: "public"},
                {Label: "其他", Value: "other"},
            }},
            {Name: "duration", Label: "演讲时长", Type: "select", Options: []PhaseInputOption{
                {Label: "5 分钟（闪电演讲）", Value: "5min"},
                {Label: "15 分钟", Value: "15min"},
                {Label: "30 分钟", Value: "30min"},
                {Label: "45-60 分钟", Value: "60min"},
            }},
            {Name: "slide_count", Label: "页数", Type: "number", Min: ptrFloat(5), Max: ptrFloat(100), Placeholder: "建议 10-30 页"},
            {Name: "style", Label: "风格偏好", Type: "select", Options: []PhaseInputOption{
                {Label: "商务简约", Value: "business"},
                {Label: "科技感", Value: "tech"},
                {Label: "学术严谨", Value: "academic"},
                {Label: "创意活泼", Value: "creative"},
                {Label: "不限", Value: "any"},
            }},
            {Name: "key_points", Label: "核心要点", Type: "textarea", Required: true, Placeholder: "列出 3-5 个必须覆盖的核心内容点"},
            {Name: "reference_files", Label: "参考资料", Type: "file", Description: "上传相关文档、数据、图片等（可选）"},
        },
    },
}
```

## IM 通道降级方案

IM 通道（飞书/微信/QQ）无法渲染 `AgentTaskPanel`，降级为结构化文本引导：

```go
// gui/im_workflow_form.go

func (h *IMMessageHandler) buildIMFormGuidanceText(schema *workflow.PhaseInputSchema) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("📋 %s\n\n", schema.Title))
    if schema.Description != "" {
        sb.WriteString(schema.Description + "\n\n")
    }
    sb.WriteString("请依次提供以下信息（带 * 为必填）：\n\n")
    for i, f := range schema.Fields {
        prefix := " "
        if f.Required {
            prefix = "*"
        }
        sb.WriteString(fmt.Sprintf("%s%d. %s", prefix, i+1, f.Label))
        if len(f.Options) > 0 {
            labels := make([]string, len(f.Options))
            for j, o := range f.Options {
                labels[j] = o.Label
            }
            sb.WriteString("（选择：" + strings.Join(labels, " / ") + "）")
        }
        sb.WriteString("\n")
    }
    sb.WriteString("\n请按编号回复，如：\n1. 贪吃蛇游戏\n2. C++\n...")
    return sb.String()
}
```

IM 通道检测到 `ShowForm=true` 时，发送引导文本而非 emit AgentView。用户回复后，LLM 解析结构化回复提取字段值（复用 `resolveIntentLocalFallback` 的 `inferLocalAgentViewFieldsFromQuery` 思路）。

## 用户体验流程

### 桌面面板

```
用户: "开发一个游戏"
  → 工作流启动 → 进入 requirements 阶段
  → HandleInput 检测到 InputSchema 非空 + PhaseFormData 为空
  → 返回 ShowForm=true
  → handleActiveWorkflow 调用 emitWorkflowPhaseForm
  → 右侧面板弹出表单（项目名称、技术栈、平台、功能描述...）
  → 用户填写表单 → 点击"提交"
  → SubmitAgentView → handleWorkflowFormSubmit
  → engine.SubmitPhaseForm 存储 formData
  → BuildPhaseSystemPrompt 注入结构化信息
  → Agent loop 运行 → LLM 基于结构化输入生成需求文档
  → 需求文档显示在右侧预览面板
  → 等待用户确认（NeedsConfirm gate）
```

### IM 通道

```
用户: "开发一个游戏"
  → 工作流启动 → 进入 requirements 阶段
  → HandleInput 检测到 InputSchema 非空
  → 返回 ShowForm=true
  → 检测到 platform=IM → 发送结构化文本引导
  → 用户按编号回复
  → LLM 解析回复提取字段值
  → 注入 PhasePrompt → 生成需求文档 PDF
```

### 跳过表单（可选）

用户可以在表单面板中点击"跳过"（dismiss），系统回退到自然语言交互模式：

```go
// DismissAgentView 处理 workflow:form:* 的 dismiss
// → 清除 InputSchema 的 ShowForm 状态
// → 正常运行 agent loop（无 form data，LLM 自由对话收集信息）
```

## 阶段推进时清理 FormData

```go
// advancePhase 中清理上一阶段的 form data
ws.PhaseFormData = nil
```

## 与 LLM 预填的结合（Phase 3 智能化）

用户首条消息中可能已包含部分信息（如"开发一个 C++ 贪吃蛇游戏"）。可以在 emit form 前，用 LLM 从用户消息中提取字段值作为 `defaultValue` 预填到表单中：

```go
// 未来增强：从用户消息中预填表单
prefilled := h.prefillFormFromUserMessage(text, schema)
for _, field := range fields {
    if val, ok := prefilled[field["name"].(string)]; ok {
        field["value"] = val
    }
}
```

## 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/workflow/types.go` | 修改 | 新增 `PhaseInputField`/`PhaseInputOption`/`PhaseInputSchema` 类型；`PhaseTemplate` 新增 `InputSchema` 字段；`WorkflowState` 新增 `PhaseFormData` 字段；`WorkflowResponse` 新增 `ShowForm`/`FormSchema` 字段 |
| `corelib/workflow/engine.go` | 修改 | `HandleInput` default 分支新增 InputSchema 检查；新增 `SubmitPhaseForm` 方法；`advancePhase` 清理 `PhaseFormData` |
| `corelib/workflow/prompt_builder.go` | 修改 | `BuildPhaseSystemPrompt` 注入 `PhaseFormData` 结构化信息 |
| `corelib/workflow/templates.go` | 修改 | 2-3 个模板添加 `InputSchema`（coding、presentation_design、product_design） |
| `gui/mis_agent_view_id_kind.go` | 修改 | 新增 `misAgentViewIDWorkflowForm` kind + `"workflow:form:"` 前缀匹配 |
| `gui/mis_data_tool.go` | 修改 | `handleAgentViewSubmitPayload` switch 新增 `misAgentViewIDWorkflowForm` case |
| `gui/im_workflow_form.go` | 新增 | `emitWorkflowPhaseForm`、`handleWorkflowFormSubmit`、`buildIMFormGuidanceText` |
| `gui/im_message_handler_workflow.go` | 修改 | `handleActiveWorkflow` 处理 `ShowForm` 信号；`handlePostStartWorkflow` 处理首阶段 form |

## 前端改动

**零改动**。`AgentTaskPanel` 已经能渲染任何 `AgentView type="form"`，包括：
- text/textarea/number/date 输入框
- select/multiselect 下拉选择
- required 必填校验（红色星号 + 提交时校验）
- description/placeholder 提示文本
- min/max/minLength/maxLength/pattern 校验
- file 文件上传
- hidden 隐藏字段

前端通过 `agent-view:lifecycle` 事件接收表单，通过 `SubmitAgentView` binding 提交数据。这些都是已有的标准协议。

## 已实现的模板覆盖（13/22）

| # | 模板 | Schema 函数 | 字段数 |
|---|------|------------|--------|
| 1 | coding（编程开发）| `codingRequirementsInputSchema()` | 7 |
| 2 | product_design（产品设计）| `productDesignInputSchema()` | 6 |
| 3 | innovation（创新制定）| `innovationInputSchema()` | 5 |
| 4 | business_plan（商业计划书）| `businessPlanInputSchema()` | 7 |
| 5 | testing（测试方案）| `testingInputSchema()` | 5 |
| 6 | literature_review（论文综述）| `literatureReviewInputSchema()` | 5 |
| 7 | research_report（研报收集）| `researchReportInputSchema()` | 5 |
| 8 | experiment_design（实验设计）| `experimentDesignInputSchema()` | 5 |
| 9 | grant_proposal（基金申请）| `grantProposalInputSchema()` | 6 |
| 10 | project_proposal（项目立项）| `projectProposalInputSchema()` | 6 |
| 11 | event_planning（活动策划）| `eventPlanningInputSchema()` | 6 |
| 12 | competitive_analysis（竞品分析）| `competitiveAnalysisInputSchema()` | 4 |
| 13 | presentation_design（PPT 设计）| `presentationDesignInputSchema()` | 7 |

### 未添加 InputSchema 的模板（9 个）

| 模板 | 原因 |
|------|------|
| bid_response（招投标）| 输入驱动型，已有 RequiresInput 机制 |
| contract_review（合同审查）| 输入驱动型，已有 RequiresInput 机制 |
| due_diligence（尽职调查）| 输入驱动型，已有 RequiresInput 机制 |
| compliance_audit（合规审计）| 输入驱动型，已有 RequiresInput 机制 |
| patent_analysis（专利分析）| 输入驱动型，已有 RequiresInput 机制 |
| paper_writing（学术论文）| 首阶段是大纲构思，信息来源是用户已有研究数据 |
| ops_maintenance（运维）| 特殊模板，首阶段需要用户描述具体操作 |
| changjiang_scholar | 特殊模板 |
| changjiang_scholar_review | 特殊模板 |

## 验收标准

- coding 模板 requirements 阶段：右侧面板弹出项目信息表单，必填字段有红色星号
- 用户填写表单并提交 → LLM 收到结构化信息 → 生成的需求文档包含表单中的所有信息
- 用户点击"跳过"（dismiss）→ 回退到自然语言交互模式
- IM 通道 → 发送结构化文本引导，不弹表单
- 阶段推进后 PhaseFormData 被清理
- 所有现有 workflow 测试通过
- 13 个模板的首阶段均弹出对应的结构化表单
