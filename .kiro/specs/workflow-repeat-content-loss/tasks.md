# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - Agent Loop Content Loss When Streamed Content Is Significantly Longer
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the bug exists in `resolveFinalRoundContent()`
  - **Scoped PBT Approach**: For this deterministic bug, scope the property to concrete failing cases where `streamedContent` is non-empty, `finalText` is non-empty, `streamedContent.length >= finalText.length * 2`, `!streamedContent.endsWith(finalText)`, and `response_source` is `'agent_loop'` or undefined
  - **Test file**: `gui/frontend/src/components/ai/__tests__/resolveFinalRoundContent.bugcondition.test.ts`
  - **Test cases from Bug Condition in design**:
    - Case 1: Long document (3000 chars) + short non-suffix confirmation (200 chars), no `response_source` → assert returns `streamedContent` (will FAIL on unfixed code, returning `finalText` instead)
    - Case 2: `streamedContent` ends with "请确认需求文档", `finalText` = "请查看并确认上述需求" (wording variation) → assert returns `streamedContent` (will FAIL)
    - Case 3: Trailing whitespace difference — `streamedContent` ends with `\n\n`, `finalText` ends with `\n` → assert returns `streamedContent` (will FAIL)
    - Case 4: `response_source` = `'agent_loop'` with long streamed content and short non-suffix final text → assert returns `streamedContent` (will FAIL)
  - The test assertions match Expected Behavior Properties from design: `resolveFinalRoundContent` SHALL return `streamedContent` when bug condition holds
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS (this is correct - it proves the bug exists)
  - Document counterexamples found: `resolveFinalRoundContent` returns `finalText` (short text) instead of `streamedContent` (long document) because `endsWith` fails and code falls through to `if (finalText) return finalText`
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.3, 1.5, 2.2, 2.3, 2.5_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - Special Source Paths, Non-Streaming, and EndsWith Behavior
  - **IMPORTANT**: Follow observation-first methodology
  - **Test file**: `gui/frontend/src/components/ai/__tests__/resolveFinalRoundContent.preservation.test.ts`
  - **Observe behavior on UNFIXED code** for non-buggy inputs (cases where `isBugCondition` returns false):
    - Observe: Special source `ask_user` with long `streamedContent` and short `finalText` → returns `finalText` on unfixed code
    - Observe: Special source `cancel` with any `streamedContent`/`finalText` → returns `finalText` on unfixed code
    - Observe: Special source `file_delivery` → returns `finalText` on unfixed code
    - Observe: Special source `screenshot` → returns `finalText` on unfixed code
    - Observe: Empty `streamedContent` + non-empty `finalText` → returns `finalText` on unfixed code
    - Observe: `streamedContent.endsWith(finalText)` and `streamedContent.length > finalText.length` → returns `streamedContent` on unfixed code
    - Observe: Missing `response_source` (undefined) with `endsWith` match → returns `streamedContent` on unfixed code
    - Observe: Missing `response_source` with empty `streamedContent` → returns `finalText` on unfixed code
  - **Write property-based tests capturing observed behavior patterns** from Preservation Requirements in design:
    - Property 2a: For all inputs with `response_source` in `['ask_user', 'cancel', 'file_delivery', 'screenshot']`, result equals `finalText` (from Correctness Property 2)
    - Property 2b: For all inputs with empty `streamedContent` and non-empty `finalText`, result equals `finalText` (from Correctness Property 3)
    - Property 2c: For all inputs where `streamedContent.endsWith(finalText)` and `streamedContent.length > finalText.length`, result equals `streamedContent` (from Correctness Property 4)
    - Property 2d: For all inputs with undefined/empty `response_source`, function does not throw and produces consistent result via length + endsWith degraded strategy (from Correctness Property 5)
  - Property-based testing generates many test cases for stronger preservation guarantees
  - Run tests on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (this confirms baseline behavior to preserve)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

