# MaClaw

[User Manual](UserManual_EN.md) | [FAQ](faq_en.md) | [English](README_EN.md) | [中文](README.md)

**MaClaw** is a **general-purpose, self-evolving agent platform** — your personal digital work companion. It understands your intent, remembers your preferences, autonomously plans and executes complex tasks. Whether you're writing a business plan, conducting competitive analysis, reviewing contracts, developing software, or managing remote servers, it walks you through the entire process from requirements to deliverables. Built with Wails, Go, and React, it integrates **structured workflows, knowledge base (external brain), long-term memory, extensible skills, and multi-channel collaboration**, and replaces traditional enterprise MIS fixed-form entries through **Agent Dynamic UI + Structured Data Management**. The Enterprise Edition provides **Digital Workers** — digital twins of physical users and cloud-native virtual employees — running 24/7 to replace repetitive positions and reduce labor costs.

> Not just chat — it does the work. You share the idea, it delivers the result. It has its own knowledge base — documents and web pages you import become its "external brain", enabling it to work with knowledge at hand.

## What It Can Do

MaClaw ships with **38 structured workflow templates** (registered in `corelib/workflow/v2` via `RegisterBuiltinTemplates`), covering business decisions, academic applications, and controlled ops changes. Each workflow follows a quality loop of "confirm requirements → design approach → execute step by step", ensuring every deliverable is reviewed and approved by you.

| Domain | Workflows |
|--------|-----------|
| **Business & Strategy** | Business Plan, Competitive Analysis, Project Proposal, Innovation Plan, Bid Response, Bid Review |
| **Research & Analysis** | Literature Review, Research Report, Experiment Design, Patent Analysis, Paper Reproduction |
| **Compliance & Due Diligence** | Contract Review, Due Diligence, Compliance Audit |
| **Academic Writing & Funding** | Grant Proposal, Paper Writing; Changjiang Scholar / NSFC Distinguished Youth / Excellent Youth / Youth / General / Key applications and matching review templates |
| **IP** | China Patent Application, US Patent (USPTO) Application |
| **Admissions Planning** | Gaokao (college entrance) application reference |
| **Content Creation** | Presentation Design, Event Planning |
| **Product & Engineering** | Product Design (PRD), Testing Plan, Coding Project, Maintenance/Refactor |
| **Ops & Change** | Ops Maintenance (intake → read-only collection → risk policy → controlled execution) |

Each workflow advances phase by phase. After each phase produces a document, it waits for your confirmation — you can revise, supplement, or skip. It works with you to get things right, not at you.

## Core Capabilities

### Enterprise MIS Replacement — Agent Dynamic UI + Structured Data Management

One of MaClaw's ultimate goals is to **replace traditional enterprise MIS systems' fixed menus, fixed pages, and fixed forms with Agent + natural language interaction**. Users no longer need to know "which system to open, which page to navigate to, which fields to fill in" — they simply express intent through natural language, images, voice, or files, and the system automatically handles everything from intent understanding to structured data persistence.

#### Agent Dynamic UI (AG-UI)

Traditional MIS form entries are replaced by Agent-generated controlled interfaces:

* **Dynamic form generation**: After understanding user intent, the Agent automatically generates input interfaces (forms, wizards, table editors, approval confirmations) from the Schema registry — no pre-built pages needed
* **Smart field extraction**: Automatically extracts candidate field values from natural language, attachments, images, and existing data — users only need to complete and confirm
* **Right-side Task Panel**: Left side for conversation + right side for structured operations — conversation handles understanding and explanation, panel handles precise input and confirmation
* **Adapter auto-inference**: Automatically generates UI adapters from Skill/Tool/MCP schemas, OpenAPI specs, and function signatures — standard Skills need no modification
* **Business object recognition**: User says "went to Hangzhou yesterday to meet a client, train ¥174, lunch ¥86" — system automatically recognizes it as an expense claim scenario and generates a draft
* **Security-controlled**: All interfaces rendered from registered components and Schema whitelists — LLM cannot generate arbitrary frontend code, cannot bypass validation, permissions, or approvals

