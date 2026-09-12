# Agent 安全机制技术文档

> 适用范围：MaClaw / CodeClaw 仓库中 Agent 工具调用、代码执行、外部集成相关的安全控制。
> 编写方式：基于源码静态梳理，所有结论均标注文件路径与标识符。
> 日期：2026-09-08

---

## 1. 总览

代码中的 Agent 安全机制不是单一模块，而是分布在 **四条相互独立的链路** 上。理解这一点是理解全貌的前提——很多"看起来做了防护"的组件，实际上并不在你以为的那条链路上。

| # | 链路 | 主要入口 | 安全控制主体 |
|---|------|----------|--------------|
| A | **GUI / IM 工具执行链路** | `gui/im_tool_execution.go`、`guiapp/im_tool_execution.go` | `corelib/security` 包（Firewall 全栈） |
| B | **核心 Agent 主循环** | `corelib/agent/loop.go` | 渲染面围栏 + `ToolAuthorizer` + 语义调用授权（grant） |
| C | **服务端 API / 多租户** | `MaClawSrv/http.go`、`admin_auth.go` | 认证、鉴权、租户隔离、限流 |
| D | **Skill / 插件供应链** | `corelib/skill/`、`guiapp/skill_install_admission.go` | 安装前扫描 + 准入闸门 |

**关键结论（先行提示）**：链路 A 拥有最完整的防护栈；链路 B（真正的自主 Agent 循环）**不经过** `corelib/security` 的 Firewall，走的是另一套授权模型；链路 B 上的提示词注入防护目前**实际为零**。详见 §7 风险清单。

---

## 2. 核心安全包：`corelib/security`

这是仓库内唯一的通用安全框架包，被 A、D 两条链路共用。

### 2.1 组件地图

| 文件 | 组件 | 职责 |
|------|------|------|
| `risk_analyzer.go` | `RiskAnalyzer` | 正则风险识别，输出风险等级 |
| `policy_engine.go` | `PolicyEngine` | 按安全模式裁决 `allow/deny/ask/audit` |
| `firewall.go` | `Firewall` | 统一检查入口，编排以上两者 + 审批 + 审计 |
| `smart_approval.go` | `SmartApproval` | 用轻量 LLM 复核正则误报 |
| `session_allowlist.go` | `SessionAllowlist` | 会话级审批缓存（工具/类别/通配符） |
| `approval_flow.go` | `ApprovalManager` | 异步（IM）审批请求与超时 |
| `harness_gate.go` | `HarnessGate` | 项目级产出约束校验 |
| `denial_ledger.go` | `DenialLedger` | 连续拒绝熔断，自动暂停自主任务 |
| `audit_log.go` | `AuditLog` | JSONL 审计日志，轮转与保留 |
| `audit_redaction.go` | — | 审计落盘前敏感信息脱敏 |
| `sensitive_detector.go` | `SensitiveDetector` | 密钥/令牌模式识别 |
| `injection_guard.go` | `InjectionGuard` | 提示词注入检测（**未接线**） |
| `risk_assessor.go` | `RiskAssessor` | 47KB 规则库，主要服务 Skill 扫描 |

### 2.2 风险识别：`RiskAnalyzer`

四级风险模型（`types.go:8`）：

```go
RiskLow(0) < RiskMedium(1) < RiskHigh(2) < RiskCritical(3)
```

`Assess(toolName, args, ctx)` 遍历内置 + 自定义规则，命中多条时**取最高等级**。内置规则 `DefaultRiskPatterns` 共 17 条，按类别分布：

| 类别 | 典型规则 | 等级 |
|------|----------|------|
| `file_delete` | `recursive_delete`（`rm -rf`/`rmdir /s`/`del /f`）、`shutil_rmtree` | Critical |
| `network` | `data_exfil_curl`（curl POST/-d）、`data_exfil_wget`、`netcat` | High |
| `permission` | `chmod_777`（High）、`chown`（Medium） | High/Medium |
| `system` | `shutdown`/`reboot`（Critical）、`systemctl_stop`（High）、`kill_9`（Medium）、`env_secret`（Medium） | Critical~Medium |
| `package` | `pip_install_global`、`npm_install_global` | Medium |
| `database` | `drop_table`（Critical）、`delete_no_where`（High）、`database_execute`（High）、`database_export`（Medium） | Critical~Medium |

**风险降级机制**（`risk_analyzer.go:76`）：当上下文满足以下任一条件时，风险等级**下调一级**（`ReduceRiskLevel`）：

1. 用户消息命中 `userExplicitlyRequested()` 关键词（删除/delete/remove/`rm `/push/发布/publish/执行）；
2. `ctx.RecentApprovals` 中存在被本次工具名包含的历史批准项。

