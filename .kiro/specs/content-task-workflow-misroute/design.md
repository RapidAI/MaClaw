# Content Task Workflow Misroute Bugfix Design

## Overview

Content processing tasks (内容处理任务) like "看 HF 每日论文摘要做深度解读生成 PDF" are being misrouted into the workflow engine because the intent understanding LLM lacks explicit guidance to distinguish one-shot content processing from multi-phase workflow creation. When the LLM eventually corrects itself mid-session (returning `category="none"` with `ready=true`), `handleActiveUnderstanding()` unconditionally calls `StartWorkflow("none")`, which fails with "未找到匹配的工作流模板: none" and loses all conversation context.

The fix has two parts:
1. **Primary fix**: Enhance the LLM system prompt in `buildSystemPrompt()` with a dedicated section teaching the semantic distinction between content processing and workflow tasks — generalizable, not keyword-based
2. **Safety net**: Handle `category="none"` + `ready=true` in `handleActiveUnderstanding()` by cleaning up the session and falling through to the normal agent loop instead of calling `StartWorkflow`

## Glossary

- **Bug_Condition (C)**: A content processing task enters the understanding session AND/OR `HandleInput` returns `ready=true` with `category="none"`, leading to a `StartWorkflow("none")` call that fails
- **Property (P)**: Content processing tasks are classified as `category="none"` by the LLM and fall through to the normal agent loop; `category="none"` + `ready=true` never triggers `StartWorkflow`
- **Preservation**: Genuine multi-phase workflow tasks (literature review, coding, PRD, etc.) continue to be correctly classified and routed through the workflow engine
- **`buildSystemPrompt()`**: The function in `corelib/workflow/intent_understanding.go` that constructs the LLM system prompt for intent classification, including workflow type descriptions and confusable examples
- **`handleActiveUnderstanding()`**: The function in `gui/im_message_handler_workflow.go` that processes user input within an active understanding session and decides whether to call `StartWorkflow`
- **Content Processing Task**: A one-shot task that reads, processes, or transforms existing content (翻译、摘要、解读、整理、格式转换 etc.) — does NOT create new structured documents through iterative refinement
- **Workflow Task**: A multi-phase project that creates new structured documents through iterative refinement (需求→设计→编码, 文献综述, 商业计划 etc.)

## Bug Details

### Bug Condition

The bug manifests in two cascading scenarios:

**Scenario A (Misclassification)**: A user sends a content processing task containing words that overlap with workflow template keywords (e.g., "论文" overlaps with `literatureReviewTemplate.Keywords`). The `buildSystemPrompt()` lacks explicit guidance to distinguish content processing from workflow tasks, so the LLM misclassifies it as a workflow (e.g., `literature_review`), creating an unnecessary understanding session.

**Scenario B (category="none" + ready=true crash)**: During an active understanding session, the LLM eventually realizes the task is not a workflow and returns `category="none"` with `ready=true`. `handleActiveUnderstanding()` unconditionally calls `engine.StartWorkflow(userID, *intent)` with `intent.Category="none"`. `registry.Match("none")` returns nil, causing the error "未找到匹配的工作流模板: none". The understanding session has already been cleaned up (deleted in `HandleInput` when `isReady=true`), so the user's next message has no context.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type UserMessage with active understanding context
  OUTPUT: boolean

  // Scenario A: content processing task misrouted to understanding session
  LET isContentTask = taskInvolves(input, ONE_SHOT_CONTENT_PROCESSING)
                      AND NOT taskInvolves(input, MULTI_PHASE_DOCUMENT_CREATION)
  LET wasRouted = routedToUnderstandingSession(input)

  // Scenario B: category="none" + ready=true in active session
  LET llmResult = getLLMIntentResult(input)
  LET noneReadyCrash = hasActiveSession(input.userID)
                       AND llmResult.category == "none"
                       AND llmResult.ready == true

  RETURN (isContentTask AND wasRouted) OR noneReadyCrash
