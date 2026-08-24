# Documentation Index

This directory contains architecture notes, design records, and operational guides.

## LLM 服务商调度

- [同倍率服务商 Load Balance](design/llm-provider-multiplier-lb-zh.md)：Hub / HubCenter 按当前生效倍率自动成组，档内平滑加权 WRR，卡片 `LB · xN` 标志。
- [LLM 服务商模型 Token Credits 定价与扣费](design/llm-provider-token-credit-pricing-zh.md)：HubCenter 官方服务商与 Hub 外部服务商按接入模型分别配置输入/输出 Credits 单价，账本快照、倍率、扣款与统计分离。
- [虚拟动态服务组](design/virtual-dynamic-service-group-zh.md)：一张服务组卡按业务换模型。组扣费、组内只加商；官方三档；每组每类流量。分类头已改口为 HC 一份 / 每个 Hub 租户一份；热路径只读 serving。转正前 `shadow` 旁路，训练页展示效果（不作门禁）。V1 / V1.1 已落地，V2 代码仍按组一头，全局头 + 独立页未落地。无独立页 / 门禁未过不准 `pipeline=on`。

## Hub 用户邀请与奖励

- [用户邀请与奖励机制设计](hub-user-invitation-design-zh.md)：桌面端排名按钮下的邀请入口、Hub 设置、双向 Credits 奖励、归因、统计与验收方案。

## Semantic tool routing

- [语义工具路由：事实层、协议入口与回合收口](design/semantic-tool-routing-intake-improvement-zh.md)：第六轮修订。第 7 门回写 LoopContext 聊天投影；continuation 必须先看 hint 事实。不另起规划器。
- [语义路由未命中：兜底工具决策](design/semantic-routing-miss-fallback-zh.md)：未命中用有界遗留面（剥 bash/写文件；弱生成只在已发布渠道钉 `generate_pdf`）。HostReject 留给确认、策略拒绝、grant 冲突与 `workflow_task`。

## Adaptive prompt & shared agent loop (cost / ops)

- [Ops cheat sheet](adaptive-prompt-and-shared-loop-ops.md): light/full prompt, shared loop strangler, light tools, misroute upgrade, quality A/B, CLI export/merge, Hub metrics.
- [Track summary](adaptive-prompt-track-summary.md): what shipped, env knobs, package map.

## Inference-time control (J-Space)

- [J-Space inspired improvements](jspace-inspired-improvements.md): **shipped** loop-owned `WorkingState` (P0–P5). `Ensure` vs splice, file-tool allowlist, deterministic `SelectAction`, `CloseOpenOnTrust`, one-shot done-check, AskUser/RecordAudio via `WorkingStateHolder`, active-turn horizon/goal projection. Complements drift / same-tool hard-stop; no assistant-prose parsing. Rollback: `MACLAW_WORKING_STATE=off`. Not a port, Skill, or session-wide goal store.

## Inference / Embedding

- [Gemma 3 / EmbeddingGemma 推理加速开发规划](design/gemma3-embedding-inference-optimization-plan-zh.md)：审阅 `corelib/embedding` 热路径后的落地规划。SIMD 固定几何（K=768/1152）、算子融合（QKV / SwiGLU tile / RMS residual）、scratch overlay 与 seq 分桶、AMD NPU 运行时探测（失败无感回退 CPU）。不改 `Embedder` 对外接口。

## Hub approval × MaClaw App (E2E)

- **[Track freeze](approval-hub-e2e-track-freeze-2026.md)**: closed; do not extend without a new named goal or regression.
- [E2E verification handbook](approval-e2e-verification-zh.md): automated + manual matrix (#1–#10), SoT notes, log greps.
- [Release-day checklist (~15 min)](approval-release-day-checklist-zh.md): one-click script + dual-desktop + empty-roles/Hub-jitter.
- [Improvement plan](approval-maclaw-app-e2e-improvement-plan-zh.md): phase status and architecture principles.
- One-click: `scripts/run-approval-e2e-checks.cmd` (or `.ps1`).
- CI: `.github/workflows/approval-e2e.yml` (path-filtered contract gate on PR/main).

## Agent Dynamic UI & Enterprise MIS Replacement

- [Agent dynamic UI runtime design](agent-dynamic-ui-runtime-design-zh.md): AG-UI event protocol, Skill/Tool/Business Object non-invasive adapters, right-side Task Panel, structured input validation, and business data persistence.
- [Agent dynamic UI implementation status](agent-dynamic-ui-runtime-implementation-status-zh.md): current MVP wiring for right-side operable UI, MIS, skill, and registered tool adapters.
- [Enterprise structured data design](maclaw-enterprise-structured-data-design-zh.md): MaClawDataSrv product and API design — datasets, business actions, views, dashboards, governance, and approval workflows.

## Knowledge Base (外脑)

- [Clean working set / on-demand retrieval](design/clean-working-set-on-demand-retrieval.md): empty first-turn working set; retrieve memory and knowledge via tools. Chinese overview: [clean-working-set-on-demand-retrieval-zh.md](design/clean-working-set-on-demand-retrieval-zh.md). Supersedes first-turn silent inject in knowledge-auto-recall-design.md.
- [Memory architecture improvement plan](memory-architecture-improvement-plan.md): Full architecture of the three-layer memory system (conversation history → long-term memory → cold storage) and improvement phases.
- [OpenHuman inspired improvements](openhuman-inspired-improvements.md): Comprehensive improvement plan inspired by tinyhumansai/openhuman — TokenJuice, Model Routing, Memory Tree, Subconscious Engine, Tool-Scoped Memory, and more.

## MaClawDataSrv

- [MaClawDataSrv package boundary](datasrv-structureddata-boundary.md): current split between `corelib/structureddata`, `datasrv/structureddata`, and `datasrv/cmd/maclaw-data-srv`.
- [MaClawDataSrv production operations guide](datasrv-production-ops-guide.md): deployment, environment variables, backup verification, restore checklist, and offline administrator recovery.
- [MaClawDataSrv Supabase-inspired architecture plan](datasrv-supabase-inspired-architecture-plan-zh.md): comparison with Supabase and phased architecture improvements for gateway, policy engine, events, object storage, auth, and portability.
- [MaClawDataSrv enterprise simple design](datasrv-enterprise-simple-design-zh.md): simplified enterprise information-system UX and API design focused on business tables, fields, records, views, access, and rigorous data controls.
- [MaClaw MIS end-to-end refactor plan](mis-end-to-end-refactor-plan-zh.md): full redesign plan across DataSrv semantic layer, MIS tools, skill generation, MaClaw App, AG UI, and App Studio.
