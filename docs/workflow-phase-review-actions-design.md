# 工作流阶段确认按钮设计方案

## 概述

工作流阶段完成后，在 LLM 输出的文档末尾自动附带结构化确认按钮，让用户一键确认推进或聚焦输入补充意见，同时保留自由输入框的现有方式。

## UI 效果图（ASCII）

### 阶段文档输出完成后的消息样式

```
┌─────────────────────────────────────────────────────────────┐
│ [Assistant 消息内容]                                          │
│                                                              │
│ # 需求文档                                                   │
│ ## 功能需求                                                  │
│ 1. 用户注册/登录...                                          │
│ 2. 数据展示...                                               │
│ ...（完整文档内容，同时显示在右侧预览面板）                      │
│                                                              │
│ ─────────────────────────────────────────────────────────── │
│ 📋 请查看文档，确认后进入下一阶段。                              │
│                                                              │
│ ┌──────────────┐  ┌──────────────────┐       ┌────────┐     │
│ │ ✅ 确认并推进 │  │ ✏️ 输入补充/修改意见 │       │ 🚫 中止 │     │
│ └──────────────┘  └──────────────────┘       └────────┘     │
│    (primary)          (secondary)             (danger,小字)  │
└─────────────────────────────────────────────────────────────┘

[ 输入框仍然正常可用，用户可以直接打字 ]
```

**"🚫 中止"按钮视觉设计**：
- Style: `"danger"`（红色文字，透明背景，红色边框）
- 字号与其他按钮一致但视觉权重最低（不抢眼）
- 放在最右侧，与前两个按钮有额外间距（通过 flex gap 或 margin-left: auto 实现）

### 按钮点击后的视觉反馈

```
用户点击 "✅ 确认并推进" 后：

┌──────────────┐  ┌──────────────────┐       ┌────────┐
│ ✅ 确认并推进 │  │ ✏️ 输入补充/修改意见 │       │ 🚫 中止 │
└──────────────┘  └──────────────────┘       └────────┘
  (opacity: 0.7)    (opacity: 0.4)            (opacity: 0.4)
  ↑ 点击的按钮       ↑ disabled                ↑ disabled

同时在聊天区显示用户消息气泡：
> ✅ 确认并推进
```

### 用户点击 "🚫 中止" 后的交互流程

```
1. 用户点击 "🚫 中止" 按钮
   ↓
2. 前端弹出确认对话框（浏览器原生 confirm 或自定义 modal）：
   ┌──────────────────────────────────────────┐
   │ ⚠️ 确定要中止当前工作流吗？                  │
   │                                          │
   │ 中止后当前进度将被清除，无法继续，            │
   │ 只能重新发起。                              │
   │                                          │
   │         [ 取消 ]    [ 确定中止 ]            │
   └──────────────────────────────────────────┘
   ↓
3a. 用户点 "取消" → 对话框关闭，什么都不发生，按钮恢复可点
3b. 用户点 "确定中止" → 
   → 所有按钮 disabled
   → 发送 "__wf_review__ abort" 到后端
   → 后端取消工作流 + 清理环境
   → 返回 "⏹️ 工作流已中止。如需重新开始，请直接描述您的任务。"
```

### 用户点击 "✏️ 输入补充/修改意见" 后

```
按钮全部变为 disabled 状态
输入框获得焦点，placeholder 变为："请输入您的修改意见或补充内容..."
用户打字后按回车发送 → 走现有 handleWorkflowReview → LLM 分类为 supplement
```

### 用户点击 "🚫 中止" 后

```
[前端] 弹出 confirm 对话框："确定要中止？中止后无法继续，只能重新发起。"
  → 用户点"取消" → 什么都不发生
  → 用户点"确定中止"
    → disableActionsForCommand
    → sendMessage("__wf_review__ abort", {uiAction: true, displayText: "🚫 中止工作流"})

[后端] 收到 "__wf_review__ abort"
  → handleActiveWorkflow → engine.HandleInput → PendingReview=true → handleWorkflowReview
  → detectWorkflowReviewIntentFast("__wf_review__ abort") → ReviewIntentCancel
  → applyWorkflowReviewIntent → cancelWorkflowV2(userID) + 清理环境
  → 返回 "⏹️ 工作流已中止。如需重新开始，请直接描述您的任务。"
```

## 数据流

### 完整链路

