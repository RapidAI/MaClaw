# Skill 上传/下载/安装/使用 流程修复计划（最终实施版）

## 审计总结

对 MacLaw app 的 skill 全生命周期流程（上传/下载/安装/执行/删除/更新）进行了机制性审查，共发现并修复 **16 个问题**（10 个初始 + 6 个深入 review 发现），修改 **15 个文件**，新增 **1 个回归测试**。

**信任模型**：
- Hub/HubCenter 上的 skill = 可信任（自己部署，内部审核通过才上架）
- `UpdateFromHub` 和 `DownloadSkillHub` 设 trusted 是正确行为——不需要安全扫描
- ClawHub（社区镜像）和 GitHub 来源的 skill 需要完整安全扫描

---

## 修复清单

### Fix 1: `CommitStaging` 无条件 `RemoveAll(finalDir)` 丢失用户定制文件

**文件**：`corelib/skill/staging.go`

**根因**：重装/更新 skill 时先删后移，用户添加的配置文件永久丢失。

**修复**：
- 替换"删除→移入"为"备份→移入"：`os.Rename(finalDir, finalDir+".prev")`
- Rename 失败时 `os.RemoveAll(finalDir)`，RemoveAll 也失败时返回明确错误（不静默继续）
- `CleanupAllStale` 扩展清理 `.prev` 备份目录（24h 过期）

---

### Fix 2: `runUploadPortabilityGate` 误 rollback 已成功的 auto-fix

**文件**：`gui/im_tools_misc.go`

**根因**：auto-fix 修了 N 个问题但仍有残留时，已成功的 N 个修复被 rollback。

**修复**：`!result.Portable()` 时不再 rollback auto-fix。只在安全扫描失败时才 rollback。

---

### Fix 3: `SkillHubClient` HTTP 超时 10s 不足以下载大 skill 包

**文件**：`gui/skillhub_client.go`

**根因**：搜索和下载共用 10s 超时客户端。

**修复**：新增 `installClient`（120s 超时）专用于下载操作。搜索 API 保持 10s。`getBytesFromExplicitHubURL` 和 `getBytesFromExplicitHubURLWithAuth` 改用 `installClient`。

---

### Fix 4: `UploadNow` 对 tmpDir 做 auto-fix 但改动随 tmpDir 删除而丢失

**文件**：`gui/skill_lifecycle_manager.go`

**根因**：源目录的绝对路径问题未被修复，每次上传重复 auto-fix。

**修复**：打包前先对源目录执行 `PrepareSkillForUpload`（持久化 auto-fix），tmpDir 只做验证（`AutoFix: false`）。

---

### Fix 5: `Delete` 的 `removeSkillDirs` 可能误删同名不同来源的 skill

**文件**：`gui/app_nl_skills.go`

**根因**：按 name 扫描所有 roots 删除同名目录，不区分来源。

**修复**：优先使用被删 entry 的精确 `SkillDir` 删除，为空时才 fallback 到 name 扫描。

---

### Fix 6: TUI `skillRunDetailed` 不响应取消

**文件**：`tui/tool_manage_skill.go`

**根因**：使用 `context.Background()` 不从 parent context 派生。

**修复**：
- 从 `args["_ctx"]` 提取 caller 提供的 context（默认 `context.Background()`）
- 步间循环顶部新增 `execCtx.Err()` 检查实现步间取消
- Pipeline `RunSubSkill` 调用时注入 `args["_ctx"] = ctx` 传播 cancel 信号

---

### Fix 7: 安装后执行失败但 skill 状态为 active

**文件**：`gui/im_skill_hub_install.go` + `gui/app_nl_skills.go`

**根因**：`Execute` 失败时 skill 已注册为 active。

**修复**：
- 执行失败时调用 `UpdateStatus(skill.Name, "needs_setup")`
- 新增 `SkillExecutor.UpdateStatus` 方法

---

### Fix 8: `scanManagedSkillWriteback` 重复检查死代码

**文件**：`gui/im_tools_misc.go`

**修复**：删除第二个不可达的 `if report.NeedsUserReview()` 分支。

---

### Fix 9: GUI/TUI 上传质量门禁不对称

**文件**：`tui/tool_manage_skill.go`

**修复**：TUI `skillUpload` 新增 `UsageCount < 2` 和 `SuccessCount == 0` 质量门禁，与 GUI 对齐。

---

### Fix 10: ClawHub skill 无 staging 隔离

**文件**：`gui/im_skill_hub_install.go`

**修复**：ClawHub 安装路径创建 staging dir（`PrepareStagingDir`），使其走完整的 staging → security scan → commit 隔离流程。

---

### Fix 11 (Review R2): `.prev` 备份目录被 scanner 发现为 ghost skill

**文件**：`corelib/skill/scanner.go`

**根因**：`scanSkillDirInternal` 扫描所有子目录，`.prev` 内含有效 `skill.yaml` 被当作重复 skill。

**修复**：`scanSkillDirInternal` 跳过 `.prev` 后缀目录。

---

### Fix 12 (Review R2): `CleanupAllStale` 从未被调用——备份永远不清理

**文件**：`gui/skill_lifecycle_manager.go`

**修复**：`StartBackgroundProcessing` goroutine 启动时调用 `skill.CleanupAllStale(24 * time.Hour)`。

---

### Fix 13 (Review R2): `IsSkillRuntimePackageDir` 不排除 `.prev`——上传可能包含旧备份

**文件**：`corelib/skill/upload_preflight.go`

**修复**：`IsSkillRuntimePackageDir` 新增 `.prev` 后缀检查。

---

### Fix 14 (Review R3): `_ctx` 被 `NormalizeRunVars` 转为 `"context.Background"` 字符串污染 vars

**文件**：`corelib/skill/run_context.go`

