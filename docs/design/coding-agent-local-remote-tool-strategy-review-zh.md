# 本仓编程 Agent 本地 / 远程工具策略 Review

> 范围：`D:\workprj\aicoder` 自有代码。**不涉及** Grok Build / Codex / Pi（那三份见
> `docs/design/grok-build-tool-routing-survey-zh.md` 与 `docs/design/agent-tool-routing-survey-codex-pi-zh.md`）。
> 标记约定：✅ = 本人逐字核对过源码；🟡 = 由间接证据推断或未穷举。

---

## 0. 一句话结论

本仓的"本地 vs 远程"**不是一个统一的路由维度，而是两套彼此独立的机制叠在一起**：

| 层 | 位置 | 本地 / 远程是否对称 | 结论 |
|---|---|---|---|
| **持久化运行时层** | `corelib/codingruntime` | **对称**（同一 `Runner`，mode 只是 `PolicySnapshot` 的一个字符串字段） | ✅ 设计干净，是真正的抽象层 |
| **模型工具表面层** | `corelib/agentservice`（MaClawSrv）、`guiapp`（GUI）、`tui` | **极不对称**，且**三个主机各写了一遍** | ⚠️ 这是主要技术债 |

最关键的一处不对称（MaClawSrv）：

- **本地** coding runtime：模型拿到**完整宿主工具表面**（28+ 个 spec）+ `spawn_coding_agent`；
- **远程** coding runtime：模型只拿到 **1 个工具** `ssh`，且该工具 12 个 action 里只有 `exec` 真的能跑。

也就是说：**远程模式的"工具策略"是"全量定义 + 调用时拒绝"，而不是"裁剪定义"**。这正是本仓在调查 Grok/Codex 时反复讨论的那个分野——Codex 用 `ToolExposure::Hidden` 把不暴露的工具从定义里摘掉，本仓远程模式则是把 12 个 action 全发给模型、再在执行时逐个打回。

---

## 1. 先分清：本仓至少有 4 个不同的"本地/远程"实现

这是 review 的第一发现，也是最容易误判的地方。同一个词"remote"在本仓指向 4 处互不相干的代码：

| # | 实现 | 入口 | "远程" 的含义 |
|---|---|---|---|
| 1 | MaClawSrv 持久化远程编码运行时 | `corelib/agentservice/coding_runtime_remote.go` | 已验证的 SSH 会话上跑 durable coding attempt |
| 2 | GUI 远程编码子代理 | `guiapp/remote_coding_subagent.go` | 6 个 `ssh_*` 包装工具 |
| 3 | GUI 持久化远程运行时适配 | `guiapp/coding_runtime_adapter.go:354-381` | 用 #2 的 `RemoteCodingSubAgent` 填 `codingruntime` 的 `Mode:"remote"` |
| 4 | 第三方 CLI 会话 | `corelib/remote/tool_catalog.go` | claude / codex / opencode / iflow / kilo / codebuddy 六个外部 CLI over PTY |

第 4 项容易被误读成"远程工具路由"，其实它是**远程 Agent 会话托管**（把第三方 CLI 拉起来、在 Hub 可见），与本报告的"本地/远程工具策略"无关。✅ 证据：`corelib/remote/tool_catalog.go:23-64` 的 `BuiltinToolInfos` 里每条都是 `BinaryName` / `SupportsProxy` / `SmokeHint`，语义是"装哪个二进制、怎么冒烟"。

TUI **只有本地**（✅ `tui/workflow_v2_init.go:158`、`tui/coding_runtime_child.go:150` 硬编码 `Mode: "local"`）。

---

## 2. 持久化运行时层：`corelib/codingruntime` —— 本地/远程对称

### 2.1 模式判别：只有一个入口字符串

✅ `corelib/agentservice/coding_runtime_remote.go:37-42`：

```go
func isExplicitRemoteCodingRuntimeRequest(req ExecuteRequest) bool {
	if req.Message.Metadata == nil || strings.TrimSpace(req.Message.Metadata[metaCodingRuntimeMode]) != "remote_workflow" {
		return false
	}
	return strings.TrimSpace(req.Message.Metadata[metaCodingRuntimeWorkflowID]) != "" && strings.TrimSpace(req.Message.Metadata[metaCodingRuntimePhaseID]) != "" && req.MutationScope == v2.MutationScopeProject
}
```

✅ `corelib/agentservice/core_agent_executor.go:862-867` 是对称的 `isExplicitLocalCodingRuntimeRequest`（`"local_workflow"`）。

✅ 分发点唯一，`core_agent_executor.go:531-545`：

```go
	if isExplicitRemoteCodingRuntimeRequest(req) { ... return e.executeRemoteCodingRuntime(ctx, req, store) }
	if !isExplicitLocalCodingRuntimeRequest(req) { return e.executeDirect(ctx, req) }
	store := e.getCodingRuntimeStore()
	...
	return e.executeLocalCodingRuntime(ctx, req, store)
```

三选一、互斥、且**必须同时满足 `MutationScope == MutationScopeProject`**——缺任何一个 metadata 就退化为 `executeDirect`（无 durable ledger）。这是 fail-closed 的好设计。

### 2.2 模式被冻结进 `PolicySnapshot`，不可事后放宽

✅ `corelib/codingruntime/types.go:117-142`，`PolicySnapshot` 同时带 `ProjectRoot` / `RemoteTarget` / `Mode` / `ReadOnly`。

✅ 本地：`core_agent_executor.go:770`
```go
policy := codingruntime.PolicySnapshot{ProjectRoot: strings.TrimSpace(req.Instance.Workspace), Mode: "local", FinalWorkspaceGateRequired: true}
```
✅ 远程：`coding_runtime_remote.go:338`
```go
policy := codingruntime.PolicySnapshot{ProjectRoot: target.WorkDir, RemoteTarget: identity, Mode: "remote", FinalWorkspaceGateRequired: true}
```

✅ `corelib/codingruntime/executor.go:180-192`：`Runner` 在开新 Attempt 前比对 `PolicyDigest` 与上一次持久化的冻结策略，不一致直接 `ErrPolicyMismatch`。**恢复路径不可能悄悄降权**。

### 2.3 远程身份：非秘密、可绑定、可重放校验

✅ `corelib/codingruntime/remote_identity.go:56-64`：

```go
	canonical := normalized.User + "@" + normalized.Host + ":" + strconv.Itoa(normalized.Port) + "\n" + normalized.WorkDir + "\n" + normalized.HostKeyFingerprint
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("sha256:%x", sum[:]), nil
```

注释明写 `It must not be used as a credential or as a replacement for SSH host-key verification.`
✅ 且 `NormalizeRemoteTarget` 强制 `HostKeyFingerprint != ""`（`remote_identity.go:47-49`）——**没有 host key pin 的远程目标直接拒绝**。这比很多 CLI agent 的 `StrictHostKeyChecking=no` 强。

### 2.4 工作区探针：接口对称，实现极不对称

两者都实现同一个 `WorkspaceProber`（`git rev-parse HEAD` + `git status --porcelain=v1 -uall`），但：

| | 本地 | 远程 |
|---|---|---|
| 实现 | `local_workspace.go:69-103` | `coding_runtime_remote.go:160-183` |
| 机制 | `exec.CommandContext(ctx, "git", ...)` + `cmd.Dir = projectPath` | 往**交互式 PTY 会话** `WriteInput` 一段 shell，再 `WaitForOutput` 抓回显 |
| 输出可信度 | 子进程 stdout，干净 | 需 nonce 标记 + `LastIndex` 取最后一帧，绕开 PTY 命令回显 |
| 失败语义 | `git` 不在 PATH 即失败 | 会话死/目标变/fingerprint 变**全部 fail-closed，不重连** |

✅ 远程的防伪造注释（`coding_runtime_remote.go:185-204`）写得非常清楚：

```go
// serviceRemoteProbeMarkers creates a fresh delimiter pair for one fixed
// read-only probe. PTY shells normally echo the submitted command, so a
// fixed marker would occur before the actual Git output and can be parsed as
// a fabricated result. A per-probe nonce plus a closing marker lets the
// parser select the final, command-produced frame without trusting terminal
// echo or repository-controlled filenames.
```

✅ 且显式拒绝重连（`coding_runtime_remote.go:246-250`）：

```go
// serviceRemoteSSHExecReadOnly does not call sshtool.SSHExec because that
// generic helper reconnects a dead session. Recovery must never silently
// bind a task to a newly connected machine.
```

**评价**：远程探针的语义正确性靠 nonce 兜住了，但代价是**它本质上在屏幕抓取一个 PTY**。这是本仓远程模式最大的工程脆弱点——本地是 `os/exec`，远程是 terminal scraper。

### 2.5 写集合：跨模式天然隔离

✅ `corelib/codingruntime/write_set.go:427-447`：

```go
func sameWriteScope(left, right WriteScope) bool {
	leftMode := strings.ToLower(strings.TrimSpace(left.Mode))
	rightMode := strings.ToLower(strings.TrimSpace(right.Mode))
	if leftMode != rightMode {
		return false
	}
	...
	// POSIX remote paths are case-sensitive; local Windows paths retain the
	// historical case-insensitive comparison used by the admission lock.
	projectEqual := leftProject == rightProject
	if leftMode != "remote" {
		projectEqual = strings.EqualFold(leftProject, rightProject)
	}
	...
	if leftMode == "remote" {
		return left.RemoteTarget == right.RemoteTarget
	}
	return true
}
```

本地与远程**永不共享写锁**（mode 不等即 false），远程还额外按 `RemoteTarget` 区分不同机器。✅ ✅ 远程强制绝对路径 POSIX、禁止 `/`（`write_set.go:247-260`），且 `Mode=="remote"` 时 `RemoteTarget` 必填。

### 2.6 完成闸门：本地远程一致，且不允许"模型自述成功"

✅ `corelib/codingruntime/executor.go:331-357`：writer 完成后必须再探一次工作区，与 baseline 不同才允许 `completed`；相同则 `TaskBlocked` + `final_workspace_unchanged`：

```go
					} else {
						result.Status = TaskBlocked
						result.SideEffectState = SideEffectObserved
						result.ErrorCode = "final_workspace_unchanged"
						result.ErrorSummary = "writer completion requires observable workspace change evidence or verified no-change evidence"
					}
```