> ⚠️ 这是一处需要关注的宽松化设计：只要用户说过"删除"，后续同类高危操作的等级就会被系统性下调。

### 2.3 策略裁决：`PolicyEngine`

五种安全模式（`policy_engine.go:156` `PolicyRulesForMode`）：

| 模式 | Critical | High | Medium | Low |
|------|----------|------|--------|-----|
| `none` | allow | allow | allow | allow |
| `developer` | allow | allow | allow | allow |
| `relaxed`（**默认**） | allow | allow | allow | allow |
| `standard` | ask | ask | audit | allow |
| `strict` | deny/deny-dangerous | ask | ask | allow |

要点：

- **默认模式是 `relaxed`**。`NewPolicyEngine()` 直接返回 relaxed 规则集；`normalizePolicyEngineMode()` 对无法识别的输入也回落 relaxed。即：**不显式配置时全放行**。
- `strict` 是唯一硬阻断危险命令的模式，其 `deny-dangerous-keywords` 规则（priority 10）的 `ArgsPattern` 仅匹配 `(?i)(DROP\s+TABLE|\bsudo\s+(su\b|-i\b))`。
- 源码注释（`policy_engine.go:169-174`）明确承认：`rm -rf` **被有意从 deny 规则中移除**，因为它已不再被 `Assess()` 提升为 critical，只会在 standard 模式下落到 high → ask。
- 规则匹配为**有序优先级**：`sortRulesByPriority` 后逐条 `matchesRuleSnapshot`，首个命中即返回；无命中默认 `PolicyAsk`。
- 支持从 `.maclaw/security-policy.json` 加载项目级规则（`Firewall.LoadProjectPolicy`）。

### 2.4 统一入口：`Firewall.Check`

调用链（`firewall.go:65`）：

```
Check(toolName, args, ctx)
 ├─ analyzer.Assess()                      → RiskAssessment
 ├─ [mode == developer]                    → 直接放行 + 审计 developer_mode_allowed
 ├─ allowlist.IsApproved(sessionID, ...)   → 命中则放行（audit）
 ├─ isSessionApproved()（遗留兼容）         → 命中则放行（audit）
 ├─ policy.Evaluate(toolName, args, level) → PolicyAction
 └─ switch action:
      allow/audit → 放行
      deny        → 拒绝，返回原因
      ask         → handleAskAction()
```

`handleAskAction` 的三级降级（`firewall.go:124`）：

1. **Smart Approval**：LLM 判定 `safe` 则直接放行并写审计 `smart_approved`；`unsafe`/`unknown` 继续向下。
2. **同步 `onAsk` 回调**（CLI/GUI）：用户确认后记录**工具级**会话批准（注释说明 CLI/GUI 提示是按次调用，故不按类别批准）。
3. **异步 `ApprovalManager`**（IM）：发起审批请求，超时默认 **2 分钟**。
4. 若无任何确认通道：high/critical **拒绝**，其余放行并记 `allowed_without_confirmation_channel`。

这最后一条是正确的 fail-closed 设计——值得一提，因为下文会看到并非所有地方都如此。

### 2.5 Smart Approval（LLM 复核）

`SmartApproval.Evaluate()` 用轻量 LLM 判断正则是否误报，超时默认 **5 秒**，判定失败/超时一律返回 `Unknown` 并回落人工确认（fail-safe）。

- 系统提示词内置误报白名单：`rm -rf node_modules/`、`rm -rf dist/`、`DROP TABLE IF EXISTS temp_*`、`kill -9 <pid>`、`sudo apt install`。
- 会话级缓存：key 为 `tool + ":" + join(factors)`，命中即跳过 LLM。
- 解析 `parseSmartVerdict` 只看输出**前缀**是否 `SAFE` / `UNSAFE` / `DANGEROUS`。

> ⚠️ 该机制让一个 LLM 拥有安全放行权。其输入 `buildSmartApprovalPrompt` 包含工具名、风险等级、因子和**前 500 字符的参数**——若参数本身含注入内容，则构成对安全判定模型的间接注入面。

### 2.6 会话白名单：`SessionAllowlist`

匹配顺序（`session_allowlist.go:53`）：通配符 `*` → 精确工具名 → 类别 → **工具名子串**。

```go
// 子串匹配：已批准 "bash" → "bash_background" 也会被放行
if pattern != toolName && pattern != "" && strings.Contains(toolName, pattern) { return true }
```

TTL 通过 `ExpiresAt` 支持，但 `Firewall.recordSessionApproval` 传入 `ttl=0`，即**会话内永久有效**。`ApproveAll(sessionID, 0)` 可放行整个会话的全部工具。

