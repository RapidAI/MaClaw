# Requirements Document

## Introduction

When a non-SkillMarket skill (from ClawHub, GitHub, or other community sources) is assessed as Critical risk by the security risk assessor, the system currently rejects the installation outright with no recourse. This feature changes that behavior: instead of a hard reject, the system asks the user for confirmation. If the user confirms, installation proceeds; if the user rejects (or times out), installation is denied. SkillMarket (official store) skills are unaffected — their trust level is "trusted" and risk is capped at Medium.

## Glossary

- **Risk_Assessor**: The `RiskAssessor` component (`gui/risk_assessor.go`) that evaluates skill risk level (Low/Medium/High/Critical) via `AssessSkill()`
- **Skill_Installer**: The collective set of code paths that download, assess, register, and execute skills — including `toolInstallSkillHub`, `registerAndExecuteSkill`, and `CapabilityGapDetector.Resolve()`
- **Confirmation_Mechanism**: The subsystem that presents a blocking question to the user and waits for a yes/no response, using the existing `ask_user` tool (IM channel) or `IMResponseConfirmation` (desktop AI assistant panel)
- **SkillMarket**: The official skill store whose skills carry `TrustLevel="trusted"` and are capped at Medium risk — excluded from this feature
- **Non_SkillMarket_Source**: Any skill source other than SkillMarket, including ClawHub, GitHub, and community repositories
- **Audit_Log**: The `AuditLog` component that records security-relevant events (install, reject, user-confirmed-install)

## Requirements

### Requirement 1: User Confirmation for Critical-Risk Skills in toolInstallSkillHub

**User Story:** As a user, I want to be asked for confirmation when a Critical-risk skill is detected during hub_skill_install, so that I can make an informed decision instead of being silently blocked.

#### Acceptance Criteria

1. WHEN the Risk_Assessor returns `RiskCritical` for a skill in the `toolInstallSkillHub` handler, THE Skill_Installer SHALL present a confirmation prompt to the user containing the skill name and risk factors
2. WHEN the user confirms the Critical-risk installation prompt in `toolInstallSkillHub`, THE Skill_Installer SHALL proceed with skill registration and execution
3. WHEN the user rejects the Critical-risk installation prompt in `toolInstallSkillHub`, THE Skill_Installer SHALL deny the installation and return a rejection message
4. WHEN the user does not respond within 120 seconds, THE Skill_Installer SHALL treat the non-response as a rejection and deny the installation

### Requirement 2: User Confirmation for Critical-Risk Skills in registerAndExecuteSkill

**User Story:** As a user, I want to be asked for confirmation when a Critical-risk skill is detected during auto-install from search_and_install_skill, so that I retain control over what gets installed.

#### Acceptance Criteria

1. WHEN the Risk_Assessor returns `RiskCritical` for a skill in the `registerAndExecuteSkill` handler, THE Skill_Installer SHALL present a confirmation prompt to the user containing the skill name and risk factors
2. WHEN the user confirms the Critical-risk installation prompt in `registerAndExecuteSkill`, THE Skill_Installer SHALL proceed with skill registration and execution
3. WHEN the user rejects the Critical-risk installation prompt in `registerAndExecuteSkill`, THE Skill_Installer SHALL deny the installation and return a rejection message

### Requirement 3: Consistent Confirmation in CapabilityGapDetector.Resolve

**User Story:** As a user, I want the existing `confirmCallback` mechanism in CapabilityGapDetector to use the same confirmation UI as the other installation paths, so that the experience is consistent.

#### Acceptance Criteria

1. THE Confirmation_Mechanism used by `CapabilityGapDetector.Resolve()` SHALL present the same confirmation prompt format (skill name + risk factors) as the other two installation paths
2. WHEN the `confirmCallback` is not set on the CapabilityGapDetector, THE Skill_Installer SHALL treat Critical-risk skills as rejected (backward-compatible default)

### Requirement 4: Confirmation Prompt Content

**User Story:** As a user, I want the confirmation prompt to clearly communicate the risk, so that I can make an informed decision.

#### Acceptance Criteria

1. THE Confirmation_Mechanism SHALL include the skill name in the confirmation prompt
2. THE Confirmation_Mechanism SHALL include the risk factors (from `RiskAssessment.Factors`) in the confirmation prompt
3. THE Confirmation_Mechanism SHALL include the skill source (hub URL, ClawHub, GitHub) in the confirmation prompt
4. THE Confirmation_Mechanism SHALL present exactly two options: confirm installation or reject installation

### Requirement 5: Audit Logging for User-Confirmed Critical Installations

**User Story:** As a system administrator, I want user-confirmed Critical-risk installations to be logged distinctly, so that I can audit security decisions.

#### Acceptance Criteria

1. WHEN the user confirms a Critical-risk skill installation, THE Audit_Log SHALL record an entry with `PolicyAction` set to a value distinguishing it from auto-allowed installations (e.g., `PolicyUserOverride`)
2. WHEN the user rejects a Critical-risk skill installation, THE Audit_Log SHALL record an entry with `PolicyAction=PolicyDeny` and a result indicating user rejection
3. THE Audit_Log entry for user-confirmed installations SHALL include the skill name, source, risk level, and risk factors

### Requirement 6: SkillMarket Skills Excluded

**User Story:** As a user, I want SkillMarket (official store) skills to continue installing without extra confirmation, so that trusted skills are not slowed down.

#### Acceptance Criteria

1. WHILE a skill has `TrustLevel` normalized to `"trusted"` or `"builtin"`, THE Risk_Assessor SHALL cap the risk at Medium or Low respectively, ensuring the confirmation prompt is never triggered for SkillMarket skills
2. THE Skill_Installer SHALL only trigger the Critical-risk confirmation prompt for skills from Non_SkillMarket_Source

### Requirement 7: Confirmation Channel Adaptation

**User Story:** As a user, I want the confirmation to appear in the appropriate channel (IM or desktop panel), so that I can respond naturally regardless of how I'm using the system.

#### Acceptance Criteria

1. WHEN the installation is triggered from an IM channel (Feishu/WeChat/QQ/Telegram), THE Confirmation_Mechanism SHALL use the `ask_user` tool to present the confirmation prompt
2. WHEN the installation is triggered from the desktop AI assistant panel, THE Confirmation_Mechanism SHALL use the `IMResponseConfirmation` UI to present the confirmation prompt with confirm/reject buttons
3. IF the confirmation channel is unavailable or the callback is not set, THEN THE Skill_Installer SHALL reject the Critical-risk installation (fail-closed)
