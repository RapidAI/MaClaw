# Tasks

## Task 1: Add PolicyUserOverride constant

- [x] 1.1 Add `PolicyUserOverride PolicyAction = "user_override"` to `corelib/security/types.go`
- [x] 1.2 Add `PolicyUserOverride = security.PolicyUserOverride` alias in `gui/corelib_aliases.go`

## Task 2: Implement buildCriticalRiskPrompt helper

- [x] 2.1 Create `buildCriticalRiskPrompt(skillName, source string, factors []string) string` in a new file `gui/skill_confirm_critical.go`
- [ ] 2.2 Write property test for prompt content completeness (Property 4)
  - [x] pbt

## Task 3: Implement confirmCriticalRiskSkill shared function

- [x] 3.1 Add `criticalRiskConfirmResponse` struct and `pendingCriticalConfirm sync.Map` to `IMMessageHandler`
- [x] 3.2 Implement `confirmCriticalRiskSkill(ctx, skillName, source, factors, platform) bool` in `gui/skill_confirm_critical.go`
  - Desktop path: build `IMResponseConfirmation` with confirm/reject buttons, emit Wails event, block on response channel
  - IM path: build `AskUserRequest{InputType: "confirm", Options: ["确认安装", "拒绝安装"]}`, block on response channel
  - 120-second timeout via `select` with `time.After`
  - Fail-closed on nil/empty platform
- [x] 3.3 Add `ResolveCriticalConfirm(confirmID string, confirmed bool)` method for the frontend/IM gateway to call back with the user's answer
- [x] 3.4 Write unit tests for timeout, fail-closed, and channel adaptation

## Task 4: Wire confirmCriticalRiskSkill into toolInstallSkillHub

- [x] 4.1 In `gui/im_tools_misc.go` `toolInstallSkillHub`: replace the hard-reject block (`if assessment.Level == RiskCritical`) with a call to `confirmCriticalRiskSkill`
- [x] 4.2 On confirm: continue to Register, audit with `PolicyUserOverride` including skill name, source, risk level, and factors in Result
- [x] 4.3 On reject: audit with `PolicyDeny` and result indicating user rejection (preserve existing behavior)
- [ ] 4.4 Write property tests for confirm/reject paths (Properties 1, 2, 3, 5, 6)
  - [x] pbt

## Task 5: Wire confirmCriticalRiskSkill into registerAndExecuteSkill

- [x] 5.1 In `gui/im_message_handler.go` `registerAndExecuteSkill`: replace the hard-reject block with a call to `confirmCriticalRiskSkill`
- [x] 5.2 On confirm: continue to Register + Execute, audit with `PolicyUserOverride`
- [x] 5.3 On reject: audit with `PolicyDeny` (preserve existing behavior)
- [ ] 5.4 Write property tests verifying same behavior as Task 4 from this path (Properties 1, 2, 3)
  - [x] pbt

## Task 6: Wire confirmCriticalRiskSkill into CapabilityGapDetector

- [x] 6.1 Update the `SetConfirmCallback` wiring (in `app.go` or handler init) to call `confirmCriticalRiskSkill` with platform context
- [x] 6.2 Verify existing `confirmCallback == nil` → reject behavior is preserved (backward-compatible default)
- [x] 6.3 Update audit logging in CapabilityGapDetector to use `PolicyUserOverride` on user-confirmed installs
- [x] 6.4 Write unit test for nil callback rejection and format consistency with other paths

## Task 7: Write SkillMarket exclusion property test

- [x] 7.1 Write property test verifying trusted/builtin skills never reach RiskCritical (Property 7)
  - [x] pbt

## Task 8: Frontend/IM gateway callback integration

- [x] 8.1 Desktop panel: handle the critical-risk confirmation event in the frontend, render confirm/reject buttons, call `ResolveCriticalConfirm` on user action
- [x] 8.2 IM gateway: handle the ask_user response for critical-risk confirmation, route the answer back to `ResolveCriticalConfirm`