唯一例外是"宿主质量门已验证的无变化"，且**必须是成对出现的 digest**（`executor.go:421-432` 的 `noChangeEvidenceAccepted`：`verified_no_change` evidence 的 digest 必须等于 `NoWorkspaceChangeEvidenceDigest`）。模型散文不能过关。✅

---

## 3. 模型工具表面层：本地 / 远程**极不对称**

### 3.1 MaClawSrv（`corelib/agentservice`）

#### 基线：本地 = 全表面

✅ `core_agent_executor.go:1730-1786` `coreToolSpecs()` 列出 28 个 spec：
`record_audio, bash, ssh, ask_user, task, manage_schedule, im_message, send_file, send_to_im, knowledge_search, knowledge_image_search, knowledge_context_pack, knowledge_export, knowledge_import_package, knowledge_import_share, knowledge_import_directory, knowledge_import_files, knowledge_save_url, knowledge_save_text, memory, read_file, read_document, read_tool_result, write_file, edit_file, list_directory, web_search, web_fetch`
再加 `sharedHostToolSpecs()` 与 `knowledgeManagementToolSpecs()`（🟡 未穷举，实际更大）。

本地 coding runtime 走 `executeDirectWithRuntimeBinding(ctx, req, store, &attempt, nil)`（最后参数为 `nil` = 无远程绑定）→ **模型看到上面这一整份**，再追加 `spawn_coding_agent`（`core_agent_executor.go:1823-1825`）。

#### 远程 = 只剩 `ssh`

✅ `core_agent_executor.go:1826-1834`：

```go
	if c != nil && c.runtimeRemoteBinding != nil {
		filtered := tools[:0]
		for _, tool := range tools {
			if tooldef.Name(tool) == "ssh" {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}
```

✅ 执行时更狠，`coding_runtime_remote.go:412-434`：

```go
func (c *coreAgentCallbacks) remoteCodingRuntimeToolCallAllowed(name string, args map[string]interface{}) (bool, string) {
	if c == nil || c.runtimeRemoteBinding == nil {
		return true, ""
	}
	if strings.TrimSpace(name) != "ssh" {
		return false, "remote coding runtime exposes only its session-bound SSH execution tool"
	}
	action := strings.TrimSpace(agent.StringArg(args, "action"))
	if action != "exec" {
		return false, "remote coding runtime allows only SSH exec on its verified session"
	}
	if strings.TrimSpace(agent.StringArg(args, "session_id")) != c.runtimeRemoteBinding.SessionID {
		return false, "remote coding runtime SSH session does not match the verified task target"
	}
	if v2.IsDangerousCommand(agent.StringArg(args, "command")) {
		return false, "remote coding runtime rejected a dangerous SSH command"
	}
	return true, ""
}
```

四条拦截：非 ssh → 拒；非 `exec` → 拒；session 不匹配 → 拒；危险命令 → 拒。**安全性没问题**，问题在下一节。

#### 只读子代理 = 4 个工具

✅ `coding_runtime_child.go:22-27`：

```go
var serviceReadOnlyChildTools = map[string]bool{
	"read_file":      true,
	"list_directory": true,
	"web_search":     true,
	"web_fetch":      true,
}
```

注释解释了为什么这么窄（值得原样引用）：

```go
// The first service-hosted child surface is deliberately narrower than the
// ordinary planning policy.  In particular, it omits bash, SSH, skills, MCP,
// memory, IM and task-management tools: each can mutate external or durable
// host state even when it appears read-oriented.
```

并且 `codingagent.ToolPolicy.IsToolCallAllowed` 还有第二道：`web_fetch` 带 `save_path/output/dest/path/filename` 任一参数即拒（`codingagent/codingagent.go:103-109`）——**定义只读不够，参数也会让它变成写**。这个考虑很到位。

#### 三态对比表

| 模式 | 模型可见工具数 | 备注 |
|---|---|---|
| 本地 coding runtime | 28+（全宿主表面）+ `spawn_coding_agent` | 不受限 |
| 远程 coding runtime | **1**（`ssh`） | 但 ssh 定义含 12 个 action，仅 1 个可用 |
| 只读子代理 | **4** | `read_file / list_directory / web_search / web_fetch` |

### 3.2 GUI：本地 21 / 远程 18 的**镜像清单**

✅ `guiapp/coding_static_compatibility_inventory.go:39-88` 是本仓唯一一份"闭合的工具清单"（closed inventory），注释明写：

```go
// codingStaticCompatibilityInventory is the explicit S0 inventory for the
// still-legacy Coding static belt.  It is deliberately an inventory rather
// than a second planner: ... Keeping the inventory closed makes every new
// model-visible static tool an intentional review decision instead of an
// unnoticed append to a definition slice.
```

统计（✅ 逐条数过）：

- **local = 21 条**，其中控制面 5 条（`code_navigation / report_localization / spawn_coding_agent / todo / goal`）
- **remote = 18 条**，控制面同样是那 5 条
- **共享 12 条**：`web_search, web_fetch, download_file, current_datetime, coding_knowledge_search, knowledge_search, knowledge_image_search` + 5 个控制面
- **本地独有 9 条**：`Glob, ripgrep, read_file, list_directory, git_diff, edit_file, edit_lines, write_file, bash`
- **远程独有 6 条**：`ssh_read_file, ssh_list_dir, ssh_write_file, ssh_edit_file, ssh_bash, ssh_check_task`

**结论：远程不是"同一套工具的远程实现"，而是一套按名字 1:1 重写的平行表面。** 本地 `read_file` ↔ 远程 `ssh_read_file` 之间没有任何抽象关系，只是两份各自渲染的 JSON Schema。

✅ 证据：`remote_coding_subagent.go:4767-4801` `remoteCodingToolDefinitions()` 手写了 6 个 `buildRemoteToolDef(...)`，描述全是中文重写（如 `"读取远程服务器上的文件内容"`）。

### 3.3 角色（explorer / reviewer）子表面：本地与远程对不齐

✅ 本地 `guiapp/coding_subagent_spawn.go:41-56`：

```go
var codingSubAgentSpawnRoleTools = map[codingSubAgentRole]map[string]bool{
	codingRoleExplorer: {
		"Glob": true, "ripgrep": true, "read_file": true, "list_directory": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"git_diff": true, "web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
	codingRoleReviewer: {
		"Glob": true, "ripgrep": true, "read_file": true, "list_directory": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"git_diff":   true,
		"web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
	// worker: nil map means "all standard coding tools except spawn"
}
```

✅ 远程 `guiapp/remote_coding_subagent_spawn.go:22-36`：

```go
	codingRoleExplorer: {
		"ssh_read_file": true, "ssh_list_dir": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true, "knowledge_image_search": true,
	},
	codingRoleReviewer: {
		"ssh_read_file": true, "ssh_list_dir": true, "ssh_check_task": true,
		...
	},
```

对比差异（✅ 全部是逐字核对的差异）：

| 差异 | 本地 | 远程 |
|---|---|---|
| explorer 工具数 | 12 | 10 |
| reviewer 工具数 | 12 | 11 |
| **explorer 与 reviewer 是否相同** | **完全相同**（角色在工具层无区分） | reviewer = explorer + `ssh_check_task` |
| `knowledge_image_search` | **无** | **有** |
| `git_diff` | **有** | **无**（远程无法看 diff） |
| 全局搜索能力 | `ripgrep` + `Glob` | **无**（远程没有 grep 等价物） |

最后一条是真实的**能力缺口**：远程 explorer 只能 `ssh_read_file` / `ssh_list_dir`，没有内容检索，只能靠整文件读。

### 3.4 姿态过滤（inquiry / operational）：本地与远程也各写一份

✅ `guiapp/coding_workbench_plan.go` 里有四个函数，两本地两远程：

| | 本地 | 远程 |
|---|---|---|
| inquiry | `isCodingInquiryTool`（:185）9+4=13 个（含 ssh_* 名字，再被 hostKind 过滤掉） | `isRemoteCodingInquiryTool`（:234）8 个 |
| operational | `isCodingOperationalTool`（:212）9 个 | `isRemoteCodingOperationalTool`（:665）8 个 |

