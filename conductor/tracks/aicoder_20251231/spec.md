# Specification: AICoder - Multi-Model Support Expansion

## Overview
Rebrand the application to "AICoder" and expand support to include AI coding assistants such as OpenAI Codex and Anthropic's Claude Code. The application will ensure these tools are correctly installed at startup and provide a unified interface to configure and launch each one.

## Functional Requirements

### 1. Full Rebranding to "AICoder"
- Update application display name in UI (Window Title, About dialog).
- Rename build artifacts and executable to `AICoder`.
- Update internal project identifiers, bundle IDs (macOS), and application metadata.

### 2. Startup Installation & Verification
- **Installation Window:** Display a progress window during startup.
- **Verification Steps:**
    - Check if `codex` and `claude-code` are installed (PATH check).
    - Verify versions meet minimum requirements.
    - **Auto-Installation:** If a tool is missing or outdated, attempt to install it automatically (e.g., via `npm` or `go` as appropriate).
- **Blocking Progress:** Startup only continues if tools are verified or successfully installed/updated.

### 3. Unified Tabbed Interface (Vertical Sidebar)
- Implement a sidebar on the left for navigation between supported coding tools such as **Codex** and **Claude Code**.
- Each tab must provide:
    - **Model Settings:** Configuration for API Keys and Base URLs.
    - **Model Switching:** Ability to switch between different service providers/endpoints for the same tool.
    - **Launch Action:** A button to trigger the execution of the respective CLI tool with the current configuration.

## Non-Functional Requirements
- **Consistency:** Maintain a uniform layout and styling across all three tabs.
- **Robustness:** Gracefully handle installation failures with clear user guidance.
- **Platform Support:** Ensure installation logic works across macOS, Windows, and Linux.

## Acceptance Criteria
- [ ] Application title is "AICoder" and executable is named accordingly.
- [ ] Startup window correctly detects missing tools and attempts installation.
- [ ] Sidebar navigation allows switching between tool configurations.
- [ ] Changes to API Key/URL in one tab are persisted and used when launching that specific tool.
- [ ] Supported tools can be launched from their respective tabs.

## Out of Scope
- Integration with other AI models not mentioned (Codex, Claude).
- Direct terminal emulation within the GUI (launches external terminal as per existing pattern).