END FUNCTION
```

### Examples

- **"看 HF 每日论文摘要做深度解读生成 PDF"** → Expected: `category="none"` (one-shot content processing: read papers → summarize → generate PDF). Actual: `category="literature_review"` (misclassified as multi-phase academic workflow because "论文" matches template keywords)
- **"把这篇英文论文翻译成中文"** → Expected: `category="none"` (translation is one-shot). Actual: risk of `category="literature_review"` due to "论文"
- **"帮我整理这份会议纪要"** → Expected: `category="none"` (content reformatting). Actual: could be misclassified if "会议" overlaps with event_planning keywords
- **Active session → user says "都要，开工" → LLM returns `category="none"`, `ready=true`** → Expected: graceful fallthrough to agent loop. Actual: `StartWorkflow("none")` → error "未找到匹配的工作流模板: none" → context lost

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Genuine multi-phase workflow tasks ("帮我写一篇文献综述", "开发一个贪吃蛇游戏", "做一份商业计划书") must continue to be correctly classified and routed to their respective workflow templates
- QuickFilter's small talk detection, active session routing, and `FilterNeedsUnderstanding` classification must remain unchanged
- Simple directives ("翻译这段英文", "什么是微服务") must continue to return `category="none"` on first-shot classification
- Active understanding sessions must continue to support multi-round clarification, cancel ("取消"), and normal workflow startup when `category` is a valid workflow type with `ready=true`
- The keyword fallback mechanism (`tryKeywordWorkflowFallback`) must continue to work for strong keyword matches after LLM rejection
- `quick_filter.go` must NOT be modified — it correctly routes everything to LLM for classification
- `corelib/workflow/engine.go` (`StartWorkflow`) must NOT be modified — the fix prevents invalid calls from reaching it
- `corelib/workflow/registry.go` must NOT be modified

**Scope:**
All inputs that do NOT involve content processing tasks or the `category="none"` + `ready=true` edge case should be completely unaffected by this fix. This includes:
- All genuine workflow task classifications
- All non-workflow classifications (small talk, simple directives)
- All multi-round understanding session interactions
- All keyword fallback paths

## Hypothesized Root Cause

Based on the bug description and code analysis, the root causes are:

1. **Missing Semantic Distinction in LLM Prompt**: `buildSystemPrompt()` in `corelib/workflow/intent_understanding.go` lists "不需要工作流" examples (翻译、格式化、总结 etc.) and "需要工作流" examples, but does NOT explicitly teach the LLM the **semantic pattern** that distinguishes them. The LLM sees "论文" in the user's message and matches it to `literature_review` template keywords without understanding that "reading/summarizing existing papers" is fundamentally different from "writing a new literature review document". The current confusable examples section doesn't cover this specific ambiguity.

2. **No Guard for `category="none"` + `ready=true` in `handleActiveUnderstanding()`**: In `gui/im_message_handler_workflow.go`, the `handleActiveUnderstanding()` function checks `if ready && intent != nil` and unconditionally calls `engine.StartWorkflow(userID, *intent)`. It does not check whether `intent.Category` is a valid, non-"none" workflow type. When the LLM changes its mind mid-session and returns `category="none"` with `ready=true`, this code path crashes.

3. **Session Cleanup Before StartWorkflow**: In `HandleInput()` (`corelib/workflow/intent_understanding.go`), when `isReady=true`, the session is immediately deleted from memory (`delete(m.sessions, userID)`). This happens BEFORE `handleActiveUnderstanding()` calls `StartWorkflow`. When `StartWorkflow` fails, the session is already gone, and the user's next message has no context.

## Correctness Properties

Property 1: Bug Condition - LLM System Prompt Contains Content Processing Guidance

_For any_ call to `buildSystemPrompt()`, the returned prompt string SHALL contain a dedicated section titled "内容处理任务 vs 工作流任务" that teaches the LLM the semantic distinction between one-shot content processing (read/process/transform existing content → `category="none"`) and multi-phase workflow creation (create new structured documents through iterative refinement → workflow category), including confusable examples that specifically address the "论文摘要" vs "文献综述" ambiguity.

**Validates: Requirements 2.1, 2.2**

Property 2: Bug Condition - category="none" + ready=true Does Not Call StartWorkflow

_For any_ active understanding session where `HandleInput` returns `ready=true` with `intent.Category="none"`, `handleActiveUnderstanding()` SHALL NOT call `engine.StartWorkflow()`. Instead, it SHALL clean up the understanding session and return `nil` (fall through to the normal agent loop).

**Validates: Requirements 2.3, 2.4, 2.5**

Property 3: Preservation - Valid Workflow Category + ready=true Still Starts Workflows

_For any_ active understanding session where `HandleInput` returns `ready=true` with `intent.Category` set to a valid, registered workflow type (e.g., "coding", "literature_review", "product_design"), `handleActiveUnderstanding()` SHALL continue to call `engine.StartWorkflow()` and return the workflow startup message, preserving all existing workflow routing behavior.

**Validates: Requirements 3.1, 3.2, 3.7**

Property 4: Preservation - Existing System Prompt Sections Unchanged

_For any_ call to `buildSystemPrompt()`, the returned prompt string SHALL continue to contain all existing sections (核心判断, 可用的工作流类型, 你的职责, 输出格式, category 判断规则, 易混淆示例, ready 判断规则) with their original content unchanged, ensuring no regression in LLM classification behavior for non-content-processing inputs.

**Validates: Requirements 3.3, 3.4, 3.5, 3.6, 3.8**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `corelib/workflow/intent_understanding.go`

**Function**: `buildSystemPrompt()`

**Specific Changes**:
1. **Add "内容处理任务 vs 工作流任务" section**: Insert a new section after "核心判断：是否需要工作流" that explicitly teaches the LLM the semantic distinction:
   - **Content processing** (内容处理) = one-shot tasks that read, process, or transform **existing** content. The input already exists; the task is to process it into a different form. Examples: 翻译、摘要、解读、整理、格式转换、字幕处理、文档梳理、资料收集. These are `category="none"`.
   - **Workflow tasks** (工作流任务) = multi-phase projects that **create new** structured documents through iterative refinement. The output is a new artifact that doesn't exist yet and requires multiple rounds of planning, drafting, and revision. Examples: 写文献综述、开发系统、做商业计划书.
   - The key semantic test: "Is the user asking to **process existing content** or **create a new structured artifact**?"

2. **Add confusable examples for content processing vs workflow**: Add specific examples to the "易混淆示例" section that address the ambiguity:
   - "看HF论文做摘要" → `category="none"` (reading existing papers and summarizing = content processing)
   - "帮我写一篇文献综述" → `category="literature_review"` (creating a new academic document = workflow)
   - "把这份报告翻译成英文" → `category="none"` (translating existing content)
   - "帮我写一份研究报告" → `category="research_report"` (creating a new report)
   - "整理这些会议纪要" → `category="none"` (reformatting existing content)
   - "做一份竞品分析报告" → `category="competitive_analysis"` (creating a new analysis)
   - "解读这篇论文的核心观点" → `category="none"` (analyzing existing content)
   - "帮我写一篇论文" → `category="paper_writing"` (creating a new paper)

3. **No keyword-based filtering**: The fix is entirely in the LLM prompt — teaching the LLM a generalizable semantic pattern. No keyword lists are added to `QuickFilter`, `quick_filter.go`, or anywhere else. The LLM is the semantic classifier; keywords are too brittle and cause false positives/negatives.

---

**File**: `gui/im_message_handler_workflow.go`

**Function**: `handleActiveUnderstanding()`

**Specific Changes**:
4. **Guard against `category="none"` + `ready=true`**: After `HandleInput` returns `ready=true` and `intent != nil`, add a check: if `intent.Category == "none"` or `intent.Category == ""`, do NOT call `StartWorkflow`. Instead:
   - Log the event for debugging: `log.Printf("[WorkflowInterception] understanding returned ready=true with category=%q for user %s, falling through to agent loop", intent.Category, userID)`
   - Return `nil` (fall through to normal agent loop)
   - The understanding session has already been cleaned up by `HandleInput`, so no additional cleanup is needed

5. **Preserve existing behavior for valid categories**: The guard only triggers for `category="none"` or empty category. All valid workflow types (coding, literature_review, product_design, etc.) continue to flow through to `StartWorkflow` unchanged.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that verify the system prompt content and the `handleActiveUnderstanding` code path. Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **System Prompt Missing Section Test**: Assert `buildSystemPrompt()` output contains "内容处理任务 vs 工作流任务" section (will fail on unfixed code — section doesn't exist)
2. **System Prompt Missing Confusable Examples Test**: Assert prompt contains "看HF论文做摘要" → `category="none"` example (will fail on unfixed code)
3. **category="none" + ready=true Crash Test**: Mock `HandleInput` to return `ready=true` with `category="none"`, assert `StartWorkflow` is NOT called (will fail on unfixed code — `StartWorkflow` is called unconditionally)
4. **category="" + ready=true Crash Test**: Mock `HandleInput` to return `ready=true` with empty category, assert `StartWorkflow` is NOT called (will fail on unfixed code)

**Expected Counterexamples**:
- `buildSystemPrompt()` output does not contain content processing guidance section
- `handleActiveUnderstanding()` calls `StartWorkflow` with `category="none"`, resulting in error

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  // Verify system prompt enhancement
  prompt := buildSystemPrompt()
  ASSERT contains(prompt, "内容处理任务 vs 工作流任务")
  ASSERT contains(prompt, confusable content processing examples)

  // Verify handleActiveUnderstanding safety net
  IF input.ready == true AND input.category == "none" THEN
    result := handleActiveUnderstanding(input)
    ASSERT result == nil  // falls through to agent loop
    ASSERT StartWorkflow NOT called
  END IF
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT buildSystemPrompt_fixed() contains all original sections unchanged
  IF input.ready == true AND input.category IN validWorkflowTypes THEN
    ASSERT handleActiveUnderstanding_fixed(input) calls StartWorkflow
    ASSERT handleActiveUnderstanding_fixed(input) == handleActiveUnderstanding_original(input)
  END IF
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (various category values, ready states)
- It catches edge cases that manual unit tests might miss (empty strings, unknown categories)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for valid workflow categories, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Valid Category Preservation**: For all registered workflow types, verify `handleActiveUnderstanding` with `ready=true` still calls `StartWorkflow` and returns the startup message
2. **System Prompt Section Preservation**: Verify all existing sections (核心判断, 输出格式, category 判断规则, etc.) are present and unchanged in the fixed prompt
3. **Cancel Behavior Preservation**: Verify "取消" still cancels the understanding session
4. **Multi-round Clarification Preservation**: Verify `ready=false` responses still maintain the session

### Unit Tests

- Test `buildSystemPrompt()` output contains the new "内容处理任务 vs 工作流任务" section
- Test `buildSystemPrompt()` output contains all confusable content processing examples
- Test `buildSystemPrompt()` output still contains all original sections unchanged
- Test `handleActiveUnderstanding()` with `category="none"` + `ready=true` returns `nil`
- Test `handleActiveUnderstanding()` with `category=""` + `ready=true` returns `nil`
- Test `handleActiveUnderstanding()` with `category="coding"` + `ready=true` still calls `StartWorkflow`
- Test `handleActiveUnderstanding()` with `category="literature_review"` + `ready=true` still calls `StartWorkflow`

### Property-Based Tests

- Generate random valid `WorkflowType` values and verify `handleActiveUnderstanding` with `ready=true` always calls `StartWorkflow` for non-"none" categories (preservation)
- Generate random strings for `category` and verify: if category is "none" or empty, `StartWorkflow` is never called; if category matches a registered template, `StartWorkflow` is called (fix + preservation combined)
- Verify `buildSystemPrompt()` output is a superset of the original prompt (all original content preserved, new content added)

### Integration Tests

- End-to-end test: send "看HF论文做摘要" → verify it falls through to normal agent loop without creating an understanding session
- End-to-end test: simulate active session → LLM returns `category="none"` + `ready=true` → verify graceful fallthrough, no error shown to user
- End-to-end test: send "帮我写一篇文献综述" → verify it correctly enters `literature_review` workflow (preservation)