#### Structured Data Management (MaClawDataSrv)

Built-in enterprise-grade structured data platform, replacing traditional MIS database layers:

* **Business datasets**: Supports Sales (customers/opportunities/orders), Finance (expenses/invoices/payments/vouchers), HR (employees/attendance/payroll), Legal (contracts), Procurement (suppliers/purchase orders), Inventory (items/warehouses/movements), Fixed Assets — complete enterprise data structures
* **One-click template initialization**: 30+ built-in enterprise MIS templates (customer management, sales orders, expense claims, payroll, contracts, procurement, inventory, etc.) — one click to create datasets and field definitions
* **Business action catalog**: Agent doesn't operate raw CRUD — it executes through business actions (expense_submit, order_upsert, contract_status_update, etc.) with built-in validation, approval, and audit
* **Business views**: Pre-defined query views (order overview, customer directory, expense review, contract register, etc.) with field projection and permission control — no raw SQL exposed to Agent
* **Dashboards & reports**: Built-in business dashboards and reports that Agent can run directly
* **Governance & approval**: Business rules engine (amount thresholds, duplicate detection, required field validation) + approval workflows (pending → approve/reject) + operation plans (high-risk operations require admin confirmation)
* **Full audit trail**: Every structured submission records complete audit logs (original input, Agent extraction results, user modifications, final data, validation results, approval results)
* **Multi-engine support**: Local SQLite (personal/small team) → PostgreSQL (team/enterprise), same API seamless switch
* **Agent-friendly API**: Agent calls business actions via `mis_data` tool, supporting dry-run pre-checks, business intent resolution, capability discovery, and operation plan generation

#### Comparison with Traditional MIS

| Dimension | Traditional MIS | MaClaw AG-UI + DataSrv |
|-----------|----------------|----------------------|
| **Entry point** | Fixed menu → Fixed page → Fixed form | Natural language → Agent understanding → Dynamic UI |
| **Development cost** | Custom page for each business scenario | Schema declaration + Adapter auto-inference |
| **User barrier** | Training required, memorize navigation paths | Just speak naturally, zero learning curve |
| **Data entry** | Manual field-by-field input | Agent auto-extracts from conversation/attachments, user confirms |
| **Query method** | Fixed filter conditions | Natural language queries, Agent auto-converts |
| **Extension** | Change code, change database, change pages | Add templates, add Schemas, add business action definitions |

### Knowledge Base (External Brain) — Structured Knowledge Engine

MaClaw includes a full-featured knowledge base system (external brain), built on SQLite, that parses documents, web pages, and workflow outputs into structured knowledge for precise retrieval and citation during work:

* **Multi-format document import**: PDF, Word (.docx/.doc), Excel (.xlsx/.xls/.csv), PowerPoint (.pptx/.ppt), Markdown, plain text — batch directories or single files; native Office parsing (no local Word install required)
* **In-document image assets**: Embedded images are extracted on import, described via OCR/vision, and searchable with `knowledge_image_search`
* **Web knowledge collection**: URL fetch, batch URLs, domain allow/block policies, link discovery
* **Three-layer knowledge structure**: Raw documents → DocumentNode hierarchy → Cards → Fact triples
* **LLM distillation**: Optional post-import extraction of entities, topics, tags, cards, and facts
* **Full-text + semantic search**: SQLite FTS5 + vector embeddings, with source/domain/label/quality filters
* **Source & fact graphs**: Topic links (path/neighborhood) and entity-relation triples (profiles/queries)
* **Context Pack**: Automatic knowledge packing into the LLM context while working
* **Version & quality**: Refresh snapshots, quality grades, sensitive scan, duplicate suppression, maintenance plans
* **Labels & migration**: Manual/auto labels; JSONL snapshot export/import; Hub share tools (`knowledge_share_to_hub` / `knowledge_import_hub_share`)
* **Multi-tenant isolation**: Per-owner/tenant isolation in server deployments

**Relationship with Long-Term Memory**: The knowledge base is the "external brain" — storing documents and web knowledge you actively import. Long-term memory is the "internal brain" — automatically remembering preferences, habits, and project progress from conversations. Both work together; the Agent searches both systems when answering questions.