**根因**：`canonicalRunVarKey("_ctx")` → `"ctx"`（前导下划线被 Trim），`isRunControlKey` 匹配不到，`fmt.Sprintf("%v", ctx)` 产生垃圾字符串。

**修复**：在迭代循环中 type-assert `context.Context` 直接跳过——不依赖 key name，检查 value type。新增 `"context"` import。

---

### Fix 15 (Review R4): TUI 全部 6 个 `ExecuteTool` 实现中只有 1 个注入 `_ctx`

**文件**：`tui/app.go`（2处）、`tui/weixin_gateway.go`、`tui/pipe_mode.go`、`tui/loop_command.go`、`tui/agent_tools_schedule.go`

**根因**：top-level agent tool 调用不传播 cancel context 到 skill 执行。

**修复**：所有 6 个 `ExecuteTool` 实现统一注入 `args["_ctx"] = ctx`。

---

### Fix 16 (Review R5): `CommitStaging` fallback `RemoveAll` 失败时静默继续导致后续操作在非空目录上失败

**文件**：`corelib/skill/staging.go`

**修复**：`os.RemoveAll(finalDir)` 失败时返回明确错误（包含 rename 和 remove 两个失败原因），不再静默继续。

---

## 新增测试

### `TestNormalizeRunVarsExcludesContextValue`

**文件**：`corelib/skill/run_context_test.go`

验证 `context.Context` 值不会泄漏为 template 变量。防止未来改动破坏 context 过滤逻辑。

---

## 修改文件清单（15 个）

| # | 文件 | 改动 |
|---|------|------|
| 1 | `corelib/skill/staging.go` | CommitStaging 备份机制 + CleanupAllStale .prev 清理 + RemoveAll 失败返回错误 |
| 2 | `corelib/skill/scanner.go` | 跳过 .prev 后缀目录 |
| 3 | `corelib/skill/upload_preflight.go` | IsSkillRuntimePackageDir 排除 .prev |
| 4 | `corelib/skill/run_context.go` | context.Context type-assert 跳过 + 新增 context import |
| 5 | `corelib/skill/run_context_test.go` | 新增 TestNormalizeRunVarsExcludesContextValue |
| 6 | `gui/im_tools_misc.go` | 移除误 rollback + 删除死代码 |
| 7 | `gui/skillhub_client.go` | installClient 120s 超时 |
| 8 | `gui/skill_lifecycle_manager.go` | 源目录 auto-fix + CleanupAllStale 启动调用 |
| 9 | `gui/app_nl_skills.go` | Delete 精确路径 + UpdateStatus 方法 |
| 10 | `gui/im_skill_hub_install.go` | needs_setup 标记 + ClawHub staging |
| 11 | `tui/tool_manage_skill.go` | _ctx 提取 + 步间取消 + pipeline ctx 传播 + 质量门禁 |
| 12 | `tui/app.go` | ExecuteTool _ctx 注入（tuiCallbacks + tuiBtwCallbacks） |
| 13 | `tui/weixin_gateway.go` | ExecuteTool _ctx 注入 |
| 14 | `tui/pipe_mode.go` | ExecuteTool _ctx 注入 |
| 15 | `tui/loop_command.go` | ExecuteTool _ctx 注入 |
| 16 | `tui/agent_tools_schedule.go` | ExecuteTool _ctx 注入 |

---

## 不需要修复的项

- **`extractBundledSkillFiles` 路径遍历**：已有 `filepath.Rel` + `..` 前缀检查 + symlink 检测，确认安全
- **`copyDir` symlink 处理**：已显式拒绝 symlink
- **`PrepareStagingDir` 竞态**：单线程调用，无并发风险
- **`UpdateFromHub` 无安全扫描**：Hub/HubCenter 是可信来源，不需要安全扫描
- **`DownloadSkillHub` 设 trusted**：从自己的 Hub 下载理应信任
- **ClawHub/GitHub 安全扫描**：`registerAndExecuteSkill` 已覆盖
- **Windows 文件锁重试**：已有 `fileutil.renameAtomicFile` 模式但用于单文件；目录级 retry 过度工程化，当前返回明确错误足够
- **`UploadNow` 重复 auto-fix**：idempotent 且 <50ms，不值得优化
- **降级场景 `.prev` ghost skill**：降级是不支持的操作路径

---

## Review 轮次记录

| 轮次 | 角度 | 发现 |
|------|------|------|
| R1 | 初始审计 | 10 个问题（Fix 1-10） |
| R2 | 隐藏交互 | .prev scanner ghost + CleanupAllStale dead code + IsSkillRuntimePackageDir |
| R3 | `_ctx` 变量污染 | canonicalRunVarKey Trim 掉 `_` 前缀 → isRunControlKey 不匹配 |
| R4 | 架构一致性 | 6 个 ExecuteTool 实现只有 1 个注入 _ctx |
| R5 | 并发安全 + 错误恢复 | CommitStaging RemoveAll 失败静默继续 |
| R6 | 向后兼容 | 旧 entries 无 SkillDir 的 fallback / 降级 / nil guard |
| R7 | API 契约 | canonicalRunVarKey("_ctx")="ctx" bug → type-assert 修复 |
| R8 | 测试覆盖 | 新增 TestNormalizeRunVarsExcludesContextValue 回归测试 |

---

## 验收标准

- 所有 15 个修改文件零诊断错误 - 新增回归测试覆盖 context 过滤逻辑 - Cancel 信号传播链完整：用户 Ctrl+C → cancelCh → ctx → args["_ctx"] → baseCtx → execCtx → subprocess kill - .prev 备份不被 scanner 发现、不被上传打包、24h 自动清理 - 旧版 skill（无 SkillDir）删除行为不变 - CommitStaging 所有失败路径返回明确错误 