✅ 远程侧还额外配了 shell 命令级拒绝：`rejectCodingInquiryShellCommand`（:250）禁重定向、`$()`、`` ` ``、`<(`；`rejectCodingOperationalShellCommand`（:680）禁重定向与命令替换。注释说明了理由：

```go
// rejectCodingInquiryShellCommand provides the second half of the read-only
// inquiry boundary.  Keeping bash/ssh_bash available is useful for CodeGraph,
// git history, and targeted searches, but the tool allow-list alone cannot
// make an arbitrary shell command safe.
```

**"allow-list 不足以让任意 shell 命令变安全"** —— 这句是本仓里最清醒的一句话，与 Codex 的 `destructive_hint` 思路同源。

---

## 4. Review 发现（问题清单）

按严重度排序。

### P1-1 远程模式下 `ssh` 工具定义与实际能力严重不符（MaClawSrv）

✅ `corelib/agent/tool_register_core.go:358-380` 的 `ssh` 定义：

- `description`: `"Manage SSH connections and remote operations such as connect, exec, background exec, upload, download, list, and close."`
- `properties` 共 **17 个**：`action, timeout, host, user, port, auth_method, key_path, password, label, initial_command, session_id, command, wait_seconds, task_id, tail_lines, local_path, remote_path`
- `Required: ["action"]`

而在远程 coding runtime 下，**真正可用的只有 `action="exec"` + `command`（`session_id` 还必须等于固定值）**。

后果：
1. **描述主动误导模型**：它说可以 `connect / upload / download / list / close`，而这 5 个在远程模式下 100% 被拒。
2. **凭证字段仍留在模型视野**：`password / key_path / auth_method / host / user` 全都还在 schema 里，尽管这些参数在这个模式下永远不可能生效。这是纯粹的攻击面与 token 浪费。
3. **`action` 枚举没有被裁剪**：`applySSHActionEnum`（`core_agent_executor.go:1592-1606`）只按 `allowSSHFileTransfer` 决定是否追加 `upload/download`，**完全不知道当前处于远程 runtime 绑定态**。

对比 Codex 的做法：Codex 有 `ToolExposure::Hidden`——"Keep this tool registered for dispatch without exposing it to the model"，并且会对过大 spec 降级为 Hidden。本仓缺的正是"按模式裁剪参数与枚举"这一层。

**建议**：在 `applySSHActionEnum` 里增加 `runtimeRemoteBinding != nil` 分支：把 `action` 枚举收成 `["exec"]`，并只保留 `command`（`session_id` 由宿主注入，不给模型），同时换成远程专用 description。这是最小改动、收益最大的一处。

> **【状态更新 2026-09-20】已修复（agentservice 侧最小实现；P1-2 的六件套统一仍开放）**
>
> 实现（均在 `corelib/agentservice/core_agent_executor.go`，门禁与 handler 行为零改动）：
> 1. `BuildTools` 远程绑定分支改为返回 `narrowSSHDefinitionForRemoteRuntime()`：`action` 枚举强制 `["exec"]`，properties 仅 `action / session_id / command / wait_seconds`（17→4），`required=[action,session_id,command]`，description 改为"仅可在预绑定已验证会话上 exec"；凭证与连接/传输/后台任务参数全部退出模型视野。同分支以全新切片替代 `tools[:0]`，顺带消除该处 P3-1 别名隐患。
> 2. `BuildSystemPrompt` 经 `remoteRuntimeSessionContextNote()` 把绑定 session id 注入 SessionContext。
> 3. `TestRemoteCodingRuntimeRestrictsToolsToBoundSession` 扩展：钉住"恰好 4 属性、13 个禁用参数缺席、enum=["exec"]、note 含绑定 id"。
>
> **与原建议的偏差及证据**：原建议"`session_id` 由宿主注入、不给模型"不可行。核码证实：门禁（`coding_runtime_remote.go:423`）要求 `args.session_id` 与绑定值**精确相等**（空串也拒）；而该值由 `bindRemoteCodingRuntime`（执行器内部，79-84 行）才确定，`RuntimePrompt` 由模块注册表在执行前贡献（`runtime_adapter.go:69-83`）且无任何生产模块知晓此 id，`BuildSystemPrompt` 原有分支亦无注入 —— **修复前模型无从得知 session_id，首次 ssh 调用必被拒，拒绝文案也不提示正确值，工具输出 `[<sessionID>]` 前缀只在成功后才出现：远程 ssh 工具实际处于死锁态**。因此保留 `session_id` 为模型可见（required），并新增 SessionContext 注入使其可兑现。这是本次修复在 P1-1 之外发现的实质缺陷（P0 级可用性），已一并解决。

### P1-2 三份"远程工具策略"互不一致

| | MaClawSrv | GUI |
|---|---|---|
| 远程模型可见工具 | 1（`ssh`，仅 exec） | 6（`ssh_*`）+ 共享 12 = 18 |
| 文件读取方式 | `ssh exec cat`（模型自己拼命令） | `ssh_read_file`（结构化，带 offset/limit） |
| 文件写入方式 | `ssh exec`（同上） | `ssh_write_file` / `ssh_edit_file` |
| 后台任务 | 无（`exec_background` 被拒） | `ssh_bash` 自动转后台 + `ssh_check_task` |

同一个产品、同一个 `codingruntime` 底座，两个主机给出**完全不同粒度**的远程能力。MaClawSrv 的远程编码体验实质上是"给模型一个裸 shell"，GUI 的则是"给模型一套结构化远程文件工具"。

**建议**：把 GUI 的 `ssh_*` 六个工具下沉为 `corelib/agentservice` 的 core spec（它们是 transport-neutral 的），远程 runtime 模式改为"暴露 `ssh_read_file / ssh_list_dir / ssh_write_file / ssh_edit_file / ssh_bash / ssh_check_task` 六件套、隐藏裸 `ssh`"。这样既统一了两主机，也顺带修掉 P1-1（不再需要把 17 个参数的 `ssh` 发给模型）。

> **【归属更新 2026-09-20】本项移交语义路由整改计划统一处理，直接实现冻结**
>
> 交叉发现：工作区并行推进的 `semantic-tool-routing-coding-subagent-remediation-zh.md`（整改文档）已把 CodingSubAgent 工具带演进整体纳入其切片路线。`tool-routing-improvement-plan-zh.md` v4 的进展注记（2026-09-19）原文证实："同日落地切片 5 的 shadow-only 部分（整改文档 §9.19）：远程只读 provider specs（`fs.read.remote`/`repo.inspect.remote`，已验证 SSH 会话绑定，本地/远程绑定不可互换）+ 远程 S0 观察从 `not_prepared` 升级为真实 shadow plan 对账；**仍无 cutover，远程静态带照旧服务**；切片 2（callback scope/admission 接线 + E5 原子 cutover）未开始。"
>
> 本项的"六件套下沉 / 隐藏裸 ssh"是模型可见面变更，按 `managed-surface-migration-checklist-zh.md` 强制闸门须逐能力过四问 + 四场景不变量矩阵——正是整改计划的既有流程。为避免双轨实现与表面漂移，本 review 不再直接实施本项；P1-1 的最小收窄（已落地）在 cutover 前继续承担远程 ssh 定义治理职责。

### P2-1 本地 explorer 与 reviewer 的工具集完全相同

✅ 见 §3.3：`codingSubAgentSpawnRoleTools` 里两个角色的 map 内容逐字相同（唯一差别是 reviewer 那条 `"git_diff": true` 单独占了一行并多了一个对齐空格）。

后果：`codingagent.Role` 在本地侧退化成一个标签，"只读探索"与"严格评审"在工具层面无法区分。远程侧反而做到了（reviewer 多一个 `ssh_check_task`）。

**建议**：至少让 reviewer 具备本地侧的校验能力（如允许 `bash` 的受限只读子集，或补一个本地 `check_task` 等价物），否则"reviewer 子代理"在本地只是"换个名字的 explorer"。

> **【状态更新 2026-09-20】已修复（reviewer 获得白名单门禁下的受限校验 shell）**
>
> 实现：
> 1. `guiapp/coding_subagent_spawn.go`：`codingSubAgentSpawnRoleTools` 的 reviewer map 增加 `"bash": true`（explorer 保持无 shell，worker 不变）。定义侧与提示词承诺（`codingSpawnRolePromptHint` 的"跑 shell 检查"）对齐。
> 2. `corelib/codingagent/reviewer_shell.go`（新增）：`reviewerShellInvocationAllowed` 白名单门禁，接入 `ToolPolicy.IsToolCallAllowed`（ReadOnly 角色 + canonical 名为 bash/shell 时对 `args.command` 逐段校验，fail-closed）。白名单语义：只读程序头（`ls/cat/head/tail/rg/grep/diff/find…`，其中 `find` 剔除 `-delete/-exec/-ok/-fprint*` 等写入 primary）+ 项目校验入口（`go test/vet/build/list/doc/version/env`、`cargo test/check`、`npm test/run`、`make`、`pytest`、`python -m pytest|unittest`）；`git` 仅 17 个只读子命令（`branch/tag/remote/config` 等可写形式排除）；`FOO=bar` 前缀跳过、`timeout <dur>` 解包、`-C/-c` 取值跳过；跨 `;`/`&&`/`||`/`|`/换行逐段校验；`2>&1`/`1>&2` 放行、其余任何 `<`/`>` 一律拒绝；`$(` 与反引号一律拒绝。
> 3. 测试：`corelib/codingagent/reviewer_shell_test.go`（39 个允许例 / 36 个拒绝例 + ToolPolicy 集成与 nil-allowlist fail-closed）；`guiapp/coding_subagent_spawn_test.go` 的 reviewer 断言由"严格只读（无 bash）"更新为"定义可见但调用受限"（`go test ./...` 放行、`rm -rf /`、`echo x > build.log`、`git commit`、`python -c` 拒绝）。
>
> **边界声明（设计使然，非缺陷）**：项目校验（`go test` / `npm run` / `make` / `pytest`）执行的是项目可控代码，项目代码自身可能写文件（`TestMain`、`conftest.py`、`package.json` scripts）。门禁防御的是**模型把 reviewer shell 当作写入/编辑通道**，不是"项目代码写文件"——后者是"校验"这一目标的固有属性。
>
> **【加固 2026-09-20 晚·对抗性自查】** 首版放行了 7 类隐藏的写入/执行向量，均已收口：
> ① `go env -w/-u`（持久写全局 go 配置）② `go build -o` / `go test -c`（模型自选路径产物）③ `go test -coverprofile/-cpuprofile/-memprofile/-mutexprofile/-blockprofile/-trace/-outputdir`（模型自选路径 profile/trace 写入）④ `go test -exec <prog>` / `go vet -vettool=<prog>`（任意程序执行，绕过解释器拒绝）⑤ `git diff/log/show --output=<file>`（diff 机制把结果写文件）⑥ `git -c` 盲跳改为**键允许列表**（原实现对值不校验，而 `core.pager`/`core.fsmonitor`/`diff.external`/`credential.helper` 等键会让 git 自身派生程序，如 `git -c core.pager=touch log`）⑦ `pytest --junitxml/--html/--resultlog`（模型自选路径报告写入）。
> 实现方式：`reviewerShellDeniedSubFlags`（精确 token 或 `flag=` 前缀匹配，`-count=1` 不会误撞 `-c`）+ `reviewerShellGitConfigKeys`（仅格式化类键：quotepath/autocrlf/color.*/diff.*/log.*/blame.*/grep.*/safe.directory）+ git `--output` 前缀拒绝；既有允许例 `git -c x=y log` 同步改为 `git -c core.quotepath=false log`。
> 测试扩充为 44 个允许例 / 54 个拒绝例，全部通过；`go vet ./corelib/codingagent/` OK。
>
> **【加固 2026-09-20 深夜·第二/三轮自查】** 环境前缀层与 go -mod 层继续收口 4 类：
> ① **`GOFLAGS` 内联注入**——原实现盲跳 `FOO=bar` 前缀，而 `GOFLAGS=build=-o=/tmp/x go build ./...` 能整体旁路 flag 否定表、`GOFLAGS=-mod=mod` 能改写 go.mod/go.sum → go 头拒绝内联 `GOFLAGS`；
> ② **git 环境配置/程序注入**——`GIT_CONFIG_COUNT/KEY_n/VALUE_n` 等价 `-c` 但绕过键允许列表，`GIT_PAGER`/`PAGER`/`GIT_SSH(_COMMAND)`/`GIT_PROXY_COMMAND`/`GIT_ASKPASS`/`SSH_ASKPASS`/`GIT_EDITOR` 会派生程序 → git 头按名拒绝 + `GIT_CONFIG*` 前缀拒绝；
> ③ **`go -mod=mod`**（`-mod mod` 分离值同样覆盖）——允许 go 命令改写源码树内 go.mod/go.sum → go 子命令统一拒绝（readonly/vendor 放行）；
> ④ **程序/加载器解析劫持（全局）**——`PATH=/tmp/evil git log` 执行的是模型指定目录下的同名程序、`LD_PRELOAD`/`LD_LIBRARY_PATH`/`DYLD_INSERT_LIBRARIES` 是加载器级注入、`BASH_ENV`/`ENV` 会被宿主 shell 启动时 source、`IFS` 影响执行侧分词 → **对所有头**一律拒绝。
> 设计演进：环境前缀从"盲跳"改为"跳过但回收"，由 `reviewerShellEnvPrefixAllowed(head, dropped)` 按头复查。测试最终 **50 允许例 / 64 拒绝例**，vet OK，`go build ./...` OK。
>
> **【加固 2026-09-20 深夜·第四轮自查】** git 剩余 exec 通道 + 各头工具链 env 再收口 3 类：
> ① `git grep -O<pager>` / `--open-files-in-pager`——**pager 程序直接作参数传入**（`git grep -Otouch x` 即任意执行）；`git diff --ext-diff`——打开默认关闭的外部 diff 驱动执行。已按 token 拒绝（注明：porcelain `git diff` 默认开启的 textconv 驱动来自项目提交的 .gitattributes，与 package.json scripts 同属"项目可控配置"既有豁免，不另设防）；
> ② 各头工具链 env 注入——`CARGO`/`RUSTC`/`RUSTC_WRAPPER`（cargo 重解析工具链）、`SHELL`/`MAKEFLAGS`/`MAKEFILES`（make 的配方 shell 与 makefile 预载）、`NODE_OPTIONS`/`npm_config_script_shell`、`PYTEST_ADDOPTS`/`PYTEST_PLUGINS`（**`PYTEST_ADDOPTS=--junitxml=x` 可绕过 token 层的 junitxml 拒绝**）→ 按头加入 env 否定表；
> ③ `python -m pytest` 的 env 复查——解析头是 python，但有效程序是 pytest，需按 "pytest" 键二次复查被丢弃的 env 前缀。
> 测试终态 **56 允许例 / 78 拒绝例**，vet OK，`go build ./...` OK。
>
> **【加固 2026-09-20 深夜·第五轮（红队换视角）】** 前四轮是逐条扫向量；本轮改为质疑门禁的 shell 语义假设本身（宿主以 `sh -c`/`bash -c` 执行整串命令，见 `skill_integration.go:679`），立即命中一个 **P0 绕过 + 一类成建制缺口**：
> ① **裸 `&` 未作分隔符（P0）**——分割器处理了 `&&`/`||`/`|`/换行，但 `ls & rm -rf /` 作为一段、头命令 `ls` 在允许表内即放行，而 POSIX shell 里 `&` 就是命令分隔符——**整个门禁被一个字符旁路**。修复：分割时裸 `&` → 分隔符，`2>&1`/`1>&2` 用哨兵保护放行无害 fd 复制，`&>`/`&>>` 退化为含 `>` 的段被下游拒绝；
> ② **`--` 透传参数的写入翻转**——`npm run lint -- --fix`、`npm test -- -u`：脚本本体是受信项目配置，但透传参数是模型自选的，能翻成写入模式 → npm 段检查 `--` 之后的 `--fix/--write/--update/-u/-i/--in-place/--save`；pytest 同族 `--update-snapshots`/`--snapshot-update` 加入 flag 否定表；
> ③ 顺带穷尽其余语法构造确认已闭合：子壳 `(...)`、heredoc/herestring（含 `<`）、进程替换、`exec`/builtin 头、反斜杠续行、`${var}` 展开（无命令替换即无执行）——均 fail-closed。
> 测试终态 **62 允许例 / 88 拒绝例**，vet OK，`go build ./...` OK。
>
> **【加固 2026-09-20 深夜·第六轮（性质测试收口）】** 把第五轮红队结论升级为**机器验证**：新增 `TestReviewerShellSeparatorMatrix`，不变量为"无论用什么 joiner，被拒片段永远不能漏过"——30 种分隔符形态（`&`/`&&`/`;`/`;;`/`|`/`||`/换行/`\r\n` 及空格、堆叠、混合变体如 `&;&`/`&|`/`|&`/`&&&`/`\n&`/`\n;\n`）× 5 个恶意载荷 = 150 组组合全部拒绝，7 组良性管道（含 `2>&1` 哨兵路径与裸 `&` 背景只读命令）全部放行。分隔符完备性从人工断言升级为 CI 资产：未来任何人改 `splitReviewerShellSegments`，遗漏任一分隔符类即红。
>
> **【补全 2026-09-20 深夜·第七轮（远程同款矛盾 + cd 缺口）】** 转向**执行路径覆盖率**审查，发现 P2-1 的远程孪生矛盾：
> ① 远程 reviewer 提示词（`buildRemoteInspectionRoleSystemPrompt`）承诺"只读探查 + shell 检查"、工具清单列出 `ssh_bash`、规范第 2 条要求"用 ssh_bash 做只读探查（find/rg/ls/git status/diff/test 等）"，但 `remoteCodingSpawnRoleTools` 的 reviewer 表**没有 `ssh_bash`**——定义层不渲染、执行层必拒，模型按提示词调用必吃闭门羹（与本地修复前逐字同构）；且该提示词为 explorer/reviewer 共用，explorer 同样被列了拿不到的 `ssh_bash`。修复：reviewer 表加 `ssh_bash`（explorer 维持无 shell，与本地策略镜像）；corelib 门禁 `IsToolCallAllowed` 扩展到 `ssh_bash`（`ssh_bash` 执行参数同为 `command`，白名单直接适用；宿主侧高危黑名单仍在其后照跑）；提示词把 `ssh_bash` 行与规范第 2 条移入 reviewer 条件块；
> ② 白名单允许表补 `cd`（纯导航、零写入能力；远程工作流惯用 `cd <repo> && go test ./...`，缺 `cd` 会把整条 meaningful 命令在第一段就拒掉）。
> 测试终态 **64 允许例 / 92 拒绝例**（新增 ssh_bash 门禁集成 4 例、cd 允许例 2 例、远程角色调用门禁 5 例）；`go build ./...` OK；`go test -count=1 ./corelib/codingagent/` ok；guiapp 远程角色/静态带/pilot 共存测试 ok。
>
> **【补全 2026-09-20 深夜·第八轮（覆盖率全景核验，零代码行为变更）】** 完成 `IsToolCallAllowed` 之外的执行入口穷举：
> ① **本地唯一 bash 执行点** = `executeToolWithOutcome`（`coding_subagent.go:2242`）内的 `case "bash"`（:2430），2299 行角色门在 switch 之前——文件里其余 11 处 `case "bash"` 全部位于参数 schema/别名/提示 helper（非执行点）；
> ② **durable 只读子代理**（本地 `CodingSubAgent.ExecuteReadOnlyChild:190` / 远程 `RemoteCodingSubAgent.ExecuteReadOnlyChild:148`）：均为复制父对象后走同一执行循环 → 必经同一角色门，无旁路；
> ③ **horizon 路径**（`executeToolWithOutcome:2274-2277` 在角色门之前分发）：只对不在 `codingSubAgentToolNames` 的工具生效，且 `horizonPosture` 仅由 episode runner（`horizon_episode.go:109`）设置、从不用于嵌套 spawn——对 explorer/reviewer 惰性；
> ④ 远程 `ExecuteReadOnlyChild` 的过时注释（"excludes ssh_bash/write/edit tools for children"）已按第七轮事实修正。
> **结论：白名单门禁的执行路径覆盖是完备的**——本地/远程/durable 三条链全部收敛到单一角色门。验证：build/codingagent/guiapp 远程角色测试全绿。
>
> **【补全 2026-09-20 深夜·第九轮（宿主覆盖面核验，零代码改动）】** 门禁的最后一个审查维度——`ToolPolicy` 的**宿主覆盖面**。全仓检索发现除 guiapp 本地/远程外还有两个消费者：`corelib/agentservice/coding_runtime_child.go`（service 只读子代理，4 工具 read-only 面：read_file/list_directory/web_search/web_fetch，explorer-only）与 `tui/coding_runtime_child.go`（同面）。逐一核验：
> ① **四个宿主同一扇门**：guiapp 本地（`toolCallAllowedForRole`）、guiapp 远程（`remoteToolCallAllowedForRole`）、agentservice（`serviceReadOnlyChildToolCallAllowed`，两个分发点 :2334/:2400 均已接）、tui（`tuiReadOnlyChildToolCallAllowed`）全部构造 `codingagent.ToolPolicy` 并调用 `IsToolCallAllowed`——第七轮的 ssh_bash 白名单扩展对它们**自动生效**；
> ② **覆盖是结构性的**：共享 agent loop（`corelib/agent/loop.go:2872/:2898`）在分发执行前强制调用回调的 `IsToolAllowed`/`IsToolCallAllowed`（`ToolAuthorizer` 接口），任何实现该接口的宿主都被 loop 钉住，不依赖宿主自觉；
> ③ service/tui 子代理不提供 reviewer 角色、无 shell——与 guiapp 的 reviewer 校验 shell 并存不冲突（最小面 vs 校验面是两种产品姿态）；其 `web_fetch` 目的地拒绝同样由 `IsToolCallAllowed` 统一兜住。
> **结论：P2-1 门禁在宿主维度同样是完备的。** 三个维度（向量层 17 类 / transport 孪生面 / 执行路径 + 宿主覆盖）全部闭合，review/fix/optimize 循环对 P2-1 正式收官。
>
> **【外溢 2026-09-20 深夜·第十一轮（兄弟门审计）】** 收官后把同一向量清单回放给同链路的**高危黑名单分类器**（`rejectDisallowedCodingBashCommand`，本地 bash 与远程 ssh_bash 共用、喂高危审批流）。其底座扎实（引号感知分词、命令位置追踪、边界含裸 `&`、git 18 个改写子命令），但命中两个缺口并已修：① `|&` 不在 `isShellCommandBoundary` → `cmd1 |& rm -rf build` 让 rm 不在命令位置、位置感知检测器全体漏报；② `find` 写入 primary（`-delete`/`-exec` 族/`-fprint*`）无任何检测 → `find . -name '*.go' -delete` 绕过整个高危审批流。新增 `hasFindWritePrimary` + `|&` 边界，拒绝例 +4、防误伤例 +3，分类器相关宽批次测试全绿。
>
> **【第十二轮（层间一致性审计）】** 三层防御各自审完后，最后审**层与层之间**：① **顺序核验**：本地 bash 执行点的命令归一化（`normalizeSubAgentVerificationCommandForExecution`：Windows `;`→`&&` 窄改写 + 剥离退出状态回显尾）发生在白名单门**之后**、分类器**之前**——核验两改写器均为**单调收窄**（改写后段集合 ⊆ 原段集合），白名单对全量原始段 fail-closed 校验，"改写后逃过门禁"不成立；② **真冲突一枚**：`1>&2` 白名单放行、分类器 `hasShellOutputRedirection` 只豁免 `2>&1` → `go test ./... 1>&2` 过了门禁却在 guard 吃拒绝。已把 `1>&2` 加入豁免（变异检测器语义下的正确豁免——fd 复制不是文件写入）；③ **白名单 `<` 一刀切放宽**：输入重定向（`< file`/`< /dev/null`）纯读无写入能力，改为放行；`<(` 进程替换（执行）与 `<<` heredoc（正文行会被分段器当命令、fail-closed 但报错困惑）保持拒绝；④ **固化层间契约**：新增 `TestReviewerValidationVocabularyPassesHighRiskClassifier`（白名单词汇命令 × 分类器 = 全过），防止未来分类器加规则时悄悄打破 reviewer 可用性。回归：白名单全套 + 分类器宽批次全绿。
>
> **【第十三轮（证据门对齐——第三层入列）】** 层间审计的最后一层：**验证证据门**（`suppressesVerificationFailure`/`isSubAgentVerificationCommand`）。两个真冲突，根因都是第一轮白名单的词汇设计：
> ① **验证头 | 管道**：`go test ./... 2>&1 | tail -20` 被白名单拆段后逐段放行（`go test` 允许 + `tail` 允许），**管道拓扑对段级检查不可见**；但管道退出状态是最后一个命令的，`| tail` 恒为 0 → 证据门判为"失败抑制"→ 命令执行后被记 guardrail 违规。更糟的是白名单允许例里就有这条——词汇表在主动教模型踩违规模式。修复：白名单新增**管道拓扑检查**（`splitReviewerShellPipeGroups` 保留 `|` 连接的组 + `reviewerShellValidationPipeDenied`：验证头管道化 → 最早层拒绝，报错直说"管道会向证据隐藏退出状态，请裸跑"）；只读头管道（`git diff | head`、`go env | grep`）不受影响。
> ② **`1>&2` 方向性**：第一轮把 `2>&1`/`1>&2` 当对称的"无害 fd 复制"——错。`2>&1` 把 stderr **并入** stdout，证据捕获仍看到全部输出（证据门 4258 行注释明确豁免它）；`1>&2` 把 stdout **抽走**进 stderr → 验证输出在捕获端"看似空"（`subAgentVerificationOutputLooksEmpty`）→ 证据失效。且拆分器哨兵恢复时把 `1>&2` 错误还原成 `2>&1`，导致它从未到达段级检查。修复：双哨兵各自还原 + 段级只豁免 `2>&1`，`1>&2` 落入输出重定向拒绝（与证据门 `isShellVerificationOutputRedirectionToken` 的豁免集合精确对齐）。第十二轮在**变异检测器**层对 `1>&2` 的豁免保持不变——fd 复制确实不是文件写入；两层语义不同（文件写入 vs 证据捕获），各自正确，白名单从严对齐的是更紧的证据语义。
> ③ **层间契约测试升级为三层**：guiapp `TestReviewerValidationVocabularyPassesHighRiskClassifier` 每条词汇命令现在同时断言"过高危分类器" + "不触发证据抑制门"；反向钉住 `verifier|tail` 必须被判抑制（说明白名单为何拒绝）。
> 回归：白名单全套 + guiapp 分类器/证据/远程 pilot 宽批次 + agentservice 远程门禁全绿。
>
> **【第十四轮（token 与 shell tokenization 的语义分歧）】** 十三轮全部在"命令语法"层打转（分隔符、重定向、替换），本轮换轴：门禁逐 token 匹配，**shell 却先做引号剥离、反斜杠转义、`$` 展开和花括号展开再执行**——两套 tokenization 的分歧本身就是攻击面：
> ① **引号走私**：`go test "-coverprofile=x"`——门禁看到的 token 是带引号的 `"-coverprofile=x"`，flag= 前缀匹配失配 → 放行；shell 剥引号后 go 实际收到 `-coverprofile=x`。`git diff "--output=x"`、`npm run lint -- "--fix"`、`go test "-mod=mod"`、`git diff \--output=x`（反斜杠转义）同构。
> ② **`$` 展开走私**：`${IFS}`（`go test ${IFS}-coverprofile=x`）、裸变量（`go test $PKG`——环境变量内容模型不可见）、ANSI-C 引用（`go build $'-o'`）——shell 展开后的词与门禁匹配的词完全不同。`${` 在白名单此前只挡**参数位置**，展开成 flag 无任何检查。
> ③ **花括号展开**：`git diff {--output=x,y}`（bash 宿主重写成两个词）。
> ④ **证据门绕过**：`'go' test ./... | tail -1`——管道头 token 带引号，第十三轮的 `reviewerShellValidationPipeHead` 匹配不上 → 验证命令管道化逃过拒绝。
> ⑤ **路径限定头**：`./git status`——`path.Base` 把 `./git` 归一成 `git`，实际执行的是 cwd 下任意同名二进制（可由同会话早期写文件阶段布防）。
> **修复**（一套归一化，四处共用）：`reviewerShellWordRewriteRisk` 按引号/转义状态逐字符扫描原始 token，凡 shell 仍会改写的形态（非单引号内的 `$` 且非词尾、非双引号收尾前的 `$`、无引号 `{...,...}`）直接拒绝；`reviewerShellNormalizeToken` 把引号与反斜杠剥离到 shell 同款（`"git"`/`\git`/`"git"` 归一为 `git`）；`reviewerShellNormalizedFields` 成为段级与管道头检查共用的 tokenization。字面量保留可用性：`rg 'error$'`、`rg '\$PATH'`（单引号内/转义/词尾 `$` 均 literal）继续放行。头含 `/`/`\` 一律拒绝（path.Base 别名漏洞）。python 族补 env 规则：`PYTHONHOME` 拒绝；`PYTHONPATH` 仅放行 `.`/`./...` 项目相对项（与"项目代码可信"注记同边界）；unittest 分支此前完全不复检 env，已补 `reviewerShellEnvPrefixAllowed` 调用。**已知残留**（注释留档）：无引号 glob 展开（`go build *` 命中名为 `-o` 的既有文件）需先在门外写入文件布防，不在此门防线内。
> 测试：拒绝例 +22（引号/展开/花括号/引号管道头/路径头/PYTHON\*）、允许例 +9（归一化正方向 + 字面 `$` 形态）；层间契约测试扩充 6 条归一化形态（分类器 + 证据门同步判定）。回归：codingagent 全包、guiapp 契约测试、agentservice 全包（110s）、`go build ./corelib/... ./guiapp/` 全绿。白名单终态 **71 允许例 / 131 拒绝例**（本轮 +9/+22）。
>
> **【第十五轮（允许头里残留的 exec/配置通道 flag）】** tokenization 分歧收口后逐头再扫"模型自选即程序"的入口，六个残留在 token 层之下：
> ① **`make -f/--file/--makefile`**：make 在只读头表里但**没有任何 flag 否定**——`make -f /tmp/evil.mk test` 的 recipes 就是模型写的代码，任意执行。补 `make` 否定表 `-f/--file/--makefile`。
> ② **`cargo --config`**：内联 TOML，`cargo test --config build.rustc-wrapper=/tmp/evil` 或 `target.<triple>.runner` 让 cargo 生成/运行任意外部程序。补 `cargo/test`、`cargo/check` 否定表（cargo 此前只有 env 层 CARGO/RUSTC/RUSTC_WRAPPER，flag 层为空）。
> ③ **pytest argparse 前缀缩写**：`pytest --junit=x.xml`——argparse 的 prefix matching 把 `--junit` **唯一解析成** `--junitxml`，精确 token 否定表被缩写绕过（同理 `--htm`→`--html`）。`reviewerShellFlagDenied` 对 pytest 增加：严格前缀命中被拒 flag 名 → 拒。
> ④ **npm 透传值形态**：`npm run lint -- --fix=x`——map 精确匹配 `--fix`，`=x` 形态失配。透传检查改为取 `=` 前的名字再查表（`--write=true` 同堵）。
> ⑤ **npm `--prefix`**：npm 执行 prefix 目录的 package.json 脚本——`npm test --prefix /tmp/staged` 让模型把 npm 指向 staged package。新增 `reviewerShellNpmPrefixAllowed`：仅放行 `.`/`./...` 项目相对路径（与 PYTHONPATH 同边界）。
> ⑥ **git env `GIT_DIR`/`GIT_WORK_TREE`**：把 git 整体指到模型 staged 的仓库（带恶意 textconv 驱动的 .gitattributes）——`git -C` 的 env 孪生。加入 git env 否定表。`git -C` 本身因远程工作流依赖（`git -C /repo diff`）**保持放行**，残留风险与 textconv caveat 一并书面注记在 `reviewerShellGitFlagsAllowed`。
> 顺带核验：round 14 的 `$` 展开拒绝已顺带堵死 make 经典的 `make FOO='$(shell rm -rf /)'`（`$(` 在参数位置被拒）。测试：拒绝例 +12、允许例 +3（`--prefix ./`、`make -C`、`pytest -p no:cacheprovider`）；层间契约同步 +3。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **74 允许例 / 143 拒绝例**。
>
> 验证：`go build ./...` OK；`go vet ./corelib/codingagent/` OK；`go test -count=1 ./corelib/codingagent/` ok；guiapp 目标测试 ok。
>
> **【第十六轮（裸只读头自带的写/执行 flag + getopt 缩写走私 + env 孪生）】** 前十五轮聚焦子命令头（go/npm/cargo/git），本轮正面审**裸只读头**（sort/date/rg 等从未有逐 flag 否定表的头）：
> ① **`sort -o/--output`**：覆盖模型自选路径的任意文件（`sort -o ~/.bashrc x`）。**`sort --compress-program=CMD`**：GNU sort 在临时文件上执行模型自选程序——直接 exec 通道。补 `sort` 否定表 `-o/--output/--compress-program`。
> ② **`date -s/--set`**：改系统时钟。补 `date` 否定表。
> ③ **`rg --pre CMD`**：ripgrep 对每个文件跑预处理器命令——rg 此前零否定表。补 `rg` 否定表 `--pre`（`--pre-glob` 无 pre 时无害，保留）。
> ④ **`make --eval`/`--load-plugins`/`-I`/`--include-dir`**：--eval 让模型注入 makefile 代码（`include /tmp/evil` 无需 `$(` 即可绕过命令替换检查——段级替换检查拦 `'$(shell …)'` 但拦不住 include 指令）；--load-plugins dlopen 模型自选 .so；-I 把项目 makefile 的 include 解析引到 staged 目录。
> ⑤ **GNU getopt 前缀缩写走私**：round 15 只给 pytest（argparse）加了缩写规则，但 **GNU getopt_long 同样接受无歧义缩写**——`sort --compress=x` 解析成 `--compress-program`、`make --lo=x` 解析成 `--load-plugins`，精确 token 否定表被绕过。新增 `reviewerShellAbbreviationHeads`（pytest/make/sort/date），`reviewerShellFlagDenied` 对这些头统一适用严格前缀规则（短 flag 不缩写；go flag 包/git/npm/cargo(clap 关闭推断)/rg 均不缩写）。
> ⑥ **每头 env 孪生**：`GNUMAKEFLAGS`（MAKEFLAGS 的第二条注入通道，预载 `-f`）、`GOENV`（备用 env 文件注入 GOFLAGS 绕过 GOFLAGS 否定）、`RUSTFLAGS`/`CARGO_ENCODED_RUSTFLAGS`（rustc 任意 flag 含 `-C linker=`，--config 的 env 孪生）、`npm_config_prefix`（--prefix 的 env 孪生）、`GIT_OBJECT_DIRECTORY`/`GIT_ALTERNATE_OBJECT_DIRECTORIES`（staged 对象库伪造验证证据读到的历史）。`GOWORK` 特判：仅 `off`（安全方向）放行，其余值指向 staged go.work 替换模块——在 `reviewerShellEnvPrefixAllowed` 按值检查。
> ⑦ **反向修正（可用性）**：git `--output` 的 HasPrefix 误杀无害的 `--output-indicator-new/old/context` 格式 flag——收紧为精确 `--output` / `--output=` 前缀。
> 实现：只读分支的 pytest/make 特判改为**通用逐头检查**（凡有否定表的头都过 `reviewerShellFlagDenied(head, fields, 1)`）。测试：拒绝例 +22、允许例 +5（`--output-indicator-*`、`--output-delimiter`、`date -u`、`--pre-glob`、`GOWORK=off`）；guiapp 层间契约同步 +3。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **79 允许例 / 165 拒绝例**。
>
> **【第十七轮（已拒 env 的 flag 孪生面 + wrapper 命令的参数解析）】** 本轮轴心：每条已拒的 env 都要问它的 **flag 孪生**在哪，每个 wrapper 命令的参数解析都要独立审：
> ① **`git --git-dir`/`--work-tree`**：第十五轮拒了 `GIT_DIR`/`GIT_WORK_TREE` env，但 flag 形态 `git --git-dir=/tmp/staged log` 一直放行——flag 孪生缺口。补进 `reviewerShellGitFlagsAllowed`（精确 token + `=` 前缀 + 独立值形态）。
> ② **`go -C <dir>`**：`reviewerShellSubcommand` 对 `-C` 的**值跳过是和 `git -C` 共用的**——`go -C /tmp/staged test ./...` 今天会被解析成合法的 `go test` 放行，实际对 staged module 跑测试（GOWORK 的孪生）。go 分支加专用检查（`-C` 精确 + `-C=` 前缀）；`git -C` 按既定残留注记保持放行，`make -C build` 是另一个语义（切目录跑 make）继续允许。
> ③ **`go -toolexec`**：把**每次工具链调用**包进模型自选程序——任意 exec 通道，test/build/vet 三处否定表补上。**`go -overlay`**：build 时把项目源码替换为 staged 内容——staged 代码以"项目代码"身份进编译，test/build/vet 同补。
> ④ **`cargo --manifest-path`**：把 cargo 指向 staged crate——`--config` 的 token 孪生。cargo/test、cargo/check 同补。
> ⑤ **`npm -C`**：`--prefix` 的短别名（npm 7+），第十五轮的 prefix 限制只匹配了长形态——`npm -C /tmp/staged test` 绕过。`reviewerShellNpmPrefixAllowed` 扩展 `-C <val>`/`-C=<val>`，项目相对路径继续放行。
> ⑥ **`make -t/--touch`**：touch 所有目标文件的 mtime——`touch` 头被拒但这形态绕行（工作区 mtime 污染）。补进 make 否定表（`--to` 等缩写由 getopt 缩写规则自动覆盖）。
> ⑦ **反向修正（可用性）**：timeout 原先**盲剥前两个 token**——`timeout -k 5 10 go test` 的头会被解析成 `5` 而误拒。新增 `reviewerShellTimeoutCommandFields`（感知 `-k/-s/--kill-after/--signal` 的独立值、布尔长 flag、`long=value` 形态，然后跳过时长 token），段级与证据门管道头两处共用；未知形态退化为 fail-closed（剩余 token 仍按命令段全检）。
> 已知残留（注释留档）：`go test -ldflags=-extld=<prog>`（cgo 外链器的 exec 通道，仅 cgo 链接触发，flag 太常用于版本注入构建，不拒）。
> 测试：拒绝例 +18、允许例 +4（`timeout -k/--preserve-status/--kill-after=` 三形态、`npm -C ./`）；guiapp 层间契约同步 +2。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **83 允许例 / 183 拒绝例**。

> **【第十八轮（裸只读头残余写通道 + env 孪生 flag 补遗 + cd 残留定性）】** 本轮轴心：裸只读头清单逐头扫尾 + 继续追"已拒 env 的 flag 孪生"：
> ① **`tree -o`**：tree 一直在允许头清单里但零否定表——`tree -o /tmp/out /repo` 把列表写进模型自选文件（sort -o 的同构）。补 `tree` 否定表。
> ② **`file -C`/`--compile`**：file 是只读头，但 `-C` 编译 magic 数据库=写文件。补 `file` 否定表。
> ③ **`hostname <任意参数>`**：裸 `hostname` 打印主机名，但**带参数即改系统主机名**（`-F <file>` 从文件读新名——date -s 的同门）。段级专用规则：只放裸形态。
> ④ **`git --paginate`（任意位置）/ `-p`（仅子命令前）**：已拒 `PAGER`/`GIT_PAGER` env 的 **flag 孪生**——`git -p log` 生成 pager 程序。位置敏感：子命令后的 `-p` 是 patch 显示 flag（`git log -p` 必须保留），故 `reviewerShellGitFlagsAllowed` 记录 `subSeen`（首个非 flag token）后停用 `-p` 检查；`--paginate` 子命令后本就非法，任意位置拒无害。`--no-pager` 安全方向保留。
> ⑤ **`go -workfile`**：`GOWORK` env 拒绝的 flag 孪生（`go -workfile /tmp/staged/go.work test` 整体换 workspace）。并入 go 分支的 `-C` 检查循环。
> ⑥ **`go test/build/vet -modfile`**：指向备选 go.mod——其 **replace 指令可把项目模块换成 staged 代码**（GOWORK/GOENV 拒绝的 token 孪生）。三处否定表同补。
> ⑦ **`cd <绝对路径> && <验证命令>` 残留定性**：这是已拒的 `go -C`/`npm --prefix`/`cargo --manifest-path` 的**组合孪生**，但测试钉桩 `cd /repo && go test ./...` 为允许例（远程工作流依赖，与 `git -C` 同类）——按项目先例**书面注记残留、不拒**（staging 需本门之外的前置写，同 git -C 残留类）。cd 头注释与 go -C 检查处双注记。
> 测试：拒绝例 +13、允许例 +5（`tree /repo/src`、裸 `hostname`、`file -b`、`git log -p -3`、`git --no-pager log -p`）；guiapp 层间契约同步 +3。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **88 允许例 / 196 拒绝例**（落盘核准）。

> **【第十九轮（宿主身份对齐 + 验证链失败屏蔽的层间对齐补完 + cmd.exe 百分号展开）】** 本轮轴心：核验门禁注释的核心假设"宿主以 `sh -c`/`bash -c` 执行整串命令"本身：
> ① **宿主身份审计**：逐宿主找到 reviewer bash 命令的真实执行点——Unix 本地 `bash -c` ✓、远程 ssh `sh -lc` ✓，**Windows 本地不成立**：`guiapp/windowsCodingBashInvocation` 按命令形态分发——未引号 `||`/`>nul` 或 MSVC 编译形态 → **cmd.exe**、unix-inspect 清单（ls/file/stat/cd）→ 模式适配器改写成等价 PS 脚本、其余 → **PowerShell 5.1**。门禁 bash 模型在 Windows 是近似但 fail-closed：cmd.exe 的 `%VAR%` 词改写与 `||` 触发器组合拒（见③）、PS 元字符 `$`/反引号早已整体拒、PS 别名（sort→Sort-Object 等）功能语义分叉但词汇头内无写/exec 通道、MSVC 形态不可达（cl/vcvars 不在词汇表）、bash 独有的 `FOO=x cmd` env 前缀语法在 Windows 上允许但失效（解析不了，非旁路）。适配器为保守清单式改写（只动 ls/file/stat/cd，路径提取丢 flag），等价性由 guiapp 自有测试保障。宿主身份矩阵写入门禁头注释。
> ② **验证链失败屏蔽的层间对齐补完（本轮核心）**：证据门 `suppressesVerificationFailure` 覆盖 `||`（coding_subagent.go:11025）、`&`（11036）与 `&&`/`;`（尾段须为验证命令或编译产物链，11866 `verificationChainMayContinue`），**但白名单只拒了管道**——`go test ./... || echo ok` 先放行、执行、再被标记，正是第十三轮"在白名单层拒绝、而不是先跑再标记"原则漏掉的形态。新增 `reviewerShellValidationOrChainDenied`（原始命令逐 token 走链拓扑，配合 `reviewerShellSegmentIsValidationHead` 复用 env 前缀跳过 + timeout 解包 + 验证头判定）：验证头之后 `||`/`&` 一律拒；`&&`/`;` 尾段必须也是验证头（`go test ./... && go vet ./...` 保留，`go test ./... && git status` 拒）；**fallback 在验证命令之前**（`git status || go test ./...`）保留——验证命令仍是链条末段、真实退出状态可见。
> ③ **cmd.exe `%VAR%` 百分号展开**：未引号 `||` 触发 cmd.exe 分发，`%VAR%` 是门禁 `$` 拒绝的 % 孪生词改写通道。模型无法在该路径引入环境变量（`FOO=x cmd` 是 bash 独有语法，cmd.exe/PS 都解析不了），只有既有 env 可展开——组合拒 fail-closed：未引号 `||` + 命令含 ≥2 个 `%` 即拒；单个 `%`（`printf '%s'`）保持字面，允许。
> 测试：拒绝例 +9（`||`/`&`/`&&` 尾/`;` 尾各形态 + `%VAR%` 组合）、允许例 +4（fallback 前置两形态、验证尾 `&&`、单 `%`）；guiapp 层间契约同步 +2。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **92 允许例 / 205 拒绝例**（落盘核准）。

> **【第二十轮（跨层词汇契约钉桩 + `/dev/tcp` 网络魔路径）】** 本轮轴心：**把"层间对齐"从一次性核对升级为常驻契约**。逐例核算"guiapp 证据门认为是验证 ∧ corelib 白名单放行"的精确交集——逐头对比 guiapp `isSubAgentVerificationCommandSegment`（go/cargo/swift/zig/…/node/bun/dart/make/just/mage/… 巨大清单）与白名单允许头（go/cargo/npm/make/pytest/python），确认 npm `exec`/直呼 runner 形态在白名单段级已拒（不在允许子命令）、python 直呼 `.py` 脚本白名单段级也拒（只放 `-m pytest/unittest`）——**交集内无漂移缺口**，即第十九轮的对齐已经完备，但这一不变量纯靠两份清单的当前状态维持，任何一侧放宽都会静默破裂：
> ① **跨层契约测试**：corelib 新增导出包装 `ReviewerShellGateAllows`（唯一用途：让宿主侧测试直接调门禁裁决）；guiapp 新增 `TestReviewerVerificationVocabularyAgreesAcrossLayers`——枚举全部"白名单放行 ∧ 宿主证据门认为是验证"的代表形态（go test/vet/build、cargo test/check、npm test/run lint、make、pytest、python -m pytest/unittest、timeout 包装形态），对每个形态断言：宿主侧 `isSubAgentVerificationCommand` 认可为验证（清单漂移即报），且 `|| echo`/`| tail`/`& bg`/`&& git status`/`; git status` 五种失败屏蔽形态被 corelib 在门口拒绝（拒绝无理由也报）。良性镜像（fallback 前置、验证尾 `&&`）同时钉住允许方向。
> ② **`/dev/tcp`、`/dev/udp` 网络魔路径**：第十四轮放宽的裸输入重定向 `< file` 是"纯文件读"，但 bash 的 `/dev/tcp/host/port`、`/dev/udp/host/port` 是魔路径——"读"它们即打开网络通道（`sort < /dev/tcp/10.0.0.1/4444`）。段级检查对 `<` 后目标做前缀扫描（spaced `< /dev/...`、glued `</dev/...`、fd 前缀 `0< /dev/...` 三种 token 形态归一覆盖）；`< file` 文件读方向钉允许例。顺带把 `<<<` herestring（被 `<<` 的 Contains 检查顺带覆盖）钉拒绝例，防未来把 Contains 改成等值匹配。
> 方法论沉淀：**层间对齐完成后必须"导出可测入口 + 契约测试钉桩"，否则对齐只是当轮事实而非常驻约束**；跨层清单要逐例算交集（含"段级已拒所以无缺口"的头），不能凭清单规模直觉。
> 测试：拒绝例 +5（魔路径×3、`<<<`、连写形态）、允许例 +1（`sort < go.mod`）；新增跨层契约测试（13 验证形态 × 5 屏蔽形态 + 3 良性镜像，一次全绿）。回归：门禁全套、guiapp 契约、codingagent 全包、`go build ./corelib/... ./guiapp/` 全绿。终态 **93 允许例 / 210 拒绝例**（落盘核准）。

### P2-2 本地有 `ripgrep` / `git_diff`，远程没有等价物

远程 explorer 缺内容检索（`ripgrep`）与版本差异（`git_diff`）。远程 reviewer 多了 `ssh_check_task` 但依然没有 `git_diff`——也就是说**远程评审子代理无法看到"这次改了什么"**，只能整文件读。

考虑到本仓远程模式把 `FinalWorkspaceGateRequired` 设为 true、完成判定依赖 `git status` 探针，评审侧却拿不到 diff，这是一个明显的能力断层。

> **【归属更新 2026-09-20】本项移交语义路由整改计划，shadow 已部分落地**
>
> 缺口方向与整改计划切片 5 的 `fs.read.remote` / `repo.inspect.remote` 远程只读 provider specs 重合（`tool-routing-improvement-plan-zh.md` v4 进展注记证实其 2026-09-19 已以 shadow-only 形态落地：真实 shadow plan 对账在跑、不渲染不授予；cutover 未开始）。若此刻直接给远程静态带加 `ssh_git_diff`/`ssh_grep` 之类新工具，会与受管表面路线形成两套并行的远程检查能力——正是迁移闸门要防的"乱"。本项冻结为该计划 cutover 批次的验收输入：cutover 后应复核本缺口是否闭合（远程 explorer/reviewer 能否做内容检索、能否看到"这次改了什么"）。

### P2-3 远程探针在屏幕抓取 PTY

✅ 见 §2.4。虽然是当前设计下唯一可行解（要复用"已验证的活会话"、且禁止重连），但：

- `serviceRemoteSSHExecBound` 把输出截成 `output[:4000] + "... (truncated) ..." + output[len-4000:]`（`coding_runtime_remote.go:320-322`），**中间内容丢失**。若 `git status` 输出超过 8000 字符，`StatusHash` 会基于被截断的文本计算——同一状态下不同截断可能产生不同 hash，进而误判"工作区变了/没变"。
- `WaitForOutputContext` 的 `waitSeconds` 上限 600s（`coding_runtime_remote.go:315-317`），慢速远端可能超时后返回空输出 → 探针失败 → writer 被 `blockBeforeExecution` 拦成 `TaskBlocked`。

**建议**：至少让探针走独立的非交互 exec 通道（`ssh session exec` 语义而非 PTY 输入），或把 hash 输入改为"未截断的完整输出"。

> **【更正 2026-09-20】第一条 bullet 的"StatusHash 基于被截断文本计算"论断经核码不成立，撤回**
>
> 证据链：
> 1. `StatusHash` 的**唯一**生产者是恢复探针路径：`serviceRemoteWorkspaceProbeFromOutput`（对完整 status 文本做 `sha256.Sum256`），其输入来自 `serviceRemoteSSHExecReadOnly` —— 该路径**不做任何截断**。
> 2. `serviceRemoteSSHExecBound` 的 head4000+tail4000 截断只有两个消费方：模型可见的 exec 输出，以及 `serviceEnsureRemoteGitBaseline` → `RemoteGitBaselineRequest.Result()`（`corelib/codingruntime/remote_workspace.go`）——后者**只做标记行检查（OK/Initialized/Fail），完全不计算 hash**；标记位于输出尾部，尾部保留式截断恰好保得住标记。
> 3. 环形缓冲（`NewLinesSince`，`sshPreviewMaxLines=2000`，FIFO 丢弃）无法把一帧截"花"：中间行被丢 ⇒ begin-marker 已被丢 ⇒ `LastIndex` 找不到帧起点 ⇒ fail-closed 报错，而不是算出错误 hash。
>
> 结论：基线与观测走**同一条未截断管道**，"同一状态不同 hash → 误判工作区漂移"的路径不存在。剩余两条 bullet（输出 >2000 行、等待 >600s）属于**可用性**问题而非正确性问题：触发时探针失败闭合（TaskBlocked），不会产生错误判断。原建议中"hash 输入改为未截断完整输出"已无对象；"独立非交互 exec 通道"仍可作为可用性改进保留（开放项，非缺陷）。
>
> **【状态更新 2026-09-20】"输出 >2000 行"残留已修（环容量 2000 → 20000）**
>
> - `corelib/remote/ssh_manager.go`：`sshPreviewMaxLines` 2000 → 20000。理由：恢复探针与 git 基线帧共用该预览环，`git status` 输出超环 ⇒ begin-marker 被挤出 ⇒ 探针 fail-closed，健康工作区被整任务 TaskBlocked。**帧侧无法自救**：状态文本必须完整进入 hash，任何"在状态后重复 begin-marker"的花招会让被截空的状态算出一个合法 hash，把可用性问题恶化成正确性问题——环容量是该设计下唯一既保正确又救可用的杠杆。
> - 消费方核查：`PreviewTail(10/20/2000)` 均为尾读语义（输出上限不变）；`ssh_background_task` 的 `NewLinesSince` 只会受益于更大保留；hub 与 guiapp 各有独立预览结构（各自 500 行上限），不共享此环。内存代价最坏 ~2.4MB/会话。
> - 回归测试 `TestProbeFrameSurvivesRingResize`：超旧帽（2000）帧在环内完整存活（marker/HEAD 不丢）；超新帽帧仍 fail-closed（begin-marker 必被挤出，钉住"失败闭合而非错误帧"的残余边界）。
> - 600s 等待上限**保持不变**：durable Runner 租约 15 分钟，探针等待超过租约会把恢复流程卡死在租约期内；该边界是有意设计，非缺陷。

### P3-1 `tools[:0]` 复用底层数组

✅ `core_agent_executor.go:1827`：`filtered := tools[:0]` 复用 `tools` 的 backing array 再 append。当前分支里 `tools` 之后不再使用、立即 return，所以**现在是正确的**；但这是那种"改一行就炸"的写法（比如将来有人在 return 前又读一次 `tools`）。

> **【状态更新 2026-09-20】已全部消除**：review 所指的远程分支实例已随 P1-1 修复改为全新切片；排查中发现同一模式还有第二个实例——`BuildTools` 本地路径的 hardware-expert allow-set 过滤（现约 1915 行，过滤后 `tools` 仍会被 light-profile 路径继续读取，`tools` 虽是函数本地 `make` 产物，任何持有原切片引用的观察者都会被原地改写）——同日一并改为 `make([]map[string]interface{}, 0, len(tools))` 并注释原因。`go build ./...` / `go vet` / `corelib/remote` 全套测试通过。

### P3-2 `isCodingInquiryTool` 是本地/远程名字的并集

✅ `coding_workbench_plan.go:185-193` 同时列了 `bash` 和 `ssh_bash` 等。它靠下游 `filterCodingStaticCompatibilitySurface(local, ...)` 把 ssh_* 剔除才正确。能工作，但把"隔离"的责任推给了调用顺序。🟡 未穷举所有调用点，不能断言没有遗漏。

> **【状态更新 2026-09-20】调用点已穷举，🟡 解除；union 残留被定位为本地 inquiry 清单的历史条目**
>
> 穷举结果（全部 6 个生产调用点 + 证据）：
>
> **本地路径**（`coding_subagent.go`）：
> 1. 定义侧 inquiry（1677-1679）：`filterCodingInquiryTools` → **紧随同一表达式内** `filterCodingStaticCompatibilitySurface(codingStaticCompatibilityHostLocal, …)`——union 里 4 个 ssh_* 条目由 host 过滤剔除，顺序正确。
> 2. 定义侧 operational（1683-1685）：`isCodingOperationalTool`（212-220 行）**本身不含任何 ssh_\***，清单自含隔离，无顺序依赖。
> 3. 调用侧（2142/2145）：union 检查之前有两道更强的结构门——渲染面精确名围栏（`corelib/agent/loop.go`）+ `staticCompatibilityCanonicalToolAllowed`（1815 行，对**当前请求实际渲染 surface 的成员检查**；模型回合 surface 恒非 nil，`surface == nil` 仅限宿主直维/测试路径，注释 1824-1829 明示）。跨模式名字（本地回合同调 `ssh_read_file`）在成员门即被拒，union 检查只是纵深防御。
>
> **远程路径**（`remote_coding_subagent.go`）：review 的 union 指控**不适用**——远程有完整独立的分离清单：
> 4. 定义侧（2283-2287）：`filterRemoteCodingInquiryTools` / `filterRemoteCodingOperationalTools`（`isRemoteCodingInquiryTool` 234 行 / `isRemoteCodingOperationalTool` 665 行，均不含对方模式名字）→ `filterCodingStaticCompatibilitySurface(codingStaticCompatibilityHostRemote, …)`。
> 5. 调用侧（2798/2802）：`readOnlyInquiry && !isRemoteCodingInquiryTool(canonicalName)` / `operationalRequest && !isRemoteCodingOperationalTool(canonicalName)`，且已有 `coding_static_compatibility_inventory_test.go` 多个 `readOnlyInquiry` 场景测试覆盖。
>
> 残余脆弱点定位：本地 `isCodingInquiryTool` 的 4 个 ssh_* 条目（`ssh_read_file/ssh_list_dir/ssh_bash/ssh_check_task`）是历史残留——本地路径渲染面永远不会合法包含它们（hostLocal 剔除），远程路径也不查这个函数。**未删**（避免无法证明"宿主直调 surface==nil 路径无依赖"的行为变更，宁慢勿乱）；已加两个钉桩测试把 load-bearing 顺序变成 CI 资产：`TestCodingInquiryFilterCompositionDropsRemoteToolsOnLocalHost`（顺序颠倒/host 过滤被删即红）与 `TestCodingOperationalFilterIsSelfContained`（operational 清单自含隔离）。若后续要删残留条目，按 managed-surface 闸门走一次四问即可。

---

## 5. 与 Grok / Codex / Pi 的对照判决

| 维度 | Grok Build | Codex | Pi | **本仓** |
|---|---|---|---|---|
| 工具表面是否可配置裁剪 | 是，但靠 `config.tools` **切片**（未列出的从注册表里 `remove` 掉） | 是，`ToolExposure` 6 态 + `ToolExposures` bitflags，含 `Hidden` | 否，工具名是 8 个的封闭联合类型 | **部分**：MaClawSrv 远程靠"过滤 + 调用时拒绝"；GUI 靠闭合清单 `codingStaticCompatibilityInventory` |
| 能力降级 | 有（`file_toolset` 互斥校验） | 有（`supports_search_tool` / `namespace_tools` / 8KB·64KB 字节预算 → `Hidden`） | 无 | **有**：`FinalWorkspaceGateRequired` / `ReadOnly` / `MutationScope` / 角色 / 姿态，共 5 个正交维度 |
| 定义与执行边界是否同一策略对象 | 是（`FinalizedToolset` 单一来源） | 是（`ToolPolicy` 同时服务定义过滤与执行检查） | — | **是**：`codingagent.ToolPolicy` 同时实现 `FilterToolDefinitions` 与 `IsToolCallAllowed`（✅ `codingagent/codingagent.go:56`：`IsToolAllowed lets ToolPolicy serve directly as agent.ToolAuthorizer. This keeps the model-facing definition filter and the execution-time boundary on the same policy object.`） |
| 参数级降级（裁剪 enum / properties） | 部分（`register_with_params` 是注册期，非运行期） | 有（`ToolOverrides`） | 无 | **无** ← P1-1 |
| 未在上下文中暴露的工具如何被发现 | `search_tool`（BM25，结果**不注入**工具表面，靠 `use_tool` 同轮二次分发） | `tool_search`（BM25，**下一轮**物化进工具表面） | 无 | **无工具检索**；靠语义规划器 `ToolPlan → CatalogRenderer` 选能力 |

**判决**：

1. 本仓在**策略对象的统一性**上比 Grok 做得好——`codingagent.ToolPolicy` 一个对象同时管定义过滤与执行拦截，这正是 Codex 的设计意图，而 Grok 的 `search_tool` 与 `use_tool` 是分离的。✅
2. 本仓在**降级维度数量**上是四者里最多的（模式 × 只读 × MutationScope × 角色 × 姿态），但**没有"参数级降级"**这一层，导致 P1-1。Codex 有 `ToolExposure::Hidden` + 字节预算，Grok 有注册期 params，本仓缺运行期裁剪。
3. 本仓**没有工具检索**（无 BM25 / 无 `search_tool`），而是走语义能力规划（`dynamic_semantic_*` 家族，🟡 本轮未展开）。这与 Grok/Codex 的"延迟发现"路线不同，属于第三条路。
4. 本仓独有的、三者都没有的东西：**durable 执行账本 + 完成闸门**。`codingruntime` 的 `Runner` / `PolicySnapshot` / `FinalWorkspaceGateRequired` / `ErrPolicyMismatch` / `ErrStaleAttempt` 这一整套"跨重启的失败语义"，是 Grok/Codex/Pi 都没有的。这是本仓最硬的部分，且本地远程**完全对称**——问题只出在它上层的工具表面。

**一句话**：本仓的"骨架"（durable runtime）比三个参照系都强，"皮肤"（模型工具表面）是最弱的一环，而本地/远程的不对称全部集中在皮肤上。

---

## 6. 未核实 / 未覆盖

- 🟡 `sharedHostToolSpecs()` 与 `knowledgeManagementToolSpecs()` 的具体条目未展开，故 MaClawSrv 基线工具总数只给了下界（28+）。
- 🟡 `guiapp` 的 `dynamic_semantic_*` / `reviewed_dynamic_capabilities` 家族（语义能力规划器）本轮未读，它与本报告的"静态 local/remote 清单"是并存的两条路线，需要单独一份 review。
- ~~🟡 `filterCodingInquiryTools` 的所有调用点未穷举，P3-2 只是风险提示。~~（2026-09-20 已穷举并解除，见 P3-2 状态块：6 个生产调用点全部核验，钉桩测试落地。）
- 🟡 `hubcenter` / `hub` 侧是否另有远程工具策略未查（本轮限定在 `corelib` + `guiapp` + `tui`）。
- 🟡 未跑测试验证上述结论（`go build` / `go test` 未执行），所有结论均为源码静态阅读。

---

## 附：关键文件索引

| 文件 | 作用 |
|---|---|
| `corelib/codingruntime/types.go` | `Task` / `Attempt` / `PolicySnapshot` / `Store` 契约 |
| `corelib/codingruntime/executor.go` | `Runner`（唯一生命周期），完成闸门、policy 冻结校验 |
| `corelib/codingruntime/remote_identity.go` | 远程目标规范化与非秘密身份 |
| `corelib/codingruntime/local_workspace.go` | 本地 Git 探针（`os/exec`） |
| `corelib/codingruntime/write_set.go` | 写集合与 `sameWriteScope`（跨模式隔离） |
| `corelib/codingagent/codingagent.go` | `Role` / `ToolPolicy`（定义与执行同源） |
| `corelib/agentservice/coding_runtime_remote.go` | MaClawSrv 远程：绑定、探针、ssh-only 拦截 |
| `corelib/agentservice/coding_runtime_child.go` | 只读子代理：4 工具表面 |
| `corelib/agentservice/core_agent_executor.go` | `BuildTools` / `IsToolAllowed` / `IsToolCallAllowed` / `coreToolSpecs` |
| `guiapp/coding_static_compatibility_inventory.go` | **闭合清单**：本地 21 / 远程 18 |
| `guiapp/coding_workbench_plan.go` | inquiry / operational 的本地与远程 allow-list + shell 命令级拒绝 |
| `guiapp/coding_subagent_spawn.go` | 本地角色工具集 |
| `guiapp/remote_coding_subagent_spawn.go` | 远程角色工具集 |
| `guiapp/remote_coding_subagent.go` | 6 个 `ssh_*` 工具定义与 `BuildTools` |
| `guiapp/coding_subagent.go` | 本地 `BuildTools` 与工具顺序 |
