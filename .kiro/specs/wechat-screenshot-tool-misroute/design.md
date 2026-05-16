# 微信截屏工具误路由 Bugfix Design

## Overview

用户在微信通道发送"帮我截屏桌面发给我图片"时，maclaw 没有调用内建 `screenshot` 工具，而是走了 `craft_tool` 路径（生成脚本→保存→执行），产生大量中间状态消息。根因是双层叠加问题：UIC 关键词注册表缺少"截屏"，且"发给我"以 Strong 强度命中 `LabelDocumentDelivery`，导致 `craft_tool` 通过条件规则被激活并与 `screenshot` 竞争。

修复策略：
1. 在 UIC 关键词注册表中添加"截屏"为 `LabelNonCoding` Strong 关键词（与"截图"对齐但不归入 Browser）
2. 将"截图"从 `LabelBrowser` Weak 提升为 `LabelNonCoding` Strong（截图是通用操作，不应归属浏览器意图）
3. 在 `documentDeliveryKeywords` 的条件规则中添加截屏关键词排除逻辑，当消息包含截屏/截图关键词时不激活 `craft_tool`

## Glossary

- **Bug_Condition (C)**: 用户消息包含截屏关键词（"截屏"/"截图"）但 UIC 将其分类为 `document_delivery` 意图，导致 `craft_tool` 被条件激活并被 LLM 选择
- **Property (P)**: 包含截屏关键词的消息应被正确分类（不为 `document_delivery`），且 `craft_tool` 不应因截屏消息中的"发给我"等词被条件激活
- **Preservation**: 纯文档交付请求（不含截屏关键词）的分类和工具激活行为不变
- **UIC**: `UnifiedIntentClassifier`，三层意图分类器（Layer 1 关键词 → Layer 2 Embedding → Layer 3 LLM）
- **Layer 1**: `classifyByKeywords()`，基于 `KeywordRegistry` 的关键词匹配分类，`corelib/intent/layer1.go`
- **KeywordRegistry**: `corelib/intent/keyword_registry.go` 中的 `defaultKeywords` 列表，所有关键词的单一数据源
- **conditionalKeepRules**: `corelib/tool/router.go` 中的条件工具激活规则，按关键词匹配决定是否将工具加入 LLM 工具列表
- **documentDeliveryKeywords**: `corelib/tool/router.go` 中的文档交付关键词列表（"pdf"、"发给我"、"导出"等），匹配时激活 `send_file`、`open`、`craft_tool`
- **CoreToolNames**: `corelib/tool/router.go` 中始终包含在工具列表中的核心工具集，`screenshot` 已在其中
- **ToolAffinityRegistry**: `corelib/intent/tool_affinity.go`，将 IntentLabel 映射到关联工具列表

## Bug Details

### Bug Condition

用户消息包含"截屏"或"截图"关键词时，UIC Layer 1 无法正确识别截屏意图。"截屏"完全不在关键词注册表中；"截图"仅为 `LabelBrowser` Weak（单个 weak 关键词在 Layer 1 中被过滤删除）。同时"发给我"以 `LabelDocumentDelivery` Strong（confidence=0.92）胜出，关联工具列表为 `["send_file", "open", "craft_tool"]`。`craft_tool` 通过 `documentDeliveryKeywords` 条件规则被激活，与 CoreTool `screenshot` 竞争，LLM 在 `document_delivery` 意图框架下倾向选择 `craft_tool`。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type {text: string, uicResult: ClassificationResult, toolList: []string}
  OUTPUT: boolean

  hasScreenshotKeyword := contains(input.text, "截屏") OR contains(input.text, "截图")
  classifiedAsDocDelivery := input.uicResult.Primary == LabelDocumentDelivery
  craftToolActivated := "craft_tool" IN input.toolList

  RETURN hasScreenshotKeyword
         AND classifiedAsDocDelivery
         AND craftToolActivated
