# Experience Learning Layer Improvement Plan

## Purpose

MaClaw already has long-term memory, tool routing, self-evolving skills, Swarm/SubAgent execution, and current-Hub A2A group discussion. The next improvement is not another isolated memory format. It is a shared experience learning layer that can safely distill many traces into reusable knowledge without collapsing into generic summaries.

This plan adapts the Combee idea from parallel prompt learning: map local traces into dense reflections, protect high-value minority signals, and reduce them hierarchically before writing to memory, routing, or discussion results.

## Design Principles

- Preserve specifics before abstraction: paths, commands, errors, numbers, explicit user corrections, objections, evidence, and rollback constraints must survive compression.
- Learn conservatively: repeated evidence can change routing scores or create skill candidates; single events can only create hints or review candidates.
- Keep provenance: every distilled item should know whether it came from conversation, workflow, Swarm, A2A discussion, tool usage, or manual input.
- Respect security boundaries: A2A learning only uses the context actually shared in the current-Hub discussion; high-risk tools never become silent automatic actions.
- Reuse existing systems: memory conflict detection, semantic dedup, usage tracker, skill memory, and group discussion result injection remain the integration points.

## Phase 1: Thin Slice

### 1. Memory Maintenance Pipeline

Add a lightweight `ExperienceDistiller` probe inside the memory pipeline. It should not rewrite memory yet. It classifies active entries by source, detects whether a maintenance cycle is entering a large-trace regime, and reports which entries deserve protection during later compression/reflection.

Initial outputs:

- Source group counts.
- Total active entries scanned.
- Whether layered distillation is recommended.
- Protected candidate count for pinned entries, instructions, feedback, A2A discussion, tool usage, and high-strength memories.

Integration point:

- `corelib/memory/pipeline.go`, before the existing compression/promote/reflect steps.

### 2. Tool Routing And Self-Evolution

Extend tool usage records so MaClaw can learn from richer execution traces, not only single-tool success/failure.

New optional evidence fields:

- `task_type`
- `tool_sequence`
- `error_class`
- `retry_count`
- `recovery_tool`
- `final_outcome`

Add a conservative `DistillRoutingHints` method that aggregates recent records into hints. Hints can later feed router weighting, skill nudge, or repair suggestions.

Initial rule:

- Prefer tools only with enough recent evidence and high success rate.
- Avoid tools only with enough recent evidence and high failure rate.
- Treat recovery tools as suggestions, not automatic execution.

### 3. A2A Discussion Improvement

Improve `group_discussion summarize_result` so multi-expert discussions do not flatten into generic consensus.

Initial behavior:

- Small discussions use the existing direct summary path.
- Large discussions use a Combee-lite path:
  - Map: group messages by participant and message kind.
  - Protect: keep objection/evidence/risk-bearing messages visible.
  - Reduce: summarize shards, then synthesize the final result from shard summaries.

Result fields should support:

- `summary`
- `rationale`
- `risks`
- `disagreements`
- `open_questions`
- `participant_contributions`
- `confidence`


## Phase 1 Implementation Status

Implemented in this slice:

- Memory pipeline emits `ExperienceDistillResult` through `PipelineResult.Experience`.
- Tool usage supports rich `ToolExperience` records and `DistillRoutingHints`.
- A2A group discussion summary supports layered summarization for large discussions and extended result fields for risks, disagreements, open questions, participant contributions, and confidence.

Deferred to Phase 2:

- Feeding memory protected candidates into compressor/promoter prompts.
- Applying routing hints directly inside router scoring.
## Phase 2: Integration

- Feed memory `ExperienceDistiller` protected candidates into compressor/promoter prompts.
- Let tool routing hints influence router scores with small bounded weights.
- Store A2A participant contribution quality and use it in expert selection.
- Add skill nudge candidates when a tool sequence succeeds repeatedly.


## Phase 2 Implementation Status

Implemented in this slice:

- Router score calculation now applies a bounded `RoutingHintAdjustment` from recent tool experience, and detailed route logs show non-zero routing hint adjustments.
- Memory compression receives `CompressionProtectionHint` for protected A2A, tool-usage, Swarm, instruction, pinned, and high-strength memories.
- Promotion and reflection prompts now include `experience_protection` hints so protected A2A/tool/Swarm traces retain concrete evidence during higher-level abstraction.
- A2A expert profiles expose optional `contribution_score` and `contribution_evidence`, auto-invite ranking uses that signal conservatively, and local completed contributions update the local profile statistics for future publication.
- A2A expert selection now has a read-only ranking preview path that exposes selected invitee IDs, stable ranked candidates, matched skills, security-group/profile/contribution score reasons, and an explicit non-executing boundary before any discussion is started.
- Repeated successful rich tool sequences now produce conservative `ToolSkillNudgeCandidate` review candidates; no skill is auto-created.

Completed after this slice:

- Memory Status now surfaces a read-only Experience Learning view with routing hint counts, skill nudge candidates, usage patterns, and protected memory counts.
- The usage-to-memory bridge now writes routing hints and skill nudge review candidates as `tool_usage` project knowledge, so later recall can explain what MaClaw learned without silently creating skills.

## Phase 3: Full Learning Loop

- Promote successful A2A decisions and tool recovery patterns into project memory.
- Add conflict-aware review for contradictory distilled memories.
- Extend UI visibility for what MaClaw learned from a discussion or tool trace into per-trace detail screens.
- Add per-tenant controls for whether cross-agent experience may influence future routing.

## Phase 3 Implementation Status

Implemented in this slice:

