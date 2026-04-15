# MaClaw

[📖 User Manual](UserManual_EN.md) | [❓ FAQ](faq_en.md) | [English](README_EN.md) | [中文](README.md)

**MaClaw** is your **Programming Little Crayfish (编程小龙虾)** — whether you're a professional software developer or an everyday office worker, it's the most caring AI partner by your side. It doesn't just understand code — it understands *you*: customizable personality, long-term memory of your preferences, spiritual companionship while you work, and autonomous task handling when you're away. Built with Wails, Go, and React, it integrates **three-state programming, agent orchestration, personality system, long-term memory, and SSH/browser monitoring** — equally at home in **software development, office productivity, and daily tasks**.

> It's the programmer's coding partner, the office worker's productivity assistant, and everyone's intelligent companion. It stays with you when you work, and works for you when you step away.

Programming as smooth as eating a macaron.

## Three-State Programming Architecture

MaClaw pioneers a **three-state programming** model covering the full spectrum from local development to fully autonomous orchestration:

| State | Description | Use Cases |
|--------|-------------|-----------|
| **Local AI Programming** | AI CLI tools run directly on your machine with full filesystem and dev environment access | Daily coding, debugging, refactoring |
| **Remote AI Programming** | Browser-based remote access via MaClaw Hub with PWA and mobile support | Remote work, cross-device collaboration |
| **AI-Orchestrated Auto Programming** | Swarm orchestrator intelligently splits tasks, multi-agent parallel execution, automatic merging | Large features, bulk refactoring, automated pipelines |

Seamless switching between states with intelligent routing based on tool availability, network conditions, and task type.

## Dual-Mode Experience

MaClaw offers two operational modes tailored for different user groups:

| Mode | Target Users | Features |
|------|-------------|----------|
| **Professional Mode (Pro)** | Software developers | Full access to three-state programming, Swarm orchestration, SSH/browser monitoring, MCP management, memory system, and all advanced features |
| **Simplified Mode (Simple)** | Office workers & everyday users | Streamlined interface focused on conversational AI interaction, hiding technical complexity for zero-barrier onboarding — write docs, build reports, look up info with ease |

Switch between modes with one click in settings — **developers use Pro mode to code, office workers use Simple mode to get things done. Everyone's covered.**

## Core Capabilities

### Agent System

*   **Swarm Orchestrator**: Supports Greenfield (build from scratch) and Maintenance (incremental enhancement) operating modes
*   **Multi-Agent Parallelism**: Up to 5 concurrent AI developer agents with conflict detection and auto-merge
*   **Feedback Loop**: Up to 5 automatic feedback iterations per task for continuous quality improvement
*   **Background Task Manager**: Slot-based concurrency control with five task categories: coding, scheduled, auto, SSH, and browser

### Personality System — Your Programming Little Crayfish, A Warm-Hearted Code Companion

MaClaw is not just a cold tool — it's your **Programming Little Crayfish**: with its own personality, remembering your habits, understanding your style, and accompanying every coding session:

*   **Customizable Agent Identity**: Define your little crayfish's name, description, personality, and behavioral style
*   **Dynamic Role Switching**: Temporarily override roles during conversations for different scenarios (serious code review / relaxed pair programming)
*   **System Prompt Injection**: Role information automatically injected into system prompts for consistent personality and behavior
*   **Spiritual Companionship**: Through long coding nights, your little crayfish is always online — responsive, patient, and ever-present

### Memory Management — Your Little Crayfish Remembers Everything

*   **Long-Term Memory Store**: Persistent storage with BM25 + vector semantic indexing — your little crayfish remembers your preferences and project knowledge across sessions
*   **Multi-Dimensional Scoring**: Memory retrieval based on Recency, Importance, and Relevance
*   **Memory Lifecycle**: Pin, Archive, Compress, and Garbage Collection (GC) operations
*   **Six Memory Categories**: Self-identity, user facts, instructions, preferences, project knowledge, session checkpoints
*   **Knowledge Graph**: Automatic linking of related memories into a structured knowledge network

### SSH Monitoring & Control (Differentiated Capability)

MaClaw's unique SSH monitoring system provides end-to-end remote management for software operations:

*   **Remote Terminal Management**: Built-in SSH client supporting up to 10 concurrent remote sessions
*   **Real-Time Session Monitoring**: Full visualization of remote operations with process status tracking
*   **Ops Automation**: Agent-driven remote deployment, log analysis, and fault diagnosis
*   **Operation Audit**: Full remote operation logging for security compliance
*   **Batch Operations**: Batch command dispatch with result collection for improved ops efficiency

