# Bugfix Requirements Document

## Introduction

Content processing tasks (内容处理任务) such as "看 Hugging Face 每日论文摘要，做深度解读，生成 PDF" are being misrouted into the workflow engine instead of being executed directly by the normal agent loop. This causes two cascading failures:

1. The intent understanding LLM misclassifies the task as a `literature_review` workflow (because "论文" matches template keywords), leading to unnecessary multi-round clarification
2. When the user clarifies it's a simple task and the LLM eventually returns `category="none"` with `ready=true`, `StartWorkflow("none")` fails with "未找到匹配的工作流模板: none", and the system loses all conversation context

The steering rule (coding-workflow.md) explicitly states that content processing tasks (翻译、整理、总结、格式转换、字幕处理、文档梳理、资料收集等) should be executed directly without repeated confirmation. The workflow engine's LLM prompt does not enforce this distinction.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a user sends a content processing task like "看 HF 每日论文摘要做深度解读生成 PDF" THEN the system routes it to `FilterNeedsUnderstanding` and the intent understanding LLM classifies it as `literature_review` (a multi-phase academic workflow), starting a multi-round clarification session instead of executing the task directly

1.2 WHEN the intent understanding LLM system prompt (`buildSystemPrompt()`) receives a content processing task containing words that overlap with workflow template keywords (e.g., "论文" overlaps with `literatureReviewTemplate.Keywords`) THEN the LLM lacks explicit guidance to distinguish one-shot content processing tasks from multi-phase workflow tasks, and misclassifies the content processing task as a workflow

1.3 WHEN an active understanding session exists and the user says "都要，开工" (confirming they want to proceed) THEN the LLM returns `ready=true` with `category="none"` (having realized it's not a workflow task), and `handleActiveUnderstanding()` calls `engine.StartWorkflow(userID, *intent)` with `intent.Category="none"`

1.4 WHEN `StartWorkflow` is called with `intent.Category="none"` THEN `registry.Match("none")` returns nil and the system returns error "启动工作流失败：未找到匹配的工作流模板：none" to the user

1.5 WHEN the workflow startup fails with the "none" template error THEN the understanding session has already been cleaned up (deleted from memory in `HandleInput` when `isReady=true`), and the user's next message "开工" is treated as a brand new request with no context, resulting in "好的，开工！请问今天要做什么任务呢？"

### Expected Behavior (Correct)

2.1 WHEN a user sends a content processing task like "看 HF 每日论文摘要做深度解读生成 PDF" THEN the system SHALL classify it as `category="none"` (not a workflow task) and fall through to the normal agent loop for immediate execution, without starting an understanding session

2.2 WHEN the intent understanding LLM evaluates a task that involves reading/summarizing/translating/reformatting existing content (content processing) THEN the LLM system prompt SHALL contain explicit guidance distinguishing content processing tasks from multi-phase workflow tasks, and SHALL classify content processing as `category="none"` even when the task mentions words like "论文", "摘要", "报告"

2.3 WHEN `HandleInput` in an active understanding session returns `ready=true` with `category="none"` THEN `handleActiveUnderstanding()` SHALL NOT call `StartWorkflow`; instead it SHALL clean up the understanding session and return nil (fall through to normal agent loop), allowing the user's message to be processed normally

2.4 WHEN `StartWorkflow` is called with a category that does not match any registered template THEN the system SHALL gracefully handle the mismatch by canceling the workflow attempt and falling through to the normal agent loop, rather than showing a raw error message to the user

2.5 WHEN the system falls through from a failed or cancelled understanding session to the normal agent loop THEN the user's original intent and conversation context SHALL be preserved so the agent can execute the task directly

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a user sends a genuine multi-phase workflow task like "帮我写一篇文献综述" (write a literature review) THEN the system SHALL CONTINUE TO classify it as `literature_review` and start the multi-round understanding session

3.2 WHEN a user sends a genuine coding task like "开发一个贪吃蛇游戏" THEN the system SHALL CONTINUE TO classify it as `coding` and route it through the workflow engine

3.3 WHEN a user sends small talk like "你好" or "谢谢" THEN the QuickFilter SHALL CONTINUE TO classify it as `FilterSmallTalk` and bypass the workflow engine

3.4 WHEN a user sends a simple directive like "翻译这段英文" THEN the intent understanding LLM SHALL CONTINUE TO return `category="none"` and the system SHALL CONTINUE TO fall through to the normal agent loop

3.5 WHEN an active understanding session exists and the user provides additional requirements (not ready to start) THEN the system SHALL CONTINUE TO maintain the session and ask follow-up questions

3.6 WHEN an active understanding session exists and the user says "取消" THEN the system SHALL CONTINUE TO cancel the session and return "已取消。有什么其他需要帮助的吗？"

3.7 WHEN the intent understanding LLM correctly identifies a workflow task with a valid category and `ready=true` THEN `handleActiveUnderstanding()` SHALL CONTINUE TO call `StartWorkflow` and launch the workflow normally

3.8 WHEN the keyword fallback mechanism (`tryKeywordWorkflowFallback`) matches a strong keyword like "PRD" after LLM rejection THEN the system SHALL CONTINUE TO override the rejection and start the workflow

---

## Bug Condition (Formal)

### Bug Condition Function

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type UserMessage
  OUTPUT: boolean
  
  // Bug triggers when a content processing task enters the understanding session
  // and the LLM eventually returns category="none" with ready=true
  RETURN (isContentProcessingTask(X) AND routedToUnderstandingSession(X))
         OR (activeUnderstandingSession(X.userID) AND llmReturns(category="none", ready=true))
END FUNCTION
```

Where `isContentProcessingTask(X)` is true when the user's message describes a one-shot content task (read → process → output) rather than a multi-phase project requiring iterative document production.

### Fix Checking Property

```pascal
// Property: Fix Checking — Content processing tasks execute directly
FOR ALL X WHERE isContentProcessingTask(X) DO
  result ← classifyAndRoute(X)
  ASSERT result.category = "none" OR result.fallthrough = true
  ASSERT NOT result.understandingSessionCreated
END FOR

// Property: Fix Checking — category="none" + ready=true gracefully handled
FOR ALL X WHERE activeSession(X.userID) AND llmReturns(category="none", ready=true) DO
  result ← handleActiveUnderstanding(X)
  ASSERT NOT result.startWorkflowCalled
  ASSERT result.sessionCleaned = true
  ASSERT result.fallthrough = true  // returns nil to normal agent loop
END FOR
```

### Preservation Property

```pascal
// Property: Preservation Checking — Non-buggy inputs unchanged
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT classifyAndRoute(X) = classifyAndRoute'(X)
  // Genuine workflow tasks still route to workflows
  // Small talk still gets filtered
  // Simple directives still fall through
  // Valid category + ready=true still starts workflows
END FOR
```
