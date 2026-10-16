# MaClaw 可清除对象总清单（附录）

> 生成日期：2026-09-15 ｜ 仓库：`D:\workprj\aicoder`
> 上游报告：[`DEAD-CODE-REPORT.md`](./DEAD-CODE-REPORT.md)（方法论）｜[`LEAN-CODE-CANDIDATES.md`](./LEAN-CODE-CANDIDATES.md)（首轮非代码清理）
> **本文档只做枚举，未删除任何文件。**
> ⚠️ **2026-09-15 已独立复验（第一轮）**：[`DEAD-CODE-VERIFICATION.md`](./DEAD-CODE-VERIFICATION.md) —— 修正本清单 8 处（含 2 处误判可删）。
> ⚠️⚠️ **2026-09-15 第二轮复验（最新，权威）**：[`DEAD-CODE-VERIFICATION-2.md`](./DEAD-CODE-VERIFICATION-2.md) —— **本文档 §2.2/§2.3 的分档已被系统性推翻**：
> HIGH 52 → **79**、MID 87 → **121**、LOW 92 → **31**；opus 死文件 39 → **57**。
> 根因：本文档 §2 的符号交叉校验**未做包限定**，`corelib/*` 与 `guiapp/*`/`hub/*` 的同名符号互相"证明对方活着"。
> **冲突时一律以 `DEAD-CODE-VERIFICATION-2.md` 为准。**

---

## 0. 勘误（先看这节）

上一版报告有 3 处判定错误，已在本清单中修正。记录在此，避免重复踩坑：

| # | 上一版结论 | 实际事实 | 修正后判定 |
|---|---|---|---|
| 1 | `cmd/pptx-preview` 无构建引用 → 可删 | **错**。`corelib/pptx` 的 `RenderPreview` 同时被三处在用：① `corelib/agent/tools_office_preview.go`（`office(action="preview_pptx")`）② guiapp Wails 绑定 `PptxPreviewEnsure` / `PptxSlideThumbnailDataURL` ← `frontend/src/components/ai/PptxPreviewPanel.tsx` ③ `guiapp/builtin_skills/pptx-gen/skill.yaml` 明确要求生成后调 `preview_pptx` 校验排版 | **保留**。CLI 只是同一渲染器的人工校验入口 |
| 2 | `corelib/im/feishu`（路径） | **路径写错**，实际是 `corelib/feishu`（不在 `im/` 下） | 包名路径已修正 |
| 3 | 零 import 包 = 死代码，可直接删 | **不严谨**。`corelib/tool/routingeval`、`corelib/longhorizon/eval`、`corelib/experience/counterfactual`、`corelib/tool/routingarch` 自带测试与数据集，是**回归装置**；且 CI（`.github/workflows`）只跑定向 `go test`，**不跑根模块全量 `go test ./...`**（唯一那条在 `cd ClawMateMaker` 的子模块里） | 降级为**待产品确认**，不列入直接删除 |
| 4 | `cmd/` 孤儿命令 7 个 | `genphasemeta` 被 CI 调用、`maclaw-needle` 被 `scripts/needle_finetune.py` 调用、`office-read-dual-report` 被 `scripts/test-officeread-*.ps1` 调用 | 修正为 8 个无构建引用，且其中多数是人工诊断入口，**非死代码** |

> 教训：**"没有构建脚本引用"≠"没有引用"**。必须同时查 CI 工作流、`scripts/`、`Makefile`，以及"功能是否被其他入口（GUI/工具/skill）在用"。

---

## 1. 核心发现：agent-unification 迁移半成品（最有价值的清理目标）

`corelib/agent/` 下有一批文件自述 *"Migrated from gui/im_*.go as part of the agent-unification plan"*，但其中若干**复制过去了、从未接线**，而 guiapp 里的原实现仍在生产路径上运行 —— 形成**两份平行实现**。

| corelib 侧（迁移副本） | guiapp 侧（原实现，仍在用） | 状态 |
|---|---|---|
| `corelib/agent/task_understanding.go`（导出 `ParseTaskUnderstandingResponse` / `FormatTaskUnderstandingSummary` / `FormatEnhancedInstruction`） | `guiapp/im_task_understanding.go`（小写版本）+ `im_task_understanding_test.go`（12+ 用例） | corelib 侧**零 import**；guiapp 侧经 `im_confirmation_gate.go:139 → understandTaskWithLLM` 在生产路径 |
| `corelib/agent/selfconfirm.go`（`ConfirmRequestRe` / `SelfAnswerRe` / `ContainsSelfConfirmationPattern` / `TruncateAtConfirmationBoundary`） | `guiapp/im_message_handler_selfconfirm.go` + `im_message_handler_selfconfirm_contains_test.go` | corelib 侧**零 import**；guiapp 侧仍被引用 |
| `corelib/agent/confirmation_store.go` | `guiapp/*` 对应实现 | corelib 侧 2/8 符号彻底未用 |
| `corelib/agent/pending_media.go` | `guiapp/im_pending_media.go` | corelib 侧 3/8 未用，guiapp 侧 4/8 未用 |

**注意逻辑已经分叉**：corelib 版 `task_understanding.go` 的校验条件是 "summary 或 enhanced_instruction 非空"，而 guiapp 版用更强的 `hasDisplayableTaskUnderstanding`（还要求 goals/constraints/plan 有意义）。两份不是简单复制，是**各自演化过**。

> **建议**：这批不是"删了就行"，而是**迁移收尾工程** —— 要么把 guiapp 切到 corelib 版并删 guiapp 版，要么反向删掉 corelib 版。二选一，不能留着两份。

其余自述迁移的文件（`tools_office.go`、`tools_local.go`、`compress.go`、`conversation_trim.go`、`attachment.go`、`conversation_memory.go`、`topic_detector.go`，以及 `corelib/llm/anthropic_convert.go`）**均已接线**，无需处理。

---