### 2.7 异步审批：`ApprovalManager`

- 支持四种批准范围：`once` / `tool` / `category` / `session`。
- `FormatApprovalMessage` 生成中文审批卡片，`ParseApprovalReply` 解析用户回复。
- 解析为 **fail-closed**：空回复、纯空白、无法识别的内容一律判为拒绝。

> ✅ **已修复（2026-09-08）**：原实现的"拒绝"分支关键词列表含空串 `""`，而 `containsAny` 内部为 `strings.Contains(lower, sub)`，对空串恒为 `true`，导致该分支**永远命中**——"批准"/"yes"/"ok" 全部被解析为 `Approved=false`，审批功能形同不可用。
>
> 修复内容（`approval_flow.go`）：
> 1. 移除两个分支中的 `""` 字面量，并让 `containsAny` 主动跳过空串（防御同类问题再发）；
> 2. 短英文词 `no`/`nope`/`nah` 与 `y`/`ok`/`okay` 改为**整词匹配**（新增 `containsWord`），避免 "deploy node"、"not sure" 被 `no` 子串误伤；
> 3. 明确匹配顺序：范围词 → 否定 → 肯定（"批准同类"含"批准"、"不允许"含"允许"，顺序不可颠倒）。
>
> 回归测试见 `corelib/security/approval_flow_test.go`（21 个子用例 + 子串误判专项）。

### 2.8 熔断：`DenialLedger`

进程级连续拒绝计数器，借鉴 OpenSquilla 设计。

- 环境变量：`MACLAW_DENIAL_PAUSE=off|0|false|no|disabled` 关闭；`MACLAW_DENIAL_PAUSE_THRESHOLD=N` 设阈值（默认 **5**）。
- `RecordDeny()` 累加，达到阈值置 `paused`；`RecordAllow()` 清零连续计数。
- `PauseBlockMessage()` 供 Agent 循环感知并停止自主行为；`ClearPause()` 供运维恢复。

这是防御"Agent 被诱导反复尝试越权"的有效机制。

### 2.9 审计：`AuditLog`

- 格式：JSONL，按日期分文件 `audit-YYYY-MM-DD[.N].jsonl`。
- 轮转：单文件 **50MB**，序号递增；保留 **30 天**（`cleanOldLogsLocked` 在每次 rotate 时执行）。
- **落盘前强制脱敏**：`Log()` 先 `RedactedAuditCategories(entry)` 分类，再 `SanitizeAuditEntry(entry)` 清洗，命中则标记 `SensitiveDetected` + `SensitiveCategories`。
- 查询：`Query(AuditFilter)` 支持时间范围、动作、工具名、风险等级过滤，并用文件名日期做预过滤。

---

## 3. 链路 A：GUI / IM 工具执行

Firewall 的实际接线点：

| 位置 | 说明 |
|------|------|
| `gui/im_tool_execution.go:1098` | IM 场景工具执行前 |
| `guiapp/im_tool_execution.go:1097` | 同上（另一 GUI 变体） |
| `cmd/maclaw-tool/security_check.go:118` | CLI 安全检查子命令 |

执行前的完整前序校验（`gui/im_tool_execution.go:1075-1103`）依次为：

```
参数归一化 → 必填校验 → 参数值校验
   → [call_mcp_tool] preCheckMCPToolArgsForAgentLoop
   → 外部压缩包解压审批（emitArchiveExternalApprovalIfNeeded）
   → 注册工具审批面板（emitRegisteredToolApprovalAgentViewIfNeeded）
   → Firewall.Check   ← 安全策略裁决
   → 执行
```

即：Firewall 位于**业务级审批之后**，是执行前的最后一道通用闸门。

### 项目约束：HarnessGate

`HarnessGate` 在工具执行**之后**校验产出是否符合项目约束，配置来自 `.maclaw/project-constraints.json`：

- `forbidden_paths`：禁改路径（glob）
- `required_files`：必须产出的文件（如测试文件）
- `forbidden_imports`：文件名级禁依赖匹配

违规不阻断执行，而是生成 `[产出违规报告]` 回灌给模型，属于**软约束 + 模型自律**。约束文件缺失或 JSON 非法时静默跳过（fail-open）。

---

## 4. 链路 B：核心 Agent 主循环

> ⚠️ `corelib/agent` 包**仅**在 `conversation_memory.go` 中引用 `corelib/security`（用于脱敏）。`loop.go` 不使用 Firewall，也不使用 RiskAnalyzer / PolicyEngine。

主循环的安全控制是另一套模型，按执行顺序：

