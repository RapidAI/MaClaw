# Skill Runner 兼容性改进建议 v2

> 日期：2026-04-11 | 基于实际测试 + Go 二进制逆向分析
> 环境：Windows x64 / MaClaw / Node.js v22.14.0

---

## 一、已确认的 Bug（附复现步骤）

### 🔴 Bug #1：步骤数量解析缓存失效

**严重程度**：P0 — 导致多步骤 Skill 无限卡在 `running`

**复现**：
1. 安装 `nodejs-env-doctor`（SKILL.md 含 2 个 bash 代码块）
2. 修改 SKILL.md 为极简版（仅 1 个 bash 代码块）
3. 调用 `run_skill("nodejs-env-doctor")`
4. **预期**：`steps: 1`，**实际**：`steps: 3`（仍然使用旧的解析缓存）

**证据链**：

```
SKILL.md 内容（修改后）：
  1 个 ```bash 代码块，内容：node "{baseDir}/scripts/env-check-diag.mjs"

run_skill 返回：
  steps: 3                    ← 应为 1
  current_step: bash (running) ← 第 1 步执行完（diag 脚本确认已执行）
                               ← 第 2 步卡住，永远 running
```

对比测试：
| SKILL.md bash blocks | run_skill steps | 结果 |
|---------------------|-----------------|------|
| xh-md-to-pdf: 3 个 | 1（正确过滤） | ✅ 成功 |
| nodejs-env-doctor 原始: 2 个 | 2 | ❌ exit 1 |
| nodejs-env-doctor 简化: 1 个 | 3（缓存错误）| ❌ 卡住 |

**根因推测**：Skill Runner 在安装/注册时解析 SKILL.md 并 **缓存步骤列表**。后续 SKILL.md 变更后不会重新解析。

**建议修复**：
- 每次执行前 **检查 SKILL.md 文件的 mtime/size**，变更则重新解析
- 或在 `get_skill_run` 返回中暴露 `parsed_steps` 和 `source_mtime`，便于调试
- `skill.yaml` 中增加 `steps:` 字段，作为确定性步骤定义（绕过 Markdown 解析）

---

### 🔴 Bug #2：执行无超时保护

**严重程度**：P0 — 资源泄露 + 调用方无法判断终态

**复现**：
1. 触发 Bug #1（多出空步骤）
2. 观察 `get_skill_run` 连续调用 30 次、等待超过 60 秒
3. 状态永远为 `running`，无 `timeout`/`failed` 转换

**建议修复**：
```
在 runBashStepWithContext 中增加：
- 单步超时：默认 120 秒，通过 skill.yaml steps[].timeout 可配置
- 全局超时：默认 5 分钟
- 超时行为：发送 SIGTERM → 等 5 秒 → SIGKILL → status 设为 "timeout"
```

---

### 🟡 Bug #3：错误信息仅 `exit status 1`，缺少诊断数据

**严重程度**：P1 — 严重影响可调试性

**现状**：
```json
{
  "status": "failed",
  "last_error": "exit status 1"
}
```

**对比手动执行**（完全正常）：
```json
{
  "summary": { "pass": 10, "warn": 0, "fail": 0, "total": 10 },
  "exitCode": 0
}
```

**建议**：利用已存在但未使用的 `last_error_snippet` 字段：
```go
// 当前二进制中已有此 JSON tag：
// json:"last_error_snippet,omitempty"
// 推测在 skillRun 结构体中已定义但未填充

// 建议在 runBashStepWithContext 中捕获：
result.LastErrorSnippet = lastNLines(stdout, 5) + lastNLines(stderr, 5)
```

---

### 🟡 Bug #4：Windows 环境下子进程嵌套执行受限

**严重程度**：P1 — 影响 Windows 上大量 Skill

**复现**：
- `node env-check.mjs` 内部调用 `execFileSync('powershell.exe', [...])` 检查磁盘空间
- 手动执行（`cmd /c` 或直接 `node`）→ ✅ 完美运行
- Skill Runner 执行 → ❌ `exit status 1`

**诊断数据**（注入诊断脚本获取）：
```json
{
  "argv": ["C:\\Program Files\\nodejs\\node.exe", "...\\env-check-diag.mjs"],
  "cwd": "C:\\Users\\ma139\\.maclaw\\data\\skills\\nodejs-env-doctor",
  "shell": "C:\\WINDOWS\\system32\\cmd.exe",
  "PATH": "C:\\Program Files\\Git\\mingw64\\bin;...(完整 PATH，包含 nodejs)"
}
```
→ 第一步（diag 脚本，无子进程）执行成功，说明环境变量和 PATH 正确
→ 问题出在 **嵌套子进程**（Node 脚本内 `execFileSync` 调用 PowerShell）

**可能原因**：
1. Skill Runner 对子进程有 **安全限制**（如禁止创建子进程）
2. Skill Runner 的进程组/job object 设置导致子进程被终止
3. `execFileSync` 在 Skill Runner 的非交互式会话中行为不同

**建议**：
- 文档明确 Skill Runner 是否支持子进程嵌套
- 如果有限制，在 `checkPlatformCompat()` 中检测并给出明确错误
- 考虑提供 `--no-subprocess` 标志让 Skill 声明是否需要子进程能力

---

## 二、结构化改进建议

### 改进 #1：skill.yaml 增加 `steps:` 定义（根治步骤解析问题）

**现状问题**：从 Markdown 的 ` ```bash ` 代码块提取步骤是脆弱的——无法区分「使用示例」和「执行步骤」。

