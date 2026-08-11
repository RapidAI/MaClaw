# MaClaw User Manual

[FAQ](faq_en.md)

Welcome to **MaClaw** — your **Programming Little Crayfish (编程小龙虾)**. Whether you're a professional software developer or an everyday office worker, it's the most caring AI partner by your side. It doesn't just understand code — it understands *you*: customizable personality, long-term memory of your preferences, spiritual companionship while you work, and autonomous task handling when you're away. It integrates three-state programming, agent orchestration, personality system, memory management, and SSH/browser monitoring — equally at home in software development, office productivity, and daily tasks.

Here is the detailed operation guide:

## 1. First Launch & Onboarding (4 Steps)

When you launch MaClaw for the first time, a guided wizard walks you through setup in just four steps:

### Step 1: Register
*   Enter your email address to register (invitation code required in some regions)
*   Registration unlocks remote programming capabilities

### Step 2: Choose Mode
*   **Professional Mode (Pro)**: For developers — full access to all advanced features (three-state programming, Swarm orchestration, SSH/browser monitoring, MCP management, memory system, etc.)
*   **Simplified Mode (Simple)**: For office workers & everyday users — streamlined zero-barrier interface focused on conversational AI. Write documents, build reports, look up info with ease
*   Switch between modes at any time with one click — **developers use Pro mode to code, office workers use Simple mode to get things done. Everyone's covered.**

### Step 3: Configure AI Model
*   Choose from preset LLM providers (e.g., GLM, DeepSeek, etc.)
*   Enter your API Key and test connectivity
*   Also supports OAuth one-click login and free trial access

### Step 4: Bind WeChat (Optional)
*   Scan the QR code to bind your little crayfish to your WeChat
*   Once bound, chat with your little crayfish anytime via WeChat
*   Send messages from your phone to command it to write code, look up information, or process documents

> Four steps and you're done — use the desktop interface on your PC, or command it via WeChat on your phone. Always online, always available.

## 2. Startup and Environment Check

When MaClaw starts, it automatically checks your system environment:
*   **Dependency Check**: Detects Node.js and other required runtimes.
*   **Tool Installation**: Automatically detects and installs or updates `claude-code`, `codex`, `opencode`, `codebuddy`, and `qodercli` to their latest versions.
*   **Startup Window**: A progress window displays the environment preparation status.

## 2. Three-State Programming Model

MaClaw provides three complementary programming modes for different development scenarios:

### 2.1 Local AI Programming
AI CLI tools run directly on your machine with full filesystem and dev environment access.

1.  Select the target AI tool from the sidebar (Claude, Codex, OpenCode, etc.).
2.  Select a project in the "Vibe Coding" area.
3.  Click **"Launch"** — a pre-configured terminal window opens and runs automatically.

**Use Cases**: Daily coding, debugging, refactoring, code review.

### 2.2 Remote AI Programming
Browser-based remote access via MaClaw Hub with PWA and mobile support.

1.  Ensure MaClaw Hub is deployed and accessible.
2.  Open the Hub URL in your browser and activate with email and invitation code.
3.  Select a session and connect for real-time remote programming.

**Use Cases**: Remote work, cross-device collaboration, mobile emergency handling.

### 2.3 AI-Orchestrated Auto Programming
The Swarm orchestrator intelligently splits tasks, with multi-agent parallel execution and automatic merging.

1.  Select an operating mode in the orchestration panel:
    *   **Greenfield**: Build a project from scratch
    *   **Maintenance**: Incremental enhancement of existing codebase
2.  Enter requirements description — the orchestrator automatically splits into sub-tasks.
3.  Up to 5 AI developer agents execute concurrently with conflict detection and auto-merge.
4.  Up to 5 feedback iterations per task for continuous quality improvement.

**Use Cases**: Large feature development, bulk refactoring, automated pipelines.

## 3. Agent System

### 3.1 Personality Configuration — Raise Your Programming Little Crayfish
Customize your little crayfish's personality and identity to make it your own exclusive programming companion:

1.  Open the **MaclawRolePanel** (Personality Configuration Panel).
2.  Set the following parameters:
    *   **Role Name**: Give your little crayfish a name (default: "MaClaw")
    *   **Role Description**: Detailed character definition (default: "A dedicated and capable software development butler")
3.  Role information is automatically injected into system prompts, affecting all interactions and keeping your little crayfish's personality consistent.
4.  Support for dynamic temporary role override during conversations for different scenarios (serious code review / relaxed pair programming).
5.  Through long coding nights, your little crayfish is always online — responsive, patient, and ever-present.

### 3.2 Background Task Management
The system manages concurrent tasks through a slot-based mechanism:

| Slot Type | Max Concurrency | Description |
|-----------|----------------|-------------|
| Coding | 1 | Interactive programming sessions |
| Scheduled | 1 | Planned automatic execution |
| Auto | 1 | ClawNet auto task pickup |
| SSH | 10 | Remote terminal connections |
| Browser | 2 | Browser automation |

Background tasks support real-time progress monitoring. Long-running tasks can be checked at any time.

## 4. Memory Management System — Your Little Crayfish Remembers Everything

MaClaw provides long-term memory capabilities, enabling your little crayfish to remember your preferences and project knowledge across sessions — the more you use it, the better it knows you:

### 4.1 Memory Categories
The system maintains six types of structured memory:

| Category | Description | Weight |
|----------|-------------|--------|
| Self-Identity | Agent identity and personality definition | 4.0 |
| User Facts | Basic information about the user | 2.0 |
| Instructions | Explicit rules given by the user | 3.0 |
| Preferences | Coding style, tool preferences, etc. | 2.0 |
| Project Knowledge | Project-specific technical decisions and architecture | 2.0 |
| Session Checkpoints | Cross-session context continuation | 2.0 |

### 4.2 Memory Operations
*   **Pin**: Mark important memories as non-evictable
*   **Archive**: Move infrequently used memories to cold storage
*   **Compress**: LLM-driven deduplication + semantic merging + intelligent compression
*   **Garbage Collection (GC)**: LRU-based automatic eviction of stale memories (self-identity memories are never evicted)

### 4.3 Memory Retrieval
Three-dimensional scoring model: `Score = w1 × Recency + w2 × Importance + w3 × Relevance`

*   **Recency**: Exponential decay based on update time
*   **Importance**: Category weight + log of access count
*   **Relevance**: BM25 keyword matching + vector semantic similarity + project affinity

## 5. SSH Monitoring & Control (Differentiated Capability)

MaClaw's unique SSH monitoring system provides end-to-end remote management for software operations — a core differentiator from competing products:

### 5.1 Remote Session Management
1.  Add remote host information in the SSH panel (address, port, authentication method).
2.  After connecting, sessions are managed as background tasks (up to 10 concurrent).
3.  Real-time monitoring of remote operations and process status.

### 5.2 Ops Automation
*   Agent-driven remote deployment, log analysis, and fault diagnosis
*   Batch command execution with result collection
*   Full audit logging of all operations

## 6. Browser Monitoring & Automation (Differentiated Capability)

MaClaw's unique browser monitoring system provides core support for automated testing and business process automation — a core differentiator from competing products:

### 6.1 Flow Recording
1.  Open the browser recording panel and click **"Start Recording"**.
2.  Perform operations in the browser — the system automatically records each interaction step.
3.  Click **"Stop Recording"** to save as a replayable flow file.

### 6.2 Flow Replay
1.  Select a recorded flow file.
2.  Configure **variable overrides** for parameterized replay.
3.  Replay tasks run as background tasks with pause/resume/cancel support.
4.  Support scheduled triggers for automatic execution.

### 6.3 OCR & Verification
*   Built-in native OCR engine (PP-OCRv6, models auto-downloaded, no Python required) and LLM vision model support for screen text extraction. The model tier can be selected directly in the OCR settings panel, or via `ocr_model_tier` in config.json: `tiny` (fastest) / `small` (default) / `medium` (most accurate)
*   Built-in task verification to confirm automation correctness
*   Automatic retry strategy on failure

## 7. Sidebar Navigation & Tool Management

### 7.1 Tool Navigation
The sidebar provides quick switching:
*   **Claude**: Configure and launch Anthropic Claude Code
*   **Codex**: Configure and launch OpenAI Codex CLI
*   **OpenCode**: Configure and launch OpenCode AI assistant
*   **CodeBuddy**: Configure and launch CodeBuddy programming assistant
*   **Qoder**: Configure and launch Qoder CLI programming assistant
*   **Skills**: Manage AI programming assistant extensions

### 7.2 Skills Management
1.  **Add Skill**: Supports **Skill ID (Address)** and **Zip Package** formats.
2.  **Shared Skills**: Zip skills stored in global repository, accessible by all tools.
3.  **Compatibility Check**: Auto-detects tool capabilities and shows warnings for incompatibilities.
4.  **System Skills**: Built-in official documentation and default skills, ready to use.

## 8. Model Settings

1.  Select an AI tool from the sidebar and locate the **"Model Settings"** area.
2.  **Provider Selection**: Supports preset providers including GLM, Kimi, Doubao, MiniMax, DeepSeek, etc.
3.  **"Original" Mode**: Use official default configs; auto-clears custom proxy settings and environment variables.
4.  **API Key**: Paste your key in the input field — it's saved automatically after configuration.
5.  **Smart Sync**: Keys for the same provider auto-sync across different tools.

## 9. Multi-Project Management

### 9.1 Switching Projects
View project tabs in the **"Vibe Coding"** area and click to switch.

### 9.2 Project Management
Click **"Manage Projects"** to add, rename, or delete projects.

### 9.3 Project Parameters
1.  **Project Directory**: Click "Change" to select your code folder.
2.  **Launch Parameters**:
    *   **Yolo Mode**: Skip all permission prompts (use with caution)
    *   **As Admin (Windows)**: Launch terminal with administrator rights
3.  **Python Environment**: Auto-detect Conda/Anaconda environments with independent Python runtime support.

## 10. Tool Routing & MCP Integration

### 10.1 Intelligent Tool Routing
*   BM25 + vector semantic dual retrieval for automatic relevant tool matching
*   Up to 30 tools per request, dynamically budgeted by relevance
*   Detailed tool routing logs at `~/.maclaw/logs/tool_route.log`

### 10.2 MCP Server Management
*   **Local MCP Servers**: Stdio protocol with auto-discovery and health monitoring
*   **Remote MCP Servers**: HTTP protocol with authentication and 60-second health check cycle
*   Automatic tool definition extraction and registration

## 11. Other Features

*   **Status Bar**: Real-time feedback and error messages at the bottom of the interface
*   **Language Switch**: Change interface language in the title bar or settings
*   **Check Update**: Get the latest version of MaClaw
*   **System Tray**: Right-click the tray icon for quick tool launching or quitting
*   **Security Framework**: Risk assessment engine, audit logging, security firewall, and fine-grained permission control
