# MaClaw 可清除代码 —— 第二轮独立复验报告

> 复验日期：2026-09-15（第二轮）｜ 仓库：`D:\workprj\aicoder` ｜ **全程只读，未删除任何文件**
> 上一轮：[`DEAD-CODE-VERIFICATION.md`](./DEAD-CODE-VERIFICATION.md)（第一轮复验）
> 被复验文档：[`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)（全量清单）｜[`DEAD-CODE-REPORT.md`](./DEAD-CODE-REPORT.md)（方法论）
> **冲突时以本报告为准。**

---

## 〇、本轮任务与结论速览

第一轮复验结尾留了 **4 项「未独立重验」** 的空白（`DEAD-CODE-VERIFICATION.md` §五）。本轮专门补齐这 4 项，并附带重核保护清单、CI 引用与工作区状态。

| # | 遗留项 | 本轮结论 |
|---|---|---|
| 1 | `corelib/opus/` 39 个死文件 | ❌ **实际 57 个**（文档 HIGH 39 ∩ 复验 38，剔除 1 个误判、新增 19 个） |
| 2 | MID 档 87 个 | ⚠️ **8 个实为全死**（应升 HIGH），79 个确认部分死 |
| 3 | LOW 档 92 个 | ❌ **61 个分档错误**：22 个实为全死、39 个实为部分死；仅 31 个真正"全活" |
| 4 | 95 个死绑定的可删性 | ✅ **确认可删**；排除 4 项接口假引用、定位 3 处需连带清理的字符串 |

外加 6 项新发现（详见 §三·补 / §四）：

- **`scripts/check-agent-architecture.mjs` 是架构门禁**，用 180 条 `requireText` 文本断言钉住源码；其中 **3 个候选文件被钉**，含一个"Go 侧零引用但删了 CI 就红"的函数（`DiffSnapshotBytes`）。
- **2 个候选文件是你当前的在途修改（`M`）**，其中 `corelib/agentservice/semantic_behavior_snapshot.go` 同时被门禁钉住 —— 是最危险的一条。
- **3 个判死文件被源码自述否决**：`corelib/database/handler.go`（有意保留的兼容入口）、`corelib/remote/capability_market_auth.go`（半完成的改名迁移，底层有 6 处在用）、`guiapp/session_observer.go`（未接线的共享助手）→ 含 2 个此前被列为 "P1 零风险" 的项。
- 工作区 `M` 已从 65 个增至 **81 个**，执行清理前需重新确认保护范围。
- `docs` 保护清单里 **`vscode-ext/` 的保护理由指错了目录**（embed 的是 `guiapp/vscode_ext_asset/`）。
- 前端孤儿 `src/components/preview/index.ts` **处于未跟踪目录内**，属在途新增文件，不应按死代码删。

**分档对比**：

| | HIGH | MID | LOW | 合计 |
|---|---:|---:|---:|---:|
| 文档 | 52 | 87 | 92 | 231 |
| **复验** | **79** | **121** | **31** | 231 |

转移矩阵：

```
文档档        ->HIGH   ->MID   ->LOW     合计
HIGH             49       3       0       52
MID               8      79       0       87
LOW              22      39      31       92
```

---

## 一、复验方法（与首轮、一轮的差异）

第一轮已经修掉了「符号搜索不做包限定」与「注释算引用」两个缺陷。本轮在此之上重建了一套**从零独立的分析链**，不复用任何前轮中间结果：

1. **重建索引**：遍历工作区 6,080 个 `.go` 文件，用状态机剥离 `//` 与 `/* */` 注释（保留字符串字面量，因为 `strings.Contains(caller, "X")` 是真实约束），抽取每个文件的顶层符号（func / method / type / var / const）。
2. **包限定引用判定**：一个符号算"被引用"仅当
   - 同包（同目录）其它文件出现**无限定裸名**；或
   - 其它包出现 `alias.Symbol`，且 **alias 取自该文件真实的 import 语句**、解析结果等于目标包的 import 路径；
   - selector 前缀不是 import 别名、又在非本包出现 → 记为 **uncertain（不算引用）**，这是同名异包的判定依据。
3. **反向 import 图限定扫描范围**：只扫「目标包自身 + 所有 import 了目标包的包」，避免全仓同名误伤。反向图由**源码里的 import 语句**构建，因此独立 module（`datasrv`）的引用也能捕获。
4. **文件级判定**：`refs` 只统计**其它文件**的引用；同文件内部引用单独记为 `intra`。文件内所有符号 `refs == 0` → 整文件零引用（HIGH）。
5. **非 Go 引用扫描**：对全部"被判死"的 862 个符号，扫 7,499 个非 Go 文件（yaml/json/ts/md/ps1/bat/py/html…）。
6. **脚本/CI/构建引用扫描**：扫 `scripts/`、`.github/`、`deploy/`、`tools/`、`vscode-ext/` 与根构建脚本共 103 个文件。
7. **前端侧**：770 个源文件（**排除 `wailsjs/` 自身**，否则生成 stub 会自证引用）、Go 侧剥注释 + 排除定义行、接口满足性核查、`dist` 打包产物辨析。
8. **保护清单**：枚举全部 27 条 `//go:embed` 并逐个验证目标存在。

**方法标定**：先用第一轮已知结论（`scan_images.go` 应活、`im_tool_session_control_action.go` 应活、其余 11 个应死）跑一遍，13 项**逐项复现、活符号名单完全一致**，再投入全量 231 个候选。

**假死防护检查**（全部通过）：

| 风险 | 检查结果 |
|---|---|
| 跨 module 引用被漏 | 仓库内 5 个 `go.mod`：仅 `datasrv` 引用 corelib，且只引 `corelib/structureddata`（不在 231 候选中）→ 无影响 |
| 点导入（dot import）绕过包限定 | 全仓 0 处 |
| 平台后缀文件（`*_windows.go`） | 已包含在扫描范围内，不做平台过滤（保守） |
| `deadcode` 对 Wails/注册表固有误报 | 本轮不使用 `deadcode` 的输出做判定，只用 `go list` 的编译级 import 图 |

---

## 二、最高频误报源：同名异包（本轮的核心发现）

**文档 MID/LOW 档大面积失准的根因**：上一轮的符号交叉校验**没有做包限定**，导致本仓库里极其普遍的同名异包符号互相"证明对方活着"。

三组实证（已用**限定形式**检索全仓，命中数 = 0）：

| 目标（被判死） | 骗过交叉校验的同名实现 | 真实调用者用的是 |
|---|---|---|
| `corelib/llm/context_compressor.go` 的 `ContextCompressor` / `NewContextCompressor` / `ShouldCompress` | `guiapp/context_compressor.go`、`corelib/context/compressor.go` | `corelib/agent/compress.go:49` 用 `corelib/context.Compressor`；guiapp 用自己那份 |
| `corelib/remote/event_coalescer.go` 的 `EventCoalescer` / `NewEventCoalescer` / `Enqueue` | `guiapp/remote_event_coalescer.go`（同名 5 方法全齐） | `guiapp/remote_session_manager.go:1307` 调的是 **guiapp 自己那份** |
| `corelib/remote/image_helpers.go` 的 `ImageOutputSizeLimit` / `ValidateImageTransferMessage` | `guiapp/remote_image_helpers.go:10` 同名常量 | `guiapp/remote_screenshot.go:175`、`remote_session_manager.go:2466` 用 guiapp 那份 |
| `corelib/plugin/bootstrap.go` 的 `Bootstrap` | `hub/internal/app/bootstrap.go:47`、`hubcenter/internal/app/bootstrap.go:26` | 各自模块内的 Bootstrap |

检索验证：

```powershell
# 全仓 0 命中 → 这些 corelib 副本确实无人调用
grep -rn "llm\.(ContextCompressor|NewContextCompressor|ShouldCompress)" --include=*.go .
grep -rn "remote\.(EventCoalescer|NewEventCoalescer|ImageOutputSizeLimit|ValidateImageTransferMessage)" --include=*.go .
grep -rn "plugin\.(Bootstrap|DiscoveryManager)" --include=*.go .
# => No matches found
```

**结论**：这不是零散的判定错误，而是一个**系统性模式** —— `corelib/*` 里有一批实现被复制到 `guiapp/*`、`hub/*` 后各自演化，guiapp/hub 用的是自己那份，corelib 那份成了死副本。这与文档 §12「重复实现」是同一类问题，但**方向明确**：corelib 侧可删，guiapp/hub 侧在用。

---

## 三、逐项复验结果

### 3.1 ✅ 项 1：`corelib/opus/` 死文件簇 39 → **57**

231 个候选中 opus 文件共 67 个：**57 全死 / 7 部分死 / 3 全活**。

| | 数量 |
|---|---:|
| 文档 HIGH 的 opus 文件 | 39 |
| 复验 HIGH 的 opus 文件 | **57** |
| 交集 | 38 |
| **复验剔除** | 1 |
| **复验新增** | 19 |

> **剔除**：`corelib/opus/internal/libc/wctype.go` —— 文档判全死，实际 `WChar` 有引用（复验檔：MID 2/3）。
> 上一轮残留的疑问（`libc/time.go` 的 `Time`/`Timer` 是否只是通用词假命中）本轮确认为**假命中**：`time.go` 的 9 个符号（`Clock`/`Time`/`Timer`/`LocalTime`/`GetTime`…）在包限定下全部零引用。

**新增的 19 个**分布在 `internal/libc`（4：`assert`/`errno`/`rand`/`sort`/`stdlib`）与 `libopus`+`silk`（15，主要是 `silk_float_*_FLP` 浮点分析族与 `silk_sort`/`silk_interpolate`/`silk_stereo_*`）。

**判定不变的部分**：opus 仍须**单独立项**（`corelib/audioconv/opus.go`、`corelib/tts/opus_encode.go` 依赖本包），且这批函数是**移植未完成的预留实现**。57 个这个数字提升的只是"可删规模"，不改变"风险高、勿顺手删"的结论。

**opus 部分死（7 个）**——只能逐符号删：

| 文件 | 死/总 |
|---|---|
| `corelib/opus/internal/libc/wstring.go` | 15/20 |
| `corelib/opus/internal/libc/runtime.go` | 9/13 |
| `corelib/opus/libopus/silk_float_SigProc_FLP.go` | 5/6 |
| `corelib/opus/celt/celt_lpc.go` | 3/5 |
| `corelib/opus/silk/control_SNR.go` | 3/4 |
| `corelib/opus/silk/enc_API.go` | 3/4 |
| `corelib/opus/internal/libc/wctype.go` | 2/3 |

### 3.2 ⚠️ 项 2：MID 档 87 个 → **79 个部分死 + 8 个实为全死**

**应升 HIGH 的 8 个**（文件内所有符号跨文件零引用，与文档 HIGH 档判据完全同源）：

| 文件 | 符号数 | 全部死符号 |
|---|---:|---|
| `corelib/opus/internal/libc/assert.go` | 2 | `Assert`, `AsPtr` |
| `corelib/opus/internal/libc/errno.go` | 8 | `Errno`, `errno`, `goerr`, `strError` … |
| `corelib/opus/internal/libc/rand.go` | 3 | `RandMax`, `Rand`, `SeedRand` |
| `corelib/opus/internal/libc/stdlib.go` | 3 | `Atoi`, `Atof`, `FuncName` |
| `corelib/progress/queue.go` | 9 | `DefaultMessageQueueCapacity`, `QueuedMessage`, `MessageQueue` … |
| `corelib/remote/provider_resolver.go` | 8 | `ProviderResolveResult`, `ProviderResolver`, `Resolve` … |
| `corelib/remote/startup_responder.go` | 8 | `StartupSessionWriter`, `StartupAutoResponder`, `Feed` … |
| `guiapp/self_review_adapters.go` | 10 | `sessionStatsAdapter`, `RecordCompletion`, `CompletedSessionsSince` … |

**没有任何 MID 条目降为 LOW** —— 文档"部分符号死"的判定方向正确，只是漏掉了 8 个全死的。

**其余 79 个确认部分死**，其中需优先注意的一组是"**只差一个开关就全死**"的：

| 文件 | 死/总 | 活着的那个符号 |
|---|---|---|
| `corelib/remote/session_stall_detector.go` | 14/15 | 仅 1 个符号被测试引用 |
| `corelib/remote/session_monitor.go` | 13/14 | 同上 |
| `corelib/remote/sdk_types.go` | 16/18 | 2 个类型别名 |
| `guiapp/context_compressor.go` | 11/13 | 2 个符号 |
| `guiapp/im_message_handler_workflow_initiate.go` | 15/24 | 9 个 |

### 3.3 ❌ 项 3：LOW 档 92 个 → **22 全死 + 39 部分死 + 31 全活**

文档 §2.3 的原话是「这些文件的所有符号都能在别处找到引用 …… **几乎可断定是误报**」，并特别点名 `corelib/plugin/adapter_{mcp,nlskill,script}.go`、`corelib/remote/{session_monitor,session_stall_detector,session_io_relay,event_coalescer}.go`。

**这个告诫的方向是反的**：被点名的那几个恰恰是本次新查实的死/半死项。

#### A. LOW 中实为全死（应升 HIGH）—— 22 个（其中 1 个经"自述"复核后撤销，见 §3.5）

| 文件 | 符号数 | 代表死符号 |
|---|---:|---|
| ⛔ `corelib/database/handler.go` | 1 | `Handle` —— **自述为有意保留的兼容入口，不可删**（§3.5） |
| `corelib/guiautomation/tools.go` | 4 | `RegisterTools`, `strArg`, `intArg`, `joinLines` |
| `corelib/opus/internal/libc/sort.go` | 8 | `ptrSort`, `Len`, `elem`, `elems`, `Less`, `Swap` |
| `corelib/opus/libopus/silk_LPC_analysis_filter.go` | 2 | `USE_CELT_FIR`, `silk_LPC_analysis_filter` |
| `corelib/opus/libopus/silk_LPC_inv_pred_gain.go` | 3 | `QA`, `LPC_inverse_pred_gain_QA_c` … |
| `corelib/opus/libopus/silk_float_apply_sine_window_FLP.go` | 1 | `silk_apply_sine_window_FLP` |
| `corelib/opus/libopus/silk_float_autocorrelation_FLP.go` | 1 | `silk_autocorrelation_FLP` |
| `corelib/opus/libopus/silk_float_bwexpander_FLP.go` | 1 | `silk_bwexpander_FLP` |
| `corelib/opus/libopus/silk_float_energy_FLP.go` | 1 | `silk_energy_FLP` |
| `corelib/opus/libopus/silk_float_k2a_FLP.go` | 1 | `silk_k2a_FLP` |
| `corelib/opus/libopus/silk_float_schur_FLP.go` | 1 | `silk_schur_FLP` |
| `corelib/opus/libopus/silk_float_sort_FLP.go` | 1 | `silk_insertion_sort_decreasing_FLP` |
| `corelib/opus/libopus/silk_float_warped_autocorrelation_FLP.go` | 1 | `silk_warped_autocorrelation_FLP` |
| `corelib/opus/libopus/silk_interpolate.go` | 1 | `silk_interpolate` |
| `corelib/opus/libopus/silk_sort.go` | 2 | `silk_insertion_sort_increasing` … |
| `corelib/opus/libopus/silk_stereo_find_predictor.go` | 1 | `silk_stereo_find_predictor` |
| `corelib/opus/libopus/silk_stereo_quant_pred.go` | 1 | `silk_stereo_quant_pred` |
| `corelib/plugin/bootstrap.go` | 1 | `Bootstrap` |
| `corelib/remote/image_helpers.go` | 6 | `ImageOutputSizeLimit`, `IsValidImageMediaType` … |
| `corelib/remote/mode_hash.go` | 2 | `LaunchFingerprint`, `ModeChanged` |
| `corelib/remote/session_completion_analyzer.go` | 6 | `CompletionAnalyzer`, `completionSignals` … |
| `corelib/remote/session_io_relay.go` | 7 | `SessionIORelay`, `Subscribe`, `Unsubscribe` … |

#### B. LOW 中实为部分死（应升 MID）—— 39 个

| 文件 | 死/总 | 代表死符号 |
|---|---:|---|
| `corelib/agent/confirmation_status.go` | 2/5 | `ConfirmationStatusUnknown`, `ConfirmationStatusPending` |
| `corelib/agent/tool_subagent.go` | 2/6 | `SubAgentSpec`, `BuiltinSubAgents` |
| `corelib/browser/launch_error_kind.go` | 3/5 | `browserLaunchErrorKind` 及两个常量 |
| `corelib/configfile/claude_hook_injector.go` | 1/3 | `escapeJSON` |
| `corelib/guiautomation/input.go` | 1/10 | `ErrUnsupportedPlatform` |
| `corelib/im/router.go` | 1/9 | `Router` |
| **`corelib/intent/keyword_registry.go`** | 1/6 | `KeywordMatch` ⚠️ **该文件是在途修改，勿动** |
| `corelib/llm/context_compressor.go` | 6/7 | `ContextCompressor`, `NewContextCompressor`, `ShouldCompress` … |
| `corelib/llm/failover_error_marker.go` | 6/8 | `failoverErrorMarker` 及 5 个分类常量 |
| `corelib/memory/archiver.go` | 2/6 | `LLMSummarizer`, `formatEntryContent` |
| `corelib/memory/backend_json.go` | 1/16 | `jsonFileBackend` |
| `corelib/needledata/eval.go` | 2/14 | `EvalBucket`, `EvalMismatch` |
| `corelib/needledata/logger.go` | 2/5 | `Logger`, `Log` |
| `corelib/needledata/report.go` | 4/11 | `DatasetDuplicateSample`, `DatasetTaskStats` … |
| `corelib/opus/celt/celt_lpc.go` | 3/5 | `LPC_ORDER`, `celt_fir_c`, `celt_iir` |
| `corelib/opus/libopus/silk_float_SigProc_FLP.go` | 5/6 | `silk_sigmoid`, `silk_log2` … |
| `corelib/opus/silk/control_SNR.go` | 3/4 | `silk_TargetRate_{NB,MB,WB}_21` |
| `corelib/plugin/adapter_script.go` | 1/9 | `execute` |
| `corelib/plugin/discovery.go` | 1/6 | `DiscoveryManager` |
| `corelib/plugin/manifest.go` | 3/6 | `knownPluginTypes`, `typeConfigKeys`, `rawManifest` |
| `corelib/remote/admin_windows.go` | 1/4 | `checkProcessElevated` |
| `corelib/remote/event_coalescer.go` | 7/8 | `EventCoalescer`, `NewEventCoalescer`, `Enqueue` … |
| `corelib/remote/preview_buffer.go` | 3/4 | `RingPreviewBuffer`, `NewRingPreviewBuffer` … |
| `corelib/remote/sdk_types.go` | 16/18 | `SDKMessage`, `SDKContentBlock` … |
| `corelib/remote/session_monitor.go` | 13/14 | `SessionMonitor`, `NewSessionMonitor`, `StartWatching` … |
| `corelib/remote/session_stall_detector.go` | 14/15 | `StallDetector`, `NewStallDetector` … |
| `corelib/remote/summary_reducer.go` | 3/4 | `ClaudeSummaryReducer` … |
| `corelib/skill/dependency.go` | 1/5 | `DependencyResolution` |
| `corelib/tool/skill_memory.go` | 2/7 | `maxCapabilitySummaryItems` … |
| `corelib/tts/melotts.go` | 1/5 | `MeloTTSModel` |
| `corelib/tts/piper_g2p_en_rules.go` | 3/4 | `enSyllable`, `enRule`, `enTranslitRules` |
| `corelib/yolo/blocks.go` | 1/6 | `C2f` |
| `guiapp/browser_replay_scheduler.go` | 1/6 | `bgLoopMgrAdapter` |
| `guiapp/im_agent_reply_quality.go` | 3/4 | `substantive*Re` 三个正则 |
| `guiapp/im_tools_create_session_context.go` | 1/2 | `createSessionContextResolution` |
| `guiapp/im_tools_create_session_project.go` | 1/4 | `createSessionProjectSelection` |
| `guiapp/prompt_skill_index.go` | 1/2 | `promptSkillIndexLimit` |
| `guiapp/scheduled_action_type_kind.go` | 3/5 | `scheduledActionTypeKind` 及常量 |
| `guiapp/screenshot_native_windows.go` | 9/20 | `procGetDesktopWindow`, `procBitBlt` … |

> 另有 `guiapp/im_passthrough.go`、`guiapp/im_confirmation_action.go` 等 6 个 LOW 项在复验中维持"全活"，但下一节的架构门禁会再筛一遍。

### 3.4 ✅ 项 4：95 个 Wails 绑定 —— 确认可删（附 3 项前提）

**前端侧**：770 个源文件全扫（排除 `wailsjs/`），96 个方法名（95 + `GetMaclawLLMCurrentProviderX` 占位）中**仅 1 处命中**：

```
guiapp/frontend/src/components/ai/useAIAssistant.ts:4899-4900
                // Synchronous response (e.g. handleAgentViewControlMessage,
                // TryHandlePassthroughSlashCommand). Process immediately.
```

逐行确认这是 `//` **注释块内**的文字 → 第一轮的 `+1` 修正确立，`TryHandlePassthroughSlashCommand` 归入可删。

**Go 侧**（6,080 文件，剥注释 + 排除定义行）：

| 类别 | 数量 | 明细 |
|---|---:|---|
| 完全零引用 | **89** | — |
| 唯一"调用"是同名异接收者 | 1 | `GetIMAuditStats` ← `MaClawSrv/http_im.go:245` 的 `s.svc.GetIMAuditStats(...)`（不同包、不同接收者） |
| 命中字符串字面量 | 5 | 见下表 |

| 字符串命中 | 位置 | 性质 | 处置 |
|---|---|---|---|
| `AccumulateLLMTokenUsage` | `guiapp/config_txn.go:280` | `strings.Contains(caller, "...")` 日志抑制名单 | 删方法时**连同该字符串分支清理** |
| `ForkConversationToProject` | `guiapp/app_project_search.go:4447` | `log.Printf` 的日志标签 | 可留可删，非约束 |
| `VerifyAndActivateNLSkill` | `guiapp/app_nl_skills.go:5500` | 错误提示语 "call VerifyAndActivateNLSkill after …" | **该方法是工作流提示的一部分**，删则提示悬空，建议保留或同步改文案 |
| `ListTemplates` | `datasrv/structureddata/mis_schema_catalog_test.go` | `datasrv` 自己的同名函数 | 无关 |
| `SearchSkillHub` | `tui/commands/skillhub_security_test.go` | `tui` 自己的同名函数 | 无关 |

**接口满足性风险已排除**（第一轮未做）：4 个方法名出现在 `guiapp/manager_interfaces.go` 的接口里：

```
GetSessionStarter   <- SessionManagerInterface   (manager_interfaces.go:54)
GetGossipClient / GetSkillHubClient / GetGossipAutoPublish <- NetworkManagerInterface (:101)
```

但接口签名是 `GetXxx() interface{}`，实现方是 **`*NetworkingManager`（`app_managers.go:501/511/521`）、`*SessionManager`（:322）、`*MockNetworkManager`（:268/270/272）**；而 `*App` 的同名方法返回**具体指针**（`app_manager_access.go:51/59/63/157`）→ **签名不符，`*App` 未实现这些接口** → 删除不会破坏接口满足性。

**`dist/` 辨析**：`guiapp/frontend/dist/assets/wails-*.js` 里能搜到全部 1269 个方法名，因为它是打包后的 **Wails 生成 stub 模块**，随构建重新生成 —— 不是调用证据。判定必须基于源码（排除 `wailsjs/`）。

**结论**：95 个可删，前提是 ① 删后重新生成 `wailsjs/`；② 清理 `config_txn.go:280` 的字符串分支；③ `VerifyAndActivateNLSkill` 单独确认文案。

---

## 三·补、被源码自述否决的三项（坑 7：零引用 ≠ 死代码）

对全部 231 个候选的文件头注释做了一次语义扫描（关键词：`retained` / `backward compat` / `legacy` /
`preserve compatibility` / `intentionally` / `deferred` / `placeholder` / `unwired`），**15 个文件命中**。
其中 **3 个直接改变了删除判定**：

| 文件 | 自述原文 | 推翻的结论 | 修正后判定 |
|---|---|---|---|
| `corelib/database/handler.go` | *"Handle executes the shared database tool contract. **It is retained as the short, source-compatible entry point for hosts that used the original database package API**; HandleTool contains the single authoritative implementation"* | 文档 LOW、复验 HIGH（我判"全死"） | **⛔ 不可删** —— 是有意保留的**对外兼容入口**（`Handle` 只是 `HandleTool` 的薄封装） |
| `corelib/remote/capability_market_auth.go` | *"CapabilityMarketAuthClient is the unified name … **a type alias for backward compatibility**"*；*"This is the **preferred** constructor name; NewSkillMarketAuthClient **is retained as a backward-compatible alias**"* | 文档 HIGH、一轮复验"P1 零风险"、本轮复验 HIGH | **⛔ 不可删** —— 是**半完成的改名迁移**（详见下） |
| `guiapp/session_observer.go` | *"SendAndObserveSession sends input to a remote session … **It is intentionally a thin shared helper so IM tools and other callers can reuse the same polling semantics without duplicating logic**"* | 文档 HIGH、一轮"P1 零风险"、本轮 HIGH | **⚠️ 降级为"待确认"** —— 是未接线的重构遗留，不是垃圾（详见下） |

### 🔍 `capability_market_auth.go` —— 半完成的改名，最容易被当成垃圾删

该文件只有 3 个符号，全部零引用，从"被引用"角度看确实是死的。但它做的是一件**故意的**事：

```go
type CapabilityMarketAuthClient = SkillMarketAuthClient      // 类型别名
type CapabilityMarketAuthResult = SkillMarketAuthResult
func NewCapabilityMarketAuthClient() *CapabilityMarketAuthClient {
    return NewSkillMarketAuthClient()
}
```

底层实现（`corelib/remote/skillmarket_auth.go`）**非常活跃**，调用方遍布 6 个文件：

```
tui/commands/skillmarket_auth.go:50,88,115,142,182
tui/commands/remote.go:242
guiapp/skillmarket_client.go:517
guiapp/remote_activation.go:1923
guiapp/pet_bridge.go:262
corelib/agentservice/skills.go:620
```

→ 这是**"SkillMarket → CapabilityMarket"改名进行到一半**：新名字（"preferred"）先落地，调用方一个还没迁。
删掉它等于把已宣告的新 API 又撤回。**属迁移收尾工程，要二选一收敛**（迁调用方，或明确放弃新名），
与文档 §1 的 agent-unification 半成品同一性质。

### 🔍 `session_observer.go` —— 未接线的共享助手，且在用的等价实现存在

`SendAndObserveSession` 是一份**完整实现**（含轮询退避表、等待语义、图片计数），
注释明说目的是"让 IM 工具和其它调用方复用同一套轮询语义"。但真实在用的实现是另一处：

```
guiapp/im_message_handler_busy_session_test.go:65,177   h.toolSendAndObserve(...)
guiapp/app_nl_skills.go:2519,3181                       skillStepActionSendAndObserve
```

→ 功能是活的，这份"共享助手"是**重构未完成的产物**（原计划把 `toolSendAndObserve` 抽到这里，没抽完）。
删之前应先确认是"收敛到该助手"还是"放弃该助手"。

### 其余 12 个命中的文件（不改变判定，但删时需留意）

| 文件 | 自述要点 | 影响 |
|---|---|---|
| `corelib/agent/confirmation_status.go` | 某常量 *"remains a string alias to **preserve JSON compatibility**"* | 我的死符号是 `ConfirmationStatusUnknown`/`Pending` —— **字符串枚举常量可能被持久化在 JSON 里**，零代码引用也要保留（删除会破坏历史数据解析） |
| `corelib/agent/tool_subagent.go` | *"BuiltinSubAgents defines **legacy prompt-injection sub-agents**. Coding work is **intentionally not listed** here…"* | 是一个有意的内容注册表 → 删 `BuiltinSubAgents` 前确认无人读 |
| `corelib/intent/layer1.go` | *"classifyByKeywords is **retained only for legacy test/build compatibility**"* | 复验为 LOW（全活），不受影响 |
| `corelib/tts/piper_flow.go` | *"kept for **API compat**"* | 属 piper 未接线簇 |
| `corelib/opus/internal/libc/wstring.go` | `Deprecated: use unsafe.Slice` 等 | 移植代码的 deprecated 标注，不影响判定 |
| `corelib/plugin/adapter_mcp.go` | *"Real MCP client integration is **deferred**; Start sets healthy=true as a **placeholder**"* | 复验为 LOW（全活） |
| `corelib/skill/dependency.go`、`corelib/skill/solidify.go`、`corelib/memory/{archiver,backend_json}.go`、`corelib/browser/replay_background.go`、`guiapp/browser_replay_scheduler.go`、`guiapp/im_agent_reply_quality.go` | 只是正文里出现 legacy/placeholder 等词 | 不影响 |

> **方法学结论**：审计的最后一关必须是**读文件头的自述**。
> 「零引用」是客观事实，「可删」是意图判断，两者之间横着"有意保留 / 半完成迁移 / 未接线重构"三种情况。
> 建议做法：对判死文件批量跑一遍关键词扫描，再人工读命中的那几个。

---

## 四、新发现：脚本/CI/构建层面的引用（文档未覆盖的检查）

### 4.1 ⚠️ `scripts/check-agent-architecture.mjs` 是门禁，钉住 3 个候选

该脚本含 **180 条 `requireText(路径, 必须存在的源码文本, 说明)`** 断言。对 231 个候选做路径级扫描后，命中 3 个：

| 候选文件 | 文档档 | 门禁断言 |
|---|---|---|
| `corelib/agentservice/semantic_behavior_snapshot.go` | MID | **10 条**，如 `func DiffSnapshotBytes`、`func SyncSnapshotFile`、`func CheckPlanSurfaceFirstWaveParity`、`'no overlapping first-wave cases compared'` |
| `guiapp/coding_subagent_rollout.go` | MID | `'RuntimeStatus agentruntime.JobStatus'`（结构体字段） |
| `corelib/agent/shared_capabilities.go` | LOW | `'func ExtraSharedHostCapabilityNames() []string {'` |

其中 **1 处直接冲突**：

```
我的判定： corelib/agentservice/semantic_behavior_snapshot.go 的 DiffSnapshotBytes —— 跨文件零引用
门禁要求： scripts/check-agent-architecture.mjs:232
           requireText('corelib/agentservice/semantic_behavior_snapshot.go',
                       'func DiffSnapshotBytes', 'shared snapshot drift reporter')
```

→ **`DiffSnapshotBytes` 删了 CI 就红**。这正是「零 Go 引用 ≠ 可删」的标准案例：它是**回归装置**的一部分（同目录还有 `semantic_behavior_snapshot_test.go` 与冻结基线 `testdata/semantic_behavior_snapshot.txt`）。

**同一文件的其它事实**：32 个符号中 17 个零引用、15 个有用，**门禁钉住的 10 个函数全部落在"有用"一侧** —— 即"逐符号审查"的结论是：可删的是那 17 个私有辅助函数，10 个门禁函数与编码/快照族必须留。

### 4.2 ⚠️ 2 个候选是你当前的**在途修改**

```
$ git status --porcelain
81  M      ← 比上一轮文档记录的 65 个增加 16 个
24  ??
 4  D
```

与候选清单重叠的 `M` 文件：

| 文件 | 档 | 说明 |
|---|---|---|
| `corelib/agentservice/semantic_behavior_snapshot.go` | MID | **同时被门禁钉住** —— 最危险的一条，双重不可动 |
| `corelib/intent/keyword_registry.go` | LOW→MID | `KeywordMatch` 判死，但文件被你在改 |

**执行清理前必须重新跑一次 `git status`，把 `M` 文件从清单中剔除。**

### 4.3 ⚠️ `check-agent-architecture.mjs` 之外：CI 覆盖范围复核

- **根模块确实没有全量 `go test ./...`**（附录 §3.2 的警告成立）：唯一的 `go test ./...`（`main.yml:2286`）位于 `cd ClawMateMaker` 之后，属**子模块**。
- 根模块 CI 只跑定向包：`corelib/agentruntime`、`corelib/agentservice`、`MaClawSrv`、`guiapp`（含 `-run` 白名单）、`corelib/{embedding,memory,database}`、`hub/internal/{workflow,httpapi}`、`hubcenter/internal/httpapi`、`corelib/workflow`、`cmd/genphasemeta`。
- ⇒ **`corelib/opus`、`corelib/tts`、`corelib/remote`、`corelib/plugin`、`corelib/needledata` 等本次重灾区完全没有 CI 保护**。删这些里的东西不会有任何测试兜底，这也是它们能长期腐烂的原因。

### 4.4 前端孤儿复核：28+6 成立，但 1 个性质需改

- **10 个直接孤儿**（`WorkflowInitiationForm` / `WorkflowDirectoryPanel` / `InstanceConfirmationPanel` / `TerminalNodeConfigPanel` / `LocalAIAssistantView` / `NotificationToast` / `MaclawAppSkillsTab` / `RemoteDiagnosticsPanel` / `RemoteSmokeSummaryCard` / `RemoteStatusCards`）：逐个按 import 路径 + stem 字符串双查，**全部零引用，成立**。
- **12 个级联/测试独占/群讨论簇**：复核出的引用关系与文档 §5.2–5.5 描述的**逐一吻合**（如 `RemoteRoutingCard` ← 仅 `RemoteDiagnosticsPanel`；`VEStatusDot` ← 仅 `useVEPresence.ts`），成立。
- **`src/components/preview/index.ts`（死 barrel）确认**：全仓无目录式 import，消费方全部直接引子路径（`../preview/FilePreviewView`、`../preview/filePreviewKind`、`../preview/FilePreviewHost`）。
  - ⚠️ **但 `guiapp/frontend/src/components/preview/` 整个目录 git 跟踪数 = 0 / 磁盘 5 个文件** —— 是**尚未提交的新增模块**。该 barrel 属在途文件，**应从"死代码删除清单"移到"待你确认"**。

---

## 五、保护清单：本轮重新核实（未抄上一轮）

### 5.1 全部 27 条 `//go:embed` 目标 —— 逐个验证存在，0 缺失

| embed 目标 | 文件数 | 来源 |
|---|---:|---|
| `guiapp/frontend/dist` | 181 | `guiapp/assets_desktop.go:7` |
| `guiapp/petpack/bundled` | 43 | `guiapp/petpack/embed.go:7` |
| `guiapp/builtin_skills` | 14 | `guiapp/app_builtin_skills.go:15` |
| `datasrv/structureddata/webui_assets/*` | 5 | `datasrv/structureddata/webui_v2.go` |
| `MaClawSrv/admin_web/*`、`user_web/*` | 3+3 | `admin_web.go` / `user_web.go` |
| `TigerProxy/frontend/dist` | 3 | `TigerProxy/main.go` |
| `ClawMateMaker/frontend/dist` | 1 | `ClawMateMaker/main.go` |
| `guiapp/vscode_ext_asset/maclaw-acp.vsix` + `version.txt` | 2 | `guiapp/vscode_acp_ext_asset.go:12,15` |
| `guiapp/hello-maclaw.wav` | 1 | `guiapp/hardware_welcome_asset.go:9` |
| `guiapp/build/{appicon.png,windows/icon.ico,qianxin.png,tigerclaw.ico,mobile/bootstrap.html.tmpl}` | 5 | `resources_*.go` / `floating_*.go` / `android_pwa_shell.go` |
| `corelib/ocr/dict_ppocrv6{,_tiny}.txt` | 2 | `corelib/ocr/dict.go` |
| `corelib/tts/kokoro/kokoro-v1_0_config.json` | 1 | `corelib/tts/kokoro/config.go` |
| `corelib/vad/silero_weights.bin` | 1 | `corelib/vad/silero.go` |
| `corelib/accessibility/tools/MaclawUIASidecar/Program.cs` | 1 | `uia_csharp_source_windows.go` |
| `guiapp/frontend/src/components/ai/installCommandAllowlist.json` | 1 | `install_command_allowlist.go` |
| `TigerProxy/assets/maclaw.ico` | 1 | `TigerProxy/main.go` |

### 5.2 ❌ 需修正：`vscode-ext/` 的保护理由指错了目录

文档 §14 写：*"`vscode-ext/` — `guiapp/vscode_acp_ext_asset.go` 用 `//go:embed` 把 .vsix 编进主程序"*。

实际 `//go:embed` 的路径是 **`guiapp/vscode_ext_asset/maclaw-acp.vsix`**（另一个目录，已提交入版本库）。两者都要保护，但**理由不同**：

| 对象 | 真实角色 | 证据 |
|---|---|---|
| `guiapp/vscode_ext_asset/{maclaw-acp.vsix,version.txt}` | **真正的 embed 目标**，v 版本库内提交 | `guiapp/vscode_acp_ext_asset.go:12,15`；`git ls-files` 命中 |
| `vscode-ext/` | 扩展**源码**，由构建脚本编译出 vsix 后拷进上者 | `build_win.bat:99` 调 `vscode-ext\build-vsix.ps1`；`.github/workflows/main.yml:277,1110,1661,1897`；`build-vsix.ps1:30` "VSIX refreshed under guiapp/vscode_ext_asset/" |

文档只保护了 `vscode-ext/`（且理由是错的），**没点名真正被 embed 的 `guiapp/vscode_ext_asset/`**。

### 5.3 其余保护项 —— 全部复现

| 项 | 结论 |
|---|---|
| `third_party/shine-mp3` | ✅ 根 `go.mod:153` 有 `replace` 指向，`main.go` 存在 |
| `go.mod` 其它 replace | 另有 `./guiapp/internal/systray`（:147）、`./datasrv`（:149）→ 两者均需保留 |
| `test_16k_mono.wav`、`zhou_16k.wav`、`beiing_16k.wav` | ✅ 被 `corelib/asr/batch_eval_test.go:32`、`corelib/asr/moonshine_test.go:61,76`、`guiapp/live_diarization_probe_test.go:29` 读取 |
| `corelib/pptx/` + `cmd/pptx-preview` | ✅ 三处消费仍在：`cmd/pptx-preview/main.go:54`、`corelib/agent/tools_office_preview.go:54,75`、`corelib/agent/tool_register_core.go:520-527`（`office(action="preview_pptx")` 工具描述） |
| `corelib/opus/`、`corelib/amrnb/` | ✅ 被 `audioconv` / `tts` 引用 |

### 5.4 ➕ 新增保护项（文档未列）

| 对象 | 原因 |
|---|---|
| `guiapp/frontend/dist/` | `guiapp/assets_desktop.go:7` `//go:embed all:frontend/dist`（181 文件）——**删了 GUI 编译失败** |
| `ClawMateMaker/frontend/dist/`、`TigerProxy/frontend/dist/` | 同上，各自 module 的 embed 目标 |
| `guiapp/build/`、`TigerProxy/assets/`、`MaClawSrv/{admin_web,user_web}/`、`guiapp/petpack/bundled/`、`guiapp/builtin_skills/` | 均为 embed 目标 |
| `scripts/check-agent-architecture.mjs` **钉住的 180 个文件/片段** | 该脚本是架构门禁，改动其断言的源码文本会红 |
| 根包 `github.com/RapidAI/CodeClaw` | **零 import 是设计使然**：`generate.go` 自述 "contains no runtime code… hosts the go:generate directive"，同目录 `live_probe.go` 带 `//go:build ignore`、由 `_run_live_probe.cmd` 调用 → **不是死代码** |

---

## 六、修正后的最终清单（取代前两份文档）

### P1 高置信可删（本轮新增，文档漏判）

**LOW → HIGH 的 22 个**（§3.3-A 表，⛔ 标记的 1 个除外）+ **MID → HIGH 的 8 个**（§3.2 表）= **29 个文件**，判据与文档 HIGH 档同源（文件内所有符号跨文件零引用），风险等级同 P1。

> 例外：`corelib/remote/image_helpers.go`、`mode_hash.go`、`session_completion_analyzer.go`、`session_io_relay.go` 与 `corelib/plugin/bootstrap.go` 建议**先做重复实现归并**（确认 guiapp/hub 侧那份是唯一实现），再删 corelib 副本，避免误删唯一实现。

### P2 逐符号删（不可整文件删）

- 文档 MID 87 中确认部分死的 **79 个** —— 明细见 §3.2 与 `%TEMP%` 分析输出；**每个文件的可删符号清单已逐条列出**。
- **LOW → MID 的 39 个**（§3.3-B 表）—— 其中 `corelib/remote/sdk_types.go`(16/18)、`session_monitor.go`(13/14)、`session_stall_detector.go`(14/15)、`event_coalescer.go`(7/8) 是"整簇复制品"，建议整簇处理。

### 🚫 不可动（本轮新增拦截）

| 对象 | 原因 |
|---|---|
| `corelib/agentservice/semantic_behavior_snapshot.go` **全部 32 个符号** | ① 门禁钉住 10 个函数 ② 你在途修改（`M`） ③ 有测试 + 冻结基线 |
| `guiapp/coding_subagent_rollout.go` | 门禁钉住 `RuntimeStatus agentruntime.JobStatus` 字段 |
| `corelib/agent/shared_capabilities.go` | 门禁钉住 `ExtraSharedHostCapabilityNames` |
| **`corelib/database/handler.go`** | 自述 *"retained as the short, source-compatible entry point for hosts that used the original database package API"* —— **有意保留的对外兼容入口**（§3.5） |
| **`corelib/remote/capability_market_auth.go`** | 自述 *"the preferred constructor name"* + *"backward-compatible alias"* —— **半完成的改名迁移**，底层 `NewSkillMarketAuthClient` 有 6 个文件在用（§3.5） |
| **`corelib/agent/confirmation_status.go` 的 `ConfirmationStatusUnknown`/`Pending`** | 自述 *"remains a string alias to preserve **JSON compatibility**"* —— **字符串枚举常量可能被持久化**，零代码引用也不可删（§3.5） |
| `corelib/intent/keyword_registry.go` | 你在途修改（`M`） |
| `guiapp/frontend/src/components/preview/index.ts` | 位于未跟踪目录，属在途新增 |
| 文档 HIGH 中的 `corelib/opus/internal/libc/wctype.go` | 复验撤销：`WChar` 有引用 |
| 文档 LOW 中确认全活的 **31 个** | 复验无死符号 |

### ⚠️ 降级为"待确认"（自述显示是未接线而非垃圾）

| 对象 | 原因 |
|---|---|
| `guiapp/session_observer.go` 的 `SendAndObserveSession` | 自述为"intentionally a thin **shared helper** so IM tools and other callers can reuse"；真实在用的实现在 `h.toolSendAndObserve` / `skillStepActionSendAndObserve` → **重构未完成**（§3.5） |
| `corelib/agent/tool_subagent.go` 的 `BuiltinSubAgents` | 自述为 legacy prompt-injection sub-agent 注册表，"intentionally not listed" → 删前确认无人读（§3.5） |

### 数量总账

```
文档口径          HIGH 52   MID 87   LOW 92      (231)
复验口径          HIGH 79   MID 121  LOW 31      (231)

净变化            +27       +34      -61
其中  LOW→HIGH 22（含 1 个被自述撤销）、MID→HIGH 8、LOW→MID 39、HIGH→MID 3
自述复核另撤销 2 项（文档 HIGH）+ 降级 2 项
```

---

## 七、建议执行顺序（修订版）

| 优先级 | 动作 | 风险 | 验证 |
|---|---|---|---|
| **P0** | 先跑 `git status`，把 81 个 `M` 与全部未跟踪文件从任何清单中剔除 | — | 人工 |
| **P1** | `gui/` 整目录 + 11 个零引用 .go + `cmd/quick_test` + 3 个零引用包 | 无 | `go build ./...` |
| **P1** | `build/*.DS_Store` ×2 + `deploy/logs/*` ×14 走 `git rm --cached` | 无 | `git status` |
| **P1** | 提交那 4 条既有 `D`（`.DS_Store`、`audio_spectrum.png`、`audio_wave.png`、`maclaw2.png`） | 无 | 只暂存这 4 条路径 |
| **P2** | 前端 10 个直接孤儿 + 12 个级联/测试独占/群讨论簇（共 28 + 6 测试） | 低 | `npm test` |
| **P2** | **本轮新增的 29 个 LOW/MID→HIGH 文件** | 低 | `go build ./...` + `go vet ./...` |
| **P2** | 95 个 Wails 绑定（先清 `config_txn.go:280` 字符串分支，保留 `VerifyAndActivateNLSkill` 待议） | 低 | `wails generate module` |
| **P3** | 文档 MID 79 个 + LOW→MID 39 个的**逐符号**删除 | 中 | 每文件单独提交，便于回滚 |
| **P4** | 处理 §1 迁移半成品（二选一收敛） | 中 | `go test ./guiapp/ -run Task` |
| **P4** | 定夺零引用包 9 个（含 3 个回归装置） | 中 | **先给 CI 补一条 `go test ./corelib/tool/... ./corelib/longhorizon/... ./corelib/experience/...`** |
| **P5** | `corelib/opus` **57** 个死文件 + piper 引擎及其 21 个调试命令 | 高 | 单独立项 |
| **P5** | `hub`/`hubcenter` 12 个同名包抽公共层 | 高 | 需架构设计 |

**每步之后**：

```powershell
go build ./...
go vet ./...
go test ./corelib/... ./guiapp/...
cd guiapp/frontend; npm test
node scripts/check-agent-architecture.mjs   # 门禁，必跑
```

> CI 未覆盖的包（`corelib/opus`、`corelib/tts`、`corelib/remote`、`corelib/plugin`、`corelib/needledata`）删除后**没有自动兜底**，必须靠 `go build ./...` + `go vet ./...` 手动把关。
> 已知差异：`CodePreviewPanel.mergedHeader.test.tsx` 在 HEAD 上就失败，对比时不要算作本次引入。

---

## 八、本轮保留的不确定性

| 项 | 说明 |
|---|---|
| 231 这个**宇宙**本身来自上一轮的 `deadcode` | 本轮只在这 231 个里做独立判定。"完全没进过 `deadcode` 视野"的文件不在本轮范围内（它们至少有一个符号被引用）。若要把宇宙也独立重建，需对全部 6,080 个 `.go` 文件做可达性分析 —— 下一轮可做。 |
| `deadcode` 对 Wails/注册表的误报 | 本轮判定不依赖它；但"这 231 个是死文件"这个前提仍来自它。判定结果为"全活"的 31 个 LOW 项，反过来印证了它的误报率。 |
| B 档"部分死"的**可删性** | 本轮只判定"符号是否被引用"。符号有引用 ≠ 该引用来自活代码（可能是死簇内部互引），故 P2 的逐符号清单仍需人工过一遍调用链。 |
| `VerifyAndActivateNLSkill` | Go 侧零引用，但错误提示语里明确让调用者用它 → 可能是**未接线的流程**而非垃圾，建议单独决策。 |
| 文件头自述扫描的覆盖范围 | 只扫了每个文件的**前 45 行**注释。藏在文件中部、或被拆到别处的"有意保留"说明可能漏掉 → P1/P2 实际执行前，对每个要删的文件仍建议扫一眼文档注释。 |
| `corelib/tts/cmd/` 21 个 `piper_*` | 与未接线的 piper 引擎同生共死；本轮未复核 `corelib/tts/testdata/*.py` 对 piper 符号的依赖（已见 `splitPinyinForPiper` 被两个 py 脚本引用），若删 piper 需一并处理 testdata 脚本。 |

---

*复验手段：6,080 个 Go 文件的状态机注释剥离与顶层符号抽取 · 反向 import 图限定扫描（包限定 + alias 感知 + uncertain 分档）· 862 个死符号 × 7,499 个非 Go 文件字符串扫描 · 103 个脚本/CI/构建文件路径扫描 · 180 条 `requireText` 门禁断言比对 · 231 个候选的文件头自述语义扫描（retained/legacy/compat/intentionally 等 20 个关键词）× 命中项人工读原文 · 27 条 `//go:embed` 目标存在性验证 · 前端 770 文件源码扫描（排除生成 stub）· `go list` 编译级 import 图复算零引用包 · git 索引与工作区状态核对。全程只读。*
