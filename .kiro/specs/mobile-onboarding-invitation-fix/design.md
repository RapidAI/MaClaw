# Mobile Onboarding Invitation Fix — Bugfix Design

## Overview

移动端 onboarding 注册流程存在两个关联缺陷：(1) 邀请码输入使用浏览器 `prompt()` 弹窗，在移动端体验差且可被跳过；(2) 飞书自动注册在后台 goroutine 中运行，失败时仅写日志，用户无感知。修复策略是将 `prompt()` 替换为内联邀请码输入框并添加区号选择器（前端），同时将飞书自动注册改为同步执行并将结果返回给客户端（后端）。

## Glossary

- **Bug_Condition (C)**: 移动端 bootstrap 页面注册流程中，邀请码通过 `prompt()` 获取或飞书注册在后台静默失败的条件
- **Property (P)**: 邀请码通过内联输入框获取且飞书注册结果同步返回给客户端
- **Preservation**: 桌面端 OnboardingWizard 注册流程、Hub Center 发现流程、自动重连等现有行为不受影响
- **resolveViaPrivateHub**: `bootstrap.html.tmpl` / `bootstrap.html` 中处理私有 hub 注册的 JS 函数，包含 `prompt()` 调用
- **EnrollStartHandler**: `hub/internal/httpapi/enroll_handler.go` 中的注册 HTTP handler，当前在 goroutine 中调用飞书自动注册
- **AddToFeishuOrg**: `hub/internal/feishu/auto_enroll.go` 中将用户添加到飞书组织的方法，当前仅返回 error 但调用方忽略结果

## Bug Details

### Bug Condition

缺陷在两个层面同时存在：

**前端层面**：移动端 bootstrap 页面在 `resolveViaPrivateHub` 函数中，当 `payload.invitation_code_required` 为 true 时，使用 `prompt()` 获取邀请码。`prompt()` 在移动端浏览器中体验差（弹窗样式不可控、部分浏览器可能阻止），且用户点击取消后返回空字符串，导致注册失败且无法重新输入。同时，手机号输入框没有国际区号选择器。

**后端层面**：`EnrollStartHandler` 在注册成功后通过 `go func()` 异步调用 `ae.AddToFeishuOrg()`，结果仅写入日志。飞书中国区 API 要求 mobile 必填（错误码 41010），当手机号为空或格式不正确时静默失败。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type MobileEnrollmentRequest
  OUTPUT: boolean

  // 前端缺陷条件
  frontendBug := input.source == "mobile_bootstrap"
                 AND (input.hub.invitation_code_required == true
                      OR input.mobile != "" AND NOT hasCountryCodePrefix(input.mobile))

  // 后端缺陷条件
  backendBug := input.source == "mobile_bootstrap"
                AND input.hub.feishu_auto_enroll_enabled == true
                AND (input.mobile == "" OR NOT isValidMobileFormat(input.mobile))

  RETURN frontendBug OR backendBug