```
Agent Loop 完成
  ↓
schedulePostLoopSideEffects (同步)
  ↓
captureWorkflowDocAfterAgentLoop
  ↓
recordWorkflowV2Output → machine.RecordOutput → phase.Status = WaitingConfirm
  ↓
emitWorkflowV2Progress + emitDocUpdateV2 → 前端预览面板更新
  ↓
回到 finalizeIMAgentLoopResponse
  ↓
★ 新增：appendWorkflowReviewActions(resp, userID, loopCtx)
  → 检测 V2 workflow IsWaitingConfirm
  → 附加 resp.Actions = [确认, 补充修改, 跳过]
  → 附加 resp.Text += "\n\n📋 请查看文档，确认后进入下一阶段。"
  ↓
返回 resp 给前端
  ↓
前端渲染消息 + ActionButtons 组件
```

### 用户点击按钮后

```
[前端] 用户点击 "✅ 确认并推进"
  → executeAction("__wf_review__ confirm")
  → disableActionsForCommand（所有按钮 disabled）
  → sendMessage("__wf_review__ confirm", {uiAction: true, displayText: "✅ 确认并推进"})

[后端] 收到消息 "__wf_review__ confirm"
  → handleActiveWorkflow → engine.HandleInput → PendingReview=true
  → handleWorkflowReview(text="__wf_review__ confirm")
  → detectWorkflowReviewIntentFast 匹配 __wf_review__ 前缀 → ReviewIntentConfirm
  → applyWorkflowReviewIntent → engine 推进到下一阶段
  → 返回 RunAgentLoop=true + 新 PhasePrompt → 开始下一阶段
```

```
[前端] 用户点击 "✏️ 输入补充/修改意见"
  → executeAction("__wf_review__ supplement_focus")
  → disableActionsForCommand（所有按钮 disabled）
  → 不发送消息！仅聚焦输入框 + 更新 placeholder
  → 用户打字，如 "加一个暗黑模式的需求"
  → sendMessage("加一个暗黑模式的需求")

[后端] 收到消息 "加一个暗黑模式的需求"
  → handleActiveWorkflow → engine.HandleInput → PendingReview=true
  → handleWorkflowReview(text="加一个暗黑模式的需求")
  → detectWorkflowReviewIntentFast 不匹配
  → LLM 分类器 → "supplement"
  → applyWorkflowReviewIntent → engine 留在当前阶段 + modifyHint
  → 返回 RunAgentLoop=true + PhasePrompt + modifyHint → 重新生成文档
```

```
[前端/后端] 用户直接在输入框打字 "确认" 或 "加个登录功能"
  → 与现在的行为完全一致
  → 按钮保持可见但用户忽略了它们
  → 后端 handleWorkflowReview 正常工作（fast-path 或 LLM 分类）
```

## 后端改动详细

### 文件 1: `gui/im_post_loop.go`

在 `finalizeIMAgentLoopResponse` 中，`schedulePostLoopSideEffects` 之后、返回 resp 前，新增 review actions 附加逻辑：

```go
func (h *IMMessageHandler) finalizeIMAgentLoopResponse(...) *IMAgentResponse {
    // ... 现有逻辑 ...
    
    h.schedulePostLoopSideEffects(msg, loopCtx, resp, workflowAgentLoop)
    
    // ★ 新增：V2 工作流 doc phase 完成后附带确认按钮
    if workflowAgentLoop && loopCtx != nil && loopCtx.WorkflowDocPhase {
        h.appendWorkflowReviewActions(resp, msg.UserID, loopCtx)
    }
    
    h.maybeAttachVoiceSummary(resp, msg.Platform, isVoiceInputMessage(msg))
    return resp
}
```

### 文件 2: `gui/workflow_v2_review_actions.go`（新文件）

