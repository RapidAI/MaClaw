# 🐛 [Bug Report] Skill Runner 在 Windows 上因 `findSh()` Shell 查找优先级错误导致所有 Skill 执行失败

## 摘要

MaClaw 的 Skill Runner 在 Windows 上执行 SKILL.md 中的 bash 代码块时，由于 `findSh()` 函数的 shell 查找优先级问题，**错误地命中了 `C:\Windows\System32\bash.exe`（WSL 转发器）**，而跳过了已正常安装的 Git Bash（`C:\Program Files\Git\bin\sh.exe`）。

当系统未安装 WSL 发行版时，所有使用 bash 代码块的 Skill 执行均以 `exit status 1` 失败，且错误信息极为简略，用户难以排查根因。

> ⚠️ **重要澄清**：系统上 Git Bash 已正常安装且可用（`C:\Program Files\Git\bin\bash.exe` 和 `sh.exe` 均存在），问题出在 Runner 的查找逻辑选错了目标，**不是**环境缺少 bash。

---

## 环境信息

| 项目 | 值 |
|------|-----|
| MaClaw 版本 | 当前最新版 |
| 安装路径 | `C:\Program Files\RapidAI\MaClaw\MaClaw.exe` |
| 操作系统 | Windows 11 (x64) |
| 编译语言 | Go |
| Git Bash | ✅ 已安装 (`C:\Program Files\Git\bin\bash.exe`, `C:\Program Files\Git\bin\sh.exe`) |
| WSL 状态 | ❌ 未安装发行版（`C:\Windows\System32\bash.exe` 为转发器，调用必失败） |

### 系统 Shell 实际情况

| 路径 | 类型 | 可用性 |
|------|------|--------|
| `C:\Program Files\Git\bin\bash.exe` | Git Bash | ✅ 正常可用 |
| `C:\Program Files\Git\bin\sh.exe` | Git sh | ✅ 正常可用 |
| `C:\Program Files\Git\usr\bin\bash.exe` | Git usr/bin/bash | ✅ 正常可用 |
| `C:\Windows\System32\bash.exe` | WSL 转发器 | ❌ 无发行版，exit 1 |
| `C:\Users\ma139\AppData\Local\Microsoft\WindowsApps\bash.exe` | WSL 别名 | ❌ 无发行版，exit 1 |

---

## 复现步骤

1. 在 Windows 上安装 MaClaw
2. 确保 Git for Windows 已安装（`C:\Program Files\Git\bin\sh.exe` 存在）
3. **不安装 WSL 发行版**（`C:\Windows\System32\bash.exe` 存在但不可用）
4. 通过 IM 或 CLI 调用任意 Skill（如 `run_skill`）
5. 观察 Skill 执行结果 → 全部返回 `exit status 1`

## 期望行为

- Skill Runner 应优先使用 Git Bash（`C:\Program Files\Git\bin\sh.exe`）执行 bash 代码块
- 若无可用的 Unix shell，应给出清晰的错误提示

## 实际行为

- `findSh()` 错误地返回了 `C:\Windows\System32\bash.exe`（WSL 转发器）
- 所有 bash 代码块执行返回 `exit status 1`，无 stdout/stderr 输出
- 无法判断是 shell 找错还是脚本本身出错

---

## 根因分析

### 逆向分析

对 `MaClaw.exe` 进行字符串提取和 debug 符号分析，发现以下关键信息：

#### 1. 源码结构（从 debug 符号提取）

```
D:/workprj/aicoder/gui/skill_runner.go              → SkillRunner 主逻辑, executeTask
D:/workprj/aicoder/gui/platform_windows.go          → 平台检测, findSh
D:/workprj/aicoder/gui/remote_execution_local_pty.go → 本地 PTY 执行
D:/workprj/aicoder/corelib/skill/skill_markdown.go  → SKILL.md 解析
D:/workprj/aicoder/corelib/skill/scanner.go         → Skill 扫描
```

