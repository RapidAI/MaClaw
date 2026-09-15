# MaClaw 可清除代码 —— 独立复验报告

> 复验日期：2026-09-15 ｜ 仓库：`D:\workprj\aicoder`
> 被复验文档：[`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)（全量清单）｜[`DEAD-CODE-REPORT.md`](./DEAD-CODE-REPORT.md)（方法论）
> **本轮只做只读复验 + 文档修订，未删除任何文件。**

---

## 一、复验口径（与首轮的关键差异）

首轮的判定链条是：`deadcode` 静态调用图 → 符号交叉校验 → 字符串级复核。
本轮**不复用首轮的中间结果**，从零重新取证，并修正了两个方法论缺陷：

| 缺陷 | 后果 | 本轮做法 |
|---|---|---|
| **符号搜索不做包限定** | 别的包/别的类型里的**同名符号**会被当成引用（`corelib` 与 `corelib/remote` 各有一份 `DefaultRemoteHeartbeatSec`；guiapp 自己也定义了 `RemoteStartSessionRequest`） | 按**包限定**判定：同包无限定名、跨包必须 `alias.Symbol`，且 alias 来自该文件真实的 import 语句 |
| **注释被当成引用** | 文档注释里提到方法名（`// ArchiveVESession removes ...`）就算"有引用"，导致可删项被漏掉 | 先**剥离注释**再统计；字符串字面量保留（它是真实约束） |

同时新增一项首轮没做的检查：**非 Go 文件（json/yaml/ts/tsx/md/bat）里的字符串引用**，用于捕捉注册表、配置、前端动态调用。

---

## 二、逐类复验结论总表

| # | 文档条目 | 文档结论 | 复验结论 | 判定 |
|---|---|---|---|---|
| §1 | agent-unification 迁移半成品 | 4 组平行实现 | ✅ 复现。`corelib` 侧零外部引用；guiapp 侧在产（`im_confirmation_gate.go:139`、`hasDisplayableTaskUnderstanding`） | **确认** |
| §2.1.2 | 非 Opus 高置信死文件 13 个 | 全部可删 | ⚠️ **11 个确认**，2 个不成立（见 §3.1、§3.2） | **部分修正** |
| §2.2/2.3 | B 档 87 / C 档 92 | 分级 | 未逐个独立重验（依赖 deadcode 输出）；方向正确，**C 档勿删**的告诫成立 | 沿用 |
| §3.1 | 零引用包 9 个 | 3 个可删候选 + 3 个待确认 + 3 个回归装置 | ✅ 9 个全部复现为"生产+测试双双零 import" | **确认** |
| §3.2 | `corelib/tool/routingeval` 仅测试引用 | 回归基准 | ✅ 复现（被测试引用 1 处）。另发现 `internal/testfixtures` 同属此类（文档未列） | **确认 + 补充** |
| §4 | `gui/` 整目录可删 | 3 文件 + docs | ✅ **确认可删**；但 `docs/` 性质被误述（见 §3.8） | **确认（附更正）** |
| §5 | 前端孤儿 28 个 + 6 测试 | 列表 | ✅ **28 个逐一复现，零差异** | **确认** |
| §6 | Wails 死绑定 94 个 | 94 可删 + 168 降级 | ✅ 94 个全部复现；**另发现 1 个文档漏列**（见 §3.7） | **确认 + 补充** |
| §7 | `cmd/` 11 个子命令判定 | 8 个无构建引用、仅 1 个真孤儿 | ✅ 逐条复现（CI/脚本引用位置全部核对） | **确认** |
| §8 | `corelib/*/cmd/` 调试主程序 38 个 | 38 个 | ✅ 总数 38 复现；分解数有偏差（见 §3.6） | **确认（附更正）** |
| §9 | 版本库内垃圾 17 个 | 14 logs + 1 个 `.DS_Store` | ⚠️ 总数 17 巧合对上，构成不同（见 §3.4） | **修正** |
| §10 | sparse-checkout 未检出 1,513 个 | 12,628 索引 | ✅ 完全复现（12628 / 1513 / `core.sparseCheckout=true`） | **确认** |
| §11 | 4 个待提交删除 | 有 `D` 记录 | ✅ 4 条 `D` 记录仍在，未提交 | **确认** |
| §12 | hub/hubcenter 重复包 | 11 个 | ❌ **实为 12 个**（见 §3.5） | **修正** |
| §14 | 保护清单 | 9 项 | ✅ 全部核实（`replace`、`//go:embed`、4 个 wav 均在） | **确认** |

---

## 三、需要修正的 8 处

### 3.1 ❌ `corelib/knowledge/scan_images.go` —— **不能整文件删**

文档把它列为"整文件零引用"。实际：

```go
// corelib/knowledge/image_import.go:441-445
func (s *SQLiteStore) ProcessStandaloneImage(
	ctx context.Context, source Source, filePath string,
	refs []ImageReference,        // ← 引用了 scan_images.go 的类型
) (nodes []DocumentNode) {
```

而 `ProcessStandaloneImage` 在**生产路径**上：

```go
// corelib/knowledge/store.go:5518
imageNodes := s.ProcessStandaloneImage(ctx, source, item.FilePath, nil)
```

**结论**：`ImageReference` 类型是活的 → 删掉整个文件会编译失败。
**正确动作**：只删 5 个死符号（`BuildImageReferenceMap`、`ClassifyImageKind`、`extractRefContext`、`imageRefPatterns`、`resolveImageRefPath`），把 `ImageReference` 类型留在一个小文件里；或更彻底地去掉 `refs` 参数（两次扫描策略从未接线，生产里恒传 `nil`）。

### 3.2 ❌ `guiapp/im_tool_session_control_action.go` —— **不可删（文档误报）**

文档称"1 个函数，全无引用"。实际 5 个顶层符号里 **4 个在用**：

| 符号 | 引用者 |
|---|---|
| `sessionControlAction`（类型） | `im_tools_session_control.go:5`（`runSessionControlAction(..., action sessionControlAction)`） |
| `sessionControlActionInterrupt` | `im_tools_session_actions.go`、`im_tools_session_control.go`、`im_tools_session_control_test.go` |
| `sessionControlActionKill` | 同上 |
| `sessionControlActionUnknown` | `im_tools_session_control_test.go` |
| `normalizeSessionControlAction` | 无 ✅ 只有这个是死的 |

`sessionControlActionInterrupt/Kill` 出现在 `switch action { case ... }` 里，是**业务分支条件**，不是可选删除项。

### 3.3 ✅（确认）13 个里剩下的 11 个确实整文件零引用

`corelib/agent/selfconfirm.go`、`corelib/agent/task_understanding.go`、`corelib/im/convert.go`、`corelib/remote/{capability_market_auth,event_types,machine_profile,mobile_launch_types}.go`、`guiapp/{im_memory_recall_mode,im_tool_list_sessions,session_observer,session_terminal_status}.go`

包限定 + 剥注释后，**全部符号的跨文件引用数 = 0**。
其中 `corelib/agent/*` 两份还被 `docs/agent-security-mechanism.md` 以函数名提及 → 属"迁移收尾"而非"纯垃圾"（§1 已说明）。

> `corelib/remote/machine_profile.go`、`mobile_launch_types.go` 首轮之所以看着"有引用"，是**同名异包假引用**：`corelib/remote_heartbeat.go`（package `corelib`）自己定义了 `DefaultRemoteHeartbeatSec`；`guiapp/remote_mobile_launch.go:12,24` 自己定义了 `RemoteLaunchProject` / `RemoteStartSessionRequest`。两者互不相干。

### 3.4 ⚠️ §9 版本库内垃圾：**14 个 deploy 日志 + 3 个 `.DS_Store`**

```
deploy/logs/  14 个（full-* ×7、hub-only-* ×7）—— 不是文档说的 16 个
.DS_Store                        ← 已被首轮删除（工作区有 D 记录）
build/.DS_Store        14340 字节  ← ❌ 文档说"已在首轮删除"，实际仍在磁盘、仍被 git 跟踪
build/windows/.DS_Store 6148 字节  ← ❌ 同上
```

`git status` 只报了 4 条 `D`（`.DS_Store`、`audio_spectrum.png`、`audio_wave.png`、`maclaw2.png`），`build/` 下那两个从没被删过。

### 3.5 ❌ §12 hub/hubcenter 同名包是 **12 个**，不是 11

```
app  auth  backup  config  diagnostics  entry
httpapi  llmservice  mail  notification  skill  store
```

文档正文写"11 个"但列出的名单本身就是 12 个（数字笔误）。互相 import 均为 0 ✅ 复现。

### 3.6 ⚠️ §8 `corelib/*/cmd/` 分解数

| 项 | 文档 | 实际 |
|---|---:|---:|
| `corelib/tts/cmd/` | 33 | **32** |
| 其中 `piper_*` | 22 | **21** |
| `compare_*` | 5 | 5 ✓ |
| 其他（`asr_verify` `inspect_gguf` `kokoro_asr_eval` `synthesize` `tts_asr_test` `tts_from_py_enc`） | 6 | 6 ✓ |
| **corelib 下 main 包合计** | 38 | **38 ✓**（tts 32 + onnxrt 3 + asr 2 + ocr 1） |

### 3.7 ➕ §6 漏列 1 个死绑定：`TryHandlePassthroughSlashCommand`

```
guiapp/app_passthrough.go:127  func (a *App) TryHandlePassthroughSlashCommand(text string) (*IMAgentResponse, bool)
```
全仓唯一命中就是定义行；前端仅在一处**注释**里提到。→ 应在"可删"名单中，故实际为 **95 个**（95 + 168 降级 = 263 个前端未引用，与复验总数一致）。

### 3.8 ⚠️ `gui/docs/` 的性质被误述

文档写"3 文件 + docs"，读起来像设计文档。实际 **535 个 md 全是工作流 GUI 测试跑出来的夹具产物**：

```
gui/docs/workflow/coding/2026-08-08/04-implementation.md  （186 字节）
# Phase Output
- Functional item A
- Functional item B
- Functional item C
This document is long enough to pass the minimum quality gate and exercise GUI capture auto-advance behavior.
```

来源已定位：`guiapp/workflow_adapter_persistence.go:263` 把 Project_Storage 写到 `{projectPath}/docs/workflow/{type}/{date}/`，测试在 `gui/` 作为 CWD 时跑，产物就落在那里（`workflow_adapter_test.go:429` 正是断言这个路径）。

**判定不变：可删**（535 md 663 KB + 3 个编译不过的 test.go），且 `gui/` 整目录被 `.gitignore:79` 忽略、git 跟踪文件数 = 0，删除不影响版本库。

---

## 四、最终确认可清除清单

### P1 零风险（有独立证据、无任何引用）

| 对象 | 规模 | 证据 |
|---|---:|---|
| `gui/` 整个目录 | 538 文件 / 663 KB | `go test ./gui` 编译失败（`undefined: App / IMMessageHandler / ToolRouter`）；git 跟踪 0 文件 |
| `corelib/im/convert.go` | 1.6 KB | `IncomingFromMap`、`ToMap` 全仓 0 引用 |
| `corelib/remote/capability_market_auth.go` | 0.8 KB | 3 符号 0 引用 |
| `corelib/remote/event_types.go` | 1.7 KB | 3 符号 0 引用 |
| `corelib/remote/machine_profile.go` | 1.5 KB | 7 符号 0 引用（同名异包已排除） |
| `corelib/remote/mobile_launch_types.go` | 2.6 KB | 3 符号 0 引用（同名异包已排除） |
| `guiapp/im_memory_recall_mode.go` | 0.9 KB | 8 符号 0 引用 |
| `guiapp/im_tool_list_sessions.go` | 0.8 KB | 1 符号 0 引用，未注册进任何 map |
| `guiapp/session_observer.go` | 2.7 KB | 2 符号 0 引用 |
| `guiapp/session_terminal_status.go` | 1.0 KB | 8 符号 0 引用 |
| `cmd/quick_test/` | 767 B | 唯一命中是自身定义行；无脚本/CI/Makefile 引用 |
| `corelib/memoryshot/` | 16 KB | 生产+测试双双零 import（设计文档提到过，但代码已不再使用） |
| `corelib/skillmarket/` | 3.3 KB | 零 import，仅类型定义 |
| `corelib/misc/` | 16 KB | 零 import |
| `build/.DS_Store`、`build/windows/.DS_Store` | 20 KB | git 跟踪的垃圾（首轮漏清） |
| `deploy/logs/*` | 14 文件 | git 跟踪的运行日志/PID，建议 `.gitignore` + `git rm --cached` |
| 前端孤儿 28 个模块 + 6 个连带测试 | 275 KB | 可达性 + 字符串级复核双确认（`NotificationItem.tsx`、`preview/index.ts` 已按复核结果处理） |
| Wails 死绑定 **95** 个 | — | 前端 0 引用 + 剥注释后 Go 生产代码 0 引用 |

### P2 需先收敛再删（不是"删了就完"）

| 对象 | 原因 |
|---|---|
| `corelib/agent/{selfconfirm,task_understanding}.go` | 被 `docs/agent-security-mechanism.md` 以函数名提及，且 guiapp 有**已分叉**的在用版本 → 属**迁移收尾工程**，二选一 |
| `corelib/knowledge/scan_images.go` | 5 个死符号可删，但 `ImageReference` 类型被生产方法引用 → 需重构而非整删 |
| `guiapp/im_tool_session_control_action.go` | 只 `normalizeSessionControlAction` 可删 |
| `corelib/{feishu,wecom,dingtalk}/` | 与 `hub/internal/*` 同名不同定位（轻量网关 vs 完整适配器栈）→ **产品定取舍** |
| `corelib/{experience/counterfactual,longhorizon/eval,tool/routingarch}` | 回归装置 / 架构守卫，自带数据集，且 CI 不跑根模块全量 `go test` |
| `corelib/tts/cmd/` 21 个 `piper_*` 调试命令 | 与未接线的 piper 引擎同生共死，需确认无配置/模型指向 |
| `corelib/opus/` 死文件簇 | 见 §五 |

---

## 五、未独立重验 / 保留不确定性的项

| 项 | 说明 |
|---|---|
| §2.1.1 `corelib/opus/` 39 个死文件 | 本轮做了**部分**验证：`corelib/opus/libopus/silk_*.go` 共 96 个文件里，**95 个在 `corelib/opus` 之外零引用**（唯一"疑似有引用"的 `libc/time.go` 是 `Time`/`Timer` 通用词假命中）。文档的 39 个是其中更严格筛选（libopus 内部也无人调用）的子集，**结论可信但我未逐文件重算**。opus 被 `audioconv/opus.go`、`tts/opus_encode.go` 依赖 → 仍应单独立项 |
| §2.2 B 档 87 个 | 依赖 deadcode 输出的"部分符号死"，未逐个复验。**不要照单删** |
| §2.3 C 档 92 个 | 同上；本轮复验反而证明"同名符号假引用"确实是本仓库的主要误报来源，故 C 档**几乎可断定是误报** |
| 94/95 个死绑定的"是否可删" | 前端 0 引用已确认。但删 Go 侧方法会改变 `wailsjs/go/main/App.d.ts`，需重新生成绑定；`AccumulateLLMTokenUsage` 另有一处 `config_txn.go:282` 的**调用者名字符串**匹配（`strings.Contains(caller, "AccumulateLLMTokenUsage")`），删除时应一并清理 |

---

## 六、复验后建议的执行顺序

```
P1-a  gui/ 整目录删除（gitignored，无版本库影响）          → go build ./...
P1-b  11 个零引用 .go 文件 + cmd/quick_test + 3 个零引用包   → go build ./... && go vet ./...
P1-c  前端 28 个孤儿 + 6 个测试                             → cd guiapp/frontend && npm test
P1-d  build/*.DS_Store + deploy/logs 走 git rm --cached     → git status 应只剩你原本的 M
P2    95 个死绑定（含 TryHandlePassthroughSlashCommand）    → wails generate module
```

每步之后：

```powershell
go build ./...
go vet ./...
go test ./corelib/... ./guiapp/...
cd guiapp/frontend; npm test
node scripts/check-agent-architecture.mjs
```

> 已知差异：`CodePreviewPanel.mergedHeader.test.tsx` 在 HEAD 上就失败（属在途修改），对比时不要算作本次引入。
> 保护清单（`third_party/shine-mp3`、`vscode-ext/`、`guiapp/hello-maclaw.wav`、3 个根目录 wav、`corelib/pptx`、`cmd/pptx-preview`、`.codegraph/.gitignore`）本轮全部重新核实有效。

---

*复验手段：`go list` 全仓 import 图（240 包）· 包限定符号搜索（同包/跨包/别名感知）· 剥离注释的引用统计 · 非 Go 文件字符串引用扫描 · 前端 import 图传递可达性 + 字符串级复核 · git 索引与 CI/脚本引用核对。全程只读。*