```go
package main

// appendWorkflowReviewActions attaches structured confirmation buttons
// to the agent loop response when a V2 workflow doc phase completes
// and transitions to WaitingConfirm state.
//
// 机制：引擎层直接注入按钮，不依赖 LLM 的 ask_user 调用。
// 确认动作是确定性的（fast-path 路由），零 LLM 调用延迟。
func (h *IMMessageHandler) appendWorkflowReviewActions(resp *IMAgentResponse, userID string, loopCtx *LoopContext) {
    if resp == nil {
        return
    }
    
    wf := h.getWorkflowV2()
    if wf == nil {
        return
    }
    
    ownerID := h.workflowPolicyOwnerID(userID, loopCtx)
    state := wf.machine.GetActive(ownerID)
    if state == nil || !state.IsWaitingConfirm() {
        return
    }
    
    phase := state.ActivePhase()
    if phase == nil {
        return
    }
    
    // 构建按钮列表：确认 + 补充修改 + 中止
    // 中止按钮 danger 样式（红色，不显眼），放在最后
    resp.Actions = []IMResponseAction{
        {Label: "✅ 确认并推进", Command: "__wf_review__ confirm", Style: "primary"},
        {Label: "✏️ 输入补充/修改意见", Command: "__wf_review__ supplement_focus", Style: "secondary"},
        {Label: "🚫 中止", Command: "__wf_review__ abort", Style: "danger"},
    }
    
    // 附加提示文本（如果 resp.Text 不为空，追加分隔线）
    hint := "📋 请查看文档，确认后进入下一阶段。也可以直接在输入框中输入修改意见。"
    if resp.Text != "" {
        resp.Text += "\n\n---\n\n" + hint
    } else {
        resp.Text = hint
    }
}
```

### 文件 3: `gui/im_message_handler_workflow.go`

修改 `detectWorkflowReviewIntentFast`，新增 `__wf_review__` 前缀匹配：

```go
func detectWorkflowReviewIntentFast(text string) (v2.ReviewIntent, bool) {
    trimmed := strings.ToLower(strings.TrimSpace(text))
    trimmed = strings.Trim(trimmed, " \t\r\n.。！!？?")
    if trimmed == "" {
        return v2.ReviewIntentOther, false
    }
    
    // ★ 新增：结构化按钮命令，确定性路由，不调 LLM
    if strings.HasPrefix(trimmed, "__wf_review__ ") {
        action := strings.TrimPrefix(trimmed, "__wf_review__ ")
        switch action {
        case "confirm":
            return v2.ReviewIntentConfirm, true
        case "abort":
            return v2.ReviewIntentCancel, true
        // supplement_focus 是纯前端行为，不发送到后端
        // 但防御性处理：万一发送了，视为 other 让用户继续输入
        case "supplement_focus":
            return v2.ReviewIntentOther, false
        }
    }
    
    // ... 现有的关键词匹配逻辑不变 ...
}
```

## 前端改动详细

### 文件 1: `gui/frontend/src/components/ai/useAIAssistant.ts`

在 `executeAction` 的 pattern matching 中新增 `__wf_review__` 处理：

```typescript
const executeAction = useCallback(async (command: string) => {
    // ... 现有的 pattern matching ...
    
    // ★ 新增：工作流阶段确认按钮
    const wfReviewMatch = command.match(/^__wf_review__\s+(\S+)$/);
    if (wfReviewMatch) {
        const action = wfReviewMatch[1];
        
        // "补充修改意见"：纯前端行为，聚焦输入框
        if (action === 'supplement_focus') {
            setMessages(prev => disableActionsForCommand(prev, command));
            if (focusInputRef?.current) {
                focusInputRef.current();
            }
            return;
        }
        
        // "中止"：需要二次确认对话框
        if (action === 'abort') {
            const confirmMsg = localizeText(uiLang,
                "Are you sure you want to abort the current workflow?\n\nAll progress will be lost and you'll need to start over.",
                "确定要中止当前工作流吗？\n\n中止后当前进度将被清除，无法继续，只能重新发起。",
                "確定要中止當前工作流嗎？\n\n中止後當前進度將被清除，無法繼續，只能重新發起。"
            );
            if (!window.confirm(confirmMsg)) {
                return; // 用户取消，按钮恢复可点
            }
            setMessages(prev => disableActionsForCommand(prev, command));
            const displayText = localizeText(uiLang, "🚫 Abort workflow", "🚫 中止工作流", "🚫 中止工作流");
            return sendMessage(command, { uiAction: true, displayText });
        }
        
        // confirm：发送回后端
        const displayLabels: Record<string, string> = {
            confirm: localizeText(uiLang, "✅ Confirm and proceed", "✅ 确认并推进", "✅ 確認並推進"),
        };
        setMessages(prev => disableActionsForCommand(prev, command));
        return sendMessage(command, {
            uiAction: true,
            displayText: displayLabels[action] || command,
        });
    }
    
    // ... 现有的 generic fallback ...
}, [...]);
```