END FUNCTION
```

### Examples

- "帮我截屏桌面发给我图片" → "截屏"不在注册表，"发给我"命中 `LabelDocumentDelivery` Strong → `craft_tool` 激活 → LLM 选择 `craft_tool` 生成 PowerShell 截屏脚本（❌ 应直接调用 `screenshot`）
- "截图桌面发给我" → "截图"仅为 `LabelBrowser` Weak（单个 weak 被删除），"发给我"命中 `LabelDocumentDelivery` Strong → 同上（❌）
- "帮我截屏" → "截屏"不在注册表 → `LabelUnknown` → 无条件工具激活 → `screenshot` 作为 CoreTool 可用但 LLM 可能不确定该用哪个工具（⚠️ 次优）
- "截图发给我" → "截图" Weak 被删除，"发给我" Strong → `document_delivery` → `craft_tool` 激活（❌）

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 纯文档交付请求（如"把报告发给我"、"导出 PDF"、"附件发我"）不含截屏关键词时，继续分类为 `document_delivery`，激活 `send_file`、`open`、`craft_tool`
- 浏览器强关键词的截图请求（如"打开浏览器帮我截图"）继续分类为 `LabelBrowser`（"浏览器" Strong 优先级高于截图关键词）
- `screenshot` 作为 CoreTool 始终在工具列表中，不受意图分类结果影响
- 不包含截屏关键词的消息，UIC 关键词匹配逻辑按现有优先级规则正常工作
- `labelPriority` 冲突解决顺序不变（ssh > browser > coding > non_coding > ...）

**Scope:**
修复仅影响包含"截屏"/"截图"关键词的消息路径。所有不包含这些关键词的消息行为完全不变。

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **关键词注册表缺失"截屏"**：`defaultKeywords` 中没有"截屏"条目，Layer 1 完全无法识别该关键词。"截图"虽然存在但归类为 `LabelBrowser` Weak，单个 weak 关键词在 `classifyByKeywords()` 中被 `delete(scores, LabelBrowser)` 删除（第 82-84 行逻辑：`if bs.weak == 1 && bs.strong == 0 { delete(scores, LabelBrowser) }`）

2. **"发给我"作为 Strong 关键词无竞争对手**：`LabelDocumentDelivery` 的"发给我"是 Strong（confidence=0.92），在截屏关键词被过滤/缺失后，成为唯一的 Strong 匹配，无条件胜出

3. **条件规则无截屏排除**：`documentDeliveryKeywords` 的条件规则（`conditionalKeepRules` 第 3 条）仅检查 `containsAnyKeyword(msg, documentDeliveryKeywords)`，不检查消息是否同时包含截屏关键词。当"发给我"匹配时，`craft_tool` 无条件被激活

4. **"截图"归类为 Browser 不合理**：截图/截屏是通用桌面操作，不应归属浏览器意图。当前"截图"在 `browserActionKeywords` 中作为 Weak 关键词，语义上不准确

## Correctness Properties

Property 1: Bug Condition - 截屏关键词不被分类为 document_delivery

_For any_ input where the message contains screenshot keywords ("截屏" or "截图") and also contains document delivery keywords ("发给我", "导出", etc.), the fixed `classifyByKeywords` function SHALL NOT return `LabelDocumentDelivery` as the primary classification. The screenshot keyword SHALL take priority, preventing the document delivery intent from hijacking the screenshot request.

**Validates: Requirements 2.1, 2.4**

Property 2: Preservation - 纯文档交付请求分类不变

_For any_ input where the message contains document delivery keywords ("发给我", "导出", "pdf", etc.) but does NOT contain screenshot keywords ("截屏", "截图"), the fixed `classifyByKeywords` function SHALL produce the same classification result as the original function, preserving `LabelDocumentDelivery` as the primary label with the same confidence level.

**Validates: Requirements 3.1, 3.4, 3.5**

Property 3: Bug Condition - 截屏消息不激活 craft_tool 条件规则

_For any_ input where the message contains screenshot keywords ("截屏" or "截图"), the fixed `conditionalKeepRules` document delivery rule SHALL NOT activate `craft_tool`, even when the message also contains document delivery keywords like "发给我".

**Validates: Requirements 2.2**

Property 4: Preservation - 非截屏文档交付消息的条件规则不变

_For any_ input where the message contains document delivery keywords but does NOT contain screenshot keywords, the fixed `conditionalKeepRules` SHALL produce the same `keep` set as the original function, preserving `send_file`, `open`, `craft_tool` activation.

**Validates: Requirements 3.1, 3.5**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `corelib/intent/keyword_registry.go`

**Section**: `defaultKeywords` 列表

**Specific Changes**:
1. **添加"截屏"关键词**：在 `LabelNonCoding` Strong 区域添加 `{Keyword: "截屏", Label: LabelNonCoding, Strength: Strong}`。选择 `LabelNonCoding` 而非新建 Label 的原因：截屏是通用操作，`screenshot` 已是 CoreTool 始终可用，不需要通过意图分类来激活工具，只需要确保不被 `document_delivery` 劫持
2. **将"截图"从 Browser Weak 移到 NonCoding Strong**：删除 `LabelBrowser` Weak 区域的 `{Keyword: "截图", Label: LabelBrowser, Strength: Weak}`，在 `LabelNonCoding` Strong 区域添加 `{Keyword: "截图", Label: LabelNonCoding, Strength: Strong}`。原因：截图是通用桌面操作，不应归属浏览器意图；作为 Strong 关键词可以与"发给我"的 `LabelDocumentDelivery` Strong 竞争，通过 `labelPriority`（NonCoding=3 < DocumentDelivery=6）胜出

**File**: `corelib/tool/router.go`

**Section**: `conditionalKeepRules` 中的 document delivery 规则

**Specific Changes**:
3. **添加截屏关键词排除**：修改 document delivery 条件规则的 `matches` 函数，当消息包含截屏关键词（"截屏"/"截图"）时返回 false，不激活 `craft_tool`。这是防御性措施——即使 UIC 分类正确（NonCoding 而非 DocumentDelivery），条件规则层面也不会因"发给我"误激活 `craft_tool`

4. **定义截屏排除关键词列表**：新增 `screenshotExcludeKeywords = []string{"截屏", "截图", "screenshot"}` 变量，供条件规则排除检查使用

5. **同步更新 `documentDeliveryKeywords` 注释**：说明截屏场景的排除逻辑

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that call `classifyByKeywords()` with messages containing screenshot keywords + "发给我", and verify the classification result. Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **截屏+发给我 Test**: `classifyByKeywords("帮我截屏桌面发给我图片")` → expect NOT `LabelDocumentDelivery` (will fail on unfixed code because "截屏" not in registry, "发给我" wins as Strong)
2. **截图+发给我 Test**: `classifyByKeywords("截图桌面发给我")` → expect NOT `LabelDocumentDelivery` (will fail on unfixed code because "截图" is Browser Weak, gets deleted)
3. **条件规则 Test**: `matchConditionalKeepRules("帮我截屏桌面发给我图片")` → expect `craft_tool` NOT in keep set (will fail on unfixed code because "发给我" matches documentDeliveryKeywords)
4. **纯截屏 Test**: `classifyByKeywords("帮我截屏")` → expect NOT `LabelUnknown` (will fail on unfixed code because "截屏" not in registry)

**Expected Counterexamples**:
- `classifyByKeywords("帮我截屏桌面发给我图片")` returns `{Primary: LabelDocumentDelivery, Confidence: 0.92}`
- `matchConditionalKeepRules("截图发给我")` returns `keep` containing `craft_tool`
- Possible causes: "截屏" missing from registry, "截图" as single Browser Weak gets deleted, "发给我" Strong wins unopposed

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := classifyByKeywords_fixed(input)
  ASSERT result.Primary != LabelDocumentDelivery
  keepSet := matchConditionalKeepRules_fixed(input.text)
  ASSERT "craft_tool" NOT IN keepSet
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT containsScreenshotKeyword(input.text) DO
  ASSERT classifyByKeywords_original(input) == classifyByKeywords_fixed(input)
  ASSERT matchConditionalKeepRules_original(input.text) == matchConditionalKeepRules_fixed(input.text)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (random combinations of delivery keywords, SSH keywords, browser keywords, etc.)
- It catches edge cases that manual unit tests might miss (e.g., messages with multiple overlapping keyword categories)
- It provides strong guarantees that behavior is unchanged for all non-screenshot inputs

**Test Plan**: Observe behavior on UNFIXED code first for non-screenshot messages, then write property-based tests capturing that behavior.

**Test Cases**:
1. **纯文档交付 Preservation**: Verify "把报告发给我"、"导出 PDF" etc. continue to classify as `LabelDocumentDelivery` and activate `craft_tool`
2. **SSH 意图 Preservation**: Verify "登录服务器" etc. continue to classify as `LabelSSH`
3. **Browser 强关键词 Preservation**: Verify "打开浏览器帮我截图" continues to classify as `LabelBrowser`（"浏览器" Strong 优先）
4. **无关消息 Preservation**: Verify messages without any screenshot or delivery keywords continue to classify unchanged

### Unit Tests

- Test `classifyByKeywords` with "截屏" alone → expect `LabelNonCoding` Strong
- Test `classifyByKeywords` with "截图" alone → expect `LabelNonCoding` Strong
- Test `classifyByKeywords` with "截屏" + "发给我" → expect `LabelNonCoding` (priority 3 < DocumentDelivery priority 6)
- Test `classifyByKeywords` with "截图" + "发给我" → expect `LabelNonCoding`
- Test `matchConditionalKeepRules` with "截屏发给我" → expect `craft_tool` NOT in keep
- Test `matchConditionalKeepRules` with "把报告发给我" → expect `craft_tool` IN keep (preservation)
- Test `classifyByKeywords` with "打开浏览器帮我截图" → expect `LabelBrowser` (browser Strong wins)

### Property-Based Tests

- Generate random messages combining screenshot keywords with delivery keywords → verify classification is never `LabelDocumentDelivery`
- Generate random messages with delivery keywords but WITHOUT screenshot keywords → verify classification matches original behavior
- Generate random messages with screenshot keywords → verify `craft_tool` is never in conditional keep set
- Generate random messages without screenshot keywords → verify conditional keep set matches original behavior

### Integration Tests

- End-to-end test: simulate WeChat message "帮我截屏桌面发给我图片" through `Route()` → verify `screenshot` is in final tool list and `craft_tool` is NOT conditionally activated
- End-to-end test: simulate "把报告发给我" through `Route()` → verify `craft_tool` IS conditionally activated (preservation)
- End-to-end test: simulate "打开浏览器帮我截图网页" through `Route()` → verify browser tools are activated (preservation)
