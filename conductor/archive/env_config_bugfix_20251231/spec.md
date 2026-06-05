# Specification: targeted-env-config-bugfix

## Overview
This track addresses a bug where environment configuration and cleanup are not correctly applied based on the selected service provider and CLI tool. The goal is to ensure that every time a tool is launched, the environment is prepared exactly according to the user's selection: either a clean "Original" state or a correctly configured "Custom/Third-party" state.

## Functional Requirements
1.  **Launch-Time Configuration:** The configuration logic MUST execute every time the "Launch" button is clicked for any tool (Claude, Codex).
2.  **"Original" (原厂) Provider Logic:**
    *   If "Original" is selected for the active tool, the application MUST clear all known configuration files and environment variables associated with that specific CLI.
    *   **NO** new configuration data should be written to files or environment variables.
    *   Target items for Claude: `~/.claude/`, `~/.claude.json`, `ANTHROPIC_AUTH_TOKEN`, etc.
    *   Target items for Codex: `~/.codex/`, `OPENAI_API_KEY`, `WIRE_API`, etc.
3.  **Third-Party/Custom Provider Logic:**
    *   If a non-"Original" provider is selected, the application MUST first perform the cleanup logic described above.
    *   Subsequently, it MUST write the correct `API Key` and `Base URL` to the appropriate configuration files and environment variables required by that specific CLI.
4.  **CLI-Specific Targeting:** Cleanup and configuration MUST be restricted to the tool currently being launched to avoid disrupting other tools.

## Non-Functional Requirements
- **Reliability:** File deletion and writing operations should handle errors gracefully (e.g., if a file doesn't exist, it shouldn't crash).
- **Transparency:** Logs should clearly indicate which files were cleared and which variables were set.

## Acceptance Criteria
- [ ] Launching a tool with "Original" selected results in no `API_KEY` environment variables being set for the subprocess and no local config files being present.
- [ ] Launching a tool with a custom provider correctly sets the environment variables and populates the config files (e.g., `settings.json` for Claude).
- [ ] Switching tools and launching does not leave "leak" configurations from one tool to another.

## Out of Scope
- Modifying the global system environment variables beyond the scope of the tool's execution process (unless required for CLI persistence).
- UI changes to the sidebar or project management layout.
