# Implementation Plan

- [x] 1. Write bug condition exploration test
  - **Property 1: Bug Condition** - 移动端邀请码 prompt 与飞书注册异步静默失败
  - **CRITICAL**: This test MUST FAIL on unfixed code - failure confirms the bug exists
  - **DO NOT attempt to fix the test or the code when it fails**
  - **NOTE**: This test encodes the expected behavior - it will validate the fix when it passes after implementation
  - **GOAL**: Surface counterexamples that demonstrate the bug exists
  - **Scoped PBT Approach**: Scope the property to concrete failing cases:
    - Case A: `EnrollStartHandler` 注册成功后响应中不包含 `feishu_status` 字段（飞书注册在 goroutine 中异步执行，结果未返回给客户端）
    - Case B: `EnrollStartHandler` 中飞书注册通过 `go func()` 异步调用，注册响应在飞书注册完成前就已返回
  - Test file: `hub/internal/httpapi/enroll_handler_test.go`
  - Write a Go test that:
    1. 构造一个 mock `IdentityService` 和 mock `feishu.Notifier`（含 `AutoEnroller`）
    2. 发送 POST `/api/enroll/start` 请求（email + mobile）
    3. 断言响应 JSON 中包含 `feishu_status` 字段（值为 "ok"、"failed" 或 "disabled"）
    4. 断言飞书注册是同步完成的（响应返回时飞书注册已执行完毕）
  - Run test on UNFIXED code
  - **EXPECTED OUTCOME**: Test FAILS — 响应中无 `feishu_status` 字段，飞书注册在 goroutine 中异步执行
  - Document counterexamples found: 注册响应 JSON 不包含 `feishu_status`，飞书注册结果仅出现在 `log.Printf` 中
  - Mark task complete when test is written, run, and failure is documented
  - _Requirements: 1.4, 1.5, 2.4_