#### 2. Shell 查找相关函数

| 函数 | 源文件 | 作用 |
|------|--------|------|
| `findSh` | `platform_windows.go` | **⚠️ 问题点** - 查找可用的 Unix shell |
| `firstAvailableLookPath` | `platform_windows.go` | 按 PATH 顺序查找 |
| `detectAvailableScriptRuntimes` | `platform_windows.go` | 检测脚本运行时 |
| `installGitBash` | `platform_windows.go` | 自动安装 Git Bash |
| `generateScript` | `skill_runner.go` | 生成步骤脚本文件 |
| `executeTask` | `skill_runner.go` | 执行 Skill 任务 |
| `platformLaunch` | `platform_windows.go` | 平台启动入口 |
| `parseSKILLMarkdown` | `skill_markdown.go` | 解析 SKILL.md |

#### 3. 关键字符串证据

从二进制中提取的硬编码路径（说明 Runner 知道 Git Bash 的存在）：

```
C:\Program Files\Git\bin\sh.exe
C:\Program Files\Git\usr\bin\sh.exe
C:\Program Files\Git\cmd
C:\Program Files\Git\bin\bash.exe
C:\Program Files\Git\usr\bin\bash.exe
```

#### 4. `findSh()` 推测的查找顺序问题

基于二进制字符串和运行时行为，`findSh()` 的查找策略**可能**是：

```
1. 硬编码路径检查（Git Bash 等）      → 应该命中但实际未命中？
2. PATH 顺序查找 (LookPath)           → 命中了 System32\bash.exe (WSL)
3. Scoop 路径                          → 不存在
4. MSYS2 / Cygwin 路径                 → 不存在
```

**核心问题**: `findSh()` 可能在硬编码路径检查阶段就失败了（原因待查），回退到 PATH 查找时，`C:\Windows\System32` 排在 `C:\Program Files\Git\bin` 之前，导致 WSL 的 `bash.exe` 被优先选中。

#### 5. 执行流程图

```
用户调用 run_skill("xxx")
    │
    ▼
parseSKILLMarkdown(SKILL.md)
    │
    ├── 解析 YAML frontmatter
    ├── 提取 Markdown 代码块（按 ```bash / ```powershell 分类）
    └── 替换占位变量 ({baseDir} 等)
    │
    ▼
generateScript(step) → 生成 step_XX.sh
    │
    ▼
findSh()                → ⚠️ BUG: 返回 WSL bash.exe 而非 Git Bash sh.exe
    │
    ├──► exec.Command(shPath, step_XX.sh)   → 通过 ConPTY 执行
    │       └── WSL bash.exe 无发行版 → 输出"没有已安装的分发" → exit 1
    │
    └──► 收集 stdout/stderr（⚠️ 信息丢失，仅返回 "exit status 1"）
```

---

## 测试验证

### 对照实验

| 测试 | 命令 | Shell | 结果 |
|------|------|-------|------|
| 手动 Git Bash | `& "C:\Program Files\Git\bin\bash.exe" -c "echo hello"` | Git Bash | ✅ 成功 |
| 手动 PowerShell 调 node | `node diag.mjs` | PowerShell → node | ✅ 成功 |
| Runner 执行 bash 代码块 | `echo "hello from skill runner"` | Runner → WSL bash | ❌ exit 1 |
| craft_tool (bash 语言) | `echo "hello from skill runner"` | craft_tool → WSL bash | ❌ exit 1 |
| craft_tool (powershell 语言) | 同上 | craft_tool → PowerShell | ✅ 成功 |

### 7 个 Skill 全量测试结果