## 2. 全死文件总表（231 个）

判定方式：`deadcode`（多入口静态调用图）标记"文件内每个函数都不可达" → 再用**符号交叉校验**（该文件的符号是否在其他任何文件被引用）分档。

- 入口：`./cmd/maclaw-gui ./cmd/maclaw-tool ./cmd/maclaw-acp-bridge ./cmd/genphasemeta ./tui ./MaClawSrv ./maclaw-cli ./TigerProxy ./hub/cmd/hub ./hubcenter/cmd/hubcenter`
- 分布：`corelib` 191 个，`guiapp` 40 个

| 档位 | 数量 | 判据 | 处理建议 |
|---|---:|---|---|
| **A 高置信** | **52** | 文件内所有符号**在其他任何文件都找不到引用** | 可分簇评估删除 |
| **B 中等** | **87** | 部分符号在别处被引用 | 逐符号审查 |
| **C 低置信** | **92** | 所有符号在别处被引用（注册表 / 接口 / 平台文件） | **勿盲删**，基本是误报 |

### 2.1 A 档：高置信死文件（52 个）

#### 2.1.1 `corelib/opus/` 编解码移植（39 个）— ⚠️ 单独立项，勿顺手删

无任何调用点，判定可信；但 Opus 被 `corelib/audioconv/opus.go`、`corelib/tts/opus_encode.go` 使用，且这些函数疑似**移植未完成的预留代码**。

```
corelib/opus/internal/libc/     setjmp.go  time.go  wchar.go  wctype.go            (4)
corelib/opus/libopus/           silk_A2NLSF.go  silk_LP_variable_cutoff.go
                                silk_NLSF2A.go  silk_NLSF_VQ.go  silk_NLSF_decode.go
                                silk_NLSF_del_dec_quant.go  silk_NLSF_encode.go
                                silk_NLSF_stabilize.go  silk_NLSF_unpack.go
                                silk_NSQ.go  silk_NSQ_del_dec.go  silk_bwexpander_32.go
                                silk_decode_frame.go  silk_decode_indices.go
                                silk_decode_pulses.go  silk_decoder_set_fs.go
                                silk_init_decoder.go  silk_process_NLSFs.go
                                silk_quant_LTP_gains.go  silk_resampler_down2.go
                                silk_resampler_down2_3.go  silk_stereo_MS_to_LR.go
                                silk_stereo_decode_pred.go
                                silk_float_LPC_analysis_filter_FLP.go
                                silk_float_LPC_inv_pred_gain_FLP.go
                                silk_float_find_LPC_FLP.go  silk_float_find_LTP_FLP.go
                                silk_float_find_pitch_lags_FLP.go
                                silk_float_find_pred_coefs_FLP.go
                                silk_float_noise_shape_analysis_FLP.go
                                silk_float_pitch_analysis_core_FLP.go
                                silk_float_process_gains_FLP.go
                                silk_float_regularize_correlations_FLP.go
                                silk_float_residual_energy_FLP.go
                                silk_float_wrappers_FLP.go                          (35)
```

#### 2.1.2 非 Opus 高置信死文件（13 个）