END FUNCTION
```

### Examples

- 用户在移动端注册，hub 需要邀请码 → 弹出 `prompt()` 弹窗，用户点击取消 → 报错 "Invitation code is required"，无法重新输入，需刷新页面
- 用户在移动端输入手机号 "13800138000"（无区号）→ 传递给 API → 飞书 `normalizeChinaMobile` 补充 +86 → 注册成功但用户不知道区号是否正确
- 用户在移动端未输入手机号直接注册 → Hub 注册成功 → 飞书 `AddToFeishuOrg` 因 mobile 为空失败（错误码 41010）→ 用户无感知
- 用户在移动端输入日本手机号 "09012345678" → 无区号选择器 → `normalizeChinaMobile` 错误地补充 +86 前缀 → 飞书注册失败

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 桌面端 OnboardingWizard 的邀请码输入框和区号选择器继续正常工作
- Hub Center 发现多个 hub 并选择连接的流程不受影响
- 不需要邀请码时（`invitation_code_required` 为 false），注册表单不显示邀请码输入框
- 已存在于飞书组织的用户继续跳过创建步骤，仅绑定 open_id
- 移动端已保存 hub 连接信息时继续自动重连

**Scope:**
所有不涉及移动端 bootstrap 注册流程的输入不受此修复影响，包括：
- 桌面端通过 Wails binding 调用 `ActivateRemote` 的注册流程
- Hub Center 的 hub 发现和选择流程
- 已注册用户的自动重连流程
- 非飞书相关的注册（Lark 海外版等）

## Hypothesized Root Cause

Based on the bug description, the most likely issues are:

1. **前端使用 `prompt()` 而非内联输入框**: `resolveViaPrivateHub` 函数中 `invCode = prompt("This hub requires an invitation code:") || ""` 在移动端体验差，且取消后无法重试。根本原因是移动端 bootstrap 页面是早期实现，未考虑移动端 UX。

2. **前端缺少区号选择器**: 手机号输入框 `<input id="mobile" type="tel">` 没有区号选择器，用户只能输入裸号码。桌面端 OnboardingWizard 已有 `COUNTRY_CODES` 和 `<select>` 实现，但移动端 bootstrap 未同步。

3. **后端飞书注册异步且无反馈**: `EnrollStartHandler` 中 `go func() { ae.AddToFeishuOrg(...) }()` 在独立 goroutine 中运行，注册响应在飞书注册完成前就已返回。即使改为同步，当前 `writeJSON(w, http.StatusOK, resp)` 也不包含飞书注册结果。

4. **probe 响应缺少飞书信息**: `entry/probe` API 的 `ProbeResult` 不包含飞书自动注册是否启用的信息，前端无法据此提示用户手机号的重要性。

## Correctness Properties

Property 1: Bug Condition - 移动端邀请码内联输入与飞书注册反馈

_For any_ 移动端 bootstrap 注册请求，当 hub 需要邀请码时，系统 SHALL 在注册表单中显示内联邀请码输入框（而非 `prompt()` 弹窗），当邀请码无效时 SHALL 在输入框下方显示错误提示并允许重新输入；当飞书自动注册执行后，系统 SHALL 将飞书注册结果包含在注册响应中返回给客户端。

**Validates: Requirements 2.1, 2.2, 2.4**

Property 2: Preservation - 非移动端注册流程与现有行为

_For any_ 非移动端 bootstrap 来源的注册请求（桌面端 OnboardingWizard、Hub Center 发现等），系统 SHALL 产生与修复前完全相同的行为，保持桌面端邀请码输入、hub 发现、自动重连等所有现有功能不变。

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `build/mobile/bootstrap.html.tmpl` 和 `mobile/shared/bootstrap.html`

**Function**: `resolveViaPrivateHub`, `renderEmailForm`

**Specific Changes**:

1. **替换 `prompt()` 为内联邀请码输入框**: 在 `renderEmailForm` 中，当 probe 返回 `invitation_code_required: true` 时，动态显示邀请码输入框。移除 `resolveViaPrivateHub` 中的 `prompt()` 调用，改为从表单中读取邀请码值。
   - 添加全局变量 `invitationCodeRequired` 跟踪是否需要邀请码
   - 在 `renderEmailForm` 中条件渲染邀请码输入框
   - 邀请码验证失败时在输入框下方显示错误提示

2. **添加国际区号选择器**: 将手机号输入框改为区号 `<select>` + 号码 `<input>` 的组合布局。
   - 添加 `COUNTRY_CODES` 数组（参考桌面端 OnboardingWizard）
   - 提交时拼接区号和号码为完整格式

3. **处理飞书注册反馈**: 在注册成功后，解析响应中的 `feishu_status` 字段，显示飞书邀请状态提示。

4. **probe 后更新 UI**: 在 `resolveViaPrivateHub` 调用 probe 后，根据返回的 `invitation_code_required` 和 `feishu_auto_enroll` 字段更新表单状态。

---

**File**: `hub/internal/httpapi/enroll_handler.go`

**Function**: `EnrollStartHandler`

**Specific Changes**:

1. **飞书注册改为同步执行**: 将 `go func() { ae.AddToFeishuOrg(...) }()` 改为同步调用，在注册响应返回前完成飞书注册。
   - 设置合理的超时（如 15 秒）
   - 飞书注册失败不阻塞 Hub 注册成功

2. **注册响应包含飞书结果**: 在 `writeJSON` 返回的响应中添加 `feishu_status` 字段。
   - 成功时: `{"feishu_status": "ok", "feishu_message": "已加入飞书组织"}`
   - 失败时: `{"feishu_status": "failed", "feishu_message": "飞书邀请失败: ..."}`
   - 未启用时: `{"feishu_status": "disabled"}`

---

**File**: `hub/internal/entry/service.go`

**Function**: `ProbeByEmail`

**Specific Changes**:

1. **probe 响应添加飞书信息**: 在 `ProbeResult` 中添加 `FeishuAutoEnroll bool` 字段，让前端知道是否需要提示手机号的重要性。

---

**File**: `hub/internal/feishu/auto_enroll.go`

**Function**: `AddToFeishuOrg`

**Specific Changes**:

1. **返回结构化结果**: `AddToFeishuOrg` 当前返回 `error`，需要额外返回一个结果结构体，包含成功/失败状态和消息，供 handler 包含在响应中。

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: 检查移动端 bootstrap 页面的 `resolveViaPrivateHub` 函数中 `prompt()` 的使用，以及 `EnrollStartHandler` 中飞书注册的异步调用。在未修复代码上运行测试以观察失败模式。

**Test Cases**:
1. **移动端邀请码 prompt 测试**: 模拟 `invitation_code_required: true` 的 probe 响应，验证 `prompt()` 被调用（will fail on unfixed code — prompt 无法被自动化测试拦截）
2. **飞书注册异步测试**: 发送注册请求后立即检查响应，验证响应中不包含飞书注册结果（will fail on unfixed code — 响应中无 `feishu_status` 字段）
3. **手机号无区号测试**: 发送手机号 "13800138000"（无区号）的注册请求，验证飞书注册是否正确处理（will fail on unfixed code — 前端无区号选择器）
4. **空手机号飞书注册测试**: 发送无手机号的注册请求到启用飞书自动注册的 hub，验证响应中是否包含飞书失败信息（will fail on unfixed code — 飞书失败被静默吞掉）

**Expected Counterexamples**:
- 注册响应中不包含 `feishu_status` 字段
- 飞书注册失败仅出现在服务器日志中，客户端无感知
- Possible causes: 异步 goroutine 执行、响应结构体不包含飞书字段、前端使用 `prompt()` 而非内联输入

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := enrollStartHandler_fixed(input)
  ASSERT result.feishu_status IN ["ok", "failed", "disabled"]
  IF input.hub.invitation_code_required THEN
    ASSERT mobileBootstrapPage.hasInlineInvitationCodeInput()
    ASSERT NOT mobileBootstrapPage.usesPrompt()
  END IF
  IF input.mobile != "" THEN
    ASSERT input.mobile.startsWith("+")  // 区号已拼接
  END IF
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT enrollStartHandler_original(input) = enrollStartHandler_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for桌面端注册、Hub Center 发现等非移动端流程，then write property-based tests capturing that behavior.

**Test Cases**:
1. **桌面端注册保持不变**: 验证通过 `ActivateRemote` Wails binding 的注册流程在修复后行为完全一致
2. **Hub Center 发现保持不变**: 验证 `fetchHubList` 和 `renderHubList` 在修复后行为完全一致
3. **自动重连保持不变**: 验证 `autoReconnect` 在修复后行为完全一致
4. **不需要邀请码时无输入框**: 验证 `invitation_code_required: false` 时注册表单不显示邀请码输入框
5. **已存在飞书用户跳过创建**: 验证已在飞书组织中的用户继续跳过创建步骤

### Unit Tests

- 测试 `EnrollStartHandler` 同步调用飞书注册并在响应中包含 `feishu_status`
- 测试 `AddToFeishuOrg` 返回结构化结果（成功/失败/跳过）
- 测试 `ProbeResult` 包含 `feishu_auto_enroll` 字段
- 测试移动端 bootstrap 页面的邀请码输入框渲染逻辑
- 测试区号选择器与手机号拼接逻辑
- 测试邀请码验证失败时的错误提示显示

### Property-Based Tests

- 生成随机注册请求（含/不含邀请码、含/不含手机号、不同区号），验证飞书注册结果始终包含在响应中
- 生成随机手机号和区号组合，验证拼接后的格式始终以 "+" 开头
- 生成随机桌面端注册请求，验证修复前后行为一致（preservation）

### Integration Tests

- 完整移动端注册流程：输入邮箱 → 输入手机号（含区号）→ 输入邀请码 → 提交 → 验证飞书注册反馈
- 邀请码错误重试流程：输入错误邀请码 → 看到错误提示 → 修改邀请码 → 重新提交 → 成功
- 飞书注册失败提示流程：注册成功但飞书失败 → 页面显示飞书邀请失败提示
