# Bugfix Requirements Document

## Introduction

用户在 maclaw 编程工作流中遇到内容丢失问题：LLM 进入需求设计阶段后输出完整的需求文档，但随后 agent loop 继续迭代，LLM 重复输出了一遍确认提示（措辞不同于第一次），最终 `SendAIAssistantMessage` 返回时 `IMAgentResponse.Text` 只包含最后一轮迭代的内容。前端 `resolveFinalRoundContent()` 中改进记录 #19 的 `endsWith` 检查无法匹配（因为最后一轮的确认提示文本并非前面累积流式内容的精确后缀），导致 `response.text`（短文本）直接替换了已累积的完整流式输出（长文档），用户看到需求文档消失，只剩下简短的确认提示。

根因链路：
1. 后端 agent loop 多轮迭代，每轮 LLM 输出通过 `onToken` 回调流式推送到前端，前端将所有 token 累积到同一个 assistant 消息中
2. `IMAgentResponse.Text` 只包含最后一轮迭代的 `msgContent`（`stripThinkingTags(choice.Message.Content)`）
3. 前端 `resolveFinalRoundContent()` 的 `endsWith` 检查在以下场景失败：
   - LLM 最后一轮输出的确认提示与流式累积内容的尾部不完全匹配（措辞变化、格式差异、重复输出导致文本不同）
   - LLM 在工具调用后重新生成了一段不同的文本（不是流式累积的后缀）
4. `endsWith` 检查失败后，代码走到 `if (finalText) return finalText` 分支，用最后一轮的短文本替换了完整的多轮累积输出

增强方案概述：
- **后端侧**：在 `IMAgentResponse` 中新增 `ResponseSource` 字段，明确标识 `response.text` 的来源路径（`agent_loop` / `ask_user` / `cancel` / `file_delivery` / `screenshot` 等），让前端能精确区分"普通 agent loop 最后一轮输出"和"特殊处理路径输出"
- **前端侧**：从单一 `endsWith` 检查升级为多重判断策略（来源标记 + 长度比较 + endsWith），形成双重保护

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN agent loop 执行多轮迭代且最后一轮的 `response.text` 不是流式累积内容的精确后缀（例如 LLM 重复输出确认提示但措辞略有不同）THEN `resolveFinalRoundContent()` 的 `endsWith` 检查失败，`response.text`（短文本）替换了已累积的完整流式输出（长文档），用户看到需求文档内容消失

1.2 WHEN agent loop 在编程工作流中输出需求文档后继续迭代（因 stall/deliverable 检测误判或 NeedsConfirm gate 未及时拦截），LLM 在后续迭代中重新生成了确认提示文本 THEN 前端面板中已显示的完整需求文档被替换为简短的确认提示文本

1.3 WHEN `streamedContent.length > finalText.length` 但 `!streamedContent.endsWith(finalText)`（最后一轮输出与累积内容不是后缀关系，但也不是来自 ask_user/cancel 等特殊路径）THEN 系统错误地将 `finalText` 视为权威内容并用它替换了更完整的 `streamedContent`

1.4 WHEN 后端返回的 `response.text` 来自普通 agent loop 的最后一轮迭代 THEN 前端无法区分该文本是"普通 agent loop 片段"还是"特殊处理路径的权威内容"，因为 `IMAgentResponse` 中没有来源标记字段

1.5 WHEN 前端仅依赖 `endsWith` 单一策略判断是否保留 `streamedContent` THEN 任何导致 `endsWith` 失败的场景（措辞变化、格式差异、尾部空白不同、重复输出等）都会触发内容丢失

### Expected Behavior (Correct)

2.1 WHEN 后端构造 `IMAgentResponse` 时 THEN 系统 SHALL 在响应中包含一个 `ResponseSource` 字段，标识 `response.text` 的来源路径，可选值包括但不限于：`agent_loop`（普通 agent loop 最后一轮输出）、`ask_user`（ask_user 工具的结构化提问）、`cancel`（取消操作）、`file_delivery`（文件发送路径）、`screenshot`（截图路径）、`empty_fallback`（空结果兜底文本）

2.2 WHEN 前端 `resolveFinalRoundContent()` 判断是否保留 `streamedContent` 时 THEN 系统 SHALL 使用多重判断策略，按优先级依次为：(a) 来源标记检查——若 `response_source` 为特殊处理路径（`ask_user`/`cancel`/`file_delivery`/`screenshot`），直接使用 `response.text`；(b) 长度比较——若 `streamedContent` 长度显著大于 `response.text`（例如 2 倍以上）且来源为 `agent_loop` 或无标记，保留 `streamedContent`；(c) endsWith 检查——作为兜底的精确匹配策略

2.3 WHEN `streamedContent` 长度显著大于 `response.text`（例如 `streamedContent.length >= response.text.length * 2`）且 `response_source` 为 `agent_loop` 或未设置 THEN 系统 SHALL 始终保留 `streamedContent`，因为这表明 `response.text` 只是多轮迭代中最后一轮的片段，不应替换用户已看到的完整输出

2.4 WHEN `response_source` 为 `ask_user`、`cancel`、`file_delivery`、`screenshot` 等特殊处理路径 THEN 系统 SHALL 使用 `response.text` 作为消息内容，无论 `streamedContent` 的长度如何，因为特殊路径的文本与流式输出语义无关

2.5 WHEN 编程工作流中 LLM 输出需求文档后 agent loop 继续迭代并产生重复/变体的确认提示 THEN 系统 SHALL 保留面板中已显示的完整需求文档内容，确认提示不应覆盖文档

2.6 WHEN 后端 agent loop 的各个返回路径（正常结束、ask_user 中断、cancel、截图、文件发送、空结果兜底等）构造 `IMAgentResponse` 时 THEN 系统 SHALL 在每个路径中正确设置 `ResponseSource` 字段，确保前端能精确判断来源

### Unchanged Behavior (Regression Prevention)

3.1 WHEN `response.text` 来自 `ask_user` 工具的结构化提问（`response_source` 为 `ask_user`）THEN 系统 SHALL CONTINUE TO 使用 `response.text` 作为消息内容（因为 ask_user 的文本与流式输出无关）

3.2 WHEN `response.text` 来自取消操作（`response_source` 为 `cancel`）THEN 系统 SHALL CONTINUE TO 使用 `response.text` 作为消息内容

3.3 WHEN `response.text` 来自文件发送（`response_source` 为 `file_delivery`）或截图（`response_source` 为 `screenshot`）路径 THEN 系统 SHALL CONTINUE TO 使用 `response.text` 作为消息内容

3.4 WHEN 非流式响应（如简短闲聊，`streamedContent` 为空或与 `response.text` 相同）THEN 系统 SHALL CONTINUE TO 正常显示 `response.text`

3.5 WHEN `streamedContent` 确实以 `response.text` 结尾（改进记录 #19 的原始场景）THEN 系统 SHALL CONTINUE TO 保留 `streamedContent`（现有 endsWith 逻辑仍然有效，作为多重策略的兜底层）

3.6 WHEN `response.text` 非空但 `streamedContent` 为空（首次响应无流式内容）THEN 系统 SHALL CONTINUE TO 使用 `response.text`

3.7 WHEN `response_source` 字段未设置（兼容旧版本后端或第三方集成）THEN 系统 SHALL CONTINUE TO 使用长度比较 + endsWith 的降级策略，不因缺少标记而崩溃或行为异常
