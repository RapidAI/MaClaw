# Bugfix Requirements Document

## Introduction

用户在微信通道发送"帮我截屏桌面发给我图片"时，maclaw 没有直接调用内建的 `screenshot` 工具，而是走了 `craft_tool` 路径（生成脚本、保存脚本、执行 PowerShell/Python 脚本），产生大量中间状态消息，用户体验很差。

根因是双层叠加问题：

1. **UIC 意图分类偏差**：用户消息中"发给我"命中 `LabelDocumentDelivery`（Strong），而"截屏"不在关键词注册表中（只有"截图"是 `LabelBrowser` Weak）。UIC Layer 1 以高置信度（0.92）将消息分类为 `document_delivery`，关联工具为 `["send_file", "open", "craft_tool"]`。
2. **`craft_tool` 被条件激活后与 `screenshot` 竞争**：`documentDeliveryKeywords` 中的"发给我"触发条件规则，将 `craft_tool` 加入工具列表。虽然 `screenshot` 作为 CoreTool 始终在列表中，但 `craft_tool` 的描述更通用（"生成并执行单脚本来完成一次性自动化任务"），LLM 在 `document_delivery` 意图框架下倾向选择 `craft_tool` 而非 `screenshot`。

核心矛盾：截屏请求的语义被"发给我"关键词劫持到了文档交付意图，而截屏相关关键词（"截屏"）在 UIC 关键词注册表中缺失，无法与 `document_delivery` 竞争。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN 用户发送包含"截屏"的消息（如"帮我截屏桌面发给我图片"）THEN UIC Layer 1 因"截屏"不在关键词注册表中而无法识别截屏意图，将"发给我"匹配为 `LabelDocumentDelivery`（Strong, confidence=0.92），关联工具列表为 `["send_file", "open", "craft_tool"]` 而非包含 `screenshot`

1.2 WHEN UIC 将截屏请求分类为 `document_delivery` 意图 THEN `craft_tool` 通过 `documentDeliveryKeywords`（"发给我"）的条件规则被激活并加入工具列表，与 CoreTool `screenshot` 形成竞争

1.3 WHEN LLM 在 `document_delivery` 意图框架下同时看到 `screenshot` 和 `craft_tool` THEN LLM 倾向选择 `craft_tool`（描述更通用、与"文档交付"意图更匹配），通过生成 PowerShell/Python 截屏脚本执行，产生大量中间状态消息（"正在生成并执行脚本"、"正在保存脚本"、"正在执行脚本"等）

1.4 WHEN 用户发送包含"截图"（而非"截屏"）+ "发给我"的消息 THEN "截图"仅为 `LabelBrowser` Weak（单个 weak 关键词被 Layer 1 过滤），"发给我"仍以 Strong 胜出为 `document_delivery`，同样导致 `craft_tool` 被选择

### Expected Behavior (Correct)

2.1 WHEN 用户发送包含"截屏"或"截图"的消息 THEN UIC Layer 1 SHALL 将"截屏"和"截图"识别为与 `screenshot` 工具直接关联的关键词，不被"发给我"等文档交付关键词覆盖

2.2 WHEN UIC 识别到截屏意图 THEN 系统 SHALL 确保 `screenshot` 工具在意图分类结果的关联工具列表中，且不激活 `craft_tool` 作为截屏的替代方案

2.3 WHEN LLM 收到包含截屏关键词的用户消息 THEN LLM SHALL 直接调用内建 `screenshot` 工具一步完成截屏，而非通过 `craft_tool` 生成脚本

2.4 WHEN 用户消息同时包含截屏关键词和"发给我" THEN 系统 SHALL 优先识别截屏意图，"发给我"作为截屏后发送图片的自然表述，不应将整个消息劫持到文档交付意图

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 用户发送纯文档交付请求（如"把报告发给我"、"导出 PDF"）且不包含截屏关键词 THEN 系统 SHALL CONTINUE TO 将其分类为 `document_delivery` 意图，激活 `send_file`、`open`、`craft_tool`

3.2 WHEN 用户发送包含浏览器强关键词的截图请求（如"打开浏览器帮我截图"） THEN 系统 SHALL CONTINUE TO 将其分类为 `LabelBrowser` 意图（强关键词"浏览器"优先）

3.3 WHEN `screenshot` 工具作为 CoreTool 被包含在工具列表中 THEN 系统 SHALL CONTINUE TO 在所有消息中保持 `screenshot` 可用，不受意图分类结果影响

3.4 WHEN 用户发送不包含截屏关键词的消息 THEN UIC 关键词匹配逻辑 SHALL CONTINUE TO 按现有优先级规则（strong > weak、labelPriority）正常工作

3.5 WHEN `conditionalKeepRules` 中的 `documentDeliveryKeywords` 匹配用户消息 THEN 系统 SHALL CONTINUE TO 激活 `send_file`、`open`、`craft_tool`（截屏场景除外的文档交付场景不受影响）
