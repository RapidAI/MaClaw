# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** — Steering-Driven Coding Workflow Missing UI Events
  - **CRITICAL**: This test MUST FAIL on unfixed code — failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior — it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the bug exists in `gui/im_message_handler.go`
  - **Scoped PBT Approach**: Scope the property to concrete failing cases — coding task messages (e.g., "开发一个贪吃蛇游戏") going through the steering-driven agent loop path where `handleWorkflowInterception` returns nil and no active `WorkflowEngine` workflow exists for the user
  - **Bug Condition from design**: `isBugCondition(input)` where `isCodingTask AND isSteeringPath AND hasWorkflowDocOutput` — the message matches coding keywords, the execution path is steering-driven (no active workflow engine workflow), and tool calls include `write_file` with workflow doc patterns or `generate_pdf` with workflow content
  - Test file: `gui/im_message_handler_steering_workflow_test.go`
  - Test case 1: Simulate a coding task message "开发一个贪吃蛇游戏" where `handleWorkflowInterception` returns nil, then a `write_file(path="需求文档_贪吃蛇.md", content="# 需求文档\n...")` tool call occurs — assert that `EmitSuggestMaximize` is called once AND `EmitDocUpdate` is called with phaseID="requirements" and the document content
  - Test case 2: Simulate `write_file(path="技术设计_贪吃蛇.md")` tool call — assert `EmitDocUpdate` is called with phaseID="design"
  - Test case 3: Simulate `generate_pdf` tool call with requirements document markdown_content — assert `EmitDocUpdate` is called with phaseID="requirements"
  - Test case 4: Verify `EmitSuggestMaximize` is emitted only once even when multiple tool calls occur (duplicate emission prevention via `suggestMaximizeEmitted` flag)
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Tests FAIL (this is correct — it proves the bug exists: no `SteeringWorkflowDetector` exists yet, so no events are emitted for steering-driven coding workflows)
  - Document counterexamples found (e.g., "EmitSuggestMaximize never called", "EmitDocUpdate never called because no detection mechanism in agent loop")
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.1, 1.2, 2.1, 2.2_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** — Non-Coding Tasks, Background Messages, and Workflow Engine Path Unchanged
  - **IMPORTANT**: Follow observation-first methodology
  - **Observe on UNFIXED code first**:
    - Observe: Non-coding task messages ("翻译这个文件", "整理一下资料", "你好") produce zero workflow events — no `EmitSuggestMaximize`, no `EmitDocUpdate`
    - Observe: Background messages (`msg.IsBackground == true`) with any content produce zero workflow events
    - Observe: Coding tasks handled by workflow engine path (`handleWorkflowInterception` returns non-nil) continue to emit events through the existing `WorkflowEngine` → `GUIWorkflowAdapter` path
  - Test file: `gui/im_message_handler_steering_workflow_test.go` (same file, separate test functions)
  - Write property-based tests using `testing/quick` or table-driven approach:
    - Property: For all non-coding task messages (generated from a corpus of non-coding phrases: translations, summaries, greetings, file operations), the `SteeringWorkflowDetector` SHALL NOT activate — zero `EmitSuggestMaximize` events, zero `EmitDocUpdate` events
    - Property: For all messages with `IsBackground == true` (regardless of content — coding or non-coding), zero workflow events are emitted
    - Property: When `workflowEngine` has an active workflow for the user, the `SteeringWorkflowDetector` SHALL NOT activate (defers to existing engine path)
  - Preservation Requirements from design: workflow engine path features (phase updates, quality gates, doc updates) unchanged; non-coding tasks don't trigger workflow UI; `userClosedRef` mechanism unchanged; background messages don't trigger doc preview
  - Run tests on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS (this confirms baseline behavior to preserve — non-coding tasks and background messages already produce zero workflow events)
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [x] 3. Implement SteeringWorkflowDetector and agent loop integration

  - [x] 3.1 Create SteeringWorkflowDetector struct and coding task detection
    - Add `SteeringWorkflowDetector` struct to `gui/im_message_handler.go` with fields: `detected bool`, `suggestMaximizeEmitted bool`, `phaseDocuments map[string]string`, `userID string`
    - Implement `NewSteeringWorkflowDetector(userID string)` constructor
    - Implement `isCodingTask(message string) bool` method using keyword matching (开发、编写、实现、创建、修改代码、重构、修 bug、设计架构、添加功能、新增功能)
    - Implement `matchPhaseID(fileName string) string` method with pattern matching: `需求文档`/`需求分析`/`requirements` → "requirements", `技术设计`/`设计文档`/`design`/`架构设计` → "design", `任务拆分`/`任务列表`/`tasks`/`task_breakdown` → "tasks"
    - Activation guard: only activate when `!msg.IsBackground` AND no active workflow engine workflow exists for the user
    - _Bug_Condition: isBugCondition(input) where isCodingTask AND isSteeringPath AND hasWorkflowDocOutput_
    - _Expected_Behavior: SteeringWorkflowDetector detects coding tasks and classifies phase documents_
    - _Preservation: Non-coding tasks return false from isCodingTask; background messages blocked by !msg.IsBackground guard_
    - _Requirements: 1.1, 1.2, 2.1, 2.2, 3.2, 3.5_

  - [x] 3.2 Integrate detector into runAgentLoop for event emission
    - In `runAgentLoop`, before the first iteration, create `SteeringWorkflowDetector` if `isCodingTask(userMessage)` AND `!msg.IsBackground` AND no active workflow engine workflow
    - On first tool call when detector is active: emit `workflow:suggest_maximize` via `GUIWorkflowAdapter.EmitSuggestMaximize` (once only, guarded by `suggestMaximizeEmitted`)
    - After each tool execution, intercept `write_file` and `generate_pdf` tool calls:
      - For `write_file`: extract `path` arg, call `matchPhaseID(path)`, if non-empty extract `content` arg and call `GUIWorkflowAdapter.EmitDocUpdate(userID, phaseID, content)`
      - For `generate_pdf`: extract `markdown_content` arg, call `matchPhaseID` on inferred phase from content patterns, emit `EmitDocUpdate`
    - Reuse existing `GUIWorkflowAdapter.EmitDocUpdate` and `EmitSuggestMaximize` methods directly (they already emit events independently of WorkflowEngine state)
    - _Bug_Condition: isBugCondition(input) — steering path coding tasks with workflow doc tool calls_
    - _Expected_Behavior: workflow:suggest_maximize emitted once on first tool call; workflow:doc_update emitted for each phase document with correct phaseID and content_
    - _Preservation: Workflow engine path unaffected (detector not created when engine has active workflow); non-coding tasks unaffected (detector not created); background messages unaffected (!msg.IsBackground guard)_
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 3.1, 3.2, 3.3, 3.5_

  - [x] 3.3 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** — Steering-Driven Coding Workflow UI Events
    - **IMPORTANT**: Re-run the SAME test from task 1 — do NOT write a new test
    - The test from task 1 encodes the expected behavior (EmitSuggestMaximize called once, EmitDocUpdate called with correct phaseID/content)
    - When this test passes, it confirms the expected behavior is satisfied
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed — SteeringWorkflowDetector now detects coding tasks and emits events)
    - _Requirements: 2.1, 2.2_

  - [x] 3.4 Verify preservation tests still pass
    - **Property 2: Preservation** — Non-Coding Tasks, Background Messages, and Workflow Engine Path Unchanged
    - **IMPORTANT**: Re-run the SAME tests from task 2 — do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions — non-coding tasks still produce zero events, background messages still blocked, workflow engine path still works)
    - Confirm all tests still pass after fix (no regressions)

- [x] 4. Checkpoint — Ensure all tests pass
  - Run full test suite: `go test ./gui/ -run TestSteeringWorkflow -v`
  - Verify bug condition exploration test (Property 1) passes — confirms fix works
  - Verify preservation property tests (Property 2) pass — confirms no regressions
  - Verify existing tests in `gui/im_message_handler_spec_workflow_test.go` still pass — confirms workflow engine path unchanged
  - Ensure all tests pass, ask the user if questions arise
