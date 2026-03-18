# FAQ - Frequently Asked Questions

## 1. Why is the system tray icon unresponsive?
In earlier versions, if background operations (such as file I/O) blocked the main thread, the tray icon might temporarily become unresponsive. The current version has optimized this issue through asynchronous processing and OS thread locking. If you still encounter this, please try restarting the program.

## 2. How to use a Custom Model?
1. Select the AI tool (e.g., Claude) in the sidebar.
2. Click "Model Settings".
3. Select the "Custom" tab.
4. Enter your model name (e.g., `claude-3-5-sonnet-20241022`).
5. Enter an API Endpoint compatible with the protocol.
6. Enter your API Key and save.

## 3. My API Key is not working?
The preset shortcuts in MaClaw **may only support specific "Coding Plan" API Keys** provided by each vendor.
If you are using a general-purpose API Key, please use the **"Custom"** mode and manually enter the corresponding model name and API endpoint.

## 4. Where is the configuration file saved?
MaClaw's configuration is saved in your user home directory at `~/.maclaw/config.json`.
Native settings for various AI tools (like Claude's `~/.claude/settings.json`) are also automatically synced based on your configuration.

## 5. How to update AI CLI tools?
Each time MaClaw starts, it automatically checks the versions of supported tools (like `claude-code`, `codex`, `gemini-cli`). If a new version is available, it will attempt to update it for you. You can see the specific status in the startup progress window.

## 6. What if the environment check fails?
If Node.js or tool installation fails, please check your internet connection. In mainland China, the program automatically attempts to use domestic mirrors to speed up downloads. If automatic installation continues to fail, it is recommended to manually install the environment as prompted.

## 7. How to use the original model services of each tool?
Select **AICoderMirror** as the provider and enter your API Key. This provider acts as a relay for the original services, allowing easy access to native model services.

## 8. How to use the services provided directly by the tools themselves?
Select **"Original"** as the provider in the tool's settings to restore each tool to its default state. In this mode, MaClaw will automatically clear all related custom proxy configurations, environment variables, and the tool's own configuration files (e.g., `~/.claude`), allowing you to use the tool's built-in authentication and communication methods directly.

## 9. What does Yolo mode mean?
When Yolo mode is enabled, the programming tool will no longer ask for confirmation before every file or system operation, enhancing the coding experience. However, please be aware that this option is risky and carries the potential for accidental file deletion or modification. It is intended for expert users only.

## 10. Which tools does MaClaw support?
MaClaw currently supports **Claude Code**, **OpenAI Codex**, **Google Gemini CLI**, **OpenCode**, **CodeBuddy**, and **Qoder CLI**. You can quickly switch between them in the sidebar and configure each tool independently.

## 11. Why hasn't the tool's behavior changed after switching providers?
Please make sure you click the **"Launch"** button on the main interface to restart the tool after switching providers. MaClaw automatically syncs the environment based on your latest configuration before launching. If issues persist, try switching to **"Original"** mode first to clear old configurations, and then switch back to your target provider.

## 12. What is the difference between "Original" and "Qoder" providers in Qoder CLI?
*   **Original**: Uses the default authentication method of Qoder CLI, which requires **logging in via a web browser**.
*   **Qoder**: Uses a **Personal Access Token** for authentication. You can obtain a token from the Qoder website and enter it into MaClaw. This method is ideal for environments where opening a browser is not possible or for faster deployment.

## 13. How to use Chinese LLMs like DeepSeek in Gemini CLI?
You need to set up a **liteLLM proxy** service yourself. In Gemini CLI, select **"Custom"** as the provider and enter the Gemini protocol-compatible endpoint address and the corresponding API Key provided by your liteLLM service.

## 14. Which Python environments are supported?
MaClaw currently supports **Conda/Anaconda** environments. After enabling "Python Project" in the project settings, MaClaw scans for available conda environments on your system for selection. Environment switching is handled automatically upon launch.

## 15. Why use admin privileges to launch?
Some projects may involve system-level file operations or access to restricted directories. Launching with admin privileges can prevent tools from failing due to insufficient permissions. This feature is currently only supported on Windows.

## 16. Why can't I install some skills in Gemini/Codex?
Gemini and Codex currently only support skills in **Zip Package** format. If you try to install a Skill ID (Address) type skill (e.g., `@org/skill`), the system will report an incompatibility. These skills are only supported in Claude Code. Please try to obtain the Zip package version of the skill for installation.

## 17. Are skills shared across all tools?
Yes. Skills added via **Zip Package** are stored in a global repository and are automatically recognized and usable by **Claude**, **Gemini**, **Codex**, and other supported tools. You only need to add them once to use them anywhere.

---
*For more issues, please visit GitHub Issues: [RapidAI/cceasy/issues](https://github.com/RapidAI/cceasy/issues)*
