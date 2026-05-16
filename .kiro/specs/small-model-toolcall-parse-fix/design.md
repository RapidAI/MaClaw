# Small Model XML Tool Call Parse Fix — Bugfix Design

## Overview

Small models (e.g., xiaomi/mimo-v2-pro) via OpenRouter emit tool calls as `<tool_call>{"name":"...","arguments":{...}}</tool_call>` XML tags inside the streaming `content` field, instead of using the standard OpenAI `delta.tool_calls` JSON structure. The system currently has no awareness of this format: raw XML tags leak to the user, the JSON payloads are never parsed into `llmToolCall` objects, and the agent loop stalls.

The fix adds three layers:
1. A **streaming filter** (`toolCallFilter`) that suppresses `<tool_call>...</tool_call>` tags from the user-facing token stream during SSE, following the same state-machine pattern as `funcCallFilter`.
2. A **post-stream extraction** step that, after assembling the full content, parses any `<tool_call>` XML blocks into `llmToolCall` objects — but only when no structured `delta.tool_calls` were already received (deduplication).
3. A **content-stripping regex** (`reXMLToolCallBlock`) that removes `<tool_call>` blocks from the final `content` string, analogous to `reFuncCallBlock`.

## Glossary

- **Bug_Condition (C)**: The model outputs tool calls as `<tool_call>JSON</tool_call>` XML tags in the streaming content field, and no structured `delta.tool_calls` are present.
- **Property (P)**: XML tool call tags are suppressed from display, JSON payloads are parsed into `llmToolCall` objects, and the agent loop executes them.
- **Preservation**: All existing behavior for standard OpenAI `delta.tool_calls`, `<|FunctionCallBegin|>` markup, ` ```tool_call ``` ` code blocks, `<think>` blocks, Anthropic protocol, plain text, and non-streaming fallback must remain unchanged.
- **`toolCallFilter`**: New streaming filter in `llm_stream.go` that buffers/suppresses `<tool_call>...</tool_call>` tags (state machine with pending buffer, open/close tags).
- **`ParseXMLToolCalls()`**: New function in `tool_parser.go` that extracts `ToolCall` objects from `<tool_call>JSON</tool_call>` blocks.
- **`RemoveXMLToolCallBlocks()`**: New function in `tool_parser.go` that strips `<tool_call>...</tool_call>` blocks from content.
- **`toolAccums`**: Existing map in `doOpenAILLMRequestStream` that accumulates structured `delta.tool_calls` during SSE.

## Bug Details

### Bug Condition

The bug manifests when a small model outputs tool calls as `<tool_call>{"name":"tool_name","arguments":{...}}</tool_call>` XML tags in the SSE streaming `content` field. The system has no parser for this format: the tags are displayed as raw text, `choice.Message.ToolCalls` remains empty, and the agent loop treats the response as a final text answer.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type SSEStreamResponse
  OUTPUT: boolean

  RETURN input.contentField CONTAINS "<tool_call>"
         AND input.contentField CONTAINS "</tool_call>"
         AND input.deltaToolCalls IS EMPTY
         AND contentBetweenTags(input.contentField) IS valid JSON with "name" field
