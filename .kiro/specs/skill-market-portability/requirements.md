# Requirements Document

## Introduction

Self-crafted skills (source: "crafted", "file", "learned") often contain hardcoded absolute paths, missing platform declarations, incomplete metadata, and platform-specific script syntax that prevent them from working on other machines. This feature adds a pre-upload portability validation and auto-fix pipeline that ensures skills meet market-readiness standards before being uploaded to SkillMarket. The pipeline is invoked automatically during `UploadNLSkillToMarket` and also exposed as a standalone `validate` action in the `manage_skill` tool, so users and LLMs can check and fix portability issues without uploading.

## Glossary

- **Portability_Validator**: The new `ValidateSkillPortability()` function in `corelib/skill/` that checks a skill for market-readiness issues and returns a structured report
- **Portability_Report**: The structured result of validation, containing a list of issues (each with severity, category, message, and optional auto-fix suggestion)
- **Auto_Fixer**: The `AutoFixPortability()` function that applies safe, reversible fixes to a skill's files (path normalization, metadata defaults, placeholder insertion)
- **Skill_Directory**: The on-disk directory containing a skill's `skill.yaml`, `SKILL.md`, scripts, and supporting files
- **Absolute_Path**: A file path starting with a drive letter (e.g. `C:\Users\...`) or root slash (e.g. `/home/user/...`) that is specific to one machine
- **BaseDir_Placeholder**: The `{baseDir}` or `{base_dir}` token that the runtime resolves to the skill's directory at execution time
- **NLSkillEntry**: The runtime struct in `corelib/types.go` representing a loaded skill
- **SkillYAMLFile**: The parsed struct in `corelib/skill/scanner.go` representing a `skill.yaml` file
- **Manage_Skill_Tool**: The unified `manage_skill` LLM tool that provides skill management actions
- **SkillMarket**: The remote marketplace where users upload and share skills
- **Platform_Declaration**: The `platforms` field in `skill.yaml` listing supported operating systems (windows, linux, macos, universal)

## Requirements

### Requirement 1: Portability Validation Engine

**User Story:** As a skill author, I want to validate my skill for portability issues before uploading, so that I can identify and fix problems that would prevent the skill from working on other machines.

#### Acceptance Criteria

