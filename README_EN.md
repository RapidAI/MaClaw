# MaClaw

[📖 User Manual](UserManual_EN.md) | [❓ FAQ](faq_en.md) | [English](README_EN.md) | [中文](README.md)

**MaClaw** is a **general-purpose, self-evolving agent platform** — your personal digital work companion. It understands your intent, remembers your preferences, autonomously plans and executes complex tasks. Whether you're writing a business plan, conducting competitive analysis, reviewing contracts, developing software, or managing remote servers, it walks you through the entire process from requirements to deliverables. Built with Wails, Go, and React, it integrates **structured workflows, long-term memory, extensible skills, and multi-channel collaboration**.

> Not just chat — it does the work. You share the idea, it delivers the result.

## What It Can Do

MaClaw ships with **19 structured workflow templates** covering the full spectrum of professional work. Each workflow follows a quality loop of "confirm requirements → design approach → execute step by step", ensuring every deliverable is reviewed and approved by you.

| Domain | Workflows |
|--------|-----------|
| **Business & Strategy** | Business Plan, Competitive Analysis, Project Proposal, Innovation Plan, Bid Response |
| **Research & Analysis** | Literature Review, Research Report, Experiment Design, Patent Analysis |
| **Compliance & Due Diligence** | Contract Review, Due Diligence, Compliance Audit |
| **Academic Writing** | Grant Proposal, Paper Writing |
| **Content Creation** | Presentation Design, Event Planning |
| **Product & Engineering** | Product Design (PRD), Software Testing, Software Development |

Each workflow advances phase by phase. After each phase produces a document, it waits for your confirmation — you can revise, supplement, or skip. It works with you to get things right, not at you.

## Core Capabilities

### Long-Term Memory — It Remembers Everything

MaClaw has a persistent memory system that remembers your preferences, project knowledge, and work habits across sessions:

*   **Semantic retrieval**: BM25 + vector dual indexing — find past memories with natural language
*   **Full-text session search**: SQLite FTS5-powered full-text index over all historical conversations — every conversation is automatically persisted and indexed, supporting cross-session search with BM25 ranking and keyword highlighting, so you can trace back to any past exchange at any time
*   **Automatic capture**: Workflow outputs (requirements docs, design specs, task lists) are automatically saved as long-term memory, surviving conversation truncation
*   **Knowledge graph**: Related memories are automatically linked into a structured network
*   **Memory lifecycle**: Pin, archive, compress, and garbage collect — automatic quality management
*   **Multi-tenant isolation**: Per-user memory isolation in server deployments

### Skill System — Infinitely Extensible

Install skills to give MaClaw new capabilities, like installing apps on a phone:

*   **Multiple formats**: YAML definitions, Markdown scripts, Claude SKILL.md format
*   **Multi-step workflows**: Sequential execution, conditional branches, variable passing, output capture
*   **Three marketplaces**: Search and install from SkillHub (official), ClawHub (community), and GitHub
*   **Cross-platform**: Automatic path normalization and shell adaptation for Windows / macOS / Linux
*   **Self-evolution**: `craft_tool` dynamically generates one-off automation scripts; validated scripts can become reusable skills

### MCP Integration — Connect to the Outside World

Extend capabilities through Model Context Protocol (MCP):

*   **Dynamic discovery**: Automatically discover tools provided by MCP Servers
*   **Local + remote**: Stdio local protocol and HTTP remote protocol
*   **Health monitoring**: Automatic MCP Server status detection
*   **Unlimited extension**: Any MCP-compatible service becomes a MaClaw capability

### Tool Routing — Smart Tool Selection

MaClaw has 40+ built-in tools and uses hybrid retrieval to select the best tool combination for each task:

*   **Hybrid retrieval**: BM25 + vector semantic dual matching
*   **Conditional activation**: SSH, browser, and other tools activate on-demand by context keywords
*   **Progressive disclosure**: Core tools always available; low-frequency tools loaded via `discover_tool` on demand
*   **Outcome learning**: Success/failure/retry records feed back into routing decisions — high-failure tools are automatically deprioritized

### Self-Evolution — Automatic Capability Gap Filling

MaClaw doesn't just execute passively — it proactively discovers its own capability gaps and fills them:

*   **Capability gap detection**: When the Agent can't complete a task, it automatically searches SkillHub for matching skills and installs them
*   **Skill self-repair**: After a skill execution failure, the LLM analyzes the error and patches the skill definition (steps, parameters, paths), persisting the fix
*   **Nudge system**: After completing complex tasks, the system suggests packaging successful operation sequences into reusable skills, driving organic growth of the skill library
*   **craft_tool conversion**: One-off automation scripts can be converted into permanent skills after validation

