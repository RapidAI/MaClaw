# MaClaw 死代码报告 — corelib 死包 与 GUI 无用代码

> 生成日期：2026-09-15 ｜ 仓库：`D:\workprj\aicoder`
> 方法：`go list` 真实 import 图 + `deadcode` 静态调用图（多入口）+ 符号交叉校验
> **本报告只做分析，未删除任何文件**

> ⚠️ **勘误（2026-09-15 复核）**：本报告初版有 3 处判定错误 —— `cmd/pptx-preview`（其实功能在用）、
> `corelib/im/feishu`（路径写错，应为 `corelib/feishu`）、"零 import 包 = 死代码"（未考虑回归装置与 CI 口径）。
> 已修正的完整清单与全量文件枚举见 **[`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)**，冲突时以附录为准。

---

## 一、方法与可信度

| 手段 | 用途 | 局限 |
|---|---|---|
| `go list -f "{{.ImportPath}};{{.Imports}};{{.TestImports}}"` | 构建真实 import 图（240 包） | 无 |
| `go list` 可达性 BFS | 从 main 包出发算包级可达 | 无 |
| `deadcode`（多入口） | 函数级不可达分析，**3,715** 个符号 | **不识别反射、接口、注册表** |
| 符号交叉校验 | 对全死文件，检查其符号是否在别处被引用 | 降误报 |

**两个入口**：`cmd/maclaw-gui ./cmd/maclaw-tool ./cmd/maclaw-acp-bridge ./cmd/genphasemeta ./tui ./MaClawSrv ./maclaw-cli ./TigerProxy ./hub/cmd/hub ./hubcenter/cmd/hubcenter`

> ⚠️ **重要前提**：guiapp 的方法大量通过 **Wails 反射**暴露给前端，工具通过**注册表 map**调用。
> `deadcode` 看不到这些路径，因此 guiapp 侧"不可达"数字偏大。本报告用**符号交叉校验**筛出真正可信的部分。

`deadcode` 不可达符号按目录分布：

| 目录 | 不可达符号数 |
|---|---:|
| corelib | 1,952 |
| guiapp | 1,144 |
| hub | 418 |
| tui | 72 |
| hubcenter | 67 |
| MaClawSrv | 50 |
| TigerProxy | 10 |
| maclaw-cli | 2 |

---

## 二、corelib 死包（包级，高置信）

判定：解析全仓 import，与 158 个 corelib 包求差集。

### 2.1 零引用包（9 个）— 无任何 import，也无测试引用

| 包 | 说明 |
|---|---|
| `corelib/dingtalk` | IM 渠道轻量网关，仅 `corelib/kernel.go:72` 注释提及 |
| `corelib/feishu` | 同上（**注意：不在 `im/` 下**） |
| `corelib/wecom` | 同上 |
| `corelib/experience/counterfactual` | 反事实分析，**自带测试 → 回归装置，待确认** |
| `corelib/longhorizon/eval` | 长程任务评测装置（设计文档 §9 P5），**待确认** |
| `corelib/memoryshot` | 记忆快照，无引用 |
| `corelib/misc` | 自述"杂项工具"，与 `experience`/`memory` 职责重叠 |
| `corelib/skillmarket` | 技能市场，仅类型定义，无引用 |
| `corelib/tool/routingarch` | 工具路由架构**守卫**（扫描边界白名单），**待确认** |

> `corelib/{feishu,wecom,dingtalk}` 与 `hub/internal/{feishu,wecom,dingtalk}` **不是新旧两份**：
> corelib 侧自述是"面向独立产品的轻量网关"（各 1 个 `gateway.go`，8-10KB），
> hub 侧是完整适配器栈（各 3-9 文件，47-55KB）。二者定位不同，**需产品定取舍**。
> `guiapp/im_send_file_forward.go:48` 只把渠道名当字符串用；实际桌面端网关走 `lansenger` / `im` / `weixin` / `qqbot`。

### 2.2 仅测试引用的包（1 个）

| 包 | 证据 |
|---|---|
| `corelib/tool/routingeval` | 只被 `corelib/tool` 的 `_test.go` 引用，无生产引用 |

---

## 三、corelib 死代码（文件级）

**231 个文件内所有函数均不可达**（corelib 191 + guiapp 40），经符号交叉校验后分为三档：

| 档位 | 数量 | 含义 |
|---|---:|---|
| **高置信** | **52** | 文件内所有符号在**其他任何文件里都找不到引用** |
| 中（需复核） | **87** | 部分符号在别处被引用 |
| 低（疑似误报） | **92** | 所有符号在别处被引用（接口/注册表/平台文件） |

> 完整的 231 条逐个枚举见 [`DEAD-CODE-APPENDIX.md` §2](./DEAD-CODE-APPENDIX.md#2-全死文件总表231-个)。

### 3.1 高置信死文件（52 个）— 其中 39 个是 Opus 移植代码

⚠️ **这个分布很关键**：52 个高置信文件里有 **39 个属于 `corelib/opus/`**，**13 个为非 Opus**。

```
corelib/opus/internal/libc/{setjmp,time,wchar,wctype}.go
corelib/opus/libopus/silk_{NLSF2A,NLSF_VQ,NLSF_decode,NLSF_del_dec_quant,
  NLSF_encode,NLSF_stabilize,NLSF_unpack,NSQ,NSQ_del_dec,A2NLSF,LP_variable_cutoff,
  bwexpander_32,decode_frame,decode_indices,decode_pulses,decoder_set_fs,
  init_decoder,process_NLSFs,quant_LTP_gains,resampler_down2,resampler_down2_3,
  stereo_MS_to_LR,stereo_decode_pred,...}.go
corelib/opus/libopus/silk_float_*_FLP.go  (11 个)
```

这些是 SILK 编解码器的纯 Go 移植，逐个 C 函数一个文件。**判定为死代码是可信的**（没有任何调用点），
但**不建议直接删**：Opus 被 `corelib/audioconv/opus.go`、`corelib/tts/opus_encode.go` 使用，
删错会破坏音频链路，且这些函数可能是移植未完成的预留代码。**建议单独立项评估。**

### 3.2 高置信死文件（13 个，非 Opus）— 可直接评估删除

其中 **8 个 corelib + 5 个 guiapp**：

| 文件 | 函数数 | 说明 |
|---|---:|---|
| `corelib/agent/selfconfirm.go` | 4 | 自确认逻辑，无引用（**agent-unification 迁移未接线**） |
| `corelib/agent/task_understanding.go` | 3 | 任务理解，无引用（**agent-unification 迁移未接线**） |
| `corelib/im/convert.go` | 2 | IM 消息转换，无引用 |
| `corelib/knowledge/scan_images.go` | 4 | 知识库图片扫描，无引用 |
| `corelib/remote/capability_market_auth.go` | 1 | 无引用 |
| `corelib/remote/event_types.go` | 3 | 无引用 |
| `corelib/remote/machine_profile.go` | 4 | 无引用 |
| `corelib/remote/mobile_launch_types.go` | 1 | 无引用 |
| `guiapp/im_memory_recall_mode.go` | 1 | `normalizeIMMemoryRecallMode` + 6 常量全无引用 |
| `guiapp/im_tool_list_sessions.go` | 1 | `toolListSessions` 从未注册/调用 |
| `guiapp/im_tool_session_control_action.go` | 1 | 同上 |
| `guiapp/session_observer.go` | 1 | `SendAndObserveSession` 无调用者 |
| `guiapp/session_terminal_status.go` | 2 | `normalizeTerminalSessionStatus` / `isTerminalSessionStatus` 无调用者 |

> `selfconfirm.go` 与 `task_understanding.go` 不是普通死代码：文件头自述
> *"Migrated from gui/im_*.go as part of the agent-unification plan"*，但 **guiapp 里的原实现仍在生产路径上运行**，
> 两份逻辑已经分叉。属**迁移收尾工程**，需二选一收敛而非直接删。详见
> [`DEAD-CODE-APPENDIX.md` §1](./DEAD-CODE-APPENDIX.md#1-核心发现agent-unification-迁移半成品最有价值的清理目标)。

### 3.3 中等置信（87 个，部分符号死）

按目录聚集（括号内为"完全未使用符号数 / 文件总符号数"）：

- **`corelib/tts/`**（最大聚集）：`piper.go` `piper_duration.go` `piper_duration_cache.go` `piper_duration_mlp.go`
  `piper_flow.go` `piper_g2p.go` `piper_g2p_arpabet.go` `piper_g2p_en.go` `piper_hifigan.go` `piper_lexicon.go`
  `piper_sdp.go` `text_encoder.go` `phoneme_table.go` `weights.go` `ops.go`（3/18）
  → **与既有结论一致：piper 系列整体未接线，`manager.go` 只接 `kokoro/`**
- **`corelib/needledata/`**：`eval.go` `generate.go` `export.go` `localization_eval.go` `needle_format.go`
  `redact.go` `gate.go` `calibration.go` → 该包整体疑似评测脚手架
- **`corelib/remote/`**：`event_extractor.go`(3/15) `output_pipeline.go`(5/10) `execution_helpers.go`(4/10)
  `session_progress_tracker.go`(2/10) `provider_resolver.go`(3/6) `codex_types.go`(1/7) `output_normalize.go`
- **`corelib/skill/`**：`comparative_distiller.go`(5/8) `semver.go`(4/10) `solidify.go`(3/9) `taxonomy.go`(3/6)
  `state.go`(1/8) `description_quality.go` `execution_preamble.go`
- **`corelib/agentservice/`**：`semantic_behavior_snapshot.go`(16/28) `dynamic_effect_receipt_store.go`(1/6)
- **`corelib/llm/`**：`failover.go`(6/9) `fork_context.go`(3/15) `auxiliary.go`(2/5)
- 其他：`agent/self_review.go`(7/11)、`agent/confirmation_store.go`(2/8)、`agent/pending_media.go`(3/8)、
  `configfile/external_agent.go`(9/19)、`memory/tree/sealer.go`(6/12)、`memory/conflict.go`(2/5)、
  `plugin/adapter_local_mcp.go`(5/12)、`security/llm_review.go`(4/7)、`tool/semantic_schema_gate.go`(6/8)、
  `tool/token_compress.go`(6/15)、`intent/calibration.go`(3/5)、`embedding/tensor/q4_dot_amd64.go`(3/5)、
  `event_emitter.go`(2/4)、`browser/replay_background.go`(1/2)、`guiautomation/replay_background.go`(1/4)

### 3.4 低置信（92 个，勿盲删）

这些文件的所有符号在别处被引用 —— 典型是**接口实现、注册表项、平台特定文件**：

`corelib/plugin/adapter_{mcp,nlskill,script}.go`（插件适配器，注册表调用）、
`corelib/remote/{session_monitor,session_stall_detector,session_io_relay,event_coalescer}.go`、
`corelib/remote/{admin,screen_permission_other,execution_helpers}_*.go`（**平台后缀文件**）、
`corelib/im/router.go`、`corelib/intent/{fusion,keyword_registry,layer1}.go`、
`corelib/memory/backend_json.go`、`corelib/tts/{conv_simd,melotts,flow,duration_predictor}.go`

---

## 四、MaClaw GUI 无用代码

### 4.1 前端：从入口不可达的组件（28 个非测试模块）

方法：从 `main.tsx` / `App.tsx` 出发做传递可达性（495 个模块 → 434 可达），
再对每个候选做**字符串级动态引用复核**（排除 `React.lazy` / 路由表 / 注册表），
并剔除 `*.test.ts(x)`（由 vitest 直接运行，本就不被 import）。

| 分类 | 数量 | 文件 |
|---|---:|---|
| **直接孤儿**（全仓无任何引用，含字符串） | 10 | `workflow/{WorkflowInitiationForm,WorkflowDirectoryPanel,InstanceConfirmationPanel,TerminalNodeConfigPanel}.tsx`（**整目录**）、`ai/LocalAIAssistantView.tsx`（自述"the original AI assistant conversation view"，上一代）、`ai/NotificationToast.tsx`、`remote/{MaclawAppSkillsTab,RemoteDiagnosticsPanel,RemoteSmokeSummaryCard,RemoteStatusCards}.tsx` |
| **级联孤儿**（仅被上面的孤儿引用） | 2 | `remote/RemoteRoutingCard.tsx`、`remote/RemoteToolDiagnosticsCard.tsx`（均只被 `RemoteDiagnosticsPanel.tsx` 引用 ×3） |
| **测试独占**（生产代码 0 引用，只被自己的测试引用） | 3 | `remote/MaclawRolePanel.tsx`、`remote/RemoteSessionCard.tsx`、`settings/DatabaseProfilesPanel.tsx`（+ 连带删 3 个测试） |
| **`useVEPresence` 簇** | 2 | `hooks/useVEPresence.ts`（只被自身测试 ×11）、`ai/VEStatusDot.tsx`（只被前者 ×1）；连带删 `useVEPresence.test.tsx` |
| **群讨论簇** | 6 | `ai/{AssistantGroupDiscussionDropdown,AssistantGroupDiscussionMenu}.tsx`、`ai/AssistantGroupDiscussionSafeHandoff.ts`、`ai/groupDiscussionTraceFocus.ts`、`ai/useGroupDiscussionControls.ts`、`ai/useGroupSessionActions.ts`；连带删 2 个测试 |
| **孤立工具/类型/barrel** | 5 | `components/preview/index.ts`（**死 barrel**，消费方都直接引子路径）、`modals/installSkillI18n.ts`、`remote/MCPManagementTypes.ts`、`utils/veApprovalCapabilityCheck.ts`、`utils/wailsReady.ts` |

> ⚠️ **已排除（勿删）**：`ai/NotificationItem.tsx` 被存活的 `NotificationPanel.tsx` 引用 ×8；
> 其余 22 个 `*.test.ts(x)` 测的是存活组件；`main.tsx` 是入口。
> 逐个枚举见 [`DEAD-CODE-APPENDIX.md` §5](./DEAD-CODE-APPENDIX.md#5-前端孤儿模块28-个--6-个连带测试)。

### 4.2 Wails 绑定：前端从未调用的方法

`guiapp/frontend/wailsjs/go/main/App.d.ts` 共导出 **1,269** 个方法：

| 分类 | 数量 |
|---|---:|
| 前端完全未引用 | **262** |
| 前端未引用 **且** Go 内部也未调用 | **94** |

**94 个高置信死绑定**（节选，完整列表见分析输出）：

```
AccumulateLLMTokenUsage        AppendRecordedAudioBytes       ApproveCodingWorkbenchPlan
ArchiveVESession               BranchConversationAt           ClearDenialPause
CodingKnowledgeDeleteByScope   CodingKnowledgeReset           CreateProvisionedCloudWorkspaceTask
DeleteTemplate                 EstimateSpeakerCountAudioBase64 ExportAgentSkillDir
ForkConversationToProject      GetBrowserSessionSnapshot      GetCapabilityGapDetector
GetCodingWorkbenchPendingPlan  GetConfigSchema                GetGossipAutoPublish
GetHubUserInvitations          GetIMAuditStats                GetInferenceDiagnostics
GetMaclawBaseDir               GetOrchestrator                GetPassthroughCommand
GetProxyConfig                 GetSharedContext               GetSkillExecutor
GetSkillHubClient              GetSkillMarketClient           GetSkillRunner
ListThirdPartyHardwareDevices  SendHardwareWelcomeAudio       GetUIShellConfig
GetVSCodeACPStatus             GossipBrowse                   GroupDiscussionCreateConsultation
GroupDiscussionListInvites     InferExpertCapabilityTier      IsToolBeingInstalled
KnowledgeEnableSources         KnowledgeExportSnapshot        KnowledgeSuppressCards
ListArchiveMemories            ListExternalSkillDirs          ListSSHBackgroundTasks
ListSkillRunArtifacts          ListSkillUploadQueue           ListTemplates
MaximiseAndSaveGeometry        OpenComputerUseLastHistoryCSV  PinMemory / UnpinMemory
PingSkillHub / SearchSkillHub / ValidateSkillHub / RateHubSkill
QueryAuditLog                  RestoreWindowGeometry          RetryBlockedSkillUpload
RunDoctorFormatted             RunEnvironmentCheckCLI         RunRemoteClaudeSmoke
StartBrowserSession            StopBrowserSession             StartRemoteClaudeSession
StartWorkflowDirect            StopLansenger / StopQQBot / StopTelegram
TestInference                  UpdateConfigBinding            UpdateSkillRunArtifactCache
VerifyAndActivateNLSkill       WaitWeixinQRLogin              ...
```

> 另有 **168 个**"前端未用但 Go 内部有调用"的方法（如 `GetTempDir` 有 40+ 处内部调用），
> **只能降级为非导出，不能删**。

### 4.3 GUI Go 侧：全文件不可达

**40 个 guiapp 文件内所有函数均不可达**，交叉校验后：

| 档位 | 数量 | 文件 |
|---|---:|---|
| **高置信（5 个）** | 5 | `im_memory_recall_mode.go`、`im_tool_list_sessions.go`、`im_tool_session_control_action.go`、`session_observer.go`、`session_terminal_status.go` |
| 中等（19 个，部分符号死） | 19 | `manager_interfaces.go`(5/80，接口实现)、`app_init_optimized.go`(11/24)、`im_message_handler_workflow_initiate.go`(15/24)、`openhuman_background.go`(8/21)、`context_compressor.go`(11/13)、`diff_computer.go`(6/9)、`coding_subagent_rollout.go`(9/13)、`im_pending_media.go`(4/8)、`file_snapshot_store.go`、`self_review_adapters.go`、`provider_resolver.go`、`im_tools_create_session_*.go`(4 个)、`im_tools_session_guard.go`、`im_message_handler_selfconfirm.go`、`im_tools_session_send_observe_orchestrator.go`、`remote_tool_update_status.go` |
| 低置信（16 个，勿盲删） | 16 | `app_maclaw_apps_test_helpers.go`、`browser_replay_scheduler.go`、`im_agent_reply_quality.go`、`im_confirmation_action.go`、`im_passthrough.go`、`im_tool_skill_refresh.go`、`im_tools_create_session_{context,project,result,runner}.go`、`im_tools_list_providers.go`、`im_tools_session.go`、`mac_compat_other.go`（平台文件）、`prompt_skill_index.go`、`scheduled_action_type_kind.go`、`screenshot_native_windows.go`（平台文件）、`skill_verification_status.go` |

**为什么低置信不能删**：这些文件的符号在别处出现 —— 典型是
**IM 工具注册表**（`im_tools_*.go` 通过 map 注册）、**平台特定文件**（`*_windows.go` / `*_other.go`）、
**接口实现**（`manager_interfaces.go` 80 个函数，75 个在别处被引用）。

---

## 五、额外发现：`gui/` 目录是一个**编译不过的包**

```
$ go test ./gui
gui\tool_discover_grant_test.go:10:47: undefined: IMMessageHandler
gui\tool_discover_grant_test.go:33:32: undefined: LoopContext
gui\app_legacy_install_transaction_test.go:31:10: undefined: App
gui\app_legacy_install_transaction_test.go:32:19: undefined: NewToolRouter
gui\app_legacy_install_transaction_test.go:36:2: undefined: createSkillZip
...
FAIL    github.com/RapidAI/CodeClaw/gui [build failed]
```

- `gui/` 是 `package main`，但 **GoFiles = []**（没有任何实现文件）
- 只剩 3 个 `_test.go` 和 `docs/` 目录
- 测试引用的 `App`、`IMMessageHandler`、`NewToolRouter` 等**全部 undefined** —— 代码早已迁到 `guiapp/`
- **判定：整个 `gui/` 目录可删除**（它是 guiapp 的前身空壳）

---

## 六、结论与建议执行顺序

| 优先级 | 动作 | 规模 | 风险 |
|---|---|---|---|
| **P1** | 删 `gui/` 整个目录 | 3 测试 + docs | **无**（编译本来就失败） |
| **P1** | 提交根目录 4 个已删的**被跟踪**文件（`.DS_Store`、`audio_spectrum.png`、`audio_wave.png`、`maclaw2.png`） | 4 文件 | 无（须只暂存这 4 条路径） |
| **P1** | 清理 `deploy/logs/` 的 16 个已跟踪日志/PID + `.DS_Store` | 17 文件 | 无 |
| **P2** | 删前端孤儿 28 个模块 + 6 个连带测试 | 34 文件 | 低（跑了可达性 + 字符串级复核） |
| **P2** | 删 guiapp 高置信死文件 5 个 | 5 文件 | 低 |
| **P2** | 删 `cmd/quick_test` | 1 目录 | 低 |
| **P3** | 清理 94 个未使用 Wails 绑定 | 94 方法 | 低（需重生成 `wailsjs`） |
| **P3** | 处理 agent-unification 迁移半成品 4 组（二选一收敛，非直接删） | 4 组 | **中**（会改行为） |
| **P4** | 定夺 corelib 零引用包 9 个 | 9 包 | 中（含 4 个回归装置，建议先补 CI 全量测试） |
| **P4** | 评估 `corelib/tts/piper_*` + `melotts` 及其 22 个调试命令 | ~42 文件 | 中（确认无配置指向） |
| **P5** | 评估 `corelib/opus` 39 个死文件 | 39 文件 | **高，单独立项** |
| **P5** | 逐符号审查 87 个中置信文件 | 87 文件 | 中 |
| **P5** | `hub`/`hubcenter` 抽公共层 | 11 包 | **高，需架构设计** |

> **不要删**：`cmd/pptx-preview`（与在用的 `preview_pptx` 共用渲染器）、
> `cmd/{ws-test,volcengine-probe,connnectMaClaw}`（人工诊断入口）、
> `corelib/tool/{routingeval,routingarch}`、`corelib/longhorizon/eval`、`corelib/experience/counterfactual`（回归装置/架构守卫）。

**验证方式**（每步之后）：

```powershell
go build ./...              # 编译
go vet ./...                # 静态检查
go test ./corelib/... ./guiapp/...   # 单测
cd guiapp/frontend; npm test         # 前端测试
node scripts/check-agent-architecture.mjs
```

**注意**：本项目单测里已有 1 个 HEAD 自带的失败用例（`CodePreviewPanel.mergedHeader.test.tsx`），
清理前后对比时不要把它算作本次引入。

---

> 全量逐个枚举（231 个死文件、9 个零引用包、28 个前端孤儿、94 个死绑定、17 个跟踪垃圾、1,513 个未检出文件）
> 见 **[`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)**。