1. THE Portability_Validator SHALL accept a Skill_Directory path and return a Portability_Report containing zero or more issues
2. WHEN a bash step command contains an Absolute_Path that is not a well-known system binary path (e.g. `/usr/bin/python3`, `/bin/bash`), THE Portability_Validator SHALL report an issue with severity "error" and category "hardcoded_path"
3. WHEN a bash step command contains a user home directory reference (e.g. `/home/username/`, `C:\Users\username\`), THE Portability_Validator SHALL report an issue with severity "error" and category "hardcoded_path"
   - System binary paths (`/usr/bin/`, `/bin/`, `/usr/local/bin/`) SHALL NOT be flagged as hardcoded paths
4. WHEN a bash step command references a file inside the Skill_Directory using an Absolute_Path instead of a BaseDir_Placeholder, THE Portability_Validator SHALL report an issue with severity "error" and category "missing_basedir" and include the suggested replacement using `{baseDir}`
5. WHEN the `platforms` field is empty or missing in `skill.yaml`, THE Portability_Validator SHALL report an issue with severity "warning" and category "missing_platforms"
6. WHEN the `description` field is empty or shorter than 10 characters, THE Portability_Validator SHALL report an issue with severity "warning" and category "incomplete_metadata"
7. WHEN the `triggers` field is empty, THE Portability_Validator SHALL report an issue with severity "warning" and category "incomplete_metadata"
8. WHEN a bash step command references `python3` without a platform-conditional fallback, THE Portability_Validator SHALL report an issue with severity "info" and category "platform_compat" noting that Windows may require `python` instead of `python3`
9. WHEN a bash step command contains a backslash path separator pattern (e.g. `scripts\run.py`, `subdir\file.txt`) that is not a shell escape sequence (`\n`, `\t`, `\"`, `\\`), THE Portability_Validator SHALL report an issue with severity "warning" and category "path_separator" noting that forward slashes are portable across all platforms
10. WHEN a bash step command references an environment variable using Windows syntax (`%VAR%`) without a cross-platform alternative, THE Portability_Validator SHALL report an issue with severity "warning" and category "platform_compat"
11. IF the Skill_Directory does not exist or is not readable, THEN THE Portability_Validator SHALL return an error
12. FOR ALL valid Skill_Directory inputs, validating then auto-fixing then validating again SHALL produce a report with zero "error" severity issues (round-trip: validate → fix → validate produces clean result)

### Requirement 2: Automatic Portability Fixes

**User Story:** As a skill author, I want the system to automatically fix common portability issues, so that I do not have to manually edit every hardcoded path and missing field.

#### Acceptance Criteria

1. WHEN Auto_Fixer encounters a bash step command containing an Absolute_Path that points inside the Skill_Directory, THE Auto_Fixer SHALL replace the Absolute_Path with a `{baseDir}`-relative path
2. WHEN Auto_Fixer encounters a `platforms` field that is empty, THE Auto_Fixer SHALL set it to `["universal"]`
3. WHEN Auto_Fixer encounters a bash step command containing backslash path separators in file references, THE Auto_Fixer SHALL replace backslashes with forward slashes
4. WHEN Auto_Fixer encounters a bash step command containing a user home directory path (matching the current machine's home directory), THE Auto_Fixer SHALL replace it with `$HOME` (POSIX) or note the replacement in the report
5. THE Auto_Fixer SHALL write all changes back to the `skill.yaml` file in the Skill_Directory
6. THE Auto_Fixer SHALL create a backup of the original `skill.yaml` as `skill.yaml.bak` before making changes
7. THE Auto_Fixer SHALL return a list of changes made, each with the original value and the replacement value
8. IF no fixable issues are found, THEN THE Auto_Fixer SHALL make no changes and return an empty change list
9. THE Auto_Fixer SHALL preserve all fields in `skill.yaml` that are not being fixed, including `Extra` fields parsed by `ParseSkillYAMLFile`

### Requirement 3: Pre-Upload Validation Gate

**User Story:** As a skill marketplace operator, I want skills to be validated for portability before upload, so that marketplace users receive skills that work across platforms.

#### Acceptance Criteria

1. WHEN `UploadNLSkillToMarket` is called (GUI) or the `upload` action is invoked (manage_skill tool), THE system SHALL run the Portability_Validator on the skill before packaging
2. IF the Portability_Report contains one or more issues with severity "error", THEN THE system SHALL block the upload and return an error message listing all error-severity issues
3. IF the Portability_Report contains only "warning" or "info" severity issues, THEN THE system SHALL proceed with the upload and include the warnings in the success response
4. WHEN the upload is blocked due to validation errors, THE system SHALL suggest running the `validate` action with `auto_fix=true` to attempt automatic fixes
5. THE pre-upload validation SHALL run after `packageSkillForMarket` copies the skill to a temporary directory, so that validation operates on the packaged copy (not the original)

### Requirement 4: Validate Action in manage_skill Tool

**User Story:** As an LLM, I want a `validate` action in `manage_skill`, so that I can check a skill's portability and optionally auto-fix issues before the user uploads.

#### Acceptance Criteria

1. WHEN action is `validate`, THE Manage_Skill_Tool SHALL require a `name` parameter of type string
2. WHEN action is `validate`, THE Manage_Skill_Tool SHALL accept an optional `auto_fix` parameter of type boolean (default false)
3. WHEN action is `validate` and `auto_fix` is false, THE Manage_Skill_Tool SHALL run the Portability_Validator and return the Portability_Report as a formatted text summary
4. WHEN action is `validate` and `auto_fix` is true, THE Manage_Skill_Tool SHALL run the Portability_Validator, then run the Auto_Fixer, then run the Portability_Validator again, and return both the changes made and the final report
5. IF the skill name does not match any installed skill, THEN THE Manage_Skill_Tool SHALL return a descriptive error message
6. THE Manage_Skill_Tool SHALL format the report with clear severity indicators (error, warning, ℹ️ info) and actionable fix suggestions

### Requirement 5: SKILL.md Portability Validation

**User Story:** As a skill author who uses SKILL.md format, I want portability validation to also check my markdown-based skill definitions, so that SKILL.md skills are equally market-ready.

#### Acceptance Criteria

1. WHEN a Skill_Directory contains a `SKILL.md` file, THE Portability_Validator SHALL extract bash code blocks and validate them for hardcoded paths
2. WHEN a bash code block in `SKILL.md` contains an Absolute_Path that should use `{baseDir}`, THE Portability_Validator SHALL report an issue with category "hardcoded_path" and reference the SKILL.md file
3. WHEN Auto_Fixer processes a Skill_Directory with `SKILL.md`, THE Auto_Fixer SHALL replace Absolute_Paths in bash code blocks with `{baseDir}`-relative paths
4. THE Auto_Fixer SHALL create a backup of the original `SKILL.md` as `SKILL.md.bak` before making changes

### Requirement 6: Cross-Platform Script Wrapper Detection

**User Story:** As a skill author, I want to be warned when my skill uses platform-specific commands without cross-platform alternatives, so that I can make the skill work on all platforms.

#### Acceptance Criteria

1. WHEN a bash step command uses a shebang line (`#!/bin/bash` or `#!/usr/bin/env bash`), THE Portability_Validator SHALL report an issue with severity "info" and category "platform_compat" noting that Windows requires Git Bash or WSL
2. WHEN a bash step command uses `chmod`, `ln -s`, or other POSIX-only commands, THE Portability_Validator SHALL report an issue with severity "warning" and category "platform_compat" if the skill's platforms include "windows" or "universal"
3. WHEN a bash step command uses `cmd.exe` or PowerShell-specific syntax (e.g. `$env:`, `[Console]::`, `.ps1`), THE Portability_Validator SHALL report an issue with severity "warning" and category "platform_compat" if the skill's platforms include "linux" or "macos" or "universal"
4. WHEN the `preferred_shell` field is set to a platform-specific shell (e.g. "cmd") but the `platforms` field includes non-matching platforms, THE Portability_Validator SHALL report an issue with severity "warning" and category "shell_mismatch"

### Requirement 7: Dependency Declaration Validation

**User Story:** As a skill author, I want to be warned about undeclared external dependencies, so that marketplace users know what to install before using my skill.

#### Acceptance Criteria

1. WHEN a bash step command references `python`, `python3`, `node`, `npm`, `pip`, `pip3`, or other common runtime commands, THE Portability_Validator SHALL check if the skill's `required_env` or description mentions the dependency
2. IF a referenced command is not mentioned in `required_env` or description, THEN THE Portability_Validator SHALL report an issue with severity "warning" and category "undeclared_dependency" with the command name
3. WHEN a bash step command uses `pip install` or `npm install`, THE Portability_Validator SHALL report an issue with severity "info" and category "runtime_install" noting that marketplace skills should bundle dependencies or document installation requirements

### Requirement 8: Portability Report Serialization

**User Story:** As a developer, I want the portability report to be serializable to JSON, so that it can be stored, transmitted, and displayed in both GUI and TUI.

#### Acceptance Criteria

1. THE Portability_Report SHALL be serializable to JSON with fields: `skill_name`, `skill_dir`, `issues` (array), `summary` (object with counts by severity), `timestamp`
2. EACH issue in the Portability_Report SHALL contain fields: `severity` (error/warning/info), `category` (string), `message` (string), `file` (string, which file the issue was found in), `line` (int, optional), `suggestion` (string, optional auto-fix suggestion)
3. THE Portability_Report SHALL include a `market_ready` boolean field that is true when there are zero "error" severity issues
4. FOR ALL Portability_Report instances, serializing to JSON then deserializing SHALL produce an equivalent report (round-trip property)

### Requirement 9: TUI Validate Support

**User Story:** As a TUI user, I want to validate and auto-fix skill portability from the terminal, so that I have the same capability as GUI users.

#### Acceptance Criteria

1. WHEN the TUI receives a `manage_skill` call with action `validate`, THE TUI SHALL run the same Portability_Validator and return the same formatted report as the GUI
2. WHEN the TUI receives a `manage_skill` call with action `validate` and `auto_fix=true`, THE TUI SHALL run the same Auto_Fixer and return the same change list as the GUI
3. FOR ALL validate action inputs, THE TUI SHALL produce equivalent results to the GUI