END FUNCTION
```

### Examples

- **Single tool call**: Model outputs `<tool_call>{"name":"read_file","arguments":{"path":"main.go"}}</tool_call>`. Expected: parsed into `llmToolCall{Function.Name: "read_file", ...}`, tags suppressed from display. Actual: raw XML shown to user, no tool execution.
- **Multiple tool calls**: Model outputs `<tool_call>{"name":"read_file","arguments":{"path":"a.go"}}</tool_call>\n<tool_call>{"name":"read_file","arguments":{"path":"b.go"}}</tool_call>`. Expected: two `llmToolCall` entries. Actual: raw XML shown, zero tool calls.
- **Split across chunks**: SSE delivers `<tool_` in chunk 1 and `call>{"name":"x","arguments":{}}` in chunk 2 and `</tool_call>` in chunk 3. Expected: tags buffered and suppressed. Actual: partial tags leak as text.
- **Malformed JSON**: `<tool_call>not valid json</tool_call>`. Expected: block silently skipped, not displayed. Actual: raw XML shown.
- **Deduplication**: Model sends both `delta.tool_calls` AND `<tool_call>` XML in same response. Expected: structured tool_calls used, XML ignored. Actual: N/A (currently only XML is ignored because it's not parsed at all).

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Standard OpenAI `delta.tool_calls` in SSE streaming must continue to be parsed via the existing `toolAccums` mechanism
- ` ```tool_call ... ``` ` code-block format must continue to be parsed via `ParseToolCalls()` in `tool_parser.go`
- `<|FunctionCallBegin|>...<|FunctionCallEnd|>` must continue to be suppressed via `funcCallFilter`
- `<think>...</think>` must continue to be suppressed via `thinkFilter`
- Plain text content with no tool call markers must display normally
- Anthropic protocol SSE must continue to work via `doAnthropicLLMRequestStream`
- Non-streaming JSON fallback must continue to work via `parseNonStreamOpenAIResponse`

**Scope:**
All inputs that do NOT contain `<tool_call>` XML tags in the content field should be completely unaffected by this fix. This includes:
- Standard structured `delta.tool_calls` (no content-field XML)
- `<|FunctionCallBegin|>` markup
- `<think>` reasoning blocks
- Plain text responses
- Anthropic protocol responses
- Non-streaming fallback responses

## Hypothesized Root Cause

Based on the bug description, the root cause is straightforward — the system was never designed to handle this XML tool call format:

1. **No streaming filter for `<tool_call>` tags**: The filter chain in `doOpenAILLMRequestStream` only includes `thinkFilter` and `funcCallFilter`. There is no filter that recognizes `<tool_call>` / `</tool_call>` tags, so they pass through to the user-facing token callback as raw text.

2. **No post-stream XML extraction**: After the SSE loop, the code only assembles structured `delta.tool_calls` from `toolAccums`. There is no fallback path that inspects the raw `contentBuf` for `<tool_call>` XML blocks and converts them to `llmToolCall` objects.

3. **No content-stripping regex for XML tags**: `stripFunctionCalls()` only removes `<|FunctionCallBegin|>...<|FunctionCallEnd|>` blocks. There is no equivalent regex for `<tool_call>...</tool_call>`, so even if extraction were added, the XML tags would remain in the final `content` string.

## Correctness Properties

Property 1: Bug Condition — XML Tool Calls Are Parsed and Executed

_For any_ SSE streaming response where the content field contains one or more `<tool_call>{"name":"...","arguments":{...}}</tool_call>` XML blocks AND no structured `delta.tool_calls` are present, the fixed system SHALL parse each valid XML block into an `llmToolCall` object in `choice.Message.ToolCalls`, suppress the XML tags from the user-facing token stream, and strip the XML blocks from the final `content` string.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6**

Property 2: Preservation — Non-XML-Tool-Call Behavior Unchanged

_For any_ SSE streaming response where the content field does NOT contain `<tool_call>` XML blocks (standard `delta.tool_calls`, `<|FunctionCallBegin|>` markup, `<think>` blocks, plain text, Anthropic protocol, non-streaming fallback), the fixed system SHALL produce exactly the same behavior as the original system, preserving all existing parsing, filtering, and display logic.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

Property 3: Deduplication — Structured Tool Calls Take Priority

_For any_ SSE streaming response where BOTH structured `delta.tool_calls` AND `<tool_call>` XML blocks are present in the same response, the fixed system SHALL use only the structured `delta.tool_calls` and ignore the XML blocks, preventing duplicate tool execution.

**Validates: Requirement 2.7**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/llm_stream.go`

**1. Add `toolCallFilter` streaming filter** (follows `funcCallFilter` pattern):
   - New struct `toolCallFilter` with `downstream TokenCallback`, `inside bool`, `pending strings.Builder`
   - Constants `toolCallOpen = "<tool_call>"` and `toolCallClose = "</tool_call>"`
   - State machine: outside mode forwards text and watches for `<tool_call>`; inside mode swallows text and watches for `</tool_call>`
   - Partial tag buffering via existing `partialTagTail` / `hasPartialTagSuffix` / `safeEmitLen` helpers
   - `Write(delta)` and `Flush()` methods identical in structure to `funcCallFilter`

**2. Insert `toolCallFilter` into the filter chain**:
   - In `doOpenAILLMRequestStream`, the current chain is: `onToken → funcCallFilter → thinkFilter`
   - New chain: `onToken → toolCallFilter → funcCallFilter → thinkFilter`
   - i.e., `tcf := newToolCallFilter(onToken)`, `fcf := newFuncCallFilter(func(s string) { tcf.Write(s) })`, `tf := newThinkFilter(func(s string) { fcf.Write(s) })`

**3. Add `stripXMLToolCalls` regex and function**:
   - `var reXMLToolCallBlock = regexp.MustCompile("(?s)<tool_call>.*?</tool_call>")`
   - `func stripXMLToolCalls(s string) string` — analogous to `stripFunctionCalls`
   - Apply in the post-stream content assembly: `content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))`

**4. Add post-stream XML tool call extraction with deduplication**:
   - After assembling `msg.ToolCalls` from `toolAccums`, check: if `len(msg.ToolCalls) == 0`, call `freeproxy.ParseXMLToolCalls(contentBuf.String())` on the raw content buffer
   - Convert each returned `freeproxy.ToolCall` to `llmToolCall` and append to `msg.ToolCalls`
   - If XML tool calls were found, set `finishReason = "tool_calls"`