| # | Skill | 状态 | 错误 | 代码块语言 |
|---|-------|------|------|-----------|
| 1 | `_test-echo` | ❌ | craft_tool failed: 无产物 | bash |
| 2 | `_test-minimal` | ❌ | craft_tool failed: 无产物 | bash |
| 3 | `_test-direct` | ❌ | craft_tool failed: 无产物 / exit 1 | bash |
| 4 | `_test-nq` | ❌ | craft_tool failed: 无产物 | bash |
| 5 | `_test-runner` | ❌ | bash failed: exit 1 | bash |
| 6 | `_test-full` | ❌ | bash failed: exit 1 | bash |
| 7 | `nodejs-env-doctor` | ❌ | bash failed: exit 1, skipped | bash |

**成功率: 0/7 (0%)**

---

## 问题分类

### 🔴 P0: `findSh()` 选错了 Shell（核心 Bug）

`findSh()` 在有 Git Bash 可用的情况下，返回了 WSL 的 `bash.exe`（不可用）。

**修复建议**: 硬编码路径应优先于 PATH 查找，且 PATH 查找应过滤掉已知的 WSL 路径：

```go
func (a *App) findSh() (string, error) {
    // 1. 硬编码路径（最高优先级）
    candidates := []string{
        `C:\Program Files\Git\bin\sh.exe`,
        `C:\Program Files\Git\usr\bin\sh.exe`,
        `C:\Program Files\Git\bin\bash.exe`,
        `C:\Program Files\Git\usr\bin\bash.exe`,
        // Scoop
        filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims", "bash.exe"),
        // MSYS2 / Cygwin
        `C:\msys64\usr\bin\bash.exe`,
        `C:\cygwin64\bin\bash.exe`,
    }
    for _, path := range candidates {
        if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
            return path, nil
        }
    }

    // 2. PATH 查找（必须排除 WSL）
    for _, name := range []string{"bash.exe", "sh.exe"} {
        if path, err := exec.LookPath(name); err == nil {
            if !isWSLBash(path) {
                return path, nil
            }
        }
    }

    return "", fmt.Errorf(
        "Skill Runner requires a Unix-compatible shell (e.g., Git Bash). " +
        "Install Git for Windows: https://git-scm.com/download/win")
}

func isWSLBash(path string) bool {
    abs, _ := filepath.Abs(path)
    lower := strings.ToLower(abs)
    return strings.Contains(lower, `windows\system32\bash`) ||
        strings.Contains(lower, `windowsapps\bash`) ||
        strings.Contains(lower, `windows\system32\sh.`)
}
```

### 🔴 P1: 纯输出型脚本被误判为"失败"

当脚本 exit code = 0 但未产出文件时，Runner 报告 `failed`。

**修复建议**: 区分 `exit_code != 0`（执行失败）和 `exit_code == 0 && no artifact`（执行成功但无产物），后者应视为成功。

### 🟡 P2: 错误信息缺失 stderr

失败时仅返回 `exit status 1`，不包含 WSL 的实际错误输出（"适用于 Linux 的 Windows 子系统没有已安装的分发"）。

**修复建议**: 完整捕获并返回 stderr 内容，方便用户和开发者排查。

### 🟡 P3: `{baseDir}` 变量替换不透明

SKILL.md 中的 `{baseDir}` 占位符替换是否生效无法确认，替换失败时也仅报 `exit 1`。

**修复建议**: 在 Runner 日志中记录变量替换结果。

---

## 优先级排序

| 优先级 | 问题 | 预期影响 |
|--------|------|---------|
| **P0** | `findSh()` 选错 Shell | 修复后所有 Skill 在 Windows 上可立即运行 |
| **P1** | 纯输出脚本误判失败 | 诊断类 Skill 可正常工作 |
| **P2** | stderr 信息丢失 | 大幅提升调试效率 |
| **P3** | 变量替换不透明 | 改善跨平台路径兼容性 |

---

*报告人：MaClaw Agent (安娜)*
*分析日期：2026-04-12*
*分析方法：MaClaw.exe 二进制字符串提取 + Go debug 符号分析 + 运行时行为对照实验*
*文件路径：`~/Desktop/Skill_Runner_Bug_Report.md`*
