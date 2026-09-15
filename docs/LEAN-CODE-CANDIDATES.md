# MaClaw 代码精简清单

> 生成日期：2026-09-15 ｜ 仓库：`D:\workprj\aicoder`（module `github.com/RapidAI/CodeClaw`）
> 状态：**候选清单，尚未执行任何删除**
> 判定方法：git 索引分析 + 构建链路追踪（`Makefile` / `build_win.bat` / `.github/workflows` / `deploy_*.cmd`）+ import 图统计

> 📎 **配套文档**
> - **[`DEAD-CODE-REPORT.md`](./DEAD-CODE-REPORT.md)** —— 死代码分析方法与结论（`go list` import 图 + `deadcode` 调用图 + 符号交叉校验）
> - **[`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)** —— **全量逐个枚举**（231 个全死文件、9 个零引用包、28 个前端孤儿、94 个死绑定、17 个跟踪垃圾、1,513 个未检出文件）
>
> ⚠️ 本文档第 2 节已于 2026-09-15 按复核结果修正（原判 `cmd/pptx-preview` 等 7 项为"待删"有误）。
> 三份文档冲突时，以 `DEAD-CODE-APPENDIX.md` 为准。

---

## 摘要

| 指标 | 数值 |
|---|---:|
| 可立即清理体积 | **约 4.3 GB** |
| 待确认代码文件 | 约 1,513 个（未检出模块）+ 数十处代码级候选 |
| 最大单项 | `official-sensevoice/` 1.15 GB |
| 最大技术债 | `hub/` 与 `hubcenter/` 11 个复制粘贴的同名包（不可删，需重构） |

分级：`可删` = 有证据表明无引用 ｜ `待确认` = 证据不足或涉及业务取舍 ｜ `保留` = 主链路在用

---

## 执行记录（2026-09-15）

本轮按「只清理非代码文件」执行，**代码 / 编译脚本 / 资源一律未动**。

| 项目 | 结果 |
|---|---|
| 根目录文件 | 124 → 80 个 |
| 根目录体积 | 约 1,487 MB → 754 MB（**−733 MB**） |
| 已删目录 | `dist/`、`tmp/`、`dev/`、`output/`、`graphify-out/`、`.research-wuming-xiaozhi/`、`.research-esp-ml307/`、`.maclaw-tmp/`、`tmp-tail-check/`、`build-host-tests/`、`.audit-mobile/` |
| 已删文件 | 41 个（`gui.exe` 147MB、`tui.exe` 110MB、8 个 `build_win_output*.log`、3 个 `phase7b-*.log.err`、12 张调试截图、8 个 About 设计稿、`.tmp-*` 系列等） |
| 本轮实际释放 | **约 1.9 GB** |
| 未执行（按指示保留） | 模型与测试数据：`official-sensevoice/`、`tts_eval/`、`sensevoice-small-f16.gguf`、`campplus-cn-common.cmpg` |
| 未执行（性质待定） | 根目录 14 个调试音频素材（约 2.1 MB） |
| 未完成（权限不足） | `.codegraph/codegraph.db` 等 723 MB，见 [1.2](#12-构建产物与缓存) 备注 |

删除方式：全部走系统回收站（可恢复）。

---

## 目录

- [0. 前置发现：仓库开着 sparse-checkout](#0-前置发现仓库开着-sparse-checkout)
- [1. 可立即删除](#1-可立即删除零风险)
- [2. 待确认后删除](#2-待确认后删除)
- [3. 需重构而非删除](#3-需重构而非删除)
- [4. 保护清单（勿删）](#4-保护清单勿删)
- [5. 执行计划](#5-执行计划)
- [附录：判定依据](#附录判定依据)

> 全量枚举与勘误另见 [`DEAD-CODE-APPENDIX.md`](./DEAD-CODE-APPENDIX.md)。

---

## 0. 前置发现：仓库开着 sparse-checkout

这是本次 review 最重要的发现，先读这一节再动手。

```
core.sparseCheckout      = true
core.sparseCheckoutCone  = true
已检出目录               ≈ 25 个
git ls-files 总数        = 12,628
未检出（skip-worktree）  = 1,513
```

**后果**：`git status` 对未检出文件**不报 `D`**。也就是说，一批模块虽然仍完整地躺在版本库里，却已从工作区消失——而且从 `git status` 完全看不出来。判断某模块是否还在仓库里，必须看 `git ls-files -v` 中的 `S` 标记，不能只看 `status` 或磁盘目录。

### 0.1 已事实下线的模块（未检出文件分布）

| 目录 | 未检出文件数 | 性质 |
|---|---:|---|
| `.kiro/` | 451 | Kiro 工具规格文档与截图 |
| `mobile/` | 270 | 移动端 Flutter 客户端（另有 140 个文件已检出，是活跃产品） |
| `iWorkerCenter/` | 227 | 旧 iWorker 中心服务 |
| `dangbei-api-deployment/` | 145 | 当贝 API 部署 |
| `iWorkerCloud/` | 93 | 旧 iWorker 云 |
| `.agents/` | 90 | 工具配置 |
| `iWorker/` | 60 | 旧 iWorker 主项目 |
| `tmp-weixin/` | 44 | 微信临时调试 |
| `conductor/` | 28 | 用途不明 |
| `Ins-maclaw/` | 27 | 安装包工程 |
| `ThirdAPIDemo/` | 18 | 第三方 API 示例 |
| `mcps/` | 10 | MCP 示例 |
| `paper-review/` | 9 | 论文评审 |
| `testdata/` | 8 | 根级测试资产 |
| `srvdemo/` | 8 | 服务 demo |
| `tools/`（部分） | 8 | 仅 `windowsresgen` 已检出 |
| 其他（`.workflow` `.claude` `.aicoder` `stats` `ecapa-raw` `assets` `data` `.vscode`） | 14 | 工具产物 |

**处理建议**：先与相关同学确认 `iWorker*` / `dangbei-api-deployment` / `conductor` / `Ins-maclaw` / `ThirdAPIDemo` / `srvdemo` / `.kiro` 是否彻底废弃。确认后用 `git rm -r --cached <路径>` 提交删除——它们不在工作区，用普通 `rm` 无效。

---

## 1. 可立即删除（零风险）

约 **4.3 GB**，全部是构建产物、第三方副本或临时文件，不含任何源码。

### 1.1 第三方仓库克隆与模型副本

> **本轮未执行** — 按你的指示，模型与测试数据保留。（另注：`xiaozhi-esp32-server-ref/`、`esp-wifi-connect-reference/` 虽是第三方克隆，但属于代码，按定义不清理。）

- [ ] `xiaozhi-esp32-server-ref/` — 13 MB — 第三方「小智」服务端克隆，自带 LICENSE/README，`git ls-files` = 0
- [ ] `esp-wifi-connect-reference/` — 0.5 MB — espressif `esp-wifi-connect` 组件克隆，自带 LICENSE，`git ls-files` = 0
- [ ] `official-sensevoice/` — **1.15 GB** — SenseVoice 模型/代码副本（`.gitignore:243`）
- [ ] `tts_eval/` — 509 MB — TTS 评测数据（`.gitignore:191`）

### 1.2 构建产物与缓存

> 本小节目录**已全部清理完毕**（2026-09-15），回收站可恢复。

- [x] `dist/` — 981 MB — 构建产物（`.gitignore:2`）
- [x] `dev/` — 124 MB — 临时开发目录（`.gitignore:12`）
- [x] `tmp/` — 105 MB — 临时目录（`.gitignore:20`）
- [x] `.research-wuming-xiaozhi/` — 10.5 MB — 调研缓存（`.gitignore:249`）
- [x] `graphify-out/` — 6.8 MB — 分析产物（`.gitignore:251`）
- [x] `.maclaw-tmp/` — 4.1 MB — 临时目录
- [x] `.research-esp-ml307/` — 0.3 MB — 调研缓存（`.gitignore:249`）
- [x] `output/` — 0.3 MB — 构建输出（`.gitignore:21`）
- [x] `.audit-mobile/` — 0.5 MB — 一次性审计目录
- [x] `tmp-tail-check/` — 0.1 MB — 一次性检查目录
- [x] `build-host-tests/` — 0.1 MB — 一次性测试目录
- [ ] `.codegraph/` — **713 MB** — 代码图缓存（git 仅跟踪 1 个文件，内容可清）

> **`.codegraph/` 未完成**：`codegraph.db` 被 Windows 服务 **`MaClawDataSrv`**（Session 0，自动启动）占用，当前非管理员无法停止。需管理员 PowerShell 执行：
> ```powershell
> Stop-Service MaClawDataSrv
> Remove-Item D:\workprj\aicoder\.codegraph\codegraph.db* -Force   # 保留 .gitignore
> Start-Service MaClawDataSrv
> ```

> 后四项（`.maclaw-tmp`、`tmp-tail-check`、`build-host-tests`、`.audit-mobile`）目前**未被 .gitignore 覆盖**，建议先补规则再清理。

### 1.3 根目录散落文件（约 732 MB / 40+ 项）

**大文件**

- [ ] `sensevoice-small-f16.gguf` — 470 MB — **本轮保留**（模型，按指示）
- [x] `gui.exe` — 147 MB — 构建产物（`.gitignore:78`）
- [x] `tui.exe` — 110 MB — 构建产物（`.gitignore:149`）
- [ ] `knowledge.test` — 30 MB — **本轮保留**（属测试数据，按指示；`.gitignore:161`）

**日志**

- [x] `build_win_output.log`、`build_win_output_0913/0914/0914b/0914c/0914d/0914e/0915a.log`（8 个）— 约 220 KB（`.gitignore:237`）
- [x] `phase7b-wake-restart-{echoear,fangtang,waveshare}-build.log.err`（3 个）— 约 20 KB

**调试截图与素材**（约 4.6 MB）

- [x] `tmp_maclaw_{latest,real,launch,current,latest_max,max,print}.png`、`tmp_crop.png`
- [x] `about-appicon.png`、`about-verify.png`、`about-verify-dark.png`、`about-redesign-mockup.png`
- [x] `maclaw2.png`、`04-flat-modern.jpg`、`audio_spectrum.png`、`audio_wave.png`

**About 改版设计稿**（约 55 KB）

- [x] `about-verify.html`、`about-verify-dark.html`、`about-verify.css`、`about-redesign-mockup.html`

**语音调试素材**（约 2.1 MB）

> **本轮未执行** — 均为音频资源，性质介于「产品资源」与「临时测试素材」之间，待你确认后处理。

- [ ] `test-waveshare.m4a`、`waveshare.m4a`、`waveshare2.m4a`、`bread.m4a`、`test.m4a`
- [ ] `明明白白我的心.wav` / `.m4a`、`明明白2.wav` / `.m4a`、`码卡龙-唤醒词.m4a`、`北京天气.m4a`
- [ ] `beiing.m4a`、`zhou.m4a`、`hello-maclaw.m4a`

**临时脚本与一次性文件**（约 0.7 MB）

- [x] `.tmp-review-current.diff`、`.tmp-esp-codec-dev-1.3.4.bin`、`.tmp_hub_com3_preview.json`、`.tmp-maclaw-test.exe`
- [ ] `.tmp_recover_tui_patch.diff`（已随上方批次清除）、`.tmp-tree.ps1`、`.tmp-click.ps1`、`.tmp_hub_com3_preview.{go,js}` — 脚本与代码，按定义**保留**
- [ ] `.maclaw-tmp-capture-win.ps1`、`.waveshare-syntax-command.txt`（已删）、`esptool.spec`、`test_funasr.py` — 脚本与代码，按定义**保留**
- [ ] `live_probe.go`、`_run_live_probe.cmd`、`_run_registration_tests.cmd` — 代码与脚本，按定义**保留**
- [ ] `deploy_all_maclinux 2.sh` — 脚本副本，按定义**保留**
- [ ] `deploy_iworker.cmd` — 脚本，按定义**保留**（iWorker 系列已下线，可随该模块一并处理）
- [x] `build_exit.txt`、`.DS_Store`

### 1.4 额外的磁盘空间（不含 git，可安全清理）

以下目录**源码必须保留**，但磁盘上的构建缓存可以清：

| 目录 | 磁盘占用 | 源码规模 | 可清理部分 |
|---|---:|---:|---|
| `mobile/` | 4.3 GB | 410 个 git 文件 | `.gradle/`、Android `build/`（约 4 GB，已忽略） |
| `iot-agentos/` | 1.9 GB | 696 个 git 文件 | ESP-IDF `build-unified-*/`（已忽略） |
| `esp32-nv3023-lcd-test/` | 299 MB | 15 个 git 文件 | `build*/`、`managed_components/`（已忽略） |
| `vscode-ext/` | 132 MB | 35 个 git 文件 | `node_modules/`（已忽略） |
| `guiapp/frontend/` | 约 1 GB | — | `node_modules/`（依赖，重建即可） |

---

## 2. 待确认后删除

### 2.1 `corelib/` 零 import 的死包（**9 个**）

判定方法：用 `go list` 解析全仓 **240 个包**的真实 import 图，与 **158 个 corelib 包**求差集。
（此前的 grep 估算为 6 个，精确分析后修正为 9 个。）

| 包 | 文件数 | 证据 | 风险 |
|---|---:|---|---|
| `corelib/feishu` | 2 | import **0 命中**；仅 `corelib/kernel.go:72` 注释提及。**注意不在 `im/` 下** | 中：与 `hub/internal/feishu` 同名不同定位 |
| `corelib/wecom` | 2 | 同上 | 中：同上 |
| `corelib/dingtalk` | 1 | 同上 | 中：同上 |
| `corelib/experience/counterfactual` | 3 | import 0 命中，但**自带 `evaluator_test.go`** → **回归装置，待确认** | 中：删前先补 CI |
| `corelib/longhorizon/eval` | 4+5 | import 0 命中，**自带 5 个 JSON 样本 + harness 测试** → **回归装置，待确认** | 中：删前先补 CI |
| `corelib/memoryshot` | 4 | import 0 命中 | 低 |
| `corelib/misc` | 5 | import 0 命中；自述「杂项工具」，与 `experience` / `memory` 职责重叠 | 低 |
| `corelib/skillmarket` | 1 | import 0 命中（仅类型定义） | 低 |
| `corelib/tool/routingarch` | 6 | import 0 命中，但**自带测试 + 机检白名单**（架构守卫）→ **待确认** | 中：删前先补 CI |

**仅测试引用（1 个）**：`corelib/tool/routingeval` — 只被 `corelib/tool` 的 `_test.go` 引用，
但含 **40+ 个 JSON 回归样本**与 18KB `runner.go`，是语义路由回归基准，**不是死代码**。

> ⚠️ **关键口径**：根模块**没有**全量 `go test ./...` 的 CI 任务（唯一那条在 `ClawMateMaker` 子模块内）。
> 所以这四个"零 import 但有自测"的包处于"只有手动跑才有保护作用"的状态。
> **删之前建议先给 CI 补 `go test ./corelib/tool/... ./corelib/longhorizon/... ./corelib/experience/...`。**

> 更精确的函数级死代码分析（3,715 个不可达符号、231 个全死文件、52 个高置信）
> 见 **[docs/DEAD-CODE-REPORT.md](DEAD-CODE-REPORT.md)**；
> 全量逐个枚举见 **[docs/DEAD-CODE-APPENDIX.md](DEAD-CODE-APPENDIX.md)**。

### 2.2 `corelib/tts/` 两套未接线引擎

- `melotts.go` + `piper_*.go`（约 18 个文件）：在 `manager.go` / `voice_synthesize.go` / `summary.go` 中 grep `piper` **0 命中**；`melotts` 仅剩注释引用（`g2p.go:8`、`bert_embedding.go:29`）。
- 实际接线到 `manager.go` 的只有 `kokoro/`。
- 删除前需确认没有配置项把 TTS 引擎指向 melotts / piper。

### 2.3 `cmd/` 下的子命令（11 个）— 8 个无构建引用，但**不等于死代码**

> ⚠️ **本节已按 2026-09-15 复核修正**。判定口径补上了 CI 工作流、`scripts/`、`Makefile`，
> 并区分"引用了这个二进制"与"这个功能是否在其他入口（GUI / 工具 / skill）在用"。
> 完整说明见 [`DEAD-CODE-APPENDIX.md` §0 与 §7](./DEAD-CODE-APPENDIX.md#0-勘误先看这节)。

| 状态 | 子命令 | 依据 |
|---|---|---|
| 保留 | `maclaw-gui` | `Makefile:36`、CI `main.yml`、3 个 build 脚本 |
| 保留 | `maclaw-tool` | `Makefile:42`、CI `main.yml`、build 脚本 |
| 保留 | `maclaw-acp-bridge` | `build_win.bat:244`、CI `main.yml` |
| 保留 | `genphasemeta` | `.github/workflows/workflow-phase-metadata.yml:16`（`go run` + 定向测试） |
| 保留 | `maclaw-needle` | `scripts/needle_finetune.py:5` 调用 |
| 保留 | `office-read-dual-report` | `scripts/test-officeread-{fixtures,acceptance}.ps1`（发布门禁） |
| 保留 | `pptx-preview` | **功能在用**：与 `office(action="preview_pptx")`、GUI `PptxPreviewPanel` 共用 `corelib/pptx.RenderPreview`；CLI 是人工校验入口 |
| 保留 | `ws-test` | 人工诊断入口（OpenAI Responses WebSocket），对应通道在用 |
| 保留 | `volcengine-probe` | 人工诊断入口，对应 provider 在用 |
| 保留 | `connnectMaClaw` | 人工诊断入口（第三方接入握手测试），目录名拼写有误但不影响 |
| **可删** | `quick_test` | 硬编码 8 个模板名打印 `workflow/v2` 匹配结果的一次性脚本，无任何引用 |

### 2.3b `gui/` 目录：**编译失败的孤儿包**（可整删）

```
$ go test ./gui
gui\tool_discover_grant_test.go:10:47: undefined: IMMessageHandler
gui\tool_discover_grant_test.go:33:32: undefined: LoopContext
gui\app_legacy_install_transaction_test.go:31:10: undefined: App
gui\app_legacy_install_transaction_test.go:32:19: undefined: NewToolRouter
gui\app_legacy_install_transaction_test.go:36:2: undefined: createSkillZip
FAIL    github.com/RapidAI/CodeClaw/gui [build failed]
```

- `gui/` 的包声明是 `package main`，但 `go list` 显示 **GoFiles = []** —— 没有任何实现文件
- 目录里只剩 3 个 `_test.go` + `docs/`
- 测试引用的 `App`、`IMMessageHandler`、`NewToolRouter`、`createSkillZip` 等**全部 undefined**（代码早已迁到 `guiapp/`）
- `git ls-files gui` = 0，且 `.gitignore:79` 有 `/gui`
- **判定：整个 `gui/` 目录可删除**（它是 guiapp 的前身空壳，且当前连编译都过不了）

### 2.4 `guiapp/frontend/src` 无人引用的模块（**28 个非测试模块 + 6 个连带测试**）

> ⚠️ **本节已按 2026-09-15 复核重写**。原判定只做了"从入口的传递可达性"，
> 会把 `NotificationItem.tsx`（实际被存活的 `NotificationPanel.tsx` 引用 ×8）误列为可删。
> 现采用**两级判定**：① 可达性筛候选 → ② **字符串级复核**（排除 `React.lazy` / 路由表 / 注册表）
> → ③ 剔除 `*.test.ts(x)`。**以第 ② 步为准**。
> 逐个枚举见 [`DEAD-CODE-APPENDIX.md` §5](./DEAD-CODE-APPENDIX.md#5-前端孤儿模块28-个--6-个连带测试)。

| 分类 | 数量 | 文件 |
|---|---:|---|
| **直接孤儿**（全仓零引用，含字符串） | 10 | `workflow/{WorkflowInitiationForm,WorkflowDirectoryPanel,InstanceConfirmationPanel,TerminalNodeConfigPanel}.tsx`（**整目录**）、`ai/LocalAIAssistantView.tsx`（自述"the original AI assistant conversation view"，上一代）、`ai/NotificationToast.tsx`、`remote/{MaclawAppSkillsTab,RemoteDiagnosticsPanel,RemoteSmokeSummaryCard,RemoteStatusCards}.tsx` |
| **级联孤儿**（仅被上面引用） | 2 | `remote/RemoteRoutingCard.tsx`、`remote/RemoteToolDiagnosticsCard.tsx`（均只被 `RemoteDiagnosticsPanel.tsx` ×3） |
| **测试独占**（生产 0 引用，只被自身测试引用） | 3 | `remote/MaclawRolePanel.tsx`、`remote/RemoteSessionCard.tsx`、`settings/DatabaseProfilesPanel.tsx`（+ 连带删 3 个测试） |
| **`useVEPresence` 簇** | 2 | `hooks/useVEPresence.ts`、`ai/VEStatusDot.tsx`（+ 连带删 `useVEPresence.test.tsx`） |
| **群讨论簇**（自成闭环） | 6 | `ai/{AssistantGroupDiscussionDropdown,AssistantGroupDiscussionMenu}.tsx`、`ai/AssistantGroupDiscussionSafeHandoff.ts`、`ai/groupDiscussionTraceFocus.ts`、`ai/useGroupDiscussionControls.ts`、`ai/useGroupSessionActions.ts`（+ 连带删 2 个测试） |
| **孤立工具/类型/barrel** | 5 | `components/preview/index.ts`（**死 barrel**）、`modals/installSkillI18n.ts`、`remote/MCPManagementTypes.ts`、`utils/veApprovalCapabilityCheck.ts`、`utils/wailsReady.ts` |

> ⚠️ **已排除，勿删**：`ai/NotificationItem.tsx`（被 `NotificationPanel.tsx` 引用 ×8）；
> 其余 22 个 `*.test.ts(x)` 测的是存活组件；`main.tsx` 是入口。

### 2.5 前端从未调用的 Wails 绑定方法（**262 个**，其中 **94 个**连 Go 内部也没调用）

`guiapp/frontend/wailsjs/go/main/App.d.ts` 共导出 **1,269** 个方法，经全量比对：

| 分类 | 数量 | 处置 |
|---|---:|---|
| 前端未引用 **且** Go 内部也未调用 | **94** | **可删**（完整清单见 [DEAD-CODE-REPORT.md](DEAD-CODE-REPORT.md) §4.3） |
| 前端未用但 Go 内部有调用 | 168 | **只能降级为非导出**，不可删 |

94 个高置信死绑定中的代表（按功能族）：

- **Hub / SkillHub 市场**：`PingSkillHub`、`SearchSkillHub`、`ValidateSkillHub`、`RateHubSkill`、`GetSkillHubClient`、`GetSkillMarketClient`、`GetHubUserInvitations`
- **浏览器会话**：`StartBrowserSession`、`StopBrowserSession`、`GetBrowserSessionSnapshot`
- **IM 启停**：`StopLansenger`、`StopQQBot`、`StopTelegram`、`WaitWeixinQRLogin`
- **记忆**：`PinMemory`、`UnpinMemory`、`ListArchiveMemories`
- **知识库**：`KnowledgeEnableSources`、`KnowledgeExportSnapshot`、`KnowledgeSuppressCards`
- **技能产物**：`ListSkillRunArtifacts`、`OpenSkillRunArtifactForOwner`、`RevealSkillRunArtifactForOwner`、`UpdateSkillRunArtifactCache`、`RetryBlockedSkillUpload`、`ListSkillUploadQueue`
- **VSCode ACP**：`GetVSCodeACPStatus`、`PrepareVSCodeACP`、`PrepareVSCodeACPExtension`
- **窗口/几何**：`MaximiseAndSaveGeometry`、`RestoreWindowGeometry`
- **远程调试**：`RunRemoteClaudeSmoke`、`StartRemoteClaudeSession`
- **其他**：`AccumulateLLMTokenUsage`、`QueryAuditLog`、`RunDoctorFormatted`、`RunEnvironmentCheckCLI`、`TestInference`、`UpdateConfigBinding`、`DeleteTemplate`、`ListTemplates`、`GetConfigSchema`

> `GetTempDir()` 前端 0 命中但 Go 内有 40+ 处调用 → 属于"168 个"那一类，**只能降级为非导出，不可删**。

### 2.6 `guiapp/` 内的临时与遗留文件

```
guiapp/LegacyTaskOutput.csv        guiapp/_header_temp.txt          guiapp/notes.txt
guiapp/_run_tests.js               guiapp/_run_config_fix.ps1       guiapp/_run_computer_use_tests.js / .bat
guiapp/_patch_test.py              guiapp/LegacyTask/task_result.txt
guiapp/frontend/.utilities-critique-vite.log / .err.log
guiapp/frontend/.utilities-expert-layout-vite.log / .err.log
guiapp/frontend/.websearch-tsc.log
guiapp/frontend/.npm_cache/_logs/*.log
```

### 2.7 性质不明、需业务确认的文件

| 文件 | 体积 | 情况 |
|---|---:|---|
| `startup_chime.wav` | 335 KB | 全仓无代码引用，但文件名暗示是产品启动提示音，可能待接入 |
| `campplus-cn-common.cmpg` | 27 MB | 说话人分离模型；代码从 app model 目录加载（`docs/campplus-go-diarization.md`），根目录这份是本地副本，`.gitignore:242` 已忽略 |
| `wx_article.html` | 3.7 MB | 已被 git 跟踪，无代码引用；疑似公众号文章导出 |
| `guiapp/hello-maclaw.wav` | `guiapp/hardware_welcome_asset.go:9` 用 `//go:embed hello-maclaw.wav` 引用，删了编译失败（可删的是根目录的 `hello-maclaw.m4a`，不是这个 `.wav`） |

---

## 3. 需重构而非删除

| 问题 | 规模 | 说明 |
|---|---|---|
| **`hub/` 与 `hubcenter/` 11 个同名包复制粘贴** | `app` `auth` `backup` `config` `diagnostics` `entry` `httpapi` `llmservice` `mail` `notification` `skill` `store` | 两者**互相 import 均为 0 命中**。是两个不同服务（`hub` = 远程控制，`hubcenter` = 目录与入口解析），都还在 `deploy_all.cmd` 中，**不能删**，但应抽公共层。这是全仓最大的一份重复。 |
| **Markdown 渲染 ≥ 3 套** | `src/**/*arkdown*` 命中 19 个文件 | `ai/aiAssistantMarkdown.tsx` 系列 + `ai/IncrementalMarkdownRenderer.tsx` + `ai/MessageContentRenderer.tsx`(未用) + `ai/CodePreviewMarkdown.tsx` + `common/MarkdownLink.tsx`。先删未用的，再评估收敛 |
| **Remote 探针三文件互包** | `remote_status.go` / `remote_diagnostics.go` / `remote_smoke.go` | Claude 专用版与 Tool 通用版各出 `Check*` + `Get*` 两个包装 |
| **会话 / 历史列表三代** | `layout/SidebarHistorySessions.tsx` / `remote/RemoteSessionList.tsx` / `remote/MemorySessionHistoryTab.tsx` | 均在用，属功能分化，待确认能否合并 |
| **设置面板多层壳** | `SettingsPage → SettingsActiveContent → SettingsTabsRail / SettingsPanelErrorBoundary / SettingsPanelFallback` + `GeneralSettingsPanel` + `GeneralAdvancedSettingsPanel` + `GeneralSettingsOptionGrid` | 待确认 |
| **状态徽章三套** | `ai/VEStatusDot.tsx` / `settings/ConnectionStatusBadge.tsx` / `ai/WorkbenchIcons.tsx` 的 `StatusGlyph`（12+ 处引用） | 建议统一到 `StatusGlyph` |
| **`corelib/workflow/` 只剩 `v2/`** | 40 个 go 文件 | 无 v1，`v2` 后缀是历史包袱，可考虑重命名 |
| **workflow 双实现** | `corelib/workflow/v2` vs `hub/internal/workflow`（77 go） | `hub/internal/app/bootstrap.go:219` 注释称 "Workflow Engine (removed)... now handled by device-side agent" → hub 侧那份待确认是否死代码 |

---

## 4. 保护清单（勿删）

| 路径 | 原因 |
|---|---|
| `third_party/shine-mp3/` | 根 `go.mod:153` 有 `replace` 指向它，**删除会导致主模块编译失败** |
| `guiapp/hello-maclaw.wav` | `guiapp/hardware_welcome_asset.go:9` `//go:embed` 引用 |
| 根目录 `test_16k_mono.wav`、`zhou_16k.wav`、`beiing_16k.wav` | ASR / 说话人分离测试直接读取：`corelib/asr/moonshine_test.go:61,76`、`corelib/asr/batch_eval_test.go:32`、`guiapp/live_diarization_probe_test.go:29,87,179` |
| 根目录 `sensevoice-small-q8.gguf`（254 MB） | README 指定的默认 ASR 模型；建议移到 `~/.maclaw/models/` 而不是删除 |
| `vscode-ext/` | `guiapp/vscode_acp_ext_asset.go:8` 用 `//go:embed` 把 `.vsix` 编进主程序；`build_win.bat:99` 调用 |
| `mobile/`、`iot-agentos/`、`esp32-nv3023-lcd-test/` 源码 | 第一方产品；大体积来自构建缓存（见 [1.4](#14-额外的磁盘空间不含-git可安全清理)） |
| `hub/`、`hubcenter/`、`MaClawSrv/`、`datasrv/`、`tui/`、`maclaw-cli/`、`TigerProxy/`、`openclaw-bridge/`、`ClawMateMaker/`、`deploy/`、`build/`、`scripts/` | 均在主构建/部署链路中，最后提交集中在 2026-09 |
| `corelib/asr/` 双引擎（sensevoice + moonshine） | `manager.go:126` 同时支持，都在用 |
| `corelib/opus/`（276 文件）、`corelib/amrnb/` | 编解码实现，被 `audioconv` / `tts` 引用 |

---

## 5. 执行计划

```powershell
# 开始前先建分支
& "C:\Program Files\Git\cmd\git.exe" -C D:\workprj\aicoder checkout -b chore/lean-code
```

| 步骤 | 内容 | 风险 | 验证命令 |
|---|---|---|---|
| 1 | [第 1 节](#1-可立即删除零风险) 全部（约 4.3 GB） | 零 | `git status` 应无变化（均未被跟踪） |
| 2 | [第 0.1 节](#01-已事实下线的模块未检出文件分布) 未检出模块，确认后 `git rm -r --cached` | 中 | `git ls-files -v \| Select-String '^S'` |
| 3 | [2.2](#22-corelibtts-两套未接线引擎) + [2.4](#24-guiappfrontendsrc-无人-import-的组件) tts 死引擎 + 孤儿组件 | 中 | `go build ./...` + 前端测试 |
| 4 | [2.1](#21-corelib-零-import-的死包6-个) + [2.3](#23-cmd-下的孤儿子命令7-个) + [2.5](#25-前端从未调用的-wails-绑定方法) 死包 / 孤儿命令 / 未用绑定 | 中 | 每删一个跑一次 `go build ./...` |
| 5 | [第 3 节](#3-需重构而非删除) 重构 | 高 | 单独立项，不与清理混做 |

每步之后执行：

```powershell
go build ./...
node scripts/check-agent-architecture.mjs
```

---

## 附录：判定依据

| 手段 | 命令 |
|---|---|
| 未检出文件 | `git ls-files -v \| Select-String '^S'` |
| 是否被 git 跟踪 | `git ls-files <路径>` |
| 是否被忽略 | `git check-ignore -v <路径>` |
| 目录最后提交时间 | `git log -1 --format=%ci -- <路径>` |
| Go 包引用数 | Grep `github.com/RapidAI/CodeClaw/<包路径>` |
| 构建链路引用 | Grep 目录名 in `Makefile` / `build_win.bat` / `.github/workflows/` / `deploy_*.cmd` |
| 前端组件引用 | Grep 组件名 in `guiapp/frontend/src/**` |
| 前端绑定调用 | Grep 方法名 in `guiapp/frontend/src/**` 与 `App.d.ts` |

**构建入口一览**

- `Makefile`：`check-corelib-deps`（corelib 禁止 import wails/systray）、`check-bindings`、`build-corelib-headless`、`build-tui`、`build-gui`、`build-tool`
- `build_win.bat`：Windows 全量构建，含 `cmd/maclaw-acp-bridge`、`datasrv`、`maclaw-cli`、`vscode-ext\build-vsix.ps1`
- 独立 module：`datasrv/`（`github.com/RapidAI/CodeClaw/datasrv`）
