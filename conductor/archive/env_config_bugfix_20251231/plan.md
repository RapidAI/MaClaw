# Implementation Plan - Targeted Environment Configuration Bugfix

This plan outlines the steps to fix the environment configuration logic, ensuring correct cleanup and setup for Claude and Codex based on the selected provider.

## Phase 1: Core Logic Refactoring & Infrastructure
- [x] Task: Audit and consolidate configuration paths for all tools in `common.go` or `app.go`.
- [x] Task: Implement standardized, tool-specific cleanup functions (e.g., `clearClaudeConfig`, `clearCodexConfig`).
- [x] Task: Create a unified environment variable clearing utility for the launch process.
- [x] Task: Conductor - User Manual Verification 'Phase 1: Core Infrastructure' (Protocol in workflow.md) [checkpoint: 505f065]

## Phase 2: Claude Code Configuration Fixes
- [x] Task: Update `syncToClaudeSettings` to handle the "Original" provider by strictly deleting files and returning early.
- [x] Task: Refactor `LaunchTool` for Claude to ensure environment variables are unset before launch in "Original" mode.
- [x] Task: Write unit tests to verify Claude config file deletion and variable unsetting.
- [x] Task: Conductor - User Manual Verification 'Phase 2: Claude Code' (Protocol in workflow.md) [checkpoint: 80edf01]

## Phase 4: OpenAI Codex Configuration Fixes
- [x] Task: Update `syncToCodexSettings` to handle the "Original" provider by deleting `.codex` directory or files.
- [x] Task: Refactor `LaunchTool` for Codex to handle `OPENAI_API_KEY` and `WIRE_API` unsetting in "Original" mode.
- [x] Task: Write unit tests to verify Codex cleanup and custom setup.
- [x] Task: Conductor - User Manual Verification 'Phase 4: OpenAI Codex' (Protocol in workflow.md) [checkpoint: d587097]

## Phase 5: Global Launch & Persistence Refinement
- [x] Task: Ensure `SaveConfig` does not inadvertently write "leak" data when switching providers.
- [x] Task: Final verification of system-level environment variable sync (`setx` logic) to ensure it respects the "Original" mode.
- [x] Task: Conductor - User Manual Verification 'Phase 5: Global Integration' (Protocol in workflow.md)