| 阶段 | 机制 | 位置 |
|------|------|------|
| 1 | **渲染面围栏** `toolCallNameWasRendered()`：精确字符串匹配本次请求已渲染的工具定义，未命中即拒 | `loop.go:2655` |
| 2 | **名称级策略** `authorizeLoopTool()` → `ToolAuthorizer.IsToolAllowed(name)` | `loop.go:2803` |
| 3 | **调用级策略** `ToolCallAuthorizer.IsToolCallAllowed(name, argsJSON)` | `loop.go:572`（接口） |
| 4 | **执行器内部** `clientsecurity.EnforceConfig` + `ValidateToolCallByPolicyWithApproval` | `agentservice/core_agent_executor.go:2240+` |

### 4.1 工具申请（Petition）与授权（Grant）

- **Petition**：`ToolCallPetitioner.PetitionToolCall(name)`（`loop.go:492`）。注意它**不直接放行**——返回 true 只把拒绝文案换成"请重新发起"，本次仍记为 Error，工具在**下一轮**扩面后才真正执行。
- **Grant**：一次性语义授权 `InvocationGrant{Token, Nonce, Signature, ExpiresAt, Scope, ParameterAuthorization}`（`corelib/tool/semantic_invocation.go:109`），TTL 默认 **10 分钟**。状态机 `accepted/consumed/revoked/expired/invalid`，SQLite 实现用 `UPDATE ... WHERE state='issued' AND expires_at > ?` 做线性化消费点。
- **防误读**：`succeededToolNames` + `consumedGrantToolCallDeniedMessage` 防止模型把"额度已用尽"误读为失败。
- **拒绝语义**：策略拒绝**不**写 WorkingState、不触发 `OnToolExecuted`，避免留下"未关闭项"引发伪造的 finish-nudge。

### 4.2 权限粒度

| 维度 | 载体 |
|------|------|
| 工具名 | `ToolPolicy.Allowed`（`codingagent/codingagent.go:47`），角色 worker/explorer/reviewer |
| 工具名 + 参数 | `IsToolCallAllowed`：拦截 `web_fetch` 的 `save_path/output/dest`、`database` 的 `action=execute`、bash/ssh 命令串 |
| 提示档位 | `PromptProfileToolAuthorizer.IsToolAllowedForPromptProfile` |
| 参数字段/目标 | `ParameterAuthorization{AllowedFields, AllowedTargets, AllowedArtifactIDs, Digest}` |
| 会话/主体 | `InvocationScope{RootTaskID, PlanID, SessionID, TurnID, PrincipalID, ToolSnapshotID}` |

### 4.3 风险分级模型（链路 B 自身）

- 语义目录四级：`EffectReadOnly / EffectLocalMutation / EffectExternalEffect / EffectSensitive`（`corelib/tool/semantic_catalog.go:23`）。
- 工作流档位：`DocOnly / Planning / Full / OpsControlled`，配套白名单表。
- 运维风险：`OpsRiskLevel{L0..L4}`、`OpsApprovalRequirement{none/user/admin/single/double}`、`OpsRiskDecision{...auto_execute/deny}`。

### 4.4 执行侧熔断

`hardStopSameToolFailures = 12`、`hardStopNoProgressIterations = 5`（`loop.go:912/923`）。

### 4.5 `confirmation_status.go` 的真相

该文件只有两个状态：`Unknown("")` 与 `Pending("pending")`，**没有 approved/denied**。它承载的是**任务执行前的意图级确认**（由 `ShouldRequireExecutionConfirmationForIntent` 判定，仅 coding/ssh 意图需确认），且该函数在当前代码库中**无调用方**，属遗留代码。不要把它误认为工具审批的状态机。

### 4.6 `selfconfirm.go` 的真实作用

与工具审批无关。它用 `ConfirmRequestRe` / `SelfAnswerRe` 检测 LLM **自问自答**，并用 `TruncateAtConfirmationBoundary` 截断（截断后不足 50 rune 则放弃）。这是一道**反绕过**逻辑，防止模型伪造用户确认。

---

## 5. 命令执行与文件系统管控

### 5.1 沙箱边界（实际强度）

`codingruntime` **不是 OS 级沙箱**——无 namespace、无 cgroup、无 rlimit、无 seccomp。它是"执行账本 + 冻结策略快照"层：

