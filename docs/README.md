# Documentation Index

This directory contains architecture notes, design records, and operational guides.

## Adaptive prompt & shared agent loop (cost / ops)

- [Ops cheat sheet](adaptive-prompt-and-shared-loop-ops.md): light/full prompt, shared loop strangler, light tools, misroute upgrade, quality A/B, CLI export/merge, Hub metrics.
- [Track summary](adaptive-prompt-track-summary.md): what shipped, env knobs, package map.

## Hub approval × MaClaw App (E2E)

- [E2E verification handbook](approval-e2e-verification-zh.md): automated + manual matrix (#1–#10), SoT notes, log greps.
- [Release-day checklist (~15 min)](approval-release-day-checklist-zh.md): one-click script + dual-desktop + empty-roles/Hub-jitter.
- [Improvement plan](approval-maclaw-app-e2e-improvement-plan-zh.md): phase status and architecture principles.
- One-click: `scripts/run-approval-e2e-checks.cmd` (or `.ps1`).

## Agent Dynamic UI & Enterprise MIS Replacement

- [Agent dynamic UI runtime design](agent-dynamic-ui-runtime-design-zh.md): AG-UI event protocol, Skill/Tool/Business Object non-invasive adapters, right-side Task Panel, structured input validation, and business data persistence.
- [Agent dynamic UI implementation status](agent-dynamic-ui-runtime-implementation-status-zh.md): current MVP wiring for right-side operable UI, MIS, skill, and registered tool adapters.
- [Enterprise structured data design](maclaw-enterprise-structured-data-design-zh.md): MaClawDataSrv product and API design — datasets, business actions, views, dashboards, governance, and approval workflows.

## Knowledge Base (外脑)

- [Memory architecture improvement plan](memory-architecture-improvement-plan.md): Full architecture of the three-layer memory system (conversation history → long-term memory → cold storage) and improvement phases.
- [OpenHuman inspired improvements](openhuman-inspired-improvements.md): Comprehensive improvement plan inspired by tinyhumansai/openhuman — TokenJuice, Model Routing, Memory Tree, Subconscious Engine, Tool-Scoped Memory, and more.

## MaClawDataSrv

- [MaClawDataSrv package boundary](datasrv-structureddata-boundary.md): current split between `corelib/structureddata`, `datasrv/structureddata`, and `datasrv/cmd/maclaw-data-srv`.
- [MaClawDataSrv production operations guide](datasrv-production-ops-guide.md): deployment, environment variables, backup verification, restore checklist, and offline administrator recovery.
- [MaClawDataSrv Supabase-inspired architecture plan](datasrv-supabase-inspired-architecture-plan-zh.md): comparison with Supabase and phased architecture improvements for gateway, policy engine, events, object storage, auth, and portability.
- [MaClawDataSrv enterprise simple design](datasrv-enterprise-simple-design-zh.md): simplified enterprise information-system UX and API design focused on business tables, fields, records, views, access, and rigorous data controls.
- [MaClaw MIS end-to-end refactor plan](mis-end-to-end-refactor-plan-zh.md): full redesign plan across DataSrv semantic layer, MIS tools, skill generation, MaClaw App, AG UI, and App Studio.
