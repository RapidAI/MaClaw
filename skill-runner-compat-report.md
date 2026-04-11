# Skill Runner 兼容性测试报告与改进建议

> **日期**: 2026-04-11 | **测试环境**: Windows 10 x64 / MaClaw v1 / Node.js v22.14.0
> **测试方法**: 3 个线上 Skill 实测 + 5 个定制诊断 Skill 对照测试 + Go 二进制逆向分析

---

## 一、测试结论

| Skill | 来源 | 手动执行 | Runner 执行 | Runner 步骤类型 | 结论 |
|-------|------|---------|-------------|----------------|------|
| xh-md-to-pdf | Hub | ✅ 成功 | ❌ exit status 1 | `bash` | 占位符路径未替换 |
| nodejs-env-doctor | 本地 | ✅ 成功 (pass:10) | ❌ exit status 1 | `bash` | 脚本执行成功但 Runner 报失败 |
| libtv-skill | Hub | — | — | — | 文件不在本地，无法测试 |
| _test-full | 测试 | ✅ 成功 | ❌ exit status 1 | `bash` | 脚本执行成功但 Runner 报失败 |
| _test-direct | 测试 | ✅ 成功 | ❌ "无产物路径" | `craft_tool` | 脚本执行成功但产物检测失败 |
| _test-echo | 测试 | ✅ 成功 | ❌ "无产物路径" | `craft_tool` | 同上 |
| _test-minimal | 测试 | ✅ 成功 | ❌ "无产物路径" | `craft_tool` | echo hello 也报失败 |

---

## 二、已确认的 Bug（附证据链）

### 🔴 Bug #1：bash 步骤始终报 `exit status 1`（即使脚本实际执行成功）

**严重程度**: P0 — 阻断所有带 `scripts/` + `package.json` 的本地 Skill

**现象**:
- Runner 报告 `status: failed`，`last_error: exit status 1 (exit code: 1)`
- 但脚本 **实际已成功执行**，输出文件已创建

**复现步骤**:
1. 创建 Skill，SKILL.md 中有 `node "{baseDir}/scripts/diag.mjs"` 的 bash block
2. diag.mjs 输出 JSON 并 `process.exit(0)`
3. 手动执行 `node diag.mjs` → ✅ 成功
4. Runner 执行 → ❌ `exit status 1`
5. 但桌面上的诊断文件已正确生成（内容完整、时间戳吻合）

**关键证据**:
```
Runner 报告: status=failed, last_error="exit status 1"
实际结果: 诊断文件已创建于 2026-04-11T05:40:41.886Z
文件内容: JSON 有效，包含正确的 CWD、环境变量、进程信息
```

**推测根因**: Runner 使用 `cmd /c` 执行 bash block 命令。Go 的 `exec.Command("cmd", "/c", command)` 在 Windows 上传递 exit code 时可能有误：
- cmd.exe 的 `%ERRORLEVEL%` 可能被后续的 cmd 内部操作覆盖
- 或者 Runner 解析进程 stdout/stderr 时，将 stderr 上的 Node.js runtime warning 误判为错误
- 或者 `extractExitCode()` 函数对 cmd.exe 的输出解析有 bug

**修复建议**:
```go
// skill_runner.go — runBashStepWithContext 函数
// 建议：不依赖 cmd.exe 的 exit code，改用 Go 的 process.Wait() 直接获取
cmd := exec.Command("cmd", "/c", bashCommand)
err := cmd.Run()
if err != nil {
    if exitErr, ok := err.(*exec.ExitError); ok {
        // 使用 Go 的 exit code，不依赖 cmd.exe 传递
        actualExitCode := exitErr.ExitCode()
        // ...
    }
}
```

---

### 🔴 Bug #2：craft_tool 步骤的产物检测逻辑过于严格

**严重程度**: P0 — 阻断所有无 `scripts/` 目录的本地 Skill

**现象**:
- Runner 用 `craft_tool` 模式执行 bash block（如 `echo "hello"`）
- 命令成功执行，输出正确
- 但 Runner 报告："脚本已运行，但既未报告产物路径，也未检测到预期产物"
- 最终判定 `failed`