- `PolicySnapshot{ProjectRoot, RemoteTarget, Mode, ReadOnly, WriteSet, WorkspaceIsolated, ...}`（`codingruntime/types.go`）
- `PolicySnapshot.ReadOnly` / `WorkspaceIsolated` 是**自我声明**，源码注释明确写"hosts must enforce this same policy at execution time"。
- 进程层唯一约束：context 超时 + 进程树 kill（`corelib/tool/process_tree*.go`，Unix `Setpgid`）。**网络出口无任何限制**。
- 执行前后只读工作区探针（`git rev-parse HEAD` / `git status --porcelain`）；`FinalWorkspaceGateRequired` 要求前后探针必须有差异，否则 `final_workspace_unchanged` 阻断。本地非 Git（或零提交）工作区在执行前由 `codingruntime.EnsureLocalGitBaseline` 自动 `git init` + 空基线提交，仅当 Git 不可用或初始化失败时才以 `workspace_before_probe_failed` 阻断。
- 子任务强制继承父策略：`validateReadOnlyChildSpec` 要求子任务 `ReadOnly` 且 `ProjectRef`/`Mode`/`ProjectRoot` 与父一致。
- 租约 `LeaseDuration` 默认 **10 分钟**。

### 5.2 路径穿越防护

这是全仓库实现质量最高的部分，有多套独立实现：

| 位置 | 手法 |
|------|------|
| `database/manager.go:663` `resolvePath` | **最强**：先词法 `filepath.Rel` 校验，再 `resolvePathSymlinks` 逐段 `Lstat`+`Readlink`（深度上限 40，不存在路径回溯最深祖先）二次校验，报 `path_denied: path escapes the owner workspace` |
| `archiveutil/safe.go` | `canonicalEntry`（绝对路径/`..`/深度/Windows 保留名）、`safeJoin`、`ensureNoSymlinkParent`、`rejectSymlinkAncestors`、`ValidateExtractedDirectory` |
| `toolresult/read.go:149` | `validateResolvedStorePath`：`EvalSymlinks` + `isUnderRoot`，且 open 后再复验 |
| `knowledge/image_assets.go` | `os.OpenRoot` + `EvalSymlinks` |
| `agentservice/fs_security.go` | `secureRemoveAllWithin`、`secureMkdirAll`(0700) |

解压限额（`archiveutil/types.go`）：`MaxFiles=10000`、`MaxFileBytes=1GB`、`MaxTotalBytes=4GB`、`MaxDirectoryDepth=64`、`AllowSymlinks=false`。

写集校验（`codingruntime/write_set.go`）：`NormalizeWriteSet` 禁 `*?[`、`~`、`${`、`$(`。

`codingagent` 侧：`resolveBashWorkingDir` + `ensurePathWithinBase` 保证 bash 的 `working_dir` 落在 `c.workspace` 内。

### 5.3 命令拦截规则

| 守卫 | 位置 | 拦截内容 |
|------|------|----------|
| `RejectRawSSHCommand` | `tool/ssh_command_guard.go` | ssh/scp/sftp/远程 rsync |
| `RejectBroadBrowserKillCommand` | 同上 | 广谱杀浏览器进程 |
| `RejectBrowserSideEffectHTTPCommand` | 同上 | curl/wget/iwr + 非幂等方法 + cookie/authorization/csrf |
| `RejectShellBrowserAutomationCommand` | 同上 | playwright/puppeteer/selenium/CDP |
| `RejectShellDatabaseCLI` | `tool/sql_command_guard.go` | mysql/psql/sqlcmd 等 |
| `isHighRiskOpsCommand` | `workflow/v2/types.go` | `rm -rf /`、`mkfs`、`dd if=`、`format c:`、`> /dev/sd` 硬拒绝 |

这些守卫均带 `hasNested*` / `shellLikeFields` / `nestedShellCommand` 递归破解嵌套 shell，设计较用心。

**四个 bash 入口的守卫一致性**：`gui/im_tools_local.go:132`、`guiapp/im_tools_local.go`、`corelib/tool/local_background.go:103`、`corelib/agent/tools_local.go`。
其中 `corelib/agent` 入口原先漏掉 `RejectShellBrowserAutomationCommand`（其余三处均有），已于 2026-09-08 补齐并附回归测试（`corelib/agent/tools_local_guard_test.go`）。

Windows 侧 `NewWindowsShellCommand` 用 `SysProcAttr.CmdLine` 逐字传递，**不做转义**。

### 5.4 高危能力开关