---

**File**: `corelib/freeproxy/tool_parser.go`

**5. Add `ParseXMLToolCalls()` function**:
   - Regex: `var xmlToolCallBlockRe = regexp.MustCompile("(?s)<tool_call>\\s*(.*?)\\s*</tool_call>")`
   - Extract all matches, parse each inner JSON as `{"name":"...","arguments":{...}}`
   - Return `[]ToolCall` with generated IDs (reuse `generateToolCallID`)
   - Silently skip blocks with malformed JSON

**6. Add `RemoveXMLToolCallBlocks()` function**:
   - `func RemoveXMLToolCallBlocks(content string) string` — strips `<tool_call>...</tool_call>` blocks
   - Uses `xmlToolCallBlockRe.ReplaceAllString(content, "")`

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis.

**Test Plan**: Write tests that feed `<tool_call>` XML content through the streaming filter chain and the post-stream assembly, and assert that tool calls are extracted and tags are suppressed. Run on UNFIXED code to observe failures.

**Test Cases**:
1. **Single XML tool call**: Feed `<tool_call>{"name":"read_file","arguments":{"path":"x"}}</tool_call>` through the stream — expect tag suppression and tool call extraction (will fail on unfixed code)
2. **Multiple XML tool calls**: Feed two `<tool_call>` blocks — expect both parsed (will fail on unfixed code)
3. **Split tag across chunks**: Feed `<tool_` then `call>{"name":"x","arguments":{}}` then `</tool_call>` — expect buffering and suppression (will fail on unfixed code)
4. **Malformed JSON**: Feed `<tool_call>not json</tool_call>` — expect silent skip (will fail on unfixed code)

**Expected Counterexamples**:
- XML tags appear in user-facing output as raw text
- `msg.ToolCalls` is empty after stream assembly
- Possible cause: no filter or parser exists for `<tool_call>` format

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := doOpenAILLMRequestStream_fixed(input)
  ASSERT result.Choices[0].Message.ToolCalls IS NOT EMPTY
  ASSERT result.Choices[0].Message.Content DOES NOT CONTAIN "<tool_call>"
  ASSERT userTokenStream DOES NOT CONTAIN "<tool_call>"
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT doOpenAILLMRequestStream_original(input) = doOpenAILLMRequestStream_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for standard tool calls, think blocks, func call blocks, and plain text, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Standard delta.tool_calls preservation**: Verify structured tool call accumulation continues to work identically
2. **funcCallFilter preservation**: Verify `<|FunctionCallBegin|>` suppression is unchanged
3. **thinkFilter preservation**: Verify `<think>` suppression is unchanged
4. **Plain text preservation**: Verify text without any markers passes through unchanged
5. **Deduplication**: Verify that when both structured and XML tool calls exist, only structured are used

### Unit Tests

**File**: `gui/llm_stream_test.go`
- `TestToolCallFilter_FullBlock` — single `<tool_call>` block suppressed
- `TestToolCallFilter_TextAroundBlock` — text before/after block preserved
- `TestToolCallFilter_SplitAcrossChunks` — tag split across Write() calls
- `TestToolCallFilter_CharByChar` — extreme fragmentation
- `TestToolCallFilter_MultipleBlocks` — multiple blocks suppressed
- `TestToolCallFilter_NoMarkers` — plain text passes through
- `TestToolCallFilter_MalformedJSON` — block suppressed even with bad JSON
- `TestStripXMLToolCalls` — regex strips blocks from complete strings
- `TestToolCallAndThinkAndFuncCallFilterChained` — all three filters chained

**File**: `corelib/freeproxy/filter_funccall_test.go`
- `TestParseXMLToolCalls_Single` — single block parsed
- `TestParseXMLToolCalls_Multiple` — multiple blocks parsed
- `TestParseXMLToolCalls_MalformedJSON` — malformed blocks skipped
- `TestParseXMLToolCalls_Empty` — no blocks returns nil
- `TestRemoveXMLToolCallBlocks` — blocks stripped from content

### Property-Based Tests

- Generate random valid JSON tool call payloads wrapped in `<tool_call>` tags, verify all are parsed correctly
- Generate random strings without `<tool_call>` tags, verify `ParseXMLToolCalls` returns nil and `toolCallFilter` passes them through unchanged
- Generate mixed content with `<think>`, `<|FunctionCallBegin|>`, and `<tool_call>` tags, verify each filter handles only its own tags

### Integration Tests

- Test full `doOpenAILLMRequestStream` flow with mock SSE server returning `<tool_call>` XML in content deltas
- Test deduplication: mock SSE server returning both `delta.tool_calls` and `<tool_call>` XML
- Test filter chain ordering: content with all three tag types (`<think>`, `<|FunctionCallBegin|>`, `<tool_call>`) processed correctly
