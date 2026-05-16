# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - IM/Agent 渠道在 RemoteEnabled=false 时被拦截
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the bug exists
  - **Scoped PBT Approach**: Scope the property to concrete failing cases: LaunchSource ∈ {mobile, ai, handoff} with RemoteEnabled=false and tool ∈ {claude, codex, opencode, gemini}
  - Create test file `remote_mobile_launch_bugfix_test.go`
  - Set up a minimal `App` with `RemoteEnabled=false` in config and at least one project configured
  - For each non-desktop LaunchSource (`RemoteLaunchSourceMobile`, `RemoteLaunchSourceAI`, `RemoteLaunchSourceHandoff`), call `StartRemoteSessionForProject()` with a `RemoteStartSessionRequest` that sets `LaunchSource` to the non-desktop source and `Tool` to a supported coding tool
  - Assert that the call does NOT return the "remote mode is disabled" error (expected behavior from design)
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS because `StartRemoteSessionForProject()` unconditionally checks `cfg.RemoteEnabled` and returns "remote mode is disabled" for all sources - this confirms the bug exists
  - Document counterexamples: e.g., `StartRemoteSessionForProject(RemoteStartSessionRequest{Tool:"claude", LaunchSource:"ai"})` returns "remote mode is disabled" when it should proceed
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - 桌面端 RemoteEnabled 检查行为不变
  - **IMPORTANT**: Follow observation-first methodology
  - Create test in `remote_mobile_launch_preservation_test.go`
  - Observe on UNFIXED code: `StartRemoteSessionForProject()` with `LaunchSource=""` (empty/default) or `LaunchSource="desktop"` and `RemoteEnabled=false` returns "remote mode is disabled" error
  - Observe on UNFIXED code: `StartRemoteSession("claude", dir, false, "")` with `RemoteEnabled=false` returns "remote mode is disabled" error
  - Observe on UNFIXED code: `StartRemoteHandoffSession("claude", dir, false, "")` with `RemoteEnabled=false` returns "remote mode is disabled" error
  - Write property-based tests: for all desktop-source requests with `RemoteEnabled=false`, all three functions SHALL return "remote mode is disabled" error
  - Write property-based tests: for all requests (any source) with `RemoteEnabled=true`, functions SHALL NOT return "remote mode is disabled" error (they may fail for other reasons like missing infra, but not due to RemoteEnabled check)
  - Verify tests PASS on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (confirms baseline desktop behavior to preserve)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.4_

- [x] 3. Fix: Add LaunchSource awareness to session creation functions

  - [x] 3.1 Conditionally check RemoteEnabled in `StartRemoteSessionForProject()`
    - In `remote_mobile_launch.go`, modify the `if !cfg.RemoteEnabled` guard in `StartRemoteSessionForProject()` to only apply when `req.LaunchSource` is empty or `RemoteLaunchSourceDesktop`
    - Use `normalizeRemoteLaunchSource(req.LaunchSource)` to determine effective source; if result is `RemoteLaunchSourceDesktop`, enforce `RemoteEnabled` check; otherwise skip it
    - The `RemoteStartSessionRequest` struct already has a `LaunchSource` field - no struct change needed
    - _Bug_Condition: isBugCondition(input) where input.LaunchSource ∈ {mobile, ai, handoff} AND config.RemoteEnabled == false_
    - _Expected_Behavior: Non-desktop sources skip RemoteEnabled check and proceed to session creation_
    - _Preservation: Desktop sources (or empty LaunchSource) continue to enforce RemoteEnabled check_
    - _Requirements: 2.1, 3.1_

  - [x] 3.2 Add `launchSource` parameter to `StartRemoteSession()` in `remote_status.go`
    - Change signature from `StartRemoteSession(toolName, projectDir string, useProxy bool, provider string)` to `StartRemoteSession(toolName, projectDir string, useProxy bool, provider string, launchSource RemoteLaunchSource)`
    - Conditionally check `RemoteEnabled` only when `normalizeRemoteLaunchSource(launchSource)` is `RemoteLaunchSourceDesktop`
    - Set `spec.LaunchSource` from the parameter
    - _Bug_Condition: isBugCondition(input) where input.launchSource ∈ {mobile, ai, handoff} AND config.RemoteEnabled == false_
    - _Expected_Behavior: Non-desktop sources skip RemoteEnabled check_
    - _Preservation: Desktop sources continue to enforce RemoteEnabled check_
    - _Requirements: 2.2, 3.1_

  - [x] 3.3 Add `launchSource` parameter to `StartRemoteHandoffSession()` in `remote_status.go`
    - Change signature from `StartRemoteHandoffSession(toolName, projectDir string, useProxy bool, provider string)` to `StartRemoteHandoffSession(toolName, projectDir string, useProxy bool, provider string, launchSource RemoteLaunchSource)`
    - Conditionally check `RemoteEnabled` only when `normalizeRemoteLaunchSource(launchSource)` is `RemoteLaunchSourceDesktop`
    - Set `spec.LaunchSource` from the parameter
    - _Bug_Condition: isBugCondition(input) where input.launchSource ∈ {mobile, ai, handoff} AND config.RemoteEnabled == false_
    - _Expected_Behavior: Non-desktop sources skip RemoteEnabled check_
    - _Preservation: Desktop sources continue to enforce RemoteEnabled check_
    - _Requirements: 2.3, 3.1_

  - [x] 3.4 Update `toolCreateSession()` in `im_message_handler.go` to pass LaunchSource
    - In the `RemoteStartSessionRequest` construction, set `LaunchSource: RemoteLaunchSourceAI` to identify the request as coming from the IM/Agent channel
    - _Requirements: 2.1, 2.4_

  - [x] 3.5 Update all other call sites of `StartRemoteSession()` and `StartRemoteHandoffSession()`
    - `StartRemoteClaudeSession()` in `remote_status.go`: pass `RemoteLaunchSourceDesktop` to `StartRemoteSession()`
    - `RunRemoteToolSmoke()` in `remote_status.go`: pass `RemoteLaunchSourceDesktop` to `StartRemoteSession()`
    - `remote_smoke.go` main: pass `RemoteLaunchSourceDesktop` to `StartRemoteSession()`
    - Any other callers found via grep: pass appropriate `LaunchSource` (desktop for UI callers, specific source for IM/agent callers)
    - Update existing tests in `remote_status_test.go` to pass `RemoteLaunchSourceDesktop` as the new parameter
    - _Preservation: All existing desktop call sites continue to enforce RemoteEnabled check_
    - _Requirements: 3.1, 3.2_

  - [x] 3.6 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - IM/Agent 渠道跳过 RemoteEnabled 检查
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior
    - When this test passes, it confirms the expected behavior is satisfied
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed)
    - _Requirements: 2.1, 2.2, 2.3, 2.4_

  - [x] 3.7 Verify preservation tests still pass
    - **Property 2: Preservation** - 桌面端 RemoteEnabled 检查行为不变
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all tests still pass after fix (no regressions)

- [x] 4. Checkpoint - Ensure all tests pass
  - Run full test suite: `go test -run "TestBugCondition|TestPreservation" -v`
  - Ensure existing tests in `remote_status_test.go` still pass with updated signatures
  - Ensure all tests pass, ask the user if questions arise.
