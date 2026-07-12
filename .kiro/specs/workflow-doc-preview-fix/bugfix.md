# Bugfix Requirements Document

## Introduction

MaClaw 的编程工作流（coding workflow）有两条执行路径：

1. **工作流引擎路径**：通过 `handleWorkflowInterception` → `IntentUnderstandingManager` → `WorkflowEngine.StartWorkflow` 启动，引擎管理阶段状态、文档存储和前端事件发射。
2. **Steering 驱动路径**：通过 `coding-workflow.md` steering 规则驱动 LLM 在正常 agent loop（`runAgentLoop`）中按三阶段流程（需求文档→技术设计→任务拆分）生成文档。

三个 UI 功能（全屏建议横幅、右侧文档预览面板、聊天区可点击文件链接）的前端组件和后端事件发射机制均已实现，但它们的触发逻辑全部绑定在工作流引擎路径上。当编程任务走 steering 驱动路径时，`WorkflowEngine` 没有为该用户启动 workflow，导致：

- `EmitSuggestMaximize` 从未被调用（仅在 `handleActiveUnderstanding` → `StartWorkflow` 后调用）
- `SavePhaseOutput` 返回空字符串（检查 `e.workflows[userID]` 为 nil），`EmitDocUpdate` 不触发
- 聊天区没有将生成的文档文件渲染为可点击链接

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN 用户发送编程任务消息且该任务通过 steering 驱动的 agent loop 处理（而非工作流引擎拦截） THEN 系统不显示全屏建议横幅（"即将进入「编程」流程，全屏模式体验更佳"），因为 `EmitSuggestMaximize` 仅在 `handleActiveUnderstanding` → `StartWorkflow` 路径中被调用

1.2 WHEN LLM 在 steering 驱动的 agent loop 中生成阶段文档（需求文档/技术设计/任务拆分） THEN 右侧文档预览面板不显示，因为 `SavePhaseOutput` 检查 `e.workflows[userID]` 为 nil 返回空字符串，`EmitDocUpdate` 不被调用

1.3 WHEN LLM 在 steering 驱动的 agent loop 中通过 `write_file` 或 `generate_pdf` 工具生成文档文件 THEN 聊天区不显示可点击的文件名链接，用户无法在右侧面板中查看对应文档

### Expected Behavior (Correct)

2.1 WHEN LLM 在 agent loop 中识别到编程任务并开始三阶段工作流（通过 steering 规则驱动） THEN 系统 SHALL 向前端发射 `workflow:suggest_maximize` 事件，前端显示全屏建议横幅

2.2 WHEN LLM 在 steering 驱动的 agent loop 中生成阶段文档内容 THEN 系统 SHALL 向前端发射 `workflow:doc_update` 事件，右侧文档预览面板显示对应阶段的 Markdown 内容

2.3 WHEN LLM 在 steering 驱动的 agent loop 中通过工具生成文档文件 THEN 聊天区 SHALL 显示可点击的文件名链接，点击后在右侧面板中查看对应文档内容

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 用户的编程任务通过工作流引擎路径处理（`handleWorkflowInterception` 拦截成功） THEN 系统 SHALL CONTINUE TO 正常显示全屏建议横幅、右侧文档预览和阶段切换功能

3.2 WHEN 用户发送非编程任务消息（内容处理、闲聊等） THEN 系统 SHALL CONTINUE TO 不触发全屏建议横幅和文档预览面板

3.3 WHEN 用户在工作流引擎路径中使用阶段确认、质量门禁等功能 THEN 系统 SHALL CONTINUE TO 正常工作，`SavePhaseOutput` 和 `EmitGateResult` 行为不变

3.4 WHEN 前端收到 `workflow:doc_update` 事件且用户已手动关闭文档预览面板 THEN 系统 SHALL CONTINUE TO 尊重用户的关闭操作（`userClosedRef` 机制不变）

3.5 WHEN 后台消息（`msg.IsBackground == true`）在 agent loop 中生成文本 THEN 系统 SHALL CONTINUE TO 不触发文档预览更新（现有 `!msg.IsBackground` 守卫不变）