**示例**：`libtv-skill` 有 6 个 bash 代码块，全部是不同功能的 **使用示例**（创建会话、查询、上传…），但 Skill Runner 把它们全部当成了要顺序执行的步骤。

**建议**：在 `skill.yaml` 中支持显式 `steps` 定义：

```yaml
# skill.yaml
name: nodejs-env-doctor
version: "1.1.0"
description: "Node.js 环境检查与修复工具"
metadata:
  {"openclaw":{"emoji":"🔧","requires":{}}}

steps:
  - name: env-check
    type: bash
    command: 'node "{baseDir}/scripts/env-check.mjs"'
    description: "检查 Node.js 环境完整性"
    timeout: 60

  - name: env-fix
    type: bash
    command: 'node "{baseDir}/scripts/env-fix.mjs"'
    condition: on_failure        # 仅上一步失败时执行
    description: "自动修复环境问题"
    timeout: 120
```

```yaml
# libtv-skill 的 skill.yaml（不定义 steps，表示"按需调用"）
name: libtv-skill
description: "..."
mode: interactive               # 新增：标记为交互式，不由 Runner 自动执行步骤
```

**向后兼容**：无 `steps` 字段时 fallback 到当前 Markdown 解析逻辑。

---

### 改进 #2：Hub 安装后自动安装依赖

**现状**：`xh-md-to-pdf` 安装后首次运行必然失败，因为 `node_modules` 缺失。需手动 `npm install`（203 个包）。

**建议**：
```go
// 安装后检测 package.json / requirements.txt
func (s *SkillRunner) postInstall(skillDir string) {
    if fileExists(filepath.Join(skillDir, "package.json")) {
        // 检查 node_modules 是否存在且完整
        if !dirExists(filepath.Join(skillDir, "node_modules")) {
            s.runInstall("npm", []string{"install", "--production"}, skillDir)
        }
    }
    if fileExists(filepath.Join(skillDir, "requirements.txt")) {
        s.runInstall("pip", []string{"install", "-r", "requirements.txt"}, skillDir)
    }
}
```

---

### 改进 #3：丰富 `get_skill_run` 返回值

**当前返回**：
```json
{
  "run_id": "run-xxx",
  "status": "failed",
  "last_error": "exit status 1",
  "steps": 2,
  "current_step": "bash (failed)"
}
```

**建议返回**（利用已有但未填充的字段）：
```json
{
  "run_id": "run-xxx",
  "status": "failed",
  "current_step_index": 0,
  "current_step_status": "failed",
  "total_steps": 2,
  "last_error": "exit status 1",
  "last_error_snippet": "node env-check.mjs\nError: EACCES permission denied\n    at powershell.exe execFileSync...",
  "last_completed_step": null,
  "step_traces": [
    {
      "index": 0,
      "type": "bash",
      "command_template": "node \"{baseDir}/scripts/env-check.mjs\"",
      "command_resolved": "node \"C:\\Users\\ma139\\.maclaw\\data\\skills\\nodejs-env-doctor\\scripts\\env-check.mjs\"",
      "status": "failed",
      "exit_code": 1,
      "duration_ms": 15234
    }
  ],
  "artifact_path": "",
  "artifact_status": ""
}
```

从二进制中已确认这些 JSON tag **在结构体中已定义**（如 `step_traces`、`current_step_index`、`last_error_snippet`、`adjusted_step`、`orig_step`），建议开发组确认它们是否被正确赋值。

---

