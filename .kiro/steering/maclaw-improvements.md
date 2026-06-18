# MacLaw 改进记录

## 已修复的问题

### 1. Windows 编码问题（GBK/CP936 乱码）

**根因**：Windows 中文系统默认代码页是 GBK (CP936)，bash 工具通过 PowerShell/cmd 执行命令时，子进程的 stdout 输出使用 GBK 编码，Go 读取后当作 UTF-8 处理导致乱码。Python 脚本如果不指定 encoding='utf-8'，open() 也会用 GBK 写文件。

**修复**：
- `corelib/tool/craft.go`：新增 `AppendUTF8Env()` 函数，为所有子进程注入 `PYTHONIOENCODING=utf-8` 和 `PYTHONUTF8=1` 环境变量
- `gui/im_tools_local.go`：bash 工具在 Windows 上自动添加 `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8` 前缀
- `tui/agent_handler.go`：bash 工具从 `cmd /c` 改为 PowerShell，并添加 UTF-8 OutputEncoding

**注意**：`write_file` 工具本身没有编码问题——Go 的 `os.WriteFile(path, []byte(content))` 直接写入 UTF-8 字节。

### 2. 系统 Prompt 编码指导

在 GUI 和 TUI 的系统 prompt 中添加了明确的编码说明，防止 LLM 误判编码问题后陷入无效循环：
- write_file 始终 UTF-8，直接写中文即可
- bash 已自动设置 UTF-8 环境变量
- Python 脚本写文件时始终指定 encoding='utf-8'
- 大文件使用 mode=append 分块写入

### 3. 工具描述更新

write_file 工具描述中明确标注了 UTF-8 编码和分块写入建议。

### 4. 条件工具会话固定（Conditional Tool Session Pinning）

**根因**：`ssh`、`web_search`、`browser_*` 等工具通过 `conditionalKeepRules` 按关键词匹配决定是否加入工具列表。当用户说"登录服务器查看GPU"时，"服务器"关键词命中，`ssh` 被选入。但后续用户说"回忆下"让 LLM 通过 `memory` 工具回忆 SSH 连接信息时，消息不包含 SSH 关键词，`ssh` 工具被从列表中移除，导致 LLM 回忆起信息后无法调用 `ssh` 执行操作。

**修复**：
- `corelib/tool/router.go`：新增 `IsConditionalTool()` 导出函数，判断工具是否受条件规则管控
- `gui/_tmp_im_prefix.go`：两处工具执行循环中，当条件工具被成功调用后，通过 `ActivateSessionTool()` 将其固定到当前会话
- `tui/agent_handler.go`：同样在工具执行后固定条件工具

**原理**：`ActivateSessionTool` 将工具加入 `sessionTools` 集合，在 `Route()` 中与 `CoreToolNames` 同等对待，始终包含在工具列表中，不受后续消息关键词匹配影响。

### 5. Skill Runner 跨步骤状态传递（LibTV 兼容性修复）

**根因**：TUI 的 `toolRunSkill` 执行 skill 步骤时，各步骤之间没有状态传递机制。当 skill 的步骤 2（如 `create_session.py`）输出 sessionId，步骤 3（如 `query_session.py`）需要使用该 sessionId 时，TUI runner 无法将步骤 2 的输出传递给步骤 3，导致步骤 3 使用过期的缓存 sessionId 报 404。GUI 的 `executeSkillSteps`（旧路径）也缺少 `Capture` 支持。

**修复**：
- `tui/agent_tools.go`：
  - `toolRunSkill` 新增 output capture 支持：步骤成功后，通过 `step.Capture` 中定义的正则表达式从输出中提取变量，注入 `vars` map 供后续步骤使用
  - 新增 `resolveSkillStepTUI()` 函数：在执行每个步骤前，将 `vars` 中的变量替换到步骤的所有 params 中（不仅是 bash command）
  - 新增 `captureOutputVariablesTUI()` 函数：正则提取输出变量，与 GUI `skill_runner.go` 的 `captureOutputVariables` 行为一致
  - 新增 `classifySkillStepError()` 函数：对步骤失败进行分类（session_not_found / auth_error / timeout / network_error），提供可操作的错误提示
  - 新增 `substituteSkillVariablesRaw()` 函数：不带 shell 引号的变量替换，用于非 command 类型的 params
  - 新增 `on_failure` / `on_success` 条件步骤支持
  - 失败时标记剩余步骤为 skipped
- `gui/app_nl_skills.go`：
  - `executeSkillSteps` 新增 `vars` map 和 `Capture` 支持，与 `skill_runner.go` 的异步路径行为对齐
  - 步骤 params 中的 `{{key}}` / `${key}` 占位符会被已捕获的变量替换

**Skill 侧配合**：skill 定义中使用 `capture` 字段声明输出变量提取规则：
```json
{
  "action": "bash",
  "params": {"command": "python create_session.py --message '{{input}}'"},
  "capture": {"session_id": "sessionId[\":]\\s*([a-f0-9-]+)"}
}
```

### 6. Skill Runner 兼容性改进（基于 2026-04-12 测试报告）

**来源**：`Skill_Runner_测试报告_20260412.md`，覆盖 P1-P4 共 4 个优先级问题。

#### P1: exit status 9009 / 命令未找到 — 依赖预检 + 友好错误

**根因**：第三方 Hub Skill 依赖 `python3`/`pip`/`node` 等命令，Windows 上 `python3` 可能不存在或是 Microsoft Store 的 stub。Runner 直接报 `exit status 9009`，用户无法理解。

**修复**：
- `tui/agent_tools.go`：
  - `toolRunSkill` 新增执行前预检：平台兼容性检查、必需参数验证、必需环境变量验证、命令依赖检查
  - 新增 `checkCommandDependencyTUI()` 函数：在执行前通过 `exec.LookPath` 检查 bash 步骤中的主命令是否存在，Windows 上自动将 `python3` 映射为 `python` 检查
  - 针对 python/pip/node/npm 等常见命令提供具体安装指引
  - `classifySkillStepError()` 增强：识别 exit status 9009/127 并提供友好提示
- `gui/skill_runner.go`：
  - `classifyBashError()` 增强：针对 exit code 9009/127 提供具体命令的安装指引（python → python.org, node → nodejs.org 等）

#### P2: 安全策略过于严格 — 精细化关键词匹配 + 安全工具白名单

**根因**：`dangerousKeywords` 包含 `"format"`，导致所有参数中含 "format" 的 Skill 被一刀切拦截（如 `lovstudio:any2pdf` 的 "output format" 参数）。PPT生成、QR码、密码生成器等明确安全的工具类 Skill 也被拦截。

**修复**：
- `corelib/security/risk_assessor.go` + `gui/risk_assessor.go`：
  - 从 `dangerousKeywords` 中移除 `"format"`
  - 新增 `dangerousFormatPatterns`：仅匹配真正危险的磁盘格式化模式（`format c:`, `diskpart`, `mkfs` 等）
  - 新增 `safeToolCategories`：定义安全工具类别白名单（pdf, qr, pptx, image, screenshot, password, generator, converter 等）
  - `AssessSkill()` 增强：当 Skill 名称匹配安全工具类别时，风险等级上限为 medium（不会被 critical 拦截）
  - `SkillRiskInput` 新增 `Name` 字段，支持 corelib 层面的安全类别匹配

#### P3: 输入文件缺失错误处理不友好

**根因**：传入不存在的文件时直接报 `ENOENT` 原始 Node.js 错误，对非开发用户不友好。

**修复**：
- `tui/agent_tools.go`：`classifySkillStepError()` 新增 ENOENT / "no such file" 检测，返回 "输入文件不存在，请检查文件路径是否正确"
- `gui/skill_runner.go`：`classifyBashError()` 新增 ENOENT 检测，返回友好提示
- GUI 的 `checkFileReferences()` 已在 `StartRun` 中执行前校验文件存在性（此前已实现）

#### P4: API 速率限制感知缺失

**根因**：HTTP 429 错误直接透传给用户，无友好提示。

**修复**：
- `tui/agent_tools.go`：`classifySkillStepError()` 新增 HTTP 429 / "rate limit" / "too many requests" 检测，返回 "API 调用过于频繁，请稍后再试"
- `gui/skill_runner.go`：`classifyBashError()` 新增 HTTP 429 检测，返回友好提示

**验收标准**（对应测试报告）：
- `lovstudio:any2pdf`、`pptx-generator`、`QR Code Generator` 等安全工具类 Skill 不再被 critical 拦截
- 缺少 python3 时显示 "需要 Python 3 但未找到。请从 https://python.org 安装"
- ENOENT 错误显示 "输入文件不存在，请检查文件路径"
- HTTP 429 显示 "API 调用过于频繁，请稍后再试"

### 7. Skill Runner Windows 兼容性修复（基于 2025-07-10 集成测试报告）

**来源**：`Skill_Runner_Test_Report.md`，覆盖 P0 (BUG-001~003)、P1 (BUG-004~005)、P2 (IMP-001~002) 共 8 个问题。

#### BUG-001 (P0): Windows 8.3 短路径解析失败

**根因**：Windows 上 `write_file` 或 `$env:TEMP` 返回 8.3 短路径（如 `C:\Users\ADMINI~1\...`），Node.js 的 `fs.access()` 无法解析此格式，导致 `xh-md-to-pdf` 等涉及文件路径的 Skill 失败。

**修复**：
- `tui/agent_tools.go`：
  - 新增 `normalizeWindowsShortPathTUI()` 函数：通过 `filepath.EvalSymlinks` 将 8.3 短路径解析为完整长路径
  - 新增 `normalizePathsInCommandTUI()` 函数：扫描命令字符串中的 8.3 路径并替换为长路径
  - 新增 `win83PathRe` 正则：匹配包含 `~\d` 的 Windows 路径
  - `runSkillBashStreaming` 执行前自动规范化命令中的路径和工作目录
  - `toolRunSkill` 执行前规范化 `skill.SkillDir`
- `gui/skill_runner.go`：
  - 新增 `normalizeWindowsShortPathGUI()` 和 `normalizePathsInCommandGUI()` 函数
  - `runBashStepWithContextFull` 执行前自动规范化命令和工作目录路径
  - `StartRun` 执行前规范化 `target.SkillDir`

#### BUG-002 (P0): Bash shebang 在 Windows CMD 中被当作命令执行

**根因**：Skill 脚本以 `#!/bin/bash` 开头，在 Windows CMD/PowerShell 中 `#` 不是注释符号，被当作可执行命令报错 `'#' is not recognized`。TUI 之前统一使用 PowerShell 执行所有命令，无法处理 bash 语法。

**修复**：
- `tui/agent_tools.go`：
  - 新增 `needsBashShellTUI()` 函数：检测命令是否包含 bash 特有语法（shebang、export、管道、命令替换、tilde 展开等），与 GUI 的 `needsBashShell()` 行为一致
  - 新增 `findShTUI()` 函数：在 Windows 上查找 sh.exe/bash.exe（Git for Windows 提供）
  - 新增 `convertWindowsPathsInCommandTUI()` 函数：将反斜杠路径转换为正斜杠供 bash 使用
  - `runSkillBashStreaming` 重写 Windows 执行逻辑：
    - 需要 bash 时：通过 `findShTUI()` 找到 sh.exe，写入临时 .sh 脚本文件执行
    - 不需要 bash 时：使用 cmd.exe + 临时 .cmd 脚本（含 `chcp 65001` UTF-8 切换）
    - 支持 `preferred_shell` 参数强制指定 shell 类型
  - 新增 UTF-8 BOM 剥离（防止 BOM 导致 cmd.exe 报错）

#### BUG-003 (P0): craft_tool 步骤在 Windows 上挂起

**根因**：`craft_tool` 类型步骤通过 LLM 动态生成脚本并执行，在 Windows 上可能因子进程管理不稳定而挂起。TUI 之前完全不支持 `craft_tool` 步骤类型（返回 `unsupported action`）。GUI 的 `executeCraftToolCore` 不接受 context 参数，无法被全局超时取消。

**修复**：
- `tui/agent_tools.go`：
  - `runSkillStep` 新增 `craft_tool` 分支，调用 `runCraftToolStepTUI()`
  - 新增 `runCraftToolStepTUI()` 函数：支持预生成脚本的执行（带超时控制），对需要 LLM 动态生成的场景给出明确错误提示
  - `classifySkillStepError()` 新增 `context deadline exceeded` / `signal: killed` 检测
- `gui/skill_runner.go`：
  - `executeStepWithContext` 的 `craft_tool` 分支改为在 goroutine 中执行，通过 `select` 监听 context 取消，实现超时控制

#### BUG-004 (P1): Run ID 与 Session ID 不统一

**根因**：TUI 的 `toolRunSkill` 不生成 Run ID，无法通过 `get_session_output` 查询运行结果。

**修复**：
- `tui/agent_tools.go`：`toolRunSkill` 新增 Run ID 生成（格式 `run-{timestamp}-{counter}`），在执行输出中显示，方便调试和状态追踪。

#### BUG-005 (P1): 临时路径不一致

**根因**：`write_file` 写入路径与 `$env:TEMP` 实际路径不一致（8.3 短路径 vs 长路径），导致 Skill 找不到文件。

**修复**：
- 通过 BUG-001 的路径规范化统一解决：`skill.SkillDir`、命令中的路径、工作目录均在执行前规范化为长路径。

#### IMP-001 (P2): 跨平台 Shell 检测机制

**修复**：通过 BUG-002 的 `needsBashShellTUI()` 实现。TUI 现在与 GUI 一样，根据命令内容自动选择正确的 shell 解释器。

#### IMP-002 (P2): Skill 参数说明不够明确

**修复**：
- `tui/agent_tools.go`：
  - 新增 `detectImplicitRequiredArgsTUI()` 函数：扫描步骤命令中的 `{{key}}` / `${key}` 占位符，检测未提供的隐式必需参数
  - `toolRunSkill` 在执行前调用此检测，对未提供的参数给出明确提示（包含 Skill 描述信息）

**验收标准**（对应测试报告）：
- `xh-md-to-pdf`：8.3 短路径自动解析为长路径，Node.js `fs.access()` 不再报 ENOENT
- `pptx-generator`：shebang 行不再被 CMD 当作命令，自动路由到 bash 执行
- `craft_tool` 类型 Skill：TUI 支持预生成脚本执行，GUI 增加超时取消机制
- 所有 Skill 执行输出包含 Run ID，方便状态追踪
- 未提供 `{{input}}`/`{{output}}` 参数时，给出明确的参数缺失提示

### 8. SSH 后台任务 sudo 支持 + 认证方式优化

**根因**：SSH 后台任务通过 `nohup bash -c` 执行，无 TTY 交互能力，`sudo` 因无法读取密码而失败。LLM 遇到 sudo 失败后会陷入"换用非 sudo 方式"的循环。同时 `AuthMethod` 默认 `"key"`，但大多数服务器使用密码认证。

**修复**：

#### 方案 2: sudo 自动检测与降级（`corelib/remote/ssh_background_task.go`）

- `Submit()` 新增 sudo 检测：命令包含 sudo 时，先用 `sudo -n true` 测试免密权限
- 新增 `containsSudo()` 函数：正则匹配命令中的 sudo 调用
- 新增 `checkSudoNopasswd()` 函数：通过 SSH 会话测试免密 sudo
- 新增 `sudoFallback()` 函数 + `sudoFallbackRules` 规则表：
  - `sudo truncate` → `sudo -n truncate || echo 跳过`
  - `sudo rm /var/log/` → `sudo -n rm || echo 跳过`
  - `sudo journalctl --vacuum` → `sudo -n journalctl || echo 跳过`
  - `sudo systemctl` → `sudo -n systemctl || echo 跳过`
  - `sudo apt/yum/dnf` → `sudo -n ... || echo 跳过`
  - `sudo docker` → 去掉 sudo（提示加入 docker 组）
  - 通用 sudo → 改为 `sudo -n`（non-interactive）

#### 方案 3: PTY 交互式 sudo token 获取（`corelib/remote/ssh_background_task.go`）

- 新增 `EnsureSudoToken()` 函数：通过 PTY stdin 写入密码完成 sudo 认证
  - 先检测是否已有 token 或 NOPASSWD
  - 从 `SSHHostConfig.Password` 获取密码
  - 执行 `sudo -v` 触发密码提示
  - 检测密码提示后写入密码
  - 检测密码错误（Sorry/incorrect）时发送 Ctrl+C 中断
  - 验证 token 获取成功
- 新增 `RefreshSudoToken()` 函数：刷新/续期 sudo token
- `Submit()` 流程：先尝试方案 3（PTY 交互），失败再 fallback 到方案 2（降级）

#### sudo_prepare 工具暴露

- `tui/agent_tools_ssh.go`：新增 `sudo_prepare` action，调用 `EnsureSudoToken()`
- `gui/im_ssh_tools.go`：同上，GUI 侧也暴露 `sudo_prepare` action
- SSH 工具描述更新，包含 `sudo_prepare` 操作说明

#### SSH 认证方式优化（`corelib/remote/ssh_types.go` + `ssh_dial.go`）

- `Defaults()` 中 `AuthMethod` 默认值改为智能推断：有密码 → `"password"`，有密钥路径 → `"key"`，都没有 → `"password"`
- `buildAuthMethods()` 重写为多方法 fallback 模式：
  - 首选方式排第一
  - 密码认证时同时加 `keyboard-interactive`（兼容某些服务器）
  - 其他可用方式（密码/密钥/agent）作为 fallback 静默追加
  - SSH 库按顺序尝试，首选失败自动 fallback
- 提取 `buildKeyAuth()` 为独立函数复用
- `Password` 为空时不再报错，让 fallback 兜底

### 8. Skill Runner 补充修复（基于 2026-04-12 综合测试报告第二版）

**来源**：`Skill_Runner_Test_Report.md`（2026-04-12 第二版），覆盖 Bug #2、#3、#5 三个前一轮未涉及的问题。

#### Bug #2 (P0): craft_tool 缺乏 API 速率限制处理

**根因**：`craft_tool` 内部调用 LLM API 生成脚本时，HTTP 429 错误直接透传为失败，无重试机制。测试中 67% 的 Skill 使用 `craft_tool`，多个因 429 连续失败。

**修复**：
- `gui/tool_craft.go`：`generateScript()` 新增指数退避重试机制
  - 检测 HTTP 429 / "rate limit" / "too many requests" 错误
  - 最多重试 3 次，退避间隔 2s → 4s → 8s
  - 非 429 错误立即失败，不浪费重试
  - 所有重试耗尽后返回明确的 "已重试 N 次仍失败" 错误信息

#### Bug #3 (P1): needs_setup 状态 Skill 报错信息误导

**根因**：`lovstudio:any2pdf` 安装后状态为 `needs_setup`（依赖安装失败），但 GUI `StartRun` 的查找逻辑只匹配 `status == "active"`，导致 `needs_setup` 的 Skill 被报为 "not found or disabled"，误导用户。

**修复**：
- `gui/skill_runner.go`：`StartRun()` 的 Skill 查找改为不过滤状态，找到后再根据状态给出具体错误：
  - `needs_setup` → "needs setup. Installation was incomplete..."
  - `disabled` → "is disabled. Please enable it first"
  - 其他非 active 状态 → 显示实际状态值
- `tui/agent_tools.go`：`toolRunSkill()` 同样区分三种状态，给出具体操作指引

#### Bug #5 (P1): tts-to-mp3 "no executable steps" 错误不友好

**根因**：Skill 定义中未正确声明 steps 或缺少必要参数，Runner 只报 "no executable steps"，用户无法判断原因。

**修复**：
- `gui/skill_runner.go`：`StartRun()` 的空步骤错误信息增强，包含 `required_args` 列表和 Skill 描述
- `tui/agent_tools.go`：`toolRunSkill()` 同样增强，显示必需参数和描述信息

### 9. Skill Runner 方案 B 增强（Operations / Poll / When 条件）

**来源**：tvlib skill 支持需求——Runner 侧增强以支持多操作路由、异步轮询、条件分支。

#### 9.1 Operations（操作路由）

**需求**：api_workflow 模式的 Skill 包含多个操作（如 generate/query），LLM 需要根据用户意图选择执行哪个操作。

**修复**：
- `corelib/types.go`：新增 `NLSkillOperation` 类型（Name/Description/Params/Labels）；`NLSkillEntry` 新增 `Operations` 字段
- `corelib/skill/scanner.go`：新增 `SkillYAMLOperation` 类型；`SkillYAMLFile` 新增 `Operations` 字段；`loadSkillFromDir` 传递 operations；`ParseSkillYAMLFile` 的 `knownKeys` 加 `operations`
- `gui/skill_runner.go`：`StartRun` 支持 `runArgs["operation"]` → 查找匹配的 operation → 设置 `selectedSteps` 为 operation 的 labels
- `gui/im_tool_definitions.go`：`run_skill` 工具定义新增 `operation` 参数
- `gui/im_tools_misc.go`：`buildRunSkillArgs` 传递 `operation`
- `tui/agent_tools.go`：`toolRunSkill` 同步支持 operation → labels 映射

#### 9.2 Poll 循环轮询

**需求**：异步任务（如图片/视频生成）提交后需要轮询等待完成。

**修复**：
- `corelib/types.go`：新增 `StepPollConfig` 类型（Interval/MaxAttempts/UntilMatch/UntilStatus）；`NLSkillStep` 新增 `Poll` 字段
- `corelib/skill/scanner.go`：新增 `SkillYAMLStepPoll` 类型；`SkillYAMLStep` 新增 `Poll` 字段；`loadSkillFromDir` 传递 poll 配置
- `gui/skill_runner.go`：新增 `executeStepWithPoll()` 函数，包装 `executeStepWithContext` 实现循环轮询；`executeAsync` 改为调用 `executeStepWithPoll`
- `tui/agent_tools.go`：新增 `runSkillStepWithPollTUI()` 函数，同步实现轮询逻辑

#### 9.3 When 条件表达式

**需求**：根据运行时参数动态决定是否执行某个步骤。

**修复**：
- `corelib/types.go`：`NLSkillStep` 新增 `When` 字段
- `corelib/skill/scanner.go`：`SkillYAMLStep` 新增 `When` 字段；`loadSkillFromDir` 传递 when
- `gui/skill_runner.go`：`executeAsync` 在步骤执行前检查 `when` 条件；新增 `evaluateSimpleCondition()` 和 `substituteSkillVarsInString()` 辅助函数
- `tui/agent_tools.go`：同步支持 when 条件检查；新增 `evaluateSimpleConditionTUI()` 和 `substituteSkillVarsInStringTUI()`

#### 9.4 bash.norun（已有支持确认）

`extractAllBashBlocksFromMarkdown` 和 `extractOperationLabeledBlocks` 已通过 `bashBlockRe` 正则（捕获 `.norun` 后缀）和显式检查跳过 `.norun` 标记的代码块。无需额外修改。

**Skill YAML 示例**：
```yaml
name: openclaw
mode: api_workflow
operations:
  - name: generate
    description: "创建会话并生成图片/视频"
    params: ["message"]
    labels: ["create_session", "submit_task"]
  - name: query
    description: "查询生成进度"
    params: ["session_id"]
    labels: ["query_status"]
steps:
  - action: bash
    label: create_session
    params: { command: "python create_session.py --message '{{message}}'" }
    capture: { session_id: 'sessionId[":]\s*([a-f0-9-]+)' }
  - action: bash
    label: submit_task
    params: { command: "python submit.py --session {{session_id}}" }
  - action: bash
    label: query_status
    params: { command: "python query.py --session {{session_id}}" }
    poll:
      interval: 10
      max_attempts: 30
      until_match: '"status":\s*"(completed|failed)"'
```


### 10. Skill Runner 改进（基于 2026-04-13 测试报告）

**来源**：`Skill Runner 测试报告.md`（2026-04-13），覆盖 5 个问题（P0×2, P1×2, P2×1）。

#### 问题 1 (P0): SKILL.md 步骤间变量传递失败

**根因**：SKILL.md 中的步骤使用字面量占位符（如 `SESSION_ID`），但 Runner 没有从上一步输出中提取变量的机制。`Capture` 字段仅在 YAML 定义的 Skill 中可用，SKILL.md 解析器不支持。

**修复**：
- `corelib/skill/skill_markdown.go`：
  - 新增 `extractCommentRe` 正则：匹配 `<!-- extract: VAR=regex -->` HTML 注释
  - 新增 `extractCaptureDirectives()` 函数：解析 SKILL.md 中每个 bash 块前的 extract 注释，返回 capture map 列表
  - `ImportMarkdownSkillDir()` 在构建步骤时调用 `extractCaptureDirectives()`，将 capture 指令附加到对应步骤的 `Capture` 字段

**SKILL.md 使用示例**：
```markdown
## Step 2: Create Session
<!-- extract: SESSION_ID=sessionId[":]\s*([a-f0-9-]+) -->
```bash
python3 {baseDir}/scripts/create_session.py "hello"
```

## Step 3: Query Session
```bash
python3 {baseDir}/scripts/query_session.py {{SESSION_ID}}
```
```

#### 问题 2 (P0): `#` 注释行导致 Shell 选择错误

**根因**：SKILL.md 中的 bash 代码块包含 `#` 注释行，`needsBashShell()` 未检测到，选择了 cmd.exe 执行，导致 `'#' is not recognized` 错误。

**修复**：
- `gui/skill_runner.go`：`needsBashShell()` 新增多行扫描，检测 `#` 开头的行并返回 `true`（需要 bash）
- `tui/agent_tools.go`：`needsBashShellTUI()` 同步添加 `#` 注释行检测
- `corelib/skill/skill_markdown.go`：新增导出函数 `StripBashCommentLines()`，移除 `#` 开头的行
- `gui/skill_runner.go`：`runBashStepWithContextFull()` 在 cmd.exe 路径中调用 `StripBashCommentLines()` 作为安全兜底
- `tui/agent_tools.go`：`runSkillBashStreaming()` 在 cmd.exe 路径中同步调用

#### 问题 3 (低): tts-to-mp3 旧路径引用（.cceasy → .maclaw 迁移）

**根因**：crafted 类型 Skill 的 `skill.json` 中硬编码了旧版 `.cceasy` 目录路径，当前版本使用 `.maclaw`。

**修复**：
- `gui/skill_runner.go`：新增 `migrateLegacyCceasyPaths()` 函数，在 `StartRun()` 中执行前自动将步骤命令中的 `.cceasy` 路径替换为 `.maclaw`
- `tui/agent_tools.go`：新增 `migrateLegacyCceasyPathsTUI()` 函数，`toolRunSkill()` 中同步调用
- 仅在旧目录不存在且新目录存在时触发迁移

#### 问题 4 (P1): craft_tool 型 Skill 缺少用户上下文

**根因**：craft_tool 需要 AI 动态生成脚本，但 Runner 只传入 Skill 描述，不传入用户的原始请求，导致生成的脚本缺乏上下文。

**修复**：
- `gui/im_tool_definitions.go`：`run_skill` 工具定义新增 `user_prompt` 参数
- `gui/skill_runner.go`：`normalizeSkillRunVars()` 新增 `user_prompt` 变量传递；`executeAsync()` 在 craft_tool 步骤中注入 `user_prompt` 到 params
- `tui/agent_tools.go`：`normalizeRunSkillVars()` 新增 `user_prompt`；`runCraftToolStepTUI()` 将 `user_prompt` 拼接到 task 描述中

#### 问题 5 (P2): 参数校验提示优化

**修复**：`normalizeRunSkillVars()`（GUI/TUI）均已支持 `user_prompt` 作为顶层参数传递，与 `input`/`output`/`operation` 同等对待。

**验收标准**：
- libtv-skill：SKILL.md 中添加 `<!-- extract: SESSION_ID=... -->` 后，Step 3 使用实际 sessionId
- pptx-generator：`# Text extraction` 注释行不再导致 cmd.exe 报错，自动路由到 bash
- tts-to-mp3：`.cceasy` 路径自动迁移为 `.maclaw`
- craft_tool Skill：传入 `user_prompt` 后，LLM 有上下文生成更准确的脚本


### 11. CodeGen SSO 登录后 LLM Provider 被覆盖回"免费"

**根因**：`saveRemoteConfigField`（`useRemotePanel.ts`）使用前端 React state 中缓存的 `config` 对象做 merge + `SaveConfig`。SSO 登录完成后，后端已将 `maclaw_llm_current_provider` 设为 `"CodeGen"`，但前端 state 仍持有旧值 `"免费"`。紧接着 `onSaveField({ remote_email })` 被调用，用 stale config 覆盖了后端的更新，导致 maclaw 仍然连接 `localhost:18099`（免费代理）而非 CodeGen API。

**触发路径**：
1. `StartCodeGenSSOEmbedded` → 后端 goroutine 中 `SaveMaclawLLMProviders(providers, "CodeGen")` → config.json `maclaw_llm_current_provider = "CodeGen"` ✓
2. 前端收到 `WaitCodeGenSSOResult` 回调
3. 前端调用 `onSaveField({ remote_email: userEmail })` → `saveRemoteConfigField`
4. `saveRemoteConfigField` 用前端缓存的旧 `config`（`maclaw_llm_current_provider = "免费"`）merge `{ remote_email }` → `SaveConfig` 覆盖回 "免费"
5. maclaw 发起 LLM 请求 → 解析到 "免费" → `localhost:18099` → connection refused

**修复**：
- `gui/frontend/src/components/remote/useRemotePanel.ts`：`saveRemoteConfigField` 改为先 `await LoadConfig()` 从后端获取最新 config，再 merge patch 字段，避免用 stale 前端 state 覆盖后端的并发更新
- `gui/frontend/src/App.tsx`：`onSaveField` 的 `ui_mode` 分支移除多余的 `SaveConfig(c)` 调用（persist 已由 `saveRemoteConfigField` 处理），仅保留 `setConfig` 做前端 reactivity 更新


### 12. 条件工具提前固定 + 主动记忆召回增强

**来源**：用户报告"让 maclaw 查看 4090 服务器 GPU 占用率，它说不知道，让它回忆下，它就想起来了但没用 ssh 工具去搞"。

#### 问题 1: 条件工具在首轮匹配但未调用时，后续轮次丢失

**根因**：`conditionalKeepRules` 按每条消息的关键词匹配决定工具是否加入列表。`ActivateSessionTool`（session pinning）只在工具被**成功调用后**才触发。当用户第一条消息"登录4090服务器查看GPU"命中 ssh 关键词，但 LLM 因缺少 SSH 信息而未调用 ssh（先问用户要信息），后续消息"回忆下"不含 SSH 关键词，ssh 被移除。LLM 通过 memory 回忆到 SSH 信息后，工具列表里已经没有 ssh 了。

**修复**：
- `corelib/tool/router.go`：`Route()` 中 `matchConditionalKeepRules` 返回匹配结果后，立即对匹配到的工具调用 `ActivateSessionTool`（eager pin），不再等到工具被实际调用。受 `noPinConditionalTools`（如 `generate_pdf`）排除的工具不会被 eager pin。

#### 问题 2: 主动记忆召回（proactive recall）budget 不足，导致首轮未召回 SSH 信息

**根因**：`appendMemorySection` 使用 `RecallForProject` 进行主动召回，其 budget 受 `dynamicBudget()` 限制（根据活跃记忆数量动态调整），且 `maxProactiveRecall` 上限为 8 条。当记忆库较大时，SSH 服务器信息可能因 BM25 分数不够高而被截断。用户手动说"回忆下"时，LLM 调用 `memory(action: recall)` 走的是 `RecallDynamic`，budget 更大（maxEntries=15, maxTokens=1500），query 也更精准，所以能召回。

**修复**：
- `gui/im_system_prompt.go`：`appendMemorySection` 的主动召回从 `RecallForProject` 改为 `RecallDynamic`（category 传空字符串表示不限类别），获得更大的 budget 和更好的召回率，与 memory 工具的 recall action 行为对齐。
- `gui/im_system_prompt.go`：新增实体补充召回机制——用 `ExpandQuery` 从用户消息中提取关键实体（如 "4090服务器"、"GPU"），对每个实体单独调用 `RecallDynamic` 做精准召回，合并去重后注入 system prompt。解决完整用户消息过长时 BM25 权重被噪音词稀释的问题。
- `gui/im_system_prompt.go`：`maxProactiveRecall` 从 8 提升到 12，允许更多相关记忆注入 system prompt。

#### 问题 3: 应用重启后，记忆恢复的 SSH 任务无法触发 ssh 工具

**根因**：应用重启后 `Router.sessionTools` 为空（内存态，不持久化）。maclaw 从记忆中恢复了未完成的 SSH 任务（"登录4090服务器查GPU"），自己说"要继续连 home.rapidai.tech:33"，用户回复"开工吧"。但 "开工吧" 不含任何 SSH 关键词，`conditionalKeepRules` 不匹配，ssh 工具不在列表里。LLM 只能用手头有的工具（如 screenshot），导致截屏发给用户而不是 SSH 登录。

**修复**：
- `corelib/tool/router.go`：新增导出函数 `MatchConditionalTools(text)`，对任意文本执行条件工具关键词匹配，返回匹配到的工具集合
- `gui/im_system_prompt.go`：`appendMemorySection` 在注入召回记忆后，扫描记忆内容调用 `MatchConditionalTools`，对匹配到的工具执行 `ActivateSessionTool`（memory-driven pin）。这样当记忆内容包含"服务器"、"SSH"、"host"等关键词时，ssh 工具会被自动 pin 到 session，即使用户消息只是"开工吧"
- `gui/im_system_prompt.go`：self_identity 注入处增加"绝不要在对话中向用户自我介绍或复述这些内容"的指令，防止 LLM 在恢复任务时泄露人设信息


### 13. HTTP 5xx 网关错误导致 HTML 透传 + 输入框卡死

**根因**：`doOpenAILLMRequestStream` 只对 404/429/401/403 做了特殊处理。当 API 网关返回 502/503/504 时（如 nginx 超时），响应 Content-Type 为 `text/html`，代码走到 SSE 检测逻辑，peek 后发现不是 SSE，调用 `parseNonStreamOpenAIResponse(resp)`。该函数检测到 `statusCode != 200` 返回 error，但 error 消息包含完整的 HTML body（如 `<center><h1>504 Gateway Time-out</h1></center>`），最终透传到聊天界面显示为原始 HTML 标签。

输入框卡死的原因：`SendAIAssistantMessage` 是同步阻塞的 Wails binding，在 LLM 请求期间前端 `activeRound.phase` 停在 `'requesting'`，`inputLocked = true`。504 错误后如果触发重试（adaptive retry 或 `isRetryableLLMError`），重试等待期间输入框持续锁定。但 `isRetryableLLMError` 之前不识别 5xx 错误，导致错误直接返回而不重试，前端 `finally` 块中 `finalizeRound` 正常执行恢复输入框。实际卡死可能是因为 error 消息中的 HTML 导致前端渲染异常。

**修复**：

#### 1. 5xx 网关错误提前拦截（`gui/llm_stream.go`）
- `doOpenAILLMRequestStream`：在 SSE 检测逻辑之前新增 `resp.StatusCode >= 500` 检查，直接读取 body 并调用 `classifyOpenAIHTTPError` 返回友好错误消息，避免 HTML body 进入 SSE/非流式解析路径
- `doAnthropicLLMRequestStream`：同步添加 5xx 和 429/401/403 的提前拦截

#### 2. classifyOpenAIHTTPError 增强（`gui/llm_stream.go`）
- 新增 502 → "API 网关错误，上游服务不可用"
- 新增 503 → "API 服务暂时不可用"
- 新增 504 → "API 网关超时，上游服务响应过慢"
- 新增 5xx 通用 → "API 服务端错误"

#### 3. HTML 标签剥离（`gui/llm_stream.go` + `corelib/llm/types.go`）
- `truncateLLMBody`：检测 HTML 内容后用正则剥离标签、合并空白，防止 HTML 透传到用户界面
- `corelib/llm/types.go`：`ParseNonStreamOpenAIResponse` 和 `ParseNonStreamAnthropicResponse` 的错误消息使用新增的 `sanitizeHTMLBody` 函数清理 HTML

#### 4. 5xx 错误可重试（`gui/llm_retry.go` + `gui/adaptive_retry.go`）
- `isRetryableLLMError`：新增 "HTTP 502"/"HTTP 503"/"HTTP 504" 匹配
- `AdaptiveRetry.networkKeywords`：新增 "http 502"/"http 503"/"http 504"/"gateway timeout"/"bad gateway"/"service unavailable"

**验收标准**：
- 504 Gateway Timeout 时显示 "API 网关超时，上游服务响应过慢，请稍后再试 (HTTP 504)" 而非原始 HTML
- 502/503 同理显示友好中文提示
- 5xx 错误触发自动重试（最多 1 次），重试失败后正常返回错误，输入框恢复可用
- 暂停键在 LLM 请求期间正常生效（context 取消中断 HTTP 请求）


### 14. 工具合并 + 渐进式暴露（基于 Anthropic Claude Code 工具设计复盘）

**来源**：Anthropic 2026-04-13 复盘文章《做Agent，最难的不是加工具，而是站到模型那边》。核心观点：工具不是越多越好，每多一个工具模型多一层判断成本。

#### 14.1 工具合并（13 个 → 3 个）

**根因**：配置管理 6 个工具、定时任务 4 个工具、会话模板 3 个工具，共 13 个独立工具定义占据大量 context，增加模型选择负担。

**修复**：
- `gui/im_tool_definitions.go`：
  - `get_config` + `update_config` + `batch_update_config` + `list_config_schema` + `export_config` + `import_config` → `manage_config(action=get/set/batch/schema/export/import)`
  - `create_template` + `list_templates` + `launch_template` → `manage_template(action=create/list/launch)`
  - `create_scheduled_task` + `list_scheduled_tasks` + `delete_scheduled_task` + `update_scheduled_task` → `manage_schedule(action=create/list/delete/update)`
- `gui/tool_registry_builtin.go`：注册合并工具 + 旧工具名 backward-compat aliases（仅 handler 可用，不生成定义）
- `gui/im_tools_misc.go`：新增 `toolManageConfig()`、`toolManageTemplate()`、`toolManageSchedule()` 调度器
- `tui/agent_handler.go`：`dispatchTool` 新增合并工具 case + 保留旧工具名 backward compat
- `tui/agent_tools.go`：新增 `toolManageConfig()`、`toolManageTemplate()`、`toolManageSchedule()` 调度器
- `corelib/tool/router.go`：`BuiltinToolNames` 新增 `manage_config`、`manage_template`、`manage_schedule`

#### 14.2 渐进式工具暴露（Progressive Tool Discovery）

**根因**：40+ 工具定义全部塞在初始 prompt 中，占据约 3000-5000 token。低频工具（配置管理、定时任务、模板、AgentNet、审计日志等）在大多数对话中不会被使用，但始终占据 context。

**修复**：
- `gui/tool_deferred.go`：新增 `DeferredToolNames` 列表，定义延迟加载的工具集合
- `gui/tool_definition_generator.go`：
  - 新增 `deferredTools` 字段和 `SetDeferredTools()` 方法
  - `Generate()` 过滤掉 deferred 工具
  - 新增 `SearchDeferred()` 和 `GenerateDeferred()` 方法
- `gui/app.go`：初始化 `toolDefGenerator` 时调用 `SetDeferredTools(DeferredToolNames)`
- `gui/tool_registry_builtin.go`：`discover_tool` 描述增强，明确列出可发现的能力类别
- `gui/tool_discover.go`：`toolDiscoverTool` 增强，同时搜索 registry 和 deferred 工具定义

**延迟工具列表**：
- 合并工具：`manage_config`、`manage_schedule`、`manage_template`
- 旧工具名（backward compat）：`get_config`、`update_config` 等 13 个
- 低频工具：`agentnet_search`、`agentnet_publish`、`query_audit_log`、`parallel_execute`、`recommend_tool`、`switch_llm_provider`

**工作原理**：
1. 初始 prompt 只包含核心工具（~15 个）+ `discover_tool`
2. 模型需要配置管理/定时任务等能力时，调用 `discover_tool(need="修改配置")`
3. `discover_tool` 搜索 registry + deferred 定义，返回匹配工具并 session pin
4. 后续轮次中，已发现的工具自动可用

**预期收益**：
- 初始 prompt 工具定义减少约 40%，节省 2000-3000 token
- 模型在简单任务中不被无关工具干扰，选择准确率提升
- 复杂任务通过 discover_tool 按需加载，不影响能力完整性


### 15. 工具使用反馈闭环（Tool Outcome Learning）

**来源**：Anthropic 复盘文章强调"多实验，认真读输出"。maclaw 的 `UsageTracker` 已记录工具使用频率，但缺少结果反馈——模型调用工具后成功还是失败、是否重试/放弃，这些信号没有回流到路由决策中。

**修复**：
- `corelib/tool/usage_tracker.go`：
  - `UsageRecord` 新增 `FollowUp` 字段（"continue"/"retry"/"abandon"）
  - 新增 `RecordOutcome()` 方法：记录工具调用的完整结果（成功/失败 + 后续动作）
  - 新增 `OutcomeScore()` 方法：基于近 7 天的成功率、重试率、放弃率计算工具质量分 [0,1]
- `corelib/tool/router.go`：
  - `Route()` 多信号评分新增 outcome 信号（第五个信号）
  - 有 tracker 时权重分配：retrieval=0.45, experience=0.20, skill_match=0.15, outcome=0.10, priority=0.10
  - 无 skill provider 时：retrieval=0.50, experience=0.25, outcome=0.15, priority=0.10
- `gui/im_message_handler.go`：
  - 新增 `usageTracker` 字段和 `SetUsageTracker()` 方法
  - Agent loop 的 `observeIteration` 后自动调用 `RecordOutcome()`，基于 `classifyToolOutcome` 结果记录
  - 自动检测 retry（同批次同名工具重复调用）和 abandon（整体失败时非重试的失败工具）
- `gui/app.go`：
  - App 结构体新增 `usageTracker` 字段
  - 初始化时创建 `UsageTracker`（持久化到 `~/.maclaw/data/tool_usage.json`）
  - 自动接线到 `toolRouter` 和 `IMMessageHandler`

**工作原理**：
1. 每次工具调用后，`classifyToolOutcome` 判断成功/失败/不确定
2. `RecordOutcome` 记录结果 + 后续动作（continue/retry/abandon）
3. `OutcomeScore` 计算近 7 天质量分：successRate - retryPenalty*0.3 - abandonPenalty*0.5
4. `Route()` 将 outcomeScore 作为第五个信号融入工具排序
5. 高失败率/高重试率的工具自动降权，高成功率的工具自动升权

### 16. AskUser 结构化提问工具

**来源**：Anthropic 发现让 Claude 在文本中提问不稳定（格式漂移、选项遗漏），最终做成独立的 `AskUserQuestion` 工具。判断标准：动作足够重要、足够高频、做错会打断流程时，就应该升级为工具。

**问题**：maclaw 的 LLM 在需要向用户澄清需求时，只能在文本中自由发挥。coding-workflow 规则要求"阶段确认"，但完全依赖 prompt 约束，模型经常跳过或混淆。

**修复**：
- `gui/im_tool_ask_user.go`：新文件
  - `AskUserRequest` 结构体：Question/Options/Context/InputType
  - `toolAskUser()` handler：解析参数，返回 `__ASK_USER__` 标记的结构化结果
  - `IsAskUserResult()` / `ParseAskUserResult()`：检测和解析 ask_user 结果
  - `FormatAskUserForDisplay()`：文本格式化（TUI/IM 网关使用）
- `gui/im_tool_definitions.go`：新增 `ask_user` 工具定义
- `gui/tool_registry_builtin.go`：注册 `ask_user` 工具
- `corelib/tool/router.go`：`CoreToolNames` 新增 `ask_user`（始终可用）
- `gui/im_message_handler.go`：Agent loop 中检测 `__ASK_USER__` 结果，暂停循环并返回结构化响应
  - choice 类型：返回带选项按钮的 `IMResponseAction` 列表
  - confirm 类型：返回确认/取消按钮
  - text 类型：返回问题文本，等待用户自由输入
- `tui/agent_handler.go`：`dispatchTool` 新增 `ask_user` case
- `tui/agent_tools.go`：新增 `toolAskUser()` handler（TUI 版本）

**工作原理**：
1. 模型需要用户输入时，调用 `ask_user(question="...", input_type="confirm")`
2. Handler 返回 `__ASK_USER__` 标记的结构化数据
3. Agent loop 检测到标记，暂停循环，将问题渲染为带按钮的 UI
4. 用户点击按钮或输入文本后，回答作为下一条消息进入 agent loop
5. 模型在下一轮收到用户回答，继续执行

**预期收益**：
- coding-workflow 的阶段确认可以从 prompt 约束迁移为 `ask_user(input_type="confirm")` 调用
- 消除模型"问了但没等回答就继续"的问题
- IM 侧用户体验提升（按钮交互 vs 纯文本）


### 17. Task 任务管理工具

**来源**：Anthropic 将 `TodoWrite` 替换为 `Task`，核心从"让单个模型别跑偏"变为"让多个 Agent 共享任务、依赖和进展"。

**修复**：
- `corelib/task/store.go`：新包，轻量级内存任务存储
  - `Task` 结构体：ID/Title/Description/Status/DependsOn/DelegatedTo/StatusNote
  - `Store`：线程安全的 CRUD + 自动依赖解锁（任务完成时自动 unblock 依赖它的任务）
  - 状态：pending → in_progress → completed/failed，blocked（依赖未满足时自动设置）
- `gui/im_tool_task.go`：GUI 侧 `task` 工具 handler
  - 支持 create/update/complete/fail/list/delegate/delete 七个 action
  - `taskStore` 字段添加到 `IMMessageHandler`，按需初始化
- `tui/agent_tool_task.go`：TUI 侧 `task` 工具 handler
- `gui/im_tool_definitions.go`：新增 `task` 工具定义
- `gui/tool_registry_builtin.go`：注册 `task` 工具
- `corelib/tool/router.go`：`CoreToolNames` 新增 `task`（始终可用）
- `tui/agent_handler.go`：`dispatchTool` 新增 `task` case + `taskStore` 字段

**工作原理**：
1. 模型收到复杂任务时，调用 `task(action="create", title="步骤1: ...")` 拆分任务
2. 设置依赖关系：`task(action="create", title="步骤2", depends_on=["task-1"])`
3. 逐个执行并更新：`task(action="complete", task_id="task-1")`
4. task-1 完成后，task-2 自动从 blocked → pending
5. 可委派给编程会话：`task(action="delegate", task_id="task-2", delegate_to="session-xxx")`

### 18. 子 Agent 委派工具（delegate_task）

**来源**：Anthropic 做了 "Claude Code Guide 子代理"——当用户问使用问题时，主 Agent 调用子代理查文档，而非在主 context 中塞满使用说明。核心原则：不是所有知识都要塞进主上下文。

**修复**：
- `gui/im_tool_subagent.go`：新文件
  - `SubAgentSpec` 结构体：Name/Description/Prompt
  - `builtinSubAgents` 预定义两个子 Agent：
    - `coding_workflow`：编码工作流专家，引导需求→设计→任务拆分，使用 ask_user 确认每个阶段
    - `help`：MaClaw 使用帮助专家，回答功能/配置/工具使用问题
  - `toolDelegateTask()` handler：返回 `__SUBAGENT_CONTEXT__` 标记的专业 prompt
  - `IsSubAgentContext()` / `ExtractSubAgentContext()`：检测和提取子 Agent 上下文
- `tui/agent_tool_subagent.go`：TUI 侧 handler
- `gui/im_tool_definitions.go`：新增 `delegate_task` 工具定义
- `gui/tool_registry_builtin.go`：注册 `delegate_task` 工具
- `gui/im_message_handler.go`：Agent loop 中检测 `__SUBAGENT_CONTEXT__`，将子 Agent 的专业 prompt 注入为 tool_result

**工作原理**：
1. 用户提出编码需求 → 模型调用 `delegate_task(agent="coding_workflow", request="开发一个...")`
2. Handler 返回 coding_workflow 子 Agent 的专业 prompt + 用户请求
3. Agent loop 将 prompt 注入为 tool_result，模型在下一轮按照专业指导执行
4. coding_workflow 子 Agent 使用 ask_user 确认每个阶段，使用 task 创建任务列表

**预期收益**：
- 主 system prompt 可以移除 coding-workflow 的详细规则（约 2000 字）
- 编码工作流的遵从率提升（专业 prompt 在 tool_result 中，距离近、权重高）
- 主 Agent 的 context 更干净，不被辅助信息污染
- 未来可扩展更多子 Agent（代码审查、测试生成、文档编写等）


### 19. AI 助手面板流式内容被最终响应覆盖

**根因**：Agent loop 多轮迭代时，每轮 LLM 输出通过 `onToken` 回调流式推送到前端，前端将所有 token 累积到同一个 assistant 消息中。但当 `SendAIAssistantMessage` 同步调用返回时，`IMAgentResponse.Text` 只包含**最后一轮**迭代的 `msgContent`（即 `stripThinkingTags(choice.Message.Content)`），而非所有轮次的累积文本。

前端 `resolveFinalRoundContent()` 的逻辑是：如果 `response.text` 非空，直接用它替换消息内容。这导致用户已经看到的完整流式输出（如需求文档）被最后一轮的短文本（如 PPTX 相关评论）覆盖。

**触发场景**：
1. 用户发送"开发一个贪吃蛇游戏"
2. LLM 第一轮输出需求文档（通过 streaming 显示在面板中）
3. LLM 调用工具（如 generate_pdf）
4. LLM 第二轮输出关于 PPTX 的补充说明
5. `resp.Text` = 第二轮的短文本 → 覆盖了面板中已显示的完整需求文档

**修复**：
- `gui/frontend/src/components/ai/useAIAssistant.ts`：`resolveFinalRoundContent()` 增加流式内容保护逻辑
  - 当消息已有流式累积内容（`message.content`）且 `response.text` 也非空时，检查流式内容是否以 `response.text` 结尾（`endsWith`）
  - 如果是后缀关系且流式内容更长 → 保留流式内容（它是完整的多轮累积输出，`response.text` 只是最后一轮的片段）
  - 如果不是后缀关系 → 使用 `response.text`（可能来自 `ask_user`、取消、文件发送等特殊处理路径，内容与流式输出无关）
  - 这确保了多轮 agent loop 中，前端已累积的完整输出不会被最后一轮的片段覆盖，同时不影响 `ask_user`、截图、文件发送等特殊路径的正确行为

**验收标准**：
- 多轮 agent loop（如编码工作流的需求文档输出 + 工具调用 + 后续文本）中，面板显示完整的累积流式内容
- `ask_user`、取消、截图、文件发送等特殊路径仍正确显示 `response.text`
- 非流式响应（如简短闲聊）仍正常显示 `response.text`
- 所有 69 个现有 useAIAssistant 测试通过


### 20. 第三方 API 服务商 Rate Limit 缓解（Claude Code 编程会话）

**根因**：maclaw agent 通过 `create_session` 启动 Claude Code 子进程执行编程任务时，Claude Code 的内部 agent loop 以全自动模式运行（无人工交互间隔），请求非常密集。当使用第三方 Anthropic 兼容 API（如智谱 GLM `open.bigmodel.cn`、百度千帆等）时，这些服务商的 rate limit 比 Anthropic 官方严格得多。加上 maclaw agent 自身也在调用同一个 API key（做意图判断、生成文档等），两路并发请求很容易触发 429 rate limit。

手动启动 Claude Code 时不触发 429 的原因：只有 Claude Code 一路在请求，且有用户交互间隔做天然限流。

**修复**：

#### 1. 为所有第三方服务商启用流量优化（GUI + TUI）

- `gui/app.go`：`buildClaudeLaunchEnv()` 对所有 `!selectedModel.IsBuiltin` 的服务商注入：
  - `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`：禁用 Claude Code 的后台遥测、模型可用性检查等非必要 API 调用
  - `API_TIMEOUT_MS=600000`（10 分钟）：增大超时，让 Claude Code 内部重试有更多退避时间
  - 此前这两个变量仅对百度千帆设置，现在扩展到所有第三方服务商
- `tui/tool_launch_env.go`：`buildClaudeEnv()` 同步添加相同逻辑

#### 2. settings.json 持久化流量优化配置

- `corelib/configfile/claude.go`：`writeAnthropicSettings()` 当 `baseURL` 非空（即第三方服务商）时，在 `env` 中写入 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 和 `API_TIMEOUT_MS=600000`
- 这确保即使 Claude Code 内部重启子进程（环境变量可能丢失），settings.json 中的配置仍然生效

#### 3. System Prompt 增加 Rate Limit 感知

- `gui/im_system_prompt.go`：自动续接规则新增"每次续接前等待 5 秒"
- API 错误自动重试规则增强：
  - 检测 rate_limit/429/too many requests 关键词时，必须等待至少 60 秒再重试
  - 连续 2 次 rate limit 错误后停止重试，告知用户 API 配额不足

**验收标准**：
- 使用智谱 GLM 等第三方服务商时，Claude Code 不再因非必要流量触发 429
- settings.json 中包含 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` 和 `API_TIMEOUT_MS`
- 自动续接和 API 错误重试有明确的退避等待，不会密集轰炸 API


### 21. 编码工作流启动门控过严——短指令被误拦截

**根因**：`checkSessionTaskGuard()` 仅基于 `h.lastUserText`（当前消息）调用 `classifyTaskIntent()` 做意图分类，不考虑对话历史上下文。当用户在前几轮已经明确讨论了编码任务（如"开发贪吃蛇游戏"），后续用短指令"开工"、"开干"、"动手"表示"开始执行"时，`classifyTaskIntent("开工")` 因不匹配任何 `codingKeywords` 而返回 `intentAmbiguous`，guard 返回"目标不够明确"拦截消息，导致用户被反复要求重新表述。

同样，`coding_tool_gate.go` 中的 `containsSkipSignal()` 不包含这些动作短语，导致即使 LLM 正确理解了上下文并尝试调用编码工具，gate 也会在 iteration 0 剥离这些调用。

**修复**：

#### 1. 新增 `codingActionPhrases` 动作短语列表

- `gui/im_tools_session.go`：新增 `codingActionPhrases` 变量，包含"开工"、"开干"、"动手"、"搞起来"、"开始吧"、"start"、"let's go" 等短动作指令
- 这些短语不加入 `codingKeywords`（太泛，单独出现不应触发编码分类），而是作为独立列表用于上下文感知判断

#### 2. `checkSessionTaskGuard` 上下文感知

- `gui/im_tools_session.go`：`checkSessionTaskGuard()` 新增上下文感知逻辑：
  - 当 `classifyTaskIntent` 返回 `intentAmbiguous` 或 `intentUnknown` 时
  - 检查当前消息是否包含动作短语（`hasCodingActionPhrase`）
  - 如果包含，扫描最近 10 条对话历史（`conversationHasCodingContext`），查找 `codingKeywords` 匹配
  - 如果历史中有编码上下文，视为延续信号，放行 session 创建
- 新增 `hasCodingActionPhrase(text)` 函数：子串匹配动作短语
- 新增 `conversationHasCodingContext()` 方法：扫描 `h.memory.load("desktop-user")` 最近 10 条记录

#### 3. Coding Tool Gate 跳过信号扩展

- `gui/coding_tool_gate.go`：`skipSignalsChinese` 新增"开工"、"开干"、"动手"、"搞起来"等动作短语
- `gui/coding_tool_gate.go`：`skipSignalsEnglish` 新增 "let's go"、"let's do it"、"start"、"begin"
- 这确保当 LLM 在 iteration 0 尝试调用编码工具时，gate 不会剥离这些调用

#### 4. Steering 规则更新

- `.kiro/steering/coding-workflow.md`：例外情况新增"开工"、"开干"、"动手"等短语说明

**验收标准**：
- 用户先说"开发贪吃蛇游戏"，再说"开工" → session 创建不被拦截
- 用户直接说"开工"（无上下文）→ 仍然被拦截（保守策略）
- 用户说"开工"且 coding tool gate 激活 → gate 检测到跳过信号，不剥离编码工具
- 所有现有 intent classifier / coding tool gate 测试通过


### 22. 编码任务逐 Task 执行调度器（TaskExecutionOrchestrator）

**根因**：编程任务完成三阶段流程（需求→设计→任务分解）后，LLM 进入执行阶段时，把整个项目的需求/设计文档和所有任务描述打包成一个大 prompt 一次性扔给编程工具（`send_and_observe`），导致任务分解完全失去意义。编程工具收到的是一个巨大的"做整个项目"的指令，而不是聚焦的单任务指令。

**修复**：

#### 1. 新增 `TaskExecutionOrchestrator`（`gui/task_execution_orchestrator.go`）

- `TaskExecutionOrchestrator` 结构体：管理逐 task 执行状态
  - `Tasks []*TaskItem`：有序任务列表，每个任务包含 Title/Description/Files/AcceptanceCriteria/DependsOn/Status/RetryCount
  - `CurrentIndex`：当前执行的任务索引
  - `RequirementsContext`/`DesignContext`：精简的需求/设计摘要（不是完整文档）
  - `MaxRetries`：每个任务的最大 TDD 重试次数（默认 3）
- `Activate()`：解析确认的任务列表，进入执行模式
- `BuildTaskPrompt()`：为当前任务构造聚焦的 prompt，包含：
  - 当前任务描述和涉及文件
  - 验收标准（TDD 测试条件）
  - 前置任务的产出物（文件路径、状态）
  - 精简的需求/设计上下文摘要（截断到 500 字符）
- `BuildSystemInjection()`：每次 agent loop 迭代时注入的系统消息，提醒 LLM 当前在执行哪个任务
- `BuildTDDPrompt()`/`BuildFixPrompt()`：TDD 测试和修复的专用 prompt
- `ProgressSummary()`/`FinalReport()`：进度和最终报告生成
- `ParseTaskListFromText()`：从 Markdown 任务列表文本中提取结构化任务

#### 2. Agent Loop 注入（`gui/im_message_handler.go`）

- `IMMessageHandler` 新增 `taskOrchestrator *TaskExecutionOrchestrator` 字段
- `NewIMMessageHandler()` 中初始化
- Agent loop 的系统消息注入点（recover prompt 之后）新增 orchestrator 注入：
  - 当 orchestrator 处于 active 状态时，每次迭代注入 `BuildSystemInjection()` 系统消息
  - 消息包含当前任务编号、操作指引、进度统计

#### 3. `send_and_observe` 任务上下文增强（`gui/im_tools_session.go`）

- `toolSendAndObserve()` 增强：当 orchestrator active 且当前任务为 pending 时
  - 自动调用 `BuildTaskPrompt()` 构造聚焦的任务 prompt
  - 将 LLM 原始文本作为"补充说明"追加（而非替换）
  - 自动将任务状态标记为 `in_progress`
  - 记录 session ID 到当前任务

#### 4. System Prompt 执行规则强化（`gui/im_system_prompt.go`）

- 执行规则从 6 条简单列表改为详细的 a-g 子步骤流程
- 新增"严禁行为"专区，明确禁止：
  - 把多个任务合并成一个大 prompt
  - 把完整需求/设计文档原文发给编程工具
  - 跳过 TDD 测试直接进入下一个任务
- 明确告知 LLM "系统会自动注入任务上下文"

#### 5. Steering 规则更新（`.kiro/steering/coding-workflow.md`）

- 严禁行为新增两条：禁止合并任务 prompt、禁止跳过 TDD

**工作原理**：
1. 用户确认任务列表后，orchestrator 被 `Activate()` 激活
2. 每次 agent loop 迭代，orchestrator 注入系统消息提醒 LLM 当前任务
3. LLM 调用 `create_session` + `send_and_observe` 时，orchestrator 自动将 LLM 发送的文本替换/增强为聚焦的单任务 prompt
4. 任务完成后 LLM 发送 TDD 测试指令，orchestrator 跟踪测试结果
5. 测试通过 → `AdvanceToNext()`，测试失败 → `IncrementRetry()`
6. 所有任务完成后 `FinalReport()` 生成验收报告

**验收标准**：
- 编程工具收到的 prompt 只包含当前任务描述 + 精简上下文，不包含完整项目需求
- 每个任务独立执行、独立测试、独立报告进度
- 测试失败时自动重试最多 3 次，超过后跳到下一个任务
- 所有 12 个 spec workflow property test 通过
- 6 个新增 orchestrator 单元测试通过


### 22.1 集成联调阶段（Integration Phase）

**根因**：各子任务独立开发完成后，直接跳到全量测试。但子任务之间的模块接口对接、import 引用、main 入口文件更新、胶水代码（路由注册、依赖注入等）没有人负责。导致全量测试必然失败，编程工具需要在测试阶段同时做集成+修复，效率低且容易遗漏。

**修复**：

#### 1. System Prompt 新增"第七步：集成联调"（`gui/im_system_prompt.go`）

- 插入在"任务执行"和"完成验收"之间
- 原"第七步：完成验收"改为第八步，"第八步：自动续接"改为第九步
- 集成流程：创建新会话 → 发送集成指令（含所有任务产出文件列表）→ 检查 import/依赖 → 补全胶水代码 → 编译 → 运行
- 编译失败最多重试 3 次

#### 2. `BuildIntegrationPrompt()`（`gui/task_execution_orchestrator.go`）

- 列出所有已完成任务及其产出文件（含状态图标 ✅/❌/⏭️）
- 6 步集成检查清单：import 检查 → main 入口 → 接口匹配 → 胶水代码 → 编译 → 运行
- 附带精简的需求上下文摘要

**工作原理**：
1. 所有子任务完成后，orchestrator 的 `AllDone()` 返回 true
2. Agent loop 检测到所有任务完成，进入集成阶段
3. 创建新会话，用 `BuildIntegrationPrompt()` 构造集成指令
4. 编程工具串联所有模块，修复编译错误
5. 集成通过后进入验收阶段（全量测试）


### 23. ask_user 返回时对话历史丢失 + 用户文本输入被当作新请求

**根因**：编程工作流需求确认阶段，LLM 通过 `ask_user` 工具展示需求文档并请求确认（显示"确认需求"/"我要修改需求"按钮）。当用户不点按钮而是直接输入文本（如 `c++ cmake` 表示补充需求），系统丢失任务上下文，将输入当作全新请求处理。

根因链路：
1. Agent loop 中 `ask_user` 返回 `__ASK_USER__` 标记后，代码直接 `return resp`，未调用 `saveConversationHistoryTimed()`
2. 本轮累积的 `history`（包含需求文档、工具调用、ask_user 问题）未持久化
3. 用户下一条消息加载的历史缺失需求文档
4. 没有"pending ask_user"状态追踪，LLM 无法区分"新请求"和"对确认问题的回答"

**修复**：

#### 1. ask_user 返回前保存 history（`gui/im_message_handler.go`）

- 在 `ParseAskUserResult` 成功后、`return resp` 前，将 tool result 追加到 `conversation` 和 `history`
- 调用 `saveConversationHistoryTimed(userID, history, nil)` 持久化完整历史
- 确保下一条消息能加载到包含需求文档和 ask_user 问题的完整上下文

#### 2. Pending ask_user 状态追踪（`gui/im_message_handler.go`）

- `IMMessageHandler` 新增 `pendingAskUser sync.Map` 字段
- 新增 `pendingAskUserState` 结构体（Question/Options/InputType/Timestamp）
- ask_user 返回时存储 pending 状态
- 下一条消息处理时通过 `LoadAndDelete` 消费 pending 状态（一次性）
- 超过 30 分钟的 pending 状态视为过期

#### 3. 上下文注入 + topic detection 跳过（`gui/im_message_handler.go`）

- 消费 pending 状态后构建 `askUserContext` 字符串，包含原始问题和用户回答
- 注入到 system prompt 末尾，告知 LLM "用户正在回答之前的确认问题，不是新请求"
- 有 pending ask_user 时跳过 topic switch detection，避免误判为新话题
- 有 pending ask_user 时跳过 short chit-chat interception，避免 "ok"/"好的" 等短回复被当作闲聊拦截

#### 4. 状态清理（`gui/im_message_handler.go` + `gui/im_message_handler_workflow.go`）

- `handleExitCommand`、`cancelWorkflowForUser`、`StartNewTask`、topic auto-clear 路径中清除 pending 状态
- `/new`、`/reset`、`/clear` 命令处理中清除 pending 状态
- workflow engine 拦截消息并返回响应时清除 pending 状态（防止 confirmWords 匹配后 pending 泄漏）
- 确保对话重置时不残留 stale 状态

**验收标准**：
- 需求确认阶段用户输入 `c++ cmake` → LLM 理解为"用 C++ + CMake 开发"的补充需求，更新需求文档
- 需求确认阶段用户点击"确认需求"按钮 → 行为不变
- 用户发送 /new 或切换话题 → pending 状态被清除，不影响后续对话
- 多用户并发 → pending 状态按 userID 隔离


### 24. Coding Tool Gate 仅在 iteration 0 生效——需求文档显示后直接编码

**根因**：`gui/im_message_handler.go` 中的 Coding Tool Gate 仅在 `iteration == 0` 时检查并拦截编码工具调用。在同一个 agent loop 中，LLM 的执行流程为：

1. iteration 0：LLM 尝试调用 `create_session` → gate 拦截 → 注入系统消息"请先生成需求文档"→ `continue`
2. iteration 1：LLM 生成需求文档文本（无工具调用）→ 进入 no-tool 分支
3. iteration 2+：LLM 调用 `generate_pdf`（允许）+ `create_session`（编码工具）→ **gate 不检查**（`iteration != 0`）→ 编码工具直接执行

用户看到的现象：需求文档显示完毕后，LLM 没有等待确认就直接开始写代码，完全跳过了技术设计和任务分解阶段。

同时，NeedsConfirm gate 仅在 `workflowEngine.IsPhaseNeedsConfirm()` 返回 true 时生效。当使用 steering 规则驱动的编码工作流（无 WorkflowEngine 活跃工作流）时，NeedsConfirm gate 不触发，无法阻止 stall detector 强制继续循环。

**修复**：

#### 1. Coding Tool Gate 扩展到所有迭代（`gui/im_message_handler.go`）

- 移除 `iteration == 0` 限制，gate 在整个 agent loop 的所有迭代中生效
- 每个用户消息触发一个新的 agent loop，gate 在新 loop 中重新评估（用户说"确认"时 intent 不再是 coding，gate 自然不激活）
- iteration 0 且无文本时：注入系统消息提示生成需求文档（行为不变）
- iteration 1+ 且有文本时：编码工具被拦截后 force-return 响应给用户确认，不再继续循环
- iteration 1+ 且无文本时：注入提醒消息并 continue

#### 2. Steering-based NeedsConfirm gate（`gui/im_message_handler.go`）

- 在 no-tool 分支的 NeedsConfirm gate 中，新增 steering 来源判断
- 当 `gateConfig.active`（intentCoding + 无 skip signal）且 `iteration > 0` 时，视为 NeedsConfirm 状态
- LLM 产出实质性文本（需求文档）后立即 force-return，防止 stall detector 强制继续循环
- 与 WorkflowEngine 的 NeedsConfirm 取并集（任一为 true 即触发）

#### 3. 注释更新（`gui/coding_tool_gate.go`）

- 更新模块注释，说明 gate 在所有迭代中生效而非仅 iteration 0

**验收标准**：
- 用户说"开发贪吃蛇游戏" → LLM 生成需求文档 → 响应返回给用户等待确认 → 不自动开始编码
- 用户说"确认" → 新 agent loop → intent 不匹配 coding（或匹配但进入设计阶段）→ gate 不拦截
- 用户说"直接做" → skip signal 检测到 → gate 不激活 → 直接编码
- generate_pdf、send_file 等交付工具在所有迭代中不受影响


### 25. AI 助手面板编码工作流：文档直接 Markdown 预览 + 全屏提示

**根因**：系统 prompt 不区分桌面 AI 助手面板和 IM 通道，统一要求 LLM 使用 `generate_pdf` 生成 PDF 文档。但桌面面板已有右侧 Markdown 预览区（`WorkflowDocPreview`），不需要 PDF。同时 `suggest_maximize` 事件在第一个工具调用时才发出，但桌面模式下 LLM 可能直接输出文本不调用工具，导致全屏提示不出现。

**修复**：

#### 1. 桌面面板文档交付方式覆盖（`gui/im_system_prompt.go`）

- 新增 `desktopWorkflowDocOverride()` 函数：返回桌面专用的系统 prompt 覆盖段
- 指示 LLM 不使用 `generate_pdf`，直接输出 Markdown 文本
- 系统自动将文本显示在右侧预览面板

#### 2. 平台感知注入（`gui/im_message_handler.go`）

- 两处系统 prompt 构建点（queued 路径和 streaming 路径）在 `msg.Platform == "desktop"` 时追加覆盖段
- IM 通道（飞书/微信/QQ/Telegram）行为不变，仍使用 PDF

#### 3. 文本输出拦截（`gui/im_message_handler.go`）

- `SteeringWorkflowDetector` 新增 `interceptTextOutput()` 方法：检测 LLM 纯文本输出中的工作流文档
- 在 NeedsConfirm gate 的 force-return 路径中，桌面模式下调用此方法
- 匹配到需求/设计/任务文档时，发出 `workflow:doc_update` 事件到前端预览面板

#### 4. 提前发出全屏建议（`gui/im_message_handler.go`）

- 桌面模式下，`SteeringWorkflowDetector` 激活时立即发出 `workflow:suggest_maximize`
- 不再等到第一个工具调用，确保用户在 LLM 开始生成文档前就看到全屏提示

#### 5. Steering 规则更新（`.kiro/steering/coding-workflow.md`）

- 三个阶段的文档交付方式改为：桌面面板直接输出 Markdown，IM 通道使用 generate_pdf
- generate_pdf 使用限制按通道类型区分


### 26. 意图理解 LLM 调用超时 + 错误降级

**根因**：`llmIntentTimeout = 10 * time.Second` 对第三方 API 服务商（如智谱 GLM `open.bigmodel.cn`）过短。智谱 API 响应延迟比 Anthropic 官方高，10 秒内未返回导致 `context deadline exceeded`。用户说"开工"时，意图理解 LLM 调用超时，直接返回原始错误信息"意图理解出错: intent understanding LLM call: Post ... context deadline exceeded"，用户体验差。

**修复**：
- `corelib/workflow/intent_understanding.go`：`llmIntentTimeout` 从 10s 提升到 30s，适配第三方 API 服务商的较高延迟
- `gui/im_message_handler_workflow.go`：`handleActiveUnderstanding()` 中 LLM 调用失败时，不再返回错误给用户，而是清理 understanding session 并返回 nil（fall through 到正常 agent loop），让用户的消息被正常处理
- `tui/agent_handler_workflow.go`：同步修复，LLM 调用失败时 fall through 到正常 agent loop

**原理**：意图理解是辅助功能，不应因 LLM 超时而阻断用户操作。超时时降级到正常 agent loop，用户的"开工"消息仍能被正常处理。


### 27. 编码工作流中 Browser 工具泄漏导致 LLM 输出 "Browser:" 前缀

**根因**：编码工作流三阶段流程（需求→设计→任务分解）中，`codingToolBlocklist` 仅包含 7 个编码会话工具（`create_session`、`bash`、`write_file` 等），不包含 25+ 个 `browser_*` 浏览器自动化工具。当用户的游戏开发需求描述中包含"页面"+"点击"等词时，`conditionalKeepRules` 的 browser 规则匹配，所有 browser 工具被 eager pin 到 session。Coding Tool Gate 在 tool call 层面拦截编码工具，但 browser 工具不在 blocklist 中，直接通过。更关键的是，25+ 个 browser 工具定义始终存在于发送给 LLM 的工具列表中，占据大量 context token，导致 LLM 混淆自身角色，在输出中产生 `Browser:` 前缀的幻觉文本。

**修复**：

#### 1. 扩展 `codingToolBlocklist`（`gui/coding_tool_gate.go`）

- 将所有 `browser_*` 工具（25 个）和 `gui_record_start`/`gui_record_stop` 加入 blocklist
- 在编码工作流阶段，这些工具与编码无关，应被拦截
- `deliveryToolAllowlist` 新增 `ask_user`（修复已有测试期望）

#### 2. 工具定义层面过滤（`gui/im_message_handler.go`）

- 在 `gateConfig` 计算后、迭代循环开始前，新增工具定义过滤逻辑
- 当 gate active 时，从发送给 LLM 的 `tools` 列表中移除 blocklist 中的工具定义
- 这不仅防止 LLM 调用这些工具，更重要的是防止 25+ 个 browser 工具定义污染 LLM context
- 同步更新 `toolsTokenBudget`

#### 3. 测试更新（`gui/coding_tool_gate_test.go`）

- `TestCodingGate_BlocklistContainsAllCodingTools` 更新为验证编码工具 + 浏览器工具的完整列表

**验收标准**：
- 编码工作流三阶段中，LLM 不再输出 `Browser:` 前缀
- browser 工具定义不出现在编码工作流的 LLM 工具列表中
- 编码工作流结束后（用户确认任务列表），新的 agent loop 重新评估 gate，browser 工具恢复可用
- 所有现有 coding tool gate 测试和 property test 通过


### 28. 工作流引擎统一阶段流转机制（全模板覆盖）

**来源**：PRD 设计工作流进入后，右侧面板显示 implementation 阶段的质量门禁，且 PRD 一步就完成了，没有按阶段流转。

#### 根因分析

编程工作流有三层保护机制保证阶段流转：
1. **WorkflowEngine**：`HandleInput` 检查 `NeedsConfirm` + `hasOutput`，用户确认后才 `advancePhase`
2. **Coding Tool Gate**：`gateConfig.active` 拦截编码工具，`needsConfirmFromSteering` 强制返回等待确认
3. **SteeringWorkflowDetector**：`matchPhaseID` 识别文档类型，`interceptTextOutput` 发射 doc preview 事件

但其它 13 个模板（产品设计、创新、商业计划等）只有第 1 层保护。第 2、3 层是编码专用的，导致：
- NeedsConfirm gate 的 tool 分支只在 `gateConfig.active`（编码意图）时触发
- doc preview 发射依赖 `steeringDetector`（仅编码工作流激活）
- `SavePhaseOutput` 用 `PhaseIndex` 索引模板，与 `CurrentPhase` ID 不同步时取到错误阶段的质量门禁
- `EmitSuggestMaximize` 硬编码 `"coding"` 类型
- 前端 `phaseLabels` 只映射了编码工作流的 5 个 phase ID

#### 修复：统一引擎模式

**设计原则**：所有工作流模板共享同一套引擎机制，模板只需声明 phases 数据，不需要额外的 gate/detector 代码。

##### 1. 质量门禁按 phase ID 查找（`corelib/workflow/engine.go`）

`SavePhaseOutput` 从 `tmpl.Phases[ws.PhaseIndex]` 改为按 `phaseID` 遍历查找：
```go
for i := range tmpl.Phases {
    if tmpl.Phases[i].ID == phaseID {
        phase = &tmpl.Phases[i]
        break
    }
}
```
确保质量门禁始终匹配当前阶段的 checklist，不受 PhaseIndex 漂移影响。

##### 2. NeedsConfirm gate 统一（`gui/im_message_handler.go`）

**no-tool 分支**（已有）：`needsConfirmFromEngine || needsConfirmFromSteering` — engine 路径覆盖所有模板。

**tool 分支**（新增）：从仅 `gateConfig.active` 扩展为：
```go
needsConfirmToolBranch := gateConfig.active && iteration > 0
if !needsConfirmToolBranch && iteration > 0 {
    needsConfirmToolBranch = workflowEngine.IsPhaseNeedsConfirm(userID)
}
```
所有模板的 NeedsConfirm 阶段在 tool 执行后都会强制返回等待确认。

##### 3. Doc Preview 双路径发射（`gui/im_message_handler.go`）

NeedsConfirm gate 触发时的 doc preview 发射改为双路径：
- **Path 1**：`steeringDetector.interceptTextOutput`（编码工作流，无 WorkflowEngine）
- **Path 2**：`workflowEngine.GetActiveWorkflow(userID).CurrentPhase`（所有 WorkflowEngine 工作流）

Path 2 直接使用引擎的当前 phase ID，不依赖文本内容匹配，对所有模板通用。

##### 4. EmitSuggestMaximize 动态类型（`gui/im_message_handler_workflow.go`）

- `handleActiveWorkflow`：从硬编码 `"coding"` 改为 `string(ws.Type)`
- `handleNeedsUnderstanding`：改为 `"workflow"`（此时类型未确定）

##### 5. 前端 phaseLabels 全覆盖（`WorkflowDocPreview.tsx`）

从 5 个映射扩展到约 70 个，覆盖全部 19 个模板的所有 phase ID。

#### 统一引擎模式总结

**阶段流转链路**（适用于所有 19 个模板）：

```
用户消息
  → QuickFilter.Classify → FilterNeedsUnderstanding
  → IntentUnderstandingManager.Start/HandleInput（LLM 分类 category）
  → WorkflowEngine.StartWorkflow（创建 WorkflowState，phase=0）
  → 用户确认 "开工"
  → WorkflowEngine.HandleInput → RunAgentLoop=true + PhasePrompt
  → Agent Loop:
      1. System Prompt 注入 PhasePrompt（BuildPhasePrompt）
      2. Tool 过滤（applyWorkflowToolFilter → doc_only/full）
      3. LLM 生成阶段文档
      4. NeedsConfirm gate 拦截 → force-return
      5. SavePhaseOutput → RunQualityGate（按 phase ID 查找）
      6. EmitDocUpdate → 前端 doc preview 面板
      7. EmitGateResult → 前端质量门禁横幅
  → 用户确认/修改/跳过
  → WorkflowEngine.HandleInput → advancePhase → 下一阶段
  → 重复直到所有阶段完成
```

**新增模板扩展清单**（只需做这些）：

1. `corelib/workflow/templates.go`：定义 `xxxTemplate()` 函数，声明 Phases 数组
2. `corelib/workflow/types.go`：新增 `WorkflowXxx WorkflowType = "xxx"` 常量
3. `corelib/workflow/templates.go`：`RegisterBuiltinTemplates` 中注册
4. `gui/frontend/.../WorkflowDocPreview.tsx`：`phaseLabels` 中添加 phase ID → 中文名映射

**不需要**：
- 不需要写 gate 代码（引擎统一处理）
- 不需要写 detector 代码（引擎直接用 CurrentPhase）
- 不需要改 agent loop（NeedsConfirm gate 自动覆盖）
- 不需要改 tool 过滤（ToolPolicy 声明式生效）

#### 19 个模板阶段流转矩阵

| 模板 | 阶段数 | ToolFilterFull 阶段 | CanSkip 阶段 | NeedsConfirm=false 阶段 |
|------|--------|-------------------|-------------|----------------------|
| coding | 5 | implementation | task_breakdown, review | implementation |
| product_design | 4 | 无 | prototype | 无 |
| innovation | 5 | 无 | roadmap | 无 |
| business_plan | 5 | 无 | operations | 无 |
| testing | 5 | test_execution | test_environment, defect_report | test_execution |
| literature_review | 5 | 无 | 无 | 无 |
| research_report | 5 | 无 | source_mapping | 无 |
| experiment_design | 5 | 无 | 无 | 无 |
| grant_proposal | 5 | 无 | 无 | 无 |
| paper_writing | 5 | 无 | 无 | 无 |
| project_proposal | 5 | 无 | 无 | 无 |
| event_planning | 5 | 无 | 无 | 无 |
| competitive_analysis | 5 | 无 | 无 | 无 |
| presentation_design | 5 | ppt_generation | 无 | ppt_generation |
| bid_response | 5 | 无 | 无 | 无 |
| contract_review | 5 | 无 | 无 | 无 |
| due_diligence | 5 | 无 | 无 | 无 |
| compliance_audit | 5 | 无 | 无 | 无 |
| patent_analysis | 5 | 无 | 无 | 无 |

**规律**：
- 只有 coding、testing、presentation_design 有 `ToolFilterFull` 阶段（需要执行代码/生成文件）
- `NeedsConfirm=false` 的阶段都是 `ToolFilterFull`（执行阶段不需要确认，直接干活）
- 纯文档类模板（literature_review、grant_proposal 等）所有阶段都是 `doc_only + NeedsConfirm=true`
- `CanSkip=true` 的阶段是可选的辅助阶段（原型设计、路线图、测试环境等）
- bid_response、contract_review、due_diligence、compliance_audit、patent_analysis 是输入驱动型模板，第一阶段需要用户提供外部材料（文件/文本/网址），使用统一的 `inputGuidance` 引导

**验收标准**：
- 产品设计工作流：进入后从"问题发现"阶段开始，右侧面板显示正确的阶段名和质量门禁
- 所有 19 个模板：NeedsConfirm 阶段在 LLM 输出文档后强制返回等待确认
- 所有 19 个模板：doc preview 面板正确显示当前阶段文档和质量门禁
- 新增模板只需 4 步（template + type + register + phaseLabel）


### 29. 工作流意图检测：从关键词堆砌到三层语义保底

**来源**：用户输入"生成 网络安全产品的prd文档"被分类为 `ambiguous`，没有进入工作流引擎。根因是 `isSimpleDirective` 中的 `"生成"` 前缀过早拦截，且 `isComplexTask` 的三要素检测（verb + target + constraint）不覆盖文档类任务。

#### 问题本质

之前的做法是关键词堆砌——每发现一个漏网的模板就往 `workflowDocKeywords` 列表里加关键词。这是头痛医头，不可扩展。

#### 修复：三层语义保底架构

##### Layer 1: 关键词规则（<1ms）
- `isSmallTalk` — 闲聊拦截
- `isSimpleDirective` — 简单指令拦截
- `isComplexTask` — verb + target + constraint 三要素 + codingTargets 快捷路径
- **不变**，保持快速拦截能力

##### Layer 1.5: Registry 模板关键词匹配（<1ms，新增）

**`WorkflowRegistry.MatchesAnyTemplate(text)`** — 遍历所有已注册模板的 `Keywords` 字段，两层评分：
- 强关键词（≥3 中文字符如"商业计划"、"竞品分析"，或大写缩写如 PRD/PPT/SWOT）：单个命中即匹配
- 弱关键词（短通用词如"产品"、"设计"）：同一模板需要 ≥2 个命中才匹配

在 `isSimpleDirective` 和 `isComplexTask` 中都会检查。数据来源是模板的 `Keywords` 字段，新增模板自动生效，不需要改分类代码。

删除了整个 `workflowDocKeywords` 硬编码列表。

##### Layer 2: BM25 语义检索（<5ms，新增）

**`WorkflowRegistry.BestTemplateScore(text)`** — 用 gse 中文分词 + BM25 对用户消息和所有模板的 `Name + Description + Keywords` 做相关性评分。

- 懒加载 BM25 索引，模板变更时自动重建
- 阈值 2.0（保守），典型强匹配 3-6 分，无关文本 <0.5 分
- 只在 Layer 1 和 1.5 都没命中时触发（最后的 `FilterSimpleDirective` 之前）
- 短消息（<8 字符）跳过

##### Layer 3: LLM 意图理解（已有，~10-30s）

`IntentUnderstandingManager` — 当 Layer 1/1.5/2 返回 `FilterNeedsUnderstanding` 后，LLM 多轮对话确认意图和工作流类型。最终的语义保底。

#### 架构接口设计

```go
// QuickFilter 通过接口依赖 registry，不直接耦合模板数据
type TemplateKeywordMatcher interface {
    MatchesAnyTemplate(text string) bool
}
type TemplateBM25Matcher interface {
    BestTemplateScore(text string) float64
}

// WorkflowRegistry 同时实现两个接口
// 引擎构造时自动接线：
e.filter.SetRegistry(registry)  // Layer 1.5
e.filter.SetBM25(registry)      // Layer 2
```

#### 修改文件

- `corelib/workflow/quick_filter.go`：`QuickFilter` 新增 `registry` 和 `bm25` 字段；`isSimpleDirective` 和 `isComplexTask` 改为方法，通过接口调用 registry；删除 `workflowDocKeywords` 硬编码列表；`Classify` 末尾新增 BM25 fallback 检查
- `corelib/workflow/registry.go`：新增 `MatchesAnyTemplate`（两层评分）和 `BestTemplateScore`（BM25 索引）；`Register` 时标记 `bm25Dirty`；懒加载 `rebuildBM25Locked`
- `corelib/workflow/engine.go`：构造时 `filter.SetRegistry(registry)` + `filter.SetBM25(registry)`

#### 扩展方式

在 `templates.go` 中定义新模板时声明 `Keywords` 字段即可。三层检测全部自动生效：
- Layer 1.5 自动匹配新模板的 Keywords
- Layer 2 自动将新模板加入 BM25 索引
- Layer 3 的 LLM system prompt 已包含所有模板描述（`AllDescriptions()`）

不需要改 `quick_filter.go`、不需要维护关键词列表、不需要改分类逻辑。

#### 未来演进路径

当模板数量增长到 100+ 时，可在 `BestTemplateScore` 中加入 embedding cosine similarity 作为第二信号，用已有的 `HybridRetriever.FuseScores`（BM25 + 向量融合）替代纯 BM25。基础设施已就绪（`corelib/tool/hybrid.go`），只需接线。


### 29. 输入驱动型工作流模板（招投标 + 合同审查 + 尽职调查 + 合规审计 + 专利分析）

**来源**：用户需求——添加一批需要用户提供输入材料（文件/文本/网址）的工作流模板。

#### 共同特征：输入驱动型（Input-Driven）

与纯创作型模板不同，这 5 个模板的第一阶段都需要用户提供外部材料作为分析输入。Prompt 统一支持三种输入方式：
- **上传文件**（PDF/Word/图片）
- **粘贴文本**
- **提供网址**（LLM 使用 web_fetch 工具抓取内容，失败则提示用户换方式）

提取了 `inputGuidance` 常量统一输入引导文本，各模板第一阶段 Prompt 拼接此常量。

#### 实现

- `corelib/workflow/types.go`：新增 5 个 WorkflowType 常量（`bid_response`/`contract_review`/`due_diligence`/`compliance_audit`/`patent_analysis`）
- `corelib/workflow/templates.go`：
  - 新增 `inputGuidance` 常量（统一输入引导文本）
  - 新增 5 个模板函数，注册到 `RegisterBuiltinTemplates`
  - `bidResponseTemplate` 第一阶段 Prompt 更新为使用统一输入引导
- `gui/frontend/src/components/ai/WorkflowDocPreview.tsx`：新增 25 个 phaseLabels 映射（5 模板 × 5 阶段）

#### 5 个模板概览

| 模板 | Type | 阶段 | 核心场景 |
|------|------|------|---------|
| 招投标文件生成 | `bid_response` | 招标解析→资质响应→技术方案→商务报价→文件组装 | 理解发标文件，生成投标响应 |
| 合同审查 | `contract_review` | 合同解析→条款风险→合规审查→修改建议→审查意见 | 审查合同条款风险和合规性 |
| 尽职调查 | `due_diligence` | 公司画像→商业尽调→财务尽调→法律尽调→尽调结论 | 投资/并购前的系统性调查 |
| 合规审计 | `compliance_audit` | 审计范围→合规评估→风险评级→整改计划→审计报告 | 企业合规性审查和整改 |
| 专利分析 | `patent_analysis` | 技术解析→现有技术→侵权评估→策略建议→分析报告 | 专利检索、侵权分析、布局策略 |

所有模板均为 `ToolFilterDocOnly + NeedsConfirm=true`（纯文档类），无 CanSkip 阶段。
合同审查、合规审计、专利分析的最终报告均包含 AI 免责声明。

#### 输入等待机制（RequiresInput）

5 个输入驱动型模板均声明 `RequiresInput`，引擎在 `HandleInput` 层面拦截：
- 工作流启动后，引擎检测 `IsWaitingForInput` → 提示用户上传文件/粘贴文本/提供网址
- 用户发送内容后，`isSubstantialInput` 判断是否为实质性输入（≥50 字符 / 含文件扩展名 / 含 URL / 含上传关键词）
- 判定为实质性输入 → `InputReceived=true` → 放行第一阶段执行
- 判定为非实质性输入（如"好的"、"开始"）→ 继续等待，重新提示上传

输入引导统一由引擎的 `prompt_builder.go` 注入，模板 Prompt 中不重复写输入引导文字。`AnalysisHint` 字段在收到文档后注入，指导 LLM 分析重点。

#### `isSubstantialInput` URL 识别增强

原函数不识别 URL（短于 50 字符的 `https://...` 会被误判为非实质性输入）。新增：
- `http://` / `https://` 前缀检测
- "网址"、"链接" 关键词检测

#### 提示文本统一增加网址支持

引擎提示、启动消息、prompt_builder 的输入引导文本统一从"粘贴文档内容"扩展为"粘贴文档内容，或提供网址由系统自动抓取"。


### 30. 文档预览面板阶段标签错误——任务分解阶段显示"设计"

**根因**：`gui/workflow_adapter.go` 的 `detectPhaseFromContent()` 函数扫描文档前 500 字符做内容匹配，作为"最终安全网"纠正上游可能传错的 phaseID。但任务分解文档的前 500 字符中通常包含"基于已确认的需求和**技术设计**"等对前一阶段的引用。由于 switch 语句中 `"design"` case（匹配 `"技术设计"`）排在 `"tasks"` case（匹配 `"任务列表"`）之前，导致任务分解文档被错误识别为设计文档。

**触发路径**：
1. 引擎 `SavePhaseOutput` 返回 `"task_breakdown"`（正确）
2. `EmitDocUpdate` 规范化为 `"tasks"`（正确）
3. `detectPhaseFromContent` 扫描前 500 字符，命中 `"技术设计"` → 返回 `"design"`
4. 安全网判断 `"design" != "tasks"` → 覆盖为 `"design"`（错误）
5. 前端收到 `phase_id: "design"` → 显示"设计"标签

**修复**：
- `gui/workflow_adapter.go`：`detectPhaseFromContent()` 重写为两遍检测：
  - Pass 1：提取文档的第一个 Markdown 标题行（`# ...`），仅在标题中做关键词匹配。标题是文档自身的名称，不会包含对其他阶段的引用，置信度最高
  - Pass 2：无标题时回退到前 300 字符扫描，但任务关键词优先于设计关键词检查，且设计匹配使用更严格的模式（`"技术设计文档"` 而非 `"技术设计"`）
  - 新增 `extractFirstHeading()` 辅助函数：从前 1000 字符中提取第一个 `#` 开头的标题文本
- `gui/im_message_handler.go`：`SteeringWorkflowDetector.matchPhaseID()` 调整 switch 顺序，任务关键词检查移到设计关键词之前，避免 `interceptTextOutput` 路径出现同样的误判

**验收标准**：
- 任务分解阶段：文档预览面板标签显示"任务"而非"设计"
- 需求/设计阶段：标签显示不受影响
- 文档内容中引用其他阶段名称时不触发误纠正


### 31. Coding Tool Gate 误拦截 Bug 修复任务——AI 助手中途停止工作

**根因**：Coding Tool Gate 的 `classifyTaskIntent()` 使用 `codingKeywords` 做关键词匹配，其中包含 `"bug"`、`"修复"`、`"代码"` 等词。当用户在编码任务执行过程中报告 bug（如"有bug，一直显示加载中"），gate 将其分类为 `intentCoding` 并激活三阶段流程拦截。LLM 第一轮输出分析文本（"让我检查控制台错误..."），第二轮尝试调用 `bash`/`create_session` 实际检查文件时被 gate 拦截，`iteration > 0 && msgContent != ""` 条件触发 force-return，把分析文本返回给用户等待确认。用户看到的现象是：AI 说了要做什么，然后就停了。

**核心矛盾**：Gate 的设计目的是强制新项目走三阶段流程（需求→设计→任务分解），但它把所有包含 bug/修复/调试关键词的消息都当作"新编码任务"来拦截。Bug 修复、调试、排查这类任务不需要三阶段流程，应该直接执行。

**修复**：
- `gui/coding_tool_gate.go`：
  - `codingToolGateConfig` 新增 `bugFix bool` 字段
  - 新增 `bugFixKeywords` map：修bug/修复/调试/排查/报错/崩溃/白屏/闪退/卡住/不显示等
  - 新增 `creationCodingKeywords` 列表：开发/游戏/前端/后端/写代码等创建类关键词
  - 新增 `isBugFixOnly()` 函数：当消息只匹配修复类关键词、不匹配创建类关键词时返回 true
  - `newCodingToolGateConfigWithClassifier()` 新增 bugfix 检测：`isBugFixOnly` 返回 true 时 gate 不激活
- `.kiro/steering/coding-workflow.md`：例外情况新增"Bug 修复/调试任务（自动跳过三阶段）"说明

**判断逻辑**：
- "有bug，一直显示加载中" → bugFixKeywords 命中 "bug"+"加载中"，无 creationCodingKeywords → `isBugFixOnly=true` → gate 不激活 → LLM 正常执行修复
- "开发一个bug追踪系统" → bugFixKeywords 命中 "bug"，但 creationCodingKeywords 也命中 "开发" → `isBugFixOnly=false` → gate 正常激活
- "帮我写代码" → 无 bugFixKeywords 命中 → `isBugFixOnly=false` → gate 正常激活

**验收标准**：
- 用户报告"有bug，一直显示加载中" → AI 助手正常执行修复，不中途停止
- 用户说"修复加载错误"/"调试崩溃"/"排查报错" → 直接执行，不走三阶段
- 用户说"开发一个贪吃蛇游戏" → 仍然走三阶段流程
- 用户说"开发一个bug追踪系统" → 仍然走三阶段流程（有创建类关键词）
- 所有 20 个 coding tool gate 测试通过（含 5 个新增 bug-fix bypass 测试）


### 32. AI 助手面板输入框卡死——capabilityGapDetector 同步阻塞改异步

**根因**：用户说"开工"后，LLM 在 agent loop 中积极工作（编辑文件、运行 bash、测试等），经过 35+ 次迭代后产出一段长文本总结，要求用户选择下一步操作。但此时：

1. **capabilityGapDetector 同步阻塞**：LLM 的总结文本中包含"无法"、"不支持"等词（如"Runner 的状态接口只返回元信息"），`Detect()` 将其判定为能力缺口，**同步**触发额外的 LLM 调用（`llmDetectGap`）和 Skill 搜索/安装流程，全部在返回响应给用户之前执行
2. 整个 `SearchAndInstall` + `installAndExecuteSkill` 链路可能耗时 10-30 秒，期间前端输入框一直锁定显示"正在思考..."

**触发路径**：
1. 用户发送"开工" → gate inactive（skip signal）→ LLM 开始工作
2. LLM 调用 edit_file/bash/write_file/manage_skill 等工具 30+ 次
3. LLM 最终输出长文本总结（含"无法"等信息性用词）
4. no-tool 分支 → `capabilityGapDetector.Detect(msgContent)` → 匹配"无法" → true
5. **同步** `llmDetectGap()` + `SearchAndInstall` + skill 安装 → 阻塞返回
6. 用户等待 6+ 分钟，输入框一直显示"正在思考..."

**修复**：

#### 1. `Detect()` 长响应短路（`gui/capability_gap_detector.go`）
- 响应文本超过 500 字符（rune 计数）时直接返回 false
- 长文本是详细总结/报告，不是能力缺口信号

#### 2. 能力缺口检测改为异步（`gui/im_message_handler.go`）
- 原同步阻塞的 `capabilityGapDetector.Detect()` + `SearchAndInstall` + `installAndExecuteSkill` 改为 goroutine 异步执行
- 响应立即返回给用户，输入框解锁
- 异步结果存入 `pendingCapabilityGap sync.Map`（keyed by userID）
- 新增 `pendingCapabilityGapResult` 类型：SkillName/Result/Success/Timestamp

#### 3. 异步结果注入下一轮对话（`gui/im_message_handler.go`）
- 下一条用户消息处理时，通过 `LoadAndDelete` 消费 `pendingCapabilityGap`
- 成功安装的 Skill 信息注入 system prompt：`[系统通知] 上一轮对话后，系统在后台自动搜索并安装了 Skill「xxx」`
- 10 分钟过期自动丢弃
- 失败结果仅记录日志，不注入 prompt

#### 4. 前端 Toast 通知
- Skill 安装成功时通过 `emitEvent("skill-auto-installed", ...)` 通知前端
- 用户在输入框解锁后可以看到 toast 提示"已自动安装 Skill xxx"

#### 5. 状态清理
- `/new`、`/reset`、`/clear`、`StartNewTask`、`handleExitCommand` 路径中清除 `pendingCapabilityGap`

#### 6. 仍保留短路优化
- `iteration >= 3`（LLM 已积极工作多轮）或响应超过 500 字符时，跳过能力缺口检测（连异步都不触发）

**验收标准**：
- LLM 工作 30+ 轮后输出包含"无法"的总结文本 → 响应立即返回，输入框解锁
- 短文本"我无法完成这个任务" → 异步触发能力缺口检测，输入框仍立即解锁
- 异步安装成功 → 下一轮对话 LLM 知道新 Skill 可用
- 所有现有 CapabilityGapDetector / CodingGate 测试通过


### 33. 技术设计确认后重复输出技术设计文档——前序阶段产出物未截断

**根因**：`BuildPhaseSystemPrompt()`（`corelib/workflow/prompt_builder.go`）在构建阶段系统 prompt 时，将所有前序阶段的**完整产出物原文**注入到 "前序阶段产出物" section 中。当用户确认技术设计后进入任务拆分阶段时，system prompt 包含：
1. 需求文档全文（可能数千字）
2. 技术设计文档全文（可能数千字）
3. 任务拆分阶段指令

同时，对话历史中也包含技术设计文档（作为上一轮 assistant 的回复）。技术设计内容在 LLM context 中出现**两次**（system prompt + 对话历史），形成压倒性信号，导致 LLM 在生成任务拆分文档时被"带偏"，从技术设计的结论部分继续生成，然后重新开始输出完整的技术设计文档。

**修复**：
- `corelib/workflow/prompt_builder.go`：
  - `BuildPhaseSystemPrompt()` 的前序阶段产出物从**全文注入**改为**截断摘要**
  - 紧邻的前一阶段（最相关上下文）：截断到 1200 rune
  - 更早的阶段：截断到 600 rune
  - 新增 `truncateRunesSmart()` 函数：优先在段落边界（`\n\n`）或行边界（`\n`）处截断，避免截断在句子中间
  - section 标题改为"前序阶段产出物（摘要，完整内容已在对话历史中）"，让 LLM 知道这只是摘要
  - 每个前序阶段标题标注"（摘要）"

**设计考量**：
- `SavePhaseOutput()` 仍存储完整内容（用于持久化到 `.maclaw/workflow/` 目录和质量门禁检查），截断只影响注入到 LLM system prompt 的部分
- 截断预算与 `TaskExecutionOrchestrator` 一致（`RequirementsContext`/`DesignContext` 截断到 500 字符）
- 对话历史中仍包含完整的前序阶段文档，LLM 在需要时可以参考

**验收标准**：
- 用户确认技术设计后 → LLM 生成任务拆分文档，不再重复技术设计内容
- 前序阶段摘要包含足够的上下文信息（架构、技术选型等关键决策）
- 所有 19 个工作流模板的阶段流转不受影响
- `TestProperty8_BuildPhasePromptStructuralCompleteness` 等现有测试通过


### 34. Browser 工具误激活——关键词匹配改为语义确认

**根因**：`conditionalKeepRules` 中 browser 规则使用 `browserPageKeywords`（"页面"、"网页"等）+ `browserActionKeywords`（"打开"、"点击"等）的组合关键词匹配。当用户说"开发一个打飞机游戏，浏览器直接打开即玩，页面上有飞机和子弹"时，"打开"+"页面"命中 browser 规则，25+ 个 browser 工具定义被加入 LLM context。LLM 看到大量 browser 工具定义后产生角色混淆，在需求文档输出后生成带 `Browser:` 前缀的幻觉文本。

同时，`Route()` 中的 eager pin 机制会将匹配到的 browser 工具永久 pin 到 `sessionTools`，即使是误匹配也无法撤销，后续所有 `Route()` 调用都会把 browser 工具加回来。

**修复**：

#### 1. Browser 规则拆分为强/弱两层（`corelib/tool/router.go`）

- 提取 `allBrowserToolNames` 变量，消除两条规则间的工具列表重复
- **强规则**（不需要语义确认）：用户明确提到 "浏览器"/"browser"/"chrome"/"playwright"/"录制"/"回放" 等直接关键词 → 直接激活 browser 工具
- **弱规则**（需要语义确认）：仅 `browserPageKeywords` + `browserActionKeywords` 组合匹配 → 标记为 `needsSemanticConfirm: true`，需要 `IntentClassifier` 确认为 `IntentBrowser` 才激活
- 弱规则的 `matches` 函数先检查强关键词是否已匹配，避免双重触发

#### 2. `conditionalKeepRule` 结构体扩展

- 新增 `needsSemanticConfirm bool` 字段：标记规则是否需要语义确认
- 新增 `confirmIntent string` 字段：语义确认所需的 Intent 常量（如 `IntentBrowser`）

#### 3. `matchConditionalKeepRules()` 返回值扩展

- 返回值从 `(keep, filterOut)` 改为 `(keep, filterOut, needsConfirm)`
- `needsConfirm` map 存储需要语义确认的工具名 → 所需 intent
- 匹配 `needsSemanticConfirm` 规则的工具不直接加入 `keep`，而是暂存在 `needsConfirm`

#### 4. `Route()` 语义确认流程

- `IntentClassifier.Classify()` 结果缓存为 `cachedICResult`，避免与下方 semantic intent enhancement 重复调用
- 对 `needsConfirm` 工具：`cachedICResult.Intent == requiredIntent && Confidence >= 0.50` 时提升到 `keep`，否则留在 `filterOut`
- 无 `IntentClassifier` 时回退到关键词匹配（向后兼容）

#### 5. Eager pin 防护（`noEagerPinTools`）

- 新增 `noEagerPinTools` map（从 `allBrowserToolNames` 自动生成）
- `Route()` 的 eager pin 循环中跳过 `noEagerPinTools` 中的工具
- Browser 工具不再被 eager pin，但成功调用后仍通过 `im_message_handler.go` 中的 `ActivateSessionTool` 正常 pin
- `noPinConditionalTools` 恢复为仅包含 `generate_pdf`（不影响成功调用后的 pin）

#### 6. `MatchConditionalTools()`（memory-driven pin）

- 对 `needsConfirm` 工具仍然放行——记忆内容是强信号，说明工具之前确实被使用过

**设计要点**：
- 强关键词（"浏览器"/"chrome"）→ 直接激活，零延迟
- 弱关键词组合（"页面"+"打开"）→ 语义确认，~30ms 延迟（embedding 层）
- 无 embedding 模型时 → 回退到关键词匹配（向后兼容）
- Browser 工具不被 eager pin → 即使误匹配也不会污染后续 session
- 成功调用后正常 pin → 用户确实在用浏览器时，后续消息保持可用

**验收标准**：
- "开发打飞机游戏，页面上直接打开即玩" → browser 工具不被激活，LLM 不输出 `Browser:` 前缀
- "打开浏览器帮我在网页上点击购买按钮" → browser 工具正常激活（强关键词"浏览器"）
- 无 IntentClassifier 时 → 回退到关键词匹配，行为不变
- 所有 tool 包测试通过（含 3 个新增 semantic confirm 测试）
- 已有 `TestRouter_Route_ConditionallyKeepsSSHForSSHIntent` 修复（session 重置）


### 35. NeedsConfirm 门控在执行阶段误触发——编码任务做到一半停下

**根因**：`gui/im_message_handler.go` 中 `needsConfirmFromSteering` 条件过于宽泛，在 WorkflowEngine 有活跃工作流时仍然盲目使用 `gateConfig.active && iteration > 0` 判断，不感知当前阶段的 `NeedsConfirm` 属性。

**触发链路**：
1. 用户说"开发一个打飞机游戏，C++ cmake" → `classifyTaskIntent` → `intentCoding` → `gateConfig.active = true`
2. WorkflowEngine 启动编码工作流，经过需求→设计→任务分解三阶段确认后，进入 implementation 阶段（`NeedsConfirm: false`）
3. LLM 在 implementation 阶段输出项目结构文本 + "开始写代码："（iteration 0 → no-tool 分支 → promiseOnlyDeliverable → continue）
4. iteration 1：LLM 继续输出文本或尝试调用工具
5. **no-tool 分支**：`needsConfirmFromSteering = gateConfig.active && (1 > 0) = true` → `isSubstantivePhaseDocument` = true（200+ rune）→ **force-return** → LLM 停了
6. **tool 分支**：`needsConfirmToolBranch = gateConfig.active && (1 > 0) = true` → 同样 force-return

**核心矛盾**：`gateConfig` 在整个 agent loop 中不变（loop 开始前一次性计算），即使 WorkflowEngine 已经从 NeedsConfirm=true 的阶段（需求/设计/任务分解）推进到 NeedsConfirm=false 的阶段（编码实现），`gateConfig.active` 仍然为 true。`needsConfirmFromSteering` 直接用 `gateConfig.active` 作为判断依据，没有查询 WorkflowEngine 的当前阶段属性。

**修复**：
- `gui/im_message_handler.go`：两处 `needsConfirmFromSteering` / `needsConfirmToolBranch` 计算逻辑改为阶段感知：
  - 当 WorkflowEngine 有活跃工作流时，委托给 `IsPhaseNeedsConfirm(userID)` 判断（implementation 阶段返回 false）
  - 仅当没有 WorkflowEngine 工作流（纯 steering 驱动的编码流程）时，才回退到 `gateConfig.active && iteration > 0`

**修改前**：
```go
needsConfirmFromSteering := gateConfig.active && iteration > 0
```

**修改后**：
```go
needsConfirmFromSteering := false
if gateConfig.active && iteration > 0 {
    if h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.GetActiveWorkflow(userID) != nil {
        needsConfirmFromSteering = h.app.workflowEngine.IsPhaseNeedsConfirm(userID)
    } else {
        needsConfirmFromSteering = true
    }
}
```

tool 分支的 `needsConfirmToolBranch` 同步修改。

**验收标准**：
- 编码工作流 implementation 阶段：LLM 输出文本后不被 NeedsConfirm gate 拦截，继续调用工具写代码
- 编码工作流 requirements/tech_design/task_breakdown 阶段：NeedsConfirm gate 正常触发，等待用户确认
- 纯 steering 驱动（无 WorkflowEngine 工作流）：行为不变，`gateConfig.active && iteration > 0` 仍然生效
- 所有 CodingGate / AgentLoop / Workflow 测试通过


### 36. PPT 文件操作被误路由到 PPT 设计工作流

**根因**：用户输入"打开桌面上任何一个ppt文件并截图"，系统触发了 presentation_design 工作流。触发链路：

1. `QuickFilter.Classify()` → `FilterNeedsUnderstanding`
2. `IntentUnderstandingManager.Start()` → LLM 正确返回 `category="none"`（rejected）
3. `handleNeedsUnderstanding()` 调用 `tryKeywordWorkflowFallback(strongOnly=true)`
4. `MatchTemplateByStrongKeyword` 找到 "PPT"（大写缩写）→ 匹配 `presentation_design`
5. 关键词 fallback 覆盖了 LLM 的正确拒绝，工作流被错误启动

核心矛盾：关键词匹配没有语义理解能力，无法区分"打开PPT文件"（文件操作）和"设计一个PPT"（创建任务）。用关键词排除规则修补是堆规则，不可扩展。

**修复**：信任 LLM 的判断，不用关键词覆盖

#### 1. 移除 LLM 拒绝后的关键词覆盖（`gui/im_message_handler_workflow.go`）

- `handleNeedsUnderstanding()`：`result.Rejected=true` 时直接 `return nil`（信任 LLM），不再调用 `tryKeywordWorkflowFallback`
- LLM 调用**失败**（超时/网络错误）时仍然使用关键词 fallback 作为降级方案
- `tryKeywordWorkflowFallback` 注释更新：明确只用于 LLM 失败的降级场景

#### 2. 增强 LLM 系统 prompt（`corelib/workflow/intent_understanding.go`）

给 LLM 更好的判断依据：
- "不需要工作流"列表新增"文件操作"类别
- "易混淆示例"新增 5 个 PPT 文件操作示例 → `category="none"`
- 新增 PPT 判断口诀

#### 3. 新增测试（`corelib/workflow/intent_understanding_file_operation_test.go`）

- `TestLLMRejection_FileOperationNotOverridden`：LLM 拒绝文件操作后不被覆盖
- `TestLLMRejection_CreationTaskAccepted`：LLM 接受创建任务后正常创建 session
- `TestSystemPrompt_FileOperationGuidance`：系统 prompt 包含文件操作判断依据
- `TestKeywordFallback_OnlyUsedOnLLMFailure`：关键词 fallback 仍然可用（降级场景）

**验收标准**：
- "打开桌面上任何一个ppt文件并截图" → LLM 返回 none → 不触发工作流 → 直接执行
- "帮我设计一个产品介绍PPT" → LLM 返回 presentation_design → 正常触发工作流
- LLM 调用超时时 → 关键词 fallback 仍然工作
- 所有 workflow 包测试通过


### 37. 微信回复重复漂移——跨轮次漂移记忆 + IM 状态消息节流

**根因**：微信中 agent loop 陷入重复漂移循环。用户看到的现象：反复收到"任务较复杂，正在耐心处理中"和"正在执行工具，请稍候"，最终触发"Agent 检测到重复漂移模式，需要人工介入"。用户确认后，又重新陷入同样的循环。

三个层面叠加导致：

1. **DriftDetector 每次新消息重置 replanCount**：`runAgentLoop` 每次都 `NewDriftDetector()`，`replanCount` 从 0 开始。用户确认后新 loop 完全忘记上一轮已经漂移过，需要重新走完整的"第一次漂移→recover→第二次漂移→人工介入"流程（6-8 轮迭代），期间不断发送状态消息。

2. **Recovery prompt 无效**：`buildDriftRecoverPrompt()` 注入的提示是通用的"请改用不同路径"，但没有指明具体是哪个工具失败、也没有禁止再次调用该工具。LLM 读完后继续调用同一个工具。

3. **IM 通道状态消息交替刷屏**：Hub 的 `progressMinInterval=10s` 节流使用精确字符串比较（`isDup`），"任务较复杂"和"正在执行工具"是不同字符串，交替发送时都通过节流，用户收到大量无意义的状态消息。

**修复**：

#### 1. 跨轮次漂移记忆（`gui/drift_detector.go` + `gui/im_message_handler.go`）

- `DriftDetector` 新增 `NewDriftDetectorWithHistory()` 构造函数：接受 `priorReplanCount` 参数，继承之前的重规划次数
- `DriftDetector` 新增 `ReplanCount()` 导出方法
- `DriftResult` 新增 `DriftedTool` 字段：记录触发漂移的工具名
- `IMMessageHandler` 新增 `sessionDriftReplanCount sync.Map` 和 `sessionDriftTool sync.Map`：按 userID 持久化漂移状态
- `runAgentLoop` 初始化 drift detector 时从 session 继承 `replanCount`
- 漂移退出（`NeedHumanHelp`）时保存 `replanCount` 和 `driftedTool` 到 session
- 下一轮 loop 启动时注入系统消息："上一轮因反复调用 {tool} 失败而停止，禁止再次使用相同方法"
- `/new`、`/reset`、`/clear`、`StartNewTask`、topic switch 等清理路径中清除 session drift 状态

#### 2. 漂移 recover prompt 具体化（`gui/drift_detector.go` + `gui/im_message_handler.go`）

- `DetectDrift()` 第一次漂移：提示包含具体工具名 + "如果没有其他可行路径，直接告诉用户当前的限制"
- `DetectDrift()` 第二次及以后漂移：更强指令——"禁止再次调用 {tool}，直接向用户说明具体问题和限制"
- `buildDriftRecoverPrompt()` 新增工具禁止警告
- 漂移退出消息从通用的"需要人工介入"改为具体的"在执行过程中反复调用 {tool} 未能成功，已停止尝试"

#### 3. Recover 阶段抑制状态消息（`gui/im_message_handler.go`）

- "任务较复杂，正在耐心处理中" 在 `phase.Stage == agentStageRecover` 时不发送
- 避免在 LLM 已经进入恢复阶段时继续给用户发送误导性的"正在处理中"消息

#### 4. IM 通道状态消息分类节流（`hub/internal/im/router.go`）

- 新增 `isIntermediateStatusProgress()` 函数：通过关键词（"正在耐心处理中"、"正在执行工具"、"请稍候"等）识别中间状态消息
- 新增 `lastDeliveredText` 变量：记录最后实际发送给用户的消息文本
- 进度节流逻辑增加分类去重：当上一条已发送的消息和当前消息都是中间状态消息时，视为同类重复，在 `progressMinInterval` 窗口内抑制

**验收标准**：
- 用户说"截屏桌面文件" → LLM 调用 screenshot 3 次失败 → 第一次漂移注入 recover prompt → 继续失败 → 第二次漂移返回具体错误消息
- 用户确认后 → 新 loop 继承 replanCount=2 → 第一次漂移即触发 NeedHumanHelp → 不再重走完整流程
- 漂移期间微信不再交替收到"正在处理中"和"正在执行工具"刷屏
- `/new` 或切换话题后 → session drift 状态清除，不影响后续对话
- 所有现有 Drift / AgentLoop / Progress 测试通过


### 38. 执行确认面板语义理解——从原文复述到结构化理解

**根因**：`buildPendingConfirmation()` 中 `summary` 字段使用 `fmt.Sprintf("我理解你想让我处理这项任务：%s", text)` 直接拼接用户原文，没有任何语义理解。用户看到的确认面板只是原文复述，零增值。同时 `confirmationApprovedText()` 确认后也只是把原始文本传给 agent loop，LLM 收到的是口语化的用户输入而非结构化指令。

**修复**：

#### 1. 新增 LLM 任务理解模块（`gui/im_task_understanding.go`）

- `taskUnderstandingResult` 结构体：TaskType/Summary/Goals/Constraints/ExecutionPlan/EnhancedInstruction
- `understandTaskWithLLM()` 方法：轻量 LLM 调用（~400 input + ~200 output token，12s 超时），让 LLM 生成结构化理解
- `parseTaskUnderstandingResponse()`：解析 LLM JSON 响应，容错 markdown 代码块包裹
- `formatTaskUnderstandingSummary()`：将结构化理解格式化为确认面板显示文本
- `formatEnhancedInstruction()`：提取增强执行指令

#### 2. `pendingConfirmation` 新增字段（`gui/im_confirmation_store.go`）

- `EnhancedSummary`：LLM 生成的结构化摘要，替代原文复述
- `EnhancedInstruction`：LLM 生成的结构化执行指令，确认后替代原始用户文本

#### 3. `buildPendingConfirmation` 增强（`gui/im_message_handler.go`）

- 新增 `understanding *taskUnderstandingResult` 参数
- 有 LLM 理解时：Summary 使用结构化摘要，PlannedActions 使用 LLM 执行计划
- 无 LLM 理解时（超时/失败/未配置）：回退到原文复述（向后兼容）

#### 4. `confirmationApprovedText` 增强（`gui/im_message_handler.go`）

- 优先使用 `EnhancedInstruction` 作为 agent loop 输入
- 为空时回退到 `ResumeText`（向后兼容）

#### 5. `applyConfirmationRevision` 增强（`gui/im_message_handler.go`）

- 用户修改确认内容时清除 `EnhancedSummary` 和 `EnhancedInstruction`
- 修改后的任务与原始 LLM 理解不一致，回退到 `ResumeText`

#### 6. 调用点更新（`gui/im_message_handler.go`）

- `shouldRequireExecutionConfirmation` 路径中，在 `buildPendingConfirmation` 前调用 `understandTaskWithLLM`
- LLM 调用失败时 `understanding` 为 nil，自动回退

**效果对比**：

之前（原文复述）：
```
我理解你想让我处理这项任务：搜索网上 美发师 vibehair的资料，包括经历，工作单位，水平，所在店名，地址等，越详细越好。
默认工作目录：📁 D:\workprj\aicoder
识别到的任务类型：ambiguous
```

之后（结构化理解）：
```
任务类型：信息搜集
任务理解：搜集美发师 vibehair 的详细个人资料
目标：
  • 查找从业经历和工作单位
  • 确认技术水平和所在门店
  • 获取门店地址信息
约束/要求：
  • 信息尽可能详细完整
执行计划：
  1. 搜索 vibehair 相关网页和社交媒体
  2. 提取个人信息和门店信息
  3. 整理成结构化报告
默认工作目录：D:\workprj\aicoder
```

**验收标准**：
- 确认面板显示结构化理解而非原文复述
- LLM 不可用时回退到原文复述，行为不变
- 用户确认后 agent loop 收到结构化执行指令
- 用户修改确认内容后清除 LLM 理解，回退到修改后文本
- 所有 6 个现有确认门控测试通过 + 15 个新增测试通过


### 39. Agent Loop 三大中途停止问题修复

**来源**：日志分析 `~/.maclaw/logs/maclaw.log` + trajectory 文件，发现三个叠加问题导致 maclaw 在任务中间停止工作。

#### 问题 1 (P0): LLM 空响应 → Recover 死循环

**根因**：glm-5.1 模型在 context 超过 ~110K token 时频繁返回空响应（`output=0 usage_nil=true`）。每次空响应触发 `buildEmptyResultRecoverPrompt()` 注入 Recover 系统消息（~133 字符），进一步膨胀 context，加剧空响应。trajectory 中观察到连续 8+ 轮空响应 + Recover 注入的死循环。

**修复**：
- `gui/im_message_handler.go`：
  - `agentLoopPhase` 新增 `ConsecutiveEmptyResponses int` 和 `TotalRecoverInjections int` 字段
  - `enterRecoverPhase()` 自动递增 `TotalRecoverInjections`
  - 新增 `maxConsecutiveEmptyResponses = 3` 常量：连续 3 次空响应后硬退出
  - 新增 `maxTotalRecoverInjections = 8` 常量：单个 agent loop 中 Recover 注入上限
  - 空响应检测处新增硬退出逻辑：达到上限时返回最后一条非空 assistant 消息，而非继续注入 Recover
  - 新增 `findLastAssistantContent()` 函数：从 `[]conversationEntry` 中反向查找最后一条有效 assistant 内容
  - 非空响应时重置 `ConsecutiveEmptyResponses` 计数器

#### 问题 2 (P1): NeedsConfirm 工作流门控在维护任务中误触发

**根因**：用户说"改进优化下这个技能？"，`GateIntentClassifier` 正确识别为 `maintenance`（conf=0.85），coding-gate 标记为 inactive。但 WorkflowEngine 的 NeedsConfirm 门控独立于 coding-gate，当之前有 `presentation_design` 工作流活跃时，`IsPhaseNeedsConfirm()` 返回 true，导致 LLM 产出超过 200 rune 的文本就被 `isSubstantivePhaseDocument()` 判定为"阶段文档"并强制返回等待确认。

**修复**：
- `gui/im_message_handler.go`：
  - **no-tool 分支**：`needsConfirmFromEngine` 计算处新增语义意图感知——当 `gateConfig.intent == intentCoding && !gateConfig.active`（maintenance/bug_fix）或 `gateConfig.bugFix` 时，跳过 engine 的 NeedsConfirm 检查
  - **tool 分支**：NeedsConfirm fallback gate 同步新增语义意图感知——当语义分类器判定为 maintenance/bug_fix 时，不激活 engine 的 NeedsConfirm 门控
  - 两处均添加 debug 日志 `[workflow-gate] NeedsConfirm ... bypassed: semantic intent=...`

#### 问题 3 (P1): write_file 大文件 JSON 截断 → 连续失败循环

**根因**：glm-5.1 模型生成超长 JSON 参数时（13K+ 字符的 `content` 字段），输出被截断导致 JSON 不完整，返回 `参数解析失败: unexpected end of JSON input`。LLM 不理解错误原因，反复重试同样大小的内容。

**修复**：
- `gui/im_tool_execution.go`：`executeTool()` 的 JSON 解析失败处，当错误为 `unexpected end of JSON input` 且 `argsJSON` 超过 8000 字符时，追加可操作的提示："请将内容拆分为多次调用，单次 content 建议不超过 6000 字符"
- `gui/im_message_handler.go`：
  - 新增 `consecutiveJSONTruncations` 计数器（agent loop 级别）
  - 工具结果追加到 conversation 后，检测是否包含 `参数解析失败` + `unexpected end of JSON input`
  - 连续 2 次截断失败后，注入系统消息指导 LLM 使用 `mode=append` 分块写入或改用 bash + Python 脚本

**三个问题的叠加效应**：write_file 失败 → LLM 困惑返回空响应 → Recover 注入 → context 膨胀 → 更多空响应 → 死循环。修复后：
- 空响应 3 次即硬退出，不再无限循环
- 维护任务不被工作流门控拦截
- write_file 截断后 LLM 收到明确的分块写入指导

**验收标准**：
- 连续 3 次空响应后返回最后有效结果，不再死循环
- "改进优化下这个技能？" 在有活跃工作流时不被 NeedsConfirm 拦截
- write_file 连续截断失败后 LLM 收到分块写入提示
- 所有 15 个 CodingGate 测试通过
- 所有 CodingSessionStarter / GateIntent / Drift 测试通过


### 39.1 write_file 截断根因修复——finish_reason=length 检测

**来源**：对 trajectory `2026-04-17_23-58-10.337` 的深入分析。

**根因**：不是 JSON 能力弱，不是序列化格式问题。是模型的 **`max_output_tokens` 限制**。

数据证据：
- 4 次失败的 args 长度都在 12.5K-13.1K 之间
- JSON 在 Python 代码中间被截断（`RGBColor(0x0`、`VIBRANT_OR`），不是在 JSON 语法边界
- `buildOpenAIChatRequestBody()` 没有设置 `max_tokens`，完全依赖模型默认值
- glm-5.1 默认 `max_output_tokens` 约 4096-8192 token ≈ 12K 字符（代码场景）
- LLM 自己后来学会拆分后（每个文件 1.7K-3.2K），全部成功

**为什么不改为 YAML/Markdown**：
- 问题不在序列化格式，在输出长度限制。任何格式 12K 字符都会被截断
- YAML 对缩进敏感，截断后更难恢复
- 换格式要改所有工具解析逻辑，风险大收益零

**修复**：在 LLM 流式响应层（而非工具层）拦截截断的 tool call。

- `gui/llm_stream.go`（OpenAI SSE 路径）：
  - `doOpenAILLMRequestStream()` 返回前新增 `finish_reason == "length"` + tool call 截断检测
  - 对每个 tool call 的 `Arguments` 做 `json.Unmarshal` 验证
  - 截断的 tool call 从 `msg.ToolCalls` 中移除
  - 在 `msg.Content` 中追加系统提示："模型输出长度超限，请将大文件内容拆分为多次写入"
  - 所有 tool call 都被截断时，`finishReason` 改为 `"stop"`（让 agent loop 走 no-tool 分支）

- `gui/llm_stream_responses.go`（Responses API 路径）：同步添加相同逻辑
- `gui/llm_stream_responses_ws.go`（Responses WebSocket 路径）：同步添加相同逻辑

**效果**：
- 截断的 tool call 不再到达 `executeTool()`，不再产生 `参数解析失败` 错误
- LLM 在同一轮迭代中就收到"请拆分"的提示，下一轮直接用小块写入
- 不需要等到连续 2 次失败才注入提示（之前的 `consecutiveJSONTruncations` 检测仍保留作为兜底）


### 40. SSH 会话操作后再次操作困难——Shell 响应性验证 + 连续失败自动清理

**根因**：maclaw 通过 SSH 操作服务器后，再次操作时遇到困难。级联故障链：

1. **Shell 被挂起进程锁住**：sqlite3 操作导致数据库锁，或交互式程序（vim/less/top）未正常退出，shell 不响应新命令
2. **`IsAlive()` 只检查连接不检查 shell**：`sshConnect` 复用会话时调用 `IsAlive()`，它只检查 SSH 连接级别的 keepalive（`SendRequest("keepalive@openssh.com")`），不检查 shell 是否能执行命令。连接活着但 shell 被锁住时，`IsAlive()` 返回 true，LLM 被送进死会话
3. **`sshExec` 无输出时无自动恢复**：命令发送后 `WaitForOutput` 等到超时返回 `(无新输出)`，但没有发送 Ctrl+C 中断挂起的命令，也没有连续失败计数。LLM 反复重试同一个死会话
4. **`WaitForOutput` 超时不清理**：等到 maxWait 后直接返回，不发送 Ctrl+C，导致 shell 被上一个命令永久锁住

**修复**：

#### 1. Shell 响应性验证（`corelib/remote/ssh_manager.go`）

- 新增 `CheckShellResponsive()` 方法：发送带唯一标记的 `echo __maclaw_health_xxx__` 命令，等待 3 秒看是否有包含标记的输出
- 如果无响应，先发送 Ctrl+C 尝试中断挂起的命令，再次 probe
- 两次都无响应返回 false
- 与 `IsAlive()` 的区别：`IsAlive` 检查 SSH 连接，`CheckShellResponsive` 检查 shell 是否能执行命令

#### 2. 连续失败自动清理（`gui/im_ssh_tools.go` + `tui/agent_tools_ssh.go`）

- `SSHManagedSession` 新增 `consecutiveExecFailures` 字段
- `SSHSessionManager` 新增 `RecordExecSuccess()` 和 `RecordExecFailure()` 方法
- `sshExec` 每次执行后判断是否有有效输出：
  - 有输出 → `RecordExecSuccess()`，重置计数
  - 无输出 → `RecordExecFailure()`，递增计数
- 连续 3 次无输出时：
  - 调用 `CheckShellResponsive()` 验证 shell
  - 无响应 → 自动 `RemoveSession()` 关闭死会话，返回明确提示让 LLM 重新 connect
  - Ctrl+C 恢复了 shell → 重置计数，提示 LLM 重新执行命令

#### 3. `WaitForOutput` 超时自动 Ctrl+C（`corelib/remote/ssh_manager.go`）

- `WaitForOutput` 等到 deadline 后，检查最后一行是否像 shell prompt
- 不像 prompt（命令可能挂起）→ 发送 `Interrupt()`（Ctrl+C）中断
- 等待 500ms 收集中断输出
- 在输出末尾追加 `[maclaw] ⚠️ 命令执行超时，已发送 Ctrl+C 中断`

#### 4. `sshConnect` 复用前验证 shell（`gui/im_ssh_tools.go`）

- 原逻辑：`IsAlive()` → 直接复用
- 新逻辑：`IsAlive()` → `CheckShellResponsive()` → 响应则复用，无响应则 `RemoveSession()` 后创建新会话
- 确保 LLM 不会被送进一个 shell 被锁住的死会话

**验收标准**：
- sqlite3 锁住 shell 后，下次 exec 连续 3 次无输出 → 自动关闭会话并提示重连
- `sshConnect` 复用会话时，shell 被锁住 → 自动关闭旧会话并创建新会话
- `WaitForOutput` 超时后自动发送 Ctrl+C，防止 shell 被永久锁住
- `sshClose` 调用后，`sshConnect` 不再复用已关闭的会话
- 所有 9 个现有 remote 包测试通过

#### 5. `sshClose` 不从 sessions map 移除会话（`gui/im_ssh_tools.go` + `tui/agent_tools_ssh.go`）

**根因**（从 trajectory 确认）：`sshClose` 调用 `Kill(sessionID)` 只发送 SIGKILL 到远程进程，不从 `sessions` map 中移除会话，不关闭 handle，不更新状态。LLM 调用 close 后再 connect，`findRunningSSHSession` 仍然找到旧会话，`IsAlive()` 返回 true（SSH 连接还在），于是复用了已关闭的死会话。

**修复**：
- `sshClose`：先 `Kill()`（忽略错误），再 `RemoveSession()` 从 map 中移除
- `sshCloseAll`：同步修改，每个会话都 `Kill()` + `RemoveSession()`
- TUI 侧 `sshClose` 同步修复

**Trajectory 证据**：
```
LLM: "连接确实卡了。强制关闭重建。"
→ ssh(action=close, session_id=ssh_root@api.rapidai.tech:22_1)
→ "✅ SSH 会话已关闭"
→ ssh(action=connect, host=api.rapidai.tech, ...)
→ "♻️ 复用已有 SSH 会话"  ← 旧会话没被移除！
→ ssh(action=exec, command="echo test123")
→ "(无新输出)"  ← 仍然是死会话
```


### 41. 活跃工作流期间无关消息触发文档预览面板——NeedsConfirm Gate 误捕获

**根因**：用户在 PPT 设计工作流的 NeedsConfirm 阶段（已有产出物）发送无关消息（如"查询杭州天气"）时，`handlePendingConfirm()` 通过 LLM 正确将其分类为 `"other"` 并 fall through 到正常 agent loop。但 agent loop 中的 `needsConfirmFromEngine` 仍然为 true（因为工作流确实在 NeedsConfirm 阶段），导致天气查询的 LLM 输出（200+ rune）被 `isSubstantivePhaseDocument()` 判定为"阶段文档"，通过 `EmitDocUpdate` 发送到前端预览面板，覆盖了 PPT 文档内容。

**触发路径**：
1. 用户在 PPT 工作流的某个 NeedsConfirm 阶段（如 ppt_generation），已有产出物
2. 用户发送"查询杭州天气"
3. `QuickFilter.Classify()` → `FilterActiveWorkflow`（工作流活跃）
4. `handleActiveWorkflow()` → `engine.HandleInput()` → `PendingConfirm: true`
5. `handlePendingConfirm()` → LLM 分类 → `"other"` → `return nil`（fall through）
6. Agent loop 中 `needsConfirmFromEngine = IsPhaseNeedsConfirm()` → true
7. LLM 输出天气信息（200+ rune）→ `isSubstantivePhaseDocument` = true
8. `EmitDocUpdate(userID, ws.CurrentPhase, 天气文本)` → 前端面板显示天气文本替代 PPT 文档

**修复**：
- `gui/im_message_handler_workflow.go`：`handlePendingConfirm()` 在 `"other"` 分支设置 `workflowPendingConfirmOther` 标记（`sync.Map`），标识该消息与活跃工作流无关
- `gui/agent_loop_context.go`：`LoopContext` 新增 `SkipNeedsConfirmGate bool` 字段
- `gui/im_message_handler.go`：
  - `handleIMMessageWithLoop()` 消费 `workflowPendingConfirmOther` 标记，传递到 `loopCtx.SkipNeedsConfirmGate`
  - `runAgentLoop()` no-tool 分支：`SkipNeedsConfirmGate` 为 true 时，`semanticBypass = true`，跳过 `needsConfirmFromEngine` 检查
  - `runAgentLoop()` tool 分支：`SkipNeedsConfirmGate` 为 true 时，`needsConfirmToolBranch = false`

**原理**：`handlePendingConfirm` 已经通过 LLM 判定消息与工作流无关，这个判定结果应该传递给 agent loop，防止 NeedsConfirm gate 将无关的 LLM 输出当作阶段文档捕获。标记通过 `sync.Map`（`LoadAndDelete` 一次性消费）传递，不会泄漏到后续消息。

**验收标准**：
- PPT 工作流活跃时发送"查询杭州天气" → 天气正常显示，右侧面板不被天气文本覆盖
- PPT 工作流活跃时用户说"确认" → 正常推进到下一阶段（不受影响）
- PPT 工作流活跃时用户说"修改需求" → 正常进入修改流程（不受影响）
- 无活跃工作流时 → 行为完全不变


### 42. 活跃工作流 doc_only ToolPolicy 过滤无关消息的工具列表——SSH 工具丢失

**根因**：#41 修复了 NeedsConfirm gate 对无关消息的误捕获，但遗漏了 **工具列表过滤** 层面的同一问题。当用户在活跃工作流（如 PPT 设计）的 `doc_only` 阶段发送无关消息（如"更新api服务器上的 ominiroute"），`handlePendingConfirm()` 正确将其分类为 `"other"` 并设置 `SkipNeedsConfirmGate`，但 `applyWorkflowToolFilter()` 不检查此标记，仍然按 `doc_only` 白名单过滤工具列表，导致 ssh、create_session、screenshot 等工具被移除。

**触发路径**（从日志和 trajectory 确认）：
1. 用户之前触发了某个工作流，停在 `requirements` 阶段（`NeedsConfirm=true, ToolPolicy=doc_only`）
2. 用户发送"更新api服务器上的 ominiroute"
3. `handlePendingConfirm()` → LLM 分类 → `"other"` → `SkipNeedsConfirmGate=true` → fall through
4. `Route()` 正确将 ssh 加入 Selected tools（29 个）
5. `applyWorkflowToolFilter()` 按 `doc_only` 白名单过滤 → ssh 被移除
6. LLM 收到的工具列表只有 10 个（bash, write_file, read_file 等），不含 ssh
7. LLM 通过 memory 回忆起 SSH 服务器信息，但没有 ssh 工具可调用
8. LLM 说"现在就登录服务器帮你更新"但无法执行，卡在"执行中"

**日志证据**：
- `tool_route.log`：`Selected tools (29)` 包含 ssh ✅
- `trajectory`：`tool_names (10)` 不含 ssh ❌
- `maclaw.log`：`[frozen_snapshot] reusing cached memory snapshot` — 未重新生成 memory snapshot
- `maclaw.log`：`[workflow-gate] NeedsConfirm no-tool engine bypassed: semantic intent=coding active=false bugFix=true` — NeedsConfirm gate 被正确跳过，但工具过滤未跳过

**修复**：
- `gui/im_message_handler.go`：`runAgentLoop()` 中 `applyWorkflowToolFilter` 调用处新增 `ctx.SkipNeedsConfirmGate` 检查——当 `handlePendingConfirm` 已判定消息与工作流无关时，跳过 doc_only 工具过滤，保留完整工具列表

**修改前**：
```go
if h.app != nil && h.app.workflowEngine != nil {
    tools = h.applyWorkflowToolFilter(userID, tools)
}
```

**修改后**：
```go
if h.app != nil && h.app.workflowEngine != nil && !ctx.SkipNeedsConfirmGate {
    tools = h.applyWorkflowToolFilter(userID, tools)
}
```

**验收标准**：
- 活跃工作流期间发送"更新api服务器" → ssh 工具在 LLM 工具列表中，可正常调用
- 活跃工作流期间发送"查询天气" → web_search 工具可用（本来就在 doc_only 白名单中，不受影响）
- 工作流阶段内的正常消息（如"确认"/"修改需求"）→ doc_only 过滤正常生效
- 无活跃工作流时 → 行为完全不变


### 43. 活跃工作流劫持图片识别请求——附件消息跳过工作流拦截 + hard exit 抑制 doc capture

**根因**：用户在活跃工作流（tech_design 阶段，`hasOutput=false`）期间发送"图中有什么？"+ 图片附件。`HandleInput` 在 `hasOutput=false` 时无条件返回 `RunAgentLoop=true` + PhasePrompt，把所有消息都当作"开始执行阶段"的信号。`handleActiveWorkflow` 设置 `workflowAgentLoopMarker`，agent loop 使用 PhasePrompt（"生成技术设计文档"）作为系统提示，完全忽略用户的图片识别意图。

LLM（glm-5.1）连续 3 次返回空响应（context 过大），触发 hard exit，`findLastAssistantContent` 从对话历史中回收了之前的文档片段（803 字节），post-loop doc capture 检测到 `workflowAgentLoop=true && len > 50`，将其当作阶段文档发射到预览面板。

**修复**（两层防护）：

#### 1. 附件消息跳过工作流拦截（`gui/im_message_handler.go`）
- 在 `handleWorkflowInterception` 调用前，检测消息是否带图片/文件附件且文本较短（<50 rune）
- 满足条件时跳过工作流拦截，让消息 fall through 到正常 agent loop
- 正常 agent loop 不设置 `workflowAgentLoopMarker`，doc capture 不触发

#### 2. Hard exit 抑制 doc capture（`gui/im_message_handler.go`）
- `IMAgentResponse` 新增 `HardExit bool` 字段
- 连续空响应 hard exit 路径设置 `HardExit=true`
- Post-loop doc capture 检查 `!resp.HardExit`，跳过 hard exit 的 fallback 文本
- 防止从对话历史回收的旧文本被当作新阶段文档

**验收标准**：
- 活跃工作流期间发送图片+"图中有什么？" → 正常识别图片，不弹出预览面板
- 活跃工作流期间发送"确认"/"继续" → 正常推进工作流（不受影响）
- 活跃工作流期间发送长文本（>50 rune）+ 图片 → 仍走工作流拦截（可能是工作流相关的图片输入）
- LLM 连续空响应 hard exit → 不触发 doc capture


### 44. workflow-confirm LLM 误分类导致服务器操作被劫持到需求修改分支

**根因**：用户在活跃工作流的 NeedsConfirm 阶段（requirements，已有产出物）发送"更新上面的 ominroute"。`handlePendingConfirm()` 调用轻量 LLM 分类意图，LLM 将"更新"理解为"修改文档"而非"更新服务器软件"，返回 `"modify"`。

`modify` 分支的处理方式是：
1. 构建 `phasePrompt`（需求阶段的完整系统提示）+ "用户修改请求：更新上面的 ominroute"
2. 设置 `workflowAgentLoopMarker=true` → agent loop 在工作流模式下运行
3. `applyWorkflowToolFilter` 按 `doc_only` 白名单过滤工具列表 → ssh 工具被移除
4. LLM 收到的是"更新需求文档"的指令，没有 ssh 工具可调用
5. 用户看到"执行中..."一直转圈

**核心问题**：`modify` 分支不应该把消息劫持到工作流专用 agent loop 中。工作流 agent loop 有 doc_only 工具过滤和 NeedsConfirm gate，这些机制会阻止 LLM 执行非文档操作。当 LLM 分类错误时（把操作请求误判为文档修改），这些机制会把用户锁死。

**修复**：

#### 1. `modify` 分支改为走正常 agent loop（`gui/im_message_handler_workflow.go`）

- `modify` 分支不再设置 `workflowAgentLoopMarker` 和 `stashedPhasePrompt`
- 改为与 `other` 分支相同的处理：设置 `workflowPendingConfirmOther=true`，return nil fall through 到正常 agent loop
- 正常 agent loop 有完整的工具列表（包括 ssh）和对话历史，LLM 自己能判断用户是要修改文档还是操作服务器
- 工作流阶段上下文已在对话历史中，LLM 在需要时仍能更新文档

**设计原则**：不用关键词做安全网——关键词堆砌是头痛医头，不可扩展。根本解决方案是让 `modify` 和 `other` 走相同的正常 agent loop 路径，把"修改文档 vs 操作服务器"的判断交给拥有完整上下文的主 LLM，而不是在轻量分类器层面做这个区分。

#### 2. 增强 `workflow-confirm` LLM system prompt（`gui/im_message_handler_workflow.go`）

- `modify` 的定义从"requests changes to the document content"细化为"requests changes to the DOCUMENT CONTENT itself"
- 新增 `other` 的具体示例：服务器操作（更新软件、登录服务器、npm install）、文件操作、信息查询
- 新增 CRITICAL 规则：当"更新"的对象是软件包/服务/工具名而非文档内容时，分类为 `other`

**验收标准**：
- 活跃工作流期间发送"更新上面的 ominroute" → LLM 分类为 other（或即使误分类为 modify 也走正常 agent loop）→ ssh 工具可用 → 正常执行
- 活跃工作流期间发送"加一个登录功能" → LLM 分类为 modify → 走正常 agent loop → LLM 根据对话历史更新需求文档
- 活跃工作流期间发送"确认" → LLM 分类为 confirm → 正常推进（不受影响）
- 所有 19 个工作流模板测试通过


### 45. Browser 工具 Memory-Driven Pin 泄漏 + LLM 输出 `Browser:` 前缀幻觉

**根因**：用户让 maclaw 通过 SSH 查看服务器资源，任务成功完成，但 LLM 在输出总结后产生了 `Browser: 伯伯，API 服务器资源状况如下：` 的幻觉文本。两个问题叠加：

1. **Memory-driven pin 绕过 `noEagerPinTools`**：`MatchConditionalTools()` 被 `im_system_prompt.go` 的 memory-driven pin 路径调用，扫描召回的记忆内容。SSH 服务器资源输出中包含 "Chrome 浏览器进程 PID 917323 CPU 39.6%"，"浏览器" 匹配强 browser 关键词规则，25+ browser 工具直接进入 `keep` 集合。`Route()` 中的 `noEagerPinTools` 只在 eager pin 循环中检查，不影响 `MatchConditionalTools` 的返回值。memory-driven pin 路径通过 `ShouldPinConditionalTool`（不检查 `noEagerPinTools`）将 browser 工具 pin 到 session。

2. **LLM 输出无角色前缀剥离**：LLM 在 context 中看到 25+ browser 工具定义后产生角色混淆，在正常输出后生成带 `Browser:` 前缀的重复内容。`stripThinkingTags` 只处理 `<think>` 标签，没有角色前缀剥离逻辑。

**修复**：

#### 1. `MatchConditionalTools` 过滤 `noEagerPinTools`（`corelib/tool/router.go`）

- `MatchConditionalTools()` 在返回前，从 `keep` 集合中删除所有 `noEagerPinTools` 中的工具
- 同时对 `needsConfirm` 提升也应用 `noEagerPinTools` 过滤
- 效果：记忆中的 "浏览器"/"页面"+"打开" 等文本不再导致 browser 工具被 pin 到 session
- SSH、web_search 等非 browser 工具不受影响（不在 `noEagerPinTools` 中）
- Browser 工具仍可通过实际成功调用后的 `ActivateSessionTool`（tool execution path）正常 pin

#### 2. `stripRolePrefixHallucination` 输出后处理（`gui/im_conversation_trim.go` + `gui/im_message_handler.go`）

- 新增 `rolePrefixRe` 正则：匹配行首的 `Browser:` 和 `Tool:` 角色前缀
- 新增 `stripRolePrefixHallucination()` 函数：
  - 代码块感知：不处理 ``` 围栏内的内容
  - Case 1：前缀在文本开头 → 剥离前缀 token，保留后续内容
  - Case 2：前缀在文本中间 → 截断到前缀位置（后续内容几乎总是重复）
- `im_message_handler.go`：在 `msgContent` 赋值后立即调用 `stripRolePrefixHallucination`

**测试**：
- `corelib/tool/router_memory_pin_test.go`：3 个测试
  - `TestMatchConditionalTools_MemoryBrowserNotPinned`：记忆中 "浏览器" 不 pin browser 工具
  - `TestMatchConditionalTools_MemorySSHStillPinned`：记忆中 "服务器" 仍然 pin SSH 工具
  - `TestMatchConditionalTools_WeakBrowserKeywordsNotPinned`：弱 browser 关键词不 pin
- `gui/im_conversation_trim_roleprefix_test.go`：8 个测试
  - 开头/中间/缩进的 Browser: 前缀剥离
  - 代码块内不剥离
  - 正常文本中的 Browser: 不误剥离
  - 空输入处理

**验收标准**：
- SSH 查看服务器资源时，LLM 输出不再包含 `Browser:` 前缀幻觉
- 记忆中提到 "Chrome 浏览器进程" 不导致 browser 工具被 pin
- SSH 工具的 memory-driven pin 不受影响
- Browser 工具在用户明确使用浏览器后仍可通过 tool execution path 正常 pin


### 46. SSH 会话重连后被旧 runExitLoop goroutine 杀死——连接池引用计数竞态

**根因**：`SSHSessionManager.runExitLoop()` 在 goroutine 启动时通过 `s.Handle.Exit()` 捕获了旧 handle 的 exitCh，但在收到退出信号后直接读取 `s.Handle`（而非捕获的引用）来执行 Close 和 pool.Release。当 `reconnectSession` 在旧 exitLoop 唤醒前替换了 `s.Handle` 为新的 PTY 会话时，旧 exitLoop 会：

1. 将 `s.Status` 设回 `SessionExited`，覆盖 reconnectSession 设置的 `SessionRunning`
2. 调用 `s.Handle.Close()` 关闭**新**会话（而非旧会话）
3. 调用 `pool.Release(hostCfg)` 释放**新**连接的引用计数（refCount 从 1 降到 0，连接被关闭）

**触发路径**：
1. 用户通过 SSH 查看服务器资源 → 会话 `ssh_root@api:22_1` 创建，`runExitLoop` goroutine #1 启动
2. 一段时间后 SSH 连接断开 → `waitLoop` 发送退出信号到 exitCh
3. 用户发送"帮我关掉chrome" → LLM 调用 `sshExec(session_id=ssh_root@api:22_1)`
4. `sshExec` 检测 `sessionDead=true` → 调用 `ReconnectByID` → `reconnectSession`
5. `reconnectSession`：关闭旧 handle → `pool.Reconnect` 获取新连接 → 创建新 PTY → 替换 `s.Handle` → 启动 `runExitLoop` goroutine #2
6. 旧 `runExitLoop` goroutine #1 从 exitCh 唤醒 → 读取 `s.Handle`（已是新 handle）→ Close 新 handle → Release 新连接
7. 新会话立即死亡，用户看到"SSH 会话已断开"

**修复**：
- `corelib/remote/ssh_manager.go`：`runExitLoop` 在 goroutine 启动时捕获 `handle := s.Handle`
  - 收到退出信号后检查 `isStale := s.Handle != handle`
  - `isStale=true`（handle 已被 reconnectSession 替换）时：不修改 session 状态、不释放连接池引用
  - Close 使用捕获的 `handle`（旧 handle），不读取 `s.Handle`（可能是新 handle）
  - `isStale=false`（正常退出，无重连）时：行为不变

**验收标准**：
- SSH 会话断开后重连成功，新会话不被旧 goroutine 杀死
- 连续使用 SSH（查看资源 → 关闭进程）不再卡住
- 所有 9 个 remote 包测试通过


### 46.1 SSH 连接池 Acquire 中 SendRequest 无超时保护——半开连接导致无限阻塞

**根因**：`SSHPool.Acquire()` 在检查连接是否存活时，直接调用 `candidate.SendRequest("keepalive@openssh.com", true, nil)`，没有超时保护。当 SSH 连接处于半开状态（TCP 连接看起来活着但实际已断，常见于 NAT 超时、防火墙断连、服务器重启等场景）时，`SendRequest` 会阻塞 30-120 秒（取决于 TCP 重传超时），期间整个 agent loop 卡住，用户界面显示"执行中..."无限转圈。

对比：`SSHPTYSession.IsAlive()` 已有 5 秒超时保护（goroutine + select），但 `pool.Acquire` 中的同一操作没有。

**触发路径**：
1. 用户通过 SSH 查看服务器资源 → 会话创建成功
2. 一段时间后 SSH 连接因 NAT/防火墙超时变为半开状态
3. 用户发送"检查chrome进程并杀掉" → LLM 调用 ssh 工具
4. `sshConnect` → 旧会话被清理 → fall through 到 `Create`
5. `Create` → `pool.Acquire` → `SendRequest("keepalive")` 在半开连接上 → **无限阻塞**
6. 用户看到"执行中..."一直转圈

**修复**：
- `corelib/remote/ssh_pool.go`：`Acquire()` 中的 `SendRequest` 改为 goroutine + select 模式，5 秒超时
- 超时视为连接已断，走清理 + 新建连接路径
- 与 `SSHPTYSession.IsAlive()` 的超时保护行为一致


### 47. Browser: 前缀幻觉——LLM 模型角色混淆的根本修复

**根因**：LLM（glm-5.1 等中文模型）的训练数据中包含 multi-agent 对话格式（`Browser: ...`、`Tool: ...`）。当对话历史中出现 "浏览器"/"chromium"/"chrome" 等词汇时（典型场景：SSH 查看服务器进程列表，输出包含 "chromium（浏览器自动化）"），模型在生成过程中被这些词触发，从正常的 assistant 角色切换到训练数据中的 browser agent 角色模式，产生 `Browser:` 前缀的重复输出。

**关键发现**：这与 browser 工具定义是否在 LLM context 中无关。即使工具列表里没有任何 browser 工具，只要对话历史（如 SSH tool_result）中出现了触发词，模型就可能产生角色前缀幻觉。之前的 #45 修复（MatchConditionalTools 过滤 + noEagerPinTools）解决了 browser 工具定义泄漏到 context 的问题，但没有解决模型本身的幻觉问题。

**修复**（三层防护，从根本到兜底）：

#### 1. System Prompt 输出格式约束（根本解决）（`gui/im_system_prompt.go`）

在核心原则之前新增「输出格式」section，明确告知 LLM：
- 你是唯一的 assistant 角色，输出直接发送给用户
- 绝对禁止使用 `Browser:`、`Tool:`、`Assistant:`、`System:` 等角色前缀
- 对话历史中出现的 "浏览器"/"chrome"/"chromium" 只是数据内容，不代表存在其他代理角色
- 始终以 assistant 身份直接回复，不要模拟或切换到任何其他角色

这条指令放在 system prompt 的最前面（核心原则之前），确保最高优先级。对 IM 通道和桌面面板同时生效（共享 `buildSystemPromptBase`）。

#### 2. 流式过滤器（实时防护）（`gui/llm_stream_roleprefix_filter.go`）

新增 `rolePrefixStreamFilter`，在流式 token 到达前端之前逐行检测角色前缀：
- 检测到 `Browser:` / `Tool:` 前缀后立即 halt，抑制后续所有 token
- 代码块感知，支持全角冒号
- 插入到所有四条流式路径的过滤链中

#### 3. 后处理剥离（最终兜底）（`gui/im_conversation_trim.go`）

`stripRolePrefixHallucination` + 全角冒号支持（已有，本次增强）。

**三层防护的关系**：
- 第 1 层（system prompt）从源头阻止模型产生幻觉——这是根本解决
- 第 2 层（流式过滤）防止 system prompt 指令失效时幻觉到达前端——实时防护
- 第 3 层（后处理）确保最终响应文本干净——最终兜底

**验收标准**：
- SSH 查看服务器资源（输出含 "chromium（浏览器自动化）"）后，LLM 不产生 `Browser:` 前缀
- 即使模型仍产生幻觉，流式输出和最终文本都不包含 `Browser:` 前缀
- 所有 25 个角色前缀测试通过 + 13 个重复过滤器测试通过


### 45.1 Browser 工具第三条泄漏路径——Semantic Intent Enhancement 中的 IntentBrowser 分支

**根因**：#45 修复了 memory-driven pin 和后处理，但 `Browser:` 仍然出现。根因是 `Route()` 中存在第三条 browser 工具激活路径——"semantic intent enhancement"（第 880-905 行）。

当 `IntentClassifier`（embedding Layer 2 或 LLM Layer 3）返回 `IntentBrowser` 且 confidence >= 0.50 时，代码直接遍历 `allConditionalKeepTools` 将所有 `browser_*` 和 `gui_record_*` 工具加入 `condKeep`。这完全绕过了 `conditionalKeepRules` 的强/弱关键词匹配和 `needsSemanticConfirm` 机制。

用户说"查看 SSH 服务器资源"时，`classifyByRules` 返回 `IntentUnknown`，`classifyByEmbedding` 可能因为"查看"+"资源"与 browser anchor "帮我在网页上点击登录按钮" 有一定语义相似度，返回 `IntentBrowser`（ambiguous zone，打 0.6 折扣后 confidence 可能仍 >= 0.50）。25+ browser 工具定义被注入 LLM context，导致 `Browser:` 前缀幻觉。

**修复**：
- `corelib/tool/router.go`：从 semantic intent enhancement 的 switch 中移除 `IntentBrowser` 分支
- 在 `cachedICResult.Intent` 过滤条件中新增 `!= IntentBrowser`
- Browser 工具的激活现在只有两条路径：
  1. `conditionalKeepRules` 强关键词（"浏览器"/"chrome"等）→ 直接激活
  2. `conditionalKeepRules` 弱关键词（"页面"+"打开"）→ `needsSemanticConfirm` → IntentClassifier 确认
- 不再有第三条 embedding/LLM 直接激活路径

**设计原则**：Browser 工具的激活成本极高（25+ 工具定义注入 context），必须有高置信度的信号。强关键词（"浏览器"）是高置信度信号；弱关键词 + semantic confirm 是中等置信度信号；纯 embedding 相似度是低置信度信号，不应该用来激活 25+ 工具。

**测试**：
- `TestRoute_SemanticIntentEnhancement_NoBrowserActivation`：验证 "查看服务器资源" 不激活 browser 工具


### 46. SSH 重连困难——routeTools 在 session-pinned 时仍移除 ssh 工具

**根因**：用户先查看 SSH 服务器资源（ssh 工具被 session pin），然后点击"查看 bb-browser 详情"继续操作。`routeTools()` 在 `Route()` 之后有一段逻辑：当 `classifyTaskIntent(userMessage).Intent != intentSSH` 时，从工具列表中移除 ssh 工具。"查看 bb-browser 详情"不包含 SSH 关键词，被分类为非 SSH 意图，ssh 工具被移除。LLM 没有 ssh 工具可用，只能用文字描述它想做什么，陷入漂移循环。

**修复**：
- `gui/im_message_handler.go`：`routeTools()` 在移除 ssh 前检查 `router.IsSessionPinned("ssh")`，如果 ssh 已被 session pin（说明用户之前在本会话中使用过 ssh），保留 ssh 工具不移除
- `gui/tool_router.go`：新增 `IsSessionPinned(name)` 方法，委托到 `corelib/tool.Router`
- `corelib/tool/router.go`：新增 `IsSessionPinned(name)` 方法，检查 `sessionTools[name]`
- `gui/im_message_handler_tools_test.go`：`TestRouteTools_WithRouterKeepsSSHForSSHIntent` 在两个独立 case 之间添加 `ResetSession()`，避免 session pin 跨 case 泄漏


### 45.2 / 46.1 机制性修复重构——从 workaround 升级为通用机制

**审视**：#45.1 和 #46 的修复存在 workaround 问题：
- #45.1 直接删除了 `IntentBrowser` 分支，导致 embedding 检测到的 browser 意图永远无法通过 semantic intent enhancement 激活。换个场景（如"帮我搞个能自动在网页上抢票的东西"）就失效了。
- #46 在 `routeTools` 中加了 `IsSessionPinned("ssh")` 特判，但 `routeTools` 对 `Route()` 结果做二次过滤本身就是错误的架构——`Route()` 已经通过 `conditionalKeepRules` + `sessionTools` 做了完整的工具选择。

**重构为机制性修复**：

#### 1. Semantic intent enhancement：高成本工具集自动高门槛（`corelib/tool/router.go`）

恢复 `IntentBrowser` 分支，但对 `noEagerPinTools` 中的工具要求 `highCostConfidenceThreshold = 0.78`（与 `embeddingHighThreshold` 一致）。

机制：`noEagerPinTools` 是一个声明式的"高成本工具集"标记。现在它在三个消费点行为一致：
1. `Route()` eager pin 循环：跳过（不自动 pin）
2. `MatchConditionalTools()`（memory-driven pin）：过滤掉
3. Semantic intent enhancement：要求 0.78 高置信度（而非 0.50）

未来新增高成本工具集只需加入 `noEagerPinTools`，三个消费点自动获得保护。

#### 2. 删除 `routeTools` 的 ssh 二次过滤（`gui/im_message_handler.go`）

`routeTools()` 简化为直接返回 `Route()` 结果，不再做任何 per-tool 过滤。SSH 的激活/过滤完全由 `Route()` 的 `conditionalKeepRules` + `sessionTools` 负责：
- 消息含 SSH 关键词 → `condKeep` 包含 ssh
- 消息不含 SSH 关键词但 ssh 被 session pin → `sessionTools` 包含 ssh
- 消息不含 SSH 关键词且 ssh 未被 pin → `condFilterOut` 排除 ssh

这是 `Route()` 设计时就有的机制，`routeTools` 不应该覆盖它。

**删除的代码**：`routeTools` 中 `classifyTaskIntent` + ssh 过滤 + `IsSessionPinned` 检查（约 20 行）。
**保留的代码**：`IsSessionPinned` 方法保留为公共 API，未来可能有其他消费者。


### 48. 漂移检测器误杀合法轮询——从"同参数=漂移"升级为"同参数+同结果=漂移"

**根因**：`DriftDetector.DetectDrift()` 的漂移判定条件是"连续 N 次调用同名工具且 ArgsHash 相同"。这个定义有根本性缺陷——它只看输入（参数），不看输出（结果）。

真正的漂移（死循环）= 同样的输入 + 同样的输出。外部状态没有推进。
合法轮询 = 同样的输入 + 不同的输出。外部状态在推进。

SSH `check_task` 轮询同一个 task_id 时参数完全相同（这是正确行为），但结果在变化（`running 18s` → `running 23s` → `completed`）。漂移检测器只看参数不看结果，把合法轮询判定为死循环。

**触发路径**（从 trajectory `2026-04-22_07-00-20.218` 确认）：
1. maclaw 连接 Service 服务器，提交 `git clone` 后台任务
2. 连续 3 次 `check_task` 轮询 clone 进度（结果在变化：running → running → completed）→ 第一次漂移误触发
3. clone 完成后提交 `go install` 后台任务
4. 连续 3 次 `check_task` 轮询 Go 安装进度（结果在变化）→ 第二次漂移误触发（`needHuman=true`）
5. Agent loop 终止。Go 安装实际上已经成功完成（`EXIT: 0`），但 maclaw 在同一刻被杀死

**修复**（机制性修复，非 workaround）：
- `gui/drift_detector.go`：
  - `DetectDrift()` 在检测到"连续 N 次同名工具 + 同 ArgsHash"后，新增 `resultsAreChanging()` 检查
  - `resultsAreChanging()` 比较连续调用的 `ResultHint`（工具返回结果的截断摘要，已有字段），只要有任意两次结果不同，就说明外部状态在推进，不是死循环
  - 结果有变化 → 不触发漂移（返回空 DriftResult）
  - 结果全部相同 → 触发漂移（行为不变）
  - ResultHint 全部为空（无结果数据可比较）→ 保守处理，仍触发漂移
  - `PreviewDrift()` 同步应用相同的结果变化检查
- `gui/im_message_handler.go`：无变更，所有工具调用仍然无条件记录到漂移检测窗口

**为什么是机制性修复而非 workaround**：
- 不硬编码任何工具名或 action 名到豁免列表
- 对所有工具通用：SSH check_task、get_session_output、未来的 docker wait、MCP 轮询工具等
- 利用已有的 `ResultHint` 字段（每次工具调用后已经记录），零额外开销
- 判定标准是语义正确的：漂移 = 同输入 + 同输出（死循环），不漂移 = 同输入 + 不同输出（状态推进）

**验收标准**：
- SSH 后台任务轮询（结果在变化）→ 不触发漂移
- SSH exec 连续 3 次相同命令且结果相同（如 "connection refused"）→ 正常触发漂移
- get_session_output 轮询（输出在变化）→ 不触发漂移
- get_session_output 轮询但会话卡住（输出不变）→ 正常触发漂移
- 任意 MCP 工具轮询（结果在变化）→ 不触发漂移
- ResultHint 全部为空 → 保守触发漂移（向后兼容）
- 所有 3 个现有 DriftDetector 测试 + 8 个新增机制测试通过


### 49. Steering 机制——声明式规则注入（借鉴 Kiro .kiro/steering/）

**来源**：Kiro IDE 的 `.kiro/steering/` 机制。MacLaw 的所有行为规则硬编码在 `im_system_prompt.go` 中（编码工作流 ~400 行、编码规范 ~80 行、SSH 规则 ~30 行），修改需要改 Go 代码→编译→发版。用户无法自定义规则，不同项目无法有不同规范。

**设计**：在 `~/.maclaw/steering/`（用户级）和 `<project>/.maclaw/steering/`（项目级）放置 Markdown 文件，通过 YAML front-matter 声明注入策略，按需注入到 system prompt 中。

**四种注入模式**：
- `always`：每次对话都注入（核心工作流、安全规则）
- `fileMatch`：对话中读取/编辑了匹配文件时注入（语言/框架专用规范）
- `contextMatch`：用户消息匹配关键词时注入（SSH 规则、浏览器规则）——MacLaw 特有，对应 `conditionalKeepRules` 但从"激活工具"扩展到"注入规则"
- `manual`：用户在 IM 中 `#规则名` 引用时注入（特殊项目约定）

**Token 预算**：
- 总预算 3,000 token（占 128K context 有效输入的 ~3%）
- 单文件上限 1,500 token
- always 文件最多 5 个，总文件最多 20 个
- 小 context 模型（<80K）按 3% 比例缩减，最低 500 token
- 超预算时按优先级截断到剩余空间，而非直接跳过

**实现**：

#### Phase 1: 核心框架（`corelib/steering/`）
- `types.go`：`File`、`Scope`、`InclusionMode`、`ResolveContext` 数据结构
- `budget.go`：Token 预算常量 + `effectiveBudget()` 动态缩放 + `estimateTokens()`
- `parser.go`：YAML front-matter 解析（`splitFrontMatter`）+ `ParseFile()`
- `store.go`：`Store` 实现——`Load()`（扫描两级目录、合并同名文件、排序）、`Resolve()`（四种模式匹配 + token 预算截断）、30 秒惰性热加载
- `store_test.go`：13 个单元测试

#### Phase 2: 内置默认文件 + System Prompt 注入
- `defaults.go`：`EnsureDefaults()` 首次安装生成 3 个默认文件（coding-workflow / encoding-guidance / ssh-operations），不覆盖用户修改
- `gui/app_steering_init.go`：`initSteeringStore()` GUI 侧初始化
- `gui/im_system_prompt_steering.go`：`appendSteeringSection()` 注入到 system prompt（核心原则之后、记忆之前）；`extractSteeringRefs()` 解析 IM 消息中的 `#` 引用
- `gui/im_system_prompt.go`：`buildSystemPromptBase()` 中调用 `appendSteeringSection()`
- `tui/agent_handler.go`：TUI 侧 system prompt 注入
- `tui/app_workflow_init.go`：`initTUISteeringStore()` TUI 侧初始化

#### Phase 3: contextMatch + 文件追踪
- `defaults.go`：新增 `ssh-operations.md` 默认文件（`contextMatch` 模式，关键词 ssh/服务器/远程/部署）
- `gui/im_system_prompt_steering.go`：`trackSteeringFile()` / `trackSteeringFileFromArgs()` 追踪工具调用中的文件路径；`resetSteeringContextFiles()` 对话重置时清理
- `gui/im_tool_execution.go`：`executeTool()` 中 hook 文件追踪
- `gui/im_message_handler.go`：`steeringContextFiles sync.Map` 字段 + `/new` 重置

#### Phase 4: 集成测试
- `integration_test.go`：6 个端到端测试覆盖全部四种模式 + 项目覆盖 + 组合场景 + EnsureDefaults

**与现有机制的关系**：
- **互补**：router 管工具激活，steering 管规则注入，两者独立匹配但可共享关键词
- **不替代**：CodingToolGate（代码级硬逻辑）、WorkflowEngine（状态机）、IntentClassifier（算法）仍由各自模块负责
- **渐进迁移**：硬编码规则作为内置默认值保留，steering 文件可覆盖/追加

**验收标准**：
- 首次启动后 `~/.maclaw/steering/` 下自动生成 3 个默认文件
- 用户编辑文件后下次对话自动生效（30 秒内）
- SSH 规则只在用户提到"服务器"/"ssh"时注入，不浪费 context
- 项目级同名文件覆盖用户级文件
- 总 token 不超过 3,000（128K 模型）或按比例缩减（小模型）
- 20 个测试全部通过（13 单元 + 6 集成 + 1 集成壳）
- GUI 和 TUI 编译通过


### 50. Browser: 前缀幻觉——数据流分叉导致前端显示未清理内容

**根因**：LLM 流式输出路径存在**数据流分叉**——`contentBuf`（原始内容）和流式过滤链（过滤后内容）是两条独立的数据流，但 `msg.Content` 从 `contentBuf` 读取，前端 `streamedContent` 从过滤链读取。当过滤链修改了内容（剥离 `Browser:` 前缀、抑制重复等），两条数据流产生不一致。

**数据流分叉路径**：
```
LLM delta.Content
  ├→ contentBuf.WriteString(delta)     → msg.Content → stripRolePrefixHallucination → resp.Text (干净)
  └→ tf → fcf → tcf → repf → rpf → onToken → 前端 streamedContent (过滤后)
```

正常情况下两条路径的内容一致。但当流式过滤器修改了内容时（如剥离 `Browser:` 前缀），`msg.Content`（来自 `contentBuf`）仍然是原始内容，而 `streamedContent`（来自 `onToken`）是过滤后的内容。后端的 `stripRolePrefixHallucination` 清理了 `msg.Content` → `resp.Text`，但前端的 `resolveFinalRoundContent` 在多轮 agent loop 中可能选择 `streamedContent` 而非 `resp.Text`。

如果流式过滤器漏掉了某种变体的 `Browser:` 前缀（如 blockquote 包裹、特殊空白字符等），`streamedContent` 就是脏的。后端的 `stripRolePrefixHallucination` 清理了 `resp.Text`，但前端选择了脏的 `streamedContent`。

**这不是正则匹配的问题**——给正则加 `>` 是 workaround，下次 LLM 用 `- Browser:` 或 `* Browser:` 又会失效。根本问题是两条数据流的分叉。

**修复**：统一数据源——让 `msg.Content` 和 `streamedContent` 来自同一条数据流。

在四条流式路径（OpenAI SSE、Anthropic SSE、Responses API、Responses WebSocket）中：
- 新增 `filteredBuf`：在过滤链的末端（`onToken` 之前）插入一个 tee，将过滤后的内容同时写入 `filteredBuf` 和 `onToken`
- `msg.Content` 优先从 `filteredBuf` 读取（过滤后内容），`contentBuf` 保留原始内容用于 XML tool call 解析等
- `filteredBuf` 为空时（如全部内容在 `<think>` 标签内或全是 tool calls）回退到 `contentBuf`

**修改后的数据流**：
```
LLM delta.Content
  ├→ contentBuf.WriteString(delta)     → XML tool call 解析 (原始内容)
  └→ tf → fcf → tcf → repf → rpf → filteredOnToken
                                          ├→ filteredBuf.WriteString(delta)  → msg.Content (过滤后)
                                          └→ onToken(delta)                  → 前端 streamedContent (过滤后)
```

`msg.Content` 和 `streamedContent` 现在来自同一个 `filteredOnToken` 出口。无论流式过滤器做了什么修改（剥离前缀、抑制重复、过滤 think 标签），两者都是一致的。后端的 `stripRolePrefixHallucination` 仍然作为兜底存在，但不再是唯一的清理点。

**修改文件**：
- `gui/llm_stream.go`：OpenAI SSE 路径 + Anthropic SSE 路径
- `gui/llm_stream_responses.go`：Responses API 路径
- `gui/llm_stream_responses_ws.go`：Responses WebSocket 路径

**为什么是机制性修复**：
- 不依赖正则匹配的完备性——无论 LLM 产生什么变体的幻觉前缀，只要流式过滤器处理了它，`msg.Content` 和 `streamedContent` 就是一致的
- 不依赖前端的内容选择逻辑——`resolveFinalRoundContent` 无论选择 `streamedContent` 还是 `resp.Text`，两者都是过滤后的内容
- 对未来新增的流式过滤器（如新的幻觉模式检测）自动生效——任何过滤器的修改都会同时反映在 `msg.Content` 和 `streamedContent` 中
- `contentBuf` 保留原始内容不受影响，XML tool call 解析等功能不受影响

**验收标准**：
- `Browser:` 前缀被流式过滤器剥离后，`msg.Content` 也是干净的（与 `streamedContent` 一致）
- 流式过滤器漏掉的变体，`stripRolePrefixHallucination` 仍然兜底清理 `msg.Content`
- 所有 13 个流式过滤器测试通过
- 所有 13 个后处理测试通过
- 所有 10 个 `resolveFinalRoundContent` 测试通过
- GUI 编译通过


### 51. 文件发送请求误触发 contract_review 工作流——UIC 预检实现工作流意图融合

**来源**：用户在微信中说"把D:\yuce\quant\jjquant-v2\docs\architecture\0511-foundation-core.md文件发给我"，系统启动了 contract_review 工作流。

**根因**：工作流拦截链（`QuickFilter` → `IntentUnderstandingManager`）和意图分类链（`UnifiedIntentClassifier` L1/L2/L3 fusion）是两条完全独立的管道，互不通信。

UIC 已有 `LabelDocumentDelivery` 标签，L1 关键词 `"发给我"` 是 Strong 级别，对这条消息会在 <1ms 内 confident 返回 `document_delivery`。但工作流拦截链完全不知道 UIC 的判断结果，独立调用了 `IntentUnderstandingManager` 的 LLM（10-30s），该 LLM 将"文件"+"发给"误联想为"用户要提供文件进行审查"，返回 `category="contract_review"`。

这是 `intent-fusion-upgrade-design.md` Phase 3 "工作流意图融合"标注为"待实施"的缺口：两条管道不共享信号。

**修复**：实施 Phase 3 核心——数据驱动的 UIC 预检机制

#### 1. `IntentDefinition` 新增 `MayTriggerWorkflow` 字段（`corelib/intent/fusion_types.go`）

在统一意图定义中声明哪些意图可能触发工作流。这是数据层面的声明，不是独立维护的硬编码列表。

- `LabelCoding`：`MayTriggerWorkflow: true`（coding 工作流）
- `LabelOffice`：`MayTriggerWorkflow: true`（presentation_design 工作流）
- 其他 10 个标签：`MayTriggerWorkflow: false`（默认值，不触发工作流）

#### 2. `WorkflowCandidateLabels()` 从 definitions 自动派生（`corelib/intent/fusion_types.go`）

遍历所有 `IntentDefinition`，收集 `MayTriggerWorkflow=true` 的标签 + `Ambiguous`/`Unknown`（安全兜底）。新增意图时只需在 definition 中设置 `MayTriggerWorkflow=true`，无需改任何其他代码。

#### 3. `FusionConfig.WorkflowRejectThreshold`（`corelib/intent/fusion_types.go`）

阈值从硬编码常量改为 `FusionConfig` 字段（默认 0.70），可通过 `SetFusionConfig()` 或离线校准工具调优。

#### 4. UIC 新增 `IsWorkflowCandidate()` 和 `GetWorkflowRejectThreshold()`（`corelib/intent/classifier.go`）

- `workflowCandidates` 在构造时从 definitions 预计算
- `IsWorkflowCandidate(label)` 查询预计算集合
- `GetWorkflowRejectThreshold()` 从 FusionConfig 读取

#### 5. `handleNeedsUnderstanding` UIC 预检（`gui/im_message_handler_workflow.go`）

在调用 IUM 的慢 LLM（10-30s）之前，先查询 UIC。当 UIC confidence ≥ threshold 且标签不是工作流候选时，直接 reject。

**机制性特征**：
- **数据驱动**：工作流候选集合从 `IntentDefinition.MayTriggerWorkflow` 自动派生，不维护独立列表
- **单一数据源**：新增意图时只改 `definitions.go`，所有层（keyword/embedding/tree/workflow pre-check）自动生效
- **可校准**：阈值在 `FusionConfig` 中，可通过 Phase 4 校准工具在标注数据上调优
- **安全兜底**：`Ambiguous`/`Unknown` 始终是工作流候选，不会被误 reject

**验收标准**：
- "把...文件发给我" → UIC L1 返回 `document_delivery`（conf=1.0）→ 不是工作流候选 → 快速 reject → 正常执行文件发送
- "帮我审查这个合同" → UIC 返回 `ambiguous`/`unknown` → 是工作流候选 → 进入 IUM → 正常触发 contract_review
- "开发一个贪吃蛇游戏" → UIC 返回 `coding`（`MayTriggerWorkflow=true`）→ 是工作流候选 → 进入 IUM → 正常触发 coding 工作流
- "帮我做一份PPT" → UIC 返回 `office`（`MayTriggerWorkflow=true`）→ 是工作流候选 → 进入 IUM → 正常触发 presentation_design
- UIC 不可用时 → 行为不变，直接进入 IUM
- 新增 `IntentDefinition{MayTriggerWorkflow: true}` → 自动成为工作流候选，无需改其他代码
- 11 个新增测试（5 corelib/intent + 6 gui）+ 所有 16 个现有 migration/equivalence 测试通过


### 52. IM 通道 SSH 工具被 doc_only 工作流过滤——LLM 回退到 bash 执行原始 ssh 命令

**来源**：用户在微信通道让 maclaw 查询服务器信息，maclaw 不使用内置 ssh 工具，而是通过 bash 执行 `ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no root@ap...`，导致命令挂起 240+ 秒无结果。桌面 AI 助手面板正常使用 ssh 后台管理功能。

**根因**：`DocOnlyAllowedTools` 白名单包含 `bash` 但不包含 `ssh`。当用户有活跃工作流（如之前的 PPT 设计、产品设计等）且当前阶段为 `doc_only` 时，`applyWorkflowToolFilter()` 按白名单过滤工具列表，`ssh` 被移除。LLM 在工具列表中有 `bash` 但没有 `ssh`，只能通过 bash 执行原始 ssh CLI 命令。原始 ssh 命令无法访问 maclaw 存储的 SSH 凭据、密钥和连接管理，导致认证挂起。

`SkipNeedsConfirmGate` 旁路（#41/#42）仅在 `handlePendingConfirm` 将消息分类为 "other" 时生效。以下场景不触发旁路：
1. 工作流阶段无产出物（`hasOutput=false`）→ `HandleInput` 直接返回 `RunAgentLoop=true`，不经过 `handlePendingConfirm`
2. `handlePendingConfirm` 将短回复（如 "api"）误分类为 "confirm" → 工作流推进到下一阶段，新阶段以 `doc_only` 过滤运行

**修复**：
- `corelib/workflow/types.go`：`DocOnlyAllowedTools` 新增 `ssh` 和 `screenshot`
  - `ssh` 是远程服务器操作工具，不是编码会话工具，应在所有工作流阶段可用
  - `screenshot` 是屏幕截图工具，同理
  - 与 `bash`、`web_search`、`web_fetch` 等已有的操作类工具同等对待
- `gui/im_tool_definitions.go`：`bash` 工具描述新增"禁止通过 bash 执行 ssh/scp/rsync 命令——请使用内置 ssh 工具"，作为纵深防御

**设计原则**：`DocOnlyAllowedTools` 是 doc_only 阶段工具可用性的单一数据源。操作类工具（bash、ssh、screenshot、web_search 等）应始终可用，因为用户在工作流期间发送无关操作请求是常见场景。白名单的目的是排除编码会话工具（create_session 等），不是排除所有非文档工具。

**验收标准**：
- 活跃工作流 doc_only 阶段发送"查询服务器信息" → ssh 工具在 LLM 工具列表中，正常使用内置 ssh 工具
- 活跃工作流 doc_only 阶段发送"截屏" → screenshot 工具可用
- 编码会话工具（create_session 等）在 doc_only 阶段仍被过滤
- GUI 和 TUI 共享同一个 `DocOnlyAllowedTools`，修复同时生效
- 所有 workflow 包测试通过

### 53. Skill 删除失败——removeSkillDirs 只识别 skill.yaml，不识别 SKILL.md/skill.md

**根因**：`removeSkillDirs()` 自己实现了一套"扫描目录 → 读 skill.yaml → 解析名称 → 匹配 → 删除"的逻辑，而 `ScanSkillDir()` 已经有完整的"扫描目录 → 解析所有格式（skill.yaml / SKILL.md / skill.md / Claude SKILL.md / craft_tool fallback）→ 返回 NLSkillEntry"的逻辑。两套逻辑不同步：`ScanSkillDir` 能发现 SKILL.md 格式的 skill，`removeSkillDirs` 不能。

这不是"漏了一种格式"的问题，而是**发现路径和删除路径使用了不同的解析逻辑**。任何新增的 skill 格式只要 `ScanSkillDir` 支持了，`removeSkillDirs` 就会漏掉。

另外 `removeSkillDirs` 还有一个 copy-paste bug：重试逻辑两次检查的是同一个路径（都是 `skill.yaml`）。

**修复**（机制性修复——删除路径复用发现路径）：
- `corelib/skill/scanner.go`：将 `ScanSkillDir` 的核心逻辑提取为 `scanSkillDirInternal(root, filterPlatform)`，新增 `ScanSkillDirAll()` 导出函数（不做平台过滤）。`ScanSkillDir` 行为不变（带平台过滤），`ScanSkillDirAll` 供删除/诊断场景使用——删除路径不应受发现路径的过滤策略影响。
- `gui/app_nl_skills.go`：`removeSkillDirs()` 调用 `skill.ScanSkillDirAll(root)` 获取已解析的 `NLSkillEntry` 列表（含所有格式、不过滤平台），按 `Name` / `DirName` 匹配后删除 `SkillDir`。发现和删除共享同一条解析路径，未来新增任何 skill 格式自动兼容。
- `tui/commands/skill.go`：`skillDelete()` 同样改为复用 `skill.ScanSkillDirAll`。

**验收标准**：
- 只有 SKILL.md（无 skill.yaml）的 skill → 点击删除 → 目录被清理，skill 不再出现
- 有 skill.yaml 的 skill → 行为不变
- 未来新增 skill 格式 → 只要 `ScanSkillDir` 支持，删除自动兼容
- `corelib/skill` 包所有测试通过
- GUI 和 TUI 编译通过

### 54. 打断后失忆——`runAgentLoop` 越权管理历史生命周期

**来源**：用户在桌面 AI 助手面板通过"输入缓冲区引导发射"（Buffer Queue Fire）打断正在执行的任务后，后续消息完全丢失上下文。

**触发场景**（从截图确认）：
1. 用户说"使用drawio skill 画一个北京5环图" → agent 开始编写 XML
2. 用户在 agent 工作期间输入"继续" → 进入输入缓冲区
3. 用户点击缓冲区的发射按钮（或 agent loop 完成后自动排空）
4. 前端调用 `cancelSession()` → 后端 `CancelCurrentSession()` → `ctx.Cancel()`
5. Agent loop 检测到 `IsCancelled()` → **`h.memory.Clear(userID)`** → 所有对话历史被清空
6. "继续" 作为新消息发送 → `h.memory.Load(userID)` 返回空列表
7. LLM 收到"继续"但没有任何上下文 → 回复"请问您想查询哪个城市的天气呢？"（完全跑偏）

**根因（机制性分析）**：

问题不是"取消时要不要保存"，而是 **`runAgentLoop` 越权管理了历史生命周期**。

对话历史的生命周期有两个层次：
- **消息处理层**（`handleIMMessageWithLoop`）：决定何时创建、何时清空历史。这是正确的管理层。
- **Agent Loop 层**（`runAgentLoop`）：执行 LLM 推理循环，积累新的对话条目。它应该只负责"保存积累的条目"，不应该有权力"清空历史"。

`runAgentLoop` 内部 3 个取消退出点直接调用 `memory.Clear(userID)`，越权做了"清空历史"的决策。正常退出路径调用 `saveConversationHistoryTimed()` 保存历史——这是正确的行为。取消退出应该做同样的事。

**不变量**：`runAgentLoop` 的所有退出路径（正常完成、取消、错误）统一调用 `saveConversationHistoryTimed`，不调用 `memory.Clear`。`memory.Clear` 只在消息处理层使用。

**修复**：
- `gui/im_message_handler.go`：3 个取消退出点全部从 `h.memory.Clear(userID)` 改为 `h.saveConversationHistoryTimed(userID, history, nil)`
- 取消退出和正常退出的唯一区别是返回的消息内容不同（"⏹️ 任务已取消" vs 正常响应），历史保存行为完全一致

**彻查：所有 `memory.Clear` 调用点分类**：

| 调用位置 | 触发条件 | 层次 | 是否正确 |
|---------|---------|------|---------|
| `/new`、`/reset`、`/clear` 命令 | 用户主动重置 | 消息处理层 | ✅ 正确 |
| `StartNewTask` | 用户明确开始新任务 | 消息处理层 | ✅ 正确 |
| `shouldAutoClearIncompleteTaskContext` | 自动检测到新任务 | 消息处理层 | ✅ 正确（用 `ClearConversationAndDismissSlot`） |
| TopicDetector `TopicNew` | 话题切换自动清理 | 消息处理层 | ✅ 正确（先归档摘要再清空） |
| `handleExitCommand` | `/exit` 命令 | 消息处理层 | ✅ 正确 |
| ~~取消退出点 ×3~~ | ~~agent loop 被取消~~ | ~~Agent Loop 层~~ | ❌ **已修复** |

修复后，`runAgentLoop` 内部不再有任何 `memory.Clear` 调用。历史生命周期完全由消息处理层管理。

**为什么不是 workaround**：
- 不是"取消时换个方式保存"——是从架构上确立了"谁管理历史生命周期"的边界
- `runAgentLoop` 作为纯计算函数，只负责"保存积累的条目"，不负责"决定是否清空"
- 这个不变量对所有退出路径通用，不需要按场景分别处理
- 未来新增退出路径时，只需调用 `saveConversationHistoryTimed`，不需要判断"这种情况要不要清空"

**验收标准**：
- 用户打断后发送"继续" → LLM 有完整上下文，继续之前的任务
- 用户打断后发送无关消息 → TopicDetector 在消息处理层检测到话题切换，自动清理
- 用户手动 `/new` → 在消息处理层清空历史（不受影响）
- `runAgentLoop` 内部 grep `memory.Clear` 返回 0 结果
- 5 个新增测试通过 + 所有现有 RunAgentLoop 测试通过（2 个预先存在的失败不受影响）



### 55. 应用重启后任务上下文丢失——In-Flight Task Marker + 同步持久化

**来源**：用户让 maclaw 查找并下载论文，maclaw 正在搜索多个学术平台（ScienceDirect、Semantic Scholar、Google Scholar），找到了 DOI 并准备下载 PDF。此时 maclaw 更新并重启。用户说"继续"，maclaw 回复"当前没有进行中的任务或活跃会话。看起来这是一个新的对话。"——完全丢失了论文查找的上下文。

**根因**（两个独立问题叠加）：

#### 根因 1：进程被杀时对话历史可能未持久化

`ConversationMemory` 的 `persistLoop` 使用 150ms debounce 异步写盘。`saveConversationHistoryTimed` 调用 `memory.Save()` 后只设置 dirty 标记并发送信号到 `persistCh`，不等待实际写盘完成。如果进程在 150ms 窗口内被杀（如更新程序直接终止进程），最新的对话历史丢失。

#### 根因 2：正常 agent loop 不创建 UnfinishedTaskSlot，进程被杀后无恢复标记

`UpsertUnfinishedSlot` 只在两个场景被调用：`max_rounds` 达到上限时、编程会话退出时。普通 agent loop 任务（如论文查找）在执行中途被进程杀死时，没有任何代码路径创建 UnfinishedTaskSlot。重启后系统不知道上一次任务是非正常中断的，无论用户说什么（"继续"、"上次那个论文呢"、"接着搞"），LLM 都缺少恢复上下文。

**为什么不用关键词匹配**：之前的方案 B 用 `shouldResumeIncompleteTask("继续")` 关键词匹配来触发恢复——这是 workaround。用户说"接着搞"、"上次那个呢"、"论文找到了吗"都会漏掉。真正的问题不是"用户说了继续"，而是"系统不知道上一次 agent loop 是非正常中断的"。

**修复**：

#### 方案 A：同步持久化 + Shutdown 强制 Flush

- `corelib/agent/conversation_memory.go`：新增 `FlushNow()` 导出方法，同步调用 `flushDirty()`，绕过 debounce timer
- `gui/im_message_handler.go`：`saveConversationHistoryTimed()` 在 `memory.Save()` 后立即调用 `memory.FlushNow()`，确保每次保存都同步写盘
- `gui/app.go`：`shutdown()` 中在 `aiConversationMemory.Stop()` 前调用 `FlushNow()`
- `tui/app.go`：TUI 侧同步添加 `FlushNow()` + `Stop()`

#### 方案 B：In-Flight Task Marker（飞行中任务标记）

**机制**：在 `conversationSession` 和 `persistedSession` 中新增 `inFlightTask` / `InFlightTask` 字段。

1. **Agent loop 开始时**：`SetInFlightTask(userID, task)` 写入标记 + `FlushNow()` 同步写盘
2. **Agent loop 正常结束时**（defer）：`ClearInFlightTask(userID)` 清除标记 + `FlushNow()`
3. **进程被杀时**：标记留在磁盘上（因为步骤 1 已同步写盘，步骤 2 的 defer 没机会执行）
4. **重启后第一条消息**：`ConsumeInFlightTask(userID)` 原子读取并清除标记。如果非空，说明上一次 agent loop 被中断，自动创建 `UnfinishedTaskSlot`（source="in_flight_recovery"），注入恢复上下文

**关键设计**：
- 不依赖用户消息内容——无论用户说什么，只要 in-flight 标记存在，就触发恢复
- 复用已有的 `UnfinishedTaskSlot` + `buildUnfinishedSlotHint` + `buildUnfinishedSlotResumeContext` 恢复机制，不新增恢复路径
- `ConsumeInFlightTask` 是一次性操作（读后清除），不会在后续消息中重复触发
- 标记在 `runAgentLoop` 的 defer 中清除，覆盖所有退出路径（正常完成、cancel、panic、max-rounds、drift 等）

**修改文件**：
- `corelib/agent/conversation_memory.go`：
  - `conversationSession` 新增 `inFlightTask string` 字段
  - `persistedSession` 新增 `InFlightTask string` JSON 字段
  - 新增 `SetInFlightTask()`、`ClearInFlightTask()`、`ConsumeInFlightTask()` 方法
  - 新增 `FlushNow()` 方法
  - `saveToDisk()` / `loadFromDisk()` 传递 `InFlightTask`
- `gui/im_message_handler.go`：
  - `runAgentLoop()` 开始时 `SetInFlightTask` + `FlushNow`，defer 中 `ClearInFlightTask` + `FlushNow`
  - `handleIMMessageWithLoop()` 加载 `unfinishedSlot` 后，检查 `ConsumeInFlightTask`，非空则自动创建 `UnfinishedTaskSlot` 并 bind
  - `saveConversationHistoryTimed()` 每次保存后 `FlushNow()`
- `gui/app.go`：`shutdown()` 中 `FlushNow()` + `Stop()`
- `tui/app.go`：`FlushNow()` + `Stop()`

**验收标准**：
- maclaw 更新重启后，用户说任何话 → 系统检测到 in-flight 标记 → 自动创建 UnfinishedTaskSlot → 显示"检测到一个未完成任务"提示 + 恢复按钮
- 用户点击"继续上次任务" → LLM 有完整对话历史 + 恢复上下文，继续之前的任务
- 用户点击"开始新任务" → 清除历史，正常开始
- 正常 agent loop 完成后 in-flight 标记被清除，不影响后续消息
- 所有 9 个 ConversationMemory 测试通过
- 所有 Trace/Resume 相关测试通过

### 56. 项目进行中失忆——对话历史扁平存储 + 截断/比较不区分结构层次

**来源**：用户在编程项目进行过程中（FileAPITester 已完成，RegistryAPITester 需求已确认），发送"开始开发吧"后，maclaw 只知道 FileAPITester 完成了，不知道 RegistryAPITester 的需求文档。用户说"你再查查看一下记忆"后，maclaw 通过 memory recall 才找回两个项目的完整状态。

#### 核心问题：对话历史的数据模型是扁平的

`[]ConversationEntry` 把所有信息扁平存储——用户的任务请求、LLM 的规划文档、工具调用细节、工具返回结果，全部混在一个列表里。三个消费方（trimHistory、TopicDetector、compactHistory）都不区分信息的结构层次。

#### 结构性不变量：Turn Boundary

对话历史中存在一个与内容无关的结构性不变量——**turn boundary**：
- 每个 user→assistant 交互的第一条 user 消息 = 用户的任务请求
- 紧跟其后的第一条 assistant 消息 = LLM 的规划/响应

识别方式完全基于 role 序列（`prevRole != "user"` → 新 turn；`prevRole == "user"` → 第一条 assistant 响应），不依赖任何关键词。任何语言、任何项目类型都适用。

#### Fix 1: trimHistory 两层截断

- `gui/im_conversation_trim.go`：`trimHistory` 从 FIFO "保留最后 N 条" 改为两层截断
  - Tier 1（turn boundaries）：每个 user→assistant 交互的第一条消息。预算 maxTier1=10
  - Tier 2（执行细节）：其余所有 entries。预算 MaxConversationTurns - outsideTier1
  - 中间插入 `[...中间的工具调用和执行细节已省略...]` 分隔符
  - 当所有 tier-1 在 recent window 内时，回退到简单 FIFO

#### Fix 2: TopicDetector context 用 turn boundaries

- `gui/im_topic_detector.go`：`detect()` 的 context 构建从"最后 8 条 user+assistant texts"改为"turn boundary texts + 最后 2 条 recency texts"
  - 旧方法在 30+ 轮工具调用后，最后 8 条全是执行细节（"file created"、"step 15 done"），与用户新消息零词汇重叠
  - 新方法用 turn boundaries 代表话题——用户的任务请求和 LLM 的规划才是话题的定义

#### Fix 3: compactHistory 摘要输入用 turn boundaries

- `gui/im_message_handler.go`：摘要的输入从 JSON 序列化的全部 entries 改为 turn boundary texts 的人类可读格式
  - 旧方法把 `{"role":"assistant","content":"...","tool_calls":[...]}` 格式的 JSON 扔给 LLM
  - 新方法给 LLM `[user] 开发一个项目\n[assistant] 好的，我来规划...` 格式
  - 每条 turn boundary 截断到 500 rune

**测试**：
- `gui/im_conversation_trim_anchor_test.go`：6 个测试
  - `TestTrimHistory_SmallHistory_NoTrimming`：小历史不截断
  - `TestTrimHistory_TwoTierPreservesTurnBoundaries`：turn boundaries 被保留
  - `TestTrimHistory_StructuralInvariant_NoKeywordDependency`：完全随机内容也能保留 turn boundaries（证明不依赖关键词）
  - `TestTrimHistory_NoOrphanedToolMessages`：不产生 orphaned tool messages
  - `TestTrimHistory_SeparatorBetweenTiers`：分隔符被插入
  - `TestTrimHistory_FallsBackToFIFO_WhenNoTier1Outside`：FIFO 回退
- `gui/im_topic_detector_project_test.go`：2 个测试
  - `TestTopicDetector_TurnBoundaryContext_SameProject`：turn boundary context 有词汇重叠
  - `TestTopicDetector_TurnBoundaryContext_GenuineNewTopic`：真正的话题切换仍被检测

**验收标准**：
- 编程项目跨 100+ entries 后，trimHistory 保留所有 turn boundaries
- TopicDetector 的 BM25 比较基于 turn boundaries 而非执行细节
- compactHistory 的摘要输入是人类可读文本而非 JSON
- 以上三点不依赖任何关键词（TestTrimHistory_StructuralInvariant_NoKeywordDependency 验证）
- 所有 17 个测试通过 + 11 个现有 TopicDetector 测试通过
- GUI / corelib / TUI 编译通过



### 57. TUI 助手聊天区滚动条缺失 + 用户消息回显延迟 + LLM 响应无流式显示

**来源**：用户反馈"内容多时没有滚动条，无法知道是多屏内容"和"用户提问后并不能立即回显，需要等 agent 有结果后才一起显示"。

#### 机制层面根因分析

##### 根因 1: `renderLines()` 无缓存——O(n²) 重复计算阻止了滚动条的引入

`renderLines()` 是纯计算函数（遍历所有 messages → Markdown 渲染 → 文本换行），但在一个事件周期内被**多次调用**：
- `scrollToBottom()` 调用一次（每次 ChatStreamMsg/ChatResponseMsg/AppendSystemMessage 都触发）
- `scrollDown()` 调用一次
- `View()` 主体渲染调用一次

没有缓存意味着每次需要 `totalLines` 信息（滚动条、状态栏百分比）都要重新渲染全部消息。这是滚动条不存在的**结构性原因**——加滚动条需要在 `View()` 中额外读取 `totalLines`，但每次读取都是一次完整的 `renderLines()` 调用。

**机制性修复**：引入 `cachedLines` + `cacheValid` 标记。`invalidateCache()` 在任何修改 messages/width/waiting/spinnerTick 的操作中调用。`getLines()` 是唯一的缓存入口——首次调用时计算并缓存，后续调用直接返回。`scrollToBottom`、`scrollDown`、`View` 全部通过 `getLines()` 读取。一个事件周期内 `renderLines()` 最多执行一次。

##### 根因 2: `pendingText` + `tea.Tick` 间接调度——违反 Bubble Tea 事件模型

之前的代码用 `pendingText` + `tea.Tick(0/16ms, tuiStartLoopMsg)` 在 `ChatSendMsg` 和 `handleChatSend` 之间插入延迟，"希望"中间有一次渲染。这是对 Bubble Tea 事件模型的误解。

Bubble Tea 的事件循环是 `processMsg → render → processMsg → render`。`ChatModel.Update` 处理 Enter 键时添加 user message 并返回 `ChatSendMsg` 作为 Cmd。Bubble Tea 在执行 Cmd 之前**必定**调用 `View()` 渲染。所以用户消息在 `ChatSendMsg` 被处理之前**已经可见**。`pendingText` + `tuiStartLoopMsg` 是多余的间接层。

**机制性修复**：删除 `pendingText`、`tuiStartLoopMsg`、`tea.Tick` 间接调度。`ChatSendMsg` 直接调用 `handleChatSend(msg.Text)`。

##### 根因 3: `RunLoop` 使用非流式 LLM 请求——`OnToken` 回调从未被调用

`RunLoop` 调用 `doLLMRequestWithTools` → `llm.DoOpenAIRequest(stream: false)`。整个 HTTP 响应在服务端生成完毕后才返回。`LoopCallbacks` 接口定义了 `OnToken(delta string)`，但 `RunLoop` 的实现**从不调用它**（代码注释明确说明）。用户在 LLM 思考期间（10-30 秒）只看到 spinner，响应文本突然全部出现。

这是架构层面的断裂：接口声明了流式能力，但实现不使用它。

**机制性修复**：
- `corelib/llm/stream.go`：新增 `DoOpenAIRequestStream` 和 `DoAnthropicRequestStream`，发送 `stream: true` 请求，逐 SSE chunk 调用 `onToken` 回调
- SSE/JSON 自动检测：读取完整响应后检测格式——以 `data:` 开头为 SSE，否则回退到 JSON 解析（兼容不支持流式的 API 代理）
- `corelib/agent/loop.go`：新增 `doLLMRequestWithToolsStream`，`RunLoop` 的 LLM 调用改为流式版本，`cb.OnToken` 在每个 text delta 时被调用
- 流式失败时自动 fallback 到非流式 `doLLMRequestWithTools`

#### 修改文件

##### `tui/views/chat.go`（重写）

- `ChatModel` 新增 `cachedLines []string` + `cacheValid bool` 字段
- 新增 `invalidateCache()` 方法：所有修改 messages/width/waiting/spinnerTick 的操作调用
- 新增 `getLines()` 方法：缓存入口，首次调用计算，后续直接返回
- `scrollToBottom()`、`scrollDown()` 从 `renderLines()` 改为 `getLines()`
- `View()` 从 `renderLines()` 改为 `getLines()`——整个 View() 只调用一次
- `View()` 新增滚动条渲染：`buildScrollTrack()` 基于 `getLines()` 的缓存结果，零额外开销
- `View()` 状态栏新增 `↕N%` 滚动位置百分比
- `tool_call` 处理：中间 assistant 消息清理使用 `lastAssistantAfterUser()` 精确判断，不盲目删除

##### `tui/app.go`

- 删除 `pendingText string` 字段
- 删除 `tuiStartLoopMsg` 类型
- 删除 `tea.Tick(16ms, tuiStartLoopMsg)` 间接调度
- `ChatSendMsg` 直接调用 `handleChatSend(msg.Text)`

##### `corelib/llm/stream.go`（新文件）

- `DoOpenAIRequestStream()`：发送 `stream: true` 的 OpenAI 兼容请求
- `DoAnthropicRequestStream()`：发送 Anthropic 流式请求
- `parseSSEStream()`：解析 OpenAI SSE 格式，累积 tool calls，流式输出 text delta
- `parseAnthropicSSEStream()`：解析 Anthropic SSE 格式
- SSE/JSON 自动检测：兼容不支持流式的 API 代理

##### `corelib/agent/loop.go`

- 新增 `doLLMRequestWithToolsStream()`：流式 LLM 请求 + 自动 fallback
- `RunLoop` 的 LLM 调用从 `doLLMRequestWithTools` 改为 `doLLMRequestWithToolsStream`

##### `corelib/agent/loop_test.go`

- `TestRunLoop_NoToolCalls_ReturnsFinalText`：更新断言——`OnToken` 现在被调用（通过 JSON fallback 路径）

#### 彻查：同类问题排查

1. **`session_detail.go` 的 scroll**：使用预构建的 `m.lines` 切片（`AppendOutput` 时追加），不存在重复计算问题。无需修改。
2. **其他 TUI views**：`tools.go`、`memory.go`、`audit.go`、`schedule.go` 使用固定列表渲染，无 `renderLines()` 模式。无需修改。
3. **`root.go` 的 `updateActiveTab`**：消息路由正确——`ChatStreamMsg` 始终路由到 `ChatModel`，即使当前 tab 不是 Chat。无需修改。

**验收标准**：
- 内容超出视口时，右侧显示蓝色滚动条滑块 + 灰色轨道
- 状态栏显示 `↕N%` 滚动位置百分比
- 用户按 Enter 后消息立即显示（Bubble Tea 事件模型保证，无需人工延迟）
- LLM 响应逐 token 流式显示，不再一次性出现
- 不支持 SSE 的 API 代理自动回退到 JSON 解析
- `renderLines()` 在一个事件周期内最多执行一次（缓存机制）
- 所有 8 个 RunLoop 测试通过 + 所有 LLM 包测试通过


### 57. Skill 搜索/安装逻辑分散在 5 个独立实现中——统一到 corelib/skill.HubClient

**来源**：用户报告 TUI 工具面板的 Skill 搜索只能搜索 SkillMarket，无法搜索 ClawHub；按 Enter 无法安装，只提示用户输入命令。

#### 根因（机制性分析）

问题不是"TUI 少了 ClawHub 搜索"——这只是表象。根因是**搜索和安装逻辑没有统一的共享层**。

彻查发现 5 条独立的搜索实现和 4 条独立的安装实现：

| 消费方 | 搜索源 | 安装能力 | ClawHub URL 定义 |
|--------|--------|---------|-----------------|
| GUI `skill_searcher.go` SearchAll | SkillHub + ClawHub + GitHub | 有 | `const ClawHubMirrorURL` |
| GUI `skillhub_client.go` Search | SkillHub only | 有（含依赖安装+文件提取） | N/A |
| TUI `app.go` searchSkills | SkillHub only ❌ | 占位（返回提示文本）❌ | N/A |
| TUI `tool_manage_skill.go` skillSearch | SkillHub only ❌ | 有（但无 ClawHub）❌ | N/A |
| TUI CLI `skillhub.go` skillhubSearch | SkillHub + GitHub | 有（但无 ClawHub）❌ | N/A |
| TUI CLI `skill_search_api.go` SearchSkillHub | SkillHub only ❌ | N/A | N/A |

每条路径各自定义 HTTP 请求逻辑、JSON 解析结构体、URL 常量。新增搜索源或修改 API 格式需要改 5 个文件。ClawHub URL 在 4 个地方重复定义。

#### 修复：统一到 `corelib/skill.HubClient`

**设计原则**：搜索和安装的 HTTP 逻辑只实现一次，放在 `corelib/skill/` 中。所有消费方（GUI、TUI UI、TUI agent tool、TUI CLI）共享同一套代码。新增搜索源只改 `hub_search.go` 一个文件。

##### 新增 `corelib/skill/hub_search.go`

- `ClawHubMirrorURL` 常量：**单一数据源**，所有消费方引用此常量
- `HubSearchResult` 结构体：统一搜索结果类型，消费方映射到自己的显示类型
- `HubClient` 结构体：统一的 HTTP 客户端
  - `SearchAll(ctx, hubURL, query)` → 聚合 SkillHub + ClawHub 结果
  - `SearchSkillHub(ctx, hubURL, query)` → 查询 SkillHub API
  - `SearchClawHub(ctx, query)` → 查询 ClawHub 中国镜像
  - `DownloadSkillHub(ctx, hubURL, skillID)` → 下载 SkillHub Skill → `*NLSkillEntry`
  - `DownloadClawHub(ctx, slug)` → 下载 ClawHub Skill → `*NLSkillEntry`（含 craft_tool 步骤）
- 内部 JSON 响应类型：`skillHubSearchResponse`、`clawHubSearchResponse`、`clawHubSkillResponse` 等

##### 消费方改造

- `tui/app.go`：`searchSkills()` 从 ~100 行内联 HTTP 代码改为 3 行调用 `skill.NewHubClient().SearchAll()`；`installSkill()` 从占位实现改为调用 `client.DownloadSkillHub()` / `client.DownloadClawHub()`
- `tui/tool_manage_skill.go`：`skillSearch()` 从 ~60 行内联 HTTP 代码改为 3 行调用；`skillInstall()` 从 ~40 行内联 HTTP 代码改为调用共享客户端
- `tui/commands/skill_search_api.go`：`SearchSkillHub()` 从 ~40 行内联 HTTP 代码改为委托 `skill.NewHubClient().SearchAll()`
- `tui/commands/skillhub.go`：`skillhubSearch()` 的 fallback 链从 SkillHub → GitHub 扩展为 SkillHub → ClawHub → GitHub，ClawHub 搜索使用共享客户端
- `gui/skill_searcher.go`：`ClawHubMirrorURL` 改为 re-export `cskill.ClawHubMirrorURL`（单一数据源）

##### TUI UI 确认机制

- `tui/views/tool_status.go`：
  - `SkillSearchResult` 新增 `Source` 字段
  - `ToolSkillInstallMsg` 新增 `Source` 字段
  - `ToolStatusModel` 新增 `skillConfirming` / `skillConfirmIdx` 字段
  - Enter 键显示确认对话框 `确认安装 xxx（来源: SkillHub/ClawHub）？ [Y/n]`
  - `IsEditing()` 包含 `skillConfirming` 检查

#### 扩展方式

新增搜索源（如 npm registry、PyPI 等）只需：
1. 在 `hub_search.go` 中新增 `SearchXxx()` 方法
2. 在 `SearchAll()` 中追加调用
3. 所有 5 个消费方自动获得新源——不需要改任何消费方代码

修改 API 格式（如 ClawHub 改了 JSON 字段名）只需改 `hub_search.go` 中的响应类型定义。

#### 删除的重复代码

- `tui/app.go`：删除 `searchSkillHub()`、`searchClawHub()`、`installSkillHubSkill()`、`installClawHubSkill()`、`clawHubMirrorURL` 常量（~250 行）
- `tui/tool_manage_skill.go`：删除 `skillSearch()` 和 `skillInstall()` 中的内联 HTTP 代码（~100 行）
- `tui/commands/skill_search_api.go`：删除 `SearchSkillHub()` 中的内联 HTTP 代码（~40 行）

**验收标准**：
- `ClawHubMirrorURL` 在整个代码库中只有一个定义（`corelib/skill/hub_search.go`），其他位置是 re-export
- TUI 搜索 "pdf" → 同时显示 SkillHub 和 ClawHub 的结果
- TUI 按 Enter → 确认对话框 → 确认后实际安装
- TUI CLI `skillhub search pdf` → SkillHub 无结果时 fallback 到 ClawHub → GitHub
- LLM 调用 `manage_skill(action="search")` → 同时搜索 SkillHub + ClawHub
- 所有 corelib/skill + tui 测试通过

### 57.1 GitHub 搜索未接入 HubClient.SearchAll——三源搜索名不副实

**来源**：用户反馈搜索结果中基本没有 GitHub 结果。

**根因**：`HubClient.SearchAll()` 的注释写了"Sources: 1. SkillHub 2. ClawHub 3. GitHub"，但代码只调用了 `SearchSkillHub` + `SearchClawHub`，没有调用 `SearchGitHub`。`GitHubSearcher` 在同一个包中已经实现完整，但没有被 `HubClient` 集成。GUI 的 `SearchAll()` 独立调用了 `cskill.NewGitHubSearcher().SearchGitHub()`，但 TUI 的所有路径都委托给 `HubClient.SearchAll()`，所以 TUI 完全没有 GitHub 结果。

这是上一轮修复的遗漏——创建了统一客户端但没有把已有的 GitHub 搜索接入。

**修复**：

#### 1. `HubClient` 集成 GitHub 搜索（`corelib/skill/hub_search.go`）

- `HubClient` 新增 `githubToken` 字段，从 `GITHUB_TOKEN` 环境变量读取
- 新增 `SearchGitHub(query)` 方法：委托给同包的 `GitHubSearcher.SearchGitHub()`，将 `GitHubSkillCandidate` 转换为 `HubSearchResult`
- `SearchAll()` 追加 `c.SearchGitHub(query)` 调用
- `HubSearchResult` 新增 GitHub 专用字段：`RepoURL`、`FilePath`、`InstallRef`（JSON 序列化的 `GitHubSkillCandidate`）
- 新增 `DownloadGitHub(ctx, installRef)` 方法：反序列化 `InstallRef` → 委托给 `GitHubSearcher.ImportFromCandidate()`

#### 2. TUI 消费方适配

- `tui/views/tool_status.go`：
  - `SkillSearchResult` 新增 `InstallRef` 字段
  - `ToolSkillInstallMsg` 新增 `InstallRef` 字段
  - 来源标签新增 `"gh"` 显示
  - 确认对话框新增 `"GitHub"` 来源标签
- `tui/app.go`：
  - `searchSkills` 映射传递 `InstallRef`
  - `installSkill` 新增 `github` 分支，调用 `client.DownloadGitHub(ctx, installRef)`
- `tui/tool_manage_skill.go`：
  - `skillInstall` 新增 `github` 分支 + `install_ref` 参数

#### 机制性保证

所有消费方（TUI UI、TUI agent tool、TUI CLI、GUI）调用 `HubClient.SearchAll()` 时自动获得三源结果。新增第四个搜索源只需：
1. 在 `hub_search.go` 中新增 `SearchXxx()` 方法
2. 在 `SearchAll()` 中追加一行调用
3. 零消费方代码变更

**验收标准**：
- TUI 搜索 "pdf" → 结果中包含 SkillHub、ClawHub、GitHub 三个来源的结果，各自标注来源
- TUI 选中 GitHub 结果按 Enter → 确认对话框显示"来源: GitHub" → 确认后下载并安装
- TUI CLI `skillhub search pdf` → SkillHub 无结果时 fallback 到 ClawHub → GitHub
- LLM 调用 `manage_skill(action="search")` → 三源搜索
- LLM 调用 `manage_skill(action="install", source="github", install_ref="...")` → GitHub 安装
- 所有 corelib/skill + tui 测试通过

### 57.2 GitHub 搜索静默失败——Code Search API 需要认证但代码传空 token

**来源**：用户反馈 GUI 和 TUI 搜索结果中基本没有 GitHub 结果。

**根因**：`GitHubSearcher` 使用 GitHub **Code Search API** (`/search/code`)，该 API 从 2023 年起要求认证（返回 401 Requires authentication）。所有调用方传空 token（`NewGitHubSearcher("")`），所有请求返回 401，`httpGet` 捕获为错误，`SearchGitHub` 返回 `nil, error`。GUI 的 `SearchAll` 把错误追加到 `errs` 但不返回给用户（只在所有源都失败时才返回错误），所以 GitHub 搜索是**静默失败**。

这不是"TUI 没接入 GitHub"的问题——GUI 也一样没有 GitHub 结果。根因是 API 选型错误。

**机制性修复**：改用 GitHub **Repository Search API** (`/search/repositories`)，该 API 不需要认证。用 `topic:skill` 过滤精准找到 skill 仓库（GitHub 上有 688+ 个带 `skill` + `claude-code` topic 的仓库）。

#### 1. `SearchGitHub` 改为双层策略（`corelib/skill/github_search.go`）

- **Primary**：Repository Search API（`/search/repositories?q=<query>+topic:skill&sort=stars`）——不需要认证，按 star 排序
  - 新增 `searchGitHubByRepo()` 方法和 `ghRepoSearchResponse` 类型
  - 返回的 candidate 默认 `FilePath: "SKILL.md"`（最常见的 skill 定义文件）
- **Fallback**：Code Search API（`/search/code?q=filename:skill.md+<query>`）——仅当 `gs.token != ""` 时执行
  - 保留原有的 `searchGitHubByFilename()` 逻辑
  - 结果与 repo search 按 `RepoFullName` 去重

#### 2. `ImportFromCandidate` 多路径 fallback（`corelib/skill/github_search.go`）

Repo search 返回的 candidate 默认 `FilePath: "SKILL.md"`，但实际文件可能是 `skill.yaml` 或 `skill.md`。

- 首次下载 `RawURL` 失败后，依次尝试 `SKILL.md` → `skill.md` → `skill.yaml`
- 跳过已尝试的路径
- 全部失败才返回错误

#### 3. `HubClient` 传递 `GITHUB_TOKEN`（`corelib/skill/hub_search.go`）

- `NewHubClient()` 从 `os.Getenv("GITHUB_TOKEN")` 读取 token
- 传递给 `NewGitHubSearcher(c.githubToken)`
- 有 token 时 Code Search 作为补充源，无 token 时仅用 Repo Search

#### 4. 测试更新（`corelib/skill/github_search_test.go`）

- `TestSearchGitHubReturnsCombinedCandidates`：fake HTTP client 新增 repo search 路径处理；使用 `NewGitHubSearcher("test-token")` 启用 Code Search fallback

**验收标准**：
- 无 GITHUB_TOKEN 时：搜索 "pdf" → 返回 GitHub 上带 `topic:skill` 的 PDF 相关仓库
- 有 GITHUB_TOKEN 时：额外返回 Code Search 结果（去重）
- `ImportFromCandidate` 对 repo search 结果自动尝试多个文件路径
- GUI 和 TUI 都能看到 GitHub 搜索结果
- 所有 corelib/skill + tui 测试通过


### 58. TUI Markdown 渲染问题——流式内容不完整导致原始标记泄漏 + 段落不换行 + CJK 宽度计算错误

**来源**：用户截图显示 TUI 聊天区的 Markdown 渲染有问题——`**bold**` 标记未被剥离、表格显示为原始管道符文本。

#### 根因（机制性分析）

问题不是"某个正则没匹配到"——`RenderMarkdown` 对完整内容的渲染完全正确（所有 23 个现有测试通过）。根因是**流式内容不完整时，Markdown 解析器无法识别未闭合的语法结构**。

三个独立问题叠加：

1. **流式内容不完整**：LLM 通过 SSE 逐 token 推送内容，`text_delta` 追加到 assistant 消息后立即触发 `renderLines()` → `RenderMarkdown()`。此时内容可能是 `📁 文件：**HuggingFace_Daily_Papers`（缺少闭合 `**`），`processInlinePattern` 找不到闭合分隔符，原始 `**` 标记直接显示。同理，单行表格 `| 章节 | 内容 |` 因为没有后续行，不满足"至少 2 行连续表格行"的检测条件，原始 `|` 管道符直接显示。

2. **段落不换行**：`RenderMarkdown` 的普通段落路径只做 `renderInlineMarkdown(line)` + `"  "` 前缀，不做换行。超长段落溢出终端宽度。

3. **`wrapLine` 用 rune 计数而非显示宽度**：`chat.go` 的 `wrapLine` 用 `len([]rune(s))` 判断是否需要换行，CJK 字符（显示宽度 2）被当作宽度 1 计算，导致中文文本在错误位置换行。

#### 修复（三层机制性修复）

##### 1. 流式孤立分隔符清理（`tui/views/markdown.go`）

新增 `cleanOrphanedDelimiters()` 函数，在 `RenderMarkdown` 入口处对文本最后一行（流式截断点）执行孤立分隔符清理：

- **`**` / `__` 孤立标记**：`cleanOrphanedPairMarker()` 统计标记出现次数，奇数次说明有未闭合的标记，移除最后一个
- **`` ` `` 孤立反引号**：奇数个反引号时移除最后一个
- **`[text](url` 孤立链接**：检测 `](` 后无 `)` 的情况，提取链接文本显示
- **`[text` 孤立方括号**：检测 `[` 后无 `]` 的情况，移除孤立 `[`
- **代码块感知**：统计 prefix 中的 ``` 围栏数量，奇数说明最后一行在代码块内，不做任何清理

##### 2. 孤立表格行降级渲染（`tui/views/markdown.go`）

在多行表格检测之后新增孤立表格行处理：当一行看起来像表格行（`isTableLine` 返回 true）但没有后续表格行时，用 `parseTableCells` 提取单元格内容，用双空格连接后作为普通文本渲染。用户看到 `章节  内容` 而非 `| 章节 | 内容 |`。

##### 3. 段落显示宽度换行（`tui/views/markdown.go` + `tui/views/chat.go`）

- `markdown.go`：新增 `wrapToWidth()` 函数，对渲染后的文本（可能含 ANSI 转义码）按显示宽度换行。正确处理 CJK 字符（宽度 2）和 ANSI 转义序列（宽度 0）。普通段落路径从 `result = append(result, "  "+rendered)` 改为 `wrapToWidth(rendered, maxWidth-2)` 后逐行追加。
- `chat.go`：`wrapLine()` 从 `len([]rune(s))` 改为 `displayWidth(s)` 判断，换行位置按显示宽度计算。CJK 字符不再在错误位置被截断。

**设计要点**：
- `cleanOrphanedDelimiters` 只处理最后一行——流式截断只发生在最后一行，处理所有行会误伤合法的单 `*`（如乘法符号）
- 代码块内容不受影响——`**kwargs` 等 Python 语法不会被误清理
- 孤立表格行降级是渐进增强——当后续行到达后，完整表格会被正确渲染为对齐列
- `wrapToWidth` 在 ANSI 转义序列中间截断时追加 `\x1b[0m` 重置，防止样式泄漏到下一行

**修改文件**：
- `tui/views/markdown.go`：
  - `RenderMarkdown()` 入口新增 `cleanOrphanedDelimiters()` 调用
  - 新增 `cleanOrphanedDelimiters()` 函数（最后一行孤立分隔符清理）
  - 新增 `cleanOrphanedPairMarker()` 函数（配对标记奇偶检测）
  - 新增 `wrapToWidth()` 函数（ANSI 感知的显示宽度换行）
  - 普通段落路径改为 `wrapToWidth` 换行
  - 多行表格检测后新增孤立表格行降级渲染
- `tui/views/chat.go`：
  - `wrapLine()` 从 rune 计数改为 `displayWidth()` 显示宽度计算

**测试**：
- `tui/views/markdown_repro_test.go`：17 个新增测试
  - `TestScreenshotRepro`：完整截图内容渲染验证
  - `TestBoldInParagraph`：段落中的加粗渲染
  - `TestBoldWithFullwidthColon`：全角冒号后的加粗（5 个子用例）
  - `TestWrapLineBreaksMarkdown`：窄宽度下加粗文本换行
  - `TestStreamingPartialBold`：流式未闭合 `**` 标记清理
  - `TestStreamingPartialTable`：流式单行表格降级渲染
  - `TestStreamingPartialInlineCode`：流式未闭合反引号
  - `TestStreamingPartialLink`：流式未闭合链接（3 个子用例）
  - `TestStreamingMultipleOrphanedBold`：多个加粗段中第二个未闭合
  - `TestOrphanedDelimitersInCodeBlock`：代码块内 `**kwargs` 不被清理
  - `TestOrphanedDelimitersUnclosedCodeBlock`：未闭合代码块内容不被清理
  - `TestEmojiInTableCells`：含 emoji 的表格渲染
  - `TestTableFollowedByParagraph`：表格后接段落
  - `TestMultipleTablesWithTextBetween`：多表格间隔文字

**验收标准**：
- 流式输出 `**bold` 时不显示原始 `**` 标记
- 流式输出单行表格时不显示原始 `|` 管道符
- 代码块内的 `**kwargs` 不被误清理
- 超长中文段落在正确的显示宽度位置换行
- 所有 40 个 tui/views 测试通过
- 全项目编译通过


### 59. 翻译论文中途停止——Agent 无法监控异步进程 + 漂移检测误杀合法等待

**来源**：用户让 maclaw 翻译论文 "Reward Hacking in the Era of Large Models"，maclaw 通过 `open` 工具启动 babeldoc 翻译脚本（.bat），翻译在独立 cmd 窗口中运行。Agent 在等待翻译完成的过程中被漂移检测器终止。

**根因**（三个机制性问题叠加）：

#### 问题 1 (P0): `read_file` 不支持读取文件尾部

`toolReadFile` 的 `lines` 参数语义是"从头读取前 N 行"。对于正在增长的日志文件，Agent 需要"读取最后 N 行"（类似 `tail -n`），但工具不支持。LLM 反复调用 `read_file(lines=500)` 每次都返回文件开头的相同内容，无法看到翻译进度。LLM 甚至说了"让我跳到文件末尾看看最新进展"，但工具不支持这个操作。

**修复**：
- `gui/im_tool_definitions.go`：`read_file` 工具定义新增 `offset` 参数（`"从文件末尾倒数的行数开始读取，类似 tail -n"`）
- `gui/im_tools_local.go`：`toolReadFile()` 新增 `offset` 处理逻辑——当 `offset` 参数存在时，从文件末尾倒数 N 行开始读取，返回格式 `"... (跳过前 X 行，显示最后 N 行，共 Y 行)\n{内容}"`
- `offset` 与 `lines` 互斥，优先使用 `offset`

#### 问题 2 (P1): 漂移检测器不区分紧密循环和慢速轮询

`list_directory` 连续 3 次返回相同结果（`共 0 项`），触发漂移检测。但这 3 次调用的时间跨度是 22 秒（06:35:58 → 06:36:20），中间穿插了 screenshot 调用。LLM 不是在紧密循环中，而是在慢速轮询等待异步进程完成。

**修复**：
- `gui/drift_detector.go`：新增 `slowPollTimeSpan`（15s）和 `slowPollMinRepeat`（5 次）常量
- `DetectDrift()` 在检测到连续相同调用后，计算时间跨度。如果跨度超过 `slowPollTimeSpan`，提高触发阈值到 `slowPollMinRepeat`（5 次），给 LLM 更多轮询机会
- `PreviewDrift()` 同步应用相同的时间跨度检查
- 紧密循环（<15s 内 3 次相同调用）仍然按原阈值触发

#### 问题 3 (P1): 漂移 recover 提示缺乏可操作建议

`buildDriftRecoverPrompt()` 只说"换个方法"，不告诉 LLM 具体该用什么替代工具。LLM 在 recover 后只能切换到 screenshot（30s 冷却浪费大量迭代），然后又回到 list_directory，再次触发漂移。

**修复**：
- `gui/im_message_handler.go`：`buildDriftRecoverPrompt()` 新增针对特定工具的可操作替代建议：
  - `list_directory` 漂移 → 建议使用 `bash` 检查进程状态、`read_file(offset=50)` 读取日志尾部
  - `read_file` 漂移 → 建议使用 `read_file(offset=...)` 读取文件尾部
  - `screenshot` 漂移 → 建议使用 `bash` 或 `read_file(offset=...)` 替代

**Trajectory 数据证据**（`2026-04-24_06-36-20.617_chat.json`，712 entries）：
- 54 次 screenshot 调用，39 次 list_directory 调用，54 次 read_file 调用
- read_file 每次都返回文件开头相同内容（"Starting translation..."）
- list_directory 每次都返回"共 0 项"（翻译未完成）
- 第一次漂移（entry 648）：list_directory 连续 3 次相同结果
- 第二次漂移（entry 711）：list_directory 连续 3 次相同结果，NeedHumanHelp=true，Agent 终止
- 两次漂移之间 LLM 穿插了大量 screenshot 调用（30s 冷却导致浪费迭代）

**验收标准**：
- `read_file(offset=50)` 返回文件最后 50 行，LLM 能看到翻译进度
- 慢速轮询（调用间隔 >15s）需要 5 次连续相同结果才触发漂移，而非 3 次
- 漂移 recover 提示包含具体的替代工具建议
- 所有 15 个 DriftDetector 测试 + 1 个集成测试通过
- GUI 编译通过（无新增诊断错误）


### 60. 本机后台进程管理——从 workaround 到机制性修复

**来源**：#59 的修复中，`async_wait` 工具和漂移 recover 提示中的硬编码工具建议都是 workaround。`async_wait` 要求 LLM 自己猜测文件路径和 PID，漂移提示按工具名硬编码替代方案。根本问题是：**Agent 启动外部进程的方式（`open` fire-and-forget / `bash` 同步阻塞）和监控外部进程的方式之间存在机制性断裂**。

**机制性分析**：

SSH 后台任务已有正确模式：
- `Submit(command)` → 返回 task_id + log_file + PID（进程元数据与进程绑定）
- `CheckTask(task_id)` → 通过 PID 检查存活 + tail 日志（非阻塞查询）
- `KillTask(task_id)` → 通过 PID 终止

本机缺失对应机制：
- `open` → fire-and-forget，无 PID、无日志、无状态
- `bash` → 同步阻塞，有输出但卡迭代
- 没有 Submit / Check / Wait / Kill 的本机等价物

**修复**：复用 SSH 后台任务管理器的 Submit/Check/Wait/Kill 模式，在本机实现对称的后台进程管理。

#### 1. `corelib/tool/local_background.go`（新文件）

`LocalBackgroundTaskManager` — 本机后台进程管理器，与 `SSHBackgroundTaskManager` 对称：

- `Submit(command, workDir)` → 启动进程，stdout/stderr 重定向到日志文件，返回 `*LocalBackgroundTask`（含 TaskID、PID、LogFile）。后台 goroutine 监控进程退出并更新状态。
- `Check(taskID, tailLines)` → 非阻塞查询状态 + 日志尾部，返回 `*LocalTaskStatus`
- `Wait(taskID, timeout, tailLines)` → 阻塞等待进程退出或超时，通过 `task.doneC` channel 实现（不轮询）
- `Kill(taskID)` → 通过 context cancel 终止进程，3 秒后 force kill
- `List()` → 列出所有任务
- `Cleanup(maxAge)` → 清理已完成的旧任务和日志文件

关键设计：进程退出通过 `doneC` channel 通知（`close(task.doneC)`），`Wait` 用 `select` 等待 channel 或 timeout，零 CPU 开销。

#### 2. `bash` 工具新增 `background=true` 参数

`bash(command="babeldoc ...", background=true)` → 调用 `LocalBackgroundTaskManager.Submit()`，立即返回：
```
✅ 后台任务已启动
task_id: local_1714000000_1
PID: 12345
日志文件: ~/.maclaw/data/bg_tasks/local_1714000000_1.log

使用 async_wait(action="check", task_id="local_1714000000_1") 查询状态
使用 async_wait(action="wait", task_id="local_1714000000_1", timeout=60) 等待完成
```

进程元数据（PID、日志路径）由系统自动捕获，LLM 不需要猜测。

#### 3. `async_wait` 工具改造为后台任务管理接口

从独立的"sleep+check"工具变为 `LocalBackgroundTaskManager` 的查询接口：

- `async_wait(action="check", task_id="...")` → 非阻塞查询状态 + 日志尾部
- `async_wait(action="wait", task_id="...", timeout=60)` → 阻塞等待完成
- `async_wait(action="kill", task_id="...")` → 终止任务
- `async_wait(action="list")` → 列出所有后台任务

#### 4. 删除 workaround

- 删除 `buildDriftRecoverPrompt` 中按工具名硬编码的替代建议（`list_directory` → "用 bash 检查进程"等）
- 恢复为通用的 recover 提示

#### 机制对称性

| 操作 | SSH 远程 | 本机 |
|------|---------|------|
| 启动后台任务 | `ssh(action="submit_task")` | `bash(background=true)` |
| 查询状态 | `ssh(action="check_task")` | `async_wait(action="check")` |
| 等待完成 | — | `async_wait(action="wait")` |
| 终止任务 | `ssh(action="kill_task")` | `async_wait(action="kill")` |
| 列出任务 | `ssh(action="list_tasks")` | `async_wait(action="list")` |

#### 翻译论文场景的正确流程（修复后）

```
1. bash(command="babeldoc translate ...", background=true)
   → task_id: local_xxx, PID: 12345, 日志: ~/.maclaw/data/bg_tasks/local_xxx.log

2. async_wait(action="wait", task_id="local_xxx", timeout=120)
   → 阻塞等待，进程退出后返回状态 + 日志尾部
   → 或超时后返回当前状态 + 日志尾部

3. 根据返回的状态和日志内容决定下一步
```

消耗 2 个迭代，零漂移检测风险，LLM 不需要猜测任何路径或 PID。

#### 修改文件

- `corelib/tool/local_background.go`：新文件，`LocalBackgroundTaskManager` 完整实现
- `gui/im_tool_async_wait.go`：重写为后台任务管理接口（check/wait/kill/list）
- `gui/im_tools_local.go`：`toolBash` 新增 `background=true` 分支
- `gui/im_tool_definitions.go`：`bash` 新增 `background` 参数，`async_wait` 改为任务管理接口
- `gui/tool_registry_builtin.go`：更新 `async_wait` 注册
- `gui/im_message_handler.go`：新增 `localBgTaskMgr` 字段 + 删除 recover 提示 workaround

**验收标准**：
- `bash(command="sleep 3 && echo done", background=true)` 立即返回 task_id
- `async_wait(action="check", task_id="...")` 返回 running + 日志尾部
- `async_wait(action="wait", task_id="...", timeout=10)` 3 秒后返回 completed + 日志
- `async_wait(action="kill", task_id="...")` 终止运行中的任务
- `buildDriftRecoverPrompt` 不包含任何硬编码工具名
- 所有 Drift / Registry / Router / CoreToolNames 测试通过
- GUI 和 corelib 编译通过


### 61. TUI 彻底移除编程工具——平台能力声明机制

**根因**：TUI 的终端模型是独占式的——Bubble Tea 占据整个终端（alt-screen mode），编程工具（claude code 等）也需要完整的终端控制。两者不能同时占据同一个终端。GUI 通过 stdin/stdout pipe 管理编程工具子进程（后台进程管理模型），TUI 无法复制这个模型。

TUI 中 `create_session`、`send_and_observe` 等编程会话工具从未被注册到 `CoreToolRegistry`（`RegisterCoreTools` 不包含这些工具），但 system prompt 中的编码工作流规则仍然告诉 LLM "调用 create_session 启动远程编程工具"。LLM 看到指令但找不到工具，行为不可预测。同时 Sessions Tab 和 CodingSetupModel 是死 UI——没有后端支持。

**修复**（平台能力声明机制）：

#### 1. `SystemPromptConfig.HasCodingSessions` 字段（`corelib/agent/system_prompt.go`）

新增 `HasCodingSessions bool` 字段，声明宿主平台是否支持外部编程会话工具。这是数据层面的声明，不是 if-else workaround。

- GUI 设置 `HasCodingSessions: true` → system prompt 包含 `create_session` 工作流规则
- TUI 设置 `HasCodingSessions: false` → system prompt 包含直接编码规则（bash/write_file/edit_file）

#### 2. `appendCodingWorkflowRules` 分支（`corelib/agent/system_prompt.go`）

根据 `HasCodingSessions` 生成不同的编码工作流规则：
- `true`：原有的 `create_session` 工作流（GUI 行为不变）
- `false`：直接编码工作流——明确告诉 LLM "你没有 create_session 等远程编程会话工具"，列出可用的编码工具（read_file/write_file/edit_file/bash/list_directory）和编码规范

#### 3. 移除 TUI Sessions Tab 和编程工具 UI

- 删除 `TabCoding` 常量和 Sessions Tab
- 删除 `SessionListModel`、`SessionDetailModel`、`SessionCreateModel`、`CodingSetupModel` 四个视图文件
- 删除 `saveCodingToolConfig()`、`launchCodingTool()`、`refreshSessionData()` 函数
- 删除 `os/exec` import（不再需要启动子进程）
- `truncate()` 辅助函数从 `session_list.go` 提取到 `helpers.go`（被 5 个视图文件共用）

#### 4. TUI 的编码能力

TUI 通过 `CoreToolRegistry` 已注册的工具直接编码：
- `bash`：编译、lint、运行测试
- `write_file`：创建新文件（mode=append 分块写入大文件）
- `edit_file`：增量修改现有文件
- `read_file`：理解现有代码结构
- `list_directory`：浏览项目结构

这些工具在 `RegisterCoreTools` 中注册，TUI 和 GUI 共享同一套实现。

**删除的文件**：
- `tui/views/session_coding.go`（CodingSetupModel 编程工具启动向导）
- `tui/views/session_detail.go`（SessionDetailModel 会话详情）
- `tui/views/session_list.go`（SessionListModel 会话列表）
- `tui/views/session_create.go`（SessionCreateModel 会话创建表单）

**新增的文件**：
- `tui/views/helpers.go`（共享 truncate 辅助函数）

**验收标准**：
- TUI system prompt 不包含 `create_session`，包含直接编码规则
- GUI system prompt 行为不变（`HasCodingSessions: true`）
- TUI Tab 栏不显示 Sessions Tab
- LLM 在 TUI 中收到编码任务时，直接使用 bash/write_file/edit_file 编码
- 所有 55 个 TUI 测试通过
- 所有 corelib/agent 测试通过
- TUI 和 corelib 编译通过


### 62. 记忆架构改进 Phase 1: 高价值产出物实时沉淀

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #1（对话历史与长期记忆之间的断崖）。

**根因**：对话历史到长期记忆的流转是被动的（等会话过期）、延迟的（KnowledgeExtractor 1h 冷却）、粗粒度的（LLM 压缩成几句话）。编程工作流的需求文档、技术设计文档等高价值产出物在 `trimHistory` 截断时永久丢失——它们不是 turn boundary（#56 保留的是用户请求和 LLM 首条响应），也没有进入长期记忆。

**修复**：

#### 1. 新增 `CategoryTaskArtifact` 记忆类别

- `corelib/memory/types.go`：新增 `CategoryTaskArtifact Category = "task_artifact"`
- 属性：TierSemantic（结构化任务知识）、ScopeProject、ImportanceWeight=3.0
- 与 `session_checkpoint` 的区别：checkpoint 是编程会话进度快照（在 proactive recall 中被过滤），task_artifact 是工作流阶段产出物摘要（不被过滤，正常参与召回）

#### 2. `WorkflowEngine.SavePhaseOutput` 沉淀产出物摘要

- `corelib/workflow/types.go`：新增 `ArtifactSaver` 接口（`SaveArtifact(content, tags, sourceURL) error`），避免 workflow 包直接导入 memory 包
- `corelib/workflow/engine.go`：`WorkflowEngine` 新增 `artifactSaver` 字段 + `SetArtifactSaver()` 方法
- `SavePhaseOutput()` 末尾：当产出物 >200 rune 时，截断到 800 rune（段落边界优先），通过 `artifactSaver.SaveArtifact()` 沉淀到长期记忆

#### 3. GUI 侧适配器

- `gui/workflow_artifact_saver.go`：新文件
  - `workflowArtifactSaver`：实现 `ArtifactSaver` 接口，委托给 `memory.Store.Save()`
  - 去重机制 1：ContentHash 精确去重（相同内容不重复保存）
  - 去重机制 2：phaseTag 更新去重（同一阶段的产出物更新已有 entry 而非创建新 entry）
  - `deferredArtifactSaver`：延迟初始化适配器，处理 WorkflowEngine 和 memoryStore 的异步初始化顺序
- `gui/app_workflow_init.go`：`initWorkflowEngineWithStore` 中接线 `deferredArtifactSaver`

#### 4. Proactive Recall 放行

- `gui/im_system_prompt.go`：`appendProactiveRecall` 的过滤逻辑只排除 `session_checkpoint` 和 `conversation_summary`，`task_artifact` 不在过滤列表中，自然放行

**验收标准**：
- 编码工作流需求确认后，`memory.Store` 中出现 `task_artifact` 类别的条目
- 对话历史被截断到 40 条后，`RecallDynamic("贪吃蛇游戏需求")` 能召回需求文档摘要
- 同一阶段的产出物更新时，更新已有 entry 而非创建新 entry
- 7 个新增测试全部通过 + 所有现有 memory/workflow 测试通过
- GUI 编译通过


### 63. 记忆架构改进 Phase 2: 上下文感知的标签增强

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #2（写入-召回语义鸿沟）。

**根因**：记忆写入时 `ExpandQuery(content)` 只从 content 本身提取实体作为 tags。当用户在对话中使用别名（如称 api.rapidai.tech 为"4090服务器"），这个别名不在 content 中，不会被提取为 tag。后续用"4090服务器"查询时，BM25 和 embedding 都找不到匹配。

**修复**：

#### 1. `Store.SaveWithContext(entry, contextHint)` 新增方法

- `corelib/memory/store.go`：`Save()` 委托给 `SaveWithContext(entry, "")`
- `SaveWithContext` 在 injection scan 之后、hash 去重之前，从 `contextHint` 中提取实体并合并到 `entry.Tags`
- `contextHint` 为空时行为与原 `Save()` 完全一致（向后兼容）

#### 2. `tagExactMatchBoost` 新增函数

- `corelib/memory/store.go`：当 query 中的实体与 entry 的 tag 精确匹配（case-insensitive）时，给予 +5.0 分的强 boost（上限 10.0）
- 在 `RecallDynamic` 中，RRF 融合 + memoryStreamScore 之后、排序之前应用
- 与 `tagCrossScore`（RRF 内部的弱信号）互补：tagCrossScore 是 rank-based 的相对信号，tagExactMatchBoost 是 score-based 的绝对信号

#### 3. GUI `toolMemory` save action 注入对话上下文

- `gui/im_tools_misc.go`：`toolMemory` 的 save 分支调用 `SaveWithContext(entry, contextHint)`
- `buildMemoryContextHint()`：从最近 10 条对话历史中提取 user+assistant 文本（每条截断到 300 rune），拼接为 contextHint
- `corelib/agent/tool_memory.go`：save action 支持 `_context_hint` 参数（TUI 路径）

**验收标准**：
- 保存 SSH 信息时对话上下文中的别名被提取为 tag
- `RecallDynamic("4090server")` 通过 tag exact match boost 将 SSH 信息排到前 3
- 空 contextHint 时行为与原 `Save()` 完全一致
- 9 个新增测试 + 所有现有 memory 测试通过
- GUI/corelib 编译通过


### 64. 记忆架构改进 Phase 3: 写入时增量子串去重

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #4（Pipeline 6 小时批处理导致记忆质量滞后）。

**根因**：`Store.Save()` 只做精确去重（ContentHash），不做模糊去重（子串包含）。`KnowledgeExtractor` 提取的知识点是 LLM 生成的，措辞每次不同，hash 不同，但语义重复。模糊去重（`Compressor.dedup()` + `mergeSemanticDuplicates()`）要等 6 小时 Pipeline 才执行。期间 500 条容量中可能有 30-50 条语义重复的记忆，浪费 RecallDynamic 的 token budget。

**修复**：

#### 1. `SaveWithContext` 新增子串去重

- `corelib/memory/store.go`：在 hash 精确去重之后、创建新 entry 之前，新增 `findSubstringDuplicate(content)` 检查
- 匹配到子串重复时：合并 tags 到已有 entry，更新 UpdatedAt 和 AccessCount，不创建新 entry
- 只扫描最近 50 条 entries（按 slice 位置，与创建顺序相关），控制写入延迟 <5ms

#### 2. `findSubstringDuplicate` 新增方法

- `corelib/memory/store.go`：复用 `minSubstringLen=20` 常量（与 `Compressor.dedup` 一致）
- 双向子串检查：新内容包含已有内容 OR 已有内容包含新内容
- 返回匹配 entry 的 index，-1 表示无匹配
- 调用方必须持有 `s.mu.Lock`（在 SaveWithContext 的锁内调用）

**验收标准**：
- 保存"PostgreSQL 16 with pgvector"后再保存"PostgreSQL 16 with pgvector extension for vector search"→ 合并为 1 条
- 短内容（<20 字符）不触发子串去重
- 不同内容不被误合并
- 只扫描最近 50 条，不扫描全部 500 条
- 5 个新增测试 + 所有现有 memory 测试通过
- GUI 编译通过


### 65. 记忆架构改进 Phase 4: 产出物可召回索引——memory tool recall 全文展开

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #5（工作流产出物与记忆系统隔离）。

**修复**：

- `corelib/agent/tool_memory.go`：recall action 增强——当返回的 entry 是 `task_artifact` 且 `SourceURL` 非空时，读取文件全文（最多 5000 字符）返回给 LLM
- proactive recall（`appendProactiveRecall`）仍然只注入 200 字符摘要，不读取全文——控制 system prompt token 预算
- LLM 需要完整内容时，主动调用 `memory(action=recall)` 获取全文

**验收标准**：
- `memory(action=recall, query="需求文档")` 返回 task_artifact 的完整内容（或 800 rune 摘要）
- proactive recall 注入 200 字符截断摘要
- corelib/agent 编译通过 + 测试通过


### 66. 记忆架构改进 Phase 7: 对话历史智能压缩——LLM 摘要替代静态占位符 + 截断时记忆沉淀

**来源**：`docs/memory-architecture-improvement-plan.md`，Phase 7（对话历史智能压缩）+ Phase 1 补充（trimHistory 截断时沉淀）。

**根因**：`trimHistory` 截断对话历史时，被截断的 entries 用静态占位符 `[...中间的工具调用和执行细节已省略...]` 替代，LLM 完全丢失了这些 entries 的上下文。同时，非工作流场景下的实质性 assistant 消息（分析报告、研究总结等）在截断时永久丢失——Phase 1 的 `SavePhaseOutput` 只覆盖工作流场景。

**修复**：

#### 1. `trimHistoryWithSummary` 新增函数

- `gui/im_conversation_trim.go`：`trimHistory` 委托给 `trimHistoryWithSummary(entries, nil, nil)`（向后兼容）
- `trimHistoryWithSummary` 接受两个可选回调：
  - `summarizer func(string) string`：将被截断的 entries 文本压缩为 LLM 摘要
  - `memorySink func(string, []string)`：将实质性 assistant 消息（>500 rune）沉淀到长期记忆

#### 2. LLM 摘要替代静态占位符

- 被截断的 entries 文本（每条截断到 200 rune）拼接后传给 summarizer
- summarizer 返回非空摘要时，separator 从 `[...已省略...]` 变为 `[对话摘要] {summary}`
- summarizer 为 nil 或返回空字符串时，回退到静态占位符（向后兼容）

#### 3. 截断时记忆沉淀（Phase 1 补充）

- 被截断的非 tier-1 assistant 消息中，>500 rune 的实质性文档被沉淀为 `task_artifact`
- 覆盖非工作流场景（分析报告、研究总结等不经过 SavePhaseOutput 的产出物）

#### 4. `saveConversationHistoryTimed` 接线

- `gui/im_message_handler.go`：从 `trimHistory(history)` 改为 `trimHistoryWithSummary(history, summarizer, memorySink)`
- summarizer 使用已有的 `makeSummarizer(cfg, httpClient)`（15 秒超时）
- memorySink 使用 `h.memoryStore.Save(entry)` 保存 `task_artifact`
- LLM 未配置时 summarizer 为 nil，行为与原 `trimHistory` 完全一致

**验收标准**：
- 对话历史被截断时，separator 包含 LLM 生成的摘要（而非静态占位符）
- LLM 不可用时回退到静态占位符
- 实质性 assistant 消息在截断时被沉淀到长期记忆
- 所有现有 trimHistory 测试通过（`trimHistory` 委托给 `trimHistoryWithSummary(entries, nil, nil)`）
- GUI 编译通过
- 注：gui 测试构建有一个预先存在的 `wasSkillRecentlyRepaired` 未定义错误，与本次改动无关


### 67. 记忆架构改进 Phase 6: maclawsrv 多用户记忆隔离——Entry 新增 OwnerID 字段

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #6（maclawsrv 多用户记忆隔离不完整）。

**根因**：`memory.Store` 是全局单例，所有用户的记忆混在一个 `[]Entry` 中。`Entry` 没有 `UserID` 字段，`RecallDynamic` 没有按用户过滤的能力。IM 通道（飞书/微信）的用户 A 的项目知识可能被召回给用户 B。

**影响范围**：仅影响 maclawsrv（多租户）。GUI/TUI 是单用户，所有记忆天然属于同一个用户，OwnerID 始终为空，零开销。

**修复**：

#### 1. Entry 新增 OwnerID 字段

- `corelib/memory/types.go`：`Entry` 新增 `OwnerID string` (`json:"owner_id,omitempty"`)
- 空字符串表示"共享"——对所有用户可见（向后兼容旧数据）
- GUI/TUI：始终为空
- maclawsrv：设为 IM 用户 ID（如 `feishu_ou_xxx`）

#### 2. SaveForUser 新增方法

- `corelib/memory/store.go`：`SaveForUser(entry, ownerID)` 设置 OwnerID 后委托给 `Save()`

#### 3. RecallDynamic 按 OwnerID 过滤

- `corelib/memory/store.go`：`RecallDynamic` 签名改为 `RecallDynamic(query, category, projectPath string, ownerID ...string)`
- 可变参数保持向后兼容：GUI/TUI 的调用不需要改动
- 过滤逻辑：`filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner` → skip
- 空 OwnerID 的 entry 对所有用户可见（共享记忆）

#### 4. graphExpand 后二次过滤

- `graphExpand` 通过图谱边拉入邻居 entry 时不检查 OwnerID（图谱边是基于 tag 相似度建立的，跨用户）
- 在 graphExpand 之后新增 OwnerID 二次过滤，移除被图谱扩展拉入的其他用户 entry

#### 5. KnowledgeExtractor 注入 OwnerID

- `corelib/memory/knowledge_extractor.go`：`Extract(userID, msgs)` 将 `userID` 设为提取 entry 的 `OwnerID`

#### 6. ConversationArchiver 注入 OwnerID

- `gui/conversation_archiver.go`：`Archive(userID, entries)` 将 `userID` 设为摘要 entry 的 `OwnerID`

**验收标准**：
- maclawsrv：用户 A 保存的记忆不会被 RecallDynamic 返回给用户 B
- maclawsrv：OwnerID 为空的旧记忆对所有用户可见（向后兼容）
- GUI/TUI：行为完全不变，零改动
- OwnerID 正确持久化到 JSON 并在重启后恢复
- graphExpand 拉入的其他用户 entry 被二次过滤移除
- 6 个新增测试 + 所有现有 memory/agent/workflow 测试通过
- GUI 编译通过


### 68. 记忆架构改进 Phase 5: 按 Category 分区存储——从单文件到增量持久化

**来源**：`docs/memory-architecture-improvement-plan.md`，问题 #7（单文件 JSON 写放大）。

**根因**：`memory.Store` 将所有 500 条 entries 序列化为单个 `memories.json` 文件。每次 `signalSave()` 触发的 `flush()` 写入整个文件（250KB-1.5MB）。Pipeline 一次运行可能触发 10+ 次 Save，每次都全量写入。

**修复**：

#### 1. 分区管理器 (`corelib/memory/partition.go`, 新文件)

- `partitionManager`：管理 5 个分区组（identity/user/project/episodic/profile）
- 每个分区组对应一个独立的 JSON 文件（`part_identity.json` 等）
- `flushDirty(entries)`：只写入 dirty 的分区文件
- `loadPartitions()`：从分区文件加载所有 entries
- `migrateFromLegacy(entries, legacyPath)`：将旧格式单文件拆分为分区文件，重命名旧文件为 `.migrated`

#### 2. 分区组映射

| 分区 | 文件名 | 包含的 Category |
|------|--------|----------------|
| identity | part_identity.json | self_identity |
| user | part_user.json | user_fact, user, preference, instruction, feedback |
| project | part_project.json | project_knowledge, project, reference, task_artifact |
| episodic | part_episodic.json | conversation_summary, session_checkpoint |
| profile | part_profile.json | profile |

#### 3. 迁移策略——保守阈值

- **新安装**：使用旧格式单文件（所有现有测试不受影响）
- **旧用户 <100 条记忆**：继续使用单文件（小文件无写放大问题）
- **旧用户 ≥100 条记忆**：自动迁移到分区文件，旧文件重命名为 `.migrated`
- **已迁移用户**：直接从分区文件加载

#### 4. Store 集成

- `corelib/memory/store.go`：`Store` 新增 `partMgr *partitionManager` 字段
- `load()`：先尝试分区文件 → 回退到旧格式 → ≥100 条时自动迁移
- `flush()`：分区模式下 `markAllDirty` + `flushDirty`；旧模式下全量写入
- `NewStore()`：初始化 `partitionManager`

#### 5. 清理 stale rapid 测试缓存

- 删除 `corelib/memory/testdata/rapid/` 目录：Phase 3 的子串去重改变了 property test 的前提条件（随机字符串可能被去重），导致缓存的 fail file 无效

**验收标准**：
- 新安装使用旧格式单文件，所有现有测试通过
- ≥100 条记忆的旧用户自动迁移到分区文件
- 迁移后重启从分区文件加载，entries 数量正确
- RecallDynamic 跨分区正常工作
- 6 个新增测试 + 所有现有 memory 测试通过（含 property tests）
- GUI 编译通过


### 69. TUI/GUI HubCenter 接入 + Hub 匹配 + 注册全流程统一

**来源**：Review 发现 TUI 完全没有注册流程（P0），GUI 的注册逻辑内联在 `remote_activation.go` 中无法复用，以及多个 P1 问题。

#### 问题清单

| # | 优先级 | 问题 | 修复 |
|---|--------|------|------|
| 1 | P0 | TUI 完全没有注册流程，依赖环境变量或 GUI 预注册 | 新增 `remote activate` 命令 |
| 2 | P1 | 客户端选 hub 时不检查 hub status | `pickBestHub` 新增 status 检查 |
| 3 | P1 | `autoRegisterOnStartup` 的 config 竞态（与 #11 同模式） | 持久化前 reload config，只 merge 注册字段 |
| 4 | P1 | `DiscoverHubCenterURLs` 双重 probe 导致首次连接延迟 10s+ | 无新 URL 时跳过第二次 probe |
| 5 | P1 | `generateClientID` 在 GUI 和 corelib 重复定义 | GUI 委托 `remote.GenerateClientID()` |

#### 修复

##### 1. 注册逻辑提取到 `corelib/remote/enrollment.go`（单一实现）

- `EnrollmentClient` 结构体：封装 HubCenter discovery → Hub resolve → Enroll 全流程
- `Enroll(ctx, EnrollConfig) (*EnrollResult, error)`：完整注册流程，不做持久化（由调用方负责）
- `BuildMachineProfile(appVersion)` → `EnrollConfig`：填充机器级默认值（hostname/platform/arch）
- `NewHubHTTPClient()` → `*http.Client`：统一的 TLS-skip HTTP 客户端（hub 常用自签证书）
- `NormalizedPlatform()`：统一的平台字符串（windows/mac/linux）
- `pickBestHub()`：从 resolve 结果中选最佳 hub，新增 `isHubOnline()` status 检查
- `buildCenterURLList()`：组装去重的 HubCenter URL 列表

##### 2. GUI `ActivateRemote` 委托给 `EnrollmentClient`

- 核心注册逻辑从 ~80 行内联 HTTP 代码改为 3 行调用 `enrollClient.Enroll()`
- 保留 GUI 特有的后处理：`emitRemoteStateChanged()`、`ensureRemoteInfra()`、`ensureHubClient().Connect()`
- `generateClientID()` 改为委托 `remote.GenerateClientID()`，删除 `crypto/rand` import
- **Config 竞态修复**：持久化前 `LoadConfig()` 获取最新 config，只 merge 注册相关字段（RemoteEmail/MachineID/MachineToken/HubURL 等），不覆盖用户在 UI 中的并发修改

##### 3. TUI 新增 `remote activate` 命令

- `tui/commands/remote.go`：新增 `remoteActivate()` 函数
- 支持 `--email` 和 `--invitation-code` 参数
- 调用 `remote.NewEnrollmentClient().Enroll()` 完成注册
- 持久化到 `config.json`（与 GUI 共享）
- 已激活时提示用户先 deactivate

##### 4. TUI 启动时不完整配置提示

- `tui/app.go`：检测 `RemoteEmail + RemoteHubURL` 非空但 `RemoteMachineID` 或 `RemoteMachineToken` 为空时，提示运行 `remote activate`

##### 5. `DiscoverHubCenterURLs` 优化

- `corelib/remote/hubcenter_discovery.go`：新增 `hasNewURLs()` 函数
- 当 discovery 没有发现新的 HubCenter URL 时，跳过第二次 `SelectBestCenter` probe
- 避免在种子 URL 已经覆盖所有节点时的冗余 3-10s 延迟

##### 6. `pickBestHub` hub status 检查

- 三层 fallback：online default hub → online any hub → any hub（last resort）
- `isHubOnline(status)`：空 status 视为 online（向后兼容旧 HubCenter）
- 防御 HubCenter snapshot 和实际状态之间的时间差

**修改文件**：

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/remote/enrollment.go` | 新增 | `EnrollmentClient` + `Enroll()` 全流程 |
| `corelib/remote/enrollment_test.go` | 新增 | 14 个测试 |
| `corelib/remote/hubcenter_discovery.go` | 修改 | `hasNewURLs()` 优化 |
| `gui/remote_activation.go` | 修改 | 委托 `EnrollmentClient` + config 竞态修复 |
| `gui/remote_activation_test.go` | 修改 | 覆盖 `DefaultRemoteHubCenterURLs` 防止测试 probe 真实节点 |
| `tui/commands/remote.go` | 修改 | 新增 `activate` 子命令 |
| `tui/app.go` | 修改 | 启动时不完整配置提示 |

**不变量**：
- `EnrollmentClient.Enroll()` 是注册逻辑的单一实现——GUI 和 TUI 共享
- `config.json` 是持久化的单一数据源——两端读写同一个文件
- `GenerateClientID()` 在 `corelib/remote/` 中只有一个定义
- `NewHubHTTPClient()` 是 hub/hubcenter HTTP 客户端的单一工厂

**TUI 用户的完整注册流程**：
```
maclaw-tui remote set-email user@example.com   # 可选，也可用 --email 参数
maclaw-tui remote activate                      # 一条命令完成 discovery → resolve → enroll
maclaw-tui remote status                        # 验证注册状态
```

**验收标准**：
- `maclaw-tui remote activate --email user@example.com` → 完成注册，显示 machine_id
- GUI `ActivateRemote` 行为不变，所有 4 个现有测试通过
- `pickBestHub` 跳过 `pending_confirmation` 状态的 hub
- `DiscoverHubCenterURLs` 无新 URL 时不做第二次 probe
- config 竞态：`autoRegisterOnStartup` 不覆盖用户在 UI 中的并发修改
- 14 个新增 corelib/remote 测试 + 所有现有 GUI 测试通过
- corelib/remote、GUI、TUI 编译通过


### 70. HubCenter 接入机制层面 Review + 多路径 Failover 统一

**来源**：用户要求从机制层面 review HubCenter 接入逻辑，找出根本性问题而非 workaround。

**问题清单**：

| # | 优先级 | 问题 | 修复 |
|---|--------|------|------|
| 1 | P1 | `rememberHubCenterSelectionThrottled` 只比较 base URL，不比较 discovered 列表 | 改为同时比较 base 和 discovered 列表（`StringSliceEqual`） |
| 2 | P1 | `hubUpdateCache.set` 用 O(N×M) 嵌套循环反查 name→hubID 映射 | 直接从 `HubSkillUpdateInfo.HubSkillID` 读取，删除 `hubSkillIDMapping` 类型 |
| 3 | P1 | `DefaultHubClient` 在单例初始化时固定 `githubToken` | `SearchGitHub` 改为每次调用时动态读取 `ResolveGitHubToken()` |
| 4 | P1 | TUI 的 failover 逻辑不完整——缺少 `DiscoverHubCenterURLs` | TUI 的 `searchSkills` 和 `skillSearch` 添加 `DiscoverHubCenterURLs` → `SelectBestCenter` 两层 fallback |
| 5 | P0 | TUI 的 failover 结果没有被持久化 | 创建共享的 `HubCenterSelectionCache` + `HubCenterPersister` 接口 |
| 6 | P1 | CLI 命令的 HubCenter URL 解析没有 failover | `resolveHubURL()` (skillhub.go) 和 `resolveHubCenterURL()` (skillmarket.go) 都添加了 failover + 持久化 |
| 7 | P2 | GUI 和 corelib 有重复的 HubCenter 缓存实现 | 已识别，低优先级，未修复 |
| 8 | P1 | `tui/commands/skill_search_api.go` 中的导出 API 没有 failover | 添加完整的 failover 逻辑 + 持久化 |

#### 问题 1: `rememberHubCenterSelectionThrottled` 只比较 base URL

**根因**：`HubCenterSelectionCache.RememberSelectionThrottled()` 只比较 `base == cachedBase`，不比较 `discovered` 列表。当 HubCenter 节点列表变化但首选节点不变时，新的 discovered 列表不会被持久化。

**修复**：
- `gui/hub_update_cache.go`：`RememberSelectionThrottled` 改为同时比较 base 和 discovered 列表
- `corelib/remote/hubcenter_persist.go`：新增 `StringSliceEqual()` 函数，共享的列表比较逻辑

#### 问题 2: `hubUpdateCache.set` 用 O(N×M) 嵌套循环

**根因**：`hubUpdateCache.set()` 需要从 skill name 反查 hubSkillID，使用 O(N×M) 嵌套循环遍历所有 skills 和所有 updates。

**修复**：
- `gui/hub_update_cache.go`：直接从 `HubSkillUpdateInfo.HubSkillID` 读取（该字段已存在），删除 `hubSkillIDMapping` 类型
- `gui/app_wails_bindings.go`：同步删除 `hubSkillIDMapping` 的使用

#### 问题 3: `DefaultHubClient` 在单例初始化时固定 `githubToken`

**根因**：`DefaultHubClient()` 是单例，在首次调用时读取 `GITHUB_TOKEN` 环境变量。如果用户在运行时设置了 token，单例不会更新。

**修复**：
- `corelib/skill/hub_search.go`：`SearchGitHub` 改为每次调用时动态读取 `ResolveGitHubToken()`，而非使用构造时固定的 token

#### 问题 4: TUI 的 failover 逻辑不完整

**根因**：TUI 的 `searchSkills` 和 `skillSearch` 只使用配置的 HubCenter URL，没有 `DiscoverHubCenterURLs` → `SelectBestCenter` 两层 fallback。

**修复**：
- `tui/app.go`：`searchSkills()` 添加 `DiscoverHubCenterURLs` → `SelectBestCenter` 两层 fallback
- `tui/tool_manage_skill.go`：`skillSearch()` 同步添加 failover 逻辑

#### 问题 5: TUI 的 failover 结果没有被持久化

**根因**：TUI 的 failover 选择了新的 HubCenter URL，但没有持久化到 config.json。下次启动时又从头开始 failover。

**修复**：
- `corelib/remote/hubcenter_persist.go`：新文件
  - `HubCenterPersister` 接口：`LoadHubCenterURLs()` / `SaveHubCenterURLs()`
  - `HubCenterSelectionCache`：共享的写节流缓存，防止重复写盘
  - `RememberSelectionThrottled()`：比较 base + discovered 列表，只在变化时持久化
  - `StringSliceEqual()`：列表比较辅助函数
- `tui/app.go`：实现 `tuiHubCenterPersister`，在 failover 后调用 `RememberSelectionThrottled()`
- `tui/tool_manage_skill.go`：同步添加持久化逻辑

#### 问题 6: CLI 命令的 HubCenter URL 解析没有 failover

**根因**：`tui/commands/skillhub.go` 的 `resolveHubURL()` 和 `tui/commands/skillmarket.go` 的 `resolveHubCenterURL()` 只读取配置，没有 failover。

**修复**：
- `tui/commands/skillhub.go`：`resolveHubURL()` 添加 `DiscoverHubCenterURLs` → `SelectBestCenter` failover + 持久化
- `tui/commands/skillmarket.go`：`resolveHubCenterURL()` 同步添加 failover + 持久化

#### 问题 8: `tui/commands/skill_search_api.go` 中的导出 API 没有 failover

**根因**：`SearchSkillHub()` 和 `SearchSkillMarket()` 是导出函数，被 agent tool 调用。它们直接使用配置的 URL，没有 failover 逻辑。

**修复**：
- `tui/commands/skill_search_api.go`：
  - `SearchSkillMarket()` 添加 `DiscoverHubCenterURLs` → `SelectBestCenter` 两层 fallback
  - `SearchSkillHub()` 同步添加 failover 逻辑
  - 新增 `commandsHubCenterPersister` 类型实现 `HubCenterPersister` 接口
  - failover 结果通过 `HubCenterSelectionCache.RememberSelectionThrottled()` 持久化

**修改文件**：

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/remote/hubcenter_persist.go` | 新增 | 共享的 `HubCenterPersister` 接口 + `HubCenterSelectionCache` |
| `corelib/skill/hub_search.go` | 修改 | `SearchGitHub` 动态读取 token |
| `gui/hub_update_cache.go` | 修改 | 比较 discovered 列表 + 删除 O(N×M) 循环 |
| `gui/app_wails_bindings.go` | 修改 | 删除 `hubSkillIDMapping` |
| `tui/app.go` | 修改 | failover + 持久化 |
| `tui/tool_manage_skill.go` | 修改 | failover + 持久化 |
| `tui/commands/skillhub.go` | 修改 | failover + 持久化 |
| `tui/commands/skillmarket.go` | 修改 | failover + 持久化 |
| `tui/commands/skill_search_api.go` | 修改 | failover + 持久化 |

**不变量**：
- `HubCenterPersister` 是持久化逻辑的单一接口——GUI、TUI UI、TUI agent tool、TUI CLI 共享
- `HubCenterSelectionCache.RememberSelectionThrottled()` 是写节流的单一实现
- `DiscoverHubCenterURLs` → `SelectBestCenter` 是 failover 的标准两层模式
- 所有 failover 结果都持久化到 `config.json`

**验收标准**：
- 所有 `corelib/remote/` 测试通过（36 个测试）
- `tui/commands/` 编译通过
- `tui/` 整体编译通过
- `gui/` 编译通过
- HubCenter 节点列表变化时，新列表被持久化
- TUI 和 CLI 的 failover 结果被持久化，下次启动时直接使用

#### Review/Fix/Optimize 阶段（机制性重构）

初步修复后进行代码 review，发现 4 个机制性问题：

| # | 问题 | 根因 | 修复 |
|---|------|------|------|
| 1 | `HubCenterSelectionCache` 每次调用都创建新实例 | 缓存无效，每次 failover 都重新 probe | 改为包级别单例模式 |
| 2 | failover 逻辑在 4 个地方重复 | `tui/app.go`、`tool_manage_skill.go`、`skillhub.go`、`skillmarket.go` 各自实现 ~15-25 行相同代码 | 统一到 `ResolveHubCenterWithFailover()` |
| 3 | `tuiHubCenterPersister` 和 `commandsHubCenterPersister` 重复 | 两个包各自定义相同的 persister 实现 | 删除 TUI 侧重复定义，共享 commands 包的实现 |
| 4 | `skillhub.go` 的 failover 函数与 `skill_search_api.go` 重复 | 两个文件各自实现 failover 逻辑 | 委托给共享的 `ResolveHubCenterWithFailover()` |

##### 共享基础设施（`tui/commands/skill_search_api.go`）

```go
// 包级别单例
var (
    sharedHubCenterCache     *remote.HubCenterSelectionCache
    sharedHubCenterCacheOnce sync.Once
    sharedHubCenterPersister remote.HubCenterPersister
    sharedHubCenterPersistOnce sync.Once
)

// SharedHubCenterCache 返回共享的 HubCenter 选择缓存单例
func SharedHubCenterCache() *remote.HubCenterSelectionCache

// SharedHubCenterPersister 返回共享的 HubCenter 持久化器单例
func SharedHubCenterPersister() remote.HubCenterPersister

// HubCenterPersister 导出类型供 TUI app 使用
type HubCenterPersister = remote.HubCenterPersister

// ResolveHubCenterWithFailover 是 failover 逻辑的单一实现
// 所有调用方（TUI UI、TUI agent tool、TUI CLI）共享此函数
func ResolveHubCenterWithFailover(ctx context.Context, configuredURL string, 
    cache *remote.HubCenterSelectionCache, persister remote.HubCenterPersister) string
```

##### 调用方改造

| 文件 | 改造前 | 改造后 |
|------|--------|--------|
| `tui/app.go` | ~25 行内联 failover 代码 + `tuiHubCenterPersister` 类型定义 | 1 行调用 `commands.ResolveHubCenterWithFailover()` |
| `tui/tool_manage_skill.go` | ~15 行内联 failover 代码 | 1 行调用 `commands.ResolveHubCenterWithFailover()` |
| `tui/commands/skillhub.go` | ~20 行内联 failover 代码 | 1 行调用 `ResolveHubCenterWithFailover()` |
| `tui/commands/skillmarket.go` | ~15 行内联 failover 代码 | 1 行调用 `ResolveHubCenterWithFailover()` |

##### 删除的重复代码

- `tui/app.go`：删除 `tuiHubCenterPersister` 类型定义和相关方法（~30 行）
- `tui/app.go`：删除 `TUIApp` 结构体中的 `hubCenterCache` 和 `hubCenterPersist` 字段
- 4 个文件中的内联 failover 代码（共 ~75 行）

##### 机制性设计原则

1. **单例模式**：`HubCenterSelectionCache` 和 `HubCenterPersister` 使用包级别单例（`sync.Once`），确保跨调用的缓存有效
2. **单一数据源**：`ResolveHubCenterWithFailover()` 是 failover 逻辑的唯一实现，所有调用方共享
3. **导出类型**：`HubCenterPersister` 导出供 TUI app 使用，避免重复定义
4. **向后兼容**：函数签名保持兼容，cache 和 persister 参数可选（nil 时使用单例）

##### 不变量（Review/Fix/Optimize 后）

- `ResolveHubCenterWithFailover()` 是 TUI 侧 failover 逻辑的**单一实现**——新增 failover 调用点只需调用此函数
- `SharedHubCenterCache()` 和 `SharedHubCenterPersister()` 是单例的**单一入口**——不允许在其他地方创建实例
- 所有 TUI 侧的 HubCenter URL 解析都经过 failover 逻辑——无遗漏路径


### 71. OwnerID 多租户隔离补全——6 个遗漏路径修复

**来源**：记忆管理机制全面调查（`docs/memory-mechanism-investigation-report.md`），发现 Phase 6（#67）引入 OwnerID 字段后，5 个消费路径未适配 OwnerID 隔离。

**影响范围**：仅影响 maclawsrv（多租户）。GUI/TUI 单用户场景 OwnerID 始终为空，零开销。

#### 问题 1: SaveWithContext hash 精确去重不检查 OwnerID

**根因**：`SaveWithContext()` 的 hash 精确去重循环（`ContentHash == hash || Content == entry.Content`）不检查 OwnerID。当两个不同用户保存相同内容时，第二个用户的 entry 被合并到第一个用户的 entry 中，第二个用户的 OwnerID 丢失。

**修复**：
- `corelib/memory/store.go`：hash 精确去重循环新增 OwnerID 检查——`entry.OwnerID != "" && existingOwner != "" && existingOwner != entry.OwnerID` 时 `continue`，与 `findSubstringDuplicate` 的隔离逻辑一致

#### 问题 2: Archiver 缺少 OwnerID

**根因**：`Archive()` 创建的 `conversation_summary` entry 没有设置 OwnerID。

**修复**：
- `corelib/memory/archiver.go`：`Archive()` 创建 entry 时设置 `OwnerID: userID`

#### 问题 3: Compressor.dedup 不检查 OwnerID

**根因**：`isDuplicateLower()` 只检查 Category，不检查 OwnerID。不同用户的相同内容可能被跨用户删除。

**修复**：
- `corelib/memory/compressor.go`：`isDuplicateLower()` 新增 OwnerID 检查——不同 OwnerID 的 entry 不视为重复

#### 问题 4: mergeSemanticDuplicates 不按 OwnerID 分组

**根因**：`mergeSemanticDuplicates()` 按 Category 分组后做语义合并，不区分 OwnerID。不同用户的语义相似记忆可能被跨用户合并。

**修复**：
- `corelib/memory/compressor.go`：分组 key 从 `Category` 改为 `(Category, OwnerID)` 二元组

#### 问题 5: Consolidator 缺少 OwnerID

**根因**：`ConsolidateSegment()` 和 `ConsolidateLevel()` 创建的 entry 没有设置 OwnerID。

**修复**：
- `corelib/memory/consolidator.go`：所有方法新增 `ownerID string` 参数，创建 entry 时设置 `OwnerID`
- `corelib/memory/knowledge_extractor.go`：`ConsolidateSegment` 调用传递 `userID`
- `corelib/memory/pipeline.go`：按用户分组执行 consolidation，新增 `UniqueOwnerIDs()` 方法到 Store

#### 问题 6: ArchiveStore.FindRelevant 缺少 OwnerID 过滤

**根因**：GC revive 通过 `FindRelevant()` 从 archive 恢复记忆时不检查 OwnerID，可能跨用户恢复。

**修复**：
- `corelib/memory/archive.go`：`FindRelevant()` 新增 `ownerID string` 参数，过滤逻辑与 `RecallDynamic` 一致
- `corelib/memory/compressor.go`：`RunGC()` 签名改为可变参数 `ownerID ...string`，传递给 `FindRelevant`

#### 问题 7: findSubstringDuplicate 缺少 OwnerID

**修复**：
- `corelib/memory/store.go`：`findSubstringDuplicate()` 新增 `ownerID string` 参数，跳过不同用户的 entry

**修改文件**：
- `corelib/memory/store.go`：hash 去重 OwnerID 检查 + `findSubstringDuplicate` 签名 + `UniqueOwnerIDs()` 方法
- `corelib/memory/archiver.go`：OwnerID 注入
- `corelib/memory/compressor.go`：`isDuplicateLower` + `mergeSemanticDuplicates` + `RunGC` OwnerID 适配
- `corelib/memory/consolidator.go`：所有方法新增 ownerID 参数
- `corelib/memory/archive.go`：`FindRelevant` 新增 ownerID 参数
- `corelib/memory/pipeline.go`：按用户分组 consolidation
- `corelib/memory/knowledge_extractor.go`：传递 userID
- `corelib/memory/owner_isolation_test.go`：新增 12 个测试
- `corelib/memory/save_dedup_test.go`：更新 `findSubstringDuplicate` 调用签名

**不变量**：
- 空 OwnerID（共享记忆）对所有用户可见——所有 6 个修复点的过滤逻辑一致
- GUI/TUI 单用户场景 OwnerID 始终为空，所有路径行为不变
- 可变参数 `ownerID ...string` 保持向后兼容

**验收标准**：
- 不同用户保存相同内容 → 各自保留独立 entry（不跨用户去重）
- 同一用户保存相同内容 → 正常去重
- Pipeline consolidation 按用户分组执行
- GC revive 不跨用户恢复记忆
- 所有 memory 包测试通过（含 12 个新增 OwnerID 测试 + 所有现有测试）


### 72. AI 助手面板路径链接失效——反引号/加粗包裹的路径被当作代码渲染

**根因**：`renderInlineMarkdown` 的正则中，内联代码（`` `...` ``，Group 1）和加粗（`**...**`，Group 2）的匹配优先级高于路径匹配（Group 5/6）。LLM 输出路径时经常用反引号包裹（Markdown 标准做法），导致路径被 Group 1 捕获，渲染为红色代码文本（`codeText: #f87171`）而非绿色可点击链接（`pathColor: #4ade80`）。

**触发路径**：
1. LLM 输出 `` `D:\workprj\aicoder\docs\iworker\pptx_output\` ``
2. 正则 Group 1（`` `[^`]+` ``）优先匹配整个反引号包裹的内容
3. 渲染为 `<code>` 元素（红色文本，不可点击）
4. Group 5/6（路径匹配）永远没有机会匹配

同样的问题影响加粗包裹的路径（`**D:\path\to\dir**`）和 Markdown 链接中的本地路径（`[打开文件](D:\path\to\file.pdf)`）。

**修复**：

#### 1. 内联代码和加粗分支新增路径检测（`AIAssistantPanel.tsx`）

- 新增 `looksLikeFilePath(s)` 函数：检测 Windows 盘符路径（`C:\...`）和 Unix 绝对/home 路径（`/home/...`、`~/...`）
- 内联代码分支（Group 1）：提取 inner 内容后先检测是否为路径，是则渲染为可点击链接
- 加粗分支（Group 2）：同上
- 普通代码内容（`npm install`、`console.log()`、`**kwargs` 等）不受影响

#### 2. `renderPathLink` 提取为共享函数

- 消除了三处重复的路径链接渲染代码
- 新增 `trimTrailing` 参数：裸路径（bare path）需要清理正则可能过度捕获的尾部标点；反引号/加粗路径已由 `slice` 剥离分隔符，内容干净，不需要清理

#### 3. Markdown 链接中的本地路径可点击

- 原逻辑：`[text](href)` 只处理 `https?://` URL，非 HTTP 链接渲染为不可点击的 `<span>`
- 新增 `looksLikeFilePath(href)` 检查：本地路径渲染为 📂 可点击链接，使用用户提供的 label 文本

**未修改**：
- 斜体分支（`*C:\path*`）：LLM 几乎不会斜体化路径，实际场景极少，不值得增加代码复杂度

**验收标准**：
- `` `D:\workprj\aicoder\docs\iworker\pptx_output\` `` → 绿色可点击链接（📂 图标 + 下划线），点击打开目录
- `**D:\workprj\file.txt**` → 同上
- `[打开文件](D:\workprj\file.pdf)` → 可点击链接，显示"打开文件"文本
- `` `npm install` `` → 红色代码文本（不受影响）
- `**重要提示**` → 加粗文本（不受影响）
- 裸路径 `D:\workprj\output\` → 行为不变（可点击链接）
- 所有 45 个现有 AIAssistantPanel 测试通过（1 个预先存在的 theme 测试失败不受影响）


### 73. SkipNeedsConfirmGate 无条件旁路 Coding Tool Gate——新编码任务在活跃工作流期间跳过三阶段

**根因**：`SkipNeedsConfirmGate` 是 #41/#42 引入的旁路机制，设计意图是"活跃工作流期间的无关消息不被工作流门控拦截"。但它是一个粗粒度的开关——不区分"无关的操作请求"（查天气→不需要三阶段）和"无关的编码请求"（开发游戏→需要三阶段）。

当用户在活跃工作流（如 PPT 设计的 `audience_goal` 阶段）期间发送"开发一个c++的超级玛利游戏"时：
1. `handlePendingConfirm` 的轻量 LLM 正确将其分类为 `"other"`（与 PPT 工作流无关）
2. `workflowPendingConfirmOther.Store(userID, true)` → `SkipNeedsConfirmGate=true`
3. `GateIntentClassifier` 正确识别为 `new_project`（conf=0.91）→ `gateConfig.active=true`
4. 但 `SkipNeedsConfirmGate=true` 在 **5 个消费点** 无条件旁路了 Coding Tool Gate：
   - 工具定义过滤（2 处）：`gateConfig.active && !ctx.SkipNeedsConfirmGate` → 编码工具定义未被移除
   - 工具调用拦截（1 处）：`gateConfig.active && !ctx.SkipNeedsConfirmGate` → 编码工具调用未被拦截
   - NeedsConfirm no-tool 分支（1 处）：`ctx.SkipNeedsConfirmGate` → `semanticBypass=true`
   - NeedsConfirm tool 分支（1 处）：`ctx.SkipNeedsConfirmGate` → `needsConfirmToolBranch=false`
5. LLM 看到完整的编码工具列表，直接调用 write_file 开始写代码，完全跳过三阶段流程

**核心矛盾**：`SkipNeedsConfirmGate`（来自 handlePendingConfirm 的"与当前工作流无关"判定）和 `gateConfig.active`（来自 GateIntentClassifier 的"当前消息是编码任务"判定）是两个独立的信号。当两者同时为 true 时（"与当前工作流无关的编码任务"），`SkipNeedsConfirmGate` 不应该覆盖 `gateConfig.active`。

**修复**（机制性修复——`SkipNeedsConfirmGate` 从"无条件旁路所有门控"变为"只旁路工作流引擎的门控，不旁路编码意图的门控"）：

- `gui/im_message_handler.go`：5 个消费点修改：
  1. **工具定义过滤**（2 处）：从 `gateConfig.active && !ctx.SkipNeedsConfirmGate` 改为 `gateConfig.active`。当 `gateConfig.active=true` 时，无论 `SkipNeedsConfirmGate` 如何，编码工具定义始终被过滤。非编码任务 `gateConfig.active=false`，gate 自然不触发。
  2. **工具调用拦截**（1 处）：从 `gateConfig.active && !ctx.SkipNeedsConfirmGate` 改为 `gateConfig.active`。同上。
  3. **NeedsConfirm no-tool 分支**：从 `if ctx.SkipNeedsConfirmGate` 改为 `if ctx.SkipNeedsConfirmGate && !gateConfig.active`。只有非编码意图才旁路。
  4. **NeedsConfirm tool 分支**：从 `ctx.SkipNeedsConfirmGate` 改为 `ctx.SkipNeedsConfirmGate && !gateConfig.active`。同上。
  5. **`applyWorkflowToolFilter`**（doc_only 过滤）：**不变**——doc_only 是工作流引擎的策略，与编码意图无关，`SkipNeedsConfirmGate` 正确旁路它。

**设计原则**：`gateConfig.active` 和 `SkipNeedsConfirmGate` 是正交的两个信号：
- `gateConfig.active`：当前消息是否是编码任务（由 GateIntentClassifier 判定）
- `SkipNeedsConfirmGate`：当前消息是否与活跃工作流无关（由 handlePendingConfirm 判定）
- 两者同时为 true = "与当前工作流无关的新编码任务" → 编码门控生效，工作流门控旁路
- `gateConfig.active=false` + `SkipNeedsConfirmGate=true` = "与当前工作流无关的非编码任务" → 两个门控都旁路

**验收标准**：
- 活跃 PPT 工作流期间发送"开发超级玛利游戏" → Coding Tool Gate 激活，LLM 生成需求文档，不直接编码
- 活跃 PPT 工作流期间发送"查天气" → 两个门控都旁路，正常查天气
- 活跃 PPT 工作流期间发送"更新服务器上的omniroute" → 编码门控不激活（非编码意图），工作流门控旁路，ssh 工具可用
- 无活跃工作流时 → 行为完全不变
- GUI 编译通过（仅 pre-existing `im_tools_misc.go:344` 错误）


### 74. Agent Loop 停止的两个独立问题——Context 膨胀 + 纯文本截断未续写

**来源**：日志 `~/.maclaw/logs/maclaw.log`，trajectory `2026-04-25_21-36-14.715_chat.json`（409 entries）。

#### 问题 1 (P0): estimateConversationTokens 低估导致 trimConversation 不触发——Context 膨胀到 134K 后空响应 hard exit

**根因**：`estimateConversationTokens` 使用 2.5 bytes/token 的固定比率估算 token 数。对中文+代码混合内容，实际 tokenizer 的比率更低（中文字符通常 1-2 个 token 对应 3 个 UTF-8 字节），导致估算值比 API 报告的实际 token 数低 ~30%。

**日志证据**：
- Agent loop 从 20:51 开始，跑了 129 次迭代，45 分钟
- `input=134460` → `input=135098` — context 持续在 134-135K token（远超 `EffectiveContextTokens = 128K * 80% = 102.4K`）
- `trimConversation` 每次迭代都被调用，但 `estimateConversationTokens` 估算出 ~80-90K（在预算内），不触发裁剪
- 最后 3 次 LLM 调用：`output=0 usage_nil=true` — glm-5.1 在 134K+ context 下返回空响应
- 触发 `[agent-loop] hard exit: 3 consecutive empty responses, 6 total recovers`

**修复**：
- `gui/im_message_handler.go`：agent loop 中 `trimConversation` 调用前新增 **实际 token 校准**（actual-token calibration）
  - 使用上一次 LLM 调用返回的 `lastLLMInputTokens`（API 报告的实际 input token 数）
  - 与 `estimateConversationTokens` 的估算值计算比率
  - 比率 > 1.15（低估超过 15%）时，按比率缩减传给 `trimConversation` 的 token limit
  - 例：API 报告 134K，估算 90K，ratio=1.49 → limit 从 102K 缩减到 102K/1.49=68K → `trimConversation` 触发裁剪
  - `effectiveTokenLimit` 变量提升到 loop 外部，bonus round 也使用校准后的值
  - 最低 4000 token 保底，防止极端情况

**设计要点**：
- 不修改 `estimateBytesToTokens` 的比率——不同模型的 tokenizer 不同，固定比率无法通用
- 用 API 返回的实际数据做运行时校准——对所有模型、所有语言自动适配
- 校准只在 ratio > 1.15 时触发——小误差不需要校准，避免不必要的裁剪
- 第一次迭代（`lastLLMInputTokens=0`）不校准——没有参考数据

#### 问题 2 (P1): finish_reason=length 的纯文本响应未续写

**根因**：`filterTruncatedToolCalls()` 只处理 tool call 参数的截断，不处理纯文本响应的截断。当 LLM 的纯文本输出被 output token 限制截断时，agent loop 将截断的文本当作完整响应返回。

**修复**：
- `gui/im_message_handler.go`：
  - `agentLoopPhase` 新增 `LengthContinuations int` 字段
  - no-tool 分支新增 `finish_reason=length` 检测（在 NeedsConfirm gate 之前）
  - 检测到截断时注入续写 prompt，最多续写 3 次

**验收标准**：
- Context 膨胀到 134K 时 → `trimConversation` 被校准后的 limit 触发裁剪，conversation 被压缩
- 模型不再因 context 过大返回空响应
- 纯文本输出被截断时 → 自动续写
- 所有现有测试通过
- GUI / TUI / corelib 编译通过


### 75. 编码 SubAgent——纯净上下文编程执行器

**来源**：`docs/coding-subagent-architecture-design.md`，日志分析确认主 Agent 编码时 context 膨胀到 134K+ token 导致模型返回空响应。

**根因**：主 Agent 是全能型单体——同一个 context 里塞了角色人设（12K）、40+ 工具定义（15K）、记忆/steering（5K）、IM 规则、安全防火墙等，初始开销 40K token（30%）。编码任务 129 轮迭代后 context 膨胀到 134K，模型无法工作。`trimConversation` 无法区分"编码相关的重要历史"和"IM 规则注入的噪音"——它只能按时间顺序裁剪。

**修复**：实现编码 SubAgent，在纯净上下文中执行编码任务。

#### Phase 1: CodingSubAgent 核心（`gui/coding_subagent.go`）

- `CodingSubAgent` 结构体：独立的编码执行器，复用 `corelib/agent.RunLoop`
- `codingSubAgentCallbacks`：实现 `agent.LoopCallbacks`，提供精简配置
- 精简 system prompt（~2K token）：只含编码规范、项目路径、需求/设计摘要、前置任务产出
- 精简工具集（5 个，~744 token）：read_file、write_file、edit_file、bash、list_directory
- 工具执行委托给主 Agent 的现有实现（`toolReadFile` 等），零重复代码
- 独立 conversation history，不被 IM 规则、记忆、steering 等污染
- 单任务 50 轮迭代上限
- 支持 cancel、streaming token 回调、进度回调
- `RunTaskWithSubAgent()` 便捷入口

#### Phase 2: Orchestrator 桥接（`gui/coding_subagent_orchestrator.go`）

- `SubAgentTaskRunner`：连接 `TaskExecutionOrchestrator` 和 `CodingSubAgent`
- `RunCurrentTask()`：执行单个任务，自动处理成功/失败/重试（最多 MaxRetries 次）
- `RunAllTasks()`：顺序执行所有任务，生成最终报告
- `ShouldUseSubAgent()`：决策函数——orchestrator active + direct mode → SubAgent
- `collectPreviousOutputs()`：收集已完成任务的文件列表注入后续任务 context

#### Phase 3: 主 Agent Loop 接入（`gui/im_message_handler.go`）

- 在 `handleIMMessageWithLoop` 中 `runAgentLoop` 调用前新增 SubAgent 拦截
- 检测条件：`workflowAgentLoop=true` + 当前阶段为 `implementation` + orchestrator 未激活
- 满足条件时：从 `ws.PhaseOutputs["task_breakdown"]` 解析任务列表 → `ParseTaskListFromText` → `orchestrator.Activate()`
- orchestrator 激活后：`ShouldUseSubAgent()` 返回 true → `SubAgentTaskRunner.RunAllTasks()` 接管
- 所有任务完成后 `orchestrator.Deactivate()`，返回执行报告

**Context 效率对比**：

| 指标 | 主 Agent | SubAgent | 改善 |
|------|---------|----------|------|
| System prompt | ~12,000 token | ~2,000 token | -83% |
| 工具定义 | ~15,000 token (40+工具) | ~744 token (5工具) | -95% |
| 初始开销 | ~40,000 token | ~7,000 token | -82% |
| 可用编码空间 | ~62,000 token | ~95,000 token | +53% |
| 单任务 context 隔离 | ❌ 所有任务共享 | ✅ 每个任务独立 | 不会累积膨胀 |

**设计要点**：
- SubAgent 不是独立进程——是同一个 Go 进程内的独立 LLM 对话，共享 HTTP client 和 LLM config
- 复用 `corelib/agent.RunLoop`——不重新实现 agent loop
- 复用主 Agent 的工具实现——`toolReadFile` 等方法通过 `IMMessageHandler` 引用调用
- `TaskExecutionOrchestrator` 终于被激活——`Activate()` 从未被调用的问题解决
- 与外部编程工具并存——`ShouldUseSubAgent` 只在 direct mode 时返回 true，external mode 仍走 `create_session`

**测试**：10 个测试全部通过
- System prompt 纯净性（不含 IM/Browser/SSH/memory 噪音）
- 工具定义精简性（5 工具，744 token）
- Context 截断
- Orchestrator 路由决策（direct/external mode）
- 前置任务产出物收集

**验收标准**：
- 编码工作流确认任务列表后 → orchestrator 被激活 → SubAgent 逐任务执行
- 每个任务在独立的 ~7K 初始 context 中执行，不会膨胀到 134K
- 任务失败自动重试（最多 3 次）
- 所有任务完成后生成执行报告
- 无外部编程工具时自动使用 SubAgent（direct mode）
- 有外部编程工具时仍走 create_session（external mode）
- GUI / TUI / corelib 编译通过


### 76. SubAgent Path 2 越权激活 orchestrator——新编码任务跳过三阶段直接编码

**来源**：用户截图——"在d:\workprj\morio 下开发一个c++ 的超级玛利游戏。图形界面，cmake管理，有音效。" 发送后，agent 直接检查现有代码、修复编译错误、创建 CMakeLists.txt，完全跳过了需求→设计→任务分解三阶段流程。

**根因**：`gui/im_message_handler.go` 中 SubAgent 拦截的 Path 2a/2b 越权激活了 `TaskExecutionOrchestrator`，绕过了工作流引擎的三阶段生命周期管理。

**权限链违规**：
- **工作流引擎**拥有三阶段生命周期（需求→设计→任务分解→实现）
- **Orchestrator** 拥有实现阶段的任务执行（仅在工作流进入 implementation 阶段后）
- **SubAgent** 拥有纯净上下文编码（仅在 orchestrator 委派时）

Path 2a 将三步压缩为一步：检测编码意图 → 激活 orchestrator → SubAgent 直接编码。它绕过了工作流引擎，直接从意图分类跳到代码执行。

**触发链路**：
1. 工作流拦截（`handleWorkflowInterception`）因 UIC 未就绪/IUM 超时/LLM 不可用等原因返回 nil
2. Path 2a 检测到 `preGate.active=true`（"开发" 是 `creationCodingKeywords`）
3. `taskOrch.Activate(singleTask, "", "", projectPath, "")` — 用原始用户消息作为单个任务激活 orchestrator，无需求/设计上下文
4. `ShouldUseSubAgent()` 返回 true（`Tool=""` → `TaskExecModeDirect`）
5. SubAgent 接管，直接编码

**叠加效应**：orchestrator 激活后，主 agent loop 中的 Coding Tool Gate 检查 `!orchestratorActive()` 返回 false，gate 被禁用。即使消息 fall through 到主 agent loop，gate 也不会拦截编码工具。

**修复**：删除 Path 2a 和 Path 2b，orchestrator 只从工作流引擎的 implementation 阶段激活（原 Path 1）。

- `gui/im_message_handler.go`：
  - 删除 Path 2a（`preGate.active && !preGate.skipSignal` → 单任务 Activate）
  - 删除 Path 2b（`preGate.skipSignal` + `conversationHasCodingContext` → 单任务 Activate）
  - 保留 Path 1（`workflowAgentLoop && ws.CurrentPhase == PhaseCodingImplementation` → 从任务分解文档解析任务列表 → Activate）
  - Path 1 新增降级日志：`ParseTaskListFromText` 返回 0 任务或 `task_breakdown` 输出为空时记录原因，便于排查
  - 更新注释说明权限链：工作流引擎 → orchestrator → SubAgent
  - 更新 coding iteration budget 注释（移除对已删除 Path 2 的引用）
- `gui/im_session_state.go`：
  - `clearPerUserSessionState` 新增 orchestrator 的 `Deactivate()` 调用，防止会话重置后残留活跃 orchestrator 将下一条消息路由到 SubAgent

**设计原则**：orchestrator 的激活权限只属于工作流引擎的 implementation 阶段。意图分类的职责是路由到工作流引擎（强制三阶段）或主 agent loop（Coding Tool Gate 强制三阶段），不是直接路由到 orchestrator（跳过三阶段）。

**正确的执行链路**（修复后）：
```
用户: "开发一个c++的超级玛利游戏"
  → QuickFilter → FilterNeedsUnderstanding
  → handleNeedsUnderstanding → UIC/IUM 分类 → StartWorkflow(coding)
  → 返回 "🚀 工作流已启动"
  → 用户确认需求 → 用户确认设计 → 用户确认任务列表
  → 工作流进入 implementation 阶段
  → Path 1 激活 orchestrator（带需求/设计上下文）
  → SubAgent 逐任务执行
```

**降级链路**（工作流引擎不可用时）：
```
用户: "开发一个c++的超级玛利游戏"
  → handleWorkflowInterception 返回 nil（UIC/IUM 不可用）
  → fall through 到主 agent loop
  → Coding Tool Gate: gateConfig.active=true（"开发" 关键词）
  → 拦截编码工具，注入系统消息"请先生成需求文档"
  → LLM 生成需求文档 → force-return 等待用户确认
  → 三阶段流程在 agent loop 内部由 gate 强制执行
```

**验收标准**：
- "开发一个c++的超级玛利游戏" → 工作流引擎启动三阶段，不直接编码
- 工作流引擎不可用时 → Coding Tool Gate 在 agent loop 内强制三阶段
- 工作流进入 implementation 阶段后 → orchestrator 正常激活，SubAgent 正常执行
- 所有 14 个 CodingGate 测试通过
- 所有 GateIntent / Orchestrator / SubAgent / RouteTools 测试通过
- GUI 编译通过


### 77. 工作流启动后停滞——StartWorkflow 后未触发 Agent Loop 的机制性断裂

**来源**：用户截图——"在d:\workprj\steave2 下开发一个c++的警察抓小偷游戏。图形界面，画面精美，cmake管理，有音效。" 发送后，面板显示"🚀 工作流已启动：coding，📋 当前阶段：requirements"，然后就停了。右侧面板显示"暂无文档内容"，LLM 没有生成需求文档。

**根因（机制性分析）**：

`StartWorkflow` 和 `HandleInput` 之间存在**职责断裂**：

- `StartWorkflow` 创建 `*WorkflowState`（phase 0），不返回 `WorkflowResponse`
- `HandleInput` 处理已有活跃工作流的输入，返回 `WorkflowResponse`（含 `PhasePrompt` + `RunAgentLoop=true`）
- `handleActiveWorkflow` 是**唯一**知道如何消费 `WorkflowResponse` 的函数（设置 `stashedPhasePrompt` + `workflowAgentLoopMarker` → return nil → agent loop 运行）

但 GUI 侧有 **3 个独立的 `StartWorkflow` 调用点**，每个都在 `StartWorkflow` 成功后自己处理后续逻辑：

| 调用点 | 位置 | 修复前行为 | 问题 |
|--------|------|-----------|------|
| UIC 路径 | `handleNeedsUnderstanding` | 直接返回 `&IMAgentResponse{Text: overview}` | **完全没有触发 agent loop**（本次 bug） |
| IUM 路径 | `handleActiveUnderstanding` | 手动 `BuildPhaseSystemPrompt` + 手动设置 markers | Workaround——复制了 `handleActiveWorkflow` 的逻辑 |
| Keyword fallback | `tryKeywordWorkflowFallback` | 直接返回 `&IMAgentResponse{Text: overview}` | **完全没有触发 agent loop**（同样的 bug） |

三个调用点各自实现（或遗漏）了 marker 设置逻辑，违反了 DRY 原则。`handleActiveWorkflow` 是消费 `WorkflowResponse` 的单一代码路径，但 `StartWorkflow` 的调用方绕过了它。

**日志证据**（`maclaw.log` 05:40:41）：
```
[WorkflowInterception] UIC fusion determined workflow: coding
[AI assistant] agent_loop=2.380381s first_token=0s  ← agent loop 耗时 2.38s 但 first_token=0s，没有 LLM 调用
```
05:40:41 之后没有任何 LLM usage 日志，没有 trajectory 文件。Agent loop 根本没有运行。

**修复（机制性——所有 StartWorkflow 路径收敛到 handleActiveWorkflow）**：

所有 3 个 GUI 侧 `StartWorkflow` 调用点在成功后，不再自己处理后续逻辑，而是统一调用 `handlePostStartWorkflow()` 辅助函数：

#### `handlePostStartWorkflow(engine, userID, text, state, extraText)` — 单一实现

1. `EmitSuggestMaximize`（桌面面板全屏建议）
2. 构建 overview 文本（"🚀 工作流已启动"），可选追加 `extraText`（IUM 的 reply）
3. 输入驱动型工作流（`RequiresInput` 非空）→ 返回 overview + 输入引导，等待用户上传
4. 非输入驱动型工作流 → `SendTextToUser(overview)` → `handleActiveWorkflow(engine, userID, text)`

`handleActiveWorkflow` 的执行链路：
- cross-type detection：UIC 缓存命中（同一消息已分类过），类型匹配（coding == coding），不触发取消
- `HandleInput` → default 分支 → `RunAgentLoop=true` + `PhasePrompt`
- 设置 `stashedPhasePrompt` + `workflowAgentLoopMarker`
- return nil → agent loop 运行 → LLM 生成需求文档

#### 三个调用点简化为一行

| 调用点 | 修复后代码 |
|--------|-----------|
| UIC 路径 | `return h.handlePostStartWorkflow(engine, userID, text, state, "")` |
| IUM 路径 | `return h.handlePostStartWorkflow(engine, userID, text, state, reply)` |
| Keyword fallback | `return h.handlePostStartWorkflow(engine, userID, text, state, "")` |

**不变量**：`handleActiveWorkflow` 是 GUI 侧消费 `WorkflowResponse` 的**单一代码路径**。所有 `StartWorkflow` 调用点在成功后收敛到它，不自己实现 marker 设置逻辑。

**TUI 路径**：TUI 有自己的 marker 机制（`pendingPhasePrompt` / `workflowAgentLoop` 字段），不共享 GUI 的 `sync.Map`。TUI 路径仍然手动调用 `BuildPhaseSystemPrompt`，这是 TUI 架构的已知限制，不在本次修复范围内。

**验收标准**：
- 用户发送编码需求 → 工作流启动 → 自动进入 requirements 阶段 → LLM 生成需求文档 → 右侧面板显示文档内容
- 输入驱动型工作流（合同审查等）→ 仍然返回 overview + 输入引导，等待用户上传
- Keyword fallback 路径 → 同样自动触发第一阶段 agent loop（之前完全没有）
- IUM 路径 → 不再手动调用 `BuildPhaseSystemPrompt`，委托给 `handleActiveWorkflow`
- 所有 CodingWorkflowProperty 1-5 测试通过
- 所有 CodingGate / GateIntent / WorkflowCandidate / CodingSession 测试通过
- corelib/workflow 所有测试通过
- GUI 编译通过



### 78. NeedsConfirm 门控在首次执行时永久失效——移除冗余的 HasPhaseOutput 前置检查

**来源**：用户截图——PPT 设计工作流的逐页脚本阶段（slide_scripting），LLM 输出全部 26 页详稿后说"请确认或提出修改意见"，然后立即自答"好的，全部26页详稿已确认完毕"并开始生成 PPT Python 脚本。用户没有机会确认或修改。

**根因**：`engineGateActive` 的计算有一个前置检查 `HasPhaseOutput(userID)`——当该方法返回 `false` 时，门控被禁用。`HasPhaseOutput` 检查 `WorkflowState.PhaseOutputs[currentPhase]` 是否非空，但 `SavePhaseOutput` 只在 agent loop **退出后**（post-loop doc capture）才被调用。在 agent loop 运行期间，`HasPhaseOutput` 始终返回 `false`，门控在首次执行时被永久禁用。

**鸡生蛋死循环**：
- 门控需要 `HasPhaseOutput=true` 才能激活
- `SavePhaseOutput` 需要 agent loop 退出才能执行
- Agent loop 需要门控 force-return 才能退出
- 门控需要 `HasPhaseOutput=true` 才能激活 → 死循环

**影响范围**：所有 19 个工作流模板的所有 `NeedsConfirm=true` 阶段。编码工作流因为有 `needsConfirmFromSteering`（依赖 `gateConfig.active`，不依赖 `HasPhaseOutput`）作为第二道防线，所以不受影响。但非编码工作流（PPT 设计、产品设计、商业计划等）只有 `needsConfirmFromEngine` 一道防线，全部受影响。

**机制性分析**：`HasPhaseOutput` 检查的设计意图是区分"首次执行"（让 LLM 生成文档）和"再次执行"（用户确认后回来）。但门控内部已有 `isSubstantivePhaseDocument(trimmedForGate)` 检查——它在 LLM 输出不足 200 rune 且无结构化标记时返回 false，让 loop 继续。这两个检查解决的是同一个问题："不要在 LLM 还没产出文档时就 force-return"。`isSubstantivePhaseDocument` 基于当前迭代的实际输出内容判断，是正确的守卫；`HasPhaseOutput` 基于 post-loop 持久化状态判断，在首次执行时永远为 false，是冗余且有害的守卫。

**修复**（机制性修复——移除冗余守卫，让正确的守卫生效）：

- `gui/im_message_handler.go`（no-tool 分支）：移除 `engineGateActive` 计算中的 `HasPhaseOutput` 检查。`engineGateActive` 直接等于 `needsConfirmFromEngine`。门控是否 force-return 完全由 `isSubstantivePhaseDocument` 决定。
- `gui/im_message_handler.go`（tool 分支）：同步移除 `needsConfirmToolBranch` 中的 `HasPhaseOutput` 检查。

**修改前**：
```go
engineGateActive := needsConfirmFromEngine
if engineGateActive && h.getWorkflowEngine() != nil {
    if !h.getWorkflowEngine().HasPhaseOutput(userID) {
        engineGateActive = false  // 首次执行，禁用门控 ← 冗余且有害
    }
}
```

**修改后**：
```go
engineGateActive := needsConfirmFromEngine
// 不检查 HasPhaseOutput。门控依赖 isSubstantivePhaseDocument()
// 区分"LLM 还没产出文档"（短前言，门控跳过）和"LLM 已产出文档"
// （实质性文本，门控触发）。HasPhaseOutput 是 post-loop 持久化标志，
// 在首次执行时永远为 false，会永久禁用门控。
```

**为什么不是 workaround**：
- 不新增状态标记（如 `PhaseDocProducedInLoop`）来补丁 `HasPhaseOutput` 的缺陷
- 不新增 if-else 分支来处理"首次执行 vs 再次执行"
- 直接移除冗余守卫，让已有的正确守卫（`isSubstantivePhaseDocument`）独立生效
- `HasPhaseOutput` 方法本身保留（用于 post-loop doc capture 等其他场景），只是不再用于门控决策

**边界情况验证**：
- LLM 输出短前言"让我来生成..." → `isSubstantivePhaseDocument` = false → 门控跳过 → loop 继续 ✅
- LLM 输出完整文档 → `isSubstantivePhaseDocument` = true → 门控触发 → force-return ✅
- LLM 输出文档 + 自我确认 → `containsSelfConfirmationPattern` 截断自答 → `isSubstantivePhaseDocument` = true → force-return ✅
- LLM 输出 stall reply → `looksLikeNoToolStallReply` = true → 门控跳过 → loop 继续 ✅
- `NeedsConfirm=false` 阶段 → `needsConfirmFromEngine = false` → 门控不激活 ✅
- 用户确认后回来，LLM 输出短过渡 → `isSubstantivePhaseDocument` = false → 门控跳过 ✅

**验收标准**：
- PPT 工作流 slide_scripting 阶段：LLM 输出详稿后 → 返回给用户确认，不自动继续
- 所有 19 个工作流模板的 NeedsConfirm=true 阶段：门控在 LLM 产出实质性文档时立即触发
- 编码工作流：行为不变（`needsConfirmFromSteering` 路径不受影响）
- 所有 Property / CodingGate / NeedsConfirm / SelfConfirm / BugCondition 测试通过
- corelib/workflow 所有测试通过
- GUI 编译通过


### 79. Browser: 前缀幻觉 + 重复输出——根因修复：30 个 browser_* 工具合并为 1 个 + repetitionFilter 段落层

**来源**：用户截图——maclaw 输出了重复的 Markdown 表格内容（命令列表重复 3 次），末尾出现 `Browser: 请先在浏览器中登录小红书网页版...` 幻觉前缀。

#### 根因分析（本质原因）

**为什么只有 Browser 出现角色前缀幻觉，SSH/Bash/WriteFile 从不出现？**

两个因素叠加：

1. **训练数据中的 multi-agent 角色名**：LLM 训练语料包含大量 multi-agent 框架（AutoGPT、BabyAGI、MetaGPT 等）的对话记录，使用 `Browser:`、`Tool:` 等角色前缀。`Browser` 是训练数据中的高频 agent 角色名。而 `ssh`、`bash`、`write_file` 是工具名/命令名，不是 agent 角色名——训练数据中没有 `SSH: 好的我来连接...` 这种格式。

2. **30 个工具定义的 context token 密度**：`ssh` 只有 1 个定义（~200 token），`bash` 也只有 1 个。但 browser 有 **30 个定义**（`browser_session_start`、`browser_navigate`、`browser_click`...），每个带 description 和 parameters，总共 **~4500 token**。30 个以 `browser_` 开头的工具定义在 LLM context 中形成压倒性信号，激活训练数据中的 Browser agent 角色模式。

**根因不是"browser 工具被召回"，而是"30 个 browser_* 工具定义在 context 中的 token 密度，激活了模型训练数据中的 Browser agent 角色模式"。**

之前的修复（#27 编码工作流移除 browser 定义、#34 语义确认、#45 memory-driven pin 过滤、#47 system prompt 约束、#50 数据流统一）都是**症状层面的防护**——在各种场景下阻止 browser 工具定义进入 context，或在输出端拦截幻觉。根本解决方案是**在输入端消除 token 密度**。

#### 根因修复：30 个 browser_* 工具合并为 1 个 `browser` 工具

**设计**：复用 `#14` 的工具合并模式（`manage_config`/`manage_template`/`manage_schedule`），将 30 个 `browser_*` 工具合并为 1 个 `browser(action=...)` 工具。

- `gui/tools_browser_merged.go`（已有，之前是死代码）：
  - `MergedBrowserToolName = "browser"`
  - `mergedBrowserToolDescription`：列出所有 action 的一行描述
  - `mergedBrowserInputSchema`：公共参数（action, session_id, url, ref, selector, text 等）
  - `dispatchMergedBrowser()`：根据 action 参数路由到对应的 `browser_*` handler

- `gui/tools_browser.go`（重写）：
  - 个别 `browser_*` 工具仍注册到 registry（handler 可用于 dispatch），但 **Description 设为空**
  - `BuildAll()` 跳过 `Description == ""` 的工具（已有机制，`#14` 引入）
  - 只有合并后的 `browser` 工具有 Description，是 LLM 唯一可见的 browser 工具定义
  - LLM 调用 `browser(action="navigate", url="...")` → `dispatchMergedBrowser` → `browser_navigate` handler

- `corelib/tool/router.go`：
  - `allBrowserToolNames` 从 30 个名字改为 `["browser", "browser_task_run", ..., "gui_record_stop"]`（1 个合并 + 10 个低频个别工具）
  - `allConditionalKeepTools` 同步更新
  - `noEagerPinTools` 自动从 `allBrowserToolNames` 生成（不需要改）

- `gui/coding_tool_gate.go`：`codingToolBlocklist` 从 30 个名字改为 `"browser"` + 10 个低频工具

- `corelib/intent/definitions.go` + `tool_affinity.go`：browser 意图的 ToolNames 从 20+ 个改为 `["browser"]`

**Token 密度对比**：

| 指标 | 修复前 | 修复后 | 降幅 |
|------|--------|--------|------|
| LLM 可见的 browser 工具定义数 | 30 | 1 | -97% |
| browser 工具定义占用 token | ~4500 | ~500 | -89% |
| `browser_` 前缀在 context 中出现次数 | ~60（名字+描述） | 0 | -100% |

**为什么这是机制性修复而非 workaround**：
- 不依赖场景判断（编码工作流/工作流阶段/memory 内容）来决定是否移除 browser 定义
- 不依赖输出端过滤（流式过滤器/后处理）来拦截幻觉
- 从根本上消除了触发条件——LLM context 中不再有 30 个 `browser_*` 前缀的工具定义
- 对所有场景通用：IM 通道、桌面面板、编码工作流、非编码工作流

#### 辅助修复 1：repetitionFilter 新增 Layer 2（段落级检测）

**根因**：`repetitionFilter` 只有句子级检测（Layer 1），按 `。！？!?` 分割。Markdown 表格不含这些标点，重复检测完全失效。

**修复**：`Write()` 重写为双边界分割——同时检测句子边界和段落边界（`\n\n`），哪个先出现就先处理。段落内容 normalized 后存入 `recentParagraphs` 滑动窗口，用统一的 `detectRepetition(window, maxPatLen)` 方法检测重复。

- `gui/llm_stream_repetition_filter.go`：
  - `Write()` 中 `findSentenceBoundary` 和 `findParagraphBreak` 并行扫描
  - 新增 `drainSentence()` 和 `drainParagraph()` 方法，分别处理两种边界
  - `detectRepetition()` 统一为接受 `(window []string, maxPatLen int)` 的通用方法，Layer 1 和 Layer 2 共享
  - 无 downstream 包装（消除递归风险），无 `emittedBuf` 二次分割（消除 DRY 违反）

#### 辅助修复 2：rolePrefixStreamFilter 新增 `checkMidLinePrefix`

**修复**：`Write()` 中无 `\n` 时，当 `lineBuf` 超过 40 字节阈值，用 `midLineRolePrefixRe`（`\n[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)`）扫描 buffer 中嵌入的 `\n` + 角色前缀。

去掉了第一版的 `truncateAtMidLinePrefix`（`|` 边界检查）——这是 workaround，只覆盖表格场景。`checkMidLinePrefix` 基于 `\n` 边界，是通用机制。

**三层防护的关系（修复后）**：
1. **根因层**（本次）：合并工具定义，消除 token 密度触发条件 → 幻觉不产生
2. **流式层**（#47）：`rolePrefixStreamFilter` + `checkMidLinePrefix` → 幻觉产生时实时拦截
3. **后处理层**（#47）：`stripRolePrefixHallucination` → 最终兜底清理

根因层消除了 99% 的触发场景。流式层和后处理层作为纵深防御保留，处理极端边缘情况（如对话历史中的"浏览器"一词仍可能触发微弱的角色切换）。

**验收标准**：
- LLM context 中只有 1 个 `browser` 工具定义（~500 token），不再有 30 个 `browser_*` 定义
- `browser(action="navigate", url="...")` 正确路由到 `browser_navigate` handler
- Markdown 表格重复 2 次后 → repetitionFilter Layer 2 halt
- 所有 corelib/tool 测试通过（含 router/BM25/memory-pin/semantic-confirm）
- 所有 gui 测试通过（含 browser registration/repetition filter/role prefix filter/coding gate）
- GUI + corelib 编译通过


### 80. 安全护栏机制性修复——误判消除 + Guard 机制 + 开发者模式

**来源**：安全护栏在实际使用中存在误判问题，同时安全研究人员需要一个不拦截任何操作的模式。

#### 根因分析（三层误判）

1. **`dangerousKeywords` 纯子串匹配**：`"sudo"` 匹配 `"pseudo"`、`"sudoku"`、文档中的 `"Run without sudo"`
2. **`threatPatternCategories` 正则过于宽泛**：`\$\(.*\)` 匹配所有命令替换（包括 `$(date)`）；`\.env` 匹配 `.env.example`；`exec\s*\(` 匹配 `executor.execute()`；`python\s+-c` 匹配 `python -c "print('hello')"`
3. **`denyDangerous` 策略规则**：`sudo` 无词边界，同样匹配子串

#### 修复 1: ThreatPattern Guard 机制（模式级上下文感知）

- `corelib/security/risk_assessor.go`：`ThreatPattern` 结构体新增 `Guard string` 字段
- `compiledPattern` 新增 `GuardRe *regexp.Regexp` 字段
- `matchPattern()` 在 pattern 命中后检查 guard——guard 也命中则视为误报，返回 false
- `init()` 编译 guard 正则

已添加 guard 的高误报模式（12 个）：
- `$(...)`（injection）→ guard 允许 `$(date)`, `$(pwd)`, `$(basename ...)` 等安全替换
- `eval(` / `exec(`（injection）→ guard 允许 `executor`, `evaluator`, `exec.Command`
- `base64 -d` / `base64 --decode`（obfuscation）→ guard 允许解码到图片/PDF/JSON/证书
- `\x??`（obfuscation）→ guard 允许 log/txt/csv/json 文件上下文
- `chmod +x`（execution）→ guard 允许 `./` 路径和 `.sh/.py` 脚本
- `python -c`（execution）→ guard 允许 `import json`, `print(`, `sys.version`
- `.bashrc` / `.bash_profile` / `.profile`（persistence）→ guard 允许只读访问和 source
- `.env`（credential_exposure）→ guard 允许 `.env.example`, `.env.local`, `venv/.env`
- `password=` / `api_key=` / `secret_key=`（credential_exposure）→ guard 允许空值/占位符
- `npm install @latest`（supply_chain）→ guard 允许 `@types/`, `@babel/` 等知名 scoped 包
- `go install @`（supply_chain）→ guard 允许 `golang.org/`, `github.com/`

**设计原则**：Guard 只做"降级"（从命中变为不命中），不做"升级"。最坏情况是 guard 没生效，pattern 正常命中——不会漏掉真正的威胁。

#### 修复 2: dangerousCmdPatterns 上下文感知

- `corelib/security/risk_assessor.go`：`"sudo"` 从 `dangerousKeywords`（纯子串）移到 `dangerousCmdPatterns`（`\bsudo\b` 词边界正则）
- 新增 `dangerousCmdSafeContexts`：`sudo apt install`、`sudo systemctl start`、`sudo docker`、`sudo pip install`、`sudo -n`、`sudo chown $USER` 等安全上下文
- 命中 `\bsudo\b` 但在安全上下文中 → 降级到 High（而非 Critical）
- 新增 `CheckDangerousCmdPatterns()` 导出函数，gui 和 corelib 共享同一套逻辑
- `gui/risk_assessor.go`：`Assess()` 调用 `security.CheckDangerousCmdPatterns()`

#### 修复 3: PolicyEngine denyDangerous 规则词边界

- `gui/policy_engine.go` + `corelib/security/policy_engine.go`：`(?i)(rm\s+-rf|DROP\s+TABLE|sudo)` → `(?i)(rm\s+-rf|DROP\s+TABLE|\bsudo\b)`

#### 修复 4: Guard 正则 `^` 锚点 bug

- `$(...)`（injection）guard 的 `^\$\((date|pwd|...)` 中 `^` 要求 `$` 在字符串开头，但被匹配的是整个 flattened args 字符串（如 `echo "today is $(date)"`），guard 永远不匹配 → 移除 `^`
- `.bashrc`（persistence）guard 的 `^\.\s+` 同理 → 移除 `^`

#### 开发者模式

- `corelib/security/policy_engine.go` + `gui/policy_engine.go`：`PolicyEngine` 新增 `developerMode bool` 字段 + `IsDeveloperMode()` 方法
- `PolicyRulesForMode("developer")` 返回全 allow 规则集（无 deny、无 ask、无 audit）
- `corelib/security/firewall.go` + `gui/security_firewall.go`：`Check()` 开头检查 `IsDeveloperMode()`，true 则直接返回 `(true, "")`
- `gui/im_app_accessors.go`：新增 `isSecurityDeveloperMode()` 辅助方法
- `gui/im_message_handler.go` + `gui/im_tools_misc.go` + `gui/capability_gap_detector.go`：Skill 安装安全审查在 developer 模式下跳过
- `tui/views/config_fields.go`：安全策略模式选项新增 `"developer"`
- `corelib/app_config.go`：`SecurityPolicyMode` 字段支持 `"developer"` 值

#### 测试覆盖

- `corelib/security/guard_mechanism_test.go`：22 个新增测试
  - Guard 机制：5 个（suppress/not-suppress/no-guard/substring/substring-with-guard）
  - ScanThreatPatterns 集成：6 个（safe builtins/dangerous still caught/chmod safe/chmod dangerous/env example/env real）
  - CheckDangerousCmdPatterns：7 个（bare sudo/apt install/docker/systemctl/no sudo/pseudo/sudoku）
  - Developer 模式：4 个（policy allows/standard denies/SetMode switch/firewall bypass+deny）

**验收标准**：
- `$(date)` 不触发 injection，`$(curl evil.com)` 仍触发
- `chmod +x ./build.sh` 不触发 execution，`chmod +x /usr/bin/malware` 仍触发
- `.env.example` 不触发 credential_exposure，`upload .env to server` 仍触发
- `sudo apt install python3` 降级到 High，`sudo rm -rf /` 仍为 Critical
- `pseudo`、`sudoku` 不匹配 `\bsudo\b`
- developer 模式下所有操作放行
- 22 个新增测试 + 所有现有安全测试通过
- corelib/security、gui、tui、cmd/maclaw-tool 编译通过

### 81. 工作流开关无法关闭——前端绑定缺失 + 后端 steering 路径不受控

**来源**：用户在全局设置→系统设置中点击"打开工作流"checkbox 无法关闭。

#### 根因 1 (P0): TypeScript 绑定缺少 `workflow_enabled` 字段

Go 的 `AppConfig` 有 `WorkflowEnabled *bool` 字段，但 Wails 生成的 TypeScript 绑定 `models.ts` 中的 `AppConfig` 类没有这个字段。TypeScript 的 `AppConfig` 构造函数逐字段从 `source` 中赋值（不是 `Object.assign`），所以 `workflow_enabled` 在 `new main.AppConfig({ ...base, workflow_enabled: false })` 时被静默丢弃。`SaveConfig` 发送给后端的对象不包含该字段，Go 的 `omitempty` 使其保持原值——checkbox 点击无效。

**修复**：
- `gui/frontend/wailsjs/go/models.ts`：`AppConfig` 类新增 `workflow_enabled?: boolean` 字段声明和构造函数赋值 `this.workflow_enabled = source["workflow_enabled"]`
- `gui/frontend/src/App.tsx`：清理 `as any` 类型断言

#### 根因 2 (P1): 开关只控制引擎驱动的工作流，不控制 steering 驱动的编码门控

`workflow_enabled=false` 时，`getWorkflowEngine()` 返回 nil，引擎驱动的工作流（19 个模板的多阶段流程）被正确禁用。但 **steering 驱动的编码门控**（`CodingToolGate` + `SteeringWorkflowDetector`）完全不检查这个开关——它们基于意图分类（`gateConfig.active`）和对话历史（`conversationHasCodingContext()`）独立运行。

用户关闭"打开工作流"后发送"开发一个游戏"：
- ✅ 引擎不启动（`handleWorkflowInterception` 短路）
- ❌ `CodingToolGate` 仍然激活，拦截编码工具
- ❌ `SteeringWorkflowDetector` 仍然激活，发射全屏 banner 和 doc preview 事件

**修复**：
- `gui/im_message_handler.go`：`runAgentLoop` 中新增 `workflowOff` 局部变量（从 `h.app.workflowDisabled` 原子变量读取一次），在两个消费点使用：
  1. `gateConfig` 计算后：`workflowOff` 时强制 `gateConfig.active = false`
  2. `SteeringWorkflowDetector` 激活前：`workflowOff` 时强制 `shouldActivate = false`

**机制性保证**：`workflowDisabled` 是单一数据源（`atomic.Bool`），由 `SaveConfig`/`LoadConfig` 在 mutex 内同步更新。三个消费点行为一致：
1. `getWorkflowEngine()` → 返回 nil（引擎驱动的工作流禁用）
2. `gateConfig.active` → 强制 false（steering 驱动的编码门控禁用）
3. `shouldActivate` → 强制 false（steering 检测器不激活）

**测试**：
- `corelib/app_config_test.go`：新增 `TestIsWorkflowEnabled`——验证三态行为（nil→true, true→true, false→false）和 JSON round-trip

**验收标准**：
- 关闭"打开工作流" → 发送"开发一个游戏" → LLM 直接编码，不走三阶段
- 开启"打开工作流" → 发送"开发一个游戏" → 正常走三阶段流程
- 所有 CodingGate / GateIntent / RouteTools 测试通过
- GUI / TUI / corelib 编译通过


### 82. 对话历史为空但长期记忆有活跃项目时——短指令"开工"被当作新任务

**来源**：用户截图——说"开工"后，maclaw 回复"伯伯早呀！今天要做什么活儿？是接着搞那个 C++ 打飞机游戏（test5），还是有什么新任务？"。maclaw 记得项目但不自动继续。

**根因**：proactive recall 从长期记忆中召回了 C++ 打飞机游戏的 `project_knowledge`，但只作为"参考信息"注入 system prompt（`相关记忆（自动召回）`）。LLM 看到空对话历史 + 参考记忆 + "开工"两个字，合理地反问用户要做什么——因为没有任何信号告诉它"这是你应该继续的活跃项目"。

**核心问题**：proactive recall 的语义是"参考信息"，但在空历史 + 短续接信号的场景下，语义应该是"活跃项目上下文"。

**修复**（纯 prompt 层——不改决策层）：

在 `appendProactiveRecall()`（`gui/im_system_prompt.go`）中，当召回的记忆包含 `project_knowledge` 或 `task_artifact` 条目，且用户消息是短续接信号（`0 < CountWords < 4`）时，在记忆注入后追加一段"活跃项目上下文"指令，告知 LLM：
- 上方召回的记忆包含之前正在进行的项目
- 对话历史已清空（可能因重启或会话过期），但项目仍然有效
- 用户发送了继续工作的信号，请简要确认项目状态后继续推进

**为什么不改 TaskContextManager**：

经过五轮 review 发现，当对话历史为空时，`TaskNew` 和 `TaskContinue` 的下游行为几乎完全相同——`clearPerUserSessionState` 在空状态下是 no-op，execution confirmation gate 对短消息不触发（`looksLikeFreshTaskRequest` 对 <4 words 返回 false）。唯一的实际差异是 system prompt 中是否有"活跃项目"指令。把这个指令放在 `appendProactiveRecall` 中（它已经知道召回了什么记忆）比在决策层添加 `HasRecalledProjectContext`/`MemoryBridged`/`HasRecentProjectContext` 三个新字段简单得多，且不改变任何决策逻辑。

**修改文件**：
- `gui/im_system_prompt.go`：`appendProactiveRecall()` 末尾新增"活跃项目桥接"逻辑——检查 recalled entries 中是否有 `project_knowledge`/`task_artifact`，结合 `CountWords(msg) < 4` 判断，注入指令

**验收标准**：
- 应用重启后说"开工" → 记忆中有 C++ 打飞机游戏 → LLM 回复"好的，继续 C++ 打飞机游戏项目 test5，上次进展到..."
- 应用重启后说"帮我开发一个新游戏"（≥4 words）→ 不注入指令 → LLM 正常处理新任务
- 记忆中无 project_knowledge → 不注入指令 → 行为不变
- 所有 14 个 Resolve 测试通过 + 所有 agent/memory 包测试通过
- GUI / corelib 编译通过


### 83. 截断的 tool call 导致 agent loop 提前退出 + capabilityGapDetector 误触发

**来源**：用户让 maclaw 生成 PPT，maclaw 尝试调用 `manage_skill(action="run", name="pptx-generator")` 但参数 JSON 被模型 `max_output_tokens` 截断。Agent loop 提前退出，然后 capabilityGapDetector 异步搜索到不相关的 skill 并尝试安装。

#### 根因 1 (P0): filterTruncatedToolCalls 注入的恢复提示被当作最终响应返回

**根因**：`filterTruncatedToolCalls` 移除截断的 tool call 后，将恢复提示（"请将大文件内容拆分为多次写入"）追加到 `msg.Content`，`finishReason` 改为 `"stop"`。Agent loop 进入 no-tool 分支，但没有任何机制识别"这是系统注入的截断恢复提示，LLM 还没有看到这个提示"。所有 `continue` 条件（`emptyVisibleResult`、`promiseOnlyDeliverable`、`noToolStall`）都不满足，代码 fall through 到 `agentStageFinalize`，将截断提示作为最终响应返回给用户。

**机制性问题**：`filterTruncatedToolCalls` 通过文本内容（`msg.Content += hint`）传递恢复信息，但 agent loop 的 no-tool 分支只看文本内容的语义（关键词匹配），不知道这段文本是系统注入的还是 LLM 自主生成的。

**修复**：

1. `corelib/llm/types.go`：`Choice` 结构体新增 `TruncatedToolCalls bool` 字段（`json:"-"`，仅进程内信号）
2. `gui/llm_stream.go`：`filterTruncatedToolCalls` 返回值从 `string` 改为 `(string, bool)`，第二个返回值表示是否有截断
3. 四条流式路径（OpenAI SSE、Responses API、Responses WebSocket）在构建 `llm.Response` 时设置 `TruncatedToolCalls` 标志
4. `gui/im_message_handler.go`：
   - `agentLoopPhase` 新增 `TruncationRetries int` 字段
   - no-tool 分支在 tool hallucination correction 之后、finish_reason=length 续写之前，新增截断恢复逻辑
   - 检测到 `choice.TruncatedToolCalls` 时：重置 `ConsecutiveNoTool`，将截断提示注入 conversation 作为 system message，`continue` 让 LLM 在下一轮看到提示并重试
   - 最多重试 3 次（`maxTruncationRetries`），防止无限循环

#### 根因 2 (P1): capabilityGapDetector 被截断提示误触发

**根因**：截断提示中的"参数不完整"等词被 `llmDetectGap` 判定为能力缺口。但截断是输出长度限制问题，不是能力缺口——LLM 有 `manage_skill` 工具，只是参数太长被截断了。异步搜索用的是用户原始消息（"面向普通人，发展成就，20页"），搜到完全不相关的 "Academic Press Release Writing" skill。

**修复**：`skipCapabilityGap` 条件新增 `choice.TruncatedToolCalls` 检查——截断时跳过能力缺口检测（连异步都不触发）。

**设计原则**：
- 结构化信号（`TruncatedToolCalls bool`）替代文本内容匹配——不依赖关键词，对所有语言和工具通用
- 截断恢复与 finish_reason=length 续写是同一类问题（输出长度限制），放在相邻位置
- `TruncatedToolCalls` 是 `json:"-"` 字段，不影响序列化/反序列化

**验收标准**：
- `manage_skill` 参数被截断时 → agent loop continue，LLM 在下一轮看到截断提示并用更短参数重试
- 截断提示不触发 capabilityGapDetector
- 连续 3 次截断后 fall through 到 finalize（防止无限循环）
- 所有 12 个 RunAgentLoop 测试通过
- corelib/llm 全部 38 个测试通过
- GUI / corelib 编译通过


### 84. 漂移检测 Layer 2 (frequency) 误杀 SSH 多步工作流——统一结果推进检查

**来源**：用户让 maclaw 升级 api 服务器上的 omniroute docker 版本，maclaw 在执行 SSH 多步操作（connect → exec → check_task → exec → check_task...）时被漂移检测器强制终止。

**根因**：Layer 2 (`detectFrequencyAnomaly`) 只检查输入侧（哪个工具被调用了多少次），不检查输出侧（调用的结果是否表明任务在推进）。这与 Layer 1 修复前（#48）的问题完全相同——#48 给 Layer 1 加了 `resultsAreChanging()` 检查，但 Layer 2 缺少同样的检查。

SSH 操作天然是多步骤序列（connect → exec → check_task → exec → check_task...），8 次调用全部是 `ssh` 工具，占窗口 100%（远超 75% 阈值），且参数全不同（不是轮询模式）。Layer 2 将其判定为"语义循环"。但每次调用的结果完全不同——"连接成功"、"后台任务已提交"、"command not found"、"容器状态"、"git branch 列表"、"git fetch 完成"——任务在明确推进。

**机制性不变量**：如果窗口内的工具调用结果在变化（每次返回不同的内容），说明外部状态在推进，不是死循环。这个不变量对 Layer 1 和 Layer 2 都成立。

**修复**：

- `gui/drift_detector.go`：`detectFrequencyAnomaly()` 在 `isPollingPattern` 排除之后、触发漂移之前，新增 `freqResultsAreProgressing()` 检查
  - 收集窗口内 dominant tool 的所有结果（按时间顺序）
  - 比较连续结果对是否不同，>50% 的对不同 → 任务在推进 → 不触发漂移
  - 结果全部相同（如 memory(save) 4 次都返回"已保存"）→ 语义循环 → 触发漂移
  - 结果全部为空 → 保守处理，仍触发漂移
  - 优先使用 `ResultHash`（完整结果哈希），回退到 `ResultHint`（截断摘要）
- `gui/drift_detector.go`：`PreviewDrift()` 的 Layer 2 部分同步新增 `freqResultsAreProgressing` 检查
- `gui/im_message_handler.go`：无变更（Record 调用不变，不需要提取 ActionKey）

**为什么是机制性修复而非 workaround**：
- 不在输入侧做分桶（如按 action 分组）——换个场景（如 LLM 交替调用 `ssh(exec, cmd_A)` → `ssh(check_task, task_A)` → `ssh(exec, cmd_B)` → `ssh(check_task, task_B)`）分桶方案就失效
- 在输出侧做判断——复用 Layer 1 已有的 `resultsAreChanging` 机制的核心思想
- 对所有工具通用：ssh、memory、bash、manage_skill、任何 MCP 工具
- 利用已有的 `ResultHint`/`ResultHash` 字段，零额外开销
- 判定标准语义正确：漂移 = 同工具主导 + 结果不变（语义循环），不漂移 = 同工具主导 + 结果在变（任务推进）

**测试**：14 个现有测试全部通过 + 13 个新增测试通过
- `TestFrequencyAnomaly_SSHMultiStepWorkflow_ResultsProgressing_NotDetected`：SSH 多步工作流（结果全不同）→ 不触发
- `TestFrequencyAnomaly_SSHFullBugScenario_8Calls_NotDetected`：完整 8 次调用序列 → 不触发
- `TestFrequencyAnomaly_MemorySaveLoop_DifferentResults_NotDetected`：memory(save) 结果全不同 → 不触发（结果在推进）
- `TestFrequencyAnomaly_MemorySaveLoop_IdenticalResults_Detected`：memory(save) 结果全相同 → 触发
- `TestFrequencyAnomaly_BashSameErrorRepeated_Detected`：bash 4 次都返回 "permission denied" → 触发
- `TestFrequencyAnomaly_BashDifferentResults_NotDetected`：bash 4 次结果全不同 → 不触发
- `TestFreqResultsAreProgressing_*`：7 个单元测试覆盖全不同/全相同/多数不同/多数相同/全空/ResultHash 优先/忽略其他工具

**验收标准**：
- SSH 多步操作（connect → exec → check_task...）结果在变化 → 不触发 frequency 漂移
- memory(save) 4 次返回相同"已保存" → 仍然触发 frequency 漂移
- bash 4 次返回相同错误 → 仍然触发 frequency 漂移
- 所有 27 个漂移检测测试通过
- GUI / corelib 编译通过


### 85. LLM 调用失败后任务上下文丢失——错误退出路径未保存对话历史

**来源**：用户报告"maclaw在做任务时，如果当前是LLM服务出错，当前任务就会失忆，maclaw会记不得当前做了什么"。

**根因**：`runAgentLoop` 中有两个 LLM 错误退出路径直接 `return &IMAgentResponse{Error: ...}`，没有调用 `saveConversationHistoryTimed` 保存已积累的对话历史。当 LLM 调用失败（如 HTTP 429 rate limit、网络超时、API 返回 0 choices）时，agent loop 中已积累的 conversation entries（用户消息、之前的工具调用结果、assistant 响应等）全部丢失。用户说"继续吧"时，LLM 收到空的对话历史，完全不知道之前在做什么。

这与 #54（打断后失忆）和 #55（应用重启后任务上下文丢失）是同一类问题——`runAgentLoop` 的退出路径越权管理了历史生命周期。#54 修复了取消退出路径（`cancelledExitResponse`），但遗漏了 LLM 错误退出路径。

**机制性不变量**：`runAgentLoop` 的所有退出路径（正常完成、取消、错误）统一调用 `saveConversationHistoryTimed`，不调用 `memory.Clear`。`memory.Clear` 只在消息处理层使用。

**两个错误退出路径**：
1. `err != nil`（LLM 调用失败，含所有重试和 context trim 重试后仍失败）→ `return &IMAgentResponse{Error: "LLM 调用失败: ..."}`
2. `len(resp.Choices) == 0`（LLM 返回空 choices）→ `return &IMAgentResponse{Error: "LLM 未返回有效回复"}`

**修复**：
- `gui/im_message_handler.go`：新增 `llmErrorExitResponse()` 函数，与 `cancelledExitResponse()` 对称：
  - 调用 `stripTrailingBrokenToolGroup(history)` 清理可能的不完整工具调用组（LLM 错误可能发生在工具执行循环中间）
  - 调用 `h.saveConversationHistoryTimed(userID, history, nil)` 保存对话历史
  - 返回 `&IMAgentResponse{Error: errorMsg}`
- 两个错误退出路径从 `return &IMAgentResponse{Error: ...}` 改为 `return h.llmErrorExitResponse(userID, history, ...)`

**彻查：`runAgentLoop` 所有退出路径分类**：

| 退出路径 | 修复前 | 修复后 |
|---------|--------|--------|
| 正常完成（finalize） | `saveConversationHistoryTimed` ✅ | 不变 |
| 取消退出（cancel ×3） | `cancelledExitResponse` → `saveConversationHistoryTimed` ✅（#54 修复） | 不变 |
| 空响应 hard exit | `saveConversationHistoryTimed` ✅（#39 修复） | 不变 |
| LLM 调用失败 | `return &IMAgentResponse{Error}` ❌ **未保存** | `llmErrorExitResponse` → `saveConversationHistoryTimed` ✅ |
| LLM 返回 0 choices | `return &IMAgentResponse{Error}` ❌ **未保存** | `llmErrorExitResponse` → `saveConversationHistoryTimed` ✅ |

**验收标准**：
- LLM 调用失败（HTTP 429/超时/网络错误）后，用户说"继续" → LLM 有完整对话历史，知道之前在做什么
- LLM 返回 0 choices 后，用户说"继续" → 同上
- 正常完成和取消退出路径行为不变
- GUI 编译通过


### 85. LLM 429 Rate Limit 指数退避重试 + 错误恢复增强

**来源**：用户截图——maclaw 在执行任务时 LLM 返回 HTTP 429（rate limit），agent loop 直接报错退出。用户说"继续吧"后 maclaw 不知道之前在做什么。

#### 根因 1 (P0): 429 错误没有被 agent loop 正确重试

Agent loop 中 LLM 调用失败后的重试链路有三层，**全部不识别 429**：

| 层 | 机制 | 是否识别 429 | 问题 |
|---|------|-------------|------|
| Layer 1 | `AdaptiveRetry.Classify()` | ❌ `networkKeywords` 不含 429/rate_limit | 429 被分类为 `FailureUnknown` |
| Layer 2 | `AdaptiveRetry.Decide(FailureUnknown)` | — | `FailureUnknown` 只重试 1 次，延迟 1s |
| Layer 3 | `isRetryableLLMError()` | ❌ 不含 429 | 只处理 timeout/network/5xx |

同时，`isRateLimitError()` 函数**已经存在**且能正确识别 429，但**没有被 agent loop 的重试链路使用**。

#### 根因 2 (P1): LLM 错误退出后 in-flight task marker 被清除

`llmErrorExitResponse` 是正常 return，defer 中的 `ClearInFlightTask` 正常执行。下次用户说"继续"时，没有 in-flight marker，系统不知道上一次是异常中断的。

#### 根因 3 (P1): 错误消息不包含任务上下文

`llmErrorExitResponse` 返回的错误消息只有 LLM 技术错误信息，不包含"你之前在做什么"的上下文。

**修复**：

#### Fix 1: AdaptiveRetry 新增 `FailureRateLimit` 分类 + 指数退避

- `gui/adaptive_retry.go`：
  - 新增 `FailureRateLimit FailureCategory = "rate_limit"`
  - 新增 `maxRateLimitRetries = 3` 和 `baseRateLimitDelay = 5 * time.Second`
  - 新增 `rateLimitKeywords`：`429`、`rate limit`、`rate_limit`、`too many requests`、`quota exceeded`、`请求过于频繁`、`调用频率`、`限流`
  - `Classify()` 在 `networkKeywords` 之前检查 `rateLimitKeywords`（429 优先于 network，因为 429 错误可能包含 "http" 等网络关键词）
  - `Decide()` 新增 `FailureRateLimit` 分支：指数退避 5s → 10s → 20s，最多 3 次

#### Fix 2: Agent loop 重试逻辑改为循环

- `gui/im_message_handler.go`：AdaptiveRetry 路径从单次重试改为循环重试
  - `for retryAttempt := 0; err != nil && !ctx.IsCancelled(); retryAttempt++`
  - 每次循环调用 `Decide(toolName, category, retryAttempt)`，直到 `Action != "retry"`
  - 重试期间通过 `onProgress` 回调通知前端："⏳ API 请求频率受限，等待 Ns 后重试 (M/3)..."
  - 重试后重新 `Classify` 错误（错误类型可能在重试后变化）
  - Fallback 路径（无 AdaptiveRetry）也支持 429 多次重试：`isRateLimitError` → 3 次指数退避

#### Fix 3: `isRetryableLLMError` 新增 429

- `gui/llm_retry.go`：`isRetryableLLMError()` 新增 `isRateLimitError(err)` 检查，作为 AdaptiveRetry 不可用时的 fallback

#### Fix 4: LLM 错误退出保留 in-flight task marker

- `gui/im_message_handler.go`：
  - 新增 `llmErrorExitFlag` 局部变量
  - defer 中 `ClearInFlightTask` 检查 `!llmErrorExitFlag`
  - 两个 `llmErrorExitResponse` 调用点前设置 `llmErrorExitFlag = true`
  - 效果：LLM 错误后 marker 留在磁盘上，下次消息触发 in-flight recovery

#### Fix 5: 错误消息包含任务上下文

- `gui/im_message_handler.go`：`llmErrorExitResponse` 从 history 中提取最后一条 user 消息作为任务摘要
  - 错误消息末尾追加 `💡 你之前的任务：{摘要}\n发送任意消息即可继续。`

**指数退避策略**：

| 重试次数 | 延迟 | 累计等待 |
|---------|------|---------|
| 第 1 次 | 5s | 5s |
| 第 2 次 | 10s | 15s |
| 第 3 次 | 20s | 35s |
| 放弃 | — | — |

**测试**：12 个新增测试全部通过
- `TestClassify_RateLimit_429`：HTTP 429 → FailureRateLimit
- `TestClassify_RateLimit_TooManyRequests`：Too Many Requests → FailureRateLimit
- `TestClassify_RateLimit_QuotaExceeded`：quota exceeded → FailureRateLimit
- `TestClassify_RateLimit_Chinese`：调用频率超限 → FailureRateLimit
- `TestClassify_RateLimit_PrecedesNetwork`：429 + service unavailable → FailureRateLimit（不是 FailureNetwork）
- `TestClassify_Network_StillWorks`：connection refused → FailureNetwork（不受影响）
- `TestClassify_Network_502`：HTTP 502 → FailureNetwork（不受影响）
- `TestDecide_RateLimit_ExponentialBackoff`：5s → 10s → 20s → skip
- `TestDecide_Network_StillWorks`：1s → 2s → 4s → skip（不受影响）
- `TestIsRetryableLLMError_Includes429`：429 → true
- `TestIsRetryableLLMError_StillMatchesTimeout`：timeout → true（不受影响）
- `TestIsRateLimitError_ZhipuCode1234`：智谱 code:1234 → true

**验收标准**：
- LLM 返回 429 时 → 自动指数退避重试 3 次（5s → 10s → 20s）
- 重试期间前端显示"等待 N 秒后重试"进度提示
- 3 次重试后仍然 429 → 返回错误 + 保留 in-flight marker + 显示任务摘要
- 用户说"继续" → 检测到 in-flight marker → 恢复上下文
- 网络错误（timeout/502/503/504）的重试行为不变
- GUI / corelib 编译通过


### 86. Compaction 丢失工具产出物 + UIC 预检 Layer 限制导致上下文丢失

**来源**：用户在 AI 助手面板中完成 HuggingFace 论文采集（99 篇论文 + 56 条评论，数据保存到 `D:\workprj\hf_agent_papers_complete.json`），然后说"整理成技术综述ｐｄｆ"。maclaw 丢失了上下文，开始问澄清问题（"您手头已经有相关的资料或初稿，还是需要通过调研新写一份？"）。

**根因（两个独立问题叠加）**：

#### 根因 1 (P0): `compactHistory` 的 summarizer 输入只有 turn boundary texts，丢失了工具产出物的关键数据

**触发链路**：
1. 第一轮对话（论文采集）：82 条 entries → compaction → 41 条
2. 第二轮对话（"继续"）：107 条 entries → compaction → 40 条，`compaction_count=2 — quality warning threshold reached`
3. 持久化的对话历史只有 40 条 entries，其中只有 2 条 user 消息

**机制性问题**：`compactHistory` 使用 `extractTurnBoundaryTexts` 提取 summarizer 输入。Turn boundary 只包含"第一条 user 消息"和"第一条 assistant 响应"。但关键数据（文件路径 `D:\workprj\hf_agent_papers_complete.json`、数据统计 "99篇Agent论文"）出现在：
- **工具调用结果**（`role: tool`）——web_fetch 返回的数据、write_file 的确认
- **后续 assistant 消息**（非第一条）——LLM 在多轮工具调用后的总结

这些都不是 turn boundary，被 `extractTurnBoundaryTexts` 排除。summarizer 的输入中根本没有工具产出物的数据，LLM 无法总结不存在的信息。

**对比**：`trimHistoryWithSummary` 使用 `compactionHandoffPrompt`（结构化交接摘要，包含"关键数据"section），且输入包含 assistant/tool 内容。但 `compactHistory` 完全不用这个 prompt，也不包含 tool 内容。两个压缩路径的质量不一致。

**修复**：
- `gui/im_message_handler.go`：`compactHistory` 重写，summarizer 输入从三个数据源构建：
  1. **Turn boundaries**（原有）：用户请求和 LLM 首条响应
  2. **工具产出的关键数据**（新增）：`extractKeyDataFromEntries()` 从 tool/assistant 消息中提取文件路径、URL、数据统计
  3. **最终 assistant 消息**（新增）：`extractFinalAssistantTexts()` 提取每个 turn 的最后一条 assistant 消息（通常包含多轮工具调用后的结论/结果）
- Summarizer prompt 从通用的"请简洁总结"改为 `compactionHandoffPrompt`（结构化 4-section 交接摘要），与 `trimHistoryWithSummary` 统一
- 压缩后的摘要使用 `compactionRecoveryPrefix`（"另一个语言模型已经开始处理此任务..."），与 `trimHistoryWithSummary` 统一

**新增函数**：
- `extractKeyDataFromEntries(entries)`：扫描 tool/assistant 消息，提取文件路径（Windows/Unix）、URL、数据统计（数字+量词+数据关键词），去重后返回最多 30 条
- `extractKeyDataRefsFromText(text)`：从单条文本中提取关键数据引用（模式匹配，非 LLM）
- `containsDataStatistic(line)`：检测数据统计模式（数字+量词+数据关键词）
- `extractFinalAssistantTexts(entries, maxTexts)`：提取每个 turn 的最后一条 assistant 消息（与 turn boundary 的首条互补）

#### 根因 2 (P1): UIC 预检的 `Layer >= 2` 条件导致 L1 关键词匹配的 `document_delivery` 无法被 reject

**日志证据**：
```
08:01:53 [UnifiedIntentClassifier] result: text="整理成技术综述ｐｄｆ" primary=document_delivery conf=0.92 layer=1
08:01:53 [WorkflowInterception] UIC fusion: intent=document_delivery conf=0.92 layer=1 wf= — not decisive, proceeding to IUM
```

UIC L1 关键词匹配到 "ｐｄｆ"（全角）→ `document_delivery`（Strong keyword），conf=0.92，layer=1。预检条件 `uicResult.Layer >= 2` 要求至少 L2（embedding）才能 reject。L1 被跳过，`document_delivery` fall through 到 IUM，IUM 发起 LLM 调用（deepseek-reasoner，10-30s），LLM 返回了"需要更多信息"的回复。

**机制性问题**：`Layer >= 2` 限制的设计意图是防止 L1 误分类导致工作流任务被错误 reject（如 "宣传ppt" 被 L1 误分类为 `non_coding`）。但对 `MayTriggerWorkflow=false` 的意图（如 `document_delivery`），L1 的判定已经足够——无论 L1 是否误判了具体是哪个非工作流意图，结果（不触发工作流）都是正确的。

**修复**：
- `gui/im_message_handler_workflow.go`：UIC 预检改为两层 rejection 策略：
  - **`MayTriggerWorkflow=false` 意图**（document_delivery, search, non_coding 等）：L1 关键词（layer=1）+ conf >= threshold 即可 reject。这些意图不可能触发工作流，L1 误分类风险不影响结果。
  - **`MayTriggerWorkflow=true` 意图**（coding, office, workflow_task）：仍要求 layer >= 2（fusion 确认），防止 L1 误分类导致工作流任务被错误 reject。

**设计原则**：rejection 的安全性取决于"误 reject 的后果"。对 `MayTriggerWorkflow=false` 的意图，误 reject 的后果是"本该进入 IUM 的非工作流消息直接进入 agent loop"——这是正确行为。对 `MayTriggerWorkflow=true` 的意图，误 reject 的后果是"本该触发工作流的任务被当作普通消息处理"——这是严重错误，需要更高的置信度。

**验收标准**：
- "整理成技术综述ｐｄｆ" → UIC L1 返回 `document_delivery`（conf=0.92）→ `MayTriggerWorkflow=false` → 直接 reject → 正常 agent loop → LLM 有完整上下文（含文件路径和数据统计）
- compaction 后的摘要包含"关键数据"section，列出文件路径和数据统计
- "帮我设计一个产品介绍PPT" → UIC L1 返回 `office`（`MayTriggerWorkflow=true`）→ 仍需 layer >= 2 确认
- 所有 trimHistory / TopicDetector / WorkflowCandidate / MayTriggerWorkflow 测试通过
- GUI / corelib 编译通过


### 87. 记忆串台——完成任务后"继续"拉出旧项目 + gui_observe/gui_verify 泄漏到 LLM 工具列表

**来源**：用户完成论文报告任务后说"继续"，maclaw 从记忆中拉出不相关的 OmniRoute 升级任务开始执行。同时 `gui_observe`/`gui_verify` 工具通过 memory-driven pin 泄漏到 LLM 工具列表，导致 `Browser:` 前缀幻觉。

#### 问题 1 (P0): 完成任务后"继续"拉出旧项目

**根因**：#82 的"活跃项目桥接"机制用 system prompt 指令强制 LLM 恢复记忆中的项目（"请基于召回的项目记忆继续推进"）。这是 prompt 层的 workaround——它不让 LLM 自己判断用户意图，而是用指令覆盖 LLM 的判断。当对话历史非空时（刚完成论文任务），"继续"被指令强制解读为"恢复记忆中的旧项目"。

**根因修复**：删除活跃项目桥接机制。proactive recall 已经把相关记忆注入了 system prompt 的"相关记忆"section，LLM 自己能根据上下文判断用户意图。不需要额外的指令强制行为。

应用重启后的任务恢复由 #55 的 in-flight task marker 机制覆盖（进程被杀 → marker 留在磁盘 → 重启后检测 → 显示恢复 UI）。

- `gui/im_system_prompt.go`：删除 `appendProactiveRecall` 中的整个"活跃项目上下文"代码块，恢复函数签名（移除 `isFirstTurn` 参数）

#### 问题 2 (P1): gui_observe/gui_verify 通过 memory-driven pin 泄漏

**根因**：`desktopGUIKeywords` 包含 `"gui"`、`"window"`、`"desktop"` 等短泛英文词，`containsAnyKeyword` 用 `strings.Contains` 纯子串匹配。召回的记忆内容包含 AI 研究术语（如 "GUI agent, web agent, browser agent"），`"gui"` 子串匹配触发 desktop GUI 规则。关键词匹配本质上是在猜"用户可能需要什么工具"，猜错了就是误激活。

**根因修复**：不猜——彻底删除 desktop GUI 的条件关键词规则，改为 `discover_tool` 按需发现。

- `corelib/tool/router.go`：从 `conditionalKeepRules` 中删除 desktop GUI 规则，删除 `desktopGUIKeywords` 列表。`gui_observe`/`gui_verify` 不再是条件工具，不参与关键词匹配、不参与 memory-driven pin、不参与 eager pin
- `gui/tool_deferred.go`：`gui_observe`、`gui_verify`、`gui_record_start`、`gui_record_stop` 加入 `DeferredToolNames`，通过 `discover_tool` 按需发现

**激活路径变更**：
- 修复前：关键词匹配（`"gui"` 子串命中）→ 提前加入工具列表 → 误匹配导致泄漏
- 修复后：LLM 需要时调用 `discover_tool(need="观测桌面窗口")` → 发现 GUI 工具 → session pin → 后续可用

**防御纵深**（保留）：
- `gui/im_system_prompt.go`：memory-driven pin 路径额外检查 `noEagerPinTools`
- `corelib/tool/router.go`：`Route()` self-healing 清理 `sessionTools` 中的 `noEagerPinTools` 工具

**测试**：更新 3 个测试 + 新增 1 个测试验证 GUI 工具不在条件工具系统中

**验收标准**：
- 完成任务 A 后说"继续" → 不拉出记忆中的旧项目 B（对话历史非空，桥接不触发）
- 应用重启后说"继续" → 正常恢复记忆中的项目（对话历史为空，桥接正常触发）
- 记忆中包含 "GUI agent" 等研究术语 → gui_observe/gui_verify 不被 pin 到 session
- 即使 gui_observe 被错误 pin → 下一次 Route() 自动清理
- 所有 MatchConditionalTools / Route / ProactiveMemory 测试通过
- GUI / corelib 编译通过


### 88. write_file 截断死循环——从提示层升级到工具执行层干预

**来源**：用户让 maclaw 搜索热点论文并生成综述 PDF。maclaw 搜索完论文后尝试用 `write_file` 一次性写入完整的综述 Markdown 文件（~15K bytes），但智谱编程模型的 `max_output_tokens=4096` 导致 tool call 参数被截断。Agent loop 陷入 17 分钟的死循环后被 hard cap 强制终止。

**根因（机制性分析）**：

截断恢复机制（#83）有两个阶段：
- **Phase 1（提示层）**：前 3 次截断注入系统消息"请将内容拆分为多次写入"→ `continue`
- **Phase 2（耗尽后）**：`TruncationRetries >= maxTruncationRetries` → 提示被跳过 → 进入 no-tool 分支

Phase 1 的问题：提示是**建议**，模型可以（且确实）忽略。智谱 glm 模型完全无视"请拆分"的提示，每次仍然尝试一次性写入 15K bytes。

Phase 2 的问题：重试耗尽后，截断的 tool call 被 `filterTruncatedToolCalls` 移除，`finishReason` 改为 `"stop"`，进入 no-tool 分支。但 no-tool 分支的各种检查（empty response → recover → continue, promiseOnlyDeliverable → recover → continue）导致 loop 继续。LLM 在下一轮又尝试 `write_file` → 又被截断 → 又进入 no-tool → 又 continue。

**死循环的精确路径**（从日志确认）：
1. LLM 调用 `write_file(content=15K bytes)` → `finish_reason=length` → `filterTruncatedToolCalls` 移除 → `finishReason="stop"` → no-tool 分支
2. `content_len=0`（空内容）→ `emptyVisibleResult=true` → `ConsecutiveEmptyResponses++` → recover prompt → `continue`
3. LLM 输出短文本"好的，我来创建详细的综述Markdown文件" + 再次调用 `write_file(15K)` → 截断 → no-tool
4. `content_len=74`（短文本）→ `emptyVisibleResult=false` → `ConsecutiveEmptyResponses` 重置为 0 → `promiseOnlyDeliverable` 可能触发 → recover → `continue`
5. 重复步骤 1-4，`content_len=0` 和 `content_len=74` 交替出现，重置各种计数器，防止任何单一 hard cap 快速触发
6. 最终 `ConsecutiveNoTool > 5` 触发 hard cap，但已浪费 ~12 分钟（8 次截断 × ~90s/次）

**核心矛盾**：提示层干预（"请拆分"）是**可忽略的建议**。模型忽略建议后，系统没有**不可忽略的**后续手段。

**修复（机制性——从提示层升级到工具执行层干预）**：

当 `maxTruncationRetries` 耗尽后，不再注入提示，而是**从 LLM 的工具列表中移除被截断的工具**。这是工具执行层的干预——LLM 物理上无法调用被移除的工具，必须使用替代方案（bash + heredoc/Python 脚本）。

#### 1. `agentLoopPhase` 新增 `TruncationBlockedTools` 字段

- `gui/im_message_handler.go`：`agentLoopPhase` 新增 `TruncationBlockedTools map[string]bool`
- 记录被临时移除的工具名集合

#### 2. 截断恢复改为两阶段

- **Phase 1（提示层，不变）**：前 3 次截断注入"请拆分"提示 → `continue`
- **Phase 2（工具执行层，新增）**：第 4 次截断时：
  - 将截断的工具名加入 `TruncationBlockedTools`
  - 从 `tools` 切片中过滤掉被阻止的工具定义
  - 注入强制系统消息，告知 LLM 工具已被禁用，并提供具体的替代方案（bash + Python pathlib / heredoc）
  - `continue` 让 LLM 在下一轮使用替代方案

#### 3. 替代方案指导（按工具类型）

- `write_file` 被阻止 → 指导使用 `bash(command="python3 -c \"import pathlib; pathlib.Path('output.md').write_text(content, encoding='utf-8')\"")` 或 `bash(command="cat > output.md << 'MACLAW_EOF'\n内容\nMACLAW_EOF")`
- `edit_file` 被阻止 → 指导使用 `bash + sed` 或 Python 脚本
- 其他工具 → 通用提示

#### 4. Finalize 路径更新

- 当 `TruncationBlockedTools` 非空时，finalize 追加用户可见的说明（"工具 X 因参数过长被反复截断，已自动切换到替代方式"）

**为什么是机制性修复而非 workaround**：
- **不可忽略**：从工具列表中移除工具是物理操作，LLM 无法调用不存在的工具。与提示层的"请拆分"不同，这不是建议。
- **通用**：对所有工具通用（write_file、edit_file、任何未来的大参数工具），不硬编码工具名到检测逻辑中
- **自动降级**：不需要用户干预，系统自动从"直接写入"降级到"bash 脚本写入"
- **消除死循环**：工具被移除后，LLM 不会再尝试调用它，`content_len=0` / `content_len=74` 的交替模式被打破

**日志对比**：

修复前（17 分钟死循环）：
```
06:21:49 truncated tool call recovery (retry 1/3, tools=write_file)
06:23:22 truncated tool call recovery (retry 2/3, tools=write_file)
06:24:53 truncated tool call recovery (retry 3/3, tools=write_file)
06:26:36 [LLM Stream] truncated tool call: write_file args=14975 bytes  ← 重试耗尽，无干预
06:28:05 [LLM Stream] truncated tool call: write_file args=4498 bytes   ← 继续截断
06:29:26 [LLM Stream] truncated tool call: write_file args=14415 bytes
06:31:01 [LLM Stream] truncated tool call: write_file args=14979 bytes
06:32:21 [LLM Stream] truncated tool call: write_file args=15345 bytes
06:33:43 [LLM Stream] truncated tool call: write_file args=14959 bytes
06:35:34 [LLM Stream] truncated tool call: write_file args=15597 bytes
06:37:00 [LLM Stream] truncated tool call: write_file args=15362 bytes
06:37:00 hard cap: 8 consecutive no-tool iterations  ← 17 分钟后才终止
```

修复后（预期行为）：
```
06:21:49 truncated tool call recovery (retry 1/3, tools=write_file)
06:23:22 truncated tool call recovery (retry 2/3, tools=write_file)
06:24:53 truncated tool call recovery (retry 3/3, tools=write_file)
06:26:36 truncation retries exhausted: blocking tools [write_file] from LLM tool list (remaining=N)
06:26:36 [系统提示] write_file 已被临时禁用，请改用 bash + Python 脚本写入
06:28:xx LLM 调用 bash(command="python3 -c ...") → 文件写入成功  ← 立即切换到替代方案
```

**验收标准**：
- `write_file` 连续 3 次截断后 → 工具从 LLM 工具列表中移除
- LLM 在下一轮使用 bash + Python 脚本写入文件
- 不再出现 17 分钟的死循环
- 所有 CodingGate / DriftDetector / GateIntent / RouteTools 测试通过
- GUI 编译通过


#### Review/Fix/Optimize（#88 补充修复）

初步实现后进行代码 review，发现 4 个问题：

| # | 类型 | 问题 | 修复 |
|---|------|------|------|
| 1 | Bug | `tools = baseTools` 重置撤销截断阻止 | skill_failed 恢复路径新增 `TruncationBlockedTools` 重过滤 |
| 2 | Bug | 被阻止的工具仍可通过 LLM 幻觉调用执行 | 两处 `executeTool` 调用前新增 `TruncationBlockedTools` 执行守卫 |
| 3 | Bug | `newlyBlocked` 为空时（工具已阻止但 LLM 再次幻觉调用）代码 fall through 到死循环 | 空 `newlyBlocked` 时直接 `continue`（工具已阻止，提示已在对话中） |
| 4 | Optimize | Python 示例中过度转义（`\\\"\\n`）LLM 难以复现 | 简化转义，使用更自然的多行格式 |

##### Fix 1: `tools = baseTools` 重置撤销截断阻止

**根因**：`RecoverReason == "skill_failed"` 时 `tools = baseTools` 重置工具列表，后续只重新应用 coding gate 和 direct-mode 过滤，不重新应用 `TruncationBlockedTools` 过滤。被阻止的 `write_file` 重新出现在工具列表中。

**修复**：在 `toolsTokenBudget = estimateToolsTokens(tools)` 之后新增 `TruncationBlockedTools` 重过滤。

##### Fix 2: 执行层守卫

**根因**：`executeTool` 通过 registry 分发，不检查 `TruncationBlockedTools`。LLM 幻觉调用被阻止的工具时（工具不在定义列表但模型仍生成调用），工具会被正常执行。

**修复**：两处 `executeTool` 调用点（主循环 + bonus round）前新增 `phase.TruncationBlockedTools[tc.Function.Name]` 检查，命中时返回 `[系统拒绝]` 错误消息。

##### Fix 3: 已阻止工具的重复截断

**根因**：工具已在 `TruncationBlockedTools` 中，LLM 幻觉调用后被 `filterTruncatedToolCalls` 检测为截断。`newlyBlocked` 为空（已在集合中），`if len(newlyBlocked) > 0` 跳过，代码 fall through 到 length continuation / NeedsConfirm / hard cap 等检查，可能导致 `continue` 或 finalize 的非预期行为。

**修复**：`newlyBlocked` 为空时直接 `continue`，附带日志。

##### Fix 4: 简化 Python 示例转义

**修复**：`write_file` 替代方案中的 Python pathlib 示例从 `\\\"\\nimport pathlib\\n` 简化为 `\\\"\nimport pathlib\n`，减少转义层数。

**修改文件**：`gui/im_message_handler.go`（4 处修改）

**验收标准**：
- skill_failed 恢复后 `write_file` 仍被阻止（不重新出现在工具列表中）
- LLM 幻觉调用被阻止的工具 → 返回 `[系统拒绝]` 错误，不执行
- 已阻止工具的重复截断 → 直接 continue，不 fall through
- 所有 CodingGate / DriftDetector / GateIntent / RouteTools / Frequency 测试通过
- GUI 编译通过


### 89. Compaction 质量 + Context 膨胀 + online_extractor JSON 解析失败

**来源**：用户截图 + `maclaw.log` 分析（2026-04-30）。HuggingFace 论文采集任务中 agent loop 跑了 35 轮迭代（5m42s），每次结束后 compaction 触发（entries 44→40/41），第 2 次就触发 quality warning。用户后续消息时 LLM 丢失工具产出物上下文。同时 `online_extractor` 报 `json: cannot unmarshal array into Go struct field ExtractedFact.entities of type string`。

#### 问题 1 (P0): compactHistory token 阈值过低——从固定阈值到动态阈值

**根因**：`MaxMemoryTokenEstimate=60000` 是固定值。35 轮工具调用的对话轻松超过 60K token，每次 agent loop 结束后都触发 compaction。

**修复**：
- `gui/im_message_handler.go`：`compactHistory` 的触发阈值从固定 `MaxMemoryTokenEstimate` 改为动态计算——模型 `EffectiveContextTokens()` 的 50%，clamped 到 [60K, 100K]。128K context 模型阈值从 60K 提升到 ~64K。

#### 问题 2 (P1): compactHistory split 点改为信息密度感知

**根因**：`split = len(entries) / 2` 固定 50% 分割，不考虑信息密度。前半部分可能包含关键搜索结果和文件路径。

**修复**：
- `gui/im_message_handler.go`：split 点从固定 50% 改为"从后往前扫描，保留尽可能多的最近 entries 使 token 在阈值 70% 以内"。对 35 轮迭代的对话，可能只压缩前 10 轮而非前 17 轮。

#### 问题 3 (P1): summarizer 输入新增工具操作摘要 section

**根因**：`extractKeyDataFromEntries` 只提取文件路径/URL/数据统计，缺少工具调用的操作语义（"搜索了 HuggingFace"、"抓取了 5 个页面"）。

**修复**：
- `gui/im_message_handler.go`：新增 `extractToolOperationSummary()` 函数，从 assistant entries 的 ToolCalls 中提取工具名 + 关键参数（如 `web_fetch: https://huggingface.co/papers`、`write_file: D:\workprj\hf_papers.json`）
- 新增 `extractKeyToolArg()` 函数：按工具名提取最有意义的参数（web_fetch→url, write_file→path, bash→command 等）
- summarizer 输入从 3 个 section 增加到 4 个：对话轮次 + 关键数据 + 工具操作 + 任务结果

#### 问题 4 (P1): ExtractedFact.Entities JSON 反序列化——容忍嵌套数组

**根因**：LLM 返回嵌套数组 `[["entity:X", "relation:Y", "entity:Z"]]`，Go 的 `[]string` 无法反序列化。

**修复**：
- `corelib/memory/types.go`：`ExtractedFact.Entities` 从 `[]string` 改为 `json.RawMessage`（字段重命名为 `RawEntities`）
- 新增 `ParsedEntities()` 方法：依次尝试扁平数组、嵌套数组、单字符串三种格式
- `corelib/memory/online_extractor.go`：所有 `fact.Entities` 改为 `fact.ParsedEntities()`（9 处）
- 7 个新增测试覆盖所有格式 + JSON round-trip

#### 问题 5 (P2): compaction 后 assistant 确认包含关键数据引用

**修复**：
- `gui/im_message_handler.go`：assistant 确认从固定 "好的，我已了解之前的对话上下文。" 改为包含 summary 中提取的前 3 条关键数据引用

**验收标准**：
- 35 轮工具调用的对话在 128K context 模型上不触发 compaction（阈值 64K > 55K 实际 token）
- compaction 触发时保留尽可能多的最近 entries
- summarizer 输入包含工具操作摘要
- `online_extractor` 不再因 entities 嵌套数组报 JSON 解析错误
- 所有 memory 包测试通过（含 7 个新增 ParsedEntities 测试）
- 所有 gui trimHistory / CodingGate / GateIntent / RouteTools 测试通过


#### Review/Fix: 三个机制性问题修复

##### Review 问题 1 (P0): 两条压缩路径阈值不一致——compactHistory 提高了阈值但 trimHistoryWithSummary 没有

**根因**：对话历史有两条独立压缩路径：`compactHistory`（消息处理前，token 阈值触发）和 `trimHistoryWithSummary`（消息处理后，entry 数阈值触发）。日志中的 `entries=44->40` 是 `trimHistoryWithSummary` 触发的（`MaxConversationTurns=40`），不是 `compactHistory`。提高 `compactHistory` 的 token 阈值对实际问题没有效果。

**修复**：
- `gui/im_conversation_trim.go`：`trimHistoryWithSummary` 新增 `maxEntries int` 参数，`> 0` 时覆盖 `MaxConversationTurns`。`trimHistory`（wrapper）传 0 保持默认行为。函数内部所有 `agent.MaxConversationTurns` 引用改为 `limit` 局部变量。
- `gui/im_message_handler.go`：`saveConversationHistoryTimed` 计算动态 entry limit——`EffectiveContextTokens / 1500`（每条 entry 平均 ~1500 token），clamped 到 [40, 80]。128K context 模型的 limit 从 40 提升到 ~68。传给 `trimHistoryWithSummary`。

**效果**：128K context 模型上，35 轮迭代产出 44 条 entries 不再触发 trimHistoryWithSummary（limit=68 > 44）。

##### Review 问题 2 (P1): density-aware split 的巨大 entry 边界情况

**分析**：单条 entry token > `recentBudget` 时，split 被推到 `len(entries)`，group-align 后返回原始 entries。这不是新引入的 bug（原来的 `len/2` 也有同样的 group-align 边界），且实际场景中单条 entry 不太可能超过 ~45K token（recentBudget）。保留现有行为，不额外处理。

##### Review 问题 3 (P1): extractToolOperationSummary dedup 粒度过细

**根因**：用完整的 `name + keyArg` 做 dedup，20 次 `web_fetch`（每次不同 URL）产出 15 条 op，挤掉了 `write_file`、`generate_pdf` 等工具的 op。

**修复**：
- `gui/im_message_handler.go`：`extractToolOperationSummary` 改为两遍扫描——先统计每个工具的调用频率，再按工具名限制每个工具最多 2 条示例。高频工具（>2 次）在最后一条示例后追加 `(共N次)` 计数。确保不同工具都有机会出现在 summarizer 输入中。


#### Review/Fix Round 3: 两条压缩路径预算不一致导致双重压缩

**根因**：`trimHistoryWithSummary` 的动态 limit = `ect/1500` entries（128K 模型 → 68 entries → ~102K tokens），但 `compactHistory` 的 threshold 仍然是 60K tokens。`trimHistoryWithSummary` 产出的 68 entries（~102K tokens）在下一条消息时被 `compactHistory` 检测到超过 60K → 触发 LLM 摘要压缩。用户的对话历史被压缩两次：一次 entry 数截断，一次 token 数摘要。

**修复**：`compactHistory` 的 threshold 改为 `max(60K, ect)`，与 `trimHistoryWithSummary` 的 entry limit 的 token 等价值一致。128K 模型：threshold = 102K，不会对 `trimHistoryWithSummary` 的产出触发二次压缩。agent loop 内部的 `trimConversation` 负责实际的 context window 管理。


#### Review/Fix Round 4: 两条压缩路径合并为一条

**根因**：`compactHistory`（pre-loop，token 阈值）和 `trimHistoryWithSummary`（post-loop，entry 数阈值）是两条独立的压缩路径，操作同一个 `history` 数据。两条路径的预算不一致导致双重压缩。更根本的问题是：两条路径没有必要同时存在。

**分析**：
- `compactHistory` 的优势：结构化 4-section LLM 摘要输入（turn boundaries + key data + tool ops + final assistant）
- `trimHistoryWithSummary` 的优势：三层截断（tier-1 + tier-user + recent）+ memorySink 沉淀到长期记忆
- `trimConversation`（agent loop 内部）：负责 LLM context window 管理，与 history 压缩无关

**修复**：删除 `compactHistory`，将其结构化摘要输入合并到 `trimHistoryWithSummary`。

具体改动：
- `gui/im_message_handler.go`：删除 `compactHistory` 函数（~160 行）；删除两处 `h.compactHistory(history, httpClient)` 调用
- `gui/im_conversation_trim.go`：
  - `trimHistoryWithSummary` 新增 `maxTokens int` 参数，同时检查 entry 数和 token 数
  - separator 构建从弱输入（`[role] text` 行 dump）改为结构化 4-section 输入（`buildCompactionSummarizerInput`）
  - 新增 `buildCompactionSummarizerInput()` 函数：从 `compactHistory` 提取的结构化摘要输入构建逻辑
  - `trimHistory` wrapper 传 `maxTokens=0`
- `gui/im_message_handler.go`：`saveConversationHistoryTimed` 计算 `dynamicTokenLimit = dynamicLimit * 1500`，传给 `trimHistoryWithSummary`

**合并后的单一压缩路径**：
- 触发条件：`len(entries) > dynamicLimit` OR `tokens > dynamicTokenLimit`
- 压缩策略：三层截断（tier-1 + tier-user + recent）+ 结构化 4-section LLM 摘要 separator + memorySink
- 预算一致性：`dynamicLimit * 1500 == dynamicTokenLimit`，entry 和 token 阈值等价

**效果**：
- 消除双重压缩——只有一条路径，不可能触发两次
- 摘要质量提升——`trimHistoryWithSummary` 的 separator 从弱输入升级为结构化 4-section 输入
- 代码简化——删除 ~160 行 `compactHistory` 函数


### 89.1 记忆管理体系架构审计——6 个问题修复

**来源**：对记忆管理体系的系统性审计，检查所有写入路径、召回路径、压缩路径的一致性。

#### 问题 5 (P1): RecallDynamic 名额浪费——过滤逻辑分散在两层

**根因**：`RecallDynamic` 只硬编码过滤 `user_fact`，不过滤 `self_identity`/`session_checkpoint`/`conversation_summary`。`appendProactiveRecall` 在 `RecallDynamic` 之后再过滤这些类别。结果：这些类别占据 `RecallDynamic` 的 15 条名额后被丢弃，真正有用的 `project_knowledge`/`task_artifact` 被挤出。

**修复**：
- `corelib/memory/store.go`：`RecallDynamic` 在 `category==""` 时（通用召回）统一过滤 `user_fact`/`self_identity`/`session_checkpoint`/`conversation_summary`。当 `category != ""`（指定类别）时不过滤，保持 `toolMemory` recall 的灵活性。
- `gui/im_system_prompt.go`：`appendProactiveRecall` 删除冗余的二次过滤。

#### 问题 1 (P1): 写入路径 OwnerID 不一致——5 个 Save 调用不设 OwnerID

**根因**：#67 引入 OwnerID 但只修复了 archiver/knowledge_extractor/online_extractor/consolidator。其余 GUI 侧写入路径（sedimentTaskEntry、memorySink、toolMemory、workflow_artifact_saver）不设 OwnerID。maclawsrv 多租户场景下这些记忆对所有用户可见。

**修复**：
- `gui/im_task_sediment.go`：`sedimentTaskEntry` 的 entry 设置 `OwnerID: userID`
- `gui/im_message_handler.go`：`memorySink` 闭包的 entry 设置 `OwnerID: userID`
- `gui/im_tools_misc.go`：`toolMemory` save 的 entry 设置 `OwnerID: h.lastUserID`
- `gui/workflow_artifact_saver.go`：新增 `SaveArtifactForUser` 方法接受 `ownerID` 参数；`deferredArtifactSaver` 新增 `getUserID` 回调
- `gui/app_workflow_init.go`：`deferredArtifactSaver` 的 `getUserID` 从 `hubClient.ensureIMHandler().lastUserID` 读取

#### 附带修复：pre-existing 编译错误

- `gui/im_message_handler.go`：`maybeAttachVoiceSummary(resp, platform)` 中 `platform` 未定义 → 改为 `msg.Platform`

#### 审计发现的 P2 问题（记录但不修复）

- **问题 2**：`autoCompressConversation` 在有 tool_calls 时完全跳过——agent loop 中几乎每次迭代都有 tool_calls，实际上是死代码
- **问题 3**：`corelib/agent.TrimHistory` 在非测试代码中无调用者——死代码
- **问题 4**：`corelib/agent.TrimConversation` 与 `gui/trimConversation` 重复实现
- **问题 6**：`gui/im_compress.go` 和 `corelib/agent/compress.go` 的 7 个转换函数完全重复

这些是 agent-unification 迁移的遗留，属于代码清理任务，不影响功能。


### 90. 独立任务（论文摘要、翻译等）不出现在"最近任务"列表中

**来源**：用户截图——完成"找篇新的hugging face上的agent相关论文，做成中文详细综述，发我pdf版本"任务后，左侧"最近任务"列表中没有该任务。

**根因**：`sedimentTaskEntry` 无条件使用 `getCurrentProjectPath()` 作为 `ProjectIndex` 的项目路径标签。`ProjectIndex` 按项目路径聚合所有 entries 为一条 `ProjectRecord`。所有任务（论文摘要、翻译、编码、SSH）共享同一个项目路径 → 合并为一条记录 → 只有最新任务的标题可见。独立任务被"淹没"在项目记录中。

**机制性分析**：

`ProjectIndex.inferProjectPath` 从 entry 的 Tags 中找第一个通过 `looksLikeProjectPath` 的值作为索引 key。原代码只添加一个 tag（`getCurrentProjectPath()`），所有任务共享同一个 key → 合并为一条 `ProjectRecord`。

**Review 过程中废弃的两个方案**：

1. **输出文件路径方案**（初版）：从工具调用参数中提取 `write_file`/`generate_pdf` 的输出路径，根据路径位置判断项目归属。废弃原因：`generate_pdf` 没有 `output` 参数（PDF 路径由内部自动生成）；LLM 可能将独立任务输出写到项目目录（如 `D:\workprj\aicoder\translation.md`）；输出文件位置是 LLM 的自由决策，不是可靠信号。

2. **活跃工作流方案**（二版）：有活跃工作流 → 项目任务；无活跃工作流 → 独立任务。废弃原因：`WorkflowState.ProjectPath` 在 `StartWorkflow` 中从未被设置（始终为空），工作流 artifact 的 tags 也不包含项目路径；用户说"直接做"跳过工作流时，编码任务也没有活跃工作流，会被错误分类为独立任务。

**根因修复**：每个任务始终创建独立的 standalone 路径作为索引 key。

- `resolveTaskProjectPath` 返回两个值：`(standalonePath, projectTag)`
- `standalonePath`：`{maclawDataDir}/tasks/{sha256(title)[:12]}`，作为 Tags 的第一个路径 tag → `inferProjectPath` 用它做索引 key → 每个任务有自己的 `ProjectRecord`
- `projectTag`：`getCurrentProjectPath()`，作为 Tags 的第二个路径 tag → 不影响索引 key（`inferProjectPath` 取第一个），但保留在 tags 中用于搜索亲和度

**为什么"始终独立"是正确的机制**：

| 场景 | 原代码 | 活跃工作流方案 | 始终独立方案 |
|------|--------|--------------|------------|
| 论文摘要 | ❌ 合并到项目 | ✅ 独立 | ✅ 独立 |
| SSH 操作 | ❌ 合并到项目 | ✅ 独立 | ✅ 独立 |
| 编码（有工作流）| ✅ 合并到项目 | ✅ 合并到项目 | ✅ 独立（工作流 artifact 另有项目记录）|
| 编码（无工作流）| ✅ 合并到项目 | ❌ 独立（回归）| ✅ 独立 |
| 翻译写到项目目录 | ❌ 合并到项目 | ❌ 合并到项目 | ✅ 独立 |

"始终独立"是唯一在所有场景下都正确的方案。编码任务（有工作流）的项目记录由 `workflow_artifact_saver` 创建，不依赖 sediment entry。编码任务（无工作流）作为独立项出现在列表中，用户可以看到每个任务——这比合并到一个不可区分的项目记录更好。

**修改文件**：
- `gui/im_task_sediment.go`：`resolveTaskProjectPath` 返回 `(standalone, projectTag)` 两个值；`sedimentTaskEntry` 将两个路径都加入 tags（standalone 在前，projectTag 在后）
- `gui/im_task_sediment_test.go`：5 个测试

**验收标准**：
- 论文摘要任务完成后 → 出现在"最近任务"列表中
- SSH 操作完成后 → 出现在"最近任务"列表中
- 编码任务（有/无工作流）→ 出现在"最近任务"列表中
- 不同任务 → 各自有独立的列表项
- 相同任务重复执行 → 更新同一条记录（不创建重复项）
- 5 个测试 + 所有现有 gui 测试通过
- GUI 编译通过


### 91. MaClawDataSrv 结构化数据服务模块 Review 修复

**来源**：模块 review 查缺补漏。

#### P1: 写操作使用 RLock（7 处并发安全修复）

**根因**：`Service` 的 7 个写操作方法错误使用 `RLock`（读锁），在并发场景下可能导致数据竞态。

**修复**：
- `service.go`：`CreateDataset`、`UpdateDataset`、`DeleteDataset`、`UpsertFields`、`CreateBackup` 从 `RLock` 改为 `Lock`
- `schema_proposals.go`：`ProposeSchema` 从 `RLock` 改为 `Lock`
- `quality_checks.go`：`RunQualityCheck` 从 `RLock` 改为 `Lock`

#### P1: 硬编码 dataset 验证改为注册表模式

**根因**：`datasetDataErrors` 使用 switch 硬编码 `"finance.vouchers"` 的特殊验证逻辑，新增 dataset 验证需要改代码。

**修复**：
- `validation.go`：从 switch 改为 `datasetValidationRules` map 注册表，新增验证只需往 map 里加一条

#### P2: Store 接口拆分为 6 个子接口

**根因**：`Store` 接口有 83 个方法，违反接口隔离原则，替换存储引擎成本极高。

**修复**：
- `service.go`：拆分为 `DatasetStore`（11）、`RecordStore`（12）、`EventStore`（7）、`ConnectorStore`（4）、`GovernanceStore`（15）、`AdminStore`（34）
- `Store` 保留为组合接口（向后兼容），`SQLiteStore` 仍实现完整 `Store`
- `architecture_test.go`：`allowedImplementationTypes` 新增 6 个子接口名

#### P2: 批量操作 context 取消检查

**根因**：`BulkUpdateRecords`（最多 500 条）、`BulkDeleteRecords`、`BatchImportRecords`（最多 1000 条）在 context 取消后仍继续执行。

**修复**：
- `service.go`：三个批量操作的 commit 循环顶部新增 `ctx.Err()` 检查，取消后立即返回

#### P3: HTTP Rate Limiting

**根因**：`/api/v1/login` 和 `/api/v1/setup/admin` 无请求频率限制，可被暴力破解。

**修复**：
- `http.go`：新增 `httpRateLimiter`（per-IP token-bucket，burst=10，sustained=2 req/s）
- `withRateLimit` 中间件应用于 login 和 setup/admin 端点
- 定期 GC（每 100 次调用或 bucket 数超 1000 时清理 5 分钟无活动的 IP）
- 支持 `X-Forwarded-For` / `X-Real-IP` 代理头

**验收标准**：
- 所有 `datasrv/structureddata` 测试通过（含架构测试）
- 所有 `corelib/structureddata` 测试通过
- 编译无错误


### 91.1 MaClawDataSrv 结构化数据服务模块 Review 补充修复

**来源**：模块 review 第二轮——GUI 侧 `mis_data` 工具接口层。

#### P1: `callMISDataAPIBytes` 每次请求创建新 `http.Client`——阻止 TCP 连接复用

**根因**：`callMISDataAPIBytes`、`callMISDataDownloadSummary`、`TestMISDataConnection` 三个函数各自创建 `&http.Client{Timeout: N * time.Second}`。每次 MIS data 工具调用都创建新客户端，阻止 TCP keep-alive 连接复用，浪费 TLS 握手开销。`mis_data` 工具在单个 agent loop 迭代中可能被调用 10+ 次（resolve_intent → get_business_action → execute_business_action 等链式调用）。

**修复**：
- `gui/mis_data_tool.go`：新增 `misDataHTTPClient` 包级共享客户端
  - `Transport` 配置：`MaxIdleConns=10`、`MaxIdleConnsPerHost=5`、`IdleConnTimeout=90s`
  - 不设 client-level `Timeout`——per-request 超时通过 `context.WithTimeout` 控制
- `gui/mis_data_tool.go`：`callMISDataAPIBytes` 改为使用 `misDataHTTPClient` + `context.WithTimeout(30s)`
- `gui/mis_data_tool.go`：`callMISDataDownloadSummary` 改为使用 `misDataHTTPClient` + `context.WithTimeout(60s)`
- `gui/app_mis_data.go`：`TestMISDataConnection` 改为使用 `misDataHTTPClient` + `context.WithTimeout(10s)`

**设计原则**：
- 单一 HTTP 客户端：所有 MIS data service 调用共享同一个 `http.Client`，TCP 连接自动复用
- Per-request 超时：通过 `context.WithTimeout` 控制每个请求的超时（API 调用 30s，下载 60s，连接测试 10s）
- 连接池配置：`MaxIdleConnsPerHost=5` 适配 MIS 服务单节点部署场景

#### P2: `cloneMISInterfaceValue` 函数末尾多余的 `}`——语法错误

**根因**：`cloneMISInterfaceValue` 函数体末尾有一个多余的 `}`，导致后续函数声明被解析为"非声明语句在函数体外"。这是一个 pre-existing 的语法错误，之前可能因为其他编译错误掩盖了它。

**修复**：删除多余的 `}`。

#### P2: `mcp_auto_discovery.go` 未使用的 `"strings"` import

**根因**：`import "strings"` 声明但未使用，`go vet` 报错。Pre-existing 问题。

**修复**：移除未使用的 import。

#### P2: `mis_data_tool_action.go` action 常量化 + `normalizeMISDataToolAction` 简化

**根因**：`executeMISDataTool` 的 ~100 个 switch case 中，只有 5 个使用了常量（`misDataToolActionGetInbox` 等），其余全是字符串字面量。`normalizeMISDataToolAction` 函数有一个冗余的 switch——它将输入 normalize 后检查是否匹配已知常量，不匹配则返回 normalized 字符串。由于 `misDataToolAction` 是 `string` 的类型别名，switch 完全多余。

**修复**：
- 新增 40+ 个高频 action 常量（status/get_capabilities/list_domains/...）
- `normalizeMISDataToolAction` 简化为单行 `return misDataToolAction(strings.ToLower(strings.TrimSpace(action)))`

**验收标准**：
- `go vet ./gui/...` 通过
- `go test ./datasrv/structureddata/ -short` 通过
- `go test ./corelib/structureddata/ -short` 通过


### 92. UIC "快速 accept" 越权直接启动工作流——删除 UIC 的工作流启动权力

**来源**：用户在桌面 AI 助手面板说"帮我测试 ag ui功能，生成一个用户信息输入界面"，系统误触发了 coding 工作流。用户之前三次表达同样意图时，IUM 都正确拒绝了。

**根因**：`handleNeedsUnderstanding` 中 UIC 的 `isConcreteWorkflowType` 路径赋予了 UIC "快速 accept"的权力——当 tree channel 返回 `workflow_type="coding"` 时，直接调用 `confirmWorkflowStart` + `StartWorkflow`，完全跳过 IUM 的深度语义确认。

UIC 是单轮 LLM 调用，只看用户消息文本，不看对话历史、不知道 maclaw 自身功能。它把"帮我测试 ag ui功能，生成一个用户信息输入界面"中的"生成"+"界面"理解为编码任务（conf=0.87），但实际上用户想测试 maclaw 的 GUI 自动化功能。

IUM 有更丰富的上下文（system prompt 中包含 maclaw 功能描述、工作流模板描述），之前三次都正确拒绝了同样的意图。但 UIC 的 `isConcreteWorkflowType` 路径绕过了 IUM。

**机制性修复原则**：

> UIC 只有"快速 reject"的权力（排除明确不是工作流的消息），没有"快速 accept"的权力（确认是工作流并直接启动）。"accept"的权力属于 IUM。

**修复**：
- `gui/im_message_handler_workflow.go`：`handleNeedsUnderstanding` 中删除整个 `isConcreteWorkflowType` 直接启动路径（`confirmWorkflowStart` + `StartWorkflow`）
- UIC 的 `WorkflowType` 推断结果仅作为日志 hint 记录，不触发任何工作流启动动作
- 所有工作流启动必须经过 IUM 的深度语义确认（`understanding.Start()`）
- UIC 的"快速 reject"路径（`isNonWorkflowIntent && conf >= threshold`）保持不变

**权力分配（修复后）**：
```
UIC (UnifiedIntentClassifier)
  ├── 快速 reject 权 ✅：MayTriggerWorkflow=false + conf >= threshold → 直接 reject
  └── 快速 accept 权 ❌：已删除。WorkflowType 仅作为 hint 记录到日志

IUM (IntentUnderstandingManager)
  └── 深度确认权 ✅：所有工作流启动必须经过 IUM 确认
```

**延迟优化（first-round ready）**：

为避免 IUM 路径对明确任务引入不必要的多轮交互，`Start()` 现在尊重 LLM 首轮返回的 `ready=true` 信号：
- `corelib/workflow/intent_understanding.go`：`StartResult` 新增 `Ready bool` 和 `Intent *StructuredIntent` 字段
- `Start()` 不再丢弃 `isReady` 标志（之前用 `_` 忽略）。当 LLM 首轮返回 `ready=true` 时，不创建 session，直接返回 `StartResult{Ready: true, Intent: &intent}`
- `gui/im_message_handler_workflow.go`：`handleNeedsUnderstanding` 检查 `result.Ready`，为 true 时直接走 `confirmWorkflowStart` → 确认面板 → `StartWorkflow`
- IUM system prompt 的 `ready 判断规则` 更新：允许首轮 `ready=true`（当任务意图完全明确、无需追问时）

**延迟影响（优化后）**：
- 明确任务（如"开发一个贪吃蛇游戏，C++ cmake"）：+10-30s（IUM LLM 调用），但用户交互次数不变（IUM 首轮 ready=true → 确认面板 → 启动）
- 模糊任务（如"帮我做个东西"）：IUM 多轮澄清后启动
- 误分类任务（如"测试 ag ui 功能"）：IUM 正确拒绝，fall through 到 agent loop

**不受影响的路径**：
- `shouldBypassWorkflowForClassification`（活跃工作流期间的 cross-type detection）：仍使用 `isConcreteWorkflowType` 判断是否需要切换工作流类型，不受影响
- UIC 的 `WorkflowType` 推断机制本身（`fusionToClassification` 中的 degraded-mode inference）：仍正常工作，只是结果不再被 `handleNeedsUnderstanding` 直接消费
- `corelib/intent/workflow_type_fallback_test.go`：验证 UIC 内部推断机制，不涉及消费逻辑

**验收标准**：
- "帮我测试 ag ui功能，生成一个用户信息输入界面" → UIC 推断 coding → 日志记录 hint → fall through 到 IUM → IUM 正确拒绝 → 正常 agent loop
- "开发一个贪吃蛇游戏" → UIC 推断 coding → 日志记录 hint → fall through 到 IUM → IUM 确认 coding → 启动工作流
- "把文件发给我" → UIC 快速 reject（document_delivery, MayTriggerWorkflow=false）→ 不进 IUM → 正常 agent loop
- 所有 ShouldBypass / WorkflowConfirmation / WorkflowCandidate / intent / workflow 测试通过
- GUI 编译通过


### 93. Merge Injection 导致 SSH 工具缺失——已取消 loop 仍接受 merge + 工具列表不随 injection 更新

**来源**：用户截图——上一个任务完成/取消后，用户发送"直接用ssh连上api服务器修改配置呀"，系统回复"👌 收到，已纳入当前任务"，然后用 bash 执行原始 `ssh -o StrictHostKeyChecking=n...` 命令挂起 6-10 分钟。

**根因（两层叠加）**：

#### 层 1: 已取消的 loop 仍接受 merge

**根因**：`enterIMMessageSerializationBoundary`（桌面面板消息入口）判断"是否有活跃 loop 可以 merge"时，只检查 `currentLoopCtx != nil`，不检查 `IsCancelled()`。`CancelCurrentSession()` 调用 `ctx.Cancel()` 后等待 `DoneC`（最多 10s），在 cancel 到 defer 清理 `currentLoopCtx=nil` 之间的窗口期内，新消息被错误 merge 进正在死亡的 loop。

同一个判断逻辑在两个入口有不同实现：
- `shouldTryInlineInterrupt`（IM 通道）：检查 `currentLoopCtx != nil && !IsCancelled()` ✅
- `enterIMMessageSerializationBoundary`（桌面面板）：只检查 `currentLoopCtx != nil` ❌

**修复**：提取 `hasActiveInterruptableLoop()` 作为单一数据源，所有 interrupt/merge/injection 入口共享。

- `gui/im_interrupt_inline.go`：新增 `hasActiveInterruptableLoop()` 方法
- `gui/im_entry_serialization.go`：interrupt 检查改用 `hasActiveInterruptableLoop()`
- `gui/im_loop_control.go`：`InjectSupplementary` 改用 `hasActiveInterruptableLoop()`

**不变量**：已取消的 loop 不接受任何形式的 merge/injection。新消息 fall through 到 `chatLoopMu.Lock()`，等 loop 退出后作为独立请求正常处理。

#### 层 2: Merge 后工具列表不更新

**根因**：工具列表在 loop 启动时基于原始用户消息一次性计算（`prepareAgentLoopTools` → `routeTools`）。当 merge injection 改变了任务方向（从"分析 Nginx 配置"变为"SSH 连接服务器修改配置"），工具列表不更新。LLM 看到 injection 中的 SSH 请求，但工具列表中没有 `ssh` 工具（因为原始消息不含 SSH 关键词），只能用 `bash` 执行原始 ssh 命令——挂起 6+ 分钟。

**修复**：injection 消费时，用 injection 文本重新执行工具路由，将新激活的工具追加到当前工具列表。

- `gui/im_agent_loop_iteration.go`：`prepareAgentLoopIteration` 返回 injection 文本（第三个返回值）
- `gui/im_agent_loop_round_prep.go`：消费 injection 文本，调用 `augmentToolsFromInjection`
- `gui/im_agent_loop_tool_augment.go`：新文件
  - `augmentToolsFromInjection()`：对 injection 文本执行 `routeTools`，将新激活的工具追加到当前工具列表
  - `stripInjectionPrefix()`：剥离 `[用户补充]` 等前缀后再传给 routeTools，避免前缀干扰意图分类
  - 尊重 coding gate：当 `gateConfig.active=true` 时，不追加 `codingToolBlocklist` 中的工具

**不变量**：工具列表反映当前任务方向。Merge 的语义是"当前 loop 能处理这条消息"——通过动态工具增量路由保证这个语义成立。同时尊重 coding gate 的不变量（blocklist 中的工具在三阶段完成前不出现）。

**验收标准**：
- 取消后发送 SSH 请求 → 不被 merge 进死 loop，作为新请求独立执行
- 运行中 loop 收到 SSH injection → ssh 工具被动态追加到工具列表，LLM 使用内置 ssh 工具而非 bash
- 编码工作流三阶段期间收到 injection → 编码工具不被追加（coding gate 保护）
- 所有 interrupt/cancel/inject 测试通过
- GUI / corelib 编译通过


### 94. 桌面面板 120 秒活动超时误触发——心跳被后端过滤不到达前端

**来源**：用户截图——AI 助手面板显示"⏱️ 请求超时（120秒无响应），请重试。"，此时后端正在执行 `search_files` 工具（遍历大项目目录）。

**根因**：前端的 120 秒活动超时是 sliding window——从最后一次收到后端事件（token/progress/new-round）开始计时。后端有心跳机制（`imHeartbeatMsg = "__heartbeat__"`，每 60 秒通过 `startAgentLoopHeartbeat` 发送），但在桌面面板的 `onProgress` 回调中被**无条件过滤掉**：

```go
onProgress := func(progressText string) {
    if progressText == imHeartbeatMsg {
        return  // ← 心跳永远不到达前端，不重置计时器
    }
    ...
}
```

前端的 progress handler 架构是正确的——**先重置计时器，再决定是否显示**：
```typescript
// Reset the sliding-window activity timeout — backend is alive.
if (responseTimeoutClearRef.current) {
    responseTimeoutClearRef.current();  // ← 重置计时器
}
if (shouldHideProgressText(progressText)) {
    return;  // ← 不渲染，但计时器已重置
}
```

**核心矛盾**：心跳的设计目的是"告诉对端我还活着"，但桌面面板把它过滤掉了。"重置计时器"和"不显示给用户"是正交的两个职责，后端把它们耦合在一起——"不可见就不发送"。

**触发场景**（从日志确认）：
1. `proactive_recall` 耗时 21s（无事件发送到前端）
2. LLM 首次请求 15s（无事件）
3. `search_files` 遍历大项目 60-90s（仅开始时发送一次 `SendToolProgress`）
4. 心跳每 60s 发送一次，但被后端 `return` 掉
5. 总静默时间 = 工具执行时间 > 120s → 前端误判超时

**修复**（机制性——让心跳到达前端重置计时器，但不渲染）：

#### 1. 后端：心跳不再被过滤，正常发送到前端（`gui/app_wails_bindings.go`）

```go
onProgress := func(progressText string) {
    if progressText == imHeartbeatMsg {
        // Heartbeat must reach the frontend to reset the activity timeout
        // timer, but should not be rendered to the user.
        emitEvent("ai-assistant-progress", progressText)
        return
    }
    ...
}
```

#### 2. 前端：`HIDDEN_PROGRESS_PATTERNS` 新增心跳模式（`useAIAssistant.ts`）

```typescript
const HIDDEN_PROGRESS_PATTERNS = [
    /^__heartbeat__$/,
    /^[⏳]\s*命令仍在执行中（已\s*\d+s）:/,
];
```

**工作原理**：
1. 后端心跳 ticker 每 60s 发送 `__heartbeat__` → `emitEvent("ai-assistant-progress", "__heartbeat__")`
2. 前端 progress handler 收到事件 → `responseTimeoutClearRef.current()` 重置 120s 计时器
3. `shouldHideProgressText("__heartbeat__")` → `HIDDEN_PROGRESS_PATTERNS[0]` 匹配 → `return`（不渲染）
4. 用户看不到心跳，但计时器被重置

**为什么是机制性修复**：
- 不改超时时间（120s 对正常操作足够）
- 不在每个慢操作中手动发送进度事件（不可扩展）
- 复用已有的心跳机制（60s ticker 已存在），只是让它到达前端
- 前端的 `shouldHideProgressText` 机制已经是正确的架构（先重置计时器再过滤显示），只需让心跳通过后端的过滤

**验收标准**：
- `search_files` 遍历大项目 90s → 心跳在 60s 时重置计时器 → 不超时
- `proactive_recall` 21s + LLM 15s + 工具 90s → 心跳在 agent loop 启动后 60s 重置 → 不超时
- 用户看不到 `__heartbeat__` 文本
- 所有现有 useAIAssistant 测试通过
- GUI Go 编译通过 + 前端 TypeScript 编译通过


### 95. Skill 安装安全风险从直接拒绝改为用户确认——Critical Risk 可人工通过

**来源**：用户在技能市场搜索 Gmail skill 并点击安装时，安全扫描判定为 critical 风险后直接拒绝安装，显示红色错误提示。用户需求是：有安全风险时提示用户，用户人工确认通过后仍可安装。

**根因**：`admitManualSkillInstall`（桌面面板路径）和 `registerAndExecuteSkill`（IM/Agent 路径）中，`IsDangerous()`（level=critical）分支直接拒绝安装并返回错误，不给用户确认机会。而 `NeedsUserReview()`（level=high）分支已有确认流程。

**修复**：

#### 1. 后端：Critical risk 走确认流程（`gui/skill_install_admission.go`）

- `admitManualSkillInstall`：`IsDangerous()` 和 `NeedsUserReview()` 合并为单一分支 `IsDangerous() || NeedsUserReview()`
- 两种风险级别统一走 `confirmManualSkillInstall` 确认流程
- 确认消息区分 critical/high 两种风险等级的提示文本
- 用户拒绝 → 返回错误；用户确认 → 允许安装

#### 2. 后端：IM 路径同步修改（`gui/im_skill_hub_install.go`）

- `registerAndExecuteSkill`：`IsDangerous()` 和 `NeedsUserReview()` 合并为 `IsDangerous() || NeedsUserReview()`
- 统一走 `confirmRiskSkillInstall` 确认流程
- 用户确认/拒绝后均记录审计日志（`PolicyUserOverride` / `PolicyDeny`）

#### 3. 前端：技能面板本地化确认对话框（`SkillsManagementPanel.tsx`）

- 新增 `skill-install-risk-confirm` 事件监听器（专用事件名，与 AI 助手面板的 `critical-risk-confirm` 隔离）
- 收到后端结构化数据（skill_name/source/level/factors）后，用 `localizeText` 组装三语消息（英文/简体中文/繁体中文）
- 使用 `showConfirm`（本地化模态对话框，非系统 alert）显示风险信息
- 用户确认/取消后通过 `ResolveCriticalConfirm` 回传结果

#### 4. 后端事件名隔离

- `confirmManualSkillInstall`（桌面技能面板路径）发出 `"skill-install-risk-confirm"` 事件
- `confirmRiskSkillInstall`（IM/Agent 路径）发出 `"critical-risk-confirm"` 事件
- 两条路径各自有专用事件名，前端监听器互不干扰，零竞态

#### 5. 后端传结构化数据，前端负责本地化

- payload 从预格式化文本改为结构化字段：`confirm_id`、`skill_name`、`source`、`level`、`factors`
- `factors` 为 nil 时后端保证发送空数组（`[]string{}`），避免 JSON null
- 前端用 `localizeText(en, zhHans, zhHant)` 组装本地化消息

#### 6. 审计日志一致性

- 桌面面板路径和 IM 路径的审计行为对称：
  - 用户拒绝 → `AuditActionHubSkillReject` + `PolicyDeny`
  - 用户确认 → `AuditActionHubSkillInstall` + `PolicyUserOverride`

#### 7. 测试更新

- `TestScanAndAdmitSkillBeforeRegisterIgnoresClaimedTrustedLevel`：更新断言——critical skill 经确认后可安装（test harness 自动确认），但 scan 仍检测到 critical risk（claimed trusted 不绕过扫描）

**验收标准**：
- 技能市场安装 critical risk skill → 弹出本地化确认对话框（非系统 alert）
- 用户点"确定" → 安装继续
- 用户点"取消" → 安装被拒绝
- IM 通道安装 critical risk skill → 显示确认按钮
- 审计日志正确记录用户决策
- 所有 3 个 admission 测试通过
- Go + TypeScript 编译通过


### 95. IUM 无工具导致能力否认幻觉——Understanding Session Escape Hatch + Contract Breach Detection

**来源**：用户在 AI 助手面板说"继续改进刚才的对比文档"后提供文件路径 `d:\workprj\AI数字员工平台_MaClaw_对比分析报告.md`，maclaw 回复"我无法直接访问你本地的文件"并解释"隐私与安全"、"技术架构限制"等错误原因。

**根因**：IUM（IntentUnderstandingManager）的 LLM **没有工具**（只做意图分类），当用户发送文件路径时，LLM 按照通用 AI 训练数据回复"我无法访问本地文件"。这个非 JSON 回复被 `buildIntentParseFailureReply` 直接透传给用户。同时 understanding session 被保留，后续消息继续被困在无工具的 IUM 中。

**触发链路**：
1. "继续改进刚才的对比文档" → UIC `non_coding` conf=0.68 (< threshold 0.70) → 未被 reject → 进入 IUM → 创建 session
2. 文件路径 → `HasActiveUnderstanding=true` → 无条件路由到 IUM → IUM LLM 回复"无法访问" → parse failed → 透传给用户

**修复（三层机制性修复）**：

#### Layer 1: IUM System Prompt 能力声明（`corelib/workflow/intent_understanding.go`）

`buildSystemPrompt()` 新增"系统能力声明"section，告知 LLM maclaw 是桌面应用，可以直接读写本地文件。严禁回复"我无法访问你的本地文件"。当用户提供文件路径时，将其视为任务输入材料，按正常意图分类流程处理。

#### Layer 2: Understanding Session Escape Hatch（`gui/im_message_handler_workflow.go`）

在 `shouldBypassWorkflowForIntent` 返回 false 之后、`QuickFilter.Classify` 之前，新增 escape hatch：

- 条件：有活跃 understanding session + 消息 >= 10 rune + `len(sess.Rounds) > 0`
- 判断：`uic.Classify` 对当前消息不确信是工作流任务（`!(hasConcreteType && isConfident)`）
- 动作：cancel session + return nil → fall through 到正常 agent loop（有工具）

Guards 防止误杀合法 session 交互：
- 短消息（< 10 rune）跳过 escape hatch → 由 short message fast path 路由到 `handleActiveUnderstanding`
- UIC 确信是工作流任务（`hasConcreteType && isConfident`）→ 不 escape

#### Layer 3: Contract Breach Detection（`corelib/workflow/intent_understanding.go`）

`HandleInput` 中 parse failed 时，`buildIntentParseFailureReply` 检测 LLM 是否完全脱离结构化 JSON 合约：
- 能力否认模式（"无法访问"/"不能读取"等）→ contract breach
- 长自由文本（>200 rune 无 JSON 结构）→ contract breach

检测到 contract breach 时，`HandleInput` 自己 cancel session 并返回 error（不是返回 marker 让调用方 cancel）。`handleActiveUnderstanding` 的 `err != nil` 路径 cancel session + return nil → fall through。

**三层防护的关系**：
- Layer 1 从源头阻止 LLM 产生能力否认（根本解决）
- Layer 2 在路由层拦截，UIC 不确信时 cancel session（正确层面的修复）
- Layer 3 在 IUM 内部兜底，LLM 仍然脱离合约时恢复（纵深防御）

**验收标准**：
- 用户发送文件路径 → escape hatch 触发（UIC `office` conf=0.67 < 0.70）→ session cancel → agent loop 正常读取文件
- 用户在 session 中说"确认"（< 10 rune）→ 跳过 escape hatch → 正常路由到 IUM
- 用户在 session 中说"用 React 开发前端"（UIC `coding` conf=0.85）→ 不 escape → 正常路由到 IUM
- IUM LLM 仍然回复"无法访问" → contract breach → cancel session → fall through
- 所有 150+ workflow 测试通过 + 3 个新增 contract breach 测试通过


### 96. 同类型工作流重启时旧内容泄漏到新工作流面板

**来源**：用户发送"设计一个庆祝布宝生日的ppt"，系统正确识别为 `presentation_design` 工作流并启动，但右侧文档预览面板显示的是上一次 PPT 工作流的产出物（"已确认最新 PPT 文件在这里！"），而不是新任务的第一阶段文档。

**根因**（两个问题叠加）：

1. **磁盘上的旧工作流文档未清理**：`{projectPath}/.maclaw/workflow/` 目录下保留着上一次 PPT 工作流的阶段文档文件（`audience_goal.md` 等）。`EmitDocUpdate` 中的 `readPersistedDoc` 可能读到旧文件。新工作流启动时没有任何代码清理这个目录。

2. **对话历史污染**：新工作流启动时，对话历史中仍包含旧 PPT 工作流的完整对话（需求文档、确认消息、PPT 生成结果等）。LLM 在新工作流的第一阶段 agent loop 中看到旧上下文，输出了旧内容。`captureWorkflowDocAfterAgentLoop` 将这个包含旧内容的输出保存为新工作流的阶段产出物并发送到前端面板。

**修复**：

#### 1. 新工作流启动时清理持久化文档（`gui/workflow_adapter_persistence.go`）

- 新增 `CleanPersistedWorkflowDocs()` 方法：删除 `.maclaw/workflow/` 目录下所有 `.md`/`.txt` 文件
- 在 `handlePostStartWorkflow` 中调用，确保新工作流不会读到旧文件

#### 2. 新工作流启动时清理对话历史（`gui/im_message_handler_workflow.go`）

- `handlePostStartWorkflow` 中新增 `memory.Clear(userID)` 调用
- 清空旧工作流的对话历史，防止 LLM 被旧上下文污染
- 新工作流有自己的 phase prompt 提供所有必要指令

**关键设计决策**：不调用 `clearPerUserSessionState`——它会调用 `cancelWorkflowForUser` 取消刚启动的新工作流。只清理 memory（对话历史）和磁盘文件，不清理工作流引擎状态。

**验收标准**：
- 完成一个 PPT 工作流后，发送新的 PPT 任务 → 右侧面板不显示旧内容
- 新工作流的第一阶段 agent loop 中 LLM 不受旧对话历史影响
- `.maclaw/workflow/` 目录在新工作流启动时被清理
- 所有 TestWorkflow* 测试通过
- corelib/workflow 所有测试通过
- GUI go vet 通过


### 97. Skill 运行时 AgentView 表单拦截 Agent 自动执行——参数缺失应返回结构化错误让 LLM 补全

**来源**：用户测试 drawio-skill，Agent 调用 `run_skill(name="drawio-skill")` 时因缺少 `input` 参数，系统弹出 AgentView 表单让用户手动填写，而不是让 Agent 从对话上下文中提取参数后重试。

**根因**：`toolRunSkill()` 中调用 `emitSkillRunAgentViewIfNeeded(name, args)`，当 skill 有必需参数未提供时，直接弹出 UI 表单（AgentView）并返回"请在右侧任务面板填写后提交"。这是**职责错位**——Agent（LLM）应该自动从用户对话上下文中提取所需参数并重试调用，而不是把填表的责任推给用户。

**机制性问题**：
1. LLM 的 `run_skill` 工具定义只有通用的 `input (string, optional)` 参数描述，不知道具体 skill 需要什么参数
2. 参数缺失时系统选择"弹表单让用户填"而不是"告诉 LLM 缺什么参数让它补全"
3. LLM 收到"请在右侧任务面板填写"后进入死胡同——它无法代替用户填表单
4. 用户已经在对话中说了要做什么（如"画一个北京5环图"），这个信息应该由 Agent 自动提取

**修复**：

#### 1. Agent 路径：参数缺失返回结构化错误（`gui/im_tool_skill_run.go`）

- `toolRunSkill()` 中将 `emitSkillRunAgentViewIfNeeded` 替换为 `checkSkillRunMissingParams`
- 新增 `checkSkillRunMissingParams()` 方法：检测缺失参数后返回结构化错误信息，包含：
  - 缺少的参数名和描述
  - 如何修复的示例调用
  - Skill 描述（帮助 LLM 理解参数语义）
  - `[action: provide_args]` 标记（与 corelib 的 `FormatMissingRequiredArgsMessage` 一致）
- LLM 收到错误后从用户对话上下文中提取信息，重新调用 `run_skill` 并补全参数

#### 2. 用户路径：AgentView 表单保留（`gui/agent_view_skill.go`）

- `emitSkillRunAgentViewIfNeeded` 保留但不再从 Agent 路径调用
- `handleSkillRunAgentViewSubmit` 保留——用户从技能面板手动点击"运行"时仍使用表单
- `buildSkillRunAgentView` 保留——用户主动触发时构建表单

**设计原则**：
- **Agent 路径**（LLM 调用 `run_skill`）：参数缺失 → 返回结构化错误 → LLM 补全后重试
- **用户路径**（用户在技能面板点击"运行"）：参数缺失 → 显示表单 → 用户填写后提交
- 两条路径的区别：Agent 有对话上下文可以自动推断参数，用户需要 UI 引导

**正确的执行链路（修复后）**：
```
用户: "用 drawio-skill 画一个北京5环图"
  → LLM 调用 manage_skill(action="run", name="drawio-skill")  [缺少 input]
  → checkSkillRunMissingParams 返回: "缺少参数 input (描述要画的图表内容)"
  → LLM 从对话上下文提取: input="北京5环地图，标注各环路名称"
  → LLM 重新调用 manage_skill(action="run", name="drawio-skill", input="北京5环地图...")
  → 参数齐全 → 执行
```

**验收标准**：
- Agent 调用 `run_skill` 缺少参数时 → 返回结构化错误信息，不弹 AgentView 表单
- LLM 收到错误后能从上下文补全参数并重试
- 用户在技能面板手动点击"运行" → 仍显示 AgentView 表单（行为不变）
- GUI 编译通过


### 98. SSH exec 输出丢失——WaitForOutput 从"沉默猜测"升级为"Prompt-Driven Completion Detection"

**来源**：用户让 maclaw 升级 api.maclaw.top 上的 OmniRoute 到 3.8。SSH exec 执行 `cp -r ... ; ls -la ...` 等命令时，返回结果只有命令回显（PTY echo），没有实际命令输出。LLM 说"SSH 输出好像被截断了"、"SSH 输出一直不返回"，反复切换方法（exec → bash → 重连），浪费大量迭代。

**根因（机制性分析）**：

`WaitForOutput` 使用"沉默时间"（连续 N 次轮询无新输出）来**猜测**命令是否完成。这是启发式猜测，不是确定性信号。

问题链路：
1. LLM 调用 `ssh(exec, command="cp -r /opt/data /opt/backup; ls -la /opt/backup")`
2. PTY 回显命令文本（1-2 行）→ `WaitForOutput` 开始计时
3. `cp -r` 开始执行，复制大目录需要 5-10 秒，期间无输出
4. 旧稳定阈值 8 × 300ms = 2.4s 后，`WaitForOutput` 判定"输出稳定"并返回
5. 返回结果只有命令回显，`ls -la` 的输出永远不会被捕获
6. LLM 看到只有回显没有结果，认为"SSH 有问题"，开始切换方法

**核心矛盾**：用"沉默时间"猜测命令完成是不可靠的。`cp -r` 可能沉默 30 秒，`echo hello` 只沉默 10ms。固定阈值无法同时满足两者。

**机制性修复：Prompt-Driven Completion Detection**

命令完成的**唯一确定性信号**是 shell prompt 重新出现。当命令执行完毕后，shell 打印新的 prompt（如 `root@server:~# `），等待下一条命令。

修复后的 `WaitForOutput` 使用三层检测：

1. **主信号（prompt 检测）**：每次有新输出时检查最后一行是否是 shell prompt。如果是，立即返回。快速命令（`echo hello`）在 prompt 出现时立即返回（~300ms），不需要等稳定阈值。

2. **辅助信号（两阶段稳定性 fallback）**：
   - 阶段 1（等待首行实际输出）：命令回显后等待 ~4s（13 × 300ms）。覆盖 cp/docker 等命令的启动延迟。
   - 阶段 2（等待输出结束）：已有实际输出后等待 ~2.4s（8 × 300ms）。

3. **超时保护**：`maxWait` 到期后发送 Ctrl+C 防止 shell 被锁住。

**关键改进**：
- `looksLikeShellPrompt` 增强：正确剥离 ANSI 转义序列（CSI `\x1b[...X` 和 OSC `\x1b]...BEL`），支持带颜色/标题设置的 prompt
- 默认 `wait_seconds` 从 5 提升到 15：有了 prompt 检测后，快速命令不受影响（prompt 出现即返回），慢命令有足够时间完成
- `phase2TriggerLines` 从 2 提升到 3：PTY 回显可能是 1-2 行（命令文本 + 换行），实际输出从第 3 行开始

**效果对比**：

| 命令 | 修复前 | 修复后 |
|------|--------|--------|
| `echo hello` | 等 2.4s 稳定阈值 | prompt 出现即返回（~300ms）|
| `cp -r big_dir/ backup/; ls -la backup/` | 2.4s 后返回空结果 | 等 cp 完成 → ls 输出 → prompt 出现 → 返回完整结果 |
| `docker pull image:tag` | 2.4s 后返回空结果 | 等 pull 完成 → prompt 出现 → 返回完整结果 |
| 挂起的命令（sqlite3 锁）| 等 maxWait 后 Ctrl+C | 等 maxWait 后 Ctrl+C（行为不变）|

**修改文件**：
- `corelib/remote/ssh_manager.go`：
  - `WaitForOutput()` 重写为 Prompt-Driven 机制
  - `looksLikeShellPrompt()` 增强 ANSI 转义码剥离
  - 新增 `stripANSIForPromptCheck()` 函数
- `gui/im_ssh_tools.go`：默认 `waitSec` 从 5 提升到 15
- `gui/im_tool_definitions.go`：`wait_seconds` 描述更新

**测试**：
- `corelib/remote/ssh_prompt_detect_test.go`：新增 17 个测试
  - `TestLooksLikeShellPrompt`：11 个用例覆盖各种 prompt 格式（含 ANSI 转义码）
  - `TestStripANSIForPromptCheck`：6 个用例覆盖 CSI/OSC/组合转义码

**验收标准**：
- `cp -r big_dir/ backup/; ls -la backup/` → 等 cp 完成后返回 ls 输出（不再只返回命令回显）
- `echo hello` → prompt 出现即返回（~300ms，不等 4s 稳定阈值）
- 带 ANSI 颜色/标题的 prompt → 正确识别为 prompt
- 所有 remote 包测试通过
- corelib/remote 编译通过


### 99. Skill 上传前可移植性门禁——共享 PrepareSkillForUpload 预检

**来源**：用户需求——优化 skill 上传到 SkillMarket（hubcenter/hub）之前的处理，让 agent 检查确认并修正以下问题再上传：文件完整、skill 中没有影响其它机器安装使用的绝对路径（如果有，需改为 SkillRunner 支持的宏或相对路径）。

#### 根因（机制性分析）

可移植性基础设施已存在（`ValidateSkillPortability` 检测绝对路径、`AutoFixPortability` 把包内绝对路径改写为 `{baseDir}`/把家目录路径改写为 `$HOME`、`CheckStepFileReferences` 检查引用文件是否打包），但 **agent 上传路径存在两个机制缺口**：

1. **GUI `toolUploadSkill`**：只检查 usage/success 计数，然后委托 `UploadNow`。`UploadNow` 的 autofix 跑在**临时副本**上——源 skill 目录从不被持久化修正；被质量门禁拦截时只返回一句 `score=X reasons=Y`，agent 看不到**具体是哪些绝对路径/缺失文件**，无法定位修正后重试。
2. **TUI `skillUpload`**：只做 `ValidateSkillPortability`（无 autofix），且**完全没有**文件完整性检查，引用但未打包的脚本会漏到其它机器。

两端各自实现一套上传前处理，缺口不一致。

#### 修复：单一共享预检 `PrepareSkillForUpload`

**机制原则**：上传前"是否能在其它机器安装/运行"的判定只实现一次，放在 corelib，GUI 和 TUI 共享同一条预检路径。

- `corelib/skill/upload_preflight.go`（新文件）：
  - `PrepareSkillForUpload(skillDir)`：
    1. 对**真实** skill 目录运行 `AutoFixPortability`（持久化改写 + `.bak` 备份）——包内绝对路径→`{baseDir}`、家目录路径→`$HOME`、反斜杠→正斜杠、补全 platforms
    2. 重新 `ValidateSkillPortability`，收集 autofix 后**仍残留**的绝对路径（既不在包内也不在 `$HOME` 下的机器特定路径，如 `/opt/acme/...`）→ `BlockingPaths`
    3. 复用 `CheckStepFileReferences`（与 runner 同一套 precheck）验证命令引用的本地文件是否都打包进 skill 目录 → `MissingFiles`
  - `UploadPreflightResult.Portable()`：无残留绝对路径且无缺失文件时为 true
  - `FormatUploadPreflight(result)`：生成 agent 可读报告——列出每个绝对路径及其修正建议、缺失文件清单，并说明 SkillRunner 支持的可移植引用方式（`{baseDir}/...`、`$HOME/...`、`{{key}}` 运行时参数），引导 agent 修正后重试

- `gui/im_tools_misc.go`：
  - `toolUploadSkill` 在 usage/quality 门禁**之前**新增 `runUploadPortabilityGate(name)`
  - `runUploadPortabilityGate`：解析真实 skill 目录 → 快照（用于回滚）→ `PrepareSkillForUpload`（持久化 autofix）→ 若 autofix 改写了文件则跑安全扫描（`scanManagedSkillWriteback`），未通过则回滚 → 刷新索引（`refreshSkillIndexesAfterMutation`）→ 不可移植时返回 `FormatUploadPreflight` 报告阻止上传
  - 新增 `resolveManagedSkillDir(name)`：按名解析已注册 skill 的目录，回退到 `PrimarySkillsDir/<name>`
  - `force=true` 时跳过门禁（agent 已确认并修正后强制上传）

- `tui/tool_manage_skill.go`：`skillUpload` 把原来的"仅 validate"改为 `PrepareSkillForUpload`（autofix + blocking paths + missing files），不可移植时返回 `FormatUploadPreflight`，与 GUI 行为一致；同样支持 `force`

- `corelib/skill/manage_skill_actions.go`：`upload` action 描述更新为"上传前自动检查并修正绝对路径、补全缺失文件等可移植性问题"
- `gui/im_tool_definitions.go`：`manage_skill` 工具新增 `force` 参数说明（与 action=upload 配合，跳过门禁强制上传）

#### 机制性特征

- **单一数据源**：`PrepareSkillForUpload` 是上传前可移植性判定的唯一实现，GUI/TUI 共享；新增检测规则只改这一处
- **复用运行时同款检查**：文件完整性用 `CollectMissingStepFileReferences`（与 runner precheck `CheckStepFileReferences` 共享同一套引用提取原语），上传门禁与运行时行为一致
- **一次报告所有问题**：`CheckStepFileReferences` 在第一个缺失文件处即返回（运行时快速 precheck），但上传门禁用 `CollectMissingStepFileReferences` 一次性收集所有缺失文件，避免 agent 反复"修一个→重传→再发现下一个"的多轮往返；不再依赖脆弱的错误字符串解析
- **持久化修正**：autofix 跑在真实 skill 目录（带 `.bak` 备份 + 安全扫描回滚），而非临时副本——修正结果对后续 run/upload 都生效
- **可操作报告**：阻止时列出具体绝对路径、缺失文件和修正方式，agent 能定位并修正后重试，而非只看到一个分数

#### Review/Fix/Optimize（本轮）

- `corelib/skill/runner_filecheck.go`：新增 `CollectMissingStepFileReferences(entry)`——不在首个缺失文件处提前返回，一次扫描收集所有缺失的引用文件和 working_dir（绝对路径、去重、保持首次出现顺序），与 runtime precheck 共享 `commandFileReferencesForPrecheck`/`resolveCommandFileReference` 原语
- `corelib/skill/upload_preflight.go`：`missingBundledFileReferences` 改为调用 `CollectMissingStepFileReferences`，删除脆弱的错误字符串解析（`extractMissingReferencePath`/`stripActionTag`）
- `gui/im_tools_misc.go`：`toolUploadSkill` 两个 `if !force` 块合并为一个，门禁顺序不变（可移植性门禁 → usage/quality 门禁）

#### 验收标准

- skill 含包内绝对路径（如 `python /skills/x/scripts/run.py`）→ 自动改写为 `{baseDir}/scripts/run.py` 并持久化，门禁放行
- skill 含机器特定绝对路径（如 `cat /opt/acme/config.json`）→ 门禁阻止，报告列出该路径及"改为 {baseDir}/相对路径或 {{key}} 运行时参数"建议
- skill 引用未打包的脚本（如 `{baseDir}/scripts/missing.py` 不存在）→ 门禁阻止，报告列出缺失文件
- skill 跨多个 step 引用多个缺失文件 → 报告**一次性**列出所有缺失文件（不只第一个）
- 干净 skill（`{baseDir}/scripts/run.py` 且文件已打包）→ 门禁放行
- `force=true` → 跳过门禁
- 6 个 corelib 预检测试 + 2 个 GUI 门禁测试通过；corelib/skill 全量测试通过；GUI/TUI/corelib 编译通过、`go vet` 通过
- 注：`TestToolValidateSkillAutoFixScansAndRollsBackRiskyWriteback`（TUI `TestManageSkillRunHydratesMarkdownMetadata`）为预先存在的失败，与本次改动无关


### 100. 多 Agent Tab 并发时结果不显示——`appendUnique` 丢弃已更新消息版本

**来源**：用户在 project tab（如"北京天气"）使用 buffer queue 依次发送多个城市天气查询，每次请求完成后显示 `▶ 🌙 思考中...`（空 placeholder）和最终响应文本**同时**出现，导致结果看似没有正确显示。

**根因**：`AIAssistantPanel.tsx` 中 `wasSending && !sending` effect 里有一个 `appendUnique` 内联函数，其语义是"只添加 history 里没有的新 ID，跳过已有 ID 的消息"。这导致了以下竞态：

1. 流式响应开始 → `appendTokenToDetachedRound` / live-sync effect（第 302 行）把**空内容的** assistant placeholder 写入 `projectTabMessages`（ID 已注册）
2. LLM 完成 → `finalizeRoundMessage` 把**最终内容**写入共享 `messages` 里的同一条消息（相同 ID，content 更新）
3. `sending` 变为 false → `wasSending && !sending` effect 执行 `appendUnique`
4. `appendUnique` 发现 placeholder 的 ID 已在 `projectTabMessages` 中 → **跳过**，不更新
5. `projectTabMessages` 里保留的是空内容的旧版本
6. `displayMessages` 通过 `mergeChatMessages(projectTabMessages, liveProjectMessages)` 合并时，空 placeholder 和最终内容都出现（因为 React state 更新的异步性导致两个版本可能同时可见）

**修复**：将 `wasSending && !sending` effect 里的两处 `appendUnique` 调用替换为 `mergeChatMessages`：

```typescript
// 修复前（有 bug）
const appendUnique = (history) => {
    const existingIds = new Set(history.map(m => m.id));
    const unique = newMessages.filter(m => !existingIds.has(m.id));
    return unique.length === 0 ? existingHistory : [...existingHistory, ...unique];
};
setProjectTabMessages(prev => appendUnique(prev));
saveTabState(..., { history: appendUnique(baseHistory) });

// 修复后（正确）
setProjectTabMessages(prev => mergeChatMessages(prev, newMessages));
saveTabState(..., { history: mergeChatMessages(baseHistory, newMessages) });
```

`mergeChatMessages` 已经是系统内所有其他消息合并路径（busySessionKeys effect、live-sync effect、displayMessages）使用的统一函数，语义是"相同 ID 的消息，后面的 group wins"——确保 `newMessages` 里的最终内容版本正确替换 `projectTabMessages` 里的空 placeholder。

**修改文件**：`gui/frontend/src/components/ai/AIAssistantPanel.tsx`

**测试**：新增回归测试 `replaces streaming placeholder with final content when round completes (no ghost 思考中)`，验证请求完成后 UI 里只有一条最终内容的 assistant 消息，不出现空 placeholder 幽灵。

**验收标准**：
- buffer queue 排队多个城市天气查询，每个请求完成后只显示最终结果，不出现"思考中"幽灵
- 116 个 AIAssistantPanel 测试全部通过
- 157 个 useAIAssistant 测试全部通过


### 101. 安全策略默认改为 relaxed——减少普通用户的确认弹框打断

**来源**：用户反馈 standard 模式下任何 `rm`、`sudo` 命令都弹确认框，个人使用场景下太频繁。

**根因**：`standard` 模式对 high/critical 风险统一触发 `PolicyAsk`（弹确认框）。`rm -rf`（无论目标路径）、`sudo`（无论后接什么命令）都被升级到 critical，每次都弹框。普通用户在本机本用户目录下操作，这些是日常命令，不应每次都拦截。

**修复（三层改进）**：

#### 1. 默认安全模式从 `standard` 改为 `relaxed`

`relaxed` 模式的规则：所有风险等级均 allow（审计记录但不弹框）。这适合本机个人使用——安全检测仍然运行并记录审计日志，但不中断工作流。

**变更点**：
- `corelib/security/policy_engine.go`：`NewPolicyEngine()` 和 `normalizePolicyEngineMode()` 默认值从 `"standard"` 改为 `"relaxed"`
- `gui/policy_engine.go`：同步修改
- `tui/views/config.go`：`normalizeImplicitDefaults()` 从 `applySecurityProfile("standard")` 改为 `applySecurityProfile("relaxed")`
- `tui/views/config_fields.go`：`currentSecurityProfile()` 空值默认返回 `"relaxed"`；`securityProfilePresets` 新增 `relaxed` 预设（在 standard 之前，让用户在 UI 中看到推荐选项在第一位）

用户仍可在设置中选择 standard（弹确认框）或 strict（危险命令直接拒绝）。

#### 2. `rm -rf` 改为路径感知匹配

从 `dangerousKeywords`（纯子串，无上下文）移到 `threatPatternCategories.destructive`（正则 + Guard）：

- Pattern: `rm\s+-r[f]?\s+/` — 匹配 `rm -rf /...` 和 `rm -r /...`
- Guard: `rm\s+-r[f]?\s+(/home/|/root/|/tmp/|/var/tmp/|~/|\$HOME/)` — 用户目录操作视为安全上下文

效果：
- `rm -rf /usr/local/` → critical（系统路径，无 guard 匹配）
- `rm -rf ~/old_build/` → 不命中 destructive 模式（guard 匹配抑制）→ medium（bash 工具的基础风险）
- `rm -rf ./node_modules/` → 不命中（非绝对路径 `/` 开头）→ medium

#### 3. `sudo` 安全上下文大幅扩充

从 7 个安全上下文扩充到 23 个，覆盖日常系统管理命令：
- 新增：`mkdir`、`cp`、`mv`、`ln`、`tar`、`cat`、`tee`、`kill`、`killall`、`journalctl`、`service ... start/stop`、`snap install`、`dpkg`、`rpm`、`ufw`、`certbot`、`chmod`（数字模式）
- 增强：`chown`（不再要求 `$` 后缀）、`systemctl`（增加 stop/enable/disable）、`apt`（增加 remove）

安全上下文命中后降为 high（standard 模式下仍会弹框，但 relaxed 模式直接放行）。

**安全档位说明（修改后）**：

| 档位 | PolicyMode | 行为 | 适用场景 |
|------|-----------|------|---------|
| 轻松模式（默认）| `relaxed` | 只审计不拦截 | 本机个人使用 |
| 标准模式 | `standard` | high/critical 弹确认框 | 共享机器/企业环境 |
| 严格模式 | `strict` | dangerous 直接拒绝，medium 也弹框 | 高安全环境 |
| 开发者模式 | `developer` | 完全放行 | 安全研究人员 |

**验收标准**：
- 新安装默认 relaxed：`rm -rf ./build/` 不弹框
- 用户切到 standard 后：`rm -rf /` 仍弹框（critical），`rm -rf ~/trash/` 不弹框（medium→audit）
- `sudo apt install python3` 在 standard 模式下不弹框（safe context → high → ask，但 relaxed 下直接放行）
- 所有 25 个 corelib/security 测试通过
- 所有 5 个 TUI config security 测试通过
- GUI / TUI / corelib 编译通过


### 102. 非代码修复命令误触发 CodingSubAgent——删除 bug_fix 意图到 SubAgent 的自动路由

**来源**：用户反馈——带"修复"、"改正"等修复类关键词的命令容易触发 CodingSubAgent，导致体验异常。如"修复配置文件"、"改正文档错误"等请求被路由到精简编码环境执行。

**根因**：`shouldRouteGateResultToDirectCodingSubAgent()` 在 UIC 分类为 `GateIntentBugFix` 且 trusted（Layer 2/3, confidence ≥ 0.60）时，无条件路由到 CodingSubAgent。`RequireExistingCodeEvidence` 只检查项目目录有代码文件（在代码仓库中永远为 true），不区分用户请求是否指向代码修复。

**机制性问题**：这不是"检测不准"的问题——用关键词或语义来区分"修复代码"vs"修复配置"是无穷尽的 workaround。真正的问题是 **CodingSubAgent 不应该从意图分类自动激活**。

CodingSubAgent 的设计定位（#75）是"纯净上下文编码执行器"：
- 只有 5 个精简工具（read_file/write_file/edit_file/bash/list_directory）
- 无对话历史、无记忆、无 steering、无 SSH/browser/memory 等 35+ 工具
- 设计用途：工作流 orchestrator 委派的编码子任务（implementation 阶段）

用户发起的 bug-fix 请求：
- 需要完整工具集（SSH、browser、memory、配置修改等）
- 需要对话历史上下文
- 主 agent loop 已有完整支持（#31 CodingToolGate 对 bug_fix bypass 三阶段）

**修复**：`shouldRouteGateResultToDirectCodingSubAgent()` 直接返回 false——删除整个 bug_fix → SubAgent 的自动路由。

SubAgent 激活路径仅保留两条：
1. **Orchestrator-driven**（`ShouldUseSubAgent`）：工作流 implementation 阶段的任务委派
2. **delegate_task 工具**（`im_tool_execution.go`）：LLM 显式调用 delegate_task(agent="coding_workflow")

**权限链（修复后）**：
```
用户消息 → 意图分类 → bug_fix
                        ↓
             主 Agent Loop（完整工具集 + 对话历史 + 记忆）
                        ↓
             CodingToolGate bypass（#31，不走三阶段）
                        ↓
             LLM 直接用 read_file/edit_file/bash 等修复
```

**不受影响的路径**：
- 工作流 orchestrator 驱动的 SubAgent（ShouldUseSubAgent）→ 不变
- delegate_task 工具显式委派 → 不变（LLM 主动决策）
- CodingToolGate 的 bug_fix bypass → 不变

**验收标准**：
- "修复配置文件" / "改正文档" / "修改 steering 规则" → 主 agent loop 正常执行，不触发 SubAgent
- "修复代码中的 bug" / "repair the startup crash" → 主 agent loop 正常执行（有完整工具集），不触发 SubAgent
- 工作流 implementation 阶段的 orchestrator 委派 → SubAgent 正常工作（不受影响）
- LLM 调用 delegate_task(agent="coding_workflow") → SubAgent 正常工作（不受影响）
- 所有 18 个 DirectCodingSubAgent 测试通过
- 所有 CodingGate / GateIntent / ShouldUseSubAgent / RouteTools / RouteSubAgent 测试通过
- GUI `go vet` 通过


### 103. SSH 后台任务注册表持久化 + Orphan 任务重新发现

**来源**：用户反馈——SSH 后台任务在 agent loop 中断/进程重启后丢失，LLM 无法发现已运行的后台任务导致重复创建（截图显示 6 个相同的 sshpass 后台任务并行运行）。

**根因**：`SSHBackgroundTaskManager.tasks` 是纯内存 `map[string]*SSHBackgroundTask`，没有任何持久化机制。当 maclaw 进程重启（或 agent loop 因各种原因终止后重新启动）时：
1. `NewSSHBackgroundTaskManager()` 创建全新的空 map
2. `list_tasks` 返回 "当前无后台任务"
3. `check_task(旧task_id)` 返回 "background task not found"
4. 但远程服务器上的进程仍在运行（通过 `nohup` 保证存活）
5. 日志文件 `/tmp/maclaw_bg_*.log` 和 PID 文件 `/tmp/maclaw_bg_*.pid` 仍在远程服务器上
6. LLM 看到 list_tasks 为空，以为没有任务在跑，重新提交相同命令

**修复（两层机制性修复）**：

#### Layer 1: 任务注册表持久化到磁盘

- `corelib/remote/ssh_background_task_persist.go`：新文件
  - `SetPersistDir(dir string)`：设置持久化目录，调用后立即从文件加载已有任务
  - `saveToDisk()`：将 active 或最近 24 小时内的任务序列化为 JSON 写入 `{dir}/ssh_bg_tasks.json`（原子写入：临时文件 + rename）
  - `loadPersistedTasks()`：从文件恢复 active 状态的任务到内存 map（不覆盖已有的内存任务）
  - `signalPersist()`：异步 debounced 触发持久化（150ms 去抖，避免频繁写盘）
  - `persistLoop()`：后台 goroutine，消费 debounce 信号后执行 `saveToDisk()`
  - `extractHostIDFromSessionID()`：从 session ID 格式 `ssh_user@host:port_N` 中提取 host ID
- `corelib/remote/ssh_background_task.go`：
  - `SSHBackgroundTaskManager` 新增 `persistDir`、`persistCh`、`persistOnce` 字段
  - `Submit()` 成功后调用 `signalPersist()`
  - `CheckTask()` 状态变更后调用 `signalPersist()`
  - `KillTask()` 后调用 `signalPersist()`
- `gui/im_ssh_tools.go`：`ensureSSHManager()` 创建 `bgTaskMgr` 后调用 `SetPersistDir(app.GetDataDir())`
- `tui/app.go`：创建 `bgTaskMgr` 后调用 `SetPersistDir(filepath.Join(dataDir, "data"))`
- `tui/pipe_mode.go`：同步添加持久化

#### Layer 2: SSH 重连时重新发现 Orphan 任务

- `corelib/remote/ssh_background_task_persist.go`：
  - `RediscoverOrphanTasks(sessionID string)`：SSH 连接成功后调用
    1. 扫描远程服务器 `/tmp/maclaw_bg_*.pid` 文件
    2. 读取每个 PID 文件内容
    3. 检查 PID 是否存活（`kill -0`）
    4. 存活且注册表中没有 → 重新注册到 `tasks` map
    5. 注册后触发 `saveToDisk()` 持久化
- `gui/im_ssh_tools.go`：
  - 新建 SSH 会话成功后异步调用 `bgTaskMgr.RediscoverOrphanTasks(session.ID)`
  - 重连成功后同步调用
- `tui/app.go`：TUI 的 `OnConnected` 回调中异步调用 `RediscoverOrphanTasks`

**持久化文件格式**：
```json
{
  "tasks": [
    {
      "task_id": "bg_1781147540_31",
      "session_id": "ssh_root@api.example.com:22_1",
      "host_id": "root@api.example.com:22",
      "command": "pip install torch",
      "log_file": "/tmp/maclaw_bg_bg_1781147540_31.log",
      "pid_file": "/tmp/maclaw_bg_bg_1781147540_31.pid",
      "status": "running",
      "pid": "12345",
      "started_at": "2026-06-11T10:00:00Z"
    }
  ],
  "updated_at": "2026-06-11T10:05:00Z"
}
```

**机制性特征**：
- **单一数据源**：`SetPersistDir` 是持久化配置的唯一入口，GUI/TUI 共享同一套持久化逻辑
- **不修改现有 API**：`ListTasks()`、`CheckTask()`、`Submit()` 行为不变，持久化对消费方透明
- **自动清理**：只持久化 active 或 24 小时内的任务，文件不会无限膨胀
- **重新发现**：远程进程通过 PID 文件和 `kill -0` 验证存活，不依赖本地注册表
- **向后兼容**：不调用 `SetPersistDir` 时行为完全不变（纯内存）

**测试**：5 个新增测试全部通过
- `TestSetPersistDir_SavesAndLoadsActiveTasks`：active 任务持久化后恢复，completed 不恢复
- `TestSetPersistDir_DoesNotOverwriteExistingInMemoryTask`：磁盘加载不覆盖内存中的最新状态
- `TestExtractHostIDFromSessionID`：session ID → host ID 解析正确
- `TestSignalPersist_NoPersistDirIsNoop`：未配置持久化时不 panic
- `TestSaveToDisk_ExpiresOldNonActiveTasks`：>24h 的非 active 任务不持久化

**验收标准**：
- 进程重启后 `list_tasks` 能列出之前仍在运行的后台任务
- SSH 重连后自动发现远程服务器上的 orphan 进程并注册
- LLM 不再因 `list_tasks` 返回空而创建重复的后台任务
- 所有 11 个 corelib/remote SSH 测试通过
- GUI / TUI / corelib 编译通过、`go vet` 通过


### 104. 配置文件并发读写竞态——从全量覆盖到原子增量 Patch + Hub 重连 Provider 保护 + LLM Ping 瞬态容错

**来源**：用户报告"工作过程中突然弹出 onboarding，需要用户注册，然后服务模型切换为了 maclaw 官方，通用设置的日志相关设置也丢失"。

#### 根因（三层叠加）

##### 根因 1 (P0): 前端 `SaveConfig` 全量覆盖后端并发修改

前端模型设置面板的"保存"按钮调用 `SaveConfig(全量config快照)`。该快照在用户打开面板时加载——从打开到点击保存的时间窗口内，后端 goroutine 通过 `PatchConfig` 更新了 credentials/provider/onboarding/log settings 等字段，这些更新被前端的 stale 快照覆盖。

同样，`onClearRegistration` 路径使用 `LoadConfig → mutate → SaveConfig` 模式，存在相同的 TOCTOU 竞态。

##### 根因 2 (P1): Hub 重连时 `applyHubLLMServiceStatusToConfig` 覆盖用户 Provider 选择

`syncHubLLMServiceStatusToConfig` 在 hub WebSocket 重连后调用。旧逻辑中 `!isMaclawLLMConfiguredWithConfig(*cfg)` 条件在 provider 配置不完整时（如 hub 波动期间 viewer_token 暂时为空导致判断异常）无条件覆盖 `MaclawLLMCurrentProvider` 为"MaClaw官方"。

##### 根因 3 (P1): LLM 首次 Ping 对瞬态故障零容忍

启动时 `PingMaclawLLM` 如果因 hub 502（几秒后恢复的瞬态错误）返回 `online: false`，前端立即弹出 onboarding/LLM 配置面板。没有重试机制。

#### 修复

##### Fix 1: 前端从全量 `SaveConfig` 改为原子 `PatchConfigFields`

- `gui/app.go`：`PatchConfigFields` 新增 `case "claude", "codex", "opencode", "codebuddy", "iflow", "kilo"` 支持完整 ToolConfig 对象 patch
- `gui/frontend/src/App.tsx`：模型设置保存从 `SaveConfig(sanitizedConfig)` 改为 `PatchConfigFields({ active_tool, claude, codex, opencode, codebuddy, iflow, kilo })`
- `gui/frontend/src/App.tsx`：`onClearRegistration` 从 `LoadConfig→mutate→SaveConfig` 改为 `PatchConfigFields({ onboarding_done: false })`
- 前端不再有任何 `SaveConfig` 的实际调用（import 保留，无调用点）

##### Fix 2: `SaveConfig` 纵深防御——`preserveBackendOwnedFields`

- `gui/app.go`：新增 `preserveBackendOwnedFields(incoming, ondisk)` 函数
- 在 `SaveConfig` 的 `configMu.Lock()` 内、写盘前调用
- 无条件从磁盘最新值恢复后端独占字段：Remote 凭据（MachineID/Token/ViewerToken/SN/UserID/ClientID/TenantID/TenantName/Nickname/MachineName/SkillMarketSessionToken）、LLM Provider 状态（CurrentProvider/URL/Key/Model/Protocol/TimeoutSec/ContextLength/Providers[]）、OnboardingDone、HubCenterURLs
- 只在前端传入值与磁盘值不同时打印诊断日志 `[config] SaveConfig:preserved_backend_fields=[...]`
- `oldConfig` 优先从 `configCache`（内存权威值）读取，避免 Windows 文件锁导致磁盘读取失败

##### Fix 3: Hub 重连不覆盖用户手动选择的 Provider

- `gui/hub_llm_service.go`：`applyHubLLMServiceStatusToConfig` 中移除 `!a.isMaclawLLMConfiguredWithConfig(*cfg)` 条件
- 新逻辑：只在 `MaclawLLMCurrentProvider == ""` 或 `== hubServiceProviderName` 时设置官方 provider
- 用户手动选择的第三方 provider（如"智谱编程"）不被 hub sync 覆盖
- `forceCurrentProvider=true`（仅 `ActivateRemote` 注册成功后）仍可强制切换
- 新增日志 `[hub-llm-sync] respecting user provider choice: "xxx"`

##### Fix 4: LLM Ping 瞬态容错——5 秒重试

- `gui/frontend/src/App.tsx`：LLM 首次 ping 失败后不立即弹窗，延迟 5 秒重试一次
- 重试仍失败才弹出 popup（真正的 LLM 不可用）
- 瞬态故障（hub 502、网络波动）通常在秒级恢复，5 秒重试消除误触发

##### Fix 5: 可观测性增强

- `gui/app.go`：`PatchConfigFields:done` 日志输出具体字段名 `keys=[...]`
- `gui/app.go`：`preserveBackendOwnedFields` 只在实际恢复时打印被保护的字段
- `gui/hub_llm_service.go`：hub sync 尊重用户选择时打印日志

**验收标准**：
- 模型设置面板保存不再覆盖 remote credentials/LLM provider/onboarding/log settings
- Hub 重连后用户选择的"智谱编程"不被切换为"MaClaw官方"
- Hub 瞬态 502 不触发 onboarding 弹窗
- 日志中可追踪所有 config 写入的字段名和保护触发情况
- Go 编译通过（gui/corelib/tui）


### 105. CodingSubAgent Skill 调用支持——任务感知的 Skill 选择 + 安全执行守卫

**来源**：用户需求——SubAgent 执行编码任务时应能调用 UI 优化类 skill（如 ui-ux-pro-max）等编程辅助 skill。

**根因**：SubAgent 的工具集是硬编码的 9 个精简工具（read_file/write_file/edit_file/bash/list_directory/Glob/ripgrep/edit_lines/git_diff），不包含 `manage_skill`。用户安装了 UI 优化、lint 修复等 skill，SubAgent 无法调用。

**修复**：

#### 1. 三信号融合 Skill 选择（`gui/coding_subagent_skills.go`，新文件）

- `selectRelevantSkillsForTask(taskDescription)` → top-3 最相关 skill
- 三信号融合：BM25（英文/混合）+ Bigram Jaccard（中文/CJK）+ Embedding cosine（语义）
- 取 max(三信号) 作为最终分数，阈值 0.15
- Embedding 分数减去 baseline 0.2（Gemma 300M 对无关短文本的 baseline cosine）后 clamp 到 [0,1]
- Embedding 使用 `EmbedBatch` 批量推理（50 skill ~30-80ms），而非 50 次串行 Embed
- Embedder 不可用时自动退化为 BM25 + bigram 双模式

#### 2. 动态工具注入（`gui/coding_subagent.go`）

- `BuildSystemPrompt`：检测 matched skills 后追加"可用 Skill"section（含 skill 名称、描述、必需参数）
- `BuildTools`：matched skills 非空时追加 `manage_skill` 工具定义（仅暴露 run/status action）
- `codingSubAgentDynamicToolNames`：新增动态工具白名单概念，与静态白名单并列检查
- `canonicalCodingSubAgentToolName`：扩展支持动态工具名的大小写标准化

#### 3. 安全执行守卫（`gui/coding_subagent_skills.go`）

- `executeManageSkill`：action 限制（只允许 run/status）+ skill name 限制（只允许 BM25 匹配到的 skill）
- 不允许 install/uninstall/upload/patch（SubAgent 不应改变系统状态）
- 传递 SubAgent 的 progress 回调给 skill runner（长时间 skill 有 UI 进度反馈）
- 结果分类使用 `toolManageSkill` 返回值的已知失败前缀，不使用脆弱的子串匹配

#### 4. 导出 BM25 原语（`corelib/skill/scanner.go`）

- `TokenizeSimple()` 和 `BM25ScoreSimple()` 导出为公开函数供 SubAgent 复用
- 原有内部 `tokenizeSimple()` 和 `bm25ScoreSimple()` 保持不变

#### 5. Embedder 接入（`gui/im_interrupt_handler.go`）

- 新增 `EmbedderForSubAgent()` 方法：暴露本地 Gemma 300M 模型给 SubAgent

**Token 预算**：
- 0 个 matched skill → 0 增量（SubAgent 纯编码模式，与当前一致）
- 3 个 matched skill → ~500 token（skill 列表 ~100 + manage_skill 定义 ~400）

**修改文件**：
- `gui/coding_subagent_skills.go`（新增）：Skill 选择 + 工具定义 + 执行守卫
- `gui/coding_subagent_skills_test.go`（新增）：14 个测试
- `gui/coding_subagent.go`：3 处改动（matchedSkills 字段、BuildSystemPrompt/BuildTools 增强、executeToolWithOutcome 新增 case）
- `gui/im_interrupt_handler.go`：新增 EmbedderForSubAgent 方法
- `corelib/skill/scanner.go`：导出 TokenizeSimple/BM25ScoreSimple

**验收标准**：
- task "优化登录页面 UI" + 已安装 ui-ux-pro-max → system prompt 包含 skill 列表，LLM 可调用
- task "实现数据库连接" + 无相关 skill → manage_skill 不注入，零 token 开销
- LLM 调用 manage_skill(action=install) → 被守卫拒绝
- LLM 调用 manage_skill(name="tts-to-mp3") 但该 skill 未匹配 → 被守卫拒绝
- Embedder 不可用时 → 退化为 BM25+bigram，功能不消失
- 14 个测试 + 所有现有 SubAgent 测试通过
- GUI / corelib 编译通过


### 106. 论文复现工作流模板（paper_reproduction）

**来源**：用户需求——添加一个完整的论文复现工作流，覆盖从阅读论文到远程服务器跑实验、迭代改进、生成实验报告的全流程。

**核心特征**：
- **输入驱动**：用户需先上传论文 PDF 或提供 URL
- **需要远程服务器**：用户需提供 SSH 连接信息（IP/域名、用户名、密码、工作目录可选）
- **迭代式实验**：复现基线后循环改进，直到超越论文或达到上限
- **项目化产出**：有明确的远程项目目录结构，存放源码、数据、checkpoint、结果、报告
- **完整实验报告**：对比实验、消融实验、超参数分析的原始数据和图表

**6 个阶段**：

| 阶段 | ID | ToolPolicy | NeedsConfirm | 说明 |
|------|-----|-----------|-------------|------|
| 论文深度解读 | paper_analysis | doc_only | ✅ | 精读论文提取方法、实验设置、关键数值 |
| 复现规划 | reproduction_plan | full | ✅ | 搜索源码（GitHub）、搜索数据集、确定项目结构 |
| 环境搭建与数据准备 | env_and_data | full | ❌ | SSH 连接服务器、安装依赖、下载数据 |
| 基线实验复现 | baseline_reproduction | full | ❌ | 按论文参数跑实验，对比论文数值 |
| 迭代改进 | iterative_improvement | full | ❌ | 循环修改程序直到结果超越论文或达到上限 |
| 实验报告 | experiment_report | full | ✅ | 生成完整报告含对比/消融/超参分析 |

**复现规划阶段**设为 `ToolPolicyFull`：需要 `web_search`/`web_fetch` 实际搜索 GitHub 源码和数据集下载链接。

**修改文件**：
- `corelib/workflow/types.go`：新增 `WorkflowPaperReproduction WorkflowType = "paper_reproduction"`
- `corelib/workflow/v2/templates.go`：新增 `PaperReproductionTemplate()` + 注册到 `RegisterBuiltinTemplates`
- `corelib/workflow/v2/phase_prompt.go`：新增 6 个阶段的专用 phase instructions
- `gui/frontend/src/components/ai/WorkflowDocPreview.tsx`：新增 `phaseLabels` 和 `workflowPhaseOrders` 映射

**验收标准**：
- 用户说"复现这篇论文" + 给出 URL → 工作流启动，第一阶段解读论文
- 复现规划阶段实际搜索 GitHub 和数据集链接
- 环境搭建阶段询问 SSH 服务器信息后登录执行
- 基线复现和迭代改进阶段在远程服务器通过 SSH 后台任务执行
- 实验报告包含所有实验的原始数据表和对比分析
- corelib/workflow/v2 编译通过 + TestTemplate 测试通过


### 107. RemoteCodingSubAgent——远程服务器自动编码执行器（设计）

**来源**：论文复现工作流的迭代改进阶段需要在远程 GPU 服务器上自动修改代码、跑实验、循环改进。现有 CodingSubAgent 只支持本地文件操作。

**设计目标**：与本地 CodingSubAgent 对称的远程编码执行器，精简 context、自动长时间运行、可观测。

#### 与本地 CodingSubAgent 的对比

| 维度 | CodingSubAgent（本地） | RemoteCodingSubAgent（远程） |
|------|----------------------|---------------------------|
| 工具集 | read_file/write_file/edit_file/bash/list_dir/git_diff | ssh_read/ssh_write/ssh_edit/ssh_bash/ssh_list |
| 安全边界 | 本地 projectPath 范围 | 远程 workDir 范围 |
| 运行模式 | 同步，几分钟完成 | 异步后台，数小时/天 |
| 可观测性 | 前端 SubAgent 面板（tool events） | 后台任务监控面板 + IM 通知 |
| 生命周期 | 跟随 agent loop iteration | 独立后台 goroutine，支持暂停/恢复/停止 |
| 进度汇报 | onToken/onProgress 回调 | emitEvent → 任务监控面板 + IM 推送 |

#### 工具集设计（5 个 SSH 封装工具）

```
ssh_read_file(path)           → ssh exec "cat {path}"
ssh_write_file(path, content) → ssh exec "cat > {path} << 'MACLAW_EOF'\n{content}\nMACLAW_EOF"
ssh_edit_file(path, old, new) → ssh exec python -c "import pathlib; p=pathlib.Path('{path}'); p.write_text(p.read_text().replace(old, new))"
ssh_bash(command, working_dir)→ ssh exec "cd {working_dir} && {command}"（短命令同步）/ ssh submit_task（长命令后台）
ssh_list_directory(path)      → ssh exec "ls -la {path}"
```

SubAgent 内部自动判断 ssh_bash 的命令是否为长时间训练（含 train/fit/epoch 等关键词），长命令自动用 submit_task + check_task 轮询。

#### 后台任务监控集成

RemoteCodingSubAgent 启动后注册到 GUI 的任务监控系统：
- **任务名**：`论文复现: {paper_title} - 迭代改进`
- **状态列表**：
  - `🔧 修改代码中 (exp_017)` — 当 LLM 在生成代码修改
  - `🏃 训练中 (exp_017, epoch 15/100, loss=0.342)` — 训练后台任务运行中
  - `📊 评估中 (exp_017)` — 训练完成，正在跑评估脚本
  - `💤 等待中 (下一轮改进)` — 评估完成，等待开始下一轮
  - `⏸️ 已暂停` — 用户暂停
  - `🎉 达成目标` — 超越论文指标
- **进度数据**：
  - 当前轮次 / 最大轮数
  - 累计运行时间 / 最大运行时间
  - 当前最佳指标 / 论文指标 / 目标值
  - 最近 3 轮的指标趋势

#### 异步生命周期

```
用户确认迭代参数
  → RemoteCodingSubAgent.Start(sessionID, params)
  → 注册到 BackgroundTaskRegistry
  → 后台 goroutine 开始循环
       ┌─→ 分析历史 → 生成改进方案
       │   ↓
       │   SSH 修改远程代码
       │   ↓
       │   SSH submit_task 启动训练
       │   ↓
       │   轮询 check_task（每 30s）
       │   ↓ 训练完成
       │   SSH 执行评估脚本
       │   ↓
       │   记录结果 → 判断停止条件
       │   ↓ 未达标
       └───┘
  → 达到停止条件
  → IM 通知用户
  → 等待用户响应（继续/停止/换方向）
```

#### 待实现（后续 task）

1. `gui/remote_coding_subagent.go`：核心实现
2. `gui/remote_coding_subagent_tools.go`：5 个 SSH 封装工具
3. `gui/remote_coding_subagent_monitor.go`：后台任务监控集成
4. 前端：任务监控面板中显示 RemoteCodingSubAgent 状态
5. 迭代改进阶段 `ExecMode` 改为新的 `ExecModeRemoteSubAgent`
6. IM 通知与用户交互联动


### 108. ToolPolicyFull 工作流阶段 LLM 用 write_file 产出文档——面板无内容 + 阶段产出物只有短评论文本

**来源**：用户启动专利申请工作流（`patent_application`），第一阶段 `pa_disclosure_parsing`（交底书解析与技术提炼）完成后，右侧工作流面板没有显示文档内容。LLM 将完整文档通过 `write_file` 写入磁盘（14947 字节），但面板只捕获到 889 字节的 LLM 短评论文本。

**根因**：`captureWorkflowDocAfterAgentLoop` 使用两个数据源确定阶段产出物：
1. `WorkflowDocBuffer`：累积 LLM 的 `msgContent`（纯文本输出）
2. `resp.Text`：最后一轮迭代的文本

当 `ToolPolicy=ToolPolicyFull` 时，LLM 选择调用 `write_file` 将文档写入磁盘，而不是在聊天流中输出文本。`msgContent` 只有短评论（"win32com 可用！让我..."、"文档已完整写入..."），`write_file` 的 `content` 参数从不经过 `WorkflowDocBuffer`（它只捕获 LLM text output，不捕获 tool call arguments）。

**修复**：

#### 1. `LoopContext` 新增 `WorkflowWrittenFiles` 字段（`gui/agent_loop_context.go`）

追踪 workflow agent loop 期间通过 `write_file` 成功写入的文件路径列表。

#### 2. 工具执行后追踪写入文件（`gui/im_agent_loop_tool_exec.go`）

在 `executeAgentLoopToolCalls` 中，当 `write_file` 执行成功且处于 workflow agent loop 时，从 tool call 参数中提取 `path` 字段，去重后追加到 `WorkflowWrittenFiles`。

新增 `extractWriteFilePathFromArgs()` 辅助函数。

#### 3. `captureWorkflowDocAfterAgentLoop` 新增第三数据源（`gui/im_post_loop.go`）

内容解析优先级变为：
1. `WorkflowDocBuffer`（LLM 纯文本输出累积）
2. **`WorkflowWrittenFiles`（从磁盘读取 write_file 产出的文件内容）**— 当文件内容比 buffer 更长时使用
3. `resp.Text`（最后一轮文本）
4. `resp.Error`（错误信息）

新增 `readWorkflowWrittenFiles()` 函数：读取追踪到的文件，跳过二进制/超大/空文件，拼接为阶段产出物。
新增 `looksLikeBinary()` 辅助函数：检测二进制内容。

**机制性特征**：
- **不依赖 LLM 行为**：无论 LLM 选择输出文本还是 write_file，阶段产出物都能正确捕获
- **通用**：对所有 `ToolPolicyFull` 阶段通用（专利解析、权利要求撰写、PPT 生成等），不硬编码任何阶段 ID
- **安全**：跳过二进制文件、超大文件（>100KB），总输出截断到 50000 rune
- **去重**：同一文件被 `mode=append` 多次写入时只追踪一次路径，`os.ReadFile` 读取最终完整内容

**验收标准**：
- 专利工作流 `pa_disclosure_parsing` 阶段 LLM 用 write_file 写入文档 → 面板显示完整文档内容
- `WorkflowDocBuffer` 有大量文本输出（如编码工作流的设计文档）→ 行为不变，buffer 优先
- LLM 同时输出文本 + write_file（如短摘要 + 完整文件）→ 取更长的内容作为产出物
- 二进制文件（如 .docx/.pdf）→ 跳过，不作为产出物
- GUI 编译通过