### Office Document Processing

Built-in document generation and processing:

*   **PDF generation**: Generate PDFs directly from Markdown; workflow phase documents are auto-generated as PDFs and sent via IM
*   **Excel read/write**: Read and write Excel files
*   **PPTX reading**: Parse PowerPoint file content
*   **File sending**: Generated files can be sent directly through IM channels (Feishu/WeChat/QQ)

### Information Retrieval

*   **Web search**: Search the internet, returning titles, URLs, and snippets
*   **Web fetch**: Extract page content from URLs with automatic encoding detection (GBK/UTF-8), JS rendering support, and long-page continuation
*   **Screenshot**: Capture the desktop screen and send to users, supporting remote IM supervision scenarios

### Voice Processing

*   **Voice message recognition**: Voice messages from IM channels are automatically converted to WAV format with ASR support (built-in Moonshine model)
*   **Voiceprint identification**: ECAPA embedding-based voiceprint enrollment and 1:N identity matching (Hub-side capability)

### Scheduled Tasks

Create automated tasks that run on a schedule:

*   Daily/weekly/monthly scheduling with one-time task support
*   Natural language task descriptions, auto-executed at the scheduled time
*   Pause, resume, and delete tasks

### AgentNet — P2P Agent Network

Decentralized agent collaboration network (experimental):

*   Node discovery, knowledge publishing and search, credit system
*   Cross-node task delegation, Swarm collaboration
*   Reputation system, dispute arbitration, DAG task orchestration

### Audit Logging

Full operation recording for compliance review:

*   Tool calls, file operations, SSH commands, and other key operations are automatically logged
*   Audit logs queryable via tools

### Intent Understanding

Three-layer fused intent classification system for accurate request understanding:

*   **Layer 1**: Keyword rules (<1ms)
*   **Layer 2**: BM25 semantic retrieval (<5ms)
*   **Layer 3**: LLM multi-turn dialogue confirmation (10-30s)
*   Automatic routing to the matching workflow template or direct execution

### Behavior Customization — Steering Rules

Declare behavior rules in Markdown files to customize how MaClaw works, without code changes:

*   **Four injection modes**: Always, file-match, keyword-match, manual reference
*   **Two scopes**: User-level (`~/.maclaw/steering/`) and project-level (`<project>/.maclaw/steering/`)
*   **Token budgeting**: Intelligent control of rule injection volume
*   **Hot reload**: Changes take effect within 30 seconds

## Multi-Modal Interaction

MaClaw isn't limited to one interface — collaborate through multiple entry points:

| Mode | Description |
|------|-------------|
| **Desktop AI Assistant Panel** | Native GUI with right-side Markdown preview for workflow documents, streaming output |
| **Terminal TUI** | Command-line interface with chat, memory viewer, skill management, scrollbar, and streaming |
| **WeChat / Feishu / QQ / Telegram** | Chat with MaClaw through IM channels — give instructions from your phone |
| **REST API (MaClawSrv)** | Multi-tenant REST service for external programs, automation platforms, and control planes |

### Dual-Mode Experience

| Mode | For | Features |
|------|-----|----------|
| **Professional** | Developers, researchers | Full access to all tools, workflows, memory, MCP management |
| **Simplified** | Office workers, everyday users | Streamlined interface, conversational AI focus, zero-barrier onboarding |

## Execution Capabilities

### SSH Remote Management

Built-in SSH client for managing remote servers directly from conversation:

*   Up to 10 concurrent sessions with password/key/agent authentication
*   Sync execution, background tasks, file upload/download
*   Automatic sudo token management, shell responsiveness detection, auto-cleanup on consecutive failures
*   Full operation audit logging

### Browser Automation

Chrome DevTools Protocol-based browser interaction:

*   Page navigation, element clicking, text input, content extraction, screenshots
*   Flow recording and replay with scheduled triggers and parameterized variables
*   OCR integration (RapidOCR + LLM vision models)

### Desktop GUI Automation

Directly control native desktop applications (Notepad, Excel, Calculator, or any native app) without a browser:

