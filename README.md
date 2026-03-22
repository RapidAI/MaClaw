# MaClaw（码卡龙）

[📖 使用说明书](UserManual_CN.md) | [❓ FAQ](faq.md) | [English](README_EN.md) | [中文](README.md)

MaClaw（码卡龙）是一款基于 Wails + Go + React 开发的桌面 AI 编程辅助工具仪表盘。它旨在为多种 AI 命令行工具（Anthropic Claude Code, OpenAI Codex, Google Gemini CLI, OpenCode, CodeBuddy, Qoder CLI）提供统一的配置管理、环境自动部署以及一键启动功能。

本工具支持远程编程，随时随地可编，编程流畅就像吃马卡龙。

本程序特别针对主流 AI 编程模型进行了深度集成，支持 API Key 的快速配置与自动同步。


## 核心功能

*   **🚀 环境自动准备**：启动时自动检测并准备所需的 AI CLI 环境（Claude Code, Codex, Gemini, OpenCode, CodeBuddy, Qoder CLI），支持自动安装与版本更新。
*   **🖼️ 统一侧边栏 UI**：采用现代化的垂直侧边栏导航，支持在不同的 AI 编程工具间快速切换。
*   **📢 消息中心 (BBS)**：集成实时公告，第一时间获取工具更新动态、使用技巧及社区公告。
*   **📚 交互式教程 (Tutorial)**：内置新手引导与进阶教程，通过 Markdown 交互展示，助您快速上手各款 AI 编程神器。
*   **🛒 API 商店 (API Store)**：精选优质 API 服务商，一键直达，轻松解决模型获取难题。
*   **🛠️ 统一技能管理 (Skills)**：
    *   **全局技能仓库**：支持 **Skill ID (Address)** 与 **Zip 包** 两种格式。Zip 技能一次添加，所有工具共享。
    *   **智能兼容性检查**：自动识别工具特性，针对 Gemini/Codex 仅显示并允许安装 Zip 技能。
    *   **系统内置技能**：预置官方文档与常用工具包，开箱即用。
*   **📂 多项目管理 (Vibe Coding)**：
    *   **多标签页切换**：支持同时管理多个项目，通过顶部标签页快速切换工作上下文。
    *   **独立配置**：每个项目可独立设置工作目录和启动参数（如 Yolo 模式）。
    *   **Python 环境支持**：深度集成 Conda/Anaconda，支持为不同项目选择独立的 Python 运行环境。
*   **🔄 多模型 & 跨平台支持**：
    *   集成 **Claude Code**, **OpenAI Codex**, **Google Gemini CLI**, **OpenCode**, **CodeBuddy**, **Qoder CLI** 等主流工具。
    *   **"原厂" (Original) 模式**：支持一键切换回官方原始配置，确保官方工具的纯净运行。
    *   **智能同步**：同一服务商的 API Key 可在不同工具间自动同步，无需重复输入。
*   **🖱️ 系统托盘支持**：快速切换模型、一键启动及退出程序。
*   **⚡ 一键启动**：主界面提供大按钮一键启动对应的 CLI 工具，自动处理认证与环境配置。

## 快速开始

### 1. 运行程序
直接运行 `MaClaw.exe` (Windows) 或 `MaClaw.app` (macOS)。

### 2. 环境检测
程序首次启动会进行环境自检。如果您的电脑未安装所需的运行环境（如 Node.js），程序会尝试自动安装/更新相关组件。

### 3. 配置 API Key
在各工具的配置面板中选择服务商并输入您的 API Key。
*   **同步特性**：当您在 Claude 中设置了某服务商的 Key，Gemini 和 Codex 中相同的服务商会自动同步该 Key。
*   如果您还没有 Key，可以点击输入框旁的 **"Get Key"** 按钮跳转到对应厂商的申请页面。

### 4. 切换与启动
*   在左侧侧边栏选择您想要使用的 AI 工具（Claude, Codex 或 Gemini）。
*   **选择项目**：在 "Vibe Coding" 区域点击项目标签切换项目。
*   点击 **"Launch"**，程序会弹出一个预配置好环境的终端窗口并自动运行。

## 关于

*   **版本**：V5.1.0.9400
*   **作者**：Dr. Daniel
*   **GitHub**：[RapidAI/MaClaw](https://github.com/rapidai/maclaw)
*   **产品网站**：[maclaw.top](https://maclaw.top)
*   **资源**：[CS146s 中文版](https://github.com/BIT-ENGD/cs146s_cn)

---
*本工具仅作为配置管理辅助，请确保遵守各模型厂商的服务条款。*
