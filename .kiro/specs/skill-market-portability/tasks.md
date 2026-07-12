# Implementation Plan: Skill Market Portability

## Overview

Implement a portability validation and auto-fix pipeline for skills before marketplace upload. All core logic lives in `corelib/skill/` (shared by GUI and TUI), with integration points in the `manage_skill` tool and the pre-upload gate. Implementation proceeds bottom-up: types → validator → fixer → formatter → integration → tests.

## Tasks

- [x] 1. Define portability report types
  - [x] 1.1 Create `corelib/skill/portability_types.go` with `IssueSeverity` constants (`SeverityError`, `SeverityWarning`, `SeverityInfo`), `PortabilityIssue`, `IssueSummary`, `PortabilityReport`, and `PortabilityChange` structs as specified in the design
    - Include JSON tags on all fields, `omitempty` on `Line` and `Suggestion`
    - `MarketReady` is a plain field (not a method), set at report construction time
    - Add `NewPortabilityReport(skillName, skillDir string, issues []PortabilityIssue) *PortabilityReport` constructor that computes `Summary` counts and `MarketReady` from the issues list
    - _Requirements: 8.1, 8.2, 8.3_

  - [ ]* 1.2 Write property test for Report JSON round-trip
    - **Property 10: Report JSON Round-Trip**
    - Generate random `PortabilityReport` instances with varying issues, summaries, and timestamps; verify `json.Marshal` → `json.Unmarshal` produces identical fields
    - **Validates: Requirements 8.4**

  - [ ]* 1.3 Write property test for MarketReady invariant
    - **Property 11: MarketReady Invariant**
    - Generate `PortabilityReport` with varying error counts; verify `MarketReady == (Summary.Errors == 0)`
    - **Validates: Requirements 8.3**

- [x] 2. Implement validation engine
  - [x] 2.1 Create `corelib/skill/portability_validator.go` with `ValidateSkillPortability(skillDir string) (*PortabilityReport, error)` function
    - Parse `skill.yaml` using existing `ParseSkillYAMLFile`
    - Build report with `SkillName`, `SkillDir`, `Timestamp`, computed `Summary` and `MarketReady`
    - Return wrapped `os.ErrNotExist` for missing directory, parse error for invalid YAML
    - If neither `skill.yaml` nor `SKILL.md` exists, return error
    - _Requirements: 1.1, 1.11_

  - [x] 2.2 Implement `checkMissingBaseDir` checker (runs FIRST, before hardcoded paths)
    - Compare detected absolute paths against the skill directory path (normalized to forward slashes)
    - Report severity "error", category "missing_basedir" with suggestion containing `{baseDir}`
    - Return the set of matched paths so `checkHardcodedPaths` can exclude them
    - _Requirements: 1.4_

  - [x] 2.3 Implement `checkHardcodedPaths` checker
    - Detect absolute paths in bash step commands using regex: Unix `/(home|usr|opt|tmp|var|etc)/` or `/Users/`, Windows `[A-Za-z]:\\`
    - Exclude system binary paths (`/usr/bin/`, `/bin/`, `/usr/local/bin/`, `/usr/sbin/`, `/sbin/`)
    - Exclude paths already reported by `checkMissingBaseDir` (avoid duplicate issues)
    - Report severity "error", category "hardcoded_path"
    - _Requirements: 1.2, 1.3_

  - [x] 2.4 Implement `checkMetadata` checker
    - Check empty platforms → "missing_platforms" warning
    - Check description < 10 chars → "incomplete_metadata" warning
    - Check empty triggers → "incomplete_metadata" warning
    - _Requirements: 1.5, 1.6, 1.7_

  - [x] 2.5 Implement `checkPathSeparators` checker
    - Detect backslash path separator patterns in bash commands (e.g. `scripts\run.py`, `subdir\file.txt`)
    - Exclude shell escape sequences (`\n`, `\t`, `\"`, `\\`, `\$`, `\'`)
    - Report severity "warning", category "path_separator"
    - _Requirements: 1.9_

  - [x] 2.6 Implement `checkPlatformCompat` checker
    - Detect `python3` without fallback → info "platform_compat"
    - Detect `%VAR%` Windows env vars → warning "platform_compat"
    - Detect shebangs (`#!/bin/bash`, `#!/usr/bin/env bash`) → info "platform_compat"
    - Detect POSIX-only commands (`chmod`, `ln -s`, `chown`, `grep -P`) when platforms include "windows"/"universal"/empty → warning "platform_compat"
    - Detect Windows-specific syntax (`$env:`, `[Console]::`, `.ps1`, `cmd.exe`) when platforms include "linux"/"macos"/"universal"/empty → warning "platform_compat"
    - _Requirements: 1.8, 1.10, 6.1, 6.2, 6.3_

  - [x] 2.7 Implement `checkShellMismatch` checker
    - Detect `preferred_shell` conflicts with declared platforms (e.g. "cmd" with linux/macos)
    - Report severity "warning", category "shell_mismatch"
    - _Requirements: 6.4_

  - [x] 2.8 Implement `checkDependencies` checker
    - Maintain list of known runtime commands: python, python3, node, npm, npx, pip, pip3, java, go, cargo, ruby, perl
    - Extract first word of each bash command, check against `required_env`, `description`, `triggers` (case-insensitive substring)
    - Undeclared → warning "undeclared_dependency"; `pip install`/`npm install` → info "runtime_install"
    - _Requirements: 7.1, 7.2, 7.3_

  - [x] 2.9 Add SKILL.md validation support
    - Check for `SKILL.md` (or `skill.md`) in skill directory
    - Use existing `extractAllBashBlocksFromMarkdown()` to extract bash code blocks
    - Run path/platform checks on each block, referencing `file: "SKILL.md"`
    - Log warning and continue if SKILL.md parse fails
    - _Requirements: 5.1, 5.2_

  - [ ]* 2.10 Write property test for absolute path detection
    - **Property 2: Absolute Path Detection**
    - Generate bash commands with embedded Unix/Windows absolute paths; verify report includes issue with "hardcoded_path" or "missing_basedir"
    - **Validates: Requirements 1.2, 1.3**

  - [ ]* 2.11 Write property test for missing baseDir suggestion
    - **Property 3: Missing BaseDir Suggestion**
    - Generate commands referencing files inside the skill dir; verify issue category "missing_basedir" with suggestion containing `{baseDir}`
    - **Validates: Requirements 1.4**

  - [ ]* 2.12 Write property test for metadata validation completeness
    - **Property 4: Metadata Validation Completeness**
    - Generate `SkillYAMLFile` with varying metadata (empty platforms, short description, empty triggers); verify each violated condition produces a warning
    - **Validates: Requirements 1.5, 1.6, 1.7**

  - [ ]* 2.13 Write property test for cross-platform construct detection
    - **Property 7: Cross-Platform Construct Detection**
    - Generate commands with platform-specific constructs paired with conflicting platform declarations; verify "platform_compat" or "shell_mismatch" issue
    - **Validates: Requirements 1.8, 1.9, 1.10, 6.1, 6.2, 6.3, 6.4**

  - [ ]* 2.14 Write property test for dependency declaration validation
    - **Property 8: Dependency Declaration Validation**
    - Generate commands with known runtime commands and varying `required_env`/`description`; verify "undeclared_dependency" when not mentioned
    - **Validates: Requirements 7.1, 7.2**

  - [ ]* 2.15 Write property test for SKILL.md bash block validation
    - **Property 6: SKILL.md Bash Block Validation**
    - Generate SKILL.md content with bash blocks containing absolute paths; verify issue with `File == "SKILL.md"` and category "hardcoded_path" or "missing_basedir"
    - **Validates: Requirements 5.1, 5.2**

