# Bug: 文件上传时历史图片被当作当前上传一起处理

## 问题描述

用户在 AI 助手面板上传 2 张图片并问"图中有什么？"，maclaw 却识别出 4 张图片——把上一轮对话中上传的 `logo108.png` 和 `rapidai.png` 也当作本次上传一起处理了。

## 根因分析

### 触发路径

1. **第一轮对话**：用户上传 `logo108.png` + `rapidai.png`，前端通过 `buildOutgoingMessageMulti()` 将文件路径嵌入消息文本：
   ```
   图中有什么？

   [用户选择的本地文件路径]
   C:\Users\ma139\Pictures\logo108.png
   C:\Users\ma139\Pictures\rapidai.png
   这是用户已经提供的本地文件。图片文件不要调用 screenshot 或重新截图；请直接使用这些路径。
   ```
   同时，如果模型支持 vision，`buildUserContent()` 将图片 base64 数据嵌入为 multimodal content blocks（`image_url` 类型）。

2. **对话历史持久化**：整个 user message（包含 `[用户选择的本地文件路径]` 文本段 + base64 图片数据）作为 `conversationEntry` 存入 `conversationMemory`。

3. **第二轮对话**：用户上传 `屏幕截图 2025-10-18 171208.png` + `屏幕截图 2025-10-20 063747.png`。

4. **构建 LLM conversation**（`im_message_handler.go` 第 4654-4661 行）：
   ```go
   for _, entry := range history {
       conversation = append(conversation, entry.toMessage())  // ← 包含第一轮的图片
   }
   userContent := buildUserContent(userText, attachments, ...)
   conversation = append(conversation, ...)  // ← 当前轮的图片
   ```

5. **LLM 看到的 context**：
   - 历史 user message：`[用户选择的本地文件路径]\nlogo108.png\nrapidai.png` + 2 张 base64 图片
   - 当前 user message：`[用户选择的本地文件路径]\n屏幕截图...171208.png\n屏幕截图...063747.png` + 2 张 base64 图片
   - LLM 无法区分"历史上传"和"当前上传"，把 4 张图片全部当作当前请求处理

### 核心问题

`conversationEntry.toMessage()` 原样返回历史消息的完整内容，不对旧消息中的图片数据做任何处理。对话历史中的 multimodal content（base64 图片）和 `[用户选择的本地文件路径]` 文本段在后续轮次中仍然完整保留，LLM 将其与当前消息的图片混为一谈。

## 修复方案

在构建 LLM conversation 时，对**历史消息**（非当前轮）中的图片相关内容进行清理：

### 1. 剥离历史消息中的 base64 图片数据

在 `runAgentLoop()` 构建 conversation 的循环中，对历史 user 消息的 multimodal content 做处理：
- `image_url` 类型的 block → 替换为文本 `[之前上传的图片]`
- `image` 类型的 block（Anthropic 格式）→ 同上
- 保留 `text` 类型的 block 不变

这样 LLM 知道之前有图片上传过（保留上下文连贯性），但不会把旧图片当作当前请求的一部分。

### 2. 标注历史消息中的 `[用户选择的本地文件路径]` 段

对历史 user 消息中的文本内容，将 `[用户选择的本地文件路径]` 替换为 `[之前选择的本地文件路径（仅供参考，非本次上传）]`，防止 LLM 将旧文件路径与当前上传混淆。

## 修改文件

- `gui/im_attachment.go`：新增 `stripHistoryImageData()` 函数，处理历史消息中的图片数据和文件路径标注
- `gui/im_message_handler.go`：在 `runAgentLoop()` 构建 conversation 时，对历史消息调用 `stripHistoryImageData()`
- `gui/im_attachment_strip_test.go`：新增 6 个测试用例覆盖 OpenAI/Anthropic multimodal、纯文本、assistant 消息等场景

## 验收标准

- 用户第一轮上传 2 张图片 → LLM 正确识别 2 张
- 用户第二轮上传 2 张新图片 → LLM 只处理当前 2 张，不混入第一轮的图片
- 第一轮的图片在历史中显示为 `[之前上传的图片]`，保持上下文连贯
- 对话历史中的文本内容不受影响
- 非图片附件（PDF、文档等）的历史引用不受影响
