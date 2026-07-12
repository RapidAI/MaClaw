# Tasks — Coding Session Premature Abandon Bugfix

## Exploratory Phase (Confirm Bug on Unfixed Code)

- [x] 1. Write exploratory tests to confirm bug conditions on unfixed code
  - [x] 1.1 Create test file `im_message_handler_busy_session_test.go` with test scaffolding (mock session manager, mock session with controllable status/output)
  - [x] 1.2 Write exploratory test: `TestSendAndObserve_BusySession_NoBusyHint` — call `toolSendAndObserve` with a mock session that stays `busy` for 60s, assert return value does NOT contain busy-wait guidance (expected to pass on unfixed code, confirming the bug)
  - [x] 1.3 Write exploratory test: `TestGetSessionOutput_BusyStatus_NoHint` — call `toolGetSessionOutput` with a session in `busy` status, assert return value does NOT contain a busy-state hint (expected to pass on unfixed code, confirming missing hint)
  - [x] 1.4 Write exploratory test: `TestSystemPrompt_DiscoursagesPolling` — verify system prompt contains "不要反复轮询 get_session_output" without qualification for busy sessions (expected to pass on unfixed code, confirming contradictory guidance)

## Fix Implementation Phase

- [x] 2. Extend `send_and_observe` polling duration
  - [x] 2.1 In `im_message_handler.go` function `toolSendAndObserve`, replace `waitMs` array `[500, 500, 1000, 1000, 1500, 1500, 2000]` (~8s) with extended array `[500, 500, 1000, 1000, 1500, 1500, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000, 3000]` (~30s)
  - [x] 2.2 Optionally add support for `timeout_seconds` float64 parameter in args — if provided and > 0, generate a polling array that totals approximately that many seconds (cap at 120s)

- [x] 3. Add busy-state guidance to `send_and_observe` return value
  - [x] 3.1 After the polling loop in `toolSendAndObserve`, before calling `toolGetSessionOutput`, check if session status is still `SessionBusy`. If so, set a flag `stillBusy = true`
  - [x] 3.2 After getting output from `toolGetSessionOutput`, if `stillBusy` is true, append hint: `"\n\n编程工具仍在工作中（状态: busy）。请等待 15-30 秒后调用 get_session_output(session_id=\"%s\") 检查进度。不要终止会话。"`

- [x] 4. Add busy-state hint in `toolGetSessionOutput`
  - [x] 4.1 In `toolGetSessionOutput`, after the existing `starting` state hint block (around the `} else if status == string(SessionStarting) {` block), add a new condition: when `status == string(SessionBusy)`, append hint `"\n编程工具正在工作中，请等待 15-30 秒后再次检查进度。不要终止正在工作的会话。"`

- [x] 5. Update system prompt for long-running task guidance
  - [x] 5.1 In the system prompt string (around line 1560), replace step 4 content. Change `"4. 跟踪进度：根据输出跟踪执行情况，必要时追加指令\n编程工具启动后会等待输入，不发送指令它不会开始工作。不要反复轮询 get_session_output。"` to new guidance that: (a) distinguishes quick vs long tasks, (b) instructs Agent to call `get_session_output` every 15-30s for busy sessions, (c) warns against terminating busy sessions prematurely

## Fix Verification Phase

- [x] 6. Write fix-checking tests (verify bug is fixed)
  - [x] 6.1 Write test: `TestSendAndObserve_BusySession_ReturnsBusyHint` — call `toolSendAndObserve` with mock busy session, assert return value CONTAINS busy-wait guidance string "编程工具仍在工作中"
  - [x] 6.2 Write test: `TestSendAndObserve_ExtendedPolling` — verify polling duration is ~30s by checking that the function takes at least 25s to return when session stays busy (use mock with time tracking)
  - [x] 6.3 Write test: `TestGetSessionOutput_BusyStatus_ReturnsHint` — call `toolGetSessionOutput` with busy session, assert return value CONTAINS "编程工具正在工作中"
  - [x] 6.4 Write test: `TestSystemPrompt_ContainsLongRunningGuidance` — verify updated system prompt contains guidance for periodic polling of busy sessions and does NOT contain unqualified "不要反复轮询"

## Preservation Verification Phase

- [x] 7. Write preservation tests (verify no regressions)
  - [x] 7.1 Write test: `TestSendAndObserve_ExitedSession_PreservesEarlyReturn` — verify `send_and_observe` returns immediately (< 2s) when session exits during polling, with no busy hint appended
  - [x] 7.2 Write test: `TestSendAndObserve_WaitingInput_PreservesEarlyReturn` — verify `send_and_observe` returns immediately when session enters `waiting_input`, with no busy hint
  - [x] 7.3 Write test: `TestSendAndObserve_FastOutput_PreservesEarlyReturn` — verify `send_and_observe` returns immediately when meaningful output (>1 new line) appears within first 2s
  - [x] 7.4 Write test: `TestGetSessionOutput_ExitedError_PreservesStopLoss` — verify stop-loss hint is still present for exited sessions with non-zero exit code
  - [x] 7.5 Write test: `TestGetSessionOutput_RunningNoOutput_PreservesHint` — verify "编程工具在等待输入" hint is still present for running sessions with no output
  - [x] 7.6 Write test: `TestGetSessionOutput_StartingState_PreservesHint` — verify "会话正在启动中" hint is still present for starting sessions