*   **Accessibility element tree**: Cross-platform (Windows/macOS/Linux) window control structure reading — buttons, text fields, menus, and more
*   **YOLO visual detection**: Built-in OmniParser V2 model detects interactable UI elements (buttons, icons, inputs) from screenshots, independent of Accessibility APIs
*   **Mouse and keyboard**: Click at coordinates, type text
*   **Flow recording and replay**: Record GUI operation sequences, save as replayable flows with parameter overrides and async background execution
*   **State observation and verification**: `gui_observe` returns the window element tree + OCR text (plain text, no vision token cost); `gui_verify` checks GUI state against criteria (text contains, element exists, window exists, etc.)
*   **Multi-monitor support**: List all connected displays, screenshot specific monitors

### Software Development

Programming is one of MaClaw's work capabilities, executed through coding workflows and external programming tools:

*   **Structured process**: Requirements → Design → Task Breakdown → Per-task Execution → Integration
*   **Multi-tool support**: Claude Code, Codex, Gemini CLI, OpenCode, CodeBuddy, Qoder CLI
*   **Coding SubAgent**: Clean-context executor — each task gets independent context, no history bloat
*   **Swarm orchestration**: Large tasks split across multiple AI developers with automatic merging

### Local Background Process Management

Launch local background tasks via `bash(background=true)` with automatic PID and log capture:

*   Non-blocking status queries, blocking wait, task termination
*   Symmetric Submit / Check / Wait / Kill pattern matching SSH background tasks

## Quick Start

### Four Steps to Get Started

| Step | What | Details |
|------|------|---------|
| **Register** | Email signup | Unlocks remote collaboration |
| **Choose Mode** | Professional / Simplified | Switchable anytime |
| **Configure AI** | Select LLM provider | Enter API Key and test connectivity; OAuth and free trial also available |
| **Bind IM (optional)** | Scan QR for WeChat | Chat with MaClaw from your phone anytime |

### Run

*   Windows: `MaClaw.exe`
*   macOS: `MaClaw.app`
*   Linux: `MaClaw.AppImage`
*   Terminal: `maclaw-tui`

First launch auto-detects the environment and installs missing components.

## MaClawSrv — REST Agent Service

[MaClawSrv](MaClawSrv/) is MaClaw's multi-tenant REST service entrypoint, exposing Agent capabilities as standard HTTP APIs for external programs.

**Key Features**:

*   **Multi-tenant isolation**: Data isolated at `tenant → user` level; multiple instances per user
*   **Shared user data**: All instances share config, memory, skills, MCP state
*   **Security first**: Dual-layer auth (admin + user); scrypt credential storage; TLS support
*   **Full API coverage**: Admin control plane, user config, Instance/Session/Message/Run runtime, Skill/MCP lifecycle, async Jobs, Usage/Audit/Dashboard

```bash
export MACLAW_ADMIN_SECRET="your-admin-secret-at-least-24-chars"
export MACLAW_TOKEN_SECRET="your-token-secret-at-least-32-chars"
go run ./MaClawSrv
```

**API Documentation**:

| Document | Description |
|----------|-------------|
| [README](MaClawSrv/README.md) | Project positioning, API groups, security model, data layout |
| [API Manual (English)](MaClawSrv/API_MANUAL.md) | Full field-level reference with auth, pagination, error model |
| [API 对接手册（中文）](MaClawSrv/API_MANUAL.zh-CN.md) | Chinese API reference |
| [Quickstart (English)](MaClawSrv/QUICKSTART.md) | Shortest usable path |
| [5 分钟快速接入](MaClawSrv/QUICKSTART.zh-CN.md) | Chinese quickstart |
| [Gap Analysis](MaClawSrv/GAP_ANALYSIS.md) | Implemented capabilities and remaining gaps |
| [缺口分析](MaClawSrv/GAP_ANALYSIS.zh-CN.md) | Chinese gap analysis |
| OpenAPI | Available at `GET /openapi.json` when the service is running |

## srvdemo — API Demo Client

[srvdemo](srvdemo/) is a Go + Wails desktop client demonstrating how to integrate with all MaClawSrv APIs. One-click demo data setup, token exchange, config management, full Instance/Session/Message/Run lifecycle, Skill and MCP management. See [srvdemo/README.md](srvdemo/README.md).

## License (Dual License)

*   **Open source use**: Free to use in open source projects
*   **Commercial use**: Commercial license required — contact **znsoft@163.com**

## About

*   **Author**: Dr. Daniel
*   **GitHub**: [RapidAI/MaClaw](https://github.com/rapidai/maclaw)
*   **Website**: [maclaw.top](https://maclaw.top)

---
*This tool is intended as a configuration management aid. Please ensure you comply with the service terms of each model provider.*