### Long-Term Memory — It Remembers Everything

MaClaw has a persistent memory system that remembers your preferences, project knowledge, and work habits across sessions:

* **Semantic retrieval**: BM25 + vector dual indexing — find past memories with natural language
* **Full-text session search**: SQLite FTS5-powered full-text index over all historical conversations — every conversation is automatically persisted and indexed, supporting cross-session search with BM25 ranking and keyword highlighting, so you can trace back to any past exchange at any time
* **Automatic capture**: Workflow outputs (requirements docs, design specs, task lists) are automatically saved as long-term memory, surviving conversation truncation
* **Knowledge graph**: Related memories are automatically linked into a structured network
* **Memory lifecycle**: Pin, archive, compress, and garbage collect — automatic quality management
* **Multi-tenant isolation**: Per-user memory isolation in server deployments

### Expert System — Role-Constrained Sessions

The desktop app ships switchable **Experts** (`gui/expert_*.go`): role prompts plus capability tiers that constrain tools/skills per session:

* **Built-in experts**: Paper polish, academic translation, PPT maker (resettable to factory definitions)
* **Capability tiers**: `full` / `advisor` / `docs` / `office` / `custom` — limits tools and skill risk levels
* **Custom experts**: Create, edit, import/export expert packages; session-level policy overrides
* **Optimize & Hub**: Expert definition optimization; Hub expert store integration (built-ins are not pushed to Hub by default)

### Skill System — Infinitely Extensible

Install skills to give MaClaw new capabilities, like installing apps on a phone:

* **Multiple formats**: YAML definitions, Markdown scripts, Claude SKILL.md format
* **Multi-step workflows**: Sequential execution, conditional branches, variable passing, output capture
* **Three marketplaces**: Search and install from SkillHub (official), ClawHub (community), and GitHub
* **Cross-platform**: Automatic path normalization and shell adaptation for Windows / macOS / Linux
* **Self-evolution**: `craft_tool` dynamically generates one-off automation scripts; validated scripts can become reusable skills
* **Skill maintenance plans**: Read-only maintenance plans with approved execution (mark for review, archive stale skills, queue repairs)

### MCP Integration — Connect to the Outside World

Extend capabilities through Model Context Protocol (MCP):

* **Dynamic discovery**: Automatically discover tools provided by MCP Servers
* **Local + remote**: Stdio local protocol and HTTP remote protocol
* **Health monitoring**: Automatic MCP Server status detection
* **Unlimited extension**: Any MCP-compatible service becomes a MaClaw capability

### Tool Routing — Smart Tool Selection

MaClaw has 40+ built-in tools and uses hybrid retrieval to select the best tool combination for each task:

* **Hybrid retrieval**: BM25 + vector semantic dual matching
* **Conditional activation**: SSH, browser, and other tools activate on-demand by context keywords
* **Progressive disclosure**: Core tools always available; low-frequency tools loaded via `discover_tool` on demand
* **Outcome learning**: Success/failure/retry records feed back into routing decisions — high-failure tools are automatically deprioritized

### Self-Evolution — Automatic Capability Gap Filling

MaClaw doesn't just execute passively — it proactively discovers its own capability gaps and fills them:

* **Capability gap detection**: When the Agent can't complete a task, it automatically searches SkillHub for matching skills and installs them
* **Skill self-repair**: After a skill execution failure, the LLM analyzes the error and patches the skill definition (steps, parameters, paths), persisting the fix
* **Nudge system**: After completing complex tasks, the system suggests packaging successful operation sequences into reusable skills, driving organic growth of the skill library
* **craft_tool conversion**: One-off automation scripts can be converted into permanent skills after validation
* **Experience-learning governance**: `experience_learning` inspects memory maintenance, routing self-evolution, tool recovery, and A2A discussion traces in a non-executing / draft style

### Office Document Processing

Unified `office` tool (native parsers, no Python or local Office required) plus dedicated delivery tools:

* **Read documents**: `read_document` auto-detects PDF / Word / Excel / PPT; format-specific readers also available
* **Excel read/write**: Table IO with default row limits and pagination
* **PPT**: Read `.pptx`; Presentation workflow and PPT Maker expert generate real `.pptx` via pptx skills
* **PDF generation**: `generate_pdf` / `office generate_pdf` renders Markdown to PDF and delivers it
* **File delivery**: `send_file` / `send_to_im` / `open` for desktop preview, IM delivery, or OS default apps

### Information Retrieval

* **Web search**: `web_search` returns titles, URLs, snippets; falls back to page scraping and real-browser search when APIs fail
* **Web fetch**: `web_fetch` extracts main content with encoding detection (GBK/UTF-8, etc.), optional JS rendering, long-page `offset` continuation
* **File download**: `download_file` saves HTTP(S) assets into the working directory; built-in anti-bot upgrade chain (headers → TLS fingerprint → browser session cookies; optional `via_browser`)
* **Screenshot**: Capture desktop (optional display index) and send to users for remote IM supervision

### Voice: ASR Transcription + TTS Playback

* **Local ASR (SenseVoice)**: Default model `sensevoice-small-q8.gguf` (auto-downloaded under `~/.maclaw/models`); transcribes IM voice and local audio; 16 kHz mono WAV recommended
* **Speaker diarization**: Multi-speaker segments labeled when enabled; `known_speakers` improves accuracy
* **Meeting minutes**: `asr` with `for_minutes=true` uses map-reduce draft; desktop `record_audio` opens a long-form recording UI
* **Local TTS (Kokoro)**: `tts` synthesizes speech; IM voice bubbles or desktop playback; multiple Chinese/English voices; idle auto-unload

### Scheduled Tasks

Create automated tasks that run on a schedule:

* Daily/weekly/monthly scheduling with one-time task support
* Natural language task descriptions, auto-executed at the scheduled time
* Pause, resume, run-now, delete; optional IM delivery targets (including Lansenger group resolution)

### Long-Running Goals

The `goal` tool manages persistent goals: once created, the system keeps advancing within budget until complete or failed. Create only when the user explicitly requests a goal.

### Audit Logging

Full operation recording for compliance review:

* Tool calls, file operations, SSH commands, and other key operations are automatically logged
* Query via `query_audit_log` by time range, tool name, and risk level

### Intent Understanding

Three-layer fused intent classification system for accurate request understanding:

* **Layer 1**: Keyword rules (<1ms)
* **Layer 2**: BM25 semantic retrieval (<5ms)
* **Layer 3**: LLM multi-turn dialogue confirmation (10-30s)
* Automatic routing to the matching workflow template or direct execution; some templates (e.g. maintenance) are SemanticOnly and activate only via IUM LLM classification

### Behavior Customization — Steering Rules

Declare behavior rules in Markdown files to customize how MaClaw works, without code changes:

* **Four injection modes**: Always, file-match, keyword-match, manual reference
* **Two scopes**: User-level (`~/.maclaw/steering/`) and project-level (`<project>/.maclaw/steering/`)
* **Token budgeting**: Intelligent control of rule injection volume
* **Hot reload**: Changes take effect within 30 seconds

## Multi-Modal Interaction

MaClaw isn't limited to one interface — collaborate through multiple entry points:

| Mode | Description |
|------|-------------|
| **Desktop AI Assistant Panel** | Native GUI with streaming output, workflow previews, expert switching, knowledge and settings |
| **Terminal TUI** | Command-line interface with chat, memory viewer, skill management, scrollbar, and streaming |
| **WeChat / Feishu / QQ / Telegram / Lansenger** | Local or Hub-bound IM; Lansenger supports group policy, @-gating, group knowledge sources, and file send |
| **Hub PWA / Mobile** | Self-hosted Hub `/app` PWA for cross-device sessions and remote collaboration |
| **VS Code ACP** | Built-in ACP host and VS Code extension assets for the coding workbench |
| **Third-party device gateway** | Local HTTP gateway (default `127.0.0.1:18777`) for ESP32 and custom hardware (voice pairing, message poll) |
| **REST API (MaClawSrv)** | Multi-tenant REST service for external programs, automation platforms, and control planes |