### 改进 #4：区分「自动执行」和「交互式」Skill

**问题**：`libtv-skill` 有 6 个功能入口（创建会话/查询/上传/下载/切换项目/获取进度），Skill Runner 把它们当成 6 个顺序步骤执行。但这类 Skill 应该由 AI Agent 根据用户请求 **按需调用** 特定脚本。

**建议**：
- `skill.yaml` 增加 `mode` 字段：`sequential`（顺序执行）| `interactive`（按需调用）
- `sequential` 模式：当前行为，顺序执行所有 bash 步骤
- `interactive` 模式：不自动执行，而是将 SKILL.md 的内容注入 AI 上下文，让 AI 决定调用哪个脚本

---

## 三、从二进制提取的关键信息（供开发组参考）

### 源码位置
```
D:/workprj/aicoder/gui/skill_runner.go
```

### 关键函数调用链
```
executeSkillSteps → executeStepWithContext → runBashStepWithContext
                                         ↘ checkPlatformCompat
                                         ↘ checkFileReferences → extractFileReferences
```

### 步骤解析相关
```
parseRawSteps              ← corelib/skill 包，解析 SKILL.md 中的步骤
extractStepsFromToolCalls  ← 从工具调用结果中提取步骤
mergeConsecutiveSteps      ← 合并连续步骤
parseSkillMarkdown         ← GitHub 搜索结果的 Markdown 解析
```

### SkillRunner 已有 JSON 字段（已定义但可能未全部使用）
```
run_id, skill_name, skill_dir
status, current_step, current_step_index, current_step_status
last_completed_step, last_completed_step_index
total_steps, steps, step_count, step_index
step_progress, step_traces
last_error, last_error_snippet
adjusted_step, orig_step
artifact_path, artifact_status
```

### 模板变量
```
{baseDir} → 替换为 skill 安装目录绝对路径
{{input}} → 替换为用户输入参数
```

---

## 四、优先级矩阵

| 优先级 | 编号 | 内容 | 预期收益 | 工作量估计 |
|--------|------|------|----------|-----------|
| **P0** | Bug #1 | 步骤缓存失效 | 消除「steps:3 但只有1个block」 | S (检查 mtime) |
| **P0** | Bug #2 | 执行超时保护 | 消除无限 running | M (加 timer) |
| **P1** | Bug #3 | 填充 last_error_snippet | 可调试性飞跃 | S (已定义字段) |
| **P1** | Bug #4 | 子进程嵌套支持 | Windows 兼容性 | M-L |
| **P1** | 改进 #1 | skill.yaml steps 定义 | 根治步骤解析 | M |
| **P2** | 改进 #2 | 依赖自动安装 | 首次使用体验 | S |
| **P2** | 改进 #3 | 丰富返回值 | 上层 AI 可调试 | S (字段已存在) |
| **P3** | 改进 #4 | 交互式 Skill 模式 | 扩展 Skill 类型 | M |

---

## 五、测试矩阵

| Skill | Bash Blocks | Runner Steps | 依赖 | 子进程 | 结果 | 根因 |
|-------|-------------|-------------|------|--------|------|------|
| xh-md-to-pdf | 3 | 1 | npm 203 pkg | Puppeteer/Chromium | ✅ 成功 | 依赖已手动安装 |
| nodejs-env-doctor 原始 | 2 | 2 | 无 | PowerShell/wmic | ❌ exit 1 | 子进程嵌套限制 |
| nodejs-env-doctor 简化 | 1 | **3** | 无 | 无 | ❌ 卡住 | 步骤缓存 + 无超时 |
| libtv-skill | 6 (全为示例) | 6 | python3 + API | HTTP | ❌ exit 1 | 环境缺失 + 错误解析 |

---

## 附录 A：复现 Bug #1 的最小 SKILL.md

```markdown
---
name: repro-bug1
version: "1.0"
description: "Reproduce step count bug"
metadata: {"openclaw":{"emoji":"🧪","requires":{}}}
---

# Repro

```bash
echo "step 1"
```
```

1. 先用上面的 SKILL.md 安装，此时 steps = 1 ✅
2. 修改 SKILL.md，添加第二个 bash block
3. 再次运行 → 观察 steps 是否更新

如果 steps 仍为 1 → **确认缓存失效 bug**。

## 附录 B：验证 last_error_snippet 是否被填充

用 `get_skill_run` 查看返回的 JSON，确认 `last_error_snippet` 字段是否有值。如果始终为空 → **确认字段未赋值 bug**。