- [x] 3. Checkpoint
  - Run `go build ./corelib/skill/...` to verify compilation
  - Run `go test ./corelib/skill/... -run TestPortability -v` to verify all validator tests pass
  - Ask the user if questions arise.

- [x] 4. Implement auto-fixer
  - [x] 4.1 Create `corelib/skill/portability_fixer.go` with `AutoFixPortability(skillDir string) ([]PortabilityChange, error)` function
    - Parse `skill.yaml` with `ParseSkillYAMLFile`, apply fixes, write back with `FormatSkillYAMLFile`
    - Create `.bak` backup before modifying any file (write backup first, then modify original)
    - Return empty change list if no fixable issues found
    - _Requirements: 2.5, 2.6, 2.7, 2.8_

  - [x] 4.2 Implement `missing_basedir` fix — replace absolute paths inside skill dir with `{baseDir}/relative/path`
    - _Requirements: 2.1_

  - [x] 4.3 Implement `hardcoded_path` home dir fix — replace `/home/<user>/...` or `C:\Users\<user>\...` with `$HOME/...`
    - _Requirements: 2.4_

  - [x] 4.4 Implement `missing_platforms` fix — set `platforms: ["universal"]` when empty
    - _Requirements: 2.2_

  - [x] 4.5 Implement `path_separator` fix — replace backslashes with forward slashes in file path references
    - _Requirements: 2.3_

  - [x] 4.6 Implement SKILL.md fix support — string replacement on raw markdown for absolute paths in bash blocks, create `SKILL.md.bak` backup
    - _Requirements: 5.3, 5.4_

  - [ ]* 4.7 Write property test for validate-fix-validate round-trip
    - **Property 1: Validate-Fix-Validate Round-Trip**
    - Generate random skill dirs with fixable issues (hardcoded paths inside skill dir, empty platforms, backslash separators); verify validate → fix → validate produces zero errors
    - **Validates: Requirements 1.12**

  - [ ]* 4.8 Write property test for extra field preservation
    - **Property 9: Extra Field Preservation**
    - Generate `SkillYAMLFile` with random `Extra` fields + fixable issues; verify `AutoFixPortability` preserves all Extra keys and values
    - **Validates: Requirements 2.9**

  - [ ]* 4.9 Write unit tests for auto-fixer
    - `TestAutoFix_NoChanges` — empty change list for clean skill
    - `TestAutoFix_CreatesBackup` — verifies `skill.yaml.bak` creation
    - `TestAutoFix_SKILLMDBackup` — verifies `SKILL.md.bak` creation
    - _Requirements: 2.6, 5.4_