- [x] 2. Write preservation property tests (BEFORE implementing fix)
  - **Property 2: Preservation** - 非移动端注册流程与现有行为保持不变
  - **IMPORTANT**: Follow observation-first methodology
  - **Observation targets** (run on UNFIXED code):
    - Observe: `EnrollStartHandler` 对正常注册请求（email 有效、无飞书配置）返回 `{"status":"approved", ...}` 格式
    - Observe: `EnrollStartHandler` 对邮箱被封禁的请求返回 `{"ok":false, "code":"EMAIL_BLOCKED", ...}`
    - Observe: `EnrollStartHandler` 对需要邀请码但未提供的请求返回 `{"ok":false, "code":"INVITATION_CODE_REQUIRED", ...}`
    - Observe: `EnrollStartHandler` 对无效邀请码的请求返回 `{"ok":false, "code":"INVALID_INVITATION_CODE", ...}`
    - Observe: `ProbeByEmail` 返回的 `ProbeResult` 结构体包含 `email`, `status`, `bound`, `can_login`, `invitation_code_required` 字段
  - Test file: `hub/internal/httpapi/enroll_handler_preservation_test.go` 和 `hub/internal/entry/service_preservation_test.go`
  - Write property-based tests (using Go `testing/quick` or table-driven with random inputs):
    1. 对于所有不涉及飞书自动注册的注册请求（`feishuNotifier == nil`），`EnrollStartHandler` 的响应格式和状态码与修复前完全一致
    2. 对于所有错误场景（邮箱无效、被封禁、邀请码错误等），错误响应的 `code` 和 HTTP 状态码与修复前完全一致
    3. 对于 `ProbeByEmail`，所有现有字段的值与修复前完全一致（新增字段不影响现有字段）
  - Verify tests PASS on UNFIXED code
  - **EXPECTED OUTCOME**: Tests PASS — 确认现有行为的基线
  - Mark task complete when tests are written, run, and passing on unfixed code
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [x] 3. Fix: 移动端邀请码内联输入、区号选择器与飞书注册同步反馈

  - [x] 3.1 `AddToFeishuOrg` 返回结构化结果
    - 在 `hub/internal/feishu/auto_enroll.go` 中新增 `AutoEnrollResult` 结构体：`Status string` (ok/failed/skipped/disabled), `Message string`, `OpenID string`
    - 修改 `AddToFeishuOrg` 签名为 `AddToFeishuOrg(ctx, email, displayName, mobile string) (*AutoEnrollResult, error)`
    - 未启用时返回 `{Status: "disabled"}`，已存在时返回 `{Status: "ok", Message: "already in org"}`，创建成功返回 `{Status: "ok"}`，失败返回 `{Status: "failed", Message: err.Error()}`
    - 更新 `hub/internal/feishu/auto_enroll_test.go` 中的现有测试以适配新签名
    - _Bug_Condition: isBugCondition(input) where feishu_auto_enroll_enabled AND (mobile == "" OR invalid format)_
    - _Expected_Behavior: AddToFeishuOrg returns structured AutoEnrollResult instead of just error_
    - _Preservation: 所有现有调用方行为不变，仅增加返回值_
    - _Requirements: 1.4, 1.5, 2.4_

  - [x] 3.2 `EnrollStartHandler` 飞书注册改为同步并返回 `feishu_status`
    - 在 `hub/internal/httpapi/enroll_handler.go` 的 `EnrollStartHandler` 中：
    - 将 `go func() { ae.AddToFeishuOrg(...) }()` 改为同步调用（设置 15 秒超时 context）
    - 构造包含 `feishu_status` 和 `feishu_message` 的响应 map
    - 飞书注册失败时仍返回 HTTP 200（Hub 注册成功），但 `feishu_status` 为 "failed"
    - 未启用飞书时 `feishu_status` 为 "disabled"
    - _Bug_Condition: isBugCondition(input) where feishu auto-enroll runs in goroutine, result not in response_
    - _Expected_Behavior: response contains feishu_status field, feishu enroll runs synchronously_
    - _Preservation: HTTP 状态码和错误响应格式不变，仅在成功响应中增加字段_
    - _Requirements: 1.4, 2.4, 3.1_

  - [x] 3.3 `ProbeResult` 添加 `FeishuAutoEnroll` 字段
    - 在 `hub/internal/entry/service.go` 的 `ProbeResult` 结构体中添加 `FeishuAutoEnroll bool \`json:"feishu_auto_enroll"\``
    - 在 `Service` 中注入飞书自动注册状态检查接口（或直接传入 bool 值）
    - 在 `ProbeByEmail` 的各个返回路径中设置 `FeishuAutoEnroll` 字段
    - _Bug_Condition: probe response missing feishu_auto_enroll field, frontend cannot show mobile importance hint_
    - _Expected_Behavior: ProbeResult includes FeishuAutoEnroll bool field_
    - _Preservation: 所有现有 ProbeResult 字段值不变_
    - _Requirements: 2.5, 3.3_

  - [x] 3.4 移动端 bootstrap 页面：替换 `prompt()` 为内联邀请码输入框 + 区号选择器 + 飞书反馈
    - 同时修改 `build/mobile/bootstrap.html.tmpl` 和 `mobile/shared/bootstrap.html`（两个文件需同步）
    - **邀请码输入框**:
      - 添加全局变量 `invitationCodeRequired = false` 和 `feishuAutoEnroll = false`
      - 在 `renderEmailForm` 中，当 `invitationCodeRequired` 为 true 时渲染 `<input id="inv-code">` 邀请码输入框
      - 在 `resolveViaPrivateHub` 中，probe 返回后根据 `payload.invitation_code_required` 和 `payload.feishu_auto_enroll` 更新全局变量并重新渲染表单
      - 移除 `prompt()` 调用，改为从 `document.getElementById("inv-code").value` 读取邀请码
      - 邀请码验证失败时（INVITATION_CODE_REQUIRED / INVALID_INVITATION_CODE），在输入框下方显示红色错误提示，允许用户修改后重新提交
    - **区号选择器**:
      - 添加 `COUNTRY_CODES` 数组（参考桌面端 OnboardingWizard 的定义）
      - 将 `<input id="mobile">` 替换为 `<select id="country-code">` + `<input id="mobile-number">` 的 flex 布局
      - 提交时拼接 `countryCode + mobileNumber` 为完整手机号
    - **飞书注册反馈**:
      - 注册成功后解析响应中的 `feishu_status` 和 `feishu_message`
      - 在状态区域显示飞书邀请结果（成功/失败/跳过）
    - _Bug_Condition: mobile bootstrap uses prompt() for invitation code, no country code selector, no feishu feedback_
    - _Expected_Behavior: inline invitation code input, country code selector, feishu status displayed_
    - _Preservation: Hub Center 发现流程、自动重连、桌面端流程不受影响_
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2, 2.3, 2.4, 2.5, 3.2, 3.5_

  - [x] 3.5 Verify bug condition exploration test now passes
    - **Property 1: Expected Behavior** - 注册响应包含 feishu_status 字段
    - **IMPORTANT**: Re-run the SAME test from task 1 - do NOT write a new test
    - The test from task 1 encodes the expected behavior
    - When this test passes, it confirms the expected behavior is satisfied
    - Run bug condition exploration test from step 1
    - **EXPECTED OUTCOME**: Test PASSES (confirms bug is fixed)
    - _Requirements: 2.4_

  - [x] 3.6 Verify preservation tests still pass
    - **Property 2: Preservation** - 非移动端注册流程保持不变
    - **IMPORTANT**: Re-run the SAME tests from task 2 - do NOT write new tests
    - Run preservation property tests from step 2
    - **EXPECTED OUTCOME**: Tests PASS (confirms no regressions)
    - Confirm all tests still pass after fix (no regressions)

- [x] 4. Checkpoint - Ensure all tests pass
  - Run full test suite: `go test ./hub/internal/httpapi/... ./hub/internal/entry/... ./hub/internal/feishu/...`
  - Verify exploration test (task 1) passes
  - Verify preservation tests (task 2) pass
  - Verify existing `hub/internal/feishu/auto_enroll_test.go` passes with updated `AddToFeishuOrg` signature
  - Manually verify `build/mobile/bootstrap.html.tmpl` and `mobile/shared/bootstrap.html` are in sync
  - Ensure all tests pass, ask the user if questions arise.