- [x] 3. Fix for agent loop content loss when streamed content is significantly longer than response text

  - [x] 3.1 Add `ResponseSource` field to `IMAgentResponse` struct
    - In `gui/im_message_handler.go`, add `ResponseSource string \`json:"response_source,omitempty"\`` to `IMAgentResponse`
    - _Bug_Condition: isBugCondition(input) where streamedContent.length >= finalText.length * 2 AND !streamedContent.endsWith(finalText) AND (responseSource is undefined OR responseSource == 'agent_loop')_
    - _Requirements: 2.1, 2.6_

  - [x] 3.2 Set `ResponseSource` on all agent loop return paths
    - NeedsConfirm gate path: `ResponseSource: "agent_loop"` (around line 4391)
    - Hard cap path: `ResponseSource: "agent_loop"` (around line 4410)
    - Normal finalize path: `ResponseSource: "agent_loop"` (around line 4510)
    - Capability gap path: `ResponseSource: "agent_loop"` (around line 4495)
    - _Requirements: 2.1, 2.6_

  - [x] 3.3 Set `ResponseSource` on all special handler return paths
    - `ask_user` path (ParseAskUserResult success): `ResponseSource: "ask_user"`
    - `cancel` paths (all ~5 cancelMsg returns): `ResponseSource: "cancel"`
    - `screenshot` paths (screenshotAlreadySent, saveScreenshotToFile): `ResponseSource: "screenshot"`
    - `file_delivery` path (pendingFiles handling): `ResponseSource: "file_delivery"`
    - `empty_fallback` path (max iterations reached): `ResponseSource: "empty_fallback"`
    - Other special paths (Deferred, conversation reset, short chat): set corresponding `ResponseSource`
    - _Requirements: 2.1, 2.4, 2.6_

  - [x] 3.4 Upgrade `resolveFinalRoundContent` to three-layer strategy
    - In `gui/frontend/src/components/ai/useAIAssistant.ts`, modify `resolveFinalRoundContent()`:
    - Layer 1 — Source check: extract `response_source` from `response`; if `response_source` is `'ask_user'`/`'cancel'`/`'file_delivery'`/`'screenshot'`, return `finalText` immediately
    - Layer 2 — Length comparison: if `streamedContent` is non-empty AND `streamedContent.length >= finalText.length * 2` AND (`response_source` is `'agent_loop'` or undefined/empty), return `streamedContent`
    - Layer 3 — endsWith fallback: keep existing `endsWith` logic as final check
    - Subsequent fallbacks unchanged: `hasVisibleTerminalPayload`, `isFailedTerminalTraceStatus`, empty content fallback
    - _Bug_Condition: isBugCondition(input) from design — streamedContent non-empty, finalText non-empty, length ratio >= 2x, not endsWith, source is agent_loop or undefined_
    - _Expected_Behavior: when isBugCondition holds, return streamedContent (preserve complete multi-round output)_
    - _Preservation: special source paths return finalText; non-streaming returns finalText; endsWith match returns streamedContent; missing source degrades gracefully_
    - _Requirements: 2.2, 2.3, 2.4, 2.5, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

  - [x] 3.5 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - Agent Loop Content Preserved
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior
    - When this test passes, it confirms the expected behavior is satisfied
    - Run bug condition exploration test from step 1: `gui/frontend/src/components/ai/__tests__/resolveFinalRoundContent.bugcondition.test.ts`
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed — `resolveFinalRoundContent` now returns `streamedContent` for bug condition inputs)
    - _Requirements: 2.2, 2.3, 2.5_

  - [x] 3.6 Verify preservation tests still pass
    - **Property 2: Preservation** - All Non-Buggy Behavior Unchanged
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run preservation property tests from step 2: `gui/frontend/src/components/ai/__tests__/resolveFinalRoundContent.preservation.test.ts`
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all preservation tests still pass after fix:
      - Special source paths (`ask_user`/`cancel`/`file_delivery`/`screenshot`) still return `finalText`
      - Non-streaming responses still return `finalText`
      - `endsWith` matching still returns `streamedContent`
      - Missing `response_source` degrades gracefully without errors

- [x] 4. Checkpoint - Ensure all tests pass
  - Run full test suite: existing 69 useAIAssistant tests + new bug condition test + new preservation tests
  - Verify no regressions in existing test suite
  - Verify `IMAgentResponse.ResponseSource` JSON serialization works correctly (Go unit tests if applicable)
  - Verify backward compatibility when `response_source` is missing from response JSON
  - Ensure all tests pass, ask the user if questions arise
