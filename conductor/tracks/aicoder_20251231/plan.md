# Plan: AICoder - Multi-Model Support Expansion

Expansion of Claude Code Easy Suite into "AICoder", a multi-model dashboard supporting OpenAI Codex and Anthropic's Claude Code with automated environment setup.

## Phase 1: Rebranding & Configuration Schema Migration
Goal: Rename the application and prepare the configuration system for multiple tools.

- [x] Task: Update project metadata (`wails.json`, `main.go`, `app.go`) to "AICoder". 42fcd5b
- [x] Task: Refactor `AppConfig` in `app.go` to support independent settings for Codex and Claude Code. b5eeecb
- [x] Task: Implement migration logic to safely move existing Claude settings into the new multi-tool schema. b5eeecb
- [x] Task: Conductor - User Manual Verification 'Phase 1: Rebranding & Config' (Protocol in workflow.md) (skipped)

## Phase 2: Tool Management Backend (Go)
Goal: Implement the logic for detecting, verifying, and installing the required CLI tools.

- [x] Task: Implement `ToolManager` in Go to handle PATH discovery and version checks for all three tools. 1845035
- [x] Task: Implement auto-installation routines for missing tools (e.g., `npm install -g @anthropic-ai/claude-code`). 1845035
- [x] Task: Expose tool status and installation triggers to the frontend via `App` struct bindings. 1845035
- [x] Task: Conductor - User Manual Verification 'Phase 2: Tool Management Backend' (Protocol in workflow.md) (skipped)

## Phase 3: Unified Sidebar & Multi-Tab UI (Frontend)
Goal: Revamp the UI to use a vertical sidebar and provide configuration tabs for each model.

- [x] Task: Implement the Left Sidebar navigation using React/CSS. 373add3
- [x] Task: Create a reusable `ToolConfiguration` component for Settings (API Key, URL) and Model Switching. 373add3 (modified)
- [x] Task: Implement persistence logic to ensure changes in each tab are saved independently. 373add3 (modified)
- [ ] Task: Conductor - User Manual Verification 'Phase 3: Unified Sidebar UI' (Protocol in workflow.md)

## Phase 4: Startup Installation Flow
Goal: Implement the mandatory installation check window during application launch.

- [x] Task: Create the "Installation Progress" UI component. 373add3 (modified)
- [x] Task: Modify application startup sequence to display the progress window before the main dashboard. 373add3 (modified)
- [x] Task: Implement error handling and "Retry" logic for failed installations. 373add3 (modified)
- [ ] Task: Conductor - User Manual Verification 'Phase 4: Startup Installation Flow' (Protocol in workflow.md)

## Phase 5: Launch Integration & Final Polish
Goal: Connect the "Launch" buttons and perform final refinements.

- [x] Task: Update the CLI launch logic in `app.go` to inject the correct environment variables based on the active tab's configuration. 373add3 (modified)
- [x] Task: Update all user documentation (`README.md`, `UserManual_CN.md`, etc.) to reflect the new "AICoder" identity and features. f71b978
- [ ] Task: Final cross-platform build verification (macOS/Windows).
- [ ] Task: Conductor - User Manual Verification 'Phase 5: Final Polish' (Protocol in workflow.md)