| 能力 | 开关 | 默认 |
|------|------|------|
| 本地 bash | `CoreAgentExecutor.AllowLocalBash` | 宿主决定；mobile 显式 false |
| SSH 直连 | `AllowDirectSSH`；`canUseSSH()` | hub 移动侧 false |
| SSH 后台任务 | `sshtool.requireBackgroundTaskPolicyOwner` | **fail-closed**（PolicyOwnerID 空即拒） |
| SSH 主机校验 | `remote/ssh_dial.go:59 sshHostKeyCallback` | ⚠️ 无 pin/known_hosts 时回退 `ssh.InsecureIgnoreHostKey()` |
| Computer Use | `AppConfig.ComputerUseEnabled` | **true** |
| 像素点击 | `computeruse.DefaultConfig.AllowPixelClick` | false；`TargetApps` 空 = 允许所有窗口 |
| 桌面控制发布 | `TrustedComputerUse` 非 nil 才发布 | 默认不发布 |
| bash 超时 | `ResolveBashTimeout` | 240–600s |

---

## 6. 服务端与供应链安全

### 6.1 认证方式

| 方式 | 凭据 | 生命周期 |
|------|------|----------|
| Admin 静态密钥 | `X-MaClaw-Admin-Secret`，`subtle.ConstantTimeCompare` | 不过期 |
| Admin 会话令牌 | `mca_` + base64url(32B CSPNG)，落盘仅存 SHA-256 | 12h，最多 20 会话 |
| 初始化引导 | `MACLAW_ADMIN_SETUP_TOKEN` | 一次性 |
| 用户 API 令牌 | api_key + api_secret；**scrypt(N=32768,r=8,p=1) + 16B 盐 + pepper**；令牌为 `base64url(claims).HMAC` | 12h |
| 设备配对码 | 6 位数字（`crypto/rand`） | 30min，单次消费 |
| A2A Hub | 单一静态 Bearer | 不过期 |

管理员密码用 `bcrypt.DefaultCost`，最小长度 10（**不要求字符类别**）。无账号锁定，仅有 IP 级限流。

### 6.2 鉴权与租户隔离

- 用户侧唯一中间件：`withPrincipal`（`http.go:1772`）→ 注入 `Principal{TenantID, UserID, Roles}`。
- Admin 侧：`withAdmin`（`http.go:1753`）只校验"是有效 admin 会话"或 root secret，**不区分角色**；角色校验由各 handler 自行调用 `requireAdminOwner`（owner）等完成。
- 因此判断一个 admin 接口的真实权限级别，必须**同时看路由中间件和 handler 内联检查**。

**知识库授权接口的权限模型（读 operator / 写 owner，为有意设计）**：

| 接口 | 方法 | 角色 |
|------|------|------|
| `.../knowledge-access/tenants/{t}/users/{u}` | GET | operator |
| `.../knowledge-access/tenants/{t}/users/{u}/resolve` | GET | operator |
| `.../knowledge-access/cross-tenant` | GET | operator |
| 上述路径的 PUT / DELETE / POST | — | **owner** |

三条证据支持"故意为之"而非遗漏：
1. 两个读接口（GetUser、ResolveUser）行为一致，均不调 `requireAdminOwner`；
2. `openapi.go:390` `isOwnerOpenAPIRoute` 只登记了该路径的**写方法**，读方法按默认规则落为 operator，即对外契约就是如此；
3. `http_test.go:1165-1169` 的路由角色断言表同样只列写方法。

> 初版文档曾将 GetUser 缺 `requireAdminOwner` 列为 P1 越权缺陷，复核后已撤销：读接口对 operator 开放属于合理的"读低写高"权限模型，且改动会使代码与已发布的 OpenAPI 契约冲突。**若产品上确需收紧，应同步修改 `isOwnerOpenAPIRoute` 与 `http_test.go` 断言表，三者一起改。**
- 知识库跨租户需**两个条件同时满足**：全局开关 `knowledge_access_cross_tenant` + `SetUser` 校验；且 `ResolveForUser` 末尾无条件跑 `filterKnowledgeScopesByTenant` 兜底。
- 硬件绑定 key = `TenantID \x00 UserID \x00 clientID`，`activate()` 会先把其他 owner 的同 clientID 绑定打墓碑。

### 6.3 限流

| 实例 | 阈值 | Key |
|------|------|-----|
| `authLimiter` | 20/分钟 | `admin-login:{ip}:{user}`、`{ip}:{apiKey}` |
| `databaseAdminLimiter` | 30/分钟 | admin 数据库端点 |
| `devicePairLimit` | 6/分钟 | `device-pair:{ip}` |

失败阈值 5，封禁 `1min << (count-5)`，上限 15min。

运行时限流 `MACLAW_RUNTIME_RATE_LIMIT_RATE` **默认 0 = 不限**（源码注释明示为向后兼容）。

### 6.4 MCP / A2A