- Tool usage now distills repeated successful failed-tool -> recovery-tool flows into `ToolRecoveryPattern` signals.
- The usage-to-memory bridge writes recovery patterns as `tool_usage` project knowledge with identity tags for context, failed tool, and recovery tool.
- The read-only Experience Learning snapshot and Memory Status panel expose recovery pattern counts and expandable per-signal details.
- Completed A2A discussion summaries are promoted into `group_discussion` project memory with topic/question/discussion provenance tags.
- Contradictory A2A result signals for the same topic or question create `a2a_conflict_review` memory candidates instead of superseding older memories automatically.
- A2A group discussion now has a `use_cross_agent_experience` control: when disabled, local contribution scoring pauses, local contribution stats are hidden from the published expert profile, and remote contribution scores are ignored during auto-invite ranking.
- The read-only Experience Learning snapshot now includes normalized `trace_details` for routing hints, skill candidates, recovery flows, usage observations, memory-backed tool signals, A2A discussion results, and A2A conflict reviews; the Memory Status panel can inspect each signal with source, evidence, confidence, impact, tags, and original detail text.
- The A2A group discussion menu now links into Settings > Memory > Memory Status and passes the latest discussion id as a trace focus token; Experience Learning detail selection can resolve focus by trace id, source URL, kind, or `discussion:<id>` tag.
- Session History now exposes an Experience action that opens Settings > Memory > Memory Status focused on `session:<id>`, and the backend snapshot appends recent `session_history` trace details with session and platform provenance.
- Group Discussion settings now includes a current-Hub Discussion History panel with detail viewing and an Experience action that focuses `discussion:<id>` in Memory Status.
- Discussion detail can now run a preview-only summary through `GroupDiscussionSummarizeResult(preview=true)`, showing the layered summary without submitting to Hub, injecting into chat, or promoting memory.
- After preview, discussion detail exposes confirmed Inject to Chat and Submit Result actions; submit is hidden when Hub already has a result, and both actions continue to use backend readiness checks.
- Open discussion detail now supports confirmed follow-up messages with explicit message kind and confirmed expert invitations that refresh the Hub detail after writing.
- The agent-facing `group_discussion` tool now exposes `preview` for `summarize_result` and `message_kind` for `send_message`, keeping preview-only synthesis available outside the UI.
- Discussion detail now exposes confirmed state controls for pause, resume, and cancel through `GroupDiscussionSetState`, then refreshes the Hub detail so the collaboration loop can be managed from history.
- Public consultation APIs, HubClient, Wails, the discussion detail UI, and the agent-facing `group_discussion` tool now support proposal, review, and decision authoring, turning history detail into a full collaboration loop.
- A2A decisions now carry rationale and compact rollback triggers across public APIs, HubClient, Wails, UI detail, and the `group_discussion` tool; discussion detail can author escalation requests.
- Hub discussion detail now emits structured per-proposal `review_summaries`, and the UI uses that canonical summary with a local fallback for older Hub responses.
- The agent-facing `group_discussion` tool now exposes a compact read-only `workflow_state` action with readiness, proposal review totals, policy-satisfied proposal detection, decision/escalation flags, and suggested next action, so A2A collaboration routing can reason from a stable state model instead of raw message lists.
- `workflow_state` now also exposes structured `missing_answer_participants` and per-proposal `missing_reviewers`, letting agents and the UI target follow-up/review collection without parsing draft prose.
- `workflow_action_draft` now carries structured `target_participants` and `target_proposal_ids` alongside the natural-language draft, giving tool routing a stable non-executing target model before any confirmed follow-up, review request, or decision.
- `GroupDiscussionGetWorkflowState` is now a Wails/App method as well as an agent-tool action, and Discussion History detail renders the same workflow state model with readiness, proposal/review counts, decidable/blocking counts, suggested next action, and proposal policy badges.
- A2A project-memory promotion now preserves decision rationale, rollback triggers, and structured review counts as durable evidence tags/content instead of reducing them to the final summary only.
- A2A escalation is now summarized and promoted as governance evidence, preserving escalation reason, target, and raiser with a hashed target tag for later routing analysis.
- Discussion detail now renders per-proposal structured review badges for approvals, concerns, rejections, abstains, and reviewers, using Hub canonical summaries with local fallback statistics.
- Escalation requests now normalize empty targets to `iworkercenter` and empty raisers to the local MaClaw identity across Wails/app and agent-tool paths, keeping UI and tool escalation routing behavior consistent.
- A2A decisions with rollback triggers now emit rollback-condition hash tags and appear as `a2a_rollback_review` trace details in Experience Learning, making rollback execution explicitly review-gated rather than automatic.
- Experience Learning snapshot and UI now expose a `review_required_trace_count`, so A2A conflict and rollback review traces are visible at a glance instead of only inside trace detail rows.
- Experience trace detail ordering now prioritizes A2A conflict/rollback review items before ordinary routing and usage signals, and review-required counts are computed before trace-list truncation.
- Review-required trace details now carry explicit `review_action` guidance, and the Experience Learning UI can filter traces by All, Needs Review, A2A, Tools, or Sessions for faster governance review.
- Experience Learning snapshots now include full `trace_detail_count`, `trace_kind_counts`, and `trace_source_counts`; the trace filter controls display count badges based on full pre-truncation signal counts.
- Memory-backed A2A conflict/rollback traces can now record human review outcomes (`approved`, `rejected`, or `deferred`) from Memory Status; the backend appends an auditable review record, updates review-status tags, refreshes pending counts, and still performs no automatic rollback, routing, or policy change.
- Memory-backed `skill_nudge_candidate` signals now use the same review-gated path as A2A governance traces: repeated tool sequences appear as `skill_nudge_review`, can be approved/rejected/deferred with an audit note, and still never create or update a skill automatically.
- When A2A rollback/conflict memory or skill nudge memory is updated with changed content, prior review-status tags are cleared and `review_required` is restored, preventing stale approval from covering new evidence.
- Experience trace details now expose normalized `review_status` and `reviewed_at` fields; Memory Status renders status badges and review timestamps directly instead of requiring users to inspect raw tags.
- Experience Learning snapshots now include `review_status_counts`, and the Memory Status trace controls include a Reviewed filter for approved/rejected review outcomes.
- Experience trace details now parse the latest memory-backed review audit record into structured `reviewer`, `review_note`, and `review_count` fields, and Memory Status displays those fields beside review status and timestamps.
- Experience review records now default the reviewer to the cached local machine/client identity when the UI does not provide an explicit reviewer, while still falling back to `local` offline.
- Experience Learning snapshots now include `review_summaries` with per-status counts and latest representative review trace metadata; Memory Status renders a compact Review Queue that can focus required, deferred, approved, or rejected review states without approving anything automatically.
- Review-gated A2A conflict, A2A rollback, and skill-nudge traces now emit structured safe follow-up guidance through `next_action_kind` and `next_action`; approved reviews point to manual reconciliation, rollback workflow drafting, or skill drafting, while deferred/rejected reviews remain non-executing evidence.
- Experience Learning snapshots now aggregate `next_action_trace_count` and `next_action_kind_counts`, and Memory Status exposes a Next Actions stat plus an Actions trace filter so safe follow-up work can be reviewed from the full pre-truncation signal set.
- Experience Learning snapshots now include `next_action_summaries` with per-action counts and latest representative trace metadata; Memory Status renders a compact Action Queue that jumps into the Actions filter without executing the suggested work.
- Action-bearing review traces, including approved/deferred skill-nudge reviews, are prioritized ahead of generic routing and usage signals so Action Queue representative traces remain visible in the bounded trace list.
- Action Queue entries now focus the trace list by their concrete `next_action_kind`, with a clear control to return to the full non-executing Actions view.
- Reviewed next-action traces can now generate non-executing follow-up drafts from Memory Status. These drafts provide manual checklists for conflict reconciliation, rollback workflow drafting, evidence collection, or skill drafting, but they do not write files, execute rollback, change routing, or install skills.
- Memory Status can now record manual follow-up outcomes as `completed`, `blocked`, or `deferred`. Completed/blocked outcomes close the queued action, deferred outcomes keep it visible, and every outcome is appended as non-executing memory audit evidence.
- Experience Learning snapshots now aggregate follow-up audit state through `follow_up_trace_count` and `follow_up_status_counts`; Memory Status adds a Follow-ups stat and filter so completed/blocked/deferred manual outcomes remain inspectable after they leave the active Actions queue.
- Experience Learning snapshots now include `follow_up_summaries` with per-status counts, latest representative trace metadata, original action kind, and latest note; Memory Status renders a compact Follow-up Log that jumps into a status-focused Follow-ups filter without executing the recorded work.
- Experience Learning snapshots and the agent-facing `next_actions` / `queues` views now also aggregate follow-up evidence by `follow_up_action_kind`, and Memory Status renders a Follow-up Action Log that can jump directly to memory-maintenance draft reviews, routing-adjustment draft reviews, rollback workflow outcomes, skill-draft outcomes, or other recorded manual actions.
- The agent-facing `experience_learning follow_up_actions` action now returns a direct read-only action-kind audit queue with summaries, status counts, filtered details, and an explicit non-executing boundary, so self-evolution loops do not need to infer the right `trace_details` query shape.
- `QueryExperienceFollowUpActions` is now also exposed as an App/Wails method, giving UI and external callers the same filtered follow-up action audit model without pulling the full snapshot or changing any memory/routing/skill state.
- The agent-facing `experience_learning governance_summary` action now provides a compact read-only three-track overview for memory maintenance, routing/self-evolution, and A2A discussion governance queues, optionally including matched routing candidates for a query while preserving the same non-executing boundary.
- `GetExperienceGovernanceSummary` is now also exposed as an App/Wails method and includes a read-only `recommended_next_action` / reason derived from review queues, next actions, routing candidates, memory-maintenance pressure, and follow-up audit state.
- Governance summaries now include a structured `recommended_focus` hint, and Memory Status consumes that same App/Wails summary to render the recommended next action, memory/routing/A2A/queue counts, and the explicit non-executing boundary above the detailed Experience Learning queues; its recommended-action control only focuses the relevant queue/filter and never executes the suggested work.
- Governance summaries now also include a structured `recommended_tool_call` descriptor for the agent-facing `experience_learning` tool, pointing to the safest read-only queue, trace, or non-executing draft action with arguments and an explicit boundary instead of asking self-evolution loops to parse prose.
- Memory Status now renders and can copy the governance `recommended_tool_call` JSON directly in the governance summary panel, making the agent-facing safe inspection/draft call visible without executing it.
- Governance `recommended_tool_call` now uses `next_action_summaries.latest_trace_id` when available to point directly at the matching non-executing follow-up, skill, rollback, escalation, or conflict draft action, and falls back to read-only trace inspection when no trace id is available.
- Memory Status governance summaries can now run a task-type/query/tool-scoped preview through `GetExperienceGovernanceSummary`, showing bounded routing candidates and recommendations in the same non-executing panel without refreshing memory, changing router state, or running tools.
- Query-scoped governance previews now let matched bounded routing candidates drive the recommended focus for that preview, while the unscoped governance summary still prioritizes global review-required queues first.
- Memory Status routing previews can now send their scoped task/query/tool evidence directly into the existing non-executing routing-adjustment draft panel, so candidate review starts from the same bounded preview without changing routing, running tools, writing files, or installing skills.
- Memory Status governance summaries can now send layered/protected-memory pressure directly into the existing non-executing memory-maintenance draft panel, preserving the same review-only boundary before any compression, reflection, or memory rewrite step.
- Follow-up audit traces are now prioritized ahead of generic routing and usage signals, so completed or blocked manual outcomes remain visible in the bounded trace list when Follow-up Log jumps to a representative item.
- The agent-facing `experience_learning` tool now exposes read-only `snapshot` / `next_actions` inspection, filtered `trace_details` retrieval, `build_followup` draft generation, and `record_followup` audit recording, keeping self-evolution assistance available to the agent loop without granting review approval, rollback execution, routing changes, file writes, or skill installation authority.
- The `experience_learning next_actions` and `queues` views now include the same `recommended_focus` and `recommended_tool_call` descriptor as governance summaries, so agent loops can follow the safest read-only or non-executing draft path without parsing queue prose.
- The agent-facing `experience_learning` tool now exposes a read-only `queues` view that returns compact review, next-action, and follow-up summaries together with bounded queue details, so self-evolution loops can inspect pending work without pulling the full snapshot or executing any governance action.
- Governance `recommended_tool_call` now narrows follow-up inspection to the top concrete `follow_up_action_kind` when follow-up audit evidence exists, giving agent loops a stable read-only queue target instead of a broad follow-up scan.
- The agent-facing `experience_learning` tool now exposes read-only `routing_signals` retrieval filtered by task type, tool, or query text across routing hints, recovery patterns, skill-nudge candidates, and usage patterns, giving tool routing and self-evolution loops targeted evidence without changing routing scores or installing skills.
- `routing_signals` now includes read-only `score_adjustments` that explain the bounded `RoutingHintAdjustment` a query/tool would receive, including matched query tokens, direction, success/failure/recovery evidence, and reasons, without changing routing or running tools.
- The dynamic tool builder now applies the same bounded `RoutingHintAdjustment` used by the main router, so repeated recovery/success/failure evidence can conservatively nudge tool ordering during definition construction while staying capped below the primary retrieval and outcome signals.
- `experience_learning routing_signals` now returns simplified read-only `tool_candidates` and a routing recommendation derived from bounded score adjustments, so agents can inspect learned routing evidence without changing router state or executing tools.
- `experience_learning build_routing_adjustment_draft` now converts bounded routing candidates and score-adjustment evidence into a non-executing manual review draft with candidate directions, evidence counts, checks, and an explicit no-tool/no-routing-change boundary.
- Memory maintenance distillation now emits richer protected-memory candidate samples with title, bounded summary, tags, strength, pin state, updated time, and reason/source breakdowns; the agent-facing `experience_learning memory_candidates` action can inspect those protected candidates by source, reason, or query without mutating memory.
- `experience_learning memory_candidates` now also returns scanned/active entry counts, layered-maintenance recommendation fields, and an explicit non-executing boundary, so agents can decide whether to use retention-aware maintenance before compression without triggering any rewrite.
- `experience_learning build_memory_maintenance_draft` now turns protected-memory candidates into a non-executing retention checklist with anchors, reason/source counts, prompt-safe anchor blocks, and an explicit no-compress/no-rewrite boundary for maintenance review loops.
- Memory Status now calls the same Wails/App draft builders for non-executing memory maintenance and routing adjustment drafts, with optional query filters so users can inspect targeted retention anchors, routing candidate adjustments, checks, and safety boundaries from the UI before any rewrite-capable or router-changing step.
- Memory Status now renders compact protected-memory sample cards from the distiller, so users can see which concrete memories are being preserved before deeper compression or reflection.
- Experience Learning snapshots and Memory Status now surface the memory maintenance recommendation, layered-distillation reason, and read-only maintenance boundary beside protected memory samples, keeping retention decisions visible before any rewrite-capable maintenance step.
- The memory maintenance pipeline now passes protected experience samples from the distiller into LLM-backed compression merge and promotion prompts as retention anchors, and per-entry A2A/tool/swarm protection hints are included in semantic merge batches so concrete objections, recovery paths, rollback constraints, and minority views are not flattened during abstraction.
- Approved skill-nudge review traces can now generate a structured, non-executing skill draft through the agent-facing `experience_learning build_skill_draft` action and the Memory Status trace detail panel, including suggested name, evidence, tool sequence, tokens, checks, and explicit safety boundaries without writing files, installing skills, changing routing, or executing tools.
- Approved A2A rollback review traces can now generate a structured, non-executing rollback workflow draft through the agent-facing `experience_learning build_rollback_draft` action and the Memory Status trace detail panel, preserving rollback triggers, decision summary/rationale, manual workflow skeleton, checks, and explicit safety boundaries without executing rollback or changing policy.
- A2A escalation evidence now appears as its own `a2a_escalation_evidence` trace with a safe `prepare_escalation_brief` next action; the agent-facing `experience_learning build_escalation_brief` action and Memory Status trace detail panel can generate a non-executing escalation handoff brief with reason, target, raiser, requested manual action, checks, and safety boundaries without sending notifications or changing routing.
- Approved A2A conflict review traces can now generate a structured, non-executing reconciliation draft through the agent-facing `experience_learning build_conflict_draft` action and the Memory Status trace detail panel, preserving topic, question, new/existing discussion signals, manual decision options, checks, and safety boundaries without changing memory, policy, routing, files, or tools.
- Memory Status draft blocks now include a copy affordance for memory maintenance, routing adjustment, follow-up, skill, rollback, escalation, and conflict drafts, supporting manual review/editing outside the app without converting draft generation into execution.
- Memory Status can now record completed/blocked/deferred manual review outcomes for non-executing memory maintenance and routing adjustment drafts as `experience_learning` project-memory evidence; these records appear in the Follow-up Log without executing the draft, compressing memory, changing routing, writing files, installing skills, or running tools.
- Trace-level skill, rollback, escalation, and conflict drafts now use the same completed/blocked/deferred draft-review audit path from Memory Status; the backend classifies each draft review kind separately while still performing no skill creation, rollback, notification, routing change, file write, or memory rewrite.
- Draft-review audit records now preserve `source_trace_id` provenance for trace-level drafts, and Memory Status renders that source trace so follow-up evidence can be linked back to the original skill, rollback, escalation, or conflict signal without executing anything.
- The agent-facing `trace_details` and `follow_up_actions` views can now filter by `source_trace_id`, giving governance loops a precise read-only way to inspect draft-review audit records linked to a specific original trace.
- The agent-facing `group_discussion rank_experts` action and `GroupDiscussionRankExperts` app/Wails method now expose the same auto-invite scoring as a read-only preview, including contribution-score reasons and matched-skill evidence, while making clear that no consultation or invitation is created.
- The agent-facing `group_discussion escalation_route` action and `GroupDiscussionSuggestEscalationRoute` app/Wails method now provide a read-only escalation route suggestion with target, reason, triggers, blocking/decidable counts, existing escalation context, and an explicit non-executing boundary before any escalation is sent.
- Read-only A2A escalation routing now applies conservative metadata policies for security/compliance, release/rollback, and self-evolution discussions, enriching the suggested target, reason, and triggers while still requiring the existing explicit confirmation before escalation.
- A2A escalation route suggestions now carry `policy_evidence` with the selected policy target, trigger, reason, and bounded matched keywords; workflow action drafts and Discussion History render that evidence before any form-fill or confirmed escalation path.
- Discussion History detail now renders the same read-only escalation route suggestion and can copy the suggested reason/target into the escalation form, while the actual escalation still requires the existing explicit confirmation path.
- `workflow_state` now embeds the read-only escalation route suggestion, so agent and UI callers can inspect readiness, proposal/review state, next action, and escalation routing from a single stable state payload without executing an escalation.
- `workflow_state` now also exposes its own `recommended_focus_context` and safe `recommended_tool_call`, giving agent/UI callers a reusable discussion-level inspection target before drilling into escalation, rollback, or workflow-action drafts.
- The agent-facing `group_discussion workflow_action_draft` action and `GroupDiscussionBuildWorkflowActionDraft` app/Wails method now produce non-executing follow-up drafts for escalation, decision, summary preview, targeted follow-up, or waiting states, including confirmation requirements and suggested arguments without calling those actions.
- `workflow_state` now also embeds that non-executing workflow action draft, and Discussion History detail renders it with a checklist plus optional form-fill support for escalation/decision drafts while still requiring the normal explicit confirmation for execution.
- Non-executing memory-maintenance and routing-adjustment drafts now include a safe `recommended_tool_call` template for `record_draft_review`; the template intentionally omits status so it cannot record audit evidence until a caller explicitly supplies a completed/blocked/deferred review outcome.
- Trace-level follow-up, skill, rollback, escalation, and conflict drafts now carry safe `recommended_tool_call` handoffs back to exact `trace_details` inspection, keeping draft review loops anchored to the original trace before any manual audit outcome is recorded.
- Memory Status now renders and copies recommended tool calls for draft outputs, so users and agent loops can see the next safe inspection/audit handoff without executing rollback, notification, routing, memory rewrite, file write, skill installation, or tool execution.
- After Memory Status records a memory-maintenance, routing-adjustment, skill, rollback, escalation, or conflict draft review outcome, it now renders the backend-returned recommended tool call for inspecting the newly written audit trace, closing the safe review loop without executing the reviewed draft.
- Memory-backed A2A conflict/rollback and skill-nudge review submissions now return a structured review record with a safe exact-`trace_details` recommended tool call, and Memory Status renders that handoff after approval/rejection/defer recording so human review outcomes remain inspectable without triggering the next action.
- Manual follow-up outcome recording now also returns a structured follow-up record with the same exact-trace safe inspection handoff, and Memory Status renders it after completed/blocked/deferred recording so outcome audit can be inspected without replaying the follow-up action.
- Discussion History `Use Draft` now handles `preview_summary` drafts by invoking the existing preview-only summary path, which may compute a local preview but still does not submit to Hub or inject into chat.
- A2A workflow action drafts now carry compact evidence lines and explicit risk boundaries, and Discussion History renders both before suggested arguments so escalation/decision form-fill remains grounded in visible review/readiness signals.
- A2A workflow action drafts now treat existing decisions, results, and escalations as terminal governance states: they emit read-only result-review or escalation-handoff drafts with decision/rollback/escalation evidence instead of proposing duplicate decisions, summaries, or escalations.
- A2A decision drafts now include an editable review-backed rationale and conservative rollback trigger draft, and the Discussion History `Use Draft` flow fills those fields without recording the decision.
- A2A workflow action drafts now cover open proposal review collection: they identify missing reviewers, draft a scoped review request, and let Discussion History fill the message composer without sending it.
- A2A follow-up drafts now derive missing expected answerers from participant and answer-message evidence, generate a scoped question draft, and let Discussion History `Use Draft` fill the message composer without sending it.
- A2A waiting-state drafts now identify missing initial answerers and can fill a scoped expert-answer reminder into Discussion History without sending it.
- A2A workflow state now exposes structured `workflow_blockers` plus per-proposal blockers for missing answers, pending reviews, blocking reviews, terminal results, and existing escalations; Discussion History renders these blockers directly beside readiness and proposal policy status.
- A2A workflow state now carries and renders its own explicit read-only/non-executing boundary, so status inspection cannot be mistaken for proposal, review, decision, escalation, message, result, or state mutation.
- Discussion History workflow drafts can now copy their title, summary, targets, evidence, boundaries, checklist, suggested arguments, and non-executing boundary for manual review while keeping `Use Draft` as a separate form-fill-only action.
- A2A workflow action drafts now include a structured `recommended_tool_call` descriptor for the agent-facing `group_discussion` tool. The descriptor points only to safe inspection, preview, or draft-building calls while keeping state-changing `suggested_arguments` behind the existing explicit confirmation/form-fill path.
- A2A workflow action drafts now also include `recommended_focus_context`, and their `recommended_tool_call` carries matching `discussion_focus_context`, so agents can preserve consultation/action/proposal/participant/escalation targets without parsing prose before any confirmed follow-up, review request, decision, escalation, or rollback-readiness inspection.
- A2A escalation-route and rollback-readiness outputs now also include `recommended_focus_context`; their safe recommended tool calls carry matching `discussion_focus_context`, and the discussion history panel displays/copies these contexts so escalation and rollback inspections preserve consultation, trigger, proposal, and owner-review targets.
- Discussion History now renders and copies that workflow-draft focus context alongside suggested arguments and the safe recommended tool call, making the target model visible before any `Use Draft` form-fill action.
- The agent-facing `group_discussion` result wrapper now normalizes safe handoff descriptors by backfilling missing `recommended_focus_context`, `discussion_focus_context`, `non_executing=true`, and non-executing boundaries, so future read-only A2A actions cannot accidentally drop the safe inspection contract.
- The agent-facing `experience_learning` result wrapper now applies the same safe-handoff normalization to top-level recommended tool calls, including `governance_focus_context` aliases for UI copy/display flows, so memory-maintenance, routing, and self-evolution actions keep a consistent non-executing contract even when individual builders return a minimal descriptor.
- Experience trace-inspection, follow-up audit, and draft-review recommended tool calls now reuse the same normalization helper before they are embedded in nested draft/record objects, so expanded UI details and agent loops see matching focus aliases and non-executing boundaries without relying on each builder to duplicate the contract.
- The built-in `experience_learning` tool description now documents the normalized `governance_focus_context`, `non_executing=true`, and explicit boundary contract, so tool discovery and schema tests line up with the actual result wrapper behavior.
- A2A direct App/Wails result finalizers for status, expert ranking, readiness, summary preview, workflow state, workflow action draft, escalation route, and rollback readiness now also normalize safe recommended tool calls, keeping UI-direct responses aligned with the agent-facing `group_discussion` JSON wrapper.
- A2A workflow state now embeds read-only rollback readiness for decided discussions with rollback triggers, and the agent-facing `group_discussion rollback_readiness` action can match optional evidence against those triggers while returning only audit guidance, matched/unmatched conditions, and a safe inspection `recommended_tool_call`.
- Discussion History now renders rollback readiness beside workflow state, escalation route, and workflow drafts, making matched rollback triggers visible and copyable without executing rollback, changing Hub state, rewriting memory, or changing routing.
- When rollback readiness is actually triggered by current discussion evidence, A2A workflow drafts now pivot from generic result review to a dedicated non-executing rollback review draft, so agent/UI callers can follow the same safe governance path before any owner-approved rollback workflow is written or run.
- Triggered rollback signals are now also preserved in project memory as `rollback_triggered` evidence with matched-trigger hashes, and Experience Learning upgrades their next action from a generic review signal to a more explicit triggered-rollback review path without authorizing execution.
- Governance `recommended_next_action` / `recommended_tool_call` now prioritize triggered rollback reviews ahead of the generic review queue and narrow the safe read-only inspection call to `a2a_rollback_review` traces with `review_triggered_rollback_signal`, so agent loops can start from the highest-risk rollback evidence first.
- The Experience Learning panel now surfaces `review_triggered_rollback_signal` with an explicit triggered-rollback label and warning-state governance summary, so UI reviewers see the high-risk rollback signal before drafting or reusing any rollback workflow.
- Triggered rollback reviews now keep the same warning semantics after focus/open: governance focus routes directly to the action queue filter, and trace details render a rollback warning badge plus a read-only review notice instead of falling back to generic next-action wording.
- After a triggered rollback signal is approved and naturally advances into `draft_rollback_workflow`, follow-up drafts and audit records still preserve the triggered-rollback semantics from matched evidence instead of collapsing back to a generic rollback follow-up template.
- Experience governance now also recognizes triggered rollback follow-up audit summaries as their own high-risk queue, so agent/tool callers can jump straight into `a2a_rollback_review` follow-up inspection instead of opening a generic follow-up bucket.
- `follow_up_actions` inspection now exposes and accepts triggered-rollback markers directly (`triggered_rollback`, `triggered_count`, `triggered_rollback_only`), so both UI and agent/tool callers can filter the high-risk rollback audit trail without re-deriving it from raw trace text.
- Triggered rollback follow-up summaries now also carry a recommended trace target, and the Experience Learning panel uses that target when opening rollback-focused follow-up summaries so reviewers land on the most relevant audit trace immediately.
- Triggered rollback follow-up summaries now also carry a compact recommendation reason, so both cards and agent/tool consumers can explain why that trace was prioritized without reopening the full audit record first.
- Triggered rollback focus state in the Experience Learning panel now carries that same recommendation reason into the active follow-up filter, so reviewers can see why the rollback-risk view was chosen even after they switch from summaries into the trace list.
- Triggered rollback follow-up focus now also marks the recommended trace inside the trace list itself and shows the compact recommendation reason inline, so reviewers can tell which audit trace was prioritized before opening the detail pane.
- The selected trace detail and follow-up draft area now preserve that same recommended rollback trace context and recommendation reason, so reviewers keep one continuous explanation from summary, to list, to detail, to follow-up review handoff.
- Copying a rollback-focused follow-up or rollback draft from trace detail now includes a compact recommendation-context header, so exported handoff text keeps the same high-risk explanation outside the UI.
- That same recommended rollback context now also prefixes and enriches skill, escalation, and conflict drafts generated from the selected trace detail, so all manual draft branches keep a consistent rollback-risk explanation and copied handoff text.
- Review note and draft-review note placeholders now also adapt to the recommended rollback trace context, so reviewers get rollback-specific prompts when recording audit outcomes instead of generic note guidance.
- Review, follow-up, and draft-review success messages now also adapt to the recommended rollback trace context, so completion feedback keeps the same high-risk framing after a reviewer records the outcome.
- Read-only trace detail fields for reviewer, review note, follow-up action, follow-up actor/time, and follow-up note now also switch to rollback-audit labels when a recommended rollback trace is active, so historical records keep the same context after submission.
- Rollback-focused follow-up summary cards now also render a compact `Rollback audit` history line with triggered counts and the latest note/reason, so reviewers can distinguish rollback audit follow-ups from ordinary follow-ups before opening trace detail.
- Governance Summary queue stats now also surface the active rollback-audit follow-up count and leading reason, so the top-level governance view exposes triggered rollback follow-up pressure before reviewers drill into the queue.
- The Experience Learning top stat grid now also includes a `Rollback Audit` count, so rollback-audit pressure is visible from the shallowest overview layer instead of only inside governance or follow-up sections.
- Review Queue cards for rollback-review traces and Action Queue cards for `review_triggered_rollback_signal` now also use the warning summary treatment and a compact rollback-audit line, so high-risk queue cards match the same visual/opening path semantics as rollback-focused follow-up cards.
- Summary ranking now also prioritizes rollback-audit review/action/follow-up cards ahead of ordinary cards and then falls back to count/recency, so the highest-risk rollback items stay near the top instead of being pushed out by noisier generic traffic.
- Trace-detail list fallback ordering now also respects a remembered priority trace id and rollback preference, so opening a high-risk summary card is more likely to land on the intended rollback trace even when the original selected id is filtered out or missing.
- Governance focus now also derives its own priority trace from the filtered rollback-aware trace set and seeds both selected/priority trace ids from that result, so top-level governance entry points land on the same kind of high-risk trace that summary cards already prefer.
- Rollback-audit priority trace context is now shared across review/action/follow-up/governance entry paths instead of only follow-up cards, so the trace list badge, focus reason, and detail-pane recommendation stay aligned no matter which high-risk summary opened the audit trace.
- Governance Summary now previews the current priority rollback trace target beside the recommended action itself, including the target title/id and compact reason, so reviewers know where `Focus` will land before they leave the top-level governance view.
- `Recommended Tool Call` output in Governance Summary now embeds the same priority rollback trace context and reason in a structured `governance_focus_context` JSON field, so copied/read-only agent guidance keeps the target-trace explanation together with valid tool-call payload data.
- Governance priority trace fallback now also derives missing filters/action kinds from the recommended action itself, so `review_triggered_rollback_signal` and triggered rollback follow-up recommendations still land on rollback-aware traces even when the backend omits an explicit `recommended_focus`.
- Backend governance `recommended_tool_call` now also carries structured `governance_focus_context` for triggered rollback reviews/follow-ups and draft/follow-up trace targets, so non-UI agent consumers receive the same priority trace id/title/reason without depending on frontend decoration.
- Backend governance `recommended_tool_call` now also mirrors that payload as `recommended_focus_context`, keeping the focus-context contract consistent with routing, memory, trace, follow-up, and A2A handoff outputs while retaining `governance_focus_context` for compatibility.
- Governance summaries and queue/next-action responses now also expose top-level `recommended_focus_context`, and the Experience Learning panel prefers that backend context before local fallback derivation, keeping UI and agent focus targets aligned.
- The `experience_learning` tool definition now documents `recommended_focus_context` in its built-in description, so tool-routing agents can discover the priority-trace context contract before invoking governance summary or queue actions.
- The agent-facing `snapshot` result now starts the same safe handoff chain by exposing a top-level `recommended_focus_context`, a non-executing `governance_summary` recommended tool call, and a read-only boundary, so memory/routing/A2A consumers can enter governance review without guessing the next inspection action.
- The built-in `experience_learning` tool description now documents that `snapshot` points to `governance_summary` and that `trace_details` carries a read-only `non_executing_boundary`, so agents can discover the full entry-to-trace safe handoff contract before invoking it.
- The agent-facing `governance_summary` result now mirrors `recommended_focus_context`, `recommended_tool_call`, `recommended_next_action`, and `non_executing_boundary` at the tool-result top level as well as inside the nested summary, making governance routing consistent with queues, follow-up actions, trace details, and draft builders.
- `routing_signals` results now also expose `recommended_focus_context` with the leading candidate tool, direction, task type, query, and read-only rationale, so routing/self-evolution loops can carry candidate evidence into manual adjustment drafts without reparsing recommendation text.
- `routing_signals` results now also include a safe `recommended_tool_call` for `build_routing_adjustment_draft`, preserving the same task/tool/query filters while still preventing tool execution, router mutation, file writes, policy changes, or skill installation.
- `memory_candidates` results now also expose `recommended_focus_context` for the leading protected retention anchor, including memory trace id, title, reason/source, query, and maintenance rationale before any compression or rewrite-capable step.
- `memory_candidates` results now also include a safe `recommended_tool_call` for `build_memory_maintenance_draft`, preserving the same reason/source/query filters while still preventing compression, promotion, deletion, rewrite, routing changes, file writes, tool execution, or skill installation.
- `follow_up_actions` results now expose `recommended_focus_context` alongside `recommended_trace_id/title/reason`, so agents that execute the recommended inspection call keep the same priority-trace target after leaving governance summary.
- `follow_up_actions` results now also include a safe `recommended_tool_call` back into `trace_details` for the prioritized follow-up audit trace, preserving action/status/kind filters while staying read-only.
- `trace_details` results now also expose the same recommended trace target and `recommended_focus_context`, so action/review inspection calls preserve priority-trace context just like follow-up inspections.
- Action-bearing `trace_details` results now also include a safe `recommended_tool_call` for the matching non-executing follow-up, skill, rollback, escalation, or conflict draft builder, using the recommended trace id while still forbidding approval, execution, routing changes, memory rewrites, file writes, notifications, tool runs, or skill installation.
- `trace_details` now accepts an exact `trace_id` filter, and follow-up/draft-review audit recording returns a safe `recommended_tool_call` that uses it for precise read-only inspection of the recorded audit trace.
- `trace_details` query results now also carry a structured read-only `non_executing_boundary` at both App result and agent-tool top level, so governance, follow-up, and draft-builder handoffs preserve the no-write/no-execute contract while moving between exact trace inspection and non-executing draft generation.
- Non-executing follow-up, skill, rollback, escalation, and conflict draft outputs now also carry `recommended_focus_context`, so agent/UI consumers keep the priority trace id/title/reason all the way from governance recommendation into the manual draft artifact.
- `record_followup` and `record_draft_review` tool outputs now preserve `recommended_focus_context`, so the audit-recording step returns the same priority trace context instead of ending the governance handoff with a bare status or draft-review id.
- `record_followup` and `record_draft_review` records now also include mirrored `non_executing_boundary` fields at both nested-record and top-level tool result positions, making it explicit that recording audit evidence did not execute rollback, apply routing, rewrite memory beyond audit notes, write files, run tools, send notifications, or install skills.
- Memory Status trace detail now displays the returned safety boundary after recording follow-up or review outcomes, so UI reviewers see the same audit-only/no-execution contract that the agent-facing tool receives.
- The Experience Learning panel `Focus` action now prefers backend `recommended_focus_context` before local fallback lookup, keeping clickable governance focus behavior aligned with the agent-facing recommended tool-call payload.
- Trace-level draft review controls now pass the draft's `recommended_focus_context` back as the review `source_trace_id` when available, so UI-recorded skill, rollback, escalation, and conflict draft reviews remain attached to the intended priority trace.
- Memory maintenance and routing adjustment draft outputs now also include `recommended_focus_context`; maintenance drafts point at the leading retention anchor when available, and the panel uses that context as `source_trace_id` when recording maintenance/routing draft reviews.
- Maintenance and routing draft panels now display the draft `recommended_focus_context` before the draft body, so reviewers can see the priority trace/reason that will be preserved when recording the manual draft review.
- Manual follow-up drafts now carry a structured `non_executing_boundary` both inside the draft object and at the `experience_learning build_followup` tool-result top level, matching the skill, rollback, escalation, conflict, memory-maintenance, and routing-adjustment draft contracts.
- The agent-facing `experience_learning` tool now mirrors safe handoff fields at the top level for follow-up action queues, maintenance/routing draft builders, trace-level draft builders, and draft-review recording, so tool-routing/self-evolution loops can consume `recommended_focus_context`, `recommended_tool_call`, and safety boundaries without digging through nested records.
- The agent-facing `group_discussion` tool now mirrors safe handoff fields at the top level for `workflow_state`, `workflow_action_draft`, `escalation_route`, and `rollback_readiness`, while preserving the nested result object; A2A planning loops can therefore inspect the recommended non-executing follow-up/route/readiness target without parsing UI-oriented detail payloads.
- A2A `recommended_tool_call` objects now include both `recommended_focus_context` and the legacy `discussion_focus_context` alias with the same consultation/proposal/participant/rollback target data, keeping the handoff contract consistent with the Experience Learning tool while preserving existing discussion-specific consumers.
- Discussion History now renders workflow state, escalation route, rollback readiness, and workflow draft handoffs through a shared copyable Safe Handoff block with pretty-printed `recommended_focus_context`, `recommended_tool_call`, and `non_executing_boundary`, so UI users and agent operators see the same non-executing payload shape.
- `rank_experts` now also returns a safe handoff with selected invitee IDs, topic/question/risk/skill filters, and a non-executing ranking-preview tool call, making expert selection reviewable before any `start_authorized` call or Hub invitation is sent.
- `suggest` now returns a pre-authorization safe handoff as well: when group discussion is available it points to read-only `rank_experts`, and when unavailable it falls back to `status`; either way it explicitly records that no discussion was started, no invitations were sent, and no Hub state changed.
- Basic `readiness` now also returns a safe handoff: ready discussions point to `summarize_result` with `preview=true`, while not-ready discussions point to read-only `workflow_state`; neither path submits a result, injects chat, sends messages, invites experts, or mutates Hub state.
- `summarize_result` now returns the same top-level safe handoff when called with `preview=true`, pointing follow-up inspection to read-only `get_detail` while explicitly stating that no result submission, chat injection, memory promotion, message send, Hub mutation, or routing change occurred; non-preview summary calls remain write-capable and are intentionally not marked non-executing.
- Discussion History now also renders the summary-preview safe handoff beside the preview body, so UI reviewers can copy the same `recommended_focus_context`, `recommended_tool_call`, and boundary that agent callers receive from the tool.
- `get_detail` now preserves the safe-handoff chain after a summary preview: detail inspection mirrors a read-only focus context at the top level and points onward to `workflow_state`, without submitting results, injecting chat, sending messages/invitations, mutating Hub state, promoting memory, or changing routing.
- A2A list and cleanup-preview reads now follow the same contract: `list_mine` points to the leading `get_detail` inspection, `get_discussion` points to full detail, and `cleanup_stale dry_run=true` points to either stale discussion detail or another dry-run preview, while non-dry cleanup remains a write-capable cancellation path and is not marked non-executing.
- Manual `ReviewExperienceTrace` outcomes now return `non_executing_boundary` alongside `recommended_focus_context` and `recommended_tool_call`, so UI and agent callers can see that approval/rejection/defer only recorded audit evidence and did not run rollback, create/install skills, change routing, write files, run tools, or send notifications.
- A2A `status` and `list_experts` now start the same non-executing handoff chain: status points to safe expert/list/detail inspection depending on current state, and expert discovery points to a ranking preview without starting a discussion, inviting experts, sending messages, mutating Hub state, promoting memory, or changing routing.
- The agent-facing `experience_learning` tool now exposes `record_review` for explicit manual review outcomes, mirroring `review_record`, `recommended_focus_context`, `recommended_tool_call`, and `non_executing_boundary` at the top level so review audit recording joins the same safe chain as follow-up and draft-review recording.
- Memory Status trace details now surface the current read-only governance/trace safety boundary before any review, follow-up, or draft action, so UI reviewers see the no-execution/no-rewrite contract at inspection time instead of only after recording an audit outcome.
- Discussion History expert discovery now renders a copyable Safe Handoff block after loading invite candidates, matching the `list_experts` tool contract and making it explicit that discovery/ranking does not start a discussion, invite experts, send messages, mutate Hub state, promote memory, or change routing.
- The assistant title-bar A2A status menu now also renders a compact copyable Safe Handoff for direct `GroupDiscussionStatus` reads, using backend handoff fields when available and otherwise generating a read-only status/detail inspection contract locally without starting discussions, inviting experts, sending messages, mutating Hub state, promoting memory, or changing routing; the fallback omits `consultation_id` unless it has a concrete discussion target.
- `GroupDiscussionStatus` itself now carries the same read-only safe handoff fields in the Wails/App model, so title-bar, settings, and future UI consumers can share the agent-facing `status` tool contract without rebuilding it from raw counts.
- The agent-facing `group_discussion status` action now reuses those `GroupDiscussionStatus` handoff fields instead of recomputing them separately, keeping App/Wails/UI/tool consumers aligned on the same non-executing status contract.
- Pending A2A invites now enrich the status safe handoff with invite/session/from/role/topic context while deliberately keeping the recommended tool call on read-only `status`, avoiding accidental drift toward `process_invites`, which may auto-accept or reject invites under local policy.
- Routing/self-evolution signal results now carry their own shared `non_executing_boundary`, and the agent-facing `routing_signals` action reuses that model field so direct App calls, governance summaries, and tool responses agree that routing evidence inspection does not execute tools, change routing, create/install skills, rewrite memory, write files, send notifications, or update policy.
- Governance summaries now also embed the routing signal `recommended_focus_context`, `recommended_tool_call`, and `non_executing_boundary` inside `routing_self_evolution`, and the Memory Status governance preview renders that copyable safe handoff beside scoped routing evidence, so reviewers do not have to infer the no-execute contract from the top-level summary.
- Governance summaries now also embed a memory-maintenance safe handoff inside `memory` when protected anchors or layered maintenance are present, pointing to read-only `memory_candidates` inspection or non-executing `build_memory_maintenance_draft` while preserving the memory maintenance safety boundary in the Memory Status preview.
- Governance summaries now also embed an A2A safe handoff inside `a2a_discussion` when discussion, conflict, rollback, escalation, or review-required evidence exists, pointing to read-only `trace_details` inspection and explicitly forbidding discussion starts, invitations, messages, result submission, chat injection, memory promotion, rollback execution, or routing changes.
- Memory Status governance preview now renders each Memory, A2A, and Routing handoff with its focus context before the recommended tool call and safety boundary, making the reason/target visible to human reviewers instead of burying it inside the copied JSON.
- Memory Status now uses one shared governance handoff renderer for Memory, A2A, and Routing blocks, keeping future safe-handoff fields visually consistent across the three improvement tracks.
- The `experience_learning governance_summary` tool-result path now has regression coverage proving nested Memory and A2A handoff blocks survive JSON serialization with focus context, recommended tool call, and safety boundary intact for agent consumers.
- Routing governance-summary JSON coverage now also asserts the nested routing focus context survives serialization with candidate tool, query, and direction intact.
- Memory Status governance handoff focus contexts are now copyable just like recommended tool calls, so reviewer-visible context can be moved into external review notes without copying the larger tool-call payload.

Deferred after this slice:

- Deeper per-discussion action screens can later add actual owner-approved rollback execution connectors, richer owner-approved escalation routing policies based on discussion metadata, and UI-assisted skill/conflict draft editing or publishing from approved governance reviews; the history/detail trace focus, preview summary path, bounded routing-hint adjustments in both router and dynamic builder, protected experience retention anchors in memory maintenance prompts, follow-up messages, invitations, post-preview result actions, state controls, proposal/review/decision loop, decision rationale/rollback capture, rollback readiness inspection, rollback review traces/counts, review outcome recording, draft-review evidence recording, skill nudge review gating, non-executing skill/rollback/escalation/conflict draft generation, escalation authoring, canonical review-summary payload, structured review-summary display, target normalization, escalation evidence memory, expert-ranking preview/explanations, read-only escalation route suggestion, exact trace inspection, and safe recommended_tool_call handoffs are now in place.

## Phase 1 Acceptance Criteria

- Memory pipeline emits an experience distillation summary without changing existing memory content.
- Tool usage remains backward-compatible with old JSON records and can produce routing hints from richer traces.
- A2A summary keeps objections, risks, and participant contributions visible in large discussions.
- Existing tests continue to pass, with focused tests added for the new behavior.