| 文件 | 函数数 | 说明 |
|---|---:|---|
| `corelib/agent/selfconfirm.go` | 4 | 迁移未接线（见 [§1](#1-核心发现agent-unification-迁移半成品最有价值的清理目标)） |
| `corelib/agent/task_understanding.go` | 3 | 迁移未接线（同上） |
| `corelib/im/convert.go` | 2 | IM 消息转换，无引用 |
| ~~`corelib/knowledge/scan_images.go`~~ | 4 | ⚠️ **复验撤销**：`ImageReference` 类型被生产方法 `ProcessStandaloneImage`（`image_import.go:445` ← `store.go:5518`）引用 → **不可整文件删**，仅 5 个符号可删 |
| `corelib/remote/capability_market_auth.go` | 1 | 无引用 |
| `corelib/remote/event_types.go` | 3 | 无引用 |
| `corelib/remote/machine_profile.go` | 4 | 无引用（`corelib` 包里另有同名常量，属同名异包，不算引用） |
| `corelib/remote/mobile_launch_types.go` | 1 | 无引用（guiapp 自有一份同名类型，不算引用） |
| `guiapp/im_memory_recall_mode.go` | 1 | `normalizeIMMemoryRecallMode` 及 6 个常量，全无引用 |
| `guiapp/im_tool_list_sessions.go` | 1 | `toolListSessions` 方法定义后从未注册/调用 |
| ~~`guiapp/im_tool_session_control_action.go`~~ | 1 | ⚠️ **复验撤销（误报）**：5 个符号中 4 个在用（`sessionControlAction` 类型 + `Interrupt`/`Kill`/`Unknown` 被 `im_tools_session_control.go`、`im_tools_session_actions.go` 及测试引用）→ **不可删**，仅 `normalizeSessionControlAction` 是死的 |
| `guiapp/session_observer.go` | 1 | 导出的 `SendAndObserveSession` + `SessionObserveOptions`，无调用者 |
| `guiapp/session_terminal_status.go` | 2 | `normalizeTerminalSessionStatus` / `isTerminalSessionStatus`，无调用者 |

> 复验后：13 个中 **11 个确认整文件零引用**，2 个撤销（见上表删除线）。

> 已逐个人工复核：上表 5 个 guiapp 文件确实**既不在注册表 map、也无字符串引用**，非误报。

### 2.2 B 档：中等置信（87 个，部分符号死）

格式：`文件 (完全未使用的符号数 / 文件总符号数)`

**最大聚集 — `corelib/tts/`（16 个）**：`ops.go`(3/18) `phoneme_table.go`(2/6) `piper.go`(1/5)
`piper_duration.go`(2/9) `piper_duration_cache.go`(2/5) `piper_duration_mlp.go`(1/2)
`piper_flow.go`(2/3) `piper_g2p.go`(4/7) `piper_g2p_arpabet.go`(7/12) `piper_g2p_en.go`(4/9)
`piper_hifigan.go`(1/2) `piper_lexicon.go`(1/4) `piper_sdp.go`(4/8) `text_encoder.go`(4/8) `weights.go`(1/3)
→ 与既有结论一致：**piper 系列整体未接线，`manager.go` 只接了 `kokoro/`**

**`corelib/needledata/`（8 个）** — 评测脚手架：`calibration.go`(1/4) `export.go`(2/9) `gate.go`(1/3)
`generate.go`(6/10) `localization_eval.go`(3/6) `needle_format.go`(5/7) `redact.go`(2/4)

**`corelib/remote/`（10 个）**：`codex_types.go`(1/7) `event_extractor.go`(3/15)
`execution_helpers.go`(4/10) `output_normalize.go`(2/4) `output_pipeline.go`(5/10)
`provider_resolver.go`(3/6) `session_progress_tracker.go`(2/10) `startup_responder.go`(1/4)
`summary_output_marker.go`(1/2)

**`corelib/skill/`（7 个）**：`comparative_distiller.go`(5/8) `description_quality.go`(1/3)
`execution_preamble.go`(1/3) `semver.go`(4/10) `solidify.go`(3/9) `state.go`(1/8) `taxonomy.go`(3/6)

**`corelib/opus/internal/libc/`（6 个）**：`assert.go`(1/2) `errno.go`(3/5) `rand.go`(1/2)
`runtime.go`(5/11) `stdlib.go`(2/3) `wstring.go`(13/18)

**`corelib/agent/`（3 个）**：`confirmation_store.go`(2/8) `pending_media.go`(3/8) `self_review.go`(7/11)

**`corelib/plugin/`（3 个）**：`adapter_local_mcp.go`(5/12) `envresolve.go`(2/4) `trust.go`(1/5)

**`corelib/tts/` 再计**：无（已列）
**`corelib/llm/`（3 个）**：`auxiliary.go`(2/5) `failover.go`(6/9) `fork_context.go`(3/15)

**`corelib/agentservice/`（2 个）**：`dynamic_effect_receipt_store.go`(1/6) `semantic_behavior_snapshot.go`(16/28)

**其他（各 1-2 个）**：
`corelib/browser/replay_background.go`(1/2)、`corelib/configfile/external_agent.go`(9/19)、
`corelib/embedding/tensor/q4_dot_amd64.go`(3/5)、`corelib/event_emitter.go`(2/4)、
`corelib/guiautomation/replay_background.go`(1/4)、`corelib/intent/calibration.go`(3/5)、
`corelib/memory/conflict.go`(2/5)、`corelib/memory/tree/sealer.go`(6/12)、`corelib/memory/tree/types.go`(1/3)、
`corelib/needledata/*`（已列）、`corelib/opus/silk/enc_API.go`(1/4)、`corelib/progress/queue.go`(1/5)、
`corelib/security/llm_review.go`(4/7)、`corelib/tool/semantic_schema_gate.go`(6/8)、
`corelib/tool/token_compress.go`(6/15)

**guiapp（18 个）**：
`app_init_optimized.go`(11/24) `coding_subagent_rollout.go`(9/13) `context_compressor.go`(11/13)
`diff_computer.go`(6/9) `file_snapshot_store.go`(1/6) `im_message_handler_selfconfirm.go`(1/4)
`im_message_handler_workflow_initiate.go`(15/24) `im_pending_media.go`(4/8)
`im_tools_create_session_precheck.go`(1/2) `im_tools_create_session_provider.go`(1/2)
`im_tools_create_session_start.go`(2/5) `im_tools_session_guard.go`(1/2)
`im_tools_session_send_observe_orchestrator.go`(1/3) `manager_interfaces.go`(5/80)
`openhuman_background.go`(8/21) `provider_resolver.go`(2/5) `remote_tool_update_status.go`(1/2)
`self_review_adapters.go`(2/6)

### 2.3 C 档：低置信 / 疑似误报（92 个）— ⚠️ **本节判定已被第二轮复验推翻，勿据此操作**

> **2026-09-15 第二轮复验结论**：这 92 个里只有 **31 个**真正"所有符号都被引用"；
> **22 个实为全死**（应升 HIGH）、**39 个实为部分死**（应升 MID）。
> 名单与逐符号明细见 [`DEAD-CODE-VERIFICATION-2.md`](./DEAD-CODE-VERIFICATION-2.md) §3.3。
> 下面"几乎可断定是误报"的表述**是错的**，被它点名的 `corelib/plugin/adapter_script.go`、
> `corelib/remote/{session_monitor,session_stall_detector,session_io_relay,event_coalescer}.go`
> 恰恰是查实的死/半死项（如同名异包假引用所致）。

这些文件的所有符号都能在别处找到引用，典型成因是 **IM 工具注册表 map**、**接口实现**、**平台后缀文件**（`*_windows.go` / `*_other.go`）。~~**请勿照单删除**~~ → 见上方警告。

代表项：
`guiapp/manager_interfaces.go`（80 个函数，75 个被引用 —— 接口实现）、
`guiapp/im_tools_*.go`（经注册表 map 调用）、
`guiapp/mac_compat_other.go`、`guiapp/screenshot_native_windows.go`（平台文件）、
`corelib/plugin/adapter_{mcp,nlskill,script}.go`（插件适配器，注册表调用）、
`corelib/remote/{session_monitor,session_stall_detector,session_io_relay,event_coalescer}.go`、
`corelib/remote/{admin_windows,execution_helpers_windows,screen_permission_other}.go`（平台文件）、
`corelib/im/router.go`、`corelib/intent/{fusion,keyword_registry,layer1}.go`、
`corelib/memory/backend_json.go`、`corelib/tts/{conv_simd,melotts,flow,duration_predictor}.go`、
`corelib/opus/` 余下项、`corelib/needledata/{eval,jsonl,logger,report}.go`、
`corelib/plugin/{adapter_mcp,adapter_nlskill,adapter_script,bootstrap,discovery,manifest}.go`、
`corelib/skill/dependency.go`、`corelib/tool/{reranker,skill_memory}.go`、`corelib/yolo/blocks.go`

> 完整 92 条明细见分析脚本输出 `%TEMP%\aicoder_deadfile_crosscheck.txt` 的 LOW 段。

---

## 3. 零引用包（10 个）

判定：`go list` 解析全仓 import 图（240 包 / 158 个 corelib 包），与"被 import 的包集合"求差集。

### 3.1 完全零引用（9 个）

| 包 | 内容 | 判定 |
|---|---|---|
| `corelib/feishu` | `gateway.go`(10KB) + 语音测试。自述 *"lightweight implementation suitable for standalone products without the full hub IM adapter stack"* | **待产品确认**：hub 有完整实现（`hub/internal/feishu/` 9 文件 ~140KB），corelib 这份是**另一套轻量网关**，非陈旧副本 |
| `corelib/wecom` | `gateway.go`(8.3KB)。同款自述 | 同上（hub 侧 `hub/internal/wecom/plugin.go` 54.8KB） |
| `corelib/dingtalk` | `gateway.go`(8.5KB) | 同上（hub 侧 `hub/internal/dingtalk/plugin.go` 47.3KB） |
| `corelib/memoryshot` | `manager.go` `store.go` `types.go` + 测试 | 功能与在用 `corelib/memory` 重叠，**可删候选** |
| `corelib/skillmarket` | `types.go`(3.2KB) 仅类型 | 功能已由 skill hub 承担，**可删候选** |
| `corelib/misc` | 5 文件：`context_bridge.go` `shared_context.go` `task_orchestrator*.go` | 自述"杂项工具"，与 `experience`/`memory` 职责重叠，**可删候选** |
| `corelib/experience/counterfactual` | `dataset.go` `evaluator.go` + 测试 | **回归装置**，自带测试 → 待确认 |
| `corelib/longhorizon/eval` | `dataset.go` `runner.go` + 5 个 JSON 样本 | **回归装置**（设计文档 `docs/design/longhorizon-harness-plan-zh.md` §9 P5 指定）→ 待确认 |
| `corelib/tool/routingarch` | `baseline.go` `routingarch.go` `zeroinvariants.go` + 测试 | **架构守卫**（扫描边界站点白名单，机检设计 §8 item 11）→ 待确认 |

### 3.2 仅测试引用（1 个）

| 包 | 证据 |
|---|---|
| `corelib/tool/routingeval` | 被 `corelib/tool` 的 `_test.go` 引用；含 **40+ 个 JSON 回归样本**与 18KB `runner.go`。是语义路由的**回归基准**，非死代码 |
| `internal/testfixtures` | 复验新发现：仅被测试引用 2 处（本清单原未列）。测试夹具包，**保留** |

> **警告**：根模块**没有**全量 `go test ./...` 的 CI 任务（唯一那条在 `ClawMateMaker` 子模块里）。
> 因此上面这些"零 import 但有自测"的包处于"**只有手动跑才有保护作用**"的状态。
> 删之前建议先给 CI 补一条 `go test ./corelib/tool/... ./corelib/longhorizon/... ./corelib/experience/...`。

---

## 4. `gui/` 目录 — 编译不过的空壳包

```
$ go test ./gui
gui\tool_discover_grant_test.go:10:47: undefined: IMMessageHandler
gui\tool_discover_grant_test.go:33:32: undefined: LoopContext
gui\app_legacy_install_transaction_test.go:31:10: undefined: App
gui\app_legacy_install_transaction_test.go:32:19: undefined: NewToolRouter
gui\app_legacy_install_transaction_test.go:36:2: undefined: createSkillZip
FAIL    github.com/RapidAI/CodeClaw/gui [build failed]
```

- `package main`，但 **`GoFiles = []`** —— 零实现文件
- 只剩 3 个 `_test.go`（`app_legacy_install_transaction_test.go`、`im_llm_retry_auth_test.go`、`tool_discover_grant_test.go`）与一个 `docs/` 子目录
- 测试引用的 `App`、`IMMessageHandler`、`NewToolRouter` 全部 undefined —— 代码早已迁往 `guiapp/`

**判定：整个 `gui/` 目录可删（3 文件 + docs），零风险，因为编译本来就是失败的。**

> 复验补充（2026-09-15）：`gui/docs/` **不是设计文档**，而是**工作流 GUI 测试跑出来的夹具产物** —— 535 个 186 字节的 md，内容形如
> `# Phase Output / - Functional item A ... / This document is long enough to pass the minimum quality gate`。
> 来源：`guiapp/workflow_adapter_persistence.go:263` 把 Project_Storage 写到 `{projectPath}/docs/workflow/{type}/{date}/`，
> 测试以 `gui/` 为 CWD 运行时产物就落在这里（`workflow_adapter_test.go:429` 正是断言该路径）。
> `gui/` 整目录被 `.gitignore:79` 忽略、git 跟踪文件数 = **0**，删除对版本库无影响。合计 538 文件 / 663 KB。

---

## 5. 前端孤儿模块（28 个 + 6 个连带测试）

判定：① 从 `main.tsx` / `App.tsx` 做传递可达性（495 模块 → 434 可达，61 不可达）；② 再做**字符串级复核**（排除 `React.lazy` / 路由表 / 注册表等动态引用）；③ 剔除 `*.test.ts(x)`（由 vitest 直接运行，本就不被 import）。

### 5.1 直接孤儿（10 个）— 全仓无任何引用（含字符串）

| 文件 | 备注 |
|---|---|
| `src/components/workflow/WorkflowInitiationForm.tsx` | **整目录孤儿** |
| `src/components/workflow/WorkflowDirectoryPanel.tsx` | 同上 |
| `src/components/workflow/InstanceConfirmationPanel.tsx` | 同上 |
| `src/components/workflow/TerminalNodeConfigPanel.tsx` | 同上 |
| `src/components/ai/LocalAIAssistantView.tsx` | 注释自述 *"the original AI assistant conversation view"*，**上一代会话视图** |
| `src/components/ai/NotificationToast.tsx` | 无引用 |
| `src/components/remote/MaclawAppSkillsTab.tsx` | 无引用 |
| `src/components/remote/RemoteDiagnosticsPanel.tsx` | 无引用（删它会连带下方 2 个） |
| `src/components/remote/RemoteSmokeSummaryCard.tsx` | 无引用 |
| `src/components/remote/RemoteStatusCards.tsx` | 无引用 |

### 5.2 级联孤儿（2 个）— 仅被 5.1 的孤儿引用

| 文件 | 唯一引用者 |
|---|---|
| `src/components/remote/RemoteRoutingCard.tsx` | `RemoteDiagnosticsPanel.tsx` ×3 |
| `src/components/remote/RemoteToolDiagnosticsCard.tsx` | `RemoteDiagnosticsPanel.tsx` ×3 |

### 5.3 测试独占组件（3 个 + 3 个测试）— 生产代码 0 引用，只被自己的测试引用

| 组件 | 唯一引用者 | 连带删除 |
|---|---|---|
| `src/components/remote/MaclawRolePanel.tsx` | `remote/__tests__/MaclawRolePanel.test.tsx` ×4 | 该测试 |
| `src/components/remote/RemoteSessionCard.tsx` | `remote/__tests__/RemoteSessionCard.test.tsx` ×7 | 该测试 |
| `src/components/settings/DatabaseProfilesPanel.tsx` | `settings/__tests__/DatabaseProfilesPanel.test.tsx` ×20 | 该测试 |

### 5.4 `useVEPresence` 簇（2 个 + 1 个测试）

| 文件 | 引用关系 |
|---|---|
| `src/hooks/useVEPresence.ts` | 仅被自身的 `useVEPresence.test.tsx` ×11 引用 |
| `src/components/ai/VEStatusDot.tsx` | 仅被 `useVEPresence.ts` ×1 引用 |
| （连带）`src/hooks/useVEPresence.test.tsx` | 被测对象消失，一并删 |

### 5.5 群讨论功能簇（6 个 + 1 个测试）— 自成闭环，只被彼此与自身测试引用

| 文件 | 引用关系 |
|---|---|
| `src/components/ai/AssistantGroupDiscussionDropdown.tsx` | 仅被自己的 `__tests__` ×5 与 `...Menu.tsx` ×3 |
| `src/components/ai/AssistantGroupDiscussionMenu.tsx` | 仅被 `...Dropdown.tsx` ×1 |
| `src/components/ai/AssistantGroupDiscussionSafeHandoff.ts` | 仅被 `...Menu.tsx` ×1 |
| `src/components/ai/groupDiscussionTraceFocus.ts` | 仅被 `...Menu.tsx` ×1 |
| `src/components/ai/useGroupDiscussionControls.ts` | 无引用 |
| `src/components/ai/useGroupSessionActions.ts` | 仅被自己的 `__tests__/useGroupSessionActions.test.tsx` ×11 |
| （连带）`src/components/ai/__tests__/AssistantGroupDiscussionDropdown.test.tsx`、`.../useGroupSessionActions.test.tsx` | 被测对象消失 |

### 5.6 孤立工具 / 类型 / barrel（5 个）

| 文件 | 备注 |
|---|---|
| `src/components/preview/index.ts` | **死 barrel**：所有消费方都直接 `from '../preview/FilePreviewView'` 等子路径，无人 import 此入口 |
| `src/components/modals/installSkillI18n.ts` | 无引用 |
| `src/components/remote/MCPManagementTypes.ts` | 无引用 |
| `src/utils/veApprovalCapabilityCheck.ts` | 无引用 |
| `src/utils/wailsReady.ts` | 无引用 |

### 5.7 ⚠️ 曾经入选但已排除（避免误删）

| 文件 | 排除原因 |
|---|---|
| `src/components/ai/NotificationItem.tsx` | 被存活的 `NotificationPanel.tsx` 引用 ×8 |
| `src/components/ai/IncrementalMarkdownRenderer.pipeline.test.tsx` | 测试文件；被测的 `IncrementalMarkdownRenderer.tsx` 存活（`AIAssistantPanel.tsx` 引用） |
| 其余 22 个 `*.test.ts(x)` | 测的是存活组件，vitest 直接运行，**保留** |
| `src/main.tsx` | 入口，零入度属正常 |

---

## 6. Wails 绑定：前端从未调用（263 个，其中 **95** 个可删）

`guiapp/frontend/wailsjs/go/main/App.d.ts` 共导出 **1,269** 个方法；扫描 758 个前端文件与 6,067 个 Go 文件。

| 分类 | 数量 | 处理 |
|---|---:|---|
| 前端未引用，**剥注释后 Go 生产代码也 0 引用** | **95** | **可删**（连 Go 侧导出方法一起删） |
| 前端未引用，但 Go 内部有调用 | **168** | **只能降级为非导出**，不可删（如 `GetTempDir` 有 40+ 处内部调用） |

> 复验补充：95 = 本清单原 94 + 漏列的 `TryHandlePassthroughSlashCommand`（`guiapp/app_passthrough.go:127`，全仓唯一命中即定义行）。
> 95 + 168 = 263，与复验统计的"前端未引用总数"一致。
> **不要用"名字在别处出现过"当引用判据**：本仓库里 `guiapp/manager_interfaces.go` 有无同名接口方法、`MaClawSrv`/`datasrv`/`tui` 有同名函数，
> 还有 `strings.Contains(caller, "AccumulateLLMTokenUsage")` 这类调用者名字符串 —— 这些都不是对 App 方法的调用。

**95 个可删绑定**：

```
AccumulateLLMTokenUsage      AppendRecordedAudioBytes     ApproveCodingWorkbenchPlan
ArchiveVESession             BranchConversationAt         ClearDenialPause
CodingKnowledgeDeleteByScope CodingKnowledgeReset         CreateProvisionedCloudWorkspaceTask
DeleteTemplate               EstimateSpeakerCountAudioBase64  ExportAgentSkillDir
ForkConversationToProject    GetBrowserSessionSnapshot    GetCapabilityGapDetector
GetCodingWorkbenchPendingPlan GetConfigSchema             GetExperienceExtractor
GetGossipAutoPublish          GetGossipClient              GetHubUserInvitations
GetIMAuditStats               GetInferenceDiagnostics      GetLLMTrajectoryLogging
GetMaclawBaseDir              GetMaclawLLMPanelState       GetOrchestrator
GetPassthroughCommand         GetProxyConfig               GetSessionStarter
GetSharedContext              GetSkillExecutor             GetSkillHubClient
GetSkillMarketClient          GetSkillRunner               GetSkillSuiteVersions
GetUIShellConfig              GetVSCodeACPStatus           GossipBrowse
GroupDiscussionCreateConsultation  GroupDiscussionListInvites  InferExpertCapabilityTier
IsToolBeingInstalled          KnowledgeEnableSources       KnowledgeExportSnapshot
KnowledgeSuppressCards        ListArchiveMemories          ListExternalSkillDirs
ListSSHBackgroundTasks        ListSkillRunArtifacts        ListSkillUploadQueue
ListTemplates                 ListThirdPartyHardwareDevices MaximiseAndSaveGeometry
OpenComputerUseLastHistoryCSV OpenSkillRunArtifactForOwner PeekExternalSkillDirs
PeekLanguage                  PeekMaclawLLMCurrentProvider PeekRemoteMachineToken
PinMemory                     PingSkillHub                 PrepareVSCodeACP
PrepareVSCodeACPExtension     ProvideSwarmUserInput        QueryAuditLog
RateHubSkill                  RejectCodingWorkbenchPlan    RestoreCodingWorkbenchCheckpoint
RestoreWindowGeometry         RetryBlockedSkillUpload      RetrySkillUploadQueue
RevealSkillRunArtifactForOwner RunDoctorFormatted          RunEnvironmentCheckCLI
RunRemoteClaudeSmoke          SearchSkillHub               SendHardwareWelcomeAudio
SetLLMTrajectoryLogging       SetTrialReflectEnabled       StartBrowserSession
StartRemoteClaudeSession      StartWorkflowDirect          StopBrowserSession
StopLansenger                 StopQQBot                    StopTelegram
TestInference                 TryHandlePassthroughSlashCommand   UnpinMemory
UpdateConfigBinding           UpdateSkillRunArtifactCache  ValidateSkillHub
VerifyAndActivateNLSkill      WaitWeixinQRLogin
```

---

## 7. `cmd/` 子命令（11 个）— 8 个无构建引用，但**不等于死代码**

判定口径：搜 `build_win*.bat` / `Makefile` / `.github/workflows` / `scripts/` / `deploy/`（排除 `build/` 产物与 `docs/`）。

| 子命令 | 构建引用 | 判定 |
|---|---|---|
| `cmd/maclaw-gui` | CI `main.yml` + Makefile + 3 个 build 脚本 | 在用 |
| `cmd/maclaw-tool` | CI + Makefile + build 脚本 | 在用 |
| `cmd/maclaw-acp-bridge` | CI + `build_win.bat` | 在用 |
| `cmd/genphasemeta` | **CI `workflow-phase-metadata.yml`**（`go run ./cmd/genphasemeta` + 定向测试） | 在用 |
| `cmd/maclaw-needle` | `scripts/needle_finetune.py` | 在用（离线微调） |
| `cmd/office-read-dual-report` | `scripts/test-officeread-fixtures.ps1` / `test-officeread-acceptance.ps1`（发布门禁） | 在用 |
| `cmd/pptx-preview` | 无 | **保留** —— 与在用 `preview_pptx` 共用 `corelib/pptx.RenderPreview`，是人工校验入口 |
| `cmd/ws-test` | 无 | **人工诊断入口**（OpenAI Responses WebSocket，读 `~/.maclaw/config.json`），对应通道在用 |
| `cmd/volcengine-probe` | 无 | **人工诊断入口**（Volcengine HTTP 探针），对应 provider 在用 |
| `cmd/connnectMaClaw` | 无 | **人工诊断入口**（第三方接入握手测试客户端） |
| `cmd/quick_test` | 无 | **真孤儿** —— 硬编码 8 个模板名打印 `workflow/v2` 匹配结果的一次性脚本 |

**建议**：`quick_test` 可删；其余 4 个人工入口**不要删**，若嫌根目录乱，统一挪到 `tools/devprobe/` 并在 README 登记。

---

## 8. `corelib/*/cmd/` 调试主程序（38 个）

`go list` 识别出 38 个 `main` 包，全部位于 `corelib/` 下（无构建引用，不被任何包 import，仅手动 `go run`）：

- **`corelib/tts/cmd/`（32 个）**：`piper_*` 系列 **21** 个（`piper_bench` `piper_debug` `piper_dur_compare` `piper_dur_debug` `piper_dur_print` `piper_enc_debug` `piper_exact_test` `piper_flow_debug` `piper_g2p_test` `piper_noisy_zp_test` `piper_onnx_dur_full` `piper_onnx_dur_test` `piper_onnx_zp_nihao` `piper_profile` `piper_sdp_debug` `piper_sdp_durs` `piper_shijie` `piper_test` `piper_vocoder_test` `piper_wavenet_debug` `piper_conv_post_test`）、`compare_*` 5 个、`kokoro_asr_eval` `asr_verify` `inspect_gguf` `synthesize` `tts_asr_test` `tts_from_py_enc`
- **`corelib/asr/cmd/`（2 个）**：`asr_live_test` `asr_record`
- **`corelib/ocr/cmd/ocrdbg`**、**`corelib/onnxrt/cmd/`（3 个）**：`genfuzz` `gengolden` `onnxdump`

> 复验更正：§8 原写"`corelib/tts/cmd/` 33 个、`piper_*` 22 个"，实际为 **32 个 / 21 个**（`go list` 复算）。corelib 下 main 包总数 38 不变。

**判定**：不打包进产品、也不影响构建，但其中 22 个 `piper_*` 服务于**已确认未接线的 piper 引擎**（见 [§2.2](#22-b-档中等置信87-个部分符号死)）。若决定放弃 piper，这 22 个可一并清理；`kokoro`/`asr`/`onnxrt` 相关调试工具建议保留。

---

## 9. 版本库内的垃圾文件（被 git 跟踪，首轮未清）

首轮只清了**未跟踪**的工作区文件，版本库里被跟踪的垃圾仍有 **17** 个（另有 456 个二进制资源，绝大多数是测试夹具，**不动**）：

**`deploy/logs/` 运行日志与 PID（14 个）**：

```
deploy/logs/full-20260606-123808.err.log   full-20260606-123808.out.log
deploy/logs/full-20260606-132848.err.log   full-20260606-132848.out.log
deploy/logs/full-current.err               full-current.out     full-current.pid
deploy/logs/hub-only-20260606-104416.err.log   hub-only-20260606-104416.out.log
deploy/logs/hub-only-20260606-104457.err.log   hub-only-20260606-104457.out.log
deploy/logs/hub-only-current.err           hub-only-current.out  hub-only-current.pid
```

**macOS 元数据（3 个）**：`.DS_Store`、`build/.DS_Store`（14,340 B）、`build/windows/.DS_Store`（6,148 B）

> 复验更正：① `deploy/logs/` 是 **14** 个不是 16 个；② `build/.DS_Store`、`build/windows/.DS_Store`
> **并未在首轮删除** —— 它们在磁盘上仍在、且仍被 git 跟踪（`git status` 只报了 `.DS_Store` 等 4 条 `D`）。
> 建议：`deploy/logs/` 与 `build/.DS_Store` 加进 `.gitignore` 后 `git rm --cached`。

---

## 10. `sparse-checkout` 未检出文件（1,513 个）

仓库开着 `core.sparseCheckout = true`，索引 12,628 个文件中 **1,513 个是 `skip-worktree` 未检出**。

**关键风险**：`git status` **对这些文件不报 `D`**，所以 `iWorker/`、`iWorkerCenter/`、`iWorkerCloud/`、`dangbei-api-deployment/`、`conductor/`、`Ins-maclaw/`、`.kiro/`（451 个）等整模块其实已经从工作区消失，但完全看不出来。

**判定**：代码仍在版本库历史里，只是没人看见。清理需用 `git rm --cached`（普通 `rm` 无效）。**明细请先看 `git ls-files -v | Select-String '^S'`，不要只看 `git status`。**

---

## 11. ⚠️ 待你决策：上一轮清理留下的 4 个待提交删除

首轮清理中，这 4 个根目录文件其实是**被 git 跟踪的**，删除后工作区已有 `D` 记录（尚未提交）：

| 文件 | 版本库大小 | 最后提交 |
|---|---:|---|
| `.DS_Store` | 14 KB | 2026-06-17 znsoft |
| `audio_spectrum.png` | 1.0 MB | 2026-08-03 mayong |
| `audio_wave.png` | 10 KB | 2026-08-03 mayong |
| `maclaw2.png` | 141 KB | 2026-04-10 znsoft |

**它们确实是非代码文件**（音频频谱/波形调试图 + 图标草稿 + macOS 元数据），删除合理；但需要**显式提交**才能在版本库里生效。

> 注意：工作区当前另有 **65 个 `M`**（corelib/intent、llmpool、guiapp/frontend 等）是**你自己的在途修改**，本次未触碰。提交这 4 个删除时请只暂存这 4 个路径。

---

## 12. 需重构而非删除：重复实现

| 对象 | 规模 | 说明 |
|---|---|---|
| `hub/` ↔ `hubcenter/` | **12 个同名 internal 包**（app/auth/backup/config/diagnostics/entry/httpapi/llmservice/mail/notification/skill/store），互相 import 均为 0（已复验） | 复制粘贴的两份，都在部署链路里，**只能抽公共层** |
| `corelib/{feishu,wecom,dingtalk}` ↔ `hub/internal/{feishu,wecom,dingtalk}` | 3 组 | 前者是"独立产品轻量网关"，后者是"完整 hub 适配器栈"，同名不同定位 → 需产品定取舍 |
| `corelib/agent/task_understanding.go` ↔ `guiapp/im_task_understanding.go` | 2 份 | 见 [§1](#1-核心发现agent-unification-迁移半成品最有价值的清理目标)，**逻辑已分叉** |
| `corelib/agent/selfconfirm.go` ↔ `guiapp/im_message_handler_selfconfirm.go` | 2 份 | 同上 |
| `corelib/tts/` | 3 套引擎：`kokoro/`（**已接线**）、`piper_*`（未接线）、`melotts.go`（未接线） | 建议只留 kokoro |
| 前端 Markdown 渲染 | 3 套：`AIAssistantPanel.tsx` / `IncrementalMarkdownRenderer.tsx` / `aiAssistantMarkdown.tsx` | 需人工判定是否可合并 |

---

## 13. 建议执行顺序

| 优先级 | 动作 | 规模 | 风险 | 验证 |
|---|---|---|---|---|
| **P1** | 删 `gui/` 整个目录 | 3 文件 + docs | **无**（编译本就失败） | `go build ./...` |
| **P1** | 提交 §11 的 4 个删除 | 4 文件 | 无 | 只暂存这 4 条路径 |
| **P1** | 清理 §9 的 17 个跟踪垃圾 | 17 文件 | 无 | `git rm --cached` + `.gitignore` |
| **P2** | 删前端孤儿 §5（28 + 6 测试） | 34 文件 | 低 | `cd guiapp/frontend; npm test` |
| **P2** | 删 guiapp 高置信死文件 §2.1.2 —— **11 个**（原 13，复验撤销 2 个） | 11 文件 | 低 | `go build ./...` + `go test ./guiapp/` |
| **P2** | 删 `cmd/quick_test` | 1 目录 | 低 | `go build ./...` |
| **P2** | 删 95 个未用 Wails 绑定 | 95 方法 | 低（需重生成 `wailsjs`） | `wails generate module` |
| **P3** | 处理 §1 迁移半成品（二选一收敛） | 4 组 | **中**（改行为） | `go test ./guiapp/ -run Task` |
| **P4** | 定夺 §3 零引用包 9 个 | 9 包 | 中（含回归装置） | 先补 CI 全量测试 |
| **P4** | 定夺 piper 引擎及其 22 个调试命令 | ~42 文件 | 中 | 确认无配置/模型指向 |
| **P5** | `corelib/opus` 39 个死文件 | 39 文件 | **高** | **单独立项**，勿顺手删 |
| **P5** | `hub`/`hubcenter` 抽公共层 | 11 包 | **高** | 需架构设计 |

**每步之后跑**：

```powershell
go build ./...
go vet ./...
go test ./corelib/... ./guiapp/...
cd guiapp/frontend; npm test
node scripts/check-agent-architecture.mjs
```

> 已知差异：`CodePreviewPanel.mergedHeader.test.tsx` 在 HEAD 上就失败（属在途修改），对比时不要算作本次引入。

---

## 14. 保护清单（勿删）

| 对象 | 原因 |
|---|---|
| `third_party/shine-mp3` | 根 `go.mod` 有 `replace` 指向，删除则主模块编译失败 |
| `guiapp/internal/systray`、`datasrv/` | 根 `go.mod:147,149` 亦有 `replace` 指向 |
| ~~`vscode-ext/`：`guiapp/vscode_acp_ext_asset.go` 用 `//go:embed` 把 .vsix 编进主程序~~ **（第二轮更正：理由指错了目录）** | 真正的 `//go:embed` 目标是 **`guiapp/vscode_ext_asset/maclaw-acp.vsix` + `version.txt`**（已提交入版本库，见 `vscode_acp_ext_asset.go:12,15`）。`vscode-ext/` 是扩展**源码**，由 `build_win.bat:99` → `vscode-ext/build-vsix.ps1` 与 CI（`main.yml:277,1110,1661,1897`）编译后拷进前者。**两者都要留，但理由不同** |
| `guiapp/hello-maclaw.wav` | `guiapp/hardware_welcome_asset.go:9` 是 `//go:embed hello-maclaw.wav`；`.gitignore` 有白名单。**注意与根目录的 `hello-maclaw.m4a` 只差扩展名** |
| `test_16k_mono.wav`、`zhou_16k.wav`、`beiing_16k.wav` | 被 `corelib/asr/moonshine_test.go`、`guiapp/live_diarization_probe_test.go` 直接读取 |
| `corelib/opus/`、`corelib/amrnb/` | 被 `audioconv` / `tts` 引用的编解码实现 |
| `corelib/pptx/`、`cmd/pptx-preview` | `preview_pptx` 三处在用（见 [§0](#0-勘误先看这节)） |
| `corelib/tool/routingeval`、`corelib/longhorizon/eval`、`corelib/experience/counterfactual`、`corelib/tool/routingarch` | 回归装置 / 架构守卫，自带数据集 |
| `.codegraph/.gitignore` | 该目录唯一被 git 跟踪的文件 |
| **`guiapp/frontend/dist/`**（181 文件） | 第二轮新增：`guiapp/assets_desktop.go:7` 是 `//go:embed all:frontend/dist` —— **删了 GUI 编译失败** |
| **`ClawMateMaker/frontend/dist/`、`TigerProxy/frontend/dist/`、`TigerProxy/assets/`** | 第二轮新增：各自 module 的 `//go:embed` 目标 |
| **`guiapp/build/`、`MaClawSrv/{admin_web,user_web}/`、`guiapp/petpack/bundled/`、`guiapp/builtin_skills/`** | 第二轮新增：均为 `//go:embed` 目标（共 27 条 embed 已逐条验证，0 缺失） |
| **`scripts/check-agent-architecture.mjs` 钉住的 180 个文件/源码片段** | 第二轮新增：该脚本是**架构门禁**，`requireText` 断言源码文本。已确认钉住 `corelib/agentservice/semantic_behavior_snapshot.go`（10 条）、`guiapp/coding_subagent_rollout.go`、`corelib/agent/shared_capabilities.go` |
| **根包 `github.com/RapidAI/CodeClaw`** | 第二轮新增：零 import 是**设计使然**（`generate.go` 自述无运行时代码、仅作 `go:generate` 锚点；`live_probe.go` 带 `//go:build ignore` 由 `_run_live_probe.cmd` 调用）—— **不是死代码** |
| 根目录 **81 个 `M`** 改动（第二轮实测，前值为 65） | 你自己的在途修改。**执行任何清理前先重跑 `git status`**；已确认 `corelib/agentservice/semantic_behavior_snapshot.go`、`corelib/intent/keyword_registry.go` 两个候选文件正在被修改，**不可动** |

---

*本清单由 `go list` import 图 + `deadcode` 调用图 + 符号交叉校验 + 字符串级动态引用复核 + CI/脚本引用扫描交叉验证生成。*