- [x] 5. Implement report formatter
  - [x] 5.1 Create `corelib/skill/portability_format.go` with `FormatPortabilityReport(report *PortabilityReport) string` and `FormatPortabilityChanges(changes []PortabilityChange) string`
    - Use severity indicators: error, warning, ℹ️ info
    - Include category, file, message, and suggestion for each issue
    - Include summary line and market-ready status
    - _Requirements: 4.6_

  - [ ]* 5.2 Write unit test for format output
    - `TestFormatReport_SeverityIndicators` — verify //ℹ️ appear in formatted output for each severity level
    - _Requirements: 4.6_

- [x] 6. Checkpoint
  - Run `go build ./corelib/skill/...` to verify compilation
  - Run `go test ./corelib/skill/... -run "TestPortability|TestAutoFix|TestFormat" -v` to verify all corelib tests pass
  - Ask the user if questions arise.

- [x] 7. Integrate validate action into manage_skill tool
  - [x] 7.1 Add `case "validate"` to `toolManageSkill` in `gui/im_tools_misc.go`
    - Extract `name` and `auto_fix` parameters
    - Resolve skill directory from name (reuse existing skill lookup logic)
    - Call `skill.ValidateSkillPortability(skillDir)`
    - If `auto_fix`: call `skill.AutoFixPortability(skillDir)`, then re-validate
    - Return `skill.FormatPortabilityReport` + `skill.FormatPortabilityChanges`
    - Return descriptive error if skill name not found
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x] 7.2 Add `case "validate"` to `toolManageSkill` in `tui/agent_tools.go`
    - Identical logic to GUI, calling the same corelib functions
    - _Requirements: 9.1, 9.2, 9.3_

  - [x] 7.3 Update `manage_skill` tool definition in `gui/im_tool_definitions.go`
    - Add `validate` to the action enum/description
    - Add `auto_fix` parameter (boolean, optional, default false)
    - _Requirements: 4.1, 4.2_

  - [ ]* 7.4 Write unit tests for validate action
    - `TestValidateAction_MissingName` — error when name param missing
    - `TestValidateAction_SkillNotFound` — descriptive error for unknown skill
    - `TestValidateAction_AutoFixFlow` — validate → fix → validate flow via tool action
    - _Requirements: 4.5_

- [x] 8. Integrate pre-upload validation gate
  - [x] 8.1 Add portability validation to `UploadNLSkillToMarket` in `gui/app_nl_skills.go`
    - Run `skill.ValidateSkillPortability(tmpDir)` after `packageSkillForMarket` creates the temp directory
    - Block upload if `report.Summary.Errors > 0` with formatted error message and auto_fix suggestion
    - Allow upload to proceed with warnings/info only
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

  - [ ]* 8.2 Write property test for upload gate decision
    - **Property 5: Upload Gate Decision**
    - Generate `PortabilityReport` with varying error counts; verify upload blocked iff `Summary.Errors > 0`
    - **Validates: Requirements 3.2, 3.3**

  - [ ]* 8.3 Write unit tests for upload gate
    - `TestUploadGate_BlocksOnErrors` — upload blocked with error-severity issues
    - `TestUploadGate_PassesWithWarnings` — upload proceeds with warning-only issues
    - _Requirements: 3.2, 3.3_

- [x] 9. Checkpoint
  - Run `go build ./...` to verify full project compilation
  - Run `go test ./corelib/skill/... ./gui/... ./tui/... -run "TestPortability|TestAutoFix|TestFormat|TestValidateAction|TestUploadGate" -v` to verify all feature tests pass
  - Ask the user if questions arise.

- [x] 10. Validator unit tests and integration tests
  - [ ]* 10.1 Write unit tests for validator edge cases
    - `TestValidate_NonExistentDir` — returns error for missing directory
    - `TestValidate_CleanSkill` — returns zero issues for well-formed skill
    - _Requirements: 1.1, 1.11_

  - [ ]* 10.2 Write integration test for GUI/TUI equivalence
    - `TestGUI_TUI_Equivalence` — both dispatchers produce identical results for the same input
    - _Requirements: 9.3_

- [x] 11. Final checkpoint
  - Run `go build ./...` to verify full project compilation
  - Run `go test ./corelib/skill/... ./gui/... ./tui/... -v` to verify all tests pass (including integration tests)
  - Ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests use the `rapid` library with minimum 100 iterations per property
- All core logic is in `corelib/skill/` so GUI and TUI share a single implementation
- The auto-fixer uses existing `ParseSkillYAMLFile`/`FormatSkillYAMLFile` for Extra field preservation