## Execution Capabilities

### SSH Remote Management

Built-in SSH client for managing remote servers directly from conversation:

* Up to 10 concurrent sessions with password/key/agent authentication
* Sync execution, background tasks, file upload/download
* Automatic sudo token management, shell responsiveness detection, auto-cleanup on consecutive failures
* Full operation audit logging

### Browser Automation

Chrome DevTools Protocol-based browser interaction:

* Page navigation, element clicking, text input, content extraction, screenshots
* Flow recording and replay with scheduled triggers and parameterized variables
* OCR integration (built-in native PP-OCRv6 engine — models auto-downloaded, no Python required — plus LLM vision models). Model tier can be picked directly in the OCR settings panel, or configured via `ocr_model_tier` in config.json: `tiny` (fastest) / `small` (default, balanced) / `medium` (most accurate); `ocr_enabled` toggles the OCR tool

### Desktop GUI Automation & Computer Use

Control native desktop applications without a browser. Two complementary tool surfaces exist in code:

**GUI automation (`gui_*`)**

* **Accessibility element tree**: Cross-platform window control structure
* **Flow record/replay**: `gui_record_*` / `gui_replay` with parameterization and async execution
* **Observe & verify**: `gui_observe` (element tree + OCR text), `gui_verify`, multi-monitor screenshots

**Computer Use (`computer_*`, settings toggle)**

* **Text-primary observation**: `computer_observe` / `computer_find` combine Accessibility, OmniParser (YOLO), and local PP-OCR for non-multimodal models
* **Action primitives**: `computer_click` / `computer_type` / `computer_key` / `computer_scroll` / `computer_wait` / `computer_focus` / `computer_done`
* **Playbook**: `computer_playbook` freezes successful sequences into reusable playbooks
* **Target-app policy & logs**: Restrict operable apps; automatic Computer Use log pruning

### Software Development

Programming is one of MaClaw's work capabilities, via coding workflows, pure coding tasks, and external coding tools:

* **Structured process**: Requirements → Design → Task Breakdown → Execution → Verification (`coding` workflow); lighter `maintenance` three-phase path
* **Local / remote coding**: Requirements phase can target a local directory or SSH remote workdir (sensitive password fields scrubbed after use)
* **Multi-tool support**: Claude Code, Codex, OpenCode, CodeBuddy, iFlow, Kilo (`active_tool` config)
* **Coding SubAgent / workbench**: Clean-context executors; sticky `.coding_workbench.json` state; ACP bridge to VS Code
* **Parallel & Swarm**: `parallel_execute` for batched concurrent coding tasks; large work can split across multiple AI developers
* **Remote session tools**: `list_sessions` / `send_input` / `get_session_output` / `interrupt_session` / `kill_session`, etc.

### Local Background Process Management

Launch local background tasks via `bash(background=true)` with automatic PID and log capture:

* Non-blocking status queries, blocking wait, task termination
* Symmetric Submit / Check / Wait / Kill pattern matching SSH background tasks

### Passthrough Tasks — One-Click Execution for Emergencies

Passthrough Tasks are pre-registered commands for **emergency ops, rescue, and rapid actions**. They bypass Agent intent understanding and workflow orchestration.

* **Pre-registered commands**: Script/executable + argument templates (`${param}`); runtimes include auto/powershell/cmd/bash/python/node/direct
* **Parameters & confirmation**: Required/optional/defaults; high-risk defaults to `confirm_required=true`
* **Timeout & audit**: Default 120s timeout; every run audited; enable/disable without deleting definitions
* **Entry split**:
  * **Execute**: Desktop/IM slash command `/run <name> ...` (remote runs often need `--confirm`)
  * **Registry management**: `/runctl` and Agent tool `passthrough_task` (list/save/delete/export — **does not execute scripts**)
* **Cross-device**: IM can trigger tasks registered on the desktop machine

**Typical use cases**:

| Scenario | Example Command |
|----------|----------------|
| Emergency server restart | `/run restart-nginx --confirm` |
| Database backup | `/run backup-db --target=production --confirm` |
| Deployment rollback | `/run rollback --version=v2.3.1 --confirm` |
| Disk space cleanup | `/run cleanup-logs --days=7 --confirm` |
| Export registry command to IM | `/runctl export backup-db` |

