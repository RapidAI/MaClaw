# MaClaw ACP Bridge — Production Closed Loop

把 **Agent Client Protocol (ACP)** 客户端（VS Code 等）接到 **正在运行的 MaClaw GUI**。

> 工业 ACP（[agentclientprotocol.com](https://agentclientprotocol.com/)），**不是** iFlow 私有 ACP WebSocket。

---

## 产品决策（已锁定）

| 决策 | 结论 |
|------|------|
| **唯一 agent 大脑** | **MaClaw GUI** 桌面 AI 助手（`RunAIAssistantProgrammingPrompt` → 同一 IM handler / 工具 / 模型配置） |
| **`maclaw-acp-bridge`** | **薄协议适配器**（stdio ACP ↔ Mode B TCP 或 Gateway），**禁止**再实现第二套 RunLoop/工具 |
| **禁止** | 再做一个 TUI 式独立 agent 栈（`cmd/maclaw-acp` 完整 RunLoop 不作为主路径） |
| **改 GUI 逻辑** | ACP / VS Code **立即受益**（同进程、同 handler） |
| **VS Code 体验目标** | 在编辑器里对话；**文件改动落在 `session/new.cwd` 工作区磁盘**，VS Code 直接打开/使用 |

```text
VS Code (ACP Client)
    │  stdio NDJSON
    ▼
maclaw-acp-bridge          ← 仅协议转发，无业务大脑
    │  Mode B（优先）
    ▼
MaClaw.exe Mode B host
    │  RunAIAssistantProgrammingPrompt
    │  userID = desktop-user:{cwd}
    │  tools cwd = EffectiveWorkingDirForOwner(userID) ≈ VS Code 打开的文件夹
    ▼
同一套 GUI AI 助手 / 工具 / LLM
```

**用户感知**：VS Code 是主界面；GUI 可最小化/托盘运行。  
**工程感知**：只维护 GUI 侧助手逻辑；bridge 尽量不动。

---

## 产品语义（Mode B 优先）

**期望**：VS Code 里的编程 agent = **现有 MaClaw GUI AI 助手**  
（同一套 LLM、工具、项目会话、`project_path` / 工作区），而不是另起无头 agent。

```text
Mode B（首选）
  VS Code → maclaw-acp-bridge → GUI loopback ACP
           → RunAIAssistantProgrammingPrompt
           → desktop AI assistant (cwd = session project)

Mode Gateway（回退）
  VS Code → bridge → IM Gateway :18777 → third-party IM handler
```

### 工作区闭环（结果在 VS Code 可用）

1. `session/new.cwd` → 规范化后写入 session，并作为 `project_path`  
2. `userID = desktop-user:{cwd}` → 工具/系统提示通过 `EffectiveWorkingDirForOwner` 使用该目录  
3. 回合正文带 **ACP programming workspace** 契约（写盘、列出变更路径）  
4. 助手文字经 `session/update` `agent_message_chunk` 回 VS Code（非流式也会 flush）  
5. 工具写出的文件在 **磁盘工作区** → VS Code 资源管理器/已打开文件可见（编辑器刷新）

GUI 镜像（工程 Tab 气泡）为 **可选旁路**（`acp_host_mirror_ui`），不是主交付面。

---

## 生产级闭环（当前实现）

两条独立入口（共用 Mode B / Gateway / bridge 准备步骤）：

```text
前置检查（两条链路共用）
    └─ 检测 VS Code CLI；未安装则立即返回 needVSCodeInstall，
       前端弹确认框，用户确认后打开 https://code.visualstudio.com/Download

实用工具「启动 VS Code」（第三方客户端，原有能力）
    ├─ 启动 Mode B ACP Host（GUI AI 助手 = 唯一大脑）
    ├─ 开启 Third-party Gateway（回退）
    ├─ 安装/定位 maclaw-acp-bridge（薄桥）
    ├─ 安装 formulahendry.acp-client（尽力）
    ├─ 写入 settings.json → acp.agents["MaClaw GUI"]
    └─ 启动 VS Code

实用工具「启动 VS Code（扩展）」（一方扩展，推荐）
    ├─（同上准备 Mode B / Gateway / bridge）
    ├─ 安装一方扩展 maclaw.maclaw-acp（嵌入 VSIX，离线静默；
    │   聊天默认在底部面板，不挡文件树）
    └─ 启动 VS Code
```

### 一方 VS Code 扩展（Mode C）

- 源码：`vscode-ext/`（TypeScript，esbuild 打包，`@vscode/vsce` 生成 VSIX）。
- 分发：VSIX 随仓库提交在 `gui/vscode_ext_asset/`，GUI 编译时 `go:embed`
  （`gui/vscode_acp_ext_asset.go`）；启动器解压到 `<MaclawBaseDir>/bin/vsix/` 后
  `code --install-extension <vsix> --force`。
- 升级：比对 `code --list-extensions --show-versions` 与嵌入版本，不一致才重装。
- UI：聊天 webview 贡献到 `viewsContainers.panel`（底部面板），可拖到右侧副侧边栏；
  `retainContextWhenHidden` + 事件 transcript 回放，拖动/隐藏不丢对话显示。
- 连接：扩展直接 spawn `maclaw-acp-bridge`（设置 `maclaw-acp.bridgePath` 可覆盖），
  不依赖 `acp.agents` 配置；与第三方客户端可共存。
- 重新构建 VSIX：`cd vscode-ext && npm install && npm run package`
  （或 `vscode-ext/build-vsix.ps1`，`build_win.bat` 发版时 best-effort 调用）。
- Go 入口：`LaunchVSCodeWithACPExtension` / `PrepareVSCodeACPExtension`
  （第三方链路仍为 `LaunchVSCodeWithACP` / `PrepareVSCodeACP`）。

### 远程编程附着（VS Code Phase 1）

扩展侧栏可附着已有 `remote_coding_dev` 任务（大脑仍是 GUI sticky remote）：

| ACP 方法（Mode B） | 作用 |
|--------------------|------|
| `maclaw/list_remote_coding_tasks` | 列出远程编程任务 + armed/needs_reconnect |
| `maclaw/get_coding_workbench_status` | 单任务 workbench 状态 |
| `maclaw/ensure_coding_workbench_armed` | 无密码 re-arm（SSH 仍存活时） |
| `maclaw/prepare_remote_coding` | 密码连接并 arm（密码不落盘） |
| `maclaw/read_remote_file` | 经 sticky SSH 只读预览远端文本（限 work_dir 内） |
| `maclaw/list_remote_dir` | 远端目录 `ls -la`（限 work_dir 内） |
| `maclaw/search_remote` | 远端内容搜索（rg 优先，否则 grep；限 work_dir） |

附着时：`session/new.cwd` = 本地 task path → `userID=desktop-user:{path}` → sticky remote → `RemoteCodingSubAgent`。  
文件改在远端；VS Code 可通过 `maclaw-remote://` 虚拟文档预览远端源码（只读）。  
回合成功后自动刷新已打开的远端预览；侧栏可一键 **Remote-SSH 打开** work_dir（需安装 `ms-vscode-remote.remote-ssh`）。  
**远端 ls** 为可点选 QuickPick（进目录 / 打开预览）；**远端↔本地** 用 `vscode.diff` 对比 work_dir 相对路径对应的本地文件。  
**远端搜索** 结果树可导出 MD/JSON；打开命中文件时高亮行并支持 F4 下一条；可复制远端路径 / 打开最近预览。  
侧栏 **Remote Explorer** 懒加载 work_dir 目录树（默认隐藏点文件、可名称过滤）；**Agent Changes** 汇总本会话 agent 写入/删除的文件（可导出 MD/JSON；回合结束可一键打开）。  
聊天 **File change** 卡片与 tool 路径芯片支持一键打开远端预览 / Diff 本地。

### Mode B 发现

```text
<MaclawBaseDir>/acp/endpoint.json   # host, port, pid, protocol=acp-ndjson-tcp
<MaclawBaseDir>/acp/token           # owner-only
```

PID 失效时清理发现文件，避免连僵尸进程。

### 配置（设置 → 编程工具）

| 字段 | 默认 | 含义 |
|------|------|------|
| `acp_host_enabled` | true | 启用 Mode B |
| `acp_host_port` | 0 | **0**：优先 18789，失败再随机；**>0**：严格绑定 |
| `acp_host_mirror_ui` | true | 镜像到 GUI 工程 Tab（旁路） |

### GUI 镜像（可选）

1. `acp-mode-b-message` → 打开/激活 `desktop-user:<cwd>` 工程 tab  
2. `ai-assistant-foreground-round-started` → 占位气泡  
3. token / progress / response → 同一 `request_id`  
4. 用户前缀：`〔VS Code / ACP · <项目名>〕…`

### Cancel

`session/prompt` 异步；`session/cancel` 取消 GUI 会话与 in-flight prompt。

---

## 构建与发版

```bash
go build -o dist/maclaw-acp-bridge.exe ./cmd/maclaw-acp-bridge
# Windows：build_win.bat / rebuild_windows_gui.ps1 一并产出 bridge
```

## 使用

1. 启动 **MaClaw GUI**（大脑）  
2. 实用工具 → **启动 VS Code**，或手动配置 bridge  
3. VS Code 打开**工程文件夹**（cwd）  
4. ACP 选 **MaClaw GUI**，发编程指令  
5. 看 VS Code 聊天回复 + 工作区文件变化  

```bash
dist\maclaw-acp-bridge.exe --doctor   # modeB.dial: ok
```

## 非目标（避免双维护）

- 新建完整 `cmd/maclaw-acp` RunLoop / 第二套工具注册（TUI 分叉模式）  
- 在 bridge 内实现业务工具、模型路由、会话记忆  
- 以 GUI 气泡为唯一结果出口（VS Code 必须能收到文字；文件以磁盘为准）

## Cursor 向体验（仍单大脑）

| 能力 | 实现 |
|------|------|
| 聊天文字 | `agent_message_chunk`（含非流式 flush） |
| **工具 chips UI** | GUI 工具 start/end → `tool_call` / `tool_call_update`（title/kind/locations/status） |
| 文件结果 | 工具写盘到 `cwd`；回复附 `[workspace files]`；locations 带 path |
| Diff 面板 | 依赖 VS Code ACP 扩展能力；formulahendry 当前以 **tool chip + 路径** 为主，完整 inline diff 需扩展支持 |
| **权限** | `session/request_permission`；工作区内写文件自动允许；`bash`/`delete` 需确认 |
| **Allow always** | 按 **ACP sessionId + 工具名** 记忆到 TCP 连接结束（选 Allow always 后同会话不再弹） |
| **Diff 卡片** | 写文件前快照 → 写后生成 `### File change` + ````diff` 块：挂在 `tool_call_update.content` 并再推一条 `agent_message_chunk`（聊天可见） |
| **完整 inline diff 面板** | 仍取决于 VS Code ACP 扩展；当前 formulahendry 以 chip + 文本 diff 为主 |

**禁止**在 bridge 内实现工具执行或 diff 引擎。

### 权限数据流

```text
GUI 工具执行前
  → 若 session 已 allow_always[tool] → 直接放行
  → 否则 session/request_permission (host → TCP → bridge → VS Code QuickPick)
  → allow_once | allow_always | reject
  → allow_always 写入 host session 内存表
```

### Diff 卡片数据流

```text
tool start (write_file/edit_file)
  → 读取磁盘旧内容快照 (request_id + abs path)
tool end (ok)
  → unified 行 diff → markdown
  → tool_call_update.content + agent_message_chunk
```