### 文件 2: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`（可选增强）

无需改动核心渲染逻辑——`ActionButtons` 组件已经通用化。

可选增强：当 `supplement_focus` 被触发时，设置输入框 placeholder 为"请输入您的修改意见或补充内容..."。通过一个 state 变量控制：

```typescript
const [reviewInputPlaceholder, setReviewInputPlaceholder] = useState<string | null>(null);

// 在 executeAction 的 supplement_focus 分支中：
setReviewInputPlaceholder(
    localizeText(uiLang, 
        "Enter your feedback or modifications...",
        "请输入您的修改意见或补充内容...",
        "請輸入您的修改意見或補充內容..."
    )
);

// 输入框 placeholder 属性：
placeholder={reviewInputPlaceholder || defaultPlaceholder}

// 用户发送消息后清除：
// 在 sendMessage 成功回调中
setReviewInputPlaceholder(null);
```

## IM 通道（飞书/微信/QQ）兼容

IM 网关已有 `Actions → 交互式卡片按钮` 的通用支持。`IMAgentResponse.Actions` 中的按钮会被 IM Gateway 渲染为：

- **飞书**：Interactive Card 的 Button 组件
- **微信/QQ**：文本按钮（带编号，用户回复编号选择）

用户点击后发送按钮 Command 文本（如 `__wf_review__ confirm`），后端 `detectWorkflowReviewIntentFast` fast-path 匹配，行为一致。

## 边界情况处理

| 场景 | 处理 |
|------|------|
| 用户不点按钮，直接打字"确认" | 走现有 fast-path → ReviewIntentConfirm |
| 用户不点按钮，直接打字"加个登录功能" | 走现有 LLM 分类 → supplement |
| 用户点"补充修改"后不输入就切 tab | 按钮已 disabled，下次回来输入框仍可用 |
| NeedsConfirm=false 的执行阶段 | `loopCtx.WorkflowDocPhase` 为 false → 不附加按钮 |
| 用户想中止工作流 | 点击"🚫 中止" → 二次确认 → 后端 cancelWorkflowV2 + 清理环境 |
| 多 tab 隔离 | Actions 附在 resp 中，通过 SessionKey 路由到正确 tab |
| Agent loop 产出空文档（hard exit） | `recordWorkflowV2Output` 不设 WaitingConfirm → `appendWorkflowReviewActions` 检测到非 WaitingConfirm 状态 → 不附加按钮 |
| 用户发送附件（图片/文件）| 正常走 handleActiveWorkflow → 引擎检测 WaitingConfirm → handleWorkflowReview |

## 改动量评估

| 文件 | 改动类型 | 行数 |
|------|---------|------|
| `gui/workflow_v2_review_actions.go` | 新增 | ~50 行 |
| `gui/im_post_loop.go` | 修改 | +5 行（调用点） |
| `gui/im_message_handler_workflow.go` | 修改 | +12 行（fast-path 识别） |
| `gui/frontend/.../useAIAssistant.ts` | 修改 | +25 行（executeAction 分支） |
| `gui/frontend/.../AIAssistantPanel.tsx` | 可选修改 | +10 行（placeholder 增强） |
| **总计** | | ~100 行 |

## 与现有机制的关系

- **不替代** `handleWorkflowReview`：按钮只是快捷入口，LLM 分类器作为自由输入的 fallback 保留
- **不替代** `ask_user`：ask_user 是 LLM 主动提问的通用工具，本方案是引擎层的确定性注入
- **不替代** `detectWorkflowReviewIntentFast`：新增 `__wf_review__` 前缀只是扩展 fast-path 的覆盖范围
- **复用** `ActionButtons` 组件：零 UI 组件新增，完全复用已有的按钮渲染和交互模式
- **复用** `disableActionsForCommand`：一键 disable 所有按钮的交互模式

## 不做的事

1. **不改变引擎状态机**：引擎的 PendingReview/WaitingConfirm 状态流转完全不变
2. **不改变 LLM 分类器**：自由输入仍走 LLM 分类，按钮只是跳过分类器的快捷路径
3. **不改变文档预览面板**：右侧预览面板的显示逻辑不受影响
4. **不新增前端组件**：复用 ActionButtons
5. **不影响非 V2 工作流**：条件判断 `h.isWorkflowV2Active(ownerID)` 保护