## Hub Remote Collaboration & Multi-Agent

[Hub](hub/) is a self-hosted remote control and collaboration service (desktop registers and connects):

* **Identity & devices**: Hub-local identity, session summaries/event reporting
* **IM routing**: Multi-channel coordination, device gateway, capability discovery
* **Digital assets & knowledge share**: Knowledge host, share import, backup merge
* **Experts & discussion**: Hub expert store; **A2A group discussion** (`corelib/a2a` + desktop `group_discussion`) — multi-agent consultation, invites, majority/unanimous decision policies
* **Workflow approval & market**: Hub workflow runtime, confirmation/approval, market listing
* **PWA**: `/app` mobile/browser entry; backup/restore CLI for disaster recovery
* **HA**: Multiple Hub Center base URLs (see `docs/hubcenter-ha-3nodes.md`)

Desktop agents can also use `switch_llm_provider`, `set_nickname` (Hub group nicknames), and `im_message` to push text/files to IM targets.

## Hardware Companion & Third-Party Device Gateway

* **Third-party gateway**: When enabled on desktop, listens on a local port (default `127.0.0.1:18777`) for device pairing (including voice pairing with SenseVoice digit extraction), message polling, and media exchange for IoT / custom clients
* **ClawMate / ESP32**: [ClawMateMaker](ClawMateMaker/) is the ESP32-S3 firmware flasher/diagnostics tool; [esp32s3-maclaw-client](esp32s3-maclaw-client/) and [iot-agentos](iot-agentos/) cover device firmware and AgentOS pieces
* **Protocol constraints**: Flasher accepts only signed release packages and auto-matches nonce-bound protocol:2 runtime identity (see ClawMateMaker docs)

## Quick Start

### Four Steps to Get Started

| Step | What | Details |
|------|------|---------|
| **Register** | Email signup | Unlocks remote collaboration |
| **Choose mode** | Pro / Simple | Switch anytime in settings |
| **Configure AI** | Select LLM provider | Enter API Key and test connectivity; OAuth and free trial also available |
| **Bind IM (optional)** | Scan QR for WeChat, etc. | Chat with MaClaw from your phone anytime |

### Run

* Windows: `MaClaw.exe`
* macOS: `MaClaw.app`
* Linux: `MaClaw.AppImage`
* Terminal: `maclaw-tui`

First launch auto-detects the environment and installs missing components.

## MaClawSrv — REST Agent Service

[MaClawSrv](MaClawSrv/) is MaClaw's multi-tenant REST service entrypoint, exposing Agent capabilities as standard HTTP APIs for external programs.

**Key Features**:

* **Multi-tenant isolation**: Data isolated at `tenant → user` level; multiple instances per user
* **Shared user data**: All instances share config, memory, skills, MCP state
* **Security first**: Dual-layer auth (admin + user); scrypt credential storage; TLS support
* **Full API coverage**: Admin control plane, user config, Instance/Session/Message/Run runtime, Skill/MCP lifecycle, async Jobs, Usage/Audit/Dashboard, knowledge APIs, and more

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

## MaClawDataSrv — Structured Data Service

[datasrv](datasrv/) is a separately buildable module (`maclaw-data-srv`) implementing the enterprise MIS structured-data backend: SQLite store, business actions and governance, HTTP API, OpenAPI, and embedded Web Console. Default listen address is `127.0.0.1:18180`; the Windows installer can register it as a system service. See [datasrv/README.md](datasrv/README.md).

## License (Dual License)

* **Open source use**: Free to use in open source projects
* **Commercial use**: Contact us to obtain commercial authorization free of charge — **znsoft@163.com**

## About

* **Author**: Dr. Daniel
* **GitHub**: [RapidAI/MaClaw](https://github.com/rapidai/maclaw)
* **Website**: [maclaw.top](https://maclaw.top)

---
*This tool is intended as a configuration management aid. Please ensure you comply with the service terms of each model provider.*