**复现步骤**:
1. 创建 Skill，只有 SKILL.md + skill.yaml（无 package.json / scripts/）
2. SKILL.md 中有 ````bash\necho "hello"\n````
3. Runner 执行 → `craft_tool (failed)`
4. 错误信息："宿主尚未定位目标产物路径"

**推测根因**: 
- Runner 对 `craft_tool` 步骤强制要求产物路径输出
- 但诊断类 Skill（如环境检查）不需要产出文件，只需要报告结果
- Runner 没有区分"需要产物"和"仅诊断/报告"类型的 Skill

**修复建议**:
- 在 skill.yaml 中增加 `produces_artifact: true/false` 标志
- 或在 SKILL.md 的 frontmatter 中增加 `artifact_required: false` 选项
- 或在 craft_tool 模式中，exit code 0 即视为成功，不强制检查产物

---

### 🟡 Bug #3：Hub Skill 的 bash block 包含不可执行占位符

**严重程度**: P1 — 影响 xh-md-to-pdf 等所有 Hub Skill

**现象**:
- xh-md-to-pdf 的 SKILL.md 中第一个 bash block 是示例命令：
  ```
  node "{baseDir}/scripts/xh-md-to-pdf.mjs" "/绝对路径/输入.md" "/绝对路径/输出.pdf"
  ```
- `{baseDir}` 被替换 ✅
- 但 `"/绝对路径/输入.md"` 是中文占位符，不会被 args 替换 ❌
- Runner 直接执行 → `ENOENT: no such file or directory` → `exit status 1`

**根因分析**:
- SKILL.md 的 bash block 是 **使用示例**，不是 **可执行命令**
- Runner 的 `extractBashBlocksFromMarkdown()` 无法区分"示例"和"步骤"
- Runner 不用 args 参数替换 bash block 中的占位符

**修复建议**:
1. **短期**: Runner 跳过包含中文/非 ASCII 路径的 bash block
2. **长期**: 在 skill.yaml 中显式定义 steps，而非从 Markdown 解析：
```yaml
steps:
  - name: convert
    type: bash
    command: 'node "{baseDir}/scripts/xh-md-to-pdf.mjs" "{{input}}" "{{output}}"'
    params: ["input", "output"]  # 从 args 获取
```

---

### 🟡 Bug #4：步骤类型判断逻辑不透明

**严重程度**: P1 — 影响 Skill 开发者的体验

**现象**:
- 相同的 SKILL.md，有无 `package.json` 会导致步骤类型不同：
  - 有 `package.json` + `scripts/` → `bash` 步骤
  - 无 `package.json` / `scripts/` → `craft_tool` 步骤
- bash block 中有无 `{baseDir}` 也影响步骤类型

**问题**:
- 这个判断逻辑对 Skill 开发者完全不透明
- 没有文档说明什么条件下走 bash vs craft_tool
- 导致开发出的 Skill 行为不可预测

**修复建议**:
- 在 skill.yaml 中增加 `mode: bash | craft_tool | interactive` 字段
- 让 Skill 开发者明确指定执行模式，而非依赖隐式推断

---

## 三、Runner 架构分析（供开发组参考）

### 源码定位
```
核心文件: D:/workprj/aicoder/gui/skill_runner.go
```

### 两套执行器

| 执行器 | 适用场景 | 步骤类型 |
|--------|---------|---------|
| `SkillExecutor` | Hub Skill（有 SSH/远程能力） | `bash` |
| `SkillRunner` | 本地 Skill | `bash` / `craft_tool` |

### 关键函数调用链
```
SkillRunner.executeSkillSteps
  → SkillRunner.executeStepWithContext
    → runBashStepWithContext / runBashStepWithContextFull
      → needsBashShell (判断是否需要 bash shell)
      → checkPlatformCompat (平台兼容性检查)
      → extractExitCode (提取退出码)
      → lastNLines (获取最后 N 行输出)
```