- **MCP 用户自助接入，无需管理员审批**。`MCPServerCreateInput` 支持 `AuthSecret`/`Headers`/`Env`/`Command`/`Args`，凭据随用户配置落盘。本地 `kind:"local"` 会拉起子进程，**命令无白名单**。仅 API 出参用 `sanitizeMCPServerViewForAPI` 脱敏。
- **A2A 信任模型最弱**：单一静态 Bearer，身份仅为字符串 ID，`AuthorID` 为自声明字段，无请求签名、无 mTLS、无 nonce/时间戳。

### 6.5 Skill 供应链

**有的**：
- `skill/security_scanner.go` `ScanInstallStaged()` 安装前执行，强制把 `TrustLevel` 降为 `community` 再评。
- `runStaticFileScan()` 规则覆盖 command_injection / prompt_injection / 密钥 / download-execute。
- 可选 LLM 复审 `agentScan()`。
- 准入闸门 `guiapp/skill_install_admission.go` `admitManualSkillInstall()`：缺报告按 Critical 拦，High/Critical 弹确认。
- `package_manifest.go` `VerifyPackageIntegrity()` 提供 SHA256 清单校验（防篡改/目录穿越/软链）。

**没有的**：
- **无数字签名、无作者身份验证、无来源白名单**。
- 存在绕过开关：`isRiskGuardrailOffMode()` 与 `isSecurityDeveloperMode()` 直接放行仅记审计；`skillInstallAuditOnlyMarketplaceSource()` 对市场来源只记录不拦截。

---

## 7. 已识别风险清单

按严重程度排序，均已在源码中核实。

### P0

| # | 问题 | 位置 |
|---|------|------|
| 1 | **提示词注入防护完全未接线**。`InjectionGuard` 全仓库仅在自身测试与定义中出现，声明的 4 个检查点（用户消息、工具结果、web_fetch、read_file）一个都没接上 | `corelib/security/injection_guard.go` |
| 2 | **默认安全模式为 relaxed**（全风险等级 allow）。不显式配置等于无策略 | `policy_engine.go:23,54` |
| 3 | **脱敏只在落盘时**。发往 LLM 的载荷未脱敏，密钥仍会出网 | `conversation_memory.go:1718` |
| 4 | **外部内容零隔离**。网页/文档/IM 内容与用户指令同层拼接，无不可信标注、无边界分隔符 | `agent/auto_fetch.go` 等 |

### P1

| # | 问题 | 位置 |
|---|------|------|
| 5 | ~~`ParseApprovalReply` 空串缺陷导致审批回复全部判为拒绝~~ **✅ 已修复** | `approval_flow.go` |
| 6 | `sshHostKeyCallback` 无 pin/known_hosts 时关闭主机校验（MITM 风险） | `remote/ssh_dial.go:93` |
| 7 | ~~`tools_local.go` `ToolBashWithContext` 漏调 `RejectShellBrowserAutomationCommand`，可建立第二个浏览器控制面~~ **✅ 已修复** | `agent/tools_local.go` |
| 8 | `codingagent.ToolPolicy.Allows` 对 **worker 且 `Allowed==nil` 返回 true**（fail-open），仅 explorer/reviewer fail-closed | `codingagent.go` |
| 9 | knowledge 单源操作（`GetSource`/`UpdateSourceMetadata`/`DeleteSource`/`EnableSource`）**直接透传底层 store，未做租户校验** | `knowledge_access.go:682-702` |
| 10 | ~~`handleAdminKnowledgeAccessGetUser` 未调 `requireAdminOwner`~~ **❌ 经复核为设计如此，非缺陷**——见 §6.2 说明 | `http_knowledge_access.go:123` |
| 11 | `experience/safety.go` 的 `dangerousCommandPatterns` **未接入 bash 执行链**，是死规则 | `experience/safety.go` |

### P2

| # | 问题 | 位置 |
|---|------|------|
| 12 | `adminSetupTokenValid` 在未设 `MACLAW_ADMIN_SETUP_TOKEN` 时返回 `true`，初始化端点裸奔 | `admin_auth.go` |
| 13 | `MACLAW_CREDENTIAL_PEPPER` 为空时仍允许，secret 退化为无 pepper 的 scrypt | `token.go` |
| 14 | 正则式脱敏/拦截可被大小写变形、换行拆分、变量拼接、脚本文件绕过 | 全局 |
| 15 | `computeruse` `TargetApps` 空即放行全窗口；`AllowPixelClick` 可运行时置 true | `computeruse` |
| 16 | 无 rlimit / 网络出口限制；`archiveutil` 为可信 bundle 保留 `AllowSymlinks` 口子 | `codingruntime`、`archiveutil` |
| 17 | `tools_local.go:1895` `ResolvePath` 对 `working_dir` 无逃逸校验，与 `agentservice.ensurePathWithinBase` 不一致 | `agent/tools_local.go:1895` |
| 18 | 风险降级机制：用户说"删除"即系统性下调后续高危操作等级 | `risk_analyzer.go:76` |
| 19 | Skill 无签名，且开发者模式/护栏关闭模式可一键绕过全部扫描 | `skill/` |