### Browser Monitoring & Automation (Differentiated Capability)

MaClaw's unique browser monitoring system provides core support for automated testing and business process automation:

*   **Flow Recording & Replay**: Chrome DevTools Protocol-based browser interaction recording and replay
*   **Background Automation**: Browser tasks run as background tasks with pause/resume/cancel support
*   **OCR Integration**: RapidOCR and LLM vision model support for screen text extraction
*   **Scheduled Replay**: Time-based automatic triggering of browser automation tasks
*   **Variable Override**: Dynamic variable substitution during replay for parameterized automated testing
*   **Intelligent Verification**: Built-in execution result verification for automatic correctness checking

### Tool Routing & MCP Integration

*   **Hybrid Retrieval Routing**: BM25 + vector semantic dual retrieval for intelligent tool matching
*   **Local MCP Servers**: Stdio protocol with auto-discovery and health monitoring
*   **Remote MCP Servers**: HTTP protocol with authentication and health checks
*   **Dynamic Tool Budget**: Up to 30 tools per request, allocated by relevance

## Base Features

*   **🚀 Automatic Environment Preparation**: Auto-detect and prepare AI CLI environments (Claude Code, Codex, Gemini, OpenCode, CodeBuddy, Qoder CLI) with auto-install and version updates
*   **🖼️ Unified Sidebar UI**: Modern vertical sidebar navigation for quick switching between AI programming tools
*   **📢 Message Center (BBS)**: Integrated real-time announcements for tool updates and community news
*   **📚 Interactive Tutorial**: Built-in beginner and advanced guides via interactive Markdown
*   **🛒 API Store**: Curated quality API providers, accessible with one click
*   **🛠️ Unified Skill Management**: Skill ID and Zip package support with smart compatibility checks and built-in system skills
*   **📂 Multi-Project Management (Vibe Coding)**: Tabbed interface, independent configs, Python environment support (Conda/Anaconda)
*   **🔄 Multi-Model & Cross-Platform**: Claude Code, OpenAI Codex, Google Gemini CLI, OpenCode, CodeBuddy, Qoder CLI; "Original" mode and smart API Key sync
*   **🖱️ System Tray**: One-click launch and quit
*   **⚡ One-Click Launch**: Large buttons to launch CLI tools with pre-configured environments

## Quick Start

### Post-Install Onboarding (4 Steps)
After launching MaClaw for the first time, a guided wizard walks you through setup:

| Step | Content | Description |
|------|---------|-------------|
| **Step 1: Register** | Email registration | Enter your email to register (invitation code required in some regions). Unlocks remote programming capabilities. |
| **Step 2: Choose Mode** | Pro / Simple | Pro mode for developers, Simple mode for office workers and everyday users. Switchable at any time. |
| **Step 3: Configure AI** | Select LLM provider | Choose from preset providers, enter API Key and test connectivity. Also supports OAuth one-click login and free trial. |
| **Step 4: Bind WeChat** | QR code scan (optional) | Scan the QR code to bind your little crayfish to your WeChat. Then chat with it anytime, anywhere via WeChat. |

> Four steps and your little crayfish is ready — use the desktop interface on your PC, or command it via WeChat on your phone. Always online, always available.

### Run the Program
Run `MaClaw.exe` (Windows) or `MaClaw.app` (macOS).

### Environment Detection
On first launch, MaClaw performs an environment self-check and auto-installs missing runtimes (e.g., Node.js).

### Choose a State and Launch
*   **Local Programming**: Select an AI tool in the sidebar and click **"Launch"**
*   **Remote Programming**: Access remotely via MaClaw Hub in your browser
*   **Auto Orchestration**: Configure Swarm orchestrator to let AI split and execute tasks automatically

## License (Dual License)

This project is released under a **Dual License** model:

- **Open Source Use**: Free to use in open source software projects without additional authorization.
- **Commercial Use**: A commercial license is required for use in commercial software or for commercial purposes.

For commercial licensing inquiries, please contact: **znsoft@163.com**

## About

*   **Version**: V5.3.0.9800
*   **Author**: Dr. Daniel
*   **GitHub**: [RapidAI/MaClaw](https://github.com/rapidai/maclaw)
*   **Website**: [maclaw.top](https://maclaw.top)
*   **Resources**: [CS146s Chinese Version](https://github.com/BIT-ENGD/cs146s_cn)

---
*This tool is intended as a configuration management aid. Please ensure you comply with the service terms of each model provider.*
