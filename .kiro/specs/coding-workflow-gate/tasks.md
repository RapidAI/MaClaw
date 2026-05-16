# Tasks

## Task 1: Create coding tool gate core module

- [x] 1.1 Create `gui/coding_tool_gate.go` with `codingToolGateConfig` and `codingToolGateResult` structs
- [x] 1.2 Implement `codingToolBlocklist` map and `deliveryToolAllowlist` map with all specified tool names
- [x] 1.3 Implement `isCodingTool(name string) bool` — returns true iff name is in blocklist and not in allowlist
- [x] 1.4 Implement `containsSkipSignal(text string) bool` — case-insensitive substring matching against Chinese and English skip signal lists
- [x] 1.5 Implement `newCodingToolGateConfig(userText string, loopKind LoopKind) codingToolGateConfig` — calls `classifyTaskIntent`, checks skip signal, checks loop kind, returns config with active/intent/skipSignal/reason fields
- [x] 1.6 Implement `applyCodingToolGate(calls []llm.ToolCall) codingToolGateResult` — partitions tool calls into stripped (coding) and remaining (non-coding), sets applied=true if any were stripped

## Task 2: Integrate gate into runAgentLoop

- [x] 2.1 In `runAgentLoop` (gui/im_message_handler.go), add `gateConfig := newCodingToolGateConfig(userText, ctx.Kind)` before the iteration loop, after conversation construction
- [x] 2.2 After LLM response parsing and assistantMsg construction, before the tool execution loop, add gate check: if `iteration == 0 && gateConfig.active && len(choice.Message.ToolCalls) > 0`, call `applyCodingToolGate` and update `choice.Message.ToolCalls` and `assistantMsg["tool_calls"]`
- [x] 2.3 Handle the "all tools stripped, text non-empty" case: when `gateResult.applied` and `len(gateResult.remaining) == 0` and text content is non-empty, skip the tool execution loop and let the existing no-tool-call branch return the text as final response
- [x] 2.4 Handle the "all tools stripped, text empty" case: inject a system message prompting the LLM to generate the requirements document and `continue` the loop
- [x] 2.5 Add INFO-level log when gate activates (stripped tool names, preserved tool names, reason)
- [x] 2.6 Add DEBUG-level log when gate is evaluated but does not activate (reason: non-coding intent / skip signal / iteration > 0)
- [x] 2.7 Add trace event `gate.coding_tool_stripped` when trace service is available and gate activates

## Task 3: Property-based tests

- [x] 3.1 Create `gui/coding_tool_gate_property_test.go`
- [x] 3.2 [PBT] Property 1: Tool stripping correctness — generate random tool call lists, verify applyCodingToolGate partitions correctly (stripped = coding tools, remaining = non-coding tools, order preserved)
- [x] 3.3 [PBT] Property 2: Text content preservation — generate random text + tool calls, verify gate does not modify text
- [x] 3.4 [PBT] Property 3: Gate inactivity — generate random inactive configs (non-coding intent / skip signal / background), verify no tools stripped
- [x] 3.5 [PBT] Property 4: Tool classification determinism — generate random tool names, verify isCodingTool matches blocklist/allowlist membership
- [x] 3.6 [PBT] Property 5: Skip signal detection — generate random messages with embedded skip signals (random case, random prefix/suffix), verify containsSkipSignal returns true

## Task 4: Unit tests

- [x] 4.1 Create `gui/coding_tool_gate_test.go` with example-based tests
- [x] 4.2 Test blocklist contains all 7 specified coding tools
- [x] 4.3 Test allowlist contains all 6 specified delivery tools
- [x] 4.4 Test `newCodingToolGateConfig` returns active=true for intentCoding + LoopKindChat + no skip signal
- [x] 4.5 Test `newCodingToolGateConfig` returns active=false for intentAmbiguous, intentSSH, intentNonCoding, intentUnknown
- [x] 4.6 Test `newCodingToolGateConfig` returns active=false for LoopKindBackground regardless of intent
- [x] 4.7 Test `newCodingToolGateConfig` returns active=false when skip signal is present
- [x] 4.8 Test gate strips coding tools but preserves delivery tools in a mixed list
- [x] 4.9 Test gate handles empty tool call list (no-op)
- [x] 4.10 Test gate handles list with only delivery tools (no stripping)
- [x] 4.11 Test gate handles list with only coding tools (all stripped)
