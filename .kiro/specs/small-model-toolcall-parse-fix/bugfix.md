# Bugfix Requirements Document

## Introduction

When using small models (e.g., xiaomi/mimo-v2-pro) via OpenRouter's OpenAI-compatible API, the model outputs tool calls in XML format `<tool_call>{"name":"tool_name","arguments":{...}}</tool_call>` within the `content` field of SSE streaming deltas, instead of using the standard OpenAI `tool_calls` JSON structure. The system currently does not recognize or parse this XML format, causing the raw XML tags to be displayed to the user as plain text and the embedded tool calls to never be executed. This breaks the agent loop for any model that uses this convention.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a small model outputs `<tool_call>{"name":"tool_name","arguments":{...}}</tool_call>` XML tags in the streaming content field THEN the system displays the raw `<tool_call>` and `</tool_call>` XML tags as plain text to the user in the chat

1.2 WHEN a small model outputs tool calls embedded in `<tool_call>` XML tags in the content field THEN the system does not parse them into structured `llmToolCall` objects, so `choice.Message.ToolCalls` remains empty

1.3 WHEN `choice.Message.ToolCalls` is empty because XML-format tool calls were not parsed THEN the agent loop treats the response as a final text response and never executes the requested tools

1.4 WHEN a small model outputs multiple `<tool_call>` blocks in a single response THEN none of the tool calls are parsed or executed

1.5 WHEN `<tool_call>` XML tags are split across SSE streaming chunks (e.g., `<tool_` in one chunk and `call>` in the next) THEN the partial tags are displayed as raw text to the user

### Expected Behavior (Correct)

2.1 WHEN a small model outputs `<tool_call>` XML tags in the streaming content field THEN the system SHALL suppress the `<tool_call>` and `</tool_call>` tags from being displayed to the user during streaming (similar to how `<|FunctionCallBegin|>` tags are suppressed)

2.2 WHEN a small model outputs tool calls embedded in `<tool_call>` XML tags in the content field THEN the system SHALL parse the JSON payload inside each `<tool_call>...</tool_call>` block and populate `choice.Message.ToolCalls` with the corresponding `llmToolCall` objects

2.3 WHEN XML-format tool calls are parsed and `choice.Message.ToolCalls` is populated THEN the agent loop SHALL execute the extracted tool calls normally, just as it would for standard OpenAI-format tool calls

2.4 WHEN a small model outputs multiple `<tool_call>` blocks in a single response THEN the system SHALL parse all of them and populate `choice.Message.ToolCalls` with one entry per block

2.5 WHEN `<tool_call>` XML tags are split across SSE streaming chunks THEN the system SHALL buffer partial tags (similar to the existing `thinkFilter` and `funcCallFilter` mechanisms) and correctly suppress them from display

2.6 WHEN the JSON payload inside a `<tool_call>` block is malformed THEN the system SHALL silently skip that block without crashing, and the malformed content SHALL NOT be displayed to the user

2.7 WHEN the model outputs both standard OpenAI `delta.tool_calls` AND `<tool_call>` XML tags in the same response THEN the system SHALL use the structured `delta.tool_calls` and ignore the XML tags to avoid duplicate tool execution

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a model outputs standard OpenAI `delta.tool_calls` in SSE streaming chunks (no XML tags) THEN the system SHALL CONTINUE TO parse and execute them via the existing `toolAccums` mechanism in `doOpenAILLMRequestStream`

3.2 WHEN a model outputs `` ```tool_call ... ``` `` code-block format tool calls THEN the system SHALL CONTINUE TO parse them via the existing `ParseToolCalls()` function in `tool_parser.go`

3.3 WHEN a model outputs `<|FunctionCallBegin|>...<|FunctionCallEnd|>` format THEN the system SHALL CONTINUE TO suppress them via the existing `funcCallFilter` in `llm_stream.go`

3.4 WHEN a model outputs `<think>...</think>` reasoning blocks THEN the system SHALL CONTINUE TO suppress them via the existing `thinkFilter` in `llm_stream.go`

3.5 WHEN a model outputs plain text content with no tool call markers of any kind THEN the system SHALL CONTINUE TO display the text normally and treat it as a final response

3.6 WHEN the Anthropic protocol is used THEN the system SHALL CONTINUE TO parse tool calls via the existing Anthropic SSE content_block mechanism without interference from the XML tag parser

3.7 WHEN the non-streaming (JSON) fallback path is used THEN the system SHALL CONTINUE TO parse responses correctly via `parseNonStreamOpenAIResponse`
