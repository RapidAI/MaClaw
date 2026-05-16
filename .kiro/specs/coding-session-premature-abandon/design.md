# Coding Session Premature Abandon Bugfix Design

## Overview

MaClaw Agent prematurely abandons coding tool sessions (Claude Code, etc.) when complex tasks take longer than ~8 seconds, falling back to inferior write_file/bash tools. The fix addresses four root causes: insufficient polling duration in `send_and_observe`, contradictory system prompt guidance, missing busy-state hints in `get_session_output`, and lack of busy-state guidance in `send_and_observe` return values. The approach is minimal and surgical — extend wait times, add clear hints, and update prompt guidance without changing session lifecycle or architecture.

## Glossary

- **Bug_Condition (C)**: The condition that triggers premature abandonment — Agent sends a complex coding task via `send_and_observe`, the 8-second polling window expires, session is still `busy`, and Agent receives no guidance to wait
- **Property (P)**: The desired behavior — Agent receives clear guidance to periodically check progress on busy sessions, and `send_and_observe` waits long enough for initial output on most tasks
- **Preservation**: Existing behaviors that must remain unchanged — fast return on session exit/error/waiting_input, stop-loss on failed sessions, simple operations bypassing sessions
- **`send_and_observe`**: Function in `im_message_handler.go` (~line 2448) that sends text to a session and polls for output with a fixed ~8s window
- **`toolGetSessionOutput`**: Function in `im_message_handler.go` (~line 2274) that returns session status and output, with hints for `running` and `starting` states but none for `busy`
- **`SessionBusy`**: Status constant in `remote_types.go` indicating the coding tool is actively executing (e.g., TodoWrite, file writes) and not yet ready for new input

## Bug Details

### Bug Condition

The bug manifests when Agent sends a complex coding instruction via `send_and_observe` and the coding tool needs more than ~8 seconds to produce meaningful output. The `send_and_observe` function returns with partial/no output, the session is still `busy`, and neither the return value nor subsequent `get_session_output` calls provide any hint that the Agent should wait. Combined with system prompt guidance that discourages polling, the Agent concludes the session is unresponsive and abandons it.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type SendAndObserveCall
  OUTPUT: boolean
  
  RETURN input.taskComplexity > SIMPLE_THRESHOLD
         AND session.status == "busy" AFTER send_and_observe returns
         AND session.hasNoNewMeaningfulOutput WITHIN 8 seconds
         AND returnValue.containsNoBusyWaitGuidance == true