---

## 8. 改进建议（按投入产出排序）

> 详细的分阶段执行计划、验收标准与待决策问题见
> [`agent-security-remediation-plan.md`](./agent-security-remediation-plan.md)。

1. **接线 InjectionGuard**：把它接到 `corelib/agent/loop.go` 的工具结果回灌点与 `auto_fetch` 链路。这是投入最小、收益最大的一项——检测器已经写好且有测试，只差调用。
2. **把默认安全模式改为 `standard`**：`relaxed` 作为默认值使整套策略引擎在生产中近乎空转。
3. ~~**修 `ParseApprovalReply` 空串缺陷**（§2.7）~~ ✅ 已完成（含回归测试）。
4. **补齐 SSH 主机校验默认值**，默认拒绝而非 `InsecureIgnoreHostKey()`。
5. **统一 `ToolPolicy.Allows` 为 fail-closed**，消除 worker 角色的隐式全放行。
6. **为外部内容加不可信边界**：在网页/文档/IM 内容外包一层显式标记（如 `<untrusted source="web_fetch">`），并在 system prompt 中声明其不可作为指令来源。
7. **给高风险能力加运行时审计告警**：`ComputerUseEnabled=true` 与 `AllowPixelClick` 运行时可变更，值得纳入审计。

---

## 附录 A：关键文件索引

```
corelib/security/                    核心安全框架包（链路 A、D 共用）
  ├─ risk_analyzer.go               风险识别（17 条内置规则）
  ├─ policy_engine.go               5 种安全模式裁决
  ├─ firewall.go                    统一检查入口
  ├─ smart_approval.go              LLM 复核误报
  ├─ session_allowlist.go           会话审批缓存
  ├─ approval_flow.go               异步审批（IM）
  ├─ harness_gate.go                项目产出约束
  ├─ denial_ledger.go               连续拒绝熔断
  ├─ audit_log.go / audit_redaction.go   审计与脱敏
  ├─ injection_guard.go             注入检测（未接线）
  └─ risk_assessor.go               规则库（服务 Skill 扫描）

corelib/agent/loop.go                主循环（链路 B，不走 Firewall）
corelib/agent/confirmation_store.go  意图级执行前确认（TTL 2h）
corelib/agent/selfconfirm.go         反自问自答绕过
corelib/tool/semantic_invocation.go  语义调用授权 grant（TTL 10min）
corelib/tool/ssh_command_guard.go    SSH / 浏览器命令守卫
corelib/tool/sql_command_guard.go    数据库 CLI 守卫
corelib/archiveutil/safe.go          解压路径穿越防护
corelib/database/manager.go:663      最强的路径 + 符号链接校验
corelib/codingruntime/               执行账本与策略快照（非 OS 沙箱）

gui/im_tool_execution.go:1098        Firewall 接线点（IM）
guiapp/im_tool_execution.go:1097     Firewall 接线点（GUI）
cmd/maclaw-tool/security_check.go    Firewall 接线点（CLI）

MaClawSrv/admin_auth.go             管理端认证
MaClawSrv/http.go:1772              withPrincipal 中间件
MaClawSrv/knowledge_access.go       租户隔离
MaClawSrv/http_auth_limiter.go      认证限流
```

## 附录 B：配置项速查

| 环境变量 / 配置 | 默认值 | 作用 |
|-----------------|--------|------|
| `MACLAW_DENIAL_PAUSE` | 启用 | 关闭连续拒绝熔断 |
| `MACLAW_DENIAL_PAUSE_THRESHOLD` | 5 | 熔断阈值 |
| `MACLAW_ADMIN_SECRET` | — | Admin 静态密钥 |
| `MACLAW_ADMIN_SETUP_TOKEN` | 空（=不校验） | 初始化引导 |
| `MACLAW_CREDENTIAL_PEPPER` | 空（允许） | scrypt pepper |
| `MACLAW_RUNTIME_RATE_LIMIT_RATE` | 0（不限） | 运行时限流 |
| 安全模式（`SecurityPolicyMode`） | `relaxed` | `none/developer/relaxed/standard/strict` |
| `.maclaw/security-policy.json` | 无 | 项目级策略规则 |
| `.maclaw/project-constraints.json` | 无 | 项目产出约束 |