### 步骤解析函数链
```
ParseMarkdownSkill
  → extractBashBlocksFromMarkdown (从 SKILL.md 提取 bash blocks)
  → scriptExecutionCommandFromMarkdown
  → commandFromSkillMarkdown
  → normalizeImportedScriptCommand
  → quoteScriptPath
```

### 已定义但未充分利用的 JSON 字段
```
json:"last_error_snippet"     ← 应填充 stdout/stderr 最后几行
json:"step_traces"            ← 应填充每步的执行追踪
json:"current_step_index"     ← 应填充当前步骤索引
json:"current_step_status"    ← 应填充当前步骤状态
json:"last_completed_step"    ← 应填充最后完成的步骤名
json:"duration_ms"            ← 应填充执行耗时
json:"command_resolved"       ← 应填充解析后的实际命令
```

### 模板变量（已确认支持）
```
{baseDir}     ← Skill 根目录（已在二进制中确认存在）
```

---

## 四、改进建议优先级矩阵

| 优先级 | 编号 | 改进项 | 工作量 | 影响范围 |
|--------|------|--------|--------|---------|
| **P0** | #1 | 修复 bash 步骤始终报 exit status 1 | 2h | 所有本地 Skill |
| **P0** | #2 | craft_tool 不强制要求产物路径 | 1h | 所有诊断类 Skill |
| **P1** | #3 | skill.yaml 增加 steps 定义 + 模式选择 | 1d | Skill 生态长期 |
| **P1** | #4 | 充实 get_skill_run 返回值 | 4h | 所有 Skill 调试体验 |
| **P2** | #5 | Hub 安装后自动 npm install | 2h | 所有 Node.js Hub Skill |
| **P2** | #6 | 区分"示例"和"步骤"bash block | 4h | SKILL.md 编写体验 |
| **P3** | #7 | 执行超时保护（默认 120s） | 2h | 系统稳定性 |
| **P3** | #8 | 步骤解析缓存失效机制 | 1h | 开发体验 |

---

## 五、推荐的 skill.yaml 扩展格式

```yaml
name: my-skill
version: "1"
description: "My awesome skill"
mode: bash                    # bash | craft_tool | interactive
produces_artifact: true       # 是否必须产出文件
artifact_pattern: "*.pdf"     # 产物文件匹配模式

steps:
  - name: check-env
    command: 'node "{baseDir}/scripts/env-check.mjs"'
    timeout: 60               # 超时秒数
    continue_on_error: false  # 失败后是否继续

  - name: generate
    command: 'node "{baseDir}/scripts/generate.mjs" "{{input}}" "{{output}}"'
    params: [input, output]   # 从 run_skill args 获取
    timeout: 120
    produces: ["{{output}}"]  # 预期产物路径

dependencies:
  npm: ["md-to-pdf@^5"]      # 自动 npm install
```

---

## 六、测试环境详情

```
OS:          Windows 10 19045 (x64)
Node.js:     v22.14.0 (C:\Program Files\nodejs\node.exe)
npm:         10.9.2
MaClaw:      C:\Program Files\RapidAI\MaClaw\MaClaw.exe (30.6 MB)
Shell:       cmd.exe (ComSpec=C:\WINDOWS\system32\cmd.exe)
HOME:        (not set)
USERPROFILE: C:\Users\ma139
```

---

## 附录：测试用 Skill 清单

| Skill | 位置 | 用途 |
|-------|------|------|
| _test-echo | ~/.maclaw/data/skills/_test-echo | craft_tool 模式测试 |
| _test-direct | ~/.maclaw/data/skills/_test-direct | 文件写入测试 |
| _test-minimal | ~/.maclaw/data/skills/_test-minimal | 最简 echo 测试 |
| _test-full | ~/.maclaw/data/skills/_test-full | 带 package.json 的 bash 模式测试 |
| _test-nq | ~/.maclaw/data/skills/_test-nq | 无引号路径测试 |

所有测试 Skill 可保留供开发组回归测试使用。