END FUNCTION
```

### Examples

- Agent sends "用 C++ 写一个贪吃蛇游戏" → Claude Code starts TodoWrite planning (takes 30-60s) → `send_and_observe` returns after 8s with only echo line → Agent sees no guidance → kills session → writes code itself with write_file
- Agent sends "重构这个模块的错误处理" → coding tool analyzes files (takes 20s) → `send_and_observe` returns with partial output → Agent calls `get_session_output`, sees `busy` status but no hint → gives up
- Agent sends "ls" (simple command) → output appears in <1s → `send_and_observe` returns immediately with full output → no issue (this is NOT a bug condition)
- Agent sends complex task → session exits with error code → `send_and_observe` returns with error hint → Agent correctly reports failure (this is NOT a bug condition)

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- `send_and_observe` MUST continue to return immediately when session status becomes `exited`, `error`, or `waiting_input` during polling
- Stop-loss behavior for failed sessions (exit code ≠ 0) must remain intact with the 🛑 hint
- Simple file/command operations must continue to use bash/read_file/write_file directly
- The `starting` state hint ("⏳ 会话正在启动中") must remain unchanged
- The `running` state hint (no output, waiting for input) must remain unchanged
- Image delivery notes in `send_and_observe` must remain unchanged

**Scope:**
All inputs where the session is NOT in `busy` state after `send_and_observe` polling should be completely unaffected. This includes:
- Sessions that produce output within the polling window
- Sessions that exit or error during polling
- Sessions waiting for user input
- Non-session tool calls (bash, read_file, write_file, etc.)

## Hypothesized Root Cause

Based on code analysis, the root causes are confirmed (not hypothesized):

1. **Insufficient polling duration in `send_and_observe`** (im_message_handler.go ~line 2480):
   - `waitMs := []int{500, 500, 1000, 1000, 1500, 1500, 2000}` totals ~8 seconds
   - Complex coding tasks (C++ game, refactoring) need 30-120+ seconds
   - After 8s, function returns with only the PTY echo line, no real output

2. **Contradictory system prompt guidance** (im_message_handler.go ~line 1545-1575):
   - Step 4 says "跟踪进度" but provides no mechanism for long-running tasks
   - "不要反复轮询 get_session_output" discourages the exact behavior needed for busy sessions
   - No distinction between quick operations and long coding tasks

3. **Missing busy-state hint in `toolGetSessionOutput`** (im_message_handler.go ~line 2274):
   - Has hints for `running` (no output) → "编程工具在等待输入"
   - Has hints for `starting` → "会话正在启动中"
   - Has hints for `exited` (error) → "🛑 会话已失败退出"
   - NO hint when status is `busy` — Agent gets raw output with no guidance

4. **Missing busy-state guidance in `send_and_observe` return**:
   - When polling ends and session is still `busy`, the return value is just the `toolGetSessionOutput` output
   - No additional note telling Agent the session is still working and to check back later

## Correctness Properties

Property 1: Bug Condition - Busy Session Wait Guidance

_For any_ `send_and_observe` call where the session remains in `busy` status after the polling window expires, the return value SHALL include a clear hint indicating the coding tool is still working and the Agent should check progress again after 15-30 seconds using `get_session_output`.

**Validates: Requirements 2.1, 2.2**

Property 2: Preservation - Non-Busy Session Behavior

_For any_ `send_and_observe` call where the session exits, errors, produces meaningful output, or enters `waiting_input` state during the polling window, the function SHALL return the same result as the original implementation, preserving all existing early-return and hint behaviors.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**

## Fix Implementation

### Changes Required

**File**: `im_message_handler.go`

**Change 1: Extend `send_and_observe` polling duration** (~line 2480)

Replace the current 8-second polling array with a longer default (~30 seconds), using escalating intervals:
- Current: `[500, 500, 1000, 1000, 1500, 1500, 2000]` = ~8s
- New: `[500, 500, 1000, 1000, 1500, 1500, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000, 3000]` = ~30s
- Optionally support a `timeout_seconds` parameter in args to allow the LLM to request longer waits

**Change 2: Add busy-state guidance to `send_and_observe` return** (~after line 2510)

After the polling loop and before returning, check if session is still `busy`. If so, append a hint:
```
⏳ 编程工具仍在工作中（状态: busy）。请等待 15-30 秒后调用 get_session_output(session_id="...") 检查进度。不要终止会话。
```

**Change 3: Add busy-state hint in `toolGetSessionOutput`** (~line 2350, after the `starting` hint block)

When session status is `busy`, add a hint similar to the existing `starting` and `running` hints:
```
⏳ 编程工具正在工作中，请等待 15-30 秒后再次检查进度。不要终止正在工作的会话。
```

**Change 4: Update system prompt for long-running task guidance** (~line 1560)

Replace the contradictory step 4 and polling warning with clear guidance:
- Distinguish between quick operations and long coding tasks
- For busy sessions: "每 15-30 秒调用一次 get_session_output 检查进度是正常的"
- Remove or qualify "不要反复轮询 get_session_output" to only apply to exited/error sessions

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm the root cause analysis.

**Test Plan**: Write unit tests that simulate `send_and_observe` with a session that stays `busy` for >8 seconds and verify the return value lacks busy-wait guidance. Run on UNFIXED code to observe failures.

**Test Cases**:
1. **Long-running task test**: Call `send_and_observe` with a mock session that stays `busy` for 60s — verify return value contains no busy-wait hint (will fail to find hint on unfixed code)
2. **get_session_output busy test**: Call `toolGetSessionOutput` with a `busy` session — verify no busy hint is present (will confirm missing hint on unfixed code)
3. **System prompt test**: Verify system prompt contains "不要反复轮询" without qualification for busy sessions (will confirm contradictory guidance on unfixed code)

**Expected Counterexamples**:
- `send_and_observe` returns after ~8s with no busy-wait guidance
- `toolGetSessionOutput` returns status `busy` with no actionable hint
- System prompt discourages polling without exception for busy sessions

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed functions produce the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := send_and_observe_fixed(input)
  ASSERT result.containsBusyWaitHint == true
  ASSERT result.pollingDuration >= 30 seconds
  
  outputResult := get_session_output_fixed(busy_session)
  ASSERT outputResult.containsBusyHint == true
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed functions produce the same result as the original.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT send_and_observe_original(input) == send_and_observe_fixed(input)
  ASSERT get_session_output_original(input) == get_session_output_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many session states and input combinations automatically
- It catches edge cases in status transitions that manual tests might miss
- It provides strong guarantees that non-busy behavior is unchanged

**Test Plan**: Observe behavior on UNFIXED code first for non-busy sessions, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Exited session preservation**: Verify `send_and_observe` returns immediately with error hint when session exits during polling — same behavior before and after fix
2. **Waiting-input preservation**: Verify `send_and_observe` returns immediately when session enters `waiting_input` — same behavior before and after fix
3. **Fast output preservation**: Verify `send_and_observe` returns immediately when meaningful output appears within first few seconds — same behavior before and after fix
4. **Stop-loss preservation**: Verify `toolGetSessionOutput` still shows 🛑 hint for exited sessions with non-zero exit code

### Unit Tests

- Test `send_and_observe` polling duration is ~30s with mock busy session
- Test `send_and_observe` appends busy-wait hint when session is still busy after polling
- Test `send_and_observe` does NOT append busy-wait hint when session exits/errors/produces output
- Test `toolGetSessionOutput` includes busy hint when status is `busy`
- Test `toolGetSessionOutput` does NOT include busy hint for other statuses
- Test system prompt contains long-running task guidance
- Test optional `timeout_seconds` parameter (if implemented)

### Property-Based Tests

- Generate random session states (starting, running, busy, waiting_input, exited, error) and verify hints are correct for each state
- Generate random polling scenarios (output at various times, status transitions) and verify early-return behavior is preserved
- Generate random exit codes and verify stop-loss hint is preserved for non-zero codes

### Integration Tests

- Test full flow: create session → send complex task → verify Agent receives busy-wait guidance → poll with get_session_output → verify busy hint → session completes → verify final output
- Test that simple operations (bash, read_file) are unaffected by changes
- Test session that transitions from busy → waiting_input during polling returns correctly
